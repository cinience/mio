#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

VERSION=$(date +%Y%m%d)
#VERSION="v1.10"
VERSION=20251002

docker build -t registry.cn-shanghai.aliyuncs.com/dapp/xiaozhi:backend-go-server-${VERSION} -f ./docker/Dockerfile .
docker push registry.cn-shanghai.aliyuncs.com/dapp/xiaozhi:backend-go-server-${VERSION}
