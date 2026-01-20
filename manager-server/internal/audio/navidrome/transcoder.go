package navidrome

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TranscodeOptions configures the desired output format.
type TranscodeOptions struct {
	OutputDir    string
	TargetFormat string
	Bitrate      int
}

// TranscodeResult describes the produced artifact.
type TranscodeResult struct {
	Path         string
	TargetFormat string
	Bitrate      int
}

// Transcoder converts audio into target formats.
type Transcoder interface {
	Transcode(ctx context.Context, inputPath string, opts TranscodeOptions) (*TranscodeResult, error)
}

// NoopTranscoder bypasses conversion.
type NoopTranscoder struct{}

// Transcode returns the input as-is.
func (NoopTranscoder) Transcode(_ context.Context, inputPath string, opts TranscodeOptions) (*TranscodeResult, error) {
	return &TranscodeResult{
		Path:         inputPath,
		TargetFormat: opts.TargetFormat,
		Bitrate:      opts.Bitrate,
	}, nil
}

// FFMPEGTranscoder shells out to ffmpeg.
type FFMPEGTranscoder struct {
	Binary string
}

// Transcode invokes ffmpeg with basic parameters.
func (t *FFMPEGTranscoder) Transcode(ctx context.Context, inputPath string, opts TranscodeOptions) (*TranscodeResult, error) {
	targetFormat := strings.TrimPrefix(strings.ToLower(opts.TargetFormat), ".")
	if targetFormat == "" {
		targetFormat = "mp3"
	}

	baseDir := opts.OutputDir
	if strings.TrimSpace(baseDir) == "" {
		baseDir = filepath.Dir(inputPath)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare transcode directory: %w", err)
	}
	output := filepath.Join(baseDir, fmt.Sprintf("%d_transcoded.%s", time.Now().UnixNano(), targetFormat))

	args := []string{"-y", "-i", inputPath}
	if opts.Bitrate > 0 {
		args = append(args, "-b:a", strconv.Itoa(opts.Bitrate))
	}
	args = append(args, output)

	bin := t.Binary
	if strings.TrimSpace(bin) == "" {
		bin = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if outputBytes, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg transcode failed: %w (%s)", err, string(outputBytes))
	}
	return &TranscodeResult{
		Path:         output,
		TargetFormat: targetFormat,
		Bitrate:      opts.Bitrate,
	}, nil
}
