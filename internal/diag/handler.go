package diag

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/kaz/pprotein/internal/collect"
	"github.com/labstack/echo/v4"
)

// Source supplies the processed artifacts of one snapshot type. It is satisfied
// by the existing collectors, so diag reads whatever they have already produced
// instead of collecting anything itself.
type Source interface {
	List() []*collect.Entry
	Get(id string) (io.ReadCloser, error)
}

// Handler serves the diagnosis and diff endpoints.
type Handler struct {
	httplog Source
	slowlog Source
	pprof   Source
	score   Source

	thresholds Thresholds
}

// NewHandler wires diag to the collectors owned by the other handlers. Any of
// them may be nil, in which case the corresponding rules are skipped and the
// gap is reported as a health issue.
func NewHandler(httplog, slowlog, pprofSrc, scoreSrc Source) *Handler {
	return &Handler{
		httplog:    httplog,
		slowlog:    slowlog,
		pprof:      pprofSrc,
		score:      scoreSrc,
		thresholds: DefaultThresholds(),
	}
}

func (h *Handler) Register(g *echo.Group) error {
	g.GET("/groups", h.getGroups)
	g.GET("/report/:gid", h.getReport)
	g.GET("/diff", h.getDiff)
	return nil
}

// GroupInfo is a measurement group that diag can analyze.
type GroupInfo struct {
	GroupID    string `json:"GroupID"`
	Datetime   string `json:"Datetime"`
	HasHTTPLog bool   `json:"HasHTTPLog"`
	HasSlowLog bool   `json:"HasSlowLog"`
	HasPprof   bool   `json:"HasPprof"`
}

func (h *Handler) getGroups(c echo.Context) error {
	type acc struct {
		info GroupInfo
		when string
	}
	groups := map[string]*acc{}

	mark := func(src Source, set func(*GroupInfo)) {
		if src == nil {
			return
		}
		for _, e := range src.List() {
			if e.Status != collect.StatusOk || e.Snapshot == nil {
				continue
			}
			gid := e.Snapshot.GroupId
			if gid == "" {
				continue
			}
			a, ok := groups[gid]
			if !ok {
				a = &acc{info: GroupInfo{GroupID: gid}}
				groups[gid] = a
			}
			set(&a.info)

			ts := e.Snapshot.Datetime.Format("2006-01-02 15:04:05")
			if a.when == "" || ts < a.when {
				a.when = ts
			}
		}
	}

	mark(h.httplog, func(g *GroupInfo) { g.HasHTTPLog = true })
	mark(h.slowlog, func(g *GroupInfo) { g.HasSlowLog = true })
	mark(h.pprof, func(g *GroupInfo) { g.HasPprof = true })

	out := make([]GroupInfo, 0, len(groups))
	for _, a := range groups {
		a.info.Datetime = a.when
		out = append(out, a.info)
	}
	// Newest first: the group you just measured is the one you want.
	sort.Slice(out, func(i, j int) bool {
		return out[i].GroupID > out[j].GroupID
	})
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) getReport(c echo.Context) error {
	gid := c.Param("gid")
	if gid == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "group id is required")
	}

	in, err := h.buildInput(gid)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to build input: %v", err))
	}

	return c.JSON(http.StatusOK, Analyze(in, h.thresholds))
}

func (h *Handler) getDiff(c echo.Context) error {
	beforeID := c.QueryParam("before")
	afterID := c.QueryParam("after")
	if beforeID == "" || afterID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "both 'before' and 'after' group ids are required")
	}

	before, err := h.buildInput(beforeID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to build before input: %v", err))
	}
	after, err := h.buildInput(afterID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to build after input: %v", err))
	}

	rep := Compare(beforeID, afterID, before, after)
	// The score is attached after the fact because it is recorded separately
	// from the profiling data and may be missing for either run.
	rep.Totals.Score = CompareScore(
		h.scoreOf(beforeID),
		h.scoreOf(afterID),
		directionForLowerIsBetter(rep.Totals.AppTimePct, DiffBoth),
	)

	return c.JSON(http.StatusOK, rep)
}

// scoreOf returns the benchmark result recorded for a group, or nil when none
// was reported. When a group holds several runs the newest one wins, so that
// re-running the benchmark against the same collection reflects the latest
// attempt.
func (h *Handler) scoreOf(gid string) *ScoreRun {
	if h.score == nil {
		return nil
	}

	var (
		newest *ScoreRun
		at     time.Time
	)
	for _, e := range entriesOf(h.score, gid) {
		r, err := h.score.Get(e.Snapshot.ID)
		if err != nil {
			continue
		}
		var v ScoreRun
		err = json.NewDecoder(r).Decode(&v)
		r.Close()
		if err != nil {
			continue
		}
		if newest == nil || e.Snapshot.Datetime.After(at) {
			newest, at = &v, e.Snapshot.Datetime
		}
	}
	return newest
}

// buildInput gathers every processed artifact belonging to a group.
//
// A group may contain several snapshots of the same type when multiple servers
// are measured at once; their stats are merged so the report describes the
// system as a whole.
func (h *Handler) buildInput(gid string) (Input, error) {
	in := Input{GroupID: gid}

	if h.httplog != nil {
		for _, e := range entriesOf(h.httplog, gid) {
			in.HasHTTPLog = true

			r, err := h.httplog.Get(e.Snapshot.ID)
			if err != nil {
				continue
			}
			stats, err := ParseHTTPStats(r)
			r.Close()
			if err != nil {
				continue
			}
			in.HTTP = mergeHTTP(in.HTTP, stats)
		}
	}

	if h.slowlog != nil {
		for _, e := range entriesOf(h.slowlog, gid) {
			in.HasSlowLog = true

			r, err := h.slowlog.Get(e.Snapshot.ID)
			if err != nil {
				continue
			}
			stats, err := ParseQueryStats(r)
			r.Close()
			if err != nil {
				continue
			}
			in.Query = mergeQuery(in.Query, stats)
		}
	}

	if h.pprof != nil {
		for _, e := range entriesOf(h.pprof, gid) {
			in.HasPprof = true

			// The pprof collector's Process registers HTTP handlers and returns
			// no reader, so the raw protobuf is read from the snapshot body.
			path, err := e.Snapshot.BodyPath()
			if err != nil {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			funcs, _, err := ParsePprof(f)
			f.Close()
			if err != nil {
				continue
			}
			in.Funcs = mergeFuncs(in.Funcs, funcs)
		}
	}

	return in, nil
}

func entriesOf(src Source, gid string) []*collect.Entry {
	var out []*collect.Entry
	for _, e := range src.List() {
		if e.Status != collect.StatusOk || e.Snapshot == nil {
			continue
		}
		if e.Snapshot.GroupId != gid {
			continue
		}
		out = append(out, e)
	}
	return out
}

// mergeHTTP combines alp aggregates for the same endpoint across servers.
//
// Sums and counts add; averages are recomputed from them. Percentiles cannot be
// merged correctly from aggregates, so the maximum is kept as a conservative
// stand-in rather than inventing a precise-looking wrong number.
func mergeHTTP(dst, src []HTTPStat) []HTTPStat {
	if len(dst) == 0 {
		// Copy rather than alias: the caller owns src, and a later merge into
		// the returned slice would otherwise write through into their data.
		return append([]HTTPStat(nil), src...)
	}
	idx := map[string]int{}
	for i, h := range dst {
		idx[h.Endpoint()] = i
	}
	for _, s := range src {
		i, ok := idx[s.Endpoint()]
		if !ok {
			dst = append(dst, s)
			idx[s.Endpoint()] = len(dst) - 1
			continue
		}
		d := &dst[i]
		d.Count += s.Count
		d.Status1xx += s.Status1xx
		d.Status2xx += s.Status2xx
		d.Status3xx += s.Status3xx
		d.Status4xx += s.Status4xx
		d.Status5xx += s.Status5xx
		d.Sum += s.Sum
		d.SumBody += s.SumBody
		if s.Max > d.Max {
			d.Max = s.Max
		}
		if d.Min == 0 || (s.Min > 0 && s.Min < d.Min) {
			d.Min = s.Min
		}
		d.P90 = maxF(d.P90, s.P90)
		d.P95 = maxF(d.P95, s.P95)
		d.P99 = maxF(d.P99, s.P99)
		if d.Count > 0 {
			d.Avg = d.Sum / float64(d.Count)
			d.AvgBody = d.SumBody / float64(d.Count)
		}
	}
	return dst
}

func mergeQuery(dst, src []QueryStat) []QueryStat {
	if len(dst) == 0 {
		// Copy rather than alias; see mergeHTTP.
		return append([]QueryStat(nil), src...)
	}
	idx := map[string]int{}
	for i, q := range dst {
		idx[q.Query] = i
	}
	for _, s := range src {
		i, ok := idx[s.Query]
		if !ok {
			dst = append(dst, s)
			idx[s.Query] = len(dst) - 1
			continue
		}
		d := &dst[i]
		d.Count += s.Count
		d.SumQueryTime += s.SumQueryTime
		d.SumLockTime += s.SumLockTime
		d.SumRowsSent += s.SumRowsSent
		d.SumRowsExamined += s.SumRowsExamined
		d.MaxQueryTime = maxF(d.MaxQueryTime, s.MaxQueryTime)
		d.MaxLockTime = maxF(d.MaxLockTime, s.MaxLockTime)
		d.MaxRowsSent = maxF(d.MaxRowsSent, s.MaxRowsSent)
		d.MaxRowsExamined = maxF(d.MaxRowsExamined, s.MaxRowsExamined)
		if d.MinQueryTime == 0 || (s.MinQueryTime > 0 && s.MinQueryTime < d.MinQueryTime) {
			d.MinQueryTime = s.MinQueryTime
		}
		if d.Count > 0 {
			n := float64(d.Count)
			d.AvgQueryTime = d.SumQueryTime / n
			d.AvgLockTime = d.SumLockTime / n
			d.AvgRowsSent = d.SumRowsSent / n
			d.AvgRowsExamined = d.SumRowsExamined / n
		}
	}
	return dst
}

// mergeFuncs combines CPU profiles by absolute sample value, then recomputes
// each function's share against the new combined total.
func mergeFuncs(dst, src []FuncStat) []FuncStat {
	if len(src) == 0 {
		return dst
	}
	if len(dst) == 0 {
		// Copy rather than alias; see mergeHTTP.
		return append([]FuncStat(nil), src...)
	}

	flat := map[string]int64{}
	var total int64
	for _, f := range dst {
		flat[f.Function] += f.Flat
		total += f.Flat
	}
	for _, f := range src {
		flat[f.Function] += f.Flat
		total += f.Flat
	}

	out := make([]FuncStat, 0, len(flat))
	for fn, v := range flat {
		pct := 0.0
		if total > 0 {
			pct = float64(v) / float64(total)
		}
		out = append(out, FuncStat{Function: fn, Flat: v, FlatPct: pct})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Flat != out[j].Flat {
			return out[i].Flat > out[j].Flat
		}
		return out[i].Function < out[j].Function
	})
	return out
}
