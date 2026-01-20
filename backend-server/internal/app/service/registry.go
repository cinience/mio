package service

import (
	"context"
	"sync"
	"sync/atomic"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/adapters/workflow"
)

// Registry provides a threadsafe container for sharing service singletons
// without relying on ad-hoc package level globals.
type Registry struct {
	mu                  sync.RWMutex
	managerAPI          manager_api.ManagerAPIService
	deviceStatusManager *manager_api.DeviceStatusManager
	workflowRuntime     *workflow.Runtime
}

// NewRegistry creates an empty service registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// SetManagerAPIService registers the manager-api service instance.
func (r *Registry) SetManagerAPIService(service manager_api.ManagerAPIService) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.managerAPI = service
}

// ManagerAPIService returns the registered manager-api service.
func (r *Registry) ManagerAPIService() manager_api.ManagerAPIService {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.managerAPI
}

// SetDeviceStatusManager registers the device status manager instance.
func (r *Registry) SetDeviceStatusManager(manager *manager_api.DeviceStatusManager) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deviceStatusManager = manager
}

// DeviceStatusManager returns the registered device status manager instance.
func (r *Registry) DeviceStatusManager() *manager_api.DeviceStatusManager {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.deviceStatusManager
}

// SetWorkflowRuntime registers the workflow runtime instance.
func (r *Registry) SetWorkflowRuntime(rt *workflow.Runtime) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workflowRuntime = rt
}

// WorkflowRuntime returns the registered workflow runtime.
func (r *Registry) WorkflowRuntime() *workflow.Runtime {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.workflowRuntime
}

type registryContextKey struct{}

// WithRegistry injects the registry into the context.
func WithRegistry(ctx context.Context, registry *Registry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, registryContextKey{}, registry)
}

// FromContext extracts the registry from the context, falling back to the default registry.
func FromContext(ctx context.Context) *Registry {
	if ctx != nil {
		if reg, ok := ctx.Value(registryContextKey{}).(*Registry); ok && reg != nil {
			return reg
		}
	}
	return DefaultRegistry()
}

var defaultRegistry atomic.Pointer[Registry]

// SetDefaultRegistry installs the shared registry used by packages that cannot
// easily accept dependencies explicitly.
func SetDefaultRegistry(registry *Registry) {
	if registry == nil {
		return
	}
	defaultRegistry.Store(registry)
}

// DefaultRegistry returns the globally shared registry. It lazily initialises
// an empty registry to avoid nil checks across the codebase.
func DefaultRegistry() *Registry {
	if reg := defaultRegistry.Load(); reg != nil {
		return reg
	}

	newRegistry := NewRegistry()
	if defaultRegistry.CompareAndSwap(nil, newRegistry) {
		return newRegistry
	}
	return defaultRegistry.Load()
}
