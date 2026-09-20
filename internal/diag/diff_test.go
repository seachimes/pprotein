package diag

import "testing"

func httpDiffFor(diffs []HTTPDiff, endpoint string) *HTTPDiff {
	for i := range diffs {
		if diffs[i].Endpoint == endpoint {
			return &diffs[i]
		}
	}
	return nil
}

func TestCompareHTTPDetectsImprovement(t *testing.T) {
	before := []HTTPStat{{Count: 100, Method: "GET", URI: "/a", Sum: 50, Avg: 0.5}}
	after := []HTTPStat{{Count: 100, Method: "GET", URI: "/a", Sum: 10, Avg: 0.1}}

	d := httpDiffFor(CompareHTTP(before, after), "GET /a")
	if d == nil {
		t.Fatal("endpoint missing from diff")
	}
	if d.Direction != DirImproved {
		t.Errorf("Direction = %v, want improved", d.Direction)
	}
	if d.AvgPct != -80 {
		t.Errorf("AvgPct = %v, want -80", d.AvgPct)
	}
}

// The key correctness property of the diff: a successful optimization raises
// throughput, which raises total time. Judging by total would call that a
// regression and mislead the user into reverting a good change.
func TestCompareHTTPUsesAverageNotTotal(t *testing.T) {
	// Latency halved, so the benchmark pushed 4x the requests through.
	before := []HTTPStat{{Count: 100, Method: "GET", URI: "/a", Sum: 50, Avg: 0.5}}
	after := []HTTPStat{{Count: 400, Method: "GET", URI: "/a", Sum: 100, Avg: 0.25}}

	d := httpDiffFor(CompareHTTP(before, after), "GET /a")
	if d == nil {
		t.Fatal("endpoint missing from diff")
	}
	if d.SumDelta <= 0 {
		t.Fatalf("precondition: total time should have risen, got delta %v", d.SumDelta)
	}
	if d.Direction != DirImproved {
		t.Errorf("Direction = %v; higher throughput at lower latency is an improvement", d.Direction)
	}
}

// Benchmark runs vary by a few percent; that must not be painted as a change.
func TestCompareHTTPTreatsSmallChangeAsNeutral(t *testing.T) {
	before := []HTTPStat{{Count: 100, Method: "GET", URI: "/a", Sum: 10, Avg: 0.100}}
	after := []HTTPStat{{Count: 100, Method: "GET", URI: "/a", Sum: 10.2, Avg: 0.102}}

	d := httpDiffFor(CompareHTTP(before, after), "GET /a")
	if d.Direction != DirNeutral {
		t.Errorf("Direction = %v, want neutral for a 2%% change", d.Direction)
	}
}

// A newly introduced error is a regression even if the endpoint got faster.
func TestCompareHTTPFlagsNewErrors(t *testing.T) {
	before := []HTTPStat{{Count: 100, Status2xx: 100, Method: "GET", URI: "/a", Sum: 50, Avg: 0.5}}
	after := []HTTPStat{{Count: 100, Status2xx: 50, Status5xx: 50, Method: "GET", URI: "/a", Sum: 5, Avg: 0.05}}

	d := httpDiffFor(CompareHTTP(before, after), "GET /a")
	if d.Direction != DirWorsened {
		t.Errorf("Direction = %v; an endpoint that got fast by erroring out is a regression", d.Direction)
	}
}

// Fixing a failing endpoint is a real improvement even when its latency is
// unchanged; reporting it as flat hides the most important kind of progress.
func TestCompareHTTPCreditsErrorFixes(t *testing.T) {
	before := []HTTPStat{{Count: 40, Status5xx: 40, Method: "POST", URI: "/api/pay", Sum: 0.4, Avg: 0.01}}
	after := []HTTPStat{{Count: 40, Status2xx: 40, Method: "POST", URI: "/api/pay", Sum: 0.4, Avg: 0.01}}

	d := httpDiffFor(CompareHTTP(before, after), "POST /api/pay")
	if d == nil {
		t.Fatal("endpoint missing from diff")
	}
	if d.Direction != DirImproved {
		t.Errorf("Direction = %v; eliminating 40 errors should count as an improvement", d.Direction)
	}
}

func TestCompareHTTPMarksAddedAndGone(t *testing.T) {
	before := []HTTPStat{{Count: 10, Method: "GET", URI: "/old", Sum: 5, Avg: 0.5}}
	after := []HTTPStat{{Count: 10, Method: "GET", URI: "/new", Sum: 5, Avg: 0.5}}

	diffs := CompareHTTP(before, after)
	if d := httpDiffFor(diffs, "GET /old"); d == nil || d.Status != DiffGone {
		t.Errorf("/old status = %v, want gone", d)
	}
	if d := httpDiffFor(diffs, "GET /new"); d == nil || d.Status != DiffAdded {
		t.Errorf("/new status = %v, want added", d)
	}
}

// Replacing an N+1 loop with a single bulk query makes a brand-new query appear
// while the old one vanishes. That is the textbook success case, so the new row
// must not be painted as a regression.
func TestCompareQueryDoesNotPenalizeReplacementQuery(t *testing.T) {
	before := []QueryStat{
		{Count: 5400, Query: "SELECT `name` FROM `users` WHERE `id` = N", SumQueryTime: 3.24},
	}
	after := []QueryStat{
		{Count: 900, Query: "SELECT `name` FROM `users` WHERE `id` IN (N, N, N)", SumQueryTime: 1.35},
	}

	diffs := CompareQuery(before, after)

	var old, bulk *QueryDiff
	for i := range diffs {
		if diffs[i].Status == DiffGone {
			old = &diffs[i]
		}
		if diffs[i].Status == DiffAdded {
			bulk = &diffs[i]
		}
	}
	if old == nil || bulk == nil {
		t.Fatalf("expected one gone and one added query, got %+v", diffs)
	}
	if old.Direction != DirImproved {
		t.Errorf("eliminated query direction = %v, want improved", old.Direction)
	}
	if bulk.Direction == DirWorsened {
		t.Error("the bulk query that replaced an N+1 loop was reported as a regression")
	}
}

// Removing an N+1 loop cuts both the count and the total; that is the signal we
// most want the diff to confirm.
func TestCompareQueryDetectsNPlusOneRemoval(t *testing.T) {
	before := []QueryStat{
		{Count: 18000, Query: "SELECT `n` FROM `u` WHERE `id` = N", SumQueryTime: 9},
	}
	after := []QueryStat{
		{Count: 1000, Query: "SELECT `n` FROM `u` WHERE `id` = N", SumQueryTime: 0.5},
	}

	diffs := CompareQuery(before, after)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Direction != DirImproved {
		t.Errorf("Direction = %v, want improved", diffs[0].Direction)
	}
	if diffs[0].CountDelta != -17000 {
		t.Errorf("CountDelta = %v, want -17000", diffs[0].CountDelta)
	}
}

// Confirming an index landed: same count, far fewer rows examined.
func TestCompareQueryShowsExaminedRowsDrop(t *testing.T) {
	before := []QueryStat{
		{Count: 1000, Query: "SELECT * FROM `c` WHERE `t` = N", SumQueryTime: 12, SumRowsExamined: 980000, SumRowsSent: 1000},
	}
	after := []QueryStat{
		{Count: 1000, Query: "SELECT * FROM `c` WHERE `t` = N", SumQueryTime: 0.4, SumRowsExamined: 1000, SumRowsSent: 1000},
	}

	d := CompareQuery(before, after)[0]
	if d.ExaminedBefore != 980000 || d.ExaminedAfter != 1000 {
		t.Errorf("examined rows not carried through: %v -> %v", d.ExaminedBefore, d.ExaminedAfter)
	}
	if d.Direction != DirImproved {
		t.Errorf("Direction = %v, want improved", d.Direction)
	}
}

func TestCompareOrdersByLargestChange(t *testing.T) {
	before := []HTTPStat{
		{Count: 100, Method: "GET", URI: "/small", Sum: 10, Avg: 0.1},
		{Count: 100, Method: "GET", URI: "/big", Sum: 100, Avg: 1.0},
	}
	after := []HTTPStat{
		{Count: 100, Method: "GET", URI: "/small", Sum: 9, Avg: 0.09},
		{Count: 100, Method: "GET", URI: "/big", Sum: 20, Avg: 0.2},
	}

	diffs := CompareHTTP(before, after)
	if diffs[0].Endpoint != "GET /big" {
		t.Errorf("first entry = %q, want the endpoint with the largest change", diffs[0].Endpoint)
	}
}

func TestCompareTotals(t *testing.T) {
	before := Input{
		HTTP:  []HTTPStat{{Count: 100, Status2xx: 100, Method: "GET", URI: "/a", Sum: 50, Avg: 0.5}},
		Query: []QueryStat{{Count: 1000, Query: "SELECT 1", SumQueryTime: 30}},
	}
	after := Input{
		HTTP:  []HTTPStat{{Count: 200, Status2xx: 190, Status5xx: 10, Method: "GET", URI: "/a", Sum: 50, Avg: 0.25}},
		Query: []QueryStat{{Count: 500, Query: "SELECT 1", SumQueryTime: 10}},
	}

	rep := Compare("g1", "g2", before, after)

	if rep.Totals.RequestsBefore != 100 || rep.Totals.RequestsAfter != 200 {
		t.Errorf("request totals wrong: %+v", rep.Totals)
	}
	if rep.Totals.ErrorsAfter != 10 {
		t.Errorf("ErrorsAfter = %d, want 10", rep.Totals.ErrorsAfter)
	}
	// Per-request app time halved: 0.5s -> 0.25s.
	if rep.Totals.AppTimePct != -50 {
		t.Errorf("AppTimePct = %v, want -50", rep.Totals.AppTimePct)
	}
}

func TestCompareHandlesEmptySides(t *testing.T) {
	rep := Compare("g1", "g2", Input{}, Input{})
	if len(rep.HTTP) != 0 || len(rep.Query) != 0 {
		t.Error("expected empty diff for two empty inputs")
	}
	if rep.Totals.AppTimePct != 0 {
		t.Errorf("AppTimePct = %v, want 0", rep.Totals.AppTimePct)
	}
}
