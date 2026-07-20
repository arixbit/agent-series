package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/arixbit/agent-series/agent"
)

const maxToolInputRunes = 240

// consoleTracer 把研究过程中最重要的事件打印到终端。
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
		_, err := fmt.Fprintf(t.w, "[Agent] 第 %d 轮：请求模型（%d 条消息，%d 个可用工具）\n",
			event.Iteration,
			metadataInt(event.Metadata, "message_count"),
			metadataInt(event.Metadata, "tool_count"),
		)
		return err
	case "model_response":
		result := "返回最终回答"
		switch event.StopReason {
		case "tool_use":
			result = "决定调用工具"
		case "max_tokens":
			result = "输出达到 token 上限"
		}
		_, err := fmt.Fprintf(t.w, "[Agent] 第 %d 轮：模型%s（耗时 %dms，输入 %d token，输出 %d token）\n",
			event.Iteration,
			result,
			event.DurationMS,
			event.InputTokens,
			event.OutputTokens,
		)
		return err
	case "tool_call":
		input, _ := event.Metadata["input"].(string)
		_, err := fmt.Fprintf(t.w, "[Tool] 调用 %s，参数：%s\n", event.ToolName, singleLine(input))
		return err
	case "tool_result":
		if event.Error != "" {
			_, err := fmt.Fprintf(t.w, "[Tool] %s 失败（耗时 %dms）：%s\n",
				event.ToolName,
				event.DurationMS,
				event.Error,
			)
			return err
		}
		_, err := fmt.Fprintf(t.w, "[Tool] %s 完成（耗时 %dms，返回 %d 字节）\n",
			event.ToolName,
			event.DurationMS,
			metadataInt(event.Metadata, "result_len"),
		)
		return err
	default:
		return nil
	}
}

func metadataInt(metadata map[string]any, key string) int {
	value, _ := metadata[key].(int)
	return value
}

func singleLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxToolInputRunes {
		return value
	}
	return string(runes[:maxToolInputRunes]) + "…"
}
