#!/bin/bash

# 小智桌面端开发模式启动脚本

echo "启动小智桌面端开发模式..."

# 进入项目目录
cd xiaozhi-desktop

# 检查依赖
echo "检查依赖..."

# 检查 Go 模块
if [ ! -f "go.mod" ]; then
    echo "错误: 未找到 go.mod 文件"
    exit 1
fi

# 检查前端依赖
if [ ! -d "frontend/node_modules" ]; then
    echo "安装前端依赖..."
    cd frontend
    npm install
    cd ..
fi

# 生成 Wails 绑定
echo "生成 Wails 绑定..."
export PATH=$PATH:/home/vipas/go/bin
wails generate module

# 启动开发模式
echo "启动开发模式..."
wails dev

echo "开发模式已启动！"
