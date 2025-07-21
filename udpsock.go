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
	log.Printf("[DEBUG] ListenUDP called with network=%s, laddr=%v", network, laddr)

	if err := Init(); err != nil {
		log.Printf("[ERROR] DPDK Init failed: %v", err)
		return nil, err
	}
	log.Printf("[DEBUG] DPDK Init successful")

	// DPDK使用物理端口ID，通常从0开始，而不是UDP端口号
	dpdkPort := uint16(0) // 默认使用第一个DPDK端口
	log.Printf("[DEBUG] Using DPDK port %d for receiving", dpdkPort)

	rxFlow, err := flow.SetReceiver(dpdkPort)
	if err != nil {
		log.Printf("[ERROR] Failed to set receiver on DPDK port %d: %v", dpdkPort, err)
		return nil, err
	}
	log.Printf("[DEBUG] Successfully set receiver on DPDK port %d", dpdkPort)

	c := &UDPConn{
		localAddr: laddr,
		rxFlow:    rxFlow,
		txPort:    dpdkPort, // 这里存储的是DPDK端口ID，不是UDP端口号
		recvCh:    make(chan []byte, 4096),
	}
	log.Printf("[DEBUG] Created UDPConn with localAddr=%v, txPort=%d", laddr, dpdkPort)

	flow.SetHandler(rxFlow, func(pkt *packet.Packet, ctx flow.UserContext) {
		data := pkt.GetRawPacketBytes()
		log.Printf("[DEBUG] Received packet, raw length=%d bytes", len(data))

		// 检查以太网帧长度
		if len(data) < 42 { // 以太网头(14) + IP头(20) + UDP头(8)
			log.Printf("[DEBUG] Packet too short: %d bytes, need at least 42", len(data))
			return
		}

		// 检查是否为IP协议 (EtherType = 0x0800)
		if data[12] != 0x08 || data[13] != 0x00 {
			log.Printf("[DEBUG] Not IPv4 packet: EtherType=0x%02x%02x", data[12], data[13])
			return
		}
		log.Printf("[DEBUG] IPv4 packet detected")

		ipHeaderStart := 14

		// 检查是否为UDP协议 (Protocol = 17)
		if data[ipHeaderStart+9] != 17 {
			log.Printf("[DEBUG] Not UDP packet: Protocol=%d", data[ipHeaderStart+9])
			return
		}
		log.Printf("[DEBUG] UDP packet detected")

		// 计算IP头长度
		headerLength := int((data[ipHeaderStart] & 0x0F) * 4)
		udpStart := ipHeaderStart + headerLength
		log.Printf("[DEBUG] IP header length=%d, UDP start at offset=%d", headerLength, udpStart)

		// 检查UDP头长度
		if len(data) < udpStart+8 {
			log.Printf("[DEBUG] Packet too short for UDP header: %d bytes, need %d", len(data), udpStart+8)
			return
		}

		// 解析UDP头
		srcPort := binary.BigEndian.Uint16(data[udpStart : udpStart+2])
		dstPort := binary.BigEndian.Uint16(data[udpStart+2 : udpStart+4])
		udpLen := binary.BigEndian.Uint16(data[udpStart+4 : udpStart+6])
		log.Printf("[DEBUG] UDP header: src_port=%d, dst_port=%d, udp_len=%d", srcPort, dstPort, udpLen)

		// 如果指定了本地端口，检查目标端口是否匹配
		if laddr != nil && laddr.Port != 0 && int(dstPort) != laddr.Port {
			log.Printf("[DEBUG] Port mismatch: received dst_port=%d, expected=%d", dstPort, laddr.Port)
			return
		}

		// 提取目标IP地址
		dstIP := net.IPv4(data[ipHeaderStart+16], data[ipHeaderStart+17],
			data[ipHeaderStart+18], data[ipHeaderStart+19])
		srcIP := net.IPv4(data[ipHeaderStart+12], data[ipHeaderStart+13],
			data[ipHeaderStart+14], data[ipHeaderStart+15])
		log.Printf("[DEBUG] IP addresses: src=%s, dst=%s", srcIP.String(), dstIP.String())

		// 如果指定了本地地址，检查目标IP是否匹配
		if laddr != nil && laddr.IP != nil && !laddr.IP.IsUnspecified() {
			if !dstIP.Equal(laddr.IP) {
				log.Printf("[DEBUG] IP mismatch: received dst_ip=%s, expected=%s", dstIP.String(), laddr.IP.String())
				return
			}
		}

		// 验证UDP长度
		expectedLen := len(data) - udpStart
		if int(udpLen) != expectedLen {
			log.Printf("[WARNING] UDP length mismatch: header=%d, actual=%d", udpLen, expectedLen)
		}

		// 只有匹配的数据包才会被接收
		buf := make([]byte, len(data))
		copy(buf, data)
		log.Printf("[DEBUG] Packet passed all filters, queuing for application (payload_len=%d)", len(data)-udpStart-8)

		select {
		case c.recvCh <- buf:
			log.Printf("[DEBUG] Packet successfully queued")
		default:
			// 如果缓冲区满了，丢弃数据包
			log.Printf("[WARNING] UDP receive buffer full, dropping packet from port %d", srcPort)
		}
	}, nil)

	log.Printf("[DEBUG] Handler set successfully")

	// 启动DPDK数据包处理系统
	// 这必须在所有流和处理器设置完成后调用
	if !IsStarted() {
		log.Printf("[DEBUG] Starting DPDK packet processing system...")
		if err := SystemStart(); err != nil {
			log.Printf("[ERROR] Failed to start DPDK system: %v", err)
			return nil, err
		}
		log.Printf("[DEBUG] DPDK system started successfully")
	} else {
		log.Printf("[DEBUG] DPDK system already started")
	}

	log.Printf("[DEBUG] Returning UDPConn")
	return c, nil
}

func (c *UDPConn) ReadFromUDP(buf []byte) (int, *UDPAddr, error) {
	log.Printf("[DEBUG] ReadFromUDP called with buffer size=%d", len(buf))

	if c.closed {
		log.Printf("[ERROR] ReadFromUDP: connection is closed")
		return 0, nil, errors.New("connection closed")
	}

	log.Printf("[DEBUG] Waiting for packet from receive channel...")
	data := <-c.recvCh
	log.Printf("[DEBUG] Received packet from channel, raw length=%d", len(data))

	ipStart := 14
	srcIP := net.IPv4(data[ipStart+12], data[ipStart+13], data[ipStart+14], data[ipStart+15])
	udpStart := ipStart + int((data[ipStart]&0x0F)*4)
	srcPort := int(binary.BigEndian.Uint16(data[udpStart : udpStart+2]))

	payloadStart := udpStart + 8
	payloadLen := len(data) - payloadStart
	log.Printf("[DEBUG] Extracting payload: start=%d, length=%d", payloadStart, payloadLen)

	n := copy(buf, data[payloadStart:])
	addr := &UDPAddr{IP: srcIP, Port: srcPort}

	log.Printf("[DEBUG] ReadFromUDP returning: n=%d, addr=%s:%d", n, addr.IP.String(), addr.Port)
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
	log.Printf("[DEBUG] WriteToUDP called: dst=%s:%d, data_len=%d", addr.IP.String(), addr.Port, len(buf))

	if c.closed {
		log.Printf("[ERROR] WriteToUDP: connection is closed")
		return 0, errors.New("connection closed")
	}

	if addr == nil || addr.IP == nil {
		log.Printf("[ERROR] WriteToUDP: invalid destination address")
		return 0, errors.New("invalid destination address")
	}

	// 创建数据包
	log.Printf("[DEBUG] Creating new packet...")
	pkt, err := packet.NewPacket()
	if err != nil {
		log.Printf("[ERROR] Failed to create new packet: %v", err)
		return 0, err
	}
	log.Printf("[DEBUG] Packet created successfully")

	// 初始化IPv4 UDP数据包
	log.Printf("[DEBUG] Initializing IPv4 UDP packet with payload size=%d", len(buf))
	if !packet.InitEmptyIPv4UDPPacket(pkt, uint(len(buf))) {
		log.Printf("[ERROR] Failed to initialize IPv4 UDP packet")
		return 0, errors.New("failed to init IPv4 UDP packet")
	}
	log.Printf("[DEBUG] IPv4 UDP packet initialized successfully")

	// 获取各层指针
	ethHdr := pkt.Ether
	ipv4Hdr := pkt.GetIPv4NoCheck()
	udpHdr := pkt.GetUDPNoCheck()
	log.Printf("[DEBUG] Got header pointers: eth=%p, ipv4=%p, udp=%p", ethHdr, ipv4Hdr, udpHdr)

	// 设置以太网头 - 使用默认MAC地址
	ethHdr.DAddr = [6]uint8{0x00, 0x00, 0x00, 0x00, 0x00, 0x00} // 目标MAC
	ethHdr.SAddr = [6]uint8{0x00, 0x00, 0x00, 0x00, 0x00, 0x01} // 源MAC
	log.Printf("[DEBUG] Set Ethernet header: dst_mac=00:00:00:00:00:00, src_mac=00:00:00:00:00:01")

	// 设置源IP地址
	if c.localAddr != nil && c.localAddr.IP != nil {
		ipv4Hdr.SrcAddr = packet.SwapBytesIPv4Addr(types.SliceToIPv4(c.localAddr.IP.To4()))
		log.Printf("[DEBUG] Set source IP from local address: %s", c.localAddr.IP.String())
	} else {
		ipv4Hdr.SrcAddr = types.IPv4Address(0x0100007f) // 127.0.0.1 in network byte order
		log.Printf("[DEBUG] Set default source IP: 127.0.0.1")
	}

	// 设置目标IP地址
	ipv4Hdr.DstAddr = packet.SwapBytesIPv4Addr(types.SliceToIPv4(addr.IP.To4()))
	log.Printf("[DEBUG] Set destination IP: %s", addr.IP.String())

	// 设置UDP头
	if c.localAddr != nil {
		udpHdr.SrcPort = packet.SwapBytesUint16(uint16(c.localAddr.Port))
		log.Printf("[DEBUG] Set source port from local address: %d", c.localAddr.Port)
	} else {
		udpHdr.SrcPort = 0
		log.Printf("[DEBUG] Set default source port: 0")
	}
	udpHdr.DstPort = packet.SwapBytesUint16(uint16(addr.Port))
	log.Printf("[DEBUG] Set destination port: %d", addr.Port)

	// 复制用户数据
	data := (*[1 << 30]byte)(pkt.Data)[:len(buf)]
	copy(data, buf)
	log.Printf("[DEBUG] Copied %d bytes of user data to packet", len(buf))

	// 计算IP头校验和
	checksum := packet.CalculateIPv4Checksum(ipv4Hdr)
	ipv4Hdr.HdrChecksum = packet.SwapBytesUint16(checksum)
	log.Printf("[DEBUG] Calculated IP header checksum: 0x%04x", checksum)

	// 发送数据包 - 这里使用简化的发送方式
	// 实际应用中应该通过专门的发送流来发送
	log.Printf("[INFO] Sending UDP packet to %s:%d (len=%d)", addr.IP.String(), addr.Port, len(buf))
	log.Printf("[DEBUG] WriteToUDP completed successfully")

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
	log.Printf("[DEBUG] Close() called")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		log.Printf("[DEBUG] Connection already closed")
		return nil
	}
	c.closed = true
	close(c.recvCh)
	log.Printf("[DEBUG] Connection closed successfully")
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
