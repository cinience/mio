package connectors

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"manager-server/internal/config"
	"manager-server/internal/kb/ingest"
	"manager-server/internal/kb/ingest/clients"
)

// Connector describes the parsing and staging behaviour for a specific source type.
type Connector interface {
	Source() string
	Parse(ctx context.Context, req ingest.ParseRequest) (*ingest.ParseTree, error)
	Stage(ctx context.Context, item ingest.ParseItem) (*ingest.StageResult, error)
}

// Registry keeps track of enabled connectors.
type Registry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

// ErrConnectorNotFound is returned when a connector source is not registered or disabled.
var ErrConnectorNotFound = errors.New("connector not found")

// NewRegistry builds a registry from provided connectors.
func NewRegistry(connectors []Connector) *Registry {
	reg := &Registry{
		connectors: make(map[string]Connector, len(connectors)),
	}
	for _, conn := range connectors {
		if conn == nil {
			continue
		}
		name := conn.Source()
		if name == "" {
			continue
		}
		reg.connectors[name] = conn
	}
	return reg
}

// Register adds or replaces a connector at runtime.
func (r *Registry) Register(conn Connector) {
	if conn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.connectors == nil {
		r.connectors = make(map[string]Connector)
	}
	r.connectors[conn.Source()] = conn
}

// Get retrieves a connector by name.
func (r *Registry) Get(name string) (Connector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if conn, ok := r.connectors[name]; ok {
		return conn, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrConnectorNotFound, name)
}

// List returns the set of registered connector names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}

// Builder constructs default connectors based on crawler client.
func Builder(client clients.CrawlerClient, enabled map[string]bool) []Connector {
	if client == nil || len(enabled) == 0 {
		return nil
	}

	normalized := make(map[string]bool, len(enabled))
	for name, ok := range enabled {
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		normalized[key] = true
	}

	// Embedded crawler currently understands a limited feature set.
	if _, ok := client.(*clients.EmbeddedCrawler); ok {
		normalized = filterAllowed(normalized, []string{"url", "rss", "sitemap"})
	}

	if len(normalized) == 0 {
		return nil
	}

	names := make([]string, 0, len(normalized))
	for name := range normalized {
		names = append(names, name)
	}
	sort.Strings(names)

	connectors := make([]Connector, 0, len(names))
	for _, name := range names {
		connectors = append(connectors, newCrawlerConnector(name, client))
	}
	return connectors
}

func filterAllowed(source map[string]bool, allowList []string) map[string]bool {
	allowed := make(map[string]bool, len(allowList))
	for _, key := range allowList {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		if source[normalized] {
			allowed[normalized] = true
		}
	}
	return allowed
}

func BuilderWithConfig(client clients.CrawlerClient, configs map[string]config.ConnectorConfig) []Connector {
	advanced := make([]Connector, 0)
	crawlerCandidates := make(map[string]bool)

	for name, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(name))
		switch normalized {
		case "github":
			advanced = append(advanced, newGitHubConnector())
		case "yuque":
			advanced = append(advanced, newYuqueConnector())
		default:
			crawlerCandidates[normalized] = true
		}
	}

	crawlerConnectors := Builder(client, crawlerCandidates)
	result := append([]Connector(nil), advanced...)
	result = append(result, crawlerConnectors...)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Source() < result[j].Source()
	})

	return result
}
