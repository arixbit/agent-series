package main

import (
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
// DeepSeek V4 默认开启 Thinking Mode，工具调用场景下不传
// reasoning_content 回后续请求会 400。显式关闭 thinking 避免此问题。
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

// 三个工具定义

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
						"description": "搜索关键词，如 Go for loop 语法",
					},
				},
				"required": []string{"keyword"},
			},
		},
	},
}

// Mock 数据

var weatherData = map[string]string{
	"北京":       "15°C，晴，西北风3级",
	"上海":       "18°C，多云，东南风2级",
	"东京":       "22°C，小雨，南风1级",
	"New York": "12°C，晴，北风4级",
	"London":   "8°C，阴，西风3级",
}

var searchResults = map[string]string{
	"go for loop":    "Go 只有 for 一种循环关键字，支持三种形式：经典 for init; condition; post {}、while 风格 for condition {}、无限循环 for {}。Go 1.22 起 for 循环变量每次迭代创建新变量——修复了 Go 最经典的闭包并发陷阱。",
	"goroutine":      "goroutine 是 Go 的轻量级用户态执行单元，被 Go 调度器复用到 OS 线程上。启动一个 goroutine 只需 go func() {}。多个 goroutine 可以共享同一个 OS 线程，一个 goroutine 也可能在不同时间片跑在不同的 OS 线程上。",
	"rust ownership": "Rust 的所有权系统在编译期保证内存安全，无需 GC。每个值有且只有一个 owner，离开作用域时自动释放。通过借用（&T 不可变借用，&mut T 可变借用）临时访问值而不转移所有权。",
}

// 工具实现

func handleGetWeather(input json.RawMessage) (string, error) {
	var args struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	if weather, ok := weatherData[args.City]; ok {
		return weather, nil
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
	// 整数结果不显示小数点
	if result == float64(int64(result)) {
		return fmt.Sprintf("%s = %d", args.Expression, int64(result)), nil
	}
	return fmt.Sprintf("%s = %s", args.Expression, strconv.FormatFloat(result, 'f', -1, 64)), nil
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
	return fmt.Sprintf("未找到关于 '%s' 的相关结果，建议换一组关键词", args.Keyword), nil
}

// 表达式解析器

func evalExpression(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	if expr == "" {
		return 0, fmt.Errorf("空表达式")
	}
	return evalAddSub(expr)
}

func evalAddSub(expr string) (float64, error) {
	// 从右向左找最后一个 + 或 -（处理左结合）
	parenDepth := 0
	for i := len(expr) - 1; i >= 0; i-- {
		switch expr[i] {
		case ')':
			parenDepth++
		case '(':
			parenDepth--
		case '+':
			if parenDepth == 0 {
				left, err := evalAddSub(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalMulDiv(expr[i+1:])
				if err != nil {
					return 0, err
				}
				return left + right, nil
			}
		case '-':
			if parenDepth == 0 {
				if i == 0 {
					right, err := evalMulDiv(expr[1:])
					if err != nil {
						return 0, err
					}
					return -right, nil
				}
				left, err := evalAddSub(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalMulDiv(expr[i+1:])
				if err != nil {
					return 0, err
				}
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
				left, err := evalMulDiv(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalPrimary(expr[i+1:])
				if err != nil {
					return 0, err
				}
				return left * right, nil
			}
		case '/':
			if parenDepth == 0 {
				left, err := evalMulDiv(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalPrimary(expr[i+1:])
				if err != nil {
					return 0, err
				}
				if right == 0 {
					return 0, fmt.Errorf("除数不能为零")
				}
				return left / right, nil
			}
		}
	}
	return evalPrimary(expr)
}

func evalPrimary(expr string) (float64, error) {
	if expr == "" {
		return 0, fmt.Errorf("空表达式")
	}
	// 处理括号
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
		return 0, fmt.Errorf("括号不匹配")
	}
	// 处理负数（一元负号）
	if expr[0] == '-' {
		val, err := evalPrimary(expr[1:])
		if err != nil {
			return 0, err
		}
		return -val, nil
	}
	return strconv.ParseFloat(expr, 64)
}

// 工具路由

var toolHandlers = map[string]func(json.RawMessage) (string, error){
	"get_weather": handleGetWeather,
	"calculate":   handleCalculate,
	"search":      handleSearch,
}

func traceRawResponseEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TRACE_DEEPSEEK_RESPONSE")))
	return v == "1" || v == "true" || v == "yes"
}

func printJSON(label string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("%s: JSON 序列化失败: %v\n", label, err)
		return
	}
	fmt.Printf("%s\n%s\n", label, string(b))
}

// Agent 循环

func runAgent(client *openai.Client, ctx context.Context, question string) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: question},
	}

	fmt.Printf("[user] %s\n", question)
	for i := 0; i < 5; i++ {
		round := i + 1
		fmt.Printf("[round %d] request: messages=%d tools=%d\n", round, len(messages), len(tools))

		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     "deepseek-v4-flash",
			MaxTokens: 4096,
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			fmt.Printf("API 调用失败: %v\n", err)
			return
		}

		choice := resp.Choices[0]
		if traceRawResponseEnabled() {
			printJSON(fmt.Sprintf("[round %d] raw_response", round), resp)
		}
		fmt.Printf("[round %d] response: finish_reason=%v tool_calls=%d\n", round, choice.FinishReason, len(choice.Message.ToolCalls))

		if choice.FinishReason == openai.FinishReasonToolCalls {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})
			for _, tc := range choice.Message.ToolCalls {
				fmt.Printf("[tool_call] id=%s name=%s arguments=%s\n", tc.ID, tc.Function.Name, tc.Function.Arguments)
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
				fmt.Printf("[tool_result] id=%s name=%s result=%s\n", tc.ID, tc.Function.Name, result)
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			fmt.Printf("[round %d] tool results appended, continue\n", round)
			continue
		}

		fmt.Println("[assistant_final]")
		fmt.Println(choice.Message.Content)
		if choice.FinishReason == openai.FinishReasonLength {
			fmt.Println("\n⚠️ 警告：模型输出被截断，可能需要增大 MaxTokens")
		}
		return
	}

	fmt.Println("达到最大迭代次数，未能完成请求")
}

func main() {
	loadEnv(".env")

	config := openai.DefaultConfig(os.Getenv("DEEPSEEK_API_KEY"))
	config.BaseURL = "https://api.deepseek.com"
	// 注入 thinking 禁用
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{base: httpClient.Transport}
	}
	client := openai.NewClientWithConfig(config)

	fmt.Println("=== 示例 1: 天气查询 ===")
	runAgent(client, context.Background(), "北京天气怎么样？")

	fmt.Println("\n=== 示例 2: 计算 + 搜索 ===")
	runAgent(client, context.Background(), "帮我算一下 (15 + 27) * 3，然后查一下 goroutine 是什么")
}
