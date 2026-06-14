package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	openai "github.com/sashabaranov/go-openai"
)

// deepseekTransport 注入 thinking: {type: "disabled"}。
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

// ========== 工具定义（来自第4篇） ==========

var tools = []openai.Tool{
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_weather",
			Description: "获取指定城市当前的天气信息，包括温度和天气状况",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "城市名称，如北京、上海、New York",
					},
				},
				"required": []string{"city"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "calculate",
			Description: "计算数学表达式，支持加减乘除和括号，如 '(1 + 2) * 3'",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "数学表达式，如 '(1 + 2) * 3'",
					},
				},
				"required": []string{"expression"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "search",
			Description: "搜索本地知识库中的信息。适用于查找编程概念、技术文档等内容",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{
						"type":        "string",
						"description": "搜索关键词",
					},
				},
				"required": []string{"keyword"},
			},
		},
	},
}

var weatherData = map[string]string{
	"北京":       "15°C，晴，西北风3级",
	"上海":       "18°C，多云，东南风2级",
	"东京":       "22°C，小雨，南风1级",
	"New York": "12°C，晴，北风4级",
	"London":   "8°C，阴，西风3级",
}

var searchResults = map[string]string{
	"go for loop":    "Go 只有 for 一种循环关键字，支持三种形式。Go 1.22 起 for 循环变量每次迭代创建新变量。",
	"goroutine":      "goroutine 是 Go 的轻量级用户态执行单元，被 Go 调度器复用到 OS 线程上。",
	"rust ownership": "Rust 的所有权系统在编译期保证内存安全，无需 GC。",
}

var toolHandlers = map[string]func(json.RawMessage) (string, error){
	"get_weather": handleGetWeather,
	"calculate":   handleCalculate,
	"search":      handleSearch,
}

func handleGetWeather(input json.RawMessage) (string, error) {
	var args struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	if w, ok := weatherData[args.City]; ok {
		return w, nil
	}
	return fmt.Sprintf("%s 的天气数据暂时不可用", args.City), nil
}

func handleCalculate(input json.RawMessage) (string, error) {
	var args struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	result, err := evalExpression(args.Expression)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s = %v", args.Expression, result), nil
}

func handleSearch(input json.RawMessage) (string, error) {
	var args struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	kw := strings.ToLower(strings.TrimSpace(args.Keyword))
	for key, content := range searchResults {
		if strings.Contains(key, kw) || strings.Contains(kw, key) {
			return content, nil
		}
	}
	return fmt.Sprintf("未找到关于 '%s' 的相关结果", args.Keyword), nil
}

func evalExpression(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	if expr == "" {
		return 0, fmt.Errorf("空表达式")
	}
	return evalAddSub(expr)
}

func evalAddSub(expr string) (float64, error) {
	parenDepth := 0
	for i := len(expr) - 1; i >= 0; i-- {
		switch expr[i] {
		case ')':
			parenDepth++
		case '(':
			parenDepth--
		case '+':
			if parenDepth == 0 {
				left, _ := evalAddSub(expr[:i])
				right, _ := evalMulDiv(expr[i+1:])
				return left + right, nil
			}
		case '-':
			if parenDepth == 0 {
				if i == 0 {
					right, _ := evalMulDiv(expr[1:])
					return -right, nil
				}
				left, _ := evalAddSub(expr[:i])
				right, _ := evalMulDiv(expr[i+1:])
				return left - right, nil
			}
		}
	}
	return evalMulDiv(expr)
}

func evalMulDiv(expr string) (float64, error) {
	parenDepth := 0
	for i := len(expr) - 1; i >= 0; i-- {
		switch expr[i] {
		case ')':
			parenDepth++
		case '(':
			parenDepth--
		case '*':
			if parenDepth == 0 {
				left, _ := evalMulDiv(expr[:i])
				right, _ := evalPrimary(expr[i+1:])
				return left * right, nil
			}
		case '/':
			if parenDepth == 0 {
				left, _ := evalMulDiv(expr[:i])
				right, _ := evalPrimary(expr[i+1:])
				return left / right, nil
			}
		}
	}
	return evalPrimary(expr)
}

func evalPrimary(expr string) (float64, error) {
	if expr[0] == '(' {
		depth := 1
		for i := 1; i < len(expr); i++ {
			switch expr[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return evalAddSub(expr[1:i])
				}
			}
		}
	}
	if expr[0] == '-' {
		val, _ := evalPrimary(expr[1:])
		return -val, nil
	}
	var result float64
	fmt.Sscanf(expr, "%f", &result)
	return result, nil
}

// ========== 第5篇新增：Session + Token 管理 ==========

// Session 管理对话历史和 token 预算
type Session struct {
	Messages  []openai.ChatCompletionMessage
	MaxTokens int
	client    *openai.Client
}

func NewSession(client *openai.Client) *Session {
	return &Session{
		MaxTokens: 110_000, // 示例中故意设低阈值（远小于 1M 实际窗口），方便触发压缩演示
		client:    client,
	}
}

// countTokens 估算对话历史的 token 数。
// tiktoken 运行时下载 tokenizer 资源，大陆/离线可能失败。
// 失败时回退到字符粗估（1 token ≈ 2 字符）。
func countTokens(messages []openai.ChatCompletionMessage) (int, error) {
	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		// 回退：保守估计 1 token ≈ 2 字符
		total := 0
		for _, msg := range messages {
			total += len(msg.Content)/2 + 4
		}
		return total, nil
	}
	total := 0
	for _, msg := range messages {
		tokens := tke.Encode(msg.Content, nil, nil)
		total += len(tokens)
		total += 4
	}
	return total, nil
}

// compressByTruncation 按完整轮次截断旧消息。
// user 消息是每轮对话的天然分界点，从第二条 user 处切开，
// 保证 tool 消息永远不会脱离它的 tool_calls。
func compressByTruncation(messages []openai.ChatCompletionMessage, targetTokens int) []openai.ChatCompletionMessage {
	for {
		count, _ := countTokens(messages)
		if count <= targetTokens || len(messages) == 0 {
			return messages
		}
		// 找到第二条 user 消息的起始位置
		cutIdx := 1
		for cutIdx < len(messages) && messages[cutIdx].Role != openai.ChatMessageRoleUser {
			cutIdx++
		}
		if cutIdx >= len(messages) {
			break // 只剩最后一轮了，不能再删
		}
		messages = messages[cutIdx:]
	}
	return messages
}

// compressBySummarization 把旧对话摘要化。
// 切分点必须对齐 user 消息边界，避免把 assistant tool_calls 和 tool 消息切到两边。
func compressBySummarization(client *openai.Client, ctx context.Context, messages []openai.ChatCompletionMessage, targetTokens int) []openai.ChatCompletionMessage {
	// 找到第一个 user 消息边界作为切分点
	splitIdx := len(messages) / 2
	for splitIdx < len(messages) && messages[splitIdx].Role != openai.ChatMessageRoleUser {
		splitIdx++
	}
	if splitIdx >= len(messages) {
		return compressByTruncation(messages, targetTokens)
	}

	var oldConversation strings.Builder
	for _, m := range messages[:splitIdx] {
		fmt.Fprintf(&oldConversation, "[%s] %s\n", m.Role, m.Content)
	}

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: 512,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "用一两句话概括以下对话的核心信息：\n\n" + oldConversation.String()},
		},
	})
	if err != nil {
		return compressByTruncation(messages, targetTokens)
	}

	summaryText := resp.Choices[0].Message.Content
	return append([]openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleAssistant, Content: "【对话摘要】" + summaryText},
	}, messages[splitIdx:]...)
}

// compressMessages 混合压缩策略
func compressMessages(client *openai.Client, ctx context.Context, messages []openai.ChatCompletionMessage, targetTokens int) []openai.ChatCompletionMessage {
	result := compressByTruncation(messages, targetTokens)
	if len(result) < len(messages)/2 {
		result = compressBySummarization(client, ctx, messages, targetTokens)
	}
	// 摘要后仍可能超预算，最后再截一次兜底
	if count, _ := countTokens(result); count > targetTokens {
		result = compressByTruncation(result, targetTokens)
	}
	return result
}

// Chat 执行一轮对话
func (s *Session) Chat(ctx context.Context, question string) string {
	s.Messages = append(s.Messages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser, Content: question,
	})

	// 检查 token 预算
	count, err := countTokens(s.Messages)
	if err == nil && count >= s.MaxTokens {
		s.Messages = compressMessages(s.client, ctx, s.Messages, s.MaxTokens)
	}

	for i := 0; i < 5; i++ {
		resp, err := s.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     "deepseek-v4-flash",
			MaxTokens: 4096,
			Messages:  s.Messages,
			Tools:     tools,
		})
		if err != nil {
			return "API 调用失败: " + err.Error()
		}

		choice := resp.Choices[0]

		if choice.FinishReason == openai.FinishReasonToolCalls {
			s.Messages = append(s.Messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})
			for _, tc := range choice.Message.ToolCalls {
				handler, ok := toolHandlers[tc.Function.Name]
				var result string
				if ok {
					result, err = handler(json.RawMessage(tc.Function.Arguments))
					if err != nil {
						result = "工具执行出错: " + err.Error()
					}
				} else {
					result = fmt.Sprintf("未知工具: %s", tc.Function.Name)
				}
				s.Messages = append(s.Messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			continue
		}

		s.Messages = append(s.Messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: choice.Message.Content,
		})
		return choice.Message.Content
	}

	return "抱歉，我暂时无法完成这个请求"
}

// ========== main ==========

func main() {
	loadEnv(".env")

	config := openai.DefaultConfig(os.Getenv("DEEPSEEK_API_KEY"))
	config.BaseURL = "https://api.deepseek.com"
	// 注入 thinking 禁用
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{base: httpClient.Transport}
	}
	client := openai.NewClientWithConfig(config)
	ctx := context.Background()

	session := NewSession(client)

	// 第一轮
	resp1 := session.Chat(ctx, "北京天气怎么样？")
	fmt.Println("Q1: 北京天气怎么样？")
	fmt.Printf("A1: %s\n\n", resp1)

	// 第二轮——模型能看到第一轮的完整上下文
	resp2 := session.Chat(ctx, "那上海呢？")
	fmt.Println("Q2: 那上海呢？")
	fmt.Printf("A2: %s\n\n", resp2)
}
