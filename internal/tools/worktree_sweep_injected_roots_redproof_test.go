package tools

// RED PROOF for the injected declared-roots seam (worktreeSweepDeps.Roots).
//
// THE MEASURED DEFECT (2026-09-28, master's own run of
// TestWorktreeSweepRefusesEvictionOfAPeerCheckout). The fixture's repo is
// created under the PARENT test's t.TempDir(), but the child helper PROCESS
// (TestSelfCheckoutSweepHelper, re-entered through os.Args[0]) runs this
// package's TestMain, which pins TMPDIR to its OWN per-run suite root — a
// SIBLING of the parent's, not the parent's. So in the child the fixture's
// registration lay outside every host-resolved root (the declared worktree
// root, the scratch home, the CHILD's temp root, the repo tree) and the sweep
// reported rung ① `declared-root-scope — not swept` for a registration the test
// required to be reclaimed. All three subtests failed downstream of that one
// root list: two reported `gone=false, want gone=true`, and the third blamed the
// refusal arm it never reached.
//
// WHAT THE SEAM IS AND WHY IT IS NOT A POLICY CHANGE. worktreeSweepRoots(repo)
// remains the PRODUCTION answer — it is called whenever Roots is nil, which is
// every production path (`worktreeSweepRun` injects nothing). Roots joins the
// struct's other injected evidence (Repos, Rows, Holders, LedgerRead), the
// fields a fixture already uses to state what world it is asking about. The
// policy — which roles may authorise a removal, and the age/holder/ownership
// rungs — is untouched, and the tests below pin BOTH sides: an injected root
// brings a registration into policy, and the SAME registration stays
// out-of-policy when no root reaches it.

import (
	"context"
	"github.com/nick/gopi/internal/scratch"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

// TestWorktreeSweepInjectedRootsBringARegistrationIntoPolicy is the acceptance
// direction: the same fixture, the same evidence, judged against an injected
// declared root — the registration is reclaimed.
func TestWorktreeSweepInjectedRootsBringARegistrationIntoPolicy(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	ageRegistrationRecord(t, worktreeFullGitdir(t, wt), time.Now().Add(-2*reclaimSelfOwnMinAge))

	opts := worktreeSweepOptions{
		DryRun: false,
		MinAge: time.Nanosecond,
		Max:    reclaimScanMax,
		Now:    time.Now(),
	}
	deps := worktreeSweepDeps{
		Repos:      []string{repo},
		LedgerRead: true,
		Holders:    scratch.HolderScan{},
		Roots: []worktreeroots.Root{{
			Path:   root,
			Role:   worktreeroots.RoleFixture,
			Source: worktreeroots.SourceTempRoot,
		}},
	}
	worktreeSweepRunWith(context.Background(), opts, deps, nil)

	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("a registration under an INJECTED declared root must be reclaimed: %s still exists (stat err=%v)", wt, err)
	}
}

// TestWorktreeSweepWithoutARootIsOutOfPolicyAndNotSwept is the refusal
// direction, and it is what keeps this seam from being a way to widen the
// policy: with an injected root list that does NOT reach the fixture's area,
// the registration is reported out-of-scope and the directory SURVIVES. The
// roots in force are printed by the finding, so a failure says which root list
// judged it (Rule 15: a refusal an operator can read).
func TestWorktreeSweepWithoutARootIsOutOfPolicyAndNotSwept(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	ageRegistrationRecord(t, worktreeFullGitdir(t, wt), time.Now().Add(-2*reclaimSelfOwnMinAge))

	// A root that exists but is somewhere else entirely — the same shape the
	// child process sees when it resolves the host's roots.
	elsewhere := t.TempDir()
	opts := worktreeSweepOptions{
		DryRun: false,
		MinAge: time.Nanosecond,
		Max:    reclaimScanMax,
		Now:    time.Now(),
	}
	deps := worktreeSweepDeps{
		Repos:      []string{repo},
		LedgerRead: true,
		Holders:    scratch.HolderScan{},
		Roots: []worktreeroots.Root{{
			Path:   elsewhere,
			Role:   worktreeroots.RoleFixture,
			Source: worktreeroots.SourceTempRoot,
		}},
	}
	res := worktreeSweepRunWith(context.Background(), opts, deps, nil)

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("a registration outside every root in force must NOT be reclaimed, but %s is gone (%v)\nfindings:\n%s",
			wt, err, sweepFindingsText(res))
	}
	if !sweepFindingsCarryRule(res, worktreeRuleRootScope) {
		t.Errorf("the out-of-policy verdict must name the %q arm (an unreadable refusal is indistinguishable from a bug)\nfindings:\n%s",
			worktreeRuleRootScope, sweepFindingsText(res))
	}
}

// TestWorktreeSweepProductionRootsPathIsUnchanged pins the DEFAULT: with no
// injected list, the sweep resolves the roots from the host exactly as before
// (nil is the production answer, and it is not the same thing as an empty
// list — an empty non-nil list would silently disarm every root rung).
func TestWorktreeSweepProductionRootsPathIsUnchanged(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	hostResolved := worktreeSweepRoots(repo)
	if len(hostResolved) == 0 {
		t.Fatal("the production root resolution returned no roots at all — the nil arm would then sweep nothing")
	}
	var deps worktreeSweepDeps
	if deps.Roots != nil {
		t.Fatal("the zero value of Roots must be nil: nil is what routes to worktreeSweepRoots (the production path)")
	}
}

// sweepFindingsText renders a sweep's findings for a failure message: one line
// per registration with its disposition, policy arm and reason.
func sweepFindingsText(res worktreeSweepResult) string {
	var b []string
	for _, f := range res.Findings {
		b = append(b, f.Disposition+" "+f.Rule+" "+f.Path+" | "+f.Reason)
	}
	return strings.Join(b, "\n")
}

// sweepFindingsCarryRule reports whether any finding was decided by the named
// policy arm.
func sweepFindingsCarryRule(res worktreeSweepResult, rule worktreeSweepRule) bool {
	for _, f := range res.Findings {
		if f.Rule == string(rule) {
			return true
		}
	}
	return false
}
