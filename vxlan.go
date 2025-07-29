package dpdknet

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// VXLANConfig VXLAN 配置
type VXLANConfig struct {
	VNI       uint32           // VXLAN Network Identifier
	LocalIP   net.IP           // 本地 VTEP IP
	RemoteIP  net.IP           // 远程 VTEP IP
	UDPPort   uint16           // VXLAN UDP 端口，默认 4789
	LocalMAC  net.HardwareAddr // 本地 MAC 地址
	RemoteMAC net.HardwareAddr // 远程 MAC 地址

	// ARP缓存，用于存储内层网络的IP到MAC映射
	ARPCache map[string]net.HardwareAddr // key: IP地址字符串, value: MAC地址
	ARPMutex sync.RWMutex                // 保护ARP缓存的读写锁

	// 网关信息
	GatewayIP  net.IP           // 网关IP地址
	GatewayMAC net.HardwareAddr // 网关MAC地址
}

// DefaultVXLANConfig 默认 VXLAN 配置
func DefaultVXLANConfig() *VXLANConfig {
	return &VXLANConfig{
		VNI:      1000,
		UDPPort:  4789, // IANA 分配的 VXLAN 端口
		ARPCache: make(map[string]net.HardwareAddr),
	}
}

// GetMACFromARP 从ARP缓存或系统ARP表获取MAC地址
func (config *VXLANConfig) GetMACFromARP(ip net.IP) net.HardwareAddr {
	if config.ARPCache == nil {
		config.ARPCache = make(map[string]net.HardwareAddr)
	}

	ipStr := ip.String()

	// 首先检查缓存
	config.ARPMutex.RLock()
	if mac, exists := config.ARPCache[ipStr]; exists {
		config.ARPMutex.RUnlock()
		return mac
	}
	config.ARPMutex.RUnlock()

	// 缓存中没有，从系统ARP表查询
	mac := querySystemARP(ipStr)

	// 将结果存入缓存
	config.ARPMutex.Lock()
	config.ARPCache[ipStr] = mac
	config.ARPMutex.Unlock()

	return mac
}

// ClearARPCache 清空ARP缓存
func (config *VXLANConfig) ClearARPCache() {
	config.ARPMutex.Lock()
	defer config.ARPMutex.Unlock()
	config.ARPCache = make(map[string]net.HardwareAddr)
}

// SetARPEntry 手动设置ARP条目
func (config *VXLANConfig) SetARPEntry(ip net.IP, mac net.HardwareAddr) {
	if config.ARPCache == nil {
		config.ARPCache = make(map[string]net.HardwareAddr)
	}

	config.ARPMutex.Lock()
	defer config.ARPMutex.Unlock()
	config.ARPCache[ip.String()] = mac
}

// SetGateway 设置网关信息
func (config *VXLANConfig) SetGateway(gatewayIP net.IP, gatewayMAC net.HardwareAddr) {
	config.GatewayIP = gatewayIP
	config.GatewayMAC = gatewayMAC
	// 同时也将网关添加到ARP缓存中
	config.SetARPEntry(gatewayIP, gatewayMAC)
}

// GetDestinationMAC 获取目标MAC地址，如果是跨网段则返回网关MAC
func (config *VXLANConfig) GetDestinationMAC(dstIP net.IP) net.HardwareAddr {
	// 检查目标IP是否为内网地址
	if isPublicIP(dstIP) || isRemoteNetwork(dstIP) {
		// 对于公网IP或跨网段的地址，使用网关MAC
		if config.GatewayMAC != nil {
			return config.GatewayMAC
		}
	}

	// 同网段或直连的情况，从ARP缓存获取
	return config.GetMACFromARP(dstIP)
} // isPublicIP 检查IP是否为公网地址
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// 检查是否为本地回环地址
	if ip.IsLoopback() {
		return false
	}

	// 检查是否为私有地址
	ip4 := ip.To4()
	if ip4 == nil {
		return false // IPv6暂时不处理
	}

	// 10.0.0.0/8
	if ip4[0] == 10 {
		return false
	}

	// 172.16.0.0/12
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return false
	}

	// 192.168.0.0/16
	if ip4[0] == 192 && ip4[1] == 168 {
		return false
	}

	// 169.254.0.0/16 (链路本地地址)
	if ip4[0] == 169 && ip4[1] == 254 {
		return false
	}

	return true // 是公网地址
}

// isRemoteNetwork 检查IP是否为远程网络（这里简化为非11.x.x.x网段）
func isRemoteNetwork(ip net.IP) bool {
	if ip == nil {
		return false
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}

	// 如果不是11.x.x.x网段，认为是远程网络
	return ip4[0] != 11
} // querySystemARP 查询系统ARP表获取MAC地址
func querySystemARP(ipStr string) net.HardwareAddr {
	// 先尝试ping来触发ARP
	exec.Command("ping", "-c", "1", "-W", "1", ipStr).Run()

	// 查询ARP表
	cmd := exec.Command("arp", "-n", ipStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return generateVirtualMAC(ipStr)
	}

	// 解析ARP输出
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == ipStr {
			// 格式通常是: IP ... MAC ...
			for _, field := range fields {
				if strings.Contains(field, ":") && len(field) == 17 {
					mac, err := net.ParseMAC(field)
					if err == nil {
						return mac
					}
				}
			}
		}
	}

	return generateVirtualMAC(ipStr)
}

// generateVirtualMAC 为找不到ARP记录的IP生成虚拟MAC
func generateVirtualMAC(ipStr string) net.HardwareAddr {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}

	// 使用02:ff前缀表示这是一个虚拟MAC地址
	return net.HardwareAddr{0x02, 0xff, ip4[0], ip4[1], ip4[2], ip4[3]}
}

// VXLANHandler VXLAN 处理器
type VXLANHandler struct {
	config *VXLANConfig
}

// NewVXLANHandler 创建新的 VXLAN 处理器
func NewVXLANHandler(config *VXLANConfig) *VXLANHandler {
	if config == nil {
		config = DefaultVXLANConfig()
	}
	return &VXLANHandler{
		config: config,
	}
}

// EncapsulateVXLAN 封装 VXLAN 包，支持多种内层协议
func (vh *VXLANHandler) EncapsulateVXLAN(innerPayload []byte, innerSrcIP, innerDstIP net.IP, innerSrcPort, innerDstPort uint16, protocol layers.IPProtocol) ([]byte, error) {
	// 创建包缓冲区
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	// 1. 构建外层以太网头
	ethLayer := &layers.Ethernet{
		SrcMAC:       vh.config.LocalMAC,
		DstMAC:       vh.config.RemoteMAC,
		EthernetType: layers.EthernetTypeIPv4,
	}

	// 2. 构建外层 IP 头
	ipLayer := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    vh.config.LocalIP,
		DstIP:    vh.config.RemoteIP,
	}

	// 3. 构建外层 UDP 头
	udpLayer := &layers.UDP{
		SrcPort: layers.UDPPort(10000 + uint16((vh.config.VNI)%55535)), // 简单随机，实际可用更复杂算法
		DstPort: layers.UDPPort(vh.config.UDPPort),
	}
	udpLayer.SetNetworkLayerForChecksum(ipLayer)

	// 4. 构建 VXLAN 头
	vxlanLayer := &layers.VXLAN{
		ValidIDFlag: true,
		VNI:         vh.config.VNI,
	}

	// 5. 构建内层以太网头
	innerEthLayer := &layers.Ethernet{
		SrcMAC:       generateInnerSrcMAC(innerSrcIP),         // 为源IP生成虚拟MAC
		DstMAC:       vh.config.GetDestinationMAC(innerDstIP), // 使用网关MAC或ARP查询的MAC
		EthernetType: layers.EthernetTypeIPv4,
	}
	// 6. 直接使用原始内层 IP 包数据，而不是重新构建
	// 序列化外层结构
	layersToSerialize := []gopacket.SerializableLayer{
		ethLayer, ipLayer, udpLayer, vxlanLayer, innerEthLayer,
	}

	// 创建payload层包含原始内层IP数据
	payloadLayer := gopacket.Payload(innerPayload)
	layersToSerialize = append(layersToSerialize, payloadLayer)

	// 序列化所有层
	err := gopacket.SerializeLayers(buf, opts, layersToSerialize...)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize VXLAN packet: %v", err)
	}

	return buf.Bytes(), nil
}

// generateInnerSrcMAC 为内层源IP生成虚拟MAC地址
func generateInnerSrcMAC(ip net.IP) net.HardwareAddr {
	if ip == nil {
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}

	// 对于其他11.x.x.x网段的地址，使用统一的MAC模式
	ip4 := ip.To4()
	if ip4 != nil && ip4[0] == 11 {
		// 为11网段使用特殊的MAC前缀
		return net.HardwareAddr{0x02, 0x11, ip4[1], ip4[2], ip4[3], 0x00}
	}

	// 默认情况：使用 IP 的最后4个字节生成 MAC（前缀02:00表示本地管理地址）
	if ip4 == nil {
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}
	return net.HardwareAddr{0x02, 0x00, ip4[0], ip4[1], ip4[2], ip4[3]}
}

// DecapsulateVXLAN 解封装 VXLAN 包，返回内层网络信息
func (vh *VXLANHandler) DecapsulateVXLAN(data []byte) (innerPayload []byte, innerSrcIP, innerDstIP net.IP, innerSrcPort, innerDstPort uint16, protocol layers.IPProtocol, vni uint32, err error) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)

	// 检查是否包含 VXLAN 层
	vxlanLayer := packet.Layer(layers.LayerTypeVXLAN)
	if vxlanLayer == nil {
		return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("packet does not contain VXLAN layer")
	}

	vxlan, ok := vxlanLayer.(*layers.VXLAN)
	if !ok {
		return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("invalid VXLAN layer")
	}

	// 返回实际的 VNI，不进行强制匹配检查
	// 这样可以处理不同 VNI 的包
	vni = vxlan.VNI

	// 解析内层包 - 优先尝试以太网帧解析
	innerPacket := gopacket.NewPacket(vxlan.LayerPayload(), layers.LayerTypeEthernet, gopacket.Default)

	// 检查是否为ARP包
	if arpLayer := innerPacket.Layer(layers.LayerTypeARP); arpLayer != nil {
		// ARP包直接返回整个以太网帧
		return vxlan.LayerPayload(), nil, nil, 0, 0, 0, vni, nil
	}

	// 尝试获取IP层
	innerIPLayer := innerPacket.Layer(layers.LayerTypeIPv4)
	if innerIPLayer == nil {
		// 如果以太网帧解析失败，尝试直接解析为IP包
		innerPacket = gopacket.NewPacket(vxlan.LayerPayload(), layers.LayerTypeIPv4, gopacket.Default)
		innerIPLayer = innerPacket.Layer(layers.LayerTypeIPv4)
		if innerIPLayer == nil {
			return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("inner packet does not contain IP layer")
		}
	}

	innerIP, ok := innerIPLayer.(*layers.IPv4)
	if !ok {
		return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("invalid inner IP layer")
	}

	innerSrcIP = innerIP.SrcIP
	innerDstIP = innerIP.DstIP
	protocol = innerIP.Protocol

	// 根据协议类型解析传输层
	switch protocol {
	case layers.IPProtocolUDP:
		// 获取内层 UDP 层
		innerUDPLayer := innerPacket.Layer(layers.LayerTypeUDP)
		if innerUDPLayer == nil {
			return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("inner packet does not contain UDP layer")
		}

		innerUDP, ok := innerUDPLayer.(*layers.UDP)
		if !ok {
			return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("invalid inner UDP layer")
		}

		innerSrcPort = uint16(innerUDP.SrcPort)
		innerDstPort = uint16(innerUDP.DstPort)
		innerPayload = innerUDP.Payload

	case layers.IPProtocolTCP:
		// 获取内层 TCP 层
		innerTCPLayer := innerPacket.Layer(layers.LayerTypeTCP)
		if innerTCPLayer == nil {
			return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("inner packet does not contain TCP layer")
		}

		innerTCP, ok := innerTCPLayer.(*layers.TCP)
		if !ok {
			return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("invalid inner TCP layer")
		}

		innerSrcPort = uint16(innerTCP.SrcPort)
		innerDstPort = uint16(innerTCP.DstPort)
		innerPayload = innerTCP.Payload

	case layers.IPProtocolICMPv4:
		// 对于 ICMP，没有端口概念
		innerSrcPort = 0
		innerDstPort = 0
		// 获取 ICMP 层数据
		innerICMPLayer := innerPacket.Layer(layers.LayerTypeICMPv4)
		if innerICMPLayer != nil {
			innerICMP := innerICMPLayer.(*layers.ICMPv4)
			innerPayload = innerICMP.Payload
		} else {
			// 如果没有找到 ICMP 层，使用 IP 载荷
			innerPayload = innerIP.Payload
		}

	default:
		// 其他协议，直接使用 IP 载荷
		innerSrcPort = 0
		innerDstPort = 0
		innerPayload = innerIP.Payload
	}

	return innerPayload, innerSrcIP, innerDstIP, innerSrcPort, innerDstPort, protocol, vxlan.VNI, nil
}

// IsVXLANPacket 检查数据包是否为 VXLAN 包
func IsVXLANPacket(data []byte) bool {
	if len(data) < 42 { // 最小 VXLAN 包长度
		return false
	}

	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)

	// 检查是否有 UDP 层且目标端口为 4789
	udpLayer := packet.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return false
	}

	udp, ok := udpLayer.(*layers.UDP)
	if !ok {
		return false
	}

	// 检查是否为 VXLAN 端口
	return udp.DstPort == 4789

}

// CreateVXLANTunnel 创建 VXLAN 隧道配置
func CreateVXLANTunnel(localIP, remoteIP net.IP, vni uint32) *VXLANConfig {
	return &VXLANConfig{
		VNI:       vni,
		LocalIP:   localIP,
		RemoteIP:  remoteIP,
		UDPPort:   4789,
		LocalMAC:  getLocalMAC(),
		RemoteMAC: getRemoteMACFromARP(remoteIP), // 通过 ARP 表获取
	}
}

// GetLocalMAC 获取本地 MAC 地址（从 DPDK 启动时获取的 MAC）
func GetLocalMAC() net.HardwareAddr {
	// 使用全局的 localMAC
	mac := make(net.HardwareAddr, 6)
	copy(mac, localMAC[:])
	return mac
}

// getLocalMAC 获取本地 MAC 地址（内部使用）
func getLocalMAC() net.HardwareAddr {
	return GetLocalMAC()
}

// getRemoteMACFromARP 通过 ARP 表获取远程 MAC 地址
func getRemoteMACFromARP(remoteIP net.IP) net.HardwareAddr {
	ipStr := remoteIP.String()
	out, err := execCommand("arp", []string{"-n", ipStr})
	if err != nil {
		log.Printf("[WARN] ARP 查询失败: %v", err)
		// 查询失败时，返回广播 MAC，防止 nil
		return net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	}
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "at" && i+1 < len(fields) {
			macStr := fields[i+1]
			mac, err := net.ParseMAC(macStr)
			if err == nil {
				return mac
			}
		}
	}
	log.Printf("[WARN] 未找到 ARP MAC, IP: %s，使用广播 MAC", ipStr)
	return net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
}

// execCommand 执行命令并返回输出
func execCommand(cmd string, args []string) (string, error) {
	c := exec.Command(cmd, args...)
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// VXLANStats VXLAN 统计信息
type VXLANStats struct {
	EncapsulatedPackets uint64
	DecapsulatedPackets uint64
	EncapsulationErrors uint64
	DecapsulationErrors uint64
	VNIMismatches       uint64
}

// GetStats 获取 VXLAN 统计信息
func (vh *VXLANHandler) GetStats() *VXLANStats {
	// 简化实现，返回空统计
	return &VXLANStats{}
}

// SetConfig 更新 VXLAN 配置
func (vh *VXLANHandler) SetConfig(config *VXLANConfig) {
	vh.config = config
}

// GetConfig 获取当前 VXLAN 配置
func (vh *VXLANHandler) GetConfig() *VXLANConfig {
	return vh.config
}

// -------------------- VXLAN 包处理入口 --------------------
// handleIncomingVXLANPacket 处理传入的 VXLAN 包
func handleIncomingVXLANPacket(data []byte) {

	// 检查是否为 VXLAN 包
	if !IsVXLANPacket(data) {
		return
	}

	// 直接注入原始的VXLAN内层数据到gVisor
	// VXLAN解包已经返回了完整的内层以太网帧，不需要重新构建
	if gvisor := GetGVisorNetstack(); gvisor != nil {
		// 从VXLAN包中提取完整的内层以太网帧
		packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)
		vxlanLayer := packet.Layer(layers.LayerTypeVXLAN)
		if vxlanLayer != nil {
			vxlan := vxlanLayer.(*layers.VXLAN)
			innerEthernetFrame := vxlan.LayerPayload() // 完整的内层以太网帧
			// 检查内层以太网帧的MAC地址
			if len(innerEthernetFrame) >= 14 {
				// 修复目标MAC地址：将内层目标MAC替换为本地gVisor的MAC地址
				localMAC := GetLocalMAC()
				copy(innerEthernetFrame[0:6], localMAC)
			}

			gvisor.InjectDPDKPacket(innerEthernetFrame)
		}
	}
}


// -------------------- VXLAN 核心处理方法 --------------------

// extractVNIFromPacket 从 VXLAN 包中提取 VNI
func extractVNIFromPacket(data []byte) (uint32, error) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)

	// 获取 VXLAN 层
	vxlanLayer := packet.Layer(layers.LayerTypeVXLAN)
	if vxlanLayer == nil {
		return 0, fmt.Errorf("packet does not contain VXLAN layer")
	}

	vxlan, ok := vxlanLayer.(*layers.VXLAN)
	if !ok {
		return 0, fmt.Errorf("invalid VXLAN layer")
	}

	return vxlan.VNI, nil
}

func constructInnerIPPacket(payload []byte, srcIP, dstIP net.IP, srcPort, dstPort uint16, protocol layers.IPProtocol) []byte {
	// 创建以太网帧
	ethLayer := &layers.Ethernet{
		SrcMAC:       generateInnerSrcMAC(srcIP),         // 源MAC使用虚拟MAC
		DstMAC:       generateVirtualMAC(dstIP.String()), // 目标MAC暂时使用虚拟MAC，后续可优化
		EthernetType: layers.EthernetTypeIPv4,
	}

	// 创建 IP 层
	ipLayer := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: protocol,
		SrcIP:    srcIP,
		DstIP:    dstIP,
	}

	// 创建传输层
	var transportLayer gopacket.SerializableLayer

	switch protocol {
	case layers.IPProtocolTCP:
		transportLayer = &layers.TCP{
			SrcPort: layers.TCPPort(srcPort),
			DstPort: layers.TCPPort(dstPort),
		}
		tcp := transportLayer.(*layers.TCP)
		tcp.SetNetworkLayerForChecksum(ipLayer)
	case layers.IPProtocolUDP:
		transportLayer = &layers.UDP{
			SrcPort: layers.UDPPort(srcPort),
			DstPort: layers.UDPPort(dstPort),
		}
		udp := transportLayer.(*layers.UDP)
		udp.SetNetworkLayerForChecksum(ipLayer)
	case layers.IPProtocolICMPv4:
		transportLayer = &layers.ICMPv4{
			TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0),
		}
	}

	// 序列化包
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	var err error
	if transportLayer != nil {
		err = gopacket.SerializeLayers(buf, opts, ethLayer, ipLayer, transportLayer, gopacket.Payload(payload))
	} else {
		err = gopacket.SerializeLayers(buf, opts, ethLayer, ipLayer, gopacket.Payload(payload))
	}

	if err != nil {
		log.Printf("[ERROR] Failed to serialize inner IP packet: %v", err)
		return nil
	}

	return buf.Bytes()
}
