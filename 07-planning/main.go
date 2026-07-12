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
	"sync"

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

// ========== Agent 状态枚举 ==========

type AgentState int

const (
	StatePlanning   AgentState = iota // 分析需求，制定计划
	StateActing                       // 调用工具，获取信息
	StateObserving                    // 分析工具结果
	StateConcluding                   // 给出最终答案
)

func (s AgentState) String() string {
	switch s {
	case StatePlanning:
		return "Planning"
	case StateActing:
		return "Acting"
	case StateObserving:
		return "Observing"
	case StateConcluding:
		return "Concluding"
	default:
		return "Unknown"
	}
}

// ========== System Prompts ==========

const planningSystemPrompt = `你是一个有规划能力的助手。处理复杂问题时：

1. 先分析：用户问了什么？需要哪些信息？
2. 列出步骤：我需要先查什么、再查什么？
3. 逐步执行：用工具获取每个步骤需要的信息
4. 总结：基于所有信息给出答案

对于需要从多个来源搜集信息的问题（如对比分析），优先考虑并行搜索。`

// ========== 工具定义 ==========

var tools = []openai.Tool{
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_weather",
			Description: "获取指定城市当前的天气信息",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "城市名称"},
				},
				"required": []string{"city"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "calculate",
			Description: "计算数学表达式",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{"type": "string", "description": "数学表达式"},
				},
				"required": []string{"expression"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "search",
			Description: "搜索本地知识库中的信息",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{"type": "string", "description": "搜索关键词"},
				},
				"required": []string{"keyword"},
			},
		},
	},
	saveUserMemoryTool,
}

// ========== Mock 数据 ==========

var weatherData = map[string]string{
	"北京": "15°C，晴，西北风3级",
	"上海": "18°C，多云，东南风2级",
	"东京": "22°C，小雨，南风1级",
}

var searchResults = map[string]string{
	"go for loop":        "Go 只有 for 一种循环关键字。Go 1.22 起 for 循环变量每次迭代创建新变量。",
	"goroutine":          "goroutine 是 Go 的轻量级用户态执行单元，被 Go 调度器复用到 OS 线程上。启动一个 goroutine 只需 go func() {}。",
	"rust concurrency":   "Rust 通过 ownership 和 borrowing 在编译期保证数据竞争安全。Send trait 标记可跨线程传递的类型，Sync trait 标记可跨线程共享的类型。",
	"go rust comparison": "Go 的并发模型侧重运行时安全（GC + 调度器），Rust 侧重编译期安全（借用检查器）。Go 学习曲线平缓，Rust 学习曲线陡峭。",
	"agent definition":   "Agent 是一个能自主决策的 AI 程序。核心是 ReAct 循环：思考→行动→观察→思考…直到任务完成。",
	"token context":      "上下文窗口是模型一次能处理的 token 总数上限。deepseek-v4-flash 支持 1M token。管理策略包括截断和摘要压缩。",
}

// ========== 工具实现 ==========

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

// ========== 用户记忆（第7篇新增：规划能力之"识别值得记住的信息"） ==========

// UserMemory 用户记忆存储。
// 用一个内存 map 存用户偏好和约束，生产环境换持久化 KV 或数据库。
type UserMemory struct {
	mu    sync.RWMutex
	store map[string]map[string]bool // userID → {记忆内容: true}
}

func NewUserMemory() *UserMemory {
	return &UserMemory{store: make(map[string]map[string]bool)}
}

func (m *UserMemory) Save(userID, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store[userID] == nil {
		m.store[userID] = make(map[string]bool)
	}
	m.store[userID][content] = true
}

func (m *UserMemory) Load(userID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var items []string
	for content := range m.store[userID] {
		items = append(items, content)
	}
	return items
}

// save_user_memory 工具定义
var saveUserMemoryTool = openai.Tool{
	Type: openai.ToolTypeFunction,
	Function: &openai.FunctionDefinition{
		Name:        "save_user_memory",
		Description: "记录用户的偏好、约束条件或重要事实，供后续对话使用。只有对后续交互有持续影响的信息才值得记录——如饮食习惯、技术偏好、项目约定等。不要记录琐碎的聊天内容。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "需要记录的信息，用简洁的陈述句",
				},
			},
			"required": []string{"content"},
		},
	},
}

func handleSaveUserMemory(input json.RawMessage) (string, error) {
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	userMemories.Save("default", args.Content)
	return fmt.Sprintf("已记录: %s", args.Content), nil
}

var userMemories = NewUserMemory()

// buildSystemPrompt 根据用户记忆构建系统提示词。
func buildSystemPrompt(basePrompt string) string {
	items := userMemories.Load("default")
	if len(items) == 0 {
		return basePrompt
	}
	var sb strings.Builder
	sb.WriteString(basePrompt)
	sb.WriteString("\n\n关于当前用户：\n")
	for _, item := range items {
		sb.WriteString("- ")
		sb.WriteString(item)
		sb.WriteString("\n")
	}
	return sb.String()
}

var toolHandlers = map[string]func(json.RawMessage) (string, error){
	"get_weather":      handleGetWeather,
	"calculate":        handleCalculate,
	"search":           handleSearch,
	"save_user_memory": handleSaveUserMemory,
}

// ========== Token 计数 ==========

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

// compressMessages 混合压缩：优先截断，截掉太多时改用摘要
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

// ========== 表达式求值 ==========

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

// ========== PlanningAgent ==========

type PlanningAgent struct {
	client    *openai.Client
	state     AgentState
	maxTokens int
}

func NewPlanningAgent(client *openai.Client) *PlanningAgent {
	return &PlanningAgent{
		client:    client,
		state:     StatePlanning,
		maxTokens: 110_000,
	}
}

// ChatWithSystem 带系统提示词的多轮对话
func (a *PlanningAgent) ChatWithSystem(ctx context.Context, systemPrompt string, question string, history []openai.ChatCompletionMessage) (string, []openai.ChatCompletionMessage) {
	messages := append(history, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser, Content: question,
	})

	a.state = StatePlanning

	for i := 0; i < 8; i++ { // 规划任务给更多迭代次数
		// Token 预算检查
		count, _ := countTokens(messages)
		if count >= a.maxTokens {
			messages = compressMessages(a.client, ctx, messages, a.maxTokens)
		}

		// 构建请求（第一条消息是 system prompt）
		allMsgs := append([]openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		}, messages...)

		resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     "deepseek-v4-flash",
			MaxTokens: 4096,
			Messages:  allMsgs,
			Tools:     tools,
		})
		if err != nil {
			return "API 调用失败: " + err.Error(), messages
		}

		choice := resp.Choices[0]

		if choice.FinishReason == openai.FinishReasonToolCalls {
			a.state = StateActing
			messages = append(messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})

			for _, tc := range choice.Message.ToolCalls {
				handler, ok := toolHandlers[tc.Function.Name]
				var result string
				if ok {
					r, handlerErr := handler(json.RawMessage(tc.Function.Arguments))
					if handlerErr != nil {
						result = "工具执行出错: " + handlerErr.Error()
					} else {
						result = r
					}
				} else {
					result = fmt.Sprintf("未知工具: %s", tc.Function.Name)
				}
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			a.state = StateObserving
			continue
		}

		a.state = StateConcluding
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: choice.Message.Content,
		})
		return choice.Message.Content, messages
	}

	return "抱歉，我暂时无法完成这个复杂请求", messages
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

	agent := NewPlanningAgent(client)

	// 先演示用户记忆：存一条偏好
	fmt.Println("=== 用户记忆演示 ===")
	handleSaveUserMemory(json.RawMessage(`{"content":"用户习惯用命令行操作，不喜欢 GUI 工具"}`))
	fmt.Printf("已存入记忆。当前记忆: %v\n\n", userMemories.Load("default"))

	// 多步推理示例：需要先搜索两个概念，再对比
	fmt.Println("=== 规划 Agent：多步推理 ===")
	systemPrompt := buildSystemPrompt(planningSystemPrompt)
	answer, _ := agent.ChatWithSystem(ctx, systemPrompt,
		"Go 和 Rust 的并发模型有什么区别？各有什么优缺点？", nil)
	fmt.Println(answer)
}
