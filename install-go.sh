#!/bin/bash

# Go 环境快速安装脚本
# 使用方法: sudo ./install-go.sh

echo "=========================================="
echo "         Go 环境快速安装脚本"
echo "=========================================="

# 检查是否为root权限
if [[ $EUID -ne 0 ]]; then
   echo "错误: 此脚本需要root权限运行"
   echo "请使用: sudo $0"
   exit 1
fi

# 检查当前Go版本
if command -v go &> /dev/null; then
    current_version=$(go version)
    echo "当前Go版本: $current_version"
    
    if go version | grep -q "go1.1[9-9]\|go1.[2-9][0-9]"; then
        echo "✅ Go版本满足要求 (>= 1.19)"
        echo "如需重新安装，请继续。否则按 Ctrl+C 退出。"
        read -p "按回车键继续安装，或按 Ctrl+C 退出: "
    fi
fi

echo ""
echo "开始安装 Go 1.19.13..."

# 检查SSL证书问题
echo ""
echo "检查网络和SSL配置..."
if ! curl -s --connect-timeout 5 https://go.dev > /dev/null; then
    echo "⚠️  警告: 检测到网络或SSL证书问题"
    echo "   这可能影响HTTPS下载，脚本会自动尝试多种解决方案"
    echo "   包括: HTTP镜像源、跳过SSL验证等"
else
    echo "✅ 网络连接正常"
fi

cd /tmp

# 清理旧文件
rm -f go1.19.13.linux-amd64.tar.gz*

# 尝试多个下载方法
download_success=false

# 方法1: curl (通常比wget更可靠)
echo "1. 尝试使用 curl 下载..."
if command -v curl &> /dev/null; then
    # 首先尝试正常下载
    if curl -L --connect-timeout 30 --max-time 300 --progress-bar \
       -o go1.19.13.linux-amd64.tar.gz \
       https://go.dev/dl/go1.19.13.linux-amd64.tar.gz; then
        download_success=true
        echo "✅ curl 下载成功"
    else
        echo "   正常下载失败，尝试跳过SSL验证..."
        # 如果SSL验证失败，尝试跳过SSL验证 (仅作为备选方案)
        if curl -L -k --connect-timeout 30 --max-time 300 --progress-bar \
           -o go1.19.13.linux-amd64.tar.gz \
           https://go.dev/dl/go1.19.13.linux-amd64.tar.gz; then
            download_success=true
            echo "✅ curl 下载成功 (跳过SSL验证)"
            echo "⚠️  警告: 已跳过SSL证书验证，建议检查网络配置"
        fi
    fi
fi

# 方法2: wget 官方源
if [ "$download_success" = false ]; then
    echo "2. 尝试使用 wget 下载官方源..."
    if wget --timeout=30 --tries=3 --continue --progress=bar \
       https://go.dev/dl/go1.19.13.linux-amd64.tar.gz; then
        download_success=true
        echo "✅ wget 官方源下载成功"
    else
        echo "   wget 正常下载失败，尝试跳过SSL验证..."
        if wget --timeout=30 --tries=3 --continue --progress=bar --no-check-certificate \
           https://go.dev/dl/go1.19.13.linux-amd64.tar.gz; then
            download_success=true
            echo "✅ wget 官方源下载成功 (跳过SSL验证)"
            echo "⚠️  警告: 已跳过SSL证书验证，建议检查网络配置"
        fi
    fi
fi

# 方法3: 中国镜像源
if [ "$download_success" = false ]; then
    echo "3. 尝试中国镜像源..."
    if wget --timeout=30 --tries=3 --continue --progress=bar \
       https://studygolang.com/dl/golang/go1.19.13.linux-amd64.tar.gz; then
        download_success=true
        echo "✅ 中国镜像源下载成功"
    else
        echo "   HTTPS镜像源失败，尝试HTTP镜像源..."
        if wget --timeout=30 --tries=3 --continue --progress=bar \
           http://mirrors.ustc.edu.cn/golang/go1.19.13.linux-amd64.tar.gz; then
            download_success=true
            echo "✅ HTTP镜像源下载成功"
        fi
    fi
fi

# 方法4: 阿里云镜像源 (HTTP)
if [ "$download_success" = false ]; then
    echo "4. 尝试阿里云镜像源 (HTTP)..."
    if wget --timeout=30 --tries=3 --continue --progress=bar \
       http://mirrors.aliyun.com/golang/go1.19.13.linux-amd64.tar.gz; then
        download_success=true
        echo "✅ 阿里云镜像源下载成功"
    fi
fi

# 方法5: 使用代理 (如果设置了)
if [ "$download_success" = false ] && [ ! -z "$http_proxy" ]; then
    echo "5. 尝试使用代理下载..."
    if wget --timeout=30 --tries=2 --progress=bar \
       https://go.dev/dl/go1.19.13.linux-amd64.tar.gz; then
        download_success=true
        echo "✅ 代理下载成功"
    fi
fi

# 方法6: 手动下载提示
if [ "$download_success" = false ]; then
    echo ""
    echo "❌ 自动下载失败"
    echo ""
    echo "可能的问题:"
    echo "- SSL证书验证失败"
    echo "- 网络连接问题" 
    echo "- 防火墙或代理限制"
    echo ""
    echo "解决方案:"
    echo ""
    echo "方案1: 运行SSL证书修复脚本"
    echo "  sudo ./fix-ssl-certs.sh"
    echo ""
    echo "请尝试以下手动下载方法之一:"
    echo ""
    echo "方法1: 使用浏览器下载"
    echo "  1. 打开: https://go.dev/dl/"
    echo "  2. 下载: go1.19.13.linux-amd64.tar.gz"
    echo "  3. 复制到: /tmp/go1.19.13.linux-amd64.tar.gz"
    echo "  4. 重新运行此脚本"
    echo ""
    echo "方法2: 解决SSL证书问题后下载"
    echo "  # 更新系统证书"
    echo "  sudo apt update && sudo apt install ca-certificates"
    echo "  # 或者跳过SSL验证 (不推荐，但可临时使用)"
    echo "  curl -L -k -o /tmp/go1.19.13.linux-amd64.tar.gz https://go.dev/dl/go1.19.13.linux-amd64.tar.gz"
    echo ""
    echo "方法3: 使用HTTP镜像源"
    echo "  wget -O /tmp/go1.19.13.linux-amd64.tar.gz http://mirrors.ustc.edu.cn/golang/go1.19.13.linux-amd64.tar.gz"
    echo "  # 或阿里云镜像"
    echo "  wget -O /tmp/go1.19.13.linux-amd64.tar.gz http://mirrors.aliyun.com/golang/go1.19.13.linux-amd64.tar.gz"
    echo ""
    echo "方法4: 使用其他工具下载"
    echo "  # 使用 curl"
    echo "  curl -L -o /tmp/go1.19.13.linux-amd64.tar.gz https://go.dev/dl/go1.19.13.linux-amd64.tar.gz"
    echo ""
    echo "  # 使用 aria2c (如果安装了)"
    echo "  aria2c -d /tmp https://go.dev/dl/go1.19.13.linux-amd64.tar.gz"
    echo ""
    echo "下载完成后，运行: sudo $0"
    exit 1
fi

# 验证文件
echo ""
echo "验证下载文件..."
if [ ! -f "go1.19.13.linux-amd64.tar.gz" ]; then
    echo "❌ 下载文件不存在"
    exit 1
fi

file_size=$(stat -c%s "go1.19.13.linux-amd64.tar.gz" 2>/dev/null || echo 0)
if [ $file_size -lt 100000000 ]; then  # 小于100MB说明下载不完整
    echo "❌ 下载文件不完整 (大小: ${file_size} bytes)"
    echo "正常大小应该约150MB"
    rm -f go1.19.13.linux-amd64.tar.gz
    exit 1
fi

echo "✅ 文件验证通过 (大小: ${file_size} bytes)"

# 安装Go
echo ""
echo "安装 Go..."

# 备份旧版本
if [ -d "/usr/local/go" ]; then
    echo "备份旧版本..."
    mv /usr/local/go /usr/local/go.backup.$(date +%s) 2>/dev/null || rm -rf /usr/local/go
fi

# 解压安装
echo "解压到 /usr/local/go..."
tar -C /usr/local -xzf go1.19.13.linux-amd64.tar.gz

# 设置PATH
if ! grep -q '/usr/local/go/bin' /etc/profile; then
    echo 'export PATH=/usr/local/go/bin:$PATH' >> /etc/profile
fi

export PATH=/usr/local/go/bin:$PATH

# 验证安装
echo ""
echo "验证安装..."
if /usr/local/go/bin/go version; then
    echo ""
    echo "✅ Go 安装成功!"
    echo "版本: $(/usr/local/go/bin/go version)"
    echo ""
    echo "重要提示:"
    echo "1. 当前会话请执行: export PATH=/usr/local/go/bin:\$PATH"
    echo "2. 新会话会自动加载PATH (已写入/etc/profile)"
    echo "3. 验证命令: go version"
else
    echo "❌ Go 安装失败"
    exit 1
fi

# 清理下载文件
echo ""
read -p "是否删除下载文件? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -f go1.19.13.linux-amd64.tar.gz
    echo "✅ 下载文件已清理"
fi

echo ""
echo "=========================================="
echo "         Go 安装完成!"
echo "=========================================="
