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
}

// DefaultVXLANConfig 默认 VXLAN 配置
func DefaultVXLANConfig() *VXLANConfig {
	return &VXLANConfig{
		VNI:     1000,
		UDPPort: 4789, // IANA 分配的 VXLAN 端口
	}
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
		SrcMAC:       generateInnerMAC(innerSrcIP), // 基于内层 IP 生成 MAC
		DstMAC:       generateInnerMAC(innerDstIP), // 基于内层 IP 生成 MAC
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

// generateInnerMAC 基于内层 IP 生成虚拟 MAC 地址
func generateInnerMAC(ip net.IP) net.HardwareAddr {
	if ip == nil {
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}

	// 对特定IP使用固定MAC
	if ip.String() == "10.10.10.1" {
		return net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}
	}

	// 使用 IP 的最后4个字节生成 MAC（前缀02:00表示本地管理地址）
	ip4 := ip.To4()
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
	log.Printf("[VXLAN] 解封装包: VNI=%d (期望=%d)", vni, vh.config.VNI)

	// 解析内层包
	innerPacket := gopacket.NewPacket(vxlan.LayerPayload(), layers.LayerTypeEthernet, gopacket.Default)

	// 获取内层 IP 层
	innerIPLayer := innerPacket.Layer(layers.LayerTypeIPv4)
	if innerIPLayer == nil {
		return nil, nil, nil, 0, 0, 0, 0, fmt.Errorf("inner packet does not contain IP layer")
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
	log.Printf("[INFO] VXLAN config updated: VNI=%d, LocalIP=%s, RemoteIP=%s",
		config.VNI, config.LocalIP, config.RemoteIP)
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

	// 先解析包获取实际的 VNI
	actualVNI, err := extractVNIFromPacket(data)
	if err != nil {
		log.Printf("[DEBUG] Failed to extract VNI from VXLAN packet: %v", err)
		return
	}

	// 创建对应 VNI 的处理器进行解封装
	config := DefaultVXLANConfig()
	config.VNI = actualVNI // 使用实际的 VNI
	tmpHandler := NewVXLANHandler(config)

	innerPayload, innerSrcIP, innerDstIP, innerSrcPort, innerDstPort, protocol, vni, err := tmpHandler.DecapsulateVXLAN(data)
	if err != nil {
		log.Printf("[DEBUG] Failed to decapsulate VXLAN packet: %v", err)
		return
	}

	// 保存 VNI 映射信息，用于回程封装
	saveVNIMapping(innerSrcIP, innerDstIP, vni)

	// 构建内层 IP 包
	innerIPPacket := constructInnerIPPacket(innerPayload, innerSrcIP, innerDstIP, innerSrcPort, innerDstPort, protocol)

	// 注入到 gVisor netstack 进行协议栈处理
	if gvisor := GetGVisorNetstack(); gvisor != nil {
		gvisor.InjectDPDKPacket(innerIPPacket)
		log.Printf("[DEBUG] VXLAN packet injected to gVisor: src=%s:%d, dst=%s:%d, VNI=%d",
			innerSrcIP, innerSrcPort, innerDstIP, innerDstPort, vni)
	}
}

// constructInnerIPPacket 构建内层 IP 包

// -------------------- VXLAN VNI 映射相关 --------------------
var (
	vniMappings     = make(map[string]uint32) // IP对 -> VNI
	vniMappingMutex sync.RWMutex
)

// saveVNIMapping 保存 VNI 映射信息
func saveVNIMapping(srcIP, dstIP net.IP, vni uint32) {
	key := fmt.Sprintf("%s-%s", srcIP.String(), dstIP.String())
	vniMappingMutex.Lock()
	vniMappings[key] = vni
	vniMappingMutex.Unlock()
}

// getVNIForDestination 获取目标 IP 对应的 VNI
func getVNIForDestination(srcIP, dstIP net.IP) (uint32, bool) {
	key := fmt.Sprintf("%s-%s", srcIP.String(), dstIP.String())
	vniMappingMutex.RLock()
	vni, exists := vniMappings[key]
	vniMappingMutex.RUnlock()
	return vni, exists
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
		SrcMAC:       generateInnerMAC(srcIP),
		DstMAC:       generateInnerMAC(dstIP),
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
