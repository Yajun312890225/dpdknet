#!/bin/bash

# 手动下载Go安装包脚本
# 使用方法: ./download-go-manual.sh

echo "=========================================="
echo "     Go 安装包手动下载脚本"
echo "=========================================="

TARGET_DIR="/tmp"
FILENAME="go1.19.13.linux-amd64.tar.gz"
TARGET_PATH="$TARGET_DIR/$FILENAME"

echo "目标文件: $TARGET_PATH"
echo ""

# 清理现有文件
if [ -f "$TARGET_PATH" ]; then
    echo "发现现有文件，删除中..."
    rm -f "$TARGET_PATH"
fi

# 尝试多个下载源
declare -a URLS=(
    "https://go.dev/dl/go1.19.13.linux-amd64.tar.gz"
    "http://mirrors.ustc.edu.cn/golang/go1.19.13.linux-amd64.tar.gz"
    "http://mirrors.aliyun.com/golang/go1.19.13.linux-amd64.tar.gz"
    "http://mirrors.cloud.tencent.com/golang/go1.19.13.linux-amd64.tar.gz"
    "https://studygolang.com/dl/golang/go1.19.13.linux-amd64.tar.gz"
    "https://golang.google.cn/dl/go1.19.13.linux-amd64.tar.gz"
)

download_success=false

for i in "${!URLS[@]}"; do
    url="${URLS[$i]}"
    echo "[$((i+1))/${#URLS[@]}] 尝试下载: $url"
    
    # 尝试curl
    if command -v curl &> /dev/null; then
        echo "   使用 curl 下载..."
        if curl -L --connect-timeout 30 --max-time 600 --progress-bar \
           -o "$TARGET_PATH" "$url"; then
            
            # 验证文件大小
            if [ -f "$TARGET_PATH" ]; then
                file_size=$(stat -c%s "$TARGET_PATH" 2>/dev/null || echo 0)
                echo "   下载文件大小: ${file_size} bytes"
                
                if [ $file_size -gt 100000000 ]; then  # 大于100MB
                    echo "   ✅ curl 下载成功！"
                    download_success=true
                    break
                else
                    echo "   ❌ 文件太小，可能不是正确的安装包"
                    rm -f "$TARGET_PATH"
                fi
            fi
        fi
        
        # 如果普通curl失败，尝试跳过SSL验证
        if [ "$download_success" = false ] && [[ "$url" == https:* ]]; then
            echo "   使用 curl (跳过SSL验证) 下载..."
            if curl -L -k --connect-timeout 30 --max-time 600 --progress-bar \
               -o "$TARGET_PATH" "$url"; then
                
                if [ -f "$TARGET_PATH" ]; then
                    file_size=$(stat -c%s "$TARGET_PATH" 2>/dev/null || echo 0)
                    echo "   下载文件大小: ${file_size} bytes"
                    
                    if [ $file_size -gt 100000000 ]; then
                        echo "   ✅ curl (跳过SSL) 下载成功！"
                        download_success=true
                        break
                    else
                        echo "   ❌ 文件太小，可能不是正确的安装包"
                        rm -f "$TARGET_PATH"
                    fi
                fi
            fi
        fi
    fi
    
    # 尝试wget
    if [ "$download_success" = false ] && command -v wget &> /dev/null; then
        echo "   使用 wget 下载..."
        if wget --timeout=30 --tries=3 --progress=bar \
           -O "$TARGET_PATH" "$url"; then
            
            if [ -f "$TARGET_PATH" ]; then
                file_size=$(stat -c%s "$TARGET_PATH" 2>/dev/null || echo 0)
                echo "   下载文件大小: ${file_size} bytes"
                
                if [ $file_size -gt 100000000 ]; then
                    echo "   ✅ wget 下载成功！"
                    download_success=true
                    break
                else
                    echo "   ❌ 文件太小，可能不是正确的安装包"
                    rm -f "$TARGET_PATH"
                fi
            fi
        fi
        
        # 如果普通wget失败，尝试跳过SSL验证
        if [ "$download_success" = false ] && [[ "$url" == https:* ]]; then
            echo "   使用 wget (跳过SSL验证) 下载..."
            if wget --timeout=30 --tries=3 --progress=bar --no-check-certificate \
               -O "$TARGET_PATH" "$url"; then
                
                if [ -f "$TARGET_PATH" ]; then
                    file_size=$(stat -c%s "$TARGET_PATH" 2>/dev/null || echo 0)
                    echo "   下载文件大小: ${file_size} bytes"
                    
                    if [ $file_size -gt 100000000 ]; then
                        echo "   ✅ wget (跳过SSL) 下载成功！"
                        download_success=true
                        break
                    else
                        echo "   ❌ 文件太小，可能不是正确的安装包"
                        rm -f "$TARGET_PATH"
                    fi
                fi
            fi
        fi
    fi
    
    echo "   ❌ 此源下载失败，尝试下一个..."
    echo ""
done

if [ "$download_success" = true ]; then
    echo ""
    echo "=========================================="
    echo "        Go 安装包下载成功！"
    echo "=========================================="
    echo ""
    echo "文件位置: $TARGET_PATH"
    echo "文件大小: $(stat -c%s "$TARGET_PATH" 2>/dev/null || echo "未知") bytes"
    echo ""
    echo "下一步："
    echo "1. 运行安装脚本: sudo ./install-go.sh"
    echo "   (脚本会自动检测并使用已下载的文件)"
    echo ""
    echo "或者手动安装："
    echo "2. sudo tar -C /usr/local -xzf $TARGET_PATH"
    echo "3. export PATH=/usr/local/go/bin:\$PATH"
    echo "4. echo 'export PATH=/usr/local/go/bin:\$PATH' | sudo tee -a /etc/profile"
else
    echo ""
    echo "=========================================="
    echo "         下载失败"
    echo "=========================================="
    echo ""
    echo "所有下载源都失败了。可能的原因："
    echo "- 网络连接问题"
    echo "- 防火墙或代理限制"
    echo "- SSL证书问题"
    echo ""
    echo "建议尝试："
    echo "1. 检查网络连接"
    echo "2. 配置代理设置"
    echo "3. 使用浏览器手动下载："
    echo "   访问: https://go.dev/dl/"
    echo "   下载: go1.19.13.linux-amd64.tar.gz"
    echo "   保存到: $TARGET_PATH"
    echo ""
    echo "4. 或在其他网络环境下载后传输到此服务器"
fi
