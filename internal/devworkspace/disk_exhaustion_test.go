package devworkspace

// The MEASURED case this file pins, in one row: a clone-channel run on this host
//
//	build: /bin/sh -c '<declared build>'
//	… /usr/local/go/pkg/tool/linux_amd64/link: writing …: no space left on device
//	build step refused, kind=build
//
// and the next round re-ran the same build on the same partition. Nothing in
// that sentence said "filesystem", so nobody reclaimed anything; the revision
// had not been linked at all. These tests are the regression guard for the
// classification that replaced it (disk_exhaustion.go).

import (
	"strings"
	"testing"
)

// TestBuildBlockerClassifiesAFilesystemRefusalAsItsOwnKind is the minimal repro
// (Rule 5): ONE line of the kernel's own text, fed to the classifier that the
// build step calls, must not come back as a code failure.
func TestBuildBlockerClassifiesAFilesystemRefusalAsItsOwnKind(t *testing.T) {
	out := "/usr/local/go/pkg/tool/linux_amd64/link: writing /home/nick/workspace/x/a.out: no space left on device\n"
	blk := buildBlocker(t.TempDir(), out)
	if blk.Kind != BlockerKindDiskExhaustion {
		t.Fatalf("a `no space left on device` build output must be kind %q, got %q (words: %s)",
			BlockerKindDiskExhaustion, blk.Kind, blk.Words)
	}
	if blk.Marker != "no space left on device" {
		t.Fatalf("the blocker must quote the marker it matched, got %q", blk.Marker)
	}
	if !strings.Contains(blk.Words, "no space left on device") {
		t.Fatalf("the build's own words must survive verbatim, got %q", blk.Words)
	}

	report := blk.Report()
	// THE THREE FACTS THE DISK KIND EXISTS TO CARRY.
	if !strings.Contains(report, "NEVER LINKED") {
		t.Fatalf("the report must say the revision was never linked — that is the whole point of the kind:\n%s", report)
	}
	if !strings.Contains(report, "gopi tool scratch-gc gocache --trim") {
		t.Fatalf("the report must carry the reclaim command, copy-pasteable:\n%s", report)
	}
	if !strings.Contains(report, "device ") {
		t.Fatalf("the report must render the ground's filesystem (Rule 22: judged by device id):\n%s", report)
	}
	// AND IT MUST NOT WEAR THE CODE-FAILURE WORDING — that conflation is the
	// defect being fixed.
	if strings.Contains(report, "the build step refused") {
		t.Fatalf("a full disk must not be reported as the build step refusing a code failure:\n%s", report)
	}
}

// TestBuildBlockerKeepsTheCodeClassWhenADiagnosticIsPresent is the GUARD that
// keeps the class honest (scratch.DiskExhaustionDiagnosticShape wins): a repo
// whose declared build runs a test suite can print a disk phrase in a failing
// test's message while the compile error is the real defect, and relabelling
// that as a full disk sends the reader to the wrong machine.
func TestBuildBlockerKeepsTheCodeClassWhenADiagnosticIsPresent(t *testing.T) {
	out := "./disk_exhaustion.go:42:7: undefined: tail\n" +
		"--- FAIL: TestSomething (0.01s)\n" +
		"    something_test.go:9: no space left on device\n" +
		"make: *** [Makefile:42: build] Error 1\n"
	blk := buildBlocker(t.TempDir(), out)
	if blk.Kind != BlockerKindBuild {
		t.Fatalf("an output carrying a compiler diagnostic is a CODE failure whatever else it mentions, got kind %q:\n%s",
			blk.Kind, blk.Words)
	}
	if blk.Marker != "" {
		t.Fatalf("the disk marker must not be claimed when a diagnostic is present, got %q", blk.Marker)
	}
	if !strings.Contains(blk.Report(), "the build step refused") {
		t.Fatalf("the code-failure report must keep its own sentence:\n%s", blk.Report())
	}
}

// TestBuildBlockerKeepsAnOrdinaryCompileFailure pins the OTHER half: a build
// that fails for a real reason, with no disk phrase anywhere, is unchanged.
func TestBuildBlockerKeepsAnOrdinaryCompileFailure(t *testing.T) {
	blk := buildBlocker(t.TempDir(), "make: *** [Makefile:42: build] Error 1\n")
	if blk.Kind != BlockerKindBuild {
		t.Fatalf("an ordinary build failure must stay %q, got %q", BlockerKindBuild, blk.Kind)
	}
}

// TestBlockerDecisiveLines_DiskKindNamesTheRefusal pins the extractor for the new
// kind: the decisive line is the one carrying the marker phrase, from the SHARED
// vocabulary (scratch.DiskExhaustionMarkers) rather than a second copy of it.
func TestBlockerDecisiveLines_DiskKindNamesTheRefusal(t *testing.T) {
	blk := &Blocker{
		Kind:   BlockerKindDiskExhaustion,
		Marker: "no space left on device",
		Words: "go: downloading example.com/x | " +
			"# github.com/nick/gopi/internal/devworkspace | " +
			"link: writing /home/nick/workspace/x/a.out: no space left on device | " +
			"make: *** [Makefile:42: build] Error 1",
	}
	note := blk.DecisiveLines()
	if !note.Found() {
		t.Fatalf("the disk extractor recognised nothing: %s", note.Why)
	}
	if !strings.Contains(strings.ToLower(note.Lines[0]), "no space left on device") {
		t.Fatalf("the first decisive line must be the filesystem refusal, got %q (all: %v)", note.Lines[0], note.Lines)
	}
}
