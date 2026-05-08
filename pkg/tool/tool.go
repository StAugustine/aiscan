package tool

import (
	"context"
	"fmt"

	"github.com/chainreactors/aiscan/pkg/provider"
	"github.com/chainreactors/aiscan/pkg/registry"
)

type Tool interface {
	Name() string
	Description() string
	Definition() provider.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
}

type ToolRegistry struct {
	*registry.OrderedRegistry[Tool]
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{OrderedRegistry: registry.New[Tool]()}
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
