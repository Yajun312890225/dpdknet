package dpdknet

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/flow"
	"github.com/Yajun312890225/nff-go/packet"
)

type UDPConn struct {
	localAddr  *UDPAddr
	remoteAddr *UDPAddr
	rxFlow     *flow.Flow
	txPort     uint16
	recvCh     chan []byte
	mu         sync.Mutex
	closed     bool
}

func ListenUDP(network string, laddr *UDPAddr) (*UDPConn, error) {
	if err := Init(); err != nil {
		return nil, err
	}

	// DPDK使用物理端口ID，通常从0开始，而不是UDP端口号
	dpdkPort := uint16(0) // 默认使用第一个DPDK端口

	rxFlow, err := flow.SetReceiver(dpdkPort)
	if err != nil {
		return nil, err
	}

	c := &UDPConn{
		localAddr: laddr,
		rxFlow:    rxFlow,
		txPort:    dpdkPort, // 这里存储的是DPDK端口ID，不是UDP端口号
		recvCh:    make(chan []byte, 4096),
	}

	flow.SetHandler(rxFlow, func(pkt *packet.Packet, ctx flow.UserContext) {
		data := pkt.GetRawPacketBytes()
		buf := make([]byte, len(data))
		copy(buf, data)
		c.recvCh <- buf
	}, nil)

	return c, nil
}

func (c *UDPConn) ReadFromUDP(buf []byte) (int, *UDPAddr, error) {
	if c.closed {
		return 0, nil, errors.New("connection closed")
	}
	data := <-c.recvCh
	ipStart := 14
	srcIP := net.IPv4(data[ipStart+12], data[ipStart+13], data[ipStart+14], data[ipStart+15])
	udpStart := ipStart + int((data[ipStart]&0x0F)*4)
	srcPort := int(binary.BigEndian.Uint16(data[udpStart : udpStart+2]))
	n := copy(buf, data[udpStart+8:])
	addr := &UDPAddr{IP: srcIP, Port: srcPort}
	return n, addr, nil
}

// ReadFrom reads a packet from the connection, copying the payload into buf.
// It implements the PacketConn ReadFrom method.
func (c *UDPConn) ReadFrom(buf []byte) (int, net.Addr, error) {
	n, addr, err := c.ReadFromUDP(buf)
	return n, addr, err
}

// Read reads data from the connection.
func (c *UDPConn) Read(buf []byte) (int, error) {
	n, _, err := c.ReadFromUDP(buf)
	return n, err
}

// WriteTo writes a packet with payload buf to addr.
func (c *UDPConn) WriteTo(buf []byte, addr net.Addr) (int, error) {
	udpAddr, ok := addr.(*UDPAddr)
	if !ok {
		return 0, errors.New("invalid address type")
	}
	return c.WriteToUDP(buf, udpAddr)
}

// WriteToUDP writes a UDP packet to addr.
func (c *UDPConn) WriteToUDP(buf []byte, addr *UDPAddr) (int, error) {
	if c.closed {
		return 0, errors.New("connection closed")
	}

	// TODO: 实现实际的UDP包发送
	// 目前是简化实现
	return len(buf), nil
}

// Write writes data to the connection.
func (c *UDPConn) Write(buf []byte) (int, error) {
	if c.remoteAddr == nil {
		return 0, errors.New("write to unconnected UDP connection")
	}
	return c.WriteToUDP(buf, c.remoteAddr)
}

// LocalAddr returns the local network address.
func (c *UDPConn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote network address.
func (c *UDPConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *UDPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.recvCh)
	return nil
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *UDPConn) SetDeadline(t time.Time) error {
	// TODO: 实现超时逻辑
	return nil
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *UDPConn) SetReadDeadline(t time.Time) error {
	// TODO: 实现读超时逻辑
	return nil
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *UDPConn) SetWriteDeadline(t time.Time) error {
	// TODO: 实现写超时逻辑
	return nil
}
