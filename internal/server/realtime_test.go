package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jack-ketch/internal/core"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func realtimeTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := core.NewWithOptions(db, core.Options{DataDir: dir})
	srv, err := New(eng, filepath.Join(dir, "jackketchd.sock"), "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.handler)
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Close()
		_ = eng.Close()
		_ = db.Close()
	})
	return srv, ts
}

type realtimeConn struct {
	resp   *http.Response
	frames <-chan map[string]any
}

func openRealtime(t *testing.T, ts *httptest.Server, token string, subs []map[string]any) *realtimeConn {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"subscriptions": subs})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/jackketch.v1.RealtimeService/Connect", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect status = %d", resp.StatusCode)
	}
	frames := make(chan map[string]any, 64)
	go func() {
		defer close(frames)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				frames <- m
			}
		}
	}()
	return &realtimeConn{resp: resp, frames: frames}
}

func (c *realtimeConn) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m, ok := <-c.frames:
		if !ok {
			t.Fatal("stream closed")
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for frame")
		return nil
	}
}

func (c *realtimeConn) until(t *testing.T, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	for {
		m := c.next(t)
		if pred(m) {
			return m
		}
	}
}

func (c *realtimeConn) close() { _ = c.resp.Body.Close() }

func realtimeUnary(t *testing.T, ts *httptest.Server, token, method string, body map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/jackketch.v1.RealtimeService/"+method, strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestRealtimeHelloAndMultiplex(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	conn := openRealtime(t, ts, "tok", []map[string]any{
		{"sub": "ev", "topic": "events"},
		{"sub": "au", "topic": "audit"},
	})
	defer conn.close()
	hello := conn.next(t)
	if hello["kind"] != "hello" || hello["streamId"] == "" {
		t.Fatalf("first frame = %v", hello)
	}
	got := map[string]string{}
	deadline := time.After(8 * time.Second)
	for len(got) < 2 {
		if _, err := srv.engine.Store().AppendEvent(time.Now().UnixNano(), "test.rt", "p", "w", "", "", "", `{"n":1}`); err != nil {
			t.Fatal(err)
		}
		if err := srv.engine.Store().AppendAudit("s", "test", "p", "w", "DoThing", "{}", "ok", 1); err != nil {
			t.Fatal(err)
		}
		attempt := time.After(250 * time.Millisecond)
	drain:
		for len(got) < 2 {
			select {
			case m, ok := <-conn.frames:
				if !ok {
					t.Fatal("stream closed")
				}
				sub, _ := m["sub"].(string)
				if (sub == "ev" && m["kind"] == "event") || (sub == "au" && m["kind"] == "entry") {
					got[sub] = fmt.Sprint(m["kind"])
				}
			case <-attempt:
				break drain
			case <-deadline:
				t.Fatalf("timed out waiting for frames, got %v", got)
			}
		}
	}
}

func TestRealtimeSubscribeUnsubscribeMidStream(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "ev", "topic": "events"}})
	defer conn.close()
	hello := conn.next(t)
	streamID := fmt.Sprint(hello["streamId"])
	resp, _ := realtimeUnary(t, ts, "tok", "Subscribe", map[string]any{"streamId": streamID, "sub": "ss", "topic": "sessions"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status = %d", resp.StatusCode)
	}
	conn.until(t, func(m map[string]any) bool { return m["sub"] == "ss" && m["kind"] == "snapshot" })
	if resp, _ := realtimeUnary(t, ts, "tok", "Unsubscribe", map[string]any{"streamId": streamID, "sub": "ev"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("unsubscribe status = %d", resp.StatusCode)
	}
	if _, err := srv.engine.Store().AppendEvent(time.Now().UnixNano(), "test.after.unsub", "", "", "", "", "", "{}"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(400 * time.Millisecond)
	for {
		select {
		case m, ok := <-conn.frames:
			if !ok {
				return
			}
			if m["sub"] == "ev" {
				t.Fatalf("event arrived after unsubscribe: %v", m)
			}
		case <-deadline:
			return
		}
	}
}

func TestRealtimeWrongTokenSubscribe(t *testing.T) {
	_, ts := realtimeTestServer(t)
	conn := openRealtime(t, ts, "owner", nil)
	defer conn.close()
	hello := conn.next(t)
	resp, _ := realtimeUnary(t, ts, "other", "Subscribe", map[string]any{
		"streamId": fmt.Sprint(hello["streamId"]), "sub": "ss", "topic": "sessions",
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatal("subscribe with a different token must fail")
	}
}

func TestRealtimeEventsResume(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	var ids []int64
	for i := 0; i < 3; i++ {
		id, err := srv.engine.Store().AppendEvent(time.Now().UnixNano(), fmt.Sprintf("test.resume.%d", i), "", "", "", "", "", "{}")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "ev", "topic": "events", "cursor": map[string]any{"since": ids[0]}}})
	defer conn.close()
	conn.next(t)
	seen := []int64{}
	for len(seen) < 2 {
		m := conn.until(t, func(m map[string]any) bool { return m["kind"] == "event" })
		seen = append(seen, int64(m["id"].(float64)))
	}
	if seen[0] != ids[1] || seen[1] != ids[2] {
		t.Fatalf("resumed ids = %v, want %v", seen, ids[1:])
	}
}

func TestRealtimeDisconnectReleasesSubscriptions(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "ss", "topic": "sessions"}})
	conn.next(t)
	if srv.streamCount.Load() != 1 {
		t.Fatalf("stream count = %d", srv.streamCount.Load())
	}
	conn.close()
	for i := 0; i < 100; i++ {
		if srv.streamCount.Load() == 0 {
			srv.rtMu.Lock()
			n := len(srv.rtConns)
			srv.rtMu.Unlock()
			if n != 0 {
				t.Fatalf("connections still tracked: %d", n)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("connection was not released after disconnect")
}

func TestRTSubscriptionQueueOverflow(t *testing.T) {
	sub := newRTSub("s", "logs")
	for i := 0; i < 1100; i++ {
		_ = sub.emit(map[string]any{"kind": "line"})
	}
	if sub.dropped.Load() == 0 {
		t.Fatal("slow subscription did not drop")
	}
}

func TestRealtimeLogsResume(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	wsPath := t.TempDir()
	_, wsID, err := srv.engine.ResolveWorkspace(wsPath)
	if err != nil {
		t.Fatal(err)
	}
	ws := string(wsID)
	pr, err := srv.engine.GetOrCreateRuntime(ws)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := pr.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	started, err := rt.Start(context.Background(), apiStart("one"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Stop(context.Background(), started.ProcessID)
	proc, ok := rt.Manager().Get(started.ProcessID)
	if !ok {
		t.Fatal("process missing")
	}
	for i := 0; i < 100 && proc.Logs.NextID() == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "lg", "topic": "logs", "params": map[string]any{"workspaceId": ws, "processIds": []string{started.ProcessID}, "backlog": 100}}})
	defer conn.close()
	conn.next(t)
	first := conn.until(t, func(m map[string]any) bool { return m["sub"] == "lg" && m["kind"] == "batch" })
	lines, _ := first["lines"].([]any)
	if len(lines) == 0 {
		t.Fatalf("no backlog lines: %v", first)
	}
	var resumeAfter int64
	for _, raw := range lines {
		m := raw.(map[string]any)
		if id := int64(m["id"].(float64)); id > resumeAfter {
			resumeAfter = id
		}
	}
	rt2 := openRealtime(t, ts, "tok", []map[string]any{{"sub": "lg", "topic": "logs", "params": map[string]any{"workspaceId": ws, "processIds": []string{started.ProcessID}}, "cursor": map[string]any{"resume": map[string]any{started.ProcessID: resumeAfter + 1}}}})
	defer rt2.close()
	rt2.next(t)
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case m, ok := <-rt2.frames:
			if !ok {
				return
			}
			if m["kind"] == "batch" {
				for _, raw := range m["lines"].([]any) {
					line := raw.(map[string]any)
					if int64(line["id"].(float64)) <= resumeAfter {
						t.Fatalf("resume replayed line %v, expected > %d", line["id"], resumeAfter)
					}
				}
				return
			}
		case <-deadline:
			return
		}
	}
}

func apiStart(word string) api.StartRequest {
	return api.StartRequest{
		Command: "sh",
		Args:    []string{"-c", "printf '" + word + "\\n'; sleep 30"},
		WorkDir: "/tmp",
	}
}

func TestRealtimeConfigResume(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	proj, err := srv.engine.Store().CreateProject("rt")
	if err != nil {
		t.Fatal(err)
	}
	revs := []int64{}
	for i := 0; i < 3; i++ {
		id, err := srv.engine.Store().AddRevision(proj.ID, "project", "version: 3\n", "sha", true, "", "test", "", fmt.Sprintf("r%d", i))
		if err != nil {
			t.Fatal(err)
		}
		revs = append(revs, id)
	}
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "cfg", "topic": "config", "params": map[string]any{"projectId": proj.ID}, "cursor": map[string]any{"since": revs[0]}}})
	defer conn.close()
	conn.next(t)
	seen := []int64{}
	for len(seen) < 2 {
		m := conn.until(t, func(m map[string]any) bool {
			return m["sub"] == "cfg" && (m["kind"] == "revision" || m["kind"] == "invalid")
		})
		rev, _ := m["revision"].(map[string]any)
		seen = append(seen, int64(rev["id"].(float64)))
	}
	if seen[0] != revs[1] || seen[1] != revs[2] {
		t.Fatalf("config resume = %v, want %v", seen, revs[1:])
	}
}

func TestRealtimeSlowSubscriptionGap(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	srv.rtTopics["flood"] = func(_ context.Context, _ rtParams, emit func(map[string]any) error) error {
		for i := 0; i < 4096; i++ {
			_ = emit(map[string]any{"kind": "line", "i": i})
		}
		select {}
	}
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "f", "topic": "flood"}})
	defer conn.close()
	conn.next(t)
	gap := conn.until(t, func(m map[string]any) bool { return m["sub"] == "f" && m["kind"] == "gap" })
	inner, _ := gap["gap"].(map[string]any)
	if inner == nil || inner["dropped"] == nil {
		t.Fatalf("gap frame missing dropped count: %v", gap)
	}
}

func apiCall(t *testing.T, ts *httptest.Server, token, service, method string, body map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/jackketch.v1."+service+"/"+method, strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s/%s status = %d: %v", service, method, resp.StatusCode, out)
	}
	return out
}

func TestRealtimeProcessesLive(t *testing.T) {
	_, ts := realtimeTestServer(t)
	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "s1", "topic": "processes", "params": map[string]any{"workspaceId": "", "allWorkspaces": true}}})
	defer conn.close()
	conn.until(t, func(m map[string]any) bool { return m["kind"] == "hello" })
	conn.until(t, func(m map[string]any) bool { return m["kind"] == "snapshot" })

	ws := t.TempDir()
	res := apiCall(t, ts, "tok", "ProjectService", "Resolve", map[string]any{"path": ws})
	started := apiCall(t, ts, "tok", "ProcessService", "Start", map[string]any{
		"workspaceId": res["workspaceId"],
		"command":     []string{"sleep", "30"},
		"workDir":     ws,
	})
	id := fmt.Sprint(started["processId"])
	defer apiCall(t, ts, "tok", "ProcessService", "Stop", map[string]any{"processId": id})

	m := conn.until(t, func(m map[string]any) bool {
		return m["kind"] == "upsert" && fmt.Sprint(m["processId"]) == id
	})
	if m["process"] == nil {
		t.Fatal("upsert carried no process")
	}
}

func TestRealtimeLateProcessSubscriptionSurvives(t *testing.T) {
	_, ts := realtimeTestServer(t)
	conn := openRealtime(t, ts, "tok", nil)
	defer conn.close()
	hello := conn.until(t, func(m map[string]any) bool { return m["kind"] == "hello" })
	streamID := fmt.Sprint(hello["streamId"])

	resp, out := realtimeUnary(t, ts, "tok", "Subscribe", map[string]any{
		"streamId": streamID,
		"sub":      "late",
		"topic":    "processes",
		"params":   map[string]any{"workspaceId": "", "allWorkspaces": true},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status = %d: %v", resp.StatusCode, out)
	}
	conn.until(t, func(m map[string]any) bool { return m["sub"] == "late" && m["kind"] == "snapshot" })

	ws := t.TempDir()
	res := apiCall(t, ts, "tok", "ProjectService", "Resolve", map[string]any{"path": ws})
	started := apiCall(t, ts, "tok", "ProcessService", "Start", map[string]any{
		"workspaceId": res["workspaceId"],
		"command":     []string{"sleep", "30"},
		"workDir":     ws,
	})
	id := fmt.Sprint(started["processId"])
	defer apiCall(t, ts, "tok", "ProcessService", "Stop", map[string]any{"processId": id})

	m := conn.until(t, func(m map[string]any) bool {
		return m["sub"] == "late" && m["kind"] == "upsert" && fmt.Sprint(m["processId"]) == id
	})
	if m["process"] == nil {
		t.Fatal("upsert carried no process")
	}
}

func TestRealtimeProcessesFollowReloadedRuntime(t *testing.T) {
	srv, ts := realtimeTestServer(t)
	ws := t.TempDir()
	res := apiCall(t, ts, "tok", "ProjectService", "Resolve", map[string]any{"path": ws})
	wsID := fmt.Sprint(res["workspaceId"])
	first := apiCall(t, ts, "tok", "ProcessService", "Start", map[string]any{
		"workspaceId": wsID,
		"command":     []string{"sh", "-c", "exit 0"},
		"workDir":     ws,
	})
	firstID := fmt.Sprint(first["processId"])

	conn := openRealtime(t, ts, "tok", []map[string]any{{"sub": "s1", "topic": "processes", "params": map[string]any{"workspaceId": "", "allWorkspaces": true}}})
	defer conn.close()
	conn.until(t, func(m map[string]any) bool { return m["kind"] == "snapshot" })

	pr, err := srv.engine.GetOrCreateRuntime(wsID)
	if err != nil {
		t.Fatal(err)
	}
	rt := pr.LoadedRuntime()
	if rt == nil {
		t.Fatal("runtime not loaded")
	}
	for i := 0; i < 100; i++ {
		if st, err := rt.Status(firstID); err == nil && !liveStatus(st.Status) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = rt.RemoveProcess(firstID, true)
	srv.engine.UnloadRuntime(wsID)

	second := apiCall(t, ts, "tok", "ProcessService", "Start", map[string]any{
		"workspaceId": wsID,
		"command":     []string{"sleep", "30"},
		"workDir":     ws,
	})
	secondID := fmt.Sprint(second["processId"])
	defer apiCall(t, ts, "tok", "ProcessService", "Stop", map[string]any{"processId": secondID})

	conn.until(t, func(m map[string]any) bool {
		if m["kind"] != "upsert" || fmt.Sprint(m["processId"]) != secondID {
			return false
		}
		proc, _ := m["process"].(map[string]any)
		return proc != nil && liveStatus(fmt.Sprint(proc["status"]))
	})
}
