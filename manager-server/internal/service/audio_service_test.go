package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	navidrome "manager-server/internal/audio/navidrome"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
)

type enqueueStub struct {
	jobs []uint64
}

func (e *enqueueStub) Enqueue(_ context.Context, jobID uint64) error {
	e.jobs = append(e.jobs, jobID)
	return nil
}

type fakeTranscoder struct {
	payload []byte
	format  string
}

func (f fakeTranscoder) Transcode(_ context.Context, _ string, opts navidrome.TranscodeOptions) (*navidrome.TranscodeResult, error) {
	format := f.format
	if format == "" {
		format = opts.TargetFormat
	}
	output := filepath.Join(opts.OutputDir, fmt.Sprintf("fake-%d.%s", time.Now().UnixNano(), format))
	if err := os.WriteFile(output, f.payload, 0o644); err != nil {
		return nil, err
	}
	return &navidrome.TranscodeResult{
		Path:         output,
		TargetFormat: format,
		Bitrate:      opts.Bitrate,
	}, nil
}

func setupAudioTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	err = db.AutoMigrate(
		&models.AudioProject{},
		&models.AudioEpisode{},
		&models.AudioPlaylist{},
		&models.AudioPlaylistEpisode{},
		&models.AudioJob{},
	)
	require.NoError(t, err)
	return db
}

func TestAudioServiceProjectLifecycle(t *testing.T) {
	db := setupAudioTestDB(t)
	repo := repository.NewAudioRepository(db)
	tempDir := t.TempDir()
	storageSvc, err := storage.NewLocalAudioStorage(tempDir)
	require.NoError(t, err)

	cfg := AudioServiceConfig{
		MaxUploadBytes:    10 * 1024 * 1024,
		AllowedFormats:    []string{".mp3", ".wav"},
		StorageQuotaBytes: 100 * 1024 * 1024,
		TokenSecret:       "test-secret",
		TokenTTL:          time.Minute,
	}

	stubRunner := &enqueueStub{}
	svc := NewAudioService(repo, storageSvc, stubRunner, navidrome.NewDefaultScanner(nil), navidrome.NoopTranscoder{}, cfg)
	svc.SetJobRunner(stubRunner)
	ctx := context.Background()
	actor := ActorContext{UserID: 1}

	project, err := svc.CreateProject(ctx, actor, CreateAudioProjectInput{Name: "Podcast Hub"})
	require.NoError(t, err)
	require.NotZero(t, project.ID)

	projects, total, err := svc.ListProjects(ctx, actor, repository.AudioProjectFilter{}, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, projects, 1)
	require.Equal(t, "Podcast Hub", projects[0].Project.Name)

	data := bytes.Repeat([]byte("A"), 1024)
	reader := io.NopCloser(bytes.NewReader(data))
	episode, err := svc.UploadEpisode(ctx, actor, UploadEpisodeInput{
		ProjectID:   project.ID,
		FileName:    "welcome.mp3",
		Size:        int64(len(data)),
		ContentType: "audio/mpeg",
		Reader:      reader,
		UploaderID:  actor.UserID,
	})
	require.NoError(t, err)
	require.NotZero(t, episode.ID)
	require.Equal(t, "queued", episode.ParseStatus)
	require.NotEmpty(t, episode.FileURI)

	absPath, err := storageSvc.ResolvePath(episode.FileURI)
	require.NoError(t, err)
	if _, err := os.Stat(absPath); err != nil {
		t.Fatalf("expected stored file to exist: %v", err)
	}

	require.Len(t, stubRunner.jobs, 1, "expected ingestion job to be enqueued")

	// verify job persisted
	job, err := repo.GetJobByID(ctx, stubRunner.jobs[0])
	require.NoError(t, err)
	require.Equal(t, "queued", job.Status)
	require.NotNil(t, job.EpisodeID)

	// ensure ListEpisodes returns uploaded entry
	episodes, totalEpisodes, err := svc.ListEpisodes(ctx, actor, repository.AudioEpisodeFilter{ProjectID: project.ID}, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), totalEpisodes)
	require.Len(t, episodes, 1)
	require.Equal(t, episode.ID, episodes[0].ID)

	// token generation should succeed
	token, err := svc.GenerateStreamToken(ctx, actor, episode.ID)
	require.NoError(t, err)
	claims, err := svc.VerifyStreamToken(token)
	require.NoError(t, err)
	require.Equal(t, episode.ID, claims.EpisodeID)
	require.Equal(t, actor.UserID, claims.UserID)

	// update metadata
	newTitle := "Episode Alpha"
	updated, err := svc.UpdateEpisode(ctx, actor, episode.ID, UpdateEpisodeInput{EpisodeTitle: &newTitle})
	require.NoError(t, err)
	require.Equal(t, newTitle, updated.EpisodeTitle)

	// clean up stored files
	files, err := filepath.Glob(filepath.Join(tempDir, "audio", "project_*", "episodes", "*", "*.mp3"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
}

func TestUploadEpisodeWithTranscoding(t *testing.T) {
	db := setupAudioTestDB(t)
	repo := repository.NewAudioRepository(db)
	tempDir := t.TempDir()
	storageSvc, err := storage.NewLocalAudioStorage(tempDir)
	require.NoError(t, err)

	cfg := AudioServiceConfig{
		MaxUploadBytes:     10 * 1024 * 1024,
		AllowedFormats:     []string{".wav", ".mp3"},
		StorageQuotaBytes:  100 * 1024 * 1024,
		TranscodingEnabled: true,
		TranscodeFormat:    "mp3",
		TranscodeBitrate:   128000,
	}

	payload := []byte("normalized-mp3")
	stubRunner := &enqueueStub{}
	svc := NewAudioService(repo, storageSvc, stubRunner, navidrome.NewDefaultScanner(nil), fakeTranscoder{
		payload: payload,
		format:  "mp3",
	}, cfg)
	svc.SetJobRunner(stubRunner)

	ctx := context.Background()
	actor := ActorContext{UserID: 2}
	project, err := svc.CreateProject(ctx, actor, CreateAudioProjectInput{Name: "Standardized"})
	require.NoError(t, err)

	reader := io.NopCloser(bytes.NewReader([]byte("wave-bytes")))
	episode, err := svc.UploadEpisode(ctx, actor, UploadEpisodeInput{
		ProjectID:   project.ID,
		FileName:    "clip.wav",
		Size:        int64(10),
		ContentType: "audio/wav",
		Reader:      reader,
		UploaderID:  actor.UserID,
	})
	require.NoError(t, err)
	require.NotZero(t, episode.ID)

	storedPath, err := storageSvc.ResolvePath(episode.FileURI)
	require.NoError(t, err)
	data, err := os.ReadFile(storedPath)
	require.NoError(t, err)
	require.Equal(t, payload, data)
	require.Equal(t, ".mp3", strings.ToLower(filepath.Ext(storedPath)))
}
