package dpdknet

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"time"

	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
)

// ARPTable ARP 表项
type ARPTable struct {
	entries map[string]ARPEntry
}

// ARPEntry ARP 表项
type ARPEntry struct {
	IP       net.IP
	MAC      net.HardwareAddr
	ExpireAt time.Time
}

// ARPHandler ARP 处理器
type ARPHandler struct {
	localIP  net.IP
	localMAC net.HardwareAddr
	arpTable *ARPTable
	client   *arp.Client
}

// NewARPHandler 创建新的 ARP 处理器
func NewARPHandler(localIP net.IP, localMAC net.HardwareAddr) *ARPHandler {
	return &ARPHandler{
		localIP:  localIP,
		localMAC: localMAC,
		arpTable: NewARPTable(),
	}
}

// NewARPTable 创建新的 ARP 表
func NewARPTable() *ARPTable {
	return &ARPTable{
		entries: make(map[string]ARPEntry),
	}
}

// AddEntry 添加 ARP 表项
func (at *ARPTable) AddEntry(ip net.IP, mac net.HardwareAddr) {
	ipStr := ip.String()
	at.entries[ipStr] = ARPEntry{
		IP:       ip,
		MAC:      mac,
		ExpireAt: time.Now().Add(5 * time.Minute), // 5分钟过期
	}
}

// LookupMAC 查找 MAC 地址
func (at *ARPTable) LookupMAC(ip net.IP) (net.HardwareAddr, bool) {
	ipStr := ip.String()
	entry, exists := at.entries[ipStr]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(entry.ExpireAt) {
		delete(at.entries, ipStr)
		return nil, false
	}

	return entry.MAC, true
}

// SendARPRequest 发送 ARP 请求
func (ah *ARPHandler) SendARPRequest(targetIP net.IP) error {
	log.Printf("[ARP] 发送ARP请求查询 %s 的MAC地址", targetIP.String())

	// 转换IP地址为netip.Addr
	senderIPAddr, err := netip.ParseAddr(ah.localIP.String())
	if err != nil {
		return fmt.Errorf("转换本地IP失败: %v", err)
	}

	targetIPAddr, err := netip.ParseAddr(targetIP.String())
	if err != nil {
		return fmt.Errorf("转换目标IP失败: %v", err)
	}

	// 创建 ARP 请求包
	arpRequest := &arp.Packet{
		HardwareType:       1,      // 以太网
		ProtocolType:       0x0800, // IPv4
		HardwareAddrLength: 6,      // MAC地址长度
		IPLength:           4,      // IPv4地址长度
		Operation:          arp.OperationRequest,
		SenderHardwareAddr: ah.localMAC,
		SenderIP:           senderIPAddr,
		TargetHardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 0}, // 未知
		TargetIP:           targetIPAddr,
	}

	// 调试信息：验证ARP请求包
	log.Printf("[ARP] ARP请求包内容 - 操作: %d, 发送者IP: %s, 发送者MAC: %s, 目标IP: %s, 目标MAC: %s",
		arpRequest.Operation,
		arpRequest.SenderIP.String(), arpRequest.SenderHardwareAddr.String(),
		arpRequest.TargetIP.String(), arpRequest.TargetHardwareAddr.String())

	// 序列化 ARP 包
	arpData, err := arpRequest.MarshalBinary()
	if err != nil {
		return fmt.Errorf("序列化ARP请求失败: %v", err)
	}

	// 通过 VXLAN 发送
	return ah.sendVXLANPacket(arpData, net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
}

// SendARPReply 发送 ARP 回复
func (ah *ARPHandler) SendARPReply(targetIP net.IP, targetMAC net.HardwareAddr) error {
	return ah.SendARPReplyWithMode(targetIP, targetMAC, false)
}

// SendVXLANARPReply 发送 VXLAN ARP 回复
func (ah *ARPHandler) SendVXLANARPReply(targetIP net.IP, targetMAC net.HardwareAddr) error {
	return ah.SendARPReplyWithMode(targetIP, targetMAC, true)
}

// SendARPReplyWithMode 发送 ARP 回复（支持选择发送模式）
func (ah *ARPHandler) SendARPReplyWithMode(targetIP net.IP, targetMAC net.HardwareAddr, isVXLAN bool) error {
	if isVXLAN {
		log.Printf("[ARP] 发送VXLAN ARP回复: %s -> %s", ah.localIP.String(), ah.localMAC.String())
	} else {
		log.Printf("[ARP] 发送普通ARP回复: %s -> %s", ah.localIP.String(), ah.localMAC.String())
	}

	// 调试信息：验证输入参数
	log.Printf("[ARP] 构建ARP回复 - 本地IP: %s, 本地MAC: %s", ah.localIP.String(), ah.localMAC.String())
	log.Printf("[ARP] 构建ARP回复 - 目标IP: %s, 目标MAC: %s", targetIP.String(), targetMAC.String())

	// 验证IP地址不为空
	if ah.localIP == nil {
		return fmt.Errorf("本地IP地址为空")
	}
	if targetIP == nil {
		return fmt.Errorf("目标IP地址为空")
	}

	// 转换IP地址为netip.Addr
	senderIPAddr, err := netip.ParseAddr(ah.localIP.String())
	if err != nil {
		return fmt.Errorf("转换本地IP失败: %v", err)
	}

	targetIPAddr, err := netip.ParseAddr(targetIP.String())
	if err != nil {
		return fmt.Errorf("转换目标IP失败: %v", err)
	}

	// 创建 ARP 回复包
	arpReply := &arp.Packet{
		HardwareType:       1,      // 以太网
		ProtocolType:       0x0800, // IPv4
		HardwareAddrLength: 6,      // MAC地址长度
		IPLength:           4,      // IPv4地址长度
		Operation:          arp.OperationReply,
		SenderHardwareAddr: ah.localMAC,
		SenderIP:           senderIPAddr,
		TargetHardwareAddr: targetMAC,
		TargetIP:           targetIPAddr,
	}

	log.Printf("[ARP] ARP回复包内容 - 操作: %d, 发送者IP: %s, 发送者MAC: %s, 目标IP: %s, 目标MAC: %s",
		arpReply.Operation,
		arpReply.SenderIP.String(), arpReply.SenderHardwareAddr.String(),
		arpReply.TargetIP.String(), arpReply.TargetHardwareAddr.String())

	// 序列化 ARP 包
	arpData, err := arpReply.MarshalBinary()
	if err != nil {
		return fmt.Errorf("序列化ARP回复失败: %v", err)
	}

	// 根据模式选择发送方式
	if isVXLAN {
		return ah.sendVXLANPacket(arpData, targetMAC)
	} else {
		return ah.sendDirectPacket(arpData, targetMAC)
	}
}

// sendVXLANPacket 通过 VXLAN 发送 ARP 包
func (ah *ARPHandler) sendVXLANPacket(arpData []byte, dstMAC net.HardwareAddr) error {
	// 调试信息：打印ARP数据
	log.Printf("[ARP] 构建的ARP数据 (长度: %d): %x", len(arpData), arpData)
	log.Printf("[ARP] 目标MAC: %s, 本地MAC: %s", dstMAC.String(), ah.localMAC.String())

	// 构建以太网帧
	ethFrame := &ethernet.Frame{
		Destination: dstMAC,
		Source:      ah.localMAC,
		EtherType:   ethernet.EtherTypeARP,
		Payload:     arpData,
	}

	// 序列化以太网帧
	frameData, err := ethFrame.MarshalBinary()
	if err != nil {
		return fmt.Errorf("序列化以太网帧失败: %v", err)
	}

	log.Printf("[ARP] 发送VXLAN ARP包，以太网帧长度: %d字节", len(frameData))
	log.Printf("[ARP] 以太网帧数据: %x", frameData)

	// 获取全局VXLAN处理器
	handler := GetGlobalVXLANHandler()
	if handler == nil {
		return fmt.Errorf("全局VXLAN处理器未初始化")
	}

	// 直接使用VXLAN封装以太网帧 (不是IP包)
	// ARP包是二层协议，应该直接封装以太网帧
	vxlanPacket, err := handler.EncapsulateEthernetFrame(frameData)
	if err != nil {
		return fmt.Errorf("VXLAN封装ARP失败: %v", err)
	}

	log.Printf("[ARP] 通过VXLAN发送ARP包，封装后长度: %d字节", len(vxlanPacket))
	// 安全地打印前64字节或整个包（如果包更小）
	debugLen := 64
	if len(vxlanPacket) < debugLen {
		debugLen = len(vxlanPacket)
	}
	log.Printf("[ARP] VXLAN封装后数据的前%d字节: %x", debugLen, vxlanPacket[:debugLen])
	return SendRawBytes(vxlanPacket)
}

// sendDirectPacket 直接发送 ARP 包（不通过 VXLAN）
func (ah *ARPHandler) sendDirectPacket(arpData []byte, dstMAC net.HardwareAddr) error {
	// 构建以太网帧
	ethFrame := &ethernet.Frame{
		Destination: dstMAC,
		Source:      ah.localMAC,
		EtherType:   ethernet.EtherTypeARP,
		Payload:     arpData,
	}

	// 序列化以太网帧
	frameData, err := ethFrame.MarshalBinary()
	if err != nil {
		return fmt.Errorf("序列化以太网帧失败: %v", err)
	}

	log.Printf("[ARP] 直接发送ARP包，长度: %d字节", len(frameData))

	// 直接发送原始以太网帧
	return SendRawBytes(frameData)
}

// ParseARPPacket 解析 ARP 包
func (ah *ARPHandler) ParseARPPacket(data []byte) (*arp.Packet, error) {
	arpPacket := &arp.Packet{}
	err := arpPacket.UnmarshalBinary(data)
	if err != nil {
		return nil, fmt.Errorf("解析ARP包失败: %v", err)
	}
	return arpPacket, nil
}

// HandleARPPacket 处理收到的 ARP 包
func (ah *ARPHandler) HandleARPPacket(data []byte) error {
	arpPacket, err := ah.ParseARPPacket(data)
	if err != nil {
		return fmt.Errorf("解析ARP包失败: %v", err)
	}

	log.Printf("[ARP] 收到ARP包: 操作=%d, 发送者=%s(%s), 目标=%s(%s)",
		arpPacket.Operation,
		arpPacket.SenderIP.String(), arpPacket.SenderHardwareAddr.String(),
		arpPacket.TargetIP.String(), arpPacket.TargetHardwareAddr.String())

	// 更新ARP表 - 转换netip.Addr为net.IP
	senderIP := net.ParseIP(arpPacket.SenderIP.String())
	ah.arpTable.AddEntry(senderIP, arpPacket.SenderHardwareAddr)

	switch arpPacket.Operation {
	case arp.OperationRequest:
		// 如果是请求我们的IP，发送回复
		targetIP := net.ParseIP(arpPacket.TargetIP.String())
		if targetIP.Equal(ah.localIP) {
			log.Printf("[ARP] 收到针对本机IP的ARP请求，发送回复")
			return ah.SendARPReply(senderIP, arpPacket.SenderHardwareAddr)

		}
	case arp.OperationReply:
		// ARP回复，已经更新了ARP表
		log.Printf("[ARP] 收到ARP回复，已更新ARP表")
	}

	return nil
}

// HandleVXLANARPPacket 专门处理 VXLAN 内层的 ARP 包
func (ah *ARPHandler) HandleVXLANARPPacket(data []byte) error {
	arpPacket, err := ah.ParseARPPacket(data)
	if err != nil {
		return fmt.Errorf("解析VXLAN ARP包失败: %v", err)
	}

	log.Printf("[ARP] 收到VXLAN ARP包: 操作=%d, 发送者=%s(%s), 目标=%s(%s)",
		arpPacket.Operation,
		arpPacket.SenderIP.String(), arpPacket.SenderHardwareAddr.String(),
		arpPacket.TargetIP.String(), arpPacket.TargetHardwareAddr.String())

	// 更新ARP表 - 转换netip.Addr为net.IP
	senderIP := net.ParseIP(arpPacket.SenderIP.String())
	ah.arpTable.AddEntry(senderIP, arpPacket.SenderHardwareAddr)

	switch arpPacket.Operation {
	case arp.OperationRequest:
		// 如果是请求我们的IP，发送VXLAN回复
		targetIP := net.ParseIP(arpPacket.TargetIP.String())
		if targetIP.Equal(ah.localIP) {
			log.Printf("[ARP] 收到针对本机IP的VXLAN ARP请求，发送VXLAN回复")
			return ah.SendVXLANARPReply(senderIP, arpPacket.SenderHardwareAddr)
		}
	case arp.OperationReply:
		// ARP回复，已经更新了ARP表
		log.Printf("[ARP] 收到VXLAN ARP回复，已更新ARP表")
	}

	return nil
}

// RequestMAC 请求指定IP的MAC地址
func (ah *ARPHandler) RequestMAC(targetIP net.IP) (net.HardwareAddr, error) {
	// 首先检查ARP表
	if mac, found := ah.arpTable.LookupMAC(targetIP); found {
		log.Printf("[ARP] 从ARP表中找到 %s -> %s", targetIP.String(), mac.String())
		return mac, nil
	}

	// ARP表中没有，发送ARP请求
	log.Printf("[ARP] ARP表中未找到 %s，发送ARP请求", targetIP.String())
	err := ah.SendARPRequest(targetIP)
	if err != nil {
		return nil, fmt.Errorf("发送ARP请求失败: %v", err)
	}

	// 等待一段时间后再次检查ARP表
	// 实际实现中这里应该有更好的同步机制
	time.Sleep(100 * time.Millisecond)

	if mac, found := ah.arpTable.LookupMAC(targetIP); found {
		return mac, nil
	}

	return nil, fmt.Errorf("ARP请求超时，未收到 %s 的回复", targetIP.String())
}

// GetARPTable 获取ARP表
func (ah *ARPHandler) GetARPTable() map[string]ARPEntry {
	return ah.arpTable.entries
}

// CreateARPClient 创建 ARP 客户端（用于直接网络接口操作）
func (ah *ARPHandler) CreateARPClient(ifaceName string) error {
	// 注意：这个方法需要根据实际的 arp 库 API 调整
	// 当前版本的 mdlayher/arp 可能不支持这种方式
	log.Printf("[ARP] ARP客户端功能暂未实现，使用模拟模式")
	return nil
}

// CloseARPClient 关闭 ARP 客户端
func (ah *ARPHandler) CloseARPClient() error {
	if ah.client != nil {
		return ah.client.Close()
	}
	return nil
}
