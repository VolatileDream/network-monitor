package trace

// Using the x/net/icmp package to implement traceroute is very annoying.
// The ip4 and ip6 types are completely disjoint and require implementing
// a common interface layer on top of them (probably why they are in x/).
// Additionally they require you to use the unexposed PacketConn types
// from x/net/ipv4 or x/net/ipv6, which are much more complicated.
//
// Instead of all that, aedan on github has laid out a very bare bones
// traceroute implementation that binds to sockets itself, ignoring the
// x/net/icmp package entirely.
//
// We go halvesies: using x/net/icmp to parse the messages, and taking a
// page from aedan [0] to implement simple traceroute. We try to use the
// net/ package to maybe provide cross platform support, to avoid raw
// socket manipulation ourselves. Additionally, we use net/netip.Addr
// because it is a better type than net.Addr.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"time"

	"web/network-monitor/icmp"

	xicmp "golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// "Traceroute", Wikipedia: https://en.wikipedia.org/wiki/Traceroute
	traceroutePort = 33434

	// https://www.iana.org/assignments/ip-parameters/ip-parameters.xml#ip-parameters-2
	DefaultTTL = 64

	defaultRetries = 3
	defaultTimeout = 5 * time.Second
)

var (
	errNotTtlPacket     = fmt.Errorf("not a ttl exceeded packet")
	errNotDstUnreachPkt = fmt.Errorf("not a destination unreachable packet")
)

type TraceRouteOptions struct {
	// MaxHops is the maximum distance from the current device that packets
	// should be sent to determine the route.
	// Default: 64
	MaxHops int
	// Number of retry packets to send if no icmp message is received.
	// Default: 3
	Retries int
	// Timeout for each hop attempt, global timeout set via context passed in.
	// Default: 5s
	HopTimeout time.Duration
	// Local IP interface to bind to, only used if Valid.
	Interface netip.Addr
}

type TraceResult struct {
	Source netip.Addr
	Dest   netip.Addr
	// Will not be Valid if the hop is unknown.
	Hops []netip.Addr
}

func TraceRoute(ctx context.Context, dest netip.Addr, opts TraceRouteOptions) (*TraceResult, error) {
	r := rand.New(rand.NewSource(time.Now().UnixMicro()))

	result := &TraceResult{
		Dest: dest,
		Hops: make([]netip.Addr, 0, DefaultTTL),
	}
	if opts.Interface.IsValid() {
		result.Source = opts.Interface
	} else if dest.Is4() {
		result.Source = netip.IPv4Unspecified()
	} else {
		result.Source = netip.IPv6Unspecified()
	}

	if !sameIpType(result.Source, result.Dest) {
		return nil, fmt.Errorf(
			"mismatched IPv type, both source and dest must be the same: %s, %s",
			result.Source,
			result.Dest)
	}

	icmpConn, err := icmp.ListenPrivileged(result.Source)
	defer icmpConn.Close()
	if err != nil {
		return nil, fmt.Errorf("could not bind privileged icmp port: %w", err)
	}

	// First hop is always the source.
	result.Hops = append(result.Hops, result.Source)

	udpConn, err := icmp.Listen(result.Source)
	defer udpConn.Close()
	if err != nil {
		return nil, fmt.Errorf("icmp socket listen failed: %w", err)
	}

	var portId int
	if addr, ok := udpConn.LocalAddr().(*net.UDPAddr); ok {
		portId = addr.Port
	} else {
		log.Printf("traceroute could not determine UDP port number, only detecting packets via random sequence number\n")
	}

	echo := xicmp.Echo{
		// Can't be set by us, but the UDP port is used by the kernel to populate it.
		// Setting it to that port ourselves makes it easier to reason about.
		ID:   portId,
		Seq:  r.Int() & 0xFFFF, // incremented later.
		Data: []byte("VolatileDream//web/network-monitor"),
		//Data: []byte("@@@@@@"),
	}

	tries := defaultRetries
	if opts.Retries > 0 {
		tries = opts.Retries
	}
	hopTimeout := defaultTimeout
	if opts.HopTimeout > 0 {
		hopTimeout = opts.HopTimeout
	}
	maxHops := DefaultTTL
	if opts.MaxHops > 0 {
		maxHops = opts.MaxHops
	}

trace_hops:
	for ttl := 1; ttl < maxHops; ttl++ {
		err = setTTL(udpConn, ttl)
		if err != nil {
			return nil, fmt.Errorf("failed to set ttl to %d: %w", ttl, err)
		}

		found := false
		attemptDeadline := time.Now().Add(time.Duration(tries) * hopTimeout)

		for attempt := 0; attempt < tries && !found && time.Now().Before(attemptDeadline); attempt++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			echo.Seq += 1
			//log.Printf("sending ID: %d, Seq: %d, ttl:%d\n", echo.ID, echo.Seq, ttl)
			err := icmp.SendIcmpEcho(udpConn, &echo, result.Dest)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return nil, fmt.Errorf("traceroute failed: %w", err)
				}
				// do something reasonable.
				//log.Printf("icmp send err: %+v\n", err)
				continue
			}

			hopDeadline := time.Now().Add(hopTimeout)
			icmpConn.SetReadDeadline(hopDeadline)

			for !found {
				// Continue to read packets until we hit the deadline.
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}

				addr, msg, err := icmp.ReadIcmp(icmpConn)
				if err != nil {
					// Most errors are probably timeouts.
					if !errors.Is(err, os.ErrDeadlineExceeded) {
						// do something reasonable...
						log.Printf("icmp read err: %+v\n", err)
					} else {
						//log.Printf("icmp read timeout: %+v\n", err)
					}
					break
				}

				// TODO: This packets we don't want. Filter other message types better.

				var parseFn func(*xicmp.Message) (*xicmp.Echo, error)

				if msg.Type == ipv4.ICMPTypeTimeExceeded || msg.Type == ipv6.ICMPTypeTimeExceeded {
					parseFn = parseInnerMsg
				} else if msg.Type == ipv4.ICMPTypeDestinationUnreachable || msg.Type == ipv6.ICMPTypeDestinationUnreachable {
					parseFn = parseInnerMsg

				} else if msg.Type == ipv4.ICMPTypeEchoReply || msg.Type == ipv6.ICMPTypeEchoReply {
					parseFn = parseEchoReply
				} else {
					log.Printf("unexpected icmp type %v: %#v\n", msg.Type, msg.Body)
					continue
				}

				recvMsg, err := parseFn(msg)
				if err != nil {
					// failed to parse ignore it.
					log.Printf("could not extract icmp echo from received packet: %w", err)
					continue
				}

				if echo.ID != recvMsg.ID || echo.Seq != recvMsg.Seq {
					// Packet not for us.
					//log.Printf("ignoring recv ID: %d, Seq: %d\n", recvMsg.ID, recvMsg.Seq)
					continue
				}

				//log.Printf("recv with match ID: %d, Seq: %d, from: %v\n", recvMsg.ID, recvMsg.Seq, addr)
				found = true
				result.Hops = append(result.Hops, addr)

				if msg.Type == ipv4.ICMPTypeEchoReply || msg.Type == ipv6.ICMPTypeEchoReply {
					break trace_hops
				}
			} // read loop
		} // write loop

		if !found {
			log.Printf("Hop %d not found...\n", ttl)
			result.Hops = append(result.Hops, netip.Addr{})
		}
	} // hop loop

	return result, nil
}

func ResolveHops(ctx context.Context, addrs []netip.Addr, addrTimeout time.Duration) ([][]string, error) {
	results := make([][]string, 0, len(addrs))
	for _, addr := range addrs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if !addr.IsValid() {
			results = append(results, nil)
			continue
		}

		hopCtx, cancel := context.WithTimeout(ctx, addrTimeout)
		s, err := net.DefaultResolver.LookupAddr(hopCtx, addr.String())
		cancel()

		if err != nil {
			log.Printf("name resolution failed: %v\n", err)
			results = append(results, nil)
		} else {
			results = append(results, s)
		}
	}
	return results, nil
}

func sameIpType(one, two netip.Addr) bool {
	return one.Is4() == two.Is4() || one.Is4In6() == two.Is4In6() || one.Is6() == two.Is6()
}

func setTTL(conn *xicmp.PacketConn, ttl int) error {
	if p := conn.IPv4PacketConn(); p != nil {
		return p.SetTTL(ttl)
	} else if p := conn.IPv6PacketConn(); p != nil {
		return p.SetHopLimit(ttl)
	}
	return fmt.Errorf("unknown connection type: %+v", conn)
}

func parseInnerMsg(m *xicmp.Message) (*xicmp.Echo, error) {
	var data []byte
	if m.Type == ipv4.ICMPTypeTimeExceeded || m.Type == ipv6.ICMPTypeTimeExceeded {
		te, ok := m.Body.(*xicmp.TimeExceeded)
		if !ok {
			return nil, errNotTtlPacket
		}
		data = te.Data
	} else if m.Type == ipv4.ICMPTypeDestinationUnreachable || m.Type == ipv6.ICMPTypeDestinationUnreachable {
		du, ok := m.Body.(*xicmp.DstUnreach)
		if !ok {
			return nil, errNotDstUnreachPkt
		}
		data = du.Data
	}

	var protocol int
	var offset int

	// Handle ipv4
	switch m.Type.(type) {
	case ipv4.ICMPType:
		h, err := ipv4.ParseHeader(data)
		if err != nil {
			return nil, fmt.Errorf("no ip4 header: %w", err)
		}

		protocol = 1
		offset = h.Len + len(h.Options)

	case ipv6.ICMPType:
		// Handle ipv6
		protocol = 58
		offset = ipv6.HeaderLen
	}

	// This message is TRUNCATED.
	prevMsg, err := xicmp.ParseMessage(protocol, data[offset:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse contents: %w", err)
	}

	if prevMsg.Type != ipv4.ICMPTypeEcho && prevMsg.Type != ipv6.ICMPTypeEchoRequest {
		return nil, fmt.Errorf("contents not icmp echo")
	}

	return prevMsg.Body.(*xicmp.Echo), nil
}

func parseEchoReply(m *xicmp.Message) (*xicmp.Echo, error) {
	return m.Body.(*xicmp.Echo), nil
}
