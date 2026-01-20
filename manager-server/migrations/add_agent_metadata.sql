-- +goose Up
-- +goose StatementBegin
ALTER TABLE ai_agent ADD COLUMN IF NOT EXISTS metadata jsonb DEFAULT '{}';
COMMENT ON COLUMN ai_agent.metadata IS '智能体元信息（扩展字段），如SubsonicURL等';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE ai_agent DROP COLUMN IF EXISTS metadata;
-- +goose StatementEnd
