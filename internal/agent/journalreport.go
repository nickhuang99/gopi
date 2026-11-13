package agent

// journalreport.go — the READ FACE of the report-contract ownership guard
// (epic nested-report-path-clobber, 2026-09-15).
//
// taskreport.PathEnv names the destination of ONE session's task report. The
// variable is exported on the process that run owns — by `gopi task run`'s
// spawn seam, or by whatever harness started the session — and is inherited by
// everything below it, INCLUDING a gopi launched from that session's bash tool
// (a self-probe, a nested `go test`, a runner child). Such a child used to read
// the PARENT's path as its own destination and write its report over the
// parent's task-report.md. Measured: session 01a0a4ee's deliverable was
// replaced by a child probe's "ls" report. The child did nothing wrong — the
// variable carried no owner.
//
// The guard below is the face that does not depend on WHO spawned the child
// (the spawn-side strip covers this tree's own spawn helpers; a third-party
// launcher — a script, a test harness, an operator — hands the environment over
// untouched). It runs at the one moment the answer becomes knowable: when the
// session's own directory exists.
//
// THE OWNERSHIP RULE — and its hard lesson (measured, this task's own session):
// a session's report lives INSIDE that session's own directory, so the honest
// question is not "which task id am I" but "does the destination live in MY
// directory". The run-id test alone was WRONG: the harness injects
// GOPI_TASK_REPORT_PATH with NO owner marker and the session's own directory
// (<sessions>/<session-uuid>/task-report.md) is neither the run id nor the task
// id, so a run-id-only rule refused the harness hand-off — it would have broken
// the very contract it protects. The directory test is authoritative for BOTH
// exporters: `task run` (sessions/<task id>/task-report.md) and the harness
// (sessions/<session uuid>/task-report.md).
//
// Both halves are kept, in this order:
//
//	1. the DIRECTORY   the report's parent must BE this session's directory —
//	                   the general rule, covering every exporter;
//	2. the OWNER mark  beyond that, a marker that names a DIFFERENT session
//	                   refuses the contract (the inherited-marker case, which
//	                   the directory test alone cannot see when two sessions
//	                   share a directory).
//
// An empty marker is not a claim of ownership; it is simply the marker-less
// hand-off (the harness shape), and the directory decides. A refusal is
// RECORDED and returned (Rule 15): the caller prints it, so an operator can
// tell "this session inherited and refused somebody else's report path" from
// "there was nothing to refuse".
//
// THE SECOND QUESTION — USABILITY (measured 2026-10-28, session
// 01a18310-f082-702e-8d06-7d40bf6444da). Ownership answers "is this destination
// MINE"; it says nothing about "can it be USED". A launcher inherited from
// another host/channel exported GOPI_TASK_REPORT_PATH pointing at
// ~/.gopi/sessions/<32-hex dashless>/task-report.md whose PARENT DIRECTORY DID
// NOT EXIST: ownership passed (it was not another live session's directory, so
// nothing disowned it), the path entered the run's context as a FACT, and the
// contract's own write (`cat > "$GOPI_TASK_REPORT_PATH"`) died on ENOENT — the
// run's only report went nowhere while the string looked perfectly legitimate.
//
// So the guard now asks BOTH questions, and a destination must satisfy both:
//
//	1. OWNERSHIP  taskreport.ProcessOwnsContract — the destination is this
//	              session's (the rule above);
//	2. USABILITY  reportDestinationUsable — the destination's PARENT directory
//	              exists and this process may create a file in it.
//
// A destination that fails either is DROPPED and REPORTED BY NAME (path +
// the failed criterion + which variables were discarded), because a path that
// cannot be written is not a fact about this run.
//
// WHY THE GUARD DOES NOT MkdirAll THE PARENT (the tempting "fix"): the parent
// is derived from the path, and the path is exactly what is suspect. Creating
// it would MINT the misspelling — the dashless 32-hex directory the launcher
// invented becomes a real second sessions container, and the run's only report
// lands in it where nothing (no `task show`, no seal, no operator glob) looks.
// Rule 34's ban on inventing containers applies to a container nobody
// configured. The export side (tools.TaskReportDestination) legitimately DOES
// create its parent: there the path comes from a resolution this tree owns. The
// read face only decides whether what it was handed may be trusted.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nick/gopi/internal/taskreport"
)

// reportDestinationUsable reports whether path can be used as a report
// destination by THIS process, and names the reason when it cannot.
//
// The two criteria are the ones a write actually needs, in the order a write
// meets them:
//
//	parent exists  — filepath.Dir(path) is a directory (a missing or
//	                 file-shaped parent is the observed ENOENT);
//	writable       — syscall.Access(W_OK) on that directory. First, that is what
//	                 the kernel will decide at write time (access(2) answers for
//	                 a DIRECTORY, which is why an O_WRONLY directory open — the
//	                 probe shape this replaced elsewhere — reported EISDIR on
//	                 every host and was rejected as a permission probe).
//
// Second, and this is why the READ face uses access(2) rather than the write
// probe its exporter sibling owns: a probe would CREATE a file in a directory
// this process has just decided it may not be allowed to touch. `gopi chat`,
// `gopi --help` and a nested probe all run this at startup; leaving probe files
// (or creating files at all) in other people's session containers is precisely
// the class of litter this guard refuses.
//
// An empty path names no destination: the empty-value case is reported by the
// callers as its own reason (an empty value is a broken contract, not an
// unusable directory).
func reportDestinationUsable(path string) error {
	parent := filepath.Dir(path)
	fi, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("its parent directory %s does not exist (or cannot be examined): %w", parent, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("its parent %s is not a directory (mode %s)", parent, fi.Mode())
	}
	if err := syscall.Access(parent, accessWrite); err != nil {
		return fmt.Errorf("its parent directory %s is not writable by this process: %w", parent, err)
	}
	return nil
}

// accessWrite is the access(2) MAY_WRITE bit. The stdlib syscall package
// exposes Access() but names no W_OK constant, so the mode word is named here
// (Rule 1/13): it is a permission bit of the C library's ABI, not a tunable.
const accessWrite = 2

// SanitizeReportContract enforces the report destination's TWO preconditions —
// OWNERSHIP and USABILITY — for a session whose artifacts live in sessionDir:
// when the environment carries a task-report contract (taskreport.PathEnv /
// taskreport.OwnEnv) that is not this session's own, OR that names a
// destination this process cannot use, both names are REMOVED, the removed
// names returned, and the refusal handed to refuse (which the caller prints).
//
// refuse is the report channel, not decoration: an operator must be able to
// tell WHICH path was dropped and WHY (Rule 15 — a silent refusal is
// indistinguishable from nothing having happened). A nil sink is not an excuse
// for silence; the refusal then goes to stderr in the same shape.
//
// sessionDir == "" is the one unsafe-looking input, and it resolves the SAFE
// way: with no directory of its own a process can prove nothing about the
// destination, so the contract is refused rather than trusted (the same
// direction the shell sanitizer takes, one predicate). Production never passes
// "": the caller runs at session start, where the directory is the fact that
// just became true.
func SanitizeReportContract(sessionDir string, refuse func(reason string)) (removed []string) {
	env := os.Environ()
	if !taskreport.ContractPresent(env) {
		return nil // nothing inherited: nothing to disown
	}
	path := strings.TrimSpace(taskreport.EnvValue(env, taskreport.PathEnv))

	// Which preconditions failed — BOTH are evaluated even when the first
	// already refuses, because an operator fixing a broken hand-off needs the
	// whole picture (the measured case failed on both: another channel's
	// dashless directory, and that directory did not exist). The decisions
	// themselves are the leaf package's (Rule 14) plus this file's usability
	// predicate; only the WORDING lives here.
	var failures []string
	switch {
	case path == "":
		failures = append(failures, "it names no destination (empty value)")
	default:
		if !taskreport.ProcessOwnsContract(sessionDir) {
			failures = append(failures, fmt.Sprintf("it does not live in this session's directory %s, so it is another session's report", sessionDirOrNone(sessionDir)))
		}
		if err := reportDestinationUsable(path); err != nil {
			failures = append(failures, fmt.Sprintf("it cannot be used: %v", err))
		}
	}
	if len(failures) == 0 {
		return nil // ours AND usable: the contract stays, as a fact
	}
	reason := fmt.Sprintf("%s names %s — %s", taskreport.PathEnv, path, strings.Join(failures, "; "))

	// Refused. Both names go together: a path without its owner is exactly the
	// ambiguous state this epic removes, and leaving the marker behind (even
	// valueless) would let a later spawn think the contract is present.
	for _, name := range []string{taskreport.PathEnv, taskreport.OwnEnv} {
		if _, present := os.LookupEnv(name); !present {
			continue
		}
		_ = os.Unsetenv(name) //stylegate:ignore env scrub of an INHERITED contract; the failure is recorded in the returned record and rendered by the caller
		removed = append(removed, name)
	}
	reportRefusal(refuse, reason, removed)
	return removed
}

// sessionDirOrNone names the session directory in a refusal even when the
// process has none yet (an empty string in the middle of a sentence reads as a
// typo, not as "this process owns nothing").
func sessionDirOrNone(sessionDir string) string {
	if strings.TrimSpace(sessionDir) == "" {
		return "(no directory of its own)"
	}
	return sessionDir
}

// reportRefusal delivers the refusal to the caller's sink; a nil sink is not
// permission to stay silent (Rule 15).
func reportRefusal(refuse func(reason string), reason string, removed []string) {
	line := fmt.Sprintf("refused an inherited task-report contract (%v): %s — it will not be written here", removed, reason)
	if refuse == nil {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", line)
		return
	}
	refuse(line)
}
