package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"jack-ketch/internal/process"
)

type ManagedRef struct {
	ProcessID     string `json:"processId"`
	App           string `json:"app,omitempty"`
	Mode          string `json:"mode,omitempty"`
	Status        string `json:"status"`
	WorkspaceID   string `json:"workspaceId"`
	WorkspacePath string `json:"workspacePath"`
}

type DeclaredPort struct {
	WorkspaceID   string `json:"workspaceId"`
	WorkspacePath string `json:"workspacePath"`
	App           string `json:"app"`
	Name          string `json:"name"`
	Port          int    `json:"port"`
	Default       int    `json:"default,omitempty"`
}

type PortEntry struct {
	Port     int            `json:"port"`
	Address  string         `json:"address,omitempty"`
	PID      int            `json:"pid,omitempty"`
	Command  string         `json:"command,omitempty"`
	Listen   bool           `json:"listening"`
	Managed  *ManagedRef    `json:"managed,omitempty"`
	Declared []DeclaredPort `json:"declared,omitempty"`
	Conflict string         `json:"conflict,omitempty"`
	Self     bool           `json:"self,omitempty"`
}

type PortsView struct {
	Supported bool        `json:"supported"`
	Ports     []PortEntry `json:"ports"`
}

func (e *Engine) DeclaredPorts() []DeclaredPort {
	var out []DeclaredPort
	e.RangeRuntimes(func(pr *ProjectRuntime) bool {
		res := pr.ResolvedConfig()
		if res == nil {
			return true
		}
		for app, cfg := range res.Apps {
			for name, decl := range cfg.Ports {
				d := DeclaredPort{WorkspaceID: pr.WorkspaceID(), WorkspacePath: pr.WorkspacePath(), App: app, Name: name, Default: decl.Default, Port: decl.Default}
				if v, ok, _ := e.store.GetSetting("ports/" + pr.WorkspaceID() + "/" + name); ok {
					if p, err := strconv.Atoi(strings.Trim(v, `"`)); err == nil && p > 0 {
						d.Port = p
					}
				}
				if d.Port > 0 {
					out = append(out, d)
				}
			}
		}
		return true
	})
	return out
}

func (e *Engine) managedByPID() map[int]ManagedRef {
	out := map[int]ManagedRef{}
	e.RangeRuntimes(func(pr *ProjectRuntime) bool {
		rt := pr.LoadedRuntime()
		if rt == nil {
			return true
		}
		list, err := rt.List()
		if err != nil {
			return true
		}
		for _, p := range list.Processes {
			if p.PID <= 0 || (p.Status != "running" && p.Status != "starting" && p.Status != "stopping") {
				continue
			}
			ref := ManagedRef{ProcessID: p.ProcessID, App: p.App, Mode: p.Mode, Status: p.Status, WorkspaceID: pr.WorkspaceID(), WorkspacePath: pr.WorkspacePath()}
			for _, pid := range process.TreePIDs(p.PID) {
				out[pid] = ref
			}
		}
		return true
	})
	return out
}

func (e *Engine) Ports() PortsView {
	view := PortsView{Supported: process.ListenersSupported(), Ports: []PortEntry{}}
	managed := e.managedByPID()
	declared := map[int][]DeclaredPort{}
	for _, d := range e.DeclaredPorts() {
		declared[d.Port] = append(declared[d.Port], d)
	}
	self := os.Getpid()
	listening := map[int]bool{}
	for _, l := range process.Listeners() {
		listening[l.Port] = true
		pe := PortEntry{Port: l.Port, Address: l.Address, PID: l.PID, Command: l.Command, Listen: true, Declared: declared[l.Port], Self: l.PID == self}
		if m, ok := managed[l.PID]; ok {
			ref := m
			pe.Managed = &ref
		}
		pe.Conflict = conflictFor(pe)
		view.Ports = append(view.Ports, pe)
	}
	for port, ds := range declared {
		if listening[port] {
			continue
		}
		view.Ports = append(view.Ports, PortEntry{Port: port, Declared: ds})
	}
	sort.SliceStable(view.Ports, func(i, j int) bool {
		if view.Ports[i].Port != view.Ports[j].Port {
			return view.Ports[i].Port < view.Ports[j].Port
		}
		return view.Ports[i].PID < view.Ports[j].PID
	})
	return view
}

func conflictFor(pe PortEntry) string {
	if len(pe.Declared) == 0 || !pe.Listen {
		return ""
	}
	for _, d := range pe.Declared {
		if pe.Managed != nil && pe.Managed.WorkspaceID == d.WorkspaceID && pe.Managed.App == d.App {
			return ""
		}
	}
	d := pe.Declared[0]
	if pe.Self {
		return ""
	}
	if pe.Managed != nil {
		return fmt.Sprintf("declared by %s/%s but held by another managed process (%s)", d.App, d.Name, pe.Managed.ProcessID)
	}
	return fmt.Sprintf("declared by %s/%s but held by a process jack-ketch does not manage", d.App, d.Name)
}

var ErrManagedPort = errors.New("this port belongs to a managed process; stop it instead")

func (e *Engine) KillPortHolder(port, pid int, force bool) error {
	if pid <= 0 || pid == os.Getpid() {
		return errors.New("refusing to signal this process")
	}
	held := false
	for _, l := range process.Listeners() {
		if l.Port == port && l.PID == pid {
			held = true
			break
		}
	}
	if !held {
		return fmt.Errorf("pid %d does not listen on port %d (anymore)", pid, port)
	}
	if _, ok := e.managedByPID()[pid]; ok {
		return ErrManagedPort
	}
	if uid, ok := process.ProcessUID(pid); !ok || uid != os.Getuid() {
		return errors.New("refusing to signal a process owned by another user")
	}
	if err := signalPID(pid, force); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"port": port, "pid": pid, "force": force})
	_, _ = e.store.AppendEvent(time.Now().UnixNano(), "port.holder_signalled", "", "", "", "", "", string(b))
	return nil
}
