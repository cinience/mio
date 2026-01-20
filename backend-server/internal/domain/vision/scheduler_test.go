package vision

import (
	"testing"
	"time"

	"backend-server/internal/adapters/manager/types"
)

func TestBuildPrompt(t *testing.T) {
	tests := []struct {
		name   string
		system string
		rule   types.VisionAgentRule
		want   string
	}{
		{
			name:   "system and template",
			system: "系统提示",
			rule:   types.VisionAgentRule{PromptTemplate: "规则提示"},
			want:   "系统提示\n规则提示",
		},
		{
			name:   "condition fallback",
			system: "",
			rule:   types.VisionAgentRule{ConditionText: "条件描述"},
			want:   "条件描述",
		},
		{
			name:   "default prompt",
			system: " ",
			rule:   types.VisionAgentRule{},
			want:   "请描述画面内容",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildPrompt(tt.system, tt.rule); got != tt.want {
				t.Fatalf("buildPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildStreamURI(t *testing.T) {
	tests := []struct {
		name    string
		source  types.VisionAgentSource
		want    string
		wantErr bool
	}{
		{
			name:   "plain rtsp",
			source: types.VisionAgentSource{Protocol: "rtsp", SourceURI: "rtsp://example.com/live"},
			want:   "rtsp://example.com/live",
		},
		{
			name: "inject credentials",
			source: types.VisionAgentSource{
				Protocol:  "rtsp",
				SourceURI: "rtsp://example.com/live",
				Username:  "user",
				Password:  "pass",
			},
			want: "rtsp://user:pass@example.com/live",
		},
		{
			name: "keep existing credentials",
			source: types.VisionAgentSource{
				Protocol:  "rtsp",
				SourceURI: "rtsp://user:pass@example.com/live",
				Username:  "override",
			},
			want: "rtsp://user:pass@example.com/live",
		},
		{
			name:    "unsupported protocol",
			source:  types.VisionAgentSource{Protocol: "gb28181", SourceURI: "gb28181://foo"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildStreamURI(tt.source)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("buildStreamURI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseVLMResult(t *testing.T) {
	raw := "结果: true\n置信度: 0.78\n标签: dog,crossing\n摘要: 狗越界"
	parsed := parseVLMResult(raw)
	if !parsed.HasHit || !parsed.Hit {
		t.Fatalf("expected hit=true, got %+v", parsed)
	}
	if !parsed.HasConfidence || parsed.Confidence < 0.7 {
		t.Fatalf("expected confidence parsed, got %+v", parsed)
	}
	if parsed.Labels != "dog,crossing" {
		t.Fatalf("expected labels parsed, got %q", parsed.Labels)
	}
	if parsed.Summary == "" {
		t.Fatalf("expected summary parsed")
	}
}

func TestRecordHit(t *testing.T) {
	scheduler := &Scheduler{}
	state := &taskState{}
	now := time.Now()
	_, _, ready := scheduler.recordHit(state, now, 2, 0)
	if ready {
		t.Fatalf("expected minHitCount to block first hit")
	}
	_, _, ready = scheduler.recordHit(state, now.Add(time.Second), 2, 0)
	if !ready {
		t.Fatalf("expected minHitCount to pass on second hit")
	}

	state = &taskState{}
	_, _, ready = scheduler.recordHit(state, now, 0, 2)
	if ready {
		t.Fatalf("expected minHitSeconds to block first hit")
	}
	_, _, ready = scheduler.recordHit(state, now.Add(3*time.Second), 0, 2)
	if !ready {
		t.Fatalf("expected minHitSeconds to pass after duration")
	}
}

func TestShouldSkipMotion(t *testing.T) {
	scheduler := &Scheduler{}
	state := &taskState{}
	frame := []byte("frame")
	if scheduler.shouldSkipMotion(state, frame, true) {
		t.Fatalf("expected first frame to be processed")
	}
	if !scheduler.shouldSkipMotion(state, frame, true) {
		t.Fatalf("expected identical frame to be skipped")
	}
}
