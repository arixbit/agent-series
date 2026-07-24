package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStreamableHTTPServerListsAndCallsTools(t *testing.T) {
	server := newWeatherServer()
	mux := http.NewServeMux()
	mux.Handle(mcpPath, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil))
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "weather-server-test",
		Version: "1.0.0",
	}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + mcpPath,
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) != 2 {
		t.Fatalf("tool count = %d, want 2", len(tools.Tools))
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_weather",
		Arguments: map[string]any{"city": "上海"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "上海：多云，19°C" {
		t.Fatalf("content = %#v", result.Content[0])
	}
}
