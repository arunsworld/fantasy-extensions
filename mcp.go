package fantasyextensions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"charm.land/fantasy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Additional context like auth tokens may be passed in the context if desired.
type MCPSessionMaker = func(context.Context) (*mcp.ClientSession, error)

func MCPTools(ctx context.Context, sessionMaker MCPSessionMaker) ([]fantasy.AgentTool, error) {
	session, err := sessionMaker(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP session: %w", err)
	}
	defer session.Close()

	listToolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error listing MCP tools: %w", err)
	}
	tools := listToolsResult.Tools

	result := make([]fantasy.AgentTool, 0, len(tools))
	for _, tool := range tools {
		toolInfo, err := toolInfoFromMCPTool(tool)
		if err != nil {
			return nil, fmt.Errorf("failed to get tool info: %w", err)
		}
		result = append(result, &mcpFantasyTool{
			toolInfo:     toolInfo,
			sessionMaker: sessionMaker,
		})
	}
	return result, nil
}

func toolInfoFromMCPTool(mcpTool *mcp.Tool) (fantasy.ToolInfo, error) {
	toolName := mcpTool.Name
	inputSchema, ok := mcpTool.InputSchema.(map[string]any)
	if !ok {
		return fantasy.ToolInfo{}, fmt.Errorf("error parsing MCP tool %s: input schema is not a map[string]any; instead got: %T", toolName, mcpTool.InputSchema)
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		return fantasy.ToolInfo{}, fmt.Errorf("error parsing MCP tool %s: properties is not a map[string]any; instead got: %T", toolName, inputSchema["properties"])
	}
	required := []string{}
	if inputSchema["required"] != nil {
		_required, ok := inputSchema["required"].([]any)
		if !ok {
			return fantasy.ToolInfo{}, fmt.Errorf("error parsing MCP tool %s: required is not a []string; instead got: %T", toolName, inputSchema["required"])
		}
		required = make([]string, 0, len(_required))
		for _, r := range _required {
			required = append(required, r.(string))
		}
	}
	return fantasy.ToolInfo{
		Name:        toolName,
		Description: mcpTool.Description,
		Parameters:  properties,
		Required:    required,
	}, nil
}

type argWrapper struct {
	jsonContent []byte
}

func (w *argWrapper) MarshalJSON() ([]byte, error) {
	return w.jsonContent, nil
}

type mcpFantasyTool struct {
	toolInfo        fantasy.ToolInfo
	providerOptions fantasy.ProviderOptions
	sessionMaker    MCPSessionMaker
}

func (t *mcpFantasyTool) Info() fantasy.ToolInfo {
	return t.toolInfo
}

func (t *mcpFantasyTool) Run(ctx context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	session, err := t.sessionMaker(ctx)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to create MCP session: %v", err)), nil
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      params.Name,
		Arguments: &argWrapper{jsonContent: []byte(params.Input)},
	})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}

	if result.StructuredContent != nil {
		jsonResponse, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		return fantasy.ToolResponse{
			Type:    "string",
			Content: string(jsonResponse),
		}, nil
	}
	if len(result.Content) == 0 {
		return fantasy.NewTextErrorResponse("no content returned from tool"), nil
	}
	output := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		textContent, ok := content.(*mcp.TextContent)
		if !ok {
			log.Printf("error casting content to text content: %T", content)
			continue
		}
		output = append(output, textContent.Text)
	}
	return fantasy.NewTextResponse(strings.Join(output, "\n")), nil
}

func (t *mcpFantasyTool) ProviderOptions() fantasy.ProviderOptions {
	return t.providerOptions
}

func (t *mcpFantasyTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	t.providerOptions = opts
}
