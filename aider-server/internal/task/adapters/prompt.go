package adapters

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type PromptFlags struct {
	Prompt string
	PromptInteractive string
}

var promptCache = struct {
	mu    sync.Mutex
	items map[string]PromptFlags
}{
	items: map[string]PromptFlags{},
}

func DetectPromptFlags(command string) PromptFlags {
	command = strings.TrimSpace(command)
	if command == "" {
		return PromptFlags{}
	}
	promptCache.mu.Lock()
	cached, ok := promptCache.items[command]
	promptCache.mu.Unlock()
	if ok {
		return cached
	}
	flags := inspectPromptFlags(command)
	promptCache.mu.Lock()
	promptCache.items[command] = flags
	promptCache.mu.Unlock()
	return flags
}

func AppendPromptArgs(args []string, prompt string, flags PromptFlags) []string {
	if prompt == "" {
		return args
	}
	if flags.Prompt != "" {
		return append(args, flags.Prompt, prompt)
	}
	return append(args, prompt)
}

func AppendPromptArgsWithFlag(args []string, prompt string, flag string) []string {
	if prompt == "" {
		return args
	}
	if flag != "" {
		return append(args, flag, prompt)
	}
	return append(args, prompt)
}

func inspectPromptFlags(command string) PromptFlags {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, _ := exec.CommandContext(ctx, command, "-h").CombinedOutput()
	return PromptFlags{
		Prompt:            findPromptFlag(string(output)),
		PromptInteractive: findPromptInteractiveFlag(string(output)),
	}
}

func findPromptFlag(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	longFlag := "--prompt"
	if strings.Contains(output, longFlag) {
		return longFlag
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	shortFlagRe := regexp.MustCompile(`(^|\\s)(-[A-Za-z])(?:,|\\s)`)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "prompt") {
			continue
		}
		if strings.Contains(line, longFlag) {
			return longFlag
		}
		if match := shortFlagRe.FindStringSubmatch(line); len(match) > 2 {
			return match[2]
		}
	}
	return ""
}

func findPromptInteractiveFlag(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	longFlag := "--prompt-interactive"
	if strings.Contains(output, longFlag) {
		return longFlag
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	shortFlagRe := regexp.MustCompile(`(^|\\s)(-[A-Za-z])(?:,|\\s)`)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "prompt-interactive") {
			continue
		}
		if strings.Contains(line, longFlag) {
			return longFlag
		}
		if match := shortFlagRe.FindStringSubmatch(line); len(match) > 2 {
			return match[2]
		}
	}
	return ""
}
