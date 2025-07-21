package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[INFO] Starting ICMP Server example...")

	// 获取本地IP地址
	localIP := getLocalIP()
	if localIP == nil {
		log.Fatalf("[ERROR] Could not determine local IP address")
	}

	log.Printf("[INFO] Using local IP: %s", localIP.String())

	// 创建ICMP连接
	log.Printf("[INFO] Creating ICMP connection...")
	conn, err := dpdknet.NewICMPConn(localIP)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create ICMP connection: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] ICMP server started on %s", localIP.String())
	log.Printf("[INFO] Server is ready to handle ICMP packets...")
	log.Printf("[INFO] Note: ICMP echo requests are automatically handled by the network layer")

	// 设置信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 服务器主循环 - 监控ICMP活动
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Printf("[INFO] ICMP server is running... (listening for ping requests)")
				
			case <-sigCh:
				log.Printf("[INFO] Shutdown signal received")
				return
			}
		}
	}()

	// 演示发送ping到常见地址
	go demonstratePing(conn)

	// 等待退出信号
	<-sigCh
	log.Printf("[INFO] ICMP server shutting down...")
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

func demonstratePing(conn *dpdknet.ICMPConn) {
	time.Sleep(5 * time.Second) // 等待服务器启动完成

	testTargets := []string{
		"127.0.0.1",
		"192.168.1.1",
		"8.8.8.8",
	}

	log.Printf("[INFO] Starting ping demonstration...")

	for _, target := range testTargets {
		ip := net.ParseIP(target)
		if ip == nil {
			log.Printf("[ERROR] Invalid IP address: %s", target)
			continue
		}

		log.Printf("[INFO] Attempting to ping %s...", target)

		// 发送3个ping包
		err := conn.Ping(ip, 3, 2*time.Second)
		if err != nil {
			log.Printf("[WARNING] Ping to %s failed: %v", target, err)
		}

		time.Sleep(2 * time.Second) // 间隔
	}

	log.Printf("[INFO] Ping demonstration completed")
}
