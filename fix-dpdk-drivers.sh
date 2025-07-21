#!/bin/bash

# DPDK 驱动修复脚本
# 用于解决 librte_pmd_vmxnet3_uio.a 等驱动库缺失问题
# 使用方法: sudo ./fix-dpdk-drivers.sh

set -e

echo "=========================================="
echo "      DPDK 驱动库修复脚本"
echo "=========================================="

# 检查是否为root权限
if [[ $EUID -ne 0 ]]; then
   echo "错误: 此脚本需要root权限运行"
   echo "请使用: sudo $0"
   exit 1
fi

# 检查DPDK是否已安装
if ! pkg-config --exists libdpdk; then
    echo "❌ DPDK 未安装或配置不正确"
    echo "   请先运行 setup-dpdk.sh 安装DPDK"
    exit 1
fi

echo "当前DPDK版本: $(pkg-config --modversion libdpdk)"

# 获取DPDK源码目录
DPDK_SRC_DIR="/tmp/dpdk-stable-19.11.14"
if [ ! -d "$DPDK_SRC_DIR" ]; then
    echo "❌ DPDK源码目录不存在: $DPDK_SRC_DIR"
    echo "   请先运行 setup-dpdk.sh 下载DPDK源码"
    exit 1
fi

cd "$DPDK_SRC_DIR"

echo "1. 重新配置DPDK构建以启用所有驱动..."

# 删除旧的构建目录
if [ -d "build" ]; then
    echo "   删除旧的构建配置..."
    rm -rf build
fi

# 重新配置，显式启用所有网卡驱动
echo "   配置meson构建..."
meson build \
    -Denable_drivers=net/vmxnet3,net/e1000,net/ixgbe,net/i40e,net/mlx4,net/mlx5,net/af_packet,net/bonding,net/failsafe,net/kni,net/null,net/pcap,net/ring,net/tap,net/vhost \
    -Ddisable_drivers= \
    -Dmax_numa_nodes=8 \
    -Dmax_ethports=32

cd build

echo "2. 重新编译DPDK..."
ninja clean || true
ninja

echo "3. 重新安装DPDK..."
ninja install
ldconfig

echo "4. 验证驱动库文件..."

# 检查库文件路径
dpdk_lib_path=$(pkg-config --variable=libdir libdpdk 2>/dev/null || echo "/usr/local/lib/x86_64-linux-gnu")
echo "   DPDK库目录: $dpdk_lib_path"

# 列出所有网卡驱动库
echo "   已安装的网卡驱动库:"
find "$dpdk_lib_path" -name "librte_net_*.a" 2>/dev/null | sort | while read lib; do
    echo "     ✅ $(basename $lib)"
done

# 特别检查VMXNet3驱动
if [ -f "$dpdk_lib_path/librte_net_vmxnet3.a" ]; then
    echo "   ✅ VMXNet3驱动已安装: librte_net_vmxnet3.a"
else
    echo "   ❌ VMXNet3驱动仍然缺失"
    echo "      这可能是因为您的系统不支持该驱动或编译时出现问题"
fi

# 检查UIO相关库 (注意: 新版本DPDK中UIO库命名可能不同)
echo "   检查UIO相关库:"
find "$dpdk_lib_path" -name "*uio*" 2>/dev/null | while read lib; do
    echo "     ✅ $(basename $lib)"
done || echo "     ℹ️  未找到UIO相关库文件"

echo "5. 更新环境变量..."
cat > /tmp/dpdk-env.sh << 'EOF'
# DPDK 环境变量
export CGO_CFLAGS="$(pkg-config --cflags libdpdk)"
export CGO_LDFLAGS="$(pkg-config --libs libdpdk)"
export PATH=/usr/local/go/bin:$PATH
EOF

echo ""
echo "=========================================="
echo "        DPDK 驱动修复完成!"
echo "=========================================="
echo ""
echo "下一步:"
echo "1. 加载环境变量: source /tmp/dpdk-env.sh"
echo "2. 重新编译项目: CGO_LDFLAGS_ALLOW='-Wl,.*' go build"
echo ""
echo "如果仍有问题，请检查:"
echo "- 系统是否支持VMXNet3网卡"
echo "- 是否在VMware虚拟机中运行"
echo "- 编译日志是否有错误信息"
