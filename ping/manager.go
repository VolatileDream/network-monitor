package ping

import (
	"context"
	"log"
	"net/netip"

	"github.com/VolatileDream/workbench/web/network-monitor/config"
	"github.com/VolatileDream/workbench/web/network-monitor/resolve"
)

type ProbeRequest struct {
	// Sending socket address.
	Source      netip.Addr
	Destination netip.Addr
}

// Manager manages the ping workers, and sockets required to monitor
// network latency.
type Manager struct {
	pingerV4 *pinger
	pingerV6 *pinger

	configCh  <-chan config.Config
	resolveCh <-chan resolve.Result
	results   chan *PingResult

	// Targets that resolved without error.
	targets []resolve.Resolution
}

func NewManager(bufsz int, configCh <-chan config.Config, resolveCh <-chan resolve.Result) (*Manager, <-chan *PingResult) {
	m := &Manager{
		configCh:  configCh,
		resolveCh: resolveCh,
		results:   make(chan *PingResult, bufsz),
	}
	return m, m.results
}

func (m *Manager) Run(ctx context.Context) error {
	{
		// Wait for a config & resolution.
		c := <-m.configCh
		r := <-m.resolveCh
		m.initPinger(ctx, c, r)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case c := <-m.configCh:
			m.updateConfig(c)

		case r := <-m.resolveCh:
			m.updateTargets(r)
		}
	}
}

func (m *Manager) updateConfig(c config.Config) {
	m.pingerV4.interval = c.PingInterval
	m.pingerV6.interval = c.PingInterval
}

func (m *Manager) updateTargets(r resolve.Result) {
	newAddrs := make(map[netip.Addr]struct{})
	targets := make([]resolve.Resolution, 0, len(r.Resolved))
	for _, resolution := range r.Resolved {
		targets = append(targets, resolution)
		for _, ip := range resolution.Addrs {
			newAddrs[ip] = struct{}{}
		}
	}

	// Update the ping targets before we compute stats.
	prev := m.targets
	m.targets = targets

	addrs := make(map[netip.Addr]struct{})
	for _, resolution := range prev {
		for _, ip := range resolution.Addrs {
			addrs[ip] = struct{}{}
		}
	}

	add := 0
	for ip, _ := range newAddrs {
		if _, ok := addrs[ip]; !ok {
			add += 1
		}
	}

	remove := 0
	for ip, _ := range addrs {
		if _, ok := newAddrs[ip]; !ok {
			remove += 1
		}
		m.pingerV4.remove(ip)
		m.pingerV6.remove(ip)
	}

	m.pingerV4.targets = targets
	m.pingerV6.targets = targets

	log.Printf("updated %d probe endpoints\n", remove+add)
}

func (m *Manager) initPinger(ctx context.Context, c config.Config, r resolve.Result) {
	m.pingerV4 = &pinger{
		result:   m.results,
		monitors: make(map[netip.Addr]*monitor),
	}
	m.pingerV6 = &pinger{
		result:   m.results,
		monitors: make(map[netip.Addr]*monitor),
	}
	m.updateConfig(c)
	m.updateTargets(r)

	if err := m.pingerV4.start(ctx, netip.IPv4Unspecified()); err != nil {
		log.Printf("failed to start ipv4 pinger: %v", err)
	}
	if err := m.pingerV6.start(ctx, netip.IPv6Unspecified()); err != nil {
		log.Printf("failed to start ipv6 pinger: %v", err)
	}
}
