package dpdknet

import (
	"net"
	"strconv"
)

// TCPAddr represents the address of a TCP end point.
type TCPAddr struct {
	IP   net.IP
	Port int
	Zone string // IPv6 scoped addressing zone
}

// Network returns the address's network name, "tcp".
func (a *TCPAddr) Network() string {
	return "tcp"
}

// String returns the string form of the address.
func (a *TCPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	ip := ipEmptyString(a.IP)
	if a.Zone != "" {
		return "[" + ip + "%" + a.Zone + "]:" + strconv.Itoa(a.Port)
	}
	return ip + ":" + strconv.Itoa(a.Port)
}

// UDPAddr represents the address of a UDP end point.
type UDPAddr struct {
	IP   net.IP
	Port int
	Zone string // IPv6 scoped addressing zone
}

// Network returns the address's network name, "udp".
func (a *UDPAddr) Network() string {
	return "udp"
}

// String returns the string form of the address.
func (a *UDPAddr) String() string {
	if a == nil {
		return "<nil>"
	}
	ip := ipEmptyString(a.IP)
	if a.Zone != "" {
		return "[" + ip + "%" + a.Zone + "]:" + strconv.Itoa(a.Port)
	}
	return ip + ":" + strconv.Itoa(a.Port)
}

// 辅助函数，用于格式化IP地址
func ipEmptyString(ip net.IP) string {
	if len(ip) == 0 {
		return ""
	}
	return ip.String()
}

// ResolveTCPAddr returns an address of TCP end point.
func ResolveTCPAddr(network, address string) (*TCPAddr, error) {
	addr, err := net.ResolveTCPAddr(network, address)
	if err != nil {
		return nil, err
	}
	return &TCPAddr{
		IP:   addr.IP,
		Port: addr.Port,
		Zone: addr.Zone,
	}, nil
}

// ResolveUDPAddr returns an address of UDP end point.
func ResolveUDPAddr(network, address string) (*UDPAddr, error) {
	addr, err := net.ResolveUDPAddr(network, address)
	if err != nil {
		return nil, err
	}
	return &UDPAddr{
		IP:   addr.IP,
		Port: addr.Port,
		Zone: addr.Zone,
	}, nil
}
