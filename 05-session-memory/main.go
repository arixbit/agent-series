package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// deepseekTransport 注入 thinking: {type: "disabled"}。
// DeepSeek 的 thinking mode 有时会让模型在回复前输出内部推理，
// 对演示来说多花钱且输出不可控，所以禁用。
type deepseekTransport struct {
	base http.RoundTripper
}

func (t *deepseekTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "deepseek") && strings.Contains(req.URL.Path, "chat/completions") {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()

		var bodyMap map[string]any
		if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
			return nil, fmt.Errorf("请求体解析失败: %w", err)
		}
		if _, exists := bodyMap["thinking"]; !exists {
			bodyMap["thinking"] = map[string]string{"type": "disabled"}
		}
		modifiedBytes, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(modifiedBytes))
		req.ContentLength = int64(len(modifiedBytes))
	}
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// newClient 创建配置好的 DeepSeek 客户端。
func newClient() *openai.Client {
	config := openai.DefaultConfig(os.Getenv("DEEPSEEK_API_KEY"))
	config.BaseURL = "https://api.deepseek.com"
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{base: httpClient.Transport}
	}
	return openai.NewClientWithConfig(config)
}

// ========== 入口 ==========

func main() {
	loadEnv(".env")
	ctx := context.Background()

	// 解析运行模式：go run . [basic|compress|boundary|interactive]
	mode := "interactive"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "basic":
		runBasic(ctx)
	case "compress":
		runCompress(ctx)
	case "boundary":
		runBoundary(ctx)
	default:
		runInteractive(ctx)
	}
}

// ========== Demo 1: 正常流程 ==========

// 两轮对话，MaxTokens 默认 110_000，正常对话不会触发压缩。
// 演示：Session 让模型记住跨轮次的上下文。
func runBasic(ctx context.Context) {
	client := newClient()
	session := NewSession(client)

	fmt.Println("=== Demo 1: 正常流程（无压缩） ===")
	fmt.Println()

	// 第一轮
	resp1 := session.Chat(ctx, "北京天气怎么样？")
	fmt.Println("Q1: 北京天气怎么样？")
	fmt.Printf("A1: %s\n", resp1)
	tokens1, _ := countTokens(session.Messages)
	fmt.Printf("→ 当前上下文: %d 条消息，约 %d token\n\n", len(session.Messages), tokens1)

	// 第二轮
	resp2 := session.Chat(ctx, "那上海呢？")
	fmt.Println("Q2: 那上海呢？")
	fmt.Printf("A2: %s\n", resp2)
	tokens2, _ := countTokens(session.Messages)
	fmt.Printf("→ 当前上下文: %d 条消息，约 %d token\n\n", len(session.Messages), tokens2)

	fmt.Println("两轮对话完成。模型知道第二个问题在问天气，")
	fmt.Println("因为它看到了 s.Messages 数组里的完整上下文。")
}

// ========== Demo 2: 压缩触发 ==========

// MaxTokens 设为 200（极低阈值），2-3 轮后必然触发压缩。
// 演示：token 超限 → 截断旧消息，模型依靠保留的最近轮次继续回答。
func runCompress(ctx context.Context) {
	client := newClient()
	session := NewSession(client)
	session.MaxTokens = 200 // 极低阈值，确保很快触发压缩

	fmt.Println("=== Demo 2: 压缩触发（MaxTokens=200） ===")
	fmt.Println()

	rounds := []string{
		"北京天气怎么样？",
		"那上海呢？",
		"帮我算一下 100 * 200",
		"东京天气怎么样？",
	}

	for i, q := range rounds {
		fmt.Printf("--- 第 %d 轮 ---\n", i+1)
		tokensBefore, _ := countTokens(session.Messages)
		fmt.Printf("发请求前 token 数: %d\n", tokensBefore)

		resp := session.Chat(ctx, q)
		fmt.Printf("Q: %s\n", q)
		fmt.Printf("A: %s\n", resp)

		tokensAfter, _ := countTokens(session.Messages)
		fmt.Printf("发请求后 token 数: %d（%d 条消息）\n", tokensAfter, len(session.Messages))

		// 判断是否触发了压缩
		if tokensBefore >= session.MaxTokens {
			fmt.Print("⚡ 本轮触发了压缩！压缩方式: ")
			if len(session.Messages) > 0 &&
				strings.Contains(session.Messages[0].Content, "【对话摘要】") {
				fmt.Println("摘要压缩")
			} else {
				fmt.Println("截断")
			}
		}
		fmt.Println()
	}

	fmt.Println("阈值只有 200 token，约等于一轮带工具调用的对话开销。")
	fmt.Println("从第 2 或第 3 轮开始就超了，触发截断，删除最早的消息。")
	fmt.Println("模型之所以还能继续回答，是因为最近的上下文还在。")
	fmt.Println("但如果是一个旧的关键约束（比如用户说过'我是素食主义者'），它已经被截掉了。")
}

// ========== Demo 3: 轮次边界 ==========

// 跑两轮对话，可视化 messages 数组结构。
// 标注"正确切分点"和"错误切分点"，展示截断后的实际结果。
// 不谈理论，直接看数据。
func runBoundary(ctx context.Context) {
	client := newClient()
	session := NewSession(client)

	fmt.Println("=== Demo 3: 轮次边界验证 ===")
	fmt.Println()

	// 跑两轮对话，让 messages 积累完整的 tool_calls 对
	session.Chat(ctx, "北京天气怎么样？")
	session.Chat(ctx, "那上海呢？")

	// 可视化 messages 数组
	fmt.Println("两轮对话后的 messages 数组：")
	fmt.Println(strings.Repeat("─", 65))
	for i, msg := range session.Messages {
		content := msg.Content
		if len(content) > 45 {
			content = content[:45] + "..."
		}

		extra := ""
		if msg.Role == openai.ChatMessageRoleAssistant && len(msg.ToolCalls) > 0 {
			tcNames := make([]string, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				tcNames[j] = fmt.Sprintf("%s(%s)", tc.Function.Name, tc.Function.Arguments)
			}
			extra = fmt.Sprintf("  ← tool_calls: %v", tcNames)
		}
		if msg.Role == openai.ChatMessageRoleTool {
			extra = fmt.Sprintf("  ← tool_call_id: %s", msg.ToolCallID)
		}

		fmt.Printf("[%d] %-10s %s%s\n", i, msg.Role, content, extra)
	}
	fmt.Println(strings.Repeat("─", 65))
	fmt.Println()

	// 找到第二条 user 消息（安全切分点）
	cutIdx := 1
	for cutIdx < len(session.Messages) && session.Messages[cutIdx].Role != openai.ChatMessageRoleUser {
		cutIdx++
	}

	fmt.Printf("第一条 user 消息: 索引 0\n")
	fmt.Printf("第二条 user 消息: 索引 %d ← 安全切分点\n", cutIdx)
	fmt.Println()

	// 解释为什么索引 2 不行
	fmt.Println("如果切在索引 2:")
	fmt.Println("  [0] user      ← 删了")
	fmt.Println("  [1] assistant ← 删了（call_1 的定义在这里！）")
	fmt.Println("  [2] tool      ← 留着但引用了 call_1，call_1 的定义已经没了 → 400 错误")
	fmt.Println()
	fmt.Printf("如果切在索引 %d（第二条 user）:\n", cutIdx)
	fmt.Println("  [0]~[" + fmt.Sprint(cutIdx-1) + "] 整组删除（第一轮: user → tool_calls → tool → reply）")
	fmt.Println("  没有任何一条 tool 消息脱离它的 tool_calls")

	// 实际执行一次截断
	fmt.Println()
	fmt.Printf("compressByTruncation(targetTokens=100) 的实际结果:\n")
	fmt.Println(strings.Repeat("─", 65))
	truncated := compressByTruncation(session.Messages, 100)
	for i, msg := range truncated {
		content := msg.Content
		if len(content) > 50 {
			content = content[:50] + "..."
		}
		fmt.Printf("[%d] %-10s %s\n", i, msg.Role, content)
	}
	fmt.Println(strings.Repeat("─", 65))
	fmt.Println()
	fmt.Println("干净——没有孤儿 tool 消息，不会触发 400。")
	fmt.Println("这就是为什么截断必须按轮次边界切。")
}

// ========== 交互式 CLI（默认模式） ==========

func runInteractive(ctx context.Context) {
	client := newClient()
	session := NewSession(client)

	fmt.Println("Session Memory CLI")
	fmt.Println("连续输入问题，感受模型如何记住上文。")
	fmt.Println("输入 /tokens 查看上下文用量")
	fmt.Println("输入 /msgs 查看消息列表")
	fmt.Println("输入 /threshold 切换 token 阈值（200 ↔ 110000）")
	fmt.Println("输入 /exit 退出")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch input {
		case "/exit":
			fmt.Println("再见。")
			return
		case "/tokens":
			count, err := countTokens(session.Messages)
			if err != nil {
				fmt.Printf("统计失败: %v\n", err)
			} else {
				fmt.Printf("当前上下文: %d 条消息，约 %d token（上限 %d）\n",
					len(session.Messages), count, session.MaxTokens)
			}
			continue
		case "/msgs":
			fmt.Printf("共 %d 条消息:\n", len(session.Messages))
			for i, msg := range session.Messages {
				content := msg.Content
				if len(content) > 60 {
					content = content[:60] + "..."
				}
				tcInfo := ""
				if len(msg.ToolCalls) > 0 {
					tcInfo = fmt.Sprintf(" [tool_calls: %d]", len(msg.ToolCalls))
				}
				fmt.Printf("  [%d] %s%s: %s\n", i, msg.Role, tcInfo, content)
			}
			continue
		case "/threshold":
			if session.MaxTokens == 110_000 {
				session.MaxTokens = 200
				fmt.Println("阈值已设为 200 token（演示压缩用）")
			} else {
				session.MaxTokens = 110_000
				fmt.Println("阈值已恢复为 110_000 token")
			}
			continue
		}

		// "set threshold N" 命令
		if strings.HasPrefix(input, "set threshold ") {
			valStr := strings.TrimPrefix(input, "set threshold ")
			val, err := strconv.Atoi(strings.TrimSpace(valStr))
			if err != nil {
				fmt.Println("用法: set threshold <数字>")
				continue
			}
			session.MaxTokens = val
			fmt.Printf("阈值已设为 %d token\n", val)
			continue
		}

		resp := session.Chat(ctx, input)
		fmt.Printf("Agent: %s\n\n", resp)
	}
}
