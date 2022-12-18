package ping

import (
	"net/netip"
	"time"
)

type PingResult struct {
	Sent time.Time
	// optional time, recv is 0 when the packet was never received,
	// or returned out of order.
	Recv time.Time
	Src  netip.Addr
	Dest netip.Addr
}

// Elapsed returns a negative duration if PingResult.recv was zero.
func (pr *PingResult) Elapsed() time.Duration {
	if pr.Recv.IsZero() {
		return -1
	}
	return pr.Recv.Sub(pr.Sent)
}
