package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func integrationCall(t *testing.T, sock, method string, body any, out any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(http.MethodPost, "http://jackketchd/jackketch.v1.IntegrationService/"+method, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: status %d: %s", method, resp.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s: unmarshal %s: %v", method, data, err)
		}
	}
}

func TestIntegrationAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("JACK_KETCH_BIN", "/opt/ar/jack-ketch")

	eng, done := testEngine(t)
	defer done()
	sock := sockPath(t, eng)

	var harnesses struct {
		Harnesses []map[string]any `json:"harnesses"`
	}
	integrationCall(t, sock, "ListHarnesses", map[string]any{}, &harnesses)
	if len(harnesses.Harnesses) < 20 {
		t.Fatalf("harnesses = %d, want >= 20", len(harnesses.Harnesses))
	}
	var cursor map[string]any
	for _, h := range harnesses.Harnesses {
		if h["id"] == "cursor" {
			cursor = h
			break
		}
	}
	if cursor == nil {
		t.Fatal("cursor harness missing from ListHarnesses")
	}
	if cursor["tier"] != float64(1) {
		t.Fatalf("cursor tier = %v, want 1", cursor["tier"])
	}
	if _, ok := cursor["connected"]; !ok {
		t.Fatal("cursor missing connected count")
	}

	var install struct {
		Changed bool   `json:"changed"`
		Path    string `json:"path"`
	}
	integrationCall(t, sock, "InstallMCP", map[string]any{"harness": "cursor", "scope": "user"}, &install)
	if !install.Changed {
		t.Fatal("first cursor install reported changed=false")
	}
	mcpPath := filepath.Join(home, ".cursor", "mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("read %s: %v", mcpPath, err)
	}
	if !strings.Contains(string(data), "/opt/ar/jack-ketch") {
		t.Fatalf("%s missing JACK_KETCH_BIN command: %s", mcpPath, data)
	}
	if strings.Contains(string(data), "jackketchd") {
		t.Fatalf("%s contains jackketchd: %s", mcpPath, data)
	}

	var again struct {
		Changed bool `json:"changed"`
	}
	integrationCall(t, sock, "InstallMCP", map[string]any{"harness": "cursor", "scope": "user"}, &again)
	if again.Changed {
		t.Fatal("second cursor install reported changed=true")
	}

	var preview struct {
		Diff string `json:"diff"`
	}
	integrationCall(t, sock, "PreviewInstall", map[string]any{"harness": "cursor", "scope": "user", "action": "remove"}, &preview)
	if !strings.Contains(preview.Diff, "-") {
		t.Fatalf("remove preview diff has no removals: %q", preview.Diff)
	}

	var removed struct {
		Changed bool `json:"changed"`
	}
	integrationCall(t, sock, "RemoveMCP", map[string]any{"harness": "cursor", "scope": "user"}, &removed)
	if !removed.Changed {
		t.Fatal("cursor remove reported changed=false")
	}
	if data, err := os.ReadFile(mcpPath); err == nil && strings.Contains(string(data), "jack-ketch") {
		t.Fatalf("cursor entry still present after remove: %s", data)
	}

	project := t.TempDir()
	var projectInstall struct {
		Changed bool `json:"changed"`
	}
	integrationCall(t, sock, "InstallMCP", map[string]any{"harness": "claude-code", "scope": "project", "projectPath": project}, &projectInstall)
	if !projectInstall.Changed {
		t.Fatal("claude-code project install reported changed=false")
	}
	projectConfig := filepath.Join(project, ".mcp.json")
	pdata, err := os.ReadFile(projectConfig)
	if err != nil {
		t.Fatalf("read %s: %v", projectConfig, err)
	}
	if !strings.Contains(string(pdata), `"jack-ketch"`) {
		t.Fatalf("%s missing portable command: %s", projectConfig, pdata)
	}
}
