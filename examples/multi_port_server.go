package main

import (
	"fmt"
	"log"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.Println("Starting multi-port UDP server with shared DPDK system...")

	// 创建多个UDP监听器，它们会共享同一个DPDK系统
	go startUDPServer(8001)
	go startUDPServer(8002)
	go startUDPServer(8003)

	// 保持主程序运行
	select {}
}

func startUDPServer(port int) {
	addr, err := dpdknet.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Fatalf("Failed to resolve UDP addr for port %d: %v", port, err)
	}

	conn, err := dpdknet.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on UDP port %d: %v", port, err)
	}
	defer conn.Close()

	log.Printf("UDP server listening on port %d", port)

	buf := make([]byte, 4096)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Port %d: Error reading UDP message: %v", port, err)
			continue
		}

		message := string(buf[:n])
		log.Printf("Port %d: Received from %s: %s", port, clientAddr, message)

		// Echo back the message with port info
		response := fmt.Sprintf("Echo from port %d: %s", port, message)
		_, err = conn.WriteToUDP([]byte(response), clientAddr)
		if err != nil {
			log.Printf("Port %d: Error sending response: %v", port, err)
		}
	}
}
