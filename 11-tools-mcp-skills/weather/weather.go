package weather

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Input struct {
	City string `json:"city" jsonschema:"要查询天气的城市"`
}

// ListInput 是查询支持城市列表时使用的空参数对象。
type ListInput struct{}

func Get(_ context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, any, error) {
	city := strings.TrimSpace(input.City)
	if city == "" {
		return nil, nil, fmt.Errorf("city 不能为空")
	}

	report := map[string]string{
		"北京": "北京：晴，15°C",
		"上海": "上海：多云，19°C",
	}
	result, ok := report[city]
	if !ok {
		result = city + "：暂无 mock 天气数据"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil, nil
}

// ListSupportedCities 返回当前 mock 天气数据覆盖的城市。
func ListSupportedCities(_ context.Context, _ *mcp.CallToolRequest, _ ListInput) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "支持的 mock 城市：北京、上海"}},
	}, nil, nil
}
