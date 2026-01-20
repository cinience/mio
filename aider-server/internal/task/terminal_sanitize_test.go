package task

import "testing"

func TestSanitizeLogLineStripsANSI(t *testing.T) {
	t.Parallel()
	input := "\x1b[31mhello\x1b[0m"
	if got := SanitizeLogLine(input); got != "hello" {
		t.Fatalf("expected sanitized line, got %q", got)
	}
}
