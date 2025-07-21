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

# 获取DPDK源码目录（可选，用于搜索库文件）
DPDK_SRC_DIR="/tmp/dpdk-stable-19.11.14"

echo "1. 验证驱动库文件..."

# 检查库文件路径
dpdk_lib_path=$(pkg-config --variable=libdir libdpdk 2>/dev/null || echo "/usr/local/lib/x86_64-linux-gnu")
echo "   DPDK库目录: $dpdk_lib_path"

# 列出所有网卡驱动库
echo "   已安装的网卡驱动库:"
find "$dpdk_lib_path" -name "librte_net_*.a" 2>/dev/null | sort | while read lib; do
    echo "     ✅ $(basename $lib)"
done

# 特别检查VMXNet3驱动
vmxnet3_lib_found=false
if [ -f "$dpdk_lib_path/librte_net_vmxnet3.a" ]; then
    echo "   ✅ VMXNet3驱动已安装: librte_net_vmxnet3.a"
    vmxnet3_lib_found=true
else
    echo "   ❌ VMXNet3驱动仍然缺失"
    echo "      这可能是因为您的系统不支持该驱动或编译时出现问题"
fi

# 检查并创建 librte_pmd_vmxnet3_uio.a (用于兼容旧版本链接器需求)
echo "2. 处理 VMXNet3 UIO 驱动兼容性..."
if [ "$vmxnet3_lib_found" = true ]; then
    # 如果存在 librte_net_vmxnet3.a，创建 librte_pmd_vmxnet3_uio.a 的符号链接或拷贝
    if [ ! -f "/usr/local/lib/librte_pmd_vmxnet3_uio.a" ]; then
        echo "   创建 VMXNet3 UIO 驱动文件..."
        cp "$dpdk_lib_path/librte_net_vmxnet3.a" "/usr/local/lib/librte_pmd_vmxnet3_uio.a"
        echo "   ✅ 已拷贝: librte_net_vmxnet3.a -> /usr/local/lib/librte_pmd_vmxnet3_uio.a"
    else
        echo "   ✅ librte_pmd_vmxnet3_uio.a 已存在"
    fi
else
    # 尝试从构建目录和其他可能位置查找
    echo "   尝试从已安装目录查找 VMXNet3 驱动..."
    search_paths=(
        "$DPDK_SRC_DIR/build/drivers/net/vmxnet3"
        "$DPDK_SRC_DIR/build/lib"
        "$DPDK_SRC_DIR/build"
        "/usr/local/lib/x86_64-linux-gnu"
        "/usr/lib/x86_64-linux-gnu"
        "/usr/local/lib"
        "/usr/lib"
    )
    
    found_vmxnet3=false
    for search_path in "${search_paths[@]}"; do
        if [ -d "$search_path" ]; then
            vmxnet3_files=$(find "$search_path" -name "*vmxnet3*.a" -type f 2>/dev/null || true)
            if [ -n "$vmxnet3_files" ]; then
                echo "   找到VMXNet3库文件:"
                echo "$vmxnet3_files" | while read file; do
                    echo "     - $file"
                    if [ ! -f "/usr/local/lib/librte_pmd_vmxnet3_uio.a" ]; then
                        cp "$file" "/usr/local/lib/librte_pmd_vmxnet3_uio.a"
                        echo "   ✅ 已拷贝: $(basename $file) -> /usr/local/lib/librte_pmd_vmxnet3_uio.a"
                        found_vmxnet3=true
                    fi
                done
                if [ "$found_vmxnet3" = true ]; then
                    break
                fi
            fi
        fi
    done
    
    if [ "$found_vmxnet3" = false ]; then
        echo "   ⚠️  未找到VMXNet3相关库文件"
        echo "      可能需要先运行: sudo ./setup-dpdk.sh --compile-only"
    fi
fi

# 确保 /usr/local/lib 在库搜索路径中
if [ ! -f "/usr/local/lib/librte_pmd_vmxnet3_uio.a" ]; then
    echo "   ⚠️  仍然无法找到或创建 librte_pmd_vmxnet3_uio.a"
    echo "      可能需要手动编译或您的系统不支持VMXNet3"
fi

# 检查UIO相关库 (注意: 新版本DPDK中UIO库命名可能不同)
echo "3. 检查UIO相关库:"
find "$dpdk_lib_path" -name "*uio*" 2>/dev/null | while read lib; do
    echo "     ✅ $(basename $lib)"
done
find "/usr/local/lib" -name "*uio*" 2>/dev/null | while read lib; do
    echo "     ✅ $(basename $lib) (在 /usr/local/lib)"
done
if [ ! -f "$dpdk_lib_path/librte_pmd_vmxnet3_uio.a" ] && [ ! -f "/usr/local/lib/librte_pmd_vmxnet3_uio.a" ]; then
    echo "     ℹ️  未找到UIO相关库文件"
fi

echo "4. 更新环境变量..."
cat > /tmp/dpdk-env.sh << 'EOF'
# DPDK 环境变量
export CGO_CFLAGS="$(pkg-config --cflags libdpdk)"
export CGO_LDFLAGS="$(pkg-config --libs libdpdk) -L/usr/local/lib"
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
echo "- /usr/local/lib/librte_pmd_vmxnet3_uio.a 是否存在"
echo "- 是否需要先运行: sudo ./setup-dpdk.sh --compile-only"
echo "- 系统是否支持VMXNet3网卡 (仅VMware虚拟机需要)"
