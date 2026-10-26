// EventService: WatchEvents (stream with since cursor) + ListEvents
// (paged history from DB) + MCP-compat Subscribe/Drain/Unsubscribe.
// SessionService: Register, Heartbeat, ListSessions, GetSession, Close.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"jack-ketch/internal/session"
	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

// --- EventService ---

func (s *Server) routeEvent(mux *http.ServeMux) {
	p := "/jackketch.v1.EventService/"
	mux.HandleFunc(p+"WatchEvents", s.eventWatch)
	mux.HandleFunc(p+"ListEvents", s.wrap(s.eventList))
	mux.HandleFunc(p+"Subscribe", s.wrap(s.eventSubscribe))
	mux.HandleFunc(p+"Drain", s.wrap(s.eventDrain))
	mux.HandleFunc(p+"Unsubscribe", s.wrap(s.eventUnsubscribe))
}

func (s *Server) eventList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		ProcessID   string   `json:"processId"`
		Since       string   `json:"since"`
		Limit       int      `json:"limit"`
		Types       []string `json:"types"`
		Before      string   `json:"before"`
		Order       string   `json:"order"`
	}
	_ = decode(r, &req)
	var since, before int64
	if req.Since != "" {
		since, _ = strconv.ParseInt(req.Since, 10, 64)
	}
	if req.Before != "" {
		before, _ = strconv.ParseInt(req.Before, 10, 64)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	desc := req.Order == "desc"
	rows, err := s.engine.Store().QueryEvents(store.EventQuery{
		Workspace: req.WorkspaceID, Process: req.ProcessID, Since: since,
		Before: before, Types: req.Types, Limit: limit + 1, Desc: desc,
	})
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]any, 0, len(rows))
	var nextBefore string
	for _, e := range rows {
		out = append(out, eventJSON(e))
		nextBefore = fmt.Sprint(e.ID)
	}
	latest, _ := s.engine.Store().LatestEventID()
	return map[string]any{"events": out, "nextBefore": nextBefore, "latestId": latest, "hasMore": hasMore}, nil
}

func eventJSON(e *store.EventRow) map[string]any {
	return map[string]any{
		"id": e.ID, "ts": e.TS, "type": e.Type,
		"projectId": e.ProjectID, "workspaceId": e.WorkspaceID,
		"processId": e.ProcessID, "instanceId": e.InstanceID,
		"sessionId": e.SessionID, "payload": eventPayload(e.Payload),
		"cursor": fmt.Sprint(e.ID),
	}
}

func eventPayload(raw string) any {
	if raw == "" {
		return map[string]any{}
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) == nil {
		return v
	}
	return map[string]any{"message": raw}
}

func (s *Server) eventWatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		ProcessIDs  []string `json:"processIds"`
		Types       []string `json:"types"`
		Since       string   `json:"since"`
		Tail        int      `json:"tail"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	var since int64
	if req.Since != "" {
		since, _ = strconv.ParseInt(req.Since, 10, 64)
	}
	params := rtParams{
		"workspaceId": req.WorkspaceID, "processIds": req.ProcessIDs,
		"types": req.Types, "since": since, "tail": req.Tail,
	}
	if err := s.topicEvents(r.Context(), params, sw.sendMap); err != nil {
		return
	}
}

func eventAllowed(typ, proc string, types, procs []string) bool {
	if len(types) > 0 {
		ok := false
		for _, t := range types {
			if t == typ {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(procs) > 0 {
		for _, p := range procs {
			if p == proc {
				return true
			}
		}
		return false
	}
	return true
}

func (s *Server) eventSubscribe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID   string   `json:"sessionId"`
		WorkspaceID string   `json:"workspaceId"`
		Types       []string `json:"types"`
		ProcessID   string   `json:"processId"`
		Since       string   `json:"since"`
	}
	_ = decode(r, &req)
	if req.SessionID == "" {
		req.SessionID, _ = s.sessionOf(r)
	}
	ws := req.WorkspaceID
	if ws == "" && req.SessionID != "" {
		if se, ok := s.engine.Sessions().Get(req.SessionID); ok {
			ws = se.WorkspaceID
		}
	}
	if ws == "" && req.ProcessID != "" {
		if _, _, wsID, err := s.findProcess(req.ProcessID); err == nil {
			ws = wsID
		}
	}
	if ws == "" {
		return nil, fmt.Errorf("invalid_argument: workspaceId required")
	}
	client := req.SessionID
	if client == "" {
		client = "anonymous"
	}
	_, rt, err := s.engine.RuntimeFor(ws)
	if err != nil {
		return nil, err
	}
	res, err := rt.SubscribeEvents(client, api.SubscribeEventsRequest{ProcessID: req.ProcessID, Types: req.Types, Since: req.Since})
	if err != nil {
		return nil, err
	}
	s.engine.MCPSubscriptions().Bind(res.SubscriptionID, ws, client)
	return map[string]any{"subscription_id": res.SubscriptionID, "subscriptionId": res.SubscriptionID}, nil
}

func (s *Server) eventDrain(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SubscriptionID string `json:"subscriptionId"`
		Limit          int    `json:"limit"`
	}
	_ = decode(r, &req)
	b, ok := s.engine.MCPSubscriptions().Lookup(req.SubscriptionID)
	if !ok {
		return nil, fmt.Errorf("not_found: unknown subscription %q", req.SubscriptionID)
	}
	_, rt, err := s.engine.RuntimeFor(b.WorkspaceID)
	if err != nil {
		return nil, err
	}
	res, err := rt.GetEvents(api.GetEventsRequest{SubscriptionID: req.SubscriptionID, Limit: req.Limit})
	if err != nil {
		s.engine.MCPSubscriptions().Forget(req.SubscriptionID)
		return nil, err
	}
	return res, nil
}

func (s *Server) eventUnsubscribe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	_ = decode(r, &req)
	b, ok := s.engine.MCPSubscriptions().Lookup(req.SubscriptionID)
	if ok {
		if _, rt, err := s.engine.RuntimeFor(b.WorkspaceID); err == nil {
			_, _ = rt.UnsubscribeEvents(b.Client, req.SubscriptionID)
		}
		s.engine.MCPSubscriptions().Forget(req.SubscriptionID)
	}
	return map[string]any{"subscription_id": req.SubscriptionID, "subscriptionId": req.SubscriptionID, "unsubscribed": ok}, nil
}

// --- SessionService ---

func (s *Server) routeSession(mux *http.ServeMux) {
	p := "/jackketch.v1.SessionService/"
	mux.HandleFunc(p+"Register", s.wrap(s.sessRegister))
	mux.HandleFunc(p+"Update", s.wrap(s.sessUpdate))
	mux.HandleFunc(p+"Heartbeat", s.sessHeartbeat)
	mux.HandleFunc(p+"Ping", s.wrap(s.sessPing))
	mux.HandleFunc(p+"ListSessions", s.wrap(s.sessList))
	mux.HandleFunc(p+"GetSession", s.wrap(s.sessGet))
	mux.HandleFunc(p+"Close", s.wrap(s.sessClose))
}

// sessPing is the unary heartbeat used by the MCP bridge (cheap,
// no stream). Missing/closed sessions get a clear error so the bridge
// re-registers.
func (s *Server) sessPing(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	if err := s.engine.Sessions().Heartbeat(req.SessionID); err != nil {
		return nil, err
	}
	_ = s.engine.Store().HeartbeatSession(req.SessionID)
	return map[string]any{"ok": true}, nil
}

func (s *Server) sessRegister(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Kind           string `json:"kind"`
		Harness        string `json:"harness"`
		HarnessVersion string `json:"harnessVersion"`
		ClientName     string `json:"clientName"`
		ClientPID      int    `json:"clientPid"`
		PID            int    `json:"pid"`
		WorkspaceID    string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	if req.Kind == "" {
		req.Kind = "mcp"
	}
	if req.ClientPID == 0 {
		req.ClientPID = req.PID
	}
	sess, err := s.engine.Sessions().Register(
		sessionKind(req.Kind), sessionHarness(req.Harness), req.ClientPID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if req.HarnessVersion != "" || req.ClientName != "" {
		_ = s.engine.Sessions().Update(sess.ID, func(se *session.Session) {
			if req.HarnessVersion != "" {
				se.HarnessVersion = req.HarnessVersion
			}
			if req.ClientName != "" {
				se.ClientName = req.ClientName
			}
		})
	}
	_ = s.engine.Store().CreateSession(sess.ID, string(sess.Kind), string(sess.Harness),
		int64(sess.ClientPID), sess.WorkspaceID)
	_ = s.engine.Store().UpdateSession(sess.ID, string(sess.Harness), req.HarnessVersion, sess.WorkspaceID)
	return map[string]any{"sessionId": sess.ID}, nil
}

func (s *Server) sessUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID      string `json:"sessionId"`
		Harness        string `json:"harness"`
		HarnessVersion string `json:"harnessVersion"`
		ClientName     string `json:"clientName"`
		WorkspaceID    string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	if req.SessionID == "" {
		return nil, fmt.Errorf("invalid_argument: sessionId required")
	}
	if err := s.engine.Sessions().Update(req.SessionID, func(se *session.Session) {
		if req.Harness != "" {
			se.Harness = sessionHarness(req.Harness)
		}
		if req.HarnessVersion != "" {
			se.HarnessVersion = req.HarnessVersion
		}
		if req.ClientName != "" {
			se.ClientName = req.ClientName
		}
	}); err != nil {
		return nil, err
	}
	if req.WorkspaceID != "" {
		if err := s.engine.Sessions().SetWorkspace(req.SessionID, req.WorkspaceID); err != nil {
			return nil, err
		}
	}
	se, ok := s.engine.Sessions().Get(req.SessionID)
	if !ok {
		return nil, fmt.Errorf("not_found: session %q", req.SessionID)
	}
	_ = s.engine.Store().UpdateSession(se.ID, string(se.Harness), se.HarnessVersion, se.WorkspaceID)
	return map[string]any{"session": sessionJSON(se)}, nil
}

// sessHeartbeat holds the stream open for the session's lifetime: the
// client sends periodic lines, the server bumps last_seen. When the
// stream drops the session is marked closed (lease grace covers agent
// restarts before session-leased processes stop).
func (s *Server) sessHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	_ = s.engine.Sessions().Heartbeat(req.SessionID)
	_ = s.engine.Store().HeartbeatSession(req.SessionID)
	for {
		select {
		case <-r.Context().Done():
			// Grace timer for lifetime:session processes is enforced by
			// the reap loop; mark closed now.
			_ = s.engine.Sessions().Close(req.SessionID)
			_ = s.engine.Store().CloseSession(req.SessionID)
			s.engine.SessionClosed(req.SessionID)
			return
		case <-t.C:
			_ = s.engine.Sessions().Heartbeat(req.SessionID)
			_ = s.engine.Store().HeartbeatSession(req.SessionID)
			_ = sw.send(map[string]any{"ok": true})
		}
	}
}

func (s *Server) sessList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	var out []any
	for _, se := range s.engine.Sessions().List() {
		if req.WorkspaceID != "" && se.WorkspaceID != req.WorkspaceID {
			continue
		}
		out = append(out, sessionJSON(se))
	}
	if out == nil {
		out = []any{}
	}
	return map[string]any{"sessions": out}, nil
}

func (s *Server) sessGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	se, ok := s.engine.Sessions().Get(req.SessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %q", req.SessionID)
	}
	return map[string]any{"session": sessionJSON(se)}, nil
}

func (s *Server) sessClose(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	if err := s.engine.Sessions().Close(req.SessionID); err != nil {
		return nil, err
	}
	_ = s.engine.Store().CloseSession(req.SessionID)
	s.engine.SessionClosed(req.SessionID)
	return map[string]any{"closed": true}, nil
}
