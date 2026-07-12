package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

type loopProvider struct {
	calls int
}

func (p *loopProvider) Chat(_ context.Context, req *ChatRequest) (*ChatResponse, error) {
	p.calls++
	if p.calls == 1 {
		return &ChatResponse{
			Content: []ContentBlock{
				NewToolUseBlock("call_1", "echo", json.RawMessage(`{"text":"hello"}`)),
			},
			StopReason: "tool_use",
		}, nil
	}

	if !containsToolResult(req.Messages, "call_1", "hello") {
		return nil, fmt.Errorf("第二次模型请求缺少工具结果")
	}
	return &ChatResponse{
		Content:    []ContentBlock{NewTextBlock("完成")},
		StopReason: "end_turn",
	}, nil
}

func (p *loopProvider) CountTokens(_ context.Context, _ []Message) (int, error) {
	return 10, nil
}

type echoTool struct {
	BaseTool
}

func (t *echoTool) Definition() ToolDefinition {
	return ToolDefinition{Name: "echo"}
}

func (t *echoTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("解析 echo 参数: %w", err)
	}
	return args.Text, nil
}

func TestAgentRunExecutesToolAndReturnsFinalAnswer(t *testing.T) {
	provider := &loopProvider{}
	registry := NewToolRegistry()
	if err := registry.Register(&echoTool{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	runtime := NewAgent(provider,
		WithToolRegistry(registry),
		WithMaxIterations(3),
	)
	resp, err := runtime.Run(context.Background(), Request{Message: "say hello"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if resp.Text != "完成" {
		t.Fatalf("Run().Text = %q, want %q", resp.Text, "完成")
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
}

func containsToolResult(messages []Message, id, text string) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type() == "tool_result" && block.ID() == id && block.Text() == text {
				return true
			}
		}
	}
	return false
}
