package diag

import (
	"strings"
	"testing"
)

// The fixtures below are verbatim output from the actual alp/slp binaries that
// internal/extproc invokes, so a change in their column layout breaks these
// tests rather than silently producing wrong numbers in production.

const alpFixture = "Count\t1xx\t2xx\t3xx\t4xx\t5xx\tMethod\tUri\tMin\tMax\tSum\tAvg\tP90\tP95\tP99\tStddev\tMin(Body)\tMax(Body)\tSum(Body)\tAvg(Body)\n" +
	"1\t0\t0\t1\t0\t0\tPOST\t/login\t1.520\t1.520\t1.520\t1.520\t1.520\t1.520\t1.520\t0.000\t0.000\t0.000\t0.000\t0.000\n" +
	"2\t0\t2\t0\t0\t0\tGET\t/api/courses\t0.420\t0.520\t0.940\t0.470\t0.520\t0.520\t0.520\t0.050\t1200.000\t1234.000\t2434.000\t1217.000\n"

const slpFixture = "Count\tQuery\tMin(QueryTime)\tMax(QueryTime)\tSum(QueryTime)\tAvg(QueryTime)\tMin(LockTime)\tMax(LockTime)\tSum(LockTime)\tAvg(LockTime)\tMin(RowsSent)\tMax(RowsSent)\tSum(RowsSent)\tAvg(RowsSent)\tMin(RowsExamined)\tMax(RowsExamined)\tSum(RowsExamined)\tAvg(RowsExamined)\n" +
	"2\tSELECT * FROM `courses` WHERE `teacher_id` = N ORDER BY `created_at`\t0.423000\t0.523000\t0.946000\t0.473000\t0.000100\t0.000100\t0.000200\t0.000100\t1\t1\t2\t1.000000\t97000\t98000\t195000\t97500.000000\n"

func TestParseHTTPStats(t *testing.T) {
	stats, err := ParseHTTPStats(strings.NewReader(alpFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(stats))
	}

	login := stats[0]
	if login.Method != "POST" || login.URI != "/login" {
		t.Errorf("unexpected endpoint: %q", login.Endpoint())
	}
	if login.Count != 1 {
		t.Errorf("Count = %d, want 1", login.Count)
	}
	if login.Status3xx != 1 {
		t.Errorf("Status3xx = %d, want 1", login.Status3xx)
	}
	if login.Sum != 1.520 {
		t.Errorf("Sum = %v, want 1.520", login.Sum)
	}

	courses := stats[1]
	if courses.Endpoint() != "GET /api/courses" {
		t.Errorf("Endpoint = %q", courses.Endpoint())
	}
	if courses.Status2xx != 2 {
		t.Errorf("Status2xx = %d, want 2", courses.Status2xx)
	}
	if courses.AvgBody != 1217.0 {
		t.Errorf("AvgBody = %v, want 1217", courses.AvgBody)
	}
}

func TestParseQueryStats(t *testing.T) {
	stats, err := ParseQueryStats(strings.NewReader(slpFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 row, got %d", len(stats))
	}

	q := stats[0]
	if q.Count != 2 {
		t.Errorf("Count = %d, want 2", q.Count)
	}
	if q.SumQueryTime != 0.946 {
		t.Errorf("SumQueryTime = %v, want 0.946", q.SumQueryTime)
	}
	if q.SumRowsExamined != 195000 {
		t.Errorf("SumRowsExamined = %v, want 195000", q.SumRowsExamined)
	}
	if got := q.ExaminedPerSent(); got != 97500 {
		t.Errorf("ExaminedPerSent = %v, want 97500", got)
	}
}

// A missing or renamed column must not be read as a zero from the wrong index.
func TestParseToleratesColumnReorder(t *testing.T) {
	reordered := "Uri\tCount\tMethod\tSum\n/x\t7\tGET\t3.5\n"
	stats, err := ParseHTTPStats(strings.NewReader(reordered))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 row, got %d", len(stats))
	}
	if stats[0].Count != 7 || stats[0].Sum != 3.5 || stats[0].URI != "/x" {
		t.Errorf("columns resolved by position instead of name: %+v", stats[0])
	}
}

func TestParseEmptyInput(t *testing.T) {
	h, err := ParseHTTPStats(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h) != 0 {
		t.Errorf("expected no rows, got %d", len(h))
	}

	q, err := ParseQueryStats(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q) != 0 {
		t.Errorf("expected no rows, got %d", len(q))
	}
}

// slp emits backtick-quoted identifiers and quoted literals; the TSV reader
// must not treat those as field structure.
func TestParseQueryWithQuotes(t *testing.T) {
	in := "Count\tQuery\tSum(QueryTime)\n" +
		"3\tSELECT * FROM `u` WHERE `name` = \"x\" AND `t` = 'y'\t1.5\n"
	stats, err := ParseQueryStats(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 row, got %d", len(stats))
	}
	if !strings.Contains(stats[0].Query, "name") {
		t.Errorf("query mangled: %q", stats[0].Query)
	}
	if stats[0].SumQueryTime != 1.5 {
		t.Errorf("SumQueryTime = %v, want 1.5", stats[0].SumQueryTime)
	}
}
