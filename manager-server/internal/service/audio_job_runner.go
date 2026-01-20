package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/datatypes"

	"manager-server/internal/audio/navidrome"
	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
)

type AudioJobRunner struct {
	service *AudioService

	queue chan uint64
	stop  chan struct{}

	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

// NewAudioJobRunner wires an AudioService into an asynchronous dispatcher.
func NewAudioJobRunner(service *AudioService) *AudioJobRunner {
	return &AudioJobRunner{
		service: service,
		queue:   make(chan uint64, 64),
		stop:    make(chan struct{}),
	}
}

// Start spins up worker goroutines.
func (r *AudioJobRunner) Start(workerCount int) {
	if workerCount <= 0 {
		workerCount = 1
	}
	r.startOnce.Do(func() {
		for i := 0; i < workerCount; i++ {
			r.wg.Add(1)
			go r.worker()
		}
	})
}

// Stop waits for active workers to exit.
func (r *AudioJobRunner) Stop() {
	r.stopOnce.Do(func() {
		close(r.stop)
	})
	r.wg.Wait()
}

// Enqueue implements AudioJobDispatcher.
func (r *AudioJobRunner) Enqueue(ctx context.Context, jobID uint64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.stop:
		return errors.New("audio job runner stopped")
	case r.queue <- jobID:
		return nil
	}
}

func (r *AudioJobRunner) worker() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stop:
			return
		case jobID := <-r.queue:
			if err := r.processJob(context.Background(), jobID); err != nil {
				logger.Warnf("audio job %d failed: %v", jobID, err)
			}
		}
	}
}

func (r *AudioJobRunner) processJob(ctx context.Context, jobID uint64) error {
	job, err := r.service.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("fetch job %d: %w", jobID, err)
	}

	switch job.JobType {
	case "ingestion":
		return r.handleIngestion(ctx, job)
	case "transcript":
		return r.handleTranscript(ctx, job)
	case "transcode":
		return r.handleTranscode(ctx, job)
	default:
		return r.markSkipped(ctx, job.ID)
	}
}

func (r *AudioJobRunner) handleIngestion(ctx context.Context, job *models.AudioJob) error {
	if job.EpisodeID == nil {
		return r.failJob(ctx, job, errors.New("ingestion job missing episode id"))
	}
	episode, err := r.service.repo.GetEpisodeByID(ctx, *job.EpisodeID)
	if err != nil {
		return r.failJob(ctx, job, fmt.Errorf("load episode: %w", err))
	}

	var payload map[string]any
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return r.failJob(ctx, job, fmt.Errorf("parse payload: %w", err))
		}
	}

	start := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:    strPtr("processing"),
		Progress:  intPtr(5),
		StartedAt: &start,
	}); err != nil {
		return fmt.Errorf("update job start: %w", err)
	}

	// Ensure audio file stored locally.
	if strings.TrimSpace(episode.FileURI) == "" && episode.SourceType == "url" {
		sourceURL, _ := payload["sourceUrl"].(string)
		if sourceURL == "" {
			return r.failJob(ctx, job, errors.New("missing source url for url ingestion"))
		}
		tempFile, err := os.CreateTemp("", "audio_import_*.tmp")
		if err != nil {
			return r.failJob(ctx, job, fmt.Errorf("create temp file: %w", err))
		}
		defer func() {
			tempFile.Close()
			_ = os.Remove(tempFile.Name())
		}()

		if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
			return r.failJob(ctx, job, fmt.Errorf("seek temp file: %w", err))
		}
		bytesWritten, err := r.service.downloadToWriter(ctx, sourceURL, tempFile)
		if err != nil {
			return r.failJob(ctx, job, fmt.Errorf("download source: %w", err))
		}

		if err := r.service.checkStorageQuota(ctx, episode.ProjectID, bytesWritten); err != nil {
			return r.failJob(ctx, job, err)
		}

		if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
			return r.failJob(ctx, job, fmt.Errorf("rewind temp file: %w", err))
		}
		saveResult, err := r.service.storage.SaveEpisode(ctx, episode.ProjectID, &storage.DocumentFile{
			Name:   deriveTitleFromFile(sourceURL) + filepath.Ext(sourceURL),
			Reader: tempFile,
			Size:   bytesWritten,
		})
		if err != nil {
			return r.failJob(ctx, job, fmt.Errorf("persist downloaded audio: %w", err))
		}
		episode.FileURI = saveResult.URI
		episode.FileSize = saveResult.Size
		if err := r.service.repo.UpdateEpisode(ctx, episode); err != nil {
			return r.failJob(ctx, job, fmt.Errorf("update episode storage: %w", err))
		}
	}

	if strings.TrimSpace(episode.FileURI) == "" {
		return r.failJob(ctx, job, errors.New("episode missing audio payload"))
	}
	filePath, err := r.service.storage.ResolvePath(episode.FileURI)
	// take snapshot of path to reopen after scanning
	if err != nil {
		return r.failJob(ctx, job, fmt.Errorf("resolve audio path: %w", err))
	}

	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:   strPtr("scanning"),
		Progress: intPtr(25),
	}); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	var scan *navidrome.ScanResult
	if r.service.scanner != nil {
		if scan, err = r.service.scanner.Scan(ctx, filePath); err != nil {
			logger.Warnf("scanner failed for job=%d episode=%d: %v", job.ID, episode.ID, err)
		}
	}

	if scan != nil {
		applyScanResult(episode, scan)
		if scan.Artwork != nil && len(scan.Artwork.Data) > 0 {
			if err := r.persistArtwork(ctx, episode, scan.Artwork); err != nil {
				logger.Warnf("store artwork failed: %v", err)
			}
		}
		// Update metadata JSON
		if err := mergeEpisodeMetadata(episode, scan.Metadata); err != nil {
			logger.Warnf("merge metadata failed: %v", err)
		}
	}

	if r.service.config.TranscodingEnabled && r.service.transcoder != nil {
		if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
			Status:   strPtr("transcoding"),
			Progress: intPtr(60),
		}); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}
		if err := r.performTranscode(ctx, episode, filePath); err != nil {
			logger.Warnf("transcode failed job=%d episode=%d: %v", job.ID, episode.ID, err)
		}
	}

	episode.ParseStatus = "ready"
	episode.ParseError = ""
	if err := r.service.repo.UpdateEpisode(ctx, episode); err != nil {
		return r.failJob(ctx, job, fmt.Errorf("save episode: %w", err))
	}

	completed := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:      strPtr("completed"),
		Progress:    intPtr(100),
		CompletedAt: &completed,
	}); err != nil {
		return fmt.Errorf("finalize job: %w", err)
	}

	r.service.scheduleTranscription(ctx, episode)
	return nil
}

func (r *AudioJobRunner) handleTranscript(ctx context.Context, job *models.AudioJob) error {
	if job.EpisodeID == nil {
		return r.failJob(ctx, job, errors.New("transcript job missing episode id"))
	}
	episode, err := r.service.repo.GetEpisodeByID(ctx, *job.EpisodeID)
	if err != nil {
		return r.failJob(ctx, job, fmt.Errorf("load episode: %w", err))
	}

	start := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:    strPtr("processing"),
		Progress:  intPtr(10),
		StartedAt: &start,
	}); err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	content := "自动生成的文字稿将在后续任务中替换此占位文本。\n"
	reader := bytes.NewReader([]byte(content))
	result, err := r.service.storage.SaveEpisodeAsset(ctx, episode.ProjectID, episode.ID, "transcripts", &storage.DocumentFile{
		Name:   fmt.Sprintf("episode_%d.txt", episode.ID),
		Reader: reader,
		Size:   int64(len(content)),
	})
	if err != nil {
		return r.failJob(ctx, job, fmt.Errorf("store transcript: %w", err))
	}
	episode.TranscriptURI = result.URI
	if err := r.service.repo.UpdateEpisode(ctx, episode); err != nil {
		return r.failJob(ctx, job, fmt.Errorf("update episode transcript: %w", err))
	}

	completed := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:      strPtr("completed"),
		Progress:    intPtr(100),
		CompletedAt: &completed,
	}); err != nil {
		return fmt.Errorf("finalize transcript job: %w", err)
	}
	return nil
}

func (r *AudioJobRunner) handleTranscode(ctx context.Context, job *models.AudioJob) error {
	if job.EpisodeID == nil {
		return r.failJob(ctx, job, errors.New("transcode job missing episode id"))
	}
	episode, err := r.service.repo.GetEpisodeByID(ctx, *job.EpisodeID)
	if err != nil {
		return r.failJob(ctx, job, fmt.Errorf("load episode: %w", err))
	}
	if strings.TrimSpace(episode.FileURI) == "" {
		return r.failJob(ctx, job, errors.New("episode has no audio file"))
	}
	filePath, err := r.service.storage.ResolvePath(episode.FileURI)
	if err != nil {
		return r.failJob(ctx, job, fmt.Errorf("resolve file: %w", err))
	}
	start := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:    strPtr("processing"),
		Progress:  intPtr(20),
		StartedAt: &start,
	}); err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	if err := r.performTranscode(ctx, episode, filePath); err != nil {
		return r.failJob(ctx, job, fmt.Errorf("transcode: %w", err))
	}

	completed := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:      strPtr("completed"),
		Progress:    intPtr(100),
		CompletedAt: &completed,
	}); err != nil {
		return fmt.Errorf("finalize transcode job: %w", err)
	}
	return nil
}

func (r *AudioJobRunner) failJob(ctx context.Context, job *models.AudioJob, failure error) error {
	msg := failure.Error()
	now := time.Now()
	if err := r.service.repo.UpdateJob(ctx, job.ID, repository.AudioJobUpdate{
		Status:       strPtr("failed"),
		ErrorMessage: &msg,
		CompletedAt:  &now,
	}); err != nil {
		logger.Warnf("fail job update error: %v", err)
	}
	if job.EpisodeID != nil {
		if episode, err := r.service.repo.GetEpisodeByID(ctx, *job.EpisodeID); err == nil {
			episode.ParseStatus = "error"
			episode.ParseError = msg
			_ = r.service.repo.UpdateEpisode(ctx, episode)
		}
	}
	return failure
}

func (r *AudioJobRunner) markSkipped(ctx context.Context, jobID uint64) error {
	now := time.Now()
	return r.service.repo.UpdateJob(ctx, jobID, repository.AudioJobUpdate{
		Status:      strPtr("skipped"),
		CompletedAt: &now,
	})
}

func (r *AudioJobRunner) persistArtwork(ctx context.Context, episode *models.AudioEpisode, art *navidrome.Artwork) error {
	reader := bytes.NewReader(art.Data)
	doc := &storage.DocumentFile{
		Name:        fmt.Sprintf("episode_%d_cover", episode.ID),
		ContentType: art.MIMEType,
		Reader:      reader,
		Size:        int64(len(art.Data)),
	}
	result, err := r.service.storage.SaveEpisodeAsset(ctx, episode.ProjectID, episode.ID, "covers", doc)
	if err != nil {
		return err
	}
	episode.CoverURI = result.URI
	return nil
}

func (r *AudioJobRunner) performTranscode(ctx context.Context, episode *models.AudioEpisode, inputPath string) error {
	if r.service.transcoder == nil {
		return nil
	}
	opts := navidrome.TranscodeOptions{
		OutputDir:    filepath.Dir(inputPath),
		TargetFormat: r.service.config.TranscodeFormat,
		Bitrate:      r.service.config.TranscodeBitrate,
	}
	result, err := r.service.transcoder.Transcode(ctx, inputPath, opts)
	if err != nil {
		return err
	}
	file, err := os.Open(result.Path)
	if err != nil {
		return fmt.Errorf("open transcoded output: %w", err)
	}
	defer func() {
		file.Close()
		_ = os.Remove(result.Path)
	}()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat transcoded output: %w", err)
	}

	saveResult, err := r.service.storage.SaveEpisodeAsset(ctx, episode.ProjectID, episode.ID, "transcodes", &storage.DocumentFile{
		Name:   filepath.Base(result.Path),
		Reader: file,
		Size:   stat.Size(),
	})
	if err != nil {
		return fmt.Errorf("store transcoded asset: %w", err)
	}

	meta, err := audioJSONToMap(episode.Metadata)
	if err != nil {
		meta = map[string]any{}
	}
	meta["transcodedUri"] = saveResult.URI
	if err := mergeEpisodeMetadata(episode, meta); err != nil {
		logger.Warnf("merge transcoded metadata: %v", err)
	}
	return nil
}

func applyScanResult(ep *models.AudioEpisode, scan *navidrome.ScanResult) {
	if scan.EpisodeTitle != "" {
		ep.EpisodeTitle = scan.EpisodeTitle
	}
	if scan.ShowTitle != "" {
		ep.ShowTitle = scan.ShowTitle
	}
	if scan.PrimaryHost != "" {
		ep.PrimaryHost = scan.PrimaryHost
	}
	if scan.Category != "" {
		ep.Category = scan.Category
	}
	if scan.PublishAt != nil {
		ep.PublishAt = scan.PublishAt
	}
	if scan.Season > 0 {
		ep.SeasonNumber = scan.Season
	}
	if scan.Episode > 0 {
		ep.EpisodeNumber = scan.Episode
	}
	if scan.DurationSeconds > 0 {
		ep.Duration = scan.DurationSeconds
	}
	if scan.Bitrate > 0 {
		ep.Bitrate = scan.Bitrate
	}
}

func mergeEpisodeMetadata(ep *models.AudioEpisode, values map[string]any) error {
	if values == nil {
		return nil
	}
	meta, err := audioJSONToMap(ep.Metadata)
	if err != nil {
		meta = map[string]any{}
	}
	for k, v := range values {
		meta[k] = v
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	ep.Metadata = datatypes.JSON(raw)
	return nil
}

func audioJSONToMap(data datatypes.JSON) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func strPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}
