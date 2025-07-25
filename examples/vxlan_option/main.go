package main

import (
	"fmt"
	"log"
	"net"
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

	// 创建 VXLAN 配置
	vxlanConfig := &dpdknet.VXLANConfig{
		VNI:       1000,
		LocalIP:   net.ParseIP("11.74.225.249"), // 本地 VTEP IP
		RemoteIP:  net.ParseIP("10.64.12.7"),    // 远程 VTEP IP
		UDPPort:   4789,                         // VXLAN 端口
		LocalMAC:  net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		RemoteMAC: net.HardwareAddr{0x00, 0x66, 0x77, 0x88, 0x99, 0xaa},
	}

	// 使用新的 Option 风格
	conn, err := dpdknet.Dial("tcp", "192.168.1.10:8080", dpdknet.WithVXLAN(vxlanConfig))
	if err != nil {
		log.Fatalf("Failed to dial TCP with VXLAN: %v", err)
	}
	defer conn.Close()

	fmt.Println("TCP client connected with VXLAN to 192.168.1.10:8080 (Option 风格)")
	fmt.Printf("VXLAN VNI: %d, Local VTEP: %s, Remote VTEP: %s\n",
		vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)

	// 发送数据
	message := "Hello from VXLAN client!"
	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Printf("Write error: %v", err)
		return
	}

	fmt.Printf("Sent message: %s\n", message)
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
