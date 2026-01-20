package service

import (
	"context"
	"time"

	"manager-server/internal/logger"
	"manager-server/internal/repository"
)

type VisionCleanupRunner struct {
	repo      repository.VisionEventRepository
	retention time.Duration
	interval  time.Duration
	stop      chan struct{}
}

func NewVisionCleanupRunner(repo repository.VisionEventRepository, retentionDays int, intervalMinutes int) *VisionCleanupRunner {
	if repo == nil || retentionDays <= 0 {
		return nil
	}
	interval := time.Duration(intervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	return &VisionCleanupRunner{
		repo:      repo,
		retention: time.Duration(retentionDays) * 24 * time.Hour,
		interval:  interval,
		stop:      make(chan struct{}),
	}
}

func (r *VisionCleanupRunner) Start() {
	if r == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		r.cleanup()
		for {
			select {
			case <-ticker.C:
				r.cleanup()
			case <-r.stop:
				return
			}
		}
	}()
}

func (r *VisionCleanupRunner) Stop() {
	if r == nil {
		return
	}
	close(r.stop)
}

func (r *VisionCleanupRunner) cleanup() {
	if r == nil {
		return
	}
	cutoff := time.Now().Add(-r.retention)
	count, err := r.repo.DeleteOlderThan(context.Background(), cutoff)
	if err != nil {
		logger.Warnf("vision cleanup failed: %v", err)
		return
	}
	if count > 0 {
		logger.Infof("vision cleanup removed %d events before %s", count, cutoff.Format(time.RFC3339))
	}
}
