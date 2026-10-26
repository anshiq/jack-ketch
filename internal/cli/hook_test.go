package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHookPretooluseDeniesDevServers(t *testing.T) {
	cases := []string{
		`{"tool_name":"Bash","tool_input":{"command":"npm run dev"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"cd web && pnpm dev"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"echo hi; nodemon src"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"./mvnw spring-boot:run"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"uvicorn app:app --reload"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"cargo watch -x run"}}`,
	}
	for _, in := range cases {
		var out bytes.Buffer
		if err := runHookPretooluse(strings.NewReader(in), &out); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if !strings.Contains(out.String(), `"permissionDecision":"deny"`) {
			t.Fatalf("%s: expected deny, got %q", in, out.String())
		}
		if !strings.Contains(out.String(), `"hookEventName":"PreToolUse"`) {
			t.Fatalf("%s: wrong hook event in %q", in, out.String())
		}
	}
}

func TestHookPretooluseIgnoresOtherCommands(t *testing.T) {
	cases := []string{
		`{"tool_name":"Bash","tool_input":{"command":"npm test"}}`,
		`{"tool_name":"Read","tool_input":{"command":"npm run dev"}}`,
		`{"tool_name":"Bash","tool_input":{}}`,
		`{"tool_name":"Bash","tool_input":{"command":"go test ./..."}}`,
	}
	for _, in := range cases {
		var out bytes.Buffer
		if err := runHookPretooluse(strings.NewReader(in), &out); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s: expected silence, got %q", in, out.String())
		}
	}
}

func TestHookPretooluseIgnoresMalformedInput(t *testing.T) {
	var out bytes.Buffer
	if err := runHookPretooluse(strings.NewReader("not json"), &out); err != nil {
		t.Fatalf("runHookPretooluse: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected silence, got %q", out.String())
	}
}
