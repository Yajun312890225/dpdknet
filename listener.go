package dpdknet

import (
	"errors"
	"net"
)

// Listen announces on the local network address.
func Listen(network, address string, options ...*VXLANConfig) (net.Listener, error) {
	var vxlanConfig *VXLANConfig
	if len(options) > 0 {
		vxlanConfig = options[0]
	}

	switch network {
	case "tcp", "tcp4", "tcp6":
		addr, err := ResolveTCPAddr(network, address)
		if err != nil {
			return nil, err
		}
		if vxlanConfig != nil {
			return ListenTCPWithVXLAN(network, addr, vxlanConfig)
		}
		return ListenTCP(network, addr)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// ListenWithOptions announces on the local network address with options.
func ListenWithOptions(network, address string, vxlanConfig *VXLANConfig) (net.Listener, error) {
	return Listen(network, address, vxlanConfig)
}

// ListenPacket announces on the local network address.
func ListenPacket(network, address string, options ...*VXLANConfig) (net.PacketConn, error) {
	var vxlanConfig *VXLANConfig
	if len(options) > 0 {
		vxlanConfig = options[0]
	}

	switch network {
	case "udp", "udp4", "udp6":
		addr, err := ResolveUDPAddr(network, address)
		if err != nil {
			return nil, err
		}
		if vxlanConfig != nil {
			return ListenUDPWithVXLAN(network, addr, vxlanConfig)
		}
		return ListenUDP(network, addr)
	case "icmp", "icmp4", "icmp6":
		addr, err := net.ResolveIPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return NewICMPListenerWithVXLAN(addr, vxlanConfig)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// ListenPacketWithOptions announces on the local network address with options.
func ListenPacketWithOptions(network, address string, vxlanConfig *VXLANConfig) (net.PacketConn, error) {
	return ListenPacket(network, address, vxlanConfig)
}
