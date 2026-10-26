// Compatibility: every Facade method the MCP tools use, exercised through
// the Bridge against a real daemon. Tool names/shapes stay identical;
// the bridge only changes the transport.
package mcp

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/internal/server"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func testBridge(t *testing.T) (*Bridge, string, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := core.NewWithOptions(db, core.Options{DataDir: dir})
	sock := filepath.Join(dir, "jackketchd.sock")
	srv, err := server.New(eng, sock, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	for i := 0; i < 100; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ws := t.TempDir()
	b, err := DialBridge(sock, ws, "test-harness", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b, ws, func() {
		_ = b.Close()
		cancel()
		_ = srv.Close()
		_ = eng.Close()
		_ = db.Close()
	}
}

func TestBridge_FacadeParity(t *testing.T) {
	b, _, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start / Status / List / Apps.
	started, err := b.Start(ctx, api.StartRequest{
		Command: "sh", Args: []string{"-c", "echo bridge-parity && sleep 30"},
		WorkDir: ".",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, err := b.Status(started.ProcessID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.ProcessID != started.ProcessID {
		t.Fatalf("status id = %q", st.ProcessID)
	}
	list, err := b.List()
	if err != nil || len(list.Processes) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if _, err := b.Apps(); err != nil {
		t.Fatalf("Apps: %v", err)
	}

	// Logs / WaitForLog.
	logs, err := b.GetLogs(api.GetLogsRequest{ProcessID: started.ProcessID, Lines: 50})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	_ = logs
	wr, err := b.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: started.ProcessID, Contains: "bridge-parity", TimeoutMS: 15000})
	if err != nil || !wr.Matched {
		t.Fatalf("WaitForLog = %+v, %v", wr, err)
	}

	// Stdin / Env / Signal policy surface.
	if _, err := b.SendStdin(started.ProcessID, "x\n"); err != nil {
		t.Fatalf("SendStdin: %v", err)
	}
	if _, err := b.ProcessEnv(api.ProcessEnvRequest{ProcessID: started.ProcessID}); err != nil {
		t.Fatalf("ProcessEnv: %v", err)
	}
	if _, err := b.SignalProcess(ctx, api.SignalProcessRequest{ProcessID: started.ProcessID, Signal: "SIGUSR1"}); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	// Events pull model.
	sub, err := b.SubscribeEvents("mcp", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, err := b.GetEvents(api.GetEventsRequest{SubscriptionID: sub.SubscriptionID}); err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if _, err := b.UnsubscribeEvents("mcp", sub.SubscriptionID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// Stats / audit.
	if _, err := b.RuntimeStats(); err != nil {
		t.Fatalf("RuntimeStats: %v", err)
	}
	if _, err := b.GetAuditLog(10, ""); err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}

	// Restart policy + Stop + Remove.
	if _, err := b.SetRestartPolicy(api.SetRestartPolicyRequest{ProcessID: started.ProcessID, Policy: "never"}); err != nil {
		t.Fatalf("SetRestartPolicy: %v", err)
	}
	if err := b.Stop(ctx, started.ProcessID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := b.RemoveProcess(started.ProcessID, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// v3 tools.
	info, err := b.ProjectInfo(ctx)
	if err != nil || info["configSource"] == nil {
		t.Fatalf("ProjectInfo = %v, %v", info, err)
	}
	if _, err := b.ValidateConfig(ctx, "apps:\n  a:\n    command: [x]\n"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, err := b.PlanConfig(ctx, "apps:\n  a:\n    command: [x]\n"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := b.SearchLogs(ctx, "bridge-parity", nil, false, false, "", "", 0, 10); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := b.ListSessions(ctx); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
}

func TestBridge_TwoSessionsShare(t *testing.T) {
	b1, ws, done1 := testBridge(t)
	defer done1()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A does start; B (different harness, same workspace) sees it.
	started, err := b1.Start(ctx, api.StartRequest{
		Command: "sh", Args: []string{"-c", "echo shared && sleep 30"}, WorkDir: ws,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	b2, err := DialBridge(b1.client.SocketPath(), ws, "second-harness", nil)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer b2.Close()
	list, err := b2.List()
	if err != nil {
		t.Fatalf("B list: %v", err)
	}
	found := false
	for _, p := range list.Processes {
		if p.ProcessID == started.ProcessID {
			found = true
		}
	}
	if !found {
		t.Fatal("second session does not see the first session's process")
	}
	// B restarts A's process; A observes the same id.
	if err := b2.Restart(ctx, started.ProcessID); err != nil {
		t.Fatalf("B restart: %v", err)
	}
	if _, err := b1.Status(started.ProcessID); err != nil {
		t.Fatalf("A status after B restart: %v", err)
	}
	// Close both bridges; the process persists (persistent lifetime).
	_ = b1.Close()
	_ = b2.Close()
}

func TestBridgeGetLogsFilters(t *testing.T) {
	b, ws, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	started, err := b.Start(ctx, api.StartRequest{
		Command: "sh", Args: []string{"-c", "echo to-stdout; echo to-stderr >&2; sleep 30"}, WorkDir: ws,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := b.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: started.ProcessID, Contains: "to-stdout", TimeoutMS: 10000}); err != nil {
		t.Fatalf("wait stdout: %v", err)
	}
	if _, err := b.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: started.ProcessID, Contains: "to-stderr", TimeoutMS: 10000}); err != nil {
		t.Fatalf("wait stderr: %v", err)
	}
	errOut, err := b.GetLogs(api.GetLogsRequest{ProcessID: started.ProcessID, Stream: "stderr"})
	if err != nil {
		t.Fatalf("get stderr: %v", err)
	}
	if len(errOut.Entries) == 0 {
		t.Fatal("no stderr entries")
	}
	for _, e := range errOut.Entries {
		if e.Stream != "stderr" {
			t.Fatalf("stream filter leaked: %+v", e)
		}
		if !strings.Contains(e.Line, "to-stderr") {
			t.Fatalf("stderr line = %q", e.Line)
		}
	}
	hit, err := b.GetLogs(api.GetLogsRequest{ProcessID: started.ProcessID, Contains: "to-stdout"})
	if err != nil {
		t.Fatalf("get contains: %v", err)
	}
	if len(hit.Entries) == 0 {
		t.Fatal("contains filter returned nothing")
	}
	for _, e := range hit.Entries {
		if !strings.Contains(e.Line, "to-stdout") {
			t.Fatalf("contains leaked: %+v", e)
		}
	}
}

func TestBridgeOpenShell(t *testing.T) {
	b, ws, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	started, err := b.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", "sleep 30"}, WorkDir: ws})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	sh, err := b.OpenShell(ctx, api.OpenShellRequest{ProcessID: started.ProcessID, Shell: "sh"})
	if err != nil {
		t.Fatalf("open_shell: %v", err)
	}
	if sh.Command != "sh" {
		t.Fatalf("open_shell command = %q", sh.Command)
	}
	if sh.ProcessID == "" {
		t.Fatal("open_shell process id empty")
	}
	_ = b.Stop(ctx, sh.ProcessID)
}

func TestBridgeEvents(t *testing.T) {
	b, ws, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sub, err := b.SubscribeEvents("mcp", api.SubscribeEventsRequest{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer b.UnsubscribeEvents("mcp", sub.SubscriptionID)
	if _, err := b.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", "echo evt && sleep 30"}, WorkDir: ws}); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ev, err := b.GetEvents(api.GetEventsRequest{SubscriptionID: sub.SubscriptionID})
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if len(ev.Events) > 0 {
			if ev.SubscriptionID != sub.SubscriptionID {
				t.Fatalf("subscription id = %q", ev.SubscriptionID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no events within deadline")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestBridgeAppsKeys(t *testing.T) {
	b, _, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	yaml := "apps:\n  web:\n    command: [\"sh\", \"-c\", \"sleep 30\"]\n    workdir: \".\"\n"
	res, err := b.ApplyConfig(ctx, yaml, 0, "test")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied, _ := res["applied"].(bool); !applied {
		t.Fatalf("apply = %v", res)
	}
	apps, err := b.Apps()
	if err != nil {
		t.Fatalf("apps: %v", err)
	}
	var web *api.AppInfo
	for i := range apps.Apps {
		if apps.Apps[i].Name == "web" {
			web = &apps.Apps[i]
		}
	}
	if web == nil {
		t.Fatalf("web app missing: %+v", apps.Apps)
	}
	if web.Command != "sh" || len(web.Args) != 2 || web.WorkDir != "." {
		t.Fatalf("web = %+v", web)
	}
}

func TestBridgeAuditAfterStart(t *testing.T) {
	b, ws, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := b.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", "sleep 30"}, WorkDir: ws}); err != nil {
		t.Fatalf("start: %v", err)
	}
	audit, err := b.GetAuditLog(50, "")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	found := false
	for _, e := range audit.Entries {
		if e.Tool == "ProcessService/Start" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ProcessService/Start audit entries: %+v", audit.Entries)
	}
}

func TestBridgeUnbound(t *testing.T) {
	b, _, done := testBridge(t)
	defer done()
	t.Setenv("JACK_KETCH_PROJECT", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Chdir("/")
	ub, err := DialBridge(b.client.SocketPath(), "", "x", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ub.Close()
	if ub.Bound() {
		t.Fatalf("expected unbound, got %q", ub.WorkspaceID())
	}
	info, err := ub.ProjectInfo(context.Background())
	if err != nil {
		t.Fatalf("project info: %v", err)
	}
	if bound, _ := info["bound"].(bool); bound {
		t.Fatalf("info = %v", info)
	}
	if hint, _ := info["hint"].(string); hint == "" {
		t.Fatalf("info = %v", info)
	}
}

func TestBridgeForWorkspace(t *testing.T) {
	b, _, done := testBridge(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir2 := t.TempDir()
	child, err := b.ForWorkspace(ctx, dir2)
	if err != nil {
		t.Fatalf("for workspace: %v", err)
	}
	started, err := child.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", "echo child && sleep 30"}, WorkDir: dir2})
	if err != nil {
		t.Fatalf("child start: %v", err)
	}
	childList, err := child.List()
	if err != nil {
		t.Fatalf("child list: %v", err)
	}
	childFound := false
	for _, p := range childList.Processes {
		if p.ProcessID == started.ProcessID {
			childFound = true
		}
	}
	if !childFound {
		t.Fatal("child does not see its process")
	}
	rootList, err := b.List()
	if err != nil {
		t.Fatalf("root list: %v", err)
	}
	for _, p := range rootList.Processes {
		if p.ProcessID == started.ProcessID {
			t.Fatal("root workspace lists the child's process")
		}
	}
}
