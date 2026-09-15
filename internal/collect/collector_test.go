package collect

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kaz/pprotein/internal/event"
	"github.com/kaz/pprotein/internal/storage"
)

type stubProcessor struct {
	err error
}

func (p *stubProcessor) Cacheable() bool {
	return false
}
func (p *stubProcessor) Process(snapshot *Snapshot) (io.ReadCloser, error) {
	if p.err != nil {
		return nil, p.err
	}

	bodyPath, err := snapshot.BodyPath()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewBuffer(body)), nil
}

func newTestStore(t *testing.T) storage.Storage {
	t.Helper()

	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	return store
}

func newTestCollector(t *testing.T, store storage.Storage, typ string, procErr error) *Collector {
	t.Helper()

	c, err := New(&stubProcessor{err: procErr}, &Options{
		Type:     typ,
		Ext:      "-" + typ + ".log",
		Store:    store,
		EventHub: event.NewHub(),
	})
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}
	return c
}

// snapshots restored on startup are processed in the background
func waitForEntry(t *testing.T, c *Collector) *Entry {
	t.Helper()

	for i := 0; i < 200; i++ {
		if list := c.List(); len(list) > 0 {
			return list[0]
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("no entry appeared")
	return nil
}

// Add wrote its meta with the bucket and the key swapped, so memos were not
// readable after a restart
func TestAddedSnapshotSurvivesRestart(t *testing.T) {
	store := newTestStore(t)

	c := newTestCollector(t, store, "memo", nil)
	if _, err := c.Add(&SnapshotTarget{GroupId: "g", Label: "l"}, []byte(`{"Text":"note"}`)); err != nil {
		t.Fatalf("failed to add: %v", err)
	}

	raws, err := store.GetAll("memo")
	if err != nil {
		t.Fatalf("failed to read the memo bucket: %v", err)
	}
	if len(raws) != 1 {
		t.Fatalf("want 1 snapshot in the memo bucket, got %v", len(raws))
	}

	entry := waitForEntry(t, newTestCollector(t, store, "memo", nil))
	if entry.Status != StatusOk {
		t.Errorf("want a restored memo to be ok, got %v (%v)", entry.Status, entry.Message)
	}
}

func TestFailedSnapshotSurvivesRestart(t *testing.T) {
	store := newTestStore(t)
	procErr := errors.New("slp is not installed")

	c := newTestCollector(t, store, "slowlog", procErr)
	if _, err := c.Add(&SnapshotTarget{GroupId: "g", Label: "l"}, []byte("log")); err == nil {
		t.Fatal("want Add to fail when the processor fails")
	}

	entry := waitForEntry(t, newTestCollector(t, store, "slowlog", procErr))
	if entry.Status != StatusFail {
		t.Fatalf("want a restored failure to stay failed, got %v", entry.Status)
	}
	if !strings.Contains(entry.Message, procErr.Error()) {
		t.Errorf("want the reason to be kept, got %q", entry.Message)
	}
	// a restored snapshot without its storage would panic here
	if _, err := entry.Snapshot.BodyPath(); err != nil {
		t.Errorf("want a usable body path, got %v", err)
	}
}

func TestRestoredFailuresAreScopedToTheirType(t *testing.T) {
	store := newTestStore(t)

	c := newTestCollector(t, store, "slowlog", errors.New("slp is not installed"))
	if _, err := c.Add(&SnapshotTarget{GroupId: "g", Label: "l"}, []byte("log")); err == nil {
		t.Fatal("want Add to fail when the processor fails")
	}

	other := newTestCollector(t, store, "pprof", nil)
	if list := other.List(); len(list) != 0 {
		t.Errorf("want failures of other types to stay out, got %v", len(list))
	}
}
