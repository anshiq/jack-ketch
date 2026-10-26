package integrate

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

type ServerSpec struct {
	Command string
	Args    []string
	Env     map[string]string
}

type kv struct {
	Key   string
	Value any
}

type orderedObject []kv

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(p.Key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(p.Value)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func envObject(env map[string]string) orderedObject {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := orderedObject{}
	for _, k := range keys {
		out = append(out, kv{k, env[k]})
	}
	return out
}

func jsonEntry(d Dialect, s ServerSpec) orderedObject {
	args := s.Args
	if args == nil {
		args = []string{}
	}
	full := append([]string{s.Command}, args...)
	env := envObject(s.Env)
	withEnv := func(o orderedObject) orderedObject {
		if len(s.Env) > 0 {
			o = append(o, kv{"env", env})
		}
		return o
	}
	switch d {
	case DialectClaude:
		return append(withEnv(orderedObject{{"type", "stdio"}, {"command", s.Command}, {"args", args}}), kv{"timeout", 660000})
	case DialectTypedStdio, DialectVSCode:
		return withEnv(orderedObject{{"type", "stdio"}, {"command", s.Command}, {"args", args}})
	case DialectCopilot:
		return orderedObject{{"type", "local"}, {"command", s.Command}, {"args", args}, {"env", env}, {"tools", []string{"*"}}}
	case DialectGemini:
		return append(withEnv(orderedObject{{"command", s.Command}, {"args", args}}), kv{"timeout", 660000})
	case DialectOpenCode:
		o := orderedObject{{"type", "local"}, {"command", full}, {"enabled", true}}
		if len(s.Env) > 0 {
			o = append(o, kv{"environment", env})
		}
		return append(o, kv{"timeout", 30000})
	case DialectKilo:
		return orderedObject{{"type", "local"}, {"command", full}, {"environment", env}, {"enabled", true}, {"timeout", 30000}}
	case DialectZed:
		return orderedObject{{"source", "custom"}, {"command", s.Command}, {"args", args}, {"env", env}}
	case DialectCline:
		return orderedObject{{"type", "stdio"}, {"command", s.Command}, {"args", args}, {"env", env}, {"disabled", false}, {"autoApprove", []string{}}, {"timeout", 660}}
	case DialectRoo:
		return orderedObject{{"command", s.Command}, {"args", args}, {"env", env}, {"disabled", false}, {"alwaysAllow", []string{}}, {"timeout", 300}}
	case DialectKiro:
		return orderedObject{{"command", s.Command}, {"args", args}, {"env", env}, {"disabled", false}, {"autoApprove", []string{}}}
	}
	return withEnv(orderedObject{{"command", s.Command}, {"args", args}})
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func shellQuote(s string) string {
	safe := s != "" && strings.IndexFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		}
		return !strings.ContainsRune("-_./:=@+,", r)
	}) < 0
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}
