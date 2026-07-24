package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arixbit/agent-series/agent"
)

type skillRecord struct {
	name        string
	description string
	path        string
}

type skillLoader struct {
	agent.BaseTool
	skills map[string]skillRecord
	names  []string
	out    io.Writer
}

func newSkillLoader(root string, out io.Writer) (*skillLoader, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取 Skill 目录 %q: %w", root, err)
	}

	loader := &skillLoader{
		skills: make(map[string]skillRecord),
		out:    out,
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "SKILL.md")
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("读取 Skill %q: %w", path, err)
		}
		name, description, err := parseSkillMetadata(string(content))
		if err != nil {
			return nil, fmt.Errorf("解析 Skill %q: %w", path, err)
		}
		if name != entry.Name() {
			return nil, fmt.Errorf("Skill %q 的 name %q 与目录名不一致", path, name)
		}
		loader.skills[name] = skillRecord{
			name:        name,
			description: description,
			path:        path,
		}
		loader.names = append(loader.names, name)
	}
	if len(loader.names) == 0 {
		return nil, fmt.Errorf("Skill 目录 %q 中没有可用的 SKILL.md", root)
	}
	sort.Strings(loader.names)
	for _, name := range loader.names {
		record := loader.skills[name]
		if err := writeProgress(out, "[Skill] 发现：%s - %s（正文尚未交给模型）\n", record.name, record.description); err != nil {
			return nil, err
		}
	}
	return loader, nil
}

func (t *skillLoader) Definition() agent.ToolDefinition {
	var descriptions []string
	for _, name := range t.names {
		record := t.skills[name]
		descriptions = append(descriptions, fmt.Sprintf("%s: %s", record.name, record.description))
	}
	return agent.ToolDefinition{
		Name: "load_skill",
		Description: "按名称加载一份 Skill 的完整工作说明。只有用户明确要求使用某个 Skill 时才调用。可用 Skill：" +
			strings.Join(descriptions, "；"),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "要加载的 Skill 名称",
					"enum":        t.names,
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
	}
}

func (t *skillLoader) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("解析 load_skill 参数: %w", err)
	}
	record, ok := t.skills[params.Name]
	if !ok {
		return "", fmt.Errorf("未知 Skill: %s", params.Name)
	}
	content, err := os.ReadFile(record.path)
	if err != nil {
		return "", fmt.Errorf("读取 Skill %q: %w", record.path, err)
	}
	if err := writeProgress(t.out, "[Skill] 已加载：%s（%d 字节）\n", record.name, len(content)); err != nil {
		return "", err
	}
	return string(content), nil
}

func parseSkillMetadata(content string) (string, string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", fmt.Errorf("缺少 YAML frontmatter")
	}

	var name, description string
	closed := false
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	if !closed {
		return "", "", fmt.Errorf("YAML frontmatter 没有结束标记")
	}
	if name == "" || description == "" {
		return "", "", fmt.Errorf("frontmatter 必须包含 name 和 description")
	}
	return name, description, nil
}
