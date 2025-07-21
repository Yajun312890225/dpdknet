package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[INFO] Starting TCP Client example...")

	// 检查参数
	serverAddr := "127.0.0.1:8080"
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

	// 主循环 - 发送用户输入
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Connected! Type messages to send (type 'quit' to exit):")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		message := scanner.Text()
		if message == "" {
			continue
		}

		// 发送消息
		_, err := conn.Write([]byte(message + "\n"))
		if err != nil {
			log.Printf("[ERROR] Failed to send message: %v", err)
			break
		}

		log.Printf("[INFO] Sent message: %s", message)

		// 检查是否退出
		if strings.ToLower(strings.TrimSpace(message)) == "quit" {
			log.Printf("[INFO] Quit command sent, closing connection...")
			time.Sleep(100 * time.Millisecond) // 等待服务器响应
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[ERROR] Error reading input: %v", err)
	}

	log.Printf("[INFO] Client shutting down...")
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
			return
		}

		if n > 0 {
			message := string(buffer[:n])
			// 移除换行符显示
			message = strings.TrimRight(message, "\n\r")
			fmt.Printf("\nServer: %s\n> ", message)
			
			log.Printf("[INFO] Received %d bytes from server: %s", n, message)
		}
	}
}
