package tools

// rebuild_pending.go — the DURABLE half of the Rule-12 close loop: a repo whose
// master advanced but whose binary could not be brought along YET leaves a mark
// that the NEXT merger pass drains.
//
// THE DEFECT THIS CLOSES (measured 2026-09-16, epic/verify 01a0a72c,
// branch fix/merge-rebuild-loop):
//
//	serveRebuildAfterMerge is the close loop's LAST step, and on the merge-verify
//	gate's path it runs AFTER the gate has already landed the branch and AFTER a
//	verify child the merger BLOCKS on — a child measured in tens of minutes
//	(the 07:2x round: baseline sweep 06:21 → kill 07:17, 56 minutes). A pass
//	interrupted inside that window (an operator `merger --restart`, a SIGKILL)
//	leaves master advanced and bin/gopi at the PREVIOUS position, and nothing
//	ever comes back for it: the next pass finds `merged=0` (the branch is gone),
//	so the per-round rebuild hook does not fire, and adoption re-adopts the
//	already-merged branch as a no-op. The close loop is therefore only as durable
//	as the process's willingness to survive to its own last step.
//
//	Evidence of the two variants the same day:
//	  (a) INTERRUPTED PASS — the gate landed feat/merger-stop-immediate at
//	      07:29:34 while the 06:35-era daemon (state file merger-state-2470012,
//	      `merged:2`, stage=verify, children=[verify-child 01a0a72c pid 2760166])
//	      was inside mergerVerifyGate; that daemon never logged again. bin/gopi
//	      stayed at the pre-merge 07:17:53 build, older than the merged source —
//	      the operator's symptom. No later round happened to carry a sweep
//	      landing, so `[rebuild]` lines resumed only ~4h later (11:44) and the
//	      07:29 position was never named or built.
//	  (b) JUDGED-BEFORE-BUILT — the same shape with a SURVIVING pass: the gate
//	      landed a merge at 13:10:48 and spawned the judge at 13:19:35, LEAVING
//	      bin/gopi (13:10:31) older than the merged code while the verify child
//	      judged the merged state and the operator's ./bin/gopi ran pre-merge
//	      code.
//
// THE MECHANISM: mark the repo the moment its binary is KNOWN to be behind
// (a merge just landed on a path whose close loop cannot run before the judge /
// before an interruption window), and DRAIN the mark in the close loop itself.
// The mark is a FILE at a stable path, so it survives the process: a successor
// starts its first pass, sees the mark and rebuilds. The close loop thus closes
// across process lifetimes, which is what an INVARIANT means when the process
// that lands the work is not guaranteed to be the process that finishes it.
//
// WHERE THE MARK LIVES: the merger's own control directory
// (MergerGracefulDir, i.e. <gopi data root>/merger) — already GOPI_HOME aware,
// already the merger's per-instance state area, so a sandboxed test isolates it
// with GOPI_HOME and the operator finds it beside the state files it belongs to.
// One file per repo, named by a digest of the repo's absolute path: two repos
// sharing a leaf name must not share a mark (the same collision the build-cache
// key reasons about), and trimming keeps the name filesystem-safe.
//
// NO CONTRACT, NO MARK: a repo with no declared build (docs-only / pure merge
// target) can never be rebuilt, so marking it would leave a file that no drain
// can ever clear. MarkRebuildPending refuses in that case and says nothing (the
// same deliberate silence serveRebuild keeps for an undeclared build).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
)

const (
	// RebuildPendingPrefix names one repo's pending-rebuild mark:
	// "rebuild-pending-<repo-key>".
	RebuildPendingPrefix = "rebuild-pending-"

	// rebuildPendingRepoKeyHexLen is the digest prefix length of the repo key.
	// 16 hex chars (64 bits) is far beyond the collision reach of the handful
	// of repos a host configures, and short enough to read in a directory
	// listing.
	rebuildPendingRepoKeyHexLen = 16

	// rebuildPendingDrainLog is the log prefix for every drain line, so the
	// operator can grep ONE tag for "the close loop caught up".
	rebuildPendingDrainLog = "[merger] [rebuild] pending"

	// MarkRebuildAfterLandingReason is the reason a LANDING records on the mark.
	//
	// The landing is the exact moment master advances, and the rebuild that must
	// follow it is a LATER step of the same tick (the per-repo sweep reaches it
	// only after the whole branch surface has been decided) — so the gap between
	// the two is the window a pass can die in. Naming the reason here, once,
	// keeps it a value with a single definition (Rule 1b) and makes a mark found
	// months later answer "which landing left this?" by itself.
	//
	// A landing is reachable from the merger TICK, so this path may never spawn,
	// wait or call a model (the tick is pure git — see
	// cmd/agent/merger_tick_no_child_gate_test.go): it is a single small file
	// write in the merger's own control directory, and nothing more.
	MarkRebuildAfterLandingReason = "a branch fast-forwarded into master while the rebuild is a WIP step of the same tick"
)

// rebuildPendingRepoKey is the per-repo component of a mark's filename: a
// short, filesystem-safe digest of the repo's absolute path. A digest (not the
// basename) because two repos on a host routinely share a leaf — /srv/a/tool
// and /srv/b/tool — and a shared leaf would make one repo's mark drain
// another's rebuild.
func rebuildPendingRepoKey(repo string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(repo)))
	return hex.EncodeToString(sum[:])[:rebuildPendingRepoKeyHexLen]
}

// RebuildPendingDir is the directory holding the marks — the merger's control
// directory, resolved by the single owner of that path (Rule 2: the resolver is
// MergerGracefulDir, never a second spelling).
func RebuildPendingDir() (string, error) {
	return MergerGracefulDir()
}

// rebuildPendingPath is the mark path for repo.
func rebuildPendingPath(repo string) (string, error) {
	dir, err := RebuildPendingDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, RebuildPendingPrefix+rebuildPendingRepoKey(repo)), nil
}

// MarkRebuildPending records that repo's master advanced while its binary could
// not be brought along yet, so a later pass (this process's, or a successor's
// after an interruption) rebuilds it.
//
// It is deliberately IDEMPOTENT and CHEAP: a re-mark rewrites the same file, so
// the newest reason wins. A repo with NO declared build is not marked at all
// (see the file header). Failure to write is REPORTED, never swallowed
// (Rule 15b) — a mark nobody can see is the same staleness this file exists to
// end.
func MarkRebuildPending(repo, reason string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return
	}
	// No contract ⇒ no drain can ever clear this mark ⇒ never write one.
	if _, found, err := config.ResolveRepoBuild(repo); err != nil || !found {
		return
	}
	path, err := rebuildPendingPath(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s mark FAILED for %s: %v\n", rebuildPendingDrainLog, repo, err)
		return
	}
	// The FIRST field is the repo path itself: the digest in the filename is
	// one-way, so the mark must carry the repo it rebuilds (see
	// RebuildPendingRepos). The reason follows after a tab, so it can never be
	// mistaken for the path.
	body := repo + "\t" + strings.TrimSpace(reason) + "\n"
	// 0o644 like every other control record: the merger and the operator's
	// shell are the same uid on a task host, and a mark is not a secret.
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%s mark FAILED for %s: %v\n", rebuildPendingDrainLog, repo, err)
		return
	}
	fmt.Fprintf(os.Stderr, "%s marked %s: %s\n", rebuildPendingDrainLog, repo, strings.TrimSpace(reason))
}

// ClearRebuildPending removes repo's mark — the DRAIN's success path: the binary
// now follows the tip the mark described, so the obligation is discharged.
// A missing mark is not an error: every successful build clears, including
// builds of repos that were never marked (an ordinary sweep landing). A failure
// to remove is reported — a mark left behind would rebuild the repo on every
// pass, and the operator should know why.
func ClearRebuildPending(repo string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return
	}
	path, err := rebuildPendingPath(repo)
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "%s clear FAILED for %s: %v\n", rebuildPendingDrainLog, repo, err)
	}
}

// RebuildPendingRepos returns the repos that carry a mark right now, as absolute
// paths recovered from the marks' own content (the digest names the file, the
// body's first line names the repo — see markRebuildPending's writers). Reading
// the path back from the body rather than reversing the digest is deliberate:
// a digest is one-way, so storing the repo it keys is the only faithful
// recovery.
//
// The read is defensive: an unreadable or empty mark is skipped with a log line
// rather than failing the whole pass (Rule 15b — reported, never silent, never
// fatal: the close loop's job is to rebuild what it can, not to be stopped by
// one bad file).
func RebuildPendingRepos() []string {
	dir, err := RebuildPendingDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "%s scan FAILED for %s: %v\n", rebuildPendingDrainLog, dir, err)
		}
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), RebuildPendingPrefix) {
			continue
		}
		repo := repoFromRebuildPendingMark(filepath.Join(dir, e.Name()))
		if repo == "" {
			continue
		}
		out = append(out, repo)
	}
	return out
}

// repoFromRebuildPendingMark reads the repo path out of one mark file. The
// first line is the repo path; the reason (if any) follows on the same line
// after a tab, so the reason can never be mistaken for the path.
func repoFromRebuildPendingMark(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s read FAILED for %s: %v\n", rebuildPendingDrainLog, path, err)
		return ""
	}
	first := raw
	if i := strings.IndexByte(string(raw), '\n'); i >= 0 {
		first = raw[:i]
	}
	repo := string(first)
	if i := strings.IndexByte(repo, '\t'); i >= 0 {
		repo = repo[:i]
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		fmt.Fprintf(os.Stderr, "%s mark %s carries no repo — skipping (a mark must name what it rebuilds)\n",
			rebuildPendingDrainLog, path)
	}
	return repo
}
