#!/bin/bash

# DPDK 环境检查脚本
# 使用方法: ./check-dpdk-env.sh

echo "=========================================="
echo "        DPDK 环境检查脚本"
echo "=========================================="

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查函数
check_command() {
    if command -v $1 &> /dev/null; then
        echo -e "${GREEN}✅${NC} $1: 已安装"
        if [ "$1" = "go" ]; then
            echo "   版本: $(go version)"
        fi
    else
        echo -e "${RED}❌${NC} $1: 未安装"
        return 1
    fi
}

check_file() {
    if [ -f "$1" ]; then
        echo -e "${GREEN}✅${NC} 文件存在: $1"
    else
        echo -e "${RED}❌${NC} 文件不存在: $1"
        return 1
    fi
}

echo "1. 检查基础工具..."
check_command gcc
check_command ninja
check_command meson
check_command pkg-config

echo ""
echo "2. 检查 Go 环境..."
if check_command go; then
    # 检查Go版本是否 >= 1.19
    go_version=$(go version | grep -o 'go[0-9]\+\.[0-9]\+' | sed 's/go//')
    major=$(echo $go_version | cut -d. -f1)
    minor=$(echo $go_version | cut -d. -f2)
    
    if [ "$major" -gt 1 ] || ([ "$major" -eq 1 ] && [ "$minor" -ge 19 ]); then
        echo -e "   ${GREEN}✅${NC} Go 版本满足要求 (>= 1.19)"
    else
        echo -e "   ${YELLOW}⚠️${NC} Go 版本过低，建议升级到 1.19+"
    fi
fi

echo ""
echo "3. 检查 DPDK 安装..."
if pkg-config --exists libdpdk; then
    echo -e "${GREEN}✅${NC} DPDK: 已安装"
    echo "   版本: $(pkg-config --modversion libdpdk)"
    echo "   CFLAGS: $(pkg-config --cflags libdpdk | head -c 50)..."
    echo "   LDFLAGS: $(pkg-config --libs libdpdk | head -c 50)..."
else
    echo -e "${RED}❌${NC} DPDK: 未安装或pkg-config配置错误"
fi

echo ""
echo "4. 检查环境变量..."
if [ -n "$CGO_CFLAGS" ]; then
    echo -e "${GREEN}✅${NC} CGO_CFLAGS: 已设置"
    echo "   值: ${CGO_CFLAGS:0:50}..."
else
    echo -e "${YELLOW}⚠️${NC} CGO_CFLAGS: 未设置"
    echo "   建议执行: export CGO_CFLAGS=\"\$(pkg-config --cflags libdpdk)\""
fi

if [ -n "$CGO_LDFLAGS" ]; then
    echo -e "${GREEN}✅${NC} CGO_LDFLAGS: 已设置"
    echo "   值: ${CGO_LDFLAGS:0:50}..."
else
    echo -e "${YELLOW}⚠️${NC} CGO_LDFLAGS: 未设置"
    echo "   建议执行: export CGO_LDFLAGS=\"\$(pkg-config --libs libdpdk)\""
fi

echo ""
echo "5. 检查 Hugepages..."
hugepages_2m=$(grep HugePages_Total /proc/meminfo | grep -o '[0-9]\+')
hugepages_1g=$(grep Hugepagesize /proc/meminfo | grep -o '[0-9]\+')

if [ "$hugepages_2m" -gt 0 ]; then
    echo -e "${GREEN}✅${NC} Hugepages: 已配置"
    echo "   2MB 页面数量: $hugepages_2m"
    echo "   页面大小: ${hugepages_1g}kB"
    
    # 检查是否有足够的hugepages (至少512MB)
    total_mb=$((hugepages_2m * hugepages_1g / 1024))
    if [ "$total_mb" -ge 512 ]; then
        echo -e "   ${GREEN}✅${NC} 内存充足: ${total_mb}MB"
    else
        echo -e "   ${YELLOW}⚠️${NC} 内存不足: ${total_mb}MB (建议至少512MB)"
    fi
else
    echo -e "${RED}❌${NC} Hugepages: 未配置"
    echo "   建议执行: echo 1024 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages"
fi

echo ""
echo "6. 检查网络接口..."
echo "   可用网络接口:"
for iface in $(ip link show | grep -E "^[0-9]+:" | awk '{print $2}' | sed 's/://g' | grep -v lo); do
    if [ -e "/sys/class/net/$iface/device" ]; then
        pci=$(basename $(readlink -f /sys/class/net/$iface/device) 2>/dev/null || echo "未知")
        driver=$(basename $(readlink -f /sys/class/net/$iface/device/driver) 2>/dev/null || echo "未知")
        state=$(cat /sys/class/net/$iface/operstate 2>/dev/null || echo "未知")
        echo "   - $iface: $pci (driver: $driver, state: $state)"
    fi
done

echo ""
echo "7. 检查内核模块..."
if lsmod | grep -q uio_pci_generic; then
    echo -e "${GREEN}✅${NC} uio_pci_generic: 已加载"
else
    echo -e "${YELLOW}⚠️${NC} uio_pci_generic: 未加载"
    echo "   可执行: sudo modprobe uio_pci_generic"
fi

if lsmod | grep -q vfio_pci; then
    echo -e "${GREEN}✅${NC} vfio_pci: 已加载"
else
    echo -e "${YELLOW}⚠️${NC} vfio_pci: 未加载"
    echo "   可执行: sudo modprobe vfio_pci"
fi

echo ""
echo "8. 检查 DPDK 绑定工具..."
devbind_paths=(
    "/usr/local/bin/dpdk-devbind.py"
    "/usr/bin/dpdk-devbind.py"
    "./dpdk-devbind.py"
    "/tmp/dpdk-stable-19.11.14/usertools/dpdk-devbind.py"
    "/opt/dpdk/usertools/dpdk-devbind.py"
)

found=false
for path in "${devbind_paths[@]}"; do
    if [ -f "$path" ]; then
        echo -e "${GREEN}✅${NC} dpdk-devbind.py: $path"
        found=true
        
        # 尝试显示绑定状态
        if [ -r "$path" ]; then
            echo "   DPDK 绑定状态:"
            python3 "$path" --status 2>/dev/null | head -10 || echo "   无法获取绑定状态 (可能需要root权限)"
        fi
        break
    fi
done

if [ "$found" = false ]; then
    echo -e "${RED}❌${NC} dpdk-devbind.py: 未找到"
    echo "   请检查 DPDK 安装或手动指定路径"
fi

echo ""
echo "=========================================="
echo "           环境检查完成"
echo "=========================================="

echo ""
echo "建议执行的命令:"
if ! pkg-config --exists libdpdk; then
    echo "- 安装DPDK: sudo ./setup-dpdk.sh"
fi

if [ -z "$CGO_CFLAGS" ] || [ -z "$CGO_LDFLAGS" ]; then
    echo "- 设置环境变量: source /tmp/dpdk-env.sh"
fi

if [ "$hugepages_2m" -eq 0 ]; then
    echo "- 配置hugepages: echo 1024 | sudo tee /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages"
fi

echo "- 编译项目: CGO_LDFLAGS_ALLOW='-Wl,.*' go build"
