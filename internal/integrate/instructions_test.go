package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertInstructionBlockEmpty(t *testing.T) {
	got, err := upsertInstructionBlock("")
	if err != nil {
		t.Fatal(err)
	}
	if got != instructionBlock() {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestUpsertInstructionBlockKeepsUserContent(t *testing.T) {
	user := "# My rules\n\nbe nice\n"
	got, err := upsertInstructionBlock(user)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, user) || !strings.Contains(got, instructionsStart) {
		t.Fatalf("user content lost or block missing: %q", got)
	}
	again, err := upsertInstructionBlock(got)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("second upsert changed content:\n%q\n%q", got, again)
	}
}

func TestUpsertInstructionBlockReplacesStale(t *testing.T) {
	stale := "top\n\n" + instructionsStart + "\nold body\n" + instructionsEnd + "\n\nbottom\n"
	got, err := upsertInstructionBlock(stale)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "old body") || !strings.Contains(got, "top") || !strings.Contains(got, "bottom") {
		t.Fatalf("unexpected content: %q", got)
	}
	if strings.Count(got, instructionsStart) != 1 {
		t.Fatalf("expected one block: %q", got)
	}
	again, err := upsertInstructionBlock(got)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent:\n%q\n%q", got, again)
	}
}

func TestUpsertInstructionBlockCollapsesDuplicates(t *testing.T) {
	dup := instructionBlock() + "\nmiddle\n\n" + instructionBlock()
	got, err := upsertInstructionBlock(dup)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, instructionsStart) != 1 || !strings.Contains(got, "middle") {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestUpsertInstructionBlockRejectsUnbalanced(t *testing.T) {
	for _, in := range []string{
		instructionsStart + "\nbody\n",
		"body\n" + instructionsEnd + "\n",
		instructionsEnd + "\n" + instructionsStart + "\n",
	} {
		if _, err := upsertInstructionBlock(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestStripInstructionBlocks(t *testing.T) {
	user := "# My rules\n\nbe nice\n"
	withBlock, err := upsertInstructionBlock(user)
	if err != nil {
		t.Fatal(err)
	}
	got, removed, err := stripInstructionBlocks(withBlock)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if got != user {
		t.Fatalf("got %q want %q", got, user)
	}
	only, removed, err := stripInstructionBlocks(instructionBlock())
	if err != nil || !removed || only != "" {
		t.Fatalf("only=%q removed=%v err=%v", only, removed, err)
	}
	same, removed, err := stripInstructionBlocks(user)
	if err != nil || removed || same != user {
		t.Fatalf("same=%q removed=%v err=%v", same, removed, err)
	}
}

func TestInstallWritesInstructionsAtUserScopeOnly(t *testing.T) {
	setTestEnv(t)
	project := t.TempDir()
	env := currentPathEnv(project)
	h, _ := Lookup("claude-code")
	path, _ := instructionPath(h.Instructions, env, false)
	writeFile(t, path, []byte("# mine\n"))

	if _, err := Install("claude-code", ScopeProject, Options{ProjectDir: project}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), instructionsStart) {
		t.Fatal("project scope must not touch global instructions")
	}

	if _, err := Install("claude-code", ScopeUser, Options{ProjectDir: project}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# mine\n") || !strings.Contains(string(data), instructionsStart) {
		t.Fatalf("unexpected content: %q", data)
	}

	if _, err := Remove("claude-code", ScopeUser, Options{ProjectDir: project}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "# mine\n" {
		t.Fatalf("remove left content: %q", data)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
}

func TestStripInstructionBlocksPreservesSpacing(t *testing.T) {
	cases := []string{
		"top\n\nbottom\n",
		"top\nbottom\n",
		"top\n\n\nbottom\n",
		"top\n",
	}
	for _, orig := range cases {
		withBlock := orig
		if i := strings.Index(orig, "bottom"); i >= 0 {
			gap := orig[len("top"):i]
			withBlock = "top" + gap + instructionBlock() + gap[:len(gap)-1] + "bottom\n"
		} else {
			withBlock = orig + "\n" + instructionBlock()
		}
		got, removed, err := stripInstructionBlocks(withBlock)
		if err != nil || !removed {
			t.Fatalf("%q: removed=%v err=%v", orig, removed, err)
		}
		if got != orig {
			t.Errorf("got %q want %q", got, orig)
		}
	}
}

func TestUpsertInstructionBlockKeepsHandTrimmedFile(t *testing.T) {
	trimmed := "# mine\n\n" + strings.TrimSuffix(instructionBlock(), "\n")
	got, err := upsertInstructionBlock(trimmed)
	if err != nil {
		t.Fatal(err)
	}
	if got != trimmed {
		t.Fatalf("changed hand-trimmed file: %q", got)
	}
}

func TestOpencodeInstructionsUseClaudeFileWhenOwnMissing(t *testing.T) {
	setTestEnv(t)
	env := currentPathEnv("")
	claude := filepath.Join(env.home, ".claude", "CLAUDE.md")
	writeFile(t, claude, []byte("# claude rules\n"))
	h, _ := Lookup("opencode")

	path, shared := instructionPath(h.Instructions, env, false)
	if path != claude || !shared {
		t.Fatalf("path=%q shared=%v", path, shared)
	}
	if _, err := Install("opencode", ScopeUser, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claude)
	if !strings.Contains(string(data), instructionsStart) || !strings.HasPrefix(string(data), "# claude rules\n") {
		t.Fatalf("unexpected claude file: %q", data)
	}
	own := filepath.Join(env.xdgConfig, "opencode", "AGENTS.md")
	if fileExists(own) {
		t.Fatal("opencode AGENTS.md must not be created when the Claude file is its fallback")
	}
	if st := InstructionStatus(h, env); st == nil || !st.Installed || !st.UpToDate || !st.Shared {
		t.Fatalf("status: %+v", st)
	}
	if _, err := Remove("opencode", ScopeUser, Options{}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(claude)
	if !strings.Contains(string(data), instructionsStart) {
		t.Fatal("removing opencode must leave the shared Claude file alone")
	}
}

func TestOpencodeInstructionsPreferOwnFile(t *testing.T) {
	setTestEnv(t)
	env := currentPathEnv("")
	writeFile(t, filepath.Join(env.home, ".claude", "CLAUDE.md"), []byte("# claude rules\n"))
	own := filepath.Join(env.xdgConfig, "opencode", "AGENTS.md")
	writeFile(t, own, []byte("# own\n"))
	h, _ := Lookup("opencode")
	path, shared := instructionPath(h.Instructions, env, false)
	if path != own || shared {
		t.Fatalf("path=%q shared=%v", path, shared)
	}
}

func TestGooseInstructionsTargetGoosehints(t *testing.T) {
	setTestEnv(t)
	env := currentPathEnv("")
	if err := os.MkdirAll(filepath.Join(env.xdgConfig, "goose"), 0o755); err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("goose")
	path, _ := instructionPath(h.Instructions, env, false)
	if filepath.Base(path) != ".goosehints" {
		t.Fatalf("path=%q", path)
	}
}

func TestInstructionStatusStates(t *testing.T) {
	setTestEnv(t)
	env := currentPathEnv("")
	h, _ := Lookup("claude-code")
	path, _ := instructionPath(h.Instructions, env, false)

	st := InstructionStatus(h, env)
	if st == nil || st.Installed || st.FileExists {
		t.Fatalf("missing file: %+v", st)
	}
	writeFile(t, path, []byte("# mine\n"))
	if st = InstructionStatus(h, env); st.Installed || !st.FileExists {
		t.Fatalf("no block: %+v", st)
	}
	writeFile(t, path, []byte(instructionsStart+"\nold\n"+instructionsEnd+"\n"))
	if st = InstructionStatus(h, env); !st.Installed || st.UpToDate {
		t.Fatalf("stale: %+v", st)
	}
	writeFile(t, path, []byte(instructionBlock()))
	if st = InstructionStatus(h, env); !st.Installed || !st.UpToDate {
		t.Fatalf("current: %+v", st)
	}
	cursor, _ := Lookup("cursor")
	if InstructionStatus(cursor, env) != nil {
		t.Fatal("harness without instructions must report nil")
	}
}
