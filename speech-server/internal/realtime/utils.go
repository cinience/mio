package realtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"speech-server/internal/logger"
)

func (h *connectionHandler) sendError(eventID string, err error) error {
	logger.Errorf("realtime: sending error: %v", err)
	event := openairt.ErrorEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeError,
			EventID: uuid.NewString(),
		},
		Error: openairt.Error{
			Message: err.Error(),
		},
	}
	return h.sendEvent(event)
}

func (h *connectionHandler) sendEvent(event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return h.conn.WriteMessage(websocket.TextMessage, payload)
}

func extractSessionVADModel(data []byte) string {
	var envelope struct {
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	return findVADModelInSession(envelope.Session)
}

func extractSessionMetadata(data []byte) sessionMetadata {
	var envelope struct {
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return sessionMetadata{}
	}
	return findSessionMetadata(envelope.Session)
}

func findVADModelInSession(session map[string]any) string {
	if session == nil {
		return ""
	}
	if model := findVADModelInAudio(session); model != "" {
		return model
	}
	for _, key := range []string{"realtime", "transcription"} {
		branch, _ := session[key].(map[string]any)
		if branch == nil {
			continue
		}
		if model := findVADModelInAudio(branch); model != "" {
			return model
		}
	}
	return ""
}

func findVADModelInAudio(node map[string]any) string {
	if node == nil {
		return ""
	}
	audio, _ := node["audio"].(map[string]any)
	if audio == nil {
		return ""
	}
	input, _ := audio["input"].(map[string]any)
	if input == nil {
		return ""
	}
	turnDetection, _ := input["turn_detection"].(map[string]any)
	if turnDetection == nil {
		return ""
	}
	if serverVad, _ := turnDetection["server_vad"].(map[string]any); serverVad != nil {
		if model := firstString(serverVad, "provider", "model", "name"); model != "" {
			return model
		}
	}
	if model := firstString(turnDetection, "provider", "model", "name"); model != "" {
		return model
	}
	return ""
}

func firstString(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if str, ok := value.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}

func findSessionMetadata(session map[string]any) sessionMetadata {
	var meta sessionMetadata
	if session == nil {
		return meta
	}
	if root := extractMetadataMap(session); root != nil {
		mergeMetadataMap(&meta, root)
	}
	for _, branchKey := range []string{"realtime", "transcription"} {
		if branch, _ := session[branchKey].(map[string]any); branch != nil {
			if block := extractMetadataMap(branch); block != nil {
				mergeMetadataMap(&meta, block)
			}
		}
	}
	return meta
}

func extractMetadataMap(node map[string]any) map[string]any {
	if node == nil {
		return nil
	}
	meta, _ := node["metadata"].(map[string]any)
	return meta
}

func mergeMetadataMap(meta *sessionMetadata, raw map[string]any) {
	if meta == nil || raw == nil {
		return
	}
	// noise/audio filtering
	if v, ok := raw["skip_noise_filter"]; ok {
		if parsed, ok := asBool(v); ok {
			meta.SkipNoiseFilter = &parsed
		}
	}
	if v, ok := raw["noise_energy_threshold"]; ok {
		if parsed, ok := asFloat(v); ok {
			val := float32(parsed)
			meta.EnergyThreshold = &val
		}
	}
	if v, ok := raw["noise_min_duration"]; ok {
		if parsed, ok := asFloat(v); ok {
			val := float32(parsed)
			meta.MinDuration = &val
		}
	}
	if v, ok := raw["language_hint"]; ok {
		if parsed, ok := asString(v); ok {
			meta.LanguageHint = &parsed
		}
	}
	if v, ok := raw["language_whitelist"]; ok {
		list := parseLanguageWhitelist(v)
		meta.LanguageWhitelist = &list
	}
	// speaker metadata
	parseSpeakerMetadata(meta, raw, false)
	if node, ok := raw["speaker"]; ok {
		if m, ok := node.(map[string]any); ok {
			parseSpeakerMetadata(meta, m, true)
		}
	}
}

func parseLanguageWhitelist(raw any) []string {
	switch v := raw.(type) {
	case string:
		return splitLanguageString(v)
	case []any:
		var result []string
		for _, item := range v {
			if str, ok := asString(item); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func splitLanguageString(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';'
	})
	var result []string
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func asBool(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		switch lower {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	case float64:
		return v != 0, true
	}
	return false, false
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		if err == nil {
			return parsed, true
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func asString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), true
	case fmt.Stringer:
		return strings.TrimSpace(v.String()), true
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64)), true
	case int:
		return strings.TrimSpace(strconv.Itoa(v)), true
	case json.Number:
		return strings.TrimSpace(v.String()), true
	}
	return "", false
}

func parseSpeakerMetadata(meta *sessionMetadata, raw map[string]any, nested bool) {
	if meta == nil || raw == nil {
		return
	}
	for key, val := range raw {
		k := strings.ToLower(strings.TrimSpace(key))
		switch {
		case strings.HasPrefix(k, "speaker.") && len(k) > len("speaker."):
			sub := strings.TrimPrefix(k, "speaker.")
			applySpeakerField(meta, sub, val)
		case nested:
			applySpeakerField(meta, k, val)
		}
	}
}

func applySpeakerField(meta *sessionMetadata, field string, val any) {
	switch field {
	case "intent":
		if s, ok := asString(val); ok && s != "" {
			meta.SpeakerIntent = &s
		}
	case "speaker_id", "id":
		if s, ok := asString(val); ok && s != "" {
			meta.SpeakerID = &s
		}
	case "speaker_name", "name":
		if s, ok := asString(val); ok && s != "" {
			meta.SpeakerName = &s
		}
	case "threshold":
		if f, ok := asFloat(val); ok && f > 0 {
			v := float32(f)
			meta.SpeakerThreshold = &v
		}
	case "min_duration":
		if f, ok := asFloat(val); ok && f > 0 {
			v := float32(f)
			meta.SpeakerMinDuration = &v
		}
	}
}

func extractResponseText(params openairt.ResponseCreateParams) string {
	var parts []string
	for _, item := range params.Input {
		if item.User == nil {
			continue
		}
		for _, content := range item.User.Content {
			if content.Type == openairt.MessageContentTypeInputText {
				segment := strings.TrimSpace(content.Text)
				if segment != "" {
					parts = append(parts, segment)
				}
			}
		}
	}
	text := strings.TrimSpace(strings.Join(parts, " "))
	if text == "" {
		text = strings.TrimSpace(params.Instructions)
	}
	return text
}
