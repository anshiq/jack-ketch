package mcp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jack-ketch/internal/config"
	"jack-ketch/internal/runtime"
)

func surfaceRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	rt := runtime.New(config.EmptyLoaded(t.TempDir()), nil)
	t.Cleanup(func() { _ = rt.Shutdown() })
	return rt
}

func surfaceSession(t *testing.T, srv *mcp.Server, impl *mcp.Implementation, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	if impl == nil {
		impl = &mcp.Implementation{Name: "surface-test", Version: "v1"}
	}
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go srv.Run(ctx, serverT)
	session, err := mcp.NewClient(impl, opts).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func surfaceCall(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if res.IsError {
		return nil, false
	}
	out, _ := res.StructuredContent.(map[string]any)
	return out, true
}

var readOnlyTools = map[string]bool{
	"process_status": true, "list_processes": true, "get_logs": true, "wait_for_log": true,
	"wait_for_exit": true, "list_apps": true, "get_process_env": true, "subscribe_events": true,
	"get_events": true, "unsubscribe_events": true, "runtime_stats": true, "get_audit_log": true,
	"get_project_info": true, "validate_config": true, "plan_config": true, "search_logs": true,
	"list_sessions": true, "set_workspace": true, "list_projects": true, "diagnose_process": true,
}

func assertNoTypeArray(t *testing.T, tool string, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		if typ, ok := x["type"]; ok {
			if arr, ok := typ.([]any); ok {
				t.Fatalf("tool %s: type is an array: %v", tool, arr)
			}
		}
		for _, sub := range x {
			assertNoTypeArray(t, tool, sub)
		}
	case []any:
		for _, sub := range x {
			assertNoTypeArray(t, tool, sub)
		}
	}
}

func TestToolSurface(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	res, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 35 {
		t.Fatalf("expected 35 tools, got %d", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if tool.Title == "" {
			t.Fatalf("tool %s has no title", tool.Name)
		}
		if tool.Annotations == nil {
			t.Fatalf("tool %s has no annotations", tool.Name)
		}
		if readOnlyTools[tool.Name] && !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %s should be read-only", tool.Name)
		}
		assertNoTypeArray(t, tool.Name, tool.InputSchema)
		if tool.OutputSchema != nil {
			m, _ := tool.OutputSchema.(map[string]any)
			if m["type"] != "object" {
				t.Fatalf("tool %s output schema is not an object: %v", tool.Name, m["type"])
			}
		}
	}
}

func TestWaitCapsAndProgress(t *testing.T) {
	var progress atomic.Int64
	srv := NewServerWith(Options{Facade: surfaceRuntime(t), MaxWaitMS: 300, ProgressEvery: 50 * time.Millisecond})
	session := surfaceSession(t, srv, nil, &mcp.ClientOptions{
		ProgressNotificationHandler: func(context.Context, *mcp.ProgressNotificationClientRequest) {
			progress.Add(1)
		},
	})
	start, ok := surfaceCall(t, session, "start_process", map[string]any{
		"command": "sh", "args": []string{"-c", "sleep 5"}, "workdir": t.TempDir(),
	})
	if !ok {
		t.Fatal("start_process failed")
	}
	pid, _ := start["process_id"].(string)
	if pid == "" {
		t.Fatalf("no process_id: %v", start)
	}
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "wait_for_log", Arguments: map[string]any{"process_id": pid, "contains": "never", "timeout_ms": 5000},
		Meta: mcp.Meta{"progressToken": "t1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := res.StructuredContent.(map[string]any)
	if out["clamped"] != true || out["still_running"] != true {
		t.Fatalf("expected clamped+still_running, got %v", out)
	}
	waited, _ := out["waited_ms"].(float64)
	if waited < 250 || waited > 2000 {
		t.Fatalf("waited_ms = %v", waited)
	}
	if progress.Load() < 1 {
		t.Fatal("expected at least one progress notification")
	}
}

func TestHarnessFromClientIdentity(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, &mcp.Implementation{Name: "cursor-vscode", Version: "1.0"}, nil)
	info, ok := surfaceCall(t, session, "get_project_info", map[string]any{})
	if !ok {
		t.Fatal("get_project_info failed")
	}
	if info["harness"] != "cursor" {
		t.Fatalf("harness = %v", info["harness"])
	}
	if info["maxWaitMs"] != float64(50000) {
		t.Fatalf("maxWaitMs = %v", info["maxWaitMs"])
	}
}

func TestCoreToolset(t *testing.T) {
	srv := NewServerWith(Options{Facade: surfaceRuntime(t), Toolset: "core"})
	session := surfaceSession(t, srv, nil, nil)
	res, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 17 {
		names := make([]string, 0, len(res.Tools))
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("expected 14 core tools, got %d: %v", len(res.Tools), names)
	}
}

func TestPromptsAndResources(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	ctx := context.Background()

	prompts, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range prompts.Prompts {
		got[p.Name] = true
	}
	for _, want := range []string{"start_app", "diagnose", "status", "verify"} {
		if !got[want] {
			t.Fatalf("prompt %q missing: %v", want, got)
		}
	}

	templates, err := session.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatal(err)
	}
	uris := map[string]bool{}
	for _, tpl := range templates.ResourceTemplates {
		uris[tpl.URITemplate] = true
	}
	for _, want := range []string{"jack-ketch://processes/{process_id}", "jack-ketch://processes/{process_id}/logs"} {
		if !uris[want] {
			t.Fatalf("resource template %q missing: %v", want, uris)
		}
	}

	start, ok := surfaceCall(t, session, "start_process", map[string]any{
		"command": "sh", "args": []string{"-c", "echo resource-hello && sleep 5"}, "workdir": t.TempDir(),
	})
	if !ok {
		t.Fatal("start_process failed")
	}
	pid, _ := start["process_id"].(string)
	uri := "jack-ketch://processes/" + pid + "/logs"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err == nil && len(res.Contents) == 1 && strings.Contains(res.Contents[0].Text, "resource-hello") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("resource logs did not contain the process output")
}

func TestElicitationDecline(t *testing.T) {
	b, _, done := testBridge(t)
	defer done()
	srv := NewServerWith(Options{Facade: b})
	var calls atomic.Int64
	session := surfaceSession(t, srv, nil, &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			calls.Add(1)
			return &mcp.ElicitResult{Action: "decline"}, nil
		},
	})
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "resolve_config_proposal", Arguments: map[string]any{"action": "approve"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := res.StructuredContent.(map[string]any)
	if out["resolved"] != false || out["reason"] != "user decline" {
		t.Fatalf("unexpected result: %v", out)
	}
	if calls.Load() != 1 {
		t.Fatalf("elicitation handler called %d times", calls.Load())
	}
}
