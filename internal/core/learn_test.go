package core

import (
	"strings"
	"testing"
	"time"

	"jack-ketch/internal/config"
)

func TestInsertAppYAMLKeepsExisting(t *testing.T) {
	cur := []byte("# top comment\nversion: 3\nruntime:\n  security:\n    mode: trusted\napps:\n  api:\n    command: [go, run, ./cmd/api]\nlogging:\n  redact: ['token']\n")
	out, err := insertAppYAML(cur, "worker", learnedApp{Command: []string{"npm", "run", "worker"}, WorkDir: "/tmp/w"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# top comment", "runtime:", "logging:", "api:", "worker:", "npm"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if errs := config.Validate([]byte(out)); len(errs) > 0 {
		t.Fatalf("invalid yaml: %v\n%s", errs, out)
	}
	cfg, errs := config.ParseV3([]byte(out))
	if errs != nil {
		t.Fatalf("parse: %v", errs)
	}
	if got := cfg.Apps["worker"].Command; len(got) != 3 || got[0] != "npm" || got[2] != "worker" {
		t.Fatalf("worker command = %v", got)
	}
	if got := cfg.Apps["api"].Command; len(got) != 3 || got[0] != "go" {
		t.Fatalf("api command = %v", got)
	}
}

func TestInsertAppYAMLEmptyCreatesDoc(t *testing.T) {
	out, err := insertAppYAML(nil, "web", learnedApp{Command: []string{"npm", "run", "dev"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, errs := config.ParseV3([]byte(out))
	if errs != nil {
		t.Fatalf("parse: %v\n%s", errs, out)
	}
	if len(cfg.Apps) != 1 || cfg.Apps["web"].Command[0] != "npm" {
		t.Fatalf("apps = %+v\n%s", cfg.Apps, out)
	}
}

func TestParseRetention(t *testing.T) {
	if d, err := ParseRetention("7d"); err != nil || d != 7*24*time.Hour {
		t.Fatalf("7d -> %v, %v", d, err)
	}
	if d, err := ParseRetention("90m"); err != nil || d != 90*time.Minute {
		t.Fatalf("90m -> %v, %v", d, err)
	}
	if _, err := ParseRetention("x"); err == nil {
		t.Fatal("x should error")
	}
	if _, err := ParseRetention("forever"); err == nil {
		t.Fatal("forever should error")
	}
}
