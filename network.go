package dpdknet

import (
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

	// 初始化和启动 gVisor netstack
	log.Printf("[DEBUG] Initializing gVisor netstack...")
	localIP := net.IPv4(192, 168, 66, 57) // 默认IP，可以通过环境变量或配置文件设置
	if err := IntegrateGVisorWithDPDK(localIP, localMAC); err != nil {
		log.Printf("[ERROR] Failed to integrate gVisor with DPDK: %v", err)
		return err
	}
	log.Printf("[DEBUG] gVisor netstack integrated successfully")

	return nil
}

// globalPacketHandler 全局包处理器，优先通过 gVisor netstack 处理
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

	// 首先尝试通过 gVisor netstack 处理
	if gvisor := GetGVisorNetstack(); gvisor != nil {
		// 注入到 gVisor netstack 进行协议栈处理
		gvisor.InjectDPDKPacket(data)
		return
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
