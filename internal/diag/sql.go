package diag

import (
	"regexp"
	"strings"
)

// Lightweight SQL inspection. This is deliberately not a real parser: slp has
// already normalized the query, and all we need is enough structure to name the
// table and guess index columns. Anything ambiguous yields an empty result and
// the caller degrades to a less specific suggestion.

var (
	reFrom    = regexp.MustCompile("(?i)\\bFROM\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")
	reUpdate  = regexp.MustCompile("(?i)\\bUPDATE\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")
	reInsert  = regexp.MustCompile("(?i)\\bINSERT\\s+(?:IGNORE\\s+)?INTO\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")
	reDelete  = regexp.MustCompile("(?i)\\bDELETE\\s+FROM\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")
	reReplace = regexp.MustCompile("(?i)\\bREPLACE\\s+INTO\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")

	// Equality predicates are the ones worth putting first in a composite index.
	reWhereEq = regexp.MustCompile("(?i)\\b(?:WHERE|AND)\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?\\s*(?:=|IN)\\s*")
	// Range predicates must come last in a composite index.
	reWhereRange = regexp.MustCompile("(?i)\\b(?:WHERE|AND)\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?\\s*(?:<|>|<=|>=|BETWEEN)")
	reJoin       = regexp.MustCompile(`(?i)\bJOIN\b`)
	reOrderBy    = regexp.MustCompile("(?i)\\bORDER\\s+BY\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")
	reGroupBy    = regexp.MustCompile("(?i)\\bGROUP\\s+BY\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")
)

// QueryKind classifies a statement.
type QueryKind string

const (
	KindSelect  QueryKind = "SELECT"
	KindInsert  QueryKind = "INSERT"
	KindUpdate  QueryKind = "UPDATE"
	KindDelete  QueryKind = "DELETE"
	KindReplace QueryKind = "REPLACE"
	KindOther   QueryKind = "OTHER"
)

func kindOf(query string) QueryKind {
	switch {
	case hasPrefixFold(query, "SELECT"):
		return KindSelect
	case hasPrefixFold(query, "INSERT"):
		return KindInsert
	case hasPrefixFold(query, "UPDATE"):
		return KindUpdate
	case hasPrefixFold(query, "DELETE"):
		return KindDelete
	case hasPrefixFold(query, "REPLACE"):
		return KindReplace
	}
	return KindOther
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// tableOf returns the primary table a statement touches, or "" when unknown.
func tableOf(query string) string {
	switch kindOf(query) {
	case KindUpdate:
		if m := reUpdate.FindStringSubmatch(query); m != nil {
			return m[1]
		}
	case KindInsert:
		if m := reInsert.FindStringSubmatch(query); m != nil {
			return m[1]
		}
	case KindReplace:
		if m := reReplace.FindStringSubmatch(query); m != nil {
			return m[1]
		}
	case KindDelete:
		if m := reDelete.FindStringSubmatch(query); m != nil {
			return m[1]
		}
	}
	if m := reFrom.FindStringSubmatch(query); m != nil {
		return m[1]
	}
	return ""
}

// indexColumns proposes a composite index column order for a query.
//
// The ordering rule is the standard one: equality predicates first, then the
// ORDER BY / GROUP BY column, then range predicates. Returns nil when no
// predicate could be identified, so the caller can avoid emitting a bogus DDL.
func indexColumns(query string) []string {
	seen := map[string]bool{}
	var cols []string

	add := func(c string) {
		c = strings.ToLower(c)
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		cols = append(cols, c)
	}

	// Multiple table sources mean a column cannot be attributed to a table by
	// this regex-level parse. Emitting a composite index that mixes columns
	// from different tables produces a DDL that is actively wrong, so bail out
	// and let the caller fall back to generic advice.
	if hasMultipleTableSources(query) {
		return nil
	}

	for _, m := range reWhereEq.FindAllStringSubmatch(query, -1) {
		add(m[1])
	}
	if m := reGroupBy.FindStringSubmatch(query); m != nil {
		add(m[1])
	}
	if m := reOrderBy.FindStringSubmatch(query); m != nil {
		add(m[1])
	}
	for _, m := range reWhereRange.FindAllStringSubmatch(query, -1) {
		add(m[1])
	}

	// A composite index wider than three columns is rarely what you want under
	// time pressure, and the confidence in our parse drops sharply.
	if len(cols) > 3 {
		cols = cols[:3]
	}
	return cols
}

// hasMultipleTableSources reports whether a statement reads from more than one
// table, which this parser cannot attribute columns across.
//
// A JOIN is already handled by the caller (the join keyword yields no usable
// equality predicate), but a subquery is the dangerous case: `SELECT ... FROM a
// WHERE id IN (SELECT oid FROM b WHERE y = ?)` would otherwise suggest indexing
// `a (id, y)` even though `y` lives on `b`.
func hasMultipleTableSources(query string) bool {
	if len(reFrom.FindAllStringIndex(query, -1)) > 1 {
		return true
	}
	return reJoin.MatchString(query)
}

// truncate collapses whitespace and shortens a query for display.
//
// The limit counts runes, not bytes: queries routinely contain non-ASCII text
// in identifiers or string literals, and slicing a byte offset would cut a
// multi-byte rune in half and render as U+FFFD.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
