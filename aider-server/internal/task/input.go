package task

import "strings"

func BuildPromptPayload(prompt string) []byte {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	prompt = strings.TrimRight(prompt, "\r\n")
	return []byte(prompt)
}
