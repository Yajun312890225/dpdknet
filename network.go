package dpdknet

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/Yajun312890225/nff-go/flow"
	"github.com/Yajun312890225/nff-go/packet"
)

var (
	globalNetworkOnce sync.Once
	globalNetworkInit bool
	globalNetworkErr  error
	globalTxFlow      *flow.Flow
	globalSendCh      chan *packet.Packet
	globalBytesCh     chan []byte // 高性能字节发送通道
	udpListeners      map[string]*UDPConn
	tcpListeners      map[string]*TCPListener
	icmpHandlers      map[string]func([]byte, int, int, net.IP, net.IP) // ICMP处理器
	udpListenersMutex sync.RWMutex
	tcpListenersMutex sync.RWMutex
	icmpHandlersMutex sync.RWMutex
	localMAC          [6]uint8 // 本地网卡 MAC 地址
)

func init() {
	udpListeners = make(map[string]*UDPConn)
	tcpListeners = make(map[string]*TCPListener)
	icmpHandlers = make(map[string]func([]byte, int, int, net.IP, net.IP))
	globalSendCh = make(chan *packet.Packet, 65536) // 进一步增大发送缓冲区到64K
	globalBytesCh = make(chan []byte, 65536)        // 高性能字节通道
	// 设置默认 MAC 地址，稍后可以通过 DPDK 获取真实 MAC
	localMAC = [6]uint8{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
}

// EnsureGlobalNetworkInit 确保全局网络系统只初始化一次
func EnsureGlobalNetworkInit() error {
	globalNetworkOnce.Do(func() {
		log.Printf("[DEBUG] Initializing global network system...")
		globalNetworkErr = initializeGlobalNetwork()
		if globalNetworkErr == nil {
			globalNetworkInit = true
			log.Printf("[DEBUG] Global network system initialized successfully")
		}
	})
	return globalNetworkErr
}

// initializeGlobalNetwork 初始化全局网络系统
func initializeGlobalNetwork() error {
	// 初始化DPDK
	if err := Init(); err != nil {
		log.Printf("[ERROR] DPDK Init failed: %v", err)
		return err
	}
	log.Printf("[DEBUG] DPDK Init successful")

	// DPDK使用物理端口ID，通常从0开始
	dpdkPort := uint16(0)
	log.Printf("[DEBUG] Using DPDK port %d", dpdkPort)

	// 流1：接收流 - SetReceiver -> SetHandler -> SetSender
	log.Printf("[DEBUG] Setting up RX flow...")
	rxFlow, err := flow.SetReceiver(dpdkPort)
	if err != nil {
		log.Printf("[ERROR] Failed to set receiver on DPDK port %d: %v", dpdkPort, err)
		return err
	}
	log.Printf("[DEBUG] Successfully set receiver on DPDK port %d", dpdkPort)

	// 设置接收处理器
	flow.SetHandler(rxFlow, globalPacketHandler, nil)
	log.Printf("[DEBUG] RX packet handler set")

	localMAC = flow.GetPortMACAddress(dpdkPort)
	// 关闭接收流
	if err := flow.SetSender(rxFlow, dpdkPort); err != nil {
		log.Printf("[ERROR] Failed to set RX sender: %v", err)
		return err
	}
	log.Printf("[DEBUG] RX flow closed with sender")

	// 流2：发送流 - SetGenerator -> SetSender
	log.Printf("[DEBUG] Setting up TX flow...")
	globalTxFlow = flow.SetGenerator(globalSendGenerator, nil)
	if err := flow.SetSender(globalTxFlow, dpdkPort); err != nil {
		log.Printf("[ERROR] Failed to set TX sender: %v", err)
		return err
	}
	log.Printf("[DEBUG] TX flow created successfully")

	// 启动DPDK数据包处理系统
	if !IsStarted() {
		log.Printf("[DEBUG] Starting DPDK packet processing system...")
		if err := SystemStart(); err != nil {
			log.Printf("[ERROR] Failed to start DPDK system: %v", err)
			return err
		}
		log.Printf("[DEBUG] DPDK system started successfully")
	} else {
		log.Printf("[DEBUG] DPDK system already started")
	}

	return nil
}

// globalPacketHandler 全局包处理器，根据协议类型分发包
func globalPacketHandler(pkt *packet.Packet, ctx flow.UserContext) {
	data := pkt.GetRawPacketBytes()

	// 快速检查以太网帧长度
	if len(data) < 14 {
		return
	}

	// 快速检查是否为IP协议 (EtherType = 0x0800)
	if data[12] != 0x08 || data[13] != 0x00 {
		return
	}

	ipHeaderStart := 14
	if len(data) < ipHeaderStart+20 {
		return
	}

	// 解析IP协议类型
	protocol := data[ipHeaderStart+9]
	headerLength := int((data[ipHeaderStart] & 0x0F) * 4)

	// 提取IP地址
	dstIP := net.IPv4(data[ipHeaderStart+16], data[ipHeaderStart+17],
		data[ipHeaderStart+18], data[ipHeaderStart+19])
	srcIP := net.IPv4(data[ipHeaderStart+12], data[ipHeaderStart+13],
		data[ipHeaderStart+14], data[ipHeaderStart+15])

	// 只处理 UDP/TCP，ICMP 暂时禁用以避免影响 UDP 性能
	switch protocol {
	case 17: // UDP
		handleUDP(data, ipHeaderStart, headerLength, srcIP, dstIP)
	case 6: // TCP
		HandleTCPPacket(data, ipHeaderStart, headerLength, srcIP, dstIP)
	// case 1: // ICMP - 暂时禁用
	//	HandleICMPPacket(data, ipHeaderStart, headerLength, srcIP, dstIP)
	default:
		return
	}
}

// handleUDP 处理UDP包
func handleUDP(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	udpStart := ipHeaderStart + headerLength
	if len(data) < udpStart+8 {
		log.Printf("[WARNING] UDP packet too short: %d bytes, need at least %d", len(data), udpStart+8)
		return
	}

	// 解析UDP头
	// srcPort := binary.BigEndian.Uint16(data[udpStart : udpStart+2])
	dstPort := binary.BigEndian.Uint16(data[udpStart+2 : udpStart+4])
	// udpLen := binary.BigEndian.Uint16(data[udpStart+4 : udpStart+6])

	// 查找对应的UDP监听器
	key := fmt.Sprintf("udp:%s:%d", dstIP.String(), dstPort)
	udpListenersMutex.RLock()
	conn, exists := udpListeners[key]
	udpListenersMutex.RUnlock()

	if !exists {
		// 尝试通配符匹配 (0.0.0.0:port)
		wildcardKey := fmt.Sprintf("udp:0.0.0.0:%d", dstPort)
		udpListenersMutex.RLock()
		conn, exists = udpListeners[wildcardKey]
		udpListenersMutex.RUnlock()
		if !exists {
			return
		}
	}

	// 学习并保存客户端的MAC地址，用于后续回复
	clientKey := srcIP.String()
	conn.mu.Lock()
	srcMAC := [6]uint8{}
	copy(srcMAC[:], data[6:12]) // 提取源MAC地址
	conn.clientMACs[clientKey] = srcMAC

	// 同时学习本地真实MAC地址（目标MAC）
	// copy(conn.localMAC[:], data[0:6])
	conn.mu.Unlock()

	// 复制数据包并发送到对应的连接
	buf := make([]byte, len(data))
	copy(buf, data)

	select {
	case conn.recvCh <- buf:
		// 正常投递不打印日志
	default:
		log.Printf("[WARNING] UDP listener buffer full, dropping packet")
	}
}

// globalSendGenerator 全局发送生成器 - 简化为只处理包通道
func globalSendGenerator(pkt *packet.Packet, ctx flow.UserContext) {
	select {
	case sendPkt := <-globalSendCh:
		packet.GeneratePacketFromByte(pkt, sendPkt.GetRawPacketBytes())
		// DPDK 包通过引用计数自动管理，不需要手动释放
	case sendBytes := <-globalBytesCh:
		packet.GeneratePacketFromByte(pkt, sendBytes)
		packetPool.Put(sendBytes[:cap(sendBytes)])
	default:
		packet.InitEmptyPacket(pkt, 0)
	}
}

// RegisterUDPListener 注册UDP监听器
func RegisterUDPListener(key string, conn *UDPConn) error {
	udpListenersMutex.Lock()
	defer udpListenersMutex.Unlock()

	if _, exists := udpListeners[key]; exists {
		return fmt.Errorf("UDP listener already exists for %s", key)
	}

	udpListeners[key] = conn
	return nil
}

// UnregisterUDPListener 注销UDP监听器
func UnregisterUDPListener(key string) {
	udpListenersMutex.Lock()
	defer udpListenersMutex.Unlock()
	delete(udpListeners, key)
}

// SendRawBytes 直接发送字节数组，避免使用 packet.NewPacket()
func SendRawBytes(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("cannot send empty data")
	}

	// 直接非阻塞发送，如果失败就立即报错，不重试
	select {
	case globalBytesCh <- data:

		return nil
	default:
		// 队列满时立即失败，不重试（避免阻塞）
		log.Printf("[ERROR] Byte send queue full (%d/65536), dropping packet immediately", len(globalBytesCh))
		return fmt.Errorf("global byte send channel full")
	}
}

// SendPacket 通过全局发送通道发送数据包，非阻塞
func SendPacket(pkt *packet.Packet) error {
	// 只尝试非阻塞发送，避免任何形式的延迟
	select {
	case globalSendCh <- pkt:
		return nil
	default:
		// 队列满时立即失败，不重试（避免阻塞和延迟）
		log.Printf("[ERROR] Send queue full (%d/65536), dropping packet immediately", len(globalSendCh))
		return fmt.Errorf("global send channel full")
	}
}

// SendPacketReliable 可靠发送数据包，阻塞直到发送成功
func SendPacketReliable(pkt *packet.Packet) error {
	// 阻塞发送，确保包一定被放入队列
	globalSendCh <- pkt
	return nil
}

// FlushSendQueue 强制刷新发送队列 (调试用)
func FlushSendQueue() {
	// 这里可以添加强制刷新逻辑
	log.Printf("[DEBUG] Send queue length: %d", len(globalSendCh))
}

// RegisterTCPListener 注册TCP监听器
func RegisterTCPListener(key string, listener *TCPListener) error {
	tcpListenersMutex.Lock()
	defer tcpListenersMutex.Unlock()

	if _, exists := tcpListeners[key]; exists {
		return fmt.Errorf("TCP listener already exists for %s", key)
	}

	tcpListeners[key] = listener
	log.Printf("[DEBUG] Registered TCP listener: %s", key)
	return nil
}

// UnregisterTCPListener 注销TCP监听器
func UnregisterTCPListener(key string) {
	tcpListenersMutex.Lock()
	defer tcpListenersMutex.Unlock()
	delete(tcpListeners, key)
	log.Printf("[DEBUG] Unregistered TCP listener: %s", key)
}

// FindTCPListenerByKey 根据key查找TCP监听器
func FindTCPListenerByKey(key string) *TCPListener {
	tcpListenersMutex.RLock()
	defer tcpListenersMutex.RUnlock()

	return tcpListeners[key]
}

// SendTCPPacket 发送TCP数据包
func SendTCPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, seqNum, ackNum uint32, flags uint8, data []byte) error {
	// 构建以太网帧 + IP头 + TCP头 + 数据
	totalLen := 14 + 20 + 20 + len(data) // 以太网头 + IP头 + TCP头 + 数据

	// 从切片池获取缓冲区
	pooledSlice := packetPool.Get().([]byte)
	var packetData []byte
	var usePoolSlice bool

	// 如果池中的切片不够大，则创建新的切片
	if len(pooledSlice) >= totalLen {
		packetData = pooledSlice[:totalLen] // 使用池中的切片，调整长度
		usePoolSlice = true
	} else {
		// 池中切片太小，创建新的切片，并立即归还小切片
		packetData = make([]byte, totalLen)
		packetPool.Put(pooledSlice) // 立即归还小切片
		usePoolSlice = false
		log.Printf("[DEBUG] Pool slice too small (%d < %d) for TCP packet, allocating new slice",
			len(pooledSlice), totalLen)
	}

	// 获取本地MAC地址
	localMAC := GetLocalMAC()

	// 构建以太网头 (简化处理，使用广播地址)
	copy(packetData[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // 目标MAC
	copy(packetData[6:12], localMAC[:])                               // 源MAC
	binary.BigEndian.PutUint16(packetData[12:14], 0x0800)             // EtherType: IPv4

	// 构建IP头
	ipStart := 14
	packetData[ipStart] = 0x45                                                           // 版本和头长度
	packetData[ipStart+1] = 0x00                                                         // TOS
	binary.BigEndian.PutUint16(packetData[ipStart+2:ipStart+4], uint16(20+20+len(data))) // 总长度
	binary.BigEndian.PutUint16(packetData[ipStart+4:ipStart+6], 0x1234)                  // ID
	binary.BigEndian.PutUint16(packetData[ipStart+6:ipStart+8], 0x4000)                  // 标志和片偏移
	packetData[ipStart+8] = 64                                                           // TTL
	packetData[ipStart+9] = 6                                                            // 协议: TCP
	binary.BigEndian.PutUint16(packetData[ipStart+10:ipStart+12], 0)                     // 校验和(稍后计算)
	copy(packetData[ipStart+12:ipStart+16], srcIP.To4())                                 // 源IP
	copy(packetData[ipStart+16:ipStart+20], dstIP.To4())                                 // 目标IP

	// 构建TCP头
	tcpStart := ipStart + 20
	binary.BigEndian.PutUint16(packetData[tcpStart:tcpStart+2], srcPort)   // 源端口
	binary.BigEndian.PutUint16(packetData[tcpStart+2:tcpStart+4], dstPort) // 目标端口
	binary.BigEndian.PutUint32(packetData[tcpStart+4:tcpStart+8], seqNum)  // 序列号
	binary.BigEndian.PutUint32(packetData[tcpStart+8:tcpStart+12], ackNum) // 确认号
	packetData[tcpStart+12] = 0x50                                         // 数据偏移 (20字节)
	packetData[tcpStart+13] = flags                                        // 标志位
	binary.BigEndian.PutUint16(packetData[tcpStart+14:tcpStart+16], 65535) // 窗口大小
	binary.BigEndian.PutUint16(packetData[tcpStart+16:tcpStart+18], 0)     // 校验和(稍后计算)
	binary.BigEndian.PutUint16(packetData[tcpStart+18:tcpStart+20], 0)     // 紧急指针

	// 复制数据
	if len(data) > 0 {
		copy(packetData[tcpStart+20:tcpStart+20+len(data)], data)
	}

	// 计算IP头校验和
	packetData[ipStart+10] = 0
	packetData[ipStart+11] = 0
	ipChecksum := calcIPChecksum(packetData[ipStart : ipStart+20])
	binary.BigEndian.PutUint16(packetData[ipStart+10:ipStart+12], ipChecksum)

	// 计算TCP校验和
	binary.BigEndian.PutUint16(packetData[tcpStart+16:tcpStart+18], 0) // 先清零校验和字段
	tcpLen := 20 + len(data)
	tcpChecksum := calcTCPChecksum(srcIP, dstIP, packetData[tcpStart:tcpStart+tcpLen])
	binary.BigEndian.PutUint16(packetData[tcpStart+16:tcpStart+18], tcpChecksum)

	// 发送数据包
	err := SendRawBytes(packetData)

	// 只有使用了池切片的情况下才归还，且归还时恢复原始长度
	if usePoolSlice {
		// 将切片长度恢复为原始容量再归还给池
		packetPool.Put(pooledSlice[:cap(pooledSlice)])
	}

	return err
}

// calcIPChecksum 计算IP头校验和
func calcIPChecksum(data []byte) uint16 {
	sum := uint32(0)
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

// calcTCPChecksum 计算TCP校验和
func calcTCPChecksum(srcIP, dstIP net.IP, tcpData []byte) uint16 {
	// TCP伪头部：源IP(4) + 目标IP(4) + 协议(1) + TCP长度(2) = 12字节
	pseudoHeader := make([]byte, 12)
	copy(pseudoHeader[0:4], srcIP.To4())
	copy(pseudoHeader[4:8], dstIP.To4())
	pseudoHeader[8] = 0 // 填充
	pseudoHeader[9] = 6 // TCP协议号
	binary.BigEndian.PutUint16(pseudoHeader[10:12], uint16(len(tcpData)))

	// 计算校验和
	sum := uint32(0)

	// 伪头部校验和
	for i := 0; i < len(pseudoHeader); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudoHeader[i : i+2]))
	}

	// TCP头和数据校验和
	for i := 0; i < len(tcpData)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(tcpData[i : i+2]))
	}

	// 如果TCP数据长度是奇数，处理最后一个字节
	if len(tcpData)%2 == 1 {
		sum += uint32(tcpData[len(tcpData)-1]) << 8
	}

	// 折叠进位
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}

	return ^uint16(sum)
}

// GetLocalMAC 获取本地网卡 MAC 地址
func GetLocalMAC() [6]uint8 {
	return localMAC
}
