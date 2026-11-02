package api

type CredentialField struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Target    string `json:"target,omitempty"`
	Secret    bool   `json:"secret"`
	Value     string `json:"value,omitempty"`
	HasValue  bool   `json:"has_value"`
	Keep      bool   `json:"keep,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

type CredentialExtract struct {
	Source        string `json:"source"`
	Path          string `json:"path"`
	Field         string `json:"field"`
	Kind          string `json:"kind,omitempty"`
	Target        string `json:"target,omitempty"`
	ExpiresInPath string `json:"expires_in_path,omitempty"`
}

type CredentialRecipe struct {
	Method     string              `json:"method,omitempty"`
	URL        string              `json:"url"`
	Headers    map[string]string   `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"`
	Extract    []CredentialExtract `json:"extract,omitempty"`
	TTLSeconds int                 `json:"ttl_seconds,omitempty"`
}

type CredentialCookie struct {
	Name    string `json:"name"`
	Value   string `json:"value,omitempty"`
	Host    string `json:"host,omitempty"`
	Path    string `json:"path,omitempty"`
	Expires int64  `json:"expires,omitempty"`
	Secure  bool   `json:"secure,omitempty"`
}

type CredentialProfile struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	Environment    string             `json:"environment,omitempty"`
	Default        bool               `json:"default"`
	Apps           []string           `json:"apps,omitempty"`
	Fields         []CredentialField  `json:"fields"`
	Recipe         *CredentialRecipe  `json:"recipe,omitempty"`
	Cookies        []CredentialCookie `json:"cookies,omitempty"`
	CaptureCookies bool               `json:"capture_cookies"`
	Status         string             `json:"status"`
	ExpiresAt      int64              `json:"expires_at,omitempty"`
	CreatedAt      int64              `json:"created_at"`
	UpdatedAt      int64              `json:"updated_at"`
	LastUsedAt     int64              `json:"last_used_at,omitempty"`
	LastRefreshAt  int64              `json:"last_refresh_at,omitempty"`
	LastError      string             `json:"last_error,omitempty"`
}

type ListCredentialsResult struct {
	Profiles []CredentialProfile `json:"profiles"`
	Hint     string              `json:"hint,omitempty"`
}

type UpsertCredentialRequest struct {
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	Environment    string            `json:"environment,omitempty"`
	Default        bool              `json:"default,omitempty"`
	Apps           []string          `json:"apps,omitempty"`
	Fields         []CredentialField `json:"fields"`
	Recipe         *CredentialRecipe `json:"recipe,omitempty"`
	CaptureCookies *bool             `json:"capture_cookies,omitempty"`
}

type KeystoreStatus struct {
	Backend           string `json:"backend"`
	Active            string `json:"active"`
	KeychainAvailable bool   `json:"keychain_available"`
	KeychainTool      string `json:"keychain_tool,omitempty"`
	KeychainError     string `json:"keychain_error,omitempty"`
	KeyPath           string `json:"key_path,omitempty"`
	KeyMissing        bool   `json:"key_missing,omitempty"`
}

type CredentialProxy struct {
	ID         string `json:"id"`
	Profile    string `json:"profile"`
	Upstream   string `json:"upstream"`
	URL        string `json:"url"`
	Label      string `json:"label,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	ExpiresAt  int64  `json:"expires_at"`
	Requests   int64  `json:"requests"`
	Refreshes  int64  `json:"refreshes"`
	LastStatus int    `json:"last_status,omitempty"`
}

type StartCredentialProxyRequest struct {
	Profile    string `json:"profile"`
	Upstream   string `json:"upstream,omitempty"`
	ProcessID  string `json:"process_id,omitempty"`
	Port       int    `json:"port,omitempty"`
	TTLMinutes int    `json:"ttl_minutes,omitempty"`
	Label      string `json:"label,omitempty"`
	Confirmed  bool   `json:"confirmed,omitempty"`
}
