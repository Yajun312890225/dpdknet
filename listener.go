package dpdknet

import (
	"errors"
	"net"
)

// Listen announces on the local network address with Option 风格。
func Listen(network, address string, opts ...Option) (net.Listener, error) {
	o := setDefaultOption()
	for _, opt := range opts {
		opt(o)
	}
	useVXLAN := o.useVXLAN

	switch network {
	case "tcp", "tcp4", "tcp6":
		addr, err := ResolveTCPAddr(network, address)
		if err != nil {
			return nil, err
		}
		if useVXLAN {
			return ListenTCPWithVXLAN(network, addr)
		}
		return ListenTCP(network, addr)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// ListenPacket announces on the local network address with Option 风格。
func ListenPacket(network, address string, opts ...Option) (net.PacketConn, error) {
	o := setDefaultOption()
	for _, opt := range opts {
		opt(o)
	}
	useVXLAN := o.useVXLAN

	switch network {
	case "udp", "udp4", "udp6":
		addr, err := ResolveUDPAddr(network, address)
		if err != nil {
			return nil, err
		}
		if useVXLAN {
			return ListenUDPWithVXLAN(network, addr)
		}
		return ListenUDP(network, addr)
	case "icmp", "icmp4", "icmp6":
		addr, err := net.ResolveIPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return NewICMPListenerWithVXLAN(addr)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}
