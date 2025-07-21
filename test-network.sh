#!/bin/bash

# 网络连接和下载测试脚本
# 使用方法: ./test-network.sh

echo "=========================================="
echo "      网络连接和下载能力测试"
echo "=========================================="

# 测试基本网络连接
echo "1. 测试网络连接..."

test_hosts=(
    "go.dev"
    "mirrors.ustc.edu.cn" 
    "mirrors.aliyun.com"
    "studygolang.com"
    "8.8.8.8"
)

for host in "${test_hosts[@]}"; do
    if ping -c 1 -W 5 "$host" &> /dev/null; then
        echo "   ✅ $host - 可访问"
    else
        echo "   ❌ $host - 无法访问"
    fi
done

echo ""

# 测试HTTP/HTTPS连接
echo "2. 测试HTTP/HTTPS连接..."

if command -v curl &> /dev/null; then
    echo "   使用curl测试..."
    
    # 测试HTTP
    if curl -s --connect-timeout 10 http://mirrors.ustc.edu.cn &> /dev/null; then
        echo "   ✅ HTTP连接正常"
    else
        echo "   ❌ HTTP连接失败"
    fi
    
    # 测试HTTPS
    if curl -s --connect-timeout 10 https://go.dev &> /dev/null; then
        echo "   ✅ HTTPS连接正常"
    else
        echo "   ❌ HTTPS连接失败 (可能是SSL证书问题)"
        
        # 测试跳过SSL验证
        if curl -s -k --connect-timeout 10 https://go.dev &> /dev/null; then
            echo "   ⚠️  HTTPS连接 (跳过SSL验证) 正常"
            echo "      建议修复SSL证书问题"
        else
            echo "   ❌ HTTPS连接 (即使跳过SSL验证) 仍失败"
        fi
    fi
else
    echo "   curl命令不可用，跳过连接测试"
fi

echo ""

# 测试下载一个小文件
echo "3. 测试下载能力..."

test_file="/tmp/network-test-file"
rm -f "$test_file"

# 尝试下载一个小文件进行测试
test_urls=(
    "http://mirrors.ustc.edu.cn/README"
    "http://mirrors.aliyun.com/"  
    "https://go.dev/"
)

download_working=false

for url in "${test_urls[@]}"; do
    echo "   测试下载: $url"
    
    if command -v wget &> /dev/null; then
        if wget -q --timeout=10 --tries=1 -O "$test_file" "$url" 2>/dev/null; then
            if [ -f "$test_file" ] && [ -s "$test_file" ]; then
                file_size=$(stat -c%s "$test_file" 2>/dev/null)
                echo "   ✅ 下载成功 (${file_size} bytes)"
                download_working=true
                rm -f "$test_file"
                break
            fi
        fi
    elif command -v curl &> /dev/null; then
        if curl -s --connect-timeout 10 -o "$test_file" "$url" 2>/dev/null; then
            if [ -f "$test_file" ] && [ -s "$test_file" ]; then
                file_size=$(stat -c%s "$test_file" 2>/dev/null)
                echo "   ✅ 下载成功 (${file_size} bytes)"
                download_working=true
                rm -f "$test_file"
                break
            fi
        fi
    fi
    
    echo "   ❌ 下载失败"
done

if [ "$download_working" = false ]; then
    echo "   ⚠️  所有测试下载都失败"
fi

rm -f "$test_file"

echo ""

# 检查代理设置
echo "4. 检查代理设置..."
if [ -n "$http_proxy" ] || [ -n "$HTTP_PROXY" ]; then
    echo "   HTTP代理: ${http_proxy:-${HTTP_PROXY}}"
else
    echo "   未设置HTTP代理"
fi

if [ -n "$https_proxy" ] || [ -n "$HTTPS_PROXY" ]; then
    echo "   HTTPS代理: ${https_proxy:-${HTTPS_PROXY}}"
else
    echo "   未设置HTTPS代理"
fi

echo ""

# 检查DNS设置
echo "5. 检查DNS设置..."
if [ -f /etc/resolv.conf ]; then
    echo "   DNS服务器:"
    grep "^nameserver" /etc/resolv.conf | head -3
else
    echo "   无法读取DNS配置"
fi

echo ""

# 总结和建议
echo "=========================================="
echo "           测试总结和建议"
echo "=========================================="

if [ "$download_working" = true ]; then
    echo "✅ 网络连接基本正常，可以尝试下载Go"
    echo ""
    echo "建议执行:"
    echo "  ./download-go-manual.sh   # 使用多源下载脚本"
    echo "  sudo ./install-go.sh      # 或直接使用安装脚本"
else
    echo "❌ 网络连接存在问题"
    echo ""
    echo "可能的解决方案:"
    echo "1. 检查网络连接和防火墙设置"
    echo "2. 配置HTTP代理 (如果在企业网络中):"
    echo "   export http_proxy=http://proxy.example.com:8080"
    echo "   export https_proxy=http://proxy.example.com:8080"
    echo "3. 修复SSL证书问题:"
    echo "   sudo ./fix-ssl-certs.sh"
    echo "4. 联系网络管理员"
    echo "5. 使用其他网络环境下载后传输文件"
fi
