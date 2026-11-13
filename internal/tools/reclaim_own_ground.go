package tools

// THE CLOSE-OUT GROUND MARK — the ONE owner of the fact "the ground of one of
// THIS process's own ledger rows is being released right now" (Rules 13/14).
//
// WHY THE FACT NEEDS AN OWNER AT ALL. Three independent guards withhold a
// worktree by asking a question whose answer is DIFFERENT during a close-out
// than it is when somebody sweeps a stranger's ground:
//
//	① the AGE FLOOR (reclaimSelfOwnWhy's rung ①, task_reclaim_self.go): a
//	  registration seconds old is KEPT because "a concurrent round may be
//	  creating it" — the protector of a phase-2 round registering its fresh
//	  checkout. But the ground of the row a run is closing out is, by
//	  construction, young: the run created it minutes ago.
//	② the OWN-GROUND rung (guestRowOwnCheckoutRefusal, guest_row_guard.go): a
//	  process must never free the directory it is standing in — measured
//	  breach 2026-09-21 (row 01a0c574, holder alive, branch 14 ahead).
//	③ the GUEST receiver arm (guestRowOwnRefusal, guest_row_guard.go): the
//	  asker stands in a LINKED worktree of the row's repo, so it is a guest
//	  there. True for the sweep, and false for the run's own close-out, whose
//	  cwd IS the ground it is releasing (cmd/agent taskReclaimOwnDirectories
//	  documents that shape as designed).
//
// Each of the three is asked by a DIFFERENT arm, at a different depth, and all
// three are legitimate everywhere else — so the fix cannot be a fourth
// re-derivation of the distinction inside each guard. It is ONE fact, published
// once, by the pass that knows it: the release pass of the producer close-out.
//
// WHAT ARMING IT MEANS, AND WHY IT IS NARROW. The mark carries the EXACT set of
// ground directories the pass is closing out — the worktrees of the rows the
// ledger says THIS process claims (reclaimSelfRows, the very set the pass walks).
// A path in that set is the ground of the asker's own finished row, which is the
// only thing the mark ever permits; nothing else about the process changes. It is
// NOT a mode switch for "trust me", and it is deliberately NOT an env var: a knob
// an operator can set would let a sweep free somebody else's ground (Rule 15's
// direction), while this fact is only ever true inside the pass that publishes
// it.
//
// WHAT THE MARK DOES NOT PERMIT — the safety of the whole change:
//
//	· a FOREIGN holder still vetoes (reclaimSelfOwnWhy rung ②, the shared
//	  cwd/fd/exe/maps scan): the mark answers "is this ground mine to release?",
//	  never "is anybody standing in it?".
//	· a registration OUTSIDE this set is untouched, so no unowned registration
//	  and no stranger's checkout can be freed by arming it.
//	· the release-before-removal ORDER stays a caller obligation: freeing the
//	  directory the asker stands in is only sound because the close-out hands its
//	  cwd back (worktreerelease.ReleaseReporting) — the marks make the guards
//	  agree with the contract the producers already follow, they do not license
//	  deleting a working directory out from under an active run.

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nick/gopi/internal/scratch"
)

// selfGroundReleaseMu guards the mark below. The producers' passes are
// single-goroutine in practice, but the mark is process state reachable from
// more than one seam (the worker round, the wrapper's tail), so it is guarded
// rather than assumed.
var (
	selfGroundReleaseMu    sync.Mutex
	selfGroundReleasePaths map[string]bool
)

// SelfGroundReleaseArm publishes the ground set for the pass that is about to
// run: the worktrees of the rows THIS process is closing out. It returns a
// function that disarms the mark, so a caller spells the pair in one breath
// (defer done()).
//
// An EMPTY set arms nothing: a pass with no rows of its own has no ground of
// its own, and "armed with the empty set" must not read as "everything is
// mine".
func SelfGroundReleaseArm(grounds []string) (disarm func()) {
	paths := make(map[string]bool, len(grounds))
	for _, g := range grounds {
		if c := scratchCanonicalGround(g); c != "" {
			paths[c] = true
		}
	}
	selfGroundReleaseMu.Lock()
	// SAVE, then set — and the returned disarm RESTORES what it replaced. This is
	// not tidiness: the mark is armed at more than one depth (the close-out seam
	// arms both halves; the ledger-keyed half arms its own around itself), and a
	// disarm that simply CLEARED would take the outer arm with it — measured: the
	// seam's mark survived the ledger half's read but was gone by the time the
	// ownership-independent half asked, so the ground that half exists to free was
	// refused by the age floor a second time. Nesting composes; clearing does not.
	prev := selfGroundReleasePaths
	if len(paths) == 0 {
		selfGroundReleasePaths = prev
	} else {
		selfGroundReleasePaths = paths
	}
	selfGroundReleaseMu.Unlock()
	return func() {
		selfGroundReleaseMu.Lock()
		selfGroundReleasePaths = prev
		selfGroundReleaseMu.Unlock()
	}
}

// SelfGroundReleasePendingFor reports whether worktree is the ground of one of
// the rows the current close-out pass is releasing — the ONE reading every
// guard above uses. A path is never "somebody else's" here: membership is exact,
// on canonical paths, so a parent directory or a look-alike spelling answers
// false (a partial match would free a directory nobody armed).
func SelfGroundReleasePendingFor(worktree string) bool {
	c := scratchCanonicalGround(worktree)
	if c == "" {
		return false
	}
	selfGroundReleaseMu.Lock()
	defer selfGroundReleaseMu.Unlock()
	return selfGroundReleasePaths[c]
}

// scratchCanonicalGround is the ONE spelling of "the same directory", reused
// from the shared canonicaliser the guest rungs already compare with
// (scratch.CanonicalPath), so two guards can never disagree about whether two
// spellings name one path.
func scratchCanonicalGround(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if c := scratch.CanonicalPath(dir); c != "" {
		return filepath.Clean(c)
	}
	return filepath.Clean(dir)
}

// SelfGroundReleaseArmCloseout is the ONE derivation of a producer close-out's
// ground set (Rules 13/14): the directory this process was STARTED in, when that
// directory is a linked worktree of a repository one of its own closing rows
// names — and nothing else.
//
// WHY THE LAUNCH DIRECTORY IS PART OF IT, and why this is the only place that
// derives it (measured on master f4451b9d, TestManualRunRecyclesItsWorktree's
// own child output): a run started from inside a task's linked worktree does not
// work there — the worker CREATES its own ground for the executor child
// (`gopi worker: child ground → …/gopi-<taskid>`) and the row's deliverable
// records THAT directory. The directory the operator launched from is therefore
// named by NO row, and every ledger-keyed arm is blind to it: the measured
// contract is a `gopi worker <task-id>` started in the task's worktree leaving
// it registered and on disk after a perfectly normal run.
//
// It is NOT a licence to delete where the operator works, because the launch
// directory is admitted only when ALL THREE hold:
//
//	· it IS a linked worktree (inLinkedWorktreeRoot — the same parse the guest
//	  rungs use): the repository's main checkout is never in the set, so the
//	  "launched from ~/work/GoPi" shape is untouched by construction;
//	· it belongs to a repository one of THIS close-out's rows names
//	  (reclaimSelfOwnRepoOf, compared canonically): a directory of an unrelated
//	  repo is never in the set;
//	· it is a row of THIS process that is closing out — the arming function is
//	  only reachable from a producer's own pass.
//
// Everything else about releasing it is the shared machinery, unchanged: a
// FOREIGN process standing in it still vetoes (the live-holder scan — the
// operator's own shell is exactly that, so an attended launch keeps the
// directory), uncommitted work still vetoes (the clean rung), and a live row
// naming it still vetoes. The marks only answer "may this pass free it at all",
// never "is anybody using it".
func SelfGroundReleaseArmCloseout(rows []reclaimDeadHolderRow) (disarm func()) {
	// ONLY the launch candidates go into the marks — deliberately NOT the
	// worktrees the rows record. The recorded ground is what the pre-existing
	// criterion `TestReclaimSelfRefusesItsOwnCheckout` covers, and it must keep
	// refusing: the 2026-09-21 breach was a ledger-keyed arm freeing the very
	// checkout the run stood in, under a live process (row 01a0c574). A ground
	// some row NAMES is reachable by the machinery that resolves that row —
	// finish-cleanup removes it, as the measured run's own output shows
	// (`removal-audit: worktree-removal record for …/004/gopi-<taskid>`). What no
	// machinery can reach is the directory the run was LAUNCHED in, because the
	// worker creates its own ground elsewhere and no row records the launch
	// directory. That — and only that — is what this mark adds.
	grounds := make([]string, 0, len(rows)+1)
	repos := make([]string, 0, len(rows))
	for _, r := range rows {
		repo := strings.TrimSpace(r.Repo)
		if repo == "" && strings.TrimSpace(r.Worktree) != "" {
			// A row that records its ground but not its repo is the ordinary
			// shape (the deliverable carries the worktree; the repo is derived).
			// The ground's own repository is the honest answer, and it is read
			// with the same helper the rungs use — one derivation, no second
			// notion of "which repo is this".
			repo = reclaimSelfOwnRepoOf(r.Worktree)
		}
		if repo != "" {
			repos = append(repos, repo)
		}
	}
	// The launch candidates are READ from their one recorded source, never taken
	// as a parameter: a caller that could pass any directory could arm a ground
	// the process never stood in.
	for _, dir := range SelfGroundLaunchDirs() {
		if selfGroundLaunchDirArmable(dir, repos) {
			grounds = append(grounds, dir)
		}
	}
	return SelfGroundReleaseArm(grounds)
}

// selfGroundLaunchDirArmable answers whether a close-out may treat dir as its
// own ground — the three conditions in SelfGroundReleaseArmCloseout's doc, in
// one place, so no caller can widen them by passing a bare directory.
func selfGroundLaunchDirArmable(dir string, repos []string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" || len(repos) == 0 {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	// ① a LINKED worktree, never a main checkout (the parse is the guest
	// rungs': a linked worktree's `.git` is a file naming
	// <common>/worktrees/<name>).
	if !inLinkedWorktreeRoot(abs) {
		return false
	}
	// ② it must belong to a repository a closing row names. git is the only
	// authority for "which repo is this", and it answers with the MAIN checkout,
	// so the comparison is main-to-main.
	kind := scratchCanonicalGround(reclaimSelfOwnRepoOf(abs))
	if kind == "" {
		return false
	}
	for _, repo := range repos {
		if scratchCanonicalGround(repo) == kind {
			return true
		}
	}
	return false
}

// selfGroundReleaseArmCloseoutPass arms the mark for a producer close-out pass
// that must derive its own row set: the rows THIS process claims, which is the
// ONE evidence of "my own finished work" every producer pass keys on
// (reclaimSelfRows). A pass reads the ledger for its own purposes anyway, so
// this costs it one query and keeps the mark and the pass in step (Rules 13/14).
//
// An UNREADABLE ledger arms NOTHING: the mark is a permission, and a pass that
// cannot name its own rows has none to grant (the same fail-safe direction the
// passes' own ledger read takes).
func selfGroundReleaseArmCloseoutPass() func() {
	rows, err := reclaimSelfRows()
	if err != nil {
		return SelfGroundReleaseArm(nil)
	}
	return SelfGroundReleaseArmCloseout(rows)
}

// SelfGroundReleaseArmedGrounds returns the armed set as a slice — for tests
// and for a caller that wants to REPORT what it armed (a mark nobody can
// observe is a silent one).

// selfGroundLaunchDirs / selfGroundLaunchDirMu hold the candidate directories
// this process may have been STARTED in, for the close-out's ground set (see
// SelfGroundReleaseArmCloseout). It is SET by the producer seams that own the
// values — cmd/agent captures its start directory once at main and adds the
// launcher handoff beside it — and read here, so the derivation has named
// inputs rather than a cwd read of whatever moment the pass happens to run in
// (the measured difference: the wrapper's cwd at close-out is its task's SESSION
// directory, not where it was launched).
//
// A CANDIDATE LIST, not one value, deliberately: the two sources describe
// different things and neither is authoritative alone. The captured directory is
// where THIS process stood, and the handoff (TaskWorkdirEnv) is where its
// LAUNCHER stood — measured on the fixture's own run, the handoff is inherited
// from whatever session launched the suite (an agent's own worktree, in this
// repository) and would crown a foreign directory if it simply won. Each
// candidate is therefore admitted on its own merits by
// selfGroundLaunchDirArmable, and one that belongs to no closing row's repo is
// refused by condition ② — the safety does not rest on the precedence.
var (
	selfGroundLaunchDirMu sync.Mutex
	selfGroundLaunchDirs  []string
)

// SetSelfGroundLaunchDir records candidate directories this process may have been
// started in, in the caller's order. An empty/blank candidate is ignored; a
// repeated one is recorded once (the sources routinely agree).
func SetSelfGroundLaunchDir(dirs ...string) {
	selfGroundLaunchDirMu.Lock()
	defer selfGroundLaunchDirMu.Unlock()
	selfGroundLaunchDirs = nil
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || slices.Contains(selfGroundLaunchDirs, dir) {
			continue
		}
		selfGroundLaunchDirs = append(selfGroundLaunchDirs, dir)
	}
}

// SelfGroundLaunchDirs returns the recorded candidates (nil when none was
// recorded — a producer that never recorded one arms no launch ground, which is
// the safe direction).
func SelfGroundLaunchDirs() []string {
	selfGroundLaunchDirMu.Lock()
	defer selfGroundLaunchDirMu.Unlock()
	return append([]string(nil), selfGroundLaunchDirs...)
}
func SelfGroundReleaseArmedGrounds() []string {
	selfGroundReleaseMu.Lock()
	defer selfGroundReleaseMu.Unlock()
	out := make([]string, 0, len(selfGroundReleasePaths))
	for p := range selfGroundReleasePaths {
		out = append(out, p)
	}
	return out
}
