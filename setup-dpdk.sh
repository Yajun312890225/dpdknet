#!/bin/bash

# DPDK 环境配置脚本
# 使用方法: sudo ./setup-dpdk.sh [网卡名称]
# 示例: sudo ./setup-dpdk.sh eth0

set -e

echo "=========================================="
echo "         DPDK 环境配置脚本"
echo "=========================================="

# 检查是否为root权限
if [[ $EUID -ne 0 ]]; then
   echo "错误: 此脚本需要root权限运行"
   echo "请使用: sudo $0"
   exit 1
fi

# 1. 安装依赖
echo "1. 安装编译依赖..."
apt update
apt install -y build-essential meson ninja-build pkg-config libnuma-dev wget

# 2. 安装Go环境 (如果需要)
if ! command -v go &> /dev/null || ! go version | grep -q "go1.1[9-9]\|go1.[2-9][0-9]"; then
    echo "2. 安装 Go 1.19..."
    cd /tmp
    wget -q https://go.dev/dl/go1.19.13.linux-amd64.tar.gz
    tar -C /usr/local -xzf go1.19.13.linux-amd64.tar.gz
    export PATH=/usr/local/go/bin:$PATH
    echo 'export PATH=/usr/local/go/bin:$PATH' >> /etc/profile
    echo "Go 版本: $(go version)"
else
    echo "2. Go 环境已就绪: $(go version)"
fi

# 3. 编译安装DPDK
echo "3. 下载并编译 DPDK 19.11.14..."
cd /tmp
if [ ! -d "dpdk-19.11.14" ]; then
    wget -q http://fast.dpdk.org/rel/dpdk-19.11.14.tar.xz
    tar xf dpdk-19.11.14.tar.xz
fi
cd dpdk-19.11.14

if [ ! -d "build" ]; then
    meson build
fi
cd build
ninja
ninja install
ldconfig

echo "4. 验证DPDK安装..."
if pkg-config --exists libdpdk; then
    echo "✅ DPDK 安装成功"
    echo "   版本: $(pkg-config --modversion libdpdk)"
else
    echo "❌ DPDK 安装失败"
    exit 1
fi

# 5. 配置hugepages
echo "5. 配置 Hugepages..."
# 配置2MB hugepages (1GB)
echo 1024 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

# 创建挂载点并挂载
mkdir -p /mnt/huge
if ! mountpoint -q /mnt/huge; then
    mount -t hugetlbfs nodev /mnt/huge
fi

echo "   Hugepages 配置:"
cat /proc/meminfo | grep -i huge

# 6. 查找网卡PCI地址
echo "6. 查找网络接口..."
echo "   可用网络接口:"
ip link show | grep -E "^[0-9]+:" | awk '{print $2}' | sed 's/://g'

echo ""
echo "   网卡PCI地址信息:"
for iface in $(ip link show | grep -E "^[0-9]+:" | awk '{print $2}' | sed 's/://g'); do
    if [ -e "/sys/class/net/$iface/device" ]; then
        pci_addr=$(basename $(readlink -f /sys/class/net/$iface/device))
        driver=$(basename $(readlink -f /sys/class/net/$iface/device/driver) 2>/dev/null || echo "未知")
        echo "   $iface -> $pci_addr (driver: $driver)"
    fi
done

# 7. 绑定网卡 (如果指定了网卡名称)
INTERFACE=$1
if [ ! -z "$INTERFACE" ]; then
    echo "7. 绑定网卡 $INTERFACE 到DPDK..."
    
    # 获取PCI地址
    if [ ! -e "/sys/class/net/$INTERFACE/device" ]; then
        echo "❌ 网络接口 $INTERFACE 不存在"
        exit 1
    fi
    
    PCI_ADDR=$(basename $(readlink -f /sys/class/net/$INTERFACE/device))
    echo "   网卡 $INTERFACE 的PCI地址: $PCI_ADDR"
    
    # 加载UIO驱动
    modprobe uio_pci_generic || {
        echo "❌ 无法加载 uio_pci_generic 驱动"
        exit 1
    }
    
    # 查找dpdk-devbind.py
    DEVBIND_SCRIPT=""
    for path in "/usr/local/bin/dpdk-devbind.py" "/usr/bin/dpdk-devbind.py" "/tmp/dpdk-19.11.14/usertools/dpdk-devbind.py"; do
        if [ -f "$path" ]; then
            DEVBIND_SCRIPT="$path"
            break
        fi
    done
    
    if [ -z "$DEVBIND_SCRIPT" ]; then
        echo "❌ 找不到 dpdk-devbind.py 脚本"
        exit 1
    fi
    
    echo "   使用脚本: $DEVBIND_SCRIPT"
    
    # 绑定网卡
    python3 $DEVBIND_SCRIPT -b uio_pci_generic $PCI_ADDR
    
    echo "   绑定状态:"
    python3 $DEVBIND_SCRIPT --status
else
    echo "7. 跳过网卡绑定 (未指定网卡名称)"
    echo "   如需绑定网卡，请使用: sudo $0 <网卡名称>"
fi

# 8. 设置环境变量
echo "8. 配置环境变量..."
cat > /tmp/dpdk-env.sh << 'EOF'
# DPDK 环境变量
export CGO_CFLAGS="$(pkg-config --cflags libdpdk)"
export CGO_LDFLAGS="$(pkg-config --libs libdpdk)"
export PATH=/usr/local/go/bin:$PATH
EOF

echo "   环境变量脚本已创建: /tmp/dpdk-env.sh"
echo "   执行以下命令以加载环境变量:"
echo "   source /tmp/dpdk-env.sh"

# 9. 完成
echo ""
echo "=========================================="
echo "         DPDK 环境配置完成!"
echo "=========================================="
echo ""
echo "下一步操作:"
echo "1. 加载环境变量: source /tmp/dpdk-env.sh"
echo "2. 编译项目: CGO_LDFLAGS_ALLOW='-Wl,.*' go build"
if [ -z "$INTERFACE" ]; then
    echo "3. 绑定网卡: sudo $0 <网卡名称>"
fi
echo ""
echo "验证命令:"
echo "- 检查hugepages: cat /proc/meminfo | grep -i huge"
echo "- 检查DPDK: pkg-config --modversion libdpdk"
echo "- 检查网卡绑定: python3 $DEVBIND_SCRIPT --status"
