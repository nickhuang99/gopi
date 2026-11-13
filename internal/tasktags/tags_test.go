// Package tasktags tests: the shared tag values are a PERSISTED protocol
// (written into tasks.tags, compared by every consumer), so their spelling
// is pinned here — a silent edit to a value would strand every row already
// on disk under the old spelling (Rule 13/14).
package tasktags

import "testing"

func TestTagValuesAreThePersistedSpellings(t *testing.T) {
	// The expected strings are asserted as literals ON PURPOSE: this is the
	// one place the on-disk values are pinned, so a rename has to be a
	// deliberate, reviewed change with a migration — never an accident.
	cases := []struct{ got, want string }{
		{BugTag, "bug"},
		{VerifiedReworkTag, "verified-rework"},
		{FixtureTag, "fixture"},
		{ReworkLineageTag, "rework"},
		{RerouteTag, "reroute"},
		{CloneLineageTag, "clone"},
		{ReworkOfTagPrefix, "rework-of:"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("tag value = %q, want %q — these strings are persisted in tasks.tags; changing one strands existing rows", c.got, c.want)
		}
	}
}

func TestTagsAreDistinctAndNonEmpty(t *testing.T) {
	if BugTag == VerifiedReworkTag {
		t.Fatalf("tag values must be distinct (both %q)", BugTag)
	}
	for _, v := range []string{BugTag, VerifiedReworkTag} {
		if v == "" {
			t.Error("a tag value must never be empty (an empty token matches nothing and writes a stray comma)")
		}
	}
}
