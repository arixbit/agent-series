package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Request 是一次 Agent 调用的输入
type Request struct {
	// 用户当前的问题
	Message string
	// 之前的对话历史（由框架管理）
	History []Message
	// System prompt
	System string
}

// Response 是 Agent 调用的输出
type Response struct {
	// 最终回答文本
	Text string
	// 更新后的对话历史
	History []Message
	// 消耗 token 统计
	InputTokens  int
	OutputTokens int
}

// Message 是对话中的一条消息
type Message struct {
	Role    string // "user" / "assistant"
	Content []ContentBlock
}

// ContentBlock 是消息内容块
type ContentBlock interface {
	Type() string // "text" / "tool_use" / "tool_result"
	// 以下方法仅特定类型的 Block 实现，调用前应先用 Type() 判断
	ID() string             // tool_use / tool_result
	Name() string           // tool_use
	Input() json.RawMessage // tool_use
	Text() string           // text
	IsError() bool          // tool_result
}

// NewTextBlock 创建文本内容块
func NewTextBlock(text string) ContentBlock {
	return &textBlock{text: text}
}

// NewToolResultBlock 创建工具结果内容块
func NewToolResultBlock(id string, result string, isError bool) ContentBlock {
	return &toolResultBlock{id: id, result: result, isError: isError}
}

// NewToolUseBlock 创建工具调用内容块
// 由 ModelProvider 解析 API 响应时构造，框架使用者一般不需要直接调用
func NewToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return &toolUseBlock{id: id, name: name, input: input}
}

type textBlock struct {
	text string
}

func (b *textBlock) Type() string           { return "text" }
func (b *textBlock) Text() string           { return b.text }
func (b *textBlock) ID() string             { return "" }
func (b *textBlock) Name() string           { return "" }
func (b *textBlock) Input() json.RawMessage { return nil }
func (b *textBlock) IsError() bool          { return false }

type toolResultBlock struct {
	id      string
	result  string
	isError bool
}

func (b *toolResultBlock) Type() string           { return "tool_result" }
func (b *toolResultBlock) ID() string             { return b.id }
func (b *toolResultBlock) Name() string           { return "" }
func (b *toolResultBlock) Input() json.RawMessage { return nil }
func (b *toolResultBlock) Text() string           { return b.result }
func (b *toolResultBlock) IsError() bool          { return b.isError }

type toolUseBlock struct {
	id    string
	name  string
	input json.RawMessage
}

func (b *toolUseBlock) Type() string           { return "tool_use" }
func (b *toolUseBlock) ID() string             { return b.id }
func (b *toolUseBlock) Name() string           { return b.name }
func (b *toolUseBlock) Input() json.RawMessage { return b.input }
func (b *toolUseBlock) Text() string           { return "" }
func (b *toolUseBlock) IsError() bool          { return false }

// Agent 是框架的核心接口
type Agent interface {
	// Run 执行一次 Agent 决策循环
	Run(ctx context.Context, req Request) (*Response, error)
}

// AgentOption 函数式配置
type AgentOption func(*agentConfig)

type agentConfig struct {
	maxIterations int
	maxTokens     int
	systemPrompt  string
	tools         []Tool
	memory        Memory
	middleware    []Middleware
	registry      *ToolRegistry
	askUser       func(string, json.RawMessage) bool
	allowedOnce   map[string]onceRecord
	tracer        Tracer
}

func defaultConfig() *agentConfig {
	return &agentConfig{
		maxIterations: 5,
		maxTokens:     4096,
		tools:         []Tool{},
		memory:        NewInMemoryMemory(180000),
		allowedOnce:   make(map[string]onceRecord),
		tracer:        NoopTracer{},
	}
}

func WithMaxIterations(n int) AgentOption {
	return func(c *agentConfig) { c.maxIterations = n }
}

func WithMaxTokens(n int) AgentOption {
	return func(c *agentConfig) { c.maxTokens = n }
}

func WithSystemPrompt(prompt string) AgentOption {
	return func(c *agentConfig) { c.systemPrompt = prompt }
}

func WithTools(tools ...Tool) AgentOption {
	return func(c *agentConfig) { c.tools = tools }
}

func WithMemory(m Memory) AgentOption {
	return func(c *agentConfig) { c.memory = m }
}

func WithMiddleware(mw ...Middleware) AgentOption {
	return func(c *agentConfig) {
		c.middleware = append(c.middleware, mw...)
	}
}

func WithToolRegistry(registry *ToolRegistry) AgentOption {
	return func(c *agentConfig) { c.registry = registry }
}

func WithPermissionChecker(askUser func(string, json.RawMessage) bool) AgentOption {
	return func(c *agentConfig) {
		c.askUser = askUser
		c.allowedOnce = make(map[string]onceRecord)
	}
}

// WithTracer 配置 Agent 运行过程的事件记录器。
func WithTracer(tracer Tracer) AgentOption {
	return func(c *agentConfig) {
		if tracer == nil {
			c.tracer = NoopTracer{}
			return
		}
		c.tracer = tracer
	}
}

// reactAgent 是 Agent 接口的默认实现
type reactAgent struct {
	provider ModelProvider
	config   *agentConfig
}

// NewAgent 创建 Agent 实例
func NewAgent(provider ModelProvider, opts ...AgentOption) Agent {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	var agent Agent = &reactAgent{
		provider: provider,
		config:   cfg,
	}

	// 应用中间件（从外到内包裹）
	for i := len(cfg.middleware) - 1; i >= 0; i-- {
		agent = cfg.middleware[i](agent)
	}

	return agent
}

func (a *reactAgent) Run(ctx context.Context, req Request) (*Response, error) {
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	a.trace(ctx, TraceEvent{
		RunID:   runID,
		Type:    "run_start",
		Message: req.Message,
	})

	messages := append(req.History, Message{
		Role:    "user",
		Content: []ContentBlock{NewTextBlock(req.Message)},
	})

	totalInput, totalOutput := 0, 0

	for i := 0; i < a.config.maxIterations; i++ {
		// token 预算检查
		tokenCount, err := a.provider.CountTokens(ctx, messages)
		if err == nil && a.config.memory.ShouldCompress(tokenCount) {
			messages = a.config.memory.Compress(ctx, messages)
		}

		// 收集可用工具
		var tools []Tool
		if a.config.registry != nil {
			tools = a.config.registry.List()
		} else {
			tools = a.config.tools
		}

		// 构建请求
		chatReq := &ChatRequest{
			Messages:  messages,
			System:    a.config.systemPrompt,
			Tools:     tools,
			MaxTokens: a.config.maxTokens,
		}

		a.trace(ctx, TraceEvent{
			RunID:     runID,
			Type:      "model_request",
			Iteration: i + 1,
			Metadata: map[string]any{
				"message_count": len(messages),
				"tool_count":    len(tools),
			},
		})

		// 调用模型
		modelStart := time.Now()
		resp, err := a.provider.Chat(ctx, chatReq)
		if err != nil {
			wrapped := fmt.Errorf("模型调用失败: %w", err)
			a.trace(ctx, TraceEvent{
				RunID:      runID,
				Type:       "run_error",
				Iteration:  i + 1,
				DurationMS: time.Since(modelStart).Milliseconds(),
				Error:      wrapped.Error(),
			})
			return nil, wrapped
		}
		a.trace(ctx, TraceEvent{
			RunID:        runID,
			Type:         "model_response",
			Iteration:    i + 1,
			StopReason:   resp.StopReason,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			DurationMS:   time.Since(modelStart).Milliseconds(),
		})

		totalInput += resp.InputTokens
		totalOutput += resp.OutputTokens

		if resp.StopReason == "tool_use" {
			// 执行工具调用
			var toolResults []ContentBlock
			for _, block := range resp.Content {
				if block.Type() == "tool_use" {
					a.trace(ctx, TraceEvent{
						RunID:     runID,
						Type:      "tool_call",
						Iteration: i + 1,
						ToolName:  block.Name(),
						Metadata: map[string]any{
							"input": string(block.Input()),
						},
					})
					toolStart := time.Now()
					result, err := a.executeTool(ctx, block)
					if err != nil {
						result = "工具执行出错: " + err.Error()
					}
					toolEvent := TraceEvent{
						RunID:      runID,
						Type:       "tool_result",
						Iteration:  i + 1,
						ToolName:   block.Name(),
						DurationMS: time.Since(toolStart).Milliseconds(),
						Metadata: map[string]any{
							"result_len": len(result),
						},
					}
					if err != nil {
						toolEvent.Error = err.Error()
					}
					a.trace(ctx, toolEvent)
					toolResults = append(toolResults, NewToolResultBlock(block.ID(), result, err != nil))
				}
			}

			messages = append(messages, Message{
				Role:    "assistant",
				Content: resp.Content,
			})
			messages = append(messages, Message{
				Role:    "user",
				Content: toolResults,
			})
			continue
		}

		// 最终答案（或 max_tokens 截断）
		var text string
		for _, block := range resp.Content {
			if block.Type() == "text" {
				text = block.Text()
				break
			}
		}

		if resp.StopReason == "max_tokens" {
			text += "\n\n⚠️ 警告：模型输出被截断，可能需要增大 MaxTokens"
		}

		messages = append(messages, Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		a.trace(ctx, TraceEvent{
			RunID:        runID,
			Type:         "run_end",
			Iteration:    i + 1,
			StopReason:   resp.StopReason,
			InputTokens:  totalInput,
			OutputTokens: totalOutput,
		})

		return &Response{
			Text:         text,
			History:      messages,
			InputTokens:  totalInput,
			OutputTokens: totalOutput,
		}, nil
	}

	err := fmt.Errorf("达到最大迭代次数")
	a.trace(ctx, TraceEvent{
		RunID: runID,
		Type:  "run_error",
		Error: err.Error(),
	})
	return nil, err
}

func (a *reactAgent) trace(ctx context.Context, event TraceEvent) {
	if a.config.tracer == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	// Trace 失败不应该让业务请求失败；生产环境可用 stderr tracer 或监控告警处理写入错误。
	_ = a.config.tracer.Record(ctx, event)
}

func (a *reactAgent) executeTool(ctx context.Context, block ContentBlock) (string, error) {
	var tool Tool
	if a.config.registry != nil {
		t, err := a.config.registry.Get(block.Name())
		if err != nil {
			return fmt.Sprintf("工具不存在: %s", block.Name()), nil
		}
		tool = t
	} else {
		for _, t := range a.config.tools {
			if t.Definition().Name == block.Name() {
				tool = t
				break
			}
		}
		if tool == nil {
			return fmt.Sprintf("未知工具: %s", block.Name()), nil
		}
	}

	// 权限检查
	allowed, err := a.checkPermission(ctx, tool, block.Input())
	if err != nil {
		return fmt.Sprintf("权限检查失败: %v", err), nil
	}
	if !allowed {
		return fmt.Sprintf("用户拒绝了工具 %s 的执行", block.Name()), nil
	}

	return tool.Execute(ctx, block.Input())
}

// AsChainStep 将 Agent 适配为 ChainStep
func (a *reactAgent) AsChainStep() ChainStep {
	return &agentChainStep{agent: a}
}
