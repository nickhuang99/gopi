package tools

// merge_build_gate.go — the MERGE-TIME BUILD gate. The 2026-09-13 incident:
// 82b3b6f3 merged 82b05c4f (fix/merger-rebuild-selfrestart) into master and
// the merger reported SUCCESS, but cmd/agent/merger_self_restart.go referenced
// ten never-defined symbols (mergerDaemonLogDirMode, mergerDaemonReadyEnv,
// mergerDaemonAwaitReady, …) and master stopped compiling. Nothing in the
// merge path looked at the compiler: a missing symbol is not a git conflict, so
// no lane fired, `git merge` exited 0, and the branch was deleted like any
// other clean landing.
//
// WHY A NEW LANE AND NOT THE EXISTING ONES: the merger's failure lanes are all
// about GIT-level obstructions — a textual conflict (resolve → rework →
// archive), a live worktree holder, the CLI contract gate. A patch
// that does not compile is not an obstruction to reconcile, it is a broken
// patch: spending a 15-minute resolver child (or a rework round trip) on it
// burns the queue for a defect the compiler states in one second. So this gate
// refuses and archives in the same tick — no resolver, no rework loop.
//
// MECHANISM (mirrors merge_contract_gate.go's shape deliberately):
//  1. asked BEFORE `git merge` runs (step ⓪b in taskMergeOne), so a refusal
//     leaves master and the working tree byte-identical by construction —
//     there is no merge state to abort;
//  2. the check is COMPARATIVE: the build result of the MERGE TARGET (master)
//     is measured too, and only a BRANCH-INTRODUCED failure refuses. A master
//     that is temporarily red (the state this very incident left behind) must
//     not freeze the whole queue — every branch would then be refused for a
//     failure none of them caused (the same branch-vs-target rule the contract
//     gate learned on 2026-09-13);
//  3. the build runs in a DETACHED, TEMPORARY WORKTREE of the revision under
//     test — `git worktree add --detach <scratch>/<rev> <rev>` — never by
//     checking anything out in the repo the merger is merging into. Scratch
//     lives under the workspace scratch home (internal/scratch), never /tmp
//     (the 2026-09-11 exhaustion: 13,044 abandoned scratch dirs on the boot
//     device), and removes itself when the gate returns;
//  4. ABSTAIN is the default for anything the gate cannot answer: no build
//     contract, `git worktree add` failed, the build command was killed by its
//     timeout, the scratch tree is unavailable, an unreadable timestamp. The
//     merger's design contract is DETECT, never block on ambiguity — a gate
//     that false-refuses good branches gets switched off, and a timeout is not
//     evidence of a compile error;
//  5. refusal reuses the EXISTING branch-rework lane's identifiers (summary =
//     resolveRowReworkSummaryPrefix+<branch>, the same live-row dedup, the same
//     deliverable.branch stamp) — no second failure mechanism is invented —
//     plus the archive arm every path already honors (repoSweepArchiveBranch →
//     refs/tags/archive/<branch>).

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testcompile"
)

const (
	// mergeBuildGateActionRefused is the per-branch outcome when the gate
	// refuses a branch for a build failure. Distinct from "conflict" (nothing
	// to resolve) and from the contract gate's "contract-refused" (a different
	// fix: that one only needs a mirror entry, this one needs code that
	// compiles).
	mergeBuildGateActionRefused = "build-refused"

	// mergeBuildGateLegNoteFmt is appended to the refusal when the leg that
	// FAILED is not the repo's declared build (a Go module's test-tree compile
	// runs second). It is appended rather than folded into the format above so
	// the declared-build refusal's wording — and the rework brief rendered from
	// it — stays byte-identical, while a test-tree refusal tells the reworker
	// WHICH command to verify with instead of leaving it to guess.
	mergeBuildGateLegNoteFmt = " (the failing measurement was `%s`, not the declared build)"

	// mergeBuildGateRefuseDetailFmt renders the per-branch refusal line: the
	// branch, the target (whose own build was measured and passed), and the
	// tail of the compiler's own message — the report must never make an
	// operator re-run the build to learn what broke.
	mergeBuildGateRefuseDetailFmt = "branch does not compile — %s (branch %s, target %s built fine, master unchanged)"

	// mergeBuildGateEnvName names the operator escape hatch (Rule 2: the
	// default below is the constant, and this is the ONE reader).
	mergeBuildGateEnvName = "GOPI_MERGE_BUILD_GATE"

	// mergeBuildGateDisabledValue turns the gate off (an unforeseen
	// false-positive must always have an exit that is one env var away).
	mergeBuildGateDisabledValue = "0"

	// mergeBuildGateTestCompileTimeoutSec bounds the test-compile leg, and it is
	// internal/testcompile's own budget rather than a number chosen here — one
	// figure, referenced (Rule 14), because every caller runs a byte-identical
	// command and a second budget would be a second answer to "how long may this
	// take".
	mergeBuildGateTestCompileTimeoutSec = testcompile.TimeoutSec

	// mergeBuildGateLegStartFmt is the line a leg logs when it STARTS.
	//
	// WHY A START LINE AND NOT ONLY A RESULT LINE (measured, task 01a136c7 on
	// this repo, 2026-10-16): every leg of this gate logged ONCE, when it
	// RETURNED. The two compiled-shape legs are long — `go vet ./...` measured
	// 267.9s and the merged-result step 243.6s, running concurrently — so the
	// face (and the operator in front of it) saw 260.8s in which BOTH streams
	// carried not a single byte. A leg that is slow and a leg that is wedged are
	// INDISTINGUISHABLE in that window, which is the whole defect: the reader
	// cannot tell "still working" from "never coming back".
	//
	// The fix rides the funnel that already exists (taskNotifyLog → stderr, the
	// same channel every gate line uses): ONE line per leg at its start, no new
	// knob, no new process, no new file (Rule 35). The budget is printed with it
	// because it is the number that says how long the reader may reasonably wait
	// before the missing result line becomes evidence of a problem.
	mergeBuildGateLegStartFmt = " leg START `%s` on %s (budget %ds)\n"

	// mergeBuildGateLegStartMarker is the LITERAL prefix a reader (and a test)
	// greps for to find a leg's start line. It is the format's own head with the
	// verbs stripped, declared ONCE so a reworded format cannot leave a test
	// asserting on a string no line carries (Rule 1b/13).
	mergeBuildGateLegStartMarker = mergeBuildGateName + " leg START "

	// mergeBuildGateToolchainAbsentFmt is the ABSTAIN reason when a leg's command
	// word cannot be found on the gate's PATH. The toolchain's availability is
	// PRE-FLIGHTED with exec.LookPath rather than inferred from a failed run:
	// measured, `sh -c` with `go` off the PATH exits 127 printing
	// "/bin/sh: 1: go: not found" — a hardcoded per-shell marker, and a statement
	// about THIS HOST either way, never about the branch (Rule 15b: the
	// abstention is recorded with its reason).
	mergeBuildGateToolchainAbsentFmt = "`%s` is not on the gate's PATH on this host — the leg was never measured"

	// mergeBuildGateGoDefaultCmd is the build a repo with NO declared contract
	// gets when it is a Go module: the same command every briefing in this
	// codebase tells an agent to run. A repo that is NOT a Go module and
	// declares nothing ABSTAINS instead (no guess at a build command —
	// Rule 12's declared-not-discovered rule; the settings table is the
	// single source for a build CONTRACT).
	mergeBuildGateGoDefaultCmd = "go build ./..."

	// mergeBuildGateGoModFile is the marker that makes the Go default
	// applicable (a Go module root).
	mergeBuildGateGoModFile = "go.mod"

	// mergeBuildGateDefaultTimeoutSec bounds the build subprocess when the
	// repo's own contract declares no build_timeout_sec. It mirrors the
	// config package's own default (repoBuildTimeoutDefaultSec) — the value
	// the post-merge rebuild would use for the same repo — so the gate and
	// the rebuild never disagree about how long a build may take.
	mergeBuildGateDefaultTimeoutSec = 300

	// mergeBuildGateWorktreeVerb / mergeBuildGateWorktreeRemoveVerb are the
	// git subcommands that materialise (and drop) the revision under test.
	mergeBuildGateWorktreeVerb       = "worktree"
	mergeBuildGateWorktreeAddVerb    = "add"
	mergeBuildGateWorktreeRemoveVerb = "remove"
	mergeBuildGateWorktreePruneVerb  = "prune"
	mergeBuildGateWorktreeDetachFlag = "--detach"
	mergeBuildGateWorktreeForceFlag  = "--force"

	// mergeBuildGateScratchPattern names the gate's per-call scratch
	// directory inside the runtime scratch class (the GC recognises the
	// queue-runtime- name and bounds a crashed merger's leftovers).
	mergeBuildGateScratchPattern = "merge-build-gate-"

	// mergeBuildGateCacheDirName / mergeBuildGateModCacheDirName are the leaf
	// names of the two Go caches the gate may redirect: the cache directory
	// inside the shared per-repo build-cache root, and the gate's own scratch
	// module cache (used ONLY for a declared build that cannot read modules).
	//
	// The user's caches belong to the user (this is a read-only-ish probe, it
	// must not fill them), and the BUILD cache must NOT be scratch: it is a
	// reusable cross-task asset whose per-PROCESS tmpfs placement was the
	// 2026-09-15 exhaustion — merge_build_gate_gocache.go holds the location,
	// the reason and the bound. A Go build resolves its MODULES from the SHARED
	// read-only cache instead (merge_build_gate_modcache.go), because a
	// per-process cold module cache was the false-refusal defect.
	mergeBuildGateCacheDirName    = scratch.BuildCacheGoCacheLeafName
	mergeBuildGateModCacheDirName = "gomodcache"

	// Go environment names the gate controls. Rule 1b: these are compared and
	// written, so they are constants, not bare literals at the call site.
	mergeBuildGateGoCacheEnv    = "GOCACHE"
	mergeBuildGateGoModCacheEnv = "GOMODCACHE"

	// mergeBuildGateDownloadPrefix is the toolchain's own announcement that it
	// is about to FETCH a module zip (`go: downloading modernc.org/sqlite
	// v1.44.0`). It is the first half of the unbuildable-env signature below,
	// never a diagnostic on its own: a repo whose build is normally silent
	// about modules prints it only when the zips are not in the cache the gate
	// handed it.
	mergeBuildGateDownloadPrefix = "go: downloading"
)

// mergeBuildGateEnvAbsenceMarkers are the failures that mean the gate could
// not RESOLVE the module set — an unreachable proxy, a refused connection, a
// module the toolchain cannot find, a workspace-mode refusal, or a cache
// write the environment forbids. None of them is a statement about the
// BRANCH: the gate can only reach them because it builds with its OWN cold
// per-process GOMODCACHE (see mergeBuildGateScratch), so the branch content
// was never compiled. A non-zero build carrying one of these — together
// with the download announcement — is a measurement the gate could not make
// and must ABSTAIN on, exactly as it does for a timeout. Measured
// 2026-09-15: this host has no route to proxy.golang.org, master itself
// fails that build ("target master does not build either — FAIL" in the
// merger log for dozens of branches), and master builds passed only when
// the operator's warm cache happened to be used.
var mergeBuildGateEnvAbsenceMarkers = []string{
	"i/o timeout",
	"dial tcp",
	"connection refused",
	"no such host",
	"proxyconnect tcp",
	"tls handshake timeout",
	"module lookup disabled",
	"cannot find module providing",
	"missing go.sum entry",
	"updates to go.mod needed",
	"go.mod file not found",
	"permission denied",
	"no space left on device",
	"verifying module:",
	"unrecognized import path",
}

// mergeBuildGateDiskExhaustionMarkers are the phrases that mean THE WRITE ITSELF
// could not be satisfied — the filesystem refused the bytes, whatever the
// revision says. They are a SEPARATE class from the module-resolution markers
// above for a measured reason: the module markers need the toolchain's download
// announcement beside them (they are about RESOLVING the module set), while a
// link-stage write failure prints NOTHING else — measured 2026-10-12 on this
// host, where `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` at 45M free died
// with exactly one line, `link: mapping output file failed: no space left on
// device`, with no `go: downloading` anywhere in the output. So a classifier
// that demanded the announcement classified a disk exhaustion as a COMPILE
// failure and the gate refused (archived) a branch whose code was never linked.
var mergeBuildGateDiskExhaustionMarkers = []string{
	// The kernel's own spelling (the toolchain, the linker and sqlite all
	// report it verbatim).
	mergeBuildGateNoSpaceMarker,
	// The errno NAME, which some toolchains print instead of the strerror text.
	mergeBuildGateENOSPCMarker,
	// modernc.org/sqlite (this repo's driver) and the Go runtime's WAL/db paths.
	mergeBuildGateDiskFullMarker,
	// A filesystem that enforces a quota rather than reporting a full device:
	// the same class — the write is refused by the environment, not by the code.
	mergeBuildGateQuotaMarker,
}

const (
	// These four are the disk-exhaustion vocabulary's values, ALIASED to their
	// ONE definition in internal/scratch (Rule 13/14). They are not a second
	// definition: the declared TEST ENTRY POINT carries the same four phrases on
	// the shell side, and the two surfaces are held together by
	// scratch's own gate (disk_exhaustion_markers_test.go) — so the value must
	// have exactly one home, and it is the leaf package both sides can point at.
	//
	// mergeBuildGateNoSpaceMarker is ENOSPC's strerror text, referenced both by
	// the module-resolution marker list above and by the disk-exhaustion class
	// below.
	mergeBuildGateNoSpaceMarker = scratch.DiskExhaustionNoSpaceMarker
	// mergeBuildGateENOSPCMarker is the errno's name as the toolchain may print it.
	mergeBuildGateENOSPCMarker = scratch.DiskExhaustionENOSPCMarker
	// mergeBuildGateDiskFullMarker is the driver's own phrasing of the same refusal.
	mergeBuildGateDiskFullMarker = scratch.DiskExhaustionDiskFullMarker
	// mergeBuildGateQuotaMarker is the quota-enforced form of the same refusal.
	mergeBuildGateQuotaMarker = scratch.DiskExhaustionQuotaMarker
)

// mergeBuildGateDiagnosticShape matches the SHAPE of a compiler diagnostic
// (`file:line:col:`), which is what makes a failed build a statement about the
// REVISION rather than about the machine. It is deliberately the shape and not
// one language's extension: the gate builds whatever a repo declares.
var mergeBuildGateDiagnosticShape = regexp.MustCompile(scratch.DiskExhaustionDiagnosticShape)

const (
	// mergeBuildGateStatusPass / Fail / Abstain are the three statuses the
	// gate's own bookkeeping uses (Rule 1b: compared values, one spelling).
	mergeBuildGateStatusPass    = "PASS"
	mergeBuildGateStatusFail    = "FAIL"
	mergeBuildGateStatusAbstain = "ABSTAIN"

	// MergeBuildGateStatusPass / …Fail / …Abstain are the SAME three spellings,
	// EXPORTED for the callers outside this package that must render a merge
	// verdict (cmd/agent's one-shot merge run and its pre-land gate). They are
	// deliberate ALIASES, never a second definition (Rule 14): the unexported
	// constants above stay the one place the values are WRITTEN, so a rename
	// cannot leave two vocabularies behind — and a gate in cmd/agent can say
	// "ABSTAIN" in this package's own spelling instead of re-typing the literal
	// (Rule 1b).
	MergeBuildGateStatusPass    = mergeBuildGateStatusPass
	MergeBuildGateStatusFail    = mergeBuildGateStatusFail
	MergeBuildGateStatusAbstain = mergeBuildGateStatusAbstain

	// mergeBuildGateMergeBaseVerb / mergeBuildGateAncestorFlag are the
	// history-containment probe's git spelling (Rule 1b: git verbs compared
	// and dispatched on are constants, declared once in this gate).
	mergeBuildGateMergeBaseVerb = "merge-base"
	mergeBuildGateAncestorFlag  = "--is-ancestor"

	// mergeBuildGateStatusNoContract marks "this repo declares no build and is
	// not a Go module" — an abstention with its own line in the report.
	mergeBuildGateStatusNoContract = "NO-BUILD-CONTRACT"

	// mergeBuildGateErrorLines / mergeBuildGateErrorMaxBytes bound the build
	// output that rides the refusal (the brief and the report line): a
	// compiler's diagnostics are at the HEAD of `go build`'s output, and a
	// runaway log must not become the refusal's payload.
	mergeBuildGateErrorLines    = 12
	mergeBuildGateErrorMaxBytes = 4000

	// mergeBuildGateLogTailMax bounds the stderr the gate repeats in its own
	// log line for an abstention (a git failure's reason, never its whole
	// output).
	mergeBuildGateLogTailMax = 300

	// mergeBuildGateWaitDelay bounds how long the build's Wait may keep
	// waiting after its context is already done.
	//
	// ROOT CAUSE (measured 2026-09-13): the build runs through `sh -c`, and a
	// GRANDCHILD that outlives its parent (the timeout fixture's `sleep 30`, a
	// compiler's own subprocess) still holds the stdout/stderr pipes that
	// CombinedOutput waits on. So Wait blocked for as long as the orphan lived
	// — the 1s-budget timeout arm measured 30.03s of wall time — and the
	// process-group SIGKILL below it was UNREACHABLE on exactly the path that
	// needs it (the kill happens after Wait returns). WaitDelay makes Wait
	// give up on the pipes at the deadline (returning ErrWaitDelay) instead of
	// blocking on a process the gate never sees; the SIGKILL then runs and the
	// goroutine reapers release the descriptors, so nothing survives the call.
	mergeBuildGateWaitDelay = 5 * time.Second
)

// mergeBuildGateOutcome is one revision's build result. The three statuses are
// deliberately not a bool: the gate's whole safety argument rests on telling
// "I measured a failure" (FAIL) apart from "I could not measure" (ABSTAIN),
// because only the former may refuse.
// mergeBuildGateOutcome is ONE leg's measurement of ONE revision.
type mergeBuildGateOutcome struct {
	// Status is PASS | FAIL | ABSTAIN.
	Status string
	// Output is the command's own output (capped; "" for an abstention that
	// never ran it).
	Output string
	// Err explains an ABSTAIN (a git failure, a timeout, an unusable scratch
	// root) — "" for a real measurement.
	Err string
	// Cmd is the command this outcome measured ("" for an abstention that never
	// ran one). It is carried so the refusal names the command that actually
	// failed: with more than one leg, "the branch does not compile" is not
	// enough — the reworker must verify with the SAME command (the gate's own
	// refusal contract).
	Cmd string
}

// mergeBuildGateVerdict is the gate's decision for one branch.
type mergeBuildGateVerdict struct {
	// Refused is true when the branch must not be merged (it does not
	// compile and its target does).
	Refused bool
	// Detail is the refusal reason ("" unless Refused).
	Detail string
	// BuildCmd is the command that FAILED — the leg's own command, not the
	// repo's declared build. The gate runs more than one leg (a Go module is
	// also test-compiled), so the refusal brief must name the command that
	// actually produced the diagnostics, or the reworker verifies with the
	// wrong one.
	BuildCmd string
	// BranchErr is the failed branch build's diagnostics (the real compiler
	// text).
	BranchErr string
}

// mergeBuildGateSpec is the build the gate runs for a repo: the command and
// its timeout.
type mergeBuildGateSpec struct {
	// Cmd is the build command line (run through `sh -c`, like the post-merge
	// rebuild's contract).
	Cmd string
	// TimeoutSec bounds the command (<=0 = no timeout, the config contract's
	// explicit opt-out).
	TimeoutSec int
}

// mergeBuildGate decides whether branch may land on master by BUILDING it and
// master, in temporary detached worktrees under the scratch home. It NEVER
// writes to the repo the merger merges into: the worktrees are separate
// checkouts and the only git it runs is `worktree add/remove`.
//
// Fail-open by design (the merger's design contract): every question the gate
// cannot answer — no build contract, no scratch tree, an unresolvable ref, a
// non-zero build the target reproduces, a killed build — ABSTAINS and the
// merge proceeds exactly as it would have before this gate existed.
func mergeBuildGate(repo, master, branch string) mergeBuildGateVerdict {
	if mergeBuildGateDisabled() {
		return mergeBuildGateVerdict{}
	}
	spec, ok, err := mergeBuildGateResolveSpec(repo)
	if err != nil {
		taskNotifyLog("merger: BUILD gate ABSTAINS for %s (build contract unreadable: %v) — merging unchanged", branch, err)
		return mergeBuildGateVerdict{}
	}
	if !ok {
		taskNotifyLog("merger: BUILD gate ABSTAINS for %s (%s: no declared build and not a Go module) — merging unchanged",
			branch, mergeBuildGateStatusNoContract)
		return mergeBuildGateVerdict{}
	}
	cacheDir, okCache := mergeBuildGateGoCache(repo)
	if !okCache {
		taskNotifyLog("merger: BUILD gate ABSTAINS for %s (no usable shared Go build cache — the reason is logged above) — merging unchanged", branch)
		return mergeBuildGateVerdict{}
	}
	workRoot, cleanup, okScratch := mergeBuildGateScratch(repo)
	if !okScratch {
		taskNotifyLog("merger: BUILD gate ABSTAINS for %s (no usable scratch tree for a build checkout) — merging unchanged", branch)
		return mergeBuildGateVerdict{}
	}
	// The MODULES must be resolvable BEFORE any build runs: without a populated
	// read-only module cache a Go build can only attempt a fetch, and a fetch
	// failure is a statement about this host, not about the branch
	// (merge_build_gate_modcache.go). ABSTAIN here, naming the path in the log,
	// rather than build into an empty cache and misread the fetch error.
	//
	// The requirement is scoped to builds that can READ modules at all: a repo
	// that is not a Go module and whose declared build does not run the Go
	// toolchain has no module set to resolve, so a host with no cache must not
	// abstain on it (that would abstain every non-Go repo for an irrelevant
	// reason). Such a build keeps the gate's own scratch cache, which stays the
	// never-touch-the-operator's-cache default.
	modCacheDir, okModCache := mergeBuildGateSharedModCache()
	if !okModCache {
		if mergeBuildGateIsGoRepo(repo) {
			taskNotifyLog("merger: BUILD gate ABSTAINS for %s (%s is a Go module and no populated shared module cache is resolvable — the reason is logged above) — merging unchanged",
				branch, repo)
			return mergeBuildGateVerdict{}
		}
		modCacheDir, okModCache = mergeBuildGateScratchModCache()
		if !okModCache {
			taskNotifyLog("merger: BUILD gate ABSTAINS for %s (no usable scratch module cache) — merging unchanged", branch)
			return mergeBuildGateVerdict{}
		}
	}
	defer cleanup()

	// The BRANCH first: when it builds, no other measurement is needed and
	// the gate costs exactly one build (the common case — a branch that
	// compiles is the overwhelming majority of a merge queue).
	branchRes := mergeBuildGateBuild(repo, branch, spec, workRoot, cacheDir, modCacheDir)
	if branchRes.Status != mergeBuildGateStatusFail {
		if branchRes.Status == mergeBuildGateStatusAbstain {
			taskNotifyLog("merger: BUILD gate ABSTAINS for %s (%s) — merging unchanged", branch, branchRes.Err)
		}
		return mergeBuildGateVerdict{}
	}
	// The branch FAILED — unless the target ALREADY CONTAINS it: the branch
	// adds nothing master does not have, so there is no branch-introduced
	// failure to find and the comparative question is unanswerable. Answering
	// it with a build only produced the self-contradicting "branch fails, but
	// target does not build either — FAIL" line (2026-09-14). One ancestry
	// read replaces a whole build AND gets the wording right.
	if mergeBuildGateBranchAlreadyInTarget(repo, branch, master) {
		taskNotifyLog("merger: BUILD gate ABSTAINS for %s (%s already contains %s — the branch adds nothing, so there is no branch-introduced failure to find) — merging unchanged",
			branch, master, branch)
		return mergeBuildGateVerdict{}
	}

	// Ask the COMPARATIVE question: does the target build? A master that is
	// already red (2026-09-13 left exactly that state behind) must not refuse
	// every branch in the queue for a failure none of them introduced.
	masterRes := mergeBuildGateBuild(repo, master, spec, workRoot, cacheDir, modCacheDir)
	if masterRes.Status != mergeBuildGateStatusPass {
		taskNotifyLog("merger: BUILD gate ABSTAINS for %s (branch fails to build, but target %s did not PASS either — %s) — not branch-introduced, merging unchanged",
			branch, master, masterRes.Status)
		return mergeBuildGateVerdict{}
	}
	errText := mergeBuildGateErrorText(branchRes.Output)
	if errText == "" {
		errText = mergeBuildGateAbstainBuildText
	}
	detail := fmt.Sprintf(mergeBuildGateRefuseDetailFmt, errText, branch, master)
	if branchRes.Cmd != spec.Cmd {
		detail += fmt.Sprintf(mergeBuildGateLegNoteFmt, branchRes.Cmd)
	}
	return mergeBuildGateVerdict{
		Refused:   true,
		Detail:    detail,
		BuildCmd:  branchRes.Cmd,
		BranchErr: errText,
	}
}

// mergeBuildGateAbstainBuildText stands in for the diagnostics when a failed
// build produced NO output at all (a repo whose build exits non-zero silently):
// the refusal is still correct, and the brief must not claim a compiler error
// it never saw.
const mergeBuildGateAbstainBuildText = "(the build command exited non-zero and printed no diagnostics)"

// mergeBuildGateResolveSpec resolves the build contract for repo: the
// settings.json repos table (Rule 12, via config.ResolveRepoBuild — the ONE
// resolver, shared with the post-merge rebuild), else the Go default for a Go
// module, else no contract at all (ok=false → ABSTAIN).
func mergeBuildGateResolveSpec(repo string) (mergeBuildGateSpec, bool, error) {
	rc, found, err := config.ResolveRepoBuild(repo)
	if err != nil {
		return mergeBuildGateSpec{}, false, err
	}
	goRepo := mergeBuildGateIsGoRepo(repo)
	if found {
		return mergeBuildGateSpec{Cmd: rc.Build, TimeoutSec: rc.BuildTimeoutSec}, true, nil
	}
	if goRepo {
		return mergeBuildGateSpec{
			Cmd:        mergeBuildGateGoDefaultCmd,
			TimeoutSec: mergeBuildGateDefaultTimeoutSec,
		}, true, nil
	}
	return mergeBuildGateSpec{}, false, nil
}

// mergeBuildGateIsGoRepo reports whether repo is a Go module root (the marker
// that makes mergeBuildGateGoDefaultCmd applicable).
func mergeBuildGateIsGoRepo(repo string) bool {
	if repo == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(repo, mergeBuildGateGoModFile))
	return err == nil
}

// mergeBuildGateLeg is ONE command the gate runs against a revision, measured in
// the gate's own detached scratch checkout with the gate's own caches and
// budget. It is a LIST because one measurement is no longer enough to answer "is
// this branch landable": `go build` reads no `_test.go` file, so a branch whose
// TEST TREES do not compile is green to every build-shaped measurement that has
// ever guarded this queue (measured 2026-10-12: `go build ./...` exit 0 while
// `go vet ./cmd/agent/` reported `undefined: tools.NotifyEventLedgerDivergence`
// from a redproof test file, and 2026-09-16 for the union-gate collision).
//
// The legs are run in ORDER and the FIRST FAIL is the refusal, exactly as the
// union gate's spec does it (mergeUnionGateSpec.Legs): a tree that does not
// compile makes the wider measurement's verdict uninteresting.
type mergeBuildGateLeg struct {
	// Cmd is the command line, run through `sh -c` (the declared contract's own
	// shape) with the gate's caches and process-group isolation.
	Cmd string
	// TimeoutSec bounds THIS leg; <=0 = no bound (the config contract's
	// explicit opt-out).
	TimeoutSec int
}

// mergeBuildGateLegs resolves the command set for one revision. cmdDir is the
// gate's detached checkout of that revision — the leg's command runs THERE, and
// the second leg is added only when that checkout is a Go module ROOT
// (`go.mod` present), which is the premise its own command needs.
//
// The CONTRACT-RESOLVED leg is the declared build (Rule 12's single source,
// shared with the post-merge rebuild), or the Go default for a Go module with no
// declaration. On top of it, a Go module gets the test-tree compile leg — because
// that leg is derived from the TOOLCHAIN reading the tree (every package's
// `_test.go`), not from a declaration, so it applies exactly where `go build`
// applies and needs no new configuration key (Rule 35: no new settings key, no
// new script, no new daemon — the leg rides the gate that already ticks).
//
// The premise is measured in the CHECKOUT and not in the repo the gate was
// called on: the two are the same revision by construction, and the checkout is
// the tree the leg will actually read.
//
// A repo that is NOT a Go module gets NO second leg: the declared build is all
// the toolchain knowledge this host has for it (the no-guess rule), and
// inventing a test command for an unknown language is how a gate starts
// false-refusing.
// mergeBuildGateTestCompileCmd is the TEST-TREE compile pass — the second,
// and only, leg a GO MODULE's gate runs. The SPELLING lives in
// internal/testcompile (Rules 13/14) because the clone channel's delivery
// loop runs the same command and CANNOT import this package:
// `internal/tools` imports `internal/devworkspace` in 46 files while
// `internal/devworkspace` imports this one in none, so the edge is fixed and
// a literal here would be the second copy that drifts the moment either side
// learns a flag.
//
// WHY THE LEG EXISTS AT ALL — `go build ./...` never compiles a `_test.go`
// file, so a branch whose test tree does not compile (a deleted declaration
// still referenced from another package's test, as on 2026-10-12:
// `internal/tools.tasknotify.go` lost `NotifyEventLedgerDivergence`, which
// `cmd/agent`'s redproof test still referenced) passes the declared build and
// lands. The filter `^$` matches no test, so every test binary is BUILT and
// nothing is RUN: no ledger is opened, no daemon started, no fixture
// executed — so the gate stays cheap and Rule 27's ban on generating load is
// respected by construction.
//
// The command is spelled with `go` rather than an absolute toolchain path
// because the leg is gated on mergeBuildGateIsGoRepo (a `go.mod` exists) —
// the same premise vetGateResolveSpec uses; a host without the toolchain
// gets the child's own "executable file not found", which the leg reports
// as its own abstention rather than as a branch defect.
//
// It is deliberately NOT a `go vet` leg: vet's analyzer findings are not
// merge hazards, and the union gate's own note ("A VET LEG WAS DELIBERATELY
// REJECTED") records the measured reason — `unsafe.Pointer`→`uintptr` in
// test files is reported by vet on Go 1.24, which would refuse branches that
// only touch a field offset.
var mergeBuildGateTestCompileCmd = testcompile.Command

// A var and not a const because testcompile.Command is itself derived
// (fmt.Sprintf) rather than a constant expression — the DERIVATION is what
// this file must not duplicate.
func mergeBuildGateLegs(spec mergeBuildGateSpec, goRepo bool, cmdDir string) []mergeBuildGateLeg {
	legs := []mergeBuildGateLeg{{Cmd: spec.Cmd, TimeoutSec: spec.TimeoutSec}}
	if !goRepo || !mergeBuildGateIsGoRepo(cmdDir) {
		return legs
	}
	return append(legs, mergeBuildGateLeg{
		Cmd:        mergeBuildGateTestCompileCmd,
		TimeoutSec: mergeBuildGateTestCompileTimeoutSec,
	})
}

// mergeBuildGateScratchModCache creates the gate's own scratch module cache —
// used ONLY for a repo whose declared build cannot read modules (not a Go
// module, no Go toolchain): it is what keeps a non-Go repo's build off the
// operator's caches exactly as before the shared-module-cache change, while a Go
// build never touches it (a cold scratch module cache is precisely the defect
// merge_build_gate_modcache.go removes).
//
// Its error is REPORTED and non-nil so the caller's ABSTAIN is attributable:
// the gate's contract is that a module cache it cannot see is an abstention with
// a reason in the log, never a silent retry against an assumed path (Rule 15b).
func mergeBuildGateScratchModCache() (modCacheDir string, ok bool) {
	classDir := scratch.RuntimeScratchDir(scratch.RuntimeClassMergeBuild)
	if classDir == "" {
		return "", false
	}
	modCacheDir = filepath.Join(classDir, mergeBuildGateModCacheDirName)
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(modCacheDir, scratch.DirPerm); err != nil {
		taskNotifyLog("merger: BUILD gate scratch gomodcache %s unusable: %v", modCacheDir, err)
		return "", false
	}
	return modCacheDir, true
}

// mergeBuildGateScratch prepares the gate's per-CALL scratch: the directory the
// temporary detached worktree checkouts materialise in, plus its cleanup.
func mergeBuildGateScratch(repo string) (workRoot string, cleanup func(), ok bool) {
	classDir := scratch.RuntimeScratchDir(scratch.RuntimeClassMergeBuild)
	if classDir == "" {
		return "", func() {}, false
	}
	workRoot, err := scratch.MkdirTempIn(classDir, mergeBuildGateScratchPattern)
	if err != nil {
		taskNotifyLog("merger: BUILD gate worktree scratch unusable: %v", err)
		return "", func() {}, false
	}
	return workRoot, mergeBuildGateScratchCleanup(repo, workRoot), true
}

// mergeBuildGateScratchCleanup builds the cleanup closure for one gate call.
// It drops the per-call workRoot AND is the LAST word on this call's git
// registrations: the caller may defer it while a checkout still exists (a
// SIGKILL of the whole gate process is the only path that skips it, and that
// path leaves the directory AND the registration together for the sweeper).
//
// The ORDER is load-bearing (D3), and the mechanism is MEASURED (git 2.34.1,
// three probes): git does NOT collect a registration on its own once its
// directory is gone — `git worktree prune` is the only verb that drops it, so
// a cleanup that only calls os.RemoveAll leaves a registration nothing ever
// revisits. Master's cleanup was exactly that single line, and the residue it
// produced is on this host today:
// <scratch>/queue/queue-runtime-merge-build-*/merge-build-gate-*/wt-*, listed
// by `git worktree list --porcelain` as "prunable gitdir file points to
// non-existent location". So the directory removal is followed by an explicit
// registration reap (remove --force, then drop a stale lock + retry, then
// prune), exactly the repair the build path performs.
func mergeBuildGateScratchCleanup(repo, workRoot string) func() {
	return func() {
		if rerr := os.RemoveAll(workRoot); rerr != nil {
			// Rule 15b: never swallow — a leaked checkout is invisible
			// otherwise (the scratch GC would eventually take it, but this log
			// line is what makes the leak attributable).
			taskNotifyLog("merger: BUILD gate worktree scratch %s not removed: %v", workRoot, rerr)
		}
		mergeBuildGateReapRegistrations(repo, workRoot)
	}
}

// mergeBuildGateRemoveWorktree is the gate's OWN checkout removal — the ONE
// place it runs `git worktree remove --force` on a directory the gate created
// for a single build (the gate's three call sites and its registration-repair
// sweep all go through here), so the audit record can never be forgotten at one
// of them (package removalaudit: a record per removal attempt, emitted BEFORE
// git runs — a refused attempt is recorded too).
func mergeBuildGateRemoveWorktree(repo, dir, evidence string) error {
	// reclaim-checklist-exempt: the ground checklist (reclaim_ground_checklist.go)
	// is deliberately NOT consulted here: this is the BUILD gate's own scratch checkout, created
	// for a single build and held by nobody else. Rung ① ("is the ground mine
	// to free?") has no subject — the gate itself created and owns it — and
	// rung ② ("does the branch descend from the integration branch?") has no
	// branch: the checkout is detached at the gate's own tip.
	removalaudit.WorktreeRemove(removalaudit.ArmMergeBuildGateGround, repo, dir, "", "", evidence)
	_, err := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeRemoveVerb,
		mergeBuildGateWorktreeForceFlag, dir)
	return err
}

// mergeBuildGateReapRegistrations drops every git worktree registration whose
// path is inside workRoot, whatever the reason the directory went away. It is
// the belt to the build path's braces: the removal failure path repairs the
// registration too, but a directory that vanished any other way (a concurrent
// scratch GC, a cleanup ordering change) must not leave a registration behind.
//
// Failures are REPORTED, never swallowed (Rule 15b) — an unrepairable
// registration is exactly the residue this function exists to prevent.
func mergeBuildGateReapRegistrations(repo, workRoot string) {
	out, err := repoSweepGit(repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		taskNotifyLog("merger: BUILD gate could not list worktrees in %s: %s", repo, mergeBuildGateTail(err.Error()))
		return
	}
	root := filepath.Clean(workRoot) + string(filepath.Separator)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, finishCleanupPorcelainWorktreePrefix) {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainWorktreePrefix))
		if !strings.HasPrefix(filepath.Clean(path)+string(filepath.Separator), root) {
			continue
		}
		if rerr := mergeBuildGateRemoveWorktree(repo, path,
			"the gate's reaper found this registration inside its own scratch root with no directory left"); rerr != nil {
			// The same repair ladder the build path uses, in the order git
			// documents it: unlock, retry, then prune.
			taskNotifyLog("merger: BUILD gate registration %s not removed (%s) — repairing",
				path, mergeBuildGateTail(rerr.Error()))
			if lerr := worktreeDropStaleLock(repo, path); lerr != nil {
				taskNotifyLog("merger: BUILD gate could not clear the lock on %s: %s", path, mergeBuildGateTail(lerr.Error()))
			}
			if rerr2 := mergeBuildGateRemoveWorktree(repo, path,
				"retry after the gate cleared the stale 'initializing' lock"); rerr2 != nil {
				taskNotifyLog("merger: BUILD gate registration %s STILL not removed after the lock repair: %s (pruning)",
					path, mergeBuildGateTail(rerr2.Error()))
				if _, perr := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreePruneVerb); perr != nil {
					taskNotifyLog("merger: BUILD gate prune in %s failed: %s", repo, mergeBuildGateTail(perr.Error()))
				}
			}
		}
	}
}

// mergeBuildGateBuild materialises rev in a temporary detached worktree under
// workRoot and runs the build there with the gate's own Go caches.
func mergeBuildGateBuild(repo, rev string, spec mergeBuildGateSpec, workRoot, goCacheDir, sharedModCacheDir string) mergeBuildGateOutcome {
	dir := filepath.Join(workRoot, mergeBuildGateWorktreeDirName(rev))
	// FRESH PATH (worktree_fresh_path.go): the checkout name is DERIVED from
	// the rev, so a survivor of an earlier run in the same scratch class would
	// be reused silently when empty — and the build would then measure a tree
	// this run did not create. The gate ABSTAINS instead of guessing.
	if err := worktreeFreshPath(dir); err != nil {
		return mergeBuildGateOutcome{
			Status: mergeBuildGateStatusAbstain,
			Err:    fmt.Sprintf("worktree path for %s is not fresh: %s", rev, mergeBuildGateTail(err.Error())),
		}
	}
	if _, err := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeAddVerb,
		mergeBuildGateWorktreeDetachFlag, dir, rev); err != nil {
		return mergeBuildGateOutcome{
			Status: mergeBuildGateStatusAbstain,
			Err:    fmt.Sprintf("worktree add --detach %s failed: %s", rev, mergeBuildGateTail(err.Error())),
		}
	}
	defer func() {
		// The checkout is scratch by construction; --force also drops the
		// admin entry when the worktree's index is dirty (the build may have
		// written artifacts into it).
		if err := mergeBuildGateRemoveWorktree(repo, dir,
			"the gate's own detached build checkout; the build it was created for has finished"); err != nil {
			// git REFUSED — and the shape it refuses is the one this gate
			// creates: a SIGKILLed `worktree add` leaves
			// `.git/worktrees/<id>/locked` = "initializing", which `remove
			// --force` rejects ("use 'remove -f -f' to override or unlock
			// first") and `prune` SKIPS. Measured on this host: two such
			// registrations (this gate's wt-master/wt-master3) survived every
			// sweep for hours after their merger was killed. So the gate
			// REPAIRS what it made, in the order git documents:
			//
			//	① drop the stale lock (worktreeDropStaleLock);
			//	② retry the removal — now unlocked, git accepts it;
			//	③ if git still refuses, prune so at least the registration
			//	   cannot outlive a directory this function is about to
			//	   remove, and REPORT which step failed (Rule 15b: the gate
			//	   must never leave residue silently).
			taskNotifyLog("merger: BUILD gate worktree %s not removed (%s) — repairing the registration",
				dir, mergeBuildGateTail(err.Error()))
			if lerr := worktreeDropStaleLock(repo, dir); lerr != nil {
				taskNotifyLog("merger: BUILD gate could not clear the lock on %s: %s", dir, mergeBuildGateTail(lerr.Error()))
			}
			if rerr := mergeBuildGateRemoveWorktree(repo, dir,
				"retry after the gate cleared the stale 'initializing' lock"); rerr != nil {
				taskNotifyLog("merger: BUILD gate worktree %s STILL not removed after the lock repair: %s (pruning the registration)",
					dir, mergeBuildGateTail(rerr.Error()))
				if _, perr := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreePruneVerb); perr != nil {
					taskNotifyLog("merger: BUILD gate prune in %s failed: %s", repo, mergeBuildGateTail(perr.Error()))
				}
			}
		}
	}()

	// EVERY leg runs, and the FIRST FAIL is the verdict. The legs are ordered
	// build-then-test-compile for the same reason the union gate orders its own:
	// a tree that does not compile makes the wider measurement's verdict
	// uninteresting, and the cheap decisive answer should be the one quoted.
	// An ABSTAIN from a leg never masks a later leg's FAIL: only a FAIL stops
	// the walk, and an abstention is REMEMBERED so that a run whose every leg
	// abstained still reports the first abstention's reason (Rule 15 — an
	// unmeasured tree must be distinguishable from a clean one). A leg whose
	// COMMAND WORD is not on this host's PATH is pre-flighted here and skipped
	// with its own abstention reason: the toolchain's absence is a statement
	// about the host, never about the branch.
	legs := mergeBuildGateLegs(spec, mergeBuildGateIsGoRepo(repo), dir)
	outcome := mergeBuildGateOutcome{}
	abstainReason := ""
	for _, leg := range legs {
		if _, lerr := exec.LookPath(mergeBuildGateCommandWord(leg.Cmd)); lerr != nil {
			if abstainReason == "" {
				abstainReason = fmt.Sprintf(mergeBuildGateToolchainAbsentFmt, mergeBuildGateCommandWord(leg.Cmd))
			}
			continue
		}
		res := mergeBuildGateRunLeg(dir, repo, rev, leg, goCacheDir, sharedModCacheDir)
		if res.Status == mergeBuildGateStatusFail {
			return res
		}
		if res.Status == mergeBuildGateStatusAbstain {
			if abstainReason == "" {
				abstainReason = res.Err
			}
			continue
		}
		outcome = res
	}
	if abstainReason != "" && outcome.Status == "" {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain, Err: abstainReason}
	}
	if outcome.Status == "" {
		// No leg ran at all: an empty command set is a resolution defect, not a
		// clean branch (Rule 15: never report unmeasured as verified).
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain,
			Err: fmt.Sprintf("no measurement leg was resolved for %s", rev)}
	}
	return outcome
}

// mergeBuildGateRunLeg runs ONE leg in the checkout `dir` and shapes its outcome.
// It owns the two judgements every leg shares — the budget and the
// unresolved-modules abstention — so a second leg cannot be added with a
// different reading of the same failure (Rule 13).
func mergeBuildGateRunLeg(dir, repo, rev string, leg mergeBuildGateLeg, goCacheDir, sharedModCacheDir string) mergeBuildGateOutcome {
	spec := mergeBuildGateSpec{Cmd: leg.Cmd, TimeoutSec: leg.TimeoutSec}
	taskNotifyLog(mergeBuildGateName+mergeBuildGateLegStartFmt, leg.Cmd, rev, leg.TimeoutSec)
	exit, output, timedOut := mergeBuildGateRun(dir, spec, goCacheDir, sharedModCacheDir)
	res := mergeBuildGateOutcome{Cmd: leg.Cmd}
	if timedOut {
		res.Status = mergeBuildGateStatusAbstain
		res.Err = fmt.Sprintf("`%s` on %s exceeded its %ds budget (a timeout is not a compile error)",
			leg.Cmd, rev, leg.TimeoutSec)
		return res
	}
	if exit == 0 {
		res.Status = mergeBuildGateStatusPass
		res.Output = mergeBuildGateErrorText(output)
		return res
	}
	// Before calling it FAIL the gate asks whether the output is a statement
	// about the revision AT ALL: a command that could not resolve its modules
	// was never compiled (mergeBuildGateUnresolvedModules), and refusing a
	// branch for the toolchain's cache is the 2026-09-15 false refusal this
	// guard closes. The same guard covers the toolchain being absent from the
	// PATH (the test-compile leg is gated on go.mod, not on LookPath), which is
	// a statement about THIS HOST, never about the branch.
	if mergeBuildGateUnresolvedModules(output) {
		res.Status = mergeBuildGateStatusAbstain
		res.Output = output
		res.Err = mergeBuildGateUnresolvedModulesErr
		return res
	}
	// A build the FILESYSTEM killed was never linked either: the linker's own
	// "mapping output file failed: no space left on device" (measured on this
	// host at 45M free) is the environment refusing the write, not a statement
	// about the revision. Refusing a branch for it archives work whose code was
	// never compiled — the false refusal this class closes. Its ABSTAIN carries
	// the marker AND both partitions' figures, because an operator reading a
	// disk-exhaustion line must not have to guess which filesystem ran out (on
	// this host `/` was 131G free while the worktree's own partition was full).
	if marker, yes := mergeBuildGateDiskExhausted(output); yes {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain, Output: output,
			Err: mergeBuildGateDiskExhaustedErr(marker, dir)}
	}
	res.Status = mergeBuildGateStatusFail
	res.Output = output
	return res
}

// mergeBuildGateCommandWord returns the executable word of a leg's command line
// — the token `sh -c` will resolve through PATH. It is ONE helper because two
// callers need the same answer (the PATH pre-flight and its abstention text),
// and a command line's first field is the only thing either may look at.
func mergeBuildGateCommandWord(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// mergeBuildGateBranchAlreadyInTarget reports whether the TARGET already
// contains rev — the exact question `git merge` answers with "Already up to
// date." Diagnostics: `git merge-base --is-ancestor <rev> <target>` exits 0
// when rev is an ancestor of target, i.e. target carries everything rev has.
//
// DIRECTION MATTERS (first attempt got it backwards, and the gate's own
// incident fixture caught it): "target is an ancestor of rev" is true for
// EVERY branch ahead of the integration tip — the ordinary merge case — so
// that reading would abstain on exactly the non-compiling branch this gate
// exists to refuse. The truthful "adds nothing" relation is the reverse one.
//
// Any git error reads as FALSE (the conservative side: measure the branch,
// never claim a containment that is not proven — a wrong TRUE would abstain
// on a real refusal).
func mergeBuildGateBranchAlreadyInTarget(repo, rev, target string) bool {
	_, err := adoptionGit(repo, mergeBuildGateMergeBaseVerb, mergeBuildGateAncestorFlag, rev, target)
	return err == nil
}

// mergeBuildGateWorktreeDirName derives the worktree directory name for a rev.
// The rev is sanitised because it is a branch NAME (it may contain "/", which
// would otherwise nest the checkout a level deeper and make the removal path a
// different string from the add path).
func mergeBuildGateWorktreeDirName(rev string) string {
	safe := strings.Map(func(r rune) rune {
		// Both separators (the rev is a branch name, which may be
		// "feat/foo"; a nested checkout would make the removal path a
		// different string from the add path).
		if r == '/' || r == os.PathSeparator {
			return '-'
		}
		if r < ' ' {
			return -1
		}
		return r
	}, rev)
	if safe == "" {
		safe = mergeBuildGateWorktreeFallbackRev
	}
	return mergeBuildGateWorktreePrefix + safe
}

const (
	// mergeBuildGateWorktreePrefix prefixes every gate checkout inside the
	// scratch root (one scratch root holds exactly two).
	mergeBuildGateWorktreePrefix = "wt-"
	// mergeBuildGateWorktreeFallbackRev names the checkout when the revision
	// name sanitised away to nothing (never expected; an unusable name must
	// not collide with the other revision's directory).
	mergeBuildGateWorktreeFallbackRev = "rev"
)

// mergeBuildGateRun runs the build command line through sh -c in dir, with the
// gate's own Go caches. Returns the exit code, the combined output, and whether
// the command was killed by its own timeout (which the caller maps to ABSTAIN,
// never to FAIL).
func mergeBuildGateRun(dir string, spec mergeBuildGateSpec, goCache, sharedModCache string) (int, string, bool) {
	return mergeBuildGateRunCapped(dir, spec, goCache, sharedModCache, nil)
}

// mergeBuildGateRunCapped is mergeBuildGateRun with an optional CAPTURE SINK for
// the subprocess's combined output.
//
// sink != nil — the output is streamed into it and NOT returned as a string: the
// caller keeps what it decided to keep (the close-out check runner retains both
// ends of a `go test` run, whose failing block is printed last, so the tool's
// own head cap would delete exactly the window a refusal must quote). The
// returned string is always "" on that path.
// sink == nil — the whole output is captured and returned, unchanged: the
// build/vet callers of this helper read the head of it.
func mergeBuildGateRunCapped(dir string, spec mergeBuildGateSpec, goCache, sharedModCache string, sink io.Writer) (int, string, bool) {
	var ctx context.Context
	var cancel context.CancelFunc
	if spec.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(spec.TimeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", spec.Cmd)
	cmd.Dir = dir
	cmd.Env = mergeBuildGateEnv(goCache, sharedModCache)
	// Process-group isolation: a hung build is killed together with its
	// children at the deadline (the runBuild pattern in rebuild.go).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// and the pipes are abandoned at the deadline too: killing the leader is
	// not enough when a GRANDCHILD holds the (pipe) descriptors — measured, a
	// `sh -c 'sleep 30'` under a 1s budget made Wait block the full 30s, and
	// the SIGKILL below never got a chance to run. Rule 1: the bound is named.
	cmd.WaitDelay = mergeBuildGateWaitDelay
	var out []byte
	var err error
	if sink != nil {
		cmd.Stdout = sink
		cmd.Stderr = sink
		err = cmd.Run()
	} else {
		out, err = cmd.CombinedOutput()
	}
	// The kill is issued when the context is done — BEFORE judging the error —
	// so the process group is torn down even on the path where Wait returned
	// ErrWaitDelay (an orphan still holding the pipes) rather than only on the
	// deadline path that used to be reachable.
	if ctx.Err() != nil && cmd.Process != nil {
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH (the group already gone) is the expected error
	}
	if ctx.Err() == context.DeadlineExceeded {
		return -1, string(out), true
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out), false
		}
		// The command could not be started at all (no sh, permission): that is
		// a measurement failure, not a compile failure — ABSTAIN.
		return -1, err.Error(), false
	}
	return 0, string(out), false
}

// mergeBuildGateEnv assembles the build subprocess environment:
//
//	(a) buildVCSStatusEnv() — the rebuild's own contract: the caller's
//	    GIT_CONFIG_* entries are dropped and untracked files are excluded from
//	    the toolchain's dirty predicate. The gate builds a BRANCH TIP, so a
//	    binary stamped "-dirty" would be a false signal, and the cache key
//	    must not flip on an operator's shell state (Rule 15a);
//	(b) the gate's own GOCACHE — the SHARED, disk-backed, bounded per-repo cache
//	    (merge_build_gate_gocache.go) — plus the MODULE cache its builds resolve
//	    from: the operator's shared, READ-ONLY one by default
//	    (merge_build_gate_modcache.go), or the gate's own scratch cache for a
//	    build that cannot read modules. Neither is the operator's BUILD cache:
//	    a merge probe must not fill ~/.cache/go-build, and it must not depend on
//	    it either.
func mergeBuildGateEnv(cache, modCache string) []string {
	prod := buildVCSStatusEnv()
	env := make([]string, 0, len(prod)+2)
	for _, kv := range prod {
		// The gate sets these itself; a caller-supplied duplicate would leave
		// which value wins to getenv's first-match rule (the failure mode
		// buildVCSStatusEnv documents).
		if strings.HasPrefix(kv, mergeBuildGateGoCacheEnv+"=") ||
			strings.HasPrefix(kv, mergeBuildGateGoModCacheEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		mergeBuildGateGoCacheEnv+"="+cache,
		mergeBuildGateGoModCacheEnv+"="+modCache,
	)
}

// mergeBuildGateErrorText extracts the HEAD of a build's output (the
// diagnostics) — mergeBuildGateErrorLines non-empty lines, capped at
// mergeBuildGateErrorMaxBytes. `go build` prints diagnostics in the order the
// packages were visited, so the head is where the reason is; the tail is
// usually the summarising "exit status" noise.
func mergeBuildGateErrorText(out string) string {
	var b strings.Builder
	lines := 0
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if lines >= mergeBuildGateErrorLines || b.Len()+len(ln) > mergeBuildGateErrorMaxBytes {
			break
		}
		b.WriteString(ln)
		b.WriteString("\n")
		lines++
	}
	return strings.TrimRight(b.String(), "\n")
}

// mergeBuildGateUnresolvedModulesErr is the ABSTAIN reason for a build that
// could not resolve its modules: it names the asymmetry the gate can prove from
// its own environment (the scratch GOMODCACHE) so the log line reads as the
// measurement failure it is, not as an excuse.
const mergeBuildGateUnresolvedModulesErr = "the build could not resolve its modules in the gate's own scratch GOMODCACHE (no reachable module proxy) — the revision was never compiled"

// mergeBuildGateUnresolvedModules reports whether a failed build's output is a
// MODULE-RESOLUTION failure rather than a statement about the revision: the
// toolchain announced a download (mergeBuildGateDownloadPrefix) and then failed
// with one of the environment-absence markers (mergeBuildGateEnvAbsenceMarkers).
//
// Both halves are required. The announcement alone is not a failure (`go build`
// prints it on a build that then succeeds, and a healthy repo says it once);
// a marker alone is not this failure either (a compiler diagnostic can quote
// any text, and `dial tcp` appears in ordinary code under test). Together they
// are the one shape that cannot be a branch defect: the branch was never
// compiled, so the gate has no measurement to refuse on — it ABSTAINS, like a
// timeout (mergeBuildGateStatusAbstain).
//
// Both halves are matched case-insensitively: the toolchain's own spelling of a
// marker varies (`net/http: TLS handshake timeout` in an error chain, `i/o
// timeout` inside a dial error), and a marker is a symptom phrase rather than an
// identifier — the predicate must catch the class, not one spelling of it.
func mergeBuildGateUnresolvedModules(out string) bool {
	lower := strings.ToLower(out)
	if !strings.Contains(lower, strings.ToLower(mergeBuildGateDownloadPrefix)) {
		return false
	}
	for _, marker := range mergeBuildGateEnvAbsenceMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// mergeBuildGateDiskExhausted reports whether a failed build's output is the
// FILESYSTEM refusing a write rather than a statement about the revision, and
// returns the phrase that names it ("" when it is not this class).
//
// The class needs NO second half, unlike mergeBuildGateUnresolvedModules: the
// announcement there was evidence that the toolchain was talking about the
// NETWORK, while here the sentence IS the environment speaking. What it does
// need is a guard against the opposite error — a repo whose BUILD IS A TEST
// (this project's own fixtures, and any repo whose declared build runs a test
// suite) can print `no space left on device` inside a FAILING TEST'S NAME or
// message while the compilation itself failed for a real reason. So the class is
// claimed only when the output carries no compiler diagnostic at all
// (mergeBuildGateDiagnosticShape): if the toolchain pointed at a file, line and
// column, the revision was compiled and the gate has a measurement to refuse on.
// An output with no diagnostic and no marker is neither class (it stays FAIL —
// the 2026-09-13 incident shape).
//
// Matched case-insensitively: the phrase is a symptom, not an identifier, and
// the toolchain's spelling of it varies.
func mergeBuildGateDiskExhausted(out string) (marker string, yes bool) {
	if mergeBuildGateDiagnosticShape.MatchString(out) {
		return "", false
	}
	lower := strings.ToLower(out)
	for _, m := range mergeBuildGateDiskExhaustionMarkers {
		if strings.Contains(lower, strings.ToLower(m)) {
			return m, true
		}
	}
	return "", false
}

// mergeBuildGateDiskState renders the disk figures a disk-exhaustion abstention
// must carry: BOTH the filesystem that HOLDS path and `/` itself.
//
// WHY BOTH, MEASURED (this host, 2026-10-12): `/` sat at 25% with 131G free while
// `/home` — a separate physical partition holding every worktree, the Go build
// cache (119G) and the ambient TMPDIR — was at 100% with 2.1G free. A diagnostic
// that reported `/` alone would have printed "131G free" beside a build that died
// of ENOSPC, which is worse than printing nothing: it reads as a contradiction
// and sends the reader looking for a code defect. The two figures are rendered
// together so the partition which actually ran out is unmistakable, and the
// device ids are included because on a layout like this one the MOUNT NAMES are
// the only thing that distinguishes them (Rule 22 is judged by device id, never
// by a spelling).
//
// Zero-dependency and read-only (Rule 35): statfs via the leaf package the
// build-cache placement already measures with — no external command, no
// subprocess, nothing written. An unreadable path is REPORTED inside the string
// (Rule 15b), never silently replaced by a default.
func mergeBuildGateDiskState(path string) string {
	return fmt.Sprintf(mergeBuildGateDiskStateFmt, mergeBuildGateDiskSpace(mergeBuildGateMountOf(path)), mergeBuildGateDiskSpace(scratch.RootDeviceProbePath))
}

// mergeBuildGateMountOf names the mount point whose filesystem would hold path.
// It exists because a filesystem figure means nothing without the path it was
// taken at, and because on this host the interesting path and `/` are DIFFERENT
// partitions (see mergeBuildGateDiskState).
func mergeBuildGateMountOf(path string) string {
	if strings.TrimSpace(path) == "" {
		return scratch.RootDeviceProbePath
	}
	return path
}

// mergeBuildGateDiskSpace renders "<path> (<device>, free <n>, <pct>% used)" or
// the measurement failure — never a guessed figure.
func mergeBuildGateDiskSpace(path string) string {
	free, err := scratch.FreeBytes(path)
	if err != nil {
		return fmt.Sprintf(mergeBuildGateDiskUnmeasurableFmt, path, err)
	}
	dev, devErr := scratch.DeviceID(path)
	if devErr != nil {
		return fmt.Sprintf(mergeBuildGateDiskUnmeasurableFmt, path, devErr)
	}
	return fmt.Sprintf(mergeBuildGateDiskSpaceFmt, path, dev, scratch.FormatBytes(free))
}

const (
	// mergeBuildGateRootPath is `/` — spelled once here (Rule 13), for the
	// figure that must always accompany a disk-exhaustion abstention. It is the
	// same partition internal/scratch's own probe protects
	// (scratch.RootDeviceProbePath); this constant only NAMES it at the place a
	// human reads.
	mergeBuildGateRootPath = "/"

	// mergeBuildGateDiskStateFmt / mergeBuildGateDiskSpaceFmt /
	// mergeBuildGateDiskUnmeasurableFmt render the double figure. The free
	// space is formatted as bytes because a human's `df -h` rounding is exactly
	// what hides a 2G remainder.
	mergeBuildGateDiskStateFmt        = "disk state: %s; %s"
	mergeBuildGateDiskSpaceFmt        = "%s (device 0x%x, %s free)"
	mergeBuildGateDiskUnmeasurableFmt = "%s (filesystem unmeasurable: %v)"
)

// mergeBuildGateDiskExhaustedErr is the ABSTAIN reason for a build the
// filesystem killed. It states the class, quotes the marker that named it, gives
// both partitions' figures, and names the two facts that make this an
// environment failure rather than a branch defect: the revision was never
// linked, and nothing about the code was measured.
func mergeBuildGateDiskExhaustedErr(marker, path string) string {
	return fmt.Sprintf(mergeBuildGateDiskExhaustedErrFmt, marker, mergeBuildGateDiskState(path))
}

const (
	// mergeBuildGateDiskExhaustedErrFmt is that reason. It names the cause
	// ("the filesystem refused the write"), the consequence (the revision was
	// NEVER LINKED, so nothing about the branch was measured) and the figures.
	mergeBuildGateDiskExhaustedErrFmt = "the build died of disk exhaustion (%q): the filesystem refused the write, so the revision was NEVER LINKED and nothing about the branch was measured — %s"
)

func mergeBuildGateTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= mergeBuildGateLogTailMax {
		return s
	}
	return s[:mergeBuildGateLogTailMax] + "…"
}

// mergeBuildGateDisabled reports whether the operator escape hatch is set.
// Rule 2: this is the ONE reader of mergeBuildGateEnvName; unset (or any other
// value) means ENABLED.
func mergeBuildGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(mergeBuildGateEnvName)) == mergeBuildGateDisabledValue
}
