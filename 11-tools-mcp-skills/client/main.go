package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/arixbit/agent-series/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	skillsRoot   = "skills"
	systemPrompt = `你是一个天气助手。
可用 Skill 不会自动加载。只有当用户明确要求使用某个 Skill 时，才调用 load_skill 读取它的完整说明，然后按照说明完成任务。
不要声称使用了尚未加载的 Skill。`
)

type toolSession interface {
	ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("用法: %s <MCP Server URL>", os.Args[0])
	}
	if err := agent.LoadEnv(".env"); err != nil {
		log.Fatalf("加载 .env: %v", err)
	}
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		log.Fatal("请先在 .env 中配置 DEEPSEEK_API_KEY")
	}

	if err := run(context.Background(), os.Args[1], skillsRoot, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, serverURL, skillDir string, in io.Reader, out io.Writer) (runErr error) {
	skillLoader, err := newSkillLoader(skillDir, out)
	if err != nil {
		return err
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "weather-agent-client",
		Version: "1.0.0",
	}, nil)
	if err := writeProgress(out, "[MCP] 连接 Server：%s\n", serverURL); err != nil {
		return err
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: serverURL,
	}, nil)
	if err != nil {
		return fmt.Errorf("连接 MCP Server %q: %w", serverURL, err)
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("关闭 MCP 会话: %w", closeErr)
		}
	}()

	tools, err := discoverTools(ctx, session, out)
	if err != nil {
		return err
	}
	registry := agent.NewToolRegistry()
	if err := registry.Register(skillLoader); err != nil {
		return fmt.Errorf("注册 Skill 加载工具: %w", err)
	}
	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			return fmt.Errorf("注册 MCP 工具: %w", err)
		}
	}

	weatherAgent := agent.NewAgent(
		agent.NewDeepSeekProvider(os.Getenv("DEEPSEEK_API_KEY")),
		agent.WithToolRegistry(registry),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithTracer(newConsoleTracer(out)),
		agent.WithMaxIterations(5),
	)
	return runConversation(ctx, weatherAgent, in, out)
}

func runConversation(ctx context.Context, weatherAgent agent.Agent, in io.Reader, out io.Writer) error {
	if err := writeProgress(out, "\n天气 Agent 已就绪（输入 quit 退出）\n"); err != nil {
		return err
	}

	scanner := bufio.NewScanner(in)
	var history []agent.Message
	for {
		if err := writeProgress(out, "\n> "); err != nil {
			return err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("读取用户输入: %w", err)
			}
			return nil
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "quit" {
			return nil
		}
		if input == "" {
			continue
		}

		response, err := weatherAgent.Run(ctx, agent.Request{
			Message: input,
			History: history,
		})
		if err != nil {
			if writeErr := writeProgress(out, "[Agent] 运行失败：%v\n", err); writeErr != nil {
				return writeErr
			}
			continue
		}
		history = response.History
		if err := writeProgress(out, "\n[Assistant] %s\n", response.Text); err != nil {
			return err
		}
	}
}

func discoverTools(ctx context.Context, session toolSession, out io.Writer) ([]agent.Tool, error) {
	if err := writeProgress(out, "[MCP] -> ListTools：询问 Server 提供哪些工具\n"); err != nil {
		return nil, err
	}
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("发现 MCP 工具: %w", err)
	}

	tools := make([]agent.Tool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("MCP 工具 %q 的输入结构不是 JSON 对象", tool.Name)
		}
		if err := writeProgress(out, "[MCP] <- 发现工具：%s - %s\n", tool.Name, tool.Description); err != nil {
			return nil, err
		}
		tools = append(tools, &mcpTool{
			definition: agent.ToolDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: schema,
			},
			session: session,
			out:     out,
		})
	}
	return tools, nil
}

type mcpTool struct {
	agent.BaseTool
	definition agent.ToolDefinition
	session    toolSession
	out        io.Writer
}

func (t *mcpTool) Definition() agent.ToolDefinition {
	return t.definition
}

func (t *mcpTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var arguments map[string]any
	if err := json.Unmarshal(input, &arguments); err != nil {
		return "", fmt.Errorf("解析 MCP 工具 %q 的参数: %w", t.definition.Name, err)
	}
	if err := writeProgress(t.out, "[MCP] -> CallTool：%s，参数：%s\n", t.definition.Name, input); err != nil {
		return "", err
	}
	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      t.definition.Name,
		Arguments: arguments,
	})
	if err != nil {
		return "", fmt.Errorf("调用 MCP 工具 %q: %w", t.definition.Name, err)
	}

	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	output := strings.Join(parts, "\n")
	if result.IsError {
		return "", fmt.Errorf("MCP 工具 %q 返回错误: %s", t.definition.Name, output)
	}
	if err := writeProgress(t.out, "[MCP] <- CallTool：%s\n", output); err != nil {
		return "", err
	}
	return output, nil
}

type consoleTracer struct {
	mu sync.Mutex
	w  io.Writer
}

func newConsoleTracer(w io.Writer) *consoleTracer {
	return &consoleTracer{w: w}
}

func (t *consoleTracer) Record(_ context.Context, event agent.TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch event.Type {
	case "model_request":
		return writeProgress(t.w, "[Agent] 第 %d 轮：请求模型（%d 个可用工具）\n",
			event.Iteration, metadataInt(event.Metadata, "tool_count"))
	case "model_response":
		return writeProgress(t.w, "[Agent] 第 %d 轮：模型返回 %s\n", event.Iteration, event.StopReason)
	case "tool_call":
		input, _ := event.Metadata["input"].(string)
		return writeProgress(t.w, "[Agent] 选择工具：%s，参数：%s\n", event.ToolName, input)
	default:
		return nil
	}
}

func metadataInt(metadata map[string]any, key string) int {
	value, _ := metadata[key].(int)
	return value
}

func writeProgress(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("打印运行过程: %w", err)
	}
	return nil
}
