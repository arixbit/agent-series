package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arixbit/agent-series/agent"
)

const systemPrompt = `你是一个使用最小 Agent Runtime 运行的助手。

工作方式：
1. 需要外部信息或精确计算时，先调用工具
2. 拿到工具结果后，再继续判断是否需要下一步
3. 信息足够时，给出简洁答案
4. 不要在只完成部分步骤时提前结束`

type weatherTool struct {
	agent.BaseTool
}

func (t *weatherTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "get_weather",
		Description: "获取指定城市当前的 mock 天气信息",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "城市名称，例如北京、上海、东京",
				},
			},
			"required": []string{"city"},
		},
	}
}

func (t *weatherTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	data := map[string]string{
		"北京": "15°C，晴，西北风3级",
		"上海": "18°C，多云，东南风2级",
		"东京": "22°C，小雨，南风1级",
	}
	if weather, ok := data[args.City]; ok {
		return weather, nil
	}
	return args.City + " 的天气数据暂时不可用", nil
}

type calculatorTool struct {
	agent.BaseTool
}

func (t *calculatorTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "calculate",
		Description: "计算简单四则运算表达式",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "数学表达式，例如 (15+27)*3",
				},
			},
			"required": []string{"expression"},
		},
	}
}

func (t *calculatorTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
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

func main() {
	if err := agent.LoadEnv(".env"); err != nil {
		fmt.Printf("加载 .env 失败: %v\n", err)
		return
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("请先设置 DEEPSEEK_API_KEY，或在当前目录创建 .env")
		return
	}

	provider := agent.NewDeepSeekProvider(apiKey)
	registry := agent.NewToolRegistry()
	if err := registry.Register(&weatherTool{}); err != nil {
		fmt.Printf("注册天气工具失败: %v\n", err)
		return
	}
	if err := registry.Register(&calculatorTool{}); err != nil {
		fmt.Printf("注册计算工具失败: %v\n", err)
		return
	}

	runtime := agent.NewAgent(provider,
		agent.WithToolRegistry(registry),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithMemory(agent.NewInMemoryMemory(110000)),
		agent.WithMaxIterations(5),
		agent.WithMaxTokens(2048),
	)

	ctx := context.Background()
	if len(os.Args) > 1 && os.Args[1] == "demo" {
		resp, err := runtime.Run(ctx, agent.Request{
			Message: "北京和上海天气分别怎么样？顺便算一下 (15+27)*3",
		})
		if err != nil {
			fmt.Printf("Agent 执行失败: %v\n", err)
			return
		}
		fmt.Println(resp.Text)
		return
	}

	runCLI(ctx, runtime)
}

func runCLI(ctx context.Context, runtime agent.Agent) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Agent Runtime CLI")
	fmt.Println("输入问题开始，输入 /exit 退出。")

	var history []agent.Message
	for {
		fmt.Print("\n> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			return
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "/exit" {
			return
		}

		resp, err := runtime.Run(ctx, agent.Request{
			Message: input,
			History: history,
		})
		if err != nil {
			fmt.Printf("Agent 执行失败: %v\n", err)
			continue
		}
		history = resp.History
		fmt.Println(resp.Text)
	}
}

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
				left, err := evalAddSub(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalMulDiv(expr[i+1:])
				if err != nil {
					return 0, err
				}
				return left + right, nil
			}
		case '-':
			if parenDepth == 0 {
				if i == 0 {
					right, err := evalMulDiv(expr[1:])
					if err != nil {
						return 0, err
					}
					return -right, nil
				}
				left, err := evalAddSub(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalMulDiv(expr[i+1:])
				if err != nil {
					return 0, err
				}
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
				left, err := evalMulDiv(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalPrimary(expr[i+1:])
				if err != nil {
					return 0, err
				}
				return left * right, nil
			}
		case '/':
			if parenDepth == 0 {
				left, err := evalMulDiv(expr[:i])
				if err != nil {
					return 0, err
				}
				right, err := evalPrimary(expr[i+1:])
				if err != nil {
					return 0, err
				}
				if right == 0 {
					return 0, fmt.Errorf("除数不能为 0")
				}
				return left / right, nil
			}
		}
	}
	return evalPrimary(expr)
}

func evalPrimary(expr string) (float64, error) {
	if expr == "" {
		return 0, fmt.Errorf("空表达式")
	}
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
		return 0, fmt.Errorf("括号不匹配")
	}
	if expr[0] == '-' {
		value, err := evalPrimary(expr[1:])
		if err != nil {
			return 0, err
		}
		return -value, nil
	}
	var result float64
	if _, err := fmt.Sscanf(expr, "%f", &result); err != nil {
		return 0, fmt.Errorf("无法解析数字 %q: %w", expr, err)
	}
	return result, nil
}
