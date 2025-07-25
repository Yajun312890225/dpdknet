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
	vxlanConfig := o.vxlanConfig

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

// ListenWithVXLAN 辅助方法，显式传递 VXLAN 配置。
func ListenWithVXLAN(network, address string, vxlanConfig *VXLANConfig) (net.Listener, error) {
	return Listen(network, address, WithVXLAN(vxlanConfig))
}

// ListenPacket announces on the local network address with Option 风格。
func ListenPacket(network, address string, opts ...Option) (net.PacketConn, error) {
	o := setDefaultOption()
	for _, opt := range opts {
		opt(o)
	}
	vxlanConfig := o.vxlanConfig

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

// ListenPacketWithVXLAN 辅助方法，显式传递 VXLAN 配置。
func ListenPacketWithVXLAN(network, address string, vxlanConfig *VXLANConfig) (net.PacketConn, error) {
	return ListenPacket(network, address, WithVXLAN(vxlanConfig))
}
