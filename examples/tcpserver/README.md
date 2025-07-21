# TCP Server Example

这个示例演示了如何使用 DPDK 网络库创建一个高性能的 TCP 服务器。

## 功能特性

- **高性能网络处理**: 使用 DPDK 进行零拷贝数据包处理
- **并发连接**: 支持多个客户端同时连接
- **Echo 服务**: 回显客户端发送的所有数据
- **优雅关闭**: 支持信号处理和优雅关闭
- **连接管理**: 自动处理连接建立和断开

## 使用方法

### 编译

```bash
cd examples
go build -o tcpserver tcpserver.go
```

### 运行

```bash
# 使用默认端口 8080
./tcpserver

# 指定端口
./tcpserver 9000
```

### 命令行参数

- `port`: 监听端口号 (可选，默认 8080)

## 示例输出

```
[INFO] Starting TCP Server example...
[INFO] Creating TCP listener on 0.0.0.0:8080...
[INFO] TCP server listening on 0.0.0.0:8080
[INFO] Server ready to accept connections...
[INFO] New connection from 192.168.1.100:54321
[INFO] Handling connection from 192.168.1.100:54321
[INFO] Received 12 bytes from 192.168.1.100:54321: Hello Server
[INFO] Echoed 18 bytes to 192.168.1.100:54321
```

## 协议特性

### 连接处理
- TCP 三次握手自动处理
- 连接状态跟踪
- 自动资源清理

### 数据传输
- 支持任意大小的数据包
- 自动分片和重组
- 流量控制和拥塞控制

### 错误处理
- 连接超时检测
- 异常连接自动清理
- 完整的错误日志

## 测试方法

### 使用 telnet 测试
```bash
telnet localhost 8080
```

### 使用自定义客户端
```bash
# 在另一个终端运行
./tcpclient localhost:8080
```

### 性能测试
```bash
# 使用多个客户端连接测试并发性能
for i in {1..10}; do ./tcpclient localhost:8080 & done
```

## 配置说明

### DPDK 配置
- 需要配置 hugepages
- 需要绑定网卡到 DPDK
- 确保有足够的 CPU 核心

### 网络配置
- 确保防火墙允许指定端口
- 检查端口是否被其他程序占用

## 故障排除

### 常见问题

1. **端口占用**
   ```
   Error: Failed to create TCP listener: address already in use
   ```
   解决方案：更换端口或关闭占用端口的程序

2. **DPDK 初始化失败**
   ```
   Error: Failed to initialize global network system
   ```
   解决方案：检查 DPDK 环境配置

3. **权限不足**
   ```
   Error: Permission denied
   ```
   解决方案：使用 sudo 运行或配置用户权限

### 调试选项
- 检查日志输出中的 [DEBUG] 信息
- 使用 `netstat -tlnp` 检查端口状态
- 使用 `tcpdump` 抓包分析网络流量

## 扩展功能

### 添加 HTTP 支持
可以在现有 TCP 服务器基础上添加 HTTP 协议解析。

### 添加 TLS/SSL 支持
集成 TLS 库提供加密连接支持。

### 添加负载均衡
支持多个工作进程分担连接处理。

## 性能优化

- 调整接收缓冲区大小
- 优化数据包处理路径
- 使用内存池减少分配开销
- 启用 CPU 亲和性绑定
