package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jack-ketch/internal/credentials"
	"jack-ketch/internal/process"
	"jack-ketch/pkg/api"
)

func (r *Runtime) SetCredentials(sc *credentials.Scope) { r.creds.Store(sc) }

func (r *Runtime) Credentials() *credentials.Scope { return r.creds.Load() }

func (r *Runtime) CheckReveal(tool string, args map[string]any) error {
	return r.deny(tool, args, r.policyNow().DenyReveal)
}

func noCredential(name string) bool {
	return name == "" || strings.EqualFold(name, "none")
}

func (r *Runtime) probeCredential(proc *process.ManagedProcess, req api.ProbeRequest, res *api.ProbeResult) (*credentials.Active, *credentials.Scrubber, error) {
	scope := r.creds.Load()
	name := strings.TrimSpace(req.Credential)
	if scope == nil {
		if !noCredential(name) {
			return nil, nil, errors.New("credential profiles are only available when jack-ketch runs through the daemon")
		}
		return nil, &credentials.Scrubber{}, nil
	}
	if req.TCP != "" {
		if !noCredential(name) {
			return nil, nil, errors.New("credential profiles apply to HTTP requests, not tcp connects")
		}
		return nil, scope.Scrubber(), nil
	}
	act, err := scope.Resolve(name, proc.Spec().App)
	if err != nil {
		if name != "" {
			return nil, nil, err
		}
		res.CredentialHint = "the default credential profile could not be used: " + err.Error()
		return nil, scope.Scrubber(), nil
	}
	if act == nil {
		return nil, scope.Scrubber(), nil
	}
	res.Credential = act.Name()
	return act, act.Scrubber(), nil
}

func expandCredential(act *credentials.Active, s string, esc credentials.Escape) (string, error) {
	if act == nil {
		if refs := credentials.ExpandRefs(s); len(refs) > 0 {
			return "", fmt.Errorf("the request uses ${cred:%s} but no credential profile is active; pass credential=<profile> (see list_credentials)", refs[0])
		}
		return s, nil
	}
	return act.Expand(s, esc)
}

type gateResult struct {
	reason string
	block  string
}

func credentialGate(act *credentials.Active, u *url.URL, method string, preapproved, confirmed bool) *gateResult {
	loop := loopbackHost(u.Hostname())
	if act != nil && preapproved && !loop {
		return &gateResult{block: fmt.Sprintf("credential profile %q is not sent to %s from a saved check; use probe so the user can approve it", act.Name(), u.Host)}
	}
	if preapproved || confirmed {
		return nil
	}
	suffix := ""
	if act != nil {
		suffix = " (credential profile " + act.Name() + " would be sent)"
	}
	switch {
	case !loop:
		reason := "the target " + u.Host + " is not on this machine"
		if act != nil {
			reason += " and credential profile " + act.Name() + " would be sent to it"
		}
		return &gateResult{reason: reason}
	case !safeMethod(method):
		return &gateResult{reason: method + " requests can change data" + suffix}
	case act != nil && strings.HasPrefix(strings.ToLower(act.Environment()), "prod"):
		return &gateResult{reason: "credential profile " + act.Name() + " is tagged " + act.Environment()}
	}
	return nil
}

type probeCall struct {
	req     api.ProbeRequest
	res     *api.ProbeResult
	act     *credentials.Active
	scrub   *credentials.Scrubber
	env     []string
	target  *url.URL
	method  string
	timeout time.Duration
	maxBody int
	start   time.Time
}

func originOf(u *url.URL) *url.URL {
	return &url.URL{Scheme: u.Scheme, Host: u.Host}
}

func (r *Runtime) probeHTTP(ctx context.Context, c probeCall) error {
	req, res, act := c.req, c.res, c.act
	bodyStr := ""
	if req.Body != "" {
		var err error
		bodyStr, err = expandCredential(act, expandFromEnv(req.Body, c.env), credentials.BodyEscape(req.Body))
		if err != nil {
			return err
		}
	}
	headers := make(map[string]string, len(req.Headers))
	for k, v := range req.Headers {
		ev, err := expandCredential(act, expandFromEnv(v, c.env), credentials.EscNone)
		if err != nil {
			return err
		}
		headers[k] = ev
	}
	client := &http.Client{Timeout: c.timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func() (*http.Response, context.CancelFunc, error) {
		hctx, cancel := context.WithTimeout(ctx, c.timeout)
		var body io.Reader
		if req.Body != "" {
			body = strings.NewReader(bodyStr)
		}
		hreq, err := http.NewRequestWithContext(hctx, c.method, c.target.String(), body)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		for k, v := range headers {
			hreq.Header.Set(k, v)
		}
		if req.Body != "" && hreq.Header.Get("Content-Type") == "" {
			trimmed := strings.TrimSpace(req.Body)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				hreq.Header.Set("Content-Type", "application/json")
			}
		}
		hreq.Header.Set("User-Agent", "jack-ketch-probe")
		if act != nil {
			act.Apply(hreq)
		}
		resp, err := client.Do(hreq)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		return resp, cancel, nil
	}
	do := func(rq *http.Request) (*http.Response, error) { return client.Do(rq) }
	if act != nil && act.NeedsRefresh() {
		if rerr := act.Refresh(ctx, originOf(c.target), do); rerr != nil {
			res.Failures = append(res.Failures, "credential refresh failed: "+rerr.Error())
		} else {
			res.CredentialRefreshed = true
		}
	}
	resp, cancel, err := send()
	if err == nil && act != nil && resp.StatusCode == http.StatusUnauthorized && act.HasRecipe() && !res.CredentialRefreshed {
		if rerr := act.Refresh(ctx, originOf(c.target), do); rerr != nil {
			res.Failures = append(res.Failures, "credential refresh failed: "+rerr.Error())
		} else {
			res.CredentialRefreshed = true
			_ = resp.Body.Close()
			cancel()
			resp, cancel, err = send()
		}
	}
	if err != nil {
		res.DurationMS = time.Since(c.start).Milliseconds()
		res.Error = err.Error()
		res.Failures = append(res.Failures, "request failed: "+err.Error())
		return nil
	}
	data, rerr := io.ReadAll(io.LimitReader(resp.Body, int64(c.maxBody)+1))
	_ = resp.Body.Close()
	cancel()
	if act != nil {
		act.Capture(c.target.Hostname(), resp.Cookies())
	}
	res.DurationMS = time.Since(c.start).Milliseconds()
	res.Status = resp.StatusCode
	res.StatusText = resp.Status
	res.Headers = pickHeaders(resp.Header)
	if len(data) > c.maxBody {
		data = data[:c.maxBody]
		res.BodyTruncated = true
	}
	res.BodyBytes = len(data)
	res.Body = string(bytes.ToValidUTF8(data, []byte("?")))
	if rerr != nil {
		res.Failures = append(res.Failures, "reading body: "+rerr.Error())
	}
	res.OK = true
	return nil
}

func (r *Runtime) finishCredential(act *credentials.Active, res *api.ProbeResult) {
	if act == nil {
		return
	}
	act.Touch()
	st, _ := act.Status()
	res.CredentialStatus = st
	switch {
	case res.Status == http.StatusUnauthorized:
		res.CredentialStatus = "rejected"
		hint := fmt.Sprintf("credential profile %q was rejected (HTTP 401). Do not guess or retry other values; ask the user to update it on the Credentials page of the web UI.", act.Name())
		if res.CredentialRefreshed {
			hint = fmt.Sprintf("credential profile %q was rejected (HTTP 401) even after its login recipe ran. Ask the user to check the stored login on the Credentials page of the web UI.", act.Name())
		}
		res.CredentialHint = hint
	case (st == credentials.StatusExpired || st == credentials.StatusEmpty) && !res.CredentialRefreshed:
		res.CredentialHint = fmt.Sprintf("credential profile %q is %s; ask the user to update it on the Credentials page of the web UI.", act.Name(), st)
	}
}

func scrubResult(res *api.ProbeResult, s *credentials.Scrubber) {
	if s.Empty() {
		return
	}
	res.Body = s.Scrub(res.Body)
	res.Error = s.Scrub(res.Error)
	res.URL = s.Scrub(res.URL)
	res.StatusText = s.Scrub(res.StatusText)
	res.Headers = s.ScrubMap(res.Headers)
	for i := range res.Failures {
		res.Failures[i] = s.Scrub(res.Failures[i])
	}
	res.CredentialHint = s.Scrub(res.CredentialHint)
	if c := res.Check; c != nil {
		c.Name = s.Scrub(c.Name)
		c.URL = s.Scrub(c.URL)
		c.Body = s.Scrub(c.Body)
		c.Headers = s.ScrubMap(c.Headers)
	}
}

func (r *Runtime) ProcessBase(processID string, port int) (*url.URL, error) {
	proc, ok := r.manager.Get(processID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", processID)
	}
	base, _, err := r.probeBase(proc, port)
	if err != nil {
		return nil, err
	}
	return url.Parse(base)
}

func LoopbackTarget(u *url.URL) bool { return loopbackHost(u.Hostname()) }
