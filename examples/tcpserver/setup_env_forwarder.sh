#!/bin/bash

# 环境变量控制的数据包转发器演示脚本

echo "=== DPDK数据包转发器环境变量演示 ==="
echo

# 检查权限
if [ "$EUID" -ne 0 ]; then
    echo "错误: 此脚本需要root权限运行"
    echo "请使用: sudo $0"
    exit 1
fi

echo "配置环境变量..."

# 启用数据包转发器
export ENABLE_PACKET_FORWARDER=true
echo "✓ ENABLE_PACKET_FORWARDER=true"

# 配置网桥
export FORWARDER_BRIDGE_NAME=br1
echo "✓ FORWARDER_BRIDGE_NAME=br1"

# 配置目标IP
export FORWARDER_TARGET_IPS="10.10.10.4,192.168.66.57"
echo "✓ FORWARDER_TARGET_IPS=10.10.10.4,192.168.66.57"

# 配置目标网络
export FORWARDER_TARGET_NETWORKS="10.10.10.0/24,192.168.66.0/24"
echo "✓ FORWARDER_TARGET_NETWORKS=10.10.10.0/24,192.168.66.0/24"

# 配置目标端口
export FORWARDER_TARGET_PORTS="4789,22,80,443,8080,32333"
echo "✓ FORWARDER_TARGET_PORTS=4789,22,80,443,8080,32333"

echo
echo "检查网桥状态..."
if ! ip link show br1 &> /dev/null; then
    echo "创建网桥 br1..."
    brctl addbr br1
    ip link set br1 up
    echo "✓ 网桥 br1 创建成功"
else
    echo "✓ 网桥 br1 已存在"
fi

echo
echo "环境变量配置完成！"
echo
echo "当前配置:"
echo "  启用转发器: $ENABLE_PACKET_FORWARDER"
echo "  网桥名称: $FORWARDER_BRIDGE_NAME"
echo "  目标IP: $FORWARDER_TARGET_IPS"
echo "  目标网络: $FORWARDER_TARGET_NETWORKS"
echo "  目标端口: $FORWARDER_TARGET_PORTS"
echo
echo "数据包处理流程:"
echo "  1. DPDK接收数据包"
echo "  2. globalPacketHandler处理"
echo "  3. 如果启用转发器，自动创建TAP设备："
echo "     - tap-normal: 处理普通数据包"
echo "     - tap-vxlan: 处理VXLAN数据包"
echo "  4. 根据过滤器决定本地处理还是TAP转发"
echo "  5. TAP设备绑定到网桥: $FORWARDER_BRIDGE_NAME"
echo
echo "现在可以运行您的DPDK程序，转发器会自动启动！"
echo
echo "示例程序运行："
echo "  cd examples/forwarder && go run main.go"
echo
echo "或者直接调用 dpdknet.InitializeGlobalNetwork()"
