package dpdknet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// 数据包切片池，用于减少内存分配和GC压力
var packetPool = sync.Pool{
	New: func() interface{} {
		// 预分配一个较大的切片，可以容纳大部分数据包
		// 以太网最大帧长1518字节，加上一些余量分配2048字节
		return make([]byte, 2048)
	},
}

type UDPConn struct {
	localAddr    *UDPAddr
	remoteAddr   *UDPAddr
	recvCh       chan []byte
	mu           sync.Mutex
	closed       bool
	listenerKeys []string            // 存储所有注册的监听器key
	clientMACs   map[string][6]uint8 // 存储客户端IP对应的MAC地址
	// localMAC     [6]uint8            // 本地真实MAC地址（从接收包中学习）
}

func ListenUDP(network string, laddr *UDPAddr) (*UDPConn, error) {
	// 确保全局DPDK系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		log.Printf("[ERROR] Global network init failed: %v", err)
		return nil, err
	}

	c := &UDPConn{
		localAddr:  laddr,
		recvCh:     make(chan []byte, 16384), // 增大缓冲区
		clientMACs: make(map[string][6]uint8),
	}

	// 注册UDP监听器到全局路由器
	var key string
	if laddr.IP == nil || laddr.IP.IsUnspecified() {
		// 监听所有接口
		key = fmt.Sprintf("udp:0.0.0.0:%d", laddr.Port)
	} else {
		// 监听特定接口
		key = fmt.Sprintf("udp:%s:%d", laddr.IP.String(), laddr.Port)

		// 为了兼容性，也注册一个通配符监听器
		wildcardKey := fmt.Sprintf("udp:0.0.0.0:%d", laddr.Port)
		c.listenerKeys = append(c.listenerKeys, wildcardKey)
		if err := RegisterUDPListener(wildcardKey, c); err != nil {
			log.Printf("[ERROR] Failed to register wildcard UDP listener: %v", err)
			return nil, err
		}
	}
	c.listenerKeys = append(c.listenerKeys, key)
	if err := RegisterUDPListener(key, c); err != nil {
		log.Printf("[ERROR] Failed to register UDP listener: %v", err)
		return nil, err
	}

	return c, nil
}

func (c *UDPConn) ReadFromUDP(buf []byte) (int, *UDPAddr, error) {
	if c.closed {
		return 0, nil, errors.New("connection closed")
	}

	select {
	case data, ok := <-c.recvCh:
		if !ok {
			return 0, nil, errors.New("connection closed")
		}

		// 快速提取源MAC地址
		srcMAC := [6]uint8{}
		copy(srcMAC[:], data[6:12])

		ipStart := 14
		srcIP := net.IPv4(data[ipStart+12], data[ipStart+13], data[ipStart+14], data[ipStart+15])
		udpStart := ipStart + int((data[ipStart]&0x0F)*4)
		srcPort := int(binary.BigEndian.Uint16(data[udpStart : udpStart+2]))

		// 保存客户端MAC地址和本地真实MAC地址
		clientKey := srcIP.String()
		c.mu.Lock()
		c.clientMACs[clientKey] = srcMAC
		// 学习本地真实MAC地址（数据包的目标MAC）
		// copy(c.localMAC[:], data[0:6])
		c.mu.Unlock()

		payloadStart := udpStart + 8
		n := copy(buf, data[payloadStart:])
		addr := &UDPAddr{IP: srcIP, Port: srcPort}

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
	if c.closed {
		return 0, errors.New("connection closed")
	}

	if addr == nil || addr.IP == nil {
		return 0, errors.New("invalid destination address")
	}

	// 查找客户端的MAC地址
	clientKey := addr.IP.String()
	c.mu.Lock()
	clientMAC, hasMAC := c.clientMACs[clientKey]
	c.mu.Unlock()

	if !hasMAC {
		// 使用广播 MAC，让网络自动学习路由
		clientMAC = [6]uint8{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	}
	return c.writeUDPWithMAC(buf, addr, clientMAC)
}

// writeUDPWithMAC 使用指定的目标MAC地址写入UDP包
func (c *UDPConn) writeUDPWithMAC(buf []byte, addr *UDPAddr, dstMAC [6]uint8) (int, error) {

	localMAC = GetLocalMAC()

	// 构建完整的UDP包字节数组，使用切片池避免频繁内存分配
	totalLen := 14 + 20 + 8 + len(buf) // Ethernet + IP + UDP + payload

	// 从切片池获取缓冲区
	packetData := packetPool.Get().([]byte)

	// 构建以太网头 (14字节)
	copy(packetData[0:6], dstMAC[:])    // 目标MAC
	copy(packetData[6:12], localMAC[:]) // 源MAC
	packetData[12] = 0x08               // EtherType高字节 (IPv4)
	packetData[13] = 0x00               // EtherType低字节

	// 构建IP头 (20字节)
	ipStart := 14
	packetData[ipStart] = 0x45                                                // 版本(4) + 头长度(5*4=20)
	packetData[ipStart+1] = 0x00                                              // TOS
	binary.BigEndian.PutUint16(packetData[ipStart+2:], uint16(20+8+len(buf))) // 总长度
	binary.BigEndian.PutUint16(packetData[ipStart+4:], 0x0000)                // ID
	binary.BigEndian.PutUint16(packetData[ipStart+6:], 0x4000)                // 标志位和片偏移
	packetData[ipStart+8] = 64                                                // TTL
	packetData[ipStart+9] = 17                                                // 协议: UDP

	// 设置源IP地址
	if c.localAddr != nil && c.localAddr.IP != nil {
		srcIP := c.localAddr.IP.To4()
		copy(packetData[ipStart+12:ipStart+16], srcIP)
	} else {
		// 默认127.0.0.1
		packetData[ipStart+12] = 127
		packetData[ipStart+13] = 0
		packetData[ipStart+14] = 0
		packetData[ipStart+15] = 1
	}

	// 设置目标IP地址
	dstIPBytes := addr.IP.To4()
	copy(packetData[ipStart+16:ipStart+20], dstIPBytes)

	// 构建UDP头 (8字节)
	udpStart := ipStart + 20
	if c.localAddr != nil {
		binary.BigEndian.PutUint16(packetData[udpStart:], uint16(c.localAddr.Port))
	} else {
		binary.BigEndian.PutUint16(packetData[udpStart:], 0)
	}
	binary.BigEndian.PutUint16(packetData[udpStart+2:], uint16(addr.Port))
	binary.BigEndian.PutUint16(packetData[udpStart+4:], uint16(8+len(buf))) // UDP长度
	binary.BigEndian.PutUint16(packetData[udpStart+6:], 0)                  // 校验和，稍后计算

	// 复制用户数据
	copy(packetData[udpStart+8:], buf)

	// 计算IP校验和
	binary.BigEndian.PutUint16(packetData[ipStart+10:], 0) // 清零校验和字段
	ipChecksum := calculateIPChecksum(packetData[ipStart : ipStart+20])
	binary.BigEndian.PutUint16(packetData[ipStart+10:], ipChecksum)

	// 计算UDP校验和（简化为0，很多实现都这样做）
	binary.BigEndian.PutUint16(packetData[udpStart+6:], 0)

	// 发送数据包
	err := SendRawBytes(packetData[:totalLen])

	if err != nil {
		packetPool.Put(packetData[:cap(packetData)])
		log.Printf("[ERROR] Failed to send UDP packet: %v", err)
		return 0, err
	}

	return len(buf), nil
}

// calculateIPChecksum 计算IP头校验和
func calculateIPChecksum(header []byte) uint16 {
	sum := uint32(0)
	for i := 0; i < len(header); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
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
