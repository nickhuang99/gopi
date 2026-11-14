package testutil

// THE DURABILITY PROOF (the epic's acceptance #2): the gate must be green on
// the updated tip INCLUDING after two successive master advances, where each
// round's new teaching document was produced through the WRITER — never
// hand-annotated afterwards.
//
// WHY THIS IS A REAL FIXTURE AND NOT A RE-RUN OF THE SAME ASSERTION: the whole
// defect was that the offender set is whatever the CURRENT tip contains, so a
// fix that is "annotate everything today" buys exactly one round. What has to
// be shown is the ROUND: a new document arrives (the way a merge brings one —
// bytes on disk, produced by the tool), and the gate's own decision over the
// whole repository tree is still clean, twice in a row.
//
// The fixture therefore does NOT write into the repository: it builds a
// WORKING COPY of the audited trees (git-tracked files only, via `git
// ls-files`) in a scratch root, then replays two rounds against that copy.
// The copy is what makes the two properties honest at the same time —
//
//	the walk sees the REAL corpus (hundreds of real documents, the real
//	dismiss table, the real needles), so a green means the gate is green over
//	the tree that actually reddens it, and
//	nothing in the repository is mutated, so the proof cannot leave the very
//	offence it is proving absent.
//
// Round N creates devdoc/round-N-report.md through the REAL write tool with a
// launch shape in it, asserts (before the verdict) that the file on disk does
// mention one, and then asks the gate's decision about the copy. Both rounds
// must report zero uncovered files. A round that fails would print exactly the
// file a merge round would have reddened on.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/workerownership"
)

// wodRounds is how many successive merge advances the proof replays. Two is
// the acceptance's demand; the third is cheap and catches a fix that merely
// alternates.
const wodRounds = 3

// wodWorkingCopy stages every git-tracked file of the audited trees under dst,
// so the gate's walk meets the real corpus without the repository being
// written to. Untracked files are skipped: a merge lands TRACKED bytes.
func wodWorkingCopy(t *testing.T, dst string) {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z",
		workerownership.DirDevdoc, workerownership.DirBriefings, workerownership.DirScripts).Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	staged := 0
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Fatalf("read %s: %v", rel, readErr)
		}
		dstPath := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dstPath), err)
		}
		if err := os.WriteFile(dstPath, raw, 0o644); err != nil {
			t.Fatalf("stage %s: %v", rel, err)
		}
		staged++
	}
	if staged < workerOwnershipMinScannedFiles {
		t.Fatalf("the working copy holds only %d audited files — the walk would be vacuous, so the proof would be too", staged)
	}
}

// TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds replays the
// rounds: a document teaching the shape is WRITTEN (not hand-annotated) into
// the audited tree each round, and the gate's decision over the whole corpus
// must be clean after each one.
func TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds(t *testing.T) {
	copyRoot := TestScratchDir(t, "worker-ownership-rounds")
	wodWorkingCopy(t, copyRoot)

	for round := 1; round <= wodRounds; round++ {
		rel := workerownership.DirDevdoc + "/" + "merge-round-" + string(rune('0'+round)) + "-report.md"
		path := filepath.Join(copyRoot, filepath.FromSlash(rel))
		teaching := "# Merge round " + string(rune('0'+round)) + " report\n\n" +
			"This round's operator move is `" + workerownership.Needles[0] + "`; an agent submits.\n"

		// The round's document arrives the way a merged one does: written by
		// the writer (the tool the epic put the pointer into). NOTHING from
		// here on touches the bytes by hand.
		if err := tools.Write(path, teaching); err != nil {
			t.Fatalf("round %d: write the round's document: %v", round, err)
		}
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("round %d: read back: %v", round, err)
		}
		// Premise first: the round's document really teaches the shape, so a
		// green below cannot come from a document that never triggered.
		if !workerownership.MentionsLaunchShape(string(onDisk)) {
			t.Fatalf("round %d: the round's document does not mention a launch shape — vacuous round:\n%s", round, onDisk)
		}

		missing, err := workerOwnershipUncovered(copyRoot)
		if err != nil {
			t.Fatalf("round %d: gate decision: %v", round, err)
		}
		if len(missing) != 0 {
			t.Fatalf("round %d: the gate is RED on the tip after %d advance(s) — the write-time pointer did not carry the round (uncovered: %v)",
				round, round, missing)
		}
	}

	// NON-VACUITY of the whole fixture: with the writer's annotations stripped
	// from the ROUND documents (simulating the pre-fix world, where nothing
	// added them), the same decision must redden on every one of them. Without
	// this, all the rounds above could have been green because the walk never
	// looked.
	for round := 1; round <= wodRounds; round++ {
		rel := workerownership.DirDevdoc + "/" + "merge-round-" + string(rune('0'+round)) + "-report.md"
		path := filepath.Join(copyRoot, filepath.FromSlash(rel))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read round %d: %v", round, err)
		}
		var kept []string
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, workerownership.Marker) {
				continue
			}
			kept = append(kept, line)
		}
		if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
			t.Fatalf("strip round %d: %v", round, err)
		}
	}
	missing, err := workerOwnershipUncovered(copyRoot)
	if err != nil {
		t.Fatalf("gate decision after stripping: %v", err)
	}
	if len(missing) != wodRounds {
		t.Fatalf("stripping the writer's annotations left %d uncovered file(s), want exactly the %d round documents — the walk or the needles are not seeing what the rounds wrote: %v",
			len(missing), wodRounds, missing)
	}
}
