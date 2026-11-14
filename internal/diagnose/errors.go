package diagnose

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"jack-ketch/internal/logs"
	"jack-ketch/pkg/api"
)

type ErrLine struct {
	ID     uint64
	Time   time.Time
	Stream string
	Text   string
}

type ErrorScan struct {
	Lines    []ErrLine
	Root     string
	WorkDir  string
	Baseline map[string]bool
	Limit    int
}

const (
	KindCompile   = "compile_error"
	KindException = "exception"
	KindPanic     = "panic"
	KindTest      = "test_failure"
	KindLint      = "lint_error"
	KindLog       = "log_error"
	KindBrowser   = "browser_error"
	maxSample     = 8
	maxFrames     = 60
	maxLocations  = 5
)

var srcExt = `(?:go|tsx?|jsx?|mjs|cjs|mts|cts|html?|py|pyi|rs|java|kt|kts|scala|cs|fs|vb|rb|php|swift|c|cc|cpp|cxx|h|hh|hpp|m|mm|vue|svelte|astro|ex|exs|erl|dart|lua|zig|sol|proto|sql|graphql|prisma|razor|cshtml)`

var (
	reGoCompile  = regexp.MustCompile(`^(?:#\s+\S+\s*)?((?:\.{0,2}/)?[\w./@+-]+\.go):(\d+):(\d+): (.+)$`)
	reTSC        = regexp.MustCompile(`^((?:[A-Za-z]:)?[\w./\\@+-]+\.(?:tsx?|jsx?|mts|cts|vue|svelte))\((\d+),(\d+)\): (?:error|warning) (TS\d+): (.+)$`)
	reColonLoc   = regexp.MustCompile(`^((?:[A-Za-z]:)?[\w./\\@~+-]+\.` + srcExt + `):(\d+):(\d+):?\s+(?:(?:fatal )?error|Error|ERROR)(?:\[[\w-]+\])?:?\s*(.+)$`)
	reViteErr    = regexp.MustCompile(`(?i)^(?:\[[\w:-]+\]\s*)?(?:✘ \[ERROR\]|\[vite\][^:]*error|error during build|failed to compile|Module not found|ERROR in)\s*:?\s*(.*)$`)
	reRustHead   = regexp.MustCompile(`^error(?:\[(E\d+)\])?: (.+)$`)
	reRustLoc    = regexp.MustCompile(`^\s*--> ((?:[A-Za-z]:)?[\w./\\@+-]+):(\d+):(\d+)`)
	reCSharp     = regexp.MustCompile(`^((?:[A-Za-z]:)?[^\s(]+\.(?:cs|fs|vb|razor|cshtml))\((\d+),(\d+)\): error (\w+\d+): (.+?)(?: \[.+\])?$`)
	reTraceback  = regexp.MustCompile(`^\s*Traceback \(most recent call last\):`)
	rePyFrame    = regexp.MustCompile(`^\s+File "([^"]+)", line (\d+)`)
	rePyExc      = regexp.MustCompile(`^([A-Za-z_][\w.]*(?:Error|Exception|Exit|Interrupt|Warning)|AssertionError|KeyError|StopIteration)(?::\s*(.*))?$`)
	reJSExc      = regexp.MustCompile(`^\s*(?:Uncaught |Unhandled(?:PromiseRejection)?(?:Warning)?:?\s*|\[?ERROR\]?\s*)?([A-Z]\w*(?:Error|Exception))(?:\s*\[[\w-]+\])?(?::\s+(.*)|$)`)
	reJavaExc    = regexp.MustCompile(`^(?:Exception in thread "[^"]*" |Caused by: )?((?:[a-z_$][\w$]*\.)+[A-Z][\w$]*(?:Exception|Error))(?::\s*(.*))?$`)
	reDotnetExc  = regexp.MustCompile(`^\s*(?:Unhandled exception\. )?(System\.[\w.]+Exception|[\w]+\.[\w.]*[A-Z]\w*Exception)(?::\s*(.*))?$`)
	rePanic      = regexp.MustCompile(`^panic: (.+)$`)
	reFatalGo    = regexp.MustCompile(`^fatal error: (.+)$`)
	rePytestFail = regexp.MustCompile(`^FAILED (\S+?)::(\S+)(?: - (.+))?$`)
	rePytestLoc  = regexp.MustCompile(`^((?:[\w./-]+/)?[\w-]+\.py):(\d+): (\w+(?:Error|Exception)?)$`)
	reGoTestFail = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	reGoTestLoc  = regexp.MustCompile(`^\s+([\w./-]+_test\.go):(\d+): (.+)$`)
	reJestFail   = regexp.MustCompile(`^\s*● (.+?)(?: › (.+))?$`)
	reVitestFail = regexp.MustCompile(`^\s*(?:FAIL|×|✗)\s+(\S+\.(?:test|spec)\.[jt]sx?)(?:\s+>\s+(.+))?`)
	reESLintFile = regexp.MustCompile(`^((?:[A-Za-z]:)?[/\\.~]?[\w./\\@+-]+\.` + srcExt + `)$`)
	reESLintRow  = regexp.MustCompile(`^\s+(\d+):(\d+)\s+error\s+(.+?)(?:\s{2,}([\w/@.-]+))?$`)
	reLogError   = regexp.MustCompile(`(?i)(?:^|[\s\[|])(?:error|fatal|crit(?:ical)?|severe)(?:[\]|:]|\s|$)`)
	reLocParen   = regexp.MustCompile(`\(?(?:file://)?((?:[A-Za-z]:)?(?:webpack:///)?[\w./\\@~+-]*[\w-]\.` + srcExt + `):(\d+)(?::(\d+))?\)?`)
	reJavaFrame  = regexp.MustCompile(`\(([\w$-]+\.(?:java|kt|scala|groovy)):(\d+)\)`)
	reDotnetLoc  = regexp.MustCompile(` in ((?:[A-Za-z]:)?[^\s:]+\.(?:cs|fs|vb)):line (\d+)`)
	reBrowser    = regexp.MustCompile(`^\[browser\] error: (.+?)(?: \(at [^)]+\))?(?: page=\S+)?$`)
	reQuoted     = regexp.MustCompile(`'[^']*'|"[^"]*"|` + "`[^`]*`")
)

type errBlock struct {
	kind    string
	message string
	first   ErrLine
	lines   []string
	locs    []api.ErrorLocation
}

type scanner struct {
	in     ErrorScan
	lines  []ErrLine
	blocks []errBlock
	exists map[string]bool
}

func ExtractErrors(in ErrorScan) []api.ErrorGroup {
	sc := &scanner{in: in, exists: map[string]bool{}}
	sc.lines = make([]ErrLine, len(in.Lines))
	for i, l := range in.Lines {
		l.Text = strings.TrimRight(StripANSI(l.Text), "\r")
		sc.lines[i] = l
	}
	sc.run()
	return sc.group()
}

func (sc *scanner) run() {
	for i := 0; i < len(sc.lines); {
		next := sc.match(i)
		if next <= i {
			next = i + 1
		}
		i = next
	}
}

func (sc *scanner) add(kind, msg string, first ErrLine, lines []string, locs ...api.ErrorLocation) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = strings.TrimSpace(first.Text)
	}
	if len(msg) > 400 {
		msg = msg[:400] + "…"
	}
	if len(lines) > maxSample {
		lines = lines[:maxSample]
	}
	sc.blocks = append(sc.blocks, errBlock{kind: kind, message: msg, first: first, lines: lines, locs: locs})
}

func (sc *scanner) match(i int) int {
	l := sc.lines[i]
	t := l.Text
	if strings.TrimSpace(t) == "" {
		return i + 1
	}
	if m := reBrowser.FindStringSubmatch(t); m != nil {
		return sc.trace(i, KindBrowser, m[1])
	}
	if m := reTSC.FindStringSubmatch(t); m != nil {
		sc.add(KindCompile, m[4]+": "+m[5], l, []string{t}, sc.loc(m[1], m[2], m[3]))
		return i + 1
	}
	if m := reCSharp.FindStringSubmatch(t); m != nil {
		sc.add(KindCompile, m[4]+": "+m[5], l, []string{t}, sc.loc(m[1], m[2], m[3]))
		return i + 1
	}
	if m := reGoCompile.FindStringSubmatch(t); m != nil && !strings.Contains(t, "_test.go:") {
		sc.add(KindCompile, m[4], l, []string{t}, sc.loc(m[1], m[2], m[3]))
		return i + 1
	}
	if m := reColonLoc.FindStringSubmatch(t); m != nil {
		sc.add(KindCompile, m[4], l, []string{t}, sc.loc(m[1], m[2], m[3]))
		return i + 1
	}
	if m := reRustHead.FindStringSubmatch(t); m != nil && !strings.HasPrefix(m[2], "could not compile") && !strings.HasPrefix(m[2], "aborting due to") {
		end, locs, sample := sc.follow(i+1, 6, func(s string) bool {
			return reRustLoc.MatchString(s) || strings.HasPrefix(strings.TrimSpace(s), "|") || strings.TrimSpace(s) == ""
		})
		for j := i + 1; j < end; j++ {
			if lm := reRustLoc.FindStringSubmatch(sc.lines[j].Text); lm != nil {
				locs = append([]api.ErrorLocation{sc.loc(lm[1], lm[2], lm[3])}, locs...)
				break
			}
		}
		msg := m[2]
		if m[1] != "" {
			msg = m[1] + ": " + msg
		}
		sc.add(KindCompile, msg, l, append([]string{t}, sample...), locs...)
		return end
	}
	if m := reESLintFile.FindStringSubmatch(t); m != nil && i+1 < len(sc.lines) && reESLintRow.MatchString(sc.lines[i+1].Text) {
		j := i + 1
		for ; j < len(sc.lines); j++ {
			r := reESLintRow.FindStringSubmatch(sc.lines[j].Text)
			if r == nil {
				break
			}
			msg := r[3]
			if r[4] != "" {
				msg += " (" + r[4] + ")"
			}
			sc.add(KindLint, msg, sc.lines[j], []string{m[1], sc.lines[j].Text}, sc.loc(m[1], r[1], r[2]))
		}
		return j
	}
	if reTraceback.MatchString(t) {
		return sc.python(i)
	}
	if m := rePanic.FindStringSubmatch(t); m != nil {
		return sc.trace(i, KindPanic, m[1])
	}
	if m := reFatalGo.FindStringSubmatch(t); m != nil {
		return sc.trace(i, KindPanic, m[1])
	}
	if m := rePytestFail.FindStringSubmatch(t); m != nil {
		msg := m[2]
		if m[3] != "" {
			msg += ": " + m[3]
		}
		sc.add(KindTest, msg, l, []string{t}, sc.loc(m[1], "", ""))
		return i + 1
	}
	if m := rePytestLoc.FindStringSubmatch(t); m != nil {
		sc.add(KindTest, m[3], l, []string{t}, sc.loc(m[1], m[2], ""))
		return i + 1
	}
	if m := reGoTestFail.FindStringSubmatch(t); m != nil {
		var locs []api.ErrorLocation
		sample := []string{t}
		msg := m[1]
		j := i + 1
		for ; j < len(sc.lines) && j < i+12; j++ {
			lm := reGoTestLoc.FindStringSubmatch(sc.lines[j].Text)
			if lm == nil {
				break
			}
			if len(locs) == 0 {
				msg = m[1] + ": " + strings.TrimSpace(lm[3])
			}
			locs = append(locs, sc.loc(lm[1], lm[2], ""))
			sample = append(sample, sc.lines[j].Text)
		}
		sc.add(KindTest, msg, l, sample, locs...)
		return j
	}
	if m := reJestFail.FindStringSubmatch(t); m != nil && strings.HasPrefix(strings.TrimSpace(t), "●") && !strings.Contains(t, "Console") {
		end, locs, sample := sc.follow(i+1, 40, func(s string) bool {
			return !reJestFail.MatchString(s) || !strings.HasPrefix(strings.TrimSpace(s), "●")
		})
		msg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "●"))
		for _, s := range sample {
			if st := strings.TrimSpace(s); st != "" && !logs.IsStackFrame(s) && !strings.HasPrefix(st, ">") && !strings.HasPrefix(st, "|") && !isDigitsPrefix(st) {
				msg += ": " + st
				break
			}
		}
		sc.add(KindTest, msg, l, append([]string{t}, sample...), locs...)
		return end
	}
	if m := reVitestFail.FindStringSubmatch(t); m != nil {
		msg := m[1]
		if m[2] != "" {
			msg += " > " + m[2]
		}
		sc.add(KindTest, msg, l, []string{t}, sc.loc(m[1], "", ""))
		return i + 1
	}
	if m := reJavaExc.FindStringSubmatch(t); m != nil {
		return sc.trace(i, KindException, joinMsg(m[1], m[2]))
	}
	if m := reDotnetExc.FindStringSubmatch(t); m != nil {
		return sc.trace(i, KindException, joinMsg(m[1], m[2]))
	}
	if m := reJSExc.FindStringSubmatch(t); m != nil && (m[2] != "" || i+1 < len(sc.lines) && logs.IsStackFrame(sc.lines[i+1].Text)) {
		return sc.trace(i, KindException, joinMsg(m[1], m[2]))
	}
	if m := reViteErr.FindStringSubmatch(t); m != nil {
		end, locs, sample := sc.follow(i+1, 12, func(s string) bool { return strings.TrimSpace(s) != "" })
		msg := m[1]
		if msg == "" && len(sample) > 0 {
			msg = strings.TrimSpace(sample[0])
		}
		sc.add(KindCompile, msg, l, append([]string{t}, sample...), locs...)
		return end
	}
	if logs.IsStackFrame(t) {
		return i + 1
	}
	if lvl := logs.ParseLevel(t); lvl == "error" || lvl == "" && reLogError.MatchString(t) && !benign(t) {
		var locs []api.ErrorLocation
		if loc, ok := sc.lineLoc(t); ok {
			locs = append(locs, loc)
		}
		end, flocs, sample := sc.follow(i+1, maxFrames, logs.IsStackFrame)
		sc.add(KindLog, t, l, append([]string{t}, sample...), append(locs, flocs...)...)
		return end
	}
	return i + 1
}

func benign(t string) bool {
	lt := strings.ToLower(t)
	for _, s := range []string{"0 errors", "no errors", "errors: 0", "error_count=0", "without errors", "errorhandler", "error.html", "errors.go", "onerror", "error boundary"} {
		if strings.Contains(lt, s) {
			return true
		}
	}
	return false
}

func isDigitsPrefix(s string) bool {
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}

func joinMsg(name, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return name
	}
	return name + ": " + msg
}

func (sc *scanner) follow(from, max int, keep func(string) bool) (int, []api.ErrorLocation, []string) {
	var locs []api.ErrorLocation
	var sample []string
	j := from
	for ; j < len(sc.lines) && j < from+max; j++ {
		s := sc.lines[j].Text
		if keep != nil && !keep(s) {
			break
		}
		sample = append(sample, s)
		if loc, ok := sc.lineLoc(s); ok {
			locs = append(locs, loc)
		}
	}
	return j, locs, sample
}

func (sc *scanner) trace(i int, kind, msg string) int {
	first := sc.lines[i]
	sample := []string{first.Text}
	var locs []api.ErrorLocation
	if loc, ok := sc.lineLoc(first.Text); ok {
		locs = append(locs, loc)
	}
	j := i + 1
	for ; j < len(sc.lines) && j < i+1+maxFrames; j++ {
		s := sc.lines[j].Text
		st := strings.TrimSpace(s)
		frame := logs.IsStackFrame(s) || strings.HasPrefix(st, "goroutine ") || strings.HasPrefix(st, "Caused by:") || strings.HasPrefix(st, "--- End of") || strings.HasPrefix(st, "[recovered]") || kind == KindPanic && (st == "" || strings.HasPrefix(st, "[signal") || strings.HasPrefix(st, "[recovered"))
		if !frame {
			if kind == KindException && st != "" && j == i+1 && !reLocParen.MatchString(s) && len(st) < 300 && !reLogError.MatchString(s) {
				msg += " " + st
				sample = append(sample, s)
				continue
			}
			break
		}
		if len(sample) < maxSample {
			sample = append(sample, s)
		}
		if loc, ok := sc.lineLoc(s); ok {
			locs = append(locs, loc)
		}
	}
	sc.add(kind, msg, first, sample, locs...)
	return j
}

func (sc *scanner) python(i int) int {
	first := sc.lines[i]
	sample := []string{first.Text}
	var locs []api.ErrorLocation
	j := i + 1
	for ; j < len(sc.lines) && j < i+1+maxFrames*2; j++ {
		s := sc.lines[j].Text
		if m := rePyFrame.FindStringSubmatch(s); m != nil {
			locs = append([]api.ErrorLocation{sc.loc(m[1], m[2], "")}, locs...)
			sample = append(sample, s)
			continue
		}
		if strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t") || strings.TrimSpace(s) == "" {
			continue
		}
		break
	}
	msg := "Traceback"
	if j < len(sc.lines) {
		exc := strings.TrimSpace(sc.lines[j].Text)
		if m := rePyExc.FindStringSubmatch(exc); m != nil {
			msg = joinMsg(m[1], m[2])
		} else {
			msg = exc
		}
		sample = append(sample, sc.lines[j].Text)
		j++
	}
	if len(sample) > maxSample {
		sample = append(sample[:2], sample[len(sample)-(maxSample-2):]...)
	}
	sc.add(KindException, msg, first, sample, locs...)
	return j
}

func (sc *scanner) lineLoc(s string) (api.ErrorLocation, bool) {
	if m := rePyFrame.FindStringSubmatch(s); m != nil {
		return sc.loc(m[1], m[2], ""), true
	}
	if m := reDotnetLoc.FindStringSubmatch(s); m != nil {
		return sc.loc(m[1], m[2], ""), true
	}
	if m := reJavaFrame.FindStringSubmatch(s); m != nil {
		return sc.loc(m[1], m[2], ""), true
	}
	if m := reLocParen.FindStringSubmatch(s); m != nil {
		return sc.loc(m[1], m[2], m[3]), true
	}
	return api.ErrorLocation{}, false
}

func (sc *scanner) loc(file, line, col string) api.ErrorLocation {
	file = strings.TrimPrefix(file, "file://")
	file = strings.TrimPrefix(file, "webpack:///")
	file = strings.TrimPrefix(file, "./")
	ln, _ := strconv.Atoi(line)
	cn, _ := strconv.Atoi(col)
	loc := api.ErrorLocation{File: file, Line: ln, Column: cn}
	if logs.IsDependencyFrame(file) || strings.Contains(file, "node_modules") || strings.Contains(file, "/vendor/") {
		return loc
	}
	abs := file
	if !filepath.IsAbs(abs) {
		base := sc.in.WorkDir
		if base == "" {
			base = sc.in.Root
		}
		abs = filepath.Join(base, file)
	}
	if sc.in.Root != "" {
		if rel, err := filepath.Rel(sc.in.Root, abs); err == nil && !strings.HasPrefix(rel, "..") {
			if sc.fileExists(abs) {
				loc.File = filepath.ToSlash(rel)
				loc.InProject = true
				return loc
			}
			if sc.in.WorkDir != "" && sc.in.WorkDir != sc.in.Root {
				alt := filepath.Join(sc.in.Root, file)
				if sc.fileExists(alt) {
					loc.File = filepath.ToSlash(file)
					loc.InProject = true
				}
			}
		}
	}
	return loc
}

func (sc *scanner) fileExists(p string) bool {
	if v, ok := sc.exists[p]; ok {
		return v
	}
	st, err := os.Stat(p)
	ok := err == nil && !st.IsDir()
	sc.exists[p] = ok
	return ok
}

func fingerprint(kind, msg string, locs []api.ErrorLocation) string {
	key := kind + "|" + normalizeMessage(msg)
	for _, l := range locs {
		if l.InProject {
			key += "|" + l.File + ":" + strconv.Itoa(l.Line)
			break
		}
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:6])
}

func normalizeMessage(msg string) string {
	s := reQuoted.ReplaceAllString(msg, "<s>")
	return logs.Normalize(s)
}

func Fingerprints(in ErrorScan) map[string]bool {
	out := map[string]bool{}
	for _, g := range ExtractErrors(in) {
		out[g.Fingerprint] = true
	}
	return out
}

func (sc *scanner) group() []api.ErrorGroup {
	idx := map[string]int{}
	var groups []api.ErrorGroup
	for _, b := range sc.blocks {
		locs := orderLocations(b.locs)
		fp := fingerprint(b.kind, b.message, locs)
		ts := ""
		if !b.first.Time.IsZero() {
			ts = b.first.Time.Format(time.RFC3339Nano)
		}
		if k, ok := idx[fp]; ok {
			groups[k].Count++
			if ts != "" {
				groups[k].LastSeen = ts
			}
			continue
		}
		idx[fp] = len(groups)
		groups = append(groups, api.ErrorGroup{
			Fingerprint: fp,
			Kind:        b.kind,
			Message:     b.message,
			Count:       1,
			FirstSeen:   ts,
			LastSeen:    ts,
			FirstID:     b.first.ID,
			Stream:      b.first.Stream,
			Locations:   locs,
			Sample:      b.lines,
			New:         sc.in.Baseline != nil && !sc.in.Baseline[fp],
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		ri, rj := kindRank(groups[i].Kind), kindRank(groups[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return groups[i].FirstID < groups[j].FirstID
	})
	if sc.in.Limit > 0 && len(groups) > sc.in.Limit {
		groups = groups[:sc.in.Limit]
	}
	return groups
}

func kindRank(k string) int {
	switch k {
	case KindCompile:
		return 0
	case KindPanic:
		return 1
	case KindException:
		return 2
	case KindTest:
		return 3
	case KindBrowser:
		return 4
	case KindLint:
		return 5
	}
	return 6
}

func orderLocations(in []api.ErrorLocation) []api.ErrorLocation {
	seen := map[string]bool{}
	var proj, other []api.ErrorLocation
	for _, l := range in {
		if l.File == "" {
			continue
		}
		k := l.File + ":" + strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Column)
		if seen[k] {
			continue
		}
		seen[k] = true
		if l.InProject {
			proj = append(proj, l)
		} else {
			other = append(other, l)
		}
	}
	out := append(proj, other...)
	if len(out) > maxLocations {
		out = out[:maxLocations]
	}
	return out
}
