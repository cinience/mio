#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

CONFIG_PATH="${CONFIG_PATH:-config/config.yaml}"
DEFAULT_PORT="${DEFAULT_PORT:-8009}"

check_and_kill_port() {
  local port="$1"
  if lsof -i :"$port" >/dev/null 2>&1; then
    echo "端口 $port 已被占用，正在运行的进程："
    lsof -i :"$port"
    echo "正在自动关闭占用端口 $port 的进程..."

    local pids
    pids=$(lsof -ti :"$port")
    for pid in $pids; do
      echo "杀死进程 PID: $pid"
      kill "$pid" || true
      sleep 1
      if kill -0 "$pid" 2>/dev/null; then
        echo "进程 $pid 仍在运行，强制杀死..."
        kill -9 "$pid" || true
      fi
    done

    sleep 2
    if lsof -i :"$port" >/dev/null 2>&1; then
      echo "警告：端口 $port 仍被占用，可能有其他进程占用"
      exit 1
    else
      echo "端口 $port 已释放"
    fi
  fi
}

extract_port_from_config() {
  local config_file="$1"
  if [[ ! -f "$config_file" ]]; then
    return 1
  fi

  local addr_line
  addr_line=$(grep -E '^\s*addr:\s*"' "$config_file" 2>/dev/null | head -n1 | sed -E 's/.*"([^"]+)".*/\1/' || true)
  if [[ -z "${addr_line:-}" ]]; then
    return 1
  fi

  local candidate="${addr_line##*:}"
  if [[ "$candidate" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$candidate"
    return 0
  fi

  return 1
}

PORT="${SPEECH_SERVER_PORT:-}"
if [[ -z "$PORT" ]] && ! PORT=$(extract_port_from_config "$CONFIG_PATH"); then
  PORT="$DEFAULT_PORT"
fi

echo "检查 speech-server 端口: $PORT"
check_and_kill_port "$PORT"

echo "Starting speech-server on port $PORT..."
go run ./cmd/server --config "$CONFIG_PATH"
