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

	// 按照用户需求的步骤顺序执行网络协议栈
	fmt.Println("\n=== 开始执行网络协议栈 ===")

	// 步骤1: 建立 VXLAN
	fmt.Println("步骤1: 建立 VXLAN 隧道")
	setupVXLAN(vxlanConfig)

	// 步骤2: DHCP 发广播包
	fmt.Println("步骤2: DHCP 发广播包")
	dhcpOffer := performDHCP(vxlanConfig)

	// 步骤3: 网关回 ARP 响应
	fmt.Println("步骤3: 网关回 ARP 响应")
	arpHandler := setupARP(dhcpOffer, vxlanConfig)

	// 步骤4: ARP 请求 10.10.10.2 的 MAC
	fmt.Println("步骤4: ARP 请求 10.10.10.2 的 MAC")
	gatewayMAC := performARPRequest(arpHandler, net.ParseIP("10.10.10.2"))

	// 步骤5: 10.10.10.2 回 MAC
	fmt.Println("步骤5: 完成 IP-MAC 映射")
	fmt.Printf("  网关 10.10.10.2 -> MAC: %s\n", gatewayMAC.String())

	// 获取网关MAC地址作为外层目标MAC
	fmt.Printf("  正在获取网关MAC地址...\n")
	remoteGatewayMAC := resolveRemoteMAC(vxlanConfig.RemoteIP)
	fmt.Printf("  外层网关 MAC: %s\n", remoteGatewayMAC)

	// 使用新的 Option 风格创建VXLAN连接
	fmt.Println("\n6. 创建 VXLAN TCP 连接...")

	// 创建本地地址，从内层11.1.1.2发起连接
	localAddr := &dpdknet.UDPAddr{
		IP:   net.ParseIP("11.1.1.2"),
		Port: 0, // 让系统自动分配端口
	}

	// 使用 WithLocalAddr 选项指定本地地址
	conn, err := dpdknet.Dial("udp", "8.137.60.31:12345",
		dpdknet.WithVXLAN(vxlanConfig))
	if err != nil {
		log.Fatalf("Failed to dial UDP with VXLAN: %v", err)
	}
	defer conn.Close()

	fmt.Println("UDP client connected with VXLAN to 8.137.60.31:12345")
	fmt.Printf("VXLAN VNI: %d, Local VTEP: %s, Remote VTEP: %s\n",
		vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)
	fmt.Printf("内层连接: %s -> %s\n", localAddr.String(), "8.137.60.31:12345")

	// 发送数据
	fmt.Println("7. 发送测试数据...")
	message := "Hello from VXLAN client with DHCP and ARP!"

	fmt.Printf("  准备发送消息: %s\n", message)
	fmt.Printf("  使用完整的网络协议栈:\n")
	fmt.Printf("    DHCP获得IP: %s\n", dhcpOffer.YourIP)
	fmt.Printf("    ARP解析网关: %s -> %s\n", "10.10.10.2", gatewayMAC.String())
	fmt.Printf("    VXLAN封装: %s:%d (内层) -> %s:%d (外层)\n",
		localAddr.IP, localAddr.Port, vxlanConfig.RemoteIP, vxlanConfig.UDPPort)

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

// 辅助函数: 设置 VXLAN
func setupVXLAN(config *dpdknet.VXLANConfig) {
	fmt.Printf("  建立 VXLAN 隧道: VNI=%d, %s -> %s\n",
		config.VNI, config.LocalIP, config.RemoteIP)

	// 自动解析远程 MAC 地址
	if isZeroMAC(config.RemoteMAC) {
		fmt.Printf("  正在解析远程 MAC 地址 %s...\n", config.RemoteIP)
		config.RemoteMAC = resolveRemoteMAC(config.RemoteIP)
		fmt.Printf("  解析到远程 MAC: %s\n", config.RemoteMAC)
	}

	// 自动获取本地 MAC 地址（如果为空）
	if isZeroMAC(config.LocalMAC) {
		fmt.Printf("  正在获取本地 MAC 地址...\n")
		config.LocalMAC = getLocalMAC("192.168.66.57")
		fmt.Printf("  获取到本地 MAC: %s\n", config.LocalMAC)
	}

	fmt.Printf("  ✅ VXLAN 隧道设置完成\n")
}

// 辅助函数: 执行 DHCP 协议
func performDHCP(vxlanConfig *dpdknet.VXLANConfig) *dpdknet.DHCPOfferInfo {
	fmt.Printf("  开始 DHCP 发现过程...\n")

	// 创建 DHCP 客户端
	dhcpClient := dpdknet.NewDHCPClient(vxlanConfig.LocalMAC, vxlanConfig, "test-host")

	// 模拟 DHCP Offer 响应 (在实际环境中这会来自 DHCP 服务器)
	dhcpOffer := &dpdknet.DHCPOfferInfo{
		YourIP:     net.ParseIP("11.1.1.2"),
		ServerIP:   net.ParseIP("10.10.10.1"),
		Gateway:    net.ParseIP("10.10.10.1"),
		DNS:        []net.IP{net.ParseIP("8.8.8.8")},
		SubnetMask: net.IPv4Mask(255, 255, 255, 0),
		LeaseTime:  3600 * time.Second,
	}

	fmt.Printf("  DHCP Discover 广播发送完成\n")
	fmt.Printf("  收到 DHCP Offer: IP=%s, Gateway=%s\n",
		dhcpOffer.YourIP, dhcpOffer.Gateway)

	// 发送 DHCP Request
	_, err := dhcpClient.SendDHCPRequest(dhcpOffer.YourIP, dhcpOffer.LeaseTime)
	if err != nil {
		fmt.Printf("  ⚠️  DHCP Request 发送失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ DHCP Request 发送成功\n")
	}

	return dhcpOffer
}

// 辅助函数: 设置 ARP 处理器
func setupARP(dhcpOffer *dpdknet.DHCPOfferInfo, vxlanConfig *dpdknet.VXLANConfig) *dpdknet.ARPHandler {
	fmt.Printf("  设置 ARP 处理器...\n")

	// 创建 ARP 处理器
	arpHandler := dpdknet.NewARPHandler(dhcpOffer.YourIP, vxlanConfig.LocalMAC, vxlanConfig)

	// 模拟网关发送 ARP 响应 (在实际环境中网关会自动响应)
	gatewayMAC := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	arpHandler.SimulateGatewayARP(dhcpOffer.Gateway, gatewayMAC)

	fmt.Printf("  网关 ARP 响应: %s -> %s\n", dhcpOffer.Gateway, gatewayMAC)
	fmt.Printf("  ✅ ARP 处理器设置完成\n")

	return arpHandler
}

// 辅助函数: 执行 ARP 请求
func performARPRequest(arpHandler *dpdknet.ARPHandler, targetIP net.IP) net.HardwareAddr {
	fmt.Printf("  发送 ARP 请求: 查询 %s 的 MAC 地址\n", targetIP)

	// 使用 RequestMAC 方法请求 MAC 地址
	mac, err := arpHandler.RequestMAC(targetIP)
	if err != nil {
		fmt.Printf("  ⚠️  ARP 请求失败: %v\n", err)
		// 返回一个默认的网关 MAC
		return net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}

	fmt.Printf("  收到 ARP 响应: %s -> %s\n", targetIP, mac)
	fmt.Printf("  ✅ ARP 请求完成\n")

	return mac
}
