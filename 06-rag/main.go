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

// newDeepSeekClient 创建 DeepSeek Chat API 客户端。
func newDeepSeekClient() *openai.Client {
	config := openai.DefaultConfig(os.Getenv("DEEPSEEK_API_KEY"))
	config.BaseURL = "https://api.deepseek.com"
	if httpClient, ok := config.HTTPClient.(*http.Client); ok {
		httpClient.Transport = &deepseekTransport{base: httpClient.Transport}
	}
	return openai.NewClientWithConfig(config)
}

// newEmbedClient 创建 OpenAI（或兼容）Embedding API 客户端。
// DeepSeek 公开 API 没有 embedding endpoint，需要用 OpenAI 或兼容服务。
func newEmbedClient() *openai.Client {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL
	return openai.NewClientWithConfig(config)
}

// ========== 入口 ==========

func main() {
	loadEnv(".env")
	ctx := context.Background()

	mode := "interactive"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "embed":
		runEmbed(ctx)
	case "search":
		runSearch(ctx)
	case "agent":
		runAgentDemo(ctx)
	default:
		runInteractive(ctx)
	}
}

// ========== Demo 1: Embedding ==========

func runEmbed(ctx context.Context) {
	ec := newEmbedClient()

	fmt.Println("=== Demo 1: Embedding — 文字变成向量 ===")
	fmt.Println()

	docs := []string{
		"密码找回流程：用户点击忘记密码 → 输入邮箱 → 收到重置链接 → 设置新密码",
		"如何重置密码：如果您忘记了登录密码，可通过注册邮箱申请重置",
		"北京今天天气晴朗，温度 15°C",
	}

	vecs, err := embedTexts(ec, ctx, docs)
	if err != nil {
		fmt.Printf("Embedding 失败: %v\n", err)
		return
	}

	for i, doc := range docs {
		v := vecs[i]
		fmt.Printf("文本: %s\n", doc)
		fmt.Printf("  维度: %d\n", len(v))
		fmt.Printf("  前 5 个值: [%.4f, %.4f, %.4f, %.4f, %.4f]\n\n",
			v[0], v[1], v[2], v[3], v[4])
	}

	// 算相似度
	sim12 := cosineSimilarity(vecs[0], vecs[1])
	sim13 := cosineSimilarity(vecs[0], vecs[2])
	fmt.Printf("「密码找回流程」vs「如何重置密码」的余弦相似度: %.4f\n", sim12)
	fmt.Printf("「密码找回流程」vs「北京天气」的余弦相似度:     %.4f\n", sim13)
	fmt.Println()
	fmt.Println("语义相近的文本（前两条）相似度高，语义无关的文本（第一条和第三条）相似度低。")
	fmt.Println("这就是 RAG 能做到语义搜索的数学基础。")
}

// ========== Demo 2: 语义搜索 ==========

func runSearch(ctx context.Context) {
	ec := newEmbedClient()

	fmt.Println("=== Demo 2: 语义搜索 vs 关键词匹配 ===")
	fmt.Println()

	// 知识库文档
	docs := map[string]string{
		"退款政策":     "用户可在购买后 30 天内申请全额退款。退款将原路返回到支付的银行卡或第三方支付账户，处理时间 5-7 个工作日。",
		"密码找回":     "如果您忘记了登录密码，可以通过注册邮箱找回。点击登录页的'忘记密码'链接，输入注册邮箱，系统会发送重置链接到您的邮箱。",
		"会员权益":     "高级会员享有免邮费、专属折扣和优先客服支持。年会费 299 元，到期自动续费，可随时取消。",
		"API 限流规则": "免费 API 每分钟最多 60 次请求。超出后返回 429 状态码，需等待至下一分钟窗口重置。建议实现指数退避重试。",
		"Go 并发模型":  "Go 使用 goroutine + channel 的 CSP 并发模型。goroutine 是轻量级用户态线程，启动成本极低（几 KB），可以同时运行数万个。",
		"上下文窗口管理":  "上下文窗口是 LLM 能同时处理的最大 token 数。管理策略包括截断旧消息（按轮次边界切，保证 tool_calls 完整性）、摘要压缩（保留关键信息但会丢细节）、分层保留（按优先级决定删什么）。",
	}

	// 建索引
	store := NewVectorStore()
	if err := indexDocuments(store, ec, ctx, docs); err != nil {
		fmt.Printf("索引失败: %v\n", err)
		return
	}
	fmt.Printf("已索引 %d 篇文档\n\n", store.Size())

	// 测试查询
	queries := []string{
		"怎么重置密码",
		"如何申请退款",
		"并发编程模型",
	}

	for _, q := range queries {
		fmt.Printf("查询: %s\n", q)
		fmt.Println(strings.Repeat("-", 50))

		// 关键词匹配
		fmt.Println("【关键词匹配】")
		kw := strings.ToLower(q)
		found := false
		for key, content := range docs {
			if strings.Contains(strings.ToLower(key), kw) || strings.Contains(strings.ToLower(content), kw) {
				fmt.Printf("  ✓ 匹配到: %s\n", key)
				found = true
			}
		}
		if !found {
			fmt.Println("  ✗ 无结果")
		}

		// 语义搜索
		fmt.Println("【语义搜索（余弦相似度）】")
		queryVec, _ := embedOne(ec, ctx, q)
		results := store.Search(queryVec, 3)
		for i, r := range results {
			fmt.Printf("  %d. [%.4f] %s\n", i+1, r.Score, r.ID)
		}
		fmt.Println()
	}

	fmt.Println("第一个查询「怎么重置密码」：关键词匹配不到「密码找回」，但语义搜索能找到。")
	fmt.Println("这就是 RAG 跟传统搜索的根本区别——不靠字面匹配，靠语义相似。")
}

// ========== Demo 3: Agent + RAG ==========

func runAgentDemo(ctx context.Context) {
	dc := newDeepSeekClient()
	ec := newEmbedClient()

	fmt.Println("=== Demo 3: Agent + RAG 知识库 ===")
	fmt.Println()

	// 建知识库
	docs := map[string]string{
		"退款政策":      "用户可在购买后 30 天内申请全额退款。退款原路返回，处理时间 5-7 个工作日。",
		"密码找回":      "忘记密码时点击登录页'忘记密码'，输入注册邮箱，系统发送重置链接。",
		"会员权益":      "高级会员年费 299 元，享有免邮费、专属折扣和优先客服支持。到期自动续费，可随时取消。",
		"API 限流":    "免费 API 每分钟 60 次请求，超出返回 429。建议实现指数退避重试。",
		"Go 并发模型":   "Go 使用 goroutine + channel 的 CSP 并发模型。goroutine 是轻量级用户态线程，启动成本几 KB，可同时运行数万个。",
		"上下文窗口管理":   "上下文窗口是 LLM 能同时处理的最大 token 数。deepseek-v4-flash 支持 1M token。管理策略包括截断、摘要压缩、分层保留。",
		"什么是 Agent": "Agent 是一个能自主决策的 AI 程序。核心是 ReAct 循环：思考 → 行动 → 观察 → 思考，直到任务完成。与聊天机器人的本质区别是能主动调用工具。",
	}

	store := NewVectorStore()
	if err := indexDocuments(store, ec, ctx, docs); err != nil {
		fmt.Printf("索引失败: %v\n", err)
		return
	}
	SetVectorStore(store, ec)
	fmt.Printf("知识库已就绪，%d 篇文档\n\n", store.Size())

	// 创建 Session（复用 #5 的完整实现）
	session := NewSession(dc)

	questions := []string{
		"什么是 Agent？",
		"忘记密码了怎么办？",
	}

	for i, q := range questions {
		fmt.Printf("--- 第 %d 轮 ---\n", i+1)
		fmt.Printf("Q: %s\n", q)
		resp := session.Chat(ctx, q)
		fmt.Printf("A: %s\n\n", resp)
	}

	fmt.Println("Agent 通过 search_knowledge_base 工具检索了知识库，")
	fmt.Println("基于检索到的文档片段组织了回答。")
	fmt.Println("这些知识不在模型的训练数据里——是 RAG 让 Agent 有了'课外知识'。")
}

// ========== 交互式 Agent ==========

func runInteractive(ctx context.Context) {
	dc := newDeepSeekClient()
	ec := newEmbedClient()

	// 预加载知识库
	docs := map[string]string{
		"退款政策":      "用户可在购买后 30 天内申请全额退款。退款原路返回，处理时间 5-7 个工作日。",
		"密码找回":      "忘记密码时点击登录页'忘记密码'，输入注册邮箱，系统发送重置链接。",
		"会员权益":      "高级会员年费 299 元，享有免邮费、专属折扣和优先客服支持。到期自动续费，可随时取消。",
		"API 限流":    "免费 API 每分钟 60 次请求，超出返回 429。建议实现指数退避重试。",
		"Go 并发模型":   "Go 使用 goroutine + channel 的 CSP 并发模型。",
		"上下文窗口管理":   "上下文窗口是 LLM 能同时处理的最大 token 数。管理策略包括截断、摘要压缩、分层保留。",
		"什么是 Agent": "Agent 是一个能自主决策的 AI 程序，核心是 ReAct 循环。",
	}

	store := NewVectorStore()
	if err := indexDocuments(store, ec, ctx, docs); err != nil {
		fmt.Printf("索引失败: %v\n", err)
		return
	}
	SetVectorStore(store, ec)

	session := NewSession(dc)
	fmt.Println("Agent + RAG CLI")
	fmt.Println("试试: 什么是 Agent? / 怎么重置密码? / 如何退款?")
	fmt.Println()
	fmt.Print("> ")

	var input string
	for {
		if _, err := fmt.Scanln(&input); err != nil {
			break
		}
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Print("> ")
			continue
		}
		if input == "/exit" || input == "exit" {
			fmt.Println("再见。")
			return
		}

		resp := session.Chat(ctx, input)
		fmt.Printf("Agent: %s\n\n> ", resp)
	}
}
