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

	// 使用兼容 net 包的接口创建ICMP监听器
	log.Printf("[INFO] Creating ICMP listener using dpdknet.ListenIP...")
	conn, err := dpdknet.ListenIP("ip4:icmp", nil)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create ICMP listener: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] ICMP server listening on %s", conn.LocalAddr().String())
	log.Printf("[INFO] Server is ready to handle ICMP packets...")
	log.Printf("[INFO] Compatible with net.ListenIP interface")

	// 设置信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 服务器主循环 - 读取ICMP数据包
	go func() {
		buffer := make([]byte, 1500)
		for {
			// 读取ICMP数据包
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				log.Printf("[ERROR] Failed to read ICMP packet: %v", err)
				return
			}

			if n > 0 {
				log.Printf("[INFO] Received ICMP packet from %s: %d bytes", addr.String(), n)

				// 解析ICMP类型
				if n >= 1 {
					icmpType := buffer[0]
					log.Printf("[INFO] ICMP Type: %d", icmpType)

					// ICMP Echo请求自动回复由网络层处理
					// 这里只是记录收到的数据包
				}
			}
		}
	}()

	// 演示发送ping
	go demonstratePingWithNewInterface()

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

func demonstratePingWithNewInterface() {
	time.Sleep(5 * time.Second) // 等待服务器启动完成

	log.Printf("[INFO] Starting ping demonstration with new interface...")

	// 使用兼容接口创建ICMP客户端连接
	conn, err := dpdknet.ListenIP("ip4:icmp", nil)
	if err != nil {
		log.Printf("[ERROR] Failed to create ICMP client connection: %v", err)
		return
	}
	defer conn.Close()

	testTargets := []string{
		"127.0.0.1",
		"192.168.1.1",
	}

	for _, target := range testTargets {
		addr, err := dpdknet.ResolveIPAddr("ip4:icmp", target)
		if err != nil {
			log.Printf("[ERROR] Failed to resolve address %s: %v", target, err)
			continue
		}

		log.Printf("[INFO] Attempting to ping %s using new interface...", target)

		// 构造ICMP Echo Request
		icmpData := make([]byte, 8)
		icmpData[0] = 8 // Echo Request
		icmpData[1] = 0 // Code
		// Checksum will be calculated by sendRawICMP
		icmpData[4] = 0x12 // ID
		icmpData[5] = 0x34
		icmpData[6] = 0x00 // Sequence
		icmpData[7] = 0x01

		// 发送ICMP包
		_, err = conn.WriteTo(icmpData, addr)
		if err != nil {
			log.Printf("[ERROR] Failed to send ICMP packet to %s: %v", target, err)
		} else {
			log.Printf("[INFO] ICMP packet sent to %s", target)
		}

		time.Sleep(2 * time.Second)
	}

	log.Printf("[INFO] Ping demonstration completed")
}
