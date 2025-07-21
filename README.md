# DPDK Network Library

这是一个基于DPDK的高性能网络库，提供了ICMP、TCP和UDP协议的实现。

## 功能特性

- **ICMP支持**: 实现了ICMP Echo Request/Reply处理，支持ping功能
- **TCP支持**: 提供TCP连接管理，支持服务器和客户端模式
- **UDP支持**: UDP数据包收发功能
- **高性能**: 基于DPDK实现，支持用户态网络处理
- **零拷贝**: 直接操作网络数据包，避免内核态/用户态拷贝

## 项目结构

```
.
├── go.mod              # Go模块文件
├── go.sum              # 依赖校验和
├── dpdk.go             # DPDK初始化
├── icmp.go             # ICMP协议实现
├── tcp.go              # TCP协议实现
├── conn.go             # UDP连接实现
├── listener.go         # 监听器实现
├── utils.go            # 工具函数
└── examples/           # 示例程序
    └── echo_server.go  # 综合echo服务器示例
```

## 主要组件

### 1. ICMP (icmp.go)
- `ICMPConn`: ICMP连接结构
- `HandleICMPManual()`: 手动处理ICMP数据包
- `Ping()`: 发送ping请求

### 2. TCP (tcp.go)
- `TCPConn`: TCP连接结构
- `TCPListener`: TCP监听器
- `HandleTCPManual()`: 手动处理TCP数据包
- 支持TCP三次握手和数据传输

### 3. UDP (conn.go)
- `UDPConn`: UDP连接结构
- `ListenUDP()`: 创建UDP监听器
- `ReadFromUDP()`: 读取UDP数据

### 4. 工具函数 (utils.go)
- `CalcChecksum()`: 计算校验和
- `ParseIPv4Addr()`: 解析IPv4地址
- `SwapIPv4Addrs()`: 交换IP地址
- `IsIPv4Packet()`: 检查是否为IPv4数据包

## 使用示例

### Echo服务器
```go
package main

import (
    "dpdknet"
    "github.com/Yajun312890225/nff-go/flow"
)

func main() {
    // 初始化DPDK
    config := flow.Config{
        TXQueuesNumberPerPort: 1,
        SendCPUCoresPerPort:   1,
        MaxRecv:               1,
        SchedulerInterval:     100,
    }
    
    flow.SystemInit(&config)
    rxFlow, _ := flow.SetReceiver(0)
    flow.SetHandler(rxFlow, EchoHandler, nil)
    flow.SetSender(rxFlow, 0)
    flow.SystemStart()
}
```

### ICMP Ping
```go
localIP := net.IPv4(192, 168, 1, 100)
targetIP := net.IPv4(192, 168, 1, 1)

icmpConn, err := dpdknet.NewICMPConn(localIP)
if err != nil {
    log.Fatal(err)
}
defer icmpConn.Close()

err = icmpConn.Ping(targetIP, 3, 5*time.Second)
```

### TCP服务器
```go
addr, _ := net.ResolveTCPAddr("tcp", ":8080")
listener, err := dpdknet.ListenTCP(addr)
if err != nil {
    log.Fatal(err)
}
defer listener.Close()

conn, err := listener.Accept()
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

// 读写数据
buffer := make([]byte, 1024)
n, err := conn.Read(buffer)
conn.Write(buffer[:n])
```

### UDP服务器
```go
udpConn, err := dpdknet.ListenUDP(9000)
if err != nil {
    log.Fatal(err)
}
defer udpConn.Close()

buffer := make([]byte, 1024)
n, addr, err := udpConn.ReadFromUDP(buffer)
```

## 支持的协议端口

- **ICMP**: 响应所有ping请求
- **TCP**: 默认监听8080端口进行echo
- **UDP**: 默认监听9000端口进行echo

## 编译和运行

1. 确保已安装DPDK环境
2. 绑定网卡到DPDK驱动
3. 编译运行:

```bash
go mod tidy
go build ./examples/echo_server.go
sudo ./echo_server
```

## 注意事项

1. **权限要求**: 需要root权限运行
2. **网卡绑定**: 需要将网卡绑定到DPDK兼容驱动(如igb_uio、vfio-pci)
3. **大页内存**: 需要配置足够的大页内存
4. **CPU隔离**: 建议隔离专用CPU核心给DPDK使用

## 依赖

- Go 1.19+
- github.com/Yajun312890225/nff-go (NFF-Go DPDK框架)

## 性能特点

- 零拷贝数据包处理
- 用户态网络栈
- 支持多队列网卡
- 高并发连接处理
- 低延迟数据传输

## 限制

- 当前实现主要用于演示和测试
- TCP状态机实现简化
- 校验和计算可以进一步优化
- 错误处理需要加强
