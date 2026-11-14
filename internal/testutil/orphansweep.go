package testutil

// The pre-test orphan sweep: the BACKSTOP for daemons a KILLED test binary left
// behind.
//
// THE DEFECT (2026-09-14): the lifetime binding (daemonlifetime.go) is the fix
// for daemons started after it shipped. It cannot help with an orphan that is
// ALREADY running — a daemon started by a test binary that was SIGKILLed before
// the binding existed, or by a run where the binding was not armed. Such a
// daemon has PPID=1, is its own session leader, holds its sandbox on /dev/shm
// open and keeps writing there for hours (the tmpfs reached 100%).
//
// WHY A SWEEP IS SAFE HERE — and why it is not the pkill the house rules ban:
//
//   - it is SCOPED BY PATH, never by name. A candidate is swept only when a
//     path it OWNS provably lies inside the sweep root: its cwd, its GOPI_HOME,
//     or one of its open files. The root is the suite's OWN scratch run
//     directory, so the worst possible mistake is killing a process that
//     belongs to the same scratch tree this suite is about to use.
//   - it requires the ORPHAN SHAPE. Only a session leader (pid == session)
//     reparented to the process that REAPS this host is eligible — init (1) on
//     a plain host, `systemd --user` on a host with a user session (measured
//     here: ppid 3100). A daemon whose spawner is still alive belongs to that
//     (live) test process and must not be touched; a process that is nobody's
//     child in that sense is not a leak of OURS.
//   - it signals by IDENTITY, not by number: the pid+starttime+cmdline triple
//     is re-read immediately before every signal, so a pid the kernel recycled
//     between the scan and the kill is left alone (KillIfIdentical).
//   - it never sweeps a process it cannot READ: an unreadable /proc entry is a
//     fact about a stranger, not a licence to signal.
//
// The sweep runs from TestMain BEFORE m.Run — i.e. before any test has started a
// daemon of its own — so it can never race a live fixture.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// orphanSweepTermGrace is how long a swept daemon gets to exit on SIGTERM
	// before SIGKILL. A gopi daemon runs a cancellation path (flush, lock
	// release), so a short polite window is right — but the sweep is entitled
	// to be impatient: nothing is waiting on the daemon any more.
	orphanSweepTermGrace = 2 * time.Second
	// orphanSweepKillGrace is the post-SIGKILL observation window. SIGKILL
	// cannot be ignored, so this only covers the kernel's teardown; a process
	// still present after it is REPORTED (never silently skipped).
	orphanSweepKillGrace = 2 * time.Second
	// orphanSweepPoll is the liveness poll step inside those windows.
	orphanSweepPoll = 20 * time.Millisecond
	// orphanSweepMinPID is the smallest pid a sweep may signal. Same guard as
	// the reaper (mockproc.go) and for the same reason: kill(-0)/pid 1 are
	// catastrophic, and the init process is never a leaked test daemon.
	orphanSweepMinPID = 2
	// orphanSweepFDLimit bounds how many of a candidate's open files are
	// inspected — enough to see a sandbox, bounded so a process with 100k fds
	// cannot make the sweep (which runs before EVERY suite) expensive.
	orphanSweepFDLimit = 64
	// deletedSuffix is what the kernel appends to a /proc/<pid>/cwd or fd link
	// whose target has been unlinked. A leaked daemon's sandbox is normally
	// DELETED (t.TempDir cleanup ran, or the run's scratch was GC'd) — the
	// suffix must be stripped before the path containment test, or the sweep
	// would never recognize its own leak.
	deletedSuffix = " (deleted)"
)

// OrphanSweepRootEnv is the operator override that tells a sweep which tree to
// scope itself to. It exists for the red-proof: a regression test must be able
// to point the sweep at ITS OWN throwaway root and prove the sweep finds the
// leak there — without ever aiming a sweep at the developer's real scratch tree
// (Rule 6: a test never touches real state).
const OrphanSweepRootEnv = "GOPI_TEST_ORPHAN_SWEEP_ROOT"

// SweptProcess is one process the sweep acted on, with the evidence that
// justified it (Rule 15b: a kill always names its proof).
type SweptProcess struct {
	Identity ProcIdentity
	// Proof names the owned path that placed the process inside the scope.
	Proof string
	// Outcome is what happened: "terminated", "killed" or "survived".
	Outcome string
}

// OrphanCandidate is a process that PROVABLY belongs to root: the orphan shape
// plus an owned path inside the scope. Verb "candidate" is deliberate — it names
// what the evidence proves (ownership), not what the caller will do with it.
type OrphanCandidate struct {
	Identity ProcIdentity
	// Proof names the owned path that placed the process inside the scope.
	Proof string
}

// scope decides whether a path a candidate OWNS places it inside the sweep. It
// is a function so the two legitimate scopes can share one implementation: a
// single tree (one run root — what a regression test uses) and a suite's run
// PREFIX (every run root of one suite, which is what a backstop needs, since the
// leak belongs to a PREVIOUS run).
type scope func(string) bool

// scopeOfRoot matches exactly one tree.
func scopeOfRoot(root string) scope {
	root = strings.TrimRight(stripDeletedSuffix(root), "/")
	return func(p string) bool { return PathWithin(root, p) }
}

// scopeOfPrefix matches any path under a prefix — the suite-run scope
// ("…/suite-cmd-agent-run-" matches "…/suite-cmd-agent-run-987654321").
func scopeOfPrefix(prefix string) scope {
	return func(p string) bool {
		// A PATH prefix, not a string prefix: PathWithin compares path
		// components, so "…/suite-cmd-agent-run-" matches every run root of that
		// suite (run-123, run-456 …) and never a sibling suite whose name merely
		// starts with the same letters (Rule 13's discipline applied to a scope:
		// one definition of "inside", not a second, looser comparison here).
		return PathWithin(prefix, p) || strings.HasPrefix(p, prefix)
	}
}

// FindOrphansUnderRoot returns the daemons orphaned inside root, WITHOUT
// signalling anything. It is the read-only half, and the one a regression test
// uses: "the leak count must be zero" is an assertion, not a cleanup.
//
// An empty root is refused rather than treated as "everything": an unscoped
// sweep is the accident this must never enable.
func FindOrphansUnderRoot(root string) ([]OrphanCandidate, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("orphan sweep needs a scope root — refusing to sweep unscoped")
	}
	return FindOrphansInScope(scopeOfRoot(root))
}

// FindOrphansInScope is the shared scan: every pid, then the shape test, then
// the ownership proof.
//
// The REAPER and the ANCESTRY CHAIN are measured ONCE here, for the same reason
// the scan is one pass: both are properties of this host and this reader, so
// re-deriving them per candidate would walk the whole pid table per process.
// They travel to the predicate through the identity's stamps (Orphan() is the
// only reader). Neither is silently defaulted: a host whose reaper CANNOT be
// named ends the scan with an ERROR instead of an empty result — "the sweep
// cannot see" and "the scene is clean" must never be the same answer — while a
// chain that could not be walked to the top of the tree is a per-candidate
// refusal, printed (see orphanCandidate), because the candidates found through
// the reaper's own name are still swept correctly.
func FindOrphansInScope(in scope) ([]OrphanCandidate, error) {
	if in == nil {
		return nil, fmt.Errorf("orphan sweep needs a scope — refusing to sweep unscoped")
	}
	// THE REQUIRED MEASUREMENT IS THE CHAIN, NOT THE NAME. Refusing a host whose
	// reaper cannot be named was the pre-fix framing, and it is wrong in the same
	// direction as the defect it replaced: the name is only ONE of the two arms,
	// while the ancestry chain is the one that is always measurable, and it is
	// what makes the kernel's nearest marked subreaper recognizable at all.
	// Measured: launched under a marked subreaper (this run's own launcher), the
	// name arm finds nothing and the old refusal made every case error out before
	// judging a single process.
	//
	// WHAT CANNOT BE SKIPPED IS A CHAIN THAT WAS NOT WALKED TO THE TOP: without a
	// closed chain the sweep cannot tell its own leak from a stranger's process,
	// and it refuses rather than guessing (Rule 15b).
	ancestry, chainClosed := ProcReaperChainOf()
	if !chainClosed {
		return nil, fmt.Errorf("orphan sweep cannot walk this run's ancestry to the top of the tree — it cannot tell its own leak from a third party's process: %w",
			&procReaperUnknownError{reason: fmt.Sprintf("%d level(s) of ancestry measured, cap %d", len(ancestry), procReaperChainMaxLevels)})
	}
	// The name arm is optional, and its absence is announced rather than
	// swallowed: on a marked-subreaper host it is EXPECTED to find nothing.
	reaper, reason := ProcReaperOf()
	if reaper <= 0 {
		fmt.Fprintf(os.Stderr, "testutil: orphan sweep: this host's reaper cannot be named (%s) — the ancestry-chain arm still applies: a process reparented to an ancestor of this run is still swept\n", reason)
	}
	self := os.Getpid()
	pids, err := ListPIDs()
	if err != nil {
		return nil, fmt.Errorf("orphan sweep enumerate: %w", err)
	}
	var found []OrphanCandidate
	for _, pid := range pids {
		if pid < orphanSweepMinPID || pid == self {
			continue
		}
		if c, ok := orphanCandidate(pid, in, reaper, ancestry); ok {
			found = append(found, c)
		}
	}
	return found, nil
}

// procReaperUnknownError is the refusal above, as a TYPE: a caller (or a
// regression test) can ask "was this a parenting the sweep could not establish?"
// without matching on the message, which is the part that will be reworded.
type procReaperUnknownError struct{ reason string }

func (e *procReaperUnknownError) Error() string {
	return "cannot establish this run's parenting well enough to judge eligibility (" + e.reason + ")"
}

// SweepOrphansUnderRoot finds the daemons orphaned inside root and terminates
// each one.
//
// It returns a row per process it ACTED on (empty when the tree is clean — the
// normal case) plus an error only for a failure to ENUMERATE (a missing
// /proc); per-process failures are part of the returned rows, because a sweep
// that cannot kill one orphan must still clean up the rest.
func SweepOrphansUnderRoot(root string) ([]SweptProcess, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("orphan sweep needs a scope root — refusing to sweep unscoped")
	}
	return sweepInScope(scopeOfRoot(root))
}

// sweepInScope terminates every orphan the scope matches.
func sweepInScope(in scope) ([]SweptProcess, error) {
	candidates, err := FindOrphansInScope(in)
	if err != nil {
		return nil, err
	}
	swept := make([]SweptProcess, 0, len(candidates))
	for _, c := range candidates {
		swept = append(swept, terminateSwept(c.Identity, c.Proof))
	}
	return swept, nil
}

// orphanCandidate decides one pid: is it an orphaned daemon that provably owns a
// path inside root?
//
// reaper is THIS host's reaper and ancestry this run's own ancestor chain — both
// measured once by the caller (FindOrphansInScope) and stamped onto the
// identity, so the predicate is told what reaping means here and which
// reparenting targets are provably ours.
func orphanCandidate(pid int, in scope, reaper int, ancestry []int) (OrphanCandidate, bool) {
	id, err := ReadProcIdentity(pid)
	if err != nil {
		return OrphanCandidate{}, false // gone, or not ours to read
	}
	id.ProcReaper = reaper
	id.ProcReaperChain = ancestry
	// ORPHAN SHAPE: a session leader reparented to the process that reaps this
	// host — either the one this sweep measured by name, or an ancestor of this
	// run (the kernel's nearest marked subreaper, which below Linux 6.10 no
	// /proc field exposes; see ProcIdentity.Orphan). Anything else is either a
	// live test's daemon (its spawner is alive) or an unrelated process, and
	// neither is swept.
	if !id.Detached() || !id.Orphan() {
		return OrphanCandidate{}, false
	}
	proof, ok := ownershipProof(pid, in)
	if !ok {
		return OrphanCandidate{}, false
	}
	return OrphanCandidate{Identity: id, Proof: proof}, true
}

// ownershipProof reports whether a path this process OWNS lies inside root, and
// names it. Three independent inputs, cheapest first:
//
//  1. the process's working directory — the daemon spawn seam hands every
//     detached daemon a measured directory (daemoncwd), which for a test-spawned
//     daemon is its sandbox;
//  2. GOPI_HOME from /proc/<pid>/environ — the sandbox it is writing into;
//  3. its open files — a daemon holding a lock, ledger or log inside the tree.
//
// Every returned path has the kernel's " (deleted)" marker stripped: a leaked
// sandbox is usually already unlinked, and a raw link target would never match.
func ownershipProof(pid int, in scope) (string, bool) {
	if cwd, err := Cwd(pid); err == nil {
		if c := stripDeletedSuffix(cwd); in(c) {
			return "cwd " + c, true
		}
	}
	if home, err := EnvValue(pid, GopiHomeEnvName); err == nil && home != "" {
		if h := stripDeletedSuffix(home); in(h) {
			return GopiHomeEnvName + "=" + h, true
		}
	}
	if fds, err := ownedOpenPaths(pid, in); err == nil && len(fds) > 0 {
		return "open " + fds[0], true
	}
	return "", false
}

// ownedOpenPaths lists the targets of pid's open descriptors that lie inside
// root (bounded by orphanSweepFDLimit). A descriptor that cannot be readlink'ed
// is skipped — it belongs to a race, not to this decision.
func ownedOpenPaths(pid int, in scope) ([]string, error) {
	dir := filepath.Join(procDir, strconv.Itoa(pid), procFDName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var found []string
	for i, e := range entries {
		if i >= orphanSweepFDLimit || len(found) > 0 {
			break
		}
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if t := stripDeletedSuffix(target); in(t) {
			found = append(found, t)
		}
	}
	return found, nil
}

// terminateSwept stops one confirmed orphan: SIGTERM, a bounded wait, SIGKILL,
// a bounded wait, and an honest outcome string. Every signal goes through
// KillIfIdentical, so a pid recycled mid-sweep is left alone.
func terminateSwept(id ProcIdentity, proof string) SweptProcess {
	row := SweptProcess{Identity: id, Proof: proof}
	if err := KillIfIdentical(id, syscall.SIGTERM); err != nil {
		// Not a failure to clean up: the process changed identity or left, and
		// a reused pid is exactly what must NOT be signalled.
		row.Outcome = "skipped: " + err.Error()
		return row
	}
	if !waitGone(id, orphanSweepTermGrace) {
		if err := KillIfIdentical(id, syscall.SIGKILL); err != nil {
			row.Outcome = "skipped: " + err.Error()
			return row
		}
		if !waitGone(id, orphanSweepKillGrace) {
			row.Outcome = "survived SIGTERM and SIGKILL"
			return row
		}
		row.Outcome = "killed"
		return row
	}
	row.Outcome = "terminated"
	return row
}

// waitGone polls the identity until it is no longer the same incarnation, or the
// window expires.
func waitGone(id ProcIdentity, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		if alive, _ := id.Alive(); !alive {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(orphanSweepPoll)
	}
}

// stripDeletedSuffix removes the kernel's " (deleted)" marker from a procfs link
// target.
func stripDeletedSuffix(p string) string {
	return strings.TrimSuffix(p, deletedSuffix)
}

// GopiHomeEnvName is the environment variable that names the gopi data root a
// process is using — the ownership input that survives a deleted cwd. It is an
// ALIAS of internal/config's GopiDirEnv, never a second literal (Rule 13/14):
// the daemon reads exactly this name, and a sweep that spelled it differently
// would silently stop attributing anything. The alias is a reader in the sense
// the env-name gate asks for: EnvValue (below) parses the variable out of
// /proc/<pid>/environ, and that parse is the only use.
const GopiHomeEnvName = config.GopiDirEnv

// SuiteOrphanScopeRoots is the ONE resolver of the suite-start sweep's scope:
// every path prefix a run root of THIS suite can be created UNDER — the aim the
// backstop and the scene placement both take their tree from.
//
// WHY IT EXISTS — THE DEFECT IT REPLACES (measured on this host, 2026-10-12):
// the scope used to be a SINGLE prefix derived from the scratch home
// (ScratchHomeDir), while the pin creates the run root under whichever parent
// scratchPlacementFor returned — and under placementMemoryBase that parent is the
// host MEMORY BASE (/dev/shm), a DIFFERENT TREE from the scratch home. Measured
// before this function existed: /dev/shm held 170 `suite-*-run-*` roots (3.7G)
// against exactly 1 in the home tree, so the backstop's aim and the pin's landing
// site were two expressions of one fact that had drifted apart: everything a
// KILLED run left in the memory base — its detached daemons included — was
// invisible to the sweep that exists to reap exactly that.
//
// It reads NO NEW CHANNEL (Rule 2) and it decides nothing the pin has not already
// decided: it reports the pin's OWN recorded landing site (ScratchPlacementDir,
// set by PinSuiteTempRoot) plus the parents a PREVIOUS run of this suite can have
// landed under. It deliberately does NOT call scratchPlacementFor — that
// function's saturation arm RECLAIMS directories (reclaimSaturatedCandidate
// deletes), and a scope resolver may never delete anything.
//
// The membership is NOT a licence to widen the scope: every member is the
// "<parent>/suite-<suite>-run-" prefix of THIS SUITE under a parent the pin
// itself uses, so a sibling suite, the scratch queue, another program's directory
// in the same base, and every process holding none of those paths stay untouched
// — the same ownership predicate as before, over the same namespace, aimed at the
// trees the pin actually lands in.
func SuiteOrphanScopeRoots(suite string) []string {
	pattern := scratchRunDirName(suite)
	home := ScratchHomeDir()
	if SuiteTempRootOverride() != "" {
		// The caller named the scratch HOME itself (a fixture, per
		// PinSuiteTempRoot): the scope is THEIR tree and only it — the memory base
		// is deliberately NOT added, the same stand LegacyScratchBases takes, since
		// sweeping a host's shared base on behalf of a run that pointed its home
		// elsewhere would collect outside the tree the caller owns. Both shapes the
		// pin produces under that home are scoped: the home itself, and the per-run
		// fixture directory it creates inside it.
		roots := []string{filepath.Join(home, pattern)}
		return appendScopeRoot(roots, home, fixtureRunDirName(suite))
	}
	roots := []string{filepath.Join(home, pattern)}
	if dir := ScratchPlacementDir(); dir != "" {
		roots = appendScopeRoot(roots, dir, pattern)
	}
	if SuiteTempRootPinned() {
		// The pin's landing site is the authoritative member: it is where THIS run
		// put its root, so it is a parent the pin demonstrably uses — and it is the
		// member that covers a caller-pinned TMPDIR or a scratch home that is
		// itself memory-backed, with no second read of either channel.
		roots = appendScopeRoot(roots, filepath.Dir(SuiteTempRoot()), pattern)
	}
	if base := MemoryBase(); base != "" {
		if mem, _ := ScratchRootFilesystem(base); mem {
			// The arm that answers for a PAST run whose placement differs from this
			// one (the pin has no memory of them): the memory base is where the
			// placement puts a suite whenever it offers room.
			roots = appendScopeRoot(roots, base, pattern)
		}
	}
	return roots
}

// appendScopeRoot adds "<dir>/<pattern>" to roots unless an equivalent member is
// already there. Two spellings of one directory (the base reached through a
// symlink, a trailing separator, the pin's canonicalised reading against the raw
// channel) are ONE scope member: a duplicate would only make a reader believe two
// trees are swept where one is.
func appendScopeRoot(roots []string, dir, pattern string) []string {
	candidate := filepath.Join(dir, pattern)
	for _, existing := range roots {
		if filepath.Clean(existing) == filepath.Clean(candidate) {
			return roots
		}
	}
	return append(roots, candidate)
}

// SuiteOrphanScopePrefix is the PRIMARY member of the resolved scope — the
// scratch-home prefix. It is kept for the callers that need ONE path (the scene
// placement in this package's regression, and the message a sweep prints); the
// backstop itself sweeps the WHOLE set (SuiteOrphanScopeRoots), because a run root
// created under the memory base is not under this prefix.
func SuiteOrphanScopePrefix(suite string) string {
	return SuiteOrphanScopeRoots(suite)[0]
}

// sweptKey is the identity a sweep de-duplicates on: the pid TOGETHER with the
// kernel start time. A pid alone is not an identity (the kernel recycles it), and
// two scope members can both match one process — the row must appear once, with
// one signal.
type sweptKey struct {
	pid       int
	startTime uint64
}

// SweepOrphansAtSuiteStart is the pre-test hook a suite's TestMain calls after
// testutil.PinSuiteForTestMain: it sweeps the ORPHANS OF THIS SUITE (its own run
// roots, past and present — the GOPI_TEST_ORPHAN_SWEEP_ROOT override replaces
// that scope for a fixture) and reports what it found.
//
// It runs BEFORE m.Run, so this run has started no daemon yet: everything it can
// see is a leftover from an earlier run, which is exactly the leak (a test
// binary that was SIGKILLed left its daemons behind, and those daemons are what
// filled the tmpfs). It is deliberately non-fatal about sweep failures (a suite
// must still run if procfs hiccups) but never silent: every swept row and every
// error goes to stderr, because "the leak came back" is the one thing this
// backstop exists to make visible.
func SweepOrphansAtSuiteStart(suite string) []SweptProcess {
	var (
		swept []SweptProcess
		err   error
		where string
	)
	if override := os.Getenv(OrphanSweepRootEnv); override != "" {
		where = override
		swept, err = SweepOrphansUnderRoot(override)
	} else {
		// THE WHOLE RESOLVED SCOPE, not one prefix: the pin creates the run root
		// under whichever parent the placement chose, and under memory-base that
		// parent is the memory base — a different tree from the scratch home. A
		// single-prefix aim therefore leaves a killed run's daemons in the memory
		// base unreapable (see SuiteOrphanScopeRoots for the measurement).
		roots := SuiteOrphanScopeRoots(suite)
		where = strings.Join(roots, scopeRootSeparator)
		swept, err = sweepScopeRoots(roots)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: %s: orphan sweep of %s failed: %v\n", suite, where, err)
		return nil
	}
	for _, row := range swept {
		fmt.Fprintf(os.Stderr, "testutil: %s: swept orphan daemon %s (%s) — %s\n", suite, row.Identity, row.Proof, row.Outcome)
	}
	return swept
}

// scopeRootSeparator joins the scope members for one report line, so a reader of
// the sweep's log sees the whole aim rather than a representative member of it.
const scopeRootSeparator = " + "

// FindOrphansInScopeRoots returns the orphans matched by ANY member of roots,
// WITHOUT signalling anything, with each process appearing exactly ONCE.
//
// THE DE-DUPLICATION IS BY IDENTITY, NOT BY PATH. Two scope members can both
// match one process — a memory base that is also the caller's TMPDIR is ONE
// directory spelled twice — and a process owns several paths (its cwd, its
// GOPI_HOME, its open files). Without the de-duplication such a process would
// appear twice in the rows and, worse, be signalled twice; the key is the pid
// TOGETHER with the kernel start time, because a bare pid is not an identity
// (the kernel recycles it between two members' scans, and the second scan's row
// would then be a stranger).
//
// A member whose scan FAILED is reported on stderr and the sweep continues: the
// members are disjoint trees, so one unreadable member must not silently cancel
// the reaping of another (Rule 15b — reported, never swallowed). Only when EVERY
// member failed is an error returned, because then nothing at all was swept and
// "the tree is clean" must not be the answer (the same distinction
// FindOrphansInScope draws for a single root).
func FindOrphansInScopeRoots(roots []string) ([]OrphanCandidate, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("orphan sweep needs a scope — refusing to sweep unscoped")
	}
	seen := make(map[sweptKey]bool)
	var (
		found    []OrphanCandidate
		firstErr error
		failures int
	)
	for _, root := range roots {
		in, err := FindOrphansInScope(scopeOfPrefix(root))
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("scope member %s: %w", root, err)
			}
			failures++
			fmt.Fprintf(os.Stderr, "testutil: orphan sweep: scope member %s could not be scanned: %v\n", root, err)
			continue
		}
		for _, c := range in {
			key := sweptKey{pid: c.Identity.Pid, startTime: c.Identity.StartTime}
			if seen[key] {
				continue
			}
			seen[key] = true
			found = append(found, c)
		}
	}
	if failures == len(roots) {
		return nil, firstErr
	}
	return found, nil
}

// sweepScopeRoots terminates every orphan the resolved scope matches, once per
// process (the de-duplication happens in the read half, so no process is
// signalled twice for matching two members).
func sweepScopeRoots(roots []string) ([]SweptProcess, error) {
	candidates, err := FindOrphansInScopeRoots(roots)
	if err != nil {
		return nil, err
	}
	swept := make([]SweptProcess, 0, len(candidates))
	for _, c := range candidates {
		swept = append(swept, terminateSwept(c.Identity, c.Proof))
	}
	return swept, nil
}
