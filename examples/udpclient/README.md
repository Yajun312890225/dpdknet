# UDP Client Example

这个示例演示了如何使用 DPDK 网络库创建一个高性能的 UDP 客户端。

## 功能特性

- **高速数据传输**: 基于 DPDK 的零拷贝网络处理
- **交互式界面**: 支持实时命令输入
- **RTT 测量**: 自动测量往返时间
- **多命令支持**: 内置多种测试命令
- **错误处理**: 智能的超时和错误恢复

## 使用方法

### 编译

```bash
cd examples
go build -o udpclient udpclient.go
```

### 运行

```bash
# 连接到默认服务器 127.0.0.1:9090
./udpclient

# 连接到指定服务器
./udpclient 192.168.1.100:8080
```

### 命令行参数

- `server_address`: 服务器地址，格式为 `ip:port` (可选，默认 127.0.0.1:9090)

## 交互命令

| 命令 | 功能 | 示例输出 |
|------|------|----------|
| `ping` | 测试连通性 | `Server: pong (RTT: 1.2ms)` |
| `time` | 获取服务器时间 | `Server time received: 2023-07-21 15:30:45` |
| `quit` | 退出客户端 | 客户端自动关闭 |
| 自定义消息 | Echo 测试 | `Server: Echo: Hello World` |

## 使用示例

### 基础交互会话
```
Commands: ping, time, quit, or any message to echo
> ping
Server (127.0.0.1:9090): pong (RTT: 856µs)
Ping successful!
> time
Server (127.0.0.1:9090): 2023-07-21 15:30:45 (RTT: 1.2ms)
Server time received: 2023-07-21 15:30:45
> Hello UDP Server
Server (127.0.0.1:9090): Echo: Hello UDP Server (RTT: 945µs)
> quit
[INFO] Quit command sent, exiting...
```

## 示例输出

```
[INFO] Starting UDP Client example...
[INFO] Creating UDP connection to 127.0.0.1:9090...
[INFO] UDP client ready, local address: 0.0.0.0:54321
Commands: ping, time, quit, or any message to echo
> ping
[INFO] Sent message: ping
Server (127.0.0.1:9090): pong (RTT: 856µs)
[INFO] Received response from 127.0.0.1:9090: pong (RTT: 856µs)
Ping successful!
> Hello World
[INFO] Sent message: Hello World
Server (127.0.0.1:9090): Echo: Hello World (RTT: 1.1ms)
[INFO] Received response from 127.0.0.1:9090: Echo: Hello World (RTT: 1.1ms)
```

## 协议特性

### UDP 通信优势
- **低延迟**: 无连接建立开销
- **高效率**: 最小的协议开销
- **简单性**: 无状态通信
- **适合实时应用**: 音视频、游戏等场景

### RTT 测量
客户端自动测量每个请求的往返时间：
- 微秒级精度
- 自动格式化显示
- 性能分析支持

## 测试场景

### 连通性测试
```bash
./udpclient 192.168.1.1:9090
> ping
> ping
> ping
```

### 延迟测试
```bash
# 连续发送 ping 测试延迟稳定性
> ping
> ping
> ping
# 观察 RTT 变化
```

### 吞吐量测试
```bash
# 发送较大的消息测试吞吐量
> This is a long message to test UDP throughput and packet handling capabilities
```

### 服务功能测试
```bash
# 测试所有内置命令
> ping          # 连通性测试
> time          # 时间服务
> Hello         # Echo 服务
> quit          # 优雅退出
```

## 高级用法

### 自动化测试模式
可以修改客户端支持批量命令：

```go
// 示例：批量执行命令
func runBatchCommands(conn *dpdknet.UDPConn, remoteAddr *dpdknet.UDPAddr) {
    commands := []string{"ping", "time", "Hello Batch", "quit"}
    for _, cmd := range commands {
        // 发送命令并接收响应
    }
}
```

### 性能基准测试
添加性能测试功能：

```go
func benchmarkLatency(conn *dpdknet.UDPConn, remoteAddr *dpdknet.UDPAddr, count int) {
    var totalRTT time.Duration
    for i := 0; i < count; i++ {
        start := time.Now()
        // 发送和接收
        totalRTT += time.Since(start)
    }
    avgRTT := totalRTT / time.Duration(count)
    fmt.Printf("Average RTT: %v\n", avgRTT)
}
```

### 并发客户端
启动多个客户端实例进行并发测试：

```bash
# 并发脚本
#!/bin/bash
for i in {1..10}; do
    echo "ping" | ./udpclient &
done
wait
```

## 故障排除

### 连接问题

1. **目标不可达**
   ```
   Error: Failed to receive response: network is unreachable
   ```
   解决方案：
   - 检查服务器是否运行
   - 验证网络路由配置
   - 确认防火墙设置

2. **响应超时**
   ```
   Error: Failed to receive response: timeout
   ```
   解决方案：
   - 增加超时时间
   - 检查服务器负载
   - 验证网络延迟

### DPDK 相关问题

1. **初始化失败**
   ```
   Error: Failed to create UDP connection: DPDK init failed
   ```
   解决方案：
   - 检查 DPDK 环境变量
   - 确认 hugepages 配置
   - 验证权限设置

### 性能问题

1. **高延迟**
   - 检查网络拓扑
   - 监控系统资源使用
   - 优化系统参数

2. **丢包严重**
   - 检查网络质量
   - 调整缓冲区大小
   - 监控服务器状态

## 调试技巧

### 网络调试
```bash
# 使用 ping 测试基础连通性
ping 192.168.1.100

# 使用 traceroute 检查路由
traceroute 192.168.1.100

# 使用 netstat 检查端口状态
netstat -ulnp | grep 9090
```

### 抓包分析
```bash
# 抓取 UDP 数据包
sudo tcpdump -i any -n udp port 9090 -X

# 只显示与特定主机的通信
sudo tcpdump -i any host 192.168.1.100 and udp port 9090
```

### 性能监控
```bash
# 监控客户端资源使用
top -p $(pgrep udpclient)

# 监控网络统计
watch -n 1 'cat /proc/net/snmp | grep Udp'
```

## 扩展功能

### 文件传输客户端
可以扩展支持 UDP 文件传输：
- 分片传输
- 重传机制
- 完整性校验

### 流媒体客户端
适合实时音视频流传输：
- 低延迟传输
- 容错机制
- 自适应码率

### 游戏客户端
适合实时游戏通信：
- 状态同步
- 预测补偿
- 网络优化

## 配置优化

### 缓冲区调优
```go
// 调整接收缓冲区
buffer := make([]byte, 65536) // 64KB 缓冲区

// 调整超时设置
conn.SetReadDeadline(time.Now().Add(1 * time.Second))
```

### 本地端口配置
```go
// 指定本地端口范围
localAddr := &dpdknet.UDPAddr{
    IP:   net.IPv4(0, 0, 0, 0),
    Port: 50000 + rand.Intn(10000), // 50000-59999
}
```

## 性能指标

建议监控以下指标：
- **延迟**: 平均和最大 RTT
- **成功率**: 请求成功比例
- **吞吐量**: 每秒发送的数据包数
- **错误率**: 超时和错误响应比例

## 最佳实践

1. **合理设置超时**: 根据网络条件调整超时参数
2. **错误重试**: 实现智能重试机制
3. **资源清理**: 及时关闭连接释放资源
4. **日志记录**: 记录关键操作便于调试
5. **性能测试**: 定期进行性能基准测试
