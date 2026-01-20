package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"manager-server/internal/kb/ingest"
)

const (
	yuqueAPIBaseDefault  = "https://www.yuque.com/api/v2"
	yuqueDefaultPageSize = 100
	yuqueDefaultTimeout  = 20 * time.Second
	yuqueRawAcceptHeader = "application/json"
	yuqueAuthHeader      = "X-Auth-Token"
	yuqueDefaultOnlyPub  = false
)

type yuqueConnector struct {
	client  *http.Client
	apiBase string
}

func newYuqueConnector() Connector {
	return &yuqueConnector{
		client:  &http.Client{Timeout: yuqueDefaultTimeout},
		apiBase: yuqueAPIBaseDefault,
	}
}

func (c *yuqueConnector) Source() string {
	return "yuque"
}

func (c *yuqueConnector) Parse(ctx context.Context, req ingest.ParseRequest) (*ingest.ParseTree, error) {
	namespace := strings.TrimSpace(toString(req.Params["namespace"]))
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}
	token := strings.TrimSpace(toString(req.Credentials["token"]))
	if token == "" {
		return nil, errors.New("yuque token required")
	}
	onlyPublished := parseBoolWithDefault(req.Params["onlyPublished"], yuqueDefaultOnlyPub)

	listURL := fmt.Sprintf("%s/repos/%s/docs", c.apiBase, url.PathEscape(namespace))
	query := url.Values{}
	query.Set("limit", fmt.Sprintf("%d", yuqueDefaultPageSize))
	if onlyPublished {
		query.Set("status", "1")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build yuque request: %w", err)
	}
	request.Header.Set("Accept", yuqueRawAcceptHeader)
	request.Header.Set(yuqueAuthHeader, token)

	resp, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("yuque list request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("yuque list request failed: status %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID      int64  `json:"id"`
			Title   string `json:"title"`
			Slug    string `json:"slug"`
			Format  string `json:"format"`
			Status  int    `json:"status"`
			Updated string `json:"updated_at"`
			Created string `json:"created_at"`
			repoID  int64  `json:"book_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode yuque response: %w", err)
	}

	root := &ingest.ParseNode{
		ID:         namespace,
		Title:      namespace,
		Type:       "collection",
		Selectable: false,
		Metadata: map[string]any{
			"namespace": namespace,
		},
		Children: []*ingest.ParseNode{},
	}

	for _, doc := range payload.Data {
		if doc.Slug == "" {
			continue
		}
		if onlyPublished && doc.Status != 1 {
			continue
		}
		title := strings.TrimSpace(doc.Title)
		if title == "" {
			title = doc.Slug
		}
		child := &ingest.ParseNode{
			ID:         fmt.Sprintf("yuque:%s:%s", namespace, doc.Slug),
			ParentID:   root.ID,
			Title:      title,
			Type:       "document",
			URI:        doc.Slug,
			Selectable: true,
			Metadata: map[string]any{
				"namespace": namespace,
				"slug":      doc.Slug,
				"format":    doc.Format,
				"status":    doc.Status,
			},
		}
		root.Children = append(root.Children, child)
	}

	return &ingest.ParseTree{
		Connector:   c.Source(),
		GeneratedAt: time.Now(),
		Root:        []*ingest.ParseNode{root},
		Metadata: map[string]any{
			"count": len(root.Children),
		},
	}, nil
}

func (c *yuqueConnector) Stage(ctx context.Context, item ingest.ParseItem) (*ingest.StageResult, error) {
	namespace := strings.TrimSpace(toString(item.Metadata["namespace"]))
	slug := strings.TrimSpace(toString(item.Metadata["slug"]))
	if namespace == "" || slug == "" {
		return nil, errors.New("missing namespace or slug")
	}
	token := strings.TrimSpace(toString(item.Credentials["token"]))
	if token == "" {
		return nil, errors.New("yuque token required")
	}

	detailURL := fmt.Sprintf("%s/repos/%s/docs/%s", c.apiBase, url.PathEscape(namespace), url.PathEscape(slug))
	query := url.Values{}
	query.Set("raw", "1")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build yuque detail request: %w", err)
	}
	request.Header.Set("Accept", yuqueRawAcceptHeader)
	request.Header.Set(yuqueAuthHeader, token)

	resp, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("yuque detail request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("yuque detail request failed: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read yuque detail: %w", err)
	}

	var payload struct {
		Data struct {
			Title  string `json:"title"`
			Body   string `json:"body"`
			Format string `json:"format"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode yuque detail: %w", err)
	}

	title := strings.TrimSpace(payload.Data.Title)
	if title == "" {
		title = slug
	}
	content := payload.Data.Body
	reader := bytes.NewReader([]byte(content))

	metadata := map[string]any{
		"namespace": namespace,
		"slug":      slug,
		"format":    payload.Data.Format,
	}
	for k, v := range item.Metadata {
		metadata[k] = v
	}

	return &ingest.StageResult{
		Title:       title,
		OriginID:    item.NodeID,
		SourceURI:   fmt.Sprintf("https://www.yuque.com/%s/%s", namespace, slug),
		ContentType: "text/markdown",
		SizeBytes:   int64(len(content)),
		Reader:      io.NopCloser(reader),
		RawContent:  content,
		Metadata:    metadata,
	}, nil
}

func parseBoolWithDefault(value any, def bool) bool {
	if value == nil {
		return def
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		if lower == "true" || lower == "1" || lower == "yes" {
			return true
		}
		if lower == "false" || lower == "0" || lower == "no" {
			return false
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return def
}
