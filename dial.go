package dpdknet

import (
	"errors"
	"net"
	"time"
)

// Dial connects to the address on the named network.
func Dial(network, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
		addr, err := ResolveTCPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return DialTCP(network, nil, addr)
	case "udp", "udp4", "udp6":
		addr, err := ResolveUDPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return DialUDP(network, nil, addr)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// DialTimeout acts like Dial but takes a timeout.
func DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	// TODO: Implement timeout logic
	return Dial(network, address)
}

// DialUDP acts like Dial for UDP networks.
func DialUDP(network string, laddr, raddr *UDPAddr) (*UDPConn, error) {
	conn, err := ListenUDP(network, laddr)
	if err != nil {
		return nil, err
	}
	conn.remoteAddr = raddr
	return conn, nil
}
