// attachments_archive.go — the declared tree that BOUNDS the sessions tree's
// `attachments/` archives by EVIDENCE rather than by age.
//
// THE DEFECT THIS CLOSES (measured on this host, 2026-10-01/02). The family
// registry (internal/sessionlayout/families.go) classified `attachments/` with
// the decision `keep:` and the reason "unique evidence by operator principle —
// an age-based reclaim needs a ledger/branch-aware rule". A `keep:` row bounds
// NOTHING, and `attachments/` is the one family in that tree that grows by one
// directory PER TASK: measured 2026-10-02, 75,851 `attachments/` directories in
// this host's sessions tree.
//
// THE MEASUREMENT THAT DECIDES WHAT A BOUND MAY TAKE. Of those 75,851
// directories, 75,602 are EMPTY and 249 hold content. An empty archive holds no
// evidence, so it cannot be the last copy of anything — and it is still a
// directory every walker (the declared-tree sweep, the family census, a `du`)
// has to visit, which is the family's whole cost. The 249 non-empty ones are what
// the operator's principle protects: measured here,
// `01a0f3de-…/attachments/home/user/workspace/branch-capture-list-windows.bundle`
// (284,840,438 bytes) is the operator's own example of a last surviving copy.
//
// WHY AN EMPTINESS PREDICATE IS NOT AN AGE WINDOW. The constraint is explicit
// ("never an age window"). This predicate reads the candidate's SHAPE and
// CONTENT, never how old its contents are, and it never deletes content: the only
// candidates are ordinary DIRECTORIES carrying the archive's own name that hold
// NOTHING AT ALL. Any archive holding a single byte is KEPT, whatever its age —
// and so is an archive whose contents cannot be read, an archive that is a mount
// root, and an archive that cannot be probed.
//
// THE THREE QUESTIONS, each of which defaults to KEEP:
//
//  1. EMPTY?        readdir must succeed and yield nothing. An unreadable
//     directory is kept (Rule 15: never guess about a tree you
//     cannot inspect).
//  2. THE SAME DEVICE AS ITS PARENT? A directory that is a MOUNT ROOT holds
//     bytes on another filesystem: its "emptiness" describes the
//     mount point, not the mounted tree, and removing it would
//     either be refused or would hide a whole filesystem. The
//     test is the repo's own device-id criterion (Rule 22): a
//     comparison of st_dev, never a spelling.
//  3. WRITABLE, AND PROVABLY ABLE TO LOSE A FILE? The probe creates one
//     empty-byte regular file inside the candidate and removes
//     it; the remove must SUCCEED and the path must then be GONE
//     (checked with O_NOFOLLOW, so nothing can stand in for it).
//     A directory that cannot take and lose a file is not one
//     this sweep may collect — and a probe left behind by a
//     KILLED earlier pass is recognised by its own fixed name and
//     cleaned, while a probe that is still FRESH means another
//     pass is inside the candidate right now (KEEP).
//
// Every unknown is KEEP, which is what makes applying the bound safe at all: the
// worst case of this predicate being wrong is that a directory stays.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// AttachmentsTreeName is the declared tree's identity: the logfmt `tree=`
	// field on the watchdog line, the name the declared-set gate diagnoses
	// with, and the tree a sessionlayout `bound:` family names. The family
	// registry cannot import this package (importing it pulls in
	// internal/tools, whose own client of this package makes that a cycle), so
	// this constant's VALUE is held against the registry's
	// (sessionlayout.TreeSessionAttachments) by a pin that reads BOTH sources —
	// a pair of literals with nothing joining them is exactly the drift Rules
	// 13/14 ban.
	AttachmentsTreeName = "session-attachments"

	// AttachmentsArchiveDirName is the archive area's own leaf name — this
	// tree's NAMESPACE PREDICATE, and the only directory name it may ever take.
	// The value is internal/tools.taskAttachmentsDirName (which
	// internal/sessionlayout's TestFamilyNamesMatchWriters pins against that
	// writer) and sessionlayout.AttachmentsDirName. The writer imports THIS
	// package, so the constant cannot be imported back; the equality of the
	// three is asserted in this package's own test.
	AttachmentsArchiveDirName = "attachments"

	// attachmentsArchiveProbeName is the name of the empty regular file the
	// write-probe creates inside a candidate. It is FIXED and OURS, so a probe
	// a killed pass left behind is recognisable as ours rather than mistaken for
	// an archive's evidence.
	attachmentsArchiveProbeName = ".gopi-archive-probe"

	// AttachmentsArchiveProbeContent is what the probe file holds: nothing. A
	// NAMED value rather than an inline empty literal, so a reader can see that
	// the probe writes ZERO bytes — the point is that the write is observable by
	// stat, not that anything is stored.
	AttachmentsArchiveProbeContent = ""

	// AttachmentsArchiveProbeMaxAge is the age past which the probe file is
	// treated as STRANDED — left by a pass that was interrupted (a SIGKILL, a
	// power loss) — and is therefore ours to clean so the candidate can be
	// measured again. A probe younger than this is a probe IN FLIGHT from
	// another process: the candidate is kept (Rule 15 — never guess about a tree
	// somebody else is measuring).
	AttachmentsArchiveProbeMaxAge = 24 * time.Hour
)

// attachmentsArchiveNameIsOurs is the tree's namespace predicate: the only
// directory name this tree may take. An aged directory called anything else
// inside a session container is NOT ours and is never a candidate, whatever it
// holds — a container is the ledger's own directory.
func attachmentsArchiveNameIsOurs(name string) bool { return name == AttachmentsArchiveDirName }

// attachmentsArchiveProbePath is the probe file's path inside one candidate —
// the ONE spelling of it.
func attachmentsArchiveProbePath(dir string) string {
	return filepath.Join(dir, attachmentsArchiveProbeName)
}

// attachmentsArchiveProbeRecheck removes a probe left behind by an interrupted
// pass, when (and only when) it is identifiably ours — a REGULAR file under the
// probe's own fixed name, older than AttachmentsArchiveProbeMaxAge. It reports
// whether the candidate may be MEASURED at all; false means KEEP.
func attachmentsArchiveProbeRecheck(dir string, now time.Time) bool {
	probe := attachmentsArchiveProbePath(dir)
	fi, err := os.Lstat(probe)
	if err != nil {
		if os.IsNotExist(err) {
			return true // no probe: the candidate is as the writer left it
		}
		return false // unmeasurable: KEEP (Rule 15)
	}
	if !fi.Mode().IsRegular() {
		return false // a foreign object under our probe's name: not ours to remove
	}
	if now.Sub(fi.ModTime()) <= AttachmentsArchiveProbeMaxAge {
		return false // a probe in flight: another pass is inside this candidate
	}
	return os.Remove(probe) == nil
}

// attachmentsArchiveSameDevice reports whether path sits on the SAME filesystem
// as its parent — the device-id criterion (Rule 22) applied to the mount-root
// question: a candidate whose st_dev differs from its container's is a mount
// root, so its emptiness describes the mount point while the bytes live on the
// mounted filesystem.
//
// It is a comparison of measured st_dev values, never of path spellings: a
// bind mount, a tmpfs or a foreign disk all answer the same way, and an
// environment where the stat itself fails answers "not the same device" — the
// KEEP direction.
func attachmentsArchiveSameDevice(path, parent string) bool {
	pd, perr := deviceIDOf(path)
	cd, cerr := deviceIDOf(parent)
	if perr != nil || cerr != nil {
		return false // unmeasurable: KEEP
	}
	return pd == cd
}

// deviceIDOf returns one path's device id (st_dev), or the stat's error.
func deviceIDOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}

// attachmentsArchiveWritable is the capability half of the probe: an empty-byte
// file must be creatable inside the candidate, removable, and PROVABLY gone
// afterwards.
//
// false is KEEP, and it covers every unknown: the file cannot be created (not
// writable, an immutable or read-only mount), it cannot be written, it cannot be
// removed, or something still answers to its name after the remove. That last
// case is why the re-open carries O_NOFOLLOW: a path that survives its own
// removal is either locked (immutable) or a symlink somebody swapped in while the
// probe was open, and neither is a directory this sweep may collect.
func attachmentsArchiveWritable(dir string) bool {
	probe := attachmentsArchiveProbePath(dir)
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, FilePerm)
	if err != nil {
		return false
	}
	if _, werr := f.Write([]byte(AttachmentsArchiveProbeContent)); werr != nil {
		// The write failed: the candidate is unmeasurable, so it is KEPT — and
		// the probe this call created is cleaned up on the best-effort path,
		// because leaving it behind would make the NEXT pass read the candidate
		// as unmeasurable too (a probe of ours, under our own fixed name).
		_ = f.Close()        //stylegate:ignore best-effort close of a probe this call is abandoning; the verdict is already KEEP
		_ = os.Remove(probe) //stylegate:ignore best-effort cleanup of our own probe; the verdict is already KEEP
		return false
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(probe) //stylegate:ignore best-effort cleanup of our own probe; the verdict is already KEEP
		return false
	}
	if rerr := os.Remove(probe); rerr != nil {
		return false
	}
	fd, oerr := syscall.Open(probe, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if oerr == nil {
		_ = syscall.Close(fd) //stylegate:ignore closing a descriptor we only opened to answer "is it still there"; the answer is already KEEP
		return false
	}
	// ENOENT — created, written empty, removed, and provably gone — is the ONLY
	// collectable answer. Any other errno (EACCES, ELOOP, …) is unmeasured.
	return oerr == syscall.ENOENT
}

// attachmentsArchiveHoldsNoEvidence is the tree's EVIDENCE verdict for one
// candidate: may this archive be said to hold nothing, on this device, in a
// directory we can write to and lose a file from?
//
// It is the ONE function that decides, and it is a VAR rather than a direct call
// for the same reason internal/sessionlayout exposes ScanFamiliesWith: the
// red-proof must be able to drive the REAL sweep over a NEUTERED verdict and show
// that the sweep then takes an archive which DOES hold evidence. A red-proof that
// re-implemented the walk would prove nothing about the code that runs. Production
// reads the default and nothing in the tree assigns it.
//
// THE PROBE LEAVES NO TRACE — INCLUDING THE CANDIDATE'S OWN TIMESTAMPS. Both
// steps below WRITE inside the directory (removing a stranded probe; creating and
// removing this pass's probe) and a DIRECTORY'S mtime moves when an entry inside
// it is created or removed. Without the restore, one pass would make its own
// candidate read as FRESH for the whole window — measured, and the reason this
// line exists: `scratch-gc trees --dry-run` listed the archive as a candidate and
// the sweep immediately after it found nothing stale, because the dry run had
// bumped the mtime the age rule reads.
//
// A restore that fails is the conservative direction and does not change THIS
// pass's verdict: the candidate keeps the fresh timestamp, so the NEXT pass keeps
// it too, until it ages again on its own.
var attachmentsArchiveHoldsNoEvidence = func(dir string, now time.Time) bool {
	before, statErr := os.Stat(dir)
	if statErr != nil {
		return false // unmeasurable: KEEP (Rule 15)
	}
	if !attachmentsArchiveProbeRecheck(dir, now) {
		return false
	}
	children, rerr := os.ReadDir(dir)
	if rerr != nil {
		return false
	}
	if len(children) != 0 {
		return false // it holds something: evidence, or bytes this tree cannot measure
	}
	if !attachmentsArchiveSameDevice(dir, filepath.Dir(dir)) {
		return false // a mount root: the emptiness is the mount point's, not the tree's
	}
	ok := attachmentsArchiveWritable(dir)
	// The probe's trace, undone: the directory is returned to a reader exactly as
	// it was found, so the age rule sees the age the WRITER left.
	_ = os.Chtimes(dir, before.ModTime(), before.ModTime()) //stylegate:ignore best-effort restore of the candidate's own timestamp; a failure only makes the next pass keep it
	return ok
}

// attachmentsArchiveHeldIndex is the tree's ONE /proc holder pass. It is a var so
// a test can substitute a synthetic index — the shape this package already uses
// for the evidence verdict — because a holder is a live process the test cannot
// schedule deterministically; the red proof below drives the REAL sweep with a
// holder injected, which is what makes "the run's own archive survived" a claim
// about production code.
var attachmentsArchiveHeldIndex = func(root string) map[string]struct{} {
	return heldSet(ScanHolders(), root)
}

// attachmentsArchiveCollectable is the tree's ONE predicate: may this candidate
// be collected?
//
// The order is the contract — cheapest and most decisive first, and EVERY refusal
// is a KEEP:
//
//  1. it is not a directory            → KEEP (this tree takes directories only)
//  2. it is not the archive's own name → KEEP (the namespace predicate)
//  3. it is a declared tree root       → KEEP at any age (the universal invariant)
//  4. it is held by a live process     → KEEP (the .holder registration AND the
//     indexed /proc verdict — the sweep's own)
//  5. it is younger than the window    → KEEP (age is the LAST and weakest lever)
//  6. its evidence verdict is no       → KEEP (empty, same device, writable)
//
// A candidate that survives all six is an ordinary empty directory of this
// repository's own archive family: it holds no evidence and no bytes, and it is
// collectable.
func attachmentsArchiveCollectable(path string, isDir bool, now time.Time, age time.Duration, held map[string]struct{}) bool {
	if !isDir {
		return false
	}
	if !attachmentsArchiveNameIsOurs(filepath.Base(path)) {
		return false
	}
	if IsDeclaredTreeRoot(path) {
		return false
	}
	if heldIn(path, held) {
		return false
	}
	stale, err := ScratchEntryStale(path, now, age)
	if err != nil || !stale {
		return false
	}
	if !unheldRegistered(path) {
		return false
	}
	return attachmentsArchiveHoldsNoEvidence(path, now)
}

// AttachmentsArchivePlan lists the archive candidates a sweep may collect under
// root: the SAME listing the collect face removes, measured the same way (one
// predicate, two faces).
//
// A root that does not exist yields an EMPTY list and no error — a host whose
// sessions tree was never created has no archive family, and the declared-tree
// registry reports a tree it cannot resolve as unconfigured elsewhere. A root that
// EXISTS and cannot be READ is an error, never an empty list (Rule 15: "nothing is
// there" and "I could not look" must not render the same).
//
// The walk descends exactly two levels: the sessions root holds the containers,
// each container holds its own `attachments/`. Anything deeper belongs to a
// container's own subtree, which no family of this tree owns.
func AttachmentsArchivePlan(root string, now time.Time, age time.Duration) ([]TreeEntry, error) {
	containers, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions tree %s: %w", root, err)
	}
	// ONE /proc pass for the whole tree, indexed once (heldSet): the per-candidate
	// form costs a full process-table walk per candidate (the measured trap this
	// package shipped once already — see declared_trees.go).
	//
	// The pass is behind a var for the same reason the evidence verdict is
	// (attachmentsArchiveHoldsNoEvidence): a registry swap is how this package
	// red-proofs a decision built on state a test cannot schedule, and a holder is
	// exactly that — a live process the sweep must respect.
	held := attachmentsArchiveHeldIndex(root)
	out := []TreeEntry{}
	for i, container := range containers {
		if i >= TreeEntryScanLimit {
			return out, fmt.Errorf("sessions tree %s has more than %d containers — stopped early (TreeEntryScanLimit)", root, TreeEntryScanLimit)
		}
		if !container.IsDir() {
			continue
		}
		dir := filepath.Join(root, container.Name(), AttachmentsArchiveDirName)
		fi, serr := os.Lstat(dir)
		if serr != nil {
			continue // no archive area in this container (the common case), or unmeasurable: KEEP
		}
		if !fi.IsDir() {
			continue // the name is taken by something that is not a directory: never ours
		}
		// THE CONTAINER IS PART OF THE QUESTION, measured 2026-10-16 (task
		// 01a142630844745eb09a754876bcec9a). The veto below asks about the
		// ARCHIVE DIRECTORY, and a live run does not stand in its archive: it
		// stands in its CONTAINER (`cd $S`, the shape every evidence-writing bash
		// call uses — Rule 37), so `heldIn(dir)` cannot see it. Measured against
		// this very sweep, with a live process whose cwd was the container:
		// `<container>/attachments` came back `would-remove tree=session-attachments`
		// for a container carrying metadata.json — a LIVE run's own attachments.
		// The reclaim is bounded by the archive being EMPTY (an archive holding
		// bytes is refused further down), which is exactly why the file is not
		// enough: an EMPTY archive that disappeared costs a directory with real
		// consumers — `gopi task attachments` resolves `<GOPI_TASK_SESSION_DIR>/
		// attachments`, and a task's attachments land there when the file is
		// already on disk (the no-op COPY in `internal/tools/task_attachments.go`).
		// So the container's own holder decides, the way the session-raw arm's does.
		if heldByAncestor(dir, filepath.Join(root, container.Name()), held) {
			continue
		}
		if !attachmentsArchiveCollectable(dir, true, now, age, held) {
			continue
		}
		// The candidate is an EMPTY directory, so its own bytes are zero: the
		// reclaim this row reports is the entry COUNT it removes, which is the
		// unit this family actually costs (75,602 directory visits). The age is
		// reported for the same reason the other trees report it — so an operator
		// reading a plan can see which window admitted the candidate.
		out = append(out, TreeEntry{Path: dir, IsDir: true, Age: now.Sub(fi.ModTime())})
	}
	return out, nil
}

// SweepAttachmentsArchive is the tree's COLLECT face: it removes exactly the
// candidates AttachmentsArchivePlan listed, re-checking the root invariant, the
// live-holder verdict and the owner registration at each delete through
// removeCollectableOfTree — the same delete path every other declared tree uses,
// so "never remove a tree a live process holds" stays one rule with one
// implementation.
//
// A candidate that disappears between the plan and its own delete (a concurrent
// sweep) is REPORTED and skipped, never a failure of the pass.
func SweepAttachmentsArchive(rowName, root string, age time.Duration) (int, int64, error) {
	entries, err := AttachmentsArchivePlan(root, time.Now(), age)
	if err != nil {
		return 0, 0, err
	}
	tree := DeclaredTree{Name: rowName, Class: TreeClassSessionArchive}
	removed := 0
	var freed int64
	for _, e := range entries {
		if rerr := removeCollectableOfTree(tree, e.Path); rerr != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove attachments archive %s (%s): %v\n",
				e.Path, tree.Name, rerr)
			continue
		}
		removed++
		freed += e.Size
	}
	return removed, freed, nil
}

// attachmentsArchiveTree assembles the declared-tree row: a directory-shaped tree
// whose namespace predicate is the archive's own name, whose candidates are listed
// AND collected by the two closures below (one listing, so the plan can never
// promise a different set from the one the sweep takes), and whose root is the
// sessions tree's ONE resolver.
//
// The class is its own kind: these candidates are not raw logs, cache keys or
// toolchain work directories, and a reader of a log line must be able to tell
// which bound a line reports.
func attachmentsArchiveTree() DeclaredTree {
	return DeclaredTree{
		Name:       AttachmentsTreeName,
		Class:      TreeClassSessionArchive,
		Kind:       EntryKindDir,
		StaleAge:   SessionAttachmentsStaleAge,
		EntryMatch: attachmentsArchiveNameIsOurs,
		List: func(age time.Duration) ([]TreeEntry, error) {
			root, err := SessionRawLogRoot()
			if err != nil {
				return nil, err
			}
			return AttachmentsArchivePlan(root, time.Now(), age)
		},
		Sweep: func(age time.Duration) (int, int64, error) {
			root, err := SessionRawLogRoot()
			if err != nil {
				return 0, 0, err
			}
			return SweepAttachmentsArchive(AttachmentsTreeName, root, age)
		},
		Resolve: SessionRawLogRoot,
	}
}
