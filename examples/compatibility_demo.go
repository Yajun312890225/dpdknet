package main

import (
	"fmt"
	"io"
	"log"

	// 这里只需要把 "net" 改成 "github.com/Yajun312890225/dpdknet" 即可
	net "github.com/Yajun312890225/dpdknet"
)

// 示例：TCP Echo服务器，可以无缝替换标准库
func tcpEchoServer() {
	// 原始代码：listener, err := net.Listen("tcp", ":8080")
	listener, err := net.ListenNet("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	fmt.Println("TCP server listening on :8080")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		go func() {
			defer conn.Close()

			// 标准的io.Copy可以直接使用
			_, err := io.Copy(conn, conn)
			if err != nil {
				log.Printf("Copy error: %v", err)
			}
		}()
	}
}

// 示例：TCP客户端，可以无缝替换标准库
func tcpClient() {
	// 原始代码：conn, err := net.Dial("tcp", "localhost:8080")
	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// 发送数据
	message := "Hello, DPDK TCP!"
	n, err := conn.Write([]byte(message))
	if err != nil {
		log.Printf("Write error: %v", err)
		return
	}
	fmt.Printf("Sent %d bytes: %s\n", n, message)

	// 读取回应
	buffer := make([]byte, 1024)
	n, err = conn.Read(buffer)
	if err != nil {
		log.Printf("Read error: %v", err)
		return
	}
	fmt.Printf("Received %d bytes: %s\n", n, string(buffer[:n]))
}

// 示例：UDP客户端，可以无缝替换标准库
func udpClient() {
	// 原始代码：conn, err := net.Dial("udp", "localhost:9000")
	conn, err := net.Dial("udp", "localhost:9000")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// 发送数据
	message := "Hello, DPDK UDP!"
	n, err := conn.Write([]byte(message))
	if err != nil {
		log.Printf("Write error: %v", err)
		return
	}
	fmt.Printf("UDP sent %d bytes: %s\n", n, message)
}

// 示例：使用特定的TCP方法
func specificTCPExample() {
	// 使用特定的TCP地址解析
	addr, err := net.ResolveTCPAddr("tcp", "localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	// 使用特定的TCP连接方法
	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Printf("Connected to %s from %s\n", conn.RemoteAddr(), conn.LocalAddr())
}

// 示例：使用特定的UDP方法
func specificUDPExample() {
	// 监听UDP
	addr, err := net.ResolveUDPAddr("udp", ":9000")
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Printf("UDP server listening on %s\n", conn.LocalAddr())

	// 读取数据
	buffer := make([]byte, 1024)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("ReadFromUDP error: %v", err)
			break
		}

		fmt.Printf("Received from %s: %s\n", clientAddr, string(buffer[:n]))

		// 回显数据
		_, err = conn.WriteToUDP(buffer[:n], clientAddr)
		if err != nil {
			log.Printf("WriteToUDP error: %v", err)
		}

		break // 演示用，只处理一次
	}
}

func main() {
	fmt.Println("DPDK Net Package - Drop-in Replacement Demo")
	fmt.Println("===========================================")

	fmt.Println("1. TCP specific example:")
	specificTCPExample()

	fmt.Println("\n2. UDP specific example:")
	specificUDPExample()

	// 注意：在实际应用中，你需要在支持DPDK的环境中运行
	// 这个演示展示了API的兼容性
	fmt.Println("\nAPI compatibility demonstration completed.")
	fmt.Println("To run actual network operations, deploy in DPDK-enabled environment.")
}
