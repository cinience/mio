package music

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"
	"backend-server/pkg/audio"
)

// 全局HTTP客户端，实现连接池
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// 获取配置了连接池的HTTP客户端
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		httpClient = &http.Client{
			Transport: transport,
			//Timeout:   30 * time.Second,
		}
	})
	return httpClient
}

// PlayMusicStream 从URL播放音乐，返回音频流通道
// frameDuration: 每帧时长（毫秒），默认20ms
// audioFormat: 音频格式，支持 "mp3"
func PlayMusicStream(ctx context.Context, url string, sampleRate int, frameDuration int, audioFormat string) (outputChan chan []byte, err error) {
	// 参数校验和默认值设置
	if frameDuration <= 0 {
		frameDuration = 20 // 默认20ms帧时长
	}
	if audioFormat == "" {
		audioFormat = "mp3" // 默认MP3格式
	}

	startTs := time.Now().UnixMilli()

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Accept", "audio/*")
	req.Header.Set("User-Agent", "MusicPlayer/1.0")

	// 使用连接池创建客户端
	client := getHTTPClient()

	// 创建输出通道
	outputChan = make(chan []byte, 100)

	// 启动goroutine处理流式响应
	go func() {
		// 发送请求
		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("发送请求失败: %v", err)
			close(outputChan)
			return
		}

		// 检查响应状态码
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
			close(outputChan)
			return
		}

		// 检查响应内容类型和内容长度
		contentLength := resp.ContentLength
		acceptRanges := strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes")
		log.Infof("收到Subsonic音乐流响应，Content-Length: %d, Accept-Ranges: %v, Content-Range: %s", contentLength, acceptRanges, resp.Header.Get("Content-Range"))

		// 判断Content-Length是否合理
		if contentLength == 0 {
			log.Errorf("音乐流返回空响应，Content-Length为0")
			close(outputChan)
			return
		}

		// MP3文件头至少需要100字节才能正常解析
		// -1表示未知长度（例如分块传输）
		if contentLength > 0 && contentLength < 100 {
			log.Errorf("音乐流响应太小无法解析为MP3: %d字节", contentLength)
			close(outputChan)
			return
		}

		log.Infof("开始播放音乐: %s", url)

		pipeReader, pipeWriter := io.Pipe()
		streamer := newSubsonicStreamDownloader(ctx, client, url, req.Header.Clone(), resp, pipeWriter, acceptRanges)
		go streamer.run()

		// 根据音频格式处理流式响应
		if audioFormat == "mp3" {
			// 创建 MP3 解码器，传入 context 而不是 done 通道
			mp3Decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, audioFormat, sampleRate)
			if err != nil {
				log.Errorf("创建MP3解码器失败: %v", err)
				pipeReader.CloseWithError(err)
				close(outputChan)
				return
			}

			// 启动解码过程
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3解码失败: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("音乐播放取消, URL: %s, err: %v, cause: %v", url, ctx.Err(), context.Cause(ctx))
				return
			default:
				log.Infof("音乐播放完成耗时: %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("当前仅支持MP3格式的流式播放，传入格式: %s", audioFormat)
			close(outputChan)
		}
	}()

	return outputChan, nil
}

func PlayMusicFromAudioData(ctx context.Context, audioData []byte, sampleRate int, frameDuration int, audioFormat string) (outputChan chan []byte, err error) {
	// 参数校验和默认值设置
	if frameDuration <= 0 {
		frameDuration = 20 // 默认20ms帧时长
	}
	if audioFormat == "" {
		audioFormat = "mp3" // 默认MP3格式
	}

	// 添加调试信息
	log.Debugf("PlayMusicFromAudioData: 音频数据长度=%d字节, 采样率=%d, 帧时长=%dms, 格式=%s",
		len(audioData), sampleRate, frameDuration, audioFormat)

	// 检查音频数据是否为空
	if len(audioData) == 0 {
		log.Errorf("音频数据为空，无法播放")
		return nil, fmt.Errorf("音频数据为空")
	}

	startTs := time.Now().UnixMilli()

	// 创建输出通道
	outputChan = make(chan []byte, 100)

	// 启动goroutine处理流式响应
	go func() {
		// 从 audioData 创建一个 io.ReadCloser
		audioReader := io.NopCloser(bytes.NewReader(audioData))

		// 根据音频格式处理流式响应
		if audioFormat == "mp3" {
			// 创建 MP3 解码器，传入 context 而不是 done 通道
			mp3Decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, audioReader, outputChan, frameDuration, audioFormat, sampleRate)
			if err != nil {
				log.Errorf("创建MP3解码器失败: %v", err)
				return
			}

			// 启动解码过程
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3解码失败: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("音乐播放取消, err: %v, cause: %v", ctx.Err(), context.Cause(ctx))
				return
			default:
				log.Infof("音乐播放完成耗时: %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("当前仅支持MP3格式的流式播放，传入格式: %s", audioFormat)
		}
	}()

	return outputChan, nil
}

type subsonicStreamDownloader struct {
	ctx            context.Context
	client         *http.Client
	url            string
	headers        http.Header
	writer         *io.PipeWriter
	totalExpected  int64
	bytesRead      int64
	retries        int
	maxRetries     int
	supportsRanges bool
	currentResp    *http.Response
}

func newSubsonicStreamDownloader(ctx context.Context, client *http.Client, url string, headers http.Header, resp *http.Response, writer *io.PipeWriter, supportsRange bool) *subsonicStreamDownloader {
	return &subsonicStreamDownloader{
		ctx:            ctx,
		client:         client,
		url:            url,
		headers:        cloneHeader(headers),
		writer:         writer,
		totalExpected:  determineTotalLength(resp),
		bytesRead:      0,
		retries:        0,
		maxRetries:     2,
		supportsRanges: supportsRange,
		currentResp:    resp,
	}
}

func (d *subsonicStreamDownloader) run() {
	var lastErr error
	defer func() {
		if lastErr != nil && !errors.Is(lastErr, io.EOF) && !errors.Is(lastErr, context.Canceled) && !errors.Is(lastErr, io.ErrClosedPipe) {
			d.writer.CloseWithError(lastErr)
		} else {
			d.writer.Close()
		}
	}()

	for {
		if err := d.streamCurrentResponse(); err != nil {
			lastErr = err
		} else {
			lastErr = nil
		}

		if d.shouldResume() && d.retries < d.maxRetries && (lastErr == nil || isRecoverableStreamErr(lastErr)) {
			if lastErr != nil {
				log.Warnf("Subsonic流出现可恢复错误（%v），累计读取 %d/%d 字节，准备续传第%d次", lastErr, d.bytesRead, d.totalExpected, d.retries+1)
			} else {
				log.Warnf("Subsonic流提前结束（%d/%d字节），尝试第%d次Range续传", d.bytesRead, d.totalExpected, d.retries+1)
			}
			resp, err := d.openRangeResponse()
			if err != nil {
				lastErr = err
				break
			}
			d.currentResp = resp
			continue
		}
		break
	}

	if errors.Is(lastErr, io.EOF) || errors.Is(lastErr, io.ErrClosedPipe) {
		lastErr = nil
	}

	if lastErr == nil && d.totalExpected > 0 && d.bytesRead < d.totalExpected {
		lastErr = fmt.Errorf("Subsonic流长度不足，读取 %d / %d 字节", d.bytesRead, d.totalExpected)
	}

	switch {
	case lastErr == nil:
		log.Infof("Subsonic流下载完成，读取 %d 字节，续传次数 %d", d.bytesRead, d.retries)
	case errors.Is(lastErr, context.Canceled):
		log.Debugf("Subsonic流下载被取消，已读取 %d 字节, cause: %v", d.bytesRead, context.Cause(d.ctx))
	default:
		log.Warnf("Subsonic流下载失败，已读取 %d 字节: %v", d.bytesRead, lastErr)
	}
}

func (d *subsonicStreamDownloader) streamCurrentResponse() error {
	if d.currentResp == nil {
		return io.EOF
	}
	defer d.currentResp.Body.Close()

	if d.totalExpected <= 0 {
		if total := determineTotalLength(d.currentResp); total > 0 {
			d.totalExpected = total
		}
	}

	start := time.Now()
	buf := make([]byte, 64*1024)
	for {
		if err := d.ctx.Err(); err != nil {
			return err
		}
		n, err := d.currentResp.Body.Read(buf)
		if n > 0 {
			d.bytesRead += int64(n)
			if _, writeErr := d.writer.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Infof("Subsonic流读取完成，本次耗时 %s, bytes=%d", time.Since(start), d.bytesRead)
				return nil
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				log.Warnf("Subsonic流读取出现 unexpected EOF，本次耗时 %s, 已读 %d 字节，尝试续传", time.Since(start), d.bytesRead)
				return nil
			}
			log.Warnf("Subsonic流读取错误，本次耗时 %s, 已读 %d 字节: %v", time.Since(start), d.bytesRead, err)
			return err
		}
	}
}

func (d *subsonicStreamDownloader) shouldResume() bool {
	return d.supportsRanges && d.totalExpected > 0 && d.bytesRead < d.totalExpected
}

func (d *subsonicStreamDownloader) openRangeResponse() (*http.Response, error) {
	d.retries++
	rangeStart := d.bytesRead
	log.Debugf("Subsonic流准备续传，从字节 %d 开始", rangeStart)

	req, err := http.NewRequestWithContext(d.ctx, "GET", d.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = cloneHeader(d.headers)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", rangeStart))

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("Range续传失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	d.supportsRanges = true
	if total := determineTotalLength(resp); total > 0 {
		d.totalExpected = total
	}

	log.Infof("Subsonic流续传成功：Range=%d-, 状态码=%d, 剩余长度=%d", rangeStart, resp.StatusCode, resp.ContentLength)
	return resp, nil
}

func isRecoverableStreamErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return make(http.Header)
	}
	copied := make(http.Header, len(h))
	for k, v := range h {
		copied[k] = append([]string(nil), v...)
	}
	return copied
}

func determineTotalLength(resp *http.Response) int64 {
	if resp == nil {
		return -1
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if total := parseTotalFromContentRange(cr); total > 0 {
			return total
		}
	}
	return resp.ContentLength
}

func parseTotalFromContentRange(raw string) int64 {
	if raw == "" {
		return -1
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return -1
	}
	total := strings.TrimSpace(parts[1])
	if total == "*" || total == "" {
		return -1
	}
	value, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return -1
	}
	return value
}

func PlayMusicFromPipe(ctx context.Context, pipeReader *io.PipeReader, sampleRate int, frameDuration int, audioFormat string) (outputChan chan []byte, err error) {
	// 参数校验和默认值设置
	if frameDuration <= 0 {
		frameDuration = 20 // 默认20ms帧时长
	}
	if audioFormat == "" {
		audioFormat = "mp3" // 默认MP3格式
	}

	// 添加调试信息
	log.Debugf("PlayMusicFromPipe: 采样率=%d, 帧时长=%dms, 格式=%s",
		sampleRate, frameDuration, audioFormat)

	startTs := time.Now().UnixMilli()

	// 创建输出通道
	outputChan = make(chan []byte, 100)

	// 启动goroutine处理流式响应
	go func() {
		// 根据音频格式处理流式响应
		if audioFormat == "mp3" {
			// 创建 MP3 解码器，传入 context 而不是 done 通道
			mp3Decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, audioFormat, sampleRate)
			if err != nil {
				log.Errorf("创建MP3解码器失败: %v", err)
				return
			}

			// 启动解码过程
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3解码失败: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("音乐播放取消, err: %v, cause: %v", ctx.Err(), context.Cause(ctx))
				return
			default:
				log.Infof("音乐播放完成耗时: %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("当前仅支持MP3格式的流式播放，传入格式: %s", audioFormat)
		}
	}()

	return outputChan, nil
}
