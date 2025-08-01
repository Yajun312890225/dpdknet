package dpdknet

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

// ErrNotHandled 表示转发器不处理该包
var ErrNotHandled = errors.New("packet not handled by forwarder")

// PacketForwarder 数据包转发器
type PacketForwarder struct {
	tapManager *TapManager
	filters    []PacketFilter
	mutex      sync.RWMutex

	// 统计信息
	processedPackets uint64
	forwardedPackets uint64
	droppedPackets   uint64
}

// PacketFilter 数据包过滤器接口
type PacketFilter interface {
	// ShouldProcess 判断是否应该自己处理这个包
	// 返回true表示自己处理，false表示转发到TAP
	ShouldProcess(packet []byte) bool

	// GetName 获取过滤器名称
	GetName() string
}

// IPFilter IP地址过滤器
type IPFilter struct {
	name           string
	targetIPs      map[string]bool
	targetNetworks []*net.IPNet
}

// VXLANFilter VXLAN包过滤器
type VXLANFilter struct {
	name string
	vni  uint32
}

// PortFilter 端口过滤器
type PortFilter struct {
	name  string
	ports map[uint16]bool
}

// NewPacketForwarder 创建数据包转发器
func NewPacketForwarder() (*PacketForwarder, error) {
	// 创建TAP管理器
	tapManager, err := NewTapManager()
	if err != nil {
		return nil, fmt.Errorf("创建TAP管理器失败: %v", err)
	}

	forwarder := &PacketForwarder{
		tapManager: tapManager,
		filters:    make([]PacketFilter, 0),
	}

	log.Printf("[Forwarder] 创建数据包转发器成功")
	return forwarder, nil
}

// Start 启动转发器
func (pf *PacketForwarder) Start() error {
	return pf.tapManager.Start()
}

// Stop 停止转发器
func (pf *PacketForwarder) Stop() {
	pf.tapManager.Stop()
}

// Close 关闭转发器
func (pf *PacketForwarder) Close() error {
	return pf.tapManager.Close()
}

// AddFilter 添加数据包过滤器
func (pf *PacketForwarder) AddFilter(filter PacketFilter) {
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	pf.filters = append(pf.filters, filter)
	log.Printf("[Forwarder] 添加过滤器: %s", filter.GetName())
}

// ProcessDPDKPacket 处理从DPDK收到的数据包
func (pf *PacketForwarder) ProcessDPDKPacket(packet []byte) error {

	pf.mutex.Lock()
	pf.processedPackets++
	pf.mutex.Unlock()

	// 打印DPDK收到的包的详细信息
	pf.printPacketInfo(packet, "DPDK收到")

	// 应用所有过滤器检查是否需要转发到TAP
	shouldProcess := false

	pf.mutex.RLock()
	for _, filter := range pf.filters {
		if filter.ShouldProcess(packet) {
			log.Printf("[Forwarder] 过滤器 %s 匹配，转发到TAP", filter.GetName())
			shouldProcess = true
			break
		}
	}
	pf.mutex.RUnlock()

	if shouldProcess {
		log.Printf("[Forwarder] 转发包到TAP处理，长度: %d字节", len(packet))
		// 转发到TAP设备（仅匹配过滤器的包）
		err := pf.forwardToTap(packet)
		if err != nil {
			// 转发过程中的其他错误
			return err
		}
		// 转发成功，表示已处理
		return nil
	} else {
		log.Printf("[Forwarder] 无过滤器匹配，让gVisor处理")
		// 过滤器不匹配，让gVisor处理
		return ErrNotHandled
	}
}

// forwardToTap 转发数据包到TAP设备
func (pf *PacketForwarder) forwardToTap(packet []byte) error {

	pf.mutex.Lock()
	pf.forwardedPackets++
	pf.mutex.Unlock()

	// log.Printf("[Forwarder] 转发数据包到normal TAP，长度: %d字节", len(packet))
	return pf.tapManager.SendToNormalTap(packet)
}

// printPacketInfo 打印数据包的详细信息
func (pf *PacketForwarder) printPacketInfo(packet []byte, prefix string) {
	if len(packet) < 14 {
		log.Printf("[%s] 包太短: %d字节", prefix, len(packet))
		return
	}

	// 基本以太网头信息
	dstMAC := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		packet[0], packet[1], packet[2], packet[3], packet[4], packet[5])
	srcMAC := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		packet[6], packet[7], packet[8], packet[9], packet[10], packet[11])
	etherType := uint16(packet[12])<<8 | uint16(packet[13])

	log.Printf("[%s] 以太网头: 长度=%d, 目标MAC=%s, 源MAC=%s, 类型=0x%04x",
		prefix, len(packet), dstMAC, srcMAC, etherType)

	// 如果是IP包，打印IP信息
	if etherType == 0x0800 && len(packet) >= 34 {
		protocol := packet[23]
		srcIP := fmt.Sprintf("%d.%d.%d.%d", packet[26], packet[27], packet[28], packet[29])
		dstIP := fmt.Sprintf("%d.%d.%d.%d", packet[30], packet[31], packet[32], packet[33])

		protocolName := "Unknown"
		switch protocol {
		case 6:
			protocolName = "TCP"
		case 17:
			protocolName = "UDP"
		case 1:
			protocolName = "ICMP"
		}

		log.Printf("[%s] IP头: 协议=%s(%d), 源IP=%s, 目标IP=%s",
			prefix, protocolName, protocol, srcIP, dstIP)

		// 如果是TCP/UDP，打印端口信息
		if (protocol == 6 || protocol == 17) && len(packet) >= 38 {
			srcPort := uint16(packet[34])<<8 | uint16(packet[35])
			dstPort := uint16(packet[36])<<8 | uint16(packet[37])
			log.Printf("[%s] 端口: 源端口=%d, 目标端口=%d", prefix, srcPort, dstPort)
		}
	} else if etherType == 0x0806 {
		log.Printf("[%s] ARP包", prefix)
	}

	// 打印前64字节的十六进制内容
	hexLen := len(packet)
	if hexLen > 64 {
		hexLen = 64
	}

	hexStr := ""
	for i := 0; i < hexLen; i++ {
		hexStr += fmt.Sprintf("%02x", packet[i])
		if (i+1)%16 == 0 {
			hexStr += "\n                    "
		} else if (i+1)%2 == 0 {
			hexStr += " "
		}
	}
	if len(packet) > 64 {
		hexStr += "..."
	}

	log.Printf("[%s] 十六进制: %s", prefix, hexStr)
}

// handleTapPacket 处理从TAP收到的数据包（这个方法现在由TapManager内部处理）
// 保留这个方法用于兼容性，但实际不会被调用
func (pf *PacketForwarder) handleTapPacket(packet []byte) error {
	log.Printf("[Forwarder] 收到来自TAP的数据包，长度: %d字节", len(packet))
	// TAP设备收到的已经是以太网帧，可以直接通过DPDK发送
	return SendRawBytes(packet)
}

// handleARPPacket 处理ARP包
func (pf *PacketForwarder) handleARPPacket(packet []byte) error {
	log.Printf("[Forwarder] 处理ARP包")

	// 使用全局ARP处理器
	if arpHandler := GetGlobalVXLANARPHandler(); arpHandler != nil {
		// 提取ARP数据（跳过以太网头）
		if len(packet) >= 14 {
			arpData := packet[14:]
			return arpHandler.HandleARPPacket(arpData)
		}
	}

	return fmt.Errorf("无法处理ARP包")
}

// GetStats 获取统计信息
func (pf *PacketForwarder) GetStats() (processed, forwarded, dropped uint64) {
	pf.mutex.RLock()
	defer pf.mutex.RUnlock()
	return pf.processedPackets, pf.forwardedPackets, pf.droppedPackets
}

// GetNormalTapName 获取正常TAP设备名称
func (pf *PacketForwarder) GetNormalTapName() string {
	return pf.tapManager.GetNormalTapName()
}

// GetVXLANTapName 获取VXLAN TAP设备名称
func (pf *PacketForwarder) GetVXLANTapName() string {
	return pf.tapManager.GetVXLANTapName()
}

// NewIPFilter 创建IP过滤器
func NewIPFilter(name string, targetIPs []string, targetNetworks []string) *IPFilter {
	filter := &IPFilter{
		name:           name,
		targetIPs:      make(map[string]bool),
		targetNetworks: make([]*net.IPNet, 0),
	}

	// 添加目标IP
	for _, ip := range targetIPs {
		filter.targetIPs[ip] = true
	}

	// 添加目标网络
	for _, network := range targetNetworks {
		_, ipNet, err := net.ParseCIDR(network)
		if err == nil {
			filter.targetNetworks = append(filter.targetNetworks, ipNet)
		}
	}

	return filter
}

// ShouldProcess 实现PacketFilter接口
func (f *IPFilter) ShouldProcess(packet []byte) bool {
	// 检查是否是IPv4包
	if len(packet) < 34 { // 以太网头14 + IP头20
		return false
	}

	// 检查EtherType
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType != 0x0800 {
		return false
	}

	// 提取目标IP
	dstIP := net.IPv4(packet[30], packet[31], packet[32], packet[33])
	dstIPStr := dstIP.String()

	// 检查目标IP列表
	if f.targetIPs[dstIPStr] {
		return true
	}

	// 检查目标网络
	for _, network := range f.targetNetworks {
		if network.Contains(dstIP) {
			return true
		}
	}

	return false
}

// GetName 实现PacketFilter接口
func (f *IPFilter) GetName() string {
	return f.name
}

// NewVXLANFilter 创建VXLAN过滤器
func NewVXLANFilter(name string, vni uint32) *VXLANFilter {
	return &VXLANFilter{
		name: name,
		vni:  vni,
	}
}

// ShouldProcess 实现PacketFilter接口
func (f *VXLANFilter) ShouldProcess(packet []byte) bool {
	return isVXLANPacket(packet)
}

// GetName 实现PacketFilter接口
func (f *VXLANFilter) GetName() string {
	return f.name
}

// NewPortFilter 创建端口过滤器
func NewPortFilter(name string, ports []uint16) *PortFilter {
	filter := &PortFilter{
		name:  name,
		ports: make(map[uint16]bool),
	}

	for _, port := range ports {
		filter.ports[port] = true
	}

	return filter
}

// ShouldProcess 实现PacketFilter接口
func (f *PortFilter) ShouldProcess(packet []byte) bool {
	// 检查UDP/TCP端口
	if len(packet) < 38 { // 以太网头14 + IP头20 + 端口4
		return false
	}

	// 检查EtherType
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType != 0x0800 {
		return false
	}

	// 检查协议
	protocol := packet[23]
	if protocol != 6 && protocol != 17 { // TCP或UDP
		return false
	}

	// 提取源端口和目标端口
	srcPort := uint16(packet[34])<<8 | uint16(packet[35])
	dstPort := uint16(packet[36])<<8 | uint16(packet[37])

	// 检查源端口或目标端口是否匹配（支持双向流量）
	return f.ports[srcPort] || f.ports[dstPort]
}

// GetName 实现PacketFilter接口
func (f *PortFilter) GetName() string {
	return f.name
}

// 辅助函数：检查是否是VXLAN包
func isVXLANPacket(packet []byte) bool {
	// 简化检查：UDP端口4789
	if len(packet) < 36 {
		return false
	}

	// 检查EtherType (IPv4)
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType != 0x0800 {
		return false
	}

	// 检查协议 (UDP)
	protocol := packet[23]
	if protocol != 17 {
		return false
	}

	// 检查目标端口
	dstPort := uint16(packet[36])<<8 | uint16(packet[37])
	return dstPort == 4789
}

// 辅助函数：检查是否是ARP包
func isARPPacket(packet []byte) bool {
	if len(packet) < 14 {
		return false
	}

	// 检查EtherType (ARP)
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	return etherType == 0x0806
}
