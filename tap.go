package dpdknet

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/songgao/water"
)

// 全局配置常量
const (
	// TAP设备固定MAC地址
	TAP_NORMAL_MAC = "da:67:5d:00:d4:91"
	TAP_VXLAN_MAC  = "da:67:5d:00:d4:92"

	// 固定的目标IP地址
	TARGET_IP = "172.16.1.1"

	OUTMAC = "fe:ee:89:96:ac:a3"
)

// TapDevice TAP 设备管理器，使用water库
type TapDevice struct {
	name      string
	iface     *water.Interface
	bridge    string
	running   bool
	mutex     sync.RWMutex
	closeChan chan struct{}

	// 回调函数：处理从TAP收到的包
	OnPacketReceived func([]byte) error

	// 统计信息
	rxPackets uint64
	txPackets uint64
}

// TapManager TAP设备管理器，管理两张TAP网卡
type TapManager struct {
	normalTap *TapDevice // 正常数据TAP
	vxlanTap  *TapDevice // VXLAN数据TAP
	mutex     sync.RWMutex
}

// NewTapDevice 创建TAP设备
func NewTapDevice(name, bridge string) (*TapDevice, error) {
	config := water.Config{
		DeviceType: water.TAP,
	}
	config.Name = name

	// 创建TAP接口
	iface, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("创建TAP接口失败: %v", err)
	}

	tap := &TapDevice{
		name:      name,
		iface:     iface,
		bridge:    bridge,
		closeChan: make(chan struct{}),
	}

	// 配置TAP设备并添加到网桥
	if err := tap.configure(); err != nil {
		iface.Close()
		return nil, fmt.Errorf("配置TAP设备失败: %v", err)
	}

	return tap, nil
}

// NewTapManager 创建TAP管理器，同时创建两张TAP网卡
func NewTapManager() (*TapManager, error) {
	manager := &TapManager{}

	// 创建正常数据TAP，绑定到br1
	normalTap, err := NewTapDevice("tap-normal", "br1")
	if err != nil {
		return nil, fmt.Errorf("创建正常数据TAP失败: %v", err)
	}
	manager.normalTap = normalTap

	// 创建VXLAN数据TAP，绑定到br2
	vxlanTap, err := NewTapDevice("tap-vxlan", "br2")
	if err != nil {
		normalTap.Close()
		return nil, fmt.Errorf("创建VXLAN数据TAP失败: %v", err)
	}
	manager.vxlanTap = vxlanTap

	// 设置回调函数
	normalTap.SetPacketHandler(manager.handleNormalPacket)
	vxlanTap.SetPacketHandler(manager.handleVXLANPacket)

	return manager, nil
}

// configure 配置 TAP 设备并添加到网桥
func (t *TapDevice) configure() error {
	// 验证接口名称
	if !isValidInterfaceName(t.name) {
		return fmt.Errorf("无效的TAP接口名称: %s", t.name)
	}
	if !isValidInterfaceName(t.bridge) {
		return fmt.Errorf("无效的网桥名称: %s", t.bridge)
	}

	// 启动TAP接口
	if err := runCommand("ip", "link", "set", t.name, "up"); err != nil {
		return fmt.Errorf("启动TAP接口失败: %v", err)
	}

	// 设置TAP设备的固定MAC地址
	var tapMAC string
	switch t.name {
	case "tap-normal":
		tapMAC = TAP_NORMAL_MAC
	case "tap-vxlan":
		tapMAC = TAP_VXLAN_MAC
	default:
		tapMAC = TAP_NORMAL_MAC // 默认使用normal MAC
	}

	if err := runCommand("ip", "link", "set", "dev", t.name, "address", tapMAC); err != nil {
		log.Printf("[TAP] 设置TAP设备MAC地址失败: %v", err)
	} else {
		log.Printf("[TAP] 设置TAP设备MAC地址成功: %s -> %s", t.name, tapMAC)
	}

	// 添加到网桥
	if err := runCommand("brctl", "addif", t.bridge, t.name); err != nil {

		return fmt.Errorf("添加TAP接口到网桥失败: %v", err)

	}

	return nil
}

// SetPacketHandler 设置数据包处理回调函数
func (t *TapDevice) SetPacketHandler(handler func([]byte) error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.OnPacketReceived = handler
}

// Start 启动TAP设备监听
func (t *TapDevice) Start() error {
	t.mutex.Lock()
	if t.running {
		t.mutex.Unlock()
		return fmt.Errorf("TAP设备 %s 已经在运行", t.name)
	}
	t.running = true
	t.mutex.Unlock()

	log.Printf("[TAP] 启动TAP设备监听: %s", t.name)

	// 启动数据包接收goroutine
	go t.receiveLoop()

	return nil
}

// Stop 停止TAP设备
func (t *TapDevice) Stop() {
	t.mutex.Lock()
	if !t.running {
		t.mutex.Unlock()
		return
	}
	t.running = false
	t.mutex.Unlock()

	close(t.closeChan)
	log.Printf("[TAP] 停止TAP设备: %s", t.name)
}

// receiveLoop 接收数据包循环
func (t *TapDevice) receiveLoop() {
	buffer := make([]byte, 1500) // MTU大小的缓冲区

	for {
		select {
		case <-t.closeChan:
			return
		default:
			// 从TAP接口读取数据包
			n, err := t.iface.Read(buffer)
			if err != nil {
				log.Printf("[TAP] 从TAP设备读取失败: %v", err)
				continue
			}

			if n > 0 {
				t.mutex.Lock()
				t.rxPackets++
				t.mutex.Unlock()

				// 复制数据包
				packet := make([]byte, n)
				copy(packet, buffer[:n])

				// 调用回调函数处理数据包
				if t.OnPacketReceived != nil {
					if err := t.OnPacketReceived(packet); err != nil {
						log.Printf("[TAP] 处理数据包失败: %v", err)
					}
				}
			}
		}
	}
}

// SendPacket 发送数据包到TAP设备
func (t *TapDevice) SendPacket(packet []byte) error {
	if !t.running {
		return fmt.Errorf("TAP设备 %s 未运行", t.name)
	}

	// 修改数据包的目标IP为172.16.1.1，目标MAC
	modifiedPacket, err := t.modifyPacketHeaders(packet)
	if err != nil {
		log.Printf("[TAP] 修改数据包头失败: %v", err)
		modifiedPacket = packet // 如果修改失败，使用原始包
	}

	n, err := t.iface.Write(modifiedPacket)
	if err != nil {
		return fmt.Errorf("发送数据包到TAP设备失败: %v", err)
	}

	if n != len(modifiedPacket) {
		return fmt.Errorf("数据包发送不完整: 期望 %d 字节，实际发送 %d 字节", len(modifiedPacket), n)
	}

	t.mutex.Lock()
	t.txPackets++
	t.mutex.Unlock()

	return nil
}

// Close 关闭TAP设备
func (t *TapDevice) Close() error {
	t.Stop()

	if t.iface != nil {
		return t.iface.Close()
	}
	return nil
}

// GetStats 获取统计信息
func (t *TapDevice) GetStats() (rx, tx uint64) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.rxPackets, t.txPackets
}

// GetName 获取TAP设备名称
func (t *TapDevice) GetName() string {
	return t.name
}

// === TapManager 方法 ===

// Start 启动TAP管理器
func (tm *TapManager) Start() error {
	if err := tm.normalTap.Start(); err != nil {
		return fmt.Errorf("启动正常TAP失败: %v", err)
	}

	if err := tm.vxlanTap.Start(); err != nil {
		tm.normalTap.Stop()
		return fmt.Errorf("启动VXLAN TAP失败: %v", err)
	}

	log.Printf("[TAP] TAP管理器启动成功")
	return nil
}

// Stop 停止TAP管理器
func (tm *TapManager) Stop() {
	tm.normalTap.Stop()
	if tm.vxlanTap != nil {
		tm.vxlanTap.Stop()
	}
	log.Printf("[TAP] TAP管理器已停止")
}

// Close 关闭TAP管理器
func (tm *TapManager) Close() error {
	var firstErr error

	if err := tm.normalTap.Close(); err != nil {
		firstErr = err
		log.Printf("[TAP] 关闭正常TAP失败: %v", err)
	}

	if tm.vxlanTap != nil {
		if err := tm.vxlanTap.Close(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("[TAP] 关闭VXLAN TAP失败: %v", err)
		}
	}

	return firstErr
}

// SendToNormalTap 发送数据包到正常TAP设备
func (tm *TapManager) SendToNormalTap(packet []byte) error {
	return tm.normalTap.SendPacket(packet)
}

// SendToVXLANTap 发送数据包到VXLAN TAP设备
func (tm *TapManager) SendToVXLANTap(packet []byte) error {
	if tm.vxlanTap == nil {
		return fmt.Errorf("VXLAN TAP设备未初始化")
	}
	return tm.vxlanTap.SendPacket(packet)
}

// GetNormalTapName 获取正常TAP设备名称
func (tm *TapManager) GetNormalTapName() string {
	return tm.normalTap.GetName()
}

// GetVXLANTapName 获取VXLAN TAP设备名称
func (tm *TapManager) GetVXLANTapName() string {
	if tm.vxlanTap == nil {
		return ""
	}
	return tm.vxlanTap.GetName()
}

// GetTapStats 获取TAP设备统计信息
func (tm *TapManager) GetTapStats() (normalRx, normalTx, vxlanRx, vxlanTx uint64) {
	normalRx, normalTx = tm.normalTap.GetStats()
	if tm.vxlanTap != nil {
		vxlanRx, vxlanTx = tm.vxlanTap.GetStats()
	}
	return
}

// modifyReturnPacketHeaders 修改回包的源IP和源MAC为DPDK的地址
func (tm *TapManager) modifyReturnPacketHeaders(packet []byte) []byte {
	// 检查包长度是否足够 (以太网头14字节)
	if len(packet) < 14 {
		log.Printf("[TAP] 回包太短，无法修改头部: %d字节", len(packet))
		return packet
	}

	// 复制原始包
	modifiedPacket := make([]byte, len(packet))
	copy(modifiedPacket, packet)

	copy(modifiedPacket[6:12], localMAC[:])

	// 检查是否是IP包并修改IP地址
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType == 0x0800 && len(packet) >= 34 {
		ipOffset := 14

		// 将源IP改为原始的目标服务器IP
		copy(modifiedPacket[ipOffset+12:ipOffset+16], getLocalIPFromEnv().To4())

		// 目标IP保持不变，应该是171.213.255.230

		// 重新计算IP头校验和
		if err := updateIPChecksumAtOffset(modifiedPacket, ipOffset); err != nil {
			log.Printf("[TAP] 更新回包IP校验和失败: %v", err)
		}

		// 重新计算TCP校验和（如果是TCP包）
		if packet[ipOffset+9] == 6 && len(modifiedPacket) >= ipOffset+40 { // TCP协议
			if err := updateTCPChecksumAtOffset(modifiedPacket, ipOffset); err != nil {
				log.Printf("[TAP] 更新回包TCP校验和失败: %v", err)
			}
		}
	}

	return modifiedPacket
}

// modifyOutgoingPacketHeaders 修改发出包的源IP和源MAC为DPDK的地址
func (tm *TapManager) modifyOutgoingPacketHeaders(packet []byte) []byte {
	// 检查包长度是否足够 (以太网头14字节)
	if len(packet) < 14 {
		return packet
	}

	// 复制原始包
	modifiedPacket := make([]byte, len(packet))
	copy(modifiedPacket, packet)

	// 设置目标MAC为外网网关或下一跳的MAC地址（用于发送到公网）
	outMAC := parseMACAddress(OUTMAC)
	if outMAC != nil {
		copy(modifiedPacket[0:6], outMAC)
	}

	// 设置源MAC为DPDK网卡的MAC地址（表示从DPDK网卡发出）
	copy(modifiedPacket[6:12], localMAC[:])

	// 检查是否是IP包并修改IP地址
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType == 0x0800 && len(packet) >= 34 {
		ipOffset := 14

		// 将源IP改为DPDK网卡的IP（从DPDK网卡发出）
		dpdkIP := getLocalIPFromEnv().To4()
		if dpdkIP != nil {
			copy(modifiedPacket[ipOffset+12:ipOffset+16], dpdkIP)
		}

		// 重新计算IP头校验和
		if err := updateIPChecksumAtOffset(modifiedPacket, ipOffset); err != nil {
			log.Printf("[TAP] 更新发出包IP校验和失败: %v", err)
		}

		// 重新计算TCP校验和（如果是TCP包）
		if packet[ipOffset+9] == 6 && len(modifiedPacket) >= ipOffset+40 { // TCP协议
			if err := updateTCPChecksumAtOffset(modifiedPacket, ipOffset); err != nil {
				log.Printf("[TAP] 更新发出包TCP校验和失败: %v", err)
			}
		}
	}

	return modifiedPacket
}

// handleNormalPacket 处理来自正常TAP的数据包
func (tm *TapManager) handleNormalPacket(packet []byte) error {

	// 分析包的方向：检查目标IP来判断是回包还是发出的包
	if len(packet) >= 34 {
		etherType := uint16(packet[12])<<8 | uint16(packet[13])
		if etherType == 0x0800 {
			// 获取目标IP
			dstIP := fmt.Sprintf("%d.%d.%d.%d", packet[30], packet[31], packet[32], packet[33])
			localIP := getLocalIPFromEnv().String()

			// 如果目标IP是本地IP，这是一个回包（从服务器返回给客户端）
			if dstIP == localIP {
				modifiedPacket := tm.modifyReturnPacketHeaders(packet)
				return SendRawBytes(modifiedPacket)
			} else {
				// 如果目标IP不是本地IP，这是一个要发出去的包（从客户端发给服务器）
				modifiedPacket := tm.modifyOutgoingPacketHeaders(packet)
				return SendRawBytes(modifiedPacket)
			}
		}
	}

	return SendRawBytes(packet)
}

// handleVXLANPacket 处理来自VXLAN TAP的数据包
func (tm *TapManager) handleVXLANPacket(packet []byte) error {
	// 检查是否是VXLAN包，如果是则进行特殊处理
	if isVXLANPacket(packet) {
		handler := GetGlobalVXLANHandler()
		if handler != nil {
			// 这里可以添加VXLAN特殊处理逻辑
			log.Printf("[TAP] VXLAN包特殊处理")
		}
	}

	// 将数据包发送到DPDK
	return SendRawBytes(packet)
}

// === 辅助函数 ===

// getTapIPAddress 根据TAP设备名称获取对应的IP地址
func getTapIPAddress(tapName string) string {
	switch tapName {
	case "tap-normal":
		return "172.16.1.2/24"
	case "tap-vxlan":
		return "172.16.1.3/24"
	default:
		return ""
	}
}

// GetTapIP 获取TAP设备的IP地址（不带掩码）
func (t *TapDevice) GetTapIP() string {
	fullAddr := getTapIPAddress(t.name)
	if fullAddr == "" {
		return ""
	}
	// 去掉掩码部分
	if idx := strings.Index(fullAddr, "/"); idx != -1 {
		return fullAddr[:idx]
	}
	return fullAddr
}

// modifyPacketHeaders 修改数据包的目标MAC和IP地址
func (t *TapDevice) modifyPacketHeaders(packet []byte) ([]byte, error) {
	// 使用固定的目标IP地址
	targetIP := TARGET_IP

	// 获取TAP设备对应的MAC地址
	var targetMAC []byte
	switch t.name {
	case "tap-normal":
		targetMAC = parseMACAddress(TAP_NORMAL_MAC)
	case "tap-vxlan":
		targetMAC = parseMACAddress(TAP_VXLAN_MAC)
	default:
		targetMAC = parseMACAddress(TAP_NORMAL_MAC)
	}

	// 检查包长度是否足够 (以太网头14字节)
	if len(packet) < 14 {
		return packet, fmt.Errorf("数据包太短，无法修改以太网头部: %d字节", len(packet))
	}

	// 复制原始包
	modifiedPacket := make([]byte, len(packet))
	copy(modifiedPacket, packet)

	// 修改目标MAC地址 (以太网头前6字节)
	copy(modifiedPacket[0:6], targetMAC)

	// 尝试在不同位置查找IPv4头部 (0x45表示IPv4且头长度为20字节)
	ipOffset := -1
	for i := 14; i <= 18 && i < len(packet); i++ {
		if packet[i] == 0x45 && i+20 < len(packet) {
			// 检查是否真的是IP包 (查看协议字段等)
			if packet[i+9] == 0x06 || packet[i+9] == 0x11 || packet[i+9] == 0x01 { // TCP/UDP/ICMP
				ipOffset = i
				break
			}
		}
	}

	// 如果找到了IP头，修改IP地址
	if ipOffset >= 0 && ipOffset+20 <= len(packet) {
		// 解析目标IP地址
		targetIPBytes := parseIPAddress(targetIP)
		if targetIPBytes == nil {
			return packet, fmt.Errorf("无效的目标IP地址: %s", targetIP)
		}

		// 修改目标IP地址
		copy(modifiedPacket[ipOffset+16:ipOffset+20], targetIPBytes)

		// 重新计算IP头校验和
		if err := updateIPChecksumAtOffset(modifiedPacket, ipOffset); err != nil {
			log.Printf("[TAP] 更新IP校验和失败: %v", err)
		}

		// 重新计算TCP校验和（如果是TCP包）
		if packet[ipOffset+9] == 6 && len(modifiedPacket) >= ipOffset+40 { // TCP协议
			if err := updateTCPChecksumAtOffset(modifiedPacket, ipOffset); err != nil {
				log.Printf("[TAP] 更新TCP校验和失败: %v", err)
			}
		}
	}

	return modifiedPacket, nil
}

// runCommand 执行系统命令
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("命令 '%s %v' 执行失败: %v, 输出: %s", name, args, err, string(output))
	}
	return nil
}

// isValidInterfaceName 验证接口名称是否有效
func isValidInterfaceName(name string) bool {
	if len(name) == 0 || len(name) > 15 {
		return false
	}

	for _, c := range name {
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_') {
			return false
		}
	}

	return true
}

// parseIPAddress 解析IP地址字符串为字节数组
func parseIPAddress(ipStr string) []byte {
	parts := strings.Split(ipStr, ".")
	if len(parts) != 4 {
		return nil
	}

	ip := make([]byte, 4)
	for i, part := range parts {
		val := 0
		for _, c := range part {
			if c < '0' || c > '9' {
				return nil
			}
			val = val*10 + int(c-'0')
		}
		if val > 255 {
			return nil
		}
		ip[i] = byte(val)
	}
	return ip
}

// parseMACAddress 解析MAC地址字符串为字节数组
func parseMACAddress(macStr string) []byte {
	parts := strings.Split(macStr, ":")
	if len(parts) != 6 {
		return nil
	}

	mac := make([]byte, 6)
	for i, part := range parts {
		val := 0
		for _, c := range part {
			if c >= '0' && c <= '9' {
				val = val*16 + int(c-'0')
			} else if c >= 'a' && c <= 'f' {
				val = val*16 + int(c-'a'+10)
			} else if c >= 'A' && c <= 'F' {
				val = val*16 + int(c-'A'+10)
			} else {
				return nil
			}
		}
		if val > 255 {
			return nil
		}
		mac[i] = byte(val)
	}
	return mac
}

// getTapMACByName 根据TAP设备名称获取对应的MAC地址字符串
func getTapMACByName(tapName string) string {
	switch tapName {
	case "tap-normal":
		return TAP_NORMAL_MAC
	case "tap-vxlan":
		return TAP_VXLAN_MAC
	default:
		return TAP_NORMAL_MAC
	}
}

// getDestinationMAC 获取数据包的目标MAC地址
func getDestinationMAC(packet []byte) string {
	if len(packet) < 6 {
		return "unknown"
	}

	// 提取目标MAC (以太网头前6字节)
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		packet[0], packet[1], packet[2], packet[3], packet[4], packet[5])
}

// updateIPChecksumAtOffset 在指定偏移位置更新IP头校验和
func updateIPChecksumAtOffset(packet []byte, ipOffset int) error {
	if len(packet) < ipOffset+20 {
		return fmt.Errorf("数据包太短")
	}

	// 检查IP头长度 (IP头第一个字节的低4位)
	ipHeaderLen := int(packet[ipOffset]&0x0F) * 4
	if ipHeaderLen < 20 || len(packet) < ipOffset+ipHeaderLen {
		return fmt.Errorf("IP头长度无效: %d", ipHeaderLen)
	}

	// 清零校验和字段 (IP头偏移10-11字节)
	packet[ipOffset+10] = 0
	packet[ipOffset+11] = 0

	// 计算IP头校验和
	checksum := uint32(0)
	for i := ipOffset; i < ipOffset+ipHeaderLen; i += 2 {
		if i+1 < len(packet) {
			checksum += uint32(packet[i])<<8 + uint32(packet[i+1])
		} else {
			checksum += uint32(packet[i]) << 8
		}
	}

	// 处理进位
	for checksum>>16 != 0 {
		checksum = (checksum & 0xFFFF) + (checksum >> 16)
	}

	// 取反
	checksum = ^checksum

	// 写入校验和
	packet[ipOffset+10] = byte(checksum >> 8)
	packet[ipOffset+11] = byte(checksum & 0xFF)

	return nil
}

// updateTCPChecksumAtOffset 在指定偏移位置更新TCP校验和
func updateTCPChecksumAtOffset(packet []byte, ipOffset int) error {
	if len(packet) < ipOffset+40 { // IP头20字节 + TCP头最小20字节
		return fmt.Errorf("数据包太短，无法计算TCP校验和")
	}

	// 获取IP头长度
	ipHeaderLen := int(packet[ipOffset]&0x0F) * 4
	if ipHeaderLen < 20 {
		return fmt.Errorf("无效的IP头长度: %d", ipHeaderLen)
	}

	tcpOffset := ipOffset + ipHeaderLen
	if len(packet) < tcpOffset+20 {
		return fmt.Errorf("数据包太短，无法包含完整TCP头")
	}

	// 获取TCP头长度
	tcpHeaderLen := int(packet[tcpOffset+12]>>4) * 4
	if tcpHeaderLen < 20 {
		return fmt.Errorf("无效的TCP头长度: %d", tcpHeaderLen)
	}

	// 获取总IP包长度
	ipTotalLen := int(packet[ipOffset+2])<<8 | int(packet[ipOffset+3])
	tcpTotalLen := ipTotalLen - ipHeaderLen

	// 清零TCP校验和字段
	packet[tcpOffset+16] = 0
	packet[tcpOffset+17] = 0

	// 计算TCP伪头校验和
	checksum := uint32(0)

	// 源IP地址
	checksum += uint32(packet[ipOffset+12])<<8 + uint32(packet[ipOffset+13])
	checksum += uint32(packet[ipOffset+14])<<8 + uint32(packet[ipOffset+15])

	// 目标IP地址
	checksum += uint32(packet[ipOffset+16])<<8 + uint32(packet[ipOffset+17])
	checksum += uint32(packet[ipOffset+18])<<8 + uint32(packet[ipOffset+19])

	// 协议号 (TCP = 6)
	checksum += 6

	// TCP长度
	checksum += uint32(tcpTotalLen)

	// TCP头和数据
	for i := tcpOffset; i < tcpOffset+tcpTotalLen; i += 2 {
		if i+1 < len(packet) {
			checksum += uint32(packet[i])<<8 + uint32(packet[i+1])
		} else {
			checksum += uint32(packet[i]) << 8
		}
	}

	// 处理进位
	for checksum>>16 != 0 {
		checksum = (checksum & 0xFFFF) + (checksum >> 16)
	}

	// 取反
	checksum = ^checksum

	// 写入TCP校验和
	packet[tcpOffset+16] = byte(checksum >> 8)
	packet[tcpOffset+17] = byte(checksum & 0xFF)

	return nil
}
