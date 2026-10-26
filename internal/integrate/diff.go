package integrate

import (
	"fmt"
	"strings"
)

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func unifiedDiff(path string, before, after []byte) string {
	a := splitLines(string(before))
	b := splitLines(string(after))
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	if p == len(a) && p == len(b) {
		return ""
	}
	const ctx = 3
	start := max(0, p-ctx)
	endA, endB := len(a)-s, len(b)-s
	tailA, tailB := min(len(a), endA+ctx), min(len(b), endB+ctx)
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", path, path)
	oldStart := start + 1
	if len(a) == 0 {
		oldStart = 0
	}
	fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart, tailA-start, start+1, tailB-start)
	for i := start; i < p; i++ {
		sb.WriteString(" " + a[i] + "\n")
	}
	for i := p; i < endA; i++ {
		sb.WriteString("-" + a[i] + "\n")
	}
	for i := p; i < endB; i++ {
		sb.WriteString("+" + b[i] + "\n")
	}
	for i := endA; i < tailA; i++ {
		sb.WriteString(" " + a[i] + "\n")
	}
	return sb.String()
}
