package dpdknet

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/songgao/water"
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
	bridge    string     // 网桥名称
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

	log.Printf("[TAP] 创建TAP设备成功: %s, 绑定到网桥: %s", name, bridge)
	return tap, nil
}

// NewTapManager 创建TAP管理器，同时创建两张TAP网卡
func NewTapManager(bridge string) (*TapManager, error) {
	manager := &TapManager{
		bridge: bridge,
	}

	// 创建正常数据TAP
	normalTap, err := NewTapDevice("tap-normal", bridge)
	if err != nil {
		return nil, fmt.Errorf("创建正常数据TAP失败: %v", err)
	}
	manager.normalTap = normalTap

	// // 创建VXLAN数据TAP
	// vxlanTap, err := NewTapDevice("tap-vxlan", bridge)
	// if err != nil {
	// 	normalTap.Close()
	// 	return nil, fmt.Errorf("创建VXLAN数据TAP失败: %v", err)
	// }
	// manager.vxlanTap = vxlanTap

	// 设置回调函数
	normalTap.SetPacketHandler(manager.handleNormalPacket)
	// vxlanTap.SetPacketHandler(manager.handleVXLANPacket)

	log.Printf("[TAP] TAP管理器创建成功，绑定到网桥: %s", bridge)
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

	// 为TAP设备分配IP地址
	// ipAddr := getTapIPAddress(t.name)
	// if ipAddr != "" {
	// 	if err := runCommand("ip", "addr", "add", ipAddr, "dev", t.name); err != nil {
	// 		log.Printf("[TAP] 为TAP设备分配IP失败: %v", err)
	// 	} else {
	// 		log.Printf("[TAP] 为TAP设备分配IP成功: %s -> %s", t.name, ipAddr)
	// 	}
	// }

	// 添加到网桥
	if err := runCommand("brctl", "addif", t.bridge, t.name); err != nil {
		// 如果失败，尝试创建网桥后再添加
		log.Printf("[TAP] 添加到网桥失败，尝试创建网桥: %s", t.bridge)
		if err := runCommand("brctl", "addbr", t.bridge); err != nil {
			log.Printf("[TAP] 创建网桥失败: %v", err)
		}
		if err := runCommand("ip", "link", "set", t.bridge, "up"); err != nil {
			log.Printf("[TAP] 启动网桥失败: %v", err)
		}
		// 再次尝试添加到网桥
		if err := runCommand("brctl", "addif", t.bridge, t.name); err != nil {
			return fmt.Errorf("添加TAP接口到网桥失败: %v", err)
		}
	}

	log.Printf("[TAP] TAP设备配置完成: %s -> %s", t.name, t.bridge)

	// 配置路由，确保回包能正确路由
	if err := t.configureRouting(); err != nil {
		log.Printf("[TAP] 配置路由失败: %v", err)
	}

	return nil
}

// configureRouting 配置TAP设备路由
func (t *TapDevice) configureRouting() error {
	tapIP := t.GetTapIP()
	if tapIP == "" {
		return nil // 如果没有IP，跳过路由配置
	}

	// 添加本地路由，确保目标为172.16.1.1的包能路由到TAP设备
	if err := runCommand("ip", "route", "add", "172.16.1.1/32", "dev", t.name); err != nil {
		log.Printf("[TAP] 添加路由失败，可能已存在: %v", err)
	} else {
		log.Printf("[TAP] 添加路由成功: 172.16.1.1/32 -> %s", t.name)
	}

	// 启用IP转发
	if err := runCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		log.Printf("[TAP] 启用IP转发失败: %v", err)
	}

	// 禁用反向路径过滤，避免包被丢弃
	if err := runCommand("sysctl", "-w", fmt.Sprintf("net.ipv4.conf.%s.rp_filter=0", t.name)); err != nil {
		log.Printf("[TAP] 禁用反向路径过滤失败: %v", err)
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

	// 调试：输出原始数据包信息
	log.Printf("[TAP] 发送原始数据包到 %s: 长度=%d, 前64字节=%s",
		t.name, len(packet), dumpPacketHex(packet, 64))

	// 分析TCP包
	analyzeTCPPacket(packet, "OUT")

	// 修改数据包的目标IP为172.16.1.1，目标MAC为aa:c3:49:f5:a4:33
	modifiedPacket, err := t.modifyPacketHeaders(packet)
	if err != nil {
		log.Printf("[TAP] 修改数据包头失败: %v", err)
		modifiedPacket = packet // 如果修改失败，使用原始包
	}

	// 调试：输出修改后的数据包信息
	if len(modifiedPacket) != len(packet) || dumpPacketHex(modifiedPacket, 64) != dumpPacketHex(packet, 64) {
		log.Printf("[TAP] 修改后数据包: 长度=%d, 前64字节=%s",
			len(modifiedPacket), dumpPacketHex(modifiedPacket, 64))
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

	// log.Printf("[TAP] 发送数据包到 %s: %d字节", t.name, len(modifiedPacket))
	return nil
}

// Close 关闭TAP设备
func (t *TapDevice) Close() error {
	t.Stop()

	// 清理IP地址配置
	// ipAddr := getTapIPAddress(t.name)
	// if ipAddr != "" {
	// 	if err := runCommand("ip", "addr", "del", ipAddr, "dev", t.name); err != nil {
	// 		log.Printf("[TAP] 清理TAP设备IP失败: %v", err)
	// 	} else {
	// 		log.Printf("[TAP] 清理TAP设备IP成功: %s -> %s", t.name, ipAddr)
	// 	}
	// }

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

	// if err := tm.vxlanTap.Start(); err != nil {
	// 	tm.normalTap.Stop()
	// 	return fmt.Errorf("启动VXLAN TAP失败: %v", err)
	// }

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

// GetBridgeName 获取网桥名称
func (tm *TapManager) GetBridgeName() string {
	return tm.bridge
}

// GetNormalTapIP 获取正常TAP设备的IP地址
func (tm *TapManager) GetNormalTapIP() string {
	return tm.normalTap.GetTapIP()
}

// GetVXLANTapIP 获取VXLAN TAP设备的IP地址
func (tm *TapManager) GetVXLANTapIP() string {
	if tm.vxlanTap == nil {
		return ""
	}
	return tm.vxlanTap.GetTapIP()
}

// GetTapStats 获取TAP设备统计信息
func (tm *TapManager) GetTapStats() (normalRx, normalTx, vxlanRx, vxlanTx uint64) {
	normalRx, normalTx = tm.normalTap.GetStats()
	if tm.vxlanTap != nil {
		vxlanRx, vxlanTx = tm.vxlanTap.GetStats()
	}
	return
}

// handleNormalPacket 处理来自正常TAP的数据包
func (tm *TapManager) handleNormalPacket(packet []byte) error {
	log.Printf("[TAP] 收到正常TAP数据包: %d字节, 前32字节=%s",
		len(packet), dumpPacketHex(packet, 32))

	// 分析TCP包
	analyzeTCPPacket(packet, "IN")

	// 分析包类型
	if len(packet) >= 14 {
		etherType := uint16(packet[12])<<8 | uint16(packet[13])
		if etherType == 0x0800 && len(packet) >= 34 {
			// IPv4包
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

			log.Printf("[TAP] 从TAP收到%s包: %s -> %s", protocolName, srcIP, dstIP)
		}
	}

	// 将数据包发送到DPDK
	return SendRawBytes(packet)
}

// handleVXLANPacket 处理来自VXLAN TAP的数据包
func (tm *TapManager) handleVXLANPacket(packet []byte) error {
	// log.Printf("[TAP] 收到VXLAN TAP数据包: %d字节", len(packet))

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
	// 固定目标IP地址为172.16.1.1
	targetIP := "172.16.1.1"
	// 固定目标MAC地址为aa:c3:49:f5:a4:33
	targetMAC := []byte{0xaa, 0xc3, 0x49, 0xf5, 0xa4, 0x33}

	// 检查包长度是否足够 (以太网头14字节)
	if len(packet) < 14 {
		return packet, fmt.Errorf("数据包太短，无法修改以太网头部: %d字节", len(packet))
	}

	// 复制原始包
	modifiedPacket := make([]byte, len(packet))
	copy(modifiedPacket, packet)

	// 获取原始MAC地址用于日志
	originalDestMAC := getDestinationMAC(packet)

	// 修改目标MAC地址 (以太网头前6字节)
	copy(modifiedPacket[0:6], targetMAC)

	// 检查多个可能的EtherType位置
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	ipOffset := -1

	log.Printf("[TAP] 数据包分析: 长度=%d, EtherType@12-13=0x%04x, 原始目标MAC=%s",
		len(packet), etherType, originalDestMAC)

	// 尝试在不同位置查找IPv4头部 (0x45表示IPv4且头长度为20字节)
	for i := 14; i <= 18 && i < len(packet); i++ {
		if packet[i] == 0x45 && i+20 < len(packet) {
			// 检查是否真的是IP包 (查看协议字段等)
			if packet[i+9] == 0x06 || packet[i+9] == 0x11 || packet[i+9] == 0x01 { // TCP/UDP/ICMP
				ipOffset = i
				log.Printf("[TAP] 发现IPv4头部在偏移 %d", ipOffset)
				break
			}
		}
	}

	// 如果找到了IP头，修改IP地址
	if ipOffset >= 0 && ipOffset+20 <= len(packet) {
		// 获取原始目标IP地址用于日志
		originalDestIP := fmt.Sprintf("%d.%d.%d.%d",
			packet[ipOffset+16], packet[ipOffset+17], packet[ipOffset+18], packet[ipOffset+19])

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

		log.Printf("[TAP] 修改数据包头: MAC %s->aa:c3:49:f5:a4:33, IP %s->%s (TAP: %s)",
			originalDestMAC, originalDestIP, targetIP, t.name)
	} else {
		log.Printf("[TAP] 修改目标MAC地址: %s->aa:c3:49:f5:a4:33 (未找到IP头, TAP: %s)",
			originalDestMAC, t.name)
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

// getDestinationIP 获取数据包的目标IP地址
func getDestinationIP(packet []byte) string {
	if len(packet) < 34 {
		return "unknown"
	}

	// 检查是否是IP包
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType != 0x0800 {
		return "non-IP"
	}

	// 提取目标IP (IP头第16-19字节)
	return fmt.Sprintf("%d.%d.%d.%d", packet[30], packet[31], packet[32], packet[33])
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

// dumpPacketHex 输出数据包的十六进制内容 (用于调试)
func dumpPacketHex(packet []byte, maxLen int) string {
	length := len(packet)
	if maxLen > 0 && length > maxLen {
		length = maxLen
	}

	var hex strings.Builder
	for i := 0; i < length; i++ {
		hex.WriteString(fmt.Sprintf("%02x", packet[i]))
	}
	if len(packet) > length {
		hex.WriteString("...")
	}
	return hex.String()
}

// updateIPChecksum 更新IP头校验和
func updateIPChecksum(packet []byte) error {
	return updateIPChecksumAtOffset(packet, 14)
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

// 测试函数：分析提供的数据包格式
func analyzeTestPacket() {
	// 您提供的测试数据包
	hexData := "000400010006feee8996aca30000080045a0003c203b4000fb060613abd5ffe6ac1001015731270f1a70cdbf00000000a002faf0ed3300000204058c0402080a7a42d0b20000000001030307"

	// 将十六进制字符串转换为字节数组
	packet := make([]byte, len(hexData)/2)
	for i := 0; i < len(hexData); i += 2 {
		var b byte
		fmt.Sscanf(hexData[i:i+2], "%02x", &b)
		packet[i/2] = b
	}

	log.Printf("=== 数据包分析 ===")
	log.Printf("总长度: %d 字节", len(packet))

	if len(packet) >= 14 {
		log.Printf("目标MAC: %02x:%02x:%02x:%02x:%02x:%02x",
			packet[0], packet[1], packet[2], packet[3], packet[4], packet[5])
		log.Printf("源MAC: %02x:%02x:%02x:%02x:%02x:%02x",
			packet[6], packet[7], packet[8], packet[9], packet[10], packet[11])

		etherType := uint16(packet[12])<<8 | uint16(packet[13])
		log.Printf("EtherType: 0x%04x", etherType)

		if etherType == 0x0800 && len(packet) >= 34 {
			log.Printf("IPv4包检测到")
			log.Printf("源IP: %d.%d.%d.%d", packet[26], packet[27], packet[28], packet[29])
			log.Printf("目标IP: %d.%d.%d.%d", packet[30], packet[31], packet[32], packet[33])
		}
	}
	log.Printf("==================")
}

// analyzeTCPPacket 分析TCP包的详细信息
func analyzeTCPPacket(packet []byte, direction string) {
	if len(packet) < 54 { // 以太网14 + IP20 + TCP20
		return
	}

	// 检查是否是IP包
	etherType := uint16(packet[12])<<8 | uint16(packet[13])
	if etherType != 0x0800 {
		return
	}

	// 检查是否是TCP包
	if packet[23] != 6 {
		return
	}

	// 提取IP地址
	srcIP := fmt.Sprintf("%d.%d.%d.%d", packet[26], packet[27], packet[28], packet[29])
	dstIP := fmt.Sprintf("%d.%d.%d.%d", packet[30], packet[31], packet[32], packet[33])

	// 提取TCP端口
	srcPort := uint16(packet[34])<<8 | uint16(packet[35])
	dstPort := uint16(packet[36])<<8 | uint16(packet[37])

	// 提取TCP序列号和确认号
	seqNum := uint32(packet[38])<<24 | uint32(packet[39])<<16 | uint32(packet[40])<<8 | uint32(packet[41])
	ackNum := uint32(packet[42])<<24 | uint32(packet[43])<<16 | uint32(packet[44])<<8 | uint32(packet[45])

	// 提取TCP标志
	flags := packet[47]
	flagStr := ""
	if flags&0x02 != 0 {
		flagStr += "SYN "
	}
	if flags&0x10 != 0 {
		flagStr += "ACK "
	}
	if flags&0x01 != 0 {
		flagStr += "FIN "
	}
	if flags&0x04 != 0 {
		flagStr += "RST "
	}
	if flags&0x08 != 0 {
		flagStr += "PSH "
	}
	if flags&0x20 != 0 {
		flagStr += "URG "
	}

	log.Printf("[TCP-%s] %s:%d -> %s:%d, Seq=%d, Ack=%d, Flags=[%s]",
		direction, srcIP, srcPort, dstIP, dstPort, seqNum, ackNum, strings.TrimSpace(flagStr))
}
