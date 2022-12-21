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
	commonMaximumTransmissionUnit = 1500
)

// ListenIcmp creates a packet connection to send and receive ICMP messages.
// This *should* work without privileged access, but will only receive ICMP
// Echo messages. That is: can be used to ping a host, but not much more.
func Listen(ip netip.Addr) (*xicmp.PacketConn, error) {
	return listen(ip, udpCfg)
}

// ListenPrivileged requires privileged access on the system (eg: root or
// CAP_NET_BIND_SERVICE on linux). But with this access is capable of sending
// and receiving more types of icmp messages, ex: this will receive TTL Exceeded.
func ListenPrivileged(ip netip.Addr) (*xicmp.PacketConn, error) {
	return listen(ip, icmpCfg)
}

type bindCfg struct {
	ip4 string
	ip6 string
}

var (
	icmpCfg = bindCfg{
		ip4: "ip4:icmp",
		ip6: "ip6:ipv6-icmp",
	}
	udpCfg = bindCfg{
		ip4: "udp4",
		ip6: "udp6",
	}
)

func listen(ip netip.Addr, cfg bindCfg) (*xicmp.PacketConn, error) {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}

	addr := ip.String()
	proto := cfg.ip6
	if ip.Is4() {
		proto = cfg.ip4
	}
	return xicmp.ListenPacket(proto, addr)
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

	_, err = i.WriteTo(b, &net.UDPAddr{
		IP: addr.AsSlice(),
		//Port: traceroutePort,
	})
	return err
}

type IcmpResponse struct {
	From netip.Addr
	Echo *xicmp.Echo
	When time.Time
}

func ReadIcmp(conn *xicmp.PacketConn) (netip.Addr, *xicmp.Message, error) {
	recv := make([]byte, commonMaximumTransmissionUnit)
	c, addr, err := conn.ReadFrom(recv)
	recv = recv[:c]

	if err != nil {
		return netip.Addr{}, nil, err
	}

	var recvAddr netip.Addr
	if origin, err := netip.ParseAddrPort(addr.String()); err == nil {
		recvAddr = origin.Addr()
	} else if origin, err := netip.ParseAddr(addr.String()); err == nil {
		recvAddr = origin
	} else {
		return netip.Addr{}, nil, fmt.Errorf("failed to parse into ip address: %s", addr.String())
	}

	proto := 1 // Icmp4 number.
	// This comparison doesn't work the other way, because an ipv4
	// address can always be embedded in an ipv6 address.
	if !connIsIPv4(conn) {
		proto = 58 // Icmp6 number.
	}
	msg, err := xicmp.ParseMessage(proto, recv)
	if err != nil {
		return netip.Addr{}, nil, fmt.Errorf("bad icmp packet: %w", err)
	}

	return recvAddr, msg, nil
}

func ReadIcmpEcho(conn *xicmp.PacketConn) (*IcmpResponse, error) {
	recv := make([]byte, commonMaximumTransmissionUnit)
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
	if !connIsIPv4(conn) {
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

func connIsIPv4(c *xicmp.PacketConn) bool {
	return c.IPv4PacketConn() != nil
	//return netip.MustParseAddrPort(conn.LocalAddr().String()).Addr().Is4()
}
