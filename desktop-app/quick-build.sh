#!/bin/bash
# 小智桌面端快速打包脚本

echo "🚀 小智桌面端快速打包..."

# 进入项目目录
cd xiaozhi-desktop

# 设置环境变量
export PATH=$PATH:/home/vipas/go/bin

# 检查依赖
echo "📋 检查依赖..."
if ! dpkg -l | grep -q "libgtk-3-dev"; then
    echo "⚠️  缺少 libgtk-3-dev，正在安装..."
    # 检测 Ubuntu 版本并使用正确的包名
    UBUNTU_VERSION=$(lsb_release -rs)
    if [[ "$UBUNTU_VERSION" == "24.04" ]]; then
        echo "🔧 检测到 Ubuntu 24.04，使用 libwebkit2gtk-4.1-dev"
        sudo apt install -y libgtk-3-dev libwebkit2gtk-4.1-dev build-essential
    else
        echo "🔧 使用通用包名 libwebkit2gtk-4.0-dev"
        sudo apt install -y libgtk-3-dev libwebkit2gtk-4.0-dev build-essential
    fi
fi

# 生成绑定
echo "🔗 生成 Wails 绑定..."
wails generate module

# 构建应用
echo "🔨 构建应用..."
# 生成编译时间（格式：YYYY-MM-DD HH:MM:SS）
BUILD_TIME=$(date '+%Y-%m-%d %H:%M:%S')
echo "📅 编译时间: $BUILD_TIME"
wails build -ldflags "-X 'main.BuildTime=$BUILD_TIME'"

# 创建输出目录
mkdir -p ../dist

# 复制可执行文件
cp build/bin/xiaozhi-desktop ../dist/
chmod +x ../dist/xiaozhi-desktop

echo "✅ 打包完成！"
echo "📦 可执行文件: dist/xiaozhi-desktop"
echo "🚀 运行命令: ./dist/xiaozhi-desktop"
