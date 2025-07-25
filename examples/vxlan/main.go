package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

// VXLAN 示例程序
//
// 环境变量配置:
// - DPDKNET_LOCAL_IP: 本地 VTEP IP 地址 (外层隧道地址)
// - DPDKNET_REMOTE_VTEP: 远程 VTEP IP 地址 (外层隧道地址)
//
// VXLAN 地址说明:
// - VXLANAddr.IP: 内层虚拟网络 IP (应用层地址，如 10.0.1.x)
// - VXLANAddr.Port: 内层应用端口 (应用层端口，如 8080)
// - VXLANAddr.VNI: VXLAN 网络标识符
//
// 例如:
// export DPDKNET_LOCAL_IP=192.168.66.57    # 本地物理机 IP
// export DPDKNET_REMOTE_VTEP=192.168.66.115 # 远程物理机 IP

func main() {
	log.Printf("Starting VXLAN example with DPDK integration...")

	// VXLAN 服务器示例
	// go runVXLANServer()

	// 等待服务器启动
	time.Sleep(3 * time.Second)

	// VXLAN 客户端示例
	runVXLANClient()
}

func runVXLANServer() {
	// 创建内层 VXLAN 地址（虚拟网络中的地址）
	vxlanAddr := &dpdknet.VXLANAddr{
		IP:       net.ParseIP("10.0.1.100"), // 内层虚拟 IP
		Port:     8080,                      // 内层应用端口
		VNI:      1000,                      // VXLAN 网络标识
		Protocol: "tcp",                     // 内层协议类型：tcp、udp、icmp
	}

	// 监听 VXLAN 连接 (基于 DPDK)
	// 外层 VTEP 地址通过环境变量 DPDKNET_LOCAL_IP 配置
	listener, err := dpdknet.ListenVXLAN("vxlan", vxlanAddr)
	if err != nil {
		log.Fatalf("Failed to listen on VXLAN: %v", err)
	}
	defer listener.Close()

	log.Printf("VXLAN server listening on inner address %s (DPDK mode)", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		log.Printf("New VXLAN connection from inner address %s", conn.RemoteAddr())
		go handleVXLANConnection(conn)
	}
}

func handleVXLANConnection(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	for {
		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		n, err := conn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			break
		}

		message := string(buffer[:n])
		log.Printf("Received VXLAN message: %s", message)

		// 回显消息
		response := fmt.Sprintf("VXLAN Echo: %s", message)

		// 设置写入超时
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err = conn.Write([]byte(response))
		if err != nil {
			log.Printf("Write error: %v", err)
			break
		}

		log.Printf("Sent VXLAN response: %s", response)
	}
}

func runVXLANClient() {
	// 创建远程内层 VXLAN 地址（目标虚拟网络地址）
	remoteAddr := &dpdknet.VXLANAddr{
		IP:       net.ParseIP("10.0.1.100"), // 内层目标虚拟 IP
		Port:     8080,                      // 内层目标应用端口
		VNI:      1000,                      // VXLAN 网络标识
		Protocol: "tcp",                     // 内层协议类型：tcp、udp、icmp
	}

	// 本地内层地址（可选）
	localAddr := &dpdknet.VXLANAddr{
		IP:       net.ParseIP("10.0.1.200"), // 内层本地虚拟 IP
		Port:     8081,                      // 内层本地应用端口
		VNI:      1000,                      // VXLAN 网络标识
		Protocol: "tcp",                     // 内层协议类型：tcp、udp、icmp
	}

	// 连接到 VXLAN 服务器 (通过 DPDK)
	// 外层 VTEP 地址通过环境变量配置：
	// DPDKNET_LOCAL_IP: 本地 VTEP IP
	// DPDKNET_REMOTE_VTEP: 远程 VTEP IP
	conn, err := dpdknet.DialVXLAN("vxlan", localAddr, remoteAddr)
	if err != nil {
		log.Fatalf("Failed to dial VXLAN: %v", err)
	}
	defer conn.Close()

	log.Printf("Connected to VXLAN server at inner address %s (DPDK mode)", conn.RemoteAddr())

	// 发送消息
	message := "Hello VXLAN World via DPDK!"
	log.Printf("Sending message: %s", message)

	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Fatalf("Failed to write: %v", err)
	}

	// 读取响应
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	n, err := conn.Read(buffer)
	if err != nil {
		log.Fatalf("Failed to read: %v", err)
	}

	response := string(buffer[:n])
	log.Printf("Server response: %s", response)

	log.Printf("VXLAN communication completed successfully!")
}
