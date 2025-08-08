package main

import (
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[INFO] Starting TCP Client example...")

	// 检查参数
	serverAddr := "101.35.244.39:6888"
	// serverAddr := "43.136.168.109:8000"
	if len(os.Args) > 1 {
		serverAddr = os.Args[1]
	}

	// 解析服务器地址
	addr, err := dpdknet.ResolveTCPAddr("tcp", serverAddr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to resolve server address: %v", err)
	}

	// 连接到服务器
	log.Printf("[INFO] Connecting to TCP server %s...", addr.String())
	conn, err := dpdknet.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to connect to server: %v", err)
	}
	defer conn.Close()

	log.Printf("[INFO] Connected to server %s from local %s",
		conn.RemoteAddr().String(), conn.LocalAddr().String())

	// 启动接收消息的goroutine
	go receiveMessages(conn)
	message := "Hello, TCP Server!"
	for i := 0; i < 10; i++ { // 限制循环次数，便于调试
		log.Printf("[DEBUG] Loop iteration: %d", i+1)

		// 检查连接状态
		log.Printf("[DEBUG] Connection local addr: %v, remote addr: %v", conn.LocalAddr(), conn.RemoteAddr())

		err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) // 设置写入超时
		if err != nil {
			log.Printf("[ERROR] Failed to set write deadline: %v", err)
			break
		}
		log.Printf("[DEBUG] Write deadline set successfully")

		// 发送消息
		log.Printf("[DEBUG] Attempting to write message...")
		start := time.Now()
		n, err := conn.Write([]byte(message + "\n"))
		elapsed := time.Since(start)

		if err != nil {
			log.Printf("[ERROR] Failed to send message after %v: %v", elapsed, err)
			break
		}

		log.Printf("[INFO] Sent message: %s (wrote %d bytes in %v)", message, n, elapsed)

		log.Printf("[DEBUG] Sleeping for 1 second...")
		time.Sleep(1 * time.Second) // 每秒发送一次消息
		log.Printf("[DEBUG] Sleep completed")
	}

	log.Printf("[INFO] Finished sending messages")

}

func receiveMessages(conn net.Conn) {
	buffer := make([]byte, 4096)

	for {
		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		// 读取服务器响应
		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Printf("[INFO] Server closed connection")
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("[DEBUG] Read timeout, continuing...")
				continue
			} else {
				log.Printf("[ERROR] Failed to read from server: %v", err)
			}
			log.Printf("[ERROR] Exiting receiveMessages goroutine")
			return
		}
		log.Printf("[INFO] Received %d bytes from server: %s", n, string(buffer[:n]))

	}
}
