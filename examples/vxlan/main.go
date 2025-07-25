package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.Printf("Starting VXLAN example with DPDK integration...")

	// VXLAN 服务器示例
	go runVXLANServer()

	// 等待服务器启动
	time.Sleep(3 * time.Second)

	// VXLAN 客户端示例
	runVXLANClient()
}

func runVXLANServer() {
	// 创建 VXLAN 地址
	vxlanAddr := &dpdknet.VXLANAddr{
		IP:   net.ParseIP("192.168.1.100"),
		Port: 4789,
		VNI:  1000,
	}

	// 监听 VXLAN 连接 (基于 DPDK)
	listener, err := dpdknet.ListenVXLAN("vxlan", vxlanAddr)
	if err != nil {
		log.Fatalf("Failed to listen on VXLAN: %v", err)
	}
	defer listener.Close()

	log.Printf("VXLAN server listening on %s (DPDK mode)", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		log.Printf("New VXLAN connection from %s", conn.RemoteAddr())
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
	// 创建远程 VXLAN 地址
	remoteAddr := &dpdknet.VXLANAddr{
		IP:   net.ParseIP("192.168.1.100"),
		Port: 4789,
		VNI:  1000,
	}

	// 连接到 VXLAN 服务器 (通过 DPDK)
	conn, err := dpdknet.DialVXLAN("vxlan", nil, remoteAddr)
	if err != nil {
		log.Fatalf("Failed to dial VXLAN: %v", err)
	}
	defer conn.Close()

	log.Printf("Connected to VXLAN server at %s (DPDK mode)", conn.RemoteAddr())

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
