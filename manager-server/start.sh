#!/usr/bin/env bash
set -euo pipefail

# 定位到 manager-api-go 目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

export GO111MODULE=on
export CGO_ENABLED=${CGO_ENABLED:-0}

FRONTEND_FALLBACK_DIR="$PROJECT_ROOT/webbak/manager-web"
MOBILE_FALLBACK_DIR="$PROJECT_ROOT/webbak/manager-mobile"
CONSOLE_FALLBACK_DIR="$PROJECT_ROOT/webbak/manager-console"

# 嵌入式静态资源目标目录（用于 go:embed）
EMBED_DIST_ROOT="$SCRIPT_DIR/webassets"

sync_embed_assets() {
  local src="$1"
  local subdir="$2"

  if [ ! -d "$src" ]; then
    return
  fi

  local dest="$EMBED_DIST_ROOT/$subdir"
  mkdir -p "$dest"
  rm -rf "$dest"/*
  cp -r "$src"/* "$dest"/
}

# 1) 检查前端静态资源（manager-web 默认不再自动构建）
FRONTEND_DIR="$PROJECT_ROOT/web/manager-web"
CONSOLE_DIR="$PROJECT_ROOT/web/manager-console"
MANAGER_EMBED_DIR="$EMBED_DIST_ROOT/manager"
CONSOLE_EMBED_DIR="$EMBED_DIST_ROOT/manager-console"

if [ ! -d "$FRONTEND_DIR" ] && [ -d "$FRONTEND_FALLBACK_DIR" ]; then
  echo "[INFO] 默认前端目录缺失，检测到备用目录: $FRONTEND_FALLBACK_DIR"
  echo "[INFO] 脚本不会自动同步 legacy manager-web 静态资源，可按需手动处理。"
fi

if [ ! -d "$CONSOLE_DIR" ] && [ -d "$CONSOLE_FALLBACK_DIR" ]; then
  echo "[WARN] 默认 manager-console 目录缺失，使用备用目录: $CONSOLE_FALLBACK_DIR"
  CONSOLE_DIR="$CONSOLE_FALLBACK_DIR"
fi

# 同步构建 userapp（H5）如果不存在
MOBILE_DIR="$PROJECT_ROOT/web/manager-mobile"
USERAPP_EMBED_DIR="$EMBED_DIST_ROOT/userapp"
WEBCLI_SOURCE_DIR="$PROJECT_ROOT/web/web-cli/dist"

if [ ! -d "$MOBILE_DIR" ] && [ -d "$MOBILE_FALLBACK_DIR" ]; then
  echo "[WARN] 默认 H5 目录缺失，使用备用目录: $MOBILE_FALLBACK_DIR"
  MOBILE_DIR="$MOBILE_FALLBACK_DIR"
fi

if [ -f "$MANAGER_EMBED_DIR/index.html" ]; then
  echo "[INFO] 检测到已存在的 legacy manager-web 嵌入式构建产物: $MANAGER_EMBED_DIR"
else
  echo "[INFO] 未检测到 legacy manager-web 嵌入式构建产物，默认跳过自动构建。"
  echo "[INFO] 如需生成该资源，请运行项目根目录下的 web/build.sh --with-manager-web"
fi

if [ ! -f "$CONSOLE_EMBED_DIR/index.html" ]; then
  echo "[INFO] 未检测到 manager-console 嵌入式构建产物，开始自动构建..."

  if [ ! -d "$CONSOLE_DIR" ]; then
    echo "[WARN] 未找到 manager-console 目录: $CONSOLE_DIR，跳过 manager-console 构建。"
  elif [ ! -f "$CONSOLE_DIR/package.json" ]; then
    echo "[WARN] manager-console 目录缺少 package.json: $CONSOLE_DIR，跳过 manager-console 构建。"
  else
    pushd "$CONSOLE_DIR" >/dev/null
    if command -v pnpm >/dev/null 2>&1; then
      echo "[INFO] 安装 manager-console 依赖(若首次)..."
      pnpm install
      echo "[INFO] 执行 manager-console 构建 (pnpm run build)..."
      pnpm run build
    elif command -v npm >/dev/null 2>&1; then
      echo "[INFO] 安装 manager-console 依赖(若首次)..."
      if [ -f package-lock.json ]; then
        npm ci || npm install
      else
        npm install
      fi
      echo "[INFO] 执行 manager-console 构建 (npm run build)..."
      npm run build
    else
      echo "[WARN] 未检测到 pnpm 或 npm，无法构建 manager-console，跳过。"
    fi
    popd >/dev/null

    if [ -d "$CONSOLE_DIR/dist" ] && [ -f "$CONSOLE_DIR/dist/index.html" ]; then
      echo "[INFO] 同步嵌入式静态资源: $CONSOLE_EMBED_DIR"
      sync_embed_assets "$CONSOLE_DIR/dist" "manager-console"
    else
      echo "[WARN] manager-console 构建产物缺失 (未找到 $CONSOLE_DIR/dist/index.html)，跳过同步。"
    fi
  fi
else
  echo "[INFO] 检测到已存在的 manager-console 嵌入式构建产物: $CONSOLE_EMBED_DIR"
fi

# 构建并同步 userapp (H5) 产物
if [ ! -f "$USERAPP_EMBED_DIR/index.html" ]; then
  echo "[INFO] 未检测到 userapp(H5) 嵌入式构建产物，开始自动构建..."
  if [ ! -d "$MOBILE_DIR" ]; then
    echo "[WARN] 未找到 H5 工程目录: $MOBILE_DIR，跳过 userapp 构建。"
  else
    if ! command -v pnpm >/dev/null 2>&1; then
      echo "[WARN] 未检测到 pnpm，无法自动构建 userapp(H5)，跳过。"
    else
      pushd "$MOBILE_DIR" >/dev/null
      echo "[INFO] 安装 H5 依赖(若首次)..."
      pnpm install
      echo "[INFO] 执行 H5 构建..."
      pnpm run build
      popd >/dev/null

      if [ -d "$MOBILE_DIR/dist/build/h5" ] && [ -f "$MOBILE_DIR/dist/build/h5/index.html" ]; then
        echo "[INFO] 同步嵌入式静态资源: $USERAPP_EMBED_DIR"
        sync_embed_assets "$MOBILE_DIR/dist/build/h5" "userapp"
      else
        echo "[WARN] userapp 构建产物缺失 (未找到 $MOBILE_DIR/dist/build/h5/index.html)，跳过同步。"
      fi
    fi
  fi
else
  echo "[INFO] 检测到已存在的 userapp(H5) 嵌入式构建产物: $USERAPP_EMBED_DIR"
fi

if [ -d "$WEBCLI_SOURCE_DIR" ]; then
  echo "[INFO] 同步嵌入式 web-cli 资源"
  sync_embed_assets "$WEBCLI_SOURCE_DIR" "web-cli"
fi

# 2) 直接启动 Go 服务（不编译二进制）
if ! command -v go >/dev/null 2>&1; then
  echo "[ERROR] Go toolchain 未安装或不可用，请先安装 Go。" >&2
  exit 1
fi

echo "[INFO] 直接启动服务 (go run ./cmd/server)..."
echo "[INFO] 可通过 -config 指定配置文件，如: ./start.sh -config configs/config.yaml"

export XIAOZHI_DATABASE_DSN="sqlite:///../db/manager-server.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

exec go run ./cmd/server "$@"
