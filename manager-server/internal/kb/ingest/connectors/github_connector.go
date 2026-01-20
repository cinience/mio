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
	"path"
	"sort"
	"strings"
	"time"

	"manager-server/internal/kb/ingest"
)

const (
	defaultGitHubBranch   = "main"
	defaultGitHubTimeout  = 20 * time.Second
	maxGitHubTreeEntries  = 2000
	githubMediaTypeTree   = "application/vnd.github+json"
	githubMediaTypeRaw    = "application/vnd.github.raw"
	githubAPIBaseDefault  = "https://api.github.com"
	githubRawBaseTemplate = "https://raw.githubusercontent.com/%s/%s/%s/%s"
)

type gitHubConnector struct {
	httpClient *http.Client
	apiBase    string
	rawBase    string
}

func newGitHubConnector() Connector {
	return &gitHubConnector{
		httpClient: &http.Client{Timeout: defaultGitHubTimeout},
		apiBase:    githubAPIBaseDefault,
		rawBase:    "https://raw.githubusercontent.com",
	}
}

func (c *gitHubConnector) Source() string {
	return "github"
}

func (c *gitHubConnector) Parse(ctx context.Context, req ingest.ParseRequest) (*ingest.ParseTree, error) {
	params := req.Params
	if params == nil {
		return nil, errors.New("missing parameters")
	}
	owner := strings.TrimSpace(toString(params["owner"]))
	repo := strings.TrimSpace(toString(params["repo"]))
	if owner == "" || repo == "" {
		return nil, errors.New("owner and repo are required")
	}
	branch := strings.TrimSpace(toString(params["branch"]))
	if branch == "" {
		branch = defaultGitHubBranch
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", c.apiBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))
	q := url.Values{}
	q.Set("recursive", "1")
	apiURL = fmt.Sprintf("%s?%s", apiURL, q.Encode())

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build github request: %w", err)
	}
	request.Header.Set("Accept", githubMediaTypeTree)
	if token := strings.TrimSpace(toString(req.Credentials["token"])); token != "" {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("github tree request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github tree request failed: status %d", resp.StatusCode)
	}

	var payload struct {
		SHA  string `json:"sha"`
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode github tree: %w", err)
	}

	if len(payload.Tree) == 0 {
		return &ingest.ParseTree{
			Connector: c.Source(),
			Root:      []*ingest.ParseNode{},
		}, nil
	}

	if len(payload.Tree) > maxGitHubTreeEntries {
		payload.Tree = payload.Tree[:maxGitHubTreeEntries]
	}

	rootID := fmt.Sprintf("%s/%s@%s", owner, repo, branch)
	rootNode := &ingest.ParseNode{
		ID:         rootID,
		Title:      fmt.Sprintf("%s/%s", owner, repo),
		Type:       "repository",
		Selectable: false,
		Metadata: map[string]any{
			"owner":  owner,
			"repo":   repo,
			"branch": branch,
		},
	}

	dirMap := map[string]*ingest.ParseNode{"": rootNode}
	fileNodes := make([]*ingest.ParseNode, 0)

	sort.Slice(payload.Tree, func(i, j int) bool {
		return payload.Tree[i].Path < payload.Tree[j].Path
	})

	for _, entry := range payload.Tree {
		entryPath := strings.TrimSpace(entry.Path)
		if entryPath == "" {
			continue
		}

		dir := path.Dir(entryPath)
		if dir == "." {
			dir = ""
		}

		parent, ok := dirMap[dir]
		if !ok {
			parent = ensureGitHubDirectory(dirMap, dir, rootNode)
		}

		switch entry.Type {
		case "tree":
			dirMap[entryPath] = &ingest.ParseNode{
				ID:         fmt.Sprintf("%s/%s@%s:%s", owner, repo, branch, entryPath),
				ParentID:   parent.ID,
				Title:      path.Base(entryPath),
				Type:       "directory",
				Selectable: false,
				Children:   []*ingest.ParseNode{},
				Metadata: map[string]any{
					"path":   entryPath,
					"owner":  owner,
					"repo":   repo,
					"branch": branch,
				},
			}
			parent.Children = append(parent.Children, dirMap[entryPath])
		case "blob":
			if !isGitHubDocument(entryPath) {
				continue
			}
			title := path.Base(entryPath)
			node := &ingest.ParseNode{
				ID:         fmt.Sprintf("%s/%s@%s:%s", owner, repo, branch, entryPath),
				ParentID:   parent.ID,
				Title:      title,
				Type:       "document",
				URI:        entryPath,
				SizeBytes:  entry.Size,
				Selectable: true,
				Metadata: map[string]any{
					"path":   entryPath,
					"sha":    entry.SHA,
					"owner":  owner,
					"repo":   repo,
					"branch": branch,
				},
			}
			parent.Children = append(parent.Children, node)
			fileNodes = append(fileNodes, node)
		}
	}

	return &ingest.ParseTree{
		Connector:   c.Source(),
		GeneratedAt: time.Now(),
		Root:        []*ingest.ParseNode{rootNode},
		Metadata: map[string]any{
			"count": len(fileNodes),
		},
	}, nil
}

func (c *gitHubConnector) Stage(ctx context.Context, item ingest.ParseItem) (*ingest.StageResult, error) {
	pathValue := strings.TrimSpace(toString(item.Metadata["path"]))
	owner := strings.TrimSpace(toString(item.Metadata["owner"]))
	repo := strings.TrimSpace(toString(item.Metadata["repo"]))
	branch := strings.TrimSpace(toString(item.Metadata["branch"]))
	if pathValue == "" || owner == "" || repo == "" {
		return nil, errors.New("missing metadata for github stage")
	}
	if branch == "" {
		branch = defaultGitHubBranch
	}

	segments := strings.Split(pathValue, "/")
	escapedSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		escapedSegments = append(escapedSegments, url.PathEscape(segment))
	}
	rawURL := fmt.Sprintf("%s/%s/%s/%s/%s",
		strings.TrimRight(c.rawBase, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(branch),
		strings.Join(escapedSegments, "/"),
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build github raw request: %w", err)
	}
	if token := strings.TrimSpace(toString(item.Credentials["token"])); token != "" {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	request.Header.Set("Accept", githubMediaTypeRaw)

	resp, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download github content: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("download github content failed: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read github content: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	title := path.Base(pathValue)
	metadata := map[string]any{
		"path":   pathValue,
		"owner":  owner,
		"repo":   repo,
		"branch": branch,
	}
	for k, v := range item.Metadata {
		metadata[k] = v
	}

	return &ingest.StageResult{
		Title:       title,
		OriginID:    item.NodeID,
		SourceURI:   rawURL,
		ContentType: contentType,
		SizeBytes:   int64(len(body)),
		Reader:      io.NopCloser(bytes.NewReader(body)),
		RawContent:  string(body),
		Metadata:    metadata,
	}, nil
}

func ensureGitHubDirectory(dirMap map[string]*ingest.ParseNode, dir string, root *ingest.ParseNode) *ingest.ParseNode {
	if dir == "" {
		return root
	}
	if node, ok := dirMap[dir]; ok {
		return node
	}
	parent := ensureGitHubDirectory(dirMap, path.Dir(dir), root)
	if parent == nil {
		parent = root
	}
	child := &ingest.ParseNode{
		ID:         fmt.Sprintf("%s:%s", parent.ID, dir),
		ParentID:   parent.ID,
		Title:      path.Base(dir),
		Type:       "directory",
		Selectable: false,
		Children:   []*ingest.ParseNode{},
		Metadata: map[string]any{
			"path": dir,
		},
	}
	parent.Children = append(parent.Children, child)
	dirMap[dir] = child
	return child
}

func isGitHubDocument(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") || strings.HasSuffix(lower, ".mdx") || strings.HasSuffix(lower, ".txt")
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
