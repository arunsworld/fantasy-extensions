package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"charm.land/fantasy"
	fantasyextensions "github.com/arunsworld/fantasy-extensions"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "fantasy-mcp-client",
		Version: "1.0.0",
	}, nil)

	sessionMaker := func(ctx context.Context) (*mcp.ClientSession, error) {
		transport := &mcp.CommandTransport{Command: exec.Command("uvx", "mcp-server-time", "--local-timezone=Europe/London")}
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return nil, err
		}
		return session, nil
	}

	tools, err := fantasyextensions.MCPTools(context.Background(), sessionMaker)
	if err != nil {
		return err
	}

	var getCurrentTimeTool fantasy.AgentTool
	fmt.Println("tools:")
	for _, tool := range tools {
		fmt.Printf("- %s\n", tool.Info().Name)
		if tool.Info().Name == "get_current_time" {
			getCurrentTimeTool = tool
		}
	}

	if getCurrentTimeTool == nil {
		return fmt.Errorf("get_current_time tool not found")
	}

	resp, err := getCurrentTimeTool.Run(context.Background(), fantasy.ToolCall{
		Name:  getCurrentTimeTool.Info().Name,
		Input: `{"timezone": "Europe/London"}`,
	})

	if err != nil {
		return err
	}

	fmt.Println("\nget_current_time response:")
	fmt.Printf("- %s\n", resp.Content)
	return nil
}
