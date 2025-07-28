package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[INFO] Starting UDP Client example...")

	// 检查参数
	serverAddr := "192.168.66.29:8000"
	if len(os.Args) > 1 {
		serverAddr = os.Args[1]
	}

	// 解析服务器地址
	remoteAddr, err := dpdknet.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to resolve server address: %v", err)
	}

	// 创建UDP连接
	log.Printf("[INFO] Creating UDP connection to %s...", remoteAddr.String())
	conn, err := dpdknet.ListenUDP("udp", nil) // 使用随机本地端口
	if err != nil {
		log.Fatalf("[ERROR] Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] UDP client ready, local address: %s", conn.LocalAddr().String())
	fmt.Printf("Commands: ping, time, quit, or any message to echo\n")

	// 主循环
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		message := strings.TrimSpace(scanner.Text())
		if message == "" {
			continue
		}

		// 发送消息
		start := time.Now()
		_, err := conn.WriteToUDP([]byte(message), remoteAddr)
		if err != nil {
			log.Printf("[ERROR] Failed to send message: %v", err)
			continue
		}

		log.Printf("[INFO] Sent message: %s", message)

		// 接收响应
		buffer := make([]byte, 4096)

		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		n, responseAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("[ERROR] Failed to receive response: %v", err)
			continue
		}

		rtt := time.Since(start)
		response := string(buffer[:n])

		fmt.Printf("Server (%s): %s (RTT: %v)\n",
			responseAddr.String(), response, rtt)

		log.Printf("[INFO] Received response from %s: %s (RTT: %v)",
			responseAddr.String(), response, rtt)

		// 检查是否退出
		if strings.ToLower(message) == "quit" {
			log.Printf("[INFO] Quit command sent, exiting...")
			break
		}

		// 演示特殊命令
		if message == "ping" {
			fmt.Printf("Ping successful!\n")
		} else if message == "time" {
			fmt.Printf("Server time received: %s\n", response)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[ERROR] Error reading input: %v", err)
	}

	log.Printf("[INFO] UDP client shutting down...")
}

// 辅助函数：发送多个测试包
func sendTestPackets(conn *dpdknet.UDPConn, remoteAddr *dpdknet.UDPAddr) {
	testMessages := []string{
		"ping",
		"Hello, UDP Server!",
		"time",
		"This is a test message",
		"quit",
	}

	for i, msg := range testMessages {
		fmt.Printf("\nSending test packet %d: %s\n", i+1, msg)

		start := time.Now()
		_, err := conn.WriteToUDP([]byte(msg), remoteAddr)
		if err != nil {
			log.Printf("[ERROR] Failed to send test packet: %v", err)
			continue
		}

		// 接收响应
		buffer := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))

		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("[ERROR] Failed to receive test response: %v", err)
			continue
		}

		rtt := time.Since(start)
		response := string(buffer[:n])

		fmt.Printf("Response: %s (RTT: %v)\n", response, rtt)

		time.Sleep(500 * time.Millisecond) // 间隔发送
	}
}
