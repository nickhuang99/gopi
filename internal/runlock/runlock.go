// Package runlock is the ONE definition of "this run directory is being
// executed" — a KERNEL file lock on <run-dir>/run.lock, taken before anything
// else that run does.
//
// WHY IT EXISTS. Mutual exclusion between two processes that want to execute
// the SAME run directory used to be carried by the ledger's status column (a
// claimed -> in_progress write plus a compare-and-set). That column cannot do
// the job, and the measured failure has three shapes: the status is written
// BEFORE the child is spawned (so a crash in between leaves a row that says
// "running" and runs nothing), the status is never cleared when the holder
// dies (a row stayed in_progress for 8h38m after its process was gone), and
// clearing it requires ANOTHER process to sweep — a cleaner, a resident
// mechanism, and a second thing that can be wrong.
//
// A kernel lock has none of the three. The kernel owns the lock; the process
// that took it cannot forget to have it, and when that process ends — a clean
// exit, a panic, a `kill -9` — the kernel releases it immediately. There is no
// zombie lock and there is NO cleaner to write.
//
// THE THREE RULES THIS PACKAGE EXISTS TO KEEP (each one is a real defect with
// a real cost, not a preference):
//
//  1. The VERDICT is the flock alone. The file's contents are for a human
//     reading a refusal, never for the decision: "read the pid and see if it
//     is alive" re-implements the liveness guess the ledger already does
//     badly, and inherits every way a pid can lie (recycled, other host,
//     unreadable). LOCK_EX|LOCK_NB failing IS the answer.
//  2. No O_EXCL. Creating the file exclusively looks like a lock and becomes a
//     PERMANENT one the moment the holder is killed — precisely the zombie the
//     lock exists to make impossible.
//  3. The lock FILE is never deleted. flock's subject is the inode, so
//     unlinking it races: a third process can create a NEW inode at the same
//     path and lock that one, and two processes then both "hold" the same
//     directory. The file stays; its CONTENT is overwritten by each holder.
//
// The SCOPE is one run directory, not a machine-wide singleton: two different
// runs execute at the same time and must never block each other. The lock
// lives IN the directory it guards, so the directory's own name is the whole
// address — no table, no registry, nothing that goes stale.
//
// It is a LEAF package: the standard library plus syscall.Flock, so any layer
// may depend on it.
package runlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

// LockFileName is the lock file's name INSIDE a run directory (Rule 1: one
// named constant; a caller that spells the name itself is a second definition
// of the address the lock is taken on).
const LockFileName = "run.lock"

// LockFileMode / runDirMode are the permissions this package creates: the lock
// carries only a pid, a start-ticks stamp, a timestamp and a hostname (none of
// it secret), and the run directory is the ordinary sessions-tree directory.
const (
	LockFileMode = 0o600
	runDirMode   = 0o755
)

// The holder stamp's field names. They are written for a HUMAN reading a
// refusal and are parsed back only to fill in the refusal's text — nothing
// about the decision depends on them (rule 1 in the package comment).
const (
	stampKeyPID        = "pid"
	stampKeyStartTicks = "startticks"
	stampKeyAcquiredAt = "acquired_at"
	stampKeyHost       = "host"
	stampSeparator     = "="
	stampLineEnd       = "\n"
)

// stampTimeLayout is the timestamp's spelling: RFC3339 in UTC, so a refusal
// reads as an unambiguous instant rather than a local time of unknown zone.
const stampTimeLayout = time.RFC3339

// procRootPath is where the kernel exposes a process's stat file — read only to
// stamp the holder's start ticks (procname.StartTicks), never to decide
// anything.
const procRootPath = "/proc"

// ErrHeld is the sentinel every "someone else is executing this directory"
// refusal wraps, so a caller can test with errors.Is without matching text.
var ErrHeld = errors.New("run directory is already locked by another process")

// Holder is the content of a lock file: what the process that WON the lock
// wrote into it. It is DIAGNOSTIC — a refusal renders it so an operator can
// find the process — and it is never evidence that the holder is alive.
type Holder struct {
	PID        int
	StartTicks string
	AcquiredAt string
	Host       string
}

// String renders the holder for a human. An unreadable field renders as the
// empty string it is; the caller (HeldError.Error) names that as "not
// recorded" rather than inventing a value (Rule 15).
func (h Holder) String() string {
	if h.PID == 0 && h.StartTicks == "" && h.AcquiredAt == "" {
		return ""
	}
	parts := make([]string, 0, 4)
	if h.PID != 0 {
		parts = append(parts, fmt.Sprintf("%s %d", stampKeyPID, h.PID))
	}
	if h.StartTicks != "" {
		parts = append(parts, fmt.Sprintf("%s %s", stampKeyStartTicks, h.StartTicks))
	}
	if h.AcquiredAt != "" {
		parts = append(parts, fmt.Sprintf("%s %s", stampKeyAcquiredAt, h.AcquiredAt))
	}
	if h.Host != "" {
		parts = append(parts, fmt.Sprintf("%s %s", stampKeyHost, h.Host))
	}
	return strings.Join(parts, ", ")
}

// HeldError is the refusal a second process gets. It NAMES the lock path and
// whatever the holder recorded, because the whole point of a refusal is that
// the operator can act on it without a second investigation.
type HeldError struct {
	// Path is the absolute path of the lock file that could not be taken.
	Path string
	// Holder is what the holder stamped into the file (zero when unreadable).
	Holder Holder
	// ReadErr is why the holder stamp could not be read ("" when it was).
	ReadErr error
}

// Error renders the refusal. When the holder stamp is missing it says so
// instead of printing an empty pid — a refusal that names no owner must be
// distinguishable from one whose owner was read (Rule 15).
func (e *HeldError) Error() string {
	stamp := e.Holder.String()
	if stamp == "" {
		why := "the file carries no holder stamp yet"
		if e.ReadErr != nil {
			why = e.ReadErr.Error()
		}
		return fmt.Sprintf("%s: %q (%s)", ErrHeld.Error(), e.Path, why)
	}
	return fmt.Sprintf("%s: %q (held by %s)", ErrHeld.Error(), e.Path, stamp)
}

// Unwrap makes errors.Is(err, ErrHeld) true for every refusal.
func (e *HeldError) Unwrap() error { return ErrHeld }

// Handle is a HELD lock. The open descriptor it carries IS the lock: releasing
// it (Release, or the process ending) is what frees the directory. A Handle
// must be released by the process that took it, and must not be copied.
type Handle struct {
	dir  string
	path string
	f    *os.File

	// mu guards released so Release is idempotent — a deferred release beside
	// an explicit one is ordinary, and a second flock on a closed descriptor
	// would return EBADF and be mistaken for a lock failure.
	mu       sync.Mutex
	released bool
}

// Path is the lock file's absolute path (what a refusal names, and what an
// operator greps).
func (h *Handle) Path() string { return h.path }

// Dir is the run directory this lock guards.
func (h *Handle) Dir() string { return h.dir }

// PathIn is the lock file's path inside dir — the ONE spelling of the address,
// useful to a caller that must render it before it tries to take the lock.
func PathIn(dir string) string {
	return filepath.Join(strings.TrimSpace(dir), LockFileName)
}

// Lock takes the exclusive, non-blocking kernel lock on dir/run.lock and
// returns the handle that KEEPS it held.
//
// A returned error is either the *HeldError refusal (another live process holds
// this run directory) or an I/O failure naming what could not be done. There is
// no third outcome and no partial state: on any error the descriptor is closed,
// so a failed attempt leaves no lock behind and nothing to clean up.
//
// On SUCCESS the holder stamp (pid, startticks, acquired_at, host) is written
// into the file for a human reader. A stamp that cannot be written releases the
// lock and returns the error: the lock exists so a refusal can name its holder,
// and one that cannot say who holds it is not usable for the diagnosis it is
// for (Rule 15 — loud, and no half state).
func Lock(dir string) (*Handle, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("runlock: empty run directory — there is no directory to take %s in", LockFileName)
	}
	// The run directory must exist for its lock to live in it. Creating it is
	// not a side effect a refused run leaves behind: a directory nobody has
	// created yet has no holder either (the first holder is the process that
	// creates it), so this MkdirAll and a refusal cannot co-occur.
	if err := os.MkdirAll(dir, runDirMode); err != nil {
		return nil, fmt.Errorf("runlock: create run directory %q: %w", dir, err)
	}
	path := PathIn(dir)
	// NOT O_EXCL (rule 2 in the package comment): the file is shared and
	// reused by every holder of this directory; only the flock decides.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, LockFileMode)
	if err != nil {
		return nil, fmt.Errorf("runlock: open lock file %q: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// THE verdict: the kernel refused. Everything below is diagnosis for
		// the refusal's text, never a second opinion about the verdict.
		holder, readErr := readHolder(path)
		// The verdict is already decided (the kernel refused); a close error
		// here would be a second, unknown failure wearing one message. The
		// refusal names the flock, and closing an unheld descriptor cannot
		// leave a lock behind.
		_ = f.Close() //stylegate:ignore the refusal is the result; closing a refused descriptor cannot fail meaningfully
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, &HeldError{Path: path, Holder: holder, ReadErr: readErr}
		}
		return nil, fmt.Errorf("runlock: flock %q: %w", path, err)
	}
	h := &Handle{dir: dir, path: path, f: f}
	if werr := h.writeStamp(); werr != nil {
		// Release's own error cannot change the answer (the stamp failure IS
		// the result), and the descriptor's close frees the lock regardless.
		_ = h.Release() //stylegate:ignore the stamp failure is the result; the release runs only to free the descriptor
		return nil, werr
	}
	return h, nil
}

// Release frees the lock. It is idempotent and safe on a nil handle, so a
// caller may both defer it and call it explicitly.
func (h *Handle) Release() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released || h.f == nil {
		return nil
	}
	h.released = true
	// The close alone would drop the lock; the explicit unlock is the tidy
	// path and its failure is reported (never swallowed: Rule 15) while the
	// close still runs.
	unlockErr := syscall.Flock(int(h.f.Fd()), syscall.LOCK_UN)
	closeErr := h.f.Close()
	h.f = nil
	if unlockErr != nil {
		return fmt.Errorf("runlock: unlock %q: %w", h.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("runlock: close %q: %w", h.path, closeErr)
	}
	return nil
}

// writeStamp records who holds the lock. Best-effort as EVIDENCE, mandatory as
// a result: an error means the holder could not be recorded and Lock refuses.
func (h *Handle) writeStamp() error {
	// An empty host is recorded as EMPTY, not invented (Rule 15): the stamp is
	// diagnosis for a refusal, and a made-up host is worse than a missing one.
	host, _ := os.Hostname() //stylegate:ignore an unreadable hostname leaves the field empty by design
	ticks, _ := procname.StartTicks(procRootPath, strconv.Itoa(os.Getpid()))
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s%s%d%s", stampKeyPID, stampSeparator, os.Getpid(), stampLineEnd)
	fmt.Fprintf(&sb, "%s%s%s%s", stampKeyStartTicks, stampSeparator, ticks, stampLineEnd)
	fmt.Fprintf(&sb, "%s%s%s%s", stampKeyAcquiredAt, stampSeparator, time.Now().UTC().Format(stampTimeLayout), stampLineEnd)
	fmt.Fprintf(&sb, "%s%s%s%s", stampKeyHost, stampSeparator, host, stampLineEnd)
	if err := h.f.Truncate(0); err != nil {
		return fmt.Errorf("runlock: truncate lock file %q: %w", h.path, err)
	}
	if _, err := h.f.Seek(0, 0); err != nil {
		return fmt.Errorf("runlock: seek lock file %q: %w", h.path, err)
	}
	if _, err := h.f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("runlock: write holder stamp into %q: %w", h.path, err)
	}
	return nil
}

// readHolder parses the stamp a previous holder wrote. (zero, err) when the
// file cannot be read; (zero, nil) when it is readable but carries no stamp —
// both render as "the holder is unknown", which is the truth in both cases.
func readHolder(path string) (Holder, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Holder{}, fmt.Errorf("holder stamp unreadable: %w", err)
	}
	return parseHolder(string(raw)), nil
}

// parseHolder is the pure parser over a stamp's text — one shape, one reader,
// so a test can drive it without a file.
func parseHolder(text string) Holder {
	var h Holder
	for _, line := range strings.Split(text, stampLineEnd) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), stampSeparator)
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case stampKeyPID:
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				h.PID = n
			}
		case stampKeyStartTicks:
			h.StartTicks = strings.TrimSpace(value)
		case stampKeyAcquiredAt:
			h.AcquiredAt = strings.TrimSpace(value)
		case stampKeyHost:
			h.Host = strings.TrimSpace(value)
		}
	}
	return h
}
