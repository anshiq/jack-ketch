package core

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"jack-ketch/internal/config"
	"jack-ketch/internal/events"
	"jack-ketch/internal/process"
)

const maxWatchedDirs = 4096

var defaultIgnoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, ".jack-ketch": true,
	"dist": true, "build": true, "target": true, "out": true, "bin": true, "obj": true,
	"__pycache__": true, ".venv": true, "venv": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	".ruff_cache": true, "vendor": true, ".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true,
	".cache": true, "coverage": true, ".idea": true, ".vscode": true, ".gradle": true, ".parcel-cache": true,
	".angular": true, ".expo": true, ".dart_tool": true, "tmp": true,
}

var defaultIgnoredFiles = []string{
	"*.log", "*.swp", "*.swo", "*.swx", "*~", ".#*", "#*#", "*.tmp", "*.temp", ".DS_Store", "4913",
	"*.pid", "*.lock", "*.sqlite-journal", "*.db-journal", "*.db-wal", "*.db-shm", "*.pyc",
}

type appWatcher struct {
	app      string
	key      string
	base     string
	ignore   []string
	debounce time.Duration
	fsw      *fsnotify.Watcher
	dirs     map[string]bool
	files    map[string]bool
	trees    []string
	cancel   context.CancelFunc
	done     chan struct{}
}

type watchSet struct {
	mu     sync.Mutex
	apps   map[string]*appWatcher
	closed bool
}

func (p *ProjectRuntime) appBase(app config.V3App) string {
	if app.WorkDir == "" {
		return p.wsPath
	}
	if filepath.IsAbs(app.WorkDir) {
		return app.WorkDir
	}
	return filepath.Join(p.wsPath, app.WorkDir)
}

func watchKey(base string, w *config.WatchConfig) string {
	b, _ := json.Marshal(struct {
		Base     string
		Paths    []string
		Ignore   []string
		Debounce time.Duration
	}{base, w.Paths, w.Ignore, w.DebounceOrDefault()})
	return string(b)
}

func (p *ProjectRuntime) syncWatchers() {
	res := p.ResolvedConfig()
	want := map[string]config.V3App{}
	if res != nil {
		for name, app := range res.Apps {
			if app.Watch.Enabled() {
				want[name] = app
			}
		}
	}
	p.watch.mu.Lock()
	defer p.watch.mu.Unlock()
	if p.watch.closed {
		return
	}
	if p.watch.apps == nil {
		p.watch.apps = map[string]*appWatcher{}
	}
	for name, w := range p.watch.apps {
		app, ok := want[name]
		if ok && w.key == watchKey(p.appBase(app), app.Watch) {
			continue
		}
		w.stop()
		delete(p.watch.apps, name)
	}
	for name, app := range want {
		if _, ok := p.watch.apps[name]; ok {
			continue
		}
		w, err := p.newAppWatcher(name, app)
		if err != nil {
			p.logger.Warn("file watch disabled", "app", name, "error", err)
			continue
		}
		p.watch.apps[name] = w
	}
}

func (p *ProjectRuntime) openWatchers() {
	p.watch.mu.Lock()
	p.watch.closed = false
	p.watch.mu.Unlock()
}

func (p *ProjectRuntime) stopWatchers() {
	p.watch.mu.Lock()
	defer p.watch.mu.Unlock()
	p.watch.closed = true
	for name, w := range p.watch.apps {
		w.stop()
		delete(p.watch.apps, name)
	}
}

func (p *ProjectRuntime) WatchedApps() map[string][]string {
	p.watch.mu.Lock()
	defer p.watch.mu.Unlock()
	out := map[string][]string{}
	for name, w := range p.watch.apps {
		var dirs []string
		for d := range w.dirs {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		out[name] = dirs
	}
	return out
}

func (p *ProjectRuntime) newAppWatcher(name string, app config.V3App) (*appWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	base := p.appBase(app)
	ctx, cancel := context.WithCancel(context.Background())
	w := &appWatcher{
		app:      name,
		key:      watchKey(base, app.Watch),
		base:     base,
		ignore:   app.Watch.Ignore,
		debounce: app.Watch.DebounceOrDefault(),
		fsw:      fsw,
		dirs:     map[string]bool{},
		files:    map[string]bool{},
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	for _, rel := range app.Watch.Paths {
		target := filepath.Join(base, rel)
		st, err := os.Stat(target)
		if err != nil {
			p.logger.Warn("watch path not found", "app", name, "path", target)
			continue
		}
		if st.IsDir() {
			w.trees = append(w.trees, target)
			w.addTree(target)
			continue
		}
		w.files[target] = true
		w.addDir(filepath.Dir(target))
	}
	if len(w.dirs) == 0 {
		cancel()
		_ = fsw.Close()
		return nil, os.ErrNotExist
	}
	go w.loop(ctx, func(changed []string) { p.onWatchFire(name, changed) })
	return w, nil
}

func (w *appWatcher) stop() {
	w.cancel()
	_ = w.fsw.Close()
	<-w.done
}

func (w *appWatcher) addDir(dir string) {
	if w.dirs[dir] || len(w.dirs) >= maxWatchedDirs {
		return
	}
	if err := w.fsw.Add(dir); err == nil {
		w.dirs[dir] = true
	}
}

func (w *appWatcher) addTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && w.ignoredDir(path) {
			return fs.SkipDir
		}
		if len(w.dirs) >= maxWatchedDirs {
			return fs.SkipAll
		}
		w.addDir(path)
		return nil
	})
}

func (w *appWatcher) rel(path string) string {
	if r, err := filepath.Rel(w.base, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

func (w *appWatcher) ignoredDir(path string) bool {
	if defaultIgnoredDirs[filepath.Base(path)] {
		return true
	}
	return w.userIgnored(path)
}

func (w *appWatcher) userIgnored(path string) bool {
	rel := w.rel(path)
	base := filepath.Base(path)
	for _, g := range w.ignore {
		g = strings.TrimPrefix(filepath.ToSlash(g), "./")
		if strings.Contains(g, "/") {
			g = strings.TrimPrefix(g, "**/")
			if ok, _ := filepath.Match(g, rel); ok {
				return true
			}
			if strings.HasSuffix(g, "/**") && (rel == strings.TrimSuffix(g, "/**") || strings.HasPrefix(rel, strings.TrimSuffix(g, "**"))) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(g, base); ok {
			return true
		}
	}
	return false
}

func (w *appWatcher) underTree(path string) bool {
	for _, t := range w.trees {
		if path == t || strings.HasPrefix(path, t+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (w *appWatcher) relevant(ev fsnotify.Event) bool {
	if ev.Op == fsnotify.Chmod {
		return false
	}
	path := ev.Name
	if w.files[path] {
		return true
	}
	if !w.underTree(path) {
		return false
	}
	for d := filepath.Dir(path); w.underTree(d); d = filepath.Dir(d) {
		if defaultIgnoredDirs[filepath.Base(d)] && !w.isTree(d) {
			return false
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	base := filepath.Base(path)
	for _, g := range defaultIgnoredFiles {
		if ok, _ := filepath.Match(g, base); ok {
			return false
		}
	}
	return !w.userIgnored(path)
}

func (w *appWatcher) isTree(dir string) bool {
	for _, t := range w.trees {
		if t == dir {
			return true
		}
	}
	return false
}

func (w *appWatcher) loop(ctx context.Context, fire func([]string)) {
	defer close(w.done)
	var timer *time.Timer
	var timerC <-chan time.Time
	pending := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Create) {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					if w.underTree(ev.Name) && !w.ignoredDir(ev.Name) {
						w.addTree(ev.Name)
					}
					continue
				}
			}
			if !w.relevant(ev) {
				continue
			}
			pending[w.rel(ev.Name)] = true
			if timer == nil {
				timer = time.NewTimer(w.debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
		case <-timerC:
			timer, timerC = nil, nil
			changed := make([]string, 0, len(pending))
			for f := range pending {
				changed = append(changed, f)
			}
			sort.Strings(changed)
			pending = map[string]bool{}
			fire(changed)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (p *ProjectRuntime) onWatchFire(app string, changed []string) {
	n := p.restartApp(app, true, "watch", map[string]any{"changed": limitStrings(changed, 20), "changedCount": len(changed)})
	if n > 0 {
		p.logger.Info("restarted after file change", "app", app, "processes", n, "changed", len(changed))
	}
}

func limitStrings(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func (p *ProjectRuntime) restartApp(app string, includeExited bool, reason string, extra map[string]any) int {
	rt := p.LoadedRuntime()
	if rt == nil {
		return 0
	}
	list, err := rt.List()
	if err != nil {
		return 0
	}
	n := 0
	for _, ps := range list.Processes {
		if ps.App != app {
			continue
		}
		switch process.Status(ps.Status) {
		case process.StatusRunning, process.StatusStarting:
		case process.StatusExited, process.StatusFailed, process.StatusCrashed:
			if !includeExited {
				continue
			}
		default:
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := rt.Restart(ctx, ps.ProcessID)
		cancel()
		payload := map[string]any{"app": app, "reason": reason}
		for k, v := range extra {
			payload[k] = v
		}
		if err != nil {
			payload["error"] = err.Error()
			p.logger.Warn("automatic restart failed", "app", app, "process_id", ps.ProcessID, "reason", reason, "error", err)
		} else {
			n++
		}
		p.emit("process.auto_restarted", ps.ProcessID, payload)
		rt.RecordAudit("auto_restart", map[string]any{"process_id": ps.ProcessID, "app": app, "reason": reason}, errString(err), "daemon")
	}
	if n > 0 {
		p.mu.Lock()
		delete(p.stale, app)
		p.mu.Unlock()
	}
	return n
}

func errString(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func (p *ProjectRuntime) emit(typ, processID string, payload map[string]any) {
	if bus := p.ManagerBus(); bus != nil {
		bus.Publish(events.Event{Type: events.Type(typ), ProcessID: processID, Timestamp: time.Now(), Payload: payload})
		return
	}
	if p.store != nil {
		b, _ := json.Marshal(payload)
		_, _ = p.store.AppendEvent(time.Now().UnixNano(), typ, p.projectID, p.wsID, processID, "", "", string(b))
	}
}

func (p *ProjectRuntime) ClearStale(app string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.stale, app)
}
