// Package project provides the project registry that maps
// working directories to stable project identities.
package project

// Registry resolves paths to projects and workspaces.
type Registry struct {
	store Store
}

func NewRegistry(store Store) *Registry {
	return &Registry{store: store}
}

// Resolve maps a working directory path to a project ID and workspace ID.
func (r *Registry) Resolve(path string) (ID, WorkspaceID, error) {
	resolver := NewResolver(r.store)
	resolution, err := resolver.Resolve(path)
	if err != nil {
		return "", "", err
	}
	return resolution.ProjectID, resolution.WorkspaceID, nil
}

// ResolveFull maps a path to the full resolution including NewlyCreated.
func (r *Registry) ResolveFull(path string) (*Resolution, error) {
	resolver := NewResolver(r.store)
	return resolver.Resolve(path)
}
