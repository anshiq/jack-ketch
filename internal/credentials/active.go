package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

var placeholderRe = regexp.MustCompile(`\$\{cred:([A-Za-z0-9_.\-]+)\}`)

type Escape int

const (
	EscNone Escape = iota
	EscURL
	EscJSON
)

func base64Std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

type Scope struct {
	svc       *Service
	projectID string
}

func (s *Service) Scope(projectID string) *Scope { return &Scope{svc: s, projectID: projectID} }

func (sc *Scope) ProjectID() string { return sc.projectID }

func (sc *Scope) List() []api.CredentialProfile {
	out, _ := sc.svc.List(sc.projectID)
	return out
}

func (sc *Scope) Scrubber() *Scrubber { return sc.svc.ProjectScrubber(sc.projectID) }

func (sc *Scope) HasProfiles() bool {
	rows, err := sc.svc.db.ListCredentials(sc.projectID)
	return err == nil && len(rows) > 0
}

type Active struct {
	svc       *Service
	projectID string
	profile   string
	explicit  bool
	loaded    *Loaded
}

func appAllowed(apps []string, app string) bool {
	if len(apps) == 0 {
		return true
	}
	for _, a := range apps {
		if a == app {
			return true
		}
	}
	return false
}

func (sc *Scope) Resolve(name, app string) (*Active, error) {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "none") {
		return nil, nil
	}
	if name == "" {
		row, err := sc.svc.db.DefaultCredential(sc.projectID)
		if err != nil {
			if errors.Is(err, store.ErrCredentialNotFound) {
				return nil, nil
			}
			return nil, err
		}
		l, err := sc.svc.decode(row)
		if err != nil {
			return nil, err
		}
		if !appAllowed(l.Apps, app) {
			return nil, nil
		}
		return &Active{svc: sc.svc, projectID: sc.projectID, profile: l.Row.ID, loaded: l}, nil
	}
	l, err := sc.svc.Get(sc.projectID, name)
	if err != nil {
		if errors.Is(err, ErrNoProfile) {
			names := []string{}
			if all, lerr := sc.svc.db.ListCredentials(sc.projectID); lerr == nil {
				for _, r := range all {
					names = append(names, r.Name)
				}
			}
			if len(names) == 0 {
				return nil, fmt.Errorf("credential profile %q does not exist and this project has none; ask the user to add one in the web UI (Credentials page)", name)
			}
			return nil, fmt.Errorf("credential profile %q does not exist (profiles: %s)", name, strings.Join(names, ", "))
		}
		return nil, err
	}
	if !appAllowed(l.Apps, app) {
		return nil, fmt.Errorf("credential profile %q is restricted to apps %s", name, strings.Join(l.Apps, ", "))
	}
	return &Active{svc: sc.svc, projectID: sc.projectID, profile: l.Row.ID, explicit: true, loaded: l}, nil
}

func (a *Active) Name() string                  { return a.loaded.Row.Name }
func (a *Active) ID() string                    { return a.profile }
func (a *Active) Explicit() bool                { return a.explicit }
func (a *Active) HasRecipe() bool               { return a.loaded.Recipe != nil }
func (a *Active) CaptureJar() bool              { return a.loaded.Row.CaptureJar }
func (a *Active) Recipe() *api.CredentialRecipe { return a.loaded.Recipe }

func (a *Active) Status() (string, int64) { return statusOf(a.loaded, a.svc.now()) }

func (a *Active) Scrubber() *Scrubber {
	secs := loadedSecrets(a.loaded)
	secs = append(secs, a.svc.secrets(a.projectID).labels...)
	return NewScrubber(secs)
}

func (a *Active) field(name string) (api.CredentialField, bool) {
	for _, f := range a.loaded.Fields {
		if strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return api.CredentialField{}, false
}

func (a *Active) Expand(in string, esc Escape) (string, error) {
	if !strings.Contains(in, "${cred:") {
		return in, nil
	}
	var missing []string
	out := placeholderRe.ReplaceAllStringFunc(in, func(m string) string {
		name := placeholderRe.FindStringSubmatch(m)[1]
		f, ok := a.field(name)
		if !ok || f.Value == "" {
			missing = append(missing, name)
			return m
		}
		switch esc {
		case EscURL:
			return url.QueryEscape(f.Value)
		case EscJSON:
			b, _ := json.Marshal(f.Value)
			return string(b[1 : len(b)-1])
		}
		return f.Value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("credential profile %q has no value for %s", a.Name(), strings.Join(uniq(missing), ", "))
	}
	return out, nil
}

func ExpandRefs(in string) []string {
	var out []string
	for _, m := range placeholderRe.FindAllStringSubmatch(in, -1) {
		out = append(out, m[1])
	}
	return out
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func BodyEscape(body string) Escape {
	t := strings.TrimSpace(body)
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return EscJSON
	}
	return EscNone
}

func (a *Active) jarFor(u *url.URL, now time.Time) []api.CredentialCookie {
	var out []api.CredentialCookie
	for _, c := range a.loaded.Jar {
		if c.Expires > 0 && c.Expires <= now.Unix() {
			continue
		}
		if c.Host != "" && !strings.EqualFold(c.Host, u.Hostname()) {
			continue
		}
		if c.Path != "" && c.Path != "/" && !strings.HasPrefix(u.Path, strings.TrimSuffix(c.Path, "/")) {
			continue
		}
		if c.Secure && u.Scheme != "https" && !isLoopback(u.Hostname()) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

func (a *Active) Apply(req *http.Request) {
	now := a.svc.now()
	managed := map[string]string{}
	query := req.URL.Query()
	queryChanged := false
	for _, f := range a.loaded.Fields {
		if f.Value == "" {
			continue
		}
		switch f.Kind {
		case KindBearer:
			h := f.Target
			if h == "" {
				h = "Authorization"
			}
			req.Header.Set(h, "Bearer "+f.Value)
		case KindBasic:
			h := f.Target
			if h == "" {
				h = "Authorization"
			}
			req.Header.Set(h, "Basic "+base64Std(f.Value))
		case KindHeader:
			req.Header.Set(f.Target, f.Value)
		case KindCookie:
			managed[f.Target] = f.Value
		case KindQuery:
			query.Set(f.Target, f.Value)
			queryChanged = true
		}
	}
	for _, c := range a.jarFor(req.URL, now) {
		managed[c.Name] = c.Value
	}
	if queryChanged {
		req.URL.RawQuery = query.Encode()
	}
	if len(managed) == 0 {
		return
	}
	var kept []string
	for _, c := range req.Cookies() {
		if _, ours := managed[c.Name]; !ours {
			kept = append(kept, c.Name+"="+c.Value)
		}
	}
	for _, name := range sortedKeys(managed) {
		kept = append(kept, name+"="+managed[name])
	}
	req.Header.Set("Cookie", strings.Join(kept, "; "))
}

func sortedKeys(m map[string]string) []string {
	set := map[string]bool{}
	for k := range m {
		set[k] = true
	}
	return sortedNames(set)
}

func (a *Active) Capture(host string, cookies []*http.Cookie) {
	if !a.loaded.Row.CaptureJar || len(cookies) == 0 {
		return
	}
	a.svc.lockFor(a.profile).Lock()
	defer a.svc.lockFor(a.profile).Unlock()
	if !a.reload() {
		return
	}
	before := append([]api.CredentialCookie(nil), a.loaded.Jar...)
	a.mergeCookies(host, cookies)
	if len(before) == len(a.loaded.Jar) {
		same := true
		for i := range before {
			if before[i] != a.loaded.Jar[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	a.persistLocked(false, a.loaded.Row.LastError)
}

func (a *Active) reload() bool {
	row, err := a.svc.db.GetCredentialByID(a.profile)
	if err != nil {
		return false
	}
	l, err := a.svc.decode(row)
	if err != nil {
		return false
	}
	a.loaded = l
	return true
}

func (a *Active) persistLocked(refreshed bool, lastErr string) {
	if err := a.svc.encode(a.loaded); err != nil {
		return
	}
	if err := a.svc.db.UpdateCredentialState(a.profile, a.loaded.Row.FieldsJSON, a.loaded.Row.JarJSON, lastErr, refreshed); err == nil {
		a.svc.bump()
	}
}

func (a *Active) Touch() { _ = a.svc.db.TouchCredential(a.profile) }

func (a *Active) NoteError(msg string) {
	_ = a.svc.db.SetCredentialError(a.profile, msg)
}

func joinURL(base *url.URL, ref string) (*url.URL, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return url.Parse(ref)
	}
	rel, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	out := *base
	out.Path = path.Join("/", base.Path, rel.Path)
	if strings.HasSuffix(rel.Path, "/") && !strings.HasSuffix(out.Path, "/") {
		out.Path += "/"
	}
	out.RawQuery = rel.RawQuery
	return &out, nil
}

func (a *Active) Environment() string { return a.loaded.Row.Environment }

func (sc *Scope) Refresh(ctx context.Context, name string, base *url.URL) error {
	act, err := sc.ResolveFor(name)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return act.Refresh(ctx, base, func(req *http.Request) (*http.Response, error) { return client.Do(req) })
}
