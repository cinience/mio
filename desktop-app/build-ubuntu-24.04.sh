#!/bin/bash
# Ubuntu 24.04 专用构建脚本

echo "🚀 小智桌面端 Ubuntu 24.04 专用构建脚本"
echo "========================================"

# 检查 Ubuntu 版本
UBUNTU_VERSION=$(lsb_release -rs)
echo "📋 检测到 Ubuntu 版本: $UBUNTU_VERSION"


go install github.com/wailsapp/wails/v2/cmd/wails@latest

if [[ "$UBUNTU_VERSION" != "24.04" ]]; then
    echo "⚠️  此脚本专为 Ubuntu 24.04 设计"
    echo "   当前版本: $UBUNTU_VERSION"
    echo "   建议使用: ./build-ubuntu.sh"
    read -p "是否继续？(y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# 进入项目目录
cd xiaozhi-desktop

# 设置环境变量
export PATH=$PATH:/home/vipas/go/bin

# 安装 Ubuntu 24.04 专用依赖
echo "📦 安装 Ubuntu 24.04 专用依赖..."
echo "   包名: libwebkit2gtk-4.1-dev (而不是 4.0-dev)"

# 检查是否已安装
if ! dpkg -l | grep -q "libgtk-3-dev" || ! dpkg -l | grep -q "libwebkit2gtk-4.1-dev"; then
    echo "🔧 安装依赖包..."
    sudo apt update
    sudo apt install -y libgtk-3-dev libwebkit2gtk-4.1-dev build-essential pkg-config
    echo "✅ 依赖安装完成"
else
    echo "✅ 依赖已安装"
fi

# 验证 Wails 环境
echo "🔍 验证 Wails 环境..."
wails doctor

# 生成绑定
echo "🔗 生成 Wails 绑定..."
wails generate module

# 构建前端
echo "📦 构建前端..."
cd frontend
if [ ! -d "node_modules" ]; then
    echo "📥 安装前端依赖..."
    npm install
fi
npm run build
cd ..

# 构建应用
echo "🔨 构建桌面应用..."
wails build

# 检查构建结果
if [ -f "build/bin/xiaozhi-desktop" ]; then
    echo "✅ 构建成功！"

    # 创建输出目录
    mkdir -p ../dist

    # 复制可执行文件
    cp build/bin/xiaozhi-desktop ../dist/
    chmod +x ../dist/xiaozhi-desktop

    echo "📦 可执行文件: dist/xiaozhi-desktop"
    echo "🚀 运行命令: ./dist/xiaozhi-desktop"

    # 显示文件信息
    echo ""
    echo "📊 构建信息:"
    ls -la ../dist/xiaozhi-desktop
    file ../dist/xiaozhi-desktop

else
    echo "❌ 构建失败"
    echo "📋 检查 build 目录:"
    ls -la build/ || echo "build 目录不存在"
    exit 1
fi

echo ""
echo "🎉 Ubuntu 24.04 构建完成！"
echo "   应用已准备就绪，可以运行和分发"
