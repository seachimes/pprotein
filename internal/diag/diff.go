package diag

import (
	"sort"
)

// Diff compares two measurement groups so that the effect of a change can be
// read directly instead of by eyeballing two tables side by side.
//
// A contest is a loop of measure / change / measure, and the question after
// every iteration is the same: did that help, and did it break something else?

type (
	// DiffDirection is which way a metric moved, already interpreted so the UI
	// does not have to know whether "up" is good for a given metric.
	DiffDirection string

	// HTTPDiff is the per-endpoint comparison.
	HTTPDiff struct {
		Endpoint string `json:"Endpoint"`

		CountBefore int `json:"CountBefore"`
		CountAfter  int `json:"CountAfter"`
		CountDelta  int `json:"CountDelta"`

		SumBefore float64 `json:"SumBefore"`
		SumAfter  float64 `json:"SumAfter"`
		SumDelta  float64 `json:"SumDelta"`

		AvgBefore float64 `json:"AvgBefore"`
		AvgAfter  float64 `json:"AvgAfter"`
		AvgDelta  float64 `json:"AvgDelta"`
		AvgPct    float64 `json:"AvgPct"`

		ErrBefore int `json:"ErrBefore"`
		ErrAfter  int `json:"ErrAfter"`

		Status    DiffStatus    `json:"Status"`
		Direction DiffDirection `json:"Direction"`
	}

	// QueryDiff is the per-normalized-query comparison.
	QueryDiff struct {
		Query string `json:"Query"`

		CountBefore int `json:"CountBefore"`
		CountAfter  int `json:"CountAfter"`
		CountDelta  int `json:"CountDelta"`

		SumBefore float64 `json:"SumBefore"`
		SumAfter  float64 `json:"SumAfter"`
		SumDelta  float64 `json:"SumDelta"`
		SumPct    float64 `json:"SumPct"`

		ExaminedBefore float64 `json:"ExaminedBefore"`
		ExaminedAfter  float64 `json:"ExaminedAfter"`

		Status    DiffStatus    `json:"Status"`
		Direction DiffDirection `json:"Direction"`
	}

	// DiffStatus marks whether an entry is new, gone, or present in both.
	DiffStatus string

	// DiffReport is the full comparison of two groups.
	DiffReport struct {
		BeforeGroup string `json:"BeforeGroup"`
		AfterGroup  string `json:"AfterGroup"`

		HTTP  []HTTPDiff  `json:"HTTP"`
		Query []QueryDiff `json:"Query"`

		Totals DiffTotals `json:"Totals"`
	}

	// DiffTotals is the headline summary of the comparison.
	DiffTotals struct {
		RequestsBefore int     `json:"RequestsBefore"`
		RequestsAfter  int     `json:"RequestsAfter"`
		AppTimeBefore  float64 `json:"AppTimeBefore"`
		AppTimeAfter   float64 `json:"AppTimeAfter"`
		AppTimePct     float64 `json:"AppTimePct"`

		QueriesBefore   int     `json:"QueriesBefore"`
		QueriesAfter    int     `json:"QueriesAfter"`
		QueryTimeBefore float64 `json:"QueryTimeBefore"`
		QueryTimeAfter  float64 `json:"QueryTimeAfter"`
		QueryTimePct    float64 `json:"QueryTimePct"`

		ErrorsBefore int `json:"ErrorsBefore"`
		ErrorsAfter  int `json:"ErrorsAfter"`
	}
)

const (
	DiffBoth  DiffStatus = "both"
	DiffAdded DiffStatus = "added"
	DiffGone  DiffStatus = "gone"

	DirImproved DiffDirection = "improved"
	DirWorsened DiffDirection = "worsened"
	DirNeutral  DiffDirection = "neutral"
)

// significantPct is the threshold below which a change is treated as noise.
// Run-to-run variance in a benchmark is easily a few percent, and flagging that
// as a regression trains people to ignore the colors.
const significantPct = 5.0

// CompareHTTP diffs two sets of alp aggregates.
//
// Average latency, not total, drives the verdict: a faster system serves more
// requests, which legitimately raises the total. Judging by total would report
// a successful optimization as a regression.
func CompareHTTP(before, after []HTTPStat) []HTTPDiff {
	beforeMap := map[string]HTTPStat{}
	for _, h := range before {
		beforeMap[h.Endpoint()] = h
	}
	afterMap := map[string]HTTPStat{}
	for _, h := range after {
		afterMap[h.Endpoint()] = h
	}

	keys := unionKeys(beforeMap, afterMap)
	out := make([]HTTPDiff, 0, len(keys))

	for _, k := range keys {
		b, okB := beforeMap[k]
		a, okA := afterMap[k]

		d := HTTPDiff{
			Endpoint:    k,
			CountBefore: b.Count,
			CountAfter:  a.Count,
			CountDelta:  a.Count - b.Count,
			SumBefore:   b.Sum,
			SumAfter:    a.Sum,
			SumDelta:    a.Sum - b.Sum,
			AvgBefore:   b.Avg,
			AvgAfter:    a.Avg,
			AvgDelta:    a.Avg - b.Avg,
			ErrBefore:   b.Status4xx + b.Status5xx,
			ErrAfter:    a.Status4xx + a.Status5xx,
		}

		switch {
		case !okB:
			d.Status = DiffAdded
		case !okA:
			d.Status = DiffGone
		default:
			d.Status = DiffBoth
		}

		d.AvgPct = pctChange(b.Avg, a.Avg)
		d.Direction = directionForLowerIsBetter(d.AvgPct, d.Status)

		// Correctness outranks latency, so error counts override the verdict in
		// both directions: a new error is a regression even if the endpoint got
		// faster, and eliminating errors is a win even when timings are flat.
		switch {
		case d.ErrAfter > d.ErrBefore && d.ErrAfter > 0:
			d.Direction = DirWorsened
		case d.ErrBefore > 0 && d.ErrAfter < d.ErrBefore && d.Direction == DirNeutral:
			d.Direction = DirImproved
		}

		out = append(out, d)
	}

	// Largest absolute total-time change first: that is where attention pays off.
	sort.SliceStable(out, func(i, j int) bool {
		return absF(out[i].SumDelta) > absF(out[j].SumDelta)
	})
	return out
}

// CompareQuery diffs two sets of slp aggregates.
//
// Total query time drives the verdict here, because unlike HTTP requests the
// query count is something we are actively trying to reduce (N+1 removal), so a
// drop in count is a success rather than a sign of reduced throughput.
func CompareQuery(before, after []QueryStat) []QueryDiff {
	beforeMap := map[string]QueryStat{}
	for _, q := range before {
		beforeMap[q.Query] = q
	}
	afterMap := map[string]QueryStat{}
	for _, q := range after {
		afterMap[q.Query] = q
	}

	keys := unionKeys(beforeMap, afterMap)
	out := make([]QueryDiff, 0, len(keys))

	for _, k := range keys {
		b, okB := beforeMap[k]
		a, okA := afterMap[k]

		d := QueryDiff{
			Query:          k,
			CountBefore:    b.Count,
			CountAfter:     a.Count,
			CountDelta:     a.Count - b.Count,
			SumBefore:      b.SumQueryTime,
			SumAfter:       a.SumQueryTime,
			SumDelta:       a.SumQueryTime - b.SumQueryTime,
			ExaminedBefore: b.SumRowsExamined,
			ExaminedAfter:  a.SumRowsExamined,
		}

		switch {
		case !okB:
			d.Status = DiffAdded
		case !okA:
			d.Status = DiffGone
		default:
			d.Status = DiffBoth
		}

		d.SumPct = pctChange(b.SumQueryTime, a.SumQueryTime)
		d.Direction = directionForLowerIsBetter(d.SumPct, d.Status)

		out = append(out, d)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return absF(out[i].SumDelta) > absF(out[j].SumDelta)
	})
	return out
}

// Compare builds the complete diff report for two groups.
func Compare(beforeGroup, afterGroup string, before, after Input) DiffReport {
	rep := DiffReport{
		BeforeGroup: beforeGroup,
		AfterGroup:  afterGroup,
		HTTP:        CompareHTTP(before.HTTP, after.HTTP),
		Query:       CompareQuery(before.Query, after.Query),
	}

	bs := summarize(before)
	as := summarize(after)

	for _, h := range before.HTTP {
		rep.Totals.ErrorsBefore += h.Status4xx + h.Status5xx
	}
	for _, h := range after.HTTP {
		rep.Totals.ErrorsAfter += h.Status4xx + h.Status5xx
	}

	rep.Totals.RequestsBefore = bs.TotalRequests
	rep.Totals.RequestsAfter = as.TotalRequests
	rep.Totals.AppTimeBefore = bs.TotalAppTime
	rep.Totals.AppTimeAfter = as.TotalAppTime
	rep.Totals.QueriesBefore = bs.TotalQueries
	rep.Totals.QueriesAfter = as.TotalQueries
	rep.Totals.QueryTimeBefore = bs.TotalQueryTime
	rep.Totals.QueryTimeAfter = as.TotalQueryTime

	// Compare average latency rather than the totals, for the same reason as
	// in CompareHTTP: throughput changes move the totals on their own.
	avgBefore := perRequest(bs.TotalAppTime, bs.TotalRequests)
	avgAfter := perRequest(as.TotalAppTime, as.TotalRequests)
	rep.Totals.AppTimePct = pctChange(avgBefore, avgAfter)

	qBefore := perRequest(bs.TotalQueryTime, bs.TotalRequests)
	qAfter := perRequest(as.TotalQueryTime, as.TotalRequests)
	rep.Totals.QueryTimePct = pctChange(qBefore, qAfter)

	return rep
}

func perRequest(total float64, reqs int) float64 {
	if reqs <= 0 {
		return 0
	}
	return total / float64(reqs)
}

func pctChange(before, after float64) float64 {
	if before == 0 {
		if after == 0 {
			return 0
		}
		return 100
	}
	return (after - before) / before * 100
}

func directionForLowerIsBetter(pct float64, status DiffStatus) DiffDirection {
	switch status {
	case DiffAdded:
		// There is no baseline to compare a newly appeared entry against, and
		// treating it as a regression is actively misleading: replacing an N+1
		// loop with a single IN(...) query makes a new row appear, which is the
		// success case. Status already marks it as added, so the UI can badge
		// it without the diff claiming a verdict it cannot justify.
		return DirNeutral
	case DiffGone:
		// An entry that disappeared had its cost eliminated outright.
		return DirImproved
	}
	if pct <= -significantPct {
		return DirImproved
	}
	if pct >= significantPct {
		return DirWorsened
	}
	return DirNeutral
}

func unionKeys[T any](a, b map[string]T) []string {
	seen := map[string]bool{}
	var keys []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
