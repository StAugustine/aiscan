package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chainreactors/aiscan/pkg/acp"
	"github.com/chainreactors/aiscan/pkg/provider"
)

type API interface {
	NodeID() string
	RegisterNode(ctx context.Context, name string, meta map[string]any) (acp.Node, error)
	Space(ctx context.Context, name, description string) (acp.SpaceInfo, error)
	Send(ctx context.Context, spaceID string, content map[string]any, refs *acp.Ref) (acp.Message, error)
	Read(ctx context.Context, spaceID string, opts acp.ReadOptions) ([]acp.Message, error)
}

type StreamAPI interface {
	API
	Subscribe(ctx context.Context, spaceID string) (<-chan acp.Message, <-chan error, func(), error)
}

type ToolOptions struct {
	NodeName string
	NodeMeta map[string]any
}

func NewTools(client API, opts ToolOptions) []ACPTool {
	base := &toolBase{client: client, opts: opts}
	return []ACPTool{
		&SpaceTool{base: base},
		&SendTool{base: base},
		&ReadTool{base: base},
	}
}

type ACPTool interface {
	Name() string
	Description() string
	Definition() provider.ToolDefinition
	Execute(ctx context.Context, arguments string) (string, error)
}

type toolBase struct {
	client API
	opts   ToolOptions
	mu     sync.Mutex
}

func (b *toolBase) ensureNode(ctx context.Context) error {
	if b.client.NodeID() != "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client.NodeID() != "" {
		return nil
	}
	name := b.opts.NodeName
	if name == "" {
		name = "aiscan-agent"
	}
	meta := b.opts.NodeMeta
	if meta == nil {
		meta = map[string]any{}
	}
	_, err := b.client.RegisterNode(ctx, name, meta)
	return err
}

func encodeToolResult(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type SpaceTool struct {
	base *toolBase
}

func (t *SpaceTool) Name() string { return "acp_space" }

func (t *SpaceTool) Description() string {
	return "Create or join an ACP message space for collaboration with other nodes. Requires name and description."
}

func (t *SpaceTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "ACP space name",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Your role or intent in this space",
					},
				},
				"required": []string{"name", "description"},
			},
		},
	}
}

func (t *SpaceTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if err := t.base.ensureNode(ctx); err != nil {
		return "", err
	}
	info, err := t.base.client.Space(ctx, args.Name, args.Description)
	if err != nil {
		return "", err
	}
	allMessages, err := t.base.client.Read(ctx, info.ID, acp.ReadOptions{All: true})
	if err != nil {
		return encodeToolResult(info)
	}
	var startMessages []acp.Message
	for _, m := range allMessages {
		if len(m.Refs.Messages) == 0 && len(m.Refs.Nodes) == 0 {
			startMessages = append(startMessages, m)
		}
	}
	return encodeToolResult(spaceResult{SpaceInfo: info, StartMessages: startMessages})
}

type spaceResult struct {
	acp.SpaceInfo
	StartMessages []acp.Message `json:"start_messages"`
}

type SendTool struct {
	base *toolBase
}

func (t *SendTool) Name() string { return "acp_send" }

func (t *SendTool) Description() string {
	return "Send a structured ACP message to a space. Use refs.messages and refs.nodes to target context or recipients."
}

func (t *SendTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"space_id": map[string]any{
						"type":        "string",
						"description": "ACP space id",
					},
					"content": map[string]any{
						"type":        "object",
						"description": "Structured message content",
					},
					"refs": refSchema(),
				},
				"required": []string{"space_id", "content"},
			},
		},
	}
}

func (t *SendTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		SpaceID string         `json:"space_id"`
		Content map[string]any `json:"content"`
		Refs    *acp.Ref       `json:"refs"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if err := t.base.ensureNode(ctx); err != nil {
		return "", err
	}
	if args.Content == nil {
		return "", fmt.Errorf("content is required")
	}
	message, err := t.base.client.Send(ctx, args.SpaceID, args.Content, args.Refs)
	if err != nil {
		return "", err
	}
	return encodeToolResult(message)
}

type ReadTool struct {
	base *toolBase
}

func (t *ReadTool) Name() string { return "acp_read" }

func (t *ReadTool) Description() string {
	return "Read ACP messages from a space, optionally by related message context, after cursor, limit, or all messages."
}

func (t *ReadTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"space_id": map[string]any{
						"type":        "string",
						"description": "ACP space id",
					},
					"message_id": map[string]any{
						"type":        "string",
						"description": "Optional message id to read its ancestor and descendant context",
					},
					"after": map[string]any{
						"type":        "string",
						"description": "Optional message id cursor",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of messages",
					},
					"all": map[string]any{
						"type":        "boolean",
						"description": "Read all messages instead of only messages addressed to this node",
					},
				},
				"required": []string{"space_id"},
			},
		},
	}
}

func (t *ReadTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		SpaceID   string `json:"space_id"`
		MessageID string `json:"message_id"`
		After     string `json:"after"`
		Limit     int    `json:"limit"`
		All       bool   `json:"all"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if err := t.base.ensureNode(ctx); err != nil {
		return "", err
	}
	messages, err := t.base.client.Read(ctx, args.SpaceID, acp.ReadOptions{
		MessageID: args.MessageID,
		After:     args.After,
		Limit:     args.Limit,
		All:       args.All,
	})
	if err != nil {
		return "", err
	}
	return encodeToolResult(messages)
}

func refSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Optional references to messages or node recipients",
		"properties": map[string]any{
			"messages": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"nodes": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
	}
}
