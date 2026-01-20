#!/usr/bin/env bash
#
# 更新仓库内所有需要的模块中 cloudwego/eino 以及 sherpa-onnx-go 相关依赖的版本。
# 默认升级到最新版本，可通过环境变量定制：
#   EINO_VERSION=<version> ./scripts/update-eino-sherpa.sh
#   SHERPA_VERSION=<version> ./scripts/update-eino-sherpa.sh
#
# 使用： ./scripts/update-eino-sherpa.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULES=(
    "backend-server"
    "manager-server"
    "speech-server"
    "aio-server"
    "desktop-app/xiaozhi-desktop"
)

EINO_PACKAGE="github.com/cloudwego/eino"
SHERPA_PACKAGES=(
    "github.com/k2-fsa/sherpa-onnx-go"
    "github.com/k2-fsa/sherpa-onnx-go-linux"
    "github.com/k2-fsa/sherpa-onnx-go-macos"
    "github.com/k2-fsa/sherpa-onnx-go-windows"
)

EINO_VERSION="${EINO_VERSION:-latest}"
SHERPA_VERSION="${SHERPA_VERSION:-latest}"

has_dep() {
    local go_mod_file="$1"
    local dep="$2"
    grep -Fq "${dep} " "${go_mod_file}" || \
        grep -Fq "${dep}	" "${go_mod_file}" || \
        grep -Fq "${dep} =>" "${go_mod_file}"
}

update_module() {
    local module_path="$1"
    local module_dir="${REPO_ROOT}/${module_path}"
    local go_mod_file="${module_dir}/go.mod"

	if [[ ! -f "${go_mod_file}" ]]; then
		echo "skip: ${module_path} 缺少 go.mod"
		return
	fi

	echo "==> 处理 ${module_path}"
	pushd "${module_dir}" >/dev/null

    local updated=0

    if has_dep "${go_mod_file}" "${EINO_PACKAGE}"; then
        echo "   - 更新 ${EINO_PACKAGE}@${EINO_VERSION}"
        go get "${EINO_PACKAGE}@${EINO_VERSION}"
        updated=1
    fi

    for pkg in "${SHERPA_PACKAGES[@]}"; do
        if has_dep "${go_mod_file}" "${pkg}"; then
            echo "   - 更新 ${pkg}@${SHERPA_VERSION}"
            go get "${pkg}@${SHERPA_VERSION}"
            updated=1
        fi
    done

    if [[ "${updated}" == 1 ]]; then
        go mod tidy
    else
        echo "   - 无需更新"
    fi

    popd >/dev/null
}

for module in "${MODULES[@]}"; do
	update_module "${module}"
done

echo "完成：依赖已更新并整理。"
