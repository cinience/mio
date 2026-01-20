#!/bin/bash
# 测试构建脚本

echo "🧪 测试小智桌面端构建..."

# 进入项目目录
cd xiaozhi-desktop

# 设置环境变量
export PATH=$PATH:/home/vipas/go/bin

# 检查 Wails 命令
echo "📋 检查 Wails 命令..."
if ! command -v wails &> /dev/null; then
    echo "❌ Wails 命令未找到"
    exit 1
fi
echo "✅ Wails 命令可用"

# 检查项目文件
echo "📋 检查项目文件..."
if [ ! -f "wails.json" ]; then
    echo "❌ 未找到 wails.json"
    exit 1
fi
echo "✅ 项目文件存在"

# 生成绑定
echo "🔗 生成 Wails 绑定..."
wails generate module
echo "✅ 绑定生成完成"

# 检查前端
echo "📋 检查前端..."
if [ ! -d "frontend" ]; then
    echo "❌ 前端目录不存在"
    exit 1
fi

# 安装前端依赖
echo "📦 安装前端依赖..."
cd frontend
if [ ! -d "node_modules" ]; then
    npm install
fi
npm run build
cd ..
echo "✅ 前端构建完成"

# 尝试构建
echo "🔨 尝试构建应用..."
wails build

# 检查构建结果
if [ -f "build/bin/xiaozhi-desktop" ]; then
    echo "✅ 构建成功！"
    echo "📦 可执行文件: build/bin/xiaozhi-desktop"
    ls -la build/bin/
else
    echo "❌ 构建失败"
    echo "📋 检查 build 目录:"
    ls -la build/ || echo "build 目录不存在"
fi
