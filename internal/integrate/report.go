package integrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type entryInfo struct {
	Command string
	Args    []string
}

type EntryStatus struct {
	Path         string   `json:"path"`
	FileExists   bool     `json:"fileExists"`
	Installed    bool     `json:"installed"`
	Command      string   `json:"command,omitempty"`
	Args         []string `json:"args,omitempty"`
	CommandFound bool     `json:"commandFound"`
	UpToDate     bool     `json:"upToDate"`
	Problems     []string `json:"problems,omitempty"`
}

type ScopeReport struct {
	Scope    Scope         `json:"scope"`
	Status   string        `json:"status"`
	Portable bool          `json:"portable"`
	Note     string        `json:"note,omitempty"`
	Entries  []EntryStatus `json:"entries"`
	Error    string        `json:"error,omitempty"`
}

type HarnessReport struct {
	ID          string        `json:"id"`
	DisplayName string        `json:"displayName"`
	Tier        int           `json:"tier"`
	DocsURL     string        `json:"docsUrl"`
	Detection   Detection     `json:"detection"`
	Scopes      []ScopeReport `json:"scopes"`
	SkillsAvail bool          `json:"skillsSupported"`
	ManualSteps string        `json:"manualSteps,omitempty"`
	RestartHint string        `json:"restartHint,omitempty"`
	MaxWaitMS   int           `json:"maxWaitMs"`
}

func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func entryFromMap(m map[string]any) entryInfo {
	var e entryInfo
	switch c := m["command"].(type) {
	case string:
		e.Command, e.Args = c, stringList(m["args"])
	case []any:
		parts := stringList(c)
		if len(parts) > 0 {
			e.Command, e.Args = parts[0], parts[1:]
		}
	}
	if e.Command == "" {
		if c, ok := m["cmd"].(string); ok {
			e.Command, e.Args = c, stringList(m["args"])
		}
	}
	return e
}

func readEntry(t Target, data []byte, keyPath []string) (entryInfo, bool, error) {
	switch t.Format {
	case FormatJSON, FormatJSONC:
		doc, err := parseJSONC(data)
		if err != nil {
			return entryInfo{}, false, err
		}
		raw, ok, err := doc.get(keyPath, ServerName)
		if err != nil || !ok {
			return entryInfo{}, false, err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return entryInfo{}, true, err
		}
		return entryFromMap(m), true, nil
	case FormatTOML:
		_, ours, err := tomlStrip(data)
		if err != nil || len(ours) == 0 {
			return entryInfo{}, len(ours) > 0, err
		}
		return tomlEntry(ours), true, nil
	case FormatGooseYAML, FormatContinueYAML:
		_, root, err := yamlDoc(data)
		if err != nil {
			return entryInfo{}, false, err
		}
		var node *yaml.Node
		if t.Format == FormatGooseYAML {
			m, err := yamlChild(root, "extensions", yaml.MappingNode, false)
			if err != nil || m == nil {
				return entryInfo{}, false, err
			}
			if i := yamlKeyIndex(m, ServerName); i >= 0 {
				node = m.Content[i+1]
			}
		} else {
			seq, err := yamlChild(root, "mcpServers", yaml.SequenceNode, false)
			if err != nil || seq == nil {
				return entryInfo{}, false, err
			}
			if i := listItemIndex(seq, ServerName); i >= 0 {
				node = seq.Content[i]
			}
		}
		if node == nil {
			return entryInfo{}, false, nil
		}
		var m map[string]any
		if err := node.Decode(&m); err != nil {
			return entryInfo{}, true, err
		}
		return entryFromMap(m), true, nil
	case FormatContinueBlock:
		var doc continueBlockDoc
		if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.MCPServers) == 0 {
			return entryInfo{}, false, err
		}
		return entryInfo{Command: doc.MCPServers[0].Command, Args: doc.MCPServers[0].Args}, true, nil
	}
	return entryInfo{}, false, nil
}

func commandFound(cmd string) bool {
	if cmd == "" {
		return false
	}
	if filepath.IsAbs(cmd) {
		st, err := os.Stat(cmd)
		return err == nil && !st.IsDir()
	}
	_, err := exec.LookPath(cmd)
	return err == nil
}

func entryProblems(e entryInfo) []string {
	var out []string
	if strings.TrimSuffix(filepath.Base(e.Command), ".exe") == "jackketchd" {
		out = append(out, "points to jackketchd (written by an older web UI); reinstall")
	}
	if len(e.Args) == 0 || e.Args[0] != "serve" {
		out = append(out, "arguments do not start with serve")
	}
	if !commandFound(e.Command) {
		out = append(out, "command not found: "+e.Command)
	}
	return out
}

func Inspect(id string, scope Scope, opts Options) ([]EntryStatus, error) {
	h, t, project, err := targetFor(id, scope, opts)
	if err != nil {
		return nil, err
	}
	env := currentPathEnv(project)
	paths, err := resolveTargetPaths(t, env, true)
	if err != nil {
		return nil, err
	}
	spec, specErr := serverSpec(h, t, opts)
	keyPath := expandKeyPath(t.KeyPath, project)
	out := make([]EntryStatus, 0, len(paths))
	for _, path := range paths {
		st := EntryStatus{Path: path}
		data, existed, err := readConfig(path)
		if err != nil {
			st.Problems = append(st.Problems, err.Error())
			out = append(out, st)
			continue
		}
		st.FileExists = existed
		if existed {
			e, ok, err := readEntry(t, data, keyPath)
			if err != nil {
				st.Problems = append(st.Problems, err.Error())
			}
			if ok {
				st.Installed = true
				st.Command, st.Args = e.Command, e.Args
				st.CommandFound = commandFound(e.Command)
				st.Problems = append(st.Problems, entryProblems(e)...)
				if specErr == nil {
					if desired, err := upsertEntry(h, t, data, keyPath, spec); err == nil {
						st.UpToDate = bytes.Equal(desired, data)
					}
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}

func scopeStatus(entries []EntryStatus) string {
	installed, broken, stale := 0, 0, 0
	for _, e := range entries {
		if !e.Installed {
			continue
		}
		installed++
		if len(e.Problems) > 0 {
			broken++
		} else if !e.UpToDate {
			stale++
		}
	}
	switch {
	case installed == 0:
		return "absent"
	case broken > 0:
		return "broken"
	case stale > 0 || installed < len(entries):
		return "outdated"
	}
	return "installed"
}

func Report(h Harness, projectDir string) HarnessReport {
	r := HarnessReport{
		ID: h.ID, DisplayName: h.DisplayName, Tier: h.Tier, DocsURL: h.DocsURL,
		Detection: Detect(h), SkillsAvail: h.Skills != nil, ManualSteps: h.ManualSteps,
		RestartHint: h.RestartHint, MaxWaitMS: WaitLimit(h.ID, true),
	}
	for _, sc := range h.ScopeList() {
		t := h.Scopes[sc]
		sr := ScopeReport{Scope: sc, Portable: t.Portable, Note: t.Note}
		if sc != ScopeUser && projectDir == "" {
			sr.Status = "unavailable"
			sr.Error = "select a project to manage this scope"
			r.Scopes = append(r.Scopes, sr)
			continue
		}
		entries, err := Inspect(h.ID, sc, Options{ProjectDir: projectDir})
		if err != nil {
			sr.Status = "unavailable"
			sr.Error = err.Error()
		} else {
			sr.Entries = entries
			sr.Status = scopeStatus(entries)
		}
		r.Scopes = append(r.Scopes, sr)
	}
	return r
}

func ReportAll(projectDir string) []HarnessReport {
	hs := Harnesses()
	out := make([]HarnessReport, len(hs))
	done := make(chan struct{}, len(hs))
	for i, h := range hs {
		go func(i int, h Harness) {
			out[i] = Report(h, projectDir)
			done <- struct{}{}
		}(i, h)
	}
	for range hs {
		<-done
	}
	return out
}

type Snippet struct {
	Harness  string `json:"harness"`
	Scope    Scope  `json:"scope,omitempty"`
	Format   string `json:"format"`
	Path     string `json:"path,omitempty"`
	Text     string `json:"text"`
	CLI      string `json:"cli,omitempty"`
	Deeplink string `json:"deeplink,omitempty"`
	Steps    string `json:"steps,omitempty"`
}

func SnippetFor(id string, scope Scope, opts Options) (Snippet, error) {
	h, ok := Lookup(id)
	if !ok {
		return Snippet{}, fmt.Errorf("unknown agent %q", id)
	}
	if h.Tier != 1 {
		cmd := opts.Command
		if cmd == "" {
			c, err := ResolveServerCommand(false)
			if err != nil {
				return Snippet{}, err
			}
			cmd = c
		}
		spec := ServerSpec{Command: cmd, Args: []string{"serve", "--harness", h.ID}}
		key := h.SnippetKey
		if key == nil {
			key = []string{"mcpServers"}
		}
		var v any = orderedObject{{ServerName, jsonEntry(DialectPlain, spec)}}
		for i := len(key) - 1; i >= 0; i-- {
			v = orderedObject{{key[i], v}}
		}
		text, _ := json.MarshalIndent(v, "", "  ")
		sn := Snippet{Harness: h.ID, Format: "json", Text: string(text) + "\n", Steps: h.ManualSteps}
		if h.CLIAdd != nil {
			sn.CLI = h.CLIAdd(spec)
		}
		return sn, nil
	}
	h, t, project, err := targetFor(id, scope, opts)
	if err != nil {
		return Snippet{}, err
	}
	spec, err := serverSpec(h, t, opts)
	if err != nil {
		return Snippet{}, err
	}
	text, err := upsertEntry(h, t, []byte(t.NewFile), expandKeyPath(t.KeyPath, project), spec)
	if err != nil {
		return Snippet{}, err
	}
	sn := Snippet{Harness: h.ID, Scope: scope, Format: string(t.Format), Text: string(text), Steps: h.ManualSteps}
	if cands := candidatePaths(t, currentPathEnv(project)); len(cands) > 0 {
		sn.Path = cands[0]
	}
	if h.CLIAdd != nil && scope == ScopeUser {
		sn.CLI = h.CLIAdd(spec)
	}
	if h.Deeplink != nil && scope == ScopeUser {
		sn.Deeplink = h.Deeplink(spec)
	}
	return sn, nil
}
