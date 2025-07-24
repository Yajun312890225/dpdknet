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
	count := 4      // 默认发送4个包
	timeoutSec := 5 // 默认5秒超时

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

	log.Printf("[INFO] Target IP: %s", targetIP.String())
	log.Printf("[INFO] Count: %d, Timeout: %d seconds", count, timeoutSec)

	// 使用兼容 net 包的接口创建ICMP连接
	log.Printf("[INFO] Creating ICMP connection using dpdknet.ListenIP...")
	conn, err := dpdknet.ListenIP("ip4:icmp", nil)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create ICMP connection: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] ICMP connection created successfully")
	log.Printf("[INFO] Local address: %s", conn.LocalAddr().String())

	// 解析目标地址
	targetAddr, err := dpdknet.ResolveIPAddr("ip4:icmp", target)
	if err != nil {
		log.Fatalf("[ERROR] Failed to resolve target address: %v", err)
	}

	// 开始ping
	fmt.Printf("PING %s (%s): %d data bytes\n", target, targetIP.String(), 56)

	start := time.Now()
	err = pingWithPacketConn(conn, targetAddr, count, time.Duration(timeoutSec)*time.Second)
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

// pingWithPacketConn 使用PacketConn接口实现ping功能
func pingWithPacketConn(conn net.PacketConn, targetAddr net.Addr, count int, timeout time.Duration) error {
	id := uint16(time.Now().Unix() & 0xFFFF)

	for i := 1; i <= count; i++ {
		// 构造ICMP Echo Request
		icmpData := make([]byte, 8+8) // ICMP头8字节 + 数据8字节
		icmpData[0] = 8               // Echo Request
		icmpData[1] = 0               // Code
		// Checksum 字段留空，由底层计算
		icmpData[4] = byte(id >> 8)   // ID高字节
		icmpData[5] = byte(id & 0xFF) // ID低字节
		icmpData[6] = byte(i >> 8)    // Sequence高字节
		icmpData[7] = byte(i & 0xFF)  // Sequence低字节

		// 添加时间戳作为数据
		timestamp := time.Now().UnixNano()
		for j := 0; j < 8; j++ {
			icmpData[8+j] = byte(timestamp >> (8 * (7 - j)))
		}

		start := time.Now()

		// 发送ICMP数据包
		_, err := conn.WriteTo(icmpData, targetAddr)
		if err != nil {
			log.Printf("[ERROR] Failed to send ICMP packet %d: %v", i, err)
			continue
		}

		// 等待回复
		buffer := make([]byte, 1500)
		conn.SetReadDeadline(time.Now().Add(timeout))

		n, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("Request timeout for seq=%d\n", i)
			} else {
				log.Printf("[ERROR] Failed to read ICMP reply %d: %v", i, err)
			}
			continue
		}

		// 检查是否是我们期望的回复
		if n >= 8 && buffer[0] == 0 { // Echo Reply
			replyID := (uint16(buffer[4]) << 8) | uint16(buffer[5])
			replySeq := (uint16(buffer[6]) << 8) | uint16(buffer[7])

			if replyID == id && int(replySeq) == i {
				rtt := time.Since(start)
				fmt.Printf("Reply from %s: seq=%d time=%v\n", addr.String(), i, rtt)
			}
		}

		if i < count {
			time.Sleep(time.Second)
		}
	}
	return nil
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
		"127.0.0.1",   // 本地回环
		"192.168.1.1", // 网关
		"8.8.8.8",     // Google DNS
		"1.1.1.1",     // Cloudflare DNS
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
