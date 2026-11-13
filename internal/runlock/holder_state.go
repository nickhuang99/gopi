package runlock

// THE LOCK FILE'S TWO FACTS, READ APART (bug epic 01a1ac0f, 2026-11-05).
//
// A run directory's `run.lock` carries TWO different things, and until this
// file existed nothing in the tree could tell them apart:
//
//	1. the STAMP — a few `key=value` lines the last holder wrote (pid,
//	   startticks, acquired_at, host). It is DIAGNOSIS: it exists so a refusal
//	   can name a process an operator can go and look at. It is written once per
//	   acquisition and NEVER removed, because the file itself is never deleted
//	   (rule 3 in the package comment). So a stamp SURVIVES its writer, and
//	   "the file names a pid" says nothing whatever about whether that pid is
//	   alive, let alone whether it is working.
//	2. the FLOCK — the kernel's own record of who is executing this directory.
//	 It cannot outlive its holder: the kernel drops it on exit, panic or
//	 `kill -9`, which is the whole reason the lock was built on it rather than
//	 on the ledger.
//
// The measured confusion this file ends (operator, 2026-11-05 22:12): a run
// directory's stamp named pid 2662735, a process that had exited seconds
// earlier; reading the stamp as "the run is in progress" made a dead holder
// look alive. The SAME request minutes later named a live pid. One path, two
// opposite readings, because the stamp was being read as the verdict.
//
// LiveHolderOf below answers the FLOCK question — held, by whom — and it never
// takes the lock to do so: taking it, even briefly, would refuse a real runner
// arriving in that instant, i.e. an inspection verb would perturb what it
// inspects. /proc/locks is the kernel's own ledger of every advisory lock in
// force, so reading it is a MEASUREMENT of the lock's state rather than an
// opinion about it.
//
// WHY IT IS HERE and not in internal/tools: the address of a run directory's
// lock has ONE definition (PathIn, LockFileName) and a second package spelling
// `filepath.Join(dir, "run.lock")` to go and check it would be a second
// definition of the thing it is checking (Rules 13/14). This is a LEAF
// package, so every caller that can name a run directory can ask.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The kernel's lock ledger, and the field layout of one line of it. A line is
//
//	id: FLOCK  ADVISORY  WRITE 12345 103:04:5653565 0 EOF
//
// — so the kind is field 1, the holder pid field 4 and the (major:minor:inode)
// identity of the locked file field 5. Named rather than indexed inline (Rule
// 1): the format is the kernel's and it is the only place these numbers are
// read.
const (
	procLocksName = "locks"

	locksFieldKind   = 1
	locksFieldPID    = 4
	locksFieldFileID = 5
	locksFieldCount  = 6

	// lockKindFlock is the /proc/locks spelling of the kind this package takes
	// (POSIX record locks are a different mechanism and never satisfy a caller
	// guarding a run directory).
	lockKindFlock = "FLOCK"

	// locksIDPartSep separates the three parts of a locked file's identity.
	locksIDPartSep = ":"
	// locksFileIDParts is how many parts that identity has.
	locksFileIDParts = 3
)

// Device-number decomposition, in the encoding /proc/locks PRINTS and glibc
// defines: the major is bits 8-19 and the minor is bits 0-7 plus bits 20-31.
// Named because a wrong split produces a key that silently matches nothing —
// which would read as "nobody holds the lock", the one wrong answer that
// matters (Rule 15).
const (
	devMajorShift     = 8
	devMajorMask      = 0xfff
	devMinorLowMask   = 0xff
	devMinorHighShift = 12
	devMinorHighMask  = 0xfff00
)

// The three states a run directory's lock can be in, as ONE vocabulary: the
// verb that renders this must never invent a fourth word, and "not measured"
// must never be spelled as "free" (Rule 15b).
const (
	// LockStateFree: the kernel holds no flock on this file. Any stamp inside
	// it is a previous holder's diagnosis.
	LockStateFree = "free"
	// LockStateHeld: a process is executing the directory right now.
	LockStateHeld = "held"
	// LockStateUnmeasured: the question could not be answered (an unreadable
	// /proc/locks, an unresolvable path). Never a synonym for free.
	LockStateUnmeasured = "unmeasured"
)

// LiveHolder is the answer LiveHolderOf gives: the lock's measured state, the
// pid holding it when it is held, and the sentence a reader needs in order to
// know WHICH fact they are looking at.
type LiveHolder struct {
	// Path is the lock file this reading is about.
	Path string
	// State is one of LockStateFree / LockStateHeld / LockStateUnmeasured.
	State string
	// Pid is the process holding the flock (0 unless State is LockStateHeld).
	Pid int
	// Why is the evidence sentence — it names what was read, or what could not
	// be (Rule 15: a measurement states itself).
	Why string
}

// Held reports whether the kernel currently has the directory locked.
func (h LiveHolder) Held() bool { return h.State == LockStateHeld }

// LiveHolderOf measures whether path's inode currently carries a kernel flock
// and which process holds it.
//
// It reads /proc/locks — the kernel's own list of advisory locks in force —
// rather than attempting the lock: a probe that TAKES the lock refuses a real
// runner for the duration of the probe, so an inspection would perturb what it
// inspects. A lock file that does not exist holds no lock (no inode exists to
// carry one), and that is stated as its own reason rather than as a silent
// "free".
//
// An unreadable /proc/locks, or a path whose device/inode cannot be read, is
// LockStateUnmeasured with the failure named — never a fabricated "free".
func LiveHolderOf(path string) LiveHolder {
	h := LiveHolder{Path: path}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			h.State = LockStateFree
			h.Why = "no " + LockFileName + " exists at that path, so no process can hold it"
			return h
		}
		h.State = LockStateUnmeasured
		h.Why = fmt.Sprintf("the lock file could not be read (%v)", err)
		return h
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		h.State = LockStateUnmeasured
		h.Why = "the lock file's device/inode is unavailable on this platform, so its flock cannot be looked up"
		return h
	}
	key := lockFileIDKey(st.Dev, st.Ino)
	pid, found, lerr := locksHolding(filepath.Join(procRootPath, procLocksName), key)
	if lerr != nil {
		h.State = LockStateUnmeasured
		h.Why = fmt.Sprintf("the kernel's lock ledger could not be read (%v)", lerr)
		return h
	}
	if !found {
		h.State = LockStateFree
		h.Why = "the kernel holds no flock on it"
		return h
	}
	h.State = LockStateHeld
	h.Pid = pid
	h.Why = fmt.Sprintf("the kernel reports FLOCK held by pid %d", pid)
	return h
}

// StampOf returns the stamp the last holder of path wrote, rendered for a
// human, or "" when the file is absent or carries no stamp.
//
// IT IS DIAGNOSIS, NOT A VERDICT, and the caller must label it as such (see
// LiveHolderOf's doc): the file is never deleted, so a stamp outlives the
// process that wrote it, and its pid may name anything at all by the time it is
// read — a recycled pid, a dead one, or a process doing entirely unrelated work.
// That is precisely why it is read through the SAME parser the refusal text uses
// (readHolder) and reported BESIDE the flock rather than instead of it.
func StampOf(path string) string {
	h, err := readHolder(path)
	if err != nil {
		// An unreadable stamp is not a stamp: the caller already renders the
		// lock's STATE from LiveHolderOf, which names the read failure there.
		return ""
	}
	return h.String()
}

// lockFileIDKey renders the (major:minor:inode) key /proc/locks prints for a
// locked file, from the device number and inode os.Stat reported.
//
// BOTH PARTS ARE PADDED TO AT LEAST TWO HEX DIGITS, and that is measured rather
// than assumed: the kernel prints the minor as `%02x` and the major likewise,
// so a tmpfs file reads `00:1b` (dev 27) while a block-device file reads
// `103:04`. Formatting the major with a bare `%x` yields `0:1b`, which matches
// no line — and a key that matches nothing is indistinguishable from "nobody
// holds this lock", i.e. exactly the fabricated `free` Rule 15b forbids. This
// was found by driving the reader against a real flock on /dev/shm.
func lockFileIDKey(dev uint64, ino uint64) string {
	major := (dev >> devMajorShift) & devMajorMask
	minor := (dev & devMinorLowMask) | ((dev >> devMinorHighShift) & devMinorHighMask)
	return fmt.Sprintf("%02x:%02x:%d", major, minor, ino)
}

// locksHolding scans one /proc/locks document for a FLOCK on the file
// identified by key. It is the pure half of LiveHolderOf, so a fixture can pin
// the parse without a live kernel lock.
func locksHolding(locksPath, key string) (pid int, found bool, err error) {
	raw, err := os.ReadFile(locksPath)
	if err != nil {
		return 0, false, err
	}
	return locksHoldingIn(string(raw), key)
}

// locksHoldingIn is locksHolding's parser: one FLOCK line per lock in force.
//
// A line that does not parse is SKIPPED, not fatal: the ledger carries every
// lock on the machine (POSIX record locks, other users' files, malformed
// rows on an unusual kernel) and none of them is this file's, so refusing the
// whole reading because one row is odd would turn "not mine" into "unmeasured".
// What MUST NOT be skipped silently is the whole file being unreadable — that
// is the os.ReadFile error above.
func locksHoldingIn(doc, key string) (int, bool, error) {
	for _, line := range strings.Split(doc, "\n") {
		fields := strings.Fields(line)
		if len(fields) < locksFieldCount {
			continue
		}
		if fields[locksFieldKind] != lockKindFlock {
			continue
		}
		if fields[locksFieldFileID] != key {
			continue
		}
		pid, perr := strconv.Atoi(fields[locksFieldPID])
		if perr != nil || pid <= 0 {
			continue
		}
		return pid, true, nil
	}
	return 0, false, nil
}
