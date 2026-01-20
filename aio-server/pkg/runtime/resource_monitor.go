package runtime

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"
)

const (
	resourceMonitorInterval = time.Minute
	resourceSampleWindow    = 5
	resourceTrendMinSamples = 3
	heapLeakMinBytes        = 50 * 1024 * 1024
	heapLeakMinRatio        = 0.25
	goroutineLeakMin        = 100
	goroutineLeakMinRatio   = 0.25
	trendMinR2              = 0.6
	profileCooldown         = 10 * time.Minute
)

type resourceSample struct {
	heapAlloc   uint64
	heapInuse   uint64
	heapIdle    uint64
	rssBytes    uint64
	stackInuse  uint64
	sys         uint64
	heapObjects uint64
	numGC       uint32
	lastGC      uint64
	goroutines  int
}

func (r *Runtime) startResourceMonitor() {
	r.mu.Lock()
	if r.monitorCancel != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.monitorCancel = cancel
	r.monitorDone = done
	r.mu.Unlock()

	log.Printf("[aio-monitor] resource monitor started (interval=%s)", resourceMonitorInterval)

	go r.runResourceMonitor(ctx, done)
}

func (r *Runtime) stopResourceMonitor() {
	r.mu.Lock()
	cancel := r.monitorCancel
	done := r.monitorDone
	r.monitorCancel = nil
	r.monitorDone = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
		log.Printf("[aio-monitor] resource monitor stopped")
	}
}

func (r *Runtime) runResourceMonitor(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(resourceMonitorInterval)
	defer ticker.Stop()

	var samples []resourceSample
	var lastProfileAt time.Time
	record := func(sample resourceSample) {
		logResourceSample(sample)
		samples = append(samples, sample)
		if len(samples) > resourceSampleWindow {
			samples = samples[len(samples)-resourceSampleWindow:]
		}
		if reasons := detectLeak(samples); len(reasons) > 0 {
			log.Printf("[aio-monitor] possible leak detected: %s", strings.Join(reasons, "; "))
			if time.Since(lastProfileAt) >= profileCooldown {
				if err := writeLeakProfiles(); err != nil {
					log.Printf("[aio-monitor] leak profile capture failed: %v", err)
				} else {
					lastProfileAt = time.Now()
				}
			}
		}
	}

	record(readResourceSample())

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			record(readResourceSample())
		}
	}
}

func readResourceSample() resourceSample {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rssBytes, _ := readRSSBytes()
	return resourceSample{
		heapAlloc:   ms.HeapAlloc,
		heapInuse:   ms.HeapInuse,
		heapIdle:    ms.HeapIdle,
		rssBytes:    rssBytes,
		stackInuse:  ms.StackInuse,
		sys:         ms.Sys,
		heapObjects: ms.HeapObjects,
		numGC:       ms.NumGC,
		lastGC:      ms.LastGC,
		goroutines:  runtime.NumGoroutine(),
	}
}

func logResourceSample(sample resourceSample) {
	lastGC := "never"
	if sample.lastGC > 0 {
		lastGC = time.Since(time.Unix(0, int64(sample.lastGC))).Round(time.Second).String()
	}
	rss := "n/a"
	if sample.rssBytes > 0 {
		rss = formatBytes(sample.rssBytes)
	}
	log.Printf(
		"[aio-monitor] resources: goroutines=%d heap_alloc=%s heap_inuse=%s heap_idle=%s rss=%s stack_inuse=%s sys=%s heap_objects=%d gc=%d last_gc=%s",
		sample.goroutines,
		formatBytes(sample.heapAlloc),
		formatBytes(sample.heapInuse),
		formatBytes(sample.heapIdle),
		rss,
		formatBytes(sample.stackInuse),
		formatBytes(sample.sys),
		sample.heapObjects,
		sample.numGC,
		lastGC,
	)
}

func detectLeak(samples []resourceSample) []string {
	if len(samples) < resourceSampleWindow {
		return nil
	}

	var reasons []string

	if postGC, ok := samplesAfterGC(samples); ok && len(postGC) >= resourceTrendMinSamples {
		heapAllocTrend := computeTrend(postGC, func(sample resourceSample) float64 {
			return float64(sample.heapAlloc)
		})
		heapInuseTrend := computeTrend(postGC, func(sample resourceSample) float64 {
			return float64(sample.heapInuse)
		})
		heapObjectsTrend := computeTrend(postGC, func(sample resourceSample) float64 {
			return float64(sample.heapObjects)
		})

		heapAllocUp := trendUp(heapAllocTrend, float64(heapLeakMinBytes), heapLeakMinRatio)
		heapInuseUp := trendUp(heapInuseTrend, 0, 0)
		heapObjectsUp := trendUp(heapObjectsTrend, 0, 0)

		if heapAllocUp && (heapInuseUp || heapObjectsUp) {
			reasons = append(reasons, fmt.Sprintf("heap_alloc +%s (from %s to %s)", formatBytes(uint64(heapAllocTrend.delta)), formatBytes(uint64(heapAllocTrend.base)), formatBytes(uint64(heapAllocTrend.last))))
			if heapInuseUp {
				reasons = append(reasons, fmt.Sprintf("heap_inuse +%s (from %s to %s)", formatBytes(uint64(heapInuseTrend.delta)), formatBytes(uint64(heapInuseTrend.base)), formatBytes(uint64(heapInuseTrend.last))))
			}
			if heapObjectsUp {
				reasons = append(reasons, fmt.Sprintf("heap_objects +%s (from %s to %s)", formatCount(heapObjectsTrend.delta), formatCount(heapObjectsTrend.base), formatCount(heapObjectsTrend.last)))
			}
		}
	}

	goroutineTrend := computeTrend(samples, func(sample resourceSample) float64 {
		return float64(sample.goroutines)
	})
	if trendUp(goroutineTrend, float64(goroutineLeakMin), goroutineLeakMinRatio) {
		reasons = append(reasons, fmt.Sprintf("goroutines +%d (from %d to %d)", int64(goroutineTrend.delta), int64(goroutineTrend.base), int64(goroutineTrend.last)))
	}

	if len(reasons) == 0 {
		return nil
	}

	if rssReason := detectRSSTrend(samples); rssReason != "" {
		reasons = append(reasons, rssReason)
	}

	return reasons
}

func formatBytes(value uint64) string {
	return fmt.Sprintf("%.1fMB", float64(value)/(1024*1024))
}

func formatCount(value float64) string {
	return fmt.Sprintf("%.0f", value)
}

type trendStats struct {
	base  float64
	last  float64
	delta float64
	slope float64
	r2    float64
}

func computeTrend(samples []resourceSample, getter func(resourceSample) float64) trendStats {
	if len(samples) == 0 {
		return trendStats{}
	}
	values := make([]float64, len(samples))
	for i, sample := range samples {
		values[i] = getter(sample)
	}
	stats := trendStats{
		base:  values[0],
		last:  values[len(values)-1],
		delta: values[len(values)-1] - values[0],
	}
	stats.slope, stats.r2 = linearTrend(values)
	return stats
}

func trendUp(stats trendStats, minAbs float64, minRatio float64) bool {
	if stats.delta <= 0 || stats.slope <= 0 {
		return false
	}
	if stats.r2 < trendMinR2 {
		return false
	}
	if minAbs > 0 || minRatio > 0 {
		if stats.delta < minAbs && ratio(stats.base, stats.delta) < minRatio {
			return false
		}
	}
	return true
}

func linearTrend(values []float64) (float64, float64) {
	n := float64(len(values))
	if n < 2 {
		return 0, 0
	}
	var sumX, sumY, sumXY, sumX2 float64
	for i, value := range values {
		x := float64(i)
		sumX += x
		sumY += value
		sumXY += x * value
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, 0
	}
	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n
	meanY := sumY / n

	var ssTot, ssRes float64
	for i, value := range values {
		x := float64(i)
		predicted := slope*x + intercept
		diff := value - meanY
		ssTot += diff * diff
		resid := value - predicted
		ssRes += resid * resid
	}
	if ssTot == 0 {
		return slope, 1
	}
	r2 := 1 - ssRes/ssTot
	if r2 < 0 {
		r2 = 0
	}
	return slope, r2
}

func ratio(base, delta float64) float64 {
	if base == 0 {
		return 1
	}
	return delta / base
}

func samplesAfterGC(samples []resourceSample) ([]resourceSample, bool) {
	if len(samples) == 0 {
		return nil, false
	}
	base := samples[0].numGC
	for i := 1; i < len(samples); i++ {
		if samples[i].numGC > base {
			return samples[i:], true
		}
	}
	return nil, false
}

func detectRSSTrend(samples []resourceSample) string {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if sample.rssBytes == 0 {
			return ""
		}
		values = append(values, float64(sample.rssBytes))
	}
	stats := trendStats{
		base:  values[0],
		last:  values[len(values)-1],
		delta: values[len(values)-1] - values[0],
	}
	stats.slope, stats.r2 = linearTrend(values)
	if !trendUp(stats, float64(heapLeakMinBytes), heapLeakMinRatio) {
		return ""
	}
	return fmt.Sprintf("rss +%s (from %s to %s)", formatBytes(uint64(stats.delta)), formatBytes(uint64(stats.base)), formatBytes(uint64(stats.last)))
}

func writeLeakProfiles() error {
	dir := filepath.Join("logs", "monitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ts := time.Now().Format("20060102-150405")
	heapPath := filepath.Join(dir, fmt.Sprintf("heap-%s.pprof", ts))
	goroutinePath := filepath.Join(dir, fmt.Sprintf("goroutine-%s.txt", ts))

	runtime.GC()

	heapFile, err := os.Create(heapPath)
	if err != nil {
		return err
	}
	if err := pprof.WriteHeapProfile(heapFile); err != nil {
		_ = heapFile.Close()
		return err
	}
	if err := heapFile.Close(); err != nil {
		return err
	}

	goroutineFile, err := os.Create(goroutinePath)
	if err != nil {
		return err
	}
	if err := pprof.Lookup("goroutine").WriteTo(goroutineFile, 2); err != nil {
		_ = goroutineFile.Close()
		return err
	}
	if err := goroutineFile.Close(); err != nil {
		return err
	}

	log.Printf("[aio-monitor] leak profiles written: heap=%s goroutine=%s", heapPath, goroutinePath)
	return nil
}
