package dpdknet

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/flow"
	"github.com/Yajun312890225/nff-go/packet"
)

var (
	globalNetworkOnce  sync.Once
	globalNetworkInit  bool
	globalNetworkErr   error
	globalTxFlow       *flow.Flow
	globalSendCh       chan *packet.Packet
	globalBytesCh      chan []byte                                       // 高性能字节发送通道
	icmpHandlers       map[string]func([]byte, int, int, net.IP, net.IP) // ICMP处理器
	localMAC           [6]uint8                                          // 本地网卡 MAC 地址
	globalVXLANConfig  *VXLANConfig                                      // 全局 VXLAN 配置
	globalVXLANHandler *VXLANHandler                                     // 全局 VXLAN 处理器
)

func init() {
	icmpHandlers = make(map[string]func([]byte, int, int, net.IP, net.IP))
	globalSendCh = make(chan *packet.Packet, 65536) // 进一步增大发送缓冲区到64K
	globalBytesCh = make(chan []byte, 65536)        // 高性能字节通道
	// 设置默认 MAC 地址，稍后可以通过 DPDK 获取真实 MAC
	localMAC = [6]uint8{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
}

// getLocalIPFromEnv 从环境变量获取本地IP地址，如果没有设置则使用默认值
func getLocalIPFromEnv() net.IP {
	if ipStr := os.Getenv("DPDKNET_LOCAL_IP"); ipStr != "" {
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip.To4()
		}
		log.Printf("[WARNING] Invalid IP address in DPDKNET_LOCAL_IP: %s, using default", ipStr)
	}
	// 默认IP地址
	return net.IPv4(192, 168, 66, 57)
}

// getRemoteVTEPFromEnv 从环境变量获取远程 VTEP IP地址
func getRemoteVTEPFromEnv() net.IP {
	if ipStr := os.Getenv("DPDKNET_REMOTE_VTEP"); ipStr != "" {
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip.To4()
		}
		log.Printf("[WARNING] Invalid IP address in DPDKNET_REMOTE_VTEP: %s, using default", ipStr)
	}
	// 默认远程 VTEP IP地址
	return net.IPv4(192, 168, 66, 29)
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
	log.Printf("[DEBUG] Local MAC address: %s", net.HardwareAddr(localMAC[:]).String())
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

	// 尝试从环境变量获取IP地址，如果没有设置则使用默认值
	localIP := getLocalIPFromEnv()
	if err := IntegrateGVisorWithDPDK(localIP, localMAC); err != nil {
		log.Printf("[ERROR] Failed to integrate gVisor with DPDK: %v", err)
		return err
	}
	log.Printf("[DEBUG] gVisor netstack integrated successfully")

	// 执行 VXLAN 网络协议栈初始化流程
	if err := initializeVXLANNetworkStack(); err != nil {
		log.Printf("[ERROR] Failed to initialize VXLAN network stack: %v", err)
		return err
	}
	log.Printf("[DEBUG] VXLAN network stack initialized successfully")

	return nil
}

// initializeVXLANNetworkStack 初始化 VXLAN 网络协议栈
func initializeVXLANNetworkStack() error {
	log.Printf("[VXLAN] 开始初始化 VXLAN 网络协议栈...")

	// 步骤1: 建立 VXLAN 隧道
	vxlanConfig := &VXLANConfig{
		VNI:       66,
		LocalIP:   getLocalIPFromEnv(),                                  // 外层本地VTEP IP
		RemoteIP:  getRemoteVTEPFromEnv(),                               // 外层远程VTEP IP
		UDPPort:   4789,                                                 // VXLAN 端口
		LocalMAC:  net.HardwareAddr(localMAC[:]),                        // 内层本地MAC
		RemoteMAC: net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}, // 内层远程MAC (示例)
	}
	log.Printf("[VXLAN] 步骤1: 建立 VXLAN 隧道 VNI=%d, %s -> %s",
		vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)

	// 步骤2: DHCP 发广播包获取IP地址 (dst mac: ff:ff:ff:ff:ff:ff)
	log.Printf("[VXLAN] 步骤2: DHCP 发广播包...")

	var dhcpOffer *DHCPOfferInfo

	dhcpClient := NewDHCPClient(vxlanConfig.LocalMAC, vxlanConfig, "vxlan-host")

	// 尝试简化的 DHCP 流程，设置短超时
	log.Printf("[VXLAN] 尝试 DHCP Discover (5秒超时)...")
	var err error
	dhcpOffer, err = dhcpClient.SendDHCPDiscover(5 * time.Second) // 5秒超时
	if err != nil {
		log.Printf("[VXLAN] DHCP Discover 失败，使用默认配置: %v", err)
		panic(fmt.Sprintf("DHCP Discover failed: %v", err))
	} else {
		log.Printf("[VXLAN] DHCP Discover 成功，收到 Offer")
	}

	// 从 DHCP 获取的 IP 地址和网关
	vxlanIP := dhcpOffer.YourIP        // DHCP 分配的内层 IP
	gatewayIP := dhcpOffer.Gateway     // DHCP 提供的网关 IP
	gatewayMAC := dhcpOffer.GatewayMAC // DHCP 提供的网关 MAC

	log.Printf("[VXLAN] DHCP 完成: 分配IP=%s, 网关=%s, 网关MAC=%s",
		vxlanIP, gatewayIP, gatewayMAC)

	// 设置网关信息到VXLAN配置中
	vxlanConfig.SetGateway(gatewayIP, gatewayMAC)
	log.Printf("[VXLAN] 已将网关信息设置到VXLAN配置中")

	// 保存为全局VXLAN配置
	globalVXLANConfig = vxlanConfig
	globalVXLANHandler = NewVXLANHandler(vxlanConfig)
	log.Printf("[VXLAN] 全局VXLAN配置和处理器已设置")

	// 创建 ARP 处理器
	arpHandler := NewARPHandler(vxlanIP, vxlanConfig.LocalMAC, vxlanConfig)

	// 步骤3: DHCP 提供了网关 MAC，直接添加到 ARP 表
	log.Printf("[VXLAN] 步骤3: 使用 DHCP 提供的网关 MAC...")
	arpHandler.SimulateGatewayARP(gatewayIP, gatewayMAC)
	log.Printf("[VXLAN] 网关 ARP 映射(来自DHCP): %s -> %s", gatewayIP, gatewayMAC)

	// 步骤4: 可选的 ARP 表测试（添加其他主机映射）
	log.Printf("[VXLAN] 步骤4: 完成其他主机 IP-MAC 映射...")

	// 打印最终的 ARP 表
	log.Printf("[VXLAN] 最终 ARP 表:")
	arpHandler.PrintARPTable()

	log.Printf("[VXLAN] VXLAN 网络协议栈初始化完成!")
	log.Printf("[VXLAN] - 内层网络: %s (MAC: %s)", vxlanIP, vxlanConfig.LocalMAC)
	log.Printf("[VXLAN] - 网关: %s (MAC: %s)", gatewayIP, gatewayMAC)
	log.Printf("[VXLAN] - VXLAN 隧道: %s -> %s (VNI: %d)",
		vxlanConfig.LocalIP, vxlanConfig.RemoteIP, vxlanConfig.VNI)

	return nil
}

// GetGlobalVXLANConfig 获取全局VXLAN配置
func GetGlobalVXLANConfig() *VXLANConfig {
	return globalVXLANConfig
}

// GetGlobalVXLANHandler 获取全局VXLAN处理器
func GetGlobalVXLANHandler() *VXLANHandler {
	return globalVXLANHandler
}

// IsVXLANEnabled 检查是否启用了VXLAN
func IsVXLANEnabled() bool {
	return globalVXLANConfig != nil && globalVXLANHandler != nil
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

	// 检查是否为 VXLAN 包
	if IsVXLANPacket(data) {
		// 先提取 VXLAN 内层数据
		innerData := extractVXLANPayload(data)
		if innerData != nil {
			// 检查内层是否为 DHCP 包
			if isDHCPPacket(innerData) {
				log.Printf("[DHCP] 检测到 VXLAN 内层 DHCP 包")
				dhcpData := extractDHCPData(innerData)
				if dhcpData != nil {
					// 使用带帧信息的 DHCP 处理函数
					HandleDHCPPacketWithFrame(innerData, dhcpData)
				}
				return
			}
		}
		// 处理其他 VXLAN 包
		handleIncomingVXLANPacket(data)
		return
	}

	// 检查是否为直接的 DHCP 包 (UDP 端口 67/68)
	if isDHCPPacket(data) {
		log.Printf("[DHCP] 检测到直接 DHCP 包")
		// 提取 UDP payload 作为 DHCP 数据
		dhcpData := extractDHCPData(data)
		if dhcpData != nil {
			// 使用带帧信息的 DHCP 处理函数
			HandleDHCPPacketWithFrame(data, dhcpData)
		}
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
} // SendPacket 通过全局发送通道发送数据包，非阻塞
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

// isDHCPPacket 检查是否为 DHCP 包
func isDHCPPacket(data []byte) bool {
	// 检查以太网帧最小长度
	if len(data) < 42 { // 以太网头(14) + IP头(20) + UDP头(8)
		return false
	}

	// 检查 IP 协议类型 (UDP = 17)
	if data[23] != 17 {
		return false
	}

	// 提取 UDP 源端口和目标端口 (网络字节序)
	srcPort := uint16(data[34])<<8 | uint16(data[35])
	dstPort := uint16(data[36])<<8 | uint16(data[37])

	// DHCP 使用端口 67 (服务器) 和 68 (客户端)
	return (srcPort == 67 && dstPort == 68) || (srcPort == 68 && dstPort == 67)
}

// extractDHCPData 从以太网帧中提取 DHCP 数据
func extractDHCPData(data []byte) []byte {
	if len(data) < 42 {
		return nil
	}

	// 计算 IP 头长度 (IHL 字段在第14字节的低4位)
	ipHeaderLen := int(data[14]&0x0F) * 4

	// UDP 头长度固定为 8 字节
	udpHeaderLen := 8

	// DHCP 数据开始位置
	dhcpStart := 14 + ipHeaderLen + udpHeaderLen

	if len(data) <= dhcpStart {
		return nil
	}

	return data[dhcpStart:]
}

// extractVXLANPayload 从 VXLAN 包中提取内层以太网帧
func extractVXLANPayload(data []byte) []byte {
	// VXLAN 包结构: 外层以太网(14) + 外层IP(20) + 外层UDP(8) + VXLAN头(8) + 内层以太网帧
	if len(data) < 50 { // 最小 VXLAN 包长度
		return nil
	}

	// 计算外层 IP 头长度
	outerIPHeaderLen := int(data[14]&0x0F) * 4

	// VXLAN 头部长度固定为 8 字节
	vxlanHeaderLen := 8

	// 内层以太网帧开始位置
	innerFrameStart := 14 + outerIPHeaderLen + 8 + vxlanHeaderLen

	if len(data) <= innerFrameStart {
		return nil
	}

	return data[innerFrameStart:]
}
