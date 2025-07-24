package dpdknet

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/packet"
)

var (
	icmpConnections map[uint16]*ICMPConn
	icmpConnMutex   sync.RWMutex
)

func init() {
	icmpConnections = make(map[uint16]*ICMPConn)
}

const (
	ICMPEchoRequest = 8
	ICMPEchoReply   = 0
)

type ICMPHeader struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	ID       uint16
	Sequence uint16
}

type ICMPConn struct {
	localIP   net.IP
	localAddr *net.IPAddr
	id        uint16
	seq       uint16
	replyCh   chan *ICMPReply
	readCh    chan *ICMPPacket
	mu        sync.Mutex
	closed    bool
}

type ICMPReply struct {
	Addr     net.IP
	ID       uint16
	Sequence uint16
	RTT      time.Duration
}

// ICMPPacket 表示接收到的ICMP数据包
type ICMPPacket struct {
	Addr net.Addr
	Data []byte
}

// ListenIP 创建一个监听指定协议的连接，兼容 net.ListenIP
func ListenIP(network string, laddr *net.IPAddr) (net.PacketConn, error) {
	switch network {
	case "ip4:icmp", "ip:icmp":
		return NewICMPListener(laddr)
	default:
		return nil, fmt.Errorf("unsupported network type: %s", network)
	}
}

// ResolveIPAddr 解析IP地址，兼容 net.ResolveIPAddr
func ResolveIPAddr(network, address string) (*net.IPAddr, error) {
	switch network {
	case "ip4:icmp", "ip:icmp":
		if address == "" {
			return &net.IPAddr{IP: net.IPv4zero}, nil
		}
		ip := net.ParseIP(address)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address: %s", address)
		}
		return &net.IPAddr{IP: ip}, nil
	default:
		return nil, fmt.Errorf("unsupported network type: %s", network)
	}
}

// NewICMPListener 创建ICMP监听器，实现 net.PacketConn 接口
func NewICMPListener(laddr *net.IPAddr) (net.PacketConn, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	var localIP net.IP
	if laddr != nil && laddr.IP != nil {
		localIP = laddr.IP
	} else {
		// 使用默认IP或从环境变量获取
		localIP = getLocalIPFromEnv()
	}

	conn := &ICMPConn{
		localIP:   localIP,
		localAddr: &net.IPAddr{IP: localIP},
		id:        uint16(time.Now().Unix() & 0xFFFF),
		seq:       0,
		replyCh:   make(chan *ICMPReply, 1024),
		readCh:    make(chan *ICMPPacket, 1024),
	}

	// 注册ICMP连接
	icmpConnMutex.Lock()
	icmpConnections[conn.id] = conn
	icmpConnMutex.Unlock()

	return conn, nil
}

func NewICMPConn(localIP net.IP) (*ICMPConn, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	conn := &ICMPConn{
		localIP:   localIP,
		localAddr: &net.IPAddr{IP: localIP},
		id:        uint16(time.Now().Unix() & 0xFFFF),
		seq:       0,
		replyCh:   make(chan *ICMPReply, 1024),
		readCh:    make(chan *ICMPPacket, 1024),
	}

	// 注册ICMP连接
	icmpConnMutex.Lock()
	icmpConnections[conn.id] = conn
	icmpConnMutex.Unlock()

	// ICMP 处理已经通过全局网络系统处理，不需要单独的 flow
	return conn, nil
}

func (c *ICMPConn) Ping(dst net.IP, count int, timeout time.Duration) error {
	for i := 0; i < count; i++ {
		c.mu.Lock()
		c.seq++
		seq := c.seq
		c.mu.Unlock()

		start := time.Now()

		// 通过 gVisor netstack 发送ICMP Echo Request包
		if err := c.sendICMPRequest(dst, seq); err != nil {
			log.Printf("[ERROR] Failed to send ICMP request: %v", err)
			continue
		}

		// 等待回复
		select {
		case reply := <-c.replyCh:
			if reply.ID == c.id && reply.Sequence == seq {
				rtt := time.Since(start)
				fmt.Printf("Reply from %s: seq=%d time=%v\n",
					reply.Addr.String(), reply.Sequence, rtt)
			}
		case <-time.After(timeout):
			fmt.Printf("Request timeout for seq=%d\n", seq)
		}

		if i < count-1 {
			time.Sleep(time.Second)
		}
	}
	return nil
}

func (c *ICMPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	// 注销ICMP连接
	icmpConnMutex.Lock()
	delete(icmpConnections, c.id)
	icmpConnMutex.Unlock()

	close(c.replyCh)
	close(c.readCh)
	return nil
}

// 实现 net.PacketConn 接口

// ReadFrom 从连接读取数据包
func (c *ICMPConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	if c.closed {
		return 0, nil, fmt.Errorf("connection closed")
	}

	select {
	case pkt := <-c.readCh:
		n = copy(p, pkt.Data)
		return n, pkt.Addr, nil
	}
}

// WriteTo 向指定地址发送数据包
func (c *ICMPConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	if c.closed {
		return 0, fmt.Errorf("connection closed")
	}

	ipAddr, ok := addr.(*net.IPAddr)
	if !ok {
		return 0, fmt.Errorf("invalid address type")
	}

	// 解析ICMP数据包
	if len(p) < 8 {
		return 0, fmt.Errorf("ICMP packet too short")
	}

	// 发送ICMP数据包
	if err := c.sendRawICMP(ipAddr.IP, p); err != nil {
		return 0, err
	}

	return len(p), nil
}

// LocalAddr 返回本地地址
func (c *ICMPConn) LocalAddr() net.Addr {
	return c.localAddr
}

// SetDeadline 设置读写超时时间
func (c *ICMPConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

// SetReadDeadline 设置读超时时间
func (c *ICMPConn) SetReadDeadline(t time.Time) error {
	// TODO: 实现超时逻辑
	return nil
}

// SetWriteDeadline 设置写超时时间
func (c *ICMPConn) SetWriteDeadline(t time.Time) error {
	// TODO: 实现超时逻辑
	return nil
}

// sendRawICMP 发送原始ICMP数据包
func (c *ICMPConn) sendRawICMP(dst net.IP, icmpData []byte) error {
	// 获取 gVisor netstack 实例
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return fmt.Errorf("gVisor netstack not initialized")
	}

	// 创建完整的IP数据包
	totalLen := 20 + len(icmpData) // IP头 + ICMP数据
	data := make([]byte, totalLen)

	// 构建 IP 头 (20 字节)
	data[0] = 0x45                                          // Version (4) + IHL (5)
	data[1] = 0x00                                          // Type of Service
	binary.BigEndian.PutUint16(data[2:4], uint16(totalLen)) // Total Length
	binary.BigEndian.PutUint16(data[4:6], c.id)             // Identification
	binary.BigEndian.PutUint16(data[6:8], 0x4000)           // Flags + Fragment Offset
	data[8] = 64                                            // TTL
	data[9] = 1                                             // Protocol (ICMP)
	// Checksum 稍后计算
	copy(data[12:16], c.localIP.To4()) // Source IP
	copy(data[16:20], dst.To4())       // Destination IP

	// 计算 IP 头校验和
	ipChecksum := calculateChecksum(data[0:20])
	binary.BigEndian.PutUint16(data[10:12], ipChecksum)

	// 复制ICMP数据
	copy(data[20:], icmpData)

	// 重新计算ICMP校验和
	// 清零ICMP校验和字段
	data[22] = 0
	data[23] = 0
	icmpChecksum := calculateChecksum(data[20:])
	binary.BigEndian.PutUint16(data[22:24], icmpChecksum)

	// 注入数据包到 gVisor
	gvs.InjectDPDKPacket(data)

	return nil
}

// sendICMPRequest 通过 gVisor netstack 发送 ICMP Echo Request
func (c *ICMPConn) sendICMPRequest(dst net.IP, seq uint16) error {
	// 获取 gVisor netstack 实例
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return fmt.Errorf("gVisor netstack not initialized")
	}

	// 创建 ICMP Echo Request 数据包
	data := make([]byte, 64) // 包含 IP 头 + ICMP 头 + 数据

	// 构建 IP 头 (20 字节)
	data[0] = 0x45                                // Version (4) + IHL (5)
	data[1] = 0x00                                // Type of Service
	binary.BigEndian.PutUint16(data[2:4], 64)     // Total Length
	binary.BigEndian.PutUint16(data[4:6], c.id)   // Identification
	binary.BigEndian.PutUint16(data[6:8], 0x4000) // Flags + Fragment Offset
	data[8] = 64                                  // TTL
	data[9] = 1                                   // Protocol (ICMP)
	// Checksum 稍后计算
	copy(data[12:16], c.localIP.To4()) // Source IP
	copy(data[16:20], dst.To4())       // Destination IP

	// 计算 IP 头校验和
	ipChecksum := calculateChecksum(data[0:20])
	binary.BigEndian.PutUint16(data[10:12], ipChecksum)

	// 构建 ICMP 头 (8 字节)
	data[20] = ICMPEchoRequest // Type
	data[21] = 0               // Code
	// Checksum 稍后计算
	binary.BigEndian.PutUint16(data[24:26], c.id) // ID
	binary.BigEndian.PutUint16(data[26:28], seq)  // Sequence

	// 添加一些数据
	copy(data[28:], []byte("Hello, ICMP!"))

	// 计算 ICMP 校验和
	icmpChecksum := calculateChecksum(data[20:])
	binary.BigEndian.PutUint16(data[22:24], icmpChecksum)

	// 注入数据包到 gVisor
	gvs.InjectDPDKPacket(data)

	return nil
}

// calculateChecksum 计算校验和
func calculateChecksum(data []byte) uint16 {
	sum := uint32(0)

	// 按16位累加
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}

	// 处理奇数长度
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}

	// 处理进位
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}

	return ^uint16(sum)
}

// HandleICMPPacket 处理ICMP数据包 (由network.go调用)
func HandleICMPPacket(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	icmpStart := ipHeaderStart + headerLength
	if len(data) < icmpStart+8 {
		log.Printf("[DEBUG] ICMP packet too short")
		return
	}

	icmpType := data[icmpStart]
	icmpCode := data[icmpStart+1]
	log.Printf("[DEBUG] ICMP: %s -> %s (type=%d, code=%d)",
		srcIP.String(), dstIP.String(), icmpType, icmpCode)

	// 将所有ICMP数据包分发给监听连接
	distributeICMPPacket(data, icmpStart, srcIP, dstIP)

	// 处理不同类型的ICMP包
	switch icmpType {
	case ICMPEchoRequest: // Echo Request (ping)
		if icmpCode == 0 {
			handlePingRequest(data, ipHeaderStart, headerLength, icmpStart, srcIP, dstIP)
		}
	case ICMPEchoReply: // Echo Reply
		handlePingReply(data, icmpStart, srcIP)
	default:
		log.Printf("[DEBUG] Unsupported ICMP type: %d", icmpType)
	}
}

// distributeICMPPacket 将ICMP数据包分发给所有监听连接
func distributeICMPPacket(data []byte, icmpStart int, srcIP, dstIP net.IP) {
	icmpData := data[icmpStart:]
	packet := &ICMPPacket{
		Addr: &net.IPAddr{IP: srcIP},
		Data: make([]byte, len(icmpData)),
	}
	copy(packet.Data, icmpData)

	// 分发给所有ICMP连接
	icmpConnMutex.RLock()
	for _, conn := range icmpConnections {
		// 检查IP地址匹配（如果连接绑定了特定IP）
		if conn.localIP != nil && !conn.localIP.IsUnspecified() && !dstIP.Equal(conn.localIP) {
			continue
		}

		select {
		case conn.readCh <- packet:
			// 成功发送数据包
		default:
			// 通道已满，丢弃数据包
		}
	}
	icmpConnMutex.RUnlock()
}

// handlePingReply 处理ping回复
func handlePingReply(data []byte, icmpStart int, srcIP net.IP) {
	// 解析ICMP头
	id := binary.BigEndian.Uint16(data[icmpStart+4 : icmpStart+6])
	seq := binary.BigEndian.Uint16(data[icmpStart+6 : icmpStart+8])

	// 查找对应的ICMP连接
	icmpConnMutex.RLock()
	conn, exists := icmpConnections[id]
	icmpConnMutex.RUnlock()

	if !exists {
		return // 没有对应的连接
	}

	// 创建回复并发送到连接
	reply := &ICMPReply{
		Addr:     srcIP,
		ID:       id,
		Sequence: seq,
		RTT:      0, // RTT由Ping方法计算
	}

	// 发送到ping回复通道
	select {
	case conn.replyCh <- reply:
		// 成功发送回复
	default:
		// 通道已满，丢弃回复
	}

	// 同时发送到PacketConn读取通道
	icmpData := data[icmpStart:]
	packet := &ICMPPacket{
		Addr: &net.IPAddr{IP: srcIP},
		Data: make([]byte, len(icmpData)),
	}
	copy(packet.Data, icmpData)

	select {
	case conn.readCh <- packet:
		// 成功发送数据包
	default:
		// 通道已满，丢弃数据包
	}
}

// handlePingRequest 处理ping请求并发送回复
func handlePingRequest(data []byte, ipHeaderStart, headerLength, icmpStart int, srcIP, dstIP net.IP) {
	log.Printf("[DEBUG] Handling ping request from %s", srcIP.String())

	// 交换源和目标IP
	for i := 0; i < 4; i++ {
		data[ipHeaderStart+12+i], data[ipHeaderStart+16+i] = data[ipHeaderStart+16+i], data[ipHeaderStart+12+i]
	}

	// 将ICMP类型改为Echo Reply (0)
	data[icmpStart] = ICMPEchoReply

	// 重新计算ICMP校验和
	data[icmpStart+2] = 0 // 清零校验和
	data[icmpStart+3] = 0

	icmpLen := len(data) - icmpStart
	sum := uint32(0)
	for i := icmpStart; i < icmpStart+icmpLen-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if icmpLen%2 == 1 {
		sum += uint32(data[icmpStart+icmpLen-1]) << 8
	}

	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(data[icmpStart+2:icmpStart+4], checksum)

	// 重新计算IP头校验和
	data[ipHeaderStart+10] = 0
	data[ipHeaderStart+11] = 0

	sum = uint32(0)
	for i := ipHeaderStart; i < ipHeaderStart+headerLength-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}

	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	checksum = ^uint16(sum)
	binary.BigEndian.PutUint16(data[ipHeaderStart+10:ipHeaderStart+12], checksum)

	// 创建新数据包并发送
	pkt, err := packet.NewPacket()
	if err != nil {
		log.Printf("[ERROR] Failed to create ping reply packet: %v", err)
		return
	}

	// 复制修改后的数据
	rawData := pkt.GetRawPacketBytes()
	copy(rawData, data)

	if err := SendPacket(pkt); err != nil {
		log.Printf("[ERROR] Failed to send ping reply: %v", err)
	} else {
		log.Printf("[DEBUG] Ping reply sent to %s", srcIP.String())
	}
}
