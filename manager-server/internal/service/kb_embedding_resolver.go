package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/datatypes"

	"manager-server/internal/config"
	"manager-server/internal/models"
	"manager-server/internal/rag"
	"manager-server/internal/repository"
	"manager-server/internal/vectorstore"
)

// KBEmbeddingResolver resolves embedding configurations per knowledge base.
type KBEmbeddingResolver struct {
	kbRepo     repository.KBRepository
	modelRepo  repository.AIModelConfigRepository
	defaultCfg config.ES8Config
}

// NewKBEmbeddingResolver constructs a resolver for embedding runtime settings.
func NewKBEmbeddingResolver(kbRepo repository.KBRepository, modelRepo repository.AIModelConfigRepository, defaultCfg config.ES8Config) *KBEmbeddingResolver {
	return &KBEmbeddingResolver{kbRepo: kbRepo, modelRepo: modelRepo, defaultCfg: defaultCfg}
}

// GetEmbeddingConfig implements vectorstore.EmbeddingConfigProvider.
func (r *KBEmbeddingResolver) GetEmbeddingConfig(ctx context.Context, knowledgeBaseID uint64) (*vectorstore.EmbeddingConfig, error) {
	if knowledgeBaseID == 0 {
		return nil, fmt.Errorf("knowledgeBaseId is required")
	}
	kb, err := r.kbRepo.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge base: %w", err)
	}
	if kb == nil {
		return nil, fmt.Errorf("knowledge base %d not found", knowledgeBaseID)
	}

	cfg := vectorstore.EmbeddingConfig{
		APIKey:     r.defaultCfg.Models.APIKey,
		BaseURL:    r.defaultCfg.Models.BaseURL,
		Model:      r.defaultCfg.Models.EmbeddingModel,
		Dimensions: 0,
	}

	if kb.EmbeddingParams != nil {
		if params, err := jsonToMap(kb.EmbeddingParams); err == nil {
			cfg = mergeEmbeddingParams(cfg, params)
		}
	}

	// Resolve embedding model reference from knowledge base settings first.
	if strings.TrimSpace(kb.EmbeddingModel) != "" {
		if modelCfg, err := r.resolveModelConfig(ctx, kb.EmbeddingModel); err == nil && modelCfg != nil {
			cfg = mergeModelConfig(cfg, modelCfg)
		}
	}

	// Allow embedding params override to reference a dedicated model config id.
	if kb.EmbeddingParams != nil {
		if params, err := jsonToMap(kb.EmbeddingParams); err == nil {
			if modelRef := stringFromParams(params, "modelConfigId", "model_config_id"); modelRef != "" {
				if modelCfg, err := r.resolveModelConfig(ctx, modelRef); err == nil && modelCfg != nil {
					cfg = mergeModelConfig(cfg, modelCfg)
				}
			}
		}
	}

	if cfg.Dimensions <= 0 {
		cfg.Dimensions = r.defaultEmbeddingDims()
	}

	return &cfg, nil
}

func (r *KBEmbeddingResolver) resolveModelConfig(ctx context.Context, ref string) (*models.AIModelConfig, error) {
	model, err := r.modelRepo.FindByID(ctx, ref)
	if err != nil {
		return nil, err
	}
	if model != nil {
		return model, nil
	}
	return r.modelRepo.FindByModelCode(ctx, ref)
}

func (r *KBEmbeddingResolver) defaultEmbeddingDims() int {
	return rag.DefaultEmbeddingDims
}

func mergeEmbeddingParams(cfg vectorstore.EmbeddingConfig, params map[string]any) vectorstore.EmbeddingConfig {
	if val := stringFromParams(params, "apiKey", "api_key"); val != "" {
		cfg.APIKey = val
	}
	if val := stringFromParams(params, "baseURL", "base_url", "endpoint"); val != "" {
		cfg.BaseURL = val
	}
	if val := stringFromParams(params, "model", "modelName", "model_name"); val != "" {
		cfg.Model = val
	}
	if dims := intFromParams(params, "dimensions", "dims"); dims > 0 {
		cfg.Dimensions = dims
	}
	return cfg
}

func mergeModelConfig(cfg vectorstore.EmbeddingConfig, modelCfg *models.AIModelConfig) vectorstore.EmbeddingConfig {
	if modelCfg == nil {
		return cfg
	}
	params := map[string]any{}
	for k, v := range modelCfg.ConfigJSON {
		params[k] = v
	}
	// Fallback to model metadata when config does not include explicit model name.
	if strings.TrimSpace(cfg.Model) == "" && strings.TrimSpace(modelCfg.ModelName) != "" {
		cfg.Model = modelCfg.ModelName
	}
	if strings.TrimSpace(cfg.Model) == "" && strings.TrimSpace(modelCfg.ModelCode) != "" {
		cfg.Model = modelCfg.ModelCode
	}
	cfg = mergeEmbeddingParams(cfg, params)
	return cfg
}

func jsonToMap(data datatypes.JSON) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func stringFromParams(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := params[key]; ok {
			switch v := val.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			case fmt.Stringer:
				str := v.String()
				if strings.TrimSpace(str) != "" {
					return strings.TrimSpace(str)
				}
			default:
				str := fmt.Sprintf("%v", v)
				if strings.TrimSpace(str) != "" {
					return strings.TrimSpace(str)
				}
			}
		}
	}
	return ""
}

func intFromParams(params map[string]any, keys ...string) int {
	for _, key := range keys {
		if val, ok := params[key]; ok {
			switch v := val.(type) {
			case float64:
				return int(v)
			case float32:
				return int(v)
			case int:
				return v
			case int64:
				return int(v)
			case json.Number:
				if i, err := v.Int64(); err == nil {
					return int(i)
				}
			case string:
				if strings.TrimSpace(v) == "" {
					continue
				}
				if i, err := strconv.Atoi(v); err == nil {
					return i
				}
			}
		}
	}
	return 0
}
