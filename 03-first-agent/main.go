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

var weatherTool = openai.Tool{
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
}

var weatherData = map[string]string{
	"北京":       "15°C，晴，西北风3级",
	"上海":       "18°C，多云，东南风2级",
	"东京":       "22°C，小雨，南风1级",
	"New York": "12°C，晴，北风4级",
	"London":   "8°C，阴，西风3级",
}

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

func runAgent(client *openai.Client, ctx context.Context, question string) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: question},
	}

	for i := 0; i < 5; i++ {
		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     "deepseek-v4-flash",
			MaxTokens: 4096,
			Messages:  messages,
			Tools:     []openai.Tool{weatherTool},
		})
		if err != nil {
			fmt.Printf("API 调用失败: %v\n", err)
			return
		}

		choice := resp.Choices[0]

		if choice.FinishReason == openai.FinishReasonToolCalls {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})
			for _, tc := range choice.Message.ToolCalls {
				result, err := handleGetWeather(json.RawMessage(tc.Function.Arguments))
				if err != nil {
					result = "工具执行出错: " + err.Error()
				}
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			continue
		}

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
	runAgent(client, context.Background(), "北京和上海现在的天气怎么样？")
}
