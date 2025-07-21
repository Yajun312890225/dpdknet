# DPDK 网络库示例

这个目录包含了使用 DPDK 网络库的各种示例应用程序，展示了如何使用该库进行 TCP、UDP 和 ICMP 协议的网络编程。

## 目录结构

每个示例都在独立的子目录中，避免了 `package main` 的冲突：

```
examples/
├── tcpserver/          # TCP 回显服务器
│   ├── main.go         # 服务器源码
│   ├── go.mod          # 模块文件
│   └── README.md       # 详细文档
├── tcpclient/          # TCP 客户端
│   ├── main.go         # 客户端源码
│   ├── go.mod          # 模块文件
│   └── README.md       # 详细文档
├── udpserver/          # UDP 服务器
│   ├── main.go         # 服务器源码
│   ├── go.mod          # 模块文件
│   └── README.md       # 详细文档
├── udpclient/          # UDP 客户端
│   ├── main.go         # 客户端源码
│   ├── go.mod          # 模块文件
│   └── README.md       # 详细文档
├── icmpserver/         # ICMP 服务器 (Ping 响应)
│   ├── main.go         # 服务器源码
│   ├── go.mod          # 模块文件
│   └── README.md       # 详细文档
└── icmpclient/         # ICMP 客户端 (Ping 工具)
    ├── main.go         # 客户端源码
    ├── go.mod          # 模块文件
    └── README.md       # 详细文档
```

## 快速开始

### 编译示例

每个示例都是独立的 Go 模块，可以单独编译：

```bash
# TCP 服务器
cd tcpserver && go build -o tcpserver

# TCP 客户端  
cd tcpclient && go build -o tcpclient

# UDP 服务器
cd udpserver && go build -o udpserver

# UDP 客户端
cd udpclient && go build -o udpclient

# ICMP 服务器
cd icmpserver && go build -o icmpserver

# ICMP 客户端
cd icmpclient && go build -o icmpclient
```

### 批量编译

使用脚本批量编译所有示例：

```bash
#!/bin/bash
for dir in tcpserver tcpclient udpserver udpclient icmpserver icmpclient; do
    echo "Building $dir..."
    cd $dir
    go build -o $dir
    cd ..
done
```

## 示例说明

### TCP 示例
- **tcpserver**: TCP 回显服务器，接受连接并回显客户端发送的数据
- **tcpclient**: TCP 客户端，连接到服务器并进行交互式通信

### UDP 示例  
- **udpserver**: UDP 服务器，支持多种命令处理 (ping, time, echo, quit)
- **udpclient**: UDP 客户端，支持交互式命令发送和 RTT 测量

### ICMP 示例
- **icmpserver**: ICMP 服务器，响应 ping 请求并提供网络诊断功能
- **icmpclient**: ICMP 客户端，实现类似 ping 命令的功能

## 使用场景

### 学习和测试
- 学习 DPDK 网络编程基础
- 测试网络连通性和性能
- 理解不同协议的实现方式

### 性能基准测试
- 比较 DPDK 与传统网络栈的性能差异
- 测试高并发连接处理能力
- 评估网络延迟和吞吐量

### 开发参考
- 作为开发新应用的参考代码
- 了解最佳实践和错误处理
- 学习并发和异步编程模式

## 运行环境要求

### 系统要求
- Linux 操作系统 (推荐 Ubuntu 18.04+)
- DPDK 库已正确安装和配置
- 足够的 hugepages 内存
- 适当的网卡驱动绑定

### 权限要求
- 大多数示例需要 root 权限或适当的 capabilities
- ICMP 示例特别需要 `CAP_NET_RAW` 权限

### 依赖检查
```bash
# 检查 DPDK 环境
dpdk-hugepages.py --show

# 检查网卡绑定
dpdk-devbind.py --status

# 检查 hugepages
cat /proc/meminfo | grep -i huge
```

## 故障排除

### 常见问题
1. **编译错误**: 检查 DPDK 库是否正确安装
2. **运行时错误**: 检查权限和系统配置
3. **网络连接问题**: 检查防火墙和路由设置

### 调试建议
1. 查看每个示例目录下的 README.md 获取详细信息
2. 使用 `strace` 和 `tcpdump` 等工具进行调试
3. 检查系统日志和 DPDK 日志输出

## 贡献和反馈

如果你发现问题或有改进建议，请：
1. 查看现有的 issue 和 PR
2. 创建详细的 bug 报告或功能请求
3. 提交 pull request 来修复问题或添加功能

## 许可证

这些示例遵循与主项目相同的许可证。

---

更多详细信息请查看各个示例目录中的 README.md 文件。
