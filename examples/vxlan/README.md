# VXLAN 示例 (DPDK 集成)

本示例演示如何使用 dpdknet 库的 VXLAN 功能，基于 DPDK 高性能数据路径。

## 核心特性

- **DPDK 集成**: 数据收发完全基于 DPDK，不使用内核网络栈
- **VXLAN 封装/解封装**: 使用 gopacket 库处理 VXLAN 头部
- **高性能**: 零拷贝数据路径，适合高频交易和高性能网络应用
- **VNI 隔离**: 支持虚拟网络标识符，实现多租户隔离

## 编译和运行

```bash
# 从项目根目录运行
cd examples/vxlan
go mod tidy
go build -o vxlan-example
sudo ./vxlan-example  # 需要 root 权限来访问 DPDK
```

## VXLAN 地址结构

```go
type VXLANAddr struct {
    IP   net.IP  // VTEP IP 地址
    Port int     // UDP 端口 (默认 4789)
    VNI  uint32  // 虚拟网络标识符
}
```

## 数据流程

1. **发送路径**: 
   - 应用数据 → VXLAN 封装 → DPDK 发送队列 → 物理网卡
   
2. **接收路径**: 
   - 物理网卡 → DPDK 接收队列 → VXLAN 解封装 → 应用数据

## 配置选项

通过 VXLANConfig 自定义：
- `VNI`: Virtual Network Identifier (默认 1000)
- `LocalIP/RemoteIP`: 本地和远程 VTEP IP 地址
- `UDPPort`: VXLAN UDP 端口 (默认 4789)
- `LocalMAC/RemoteMAC`: 内层以太网 MAC 地址

## 性能优化

1. **零拷贝**: 直接在 DPDK 内存池中处理数据
2. **批量处理**: 支持包批量发送和接收
3. **CPU 绑定**: 可绑定特定 CPU 核心避免上下文切换
4. **大页内存**: 使用大页内存减少 TLB miss

## 网络拓扑

```
应用层     App1 ←→ App2
           ↕       ↕
VXLAN层   VNI:1000 ↔ VNI:1000  
           ↕       ↕
传输层   VTEP1 ←─UDP─→ VTEP2
           ↕       ↕
物理层   NIC1 ←─Wire─→ NIC2
```

## 注意事项

1. **权限要求**: 需要 root 权限来初始化 DPDK
2. **硬件要求**: 需要支持 DPDK 的网卡 (Intel 82599, i40e 等)
3. **内存配置**: 需要预留大页内存给 DPDK
4. **VNI 一致性**: 通信双方必须使用相同的 VNI
5. **防火墙**: 确保 VXLAN 端口 (4789) 不被阻止

## 环境变量

- `DPDKNET_LOCAL_IP`: 设置本地 VTEP IP 地址
- `DPDK_*`: DPDK 相关配置参数
