package integrate

import (
	"bytes"
	"strings"
	"testing"
)

func yamlSpec() ServerSpec {
	return ServerSpec{Command: "/opt/jack-ketch/bin/jack-ketch", Args: []string{"serve", "--harness", "goose"}}
}

func TestYAMLGooseUpsertEmpty(t *testing.T) {
	out, err := yamlUpsertMap(nil, "extensions", ServerName, gooseEntry(yamlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"extensions:", ServerName + ":", "type: stdio", "enabled: true"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestYAMLGoosePreservesCommentsAndOthers(t *testing.T) {
	src := []byte("# keep me\nextensions:\n  other:\n    type: stdio\n")
	out, err := yamlUpsertMap(src, "extensions", ServerName, gooseEntry(yamlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "# keep me") {
		t.Fatalf("comment lost: %s", s)
	}
	if !strings.Contains(s, "other:") {
		t.Fatalf("other extension lost: %s", s)
	}
	if !strings.Contains(s, ServerName+":") {
		t.Fatalf("entry missing: %s", s)
	}
	out2, err := yamlUpsertMap(out, "extensions", ServerName, gooseEntry(yamlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatalf("not idempotent:\n%s\n---\n%s", out, out2)
	}
}

func TestYAMLGooseRemove(t *testing.T) {
	out, err := yamlUpsertMap(nil, "extensions", ServerName, gooseEntry(yamlSpec()))
	if err != nil {
		t.Fatal(err)
	}
	removed, err := yamlRemoveMap(out, "extensions", ServerName)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), ServerName) {
		t.Fatalf("entry not removed: %s", removed)
	}
}

func TestYAMLContinueList(t *testing.T) {
	spec := ServerSpec{Command: "/bin/jack-ketch", Args: []string{"serve"}}
	out, err := yamlUpsertList(nil, "mcpServers", ServerName, continueEntry(spec))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "mcpServers:") || !strings.Contains(s, "name: "+ServerName) {
		t.Fatalf("entry missing: %s", s)
	}
	spec.Args = []string{"serve", "--harness", "continue"}
	out2, err := yamlUpsertList(out, "mcpServers", ServerName, continueEntry(spec))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out2), "--harness") {
		t.Fatalf("entry not replaced: %s", out2)
	}
	if strings.Count(string(out2), "name: "+ServerName) != 1 {
		t.Fatalf("duplicate entry: %s", out2)
	}
	removed, err := yamlRemoveList(out2, "mcpServers", ServerName)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), ServerName) {
		t.Fatalf("entry not removed: %s", removed)
	}
}

func TestYAMLContinueBlockRoundTrip(t *testing.T) {
	spec := ServerSpec{Command: "/bin/jack-ketch", Args: []string{"serve"}}
	out, err := continueBlockFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	e, ok, err := readEntry(Target{Format: FormatContinueBlock}, out, nil)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if e.Command != spec.Command || len(e.Args) != 1 || e.Args[0] != "serve" {
		t.Fatalf("entry = %#v", e)
	}
}
