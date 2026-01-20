# Docker Compose Quickstart

本文档介绍如何使用 `docker compose` 启动并维护基于 `xiaozhi-server` 的本地一体化环境。`docker-compose.yml` 会同时构建并运行以下服务：

- `manager-server`：管理后台与 REST API（监听 `8007`，API 基础路径为 `/xiaozhi`）。
- `backend-server`：语音/设备接入与调度服务（监听 `8002`、`2883`、`8883`、`8990/udp`）。
- `speech-server`：可选的语音模型服务，默认暴露 `8009`。

所有容器均从仓库中的 Dockerfile 构建，并通过本地卷把日志与数据库持久化到 `./logs`、`./db`。

## 前置条件

- Docker Engine 24+（包含 Compose Plugin），或 Docker Desktop 任意最新版本。
- 至少 6 GB 可用磁盘空间（镜像、模型与日志）。
- 推荐 Linux/macOS；Windows 需启用 WSL2。
- 可选：如需访问阿里云镜像站外的资源，保证能连通 `mirrors.aliyun.com` 与 `modelscope.cn`。

## 快速启动

1. 克隆或进入项目根目录（`docker-compose.yml` 所在位置）。
2. 首次运行建议构建镜像：

   ```bash
   docker compose build
   ```

   - 如果需要启用 Sherpa ONNX 推理，加上环境变量：`ENABLE_SHERPA_ONNX=1 docker compose build`。

3. 以后台模式启动全部服务：

   ```bash
   docker compose up -d
   ```

4. 等待健康检查通过（约 30 秒）。查看状态：

   ```bash
   docker compose ps
   ```

5. 验证核心端点：

   ```bash
   curl http://localhost:8007/xiaozhi/health        # manager-server
   curl http://localhost:8002/health                # backend-server
   curl http://localhost:8009/health || true        # speech-server（如启用）
   ```

默认情况下，容器使用仓库内的配置文件，日志会写入 `./logs`，管理端 SQLite 数据库存放在 `./db`。

## 常用环境变量

在项目根目录创建 `.env` 文件（被 Compose 自动读取）可覆盖下列设置：

| 变量 | 默认值 | 说明 |
| ---- | ------ | ---- |
| `MANAGER_PORT` | `8007` | 暴露到宿主的管理端口（容器内部固定 8007）。 |
| `BACKEND_WS_PORT` | `8002` | 后端 WebSocket 端口。 |
| `BACKEND_MQTT_PORT` | `2883` | MQTT 明文端口。 |
| `BACKEND_TLS_MQTT_PORT` | `8883` | MQTT TLS 端口。 |
| `BACKEND_UDP_PORT` | `8990` | 语音 UDP 端口。 |
| `MANAGER_API_SECRET` | `cc36d3ca-b065-47ef-ae29-d34f7540abd1` | 后端访问管理端 API 时使用的密钥，生产环境务必改成随机值。 |
| `ENABLE_SHERPA_ONNX` | `0` | 设置为 `1`/`true` 时构建带 ONNX Runtime 的后端镜像。 |
| `MODELSCOPE_MODEL_ID` | `gomodels/sherpa` | 语音服务默认模型。 |
| `MODELSCOPE_REVISION` | 空 | 语音模型版本，可按需指定。 |

变量变更后需要重新 `docker compose up -d`（如涉及镜像构建则需带 `--build`）。

## 日志与调试

- 查看某个服务日志：`docker compose logs -f manager-server`
- 同时追踪所有服务：`docker compose logs -f`
- 容器内执行命令（如检查配置）：`docker compose exec manager-server sh`
- 默认日志路径 `./logs` 会共享给所有容器，也可在宿主直接查看。

## 常用运维操作

| 场景 | 命令 |
| ---- | ---- |
| 停止但保留容器 | `docker compose stop` |
| 停止并移除容器 | `docker compose down` |
| 停止并清空卷（会删除 `./db` SQLite 数据） | `docker compose down -v` |
| 重启单个服务 | `docker compose restart backend-server` |
| 重建镜像并重启 | `docker compose up -d --build manager-server` |
| 清理未使用镜像/缓存 | `docker image prune`, `docker builder prune` |

> ⚠️ `docker compose down -v` 会清空数据库和日志卷，慎用。

## 语音模型与资源

- `speech-server` 默认拉取 `gomodels/sherpa`。若部署在离线环境，需要提前把模型缓存到容器镜像或挂载路径。
- 若不需要语音服务，可通过 `docker compose up -d manager-server backend-server` 仅启动核心两个服务。

## 故障排查

1. **健康检查失败**：使用 `docker compose logs <service>` 查看详细错误；确认端口未被占用、环境变量填写正确。
2. **镜像构建失败**：检查网络连通性，或换用国内镜像源。可尝试 `docker compose build --progress=plain`.
3. **端口冲突**：修改 `.env` 中对应的宿主端口，重新 `docker compose up -d`.
4. **重新导入前端资源**：运行 `manager-server/start.sh`（需宿主安装 Node/Pnpm），然后重新构建镜像。

如需更多部署形态（例如二进制部署、独立集群），请参考各服务目录下的 README。

