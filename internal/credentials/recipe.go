package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"jack-ketch/pkg/api"
)

const maxRecipeBody = 1 << 20

type Doer func(req *http.Request) (*http.Response, error)

func (a *Active) NeedsRefresh() bool {
	if !a.HasRecipe() {
		return false
	}
	st, _ := a.Status()
	return st == StatusExpired || st == StatusExpiring || st == StatusEmpty
}

func (a *Active) Refresh(ctx context.Context, base *url.URL, do Doer) error {
	before := a.loaded.Row.LastRefreshAt
	a.svc.lockFor(a.profile).Lock()
	defer a.svc.lockFor(a.profile).Unlock()
	if !a.reload() {
		return errors.New("the credential profile no longer exists")
	}
	if a.loaded.Recipe == nil {
		return errors.New("this profile has no login recipe")
	}
	if a.loaded.Row.LastRefreshAt > before {
		return nil
	}
	err := a.runRecipe(ctx, base, do)
	if err != nil {
		a.persistLocked(false, err.Error())
		return err
	}
	return nil
}

func (a *Active) runRecipe(ctx context.Context, base *url.URL, do Doer) error {
	rc := a.loaded.Recipe
	method := strings.ToUpper(strings.TrimSpace(rc.Method))
	if method == "" {
		method = http.MethodPost
	}
	rawURL, err := a.Expand(rc.URL, EscURL)
	if err != nil {
		return fmt.Errorf("login recipe: %w", err)
	}
	target, err := joinURL(base, rawURL)
	if err != nil {
		return fmt.Errorf("login recipe url: %w", err)
	}
	var body io.Reader
	expanded := ""
	if rc.Body != "" {
		expanded, err = a.Expand(rc.Body, BodyEscape(rc.Body))
		if err != nil {
			return fmt.Errorf("login recipe body: %w", err)
		}
		body = strings.NewReader(expanded)
	}
	hctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, method, target.String(), body)
	if err != nil {
		return fmt.Errorf("login recipe request: %w", err)
	}
	for k, v := range rc.Headers {
		ev, err := a.Expand(v, EscNone)
		if err != nil {
			return fmt.Errorf("login recipe header %s: %w", k, err)
		}
		req.Header.Set(k, ev)
	}
	if expanded != "" && req.Header.Get("Content-Type") == "" {
		if BodyEscape(expanded) == EscJSON {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	req.Header.Set("Accept", "application/json, */*;q=0.5")
	resp, err := do(req)
	if err != nil {
		return fmt.Errorf("login recipe request failed: %s", a.Scrubber().Scrub(err.Error()))
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxRecipeBody))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("login recipe returned HTTP %d; check the username and password stored in this profile", resp.StatusCode)
	}
	now := a.svc.now()
	cookies := resp.Cookies()
	if a.loaded.Row.CaptureJar {
		a.mergeCookies(target.Hostname(), cookies)
	}
	var doc any
	if hasJSONExtract(rc.Extract) {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return errors.New("login recipe response is not JSON but a json extract rule is configured")
		}
	}
	for _, ex := range rc.Extract {
		val, ok := extractValue(ex, resp, cookies, data, doc)
		if !ok || val == "" {
			return fmt.Errorf("login recipe response has no %s %q to extract into %q", ex.Source, ex.Path, ex.Field)
		}
		exp := int64(0)
		if ex.ExpiresInPath != "" && doc != nil {
			if secs, ok := jsonPath(doc, ex.ExpiresInPath); ok {
				if n, err := strconv.ParseFloat(secs, 64); err == nil && n > 0 {
					exp = now.Add(time.Duration(n * float64(time.Second))).Unix()
				}
			}
		}
		if exp == 0 && rc.TTLSeconds > 0 {
			exp = now.Add(time.Duration(rc.TTLSeconds) * time.Second).Unix()
		}
		a.setField(ex, val, exp, now.Unix())
	}
	if len(rc.Extract) == 0 && !a.loaded.Row.CaptureJar {
		return errors.New("login recipe extracts nothing and cookie capture is off")
	}
	a.persistLocked(true, "")
	return nil
}

func hasJSONExtract(ex []api.CredentialExtract) bool {
	for _, e := range ex {
		if e.Source == "json" || e.ExpiresInPath != "" {
			return true
		}
	}
	return false
}

func (a *Active) setField(ex api.CredentialExtract, val string, exp, now int64) {
	for i := range a.loaded.Fields {
		if strings.EqualFold(a.loaded.Fields[i].Name, ex.Field) {
			a.loaded.Fields[i].Value = val
			a.loaded.Fields[i].ExpiresAt = exp
			a.loaded.Fields[i].UpdatedAt = now
			return
		}
	}
	kind := ex.Kind
	if kind == "" {
		kind = KindBearer
	}
	a.loaded.Fields = append(a.loaded.Fields, api.CredentialField{Name: ex.Field, Kind: kind, Target: ex.Target, Secret: true, Value: val, ExpiresAt: exp, UpdatedAt: now})
}

func (a *Active) mergeCookies(host string, cookies []*http.Cookie) {
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		idx := -1
		for i, e := range a.loaded.Jar {
			if e.Name == c.Name && strings.EqualFold(e.Host, host) {
				idx = i
			}
		}
		if c.MaxAge < 0 || c.Value == "" || (!c.Expires.IsZero() && !c.Expires.After(a.svc.now())) {
			if idx >= 0 {
				a.loaded.Jar = append(a.loaded.Jar[:idx], a.loaded.Jar[idx+1:]...)
			}
			continue
		}
		entry := api.CredentialCookie{Name: c.Name, Value: c.Value, Host: strings.ToLower(host), Path: c.Path, Secure: c.Secure}
		if c.MaxAge > 0 {
			entry.Expires = a.svc.now().Add(time.Duration(c.MaxAge) * time.Second).Unix()
		} else if !c.Expires.IsZero() {
			entry.Expires = c.Expires.Unix()
		}
		if idx >= 0 {
			a.loaded.Jar[idx] = entry
		} else {
			a.loaded.Jar = append(a.loaded.Jar, entry)
		}
	}
}

func extractValue(ex api.CredentialExtract, resp *http.Response, cookies []*http.Cookie, body []byte, doc any) (string, bool) {
	switch ex.Source {
	case "json":
		return jsonPath(doc, ex.Path)
	case "header":
		v := resp.Header.Get(ex.Path)
		v = strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
		return v, v != ""
	case "cookie":
		for _, c := range cookies {
			if c.Name == ex.Path {
				return c.Value, c.Value != ""
			}
		}
		return "", false
	case "regex":
		re, err := regexp.Compile(ex.Path)
		if err != nil {
			return "", false
		}
		m := re.FindSubmatch(body)
		if m == nil {
			return "", false
		}
		if len(m) > 1 {
			return string(m[1]), len(m[1]) > 0
		}
		return string(m[0]), true
	}
	return "", false
}

func jsonPath(doc any, path string) (string, bool) {
	cur := doc
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return "", false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return "", false
			}
			cur = node[i]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	case nil:
		return "", false
	default:
		b, err := json.Marshal(v)
		return string(b), err == nil
	}
}
