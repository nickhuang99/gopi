package tools

// Rebuild-after-merge engine (epic 2026-09-02, nick's design): after a
// successful merge round lands ≥1 branch into master, the server rebuilds
// the repo's deployable binary so the machine runs the NEW code against
// NEW master — the close loop's last step. The recurring stale-bin
// incidents (2026-08-31..09-02: "unknown flag" / "falls through to agent
// loop" failures traced to a bin built hours before the merged commit)
// were exactly this gap: master advanced, the deployed binary didn't.
//
// The per-repo build contract comes from settings.json "repos":
//
//	"repos": {"/home/user/work/GoPi": {"build": "make build"}}
//
// Declared, never discovered (same philosophy as compute_nodes/sync_peers).
// A repo with NO entry has no build contract — the server skips the
// rebuild SILENTLY (a docs-only repo, or a repo whose build isn't
// declared; no error, no guess). nick's "worth and necessary" convention:
// a repo meant to be a gopi task field SHOULD declare its build here.
//
// Build failure is NOT a rollback trigger (the merge is done and good —
// git history is fine); it IS a notify event (event=build-failed, repo,
// error) so the operator knows the binary didn't follow master.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// BuildFailedEvent is the synthetic notify event fired when the post-merge
// rebuild fails (or the binary version does not match the new master tip).
// Operators subscribe via GOPI_TASK_NOTIFY_EVENTS (like "stale").
const BuildFailedEvent = "build-failed"

// rebuildVersionProbeTimeout caps the `--version` probe subprocess (a hung
// binary must not stall the merge round end).
const rebuildVersionProbeTimeout = 30 * time.Second

// RebuildResult is one rebuild attempt's outcome.
type RebuildResult struct {
	// Repo is the repo the rebuild ran in (the working directory).
	Repo string
	// Cmd is the build command that ran ("" = skipped: no build contract).
	Cmd string
	// Skipped is true when the repo has no build contract in settings —
	// the server deliberately does nothing (no error, no guess).
	Skipped bool
	// OK is true when the build succeeded AND the binary's version equals
	// the master tip it was built from.
	OK bool
	// Exit is the build command's exit code (0 = success; -1 = not run /
	// timeout kill).
	Exit int
	// Elapsed is the build duration.
	Elapsed time.Duration
	// Err is the failure detail (build stderr tail / version mismatch).
	Err string
	// ExpectedVersion is the tip of the ref the grounds are cut from — the
	// revision the build should have produced.
	ExpectedVersion string
	// ExpectedRef names the ref ExpectedVersion was read from
	// (rebuild_integration_ref.go): the integration mirror when this checkout
	// carries one, else the local integration branch, else HEAD. The close loop
	// PRINT it, so a fallback is never silent.
	ExpectedRef string
	// MeasureNote is "" when the preferred ref answered, and otherwise names
	// which ref answered instead and why. Never swallowed (Rule 15): a repo
	// with no integration ref is measured against HEAD, and whoever reads this
	// result is told that is what happened.
	MeasureNote string
	// ActualVersion is the binary's reported version ("" = probe failed).
	ActualVersion string
	// BuildOutput is the build command's output (stdout+stderr, capped).
	BuildOutput string
	// Vet is the post-merge GATE RE-RUN's verdict (rebuild_vet_gate.go): the
	// second measurement of the same close loop, reporting whether the tree
	// the round just landed still vets. It is set on EVERY path — a repo with
	// no build contract abstains here too — so a caller can always read both
	// halves of the close loop from this one result. Report-only: a red gate
	// never changes OK (the merge is done and good — Rule 12).
	Vet *VetGateResult
	// Mergeset is the MERGED-RESULT measurement (rebuild_vet_mergeset.go): the
	// repo's cheap gates re-run over the MERGE COMMIT's own tree in a detached
	// scratch worktree, as §5.2 of the 2026-09-16 stale-merge audit asks. Nil
	// when the step abstained at its resolution (no revision/toolchain/cache/
	// scratch). Like Vet it is report-only: a red gate here files a bug row and
	// notifies, and never unwinds the merge (Rule 12).
	Mergeset []mergesetGateResult
}

// RebuildAfterMergePrebuild is the close loop's PRE-build half: it measures the
// tree the round LANDED on, BEFORE any rebuild touches it, and answers with the
// gate verdict only when that measurement can force a decision — a red gate.
//
// WHY A SECOND ENTRY POINT. A red `go vet` can be the merge's fault, and when it
// is, the round must be unwound INSTEAD of built (a revert restores the pre-merge
// tip, so a build started first would stamp a binary at a merge master no longer
// has). The unwind decision therefore has to be reachable before the build runs —
// but it also needs a revert handle, which `RebuildAfterMerge` deliberately does
// not hold (its contract is Rule 12 report-only: it never touches OK/Err and never
// unwinds, see the deferred call below). So the pre-build half is a separate,
// narrow call whose caller is the one holding the handle
// (cmd/agent's serveRebuild), and the post-build half keeps its contract intact.
//
// nil IS THE COMMON ANSWER: a repo with no build contract has no close loop to
// gate, and a gate that PASSED or ABSTAINED has no decision to force (its verdict
// is still reported by RebuildAfterMerge's own call). Only a red gate returns a
// result, so the caller's branch is "there is a decision", not "there is a
// measurement".
//
// It runs the gate TWICE per landing round (here and in RebuildAfterMerge): the
// second run reads the same warm shared build cache and the measurement is the
// same, and paying it is what keeps the report-only half untouched (Rule 4:
// surgical — the alternative was threading a revert handle into
// RebuildAfterMerge's signature, which every existing caller would have had to
// change). Measured cost of the second run: the gate's own warm-cache wall time
// on the merge/verify smoke path.
func RebuildAfterMergePrebuild(repo string) *VetGateResult {
	if strings.TrimSpace(repo) == "" {
		return nil
	}
	// No build contract ⇒ no close loop for this repo ⇒ nothing to gate (the
	// same purposeful silence RebuildAfterMerge keeps). Reported, not swallowed:
	// an unreadable settings file means the question is unanswerable, and the
	// honest answer to "should this round be unwound?" is NO.
	if _, found, err := config.ResolveRepoBuild(repo); err != nil || !found {
		return nil
	}
	res := runPostMergeVetGate(repo)
	if res.Failed() {
		return res
	}
	return nil
}

// RebuildAfterMerge rebuilds repo (master checked out) IF settings declare
// a build contract for it — the merge-round-end step. Returns a result the
// caller logs; never returns an error for a failed build (failure is a
// notify event, not a crash — the merge itself is done and good).
func RebuildAfterMerge(repo string) *RebuildResult {
	res := &RebuildResult{Repo: repo}
	// The GATE RE-RUN's slot is owned HERE, before any early return: every
	// exit path of this function reports a verdict (the deferred run fills it
	// in), so no caller has to reason about which paths measured the tree.
	res.Vet = &VetGateResult{Status: mergeBuildGateStatusAbstain, Gate: vetGateName, Repo: repo,
		Reason: "the close loop did not reach the gate"}
	defer func() {
		// The gate re-runs AFTER the rebuild, on the tree the round left
		// behind (the deferred call is the ordering guarantee: every
		// already-current / non-GoPi-artifact / success return above has
		// completed by now, and a failure path reports both). Report-only —
		// it never touches res.OK (Rule 12: a gate failure is never a
		// rollback).
		res.Vet = runPostMergeVetGate(repo)
		// The MERGED-RESULT measurement follows it (§5.2): same close loop,
		// same report-only contract, measured over the merge COMMIT's tree
		// rather than the checkout. It is armed here for the same reason —
		// every exit path of this function carries it — and it resolves its
		// own revision so a landing face that has no revert handle still gets
		// the measurement.
		res.Mergeset = RebuildAfterMergeMergeset(repo, MergeHeadRev(repo))
	}()
	rc, found, err := config.ResolveRepoBuild(repo)
	if err != nil {
		res.Err = "resolve build contract: " + err.Error()
		res.notifyBuildFailed()
		return res
	}
	if !found {
		res.Skipped = true
		return res
	}
	res.Cmd = rc.Build

	// Expected version = the tip of the REF THE GROUNDS ARE CUT FROM — the
	// integration mirror when this checkout carries one, else the local
	// integration branch, else HEAD (rebuild_integration_ref.go resolves it and
	// never substitutes a fallback silently). MEASURING LOCAL HEAD HERE WAS THE
	// DEFECT this file's sibling closes: a checkout whose local master trails
	// its own upstream reads "already current" while the machine runs stale
	// code, so the close loop could not repair that class by construction.
	measure := rebuildTipForRepo(repo)
	if measure.Err != nil {
		res.Err = "resolve integration tip: " + measure.Err.Error()
		res.notifyBuildFailed()
		return res
	}
	tip := measure.Revision
	res.ExpectedVersion = tip
	res.ExpectedRef = measure.Ref
	res.MeasureNote = measure.Note

	// Already current? A binary built at this exact tip is the goal state —
	// skip the rebuild (idempotent: merge rounds with nothing new to build
	// cost nothing). The SAME revision-first verdict the post-build check
	// uses: a tagged binary must take this shortcut too, otherwise every
	// tagged merge round rebuilds for nothing. The shortcut is only as honest
	// as `tip`, which is why it is now the integration ref's tip and not
	// whatever this checkout happened to have checked out.
	if current, currentRev, _ := binaryVersion(repo); versionMatches(currentRev, tip) {
		res.OK = true
		res.ActualVersion = current
		return res
	}

	start := time.Now()
	exit, output := runBuild(repo, rc.Build, rc.BuildTimeoutSec)
	res.Elapsed = time.Since(start)
	res.BuildOutput = output
	res.Exit = exit
	if exit != 0 {
		res.Err = "build exited " + buildItos(exit) + ": " + outputTail(output)
		res.notifyBuildFailed()
		return res
	}

	// Verify: when the repo's artifact follows the GoPi convention (a
	// bin/gopi whose --version reports its build revision), assert it
	// matches the master tip. A repo with a DIFFERENT artifact shape (its
	// build declares e.g. "make build" → ./server) has no generic version
	// probe — the build command's exit 0 IS its contract (no false
	// build-failed from a probe that cannot read its version).
	if !res.hasGoPiBin() {
		res.OK = true
		return res
	}
	res.OK, res.ActualVersion, res.Err = verifyBuiltVersion(repo, tip)
	if !res.OK {
		res.notifyBuildFailed()
	}
	return res
}

// verifyBuiltVersion probes the freshly built artifact and asserts it
// follows the tip. It returns the probe's DISPLAY (what a human reads, kept
// on the result for the operator/debug surface), the OK verdict and — when
// the verdict is false — the failure text.
//
// The comparison is by REVISION, not by rendering: `--version` answers with
// the merger's version name when HEAD carries one
// ("v20260918-0932-<rev> (<rev12>)"), and with the bare revision otherwise,
// so a verifier that compared the whole string would call every TAGGED
// build a mismatch and auto-revert a perfectly good merge (the epic's
// regression — the probe refuses to be display-shaped).
func verifyBuiltVersion(repo, tip string) (ok bool, display string, errText string) {
	got, gotRev, _ := binaryVersion(repo)
	if versionMatches(gotRev, tip) {
		return true, got, ""
	}
	return false, got, fmt.Sprintf("version mismatch: expected %s, binary reports %q", tip, got)
}

// hasGoPiBin reports whether the repo follows the GoPi artifact convention
// (bin/gopi present) — the only shape the generic version probe can read.
func (r *RebuildResult) hasGoPiBin() bool {
	if r.Repo == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(r.Repo, rebuildBinDirName, rebuildBinName))
	return err == nil
}

// versionMatches compares a reporter's answer against the git tip the build
// should follow (git rev-parse --short HEAD: minimum-unique, commonly 7-12
// chars). The argument is either a REVISION (the probe recovered one out of
// the display: rebuild.go's binaryVersion -> RevisionOf path) or a whole
// DISPLAY — cmd/agent's own pre-revision callers and the tests pass
// `--version`'s line straight through — so both are accepted, in this order:
//
//  1. the argument's own revision, when it carries one (a tagged display
//     "v20260918-0932-<rev> (<rev12>)" compares by the fact inside it);
//  2. otherwise the whole string as a bare revision (the legacy shape).
//
// A "-dirty" answer never matches — a binary built from a dirty tree does
// NOT represent the committed master tip, and the caller must see the reject
// even when a revision is recoverable from it (rebuild.go relies on that for
// the tracked-dirt guard).
func versionMatches(binaryVer, tip string) bool {
	if binaryVer == "" || tip == "" {
		return false
	}
	if gopiversion.IsDirty(binaryVer) {
		return false
	}
	// Provenance: what gets compared is the revision the report carries.
	// A report yielding none (prose, a name without its revision group)
	// says nothing about the tip — refusing is the honest answer, never a
	// prefix match against a display.
	revisionCandidate, ok := gopiversion.RevisionOfDisplay(binaryVer)
	if !ok {
		return false
	}
	if revisionCandidate == tip {
		return true
	}
	// Prefix match: the longer of the two starts with the shorter.
	if len(revisionCandidate) < len(tip) {
		return strings.HasPrefix(tip, revisionCandidate)
	}
	return strings.HasPrefix(revisionCandidate, tip)
}

// notifyBuildFailed fires the build-failed synthetic alarm (best-effort —
// the notify pipeline's side-channel contract; failure to deliver is
// binaryVersionInterpreter runs the artifact with --version (never executes
// it directly: an atomically replaced binary — ETXTBSY — and a non-executable
// bit both become the honest "no version answer" instead of a hard failure).
const binaryVersionInterpreter = "sh"

// binaryVersionInterpreterArg is the -c script preceding the binary path.
const binaryVersionInterpreterArg = "-c"

// binaryVersionProbeCommand runs the artifact and its single argument
// ("$0" is the script word under `sh -c`).
const binaryVersionProbeCommand = `"$0" --version`

// binaryVersionProbeEnv prefixes PATH with the repo's bin directory so the
// probe resolves the BUILD's own artifact-side tooling (the same prepend
// tasks.go uses to make a child resolve THIS build's `gopi`: an operator's
// stale PATH binary must not answer for the fresh build). The directory is
// rebuildBinDirName, the declared artifact location.
func binaryVersionProbeEnv(repo string) []string {
	path := strings.Join(append([]string{filepath.Join(repo, rebuildBinDirName)},
		filepath.SplitList(os.Getenv(pathEnvName))...), string(os.PathListSeparator))
	return append(os.Environ(), pathEnvName+"="+path)
}

// bin/GOPI artifact location, shared by the probe (stat + run), the version
// assertion and the settings contract that declares the build.
const (
	rebuildBinDirName = "bin"
	rebuildBinName    = "gopi"
	// pathEnvName is the environment variable the probe prepends to.
	pathEnvName = "PATH"
)

// notifyBuildFailed fires the build-failed synthetic alarm (best-effort —
// the notify pipeline's side-channel contract; failure to deliver is
// logged inside, never fatal). Payload: repo, cmd, exit, error snippet.
func (r *RebuildResult) notifyBuildFailed() {
	summary := fmt.Sprintf("post-merge rebuild FAILED: repo=%s cmd=%q exit=%d err=%s",
		r.Repo, r.Cmd, r.Exit, r.Err)
	if r.ExpectedVersion != "" {
		summary += fmt.Sprintf(" expected=%s", r.ExpectedVersion)
	}
	if r.ActualVersion != "" {
		summary += fmt.Sprintf(" actual=%s", r.ActualVersion)
	}
	NotifySyntheticEvent(BuildFailedEvent, r.Repo, "", summary)
}

// binaryVersion probes the repo's gopi-style binary version. The binary is
// resolved relative to the repo: bin/gopi in the repo root is the GoPi
// convention (the epic's canonical build writes there). For other repos the
// build contract declares its own artifact — version verification is then
// build-specific and skipped (the helper only verifies when a bin/gopi
// exists; see RebuildResult.OK semantics — the build command's exit 0 is
// the general contract, version match is the GoPi-specific assertion).
// The probe asks the artifact what version it is, in the form a human reads:
// `bin/gopi --version` may answer with the merger's version tag
// ("v20260918-0932 (8380ce9caeef)") instead of the bare revision, so the
// probe recovers the REVISION from whatever shape comes back
// (gopiversion.RevisionOfDisplay is the single owner of the REPORT format
// `--version` answers with). It returns the display, the revision the
// report carries ("" when there is none or the probe failed) and whether
// the build stamped itself dirty.
//
// The reader is deliberately the report-aware RevisionOfDisplay, never the
// single-line RevisionOf: a tagged binary answers with the multi-line
// report, and a probe that re-parsed that report through the single-line
// parser recovered NO revision out of a perfectly correct answer — then
// reported it as a version mismatch, which the post-merge rebuild turned
// into an auto-revert of every tagged merge round (epic 2026-09-13).
func binaryVersion(repo string) (display, revision string, dirty bool) {
	return binaryVersionAt(repo, filepath.Join(repo, rebuildBinDirName, rebuildBinName))
}

// binaryVersionAt probes the artifact at an EXPLICIT path, keeping repo as the
// directory whose bin/ is prepended to the probe's PATH. It is the ONE probe
// body: binaryVersion (the rebuild verifier's door, the declared build
// location) and RunningDeployDrift (the operator's door, the artifact this
// machine actually runs) must not disagree about how a report is read, only
// about which file is read.
//
// The separation exists because the deployed artifact is not always under the
// directory you are standing in: /usr/local/bin/gopi is a symlink into
// /home/user/work/gopi/bin/gopi, so a run started in a task ground measured that
// ground's bin/gopi and reported a DIFFERENT artifact than the one the machine
// runs (measured 2026-10-28, the false "follows the ref" this seam fixes).
func binaryVersionAt(repo, bin string) (display, revision string, dirty bool) {
	if _, err := os.Stat(bin); err != nil {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), rebuildVersionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryVersionInterpreter, binaryVersionInterpreterArg, binaryVersionProbeCommand, bin)
	cmd.Env = binaryVersionProbeEnv(repo)
	// Run via cmd.Output with stderr discarded: the probe has no use for
	// stderr, but it must BE DRAINED — a child that writes a diagnostic to
	// stderr before exiting blocks on a full pipe, and the probe could then
	// only end by timing out (Rule 15b: an unread stream is a silent
	// failure that costs the whole timeout).
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	display = strings.TrimSpace(string(out))
	// A dirty report carries NO tip claim: the revision is deliberately
	// dropped so the verdict stays a revision-only comparison
	// (versionMatches) — the same reject the single-line parser used to
	// produce, now stated where the dirtiness is actually observed.
	if gopiversion.IsDirty(display) {
		return display, "", true
	}
	revision, _ = gopiversion.RevisionOfDisplay(display)
	return display, revision, false
}

// reportIsDirty's predicate moved to gopiversion.IsDirty (the single owner
// of the dirty witness, shared with MatchesRevision): the display can put the
// marker on the NAME ("v…-<rev>-dirty (<rev>)"), so a tail-only check here
// and in versionMatches disagreed about the same string. This file must not
// grow a second copy of that predicate back.

// The go toolchain stamps a build with vcs.modified=true when
// `git status --porcelain` reports ANYTHING — cmd/go runs that predicate
// itself (GOROOT/src/cmd/go/internal/vcs/vcs.go gitStatus) and git counts
// untracked files there. Untracked-only dirt therefore yields a "-dirty"
// binary and versionMatches (which must reject -dirty) fails the version
// assertion, so a post-merge rebuild would raise a false build-failed alarm
// AND auto-revert a perfectly good merge (epic 2026-09-12: proven by
// building the same clean-stamped source with and without an untracked
// file — vcs.modified went false → true, and back to false under this
// config). The rebuild's question is the SAME one the dirty guard asks
// (tracked state only), so the build subprocess must answer it the same way
// — untracked scratch (briefings/, logs, another agent's new file) must not
// stamp the binary, exactly as it must not defer the rebuild. Tracked dirt
// still stamps it, which is what the guard is for.
const (
	gitConfigCountEnv = "GIT_CONFIG_COUNT"
	gitConfigKeyEnv   = "GIT_CONFIG_KEY_0"
	gitConfigValueEnv = "GIT_CONFIG_VALUE_0"
	gitConfigCountOne = "1"
	// gitConfigShowUntracked = git's status.showUntrackedFiles knob: "no"
	// makes `git status --porcelain` omit ?? entries, so the toolchain's
	// dirty predicate sees tracked changes only.
	gitConfigShowUntrackedKey   = "status.showUntrackedFiles"
	gitConfigShowUntrackedValue = "no"
)

// buildVCSStatusEnv is the environment appended to every rebuild subprocess
// so the go toolchain's VCS stamping counts TRACKED changes only. A caller's
// own GIT_CONFIG_* entries are dropped first: a duplicate GIT_CONFIG_COUNT
// would otherwise leave which value wins to getenv's first-match rule, and a
// build stamp that silently flips on the operator's shell is exactly the
// class of bug this guards (Rule 15a).
func buildVCSStatusEnv() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_CONFIG_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		gitConfigCountEnv+"="+gitConfigCountOne,
		gitConfigKeyEnv+"="+gitConfigShowUntrackedKey,
		gitConfigValueEnv+"="+gitConfigShowUntrackedValue,
	)
}

// runBuild runs the build command via sh -c in repo with the given timeout
// (seconds; <=0 = no timeout — the caller resolves the default). Process-
// group isolation: a hung build is killed together with its children at
// the deadline (the taskNotifyCmd pattern). Returns the exit code and the
// combined output (stdout+stderr, capped at rebuildOutputMax).
func runBuild(repo, cmdLine string, timeoutSec int) (int, string) {
	var ctx context.Context
	var cancel context.CancelFunc
	if timeoutSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdLine)
	cmd.Dir = repo
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = buildVCSStatusEnv()

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			if cmd.Process != nil {
				// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
				// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
				_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
			}
			return -1, "build timed out after " + buildItos(timeoutSec) + "s: " + outputTail(string(out))
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		return -1, err.Error()
	}
	return 0, string(out)
}

// rebuildOutputMax caps the build output kept in a result (a runaway
// compiler log must not bloat the notify payload / log line).
const rebuildOutputMax = 2000

// outputTail returns the last rebuildOutputMax bytes of output (build
// errors live at the tail).
func outputTail(out string) string {
	if len(out) <= rebuildOutputMax {
		return strings.TrimSpace(out)
	}
	return "…" + strings.TrimSpace(out[len(out)-rebuildOutputMax:])
}

// buildItos formats an int for error strings (strconv-free helper).
func buildItos(n int) string {
	return fmt.Sprintf("%d", n)
}
