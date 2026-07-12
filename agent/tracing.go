package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// TraceEvent 是 Agent 运行过程中的一条可观测事件。
type TraceEvent struct {
	Time         time.Time      `json:"time"`
	RunID        string         `json:"run_id"`
	Type         string         `json:"type"`
	Iteration    int            `json:"iteration,omitempty"`
	Message      string         `json:"message,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	StopReason   string         `json:"stop_reason,omitempty"`
	InputTokens  int            `json:"input_tokens,omitempty"`
	OutputTokens int            `json:"output_tokens,omitempty"`
	DurationMS   int64          `json:"duration_ms,omitempty"`
	Error        string         `json:"error,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Tracer 记录 Agent 运行轨迹。
type Tracer interface {
	Record(ctx context.Context, event TraceEvent) error
}

// NoopTracer 丢弃所有 TraceEvent。
type NoopTracer struct{}

// Record 实现 Tracer 接口。
func (NoopTracer) Record(ctx context.Context, event TraceEvent) error {
	return nil
}

// JSONLTracer 将 TraceEvent 逐行写成 JSONL。
type JSONLTracer struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLTracer 创建写入指定 writer 的 JSONL tracer。
func NewJSONLTracer(w io.Writer) *JSONLTracer {
	return &JSONLTracer{w: w}
}

// NewJSONLFileTracer 创建写入文件的 JSONL tracer，并返回关闭函数。
func NewJSONLFileTracer(path string) (*JSONLTracer, func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("打开 trace 文件失败: %w", err)
	}
	return NewJSONLTracer(f), f.Close, nil
}

// Record 写入一行 JSON 事件。
func (t *JSONLTracer) Record(ctx context.Context, event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.w == nil {
		return fmt.Errorf("trace writer 未配置")
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化 trace 事件失败: %w", err)
	}
	if _, err := t.w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("写入 trace 事件失败: %w", err)
	}
	return nil
}
