# ICMP Server Example

这个示例演示了如何使用 DPDK 网络库创建一个高性能的 ICMP 服务器，主要用于处理 ping 请求和网络诊断。

## 功能特性

- **自动 Ping 响应**: 自动处理 ICMP Echo Request
- **网络层处理**: 基于 DPDK 的底层网络处理
- **多客户端支持**: 同时响应多个客户端的 ping 请求
- **诊断功能**: 提供网络连通性诊断服务
- **演示功能**: 内置 ping 演示和测试

## 使用方法

### 编译

```bash
cd examples
go build -o icmpserver icmpserver.go
```

### 运行

```bash
# 使用自动检测的本地 IP
./icmpserver

# 注意：ICMP 服务器通常需要 root 权限
sudo ./icmpserver
```

## 工作原理

### ICMP 协议处理
- **Echo Request 处理**: 自动响应 ping 请求
- **Echo Reply 生成**: 构造标准的 ping 响应
- **校验和计算**: 自动处理 ICMP 校验和
- **IP 层集成**: 与 IP 协议栈完整集成

### 网络层功能
服务器在网络层自动处理以下功能：
1. 接收 ICMP Echo Request (Type 8)
2. 交换源和目标 IP 地址
3. 修改 ICMP 类型为 Echo Reply (Type 0)
4. 重新计算校验和
5. 发送响应包

## 示例输出

```
[INFO] Starting ICMP Server example...
[INFO] Using local IP: 192.168.1.100
[INFO] Creating ICMP connection...
[INFO] ICMP server started on 192.168.1.100
[INFO] Server is ready to handle ICMP packets...
[INFO] Note: ICMP echo requests are automatically handled by the network layer
[INFO] ICMP server is running... (listening for ping requests)
[INFO] Starting ping demonstration...
[INFO] Attempting to ping 127.0.0.1...
[INFO] Ping to 127.0.0.1 completed
[INFO] Attempting to ping 192.168.1.1...
[INFO] Ping to 192.168.1.1 completed
[INFO] Ping demonstration completed
```

## 测试方法

### 使用系统 ping 命令测试
```bash
# 在另一个终端测试服务器响应
ping 192.168.1.100

# 示例输出：
# PING 192.168.1.100 (192.168.1.100) 56(84) bytes of data.
# 64 bytes from 192.168.1.100: icmp_seq=1 ttl=64 time=0.123 ms
# 64 bytes from 192.168.1.100: icmp_seq=2 ttl=64 time=0.089 ms
```

### 使用 ICMP 客户端测试
```bash
# 使用配套的 ICMP 客户端
./icmpclient 192.168.1.100 5 3
```

### 网络诊断测试
```bash
# 测试不同网络路径
ping -c 4 192.168.1.100          # 本地网络
ping -c 4 -I eth0 192.168.1.100  # 指定接口
ping -c 4 -s 1000 192.168.1.100  # 大数据包测试
```

## 协议特性

### ICMP 消息类型
服务器支持处理以下 ICMP 消息：

| 类型 | 代码 | 描述 | 处理方式 |
|------|------|------|----------|
| 8 | 0 | Echo Request (Ping) | 自动响应 Echo Reply |
| 0 | 0 | Echo Reply (Pong) | 记录和统计 |
| 其他 | - | 其他 ICMP 消息 | 记录日志 |

### 数据包处理流程
1. **接收**: 从网络接口接收 ICMP 数据包
2. **解析**: 解析 IP 头和 ICMP 头
3. **验证**: 验证校验和和数据完整性
4. **处理**: 根据 ICMP 类型进行相应处理
5. **响应**: 生成并发送响应数据包

## 高级功能

### 统计信息收集
服务器可以扩展支持详细的统计信息：

```go
type ICMPStats struct {
    EchoRequestsReceived  uint64
    EchoRepliesSent      uint64
    InvalidPackets       uint64
    ProcessingErrors     uint64
    AverageResponseTime  time.Duration
}
```

### 访问控制
可以添加 IP 访问控制功能：

```go
func isAllowedIP(srcIP net.IP) bool {
    // 实现 IP 白名单或黑名单检查
    allowedSubnets := []string{
        "192.168.0.0/16",
        "10.0.0.0/8",
        "127.0.0.0/8",
    }
    // 检查逻辑
    return true
}
```

### 负载监控
监控服务器负载和性能：

```go
func monitorPerformance() {
    ticker := time.NewTicker(60 * time.Second)
    for range ticker.C {
        log.Printf("[STATS] Processed %d pings in last minute", pingsProcessed)
        pingsProcessed = 0
    }
}
```

## 配置选项

### 网络接口配置
```go
// 指定监听的网络接口
localIP := net.ParseIP("192.168.1.100")  // 指定 IP
localIP := getInterfaceIP("eth0")         // 根据接口获取 IP
```

### DPDK 参数调优
```go
// DPDK 接收队列配置
rxQueueSize := 2048
txQueueSize := 2048

// 内存池配置  
mbufPoolSize := 8192
```

## 故障排除

### 常见问题

1. **权限不足**
   ```
   Error: Permission denied (ICMP requires root privileges)
   ```
   解决方案：
   ```bash
   sudo ./icmpserver
   # 或设置 capabilities
   sudo setcap cap_net_raw+ep ./icmpserver
   ```

2. **网络接口问题**
   ```
   Error: Failed to initialize global network system
   ```
   解决方案：
   - 检查网卡是否绑定到 DPDK
   - 验证 DPDK 环境配置
   - 确认 hugepages 设置

3. **IP 地址冲突**
   ```
   Error: IP address already in use
   ```
   解决方案：
   - 选择不同的 IP 地址
   - 检查网络配置
   - 停止冲突的服务

### 调试技巧

1. **ICMP 数据包捕获**
   ```bash
   # 捕获 ICMP 流量
   sudo tcpdump -i any icmp -v
   
   # 只捕获 ping 相关流量
   sudo tcpdump -i any 'icmp[icmptype] == 8 or icmp[icmptype] == 0'
   ```

2. **网络连通性测试**
   ```bash
   # 测试基础网络连通性
   ip addr show                    # 显示网络接口
   ip route show                   # 显示路由表
   arp -a                         # 显示 ARP 表
   ```

3. **性能分析**
   ```bash
   # 监控系统资源
   top -p $(pgrep icmpserver)
   
   # 监控网络统计
   watch -n 1 'cat /proc/net/snmp | grep Icmp'
   ```

## 部署建议

### 系统要求
- Linux 操作系统
- Root 权限或 CAP_NET_RAW 能力
- DPDK 兼容的网卡
- 足够的 hugepages 内存

### 网络配置
- 确保防火墙允许 ICMP 流量
- 配置正确的 IP 地址和路由
- 验证网络接口状态

### 监控设置
- 配置日志轮转
- 设置性能监控
- 配置告警阈值

## 扩展应用

### 网络监控服务
可以扩展为完整的网络监控服务：
- 主动健康检查
- 网络延迟监控
- 连通性报告

### 负载均衡器健康检查
作为负载均衡器的健康检查后端：
- 快速响应探测
- 状态报告
- 故障检测

### 网络诊断工具
扩展为网络诊断平台：
- MTU 发现
- 路径跟踪
- 质量分析

## 性能优化

### 系统级优化
- 绑定 CPU 亲和性
- 调整内核网络参数
- 优化中断处理

### 应用级优化
- 使用内存池
- 批量数据包处理
- 异步 I/O

### 网络级优化
- 启用网卡硬件特性
- 配置 RSS 多队列
- 优化 DMA 参数

## 监控指标

关键监控指标：
- **响应时间**: ping 响应延迟
- **成功率**: ping 响应成功率
- **吞吐量**: 每秒处理的 ping 数量
- **错误率**: 处理错误和丢包率
- **资源使用**: CPU 和内存使用情况
