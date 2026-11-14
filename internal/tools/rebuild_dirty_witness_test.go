package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// The producer's REAL dirty TAGGED display, not a fabricated one: Info.Display
// decorates the NAME, so the marker sits in the MIDDLE of the string —
//
//	"v20260918-0932-8380ce9caeef-dirty (8380ce9caeef)"
//
// — and the string still ENDS in ")" because the name half ends in the short
// revision it borrows. A verifier testing the dirty witness with a tail-only
// HasSuffix("…-dirty") call therefore reads this string as CLEAN and accepts
// it as the committed tip. The pre-existing negative test missed it because
// its fixture appended "-dirty" to the whole string, a shape Display never
// emits.
const (
	producerDirtyTip  = "8380ce9caeef"
	producerDirtyName = "v20260918-0932-" + producerDirtyTip
	// producerDirtyDisplay is exactly what Info.Display() prints for a tagged
	// build made from a dirty tree.
	producerDirtyDisplay = producerDirtyName + gopiversion.DirtySuffix + " (" + producerDirtyTip + ")"
	// producerDirtyReport is the full multi-line report for the same binary:
	// the display line, the revision fact, and the dirty witness as a report
	// line of its own (the shape this package's producer writes today).
	producerDirtyReport = producerDirtyDisplay + "\n" +
		gopiversion.ReportRevisionKey + " " + producerDirtyTip + "ffffffffffffffffffffffffffffffff\n" +
		gopiversion.MetadataModifiedKey + " " + gopiversion.MetadataModifiedTrue
)

// TestProducerDirtyTaggedDisplayNeverMatchesTip is the regression for the
// half-fixed dirty guard: the multi-line REPORT was made readable (epic
// 2026-09-13) but the reject predicate stayed a tail-only check, so the
// producer's own dirty tagged display was accepted as the tip. A dirty build
// carries NO tip claim whatever display shape it wears — the epic's rule,
// now enforced on the shape the producer actually emits.
func TestProducerDirtyTaggedDisplayNeverMatchesTip(t *testing.T) {
	if versionMatches(producerDirtyDisplay, producerDirtyTip) {
		t.Errorf("versionMatches(%q, %q) = true; a dirty tagged build must never claim the tip", producerDirtyDisplay, producerDirtyTip)
	}
	if versionMatches(producerDirtyReport, producerDirtyTip) {
		t.Errorf("versionMatches(dirty report, tip) = true; the report says the tree was dirty")
	}
	if !gopiversion.IsDirty(producerDirtyDisplay) || !gopiversion.IsDirty(producerDirtyReport) {
		t.Errorf("IsDirty missed the producer's dirty witness in %q / %q", producerDirtyDisplay, producerDirtyReport)
	}
	// The OTHER half, so the guard rejects DIRT and not tags: the same strings
	// without the marker name the tip and must match.
	cleanDisplay := producerDirtyName + " (" + producerDirtyTip + ")"
	if !versionMatches(cleanDisplay, producerDirtyTip) {
		t.Errorf("versionMatches(%q, %q) = false; the clean tagged display names the tip", cleanDisplay, producerDirtyTip)
	}
	if gopiversion.IsDirty(cleanDisplay) {
		t.Errorf("IsDirty(%q) = true; the clean display carries no marker", cleanDisplay)
	}
	// A documented fact: the display of a dirty tagged build cannot be told
	// from a clean one by a revision-carrying parse (RevisionOf reports
	// ok=true, dirty=true and yields the tip) — which is exactly why the dirty
	// verdict must be taken from the witness and never delegated to that parse.
	if rev, dirty, ok := gopiversion.RevisionOf(producerDirtyDisplay); !ok || !dirty || rev != producerDirtyTip {
		t.Fatalf("RevisionOf(%q) = %q/%v/%v, want a dirty read of the tip", producerDirtyDisplay, rev, dirty, ok)
	}
}

// TestProbeDropsRevisionOfProducerDirtyReport drives the real seam: the
// rebuild probe must report NO revision for the producer's dirty report, so
// the assertion can only refuse — never accept a dirty build.
func TestProbeDropsRevisionOfProducerDirtyReport(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	binDir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportFile := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(reportFile, []byte(producerDirtyReport+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "gopi")
	// The probe script itself prints the report (binaryVersion runs
	// `sh -c '"$0" --version' <bin>`, so $0 is this file).
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then cat " + reportFile + "; fi\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	display, revision, dirty := binaryVersion(repo)
	if !dirty {
		t.Fatalf("binaryVersion reported dirty=false for %q", display)
	}
	if revision != "" {
		t.Errorf("binaryVersion revision = %q, want \"\" (a dirty report carries no tip claim)", revision)
	}
	if !strings.Contains(display, gopiversion.DirtySuffix) {
		t.Errorf("display %q does not carry the dirty marker", display)
	}
}
