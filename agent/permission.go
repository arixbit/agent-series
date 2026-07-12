package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// onceRecord 缓存 "AskOnce" 权限的结果（会话级）
type onceRecord struct {
	allowed bool
}

// checkPermission 执行权限检查
func (a *reactAgent) checkPermission(ctx context.Context, tool Tool, input json.RawMessage) (bool, error) {
	level := tool.Permission()
	toolName := tool.Definition().Name

	switch level {
	case PermAlwaysAllow:
		return true, nil
	case PermAskOnce:
		// 检查会话缓存
		if record, ok := a.config.allowedOnce[toolName]; ok {
			return record.allowed, nil
		}
		// 询问用户
		if a.config.askUser == nil {
			return false, fmt.Errorf("权限检查器未配置，拒绝执行 %s（安全策略：fail-closed，需配置 WithPermissionChecker）", toolName)
		}
		allowed := a.config.askUser(toolName, input)
		a.config.allowedOnce[toolName] = onceRecord{allowed: allowed}
		return allowed, nil
	case PermAskAlways:
		if a.config.askUser == nil {
			return false, fmt.Errorf("权限检查器未配置，拒绝执行 %s（安全策略：fail-closed，需配置 WithPermissionChecker）", toolName)
		}
		return a.config.askUser(toolName, input), nil
	case PermDeny:
		return false, nil
	default:
		return false, fmt.Errorf("未知权限级别: %v", level)
	}
}

// InteractiveAsk 从终端读取用户输入，确认是否允许工具执行
func InteractiveAsk(toolName string, input json.RawMessage) bool {
	fmt.Printf("\n⚠️  Agent 请求调用工具: %s\n", toolName)
	fmt.Printf("   参数: %s\n", string(input))
	fmt.Print("   允许执行吗？[y/N] ")

	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// RestrictTools 创建权限受限的工具注册表
// 只保留权限级别 <= maxLevel 的工具
func RestrictTools(registry *ToolRegistry, maxLevel PermissionLevel) *ToolRegistry {
	restricted := NewToolRegistry()
	for _, t := range registry.List() {
		if t.Permission() <= maxLevel {
			_ = restricted.Register(t)
		}
	}
	return restricted
}
