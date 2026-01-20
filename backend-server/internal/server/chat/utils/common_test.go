package utils

import (
	"testing"

	"backend-server/internal/config"
)

func setTestConfig(t *testing.T, cfg *config.AppConfig) {
	t.Helper()
	original := config.GlobalConfig
	config.GlobalConfig = cfg
	t.Cleanup(func() {
		config.GlobalConfig = original
	})
}

func TestIsWakeupWordExactMatch(t *testing.T) {
	setTestConfig(t, &config.AppConfig{
		WakeupWords: []string{"你好小智", "hi, lily"},
	})

	if !IsWakeupWord("你好小智") {
		t.Fatalf("expected direct match to succeed")
	}

	if !IsWakeupWord("hi lily") {
		t.Fatalf("expected punctuation and space for 'hi lily' to be ignored")
	}

	if IsWakeupWord("unknown") {
		t.Fatalf("unexpected wakeup word match")
	}
}

func TestIsWakeupWordPinyinFuzzy(t *testing.T) {
	setTestConfig(t, &config.AppConfig{
		WakeupWords: []string{"你好小智"},
		WakeupWordMatch: config.WakeupWordMatchConfig{
			EnablePinyinFuzzy: true,
			MaxPinyinDistance: 1,
		},
	})

	cases := []string{"你好小志", "你好小治"}
	for _, c := range cases {
		if !IsWakeupWord(c) {
			t.Fatalf("expected fuzzy match for %s", c)
		}
	}

	if IsWakeupWord("你好小新") {
		t.Fatalf("expected different pronunciation not to match")
	}
}
