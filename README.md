# DPDK Net 包无缝替换指南

## 概述

本项目实现了一个可以无缝替换 Go 标准库 `net` 包的 DPDK 网络库。通过简单的导入语句修改，即可将现有代码从系统调用改为高性能的 DPDK 用户态网络处理。

## 无缝替换方法

### 方法1：导入别名（推荐）

```go
// 原始代码
import "net"

// 替换后代码
import net "github.com/Yajun312890225/dpdknet"
```

### 方法2：批量替换

```bash
# 在项目中批量替换
find . -name "*.go" -exec sed -i 's/import "net"/import net "github.com\/Yajun312890225\/dpdknet"/g' {} \;
```

## 兼容性对比表

### 地址类型 (100% 兼容)

| 标准库 | DPDK Net | 兼容性 | 说明 |
|--------|----------|--------|------|
| `net.TCPAddr` | `dpdknet.TCPAddr` | ✅ 完全兼容 | 相同的字段和方法 |
| `net.UDPAddr` | `dpdknet.UDPAddr` | ✅ 完全兼容 | 相同的字段和方法 |
| `addr.Network()` | `addr.Network()` | ✅ 完全兼容 | 返回值相同 |
| `addr.String()` | `addr.String()` | ✅ 完全兼容 | 格式相同 |

### 连接类型 (核心功能兼容)

| 标准库 | DPDK Net | 兼容性 | 说明 |
|--------|----------|--------|------|
| `net.TCPConn` | `dpdknet.TCPConn` | ✅ 接口兼容 | 实现了 `net.Conn` 接口 |
| `net.UDPConn` | `dpdknet.UDPConn` | ✅ 接口兼容 | 实现了 `net.Conn` 和 `net.PacketConn` |
| `conn.Read()` | `conn.Read()` | ✅ 完全兼容 | 相同签名 |
| `conn.Write()` | `conn.Write()` | ✅ 完全兼容 | 相同签名 |
| `conn.Close()` | `conn.Close()` | ✅ 完全兼容 | 相同签名 |
| `conn.LocalAddr()` | `conn.LocalAddr()` | ✅ 完全兼容 | 返回 `net.Addr` |
| `conn.RemoteAddr()` | `conn.RemoteAddr()` | ✅ 完全兼容 | 返回 `net.Addr` |

### 监听器类型 (兼容)

| 标准库 | DPDK Net | 兼容性 | 说明 |
|--------|----------|--------|------|
| `net.Listener` | `dpdknet.TCPListener` | ✅ 接口兼容 | 实现了 `net.Listener` 接口 |
| `listener.Accept()` | `listener.Accept()` | ✅ 完全兼容 | 返回 `net.Conn` |
| `listener.Close()` | `listener.Close()` | ✅ 完全兼容 | 相同签名 |
| `listener.Addr()` | `listener.Addr()` | ✅ 完全兼容 | 返回 `net.Addr` |

### 网络函数 (部分兼容)

| 标准库 | DPDK Net | 兼容性 | 说明 |
|--------|----------|--------|------|
| `net.Listen()` | `dpdknet.ListenNet()` | ⚠️ 函数名不同 | 功能相同，名称稍异 |
| `net.Dial()` | `dpdknet.Dial()` | ✅ 完全兼容 | 相同签名和功能 |
| `net.ResolveTCPAddr()` | `dpdknet.ResolveTCPAddr()` | ✅ 完全兼容 | 相同签名 |
| `net.ResolveUDPAddr()` | `dpdknet.ResolveUDPAddr()` | ✅ 完全兼容 | 相同签名 |
| `net.DialTCP()` | `dpdknet.DialTCP()` | ✅ 完全兼容 | 相同签名 |
| `net.ListenTCP()` | `dpdknet.ListenTCP()` | ✅ 完全兼容 | 相同签名 |
| `net.ListenUDP()` | `dpdknet.ListenUDP()` | ✅ 完全兼容 | 相同签名 |

## 代码示例对比

### TCP 服务器

```go
// === 标准库版本 ===
package main

import (
    "net"
    "io"
)

func main() {
    listener, err := net.Listen("tcp", ":8080")
    if err != nil {
        panic(err)
    }
    defer listener.Close()

    for {
        conn, err := listener.Accept()
        if err != nil {
            continue
        }
        go io.Copy(conn, conn)
    }
}

// === DPDK 版本（仅需修改import） ===
package main

import (
    net "github.com/Yajun312890225/dpdknet"  // 唯一的修改
    "io"
)

func main() {
    listener, err := net.ListenNet("tcp", ":8080")  // 注意：ListenNet
    if err != nil {
        panic(err)
    }
    defer listener.Close()

    for {
        conn, err := listener.Accept()
        if err != nil {
            continue
        }
        go io.Copy(conn, conn)  // 完全相同的逻辑
    }
}
```

### TCP 客户端

```go
// === 标准库版本 ===
conn, err := net.Dial("tcp", "localhost:8080")
if err != nil {
    return err
}
defer conn.Close()

conn.Write([]byte("hello"))
buffer := make([]byte, 1024)
n, err := conn.Read(buffer)

// === DPDK 版本（无需修改逻辑） ===
conn, err := net.Dial("tcp", "localhost:8080")  // 完全相同
if err != nil {
    return err
}
defer conn.Close()

conn.Write([]byte("hello"))  // 完全相同
buffer := make([]byte, 1024)
n, err := conn.Read(buffer)  // 完全相同
```

### UDP 通信

```go
// === 标准库版本 ===
conn, err := net.ListenUDP("udp", &net.UDPAddr{
    IP: net.IPv4(0, 0, 0, 0),
    Port: 9000,
})

buffer := make([]byte, 1024)
n, addr, err := conn.ReadFromUDP(buffer)
conn.WriteToUDP(buffer[:n], addr)

// === DPDK 版本 ===
conn, err := net.ListenUDP("udp", &net.UDPAddr{  // 类型名相同
    IP: net.IPv4(0, 0, 0, 0),
    Port: 9000,
})

buffer := make([]byte, 1024)
n, addr, err := conn.ReadFromUDP(buffer)  // 方法名相同
conn.WriteToUDP(buffer[:n], addr)       // 方法名相同
```

## 注意事项

### 1. 函数名差异
- `net.Listen()` → `dpdknet.ListenNet()` （避免与listener.go中的函数冲突）
- 其他函数保持相同

### 2. 依赖环境
- DPDK 版本需要在支持 DPDK 的 Linux 环境中运行
- 需要预先配置 DPDK 环境（大页内存、网卡绑定等）
- 需要 root 权限

### 3. 实现状态
- ✅ **TCP**: 基础功能完整，支持连接建立、数据传输
- ✅ **UDP**: 数据收发功能完整
- ✅ **ICMP**: 支持 ping 响应
- ⚠️ **高级特性**: 部分标准库的高级特性（如超时、缓冲区设置等）为简化实现

### 4. 性能优势
- 零拷贝网络处理
- 用户态协议栈
- 避免内核态/用户态切换
- 支持高并发连接

## 迁移检查清单

- [ ] 确认使用的网络功能在兼容列表中
- [ ] 修改 import 语句
- [ ] 将 `net.Listen()` 改为 `net.ListenNet()`
- [ ] 测试基础功能
- [ ] 在 DPDK 环境中部署测试
- [ ] 性能基准测试

## 总结

DPDK Net 包提供了与标准库高度兼容的 API，只需要最小的代码修改即可获得 DPDK 的高性能优势。主要的兼容性保证：

1. **类型兼容**: 地址类型完全兼容
2. **接口兼容**: 连接和监听器实现了标准接口
3. **方法兼容**: 核心方法签名保持一致
4. **行为兼容**: 网络操作行为与标准库一致

通过这种设计，现有的网络应用可以以最小的代码改动获得 DPDK 的性能提升。
