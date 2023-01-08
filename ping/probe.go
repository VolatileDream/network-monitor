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

	"github.com/VolatileDream/workbench/web/network-monitor/config"
	"github.com/VolatileDream/workbench/web/network-monitor/icmp"
	"github.com/VolatileDream/workbench/web/network-monitor/resolve"

	xicmp "golang.org/x/net/icmp"
)

const (
	maxPendingPackets = 100
)

var (
	errNoMonitor = errors.New("monitor not found")
)

type pinger struct {
	cancel   func()
	interval time.Duration
	targets  []resolve.Resolution

	source netip.Addr
	socket *xicmp.PacketConn

	result chan<- *PingResult

	lock sync.Mutex
	// Map of destination to id
	monitors map[netip.Addr]*monitor

	// next seq
	sequence uint16
}

type monitor struct {
	target config.LatencyTarget
	wire   []outstandingPacket

	// We count send errors to possibly ignore the ip.
	sendErrs int
}

type outstandingPacket struct {
	Seq  int // actually uint16
	Sent time.Time
}

// start creates and starts both the send and receive portions of the
// pinger, also populates the cancel function by creating a sub-ctx.
func (p *pinger) start(ctx context.Context, source netip.Addr) error {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.source = source
	socket, err := icmp.Listen(source)
	if err != nil {
		return fmt.Errorf("could not listen: %w", err)
	}
	p.socket = socket

	go p.sender(ctx)
	go p.receiver(ctx)

	return nil
}

func (p *pinger) remove(addr netip.Addr) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if _, ok := p.monitors[addr]; ok {
		delete(p.monitors, addr)
	}
}

func (p *pinger) sender(ctx context.Context) {
	timer := time.NewTimer(p.interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		// Reset the timer. This is when we pick up changes.
		timer.Reset(p.interval)

		targets := p.targets
		for _, t := range targets {
			for _, dest := range t.Addrs {
				if dest.Is4() != p.source.Is4() {
					continue
				}
				err := p.send(ctx, dest, t.Target)
				if err != nil {
					log.Printf("error sending packet: %v\n", err)
				}
			}
		}
	}
}

func (p *pinger) send(ctx context.Context, dest netip.Addr, t config.LatencyTarget) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	mon, ok := p.monitors[dest]
	if !ok {
		mon = &monitor{
			target: t,
			wire:   make([]outstandingPacket, 0, maxPendingPackets),
		}
		p.monitors[dest] = mon
	}

	p.sequence += 1
	echo := xicmp.Echo{
		ID:   0, // can't be set by us.
		Seq:  int(p.sequence),
		Data: []byte("github.com/VolatileDream"),
	}

	now := time.Now()
	if err := icmp.SendIcmpEcho(p.socket, &echo, dest); err != nil {
		return err
	}

	if len(mon.wire) >= maxPendingPackets {
		// Instead of removing one or two items, remove a quarter so that
		// we amortize the removal across multiple items.
		q := maxPendingPackets / 4
		mon.wire = append(mon.wire[:0], mon.wire[q:]...)
	}

	mon.wire = append(mon.wire, outstandingPacket{
		Seq:  int(p.sequence),
		Sent: now,
	})

	return nil
}

func (p *pinger) receiver(ctx context.Context) {
	// Receiver is responsible for closing the socket
	defer p.socket.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Keep extending the deadline to have an idle check.
		p.socket.SetReadDeadline(time.Now().Add(5 * time.Second))
		echo, err := icmp.ReadIcmpEcho(p.socket)

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

		if err := p.handleReceive(echo); err != nil {
			log.Printf("error handling received packet: %v", err)
		}
	}
}
func (p *pinger) handleReceive(echo *icmp.IcmpResponse) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	monitor, ok := p.monitors[echo.From]
	if !ok {
		// Should have been created on send.
		return fmt.Errorf("monitor not found for: %s", echo.From)
	}

	// Try to find the the number in the outstanding packet list.
	found := false
	for i, outstanding := range monitor.wire {
		if outstanding.Seq == echo.Echo.Seq {
			R := &PingResult{
				Sent:   outstanding.Sent,
				Recv:   echo.When,
				Src:    p.source,
				Dest:   echo.From,
				Target: monitor.target,
			}
			p.result <- R
			found = true
			monitor.wire = append(monitor.wire[:0], monitor.wire[i+1:]...)
			break
		}

		// missing packet...
		R := &PingResult{
			Sent:   outstanding.Sent,
			Src:    p.source,
			Dest:   echo.From,
			Target: monitor.target,
		}
		p.result <- R
	}

	if !found {
		// Not clear if we should drop the contents of wire here or not?
		// monitor.wire = monitor.wire[:0]
		log.Printf("did not find packet for %v seq: %d", echo.From, echo.Echo.Seq)
	}

	return nil
}
