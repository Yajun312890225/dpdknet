package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[INFO] Starting ICMP Client (Ping) example...")

	// 解析命令行参数
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <target_ip> [count] [timeout_seconds]\n", os.Args[0])
		fmt.Printf("Examples:\n")
		fmt.Printf("  %s 192.168.1.1\n", os.Args[0])
		fmt.Printf("  %s 8.8.8.8 5 3\n", os.Args[0])
		os.Exit(1)
	}

	target := os.Args[1]
	count := 4           // 默认发送4个包
	timeoutSec := 5      // 默认5秒超时

	// 解析可选参数
	if len(os.Args) > 2 {
		if c, err := strconv.Atoi(os.Args[2]); err == nil {
			count = c
		}
	}
	if len(os.Args) > 3 {
		if t, err := strconv.Atoi(os.Args[3]); err == nil {
			timeoutSec = t
		}
	}

	// 解析目标IP
	targetIP := net.ParseIP(target)
	if targetIP == nil {
		log.Fatalf("[ERROR] Invalid IP address: %s", target)
	}

	// 获取本地IP
	localIP := getLocalIP()
	if localIP == nil {
		log.Fatalf("[ERROR] Could not determine local IP address")
	}

	log.Printf("[INFO] Local IP: %s", localIP.String())
	log.Printf("[INFO] Target IP: %s", targetIP.String())
	log.Printf("[INFO] Count: %d, Timeout: %d seconds", count, timeoutSec)

	// 创建ICMP连接
	conn, err := dpdknet.NewICMPConn(localIP)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create ICMP connection: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] ICMP connection created successfully")

	// 开始ping
	fmt.Printf("PING %s (%s): %d data bytes\n", target, targetIP.String(), 56)

	start := time.Now()
	err = conn.Ping(targetIP, count, time.Duration(timeoutSec)*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("[ERROR] Ping failed: %v", err)
		os.Exit(1)
	}

	// 显示统计信息
	fmt.Printf("\n--- %s ping statistics ---\n", target)
	fmt.Printf("%d packets transmitted, time %v\n", count, elapsed)
	fmt.Printf("Average time: %v per packet\n", elapsed/time.Duration(count))

	log.Printf("[INFO] Ping completed successfully")
}

func getLocalIP() net.IP {
	// 尝试连接到外部地址来确定本地IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		// 如果无法连接外部，使用默认地址
		return net.IPv4(192, 168, 1, 100)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
}

// 演示高级ping功能
func demonstrateAdvancedPing() {
	localIP := getLocalIP()
	if localIP == nil {
		return
	}

	conn, err := dpdknet.NewICMPConn(localIP)
	if err != nil {
		return
	}
	defer conn.Close()

	targets := []string{
		"127.0.0.1",     // 本地回环
		"192.168.1.1",   // 网关
		"8.8.8.8",       // Google DNS
		"1.1.1.1",       // Cloudflare DNS
	}

	fmt.Println("\n=== Advanced Ping Demonstration ===")

	for _, target := range targets {
		targetIP := net.ParseIP(target)
		if targetIP == nil {
			continue
		}

		fmt.Printf("\nTesting connectivity to %s...\n", target)
		
		start := time.Now()
		err := conn.Ping(targetIP, 3, 2*time.Second)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ %s: FAILED (%v)\n", target, err)
		} else {
			fmt.Printf("✅ %s: SUCCESS (took %v)\n", target, elapsed)
		}
	}

	fmt.Println("\n=== Ping demonstration completed ===")
}
