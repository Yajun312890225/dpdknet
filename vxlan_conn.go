package dpdknet

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// 特殊错误，表示包已经被 VXLAN 处理
var ErrVXLANPacketHandled = errors.New("packet handled by VXLAN")

// VXLANConn VXLAN 连接，现在通过 TCP/UDP 的 VXLAN 选项创建
// 这个结构保留用于与原有的 VXLAN 地址兼容
type VXLANConn struct {
	localAddr  *VXLANAddr
	remoteAddr *VXLANAddr
	handler    *VXLANHandler
	gvisorConn net.Conn // gVisor 协议栈连接
	closed     bool
	mu         sync.RWMutex
}

// VXLANAddr VXLAN 地址 - 表示内层虚拟网络地址
type VXLANAddr struct {
	IP       net.IP // 内层虚拟 IP（应用层地址）
	Port     int    // 内层端口（应用层端口）
	VNI      uint32 // VXLAN Network Identifier
	Protocol string // 内层协议类型：udp、tcp、icmp
}

func (va *VXLANAddr) Network() string {
	return "vxlan"
}

func (va *VXLANAddr) String() string {
	if va.Protocol != "" {
		return fmt.Sprintf("%s:%d/%s/vni:%d", va.IP.String(), va.Port, va.Protocol, va.VNI)
	}
	return fmt.Sprintf("%s:%d/vni:%d", va.IP.String(), va.Port, va.VNI)
}

// parseProtocol 解析协议字符串为 gopacket 协议类型
func parseProtocol(protocol string) layers.IPProtocol {
	switch protocol {
	case "tcp":
		return layers.IPProtocolTCP
	case "icmp":
		return layers.IPProtocolICMPv4
	case "udp", "":
		return layers.IPProtocolUDP
	default:
		return layers.IPProtocolUDP
	}
}

// ParseVXLANAddr 解析 VXLAN 地址
// 格式: "ip:port/vni:1000" 或 "ip:port" (使用默认 VNI 1000)
func ParseVXLANAddr(addr string) (*VXLANAddr, error) {
	// 简化解析实现
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid VXLAN address format: %v", err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", host)
	}

	// 解析端口
	portNum := 4789 // 默认 VXLAN 端口
	if port != "" {
		if p, err := net.LookupPort("udp", port); err == nil {
			portNum = p
		}
	}

	return &VXLANAddr{
		IP:   ip,
		Port: portNum,
		VNI:  1000, // 默认 VNI
	}, nil
}

// NewVXLANAddr 创建新的 VXLAN 地址
func NewVXLANAddr(ip string, port int, vni uint32, protocol string) (*VXLANAddr, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	if port <= 0 {
		if protocol == "icmp" {
			port = 0 // ICMP 没有端口概念
		} else {
			port = 4789 // 默认端口
		}
	}

	if vni == 0 {
		vni = 1000 // 默认 VNI
	}

	if protocol == "" {
		protocol = "udp" // 默认协议
	}

	return &VXLANAddr{
		IP:       parsedIP,
		Port:     port,
		VNI:      vni,
		Protocol: protocol,
	}, nil
}

// VXLAN 连接管理
var (
	vxlanConnections = make(map[string]*VXLANConn)
	vxlanMutex       sync.RWMutex
)

// Read 实现 net.Conn 接口
func (vc *VXLANConn) Read(b []byte) (n int, err error) {
	vc.mu.RLock()
	if vc.closed {
		vc.mu.RUnlock()
		return 0, fmt.Errorf("connection closed")
	}
	vc.mu.RUnlock()

	// 直接从 gVisor 连接读取
	return vc.gvisorConn.Read(b)
}

// Write 实现 net.Conn 接口
func (vc *VXLANConn) Write(b []byte) (n int, err error) {
	vc.mu.RLock()
	if vc.closed {
		vc.mu.RUnlock()
		return 0, fmt.Errorf("connection closed")
	}
	vc.mu.RUnlock()

	// 直接写入 gVisor 连接
	return vc.gvisorConn.Write(b)
}

// Close 实现 net.Conn 接口
func (vc *VXLANConn) Close() error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if vc.closed {
		return nil
	}

	vc.closed = true

	// 关闭 gVisor 连接
	if vc.gvisorConn != nil {
		vc.gvisorConn.Close()
	}

	// 从全局连接表中移除
	connKey := fmt.Sprintf("%s:%d", vc.remoteAddr.IP.String(), vc.remoteAddr.VNI)
	vxlanMutex.Lock()
	delete(vxlanConnections, connKey)
	vxlanMutex.Unlock()

	log.Printf("[INFO] VXLAN connection closed: %s", connKey)
	return nil
}

// LocalAddr 实现 net.Conn 接口
func (vc *VXLANConn) LocalAddr() net.Addr {
	return vc.localAddr
}

// RemoteAddr 实现 net.Conn 接口
func (vc *VXLANConn) RemoteAddr() net.Addr {
	return vc.remoteAddr
}

// SetDeadline 实现 net.Conn 接口
func (vc *VXLANConn) SetDeadline(t time.Time) error {
	// 简化实现，不支持超时
	return nil
}

// SetReadDeadline 实现 net.Conn 接口
func (vc *VXLANConn) SetReadDeadline(t time.Time) error {
	// 简化实现，不支持超时
	return nil
}

// SetWriteDeadline 实现 net.Conn 接口
func (vc *VXLANConn) SetWriteDeadline(t time.Time) error {
	// 简化实现，不支持超时
	return nil
}

// VXLAN 处理器注册 - 现在由 gVisor 协议栈处理，简化注册逻辑
var (
	vxlanHandlers     = make(map[uint32]*VXLANHandler)
	vxlanHandlerMutex sync.RWMutex
)

// registerVXLANHandler 注册 VXLAN 处理器
func registerVXLANHandler(vni uint32, handler *VXLANHandler) {
	vxlanHandlerMutex.Lock()
	vxlanHandlers[vni] = handler
	vxlanHandlerMutex.Unlock()
}

// unregisterVXLANHandler 注销 VXLAN 处理器
func unregisterVXLANHandler(vni uint32) {
	vxlanHandlerMutex.Lock()
	delete(vxlanHandlers, vni)
	vxlanHandlerMutex.Unlock()
}

// handleIncomingVXLANPacket 处理传入的 VXLAN 包
func handleIncomingVXLANPacket(data []byte) {
	// 检查是否为 VXLAN 包
	if !IsVXLANPacket(data) {
		return
	}

	// 创建临时处理器进行解封装
	tmpHandler := NewVXLANHandler(DefaultVXLANConfig())
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

// 保存 VNI 映射信息，用于回程封装
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

// constructInnerIPPacket 构建内层 IP 包
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

// HandleGVisorOutgoingPacket 处理从 gVisor 发出的包，检查是否需要 VXLAN 封装
// 返回 ErrVXLANPacketHandled 表示包已被 VXLAN 处理并发送
// 返回 nil 表示不需要 VXLAN 处理，应该正常发送
func HandleGVisorOutgoingPacket(data []byte) error {
	// 解析以太网帧
	if len(data) < 14 {
		return nil // 太短，不是有效的以太网帧
	}

	// 检查是否是 IPv4 包
	etherType := uint16(data[12])<<8 | uint16(data[13])
	if etherType != 0x0800 {
		return nil // 不是 IPv4 包
	}

	// 提取 IP 包
	ipPacket := data[14:]
	if len(ipPacket) < 20 {
		return nil // IP 头部太短
	}

	// 解析 IP 头部
	srcIP := net.IP(ipPacket[12:16])
	dstIP := net.IP(ipPacket[16:20])
	protocol := ipPacket[9]

	// 检查是否需要 VXLAN 封装
	vni, needsVXLAN := getVNIForDestination(srcIP, dstIP)
	if !needsVXLAN {
		return nil // 不需要 VXLAN 封装，正常发送
	}

	// 解析传输层端口
	var srcPort, dstPort uint16
	transportOffset := int(ipPacket[0]&0x0F) * 4
	if len(ipPacket) > transportOffset+4 {
		srcPort = uint16(ipPacket[transportOffset])<<8 | uint16(ipPacket[transportOffset+1])
		dstPort = uint16(ipPacket[transportOffset+2])<<8 | uint16(ipPacket[transportOffset+3])
	}

	// 获取 VXLAN 处理器
	vxlanHandlerMutex.RLock()
	handler, exists := vxlanHandlers[vni]
	vxlanHandlerMutex.RUnlock()

	if !exists {
		log.Printf("[DEBUG] No VXLAN handler for VNI %d", vni)
		return nil
	}

	// 使用 VXLAN 处理器进行封装
	vxlanPacket, err := handler.EncapsulateVXLAN(
		ipPacket, srcIP, dstIP, srcPort, dstPort, layers.IPProtocol(protocol))
	if err != nil {
		log.Printf("[ERROR] VXLAN encapsulation failed: %v", err)
		return err
	}

	// 通过 DPDK 发送 VXLAN 包
	if err := SendRawBytes(vxlanPacket); err != nil {
		log.Printf("[ERROR] Failed to send VXLAN packet via DPDK: %v", err)
		return err
	}

	log.Printf("[DEBUG] VXLAN packet sent: src=%s:%d, dst=%s:%d, VNI=%d",
		srcIP, srcPort, dstIP, dstPort, vni)

	// 返回特殊错误，表示包已经被处理
	return ErrVXLANPacketHandled
}
