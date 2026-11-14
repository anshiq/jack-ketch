package logs

import (
	"regexp"
	"strconv"
	"strings"
)

type Compacted struct {
	Entry
	Count  int
	LastID uint64
	Folded int
}

var (
	ansiSeq  = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	tsPrefix = regexp.MustCompile(`^\s*(\[?\d{4}-\d{2}-\d{2}[T ][\d:.,]+(Z|[+-]\d{2}:?\d{2})?\]?|\[?\d{2}:\d{2}:\d{2}(\.\d+)?\]?)\s*`)
	uuidRe   = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	hexRe    = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b[0-9a-f]{12,}\b`)
	numRe    = regexp.MustCompile(`\d+(\.\d+)?`)
	frameRes = []*regexp.Regexp{
		regexp.MustCompile(`^\s+at\s`),
		regexp.MustCompile(`^\s+File "[^"]+", line \d+`),
		regexp.MustCompile(`^\s+\.\.\. \d+ (more|common frames omitted)`),
		regexp.MustCompile(`^\s*\S+\.go:\d+( \+0x[0-9a-f]+)?$`),
		regexp.MustCompile(`^(created by )?[\w./*-]+\.[\w*()\[\]]+\(.*\)$`),
		regexp.MustCompile(`^\s+\d+: [\w:<>]+`),
		regexp.MustCompile(`^\s+at [\w$.<>]+\(`),
	}
	depMarkers = []string{
		"node_modules", "site-packages", "dist-packages", "/usr/lib/", "/usr/local/lib/",
		"/go/pkg/mod/", "/pkg/mod/", "/usr/local/go/src/", "/share/go/src/", "/go/src/runtime/", "node:internal", "<frozen ",
		"/rustc/", "/.cargo/registry/", "java.base/", "jdk.internal", "sun.reflect", "org.springframework",
		"Microsoft.AspNetCore", "System.Private.CoreLib", "/lib/python", "webpack/bootstrap",
	}
)

func Normalize(line string) string {
	s := ansiSeq.ReplaceAllString(line, "")
	s = tsPrefix.ReplaceAllString(s, "")
	s = uuidRe.ReplaceAllString(s, "<uuid>")
	s = hexRe.ReplaceAllString(s, "<hex>")
	s = numRe.ReplaceAllString(s, "#")
	return strings.TrimSpace(s)
}

func IsStackFrame(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	for _, re := range frameRes {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func IsDependencyFrame(line string) bool {
	for _, m := range depMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

const keepLeadingFrames = 3

func Compact(entries []Entry) []Compacted {
	out := make([]Compacted, 0, len(entries))
	framesInRun := 0
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if IsStackFrame(e.Line) {
			framesInRun++
			if framesInRun > keepLeadingFrames && IsDependencyFrame(e.Line) {
				j := i
				for j < len(entries) && IsStackFrame(entries[j].Line) && IsDependencyFrame(entries[j].Line) {
					j++
				}
				n := j - i
				if n > 1 {
					out = append(out, Compacted{
						Entry:  Entry{ID: e.ID, Timestamp: e.Timestamp, Stream: e.Stream, Line: "    … " + plural(n, "dependency frame") + " folded"},
						Count:  1,
						LastID: entries[j-1].ID,
						Folded: n,
					})
					i = j - 1
					continue
				}
			}
		} else {
			framesInRun = 0
		}
		if k := len(out) - 1; k >= 0 && out[k].Folded == 0 && out[k].Stream == e.Stream && sameShape(out[k].Line, e.Line) {
			out[k].Count++
			out[k].LastID = e.ID
			continue
		}
		out = append(out, Compacted{Entry: e, Count: 1, LastID: e.ID})
	}
	return out
}

func sameShape(a, b string) bool {
	if a == b {
		return true
	}
	if IsStackFrame(a) || IsStackFrame(b) {
		return false
	}
	na, nb := Normalize(a), Normalize(b)
	return na != "" && na == nb
}

func plural(n int, word string) string {
	s := strconv.Itoa(n) + " " + word
	if n != 1 {
		s += "s"
	}
	return s
}
