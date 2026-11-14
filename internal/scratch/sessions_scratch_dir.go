// sessions_scratch_dir.go — bounding the `session-scratch-dir` family: the
// DIRECTORIES a run made inside its own session container, under names the RUN
// chose.
//
// WHY THIS NEEDS A PREDICATE OF ITS OWN (measured 2026-10-01 on this host): the
// sessions tree held 277 such directories in 165 distinct name shapes —
// `evidence`, `evid`, `ev`, `bin`, `artifacts`, `crops`, `figs`, `__pycache__`,
// `smoke-clone`, `master-check`, `probeN`, `acceptN`, … No NAME predicate can
// enumerate them, which is exactly why the declared-tree registry — whose whole
// mechanism is a namespace predicate (`go-build*`, `gopi-raw-*`, `*.raw.log`) —
// bounded every OTHER family in that tree and left this one unbounded. The family
// registry recorded the honest verdict at the time: `keep`, "a run's working area
// inside its own container: its name is chosen by the run, so no predicate can
// tell a dead one from a live one".
//
// THAT VERDICT WAS ABOUT THE NAME, AND IT WAS RIGHT ABOUT IT. The way out is not
// a cleverer name rule: it is to stop asking about the NAME and ask about the
// CONTAINER's lifecycle instead — the thing that already decides whether anything
// inside it is still needed.
//
// WHY IT LIVES IN A CONTAINER-ENTRY HOOK AND NOT IN A SECOND TREE ROW. The
// `session-raw` row already walks this tree one level down and already refuses to
// remove a container that is not empty of its own candidates (containerEntries'
// keptFiles); what it could not do was judge a NESTED DIRECTORY — it counted one
// and kept the container forever. So the arm is EXTENDED where it already stands:
// containerEntries calls containerScratchDirEntries for the directory children it
// used to merely count, and that hook is this file. One tree, one walk, one
// plan/sweep pair — no second row over the same root, no second schedule, and no
// second answer to "what is in this container".
//
// containerEntries' three-way verdict for one child therefore becomes:
//
//	the raw log (CAND)      → the arm's own candidate, as before
//	a plain file (KEEP)     → a container holding one is never emptied, as before
//	a DIRECTORY             → this hook: CAND when the container's ledger is
//	                          finished with it and the directory is past its
//	                          window, else KEEP — and KEEP also covers "this
//	                          directory is unowned", which exactly preserves the
//	                          old keptFiles rule.
//
// THE LIFECYCLE QUESTION, and why it is the right one. `internal/tools` mints one
// directory per run under the sessions root, and the ledger reads that directory
// back: the session readers resolve a task id through the `metadata.json` inside
// it, `gopi task show` prints a container's artifacts through it, and a record
// may be RESUMED from it. So the container is the unit whose life is known — the
// ledger either describes it or does not — while a directory INSIDE it is opaque.
//
//	containerHasNoLedger(C) reads the CONTAINER's entries ONCE and is TRUE when C
//	  holds no `metadata.json` AND no `*.jsonl` record (the ledger has nothing to
//	  read out of C, so nothing below it is addressable) AND C has not been
//	  touched for the whole window (a container the writers minted seconds ago
//	  holds its record only seconds later: that window is what separates a run in
//	  flight from one the ledger forgot).
//
// The container's own removal is NOT this arm's business: `SessionRawLogDirStaleAge`
// and the container arm that applies it live in containerEntries, unchanged. This
// hook decides only whether a directory INSIDE a container is still wanted, which
// is why it needs one question and not two.
//
// THE KEEPS, each an explicit denial rather than an omission:
//
//	LEDGER     the container holds a `metadata.json` or any `*.jsonl` — KEEP every
//	           directory inside it, at ANY age. This is the epic's own rule (a
//	           container `gopi task show` resolves through is never touched) and
//	           the one that protects the measured 399 of 430 directories.
//	FAMILY     the registry claims the directory for ANOTHER family
//	           (`attachments-archive`, `foreign-checkout`, `sandbox-profile`) —
//	           asked through `sessionlayout.ClassifyFamily`, so a name list here
//	           cannot drift from the registry and a NEW container family is
//	           respected without this file changing.
//	HELD       a live process's cwd/exe/fd lies inside the container (the tree's
//	           ONE indexed /proc pass, heldSet — the caller's, passed in), the
//	           candidate itself is held, or the candidate carries a `.holder`
//	           registration — the EXISTING liveness verdicts (heldIn /
//	           unheldRegistered), never new ones.
//	YOUNG      the directory, or the container holding it, is younger than the
//	           arm's own window (scratchDirWindow: the caller's, floored at
//	           SessionScratchDirStaleAge).
//	SYMLINK    a link is removed WITH its target if that target is a directory, so
//	           a symlink is never a candidate (refused here, and again by the
//	           sweep's own delete).
package scratch

import (
	"os"
	"path/filepath"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// heldByAncestor answers the holder question for a path INSIDE a container: is
// the path itself, or any directory between it and the container (inclusive),
// held by a live process?
//
// WHY THE CANDIDATE ALONE IS NOT THE QUESTION, measured 2026-10-16 on this host
// (task 01a142630844745eb09a754876bcec9a). `heldIn(path, index)` answers "does a
// live process hold THE CANDIDATE". For a candidate that is a DIRECTORY inside a
// run's container, the holder is the RUN, and the run does not stand in its
// archive or its evidence directory: it stands in its CONTAINER (`cd $S`, the
// shape every evidence-writing bash call uses) or holds a FILE in the container
// (its own `output.log`). Both are invisible to the candidate-only question —
// measured against the real sweep, `<container>/attachments` came back
// `would-remove tree=session-attachments` while a process whose cwd was that
// container ran. The session-raw arm does not have this exposure, because
// `heldSet` indexes every holder's path AND every ancestor of it up to the tree
// root, so a holder standing in the container already covers the directories
// beneath — this helper reads that same index for the arms that ask about a
// directory inside a container, one map lookup per level.
//
// The walk is BOUNDED AT THE CONTAINER: a holder standing in the sessions root is
// sweeping the tree, not working in one container, and must not freeze every
// container under it (the all-or-nothing trap the queue arm measured).
func heldByAncestor(path, container string, index map[string]struct{}) bool {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if heldIn(p, index) {
			return true
		}
		if p == container || p == filepath.Dir(p) {
			return false // the container itself was the last level asked
		}
	}
}

// containerScratchDirEntries is the session-raw container arm's DIRECTORY hook:
// given one container and one DIRECTORY child of it, it returns the candidate the
// arm should collect, or nil when the directory must be kept. It is a var so the
// sweep's red-proof can neuter exactly this decision and show the REAL sweep —
// the one `gopi tool scratch-gc trees` runs — taking an item the un-neutered hook
// protects.
var containerScratchDirEntries = scratchDirCandidate

// scratchDirCandidate is the hook's whole body: every unknown is a KEEP.
func scratchDirCandidate(tree DeclaredTree, path, container string, now time.Time, age time.Duration, held heldTreeScan) []TreeEntry {
	if isSymlink(path) {
		return nil // a link is removed WITH its target: never a candidate
	}
	if heldIn(path, held.index) {
		return nil
	}
	if !treeScanComplete(held) {
		return nil // the holder pass did not finish: nothing is proven unheld
	}
	if !unheldRegistered(path) {
		return nil // a `.holder` registration inside the candidate
	}
	// A directory ANOTHER family claims is that family's decision, never this
	// arm's. This arm was asked to bound ONE family — the container directory no
	// other register claims — and the registry claims three more inside a
	// container, each with a decision that is not this one:
	//
	//	attachments-archive  KEEP     unique evidence (a deleted branch's only
	//	                              surviving copy is an attached bundle)
	//	foreign-checkout     legacy   a probe's whole `git clone`
	//	sandbox-profile      legacy   a materialised `.gopi` profile
	//
	// So the membership test is asked, ONCE, through the registry's own
	// classifier (`sessionlayout.ClassifyFamily` — the same call the census
	// makes), and a directory another family answers for is refused. Written as
	// a name list instead, the two would drift the moment a family is added:
	// this way a NEW container family is respected without this file being
	// touched (Rule 13/14: the registry is the source of truth, not a copy of it).
	if fam, ok := sessionlayout.ClassifyFamily(sessionlayout.FamilyEntryFor(path, true, sessionlayout.WhereContainer)); !ok || fam.Name != sessionlayout.FamilySessionScratchDir {
		return nil // somebody else's family: its own decision bounds it
	}
	// THIS ARM's window, not the caller's: the `session-raw` row's StaleAge is
	// the window for its *.raw.log FILE arm (14d), and that is the value the
	// walk hands down. A run's own evidence directory is not that file, so the
	// walk's window is not the one it is judged by — see scratchDirWindow.
	window := scratchDirWindow(age)
	if !containerHasNoLedger(container, now, window, held) {
		return nil
	}
	stale, err := tree.entryStale(path, now, window)
	if err != nil {
		// UNMEASURABLE, and now SAID: the keep is the existing rule (Rule 15);
		// the sentence is the new half, because this branch used to fold "could
		// not be measured" into "younger than the window" and print nothing —
		// the two are different facts about different things, and only one of
		// them is a reason for an operator to go and look.
		reportTreeKeepUnmeasurable(tree, path, err)
		return nil
	}
	if !stale {
		return nil // younger than the arm's window: a run may still be writing here
	}
	return []TreeEntry{measuredEntry(tree, path, true, now)}
}

// scratchDirWindow is THE WINDOW this arm judges by: the caller's, floored at
// SessionScratchDirStaleAge.
//
// WHY THE FLOOR IS NEEDED AND WHY IT IS THIS DIRECTION. containerEntries hands
// its hook the same `age` it judges the row's own candidates by, and for the
// `session-raw` row that is `StaleAge` = 14 days — the *.raw.log file arm's
// window. Judging a run's evidence DIRECTORY by a FILE arm's window would make
// SessionScratchDirStaleAge a constant nothing reads while the doc and the
// registry row both claim it decides (measured: it was, until this floor
// existed; the red proof is
// TestSessionsScratchDirWindowFloorIsTheArmsOwn). A caller that asks for a
// LONGER window (--scratch-gc-stale-min above the floor) is honoured — the
// operator may always be more conservative — while a shorter one cannot shorten
// this arm: it is the one that deletes a run's evidence, and its own floor is
// the whole reason the family could move off `keep` at all.
func scratchDirWindow(age time.Duration) time.Duration {
	if age < SessionScratchDirStaleAge {
		return SessionScratchDirStaleAge
	}
	return age
}

// containerHasNoLedger is THE PREDICATE. It answers the one question the family
// needs — has the ledger finished with this container? — by reading the
// container's entries ONCE. TRUE is the ONLY thing that makes a directory inside
// it collectable; every other answer, and every failure to measure, is FALSE.
//
// held is the caller's ONE indexed /proc pass for the tree (heldSet), passed in
// rather than re-derived so the sweep's liveness answer stays one verdict.
func containerHasNoLedger(container string, now time.Time, age time.Duration, held heldTreeScan) bool {
	// AN UNFINISHED HOLDER PASS IS A FACT ABOUT THE SCAN, NOT ABOUT THE PATH, so
	// it is applied ONCE — in scratchDirCandidate, before this predicate is
	// asked at all: a pass that stopped at its bound has not established that
	// nobody holds the container, and an unmeasured fact authorises nothing
	// (Rule 15). It is deliberately NOT re-checked here: a second spelling of
	// one rule is what Rules 13/14 ban, and the call site is one call away.
	// A live process inside the container ends the question before anything is
	// read: it is working there, whatever the ledger says. Asked on the arm's
	// ONE pass, through the index the walk built for this tree (the candidate is
	// spelled by filepath.Join from the root the index is keyed on, so one map
	// hit answers it).
	if heldIn(container, held.index) {
		return false
	}
	// A container a WRITER registered (the `.holder` record) is a container
	// somebody owns, at any age and whatever its entries say — the existing
	// registration verdict (unheldRegistered -> OwnerKeeps), asked here because
	// the arm is about to take everything inside it: a container whose owner
	// record is unreadable or live is a KEEP, never a delete on a missing
	// record (Rule 15).
	if !unheldRegistered(container) {
		return false
	}
	entries, err := os.ReadDir(container)
	if err != nil {
		return false // unmeasurable: keep, never guess (Rule 15)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == sessionlayout.MetadataFileName || sessionlayout.IsJSONL(name) {
			// The ledger describes this container: `metadata.json` is what
			// `gopi task show` and the session readers resolve a run through, and
			// a `*.jsonl` is a record that may still be resumed. Everything
			// inside it is KEPT, at any age — the container's own lifecycle
			// deciding, which is what the epic asks for.
			return false
		}
	}
	// No record of any kind. The last thing that can still make this container a
	// run IN FLIGHT rather than a forgotten one is its AGE: the writers mint the
	// container directory first and write the record into it seconds later, so a
	// container untouched for the whole window is not a run in progress — it is
	// one whose record never existed. A young container is a KEEP whatever else
	// is true of it: the answer to an unknown is KEEP, and the window is how
	// this arm stops guessing.
	//
	// The window is this ARM's (`scratchDirWindow`: the caller's, floored at
	// SessionScratchDirStaleAge — a sweep face may be MORE conservative, never
	// less). See scratchDirCandidate for why the caller's value is not enough on
	// its own: containerEntries judges the row's own *.raw.log candidates by the
	// row's 14-day StaleAge, and that is the value it hands down.
	stale, err := ScratchEntryStale(container, now, age)
	return err == nil && stale
}

// isSymlink reports whether path is a symbolic link — read with Lstat, so a link
// to a directory is still a link.
func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}
