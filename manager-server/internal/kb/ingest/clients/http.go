package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"manager-server/internal/kb/ingest"
)

const (
	defaultHTTPCrawlerTimeout = 30 * time.Second
)

var (
	errCrawlerNotConfigured  = errors.New("http crawler not configured")
	errCrawlerMissingBase    = errors.New("crawler base url missing")
	errCrawlerMissingContent = errors.New("crawler stage response missing content")
)

// HTTPCrawler proxies parse/stage requests to an external crawler service.
type HTTPCrawler struct {
	baseURL   string
	uploadURL string
	apiKey    string
	client    *http.Client
}

// HTTPCrawlerOption configures optional parameters for the HTTP crawler client.
type HTTPCrawlerOption func(*HTTPCrawler)

// WithHTTPCrawlerTimeout overrides the default request timeout.
func WithHTTPCrawlerTimeout(timeout time.Duration) HTTPCrawlerOption {
	return func(c *HTTPCrawler) {
		if timeout > 0 {
			c.client.Timeout = timeout
		}
	}
}

// WithHTTPCrawlerAPIKey sets the API key header for outbound requests.
func WithHTTPCrawlerAPIKey(apiKey string) HTTPCrawlerOption {
	return func(c *HTTPCrawler) {
		c.apiKey = strings.TrimSpace(apiKey)
	}
}

// WithHTTPCrawlerUploadURL stores the upload callback URL (reserved for future use).
func WithHTTPCrawlerUploadURL(uploadURL string) HTTPCrawlerOption {
	return func(c *HTTPCrawler) {
		c.uploadURL = strings.TrimSpace(uploadURL)
	}
}

// NewHTTPCrawler constructs a crawler backed by an HTTP endpoint.
func NewHTTPCrawler(baseURL string, opts ...HTTPCrawlerOption) *HTTPCrawler {
	client := &HTTPCrawler{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client: &http.Client{
			Timeout: defaultHTTPCrawlerTimeout,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

// Parse delegates parse calls to the remote crawler service.
func (h *HTTPCrawler) Parse(ctx context.Context, opts ParseOptions) (*ingest.ParseTree, error) {
	if h == nil {
		return nil, errCrawlerNotConfigured
	}
	if strings.TrimSpace(h.baseURL) == "" {
		return nil, errCrawlerMissingBase
	}
	if strings.TrimSpace(opts.Connector) == "" {
		return nil, errors.New("connector required")
	}
	payload := map[string]any{
		"params":      opts.Params,
		"credentials": opts.Credentials,
		"metadata":    opts.Metadata,
	}
	req, err := h.newRequest(ctx, http.MethodPost, fmt.Sprintf("/connectors/%s/parse", url.PathEscape(opts.Connector)), payload)
	if err != nil {
		return nil, err
	}
	res, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crawler parse request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, h.errorFromResponse(res)
	}
	var response struct {
		Tree *ingest.ParseTree `json:"tree"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode parse response: %w", err)
	}
	if response.Tree == nil {
		return nil, errors.New("crawler parse response missing tree")
	}
	return response.Tree, nil
}

// Stage delegates stage calls to the remote crawler service.
func (h *HTTPCrawler) Stage(ctx context.Context, opts StageOptions) (*ingest.StageResult, error) {
	if h == nil {
		return nil, errCrawlerNotConfigured
	}
	if strings.TrimSpace(h.baseURL) == "" {
		return nil, errCrawlerMissingBase
	}
	if strings.TrimSpace(opts.Connector) == "" {
		return nil, errors.New("connector required")
	}
	if strings.TrimSpace(opts.Item.NodeID) == "" {
		return nil, errors.New("parse item nodeId required")
	}
	payload := map[string]any{
		"nodeId":      opts.Item.NodeID,
		"uri":         opts.Item.URI,
		"params":      opts.Item.Params,
		"metadata":    opts.Item.Metadata,
		"credentials": opts.Item.Credentials,
	}
	req, err := h.newRequest(ctx, http.MethodPost, fmt.Sprintf("/connectors/%s/stage", url.PathEscape(opts.Connector)), payload)
	if err != nil {
		return nil, err
	}
	res, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crawler stage request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, h.errorFromResponse(res)
	}
	var response struct {
		Title       string         `json:"title"`
		OriginID    string         `json:"originId"`
		SourceURI   string         `json:"sourceUri"`
		ContentType string         `json:"contentType"`
		SizeBytes   int64          `json:"sizeBytes"`
		RawContent  string         `json:"rawContent"`
		Metadata    map[string]any `json:"metadata"`
		DownloadURL string         `json:"downloadUrl"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode stage response: %w", err)
	}

	var reader io.ReadCloser
	var size int64 = response.SizeBytes
	raw := strings.TrimSpace(response.RawContent)

	switch {
	case response.DownloadURL != "":
		body, length, err := h.download(ctx, response.DownloadURL)
		if err != nil {
			return nil, err
		}
		reader = body
		if length >= 0 {
			size = length
		}
	case raw != "":
		reader = io.NopCloser(strings.NewReader(raw))
		if size <= 0 {
			size = int64(len(raw))
		}
	default:
		return nil, errCrawlerMissingContent
	}

	metadata := cloneMetadata(response.Metadata)
	title := response.Title
	if title == "" {
		if t, ok := metadata["title"].(string); ok && strings.TrimSpace(t) != "" {
			title = t
		} else if opts.Item.Metadata != nil {
			if t, ok := opts.Item.Metadata["title"].(string); ok && strings.TrimSpace(t) != "" {
				title = t
			}
		}
	}
	if title == "" {
		title = opts.Item.NodeID
	}
	originID := response.OriginID
	if originID == "" {
		if id, ok := metadata["originId"].(string); ok && strings.TrimSpace(id) != "" {
			originID = id
		} else {
			originID = opts.Item.NodeID
		}
	}

	result := &ingest.StageResult{
		Title:       title,
		OriginID:    originID,
		SourceURI:   response.SourceURI,
		ContentType: response.ContentType,
		SizeBytes:   size,
		Reader:      reader,
		RawContent:  response.RawContent,
		Metadata:    metadata,
	}
	return result, nil
}

func (h *HTTPCrawler) newRequest(ctx context.Context, method, endpoint string, payload any) (*http.Request, error) {
	u, err := url.Parse(h.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse crawler base url: %w", err)
	}
	u.Path = path.Join(u.Path, endpoint)
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode crawler payload: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("build crawler request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", h.apiKey))
	}
	return req, nil
}

func (h *HTTPCrawler) download(ctx context.Context, downloadURL string) (io.ReadCloser, int64, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return nil, 0, errCrawlerMissingContent
	}
	target, err := url.Parse(downloadURL)
	if err != nil {
		return nil, 0, fmt.Errorf("parse download url: %w", err)
	}
	if !target.IsAbs() {
		base, err := url.Parse(h.baseURL)
		if err != nil {
			return nil, 0, fmt.Errorf("parse crawler base url: %w", err)
		}
		target = base.ResolveReference(target)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build download request: %w", err)
	}
	if h.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", h.apiKey))
	}
	res, err := h.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("download staged content: %w", err)
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		return nil, 0, fmt.Errorf("download staged content failed: status %d", res.StatusCode)
	}
	return res.Body, res.ContentLength, nil
}

func (h *HTTPCrawler) errorFromResponse(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if len(body) == 0 {
		return fmt.Errorf("crawler request failed: status %d", res.StatusCode)
	}
	return fmt.Errorf("crawler request failed: status %d, body: %s", res.StatusCode, string(body))
}
