package integrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type PathSpec struct {
	All     string
	Windows string
	Darwin  string
}

func (p PathSpec) template() string {
	switch runtime.GOOS {
	case "windows":
		if p.Windows != "" {
			return p.Windows
		}
	case "darwin":
		if p.Darwin != "" {
			return p.Darwin
		}
	}
	return p.All
}

type pathEnv struct {
	home          string
	xdgConfig     string
	appData       string
	codexHome     string
	copilotHome   string
	clineSettings string
	project       string
}

func currentPathEnv(project string) pathEnv {
	home, _ := os.UserHomeDir()
	e := pathEnv{home: home, project: project}
	e.xdgConfig = os.Getenv("XDG_CONFIG_HOME")
	if e.xdgConfig == "" {
		e.xdgConfig = filepath.Join(home, ".config")
	}
	switch runtime.GOOS {
	case "darwin":
		e.appData = filepath.Join(home, "Library", "Application Support")
	case "windows":
		e.appData = os.Getenv("APPDATA")
		if e.appData == "" {
			e.appData = filepath.Join(home, "AppData", "Roaming")
		}
	default:
		e.appData = e.xdgConfig
	}
	e.codexHome = os.Getenv("CODEX_HOME")
	if e.codexHome == "" {
		e.codexHome = filepath.Join(home, ".codex")
	}
	e.copilotHome = os.Getenv("COPILOT_HOME")
	if e.copilotHome == "" {
		e.copilotHome = filepath.Join(home, ".copilot")
	}
	e.clineSettings = os.Getenv("CLINE_MCP_SETTINGS_PATH")
	if e.clineSettings == "" {
		e.clineSettings = filepath.Join(home, ".cline", "data", "settings", "cline_mcp_settings.json")
	}
	return e
}

func (e pathEnv) expand(tmpl string) string {
	if tmpl == "" {
		return ""
	}
	r := strings.NewReplacer(
		"{home}", e.home,
		"{xdg_config}", e.xdgConfig,
		"{appdata}", e.appData,
		"{codex_home}", e.codexHome,
		"{copilot_home}", e.copilotHome,
		"{cline_settings}", e.clineSettings,
		"{project}", e.project,
	)
	return filepath.Clean(filepath.FromSlash(r.Replace(tmpl)))
}
