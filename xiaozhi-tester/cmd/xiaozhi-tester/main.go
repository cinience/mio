package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"xiaozhi-tester/internal/client"
)

func main() {
	app := &cli.App{
		Name:  "xiaozhi-tester",
		Usage: "自动化回归测试小智后端 WebSocket 协议",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "url",
				Aliases:  []string{"u"},
				Usage:    "WebSocket 服务地址（必填）",
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:    "audio",
				Aliases: []string{"a"},
				Usage:   "本地 WAV/PCM 文件路径（可重复指定）",
			},
			&cli.StringSliceFlag{
				Name:    "audio-dir",
				Aliases: []string{"A"},
				Usage:   "包含音频文件的目录（递归扫描，可重复指定）",
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "Authorization 头使用的 Bearer Token",
				EnvVars: []string{"XIAOZHI_TOKEN"},
			},
			&cli.StringFlag{
				Name:  "device-id",
				Usage: "自定义 Device-Id（默认自动生成本地 MAC 样式串）",
			},
			&cli.StringFlag{
				Name:  "client-id",
				Usage: "自定义 Client-Id（默认自动生成 UUID）",
			},
			&cli.StringFlag{
				Name:  "mode",
				Usage: "拾音模式：auto / manual / realtime",
				Value: "auto",
			},
			&cli.StringFlag{
				Name:  "wake-word",
				Usage: "向服务端上报的唤醒词文本",
			},
			&cli.IntFlag{
				Name:  "frame-duration",
				Usage: "Opus 帧长度，单位毫秒 (10/20/40/60)",
				Value: 60,
			},
			&cli.IntFlag{
				Name:  "sample-rate",
				Usage: "输入 PCM 采样率",
				Value: 16000,
			},
			&cli.IntFlag{
				Name:  "channels",
				Usage: "输入声道数（仅支持 1）",
				Value: 1,
			},
			&cli.BoolFlag{
				Name:  "realtime",
				Usage: "按实时节奏推送音频",
			},
			&cli.IntFlag{
				Name:  "repeat",
				Usage: "对音频列表循环的次数",
				Value: 1,
			},
			&cli.DurationFlag{
				Name:  "gap",
				Usage: "多轮之间的间隔时间",
				Value: 0,
			},
			&cli.DurationFlag{
				Name:  "wait-after",
				Usage: "所有音频推送完成后保留连接的时长",
				Value: 5 * time.Second,
			},
			&cli.BoolFlag{
				Name:  "enable-mcp",
				Usage: "在 hello 中声明 MCP 能力",
			},
			&cli.BoolFlag{
				Name:  "insecure",
				Usage: "跳过 TLS 证书校验（仅限本地测试）",
			},
			&cli.BoolFlag{
				Name:  "auto-continue",
				Usage: "auto 模式下，TTS 停止后自动进入下一轮",
				Value: true,
			},
			&cli.DurationFlag{
				Name:  "tts-timeout",
				Usage: "等待 TTS 停止的超时时间",
				Value: 45 * time.Second,
			},
			&cli.BoolFlag{
				Name:  "text-mode",
				Usage: "启用文本对话模式（无需音频流）",
			},
			&cli.IntFlag{
				Name:  "text-count",
				Usage: "文本模式下随机对话轮数",
				Value: 5,
			},
			&cli.StringFlag{
				Name:  "text-file",
				Usage: "自定义文本语料文件（每行一条，文本模式下可选）",
			},
		},
		Action: func(c *cli.Context) error {
			audioInputs := normalizeAudioInputs(c.StringSlice("audio"))
			dirInputs := normalizeAudioInputs(c.StringSlice("audio-dir"))
			if len(dirInputs) > 0 {
				filesFromDirs, err := collectAudioFromDirs(dirInputs)
				if err != nil {
					return err
				}
				audioInputs = append(audioInputs, filesFromDirs...)
			}
			audioInputs = dedupeStrings(audioInputs)
			textMode := c.Bool("text-mode")
			if !textMode && len(audioInputs) == 0 {
				return cli.Exit("至少需要提供一个 --audio 或 --audio-dir 参数；或启用 --text-mode", 2)
			}

			frameDuration := c.Int("frame-duration")
			if frameDuration != 10 && frameDuration != 20 && frameDuration != 40 && frameDuration != 60 {
				return cli.Exit("--frame-duration 必须为 10、20、40 或 60", 2)
			}

			channels := c.Int("channels")
			if !textMode && channels != 1 {
				return cli.Exit("--channels 仅支持值为 1", 2)
			}

			repeat := c.Int("repeat")
			if repeat <= 0 {
				return cli.Exit("--repeat 必须大于等于 1", 2)
			}

			cfg := client.Config{
				URL:            strings.TrimSpace(c.String("url")),
				Token:          strings.TrimSpace(c.String("token")),
				DeviceID:       strings.TrimSpace(c.String("device-id")),
				ClientID:       strings.TrimSpace(c.String("client-id")),
				Mode:           strings.ToLower(strings.TrimSpace(c.String("mode"))),
				WakeWord:       strings.TrimSpace(c.String("wake-word")),
				FrameDuration:  frameDuration,
				SampleRate:     c.Int("sample-rate"),
				Channels:       channels,
				AudioPaths:     audioInputs,
				Realtime:       c.Bool("realtime"),
				Repeat:         repeat,
				Gap:            c.Duration("gap"),
				WaitAfter:      c.Duration("wait-after"),
				EnableMCP:      c.Bool("enable-mcp"),
				InsecureTLS:    c.Bool("insecure"),
				AutoContinue:   c.Bool("auto-continue"),
				TTSStopTimeout: c.Duration("tts-timeout"),
				TextMode:       textMode,
				TextCount:      c.Int("text-count"),
				TextFile:       strings.TrimSpace(c.String("text-file")),
			}

			tester, err := client.New(cfg)
			if err != nil {
				return err
			}

			return tester.Run(c.Context)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func normalizeAudioInputs(inputs []string) []string {
	var result []string
	for _, raw := range inputs {
		for _, part := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}

func collectAudioFromDirs(dirs []string) ([]string, error) {
	var files []string
	seen := make(map[string]struct{})
	allowExt := map[string]struct{}{
		".wav": {},
		".pcm": {},
		".raw": {},
	}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if _, ok := allowExt[ext]; !ok {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("扫描目录 %s 失败: %w", dir, err)
		}
	}
	sort.Strings(files)
	return files, nil
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
