package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arixbit/agent-series/agent"
)

// ReadWebTool 读取指定 URL 的网页内容，提取正文文本
type ReadWebTool struct {
	agent.BaseTool
}

func (t *ReadWebTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "read_webpage",
		Description: "读取指定 URL 的网页内容，提取正文文本。适用于深入阅读搜索结果中的链接",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "网页 URL，如 https://go.dev/doc/go1.22",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *ReadWebTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", args.URL, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "ResearchAgent/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("读取网页失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if err := resp.Body.Close(); err != nil {
			return "", fmt.Errorf("关闭网页响应失败: %w", err)
		}
		return "", fmt.Errorf("读取网页失败: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 限制 1MB
	if err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return "", fmt.Errorf("读取响应失败: %v; 关闭响应失败: %w", err, closeErr)
		}
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return "", fmt.Errorf("关闭网页响应失败: %w", err)
	}

	text := extractText(string(body))

	if len(strings.TrimSpace(text)) == 0 {
		return "该网页没有可提取的正文内容", nil
	}

	// 按字符截断，避免切断 UTF-8 编码。
	runes := []rune(text)
	if len(runes) > 8000 {
		text = string(runes[:8000]) + "\n\n...（内容过长，已截断）"
	}

	return text, nil
}

// extractText 从 HTML 中提取正文文本（简化版）
func extractText(html string) string {
	var result strings.Builder
	inTag := false
	inScript := false
	inStyle := false

	for i := 0; i < len(html); i++ {
		c := html[i]

		if !inTag && !inScript && !inStyle {
			if c == '<' {
				// 检查是否是 script 或 style 标签
				rest := strings.ToLower(html[i:])
				if strings.HasPrefix(rest, "<script") {
					inScript = true
				} else if strings.HasPrefix(rest, "<style") {
					inStyle = true
				} else {
					inTag = true
				}
			} else {
				result.WriteByte(c)
			}
			continue
		}

		if inScript {
			// </script> 共 9 字符，> 在索引 8；检查 html[i-8:i+1]
			if c == '>' && i >= 8 && strings.ToLower(html[i-8:i+1]) == "</script>" {
				inScript = false
			}
			continue
		}

		if inStyle {
			// </style> 共 8 字符，> 在索引 7；检查 html[i-7:i+1]
			if c == '>' && i >= 7 && strings.ToLower(html[i-7:i+1]) == "</style>" {
				inStyle = false
			}
			continue
		}

		if inTag {
			if c == '>' {
				inTag = false
				result.WriteByte(' ')
			}
		}
	}

	// 清理多余的空白
	lines := strings.Split(result.String(), "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 10 {
			cleanLines = append(cleanLines, trimmed)
		}
	}

	return strings.Join(cleanLines, "\n")
}
