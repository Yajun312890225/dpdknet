package dpdknet

import (
	"errors"
	"net"
)

// Listen announces on the local network address.
func Listen(network, address string) (net.Listener, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
		addr, err := ResolveTCPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return ListenTCP(network, addr)
	case "vxlan":
		addr, err := ParseVXLANAddr(address)
		if err != nil {
			return nil, err
		}
		return ListenVXLAN(network, addr)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// ListenPacket announces on the local network address.
func ListenPacket(network, address string) (net.PacketConn, error) {
	switch network {
	case "udp", "udp4", "udp6":
		addr, err := ResolveUDPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return ListenUDP(network, addr)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}
