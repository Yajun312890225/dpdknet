package dpdknet

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync"
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
	// 简化的等待机制：IP -> 通道的映射
	waitChannels map[string]chan net.HardwareAddr
	waitMutex    sync.Mutex
}

// NewARPHandler 创建新的 ARP 处理器
func NewARPHandler(localIP net.IP, localMAC net.HardwareAddr) *ARPHandler {
	return &ARPHandler{
		localIP:      localIP,
		localMAC:     localMAC,
		arpTable:     NewARPTable(),
		waitChannels: make(map[string]chan net.HardwareAddr),
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

	// 序列化 ARP 包
	arpData, err := arpRequest.MarshalBinary()
	if err != nil {
		return fmt.Errorf("序列化ARP请求失败: %v", err)
	}

	// 通过 VXLAN 发送
	return ah.sendDirectPacket(arpData, net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
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

	// 安全地打印前64字节或整个包（如果包更小）
	debugLen := 64
	if len(vxlanPacket) < debugLen {
		debugLen = len(vxlanPacket)
	}
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

// HandleARPPacket 处理收到的 ARP 包（完整的以太网帧）
func (ah *ARPHandler) HandleARPPacket(data []byte) error {
	// ARP包最小长度：以太网头(14) + ARP头(28) = 42字节
	if len(data) < 42 {
		return fmt.Errorf("ARP packet too short: %d bytes", len(data))
	}

	// 检查是否是ARP包 (EtherType = 0x0806)
	etherType := uint16(data[12])<<8 | uint16(data[13])
	if etherType != 0x0806 {
		return fmt.Errorf("not an ARP packet, EtherType: 0x%04x", etherType)
	}

	// 提取ARP数据（跳过以太网头）
	arpData := data[14:]

	arpPacket, err := ah.ParseARPPacket(arpData)
	if err != nil {
		return fmt.Errorf("解析ARP包失败: %v", err)
	}

	// 更新ARP表 - 转换netip.Addr为net.IP
	senderIP := net.ParseIP(arpPacket.SenderIP.String())
	ah.arpTable.AddEntry(senderIP, arpPacket.SenderHardwareAddr)

	// 检查是否有等待这个IP的请求
	ah.notifyPendingRequest(senderIP, arpPacket.SenderHardwareAddr)

	switch arpPacket.Operation {
	case arp.OperationRequest:
		// 如果是请求我们的IP，发送回复
		targetIP := net.ParseIP(arpPacket.TargetIP.String())
		if targetIP.Equal(ah.localIP) {
			log.Printf("[ARP] 收到对本地IP的ARP请求: %s", targetIP.String())
			return ah.SendARPReply(senderIP, arpPacket.SenderHardwareAddr)
		}
	case arp.OperationReply:
		// ARP回复，已经更新了ARP表并通知了等待的请求
	}

	return nil
}

// HandleARPPacketWithConfig 处理ARP包，支持自定义本地IP和MAC配置（用于TAP设备）
func (ah *ARPHandler) HandleARPPacketWithConfig(data []byte, localIP string, localMAC string, tapDevice interface{}) error {
	// ARP包最小长度：以太网头(14) + ARP头(28) = 42字节
	if len(data) < 42 {
		return fmt.Errorf("ARP packet too short: %d bytes", len(data))
	}

	// 检查是否是ARP包 (EtherType = 0x0806)
	etherType := uint16(data[12])<<8 | uint16(data[13])
	if etherType != 0x0806 {
		return fmt.Errorf("not an ARP packet, EtherType: 0x%04x", etherType)
	}

	// 提取ARP数据（跳过以太网头）
	arpData := data[14:]

	arpPacket, err := ah.ParseARPPacket(arpData)
	if err != nil {
		return fmt.Errorf("解析TAP ARP包失败: %v", err)
	}

	// 更新ARP表 - 转换netip.Addr为net.IP
	senderIP := net.ParseIP(arpPacket.SenderIP.String())
	ah.arpTable.AddEntry(senderIP, arpPacket.SenderHardwareAddr)

	// 检查是否有等待这个IP的请求
	ah.notifyPendingRequest(senderIP, arpPacket.SenderHardwareAddr)

	switch arpPacket.Operation {
	case arp.OperationRequest:
		// 检查是否是请求指定的本地IP
		targetIP := net.ParseIP(arpPacket.TargetIP.String())
		if targetIP.String() == localIP {
			return ah.sendTAPARPReply(data, localIP, localMAC, tapDevice)
		}
	case arp.OperationReply:
		// ARP回复，已经更新了ARP表并通知了等待的请求
	}

	return nil
}

// sendTAPARPReply 为TAP设备发送ARP回复
func (ah *ARPHandler) sendTAPARPReply(originalPacket []byte, localIP string, localMAC string, tapDevice interface{}) error {
	// 解析ARP头部（从以太网头之后开始，偏移14字节）
	arpOffset := 14

	// 检查硬件类型和协议类型
	hardwareType := uint16(originalPacket[arpOffset])<<8 | uint16(originalPacket[arpOffset+1])
	protocolType := uint16(originalPacket[arpOffset+2])<<8 | uint16(originalPacket[arpOffset+3])
	operation := uint16(originalPacket[arpOffset+6])<<8 | uint16(originalPacket[arpOffset+7])

	// 验证ARP包格式（以太网上的IPv4 ARP）
	if hardwareType != 1 || protocolType != 0x0800 {
		return fmt.Errorf("unsupported ARP packet type")
	}

	// 只处理ARP请求
	if operation != 1 {
		return fmt.Errorf("not an ARP request")
	}

	// 提取目标IP
	targetIP := originalPacket[arpOffset+24 : arpOffset+28]
	targetIPStr := fmt.Sprintf("%d.%d.%d.%d", targetIP[0], targetIP[1], targetIP[2], targetIP[3])

	// 检查是否是请求我们的IP
	if targetIPStr != localIP {
		return fmt.Errorf("target IP %s does not match local IP %s", targetIPStr, localIP)
	}

	// 构造ARP回复
	reply := make([]byte, 42)

	// 以太网头部：交换源和目标MAC
	copy(reply[0:6], originalPacket[6:12]) // 目标MAC = 原始源MAC
	localMACBytes, err := net.ParseMAC(localMAC)
	if err != nil {
		return fmt.Errorf("解析本地MAC地址失败: %v", err)
	}
	copy(reply[6:12], localMACBytes) // 源MAC = 我们的MAC
	reply[12] = 0x08                 // EtherType = ARP
	reply[13] = 0x06

	// ARP头部
	reply[arpOffset] = 0x00 // Hardware Type = 1 (以太网)
	reply[arpOffset+1] = 0x01
	reply[arpOffset+2] = 0x08 // Protocol Type = 0x0800 (IPv4)
	reply[arpOffset+3] = 0x00
	reply[arpOffset+4] = 0x06 // Hardware Address Length = 6
	reply[arpOffset+5] = 0x04 // Protocol Address Length = 4
	reply[arpOffset+6] = 0x00 // Operation = 2 (ARP回复)
	reply[arpOffset+7] = 0x02

	// Sender Hardware Address = 我们的MAC
	localMACBytes, err = net.ParseMAC(localMAC)
	if err != nil {
		return fmt.Errorf("解析本地MAC地址失败: %v", err)
	}
	copy(reply[arpOffset+8:arpOffset+14], localMACBytes)
	// Sender Protocol Address = 我们的IP
	localIPAddr := net.ParseIP(localIP)
	if localIPAddr == nil {
		return fmt.Errorf("解析本地IP地址失败: %s", localIP)
	}
	copy(reply[arpOffset+14:arpOffset+18], localIPAddr.To4())

	// Target Hardware Address = 请求方的MAC
	copy(reply[arpOffset+18:arpOffset+24], originalPacket[arpOffset+8:arpOffset+14])
	// Target Protocol Address = 请求方的IP
	copy(reply[arpOffset+24:arpOffset+28], originalPacket[arpOffset+14:arpOffset+18])

	// 类型断言，获取TAP设备接口
	if tapManager, ok := tapDevice.(*TapManager); ok {
		// 根据本地IP判断应该发送到哪个TAP设备
		if localIP == TAP_NORMAL_IP {
			return tapManager.SendToNormalTap(reply)
		} else if localIP == TAP_VXLAN_IP {
			return tapManager.SendToVXLANTap(reply)
		}
	}

	// 如果类型断言失败或IP地址不匹配，降级到原始方式
	return SendRawBytes(reply)
}

// HandleVXLANARPPacket 专门处理 VXLAN 内层的 ARP 包（完整的以太网帧）
func (ah *ARPHandler) HandleVXLANARPPacket(data []byte) error {
	// ARP包最小长度：以太网头(14) + ARP头(28) = 42字节
	if len(data) < 42 {
		return fmt.Errorf("VXLAN ARP packet too short: %d bytes", len(data))
	}

	// 检查是否是ARP包 (EtherType = 0x0806)
	etherType := uint16(data[12])<<8 | uint16(data[13])
	if etherType != 0x0806 {
		return fmt.Errorf("not an ARP packet, EtherType: 0x%04x", etherType)
	}

	// 提取ARP数据（跳过以太网头）
	arpData := data[14:]

	arpPacket, err := ah.ParseARPPacket(arpData)
	if err != nil {
		return fmt.Errorf("解析VXLAN ARP包失败: %v", err)
	}

	// 更新ARP表 - 转换netip.Addr为net.IP
	senderIP := net.ParseIP(arpPacket.SenderIP.String())
	ah.arpTable.AddEntry(senderIP, arpPacket.SenderHardwareAddr)

	// 检查是否有等待这个IP的请求
	ah.notifyPendingRequest(senderIP, arpPacket.SenderHardwareAddr)

	switch arpPacket.Operation {
	case arp.OperationRequest:
		// 如果是请求我们的IP，发送VXLAN回复
		targetIP := net.ParseIP(arpPacket.TargetIP.String())
		if targetIP.Equal(ah.localIP) {
			return ah.SendVXLANARPReply(senderIP, arpPacket.SenderHardwareAddr)
		}
	case arp.OperationReply:
		// ARP回复，已经更新了ARP表并通知了等待的请求
	}

	return nil
}

// notifyPendingRequest 通知等待指定IP的请求
func (ah *ARPHandler) notifyPendingRequest(ip net.IP, mac net.HardwareAddr) {
	ipStr := ip.String()

	ah.waitMutex.Lock()
	defer ah.waitMutex.Unlock()

	if ch, exists := ah.waitChannels[ipStr]; exists {
		// 通知等待的请求
		select {
		case ch <- mac:
			// 成功发送结果
		default:
			// 通道可能已关闭或已满，忽略
		}
		// 删除已处理的请求
		delete(ah.waitChannels, ipStr)
	}
}

// RequestMAC 请求指定IP的MAC地址
func (ah *ARPHandler) RequestMAC(targetIP net.IP) (net.HardwareAddr, error) {
	return ah.RequestMACWithTimeout(targetIP, time.Second)
}

// RequestMACWithTimeout 请求指定IP的MAC地址（带超时）
func (ah *ARPHandler) RequestMACWithTimeout(targetIP net.IP, timeout time.Duration) (net.HardwareAddr, error) {
	// 首先检查ARP表
	if mac, found := ah.arpTable.LookupMAC(targetIP); found {
		return mac, nil
	}

	ipStr := targetIP.String()

	// 检查是否已经有对这个IP的等待请求
	ah.waitMutex.Lock()
	if existingCh, exists := ah.waitChannels[ipStr]; exists {
		ah.waitMutex.Unlock()
		// 如果已经有请求在等待，直接等待其结果
		select {
		case mac := <-existingCh:
			return mac, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("ARP请求超时，未收到 %s 的回复", targetIP.String())
		}
	}

	// 创建新的等待通道
	waitCh := make(chan net.HardwareAddr, 1)
	ah.waitChannels[ipStr] = waitCh
	ah.waitMutex.Unlock()

	// 发送ARP请求
	err := ah.SendARPRequest(targetIP)
	if err != nil {
		// 请求发送失败，清理注册的通道
		ah.waitMutex.Lock()
		delete(ah.waitChannels, ipStr)
		ah.waitMutex.Unlock()
		return nil, fmt.Errorf("发送ARP请求失败: %v", err)
	}

	// 等待结果或超时
	select {
	case mac := <-waitCh:
		return mac, nil
	case <-time.After(timeout):
		// 超时清理
		ah.waitMutex.Lock()
		delete(ah.waitChannels, ipStr)
		ah.waitMutex.Unlock()
		return nil, fmt.Errorf("ARP请求超时，未收到 %s 的回复", targetIP.String())
	}
}
