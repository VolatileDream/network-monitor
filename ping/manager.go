package ping

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"web/network-monitor/icmp"
)

type ProbeRequest struct {
	// Sending socket address.
	Source      netip.Addr
	Destination netip.Addr
}

type rpc struct {
	add      *ProbeRequest
	remove   *ProbeRequest
	list     bool
	response chan<- []ProbeRequest
}

// Manager manages the ping workers, and sockets required to monitor
// network latency.
type Manager struct {
	rpcs       chan rpc
	results    chan *PingResult
	interfaces map[netip.Addr]*monitorsByInterface
}

func NewManager(bufsz int) (*Manager, <-chan *PingResult) {
	m := &Manager{
		rpcs:       make(chan rpc),
		results:    make(chan *PingResult, bufsz),
		interfaces: make(map[netip.Addr]*monitorsByInterface),
	}
	return m, m.results
}

func (p *Manager) Add(ctx context.Context, pr ProbeRequest) {
	p.do(ctx, rpc{
		add: &pr,
	})
}
func (p *Manager) Remove(ctx context.Context, pr ProbeRequest) {
	p.do(ctx, rpc{
		remove: &pr,
	})
}
func (p *Manager) List(ctx context.Context) []ProbeRequest {
	return p.do(ctx, rpc{
		list: true,
	})
}
func (p *Manager) do(ctx context.Context, r rpc) []ProbeRequest {
	responseCh := make(chan []ProbeRequest)
	r.response = responseCh
	p.rpcs <- r

	select {
	case <-ctx.Done():
		return []ProbeRequest{}
	case resp := <-responseCh:
		return resp
	}
}
func (p *Manager) Run(ctx context.Context) error {
	for {
		var req rpc
		var resp []ProbeRequest
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req = <-p.rpcs:
		}

		if req.add != nil {
			p.add(ctx, *req.add)
		} else if req.remove != nil {
			p.remove(*req.remove)
		} else if req.list {
			resp = p.list()
		}

		req.response <- resp
	}
}

func (m *Manager) add(ctx context.Context, p ProbeRequest) error {
	mon, ok := m.interfaces[p.Source]
	if !ok {
		socket, err := icmp.Listen(p.Source)
		if err != nil {
			return fmt.Errorf("could not listen: %w", err)
		}
		// Setup and start the monitor.
		monitorCtx, monitorCancel := context.WithCancel(ctx)
		monitor := &monitorsByInterface{
			cancel:   monitorCancel,
			source:   p.Source,
			socket:   socket,
			result:   m.results,
			monitors: make(map[netip.Addr]*monitor),
			sequence: 1,
		}
		m.interfaces[p.Source] = monitor

		go pinger(monitorCtx, time.Second, monitor)
		go receiver(monitorCtx, monitor)
	}

	// Guaranteed now.
	mon = m.interfaces[p.Source]

	mon.lock.Lock()
	defer mon.lock.Unlock()

	if _, ok := mon.monitors[p.Destination]; ok {
		return fmt.Errorf("address already added: %s", p.Destination)
	}

	mon.monitors[p.Destination] = &monitor{
		dest: p.Destination,
		wire: nil,
	}

	return nil
}
func (m *Manager) remove(p ProbeRequest) {
	mon, ok := m.interfaces[p.Source]
	if !ok {
		return
	}

	mon.lock.Lock()
	defer mon.lock.Unlock()

	delete(mon.monitors, p.Destination)

	if len(mon.monitors) == 0 {
		// Receiver cleans up the socket.
		mon.cancel()
		delete(m.interfaces, p.Source)
	}
}
func (m *Manager) list() []ProbeRequest {
	// We're at least going to have at least 1 per interface.
	reqs := make([]ProbeRequest, 0, len(m.interfaces))
	for _, monitor := range m.interfaces {
		monitor.lock.Lock()
		for dest, _ := range monitor.monitors {
			reqs = append(reqs, ProbeRequest{
				Source:      monitor.source,
				Destination: dest,
			})
		}
		monitor.lock.Unlock()
	}

	return reqs
}
