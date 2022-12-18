package ping

// Functions to interface with icmp without caring if the netip.Addr is 4 or 6.
import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// Derived from the common MTU for IP networks.
	// Packets larger than this get fragmented.
	icmpMaxPacketSize = 1500
)

func ListenIcmp(ip netip.Addr) (*icmp.PacketConn, error) {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}

	if ip.Is4() {
		return icmp.ListenPacket("udp4", ip.String())
	} else {
		return icmp.ListenPacket("udp6", ip.String())
	}
}

func SendIcmpEcho(i *icmp.PacketConn, e *icmp.Echo, addr netip.Addr) error {
	m := icmp.Message{
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
	from netip.Addr
	echo *icmp.Echo
	when time.Time
}

func ReadIcmpEcho(src netip.Addr, conn *icmp.PacketConn) (*IcmpResponse, error) {
	recv := make([]byte, icmpMaxPacketSize)
	c, addr, err := conn.ReadFrom(recv)
	now := time.Now()
	recv = recv[:c]

	if err != nil {
		return nil, err
	}
	resp := &IcmpResponse{
		when: now,
	}
	nip, err := netip.ParseAddrPort(addr.String())
	if err == nil {
		resp.from = nip.Addr()
	} else {
		return nil, fmt.Errorf("unable to parse packet source %s: %w", addr.String(), err)
	}

	proto := 1 // Icmp4 number.
	if src.Is6() {
		proto = 58 // Icmp6 number.
	}
	msg, err := icmp.ParseMessage(proto, recv)
	if err != nil {
		return nil, fmt.Errorf("bad icmp packet: %w", err)
	}

	if msg.Type != ipv4.ICMPTypeEchoReply && msg.Type != ipv6.ICMPTypeEchoReply {
		return nil, fmt.Errorf("packet type not echo: %d", msg.Type)
	}

	echo, ok := msg.Body.(*icmp.Echo)
	if !ok {
		return nil, fmt.Errorf("packet type not *icmp.Echo: %v", msg)
	}

	resp.echo = echo
	return resp, nil
}
