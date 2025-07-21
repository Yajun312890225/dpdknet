#!/bin/bash

# SSL证书问题诊断和修复脚本
# 使用方法: sudo ./fix-ssl-certs.sh

echo "=========================================="
echo "      SSL证书问题诊断和修复脚本"
echo "=========================================="

# 检查是否为root权限
if [[ $EUID -ne 0 ]]; then
   echo "错误: 此脚本需要root权限运行"
   echo "请使用: sudo $0"
   exit 1
fi

echo "1. 诊断SSL证书问题..."

# 测试连接
echo "   测试HTTPS连接..."
if curl -s --connect-timeout 10 https://go.dev > /dev/null; then
    echo "   ✅ HTTPS连接正常"
    echo "   SSL证书没有问题，可以正常下载"
    exit 0
else
    echo "   ❌ HTTPS连接失败"
fi

# 检查系统类型
if [ -f /etc/debian_version ]; then
    DISTRO="debian"
elif [ -f /etc/redhat-release ]; then
    DISTRO="redhat"
else
    DISTRO="unknown"
fi

echo "   检测到系统类型: $DISTRO"

echo ""
echo "2. 尝试修复SSL证书..."

case $DISTRO in
    "debian")
        echo "   更新Debian/Ubuntu系统的CA证书..."
        apt update
        apt install -y ca-certificates curl wget
        update-ca-certificates
        ;;
    "redhat")
        echo "   更新RedHat/CentOS系统的CA证书..."
        yum update -y ca-certificates curl wget
        # 或者使用dnf (newer versions)
        # dnf update -y ca-certificates curl wget
        ;;
    *)
        echo "   未知系统类型，请手动安装ca-certificates包"
        ;;
esac

echo ""
echo "3. 重新测试HTTPS连接..."
if curl -s --connect-timeout 10 https://go.dev > /dev/null; then
    echo "   ✅ HTTPS连接修复成功"
    echo "   现在可以正常下载Go了"
    echo ""
    echo "建议执行: sudo ./install-go.sh"
else
    echo "   ❌ HTTPS连接仍然失败"
    echo ""
    echo "替代解决方案:"
    echo "1. 使用HTTP镜像下载:"
    echo "   wget -O /tmp/go1.19.13.linux-amd64.tar.gz http://mirrors.ustc.edu.cn/golang/go1.19.13.linux-amd64.tar.gz"
    echo ""
    echo "2. 跳过SSL验证下载 (不推荐):"
    echo "   curl -L -k -o /tmp/go1.19.13.linux-amd64.tar.gz https://go.dev/dl/go1.19.13.linux-amd64.tar.gz"
    echo ""
    echo "3. 使用浏览器手动下载:"
    echo "   访问 https://go.dev/dl/ 下载后放到 /tmp/ 目录"
    echo ""
    echo "下载完成后执行: sudo ./install-go.sh"
fi

echo ""
echo "=========================================="
echo "        SSL证书诊断完成"
echo "=========================================="
