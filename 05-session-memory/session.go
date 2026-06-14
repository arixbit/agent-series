package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	openai "github.com/sashabaranov/go-openai"
)

// ========== 第5篇核心：Session + Token 管理 ==========

// Session 管理对话历史和 token 预算。
// 把上一篇里函数局部的 messages 数组升级为结构体字段，跨轮次保持状态。
type Session struct {
	Messages  []openai.ChatCompletionMessage // 完整对话历史
	MaxTokens int                            // token 预算上限
	client    *openai.Client                 // 用于摘要压缩时发起 API 调用
}

func NewSession(client *openai.Client) *Session {
	return &Session{
		MaxTokens: 110_000, // 示例中故意设低阈值（远小于 1M 实际窗口），方便触发压缩演示
		client:    client,
	}
}

// countTokens 估算对话历史的 token 数。
// 用 tiktoken 做近似估算（cl100k_base 编码不完全匹配 DeepSeek tokenizer，
// 但作为预算管理参考精度足够）。tiktoken 运行时下载 tokenizer 资源，
// 大陆/离线环境可能失败——此时回退到字符粗估（1 token ≈ 2 字符）。
//
// 注意：只统计了 msg.Content，是教学用估算。生产环境需计入 tool_calls、
// tool_call_id、system prompt 等结构化字段，最终以 API 返回的 usage 为准。
func countTokens(messages []openai.ChatCompletionMessage) (int, error) {
	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		// 回退：保守估计 1 token ≈ 2 字符，每条消息额外 +4 格式开销
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
		total += 4 // role 标记等格式开销
	}
	return total, nil
}

// compressByTruncation 按完整轮次截断旧消息。
//
// 为什么不能简单 messages[2:] 删前两条？
// OpenAI/DeepSeek 格式要求每条 role:"tool" 消息必须对应前面某条
// assistant 消息里的 tool_call。乱切可能留下孤立的 tool 消息，
// 或者丢掉 assistant 的 tool_calls，API 直接报 400。
//
// 安全做法：user 消息是每轮对话的天然分界点。
// 找到第二条 user 消息，将其之前的所有内容作为完整轮次整组删除。
// 这样 tool 消息永远不会脱离它的 tool_calls。
func compressByTruncation(messages []openai.ChatCompletionMessage, targetTokens int) []openai.ChatCompletionMessage {
	for {
		count, _ := countTokens(messages)
		if count <= targetTokens || len(messages) == 0 {
			return messages
		}
		// 找到第二条 user 消息的位置
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

// compressBySummarization 把旧对话发给模型，让它总结成一条摘要消息。
//
// 切分点同样必须对齐 user 消息边界，跟 truncation 一样的理由——
// 不能把 assistant tool_calls 和 tool 消息切到两边。
//
// 代价：多一次 API 调用（花钱 + 多一轮网络往返）。
// 会丢失细节：具体数字、代码片段、精确表述都可能被压缩掉。
// 摘要失败时自动回退到截断。
func compressBySummarization(client *openai.Client, ctx context.Context, messages []openai.ChatCompletionMessage, targetTokens int) []openai.ChatCompletionMessage {
	// 从中间位置开始找 user 消息边界作为切分点
	splitIdx := len(messages) / 2
	for splitIdx < len(messages) && messages[splitIdx].Role != openai.ChatMessageRoleUser {
		splitIdx++
	}
	if splitIdx >= len(messages) {
		// 找不到合适的切分点，回退截断
		return compressByTruncation(messages, targetTokens)
	}

	// 拼接旧对话文本
	var oldConversation strings.Builder
	for _, m := range messages[:splitIdx] {
		fmt.Fprintf(&oldConversation, "[%s] %s\n", m.Role, m.Content)
	}

	// 让模型总结
	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: 512,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "用一两句话概括以下对话的核心信息：\n\n" + oldConversation.String()},
		},
	})
	if err != nil {
		// 摘要失败，回退截断
		return compressByTruncation(messages, targetTokens)
	}

	summaryText := resp.Choices[0].Message.Content
	// 把摘要作为一条 assistant 消息放在前面，后面接剩余的新对话
	return append([]openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleAssistant, Content: "【对话摘要】" + summaryText},
	}, messages[splitIdx:]...)
}

// compressMessages 混合压缩策略。
//
// 决策逻辑：
// 1. 优先截断（免费）
// 2. 如果截断删掉了一半以上 → 丢太多了，改用摘要（保关键信息）
// 3. 摘要后仍可能超标 → 最后再截一次兜底
//
// 这个取舍逻辑跟缓存淘汰策略是同一类问题：
// 免费的先用，损失太大时花钱保质量。
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

// Chat 执行一轮对话。
//
// 流程：
// 1. 把用户问题追加到消息历史
// 2. 检查 token 预算 → 超了就压缩
// 3. 发起 API 请求（带工具定义）
// 4. 如果模型要求调工具 → 执行工具 → 把结果回填消息 → 继续循环
// 5. 如果模型给了最终回复 → 追加到历史 → 返回
func (s *Session) Chat(ctx context.Context, question string) string {
	s.Messages = append(s.Messages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser, Content: question,
	})

	// 预算检查：token 超限时触发压缩
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
			// 模型要求调工具：记录 assistant 消息（含 tool_calls）
			s.Messages = append(s.Messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})
			// 执行每个工具，结果作为 tool 消息回填
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
					ToolCallID: tc.ID, // 必须带 tool_call_id，API 用它关联 assistant 的 tool_calls
				})
			}
			continue
		}

		// 模型给出最终回复
		s.Messages = append(s.Messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: choice.Message.Content,
		})
		return choice.Message.Content
	}

	return "抱歉，我暂时无法完成这个请求"
}
