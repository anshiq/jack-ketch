// Log redaction: compiled regex rules applied to served log output
// (GetLogs/Search/Tail). Secrets never leave the daemon unmasked when a
// rule matches; env redaction (RedactEnv) is independent and stays.
package config

import (
	"regexp"
	"strings"
)

// Redactor masks configured patterns with ***.
type Redactor struct {
	res  []*regexp.Regexp
	lits *strings.Replacer
}

// CompileRedactor compiles patterns; invalid ones are skipped (and warned
// about at Validate time by callers that check).
func CompileRedactor(patterns []string) *Redactor {
	r := &Redactor{}
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			r.res = append(r.res, re)
		}
	}
	return r
}

func (r *Redactor) WithLiterals(lits []string) *Redactor {
	out := &Redactor{}
	if r != nil {
		out.res = r.res
		out.lits = r.lits
	}
	if len(lits) == 0 {
		return out
	}
	args := make([]string, 0, len(lits)*2)
	for _, l := range lits {
		if l != "" {
			args = append(args, l, "***")
		}
	}
	if len(args) > 0 {
		out.lits = strings.NewReplacer(args...)
	}
	return out
}

// Empty reports whether no rules are configured.
func (r *Redactor) Empty() bool { return r == nil || (len(r.res) == 0 && r.lits == nil) }

// Redact returns the line with all matches replaced by ***.
func (r *Redactor) Redact(line string) string {
	if r.Empty() {
		return line
	}
	if r.lits != nil {
		line = r.lits.Replace(line)
	}
	for _, re := range r.res {
		line = re.ReplaceAllString(line, "***")
	}
	return line
}
