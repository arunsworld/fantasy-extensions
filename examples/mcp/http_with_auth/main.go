package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"charm.land/fantasy"
	fantasyextensions "github.com/arunsworld/fantasy-extensions"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

type contextKey string

func run() error {
	roundTripper := &roundTripperWithAuth{transport: &http.Transport{}}
	httpClient := &http.Client{Transport: roundTripper}
	defer httpClient.CloseIdleConnections()

	mcpEndpoint := "https://mcp.context7.com/mcp"
	transport := &mcp.StreamableClientTransport{Endpoint: mcpEndpoint, HTTPClient: httpClient}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "fantasy-mcp-client",
		Version: "1.0.0",
	}, nil)

	sessionMaker := func(ctx context.Context) (*mcp.ClientSession, error) {
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return nil, err
		}
		return session, nil
	}

	ctx := context.WithValue(context.Background(), contextKey("authToken"), "testToken-01")
	tools, err := fantasyextensions.MCPTools(ctx, sessionMaker)
	if err != nil {
		return err
	}

	var resolveLibraryIDTool fantasy.AgentTool
	fmt.Println("tools:")
	for _, tool := range tools {
		fmt.Printf("- %s\n", tool.Info().Name)
		if tool.Info().Name == "resolve-library-id" {
			resolveLibraryIDTool = tool
		}
	}

	if resolveLibraryIDTool == nil {
		return fmt.Errorf("resolve-library-id tool not found")
	}

	resp, err := resolveLibraryIDTool.Run(context.Background(), fantasy.ToolCall{
		Name:  resolveLibraryIDTool.Info().Name,
		Input: `{"libraryName": "next.js"}`,
	})

	if err != nil {
		return err
	}

	fmt.Println("\nresolve-library-id response:")
	fmt.Printf("- %s\n", resp.Content)
	return nil
}

type roundTripperWithAuth struct {
	transport http.RoundTripper
}

func (r *roundTripperWithAuth) RoundTrip(req *http.Request) (*http.Response, error) {
	authToken := req.Context().Value(contextKey("authToken"))
	if authToken != nil {
		log.Printf("setting authorization header: %s. request URL: %s", authToken.(string), req.URL.String())
		req.Header.Set("Authorization", "Bearer "+authToken.(string))
	} else {
		log.Printf("no authorization header found. request URL: %s", req.URL.String())
	}
	return r.transport.RoundTrip(req)
}
