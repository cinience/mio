package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	aioruntime "aio-server/pkg/runtime"

	goruntime "runtime"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"gopkg.in/yaml.v3"
)

// BuildTime 编译时间，通过 ldflags 在编译时注入
var BuildTime = "unknown"

// App wraps Wails lifecycle hooks and orchestrates the bundled services.
type App struct {
	ctx context.Context

	runtimeMu sync.Mutex
	runtime   *aioruntime.Runtime

	cfg aioruntime.Config

	services map[string]bool

	rootDir    string
	workDir    string
	managerURL string
}

var serviceAlias = initServiceAlias()

func initServiceAlias() map[string]string {
	alias := map[string]string{
		"backend": aioruntime.ServiceBackend,
		"manager": aioruntime.ServiceManager,
		"redis":   aioruntime.ServiceRedis,
	}
	if speechServiceEnabled() {
		alias["speech"] = aioruntime.ServiceSpeech
	}
	return alias
}

// NewApp creates a new App instance with default service selections.
func NewApp() *App {
	root := resolveProjectRoot()
	wd, err := os.Getwd()
	if err != nil {
		wd = root
	}
	cfg := aioruntime.Config{
		Version:             "desktop-1.0.0",
		BackendConfig:       filepath.Join(root, "backend-server", "config", "config.yaml"),
		ManagerConfig:       filepath.Join(root, "manager-server", "config", "config.yaml"),
		DeriveRedisPassword: true,
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		RestartBackoff:      15 * time.Second,
	}

	if speechServiceEnabled() {
		cfg.SpeechConfig = defaultSpeechConfigPath(root)
	}

	serviceSelections := map[string]bool{
		aioruntime.ServiceBackend: true,
		aioruntime.ServiceManager: true,
		aioruntime.ServiceRedis:   true,
	}
	if speechServiceEnabled() && strings.TrimSpace(cfg.SpeechConfig) != "" {
		serviceSelections[aioruntime.ServiceSpeech] = true
	}

	app := &App{
		cfg:      cfg,
		rootDir:  root,
		workDir:  wd,
		services: serviceSelections,
	}
	app.managerURL = app.computeManagerURL()

	return app
}

// startup is called when the app starts. The context is saved so we can call runtime methods.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	log.Println("小智桌面端应用启动中...")
	go a.startBackendServices()
}

// startBackendServices bootstraps the selected backend services.
func (a *App) startBackendServices() {
	rt, err := a.ensureRuntime()
	if err != nil {
		wailsRuntime.LogError(a.ctx, fmt.Sprintf("初始化服务运行时失败: %v", err))
		return
	}

	services := a.selectedServices()
	if len(services) == 0 {
		wailsRuntime.LogInfo(a.ctx, "没有需要启动的服务")
		return
	}

	if err := rt.Start(a.ctx, services...); err != nil {
		wailsRuntime.LogError(a.ctx, fmt.Sprintf("启动服务失败: %v", err))
		return
	}

	wailsRuntime.LogInfo(a.ctx, fmt.Sprintf("服务启动完成: %s", strings.Join(services, ", ")))
}

// shutdown stops all managed services.
func (a *App) shutdown(ctx context.Context) {
	wailsRuntime.LogInfo(a.ctx, "正在关闭服务...")

	a.runtimeMu.Lock()
	rt := a.runtime
	a.runtimeMu.Unlock()

	if rt == nil {
		wailsRuntime.LogInfo(a.ctx, "没有正在运行的服务")
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rt.Shutdown(shutdownCtx); err != nil {
		wailsRuntime.LogError(a.ctx, fmt.Sprintf("关闭服务失败: %v", err))
	} else {
		wailsRuntime.LogInfo(a.ctx, "所有服务已关闭")
	}

	a.runtimeMu.Lock()
	a.runtime = nil
	a.runtimeMu.Unlock()
}

// GetServiceStatus returns current service running state.
func (a *App) GetServiceStatus() map[string]interface{} {
	status := make(map[string]interface{}, len(serviceAlias))
	for alias := range serviceAlias {
		status[alias] = false
	}

	rt := a.currentRuntime()
	if rt == nil {
		return status
	}

	state := rt.Status()
	for alias, runtimeKey := range serviceAlias {
		status[alias] = runtimeServiceActive(state, runtimeKey)
	}
	return status
}

// StartService starts the requested service.
func (a *App) StartService(serviceName string) string {
	key, display, ok := normalizeService(serviceName)
	if !ok {
		return "未知服务名称"
	}

	rt, err := a.ensureRuntime()
	if err != nil {
		return fmt.Sprintf("%s 服务启动失败: %v", display, err)
	}

	if err := rt.Start(a.ctx, key); err != nil {
		return fmt.Sprintf("%s 服务启动失败: %v", display, err)
	}

	return fmt.Sprintf("%s 服务启动成功", display)
}

// StopService stops the requested service.
func (a *App) StopService(serviceName string) string {
	key, display, ok := normalizeService(serviceName)
	if !ok {
		return "未知服务名称"
	}

	rt := a.currentRuntime()
	if rt == nil {
		return fmt.Sprintf("%s 服务未运行", display)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rt.Stop(ctx, key); err != nil {
		return fmt.Sprintf("%s 服务停止失败: %v", display, err)
	}

	return fmt.Sprintf("%s 服务已停止", display)
}

// RestartService restarts a single service.
func (a *App) RestartService(serviceName string) string {
	key, display, ok := normalizeService(serviceName)
	if !ok {
		return "未知服务名称"
	}

	rt := a.currentRuntime()
	if rt == nil {
		// If runtime hasn't been initialized yet, simply start the service.
		return a.StartService(serviceName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := rt.Stop(ctx, key); err != nil {
		return fmt.Sprintf("%s 服务重启失败: %v", display, err)
	}

	time.Sleep(500 * time.Millisecond)

	if err := rt.Start(a.ctx, key); err != nil {
		return fmt.Sprintf("%s 服务重启失败: %v", display, err)
	}

	return fmt.Sprintf("%s 服务重启成功", display)
}

// GetSystemInfo returns static application metadata enriched with runtime data.
func (a *App) GetSystemInfo() map[string]interface{} {
	info := map[string]interface{}{
		"app_name":            "小智桌面端",
		"version":             "1.0.0",
		"go_version":          goruntime.Version(),
		"wails_version":       wailsVersion(),
		"build_time":          BuildTime,
		"integration":         "真实服务集成",
		"services":            a.services,
		"manager_url":         a.managerURL,
		"manager_console_url": a.managerURL,
		"root_dir":            a.rootDir,
		"work_dir":            a.workDir,
	}

	if rt := a.currentRuntime(); rt != nil {
		info["redis_address"] = rt.RedisAddr()
	}

	return info
}

// OpenManagerWeb launches the manager UI in the system browser.
func (a *App) OpenManagerWeb() string {
	if a.managerURL == "" {
		return "未能解析管理界面地址"
	}

	wailsRuntime.BrowserOpenURL(a.ctx, a.managerURL)

	return "管理控制台已在浏览器中打开"
}

// GetLogs attempts to read the latest log output for a service.
func (a *App) GetLogs(serviceName string) string {
	key, display, ok := normalizeService(serviceName)
	if !ok {
		return "未知服务日志"
	}

	path := a.resolveLogPath(key)
	if path == "" {
		return fmt.Sprintf("%s 服务日志未找到", display)
	}

	content, err := readLogTail(path, 16*1024)
	if err != nil {
		return fmt.Sprintf("%s 服务日志读取失败: %v", display, err)
	}

	if strings.TrimSpace(content) == "" {
		return fmt.Sprintf("%s 服务日志暂无内容", display)
	}

	// 过滤日志，只显示 WARN 及以上级别
	filtered := filterLogsByLevel(content)
	return filtered
}

// GetServiceHealth returns a coarse health summary for each managed service.
func (a *App) GetServiceHealth() map[string]interface{} {
	summary := make(map[string]interface{}, len(serviceAlias))
	rt := a.currentRuntime()
	var (
		state     aioruntime.Status
		redisAddr string
	)
	if rt != nil {
		state = rt.Status()
		redisAddr = rt.RedisAddr()
	}

	for alias, runtimeKey := range serviceAlias {
		summary[alias] = a.serviceHealthSummary(runtimeKey, state, redisAddr)
	}

	return summary
}

func (a *App) serviceHealthSummary(service string, state aioruntime.Status, redisAddr string) map[string]interface{} {
	switch service {
	case aioruntime.ServiceBackend:
		return map[string]interface{}{
			"status":       state.Backend,
			"uptime":       uptimeLabel(state.Backend),
			"redis_target": redisAddr,
		}
	case aioruntime.ServiceManager:
		return map[string]interface{}{
			"status": state.Manager,
			"uptime": uptimeLabel(state.Manager),
			"url":    a.managerURL,
		}
	case aioruntime.ServiceRedis:
		return map[string]interface{}{
			"status":  state.Redis,
			"uptime":  uptimeLabel(state.Redis),
			"address": redisAddr,
		}
	case aioruntime.ServiceSpeech:
		return map[string]interface{}{
			"status":  state.Speech,
			"uptime":  uptimeLabel(state.Speech),
			"address": speechServerAddress(a.cfg.SpeechConfig),
		}
	default:
		return map[string]interface{}{
			"status": false,
			"uptime": uptimeLabel(false),
		}
	}
}

func runtimeServiceActive(state aioruntime.Status, service string) bool {
	switch service {
	case aioruntime.ServiceBackend:
		return state.Backend
	case aioruntime.ServiceManager:
		return state.Manager
	case aioruntime.ServiceRedis:
		return state.Redis
	case aioruntime.ServiceSpeech:
		return state.Speech
	default:
		return false
	}
}

// Greet returns a greeting for the given name.
func (a *App) Greet(name string) string {
	return fmt.Sprintf("你好 %s，欢迎使用小智桌面端！", name)
}

func (a *App) ensureRuntime() (*aioruntime.Runtime, error) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()

	if a.runtime != nil {
		return a.runtime, nil
	}

	cfg := a.cfg
	cfg.OnManagerRunComplete = func(err error) {
		if err != nil {
			wailsRuntime.LogError(a.ctx, fmt.Sprintf("Manager 服务异常退出: %v", err))
			return
		}
		wailsRuntime.LogInfo(a.ctx, "Manager 服务已退出")
	}

	rt, err := aioruntime.New(cfg)
	if err != nil {
		return nil, err
	}

	a.runtime = rt
	return rt, nil
}

func (a *App) currentRuntime() *aioruntime.Runtime {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	return a.runtime
}

func (a *App) selectedServices() []string {
	services := make([]string, 0, len(a.services))
	for name, enabled := range a.services {
		if enabled {
			services = append(services, name)
		}
	}
	return services
}

func (a *App) resolveLogPath(service string) string {
	candidates := logFileCandidates(a.rootDir, a.workDir, service)
	for _, path := range candidates {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func computeDefaultManagerURL() string {
	return "http://localhost:8002/manager-console/"
}

func (a *App) computeManagerURL() string {
	data, err := os.ReadFile(a.cfg.ManagerConfig)
	if err != nil {
		log.Printf("读取管理配置失败: %v", err)
		return computeDefaultManagerURL()
	}

	var cfg struct {
		Server struct {
			Port        int    `yaml:"port"`
			ContextPath string `yaml:"context_path"`
		} `yaml:"server"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Printf("解析管理配置失败: %v", err)
		return computeDefaultManagerURL()
	}

	//port := cfg.Server.Port
	port := 8002

	consolePath := "/manager-console/"
	contextPath := strings.TrimSpace(cfg.Server.ContextPath)
	if contextPath != "" && contextPath != "/" {
		if !strings.HasPrefix(contextPath, "/") {
			contextPath = "/" + contextPath
		}
		contextPath = strings.TrimSuffix(contextPath, "/")
		consolePath = fmt.Sprintf("%s/manager-console/", contextPath)
	}

	return fmt.Sprintf("http://localhost:%d", port) + consolePath
}

func normalizeService(name string) (key string, display string, ok bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	key, ok = serviceAlias[normalized]
	if !ok {
		return "", "", false
	}

	switch key {
	case aioruntime.ServiceBackend:
		display = "Backend"
	case aioruntime.ServiceManager:
		display = "Manager"
	case aioruntime.ServiceRedis:
		display = "Redis"
	case aioruntime.ServiceSpeech:
		display = "Speech"
	}
	return key, display, true
}

func uptimeLabel(active bool) string {
	if active {
		return "运行中"
	}
	return "已停止"
}

func logFileCandidates(root, workDir, service string) []string {
	rootLogs := filepath.Join(root, "logs")
	workLogs := filepath.Join(workDir, "logs")

	switch service {
	case aioruntime.ServiceBackend:
		return []string{
			filepath.Join(workLogs, "backend-server.log"),
			filepath.Join(rootLogs, "backend-server.log"),
			filepath.Join(root, "backend-server", "logs", "backend-server.log"),
		}
	case aioruntime.ServiceManager:
		return []string{
			filepath.Join(workLogs, "manager-server.log"),
			filepath.Join(rootLogs, "manager-server.log"),
			filepath.Join(root, "manager-server", "logs", "manager-server.log"),
		}
	case aioruntime.ServiceRedis:
		return []string{
			filepath.Join(workLogs, "redis", "redis-server.log"),
			filepath.Join(rootLogs, "redis", "redis-server.log"),
		}
	case aioruntime.ServiceSpeech:
		return []string{
			filepath.Join(workLogs, "speech-server.log"),
			filepath.Join(rootLogs, "speech-server.log"),
			filepath.Join(root, "speech-server", "logs", "speech-server.log"),
		}
	default:
		return nil
	}
}

func speechServerAddress(configPath string) string {
	addr := sanitizeSpeechAddr(":8009")
	path := strings.TrimSpace(configPath)
	if path == "" {
		return addr
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return addr
	}
	var cfg struct {
		Server struct {
			Addr string `yaml:"addr"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return addr
	}
	if strings.TrimSpace(cfg.Server.Addr) == "" {
		return addr
	}
	return sanitizeSpeechAddr(cfg.Server.Addr)
}

func sanitizeSpeechAddr(raw string) string {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return "127.0.0.1:8009"
	}
	if strings.HasPrefix(addr, "http://") {
		addr = strings.TrimPrefix(addr, "http://")
	} else if strings.HasPrefix(addr, "https://") {
		addr = strings.TrimPrefix(addr, "https://")
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	if strings.HasPrefix(addr, "[::]:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "[::]:")
	}
	return addr
}

func readLogTail(path string, maxBytes int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	size := info.Size()
	var start int64
	if size > maxBytes {
		start = size - maxBytes
	}

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", err
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// filterLogsByLevel 过滤日志内容，只保留 WARN 及以上级别的日志
func filterLogsByLevel(content string) string {
	lines := strings.Split(content, "\n")
	var filtered []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 检查是否为 WARN 或 ERROR 级别
		// 支持两种格式：
		// 1. JSON 格式: "level":"WARN" 或 "level":"ERROR"
		// 2. 文本格式: level=WARN 或 level=ERROR
		if isWarnOrErrorLevel(line) {
			filtered = append(filtered, line)
		}
	}

	if len(filtered) == 0 {
		return "暂无 WARN 及以上级别的日志"
	}

	return strings.Join(filtered, "\n")
}

// isWarnOrErrorLevel 检查日志行是否为 WARN 或 ERROR 级别
func isWarnOrErrorLevel(line string) bool {
	lineUpper := strings.ToUpper(line)

	// 检查 JSON 格式: "level":"WARN" 或 "level":"ERROR" 或 "level":"FATAL"
	if strings.Contains(lineUpper, `"LEVEL":"WARN"`) ||
		strings.Contains(lineUpper, `"LEVEL":"ERROR"`) ||
		strings.Contains(lineUpper, `"LEVEL":"FATAL"`) {
		return true
	}

	// 检查文本格式: level=WARN 或 level=ERROR 或 level=FATAL
	if strings.Contains(lineUpper, `LEVEL=WARN`) ||
		strings.Contains(lineUpper, `LEVEL=ERROR`) ||
		strings.Contains(lineUpper, `LEVEL=FATAL`) {
		return true
	}

	return false
}

func resolveProjectRoot() string {
	candidates := make([]string, 0, 4)

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if env := strings.TrimSpace(os.Getenv("XIAOZHI_DESKTOP_ROOT")); env != "" {
		candidates = append(candidates, env)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}

		if root := searchForRoot(abs); root != "" {
			return root
		}
	}

	if len(candidates) == 0 {
		return "."
	}

	abs, err := filepath.Abs(candidates[0])
	if err != nil {
		return candidates[0]
	}
	return abs
}

func searchForRoot(start string) string {
	current := filepath.Clean(start)
	for i := 0; i < 8; i++ {
		if isProjectRoot(current) {
			return current
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	return ""
}

func isProjectRoot(path string) bool {
	backendCfg := filepath.Join(path, "backend-server", "config", "config.yaml")
	managerCfg := filepath.Join(path, "manager-server", "config", "config.yaml")
	return fileExists(backendCfg) && fileExists(managerCfg)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func wailsVersion() string {
	// The Wails framework does not expose its version at runtime; keep config value for UI.
	return "2.10.2"
}
