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
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

const systemPrompt = `你是一个最小 Agent demo。

工作方式：
1. 需要外部信息或精确计算时，先调用工具
2. 拿到工具结果后，再继续判断是否需要下一步
3. 信息足够时，给出简洁答案
4. 不要在只完成部分步骤时提前结束`

var tools = []openai.Tool{
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_weather",
			Description: "获取指定城市当前的 mock 天气信息",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "城市名称，例如北京、上海、东京",
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
			Description: "计算简单四则运算表达式",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "数学表达式，例如 (15+27)*3",
					},
				},
				"required": []string{"expression"},
			},
		},
	},
}

var weatherData = map[string]string{
	"北京": "15°C，晴，西北风3级",
	"上海": "18°C，多云，东南风2级",
	"东京": "22°C，小雨，南风1级",
}

type deepseekTransport struct {
	base http.RoundTripper
}

func (t *deepseekTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "deepseek") && strings.Contains(req.URL.Path, "chat/completions") {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if err := req.Body.Close(); err != nil {
			return nil, fmt.Errorf("关闭原始请求体失败: %w", err)
		}

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

func main() {
	if err := loadEnv(".env"); err != nil {
		fmt.Printf("加载 .env 失败: %v\n", err)
		return
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("请先设置 DEEPSEEK_API_KEY，或在当前目录创建 .env")
		return
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://api.deepseek.com"
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{base: httpClient.Transport}
	}
	client := openai.NewClientWithConfig(config)

	ctx := context.Background()
	if len(os.Args) > 1 && os.Args[1] == "demo" {
		answer, err := runAgent(ctx, client, "北京和上海天气分别怎么样？顺便算一下 (15+27)*3")
		if err != nil {
			fmt.Printf("Agent 执行失败: %v\n", err)
			return
		}
		fmt.Println(answer)
		return
	}

	runCLI(ctx, client)
}

func runCLI(ctx context.Context, client *openai.Client) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Minimal Agent CLI")
	fmt.Println("输入问题开始，输入 /exit 退出。")

	for {
		fmt.Print("\n> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			return
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "/exit" {
			return
		}

		answer, err := runAgent(ctx, client, input)
		if err != nil {
			fmt.Printf("Agent 执行失败: %v\n", err)
			continue
		}
		fmt.Println(answer)
	}
}

func runAgent(ctx context.Context, client *openai.Client, question string) (string, error) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: question},
	}

	for i := 0; i < 5; i++ {
		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     "deepseek-v4-flash",
			MaxTokens: 2048,
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			return "", fmt.Errorf("调用模型失败: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("模型没有返回 choice")
		}

		choice := resp.Choices[0]
		fmt.Printf("[loop %d] finish_reason=%s\n", i+1, choice.FinishReason)

		if choice.FinishReason != openai.FinishReasonToolCalls {
			return choice.Message.Content, nil
		}

		messages = append(messages, openai.ChatCompletionMessage{
			Role:      openai.ChatMessageRoleAssistant,
			Content:   choice.Message.Content,
			ToolCalls: choice.Message.ToolCalls,
		})

		for _, toolCall := range choice.Message.ToolCalls {
			result := executeTool(toolCall)
			fmt.Printf("[tool] %s(%s) => %s\n",
				toolCall.Function.Name,
				toolCall.Function.Arguments,
				result,
			)
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				ToolCallID: toolCall.ID,
				Content:    result,
			})
		}
	}

	return "", fmt.Errorf("达到最大循环次数")
}

func executeTool(toolCall openai.ToolCall) string {
	switch toolCall.Function.Name {
	case "get_weather":
		var args struct {
			City string `json:"city"`
		}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return "参数解析失败: " + err.Error()
		}
		if weather, ok := weatherData[args.City]; ok {
			return weather
		}
		return args.City + " 的天气数据暂时不可用"
	case "calculate":
		var args struct {
			Expression string `json:"expression"`
		}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return "参数解析失败: " + err.Error()
		}
		result, err := evalExpression(args.Expression)
		if err != nil {
			return "计算失败: " + err.Error()
		}
		return fmt.Sprintf("%s = %v", args.Expression, result)
	default:
		return "未知工具: " + toolCall.Function.Name
	}
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
					return 0, fmt.Errorf("除数不能为 0")
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
	if expr[0] == '-' {
		return evalPrimary(expr[1:])
	}
	var result float64
	if _, err := fmt.Sscanf(expr, "%f", &result); err != nil {
		return 0, fmt.Errorf("无法解析数字 %q: %w", expr, err)
	}
	return result, nil
}
