package resolve

import (
	"context"
	"net"
	"net/netip"

	"web/network-monitor/config"
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
	// TODO: move the content of LatencyTarget into this package.
	addrs, err := t.Resolve(ctx, r.resolver)
	return addrs, err
}
