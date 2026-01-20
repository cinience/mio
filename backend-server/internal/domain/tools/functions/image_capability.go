package functions

import "strings"

func supportsImageCapability(cfg map[string]interface{}, capability string) bool {
	if cfg == nil {
		return true
	}
	value, ok := cfg["capabilities"]
	if !ok {
		return true
	}
	if s, ok := value.(string); ok {
		items := strings.Split(s, ",")
		for _, item := range items {
			if strings.TrimSpace(item) == capability {
				return true
			}
		}
		return false
	}
	if list, ok := value.([]interface{}); ok {
		for _, item := range list {
			if s, ok := item.(string); ok && strings.TrimSpace(s) == capability {
				return true
			}
		}
		return false
	}
	return true
}
