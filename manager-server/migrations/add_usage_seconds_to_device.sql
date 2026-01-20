-- 添加设备使用时长统计字段
-- 文件: migrations/add_usage_seconds_to_device.sql

-- 为ai_device表添加usage_seconds字段
ALTER TABLE ai_device 
ADD COLUMN usage_seconds BIGINT DEFAULT 0 COMMENT '累计使用时长(秒)';

-- 为现有设备初始化usage_seconds字段为0
UPDATE ai_device 
SET usage_seconds = 0 
WHERE usage_seconds IS NULL;

-- 添加索引以提高查询性能（可选）
CREATE INDEX idx_ai_device_usage_seconds ON ai_device(usage_seconds);

-- 验证字段添加成功
-- SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_COMMENT 
-- FROM INFORMATION_SCHEMA.COLUMNS 
-- WHERE TABLE_SCHEMA = DATABASE() 
--   AND TABLE_NAME = 'ai_device' 
--   AND COLUMN_NAME = 'usage_seconds';
