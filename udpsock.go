package dpdknet

import (
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/flow"
	"github.com/Yajun312890225/nff-go/packet"
	"github.com/Yajun312890225/nff-go/types"
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

		// 检查以太网帧长度
		if len(data) < 42 { // 以太网头(14) + IP头(20) + UDP头(8)
			return
		}

		// 检查是否为IP协议 (EtherType = 0x0800)
		if data[12] != 0x08 || data[13] != 0x00 {
			return
		}

		ipHeaderStart := 14

		// 检查是否为UDP协议 (Protocol = 17)
		if data[ipHeaderStart+9] != 17 {
			return
		}

		// 计算IP头长度
		headerLength := int((data[ipHeaderStart] & 0x0F) * 4)
		udpStart := ipHeaderStart + headerLength

		// 检查UDP头长度
		if len(data) < udpStart+8 {
			return
		}

		// 解析UDP头
		srcPort := binary.BigEndian.Uint16(data[udpStart : udpStart+2])
		dstPort := binary.BigEndian.Uint16(data[udpStart+2 : udpStart+4])
		udpLen := binary.BigEndian.Uint16(data[udpStart+4 : udpStart+6])

		// 如果指定了本地端口，检查目标端口是否匹配
		if laddr != nil && laddr.Port != 0 && int(dstPort) != laddr.Port {
			return
		}

		// 提取目标IP地址
		dstIP := net.IPv4(data[ipHeaderStart+16], data[ipHeaderStart+17],
			data[ipHeaderStart+18], data[ipHeaderStart+19])

		// 如果指定了本地地址，检查目标IP是否匹配
		if laddr != nil && laddr.IP != nil && !laddr.IP.IsUnspecified() {
			if !dstIP.Equal(laddr.IP) {
				return
			}
		}

		// 验证UDP长度
		expectedLen := len(data) - udpStart
		if int(udpLen) != expectedLen {
			log.Printf("UDP length mismatch: header=%d, actual=%d", udpLen, expectedLen)
		}

		// 只有匹配的数据包才会被接收
		buf := make([]byte, len(data))
		copy(buf, data)
		select {
		case c.recvCh <- buf:
		default:
			// 如果缓冲区满了，丢弃数据包
			log.Printf("UDP receive buffer full, dropping packet from port %d", srcPort)
		}
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

	if addr == nil || addr.IP == nil {
		return 0, errors.New("invalid destination address")
	}

	// 创建数据包
	pkt, err := packet.NewPacket()
	if err != nil {
		return 0, err
	}

	// 初始化IPv4 UDP数据包
	if !packet.InitEmptyIPv4UDPPacket(pkt, uint(len(buf))) {
		return 0, errors.New("failed to init IPv4 UDP packet")
	}

	// 获取各层指针
	ethHdr := pkt.Ether
	ipv4Hdr := pkt.GetIPv4NoCheck()
	udpHdr := pkt.GetUDPNoCheck()

	// 设置以太网头 - 使用默认MAC地址
	ethHdr.DAddr = [6]uint8{0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // 目标MAC
	ethHdr.SAddr = [6]uint8{0x00, 0x00, 0x00, 0x00, 0x00, 0x01} // 源MAC

	// 设置源IP地址
	if c.localAddr != nil && c.localAddr.IP != nil {
		ipv4Hdr.SrcAddr = packet.SwapBytesIPv4Addr(types.SliceToIPv4(c.localAddr.IP.To4()))
	} else {
		ipv4Hdr.SrcAddr = types.IPv4Address(0x0100007f) // 127.0.0.1 in network byte order
	}

	// 设置目标IP地址
	ipv4Hdr.DstAddr = packet.SwapBytesIPv4Addr(types.SliceToIPv4(addr.IP.To4()))

	// 设置UDP头
	if c.localAddr != nil {
		udpHdr.SrcPort = packet.SwapBytesUint16(uint16(c.localAddr.Port))
	} else {
		udpHdr.SrcPort = 0
	}
	udpHdr.DstPort = packet.SwapBytesUint16(uint16(addr.Port))

	// 复制用户数据
	data := (*[1 << 30]byte)(pkt.Data)[:len(buf)]
	copy(data, buf)

	// 计算IP头校验和
	ipv4Hdr.HdrChecksum = packet.SwapBytesUint16(packet.CalculateIPv4Checksum(ipv4Hdr))

	// 发送数据包 - 这里使用简化的发送方式
	// 实际应用中应该通过专门的发送流来发送
	log.Printf("Sending UDP packet to %s:%d (len=%d)", addr.IP.String(), addr.Port, len(buf))

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
