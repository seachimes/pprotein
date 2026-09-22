package score

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/goccy/go-json"
	"github.com/kaz/pprotein/internal/collect"
	"github.com/labstack/echo/v4"
)

type (
	// Source lists collected entries. It matches the collectors already in the
	// application and exists so the latest-group lookup can be tested without
	// standing up real collectors.
	Source interface {
		List() []*collect.Entry
	}

	requestBody struct {
		// GroupId ties the score to a collection. When empty the score attaches
		// to the most recent group, because the shell that knows the score has
		// no way to learn the id that pprotein generated internally.
		GroupId    string
		Label      string
		Score      int64
		Passed     *bool
		ErrorCount int64
		Target     string
		StartedAt  time.Time
		FinishedAt time.Time
		Raw        string
	}

	handler struct {
		opts      *collect.Options
		collector *collect.Collector
		sources   []Source
	}
)

// NewHandler builds the score endpoint. The sources are consulted only to
// resolve which group is newest when a request omits GroupId.
func NewHandler(opts *collect.Options, sources ...Source) *handler {
	return &handler{opts: opts, sources: sources}
}

func (h *handler) Register(g *echo.Group) error {
	var err error
	h.collector, err = collect.New(&processor{}, h.opts)
	if err != nil {
		return fmt.Errorf("failed to initialize collector: %w", err)
	}

	g.GET("", h.getIndex)
	g.POST("", h.postIndex)
	g.GET("/:id", h.getId)
	return nil
}

// Collector exposes the stored scores so diag can read them, matching how the
// other handlers share their collectors.
func (h *handler) Collector() *collect.Collector {
	return h.collector
}

func (h *handler) getIndex(c echo.Context) error {
	list := h.collector.List()
	for _, e := range list {
		v, err := h.read(e.Snapshot.ID)
		if err != nil {
			continue
		}
		e.Message = v.Summary()
	}
	return c.JSON(http.StatusOK, list)
}

func (h *handler) read(id string) (*Value, error) {
	r, err := h.collector.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get entry: %w", err)
	}
	defer r.Close()

	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read entry: %w", err)
	}

	v := &Value{}
	if err := json.Unmarshal(buf, v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal score: %w", err)
	}
	return v, nil
}

func (h *handler) postIndex(c echo.Context) error {
	req := &requestBody{}
	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("failed to parse request body: %v", err))
	}

	groupID := req.GroupId
	if groupID == "" {
		groupID = LatestGroup(h.sources)
		if groupID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "no collection to attach this score to: run a collection first, or pass GroupId explicitly")
		}
	}

	label := req.Label
	if label == "" {
		label = "bench"
	}

	// A run is treated as passing unless the caller says otherwise, so that the
	// common `{"Score": 12345}` request records a successful run rather than
	// silently marking every score as a failure.
	passed := true
	if req.Passed != nil {
		passed = *req.Passed
	}

	v := &Value{
		Score:      req.Score,
		Passed:     passed,
		ErrorCount: req.ErrorCount,
		Target:     req.Target,
		StartedAt:  req.StartedAt,
		FinishedAt: req.FinishedAt,
		Raw:        req.Raw,
	}

	buf, err := json.Marshal(v)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal score: %v", err))
	}

	snapshot, err := h.collector.Add(&collect.SnapshotTarget{GroupId: groupID, Label: label}, buf)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("failed to add snapshot: %v", err))
	}

	entry := &collect.Entry{Snapshot: snapshot, Status: collect.StatusOk, Message: v.Summary()}
	if eventData, err := json.Marshal(entry); err == nil {
		h.opts.EventHub.Publish(eventData)
	}

	return c.JSON(http.StatusOK, entry)
}

func (h *handler) getId(c echo.Context) error {
	r, err := h.collector.Get(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get entry: %v", err))
	}
	defer r.Close()

	return c.Stream(http.StatusOK, "application/json", r)
}

// LatestGroup returns the group id of the most recent successful collection.
//
// "Most recent" is decided by the newest snapshot within each group rather than
// by the group id itself. Ids are generated from wall-clock time and sort
// correctly today, but relying on that would silently break if the format ever
// changed, and a failed collection must not win over a good one.
func LatestGroup(sources []Source) string {
	newest := map[string]time.Time{}

	for _, src := range sources {
		if src == nil {
			continue
		}
		for _, e := range src.List() {
			if e == nil || e.Snapshot == nil || e.Status != collect.StatusOk {
				continue
			}
			gid := e.Snapshot.GroupId
			if gid == "" {
				continue
			}
			if at, ok := newest[gid]; !ok || e.Snapshot.Datetime.After(at) {
				newest[gid] = e.Snapshot.Datetime
			}
		}
	}

	ids := make([]string, 0, len(newest))
	for gid := range newest {
		ids = append(ids, gid)
	}
	// Sort by time, falling back to the id so the result is deterministic when
	// two collections share a timestamp.
	sort.Slice(ids, func(i, j int) bool {
		a, b := newest[ids[i]], newest[ids[j]]
		if a.Equal(b) {
			return ids[i] > ids[j]
		}
		return a.After(b)
	})

	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}
