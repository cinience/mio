# ---------- Stage 1: build Go binaries & stage sherpa-onnx runtime ----------
ARG BUILDER_BASE_IMAGE=registry.cn-shanghai.aliyuncs.com/dapp/xiaozhi:ubuntu-runtime-build
ARG SERVER_VERSION=""
FROM ${BUILDER_BASE_IMAGE} AS builder

ARG ENABLE_SHERPA_ONNX=0
ARG TARGETARCH
ARG SERVER_VERSION
ENV ENABLE_SHERPA_ONNX=${ENABLE_SHERPA_ONNX}

WORKDIR /app

ENV GOVERSION=1.25.0

RUN if command -v go >/dev/null; then \
        echo "Go already installed, skip download"; \
    else \
        wget -q -O /tmp/go.tgz https://mirrors.aliyun.com/golang/go${GOVERSION}.linux-amd64.tar.gz && \
        rm -rf /usr/local/go && \
        tar -C /usr/local -xzf /tmp/go.tgz && \
        rm /tmp/go.tgz; \
    fi
# 设置 Go 代理
ENV GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=1
ENV PATH=/usr/local/go/bin:$PATH

## 只有当 go.mod/go.sum 文件改变时，才会重新执行 go mod download
RUN mkdir -p backend-server
COPY ./backend-server/go.mod ./backend-server/go.sum /app/backend-server/
RUN cd /app/backend-server && go mod download && go version

# 提前解包 sherpa-onnx 模块里的预编译库，避免额外下载 onnxruntime
RUN set -eux; \
    sherpa_dir="$(cd /app/backend-server && go list -f '{{.Dir}}' -m github.com/k2-fsa/sherpa-onnx-go-linux)"; \
    arch="${TARGETARCH:-amd64}"; \
    case "${arch}" in \
        amd64) sherpa_subdir="x86_64-unknown-linux-gnu" ;; \
        arm64) sherpa_subdir="aarch64-unknown-linux-gnu" ;; \
        arm) sherpa_subdir="arm-unknown-linux-gnueabihf" ;; \
        *) echo "Unsupported TARGETARCH ${arch}" >&2; exit 1 ;; \
    esac; \
    mkdir -p /app/sherpa-libs; \
    cp "${sherpa_dir}/lib/${sherpa_subdir}/"*.so /app/sherpa-libs/; \
    rm -f /usr/local/lib/libonnxruntime.so*; \
    cp /app/sherpa-libs/*.so /usr/local/lib/; \
    ldconfig

RUN mkdir -p manager-server
COPY ./manager-server/go.mod ./manager-server/go.sum /app/manager-server/
RUN cd /app/manager-server && go mod download && go version


# 复制所有源代码并编译
COPY ./manager-server/ /app/manager-server/
COPY ./backend-server/ /app/backend-server/

# 获取实际源代码的git commit id和构建时间
RUN BUILD_DATE=$(date +%Y%m%d) && \
    GIT_COMMIT=$(cd /app/backend-server && git rev-parse --short HEAD 2>/dev/null || echo "none") && \
    if [ -n "${SERVER_VERSION}" ]; then \
        VERSION=${SERVER_VERSION}; \
    else \
        VERSION=${BUILD_DATE}-${GIT_COMMIT}; \
    fi && \
    echo "${VERSION}" > /tmp/version.txt

# 根据 ENABLE_SHERPA_ONNX 控制是否启用 sherpa_onnx 构建标签
RUN VERSION=$(cat /tmp/version.txt) && cd /app/backend-server && \
    if [ "${ENABLE_SHERPA_ONNX}" = "1" ] || [ "${ENABLE_SHERPA_ONNX}" = "true" ]; then \
        echo "Building backend-server with sherpa_onnx tag enabled"; \
        go build -tags webrtc_vad_cgo,sherpa_onnx -ldflags="-s -w -X 'main.version=${VERSION}'" -o /app/backend-server/server ./cmd/server/; \
    else \
        echo "Building backend-server without sherpa_onnx tag (default)"; \
        go build -tags webrtc_vad_cgo -ldflags="-s -w -X 'main.version=${VERSION}'" -o /app/backend-server/server ./cmd/server/; \
    fi

RUN VERSION=$(cat /tmp/version.txt) && cd /app/manager-server && go build -ldflags="-s -w -X 'main.version=${VERSION}'" -o /app/manager-server/server ./cmd/server/

# ---------- Stage 2: assemble runtime image ----------
#FROM ubuntu:24.04-slim
FROM registry.cn-shanghai.aliyuncs.com/dapp/xiaozhi:ubuntu-runtime-base

ARG ENABLE_SHERPA_ONNX=0
ENV ENABLE_SHERPA_ONNX=${ENABLE_SHERPA_ONNX}

# 设置工作目录
WORKDIR /apps

# 安装 ffmpeg 以支持音频转码
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ffmpeg && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# 安装阿里云日志服务 ilogtail（SLS Logtail）并创建配置目录
RUN wget -q http://aliyun-observability-release-cn-shanghai.oss-cn-shanghai.aliyuncs.com/loongcollector/linux64/latest/loongcollector.sh -O /tmp/logtail.sh && \
    chmod +x /tmp/logtail.sh && /tmp/logtail.sh install cn-shanghai-internet && \
    touch /etc/ilogtail/users/1629965279769872 && \
    echo "xiaozhi-server" > /etc/ilogtail/user_defined_id && \
    rm -rf /var/lib/apt/lists/* /tmp/logtail.sh

# 安装 supervisord 并添加配置，统一管理 ilogtail 与应用进程
COPY docker/supervisord.conf /etc/supervisor/supervisord.conf
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# 从 builder 阶段仅复制编译好的二进制文件
COPY --from=builder /app/backend-server/server /apps/backend-server/server
COPY --from=builder /app/manager-server/server /apps/manager-server/server
COPY --from=builder /app/sherpa-libs/ /usr/local/lib/
RUN ldconfig

# 创建运行时需要的目录和文件
COPY backend-server/config/config.yaml /apps/backend-server/config.yaml
COPY backend-server/config/ /apps/backend-server/config/

COPY manager-server/config/config.yaml /apps/manager-server/config.yaml
#COPY dist/ /apps/dist/

VOLUME [ "/apps/logs" ]

# 暴露端口
EXPOSE 8002 8007 8990 1883 2883 8883

# 使用 supervisord 启动多进程
CMD ["/usr/local/bin/entrypoint.sh"]
