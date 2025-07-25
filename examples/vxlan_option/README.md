# VXLAN 作为选项的新架构

这个示例展示了如何将 VXLAN 作为 TCP/UDP 的一个选项来使用，而不是作为独立的网络协议。

## 架构对比

### 原来的架构 (独立的 VXLAN 协议)
```go
// 独立的 VXLAN 网络协议
listener, err := dpdknet.Listen("vxlan", "192.168.1.10:4789")
conn, err := dpdknet.Dial("vxlan", "192.168.1.20:4789")
```

### 新的架构 (VXLAN 作为选项)
```go
// TCP/UDP 的 VXLAN 选项 - Option 风格
vxlanConfig := &dpdknet.VXLANConfig{
    VNI:      1000,
    LocalIP:  net.ParseIP("192.168.1.100"),
    RemoteIP: net.ParseIP("192.168.1.200"),
    UDPPort:  4789,
}

// 使用 WithVXLAN Option
listener, err := dpdknet.Listen("tcp", ":8080", dpdknet.WithVXLAN(vxlanConfig))
conn, err := dpdknet.Dial("tcp", "192.168.1.10:8080", dpdknet.WithVXLAN(vxlanConfig))

// 或使用辅助方法
listener, err := dpdknet.ListenWithVXLAN("tcp", ":8080", vxlanConfig)
conn, err := dpdknet.DialWithVXLAN("tcp", "192.168.1.10:8080", vxlanConfig)
```

## 新架构的优势

1. **统一接口**: TCP/UDP 可以透明地启用 VXLAN 封装
2. **简化应用层**: 应用代码不需要知道 VXLAN 的存在
3. **灵活配置**: 可以为不同的端口配置不同的 VXLAN 参数
4. **协议一致性**: 与标准的 net 包接口保持一致

## 数据流程

### 发送数据 (应用 → 网络)
```
应用层数据 → TCP/UDP 封装 → gVisor 协议栈 → 检查端口 VXLAN 配置 → VXLAN 封装 → DPDK 发送
```

### 接收数据 (网络 → 应用)  
```
DPDK 接收 → 检测 VXLAN 包 → VXLAN 解封装 → gVisor 协议栈 → TCP/UDP 处理 → 应用层数据
```

## 配置说明

- `VNI`: VXLAN Network Identifier，用于区分不同的虚拟网络
- `LocalIP/RemoteIP`: VTEP (VXLAN Tunnel Endpoint) 的 IP 地址
- `LocalMAC/RemoteMAC`: VTEP 的 MAC 地址
- `UDPPort`: VXLAN 封装使用的 UDP 端口 (默认 4789)

## 使用场景

1. **云原生网络**: 容器间的网络隔离
2. **数据中心互联**: 跨数据中心的二层网络扩展
3. **多租户环境**: 不同租户的网络隔离
4. **网络虚拟化**: 在物理网络上构建虚拟网络

## 注意事项

- 确保 DPDK 环境正确配置
- VTEP IP 地址需要在物理网络中可达
- VNI 需要在通信双方保持一致
- MAC 地址配置需要与实际网络环境匹配
