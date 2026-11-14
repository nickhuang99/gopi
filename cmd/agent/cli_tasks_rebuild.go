package main

// Rebuild-after-merge hook (epic 2026-09-02): the merger pass's LAST step —
// after adoption/merge-backlog/executor land ≥1 branch into master, the
// repo's declared build runs so the deployed binary follows master (the
// close loop's final step; the recurring stale-bin incidents were master
// advancing without the bin). One helper called from every merge-round
// end:
//
//	① the retired merger tick (runTaskMerger — deleted 2026-09-19, see below);
//	② the manual `gopi task merge` face (runTaskMerge).
//
// Contract: the build command is DECLARED in settings.json "repos"
// (internal/tools/rebuild.go, RebuildAfterMerge). A repo with no entry is
// skipped silently — docs-only repo, or a repo whose build isn't declared
// (no error, no guess). Failure is a notify event (build-failed), never a
// rollback: the merge is done and good — git history is fine.
//
// A DIRTY WORKTREE is judged by TRACKED modifications only (epic 2026-09-12,
// tools.AdoptionGitStatusTracked): the repo has to be dirty in a way a clean
// rebuild can represent, i.e. the build must not pick up someone's
// in-flight tracked edits. UNTRACKED files do NOT defer the rebuild — a busy
// task field permanently holds untracked artifacts (briefings/, logs, other
// agents' new files), and before the split the untracked-inclusive predicate
// never came back empty, so this guard skipped the Rule-12 close loop on
// EVERY pass and bin/gopi stayed on a pre-merge master forever. Genuine
// tracked dirt still defers (the bin would come out -dirty and fail the
// version assertion on every pass).
//
// The adoption chain keeps the STRICT (untracked-inclusive)
// tools.AdoptionGitStatus — probe merges and ③a commits must not entangle
// with untracked in-flight work. The split is the whole point of the epic:
// two callers, two explicit contracts, neither silently redefined.

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// mergerRebuildSkipDirty is the log line for the dirty-tree guard.
const mergerRebuildSkipDirty = "worktree dirty — rebuild deferred to a clean tree (bin stays on the last clean master)"

// rebuildMeasureSuffix renders the ref the close loop's current-ness question
// was asked of, plus the reason when the preferred ref did not answer
// (tools.RebuildResult.ExpectedRef / MeasureNote). It is a SUFFIX on the
// existing ok/FAILED lines rather than a new line of its own: one greppable
// record per rebuild, and an operator reading "already current" can see
// immediately WHICH ref it was current against — the whole point of the fix
// (a skip measured against this checkout's own HEAD must not look identical to
// a skip measured against the integration ref the grounds follow).
func rebuildMeasureSuffix(res *tools.RebuildResult) string {
	if res == nil || res.ExpectedRef == "" {
		return ""
	}
	suffix := " [measured against " + res.ExpectedRef + "]"
	if res.MeasureNote != "" {
		suffix += " " + res.MeasureNote
	}
	return suffix
}

// serveRebuildAfterMerge runs the repo's post-merge rebuild (if declared)
// and logs the outcome. Called at the END of a merger pass — after every
// merge path has run — so a pass that landed branches leaves a binary that
// matches the new master tip. Never fatal: skip/failure are logged (or
// notified for a real failure), the pass result is already committed.
//
// Mechanism 2 (epic merger-failure-dual-mechanism, the operator 2026-09-08): when
// the landing rebuild FAILS and the caller supplied revert (the merger
// sweep's pre-round restore snapshot — non-nil only when this pass's own
// sweep landed the merged commits), master is UNBUILDABLE and the merger
// must not leave it that way ("period" in the operator's verdict). serveRebuild
// then auto-reverts: RestoreMaster unwinds this round's landing back to the
// pre-sweep buildable tip + re-creates the deleted branch refs, and reports
// the revert (notify). revert nil = caller did NOT merge anything of its own
// this round (or no snapshot possible) → the old behavior: log + build-
// failed notify, no revert (never reset a merge the caller didn't land).
func serveRebuildAfterMerge(repo string, revert *tools.FailedMergeRevert) {
	if strings.TrimSpace(repo) == "" {
		repo = tools.GitSyncRepoRoot()
	}
	if repo == "" {
		return // not inside a git checkout — nothing to rebuild
	}
	// NO VERSION TAG IS MINTED HERE (epic version-identity, 2026-09-24). This
	// close loop used to call mintMergeVersionTag → internal/version.MintTag,
	// i.e. it wrote a `version/<stamp>-<slug>-<rev>` tag into the repository
	// on every landing so the rebuilt binary could introduce itself by that
	// name. That coupling is DELETED, not hidden:
	//
	//   - gopi merges ANY git repo; writing a tag into a repository gopi does
	//     not own is the repo owner's release decision, never a side effect of
	//     a merge. 183 `version/*` tags accumulated this way and are left in
	//     place (they are history; see devdoc/version-identity.md § the 183).
	//   - the binary's identity does not come from the checkout any more: it is
	//     internal/version.Version, compiled in, so there is nothing to name
	//     before the build reads it.
	//
	// Rebuilding is what the landing still owes (Rule 12), and it is what this
	// function does.
	//
	// Installed-script refresh (rework of fix/launcher-install-refresh,
	// 2026-09-12) is the OTHER half of this close loop, and it runs on a
	// DIFFERENT axis from the rebuild below: a merge changing
	// scripts/gopi-worker-up.sh must reach the operator's installed copy
	// (~/.local/bin/<name>, reached through ~/bin/<name>) or the merged fix
	// reaches nobody. That is exactly how this branch's merged state failed
	// its post-merge verification — repo copy 98e3ead4… (19138 B, merged
	// 20:31:52) vs installed 4b2ad5a2… (16457 B, 20:22:56), `--check` exit 1
	// STALE.
	//
	// It is deliberately NOT gated on the build contract or the dirty-tree
	// guard (a repo may declare scripts and no build — a pure merge target —
	// and its installed copies drift exactly the same way), but it IS skipped
	// when the rebuild below reverted this round's landing: a revert restores
	// the pre-sweep tip, so refreshing then would faithfully re-install the
	// OLD bytes and invert the very drift this closes.
	reverted := serveRebuild(repo, revert)
	if !reverted {
		serveScriptSync(repo)
	}
}

// mergerPassCloseLoop is the PASS-LEVEL rebuild INVARIANT (epic
// merger-always-rebuilds): after a merger pass has run EVERY step that can
// advance master or spawn a child, each configured repo's binary is brought
// up to its master tip — whatever code path did the landing.
//
// WHY A PASS-LEVEL INVARIANT and not another call site: the per-landing hook
// above is wired where the MERGER'S OWN sweep lands branches, so it is only as
// complete as that inventory. Measured 2026-09-15 (the same tree the operator
// reported: "every time I build gopi it takes a while, so it wasn't rebuilt"):
// serveAdoptionSweep (cli_tasks_adopt.go) ADOPTS branches into master, mints a
// version name for the position (cli_tasks_adopt.go mintAdoptionVersionTag)
// and never rebuilds; the merge-execute face (`gopi task merge-execute` →
// tools.TaskMergeBacklogExecute) merges a branch with neither a rebuild nor a
// mint. A rebuild that exists at N call sites is a rebuild that disappears
// whenever one of them is added without it — the invariant removes the
// dependence on that inventory: master moved ⇒ the binary follows, or the
// reason is logged.
//
// ORDERING (the operator's requirement: "the rebuilding is done after merger
// receives child gopi process success/failure return"): mergerPassOnce arms
// this as a DEFER at the point its repo list is known, so it runs on every
// exit path of the pass and always AFTER the per-repo sweep loop — and the
// sweep loop is exactly where the merger BLOCKS on its spawned children (the
// resolve child and the merge-verify child are started and waited on inside
// mergerSweepRepo). Every child has therefore returned before a byte of the
// rebuild is written.
//
// Why the defer and not one statement after the loop (measured defect, this
// epic's closeout): `gopi merger --once` with an EMPTY repos table returned
// early — right after the adoption sweep, which is a master-advancing step —
// and skipped the rebuild entirely. A rebuild that a `return` can step over is
// not an invariant; arming it at the top makes every exit path carry it.
//
// IDEMPOTENT BY CONSTRUCTION: RebuildAfterMerge probes the artifact first and
// reports "already current" when it already follows the tip, so a pass that
// landed nothing, or a second repo pass, costs one `--version` probe — never a
// build. That is what makes it safe to run unconditionally, which is the whole
// point of an invariant.
//
// LAYERING with the per-landing call: the sweep's own call runs EARLIER, with
// the mechanism-2 revert handle (a failed landing build unwinds the merge the
// sweep just made). This one passes NO handle: it did not land anything of
// its own, so it must never reset a merge it did not initiate — a failure here
// is logged + notified (inside RebuildAfterMerge), never a rollback.
//
// A repo with no DECLARED build is skipped in silence, exactly like
// serveRebuild: docs-only repos and pure merge targets (an empty build entry)
// are valid, and a per-pass log line for each of them is noise, not an audit
// trail.
func mergerPassCloseLoop(repos []string) {
	// DRAIN the durable marks FIRST (internal/tools/rebuild_pending.go): a repo
	// marked by an EARLIER landed merge — this pass's own gate landing, or the
	// landing of a process that was interrupted before its close loop reached
	// this step — is brought current here, in addition to whatever the caller
	// named. The marks are the pass's memory across process lifetimes, so a
	// rebuild interrupted with its lander is recovered by the successor rather
	// than lost.
	for _, repo := range tools.RebuildPendingRepos() {
		serveRebuildAfterMerge(repo, nil)
	}
	for _, repo := range mergerUniqueRepos(repos) {
		_, found, err := config.ResolveRepoBuild(repo)
		if err != nil {
			// Unreadable settings are NOT a silent skip: the invariant could
			// not be evaluated for this repo, and the operator has to know
			// (Rule 15: no silent failure).
			fmt.Fprintf(os.Stderr, "[merger] [rebuild] skip: %v\n", err)
			continue
		}
		// found=false already covers an entry with an EMPTY build (a pure
		// merge target): no contract, nothing to rebuild, and no per-pass
		// log line about it (see ResolveRepoBuild).
		if !found {
			continue
		}
		serveRebuildAfterMerge(repo, nil)
	}
}

// mergerCloseLoopAfterLanding is the close loop of a face that lands into
// master OUTSIDE a merger pass — the manual `gopi task merge` face
// (runTaskMergeRebuild) and the `gopi task merge-execute` face. The two faces
// are the two halves of ONE rule (master advanced ⇒ the binary follows), so
// they reach the SAME builder rather than each writing its own rebuild: this
// is what keeps a landing face from being added without a close loop.
//
// The repo set is the union of the CONFIGURED repos (a landing face may be
// invoked with no `--repo`, and the settings table is the same sweep list the
// merger uses) with the repo the face actually landed in — a `task merge`
// against a checkout that has no settings entry still gets its rebuild. The
// union is de-duplicated because a repo in both lists is ONE physical checkout:
// building it twice per landing doubles the cost for nothing.
//
// ORDERING: the caller invokes this after it has observed every child return
// — for `task merge` that includes the post-landing sweeps that prune the
// merged worktree (mergeRunPostLandingSweeps), the same ordering the armed
// pass invariant has.
func mergerCloseLoopAfterLanding(repo string) {
	repos, err := config.ResolveMergerRepos()
	if err != nil {
		// The settings list is only the UNION's second half; an unreadable
		// settings file must not cost the caller the rebuild of the repo it
		// actually landed in. Reported, never swallowed (Rule 15).
		fmt.Fprintf(os.Stderr, "[merger] [rebuild] skip: configured repos: %v\n", err)
		repos = nil
	}
	mergerPassCloseLoop(append(repos, repo))
}

// mergerUniqueRepos drops blank entries and de-duplicates a repo list,
// preserving order, so one physical checkout is never built twice in one close
// loop (the pass invariant and a landing face can both name the same repo).
func mergerUniqueRepos(repos []string) []string {
	seen := make(map[string]bool, len(repos))
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		if strings.TrimSpace(repo) == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		out = append(out, repo)
	}
	return out
}

// serveRebuild is the build half of the merge-round-end close loop. It
// reports whether it AUTO-REVERTED this round's landing (mechanism 2), so the
// caller can skip the installed-script refresh that must not run against a
// reverted tree.
func serveRebuild(repo string, revert *tools.FailedMergeRevert) (reverted bool) {
	// Contract check FIRST: a repo with no settings "repos" entry is the
	// deliberate silent skip (docs-only repo / undeclared build) — no git
	// probing, no error. One quiet log line for the operator's audit trail.
	_, found, err := config.ResolveRepoBuild(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[merger] [rebuild] skip: %v\n", err)
		return false
	}
	if !found {
		fmt.Printf("[merger] [rebuild] skip: no build contract for %s in settings repos table\n", repo)
		return false
	}
	// Dirty-tree guard (see the file header): a rebuild from a dirty tree
	// yields a -dirty binary that fails the version assertion — skip with
	// a log line instead of a false build-failed alarm every pass.
	tracked, terr := tools.AdoptionGitStatusTracked(repo)
	if terr != nil {
		fmt.Fprintf(os.Stderr, "[merger] [rebuild] skip: %v\n", terr)
		return false
	}
	if strings.TrimSpace(tracked) != "" {
		fmt.Printf("[merger] [rebuild] skip: %s\n", mergerRebuildSkipDirty)
		return false
	}
	// THE VET GATE'S REVERT ARM RUNS FIRST, and the ORDER is the design, not
	// style. A red post-merge `go vet` attributed to THIS round's landing means
	// the tree master now holds does not hold together, and the close loop owns
	// taking it back (internal/tools/rebuild_vet_revert.go's build-vs-vet table
	// argues why that is NOT Rule 12's rollback: a failed BUILD is a statement
	// about the artifact, a red VET is a statement about master's own content).
	// Deciding it before the build is mechanical: restoring master to the
	// pre-merge tip makes building the merged tree meaningless, and a build
	// issued afterwards would stamp a binary at a merge master no longer has.
	if verdict := tools.RebuildAfterMergePrebuild(repo); verdict != nil {
		decision := tools.ClassifyVetGateRevert(verdict, revert)
		switch {
		case decision.Revert && revert != nil && revert.Retriable():
			restored, rerr := tools.RevertIntroducedVetFailure(revert, verdict)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "[merger] [rebuild] vet-gate revert FAILED: master may be left vet-red: %v\n", rerr)
			} else {
				if len(restored) > 0 {
					fmt.Printf("[merger] [rebuild] vet-gate revert: restored %d branch ref(s): %s\n",
						len(restored), strings.Join(restored, ", "))
				}
				fmt.Printf("[merger] [rebuild] vet-gate REVERTED: master restored to pre-merge tip %s (gate failure introduced by the landing, whose work is preserved on its branch)\n",
					revert.PreTip)
				return true
			}
		case decision.Revert:
			fmt.Fprintf(os.Stderr, "[merger] [rebuild] vet-gate: failure is INTRODUCED by this merge but there is no merger-sweep snapshot to unwind — merge left as-is, rework owed\n")
		case decision.Class.Reason != "":
			fmt.Printf("[merger] [rebuild] vet-gate: %s — %s\n", decision.Class.Reason, decision.Class.Detail)
		}
	}

	res := tools.RebuildAfterMerge(repo)
	switch {
	case res.Skipped:
		// No build contract for this repo — the deliberate silent skip
		// (docs-only repo / undeclared build). One quiet log line for the
		// operator's audit trail, no error.
		fmt.Printf("[merger] [rebuild] skip: no build contract for %s in settings repos table\n", repo)
	case res.OK:
		// DRAIN (see internal/tools/rebuild_pending.go): the binary now follows
		// the tip the mark described, so the obligation is discharged. Clearing
		// here — the ONE place every caller's success passes through — is what
		// keeps a mark from surviving a drain and rebuilding every pass forever.
		tools.ClearRebuildPending(repo)
		if res.Elapsed == 0 {
			fmt.Printf("[merger] [rebuild] ok: %s already current at %s (no rebuild needed)%s\n",
				repo, res.ActualVersion, rebuildMeasureSuffix(res))
		} else {
			fmt.Printf("[merger] [rebuild] ok: %s built in %s (%s)%s\n",
				repo, res.Elapsed.Round(0), res.ActualVersion, rebuildMeasureSuffix(res))
		}
	default:
		// Build failure / version mismatch. Mechanism 2 (auto-revert) —
		// see the file header: a landing rebuild FAILURE leaves master
		// UNBUILDABLE; when this round's own sweep landed the merge
		// (revert non-nil), restore master to the pre-sweep buildable tip.
		fmt.Fprintf(os.Stderr, "[merger] [rebuild] FAILED: %s cmd=%q exit=%d err=%s%s\n",
			repo, res.Cmd, res.Exit, res.Err, rebuildMeasureSuffix(res))
		if revert != nil && revert.Retriable() {
			restored, rerr := revert.RestoreMaster()
			if rerr != nil {
				// Restore failed — master stays unbuildable. Log loudly
				// (the build-failed notify already fired inside
				// RebuildAfterMerge); a human/PA must intervene.
				fmt.Fprintf(os.Stderr, "[merger] [rebuild] revert FAILED: master may be UNBUILDABLE: %v\n", rerr)
				return false
			}
			if len(restored) > 0 {
				fmt.Printf("[merger] [rebuild] reverted merge: restored %d branch ref(s): %s\n",
					len(restored), strings.Join(restored, ", "))
			}
			fmt.Printf("[merger] [rebuild] REVERTED: master restored to pre-merge buildable tip %s (landing build failed, merge unwound)\n",
				revert.PreTip)
			tools.RevertNotify(repo, revert.PreTip, res.Cmd, res.Exit, res.Err)
			return true
		}
		// This face (runTaskMergeRebuild / no snapshot) merges the
		// operator's EXPLICIT request, or the round landed nothing of
		// its own to unwind — no auto-revert (never reset a merge the
		// caller didn't initiate). Log-only, as before.
		fmt.Fprintf(os.Stderr, "[merger] [rebuild] no auto-revert (no merger-sweep snapshot) — merge left as-is, build-failed notified\n")
	}
	return false
}

// serveRebuildScriptSyncHead is the log prefix for the installed-script
// refresh lines (one greppable tag for the whole step).
const serveRebuildScriptSyncHead = "[merger] [scripts]"

// serveScriptSync refreshes the installed copies of the repo's operator
// scripts after a merge round landed (see serveRebuildAfterMerge for why).
// Never fatal: the merge is done and good, so a script that refuses to
// refresh is a logged wart with its output tail — the same contract the
// rebuild step holds. A repo whose scripts have no installed copies at all
// stays SILENT (nothing was ever installed, so nothing drifted).
func serveScriptSync(repo string) {
	res := tools.SyncInstalledScripts(repo)
	if res == nil {
		return
	}
	for _, name := range res.Refreshed {
		fmt.Printf("%s refreshed: %s (installed copy re-installed from the repo)\n", serveRebuildScriptSyncHead, name)
	}
	// Deterministic order: an audit trail that reorders itself run to run is
	// not an audit trail.
	names := make([]string, 0, len(res.Failed))
	for name := range res.Failed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "%s FAILED: %s: %s\n", serveRebuildScriptSyncHead, name, res.Failed[name])
	}
}

// runTaskMergeRebuild is the manual-merge face's rebuild tail: after
// `gopi task merge` lands or prunes ≥1 branch, rebuild so the bin follows
// the merge the operator just did. Zero merges (nothing/conflict-only) =
// no rebuild.
//
// landed names the branches this round carried, so the version name minted
// here answers "what does this position entail" exactly like the merger
// daemon's does — this face lands branches too, and a landing that reaches no
// name is the gap this tail exists to close (it was the ONLY landing path
// with no mint at all).
func runTaskMergeRebuild(opts *tasksOptions, merged, deleted int, landed []string) {
	runTaskMergeRebuildTallies(opts, merged, deleted, landed, nil)
}

// runTaskMergeRebuildTallies is the close loop for a sweep that decided SEVERAL
// repos. tallies is the engine's per-repo report (tools.TaskMergeCounts.
// RepoTallies): every repo that landed or pruned work is rebuilt in its OWN
// directory, because the declared build contract (Rule 12) is per repo.
//
// Before this, the loop rebuilt ONE repo — opts.Repo, else the cwd — so a
// repos-table sweep that landed work in three repos rebuilt whichever one the
// operator happened to stand in and left the other two running OLD code against
// NEW master. That is the exact failure Rule 12 exists to prevent, one level
// up: the counters were a union and the close loop guessed a single repo from
// them.
func runTaskMergeRebuildTallies(opts *tasksOptions, merged, deleted int, landed []string, tallies []tools.TaskMergeRepoTally) {
	if merged+deleted == 0 {
		return
	}
	if len(tallies) > 0 {
		any := false
		for _, tally := range tallies {
			if tally.Merged+tally.Deleted == 0 {
				continue
			}
			any = true
			taskMergeRebuildRepo(tally.Repo, tally.Merged, tally.Deleted, tally.Landed)
		}
		if any {
			return
		}
		// No per-repo entry landed anything: fall through to the single-repo
		// arm so a sweep's non-landing counters cannot swallow the close loop
		// (Rule 15 — a caller with counts says so, never silently skips).
	}
	repo := opts.repo
	if repo == "" {
		repo = tools.GitSyncRepoRoot()
	}
	if repo == "" {
		return
	}
	taskMergeRebuildRepo(repo, merged, deleted, landed)
}

// taskMergeRebuildRepo runs the declared rebuild + the installed-script refresh
// for ONE repo — the ONE implementation both callers above reach (Rules 13/14).
func taskMergeRebuildRepo(repo string, merged, deleted int, landed []string) {
	// The rebuild used to be preceded by a version-tag mint here; that mint is
	// deleted with its siblings (see serveRebuildAfterMerge's note and
	// devdoc/version-identity.md). This face lands, so it rebuilds — nothing
	// else.
	res := tools.RebuildAfterMerge(repo)
	switch {
	case res.Skipped:
		fmt.Printf("task merge: rebuild skipped (no build contract for %s in settings repos table)\n", repo)
	case res.OK:
		if res.Elapsed == 0 {
			fmt.Printf("task merge: rebuild ok — %s already current at %s\n", repo, res.ActualVersion)
		} else {
			fmt.Printf("task merge: rebuild ok — %s built in %s (%s)\n", repo, res.Elapsed.Round(0), res.ActualVersion)
		}
	default:
		fmt.Fprintf(os.Stderr, "task merge: rebuild FAILED: %s cmd=%q exit=%d err=%s\n",
			repo, res.Cmd, res.Exit, res.Err)
	}
	// Installed-script refresh — the operator's `gopi task merge` is the OTHER
	// face that lands branches, so its close loop carries the same second half
	// (see serveRebuildAfterMerge for the reproduced failure this closes).
	// Deliberately not gated on a build contract: a repo may declare scripts
	// and no build and its installed copies drift identically. The
	// no-auto-revert shape of this face means there is no reverted tree to
	// guard against.
	serveScriptSync(repo)
}
