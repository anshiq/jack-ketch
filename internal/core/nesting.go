package core

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"jack-ketch/internal/config"
	"jack-ketch/internal/process"
	"jack-ketch/internal/project"
	"jack-ketch/internal/store"
)

type NestedApp struct {
	Name      string `json:"name"`
	Workdir   string `json:"workdir"`
	Status    string `json:"status"`
	ParentApp string `json:"parentApp,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type NestedSide struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
}

type NestedProject struct {
	Child   NestedSide  `json:"child"`
	Parent  NestedSide  `json:"parent"`
	Apps    []NestedApp `json:"apps"`
	Running int         `json:"running"`
}

type MergeReport struct {
	Child       NestedSide        `json:"child"`
	Parent      NestedSide        `json:"parent"`
	Apps        []NestedApp       `json:"apps"`
	Renamed     map[string]string `json:"renamed,omitempty"`
	Credentials int               `json:"credentials"`
	Running     int               `json:"running"`
	Applied     bool              `json:"applied"`
	Revision    int64             `json:"revision,omitempty"`
	Moved       int               `json:"credentialsMoved,omitempty"`
	Skipped     int               `json:"credentialsSkipped,omitempty"`
	YAML        string            `json:"yaml,omitempty"`
}

func absWorkdir(wsPath, wd string) string {
	if wd == "" || wd == "." || wd == "./" {
		return filepath.Clean(wsPath)
	}
	if filepath.IsAbs(wd) {
		return filepath.Clean(wd)
	}
	return filepath.Join(wsPath, wd)
}

func relWorkdir(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

func appKey(wsPath string, a config.V3App) string {
	return absWorkdir(wsPath, a.WorkDir) + "\x00" + strings.Join(a.Command, "\x00")
}

func projectAppsOf(db *store.DB, projectID string) (map[string]config.V3App, []byte, error) {
	data, _, err := db.GetConfig(projectID, "project", "")
	if err != nil {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]config.V3App{}, data, nil
	}
	cfg, errs := config.ParseV3(data)
	if errs != nil || cfg == nil {
		return nil, data, fmt.Errorf("project %s has an invalid config", projectID)
	}
	return cfg.Apps, data, nil
}

func (a storeAdapter) ProjectAppDirs(projectID project.ID, wsPath string) ([]string, error) {
	apps, _, err := projectAppsOf(a.db, string(projectID))
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(apps))
	for _, app := range apps {
		dirs = append(dirs, absWorkdir(wsPath, app.WorkDir))
	}
	return dirs, nil
}

func (e *Engine) sideOf(ws *store.Workspace) NestedSide {
	name := ""
	if p, err := e.store.GetProject(ws.ProjectID); err == nil && p != nil {
		name = p.Name
	}
	return NestedSide{ProjectID: ws.ProjectID, ProjectName: name, WorkspaceID: ws.ID, Path: ws.Path}
}

func (e *Engine) runningIn(wsID string) int {
	val, ok := e.runtimes.Load(wsID)
	if !ok {
		return 0
	}
	pr := val.(*ProjectRuntime)
	pr.mu.RLock()
	rt := pr.rt
	pr.mu.RUnlock()
	if rt == nil {
		return 0
	}
	n := 0
	for _, p := range rt.Manager().List() {
		switch p.Info().Status {
		case process.StatusRunning, process.StatusStarting, process.StatusStopping:
			n++
		}
	}
	return n
}

func classifyApps(parentPath string, parentApps map[string]config.V3App, childPath string, childApps map[string]config.V3App) []NestedApp {
	byKey := map[string]string{}
	byNameDir := map[string]string{}
	for name, a := range parentApps {
		byKey[appKey(parentPath, a)] = name
		byNameDir[name+"\x00"+absWorkdir(parentPath, a.WorkDir)] = name
	}
	names := make([]string, 0, len(childApps))
	for n := range childApps {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]NestedApp, 0, len(names))
	for _, n := range names {
		a := childApps[n]
		dir := absWorkdir(childPath, a.WorkDir)
		item := NestedApp{Name: n, Workdir: dir}
		if pn, ok := byKey[appKey(childPath, a)]; ok {
			item.Status, item.ParentApp = "duplicate", pn
		} else if pn, ok := byNameDir[n+"\x00"+dir]; ok {
			item.Status, item.ParentApp = "conflict", pn
			item.Detail = "same app name and workdir, different command"
		} else {
			item.Status = "unique"
		}
		out = append(out, item)
	}
	return out
}

func (e *Engine) NestedProjects() ([]NestedProject, error) {
	projects, err := e.store.ListProjects()
	if err != nil {
		return nil, err
	}
	type entry struct {
		ws   *store.Workspace
		apps map[string]config.V3App
	}
	var all []entry
	for _, p := range projects {
		wss, err := e.store.ListWorkspaces(p.ID)
		if err != nil {
			continue
		}
		apps, _, err := projectAppsOf(e.store, p.ID)
		if err != nil {
			apps = map[string]config.V3App{}
		}
		for _, ws := range wss {
			all = append(all, entry{ws: ws, apps: apps})
		}
	}
	var out []NestedProject
	for _, child := range all {
		var parent *entry
		for i := range all {
			cand := &all[i]
			if cand.ws.ProjectID == child.ws.ProjectID || cand.ws.Path == child.ws.Path || !project.Within(cand.ws.Path, child.ws.Path) {
				continue
			}
			covers := false
			for _, a := range cand.apps {
				if project.Within(child.ws.Path, absWorkdir(cand.ws.Path, a.WorkDir)) {
					covers = true
					break
				}
			}
			if !covers {
				continue
			}
			if parent == nil || len(cand.ws.Path) > len(parent.ws.Path) {
				parent = cand
			}
		}
		if parent == nil {
			continue
		}
		out = append(out, NestedProject{
			Child:   e.sideOf(child.ws),
			Parent:  e.sideOf(parent.ws),
			Apps:    classifyApps(parent.ws.Path, parent.apps, child.ws.Path, child.apps),
			Running: e.runningIn(child.ws.ID),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Child.Path < out[j].Child.Path })
	return out, nil
}

func mappingGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mappingSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
}

func nodeEmpty(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Value {
		case "", "0", "false", "null", "~":
			return true
		}
		return false
	case yaml.SequenceNode, yaml.MappingNode:
		return len(n.Content) == 0
	}
	return false
}

func nodeEqual(a, b *yaml.Node) bool {
	x, _ := yaml.Marshal(a)
	y, _ := yaml.Marshal(b)
	return bytes.Equal(x, y)
}

func mergeMissing(dst, src *yaml.Node, path string, conflicts *[]string) {
	if dst == nil || src == nil || dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		key, sv := src.Content[i].Value, src.Content[i+1]
		if path == "" && key == "workdir" {
			continue
		}
		dv := mappingGet(dst, key)
		full := key
		if path != "" {
			full = path + "." + key
		}
		switch {
		case dv == nil || nodeEmpty(dv):
			if !nodeEmpty(sv) {
				mappingSet(dst, key, sv)
			}
		case sv.Kind == yaml.MappingNode && dv.Kind == yaml.MappingNode && (key == "modes" || path == "modes"):
			mergeMissing(dv, sv, full, conflicts)
		case nodeEmpty(sv) || nodeEqual(dv, sv):
		default:
			*conflicts = append(*conflicts, full)
		}
	}
}

func (e *Engine) planNestedMerge(childID, parentID string) (*MergeReport, []byte, []*store.Workspace, error) {
	if childID == "" || parentID == "" || childID == parentID {
		return nil, nil, nil, fmt.Errorf("invalid_argument: child and parent must be two different projects")
	}
	childWS, err := e.store.ListWorkspaces(childID)
	if err != nil || len(childWS) == 0 {
		return nil, nil, nil, fmt.Errorf("child project %s has no live workspace", childID)
	}
	parentWS, err := e.store.ListWorkspaces(parentID)
	if err != nil || len(parentWS) == 0 {
		return nil, nil, nil, fmt.Errorf("parent project %s has no live workspace", parentID)
	}
	var pws, cws *store.Workspace
	for _, c := range childWS {
		for _, p := range parentWS {
			if p.Path != c.Path && project.Within(p.Path, c.Path) {
				if pws == nil || len(p.Path) > len(pws.Path) {
					pws, cws = p, c
				}
			}
		}
	}
	if pws == nil {
		return nil, nil, nil, fmt.Errorf("invalid_argument: no workspace of project %s is located inside a workspace of project %s", childID, parentID)
	}
	childApps, childYAML, err := projectAppsOf(e.store, childID)
	if err != nil {
		return nil, nil, nil, err
	}
	parentApps, parentYAML, err := projectAppsOf(e.store, parentID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(bytes.TrimSpace(parentYAML)) == 0 {
		parentYAML = []byte("version: 3\napps: {}\n")
	}

	var childDoc, parentDoc yaml.Node
	if len(bytes.TrimSpace(childYAML)) > 0 {
		if err := yaml.Unmarshal(childYAML, &childDoc); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := yaml.Unmarshal(parentYAML, &parentDoc); err != nil {
		return nil, nil, nil, err
	}
	if parentDoc.Kind != yaml.DocumentNode || len(parentDoc.Content) == 0 {
		return nil, nil, nil, fmt.Errorf("parent config is empty")
	}
	parentRoot := parentDoc.Content[0]
	parentAppsNode := mappingGet(parentRoot, "apps")
	if parentAppsNode == nil || parentAppsNode.Kind != yaml.MappingNode {
		parentAppsNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mappingSet(parentRoot, "apps", parentAppsNode)
	}
	var childAppsNode *yaml.Node
	if childDoc.Kind == yaml.DocumentNode && len(childDoc.Content) > 0 {
		childAppsNode = mappingGet(childDoc.Content[0], "apps")
	}

	report := &MergeReport{Child: e.sideOf(cws), Parent: e.sideOf(pws), Renamed: map[string]string{}}
	classified := classifyApps(pws.Path, parentApps, cws.Path, childApps)
	for i := range classified {
		item := &classified[i]
		childNode := mappingGet(childAppsNode, item.Name)
		if childNode == nil {
			continue
		}
		switch item.Status {
		case "duplicate", "conflict":
			parentNode := mappingGet(parentAppsNode, item.ParentApp)
			var conflicts []string
			mergeMissing(parentNode, childNode, "", &conflicts)
			if item.ParentApp != item.Name {
				report.Renamed[item.Name] = item.ParentApp
			}
			if len(conflicts) > 0 {
				item.Detail = strings.TrimSpace(item.Detail + " kept parent value for: " + strings.Join(conflicts, ", "))
			}
		default:
			target := item.Name
			if mappingGet(parentAppsNode, target) != nil {
				target = report.Child.ProjectName + "-" + item.Name
				for n := 2; mappingGet(parentAppsNode, target) != nil; n++ {
					target = fmt.Sprintf("%s-%s-%d", report.Child.ProjectName, item.Name, n)
				}
				report.Renamed[item.Name] = target
				item.Detail = "added as " + target + " (name already used in parent)"
			}
			clone := cloneNode(childNode)
			mappingSet(clone, "workdir", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: relWorkdir(pws.Path, item.Workdir), Style: quoteIfEmpty(relWorkdir(pws.Path, item.Workdir))})
			mappingSet(parentAppsNode, target, clone)
			item.Status = "added"
		}
	}
	report.Apps = classified
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&parentDoc); err != nil {
		return nil, nil, nil, err
	}
	_ = enc.Close()
	if verrs := config.Validate(buf.Bytes()); len(verrs) > 0 {
		return nil, nil, nil, fmt.Errorf("merged config would be invalid: %v", verrs[0])
	}
	report.YAML = buf.String()
	if creds, err := e.store.ListCredentials(childID); err == nil {
		report.Credentials = len(creds)
	}
	for _, ws := range childWS {
		report.Running += e.runningIn(ws.ID)
	}
	return report, buf.Bytes(), childWS, nil
}

func quoteIfEmpty(s string) yaml.Style {
	if s == "" {
		return yaml.DoubleQuotedStyle
	}
	return 0
}

func cloneNode(n *yaml.Node) *yaml.Node {
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, k := range n.Content {
		c.Content[i] = cloneNode(k)
	}
	return &c
}

func (e *Engine) PreviewNestedMerge(childID, parentID string) (*MergeReport, error) {
	rep, _, _, err := e.planNestedMerge(childID, parentID)
	return rep, err
}

func (e *Engine) MergeNestedProject(childID, parentID, session string) (*MergeReport, error) {
	rep, yamlOut, childWS, err := e.planNestedMerge(childID, parentID)
	if err != nil {
		return nil, err
	}
	if rep.Running > 0 {
		return nil, fmt.Errorf("failed_precondition: stop the %d running process(es) in %s before merging", rep.Running, rep.Child.ProjectName)
	}
	msg := fmt.Sprintf("merge nested project %s into %s", rep.Child.ProjectName, rep.Parent.ProjectName)
	rev, _, err := e.ApplyProjectYAML(parentID, "project", "", string(yamlOut), "merge", session, msg, false)
	if err != nil {
		return nil, err
	}
	rep.Revision = rev
	moved, skipped, err := e.store.MoveCredentials(childID, parentID, rep.Renamed)
	if err != nil {
		return nil, err
	}
	rep.Moved, rep.Skipped = moved, skipped
	for _, ws := range childWS {
		_ = e.store.MarkWorkspaceMissing(ws.ID)
		e.UnloadRuntime(ws.ID)
	}
	if err := e.store.ForgetProject(childID); err != nil {
		return nil, err
	}
	rep.Applied = true
	rep.YAML = ""
	return rep, nil
}
