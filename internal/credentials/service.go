package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

const (
	KindBearer   = "bearer"
	KindBasic    = "basic"
	KindHeader   = "header"
	KindCookie   = "cookie"
	KindQuery    = "query"
	KindVariable = "variable"

	StatusOK       = "ok"
	StatusExpiring = "expiring"
	StatusExpired  = "expired"
	StatusEmpty    = "empty"

	expirySkew = 30 * time.Second
)

var (
	nameRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]{0,63}$`)
	secretishRe  = regexp.MustCompile(`(?i)(key|token|secret|passw|pwd|auth|session|credential|signature|jwt|bearer|cookie)`)
	headerNameRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)
	ErrNoProfile = errors.New("credential profile not found")
)

type Service struct {
	db    *store.DB
	keys  *KeyManager
	now   func() time.Time
	mu    sync.Mutex
	locks map[string]*sync.Mutex
	cache map[string]secretCache
	gen   uint64
}

type secretCache struct {
	gen    uint64
	values []string
	labels []Secret
}

func NewService(db *store.DB, keys *KeyManager) *Service {
	return &Service{db: db, keys: keys, now: time.Now, locks: map[string]*sync.Mutex{}, cache: map[string]secretCache{}}
}

func (s *Service) Keys() *KeyManager { return s.keys }

func (s *Service) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

func (s *Service) bump() {
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
}

type Loaded struct {
	Row    *store.CredentialRow
	Fields []api.CredentialField
	Jar    []api.CredentialCookie
	Recipe *api.CredentialRecipe
	Apps   []string
}

func fieldAAD(id, name string) string  { return id + "/field/" + name }
func cookieAAD(id, name string) string { return id + "/cookie/" + name }

func (s *Service) decode(row *store.CredentialRow) (*Loaded, error) {
	key, err := s.keys.Key()
	if err != nil {
		return nil, err
	}
	l := &Loaded{Row: row}
	if err := json.Unmarshal([]byte(orDefault(row.FieldsJSON, "[]")), &l.Fields); err != nil {
		return nil, fmt.Errorf("profile %q is corrupt: %w", row.Name, err)
	}
	for i := range l.Fields {
		f := &l.Fields[i]
		if isSealed(f.Value) {
			v, err := open(key, fieldAAD(row.ID, f.Name), f.Value)
			if err != nil {
				return nil, fmt.Errorf("profile %q field %q: %w", row.Name, f.Name, err)
			}
			f.Value = v
		}
		f.HasValue = f.Value != ""
	}
	if strings.TrimSpace(row.JarJSON) != "" {
		if err := json.Unmarshal([]byte(row.JarJSON), &l.Jar); err != nil {
			return nil, fmt.Errorf("profile %q cookie jar is corrupt: %w", row.Name, err)
		}
		for i := range l.Jar {
			c := &l.Jar[i]
			if isSealed(c.Value) {
				v, err := open(key, cookieAAD(row.ID, c.Name), c.Value)
				if err != nil {
					return nil, fmt.Errorf("profile %q cookie %q: %w", row.Name, c.Name, err)
				}
				c.Value = v
			}
		}
	}
	if strings.TrimSpace(row.RecipeJSON) != "" {
		var rc api.CredentialRecipe
		if err := json.Unmarshal([]byte(row.RecipeJSON), &rc); err == nil && rc.URL != "" {
			l.Recipe = &rc
		}
	}
	_ = json.Unmarshal([]byte(orDefault(row.AppsJSON, "[]")), &l.Apps)
	return l, nil
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func (s *Service) encode(l *Loaded) error {
	key, err := s.keys.Key()
	if err != nil {
		return err
	}
	fields := make([]api.CredentialField, len(l.Fields))
	for i, f := range l.Fields {
		f.Keep = false
		f.HasValue = false
		if f.Secret {
			sealed, err := seal(key, fieldAAD(l.Row.ID, f.Name), f.Value)
			if err != nil {
				return err
			}
			f.Value = sealed
		}
		fields[i] = f
	}
	fb, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	l.Row.FieldsJSON = string(fb)
	jar := make([]api.CredentialCookie, len(l.Jar))
	for i, c := range l.Jar {
		sealed, err := seal(key, cookieAAD(l.Row.ID, c.Name), c.Value)
		if err != nil {
			return err
		}
		c.Value = sealed
		jar[i] = c
	}
	l.Row.JarJSON = ""
	if len(jar) > 0 {
		jb, err := json.Marshal(jar)
		if err != nil {
			return err
		}
		l.Row.JarJSON = string(jb)
	}
	l.Row.RecipeJSON = ""
	if l.Recipe != nil {
		rb, err := json.Marshal(l.Recipe)
		if err != nil {
			return err
		}
		l.Row.RecipeJSON = string(rb)
	}
	ab, err := json.Marshal(l.Apps)
	if err != nil {
		return err
	}
	l.Row.AppsJSON = string(ab)
	return nil
}

func (s *Service) Get(projectID, name string) (*Loaded, error) {
	row, err := s.db.GetCredential(projectID, name)
	if errors.Is(err, store.ErrCredentialNotFound) {
		return nil, fmt.Errorf("%w %q", ErrNoProfile, name)
	}
	if err != nil {
		return nil, err
	}
	return s.decode(row)
}

func (s *Service) GetByID(projectID, id string) (*Loaded, error) {
	row, err := s.db.GetCredentialByID(id)
	if errors.Is(err, store.ErrCredentialNotFound) {
		return nil, fmt.Errorf("%w %q", ErrNoProfile, id)
	}
	if err != nil {
		return nil, err
	}
	if row.ProjectID != projectID {
		return nil, fmt.Errorf("%w %q", ErrNoProfile, id)
	}
	return s.decode(row)
}

func fieldExpiry(f api.CredentialField) int64 {
	if f.ExpiresAt > 0 {
		return f.ExpiresAt
	}
	if f.Secret && f.Value != "" && (f.Kind == KindBearer || f.Kind == KindHeader || f.Kind == KindVariable) {
		if exp, ok := JWTExpiry(f.Value); ok {
			return exp
		}
	}
	return 0
}

func statusOf(l *Loaded, now time.Time) (string, int64) {
	status := StatusOK
	var soonest int64
	secrets, filled := 0, 0
	for _, f := range l.Fields {
		if f.Secret {
			secrets++
			if f.Value != "" {
				filled++
			}
		}
		exp := fieldExpiry(f)
		if exp == 0 || f.Value == "" {
			continue
		}
		if soonest == 0 || exp < soonest {
			soonest = exp
		}
		t := time.Unix(exp, 0)
		switch {
		case !t.After(now):
			status = StatusExpired
		case t.Sub(now) <= expirySkew && status != StatusExpired:
			status = StatusExpiring
		}
	}
	if secrets > 0 && filled == 0 {
		status = StatusEmpty
	}
	if l.Recipe != nil {
		for _, ex := range l.Recipe.Extract {
			found := false
			for _, f := range l.Fields {
				if strings.EqualFold(f.Name, ex.Field) && f.Value != "" {
					found = true
				}
			}
			if !found {
				status = StatusEmpty
			}
		}
	}
	return status, soonest
}

func (s *Service) view(l *Loaded, reveal bool) api.CredentialProfile {
	status, exp := statusOf(l, s.now())
	p := api.CredentialProfile{
		ID: l.Row.ID, Name: l.Row.Name, Description: l.Row.Description, Environment: l.Row.Environment,
		Default: l.Row.IsDefault, Apps: append([]string{}, l.Apps...), Recipe: l.Recipe,
		CaptureCookies: l.Row.CaptureJar, Status: status, ExpiresAt: exp,
		CreatedAt: l.Row.CreatedAt, UpdatedAt: l.Row.UpdatedAt, LastUsedAt: l.Row.LastUsedAt,
		LastRefreshAt: l.Row.LastRefreshAt, LastError: l.Row.LastError,
	}
	p.Fields = make([]api.CredentialField, 0, len(l.Fields))
	for _, f := range l.Fields {
		exp := fieldExpiry(f)
		f.HasValue = f.Value != ""
		if f.Secret && !reveal {
			f.Value = ""
		}
		f.ExpiresAt = exp
		p.Fields = append(p.Fields, f)
	}
	for _, c := range l.Jar {
		c.Value = ""
		p.Cookies = append(p.Cookies, c)
	}
	return p
}

func (s *Service) List(projectID string) ([]api.CredentialProfile, error) {
	rows, err := s.db.ListCredentials(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]api.CredentialProfile, 0, len(rows))
	for _, row := range rows {
		l, err := s.decode(row)
		if err != nil {
			out = append(out, api.CredentialProfile{
				ID: row.ID, Name: row.Name, Description: row.Description, Environment: row.Environment, Default: row.IsDefault,
				Fields: []api.CredentialField{}, Status: "unreadable", LastError: err.Error(), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
			continue
		}
		out = append(out, s.view(l, false))
	}
	return out, nil
}

func newID() string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return "cred_" + hex.EncodeToString(b[:])
}

func validateRecipe(r *api.CredentialRecipe) error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(r.URL) == "" {
		return errors.New("login recipe needs a url")
	}
	if !strings.HasPrefix(r.URL, "/") && !strings.HasPrefix(r.URL, "http://") && !strings.HasPrefix(r.URL, "https://") {
		return errors.New("login recipe url must be a path (/auth/login) or an absolute http(s) URL")
	}
	m := strings.ToUpper(strings.TrimSpace(r.Method))
	if m != "" && m != "POST" && m != "GET" && m != "PUT" && m != "PATCH" {
		return fmt.Errorf("login recipe method %q is not supported", r.Method)
	}
	for _, e := range r.Extract {
		switch e.Source {
		case "json", "header", "cookie", "regex":
		default:
			return fmt.Errorf("extract source %q must be json, header, cookie or regex", e.Source)
		}
		if strings.TrimSpace(e.Path) == "" || strings.TrimSpace(e.Field) == "" {
			return errors.New("every extract rule needs a path and a field")
		}
		if e.Source == "regex" {
			if _, err := regexp.Compile(e.Path); err != nil {
				return fmt.Errorf("extract regex: %w", err)
			}
		}
		if !nameRe.MatchString(e.Field) {
			return fmt.Errorf("extract field %q is not a valid name", e.Field)
		}
	}
	return nil
}

func validateFields(fields []api.CredentialField) error {
	seen := map[string]bool{}
	for _, f := range fields {
		if !nameRe.MatchString(f.Name) {
			return fmt.Errorf("field name %q must be 1-64 letters, digits, dot, dash or underscore", f.Name)
		}
		if seen[strings.ToLower(f.Name)] {
			return fmt.Errorf("duplicate field %q", f.Name)
		}
		seen[strings.ToLower(f.Name)] = true
		switch f.Kind {
		case KindBearer, KindBasic, KindVariable:
		case KindHeader:
			if !headerNameRe.MatchString(f.Target) {
				return fmt.Errorf("field %q needs a valid header name as its target", f.Name)
			}
		case KindCookie, KindQuery:
			if strings.TrimSpace(f.Target) == "" {
				return fmt.Errorf("field %q needs a %s name as its target", f.Name, f.Kind)
			}
		default:
			return fmt.Errorf("field %q has unknown kind %q", f.Name, f.Kind)
		}
		if (f.Kind == KindBearer || f.Kind == KindBasic) && f.Target != "" && !headerNameRe.MatchString(f.Target) {
			return fmt.Errorf("field %q target must be a header name", f.Name)
		}
	}
	return nil
}

func (s *Service) Upsert(projectID string, req api.UpsertCredentialRequest) (*api.CredentialProfile, error) {
	name := strings.TrimSpace(req.Name)
	if !nameRe.MatchString(name) {
		return nil, errors.New("profile name must be 1-64 letters, digits, dot, dash or underscore")
	}
	if err := validateFields(req.Fields); err != nil {
		return nil, err
	}
	if err := validateRecipe(req.Recipe); err != nil {
		return nil, err
	}
	var existing *Loaded
	if req.ID != "" {
		l, err := s.GetByID(projectID, req.ID)
		if err != nil {
			return nil, err
		}
		existing = l
	}
	if existing != nil {
		s.lockFor(existing.Row.ID).Lock()
		defer s.lockFor(existing.Row.ID).Unlock()
	}
	var l *Loaded
	if existing != nil {
		l = existing
	} else {
		l = &Loaded{Row: &store.CredentialRow{ID: newID(), ProjectID: projectID, CaptureJar: true}}
	}
	old := map[string]api.CredentialField{}
	for _, f := range l.Fields {
		old[f.Name] = f
	}
	now := s.now().Unix()
	fields := make([]api.CredentialField, 0, len(req.Fields))
	changedCookie := map[string]bool{}
	for _, f := range req.Fields {
		f.Name = strings.TrimSpace(f.Name)
		f.Target = strings.TrimSpace(f.Target)
		prev, had := old[f.Name]
		if f.Value == "" && f.Keep && had {
			f.Value = prev.Value
			f.ExpiresAt = prev.ExpiresAt
			f.UpdatedAt = prev.UpdatedAt
		} else {
			f.ExpiresAt = 0
			f.UpdatedAt = now
			if had && prev.Value != f.Value && prev.Kind == KindCookie {
				changedCookie[prev.Target] = true
			}
		}
		f.Keep = false
		f.HasValue = false
		if f.Kind == KindBearer || f.Kind == KindBasic || f.Kind == KindCookie || secretishRe.MatchString(f.Name) || secretishRe.MatchString(f.Target) {
			f.Secret = true
		}
		fields = append(fields, f)
	}
	for name, prev := range old {
		found := false
		for _, f := range fields {
			if f.Name == name {
				found = true
			}
		}
		if !found && prev.Kind == KindCookie {
			changedCookie[prev.Target] = true
		}
	}
	l.Fields = fields
	if len(changedCookie) > 0 {
		kept := l.Jar[:0]
		for _, c := range l.Jar {
			if !changedCookie[c.Name] {
				kept = append(kept, c)
			}
		}
		l.Jar = kept
	}
	l.Row.Name = name
	l.Row.Description = strings.TrimSpace(req.Description)
	l.Row.Environment = strings.TrimSpace(req.Environment)
	l.Row.IsDefault = req.Default
	if req.CaptureCookies != nil {
		l.Row.CaptureJar = *req.CaptureCookies
	}
	apps := make([]string, 0, len(req.Apps))
	for _, a := range req.Apps {
		if a = strings.TrimSpace(a); a != "" {
			apps = append(apps, a)
		}
	}
	l.Apps = apps
	if req.Recipe != nil {
		rc := *req.Recipe
		rc.Method = strings.ToUpper(strings.TrimSpace(rc.Method))
		l.Recipe = &rc
	} else {
		l.Recipe = nil
	}
	l.Row.LastError = ""
	if err := s.encode(l); err != nil {
		return nil, err
	}
	if err := s.db.PutCredential(l.Row); err != nil {
		return nil, err
	}
	s.bump()
	v := s.view(l, false)
	return &v, nil
}

func (s *Service) Delete(projectID, id string) error {
	l, err := s.GetByID(projectID, id)
	if err != nil {
		return err
	}
	if err := s.db.DeleteCredential(l.Row.ID); err != nil {
		return err
	}
	s.bump()
	return nil
}

func (s *Service) Reveal(projectID, id, field string) (string, error) {
	l, err := s.GetByID(projectID, id)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(field, "cookie:") {
		name := strings.TrimPrefix(field, "cookie:")
		for _, c := range l.Jar {
			if c.Name == name {
				return c.Value, nil
			}
		}
		return "", fmt.Errorf("no cookie %q in the jar", name)
	}
	for _, f := range l.Fields {
		if f.Name == field {
			return f.Value, nil
		}
	}
	return "", fmt.Errorf("profile %q has no field %q", l.Row.Name, field)
}

func (s *Service) ClearJar(projectID, id string) error {
	l, err := s.GetByID(projectID, id)
	if err != nil {
		return err
	}
	s.lockFor(l.Row.ID).Lock()
	defer s.lockFor(l.Row.ID).Unlock()
	l.Jar = nil
	if err := s.encode(l); err != nil {
		return err
	}
	if err := s.db.UpdateCredentialState(l.Row.ID, l.Row.FieldsJSON, "", l.Row.LastError, false); err != nil {
		return err
	}
	s.bump()
	return nil
}

func (s *Service) ResetSecrets() (int, error) {
	rows, err := s.listAllRows()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		var fields []api.CredentialField
		if json.Unmarshal([]byte(orDefault(row.FieldsJSON, "[]")), &fields) != nil {
			fields = nil
		}
		for i := range fields {
			if fields[i].Secret {
				fields[i].Value = ""
				fields[i].ExpiresAt = 0
			}
		}
		fb, _ := json.Marshal(fields)
		if err := s.db.UpdateCredentialState(row.ID, string(fb), "", "secrets were cleared because the credentials key was reset", false); err != nil {
			return n, err
		}
		n++
	}
	s.bump()
	return n, nil
}

func (s *Service) listAllRows() ([]*store.CredentialRow, error) {
	projects, err := s.db.ListProjects()
	if err != nil {
		return nil, err
	}
	var out []*store.CredentialRow
	for _, p := range projects {
		rows, err := s.db.ListCredentials(p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (s *Service) secrets(projectID string) secretCache {
	s.mu.Lock()
	c, ok := s.cache[projectID]
	gen := s.gen
	s.mu.Unlock()
	if ok && c.gen == gen {
		return c
	}
	rows, err := s.db.ListCredentials(projectID)
	if err != nil {
		return secretCache{}
	}
	var labels []Secret
	for _, row := range rows {
		l, err := s.decode(row)
		if err != nil {
			continue
		}
		labels = append(labels, loadedSecrets(l)...)
	}
	sc := NewScrubber(labels)
	c = secretCache{gen: gen, values: sc.Literals(), labels: labels}
	s.mu.Lock()
	s.cache[projectID] = c
	s.mu.Unlock()
	return c
}

func (s *Service) Secrets(projectID string) []string {
	if projectID == "" {
		return nil
	}
	return s.secrets(projectID).values
}

func (s *Service) ProjectScrubber(projectID string) *Scrubber {
	if projectID == "" {
		return &Scrubber{}
	}
	return NewScrubber(s.secrets(projectID).labels)
}

func loadedSecrets(l *Loaded) []Secret {
	var out []Secret
	for _, f := range l.Fields {
		if !f.Secret || f.Value == "" {
			continue
		}
		label := l.Row.Name + "." + f.Name
		out = append(out, Secret{Label: label, Value: f.Value})
		if f.Kind == KindBasic {
			out = append(out, Secret{Label: label, Value: base64Std(f.Value)})
			if _, pass, ok := strings.Cut(f.Value, ":"); ok {
				out = append(out, Secret{Label: label, Value: pass})
			}
		}
	}
	for _, c := range l.Jar {
		if c.Value != "" {
			out = append(out, Secret{Label: l.Row.Name + ".cookie." + c.Name, Value: c.Value})
		}
	}
	return out
}

func sortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
