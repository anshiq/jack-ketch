package project

import (
	"path/filepath"
	"strings"
)

type NestingStore interface {
	ProjectAppDirs(projectID ID, wsPath string) ([]string, error)
}

func Within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (r *Resolver) attachToAncestor(canonical string) *Resolution {
	ns, ok := r.store.(NestingStore)
	if !ok {
		return nil
	}
	projects, err := r.store.ListProjects()
	if err != nil {
		return nil
	}
	var best *Workspace
	var bestProject *Project
	for _, p := range projects {
		wss, err := r.store.ListWorkspaces(p.ID)
		if err != nil {
			continue
		}
		for _, ws := range wss {
			if ws.Path == canonical || !Within(ws.Path, canonical) {
				continue
			}
			if best != nil && len(ws.Path) <= len(best.Path) {
				continue
			}
			dirs, err := ns.ProjectAppDirs(p.ID, ws.Path)
			if err != nil {
				continue
			}
			for _, d := range dirs {
				if Within(canonical, d) {
					best, bestProject = ws, p
					break
				}
			}
		}
	}
	if best == nil {
		return nil
	}
	rel, err := filepath.Rel(best.Path, canonical)
	if err != nil {
		return nil
	}
	_ = r.store.UpdateWorkspaceLastSeen(best.ID)
	_ = r.touchProject(best.ProjectID)
	return &Resolution{
		ProjectID:       best.ProjectID,
		WorkspaceID:     best.ID,
		ProjectName:     bestProject.Name,
		AttachedSubpath: rel,
	}
}

func (r *Resolver) createProjectWithWorkspace(name, path string) (*Project, *Workspace, error) {
	p, err := r.store.CreateProject(name)
	if err != nil {
		return nil, nil, err
	}
	ws, err := r.store.CreateWorkspace(p.ID, path)
	if err != nil {
		_ = r.store.DeleteProject(p.ID)
		return nil, nil, err
	}
	return p, ws, nil
}
