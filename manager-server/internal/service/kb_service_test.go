package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"manager-server/internal/kb/ingest"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/vectorstore"
)

type captureEventSink struct {
	calls [][]uint64
}

func (c *captureEventSink) AgentsCapabilitiesChanged(_ context.Context, agentIDs []uint64) {
	idsCopy := make([]uint64, len(agentIDs))
	copy(idsCopy, agentIDs)
	c.calls = append(c.calls, idsCopy)
}

func (c *captureEventSink) JobStatusChanged(context.Context, *models.KBJob) {}

func (c *captureEventSink) contains(agentID uint64) bool {
	for _, call := range c.calls {
		for _, id := range call {
			if id == agentID {
				return true
			}
		}
	}
	return false
}

func setupKBTestDB(t *testing.T) *gorm.DB {
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
		&models.KBProject{},
		&models.KBKnowledgeBase{},
		&models.KBDocument{},
		&models.KBChunk{},
		&models.KBJob{},
		&models.KBPermission{},
		&models.KBAgentProjectMount{},
	)
	require.NoError(t, err)
	return db
}

func TestKBServiceProjectAndKnowledgeBaseLifecycle(t *testing.T) {
	db := setupKBTestDB(t)
	repo := repository.NewKBRepository(db)
	vectorAdapter := vectorstore.NewInMemoryAdapter()
	events := &captureEventSink{}
	retryCfg := IngestionRetryConfig{MaxAttempts: 3}
	jobRunner := NewKBJobRunner(repo, nil, nil, vectorAdapter, events, ingest.Config{}, retryCfg)
	jobRunner.Start(1)
	t.Cleanup(jobRunner.Stop)

	svc := NewKBService(repo, jobRunner, vectorAdapter, nil, events, 0, nil, IngestionQuotaConfig{}, retryCfg)
	ctx := context.Background()
	owner := ActorContext{UserID: 1}

	project, err := svc.CreateProject(ctx, owner, CreateProjectInput{
		Name:       "Project Alpha",
		Visibility: "shared",
		Metadata: map[string]any{
			"department": "research",
		},
	})
	require.NoError(t, err)
	require.NotZero(t, project.ID)

	project, err = svc.UpdateProject(ctx, owner, project.ID, UpdateProjectInput{
		Name:       ptr("Project Beta"),
		Visibility: ptr("private"),
		Metadata: map[string]any{
			"department": "engineering",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Project Beta", project.Name)
	require.Equal(t, "private", project.Visibility)

	kb, err := svc.CreateKnowledgeBase(ctx, owner, CreateKnowledgeBaseInput{
		ProjectID:       project.ID,
		Name:            "Docs KB",
		Description:     "Engineering documentation",
		EmbeddingModel:  "text-embedding-3-small",
		EmbeddingParams: map[string]any{"dims": 1024},
	})
	require.NoError(t, err)
	require.Equal(t, project.ID, kb.ProjectID)

	err = svc.MountAgentProject(ctx, owner, AgentProjectMountInput{
		AgentID:   42,
		ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.True(t, events.contains(42))

	adminActor := ActorContext{UserID: 99, IsAdmin: true}
	err = svc.GrantPermission(ctx, owner, PermissionGrantInput{
		KnowledgeBaseID: kb.ID,
		UserID:          2,
		Role:            KBRoleEditor,
	})
	require.NoError(t, err)

	// Non-owner but editor can add document
	editor := ActorContext{UserID: 2}
	doc, job, err := svc.CreateDocument(ctx, editor, CreateDocumentInput{
		KnowledgeBaseID:  kb.ID,
		Title:            "Test Plan",
		SourceType:       "markdown",
		EnqueueIngestion: true,
		RawContent:       "# Heading\n\nTest content for ingestion pipeline.",
	})
	require.NoError(t, err)
	require.NotNil(t, job)

	// Wait for job runner to process
	require.Eventually(t, func() bool {
		jb, err := repo.GetJobByID(ctx, job.ID)
		if err != nil {
			return false
		}
		return jb.Status == "completed"
	}, time.Second*5, time.Millisecond*100)

	updatedDoc, err := repo.GetDocumentByID(ctx, doc.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", updatedDoc.ParseStatus)

	chunks, _, err := repo.ListChunks(ctx, repository.KBChunkFilter{DocumentID: doc.ID}, 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	results, err := vectorAdapter.Query(ctx, kb.ID, "test content", 3)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Admin can list permissions
	perms, err := svc.ListPermissions(ctx, adminActor, kb.ID)
	require.NoError(t, err)
	require.Len(t, perms, 1)

	// Owner can revoke
	err = svc.RevokePermission(ctx, owner, kb.ID, 2)
	require.NoError(t, err)
}

func ptr[T any](v T) *T {
	return &v
}
