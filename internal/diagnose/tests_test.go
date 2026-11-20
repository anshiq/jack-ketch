package diagnose

import (
	"strings"
	"testing"
)

func linesOf(text string) []ErrLine {
	var out []ErrLine
	for i, l := range strings.Split(strings.TrimPrefix(text, "\n"), "\n") {
		out = append(out, ErrLine{ID: uint64(i), Text: l})
	}
	return out
}

func TestSummarizeTestsFrameworks(t *testing.T) {
	cases := []struct {
		name, text, fw  string
		pass, fail, tot int
		failure         string
	}{
		{"jest", "\n  ● math › adds\nTests:       1 failed, 4 passed, 5 total\n", "jest", 4, 1, 5, "math › adds"},
		{"vitest", "\n Test Files  1 failed | 2 passed (3)\n      Tests  2 failed | 10 passed (12)\n", "vitest", 10, 2, 12, ""},
		{"pytest", "\nFAILED tests/test_a.py::test_x - assert 1 == 2\n======== 1 failed, 7 passed, 2 skipped in 0.41s ========\n", "pytest", 7, 1, 10, "tests/test_a.py::test_x"},
		{"go", "\n=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- FAIL: TestB (0.00s)\nFAIL\nFAIL\texample.com/pkg\t0.01s\n", "go", 1, 1, 2, "TestB"},
		{"cargo", "\ntest result: FAILED. 3 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out\n", "cargo", 3, 1, 4, ""},
		{"dotnet", "\nFailed!  - Failed:     1, Passed:     9, Skipped:     0, Total:    10, Duration: 1 s\n", "dotnet", 9, 1, 10, ""},
		{"rspec", "\n12 examples, 2 failures, 1 pending\n", "rspec", 9, 2, 12, ""},
		{"mocha", "\n  8 passing (20ms)\n  1 pending\n  2 failing\n", "mocha", 8, 2, 11, ""},
	}
	for _, c := range cases {
		s := SummarizeTests(linesOf(c.text))
		if s == nil {
			t.Fatalf("%s: no summary", c.name)
		}
		if s.Framework != c.fw || s.Passed != c.pass || s.Failed != c.fail || s.Total != c.tot {
			t.Fatalf("%s: got %+v", c.name, s)
		}
		if c.failure != "" && (len(s.Failures) == 0 || s.Failures[0] != c.failure) {
			t.Fatalf("%s: failures = %v", c.name, s.Failures)
		}
	}
	if s := SummarizeTests(linesOf("server listening on :8080\nGET / 200")); s != nil {
		t.Fatalf("non-test output summarized: %+v", s)
	}
}
