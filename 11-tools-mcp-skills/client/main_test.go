package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arixbit/agent-series/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiscoverToolsAdaptsMCPDefinition(t *testing.T) {
	session := &fakeToolSession{
		tools: &mcp.ListToolsResult{
			Tools: []*mcp.Tool{{
				Name:        "get_weather",
				Description: "查询城市的 mock 天气",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			}},
		},
	}
	var output bytes.Buffer

	tools, err := discoverTools(context.Background(), session, &output)
	if err != nil {
		t.Fatalf("discoverTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(tools))
	}
	definition := tools[0].Definition()
	if definition.Name != "get_weather" || definition.Description != "查询城市的 mock 天气" {
		t.Fatalf("definition = %#v", definition)
	}
	if !strings.Contains(output.String(), "发现工具：get_weather") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestMCPToolExecuteCallsSession(t *testing.T) {
	session := &fakeToolSession{result: &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "上海：多云，19°C"}},
	}}
	var output bytes.Buffer
	tool := &mcpTool{
		definition: weatherDefinition(),
		session:    session,
		out:        &output,
	}

	got, err := tool.Execute(context.Background(), json.RawMessage(`{"city":"上海"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "上海：多云，19°C" {
		t.Fatalf("Execute() = %q", got)
	}
	if session.callParams == nil || session.callParams.Name != "get_weather" {
		t.Fatalf("CallTool() params = %#v", session.callParams)
	}
	arguments, ok := session.callParams.Arguments.(map[string]any)
	if !ok || arguments["city"] != "上海" {
		t.Fatalf("CallTool() arguments = %#v", session.callParams.Arguments)
	}
	if !strings.Contains(output.String(), "[MCP] -> CallTool：get_weather") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestMCPToolExecuteReturnsToolError(t *testing.T) {
	tool := &mcpTool{
		definition: weatherDefinition(),
		session: &fakeToolSession{result: &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "城市不存在"}},
			IsError: true,
		}},
		out: &bytes.Buffer{},
	}

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"city":"火星"}`))
	if err == nil || !strings.Contains(err.Error(), "城市不存在") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestSkillLoaderDiscoversAndLoadsSkill(t *testing.T) {
	root := t.TempDir()
	createTestSkill(t, root, "weather-report", "回答天气问题", "输出验证句。")
	var output bytes.Buffer

	loader, err := newSkillLoader(root, &output)
	if err != nil {
		t.Fatalf("newSkillLoader() error = %v", err)
	}
	definition := loader.Definition()
	if definition.Name != "load_skill" || !strings.Contains(definition.Description, "weather-report: 回答天气问题") {
		t.Fatalf("definition = %#v", definition)
	}
	if strings.Contains(output.String(), "已加载") {
		t.Fatalf("skill was loaded during discovery:\n%s", output.String())
	}

	content, err := loader.Execute(context.Background(), json.RawMessage(`{"name":"weather-report"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(content, "输出验证句。") {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(output.String(), "[Skill] 已加载：weather-report") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestSkillLoaderRejectsUnknownSkill(t *testing.T) {
	root := t.TempDir()
	createTestSkill(t, root, "weather-report", "回答天气问题", "输出验证句。")
	loader, err := newSkillLoader(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newSkillLoader() error = %v", err)
	}

	_, err = loader.Execute(context.Background(), json.RawMessage(`{"name":"missing"}`))
	if err == nil || !strings.Contains(err.Error(), "未知 Skill") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestAgentLoadsSkillThenCallsMCPTool(t *testing.T) {
	root := t.TempDir()
	createTestSkill(t, root, "weather-report", "回答天气问题", "查什么天气查天气，你又不出门游玩。")
	loader, err := newSkillLoader(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newSkillLoader() error = %v", err)
	}
	session := &fakeToolSession{
		tools: &mcp.ListToolsResult{Tools: []*mcp.Tool{{
			Name:        "get_weather",
			Description: "查询城市的 mock 天气",
			InputSchema: map[string]any{"type": "object"},
		}}},
		result: &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "上海：多云，19°C"}},
		},
	}
	tools, err := discoverTools(context.Background(), session, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("discoverTools() error = %v", err)
	}
	provider := &scriptedProvider{}
	allTools := append([]agent.Tool{loader}, tools...)
	weatherAgent := agent.NewAgent(provider,
		agent.WithTools(allTools...),
		agent.WithSystemPrompt(systemPrompt),
	)

	response, err := weatherAgent.Run(context.Background(), agent.Request{Message: "请使用 weather-report Skill 查询上海天气。"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Text != "上海：多云，19°C。查什么天气查天气，你又不出门游玩。" {
		t.Fatalf("response = %q", response.Text)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(provider.requests))
	}
	if provider.requests[0].System != systemPrompt {
		t.Fatalf("system prompt = %q", provider.requests[0].System)
	}
	if len(provider.requests[0].Tools) != 2 || provider.requests[0].Tools[0].Definition().Name != "load_skill" {
		t.Fatalf("tools = %#v", provider.requests[0].Tools)
	}
	if !chatRequestContains(provider.requests[1], "查什么天气查天气") {
		t.Fatalf("second request does not contain loaded Skill")
	}
	if session.callParams == nil || session.callParams.Name != "get_weather" {
		t.Fatalf("CallTool() params = %#v", session.callParams)
	}
}

func TestRunConversationKeepsHistoryUntilQuit(t *testing.T) {
	chatAgent := &fakeConversationAgent{}
	input := strings.NewReader("第一问\n第二问\nquit\n")
	var output bytes.Buffer

	if err := runConversation(context.Background(), chatAgent, input, &output); err != nil {
		t.Fatalf("runConversation() error = %v", err)
	}
	if len(chatAgent.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(chatAgent.requests))
	}
	if len(chatAgent.requests[0].History) != 0 {
		t.Fatalf("first history count = %d, want 0", len(chatAgent.requests[0].History))
	}
	if len(chatAgent.requests[1].History) != 2 {
		t.Fatalf("second history count = %d, want 2", len(chatAgent.requests[1].History))
	}
	for _, want := range []string{
		"天气 Agent 已就绪（输入 quit 退出）",
		"[Assistant] 第1次回答",
		"[Assistant] 第2次回答",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output does not contain %q:\n%s", want, output.String())
		}
	}
}

func weatherDefinition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "get_weather",
		Description: "查询城市的 mock 天气",
		InputSchema: map[string]any{"type": "object"},
	}
}

type fakeToolSession struct {
	tools      *mcp.ListToolsResult
	result     *mcp.CallToolResult
	listErr    error
	callErr    error
	callParams *mcp.CallToolParams
}

func (s *fakeToolSession) ListTools(_ context.Context, _ *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	return s.tools, s.listErr
}

func (s *fakeToolSession) CallTool(_ context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	s.callParams = params
	return s.result, s.callErr
}

type scriptedProvider struct {
	requests []*agent.ChatRequest
}

func (p *scriptedProvider) Chat(_ context.Context, req *agent.ChatRequest) (*agent.ChatResponse, error) {
	p.requests = append(p.requests, req)
	switch len(p.requests) {
	case 1:
		return &agent.ChatResponse{
			Content: []agent.ContentBlock{agent.NewToolUseBlock(
				"call-skill",
				"load_skill",
				json.RawMessage(`{"name":"weather-report"}`),
			)},
			StopReason: "tool_use",
		}, nil
	case 2:
		return &agent.ChatResponse{
			Content: []agent.ContentBlock{agent.NewToolUseBlock(
				"call-1",
				"get_weather",
				json.RawMessage(`{"city":"上海"}`),
			)},
			StopReason: "tool_use",
		}, nil
	default:
		return &agent.ChatResponse{
			Content:    []agent.ContentBlock{agent.NewTextBlock("上海：多云，19°C。查什么天气查天气，你又不出门游玩。")},
			StopReason: "end_turn",
		}, nil
	}
}

func (p *scriptedProvider) CountTokens(_ context.Context, _ []agent.Message) (int, error) {
	return 0, nil
}

type fakeConversationAgent struct {
	requests []agent.Request
}

func (a *fakeConversationAgent) Run(_ context.Context, req agent.Request) (*agent.Response, error) {
	a.requests = append(a.requests, req)
	round := len(a.requests)
	history := append([]agent.Message{}, req.History...)
	history = append(history,
		agent.Message{Role: "user", Content: []agent.ContentBlock{agent.NewTextBlock(req.Message)}},
		agent.Message{Role: "assistant", Content: []agent.ContentBlock{agent.NewTextBlock(fmt.Sprintf("第%d次回答", round))}},
	)
	return &agent.Response{
		Text:    fmt.Sprintf("第%d次回答", round),
		History: history,
	}, nil
}

func createTestSkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func chatRequestContains(req *agent.ChatRequest, want string) bool {
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type() == "tool_result" && strings.Contains(block.Text(), want) {
				return true
			}
		}
	}
	return false
}
