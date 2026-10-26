package integrate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestJSONCUpsertEmpty(t *testing.T) {
	doc, err := parseJSONC(nil)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := doc.upsert([]string{"mcpServers"}, ServerName, orderedObject{{"command", "x"}})
	if err != nil || !changed {
		t.Fatalf("upsert changed=%v err=%v", changed, err)
	}
	out := doc.bytes()
	if !json.Valid(out) {
		t.Fatalf("not valid JSON: %s", out)
	}
	var m map[string]map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["mcpServers"][ServerName]; !ok {
		t.Fatalf("missing entry: %s", out)
	}
}

func TestJSONCPreservesCommentsAndOthers(t *testing.T) {
	base := []byte("{\n  // keep me\n  \"mcpServers\": {\n    \"other\": {\"command\": \"other\"},\n  },\n}\n")
	doc, err := parseJSONC(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.upsert([]string{"mcpServers"}, ServerName, orderedObject{{"command", "x"}}); err != nil {
		t.Fatal(err)
	}
	out := string(doc.bytes())
	if !strings.Contains(out, "// keep me") {
		t.Fatalf("comment lost: %s", out)
	}
	if !strings.Contains(out, "\"other\"") {
		t.Fatalf("other server lost: %s", out)
	}
	if !strings.Contains(out, ServerName) {
		t.Fatalf("entry missing: %s", out)
	}
	if _, err := hujson.Standardize([]byte(out)); err != nil {
		t.Fatalf("not valid jsonc: %v", err)
	}
}

func TestJSONCIdempotent(t *testing.T) {
	doc, err := parseJSONC(nil)
	if err != nil {
		t.Fatal(err)
	}
	value := orderedObject{{"command", "x"}, {"args", []string{"serve"}}}
	if _, err := doc.upsert([]string{"mcpServers"}, ServerName, value); err != nil {
		t.Fatal(err)
	}
	doc2, err := parseJSONC(doc.bytes())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := doc2.upsert([]string{"mcpServers"}, ServerName, value)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second upsert reported a change")
	}
}

func TestJSONCReplaceKeepsNeighbours(t *testing.T) {
	base := []byte("{\n  \"mcpServers\": {\n    \"a\": {\"command\": \"a\"},\n    \"jack-ketch\": {\"command\": \"old\"},\n    \"b\": {\"command\": \"b\"}\n  }\n}\n")
	doc, err := parseJSONC(base)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := doc.upsert([]string{"mcpServers"}, ServerName, orderedObject{{"command", "new"}})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	out := string(doc.bytes())
	for _, want := range []string{`"a"`, `"b"`, `"new"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
	if strings.Contains(out, "old") {
		t.Fatalf("old entry still present: %s", out)
	}
}

func TestJSONCRemoveLastMember(t *testing.T) {
	base := []byte("{\n  \"mcpServers\": {\n    \"jack-ketch\": {\"command\": \"x\"}\n  }\n}\n")
	doc, err := parseJSONC(base)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := doc.remove([]string{"mcpServers"}, ServerName)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	out := doc.bytes()
	if !json.Valid(out) {
		t.Fatalf("not valid JSON: %s", out)
	}
	if strings.Contains(string(out), "mcpServers") {
		t.Fatalf("empty parent kept: %s", out)
	}
}

func TestJSONCNestedProjects(t *testing.T) {
	base := []byte("{\n  \"projects\": {\n    \"other\": {}\n  }\n}\n")
	doc, err := parseJSONC(base)
	if err != nil {
		t.Fatal(err)
	}
	path := []string{"projects", "/p/x", "mcpServers"}
	if _, err := doc.upsert(path, ServerName, orderedObject{{"command", "x"}}); err != nil {
		t.Fatal(err)
	}
	out := doc.bytes()
	doc2, err := parseJSONC(out)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok, err := doc2.get(path, ServerName)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.Contains(string(raw), "command") {
		t.Fatalf("entry not found: %s", raw)
	}
	if !strings.Contains(string(out), `"other"`) {
		t.Fatalf("existing project lost: %s", out)
	}
}

func TestJSONCDottedKey(t *testing.T) {
	doc, err := parseJSONC(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.upsert([]string{"amp.mcpServers"}, ServerName, orderedObject{{"command", "x"}}); err != nil {
		t.Fatal(err)
	}
	out := doc.bytes()
	if !json.Valid(out) {
		t.Fatalf("not valid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"amp.mcpServers"`) {
		t.Fatalf("dotted key missing: %s", out)
	}
}

func TestJSONCTopLevelArray(t *testing.T) {
	if _, err := parseJSONC([]byte("[]")); err == nil {
		t.Fatal("expected error for top level array")
	}
}

func TestJSONCNonObjectTable(t *testing.T) {
	doc, err := parseJSONC([]byte(`{"mcpServers": "x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.upsert([]string{"mcpServers"}, ServerName, orderedObject{{"command", "x"}}); err == nil {
		t.Fatal("expected error for non-object table")
	}
}
