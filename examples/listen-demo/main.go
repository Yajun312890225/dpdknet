package main

import (
	"fmt"
	"log"

	"github.com/Yajun312890225/dpdknet"
)

func main() {
	// 演示如何使用 dpdknet.Listen 函数
	// 这个函数提供与标准库 net.Listen 完全兼容的接口

	fmt.Println("DPDK 网络库 Listen 函数示例")
	fmt.Println("=============================")

	// 使用 dpdknet.Listen 创建TCP监听器
	// 语法与标准库完全相同
	listener, err := dpdknet.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("创建监听器失败: %v", err)
	}
	defer listener.Close()

	fmt.Printf("TCP 服务器监听地址: %s\n", listener.Addr().String())
	fmt.Println("等待客户端连接...")

	// 演示兼容性 - 可以直接替换标准库导入
	fmt.Println("\n兼容性说明:")
	fmt.Println("标准库用法:  listener, err := net.Listen(\"tcp\", \":8080\")")
	fmt.Println("DPDK库用法: listener, err := dpdknet.Listen(\"tcp\", \":8080\")")
	fmt.Println("两者接口完全相同，可以直接替换！")

	// 接受一个连接进行演示
	conn, err := listener.Accept()
	if err != nil {
		log.Printf("接受连接失败: %v", err)
		return
	}
	defer conn.Close()

	fmt.Printf("客户端连接成功: %s\n", conn.RemoteAddr().String())

	// 发送欢迎消息
	message := "欢迎使用 DPDK 网络库！\n"
	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Printf("发送消息失败: %v", err)
		return
	}

	fmt.Println("演示完成")
}
