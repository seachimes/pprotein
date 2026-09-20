package diag

import (
	"strings"
	"testing"
)

func findingsOf(r Report, cat Category) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Category == cat {
			out = append(out, f)
		}
	}
	return out
}

func hasCategory(r Report, cat Category) bool {
	return len(findingsOf(r, cat)) > 0
}

// A full table scan with a huge examined/sent ratio is the canonical INDEX case.
func TestAnalyzeDetectsMissingIndex(t *testing.T) {
	in := Input{
		GroupID:    "g1",
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP: []HTTPStat{
			{Count: 1000, Status2xx: 1000, Method: "GET", URI: "/api/courses", Sum: 20, Avg: 0.02},
		},
		Query: []QueryStat{
			{
				Count:           1000,
				Query:           "SELECT * FROM `courses` WHERE `teacher_id` = N ORDER BY `created_at`",
				SumQueryTime:    12,
				AvgQueryTime:    0.012,
				SumRowsSent:     1000,
				SumRowsExamined: 980000,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())

	idx := findingsOf(rep, CatIndex)
	if len(idx) != 1 {
		t.Fatalf("expected 1 INDEX finding, got %d", len(idx))
	}

	f := idx[0]
	if f.Effort != EffortTrivial {
		t.Errorf("Effort = %v, want trivial", f.Effort)
	}
	// Impact is measured against total app time (20s), not query time.
	if f.ImpactShare < 0.5 || f.ImpactShare > 0.65 {
		t.Errorf("ImpactShare = %v, want ~0.6", f.ImpactShare)
	}
	// The suggestion must be a runnable statement naming the real table and
	// putting the equality column before the ORDER BY column.
	want := "ALTER TABLE `courses` ADD INDEX idx_teacher_id_created_at (`teacher_id`, `created_at`);"
	if f.Suggestion != want {
		t.Errorf("Suggestion = %q, want %q", f.Suggestion, want)
	}
	if len(f.Evidence) == 0 {
		t.Error("finding carries no evidence")
	}
}

// Writes report Rows_sent = 0. Dividing examined rows by that would make every
// targeted UPDATE look like a full table scan, which was a real false positive
// during development.
func TestAnalyzeDoesNotFlagTargetedUpdateAsUnindexed(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 1000, Method: "POST", URI: "/a", Sum: 100, Avg: 0.1}},
		Query: []QueryStat{
			{
				Count: 1000, Query: "UPDATE `b` SET `y` = N WHERE `id` = N",
				SumQueryTime: 60, AvgQueryTime: 0.06,
				SumRowsSent:     0,
				SumRowsExamined: 1000, // one row per execution: properly targeted
			},
		},
	}

	if hasCategory(Analyze(in, DefaultThresholds()), CatIndex) {
		t.Error("an UPDATE touching one row per execution was flagged as missing an index")
	}
}

// A write that really does scan must still be caught.
func TestAnalyzeFlagsScanningUpdate(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 1000, Method: "POST", URI: "/a", Sum: 100, Avg: 0.1}},
		Query: []QueryStat{
			{
				Count: 100, Query: "UPDATE `b` SET `y` = N WHERE `unindexed` = N",
				SumQueryTime: 60, AvgQueryTime: 0.6,
				SumRowsSent:     0,
				SumRowsExamined: 5000000, // 50k rows examined per execution
			},
		},
	}

	if !hasCategory(Analyze(in, DefaultThresholds()), CatIndex) {
		t.Error("a scanning UPDATE was not flagged")
	}
}

// A query that examines only what it returns must not be flagged, no matter how
// much total time it consumes.
func TestAnalyzeIgnoresWellIndexedQuery(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 100, Method: "GET", URI: "/x", Sum: 50, Avg: 0.5}},
		Query: []QueryStat{
			{
				Count:           100,
				Query:           "SELECT * FROM `t` WHERE `id` = N",
				SumQueryTime:    30,
				AvgQueryTime:    0.3,
				SumRowsSent:     100,
				SumRowsExamined: 100,
			},
		},
	}

	if hasCategory(Analyze(in, DefaultThresholds()), CatIndex) {
		t.Error("flagged a query whose examined/sent ratio is 1")
	}
}

// N+1 is the join of slp execution counts against the alp request count.
func TestAnalyzeDetectsNPlusOne(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP: []HTTPStat{
			{Count: 1000, Status2xx: 1000, Method: "GET", URI: "/api/courses", Sum: 30, Avg: 0.03},
		},
		Query: []QueryStat{
			{
				Count:           18000,
				Query:           "SELECT `name` FROM `users` WHERE `id` = N",
				SumQueryTime:    9,
				AvgQueryTime:    0.0005,
				SumRowsSent:     18000,
				SumRowsExamined: 18000,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	n := findingsOf(rep, CatNPlusOne)
	if len(n) != 1 {
		t.Fatalf("expected 1 N+1 finding, got %d", len(n))
	}

	var perReq string
	for _, e := range n[0].Evidence {
		if e.Label == "1リクエストあたり" {
			perReq = e.Value
		}
	}
	if perReq != "18.0回" {
		t.Errorf("per-request evidence = %q, want 18.0回", perReq)
	}

	// The same query is well-indexed, so it must not also be an INDEX finding.
	if hasCategory(rep, CatIndex) {
		t.Error("a fast indexed query was also reported as missing an index")
	}
}

// Without the HTTP log there is no request count, so N+1 cannot be distinguished
// from a legitimately popular query and must stay silent.
func TestNPlusOneRequiresHTTPLog(t *testing.T) {
	in := Input{
		HasSlowLog: true,
		Query: []QueryStat{
			{
				Count:           18000,
				Query:           "SELECT `name` FROM `users` WHERE `id` = N",
				SumQueryTime:    9,
				AvgQueryTime:    0.0005,
				SumRowsSent:     18000,
				SumRowsExamined: 18000,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	if hasCategory(rep, CatNPlusOne) {
		t.Error("reported N+1 without a request count to divide by")
	}
	// ...and the gap must be surfaced as a health issue instead.
	if !hasHealth(rep, "httplog") {
		t.Error("missing http log was not reported as a health issue")
	}
}

// A slow query that is slow because of lock contention must be reported as LOCK,
// not INDEX: adding an index would not help.
func TestAnalyzeDetectsLockContention(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 500, Method: "POST", URI: "/api/order", Sum: 40, Avg: 0.08}},
		Query: []QueryStat{
			{
				Count:           500,
				Query:           "UPDATE `stock` SET `n` = N WHERE `id` = N",
				SumQueryTime:    20,
				AvgQueryTime:    0.04,
				SumLockTime:     16,
				SumRowsSent:     0,
				SumRowsExamined: 500,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	if !hasCategory(rep, CatLock) {
		t.Fatal("expected a LOCK finding")
	}
	if got := findingsOf(rep, CatLock)[0].Effort; got == EffortTrivial {
		t.Error("lock contention should not be presented as a trivial fix")
	}
}

// Read-heavy tables with no writes are cache candidates.
func TestAnalyzeDetectsCacheCandidate(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 2000, Method: "GET", URI: "/api/x", Sum: 60, Avg: 0.03}},
		Query: []QueryStat{
			{
				Count: 5000, Query: "SELECT * FROM `categories` WHERE `id` = N",
				SumQueryTime: 15, AvgQueryTime: 0.003,
				SumRowsSent: 5000, SumRowsExamined: 5000,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	cache := findingsOf(rep, CatCache)
	if len(cache) != 1 {
		t.Fatalf("expected 1 CACHE finding, got %d", len(cache))
	}
	if cache[0].Subject != "categories" {
		t.Errorf("Subject = %q, want categories", cache[0].Subject)
	}
}

// A table whose cost comes from a missing index must be reported as INDEX only.
// Recommending a cache alongside it sends the user down a much more expensive
// path than the one-line fix that actually resolves the problem.
func TestAnalyzeSuppressesCacheWhenIndexIsTheRealFix(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 520, Method: "GET", URI: "/api/courses", Sum: 78, Avg: 0.15}},
		Query: []QueryStat{
			{
				Count: 300, Query: "SELECT * FROM `courses` WHERE `teacher_id` = N ORDER BY `created_at`",
				SumQueryTime: 12, AvgQueryTime: 0.04,
				SumRowsSent: 1500, SumRowsExamined: 29400000,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	if !hasCategory(rep, CatIndex) {
		t.Fatal("expected the INDEX finding")
	}
	for _, f := range findingsOf(rep, CatCache) {
		if f.Subject == "courses" {
			t.Error("recommended caching a table whose real problem is a missing index")
		}
	}
}

// A table that is written to frequently is not a safe cache target.
func TestAnalyzeSkipsCacheForWriteHeavyTable(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 2000, Method: "GET", URI: "/api/x", Sum: 60, Avg: 0.03}},
		Query: []QueryStat{
			{
				Count: 5000, Query: "SELECT * FROM `posts` WHERE `id` = N",
				SumQueryTime: 15, AvgQueryTime: 0.003,
				SumRowsSent: 5000, SumRowsExamined: 5000,
			},
			{
				Count: 4000, Query: "INSERT INTO `posts` (`body`) VALUES (N)",
				SumQueryTime: 4, AvgQueryTime: 0.001,
				SumRowsSent: 0, SumRowsExamined: 0,
			},
		},
	}

	if hasCategory(Analyze(in, DefaultThresholds()), CatCache) {
		t.Error("recommended caching a table with a 1.25:1 read/write ratio")
	}
}

func TestAnalyzeDetectsStaticAssets(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HTTP: []HTTPStat{
			{Count: 5000, Status2xx: 5000, Method: "GET", URI: "/static/app.js", Sum: 25, Avg: 0.005},
			{Count: 100, Status2xx: 100, Method: "GET", URI: "/api/users", Sum: 10, Avg: 0.1},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	st := findingsOf(rep, CatStatic)
	if len(st) != 1 {
		t.Fatalf("expected 1 STATIC finding, got %d", len(st))
	}
	if st[0].Subject != "GET /static/app.js" {
		t.Errorf("Subject = %q", st[0].Subject)
	}
}

func TestAnalyzeDetectsErrors(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HTTP: []HTTPStat{
			{Count: 1000, Status2xx: 900, Status5xx: 100, Method: "POST", URI: "/api/pay", Sum: 5, Avg: 0.005},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	if !hasCategory(rep, CatError) {
		t.Fatal("expected an ERROR finding")
	}
}

// 4xx alone is frequently the benchmark testing auth on purpose, so it should be
// reported with visibly lower confidence than a 5xx.
func TestErrorConfidenceLowerFor4xxOnly(t *testing.T) {
	only4xx := Analyze(Input{
		HasHTTPLog: true,
		HTTP: []HTTPStat{
			{Count: 1000, Status2xx: 900, Status4xx: 100, Method: "GET", URI: "/a", Sum: 5, Avg: 0.005},
		},
	}, DefaultThresholds())

	with5xx := Analyze(Input{
		HasHTTPLog: true,
		HTTP: []HTTPStat{
			{Count: 1000, Status2xx: 900, Status5xx: 100, Method: "GET", URI: "/a", Sum: 5, Avg: 0.005},
		},
	}, DefaultThresholds())

	a := findingsOf(only4xx, CatError)
	b := findingsOf(with5xx, CatError)
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("expected ERROR findings in both cases")
	}
	if !(a[0].Confidence < b[0].Confidence) {
		t.Errorf("4xx confidence %v should be below 5xx confidence %v", a[0].Confidence, b[0].Confidence)
	}
}

func TestAnalyzeDetectsCPUHotspot(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasPprof:   true,
		HTTP:       []HTTPStat{{Count: 100, Method: "POST", URI: "/login", Sum: 50, Avg: 0.5}},
		Funcs: []FuncStat{
			{Function: "golang.org/x/crypto/bcrypt.bcrypt", Flat: 700, FlatPct: 0.7},
			{Function: "runtime.mallocgc", Flat: 200, FlatPct: 0.2},
		},
	}

	rep := Analyze(in, DefaultThresholds())
	cpu := findingsOf(rep, CatAppCPU)
	if len(cpu) != 1 {
		t.Fatalf("expected 1 APP-CPU finding, got %d", len(cpu))
	}
	if !strings.Contains(cpu[0].Subject, "bcrypt") {
		t.Errorf("Subject = %q, want the bcrypt frame", cpu[0].Subject)
	}
	// runtime.mallocgc is real cost but not directly actionable.
	for _, f := range cpu {
		if strings.HasPrefix(f.Subject, "runtime.") {
			t.Errorf("reported unactionable runtime frame %q", f.Subject)
		}
	}
}

// Effort must genuinely participate in the ranking, not merely decorate it.
//
// The scenario is chosen so that impact x confidence alone favours the
// expensive fix: only by dividing through effort does the cheap fix win. A
// scoring function that ignored effort would rank these the other way round.
func TestPriorityPrefersCheapFixOverHigherImpactExpensiveFix(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 1000, Status2xx: 1000, Method: "GET", URI: "/a", Sum: 100, Avg: 0.1}},
		Query: []QueryStat{
			// Missing index: 30% of time, trivial to fix.
			{
				Count: 1000, Query: "SELECT * FROM `a` WHERE `x` = N",
				SumQueryTime: 30, AvgQueryTime: 0.03,
				SumRowsSent: 1000, SumRowsExamined: 500000,
			},
			// Lock contention: 55% of time, but a structural fix.
			{
				Count: 1000, Query: "UPDATE `b` SET `y` = N WHERE `z` = N",
				SumQueryTime: 60, AvgQueryTime: 0.06, SumLockTime: 55,
				SumRowsSent: 0, SumRowsExamined: 1000,
			},
		},
	}

	rep := Analyze(in, DefaultThresholds())

	idx := findingsOf(rep, CatIndex)
	lock := findingsOf(rep, CatLock)
	if len(idx) == 0 || len(lock) == 0 {
		t.Fatalf("expected both findings; got %d index, %d lock", len(idx), len(lock))
	}

	// Precondition: without the effort term the lock finding would rank first.
	if idx[0].ImpactShare*float64(idx[0].Confidence) >= lock[0].ImpactShare*float64(lock[0].Confidence) {
		t.Fatalf("precondition failed: this scenario no longer isolates the effort term")
	}

	if rep.Findings[0].Category != CatIndex {
		t.Errorf("top finding is %v; a trivial fix worth 30%% should outrank a structural fix worth 55%%",
			rep.Findings[0].Category)
	}
}

// score() must be monotonic in each of its three inputs.
func TestScoreRespectsEachTerm(t *testing.T) {
	base := score(0.3, 0.8, EffortMedium)

	if score(0.6, 0.8, EffortMedium) <= base {
		t.Error("score must increase with impact")
	}
	if score(0.3, 0.9, EffortMedium) <= base {
		t.Error("score must increase with confidence")
	}
	if score(0.3, 0.8, EffortHuge) >= base {
		t.Error("score must decrease as effort grows")
	}
	if score(0.3, 0.8, EffortTrivial) <= base {
		t.Error("score must increase as effort shrinks")
	}
}

// Findings below the noise floor would pad the list and dilute attention.
func TestAnalyzeDropsNegligibleFindings(t *testing.T) {
	in := Input{
		HasHTTPLog: true,
		HasSlowLog: true,
		HTTP:       []HTTPStat{{Count: 1000, Method: "GET", URI: "/a", Sum: 1000, Avg: 1}},
		Query: []QueryStat{
			{
				Count: 1, Query: "SELECT * FROM `tiny` WHERE `x` = N",
				SumQueryTime: 0.001, AvgQueryTime: 0.001,
				SumRowsSent: 1, SumRowsExamined: 100000,
			},
		},
	}

	if hasCategory(Analyze(in, DefaultThresholds()), CatIndex) {
		t.Error("reported a finding worth 0.0001% of total time")
	}
}

func TestAnalyzeEmptyInput(t *testing.T) {
	rep := Analyze(Input{GroupID: "empty"}, DefaultThresholds())
	if len(rep.Findings) != 0 {
		t.Errorf("expected no findings, got %d", len(rep.Findings))
	}
	if len(rep.Health) == 0 {
		t.Error("expected health issues describing the missing data")
	}
}
