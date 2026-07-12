package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// PermissionLevel 定义工具的执行权限级别
type PermissionLevel int

const (
	PermAlwaysAllow PermissionLevel = iota // 自动放行——只读工具
	PermAskOnce                            // 会话内记住——首次询问
	PermAskAlways                          // 每次都问——修改操作
	PermDeny                               // 硬拒绝——永不执行
)

// ToolDefinition 工具元数据
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Tool 接口——定义和执行合一
type Tool interface {
	// Definition 返回工具的元数据（名字、描述、参数 schema）
	Definition() ToolDefinition

	// Execute 执行工具逻辑
	Execute(ctx context.Context, input json.RawMessage) (string, error)

	// Permission 返回工具的执行权限级别
	Permission() PermissionLevel
}

// BaseTool 提供默认的 PermAlwaysAllow 权限
// 工具结构体嵌入 BaseTool 即可继承默认权限
type BaseTool struct{}

func (BaseTool) Permission() PermissionLevel { return PermAlwaysAllow }

// ToolRegistry 工具注册表
type ToolRegistry struct {
	tools []Tool
	index map[string]Tool // name → tool 快速查找
}

// NewToolRegistry 创建工具注册表
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: []Tool{},
		index: make(map[string]Tool),
	}
}

// Register 注册工具
func (r *ToolRegistry) Register(tool Tool) error {
	def := tool.Definition()
	if _, exists := r.index[def.Name]; exists {
		return fmt.Errorf("工具 %q 已注册", def.Name)
	}
	r.tools = append(r.tools, tool)
	r.index[def.Name] = tool
	return nil
}

// Get 按名称获取工具
func (r *ToolRegistry) Get(name string) (Tool, error) {
	tool, ok := r.index[name]
	if !ok {
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	return tool, nil
}

// List 返回所有已注册的工具
func (r *ToolRegistry) List() []Tool {
	return r.tools
}
