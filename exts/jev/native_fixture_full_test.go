//go:build full

package jev

import (
	"context"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

type nativeFixtureTool struct {
	definition *aop.ToolDefinition
	run        func(context.Context, string) (*coretool.Result, error)
}

func (t nativeFixtureTool) Name() string                    { return t.definition.Name }
func (t nativeFixtureTool) Description() string             { return t.definition.Description }
func (t nativeFixtureTool) Definition() *aop.ToolDefinition { return t.definition }
func (t nativeFixtureTool) Execute(ctx context.Context, arguments string) (*coretool.Result, error) {
	return t.run(ctx, arguments)
}
