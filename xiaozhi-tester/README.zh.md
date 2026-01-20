## 小智服务端自动化测试客户端

该模块提供一个使用 Go 实现的命令行工具，用于复现 `example/py-xiaozhi` 桌面客户端的唤醒词 → 监听 → 音频推流流程，并且支持基于 `auto` 协议的多轮对话回归测试。工具会：

- 与后端 WebSocket 服务握手并发送 `hello` 消息；
- 可选发送唤醒词 `listen/detect` 通知；
- 将本地 WAV/PCM 音频编码为 Opus 流帧后推送给服务端；
- 在 `auto` 模式下自动等待 TTS 播报结束，再开始下一轮音频推流，实现多轮测试；
- 记录所有上下行 JSON/二进制消息，便于排查问题或做差异比对。

> **依赖说明**  
> 使用 `github.com/godeps/opus`（CGo 封装）。在运行或编译前需确保系统已安装 `libopus`：
> - Debian/Ubuntu: `sudo apt install libopus-dev`
> - macOS: `brew install opus`
> - Windows (MSYS2): `pacman -S mingw-w64-*-opus`

---

### 构建

```bash
go build ./cmd/xiaozhi-tester
```

### 基本用法

```bash
go run ./cmd/xiaozhi-tester \
  --url wss://localhost:9000/ws \
  --token "$XIAOZHI_TOKEN" \
  --audio ./samples/turn1.wav \
  --audio ./samples/turn2.wav \
  --wake-word "你好小智" \
  --mode auto \
  --auto-continue \
  --tts-timeout 60s \
  --frame-duration 60 \
  --realtime \
  --wait-after 10s
```

### 常用参数

- `--url`（必填）：后端 WebSocket 地址。
- `--token`：鉴权 Bearer Token。
- `--audio`：本地音频路径，可多次指定。支持任意采样率/声道的 WAV，或 16-bit LE PCM；程序会自动重采样为 16 kHz 单声道。
- `--audio-dir`：自动遍历目录（递归），收集 `.wav`/`.pcm`/`.raw` 文件，支持多目录或逗号分隔。
- `--text-mode`：启用文本测试模式，不传音频也可模拟随机回合对话；可配合 `--text-file` 定制语料、`--text-count` 限制轮数。
- `--wake-word`：向服务端上报的唤醒词文本。
- `--mode`：`auto` / `manual` / `realtime`。当选择 `auto` 且 `--auto-continue` 为 true 时，会在接收到服务端 `tts stop` 后自动开始下一轮音频。
- `--auto-continue`：是否开启自动多轮（默认 true）。
- `--tts-timeout`：等待服务端 TTS 结束的最长时间，超时会中止测试（默认 45s）。
- `--frame-duration`：Opus 帧长（10/20/40/60 毫秒）。
- `--realtime`：开启后按真实节奏发送音频；不开启时以最快速度推送，可用于压测。
- `--repeat` / `--gap`：整体音频列表循环次数与轮次间的停顿，用于快速回归多组样本。
- `--wait-after`：最后一轮推流完成后保持连接的时间，用于收集 TTS/STT/LLM 返回。
- `--enable-mcp`：在 `hello` 中声明 MCP 支持。
- `--insecure`：跳过 TLS 校验（本地或自签场景）。

工具会在缺省时自动生成 `Device-Id`（本地管理的 MAC 样式字符串）与 `Client-Id`（UUID），所有日志均带毫秒级时间戳，方便和服务端日志进行匹配。

### TTS 等待策略

- 客户端现在会同时观察 `tts` 控制消息与下行音频帧，只要检测到连续静默就会自动触发“结束”信号，避免后端遗漏 `tts stop` 时长时间挂起。
- 如果连接被正常关闭（`close_normal`、`use of closed network connection` 等），会被视作对话自然结束并继续执行后续回合。
- 超时日志会携带最后一次 TTS 状态快照，便于回溯是哪一步没有完成。
- 如需放宽/收紧等待时长，可通过 `--tts-timeout` 显式调整，默认仍为 45 秒。
- 每轮首帧日志会附带 `first_rt` 字段，表示从音频上传完成到服务端首帧 TTS 音频返回的耗时，可用于定位链路性能瓶颈。

---

### 典型场景

1. **单轮回归**  
   使用一段语音快速验证基本握手与 ASR/LLM/TTS 链路：
   ```bash
   go run ./cmd/xiaozhi-tester --url ... --audio ./samples/demo.wav --mode manual
   ```

2. **多轮连续对话（auto 协议）**  
   准备多段用户语音，可直接指向目录；程序会在每次收到 TTS 结束后自动开始下一轮：
   ```bash
   go run ./cmd/xiaozhi-tester \
     --url ... \
     --audio-dir ./samples/multi-turn \
     --mode auto \
     --auto-continue
   ```

3. **纯文本随机对话**  
   利用内置或自定义语料生成随机问题，无需推送音频：
   ```bash
   go run ./cmd/xiaozhi-tester \
     --url ... \
     --text-mode \
     --text-count 5 \
     --text-file ./samples/prompts.txt
   ```

3. **压力/容错测试**  
   关闭 `--realtime`，提高 `--repeat` 次数，可快速向后端发送大量帧。

---

### 目录结构

```
cmd/xiaozhi-tester      # CLI 入口，负责解析参数
internal/audio          # 音频加载与重采样逻辑
internal/client         # WebSocket 客户端、协议封装、流程控制
internal/util           # 随机设备/客户端 ID 工具
```

如需在 CI 中集成，只需将命令嵌入脚本，利用返回码判断成功与否，并可配合 `--wait-after`/`--tts-timeout` 控制总时长。
