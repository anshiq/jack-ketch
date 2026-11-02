package store

import (
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
)

type Change struct {
	Kind        string
	Op          string
	ID          int64
	Key         string
	ProjectID   string
	WorkspaceID string
	Event       *EventRow
	Audit       *AuditRow
}

type Watcher struct {
	C       <-chan Change
	ch      chan Change
	dropped atomic.Int64
	hub     *changeHub
	once    sync.Once
}

type changeHub struct {
	mu   sync.RWMutex
	subs map[*Watcher]struct{}
}

func newChangeHub() *changeHub {
	return &changeHub{subs: map[*Watcher]struct{}{}}
}

func (h *changeHub) publish(c Change) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for w := range h.subs {
		select {
		case w.ch <- c:
		default:
			w.dropped.Add(1)
		}
	}
}

func (h *changeHub) count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

func (d *DB) Watch() *Watcher {
	ch := make(chan Change, 1024)
	w := &Watcher{C: ch, ch: ch, hub: d.hub}
	d.hub.mu.Lock()
	d.hub.subs[w] = struct{}{}
	d.hub.mu.Unlock()
	return w
}

func (d *DB) Watchers() int { return d.hub.count() }

func (w *Watcher) Dropped() int64 { return w.dropped.Swap(0) }

func (w *Watcher) Close() {
	w.once.Do(func() {
		w.hub.mu.Lock()
		delete(w.hub.subs, w)
		close(w.ch)
		w.hub.mu.Unlock()
	})
}

func (d *DB) publish(c Change) {
	if d.hub != nil {
		d.hub.publish(c)
	}
}

func (d *DB) publishProject(projectID string) {
	d.publish(Change{Kind: "project", ProjectID: projectID})
}

func (d *DB) publishSession(id, op string) {
	d.publish(Change{Kind: "session", Op: op, Key: id})
}

type EventQuery struct {
	Workspace string
	Process   string
	Since     int64
	Before    int64
	Types     []string
	Limit     int
	Desc      bool
}

func (d *DB) QueryEvents(q EventQuery) ([]*EventRow, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	sqlText := `SELECT id, ts, type, project_id, workspace_id, process_id, instance_id, session_id, payload_json
		FROM events WHERE id>?`
	args := []any{q.Since}
	if q.Before > 0 {
		sqlText += ` AND id<?`
		args = append(args, q.Before)
	}
	if q.Workspace != "" {
		sqlText += ` AND workspace_id=?`
		args = append(args, q.Workspace)
	}
	if q.Process != "" {
		sqlText += ` AND process_id=?`
		args = append(args, q.Process)
	}
	if len(q.Types) > 0 {
		sqlText += ` AND type IN (?` + strings.Repeat(",?", len(q.Types)-1) + `)`
		for _, t := range q.Types {
			args = append(args, t)
		}
	}
	if q.Desc {
		sqlText += ` ORDER BY id DESC LIMIT ?`
	} else {
		sqlText += ` ORDER BY id ASC LIMIT ?`
	}
	args = append(args, limit)
	rows, err := d.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRow
	for rows.Next() {
		var r EventRow
		var proj, ws, proc, inst, sess, payload sql.NullString
		if err := rows.Scan(&r.ID, &r.TS, &r.Type, &proj, &ws, &proc, &inst, &sess, &payload); err != nil {
			return nil, err
		}
		r.ProjectID, r.WorkspaceID, r.ProcessID = proj.String, ws.String, proc.String
		r.InstanceID, r.SessionID, r.Payload = inst.String, sess.String, payload.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (d *DB) LatestEventID() (int64, error) {
	var id sql.NullInt64
	if err := d.db.QueryRow(`SELECT MAX(id) FROM events`).Scan(&id); err != nil {
		return 0, err
	}
	return id.Int64, nil
}

func (d *DB) ListAuditAfter(workspace string, after int64, limit int) ([]*AuditRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	q := `SELECT id, ts, session_id, harness, project_id, workspace_id, action, args_json, result, duration_ms
		FROM audit WHERE id>?`
	args := []any{after}
	if workspace != "" {
		q += ` AND workspace_id=?`
		args = append(args, workspace)
	}
	q += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditRow
	for rows.Next() {
		var r AuditRow
		var sess, harness, proj, ws, argsJ sql.NullString
		if err := rows.Scan(&r.ID, &r.TS, &sess, &harness, &proj, &ws, &r.Action, &argsJ, &r.Result, &r.DurationMs); err != nil {
			return nil, err
		}
		r.SessionID, r.Harness, r.ProjectID = sess.String, harness.String, proj.String
		r.WorkspaceID, r.Args = ws.String, argsJ.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (d *DB) LatestAuditID() (int64, error) {
	var id sql.NullInt64
	if err := d.db.QueryRow(`SELECT MAX(id) FROM audit`).Scan(&id); err != nil {
		return 0, err
	}
	return id.Int64, nil
}

const migration3 = `
CREATE INDEX IF NOT EXISTS ev_ts_type ON events(ts, type);
DELETE FROM events WHERE id IN (
  SELECT e.id FROM events e
  WHERE EXISTS (
    SELECT 1 FROM events o
    WHERE o.ts = e.ts AND o.type = e.type
      AND COALESCE(o.process_id, '') = COALESCE(e.process_id, '')
      AND COALESCE(o.instance_id, '') = COALESCE(e.instance_id, '')
      AND o.id <> e.id
      AND (
        (o.workspace_id IS NOT NULL AND e.workspace_id IS NULL)
        OR ((o.workspace_id IS NULL) = (e.workspace_id IS NULL) AND o.id < e.id)
      )
  )
);
`

const migration4 = `
CREATE TABLE IF NOT EXISTS credential_profiles (
  id             TEXT PRIMARY KEY,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  environment    TEXT NOT NULL DEFAULT '',
  is_default     INTEGER NOT NULL DEFAULT 0,
  apps_json      TEXT NOT NULL DEFAULT '[]',
  fields_json    TEXT NOT NULL DEFAULT '[]',
  recipe_json    TEXT NOT NULL DEFAULT '',
  jar_json       TEXT NOT NULL DEFAULT '',
  capture_jar    INTEGER NOT NULL DEFAULT 1,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  last_used_at   INTEGER,
  last_refresh_at INTEGER,
  last_error     TEXT NOT NULL DEFAULT '',
  UNIQUE (project_id, name)
);
CREATE INDEX IF NOT EXISTS cred_project ON credential_profiles(project_id);
`

const migration5 = `
INSERT INTO project_configs (project_id, layer, workspace_id, content, sha256, active_rev, updated_at)
SELECT c.project_id, 'project', '', c.content, c.sha256, c.active_rev, c.updated_at
FROM project_configs c
WHERE c.layer = 'project' AND c.workspace_id <> ''
  AND c.rowid = (
    SELECT c2.rowid FROM project_configs c2
    WHERE c2.project_id = c.project_id AND c2.layer = 'project' AND c2.workspace_id <> ''
    ORDER BY COALESCE(c2.active_rev, 0) DESC, c2.updated_at DESC, c2.rowid DESC
    LIMIT 1
  )
ON CONFLICT(project_id, layer, workspace_id) DO UPDATE SET
  content = excluded.content,
  sha256 = excluded.sha256,
  active_rev = excluded.active_rev,
  updated_at = excluded.updated_at
WHERE COALESCE(excluded.active_rev, 0) >= COALESCE(project_configs.active_rev, 0);

DELETE FROM project_configs WHERE layer = 'project' AND workspace_id <> '';
`
