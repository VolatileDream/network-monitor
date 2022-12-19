package icmp

// Functions to interface with icmp without caring if the netip.Addr is 4 or 6.
import (
	"fmt"
	"net"
	"net/netip"
	"time"

	xicmp "golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// Derived from the common MTU for IP networks.
	// Packets larger than this get fragmented.
	icmpMaxPacketSize = 1500

	// https://www.iana.org/assignments/ip-parameters/ip-parameters.xml#ip-parameters-2
	DefaultTTL = 64
)

func ListenIcmp(ip netip.Addr) (*xicmp.PacketConn, error) {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}

	if ip.Is4() {
		return xicmp.ListenPacket("udp4", ip.String())
	} else {
		return xicmp.ListenPacket("udp6", ip.String())
	}
}

func GetTTL(i *xicmp.PacketConn) int {
	var ttl int
	if v4 := i.IPv4PacketConn(); v4 != nil {
		ttl, _ = v4.TTL()
	} else if v6 := i.IPv6PacketConn(); v6 != nil {
		ttl, _ = v6.HopLimit()
	}
	return ttl
}

func SetTTL(i *xicmp.PacketConn, ttl int) error {
	if v4 := i.IPv4PacketConn(); v4 != nil {
		v4.SetTTL(ttl)
	} else if v6 := i.IPv6PacketConn(); v6 != nil {
		v6.SetHopLimit(ttl)
	}
	return fmt.Errorf("icmp connection not IP v4 or v6")
}

func SendIcmpEcho(i *xicmp.PacketConn, e *xicmp.Echo, addr netip.Addr) error {
	m := xicmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: e,
	}

	if addr.Is6() {
		m.Type = ipv6.ICMPTypeEchoRequest
	}

	b, err := m.Marshal(nil)
	if err != nil {
		return fmt.Errorf("could not marshal packet: %w", err)
	}

	_, err = i.WriteTo(b, &net.UDPAddr{IP: addr.AsSlice()})
	return err
}

type IcmpResponse struct {
	From netip.Addr
	Echo *xicmp.Echo
	When time.Time
}

func ReadIcmp(conn *xicmp.PacketConn) (netip.Addr, *xicmp.Message, error) {
	recv := make([]byte, icmpMaxPacketSize)
	c, addr, err := conn.ReadFrom(recv)
	recv = recv[:c]

	if err != nil {
		return netip.Addr{}, nil, err
	}

	origin, err := netip.ParseAddrPort(addr.String())
	if err != nil {
		return netip.Addr{}, nil, fmt.Errorf("unable to parse packet source %s: %w", addr.String(), err)
	}

	proto := 1 // Icmp4 number.
	// This comparison doesn't work the other way, because an ipv4
	// address can always be embedded in an ipv6 address.
	if netip.MustParseAddrPort(conn.LocalAddr().String()).Addr().Is6() {
		proto = 58 // Icmp6 number.
	}
	msg, err := xicmp.ParseMessage(proto, recv)
	if err != nil {
		return netip.Addr{}, nil, fmt.Errorf("bad icmp packet: %w", err)
	}

	return origin.Addr(), msg, nil
}

func ReadIcmpEcho(conn *xicmp.PacketConn) (*IcmpResponse, error) {
	recv := make([]byte, icmpMaxPacketSize)
	c, addr, err := conn.ReadFrom(recv)
	now := time.Now()
	recv = recv[:c]

	if err != nil {
		return nil, err
	}
	resp := &IcmpResponse{
		When: now,
	}
	nip, err := netip.ParseAddrPort(addr.String())
	if err == nil {
		resp.From = nip.Addr()
	} else {
		return nil, fmt.Errorf("unable to parse packet source %s: %w", addr.String(), err)
	}

	proto := 1 // Icmp4 number.
	// This comparison doesn't work the other way, because an ipv4
	// address can always be embedded in an ipv6 address.
	if netip.MustParseAddrPort(conn.LocalAddr().String()).Addr().Is6() {
		proto = 58 // Icmp6 number.
	}
	msg, err := xicmp.ParseMessage(proto, recv)
	if err != nil {
		return nil, fmt.Errorf("bad icmp packet: %w", err)
	}

	if msg.Type != ipv4.ICMPTypeEchoReply && msg.Type != ipv6.ICMPTypeEchoReply {
		return nil, fmt.Errorf("packet type not echo: %d", msg.Type)
	}

	echo, ok := msg.Body.(*xicmp.Echo)
	if !ok {
		return nil, fmt.Errorf("packet type not *icmp.Echo: %v", msg)
	}

	resp.Echo = echo
	return resp, nil
}
