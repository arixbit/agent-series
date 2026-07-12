package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRestrictTools(t *testing.T) {
	registry := NewToolRegistry()

	// 注册不同权限级别的工具
	for _, tool := range []Tool{
		&testTool{name: "safe", level: PermAlwaysAllow},
		&testTool{name: "ask_once", level: PermAskOnce},
		&testTool{name: "ask_always", level: PermAskAlways},
		&testTool{name: "denied", level: PermDeny},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register tool failed: %v", err)
		}
	}

	// 只允许 PermAlwaysAllow
	restricted := RestrictTools(registry, PermAlwaysAllow)
	names := toolNames(restricted)
	if len(names) != 1 || names[0] != "safe" {
		t.Fatalf("expected [safe], got %v", names)
	}

	// 允许 PermAskOnce 及以下
	restricted = RestrictTools(registry, PermAskOnce)
	names = toolNames(restricted)
	if len(names) != 2 {
		t.Fatalf("expected 2 tools, got %v", names)
	}
}

func TestCheckPermissionFailClosed(t *testing.T) {
	// 不带 askUser 的 agent
	a := &reactAgent{config: &agentConfig{
		allowedOnce: make(map[string]onceRecord),
	}}

	// PermAskOnce 没有 checker → 应拒绝
	ctx := context.Background()

	ok, err := a.checkPermission(ctx, &testTool{level: PermAskOnce}, nil)
	if ok || err == nil {
		t.Fatal("PermAskOnce without askUser should fail-closed")
	}

	// PermAskAlways 没有 checker → 应拒绝
	ok, err = a.checkPermission(ctx, &testTool{level: PermAskAlways}, nil)
	if ok || err == nil {
		t.Fatal("PermAskAlways without askUser should fail-closed")
	}

	// PermAlwaysAllow 没有 checker → 应放行
	ok, err = a.checkPermission(ctx, &testTool{level: PermAlwaysAllow}, nil)
	if !ok || err != nil {
		t.Fatal("PermAlwaysAllow should pass")
	}

	// PermDeny → 应拒绝
	ok, err = a.checkPermission(ctx, &testTool{level: PermDeny}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("PermDeny should reject")
	}
}

func TestCheckPermissionWithChecker(t *testing.T) {
	a := &reactAgent{config: &agentConfig{
		allowedOnce: make(map[string]onceRecord),
		askUser:     func(name string, _ json.RawMessage) bool { return name == "approved_tool" },
	}}

	// PermAskOnce → 检查通过
	ctx := context.Background()

	ok, err := a.checkPermission(ctx, &testTool{name: "approved_tool", level: PermAskOnce}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected approved tool to pass")
	}

	// PermAskAlways → 检查拒绝
	ok, err = a.checkPermission(ctx, &testTool{name: "bad_tool", level: PermAskAlways}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected bad_tool to be rejected")
	}

	// PermAskOnce 缓存验证（第二次不调用 askUser）
	called := false
	a.config.askUser = func(name string, _ json.RawMessage) bool { called = true; return true }
	a.config.allowedOnce["cached"] = onceRecord{allowed: true}
	ok, err = a.checkPermission(ctx, &testTool{name: "cached", level: PermAskOnce}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || called {
		t.Fatal("cached AskOnce should return cached result without calling askUser")
	}
}

type testTool struct {
	BaseTool
	name  string
	level PermissionLevel
}

func (t *testTool) Definition() ToolDefinition {
	return ToolDefinition{Name: t.name, Description: "test tool"}
}

func (t *testTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", nil
}

func (t *testTool) Permission() PermissionLevel { return t.level }

func toolNames(r *ToolRegistry) []string {
	var names []string
	for _, t := range r.List() {
		names = append(names, t.Definition().Name)
	}
	return names
}
