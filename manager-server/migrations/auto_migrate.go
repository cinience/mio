package migrations

import (
	"fmt"

	"gorm.io/gorm"

	"manager-server/internal/logger"
	"manager-server/internal/models"
)

// AutoMigrate runs the database migrations required by the manager server.
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	logger.Infof("🔄 开始数据库自动迁移...")

	if db.Dialector != nil && db.Dialector.Name() == "postgres" {
		logger.Infof("🔌 确保 pgvector 扩展可用...")
		if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
			return fmt.Errorf("确保 pgvector 扩展失败: %w", err)
		}
		logger.Infof("✅ pgvector 扩展可用")
	}

	modelsToMigrate := []interface{}{
		&models.SysUser{},
		&models.SysUserToken{},
		&models.SysParams{},
		&models.SysDictType{},
		&models.SysDictData{},
		&models.Agent{},
		&models.AgentTemplate{},
		&models.AgentPluginMapping{},
		&models.AgentVoicePrint{},
		&models.AgentChatHistory{},
		&models.AgentVisionSource{},
		&models.AgentVisionRule{},
		&models.AgentVisionEvent{},
		&models.AIModelProvider{},
		&models.AIModelConfig{},
		&models.AITTSVoice{},
		&models.OtaEntity{},
		&models.DeviceEntity{},
		&models.KBProject{},
		&models.KBKnowledgeBase{},
		&models.KBDocument{},
		&models.KBChunk{},
		&models.KBChunkEmbedding{},
		&models.KBJob{},
		&models.KBPermission{},
		&models.KBAgentProjectMount{},
		&models.KBIngestionSession{},
		&models.KBJobWebhookEvent{},
		&models.AudioProject{},
		&models.AudioEpisode{},
		&models.AudioPlaylist{},
		&models.AudioPlaylistEpisode{},
		&models.AudioJob{},
		&models.MediaAsset{},
		&models.Workflow{},
		&models.WorkflowVersion{},
		&models.WorkflowExecution{},
	}

	autoMigrateDB := db.Session(&gorm.Session{PrepareStmt: true})

	if err := autoMigrateDB.AutoMigrate(modelsToMigrate...); err != nil {
		return fmt.Errorf("自动迁移失败: %w", err)
	}

	logger.Infof("✅ 核心表结构迁移完成")

	if !autoMigrateDB.Migrator().HasColumn(&models.DeviceEntity{}, "usage_seconds") {
		logger.Infof("🔧 usage_seconds字段不存在，正在添加...")
		if err := autoMigrateDB.Migrator().AddColumn(&models.DeviceEntity{}, "UsageSeconds"); err != nil {
			return fmt.Errorf("添加usage_seconds字段失败: %w", err)
		}
		if err := autoMigrateDB.Model(&models.DeviceEntity{}).Where("usage_seconds IS NULL").Update("usage_seconds", 0).Error; err != nil {
			return fmt.Errorf("初始化usage_seconds字段失败: %w", err)
		}
		logger.Infof("✅ usage_seconds字段添加成功")
	} else {
		logger.Infof("✅ usage_seconds字段已存在，跳过添加")
	}

	var deviceCount int64
	if err := autoMigrateDB.Model(&models.DeviceEntity{}).Count(&deviceCount).Error; err != nil {
		return fmt.Errorf("验证迁移失败: %w", err)
	}

	logger.Infof("🎉 数据库自动迁移完成！当前设备数量: %d", deviceCount)
	return nil
}
