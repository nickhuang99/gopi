package tools

// merger_status.go — the LOCK-LIVENESS PROBE of the work lock.
//
// WHAT THIS FILE IS (and what it was). It used to be the whole `gopi merger
// --status` surface: a read-only render of a RESIDENT merger (its lock, its log,
// its schedule, the merge rounds it had launched). That resident merger was
// retired on 2026-09-19 (devdoc/merger-retired.md) and its render is deleted
// here. What REMAINS — and what this file is now — is the part that was never
// about the merger: the probe answering "does a LIVE process hold the serve/work
// flock, and if not, is something stray holding an UNLINKED copy of it?".
//
// WHO NEEDS IT (Rule 13: the survivors are named, not implied). The probe is the
// evidence layer of the SHARED work lock, which every live face takes:
//
//	cmd/agent/cli_worker_control.go — the worker-control surface (StatusAlive /
//	    StatusNotRunning, LockHolderNote, ReadStampedPID, OrphanLockHolder,
//	    LockProbe).
//	cmd/agent/cli_tasks.go — the serve/merge work lock (OrphanLockHolder,
//	    KernelFlockRecords, InodeOfFileInfo) and its stray-holder diagnosis.
//	internal/tools/service_control.go — the shared control-file surface
//	    (state/request placement, the pid↔name encoding); its liveness
//	    predicate DELEGATES to internal/procstat, which owns its own procfs
//	    root constant for that walk.
//
// THE SIDE-EFFECT FREEDOM IS THE CONTRACT. Every function here READS: it opens
// no lock, writes no state and spawns nothing, so a mere question ("who holds
// it?") cannot make the asker look like the holder. That is why the answers come
// from the kernel's own tables (/proc/locks, /proc/<pid>/fdinfo) rather than
// from a probe that would have to take the lock to answer.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	// procRoot is the procfs mount point: the lock table AND the per-process
	// fd directories the orphan probe walks both live under it.
	procRoot = "/proc"

	// procLockFile is the kernel's POSIX/flock lock table. Reading it is the
	// ONLY way to ask "does a live process hold this flock?" without taking
	// the lock ourselves.
	procLockFile = procRoot + "/locks"

	// procFdDirName is the per-process fd directory under /proc/<pid>/.
	procFdDirName = "fd"

	// procFdInfoDirName is the per-process fdinfo directory under /proc/<pid>/.
	// Unlike the global /proc/locks table, fdinfo is keyed by the process that
	// holds the descriptor RIGHT NOW, which is the only way to name the live
	// holder of an INHERITED flock (see fdInfoHoldsFlockOn).
	procFdInfoDirName = "fdinfo"

	// fdInfoInoField is the fdinfo label carrying the descriptor's inode — the
	// join key back to the lock file.
	fdInfoInoField = "ino:"

	// fdInfoLockField is the fdinfo label carrying the lock descriptions this
	// descriptor holds. Absent for a plain open (no lock), so its presence with
	// a FLOCK type is what proves the process is a HOLDER, not a reader.
	fdInfoLockField = "lock:"

	// procDeletedSuffix is the marker the kernel appends to a descriptor whose
	// path was unlinked while the fd stayed open ("<path> (deleted)"). It is the
	// ONLY reliable signal of the unlink-underneath state: the inode behind it is
	// unreachable by name, so the lock table cannot be joined to the path at all
	// (2026-09-12 incident).
	procDeletedSuffix = " (deleted)"

	// flockLockType is the lock-table column that marks a BSD flock (as opposed
	// to POSIX fcntl) — GoPi's singleton is syscall.Flock, never fcntl. A POSIX
	// lock on the same inode is NOT the singleton.
	flockLockType = "FLOCK"

	// StatusAlive / StatusNotRunning are the two states the liveness probe can
	// report, spelled once (Rule 1b: these strings are compared and rendered,
	// never typed inline at a second site). They are consumed by the
	// worker-control surface, whose holder question is the same one this probe
	// already answered.
	StatusAlive      = "alive"
	StatusNotRunning = "not running"

	// StatusOrphanLocked is the third state — the one that made this probe's
	// first version lie (2026-09-12): the lock PATH is unheld (its inode has no
	// FLOCK row), yet a LIVE process still locks an UNLINKED copy of that path.
	// "not running" is then factually wrong about the machine — a merge IS
	// running — and the single-instance guard is defeated, so the word has to
	// say so.
	StatusOrphanLocked = "locked by a stray holder (lock path was unlinked)"

	// StatusTakeoverUnlocked is the FOURTH state (2026-09-17): a LIVE merge round
	// is in flight while nothing holds the lock. The word names BOTH halves — a
	// merge IS running, and the singleton guard is DOWN — because either half
	// alone misleads. Kept here (rather than deleted with the status render that
	// introduced it) because the shared lock's classifier still resolves to it.
	StatusTakeoverUnlocked = "running (merge round in flight; single-instance lock NOT held — the guard is down)"

	// StatusHolderGone is the FIFTH state: the lock FILE still carries a pid
	// stamp, and the kernel's lock table says no LIVE process holds the flock —
	// i.e. the last holder EXITED and left its stamp behind. It is separated
	// from StatusNotRunning because the two are different machines:
	//
	//	StatusNotRunning  → nothing was ever here (no lock file, or one never
	//	                    stamped). Start a merger.
	//	StatusHolderGone  → a merger RAN here and is gone (the stamp is the
	//	                    evidence, and the lock is now free).
	//
	// The wire word is the MACHINE token. It deliberately avoids the word
	// "stale": "stale" is LockHolderNote's vocabulary for a different
	// measurement.
	StatusHolderGone = "holder gone"
)

// LockHolderNote is the qualification on a `lock holder:` line whose flock is
// NOT held by a live process. It takes the pid the line NAMES (Rule 15: the
// sentence must not assert more than the probe produced).
//
// THE DEFECT IT CLOSES (measured 2026-09-18, the same class the merger-status
// epic is about). The old form took one boolean and printed one sentence:
//
//	lock holder: pid 396084  (stale: the process is gone; the lock is FREE)
//	takeover:    pid 396084 running for 1m0s; …
//
// — the SAME pid declared gone on one line and running for a minute on the next,
// printed by one command about one machine, while /proc/396084/stat read state
// `S` and `flock -n serve.lock` succeeded. Two MEASUREMENTS were welded into one
// claim: LockProbe's `live=false` means "the kernel's lock table names no LIVE
// holder for this inode" (equivalently: the flock is FREE), and it says NOTHING
// about whether the stamped pid exists — but the sentence asserted death.
//
// So each measurement gets its own clause, and the pid is probed on the one
// liveness predicate the package already owns (pidAlive — zombie-aware, the same
// probe the lock table's rows are filtered by): a FREE lock beside a stamped pid
// that is still alive is reported as exactly that, and "gone" is claimed only
// when /proc agrees.
func LockHolderNote(pid int, live bool) string {
	if live || pid <= 0 {
		return ""
	}
	if pidAlive(pid) {
		return "  (the lock is FREE — no live process holds the flock; the pid named here is STILL ALIVE, so it is not the holder)"
	}
	return "  (stale: the pid named here is gone; the lock is FREE)"
}

// mergerLockProbe answers "does a LIVE process hold the merger's flock?"
// WITHOUT taking the lock — by looking up the lock file's inode in the
// kernel's lock table (/proc/locks) and reporting the holder it names.
//
// This is a read-only observer: no open-for-write, no flock, no pid stamp.
// A missing /proc/locks (non-Linux) degrades to the stamped pid plus a
// signal-0 aliveness check, so the command still answers usefully instead of
// erroring (Rule 15).
//
// THE INHERITED-HOLDER CORRECTION (2026-09-13, row 01a09264 follow-up): the
// lock the kernel records in /proc/locks is attributed to the process that
// CREATED it, and `flock` lives on the OPEN FILE DESCRIPTION — so a fork/exec
// child that inherits the descriptor KEEPS the lock after the creator exits.
// GoPi's daemon is exactly that shape (`gopi-merger merger` forks
// `gopi-merger-dae`), so the table can name a pid that is already GONE while
// the lock is held by a live descendant. Observed on the real machine:
// /proc/locks named pid 98279 (dead) while pid 98290 still held the inherited
// descriptor (fdinfo showed the same inode + the FLOCK row) — a status
// command whose whole job is "is it running?" reported a corpse as the
// holder. The kernel's table cannot be trusted alone for liveness: when it
// names a pid that is not alive, walk the descriptor table for a LIVE process
// holding a lock on the same inode (fdInfoLockHolder) before concluding.
func LockProbe(lockPath string) (holderPID int, live bool) {
	stamped := ReadStampedPID(lockPath)

	st, err := os.Stat(lockPath)
	if err != nil {
		// No lock file at all: nothing can hold it.
		return stamped, false
	}
	table, err := procLockTableRead()
	if err != nil {
		// Cannot read the kernel table (non-Linux): the stamped pid plus a
		// signal-0 probe is the best available answer.
		return stamped, pidAlive(stamped)
	}
	held, pid := lockTableHolds(strings.Split(strings.TrimRight(string(table), "\n"), "\n"), inodeOf(st))
	if held && pidAlive(pid) {
		return pid, true
	}
	if orphan := OrphanLockHolder(lockPath); orphan > 0 {
		// The inode join is blind to an unlinked holder; the orphan probe is
		// the only evidence for that state (2026-09-12 incident).
		return orphan, true
	}
	if live := fdInfoLockHolder(inodeOf(st)); live > 0 {
		return live, true
	}
	// Nothing live holds the inode: report the table's (dead) pid if it named
	// one, so the operator sees WHO left the lock behind, else the stamp.
	if held && pid > 0 {
		return pid, false
	}
	return stamped, false
}

// fdInfoLockHolder returns the pid of a LIVE process whose descriptor table
// holds a FLOCK on ino — the holder /proc/locks cannot name when the lock was
// inherited (see mergerLockProbe).
//
// fdinfo is per-process, so a hit here is proof of a LIVE holder (the entry
// only exists while the process does) and needs no aliveness check of its
// own. Scanning is bounded by the machine's process count; unreadable
// /proc entries are skipped, never guessed at (Rule 15).
func fdInfoLockHolder(ino uint64) int {
	pids, _ := fdInfoLockHolders(ino)
	if len(pids) == 0 {
		return 0
	}
	return pids[0]
}

// fdInfoLockHolders lists EVERY live process whose descriptor table holds a
// FLOCK on ino (pids ascending). It is fdInfoLockHolder's exhaustive form, and
// it exists because a MERGED lock table is a table someone will read wrongly:
// fdInfoLockHolder returns "the" holder, which is a claim about a set that can
// have more than one member. Two shapes make that real — the daemon and a
// process it spawned, both holding an inherited descriptor, and two daemons
// that raced past the identity retry. A caller that must stop something (the
// merger --stop CLI) needs all of them; a caller that only reports liveness
// (LockProbe) needs one.
//
// The second return value says whether the scan was POSSIBLE at all (a
// readable /proc): an empty list from a readable /proc means "nobody holds it",
// an empty list from no /proc means "no evidence either way" — the difference
// between those two is the whole point of the stop path's authority.
//
// Scanning is bounded by the machine's process count; unreadable /proc entries
// are skipped, never guessed at (Rule 15).
func fdInfoLockHolders(ino uint64) ([]int, bool) {
	if ino == 0 {
		return nil, false
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, false
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	out := make([]int, 0, 1)
	for _, pid := range pids {
		if fdInfoHoldsFlockOn(pid, ino) {
			out = append(out, pid)
		}
	}
	return out, true
}

// LockEvidence is ONE process proved to hold the merger's singleton lock, plus
// the SHAPE of that proof (Rule 15: a caller about to kill a pid must be able to
// tell the operator which fact named it).
type LockEvidence struct {
	PID int
	// Stray is true when the process holds an UNLINKED copy of the lock path
	// (the lock file was removed underneath it, the 2026-09-12 incident): it is
	// still a merge process — that is what holding this lock means — but its
	// guard is detached from the name a status probe resolves.
	Stray bool
}

// LockEvidences answers "which LIVE processes hold the merger's singleton
// lock?", read-only, and reports whether the question was answerable at all.
//
// WHY A SET, THE STOP PATH'S ROOT CAUSE (2026-09-13, row merger-stop-pid):
// LockProbe answers "is a live process holding this flock?" with ONE pid —
// correct for a status line, WRONG as a stop target. /proc/locks rows are per
// (pid, open-file-description), and an inherited descriptor gives a second
// process a lock row of its own; fdInfoLockHolder's scan then returns the
// lowest pid it finds, which need not be the process that (a) is stamped in the
// lock file, (b) wrote the state file the CLI resolved its target from, or
// (c) is the one keeping the daemon alive. A stop that signals only the first
// holder reports a clean exit while the daemon keeps merging.
//
// The second return value distinguishes "the lock is free" (probed=true, no
// evidence) from "this platform cannot tell" (probed=false) — the stop path
// treats the second as a documented degradation to the state file, never as a
// licence to invent an answer.
//
// The DISTINCT set is deduplicated: the /proc/locks row, the fdinfo scan and
// the stray-holder probe can name the same process, and signalling a pid twice
// is pointless noise. A pid is a member only while a probe named it — never a
// guess from the stamped pid (that stamp is advisory, and the defect being
// fixed here was a target chosen from a record instead of from the lock).
func LockEvidences(lockPath string) ([]LockEvidence, bool) {
	var out []LockEvidence
	seen := map[int]bool{}
	add := func(pid int, stray bool) {
		if pid <= 0 || seen[pid] {
			return
		}
		seen[pid] = true
		out = append(out, LockEvidence{PID: pid, Stray: stray})
	}
	ino := uint64(0)
	st, statErr := os.Stat(lockPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			// No lock file at all: nothing can hold it, and that IS an answer
			// ("the lock is free"), not a missing probe.
			return nil, true
		}
		return nil, false
	}
	ino = inodeOf(st)
	probed := false
	if ino != 0 {
		if table, err := procLockTableRead(); err == nil {
			probed = true
			// ALIVENESS IS PART OF THE EVIDENCE for a table row: /proc/locks
			// attributes the lock to the process that CREATED it, so the row
			// routinely names a pid that is already gone (GoPi's daemon forks
			// its own child and the creator exits — the inherited-flock
			// correction, LockProbe). Adding a DEAD pid here would hand the
			// stop path a recycled pid to signal — the very class of accident
			// this command is being fixed for — so a row counts only while its
			// process is alive. (The fdinfo scan and the stray probe below find
			// only live processes by construction.)
			if held, pid := lockTableHolds(strings.Split(strings.TrimRight(string(table), "\n"), "\n"), ino); held && pidAlive(pid) {
				add(pid, false)
			}
		}
		if pids, ok := fdInfoLockHolders(ino); ok {
			probed = true
			for _, pid := range pids {
				add(pid, false)
			}
		}
	}
	if stray := OrphanLockHolder(lockPath); stray > 0 {
		probed = true
		add(stray, true)
	}
	return out, probed
}

// fdInfoHoldsFlockOn reports whether pid holds a FLOCK on ino. A descriptor
// counts only when its fdinfo carries BOTH the matching `ino:` and a `lock:`
// line whose type is FLOCK — an fd that merely has the file open (the common
// case) has the inode but no lock line, and a POSIX row is somebody else's
// mechanism (mirrors lockTableHolds).
func fdInfoHoldsFlockOn(pid int, ino uint64) bool {
	fdInfoDir := filepath.Join(procRoot, strconv.Itoa(pid), procFdInfoDirName)
	entries, err := os.ReadDir(fdInfoDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(fdInfoDir, e.Name()))
		if err != nil {
			continue
		}
		if fdInfoBytesHoldFlockOn(b, ino) {
			return true
		}
	}
	return false
}

// fdInfoBytesHoldFlockOn is the pure predicate over one fdinfo block, kept
// separate so the format handling is testable without a live /proc.
//
// The inode check is STRICT: a `lock:` row that itself names a different
// inode disqualifies the descriptor even when the fd's own `ino:` matches.
// That combination (matching fd, foreign lock inode) does not occur for a
// single-inode flock, but accepting it would let any unrelated lock on the
// descriptor be read as a holder of THIS file.
func fdInfoBytesHoldFlockOn(b []byte, ino uint64) bool {
	matchedIno, matchedFlock := false, false
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case fdInfoInoField:
			if len(fields) < 2 {
				continue
			}
			if got, err := strconv.ParseUint(fields[1], 10, 64); err == nil && got == ino {
				matchedIno = true
			}
		case fdInfoLockField:
			// "\tlock:\t1: FLOCK  ADVISORY  WRITE 98279 08:04:29112398 0 EOF"
			if !fdInfoLockRowIsFlockOn(fields[1:], ino) {
				continue
			}
			matchedFlock = true
		}
	}
	return matchedIno && matchedFlock
}

// fdInfoLockRowIsFlockOn reports whether one fdinfo `lock:` row's remaining
// fields describe a FLOCK whose inode column (dev:ino) is ino. Rows that name
// another inode are not this file's lock; rows whose shape we cannot parse are
// not evidence either (Rule 15: never upgrade absent evidence).
func fdInfoLockRowIsFlockOn(fields []string, ino uint64) bool {
	if len(fields) < 6 {
		return false
	}
	found := false
	for _, f := range fields {
		if f == flockLockType {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	devIno := fields[len(fields)-3] // "... <pid> <dev>:<ino> <start> <end>"
	got, err := strconv.ParseUint(lastOf(strings.Split(devIno, ":")), 10, 64)
	return err == nil && got == ino
}

// procLockTableRead is the seam that keeps the parser hermetic: reading the
// real /proc/locks is the only non-portable part of the probe (tests inject a
// fixture table).
var procLockTableRead = func() ([]byte, error) { return os.ReadFile(procLockFile) }

// KernelFlockRecords returns the /proc/locks lines holding a FLOCK on ino —
// the kernel's own answer to "who holds this lock", exported for the serve-lock
// refusal DIAGNOSIS (cmd/agent), which must report the records for the inode
// the acquirer was ACTUALLY refused on rather than for whatever the lock PATH
// resolves to now (epic serve-lock-diagnosis-liveness).
//
// The second return (an error) exists so an UNREADABLE table is announced
// rather than rendered as "no records": an empty slice plus a nil error means
// the kernel answered "nobody holds it", which is evidence — no procfs (or a
// hardened /proc) is the absence of evidence, and the two must never be
// printed the same way (Rule 15).
//
// It routes through procLockTableRead, so the diagnosis, the status surface
// and the stop surface all read the table through ONE parser — a second
// /proc/locks reader in cmd/agent would be a second answer to "who holds it?".
func KernelFlockRecords(ino uint64) ([]string, error) {
	if ino == 0 {
		return nil, nil
	}
	table, err := procLockTableRead()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(table), "\n"), "\n")
	var out []string
	for _, line := range lines {
		cols := strings.Fields(line)
		if len(cols) < 8 || cols[1] != flockLockType {
			continue
		}
		got, err := strconv.ParseUint(lastOf(strings.Split(cols[5], ":")), 10, 64)
		if err != nil || got != ino {
			continue
		}
		out = append(out, strings.Join(cols, " "))
	}
	return out, nil
}

// InodeOfFileInfo exposes the inode join key (the private inodeOf) to the
// serve-lock refusal diagnosis, which must match the kernel table on the
// ACQUIRED DESCRIPTOR's inode. 0 means "this platform's FileInfo carries no
// inode" — callers must treat that as unprovable, never as inode zero.
func InodeOfFileInfo(st os.FileInfo) uint64 {
	if st == nil {
		return 0
	}
	return inodeOf(st)
}

// readStampedPID reads the pid a holder wrote into the lock file (0 when
// absent/garbage — the file is advisory, the flock is the guard).
func ReadStampedPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}

// NOTE: aliveness probing is NOT re-implemented here — pidAlive lives in
// taskzombie.go (the same package) and is the one signal-0 predicate.

// readLockTable was factored out during the first draft and is no longer
// needed: mergerLockProbe now reads the table through the procLockTableRead
// seam in one pass.

// lockTableHolds scans /proc/locks lines for a FLOCK entry on ino and returns
// the holding pid. The table's format is fixed and documented:
//
//	1: FLOCK  ADVISORY  WRITE 970423 08:03:4989291 0 EOF
//	^  ^                  ^      ^     ^           ^
//	id type               pid    dev   inode       start end
//
// Only FLOCK rows count (GoPi's singleton is syscall.Flock; a POSIX row on
// the same inode belongs to some other, unrelated mechanism).
func lockTableHolds(lines []string, ino uint64) (bool, int) {
	for _, line := range lines {
		cols := strings.Fields(line)
		if len(cols) < 8 || cols[1] != flockLockType {
			continue
		}
		pid, err := strconv.Atoi(cols[4])
		if err != nil {
			continue
		}
		got, err := strconv.ParseUint(lastOf(strings.Split(cols[5], ":")), 10, 64)
		if err != nil || got != ino {
			continue
		}
		return true, pid
	}
	return false, 0
}

// inodeOf is the join key between the lock file and /proc/locks. Only the
// inode is compared (not the device): the table lives on one machine and the
// lock file on one filesystem, so an inode collision would require a second
// filesystem's coincidentally-equal inode AND a FLOCK row on it — a
// false-positive class we accept rather than pulling in the major/minor
// decode for a status readout.
func inodeOf(st os.FileInfo) uint64 {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Ino)
	}
	return 0
}

func lastOf(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// OrphanLockHolder answers the question the inode join CANNOT: "does a live
// process still hold a flock on an UNLINKED copy of this lock path?"
//
// Why it is needed (2026-09-12 incident): `flock` is attached to the INODE,
// not to the name. An `rm ~/.gopi/serve.lock` (or any unlink) while a merger
// is running leaves that merger locking an inode no name points to any more;
// the kernel creates a FRESH inode for the next open, whose lock is free — so
// a second daemon's acquireServeLock SUCCEEDS and two mergers sweep the same
// branches (realized: an `--once` pass ran concurrently with the daemon). The
// lock-table lookup in mergerLockProbe reports "not running" throughout,
// because it joins on the name's inode, and the holder's inode is unreachable
// by name.
//
// The predicate is deliberately the KERNEL'S OWN MARKER, not a path-string
// comparison: a descriptor whose target was unlinked reads back as
// "<path> (deleted)", and only an unlinked-underneath holder can present a
// deleted descriptor on this path. An inherited-fd holder (the merger's
// daemon child) symlinks to the path itself — no marker — so healthy
// deployments are never misreported. Unreadable /proc entries are skipped: a
// scan that cannot prove the orphan state must not invent it (Rule 15).
func OrphanLockHolder(lockPath string) int {
	prefix := lockPath + procDeletedSuffix
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	for _, pid := range pids {
		if holdsDeletedLockOn(pid, prefix) {
			return pid
		}
	}
	return 0
}

// holdsDeletedLockOn reports whether pid has a descriptor symlink reading
// "<prefix>" aka an unlinked copy of the lock path.
func holdsDeletedLockOn(pid int, deletedTarget string) bool {
	fdDir := filepath.Join(procRoot, strconv.Itoa(pid), procFdDirName)
	links, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}
	for _, l := range links {
		target, err := os.Readlink(filepath.Join(fdDir, l.Name()))
		if err != nil {
			continue
		}
		if target == deletedTarget {
			return true
		}
	}
	return false
}

// SortedCandidates returns a copy sorted by name (stable render order for
// tests and operators alike).
func SortedCandidates(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
