package dpdknet

import (
	"fmt"
	"log"
	"os/exec"
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

	// 创建VXLAN数据TAP
	vxlanTap, err := NewTapDevice("tap-vxlan", bridge)
	if err != nil {
		normalTap.Close()
		return nil, fmt.Errorf("创建VXLAN数据TAP失败: %v", err)
	}
	manager.vxlanTap = vxlanTap

	// 设置回调函数
	normalTap.SetPacketHandler(manager.handleNormalPacket)
	vxlanTap.SetPacketHandler(manager.handleVXLANPacket)

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

	n, err := t.iface.Write(packet)
	if err != nil {
		return fmt.Errorf("发送数据包到TAP设备失败: %v", err)
	}

	if n != len(packet) {
		return fmt.Errorf("数据包发送不完整: 期望 %d 字节，实际发送 %d 字节", len(packet), n)
	}

	t.mutex.Lock()
	t.txPackets++
	t.mutex.Unlock()

	log.Printf("[TAP] 发送数据包到 %s: %d字节", t.name, len(packet))
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
	tm.vxlanTap.Stop()
	log.Printf("[TAP] TAP管理器已停止")
}

// Close 关闭TAP管理器
func (tm *TapManager) Close() error {
	var firstErr error

	if err := tm.normalTap.Close(); err != nil {
		firstErr = err
		log.Printf("[TAP] 关闭正常TAP失败: %v", err)
	}

	if err := tm.vxlanTap.Close(); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		log.Printf("[TAP] 关闭VXLAN TAP失败: %v", err)
	}

	return firstErr
}

// SendToNormalTap 发送数据包到正常TAP设备
func (tm *TapManager) SendToNormalTap(packet []byte) error {
	return tm.normalTap.SendPacket(packet)
}

// SendToVXLANTap 发送数据包到VXLAN TAP设备
func (tm *TapManager) SendToVXLANTap(packet []byte) error {
	return tm.vxlanTap.SendPacket(packet)
}

// GetNormalTapName 获取正常TAP设备名称
func (tm *TapManager) GetNormalTapName() string {
	return tm.normalTap.GetName()
}

// GetVXLANTapName 获取VXLAN TAP设备名称
func (tm *TapManager) GetVXLANTapName() string {
	return tm.vxlanTap.GetName()
}

// GetBridgeName 获取网桥名称
func (tm *TapManager) GetBridgeName() string {
	return tm.bridge
}

// handleNormalPacket 处理来自正常TAP的数据包
func (tm *TapManager) handleNormalPacket(packet []byte) error {
	log.Printf("[TAP] 收到正常TAP数据包: %d字节", len(packet))
	// 将数据包发送到DPDK
	return SendRawBytes(packet)
}

// handleVXLANPacket 处理来自VXLAN TAP的数据包
func (tm *TapManager) handleVXLANPacket(packet []byte) error {
	log.Printf("[TAP] 收到VXLAN TAP数据包: %d字节", len(packet))

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
