# DPDK 数据包分流转发器

这个项目实现了一个基于 DPDK 的数据包分流和转发系统，可以实现以下功能：

1. **DPDK 绑定 eth0**：使用 DPDK 高性能收包
2. **智能分流**：根据过滤器判断哪些包需要自己处理，哪些包转发
3. **TUN 设备转发**：不需要的包转发到 TUN 虚拟网卡
4. **双向转发**：监听 TUN 设备，将收到的包通过 DPDK 发出

## 架构设计

```
┌─────────────┐    ┌──────────────────┐    ┌─────────────┐
│   eth0      │───▶│  DPDK 数据包     │───▶│  自己处理   │
│  (物理网卡)  │    │    分流器        │    │ (VXLAN/ARP) │
└─────────────┘    │                  │    └─────────────┘
                   │   ┌──────────┐   │
                   └──▶│   TUN    │───┘
                       │ 虚拟网卡  │
                       └────┬─────┘
                            │
                       ┌────▼─────┐
                       │ 内核协议栈 │
                       │   或其他   │
                       │  处理程序  │
                       └──────────┘
```

## 核心组件

### 1. TunDevice (tun.go)
- 创建和管理 TUN 虚拟网卡
- 双向数据传输：收发 IP 包
- 自动配置 IP 地址和路由

### 2. PacketForwarder (packet_forwarder.go)
- 数据包分流管理器
- 支持多种过滤器
- 统计信息收集

### 3. 过滤器系统
- **IPFilter**: 基于 IP 地址和网段的过滤
- **VXLANFilter**: VXLAN 协议包过滤
- **PortFilter**: 基于端口的过滤

## 使用方法

### 1. 编译示例程序

```bash
cd examples/packet_forwarder_simple
go build -o packet_forwarder main.go
```

### 2. 运行程序（需要 root 权限）

```bash
sudo ./packet_forwarder
```

### 3. 配置过滤器

```go
// 创建转发器
forwarder, err := dpdknet.NewPacketForwarder("tun0", tunIP, tunMask)

// 添加VXLAN过滤器
vxlanFilter := dpdknet.NewVXLANFilter("VXLAN过滤器", 66)
forwarder.AddFilter(vxlanFilter)

// 添加IP过滤器
targetIPs := []string{"10.10.10.4", "192.168.66.57"}
targetNetworks := []string{"10.10.10.0/24"}
ipFilter := dpdknet.NewIPFilter("IP过滤器", targetIPs, targetNetworks)
forwarder.AddFilter(ipFilter)

// 添加端口过滤器
targetPorts := []uint16{4789, 22, 80, 443}
portFilter := dpdknet.NewPortFilter("端口过滤器", targetPorts)
forwarder.AddFilter(portFilter)
```

## 工作流程

1. **包接收**: DPDK 从 eth0 接收所有数据包
2. **过滤判断**: 应用配置的过滤器，判断是否需要自己处理
3. **本地处理**: 匹配过滤器的包进行本地处理（VXLAN 解封装、ARP 响应等）
4. **转发到 TUN**: 不匹配的包转发到 TUN 设备
5. **TUN 监听**: 监听 TUN 设备收到的包
6. **DPDK 发送**: 将 TUN 收到的包通过 DPDK 发出

## 配置示例

### VXLAN 配置
```go
vxlanConfig := &dpdknet.VXLANConfig{
    VNI:       66,
    LocalIP:   net.IPv4(192, 168, 66, 57),
    RemoteIP:  net.IPv4(192, 168, 66, 29),
    UDPPort:   4789,
    LocalMAC:  net.HardwareAddr{0x20, 0x90, 0x6f, 0x6b, 0x63, 0x8a},
    RemoteMAC: net.HardwareAddr{0xd8, 0x85, 0xc0, 0xa8, 0x42, 0x1d},
    ARPCache:  make(map[string]net.HardwareAddr),
}
```

### TUN 设备配置
```go
tunIP := net.IPv4(192, 168, 100, 1)      // TUN 设备IP
tunMask := net.IPv4Mask(255, 255, 255, 0) // 子网掩码
```

## 系统要求

1. **Linux 系统**: 需要 `/dev/net/tun` 支持
2. **Root 权限**: 创建 TUN 设备需要管理员权限
3. **DPDK 环境**: 已正确配置 DPDK 环境
4. **网卡绑定**: eth0 已绑定到 DPDK 驱动

## 监控和调试

程序提供详细的日志输出和统计信息：

```
[统计] 处理: 100, 转发到TUN: 200, 丢弃: 5
[TUN] 收到来自TUN的数据包，长度: 64字节
[Forwarder] 本地处理数据包，长度: 128字节
```

## 注意事项

1. **权限**: 必须以 root 权限运行
2. **网络配置**: 确保路由表正确配置
3. **性能**: DPDK 绑定的网卡无法被内核使用
4. **防火墙**: 注意 iptables 规则对 TUN 设备的影响

## 扩展开发

### 自定义过滤器

实现 `PacketFilter` 接口：

```go
type CustomFilter struct {
    name string
}

func (f *CustomFilter) ShouldProcess(packet []byte) bool {
    // 自定义过滤逻辑
    return true
}

func (f *CustomFilter) GetName() string {
    return f.name
}
```

### 自定义包处理

```go
tunDevice.SetPacketHandler(func(packet []byte) error {
    // 自定义TUN包处理逻辑
    return nil
})
```

这个系统为高性能网络应用提供了灵活的数据包分流和转发能力，特别适用于 VXLAN、VPN、网络代理等场景。
