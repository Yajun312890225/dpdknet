# ICMP Client (Ping) Example

这个示例演示了如何使用 DPDK 网络库创建一个高性能的 ICMP 客户端，实现类似 ping 命令的功能。

## 功能特性

- **高性能 Ping**: 基于 DPDK 的高速 ICMP 通信
- **精确测量**: 微秒级 RTT 测量精度
- **批量测试**: 支持发送多个 ping 包
- **可配置参数**: 灵活的超时和重试设置
- **详细统计**: 完整的连接质量统计信息

## 使用方法

### 编译

```bash
cd examples
go build -o icmpclient icmpclient.go
```

### 运行

```bash
# 基础用法
./icmpclient 192.168.1.1

# 指定参数
./icmpclient 8.8.8.8 10 3
#            目标IP  包数量 超时秒数

# 需要 root 权限
sudo ./icmpclient 192.168.1.1
```

### 命令行参数

| 参数 | 描述 | 默认值 | 示例 |
|------|------|--------|------|
| `target_ip` | 目标 IP 地址 | 必需 | `192.168.1.1` |
| `count` | 发送包数量 | 4 | `10` |
| `timeout` | 超时时间(秒) | 5 | `3` |

## 使用示例

### 基础 Ping 测试
```bash
$ sudo ./icmpclient 192.168.1.1
[INFO] Starting ICMP Client (Ping) example...
[INFO] Local IP: 192.168.1.100
[INFO] Target IP: 192.168.1.1
[INFO] Count: 4, Timeout: 5 seconds
[INFO] ICMP connection created successfully
PING 192.168.1.1 (192.168.1.1): 56 data bytes

--- 192.168.1.1 ping statistics ---
4 packets transmitted, time 3.2s
Average time: 800ms per packet
[INFO] Ping completed successfully
```

### 高频率测试
```bash
$ sudo ./icmpclient 8.8.8.8 20 2
PING 8.8.8.8 (8.8.8.8): 56 data bytes

--- 8.8.8.8 ping statistics ---
20 packets transmitted, time 15.6s
Average time: 780ms per packet
```

### 本地回环测试
```bash
$ sudo ./icmpclient 127.0.0.1 10 1
PING 127.0.0.1 (127.0.0.1): 56 data bytes

--- 127.0.0.1 ping statistics ---
10 packets transmitted, time 2.1s
Average time: 210ms per packet
```

## 测试目标

### 常见测试目标

| 目标 | IP 地址 | 描述 | 用途 |
|------|---------|------|------|
| 本地回环 | 127.0.0.1 | 本机回环接口 | 基础功能测试 |
| 本地网关 | 192.168.1.1 | 默认网关 | 本地网络测试 |
| Google DNS | 8.8.8.8 | 公共 DNS 服务器 | 互联网连通测试 |
| Cloudflare DNS | 1.1.1.1 | 公共 DNS 服务器 | 网络质量测试 |

### 测试场景

1. **连通性测试**
   ```bash
   # 测试基础连通性
   sudo ./icmpclient 192.168.1.1 3 5
   ```

2. **延迟测试**
   ```bash
   # 测试网络延迟
   sudo ./icmpclient 8.8.8.8 50 2
   ```

3. **稳定性测试**
   ```bash
   # 长时间测试网络稳定性
   sudo ./icmpclient 1.1.1.1 1000 1
   ```

4. **超时测试**
   ```bash
   # 测试超时处理
   sudo ./icmpclient 192.168.99.99 5 1  # 不存在的地址
   ```

## 协议详解

### ICMP Echo Request 格式
```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Type      |     Code      |          Checksum             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           Identifier          |        Sequence Number        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Data ...
+-+-+-+-+-
```

### RTT 计算原理
1. **发送时间戳**: 发送 ICMP Echo Request 时记录时间
2. **接收时间戳**: 收到 ICMP Echo Reply 时记录时间
3. **RTT 计算**: RTT = 接收时间 - 发送时间
4. **精度**: 使用 Go 的 time.Now() 提供纳秒级精度

## 高级功能

### 统计分析
客户端可以扩展统计功能：

```go
type PingStats struct {
    PacketsSent      int
    PacketsReceived  int
    PacketsLost      int
    MinRTT          time.Duration
    MaxRTT          time.Duration
    AvgRTT          time.Duration
    StdDevRTT       time.Duration
}
```

### 并发 Ping
支持同时 ping 多个目标：

```go
func concurrentPing(targets []string) {
    var wg sync.WaitGroup
    for _, target := range targets {
        wg.Add(1)
        go func(t string) {
            defer wg.Done()
            pingTarget(t)
        }(target)
    }
    wg.Wait()
}
```

### 数据包大小测试
测试不同大小的 ICMP 数据包：

```go
func testPacketSizes(target string) {
    sizes := []int{64, 128, 256, 512, 1024, 1472} // MTU discovery
    for _, size := range sizes {
        fmt.Printf("Testing packet size %d bytes...\n", size)
        // 发送指定大小的 ping 包
    }
}
```

## 故障排除

### 常见错误

1. **权限不足**
   ```
   Error: Failed to create ICMP connection: permission denied
   ```
   解决方案：
   ```bash
   sudo ./icmpclient 192.168.1.1
   # 或设置 capabilities
   sudo setcap cap_net_raw+ep ./icmpclient
   ```

2. **网络不可达**
   ```
   Error: Ping failed: network is unreachable
   ```
   解决方案：
   - 检查路由配置
   - 验证目标 IP 地址
   - 检查防火墙设置

3. **超时错误**
   ```
   Request timeout for seq=1
   ```
   解决方案：
   - 增加超时时间
   - 检查网络延迟
   - 验证目标主机状态

### 网络诊断

1. **路由跟踪**
   ```bash
   # 检查到目标的路由路径
   traceroute 8.8.8.8
   mtr 8.8.8.8  # 实时路由监控
   ```

2. **网络接口检查**
   ```bash
   # 检查网络接口状态
   ip link show
   ip addr show
   ip route show
   ```

3. **防火墙检查**
   ```bash
   # 检查 iptables 规则
   sudo iptables -L -n -v
   
   # 检查 ICMP 是否被阻止
   sudo iptables -L | grep -i icmp
   ```

## 性能测试

### 延迟基准测试
```bash
#!/bin/bash
# 延迟基准测试脚本

targets=("127.0.0.1" "192.168.1.1" "8.8.8.8" "1.1.1.1")

for target in "${targets[@]}"; do
    echo "Testing $target..."
    sudo ./icmpclient $target 100 2
    echo "---"
done
```

### 并发性能测试
```bash
#!/bin/bash
# 并发测试脚本

for i in {1..10}; do
    sudo ./icmpclient 8.8.8.8 10 3 &
done
wait
echo "All concurrent tests completed"
```

### 吞吐量测试
```bash
# 高频率 ping 测试
time sudo ./icmpclient 127.0.0.1 10000 1
```

## 调试技巧

### 数据包捕获
```bash
# 捕获 ICMP 流量进行分析
sudo tcpdump -i any icmp -v -n

# 只捕获 ping 相关的数据包
sudo tcpdump -i any 'icmp[icmptype] == 8 or icmp[icmptype] == 0' -v
```

### 系统监控
```bash
# 监控客户端资源使用
top -p $(pgrep icmpclient)

# 监控网络统计
watch -n 1 'cat /proc/net/snmp | grep Icmp'
```

### 详细日志
```bash
# 启用详细日志记录
DPDK_LOG_LEVEL=DEBUG sudo ./icmpclient 192.168.1.1
```

## 扩展应用

### 网络监控工具
扩展为网络监控和报警系统：
- 持续监控关键主机
- 延迟阈值报警
- 可用性统计

### MTU 发现工具
实现 Path MTU Discovery：
- 逐渐增大包大小
- 检测 MTU 限制
- 优化网络配置

### 网络质量分析
实现网络质量评估：
- 丢包率分析
- 延迟抖动测量
- 带宽估算

## 最佳实践

1. **权限管理**
   - 使用最小权限原则
   - 配置 capabilities 而不是 root
   - 在生产环境中谨慎使用

2. **参数调优**
   - 根据网络条件调整超时
   - 合理设置包数量
   - 避免过于频繁的测试

3. **错误处理**
   - 实现完整的错误处理
   - 提供有意义的错误信息
   - 支持优雅降级

4. **性能优化**
   - 使用合适的缓冲区大小
   - 避免不必要的内存分配
   - 优化数据包处理路径

## 配置建议

### 系统配置
```bash
# 调整系统网络参数
echo 1 > /proc/sys/net/ipv4/icmp_echo_ignore_all  # 禁用系统 ping 响应
echo 0 > /proc/sys/net/ipv4/icmp_echo_ignore_all  # 启用系统 ping 响应
```

### DPDK 配置
```bash
# 配置 hugepages
echo 1024 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

# 绑定网卡
dpdk-devbind.py --bind=uio_pci_generic 0000:01:00.0
```
