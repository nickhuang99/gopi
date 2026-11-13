package version

// Regression coverage for the ONE thing internal/tools/rebuild.go asks this
// package: recover a REVISION out of whatever a `--version` answer looks like,
// and refuse a dirty one. Both halves are load-bearing — a reader that cannot
// read the answer makes every merge round end in a false "version mismatch"
// plus an auto-revert of a good merge (epic 2026-09-13), and a dirty answer
// accepted as the tip puts a stale binary's revision where master's belongs.
//
// The fixture strings are the REAL shapes, kept verbatim: the multi-line one
// was recorded from merger.log while the auto-revert was happening, and the
// display shapes are what the older producer printed.

import (
	"strings"
	"testing"
)

const (
	mergedReportFullRevision = "faf5f2190e8176d5c3d8b18562df696c427247c2"
	mergedReportShortRev     = "faf5f2190e81"
	mergedReportTip          = "faf5f219"
	mergedReportTag          = "version/v20260913-1244-faf5f219"

	mergedReportThreeLines = "v20260913-1244-faf5f219 (" + mergedReportShortRev + ")\n" +
		"revision: " + mergedReportFullRevision + "\n" +
		"tag:      " + mergedReportTag
)

// The defect: `bin/gopi --version` answered with the multi-line report
// whenever the build carried an identity, and the WHOLE report was handed to
// the single-line RevisionOf — which read one line, found no parenthesised
// revision group, and reported NO revision out of a perfectly correct answer.
// That empty revision made the version assertion false and the post-merge
// rebuild auto-reverted the merge.
//
// This test pins BOTH halves: the report-aware reader finds the revision, and
// the single-line parser stays unable to read a report (if it ever becomes
// able, this test says so instead of quietly passing for the wrong reason).
func TestRevisionOfDisplayReadsMultiLineReport(t *testing.T) {
	rev, ok := RevisionOfDisplay(mergedReportThreeLines)
	if !ok {
		t.Fatalf("RevisionOfDisplay(multi-line report) = ok=false; the probe must read the report")
	}
	// The report's first line is the display, and the display outranks the
	// fact line: it carries the abbreviated revision of the SAME commit.
	if !strings.HasPrefix(mergedReportFullRevision, rev) {
		t.Errorf("RevisionOfDisplay(report) = %q, which does not name %s", rev, mergedReportFullRevision)
	}
	// The wrong-reader half: the single-token parser cannot read the report,
	// which is exactly why binaryVersion must call RevisionOfDisplay.
	if _, _, ok := RevisionOf(mergedReportThreeLines); ok {
		t.Errorf("RevisionOf(report) reported a revision; the report is multi-line and the single-line parser must not be able to read it")
	}
}

// When the answer carries NO revision on its first line (a stamped marketing
// name, a report whose first line was lost), the key-value fact line is the
// fallback — read by stripping the report's OWN key constant (Rule 13:
// ReportRevisionKey is written by the producer and stripped here).
func TestRevisionOfDisplayFallsBackToTheRevisionFactLine(t *testing.T) {
	report := "v1.0.0\nrevision: " + mergedReportFullRevision + "\ntag:      v1.0.0"
	rev, ok := RevisionOfDisplay(report)
	if !ok {
		t.Fatalf("RevisionOfDisplay(%q) = ok=false; the fact line carries the revision", report)
	}
	if rev != mergedReportFullRevision {
		t.Errorf("RevisionOfDisplay(%q) = %q, want %q", report, rev, mergedReportFullRevision)
	}
}

// The zero-value case (Rule 15a): an answer with no facts at all must not be
// mistaken for one carrying a revision, and non-revision prose (or a truncated
// fact line) must never be read as one — no blanket always-true reader.
func TestRevisionOfDisplayZeroValueAndLegacyShapes(t *testing.T) {
	if rev, ok := RevisionOfDisplay(""); ok {
		t.Errorf("RevisionOfDisplay(\"\") = (%q, true); the empty answer carries no revision", rev)
	}
	// A report whose revision fact line is EMPTY (truncated mid-write) carries
	// no revision either.
	if rev, ok := RevisionOfDisplay("v1.0.0\n" + ReportRevisionKey + "\n" + "tag:      v1.0.0"); ok {
		t.Errorf("RevisionOfDisplay(report with an empty revision fact) = (%q, true); want no revision", rev)
	}
	// The unstamped-build line, in BOTH generations of its wording: today's, and
	// the pre-fix one that named the wrong cause. Neither carries provenance, so
	// the reader must refuse both — and if a future edit made the message carry
	// something a reader could mistake for a revision, this case says so instead
	// of quietly passing for the wrong reason. (Why the old wording was wrong:
	// a build inside a LINKED WORKTREE embeds no vcs.revision either, so
	// "built outside a git checkout" was false on the shape every gopi task run
	// builds in — cmd/agent/cli_version_stamp_test.go pins the real builds.)
	olderNoVCSStampWording := "revision: unavailable — this build carries no VCS stamp (built outside a git checkout)"
	for _, prose := range []string{"dev", "1.2.3", "v1.0.0", "nightly-build", "no facts here\nat all\n",
		"revision: unavailable — this build carries no VCS stamp", olderNoVCSStampWording} {
		if rev, ok := RevisionOfDisplay(prose); ok {
			t.Errorf("RevisionOfDisplay(%q) = (%q, true); want no revision", prose, rev)
		}
	}
	// The tolerated older shapes keep working: a bare revision, and the tagged
	// display line an older binary printed.
	if rev, ok := RevisionOfDisplay(mergedReportFullRevision); !ok || rev != mergedReportFullRevision {
		t.Errorf("RevisionOfDisplay(bare revision) = (%q, %v)", rev, ok)
	}
	if rev, ok := RevisionOfDisplay("v20260913-1244-faf5f219 (" + mergedReportShortRev + ")"); !ok || rev != mergedReportShortRev {
		t.Errorf("RevisionOfDisplay(tagged display line) = (%q, %v)", rev, ok)
	}
	// A line that merely LOOKS key-value but names another key is not a
	// revision carrier on its own.
	if rev, ok := RevisionOfDisplay("tag: v1.0.0"); ok {
		t.Errorf("RevisionOfDisplay(\"tag: v1.0.0\") = (%q, true); want no revision", rev)
	}
}

// TestIsDirtyWitnessesEveryProducerShape: the dirty witness has to survive the
// producer generation change — an older binary states it as a suffix on the
// display's name, today's producer states it as its own report line, and a
// clean answer of either generation must NOT read as dirty (that false
// positive is what the tail-only predicate produced).
func TestIsDirtyWitnessesEveryProducerShape(t *testing.T) {
	dirtyShapes := []string{
		"v20260918-0932-8380ce9caeef" + DirtySuffix + " (8380ce9caeef)",
		"8380ce9caeef" + DirtySuffix,
		"v0.1.0\n" + ReportRevisionKey + " 8380ce9caeef\n" + MetadataModifiedKey + " " + MetadataModifiedTrue,
	}
	for _, s := range dirtyShapes {
		if !IsDirty(s) {
			t.Errorf("IsDirty(%q) = false; every producer shape must witness a dirty build", s)
		}
	}
	cleanShapes := []string{
		"v20260918-0932-8380ce9caeef (8380ce9caeef)",
		"8380ce9caeef",
		"v0.1.0\n" + ReportRevisionKey + " 8380ce9caeef\ngo: go1.24.5",
		"v0.1.0\n" + MetadataModifiedKey + " false",
	}
	for _, s := range cleanShapes {
		if IsDirty(s) {
			t.Errorf("IsDirty(%q) = true; a clean answer must not read as dirty", s)
		}
	}
}

// The minted-tag trap, which the name-half witness exists for: a minted tag
// name ENDS in the short revision, so a dirty tagged display reads
// "…-8380ce9caeef-dirty (8380ce9caeef)" — the tail is ")", and a tail-only
// check calls it clean. The verifier then accepted a DIRTY build's revision as
// the committed tip.
func TestRevisionOfMintedTagNameIsNotADirtyRevision(t *testing.T) {
	const tip = "8380ce9caeef"
	tagged := "v20260918-0932-" + tip
	rev, dirty, ok := RevisionOf(tagged + DirtySuffix + " (" + tip + ")")
	if !ok {
		t.Fatalf("RevisionOf(dirty tagged display) = ok=false")
	}
	if !dirty {
		t.Errorf("the dirty marker on the NAME half was not witnessed (display %q)", tagged+DirtySuffix)
	}
	if rev != tip {
		t.Errorf("RevisionOf = %q, want the revision group %q", rev, tip)
	}
	// The same name without the marker is clean, and the revision group is
	// still the revision.
	if _, clean, ok := RevisionOf(tagged + " (" + tip + ")"); !ok || clean {
		t.Errorf("a minted tag name with no marker read as dirty or unparsable")
	}
}
