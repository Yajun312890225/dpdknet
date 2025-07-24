package dpdknet

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

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

// GVisorNetstack gVisor 网络协议栈封装
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

	// 设置默认路由
	subnet, tcpErr := tcpip.NewSubnet(tcpip.AddrFromSlice(net.IPv4zero), tcpip.MaskFromBytes(net.IPv4Mask(0, 0, 0, 0)))
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
		stack:      s,
		linkEP:     linkEP,
		localIP:    localIP.To4(),
		localMAC:   localMAC,
		subnetMask: subnetMask,
		stopCh:     make(chan struct{}),
		dpdkCh:     make(chan []byte, 10000), // 大容量缓冲区
	}

	log.Printf("[INFO] GVisor Netstack initialized with IP %s/%d, MAC %02x:%02x:%02x:%02x:%02x:%02x",
		localIP.String(), getPrefixLen(subnetMask),
		localMAC[0], localMAC[1], localMAC[2], localMAC[3], localMAC[4], localMAC[5])

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
	log.Printf("[INFO] GVisor Netstack started")
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
	log.Printf("[INFO] GVisor Netstack stopped")
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
		log.Printf("[WARNING] DPDK packet dropped due to full buffer")
	}
}

// dpdkPacketProcessor 处理来自 DPDK 的数据包
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
	// 简化处理：使用广播地址
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // 目标 MAC
	copy(frame[6:12], gvs.localMAC[:])                           // 源 MAC
	frame[12] = 0x08                                             // EtherType IPv4 高字节
	frame[13] = 0x00                                             // EtherType IPv4 低字节

	// 复制 IP 数据包
	payloadBytes := payload.Flatten()
	copy(frame[14:], payloadBytes)

	// 发送到 DPDK
	if err := SendRawBytes(frame); err != nil {
		log.Printf("[ERROR] Failed to send packet to DPDK: %v", err)
		gvs.stats.PacketsDropped++
	} else {
		gvs.stats.PacketsSent++
	}
}

// CreateTCPListener 创建 TCP 监听器
func (gvs *GVisorNetstack) CreateTCPListener(port uint16) (net.Listener, error) {
	addr := &net.TCPAddr{
		IP:   gvs.localIP,
		Port: int(port),
	}

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
	log.Printf("[INFO] TCP listener created on %s", addr.String())
	return listener, nil
}

// CreateUDPConn 创建 UDP 连接
func (gvs *GVisorNetstack) CreateUDPConn(port uint16) (net.PacketConn, error) {
	addr := &net.UDPAddr{
		IP:   gvs.localIP,
		Port: int(port),
	}

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
	log.Printf("[INFO] UDP connection created on %s", addr.String())
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
	stats := gvs.GetStats()
	log.Printf("[INFO] GVisor Netstack Stats:")
	log.Printf("  Packets Received: %d", stats.PacketsReceived)
	log.Printf("  Packets Processed: %d", stats.PacketsProcessed)
	log.Printf("  Packets Dropped: %d", stats.PacketsDropped)
	log.Printf("  Packets Sent: %d", stats.PacketsSent)
	log.Printf("  TCP Connections: %d", stats.TCPConnections)
	log.Printf("  UDP Connections: %d", stats.UDPConnections)
	log.Printf("  Active Connections: %d", stats.ActiveConnections)
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

	log.Printf("[INFO] GVisor netstack integrated with DPDK successfully")
	return nil
}
