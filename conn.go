package dpdknet

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"

	"github.com/Yajun312890225/nff-go/flow"
	"github.com/Yajun312890225/nff-go/packet"
)

type UDPAddr struct {
	IP   net.IP
	Port int
}

type UDPConn struct {
	rxFlow *flow.Flow
	txPort uint16
	recvCh chan []byte
	mu     sync.Mutex
	closed bool
}

func ListenUDP(port uint16) (*UDPConn, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	rxFlow, err := flow.SetReceiver(port)
	if err != nil {
		return nil, err
	}
	c := &UDPConn{
		rxFlow: rxFlow,
		txPort: port,
		recvCh: make(chan []byte, 4096),
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
	return n, &UDPAddr{IP: srcIP, Port: srcPort}, nil
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
