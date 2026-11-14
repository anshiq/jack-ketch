package core

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"jack-ketch/internal/process"
)

type RunningProcess struct {
	ProcessID     string `json:"processId"`
	App           string `json:"app,omitempty"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	WorkspaceID   string `json:"workspaceId"`
	WorkspacePath string `json:"workspacePath"`
}

type UpdateStatus struct {
	Pending          bool             `json:"pending"`
	RunningVersion   string           `json:"runningVersion"`
	InstalledVersion string           `json:"installedVersion,omitempty"`
	Executable       string           `json:"executable,omitempty"`
	RunningProcesses int              `json:"runningProcesses"`
	Processes        []RunningProcess `json:"processes,omitempty"`
	Notice           string           `json:"notice,omitempty"`
}

type exeWatch struct {
	once      sync.Once
	mu        sync.Mutex
	path      string
	bootMod   time.Time
	bootSize  int64
	bootIno   uint64
	checked   time.Time
	lastMod   time.Time
	installed string
}

func (e *Engine) initExeWatch() {
	e.exe.once.Do(func() {
		p, err := os.Executable()
		if err != nil {
			return
		}
		e.exe.path = p
		if st, err := os.Stat(p); err == nil {
			e.exe.bootMod, e.exe.bootSize, e.exe.bootIno = st.ModTime(), st.Size(), fileIno(st)
		}
	})
}

func (e *Engine) RunningProcesses() []RunningProcess {
	var out []RunningProcess
	for _, pr := range e.LoadedRuntimes() {
		rt, err := pr.Runtime()
		if err != nil || rt == nil {
			continue
		}
		res, err := rt.List()
		if err != nil {
			continue
		}
		for _, p := range res.Processes {
			switch process.Status(p.Status) {
			case process.StatusStarting, process.StatusRunning, process.StatusStopping:
			default:
				continue
			}
			out = append(out, RunningProcess{
				ProcessID:     p.ProcessID,
				App:           p.App,
				Command:       p.Command,
				Status:        p.Status,
				WorkspaceID:   pr.WorkspaceID(),
				WorkspacePath: pr.WorkspacePath(),
			})
		}
	}
	return out
}

func (e *Engine) UpdateStatus() UpdateStatus {
	e.initExeWatch()
	st := UpdateStatus{RunningVersion: e.version, Executable: e.exe.path}
	procs := e.RunningProcesses()
	st.RunningProcesses = len(procs)
	st.Processes = procs
	installed, changed := e.installedVersion()
	if !changed {
		return st
	}
	st.InstalledVersion = installed
	if installed != "" && installed == e.version {
		return st
	}
	st.Pending = true
	st.Notice = UpdateNotice(e.version, installed, len(procs))
	return st
}

func UpdateNotice(running, installed string, procs int) string {
	target := "A new jack-ketch version"
	if installed != "" {
		target = "jack-ketch " + installed
	}
	msg := target + " is installed but the daemon is still running " + running + ". Restarting the daemon (jack-ketch daemon restart) finishes the update"
	switch procs {
	case 0:
		return msg + "; no managed processes are running."
	case 1:
		return msg + " and stops the 1 running managed process."
	default:
		return msg + " and stops all " + strconv.Itoa(procs) + " running managed processes."
	}
}

func (e *Engine) installedVersion() (string, bool) {
	w := &e.exe
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.path == "" {
		return "", false
	}
	st, err := os.Stat(w.path)
	if err != nil {
		return "", false
	}
	if st.ModTime().Equal(w.bootMod) && st.Size() == w.bootSize && fileIno(st) == w.bootIno {
		return "", false
	}
	if !w.lastMod.Equal(st.ModTime()) || time.Since(w.checked) > 10*time.Minute {
		w.lastMod = st.ModTime()
		w.checked = time.Now()
		w.installed = probeVersion(w.path)
	}
	return w.installed, true
}

func probeVersion(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if i := strings.IndexByte(v, '\n'); i >= 0 {
		v = v[:i]
	}
	if len(v) > 64 {
		return ""
	}
	return v
}
