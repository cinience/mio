package vision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	manager_api "backend-server/internal/adapters/manager"
	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/config"
	"backend-server/internal/domain/mcp"
	log "backend-server/internal/infrastructure/logger"
	chatutils "backend-server/internal/server/chat/utils"
)

type Scheduler struct {
	manager         manager_api.ManagerAPIService
	cfg             config.VisionConfig
	fetcher         FrameFetcher
	defaultInterval time.Duration
	maxInflight     int
	globalLimiter   *semaphore

	mu          sync.Mutex
	tasks       map[string]*captureTask
	ruleLimiter map[string]*semaphore
}

const autoSourceKey = "__auto__"

type captureTask struct {
	key       string
	signature string
	cancel    context.CancelFunc
}

type taskSpec struct {
	agent   types.VisionAgentConfig
	sources []types.VisionAgentSource
	rule    types.VisionAgentRule
	state   *taskState
	limiter *semaphore
}

type taskState struct {
	lastEvent int64
	failCount int32
	mu        sync.Mutex
	hitCount  int
	firstHit  int64
	lastHit   int64
	mergeBest *eventCandidate
	sourceIdx int
	lastHash  string
}

type FrameFetcher interface {
	Capture(ctx context.Context, source types.VisionAgentSource) ([]byte, string, error)
}

// NewScheduler constructs a vision scheduler with configured fetchers and limits.
func NewScheduler(manager manager_api.ManagerAPIService, cfg config.VisionConfig) *Scheduler {
	defaultInterval := time.Second
	if cfg.Sampling.DefaultIntervalMs > 0 {
		defaultInterval = time.Duration(cfg.Sampling.DefaultIntervalMs) * time.Millisecond
	}
	maxInflight := cfg.Sampling.MaxInflight
	if maxInflight <= 0 {
		maxInflight = 1
	}

	var globalLimiter *semaphore
	if cfg.Sampling.GlobalMaxInflight > 0 {
		globalLimiter = newSemaphore(cfg.Sampling.GlobalMaxInflight)
	}

	fetcher := buildFrameFetcher(cfg)

	return &Scheduler{
		manager:         manager,
		cfg:             cfg,
		fetcher:         fetcher,
		defaultInterval: defaultInterval,
		maxInflight:     maxInflight,
		globalLimiter:   globalLimiter,
		tasks:           make(map[string]*captureTask),
		ruleLimiter:     make(map[string]*semaphore),
	}
}

// Run starts the scheduler loop until context cancellation.
func (s *Scheduler) Run(ctx context.Context) {
	if s.manager == nil {
		log.Warn("vision scheduler skipped: manager api service unavailable")
		return
	}
	if !s.cfg.Ingest.Enabled {
		log.Info("vision scheduler disabled by config")
		return
	}

	interval := time.Duration(s.cfg.Ingest.SyncIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 20 * time.Second
	}

	s.sync(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.sync(ctx)
		case <-ctx.Done():
			s.stopAll()
			return
		}
	}
}

func (s *Scheduler) sync(ctx context.Context) {
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	configs, err := s.manager.GetVisionAgentConfigs(syncCtx)
	if err != nil {
		log.Warnf("vision scheduler sync failed: %v", err)
		return
	}

	desired := make(map[string]taskSpec)
	for _, agent := range configs {
		if strings.TrimSpace(agent.AgentType) != "vision-primary" {
			continue
		}
		enabledSources := make([]types.VisionAgentSource, 0, len(agent.Sources))
		for _, source := range agent.Sources {
			if source.Enabled {
				enabledSources = append(enabledSources, source)
			}
		}
		if len(enabledSources) == 0 {
			continue
		}
		for _, rule := range agent.Rules {
			if !rule.Enabled {
				continue
			}
			limiter := s.ensureRuleLimiter(rule.ID, rule.MaxInflight)
			if rule.AutoSelectSource {
				key := buildTaskKey(agent.AgentID, autoSourceKey, rule.ID)
				spec := taskSpec{
					agent:   agent,
					sources: enabledSources,
					rule:    rule,
					state:   &taskState{},
					limiter: limiter,
				}
				desired[key] = spec
				continue
			}
			for _, source := range enabledSources {
				key := buildTaskKey(agent.AgentID, source.ID, rule.ID)
				spec := taskSpec{
					agent:   agent,
					sources: []types.VisionAgentSource{source},
					rule:    rule,
					state:   &taskState{},
					limiter: limiter,
				}
				desired[key] = spec
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, spec := range desired {
		signature := buildTaskSignature(spec.agent, spec.sources, spec.rule)
		if existing, ok := s.tasks[key]; ok {
			if existing.signature == signature {
				continue
			}
			existing.cancel()
			delete(s.tasks, key)
		}
		taskCtx, taskCancel := context.WithCancel(ctx)
		s.tasks[key] = &captureTask{key: key, signature: signature, cancel: taskCancel}
		go s.runTask(taskCtx, spec)
	}

	for key, task := range s.tasks {
		if _, ok := desired[key]; !ok {
			task.cancel()
			delete(s.tasks, key)
		}
	}
}

func (s *Scheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, task := range s.tasks {
		task.cancel()
		delete(s.tasks, key)
	}
}

func (s *Scheduler) runTask(ctx context.Context, spec taskSpec) {
	interval := s.resolveInterval(spec.rule)
	cooldown := time.Duration(spec.rule.CooldownSeconds) * time.Second
	if cooldown < 0 {
		cooldown = 0
	}
	mergeWindow := s.resolveMergeWindow(interval)

	for {
		wait := interval
		if failCount := atomic.LoadInt32(&spec.state.failCount); failCount > 0 {
			wait = s.backoffDelay(failCount, interval)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if cooldown > 0 {
			if last := atomic.LoadInt64(&spec.state.lastEvent); last > 0 {
				if time.Since(time.Unix(0, last)) < cooldown {
					continue
				}
			}
		}

		if !spec.limiter.TryAcquire() {
			continue
		}
		if s.globalLimiter != nil && !s.globalLimiter.TryAcquire() {
			spec.limiter.Release()
			continue
		}

		go func() {
			defer spec.limiter.Release()
			if s.globalLimiter != nil {
				defer s.globalLimiter.Release()
			}
			if err := s.handleSample(ctx, spec, mergeWindow); err != nil {
				log.Warnf("vision task sample failed (agent=%s source=%s rule=%s): %v", spec.agent.AgentID, formatSources(spec.sources), spec.rule.ID, err)
				atomic.AddInt32(&spec.state.failCount, 1)
				return
			}
			atomic.StoreInt32(&spec.state.failCount, 0)
		}()
	}
}

func (s *Scheduler) handleSample(ctx context.Context, spec taskSpec, mergeWindow time.Duration) error {
	now := time.Now()
	if candidate := s.takeReadyCandidate(spec.state, now, mergeWindow); candidate != nil {
		if err := s.emitCandidate(ctx, spec, candidate); err != nil {
			s.restoreCandidate(spec.state, candidate)
			return err
		}
		return nil
	}

	source := s.selectSource(spec)
	if strings.TrimSpace(source.ID) == "" {
		return nil
	}

	captureTimeout := time.Duration(s.cfg.Ingest.SnapshotTimeoutSeconds) * time.Second
	if captureTimeout <= 0 {
		captureTimeout = 8 * time.Second
	}

	captureCtx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	frameCount := spec.rule.VisionUseImgCount
	if frameCount <= 0 {
		frameCount = 1
	}
	frame, mimeType, err := s.captureFrames(captureCtx, source, frameCount)
	if err != nil {
		return err
	}
	if s.shouldSkipMotion(spec.state, frame, spec.rule.MotionFilterEnabled) {
		return nil
	}

	prompt := buildPrompt(spec.agent.SystemPrompt, spec.rule)
	start := time.Now()
	result, err := chatutils.HandleVLM(spec.agent.AgentID, frame, prompt, "", nil)
	if err != nil {
		return err
	}
	latency := time.Since(start)

	parsed := parseVLMResult(result)
	hit := parsed.Hit
	if !parsed.HasHit {
		hit = strings.TrimSpace(parsed.Summary) != ""
	}
	decisionMode := strings.ToLower(strings.TrimSpace(spec.rule.DecisionMode))
	if decisionMode == "hybrid" && spec.rule.ConfidenceThreshold > 0 {
		if !parsed.HasConfidence || parsed.Confidence < spec.rule.ConfidenceThreshold {
			hit = false
		}
	}
	if !hit {
		s.resetHitState(spec.state)
		return nil
	}

	hitCount, hitSeconds, ready := s.recordHit(spec.state, now, spec.rule.MinHitCount, spec.rule.MinHitSeconds)
	if !ready {
		return nil
	}

	summary := strings.TrimSpace(parsed.Summary)
	if summary == "" {
		summary = strings.TrimSpace(result)
	}
	summary = truncateRunes(summary, 200)
	labels := strings.TrimSpace(parsed.Labels)
	candidate := &eventCandidate{
		SourceID:   source.ID,
		SourceName: source.Name,
		DeviceID:   source.DeviceID,
		Summary:    summary,
		Labels:     labels,
		Raw:        result,
		Confidence: parsed.Confidence,
		Prompt:     prompt,
		Latency:    latency,
		CapturedAt: now,
		Frame:      frame,
		MimeType:   mimeType,
		HitCount:   hitCount,
		HitSeconds: hitSeconds,
	}

	if merged := s.storeCandidate(spec.state, candidate, mergeWindow); merged != nil {
		if err := s.emitCandidate(ctx, spec, merged); err != nil {
			s.restoreCandidate(spec.state, merged)
			return err
		}
	}
	return nil
}

func (s *Scheduler) recordHit(state *taskState, now time.Time, minHitCount, minHitSeconds int) (int, int, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.firstHit == 0 {
		state.firstHit = now.UnixNano()
	}
	state.lastHit = now.UnixNano()
	state.hitCount++

	hitCount := state.hitCount
	hitSeconds := 0
	if state.firstHit > 0 {
		hitSeconds = int(now.Sub(time.Unix(0, state.firstHit)).Seconds())
	}

	if minHitCount > 0 && hitCount < minHitCount {
		return hitCount, hitSeconds, false
	}
	if minHitSeconds > 0 && hitSeconds < minHitSeconds {
		return hitCount, hitSeconds, false
	}
	return hitCount, hitSeconds, true
}

func (s *Scheduler) resetHitState(state *taskState) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.hitCount = 0
	state.firstHit = 0
	state.lastHit = 0
}

func (s *Scheduler) takeReadyCandidate(state *taskState, now time.Time, mergeWindow time.Duration) *eventCandidate {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mergeBest == nil {
		return nil
	}
	if mergeWindow > 0 && now.Sub(state.mergeBest.FirstSeen) < mergeWindow {
		return nil
	}
	candidate := state.mergeBest
	state.mergeBest = nil
	state.hitCount = 0
	state.firstHit = 0
	state.lastHit = 0
	return candidate
}

func (s *Scheduler) storeCandidate(state *taskState, candidate *eventCandidate, mergeWindow time.Duration) *eventCandidate {
	if candidate == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mergeBest == nil {
		candidate.FirstSeen = candidate.CapturedAt
		state.mergeBest = candidate
	} else {
		best := state.mergeBest
		if candidate.Confidence >= best.Confidence {
			candidate.FirstSeen = best.FirstSeen
			state.mergeBest = candidate
		}
	}
	if mergeWindow <= 0 {
		merged := state.mergeBest
		state.mergeBest = nil
		state.hitCount = 0
		state.firstHit = 0
		state.lastHit = 0
		return merged
	}
	if now := time.Now(); now.Sub(state.mergeBest.FirstSeen) >= mergeWindow {
		merged := state.mergeBest
		state.mergeBest = nil
		state.hitCount = 0
		state.firstHit = 0
		state.lastHit = 0
		return merged
	}
	return nil
}

func (s *Scheduler) restoreCandidate(state *taskState, candidate *eventCandidate) {
	if candidate == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mergeBest == nil {
		state.mergeBest = candidate
	}
}

func (s *Scheduler) emitCandidate(ctx context.Context, spec taskSpec, candidate *eventCandidate) error {
	if candidate == nil {
		return nil
	}

	summary := strings.TrimSpace(candidate.Summary)
	if summary == "" {
		summary = truncateRunes(strings.TrimSpace(candidate.Raw), 200)
	}
	if summary == "" {
		summary = "识别命中"
	}

	var snapshotID string
	if s.cfg.Ingest.UploadMedia && len(candidate.Frame) > 0 {
		uploadCtx, uploadCancel := context.WithTimeout(ctx, 8*time.Second)
		defer uploadCancel()
		mediaID, err := s.uploadSnapshot(uploadCtx, spec, candidate.SourceID, candidate.Frame, candidate.MimeType, candidate.Prompt, candidate.Raw, candidate.Latency)
		if err != nil {
			log.Warnf("vision snapshot upload failed (agent=%s source=%s): %v", spec.agent.AgentID, candidate.SourceID, err)
		} else {
			snapshotID = mediaID
		}
	}

	event := &types.VisionEventCreateRequest{
		AgentID:         spec.agent.AgentID,
		SourceID:        candidate.SourceID,
		RuleID:          spec.rule.ID,
		SnapshotMediaID: snapshotID,
		Summary:         summary,
		RawResponse:     candidate.Raw,
		Confidence:      candidate.Confidence,
		Labels:          candidate.Labels,
		ActionResult:    s.executeActions(ctx, spec, candidate.DeviceID),
		Extra: map[string]interface{}{
			"agentName":  spec.agent.AgentName,
			"sourceName": candidate.SourceName,
			"ruleName":   spec.rule.Name,
			"prompt":     candidate.Prompt,
			"latencyMs":  candidate.Latency.Milliseconds(),
			"capturedAt": candidate.CapturedAt.UTC().Format(time.RFC3339),
			"hitTrace": map[string]interface{}{
				"hitCount":    candidate.HitCount,
				"hitSeconds":  candidate.HitSeconds,
				"cooldownSec": spec.rule.CooldownSeconds,
			},
		},
	}

	if err := s.manager.ReportVisionEvent(ctx, event); err != nil {
		return err
	}

	atomic.StoreInt64(&spec.state.lastEvent, time.Now().UnixNano())
	return nil
}

func (s *Scheduler) uploadSnapshot(ctx context.Context, spec taskSpec, sourceID string, payload []byte, mimeType, prompt, result string, latency time.Duration) (string, error) {
	if s.manager == nil {
		return "", nil
	}

	fileName := fmt.Sprintf("vision-%s-%d.jpg", sourceID, time.Now().Unix())
	description := truncateRunes(strings.TrimSpace(prompt), 120)
	if description == "" {
		description = truncateRunes(strings.TrimSpace(spec.rule.Name), 120)
	}

	relatedInfo := map[string]any{
		"prompt":    prompt,
		"result":    result,
		"latencyMs": latency.Milliseconds(),
		"sourceId":  sourceID,
		"ruleId":    spec.rule.ID,
	}

	asset, err := s.manager.UploadMedia(ctx, &types.MediaUploadRequest{
		AgentID:     spec.agent.AgentID,
		Source:      "vision",
		Description: description,
		FileName:    fileName,
		ContentType: mimeType,
		Reader:      bytes.NewReader(payload),
		RelatedInfo: relatedInfo,
	})
	if err != nil {
		return "", err
	}
	if asset == nil || asset.ID == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d", asset.ID), nil
}

func (s *Scheduler) resolveInterval(rule types.VisionAgentRule) time.Duration {
	if rule.SampleIntervalMs > 0 {
		return time.Duration(rule.SampleIntervalMs) * time.Millisecond
	}
	return s.defaultInterval
}

func (s *Scheduler) resolveMergeWindow(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = s.defaultInterval
	}
	if interval < time.Second {
		return time.Second
	}
	return interval
}

func (s *Scheduler) selectSource(spec taskSpec) types.VisionAgentSource {
	if len(spec.sources) == 0 {
		return types.VisionAgentSource{}
	}
	if len(spec.sources) == 1 {
		return spec.sources[0]
	}
	spec.state.mu.Lock()
	defer spec.state.mu.Unlock()
	if spec.state.sourceIdx >= len(spec.sources) {
		spec.state.sourceIdx = 0
	}
	selected := spec.sources[spec.state.sourceIdx]
	spec.state.sourceIdx++
	return selected
}

func (s *Scheduler) captureFrames(ctx context.Context, source types.VisionAgentSource, count int) ([]byte, string, error) {
	if count <= 1 {
		return s.fetcher.Capture(ctx, source)
	}
	frames := make([][]byte, 0, count)
	mimeType := ""
	for i := 0; i < count; i++ {
		frame, mt, err := s.fetcher.Capture(ctx, source)
		if err != nil {
			if len(frames) == 0 {
				return nil, "", err
			}
			break
		}
		if mimeType == "" {
			mimeType = mt
		}
		frames = append(frames, frame)
		if i < count-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if len(frames) == 0 {
		return nil, "", fmt.Errorf("empty frames")
	}
	if len(frames) == 1 {
		return frames[0], mimeType, nil
	}
	merged, err := mergeFrames(frames)
	if err != nil {
		return frames[len(frames)-1], mimeType, nil
	}
	return merged, "image/jpeg", nil
}

func (s *Scheduler) shouldSkipMotion(state *taskState, frame []byte, enabled bool) bool {
	if !enabled || len(frame) == 0 {
		return false
	}
	hash := sha256.Sum256(frame)
	hashText := hex.EncodeToString(hash[:])
	state.mu.Lock()
	defer state.mu.Unlock()
	if hashText == state.lastHash {
		return true
	}
	state.lastHash = hashText
	return false
}

func mergeFrames(frames [][]byte) ([]byte, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("empty frames")
	}
	images := make([]image.Image, 0, len(frames))
	maxWidth := 0
	totalHeight := 0
	for _, frame := range frames {
		img, _, err := image.Decode(bytes.NewReader(frame))
		if err != nil {
			return nil, err
		}
		images = append(images, img)
		bounds := img.Bounds()
		if bounds.Dx() > maxWidth {
			maxWidth = bounds.Dx()
		}
		totalHeight += bounds.Dy()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, maxWidth, totalHeight))
	offsetY := 0
	for _, img := range images {
		bounds := img.Bounds()
		draw.Draw(canvas, image.Rect(0, offsetY, bounds.Dx(), offsetY+bounds.Dy()), img, bounds.Min, draw.Over)
		offsetY += bounds.Dy()
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Scheduler) ensureRuleLimiter(ruleID string, maxInflight int) *semaphore {
	if maxInflight <= 0 {
		maxInflight = s.maxInflight
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if limiter, ok := s.ruleLimiter[ruleID]; ok && limiter.Capacity() == maxInflight {
		return limiter
	}
	limiter := newSemaphore(maxInflight)
	s.ruleLimiter[ruleID] = limiter
	return limiter
}

func (s *Scheduler) backoffDelay(failCount int32, base time.Duration) time.Duration {
	initial := time.Duration(s.cfg.Backoff.InitialMs) * time.Millisecond
	if initial <= 0 {
		initial = 500 * time.Millisecond
	}
	maxDelay := time.Duration(s.cfg.Backoff.MaxMs) * time.Millisecond
	if maxDelay <= 0 {
		maxDelay = 10 * time.Second
	}
	delay := initial
	for i := int32(1); i < failCount; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	if delay < base {
		return base
	}
	return delay
}

func buildTaskKey(agentID, sourceID, ruleID string) string {
	return fmt.Sprintf("%s:%s:%s", strings.TrimSpace(agentID), strings.TrimSpace(sourceID), strings.TrimSpace(ruleID))
}

func formatSources(sources []types.VisionAgentSource) string {
	if len(sources) == 0 {
		return ""
	}
	if len(sources) == 1 {
		return strings.TrimSpace(sources[0].ID)
	}
	return fmt.Sprintf("auto:%d", len(sources))
}

func buildTaskSignature(agent types.VisionAgentConfig, sources []types.VisionAgentSource, rule types.VisionAgentRule) string {
	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(agent.AgentID))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(agent.SystemPrompt))
	builder.WriteString("|")
	if len(sources) == 0 {
		builder.WriteString("no-source")
		builder.WriteString("|")
	} else {
		ordered := make([]types.VisionAgentSource, len(sources))
		copy(ordered, sources)
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].ID < ordered[j].ID
		})
		for _, source := range ordered {
			builder.WriteString(strings.TrimSpace(source.ID))
			builder.WriteString(",")
			builder.WriteString(strings.TrimSpace(source.Protocol))
			builder.WriteString(",")
			builder.WriteString(strings.TrimSpace(source.SourceURI))
			builder.WriteString(",")
			builder.WriteString(strings.TrimSpace(source.Username))
			builder.WriteString(",")
			builder.WriteString(strings.TrimSpace(source.Password))
			builder.WriteString(",")
			builder.WriteString(fmt.Sprintf("%t", source.Enabled))
			builder.WriteString("|")
		}
	}
	builder.WriteString(strings.TrimSpace(rule.ID))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(rule.Name))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(rule.PromptTemplate))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(rule.ConditionText))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(rule.DecisionMode))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(rule.VllmModelID))
	builder.WriteString("|")
	builder.WriteString(fmt.Sprintf("%d:%d:%d:%d:%d", rule.SampleIntervalMs, rule.MaxInflight, rule.MinHitCount, rule.MinHitSeconds, rule.CooldownSeconds))
	builder.WriteString("|")
	builder.WriteString(fmt.Sprintf("%0.4f", rule.ConfidenceThreshold))
	builder.WriteString("|")
	builder.WriteString(fmt.Sprintf("%t:%t:%d", rule.MotionFilterEnabled, rule.AutoSelectSource, rule.VisionUseImgCount))
	builder.WriteString("|")
	builder.WriteString(fmt.Sprintf("%t", rule.Enabled))
	return builder.String()
}

func buildPrompt(systemPrompt string, rule types.VisionAgentRule) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(systemPrompt); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(rule.PromptTemplate); trimmed != "" {
		parts = append(parts, trimmed)
	} else if trimmed := strings.TrimSpace(rule.ConditionText); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if len(parts) == 0 {
		return "请描述画面内容"
	}
	return strings.Join(parts, "\n")
}

func truncateRunes(input string, max int) string {
	runes := []rune(input)
	if len(runes) <= max {
		return input
	}
	return string(runes[:max])
}

type vlmParseResult struct {
	HasHit        bool
	Hit           bool
	HasConfidence bool
	Confidence    float64
	Labels        string
	Summary       string
}

type eventCandidate struct {
	SourceID   string
	SourceName string
	DeviceID   string
	Summary    string
	Labels     string
	Raw        string
	Confidence float64
	Prompt     string
	Latency    time.Duration
	CapturedAt time.Time
	Frame      []byte
	MimeType   string
	FirstSeen  time.Time
	HitCount   int
	HitSeconds int
}

var (
	resultRegex     = regexp.MustCompile(`(?i)(结果|result|hit)\s*[:：]\s*([a-zA-Z]+|是|否|命中|未命中|有|无)`)
	confidenceRegex = regexp.MustCompile(`(?i)(置信度|confidence|score)\s*[:：]\s*([0-9]+(?:\.[0-9]+)?)`)
	labelsRegex     = regexp.MustCompile(`(?i)(标签|labels?)\s*[:：]\s*(.+)`)
	summaryRegex    = regexp.MustCompile(`(?i)(摘要|summary|描述|desc(?:ription)?)\s*[:：]\s*(.+)`)
)

type visionActionConfig struct {
	Mode             string                   `json:"mode"`
	McpTools         []visionActionToolConfig `json:"mcpTools"`
	AutomationScenes []map[string]interface{} `json:"automationScenes"`
	DeviceCommands   []map[string]interface{} `json:"deviceCommands"`
}

type visionActionToolConfig struct {
	Tool  string                 `json:"tool"`
	Input map[string]interface{} `json:"input"`
}

func parseVLMResult(raw string) vlmParseResult {
	result := vlmParseResult{}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return result
	}

	if jsonCandidate := extractJSON(trimmed); jsonCandidate != "" {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(jsonCandidate), &payload); err == nil {
			applyJSONPayload(&result, payload)
		}
	}

	if !result.HasHit {
		if matches := resultRegex.FindStringSubmatch(trimmed); len(matches) > 2 {
			if hit, ok := parseBoolToken(matches[2]); ok {
				result.HasHit = true
				result.Hit = hit
			}
		}
	}

	if !result.HasConfidence {
		if matches := confidenceRegex.FindStringSubmatch(trimmed); len(matches) > 2 {
			if value, err := strconv.ParseFloat(matches[2], 64); err == nil {
				value = normalizeConfidence(value)
				result.HasConfidence = true
				result.Confidence = value
			}
		}
	}

	if result.Labels == "" {
		if matches := labelsRegex.FindStringSubmatch(trimmed); len(matches) > 2 {
			result.Labels = normalizeLabels(matches[2])
		}
	}

	if result.Summary == "" {
		if matches := summaryRegex.FindStringSubmatch(trimmed); len(matches) > 2 {
			result.Summary = strings.TrimSpace(matches[2])
		}
	}

	if result.Summary == "" {
		result.Summary = trimmed
	}
	return result
}

func extractJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return ""
}

func applyJSONPayload(result *vlmParseResult, payload map[string]interface{}) {
	if result == nil || payload == nil {
		return
	}
	for key, value := range payload {
		normalized := strings.ToLower(strings.TrimSpace(key))
		switch normalized {
		case "result", "hit", "matched", "ok", "success", "结果", "命中":
			if hit, ok := parseAnyBool(value); ok {
				result.HasHit = true
				result.Hit = hit
			}
		case "confidence", "score", "置信度":
			if val, ok := parseAnyFloat(value); ok {
				result.HasConfidence = true
				result.Confidence = normalizeConfidence(val)
			}
		case "labels", "label", "标签":
			result.Labels = normalizeLabels(value)
		case "summary", "desc", "description", "摘要", "描述":
			if text, ok := value.(string); ok {
				result.Summary = strings.TrimSpace(text)
			}
		}
	}
}

func parseBoolToken(input string) (bool, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	switch trimmed {
	case "true", "yes", "1", "是", "命中", "有", "存在", "发生":
		return true, true
	case "false", "no", "0", "否", "未命中", "无", "不存在", "未发生":
		return false, true
	default:
		return false, false
	}
}

func parseAnyBool(value interface{}) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		return parseBoolToken(typed)
	case float64:
		if typed == 1 {
			return true, true
		}
		if typed == 0 {
			return false, true
		}
	}
	return false, false
}

func parseAnyFloat(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed, true
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func normalizeConfidence(value float64) float64 {
	if value > 1 && value <= 100 {
		value = value / 100
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func normalizeLabels(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(strings.ReplaceAll(typed, "，", ","))
	case []string:
		return strings.Join(typed, ",")
	case []interface{}:
		labels := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				labels = append(labels, strings.TrimSpace(str))
			}
		}
		return strings.Join(labels, ",")
	default:
		return ""
	}
}

func (s *Scheduler) executeActions(ctx context.Context, spec taskSpec, deviceID string) string {
	action := parseVisionAction(spec.rule.Action)
	if action == nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(action.Mode))
	if mode == "" {
		mode = "static"
	}

	result := map[string]interface{}{
		"mode":       mode,
		"executedAt": time.Now().UTC().Format(time.RFC3339),
	}

	switch mode {
	case "dynamic":
		result["status"] = "skipped"
		result["message"] = "dynamic action not implemented"
	default:
		deviceID = strings.TrimSpace(deviceID)
		mcpResults := make([]map[string]interface{}, 0, len(action.McpTools))
		successCount := 0
		for _, toolCfg := range action.McpTools {
			entry := map[string]interface{}{
				"tool": toolCfg.Tool,
			}
			if strings.TrimSpace(toolCfg.Tool) == "" {
				entry["success"] = false
				entry["error"] = "tool name is empty"
				mcpResults = append(mcpResults, entry)
				continue
			}
			tool, ok := mcp.GetToolByName(deviceID, toolCfg.Tool)
			if !ok {
				entry["success"] = false
				entry["error"] = "tool not found"
				mcpResults = append(mcpResults, entry)
				continue
			}
			payload := "{}"
			if toolCfg.Input != nil {
				if data, err := json.Marshal(toolCfg.Input); err == nil {
					payload = string(data)
				}
			}
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			output, err := tool.InvokableRun(callCtx, payload)
			cancel()
			if err != nil {
				entry["success"] = false
				entry["error"] = err.Error()
			} else {
				entry["success"] = true
				entry["result"] = output
				successCount++
			}
			mcpResults = append(mcpResults, entry)
		}
		result["mcpTools"] = mcpResults
		if len(action.McpTools) == 0 {
			result["status"] = "no_actions"
		} else if successCount == len(action.McpTools) {
			result["status"] = "ok"
		} else if successCount == 0 {
			result["status"] = "failed"
		} else {
			result["status"] = "partial"
		}
	}

	if len(action.AutomationScenes) > 0 {
		result["automationScenes"] = "not_implemented"
	}
	if len(action.DeviceCommands) > 0 {
		result["deviceCommands"] = "not_implemented"
	}

	if payload, err := json.Marshal(result); err == nil {
		return string(payload)
	}
	return ""
}

func parseVisionAction(raw interface{}) *visionActionConfig {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var action visionActionConfig
	if err := json.Unmarshal(data, &action); err != nil {
		return nil
	}
	if strings.TrimSpace(action.Mode) == "" &&
		len(action.McpTools) == 0 &&
		len(action.AutomationScenes) == 0 &&
		len(action.DeviceCommands) == 0 {
		return nil
	}
	return &action
}

type semaphore struct {
	ch chan struct{}
}

func newSemaphore(size int) *semaphore {
	if size <= 0 {
		size = 1
	}
	return &semaphore{ch: make(chan struct{}, size)}
}

func (s *semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *semaphore) Release() {
	select {
	case <-s.ch:
	default:
	}
}

func (s *semaphore) Capacity() int {
	return cap(s.ch)
}

type frameFetcher struct {
	remote *remoteFetcher
	local  *ffmpegFetcher
}

func buildFrameFetcher(cfg config.VisionConfig) FrameFetcher {
	local := &ffmpegFetcher{}
	if strings.TrimSpace(cfg.Ingest.BaseURL) == "" {
		return local
	}
	remote := &remoteFetcher{
		baseURL: strings.TrimSpace(cfg.Ingest.BaseURL),
		token:   strings.TrimSpace(cfg.Ingest.Token),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		synced:   make(map[string]bool),
		resolved: make(map[string]string),
	}
	return &frameFetcher{remote: remote, local: local}
}

func protocolRequiresIngest(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "gb28181", "onvif", "webrtc":
		return true
	default:
		return false
	}
}

func (f *frameFetcher) Capture(ctx context.Context, source types.VisionAgentSource) ([]byte, string, error) {
	protocol := strings.ToLower(strings.TrimSpace(source.Protocol))
	if f.remote != nil && (strings.TrimSpace(source.ChannelID) != "" || protocolRequiresIngest(protocol)) {
		if payload, mimeType, err := f.remote.Capture(ctx, source); err == nil {
			return payload, mimeType, nil
		} else if protocolRequiresIngest(protocol) {
			return nil, "", err
		}
	}
	return f.local.Capture(ctx, source)
}

type remoteFetcher struct {
	baseURL  string
	token    string
	client   *http.Client
	mu       sync.Mutex
	synced   map[string]bool
	resolved map[string]string
}

func (f *remoteFetcher) Capture(ctx context.Context, source types.VisionAgentSource) ([]byte, string, error) {
	channelID, err := f.resolveChannelID(ctx, source)
	if err != nil {
		return nil, "", err
	}
	endpoint := strings.TrimRight(f.baseURL, "/") + "/channels/" + url.PathEscape(channelID) + "/snapshot"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("remote snapshot status %d", resp.StatusCode)
	}
	payload, err := ioReadAll(resp)
	if err != nil {
		return nil, "", err
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return payload, mimeType, nil
}

func (f *remoteFetcher) resolveChannelID(ctx context.Context, source types.VisionAgentSource) (string, error) {
	if cached, ok := f.getResolvedChannel(source.ID); ok {
		return cached, nil
	}
	protocol := strings.ToLower(strings.TrimSpace(source.Protocol))
	channelID := strings.TrimSpace(source.ChannelID)
	switch protocol {
	case "onvif":
		return f.ensureOnvifChannel(ctx, source, channelID)
	case "gb28181":
		if channelID == "" {
			return "", fmt.Errorf("remote fetch requires channelId")
		}
		if err := f.ensureGBChannel(ctx, source); err != nil {
			return "", err
		}
		return channelID, nil
	case "webrtc":
		if channelID == "" {
			return "", fmt.Errorf("remote fetch requires channelId")
		}
		return channelID, nil
	default:
		if channelID == "" {
			return "", fmt.Errorf("remote fetch requires channelId")
		}
		return channelID, nil
	}
}

func (f *remoteFetcher) ensureGBChannel(ctx context.Context, source types.VisionAgentSource) error {
	if source.ID == "" {
		return fmt.Errorf("source id is empty")
	}
	if f.isSynced(source.ID) {
		return nil
	}
	deviceID := strings.TrimSpace(source.DeviceID)
	channelID := strings.TrimSpace(source.ChannelID)
	if deviceID == "" || channelID == "" {
		return fmt.Errorf("gb28181 requires deviceId and channelId")
	}
	deviceName := strings.TrimSpace(source.Name)
	if deviceName == "" {
		deviceName = deviceID
	}
	devicePayload := map[string]interface{}{
		"id":         deviceID,
		"name":       deviceName,
		"protocol":   "gb28181",
		"externalId": deviceID,
		"username":   strings.TrimSpace(source.Username),
		"password":   source.Password,
	}
	if err := f.postJSON(ctx, "/devices", devicePayload, nil); err != nil {
		return err
	}
	channelName := strings.TrimSpace(source.Name)
	if channelName == "" {
		channelName = channelID
	}
	channelPayload := map[string]interface{}{
		"id":         channelID,
		"deviceId":   deviceID,
		"name":       channelName,
		"protocol":   "gb28181",
		"externalId": channelID,
	}
	if err := f.postJSON(ctx, "/channels", channelPayload, nil); err != nil {
		return err
	}
	f.markSynced(source.ID, channelID)
	return nil
}

func (f *remoteFetcher) ensureOnvifChannel(ctx context.Context, source types.VisionAgentSource, preferred string) (string, error) {
	if source.ID == "" {
		return "", fmt.Errorf("source id is empty")
	}
	if cached, ok := f.getResolvedChannel(source.ID); ok {
		return cached, nil
	}
	address := strings.TrimSpace(source.SourceURI)
	if address == "" {
		return "", fmt.Errorf("onvif requires sourceUri")
	}
	deviceID := strings.TrimSpace(source.DeviceID)
	if deviceID == "" {
		deviceID = source.ID
	}
	deviceName := strings.TrimSpace(source.Name)
	if deviceName == "" {
		deviceName = deviceID
	}
	devicePayload := map[string]interface{}{
		"id":       deviceID,
		"name":     deviceName,
		"protocol": "onvif",
		"address":  address,
		"username": strings.TrimSpace(source.Username),
		"password": source.Password,
	}
	var raw json.RawMessage
	if err := f.postJSON(ctx, "/devices", devicePayload, &raw); err != nil {
		return "", err
	}
	var resp struct {
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if len(resp.Channels) == 0 {
		return "", fmt.Errorf("onvif device has no channels")
	}
	chID := strings.TrimSpace(preferred)
	if chID != "" {
		for _, ch := range resp.Channels {
			if ch.ID == chID {
				f.markSynced(source.ID, chID)
				return chID, nil
			}
		}
	}
	chID = resp.Channels[0].ID
	if chID == "" {
		return "", fmt.Errorf("onvif channel id missing")
	}
	f.markSynced(source.ID, chID)
	return chID, nil
}

func (f *remoteFetcher) postJSON(ctx context.Context, path string, payload interface{}, out interface{}) error {
	endpoint := strings.TrimRight(f.baseURL, "/") + path
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(resp.Body)
		message := strings.TrimSpace(string(payload))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("vision ingest status %d: %s", resp.StatusCode, message)
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*json.RawMessage); ok {
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*raw = payload
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (f *remoteFetcher) getResolvedChannel(sourceID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch, ok := f.resolved[sourceID]
	return ch, ok
}

func (f *remoteFetcher) isSynced(sourceID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.synced[sourceID]
}

func (f *remoteFetcher) markSynced(sourceID, channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if channelID != "" {
		f.resolved[sourceID] = channelID
	}
	f.synced[sourceID] = true
}

type ffmpegFetcher struct{}

func (f *ffmpegFetcher) Capture(ctx context.Context, source types.VisionAgentSource) ([]byte, string, error) {
	uri, err := buildStreamURI(source)
	if err != nil {
		return nil, "", err
	}
	if uri == "" {
		return nil, "", fmt.Errorf("source uri is empty")
	}
	args := []string{"-hide_banner", "-loglevel", "error"}
	if strings.EqualFold(source.Protocol, "rtsp") {
		args = append(args, "-rtsp_transport", "tcp")
	}
	args = append(args, "-i", uri, "-frames:v", "1", "-f", "image2pipe", "-vcodec", "mjpeg", "-")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, "", err
	}
	return output, "image/jpeg", nil
}

func buildStreamURI(source types.VisionAgentSource) (string, error) {
	protocol := strings.ToLower(strings.TrimSpace(source.Protocol))
	switch protocol {
	case "rtsp", "rtmp", "hls", "http-flv", "httpflv", "":
	default:
		return "", fmt.Errorf("unsupported protocol: %s", protocol)
	}
	uri := strings.TrimSpace(source.SourceURI)
	if uri == "" {
		return "", nil
	}
	username := strings.TrimSpace(source.Username)
	if username == "" {
		return uri, nil
	}
	if strings.Contains(uri, "://") {
		parts := strings.SplitN(uri, "://", 2)
		if len(parts) != 2 {
			return uri, nil
		}
		if strings.Contains(parts[1], "@") {
			return uri, nil
		}
		userInfo := url.UserPassword(username, source.Password)
		return fmt.Sprintf("%s://%s@%s", parts[0], userInfo.String(), parts[1]), nil
	}
	return uri, nil
}

func ioReadAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}
