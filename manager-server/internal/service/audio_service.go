package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"

	amodels "manager-server/internal/audio/models"
	"manager-server/internal/audio/navidrome"
	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
)

// AudioJobDispatcher represents the asynchronous job runner contract.
type AudioJobDispatcher interface {
	Enqueue(ctx context.Context, jobID uint64) error
}

// AudioServiceConfig captures operational parameters.
type AudioServiceConfig struct {
	MaxUploadBytes     int64
	AllowedFormats     []string
	StorageQuotaBytes  int64
	DownloadTimeout    time.Duration
	TranscodingEnabled bool
	TranscodeFormat    string
	TranscodeBitrate   int
	TokenSecret        string
	TokenTTL           time.Duration
}

// AudioService orchestrates podcast domain workflows.
type AudioService struct {
	repo        repository.AudioRepository
	storage     storage.AudioStorage
	jobRunner   AudioJobDispatcher
	scanner     navidrome.Scanner
	transcoder  navidrome.Transcoder
	httpClient  *http.Client
	config      AudioServiceConfig
	formatSet   map[string]struct{}
	tokenSigner *streamTokenSigner
}

// StreamOptions describes the desired output format for episode playback.
type StreamOptions struct {
	Format         string
	MaxBitRateKbps int
}

// StreamResult exposes the resolved asset for playback.
type StreamResult struct {
	Path        string
	ContentType string
	FileName    string
	Format      string
	Cleanup     func()
}

// NewAudioService constructs an AudioService.
func NewAudioService(repo repository.AudioRepository, storage storage.AudioStorage, jobRunner AudioJobDispatcher, scanner navidrome.Scanner, transcoder navidrome.Transcoder, cfg AudioServiceConfig) *AudioService {
	formatSet := make(map[string]struct{})
	for _, ext := range cfg.AllowedFormats {
		if norm := normalizeExtension(ext); norm != "" {
			formatSet[norm] = struct{}{}
		}
	}
	if cfg.DownloadTimeout <= 0 {
		cfg.DownloadTimeout = 2 * time.Minute
	}
	httpClient := &http.Client{
		Timeout: cfg.DownloadTimeout,
	}
	var signer *streamTokenSigner
	if strings.TrimSpace(cfg.TokenSecret) != "" {
		ttl := cfg.TokenTTL
		if ttl <= 0 {
			ttl = 15 * time.Minute
		}
		signer = newStreamTokenSigner(cfg.TokenSecret, ttl)
	}
	return &AudioService{
		repo:        repo,
		storage:     storage,
		scanner:     scanner,
		transcoder:  transcoder,
		httpClient:  httpClient,
		config:      cfg,
		formatSet:   formatSet,
		tokenSigner: signer,
		jobRunner:   jobRunner,
	}
}

// SetJobRunner injects or replaces the asynchronous dispatcher.
func (s *AudioService) SetJobRunner(r AudioJobDispatcher) {
	s.jobRunner = r
}

// AudioProjectSummary enriches project data with high level metrics.
type AudioProjectSummary struct {
	*amodels.Project
	EpisodeCount  int64 `json:"episodeCount"`
	PlaylistCount int64 `json:"playlistCount"`
	StorageBytes  int64 `json:"storageBytes"`
}

// CreateAudioProjectInput captures project creation payload.
type CreateAudioProjectInput struct {
	Name       string
	Visibility string
	Metadata   map[string]any
}

// UpdateAudioProjectInput captures project updates.
type UpdateAudioProjectInput struct {
	Name       *string
	Visibility *string
	Metadata   map[string]any
}

// UploadEpisodeInput represents an uploaded audio payload.
type UploadEpisodeInput struct {
	ProjectID   string
	FileName    string
	Size        int64
	ContentType string
	Reader      io.ReadCloser
	UploaderID  uint64
}

// SubmitEpisodeURLInput captures URL ingestion requests.
type SubmitEpisodeURLInput struct {
	ProjectID  string
	URL        string
	Title      string
	UploaderID uint64
}

// UpdateEpisodeInput captures mutable episode fields.
type UpdateEpisodeInput struct {
	EpisodeTitle  *string
	ShowTitle     *string
	PrimaryHost   *string
	Category      *string
	PublishAt     *time.Time
	SeasonNumber  *int
	EpisodeNumber *int
	CoverURI      *string
	TranscriptURI *string
	ParseStatus   *string
	Metadata      map[string]any
}

// PlaylistMutationInput captures playlist updates.
type PlaylistMutationInput struct {
	Name        *string
	Description *string
	CoverURI    *string
	Metadata    map[string]any
}

// PlaylistEpisodeOrder represents playlist ordering.
type PlaylistEpisodeOrder struct {
	EpisodeID uint64 `json:"episodeId"`
	Order     int    `json:"order"`
}

// CreateProject registers a new audio project scoped to the actor.
func (s *AudioService) CreateProject(ctx context.Context, actor ActorContext, input CreateAudioProjectInput) (*amodels.Project, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, errors.New("project name is required")
	}
	project := &models.AudioProject{
		OwnerID:    actor.UserID,
		Name:       input.Name,
		Visibility: defaultVisibility(input.Visibility),
	}
	if input.Metadata != nil {
		payload, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		project.Metadata = datatypes.JSON(payload)
	}
	if err := s.repo.CreateProject(ctx, project); err != nil {
		return nil, err
	}
	return (*amodels.Project)(project), nil
}

// UpdateProject mutates an existing audio project.
func (s *AudioService) UpdateProject(ctx context.Context, actor ActorContext, projectID string, input UpdateAudioProjectInput) (*amodels.Project, error) {
	project, err := s.ensureProjectAccess(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		if trimmed := strings.TrimSpace(*input.Name); trimmed != "" {
			project.Name = trimmed
		}
	}
	if input.Visibility != nil {
		project.Visibility = defaultVisibility(*input.Visibility)
	}
	if input.Metadata != nil {
		payload, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		project.Metadata = datatypes.JSON(payload)
	}
	if err := s.repo.UpdateProject(ctx, project); err != nil {
		return nil, err
	}
	return (*amodels.Project)(project), nil
}

// DeleteProject performs a soft delete.
func (s *AudioService) DeleteProject(ctx context.Context, actor ActorContext, projectID string) error {
	if _, err := s.ensureProjectAccess(ctx, actor, projectID); err != nil {
		return err
	}
	return s.repo.SoftDeleteProject(ctx, projectID)
}

// ListProjects returns projects accessible by the caller.
func (s *AudioService) ListProjects(ctx context.Context, actor ActorContext, filter repository.AudioProjectFilter, limit, offset int) ([]*AudioProjectSummary, int64, error) {
	if !actor.IsAdmin {
		filter.OwnerID = &actor.UserID
	}
	projects, total, err := s.repo.ListProjects(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if len(projects) == 0 {
		return []*AudioProjectSummary{}, 0, nil
	}
	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	metrics, err := s.repo.ProjectMetrics(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	summaries := make([]*AudioProjectSummary, 0, len(projects))
	for _, project := range projects {
		metric := metrics[project.ID]
		copied := *project
		summaries = append(summaries, &AudioProjectSummary{
			Project:       (*amodels.Project)(&copied),
			EpisodeCount:  metric.EpisodeCount,
			PlaylistCount: metric.PlaylistCount,
			StorageBytes:  metric.TotalBytes,
		})
	}
	return summaries, total, nil
}

func (s *AudioService) GetProject(ctx context.Context, actor ActorContext, projectID string) (*AudioProjectSummary, error) {
	project, err := s.ensureProjectAccess(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	metrics, err := s.repo.ProjectMetrics(ctx, []string{projectID})
	if err != nil {
		return nil, err
	}
	metric := metrics[projectID]
	copied := *project
	return &AudioProjectSummary{
		Project:       (*amodels.Project)(&copied),
		EpisodeCount:  metric.EpisodeCount,
		PlaylistCount: metric.PlaylistCount,
		StorageBytes:  metric.TotalBytes,
	}, nil
}

// UploadEpisode persists the uploaded audio file and enqueues ingestion.
func (s *AudioService) UploadEpisode(ctx context.Context, actor ActorContext, input UploadEpisodeInput) (*amodels.Episode, error) {
	project, err := s.ensureProjectAccess(ctx, actor, input.ProjectID)
	if err != nil {
		return nil, err
	}
	if input.Reader != nil {
		defer input.Reader.Close()
	}

	if s.config.MaxUploadBytes > 0 && input.Size > s.config.MaxUploadBytes {
		return nil, fmt.Errorf("file exceeds limit of %d bytes", s.config.MaxUploadBytes)
	}
	if err := s.checkStorageQuota(ctx, project.ID, input.Size); err != nil {
		return nil, err
	}

	if err := s.validateExtension(input.FileName); err != nil {
		return nil, err
	}

	docFile, cleanup, err := s.prepareUploadDocument(ctx, input)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	saveResult, err := s.storage.SaveEpisode(ctx, project.ID, docFile)
	if err != nil {
		return nil, fmt.Errorf("store episode: %w", err)
	}

	episode := &models.AudioEpisode{
		ProjectID:    project.ID,
		EpisodeTitle: deriveTitleFromFile(input.FileName),
		FileURI:      saveResult.URI,
		SourceType:   "upload",
		FileSize:     saveResult.Size,
		ParseStatus:  "queued",
	}

	if err := s.repo.CreateEpisode(ctx, episode); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"uploaderId": actor.UserID,
		"fileUri":    episode.FileURI,
		"fileName":   input.FileName,
	}
	job := &models.AudioJob{
		ProjectID: project.ID,
		EpisodeID: &episode.ID,
		JobType:   "ingestion",
		Status:    "queued",
		Payload:   toJSON(payload),
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	if err := s.jobRunner.Enqueue(ctx, job.ID); err != nil {
		logger.Warnf("enqueue audio job failed: %v", err)
	}
	return (*amodels.Episode)(episode), nil
}

func (s *AudioService) prepareUploadDocument(ctx context.Context, input UploadEpisodeInput) (*storage.DocumentFile, func(), error) {
	if input.Reader == nil {
		return nil, nil, errors.New("upload reader is nil")
	}

	if !s.config.TranscodingEnabled {
		ctype := strings.TrimSpace(input.ContentType)
		if ctype == "" {
			ctype = MediaContentType(filepath.Ext(input.FileName))
		}
		return &storage.DocumentFile{
			Name:        input.FileName,
			Size:        input.Size,
			ContentType: ctype,
			Reader:      input.Reader,
		}, nil, nil
	}

	if s.transcoder == nil {
		return nil, nil, fmt.Errorf("audio transcoder is not configured")
	}

	tmpFile, err := os.CreateTemp("", "audio-upload-src-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp file: %w", err)
	}
	if _, err = io.Copy(tmpFile, input.Reader); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return nil, nil, fmt.Errorf("buffer upload: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, nil, fmt.Errorf("flush temp file: %w", err)
	}

	targetFormat := normalizeFormat(s.config.TranscodeFormat)
	if targetFormat == "" {
		targetFormat = normalizeFormat(filepath.Ext(input.FileName))
	}
	if targetFormat == "" {
		targetFormat = "mp3"
	}

	transcoded, err := s.transcoder.Transcode(ctx, tmpFile.Name(), navidrome.TranscodeOptions{
		OutputDir:    filepath.Dir(tmpFile.Name()),
		TargetFormat: targetFormat,
		Bitrate:      s.config.TranscodeBitrate,
	})
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, nil, fmt.Errorf("transcode upload: %w", err)
	}

	finalPath := transcoded.Path
	finalFormat := targetFormat
	if normalized := normalizeFormat(transcoded.TargetFormat); normalized != "" {
		finalFormat = normalized
	}

	finalFile, err := os.Open(finalPath)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		_ = os.Remove(finalPath)
		return nil, nil, fmt.Errorf("open transcoded payload: %w", err)
	}
	info, err := finalFile.Stat()
	if err != nil {
		finalFile.Close()
		_ = os.Remove(tmpFile.Name())
		_ = os.Remove(finalPath)
		return nil, nil, fmt.Errorf("stat transcoded payload: %w", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(input.FileName), filepath.Ext(input.FileName))
	if baseName == "" {
		baseName = fmt.Sprintf("audio_%d", time.Now().UnixNano())
	}
	finalName := fmt.Sprintf("%s.%s", baseName, finalFormat)

	doc := &storage.DocumentFile{
		Name:        finalName,
		Size:        info.Size(),
		ContentType: MediaContentType(finalFormat),
		Reader:      finalFile,
	}

	cleanup := func() {
		_ = finalFile.Close()
		_ = os.Remove(tmpFile.Name())
		if finalPath != tmpFile.Name() {
			_ = os.Remove(finalPath)
		}
	}

	return doc, cleanup, nil
}

// SubmitEpisodeURL registers a remote episode import.
func (s *AudioService) SubmitEpisodeURL(ctx context.Context, actor ActorContext, input SubmitEpisodeURLInput) (*amodels.Episode, error) {
	if _, err := s.ensureProjectAccess(ctx, actor, input.ProjectID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.URL) == "" {
		return nil, errors.New("url is required")
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid url: %s", input.URL)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = deriveTitleFromFile(parsed.Path)
	}
	episode := &models.AudioEpisode{
		ProjectID:    input.ProjectID,
		EpisodeTitle: title,
		SourceType:   "url",
		ParseStatus:  "queued",
	}
	if err := s.repo.CreateEpisode(ctx, episode); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"sourceUrl": parsed.String(),
		"title":     title,
	}
	job := &models.AudioJob{
		ProjectID: input.ProjectID,
		EpisodeID: &episode.ID,
		JobType:   "ingestion",
		Status:    "queued",
		Payload:   toJSON(payload),
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	if err := s.jobRunner.Enqueue(ctx, job.ID); err != nil {
		logger.Warnf("enqueue audio job failed: %v", err)
	}
	return (*amodels.Episode)(episode), nil
}

// ListEpisodes returns paginated episodes.
func (s *AudioService) ListEpisodes(ctx context.Context, actor ActorContext, filter repository.AudioEpisodeFilter, limit, offset int) ([]*amodels.Episode, int64, error) {
	if _, err := s.ensureProjectAccess(ctx, actor, filter.ProjectID); err != nil {
		return nil, 0, err
	}
	episodes, total, err := s.repo.ListEpisodes(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	results := make([]*amodels.Episode, 0, len(episodes))
	for _, ep := range episodes {
		copied := *ep
		results = append(results, (*amodels.Episode)(&copied))
	}
	return results, total, nil
}

// GetEpisode fetches a single episode ensuring authorization.
func (s *AudioService) GetEpisode(ctx context.Context, actor ActorContext, episodeID uint64) (*amodels.Episode, error) {
	episode, err := s.repo.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, episode.ProjectID); err != nil {
		return nil, err
	}
	return (*amodels.Episode)(episode), nil
}

// UpdateEpisode applies metadata changes.
func (s *AudioService) UpdateEpisode(ctx context.Context, actor ActorContext, episodeID uint64, input UpdateEpisodeInput) (*amodels.Episode, error) {
	episode, err := s.repo.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, episode.ProjectID); err != nil {
		return nil, err
	}

	if input.EpisodeTitle != nil {
		if trimmed := strings.TrimSpace(*input.EpisodeTitle); trimmed != "" {
			episode.EpisodeTitle = trimmed
		}
	}
	if input.ShowTitle != nil {
		episode.ShowTitle = strings.TrimSpace(*input.ShowTitle)
	}
	if input.PrimaryHost != nil {
		episode.PrimaryHost = strings.TrimSpace(*input.PrimaryHost)
	}
	if input.Category != nil {
		episode.Category = strings.TrimSpace(*input.Category)
	}
	if input.PublishAt != nil {
		episode.PublishAt = input.PublishAt
	}
	if input.SeasonNumber != nil {
		episode.SeasonNumber = *input.SeasonNumber
	}
	if input.EpisodeNumber != nil {
		episode.EpisodeNumber = *input.EpisodeNumber
	}
	if input.CoverURI != nil {
		episode.CoverURI = *input.CoverURI
	}
	if input.TranscriptURI != nil {
		episode.TranscriptURI = *input.TranscriptURI
	}
	if input.ParseStatus != nil {
		episode.ParseStatus = *input.ParseStatus
	}
	if input.Metadata != nil {
		payload, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		episode.Metadata = datatypes.JSON(payload)
	}

	if err := s.repo.UpdateEpisode(ctx, episode); err != nil {
		return nil, err
	}
	return (*amodels.Episode)(episode), nil
}

// DeleteEpisode removes an episode softly.
func (s *AudioService) DeleteEpisode(ctx context.Context, actor ActorContext, episodeID uint64) error {
	episode, err := s.repo.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, episode.ProjectID); err != nil {
		return err
	}
	return s.repo.SoftDeleteEpisode(ctx, episodeID)
}

// GenerateStreamToken signs a temporary playback token.
func (s *AudioService) GenerateStreamToken(ctx context.Context, actor ActorContext, episodeID uint64) (string, error) {
	if s.tokenSigner == nil {
		return "", errors.New("stream tokens disabled")
	}
	episode, err := s.repo.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return "", err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, episode.ProjectID); err != nil {
		return "", err
	}
	return s.tokenSigner.Sign(episodeID, actor.UserID)
}

// VerifyStreamToken validates token integrity and expiration.
func (s *AudioService) VerifyStreamToken(token string) (*StreamTokenClaims, error) {
	if s.tokenSigner == nil {
		return nil, errors.New("stream tokens disabled")
	}
	return s.tokenSigner.Verify(token)
}

// ResolveEpisodePath translates an episode into an absolute file path.
func (s *AudioService) ResolveEpisodePath(ctx context.Context, episodeID uint64) (string, *models.AudioEpisode, error) {
	episode, err := s.repo.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(episode.FileURI) == "" {
		return "", nil, fmt.Errorf("episode %d has no associated audio file", episodeID)
	}
	path, err := s.storage.ResolvePath(episode.FileURI)
	if err != nil {
		return "", nil, err
	}
	return path, episode, nil
}

// CreatePlaylist creates a playlist entry.
func (s *AudioService) CreatePlaylist(ctx context.Context, actor ActorContext, projectID string, input PlaylistMutationInput) (*amodels.Playlist, error) {
	if _, err := s.ensureProjectAccess(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if input.Name == nil || strings.TrimSpace(*input.Name) == "" {
		return nil, errors.New("playlist name is required")
	}
	playlist := &models.AudioPlaylist{
		ProjectID:   projectID,
		Name:        strings.TrimSpace(*input.Name),
		Description: getStringOrDefault(input.Description),
		CoverURI:    getStringOrDefault(input.CoverURI),
	}
	if input.Metadata != nil {
		payload, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		playlist.Metadata = datatypes.JSON(payload)
	}
	if err := s.repo.CreatePlaylist(ctx, playlist); err != nil {
		return nil, err
	}
	return (*amodels.Playlist)(playlist), nil
}

// UpdatePlaylist mutates playlist fields.
func (s *AudioService) UpdatePlaylist(ctx context.Context, actor ActorContext, playlistID uint64, input PlaylistMutationInput) (*amodels.Playlist, error) {
	playlist, err := s.repo.GetPlaylistByID(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, playlist.ProjectID); err != nil {
		return nil, err
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		playlist.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		playlist.Description = *input.Description
	}
	if input.CoverURI != nil {
		playlist.CoverURI = *input.CoverURI
	}
	if input.Metadata != nil {
		payload, err := json.Marshal(input.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		playlist.Metadata = datatypes.JSON(payload)
	}
	if err := s.repo.UpdatePlaylist(ctx, playlist); err != nil {
		return nil, err
	}
	return (*amodels.Playlist)(playlist), nil
}

// DeletePlaylist soft deletes a playlist.
func (s *AudioService) DeletePlaylist(ctx context.Context, actor ActorContext, playlistID uint64) error {
	playlist, err := s.repo.GetPlaylistByID(ctx, playlistID)
	if err != nil {
		return err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, playlist.ProjectID); err != nil {
		return err
	}
	return s.repo.SoftDeletePlaylist(ctx, playlistID)
}

// ListPlaylists fetches playlists for a project.
func (s *AudioService) ListPlaylists(ctx context.Context, actor ActorContext, filter repository.AudioPlaylistFilter, limit, offset int) ([]*amodels.Playlist, int64, error) {
	if _, err := s.ensureProjectAccess(ctx, actor, filter.ProjectID); err != nil {
		return nil, 0, err
	}
	playlists, total, err := s.repo.ListPlaylists(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	results := make([]*amodels.Playlist, 0, len(playlists))
	for _, pl := range playlists {
		copied := *pl
		results = append(results, (*amodels.Playlist)(&copied))
	}
	return results, total, nil
}

// ReplacePlaylistEpisodes rewrites playlist ordering.
func (s *AudioService) ReplacePlaylistEpisodes(ctx context.Context, actor ActorContext, playlistID uint64, order []PlaylistEpisodeOrder) error {
	playlist, err := s.repo.GetPlaylistByID(ctx, playlistID)
	if err != nil {
		return err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, playlist.ProjectID); err != nil {
		return err
	}
	entries := make([]models.AudioPlaylistEpisode, 0, len(order))
	for _, item := range order {
		entries = append(entries, models.AudioPlaylistEpisode{
			PlaylistID: playlistID,
			EpisodeID:  item.EpisodeID,
			OrderValue: item.Order,
		})
	}
	return s.repo.ReplacePlaylistEpisodes(ctx, playlistID, entries)
}

// PrepareEpisodeStream resolves the appropriate media asset for playback considering requested format hints.
func (s *AudioService) PrepareEpisodeStream(ctx context.Context, episode *models.AudioEpisode, opts StreamOptions) (*StreamResult, error) {
	if episode == nil {
		return nil, errors.New("episode is nil")
	}
	if strings.TrimSpace(episode.FileURI) == "" {
		return nil, fmt.Errorf("episode %d has no associated audio file", episode.ID)
	}

	originalPath, err := s.storage.ResolvePath(episode.FileURI)
	if err != nil {
		return nil, fmt.Errorf("resolve episode path: %w", err)
	}

	originalFormat := normalizeFormat(filepath.Ext(episode.FileURI))
	targetFormat := normalizeFormat(opts.Format)
	if targetFormat == "" || targetFormat == "raw" {
		targetFormat = originalFormat
	}

	path := originalPath
	actualFormat := originalFormat
	cleanup := func() {}

	meta, err := audioJSONToMap(episode.Metadata)
	if err != nil {
		logger.Warnf("parse episode metadata failed episode=%d: %v", episode.ID, err)
		meta = map[string]any{}
	}

	if targetFormat != "" && targetFormat != actualFormat {
		if uri, ok := extractString(meta, "transcodedUri"); ok {
			if resolved, err := s.storage.ResolvePath(uri); err == nil {
				if resolvedFormat := normalizeFormat(filepath.Ext(resolved)); resolvedFormat == targetFormat {
					path = resolved
					actualFormat = resolvedFormat
				}
			} else {
				logger.Warnf("resolve transcoded asset failed episode=%d uri=%s: %v", episode.ID, uri, err)
			}
		}
	}

	if targetFormat != "" && targetFormat != actualFormat {
		transcoder := s.transcoder
		if transcoder == nil {
			transcoder = &navidrome.FFMPEGTranscoder{}
		}
		if _, ok := transcoder.(navidrome.NoopTranscoder); ok {
			transcoder = &navidrome.FFMPEGTranscoder{}
		}
		if _, ok := transcoder.(*navidrome.NoopTranscoder); ok {
			transcoder = &navidrome.FFMPEGTranscoder{}
		}
		if transcoder == nil {
			return nil, fmt.Errorf("transcoder unavailable for requested format %s", targetFormat)
		}
		bitrate := s.config.TranscodeBitrate
		if opts.MaxBitRateKbps > 0 {
			bitrate = opts.MaxBitRateKbps * 1000
		}
		transOpts := navidrome.TranscodeOptions{
			OutputDir:    os.TempDir(),
			TargetFormat: targetFormat,
			Bitrate:      bitrate,
		}
		result, err := transcoder.Transcode(ctx, path, transOpts)
		if err != nil {
			return nil, fmt.Errorf("transcode episode %d to %s: %w", episode.ID, targetFormat, err)
		}
		path = result.Path
		actualFormat = normalizeFormat(result.TargetFormat)
		if actualFormat == "" {
			actualFormat = targetFormat
		}
		cleanup = func() {
			if err := os.Remove(result.Path); err != nil {
				logger.Warnf("cleanup transcoded file failed path=%s: %v", result.Path, err)
			}
		}
	}

	contentType := MediaContentType(actualFormat)
	fileName := sanitizeFileName(episode.EpisodeTitle, episode.ID, actualFormat)

	return &StreamResult{
		Path:        path,
		ContentType: contentType,
		FileName:    fileName,
		Format:      actualFormat,
		Cleanup:     cleanup,
	}, nil
}

// AddEpisodesToPlaylist appends episodes to a playlist.
func (s *AudioService) AddEpisodesToPlaylist(ctx context.Context, actor ActorContext, playlistID uint64, episodeIDs []uint64) error {
	playlist, err := s.repo.GetPlaylistByID(ctx, playlistID)
	if err != nil {
		return err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, playlist.ProjectID); err != nil {
		return err
	}
	for index, episodeID := range episodeIDs {
		if err := s.repo.CreatePlaylistEpisode(ctx, &models.AudioPlaylistEpisode{
			PlaylistID: playlistID,
			EpisodeID:  episodeID,
			OrderValue: index,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RemoveEpisodeFromPlaylist removes membership.
func (s *AudioService) RemoveEpisodeFromPlaylist(ctx context.Context, actor ActorContext, playlistID, episodeID uint64) error {
	playlist, err := s.repo.GetPlaylistByID(ctx, playlistID)
	if err != nil {
		return err
	}
	if _, err := s.ensureProjectAccess(ctx, actor, playlist.ProjectID); err != nil {
		return err
	}
	return s.repo.DeletePlaylistEpisode(ctx, playlistID, episodeID)
}

// ListJobs returns jobs for debugging.
func (s *AudioService) ListJobs(ctx context.Context, actor ActorContext, filter repository.AudioJobFilter, limit, offset int) ([]*models.AudioJob, int64, error) {
	if _, err := s.ensureProjectAccess(ctx, actor, filter.ProjectID); err != nil {
		return nil, 0, err
	}
	return s.repo.ListJobs(ctx, filter, limit, offset)
}

func (s *AudioService) ensureProjectAccess(ctx context.Context, actor ActorContext, projectID string) (*models.AudioProject, error) {
	project, err := s.repo.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if actor.IsAdmin {
		return project, nil
	}
	if project.OwnerID != actor.UserID {
		return nil, errors.New("用户没有该项目的访问权限")
	}
	return project, nil
}

func (s *AudioService) validateExtension(filename string) error {
	if len(s.formatSet) == 0 {
		return nil
	}
	ext := normalizeExtension(filepath.Ext(filename))
	if ext == "" {
		return errors.New("无法识别的文件格式")
	}
	if _, ok := s.formatSet[ext]; !ok {
		return fmt.Errorf("不支持的文件格式: %s", ext)
	}
	return nil
}

func (s *AudioService) checkStorageQuota(ctx context.Context, projectID string, incoming int64) error {
	if s.config.StorageQuotaBytes <= 0 {
		return nil
	}
	metrics, err := s.repo.ProjectMetrics(ctx, []string{projectID})
	if err != nil {
		return err
	}
	current := metrics[projectID].TotalBytes
	if current+incoming > s.config.StorageQuotaBytes {
		return fmt.Errorf("超出项目存储配额 (当前: %d, 即将上传: %d, 配额: %d)", current, incoming, s.config.StorageQuotaBytes)
	}
	return nil
}

func (s *AudioService) downloadToWriter(ctx context.Context, src string, writer io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("下载失败，状态码: %d", resp.StatusCode)
	}
	n, err := io.Copy(writer, resp.Body)
	return n, err
}

func (s *AudioService) scheduleTranscription(ctx context.Context, episode *models.AudioEpisode) {
	if s.jobRunner == nil {
		return
	}
	payload := map[string]any{
		"episodeId": episode.ID,
	}
	job := &models.AudioJob{
		ProjectID: episode.ProjectID,
		EpisodeID: &episode.ID,
		JobType:   "transcript",
		Status:    "queued",
		Payload:   toJSON(payload),
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		logger.Warnf("enqueue transcript job failed: %v", err)
		return
	}
	if err := s.jobRunner.Enqueue(ctx, job.ID); err != nil {
		logger.Warnf("enqueue transcript job failed: %v", err)
	}
}

func defaultVisibility(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	switch trimmed {
	case "shared", "public":
		return trimmed
	default:
		return "private"
	}
}

func deriveTitleFromFile(filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "未命名音频"
	}
	name = filepath.Base(name)
	if idx := strings.LastIndexByte(name, '.'); idx > 0 {
		return strings.TrimSpace(name[:idx])
	}
	return name
}

func getStringOrDefault(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func toJSON(value map[string]any) datatypes.JSON {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		logger.Warnf("marshal json: %v", err)
		return nil
	}
	return datatypes.JSON(raw)
}

func extractString(meta map[string]any, key string) (string, bool) {
	if meta == nil {
		return "", false
	}
	raw, ok := meta[key]
	if !ok {
		return "", false
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	default:
		return "", false
	}
}

// MediaContentType maps a normalized extension (with or without dot) to a MIME type.
func MediaContentType(format string) string {
	ext := normalizeFormat(format)
	if ext == "" {
		return "application/octet-stream"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if ctype := mime.TypeByExtension(ext); ctype != "" {
		return ctype
	}
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".aac", ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

func normalizeFormat(format string) string {
	value := strings.TrimSpace(strings.ToLower(format))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, ".") {
		value = strings.TrimPrefix(value, ".")
	}
	return value
}

var fileNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeFileName(title string, episodeID uint64, format string) string {
	name := strings.TrimSpace(title)
	if name == "" {
		name = fmt.Sprintf("episode_%d", episodeID)
	}
	name = fileNameSanitizer.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		name = fmt.Sprintf("episode_%d", episodeID)
	}
	format = normalizeFormat(format)
	if format == "" {
		return name
	}
	return fmt.Sprintf("%s.%s", name, format)
}

type streamTokenSigner struct {
	secret []byte
	ttl    time.Duration
}

func newStreamTokenSigner(secret string, ttl time.Duration) *streamTokenSigner {
	return &streamTokenSigner{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// StreamTokenClaims represents decoded token data.
type StreamTokenClaims struct {
	EpisodeID uint64
	UserID    uint64
	ExpiresAt time.Time
}

func (s *streamTokenSigner) Sign(episodeID, userID uint64) (string, error) {
	if episodeID == 0 || userID == 0 {
		return "", errors.New("invalid token payload")
	}
	expires := time.Now().Add(s.ttl).Unix()
	payload := fmt.Sprintf("%d:%d:%d", episodeID, userID, expires)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	signature := mac.Sum(nil)

	token := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature)
	return token, nil
}

func (s *streamTokenSigner) Verify(token string) (*StreamTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token signature encoding")
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payloadBytes)
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, errors.New("signature mismatch")
	}

	payload := string(payloadBytes)
	fields := strings.Split(payload, ":")
	if len(fields) != 3 {
		return nil, errors.New("invalid token payload")
	}
	episodeID, err := parseUint(fields[0])
	if err != nil {
		return nil, errors.New("invalid episode id")
	}
	userID, err := parseUint(fields[1])
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	expires, err := parseInt64(fields[2])
	if err != nil {
		return nil, errors.New("invalid expiration")
	}
	expiry := time.Unix(expires, 0)
	if time.Now().After(expiry) {
		return nil, errors.New("token expired")
	}
	return &StreamTokenClaims{
		EpisodeID: episodeID,
		UserID:    userID,
		ExpiresAt: expiry,
	}, nil
}

func parseUint(value string) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(value), 10, 64)
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}
