package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/arixbit/agent-series/10-research-agent/tools"
	"github.com/arixbit/agent-series/agent"
)

const researchSystemPrompt = `你是一个研究助手。你的工作流程：

1. 收到研究话题后，先分析需要搜索哪些关键词
2. 使用 search 工具搜索相关信息。对于复杂话题，从多个角度搜索
3. 对搜索结果中重要的链接，使用 read_webpage 工具读取完整内容
4. 基于搜集的信息，整理成结构化的报告

报告格式：
- 概述：2-3 句话概括核心内容
- 要点：分条目列出关键信息
- 对比（如适用）：用表格或列表对比不同观点
- 来源：列出参考的链接

注意事项：
- 搜索时使用中英文关键词，获取更全面的信息
- 优先阅读官方文档和权威来源
- 如果搜索结果不够充分，尝试不同的关键词
- 不要把搜索摘要当成已经核实的事实
- 在报告中标注信息来源`

func main() {
	if err := agent.LoadEnv(".env"); err != nil {
		fmt.Printf("加载 .env 失败: %v\n", err)
		return
	}
	if os.Getenv("DEEPSEEK_API_KEY") == "" || os.Getenv("SERPER_API_KEY") == "" {
		fmt.Println("请先在 .env 中配置 DEEPSEEK_API_KEY 和 SERPER_API_KEY")
		fmt.Println("SERPER_API_KEY 请从 Serper 官方网站申请：https://serper.dev/")
		return
	}

	provider := agent.NewDeepSeekProvider(os.Getenv("DEEPSEEK_API_KEY"))

	registry := agent.NewToolRegistry()
	if err := registry.Register(&tools.SearchTool{APIKey: os.Getenv("SERPER_API_KEY")}); err != nil {
		fmt.Printf("注册搜索工具失败: %v\n", err)
		return
	}
	if err := registry.Register(&tools.ReadWebTool{}); err != nil {
		fmt.Printf("注册网页读取工具失败: %v\n", err)
		return
	}
	if err := registry.Register(&tools.CalculateTool{}); err != nil {
		fmt.Printf("注册计算工具失败: %v\n", err)
		return
	}

	researchAgent := agent.NewAgent(provider,
		agent.WithToolRegistry(registry),
		agent.WithSystemPrompt(researchSystemPrompt),
		agent.WithTracer(newConsoleTracer(os.Stdout)),
		agent.WithMemory(agent.NewSummaryMemory(
			agent.NewTruncationMemory(provider, 150000),
			provider,
		)),
		agent.WithMaxIterations(8),
		agent.WithMaxTokens(8192),
		agent.WithMiddleware(
			agent.LoggingMiddleware(),
		),
	)

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("研究助手 - 输入话题开始研究（每次输入一行，输入 quit 退出）")

	var history []agent.Message

	for {
		fmt.Print("\n> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			return
		}
		input = strings.TrimSpace(input)
		if input == "quit" {
			break
		}
		if input == "" {
			continue
		}
		if len(history) > 0 {
			fmt.Printf("[Session] 本轮带入 %d 条历史消息\n", len(history))
		}

		resp, err := researchAgent.Run(context.Background(), agent.Request{
			Message: input,
			History: history,
		})
		if err != nil {
			fmt.Printf("错误: %v\n", err)
			continue
		}

		history = resp.History
		fmt.Printf("[Session] 本轮结束，当前会话保存 %d 条历史消息\n", len(history))
		fmt.Println(resp.Text)
	}
}
