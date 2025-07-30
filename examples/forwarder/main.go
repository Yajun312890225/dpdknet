package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.Println("启动DPDK数据包分流转发程序 (使用TAP设备)")

	// 创建TAP设备和数据包转发器
	forwarder, err := dpdknet.NewPacketForwarder("br1")
	if err != nil {
		log.Fatalf("创建数据包转发器失败: %v", err)
	}
	defer forwarder.Close()

	// 添加过滤器

	// 1. VXLAN包过滤器 - 所有VXLAN包都自己处理
	vxlanFilter := dpdknet.NewVXLANFilter("VXLAN过滤器", 66)
	forwarder.AddFilter(vxlanFilter)

	// 2. 特定IP过滤器 - 处理特定IP的包
	targetIPs := []string{"10.10.10.4", "192.168.66.57"}
	targetNetworks := []string{"10.10.10.0/24"}
	ipFilter := dpdknet.NewIPFilter("IP过滤器", targetIPs, targetNetworks)
	forwarder.AddFilter(ipFilter)

	// 3. 端口过滤器 - 处理特定端口的包
	targetPorts := []uint16{4789, 22, 80, 443} // VXLAN, SSH, HTTP, HTTPS
	portFilter := dpdknet.NewPortFilter("端口过滤器", targetPorts)
	forwarder.AddFilter(portFilter)

	// 初始化VXLAN配置
	vxlanConfig := &dpdknet.VXLANConfig{
		VNI:       66,
		LocalIP:   net.IPv4(192, 168, 66, 57),
		RemoteIP:  net.IPv4(192, 168, 66, 29),
		UDPPort:   4789,
		LocalMAC:  net.HardwareAddr{0x20, 0x90, 0x6f, 0x6b, 0x63, 0x8a},
		RemoteMAC: net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d},
		ARPCache:  make(map[string]net.HardwareAddr),
	}

	// 设置全局VXLAN配置
	dpdknet.SetGlobalVXLANConfig(vxlanConfig)

	// 创建VXLAN处理器
	vxlanHandler := dpdknet.NewVXLANHandler(vxlanConfig)
	dpdknet.SetGlobalVXLANHandler(vxlanHandler)

	// 创建ARP处理器
	arpHandler := dpdknet.NewARPHandler(
		net.IPv4(10, 10, 10, 4),                              // 内层IP
		net.HardwareAddr{0x20, 0x90, 0x6f, 0x6b, 0x63, 0x8a}, // 内层MAC
	)
	dpdknet.SetGlobalVXLANARPHandler(arpHandler)

	// 启动转发器
	if err := forwarder.Start(); err != nil {
		log.Fatalf("启动转发器失败: %v", err)
	}

	log.Println("数据包转发器启动成功")
	log.Println("配置信息:")
	log.Printf("  - Normal TAP设备: %s", forwarder.GetNormalTapName())
	log.Printf("  - VXLAN TAP设备: %s", forwarder.GetVXLANTapName())
	log.Printf("  - 网桥: %s", forwarder.GetBridgeName())
	log.Printf("  - 内层IP: %s", arpHandler.GetLocalIP().String())

	log.Println("\n工作流程:")
	log.Println("  1. DPDK从eth0收包")
	log.Println("  2. 应用过滤器判断是否自己处理")
	log.Println("  3. 自己处理的包：VXLAN解封装、ARP处理等")
	log.Println("  4. 普通数据包：转发到tap-normal设备")
	log.Println("  5. VXLAN数据包：转发到tap-vxlan设备")
	log.Println("  6. 监听TAP设备，收到的包通过DPDK发出")

	// 启动统计信息打印
	go statsLoop(forwarder)

	// 模拟DPDK包处理
	go simulateDPDKPackets(forwarder)

	// 等待信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("\n程序运行中，按Ctrl+C退出...")
	<-sigChan

	log.Println("正在关闭程序...")
	forwarder.Stop()

	log.Println("程序已退出")
}

// statsLoop 统计信息打印循环
func statsLoop(forwarder *dpdknet.PacketForwarder) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		processed, forwarded, dropped := forwarder.GetStats()
		log.Printf("[统计] 处理: %d, 转发到TAP: %d, 丢弃: %d",
			processed, forwarded, dropped)
	}
}

// simulateDPDKPackets 模拟DPDK包接收
func simulateDPDKPackets(forwarder *dpdknet.PacketForwarder) {
	log.Println("[模拟] 开始模拟DPDK包接收...")

	// 模拟不同类型的包
	packets := []struct {
		name string
		data []byte
	}{
		{
			name: "VXLAN包",
			data: buildVXLANPacket(),
		},
		{
			name: "ARP包",
			data: buildARPPacket(),
		},
		{
			name: "普通IP包",
			data: buildIPPacket(),
		},
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	count := 0
	for range ticker.C {
		packet := packets[count%len(packets)]
		log.Printf("[模拟] 收到%s，长度: %d字节", packet.name, len(packet.data))

		if err := forwarder.ProcessDPDKPacket(packet.data); err != nil {
			log.Printf("[模拟] 处理包失败: %v", err)
		}

		count++
	}
}

// buildVXLANPacket 构建模拟VXLAN包
func buildVXLANPacket() []byte {
	// 简化的VXLAN包结构
	packet := make([]byte, 100)

	// 以太网头
	copy(packet[0:6], []byte{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d})  // 目标MAC
	copy(packet[6:12], []byte{0x20, 0x90, 0x6f, 0x6b, 0x63, 0x8a}) // 源MAC
	packet[12] = 0x08                                              // EtherType: IPv4
	packet[13] = 0x00

	// IP头（简化）
	packet[14] = 0x45 // Version + IHL
	packet[23] = 17   // Protocol: UDP

	// UDP头
	packet[36] = 0x12 // 源端口高字节
	packet[37] = 0xb5 // 源端口低字节
	packet[38] = 0x12 // 目标端口高字节 (4789)
	packet[39] = 0xb5 // 目标端口低字节

	return packet
}

// buildARPPacket 构建模拟ARP包
func buildARPPacket() []byte {
	packet := make([]byte, 60)

	// 以太网头
	copy(packet[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})  // 广播MAC
	copy(packet[6:12], []byte{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}) // 源MAC
	packet[12] = 0x08                                              // EtherType: ARP
	packet[13] = 0x06

	return packet
}

// buildIPPacket 构建模拟普通IP包
func buildIPPacket() []byte {
	packet := make([]byte, 60)

	// 以太网头
	copy(packet[0:6], []byte{0x20, 0x90, 0x6f, 0x6b, 0x63, 0x8a})  // 目标MAC
	copy(packet[6:12], []byte{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}) // 源MAC
	packet[12] = 0x08                                              // EtherType: IPv4
	packet[13] = 0x00

	// IP头（简化）
	packet[14] = 0x45 // Version + IHL
	packet[23] = 6    // Protocol: TCP

	// 目标IP (不在过滤器范围内)
	packet[30] = 8 // 8.8.8.8
	packet[31] = 8
	packet[32] = 8
	packet[33] = 8

	return packet
}
