#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DEFAULT_PORT="${DEFAULT_PORT:-8080}"
CONFIG_PATH="${CONFIG_PATH:-}"

find_config_path() {
  if [[ -n "${MIO_ROOT_DIR:-}" ]] && [[ -f "$MIO_ROOT_DIR/conf.yaml" ]]; then
    printf '%s\n' "$MIO_ROOT_DIR/conf.yaml"
    return 0
  fi

  local dir="$SCRIPT_DIR"
  for _ in {1..6}; do
    if [[ -f "$dir/conf.yaml" ]]; then
      printf '%s\n' "$dir/conf.yaml"
      return 0
    fi
    dir="$(dirname "$dir")"
  done

  return 1
}

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

  local addr_line addr_candidate
  addr_line=$(grep -E '^\s*http_addr:\s*' "$config_file" 2>/dev/null | head -n1 || true)
  if [[ -n "${addr_line:-}" ]]; then
    addr_candidate=$(printf '%s\n' "$addr_line" | sed -E 's/^\s*http_addr:\s*//; s/"//g; s/#.*$//')
    addr_candidate=$(printf '%s\n' "$addr_candidate" | tr -d '[:space:]')
    addr_candidate="${addr_candidate##*:}"
    if [[ "$addr_candidate" =~ ^[0-9]+$ ]]; then
      printf '%s\n' "$addr_candidate"
      return 0
    fi
  fi

  local system_port
  system_port=$(awk '
    $1 == "system_config:" {in=1; next}
    in && $1 ~ /^[^[:space:]]/ {in=0}
    in && $1 ~ /^port:/ {print $2; exit}
  ' "$config_file" 2>/dev/null || true)
  if [[ "${system_port:-}" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$system_port"
    return 0
  fi

  return 1
}

if [[ -z "${CONFIG_PATH:-}" ]]; then
  if CONFIG_PATH=$(find_config_path); then
    :
  else
    CONFIG_PATH=""
  fi
fi

PORT="${MIO_SERVER_PORT:-}"
if [[ -z "$PORT" ]]; then
  if [[ -n "${CONFIG_PATH:-}" ]] && PORT=$(extract_port_from_config "$CONFIG_PATH"); then
    :
  else
    PORT="$DEFAULT_PORT"
  fi
fi

echo "检查 mio-server 端口: $PORT"
check_and_kill_port "$PORT"

echo "Starting mio-server on port $PORT..."
go run ./cmd
