package core

import (
	"strings"
	"testing"

	"jack-ketch/internal/config"
	"jack-ketch/pkg/api"
)

func TestDeriveMode(t *testing.T) {
	cases := []struct {
		cmd  []string
		env  []string
		task bool
		want string
	}{
		{[]string{"npm", "run", "dev"}, nil, false, "dev"},
		{[]string{"pnpm", "dev"}, nil, false, "dev"},
		{[]string{"yarn", "start"}, nil, false, "start"},
		{[]string{"npm", "run", "dev"}, []string{"NODE_ENV=production"}, false, "prod"},
		{[]string{"npm", "run", "dev", "--", "--headless"}, nil, false, "headless"},
		{[]string{"npm", "test"}, nil, true, "test"},
		{[]string{"npx", "vitest", "run"}, nil, true, "test"},
		{[]string{"npx", "next", "build"}, nil, true, "build"},
		{[]string{"go", "run", "./cmd/api"}, nil, false, "dev"},
		{[]string{"go", "test", "./..."}, nil, true, "test"},
		{[]string{"uv", "run", "python", "manage.py", "runserver"}, nil, false, "dev"},
		{[]string{"python", "manage.py", "migrate"}, nil, true, "migrate"},
		{[]string{"uvicorn", "app:app", "--reload"}, nil, false, "dev"},
		{[]string{"poetry", "run", "celery", "-A", "proj", "worker"}, nil, false, "worker"},
		{[]string{"./mvnw", "spring-boot:run"}, []string{"SPRING_PROFILES_ACTIVE=staging"}, false, "staging"},
		{[]string{"cargo", "run", "--release"}, nil, false, "release"},
		{[]string{"make", "serve"}, nil, false, "serve"},
		{[]string{"node", "server.js"}, nil, true, "run"},
		{[]string{"./weird-tool"}, nil, false, "default"},
	}
	for _, c := range cases {
		if got := DeriveMode(c.cmd, c.env, c.task); got != c.want {
			t.Errorf("DeriveMode(%v, %v, %v) = %q, want %q", c.cmd, c.env, c.task, got, c.want)
		}
	}
}

func TestUpsertModeNewAppAndExisting(t *testing.T) {
	d := saveDraft{app: "api", mode: "dev", newApp: true, appType: "node", workdir: "services/api",
		modeSpec: config.Mode{Command: []string{"npm", "run", "dev"}, EnvSources: []config.EnvSource{{File: "services/api/.env"}, {Vars: config.EnvVars{"PORT=4000"}}}}}
	cur := []byte("# keep me\nversion: 3\napps:\n  db:\n    command: [docker-free-db]\n")
	out, outcome, err := upsertMode(cur, d, nil, api.SaveRunRequest{Description: "local dev server"})
	if err != nil || outcome != "added" {
		t.Fatalf("upsert = %q %v", outcome, err)
	}
	for _, want := range []string{"# keep me", "db:", "workdir: services/api", "type: node", "default_mode: dev", "description: local dev server", "file: services/api/.env", `PORT: "4000"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if errs := config.Validate([]byte(out)); len(errs) != 0 {
		t.Fatalf("invalid result: %v\n%s", errs, out)
	}
	again, outcome, err := upsertMode([]byte(out), saveDraft{app: "api", mode: "dev", modeSpec: d.modeSpec}, nil, api.SaveRunRequest{})
	if err != nil || outcome != "unchanged" || again != "" {
		t.Fatalf("second save = %q %v", outcome, err)
	}
	withCheck, outcome, err := upsertMode([]byte(out), saveDraft{app: "api", mode: "dev", modeSpec: d.modeSpec}, []config.Check{{Name: "ping", URL: "/ping"}}, api.SaveRunRequest{})
	if err != nil || outcome != "replaced" || !strings.Contains(withCheck, "name: ping") || !strings.Contains(withCheck, "description: local dev server") {
		t.Fatalf("adding a check to an identical mode = %q %v\n%s", outcome, err, withCheck)
	}
	other := saveDraft{app: "api", mode: "dev", modeSpec: config.Mode{Command: []string{"npm", "start"}}}
	if _, _, err := upsertMode([]byte(out), other, nil, api.SaveRunRequest{}); err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("conflict err = %v", err)
	}
	replaced, outcome, err := upsertMode([]byte(out), other, []config.Check{{Name: "health", URL: "/health"}}, api.SaveRunRequest{Overwrite: true})
	if err != nil || outcome != "replaced" || !strings.Contains(replaced, "npm, start") || !strings.Contains(replaced, "name: health") || !strings.Contains(replaced, "description: local dev server") {
		t.Fatalf("overwrite = %q %v\n%s", outcome, err, replaced)
	}
	prod := saveDraft{app: "api", mode: "prod", modeSpec: config.Mode{Command: []string{"npm", "start"}, EnvSources: []config.EnvSource{{Vars: config.EnvVars{"NODE_ENV=production"}}}}}
	withProd, outcome, err := upsertMode([]byte(replaced), prod, nil, api.SaveRunRequest{SetDefault: true})
	if err != nil || outcome != "added" || !strings.Contains(withProd, "default_mode: prod") {
		t.Fatalf("add prod = %q %v\n%s", outcome, err, withProd)
	}
	cfg, errs := config.ParseV3([]byte(withProd))
	if errs != nil {
		t.Fatal(errs)
	}
	if names := cfg.Apps["api"].ModeNames(); strings.Join(names, ",") != "dev,prod" {
		t.Fatalf("modes = %v", names)
	}
}

func TestUpsertAppendOnlyMergesVars(t *testing.T) {
	cur := []byte("apps:\n  api:\n    modes:\n      dev:\n        command: [npm, run, dev]\n        env_sources:\n          - vars: {A: \"1\"}\n")
	d := saveDraft{app: "api", mode: "dev", appendOnly: true, modeSpec: config.Mode{EnvSources: []config.EnvSource{{File: ".env.local"}, {Vars: config.EnvVars{"A=2", "B=3"}}}}}
	out, outcome, err := upsertMode(cur, d, nil, api.SaveRunRequest{})
	if err != nil || outcome != "updated" {
		t.Fatalf("append = %q %v", outcome, err)
	}
	cfg, errs := config.ParseV3([]byte(out))
	if errs != nil {
		t.Fatal(errs)
	}
	srcs := cfg.Apps["api"].Modes["dev"].EnvSources
	if len(srcs) != 3 || srcs[1].File != ".env.local" || strings.Join(srcs[2].Vars, ",") != "A=2,B=3" {
		t.Fatalf("sources = %+v\n%s", srcs, out)
	}
}

func TestSaveableOrigins(t *testing.T) {
	if Saveable(RunOrigin{Command: []string{"bash", "-c", "x"}}) {
		t.Fatal("shell wrappers must not be suggested")
	}
	if !Saveable(RunOrigin{Command: []string{"npm", "run", "dev"}}) {
		t.Fatal("raw npm run should be saveable")
	}
	if Saveable(RunOrigin{App: "api"}) || !Saveable(RunOrigin{App: "api", Env: []string{"X=1"}}) {
		t.Fatal("app runs are saveable only with overrides")
	}
	if Saveable(RunOrigin{Command: []string{"npm"}, SavedApp: "api"}) || Saveable(RunOrigin{Command: []string{"zsh"}, Shell: true}) {
		t.Fatal("saved runs and shells are not saveable")
	}
}

func TestUpsertAppLevelChecks(t *testing.T) {
	cur := []byte("apps:\n  web:\n    command: [npm, run, dev]\n    checks:\n      - name: health\n        url: /health\n")
	d := saveDraft{app: "web", mode: "", appendOnly: true}
	out, outcome, err := upsertMode(cur, d, []config.Check{{Name: "home", URL: "/"}, {Name: "health", URL: "/healthz"}}, api.SaveRunRequest{})
	if err != nil || outcome != "updated" {
		t.Fatalf("upsert = %q %v", outcome, err)
	}
	cfg, errs := config.ParseV3([]byte(out))
	if errs != nil {
		t.Fatal(errs)
	}
	app := cfg.Apps["web"]
	if len(app.Modes) != 0 || len(app.Checks) != 2 || app.Checks[0].URL != "/healthz" || app.Checks[1].Name != "home" {
		t.Fatalf("app = %+v\n%s", app.AppConfig, out)
	}
	if _, outcome, _ := upsertMode([]byte(out), d, []config.Check{{Name: "home", URL: "/"}}, api.SaveRunRequest{}); outcome != "unchanged" {
		t.Fatalf("repeat outcome = %q", outcome)
	}
	if !strings.Contains(renderModeYAML("web", "", d, []config.Check{{Name: "home", URL: "/"}}, ""), "checks:") {
		t.Fatal("app-level preview missing checks")
	}
}
