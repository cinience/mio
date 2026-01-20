package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPromptFlagsIfLowHelp(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "iflow")
	script := "#!/bin/sh\n" +
		"echo \"Usage: iflow [options] [command]\"\n" +
		"echo \"Options:\"\n" +
		"echo \"  -p, --prompt              Prompt. Appended to input on stdin (if any).\"\n" +
		"echo \"  -i, --prompt-interactive  Execute the provided prompt and continue in interactive mode\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	flags := DetectPromptFlags(scriptPath)
	if flags.Prompt != "--prompt" {
		t.Fatalf("Prompt flag mismatch: got %q", flags.Prompt)
	}
	if flags.PromptInteractive != "--prompt-interactive" {
		t.Fatalf("PromptInteractive flag mismatch: got %q", flags.PromptInteractive)
	}
}
