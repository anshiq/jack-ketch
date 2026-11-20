package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type WatchConfig struct {
	Paths    []string `yaml:"paths"`
	Ignore   []string `yaml:"ignore,omitempty"`
	Debounce Duration `yaml:"debounce,omitempty"`
}

const (
	DefaultWatchDebounce = 300 * time.Millisecond
	minWatchDebounce     = 50 * time.Millisecond
	maxWatchDebounce     = time.Minute
)

func (w *WatchConfig) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return nil
		}
		w.Paths = []string{node.Value}
		return nil
	case yaml.SequenceNode:
		return node.Decode(&w.Paths)
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			switch k.Value {
			case "paths":
				if v.Kind == yaml.ScalarNode {
					w.Paths = []string{v.Value}
				} else if err := v.Decode(&w.Paths); err != nil {
					return err
				}
			case "ignore":
				if v.Kind == yaml.ScalarNode {
					w.Ignore = []string{v.Value}
				} else if err := v.Decode(&w.Ignore); err != nil {
					return err
				}
			case "debounce":
				if err := w.Debounce.UnmarshalYAML(v); err != nil {
					return err
				}
			default:
				return fmt.Errorf("line %d: field %s not found in type config.WatchConfig", k.Line, k.Value)
			}
		}
		return nil
	}
	return fmt.Errorf("line %d: watch must be a path, a list of paths or a mapping with paths, ignore and debounce", node.Line)
}

func (w *WatchConfig) Enabled() bool { return w != nil && len(w.Paths) > 0 }

func (w *WatchConfig) DebounceOrDefault() time.Duration {
	if w == nil || w.Debounce <= 0 {
		return DefaultWatchDebounce
	}
	return w.Debounce.Time()
}

func (w *WatchConfig) validate() []string {
	if w == nil {
		return nil
	}
	var out []string
	if len(w.Paths) == 0 && (len(w.Ignore) > 0 || w.Debounce != 0) {
		out = append(out, "watch needs at least one path")
	}
	for _, p := range w.Paths {
		if strings.TrimSpace(p) == "" {
			out = append(out, "watch paths must not be empty")
			continue
		}
		if filepath.IsAbs(p) {
			out = append(out, fmt.Sprintf("watch path %q must be relative to the app workdir", p))
		}
	}
	for _, g := range w.Ignore {
		if _, err := filepath.Match(g, "x"); err != nil {
			out = append(out, fmt.Sprintf("invalid watch ignore pattern %q: %v", g, err))
		}
	}
	if d := w.Debounce.Time(); d != 0 && (d < minWatchDebounce || d > maxWatchDebounce) {
		out = append(out, fmt.Sprintf("watch debounce must be between %s and %s, got %s", minWatchDebounce, maxWatchDebounce, d))
	}
	return out
}
