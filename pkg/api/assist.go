package api

type EnvSourceSpec struct {
	File     string   `json:"file,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	Vars     []string `json:"vars,omitempty"`
}

type CheckSpec struct {
	Name           string            `json:"name"`
	Method         string            `json:"method,omitempty"`
	URL            string            `json:"url,omitempty"`
	TCP            string            `json:"tcp,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	ExpectStatus   int               `json:"expect_status,omitempty"`
	ExpectContains string            `json:"expect_contains,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
}

type ModeInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Command     []string `json:"command,omitempty"`
	WorkDir     string   `json:"workdir,omitempty"`
	EnvSources  []string `json:"env_sources,omitempty"`
	Checks      []string `json:"checks,omitempty"`
	Default     bool     `json:"default,omitempty"`
}

type RunOutcome struct {
	ProcessID    string `json:"process_id"`
	Status       string `json:"status"`
	Ready        bool   `json:"ready"`
	ReadyLine    string `json:"ready_line,omitempty"`
	HasReadiness bool   `json:"has_readiness"`
	UptimeMS     int64  `json:"uptime_ms"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	Health       string `json:"health,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Success      bool   `json:"success"`
	Kind         string `json:"kind,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type SaveSuggestion struct {
	ProcessID  string          `json:"process_id"`
	Kind       string          `json:"kind"`
	Reason     string          `json:"reason"`
	App        string          `json:"app"`
	Mode       string          `json:"mode"`
	NewApp     bool            `json:"new_app"`
	ModeExists bool            `json:"mode_exists,omitempty"`
	Type       string          `json:"type,omitempty"`
	Command    []string        `json:"command,omitempty"`
	WorkDir    string          `json:"workdir,omitempty"`
	EnvSources []EnvSourceSpec `json:"env_sources,omitempty"`
	YAML       string          `json:"yaml"`
	Ask        string          `json:"ask"`
	Next       string          `json:"next"`
}

type SaveRunRequest struct {
	ProcessID   string          `json:"process_id"`
	Action      string          `json:"action,omitempty"`
	App         string          `json:"app,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	Scope       string          `json:"scope,omitempty"`
	Description string          `json:"description,omitempty"`
	SetDefault  bool            `json:"set_default,omitempty"`
	Overwrite   bool            `json:"overwrite,omitempty"`
	EnvSources  []EnvSourceSpec `json:"env_sources,omitempty"`
	Checks      []CheckSpec     `json:"checks,omitempty"`
}

type SaveRunResult struct {
	Saved     bool   `json:"saved"`
	Dismissed bool   `json:"dismissed,omitempty"`
	App       string `json:"app,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Scope     string `json:"scope,omitempty"`
	NewApp    bool   `json:"new_app,omitempty"`
	Replaced  bool   `json:"replaced,omitempty"`
	Unchanged bool   `json:"unchanged,omitempty"`
	Revision  int64  `json:"revision,omitempty"`
	YAML      string `json:"yaml,omitempty"`
	StartWith string `json:"start_with,omitempty"`
	Message   string `json:"message,omitempty"`
}

type SuggestedFix struct {
	Kind          string `json:"kind"`
	Description   string `json:"description"`
	Command       string `json:"command,omitempty"`
	WorkDir       string `json:"workdir,omitempty"`
	NeedsApproval bool   `json:"needs_approval"`
}

type MissingEnvVar struct {
	Key     string `json:"key"`
	Hint    string `json:"hint,omitempty"`
	Example string `json:"example,omitempty"`
	Source  string `json:"source,omitempty"`
	AddTo   string `json:"add_to,omitempty"`
}

type ServiceHint struct {
	Name       string `json:"name"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Image      string `json:"image,omitempty"`
	RunHint    string `json:"run_hint,omitempty"`
	ProjectApp string `json:"project_app,omitempty"`
}

type Issue struct {
	Category string         `json:"category"`
	Summary  string         `json:"summary"`
	Actor    string         `json:"actor"`
	Evidence []string       `json:"evidence,omitempty"`
	Steps    []string       `json:"steps,omitempty"`
	Fixes    []SuggestedFix `json:"fixes,omitempty"`
}

type Diagnosis struct {
	ProcessID  string          `json:"process_id"`
	Status     string          `json:"status"`
	ExitCode   *int            `json:"exit_code,omitempty"`
	Failing    bool            `json:"failing"`
	Category   string          `json:"category"`
	Actor      string          `json:"actor"`
	Summary    string          `json:"summary"`
	Issues     []Issue         `json:"issues,omitempty"`
	MissingEnv []MissingEnvVar `json:"missing_env,omitempty"`
	EnvFiles   []string        `json:"env_files,omitempty"`
	Service    *ServiceHint    `json:"service,omitempty"`
	Tail       []string        `json:"tail,omitempty"`
	Guidance   string          `json:"guidance"`
}

type ProbeRequest struct {
	ProcessID      string            `json:"process_id"`
	Method         string            `json:"method,omitempty"`
	URL            string            `json:"url,omitempty"`
	TCP            string            `json:"tcp,omitempty"`
	Port           int               `json:"port,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	ExpectStatus   int               `json:"expect_status,omitempty"`
	ExpectContains string            `json:"expect_contains,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
	SettleMS       int               `json:"settle_ms,omitempty"`
	MaxBodyBytes   int               `json:"max_body_bytes,omitempty"`
	Confirmed      bool              `json:"confirmed,omitempty"`
}

type ProbeResult struct {
	Name           string            `json:"name,omitempty"`
	ProcessID      string            `json:"process_id"`
	Method         string            `json:"method,omitempty"`
	URL            string            `json:"url,omitempty"`
	TCP            string            `json:"tcp,omitempty"`
	OK             bool              `json:"ok"`
	Status         int               `json:"status,omitempty"`
	StatusText     string            `json:"status_text,omitempty"`
	DurationMS     int64             `json:"duration_ms"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	BodyBytes      int               `json:"body_bytes,omitempty"`
	BodyTruncated  bool              `json:"body_truncated,omitempty"`
	Error          string            `json:"error,omitempty"`
	Failures       []string          `json:"failures,omitempty"`
	Logs           []LogEntry        `json:"logs"`
	LogsTruncated  bool              `json:"logs_truncated,omitempty"`
	ErrorLogs      int               `json:"error_logs,omitempty"`
	ListeningPorts []int             `json:"listening_ports,omitempty"`
	ProcessStatus  string            `json:"process_status,omitempty"`
	Check          *CheckSpec        `json:"check,omitempty"`
}

type RunChecksRequest struct {
	ProcessID string   `json:"process_id,omitempty"`
	App       string   `json:"app,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Names     []string `json:"names,omitempty"`
}

type RunChecksResult struct {
	ProcessID string        `json:"process_id"`
	App       string        `json:"app,omitempty"`
	Mode      string        `json:"mode,omitempty"`
	Passed    int           `json:"passed"`
	Failed    int           `json:"failed"`
	Results   []ProbeResult `json:"results"`
}
