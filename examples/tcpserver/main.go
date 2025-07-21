package main

import (
	"fmt"
	"io"
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
	log.Printf("[INFO] Starting TCP Server example...")

	// 检查参数
	port := "8080"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	// 解析监听地址
	addr, err := dpdknet.ResolveTCPAddr("tcp", "0.0.0.0:"+port)
	if err != nil {
		log.Fatalf("[ERROR] Failed to resolve address: %v", err)
	}

	// 创建TCP监听器
	log.Printf("[INFO] Creating TCP listener on %s...", addr.String())
	listener, err := dpdknet.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	log.Printf("[INFO] TCP server listening on %s", listener.Addr().String())
	log.Printf("[INFO] Server ready to accept connections...")

	// 设置信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 服务器主循环
	go func() {
		for {
			// 接受新连接
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("[ERROR] Failed to accept connection: %v", err)
				continue
			}

			log.Printf("[INFO] New connection from %s", conn.RemoteAddr().String())

			// 为每个连接启动一个goroutine
			go handleTCPConnection(conn)
		}
	}()

	// 等待退出信号
	<-sigCh
	log.Printf("[INFO] Received shutdown signal, closing server...")
}

func handleTCPConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		log.Printf("[INFO] Connection closed: %s", conn.RemoteAddr().String())
	}()

	log.Printf("[INFO] Handling connection from %s", conn.RemoteAddr().String())

	// 发送欢迎消息
	welcome := fmt.Sprintf("Welcome to TCP Server! Connected from %s\n", conn.RemoteAddr().String())
	_, err := conn.Write([]byte(welcome))
	if err != nil {
		log.Printf("[ERROR] Failed to send welcome message: %v", err)
		return
	}

	// 连接处理循环
	buffer := make([]byte, 4096)
	for {
		// 设置读取超时
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		// 读取客户端数据
		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Printf("[INFO] Client closed connection: %s", conn.RemoteAddr().String())
			} else {
				log.Printf("[ERROR] Failed to read from connection: %v", err)
			}
			return
		}

		if n == 0 {
			continue
		}

		data := buffer[:n]
		log.Printf("[INFO] Received %d bytes from %s: %s", 
			n, conn.RemoteAddr().String(), string(data))

		// Echo回发数据
		response := fmt.Sprintf("Echo: %s", string(data))
		_, err = conn.Write([]byte(response))
		if err != nil {
			log.Printf("[ERROR] Failed to echo data: %v", err)
			return
		}

		log.Printf("[INFO] Echoed %d bytes to %s", 
			len(response), conn.RemoteAddr().String())

		// 检查是否收到退出命令
		if string(data) == "quit\n" || string(data) == "quit\r\n" {
			log.Printf("[INFO] Client requested quit: %s", conn.RemoteAddr().String())
			conn.Write([]byte("Goodbye!\n"))
			return
		}
	}
}
