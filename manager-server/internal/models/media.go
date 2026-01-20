package models

import "gorm.io/datatypes"

// MediaAsset represents an uploaded image, video, or audio associated with an agent or device.
type MediaAsset struct {
	BaseModel
	AgentID      string            `gorm:"column:agent_id;type:varchar(64);index;not null" json:"agentId"`
	DeviceID     *string           `gorm:"column:device_id;type:varchar(64);index" json:"deviceId,omitempty"`
	FileName     string            `gorm:"column:file_name;type:varchar(255);not null" json:"fileName"`
	OriginalName string            `gorm:"column:original_name;type:varchar(255)" json:"originalName"`
	MediaType    string            `gorm:"column:media_type;type:varchar(32);index;not null" json:"mediaType"`
	ContentType  string            `gorm:"column:content_type;type:varchar(128)" json:"contentType"`
	FileSize     int64             `gorm:"column:file_size;type:bigint" json:"fileSize"`
	StorageURI   string            `gorm:"column:storage_uri;type:varchar(512);not null" json:"storageUri"`
	Source       string            `gorm:"column:source;type:varchar(32);not null" json:"source"`
	Description  string            `gorm:"column:description;type:text" json:"description"`
	RelatedInfo  datatypes.JSONMap `gorm:"column:related_info;type:jsonb" json:"relatedInfo"`
	PublicURL    string            `gorm:"-" json:"publicUrl,omitempty"`
}

// TableName specifies the database table for media assets.
func (MediaAsset) TableName() string {
	return "ai_media_asset"
}
