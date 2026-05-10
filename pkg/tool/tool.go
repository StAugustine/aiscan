package tool

import (
	"context"
	"fmt"

	"github.com/chainreactors/aiscan/pkg/provider"
)

type Tool interface {
	Name() string
	Description() string
	Definition() provider.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
}

type ToolRegistry struct {
	items map[string]Tool
	order []string
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{items: make(map[string]Tool)}
}

func (r *ToolRegistry) Register(t Tool) {
	name := t.Name()
	if _, exists := r.items[name]; !exists {
		r.order = append(r.order, name)
	}
	r.items[name] = t
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.items[name]
	return t, ok
}

func (r *ToolRegistry) All() []Tool {
	result := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.items[name])
	}
	return result
}

func (r *ToolRegistry) Definitions() []provider.ToolDefinition {
	all := r.All()
	defs := make([]provider.ToolDefinition, 0, len(all))
	for _, t := range all {
		defs = append(defs, t.Definition())
	}
	return defs
}

func (r *ToolRegistry) Execute(ctx context.Context, name, arguments string) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return t.Execute(ctx, arguments)
}
