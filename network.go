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
	tcpListeners      map[string]interface{} // 为将来的TCP支持预留
	udpListenersMutex sync.RWMutex
	tcpListenersMutex sync.RWMutex
)

func init() {
	udpListeners = make(map[string]*UDPConn)
	tcpListeners = make(map[string]interface{})
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

	// 设置接收流
	rxFlow, err := flow.SetReceiver(dpdkPort)
	if err != nil {
		log.Printf("[ERROR] Failed to set receiver on DPDK port %d: %v", dpdkPort, err)
		return err
	}
	log.Printf("[DEBUG] Successfully set receiver on DPDK port %d", dpdkPort)

	// 设置全局包处理器 - 根据协议类型分发包
	flow.SetHandler(rxFlow, globalPacketHandler, nil)
	log.Printf("[DEBUG] Global packet handler set")

	// 设置发送流
	globalTxFlow = flow.SetGenerator(globalSendGenerator, nil)
	if err := flow.SetSender(globalTxFlow, dpdkPort); err != nil {
		log.Printf("[ERROR] Failed to set sender: %v", err)
		return err
	}
	log.Printf("[DEBUG] Global TX flow created successfully")

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
		handleICMP(data, ipHeaderStart, headerLength, srcIP, dstIP)
	case 6: // TCP
		handleTCP(data, ipHeaderStart, headerLength, srcIP, dstIP)
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

// handleTCP 处理TCP包 (预留，暂时不实现)
func handleTCP(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	log.Printf("[DEBUG] TCP packet received, not implemented yet")
}

// handleICMP 处理ICMP包 (可以实现ping响应等)
func handleICMP(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	log.Printf("[DEBUG] ICMP packet received, not implemented yet")
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
		return nil
	default:
		return fmt.Errorf("global send channel full")
	}
}
