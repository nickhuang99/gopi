// release.go — the ONE instrument that answers "is this NUMBER still the one I
// claimed, before I close it?"
//
// THE DEFECT IT EXISTS FOR, stated as the window rather than as an actor:
//
//  1. a fixture (or any caller) takes a raw descriptor number — syscall.Dup, or
//     F_DUPFD when it needs the number at or above a floor;
//  2. the number STOPS belonging to that caller: either the caller handed the
//     wrapper to a production path whose job is to close it, or a production
//     path already closed it, or the caller released it itself;
//  3. a LATE syscall.Close(number) then arrives.
//
// The kernel has already recycled the number in step 2's window — any open/dup/
// pipe between then and step 3 receives it — so the late close releases a
// STRANGER's live descriptor. And it reports success: syscall.Close returns nil
// for a number that is open, whoever holds it. The site therefore cannot tell a
// correct close from a destructive one, which is why chasing "who closed it"
// never converges and why the judgement has to move to the close itself.
//
// THE JUDGEMENT: before every raw close, ask the KERNEL what the number now
// denotes and compare it with what it denoted when the caller CLAIMED it. Equal
// → the number is still the caller's, close it. Not equal, or gone → DO NOT
// CLOSE, and say which number, what was recorded, and what was observed.
//
// Two residual facts this instrument does NOT hide (Rule 15 — a limit that is
// not stated reads as a guarantee):
//
//   - the identity is the kernel's reading of the number (readlink target plus
//     fstat dev/ino). Two DIFFERENT open file descriptions of the SAME file or
//     pipe inode are indistinguishable to it, because Linux exposes no id for an
//     open file description. For the defect above that is sufficient and it is
//     what makes the guard cheap: the recycle window hands the number to a
//     DIFFERENT object, and a different object always resolves differently.
//   - a number claimed and released inside one uninterrupted call has no window
//     at all; the instrument is for numbers that CROSS a boundary.
//
// This file stays in internal/borrowedfd on purpose (Rule 14/1): the package is
// the leaf every borrower already imports, so the retention idiom, its
// measuring window (settle.go) and this judgement have exactly ONE definition
// instead of a copy per package.
package borrowedfd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	// procSelfFD is procfs's view of THIS process's descriptors (Rule 1b).
	procSelfFD = "/proc/self/fd"
)

// Identity is what a descriptor NUMBER denoted at one moment: the kernel's own
// reading of it, recorded so a later release can compare instead of assuming.
//
// The zero value is deliberately UNUSABLE rather than "matches everything": a
// number of -1 resolves nothing, so an Identity that was never taken cannot
// authorise a close. That asymmetry is the whole safety property — a zero value
// that compared equal would make every forgotten Claim a licence to close.
type Identity struct {
	// taken is the zero value's own refusal switch. WITHOUT IT the zero struct's
	// number is 0 — a REAL number (a printable fd) — so `number < 0` never fires
	// and a forgotten claim would be compared against /dev/null and reported as a
	// MovedError instead of as "never taken". Measured, not imagined: this arm went
	// red naming fd 0 → "/dev/null" before the field existed.
	taken  bool
	number int
	target string
	dev    uint64
	ino    uint64
}

// IdentityOf takes the kernel's reading of n RIGHT NOW.
//
// It fails when n names nothing this process holds (a released or never-open
// number: readlink answers ENOENT) and when the number resolves but cannot be
// stat'ed — both are refusals, never a zero Identity, because a zero Identity
// would silently refuse every later release with no cause named.
func IdentityOf(n int) (Identity, error) {
	if n < 0 {
		return Identity{}, fmt.Errorf("descriptor number %d is not a number any process can hold", n)
	}
	path := filepath.Join(procSelfFD, strconv.Itoa(n))
	target, err := os.Readlink(path)
	if err != nil {
		return Identity{}, fmt.Errorf("descriptor %d names nothing this process holds: readlink %s: %w", n, path, err)
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(n, &st); err != nil {
		return Identity{}, fmt.Errorf("descriptor %d resolved to %q but the kernel would not stat it: %w", n, target, err)
	}
	return Identity{taken: true, number: n, target: target, dev: uint64(st.Dev), ino: uint64(st.Ino)}, nil
}

// Number is the descriptor number this identity was taken of (-1 when untaken).
func (i Identity) Number() int { return i.number }

// Target is the kernel's readlink reading, e.g. "/tmp/x" or "pipe:[12345]".
func (i Identity) Target() string { return i.target }

// String renders the identity for a refusal message: a bare number tells a
// reader nothing about WHICH object it had been.
func (i Identity) String() string {
	if !i.taken {
		return "no identity (never taken)"
	}
	return fmt.Sprintf("fd %d → %q (dev %d, ino %d)", i.number, i.target, i.dev, i.ino)
}

// Equal reports whether two readings denote the same object.
func (i Identity) Equal(other Identity) bool {
	if !i.taken || !other.taken {
		// An untaken identity is equal to nothing, including another untaken one.
		return false
	}
	return i.number == other.number && i.target == other.target && i.dev == other.dev && i.ino == other.ino
}

// MovedError is the refusal a release makes when the number is no longer the one
// that was claimed. It carries all three facts a reader needs (Rule 15): the
// NUMBER, the RECORDED identity, and what the kernel OBSERVED at release time.
//
// It is a distinct type rather than a formatted string so a caller can tell "I
// refused to close a stranger" from "the close itself failed" with errors.As,
// which is the difference between a correct decision and an I/O error.
type MovedError struct {
	Number   int
	Recorded Identity
	Observed string
	Detail   error
}

func (e *MovedError) Error() string {
	return fmt.Sprintf("refusing to close descriptor %d: it is no longer the descriptor this call claimed "+
		"(recorded: %s; observed now: %s)%s — a close here would release a number the kernel has recycled under "+
		"whoever opened it next, and syscall.Close reports success when it does",
		e.Number, e.Recorded.String(), e.Observed, detailOrEmpty(e.Detail))
}

// Unwrap exposes the kernel's own cause (usually ENOENT: the number is gone).
func (e *MovedError) Unwrap() error { return e.Detail }

func detailOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return ": " + err.Error()
}

// Check re-reads the number and compares it with what was recorded. It returns
// nil only when the number still denotes the same object, so every caller that
// treats nil as "safe to close" gets that conclusion from the kernel.
func (i Identity) Check() error {
	if !i.taken {
		return fmt.Errorf("refusing to close: this identity was never taken (Rule 15: an unrecorded claim cannot authorise a release)")
	}
	now, err := IdentityOf(i.number)
	if err != nil {
		// The number names nothing: it was closed (or never existed). Do NOT close
		// again — the second close is the one that lands on a recycled number.
		return &MovedError{Number: i.number, Recorded: i, Observed: "nothing (the number is closed)", Detail: err}
	}
	if !i.Equal(now) {
		return &MovedError{Number: i.number, Recorded: i, Observed: now.String()}
	}
	return nil
}

// Release closes the number ONLY when it still denotes what it denoted when the
// identity was taken, and reports the refusal (naming number, recorded and
// observed identity) instead of closing when it does not.
//
// This is the shape every raw close in a fixture should take: the caller states
// what it thought it held, and the kernel decides whether that is still true.
func (i Identity) Release() error {
	if err := i.Check(); err != nil {
		return err
	}
	if err := syscall.Close(i.number); err != nil {
		return fmt.Errorf("descriptor %d still carried the claimed identity (%s) but the close failed: %w", i.number, i.String(), err)
	}
	return nil
}

// Witness takes the kernel's reading of n as EVIDENCE, and never refuses.
//
// It exists for the one shape where a raw close is CORRECT: a fixture that
// deliberately releases a number to manufacture the released-stream shape DOES own
// the close — the release is the premise of the arm. What such an arm could not do
// before is say WHICH descriptor it released: syscall.Close returns the same nil
// for a number that was already gone and for one it really closed, so afterwards
// nothing distinguishes the two.
//
// The answer is usable in both directions and names its own failure: a non-nil
// error means the number did not resolve, and the caller reports it verbatim
// (a witness that silently returned a zero Identity would make every later
// comparison a refusal with no cause named). Call Check() after the close to prove
// the number really was released.
func Witness(n int) Identity {
	id, err := IdentityOf(n)
	if err != nil {
		return Identity{number: n, target: "unresolved: " + err.Error()}
	}
	return id
}
