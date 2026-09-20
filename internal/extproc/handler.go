package extproc

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/kaz/pprotein/internal/collect"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
)

type (
	Handler struct {
		processor collect.Processor
		opts      *collect.Options
		collector *collect.Collector
	}
)

func NewHandler(processor collect.Processor, opts *collect.Options) *Handler {
	return &Handler{
		processor: processor,
		opts:      opts,
	}
}

// Collector exposes the underlying collector so that cross-cutting consumers
// (such as internal/diag) can read already-processed snapshots without
// collecting anything themselves. Returns nil before Register has run.
func (h *Handler) Collector() *collect.Collector {
	return h.collector
}

func (h *Handler) Register(g *echo.Group) error {
	var err error
	h.collector, err = collect.New(h.processor, h.opts)
	if err != nil {
		return fmt.Errorf("failed to initialize collector: %w", err)
	}

	g.GET("", h.getIndex)
	g.POST("", h.postIndex)
	g.GET("/:id", h.getId)
	g.GET("/data/:id", h.getData)
	g.GET("/data/latest", h.getLatestData)

	return nil
}

func (h *Handler) getIndex(c echo.Context) error {
	return c.JSON(http.StatusOK, h.collector.List())
}

func (h *Handler) postIndex(c echo.Context) error {
	target := &collect.SnapshotTarget{}
	if err := c.Bind(target); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("failed to parse request body: %v", err))
	}

	go func() {
		if err := h.collector.Collect(target); err != nil {
			log.Error("[!] collector aborted:", err)
		}
	}()

	return c.NoContent(http.StatusOK)
}

func (h *Handler) getId(c echo.Context) error {
	r, err := h.collector.Get(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Errorf("failed to get entry: %w", err))
	}
	defer r.Close()

	return c.Stream(http.StatusOK, "application/json", r)
}

func (h *Handler) getData(c echo.Context) error {
	id := c.Param("id")
	entries := h.collector.List()
	for _, entry := range entries {
		if entry.Snapshot.ID == id {
			bodyPath, err := entry.Snapshot.BodyPath()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get body path: %v", err))
			}

			return c.File(bodyPath)
		}
	}
	return echo.NewHTTPError(http.StatusNotFound)
}

func (h *Handler) getLatestData(c echo.Context) error {
	label := c.QueryParam("label")

	entries := h.collector.List()
	slices.SortFunc(entries, func(a, b *collect.Entry) int {
		return b.Snapshot.SnapshotMeta.Datetime.Compare(a.Snapshot.SnapshotMeta.Datetime)
	})

	if len(entries) > 0 {
		for _, entry := range entries {
			if label != "" && label != entry.Snapshot.SnapshotTarget.Label {
				continue
			}
			if entry.Status != collect.StatusOk {
				continue
			}

			bodyPath, err := entry.Snapshot.BodyPath()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get body path: %v", err))
			}
			return c.File(bodyPath)
		}
	}
	return echo.NewHTTPError(http.StatusNotFound)
}
