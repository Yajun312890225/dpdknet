# UDP Server Example

这个示例演示了如何使用 DPDK 网络库创建一个高性能的 UDP 服务器。

## 功能特性

- **高性能数据包处理**: 使用 DPDK 零拷贝技术
- **无连接通信**: UDP 协议的快速数据交换
- **多客户端支持**: 同时处理多个客户端请求
- **智能响应**: 根据不同命令提供不同响应
- **统计监控**: 实时统计数据包流量

## 使用方法

### 编译

```bash
cd examples
go build -o udpserver udpserver.go
```

### 运行

```bash
# 使用默认端口 9090
./udpserver

# 指定端口
./udpserver 8080
```

### 命令行参数

- `port`: 监听端口号 (可选，默认 9090)

## 支持的命令

| 命令 | 响应 | 说明 |
|------|------|------|
| `ping` | `pong` | 连通性测试 |
| `time` | 当前时间 | 获取服务器时间 |
| `quit` | `goodbye` | 断开通信 |
| 其他 | `Echo: <message>` | 回显消息 |

## 示例输出

```
[INFO] Starting UDP Server example...
[INFO] Creating UDP listener on 0.0.0.0:9090...
[INFO] UDP server listening on 0.0.0.0:9090
[INFO] Server ready to receive packets...
[INFO] Received 4 bytes from 192.168.1.100:54321: ping
[INFO] Sent response to 192.168.1.100:54321: pong
[STATS] Client: 192.168.1.100:54321, Received: 4 bytes, Sent: 4 bytes
[INFO] Received 4 bytes from 192.168.1.100:54321: time
[INFO] Sent response to 192.168.1.100:54321: 2023-07-21 15:30:45
[STATS] Client: 192.168.1.100:54321, Received: 4 bytes, Sent: 19 bytes
```

## 协议特性

### UDP 协议优势
- **低延迟**: 无连接建立开销
- **高吞吐**: 最小的协议头开销
- **简单性**: 无状态通信模型
- **广播支持**: 支持一对多通信

### 数据包处理
- **异步处理**: 非阻塞数据包接收
- **并发响应**: 同时处理多个客户端
- **错误恢复**: 自动处理损坏的数据包

## 测试方法

### 使用 netcat 测试
```bash
# 安装 netcat
sudo apt-get install netcat

# 测试连接
echo "ping" | nc -u localhost 9090
echo "time" | nc -u localhost 9090
echo "Hello UDP Server" | nc -u localhost 9090
```

### 使用自定义客户端
```bash
# 启动 UDP 客户端
./udpclient localhost:9090
```

### 性能测试
```bash
# 使用 hping3 进行压力测试
sudo hping3 -2 -p 9090 -c 1000 localhost

# 使用多个客户端并发测试
for i in {1..10}; do ./udpclient localhost:9090 & done
```

## 高级功能

### 统计信息
服务器自动记录以下统计信息：
- 客户端连接数
- 数据包收发量
- 错误包计数
- 响应时间分布

### 负载均衡
可以扩展支持多个服务器实例进行负载均衡：

```bash
# 启动多个服务器实例
./udpserver 9090 &
./udpserver 9091 &
./udpserver 9092 &
```

### 数据包过滤
添加数据包过滤功能，支持黑名单和白名单：

```go
// 示例：IP 白名单检查
func isAllowedIP(clientIP net.IP) bool {
    allowedIPs := []string{"127.0.0.1", "192.168.1.0/24"}
    // 实现 IP 检查逻辑
    return true
}
```

## 配置选项

### 缓冲区调优
```go
// 调整接收缓冲区大小
buffer := make([]byte, 8192) // 增大缓冲区

// 调整通道容量
connCh := make(chan []byte, 10000) // 增加队列深度
```

### 超时设置
```go
// 设置读取超时
conn.SetReadDeadline(time.Now().Add(30 * time.Second))

// 设置写入超时  
conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
```

## 故障排除

### 常见问题

1. **端口占用错误**
   ```
   Error: Failed to create UDP listener: bind: address already in use
   ```
   解决方案：
   - 检查端口使用情况: `netstat -ulnp | grep 9090`
   - 更换端口或终止占用进程

2. **权限不足**
   ```
   Error: Permission denied
   ```
   解决方案：
   - 使用 `sudo` 运行
   - 或使用 > 1024 的端口号

3. **DPDK 初始化失败**
   ```
   Error: Failed to initialize global network system
   ```
   解决方案：
   - 检查 DPDK 环境配置
   - 确认 hugepages 设置
   - 验证网卡绑定状态

### 调试技巧

1. **抓包分析**
   ```bash
   # 使用 tcpdump 监控 UDP 流量
   sudo tcpdump -i any -n udp port 9090
   ```

2. **性能监控**
   ```bash
   # 监控系统资源使用
   top -p $(pgrep udpserver)
   
   # 监控网络统计
   watch -n 1 'cat /proc/net/snmp | grep Udp'
   ```

3. **日志分析**
   ```bash
   # 过滤关键日志
   ./udpserver | grep -E "(ERROR|WARNING)"
   
   # 统计连接数
   ./udpserver | grep "Received.*bytes" | wc -l
   ```

## 扩展应用

### DNS 服务器
可以基于此 UDP 服务器实现 DNS 解析服务。

### 游戏服务器
UDP 的低延迟特性适合实时游戏服务器。

### 日志收集器
可以作为日志收集服务器接收应用日志。

### 监控代理
实现系统监控数据的收集和转发。

## 性能优化

### 系统级优化
- 调整内核网络参数
- 配置 CPU 亲和性
- 优化内存分配策略

### 应用级优化
- 使用内存池减少 GC 压力
- 批量处理数据包
- 异步 I/O 操作

### 网络级优化
- 启用网卡多队列
- 配置中断合并
- 调整接收缓冲区大小

## 监控指标

建议监控以下关键指标：
- **吞吐量**: 每秒处理的数据包数
- **延迟**: 请求响应时间
- **错误率**: 丢包率和错误包比例
- **资源使用**: CPU 和内存使用情况
