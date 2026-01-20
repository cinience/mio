package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

var defaultExecutors = map[string]NodeExecutor{
	"start": func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
		result.appendLog("debug", "start node", node.ID, nil)
		return "", nil
	},
	"asr_stream": func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
		text, _ := result.State["audioInput"].(string)
		if text == "" {
			text, _ = result.State["text"].(string)
		}
		if text != "" {
			result.State["transcript"] = text
			result.appendLog("info", "transcript updated", node.ID, map[string]any{"text": text})
		}
		return "", nil
	},
	"intent_recognition": func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
		text := fmt.Sprint(result.State["transcript"])
		intent := matchIntent(text, node.Config)
		result.State["intent"] = intent
		result.appendLog("info", "intent recognized", node.ID, map[string]any{"intent": intent})
		return "", nil
	},
	"llm": func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
		prompt := fmt.Sprint(result.State["transcript"])
		model := getString(node.Config, "model", "model")
		response := fmt.Sprintf("[LLM:%s] %s", model, prompt)
		result.State["response"] = response
		result.appendLog("info", "llm response", node.ID, map[string]any{"model": model})
		return "", nil
	},
	"tts": func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
		voice := getString(node.Config, "voice", "default")
		speed := getNumber(node.Config, "speed", 1)
		output := map[string]any{
			"voice": voice,
			"speed": speed,
			"text":  result.State["response"],
		}
		result.State["tts_output"] = output
		result.appendLog("info", "tts prepared", node.ID, output)
		return "", nil
	},
	"condition": func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
		next := evaluateCondition(node.Config, result.State)
		if next != "" {
			result.appendLog("info", fmt.Sprintf("condition -> %s", next), node.ID, nil)
		}
		return next, nil
	},
}

func evaluateCondition(config map[string]any, state map[string]any) string {
	variable := getString(config, "variable", "intent")
	operator := strings.ToLower(getString(config, "operator", "equals"))
	value := strings.ToLower(fmt.Sprint(state[variable]))
	cases, _ := config["cases"].([]any)
	for _, raw := range cases {
		caseMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target := getString(caseMap, "output", "")
		caseValue := strings.ToLower(getString(caseMap, "value", ""))
		if target == "" {
			if fallback, ok := caseMap["default"].(bool); ok && fallback {
				if fallbackTarget := getString(caseMap, "output", ""); fallbackTarget != "" {
					return fallbackTarget
				}
			}
			continue
		}
		switch operator {
		case "contains":
			if caseValue != "" && strings.Contains(value, caseValue) {
				return target
			}
		default:
			if value == caseValue {
				return target
			}
		}
	}
	return getString(config, "defaultOutput", "")
}

func matchIntent(text string, config map[string]any) string {
	intents, _ := config["intents"].([]any)
	textLower := strings.ToLower(text)
	for _, raw := range intents {
		intentMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := getString(intentMap, "name", "")
		keywords, _ := intentMap["keywords"].([]any)
		for _, kw := range keywords {
			keyword := strings.ToLower(fmt.Sprint(kw))
			if keyword != "" && strings.Contains(textLower, keyword) {
				return name
			}
		}
		if fallback, ok := intentMap["default"].(bool); ok && fallback {
			return name
		}
	}
	return ""
}

func getString(config map[string]any, key, fallback string) string {
	if value, ok := config[key]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return fallback
}

func getNumber(config map[string]any, key string, fallback float64) float64 {
	if value, ok := config[key]; ok {
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case json.Number:
			if f, err := v.Float64(); err == nil {
				return f
			}
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	return fallback
}
