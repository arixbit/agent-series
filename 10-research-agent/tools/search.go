package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arixbit/agent-series/agent"
)

// SearchTool 使用 Serper API 搜索互联网
type SearchTool struct {
	agent.BaseTool
	APIKey string
}

func (t *SearchTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "search",
		Description: "搜索互联网上的信息。返回相关网页的标题、URL 和摘要。适用于查找事实、新闻、技术文档等",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "搜索关键词，如 Go 1.22 新特性",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *SearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if strings.TrimSpace(t.APIKey) == "" {
		return "", fmt.Errorf("SERPER_API_KEY 未配置")
	}

	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("搜索关键词不能为空")
	}

	body, err := json.Marshal(map[string]any{
		"q":   args.Query,
		"num": 5,
		"gl":  "cn",
		"hl":  "zh-cn",
	})
	if err != nil {
		return "", fmt.Errorf("编码搜索请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://google.serper.dev/search", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("创建搜索请求失败: %w", err)
	}
	req.Header.Set("X-API-KEY", t.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("搜索请求失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if err := resp.Body.Close(); err != nil {
			return "", fmt.Errorf("关闭搜索响应失败: %w", err)
		}
		return "", fmt.Errorf("搜索请求失败: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Organic []struct {
			Title    string `json:"title"`
			Link     string `json:"link"`
			Snippet  string `json:"snippet"`
			Position int    `json:"position"`
		} `json:"organic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return "", fmt.Errorf("解析搜索结果失败: %v; 关闭响应失败: %w", err, closeErr)
		}
		return "", fmt.Errorf("解析搜索结果失败: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return "", fmt.Errorf("关闭搜索响应失败: %w", err)
	}
	if len(result.Organic) == 0 {
		return fmt.Sprintf("未找到关于 %q 的搜索结果，请换一组关键词", args.Query), nil
	}

	var sb strings.Builder
	sb.WriteString("找到 ")
	sb.WriteString(strconv.Itoa(len(result.Organic)))
	sb.WriteString(" 个相关结果：\n\n")
	for i, item := range result.Organic {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(item.Title)
		sb.WriteString("\n   ")
		sb.WriteString(item.Link)
		sb.WriteString("\n   ")
		sb.WriteString(item.Snippet)
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}
