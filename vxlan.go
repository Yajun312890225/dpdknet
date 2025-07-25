package dpdknet

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"

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

// EncapsulateVXLAN 封装 VXLAN 包
func (vh *VXLANHandler) EncapsulateVXLAN(innerPayload []byte, innerEthSrc, innerEthDst net.HardwareAddr) ([]byte, error) {
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
		SrcMAC:       innerEthSrc,
		DstMAC:       innerEthDst,
		EthernetType: layers.EthernetTypeIPv4,
	}

	// 序列化所有层
	err := gopacket.SerializeLayers(buf, opts,
		ethLayer,
		ipLayer,
		udpLayer,
		vxlanLayer,
		innerEthLayer,
		gopacket.Payload(innerPayload),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to serialize VXLAN packet: %v", err)
	}

	return buf.Bytes(), nil
}

// DecapsulateVXLAN 解封装 VXLAN 包
func (vh *VXLANHandler) DecapsulateVXLAN(data []byte) (innerPayload []byte, innerEthSrc, innerEthDst net.HardwareAddr, vni uint32, err error) {
	packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)

	// 检查是否包含 VXLAN 层
	vxlanLayer := packet.Layer(layers.LayerTypeVXLAN)
	if vxlanLayer == nil {
		return nil, nil, nil, 0, fmt.Errorf("packet does not contain VXLAN layer")
	}

	vxlan, ok := vxlanLayer.(*layers.VXLAN)
	if !ok {
		return nil, nil, nil, 0, fmt.Errorf("invalid VXLAN layer")
	}

	// 检查 VNI 是否匹配
	if vxlan.VNI != vh.config.VNI {
		return nil, nil, nil, 0, fmt.Errorf("VNI mismatch: expected %d, got %d", vh.config.VNI, vxlan.VNI)
	}

	// 获取内层以太网头
	payload := vxlan.LayerPayload()
	if len(payload) < 14 { // 以太网头最小长度
		return nil, nil, nil, 0, fmt.Errorf("invalid inner ethernet frame")
	}

	// 解析内层以太网头
	innerEthDst = payload[0:6]
	innerEthSrc = payload[6:12]

	// 获取内层有效载荷（跳过以太网头）
	innerPayload = payload[14:]

	return innerPayload, innerEthSrc, innerEthDst, vxlan.VNI, nil
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

// getLocalMAC 获取本地 MAC 地址
func getLocalMAC() net.HardwareAddr {
	// 使用全局的 localMAC
	mac := make(net.HardwareAddr, 6)
	copy(mac, localMAC[:])
	return mac
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
