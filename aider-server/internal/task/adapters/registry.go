package adapters

import (
	"fmt"
	"strings"
)

type Adapter struct {
	Name    string
	Command string
	Args    []string
}

type Registry struct {
	items map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]Adapter{}}
}

func (r *Registry) Register(adapter Adapter) {
	name := strings.TrimSpace(adapter.Name)
	if name == "" {
		return
	}
	adapter.Name = name
	r.items[name] = adapter
}

func (r *Registry) Get(name string) (Adapter, bool) {
	adapter, ok := r.items[name]
	return adapter, ok
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.items))
	for key := range r.items {
		out = append(out, key)
	}
	return out
}

func BuildCommand(adapter Adapter, args []string) (string, []string, error) {
	cmd := strings.TrimSpace(adapter.Command)
	if cmd == "" {
		return "", nil, fmt.Errorf("adapter %q command not configured", adapter.Name)
	}
	merged := make([]string, 0, len(adapter.Args)+len(args))
	merged = append(merged, adapter.Args...)
	merged = append(merged, args...)
	return cmd, merged, nil
}
