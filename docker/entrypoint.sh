#!/usr/bin/env bash
set -euo pipefail

APP_UID=${APP_UID:-${PUID:-0}}
APP_GID=${APP_GID:-${PGID:-0}}
LOG_ROOT=${LOG_ROOT:-/apps/logs}

mkdir -p /var/log/supervisor "${LOG_ROOT}" /etc/supervisor/conf.d

# 在 root 权限下创建/修正运行用户，便于随后降权执行二进制
if [ "$(id -u)" = "0" ]; then
    if [ "${APP_GID}" != "0" ] && ! getent group "${APP_GID}" >/dev/null; then
        groupadd --gid "${APP_GID}" xiaozhi
    fi

    if [ "${APP_UID}" != "0" ] && ! getent passwd "${APP_UID}" >/dev/null; then
        useradd --no-create-home --uid "${APP_UID}" --gid "${APP_GID}" --shell /usr/sbin/nologin xiaozhi
    fi
fi

# 尝试启动 ilogtaild，如果失败也不中断脚本执行
if [ -f /etc/init.d/ilogtaild ]; then
    /etc/init.d/ilogtaild start || echo "Warning: Failed to start ilogtaild, continuing..."
fi

prepare_log_symlink() {
    local app_name=$1
    local app_path="/apps/${app_name}"
    local log_target="${LOG_ROOT}/${app_name}"

    mkdir -p "${log_target}"

    if [ "$(id -u)" = "0" ]; then
        chown -R "${APP_UID}:${APP_GID}" "${log_target}"
        chown "${APP_UID}:${APP_GID}" "${app_path}" || true
        if [ -d "${app_path}/db" ]; then
            chown -R "${APP_UID}:${APP_GID}" "${app_path}/db"
        fi
    fi

    if [ -e "${app_path}/logs" ] && [ ! -L "${app_path}/logs" ]; then
        rm -rf "${app_path}/logs"
    fi

    ln -sfn "${log_target}" "${app_path}/logs"
}

exec_with_runtime_user() {
    if [ "$(id -u)" = "0" ] && [ "${APP_UID}" != "0" ]; then
        exec setpriv --reuid "${APP_UID}" --regid "${APP_GID}" --init-groups "$@"
    else
        exec "$@"
    fi
}

# RUN_MODE: backend | manager | both (default: both)
RUN_MODE=${RUN_MODE:-both}

if [ "${RUN_MODE}" = "backend" ] ; then
    APP_NAME=backend-server
    prepare_log_symlink "${APP_NAME}"

    cd "/apps/${APP_NAME}"
    exec_with_runtime_user /apps/${APP_NAME}/server --config=/apps/${APP_NAME}/config.yaml
fi

if [ "${RUN_MODE}" = "manager" ] ; then
    APP_NAME=manager-server
    prepare_log_symlink "${APP_NAME}"

    cd "/apps/${APP_NAME}"
    exec_with_runtime_user /apps/${APP_NAME}/server --config=/apps/${APP_NAME}/config.yaml
fi

if [ "${RUN_MODE}" = "all" ]; then
    prepare_log_symlink backend-server
    prepare_log_symlink manager-server

    cat >>/etc/supervisor/conf.d/programs.conf <<'EOF'
[program:backend_server]
directory=/apps/backend-server
command=/apps/backend-server/server --config=/apps/backend-server/config.yaml
autorestart=true
startretries=3
stdout_logfile=/apps/logs/backend-server/backend.stdout.log
stderr_logfile=/apps/logs/backend-server/backend.stderr.log
EOF

    cat >>/etc/supervisor/conf.d/programs.conf <<'EOF'
[program:manager_server]
directory=/apps/manager-server
command=/apps/manager-server/server --config=/apps/manager-server/config.yaml
autorestart=true
startretries=3
stdout_logfile=/apps/logs/manager-server/manager.stdout.log
stderr_logfile=/apps/logs/manager-server/manager.stderr.log
EOF

    if [ "$(id -u)" = "0" ] && [ "${APP_UID}" != "0" ]; then
        chown -R "${APP_UID}:${APP_GID}" /apps/logs/backend-server /apps/logs/manager-server
    fi

    exec_with_runtime_user /usr/bin/supervisord -n -c /etc/supervisor/supervisord.conf
fi

echo "Invalid RUN_MODE: ${RUN_MODE}"

exit 1
