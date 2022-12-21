package ip

import (
	"fmt"
	"net"
	"net/netip"
)

func Convert(addr net.Addr) (netip.Addr, error) {
	if origin, err := netip.ParseAddrPort(addr.String()); err == nil {
		return origin.Addr(), nil
	} else if origin, err := netip.ParseAddr(addr.String()); err == nil {
		return origin, nil
	} else {
		return netip.Addr{}, fmt.Errorf("failed to convert address: %s", addr.String())
	}
}
