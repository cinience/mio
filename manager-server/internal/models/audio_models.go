package models

import (
	"time"

	"gorm.io/datatypes"
)

// AudioProject represents a scoped podcast project.
type AudioProject struct {
	BaseModelUUID
	OwnerID    uint64         `gorm:"column:owner_id;not null;index:idx_audio_project_owner_name,priority:1" json:"ownerId"`
	Name       string         `gorm:"column:name;type:varchar(128);not null;index:idx_audio_project_owner_name,priority:2" json:"name"`
	Visibility string         `gorm:"column:visibility;type:varchar(32);default:'private';not null;index" json:"visibility"`
	Metadata   datatypes.JSON `gorm:"column:metadata;type:json" json:"metadata"`
}

// TableName customizes the table for AudioProject.
func (*AudioProject) TableName() string {
	return "audio_projects"
}

// AudioEpisode stores metadata about an individual podcast episode.
type AudioEpisode struct {
	BaseModel
	ProjectID     string         `gorm:"column:project_id;type:uuid;not null;index:idx_audio_episode_project" json:"projectId"`
	EpisodeTitle  string         `gorm:"column:episode_title;type:varchar(255);not null;index:idx_audio_episode_title" json:"episodeTitle"`
	ShowTitle     string         `gorm:"column:show_title;type:varchar(255);index:idx_audio_episode_show" json:"showTitle"`
	PrimaryHost   string         `gorm:"column:primary_host;type:varchar(255);index:idx_audio_episode_host" json:"primaryHost"`
	Category      string         `gorm:"column:category;type:varchar(64);index:idx_audio_episode_category" json:"category"`
	SeasonNumber  int            `gorm:"column:season_number;type:int;default:0;index" json:"seasonNumber"`
	EpisodeNumber int            `gorm:"column:episode_number;type:int;default:0;index" json:"episodeNumber"`
	PublishAt     *time.Time     `gorm:"column:publish_at;index" json:"publishAt,omitempty"`
	Duration      float64        `gorm:"column:duration;type:decimal(10,2);not null;default:0" json:"duration"`
	Bitrate       int            `gorm:"column:bitrate;type:int;not null;default:0" json:"bitrate"`
	FileSize      int64          `gorm:"column:file_size;type:bigint;not null;default:0" json:"fileSize"`
	FileURI       string         `gorm:"column:file_uri;type:varchar(512);not null" json:"fileUri"`
	SourceType    string         `gorm:"column:source_type;type:varchar(32);not null;index" json:"sourceType"`
	CoverURI      string         `gorm:"column:cover_uri;type:varchar(512)" json:"coverUri"`
	TranscriptURI string         `gorm:"column:transcript_uri;type:varchar(512)" json:"transcriptUri"`
	ParseStatus   string         `gorm:"column:parse_status;type:varchar(32);not null;default:'queued';index" json:"parseStatus"`
	ParseError    string         `gorm:"column:parse_error;type:text" json:"parseError"`
	Metadata      datatypes.JSON `gorm:"column:metadata;type:json" json:"metadata"`
}

// TableName customizes the table for AudioEpisode.
func (*AudioEpisode) TableName() string {
	return "audio_episodes"
}

// AudioPlaylist represents a user defined playlist scoped to a project.
type AudioPlaylist struct {
	BaseModel
	ProjectID   string         `gorm:"column:project_id;type:uuid;not null;index:idx_audio_playlist_project" json:"projectId"`
	Name        string         `gorm:"column:name;type:varchar(128);not null;index:idx_audio_playlist_name" json:"name"`
	Description string         `gorm:"column:description;type:text" json:"description"`
	CoverURI    string         `gorm:"column:cover_uri;type:varchar(512)" json:"coverUri"`
	Metadata    datatypes.JSON `gorm:"column:metadata;type:json" json:"metadata"`
}

// TableName customizes the table for AudioPlaylist.
func (*AudioPlaylist) TableName() string {
	return "audio_playlists"
}

// AudioPlaylistEpisode stores ordered episode membership for playlists.
type AudioPlaylistEpisode struct {
	BaseModel
	PlaylistID uint64 `gorm:"column:playlist_id;not null;uniqueIndex:idx_audio_playlist_episode_unique,priority:1" json:"playlistId"`
	EpisodeID  uint64 `gorm:"column:episode_id;not null;uniqueIndex:idx_audio_playlist_episode_unique,priority:2" json:"episodeId"`
	// OrderValue is stored separately to avoid reserved keyword `order` across SQL dialects.
	OrderValue int `gorm:"column:order_value;not null;default:0;index" json:"order"`
}

// TableName customizes the table for AudioPlaylistEpisode.
func (*AudioPlaylistEpisode) TableName() string {
	return "audio_playlist_episodes"
}

// AudioJob tracks ingestion and transcoding operations for audio assets.
type AudioJob struct {
	BaseModel
	ProjectID    string         `gorm:"column:project_id;type:uuid;not null;index:idx_audio_jobs_project_status,priority:1" json:"projectId"`
	EpisodeID    *uint64        `gorm:"column:episode_id;index" json:"episodeId,omitempty"`
	JobType      string         `gorm:"column:job_type;type:varchar(32);not null;index" json:"jobType"`
	Status       string         `gorm:"column:status;type:varchar(32);not null;index:idx_audio_jobs_project_status,priority:2" json:"status"`
	Progress     int            `gorm:"column:progress;type:int;not null;default:0" json:"progress"`
	ErrorMessage string         `gorm:"column:error_message;type:text" json:"errorMessage"`
	Payload      datatypes.JSON `gorm:"column:payload;type:json" json:"payload"`
	StartedAt    *time.Time     `gorm:"column:started_at;index" json:"startedAt,omitempty"`
	CompletedAt  *time.Time     `gorm:"column:completed_at;index" json:"completedAt,omitempty"`
}

// TableName customizes the table for AudioJob.
func (*AudioJob) TableName() string {
	return "audio_jobs"
}
