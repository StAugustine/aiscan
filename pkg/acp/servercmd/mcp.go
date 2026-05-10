package servercmd

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/chainreactors/aiscan/pkg/acp"
	acpserver "github.com/chainreactors/aiscan/pkg/acp/server"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type mcpBridge struct {
	service *acpserver.Service
	nodeID  string
	mu      sync.Mutex
}

func (b *mcpBridge) ensureNode(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nodeID != "" {
		return b.nodeID, nil
	}
	node, err := b.service.RegisterNode(ctx, acp.NodeCreate{
		Name: "mcp-client",
		Meta: map[string]any{"transport": "mcp"},
	})
	if err != nil {
		return "", err
	}
	b.nodeID = node.ID
	return b.nodeID, nil
}

func withMCP(acpHandler http.Handler, service *acpserver.Service) http.Handler {
	bridge := &mcpBridge{service: service}
	s := mcpserver.NewMCPServer("acp", "1.0.0")
	registerMCPTools(s, bridge)

	mcpHandler := mcpserver.NewStreamableHTTPServer(s)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/", acpHandler)
	return mux
}

func registerMCPTools(s *mcpserver.MCPServer, bridge *mcpBridge) {
	spaceTool := mcp.NewTool("acp_space",
		mcp.WithDescription("Create or join an ACP message space for collaboration with other nodes. Returns space info with id, name, nodes, and message count."),
		mcp.WithString("name", mcp.Required(), mcp.Description("ACP space name")),
		mcp.WithString("description", mcp.Required(), mcp.Description("Your role or intent in this space")),
	)
	s.AddTool(spaceTool, bridge.handleSpace)

	sendTool := mcp.NewTool("acp_send",
		mcp.WithDescription("Send a structured ACP message to a space. Use refs.messages and refs.nodes to target context or recipients."),
		mcp.WithString("space_id", mcp.Required(), mcp.Description("ACP space id")),
		mcp.WithObject("content", mcp.Required(), mcp.Description("Structured message content")),
		mcp.WithObject("refs", mcp.Description("Optional references: {\"messages\": [\"msg-id\"], \"nodes\": [\"node-id\"]}")),
	)
	s.AddTool(sendTool, bridge.handleSend)

	readTool := mcp.NewTool("acp_read",
		mcp.WithDescription("Read ACP messages from a space, optionally by related message context, after cursor, limit, or all messages."),
		mcp.WithString("space_id", mcp.Required(), mcp.Description("ACP space id")),
		mcp.WithString("message_id", mcp.Description("Optional message id to read its ancestor and descendant context")),
		mcp.WithString("after", mcp.Description("Optional message id cursor for pagination")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of messages to return")),
		mcp.WithBoolean("all", mcp.Description("Read all messages instead of only messages addressed to this node")),
	)
	s.AddTool(readTool, bridge.handleRead)
}

type mcpSpaceResult struct {
	acp.SpaceInfo
	StartMessages []acp.Message `json:"start_messages"`
}

func (b *mcpBridge) handleSpace(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	description, err := request.RequireString("description")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	nodeID, err := b.ensureNode(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	info, err := b.service.CreateSpace(ctx, nodeID, acp.SpaceCreate{
		Name:        name,
		Description: description,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	startMessages, err := b.service.ReadMessages(ctx, info.ID, "", acp.ReadOptions{})
	if err != nil {
		return marshalToolResult(info)
	}

	return marshalToolResult(mcpSpaceResult{SpaceInfo: info, StartMessages: startMessages})
}

func (b *mcpBridge) handleSend(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	spaceID, err := request.RequireString("space_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	args := request.GetArguments()
	content, ok := args["content"].(map[string]any)
	if !ok || content == nil {
		return mcp.NewToolResultError("content is required and must be a JSON object"), nil
	}

	var refs *acp.Ref
	if refsRaw, hasRefs := args["refs"]; hasRefs && refsRaw != nil {
		data, _ := json.Marshal(refsRaw)
		refs = &acp.Ref{}
		_ = json.Unmarshal(data, refs)
	}

	nodeID, err := b.ensureNode(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	message, err := b.service.SendMessage(ctx, spaceID, nodeID, acp.SendMessage{
		Content: content,
		Refs:    refs,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return marshalToolResult(message)
}

func (b *mcpBridge) handleRead(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	spaceID, err := request.RequireString("space_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	messageID := request.GetString("message_id", "")
	after := request.GetString("after", "")
	limit := request.GetInt("limit", 0)

	args := request.GetArguments()
	all, _ := args["all"].(bool)

	nodeID, err := b.ensureNode(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	messages, err := b.service.ReadMessages(ctx, spaceID, nodeID, acp.ReadOptions{
		MessageID: messageID,
		After:     after,
		Limit:     limit,
		All:       all,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return marshalToolResult(messages)
}

func marshalToolResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(data)), nil
}
