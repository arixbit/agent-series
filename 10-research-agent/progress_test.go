package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arixbit/agent-series/agent"
)

func TestConsoleTracerRecord(t *testing.T) {
	tests := []struct {
		name  string
		event agent.TraceEvent
		want  string
	}{
		{
			name: "model request",
			event: agent.TraceEvent{
				Type:      "model_request",
				Iteration: 1,
				Metadata: map[string]any{
					"message_count": 2,
					"tool_count":    3,
				},
			},
			want: "[Agent] 第 1 轮：请求模型（2 条消息，3 个可用工具）\n",
		},
		{
			name: "model requests tool",
			event: agent.TraceEvent{
				Type:         "model_response",
				Iteration:    1,
				StopReason:   "tool_use",
				DurationMS:   620,
				InputTokens:  300,
				OutputTokens: 20,
			},
			want: "[Agent] 第 1 轮：模型决定调用工具（耗时 620ms，输入 300 token，输出 20 token）\n",
		},
		{
			name: "tool call",
			event: agent.TraceEvent{
				Type:      "tool_call",
				ToolName:  "search",
				Iteration: 1,
				Metadata:  map[string]any{"input": "{\n  \"query\": \"Go 1.22 loop variable\"\n}"},
			},
			want: "[Tool] 调用 search，参数：{ \"query\": \"Go 1.22 loop variable\" }\n",
		},
		{
			name: "tool result",
			event: agent.TraceEvent{
				Type:       "tool_result",
				ToolName:   "search",
				DurationMS: 415,
				Metadata:   map[string]any{"result_len": 1380},
			},
			want: "[Tool] search 完成（耗时 415ms，返回 1380 字节）\n",
		},
		{
			name: "tool error",
			event: agent.TraceEvent{
				Type:       "tool_result",
				ToolName:   "read_webpage",
				DurationMS: 90,
				Error:      "HTTP 403",
			},
			want: "[Tool] read_webpage 失败（耗时 90ms）：HTTP 403\n",
		},
		{
			name:  "ignored event",
			event: agent.TraceEvent{Type: "run_start"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			tracer := newConsoleTracer(&output)
			if err := tracer.Record(context.Background(), tt.event); err != nil {
				t.Fatalf("Record() error = %v", err)
			}
			if got := output.String(); got != tt.want {
				t.Fatalf("Record() output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSingleLineTruncatesByRune(t *testing.T) {
	input := strings.Repeat("中", maxToolInputRunes+1)
	got := singleLine(input)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("singleLine() = %q, want ellipsis suffix", got)
	}
	if gotRunes := len([]rune(strings.TrimSuffix(got, "…"))); gotRunes != maxToolInputRunes {
		t.Fatalf("singleLine() kept %d runes, want %d", gotRunes, maxToolInputRunes)
	}
}

func TestConsoleTracerShowsAgentLoop(t *testing.T) {
	var output bytes.Buffer
	provider := &progressTestProvider{}
	researchAgent := agent.NewAgent(provider,
		agent.WithTools(&progressTestTool{}),
		agent.WithTracer(newConsoleTracer(&output)),
		agent.WithMaxIterations(2),
	)

	resp, err := researchAgent.Run(context.Background(), agent.Request{Message: "研究 Go 1.22"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Text != "研究完成" {
		t.Fatalf("Run() text = %q, want %q", resp.Text, "研究完成")
	}

	wantInOrder := []string{
		"[Agent] 第 1 轮：请求模型",
		"[Agent] 第 1 轮：模型决定调用工具",
		`[Tool] 调用 search，参数：{"query":"Go 1.22"}`,
		"[Tool] search 完成",
		"[Agent] 第 2 轮：请求模型",
		"[Agent] 第 2 轮：模型返回最终回答",
	}
	remaining := output.String()
	for _, want := range wantInOrder {
		index := strings.Index(remaining, want)
		if index < 0 {
			t.Fatalf("output does not contain %q in order:\n%s", want, output.String())
		}
		remaining = remaining[index+len(want):]
	}

	output.Reset()
	secondResp, err := researchAgent.Run(context.Background(), agent.Request{
		Message: "它为什么能避免闭包错误？",
		History: resp.History,
	})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if secondResp.Text != "研究完成" {
		t.Fatalf("second Run() text = %q, want %q", secondResp.Text, "研究完成")
	}
	if got := output.String(); !strings.Contains(got, "[Agent] 第 1 轮：请求模型（5 条消息，1 个可用工具）") {
		t.Fatalf("second Run() did not include prior history:\n%s", got)
	}
}

type progressTestProvider struct {
	calls int
}

func (p *progressTestProvider) Chat(_ context.Context, _ *agent.ChatRequest) (*agent.ChatResponse, error) {
	p.calls++
	if p.calls == 1 {
		return &agent.ChatResponse{
			Content: []agent.ContentBlock{
				agent.NewToolUseBlock("call_1", "search", json.RawMessage(`{"query":"Go 1.22"}`)),
			},
			StopReason:   "tool_use",
			InputTokens:  20,
			OutputTokens: 5,
		}, nil
	}
	return &agent.ChatResponse{
		Content:      []agent.ContentBlock{agent.NewTextBlock("研究完成")},
		StopReason:   "end_turn",
		InputTokens:  30,
		OutputTokens: 8,
	}, nil
}

func (p *progressTestProvider) CountTokens(_ context.Context, _ []agent.Message) (int, error) {
	return 0, nil
}

type progressTestTool struct {
	agent.BaseTool
}

func (t *progressTestTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{Name: "search", Description: "测试搜索工具"}
}

func (t *progressTestTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "搜索结果", nil
}
