package diagnose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jack-ketch/pkg/api"
)

func scanText(t *testing.T, root, text string) []api.ErrorGroup {
	t.Helper()
	var lines []ErrLine
	for i, l := range strings.Split(strings.TrimPrefix(text, "\n"), "\n") {
		lines = append(lines, ErrLine{ID: uint64(i), Stream: "stderr", Text: l})
	}
	return ExtractErrors(ErrorScan{Lines: lines, Root: root, WorkDir: root})
}

func touch(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExtractTypeScriptCompileErrors(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "src/app.ts")
	g := scanText(t, root, `
src/app.ts(12,5): error TS2322: Type 'string' is not assignable to type 'number'.
src/app.ts(12,5): error TS2322: Type 'string' is not assignable to type 'number'.
src/app.ts(30,1): error TS2304: Cannot find name 'foo'.
Found 3 errors.`)
	if len(g) != 2 {
		t.Fatalf("groups = %d: %+v", len(g), g)
	}
	if g[0].Kind != KindCompile || g[0].Count != 2 || g[0].Locations[0].File != "src/app.ts" || g[0].Locations[0].Line != 12 || g[0].Locations[0].Column != 5 || !g[0].Locations[0].InProject {
		t.Fatalf("first group = %+v", g[0])
	}
	if !strings.HasPrefix(g[0].Message, "TS2322:") {
		t.Fatalf("message = %q", g[0].Message)
	}
}

func TestExtractGoBuildAndPanic(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "internal/api/handler.go", "main.go")
	g := scanText(t, root, `
# example.com/app/internal/api
internal/api/handler.go:41:9: undefined: userStore
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x4a2b1c]

goroutine 1 [running]:
main.run(0x0)
	`+root+`/main.go:22 +0x1c
main.main()
	`+root+`/main.go:10 +0x25
exit status 2`)
	if len(g) != 2 {
		t.Fatalf("groups = %+v", g)
	}
	if g[0].Kind != KindCompile || g[0].Message != "undefined: userStore" || g[0].Locations[0].File != "internal/api/handler.go" {
		t.Fatalf("compile = %+v", g[0])
	}
	if g[1].Kind != KindPanic || len(g[1].Locations) == 0 || g[1].Locations[0].File != "main.go" || g[1].Locations[0].Line != 22 {
		t.Fatalf("panic = %+v", g[1])
	}
}

func TestExtractPythonTraceback(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "app/views.py")
	g := scanText(t, root, `
Traceback (most recent call last):
  File "/usr/lib/python3.12/site-packages/flask/app.py", line 880, in full_dispatch_request
    rv = self.dispatch_request()
  File "`+root+`/app/views.py", line 14, in index
    return items[3]
IndexError: list index out of range`)
	if len(g) != 1 {
		t.Fatalf("groups = %+v", g)
	}
	if g[0].Kind != KindException || g[0].Message != "IndexError: list index out of range" {
		t.Fatalf("group = %+v", g[0])
	}
	if l := g[0].Locations[0]; l.File != "app/views.py" || l.Line != 14 || !l.InProject {
		t.Fatalf("location = %+v", l)
	}
}

func TestExtractNodeExceptionGroupsRepeats(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "src/routes/users.js")
	trace := `TypeError: Cannot read properties of undefined (reading 'id')
    at getUser (` + root + `/src/routes/users.js:18:24)
    at Layer.handle [as handle_request] (` + root + `/node_modules/express/lib/router/layer.js:95:5)
    at next (` + root + `/node_modules/express/lib/router/route.js:149:13)`
	g := scanText(t, root, trace+"\nGET /users/1 500 3ms\n"+strings.Replace(trace, "'id'", "'id'", 1))
	if len(g) != 1 || g[0].Count != 2 {
		t.Fatalf("groups = %+v", g)
	}
	if l := g[0].Locations[0]; l.File != "src/routes/users.js" || l.Line != 18 || !l.InProject {
		t.Fatalf("location = %+v", l)
	}
	if g[0].Locations[len(g[0].Locations)-1].InProject {
		t.Fatalf("dependency frame marked in project: %+v", g[0].Locations)
	}
}

func TestExtractTestFailures(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "tests/test_math.py", "pkg/calc_test.go")
	g := scanText(t, root, `
FAILED tests/test_math.py::test_add - assert 3 == 4
--- FAIL: TestDivide (0.00s)
    calc_test.go:17: got 2, want 3`)
	kinds := 0
	for _, x := range g {
		if x.Kind == KindTest {
			kinds++
		}
	}
	if kinds != 2 {
		t.Fatalf("groups = %+v", g)
	}
	if g[0].Locations[0].File != "tests/test_math.py" || !g[0].Locations[0].InProject {
		t.Fatalf("pytest location = %+v", g[0].Locations)
	}
}

func TestExtractESLintAndLogErrors(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "src/a.ts")
	g := scanText(t, root, `
`+root+`/src/a.ts
  3:7  error  'x' is assigned a value but never used  @typescript-eslint/no-unused-vars
  9:1  error  Unexpected console statement            no-console
INFO server listening on :8080
level=error msg="db connect failed" err="dial tcp 127.0.0.1:5432: connect: connection refused"
compiled successfully with 0 errors`)
	var lint, logErr int
	for _, x := range g {
		switch x.Kind {
		case KindLint:
			lint++
			if x.Locations[0].File != "src/a.ts" {
				t.Fatalf("lint location = %+v", x.Locations)
			}
		case KindLog:
			logErr++
		}
	}
	if lint != 2 || logErr != 1 {
		t.Fatalf("groups = %+v", g)
	}
}

func TestExtractBaselineMarksNew(t *testing.T) {
	root := t.TempDir()
	old := []ErrLine{{ID: 1, Text: "ERROR: cache miss storm"}}
	base := Fingerprints(ErrorScan{Lines: old, Root: root})
	cur := []ErrLine{{ID: 5, Text: "ERROR: cache miss storm"}, {ID: 6, Text: "ERROR: payment declined"}}
	g := ExtractErrors(ErrorScan{Lines: cur, Root: root, Baseline: base})
	if len(g) != 2 || g[0].New || !g[1].New {
		t.Fatalf("groups = %+v", g)
	}
}

func TestExtractIgnoresCleanOutput(t *testing.T) {
	g := scanText(t, t.TempDir(), `
  VITE v5.4.0  ready in 312 ms
  ➜  Local:   http://localhost:5173/
info: 0 errors, 0 warnings
GET /health 200 1ms`)
	if len(g) != 0 {
		t.Fatalf("expected no errors, got %+v", g)
	}
}

func TestExtractBrowserErrors(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "src/components/List.tsx")
	g := scanText(t, root, `
[browser] error: TypeError: Cannot read properties of undefined (reading 'map') (at src/components/List.tsx:14:22) page=/items
    at List (src/components/List.tsx:14:22)
    at renderWithHooks (node_modules/.vite/deps/react-dom.js:11548:26)
[browser] error: GET /api/items -> 500 Internal Server Error page=/items`)
	if len(g) != 2 || g[0].Kind != KindBrowser || g[1].Kind != KindBrowser {
		t.Fatalf("groups = %+v", g)
	}
	if l := g[0].Locations[0]; l.File != "src/components/List.tsx" || l.Line != 14 || !l.InProject {
		t.Fatalf("location = %+v", l)
	}
	if !strings.HasPrefix(g[0].Message, "TypeError: Cannot read") {
		t.Fatalf("message = %q", g[0].Message)
	}
}
