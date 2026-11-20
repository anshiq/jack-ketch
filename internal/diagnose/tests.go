package diagnose

import (
	"regexp"
	"strconv"
	"strings"

	"jack-ketch/pkg/api"
)

var (
	reCount = regexp.MustCompile(`(\d+)\s+(passed|passing|failed|failing|failures?|errors?|skipped|pending|todo|ignored|xfailed|xpassed|deselected|total)\b`)

	reJestTests    = regexp.MustCompile(`^Tests:\s+(.+)$`)
	reVitestTests  = regexp.MustCompile(`^\s*Tests\s+(.+?)(?:\s+\((\d+)\))?\s*$`)
	rePytestSum    = regexp.MustCompile(`^=+ (.*\b(?:passed|failed|error|errors|skipped|xfailed|deselected)\b.*) in [\d.]+s(?: \(.*\))? =+$`)
	reCargoSum     = regexp.MustCompile(`^test result: (?:ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored`)
	reMochaPassing = regexp.MustCompile(`^\s+(\d+) passing\b`)
	reMochaFailing = regexp.MustCompile(`^\s+(\d+) failing\b`)
	reMochaPending = regexp.MustCompile(`^\s+(\d+) pending\b`)
	reDotnetSum    = regexp.MustCompile(`^\s*(?:Passed|Failed)!\s+-\s+Failed:\s+(\d+),\s+Passed:\s+(\d+),\s+Skipped:\s+(\d+),\s+Total:\s+(\d+)`)
	reRSpecSum     = regexp.MustCompile(`^(\d+) examples?, (\d+) failures?(?:, (\d+) pending)?`)
	rePHPUnitSum   = regexp.MustCompile(`^(?:OK \((\d+) tests?|Tests: (\d+), Assertions: \d+(?:, (?:Failures|Errors): (\d+))?(?:, (?:Failures|Errors): (\d+))?(?:, Skipped: (\d+))?)`)
	reGoPass       = regexp.MustCompile(`^\s*--- PASS: (\S+)`)
	reGoFail       = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	reGoSkip       = regexp.MustCompile(`^\s*--- SKIP: (\S+)`)
	reGoPkgFail    = regexp.MustCompile(`^FAIL\s+(\S+)\s`)
	reGoPkgOK      = regexp.MustCompile(`^ok\s+\S+\s`)
	reJestFailName = regexp.MustCompile(`^\s*● (.+)$`)
	reXUnitFail    = regexp.MustCompile(`^\s*Failed (\S+) \[`)
)

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func fillCounts(sum *api.TestSummary, text string) {
	for _, m := range reCount.FindAllStringSubmatch(text, -1) {
		n := atoi(m[1])
		switch m[2] {
		case "passed", "passing", "xpassed":
			sum.Passed += n
		case "failed", "failing", "failure", "failures", "error", "errors":
			sum.Failed += n
		case "skipped", "pending", "todo", "ignored", "xfailed", "deselected":
			sum.Skipped += n
		case "total":
			sum.Total = n
		}
	}
}

func SummarizeTests(lines []ErrLine) *api.TestSummary {
	var sum *api.TestSummary
	set := func(fw string) *api.TestSummary {
		s := &api.TestSummary{Framework: fw}
		sum = s
		return s
	}
	var goPass, goFail, goSkip int
	var goFails []string
	goSeen := false
	var failures []string
	for _, l := range lines {
		t := strings.TrimRight(StripANSI(l.Text), "\r")
		switch {
		case reJestTests.MatchString(t):
			s := set("jest")
			fillCounts(s, reJestTests.FindStringSubmatch(t)[1])
		case strings.HasPrefix(strings.TrimSpace(t), "Tests ") && reVitestTests.MatchString(t) && strings.ContainsAny(t, "|(") && reCount.MatchString(t):
			m := reVitestTests.FindStringSubmatch(t)
			s := set("vitest")
			fillCounts(s, m[1])
			if m[2] != "" {
				s.Total = atoi(m[2])
			}
		case rePytestSum.MatchString(t):
			s := set("pytest")
			fillCounts(s, rePytestSum.FindStringSubmatch(t)[1])
		case reCargoSum.MatchString(t):
			m := reCargoSum.FindStringSubmatch(t)
			if sum == nil || sum.Framework != "cargo" {
				set("cargo")
			}
			sum.Passed += atoi(m[1])
			sum.Failed += atoi(m[2])
			sum.Skipped += atoi(m[3])
		case reDotnetSum.MatchString(t):
			m := reDotnetSum.FindStringSubmatch(t)
			if sum == nil || sum.Framework != "dotnet" {
				set("dotnet")
			}
			sum.Failed += atoi(m[1])
			sum.Passed += atoi(m[2])
			sum.Skipped += atoi(m[3])
			sum.Total += atoi(m[4])
		case reRSpecSum.MatchString(t):
			m := reRSpecSum.FindStringSubmatch(t)
			s := set("rspec")
			s.Total = atoi(m[1])
			s.Failed = atoi(m[2])
			s.Skipped = atoi(m[3])
			s.Passed = s.Total - s.Failed - s.Skipped
		case rePHPUnitSum.MatchString(t):
			m := rePHPUnitSum.FindStringSubmatch(t)
			s := set("phpunit")
			if m[1] != "" {
				s.Total = atoi(m[1])
				s.Passed = s.Total
			} else {
				s.Total = atoi(m[2])
				s.Failed = atoi(m[3]) + atoi(m[4])
				s.Skipped = atoi(m[5])
				s.Passed = s.Total - s.Failed - s.Skipped
			}
		case reMochaPassing.MatchString(t):
			if sum == nil || sum.Framework != "mocha" {
				set("mocha")
			}
			sum.Passed = atoi(reMochaPassing.FindStringSubmatch(t)[1])
		case reMochaFailing.MatchString(t) && sum != nil && sum.Framework == "mocha":
			sum.Failed = atoi(reMochaFailing.FindStringSubmatch(t)[1])
		case reMochaPending.MatchString(t) && sum != nil && sum.Framework == "mocha":
			sum.Skipped = atoi(reMochaPending.FindStringSubmatch(t)[1])
		case reGoPass.MatchString(t):
			goSeen = true
			goPass++
		case reGoFail.MatchString(t):
			goSeen = true
			goFail++
			goFails = append(goFails, reGoFail.FindStringSubmatch(t)[1])
		case reGoSkip.MatchString(t):
			goSeen = true
			goSkip++
		case reGoPkgFail.MatchString(t), reGoPkgOK.MatchString(t):
			goSeen = true
		}
		if m := reJestFailName.FindStringSubmatch(t); m != nil && !strings.Contains(m[1], "Console") {
			failures = append(failures, strings.TrimSpace(m[1]))
		}
		if m := rePytestFail.FindStringSubmatch(t); m != nil {
			failures = append(failures, m[1]+"::"+m[2])
		}
		if m := reXUnitFail.FindStringSubmatch(t); m != nil {
			failures = append(failures, m[1])
		}
	}
	if sum == nil && goSeen {
		sum = &api.TestSummary{Framework: "go", Passed: goPass, Failed: goFail, Skipped: goSkip}
		failures = append(failures, goFails...)
	}
	if sum == nil {
		return nil
	}
	if sum.Total == 0 {
		sum.Total = sum.Passed + sum.Failed + sum.Skipped
	}
	seen := map[string]bool{}
	for _, f := range failures {
		if seen[f] || len(sum.Failures) >= 25 {
			continue
		}
		seen[f] = true
		sum.Failures = append(sum.Failures, f)
	}
	return sum
}
