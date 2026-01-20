#!/bin/bash

cd "$(dirname "$0")"

# 检查端口是否被占用，如果被占用则自动关闭进程
check_and_kill_port() {
    local port=$1
    if lsof -i :$port > /dev/null 2>&1; then
        echo "端口 $port 已被占用，正在运行的进程："
        lsof -i :$port
        echo "正在自动关闭占用端口 $port 的进程..."
        
        # 获取占用端口的进程PID并杀死
        local pids=$(lsof -ti :$port)
        for pid in $pids; do
            echo "杀死进程 PID: $pid"
            kill "$pid"
            sleep 1
            # 如果进程还在，强制杀死
            if kill -0 "$pid" 2>/dev/null; then
                echo "进程 $pid 仍在运行，强制杀死..."
                kill -9 "$pid"
            fi
        done
        
        # 验证端口是否已释放
        sleep 2
        if lsof -i :$port > /dev/null 2>&1; then
            echo "警告：端口 $port 仍被占用，可能有新进程占用"
            exit 1
        else
            echo "端口 $port 已释放"
        fi
    fi
}

# 检查并关闭占用端口的进程（可以根据需要修改端口号）
check_and_kill_port 26379

rm -rf logs/*
export XIAOZHI_ENABLE_SESSION_LOG="true"
export XIAOZHI_DATABASE_DSN="sqlite:///../db/manager-server.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
#go run -tags webrtc_vad_cgo ./cmd/server $*

go run -tags sherpa_onnx ./cmd/server "$@"
