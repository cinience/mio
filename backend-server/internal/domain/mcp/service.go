package mcp

import (
	"context"
	"sync"
	"time"

	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
)

// MCPService wires together all MCP managers and provides a single lifecycle hook.
type MCPService struct {
	ctx           context.Context
	cancel        context.CancelFunc
	localManager  *LocalMCPManager
	globalManager *GlobalMCPManager
	devicePool    *McpClientPool
	manager       *MCPManager
}

// NewMCPService constructs a new MCP service that derives all internal contexts from the
// provided parent context.
func NewMCPService(parent context.Context, cfg config.MCPConfig) (*MCPService, error) {
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithCancel(parent)
	localManager := NewLocalMCPManager()
	globalManager := NewGlobalMCPManager(ctx, cfg.Global)
	devicePool := NewMcpClientPool(ctx, 30*time.Second)
	manager := NewMCPManager(localManager, globalManager, devicePool)

	return &MCPService{
		ctx:           ctx,
		cancel:        cancel,
		localManager:  localManager,
		globalManager: globalManager,
		devicePool:    devicePool,
		manager:       manager,
	}, nil
}

// Context exposes the shared root context for dependent components.
func (s *MCPService) Context() context.Context {
	if s == nil {
		return context.Background()
	}
	return s.ctx
}

// Manager returns the aggregate MCP manager facade.
func (s *MCPService) Manager() *MCPManager {
	if s == nil {
		return nil
	}
	return s.manager
}

// LocalManager returns the local MCP manager.
func (s *MCPService) LocalManager() *LocalMCPManager {
	if s == nil {
		return nil
	}
	return s.localManager
}

// GlobalManager returns the global MCP manager.
func (s *MCPService) GlobalManager() *GlobalMCPManager {
	if s == nil {
		return nil
	}
	return s.globalManager
}

// DevicePool exposes the device MCP session pool.
func (s *MCPService) DevicePool() *McpClientPool {
	if s == nil {
		return nil
	}
	return s.devicePool
}

// Shutdown stops every manager and cancels the derived context.
func (s *MCPService) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if s.manager != nil {
		if err := s.manager.Stop(); err != nil {
			log.Errorf("停止MCP管理器失败: %v", err)
		}
	}

	if s.devicePool != nil {
		s.devicePool.Close()
	}

	if s.cancel != nil {
		s.cancel()
	}

	return nil
}

var (
	defaultService   *MCPService
	serviceMu        sync.RWMutex
	serviceInitMutex sync.Mutex
)

// DefaultMCPService returns the default singleton service instance. The service is lazily
// initialized to avoid doing work during package init and can be replaced inside tests.
func DefaultMCPService() *MCPService {
	serviceMu.RLock()
	if defaultService != nil {
		defer serviceMu.RUnlock()
		return defaultService
	}
	serviceMu.RUnlock()

	serviceInitMutex.Lock()
	defer serviceInitMutex.Unlock()

	serviceMu.RLock()
	if defaultService != nil {
		serviceMu.RUnlock()
		return defaultService
	}
	serviceMu.RUnlock()

	cfg := config.GetConfig()
	svc, err := NewMCPService(context.Background(), cfg.MCP)
	if err != nil {
		panic(err)
	}

	serviceMu.Lock()
	defaultService = svc
	serviceMu.Unlock()
	return svc
}

// SetDefaultMCPService overrides the default service. The previous instance is shut down
// to avoid leaking goroutines.
func SetDefaultMCPService(service *MCPService) {
	serviceMu.Lock()
	defer serviceMu.Unlock()

	if defaultService == service {
		return
	}

	if defaultService != nil {
		_ = defaultService.Shutdown(context.Background())
	}
	defaultService = service
}
