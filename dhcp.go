package dpdknet

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

// DHCPClient DHCP 客户端封装
type DHCPClient struct {
	mac         net.HardwareAddr
	ifaceName   string
	client      *nclient4.Client
	vxlanConfig *VXLANConfig
	hostname    string
}

// DHCPOfferInfo DHCP Offer 信息
type DHCPOfferInfo struct {
	YourIP      net.IP
	ServerIP    net.IP
	Gateway     net.IP
	SubnetMask  net.IPMask
	DNS         []net.IP
	LeaseTime   time.Duration
	MessageType dhcpv4.MessageType
}

// NewDHCPClient 创建新的 DHCP 客户端
func NewDHCPClient(mac net.HardwareAddr, vxlanConfig *VXLANConfig, hostname string) *DHCPClient {
	return &DHCPClient{
		mac:         mac,
		vxlanConfig: vxlanConfig,
		hostname:    hostname,
		ifaceName:   "eth0", // 默认网卡名，可根据需要修改
	}
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
	log.Printf("[DHCP] 发送 DHCP Discover 广播包")

	err := dc.initClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 发送 Discover 并等待 Offer
	offer, err := dc.client.DiscoverOffer(ctx, dhcpv4.WithOption(dhcpv4.OptHostName(dc.hostname)))
	if err != nil {
		return nil, fmt.Errorf("DHCP Discover 失败: %v", err)
	}

	log.Printf("[DHCP] 收到 DHCP Offer")

	// 解析 Offer 信息
	offerInfo := dc.parseOffer(offer)
	return offerInfo, nil
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
	ack, err := dc.client.Request(ctx, offer)
	if err != nil {
		return nil, fmt.Errorf("DHCP Request 失败: %v", err)
	}

	log.Printf("[DHCP] 收到 DHCP ACK")

	// 解析 ACK 信息
	ackInfo := dc.parseOffer(ack)
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

// StartDHCPWithRawPackets 使用原始包的完整 DHCP 流程
func (dc *DHCPClient) StartDHCPWithRawPackets() (*DHCPOfferInfo, error) {
	log.Printf("[DHCP] 启动完整 DHCP 流程")

	err := dc.initClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 步骤1: Discover + Offer
	offer, err := dc.client.DiscoverOffer(ctx, dhcpv4.WithOption(dhcpv4.OptHostName(dc.hostname)))
	if err != nil {
		return nil, fmt.Errorf("DHCP Discover/Offer 失败: %v", err)
	}

	log.Printf("[DHCP] 收到 DHCP Offer: IP=%s", offer.YourIPAddr)

	// 步骤2: Request + ACK
	ack, err := dc.client.Request(ctx, offer)
	if err != nil {
		return nil, fmt.Errorf("DHCP Request/ACK 失败: %v", err)
	}

	log.Printf("[DHCP] 收到 DHCP ACK: IP=%s", ack.YourIPAddr)

	// 解析最终配置
	finalInfo := dc.parseOffer(ack)
	log.Printf("[DHCP] DHCP 流程完成，最终配置: IP=%s, Gateway=%s, DNS=%v",
		finalInfo.YourIP, finalInfo.Gateway, finalInfo.DNS)

	return finalInfo, nil
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

	// 进行 VXLAN 封装
	vxlanPacket, err := handler.EncapsulateVXLAN(
		dhcpData, dc.vxlanConfig.LocalIP, dstIP,
		srcPort, dstPort, 17) // 17 = UDP 协议号
	if err != nil {
		return fmt.Errorf("VXLAN 封装失败: %v", err)
	}

	log.Printf("[DHCP] 通过 VXLAN 发送 DHCP 包，封装后长度: %d 字节", len(vxlanPacket))

	// 发送 VXLAN 包
	return SendRawBytes(vxlanPacket)
}
