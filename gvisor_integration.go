package dpdknet

import (
	"fmt"
	"log"
	"net"
)

// GVisorNetworkManager 管理 gVisor 与 DPDK 的集成
type GVisorNetworkManager struct {
	netstack *GVisorNetstack
	enabled  bool
}

var globalGVisorManager *GVisorNetworkManager

// EnableGVisorNetstack 启用 gVisor 作为主要协议栈
func EnableGVisorNetstack(localIP net.IP, localMAC [6]byte) error {
	if globalGVisorManager != nil && globalGVisorManager.enabled {
		log.Printf("[INFO] gVisor netstack already enabled")
		return nil
	}

	// 初始化 gVisor netstack
	if err := IntegrateGVisorWithDPDK(localIP, localMAC); err != nil {
		return fmt.Errorf("failed to integrate gVisor with DPDK: %v", err)
	}

	globalGVisorManager = &GVisorNetworkManager{
		netstack: GetGVisorNetstack(),
		enabled:  true,
	}

	log.Printf("[INFO] gVisor netstack enabled as primary protocol stack")
	return nil
}

// IsGVisorEnabled 检查 gVisor 是否已启用
func IsGVisorEnabled() bool {
	// 首先检查管理器状态
	if globalGVisorManager != nil && globalGVisorManager.enabled {
		return true
	}

	// 然后检查实际的 netstack 是否存在
	return GetGVisorNetstack() != nil
}

// CreateTCPListener 创建 TCP 监听器（强制使用 gVisor）
func CreateTCPListener(port uint16) (net.Listener, error) {
	return CreateGVisorTCPListener(port)
}

// CreateUDPConn 创建 UDP 连接（强制使用 gVisor）
func CreateUDPConn(port uint16) (net.PacketConn, error) {
	return CreateGVisorUDPConn(port)
}

// GetProtocolStackInfo 获取当前协议栈信息
func GetProtocolStackInfo() map[string]interface{} {
	info := map[string]interface{}{
		"gvisor_enabled": IsGVisorEnabled(),
		"protocol_stack": "gvisor", // 强制使用 gVisor
	}

	if stats := GetNetworkStats(); stats != nil {
		info["stats"] = *stats
	}

	return info
}

// ProcessIncomingPacket 处理传入的数据包
func ProcessIncomingPacket(data []byte) error {
	return ProcessDPDKPacket(data)
}

// SetNetworkMode 设置网络模式
type NetworkMode string

const (
	NetworkModeGVisor NetworkMode = "gvisor"
	NetworkModeLegacy NetworkMode = "legacy"
	NetworkModeAuto   NetworkMode = "auto"
)

// SetNetworkMode 设置网络处理模式（已简化为只支持gVisor）
func SetNetworkMode(mode NetworkMode, localIP net.IP, localMAC [6]byte) error {
	switch mode {
	case NetworkModeGVisor, NetworkModeAuto:
		return EnableGVisorNetstack(localIP, localMAC)
	case NetworkModeLegacy:
		log.Printf("[WARNING] Legacy mode no longer supported, using gVisor instead")
		return EnableGVisorNetstack(localIP, localMAC)
	default:
		return fmt.Errorf("unknown network mode: %s", mode)
	}
}
