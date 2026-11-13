package tools

// own_worktree_release_subject_e2e_test.go — the REAL RUN of the production
// entry point, with BOTH poles dirty and one of each kind, on a device that is
// not `/` (Rule 22).
//
// WHAT THIS ADDS TO own_worktree_ownership_test.go. That file judges the
// resolver and the per-repo walk; this one drives the PUBLIC entry point the
// close-out calls (ReclaimSelfOwnRegisteredWorktrees) through its own reader and
// renders every line through the producer seam's shape, so the acceptance
// question is answered on exactly the string an operator sees:
//
//	① the registration NO row owns — dirty, aged past every floor — appears with
//	   NO `task <id>:` prefix and NO row id anywhere (the measured defect);
//	② the row's OWN ground — also dirty, also kept — appears WITH the prefix and
//	   with the sentence naming it as this close-out's row.
//
// WHY BOTH ARE DIRTY. Clean ones would be REMOVED (by the ordinary ladder inside
// a declared root, by the out-of-root disposal arm outside one), so no report
// line would exist to judge — measured while this file was written. The dirt is
// also the incident's own shape: the 2026-10-27 line was a DIRTY checkout, and
// "a human should look at this" is exactly the observation Rule 15 forbids
// dropping. Both registrations are therefore kept and both are reported; only
// the SUBJECT differs, which is the whole property under test.
//
// WHY THE AGES ARE BACKDATED. Both floors (reclaimSelfOwnMinAge inside a root,
// reclaimSelfOwnQuarantineAge outside one) would otherwise answer "younger than
// the floor" before the pass ever looked at the directory — a true refusal, but
// not the pole this test is about. The backdate is applied to the ONE file
// worktreeSweepCreatedAt reads (the registration's gitdir HEAD), never to the
// directory's own mtime, because the pass measures the git-internal record.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// ownSubjectE2EWorktreeAge is how far back the two fixture registrations are
// dated: past reclaimSelfOwnMinAge AND past reclaimSelfOwnQuarantineAge, so no
// floor (inside or outside a declared root) can answer first.
const ownSubjectE2EWorktreeAge = 30 * 24 * time.Hour

// ownSubjectE2EAgeRegistration backdates the ONE record worktreeSweepCreatedAt
// reads — the registration's gitdir HEAD — by d, so the age floor cannot decide
// the line before the dirty rung does.
func ownSubjectE2EAgeRegistration(t *testing.T, worktree string, d time.Duration) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(worktree, worktreeSweepGitdirName))
	if err != nil {
		t.Fatalf("read %s/%s: %v", worktree, worktreeSweepGitdirName, err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), finishCleanupGitDirFilePrefix))
	if gitdir == "" {
		t.Fatalf("%s carries no gitdir backlink", worktree)
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	// EVERY candidate the reader consults, in its own fixed order
	// (worktreeSweepCreatedAt: `gitdir`, `HEAD`, the directory) — backdating only
	// one of them is silently ineffective the moment an earlier one exists, which
	// is exactly how this helper first failed (the line still read "younger than
	// the 5m0s floor (1s)" while the test passed on the weaker assertion).
	stamp := time.Now().Add(-d)
	aged := 0
	for _, name := range []string{worktreeSweepGitdirFileName, worktreeSweepHeadName, ""} {
		if err := os.Chtimes(filepath.Join(gitdir, name), stamp, stamp); err == nil {
			aged++
		}
	}
	if aged == 0 {
		t.Fatalf("backdate %s: none of the creation records could be re-dated", gitdir)
	}
}

// TestCloseoutReleaseSubjectOnADirtyPair is the acceptance run: the PUBLIC pass,
// one own ground and one unowned registration, both dirty, both kept, and only
// the subject separating their report lines.
func TestCloseoutReleaseSubjectOnADirtyPair(t *testing.T) {
	repo, unowned, taskID := ownGroundLedgerFixture(t)
	recorded := filepath.Join(filepath.Dir(repo), ownGroundFixtureRecordedGround)

	// Rule 22: the fixture must LIVE off the `/` device. Asserted by measurement
	// (device id), never by the path's spelling, and logged so the report's
	// evidence names what was measured.
	if err := scratch.AssertPathOffRootFS(repo); err != nil {
		t.Fatalf("the fixture repo is not off the `/` device: %v", err)
	}
	t.Logf("fixture device check passed for %s (Rule 22: not the `/` device)", repo)

	// BOTH poles dirty — the incident's shape, and the condition under which a
	// registration is KEPT (hence reported) instead of removed.
	for _, dir := range []string{recorded, unowned} {
		if err := os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("work in progress"), 0o644); err != nil {
			t.Fatalf("dirty %s: %v", dir, err)
		}
		ownSubjectE2EAgeRegistration(t, dir, ownSubjectE2EWorktreeAge)
	}

	// THE PRODUCTION ENTRY POINT — not the per-repo walk, not the resolver.
	lines := ReclaimSelfOwnRegisteredWorktrees()
	if len(lines) == 0 {
		t.Fatalf("the pass reported nothing — with two aged, dirty registrations of a repo the ledger names, silence is a defect (Rule 15)")
	}

	var ownLine, unownedLine string
	for _, l := range lines {
		switch {
		case strings.Contains(l, unowned):
			unownedLine = l
		case strings.Contains(l, recorded):
			ownLine = l
		}
	}
	if unownedLine == "" {
		t.Fatalf("the pass said nothing about the unowned registration %s:\n%s", unowned, strings.Join(lines, "\n"))
	}
	if ownLine == "" {
		t.Fatalf("the pass said nothing about the row's own ground %s:\n%s", recorded, strings.Join(lines, "\n"))
	}

	// ① THE UNOWNED POLE — the measured defect, asserted as the operator sees it.
	rendered := taskRunOwnReleaseLineForTest(taskID, unownedLine)
	if strings.Contains(rendered, "task "+taskID+": ") {
		t.Fatalf("the unowned registration was printed under the closing row's prefix:\n%s", rendered)
	}
	if strings.Contains(rendered, taskID) {
		t.Fatalf("the unowned line names the closing row (%s) anywhere:\n%s", taskID, rendered)
	}
	if !strings.HasPrefix(rendered, "own-worktree-release: "+ReclaimSelfOwnUnownedLead) {
		t.Fatalf("the unowned line must open with the neutral lead %q:\n%s", ReclaimSelfOwnUnownedLead, rendered)
	}
	// Rule 15: the FACT survives — the dirt is still reported with its reason.
	if !strings.Contains(rendered, reclaimSelfOwnKeptMarker) || !strings.Contains(rendered, unowned) {
		t.Fatalf("the unowned observation was dropped (marker + path must survive):\n%s", rendered)
	}

	// ② THE OWN POLE — the positive half: the row id AND a sentence saying this
	// is THAT row's ground (not merely "some row is closing out").
	ownRendered := taskRunOwnReleaseLineForTest(taskID, ownLine)
	if !strings.HasPrefix(ownRendered, "task "+taskID+": ") {
		t.Fatalf("the row's OWN ground lost its row prefix:\n%s", ownRendered)
	}
	if !strings.Contains(ownRendered, "this close-out's row "+taskID) {
		t.Fatalf("the own line must state that it is THIS row's ground:\n%s", ownRendered)
	}
	if strings.Contains(ownRendered, ReclaimSelfOwnUnownedLead) {
		t.Fatalf("the row's OWN ground carried the neutral unowned lead:\n%s", ownRendered)
	}

	// Both directories must still EXIST: a dirty one is a human's decision, and
	// this pass never makes it (the removal is the guard ladder's, unchanged).
	for _, dir := range []string{recorded, unowned} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("a dirty registration was removed (%s): %v — the ownership verdict decides the WORDING, never the disposition", dir, err)
		}
	}

	t.Logf("own pole:      %s", ownRendered)
	t.Logf("unowned pole:  %s", rendered)
}
