package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStartFailureCarriesDiagnosis(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	out, ok := surfaceCall(t, session, "start_process", map[string]any{
		"command": "sh", "args": []string{"-c", "echo 'Error: listen EADDRINUSE: address already in use :::3999' >&2; exit 1"},
		"workdir": t.TempDir(), "timeout_ms": 5000,
	})
	if !ok {
		t.Fatal("start_process failed")
	}
	d, _ := out["diagnosis"].(map[string]any)
	if d == nil || d["category"] != "port_in_use" || d["actor"] != "agent" || !strings.Contains(d["summary"].(string), "3999") {
		t.Fatalf("diagnosis = %v", out["diagnosis"])
	}
	if out["save_suggestion"] != nil {
		t.Fatalf("failed run must not suggest saving: %v", out["save_suggestion"])
	}
}

func TestStartErrorExplainsMissingTool(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "start_process", Arguments: map[string]any{
		"command": "definitely-not-a-real-tool-xyz", "workdir": t.TempDir(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not on the PATH") {
		t.Fatalf("error text = %s", text)
	}
}

func TestSuccessfulRunSuggestsSavingThroughBridge(t *testing.T) {
	b, ws, done := testBridge(t)
	defer done()
	script := filepath.Join(ws, "build.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho built\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := NewServerWith(Options{Facade: b})
	session := surfaceSession(t, srv, nil, nil)
	out, ok := surfaceCall(t, session, "start_process", map[string]any{"command": "./build.sh", "workdir": ws, "timeout_ms": 10000, "env": []string{"TARGET=web"}})
	if !ok {
		t.Fatal("start_process failed")
	}
	sug, _ := out["save_suggestion"].(map[string]any)
	if sug == nil || sug["kind"] != "task" || sug["ask"] == "" || !strings.Contains(sug["next"].(string), "save_run_config") {
		t.Fatalf("save_suggestion = %v (result %v)", out["save_suggestion"], out)
	}
	pid, _ := out["process_id"].(string)
	saved, ok := surfaceCall(t, session, "save_run_config", map[string]any{"process_id": pid, "app": "web", "mode": "build"})
	if !ok || saved["saved"] != true || saved["start_with"] != `start_process(app="web", mode="build")` {
		t.Fatalf("save_run_config = %v", saved)
	}
	apps, ok := surfaceCall(t, session, "list_apps", map[string]any{})
	if !ok {
		t.Fatal("list_apps failed")
	}
	found := false
	for _, a := range apps["apps"].([]any) {
		app := a.(map[string]any)
		if app["name"] != "web" {
			continue
		}
		for _, m := range app["modes"].([]any) {
			mm := m.(map[string]any)
			if mm["name"] == "build" && mm["default"] == true && strings.Contains(strings.Join(toStrings(mm["env_sources"]), ","), "TARGET") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("saved mode not listed: %v", apps)
	}
	again, ok := surfaceCall(t, session, "start_process", map[string]any{"app": "web", "mode": "build", "timeout_ms": 10000})
	if !ok || again["mode"] != "build" || again["save_suggestion"] != nil {
		t.Fatalf("replay = %v", again)
	}
}

func toStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestProbeRemoteTargetAsksUser(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	asked := 0
	session := surfaceSession(t, srv, nil, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			asked++
			if !strings.Contains(req.Params.Message, "example.invalid") {
				t.Errorf("message = %s", req.Params.Message)
			}
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"approve": false}}, nil
		},
	})
	start, ok := surfaceCall(t, session, "start_process", map[string]any{"command": "sh", "args": []string{"-c", "sleep 30"}, "workdir": t.TempDir()})
	if !ok {
		t.Fatal("start failed")
	}
	out, ok := surfaceCall(t, session, "probe", map[string]any{"process_id": start["process_id"], "url": "http://example.invalid/x"})
	if !ok || asked != 1 || out["ok"] != false || !strings.Contains(out["error"].(string), "declined") {
		t.Fatalf("probe = %v asked=%d", out, asked)
	}
}

func TestProbeWithoutElicitationExplains(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	start, _ := surfaceCall(t, session, "start_process", map[string]any{"command": "sh", "args": []string{"-c", "sleep 30"}, "workdir": t.TempDir()})
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "probe", Arguments: map[string]any{"process_id": start["process_id"], "url": "/x", "method": "DELETE", "port": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "confirmed=true") {
		t.Fatalf("result = %+v", res)
	}
}
