package dpdknet

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/google/gopacket/layers"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const (
	// 网络接口参数
	defaultMTU   = 1500
	defaultNICID = tcpip.NICID(1)
)

// GVisorNetstack gVisor 网络协议栈封装，实现 DPDK 链路层集成
type GVisorNetstack struct {
	stack   *stack.Stack
	linkEP  *channel.Endpoint
	mu      sync.RWMutex
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// 网络配置
	localIP    net.IP
	localMAC   [6]byte
	subnetMask net.IPMask

	// 统计信息
	stats GVisorStats

	// DPDK 集成
	dpdkCh       chan []byte
	processingWG sync.WaitGroup

	// 作为 DPDK 的链路层处理器
	packetHandler func([]byte) error

	// VXLAN 连接注册
	vxlanConnections map[string]*VXLANConfig // key: "localIP:localPort", value: VXLAN config
	vxlanMutex       sync.RWMutex
}

// GVisorStats gVisor 协议栈统计信息
type GVisorStats struct {
	PacketsReceived   uint64
	PacketsProcessed  uint64
	PacketsDropped    uint64
	PacketsSent       uint64
	TCPConnections    uint64
	UDPConnections    uint64
	ActiveConnections uint64
}

var (
	globalGVisorStack *GVisorNetstack
	gvisorOnce        sync.Once
)

// InitGVisorNetstack 初始化 gVisor 网络协议栈
func InitGVisorNetstack(localIP net.IP, localMAC [6]byte, subnetMask net.IPMask) (*GVisorNetstack, error) {
	var err error
	gvisorOnce.Do(func() {
		globalGVisorStack, err = newGVisorNetstack(localIP, localMAC, subnetMask)
	})
	return globalGVisorStack, err
}

// newGVisorNetstack 创建新的 gVisor 网络协议栈
func newGVisorNetstack(localIP net.IP, localMAC [6]byte, subnetMask net.IPMask) (*GVisorNetstack, error) {
	// 创建协议栈
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	// 创建链路层端点
	linkEP := channel.New(1024, defaultMTU, tcpip.LinkAddress(localMAC[:]))

	// 创建并配置 NIC
	if tcpErr := s.CreateNIC(defaultNICID, linkEP); tcpErr != nil {
		return nil, fmt.Errorf("failed to create NIC: %v", tcpErr)
	}

	// 配置IP地址
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice(localIP.To4()),
			PrefixLen: getPrefixLen(subnetMask),
		},
	}

	if tcpErr := s.AddProtocolAddress(defaultNICID, protocolAddr, stack.AddressProperties{}); tcpErr != nil {
		return nil, fmt.Errorf("failed to add protocol address: %v", tcpErr)
	}

	// 设置默认路由，确保地址和掩码长度一致
	subnet, tcpErr := tcpip.NewSubnet(
		tcpip.AddrFromSlice([]byte{0, 0, 0, 0}),
		tcpip.MaskFromBytes([]byte{0, 0, 0, 0}),
	)
	if tcpErr != nil {
		return nil, fmt.Errorf("failed to create subnet: %v", tcpErr)
	}

	s.SetRouteTable([]tcpip.Route{
		{
			Destination: subnet,
			NIC:         defaultNICID,
		},
	})

	// 启用转发
	if tcpErr := s.SetForwardingDefaultAndAllNICs(ipv4.ProtocolNumber, true); tcpErr != nil {
		log.Printf("[WARNING] Failed to enable forwarding: %v", tcpErr)
	}

	gvs := &GVisorNetstack{
		stack:            s,
		linkEP:           linkEP,
		localIP:          localIP.To4(),
		localMAC:         localMAC,
		subnetMask:       subnetMask,
		stopCh:           make(chan struct{}),
		dpdkCh:           make(chan []byte, 10000),      // 大容量缓冲区
		vxlanConnections: make(map[string]*VXLANConfig), // 初始化 VXLAN 连接映射
	}

	return gvs, nil
}

// getPrefixLen 根据子网掩码计算前缀长度
func getPrefixLen(mask net.IPMask) int {
	prefixLen, _ := mask.Size()
	return prefixLen
}

// Start 启动 gVisor 协议栈处理
func (gvs *GVisorNetstack) Start() error {
	gvs.mu.Lock()
	defer gvs.mu.Unlock()

	if gvs.started {
		return nil
	}

	// 启动数据包处理协程
	gvs.wg.Add(2)
	go gvs.dpdkPacketProcessor()
	go gvs.netstackPacketProcessor()

	gvs.started = true
	return nil
}

// Stop 停止 gVisor 协议栈处理
func (gvs *GVisorNetstack) Stop() {
	gvs.mu.Lock()
	defer gvs.mu.Unlock()

	if !gvs.started {
		return
	}

	close(gvs.stopCh)
	gvs.wg.Wait()

	// 等待所有处理完成
	gvs.processingWG.Wait()

	gvs.started = false
}

// InjectDPDKPacket 从 DPDK 注入数据包到 gVisor 协议栈
func (gvs *GVisorNetstack) InjectDPDKPacket(data []byte) {
	if !gvs.started {
		gvs.stats.PacketsDropped++
		return
	}

	// 非阻塞写入
	select {
	case gvs.dpdkCh <- data:
		gvs.stats.PacketsReceived++
	default:
		gvs.stats.PacketsDropped++
	}
}

// RegisterVXLANConnection 注册 VXLAN 连接配置
func (gvs *GVisorNetstack) RegisterVXLANConnection(localIP net.IP, localPort uint16) {
	gvs.vxlanMutex.Lock()
	defer gvs.vxlanMutex.Unlock()

	key := fmt.Sprintf("%s:%d", localIP.String(), localPort)
	gvs.vxlanConnections[key] = GetGlobalVXLANConfig()
}

// UnregisterVXLANConnection 取消注册 VXLAN 连接配置
func (gvs *GVisorNetstack) UnregisterVXLANConnection(localIP net.IP, localPort uint16) {
	gvs.vxlanMutex.Lock()
	defer gvs.vxlanMutex.Unlock()

	key := fmt.Sprintf("%s:%d", localIP.String(), localPort)
	delete(gvs.vxlanConnections, key)
}

// FindVXLANConfig 查找端口对应的 VXLAN 配置
func (gvs *GVisorNetstack) FindVXLANConfig(localIP net.IP, localPort uint16) *VXLANConfig {
	gvs.vxlanMutex.RLock()
	defer gvs.vxlanMutex.RUnlock()

	key := fmt.Sprintf("%s:%d", localIP.String(), localPort)
	return gvs.vxlanConnections[key]
} // dpdkPacketProcessor 处理来自 DPDK 的数据包
func (gvs *GVisorNetstack) dpdkPacketProcessor() {
	defer gvs.wg.Done()

	for {
		select {
		case <-gvs.stopCh:
			return
		case packet := <-gvs.dpdkCh:
			gvs.processingWG.Add(1)
			go func(data []byte) {
				defer gvs.processingWG.Done()
				gvs.processDPDKPacket(data)
			}(packet)
		}
	}
}

// netstackPacketProcessor 处理来自 netstack 的数据包（发送到 DPDK）
func (gvs *GVisorNetstack) netstackPacketProcessor() {
	defer gvs.wg.Done()

	for {
		select {
		case <-gvs.stopCh:
			return
		default:
			// 读取来自 netstack 的数据包
			pkt := gvs.linkEP.Read()
			if pkt == nil {
				// 没有数据包，短暂休眠
				time.Sleep(time.Microsecond * 100)
				continue
			}

			// 构建完整的以太网帧并发送到 DPDK
			gvs.sendPacketToDPDK(pkt)
			pkt.DecRef()
		}
	}
}

// processDPDKPacket 处理单个来自 DPDK 的数据包
func (gvs *GVisorNetstack) processDPDKPacket(data []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Panic in DPDK packet processing: %v", r)
			gvs.stats.PacketsDropped++
		}
	}()

	// 验证以太网帧最小长度
	if len(data) < 14 {
		gvs.stats.PacketsDropped++
		return
	}

	// 解析以太网头部
	dstMAC := data[0:6]
	etherType := uint16(data[12])<<8 | uint16(data[13])

	// 检查是否是发给我们的包
	isBroadcast := true
	for i := 0; i < 6; i++ {
		if dstMAC[i] != 0xff {
			isBroadcast = false
			break
		}
	}

	isOurMAC := true
	for i := 0; i < 6; i++ {
		if dstMAC[i] != gvs.localMAC[i] {
			isOurMAC = false
			break
		}
	}

	if !isBroadcast && !isOurMAC {
		// 不是发给我们的包
		return
	}

	// 只处理 IP 包
	if etherType != 0x0800 { // IPv4
		gvs.stats.PacketsDropped++
		return
	}

	// 提取 IP 数据包（去除以太网头部）
	ipPacket := data[14:]
	if len(ipPacket) < 20 {
		gvs.stats.PacketsDropped++
		return
	}

	// 创建 buffer 并注入到 gVisor netstack
	var buf buffer.Buffer
	view := buffer.NewViewWithData(ipPacket)
	buf.Append(view)

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buf,
	})

	// 注入到网络协议栈
	gvs.linkEP.InjectInbound(header.IPv4ProtocolNumber, pkt)
	pkt.DecRef()

	gvs.stats.PacketsProcessed++
}

// sendPacketToDPDK 将 netstack 的数据包发送到 DPDK
func (gvs *GVisorNetstack) sendPacketToDPDK(pkt *stack.PacketBuffer) {
	// 构建以太网帧
	payload := pkt.ToBuffer()
	totalSize := payload.Size() + 14 // 以太网头部 14 字节

	// 构建完整的以太网帧
	frame := make([]byte, totalSize)

	// 以太网头部 - 这里需要根据具体的路由信息设置目标MAC
	// 简化处理：使用广播地址，实际使用中应该通过ARP解析
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // 目标 MAC
	copy(frame[6:12], gvs.localMAC[:])                           // 源 MAC
	frame[12] = 0x08                                             // EtherType IPv4 高字节
	frame[13] = 0x00                                             // EtherType IPv4 低字节

	// 复制 IP 数据包
	payloadBytes := payload.Flatten()
	copy(frame[14:], payloadBytes)

	// 检查是否需要 VXLAN 封装
	vxlanConfig := gvs.checkNeedVXLANEncapsulation(payloadBytes)
	if vxlanConfig != nil {
		// 需要 VXLAN 封装，调用专门的处理函数
		err := gvs.handleVXLANOutgoingPacket(frame, vxlanConfig)
		if err != nil {
			log.Printf("[ERROR] Failed to handle VXLAN outgoing packet: %v", err)
			gvs.stats.PacketsDropped++
		} else {
			gvs.stats.PacketsSent++
		}
		return
	}

	// 如果没有进行 VXLAN 处理，正常发送原始帧
	if err := SendRawBytes(frame); err != nil {
		log.Printf("[ERROR] Failed to send packet to DPDK: %v", err)
		gvs.stats.PacketsDropped++
	} else {
		gvs.stats.PacketsSent++
	}
}

// checkNeedVXLANEncapsulation 检查数据包是否需要 VXLAN 封装
func (gvs *GVisorNetstack) checkNeedVXLANEncapsulation(ipPacket []byte) *VXLANConfig {
	if len(ipPacket) < 20 {
		return nil // IP 包太短
	}

	// 解析 IP 头部
	protocol := ipPacket[9]

	var srcPort, dstPort uint16

	// 根据协议提取端口信息
	switch protocol {
	case 6: // TCP
		if len(ipPacket) < 24 {
			return nil
		}
		ipHeaderLen := (ipPacket[0] & 0x0F) * 4
		if len(ipPacket) < int(ipHeaderLen)+4 {
			return nil
		}
		srcPort = uint16(ipPacket[ipHeaderLen])<<8 | uint16(ipPacket[ipHeaderLen+1])
		dstPort = uint16(ipPacket[ipHeaderLen+2])<<8 | uint16(ipPacket[ipHeaderLen+3])

	case 17: // UDP
		if len(ipPacket) < 28 {
			return nil
		}
		ipHeaderLen := (ipPacket[0] & 0x0F) * 4
		if len(ipPacket) < int(ipHeaderLen)+4 {
			return nil
		}
		srcPort = uint16(ipPacket[ipHeaderLen])<<8 | uint16(ipPacket[ipHeaderLen+1])
		dstPort = uint16(ipPacket[ipHeaderLen+2])<<8 | uint16(ipPacket[ipHeaderLen+3])

	case 1: // ICMP
		// ICMP 没有端口概念，使用特殊端口号 0
		srcPort = 0
		dstPort = 0

	default:
		return nil // 其他协议暂不支持
	}

	// 检查源端口是否注册了 VXLAN 配置
	srcIP := net.IP(ipPacket[12:16])
	dstIP := net.IP(ipPacket[16:20])

	if config := gvs.FindVXLANConfig(srcIP, srcPort); config != nil {
		log.Printf("[DEBUG] 匹配源地址 VXLAN 配置: %s:%d", srcIP, srcPort)
		return config
	}

	// 检查目标端口是否注册了 VXLAN 配置
	if config := gvs.FindVXLANConfig(dstIP, dstPort); config != nil {
		log.Printf("[DEBUG] 匹配目标地址 VXLAN 配置: %s:%d", dstIP, dstPort)
		return config
	}

	return nil
}

// handleVXLANOutgoingPacket 处理需要 VXLAN 封装的输出数据包
func (gvs *GVisorNetstack) handleVXLANOutgoingPacket(frame []byte, vxlanConfig *VXLANConfig) error {
	if len(frame) < 14 {
		return fmt.Errorf("frame too short for ethernet header")
	}

	// 提取内层以太网帧（去掉外层以太网头）
	innerFrame := frame[14:]

	// 创建 VXLAN 处理器
	handler := NewVXLANHandler(vxlanConfig)

	// 解析内层 IP 包信息
	if len(innerFrame) < 20 {
		return fmt.Errorf("inner frame too short for IP header")
	}

	srcIP := net.IP(innerFrame[12:16])
	dstIP := net.IP(innerFrame[16:20])
	protocol := innerFrame[9]

	var srcPort, dstPort uint16
	ipHeaderLen := (innerFrame[0] & 0x0F) * 4

	if protocol == 6 || protocol == 17 { // TCP 或 UDP
		if len(innerFrame) >= int(ipHeaderLen)+4 {
			srcPort = uint16(innerFrame[ipHeaderLen])<<8 | uint16(innerFrame[ipHeaderLen+1])
			dstPort = uint16(innerFrame[ipHeaderLen+2])<<8 | uint16(innerFrame[ipHeaderLen+3])
		}
	} else if protocol == 1 { // ICMP
		// ICMP 没有端口概念，使用 0
		srcPort = 0
		dstPort = 0
	}

	// 使用 VXLAN 处理器进行封装
	var vxlanPacket []byte
	var err error

	if protocol == 6 { // TCP
		vxlanPacket, err = handler.EncapsulateVXLAN(
			innerFrame, srcIP, dstIP, srcPort, dstPort,
			layers.IPProtocolTCP,
		)
	} else if protocol == 17 { // UDP
		vxlanPacket, err = handler.EncapsulateVXLAN(
			innerFrame, srcIP, dstIP, srcPort, dstPort,
			layers.IPProtocolUDP,
		)
	} else if protocol == 1 { // ICMP
		vxlanPacket, err = handler.EncapsulateVXLAN(
			innerFrame, srcIP, dstIP, srcPort, dstPort,
			layers.IPProtocolICMPv4,
		)
	} else {
		return fmt.Errorf("unsupported protocol for VXLAN: %d", protocol)
	}

	if err != nil {
		return fmt.Errorf("failed to encapsulate VXLAN: %v", err)
	}

	// 发送 VXLAN 封装后的数据包
	if err := SendRawBytes(vxlanPacket); err != nil {
		return fmt.Errorf("failed to send VXLAN packet: %v", err)
	}

	return nil
}

// getRemoteVTEPForDestination 获取目标 IP 对应的远程 VTEP
func getRemoteVTEPForDestination(dstIP net.IP) net.IP {
	// 这里需要实现 VTEP 映射逻辑
	// 简化实现，可以从环境变量或配置文件获取
	return net.ParseIP(os.Getenv("REMOTE_VTEP_IP"))
}

// CreateTCPListener 创建 TCP 监听器
func (gvs *GVisorNetstack) CreateTCPListener(port uint16) (net.Listener, error) {
	fullAddr := tcpip.FullAddress{
		NIC:  defaultNICID,
		Addr: tcpip.AddrFromSlice(gvs.localIP.To4()),
		Port: port,
	}

	listener, err := gonet.ListenTCP(gvs.stack, fullAddr, ipv4.ProtocolNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP listener: %v", err)
	}

	gvs.stats.TCPConnections++
	return listener, nil
}

// CreateUDPConn 创建 UDP 连接
func (gvs *GVisorNetstack) CreateUDPConn(port uint16) (net.PacketConn, error) {
	fullAddr := tcpip.FullAddress{
		NIC:  defaultNICID,
		Addr: tcpip.AddrFromSlice(gvs.localIP.To4()),
		Port: port,
	}

	conn, err := gonet.DialUDP(gvs.stack, &fullAddr, nil, ipv4.ProtocolNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP connection: %v", err)
	}

	gvs.stats.UDPConnections++
	return conn, nil
}

// CreateUDPConnWithLocalAddr 创建指定本地地址的 UDP 连接
func (gvs *GVisorNetstack) CreateUDPConnWithLocalAddr(localIP net.IP, port uint16) (net.PacketConn, error) {
	// 检查本地IP是否已经在网络栈中配置
	err := gvs.ensureIPAddressConfigured(localIP)
	if err != nil {
		return nil, fmt.Errorf("failed to configure local IP %s: %v", localIP, err)
	}

	fullAddr := tcpip.FullAddress{
		NIC:  defaultNICID,
		Addr: tcpip.AddrFromSlice(localIP.To4()),
		Port: port,
	}

	conn, err := gonet.DialUDP(gvs.stack, &fullAddr, nil, ipv4.ProtocolNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP connection with local addr %s:%d: %v", localIP, port, err)
	}

	gvs.stats.UDPConnections++
	return conn, nil
}

// ensureIPAddressConfigured 确保指定的IP地址在gVisor网络栈中已配置
func (gvs *GVisorNetstack) ensureIPAddressConfigured(ip net.IP) error {
	// 检查IP是否已经配置
	addrs := gvs.stack.AllAddresses()
	for nicID, nicAddrs := range addrs {
		if nicID == defaultNICID {
			for _, addr := range nicAddrs {
				if addr.AddressWithPrefix.Address == tcpip.AddrFromSlice(ip.To4()) {
					// IP已经配置，直接返回
					return nil
				}
			}
		}
	}

	// IP未配置，添加为辅助地址
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice(ip.To4()),
			PrefixLen: 24, // 使用/24子网掩码
		},
	}

	if tcpErr := gvs.stack.AddProtocolAddress(defaultNICID, protocolAddr, stack.AddressProperties{}); tcpErr != nil {
		return fmt.Errorf("failed to add IP address %s: %v", ip, tcpErr)
	}

	log.Printf("[INFO] Added virtual IP address %s to gVisor network stack", ip)
	return nil
}

// CreateTCPConn 创建 TCP 客户端连接
func (gvs *GVisorNetstack) CreateTCPConn(localAddr, remoteAddr *net.TCPAddr) (net.Conn, error) {
	remoteFullAddr := tcpip.FullAddress{
		NIC:  defaultNICID,
		Addr: tcpip.AddrFromSlice(remoteAddr.IP.To4()),
		Port: uint16(remoteAddr.Port),
	}

	conn, err := gonet.DialTCP(gvs.stack, remoteFullAddr, ipv4.ProtocolNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP connection: %v", err)
	}

	gvs.stats.TCPConnections++
	gvs.stats.ActiveConnections++
	return conn, nil
}

// CreateTCPConnWithTimeout 创建带超时的 TCP 客户端连接
func (gvs *GVisorNetstack) CreateTCPConnWithTimeout(localAddr, remoteAddr *net.TCPAddr, timeout time.Duration) (net.Conn, error) {
	// 创建一个通道来接收连接结果
	resultCh := make(chan struct {
		conn net.Conn
		err  error
	}, 1)

	go func() {
		conn, err := gvs.CreateTCPConn(localAddr, remoteAddr)
		resultCh <- struct {
			conn net.Conn
			err  error
		}{conn, err}
	}()

	select {
	case result := <-resultCh:
		return result.conn, result.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("connection timeout after %v", timeout)
	}
}

// CreateTCPConnWithLocalAddr 创建指定本地地址的 TCP 连接
func (gvs *GVisorNetstack) CreateTCPConnWithLocalAddr(localIP net.IP, localPort uint16, remoteAddr *net.TCPAddr) (net.Conn, error) {
	// 检查本地IP是否已经在网络栈中配置
	err := gvs.ensureIPAddressConfigured(localIP)
	if err != nil {
		return nil, fmt.Errorf("failed to configure local IP %s: %v", localIP, err)
	}

	localFullAddr := tcpip.FullAddress{
		NIC:  defaultNICID,
		Addr: tcpip.AddrFromSlice(localIP.To4()),
		Port: localPort,
	}

	remoteFullAddr := tcpip.FullAddress{
		NIC:  defaultNICID,
		Addr: tcpip.AddrFromSlice(remoteAddr.IP.To4()),
		Port: uint16(remoteAddr.Port),
	}

	conn, err := gonet.DialTCPWithBind(context.Background(), gvs.stack, localFullAddr, remoteFullAddr, ipv4.ProtocolNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP connection with local addr %s:%d: %v", localIP, localPort, err)
	}

	gvs.stats.TCPConnections++
	gvs.stats.ActiveConnections++
	return conn, nil
}

// GetStats 获取统计信息
func (gvs *GVisorNetstack) GetStats() GVisorStats {
	gvs.mu.RLock()
	defer gvs.mu.RUnlock()
	return gvs.stats
}

// PrintStats 打印统计信息
func (gvs *GVisorNetstack) PrintStats() {
	// 统计信息可通过 GetStats() 方法获取
}

// GetGVisorNetstack 获取全局 gVisor 协议栈实例
func GetGVisorNetstack() *GVisorNetstack {
	return globalGVisorStack
}

// 兼容性函数：与现有 DPDK 集成
func IntegrateGVisorWithDPDK(localIP net.IP, localMAC [6]byte) error {
	// 默认子网掩码
	subnetMask := net.IPv4Mask(255, 255, 255, 0)

	// 初始化 gVisor netstack
	gvs, err := InitGVisorNetstack(localIP, localMAC, subnetMask)
	if err != nil {
		return fmt.Errorf("failed to initialize gVisor netstack: %v", err)
	}

	// 启动协议栈
	if err := gvs.Start(); err != nil {
		return fmt.Errorf("failed to start gVisor netstack: %v", err)
	}

	return nil
}

// CreateGVisorTCPListener 通过 gVisor netstack 创建 TCP 监听器
func CreateGVisorTCPListener(port uint16) (net.Listener, error) {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil, fmt.Errorf("gVisor netstack not initialized")
	}
	return gvs.CreateTCPListener(port)
}

// CreateGVisorUDPConn 通过 gVisor netstack 创建 UDP 连接
func CreateGVisorUDPConn(port uint16) (net.PacketConn, error) {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil, fmt.Errorf("gVisor netstack not initialized")
	}
	return gvs.CreateUDPConn(port)
}

// CreateGVisorUDPConnWithLocalAddr 通过 gVisor netstack 创建指定本地地址的 UDP 连接
func CreateGVisorUDPConnWithLocalAddr(localIP net.IP, port uint16) (net.PacketConn, error) {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil, fmt.Errorf("gVisor netstack not initialized")
	}
	return gvs.CreateUDPConnWithLocalAddr(localIP, port)
}

// CreateGVisorTCPConn 通过 gVisor netstack 创建 TCP 客户端连接
func CreateGVisorTCPConn(localAddr, remoteAddr *net.TCPAddr) (net.Conn, error) {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil, fmt.Errorf("gVisor netstack not initialized")
	}
	return gvs.CreateTCPConn(localAddr, remoteAddr)
}

// CreateGVisorTCPConnWithTimeout 通过 gVisor netstack 创建带超时的 TCP 客户端连接
func CreateGVisorTCPConnWithTimeout(localAddr, remoteAddr *net.TCPAddr, timeout time.Duration) (net.Conn, error) {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil, fmt.Errorf("gVisor netstack not initialized")
	}
	return gvs.CreateTCPConnWithTimeout(localAddr, remoteAddr, timeout)
}

// CreateGVisorTCPConnWithLocalAddr 通过 gVisor netstack 创建指定本地地址的 TCP 连接
func CreateGVisorTCPConnWithLocalAddr(localIP net.IP, localPort uint16, remoteAddr *net.TCPAddr) (net.Conn, error) {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil, fmt.Errorf("gVisor netstack not initialized")
	}
	return gvs.CreateTCPConnWithLocalAddr(localIP, localPort, remoteAddr)
}

// ProcessDPDKPacket 处理来自 DPDK 的数据包（全局入口）
func ProcessDPDKPacket(data []byte) error {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return fmt.Errorf("gVisor netstack not initialized")
	}

	gvs.InjectDPDKPacket(data)
	return nil
}

// GetNetworkStats 获取网络统计信息
func GetNetworkStats() *GVisorStats {
	gvs := GetGVisorNetstack()
	if gvs == nil {
		return nil
	}

	stats := gvs.GetStats()
	return &stats
}

// SetPacketHandler 设置数据包处理器（可选，用于自定义处理）
func (gvs *GVisorNetstack) SetPacketHandler(handler func([]byte) error) {
	gvs.mu.Lock()
	defer gvs.mu.Unlock()
	gvs.packetHandler = handler
}
