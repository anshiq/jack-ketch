package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const modesYAML = `
version: 3
apps:
  api:
    workdir: services/api
    env_file: .env.legacy
    env: ["A=legacy"]
    env_sources:
      - file: .env
      - vars: {PORT: 3000, DEBUG: true}
    checks:
      - name: health
        url: /health
    default_mode: dev
    modes:
      dev:
        command: [npm, run, dev]
      prod:
        command: [npm, start]
        env_sources:
          - file: .env.production
            optional: true
          - vars: ["NODE_ENV=production"]
        checks:
          - name: health
            url: /healthz
            expect_status: 204
          - name: home
            url: /
`

func TestResolveModeLayersEnvAndChecks(t *testing.T) {
	cfg, errs := ParseV3([]byte(modesYAML))
	if errs != nil {
		t.Fatalf("errs = %v", errs)
	}
	app := cfg.Apps["api"].AppConfig
	dev, err := app.ResolveMode("")
	if err != nil || dev.Mode != "dev" || strings.Join(dev.Command, " ") != "npm run dev" {
		t.Fatalf("dev = %+v %v", dev, err)
	}
	var labels []string
	for _, s := range dev.EnvChain {
		labels = append(labels, s.Label)
	}
	if got := strings.Join(labels, ","); got != "env_file,app.env,file:.env,vars" {
		t.Fatalf("dev chain = %s", got)
	}
	if strings.Join(dev.EnvChain[3].Vars, ",") != "PORT=3000,DEBUG=true" {
		t.Fatalf("map vars keep document order: %v", dev.EnvChain[3].Vars)
	}
	prod, err := app.ResolveMode("prod")
	if err != nil {
		t.Fatal(err)
	}
	labels = nil
	for _, s := range prod.EnvChain {
		labels = append(labels, s.Label)
	}
	if got := strings.Join(labels, ","); got != "env_file,app.env,file:.env,vars,mode:prod/file:.env.production,mode:prod/vars" {
		t.Fatalf("prod chain = %s", got)
	}
	if !prod.EnvChain[4].Optional {
		t.Fatal("optional flag lost")
	}
	if len(prod.Checks) != 2 || prod.Checks[0].URL != "/healthz" || prod.Checks[0].ExpectStatus != 204 {
		t.Fatalf("checks = %+v", prod.Checks)
	}
	if prod.WorkDir != "services/api" {
		t.Fatalf("workdir inherited = %q", prod.WorkDir)
	}
	if _, err := app.ResolveMode("staging"); err == nil || !strings.Contains(err.Error(), "dev, prod") {
		t.Fatalf("unknown mode err = %v", err)
	}
}

func TestValidateModeErrors(t *testing.T) {
	cases := map[string]string{
		"both file and vars": "apps:\n  a:\n    env_sources:\n      - file: .env\n        vars: {A: b}\n",
		"empty source":       "apps:\n  a:\n    env_sources:\n      - optional: true\n",
		"bad default":        "apps:\n  a:\n    default_mode: prod\n    modes:\n      dev:\n        command: [x]\n",
		"bad mode name":      "apps:\n  a:\n    modes:\n      \"bad name\":\n        command: [x]\n",
		"check url and tcp":  "apps:\n  a:\n    checks:\n      - name: c\n        url: /x\n        tcp: localhost:1\n",
		"check no name":      "apps:\n  a:\n    checks:\n      - url: /x\n",
		"check bad url":      "apps:\n  a:\n    checks:\n      - name: c\n        url: health\n",
		"bad env key":        "apps:\n  a:\n    env_sources:\n      - vars: [\"1BAD=x\"]\n",
		"unknown mode field": "apps:\n  a:\n    modes:\n      dev:\n        bogus: 1\n",
		"bad regex":          "apps:\n  a:\n    modes:\n      dev:\n        readiness: [\"(\"]\n",
		"bad app name":       "apps:\n  \"my app\":\n    command: [x]\n",
	}
	for name, doc := range cases {
		if errs := Validate([]byte(doc)); len(errs) == 0 {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if errs := Validate([]byte(modesYAML)); len(errs) != 0 {
		t.Fatalf("valid doc rejected: %v", errs)
	}
	overlay := "apps:\n  api:\n    default_mode: staging\n"
	if errs := Validate([]byte(overlay)); len(errs) != 0 {
		t.Fatalf("overlay default_mode without modes must validate: %v", errs)
	}
}

func TestOverlayMergesModes(t *testing.T) {
	src := &MemorySource{
		ProjectYAML: []byte(modesYAML),
		Overlays:    map[string][]byte{"ws": []byte("apps:\n  api:\n    default_mode: local\n    modes:\n      local:\n        command: [npm, run, dev]\n        env_sources:\n          - vars: {API_URL: http://localhost:9000}\n")},
	}
	res, errs := Resolve(src, "p", "ws", t.TempDir(), nil)
	if errs != nil {
		t.Fatal(errs)
	}
	app := res.Apps["api"]
	if len(app.Modes) != 3 || app.DefaultMode != "local" {
		t.Fatalf("modes = %v default = %s", app.ModeNames(), app.DefaultMode)
	}
	r, err := app.ResolveMode("")
	if err != nil || r.Mode != "local" {
		t.Fatalf("resolved = %+v %v", r, err)
	}
}

func TestPlanDetectsModeChanges(t *testing.T) {
	oldCfg, _ := ParseV3([]byte(modesYAML))
	changed := strings.Replace(modesYAML, "command: [npm, start]", "command: [node, dist/server.js]", 1)
	newCfg, _ := ParseV3([]byte(changed))
	plan := Plan(oldCfg.Apps, newCfg.Apps, nil)
	if len(plan) != 1 || plan[0].Kind != ChangeRestartRequired || strings.Join(plan[0].Fields, ",") != "modes" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestEnvVarsMarshalQuotesNonStrings(t *testing.T) {
	out, err := yaml.Marshal(EnvSource{Vars: EnvVars{"PORT=3000", "DEBUG=true", "NAME=api", "EMPTY="}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`PORT: "3000"`, `DEBUG: "true"`, `NAME: api`, `EMPTY: ""`} {
		if !strings.Contains(s, want) {
			t.Fatalf("%q missing in\n%s", want, s)
		}
	}
	var back EnvSource
	if err := yaml.Unmarshal(out, &back); err != nil || strings.Join(back.Vars, ",") != "PORT=3000,DEBUG=true,NAME=api,EMPTY=" {
		t.Fatalf("round trip = %v %v", back.Vars, err)
	}
}
