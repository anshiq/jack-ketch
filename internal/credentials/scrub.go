package credentials

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

const minScrubLen = 4

type Secret struct {
	Label string
	Value string
}

type Scrubber struct {
	replacer *strings.Replacer
	literals []string
}

func Variants(v string) []string {
	if len(v) < minScrubLen {
		return nil
	}
	set := map[string]bool{v: true}
	add := func(s string) {
		if len(s) >= minScrubLen {
			set[s] = true
		}
	}
	add(url.QueryEscape(v))
	add(url.PathEscape(v))
	add(base64.StdEncoding.EncodeToString([]byte(v)))
	add(base64.RawStdEncoding.EncodeToString([]byte(v)))
	add(base64.URLEncoding.EncodeToString([]byte(v)))
	add(base64.RawURLEncoding.EncodeToString([]byte(v)))
	if b, err := json.Marshal(v); err == nil && len(b) > 2 {
		once := string(b[1 : len(b)-1])
		add(once)
		if b2, err := json.Marshal(once); err == nil && len(b2) > 2 {
			add(string(b2[1 : len(b2)-1]))
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	return out
}

func NewScrubber(secrets []Secret) *Scrubber {
	type pair struct{ old, label string }
	var pairs []pair
	seen := map[string]bool{}
	for _, sec := range secrets {
		label := "[credential:" + sec.Label + "]"
		for _, v := range Variants(sec.Value) {
			if seen[v] {
				continue
			}
			seen[v] = true
			pairs = append(pairs, pair{v, label})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].old) > len(pairs[j].old) })
	s := &Scrubber{}
	if len(pairs) == 0 {
		return s
	}
	args := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		args = append(args, p.old, p.label)
		s.literals = append(s.literals, p.old)
	}
	s.replacer = strings.NewReplacer(args...)
	return s
}

func (s *Scrubber) Empty() bool { return s == nil || s.replacer == nil }

func (s *Scrubber) Scrub(in string) string {
	if s.Empty() || in == "" {
		return in
	}
	return s.replacer.Replace(in)
}

func (s *Scrubber) ScrubMap(in map[string]string) map[string]string {
	if s.Empty() || len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = s.Scrub(v)
	}
	return out
}

func (s *Scrubber) Literals() []string {
	if s == nil {
		return nil
	}
	return s.literals
}
