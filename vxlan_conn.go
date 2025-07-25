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

// VXLANConn VXLAN 连接，基于 gVisor 协议栈 + DPDK 数据路径
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

// DialVXLAN 创建 VXLAN 连接
func DialVXLAN(network string, localAddr, remoteAddr *VXLANAddr) (*VXLANConn, error) {
	if remoteAddr == nil {
		return nil, fmt.Errorf("remote address cannot be nil")
	}

	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, fmt.Errorf("failed to initialize DPDK network: %v", err)
	}

	// 从环境变量获取外层 VTEP 地址
	localVTEP := getLocalIPFromEnv()
	remoteVTEP := getRemoteVTEPFromEnv() // 需要新增这个函数

	// 创建 VXLAN 配置 - 外层是 VTEP 地址，内层是 VXLANAddr
	config := &VXLANConfig{
		VNI:       remoteAddr.VNI,
		LocalIP:   localVTEP,  // 外层本地 VTEP IP
		RemoteIP:  remoteVTEP, // 外层远程 VTEP IP
		UDPPort:   4789,       // 外层 VXLAN UDP 端口固定 4789
		LocalMAC:  getLocalMAC(),
		RemoteMAC: getRemoteMACFromARP(remoteVTEP), // 基于 VTEP IP 查询 MAC
	}

	// 通过 gVisor 创建内层连接
	var gvisorConn net.Conn
	var err error

	switch remoteAddr.Protocol {
	case "tcp":
		localTCPAddr := &net.TCPAddr{IP: localAddr.IP, Port: localAddr.Port}
		remoteTCPAddr := &net.TCPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port}
		gvisorConn, err = CreateGVisorTCPConn(localTCPAddr, remoteTCPAddr)
	case "udp":
		// UDP 连接稍后实现
		return nil, fmt.Errorf("UDP VXLAN connections not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", remoteAddr.Protocol)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create gVisor connection: %v", err)
	}

	vxlanConn := &VXLANConn{
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		handler:    NewVXLANHandler(config),
		gvisorConn: gvisorConn,
		closed:     false,
	}

	// 注册 VXLAN 连接到全局处理器
	connKey := fmt.Sprintf("%s:%d", remoteAddr.IP.String(), remoteAddr.VNI)
	vxlanMutex.Lock()
	vxlanConnections[connKey] = vxlanConn
	vxlanMutex.Unlock()

	// 启动 VXLAN 包处理 goroutine
	go vxlanConn.startPacketProcessor()

	log.Printf("[INFO] VXLAN connection established: local_vtep=%s, remote_vtep=%s, inner_local=%s, inner_remote=%s, VNI=%d",
		localVTEP, remoteVTEP, localAddr, remoteAddr, config.VNI)

	return vxlanConn, nil
}

// startPacketProcessor 启动包处理器
func (vc *VXLANConn) startPacketProcessor() {
	// 从 gVisor 连接读取数据，封装为 VXLAN 包，通过 DPDK 发送
	go func() {
		buffer := make([]byte, 1500)
		for {
			vc.mu.RLock()
			if vc.closed {
				vc.mu.RUnlock()
				return
			}
			vc.mu.RUnlock()

			// 从 gVisor 连接读取数据
			n, err := vc.gvisorConn.Read(buffer)
			if err != nil {
				log.Printf("[DEBUG] gVisor connection read error: %v", err)
				return
			}

			// 封装 VXLAN 包并通过 DPDK 发送
			if err := vc.sendVXLANPacket(buffer[:n]); err != nil {
				log.Printf("[ERROR] Failed to send VXLAN packet: %v", err)
			}
		}
	}()
}

// sendVXLANPacket 发送 VXLAN 包
func (vc *VXLANConn) sendVXLANPacket(data []byte) error {
	// 使用内层网络地址信息
	innerSrcIP := vc.localAddr.IP
	innerDstIP := vc.remoteAddr.IP
	innerSrcPort := uint16(vc.localAddr.Port)
	innerDstPort := uint16(vc.remoteAddr.Port)
	protocol := parseProtocol(vc.remoteAddr.Protocol)

	// 封装 VXLAN 包，包含完整的内层网络栈
	vxlanPacket, err := vc.handler.EncapsulateVXLAN(data, innerSrcIP, innerDstIP, innerSrcPort, innerDstPort, protocol)
	if err != nil {
		return fmt.Errorf("VXLAN encapsulation failed: %v", err)
	}

	// 通过 DPDK 发送原始字节
	return SendRawBytes(vxlanPacket)
}

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

// VXLANListener VXLAN 监听器，基于 gVisor 协议栈 + DPDK 数据路径
type VXLANListener struct {
	addr           *VXLANAddr
	handler        *VXLANHandler
	gvisorListener net.Listener // gVisor 协议栈监听器
	closed         bool
	mu             sync.RWMutex
}

// ListenVXLAN 创建 VXLAN 监听器
func ListenVXLAN(network string, addr *VXLANAddr) (*VXLANListener, error) {
	if addr == nil {
		return nil, fmt.Errorf("address cannot be nil")
	}

	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, fmt.Errorf("failed to initialize DPDK network: %v", err)
	}

	// 从环境变量获取本地 VTEP 地址（外层）
	localVTEP := getLocalIPFromEnv()

	// 创建 VXLAN 配置 - addr 是内层地址，VTEP 是外层地址
	config := &VXLANConfig{
		VNI:       addr.VNI,
		LocalIP:   localVTEP, // 外层本地 VTEP IP
		UDPPort:   4789,      // 外层固定端口 4789
		LocalMAC:  getLocalMAC(),
		RemoteMAC: nil, // 监听器不需要远程 MAC
	}

	// 通过 gVisor 创建内层监听器
	var gvisorListener net.Listener
	var err error

	switch addr.Protocol {
	case "tcp":
		gvisorListener, err = CreateGVisorTCPListener(uint16(addr.Port))
	case "udp":
		// UDP 监听器稍后实现
		return nil, fmt.Errorf("UDP VXLAN listeners not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", addr.Protocol)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create gVisor listener: %v", err)
	}

	listener := &VXLANListener{
		addr:           addr,
		handler:        NewVXLANHandler(config),
		gvisorListener: gvisorListener,
		closed:         false,
	}

	// 注册 VXLAN 处理器到全局包处理器
	registerVXLANHandler(addr.VNI, listener)

	log.Printf("[INFO] VXLAN listener started: vtep=%s, inner_addr=%s, VNI=%d", localVTEP, addr, addr.VNI)
	return listener, nil
}

// Accept 实现 net.Listener 接口
func (vl *VXLANListener) Accept() (net.Conn, error) {
	vl.mu.RLock()
	if vl.closed {
		vl.mu.RUnlock()
		return nil, fmt.Errorf("listener closed")
	}
	vl.mu.RUnlock()

	// 从 gVisor 监听器接受连接
	gvisorConn, err := vl.gvisorListener.Accept()
	if err != nil {
		return nil, err
	}

	// 包装为 VXLANConn
	vxlanConn := &VXLANConn{
		localAddr:  vl.addr,
		handler:    vl.handler,
		gvisorConn: gvisorConn,
		closed:     false,
	}

	// 尝试获取远程地址信息
	if remoteAddr := gvisorConn.RemoteAddr(); remoteAddr != nil {
		switch addr := remoteAddr.(type) {
		case *net.TCPAddr:
			vxlanConn.remoteAddr = &VXLANAddr{
				IP:       addr.IP,
				Port:     addr.Port,
				VNI:      vl.addr.VNI,
				Protocol: vl.addr.Protocol,
			}
		case *net.UDPAddr:
			vxlanConn.remoteAddr = &VXLANAddr{
				IP:       addr.IP,
				Port:     addr.Port,
				VNI:      vl.addr.VNI,
				Protocol: vl.addr.Protocol,
			}
		}
	}

	// 启动包处理器
	go vxlanConn.startPacketProcessor()

	return vxlanConn, nil
}

// Close 实现 net.Listener 接口
func (vl *VXLANListener) Close() error {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	if vl.closed {
		return nil
	}

	vl.closed = true

	// 关闭 gVisor 监听器
	if vl.gvisorListener != nil {
		vl.gvisorListener.Close()
	}

	// 注销 VXLAN 处理器
	unregisterVXLANHandler(vl.addr.VNI)

	log.Printf("[INFO] VXLAN listener closed: VNI=%d", vl.addr.VNI)
	return nil
}

// Addr 实现 net.Listener 接口
func (vl *VXLANListener) Addr() net.Addr {
	return vl.addr
}

// VXLAN 处理器注册
var (
	vxlanHandlers     = make(map[uint32]*VXLANListener)
	vxlanHandlerMutex sync.RWMutex
)

// registerVXLANHandler 注册 VXLAN 处理器
func registerVXLANHandler(vni uint32, listener *VXLANListener) {
	vxlanHandlerMutex.Lock()
	vxlanHandlers[vni] = listener
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
	listener, exists := vxlanHandlers[vni]
	vxlanHandlerMutex.RUnlock()

	if !exists {
		log.Printf("[DEBUG] No VXLAN handler for VNI %d", vni)
		return nil
	}

	// 使用监听器的处理器进行 VXLAN 封装
	vxlanPacket, err := listener.handler.EncapsulateVXLAN(
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
