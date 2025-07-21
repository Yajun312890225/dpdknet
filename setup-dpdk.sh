#!/bin/bash

# DPDK 环境配置脚本
# 使用方法: sudo ./setup-dpdk.sh [网卡名称] [选项]
# 示例: 
#   sudo ./setup-dpdk.sh eth0           # 完整安装并绑定网卡
#   sudo ./setup-dpdk.sh --compile-only # 仅安装编译环境
# 选项: 
#   --skip-go      跳过Go安装
#   --compile-only 仅安装编译环境，跳过网卡绑定

set -e

echo "=========================================="
echo "         DPDK 环境配置脚本"
echo "=========================================="

# 解析命令行参数
INTERFACE=""
SKIP_GO=false
COMPILE_ONLY=false

for arg in "$@"; do
    case $arg in
        --skip-go)
            SKIP_GO=true
            ;;
        --compile-only)
            COMPILE_ONLY=true
            ;;
        --help|-h)
            echo "用法: sudo $0 [网卡名称] [选项]"
            echo ""
            echo "参数:"
            echo "  网卡名称     要绑定到DPDK的网卡 (如: eth0)"
            echo ""
            echo "选项:"
            echo "  --skip-go      跳过Go环境安装"
            echo "  --compile-only 仅安装编译环境，跳过网卡绑定和hugepages配置"
            echo "  --help, -h     显示此帮助信息"
            echo ""
            echo "示例:"
            echo "  sudo $0 eth0              # 完整安装并绑定eth0"
            echo "  sudo $0 --skip-go        # 只安装DPDK,跳过Go"
            echo "  sudo $0 --compile-only   # 仅安装编译环境"
            echo "  sudo $0 eth0 --skip-go   # 跳过Go,绑定eth0"
            exit 0
            ;;
        -*)
            echo "未知选项: $arg"
            echo "使用 --help 查看帮助"
            exit 1
            ;;
        *)
            if [ -z "$INTERFACE" ] && [ "$COMPILE_ONLY" = false ]; then
                INTERFACE="$arg"
            elif [ "$COMPILE_ONLY" = false ]; then
                echo "错误: 只能指定一个网卡名称"
                exit 1
            fi
            ;;
    esac
done

# 如果启用了 compile-only 模式，清空接口参数
if [ "$COMPILE_ONLY" = true ]; then
    INTERFACE=""
fi

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
if [ "$SKIP_GO" = true ]; then
    echo "2. 跳过 Go 安装 (--skip-go 选项)"
    if command -v go &> /dev/null; then
        echo "   当前 Go 版本: $(go version)"
    else
        echo "   ⚠️ 警告: 系统中未找到 Go 环境"
        echo "   请手动安装 Go 1.19+ 或重新运行脚本不使用 --skip-go"
    fi
elif ! command -v go &> /dev/null || ! go version | grep -q "go1.1[9-9]\|go1.[2-9][0-9]"; then
    echo "2. 安装 Go 1.19..."
    cd /tmp
    
    # 检查是否已经下载
    if [ ! -f "go1.19.13.linux-amd64.tar.gz" ]; then
        echo "   正在下载 Go 1.19.13 (约150MB)..."
        
        # 尝试多个下载源
        download_success=false
        
        # 源1: 官方源
        echo "   尝试官方源..."
        if wget --timeout=30 --tries=2 --progress=bar \
           https://go.dev/dl/go1.19.13.linux-amd64.tar.gz; then
            download_success=true
        fi
        
        # 源2: 中国镜像源 (如果官方源失败)
        if [ "$download_success" = false ]; then
            echo "   官方源失败，尝试中国镜像源..."
            if wget --timeout=30 --tries=2 --progress=bar \
               https://studygolang.com/dl/golang/go1.19.13.linux-amd64.tar.gz; then
                download_success=true
            fi
        fi
        
        # 源3: 阿里镜像源
        if [ "$download_success" = false ]; then
            echo "   尝试阿里镜像源..."
            if wget --timeout=30 --tries=2 --progress=bar \
               https://mirrors.aliyun.com/golang/go1.19.13.linux-amd64.tar.gz; then
                download_success=true
            fi
        fi
        
        if [ "$download_success" = false ]; then
            echo "❌ 所有下载源都失败了"
            echo "   请检查网络连接或手动下载:"
            echo "   wget https://go.dev/dl/go1.19.13.linux-amd64.tar.gz"
            echo "   或使用 --skip-go 选项跳过Go安装"
            exit 1
        fi
    else
        echo "   Go 安装包已存在，跳过下载"
    fi
    
    # 验证文件完整性
    echo "   验证下载文件..."
    if [ ! -s "go1.19.13.linux-amd64.tar.gz" ]; then
        echo "❌ 下载的文件损坏或为空"
        rm -f go1.19.13.linux-amd64.tar.gz
        exit 1
    fi
    
    # 安装 Go
    echo "   正在安装 Go..."
    if [ -d "/usr/local/go" ]; then
        echo "   删除旧版本..."
        rm -rf /usr/local/go
    fi
    
    tar -C /usr/local -xzf go1.19.13.linux-amd64.tar.gz
    export PATH=/usr/local/go/bin:$PATH
    echo 'export PATH=/usr/local/go/bin:$PATH' >> /etc/profile
    
    # 验证安装
    if /usr/local/go/bin/go version; then
        echo "✅ Go 安装成功"
        echo "   版本: $(/usr/local/go/bin/go version)"
    else
        echo "❌ Go 安装失败"
        exit 1
    fi
else
    echo "2. Go 环境已就绪: $(go version)"
fi

# 3. 编译安装DPDK
echo "3. 下载并编译 DPDK 19.11.14..."
cd /tmp
if [ ! -d "dpdk-stable-19.11.14" ]; then
    if [ ! -f "dpdk-19.11.14.tar.xz" ]; then
        echo "   正在下载 DPDK 19.11.14 (约20MB)..."
        
        # 尝试多个下载源
        download_success=false
        
        # 源1: 官方源
        echo "   尝试官方源..."
        if wget --timeout=60 --tries=2 --progress=bar \
           http://fast.dpdk.org/rel/dpdk-19.11.14.tar.xz; then
            download_success=true
        fi
        
        # 源2: 备份源
        if [ "$download_success" = false ]; then
            echo "   官方源失败，尝试备份源..."
            if wget --timeout=60 --tries=2 --progress=bar \
               https://github.com/DPDK/dpdk/archive/v19.11.14.tar.gz -O dpdk-19.11.14.tar.gz; then
                # GitHub 源下载的是 .tar.gz 格式，需要重命名
                mv dpdk-19.11.14.tar.gz dpdk-19.11.14.tar.xz 2>/dev/null || true
                download_success=true
            fi
        fi
        
        if [ "$download_success" = false ]; then
            echo "❌ DPDK 下载失败"
            echo "   请检查网络连接或手动下载:"
            echo "   wget http://fast.dpdk.org/rel/dpdk-19.11.14.tar.xz"
            exit 1
        fi
    else
        echo "   DPDK 源码包已存在，跳过下载"
    fi
    
    echo "   正在解压 DPDK..."
    if file dpdk-19.11.14.tar.xz | grep -q "gzip"; then
        # GitHub 源下载的实际是 gzip 格式
        tar -xzf dpdk-19.11.14.tar.xz
        # GitHub源解压后目录名是 dpdk-19.11.14
        if [ -d "dpdk-19.11.14" ]; then
            mv dpdk-19.11.14 dpdk-stable-19.11.14
        fi
    else
        # 正常的 xz 格式，解压后目录名是 dpdk-stable-19.11.14
        tar -xf dpdk-19.11.14.tar.xz
    fi
    
    # 验证目录是否存在
    if [ ! -d "dpdk-stable-19.11.14" ]; then
        echo "❌ DPDK 解压失败，未找到目录 dpdk-stable-19.11.14"
        echo "   当前目录内容:"
        ls -la | grep dpdk || echo "   无DPDK相关目录"
        exit 1
    fi
fi
cd dpdk-stable-19.11.14

echo "   正在配置 DPDK 构建..."
if [ ! -d "build" ]; then
    # 配置 meson 构建，确保启用所有网卡驱动
    meson build 
fi
cd build

echo "   正在编译 DPDK (可能需要几分钟)..."
ninja
echo "   正在安装 DPDK..."
ninja install
ldconfig

echo "4. 验证DPDK安装..."
if pkg-config --exists libdpdk; then
    echo "✅ DPDK 安装成功"
    echo "   版本: $(pkg-config --modversion libdpdk)"
    
    # 检查关键驱动库文件
    echo "   检查驱动库文件..."
    dpdk_lib_path=$(pkg-config --variable=libdir libdpdk 2>/dev/null || echo "/usr/local/lib/x86_64-linux-gnu")
    
    # 检查常用网卡驱动库
    missing_libs=()
    for lib in "librte_net_vmxnet3.a" "librte_net_e1000.a" "librte_net_ixgbe.a"; do
        if [ ! -f "$dpdk_lib_path/$lib" ] && [ ! -f "/usr/local/lib/$lib" ] && [ ! -f "/usr/lib/x86_64-linux-gnu/$lib" ]; then
            missing_libs+=("$lib")
        fi
    done
    
    if [ ${#missing_libs[@]} -eq 0 ]; then
        echo "   ✅ 所有驱动库文件已安装"
    else
        echo "   ⚠️  以下驱动库文件缺失:"
        for lib in "${missing_libs[@]}"; do
            echo "     - $lib"
        done
        echo "   这可能影响特定网卡的使用，但核心功能仍然可用"
    fi
else
    echo "❌ DPDK 安装失败"
    exit 1
fi

# 5. 配置hugepages
if [ "$COMPILE_ONLY" = true ]; then
    echo "5. 跳过 Hugepages 配置 (--compile-only 模式)"
    echo "   编译模式下不需要配置hugepages"
    echo "   如需运行程序，请稍后手动配置或重新运行脚本"
else
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
fi

# 6. 查找网卡PCI地址
if [ "$COMPILE_ONLY" = true ]; then
    echo "6. 跳过网卡检测 (--compile-only 模式)"
    echo "   编译模式下不检测网卡信息"
else
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
fi

# 7. 绑定网卡 (如果指定了网卡名称)
if [ "$COMPILE_ONLY" = true ]; then
    echo "7. 跳过网卡绑定 (--compile-only 模式)"
    echo "   编译模式下不绑定网卡到DPDK"
elif [ ! -z "$INTERFACE" ]; then
    echo "7. 绑定网卡 $INTERFACE 到DPDK..."
    
    # 获取PCI地址
    if [ ! -e "/sys/class/net/$INTERFACE/device" ]; then
        echo "❌ 网络接口 $INTERFACE 不存在"
        echo "   可用接口:"
        ip link show | grep -E "^[0-9]+:" | awk '{print "     " $2}' | sed 's/://g'
        exit 1
    fi
    
    PCI_ADDR=$(basename $(readlink -f /sys/class/net/$INTERFACE/device))
    echo "   网卡 $INTERFACE 的PCI地址: $PCI_ADDR"
    
    # 停用网卡 (避免绑定冲突)
    echo "   停用网卡 $INTERFACE..."
    ip link set $INTERFACE down 2>/dev/null || true
    
    # 加载UIO驱动
    echo "   加载UIO驱动..."
    modprobe uio_pci_generic || {
        echo "❌ 无法加载 uio_pci_generic 驱动"
        echo "   尝试使用 vfio-pci 驱动..."
        modprobe vfio-pci || {
            echo "❌ 无法加载任何DPDK驱动"
            exit 1
        }
        DRIVER_NAME="vfio-pci"
    }
    
    if [ -z "$DRIVER_NAME" ]; then
        DRIVER_NAME="uio_pci_generic"
    fi
    
    # 查找dpdk-devbind.py
    DEVBIND_SCRIPT=""
    for path in "/usr/local/bin/dpdk-devbind.py" "/usr/bin/dpdk-devbind.py" "/tmp/dpdk-stable-19.11.14/usertools/dpdk-devbind.py"; do
        if [ -f "$path" ]; then
            DEVBIND_SCRIPT="$path"
            break
        fi
    done
    
    if [ -z "$DEVBIND_SCRIPT" ]; then
        echo "❌ 找不到 dpdk-devbind.py 脚本"
        echo "   请检查DPDK安装或手动指定脚本路径"
        exit 1
    fi
    
    echo "   使用脚本: $DEVBIND_SCRIPT"
    echo "   使用驱动: $DRIVER_NAME"
    
    # 绑定网卡
    echo "   绑定网卡到DPDK..."
    if python3 $DEVBIND_SCRIPT -b $DRIVER_NAME $PCI_ADDR; then
        echo "✅ 网卡绑定成功"
    else
        echo "❌ 网卡绑定失败"
        echo "   您可以稍后手动绑定:"
        echo "   python3 $DEVBIND_SCRIPT -b $DRIVER_NAME $PCI_ADDR"
    fi
    
    echo "   当前绑定状态:"
    python3 $DEVBIND_SCRIPT --status | head -20
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
if [ "$COMPILE_ONLY" = true ]; then
    echo "       DPDK 编译环境配置完成!"
else
    echo "         DPDK 环境配置完成!"
fi
echo "=========================================="
echo ""

if [ "$COMPILE_ONLY" = true ]; then
    echo "编译环境已就绪:"
    echo "1. 加载环境变量: source /tmp/dpdk-env.sh"
    echo "2. 编译项目: CGO_LDFLAGS_ALLOW='-Wl,.*' go build"
    echo ""
    echo "注意事项:"
    echo "- 当前为编译模式，未配置hugepages和网卡绑定"
    echo "- 如需运行程序，请使用完整安装模式:"
    echo "  sudo $0 <网卡名称>  # 完整安装"
    echo ""
    echo "或手动配置运行环境:"
    echo "- 配置hugepages: echo 1024 | sudo tee /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages"
    echo "- 绑定网卡: sudo $0 <网卡名称>"
else
    echo "下一步操作:"
    echo "1. 加载环境变量: source /tmp/dpdk-env.sh"
    echo "2. 编译项目: CGO_LDFLAGS_ALLOW='-Wl,.*' go build"
    if [ -z "$INTERFACE" ]; then
        echo "3. 绑定网卡: sudo $0 <网卡名称>"
    fi
    echo ""
    echo "如遇问题可尝试:"
    echo "- 驱动库缺失: sudo ./fix-dpdk-drivers.sh"
    echo "- 环境检查: ./check-dpdk-env.sh"
    echo ""
    echo "验证命令:"
    echo "- 检查hugepages: cat /proc/meminfo | grep -i huge"
    echo "- 检查DPDK: pkg-config --modversion libdpdk"
    if [ ! -z "$INTERFACE" ]; then
        echo "- 检查网卡绑定: python3 $DEVBIND_SCRIPT --status"
    fi
fi
