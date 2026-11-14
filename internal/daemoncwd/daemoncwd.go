// Package daemoncwd decides the working directory a LONG-LIVED child (a
// detached daemon, an executor child that can outlive the round that started
// it) must START in.
//
// WHY THIS EXISTS — the producer side of the worktree-reclaim deadlock
// (epic daemon-cwd-escape, 2026-09-16):
//
// A spawned process inherits the LAUNCHER's working directory. When the
// launcher stands inside a task's linked worktree (an agent session running in
// its own worktree is the normal case), the child keeps that directory as its
// cwd for its whole lifetime. For a daemon that lifetime is unbounded, so
// guard ③ of the reclaim arm — finishCleanupWorktreeInUse in
// internal/tools/task_finish_cleanup.go, which refuses to remove a worktree
// with a live process inside it — can never fire FOR THAT DIRECTORY. The
// directory is then permanently un-recyclable: the branch is delivered, the
// merge happened, and the worktree still sits there because an immortal
// process is standing in it.
//
// THE FIX IS A PRODUCER OBLIGATION, NEVER A GUARD WEAKENING. Guard ③ is
// correct: a live cwd inside a worktree is a real reason not to tear the
// directory out from under it. What is wrong is SPAWNING a long-lived process
// into that directory in the first place. So every long-lived child is given a
// cwd that is:
//
//	① provably OFF the `/` partition (PROJECT.md Rule 22 — a MEASURED device id,
//	   never a spelling: /tmp is on `/` on this host, so "not /tmp" is not the
//	   criterion), and
//	② provably not inside the worktree the child descends from — and not
//	   inside the operator's declared worktree roots either.
//
// When (②) cannot be proven for the scratch tree (an operator pointed
// GOPI_WORKTREE_ROOT, or a repo's declared worktree_root, at it), the escape
// directory under that tree is refused too and the process root degrades to
// procfs, which is always present where the measurement itself works. The
// INVARIANT is never traded away for a convenience path.
//
// THE MEASUREMENT IS THE VERDICT (Rule 15: never silent). Every escape is
// reported with the directory it came from, and the acceptance test measures
// /proc/<pid>/cwd of a REAL spawned child — never an assertion that a struct
// field was assigned.
package daemoncwd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

// --- named constants (Rules 1/1b/13: one place, referenced by name) ---

const (
	// EnvName is the operator's override for the daemon process root. It has
	// exactly ONE reader — resolve (Rule 2).
	EnvName = "GOPI_DAEMON_CWD"

	// ReportFmt is the line a caller prints when a child ESCAPED its inherited
	// directory: what is used, what was left behind, and why. A daemon that
	// moved silently is a daemon whose cwd an operator cannot check — the
	// documented follow-up is `ls -l /proc/<pid>/cwd`.
	ReportFmt = "%s: daemon cwd %s (left the inherited directory %s behind: a process standing in a task worktree vetoes its recycle for its whole lifetime)\n"

	// FallbackFmt is the line printed when NO directory could be proven off
	// `/`: the inherited directory is kept and the report says so, rather than
	// degrading in silence (Rule 22: the implicit layer is the defect).
	FallbackFmt = "%s: no daemon cwd is provably off the / partition — keeping the inherited directory %s (its worktree will not be recyclable while this process lives)\n"

	// EscapeDirLeaf is the last path component of the escape directory under
	// the scratch runtime tree.
	EscapeDirLeaf = "process"
	// RuntimeDirName is the scratch class directory the escape lives in. It is
	// an alias of scratch.RuntimeDirName (Rule 14: the value has ONE definition
	// — the scratch package owns it) rather than a second literal.
	runtimeDirName = scratch.RuntimeDirName

	// DirPerm is the mode of the directories this package creates.
	DirPerm = 0o755

	// Argv0Prefix prefixes a detached daemon's component name to form its
	// argv[0] ("gopi-merger", "gopi-worker") — the stable process name the
	// daemon advertises, and therefore what /proc/<pid>/cmdline[0] reports.
	// MEASURED, not assumed: /proc/<pid>/comm does NOT carry it (the kernel
	// names comm after the executable — a real daemon showed comm=probe-gopi
	// with cmdline[0]=gopi-merger), which is why the identity check reads
	// cmdline and never comm.
	//
	// It lives HERE (Rule 14: one definition for a value two packages must
	// agree on) because both sides need it: cmd/agent builds the argv[0] from
	// it, and internal/tools recognises a DAEMON holder by it when it reports
	// why a worktree is in use. A second literal on either side would drift
	// silently out of agreement.
	Argv0Prefix = "gopi-"

	// gitDirName is git's administrative entry (the linked-worktree marker).
	gitDirName = ".git"

	// procRootDir / procCwdName / procStatName are the kernel interfaces the
	// measurements read. Constants, not literals: a typo'd path would silently
	// disable a measurement (Rule 1b). Their VALUES live in internal/procname —
	// the leaf package that owns every process fact this repo reads — and are
	// referenced from here (Rules 13/14).
	procRootDir  = procname.ProcRootDefault
	procCwdName  = procname.CwdName
	procStatName = procname.StatName
	// procCmdlineName is where a process's argv[0] lives — the DAEMON IDENTITY
	// channel (see IsDaemonArgv0 for why it, and not comm, is the right one).
	procCmdlineName = procname.CmdlineName
	// procEnvironName / procEnvironReadLimit are the SECOND evidence channel of
	// the test-artifact classification (see IsTestScratchHolder): the exec-time
	// environment block, which is where a child's INHERITED scratch/temp root
	// survives for the child's whole life, whatever its cwd later becomes.
	procEnvironName = "environ"
	// procEnvironReadLimit bounds the raw-environment read: /proc reports no size
	// up front, so the block is capped rather than allocated whole — far above
	// any real block, and a truncated tail can only hide a variable this
	// predicate does not depend on.
	procEnvironReadLimit = 1 << 20
	// statPPIDField is the 0-based field of /proc/<pid>/stat carrying the
	// parent pid — counted AFTER the comm field's closing parenthesis. It is
	// DERIVED from the field NUMBER internal/procname owns (Rule 1a: the field
	// number is the fact, the index is arithmetic; ending the derivation at the
	// last non-addressable field keeps the two from drifting).
	statPPIDField = procname.StatFieldParent - procname.StatFieldState
	// ancestryMaxHops bounds a parent walk so a cycle can never hang a spawn.
	ancestryMaxHops = 64
)

// ErrNoProcessRoot reports that no provably-off-`/` directory exists on this
// host (no scratch tree and no readable procfs). The caller keeps the
// inherited directory and reports it — never invents a path.
var ErrNoProcessRoot = errors.New("no livable process root: no scratch tree and no readable procfs")

// deps are the measurement seams. Production callers use Resolve (all
// defaults); the in-package test injects a fake tree, because "off the /
// partition" cannot be produced with a real temp directory on a host where
// /tmp IS on `/`.
type deps struct {
	// procRoot is the procfs mount to read.
	procRoot string
	// scratchHome is the scratch tree the escape directory lives in.
	scratchHome string
	// deviceID measures a path's device id.
	deviceID func(string) (uint64, error)
	// rootDeviceID measures the `/` partition's device id.
	rootDeviceID func() (uint64, error)
	// parentOf reads a process's parent pid.
	parentOf func(int) (int, bool)
	// readCwd reads a process's working directory.
	readCwd func(int) (string, bool)
	// worktreeRoots are the declared roots the answer must stay outside of.
	worktreeRoots []worktreeroots.Root
}

// Resolve returns the directory a long-lived child of THIS process must start
// in, plus a non-empty reason when the inherited directory had to be left
// behind ("" = nothing to escape). The caller passes dir to the child and
// reports escapedFrom via Report.
//
// launchDir is where the child would inherit its cwd from (the launcher's
// working directory). worktreeOf names the task worktree the child descends
// from when the caller knows it (an executor child reads its row's
// deliverable); empty means "derive it from the launcher's ancestors".
//
// An error means no usable process root exists at all — the caller then keeps
// the inherited directory (and reports it), which is strictly better than
// inventing one.
func Resolve(launchDir, worktreeOf string) (dir, escapedFrom string, err error) {
	d, derr := productionDeps()
	if derr != nil {
		return strings.TrimSpace(launchDir), "", derr
	}
	return resolveWith(d, launchDir, worktreeOf)
}

// Report prints the escape — or the degradation — for one resolved child. It
// is the ONE renderer of both lines (Rule 15: the decision is never silent).
func Report(prog, dir, escapedFrom string) {
	switch {
	case escapedFrom == "":
		return
	case escapedFrom == dir:
		fmt.Fprintf(os.Stderr, FallbackFmt, prog, dir)
	default:
		fmt.Fprintf(os.Stderr, ReportFmt, prog, dir, escapedFrom)
	}
}

// resolveWith is Resolve against injected dependencies (the test seam).
func resolveWith(d deps, launchDir, worktreeOf string) (string, string, error) {
	inherited := strings.TrimSpace(launchDir)
	worktree := resolveWorktreeOf(d, inherited, worktreeOf)

	// ① THE OPERATOR'S OVERRIDE WINS. It is a deliberate choice, so it is used
	// as given — and the caller still reports what was left behind, so the
	// choice is never invisible in the log.
	if override := strings.TrimSpace(os.Getenv(EnvName)); override != "" {
		if worktree != "" && WithinWorktree(override, worktree) {
			return override, inherited, nil
		}
		return override, "", nil
	}

	// ② THE PROCESS ROOT — the escape directory inside the scratch runtime
	// tree, when (and only when) the measurement proves it satisfies both
	// invariants.
	if home := strings.TrimSpace(d.scratchHome); home != "" {
		candidate := filepath.Join(home, runtimeDirName, EscapeDirLeaf)
		if provablyOutside(d, candidate, worktree) && mkdirAll(candidate) == nil {
			return candidate, escapeReason(inherited, worktree), nil
		}
	}

	// ③ PROCFS — the degradation. A kernel mount, on its own filesystem where it
	// exists at all, and never a task worktree. Its existence is checked
	// EXPLICITLY first: offRootFS measures the deepest existing ancestor, so a
	// vanished mount would otherwise be measured through its parent and pass.
	if st, statErr := os.Stat(d.procRoot); statErr == nil && st.IsDir() && provablyOutside(d, d.procRoot, worktree) {
		return d.procRoot, escapeReason(inherited, worktree), nil
	}

	// ④ NOTHING PROVABLE: keep the inherited directory, and let the caller
	// report the consequence with FallbackFmt (escapedFrom == dir is the
	// sentinel that selects it).
	return inherited, inherited, ErrNoProcessRoot
}

// escapeReason renders the report payload for an escape: the inherited
// directory is what the reader needs to see, and only a child that actually
// descends from a worktree has anything to explain (an escape from a neutral
// directory is hygiene, and its reason line stays empty by using dir itself).
func escapeReason(inherited, worktree string) string {
	if worktree == "" || inherited == "" {
		return ""
	}
	if WithinWorktree(inherited, worktree) {
		return inherited
	}
	return ""
}

// provablyOutside reports whether path satisfies BOTH invariants: outside the
// worktree the child descends from and outside every declared worktree root,
// and on a device other than the one `/` lives on.
func provablyOutside(d deps, path, worktree string) bool {
	if path == "" {
		return false
	}
	// The path may not exist yet — resolve its deepest EXISTING prefix through
	// its symlinks first (Rule 22's "measure the property": the scratch home on
	// this host is a symlink under the default worktree root, so a literal
	// comparison would both false-refuse the scratch tree AND mis-measure its
	// device).
	resolved := ResolvePrefix(path)
	if worktree != "" && WithinWorktree(resolved, worktree) {
		return false
	}
	if _, inside := worktreeroots.PermitsAny(d.worktreeRoots, resolved); inside {
		return false
	}
	return offRootFS(d, resolved)
}

// offRootFS reports whether path is on a DIFFERENT device than `/` — the
// MEASURED criterion of Rule 22 (a spelling is never evidence).
//
// The path may not exist yet (the process root is created on first use, and a
// later start may find it already gone — an age sweep collects the scratch
// tree). statfs(2) needs an existing target, so the deepest EXISTING ancestor
// is measured: a directory lands on its parent's filesystem by definition, so
// the ancestor's device IS the child's device. An unmeasurable tree is never a
// permission (Rule 15) — the caller degrades to procfs and says so.
func offRootFS(d deps, path string) bool {
	probe := path
	for {
		if dev, err := d.deviceID(probe); err == nil {
			rootDev, rErr := d.rootDeviceID()
			if rErr != nil {
				return false
			}
			return dev != rootDev
		}
		parent := filepath.Dir(probe)
		if parent == probe || probe == "" {
			return false
		}
		probe = parent
	}
}

// ResolvePrefix returns path with its deepest EXISTING ancestor's symlinks
// resolved — a to-be-created path lands on its ancestor's filesystem by
// definition, so this is the spelling that can be measured and compared.
//
// It is exported because the acceptance test must compare a child's measured
// /proc/<pid>/cwd against a worktree path with the SAME normalisation the
// decision used: comparing raw spellings is the classic false negative (on this
// host the scratch home was a symlink onto /dev/shm until the memory backing was
// retired on 2026-09-18).
func ResolvePrefix(path string) string {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." {
		return clean
	}
	probe, rest := clean, ""
	for {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil && resolved != "" {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return clean // nothing on the way exists — the literal is all we have
		}
		rest = filepath.Join(filepath.Base(probe), rest)
		probe = parent
	}
}

// WithinWorktree reports whether path is dir itself or nested inside it, both
// under ResolvePrefix — the containment predicate the decision, the reclaim
// guard and their tests SHARE (one definition, Rule 14).
func WithinWorktree(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	return worktreeroots.Within(ResolvePrefix(path), ResolvePrefix(dir))
}

// resolveWorktreeOf returns the worktree directory the child must NOT land in:
// the caller's declared value when it names one, else the nearest ancestor of
// the inherited directory that is a LINKED worktree.
//
// WHY the ancestry walk: it is what makes the fix hold for the MECHANISM
// rather than for a call site — a daemon launched by an agent session that
// stands in its worktree is caught even when no caller passed the worktree
// explicitly (the live incident shape: the merger is started by the agent's
// own bash tool, whose cwd is the task worktree).
func resolveWorktreeOf(d deps, inherited, declared string) string {
	if declared = strings.TrimSpace(declared); declared != "" {
		return ResolvePrefix(declared)
	}
	if inherited == "" {
		return ""
	}
	dir := ResolvePrefix(inherited)
	for hops := 0; hops < ancestryMaxHops; hops++ {
		if dir == "" || dir == string(filepath.Separator) {
			return ""
		}
		if isLinkedWorktree(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// isLinkedWorktree reports whether dir is the top of a LINKED git worktree:
// its .git entry is a FILE (the gitdir pointer). A .git DIRECTORY is the main
// checkout or a plain repository — not a worktree this package must escape,
// and treating it as one would push every daemon out of the operator's own
// project for no gain.
func isLinkedWorktree(dir string) bool {
	st, err := os.Lstat(filepath.Join(dir, gitDirName))
	if err != nil {
		return false
	}
	return !st.IsDir()
}

// mkdirAll creates the process root, reporting the failure (the caller then
// degrades rather than pretending the directory exists).
func mkdirAll(dir string) error {
	return os.MkdirAll(dir, DirPerm)
}

// EscapeFromWorktree answers the EXECUTOR's question (a different question from
// the daemon's): dir is the directory a child is about to run in — it may
// legitimately be the task's OWN ground, but it must never be a FOREIGN task's
// worktree, because a process standing in another row's worktree vetoes that
// row's recycle for as long as it lives.
//
// ownGround names the directory this child is entitled to stand in (the row's
// deliverable repo/worktree — "" when the row has none). The nearest LINKED
// worktree ancestor of dir decides:
//
//	no linked worktree ancestor       → dir is neutral: keep it (ok=false)
//	ancestor IS ownGround (or inside) → the task's own ground: keep it (ok=false)
//	anything else                     → ESCAPE: return the measured process
//	                                    root, the worktree left behind, ok=true
//
// The refusal to escape in the second case is deliberate and load-bearing: the
// daemon-context-isolation epic (2026-09-13) established that a task's executor
// must run in the task's project so it loads the right GOPI.md and indexes the
// right tree. Escaping there would trade one bug for another (branch-backfill
// starvation — the measured regression that epic fixed).
//
// CONTRACT (read this before assigning the results): root is meaningful ONLY
// when ok=true. On every other exit root is "" — the answer is "keep the
// directory you passed in", which is the value the CALLER already has. A caller
// that assigns root unconditionally therefore blanks its child's cwd; the
// measured instance of that mistake (five cmd/agent e2e walks landing every
// executor child in the session dir) is why the caller holds the resolved
// directory as its default. escapedFrom is the REPORTED worktree and follows
// the same rule: "" means nothing to report.
func EscapeFromWorktree(dir, ownGround string) (root, escapedFrom string, ok bool) {
	if strings.TrimSpace(dir) == "" {
		return "", "", false
	}
	d, err := productionDeps()
	if err != nil {
		return "", "", false
	}
	return escapeWith(d, dir, ownGround)
}

// escapeWith is EscapeFromWorktree against injected dependencies (the same test
// seam resolveWith gives Resolve). It exists because the contract the CALLER
// must respect — ok=false spells "keep the directory you passed in", so root is
// "" on every non-escaping exit — is exactly the kind of rule that a caller
// silently breaks, and the unit test pins it without paying for a full e2e walk.
func escapeWith(d deps, dir, ownGround string) (root, escapedFrom string, ok bool) {
	if strings.TrimSpace(dir) == "" {
		return "", "", false
	}
	worktree := resolveWorktreeOf(d, dir, "")
	if worktree == "" {
		return "", "", false
	}
	if own := strings.TrimSpace(ownGround); own != "" && WithinWorktree(own, worktree) {
		return "", "", false // the task's own ground — legitimate, never escaped
	}
	root = processRoot(d, worktree)
	if root == "" || WithinWorktree(root, worktree) {
		return "", "", false // nothing provable — the caller keeps dir and its report
	}
	return root, worktree, true
}

// processRoot returns the measured process root for a given worktree to stay
// out of: the scratch runtime directory when it is provably off `/`, else
// procfs. "" = no provable root.
func processRoot(d deps, worktree string) string {
	if home := strings.TrimSpace(d.scratchHome); home != "" {
		candidate := filepath.Join(home, runtimeDirName, EscapeDirLeaf)
		if provablyOutside(d, candidate, worktree) && mkdirAll(candidate) == nil {
			return candidate
		}
	}
	if provablyOutside(d, procRootDir, worktree) {
		return procRootDir
	}
	return ""
}

// IsDaemonArgv0 reports whether an argv[0] belongs to a DETACHED GOPI DAEMON —
// the immortal holder shape a reclaim report must NAME, because no task owns it
// and it never exits on its own.
//
// argv[0] (not /proc/<pid>/comm) is the right channel, and it was MEASURED
// before being chosen: a real `gopi merger --daemon` reports comm = the
// BINARY's name (the kernel takes comm from the executable path at exec, e.g.
// "probe-gopi" for a renamed build), while its cmdline[0] is the stable
// "gopi-merger" the spawn seam assigns. A comm-based check therefore misses
// exactly the daemons this exists to name.
func IsDaemonArgv0(argv0 string) bool {
	return strings.HasPrefix(strings.TrimSpace(argv0), Argv0Prefix)
}

// ProcessArgv0 returns pid's argv[0] — the process's own identity as the kernel
// recorded it at exec.
func ProcessArgv0(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), procCmdlineName))
	if err != nil || len(b) == 0 {
		return "", false
	}
	argv0 := string(b)
	if i := strings.IndexByte(argv0, 0); i >= 0 {
		argv0 = argv0[:i]
	}
	return argv0, true
}

// IsDaemonProcess reports whether pid is a detached gopi daemon (its argv[0]
// carries the daemon prefix). It is a NAME test, not a guard: the caller still
// has to decide whether the daemon is a legitimate holder of the directory in
// question (see IsTestScratchHolder).
func IsDaemonProcess(pid int) bool {
	argv0, ok := ProcessArgv0(pid)
	return ok && IsDaemonArgv0(argv0)
}

// IsTestScratchHolder reports whether pid is a live process that a Go TEST run
// spawned — the holder whose interest in a worktree is an ARTIFACT of a test
// rather than work in progress (epic verify-no-verdict-keeps-merge, 2026-09-14).
//
// TWO EVIDENCE CHANNELS, because neither one alone covers the measured shape.
// Both are read from /proc — the kernel's own record, never a caller's claim:
//
//	① the process's CWD is inside the scratch TEST tree (IsTestScratchCwd);
//	② its EXEC-TIME ENVIRONMENT names a scratch path — TMPDIR or GOPI_HOME
//	   pointing inside the scratch home, or GOPI_TEST_SCRATCH_HOME set at all,
//	   with GOPI_TEST_TMPDIR (an operator pin, config.SuiteTempRootEnvName) as
//	   the deliberate exception, since a pinned tree is not this suite's own.
//
// WHY ② EXISTS (MEASURED, not assumed — this host, 2026-09-14): every residue
// daemon of the incident has its cwd inside a TASK WORKTREE
// (`/home/user/workspace/gopi-<task>/cmd/agent`), NOT inside the scratch tree:
//
//	pid 130432  cwd=/home/user/workspace/gopi-worker-status-assert/cmd/agent
//	            GOPI_HOME=/dev/shm/gopi-test-scratch/suite-cmd-agent-run-2837926703/TestMergerRestart_…/001
//	            TMPDIR=/dev/shm/gopi-test-scratch/suite-cmd-agent-run-2837926703
//
// A cwd-only predicate therefore read all nine as ORDINARY holders and the
// reclaim arm stayed deadlocked on exactly the case this epic dispatched. The
// environment is the channel that can still see them: a process a suite spawned
// carries the suite's scratch roots in its exec-time block no matter where its
// cwd is, and it cannot shed them (that block is fixed at exec and is not even
// affected by a later os.Setenv here).
//
// ONLY A DAEMON-SHAPED PROCESS QUALIFIES, deliberately: the test that spawns a
// child to sit in a worktree (internal/tools' holder fixtures) is evidence
// ABOUT that worktree, not an artifact with no interest in it, and a rule that
// out-voted it on age would delete the very directory the test is proving is
// protected.
//
// The caller-side rule is deliberately NOT repeated here: this predicate ONLY
// classifies the holder. The decision to converge on it (refuse while a test
// may still be in flight, reclaim once the holder is past the floor) lives with
// the reclaim arm, so a single stray read cannot delete anything.
func IsTestScratchHolder(pid int) bool {
	if pid <= 0 || !IsDaemonProcess(pid) {
		return false
	}
	cwd, haveCwd := ProcessCwd(pid)
	if haveCwd && IsTestScratchCwd(cwd) {
		return true
	}
	// A daemon standing in the escape root DID its job: that directory is the
	// legitimate immortal home of a correctly escaped daemon, so it is never
	// out-voted (the guard-weakening this epic forbids). The classification is
	// therefore about daemons that did NOT escape — which is exactly the
	// incident shape (cwd left in a task worktree).
	if haveCwd && escapedToProcessRoot(cwd) {
		return false
	}
	return testScratchEnvironment(pid)
}

// escapedToProcessRoot reports whether path is (inside) the process root a
// correctly started daemon escapes to — the directory whose occupants are
// legitimate holders by construction (see IsTestScratchPath's exclusion).
func escapedToProcessRoot(path string) bool {
	home := strings.TrimSpace(scratch.HomeDir())
	if home == "" {
		return false
	}
	return worktreeroots.Within(path, filepath.Join(home, runtimeDirName, EscapeDirLeaf))
}

// testScratchEnvironment is evidence channel ②: the process's EXEC-TIME
// environment names a scratch path. An unreadable block is NEVER evidence (a
// missing measurement is not a classification) — the predicate then falls back
// to the cwd answer alone.
//
// GOPI_TEST_SCRATCH_HOME present AT ALL is the strongest of the two signals:
// only a test run publishes the scratch home (no production process sets it),
// so the variable's VALUE needs no classification.
func testScratchEnvironment(pid int) bool {
	raw, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), procEnvironName))
	if err != nil {
		return false
	}
	if _, present, perr := scratch.ProcEnviron(raw, scratch.HomeEnvName); perr == nil && present {
		return true
	}
	value, present, perr := scratch.ProcEnviron(raw, scratch.TmpDirEnvName)
	if perr != nil || !present {
		return false
	}
	return IsTestScratchPath(value)
}

// IsTestScratchPath reports whether a PATH lies inside this host's scratch tree
// but outside the daemon escape directory — the same classification
// IsTestScratchCwd applies to a cwd, and deliberately the SAME function, so the
// two evidence channels can never disagree about what "the scratch test tree"
// means (Rule 14).
//
// The scratch home is a test tree whether it was reached through
// GOPI_TEST_SCRATCH_HOME or through the operator's GOPI_TEST_TMPDIR pin: both
// name the suite's own throwaway tree, and a leftover standing in it has the
// same absence of interest either way. The ONE exclusion is the daemon escape
// root (see IsTestScratchHolder) — the directory a correctly started daemon
// lives in for its whole immortal life.
func IsTestScratchPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	home := strings.TrimSpace(scratch.HomeDir())
	if home == "" {
		return false
	}
	if escapedToProcessRoot(path) {
		return false // the daemon's own escape root — a legitimate immortal home
	}
	return worktreeroots.Within(path, home)
}

// IsTestScratchCwd is the cwd-only half of IsTestScratchHolder: it classifies a
// PATH as a scratch-test-tree location. Split out so a caller holding cwd
// evidence can ask the question without a live pid, and so the tests can pin
// the classification without moving a live process.
//
// The daemon's own escape directory (scratchHome/runtime/process) is INSIDE the
// scratch home and is excluded deliberately: a correctly escaped daemon stands
// there for its whole immortal life, and it is a legitimate occupant of the
// process root — the point of the escape. The test tree is the part no
// production process may occupy: SuiteTempRoot/MkdirTemp are the suite's own
// sandbox (Rule 22's measured, off-/ tree).
func IsTestScratchCwd(cwd string) bool { return IsTestScratchPath(cwd) }

// --- the measurements (exported: the acceptance test reads the SAME code path
// production uses, never a re-implementation) ---

// ProcessCwd returns pid's working directory as the KERNEL reports it —
// /proc/<pid>/cwd, resolved through the process's own mount view. This is the
// measurement every acceptance claim in this epic rests on (never "cmd.Dir was
// assigned").
func ProcessCwd(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	target, err := os.Readlink(filepath.Join(procRootDir, strconv.Itoa(pid), procCwdName))
	if err != nil || target == "" {
		return "", false
	}
	return target, true
}

// ParentOf returns pid's parent pid from /proc/<pid>/stat.
//
// The comm field may contain spaces AND parentheses, so the parse resumes
// after the LAST ')' — the trap that makes a naive Field[1] read wrong exactly
// when it matters.
// ParentOf reads a process's parent pid from <procRootDir>/<pid>/<procStatName>.
// ok=false means unreadable (gone, restricted, malformed) — the caller ends its
// walk rather than inventing an ancestor. The command name in that file is
// parenthesised and may contain spaces and parentheses, so the field is read by
// internal/procname.StatField — the ONE implementation of the
// count-from-the-last-')' rule (Rules 13/14) — through the field NUMBER, not a
// bare post-comm index.
func ParentOf(pid int) (int, bool) {
	if pid <= 0 {
		return 0, false
	}
	raw, ok := procname.StatFieldString(filepath.Join(procRootDir, strconv.Itoa(pid), procStatName), procname.StatFieldParent)
	if !ok {
		return 0, false
	}
	ppid, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return 0, false
	}
	return ppid, true
}

// DescendsFrom reports whether pid's ancestor chain contains want. It is the
// containment question the /proc acceptance test asks of the spawned child
// (Rule 24's lesson: ancestry truth is the kernel's, never a shell's).
func DescendsFrom(pid, want int) bool {
	if pid <= 0 || want <= 0 {
		return false
	}
	for hops := 0; hops < ancestryMaxHops; hops++ {
		if pid == want {
			return true
		}
		next, ok := ParentOf(pid)
		if !ok || next == pid || next <= 0 {
			return false
		}
		pid = next
	}
	return false
}

// WorktreeRoots returns the declared worktree roots the decision stayed outside
// of — exported so the acceptance test asserts against the SAME set the
// resolver used, rather than a re-derived one.
func WorktreeRoots() []worktreeroots.Root {
	d, err := productionDeps()
	if err != nil {
		return nil
	}
	return d.worktreeRoots
}

// productionWorktreeRoots returns the worktree roots the escape decision must
// stay outside of — EXACTLY what the ONE resolver answers, Source included.
//
// WHY NOT DefaultRoot() (the defect this replaces, measured 2026-10-26): this
// package used to build the list from worktreeroots.DefaultRoot() and hard-code
// worktreeroots.SourceDefault, so it saw the DEFAULT layer alone while every
// other consumer (config.ResolveRepoWorktreeRoot, the spawn seam's ground
// placement, the merger's build-cache gate) went through
// worktreeroots.Resolve. The two answers could disagree: an operator declaring
// GOPI_WORKTREE_ROOT=<path> or a repo declaring `worktree_root` got a root
// this list did not contain, so a scratch tree inside the DECLARED root was
// reported as provably outside it. Rule 2/14: the resolver has ONE reader and
// this package is a reader, not a second policy — so it asks the resolver and
// carries its answer, Source and Role included, rather than re-deriving any
// layer itself.
//
// declared is the settings `worktree_root` of the repo being daemonised. The
// production seam passes "" (the daemon spawn has no repo context), which is
// also what config.ResolveRepoWorktreeRoot passes for a repo absent from the
// table — so the layers in force there are the ambient override and the
// default, and WHICH one decided is reported in Source (Rule 15).
func productionWorktreeRoots(declared string) ([]worktreeroots.Root, error) {
	root, err := worktreeroots.Resolve(declared)
	if err != nil {
		return nil, fmt.Errorf("resolve the declared worktree root: %w", err)
	}
	return []worktreeroots.Root{root}, nil
}

// productionDeps builds the production dependencies (the /proc + scratch
// measurement).
func productionDeps() (deps, error) {
	roots, err := productionWorktreeRoots("")
	if err != nil {
		return deps{}, err
	}
	return deps{
		procRoot:      procRootDir,
		scratchHome:   scratch.HomeDir(),
		deviceID:      scratch.DeviceID,
		rootDeviceID:  scratch.RootDeviceID,
		parentOf:      ParentOf,
		readCwd:       ProcessCwd,
		worktreeRoots: roots,
	}, nil
}
