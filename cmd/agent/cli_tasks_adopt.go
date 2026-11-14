package main

// Serve-tick integration for the branch adoption chain
// (internal/tools/task_adoption.go): a pass STEP riding runTaskServe — no new
// daemon (M4.5 ⑦ no-daemon posture). (It used to say "like serveStaleSweep";
// that wrapper was deleted 2026-10-30 with the task-lifecycle pass, and the
// stale report itself was deleted 2026-11-05 — a stale row is READ as stale by
// `gopi task list` / `gopi task show`, never reported by another process.) Discovery is
// stateless (git for-each-ref re-derived every pass); ⑤ dedup lives in the
// ledger (adoptionConflictTaskLive), the clock throttle just bounds cost.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// adoptionIntervalEnv gates the serve-tick sweep: seconds, absent = the
	// default below, 0 or negative = DISABLED (a repo that merges by hand
	// sets this to silence the chain entirely).
	adoptionIntervalEnv = "GOPI_ADOPTION_INTERVAL"

	// adoptionIntervalDefault matches the stale-sweep cadence (15min): two
	// serve passes inside the window share one sweep.
	adoptionIntervalDefault = 15 * time.Minute

	// adoptionSubmitDisabledEnv exists for the report-only serve posture…
	// not introduced: Submit is always true on the serve face (the chain's
	// whole point is auto-dispatch); the CLI `task adopt` face stays
	// report-only unless --submit is passed.
)

// serveAdoptionLastTick is the process-wide throttle clock.
var serveAdoptionLastTick time.Time

// serveAdoptionInterval resolves the sweep cadence (Rule 2): GOPI_ADOPTION_
// INTERVAL seconds; absent = adoptionIntervalDefault; parse-fail or <=0 =
// 0 = disabled (a typo disables rather than misfires).
func serveAdoptionInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv(adoptionIntervalEnv))
	if raw == "" {
		return adoptionIntervalDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// adoptionCleanTree reports whether the repo's worktree is clean. The
// adoption chain refuses a dirty tree: probe merges and ③a commits must
// never entangle with someone else's in-flight work.
func adoptionCleanTree(repo string) (bool, error) {
	out, err := tools.AdoptionGitStatus(repo)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// serveAdoptionSweep runs one adoption pass at most once per resolved
// interval (first pass always sweeps). Disabled (interval 0) or a dirty
// worktree → log-and-skip, never fatal: the serve pass owns the queue, not
// git state.
func serveAdoptionSweep(repo string) {
	// WHICH AXIS DECIDES IT (epic: the five conflated axes, second half): the
	// sweep BELONGS to the code-delivery axis — it lands a peer's pushed-back
	// branch into master, which is what completes the delivery the git pass
	// started. The task it dispatches per adopted branch (Submit: true below)
	// is a CONSEQUENCE of that landing, not the crossing itself. So the
	// task-row-sync switch (config.TaskSyncSettingsKey) does NOT gate it: were
	// the switch consulted here, a declared peer with `task_sync` off would
	// push branches that nobody ever adopts, and the delivery axis would be
	// silently half-on.
	//
	// DECLARED PEER decides it: the sweep is cross-node by construction (every
	// candidate arrives over a fetched peer ref), so with NO peer declared it
	// short-circuits BEFORE the throttle clock advances — the single-node shape
	// is a pure no-op, like the git pass. The role arm is the CLI face's own
	// (`gopi task adopt`), unchanged and never gated.
	if !config.HasSyncPeers() {
		return // single-node: no peer pushes back — nothing to adopt
	}
	interval := serveAdoptionInterval()
	if interval <= 0 {
		return
	}
	now := time.Now()
	if !serveAdoptionLastTick.IsZero() && now.Sub(serveAdoptionLastTick) < interval {
		return
	}
	serveAdoptionLastTick = now
	clean, err := adoptionCleanTree(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[serve] [adopt] skip: %v\n", err)
		return
	}
	if !clean {
		fmt.Println("[serve] [adopt] skipped: worktree dirty (adoption refuses a dirty tree)")
		return
	}
	counts, err := tools.AdoptionSweep(nil, tools.AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[serve] [adopt] failed: %v\n", err)
		return
	}
	fmt.Printf("[serve] [adopt] scanned=%d adopted=%d ref-kept=%d dispatched=%d duped=%d skipped=%d failed=%d\n",
		counts.Scanned, len(counts.Adopted), len(counts.Dispatch), counts.Duped, len(counts.RefKept), counts.Skipped, len(counts.Failed))
	// ref-kept entries are named, never folded into a silent counter: the
	// operator must be able to tell a healthy landing with a kept ref from a
	// real failure (Rule 15b).
	// A candidate whose tip master ALREADY contains gets its own labelled
	// block, never a `failed=` tail: the branch's work is IN master, so the
	// pass owes it no merge and the operator owes it a one-line prune (the
	// line carries the command). Folding it into failed= made a healthy
	// landing read as a broken adoption (2026-09-14).
	for _, a := range counts.AlreadyIntegrated {
		fmt.Printf("[serve] [adopt] already-integrated: %s\n", a)
	}
	for _, r := range counts.RefKept {
		fmt.Printf("[serve] [adopt] ref-kept: %s\n", r)
	}
	// NO VERSION TAG IS MINTED FOR WHAT THIS PASS ADOPTED (epic
	// version-identity, 2026-09-24). mintAdoptionVersionTag used to name the
	// position ③a commits push-back branches to, so a pass whose only landing
	// was adopted work still left a name behind. gopi's identity no longer
	// comes from the checkout, so a landing owes no name here — and writing a
	// `version/*` tag into a peer's repository as a side effect of adopting a
	// branch was never this pass's decision to make.
	for _, f := range counts.Failed {
		fmt.Fprintf(os.Stderr, "[serve] [adopt] %s\n", f)
	}
}

// runTaskAdopt is the `gopi task adopt` CLI face: report-only by default
// (--submit enables the ③b dispatch), so a human can dry-run the chain
// before letting it touch the ledger.
func runTaskAdopt(repo string, submit bool, master string) {
	// Single-instance guard: the adoption chain's ③a half COMMITS the
	// probe-merged adoptions onto master, so this face is a merge writer like
	// the merger itself (epic merger-always-flock).
	defer mergerWorkLock(adoptFaceProg)()
	counts, err := tools.AdoptionSweep(nil, tools.AdoptionOptions{
		Repo: repo, Submit: submit, MasterBranch: master,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task adopt: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("scanned=%d adopted=%d ref-kept=%d dispatched=%d duped=%d skipped=%d failed=%d\n",
		counts.Scanned, len(counts.Adopted), len(counts.Dispatch), counts.Duped, len(counts.RefKept), counts.Skipped, len(counts.Failed))
	for _, r := range counts.RefKept {
		fmt.Printf("  ref-kept: %s\n", r)
	}
	for _, b := range counts.Adopted {
		fmt.Printf("  adopted:  %s\n", b)
	}
	for _, a := range counts.AlreadyIntegrated {
		fmt.Printf("  already-integrated: %s\n", a)
	}
	for _, b := range counts.Dispatch {
		fmt.Printf("  dispatched: %s\n", b)
	}
	for _, f := range counts.Failed {
		fmt.Fprintf(os.Stderr, "  failed: %s\n", f)
	}
}
