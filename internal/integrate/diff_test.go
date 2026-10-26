package integrate

import (
	"strings"
	"testing"
)

func TestUnifiedDiff(t *testing.T) {
	if d := unifiedDiff("f", []byte("a\nb\n"), []byte("a\nb\n")); d != "" {
		t.Fatalf("expected empty diff, got %q", d)
	}
	d := unifiedDiff("f", []byte("a\nb\nc\n"), []byte("a\nx\nb\nc\n"))
	if !strings.Contains(d, "+x") || !strings.Contains(d, " b") || !strings.Contains(d, " c") || strings.Contains(d, "-b") {
		t.Fatalf("insert diff:\n%s", d)
	}
	d = unifiedDiff("f", []byte("a\n"), []byte("a\nb\n"))
	if !strings.Contains(d, "+b") {
		t.Fatalf("append diff:\n%s", d)
	}
	d = unifiedDiff("f", nil, []byte("x\ny\n"))
	if !strings.Contains(d, "@@ -0,0 +1,2 @@") {
		t.Fatalf("new file diff:\n%s", d)
	}
}
