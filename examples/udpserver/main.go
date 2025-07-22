package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[INFO] Starting UDP Server example...")

	// 检查参数
	port := "8000"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	// 解析监听地址
	addr, err := dpdknet.ResolveUDPAddr("udp", "192.168.66.53:"+port)
	if err != nil {
		log.Fatalf("[ERROR] Failed to resolve address: %v", err)
	}

	// 创建UDP连接
	log.Printf("[INFO] Creating UDP listener on %s...", addr.String())
	conn, err := dpdknet.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create UDP listener: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] UDP server listening on %s", conn.LocalAddr().String())
	log.Printf("[INFO] Server ready to receive packets...")

	// 设置信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 服务器主循环
	go func() {
		buffer := make([]byte, 4096)

		for {
			// 读取UDP数据包
			n, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				log.Printf("[ERROR] Failed to read UDP packet: %v", err)
				continue
			}

			if n == 0 {
				continue
			}

			data := buffer[:n]
			log.Printf("[INFO] Received %d bytes from %s: %s",
				n, clientAddr.String(), string(data))

			// 处理数据包
			handleUDPPacket(conn, clientAddr, data)
		}
	}()

	// 等待退出信号
	<-sigCh
	log.Printf("[INFO] Received shutdown signal, closing server...")
}

func handleUDPPacket(conn *dpdknet.UDPConn, clientAddr *dpdknet.UDPAddr, data []byte) {
	message := string(data)

	// 生成响应
	var response string
	switch message {
	case "ping":
		response = "pong"
	case "time":
		response = time.Now().Format("2006-01-02 15:04:05")
	case "quit":
		response = "goodbye"
	default:
		response = fmt.Sprintf("Echo: %s", message)
	}

	// 发送响应
	_, err := conn.WriteToUDP([]byte(response), clientAddr)
	if err != nil {
		log.Printf("[ERROR] Failed to send response to %s: %v", clientAddr.String(), err)
		return
	}

	log.Printf("[INFO] Sent response to %s: %s", clientAddr.String(), response)

	// 统计信息
	logPacketStats(clientAddr, len(data), len(response))
}

func logPacketStats(clientAddr *dpdknet.UDPAddr, receivedBytes, sentBytes int) {
	log.Printf("[STATS] Client: %s, Received: %d bytes, Sent: %d bytes",
		clientAddr.String(), receivedBytes, sentBytes)
}
