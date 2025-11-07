package fantasyextensions

import (
	"encoding/json"
	"reflect"
	"testing"

	"charm.land/fantasy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Test_toolInfoFromMCPTool(t *testing.T) {
	t.Parallel()
	// Given
	mcpToolTestData := `{
			"name": "Email_me_service",
			"description": "Call this tool to send an email to me",
			"inputSchema": {
				"$schema": "http://json-schema.org/draft-07/schema#",
				"additionalProperties": true,
				"properties": {
					"message": { "description": "Message content", "type": "string" },
					"title": { "description": "Title of the message", "type": "string" }
				},
				"required": ["title", "message"],
				"type": "object"
			}
		}`
	var mcpTool *mcp.Tool
	err := json.Unmarshal([]byte(mcpToolTestData), &mcpTool)
	if err != nil {
		t.Fatalf("failed to unmarshal tool: %v", err)
	}

	// When
	toolInfo, err := toolInfoFromMCPTool(mcpTool)
	if err != nil {
		t.Fatalf("failed to get tool info: %v", err)
	}

	// Then
	expectedToolInfo := fantasy.ToolInfo{
		Name:        "Email_me_service",
		Description: "Call this tool to send an email to me",
		Parameters: map[string]any{
			"message": map[string]any{"description": "Message content", "type": "string"},
			"title":   map[string]any{"description": "Title of the message", "type": "string"},
		},
		Required: []string{"title", "message"},
	}
	if !reflect.DeepEqual(toolInfo, expectedToolInfo) {
		t.Fatalf("tool info does not match expected tool info: %v", toolInfo)
	}
}
