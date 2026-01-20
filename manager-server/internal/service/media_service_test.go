package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"manager-server/internal/config"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
)

func setupMediaTestDB(t *testing.T) *gorm.DB {
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

	err = db.AutoMigrate(&models.MediaAsset{}, &models.DeviceEntity{})
	require.NoError(t, err)
	return db
}

func TestMediaService_UploadListDelete(t *testing.T) {
	db := setupMediaTestDB(t)
	mediaRepo := repository.NewMediaRepository(db)
	deviceRepo := repository.NewDeviceRepository(db)
	tempDir := t.TempDir()

	store, err := storage.NewLocalMediaStorage(tempDir)
	require.NoError(t, err)

	cfg := config.MediaConfig{
		Storage: config.MediaStorageConfig{
			MaxFileSizeMB:  10,
			AllowedFormats: []string{".png", ".mp4"},
		},
	}

	svc := NewMediaService(mediaRepo, deviceRepo, store, cfg)
	ctx := context.Background()

	device := &models.DeviceEntity{
		ID:         "device-1",
		AgentID:    "agent-42",
		MacAddress: "AA:BB:CC:DD",
		CreateDate: time.Now(),
		UpdateDate: time.Now(),
	}
	require.NoError(t, db.WithContext(ctx).Create(device).Error)

	payload := bytes.Repeat([]byte{0x89}, 2048)
	reader := bytes.NewReader(payload)

	asset, err := svc.UploadMedia(ctx, UploadMediaInput{
		DeviceID:     device.ID,
		OriginalName: "snapshot.png",
		ContentType:  "image/png",
		Size:         int64(len(payload)),
		Reader:       reader,
		Source:       "vision",
		Description:  "test image",
		RelatedInfo: map[string]any{
			"question": "What is in the picture?",
			"result":   "A sunset over mountains",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, asset)
	require.Equal(t, "agent-42", asset.AgentID)
	require.Equal(t, "image", asset.MediaType)
	require.NotEmpty(t, asset.StorageURI)
	require.Equal(t, "A sunset over mountains", asset.RelatedInfo["result"])

	// ensure file exists on disk
	path, err := store.ResolvePath(asset.StorageURI)
	require.NoError(t, err)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected stored media file to exist: %v", err)
	}

	items, total, err := svc.ListMedia(ctx, ListMediaInput{
		AgentID: asset.AgentID,
		Limit:   10,
		Offset:  0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, asset.ID, items[0].ID)

	readAsset, rc, err := svc.OpenMedia(ctx, asset.ID)
	require.NoError(t, err)
	require.NotNil(t, rc)
	require.Equal(t, asset.ID, readAsset.ID)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, len(payload), len(data))
	require.NoError(t, rc.Close())

	require.NoError(t, svc.DeleteMedia(ctx, asset.ID))

	// file should be removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected media file to be deleted, err=%v", err)
	}

	// list should be empty
	items, total, err = svc.ListMedia(ctx, ListMediaInput{
		AgentID: asset.AgentID,
		Limit:   10,
		Offset:  0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, items, 0)
}
