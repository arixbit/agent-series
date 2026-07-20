package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/arixbit/agent-series/agent"
)

// CalculateTool 计算数学表达式
type CalculateTool struct {
	agent.BaseTool
}

func (t *CalculateTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "calculate",
		Description: "计算数学表达式，支持加减乘除和括号",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "数学表达式，如 '(1 + 2) * 3'",
				},
			},
			"required": []string{"expression"},
		},
	}
}

func (t *CalculateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
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

	if result == float64(int64(result)) {
		return fmt.Sprintf("%s = %d", args.Expression, int64(result)), nil
	}
	return fmt.Sprintf("%s = %s", args.Expression, strconv.FormatFloat(result, 'f', -1, 64)), nil
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
					return 0, fmt.Errorf("除数不能为零")
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
		val, err := evalPrimary(expr[1:])
		if err != nil {
			return 0, err
		}
		return -val, nil
	}
	return strconv.ParseFloat(expr, 64)
}
