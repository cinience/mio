package redisservice

import (
	"path/filepath"
	"testing"

	"github.com/hdt3213/godis/config"
)

func TestServiceApplyAOFDefaults(t *testing.T) {
	origProps := config.Properties
	origVal := *config.Properties
	defer func() {
		config.Properties = origProps
		*config.Properties = origVal
	}()

	dir := t.TempDir()
	enabled := true
	usePreamble := true
	svc, err := New(Config{
		Address:           "127.0.0.1:0",
		DataDir:           dir,
		LogDir:            t.TempDir(),
		AOFEnabled:        &enabled,
		AOFFilename:       "appendonly.aof",
		AOFFsync:          "everysec",
		AOFUseRdbPreamble: &usePreamble,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := svc.prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if !config.Properties.AppendOnly {
		t.Fatalf("expected AOF enabled")
	}
	expected := filepath.Join(dir, "appendonly.aof")
	if config.Properties.AppendFilename != expected {
		t.Fatalf("expected append filename %q, got %q", expected, config.Properties.AppendFilename)
	}
	if config.Properties.AppendFsync != "everysec" {
		t.Fatalf("expected append fsync everysec, got %q", config.Properties.AppendFsync)
	}
	if !config.Properties.AofUseRdbPreamble {
		t.Fatalf("expected aof-use-rdb-preamble enabled")
	}
}

func TestServiceDisableAOF(t *testing.T) {
	origProps := config.Properties
	origVal := *config.Properties
	defer func() {
		config.Properties = origProps
		*config.Properties = origVal
	}()

	disabled := false
	svc, err := New(Config{
		Address:    "127.0.0.1:0",
		DataDir:    t.TempDir(),
		LogDir:     t.TempDir(),
		AOFEnabled: &disabled,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := svc.prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if config.Properties.AppendOnly {
		t.Fatalf("expected AOF disabled")
	}
}
