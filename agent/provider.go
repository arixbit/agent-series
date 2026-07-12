package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	openai "github.com/sashabaranov/go-openai"
)

// ModelProvider 抽象模型提供商
type ModelProvider interface {
	// Chat 发送对话请求，返回模型响应
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// CountTokens 计算消息占用的 token 数
	CountTokens(ctx context.Context, messages []Message) (int, error)
}

type ChatRequest struct {
	Model       string
	Messages    []Message
	System      string
	Tools       []Tool
	MaxTokens   int
	Temperature float64
}

type ChatResponse struct {
	Content      []ContentBlock
	StopReason   string // "tool_use" / "end_turn" / "max_tokens"
	InputTokens  int
	OutputTokens int
}

// DeepSeekProvider 基于 DeepSeek API 的 ModelProvider 实现
type DeepSeekProvider struct {
	client       *openai.Client
	defaultModel string
}

// deepseekTransport 注入 thinking: {type: "disabled"} 到请求体中。
//
// DeepSeek V4 默认开启 Thinking Mode，在工具调用场景下如果
// 不传 reasoning_content 回后续请求会 400。对于不需要推理的
// Agent 循环（ReAct 模式），显式关闭 thinking 避免这个问题。
type deepseekTransport struct {
	base http.RoundTripper
}

func (t *deepseekTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 只处理 DeepSeek API 的 chat completions 请求
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

// NewDeepSeekProvider 创建 DeepSeek 提供商。
// 自动注入 thinking: {type: "disabled"} 以避免 V4 默认 Thinking Mode
// 在工具调用场景下缺少 reasoning_content 导致的 400 错误。
func NewDeepSeekProvider(apiKey string) *DeepSeekProvider {
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://api.deepseek.com"

	// 包装 HTTPClient 注入 thinking 禁用
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{
			base: httpClient.Transport,
		}
	}

	return &DeepSeekProvider{
		client:       openai.NewClientWithConfig(config),
		defaultModel: "deepseek-v4-flash",
	}
}

func (p *DeepSeekProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// 1. 框架 Message → OpenAI ChatCompletionMessage
	var openaiMsgs []openai.ChatCompletionMessage

	if req.System != "" {
		openaiMsgs = append(openaiMsgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			for _, block := range msg.Content {
				switch block.Type() {
				case "text":
					openaiMsgs = append(openaiMsgs, openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleUser,
						Content: block.Text(),
					})
				case "tool_result":
					openaiMsgs = append(openaiMsgs, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    block.Text(),
						ToolCallID: block.ID(),
					})
				}
			}
		case "assistant":
			oaiMsg := openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant,
			}
			for _, block := range msg.Content {
				switch block.Type() {
				case "text":
					oaiMsg.Content = block.Text()
				case "tool_use":
					oaiMsg.ToolCalls = append(oaiMsg.ToolCalls, openai.ToolCall{
						ID:   block.ID(),
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      block.Name(),
							Arguments: string(block.Input()),
						},
					})
				}
			}
			openaiMsgs = append(openaiMsgs, oaiMsg)
		}
	}

	// 2. 框架 Tool → OpenAI Tool
	var oaiTools []openai.Tool
	for _, t := range req.Tools {
		def := t.Definition()
		oaiTools = append(oaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
			},
		})
	}

	// 3. 调用 API（deepseekTransport 会自动注入 thinking: disabled）
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    openaiMsgs,
		Tools:       oaiTools,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	})
	if err != nil {
		return nil, fmt.Errorf("API 调用失败: %w", err)
	}

	// 4. OpenAI 响应 → 框架 ChatResponse
	choice := resp.Choices[0]
	var blocks []ContentBlock

	if choice.Message.Content != "" {
		blocks = append(blocks, NewTextBlock(choice.Message.Content))
	}
	for _, tc := range choice.Message.ToolCalls {
		blocks = append(blocks, NewToolUseBlock(
			tc.ID,
			tc.Function.Name,
			json.RawMessage(tc.Function.Arguments),
		))
	}

	stopReason := "end_turn"
	switch choice.FinishReason {
	case openai.FinishReasonToolCalls:
		stopReason = "tool_use"
	case openai.FinishReasonLength:
		stopReason = "max_tokens"
	}

	return &ChatResponse{
		Content:      blocks,
		StopReason:   stopReason,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

func (p *DeepSeekProvider) CountTokens(ctx context.Context, messages []Message) (int, error) {
	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		// tiktoken 运行时下载 tokenizer 资源，大陆/离线环境可能失败。
		// 回退到字符估算：1 token ≈ 2 字符（保守估计，给压缩留安全边界）
		return fallbackCount(messages), nil
	}
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			tokens := tke.Encode(block.Text(), nil, nil)
			total += len(tokens)
		}
		total += 4 // 每条消息的格式开销
	}
	return total, nil
}

// fallbackCount 在 tiktoken 不可用时用字符数估算 token 数。
// 保守估计 1 token ≈ 2 字符，实际英文约 3-4 字符/token，中文约 1.5-1.7 字符/token。
func fallbackCount(messages []Message) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			total += len(block.Text()) / 2
		}
	}
	return total
}
