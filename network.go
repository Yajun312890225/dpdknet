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
	globalNetworkOnce sync.Once
	globalNetworkInit bool
	globalNetworkErr  error
	globalTxFlow      *flow.Flow
	globalSendCh      chan *packet.Packet
	globalBytesCh     chan []byte                                       // 高性能字节发送通道
	icmpHandlers      map[string]func([]byte, int, int, net.IP, net.IP) // ICMP处理器
	localMAC          [6]uint8
	// DPDK网卡的IP和网关MAC（初始化时获取并存储）
	dpdkLocalIP           net.IP           // DPDK网卡的本地IP
	dpdkGatewayMAC        net.HardwareAddr // DPDK网卡的网关MAC
	globalVXLANConfig     *VXLANConfig     // 全局 VXLAN 配置
	globalVXLANHandler    *VXLANHandler    // 全局 VXLAN 处理器
	globalPacketForwarder *PacketForwarder // 全局数据包转发器
	globalARPHandler      *ARPHandler      // 全局 ARP 处理器
)

func init() {
	icmpHandlers = make(map[string]func([]byte, int, int, net.IP, net.IP))
	globalSendCh = make(chan *packet.Packet, 65536) // 进一步增大发送缓冲区到64K
	globalBytesCh = make(chan []byte, 65536)        // 高性能字节通道
	// 设置默认 MAC 地址，稍后可以通过 DPDK 获取真实 MAC
	localMAC = [6]uint8{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
}

// getLocalIP 获取DPDK网卡本地IP地址
func getLocalIP() net.IP {
	if dpdkLocalIP != nil {
		return dpdkLocalIP
	}

	// 如果全局变量未设置，尝试从环境变量获取
	if ipStr := os.Getenv("DPDKNET_LOCAL_IP"); ipStr != "" {
		if ip := net.ParseIP(ipStr); ip != nil {
			dpdkLocalIP = ip.To4()
			return dpdkLocalIP
		}
	}

	return net.IPv4(192, 168, 66, 57)
}

// getGatewayMAC 获取DPDK网卡网关MAC地址
func getGatewayMAC() net.HardwareAddr {
	if dpdkGatewayMAC != nil {
		return dpdkGatewayMAC
	}

	return net.HardwareAddr{0xfe, 0xee, 0x89, 0x96, 0xac, 0xa3}
}

// getRemoteVTEPFromEnv 从环境变量获取远程 VTEP IP地址
func getRemoteVTEPFromEnv() net.IP {
	if ipStr := os.Getenv("DPDKNET_REMOTE_VTEP"); ipStr != "" {
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip.To4()
		}
	}
	// 默认远程 VTEP IP地址
	return net.IPv4(192, 168, 66, 29)
}

// EnsureGlobalNetworkInit 确保全局网络系统只初始化一次
func EnsureGlobalNetworkInit() error {
	globalNetworkOnce.Do(func() {
		initializeGlobalNetwork()
		globalNetworkInit = true
	})
	return globalNetworkErr
}

// initializeGlobalNetwork 初始化全局网络系统
func initializeGlobalNetwork() {
	// 初始化DPDK
	if err := Init(); err != nil {
		panic(err)
	}

	// 初始化pcap抓包功能
	if err := InitPcapCapture(); err != nil {
		log.Printf("[WARN] Failed to initialize pcap capture: %v", err)
	}

	// DPDK使用物理端口ID，通常从0开始
	dpdkPort := uint16(0)

	// 流1：接收流 - SetReceiver -> SetHandler -> SetSender
	rxFlow, err := flow.SetReceiver(dpdkPort)
	if err != nil {
		log.Printf("[ERROR] Failed to set receiver on DPDK port %d: %v", dpdkPort, err)
		panic(err)
	}

	// 设置接收处理器
	flow.SetHandler(rxFlow, globalPacketHandler, nil)

	localMAC = flow.GetPortMACAddress(dpdkPort)
	// 关闭接收流
	if err := flow.SetSender(rxFlow, dpdkPort); err != nil {
		log.Printf("[ERROR] Failed to set RX sender: %v", err)
		panic(err)
	}

	// 流2：发送流 - SetGenerator -> SetSender
	globalTxFlow = flow.SetGenerator(globalSendGenerator, nil)
	if err := flow.SetSender(globalTxFlow, dpdkPort); err != nil {
		log.Printf("[ERROR] Failed to set TX sender: %v", err)
		panic(err)
	}

	// 启动DPDK数据包处理系统
	if !IsStarted() {
		if err := SystemStart(); err != nil {
			log.Printf("[ERROR] Failed to start DPDK system: %v", err)
			panic(err)
		}
	}

	// 确保本地IP已设置
	if dpdkLocalIP == nil {
		dpdkLocalIP = getLocalIP()
		if dpdkLocalIP == nil {
			panic("[Network] 本地IP未设置，请通过环境变量DPDKNET_LOCAL_IP设置或实现DHCP获取")
		}
	}
	// 确保本地IP是IPv4格式
	dpdkLocalIP = dpdkLocalIP.To4()
	if dpdkLocalIP == nil {
		panic("[Network] 本地IP不是有效的IPv4地址")
	}

	// 获取网关IP，通过环境变量或计算默认网关
	var gatewayIP net.IP
	if gatewayIPStr := os.Getenv("DPDKNET_GATEWAY_IP"); gatewayIPStr != "" {
		gatewayIP = net.ParseIP(gatewayIPStr)
		if gatewayIP == nil {
			panic(fmt.Sprintf("[Network] 无效的网关IP环境变量: %s", gatewayIPStr))
		}
		gatewayIP = gatewayIP.To4() // 确保是IPv4
	} else {
		// 计算默认网关（假设是同网段的.1地址）
		localIPv4 := dpdkLocalIP.To4()
		if localIPv4 == nil {
			panic(fmt.Sprintf("[Network] 本地IP不是有效的IPv4地址: %s", dpdkLocalIP.String()))
		}
		gatewayIP = net.IPv4(localIPv4[0], localIPv4[1], localIPv4[2], 1)
	}

	// 通过ARP获取网关MAC
	globalARPHandler = NewARPHandler(dpdkLocalIP, net.HardwareAddr(localMAC[:]))

	gatewayMAC, err := globalARPHandler.RequestMAC(gatewayIP)
	if err != nil {
		panic(fmt.Sprintf("[Network] ARP获取网关MAC失败: %v", err))
	}

	// 保存网关MAC
	dpdkGatewayMAC = gatewayMAC

	// 使用获取到的真实IP初始化gVisor
	if err := IntegrateGVisorWithDPDK(dpdkLocalIP, localMAC); err != nil {
		log.Printf("[ERROR] Failed to integrate gVisor with DPDK: %v", err)
		panic(err)
	}

	// 执行 VXLAN 网络协议栈初始化流程
	if err := initializeVXLANNetworkStack(); err != nil {
		log.Printf("[ERROR] Failed to initialize VXLAN network stack: %v", err)
		panic(err)
	}

	// 检查环境变量是否需要启动数据包转发器
	if err := initializePacketForwarder(); err != nil {
		log.Printf("[ERROR] Failed to initialize packet forwarder: %v", err)
		panic(err)
	}

}

// initializeVXLANNetworkStack 初始化 VXLAN 网络协议栈
func initializeVXLANNetworkStack() error {

	// 步骤1: 建立 VXLAN 隧道
	vxlanConfig := &VXLANConfig{
		VNI:       66,
		LocalIP:   getLocalIP(),                                         // 外层本地VTEP IP
		RemoteIP:  getRemoteVTEPFromEnv(),                               // 外层远程VTEP IP
		UDPPort:   4789,                                                 // VXLAN 端口
		LocalMAC:  net.HardwareAddr(localMAC[:]),                        // 内层本地MAC
		RemoteMAC: net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}, // 内层远程MAC (示例)
	}

	dhcpClient := NewDHCPClient(vxlanConfig.LocalMAC, vxlanConfig, "vxlan-host")

	var err error
	dhcpOffer, err := dhcpClient.SendDHCPDiscover(5 * time.Second) // 5秒超时
	if err != nil {
		panic(fmt.Sprintf("DHCP Discover failed: %v", err))
	}

	// 从 DHCP 获取的 IP 地址和网关
	vxlanIP := dhcpOffer.YourIP        // DHCP 分配的内层 IP
	gatewayIP := dhcpOffer.Gateway     // DHCP 提供的网关 IP
	gatewayMAC := dhcpOffer.GatewayMAC // DHCP 提供的网关 MAC

	// 创建 ARP 处理器
	arpHandler := NewARPHandler(vxlanIP, vxlanConfig.LocalMAC)
	arpHandler.arpTable.AddEntry(gatewayIP, gatewayMAC)

	SetGlobalVXLANARPHandler(arpHandler)

	// 设置网关信息到VXLAN配置中
	vxlanConfig.SetGateway(gatewayIP, gatewayMAC)

	// 保存为全局VXLAN配置
	globalVXLANConfig = vxlanConfig
	globalVXLANHandler = NewVXLANHandler(vxlanConfig)

	// 确保全局网络栈监听VXLAN端口4789来接收外层VXLAN包
	if globalGVisorStack != nil {
		globalGVisorStack.RegisterVXLANConnection(
			vxlanConfig.LocalIP,
			vxlanConfig.UDPPort, // 4789端口
		)
	}

	return nil
}

// initializePacketForwarder 初始化数据包转发器
func initializePacketForwarder() error {
	// 检查环境变量是否启用数据包转发器
	enableForwarder := os.Getenv("ENABLE_PACKET_FORWARDER")
	if enableForwarder != "true" && enableForwarder != "1" {
		log.Printf("[INFO] Packet forwarder disabled (ENABLE_PACKET_FORWARDER=%s)", enableForwarder)
		return nil
	}

	// 创建数据包转发器
	forwarder, err := NewPacketForwarder()
	if err != nil {
		return fmt.Errorf("failed to create packet forwarder: %v", err)
	}

	// 添加默认过滤器
	if err := setupDefaultFilters(forwarder); err != nil {
		forwarder.Close()
		return fmt.Errorf("failed to setup default filters: %v", err)
	}
	go func() {
		// 启动转发器
		if err := forwarder.Start(); err != nil {
			forwarder.Close()
			log.Printf("failed to start packet forwarder: %v", err)
		}
	}()

	// 保存全局转发器引用
	globalPacketForwarder = forwarder

	return nil
}

// setupDefaultFilters 设置默认过滤器
func setupDefaultFilters(forwarder *PacketForwarder) error {
	// 1. VXLAN包过滤器 - 所有VXLAN包都自己处理
	vxlanFilter := NewVXLANFilter("VXLAN过滤器", 66)
	forwarder.AddFilter(vxlanFilter)

	// 默认端口过滤器
	defaultPorts := []uint16{4789, 22, 80, 443, 32333, 9999} // VXLAN, SSH, HTTP, HTTPS
	portFilter := NewPortFilter("端口过滤器", defaultPorts)
	forwarder.AddFilter(portFilter)

	return nil
}

// GetGlobalVXLANConfig 获取全局VXLAN配置
func GetGlobalVXLANConfig() *VXLANConfig {
	return globalVXLANConfig
}

// SetGlobalVXLANConfig 设置全局VXLAN配置
func SetGlobalVXLANConfig(config *VXLANConfig) {
	globalVXLANConfig = config
}

// GetGlobalVXLANHandler 获取全局VXLAN处理器
func GetGlobalVXLANHandler() *VXLANHandler {
	return globalVXLANHandler
}

// SetGlobalVXLANHandler 设置全局VXLAN处理器
func SetGlobalVXLANHandler(handler *VXLANHandler) {
	globalVXLANHandler = handler
}

// IsVXLANEnabled 检查是否启用了VXLAN
func IsVXLANEnabled() bool {
	return globalVXLANConfig != nil && globalVXLANHandler != nil
}

// globalPacketHandler 全局包处理器，按优先级处理数据包
func globalPacketHandler(pkt *packet.Packet, ctx flow.UserContext) {
	data := pkt.GetRawPacketBytes()

	// 将所有收到的数据包写入pcap文件
	if err := WritePacketToPcap(data); err != nil {
		// 不影响正常处理流程，只记录错误
		log.Printf("[PCAP] Failed to write packet to pcap: %v", err)
	}

	// 快速检查以太网帧长度
	if len(data) < 14 {
		return
	}

	// 检查EtherType
	etherType := uint16(data[12])<<8 | uint16(data[13])

	// 优先处理ARP包 (EtherType = 0x0806)
	if etherType == 0x0806 {
		// 使用全局ARP处理器处理ARP包
		if globalARPHandler != nil {
			if err := globalARPHandler.HandleARPPacket(data); err != nil {
				log.Printf("[ARP] Failed to handle ARP packet: %v", err)
			}
		}
		return
	}

	// 检查是否为IP协议 (EtherType = 0x0800)
	if etherType != 0x0800 {
		return
	}

	// 检查是否为 VXLAN 包 - VXLAN包有专门的处理逻辑，不走转发器
	if IsVXLANPacket(data) {
		// 先提取 VXLAN 内层数据
		innerData := extractVXLANPayload(data)
		if innerData != nil {
			// 检查内层是否为 DHCP 包
			if isDHCPPacket(innerData) {
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
		// 提取 UDP payload 作为 DHCP 数据
		dhcpData := extractDHCPData(data)
		if dhcpData != nil {
			// 使用带帧信息的 DHCP 处理函数
			HandleDHCPPacketWithFrame(data, dhcpData)
		}
		return
	}

	// 优先级1: 数据包转发器处理
	if globalPacketForwarder != nil {
		err := globalPacketForwarder.ProcessDPDKPacket(data)
		if err == nil {
			// 转发器成功处理，不再继续处理
			return
		} else if err != ErrNotHandled {
			// 转发器处理错误（非"不处理"错误），记录日志并继续尝试gVisor
			log.Printf("[WARN] Packet forwarder processing failed: %v", err)
			// 不要return，继续到gVisor处理
		}
		// 如果是"不处理"错误或处理失败，继续到gVisor处理
	}

	// 优先级2: 通过 gVisor netstack 处理（转发器未处理的包）
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
		if err := WritePacketToPcap(sendPkt.GetRawPacketBytes()); err != nil {
			// 不影响正常处理流程，只记录错误
			log.Printf("[PCAP] Failed to write packet to pcap: %v", err)
		}
		packet.GeneratePacketFromByte(pkt, sendPkt.GetRawPacketBytes())
		// DPDK 包通过引用计数自动管理，不需要手动释放
	case sendBytes := <-globalBytesCh:
		if err := WritePacketToPcap(sendBytes); err != nil {
			// 不影响正常处理流程，只记录错误
			log.Printf("[PCAP] Failed to write packet to pcap: %v", err)
		}
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



// GetGlobalPacketForwarder 获取全局数据包转发器
func GetGlobalPacketForwarder() *PacketForwarder {
	return globalPacketForwarder
}

// GetGlobalARPHandler 获取全局ARP处理器
func GetGlobalARPHandler() *ARPHandler {
	return globalARPHandler
}

// CleanupGlobalNetwork 清理全局网络资源
func CleanupGlobalNetwork() {
	if globalPacketForwarder != nil {
		log.Printf("[INFO] Stopping global packet forwarder...")
		globalPacketForwarder.Stop()
		globalPacketForwarder.Close()
		globalPacketForwarder = nil
	}

	// 关闭pcap抓包并打印文件信息
	ClosePcapCapture()
}
