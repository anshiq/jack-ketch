package core

import "sync"

type MCPSubscriptionBinding struct {
	WorkspaceID string
	Client      string
}

type MCPSubscriptions struct {
	mu   sync.Mutex
	subs map[string]MCPSubscriptionBinding
}

func (m *MCPSubscriptions) Bind(id, workspaceID, client string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subs == nil {
		m.subs = map[string]MCPSubscriptionBinding{}
	}
	m.subs[id] = MCPSubscriptionBinding{WorkspaceID: workspaceID, Client: client}
}

func (m *MCPSubscriptions) Lookup(id string) (MCPSubscriptionBinding, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.subs[id]
	return b, ok
}

func (m *MCPSubscriptions) Forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subs, id)
}

func (m *MCPSubscriptions) matching(fn func(MCPSubscriptionBinding) bool) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, b := range m.subs {
		if fn(b) {
			out = append(out, id)
		}
	}
	return out
}

func (m *MCPSubscriptions) ForClient(client string) []string {
	return m.matching(func(b MCPSubscriptionBinding) bool { return b.Client == client })
}

func (m *MCPSubscriptions) ForWorkspace(ws string) []string {
	return m.matching(func(b MCPSubscriptionBinding) bool { return b.WorkspaceID == ws })
}
