package diag

import (
	"strings"
	"testing"
)

func hasHealth(r Report, source string) bool {
	for _, h := range r.Health {
		if strings.Contains(h.Source, source) {
			return true
		}
	}
	return false
}

func healthWithSeverity(issues []HealthIssue, sev Severity) []HealthIssue {
	var out []HealthIssue
	for _, h := range issues {
		if h.Severity == sev {
			out = append(out, h)
		}
	}
	return out
}

// The classic mistake: slow_query_log was never enabled.
func TestHealthFlagsMissingSlowLog(t *testing.T) {
	issues := CheckHealth(Input{HasHTTPLog: true, HTTP: []HTTPStat{
		{Count: 100, Method: "GET", URI: "/a", Sum: 1},
	}})

	found := false
	for _, h := range issues {
		if h.Source == "slowlog" && strings.Contains(h.Title, "収集されていない") {
			found = true
		}
	}
	if !found {
		t.Error("missing slow log was not reported")
	}
}

// Collected but empty is a different and more alarming condition than absent.
func TestHealthDistinguishesEmptyFromAbsent(t *testing.T) {
	absent := CheckHealth(Input{HasHTTPLog: true, HTTP: []HTTPStat{{Count: 50, URI: "/a", Method: "GET", Sum: 1}}})
	empty := CheckHealth(Input{
		HasHTTPLog: true, HTTP: []HTTPStat{{Count: 50, URI: "/a", Method: "GET", Sum: 1}},
		HasSlowLog: true, Query: nil,
	})

	if len(healthWithSeverity(empty, SevError)) == 0 {
		t.Error("an empty-but-collected slow log should be an error")
	}
	if len(healthWithSeverity(absent, SevError)) != 0 {
		t.Error("an absent slow log should be a warning, not an error")
	}
}

// long_query_time left at its default hides exactly the N+1 patterns we most
// want to find.
func TestHealthFlagsHighLongQueryTime(t *testing.T) {
	in := Input{
		HasSlowLog: true,
		Query: []QueryStat{
			{Count: 100, Query: "SELECT 1", SumQueryTime: 60, AvgQueryTime: 0.6, SumRowsSent: 100, SumRowsExamined: 100},
			{Count: 100, Query: "SELECT 2", SumQueryTime: 30, AvgQueryTime: 0.3, SumRowsSent: 100, SumRowsExamined: 100},
		},
	}

	found := false
	for _, h := range CheckHealth(in) {
		if strings.Contains(h.Title, "long_query_time") {
			found = true
		}
	}
	if !found {
		t.Error("did not warn that only slow queries were captured")
	}
}

// Unnormalized paths scatter one endpoint across thousands of rows and hide the
// real hotspot.
func TestHealthFlagsUnnormalizedPaths(t *testing.T) {
	var stats []HTTPStat
	for i := 0; i < 250; i++ {
		stats = append(stats, HTTPStat{
			Count: 4, Method: "GET", URI: "/api/users/" + itoa(i), Sum: 0.4, Avg: 0.1,
		})
	}

	issues := CheckHealth(Input{HasHTTPLog: true, HTTP: stats})

	found := false
	for _, h := range issues {
		if strings.Contains(h.Title, "分散") {
			found = true
			if h.Severity != SevError {
				t.Errorf("severity = %v, want error", h.Severity)
			}
		}
	}
	if !found {
		t.Error("did not detect unnormalized endpoint paths")
	}
}

// A moderate cluster of numeric-tail paths should still warn.
func TestHealthWarnsOnNumericTailPaths(t *testing.T) {
	var stats []HTTPStat
	for i := 0; i < 15; i++ {
		stats = append(stats, HTTPStat{
			Count: 10, Method: "GET", URI: "/api/item/" + itoa(i), Sum: 1, Avg: 0.1,
		})
	}

	found := false
	for _, h := range CheckHealth(Input{HasHTTPLog: true, HTTP: stats}) {
		if strings.Contains(h.Title, "正規化されていない") {
			found = true
		}
	}
	if !found {
		t.Error("did not warn about numeric-tail paths")
	}
}

// Query time far exceeding request time means the two logs cover different
// windows — usually a log that was not truncated before the run.
func TestHealthDetectsMismatchedLogWindows(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 100, Method: "GET", URI: "/a", Sum: 10, Avg: 0.1}},
		Query: []QueryStat{
			{Count: 50000, Query: "SELECT 1", SumQueryTime: 500, AvgQueryTime: 0.01, SumRowsSent: 1, SumRowsExamined: 1},
		},
	}

	found := false
	for _, h := range CheckHealth(in) {
		if strings.Contains(h.Title, "計測期間") {
			found = true
			if h.Severity != SevError {
				t.Errorf("severity = %v, want error", h.Severity)
			}
		}
	}
	if !found {
		t.Error("did not detect mismatched measurement windows")
	}
}

// A healthy measurement should not produce error-level noise.
func TestHealthQuietOnGoodMeasurement(t *testing.T) {
	in := Input{
		HasHTTPLog: true, HasSlowLog: true, HasPprof: true,
		HTTP: []HTTPStat{
			{Count: 5000, Status2xx: 5000, Method: "GET", URI: "/api/a", Sum: 100, Avg: 0.02},
			{Count: 3000, Status2xx: 3000, Method: "POST", URI: "/api/b", Sum: 80, Avg: 0.027},
		},
		Query: []QueryStat{
			{Count: 9000, Query: "SELECT * FROM `t` WHERE `id` = N", SumQueryTime: 40, AvgQueryTime: 0.004, SumRowsSent: 9000, SumRowsExamined: 9000},
		},
	}

	if errs := healthWithSeverity(CheckHealth(in), SevError); len(errs) != 0 {
		t.Errorf("clean measurement produced %d error(s): %+v", len(errs), errs)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
