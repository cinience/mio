package clients

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"manager-server/internal/kb/ingest"
)

const (
	defaultRequestTimeout = 30 * time.Second
)

// EmbeddedCrawler provides a lightweight in-process crawler supporting URL/RSS/Sitemap.
type EmbeddedCrawler struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewEmbeddedCrawler creates a crawler backed by the standard HTTP client.
func NewEmbeddedCrawler(timeout time.Duration) *EmbeddedCrawler {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &EmbeddedCrawler{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		now: time.Now,
	}
}

// Parse inspects a source and returns a parse tree.
func (c *EmbeddedCrawler) Parse(ctx context.Context, opts ParseOptions) (*ingest.ParseTree, error) {
	if strings.TrimSpace(opts.Connector) == "" {
		return nil, errors.New("connector required")
	}
	switch opts.Connector {
	case "url":
		return c.parseURL(ctx, opts)
	case "rss":
		return c.parseRSS(ctx, opts)
	case "sitemap":
		return c.parseSitemap(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported connector: %s", opts.Connector)
	}
}

// Stage retrieves actual content for a selected node.
func (c *EmbeddedCrawler) Stage(ctx context.Context, opts StageOptions) (*ingest.StageResult, error) {
	if strings.TrimSpace(opts.Item.URI) == "" {
		return nil, errors.New("stage URI missing")
	}
	meta := cloneMetadata(opts.Item.Metadata)
	title := metadataString(meta, "title", opts.Item.URI)
	origin := metadataString(meta, "id", opts.Item.URI)
	meta["id"] = origin
	meta["title"] = title

	resp, err := c.fetch(ctx, opts.Item.URI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	rawContent := ""
	if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") {
		rawContent = string(body)
	}

	result := &ingest.StageResult{
		Title:       title,
		OriginID:    origin,
		SourceURI:   opts.Item.URI,
		ContentType: contentType,
		SizeBytes:   int64(len(body)),
		Reader:      io.NopCloser(bytes.NewReader(body)),
		RawContent:  rawContent,
		Metadata:    meta,
	}
	return result, nil
}

func (c *EmbeddedCrawler) parseURL(ctx context.Context, opts ParseOptions) (*ingest.ParseTree, error) {
	rawURL, _ := toString(opts.Params["url"])
	if rawURL == "" {
		return nil, errors.New("url parameter required")
	}

	now := c.now()
	node := &ingest.ParseNode{
		ID:         rawURL,
		Title:      titleFromMetadata(opts.Metadata, rawURL),
		Type:       "document",
		URI:        rawURL,
		Selectable: true,
		Metadata: map[string]any{
			"id":    rawURL,
			"title": titleFromMetadata(opts.Metadata, rawURL),
		},
	}
	return &ingest.ParseTree{
		Connector:   opts.Connector,
		GeneratedAt: now,
		ExpiresAt:   now.Add(30 * time.Minute),
		Root:        []*ingest.ParseNode{node},
		Metadata:    map[string]any{"count": 1},
	}, nil
}

func (c *EmbeddedCrawler) parseRSS(ctx context.Context, opts ParseOptions) (*ingest.ParseTree, error) {
	rawURL, _ := toString(opts.Params["url"])
	if rawURL == "" {
		return nil, errors.New("url parameter required")
	}
	resp, err := c.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rss: %w", err)
	}

	nodes, title, rssErr := parseRSSFeed(body, rawURL)
	if len(nodes) == 0 {
		atomNodes, atomTitle, atomErr := parseAtomFeed(body, rawURL)
		if atomErr == nil && len(atomNodes) > 0 {
			nodes = atomNodes
			title = atomTitle
		} else if rssErr != nil {
			return nil, fmt.Errorf("parse rss: %w", rssErr)
		} else if atomErr != nil {
			return nil, fmt.Errorf("parse atom: %w", atomErr)
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("feed contains no entries")
	}
	return buildFeedTree(opts.Connector, rawURL, title, nodes, c.now()), nil
}

func (c *EmbeddedCrawler) parseSitemap(ctx context.Context, opts ParseOptions) (*ingest.ParseTree, error) {
	rawURL, _ := toString(opts.Params["url"])
	if rawURL == "" {
		return nil, errors.New("url parameter required")
	}
	resp, err := c.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read sitemap: %w", err)
	}

	type urlEntry struct {
		Loc string `xml:"loc"`
	}
	type urlSet struct {
		URLs []urlEntry `xml:"url"`
	}

	var sitemap urlSet
	if err := xml.Unmarshal(body, &sitemap); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}

	nodes := make([]*ingest.ParseNode, 0, len(sitemap.URLs))
	for i, entry := range sitemap.URLs {
		loc := strings.TrimSpace(entry.Loc)
		if loc == "" {
			continue
		}
		nodeID := fmt.Sprintf("%s#%d", rawURL, i)
		nodes = append(nodes, &ingest.ParseNode{
			ID:         nodeID,
			ParentID:   rawURL,
			Title:      loc,
			Type:       "document",
			URI:        loc,
			Selectable: true,
			Metadata: map[string]any{
				"id":    nodeID,
				"title": loc,
			},
		})
	}

	root := &ingest.ParseNode{
		ID:         rawURL,
		Title:      rawURL,
		Type:       "collection",
		Selectable: false,
		Children:   nodes,
		Metadata: map[string]any{
			"id":    rawURL,
			"title": rawURL,
		},
	}
	now := c.now()
	return &ingest.ParseTree{
		Connector:   opts.Connector,
		GeneratedAt: now,
		ExpiresAt:   now.Add(30 * time.Minute),
		Root:        []*ingest.ParseNode{root},
		Metadata: map[string]any{
			"count": len(nodes),
		},
	}, nil
}

func parseRSSFeed(body []byte, rawURL string) ([]*ingest.ParseNode, string, error) {
	type rssItem struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
		Guid  string `xml:"guid"`
	}
	type rssChannel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	}
	type rssEnvelope struct {
		Channel rssChannel `xml:"channel"`
	}

	var feed rssEnvelope
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, "", err
	}

	title := strings.TrimSpace(feed.Channel.Title)
	if title == "" {
		title = rawURL
	}

	nodes := make([]*ingest.ParseNode, 0, len(feed.Channel.Items))
	for i, item := range feed.Channel.Items {
		link := strings.TrimSpace(item.Link)
		if link == "" {
			continue
		}
		nodeID := strings.TrimSpace(item.Guid)
		if nodeID == "" {
			nodeID = fmt.Sprintf("%s#%d", rawURL, i)
		}
		nodeTitle := strings.TrimSpace(item.Title)
		if nodeTitle == "" {
			nodeTitle = link
		}
		nodes = append(nodes, &ingest.ParseNode{
			ID:         nodeID,
			ParentID:   rawURL,
			Title:      nodeTitle,
			Type:       "document",
			URI:        link,
			Selectable: true,
			Metadata: map[string]any{
				"id":    nodeID,
				"title": nodeTitle,
			},
		})
	}
	return nodes, title, nil
}

func parseAtomFeed(body []byte, rawURL string) ([]*ingest.ParseNode, string, error) {
	type atomLink struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	}
	type atomEntry struct {
		Title string     `xml:"title"`
		Links []atomLink `xml:"link"`
		ID    string     `xml:"id"`
	}
	type atomFeed struct {
		Title   string      `xml:"title"`
		Entries []atomEntry `xml:"entry"`
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, "", err
	}

	selectLink := func(links []atomLink) string {
		for _, link := range links {
			href := strings.TrimSpace(link.Href)
			if href == "" {
				continue
			}
			rel := strings.TrimSpace(link.Rel)
			if rel == "" || rel == "alternate" {
				return href
			}
		}
		for _, link := range links {
			href := strings.TrimSpace(link.Href)
			if href != "" {
				return href
			}
		}
		return ""
	}

	title := strings.TrimSpace(feed.Title)
	if title == "" {
		title = rawURL
	}
	nodes := make([]*ingest.ParseNode, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		link := selectLink(entry.Links)
		if link == "" {
			candidate := strings.TrimSpace(entry.ID)
			if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
				link = candidate
			}
		}
		if link == "" {
			continue
		}
		nodeTitle := strings.TrimSpace(entry.Title)
		if nodeTitle == "" {
			nodeTitle = link
		}
		nodeID := strings.TrimSpace(entry.ID)
		if nodeID == "" {
			nodeID = link
		}
		nodes = append(nodes, &ingest.ParseNode{
			ID:         nodeID,
			ParentID:   rawURL,
			Title:      nodeTitle,
			Type:       "document",
			URI:        link,
			Selectable: true,
			Metadata: map[string]any{
				"id":    nodeID,
				"title": nodeTitle,
			},
		})
	}
	return nodes, title, nil
}

func buildFeedTree(connector, rawURL, title string, nodes []*ingest.ParseNode, now time.Time) *ingest.ParseTree {
	root := &ingest.ParseNode{
		ID:         rawURL,
		Title:      title,
		Type:       "collection",
		Selectable: false,
		Children:   nodes,
		Metadata: map[string]any{
			"id":    rawURL,
			"title": title,
		},
	}
	return &ingest.ParseTree{
		Connector:   connector,
		GeneratedAt: now,
		ExpiresAt:   now.Add(30 * time.Minute),
		Root:        []*ingest.ParseNode{root},
		Metadata: map[string]any{
			"count": len(nodes),
		},
	}
}

func (c *EmbeddedCrawler) fetch(ctx context.Context, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", target, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %d", target, resp.StatusCode)
	}
	return resp, nil
}

func titleFromMetadata(meta map[string]any, fallback string) string {
	if meta == nil {
		return fallback
	}
	if v, ok := meta["title"]; ok {
		if s, ok := toString(v); ok && s != "" {
			return s
		}
	}
	return fallback
}

func metadataString(meta map[string]any, key string, fallback string) string {
	if meta == nil {
		return fallback
	}
	if v, ok := meta[key]; ok {
		if s, ok := toString(v); ok && s != "" {
			return s
		}
	}
	return fallback
}

func cloneMetadata(meta map[string]any) map[string]any {
	if meta == nil {
		return make(map[string]any)
	}
	cloned := make(map[string]any, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return cloned
}

func toString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), v != ""
	case fmt.Stringer:
		str := strings.TrimSpace(v.String())
		return str, str != ""
	default:
		return "", false
	}
}
