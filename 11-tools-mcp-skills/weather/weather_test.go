package weather

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGet(t *testing.T) {
	result, _, err := Get(context.Background(), nil, Input{City: " 北京 "})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want *mcp.TextContent", result.Content[0])
	}
	if text.Text != "北京：晴，15°C" {
		t.Fatalf("text = %q, want %q", text.Text, "北京：晴，15°C")
	}
}

func TestGetRejectsEmptyCity(t *testing.T) {
	if _, _, err := Get(context.Background(), nil, Input{}); err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
}

func TestListSupportedCities(t *testing.T) {
	result, _, err := ListSupportedCities(context.Background(), nil, ListInput{})
	if err != nil {
		t.Fatalf("ListSupportedCities() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want *mcp.TextContent", result.Content[0])
	}
	if text.Text != "支持的 mock 城市：北京、上海" {
		t.Fatalf("text = %q, want %q", text.Text, "支持的 mock 城市：北京、上海")
	}
}
