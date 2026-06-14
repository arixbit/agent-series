package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// deepseekTransport 注入 thinking: {type: "disabled"}。
// DeepSeek V4 默认开启 Thinking Mode，
// 工具调用场景下不传 reasoning_content 会 400。
// 显式关闭 thinking 避免此问题。
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

func main() {
	loadEnv(".env")

	config := openai.DefaultConfig(os.Getenv("DEEPSEEK_API_KEY"))
	config.BaseURL = "https://api.deepseek.com"
	// 注入 thinking 禁用
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{base: httpClient.Transport}
	}
	client := openai.NewClientWithConfig(config)

	demoStreaming(client) // 先演示流式输出（打字机效果）

	// 测试两段文本（与 token-count 的示例相同，粗估 → API 实测对比）
	tests := []struct {
		label string
		text  string
	}{
		{"英文", "Hello, how are you? I'm writing a Go program to call the LLM API."},
		{"中文", "你好，最近怎么样？我正在写一个程序来调用人工智能的接口。"},
	}

	for _, tt := range tests {
		// 1. 字符粗估（1 token ≈ 2 字符）
		estimated := len([]rune(tt.text))/2 + 1
		fmt.Printf("=== %s ===\n", tt.label)
		fmt.Printf("输入文本: %s\n", tt.text)
		fmt.Printf("字符粗估: %d token\n", estimated)

		// 2. 调用 API，看真实 token 数
		resp, err := client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
			Model:     "deepseek-v4-flash",
			MaxTokens: 1024,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleUser, Content: tt.text},
			},
		})
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(resp.Choices[0].Message.Content)
		fmt.Printf("API 实测: 输入 token=%d  输出 token=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		fmt.Printf("(粗估 %d, 误差 %d)\n\n", estimated,
			resp.Usage.PromptTokens-estimated)
	}
}

// --- system prompt 示例（取消注释即可试）---
// 上面的 main 只用了 user 消息。实际使用中 system prompt 设定模型"人设"：
//
//	Messages: []openai.ChatCompletionMessage{
//	    {Role: openai.ChatMessageRoleSystem, Content: "你是一个资深的 Go 后端工程师。回答要简洁，优先给代码示例，少废话。"},
//	    {Role: openai.ChatMessageRoleUser, Content: "如何优雅地处理 map 中不存在的 key？"},
//	},
//
// 加了 system 后模型会更倾向直接给出代码，而不是长篇解释。
// system 消息必须放在 Messages 数组最前面，它设定的是整段对话的行为基调。
// demoStreaming 流式调用演示——模型逐 token 输出，实现"打字机效果"。
func demoStreaming(client *openai.Client) {
	stream, err := client.CreateChatCompletionStream(context.Background(), openai.ChatCompletionRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: 256,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "用 Go 写一个斐波那契数列"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	fmt.Print("streaming: ")
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != "" {
			fmt.Printf("\n\n结束原因: %s\n", chunk.Choices[0].FinishReason)
		}
	}
	fmt.Println()
}

// 非流式适合批量处理（等全部结果），流式适合聊天（即时反馈）。
// 后面的 Agent 循环用的是非流式——每轮需要拿到完整的 tool_calls 才能继续。
