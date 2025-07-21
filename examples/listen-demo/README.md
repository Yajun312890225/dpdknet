# Listen 函数演示

本示例演示了 DPDK 网络库中的 `Listen` 函数，该函数提供与标准库 `net.Listen` 完全兼容的接口。

## 功能特点

- ✅ **完全兼容**：与标准库 `net.Listen` 接口完全相同
- ✅ **高性能**：基于 DPDK 的用户态网络栈
- ✅ **直接替换**：可以直接替换现有代码中的标准库调用

## 接口对比

### 标准库
```go
import "net"

listener, err := net.Listen("tcp", ":8080")
```

### DPDK 网络库
```go
import "github.com/Yajun312890225/dpdknet"

listener, err := dpdknet.Listen("tcp", ":8080")
```

两者接口完全相同，只需要更改导入路径即可！

## 编译运行

### 1. 设置环境变量
```bash
source /tmp/dpdk-env.sh
```

### 2. 编译
```bash
CGO_LDFLAGS_ALLOW='-Wl,.*' go build -o listen-demo main.go
```

### 3. 运行
```bash
# 需要root权限或适当的capabilities
sudo ./listen-demo
```

## 预期输出
```
DPDK 网络库 Listen 函数示例
=============================
TCP 服务器监听地址: 0.0.0.0:8080
等待客户端连接...

兼容性说明:
标准库用法:  listener, err := net.Listen("tcp", ":8080")
DPDK库用法: listener, err := dpdknet.Listen("tcp", ":8080")
两者接口完全相同，可以直接替换！

客户端连接成功: 192.168.1.100:45678
演示完成
```

## 测试连接

可以使用任何TCP客户端连接到服务器，例如：

```bash
# 使用telnet测试
telnet localhost 8080

# 使用netcat测试
nc localhost 8080

# 使用curl测试
curl telnet://localhost:8080
```

## 优势说明

1. **API兼容性**：现有使用标准库 `net.Listen` 的代码可以无缝迁移
2. **性能提升**：利用DPDK用户态网络栈，显著提升网络性能
3. **零学习成本**：保持熟悉的标准库接口，无需学习新的API

## 注意事项

- 需要DPDK环境支持
- 需要适当的系统权限
- 建议在生产环境中配置hugepages和网卡绑定
