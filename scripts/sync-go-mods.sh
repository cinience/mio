#!/usr/bin/env bash
#
# 同步本仓库中依赖内部模块的 go.mod，确保 replace 路径与 go.mod 依赖保持最新。
#
# 当前默认会处理：
#   - aio-server
#   - desktop-app/xiaozhi-desktop
#
# 使用：./scripts/sync-go-mods.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

MODULES=(
	"aio-server"
	"desktop-app/xiaozhi-desktop"
)

declare -A MODULE_DEPENDENCIES
MODULE_DEPENDENCIES["aio-server"]="backend-server manager-server speech-server"
MODULE_DEPENDENCIES["desktop-app/xiaozhi-desktop"]="aio-server backend-server manager-server speech-server"

relpath() {
	python3 - "$1" "$2" <<'PY'
import os
import sys

target = os.path.abspath(sys.argv[1])
base = os.path.abspath(sys.argv[2])
print(os.path.relpath(target, base))
PY
}

ensure_replaces() {
	local module_path="$1"
	local deps="${MODULE_DEPENDENCIES[$module_path]:-}"
	[[ -z "${deps}" ]] && return

	local module_dir="${REPO_ROOT}/${module_path}"
	for dep in ${deps}; do
		local dep_dir="${REPO_ROOT}/${dep}"
		if [[ ! -d "${dep_dir}" ]]; then
			echo "warn: 依赖模块 ${dep} 不存在，跳过 replace" >&2
			continue
		fi
		local rel
		rel="$(relpath "${dep_dir}" "${module_dir}")"
		go mod edit -replace "${dep}=${rel}"
	done
}

update_module() {
	local module_path="$1"
	local module_dir="${REPO_ROOT}/${module_path}"

	if [[ ! -f "${module_dir}/go.mod" ]]; then
		echo "skip: ${module_path} 不包含 go.mod"
		return
	fi

	echo "==> 处理 ${module_path}"
	pushd "${module_dir}" >/dev/null

	ensure_replaces "${module_path}"
	go mod tidy

	popd >/dev/null
}

if [[ -f "${REPO_ROOT}/go.work" ]]; then
	echo "==> 更新 go.work"
	(
		cd "${REPO_ROOT}"
		go work sync
	)
fi

for module in "${MODULES[@]}"; do
	update_module "${module}"
done

echo "完成：go.mod 已同步。"
