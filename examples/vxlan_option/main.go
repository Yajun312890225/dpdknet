package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	// 示例1: TCP 服务器带 VXLAN 封装 (新的可变参数方式)
	// tcpServerExample()

	// 示例2: UDP 服务器带 VXLAN 封装
	// udpServerExample()

	// 示例3: TCP 客户端带 VXLAN 封装
	tcpClientExample()

	// 示例4: ICMP 客户端带 VXLAN 封装 (TODO: 实现)
	// icmpClientExample()
}

func tcpServerExample() {
	fmt.Println("=== TCP Server with VXLAN Example ===")

	// 创建 VXLAN 配置
	vxlanConfig := &dpdknet.VXLANConfig{
		VNI:       1000,
		LocalIP:   net.ParseIP("11.74.225.249"), // 本地 VTEP IP
		RemoteIP:  net.ParseIP("10.64.12.7"),    // 远程 VTEP IP
		UDPPort:   4789,                         // VXLAN 端口
		LocalMAC:  net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		RemoteMAC: net.HardwareAddr{0x00, 0x66, 0x77, 0x88, 0x99, 0xaa},
	}

	// 使用新的 Option 风格 - VXLAN 作为选项
	listener, err := dpdknet.Listen("tcp", ":8080", dpdknet.WithVXLAN(vxlanConfig))
	if err != nil {
		log.Fatalf("Failed to create TCP listener with VXLAN: %v", err)
	}
	defer listener.Close()

	fmt.Println("TCP server with VXLAN listening on :8080 (Option 风格)")
	fmt.Printf("VXLAN VNI: %d, Local VTEP: %s, Remote VTEP: %s\n",
		vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)

	// 接受连接（在真实应用中，这里会有循环处理）
	// go func() {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		// 处理连接
		go handleTCPConnection(conn)
	}
	// }()

	// time.S`leep(1 * time.Second) // 让服务器运行一会儿
}

func udpServerExample() {
	fmt.Println("\n=== UDP Server with VXLAN Example ===")

	// 创建 VXLAN 配置
	vxlanConfig := &dpdknet.VXLANConfig{
		VNI:       2000,
		LocalIP:   net.ParseIP("192.168.1.10"),
		RemoteIP:  net.ParseIP("192.168.1.20"),
		UDPPort:   4789,
		LocalMAC:  net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		RemoteMAC: net.HardwareAddr{0x00, 0x66, 0x77, 0x88, 0x99, 0xaa},
	}

	// 使用新的 Option 风格
	conn, err := dpdknet.ListenPacket("udp", ":9090", dpdknet.WithVXLAN(vxlanConfig))
	if err != nil {
		log.Fatalf("Failed to create UDP connection with VXLAN: %v", err)
	}
	defer conn.Close()

	fmt.Println("UDP server with VXLAN listening on :9090 (Option 风格)")
	fmt.Printf("VXLAN VNI: %d, Local VTEP: %s, Remote VTEP: %s\n",
		vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)

	// 模拟处理 UDP 数据
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				log.Printf("UDP read error: %v", err)
				continue
			}

			fmt.Printf("Received UDP data from %s: %s\n", addr, string(buffer[:n]))
		}
	}()

	time.Sleep(1 * time.Second)
}

func tcpClientExample() {
	fmt.Println("\n=== TCP Client with VXLAN Example ===")

	// 设置环境变量确保 DPDK 和 gVisor 使用正确的本地 IP
	localVTEP := "192.168.66.57"
	os.Setenv("DPDKNET_LOCAL_IP", localVTEP)
	fmt.Printf("设置 DPDKNET_LOCAL_IP=%s\n", localVTEP)

	// 确保在创建连接前先初始化网络系统（这会使用新的环境变量）
	if err := dpdknet.EnsureGlobalNetworkInit(); err != nil {
		log.Fatalf("网络初始化失败: %v", err)
	}

	// 首先检查网络连通性
	fmt.Println("1. 检查网络连通性...")
	testNetworkConnectivity()

	// 创建 VXLAN 配置
	vxlanConfig := &dpdknet.VXLANConfig{
		VNI:       66,
		LocalIP:   net.ParseIP("192.168.66.57"),                         // 外层本地VTEP IP
		RemoteIP:  net.ParseIP("192.168.66.29"),                         // 外层远程VTEP IP
		UDPPort:   4789,                                                 // VXLAN 端口
		LocalMAC:  net.HardwareAddr{0x11, 0x22, 0x33, 0x00, 0x00, 0x00}, // 内层本地MAC
		RemoteMAC: net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}, // 内层远程MAC (固定)
	}

	// 验证 VXLAN 配置
	fmt.Println("2. 验证 VXLAN 配置...")
	validateVXLANConfig(vxlanConfig)

	// 使用新的 Option 风格创建VXLAN连接
	fmt.Println("\n3. 创建 VXLAN UDP 连接...")
	conn, err := dpdknet.Dial("udp", "8.137.60.31:12345",
		dpdknet.WithVXLAN(vxlanConfig))
	if err != nil {
		log.Fatalf("Failed to dial UDP with VXLAN: %v", err)
	}
	defer conn.Close()

	fmt.Println("UDP client connected with VXLAN to 10.10.10.1:12345")
	fmt.Printf("VXLAN VNI: %d, Local VTEP: %s, Remote VTEP: %s\n",
		vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)

	// 发送数据
	fmt.Println("4. 发送测试数据...")
	message := "Hello from VXLAN client!"

	fmt.Printf("  准备发送消息: %s\n", message)
	fmt.Printf("  使用 VXLAN 配置:\n")
	fmt.Printf("    本地 VTEP: %s (MAC: %s)\n", vxlanConfig.LocalIP, vxlanConfig.LocalMAC)
	fmt.Printf("    远程 VTEP: %s (MAC: %s)\n", vxlanConfig.RemoteIP, vxlanConfig.RemoteMAC)
	fmt.Printf("    VNI: %d, UDP 端口: %d\n", vxlanConfig.VNI, vxlanConfig.UDPPort)

	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Printf("Write error: %v", err)
		return
	}

	fmt.Printf("  ✅ 数据写入成功\n")

	// 等待一段时间让数据包发送完成
	time.Sleep(500 * time.Millisecond)
	fmt.Println("8. 数据发送完成，完整的网络协议栈测试完成")
}

func handleTCPConnection(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		fmt.Printf("Received TCP data: %s\n", string(buffer[:n]))

		// 回应
		response := fmt.Sprintf("Echo: %s", string(buffer[:n]))
		_, err = conn.Write([]byte(response))
		if err != nil {
			log.Printf("Write error: %v", err)
			return
		}
	}
}

func testNetworkConnectivity() {
	// 测试到远程 VTEP 的连通性
	remoteVTEP := "192.168.66.29"
	fmt.Printf("  测试到远程 VTEP %s 的连通性...\n", remoteVTEP)

	// 使用系统 ping 命令
	cmd := exec.Command("ping", "-c", "3", "-W", "2", remoteVTEP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ❌ PING 失败: %v\n", err)
		fmt.Printf("  输出: %s\n", string(output))
	} else {
		fmt.Printf("  ✅ PING 成功\n")
	}

	// 检查 ARP 表
	fmt.Printf("  检查 ARP 表中的 %s...\n", remoteVTEP)
	cmd = exec.Command("arp", "-n", remoteVTEP)
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ❌ ARP 查询失败: %v\n", err)
	} else {
		fmt.Printf("  ARP 信息: %s\n", string(output))
	}

	// 检查路由表
	fmt.Printf("  检查到 %s 的路由...\n", remoteVTEP)
	cmd = exec.Command("ip", "route", "get", remoteVTEP)
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ❌ 路由查询失败: %v\n", err)
	} else {
		fmt.Printf("  路由信息: %s\n", string(output))
	}
}

func validateVXLANConfig(config *dpdknet.VXLANConfig) {
	fmt.Printf("  VNI: %d\n", config.VNI)
	fmt.Printf("  本地 VTEP: %s\n", config.LocalIP)
	fmt.Printf("  远程 VTEP: %s\n", config.RemoteIP)
	fmt.Printf("  VXLAN 端口: %d\n", config.UDPPort)
	fmt.Printf("  本地 MAC: %s\n", config.LocalMAC)
	fmt.Printf("  远程 MAC: %s\n", config.RemoteMAC)

	// 检查远程端口是否开放
	fmt.Printf("  检查远程 VXLAN 端口 %s:%d...\n", config.RemoteIP, config.UDPPort)
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:%d", config.RemoteIP, config.UDPPort), 2*time.Second)
	if err != nil {
		fmt.Printf("  ❌ 无法连接到远程 VXLAN 端口: %v\n", err)
	} else {
		fmt.Printf("  ✅ 远程 VXLAN 端口可达\n")
		conn.Close()
	}
}

func isZeroMAC(mac net.HardwareAddr) bool {
	for _, b := range mac {
		if b != 0 {
			return false
		}
	}
	return true
}

func getLocalMAC(localIP string) net.HardwareAddr {
	// 获取所有网络接口
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("    ❌ 获取网络接口失败: %v\n", err)
		return net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	}

	targetIP := net.ParseIP(localIP)
	if targetIP == nil {
		fmt.Printf("    ❌ 无效的 IP 地址: %s\n", localIP)
		return net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	}

	// 遍历所有接口，查找匹配的 IP 地址
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && ip.Equal(targetIP) {
				fmt.Printf("    找到匹配的接口: %s\n", iface.Name)
				return iface.HardwareAddr
			}
		}
	}

	fmt.Printf("    ❌ 未找到匹配 IP %s 的接口，使用默认 MAC\n", localIP)
	return net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
}

func resolveRemoteMAC(remoteIP net.IP) net.HardwareAddr {
	// 先尝试 ping 来确保 ARP 表有记录
	fmt.Printf("    执行 ping 来触发 ARP...\n")
	exec.Command("ping", "-c", "1", "-W", "1", remoteIP.String()).Run()

	// 查询 ARP 表
	cmd := exec.Command("arp", "-n", remoteIP.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("    ❌ ARP 查询失败: %v\n", err)
		return net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	}

	// 解析 ARP 输出
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == remoteIP.String() {
			// 格式通常是: IP ... MAC ...
			for _, field := range fields {
				if strings.Contains(field, ":") && len(field) == 17 {
					mac, err := net.ParseMAC(field)
					if err == nil {
						return mac
					}
				}
			}
		}
	}

	fmt.Printf("    ❌ 未找到 ARP 记录，使用广播 MAC\n")
	return net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
}
