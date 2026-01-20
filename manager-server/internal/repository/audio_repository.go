package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"manager-server/internal/models"
)

// AudioProjectFilter captures filters for querying audio projects.
type AudioProjectFilter struct {
	OwnerID    *uint64
	Visibility []string
	NameLike   string
}

// AudioProjectMetrics aggregates derived statistics for a project.
type AudioProjectMetrics struct {
	EpisodeCount  int64
	PlaylistCount int64
	TotalBytes    int64
}

// AudioEpisodeFilter captures filters for querying episodes.
type AudioEpisodeFilter struct {
	ProjectID  string
	PlaylistID *uint64
	ParseState []string
	SourceType []string
	Categories []string
	Query      string
	OrderBy    string
}

// AudioPlaylistFilter captures filters for querying playlists.
type AudioPlaylistFilter struct {
	ProjectID string
	NameLike  string
}

// AudioJobFilter captures filters for querying audio jobs.
type AudioJobFilter struct {
	ProjectID string
	EpisodeID *uint64
	JobTypes  []string
	Status    []string
}

// AudioJobUpdate contains partial update fields for a job.
type AudioJobUpdate struct {
	Status       *string
	Progress     *int
	ErrorMessage *string
	Payload      map[string]any
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

// AudioRepository defines persistence primitives for the audio domain.
type AudioRepository interface {
	CreateProject(ctx context.Context, project *models.AudioProject) error
	UpdateProject(ctx context.Context, project *models.AudioProject) error
	SoftDeleteProject(ctx context.Context, id string) error
	GetProjectByID(ctx context.Context, id string) (*models.AudioProject, error)
	ListProjects(ctx context.Context, filter AudioProjectFilter, limit, offset int) ([]*models.AudioProject, int64, error)
	ProjectMetrics(ctx context.Context, projectIDs []string) (map[string]AudioProjectMetrics, error)

	CreateEpisode(ctx context.Context, episode *models.AudioEpisode) error
	UpdateEpisode(ctx context.Context, episode *models.AudioEpisode) error
	SoftDeleteEpisode(ctx context.Context, id uint64) error
	GetEpisodeByID(ctx context.Context, id uint64) (*models.AudioEpisode, error)
	ListEpisodes(ctx context.Context, filter AudioEpisodeFilter, limit, offset int) ([]*models.AudioEpisode, int64, error)

	CreatePlaylist(ctx context.Context, playlist *models.AudioPlaylist) error
	UpdatePlaylist(ctx context.Context, playlist *models.AudioPlaylist) error
	SoftDeletePlaylist(ctx context.Context, id uint64) error
	GetPlaylistByID(ctx context.Context, id uint64) (*models.AudioPlaylist, error)
	ListPlaylists(ctx context.Context, filter AudioPlaylistFilter, limit, offset int) ([]*models.AudioPlaylist, int64, error)
	ReplacePlaylistEpisodes(ctx context.Context, playlistID uint64, entries []models.AudioPlaylistEpisode) error

	CreatePlaylistEpisode(ctx context.Context, entry *models.AudioPlaylistEpisode) error
	DeletePlaylistEpisode(ctx context.Context, playlistID, episodeID uint64) error

	CreateJob(ctx context.Context, job *models.AudioJob) error
	UpdateJob(ctx context.Context, jobID uint64, update AudioJobUpdate) error
	GetJobByID(ctx context.Context, jobID uint64) (*models.AudioJob, error)
	ClaimNextPendingJob(ctx context.Context, types []string) (*models.AudioJob, error)
	ListJobs(ctx context.Context, filter AudioJobFilter, limit, offset int) ([]*models.AudioJob, int64, error)
}

type audioRepository struct {
	db *gorm.DB
}

// NewAudioRepository constructs a repository backed by GORM.
func NewAudioRepository(db *gorm.DB) AudioRepository {
	return &audioRepository{db: db}
}

// CreateProject inserts a new audio project.
func (r *audioRepository) CreateProject(ctx context.Context, project *models.AudioProject) error {
	return r.db.WithContext(ctx).Create(project).Error
}

// UpdateProject persists changes to an existing project.
func (r *audioRepository) UpdateProject(ctx context.Context, project *models.AudioProject) error {
	return r.db.WithContext(ctx).Save(project).Error
}

// SoftDeleteProject marks a project as deleted.
func (r *audioRepository) SoftDeleteProject(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.AudioProject{}).Error
}

// GetProjectByID fetches a project by identifier.
func (r *audioRepository) GetProjectByID(ctx context.Context, id string) (*models.AudioProject, error) {
	var project models.AudioProject
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// ListProjects returns projects satisfying the supplied filter.
func (r *audioRepository) ListProjects(ctx context.Context, filter AudioProjectFilter, limit, offset int) ([]*models.AudioProject, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.AudioProject{})

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if filter.OwnerID != nil {
		query = query.Where("owner_id = ?", *filter.OwnerID)
	}
	if len(filter.Visibility) > 0 {
		query = query.Where("visibility IN ?", filter.Visibility)
	}
	if filter.NameLike != "" {
		query = query.Where("name LIKE ?", "%"+filter.NameLike+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var projects []*models.AudioProject
	err := query.Order("create_time DESC").Limit(limit).Offset(offset).Find(&projects).Error
	return projects, total, err
}

// ProjectMetrics aggregates statistics for the supplied projects.
func (r *audioRepository) ProjectMetrics(ctx context.Context, projectIDs []string) (map[string]AudioProjectMetrics, error) {
	if len(projectIDs) == 0 {
		return map[string]AudioProjectMetrics{}, nil
	}
	result := make(map[string]AudioProjectMetrics, len(projectIDs))

	type counter struct {
		ProjectID string
		Count     int64
		Total     int64
	}

	var episodes []counter
	if err := r.db.WithContext(ctx).Model(&models.AudioEpisode{}).
		Select("project_id, COUNT(*) AS count, COALESCE(SUM(file_size), 0) AS total").
		Where("project_id IN ?", projectIDs).
		Group("project_id").
		Scan(&episodes).Error; err != nil {
		return nil, err
	}
	for _, row := range episodes {
		metrics := result[row.ProjectID]
		metrics.EpisodeCount = row.Count
		metrics.TotalBytes = row.Total
		result[row.ProjectID] = metrics
	}

	var playlists []counter
	if err := r.db.WithContext(ctx).Model(&models.AudioPlaylist{}).
		Select("project_id, COUNT(*) AS count, 0 AS total").
		Where("project_id IN ?", projectIDs).
		Group("project_id").
		Scan(&playlists).Error; err != nil {
		return nil, err
	}
	for _, row := range playlists {
		metrics := result[row.ProjectID]
		metrics.PlaylistCount = row.Count
		result[row.ProjectID] = metrics
	}
	return result, nil
}

// CreateEpisode inserts a new episode.
func (r *audioRepository) CreateEpisode(ctx context.Context, episode *models.AudioEpisode) error {
	return r.db.WithContext(ctx).Create(episode).Error
}

// UpdateEpisode persists episode changes.
func (r *audioRepository) UpdateEpisode(ctx context.Context, episode *models.AudioEpisode) error {
	return r.db.WithContext(ctx).Save(episode).Error
}

// SoftDeleteEpisode marks an episode as deleted.
func (r *audioRepository) SoftDeleteEpisode(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.AudioEpisode{}).Error
}

// GetEpisodeByID fetches an episode by identifier.
func (r *audioRepository) GetEpisodeByID(ctx context.Context, id uint64) (*models.AudioEpisode, error) {
	var episode models.AudioEpisode
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&episode).Error; err != nil {
		return nil, err
	}
	return &episode, nil
}

// ListEpisodes enumerates episodes matching the filter.
func (r *audioRepository) ListEpisodes(ctx context.Context, filter AudioEpisodeFilter, limit, offset int) ([]*models.AudioEpisode, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.AudioEpisode{})

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if filter.ProjectID != "" {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	if filter.PlaylistID != nil {
		sub := r.db.Model(&models.AudioPlaylistEpisode{}).
			Select("episode_id").
			Where("playlist_id = ?", *filter.PlaylistID)
		query = query.Where("id IN (?)", sub)
	}
	if len(filter.ParseState) > 0 {
		query = query.Where("parse_status IN ?", filter.ParseState)
	}
	if len(filter.SourceType) > 0 {
		query = query.Where("source_type IN ?", filter.SourceType)
	}
	if len(filter.Categories) > 0 {
		query = query.Where("category IN ?", filter.Categories)
	}
	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		query = query.Where(
			r.db.Where("episode_title LIKE ?", pattern).
				Or("show_title LIKE ?", pattern).
				Or("primary_host LIKE ?", pattern),
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := filter.OrderBy
	if order == "" {
		order = "create_time DESC"
	}
	var episodes []*models.AudioEpisode
	if err := query.Order(order).Limit(limit).Offset(offset).Find(&episodes).Error; err != nil {
		return nil, 0, err
	}
	return episodes, total, nil
}

// CreatePlaylist inserts a new playlist.
func (r *audioRepository) CreatePlaylist(ctx context.Context, playlist *models.AudioPlaylist) error {
	return r.db.WithContext(ctx).Create(playlist).Error
}

// UpdatePlaylist persists playlist changes.
func (r *audioRepository) UpdatePlaylist(ctx context.Context, playlist *models.AudioPlaylist) error {
	return r.db.WithContext(ctx).Save(playlist).Error
}

// SoftDeletePlaylist marks a playlist as deleted.
func (r *audioRepository) SoftDeletePlaylist(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.AudioPlaylist{}).Error
}

// GetPlaylistByID fetches a playlist.
func (r *audioRepository) GetPlaylistByID(ctx context.Context, id uint64) (*models.AudioPlaylist, error) {
	var playlist models.AudioPlaylist
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&playlist).Error; err != nil {
		return nil, err
	}
	return &playlist, nil
}

// ListPlaylists enumerates playlists matching the filter.
func (r *audioRepository) ListPlaylists(ctx context.Context, filter AudioPlaylistFilter, limit, offset int) ([]*models.AudioPlaylist, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.AudioPlaylist{})

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if filter.ProjectID != "" {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	if filter.NameLike != "" {
		query = query.Where("name LIKE ?", "%"+filter.NameLike+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var playlists []*models.AudioPlaylist
	err := query.Order("create_time DESC").Limit(limit).Offset(offset).Find(&playlists).Error
	return playlists, total, err
}

// ReplacePlaylistEpisodes replaces playlist membership in a transaction.
func (r *audioRepository) ReplacePlaylistEpisodes(ctx context.Context, playlistID uint64, entries []models.AudioPlaylistEpisode) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("playlist_id = ?", playlistID).Delete(&models.AudioPlaylistEpisode{}).Error; err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		for i := range entries {
			entries[i].PlaylistID = playlistID
		}
		return tx.Create(&entries).Error
	})
}

// CreatePlaylistEpisode inserts a single playlist membership entry.
func (r *audioRepository) CreatePlaylistEpisode(ctx context.Context, entry *models.AudioPlaylistEpisode) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

// DeletePlaylistEpisode removes playlist membership.
func (r *audioRepository) DeletePlaylistEpisode(ctx context.Context, playlistID, episodeID uint64) error {
	return r.db.WithContext(ctx).Where("playlist_id = ? AND episode_id = ?", playlistID, episodeID).
		Delete(&models.AudioPlaylistEpisode{}).Error
}

// CreateJob enqueues a new job.
func (r *audioRepository) CreateJob(ctx context.Context, job *models.AudioJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

// UpdateJob persists selective job fields.
func (r *audioRepository) UpdateJob(ctx context.Context, jobID uint64, update AudioJobUpdate) error {
	data := map[string]any{}
	if update.Status != nil {
		data["status"] = *update.Status
	}
	if update.Progress != nil {
		data["progress"] = *update.Progress
	}
	if update.ErrorMessage != nil {
		data["error_message"] = *update.ErrorMessage
	}
	if update.StartedAt != nil {
		data["started_at"] = *update.StartedAt
	}
	if update.CompletedAt != nil {
		data["completed_at"] = *update.CompletedAt
	}
	if update.Payload != nil {
		payload, err := json.Marshal(update.Payload)
		if err != nil {
			return fmt.Errorf("marshal job payload: %w", err)
		}
		data["payload"] = datatypes.JSON(payload)
	}
	if len(data) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&models.AudioJob{}).Where("id = ?", jobID).Updates(data).Error
}

// GetJobByID retrieves a job.
func (r *audioRepository) GetJobByID(ctx context.Context, jobID uint64) (*models.AudioJob, error) {
	var job models.AudioJob
	if err := r.db.WithContext(ctx).Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// ClaimNextPendingJob atomically claims the next queued job.
func (r *audioRepository) ClaimNextPendingJob(ctx context.Context, types []string) (*models.AudioJob, error) {
	typeFilter := len(types) > 0
	for {
		var job models.AudioJob
		query := r.db.WithContext(ctx).Where("status = ?", "queued")
		if typeFilter {
			query = query.Where("job_type IN ?", types)
		}
		if err := query.Order("create_time").First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		now := time.Now()
		res := r.db.WithContext(ctx).Model(&models.AudioJob{}).
			Where("id = ? AND status = ?", job.ID, "queued").
			Updates(map[string]any{
				"status":     "processing",
				"started_at": now,
			})
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			// Lost the race, retry.
			continue
		}
		job.Status = "processing"
		job.StartedAt = &now
		return &job, nil
	}
}

// ListJobs enumerates jobs matching the filter.
func (r *audioRepository) ListJobs(ctx context.Context, filter AudioJobFilter, limit, offset int) ([]*models.AudioJob, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.AudioJob{})

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	if filter.ProjectID != "" {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	if filter.EpisodeID != nil {
		query = query.Where("episode_id = ?", *filter.EpisodeID)
	}
	if len(filter.JobTypes) > 0 {
		query = query.Where("job_type IN ?", filter.JobTypes)
	}
	if len(filter.Status) > 0 {
		query = query.Where("status IN ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var jobs []*models.AudioJob
	err := query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "create_time"},
		Desc:   true,
	}).Limit(limit).Offset(offset).Find(&jobs).Error
	return jobs, total, err
}
