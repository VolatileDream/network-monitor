package ping

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
)

var (
	errNoMonitor = errors.New("monitor not found")
)

type monitor struct {
	dest netip.Addr
	wire []outstandingPacket
}

type outstandingPacket struct {
	Seq  int // actually uint16
	Sent time.Time
}

type monitorsByInterface struct {
	// Cancel func provided by ctx used to start them.
	cancel func()
	source netip.Addr
	socket *icmp.PacketConn

	result chan<- *PingResult

	lock sync.Mutex
	// Map of destination to id
	monitors map[netip.Addr]*monitor

	// next seq
	sequence uint16
}

func (m *monitorsByInterface) nextSeq() uint16 {
	n := m.sequence
	m.sequence += 1
	return n
}

func pinger(ctx context.Context, src *monitorsByInterface) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		sendall(src)
	}
}
func sendall(src *monitorsByInterface) {
	src.lock.Lock()
	defer src.lock.Unlock()

	for dest, m := range src.monitors {
		src.sequence += 1
		now := time.Now()
		echo := icmp.Echo{
			ID:   0, // can't be set by us.
			Seq:  int(src.sequence),
			Data: []byte("VolatileDream//web/network-monitor"),
		}
		if err := SendIcmpEcho(src.socket, &echo, dest); err != nil {
			fmt.Printf("problem with sending packet: %v\n", err)
			continue
		}
		m.wire = append(m.wire, outstandingPacket{
			Seq:  int(src.sequence),
			Sent: now,
		})
	}
}

func receiver(ctx context.Context, src *monitorsByInterface) {
	// Receiver is responsible for closing the socket
	defer src.socket.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Keep extending the deadline to have an idle check.
		src.socket.SetReadDeadline(time.Now().Add(5 * time.Second))
		echo, err := ReadIcmpEcho(src.source, src.socket)

		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			} else if errors.Is(err, os.ErrClosed) {
				// unexpected!
				// Receiver is responsible for closing the socket when exiting.
				log.Printf("icmp socket closed: %v", err)
				return
			}
			// TODO: classify and do something better.
			log.Printf("receiver socket error on read: %v", err)
			continue
		}

		if err := src.handleReceive(echo); err != nil {
			log.Printf("error handling received packet: %v", err)
		}
	}
}
func (m *monitorsByInterface) handleReceive(echo *IcmpResponse) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	monitor, ok := m.monitors[echo.from]
	if !ok {
		//return errNoMonitor
		return fmt.Errorf("monitor not found for: %s", echo.from)
	}

	// Try to find the the number in the outstanding packet list.
	found := false
	for i, outstanding := range monitor.wire {
		if outstanding.Seq == echo.echo.Seq {
			p := &PingResult{
				Sent: outstanding.Sent,
				Recv: echo.when,
				Src:  m.source,
				Dest: monitor.dest,
			}
			m.result <- p
			found = true
			monitor.wire = append(monitor.wire[:0], monitor.wire[i+1:]...)
			break
		}

		// missing packet...
		p := &PingResult{
			Sent: outstanding.Sent,
			Src:  m.source,
			Dest: monitor.dest,
		}
		m.result <- p
	}

	if !found {
		// Not clear if we should drop the contents of wire here or not?
		// monitor.wire = monitor.wire[:0]
		log.Printf("did not find packet for %v seq: %d", echo.from, echo.echo.Seq)
	}

	return nil
}
