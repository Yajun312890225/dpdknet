# TCP Client Example

这个示例演示了如何使用 DPDK 网络库创建一个高性能的 TCP 客户端。

## 功能特性

- **高性能连接**: 使用 DPDK 进行高速网络通信
- **交互式界面**: 支持实时输入和显示
- **双向通信**: 同时处理发送和接收
- **连接管理**: 自动处理连接建立和断开
- **错误恢复**: 智能的错误处理和恢复机制

## 使用方法

### 编译

```bash
cd examples
go build -o tcpclient tcpclient.go
```

### 运行

```bash
# 连接到默认服务器 127.0.0.1:8080
./tcpclient

# 连接到指定服务器
./tcpclient 192.168.1.100:9000
```

### 命令行参数

- `server_address`: 服务器地址，格式为 `ip:port` (可选，默认 127.0.0.1:8080)

## 使用示例

### 交互会话
```
Connected! Type messages to send (type 'quit' to exit):
> Hello Server
Server: Echo: Hello Server
> How are you?
Server: Echo: How are you?
> quit
Server: Goodbye!
[INFO] Client shutting down...
```

## 功能详解

### 连接建立
- 自动 TCP 三次握手
- 连接状态监控
- 失败重试机制

### 消息处理
- **发送**: 用户输入实时发送到服务器
- **接收**: 异步接收服务器响应
- **显示**: 格式化显示服务器响应

### 特殊命令
- `quit`: 断开连接并退出客户端
- 空行: 忽略，不发送到服务器

## 示例输出

```
[INFO] Starting TCP Client example...
[INFO] Connecting to TCP server 127.0.0.1:8080...
[INFO] Connected to server 127.0.0.1:8080 from local 192.168.1.100:54321
Connected! Type messages to send (type 'quit' to exit):
> test message
[INFO] Sent message: test message
Server: Echo: test message
[INFO] Received 18 bytes from server: Echo: test message
> quit
[INFO] Quit command sent, closing connection...
[INFO] Client shutting down...
```

## 协议特性

### 数据传输
- 支持任意长度消息
- 自动处理 TCP 分片
- 保证数据顺序和完整性

### 连接管理
- 自动心跳检测
- 超时处理
- 优雅断开连接

### 错误处理
- 网络异常自动重连
- 数据传输错误检测
- 完整的错误日志记录

## 测试场景

### 基础功能测试
```bash
# 1. 启动服务器
./tcpserver

# 2. 在另一个终端启动客户端
./tcpclient

# 3. 发送测试消息
> Hello
> 123456
> quit
```

### 长连接测试
```bash
# 保持连接并发送大量消息
./tcpclient
> message1
> message2
> ... (发送多条消息)
> quit
```

### 并发测试
```bash
# 启动多个客户端实例
for i in {1..5}; do ./tcpclient & done
```

## 高级用法

### 自定义本地地址
修改代码中的 `DialTCP` 调用：
```go
localAddr := &dpdknet.TCPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 0}
conn, err := dpdknet.DialTCP("tcp", localAddr, addr)
```

### 批量消息发送
可以修改客户端支持文件输入或批量命令。

### 性能测试模式
添加性能测试功能，测量延迟和吞吐量。

## 故障排除

### 连接问题
1. **连接被拒绝**
   ```
   Error: Failed to connect to server: connection refused
   ```
   - 检查服务器是否运行
   - 验证服务器地址和端口
   - 检查防火墙设置

2. **连接超时**
   ```
   Error: Failed to connect to server: timeout
   ```
   - 检查网络连通性
   - 验证服务器负载
   - 调整超时参数

### 数据传输问题
1. **发送失败**
   - 检查连接状态
   - 验证数据格式
   - 查看网络状况

2. **接收超时**
   - 检查服务器响应
   - 调整读取超时
   - 验证数据完整性

### DPDK 相关问题
1. **初始化失败**
   - 检查 DPDK 环境
   - 验证权限设置
   - 确认 hugepages 配置

## 扩展功能

### 文件传输
可以扩展支持文件上传和下载功能。

### 加密通信
添加 TLS/SSL 支持提供安全连接。

### 协议扩展
支持自定义应用层协议。

### GUI 界面
可以开发图形化客户端界面。

## 性能调优

- 调整缓冲区大小优化内存使用
- 使用连接池减少连接开销  
- 启用 TCP_NODELAY 减少延迟
- 优化数据序列化格式
