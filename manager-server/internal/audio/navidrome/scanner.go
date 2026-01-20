package navidrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

// Artwork contains cover metadata extracted from an audio file.
type Artwork struct {
	MIMEType string
	Data     []byte
}

// ScanResult captures metadata derived from an audio artifact.
type ScanResult struct {
	EpisodeTitle string
	ShowTitle    string
	PrimaryHost  string
	Category     string
	PublishAt    *time.Time
	Season       int
	Episode      int

	DurationSeconds float64
	Bitrate         int
	Metadata        map[string]any
	Artwork         *Artwork
}

// Scanner extracts metadata and artwork from audio files.
type Scanner interface {
	Scan(ctx context.Context, path string) (*ScanResult, error)
}

// FFProbeClient runs ffprobe to derive duration/bitrate information.
type FFProbeClient interface {
	Probe(ctx context.Context, path string) (*ProbeResult, error)
}

// ProbeResult contains aggregated media characteristics.
type ProbeResult struct {
	Duration float64
	Bitrate  int
}

// CommandProbe executes ffprobe.
type CommandProbe struct {
	Binary string
}

// Probe runs ffprobe and parses the resulting JSON.
func (p *CommandProbe) Probe(ctx context.Context, path string) (*ProbeResult, error) {
	bin := p.Binary
	if strings.TrimSpace(bin) == "" {
		bin = "ffprobe"
	}
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=duration,bit_rate",
		"-of", "json",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var payload struct {
		Streams []struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(payload.Streams) == 0 {
		return nil, errors.New("ffprobe returned no audio streams")
	}
	stream := payload.Streams[0]
	result := ProbeResult{}
	if stream.Duration != "" {
		if seconds, err := strconv.ParseFloat(stream.Duration, 64); err == nil {
			result.Duration = seconds
		}
	}
	if stream.BitRate != "" {
		if br, err := strconv.ParseInt(stream.BitRate, 10, 64); err == nil {
			result.Bitrate = int(br)
		}
	}
	return &result, nil
}

// DefaultScanner uses github.com/dhowden/tag plus ffprobe.
type DefaultScanner struct {
	probe FFProbeClient
}

// NewDefaultScanner constructs a scanner with the provided probe implementation.
func NewDefaultScanner(probe FFProbeClient) *DefaultScanner {
	return &DefaultScanner{probe: probe}
}

// Scan extracts metadata and optional artwork.
func (s *DefaultScanner) Scan(ctx context.Context, path string) (*ScanResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}
	defer file.Close()

	meta, err := tag.ReadFrom(file)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}

	title := meta.Title()
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	show := firstNonEmpty(meta.Album(), meta.AlbumArtist())
	host := firstNonEmpty(meta.Artist(), meta.Composer())
	category := meta.Genre()
	metadata := map[string]any{
		"format": meta.Format(),
		"type":   meta.FileType(),
	}

	publishAt := extractPublishTime(meta)
	season, episode := extractSeasonEpisode(meta)

	var artwork *Artwork
	if picture := meta.Picture(); picture != nil && len(picture.Data) > 0 {
		artwork = &Artwork{
			MIMEType: picture.MIMEType,
			Data:     picture.Data,
		}
	}

	if raw := meta.Raw(); raw != nil {
		for k, v := range raw {
			if _, exists := metadata[k]; !exists {
				metadata[k] = v
			}
		}
	}

	result := &ScanResult{
		EpisodeTitle:    title,
		ShowTitle:       show,
		PrimaryHost:     host,
		Category:        category,
		PublishAt:       publishAt,
		Season:          season,
		Episode:         episode,
		Metadata:        metadata,
		DurationSeconds: 0,
		Bitrate:         0,
		Artwork:         artwork,
	}

	if s.probe != nil {
		if probe, err := s.probe.Probe(ctx, path); err == nil {
			if probe.Duration > 0 {
				result.DurationSeconds = probe.Duration
			}
			if probe.Bitrate > 0 {
				result.Bitrate = probe.Bitrate
			}
		}
	}

	if stat, err := file.Stat(); err == nil {
		result.Metadata["file_size"] = stat.Size()
	}

	return result, nil
}

func extractPublishTime(meta tag.Metadata) *time.Time {
	if meta.Year() != 0 {
		year := meta.Year()
		loc := time.UTC
		t := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
		return &t
	}
	if raw := meta.Raw(); raw != nil {
		if candidate, ok := raw["date"]; ok {
			if text, ok := candidate.(string); ok {
				if parsed, err := parseDateString(text); err == nil {
					return &parsed
				}
			}
		}
	}
	return nil
}

func extractSeasonEpisode(meta tag.Metadata) (int, int) {
	var season, episode int
	if raw := meta.Raw(); raw != nil {
		if v, ok := raw["season_number"]; ok {
			season = toInt(v)
		}
		if v, ok := raw["episode_number"]; ok {
			episode = toInt(v)
		}
		if v, ok := raw["tvseason"]; ok && season == 0 {
			season = toInt(v)
		}
		if v, ok := raw["tvepisode"]; ok && episode == 0 {
			episode = toInt(v)
		}
		if v, ok := raw["disc"]; ok && season == 0 {
			season = toInt(v)
		}
		if v, ok := raw["track"]; ok && episode == 0 {
			episode = toInt(v)
		}
	}
	return season, episode
}

func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
		if strings.Contains(v, "/") {
			if parts := strings.SplitN(v, "/", 2); len(parts) > 0 {
				if i, err := strconv.Atoi(parts[0]); err == nil {
					return i
				}
			}
		}
	}
	return 0
}

func parseDateString(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01",
		"2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format: %s", value)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
