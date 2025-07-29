package main

import (
	"fmt"
	"log"
	"net"
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

	// 示例3: UDP 客户端带 VXLAN 封装
	// go udpClientExample("0.0.0.0:8080")
	// go udpClientExample("11.1.1.3:555")
	// select {}

	// 示例4: TCP 客户端带 VXLAN 封装
	tcpClientExample()

	// 示例5: ICMP 客户端带 VXLAN 封装 (TODO: 实现)
	// icmpClientExample()
}

func tcpServerExample() {
	fmt.Println("=== TCP Server with VXLAN Example ===")

	// 使用新的 Option 风格 - VXLAN 作为选项
	listener, err := dpdknet.Listen("tcp", ":8080", dpdknet.WithVXLAN())
	if err != nil {
		log.Fatalf("Failed to create TCP listener with VXLAN: %v", err)
	}
	defer listener.Close()

	fmt.Println("TCP server with VXLAN listening on :8080 (Option 风格)")

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

	// time.Sleep(1 * time.Second) // 让服务器运行一会儿
}

func udpServerExample() {
	fmt.Println("\n=== UDP Server with VXLAN Example ===")

	// 使用新的 Option 风格
	conn, err := dpdknet.ListenPacket("udp", ":9090", dpdknet.WithVXLAN())
	if err != nil {
		log.Fatalf("Failed to create UDP connection with VXLAN: %v", err)
	}
	defer conn.Close()

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

func udpClientExample(ip string) {
	fmt.Println("\n=== UDP Client with Global VXLAN Example ===")

	// 确保在创建连接前先初始化网络系统（这会使用新的环境变量并配置VXLAN）
	if err := dpdknet.EnsureGlobalNetworkInit(); err != nil {
		log.Fatalf("网络初始化失败: %v", err)
	}

	// 检查全局VXLAN配置是否可用
	if !dpdknet.IsVXLANEnabled() {
		log.Fatalf("全局VXLAN配置未初始化")
	}

	// 创建内层本地地址（OSPF 网络地址）
	innerLocalAddr, err := dpdknet.ResolveUDPAddr("udp", ip) // 将自动在gVisor中配置此地址
	if err != nil {
		log.Fatalf("Failed to resolve inner local address: %v", err)
	}

	conn, err := dpdknet.Dial("udp", "124.221.130.129:8000",
		dpdknet.WithLocalAddr(innerLocalAddr),
		dpdknet.WithVXLAN())
	if err != nil {
		log.Fatalf("Failed to dial UDP with VXLAN: %v", err)
	}
	defer conn.Close()

	// 发送数据
	fmt.Println("发送测试数据...")
	message := ip

	go func() {
		// 增加读的代码
		for {
			_, err = conn.Write([]byte(message))
			if err != nil {
				log.Printf("Write error: %v", err)
				return
			}

			// 等待一段时间让数据包发送完成
			time.Sleep(500 * time.Millisecond)
		}

	}()

	buffer := make([]byte, 1024)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}
		fmt.Printf("Message %s Received UDP data: %s\n", ip, string(buffer[:n]))

	}
}

func handleTCPConnection(conn net.Conn) {
	defer func() {
		err := conn.Close()
		if err != nil {
			// Silent error handling
		}
	}()

	buffer := make([]byte, 1024)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			return
		}

		// Echo response
		response := fmt.Sprintf("Echo: %s", string(buffer[:n]))
		_, err = conn.Write([]byte(response))
		if err != nil {
			return
		}
	}
}

func testNetworkConnectivity() {
	// 测试到远程 VTEP 的连通性
	remoteVTEP := "192.168.66.29"
	fmt.Printf("[DEBUG] 测试到远程 VTEP %s 的连通性...\n", remoteVTEP)

	// 使用系统 ping 命令
	fmt.Printf("[DEBUG] 执行 ping 命令...\n")
	cmd := exec.Command("ping", "-c", "3", "-W", "2", remoteVTEP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[ERROR] ❌ PING 失败: %v\n", err)
		fmt.Printf("[DEBUG] PING 输出: %s\n", string(output))
	} else {
		fmt.Printf("[DEBUG] ✅ PING 成功\n")
		// 解析ping统计信息
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "packet loss") || strings.Contains(line, "min/avg/max") {
				fmt.Printf("[DEBUG] PING 统计: %s\n", strings.TrimSpace(line))
			}
		}
	}

	// 检查 ARP 表
	fmt.Printf("[DEBUG] 检查 ARP 表中的 %s...\n", remoteVTEP)
	cmd = exec.Command("arp", "-n", remoteVTEP)
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[ERROR] ❌ ARP 查询失败: %v\n", err)
	} else {
		fmt.Printf("[DEBUG] ARP 信息: %s\n", strings.TrimSpace(string(output)))
	}

	// 检查路由表
	fmt.Printf("[DEBUG] 检查到 %s 的路由...\n", remoteVTEP)
	cmd = exec.Command("ip", "route", "get", remoteVTEP)
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[ERROR] ❌ 路由查询失败: %v\n", err)
	} else {
		fmt.Printf("[DEBUG] 路由信息: %s\n", strings.TrimSpace(string(output)))
	}

	// 检查本地网络接口
	fmt.Printf("[DEBUG] 检查本地网络接口...\n")
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("[ERROR] 获取网络接口失败: %v\n", err)
	} else {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
				fmt.Printf("[DEBUG] 活动接口: %s (MAC: %s)\n", iface.Name, iface.HardwareAddr)

				addrs, err := iface.Addrs()
				if err == nil {
					for _, addr := range addrs {
						if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
							if ipnet.IP.To4() != nil {
								fmt.Printf("[DEBUG]   IPv4: %s\n", ipnet.IP)
							}
						}
					}
				}
			}
		}
	}
}

func validateVXLANConfig(config *dpdknet.VXLANConfig) {
	fmt.Printf("[DEBUG] VXLAN配置验证:\n")
	fmt.Printf("  VNI: %d\n", config.VNI)
	fmt.Printf("  本地 VTEP: %s\n", config.LocalIP)
	fmt.Printf("  远程 VTEP: %s\n", config.RemoteIP)
	fmt.Printf("  VXLAN 端口: %d\n", config.UDPPort)
	fmt.Printf("  本地 MAC: %s\n", config.LocalMAC)
	fmt.Printf("  远程 MAC: %s\n", config.RemoteMAC)

	// 验证配置有效性
	if config.VNI == 0 {
		fmt.Printf("[WARNING] VNI为0，这可能不是有效的VNI值\n")
	}
	if config.LocalIP == nil {
		fmt.Printf("[ERROR] 本地VTEP IP未设置\n")
	}
	if config.RemoteIP == nil {
		fmt.Printf("[ERROR] 远程VTEP IP未设置\n")
	}
	if config.UDPPort == 0 {
		fmt.Printf("[WARNING] VXLAN UDP端口为0，使用默认值4789\n")
	}
	if isZeroMAC(config.LocalMAC) {
		fmt.Printf("[WARNING] 本地MAC地址为零值\n")
	}
	if isZeroMAC(config.RemoteMAC) {
		fmt.Printf("[WARNING] 远程MAC地址为零值\n")
	}

	// 检查远程端口是否开放
	if config.RemoteIP != nil && config.UDPPort > 0 {
		remoteAddr := fmt.Sprintf("%s:%d", config.RemoteIP, config.UDPPort)
		fmt.Printf("[DEBUG] 检查远程 VXLAN 端口 %s...\n", remoteAddr)

		// 测试UDP连接（VXLAN使用UDP）
		conn, err := net.DialTimeout("udp", remoteAddr, 2*time.Second)
		if err != nil {
			fmt.Printf("[ERROR] ❌ 无法连接到远程 VXLAN 端口: %v\n", err)
			fmt.Printf("[DEBUG] 这可能表明:\n")
			fmt.Printf("  1. 远程VTEP未启动\n")
			fmt.Printf("  2. 防火墙阻止了VXLAN流量\n")
			fmt.Printf("  3. 网络路由问题\n")
		} else {
			fmt.Printf("[DEBUG] ✅ 远程 VXLAN 端口可达\n")
			conn.Close()
		}
	}

	// 检查ARP缓存
	if config.ARPCache != nil && len(config.ARPCache) > 0 {
		fmt.Printf("[DEBUG] ARP缓存条目数: %d\n", len(config.ARPCache))
		for ip, mac := range config.ARPCache {
			fmt.Printf("[DEBUG]   %s -> %s\n", ip, mac)
		}
	} else {
		fmt.Printf("[DEBUG] ARP缓存为空\n")
	}

	// 检查网关配置
	if config.GatewayIP != nil {
		fmt.Printf("[DEBUG] 网关IP: %s\n", config.GatewayIP)
		if config.GatewayMAC != nil {
			fmt.Printf("[DEBUG] 网关MAC: %s\n", config.GatewayMAC)
		}
	} else {
		fmt.Printf("[DEBUG] 未配置网关\n")
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

// getRemoteMACFromARP 从 ARP 表获取远端 MAC 地址
func getRemoteMACFromARP(remoteIP string) net.HardwareAddr {
	// 先尝试 ping 来确保 ARP 表有记录
	fmt.Printf("    执行 ping 来触发 ARP...\n")
	exec.Command("ping", "-c", "1", "-W", "1", remoteIP).Run()

	// 查询 ARP 表
	cmd := exec.Command("arp", "-n", remoteIP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("    ❌ ARP 查询失败: %v，使用默认 MAC\n", err)
		return net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}
	}

	// 解析 ARP 输出
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == remoteIP {
			// 格式通常是: IP ... MAC ...
			for _, field := range fields {
				if strings.Contains(field, ":") && len(field) == 17 {
					mac, err := net.ParseMAC(field)
					if err == nil {
						fmt.Printf("    ✅ 从 ARP 表获取到 %s 的 MAC: %s\n", remoteIP, mac)
						return mac
					}
				}
			}
		}
	}

	fmt.Printf("    ❌ 未找到 ARP 记录，使用默认 MAC\n")
	return net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d}
}

func tcpClientExample() {
	fmt.Println("\n=== TCP Client with Global VXLAN Example ===")

	// 确保在创建连接前先初始化网络系统（这会使用新的环境变量并配置VXLAN）
	if err := dpdknet.EnsureGlobalNetworkInit(); err != nil {
		log.Fatalf("网络初始化失败: %v", err)
	}

	// 创建内层本地地址（OSPF 网络地址）
	innerLocalAddr, err := dpdknet.ResolveTCPAddr("tcp", "11.1.1.2:0") // 将自动在gVisor中配置此地址
	if err != nil {
		log.Fatalf("Failed to resolve inner local address: %v", err)
	}

	// 准备连接参数
	targetAddr := "124.221.130.129:8080"

	// 建立TCP连接
	conn, err := dpdknet.Dial("tcp", targetAddr,
		dpdknet.WithLocalAddr(innerLocalAddr),
		dpdknet.WithVXLAN())
	if err != nil {
		log.Fatalf("Failed to dial TCP with VXLAN: %v", err)
	}

	// 添加详细的连接关闭日志
	defer func() {
		_ = conn.Close()
		// 等待一下，让网络包处理完成
		time.Sleep(time.Millisecond * 100)
		log.Printf("[CLIENT] 🔚 Connection closure process finished")
	}()

	// 发送数据
	fmt.Println("\n[DEBUG] 开始发送测试数据...")
	message := "Hello from TCP Client with VXLAN Debug"
	go func() {
		for {
			_, err = conn.Write([]byte(message))
			if err != nil {
				log.Printf("Write error: %v", err)
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	if err != nil {
		return
	}

	// 尝试读取响应
	buffer := make([]byte, 1024)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			return
		}
		log.Printf("[CLIENT] 📨 Received data from %s: %s", conn.RemoteAddr(), string(buffer[:n]))
	}

}

// 分析连接错误的详细信息
func analyzeConnectionError(err error, targetAddr string, config *dpdknet.VXLANConfig) {
	fmt.Printf("[DEBUG] 错误分析: %v\n", err)

	// 解析目标地址
	host, port, splitErr := net.SplitHostPort(targetAddr)
	if splitErr != nil {
		fmt.Printf("[ERROR] 无法解析目标地址: %v\n", splitErr)
		return
	}

	fmt.Printf("[DEBUG] 目标主机: %s, 端口: %s\n", host, port)

	// 检查DNS解析
	fmt.Printf("[DEBUG] 检查DNS解析...\n")
	ips, dnsErr := net.LookupIP(host)
	if dnsErr != nil {
		fmt.Printf("[ERROR] DNS解析失败: %v\n", dnsErr)
	} else {
		fmt.Printf("[DEBUG] DNS解析成功: %v\n", ips)
	}

	// 检查是否可以通过标准网络栈连接
	fmt.Printf("[DEBUG] 测试标准TCP连接...\n")
	stdConn, stdErr := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if stdErr != nil {
		fmt.Printf("[ERROR] 标准TCP连接也失败: %v\n", stdErr)
		fmt.Printf("[DEBUG] 这表明目标服务器可能不可达或未监听该端口\n")
	} else {
		stdConn.Close()
		fmt.Printf("[DEBUG] ✅ 标准TCP连接成功，问题可能在VXLAN层\n")
	}

	// 检查VXLAN配置
	fmt.Printf("[DEBUG] 检查VXLAN配置有效性...\n")
	if config.LocalIP == nil {
		fmt.Printf("[ERROR] 本地VTEP IP为空\n")
	}
	if config.RemoteIP == nil {
		fmt.Printf("[ERROR] 远程VTEP IP为空\n")
	}
	if config.VNI == 0 {
		fmt.Printf("[WARNING] VNI为0，可能不是有效值\n")
	}
}
