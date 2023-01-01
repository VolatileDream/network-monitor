package resolve

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"web/network-monitor/config"
	"web/network-monitor/trace"
)

type Resolver interface {
	Resolve(context.Context, config.LatencyTarget) ([]netip.Addr, error)
}

type netresolver struct {
	// Resolver to use
	resolver *net.Resolver
}

var _ Resolver = &netresolver{}

func DefaultResolver() Resolver {
	return NewResolver(net.DefaultResolver)
}

func NewResolver(resolver *net.Resolver) Resolver {
	return &netresolver{
		resolver: resolver,
	}
}

func (r *netresolver) Resolve(ctx context.Context, t config.LatencyTarget) ([]netip.Addr, error) {
	switch t.(type) {
	case *config.TraceHops:
		return r.resolveHops(ctx, t.(*config.TraceHops))
	case *config.HostnameTarget:
		return r.resolveHost(ctx, t.(*config.HostnameTarget))
	case *config.StaticIP:
		s := t.(*config.StaticIP)
		return []netip.Addr{s.IP}, nil
	}
	return nil, fmt.Errorf("could not resolve target of type %v\n", t)
}

func (r *netresolver) resolveHops(ctx context.Context, th *config.TraceHops) ([]netip.Addr, error) {
	res, err := trace.TraceRoute(ctx, th.Dest, trace.TraceRouteOptions{
		MaxHops:    th.Hop + 1,
		Retries:    5,
		HopTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	index := th.Hop
	if index < 0 {
		index += len(res.Hops)
	}
	// If the index is outside the range of reasonable, then it's an exception.
	// Since it's not possible to know the number of hops without having run a
	// trace route out of band, this likely constrains passed indexes to the
	// range between -2 and 2.
	if index < 0 || len(res.Hops) <= index {
		return nil, fmt.Errorf("traceroute has less than %d hops", th.Hop)
	}

	return []netip.Addr{
		res.Hops[index].Unmap(),
	}, nil
}

func (r *netresolver) resolveHost(ctx context.Context, s *config.HostnameTarget) ([]netip.Addr, error) {
	addrs, err := r.resolver.LookupNetIP(ctx, "ip", s.Host)
	return addrs, err
}

func noMixIp(addrs []netip.Addr) []netip.Addr {
	if len(addrs) == 0 {
		return addrs
	}

	mixed := false
	for _, addr := range addrs {
		if addr.Is4In6() {
			mixed = true
			break
		}
	}
	if !mixed {
		return addrs
	}

	unmix := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if addr.Is4In6() {
			unmix = append(unmix, addr.Unmap())
		} else {
			unmix = append(unmix, addr)
		}
	}

	return unmix
}
