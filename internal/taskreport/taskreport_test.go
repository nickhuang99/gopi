package taskreport

// taskreport_test.go — the PURE faces of the report contract. The
// writer/reader seam is proven in internal/tools (task_report_test.go, where
// the filesystem and the session-dir resolution live); this file pins the
// value-level rules the seam leans on: the bound announces itself, the
// section check is line-anchored, the ledger line round-trips, and the
// degradation note never dresses an unknown timestamp up as history.

import (
	"strings"
	"testing"
)

// TestBoundCapsOnALineBoundary: the "bounded" half. A cut mid-line would
// splice two report sentences together; the marker is mandatory so a reader
// can tell "the author ended here" from "the harness stopped the file"
// (Rule 15 — no silent failure).
func TestBoundCapsOnALineBoundary(t *testing.T) {
	small := "## Completed\n- short\n"
	if got, truncated := Bound(small); truncated || got != small {
		t.Errorf("a report under the cap must pass through untouched: %q truncated=%v", got, truncated)
	}

	var b strings.Builder
	for b.Len() < MaxBytes*2 {
		b.WriteString("- a line of report body that keeps going and going\n")
	}
	got, truncated := Bound(b.String())
	if !truncated {
		t.Fatal("oversized body must report truncated=true")
	}
	if !strings.HasSuffix(got, TruncationMarker()) {
		t.Errorf("bounded body must END with the marker: %q", got[len(got)-80:])
	}
	// The marker is the only allowance over the cap; the content itself is cut
	// at a line boundary, so the last content line is whole.
	if len(got) > MaxBytes+len(TruncationMarker()) {
		t.Errorf("bounded length %d exceeds cap+marker", len(got))
	}
	// The cut lands exactly ON a line boundary: the retained body is a prefix
	// of the source and the very next source byte is a newline — no partial
	// line was spliced into the report (the marker supplies its own leading
	// blank line).
	src := b.String()
	body := strings.TrimSuffix(got, TruncationMarker())
	if !strings.HasPrefix(src, body) || len(body) >= len(src) || src[len(body)] != '\n' {
		t.Errorf("cut must land on a line boundary, got tail %q", body[len(body)-40:])
	}
}

// TestMissingSectionsIsLineAnchored: the contract check must not be fooled by
// prose. A report that merely SAYS "completed" has not answered the section;
// only a heading line does.
func TestMissingSectionsIsLineAnchored(t *testing.T) {
	complete := "# Task Report: x\n\n## Completed\n- a\n\n## Files\n- b\n\n## Verification\n- c\n\n" +
		SectionEvidence + "\n- d\n\n## Not Done\n- e\n"
	if missing := MissingSections(complete); len(missing) != 0 {
		t.Errorf("complete report flagged missing %v", missing)
	}
	prose := "I completed the work and verified it.\n"
	if missing := MissingSections(prose); len(missing) != len(RequiredSections) {
		t.Errorf("prose-only body must miss every section, got %v", missing)
	}
	// An INDENTED heading-shaped line is a continuation, not a heading: the
	// measured corpus case is a report whose only `## Not Done` occurrence was
	// a wrapped bullet's continuation, and the trimmed-prefix read counted it as
	// a section (task 01a19466's sibling finding). A section's own line starts
	// at column 0.
	if missing := MissingSections("  ## Completed\n"); len(missing) != len(RequiredSections) {
		t.Errorf("an indented continuation must not satisfy a section, got %v", missing)
	}
	if missing := MissingSections("## Completed\n"); len(missing) != len(RequiredSections)-1 {
		t.Errorf("a column-0 heading must satisfy its section, got %v", missing)
	}
}

// TestSkeletonSatisfiesItsOwnContract: the writer and the reader share ONE
// section list, so the harness's own fallback can never be flagged by the
// reader's check.
func TestSkeletonSatisfiesItsOwnContract(t *testing.T) {
	body := Skeleton("t-1", []string{FormatFact("status", "completed"), FormatFact("branch", "")})
	if missing := MissingSections(body); len(missing) != 0 {
		t.Errorf("skeleton missing required sections %v:\n%s", missing, body)
	}
	if !strings.Contains(body, SkeletonPlaceholder) {
		t.Errorf("skeleton must mark the unrecorded parts:\n%s", body)
	}
	if !strings.Contains(body, "status: completed") {
		t.Errorf("skeleton must carry the harness facts:\n%s", body)
	}
	// An empty fact renders an explicit "(unknown)", never a dangling key or a
	// fabricated value.
	if !strings.Contains(body, "branch: (unknown)") {
		t.Errorf("an unknown fact must render as (unknown):\n%s", body)
	}
}

// TestDigestLineRoundTrips: the ledger line is the INDEX that outlives the
// session directory, so what the seal writes must be what show reads back.
func TestDigestLineRoundTrips(t *testing.T) {
	const path = "/home/x/.gopi/sessions/t-1/task-report.md"
	line := DigestLine(path, 642, []string{"## Files"})
	if got := PathFromDigest("gopi run completed — " + line); got != path {
		t.Errorf("PathFromDigest = %q, want %q", got, path)
	}
	if !strings.Contains(line, DigestLinePrefix) || !strings.Contains(line, "642 bytes") {
		t.Errorf("digest line must name the prefix and the size: %q", line)
	}
	if !strings.Contains(line, "missing sections: ## Files") {
		t.Errorf("digest line must record the section gap: %q", line)
	}
	// A digest naming no report yields no path (the pre-seam / no-run case).
	if got := PathFromDigest("gopi run completed — run: elapsed=3s"); got != "" {
		t.Errorf("digest without the prefix must yield empty, got %q", got)
	}
}

// TestDegradationNoteIsHonest: the pre-seam note is claimed ONLY on a
// positive timestamp that predates the seam; an unknown timestamp is treated
// as post-seam — an unknown must never be dressed up as history.
func TestDegradationNoteIsHonest(t *testing.T) {
	if got := DegradationNote(float64(LegacyCutoffUnix - 1)); got != LegacyNote {
		t.Errorf("pre-seam row = %q, want %q", got, LegacyNote)
	}
	if got := DegradationNote(float64(LegacyCutoffUnix + 1)); got != NoneNote {
		t.Errorf("post-seam row = %q, want %q", got, NoneNote)
	}
	if got := DegradationNote(0); got != NoneNote {
		t.Errorf("unknown timestamp = %q, want the plain gap note %q", got, NoneNote)
	}
	if !strings.Contains(LegacyNote, LegacyCutoffDate) {
		t.Errorf("the legacy note must name the seam date %q: %q", LegacyCutoffDate, LegacyNote)
	}
}

// TestShowBodyBoundIsTighterThanTheFileBound: show is a terminal digest, so
// its cut must be strictly tighter than the file cap — otherwise a file at
// the cap would be fully re-printed by `task show` and the digest would stop
// being a digest.
func TestShowBodyBoundIsTighterThanTheFileBound(t *testing.T) {
	if ShowBodyMaxBytes >= MaxBytes {
		t.Fatalf("ShowBodyMaxBytes (%d) must be < MaxBytes (%d)", ShowBodyMaxBytes, MaxBytes)
	}
	var b strings.Builder
	for b.Len() < MaxBytes {
		b.WriteString("- a line of report body that keeps going and going\n")
	}
	shown, truncated := TruncateBody(b.String())
	if !truncated || len(shown) > ShowBodyMaxBytes+len(TruncationMarker())+1 {
		t.Errorf("display body must be cut at ShowBodyMaxBytes, got %d bytes truncated=%v", len(shown), truncated)
	}
	if short, trunc := TruncateBody("## Completed\n- fine\n"); trunc || short != "## Completed\n- fine\n" {
		t.Errorf("a short body must pass through untouched: %q", short)
	}
}
