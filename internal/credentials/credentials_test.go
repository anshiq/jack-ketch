package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

func noKeychain() *keychainKeystore {
	return &keychainKeystore{
		run: func(context.Context, []byte, string, ...string) ([]byte, []byte, int, error) {
			return nil, nil, 1, errors.New("no keychain")
		},
		goos: "linux",
		look: func(string) (string, error) { return "", errors.New("not found") },
	}
}

func newSvc(t *testing.T) (*Service, *store.DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := db.CreateProject("proj")
	if err != nil {
		t.Fatal(err)
	}
	km := NewKeyManager(dir, db)
	km.chain = noKeychain()
	return NewService(db, km), db, p.ID, dir
}

func jwtWithExp(exp int64) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]any{"alg": "HS256"}) + "." + enc(map[string]any{"sub": "u1", "exp": exp}) + ".signature-part-1234"
}

func basicProfile(token string) api.UpsertCredentialRequest {
	return api.UpsertCredentialRequest{
		Name: "admin", Environment: "test", Default: true,
		Fields: []api.CredentialField{
			{Name: "token", Kind: KindBearer, Value: token, Secret: true},
			{Name: "tenant", Kind: KindHeader, Target: "X-Tenant-Id", Value: "tenant-a"},
			{Name: "session", Kind: KindCookie, Target: "sid", Value: "cookie-secret-9876", Secret: true},
			{Name: "api_key", Kind: KindQuery, Target: "key", Value: "query-secret-5555", Secret: true},
		},
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 7
	enc, err := seal(key, "aad", "hello")
	if err != nil || !isSealed(enc) || strings.Contains(enc, "hello") {
		t.Fatalf("seal: %q %v", enc, err)
	}
	got, err := open(key, "aad", enc)
	if err != nil || got != "hello" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := open(key, "other", enc); err == nil {
		t.Fatal("aad mismatch must fail")
	}
	key2 := make([]byte, 32)
	if _, err := open(key2, "aad", enc); err == nil {
		t.Fatal("wrong key must fail")
	}
	if e, _ := seal(key, "aad", ""); e != "" {
		t.Fatal("empty stays empty")
	}
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Now().Add(time.Hour).Unix()
	got, ok := JWTExpiry(jwtWithExp(exp))
	if !ok || got != exp {
		t.Fatalf("got %d %v", got, ok)
	}
	if _, ok := JWTExpiry("not-a-jwt"); ok {
		t.Fatal("not a jwt")
	}
	if _, ok := JWTExpiry("a.b.c"); ok {
		t.Fatal("garbage payload")
	}
}

func TestScrubberCoversEncodings(t *testing.T) {
	secret := "s3cr/et+value=="
	sc := NewScrubber([]Secret{{Label: "p.f", Value: secret}})
	cases := []string{
		secret,
		url.QueryEscape(secret),
		base64.StdEncoding.EncodeToString([]byte(secret)),
		base64.RawURLEncoding.EncodeToString([]byte(secret)),
	}
	for _, c := range cases {
		out := sc.Scrub("prefix " + c + " suffix")
		if strings.Contains(out, c) || !strings.Contains(out, "[credential:p.f]") {
			t.Fatalf("not scrubbed: %q -> %q", c, out)
		}
	}
	short := NewScrubber([]Secret{{Label: "x", Value: "abc"}})
	if !short.Empty() {
		t.Fatal("values below the minimum length are not scrubbed")
	}
	if NewScrubber(nil).Scrub("keep") != "keep" {
		t.Fatal("empty scrubber is a no-op")
	}
}

func TestUpsertMasksSecretsAndKeepsOnEmpty(t *testing.T) {
	svc, db, project, _ := newSvc(t)
	view, err := svc.Upsert(project, basicProfile("tok-abcdef-123456"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range view.Fields {
		if f.Secret && f.Value != "" {
			t.Fatalf("secret %s leaked in view", f.Name)
		}
		if f.Name == "tenant" && f.Value != "tenant-a" {
			t.Fatalf("non-secret value must be visible: %+v", f)
		}
		if !f.HasValue {
			t.Fatalf("has_value missing for %s", f.Name)
		}
	}
	row, _ := db.GetCredentialByID(view.ID)
	if strings.Contains(row.FieldsJSON, "tok-abcdef-123456") || strings.Contains(row.FieldsJSON, "cookie-secret-9876") {
		t.Fatalf("secret stored in plaintext: %s", row.FieldsJSON)
	}
	if !strings.Contains(row.FieldsJSON, "tenant-a") {
		t.Fatal("non-secret value should stay plain")
	}

	edit := basicProfile("")
	edit.ID = view.ID
	for i := range edit.Fields {
		edit.Fields[i].Keep = true
		if edit.Fields[i].Name == "tenant" {
			edit.Fields[i].Value = "tenant-b"
		} else {
			edit.Fields[i].Value = ""
		}
	}
	if _, err := svc.Upsert(project, edit); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Reveal(project, view.ID, "token")
	if err != nil || got != "tok-abcdef-123456" {
		t.Fatalf("kept value lost: %q %v", got, err)
	}
	if v, _ := svc.Reveal(project, view.ID, "tenant"); v != "tenant-b" {
		t.Fatalf("tenant not updated: %q", v)
	}
}

func TestUpsertValidationAndUniqueness(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	bad := []api.UpsertCredentialRequest{
		{Name: "bad name!", Fields: nil},
		{Name: "a", Fields: []api.CredentialField{{Name: "x", Kind: "weird"}}},
		{Name: "a", Fields: []api.CredentialField{{Name: "x", Kind: KindHeader}}},
		{Name: "a", Fields: []api.CredentialField{{Name: "x", Kind: KindBearer}, {Name: "X", Kind: KindBearer}}},
		{Name: "a", Recipe: &api.CredentialRecipe{URL: "login"}},
		{Name: "a", Recipe: &api.CredentialRecipe{URL: "/l", Extract: []api.CredentialExtract{{Source: "nope", Path: "a", Field: "f"}}}},
	}
	for i, req := range bad {
		if _, err := svc.Upsert(project, req); err == nil {
			t.Fatalf("case %d should fail", i)
		}
	}
	if _, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "one", Fields: []api.CredentialField{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "one", Fields: []api.CredentialField{}}); err == nil {
		t.Fatal("duplicate name must fail")
	}
}

func TestSingleDefaultPerProject(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	a, _ := svc.Upsert(project, api.UpsertCredentialRequest{Name: "a", Default: true, Fields: []api.CredentialField{}})
	b, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "b", Default: true, Fields: []api.CredentialField{}})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := svc.List(project)
	defaults := 0
	for _, p := range list {
		if p.Default {
			defaults++
			if p.ID != b.ID {
				t.Fatalf("default should be b, got %s (a=%s)", p.Name, a.ID)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("want one default, got %d", defaults)
	}
}

func TestStatusFromJWTExpiry(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	cases := map[string]string{
		jwtWithExp(time.Now().Add(time.Hour).Unix()):        StatusOK,
		jwtWithExp(time.Now().Add(10 * time.Second).Unix()): StatusExpiring,
		jwtWithExp(time.Now().Add(-time.Minute).Unix()):     StatusExpired,
	}
	i := 0
	for tok, want := range cases {
		i++
		v, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: fmt.Sprintf("p%d", i), Fields: []api.CredentialField{{Name: "t", Kind: KindBearer, Value: tok, Secret: true}}})
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != want {
			t.Fatalf("want %s got %s", want, v.Status)
		}
		if v.ExpiresAt == 0 {
			t.Fatal("expiry should be exposed")
		}
	}
	v, _ := svc.Upsert(project, api.UpsertCredentialRequest{Name: "blank", Fields: []api.CredentialField{{Name: "t", Kind: KindBearer, Secret: true}}})
	if v.Status != StatusEmpty {
		t.Fatalf("blank secret should be empty, got %s", v.Status)
	}
}

func TestResolveDefaultNoneExplicitAndApps(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	req := basicProfile("tok-abcdef-123456")
	req.Apps = []string{"api"}
	if _, err := svc.Upsert(project, req); err != nil {
		t.Fatal(err)
	}
	sc := svc.Scope(project)
	if a, err := sc.Resolve("", "api"); err != nil || a == nil || a.Name() != "admin" {
		t.Fatalf("default for allowed app: %v %v", a, err)
	}
	if a, err := sc.Resolve("", "web"); err != nil || a != nil {
		t.Fatalf("default must not apply to other apps: %v %v", a, err)
	}
	if _, err := sc.Resolve("admin", "web"); err == nil {
		t.Fatal("explicit use for a restricted app must fail")
	}
	if a, err := sc.Resolve("none", "api"); err != nil || a != nil {
		t.Fatal("none disables credentials")
	}
	if _, err := sc.Resolve("ghost", "api"); err == nil || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("missing profile should list names: %v", err)
	}
	other := svc.Scope("another-project")
	if _, err := other.Resolve("admin", "api"); err == nil {
		t.Fatal("profiles are per project")
	}
}

func TestApplyInjectsAllKinds(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	if _, err := svc.Upsert(project, basicProfile("tok-abcdef-123456")); err != nil {
		t.Fatal(err)
	}
	act, _ := svc.Scope(project).Resolve("admin", "")
	req, _ := http.NewRequest("GET", "http://localhost:3000/api/x?keep=1", nil)
	req.Header.Set("Cookie", "theme=dark; sid=old")
	req.Header.Set("Authorization", "Bearer agent-supplied")
	act.Apply(req)
	if got := req.Header.Get("Authorization"); got != "Bearer tok-abcdef-123456" {
		t.Fatalf("auth: %q", got)
	}
	if req.Header.Get("X-Tenant-Id") != "tenant-a" {
		t.Fatal("tenant header")
	}
	cookie := req.Header.Get("Cookie")
	if !strings.Contains(cookie, "theme=dark") || !strings.Contains(cookie, "sid=cookie-secret-9876") || strings.Contains(cookie, "sid=old") {
		t.Fatalf("cookie merge: %q", cookie)
	}
	q := req.URL.Query()
	if q.Get("key") != "query-secret-5555" || q.Get("keep") != "1" {
		t.Fatalf("query: %v", q)
	}
}

func TestApplyBasicAndCustomBearerHeader(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	_, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "b", Fields: []api.CredentialField{
		{Name: "login", Kind: KindBasic, Value: "user:pa55word"},
		{Name: "tok", Kind: KindBearer, Target: "X-Auth", Value: "tokentoken1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	act, _ := svc.Scope(project).Resolve("b", "")
	req, _ := http.NewRequest("GET", "http://localhost/", nil)
	act.Apply(req)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pa55word"))
	if req.Header.Get("Authorization") != want {
		t.Fatalf("basic: %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Auth") != "Bearer tokentoken1" {
		t.Fatal("custom bearer header")
	}
	sc := act.Scrubber()
	if strings.Contains(sc.Scrub(want), "pa55word") || strings.Contains(sc.Scrub("pass is pa55word"), "pa55word") {
		t.Fatal("basic credentials must be scrubbed in every form")
	}
}

func TestExpandPlaceholders(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	_, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "p", Fields: []api.CredentialField{
		{Name: "tenant", Kind: KindVariable, Value: "t/1 &x"},
		{Name: "pw", Kind: KindVariable, Value: `pa"ss\word`, Secret: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	act, _ := svc.Scope(project).Resolve("p", "")
	got, err := act.Expand("/t/${cred:tenant}", EscURL)
	if err != nil || got != "/t/t%2F1+%26x" {
		t.Fatalf("url escape: %q %v", got, err)
	}
	got, err = act.Expand(`{"p":"${cred:pw}"}`, BodyEscape(`{"p":"${cred:pw}"}`))
	if err != nil || got != `{"p":"pa\"ss\\word"}` {
		t.Fatalf("json escape: %q %v", got, err)
	}
	if _, err := act.Expand("${cred:nope}", EscNone); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("missing field must fail: %v", err)
	}
	if got, _ := act.Expand("plain ${HOME}", EscNone); got != "plain ${HOME}" {
		t.Fatal("env placeholders are untouched")
	}
}

func TestCookieJarCaptureApplyAndReset(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	v, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "j", Fields: []api.CredentialField{{Name: "session", Kind: KindCookie, Target: "sid", Value: "initial-session", Secret: true}}})
	if err != nil {
		t.Fatal(err)
	}
	act, _ := svc.Scope(project).Resolve("j", "")
	act.Capture("localhost", []*http.Cookie{{Name: "sid", Value: "rotated-session-id", Path: "/"}, {Name: "csrf", Value: "csrf-token-abcd", Path: "/", MaxAge: 3600}})
	act2, _ := svc.Scope(project).Resolve("j", "")
	req, _ := http.NewRequest("GET", "http://localhost:3000/a", nil)
	act2.Apply(req)
	c := req.Header.Get("Cookie")
	if !strings.Contains(c, "sid=rotated-session-id") || !strings.Contains(c, "csrf=csrf-token-abcd") {
		t.Fatalf("jar not applied: %q", c)
	}
	other, _ := http.NewRequest("GET", "http://example.com/a", nil)
	act2.Apply(other)
	if strings.Contains(other.Header.Get("Cookie"), "csrf") || strings.Contains(other.Header.Get("Cookie"), "rotated-session-id") {
		t.Fatalf("jar leaked to another host: %q", other.Header.Get("Cookie"))
	}
	list, _ := svc.List(project)
	for _, ck := range list[0].Cookies {
		if ck.Value != "" {
			t.Fatal("cookie values must not be listed")
		}
	}
	if len(list[0].Cookies) != 2 {
		t.Fatalf("cookies: %+v", list[0].Cookies)
	}
	sc := act2.Scrubber()
	if strings.Contains(sc.Scrub("x rotated-session-id y"), "rotated-session-id") {
		t.Fatal("jar values must be scrubbed")
	}
	edit := api.UpsertCredentialRequest{ID: v.ID, Name: "j", Fields: []api.CredentialField{{Name: "session", Kind: KindCookie, Target: "sid", Value: "user-replaced-value", Secret: true}}}
	if _, err := svc.Upsert(project, edit); err != nil {
		t.Fatal(err)
	}
	act3, _ := svc.Scope(project).Resolve("j", "")
	req3, _ := http.NewRequest("GET", "http://localhost:3000/a", nil)
	act3.Apply(req3)
	if !strings.Contains(req3.Header.Get("Cookie"), "sid=user-replaced-value") {
		t.Fatalf("editing a cookie field must override the jar: %q", req3.Header.Get("Cookie"))
	}
	if err := svc.ClearJar(project, v.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.List(project)
	if len(list[0].Cookies) != 0 {
		t.Fatal("jar should be empty")
	}
}

func loginServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			hits.Add(1)
			body, _ := io.ReadAll(r.Body)
			var in map[string]string
			_ = json.Unmarshal(body, &in)
			if in["user"] != "alice" || in["pass"] != `p"w` {
				http.Error(w, "bad login", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "refresh", Value: "refresh-cookie-value", Path: "/", MaxAge: 3600})
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": fmt.Sprintf("access-token-%d", hits.Load()), "expires_in": 120}})
		case "/me":
			if r.Header.Get("Authorization") == fmt.Sprintf("Bearer access-token-%d", hits.Load()) && hits.Load() > 0 {
				_, _ = io.WriteString(w, `{"user":"alice","echo":"`+r.Header.Get("Authorization")+`"}`)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
}

func recipeProfile() api.UpsertCredentialRequest {
	return api.UpsertCredentialRequest{
		Name: "login", Default: true,
		Fields: []api.CredentialField{
			{Name: "user", Kind: KindVariable, Value: "alice"},
			{Name: "pass", Kind: KindVariable, Value: `p"w`, Secret: true},
		},
		Recipe: &api.CredentialRecipe{
			Method: "POST", URL: "/auth/login", Body: `{"user":"${cred:user}","pass":"${cred:pass}"}`,
			Extract: []api.CredentialExtract{{Source: "json", Path: "data.access_token", Field: "token", Kind: KindBearer, ExpiresInPath: "data.expires_in"}},
		},
	}
}

func TestRecipeRefreshExtractsTokenCookieAndExpiry(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	srv := loginServer(t, &hits)
	defer srv.Close()
	if _, err := svc.Upsert(project, recipeProfile()); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(srv.URL)
	act, _ := svc.Scope(project).Resolve("login", "")
	if !act.NeedsRefresh() {
		t.Fatal("profile without a token needs a refresh")
	}
	if err := svc.Scope(project).Refresh(context.Background(), "login", base); err != nil {
		t.Fatal(err)
	}
	act, _ = svc.Scope(project).Resolve("login", "")
	if act.NeedsRefresh() {
		t.Fatal("fresh token must not need a refresh")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	act.Apply(req)
	if req.Header.Get("Authorization") != "Bearer access-token-1" {
		t.Fatalf("token not applied: %q", req.Header.Get("Authorization"))
	}
	if !strings.Contains(req.Header.Get("Cookie"), "refresh=refresh-cookie-value") {
		t.Fatalf("login cookie not captured: %q", req.Header.Get("Cookie"))
	}
	list, _ := svc.List(project)
	var tok api.CredentialField
	for _, f := range list[0].Fields {
		if f.Name == "token" {
			tok = f
		}
	}
	if tok.Name == "" || !tok.Secret || tok.Value != "" || tok.ExpiresAt == 0 {
		t.Fatalf("created field: %+v", tok)
	}
	if list[0].LastRefreshAt == 0 || list[0].LastError != "" {
		t.Fatalf("refresh bookkeeping: %+v", list[0])
	}
}

func TestRecipeFailureIsRecordedAndHidesSecrets(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	srv := loginServer(t, &hits)
	defer srv.Close()
	req := recipeProfile()
	req.Fields[1].Value = "wrong-password"
	if _, err := svc.Upsert(project, req); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(srv.URL)
	err := svc.Scope(project).Refresh(context.Background(), "login", base)
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "wrong-password") {
		t.Fatalf("error: %v", err)
	}
	list, _ := svc.List(project)
	if !strings.Contains(list[0].LastError, "401") {
		t.Fatalf("last_error should be recorded: %+v", list[0])
	}
}

func TestRefreshIsSerialised(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	var hits atomic.Int32
	srv := loginServer(t, &hits)
	defer srv.Close()
	if _, err := svc.Upsert(project, recipeProfile()); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(srv.URL)
	done := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() { done <- svc.Scope(project).Refresh(context.Background(), "login", base) }()
	}
	for i := 0; i < 6; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() < 1 {
		t.Fatal("login never ran")
	}
}

func TestJSONPathHelpers(t *testing.T) {
	var doc any
	_ = json.Unmarshal([]byte(`{"a":{"b":[{"c":"x"},{"c":2}]},"t":true}`), &doc)
	if v, ok := jsonPath(doc, "a.b.0.c"); !ok || v != "x" {
		t.Fatal(v, ok)
	}
	if v, ok := jsonPath(doc, "t"); !ok || v != "true" {
		t.Fatal(v, ok)
	}
	if _, ok := jsonPath(doc, "a.b.9.c"); ok {
		t.Fatal("out of range")
	}
	if _, ok := jsonPath(doc, "a.zz"); ok {
		t.Fatal("missing key")
	}
}

func TestSecretsCacheInvalidatesOnWrite(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	if len(svc.Secrets(project)) != 0 {
		t.Fatal("no secrets yet")
	}
	v, _ := svc.Upsert(project, api.UpsertCredentialRequest{Name: "a", Fields: []api.CredentialField{{Name: "t", Kind: KindBearer, Value: "first-secret-1", Secret: true}}})
	found := func(s string) bool {
		for _, x := range svc.Secrets(project) {
			if x == s {
				return true
			}
		}
		return false
	}
	if !found("first-secret-1") {
		t.Fatal("secret should be tracked")
	}
	_, _ = svc.Upsert(project, api.UpsertCredentialRequest{ID: v.ID, Name: "a", Fields: []api.CredentialField{{Name: "t", Kind: KindBearer, Value: "second-secret-2", Secret: true}}})
	if found("first-secret-1") || !found("second-secret-2") {
		t.Fatal("cache must refresh after an update")
	}
	_ = svc.Delete(project, v.ID)
	if len(svc.Secrets(project)) != 0 {
		t.Fatal("deleted profile secrets must go")
	}
}

func TestDeleteAndRevealErrors(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	if _, err := svc.Reveal(project, "cred_missing", "x"); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("got %v", err)
	}
	v, _ := svc.Upsert(project, basicProfile("tok-abcdef-123456"))
	if _, err := svc.Reveal(project, v.ID, "nope"); err == nil {
		t.Fatal("unknown field")
	}
	if _, err := svc.Reveal("other-project", v.ID, "token"); err == nil {
		t.Fatal("cross-project reveal must fail")
	}
	if err := svc.Delete("other-project", v.ID); err == nil {
		t.Fatal("cross-project delete must fail")
	}
}

func TestSecretLookingFieldsAreAlwaysSecret(t *testing.T) {
	svc, _, project, _ := newSvc(t)
	v, err := svc.Upsert(project, api.UpsertCredentialRequest{Name: "p", Fields: []api.CredentialField{
		{Name: "gateway", Kind: KindHeader, Target: "X-Api-Key", Value: "key-value-123456", Secret: false},
		{Name: "password", Kind: KindVariable, Value: "hunter2hunter2", Secret: false},
		{Name: "theme", Kind: KindCookie, Target: "theme", Value: "dark-mode-cookie", Secret: false},
		{Name: "tenant", Kind: KindHeader, Target: "X-Tenant-Id", Value: "tenant-a", Secret: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range v.Fields {
		want := f.Name != "tenant"
		if f.Secret != want {
			t.Fatalf("field %s secret=%v want %v", f.Name, f.Secret, want)
		}
		if f.Secret && f.Value != "" {
			t.Fatalf("secret %s leaked in view", f.Name)
		}
	}
	if raw, _ := svc.Reveal(project, v.ID, "gateway"); raw != "key-value-123456" {
		t.Fatalf("value is still stored: %q", raw)
	}
}
