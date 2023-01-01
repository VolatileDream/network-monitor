package resolve

import (
	"flag"
	"net/netip"
)

var (
	ipv4Flag = flag.Bool("allow-ip4", true, "Resolver returns ipv4 addresses, disable to filter them out.")
	ipv6Flag = flag.Bool("allow-ip6", true, "Resolver returns ipv6 addresses, disable to filter them out.")
)

func AllowedAddr(a netip.Addr) bool {
	return (a.Is6() && *ipv6Flag) || (a.Is4() && *ipv4Flag)
}
