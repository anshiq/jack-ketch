package config

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var modeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func ValidModeName(name string) bool { return modeNameRe.MatchString(name) }

func ValidAppName(name string) bool { return modeNameRe.MatchString(name) }

type EnvVars []string

func (v *EnvVars) UnmarshalYAML(node *yaml.Node) error {
	out := EnvVars{}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			k, val := node.Content[i], node.Content[i+1]
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: env var %q must be a scalar value", val.Line, k.Value)
			}
			value := val.Value
			if val.Tag == "!!null" {
				value = ""
			}
			out = append(out, k.Value+"="+value)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: env entries must be KEY=VALUE strings", item.Line)
			}
			out = append(out, item.Value)
		}
	case yaml.ScalarNode:
		if node.Tag != "!!null" {
			return fmt.Errorf("line %d: vars must be a mapping of KEY: value or a list of KEY=VALUE", node.Line)
		}
	default:
		return fmt.Errorf("line %d: vars must be a mapping of KEY: value or a list of KEY=VALUE", node.Line)
	}
	*v = out
	return nil
}

func (v EnvVars) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, kv := range v {
		k, val, _ := strings.Cut(kv, "=")
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			scalarNode(val))
	}
	return node, nil
}

func scalarNode(val string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
	var probe any
	if val == "" || yaml.Unmarshal([]byte(val), &probe) != nil {
		n.Style = yaml.DoubleQuotedStyle
		return n
	}
	if str, ok := probe.(string); !ok || str != val {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

type EnvSource struct {
	File     string  `yaml:"file,omitempty" json:"file,omitempty"`
	Optional bool    `yaml:"optional,omitempty" json:"optional,omitempty"`
	Vars     EnvVars `yaml:"vars,omitempty" json:"vars,omitempty"`
}

func (s EnvSource) validate() error {
	hasFile := strings.TrimSpace(s.File) != ""
	hasVars := s.Vars != nil
	switch {
	case hasFile && hasVars:
		return fmt.Errorf("set either file or vars, not both")
	case !hasFile && !hasVars:
		return fmt.Errorf("set file (path to a dotenv file) or vars (KEY: value)")
	case hasVars && s.Optional:
		return fmt.Errorf("optional only applies to file sources")
	}
	for _, kv := range s.Vars {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("env entry %q must be KEY=VALUE", kv)
		}
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("invalid env var name %q", k)
		}
	}
	return nil
}

type LabeledEnvSource struct {
	Label string
	EnvSource
}

type Check struct {
	Name           string            `yaml:"name" json:"name"`
	Method         string            `yaml:"method,omitempty" json:"method,omitempty"`
	URL            string            `yaml:"url,omitempty" json:"url,omitempty"`
	TCP            string            `yaml:"tcp,omitempty" json:"tcp,omitempty"`
	Headers        map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body           string            `yaml:"body,omitempty" json:"body,omitempty"`
	ExpectStatus   int               `yaml:"expect_status,omitempty" json:"expect_status,omitempty"`
	ExpectContains string            `yaml:"expect_contains,omitempty" json:"expect_contains,omitempty"`
	Timeout        Duration          `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

func (c Check) validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("check needs a name")
	}
	hasURL := strings.TrimSpace(c.URL) != ""
	hasTCP := strings.TrimSpace(c.TCP) != ""
	if hasURL == hasTCP {
		return fmt.Errorf("check %q: set exactly one of url or tcp", c.Name)
	}
	if hasTCP && (c.Method != "" || c.Body != "" || len(c.Headers) > 0 || c.ExpectStatus != 0 || c.ExpectContains != "") {
		return fmt.Errorf("check %q: tcp checks only take tcp and timeout", c.Name)
	}
	if c.Method != "" && !httpMethods[strings.ToUpper(c.Method)] {
		return fmt.Errorf("check %q: unsupported method %q", c.Name, c.Method)
	}
	if c.ExpectStatus != 0 && (c.ExpectStatus < 100 || c.ExpectStatus > 599) {
		return fmt.Errorf("check %q: expect_status must be 100-599", c.Name)
	}
	if hasURL && !strings.HasPrefix(c.URL, "/") && !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") && !strings.HasPrefix(c.URL, "${") {
		return fmt.Errorf("check %q: url must be a path (/health) or an absolute http(s) URL", c.Name)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("check %q: timeout must be positive", c.Name)
	}
	return nil
}

func (c Check) EffectiveMethod() string {
	if c.Method == "" {
		return http.MethodGet
	}
	return strings.ToUpper(c.Method)
}

func (c Check) EffectiveTimeout() time.Duration {
	if c.Timeout <= 0 {
		return 10 * time.Second
	}
	return c.Timeout.Time()
}

type Mode struct {
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Type        string       `yaml:"type,omitempty" json:"type,omitempty"`
	WorkDir     string       `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	Command     []string     `yaml:"command,omitempty,flow" json:"command,omitempty"`
	EnvFile     string       `yaml:"env_file,omitempty" json:"env_file,omitempty"`
	Env         []string     `yaml:"env,omitempty" json:"env,omitempty"`
	EnvSources  []EnvSource  `yaml:"env_sources,omitempty" json:"env_sources,omitempty"`
	Readiness   []string     `yaml:"readiness,omitempty" json:"readiness,omitempty"`
	HealthCheck *HealthCheck `yaml:"health_check,omitempty" json:"health_check,omitempty"`
	Restart     *Restart     `yaml:"restart,omitempty" json:"restart,omitempty"`
	Checks      []Check      `yaml:"checks,omitempty" json:"checks,omitempty"`
}

func baseChain(prefix, envFile string, env []string, sources []EnvSource) []LabeledEnvSource {
	var out []LabeledEnvSource
	if envFile != "" {
		label := "env_file"
		if prefix != "" {
			label = prefix + "env_file"
		}
		out = append(out, LabeledEnvSource{Label: label, EnvSource: EnvSource{File: envFile}})
	}
	if len(env) > 0 {
		label := "app.env"
		if prefix != "" {
			label = prefix + "env"
		}
		out = append(out, LabeledEnvSource{Label: label, EnvSource: EnvSource{Vars: EnvVars(env)}})
	}
	vars := 0
	for _, s := range sources {
		if s.Vars != nil {
			vars++
		}
	}
	n := 0
	for _, s := range sources {
		var label string
		if s.File != "" {
			label = "file:" + s.File
		} else {
			n++
			label = "vars"
			if vars > 1 {
				label = "vars#" + strconv.Itoa(n)
			}
		}
		out = append(out, LabeledEnvSource{Label: prefix + label, EnvSource: s})
	}
	return out
}

type ResolvedApp struct {
	AppConfig
	Mode     string
	EnvChain []LabeledEnvSource
}

func (a AppConfig) ModeNames() []string {
	names := make([]string, 0, len(a.Modes))
	for n := range a.Modes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (a AppConfig) PickMode(requested string) (string, error) {
	if requested != "" {
		if _, ok := a.Modes[requested]; !ok {
			if len(a.Modes) == 0 {
				return "", fmt.Errorf("has no modes; omit mode or add one under modes:")
			}
			return "", fmt.Errorf("has no mode %q (modes: %s)", requested, strings.Join(a.ModeNames(), ", "))
		}
		return requested, nil
	}
	if a.DefaultMode != "" {
		if _, ok := a.Modes[a.DefaultMode]; !ok {
			return "", fmt.Errorf("default_mode %q is not defined under modes", a.DefaultMode)
		}
		return a.DefaultMode, nil
	}
	if len(a.Command) == 0 && len(a.Modes) == 1 {
		for n := range a.Modes {
			return n, nil
		}
	}
	return "", nil
}

func (a AppConfig) ResolveMode(requested string) (ResolvedApp, error) {
	name, err := a.PickMode(requested)
	if err != nil {
		return ResolvedApp{}, err
	}
	out := a
	out.Modes = nil
	out.DefaultMode = ""
	out.Checks = append([]Check(nil), a.Checks...)
	chain := baseChain("", a.EnvFile, a.Env, a.EnvSources)
	if name != "" {
		m := a.Modes[name]
		if m.Description != "" {
			out.Description = m.Description
		}
		if m.Type != "" {
			out.Type = m.Type
		}
		if m.WorkDir != "" {
			out.WorkDir = m.WorkDir
		}
		if len(m.Command) > 0 {
			out.Command = append([]string(nil), m.Command...)
		}
		if len(m.Readiness) > 0 {
			out.Readiness = append([]string(nil), m.Readiness...)
		}
		if m.HealthCheck != nil {
			out.HealthCheck = *m.HealthCheck
		}
		if m.Restart != nil {
			out.Restart = *m.Restart
		}
		out.Checks = mergeChecks(out.Checks, m.Checks)
		chain = append(chain, baseChain("mode:"+name+"/", m.EnvFile, m.Env, m.EnvSources)...)
	}
	return ResolvedApp{AppConfig: out, Mode: name, EnvChain: chain}, nil
}

func mergeChecks(base, over []Check) []Check {
	if len(over) == 0 {
		return base
	}
	idx := map[string]int{}
	out := append([]Check(nil), base...)
	for i, c := range out {
		idx[c.Name] = i
	}
	for _, c := range over {
		if i, ok := idx[c.Name]; ok {
			out[i] = c
			continue
		}
		idx[c.Name] = len(out)
		out = append(out, c)
	}
	return out
}

func (a *AppConfig) ExpandEnv(fn func([]string) ([]string, error)) error {
	expand := func(list []string) ([]string, error) {
		if len(list) == 0 {
			return list, nil
		}
		return fn(list)
	}
	var err error
	if a.Env, err = expand(a.Env); err != nil {
		return err
	}
	for i := range a.EnvSources {
		v, err := expand(a.EnvSources[i].Vars)
		if err != nil {
			return err
		}
		a.EnvSources[i].Vars = v
	}
	if len(a.Modes) == 0 {
		return nil
	}
	modes := make(map[string]Mode, len(a.Modes))
	for name, m := range a.Modes {
		if m.Env, err = expand(m.Env); err != nil {
			return err
		}
		srcs := append([]EnvSource(nil), m.EnvSources...)
		for i := range srcs {
			v, err := expand(srcs[i].Vars)
			if err != nil {
				return err
			}
			srcs[i].Vars = v
		}
		m.EnvSources = srcs
		modes[name] = m
	}
	a.Modes = modes
	return nil
}

func (a AppConfig) AllEnvLists() [][]string {
	out := [][]string{a.Env}
	for _, s := range a.EnvSources {
		out = append(out, s.Vars)
	}
	for _, name := range a.ModeNames() {
		m := a.Modes[name]
		out = append(out, m.Env)
		for _, s := range m.EnvSources {
			out = append(out, s.Vars)
		}
	}
	return out
}

func (a AppConfig) validateExtensions() []string {
	var errs []string
	for i, s := range a.EnvSources {
		if err := s.validate(); err != nil {
			errs = append(errs, fmt.Sprintf("env_sources[%d]: %v", i, err))
		}
	}
	seen := map[string]bool{}
	for _, c := range a.Checks {
		if err := c.validate(); err != nil {
			errs = append(errs, "checks: "+err.Error())
		}
		if seen[c.Name] {
			errs = append(errs, fmt.Sprintf("checks: duplicate check name %q", c.Name))
		}
		seen[c.Name] = true
	}
	if a.DefaultMode != "" && len(a.Modes) > 0 {
		if _, ok := a.Modes[a.DefaultMode]; !ok {
			errs = append(errs, fmt.Sprintf("default_mode %q is not one of the modes (%s)", a.DefaultMode, strings.Join(a.ModeNames(), ", ")))
		}
	}
	for _, name := range a.ModeNames() {
		m := a.Modes[name]
		if !ValidModeName(name) {
			errs = append(errs, fmt.Sprintf("modes.%s: mode names use letters, digits, '.', '_' or '-'", name))
		}
		for i, s := range m.EnvSources {
			if err := s.validate(); err != nil {
				errs = append(errs, fmt.Sprintf("modes.%s.env_sources[%d]: %v", name, i, err))
			}
		}
		for _, pat := range m.Readiness {
			if err := checkRegexp(pat); err != nil {
				errs = append(errs, fmt.Sprintf("modes.%s.readiness: %v", name, err))
			}
		}
		if m.Restart != nil {
			switch m.Restart.Policy {
			case "", RestartNever, RestartOnFailure, RestartAlways:
			default:
				errs = append(errs, fmt.Sprintf("modes.%s.restart: invalid policy %q (want never|on-failure|always)", name, m.Restart.Policy))
			}
		}
		if m.HealthCheck != nil && m.HealthCheck.HTTP != "" && m.HealthCheck.TCP != "" {
			errs = append(errs, fmt.Sprintf("modes.%s.health_check: set only one of http or tcp", name))
		}
		mseen := map[string]bool{}
		for _, c := range m.Checks {
			if err := c.validate(); err != nil {
				errs = append(errs, fmt.Sprintf("modes.%s.checks: %v", name, err))
			}
			if mseen[c.Name] {
				errs = append(errs, fmt.Sprintf("modes.%s.checks: duplicate check name %q", name, c.Name))
			}
			mseen[c.Name] = true
		}
	}
	return errs
}

func (m *Mode) defaults() {
	if m.Restart != nil {
		r := *m.Restart
		if r.Policy == "" {
			r.Policy = RestartNever
		}
		if len(r.Backoff) == 0 {
			r.Backoff = []Duration{
				Duration(1 * time.Second), Duration(2 * time.Second),
				Duration(5 * time.Second), Duration(15 * time.Second),
				Duration(60 * time.Second),
			}
		}
		if r.MaxRestarts == 0 {
			r.MaxRestarts = 10
		}
		m.Restart = &r
	}
	if m.HealthCheck != nil {
		h := *m.HealthCheck
		if h.Interval == 0 {
			h.Interval = Duration(10 * time.Second)
		}
		if h.Timeout == 0 {
			h.Timeout = Duration(3 * time.Second)
		}
		if h.FailureThreshold == 0 {
			h.FailureThreshold = 3
		}
		m.HealthCheck = &h
	}
}
