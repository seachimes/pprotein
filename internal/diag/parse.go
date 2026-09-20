package diag

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The column layouts below were confirmed against the actual output of
// `alp ltsv --format tsv` and `slp my --output standard --format tsv`, which is
// what internal/extproc/{alp,slp} invoke. Columns are looked up by name rather
// than by index so that a future alp/slp release that adds or reorders columns
// degrades gracefully instead of silently mis-reading numbers.

// HTTPStat is one row of alp output: an endpoint aggregate.
type HTTPStat struct {
	Count                                                 int
	Status1xx, Status2xx, Status3xx, Status4xx, Status5xx int
	Method                                                string
	URI                                                   string
	Min, Max, Sum, Avg                                    float64
	P90, P95, P99                                         float64
	SumBody, AvgBody                                      float64
}

// Endpoint is the "METHOD URI" key used to join against other sources.
func (h HTTPStat) Endpoint() string {
	return h.Method + " " + h.URI
}

// QueryStat is one row of slp output: a normalized query aggregate.
type QueryStat struct {
	Count                                                  int
	Query                                                  string
	MinQueryTime, MaxQueryTime, SumQueryTime, AvgQueryTime float64
	MinLockTime, MaxLockTime, SumLockTime, AvgLockTime     float64
	MinRowsSent, MaxRowsSent, SumRowsSent, AvgRowsSent     float64
	MinRowsExamined, MaxRowsExamined                       float64
	SumRowsExamined, AvgRowsExamined                       float64
}

// ExaminedPerSent is the classic "is this query using an index?" ratio. A value
// far above 1 means the engine walked many rows to return few.
//
// Writes report Rows_sent = 0, so the row count alone would make every UPDATE
// and DELETE look like a full scan. In that case the execution count is used as
// the denominator instead, which asks the more meaningful question: how many
// rows were examined per statement? A well-targeted `UPDATE ... WHERE id = ?`
// then scores ~1, while a scanning update still scores high.
func (q QueryStat) ExaminedPerSent() float64 {
	denom := q.SumRowsSent
	if denom <= 0 {
		denom = float64(q.Count)
	}
	if denom <= 0 {
		return q.SumRowsExamined
	}
	return q.SumRowsExamined / denom
}

// tsvTable indexes a TSV document by header name.
type tsvTable struct {
	header map[string]int
	rows   [][]string
}

func parseTSV(r io.Reader) (*tsvTable, error) {
	cr := csv.NewReader(r)
	cr.Comma = '\t'
	// alp and slp emit queries that may legitimately contain quotes; do not let
	// the CSV reader treat them as field delimiters.
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read tsv: %w", err)
	}
	if len(records) == 0 {
		return &tsvTable{header: map[string]int{}}, nil
	}

	header := map[string]int{}
	for i, name := range records[0] {
		header[strings.TrimSpace(name)] = i
	}
	return &tsvTable{header: header, rows: records[1:]}, nil
}

func (t *tsvTable) str(row []string, name string) string {
	i, ok := t.header[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func (t *tsvTable) num(row []string, name string) float64 {
	v, err := strconv.ParseFloat(t.str(row, name), 64)
	if err != nil {
		return 0
	}
	return v
}

func (t *tsvTable) int(row []string, name string) int {
	return int(t.num(row, name))
}

// ParseHTTPStats reads alp TSV output.
func ParseHTTPStats(r io.Reader) ([]HTTPStat, error) {
	t, err := parseTSV(r)
	if err != nil {
		return nil, err
	}
	if _, ok := t.header["Uri"]; !ok {
		if len(t.rows) == 0 && len(t.header) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected alp output: missing Uri column")
	}

	stats := make([]HTTPStat, 0, len(t.rows))
	for _, row := range t.rows {
		if len(row) == 0 {
			continue
		}
		s := HTTPStat{
			Count:     t.int(row, "Count"),
			Status1xx: t.int(row, "1xx"),
			Status2xx: t.int(row, "2xx"),
			Status3xx: t.int(row, "3xx"),
			Status4xx: t.int(row, "4xx"),
			Status5xx: t.int(row, "5xx"),
			Method:    t.str(row, "Method"),
			URI:       t.str(row, "Uri"),
			Min:       t.num(row, "Min"),
			Max:       t.num(row, "Max"),
			Sum:       t.num(row, "Sum"),
			Avg:       t.num(row, "Avg"),
			P90:       t.num(row, "P90"),
			P95:       t.num(row, "P95"),
			P99:       t.num(row, "P99"),
			SumBody:   t.num(row, "Sum(Body)"),
			AvgBody:   t.num(row, "Avg(Body)"),
		}
		if s.URI == "" {
			continue
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// ParseQueryStats reads slp TSV output.
func ParseQueryStats(r io.Reader) ([]QueryStat, error) {
	t, err := parseTSV(r)
	if err != nil {
		return nil, err
	}
	if _, ok := t.header["Query"]; !ok {
		if len(t.rows) == 0 && len(t.header) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected slp output: missing Query column")
	}

	stats := make([]QueryStat, 0, len(t.rows))
	for _, row := range t.rows {
		if len(row) == 0 {
			continue
		}
		s := QueryStat{
			Count:           t.int(row, "Count"),
			Query:           t.str(row, "Query"),
			MinQueryTime:    t.num(row, "Min(QueryTime)"),
			MaxQueryTime:    t.num(row, "Max(QueryTime)"),
			SumQueryTime:    t.num(row, "Sum(QueryTime)"),
			AvgQueryTime:    t.num(row, "Avg(QueryTime)"),
			MinLockTime:     t.num(row, "Min(LockTime)"),
			MaxLockTime:     t.num(row, "Max(LockTime)"),
			SumLockTime:     t.num(row, "Sum(LockTime)"),
			AvgLockTime:     t.num(row, "Avg(LockTime)"),
			MinRowsSent:     t.num(row, "Min(RowsSent)"),
			MaxRowsSent:     t.num(row, "Max(RowsSent)"),
			SumRowsSent:     t.num(row, "Sum(RowsSent)"),
			AvgRowsSent:     t.num(row, "Avg(RowsSent)"),
			MinRowsExamined: t.num(row, "Min(RowsExamined)"),
			MaxRowsExamined: t.num(row, "Max(RowsExamined)"),
			SumRowsExamined: t.num(row, "Sum(RowsExamined)"),
			AvgRowsExamined: t.num(row, "Avg(RowsExamined)"),
		}
		if s.Query == "" {
			continue
		}
		stats = append(stats, s)
	}
	return stats, nil
}
