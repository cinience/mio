package functions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend-server/internal/domain/music"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/delucks/go-subsonic"
)

const (
	PlayMusicFunctionName        = "music"
	operationSearchLibrary       = "search_library"
	operationControlPlayback     = "control_playback"
	maxSearchResultCount     int = 50
	defaultSearchResultCount int = 10
	defaultSongSearchLimit   int = 5
)

var songQueryCleaner = strings.NewReplacer(
	"　", "",
	" ", "",
	"《", "",
	"》", "",
	"\"", "",
	"'", "",
	"-", "",
	"·", "",
)

// PlayMusicFunctionDesc defines the function description for LLM
var PlayMusicFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        PlayMusicFunctionName,
		"description": "基于 Subsonic 媒体库进行音乐搜索与播放控制，支持搜索曲目、播放、暂停、切换下一首与加入播放队列。当用户询问有哪些节目/音乐/音频或希望获取推荐时，可调用 search_library 并留空 query 以返回推荐列表。",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "操作类型: search_library(搜索媒体) 或 control_playback(控制播放)。默认 control_playback。",
					"enum":        []string{operationSearchLibrary, operationControlPlayback},
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "媒体搜索关键词，仅在 search_library 操作下使用。留空可返回系统推荐节目/音频列表。",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "搜索返回的最大结果数量，范围 1-50，默认 10。",
					"minimum":     1,
					"maximum":     maxSearchResultCount,
					"default":     defaultSearchResultCount,
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "播放控制指令，例如：\"播放周杰伦\"、\"暂停\"、\"下一首\"、\"加入播放队列 舞娘\"。",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "向后兼容字段。当未提供 command 时，可使用 name 表示播放目标。",
				},
				"song": map[string]interface{}{
					"type":        "string",
					"description": "歌曲/节目名称。若提供 artist，将优先用 song + artist 精准检索。",
				},
				"artist": map[string]interface{}{
					"type":        "string",
					"description": "歌手/作者名称，可与 song 配对提升命中率。",
				},
			},
		},
	},
}

// PlayMusicFunction 实现基于 Subsonic 的音乐功能
type PlayMusicFunction struct {
	playbackStates sync.Map

	httpClient *http.Client
	httpOnce   sync.Once
}

type subsonicConfig struct {
	BaseURL     string
	APIBase     string
	Username    string
	Password    string
	ExtraParams url.Values
}

type songInfo struct {
	ID       string
	Title    string
	Artist   string
	Album    string
	Duration int
	CoverArt string
}

type subsonicClient struct {
	cfg        *subsonicConfig
	httpClient *http.Client
}

type subsonicEnvelope struct {
	Response *subsonicResponse `json:"subsonic-response"`
}

type subsonicResponse struct {
	Status        string                 `json:"status"`
	Version       string                 `json:"version"`
	Error         *subsonicError         `json:"error"`
	SearchResult3 *subsonicSongContainer `json:"searchResult3"`
	RandomSongs   *subsonicSongContainer `json:"randomSongs"`
}

type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type subsonicSongContainer struct {
	Songs subsonicSongList `json:"song"`
}

type subsonicSongList struct {
	Items []subsonicSong
}

type subsonicSong struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int    `json:"duration"`
	CoverArt string `json:"coverArt"`
}

func (l *subsonicSongList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || len(data) == 0 {
		return nil
	}
	if data[0] == '[' {
		return json.Unmarshal(data, &l.Items)
	}
	var single subsonicSong
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	l.Items = []subsonicSong{single}
	return nil
}

func (s *subsonicSong) UnmarshalJSON(data []byte) error {
	type alias subsonicSong
	aux := struct {
		Duration json.Number `json:"duration"`
		*alias
	}{
		alias: (*alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Duration != "" {
		if v, err := aux.Duration.Int64(); err == nil {
			s.Duration = int(v)
		}
	}
	return nil
}

func (c *subsonicSongContainer) list() []subsonicSong {
	if c == nil {
		return nil
	}
	return c.Songs.Items
}

func newSubsonicClient(ctx context.Context, cfg *subsonicConfig, httpClient *http.Client) (*subsonicClient, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &subsonicClient{
		cfg:        cfg,
		httpClient: httpClient,
	}
	if err := client.ping(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *subsonicClient) ping(ctx context.Context) error {
	_, err := c.request(ctx, "ping", nil)
	return err
}

func (c *subsonicClient) searchSongs(ctx context.Context, query string, limit int) ([]subsonicSong, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("songCount", strconv.Itoa(limit))
	resp, err := c.request(ctx, "search3", params)
	if err != nil {
		return nil, err
	}
	return resp.SearchResult3.list(), nil
}

func (c *subsonicClient) randomSong(ctx context.Context) (*subsonicSong, error) {
	params := url.Values{}
	params.Set("size", "1")
	resp, err := c.request(ctx, "getRandomSongs", params)
	if err != nil {
		return nil, err
	}
	songs := resp.RandomSongs.list()
	if len(songs) == 0 {
		return nil, fmt.Errorf("未获取到随机歌曲")
	}
	return &songs[0], nil
}

func (cfg *subsonicConfig) applyExtraParams(values url.Values) {
	if cfg == nil || len(cfg.ExtraParams) == 0 {
		return
	}
	for key, extras := range cfg.ExtraParams {
		for _, val := range extras {
			values.Add(key, val)
		}
	}
}

func (c *subsonicClient) streamURL(songID string) string {
	base := strings.TrimRight(c.cfg.APIBase, "/")
	values := url.Values{}
	values.Set("u", c.cfg.Username)
	values.Set("p", c.cfg.Password)
	values.Set("id", songID)
	values.Set("format", "mp3")
	values.Set("c", "xiaozhi-server")
	values.Set("v", "1.16.1")
	c.cfg.applyExtraParams(values)
	return fmt.Sprintf("%s/stream.view?%s", base, values.Encode())
}

func (c *subsonicClient) request(ctx context.Context, endpoint string, params url.Values) (*subsonicResponse, error) {
	req, err := c.newRequest(ctx, endpoint, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Subsonic HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	envelope := subsonicEnvelope{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析 Subsonic 响应失败: %w", err)
	}

	if envelope.Response == nil {
		return nil, fmt.Errorf("Subsonic 响应无效")
	}
	if envelope.Response.Error != nil {
		return nil, fmt.Errorf("Subsonic 错误 #%d: %s", envelope.Response.Error.Code, envelope.Response.Error.Message)
	}
	return envelope.Response, nil
}

func (c *subsonicClient) newRequest(ctx context.Context, endpoint string, params url.Values) (*http.Request, error) {
	base := strings.TrimRight(c.cfg.APIBase, "/")
	fullEndpoint := fmt.Sprintf("%s/%s.view", base, strings.TrimLeft(endpoint, "/"))

	reqURL, err := url.Parse(fullEndpoint)
	if err != nil {
		return nil, err
	}

	query := reqURL.Query()
	query.Set("u", c.cfg.Username)
	query.Set("p", c.cfg.Password)
	query.Set("v", "1.16.1")
	query.Set("c", "xiaozhi-server")
	query.Set("f", "json")
	c.cfg.applyExtraParams(query)
	for key, values := range params {
		for _, val := range values {
			query.Add(key, val)
		}
	}
	reqURL.RawQuery = query.Encode()

	return http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
}

func (s *songInfo) displayTitle() string {
	if s == nil {
		return ""
	}
	if s.Artist != "" && s.Title != "" {
		return fmt.Sprintf("%s - %s", s.Artist, s.Title)
	}
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

func (s *songInfo) toMap() map[string]interface{} {
	if s == nil {
		return nil
	}
	result := map[string]interface{}{
		"id":       s.ID,
		"title":    s.Title,
		"artist":   s.Artist,
		"album":    s.Album,
		"duration": s.Duration,
	}
	if s.CoverArt != "" {
		result["cover_art"] = s.CoverArt
	}
	return result
}

type playbackState struct {
	mu        sync.Mutex
	current   *songInfo
	queue     []*songInfo
	cancel    context.CancelCauseFunc
	manual    bool
	cfg       *subsonicConfig
	seq       int64
	activeSeq int64
}

func (ps *playbackState) beginPlayback(song *songInfo, cancel context.CancelCauseFunc, cfg *subsonicConfig) int64 {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.seq++
	ps.activeSeq = ps.seq
	ps.manual = false
	ps.current = song
	ps.cancel = cancel
	if cfg != nil {
		cfgCopy := *cfg
		ps.cfg = &cfgCopy
	}
	return ps.activeSeq
}

func (ps *playbackState) finishPlayback(seq int64) *songInfo {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if seq != ps.activeSeq {
		return nil
	}
	ps.cancel = nil
	ps.current = nil

	if ps.manual {
		ps.manual = false
		return nil
	}

	if len(ps.queue) == 0 {
		return nil
	}
	next := ps.queue[0]
	ps.queue = ps.queue[1:]
	return next
}

func (ps *playbackState) stop(manual bool) bool {
	ps.mu.Lock()
	cancel := ps.cancel
	hasCurrent := ps.current != nil
	if manual {
		ps.manual = true
	}
	ps.cancel = nil
	ps.current = nil
	ps.mu.Unlock()

	if cancel != nil {
		if manual {
			cancel(errors.New("manual stop"))
		} else {
			cancel(errors.New("playback replaced"))
		}
	}
	return hasCurrent
}

func (ps *playbackState) resetIfSeq(seq int64) {
	ps.mu.Lock()
	if seq == ps.activeSeq {
		ps.cancel = nil
		ps.current = nil
		ps.manual = false
	}
	ps.mu.Unlock()
}

func (ps *playbackState) enqueue(song *songInfo) {
	if song == nil {
		return
	}
	ps.mu.Lock()
	ps.queue = append(ps.queue, song)
	ps.mu.Unlock()
}

func (ps *playbackState) popNext() *songInfo {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if len(ps.queue) == 0 {
		return nil
	}
	next := ps.queue[0]
	ps.queue = ps.queue[1:]
	return next
}

func (ps *playbackState) queueLength() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.queue)
}

func (ps *playbackState) hasCurrent() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.current != nil
}

func (ps *playbackState) getConfig() *subsonicConfig {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.cfg == nil {
		return nil
	}
	cfgCopy := *ps.cfg
	return &cfgCopy
}

// NewPlayMusicFunction creates a new music function bound to Subsonic
func NewPlayMusicFunction() *PlayMusicFunction {
	return &PlayMusicFunction{}
}

// Execute implements the function execution
func (f *PlayMusicFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	if conn == nil {
		return types.NewActionResponse(types.ActionError, "缺少会话连接", nil), fmt.Errorf("connection is nil")
	}

	logger := conn.GetLogger()
	if logger == nil {
		logger = log.GetLogger()
	}

	cfg, err := f.loadSubsonicConfig(conn)
	if err != nil {
		logger.Warn("Subsonic 配置不可用", "error", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, err.Error()), nil
	}

	operation := strings.TrimSpace(strings.ToLower(safeStringArg(args, "operation")))
	if operation == "" {
		operation = operationControlPlayback
	}

	switch operation {
	case operationSearchLibrary:
		query := strings.TrimSpace(safeStringArg(args, "query"))
		if query == "" {
			// 向后兼容 Name 字段
			query = strings.TrimSpace(safeStringArg(args, "name"))
		}
		limit := clampInt(intArgWithDefault(args, "limit", defaultSearchResultCount), 1, maxSearchResultCount)
		return f.handleSearchLibrary(ctx, cfg, query, limit, logger)

	case operationControlPlayback:
		command := strings.TrimSpace(safeStringArg(args, "command"))
		if command == "" {
			command = strings.TrimSpace(safeStringArg(args, "name"))
		}
		return f.handleControlPlayback(ctx, conn, cfg, command, args, logger)

	default:
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("不支持的操作类型: %s", operation)), nil
	}
}

func (f *PlayMusicFunction) handleSearchLibrary(ctx context.Context, cfg *subsonicConfig, query string, limit int, logger *slog.Logger) (*types.ActionResponse, error) {
	query = strings.TrimSpace(query)

	client, err := f.newSubsonicClient(cfg)
	if err != nil {
		logger.Warn("创建 Subsonic 客户端失败", "error", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("Subsonic 认证失败: %v", err)), nil
	}

	if shouldRecommendRandom(query) {
		return f.recommendAvailableSongs(ctx, client, limit, logger)
	}

	if query == "" {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "请提供搜索关键词"), nil
	}

	params := map[string]string{
		"songCount": strconv.Itoa(limit),
	}

	result, err := client.Search3(query, params)
	if err != nil {
		logger.Warn("Subsonic 搜索失败", "error", err, "query", query)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("搜索失败: %v", err)), nil
	}

	if result == nil || len(result.Song) == 0 {
		return types.NewActionResponse(types.ActionDirectResponse, map[string]interface{}{
			"query": query,
			"songs": []interface{}{},
			"count": 0,
		}, fmt.Sprintf("没有找到与 \"%s\" 匹配的歌曲", query)), nil
	}

	songs := make([]map[string]interface{}, 0, len(result.Song))
	for _, song := range result.Song {
		info := convertSong(song)
		songs = append(songs, info.toMap())
	}

	response := map[string]interface{}{
		"query":        query,
		"songs":        songs,
		"count":        len(songs),
		"instructions": "请根据 songs 列表逐条向用户介绍可播放的节目/音乐，并包含标题与歌手信息。",
	}

	summary := summarizeSongTitles(songs, 3)
	message := fmt.Sprintf("找到 %d 首与 \"%s\" 相关的曲目", len(songs), query)
	if summary != "" {
		message = fmt.Sprintf("找到 %d 首与 \"%s\" 相关的曲目，例如：%s", len(songs), query, summary)
	}
	return types.NewActionResponse(types.ActionDirectResponse, response, message), nil
}

func (f *PlayMusicFunction) recommendAvailableSongs(ctx context.Context, client *subsonic.Client, limit int, logger *slog.Logger) (*types.ActionResponse, error) {
	size := clampInt(limit, 1, maxSearchResultCount)
	if size < 3 {
		size += 2
		if size > maxSearchResultCount {
			size = maxSearchResultCount
		}
	}
	params := map[string]string{
		"size":      strconv.Itoa(size),
		"songCount": strconv.Itoa(size),
	}
	songs, err := client.GetRandomSongs(params)
	if err != nil {
		logger.Warn("Subsonic 获取随机节目失败", "error", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, "暂时无法获取可播放的节目，请稍后重试"), nil
	}
	if len(songs) == 0 {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前没有可推荐的节目"), nil
	}
	entries := make([]map[string]interface{}, 0, len(songs))
	for _, song := range songs {
		info := convertSong(song)
		if info == nil {
			continue
		}
		entries = append(entries, info.toMap())
	}
	if len(entries) == 0 {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前没有可推荐的节目"), nil
	}
	response := map[string]interface{}{
		"songs":                entries,
		"count":                len(entries),
		"recommendation":       true,
		"recommendationSource": "subsonic.random",
		"instructions":         "请根据 songs 列表逐条向用户介绍可播放的节目/音乐，并包含标题与歌手信息。",
	}
	summary := summarizeSongTitles(entries, 3)
	message := fmt.Sprintf("这里有 %d 个可以直接播放的节目/音乐，随时可以点播", len(entries))
	if summary != "" {
		message = fmt.Sprintf("这里有 %d 个可以直接播放的节目/音乐，例如：%s。随时可以点播", len(entries), summary)
	}
	return types.NewActionResponse(types.ActionDirectResponse, response, message), nil
}

func summarizeSongTitles(entries []map[string]interface{}, limit int) string {
	if len(entries) == 0 || limit <= 0 {
		return ""
	}
	names := make([]string, 0, min(limit, len(entries)))
	for _, entry := range entries {
		if len(names) >= limit {
			break
		}
		title := strings.TrimSpace(fmt.Sprintf("%v", entry["title"]))
		artist := strings.TrimSpace(fmt.Sprintf("%v", entry["artist"]))
		name := title
		if title != "" && artist != "" {
			name = fmt.Sprintf("%s - %s", artist, title)
		} else if title == "" && artist != "" {
			name = artist
		} else if title == "" {
			name = strings.TrimSpace(fmt.Sprintf("%v", entry["id"]))
		}
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return strings.Join(names, "、")
}

func shouldRecommendRandom(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	keywords := []string{
		"有哪些节目",
		"有什么节目",
		"推荐节目",
		"节目推荐",
		"有哪些音乐",
		"有什么音乐",
		"推荐音乐",
		"音乐推荐",
		"有哪些音频",
		"有什么音频",
		"推荐音频",
		"能听啥",
		"listen",
		"playlist",
		"what can i play",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func (f *PlayMusicFunction) handleControlPlayback(ctx context.Context, conn types.Connection, cfg *subsonicConfig, rawCommand string, args map[string]interface{}, logger *slog.Logger) (*types.ActionResponse, error) {
	song := strings.TrimSpace(safeStringArg(args, "song"))
	artist := strings.TrimSpace(safeStringArg(args, "artist"))
	command := sanitizeCommand(rawCommand)
	if command == "" {
		if name := strings.TrimSpace(safeStringArg(args, "name")); name != "" {
			command = "播放 " + name
		}
	}
	if command == "" && song != "" {
		command = "播放 " + song
	}
	if command == "" {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "请提供播放指令，例如“播放周杰伦”或“暂停”"), nil
	}

	state := f.getPlaybackState(conn)

	switch {
	case strings.HasPrefix(command, "播放"):
		target := strings.TrimSpace(strings.TrimPrefix(command, "播放"))
		return f.startPlayCommand(ctx, conn, cfg, state, target, song, artist, logger)

	case strings.Contains(command, "暂停"):
		if state.stop(true) {
			logger.Info("收到暂停指令，停止当前播放")
			return types.NewActionResponse(types.ActionDirectResponse, nil, "播放已暂停"), nil
		}
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前没有正在播放的音乐"), nil

	case strings.Contains(command, "下一首"):
		return f.playNext(ctx, conn, cfg, state, logger)

	case strings.HasPrefix(command, "加入播放队列"):
		target := strings.TrimSpace(strings.TrimPrefix(command, "加入播放队列"))
		if target == "" {
			target = strings.TrimSpace(safeStringArg(args, "name"))
		}
		return f.enqueueTrack(ctx, conn, cfg, state, target, song, artist, logger)

	default:
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("无法识别的播放指令: %s", rawCommand)), nil
	}
}

func (f *PlayMusicFunction) startPlayCommand(ctx context.Context, conn types.Connection, cfg *subsonicConfig, state *playbackState, target, songName, artist string, logger *slog.Logger) (*types.ActionResponse, error) {
	client, err := f.newSubsonicClient(cfg)
	if err != nil {
		logger.Warn("创建 Subsonic 客户端失败", "error", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("Subsonic 认证失败: %v", err)), nil
	}

	var song *songInfo
	if target == "" || target == "*" || strings.Contains(target, "随机") {
		randomSongs, err := client.GetRandomSongs(map[string]string{"songCount": "1"})
		if err != nil || len(randomSongs) == 0 {
			logger.Warn("获取随机歌曲失败", "error", err)
			return types.NewActionResponse(types.ActionDirectResponse, nil, "未能获取随机歌曲，请稍后重试"), nil
		}
		song = convertSong(randomSongs[0])
	} else {
		var searchErr error
		song, searchErr = f.findBestMatchingSong(client, target, songName, artist, logger)
		if searchErr != nil {
			return types.NewActionResponse(types.ActionDirectResponse, nil, searchErr.Error()), nil
		}
	}

	if song == nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "无法解析到歌曲信息"), nil
	}

	if err := f.startPlayback(ctx, conn, cfg, state, song, logger); err != nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("播放失败: %v", err)), nil
	}

	return types.NewActionResponse(types.ActionDirectResponse, map[string]interface{}{
		"now_playing": song.toMap(),
	}, fmt.Sprintf("正在播放：%s", song.displayTitle())), nil
}

func (f *PlayMusicFunction) enqueueTrack(ctx context.Context, conn types.Connection, cfg *subsonicConfig, state *playbackState, target, songName, artist string, logger *slog.Logger) (*types.ActionResponse, error) {
	target = strings.TrimSpace(target)
	if target == "" && songName == "" {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "加入播放队列需要提供歌曲名称"), nil
	}

	client, err := f.newSubsonicClient(cfg)
	if err != nil {
		logger.Warn("创建 Subsonic 客户端失败", "error", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("Subsonic 认证失败: %v", err)), nil
	}

	song, searchErr := f.findBestMatchingSong(client, target, songName, artist, logger)
	if searchErr != nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, searchErr.Error()), nil
	}

	state.enqueue(song)

	if state.hasCurrent() {
		return types.NewActionResponse(types.ActionDirectResponse, map[string]interface{}{
			"queued": song.toMap(),
			"queue": map[string]int{
				"length": state.queueLength(),
			},
		}, fmt.Sprintf("已加入播放队列：%s", song.displayTitle())), nil
	}

	next := state.popNext()
	if next == nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "播放队列为空"), nil
	}

	if err := f.startPlayback(ctx, conn, cfg, state, next, logger); err != nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("播放失败: %v", err)), nil
	}

	return types.NewActionResponse(types.ActionDirectResponse, map[string]interface{}{
		"now_playing": next.toMap(),
	}, fmt.Sprintf("正在播放：%s", next.displayTitle())), nil
}

func (f *PlayMusicFunction) findBestMatchingSong(client *subsonic.Client, target, songName, artist string, logger *slog.Logger) (*songInfo, error) {
	queries := buildSongSearchQueries(target, songName, artist)
	if len(queries) == 0 {
		return nil, fmt.Errorf("未找到歌曲：%s", describeSongTarget(target, songName, artist))
	}

	params := map[string]string{
		"songCount": strconv.Itoa(defaultSongSearchLimit),
	}

	var fallback *songInfo
	for _, query := range queries {
		searchResult, err := client.Search3(query, params)
		if err != nil {
			logger.Warn("歌曲搜索失败", "error", err, "target", query)
			continue
		}
		if searchResult == nil || len(searchResult.Song) == 0 {
			continue
		}
		for _, candidate := range searchResult.Song {
			info := convertSong(candidate)
			if matchesSongCriteria(info, songName, artist) {
				return info, nil
			}
			if fallback == nil {
				fallback = info
			}
		}
	}

	if fallback != nil {
		return fallback, nil
	}

	return nil, fmt.Errorf("未找到歌曲：%s", describeSongTarget(target, songName, artist))
}

func buildSongSearchQueries(target, songName, artist string) []string {
	added := map[string]struct{}{}
	var queries []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := added[value]; ok {
			return
		}
		added[value] = struct{}{}
		queries = append(queries, value)
	}

	if songName != "" && artist != "" {
		add(fmt.Sprintf("%s %s", artist, songName))
		add(fmt.Sprintf("%s %s", songName, artist))
	}
	add(songName)
	add(target)
	if artist != "" {
		add(artist)
	}
	return queries
}

func describeSongTarget(target, songName, artist string) string {
	switch {
	case songName != "" && artist != "":
		return fmt.Sprintf("%s - %s", artist, songName)
	case songName != "":
		return songName
	case target != "":
		return target
	case artist != "":
		return artist
	default:
		return "目标歌曲"
	}
}

func matchesSongCriteria(info *songInfo, songName, artist string) bool {
	if info == nil {
		return false
	}
	if songName != "" && !strings.Contains(normalizeSongText(info.Title), normalizeSongText(songName)) {
		return false
	}
	if artist != "" && !strings.Contains(normalizeSongText(info.Artist), normalizeSongText(artist)) {
		return false
	}
	return true
}

func normalizeSongText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return songQueryCleaner.Replace(value)
}

func (f *PlayMusicFunction) playNext(ctx context.Context, conn types.Connection, cfg *subsonicConfig, state *playbackState, logger *slog.Logger) (*types.ActionResponse, error) {
	next := state.popNext()
	if next == nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "播放队列中没有更多歌曲了"), nil
	}

	if state.stop(true) {
		logger.Info("收到下一首指令，停止当前播放")
	}

	if err := f.startPlayback(ctx, conn, cfg, state, next, logger); err != nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("播放下一首失败: %v", err)), nil
	}

	return types.NewActionResponse(types.ActionDirectResponse, map[string]interface{}{
		"now_playing": next.toMap(),
	}, fmt.Sprintf("正在播放：%s", next.displayTitle())), nil
}

func (f *PlayMusicFunction) startPlayback(ctx context.Context, conn types.Connection, cfg *subsonicConfig, state *playbackState, song *songInfo, logger *slog.Logger) error {
	if song == nil {
		return fmt.Errorf("歌曲信息为空，无法播放")
	}
	if cfg == nil {
		return fmt.Errorf("Subsonic 配置缺失")
	}

	if state.stop(false) {
		logger.Info("终止上一首播放以开始新曲目", "next_song", song.displayTitle())
	}

	streamURL := buildStreamURL(cfg, song.ID)

	sampleRate, frameDuration, downstreamFormat := conn.GetAudioFormat()
	if sampleRate <= 0 {
		sampleRate = 24000
	}
	if frameDuration <= 0 {
		frameDuration = 20
	}
	streamFormat := "mp3"
	if downstreamFormat == "" {
		downstreamFormat = "opus"
	}
	if strings.ToLower(downstreamFormat) == "opus" {
		logger.Debug("终端请求 Opus 下行音频，自动将 MP3 流实时转码")
	} else {
		logger.Debug("终端请求非 Opus 音频，将按默认方式回传", "format", downstreamFormat)
	}

	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	audioCtx, cancel := context.WithCancelCause(baseCtx)

	audioChan, err := music.PlayMusicStream(audioCtx, streamURL, sampleRate, frameDuration, streamFormat)
	if err != nil {
		cancel(errors.New("stream init failed"))
		logger.Warn("播放音乐流失败", "error", err, "url", streamURL)
		return fmt.Errorf("音乐流读取失败: %w", err)
	}

	seq := state.beginPlayback(song, cancel, cfg)

	done, err := conn.StreamAudio(audioCtx, fmt.Sprintf("正在播放音乐：%s", song.displayTitle()), audioChan)
	if err != nil {
		cancel(errors.New("downstream stream init failed"))
		state.resetIfSeq(seq)
		logger.Warn("向终端推送音频失败", "error", err)
		return fmt.Errorf("发送音频失败: %w", err)
	}

	go func(expectedSeq int64, currentSong *songInfo) {
		var streamErr error
		if done != nil {
			streamErr = <-done
		}
		if streamErr != nil {
			logger.Warn("播放过程中出现异常", "error", streamErr, "song", currentSong.displayTitle())
		}
		next := state.finishPlayback(expectedSeq)
		if next == nil {
			return
		}
		if err := f.startPlayback(context.Background(), conn, cfg, state, next, logger); err != nil {
			logger.Warn("自动播放下一首失败", "error", err, "song", next.displayTitle())
		}
	}(seq, song)

	return nil
}

func (f *PlayMusicFunction) loadSubsonicConfig(conn types.Connection) (*subsonicConfig, error) {
	metadata := conn.GetMetadata()
	if metadata == nil {
		return nil, fmt.Errorf("未获取到设备元数据，请检查设备配置")
	}

	rawURL, ok := metadata["SubsonicURL"]
	if !ok || strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("设备未配置 SubsonicURL，请在管理端补全")
	}

	return parseSubsonicURL(rawURL)
}

func (f *PlayMusicFunction) newSubsonicClient(cfg *subsonicConfig) (*subsonic.Client, error) {
	client := &subsonic.Client{
		Client:       f.getHTTPClient(),
		BaseUrl:      cfg.BaseURL,
		User:         cfg.Username,
		ClientName:   "xiaozhi-server",
		PasswordAuth: true,
	}
	if err := client.Authenticate(cfg.Password); err != nil {
		return nil, err
	}
	return client, nil
}

func (f *PlayMusicFunction) getHTTPClient() *http.Client {
	f.httpOnce.Do(func() {
		f.httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	})
	return f.httpClient
}

func (f *PlayMusicFunction) getPlaybackState(conn types.Connection) *playbackState {
	key := strings.TrimSpace(conn.GetDeviceID())
	if key == "" {
		key = strings.TrimSpace(conn.GetRemoteAddr())
	}
	if key == "" {
		key = "default"
	}

	if value, ok := f.playbackStates.Load(key); ok {
		return value.(*playbackState)
	}

	state := &playbackState{}
	actual, _ := f.playbackStates.LoadOrStore(key, state)
	return actual.(*playbackState)
}

func parseSubsonicURL(raw string) (*subsonicConfig, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 SubsonicURL 失败: %w", err)
	}

	query := parsed.Query()
	username := query.Get("username")
	if username == "" {
		username = query.Get("u")
	}
	password := query.Get("password")
	extraParams := url.Values{}
	for key, values := range query {
		lowerKey := strings.ToLower(key)
		if lowerKey == "username" || lowerKey == "u" || lowerKey == "user" || lowerKey == "p" || lowerKey == "password" {
			continue
		}
		for _, v := range values {
			extraParams.Add(key, v)
		}
	}

	if parsed.User != nil {
		if u := parsed.User.Username(); u != "" {
			username = u
		}
		if p, has := parsed.User.Password(); has {
			password = p
		}
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil

	cleanPath := strings.TrimSuffix(parsed.Path, "/")
	if strings.HasSuffix(cleanPath, "/rest") {
		cleanPath = strings.TrimSuffix(cleanPath, "/rest")
	}
	parsed.Path = cleanPath

	baseURL := strings.TrimSuffix(parsed.String(), "/")
	apiBase := baseURL
	if !strings.HasSuffix(apiBase, "/rest") {
		apiBase = fmt.Sprintf("%s/rest", apiBase)
	}

	if baseURL == "" || username == "" || password == "" {
		return nil, fmt.Errorf("SubsonicURL 格式不完整，需要包含 baseURL、username、password")
	}

	return &subsonicConfig{
		BaseURL:     baseURL,
		APIBase:     strings.TrimRight(apiBase, "/"),
		Username:    username,
		Password:    password,
		ExtraParams: extraParams,
	}, nil
}

func buildStreamURL(cfg *subsonicConfig, songID string) string {
	base := strings.TrimRight(cfg.BaseURL, "/")
	values := url.Values{}
	values.Set("u", cfg.Username)
	values.Set("p", cfg.Password)
	values.Set("id", songID)
	values.Set("format", "mp3")
	values.Set("c", "xiaozhi-server")
	values.Set("v", "1.16.1")
	cfg.applyExtraParams(values)
	return fmt.Sprintf("%s/rest/stream?%s", base, values.Encode())
}

func convertSong(child *subsonic.Child) *songInfo {
	if child == nil {
		return nil
	}
	return &songInfo{
		ID:       child.ID,
		Title:    child.Title,
		Artist:   child.Artist,
		Album:    child.Album,
		Duration: child.Duration,
		CoverArt: child.CoverArt,
	}
}

func sanitizeCommand(command string) string {
	command = strings.ReplaceAll(command, "　", " ")
	return strings.TrimSpace(command)
}

func safeStringArg(args map[string]interface{}, key string) string {
	if val := getStringArg(args, key); val != "" {
		return val
	}
	if args == nil {
		return ""
	}
	if raw, ok := args[key]; ok {
		switch v := raw.(type) {
		case fmt.Stringer:
			return strings.TrimSpace(v.String())
		case []byte:
			return strings.TrimSpace(string(v))
		}
	}
	return ""
}

func intArgWithDefault(args map[string]interface{}, key string, defaultValue int) int {
	if val, ok := getIntArg(args, key); ok {
		return val
	}
	if args == nil {
		return defaultValue
	}
	if raw, ok := args[key]; ok {
		switch v := raw.(type) {
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		case fmt.Stringer:
			if n, err := strconv.Atoi(strings.TrimSpace(v.String())); err == nil {
				return n
			}
		}
	}
	return defaultValue
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetInfo returns the eino ToolInfo
func (f *PlayMusicFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(PlayMusicFunctionName, PlayMusicFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", PlayMusicFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *PlayMusicFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the function name
func (f *PlayMusicFunction) GetName() string {
	return PlayMusicFunctionName
}

// GetDescription returns the function description
func (f *PlayMusicFunction) GetDescription() interface{} {
	return PlayMusicFunctionDesc
}

// RegisterPlayMusicFunction registers the music function
func RegisterPlayMusicFunction() error {
	function := NewPlayMusicFunction()
	return tools.RegisterGlobalFunction(PlayMusicFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterPlayMusicFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", PlayMusicFunctionName, err)
	}
}
