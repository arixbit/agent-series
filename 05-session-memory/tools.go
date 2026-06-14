package main

import (
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// ========== 工具定义（来自第4篇） ==========

var tools = []openai.Tool{
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_weather",
			Description: "获取指定城市当前的天气信息，包括温度和天气状况",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "城市名称，如北京、上海、New York",
					},
				},
				"required": []string{"city"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "calculate",
			Description: "计算数学表达式，支持加减乘除和括号，如 '(1 + 2) * 3'",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "数学表达式，如 '(1 + 2) * 3'",
					},
				},
				"required": []string{"expression"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "search",
			Description: "搜索本地知识库中的信息。适用于查找编程概念、技术文档等内容",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{
						"type":        "string",
						"description": "搜索关键词",
					},
				},
				"required": []string{"keyword"},
			},
		},
	},
}

// ========== Mock 数据 ==========

var weatherData = map[string]string{
	"北京":       "15°C，晴，西北风3级",
	"上海":       "18°C，多云，东南风2级",
	"东京":       "22°C，小雨，南风1级",
	"New York": "12°C，晴，北风4级",
	"London":   "8°C，阴，西风3级",
}

var searchResults = map[string]string{
	"go for loop":    "Go 只有 for 一种循环关键字，支持三种形式。Go 1.22 起 for 循环变量每次迭代创建新变量。",
	"goroutine":      "goroutine 是 Go 的轻量级用户执行单元，被 Go 调度器复用到 OS 线程上。",
	"rust ownership": "Rust 的所有权系统在编译期保证内存安全，无需 GC。",
}

// ========== 工具注册表 ==========

var toolHandlers = map[string]func(json.RawMessage) (string, error){
	"get_weather": handleGetWeather,
	"calculate":   handleCalculate,
	"search":      handleSearch,
}

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

// ========== 简单表达式求值器 ==========

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
