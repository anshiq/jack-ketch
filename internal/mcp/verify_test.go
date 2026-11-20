package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const hotReloadScript = `echo "compiled successfully"
while read l; do
  if [ "$l" = bad ]; then
    echo "src/x.ts(3,4): error TS2304: Cannot find name 'y'."
  else
    echo "compiled successfully"
  fi
done`

func TestVerifyChangeRestartPasses(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	start, ok := surfaceCall(t, session, "start_process", map[string]any{"command": "sh", "args": []string{"-c", "echo up; sleep 30"}, "workdir": t.TempDir()})
	if !ok {
		t.Fatal("start_process failed")
	}
	pid := start["process_id"].(string)
	out, ok := surfaceCall(t, session, "verify_change", map[string]any{"process_id": pid, "settle_ms": 300})
	if !ok {
		t.Fatal("verify_change errored")
	}
	if out["verdict"] != "pass" {
		t.Fatalf("verdict = %v: %v", out["verdict"], out)
	}
	if out["instance_id"] == start["instance_id"] {
		t.Fatalf("process was not restarted: %v", out)
	}
}

func TestVerifyChangeRestartDetectsCrash(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	dir := t.TempDir()
	start, ok := surfaceCall(t, session, "start_process", map[string]any{"command": "sh", "args": []string{"-c", "if [ -f crash ]; then echo 'panic: boom' >&2; exit 2; fi; sleep 30"}, "workdir": dir})
	if !ok {
		t.Fatal("start_process failed")
	}
	pid := start["process_id"].(string)
	writeFile(t, dir, "crash")
	out, _ := surfaceCall(t, session, "verify_change", map[string]any{"process_id": pid, "settle_ms": 1500})
	if out["verdict"] != "fail" {
		t.Fatalf("crash not detected: %v", out)
	}
	errs, _ := out["new_errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("panic not reported as a new error: %v", out)
	}
}

func TestVerifyChangeHotReloadFailLine(t *testing.T) {
	srv := NewServer(surfaceRuntime(t), nil)
	session := surfaceSession(t, srv, nil, nil)
	start, ok := surfaceCall(t, session, "start_process", map[string]any{"command": "sh", "args": []string{"-c", hotReloadScript}, "workdir": t.TempDir()})
	if !ok {
		t.Fatal("start_process failed")
	}
	pid := start["process_id"].(string)
	if _, ok := surfaceCall(t, session, "wait_for_log", map[string]any{"process_id": pid, "contains": "compiled successfully", "timeout_ms": 5000}); !ok {
		t.Fatal("initial wait failed")
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		surfaceCall(t, session, "send_stdin", map[string]any{"process_id": pid, "data": "ok\n"})
	}()
	pass, _ := surfaceCall(t, session, "verify_change", map[string]any{"process_id": pid, "restart": false, "wait_contains": "compiled successfully", "fail_pattern": "error", "timeout_ms": 5000})
	if pass["verdict"] != "pass" {
		t.Fatalf("hot reload pass = %v", pass)
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		surfaceCall(t, session, "send_stdin", map[string]any{"process_id": pid, "data": "bad\n"})
	}()
	failed, _ := surfaceCall(t, session, "verify_change", map[string]any{"process_id": pid, "restart": false, "wait_contains": "compiled successfully", "fail_pattern": "error", "timeout_ms": 5000})
	if failed["verdict"] != "fail" {
		t.Fatalf("hot reload fail = %v", failed)
	}
	errs, _ := failed["new_errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("new_errors = %v", failed["new_errors"])
	}
}

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
