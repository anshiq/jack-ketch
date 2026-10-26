package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"jack-ketch/internal/events"
	"jack-ketch/internal/logs"
	"jack-ketch/internal/process"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

type rtParams map[string]any

func (p rtParams) str(k string) string {
	if v, ok := p[k].(string); ok {
		return v
	}
	return ""
}

func (p rtParams) boolean(k string) bool {
	if v, ok := p[k].(bool); ok {
		return v
	}
	return false
}

func (p rtParams) integer(k string) int {
	switch v := p[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func (p rtParams) int64(k string) int64 {
	switch v := p[k].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		var n int64
		_, _ = fmt.Sscan(v, &n)
		return n
	}
	return 0
}

func (p rtParams) strs(k string) []string {
	switch v := p[k].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

func (p rtParams) uint64Map(k string) map[string]uint64 {
	raw, ok := p[k].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(raw))
	for id, v := range raw {
		switch n := v.(type) {
		case float64:
			out[id] = uint64(n)
		case json.Number:
			u, _ := n.Int64()
			out[id] = uint64(u)
		}
	}
	return out
}

type topicFunc func(ctx context.Context, p rtParams, emit func(map[string]any) error) error

type rtSub struct {
	id      string
	topic   string
	queue   chan map[string]any
	cancel  context.CancelFunc
	done    chan struct{}
	dropped atomic.Int64
}

func newRTSub(id, topic string) *rtSub {
	return &rtSub{id: id, topic: topic, queue: make(chan map[string]any, 1024), done: make(chan struct{})}
}

func (s *rtSub) emit(m map[string]any) error {
	select {
	case <-s.done:
		return fmt.Errorf("subscription closed")
	case s.queue <- m:
		return nil
	default:
		s.dropped.Add(1)
		return nil
	}
}

type rtConn struct {
	id     string
	token  string
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	subs   map[string]*rtSub
	order  []string
	closed bool
	wake   chan struct{}
}

func newRTConn(id, token string) *rtConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &rtConn{id: id, token: token, ctx: ctx, cancel: cancel, subs: map[string]*rtSub{}, wake: make(chan struct{}, 1)}
}

func (c *rtConn) notify() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func randomStreamID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("rt-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *Server) routeRealtime(mux *http.ServeMux) {
	p := "/jackketch.v1.RealtimeService/"
	mux.HandleFunc(p+"Connect", s.rtConnect)
	mux.HandleFunc(p+"Subscribe", s.wrap(s.rtSubscribe))
	mux.HandleFunc(p+"Unsubscribe", s.wrap(s.rtUnsubscribe))
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return ""
}

func (s *Server) rtConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subscriptions []struct {
			Sub    string         `json:"sub"`
			Topic  string         `json:"topic"`
			Params map[string]any `json:"params"`
			Cursor map[string]any `json:"cursor"`
		} `json:"subscriptions"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	token := bearerToken(r)
	conn := newRTConn(randomStreamID(), token)
	s.rtMu.Lock()
	if s.rtConns == nil {
		s.rtConns = map[string]*rtConn{}
	}
	s.rtConns[conn.id] = conn
	s.rtMu.Unlock()
	s.streamCount.Add(1)
	defer func() {
		s.streamCount.Add(-1)
		s.rtMu.Lock()
		delete(s.rtConns, conn.id)
		s.rtMu.Unlock()
		conn.cancel()
		s.closeRTConn(conn)
	}()
	for _, in := range req.Subscriptions {
		params := rtParams{}
		for k, v := range in.Params {
			params[k] = v
		}
		for k, v := range in.Cursor {
			params[k] = v
		}
		if in.Sub == "" || in.Topic == "" {
			continue
		}
		s.startRTSub(conn.ctx, conn, in.Sub, in.Topic, params)
	}
	if err := sw.send(map[string]any{"kind": "hello", "streamId": conn.id, "heartbeatMs": heartbeatInterval.Milliseconds()}); err != nil {
		return
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-conn.wake:
		case <-ticker.C:
		}
		conn.mu.Lock()
		subs := make([]*rtSub, 0, len(conn.order))
		for _, id := range conn.order {
			if sub := conn.subs[id]; sub != nil {
				subs = append(subs, sub)
			}
		}
		closed := conn.closed
		conn.mu.Unlock()
		if closed {
			return
		}
		for _, sub := range subs {
			if n := sub.dropped.Swap(0); n > 0 {
				if err := sw.send(map[string]any{"sub": sub.id, "kind": "gap", "gap": map[string]any{"dropped": n, "reason": "slow consumer"}}); err != nil {
					return
				}
			}
			select {
			case msg := <-sub.queue:
				msg["sub"] = sub.id
				if err := sw.send(msg); err != nil {
					return
				}
			case <-sub.done:
			default:
			}
		}
	}
}

func (s *Server) closeRTConn(conn *rtConn) {
	conn.mu.Lock()
	subs := make([]*rtSub, 0, len(conn.subs))
	for _, sub := range conn.subs {
		subs = append(subs, sub)
	}
	conn.subs = map[string]*rtSub{}
	conn.order = nil
	conn.closed = true
	conn.mu.Unlock()
	for _, sub := range subs {
		sub.cancel()
	}
}

func (s *Server) startRTSub(parent context.Context, conn *rtConn, id, topic string, params rtParams) {
	fn, ok := s.rtTopics[topic]
	if !ok {
		select {
		case conn.wake <- struct{}{}:
		default:
		}
		conn.mu.Lock()
		sub := newRTSub(id, topic)
		sub.queue <- map[string]any{"kind": "error", "error": fmt.Sprintf("unknown topic %q", topic)}
		if _, exists := conn.subs[id]; !exists {
			conn.order = append(conn.order, id)
		}
		conn.subs[id] = sub
		conn.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	sub := newRTSub(id, topic)
	sub.cancel = cancel
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		cancel()
		return
	}
	if old := conn.subs[id]; old != nil {
		old.cancel()
	} else {
		conn.order = append(conn.order, id)
	}
	conn.subs[id] = sub
	conn.mu.Unlock()
	go func() {
		defer close(sub.done)
		defer cancel()
		err := fn(ctx, params, func(m map[string]any) error {
			e := sub.emit(m)
			conn.notify()
			return e
		})
		if err != nil && ctx.Err() == nil {
			_ = sub.emit(map[string]any{"kind": "error", "error": err.Error()})
			conn.notify()
		}
	}()
}

func (s *Server) rtSubscribe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		StreamID string         `json:"streamId"`
		Sub      string         `json:"sub"`
		Topic    string         `json:"topic"`
		Params   map[string]any `json:"params"`
		Cursor   map[string]any `json:"cursor"`
	}
	_ = decode(r, &req)
	conn := s.rtConn(req.StreamID)
	if conn == nil {
		return nil, fmt.Errorf("not_found: unknown streamId %q", req.StreamID)
	}
	if conn.token != bearerToken(r) {
		return nil, fmt.Errorf("permission_denied: stream belongs to another token")
	}
	params := rtParams{}
	for k, v := range req.Params {
		params[k] = v
	}
	for k, v := range req.Cursor {
		params[k] = v
	}
	s.startRTSub(conn.ctx, conn, req.Sub, req.Topic, params)
	return map[string]any{"sub": req.Sub}, nil
}

func (s *Server) rtUnsubscribe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		StreamID string `json:"streamId"`
		Sub      string `json:"sub"`
	}
	_ = decode(r, &req)
	conn := s.rtConn(req.StreamID)
	if conn == nil {
		return nil, fmt.Errorf("not_found: unknown streamId %q", req.StreamID)
	}
	if conn.token != bearerToken(r) {
		return nil, fmt.Errorf("permission_denied: stream belongs to another token")
	}
	conn.mu.Lock()
	sub := conn.subs[req.Sub]
	delete(conn.subs, req.Sub)
	for i, id := range conn.order {
		if id == req.Sub {
			conn.order = append(conn.order[:i], conn.order[i+1:]...)
			break
		}
	}
	conn.mu.Unlock()
	if sub != nil {
		sub.cancel()
	}
	return map[string]any{"sub": req.Sub, "unsubscribed": sub != nil}, nil
}

func (s *Server) rtConn(id string) *rtConn {
	s.rtMu.Lock()
	defer s.rtMu.Unlock()
	return s.rtConns[id]
}

func (s *Server) rtTopicFuncs() map[string]topicFunc {
	return map[string]topicFunc{
		"processes": s.topicProcesses,
		"events":    s.topicEvents,
		"logs":      s.topicLogs,
		"attach":    s.topicAttach,
		"config":    s.topicConfig,
		"sessions":  s.topicSessions,
		"resources": s.topicResources,
		"audit":     s.topicAudit,
	}
}

type rtProcRecord struct {
	sig    string
	status string
	ws     string
}

type rtWatched struct {
	pr  *corePR
	rt  *coreRT
	off func()
}

func procSig(m map[string]any) string {
	b, _ := json.Marshal([]any{m["status"], m["pid"], m["health"], m["restarts"], m["instanceId"], m["exitCode"], m["ports"], m["stale"]})
	return string(b)
}

func liveStatus(status string) bool {
	return status == "running" || status == "ready" || status == "starting"
}

func (s *Server) topicProcesses(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	ws := p.str("workspaceId")
	all := p.boolean("allWorkspaces") || ws == ""
	snap, _ := s.snapshotProcesses(ws, all)
	if snap == nil {
		snap = []any{}
	}
	if err := emit(map[string]any{"kind": "snapshot", "snapshot": snap}); err != nil {
		return err
	}
	sent := map[string]rtProcRecord{}
	for _, item := range snap {
		m, ok := item.(map[string]any)
		if !ok || m["loaded"] != true {
			continue
		}
		id, _ := m["processId"].(string)
		wsID, _ := m["workspaceId"].(string)
		status, _ := m["status"].(string)
		sent[id] = rtProcRecord{sig: procSig(m), status: status, ws: wsID}
	}
	send := func(msg map[string]any) error {
		id, _ := msg["processId"].(string)
		switch msg["kind"] {
		case "upsert":
			if proc, ok := msg["process"].(map[string]any); ok {
				wsID, _ := proc["workspaceId"].(string)
				status, _ := proc["status"].(string)
				sent[id] = rtProcRecord{sig: procSig(proc), status: status, ws: wsID}
			}
		case "removed":
			delete(sent, id)
		}
		return emit(msg)
	}
	queue := make(chan map[string]any, 1024)
	var dropped atomic.Int64
	enqueue := func(msg map[string]any) {
		select {
		case queue <- msg:
		default:
			dropped.Add(1)
		}
	}
	var mu sync.Mutex
	stopped := false
	watched := map[string]*rtWatched{}
	defer func() {
		mu.Lock()
		stopped = true
		for _, w := range watched {
			w.off()
		}
		watched = map[string]*rtWatched{}
		mu.Unlock()
	}()
	watch := func(pr *corePR) {
		rt := pr.LoadedRuntime()
		if rt == nil {
			return
		}
		wsID := pr.WorkspaceID()
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		if cur := watched[wsID]; cur != nil {
			if cur.rt == rt {
				mu.Unlock()
				return
			}
			cur.off()
		}
		ch, off := rt.Manager().Events().Subscribe()
		watched[wsID] = &rtWatched{pr: pr, rt: rt, off: off}
		mu.Unlock()
		go func() {
			for _, proc := range rt.Manager().List() {
				if st, err := rt.Status(proc.ID); err == nil {
					enqueue(map[string]any{"kind": "upsert", "processId": proc.ID, "process": s.processInfo(pr, st)})
				}
			}
			for ev := range ch {
				if ev.Type == events.Removed {
					enqueue(map[string]any{"kind": "removed", "processId": ev.ProcessID})
					continue
				}
				if ev.Type == events.Stdout || ev.Type == events.Stderr {
					continue
				}
				st, err := rt.Status(ev.ProcessID)
				if err != nil {
					continue
				}
				enqueue(map[string]any{"kind": "upsert", "event": string(ev.Type), "processId": ev.ProcessID, "process": s.processInfo(pr, st)})
			}
		}()
	}
	discover := func() {
		if all {
			s.engine.RangeRuntimes(func(pr *corePR) bool {
				watch(pr)
				return true
			})
			return
		}
		if pr, err := s.engine.GetOrCreateRuntime(ws); err == nil {
			if _, err := pr.Runtime(); err == nil {
				watch(pr)
			}
		}
	}
	discover()
	loadCh, unload := s.engine.SubscribeRuntimeLoad()
	defer unload()
	go func() {
		for wsID := range loadCh {
			if !all && wsID != ws {
				continue
			}
			if pr, err := s.engine.GetOrCreateRuntime(wsID); err == nil {
				watch(pr)
			}
		}
	}()
	reconcile := func() error {
		discover()
		mu.Lock()
		current := make(map[string]*rtWatched, len(watched))
		for wsID, w := range watched {
			if w.pr.LoadedRuntime() != w.rt {
				w.off()
				delete(watched, wsID)
				continue
			}
			current[wsID] = w
		}
		mu.Unlock()
		for wsID, w := range current {
			present := map[string]bool{}
			for _, proc := range w.rt.Manager().List() {
				present[proc.ID] = true
				st, err := w.rt.Status(proc.ID)
				if err != nil {
					continue
				}
				prev, known := sent[proc.ID]
				if known && !liveStatus(st.Status) && prev.status == st.Status {
					continue
				}
				info := s.processInfo(w.pr, st)
				if known && procSig(info) == prev.sig {
					continue
				}
				if err := send(map[string]any{"kind": "upsert", "event": "reconcile", "processId": proc.ID, "process": info}); err != nil {
					return err
				}
			}
			for id, rec := range sent {
				if rec.ws == wsID && !present[id] {
					if err := send(map[string]any{"kind": "removed", "processId": id}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	coalesce := map[string]map[string]any{}
	flush := time.NewTicker(100 * time.Millisecond)
	defer flush.Stop()
	resync := time.NewTicker(5 * time.Second)
	defer resync.Stop()
	sendCoalesced := func() error {
		for _, m := range coalesce {
			if err := send(m); err != nil {
				return err
			}
		}
		coalesce = map[string]map[string]any{}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-queue:
			if msg["kind"] == "upsert" {
				key, _ := msg["processId"].(string)
				coalesce[key] = msg
				continue
			}
			if err := sendCoalesced(); err != nil {
				return err
			}
			if err := send(msg); err != nil {
				return err
			}
		case <-flush.C:
			if n := dropped.Swap(0); n > 0 {
				if err := emit(gapMessage(int(n), "slow consumer")); err != nil {
					return err
				}
			}
			if err := sendCoalesced(); err != nil {
				return err
			}
		case <-resync.C:
			if err := sendCoalesced(); err != nil {
				return err
			}
			if err := reconcile(); err != nil {
				return err
			}
		}
	}
}

func (s *Server) topicEvents(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	ws := p.str("workspaceId")
	types := p.strs("types")
	procs := p.strs("processIds")
	since := p.int64("since")
	tail := p.integer("tail")
	watcher := s.engine.Store().Watch()
	defer watcher.Close()
	last := since
	if since == 0 && tail > 0 {
		rows, err := s.engine.Store().QueryEvents(store.EventQuery{Workspace: ws, Types: types, Limit: tail, Desc: true})
		if err != nil {
			return err
		}
		for i := len(rows) - 1; i >= 0; i-- {
			e := rows[i]
			if !eventAllowed(e.Type, e.ProcessID, types, procs) {
				continue
			}
			if err := emit(eventFrame(e)); err != nil {
				return err
			}
			if e.ID > last {
				last = e.ID
			}
		}
	} else if since > 0 {
		rows, err := s.engine.Store().QueryEvents(store.EventQuery{Workspace: ws, Since: since, Types: types, Limit: 2000})
		if err != nil {
			return err
		}
		for _, e := range rows {
			if !eventAllowed(e.Type, e.ProcessID, types, procs) {
				continue
			}
			if err := emit(eventFrame(e)); err != nil {
				return err
			}
			if e.ID > last {
				last = e.ID
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case c, ok := <-watcher.C:
			if !ok {
				return nil
			}
			if c.Kind != "event" || c.Event == nil || c.Event.ID <= last {
				continue
			}
			e := c.Event
			if ws != "" && e.WorkspaceID != ws {
				continue
			}
			if !eventAllowed(e.Type, e.ProcessID, types, procs) {
				continue
			}
			last = e.ID
			if err := emit(eventFrame(e)); err != nil {
				return err
			}
		}
	}
}

func eventFrame(e *store.EventRow) map[string]any {
	return map[string]any{
		"kind": "event", "id": e.ID, "ts": e.TS, "type": e.Type,
		"projectId": e.ProjectID, "workspaceId": e.WorkspaceID,
		"processId": e.ProcessID, "instanceId": e.InstanceID,
		"sessionId": e.SessionID, "payload": eventPayload(e.Payload),
		"cursor": fmt.Sprint(e.ID),
	}
}

func (s *Server) topicAudit(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	ws := p.str("workspaceId")
	last := p.int64("since")
	watcher := s.engine.Store().Watch()
	defer watcher.Close()
	if last > 0 {
		rows, err := s.engine.Store().ListAuditAfter(ws, last, 500)
		if err != nil {
			return err
		}
		for _, a := range rows {
			if err := emit(auditFrame(a)); err != nil {
				return err
			}
			if a.ID > last {
				last = a.ID
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case c, ok := <-watcher.C:
			if !ok {
				return nil
			}
			if c.Kind != "audit" || c.Audit == nil || c.Audit.ID <= last {
				continue
			}
			if ws != "" && c.Audit.WorkspaceID != ws {
				continue
			}
			last = c.Audit.ID
			if err := emit(auditFrame(c.Audit)); err != nil {
				return err
			}
		}
	}
}

func auditFrame(a *store.AuditRow) map[string]any {
	return map[string]any{
		"kind": "entry", "id": a.ID, "ts": a.TS, "sessionId": a.SessionID,
		"harness": a.Harness, "projectId": a.ProjectID, "workspaceId": a.WorkspaceID,
		"action": a.Action, "args": a.Args, "result": a.Result, "durationMs": a.DurationMs,
		"cursor": fmt.Sprint(a.ID),
	}
}

func (s *Server) topicSessions(ctx context.Context, _ rtParams, emit func(map[string]any) error) error {
	snapshot := make([]any, 0)
	for _, se := range s.engine.Sessions().List() {
		snapshot = append(snapshot, sessionJSON(se))
	}
	if err := emit(map[string]any{"kind": "snapshot", "sessions": snapshot}); err != nil {
		return err
	}
	watcher := s.engine.Store().Watch()
	defer watcher.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case c, ok := <-watcher.C:
			if !ok {
				return nil
			}
			if c.Kind != "session" || c.Key == "" {
				continue
			}
			if c.Op == "closed" {
				if err := emit(map[string]any{"kind": "closed", "sessionId": c.Key}); err != nil {
					return err
				}
				continue
			}
			se, ok := s.engine.Sessions().Get(c.Key)
			if !ok {
				continue
			}
			if err := emit(map[string]any{"kind": "upsert", "session": sessionJSON(se)}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) topicConfig(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	projectID := p.str("projectId")
	if projectID == "" {
		return fmt.Errorf("invalid_argument: projectId required")
	}
	last := p.int64("since")
	emitRevisions := func() error {
		revs, err := s.engine.Store().ListRevisions(projectID, "", 50)
		if err != nil {
			return err
		}
		for i := len(revs) - 1; i >= 0; i-- {
			rev := revs[i]
			if rev.ID <= last {
				continue
			}
			last = rev.ID
			kind := "revision"
			if !rev.Valid {
				kind = "invalid"
			}
			if err := emit(map[string]any{"kind": kind, "revision": revisionJSON(rev), "layer": rev.Layer, "errors": rev.Errors, "cursor": fmt.Sprint(rev.ID)}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := emitRevisions(); err != nil {
		return err
	}
	if err := emit(map[string]any{"kind": "proposal", "proposal": s.pendingProposal(projectID)}); err != nil {
		return err
	}
	watcher := s.engine.Store().Watch()
	defer watcher.Close()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case c, ok := <-watcher.C:
			if !ok {
				return nil
			}
			if c.ProjectID != projectID || (c.Kind != "project" && c.Kind != "config") {
				continue
			}
			if err := emitRevisions(); err != nil {
				return err
			}
			if c.Kind == "config" {
				if err := emit(map[string]any{"kind": "proposal", "proposal": s.pendingProposal(projectID)}); err != nil {
					return err
				}
			}
		case <-tick.C:
			if err := emit(map[string]any{"kind": "proposal", "proposal": s.pendingProposal(projectID)}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) topicResources(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	ids := p.strs("processIds")
	interval := p.integer("intervalMs")
	if interval < 1000 {
		interval = 2000
	}
	t := time.NewTicker(time.Duration(interval) * time.Millisecond)
	defer t.Stop()
	sample := func() error {
		for _, id := range ids {
			_, rt, _, err := s.findProcess(id)
			if err != nil {
				continue
			}
			proc, ok := rt.Manager().Get(id)
			if !ok {
				continue
			}
			mem, cpu := proc.LiveUsage()
			pid := 0
			if st, err := rt.Status(id); err == nil {
				pid = st.PID
			}
			ports := []int{}
			if pid > 0 {
				if found := process.ListeningPortsForPID(pid); found != nil {
					ports = found
				}
			}
			msg := usagePayload(id, pid, mem, cpu, ports)
			msg["kind"] = "sample"
			if err := emit(msg); err != nil {
				return err
			}
		}
		return nil
	}
	if err := sample(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := sample(); err != nil {
				return err
			}
		}
	}
}

func (s *Server) topicLogs(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	ws := p.str("workspaceId")
	ids := p.strs("processIds")
	backlog := p.integer("backlog")
	if backlog <= 0 {
		backlog = 500
	}
	resume := p.uint64Map("resume")
	contains := p.str("contains")
	if len(ids) == 0 && ws != "" {
		if pr, err := s.engine.GetOrCreateRuntime(ws); err == nil {
			if rt, err := pr.Runtime(); err == nil {
				if l, err := rt.List(); err == nil {
					for _, proc := range l.Processes {
						ids = append(ids, proc.ProcessID)
					}
				}
			}
		}
	}
	if len(ids) == 0 && ws == "" {
		s.engine.RangeRuntimes(func(pr *corePR) bool {
			if rt, err := pr.Runtime(); err == nil {
				if l, err := rt.List(); err == nil {
					for _, proc := range l.Processes {
						ids = append(ids, proc.ProcessID)
					}
				}
			}
			return true
		})
	}
	red := s.redactorForWorkspace(ws)
	type sub struct {
		off    func()
		id     string
		instID string
		wake   <-chan struct{}
	}
	var subs []sub
	lastIDs := map[string]uint64{}
	fanIn := make(chan string, 1024)
	for _, id := range ids {
		_, rt, _, err := s.findProcess(id)
		if err != nil {
			continue
		}
		proc, ok := rt.Manager().Get(id)
		if !ok {
			continue
		}
		subID, wake := proc.SubscribeLogs()
		proc2 := proc
		pid := id
		go func() {
			for range wake {
				select {
				case fanIn <- pid:
				default:
				}
			}
		}()
		subs = append(subs, sub{off: func() { proc2.UnsubscribeLogs(subID) }, id: id, instID: proc.InstanceID(), wake: wake})
		lastIDs[id] = proc.Logs.NextID()
		if rv, ok := resume[id]; ok {
			oldest := proc.Logs.OldestID()
			if rv < oldest && oldest > 0 {
				_ = emit(map[string]any{"kind": "gap", "processId": id, "expected": rv, "oldest": oldest})
			}
			lastIDs[id] = rv
		} else {
			res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: backlog})
			if err != nil {
				continue
			}
			lines := make([]any, 0, len(res.Entries))
			for _, e := range res.Entries {
				line := e.Line
				if contains != "" && !strings.Contains(e.Line, contains) {
					continue
				}
				if red != nil {
					line = red.Redact(line)
				}
				lines = append(lines, map[string]any{
					"processId": id, "stream": e.Stream, "line": line,
					"timestamp": e.Timestamp, "id": e.ID, "level": logs.ParseLevel(line),
					"instanceId": proc.InstanceID(), "cursor": id + ":" + fmt.Sprint(e.ID),
				})
				if e.ID+1 > lastIDs[id] {
					lastIDs[id] = e.ID + 1
				}
			}
			if err := emit(map[string]any{"kind": "batch", "processId": id, "lines": lines}); err != nil {
				return err
			}
		}
	}
	defer func() {
		for _, sb := range subs {
			sb.off()
		}
	}()
	byID := map[string]*sub{}
	for i := range subs {
		byID[subs[i].id] = &subs[i]
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case pid := <-fanIn:
			sb, ok := byID[pid]
			if !ok {
				continue
			}
			_, rt, _, err := s.findProcess(sb.id)
			if err != nil {
				continue
			}
			proc, ok := rt.Manager().Get(sb.id)
			if !ok {
				continue
			}
			cur := proc.InstanceID()
			if cur != sb.instID {
				sb.instID = cur
				if err := emit(map[string]any{"kind": "instance", "processId": sb.id, "instanceId": cur}); err != nil {
					return err
				}
			}
			for _, e := range proc.Logs.From(lastIDs[sb.id]) {
				lastIDs[sb.id] = e.ID + 1
				if contains != "" && !strings.Contains(e.Line, contains) {
					continue
				}
				line := e.Line
				if red != nil {
					line = red.Redact(line)
				}
				if err := emit(map[string]any{"kind": "line",
					"processId": sb.id, "stream": string(e.Stream), "line": line,
					"id": e.ID, "timestamp": e.Timestamp.Format(time.RFC3339Nano),
					"level": logs.ParseLevel(line), "instanceId": cur,
					"cursor": sb.id + ":" + fmt.Sprint(e.ID)}); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) topicAttach(ctx context.Context, p rtParams, emit func(map[string]any) error) error {
	id := p.str("processId")
	backlog := p.integer("backlog")
	if backlog <= 0 {
		backlog = 500
	}
	_, rt, _, err := s.findProcess(id)
	if err != nil {
		return err
	}
	proc, ok := rt.Manager().Get(id)
	if !ok {
		return fmt.Errorf("unknown process %q", id)
	}
	subID, wake := proc.SubscribeLogs()
	defer proc.UnsubscribeLogs(subID)
	lastID := proc.Logs.NextID()
	res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: backlog})
	if err != nil {
		return err
	}
	for _, e := range res.Entries {
		if e.ID+1 > lastID {
			lastID = e.ID + 1
		}
	}
	if err := emit(map[string]any{"kind": "batch", "lines": attachBatchLines(res.Entries)}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
			for _, e := range proc.Logs.From(lastID) {
				lastID = e.ID + 1
				if err := emit(map[string]any{"kind": "output", "data": e.Line,
					"stream": string(e.Stream), "id": e.ID,
					"timestamp": e.Timestamp.Format(time.RFC3339Nano),
					"cursor":    id + ":" + fmt.Sprint(e.ID)}); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) rtStreamCounts() map[string]any {
	n := s.streamCount.Load()
	return map[string]any{"total": n, "byRoute": map[string]int64{"RealtimeService/Connect": n}}
}
