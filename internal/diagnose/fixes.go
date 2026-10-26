package diagnose

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"jack-ketch/pkg/api"
)

type projectFiles struct {
	workDir    string
	projectDir string
	cache      map[string]string
}

func scanProject(workDir, projectDir string) *projectFiles {
	if projectDir == "" {
		projectDir = workDir
	}
	return &projectFiles{workDir: workDir, projectDir: projectDir, cache: map[string]string{}}
}

func (p *projectFiles) dirs() []string {
	if p.workDir == "" {
		return nil
	}
	var out []string
	dir := filepath.Clean(p.workDir)
	root := filepath.Clean(p.projectDir)
	for {
		out = append(out, dir)
		if dir == root || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

func (p *projectFiles) find(name string) string {
	if v, ok := p.cache[name]; ok {
		return v
	}
	found := ""
	for _, d := range p.dirs() {
		cand := filepath.Join(d, name)
		if _, err := os.Stat(cand); err == nil {
			found = cand
			break
		}
	}
	p.cache[name] = found
	return found
}

func (p *projectFiles) has(name string) bool { return p.find(name) != "" }

func (p *projectFiles) inWorkDir(name string) bool {
	if p.workDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(p.workDir, name))
	return err == nil
}

func (p *projectFiles) rel(name string) string {
	abs := p.find(name)
	if abs == "" {
		abs = filepath.Join(p.workDir, name)
	}
	if rel, err := filepath.Rel(p.projectDir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}

func (p *projectFiles) dirOf(name string) string {
	if f := p.find(name); f != "" {
		return filepath.Dir(f)
	}
	return p.workDir
}

func (p *projectFiles) read(name string) string {
	f := p.find(name)
	if f == "" {
		return ""
	}
	data, err := os.ReadFile(f)
	if err != nil || len(data) > 2<<20 {
		return ""
	}
	return string(data)
}

func (p *projectFiles) nodePM() string {
	switch {
	case p.has("pnpm-lock.yaml"):
		return "pnpm"
	case p.has("yarn.lock"):
		return "yarn"
	case p.has("bun.lockb"), p.has("bun.lock"):
		return "bun"
	}
	return "npm"
}

func nodeAdd(pm, pkg string) string {
	if pm == "npm" {
		return "npm install " + pkg
	}
	return pm + " add " + pkg
}

type exampleEntry struct {
	value string
	hint  string
}

type exampleFile struct {
	name    string
	order   []string
	entries map[string]exampleEntry
}

var exampleNames = []string{".env.example", ".env.sample", ".env.template", ".env.dist", "example.env", ".env.defaults", "env.example"}

func (p *projectFiles) envExamples() []exampleFile {
	var out []exampleFile
	seen := map[string]bool{}
	for _, d := range p.dirs() {
		for _, n := range exampleNames {
			path := filepath.Join(d, n)
			if seen[path] {
				continue
			}
			seen[path] = true
			ef, ok := parseExample(path)
			if !ok {
				continue
			}
			if rel, err := filepath.Rel(p.projectDir, path); err == nil && !strings.HasPrefix(rel, "..") {
				ef.name = rel
			} else {
				ef.name = path
			}
			out = append(out, ef)
		}
	}
	return out
}

func parseExample(path string) (exampleFile, bool) {
	f, err := os.Open(path)
	if err != nil {
		return exampleFile{}, false
	}
	defer f.Close()
	ef := exampleFile{entries: map[string]exampleEntry{}}
	var comment []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			comment = nil
			continue
		}
		if strings.HasPrefix(line, "#") {
			c := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if c != "" && !strings.Contains(c, "=") {
				comment = append(comment, c)
			}
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if !upperKeyRe.MatchString(k) && !strings.Contains(k, "_") {
			continue
		}
		v = strings.TrimSpace(v)
		inline := ""
		if i := strings.Index(v, " #"); i >= 0 {
			inline = strings.TrimSpace(v[i+2:])
			v = strings.TrimSpace(v[:i])
		}
		v = strings.Trim(v, `"'`)
		hint := strings.Join(comment, " ")
		if inline != "" {
			hint = strings.TrimSpace(hint + " " + inline)
		}
		if _, dup := ef.entries[k]; !dup {
			ef.order = append(ef.order, k)
		}
		ef.entries[k] = exampleEntry{value: v, hint: truncate(hint, 200)}
		comment = nil
	}
	return ef, len(ef.order) > 0
}

type serviceInfo struct {
	name  string
	image string
	env   func(u *url.URL) []string
	extra string
}

var servicesByPort = map[int]serviceInfo{
	5432: {name: "postgres", image: "postgres:16", env: func(u *url.URL) []string {
		user, pass, db := urlParts(u, "postgres", "postgres", "")
		out := []string{"POSTGRES_USER=" + user, "POSTGRES_PASSWORD=" + pass}
		if db != "" {
			out = append(out, "POSTGRES_DB="+db)
		}
		return out
	}},
	3306: {name: "mysql", image: "mysql:8", env: func(u *url.URL) []string {
		user, pass, db := urlParts(u, "root", "root", "")
		out := []string{"MYSQL_ROOT_PASSWORD=" + pass}
		if user != "root" {
			out = append(out, "MYSQL_USER="+user, "MYSQL_PASSWORD="+pass)
		}
		if db != "" {
			out = append(out, "MYSQL_DATABASE="+db)
		}
		return out
	}},
	6379:  {name: "redis", image: "redis:7"},
	27017: {name: "mongodb", image: "mongo:7"},
	5672:  {name: "rabbitmq", image: "rabbitmq:3-management", extra: "-p 15672:15672"},
	9200:  {name: "elasticsearch", image: "docker.elastic.co/elasticsearch/elasticsearch:8.13.4", extra: "-e discovery.type=single-node -e xpack.security.enabled=false"},
	9092:  {name: "kafka", image: "apache/kafka:3.7.0"},
	11211: {name: "memcached", image: "memcached:1.6"},
	4566:  {name: "localstack", image: "localstack/localstack"},
	1025:  {name: "mailpit", image: "axllent/mailpit", extra: "-p 8025:8025"},
	9000:  {name: "minio", image: "minio/minio", extra: "-p 9001:9001 server /data --console-address :9001"},
	7700:  {name: "meilisearch", image: "getmeili/meilisearch:v1.8"},
	6333:  {name: "qdrant", image: "qdrant/qdrant"},
	8123:  {name: "clickhouse", image: "clickhouse/clickhouse-server"},
	1433:  {name: "mssql", image: "mcr.microsoft.com/mssql/server:2022-latest", extra: "-e ACCEPT_EULA=Y -e MSSQL_SA_PASSWORD=Passw0rd!dev"},
	4222:  {name: "nats", image: "nats:2"},
	2181:  {name: "zookeeper", image: "zookeeper:3.9"},
	26257: {name: "cockroachdb", image: "cockroachdb/cockroach", extra: "start-single-node --insecure"},
}

var schemePorts = map[string]int{
	"postgres": 5432, "postgresql": 5432, "mysql": 3306, "mariadb": 3306, "redis": 6379, "rediss": 6379,
	"mongodb": 27017, "mongodb+srv": 27017, "amqp": 5672, "amqps": 5672, "nats": 4222,
}

var nameKeywords = map[string]int{
	"postgres": 5432, "psycopg": 5432, "pg": 5432, "mysql": 3306, "mariadb": 3306, "redis": 6379, "mongo": 27017,
	"rabbit": 5672, "amqp": 5672, "elastic": 9200, "kafka": 9092, "memcache": 11211,
}

func urlParts(u *url.URL, defUser, defPass, defDB string) (string, string, string) {
	user, pass, db := defUser, defPass, defDB
	if u == nil {
		return user, pass, db
	}
	if u.User != nil {
		if n := u.User.Username(); n != "" {
			user = n
		}
		if p, ok := u.User.Password(); ok && p != "" {
			pass = p
		}
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		db = p
	}
	return user, pass, db
}

func isLocalHost(h string) bool {
	switch strings.ToLower(strings.Trim(h, "[]")) {
	case "", "localhost", "127.0.0.1", "::1", "0.0.0.0", "host.docker.internal", "::":
		return true
	}
	return false
}

func (a *analysis) envURLFor(port int) (*url.URL, string) {
	for _, kv := range a.in.Env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.Contains(v, "://") {
			continue
		}
		u, err := url.Parse(v)
		if err != nil {
			continue
		}
		sp, known := schemePorts[strings.ToLower(u.Scheme)]
		if !known {
			continue
		}
		up := sp
		if u.Port() != "" {
			if n, err := strconv.Atoi(u.Port()); err == nil {
				up = n
			}
		}
		if port == 0 || up == port || sp == port {
			return u, k
		}
	}
	return nil, ""
}

func (a *analysis) serviceIssue(hits []match) (api.Issue, *api.ServiceHint) {
	host, port := "", 0
	notFound := ""
	for _, h := range hits {
		if strings.Contains(h.line, "getaddrinfo") && len(h.groups) > 0 && h.groups[0] != "" {
			notFound = h.groups[0]
			continue
		}
		if len(h.groups) >= 2 && h.groups[1] != "" {
			if p, err := strconv.Atoi(h.groups[1]); err == nil {
				host, port = h.groups[0], p
				break
			}
		}
		if len(h.groups) == 1 && h.groups[0] != "" && host == "" {
			if strings.Contains(strings.ToLower(h.line), "mysql") {
				host, port = h.groups[0], 3306
			}
		}
	}
	u, envKey := a.envURLFor(port)
	if port == 0 && u != nil {
		port = schemePorts[strings.ToLower(u.Scheme)]
		if u.Port() != "" {
			if n, err := strconv.Atoi(u.Port()); err == nil {
				port = n
			}
		}
		host = u.Hostname()
	}
	if port == 0 {
		lower := strings.ToLower(strings.Join(evidence(hits), " "))
		for kw, p := range nameKeywords {
			if strings.Contains(lower, kw) {
				port = p
				break
			}
		}
	}
	svc, known := servicesByPort[port]
	name := "the service"
	if known {
		name = svc.name
	} else if port > 0 {
		name = fmt.Sprintf("the service on port %d", port)
	}
	hint := &api.ServiceHint{Name: name, Host: host, Port: port}
	iss := api.Issue{Category: CatServiceDown, Evidence: evidence(hits)}
	if notFound != "" && host == "" {
		hint.Host = notFound
		iss.Actor = ActorAgentApproval
		iss.Summary = fmt.Sprintf("The host %q cannot be resolved.", notFound)
		iss.Steps = []string{
			fmt.Sprintf("%q looks like a docker-compose service name or an internal hostname. Outside compose, local services listen on localhost.", notFound),
			"Point the connection setting at localhost (ask the user before changing their env files), or start the dependency.",
		}
		if envKey != "" {
			iss.Steps = append(iss.Steps, "The connection comes from "+envKey+".")
		}
		return iss, hint
	}
	if !isLocalHost(host) {
		iss.Actor = ActorUser
		iss.Summary = fmt.Sprintf("Cannot reach %s at %s:%d (a remote host).", name, host, port)
		iss.Steps = []string{"The dependency runs on another machine. Ask the user whether it should be reachable (VPN, firewall, credentials, right environment).", "If local development should use a local instance, the user can switch the host to localhost in the env file."}
		if envKey != "" {
			iss.Steps = append(iss.Steps, "The connection comes from "+envKey+".")
		}
		return iss, hint
	}
	iss.Actor = ActorAgentApproval
	if port > 0 {
		iss.Summary = fmt.Sprintf("%s is not running on localhost:%d.", capitalize(name), port)
	} else {
		iss.Summary = "A dependency the app connects to is not running."
	}
	key := strconv.Itoa(port)
	if appName := a.in.ServiceApps[key]; appName != "" {
		hint.ProjectApp = appName
	} else if known {
		hint.ProjectApp = a.in.ServiceApps[svc.name]
	}
	steps := []string{}
	if hint.ProjectApp != "" {
		steps = append(steps, fmt.Sprintf("This project declares app %q for it: start it with start_process(app=%q, timeout_ms=60000), then restart_process this one.", hint.ProjectApp, hint.ProjectApp))
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "start_app", Description: "Start the project's own " + name + " app", Command: fmt.Sprintf("start_process(app=%q)", hint.ProjectApp), NeedsApproval: false})
		iss.Actor = ActorAgent
	}
	if known {
		hint.Image = svc.image
		project := filepath.Base(a.in.ProjectDir)
		if project == "." || project == "/" || project == "" {
			project = "dev"
		}
		container := sanitize(project) + "-" + svc.name
		var envArgs []string
		if svc.env != nil {
			for _, e := range svc.env(u) {
				envArgs = append(envArgs, "-e "+shellQuote(e))
			}
		}
		run := fmt.Sprintf("docker run -d --name %s -p %d:%d", container, port, port)
		if len(envArgs) > 0 {
			run += " " + strings.Join(envArgs, " ")
		}
		if svc.extra != "" && !strings.Contains(svc.extra, "server ") && !strings.Contains(svc.extra, "start-single-node") {
			run += " " + svc.extra
		}
		run += " " + svc.image
		if strings.Contains(svc.extra, "server ") || strings.Contains(svc.extra, "start-single-node") {
			run += " " + svc.extra
		}
		hint.RunHint = run
		steps = append(steps,
			fmt.Sprintf("Check for an existing %s container to reuse first: docker ps -a --filter publish=%d (docker start <name> if it is stopped).", svc.name, port),
			"Otherwise, with the user's permission, start one: "+run,
			"Wait until it accepts connections, then restart_process this app.")
		iss.Fixes = append(iss.Fixes,
			api.SuggestedFix{Kind: "command", Description: "Look for a reusable container", Command: fmt.Sprintf("docker ps -a --filter publish=%d", port), NeedsApproval: false},
			api.SuggestedFix{Kind: "container", Description: "Start " + svc.name + " in Docker", Command: run, NeedsApproval: true})
	} else {
		steps = append(steps, "Start the dependency (ask the user how they normally run it), then restart_process this app.")
	}
	if envKey != "" {
		steps = append(steps, "The connection settings come from "+envKey+"; container credentials above match it.")
	}
	iss.Steps = steps
	return iss, hint
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./-_", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (a *analysis) migrationIssue(hits []match) api.Issue {
	iss := api.Issue{Category: CatMigration, Actor: ActorAgentApproval, Evidence: evidence(hits),
		Summary: "The database schema is behind the code: tables are missing or migrations are pending."}
	var cmd, dir string
	switch {
	case a.files.has("manage.py"):
		cmd, dir = "python manage.py migrate", a.files.dirOf("manage.py")
	case a.files.has("prisma/schema.prisma"):
		cmd, dir = "npx prisma migrate dev", a.files.dirOf("prisma/schema.prisma")
		dir = filepath.Dir(dir)
	case a.files.has("alembic.ini"):
		cmd, dir = "alembic upgrade head", a.files.dirOf("alembic.ini")
	case a.files.has("bin/rails"):
		cmd, dir = "bin/rails db:migrate", filepath.Dir(a.files.dirOf("bin/rails"))
	case a.files.has("knexfile.js"), a.files.has("knexfile.ts"):
		cmd, dir = "npx knex migrate:latest", a.files.workDir
	case a.files.has("drizzle.config.ts"), a.files.has("drizzle.config.js"):
		cmd, dir = "npx drizzle-kit migrate", a.files.workDir
	case a.files.has(".sequelizerc"):
		cmd, dir = "npx sequelize-cli db:migrate", a.files.workDir
	}
	if cmd != "" {
		iss.Steps = []string{
			"Ask the user before migrating: it changes the database (confirm it is a local/dev database).",
			fmt.Sprintf("Run it as an app-tied one-shot: start_process(command=%q, args=[...], workdir=%q, timeout_ms=120000) and check wait.exit_code.", strings.Fields(cmd)[0], relTo(a.in.ProjectDir, dir)),
			"Then restart_process this app.",
		}
		iss.Fixes = []api.SuggestedFix{{Kind: "migration", Description: "Apply pending migrations", Command: cmd, WorkDir: relTo(a.in.ProjectDir, dir), NeedsApproval: true}}
	} else {
		iss.Steps = []string{"Find how this project applies migrations (README, package scripts, Makefile), ask the user, run it, then restart_process."}
	}
	return iss
}

func where(base, dir string) string {
	r := relTo(base, dir)
	if r == "." || r == "" {
		return "the project root"
	}
	return r
}

func relTo(base, p string) string {
	if p == "" {
		return "."
	}
	if rel, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

var nodeBuiltins = map[string]bool{"fs": true, "path": true, "http": true, "https": true, "os": true, "crypto": true, "util": true, "url": true, "stream": true, "events": true, "child_process": true, "net": true, "zlib": true, "buffer": true, "assert": true, "tls": true, "dns": true, "readline": true, "worker_threads": true, "querystring": true, "module": true, "process": true}

var pyPackageNames = map[string]string{
	"yaml": "pyyaml", "cv2": "opencv-python", "PIL": "pillow", "sklearn": "scikit-learn", "dotenv": "python-dotenv",
	"bs4": "beautifulsoup4", "jwt": "pyjwt", "psycopg2": "psycopg2-binary", "MySQLdb": "mysqlclient", "attr": "attrs",
	"dateutil": "python-dateutil", "magic": "python-magic", "Crypto": "pycryptodome", "jose": "python-jose",
	"multipart": "python-multipart", "google": "protobuf", "docx": "python-docx", "dns": "dnspython", "serial": "pyserial",
	"OpenSSL": "pyopenssl", "zmq": "pyzmq", "git": "gitpython", "fitz": "pymupdf", "slugify": "python-slugify",
}

func nodeRoot(mod string) string {
	if strings.HasPrefix(mod, "@") {
		parts := strings.SplitN(mod, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return mod
	}
	return strings.SplitN(mod, "/", 2)[0]
}

func (a *analysis) depIssue(hits []match) (api.Issue, bool) {
	iss := api.Issue{Category: CatMissingDep, Actor: ActorAgentApproval, Evidence: evidence(hits)}
	var nodeMods, pyMods, goPkgs []string
	kinds := map[string]bool{}
	localImport := ""
	for _, h := range hits {
		g := firstGroup(h)
		l := h.line
		switch {
		case strings.Contains(l, "No module named"):
			if g != "" {
				pyMods = appendUnique(pyMods, strings.SplitN(g, ".", 2)[0])
			}
		case strings.Contains(l, "Cannot find module"), strings.Contains(l, "Can't resolve"), strings.Contains(l, "Cannot find package '"), strings.Contains(l, "Failed to resolve import"):
			if g == "" {
				continue
			}
			if strings.HasPrefix(g, ".") || strings.HasPrefix(g, "/") || strings.HasPrefix(g, "~") {
				localImport = g
				continue
			}
			root := nodeRoot(strings.TrimPrefix(g, "node:"))
			if nodeBuiltins[root] || strings.HasPrefix(g, "node:") {
				continue
			}
			nodeMods = appendUnique(nodeMods, root)
		case strings.Contains(l, "go.sum"), strings.Contains(l, "no required module provides"), strings.Contains(l, "cannot find package \""), strings.Contains(l, "updates to go.mod needed"):
			if g != "" {
				goPkgs = appendUnique(goPkgs, g)
			}
			kinds["go"] = true
		case strings.Contains(l, "gem") || strings.Contains(l, "Bundler"):
			kinds["ruby"] = true
		case strings.Contains(l, "Could not resolve dependencies"), strings.Contains(l, "Could not find artifact"):
			kinds["maven"] = true
		case strings.Contains(l, "Could not resolve all"):
			kinds["gradle"] = true
		case strings.Contains(l, "vendor/autoload.php"):
			kinds["composer"] = true
		case strings.Contains(l, "npm install"), strings.Contains(l, "yarn install"), strings.Contains(l, "pnpm install"):
			kinds["node"] = true
		}
	}
	if len(nodeMods) == 0 && len(pyMods) == 0 && len(kinds) == 0 {
		if localImport != "" {
			return api.Issue{Category: CatCodeError, Actor: ActorAgent, Evidence: evidence(hits),
				Summary: fmt.Sprintf("A local import cannot be resolved: %s.", localImport),
				Steps:   []string{"Check the import path and file name (case-sensitive), or whether the file needs to be generated or built first.", "Fix it, then restart_process."}}, true
		}
		return api.Issue{}, false
	}
	var steps []string
	switch {
	case len(nodeMods) > 0 || kinds["node"]:
		pm := a.files.nodePM()
		dir := a.files.dirOf("package.json")
		pkgJSON := a.files.read("package.json")
		var cmd string
		if !a.files.has("node_modules") {
			cmd = pm + " install"
			iss.Summary = "Node dependencies are not installed (node_modules is missing)."
		} else {
			var notListed []string
			for _, m := range nodeMods {
				if !strings.Contains(pkgJSON, `"`+m+`"`) {
					notListed = append(notListed, m)
				}
			}
			if len(notListed) > 0 {
				cmd = nodeAdd(pm, strings.Join(notListed, " "))
				iss.Summary = "Package(s) used by the code are not dependencies yet: " + strings.Join(notListed, ", ") + "."
			} else {
				cmd = pm + " install"
				iss.Summary = "Installed node_modules is out of date: " + strings.Join(limitList(nodeMods, 6), ", ") + " missing."
			}
		}
		steps = append(steps, "Ask the user, then run in "+where(a.in.ProjectDir, dir)+": "+cmd)
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Install Node dependencies", Command: cmd, WorkDir: relTo(a.in.ProjectDir, dir), NeedsApproval: true})
	case len(pyMods) > 0:
		var pkgs []string
		for _, m := range pyMods {
			if n, ok := pyPackageNames[m]; ok {
				pkgs = append(pkgs, n)
			} else {
				pkgs = append(pkgs, m)
			}
		}
		listed := true
		manifests := a.files.read("requirements.txt") + a.files.read("pyproject.toml") + a.files.read("Pipfile")
		for _, p := range pkgs {
			if !strings.Contains(strings.ToLower(manifests), strings.ToLower(p)) {
				listed = false
			}
		}
		var cmd, dir string
		switch {
		case a.files.has("uv.lock"):
			dir = a.files.dirOf("uv.lock")
			cmd = "uv sync"
			if !listed {
				cmd = "uv add " + strings.Join(pkgs, " ")
			}
		case a.files.has("poetry.lock"):
			dir = a.files.dirOf("poetry.lock")
			cmd = "poetry install"
			if !listed {
				cmd = "poetry add " + strings.Join(pkgs, " ")
			}
		case a.files.has("Pipfile"):
			dir = a.files.dirOf("Pipfile")
			cmd = "pipenv install"
			if !listed {
				cmd = "pipenv install " + strings.Join(pkgs, " ")
			}
		case a.files.has("requirements.txt") && listed:
			dir = a.files.dirOf("requirements.txt")
			cmd = "pip install -r requirements.txt"
		default:
			dir = a.files.workDir
			cmd = "pip install " + strings.Join(pkgs, " ")
		}
		if listed {
			iss.Summary = "Python package(s) are declared but not installed in the interpreter this process uses: " + strings.Join(pyMods, ", ") + "."
		} else {
			iss.Summary = "Python module(s) are not installed or declared: " + strings.Join(pyMods, ", ") + "."
		}
		steps = append(steps, "Ask the user, then run in "+where(a.in.ProjectDir, dir)+": "+cmd)
		if venv := a.files.find(".venv"); venv != "" && !usesVenv(a.in.Command, a.in.Args) {
			steps = append(steps, "The project has "+relTo(a.in.ProjectDir, venv)+" but this process runs "+a.in.Command+" from PATH. Start it with "+relTo(a.in.ProjectDir, filepath.Join(venv, "bin", "python"))+" (or uv run / poetry run) so it sees the installed packages.")
		}
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Install Python dependencies", Command: cmd, WorkDir: relTo(a.in.ProjectDir, dir), NeedsApproval: true})
	case kinds["go"]:
		cmd := "go mod tidy"
		iss.Summary = "Go module requirements are incomplete (go.mod/go.sum)."
		if len(goPkgs) > 0 {
			iss.Summary = "Go module(s) are missing from go.mod/go.sum: " + strings.Join(limitList(goPkgs, 4), ", ") + "."
		}
		dir := a.files.dirOf("go.mod")
		steps = append(steps, "Ask the user (it edits go.mod/go.sum), then run in "+where(a.in.ProjectDir, dir)+": "+cmd)
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Sync Go modules", Command: cmd, WorkDir: relTo(a.in.ProjectDir, dir), NeedsApproval: true})
	case kinds["ruby"]:
		iss.Summary = "Ruby gems are not installed."
		steps = append(steps, "Ask the user, then run: bundle install")
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Install gems", Command: "bundle install", NeedsApproval: true})
	case kinds["maven"]:
		cmd := "mvn -U dependency:resolve"
		if a.files.has("mvnw") {
			cmd = "./mvnw -U dependency:resolve"
		}
		iss.Summary = "Maven could not resolve dependencies."
		steps = append(steps, "Ask the user, then run: "+cmd+" (check network/proxy and ~/.m2/settings.xml if it keeps failing)")
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Resolve Maven dependencies", Command: cmd, NeedsApproval: true})
	case kinds["gradle"]:
		cmd := "gradle build --refresh-dependencies"
		if a.files.has("gradlew") {
			cmd = "./gradlew build --refresh-dependencies"
		}
		iss.Summary = "Gradle could not resolve dependencies."
		steps = append(steps, "Ask the user, then run: "+cmd)
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Refresh Gradle dependencies", Command: cmd, NeedsApproval: true})
	case kinds["composer"]:
		iss.Summary = "PHP dependencies are not installed (vendor/ is missing)."
		steps = append(steps, "Ask the user, then run: composer install")
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "install", Description: "Install Composer dependencies", Command: "composer install", NeedsApproval: true})
	}
	steps = append(steps, "Run installs with your own shell, not start_process. Then restart_process this app and wait_for_log(ready=true).")
	iss.Steps = steps
	return iss, true
}

func usesVenv(cmd string, args []string) bool {
	all := cmd + " " + strings.Join(args, " ")
	return strings.Contains(all, ".venv") || strings.Contains(all, "venv/") || strings.HasPrefix(filepath.Base(cmd), "uv") || strings.HasPrefix(filepath.Base(cmd), "poetry") || strings.HasPrefix(filepath.Base(cmd), "pipenv")
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

var nodeBins = map[string]bool{"vite": true, "next": true, "tsc": true, "tsx": true, "ts-node": true, "nodemon": true, "jest": true, "vitest": true, "webpack": true, "react-scripts": true, "nuxt": true, "astro": true, "svelte-kit": true, "eslint": true, "prisma": true, "nest": true, "remix": true, "turbo": true, "concurrently": true, "rollup": true, "esbuild": true, "playwright": true, "mocha": true, "parcel": true, "expo": true, "ng": true, "vue-cli-service": true, "gatsby": true, "tailwindcss": true, "drizzle-kit": true, "knex": true}

var toolchainBins = map[string]string{"node": "Node.js", "npm": "Node.js", "npx": "Node.js", "pnpm": "pnpm", "yarn": "Yarn", "bun": "Bun", "deno": "Deno", "python": "Python", "python3": "Python", "pip": "Python", "pip3": "Python", "uv": "uv", "poetry": "Poetry", "go": "Go", "java": "a JDK", "mvn": "Maven", "gradle": "Gradle", "cargo": "Rust", "ruby": "Ruby", "bundle": "Bundler", "php": "PHP", "composer": "Composer", "dotnet": ".NET SDK", "docker": "Docker", "make": "make"}

func (a *analysis) toolIssue(hits []match) api.Issue {
	tool := ""
	for _, h := range hits {
		if g := firstGroup(h); g != "" {
			tool = g
			break
		}
	}
	base := filepath.Base(tool)
	iss := api.Issue{Category: CatMissingTool, Evidence: evidence(hits)}
	pkgJSON := a.files.read("package.json")
	if nodeBins[base] || (pkgJSON != "" && strings.Contains(pkgJSON, `"`+base+`"`)) {
		pm := a.files.nodePM()
		dir := a.files.dirOf("package.json")
		iss.Category = CatMissingDep
		iss.Actor = ActorAgentApproval
		iss.Summary = fmt.Sprintf("%q comes from node_modules/.bin, which is not installed.", base)
		iss.Steps = []string{"Ask the user, then run in " + where(a.in.ProjectDir, dir) + ": " + pm + " install", "Then restart_process."}
		iss.Fixes = []api.SuggestedFix{{Kind: "install", Description: "Install Node dependencies", Command: pm + " install", WorkDir: relTo(a.in.ProjectDir, dir), NeedsApproval: true}}
		return iss
	}
	if strings.HasPrefix(tool, "./") || strings.HasPrefix(tool, "/") {
		iss.Actor = ActorAgent
		iss.Summary = fmt.Sprintf("The executable %s does not exist at that path.", tool)
		iss.Steps = []string{"Check the command path relative to the workdir (" + a.in.WorkDir + ").", "If it is a wrapper script (mvnw, gradlew) make sure it exists and is executable (chmod +x, ask the user first)."}
		return iss
	}
	path := envValue(a.in.Env, "PATH")
	iss.Actor = ActorAgentApproval
	what := toolchainBins[base]
	if what == "" {
		what = base
	}
	iss.Summary = fmt.Sprintf("%q is not on the PATH this process runs with.", base)
	steps := []string{
		fmt.Sprintf("If %s is installed, it is missing from the login-shell PATH jack-ketch captures (version managers like nvm/pyenv/asdf/mise must be initialised in the login shell). Compare: get_process_env(process_id) PATH vs `which %s` in your shell.", what, base),
		fmt.Sprintf("If it is not installed, ask the user to install %s (or approve installing it), then restart_process.", what),
	}
	if a.files.has(".nvmrc") && toolchainBins[base] == "Node.js" {
		steps = append(steps, "This project pins Node in .nvmrc: nvm install && nvm alias default $(cat .nvmrc).")
	}
	if a.files.has(".tool-versions") {
		steps = append(steps, "This project pins tools in .tool-versions: asdf install (or mise install).")
	}
	if a.files.has("mise.toml") || a.files.has(".mise.toml") {
		steps = append(steps, "This project pins tools with mise: mise install.")
	}
	if path != "" {
		steps = append(steps, "PATH for this process: "+truncate(path, 300))
	}
	iss.Steps = steps
	return iss
}

func (a *analysis) toolchainIssue(hits []match) api.Issue {
	iss := api.Issue{Category: CatToolchain, Actor: ActorAgentApproval, Evidence: evidence(hits)}
	h := hits[0]
	switch {
	case strings.Contains(h.line, "The engine") && len(h.groups) >= 3:
		iss.Summary = fmt.Sprintf("%s %s is required but %s is in use.", h.groups[0], h.groups[1], h.groups[2])
	case strings.Contains(h.line, "go.mod requires go") && len(h.groups) >= 2:
		iss.Summary = fmt.Sprintf("go.mod requires Go %s but Go %s is in use.", h.groups[0], h.groups[1])
	case strings.Contains(h.line, "requires a different Python") && len(h.groups) >= 2:
		iss.Summary = fmt.Sprintf("Python %s is in use but the project requires %s.", h.groups[0], h.groups[1])
	case strings.Contains(h.line, "UnsupportedClassVersionError"):
		iss.Summary = "The Java runtime is older than the version the code was compiled for."
	default:
		iss.Summary = "The installed language runtime version does not match what the project requires."
	}
	steps := []string{"Ask the user before installing or switching toolchains."}
	switch {
	case a.files.has(".nvmrc"):
		steps = append(steps, "Node version is pinned in .nvmrc: nvm install && nvm alias default $(cat .nvmrc)")
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "toolchain", Description: "Install the pinned Node version", Command: "nvm install && nvm alias default $(cat .nvmrc)", NeedsApproval: true})
	case a.files.has(".tool-versions"):
		steps = append(steps, "Versions are pinned in .tool-versions: asdf install (or mise install)")
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "toolchain", Description: "Install pinned tool versions", Command: "asdf install", NeedsApproval: true})
	case a.files.has(".python-version"):
		steps = append(steps, "Python is pinned in .python-version: pyenv install -s (or uv python install)")
		iss.Fixes = append(iss.Fixes, api.SuggestedFix{Kind: "toolchain", Description: "Install the pinned Python", Command: "uv python install", NeedsApproval: true})
	}
	steps = append(steps, "jack-ketch processes inherit the login-shell environment, so make the right version the login-shell default (or put its absolute path in the app command), then restart_process.")
	iss.Steps = steps
	return iss
}

func (a *analysis) permissionIssue(hits []match) api.Issue {
	iss := api.Issue{Category: CatPermission, Evidence: evidence(hits)}
	for _, h := range hits {
		g := firstGroup(h)
		if strings.Contains(h.line, "listen EACCES") {
			if p, err := strconv.Atoi(g); err == nil && p < 1024 {
				iss.Actor = ActorAgent
				iss.Summary = fmt.Sprintf("Port %d is privileged (<1024) and needs root.", p)
				iss.Steps = []string{"Run on a port >= 1024 instead, e.g. start with env PORT=3000."}
				iss.Fixes = []api.SuggestedFix{{Kind: "env", Description: "Use an unprivileged port", Command: "PORT=3000", NeedsApproval: false}}
				return iss
			}
		}
		if g != "" && !strings.Contains(h.line, "listen") {
			inside := strings.HasPrefix(g, a.in.ProjectDir) || !filepath.IsAbs(g)
			iss.Summary = fmt.Sprintf("Permission denied on %s.", g)
			if inside {
				iss.Actor = ActorAgentApproval
				iss.Steps = []string{"The file is inside the project: check its owner and mode (ls -l). Files created by a root/docker run are a common cause.", "Ask the user before changing ownership or permissions, then restart_process."}
			} else {
				iss.Actor = ActorUser
				iss.Steps = []string{"The path is outside the project; the user must grant access or point the app at a writable location."}
			}
			return iss
		}
	}
	iss.Actor = ActorUser
	iss.Summary = "The process was denied permission for an operation."
	iss.Steps = []string{"Read the evidence to see which path or resource was denied, then decide with the user how to grant access."}
	return iss
}

func (a *analysis) fileIssue(hits []match) api.Issue {
	path := firstGroup(hits[0])
	iss := api.Issue{Category: CatMissingFile, Evidence: evidence(hits)}
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case strings.HasPrefix(base, ".env"):
		iss.Actor = ActorUser
		iss.Summary = fmt.Sprintf("The env file %s does not exist.", path)
		steps := []string{"The user has to create it with real values."}
		if ex := a.files.envExamples(); len(ex) > 0 {
			steps = append(steps, fmt.Sprintf("Start from the example: cp %s %s (with the user's OK), then the user fills in the values.", ex[0].name, relTo(a.in.ProjectDir, path)))
			iss.Fixes = []api.SuggestedFix{{Kind: "command", Description: "Create the env file from the example", Command: fmt.Sprintf("cp %s %s", ex[0].name, relTo(a.in.ProjectDir, path)), NeedsApproval: true}}
		}
		iss.Steps = steps
	case ext == ".pem" || ext == ".key" || ext == ".crt" || ext == ".p12" || ext == ".jks" || strings.Contains(strings.ToLower(base), "credential"):
		iss.Actor = ActorUser
		iss.Summary = fmt.Sprintf("A key or certificate file is missing: %s.", path)
		iss.Steps = []string{"Only the user can provide keys and certificates. Ask them where it should come from; for local dev a self-signed certificate may be acceptable (ask first)."}
	case strings.Contains(path, "/dist/") || strings.Contains(path, "/build/") || strings.HasPrefix(path, "dist/") || strings.HasPrefix(path, "build/") || strings.Contains(path, "/target/"):
		iss.Actor = ActorAgent
		iss.Summary = fmt.Sprintf("A build output is missing: %s.", path)
		iss.Steps = []string{"Run the project's build first as a one-shot: start_process(command=..., timeout_ms=300000), check exit code 0, then restart_process."}
	default:
		iss.Actor = ActorAgent
		iss.Summary = fmt.Sprintf("A file the app needs does not exist: %s.", path)
		iss.Steps = []string{"Check whether the path is right relative to the workdir (" + a.in.WorkDir + ") or whether a setup step generates it.", "Fix it, then restart_process."}
	}
	return iss
}
