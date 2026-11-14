package devworkspace

// The REAL path, end to end: a clone-channel delivery attempt whose DECLARED
// build emits the kernel's own ENOSPC text. Everything here is the shipped code
// — `Open` clones a real origin, `AttemptDelivery` runs the real
// clean → pull → build loop through `/bin/sh`, and the blocker that comes back
// is the one a row would receive. The unit tests in disk_exhaustion_test.go pin
// the classifier; this one proves the classifier is WIRED to the build step.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAttemptDeliveryClassifiesAnENOSPCBuildAsDiskExhaustion is Rule 31's
// evidence at the channel's own surface: real git, real subprocess, real words.
func TestAttemptDeliveryClassifiesAnENOSPCBuildAsDiskExhaustion(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	// The declared build prints the kernel's text and fails, which is exactly
	// what the linker did on this host when the partition hit 2.3 GiB free.
	cfg.Build = "echo 'link: writing /home/user/workspace/x/a.out: no space left on device'; exit 1"
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a filesystem refusal is a BLOCKER, not an error: %v", err)
	}
	if blk == nil {
		t.Fatalf("want a blocker, got none (%+v)", res)
	}
	if blk.Kind != BlockerKindDiskExhaustion {
		t.Fatalf("want kind %q for an ENOSPC build, got %q (words: %s)",
			BlockerKindDiskExhaustion, blk.Kind, blk.Words)
	}
	if blk.Ground != dir {
		t.Fatalf("the blocker must name the GROUND it measured, got %q want %q", blk.Ground, dir)
	}
	// THE RAW TEXT, said out loud on every run: this is the sentence a row will
	// carry, and a reader reproducing the fix needs it verbatim (Rule 31).
	t.Logf("REAL BLOCKER REPORT (kind=%s marker=%q):\n%s", blk.Kind, blk.Marker, blk.Report())
	report := blk.Report()
	for _, want := range []string{
		"the FILESYSTEM, not by the code",
		"NEVER LINKED",
		"gopi tool scratch-gc gocache",
		"gopi tool scratch-gc gocache --trim",
		blk.Ground,
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report must carry %q:\n%s", want, report)
		}
	}
	// A FAILED BUILD MUST NEVER REACH ORIGIN, at any kind.
	if out := gitIn(t, origin, "log", "--oneline", DefaultBranch); strings.Contains(out, "deliverable work") {
		t.Fatalf("a workspace that failed its build must not be pushed:\n%s", out)
	}
}
