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
	dest = dest.Unmap() // remove 4in6 weirdness
	result := &TraceResult{
		Dest: dest,
		Hops: make([]netip.Addr, 0, DefaultTTL),
	}
	if opts.Interface.IsValid() {
		result.Source = opts.Interface.Unmap()
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
	if err != nil {
		return nil, fmt.Errorf("could not bind privileged icmp port: %w", err)
	}

	udpConn, err := icmp.Listen(result.Source)
	defer udpConn.Close()
	if err != nil {
		return nil, fmt.Errorf("icmp socket listen failed: %w", err)
	}

	echo := xicmp.Echo{
		ID:   0, // can't be set.
		Seq:  0, // incremented later.
		Data: []byte("VolatileDream//web/network-monitor"),
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
		for attempt := 0; attempt < tries && !found; attempt++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			echo.Seq += 1
			err := icmp.SendIcmpEcho(udpConn, &echo, result.Dest)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return nil, fmt.Errorf("traceroute failed: %w", err)
				}
				// do something reasonable.
				log.Printf("icmp send err: %+v\n", err)
				continue
			}

			icmpConn.SetReadDeadline(time.Now().Add(hopTimeout))
			addr, msg, err := icmp.ReadIcmp(icmpConn)
			if err != nil {
				// Most errors are probably timeouts.
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					// do something reasonable...
					log.Printf("icmp read err: %+v\n", err)
				}
				continue
			}

			found = true

			result.Hops = append(result.Hops, addr)

			if msg.Type == ipv4.ICMPTypeEchoReply || msg.Type == ipv6.ICMPTypeEchoReply {
				break trace_hops
			} else {
				break
			}
		}

		if !found {
			result.Hops = append(result.Hops, netip.Addr{})
		}
	}

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
