package tools

import (
	"context"
	"testing"
)

func TestSearchToolRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		tool  *SearchTool
		input string
	}{
		{name: "missing API key", tool: &SearchTool{}, input: `{"query":"Go"}`},
		{name: "invalid JSON", tool: &SearchTool{APIKey: "test"}, input: `{`},
		{name: "empty query", tool: &SearchTool{APIKey: "test"}, input: `{"query":"  "}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.tool.Execute(context.Background(), []byte(tt.input)); err == nil {
				t.Fatal("Execute() expected error")
			}
		})
	}
}
