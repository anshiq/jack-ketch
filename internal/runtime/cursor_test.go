package runtime_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"jack-ketch/pkg/api"
)

const reloadScript = `echo "compiled successfully"
while read l; do
  if [ "$l" = bad ]; then
    echo "src/x.ts(3,4): error TS2304: Cannot find name 'y'."
    echo "compile failed"
  else
    echo "compiled successfully"
  fi
done`

func TestWaitSinceNowIgnoresOldLinesAndFailPatternStopsEarly(t *testing.T) {
	rt := newRuntime(t)
	ctx := context.Background()
	res, err := rt.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", reloadScript}, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Stop(ctx, res.ProcessID)
	first, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "compiled successfully", TimeoutMS: 5000})
	if err != nil || !first.Matched || first.Cursor == "" {
		t.Fatalf("first wait = %+v, %v", first, err)
	}

	stale, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "compiled successfully", Since: "now", TimeoutMS: 300})
	if err != nil || stale.Matched || !stale.Timeout {
		t.Fatalf("since=now matched an old line: %+v, %v", stale, err)
	}

	before, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID})
	if err != nil || before.Cursor == "" {
		t.Fatalf("get_logs = %+v, %v", before, err)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = rt.SendStdin(res.ProcessID, "ok\n")
	}()
	fresh, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "compiled successfully", Since: "now", TimeoutMS: 5000})
	if err != nil || !fresh.Matched {
		t.Fatalf("rebuild not seen: %+v, %v", fresh, err)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = rt.SendStdin(res.ProcessID, "bad\n")
	}()
	failed, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "compiled successfully", Since: "now", FailPattern: `error|failed`, TimeoutMS: 5000})
	if err != nil || !failed.Failed || failed.Matched || !strings.Contains(failed.Entry.Line, "TS2304") {
		t.Fatalf("fail pattern not honoured: %+v, %v", failed, err)
	}

	newer, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Since: before.Cursor})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "compile failed line", func() bool {
		newer, _ = rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Since: before.Cursor})
		return len(newer.Entries) == 3
	})
	if newer.Entries[0].Line != "compiled successfully" || newer.Entries[2].Line != "compile failed" {
		t.Fatalf("since cursor returned %+v", newer.Entries)
	}
	again, _ := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Since: newer.Cursor})
	if len(again.Entries) != 0 {
		t.Fatalf("cursor did not advance: %+v", again.Entries)
	}

	errs, err := rt.Errors(api.GetErrorsRequest{ProcessID: res.ProcessID, Baseline: before.Cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs.Groups) == 0 || errs.Groups[0].Kind != "compile_error" || !errs.Groups[0].New || errs.Groups[0].Locations[0].Line != 3 {
		t.Fatalf("errors = %+v", errs)
	}

	if _, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Since: "bogus"}); err == nil {
		t.Fatal("invalid since accepted")
	}
}

func TestGetLogsCompactFoldsRepeats(t *testing.T) {
	rt := newRuntime(t)
	ctx := context.Background()
	res, err := rt.Start(ctx, api.StartRequest{Command: "sh", Args: []string{"-c", `i=0; while [ $i -lt 40 ]; do echo "GET /api/items/$i 200 ${i}ms"; i=$((i+1)); done; echo done`}, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Contains: "done", TimeoutMS: 5000}); w == nil || !w.Matched {
		t.Fatalf("wait = %+v", w)
	}
	out, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 || out.Entries[0].Count != 40 || out.Compacted != 39 {
		t.Fatalf("compact = %+v", out)
	}
}

func TestWaitForLogReadyUsesAppReadiness(t *testing.T) {
	rt := newRuntimeWithConfig(t, "apps:\n  db:\n    command: [sh, -c, \"echo booting; echo db ready; sleep 30\"]\n    readiness: [\"db ready\"]\n")
	ctx := context.Background()
	res, err := rt.Start(ctx, api.StartRequest{App: "db"})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Stop(ctx, res.ProcessID)
	w, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Ready: true, TimeoutMS: 5000})
	if err != nil || !w.Matched || w.Entry.Line != "db ready" {
		t.Fatalf("app readiness not matched: %+v, %v", w, err)
	}
}
