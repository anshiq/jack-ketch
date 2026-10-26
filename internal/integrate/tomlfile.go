package integrate

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	tomlOurHeader     = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\.\s*(?:"jack-ketch"|'jack-ketch'|jack-ketch)\s*(?:\.[^\]]*)?\]\s*(?:#.*)?$`)
	tomlAnyHeader     = regexp.MustCompile(`^\s*\[`)
	tomlServersHeader = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\]\s*(?:#.*)?$`)
	tomlInlineInTable = regexp.MustCompile(`^\s*(?:"jack-ketch"|'jack-ketch'|jack-ketch)\s*=`)
	tomlDottedAtRoot  = regexp.MustCompile(`^\s*mcp_servers\s*\.\s*(?:"jack-ketch"|'jack-ketch'|jack-ketch)\s*[.=]`)
	tomlCommandLine   = regexp.MustCompile(`^\s*command\s*=\s*"((?:[^"\\]|\\.)*)"`)
	tomlArgsLine      = regexp.MustCompile(`^\s*args\s*=\s*\[(.*)\]`)
	tomlQuoted        = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	tomlBareKey       = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

var errUnmanagedTOML = errors.New("jack-ketch is configured with an inline or dotted TOML form; remove it by hand and retry")

func collapseBlanks(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func tomlStrip(src []byte) ([]byte, []string, error) {
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	var ours []string
	inOurs, inServers, seenHeader := false, false, false
	for _, l := range lines {
		if tomlOurHeader.MatchString(l) {
			inOurs, inServers, seenHeader = true, false, true
			ours = append(ours, l)
			continue
		}
		if tomlAnyHeader.MatchString(l) {
			if inOurs {
				k := len(ours)
				for k > 1 {
					t := strings.TrimSpace(ours[k-1])
					if t != "" && !strings.HasPrefix(t, "#") {
						break
					}
					k--
				}
				out = append(out, ours[k:]...)
				ours = ours[:k]
			}
			inOurs = false
			inServers = tomlServersHeader.MatchString(l)
			seenHeader = true
			out = append(out, l)
			continue
		}
		if inOurs {
			ours = append(ours, l)
			continue
		}
		if (!seenHeader && tomlDottedAtRoot.MatchString(l)) || (inServers && tomlInlineInTable.MatchString(l)) {
			return nil, nil, errUnmanagedTOML
		}
		out = append(out, l)
	}
	return []byte(strings.Join(collapseBlanks(out), "\n")), ours, nil
}

func tomlUpsert(src []byte, block string) ([]byte, error) {
	stripped, _, err := tomlStrip(src)
	if err != nil {
		return nil, err
	}
	body := strings.TrimRight(string(stripped), "\n")
	if strings.TrimSpace(body) == "" {
		return []byte(block), nil
	}
	return []byte(body + "\n\n" + block), nil
}

func tomlRemove(src []byte) ([]byte, error) {
	stripped, ours, err := tomlStrip(src)
	if err != nil {
		return nil, err
	}
	if len(ours) == 0 {
		return src, nil
	}
	body := strings.TrimRight(string(stripped), "\n")
	if strings.TrimSpace(body) == "" {
		return []byte{}, nil
	}
	return []byte(body + "\n"), nil
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlKey(k string) string {
	if tomlBareKey.MatchString(k) {
		return k
	}
	return tomlString(k)
}

func codexBlock(s ServerSpec) string {
	var b strings.Builder
	b.WriteString("[mcp_servers.jack-ketch]\n")
	b.WriteString("command = " + tomlString(s.Command) + "\n")
	args := make([]string, len(s.Args))
	for i, a := range s.Args {
		args[i] = tomlString(a)
	}
	b.WriteString("args = [" + strings.Join(args, ", ") + "]\n")
	b.WriteString("startup_timeout_sec = 30\n")
	b.WriteString("tool_timeout_sec = 660\n")
	if len(s.Env) > 0 {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("\n[mcp_servers.jack-ketch.env]\n")
		for _, k := range keys {
			b.WriteString(tomlKey(k) + " = " + tomlString(s.Env[k]) + "\n")
		}
	}
	return b.String()
}

func tomlEntry(ours []string) entryInfo {
	var e entryInfo
	for _, l := range ours {
		if m := tomlCommandLine.FindStringSubmatch(l); m != nil && e.Command == "" {
			if v, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
				e.Command = v
			}
		}
		if m := tomlArgsLine.FindStringSubmatch(l); m != nil && e.Args == nil {
			e.Args = []string{}
			for _, q := range tomlQuoted.FindAllStringSubmatch(m[1], -1) {
				if v, err := strconv.Unquote(`"` + q[1] + `"`); err == nil {
					e.Args = append(e.Args, v)
				}
			}
		}
	}
	return e
}
