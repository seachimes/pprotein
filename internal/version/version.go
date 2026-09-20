// Package version reports which build of pprotein is running.
//
// During a contest several builds often get deployed in quick succession, so
// being able to confirm from the UI which one is actually serving avoids the
// classic "I fixed that already" confusion caused by a stale process.
package version

import (
	"runtime/debug"
	"strings"
	"sync"
)

// Version is overridable at link time for release builds:
//
//	go build -ldflags "-X github.com/kaz/pprotein/internal/version.Version=v1.2.6"
//
// When it is empty the value is derived from the VCS metadata the Go toolchain
// embeds automatically, so development builds report something useful without
// any build flags.
var Version string

// Info describes the running binary.
type Info struct {
	// Version is a release tag when known, otherwise a short commit hash, and
	// "unknown" when no build metadata is available at all.
	Version string `json:"Version"`
	// Revision is the full commit hash, empty when unavailable.
	Revision string `json:"Revision"`
	// Modified reports whether the working tree had uncommitted changes.
	Modified bool `json:"Modified"`
	// GoVersion is the toolchain that produced the binary.
	GoVersion string `json:"GoVersion"`
}

var (
	once   sync.Once
	cached Info
)

// Get returns the build information, computed once.
func Get() Info {
	once.Do(func() { cached = read() })
	return cached
}

func read() Info {
	info := Info{Version: Version}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		if info.Version == "" {
			info.Version = "unknown"
		}
		return info
	}

	info.GoVersion = bi.GoVersion

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}

	if info.Version == "" {
		// go install of a tagged module records a real tag here. A plain
		// `go build` leaves "(devel)", and an untagged build records a
		// pseudo-version such as v1.2.7-0.20260920055717-6246413b890e, which is
		// too long to sit in a header and already contains the commit hash.
		if v := bi.Main.Version; v != "" && v != "(devel)" && !isPseudoVersion(v) {
			info.Version = v
		} else if info.Revision != "" {
			info.Version = shortRevision(info.Revision)
		} else {
			info.Version = "unknown"
		}
	}

	// Mark dirty builds so a locally patched binary is never mistaken for the
	// commit it was built from. The suffix may already be present when Version
	// was injected at link time from a dirty tree.
	if info.Modified && !strings.HasSuffix(info.Version, dirtySuffix) {
		info.Version += dirtySuffix
	}

	return info
}

const dirtySuffix = "+dirty"

// isPseudoVersion reports whether v is a Go pseudo-version, which encodes a
// timestamp and commit hash rather than a human-meaningful release.
//
// The shapes produced by the toolchain all end in
// "-<14-digit UTC timestamp>-<12-hex revision>", where the timestamp segment
// may itself be prefixed (for example "0.20260920055717" when the base version
// has no pre-release part).
func isPseudoVersion(v string) bool {
	// The toolchain may already have appended the dirty marker.
	v = strings.TrimSuffix(v, dirtySuffix)

	i := strings.LastIndex(v, "-")
	if i < 0 {
		return false
	}
	rev := v[i+1:]
	if len(rev) != 12 || !isHex(rev) {
		return false
	}

	rest := v[:i]
	j := strings.LastIndex(rest, "-")
	if j < 0 {
		return false
	}
	ts := rest[j+1:]
	// Drop a leading "0." or "<pre>." qualifier before checking the timestamp.
	if k := strings.LastIndex(ts, "."); k >= 0 {
		ts = ts[k+1:]
	}
	return len(ts) == 14 && isDigits(ts)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}

func shortRevision(rev string) string {
	const n = 7
	if len(rev) <= n {
		return rev
	}
	return rev[:n]
}

// String renders the version for logs and CLI output.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.Version)
	if i.GoVersion != "" {
		b.WriteString(" (")
		b.WriteString(i.GoVersion)
		b.WriteString(")")
	}
	return b.String()
}
