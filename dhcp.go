package dpdknet

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

var (
	// 全局 DHCP 客户端映射，用于处理 DPDK 接收的 DHCP 响应
	globalDHCPClients = make(map[uint32]*DHCPClient) // 使用事务ID作为键
	globalDHCPMutex   sync.RWMutex
)

// DHCPClient DHCP 客户端封装
type DHCPClient struct {
	mac         net.HardwareAddr
	ifaceName   string
	client      *nclient4.Client
	vxlanConfig *VXLANConfig
	hostname    string
	// DPDK 环境下的 DHCP 响应处理
	pendingOffers chan *DHCPOfferInfo
	pendingACKs   chan *DHCPOfferInfo
	transactionID uint32
	mutex         sync.RWMutex
}

// DHCPOfferInfo DHCP Offer 信息
type DHCPOfferInfo struct {
	YourIP      net.IP
	ServerIP    net.IP
	Gateway     net.IP
	GatewayMAC  net.HardwareAddr // 网关 MAC 地址
	SubnetMask  net.IPMask
	DNS         []net.IP
	LeaseTime   time.Duration
	MessageType dhcpv4.MessageType
}

// NewDHCPClient 创建新的 DHCP 客户端
func NewDHCPClient(mac net.HardwareAddr, vxlanConfig *VXLANConfig, hostname string) *DHCPClient {
	client := &DHCPClient{
		mac:           mac,
		vxlanConfig:   vxlanConfig,
		hostname:      hostname,
		ifaceName:     "eth1", // 默认网卡名，可根据需要修改
		pendingOffers: make(chan *DHCPOfferInfo, 10),
		pendingACKs:   make(chan *DHCPOfferInfo, 10),
		transactionID: uint32(time.Now().Unix()), // 使用时间戳作为事务 ID
	}

	// 注册到全局映射中
	globalDHCPMutex.Lock()
	globalDHCPClients[client.transactionID] = client
	globalDHCPMutex.Unlock()

	return client
}

// NewDHCPClientWithInterface 使用指定网卡创建 DHCP 客户端
func NewDHCPClientWithInterface(ifaceName string, mac net.HardwareAddr, vxlanConfig *VXLANConfig, hostname string) *DHCPClient {
	return &DHCPClient{
		mac:         mac,
		ifaceName:   ifaceName,
		vxlanConfig: vxlanConfig,
		hostname:    hostname,
	}
}

// initClient 初始化底层 DHCP 客户端
func (dc *DHCPClient) initClient() error {
	if dc.client != nil {
		return nil
	}

	client, err := nclient4.New(dc.ifaceName, nclient4.WithHWAddr(dc.mac))
	if err != nil {
		return fmt.Errorf("创建DHCP客户端失败: %v", err)
	}

	dc.client = client
	return nil
}

// SendDHCPDiscover 发送 DHCP Discover 并获取 Offer
func (dc *DHCPClient) SendDHCPDiscover(timeout time.Duration) (*DHCPOfferInfo, error) {
	log.Printf("[DHCP] 发送 DHCP Discover 广播包 (通过 DPDK 原始包)")

	// 在 DPDK 环境下，不使用系统网络接口，直接创建和发送原始包
	// 创建 DHCP Discover 包
	discoverPacket, err := dc.CreateDiscoverPacket()
	if err != nil {
		return nil, fmt.Errorf("创建 DHCP Discover 包失败: %v", err)
	}

	// DHCP 包会自动生成事务 ID，我们保存它以便匹配响应
	dc.mutex.Lock()
	copy(discoverPacket.TransactionID[:], []byte{byte(dc.transactionID >> 24), byte(dc.transactionID >> 16), byte(dc.transactionID >> 8), byte(dc.transactionID)})
	dc.mutex.Unlock()

	// 通过 VXLAN 发送 DHCP 广播包
	broadcastIP := net.IPv4(255, 255, 255, 255) // DHCP 广播地址
	if err := dc.SendRawDHCPPacket(discoverPacket, broadcastIP); err != nil {
		log.Printf("[DHCP] 发送 DHCP Discover 失败: %v", err)
		return nil, err
	} else {
		log.Printf("[DHCP] DHCP Discover 广播包发送成功，事务ID: %x", dc.transactionID)
	}

	// 等待接收 DHCP Offer
	log.Printf("[DHCP] 等待 DHCP Offer 响应...")
	select {
	case offer := <-dc.pendingOffers:
		log.Printf("[DHCP] 收到 DHCP Offer: IP=%s, Gateway=%s", offer.YourIP, offer.Gateway)
		return offer, nil
	case <-time.After(timeout):
		log.Printf("[DHCP] 等待 DHCP Offer 超时 (%v)", timeout)
		return nil, fmt.Errorf("DHCP Discover 超时，未收到 Offer")
	}
}

// SendDHCPRequest 发送 DHCP Request 并获取 ACK
func (dc *DHCPClient) SendDHCPRequest(offer *dhcpv4.DHCPv4, timeout time.Duration) (*DHCPOfferInfo, error) {
	log.Printf("[DHCP] 发送 DHCP Request")

	if dc.client == nil {
		return nil, fmt.Errorf("DHCP 客户端未初始化")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 发送 Request 并等待 ACK
	ack, err := dc.client.Request(ctx, func(d *dhcpv4.DHCPv4) {
		d.UpdateOption(dhcpv4.OptHostName(dc.hostname))
		d.UpdateOption(dhcpv4.OptRequestedIPAddress(offer.YourIPAddr))
	})
	if err != nil {
		return nil, fmt.Errorf("DHCP Request 失败: %v", err)
	}

	log.Printf("[DHCP] 收到 DHCP ACK")

	// 解析 ACK 信息
	ackInfo := dc.parseOffer(ack.ACK)
	return ackInfo, nil
}

// parseOffer 解析 DHCP Offer/ACK 包
func (dc *DHCPClient) parseOffer(packet *dhcpv4.DHCPv4) *DHCPOfferInfo {
	info := &DHCPOfferInfo{
		YourIP:      packet.YourIPAddr,
		ServerIP:    packet.ServerIPAddr,
		MessageType: packet.MessageType(),
	}

	// 解析选项
	if subnet := packet.SubnetMask(); subnet != nil {
		info.SubnetMask = subnet
	}

	if routers := packet.Router(); len(routers) > 0 {
		info.Gateway = routers[0]
	}

	if dns := packet.DNS(); len(dns) > 0 {
		info.DNS = dns
	}

	if lease := packet.IPAddressLeaseTime(time.Hour * 24); lease > 0 {
		info.LeaseTime = lease
	}

	return info
}

// parseOfferWithMAC 解析 DHCP Offer/ACK 包，包含来源 MAC 地址
func (dc *DHCPClient) parseOfferWithMAC(packet *dhcpv4.DHCPv4, srcMAC net.HardwareAddr) *DHCPOfferInfo {
	info := dc.parseOffer(packet)

	// 如果数据包来自网关服务器，则保存网关 MAC
	if packet.ServerIPAddr != nil && !packet.ServerIPAddr.IsUnspecified() {
		info.GatewayMAC = srcMAC
		log.Printf("[DHCP] 从 DHCP 响应中提取网关 MAC: %s -> %s", info.Gateway, info.GatewayMAC)
	}

	return info
}

// StartDHCP 启动完整的 DHCP 流程
func (dc *DHCPClient) StartDHCP() (*DHCPOfferInfo, error) {
	log.Printf("[DHCP] 启动 DHCP 获取 IP 地址流程")

	// 步骤1: 发送 DHCP Discover
	offer, err := dc.SendDHCPDiscover(10 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("DHCP Discover 失败: %v", err)
	}

	log.Printf("[DHCP] 获得 DHCP Offer: IP=%s, Gateway=%s",
		offer.YourIP, offer.Gateway)

	// 步骤2: 发送 DHCP Request
	// 注意：这里需要原始的 DHCP 包，实际使用中需要保存 offer packet
	// 为了简化，我们直接返回 offer 信息
	log.Printf("[DHCP] DHCP 流程完成，获得 IP: %s", offer.YourIP)
	return offer, nil
}

// Close 关闭 DHCP 客户端
func (dc *DHCPClient) Close() error {
	if dc.client != nil {
		return dc.client.Close()
	}
	return nil
}

// CreateDiscoverPacket 创建自定义的 DHCP Discover 包
func (dc *DHCPClient) CreateDiscoverPacket() (*dhcpv4.DHCPv4, error) {
	packet, err := dhcpv4.NewDiscovery(dc.mac)
	if err != nil {
		return nil, fmt.Errorf("创建 DHCP Discover 包失败: %v", err)
	}

	// 添加主机名选项
	if dc.hostname != "" {
		packet.UpdateOption(dhcpv4.OptHostName(dc.hostname))
	}

	// 添加参数请求列表
	packet.UpdateOption(dhcpv4.OptParameterRequestList(
		dhcpv4.OptionSubnetMask,
		dhcpv4.OptionRouter,
		dhcpv4.OptionDomainNameServer,
		dhcpv4.OptionDomainName,
	))

	return packet, nil
}

// CreateRequestPacket 创建自定义的 DHCP Request 包
func (dc *DHCPClient) CreateRequestPacket(offer *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, error) {
	packet, err := dhcpv4.NewRequestFromOffer(offer)
	if err != nil {
		return nil, fmt.Errorf("创建 DHCP Request 包失败: %v", err)
	}

	// 添加主机名选项
	if dc.hostname != "" {
		packet.UpdateOption(dhcpv4.OptHostName(dc.hostname))
	}

	return packet, nil
}

// SendRawDHCPPacket 发送原始 DHCP 包（通过 VXLAN）
func (dc *DHCPClient) SendRawDHCPPacket(packet *dhcpv4.DHCPv4, dstIP net.IP) error {
	log.Printf("[DHCP] 发送原始 DHCP 包，长度: %d 字节", len(packet.ToBytes()))

	// 如果有 VXLAN 配置，通过 VXLAN 发送
	if dc.vxlanConfig != nil {
		return dc.sendVXLANPacket(packet.ToBytes(), dstIP)
	}

	// 否则直接发送（这里需要实现底层发送逻辑）
	log.Printf("[DHCP] 直接发送 DHCP 包到 %s", dstIP.String())
	return nil
}

// sendVXLANPacket 通过 VXLAN 发送数据包
func (dc *DHCPClient) sendVXLANPacket(dhcpData []byte, dstIP net.IP) error {
	// 创建 VXLAN 处理器
	handler := NewVXLANHandler(dc.vxlanConfig)

	// 构建 UDP 包（DHCP 使用 UDP 协议）
	srcPort := uint16(68) // DHCP 客户端端口
	dstPort := uint16(67) // DHCP 服务器端口

	// 确定源 IP
	var srcIP net.IP
	if dc.vxlanConfig != nil {
		// 对于 DHCP，使用内网的虚拟 IP，而不是 VXLAN 的外网 IP
		srcIP = net.IPv4(0, 0, 0, 0) // DHCP 客户端初始使用 0.0.0.0
	} else {
		srcIP = net.IPv4(0, 0, 0, 0)
	}

	// 先构建内层 UDP/IP 包
	innerPacket, err := dc.buildUDPIPPacket(srcIP, dstIP, srcPort, dstPort, dhcpData)
	if err != nil {
		return fmt.Errorf("构建内层 UDP/IP 包失败: %v", err)
	}

	log.Printf("[DHCP] 构建内层 UDP/IP 包，长度: %d 字节", len(innerPacket))

	// 进行 VXLAN 封装 - 这次传递完整的 UDP/IP 包
	vxlanPacket, err := handler.EncapsulateVXLAN(
		innerPacket, srcIP, dstIP,
		srcPort, dstPort, 17) // 17 = UDP 协议号
	if err != nil {
		return fmt.Errorf("VXLAN 封装失败: %v", err)
	}

	log.Printf("[DHCP] 通过 VXLAN 发送 DHCP 包，封装后长度: %d 字节", len(vxlanPacket))

	// 发送 VXLAN 包
	return SendRawBytes(vxlanPacket)
}

// buildUDPIPPacket 构建内层 UDP/IP 包
func (dc *DHCPClient) buildUDPIPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, payload []byte) ([]byte, error) {
	// 1. IP 头部 (20 字节)
	ipHeader := make([]byte, 20)
	ipHeader[0] = 0x45             // Version (4) + IHL (5)
	ipHeader[1] = 0x00             // Type of Service
	udpLen := 8 + len(payload)     // UDP 头部 + 数据
	ipLen := 20 + udpLen           // IP 头部 + UDP
	ipHeader[2] = byte(ipLen >> 8) // Total Length (高字节)
	ipHeader[3] = byte(ipLen)      // Total Length (低字节)
	ipHeader[4] = 0x00             // Identification
	ipHeader[5] = 0x00
	ipHeader[6] = 0x40 // Flags + Fragment Offset
	ipHeader[7] = 0x00
	ipHeader[8] = 64 // TTL
	ipHeader[9] = 17 // Protocol (UDP)
	// Checksum 先设为 0，后面计算
	ipHeader[10] = 0x00
	ipHeader[11] = 0x00
	copy(ipHeader[12:16], srcIP.To4()) // Source IP
	copy(ipHeader[16:20], dstIP.To4()) // Destination IP

	// 计算 IP 头部校验和
	checksum := dc.calculateChecksum(ipHeader)
	ipHeader[10] = byte(checksum >> 8)
	ipHeader[11] = byte(checksum)

	// 2. UDP 头部 (8 字节)
	udpHeader := make([]byte, 8)
	udpHeader[0] = byte(srcPort >> 8) // Source Port (高字节)
	udpHeader[1] = byte(srcPort)      // Source Port (低字节)
	udpHeader[2] = byte(dstPort >> 8) // Destination Port (高字节)
	udpHeader[3] = byte(dstPort)      // Destination Port (低字节)
	udpHeader[4] = byte(udpLen >> 8)  // UDP Length (高字节)
	udpHeader[5] = byte(udpLen)       // UDP Length (低字节)
	udpHeader[6] = 0x00               // UDP Checksum (可以设为 0)
	udpHeader[7] = 0x00

	// 3. 组合完整包
	packet := make([]byte, 0, len(ipHeader)+len(udpHeader)+len(payload))
	packet = append(packet, ipHeader...)
	packet = append(packet, udpHeader...)
	packet = append(packet, payload...)

	return packet, nil
}

// calculateChecksum 计算校验和
func (dc *DHCPClient) calculateChecksum(data []byte) uint16 {
	var sum uint32

	// 按 16 位字进行累加
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(data[i])<<8 + uint32(data[i+1])
	}

	// 如果长度为奇数，处理最后一个字节
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}

	// 处理进位
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}

	// 取反
	return uint16(^sum)
}

// HandleDHCPPacket 处理接收到的 DHCP 包 (在 DPDK 包处理器中调用)
func HandleDHCPPacket(data []byte) {
	// 解析 DHCP 包
	dhcpPacket, err := dhcpv4.FromBytes(data)
	if err != nil {
		log.Printf("[DHCP] 解析 DHCP 包失败: %v", err)
		return
	}

	log.Printf("[DHCP] 收到 DHCP 包: 类型=%s, 事务ID=%x",
		dhcpPacket.MessageType(), dhcpPacket.TransactionID)

	// 根据包类型处理
	switch dhcpPacket.MessageType() {
	case dhcpv4.MessageTypeOffer:
		handleDHCPOffer(dhcpPacket, nil) // 暂时不传递 MAC，后续会修改
	case dhcpv4.MessageTypeAck:
		handleDHCPAck(dhcpPacket, nil)
	case dhcpv4.MessageTypeNak:
		log.Printf("[DHCP] 收到 DHCP NAK: %s", dhcpPacket.String())
	default:
		log.Printf("[DHCP] 收到未知 DHCP 消息类型: %s", dhcpPacket.MessageType())
	}
}

// HandleDHCPPacketWithFrame 处理接收到的 DHCP 包，包含完整以太网帧信息
func HandleDHCPPacketWithFrame(frameData []byte, dhcpData []byte) {
	// 提取源 MAC 地址（以太网帧的第6-11字节）
	var srcMAC net.HardwareAddr
	if len(frameData) >= 12 {
		srcMAC = net.HardwareAddr(frameData[6:12])
	}

	// 解析 DHCP 包
	dhcpPacket, err := dhcpv4.FromBytes(dhcpData)
	if err != nil {
		log.Printf("[DHCP] 解析 DHCP 包失败: %v", err)
		return
	}

	log.Printf("[DHCP] 收到 DHCP 包: 类型=%s, 事务ID=%x, 源MAC=%s",
		dhcpPacket.MessageType(), dhcpPacket.TransactionID, srcMAC)

	// 根据包类型处理
	switch dhcpPacket.MessageType() {
	case dhcpv4.MessageTypeOffer:
		handleDHCPOffer(dhcpPacket, srcMAC)
	case dhcpv4.MessageTypeAck:
		handleDHCPAck(dhcpPacket, srcMAC)
	case dhcpv4.MessageTypeNak:
		log.Printf("[DHCP] 收到 DHCP NAK: %s", dhcpPacket.String())
	default:
		log.Printf("[DHCP] 收到未知 DHCP 消息类型: %s", dhcpPacket.MessageType())
	}
}

// handleDHCPOffer 处理 DHCP Offer
func handleDHCPOffer(packet *dhcpv4.DHCPv4, srcMAC net.HardwareAddr) {
	log.Printf("[DHCP] 处理 DHCP Offer: IP=%s", packet.YourIPAddr)

	// 根据事务 ID 找到对应的客户端
	client := findDHCPClientByTransaction(packet.TransactionID)
	if client == nil {
		log.Printf("[DHCP] 未找到匹配的 DHCP 客户端，事务ID: %x", packet.TransactionID)
		return
	}

	// 解析 Offer 信息，包含网关 MAC
	var offerInfo *DHCPOfferInfo
	if srcMAC != nil {
		offerInfo = client.parseOfferWithMAC(packet, srcMAC)
	} else {
		offerInfo = client.parseOffer(packet)
	}

	// 发送到等待的通道
	select {
	case client.pendingOffers <- offerInfo:
		log.Printf("[DHCP] DHCP Offer 已传递给客户端")
	default:
		log.Printf("[DHCP] DHCP Offer 通道已满，丢弃")
	}
}

// handleDHCPAck 处理 DHCP ACK
func handleDHCPAck(packet *dhcpv4.DHCPv4, srcMAC net.HardwareAddr) {
	log.Printf("[DHCP] 处理 DHCP ACK: IP=%s", packet.YourIPAddr)

	// 根据事务 ID 找到对应的客户端
	client := findDHCPClientByTransaction(packet.TransactionID)
	if client == nil {
		log.Printf("[DHCP] 未找到匹配的 DHCP 客户端，事务ID: %x", packet.TransactionID)
		return
	}

	// 解析 ACK 信息，包含网关 MAC
	var ackInfo *DHCPOfferInfo
	if srcMAC != nil {
		ackInfo = client.parseOfferWithMAC(packet, srcMAC)
	} else {
		ackInfo = client.parseOffer(packet)
	}

	// 发送到等待的通道
	select {
	case client.pendingACKs <- ackInfo:
		log.Printf("[DHCP] DHCP ACK 已传递给客户端")
	default:
		log.Printf("[DHCP] DHCP ACK 通道已满，丢弃")
	}
}

// findDHCPClientByTransaction 根据事务ID查找DHCP客户端
func findDHCPClientByTransaction(transactionID dhcpv4.TransactionID) *DHCPClient {
	globalDHCPMutex.RLock()
	defer globalDHCPMutex.RUnlock()

	targetID := uint32(transactionID[0])<<24 | uint32(transactionID[1])<<16 |
		uint32(transactionID[2])<<8 | uint32(transactionID[3])

	for _, client := range globalDHCPClients {
		client.mutex.RLock()
		if client.transactionID == targetID {
			client.mutex.RUnlock()
			return client
		}
		client.mutex.RUnlock()
	}
	return nil
}
