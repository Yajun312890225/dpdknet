package dpdknet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/packet"
	"github.com/Yajun312890225/nff-go/types"
)

type UDPConn struct {
	localAddr    *UDPAddr
	remoteAddr   *UDPAddr
	recvCh       chan []byte
	mu           sync.Mutex
	closed       bool
	listenerKeys []string            // 存储所有注册的监听器key
	clientMACs   map[string][6]uint8 // 存储客户端IP对应的MAC地址
}

func ListenUDP(network string, laddr *UDPAddr) (*UDPConn, error) {
	log.Printf("[DEBUG] === ListenUDP ENTRY === network=%s, laddr=%v", network, laddr)

	// 确保全局DPDK系统已初始化
	log.Printf("[DEBUG] Calling EnsureGlobalNetworkInit...")
	if err := EnsureGlobalNetworkInit(); err != nil {
		log.Printf("[ERROR] Global network init failed: %v", err)
		return nil, err
	}
	log.Printf("[DEBUG] Global network init completed successfully")

	c := &UDPConn{
		localAddr:  laddr,
		recvCh:     make(chan []byte, 4096),
		clientMACs: make(map[string][6]uint8),
	}
	log.Printf("[DEBUG] Created UDPConn with localAddr=%v", laddr)

	// 注册UDP监听器到全局路由器
	var key string
	log.Printf("[DEBUG] laddr.IP = %v, IsUnspecified = %v", laddr.IP, laddr.IP != nil && laddr.IP.IsUnspecified())
	if laddr.IP == nil || laddr.IP.IsUnspecified() {
		// 监听所有接口
		key = fmt.Sprintf("udp:0.0.0.0:%d", laddr.Port)
		log.Printf("[DEBUG] Registering UDP listener for all interfaces: %s", key)
	} else {
		// 监听特定接口
		key = fmt.Sprintf("udp:%s:%d", laddr.IP.String(), laddr.Port)
		log.Printf("[DEBUG] Registering UDP listener for specific interface: %s", key)

		// 为了兼容性，也注册一个通配符监听器
		wildcardKey := fmt.Sprintf("udp:0.0.0.0:%d", laddr.Port)
		log.Printf("[DEBUG] Also registering wildcard UDP listener: %s", wildcardKey)
		c.listenerKeys = append(c.listenerKeys, wildcardKey)
		if err := RegisterUDPListener(wildcardKey, c); err != nil {
			log.Printf("[ERROR] Failed to register wildcard UDP listener: %v", err)
			return nil, err
		} else {
			log.Printf("[DEBUG] Wildcard UDP listener registered successfully: %s", wildcardKey)
		}
	}
	c.listenerKeys = append(c.listenerKeys, key)
	if err := RegisterUDPListener(key, c); err != nil {
		log.Printf("[ERROR] Failed to register UDP listener: %v", err)
		return nil, err
	}

	log.Printf("[DEBUG] UDP listener registered for %s", key)
	log.Printf("[DEBUG] Total listeners registered: %d", len(c.listenerKeys))
	log.Printf("[DEBUG] === ListenUDP COMPLETE === returning connection")
	return c, nil
}

func (c *UDPConn) ReadFromUDP(buf []byte) (int, *UDPAddr, error) {
	log.Printf("[DEBUG] ReadFromUDP called with buffer size=%d", len(buf))

	if c.closed {
		log.Printf("[ERROR] ReadFromUDP: connection is closed")
		return 0, nil, errors.New("connection closed")
	}

	log.Printf("[DEBUG] Waiting for packet from receive channel...")
	select {
	case data, ok := <-c.recvCh:
		if !ok {
			log.Printf("[DEBUG] Receive channel closed")
			return 0, nil, errors.New("connection closed")
		}
		log.Printf("[DEBUG] Received packet from channel, raw length=%d", len(data))

		// 提取源MAC地址
		srcMAC := [6]uint8{}
		copy(srcMAC[:], data[6:12])
		log.Printf("[DEBUG] Source MAC: %02x:%02x:%02x:%02x:%02x:%02x",
			srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])

		ipStart := 14
		srcIP := net.IPv4(data[ipStart+12], data[ipStart+13], data[ipStart+14], data[ipStart+15])
		udpStart := ipStart + int((data[ipStart]&0x0F)*4)
		srcPort := int(binary.BigEndian.Uint16(data[udpStart : udpStart+2]))

		// 保存客户端MAC地址
		clientKey := srcIP.String()
		c.mu.Lock()
		c.clientMACs[clientKey] = srcMAC
		c.mu.Unlock()
		log.Printf("[DEBUG] Saved client MAC for %s: %02x:%02x:%02x:%02x:%02x:%02x",
			clientKey, srcMAC[0], srcMAC[1], srcMAC[2], srcMAC[3], srcMAC[4], srcMAC[5])

		payloadStart := udpStart + 8
		payloadLen := len(data) - payloadStart
		log.Printf("[DEBUG] Extracting payload: start=%d, length=%d", payloadStart, payloadLen)

		n := copy(buf, data[payloadStart:])
		addr := &UDPAddr{IP: srcIP, Port: srcPort}

		log.Printf("[DEBUG] ReadFromUDP returning: n=%d, addr=%s:%d", n, addr.IP.String(), addr.Port)
		return n, addr, nil
	}
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

	// 查找客户端的MAC地址
	clientKey := addr.IP.String()
	c.mu.Lock()
	clientMAC, hasMAC := c.clientMACs[clientKey]
	c.mu.Unlock()

	if hasMAC {
		log.Printf("[DEBUG] Found client MAC for %s: %02x:%02x:%02x:%02x:%02x:%02x",
			clientKey, clientMAC[0], clientMAC[1], clientMAC[2], clientMAC[3], clientMAC[4], clientMAC[5])
		return c.writeUDPWithMAC(buf, addr, clientMAC)
	} else {
		log.Printf("[DEBUG] No MAC found for client %s, using broadcast", clientKey)
		return c.writeUDPWithMAC(buf, addr, [6]uint8{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	}
}

// writeUDPWithMAC 使用指定的目标MAC地址写入UDP包
func (c *UDPConn) writeUDPWithMAC(buf []byte, addr *UDPAddr, dstMAC [6]uint8) (int, error) {
	log.Printf("[DEBUG] WriteToUDP with MAC called: dst=%s:%d, data_len=%d", addr.IP.String(), addr.Port, len(buf))

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

	// 设置以太网头 - 使用正确的MAC地址
	ethHdr.DAddr = dstMAC                                       // 目标MAC（客户端）
	ethHdr.SAddr = [6]uint8{0x52, 0x54, 0x00, 0x1c, 0x9c, 0xb6} // 服务器的真实MAC地址
	log.Printf("[DEBUG] Set Ethernet header: dst_mac=%02x:%02x:%02x:%02x:%02x:%02x, src_mac=52:54:00:1c:9c:b6",
		dstMAC[0], dstMAC[1], dstMAC[2], dstMAC[3], dstMAC[4], dstMAC[5])

	// 设置源IP地址 (使用主机字节序给types.IPv4Address)
	if c.localAddr != nil && c.localAddr.IP != nil {
		srcIP := c.localAddr.IP.To4()
		// types.IPv4Address期望主机字节序，所以反转字节顺序
		ipv4Hdr.SrcAddr = types.IPv4Address(uint32(srcIP[3])<<24 | uint32(srcIP[2])<<16 | uint32(srcIP[1])<<8 | uint32(srcIP[0]))
		log.Printf("[DEBUG] Set source IP from local address: %s (0x%08x)", c.localAddr.IP.String(), uint32(ipv4Hdr.SrcAddr))
	} else {
		ipv4Hdr.SrcAddr = types.IPv4Address(0x7f000001) // 127.0.0.1
		log.Printf("[DEBUG] Set default source IP: 127.0.0.1")
	}

	// 设置目标IP地址 (使用主机字节序给types.IPv4Address)
	dstIPBytes := addr.IP.To4()
	// types.IPv4Address期望主机字节序，所以反转字节顺序
	ipv4Hdr.DstAddr = types.IPv4Address(uint32(dstIPBytes[3])<<24 | uint32(dstIPBytes[2])<<16 | uint32(dstIPBytes[1])<<8 | uint32(dstIPBytes[0]))
	log.Printf("[DEBUG] Set destination IP: %s (0x%08x)", addr.IP.String(), uint32(ipv4Hdr.DstAddr))

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

	// 计算UDP校验和 (简化实现，设为0表示不使用校验和)
	udpHdr.DgramCksum = 0
	log.Printf("[DEBUG] Set UDP checksum to 0 (disabled)")

	// 计算IP头校验和
	checksum := packet.CalculateIPv4Checksum(ipv4Hdr)
	ipv4Hdr.HdrChecksum = packet.SwapBytesUint16(checksum)
	log.Printf("[DEBUG] Calculated IP header checksum: 0x%04x", checksum)

	// 通过全局发送通道发送数据包
	log.Printf("[DEBUG] Sending packet through global TX channel...")
	log.Printf("[DEBUG] Final packet raw bytes length: %d", len(pkt.GetRawPacketBytes()))

	// 打印前64字节的包内容用于调试
	rawBytes := pkt.GetRawPacketBytes()
	if len(rawBytes) > 0 {
		debugLen := len(rawBytes)
		if debugLen > 64 {
			debugLen = 64
		}
		log.Printf("[DEBUG] Packet hex dump (first %d bytes): %x", debugLen, rawBytes[:debugLen])
	}

	if err := SendPacket(pkt); err != nil {
		log.Printf("[ERROR] Failed to send packet: %v", err)
		return 0, err
	}

	log.Printf("[INFO] UDP packet sent to %s:%d (len=%d)", addr.IP.String(), addr.Port, len(buf))
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

	// 从全局路由器注销监听器
	for _, key := range c.listenerKeys {
		if key != "" {
			UnregisterUDPListener(key)
		}
	}

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
