package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

func ParseRetention(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

func (e *Engine) settingString(key, def string) string {
	raw, ok, err := e.store.GetSetting(key)
	if err != nil || !ok {
		return def
	}
	var s string
	if json.Unmarshal([]byte(raw), &s) == nil {
		return s
	}
	return raw
}

func (e *Engine) settingBool(key string, def bool) bool {
	raw, ok, err := e.store.GetSetting(key)
	if err != nil || !ok {
		return def
	}
	var b bool
	if json.Unmarshal([]byte(raw), &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal([]byte(raw), &s) == nil {
		return s == "true"
	}
	return def
}

func (e *Engine) LearnRawStarts() bool { return e.settingBool("learn_raw_starts", true) }

func (e *Engine) EventRetention() time.Duration {
	d, err := ParseRetention(e.settingString("retention", "30d"))
	if err != nil {
		return 30 * 24 * time.Hour
	}
	return d
}

func (e *Engine) IdleExit() time.Duration {
	s := e.settingString("idle_exit", "never")
	if s == "" || s == "never" {
		return 0
	}
	d, err := ParseRetention(s)
	if err != nil {
		return 0
	}
	return d
}

func (e *Engine) ApplySettings() {
	if e.levelVar == nil {
		return
	}
	switch e.settingString("log_level", "info") {
	case "debug":
		e.levelVar.Set(slog.LevelDebug)
	case "warn":
		e.levelVar.Set(slog.LevelWarn)
	case "error":
		e.levelVar.Set(slog.LevelError)
	default:
		e.levelVar.Set(slog.LevelInfo)
	}
}
