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
	udpListeners      map[string]*UDPConn
	tcpListeners      map[string]*TCPListener
	icmpHandlers      map[string]func([]byte, int, int, net.IP, net.IP) // ICMP处理器
	udpListenersMutex sync.RWMutex
	tcpListenersMutex sync.RWMutex
	icmpHandlersMutex sync.RWMutex
)

func init() {
	udpListeners = make(map[string]*UDPConn)
	tcpListeners = make(map[string]*TCPListener)
	icmpHandlers = make(map[string]func([]byte, int, int, net.IP, net.IP))
	globalSendCh = make(chan *packet.Packet, 4096)
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
	log.Printf("[DEBUG] Received packet, raw length=%d bytes", len(data))

	// 检查以太网帧长度
	if len(data) < 14 {
		log.Printf("[DEBUG] Packet too short: %d bytes", len(data))
		return
	}

	// 检查是否为IP协议 (EtherType = 0x0800)
	if data[12] != 0x08 || data[13] != 0x00 {
		log.Printf("[DEBUG] Not IPv4 packet: EtherType=0x%02x%02x", data[12], data[13])
		return
	}

	ipHeaderStart := 14
	if len(data) < ipHeaderStart+20 {
		log.Printf("[DEBUG] IPv4 packet too short")
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

	log.Printf("[DEBUG] Protocol=%d, src=%s, dst=%s", protocol, srcIP.String(), dstIP.String())

	switch protocol {
	case 1: // ICMP
		HandleICMPPacket(data, ipHeaderStart, headerLength, srcIP, dstIP)
	case 6: // TCP
		HandleTCPPacket(data, ipHeaderStart, headerLength, srcIP, dstIP)
	case 17: // UDP
		handleUDP(data, ipHeaderStart, headerLength, srcIP, dstIP)
	default:
		log.Printf("[DEBUG] Unsupported protocol: %d", protocol)
	}
}

// handleUDP 处理UDP包
func handleUDP(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	udpStart := ipHeaderStart + headerLength
	if len(data) < udpStart+8 {
		log.Printf("[DEBUG] UDP packet too short")
		return
	}

	// 解析UDP头
	srcPort := binary.BigEndian.Uint16(data[udpStart : udpStart+2])
	dstPort := binary.BigEndian.Uint16(data[udpStart+2 : udpStart+4])
	log.Printf("[DEBUG] UDP: %s:%d -> %s:%d", srcIP.String(), srcPort, dstIP.String(), dstPort)

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
			log.Printf("[DEBUG] No UDP listener found for %s or %s", key, wildcardKey)
			return
		}
	}

	// 复制数据包并发送到对应的连接
	buf := make([]byte, len(data))
	copy(buf, data)

	select {
	case conn.recvCh <- buf:
		log.Printf("[DEBUG] UDP packet delivered to listener")
	default:
		log.Printf("[WARNING] UDP listener buffer full, dropping packet")
	}
}

// globalSendGenerator 全局发送生成器
func globalSendGenerator(pkt *packet.Packet, ctx flow.UserContext) {
	select {
	case sendPkt := <-globalSendCh:
		*pkt = *sendPkt
		log.Printf("[DEBUG] Global generator: packet prepared for sending")
	default:
		// 没有数据包要发送，生成一个空包
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
	log.Printf("[DEBUG] Registered UDP listener: %s", key)
	return nil
}

// UnregisterUDPListener 注销UDP监听器
func UnregisterUDPListener(key string) {
	udpListenersMutex.Lock()
	defer udpListenersMutex.Unlock()
	delete(udpListeners, key)
	log.Printf("[DEBUG] Unregistered UDP listener: %s", key)
}

// SendPacket 通过全局发送通道发送数据包
func SendPacket(pkt *packet.Packet) error {
	select {
	case globalSendCh <- pkt:
		log.Printf("[DEBUG] Packet queued for sending")
		return nil
	default:
		return fmt.Errorf("global send channel full")
	}
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
	// 创建新的数据包
	pkt, err := packet.NewPacket()
	if err != nil {
		return fmt.Errorf("failed to create packet: %v", err)
	}

	// 构建以太网帧 + IP头 + TCP头 + 数据
	totalLen := 14 + 20 + 20 + len(data) // 以太网头 + IP头 + TCP头 + 数据
	rawData := pkt.GetRawPacketBytes()

	// 确保有足够空间
	if len(rawData) < totalLen {
		return fmt.Errorf("packet buffer too small")
	}

	// 构建以太网头 (简化处理，使用广播地址)
	copy(rawData[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})  // 目标MAC
	copy(rawData[6:12], []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}) // 源MAC
	binary.BigEndian.PutUint16(rawData[12:14], 0x0800)              // EtherType: IPv4

	// 构建IP头
	ipStart := 14
	rawData[ipStart] = 0x45                                                           // 版本和头长度
	rawData[ipStart+1] = 0x00                                                         // TOS
	binary.BigEndian.PutUint16(rawData[ipStart+2:ipStart+4], uint16(20+20+len(data))) // 总长度
	binary.BigEndian.PutUint16(rawData[ipStart+4:ipStart+6], 0x1234)                  // ID
	binary.BigEndian.PutUint16(rawData[ipStart+6:ipStart+8], 0x4000)                  // 标志和片偏移
	rawData[ipStart+8] = 64                                                           // TTL
	rawData[ipStart+9] = 6                                                            // 协议: TCP
	binary.BigEndian.PutUint16(rawData[ipStart+10:ipStart+12], 0)                     // 校验和(稍后计算)
	copy(rawData[ipStart+12:ipStart+16], srcIP.To4())                                 // 源IP
	copy(rawData[ipStart+16:ipStart+20], dstIP.To4())                                 // 目标IP

	// 构建TCP头
	tcpStart := ipStart + 20
	binary.BigEndian.PutUint16(rawData[tcpStart:tcpStart+2], srcPort)   // 源端口
	binary.BigEndian.PutUint16(rawData[tcpStart+2:tcpStart+4], dstPort) // 目标端口
	binary.BigEndian.PutUint32(rawData[tcpStart+4:tcpStart+8], seqNum)  // 序列号
	binary.BigEndian.PutUint32(rawData[tcpStart+8:tcpStart+12], ackNum) // 确认号
	rawData[tcpStart+12] = 0x50                                         // 数据偏移 (20字节)
	rawData[tcpStart+13] = flags                                        // 标志位
	binary.BigEndian.PutUint16(rawData[tcpStart+14:tcpStart+16], 65535) // 窗口大小
	binary.BigEndian.PutUint16(rawData[tcpStart+16:tcpStart+18], 0)     // 校验和(稍后计算)
	binary.BigEndian.PutUint16(rawData[tcpStart+18:tcpStart+20], 0)     // 紧急指针

	// 复制数据
	if len(data) > 0 {
		copy(rawData[tcpStart+20:tcpStart+20+len(data)], data)
	}

	// 计算IP头校验和
	rawData[ipStart+10] = 0
	rawData[ipStart+11] = 0
	ipChecksum := calcIPChecksum(rawData[ipStart : ipStart+20])
	binary.BigEndian.PutUint16(rawData[ipStart+10:ipStart+12], ipChecksum)

	// 计算TCP校验和 (简化处理，设为0)
	binary.BigEndian.PutUint16(rawData[tcpStart+16:tcpStart+18], 0)

	// 发送数据包
	return SendPacket(pkt)
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
