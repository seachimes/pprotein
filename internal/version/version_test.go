package version

import (
	"strings"
	"testing"
)

func TestGetAlwaysProducesADisplayableVersion(t *testing.T) {
	i := Get()
	if i.Version == "" {
		t.Fatal("Version must never be empty; the header would render blank")
	}
	// A pseudo-version is ~36 characters and does not fit a header; Get() is
	// expected to collapse it to a short revision.
	if len(i.Version) > 24 {
		t.Errorf("Version %q is too long to display (%d chars)", i.Version, len(i.Version))
	}
}

func TestGetIsDeterministic(t *testing.T) {
	if Get() != Get() {
		t.Error("Get() must return a stable, cached value")
	}
}

// Go itself appends the dirty marker to bi.Main.Version, so a naive
// implementation ends up with "...+dirty+dirty".
func TestDirtySuffixIsNotDuplicated(t *testing.T) {
	i := Get()
	if n := strings.Count(i.Version, dirtySuffix); n > 1 {
		t.Errorf("Version %q contains %d dirty markers, want at most 1", i.Version, n)
	}
}

func TestIsPseudoVersion(t *testing.T) {
	cases := map[string]bool{
		// Real shapes emitted by the toolchain, including the dirty variant
		// that broke the first implementation.
		"v1.2.7-0.20260920055717-6246413b890e":       true,
		"v1.2.7-0.20260920055717-6246413b890e+dirty": true,
		"v0.0.0-20260920055717-6246413b890e":         true,
		"v1.2.6":                                     false,
		"v1.2.6+dirty":                               false,
		"v1.2.6-rc1":                                 false,
		"":                                           false,
		"v1.2.7-0.20260920055717-zzzzzzzzzzzz":       false, // not hex
		"v1.2.7-0.2026092005571-6246413b890e":        false, // 13-digit stamp
	}
	for v, want := range cases {
		if got := isPseudoVersion(v); got != want {
			t.Errorf("isPseudoVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestShortRevision(t *testing.T) {
	if got := shortRevision("6246413b890e4747f13671021f0cf589740ef99a"); got != "6246413" {
		t.Errorf("got %q, want 6246413", got)
	}
	if got := shortRevision("abc"); got != "abc" {
		t.Errorf("a short input must pass through unchanged, got %q", got)
	}
}

func TestStringIncludesVersion(t *testing.T) {
	i := Info{Version: "v1.2.6", GoVersion: "go1.24.4"}
	if got := i.String(); got != "v1.2.6 (go1.24.4)" {
		t.Errorf("got %q", got)
	}
	// The Go version is optional.
	if got := (Info{Version: "v1.2.6"}).String(); got != "v1.2.6" {
		t.Errorf("got %q, want bare version", got)
	}
}
