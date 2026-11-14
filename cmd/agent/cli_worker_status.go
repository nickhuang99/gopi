package main

// cli_worker_status.go — the per-WORKER liveness enumeration behind
// `gopi worker --status` (epic worker-status-record, 2026-09-13).
//
// WHY IT EXISTS: cli_worker_control.go's status face reports the resident
// DAEMON — one lock probe plus "the live state files". That answers "is the
// service up" but not the operator's next question, which is a question about
// PROCESSES: which worker pids are running right now, what is each one doing,
// and is any of them a ghost. Three facts the daemon-shaped report could not
// give:
//
//   - a worker started by the OPERATOR'S LAUNCHER (scripts/gopi-worker-up.sh)
//     is a FOREGROUND `gopi worker`: it holds no lock and, before this epic, it
//     wrote no state file, so `--status` showed nothing while N worker
//     processes were visibly draining the queue;
//   - a worker killed the hard way (or a writer that never owned the record —
//     a test fixture, a crash, a foreign process) leaves its state file behind
//     with no cleanup — the file must be reported as STALE, never rendered as a
//     running worker, and its cause must never be GUESSED from the file (the
//     bytes cannot tell a SIGKILL from a crash from a test fixture: measured
//     2026-09-14, the operator was sent hunting a killer over 14 files a
//     cmd/agent fixture had written — devdoc/worker-phantom-selfspawn.md);
//   - a state file whose body is unreadable/undecodable must be COUNTED and
//     named, never silently dropped (Rule 15).
//
// THE ENUMERATION HAS TWO SOURCES, and the second one is why the reader lives
// here rather than in the worker's state writer:
//
//	① STATE FILES: <gopi dir>/worker/worker-state-<pid>, the record a worker
//	   writes at startup and at every claim boundary. Only files whose pid is a
//	   live, NON-ZOMBIE process (tools.PidAlive) are workers; a file for a dead
//	   pid is a STALE record (reported as such, and never counted as running).
//	② PROCESS TABLE (the fallback): a worker that never wrote a file — launched
//	   without a writable state directory, or by an older binary — is still a
//	   worker, and its /proc entry says so by ARGV SHAPE: comm == the gopi
//	   binary name AND argv[1] == the worker verb. That is exactly the predicate
//	   scripts/gopi-worker-up.sh's `--count` uses (internal/procname + the argv
//	   scan below), so the operator's count and `--status` cannot disagree about
//	   what a worker is. A `gopi task run <id>` child re-execs the SAME binary
//	   and is therefore excluded POSITIVELY (argv[1] is the task verb, not the
//	   worker verb) — a substring filter would count it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/tools"
)

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// workerStateFileSuffix is the tail of a state file's name — the "-<pid>"
	// half of workerStateFilePrefix's "worker-state-<pid>". Reader-SIDE only:
	// covered by stateFileCarriesPID below.
	workerStateFileSuffix = "-"
	// workerStatusStampLayout is the timestamp layout of the state record's
	// `updated` field. It is the layout the writer uses (workerStateUpdatedAt).
	// RFC3339-ish by design: a status line a human reads does not need
	// nanoseconds, and a longer layout is a second string to drift.
	workerStatusStampLayout = "2006-01-02T15:04:05-07:00"

	// workerStatusNoWorkers is the EMPTY-STATE line: an enumeration that found
	// nothing must SAY SO (an empty list is indistinguishable from a broken
	// scan, which is the silent failure every status surface must refuse).
	workerStatusNoWorkers = "no workers running"

	// workerStatusResidueFmt is the HEADER for state files that do NOT belong to
	// a live gopi process — the residue a writer left behind and the records a
	// RECYCLED pid left behind. They are NOT workers and are never rendered as
	// one: the count is exact and every sampled entry carries the reason it was
	// rejected. %s = program name, %d = count.
	workerStatusResidueFmt = "%s: %d stale state file record(s) found — NONE of these is a worker:"

	// workerStatusResidueMeaningA / workerStatusResidueMeaningB explain WHY the
	// reader is looking at residue files at all. The previous shape put this
	// enumeration on one comma-joined line, which read like a second worker LIST;
	// a reader who asked "is a worker running?" must be told in plain words that
	// these are corpses, not workers, where they came from and that removing them
	// is safe. Two lines, because ONE sentence long enough to say all three is a
	// paragraph on a status screen (each row is capped by workerStatusResidueMaxRowRunes).
	workerStatusResidueMeaningA = "  (what these are: state files of a worker pid that has EXITED — a SIGKILL, or a leaked test fixture, leaves one behind.)"
	workerStatusResidueMeaningB = "  (a \"recycled pid\" row means the number now names an UNRELATED process. Deleting these files is safe; the next worker start clears them.)"

	// workerStatusResidueMoreFmt states how many entries the sample omitted, so
	// the header's exact count and the printed rows cannot be read as
	// disagreeing. %d = how many entries were left out.
	workerStatusResidueMoreFmt = "  … and %d more (not listed — rerun with %s)"

	// workerStatusResidueRemedyFmt names the ACTION after the sample: the exact
	// command that deletes what the report just counted, and how many files that
	// is. A dead end ("you have 121 corpses") with no next step is what made
	// this output useless to the operator who asked a yes/no question.
	workerStatusResidueRemedyFmt = "  to clear them now: %s   (%d file(s))"

	// workerStatusResidueVerboseFlag is the opt-in second half of the status
	// face: without it an operator reads a COUNT plus a few named samples (the
	// measured host carried 121 entries, and 4KB of pid noise is not a status
	// report); with it the reader gets every residue line, one per file, for
	// when the count itself is the thing being debugged.
	//
	// TWO gates must know this word, and passing one does not pass the other
	// (both were measured wrong on the shipped binary 2026-09-14):
	//   - ROUTING (workerControlRequested → workerStatusRequested): the BARE
	//     alias must open the control route, or the block's own "rerun with …"
	//     advice is refused as an unknown worker argument (exit
	//     workerUnexpectedArgExitCode) and the full file list is unreachable;
	//   - ROLE (workerControlParse classifies it READ-ONLY beside
	//     controlActionRoleOf, the merger face's classifier): an unclassified
	//     alias makes the read+act refusal blind, so `--status-verbose --stop`
	//     prints the report and exits 0 with the act silently DROPPED — the very
	//     F1 defect class this refusal exists for (a monitor concludes the
	//     service was stopped; it was not).
	workerStatusResidueVerboseFlag = "--status-verbose"

	// workerStatusResidueSample is how many residue entries the default status
	// output names before it summarizes the rest.
	workerStatusResidueSample = 6

	// workerStatusResidueIndent prefixes every listing row, so the rows read as
	// children of the header that introduces them.
	workerStatusResidueIndent = "  "

	// workerStatusResidueMaxRowRunes is the readability cap on ONE rendered
	// residue row. The defect this block replaces put 121 entries on a single
	// 929-character line; a row that cannot fit a terminal row is the same defect
	// in a different direction, so the shape is pinned by a test, not by taste.
	workerStatusResidueMaxRowRunes = 200

	// workerStatusResidueRowFmt / workerStatusResidueRowVerboseFmt lay out ONE
	// residue row: the indent, the file's BASENAME, then the REASON (which is
	// what the operator acts on, and which already names the pid for three of
	// the five causes). The verbose form appends the FULL path — only the
	// verbose form, so six default rows do not repeat a 40-character prefix
	// that the dir line and the remedy command already name once.
	workerStatusResidueRowFmt        = "%s%s: %s"
	workerStatusResidueRowVerboseFmt = "%s%s: %s  (%s)"

	// workerStatusNoWorkersWhy / LockNoteFmt / StartNoteFmt answer the question
	// the bare "no workers running" line left open — WHY there is no worker, and
	// how one appears. Every half is MEASURED at the point of printing (the
	// process-table scan found nothing; the daemon lock names no holder), so
	// these are a report and not advice (Rule 15). Three lines, each prefixed
	// with what it answers, because one sentence carrying all three facts is a
	// paragraph on a status screen.
	//
	// %s = the program name (the note spells the two ways to start it).
	workerStatusNoWorkersWhy  = "  why: no detached `%s --daemon` holds the lock, and no `%s` process is in the process table"
	workerStatusNoWorkersLock = "  lock: %s"
	workerStatusNoWorkersGo   = "  start one: `%s --daemon` (detached) — or `%s` in the foreground to watch it run"
	// workerStatusNoWorkersLockNote explains what the lock line MEANS, because
	// "no daemon holds the lock" is not the same claim as "no worker exists": a
	// FOREGROUND worker takes no lock at all, so an empty lock is consistent with
	// a worker running (which is why the process-table half is read too).
	workerStatusNoWorkersLockNote = "   (a FOREGROUND worker takes no lock — the process-table half of this scan is what finds it)"

	// workerStatusStateFileGlob is the shell glob matching exactly the residue
	// files pruned below — the same workerStateFilePrefix the reader matches,
	// expanded by the shell rather than by a second pattern.
	workerStatusStateFileGlob = workerStateFilePrefix + "*"

	// workerStatusRemedyCommandFmt is the operator-facing remedy: delete exactly
	// the files this reader matched. %s = state dir, %s = the glob. It is built
	// from the dir the enumeration READ (never a re-derived default) and the
	// glob is built from the same workerStateFilePrefix the reader matches, so
	// the advice cannot name a different set of files than the report counted.
	workerStatusRemedyCommandFmt = "rm -f %s/%s"

	// residueReason* are the honest WHY of one residue entry: the fact that
	// separates "dead pid" (nothing cleaned up after the writer — and the file
	// alone cannot say WHICH of a hard kill, a crash or a foreign writer it
	// was: measured 2026-09-14, files in ~/.gopi/worker left by a test fixture
	// were reported as "SIGKILL residue", an unprovable claim that sent the
	// operator hunting a killer) from "recycled pid" (the file names a live
	// process that is NOT a worker) and "cannot attest" (procfs gave no
	// answer). Rendered verbatim — a reader must never have to guess which of
	// the three it is looking at.
	residueReasonDead       = "pid %d is gone — cleanup never ran (hard kill, crash, or a foreign writer; the file alone does not say which)"
	residueReasonNotGopi    = "pid %d is alive but does not run the %s program (image %q) — a RECYCLED pid, not a worker"
	residueReasonUnattested = "pid %d: cannot attest (no readable %s entry)"
	residueReasonUnreadable = "pid %d: state file unreadable (%v)"
	residueReasonUnparsable = "pid %d: body is not a worker state record (%v)"

	// workerStatusPruneFmt / workerStatusPruneFailedFmt report the start-up
	// prune: how many stale files a STARTING worker removed, and any removal
	// that FAILED (a delete that did not happen is never a silent success).
	workerStatusPruneFmt       = "%s: removed %d stale worker state file(s) left by dead or recycled pids\n"
	workerStatusPruneFailedFmt = "%s: could not remove stale worker state file %s: %v\n"

	// workerStatusFallbackNote explains WHY a listed worker has no state file:
	// the argv[0] fallback is a weaker (but honest) source, and the operator
	// must be able to tell the two apart.
	workerStatusFallbackNote = "(no state file — identified by argv; it predates the state record or could not write it)"

	// workerStatusOwnProcessNote marks the line this very process contributed —
	// a `gopi worker --status` run from inside a worker's session would
	// otherwise look like a second worker.
	workerStatusOwnProcessNote = "(this process: the --status invocation itself)"

	// --- FOREIGN-PROFILE attribution (epic worker-count-attribution) ---
	//
	// A worker-shaped process that belongs to ANOTHER profile is real, running
	// and none of this operator's business: it drains the other profile's
	// queue. It is therefore reported as its OWN block — never as a worker line
	// (that is the miscount), and never dropped (a process the scan saw and did
	// not explain is the silent omission Rule 15 forbids). %s = the program name
	// ("gopi worker"), %d = the count, %s = the binary name, %s = the worker verb.
	workerStatusForeignFmt = "%s: %d detached `%s %s` process(es) belong to ANOTHER profile — NOT counted in the listing above:"
	// workerStatusForeignMeaningA names the criterion (the process's OWN
	// environment), because that is the one fact that decides the block.
	workerStatusForeignMeaningA = "  (what this means: a worker is attributed to the profile ITS OWN environment names — GOPI_HOME, else HOME — so another profile's workers can run while this profile has none.)"
	// workerStatusForeignMeaningB answers the operator's next question ("so why
	// is my queue not draining?") and names the usual origin of the leak.
	workerStatusForeignMeaningB = "  (they drain the OTHER profile's queue, so they will never pick up THIS profile's pending tasks. A leaked test fixture — a worker started with HOME pointing at a sandbox — is a common origin.)"
	// workerStatusForeignRowFmt lays out ONE foreign row: pid + the profile the
	// process's own environment names. %s = indent, %d = pid, %s = profile dir.
	workerStatusForeignRowFmt = "%spid %d: profile %s"
	// workerStatusForeignIndent prefixes every foreign row, so the block reads
	// as a list even when the profile paths are long.
	workerStatusForeignIndent = "  "
	// workerStatusForeignSample bounds the named rows (the operator reads a
	// COUNT first; the sample is for orientation, and a fleet of leaked
	// fixtures must not push the report off the screen).
	workerStatusForeignSample = 6
	// workerStatusForeignMoreFmt states how many rows the sample omitted.
	workerStatusForeignMoreFmt = "  … and %d more (not listed)"
	// workerStatusForeignRowPrefixFmt is the fixed part of a foreign row, so the
	// profile path can be capped to fit the row without re-deriving the layout.
	// %s = indent, %d = pid.
	workerStatusForeignRowPrefixFmt = "%spid %d: profile "
	// workerStatusForeignProfileMinRunes is the floor of the profile budget: a
	// degenerate layout still names something rather than nothing.
	workerStatusForeignProfileMinRunes = 24

	// workerStatusNoWorkersForeignFmt is the extra note under an EMPTY
	// enumeration that explains it when foreign workers exist: without it,
	// "no workers running" beside a non-empty pending queue looks like the
	// scan's failure rather than the profile's exact situation. %d = count.
	workerStatusNoWorkersForeignFmt = "  note: %d detached worker process(es) on this host belong to ANOTHER profile (their own GOPI_HOME/HOME) — listed below, not this profile's fleet."

	// procWorkerCommName is the per-process NAME file under a procfs root
	// (/proc/<pid>/comm): the basename of the executed binary, as the kernel
	// reports it. Declared here rather than reaching into internal/tools'
	// unexported procfs names, which belong to another package's scan.
	procWorkerCommName = "comm"
	// procWorkerCmdlineName is the NUL-separated argv file (/proc/<pid>/
	// cmdline) the argv-shape predicate reads.
	procWorkerCmdlineName = "cmdline"
	// procWorkerStatName is the process STATUS file (/proc/<pid>/stat), read
	// only for its SESSION field — the detachment test of the argv fallback.
	procWorkerStatName = procname.StatName
	// procWorkerEnvironName is the per-process ENVIRONMENT file under a procfs
	// root (/proc/<pid>/environ), read for the PROFILE ATTRIBUTION: GOPI_HOME
	// (or HOME) from the process's OWN environment is what says which profile a
	// worker belongs to. The file is readable by the process's OWNER only, so
	// an unreadable one is handled as unresolvable, never guessed at.
	procWorkerEnvironName = "environ"

	// workerStatusActivityWindow bounds how long an unclosed round may be
	// reported as in flight. A worker that started a round and never stamped its
	// end either is still working or was killed mid-round; past the window the
	// honest verdict is "idle" (the operator still sees the last round's stamps),
	// never a ghost "busy" that outlives the process (Rule 15).
	workerStatusActivityWindow = 6 * time.Hour

	// workerStatusCwdTool / workerStatusCwdField are the userland prober and
	// the field it prints a process's working directory under. Named once so
	// the argv of the bounded child is not a literal at the call site.
	workerStatusCwdTool  = "ps"
	workerStatusCwdField = "cwd"

	// workerStatusCwdProbeTimeout bounds the child that resolves a fallback
	// worker's CWD. /proc/<pid>/cwd needs the OWNER's privileges (the readlink
	// fails for another user's process), so `ps -o cwd` as THIS user is the
	// best-effort substitute; the read must never hang a status query.
	workerStatusCwdProbeTimeout = 3 * time.Second
)

// workerStatusState values — the IDLE/BUSY vocabulary of the record. They are
// the wire values of the state file's "state" field, so they are constants
// (Rule 1b: compared, written and rendered through one spelling each).
const (
	workerStatusStateIdle = "idle"
	workerStatusStateBusy = "busy"
)

// workerStatusActivity* are the two activity words the report renders beside
// the state — one definition each, referenced by the ONE renderer
// (workerStateRecord.activity).
const (
	workerStatusActivityBusy    = "busy"
	workerStatusActivityDrained = "drained queue"
)

// workerStateRecord is the durable per-pid worker record read by the
// enumeration: the writer's workerControlState PLUS the fields this epic adds
// (binary version, the current task/session, the idle|busy state and the
// update stamp). It is a SUPERSET type, not a re-declaration of the writer's
// struct, so an older file (missing the new fields) still decodes and every
// field the reader needs has a defined zero value.
type workerStateRecord struct {
	PID     int    `json:"pid"`
	Started string `json:"started"`
	Version string `json:"version,omitempty"`
	Rounds  int    `json:"rounds"`
	// State is the idle|busy word the writer publishes (workerStatusState*).
	// It is ADVISORY: the reader derives idle/busy from the round timestamps
	// below (one source of truth for "is a round in flight" — the writer's own
	// Busy() does the same), so a hand-edited or stale word cannot make the
	// status line lie about whether the worker is mid-round.
	State          string `json:"state,omitempty"`
	Updated        string `json:"updated,omitempty"`
	LastRoundStart string `json:"last_round_start,omitempty"`
	LastRoundEnd   string `json:"last_round_end,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`

	// Weaker carries the honest source of this record: the argv[0] fallback
	// (true) or the state file (false). The reader sets it alone, so rendering
	// is the only thing that branches.
	Weaker bool `json:"-"`
}

// Busy reports whether the record describes an IN-FLIGHT round — the same
// timestamp comparison workerControlState.Busy performs (Rule 2: the record
// timestamps carry the fact; the "state" word is advisory only).
func (r workerStateRecord) Busy() bool {
	if r.LastRoundStart == "" {
		return false
	}
	if r.LastRoundEnd != "" && r.LastRoundStart <= r.LastRoundEnd {
		return false
	}
	return r.inFlightWindowOpen()
}

// inFlightWindowOpen is the honesty guard on the busy verdict (Rule 15): the
// file says a round started and no timestamp closed it, but the writer may have
// died mid-round (SIGKILL leaves no chance to write anything). A round that
// started more than workerStatusActivityWindow ago is therefore NOT reported as
// in-flight — the operator sees "idle" plus the file's stamps and can judge,
// instead of a ghost "busy" that never ends. A timestamp equal to the window
// edge counts as open, so the boundary is not a silent rounding.
func (r workerStateRecord) inFlightWindowOpen() bool {
	started, err := time.Parse(workerStatusStampLayout, r.LastRoundStart)
	if err != nil {
		return false // unparsable stamp: never claim activity we cannot date
	}
	age := time.Since(started)
	return age <= workerStatusActivityWindow
}

// StateWord is the idle|busy word rendered for an operator — DERIVED, never
// read from the file's advisory field.
func (r workerStateRecord) StateWord() string {
	if r.Busy() {
		return workerStatusStateBusy
	}
	return workerStatusStateIdle
}

// activity names what the worker is doing RIGHT NOW, MINUS the task: the
// session of the in-flight round, or the fact that the queue is drained.
//
// The TASK is deliberately not here. It is a FIELD on the line
// (worker_status_task.go, `task=<id>`), derived by ONE function for all three
// worker shapes, and printing it twice — once as the field, once inside this
// sentence — would leave a reader (and a grep) unable to tell which occurrence
// is the queryable one. Rule 2: one fact, one renderer.
func (r workerStateRecord) activity() string {
	if !r.Busy() {
		return workerStatusActivityDrained
	}
	if r.SessionID != "" {
		return workerStatusActivityBusy + " (session " + r.SessionID + ")"
	}
	return workerStatusActivityBusy
}

// workerStatusWorker is ONE worker as the enumeration found it: the pid, where
// it came from (state file or argv fallback), the record when there is one, and
// the pid's command description for the fallback case.
type workerStatusWorker struct {
	PID       int
	StatePath string
	Record    workerStateRecord
	Command   string
	Fallback  bool
	StateDir  string
	// ProcessImage is the base name of the live process's EXECUTABLE IMAGE
	// (/proc/<pid>/exe, then the process's own argv[0]) — "" when neither
	// channel could be read. For a state-file entry it is the ATTESTATION: a
	// record is rendered as a worker only when this image names the gopi
	// program. It is the IMAGE and not comm (the executable FILE's basename)
	// because a gopi binary built under another file name is still this program
	// (bug 01a131fcb8337c1b).
	ProcessImage string
	// Attested is true when the entry was confirmed to BE a gopi process, not
	// merely to have a state file named after a live pid. It is what stops a
	// recycled pid from being reported as a worker.
	Attested bool
	// Profile is the profile this worker belongs to, as its OWN environment
	// named it (the gopi data root, e.g. /home/user/.gopi), or "" when the /proc
	// entry could not be read. It is the ATTRIBUTION fact: a detached worker
	// whose profile differs from the caller's is another profile's worker.
	Profile string
}

// Report renders the one honest line per worker the epic asks for: the pid,
// the idle|busy word (or the argv-fallback marker), the TASK FIELD this epic
// adds (`task=<id>` — see worker_status_task.go), what it is running and which
// record was read.
//
// THE FIELD GOES BEFORE THE PROSE on purpose: the token is the queryable half
// (`grep -o 'task=[^ ]*'`), so a reader who only wants it never has to walk the
// sentence that follows it, and its position is identical on every shape.
func (w workerStatusWorker) Report() string {
	task := w.Task()
	var b strings.Builder
	fmt.Fprintf(&b, "%s pid %d: %s %s", workerProgTopLevel, w.PID, w.Record.StateWord(), task.Field())
	if w.Fallback {
		fmt.Fprintf(&b, " — %s %s", w.Command, workerStatusFallbackNote)
		return b.String()
	}
	// WHICH IMAGE THIS PID RUNS — the fact that decided whether the sentences it
	// prints can be trusted (row 01a18301: a worker on an image two merged
	// delivery fixes old reported an ALREADY-DELIVERED row as "delivery did not
	// land", and nothing in this report said which binary was speaking). It goes
	// on the worker's own line because that is the line a dispatcher reads, and
	// it is a clause rather than a field so an existing reader's grep for the
	// pid/state/task triple is unaffected.
	fmt.Fprintf(&b, " — %s", workerImageRowNote(w.PID))
	// The in-flight round's activity MINUS the task (it is the field above).
	fmt.Fprintf(&b, " — %s", w.Record.activity())
	if w.StatePath != "" {
		fmt.Fprintf(&b, " [state %s", w.StatePath)
		if w.Record.Updated != "" {
			fmt.Fprintf(&b, ", updated %s", w.Record.Updated)
		}
		b.WriteString("]")
	}
	return b.String()
}

// workerStatusResidue is ONE state file the enumeration read but REFUSED to
// render as a worker: the file, and the honest WHY (dead pid / recycled pid /
// unattestable / unreadable / unparsable). The two fields are separate — the
// reason is what the operator acts on and is rendered verbatim; only the
// verbose listing adds the path back beside it.
type workerStatusResidue struct {
	Path   string
	Reason string
}

// workerStatusForeign is ONE worker-shaped process that belongs to a DIFFERENT
// profile: real, detached, and not this operator's fleet. It carries the pid and
// the profile the process's own environment names, so the operator can see WHERE
// the leaked fleet came from (a sandbox path is self-evidently a test fixture).
type workerStatusForeign struct {
	PID     int
	Profile string
}

// workerStatusScan is one whole enumeration: the live workers, the stale state
// files (pid gone) and the unreadable ones. It is a VALUE, not a print — the
// CLI renders it, so the rendering rules (the empty case, the counts) live in
// exactly one place.
type workerStatusScan struct {
	Workers []workerStatusWorker
	// Residue is every state file the enumeration read but REFUSED to render as
	// a worker, each carrying the reason (dead pid / recycled pid / unattestable
	// / unreadable / unparsable). One list, because the operator's question is
	// "which of these files is a worker?" and the answer for all of them is no.
	Residue []workerStatusResidue
	// Foreign is every detached worker-shaped process whose PROFILE is not the
	// caller's. They are kept OUT of Workers (they are not this profile's fleet)
	// and OUT of Residue (residue is state FILES; these are live processes) —
	// a third list, because both of the other two would lie about them.
	Foreign []workerStatusForeign
	// Foreground is every worker-shaped process in THIS profile that is NOT a
	// detached daemon — a bare `gopi worker`, the shape the operator most often
	// runs. It is a FOURTH list because each of the other three would lie about
	// it: Workers would call it a daemon (it has no state file), Residue is
	// about files (there is no file), and Foreign is about other profiles
	// (this one is the caller's own). Bug 01a131c8-0500 is what its absence
	// cost: `--status` said "no workers running" and `--stop` said "a
	// FOREGROUND `gopi worker` … is unaffected" while this very pid was alive.
	Foreground []int
	// Profile is the caller's own profile (the environment's gopi data root), or
	// "" when it could not be resolved — in which case every worker is counted
	// (the pre-attribution behaviour), because a scan that drops workers it
	// cannot attribute under-reports.
	Profile string
	Dir     string
}

// workerStatusForeignLines renders the foreign-profile block: the count, what it
// means, at most workerStatusForeignSample pids with their profiles, and how
// many were left out. It returns nil when there is nothing foreign — the block
// does not exist on the happy path.
func workerStatusForeignLines(foreign []workerStatusForeign) []string {
	if len(foreign) == 0 {
		return nil
	}
	lines := []string{
		fmt.Sprintf(workerStatusForeignFmt, workerProgTopLevel, len(foreign), binaryNameGopi, procname.VerbWorker),
		workerStatusForeignMeaningA,
		workerStatusForeignMeaningB,
	}
	shown := foreign
	more := 0
	if len(shown) > workerStatusForeignSample {
		more = len(shown) - workerStatusForeignSample
		shown = shown[:workerStatusForeignSample]
	}
	for _, f := range shown {
		lines = append(lines, workerStatusForeignRow(f))
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf(workerStatusForeignMoreFmt, more))
	}
	return lines
}

// workerStatusForeignRow renders ONE foreign row: the pid and the profile the
// process's own environment names. The PROFILE is capped so the row fits a
// terminal line (Rule: the report is read on a terminal, the measured host's
// sandbox paths are 130+ characters) — the pid, which is the actionable part,
// never is. The cap is computed from the actual prefix, so the row stays inside
// workerStatusResidueMaxRowRunes whatever the pid's digit count.
func workerStatusForeignRow(f workerStatusForeign) string {
	prefix := fmt.Sprintf(workerStatusForeignRowPrefixFmt, workerStatusForeignIndent, f.PID)
	return prefix + truncateRunes(f.Profile, workerStatusForeignProfileBudget(prefix))
}

// workerStatusForeignProfileBudget is how many runes of a profile path fit
// beside prefix: the row cap minus the prefix (and the ellipsis truncateRunes
// appends), floored at workerStatusForeignProfileMinRunes so a degenerate layout
// still names something — a row that names NO profile is worse than a long one.
func workerStatusForeignProfileBudget(prefix string) int {
	budget := workerStatusResidueMaxRowRunes - len([]rune(prefix)) - 1
	if budget < workerStatusForeignProfileMinRunes {
		return workerStatusForeignProfileMinRunes
	}
	return budget
}

// workerStatusEnumerate is the reader: the state files (the STRONG source —
// they carry the pid's own record) first, then the process table for every
// worker those files did not account for.
//
// The ORDER is the fix for a measured lie (2026-09-14): the argv half used to
// run FIRST and mark its pids seen, so a daemon that had written its own state
// file was rendered from the WEAK source instead — `gopi worker --status`
// printed "pid N: idle — … (no state file — identified by argv; it predates the
// state record or could not write it)" for a worker whose record was sitting
// right there on disk (reproduced on a sandboxed profile: the daemon wrote
// worker-state-<pid> and the status line denied it). The record half is
// authoritative because it is the only source that can carry what the operator
// actually asks — idle|busy, the current task and session, the version, the
// last round's stamps; the process table can only say "a worker-shaped process
// exists". So the strong source goes first, and the fallback covers the
// remainder: a worker that never wrote a file (foreground, an older binary, an
// unwritable state dir) — exactly what workerStatusFallbackNote claims.
//
// PROFILE ATTRIBUTION additionally gates that fallback half (epic worker-count-
// attribution): the process-table scan is machine-wide by construction (it is
// argv shape, the same predicate the launcher's --count uses), so a detached
// worker in ANOTHER profile — a leaked test fixture, a second profile's fleet —
// was rendered as this operator's worker. It is not: it drains the other
// profile's queue and will never touch this one's pending tasks (measured
// 2026-09-13: two such workers sat beside the operator's own, and the report
// made a queue that had workers look like one that did not). Each process is
// attributed by ITS OWN environment (GOPI_HOME, else HOME); a differing profile
// becomes a Foreign row, never a worker line — and never a silent drop.

func workerStatusEnumerate(dir string) (workerStatusScan, error) {
	scan := workerStatusScan{Dir: dir, Profile: workerStatusOwnProfile()}
	seen := map[int]bool{}

	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// A state directory that cannot be listed is REPORTED (Rule 15) — the
		// caller still falls back to the process table, which needs no
		// directory at all.
		return scan, fmt.Errorf("read worker state directory %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		pid, ok := stateFileCarriesPID(name)
		if !ok {
			// A file that is not a state record is not THIS reader's business:
			// the directory also holds stop/force request files (their names
			// carry no pid prefix) and the writers' ".tmp" install files.
			continue
		}
		if tools.PidGone(pid) {
			// A pid that is PROVABLY gone is a record of an instance that exited
			// without running its cleanup — the file says THAT much and no more:
			// the cause (hard kill, crash, or a foreign writer such as a test
			// fixture) is not in the file. The reason string names the measured
			// fact and refuses the unprovable cause (see residueReasonDead).
			//
			// PidGone, not `!PidAlive`: this arm CLASSIFIES the record as
			// residue, and the residue listing is what the mutating sweep acts
			// on. EPERM (alive, not ours) is not a death, so such a record is
			// not residue — it falls through to the identity check below and is
			// reported as somebody else's process instead of being described as
			// dead (epic 01a17e87).
			scan.Residue = append(scan.Residue, workerStatusResidue{Path: path,
				Reason: fmt.Sprintf(residueReasonDead, pid)})
			continue
		}
		// The pid is ALIVE — but liveness is not identity. The file is trusted
		// only when the live process behind that pid is actually running the
		// gopi PROGRAM: the pid counter wraps (pid_max is in the millions on this
		// host), so a state file outliving its worker sooner or later names an
		// unrelated process, and rendering that as "gopi worker pid N: idle" is
		// a fabricated worker (measured 2026-09-13: a file for pid 899174 read
		// as a live agent.test).
		//
		// THE PROGRAM IS READ FROM THE IMAGE, NOT FROM comm (bug 01a131fcb8337c1b):
		// comm is the executable FILE's basename, so a worker built as bin/gopi-fix
		// was reported "alive but is not gopi" while running this very program.
		image, ierr := processImageName(pid)
		switch {
		case ierr != nil:
			// Neither the exe link nor the process's own argv[0] answers: the
			// record cannot be attributed, so it is reported as residue rather
			// than promoted to a worker (Rule 15).
			scan.Residue = append(scan.Residue, workerStatusResidue{Path: path,
				Reason: fmt.Sprintf(residueReasonUnattested, pid, procname.ExeName)})
			continue
		case !strings.HasPrefix(image, workerProgramPrefix):
			scan.Residue = append(scan.Residue, workerStatusResidue{Path: path,
				Reason: fmt.Sprintf(residueReasonNotGopi, pid, workerProgramPrefix, image)})
			continue
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			scan.Residue = append(scan.Residue, workerStatusResidue{Path: path,
				Reason: fmt.Sprintf(residueReasonUnreadable, pid, rerr)})
			continue
		}
		var rec workerStateRecord
		if jerr := json.Unmarshal(raw, &rec); jerr != nil {
			scan.Residue = append(scan.Residue, workerStatusResidue{Path: path,
				Reason: fmt.Sprintf(residueReasonUnparsable, pid, jerr)})
			continue
		}
		if rec.PID == 0 {
			rec.PID = pid // an old writer that omitted the field: the NAME is the pid
		}
		// Marked BEFORE the fallback half runs — the record half runs FIRST, so
		// the pid is rendered from this record and never re-emitted from the
		// process table: a second line for the same process is the over-report
		// this epic removed.
		seen[pid] = true
		scan.Workers = append(scan.Workers, workerStatusWorker{
			PID: pid, StatePath: path, Record: rec, StateDir: dir,
			ProcessImage: image, Attested: true,
		})
	}

	// The FALLBACK half (PROFILE ATTRIBUTION): the process table supplies only
	// the workers the record half did not account for — the pids it emitted are
	// `seen`, so a recorded worker is never ALSO described by argv (the dedupe is
	// true by construction, not by a later pass). Each pid is attributed by ITS
	// OWN environment: a detached worker whose profile differs from the caller's
	// is another profile's worker — a Foreign row, never a worker line, and never
	// a silent drop.
	pids, foreign := fallbackWorkerPIDs(scan.Profile)
	for _, f := range foreign {
		// A foreign worker owns no pid in THIS profile's accounting: the pid is
		// deliberately NOT marked seen, because its state file (if it ever wrote
		// one) lives in the other profile's directory and can never appear here.
		scan.Foreign = append(scan.Foreign, f)
	}
	for _, pid := range pids {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if pid == os.Getpid() {
			// THIS process is a `gopi worker --status` invocation, not a worker
			// loop: its verb is the worker verb (so the argv shape matches) but
			// it runs no round. It is listed, marked, rather than suppressed —
			// silently dropping a process the scan really saw is the kind of
			// help an operator cannot audit.
			scan.Workers = append(scan.Workers, workerStatusWorker{
				PID:      pid,
				Record:   workerStateRecord{PID: pid, Weaker: true},
				Command:  workerStatusCommandLine(pid) + " " + workerStatusOwnProcessNote,
				Fallback: true,
			})
			continue
		}
		scan.Workers = append(scan.Workers, workerStatusWorker{
			PID:      pid,
			Record:   workerStateRecord{PID: pid, Weaker: true},
			Command:  workerStatusCommandLine(pid),
			Fallback: true,
			StateDir: dir,
		})
	}

	sort.Slice(scan.Workers, func(i, j int) bool { return scan.Workers[i].PID < scan.Workers[j].PID })
	// The FOREGROUND half: the workers the record half and the detached
	// fallback above both cannot see (no state file, no lock, no setsid). The
	// pids already accounted for are skipped by construction, so one process is
	// never described twice.
	for _, pid := range foregroundWorkerPIDs(scan.Profile) {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		scan.Foreground = append(scan.Foreground, pid)
	}
	sort.Ints(scan.Foreground)
	return scan, nil
}

// stateFileCarriesPID recovers the pid from a state file NAME, strictly:
// workerStateFilePrefix + digits, nothing else. It is deliberately NOT
// tools.ControlPidFromName (which accepts any "<prefix><int>"): the file name
// here ends in the pid, and a looser match would accept a future sibling name.
func stateFileCarriesPID(name string) (int, bool) {
	if !strings.HasPrefix(name, workerStateFilePrefix) {
		return 0, false
	}
	digits := strings.TrimPrefix(name, workerStateFilePrefix)
	if digits == "" || strings.HasSuffix(digits, workerStateFileSuffix) {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	pid, err := strconv.Atoi(digits)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// fallbackWorkerPIDs lists the pids whose PROCESS SHAPE is a gopi worker in the
// CALLER'S OWN profile, plus (separately) the worker-shaped processes that
// belong to ANOTHER profile. Shape alone is machine-wide by design — comm == the
// gopi binary name and argv[1] == the worker verb, the launcher's --count
// predicate — so ownership is a SECOND question, answered by the process's own
// environment (workerProfileOf). Splitting the two questions is what keeps the
// shape predicate honest (no ownership test smuggled into it) and keeps the
// report honest (a foreign worker is listed in its own block, never counted and
// never hidden). Unreadable /proc entries are skipped, never guessed at.
func fallbackWorkerPIDs(own string) (mine []int, foreign []workerStatusForeign) {
	entries, err := os.ReadDir(procnameRoot())
	if err != nil {
		return nil, nil
	}
	type shaped struct {
		pid     int
		profile string
	}
	var matches []shaped
	for _, e := range entries {
		pid, aerr := strconv.Atoi(e.Name())
		if aerr != nil || pid <= 0 {
			continue
		}
		if !tools.PidAlive(pid) {
			continue // a zombie is not a running worker (the epic's zombie case)
		}
		if workerProcessShape(pid) {
			matches = append(matches, shaped{pid: pid, profile: workerProfileOf(pid)})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pid < matches[j].pid })
	for _, m := range matches {
		// THE DEGRADATION, stated: when either side is unresolvable the verdict
		// is "count it" — never "drop it". An unattributable worker reported as
		// this profile's is a visible miscount of one line; an unattributable
		// worker dropped is an invisible under-report of the whole fleet, which
		// is the failure mode an operator cannot audit.
		if own == "" || m.profile == "" || m.profile == own {
			mine = append(mine, m.pid)
			continue
		}
		foreign = append(foreign, workerStatusForeign{PID: m.pid, Profile: m.profile})
	}
	return mine, foreign
}

// workerStatusOwnProfile resolves THIS process's profile — the gopi data root
// the status report is about, from the same environment this process runs under
// (config.GopiDir already applies GOPI_HOME-then-HOME; the environment read is
// workerProfileFromEnv's, so both sides share one resolution).
//
// It is a FUNCTION VALUE indirection ("sealed" var) because a test must be able
// to pose as another profile: the CLI e2e checks attribution against a real
// binary whose own profile is a temp dir, and the only honest way to do that
// without a second flag is to let the test name the pid's environment. Sealed
// means the seam is never written outside this file.
var workerStatusOwnProfile = func() string {
	return workerProfileFromEnv(os.Environ())
}

// workerProfileOf resolves the profile a PID belongs to, from that process's OWN
// environment (/proc/<pid>/environ). It answers "" when the environment cannot
// be read (another user's process, a kernel thread, a sandboxed hidepid mount)
// or names no home at all — an empty answer is "unattributable", never a guess.
func workerProfileOf(pid int) string {
	b, err := os.ReadFile(filepath.Join(procnameRoot(), strconv.Itoa(pid), procWorkerEnvironName))
	if err != nil {
		return ""
	}
	// /proc/<pid>/environ is NUL separated NAME=VALUE pairs and does NOT preserve
	// duplicates (the kernel keeps one entry per name, the last written value) —
	// which is exactly what the process itself would see. Trailing NUL.
	return workerProfileFromEnv(strings.Split(strings.TrimRight(string(b), "\x00"), "\x00"))
}

// workerProfileFromEnv is the ONE attribution rule, in the one place: a gopi
// process's profile is $GOPI_HOME/.gopi when GOPI_HOME is set, else $HOME/.gopi.
// It mirrors config.GopiDir (config.GopiDirEnv / config.ProfileDirName — the
// names, never re-typed literals, so the rule cannot drift from the one every
// other data-path resolution uses).
func workerProfileFromEnv(env []string) string {
	home := ""
	gopiHome := ""
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, config.GopiDirEnv+"="):
			gopiHome = strings.TrimSpace(strings.TrimPrefix(kv, config.GopiDirEnv+"="))
		case strings.HasPrefix(kv, procname.EnvHome+"="):
			home = strings.TrimSpace(strings.TrimPrefix(kv, procname.EnvHome+"="))
		}
	}
	if gopiHome != "" {
		return filepath.Join(gopiHome, config.ProfileDirName)
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, config.ProfileDirName)
}

// procnameRoot is the procfs mount point of the fallback scan. It honors
// procname.EnvProcRoot — the SAME seam the launcher script and its gate use —
// so a test can drive the exact production scan over a synthetic tree.
func procnameRoot() string {
	if v := strings.TrimSpace(os.Getenv(procname.EnvProcRoot)); v != "" {
		return v
	}
	return tools.ProcRootPath
}

// workerProcessShape is the launcher's predicate, evaluated against /proc:
// the process RUNS THE GOPI PROGRAM, its FIRST ARGUMENT is the worker verb, and
// the process is DETACHED (it leads its own session — the post-setsid signature
// of the daemon). The program is identified by its IMAGE (procname.IsGopiImage:
// /proc/<pid>/exe, then the process's own argv[0]) and NEVER by comm — comm is
// the executable FILE's basename, so a gopi binary built under another file name
// (bin/gopi-fix) produced "no live gopi worker" for a worker that was running
// this very program (bug 01a131fcb8337c1b; Rule 15: a proxy metric is never
// reported as the measurement). The argv test is an exact comparison of argv[1] —
// so a shell whose command line merely mentions `gopi worker` is not a worker,
// and a `gopi task run` child (argv[1] == the task verb) is NOT counted. The
// detachment test is what keeps the operator's OWN `gopi worker --status`
// (a foreground invocation) out of the enumeration.
func workerProcessShape(pid int) bool {
	root := procnameRoot()
	argv, ok := procname.Cmdline(root, strconv.Itoa(pid))
	if !ok || len(argv) < 2 || argv[1] != procname.VerbWorker {
		return false
	}
	if !procname.WorkerImageIsGopi(root, strconv.Itoa(pid), argv[0]) {
		return false
	}
	return workerDetached(root, pid)
}

// workerDetached reports whether pid leads its own process session — the
// post-setsid signature of the daemon (pid == session id, field 6 of
// /proc/<pid>/stat). An unreadable or malformed stat is NOT detached: the
// fallback only ever lists a worker it can positively identify.
func workerDetached(root string, pid int) bool {
	// The comm field may itself contain spaces AND parentheses, so the field is
	// read through internal/procname.StatField — the ONE parse that counts from
	// the LAST ')' (Rules 13/14). The session field is what says "detached":
	// session == pid means the process is a session leader.
	sessionRaw, ok := procname.StatFieldString(filepath.Join(root, strconv.Itoa(pid), procWorkerStatName), procname.StatFieldSession)
	if !ok {
		return false
	}
	session, err := strconv.Atoi(sessionRaw)
	if err != nil {
		return false
	}
	return session == pid
}

// processImageName resolves the EXECUTABLE IMAGE base name of a live pid — the
// channel that identifies the PROGRAM a process runs, as opposed to comm (the
// executable FILE's basename). It is procname.ImageName bound to a pid's own
// argv[0], which this reads from /proc/<pid>/cmdline (Rule 14: the identity
// judgement lives in the leaf package; this adapter only supplies the pid's
// argv).
//
// An error means NEITHER channel answered (no exe link, no cmdline): the record
// cannot be attributed, and the caller reports that rather than reading it as
// "not a gopi process" (Rule 15a — unknown is not a value of the no-branch).
func processImageName(pid int) (string, error) {
	root := procnameRoot()
	argv, _ := procname.Cmdline(root, strconv.Itoa(pid))
	argv0 := ""
	if len(argv) > 0 {
		argv0 = argv[0]
	}
	if name := procname.ImageName(root, strconv.Itoa(pid), argv0); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("no readable %s link and no cmdline for %s/%d", procname.ExeName, root, pid)
}

// pruneWorkerStateResidue clears the state files of pids that are gone in THIS
// profile's worker state directory. It is called when a worker STARTS — the
// mutating moment that heals the residue — and never from the read-only
// `--status` face (cmd/agent/status_action_conflict_e2e_test.go pins that a
// bare `--status` creates and removes nothing).
//
// Every worker face calls it, including a FOREGROUND one with no state file of
// its own: the measured residue was left by exactly those (leaked test-fixture
// workers), so tying the sweep to the resident daemon alone would leave the
// pile that the operator complained about untouched. Failures are reported and
// never stop the worker (Rule 15).
func pruneWorkerStateResidue() {
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v — stale worker state files were not swept\n", workerProgTopLevel, err)
		return
	}
	if removed := workerStateResiduePrune(dir, workerStateFilePrefix); removed > 0 {
		fmt.Fprintf(os.Stderr, workerStatusPruneFmt, workerProgTopLevel, removed)
	}
}

// workerStateResiduePrune removes the state files of pids that are gone —
// records of instances that exited without running their cleanup (a hard kill,
// a crash, or a writer that never owned the record). It runs on a MUTATING
// path (a worker starting up), never in `--status`: the status face is
// read-only by contract (cmd/agent/status_action_conflict_e2e_test.go pins that
// a bare `--status` creates nothing). Removal is best-effort and each failure
// is REPORTED, because a delete that silently did not happen leaves the
// operator's enumeration permanently polluted (Rule 15).
//
// A live pid's file is NEVER touched, even when the pid turns out to belong to
// another process: a running worker may hold it, and the residue case costs
// nothing but a bounded status line. The count returned is how many files were
// really removed.
//
// THE DELETE PREDICATE IS PidGone, NOT `!PidAlive` (epic 01a17e87). This arm
// REMOVES a file, so it must ask a question whose false answer is "keep": only
// a MEASURED death (ESRCH, or a proven-terminal /proc state) is a reason to
// delete. EPERM — the process exists and this user may not signal it — is a
// measurement of EXISTENCE, so it keeps the file. Written as `!PidAlive` the
// intent would be identical today but invisible: a reader could not tell
// whether the unknown case was considered, and the next change to PidAlive's
// conservative mapping would silently decide it here.
func workerStateResiduePrune(dir, prefix string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		pid, ok := stateFileCarriesPID(e.Name())
		if !ok || !tools.PidGone(pid) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if rerr := tools.ClearControlText(path); rerr != nil {
			fmt.Fprintf(os.Stderr, workerStatusPruneFailedFmt, workerProgTopLevel, path, rerr)
			continue
		}
		removed++
	}
	return removed
}

// workerStatusResidueLines renders the residue as a readable BLOCK: a header
// with the exact count, what the files ARE, at most workerStatusResidueSample
// named entries with their reasons, how many were left out, and the one-line
// remedy. It returns nil when there is no residue.
//
// The bound and the SHAPE are both the point. The measured host carried 121
// entries, and the previous rendering — every entry comma-joined onto ONE line
// after a colon — produced 929 characters that read like a worker LIST (the
// count was correct and the form still lied: an operator scanning for "what is
// running" sees a wall of pids). One row per entry, basename first, reason
// second; the full path only in the verbose listing.
//
// The remedy is built from the DIR the enumeration actually read and the same
// prefix the reader matches, so the command an operator copies deletes exactly
// what the count counted (Rule 15: a count with no next step is a dead end).
func workerStatusResidueLines(dir string, residue []workerStatusResidue, verbose bool) []string {
	if len(residue) == 0 {
		return nil
	}
	lines := []string{
		fmt.Sprintf(workerStatusResidueFmt, workerProgTopLevel, len(residue)),
		workerStatusResidueMeaningA,
		workerStatusResidueMeaningB,
	}
	shown := residue
	more := 0
	if !verbose && len(shown) > workerStatusResidueSample {
		more = len(shown) - workerStatusResidueSample
		shown = shown[:workerStatusResidueSample]
	}
	for _, r := range shown {
		if verbose {
			lines = append(lines, fmt.Sprintf(workerStatusResidueRowVerboseFmt,
				workerStatusResidueIndent, filepath.Base(r.Path), r.Reason, r.Path))
			continue
		}
		lines = append(lines, fmt.Sprintf(workerStatusResidueRowFmt,
			workerStatusResidueIndent, filepath.Base(r.Path), r.Reason))
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf(workerStatusResidueMoreFmt, more, workerStatusResidueVerboseFlag))
	}
	lines = append(lines, fmt.Sprintf(workerStatusResidueRemedyFmt,
		fmt.Sprintf(workerStatusRemedyCommandFmt, dir, workerStatusStateFileGlob), len(residue)))
	return lines
}

// binaryNameGopi is the process name of every gopi process (the kernel's
// (comm) — the basename of the binary). It is read from the leaf package that
// owns the process-name vocabulary (Rule 14), never typed inline.
const binaryNameGopi = procname.NameGopi

// workerStatusCommandLine renders what a fallback pid is running: its full
// argv, plus the CWD when it can be resolved. Both are best-effort and both
// say so by their absence — a pid we cannot describe is still listed by pid,
// because "there is a worker here I cannot describe" is the truth an operator
// needs (Rule 15).
func workerStatusCommandLine(pid int) string {
	root := procnameRoot()
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), procWorkerCmdlineName))
	line := ""
	if err == nil {
		line = strings.TrimSpace(strings.Join(strings.Split(strings.TrimRight(string(b), "\x00"), "\x00"), " "))
	}
	if line == "" {
		line = workerStatusCommandUnknown
	}
	if cwd := workerStatusCwd(pid); cwd != "" {
		line += " (cwd " + cwd + ")"
	}
	return line
}

// workerStatusCommandUnknown marks a pid whose argv could not be read.
const workerStatusCommandUnknown = "(argv unreadable)"

// workerStatusCwd resolves a fallback worker's working directory. /proc/<pid>/
// cwd is only readable by the process's OWNER (or root), so this asks the
// userland prober for this user — and a failure yields "" (no directory
// claimed), never a guess. The child is bounded by workerStatusCwdProbeTimeout
// so an unresponsive probe cannot hang a status query.
func workerStatusCwd(pid int) string {
	ctx, cancel := context.WithTimeout(context.Background(), workerStatusCwdProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, workerStatusCwdTool, workerStatusCwdField, strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// --- the `gopi worker --status` face -------------------------------------

// workerStatusReport renders the whole enumeration and returns the exit code
// the caller must use. The report answers the operator's question in the order
// they ask it: what is running, why is nothing running, and what is the pile of
// state files that is NOT running.
//
// It is a REPORT, not a data structure dump. The first output an operator sees
// for an idle machine is one line — "no workers running" — and the rest of the
// answer is grouped under plain-language headings, so a reader who only wanted
// the yes/no answer stops at line 1 (measured before this change: 2 lines,
// 962 bytes, of which 929 characters were the residue list joined onto one
// line, with nothing saying what a residue file IS or what to do about it).
//
// verbose (workerStatusResidueVerboseFlag) appends the full path of every
// residue entry — for the case where the count itself is what is being
// debugged, not the yes/no answer.
func workerStatusReport(verbose, asJSON bool) int {
	dir, err := tools.ControlDirReadOnly(workerStateDirName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		return workerControlExitFailed
	}
	scan, scanErr := workerStatusEnumerate(dir)
	if asJSON {
		// The MACHINE form: the same scan, the same task derivation, one
		// document (worker_status_json.go). It returns BEFORE the human
		// rendering, so the two forms can never print halves of each other.
		return workerStatusReportJSON(dir, scan, scanErr, verbose)
	}
	// The workers first: the operator's question is "what is running?".
	//
	// A FOREGROUND worker runs but owns no daemon record, so it lands in
	// scan.Foreground, not scan.Workers — and the empty case must therefore be
	// "no workers running" only when BOTH halves are empty (bug
	// 01a131c8-0500: the line was printed while a foreground worker was alive).
	if len(scan.Workers) == 0 && len(scan.Foreground) == 0 {
		fmt.Printf("%s: %s\n", workerProgTopLevel, workerStatusNoWorkers)
		// WHAT IT MEANS and WHAT TO DO. This is the question the operator asked
		// ("why no worker?") and the bare line above answered only its first half:
		// the scan found no gopi worker process at all, and the lock names no
		// holder. Both facts are READ here, not asserted.
		for _, line := range workerStatusNoWorkersLines(workerLockPathFor(), len(scan.Foreign)) {
			fmt.Println(line)
		}
	} else {
		for _, w := range scan.Workers {
			fmt.Println(w.Report())
		}
	}
	// The FOREGROUND block sits with the workers (it is about live processes):
	// each pid, the fact that it holds no lock, and the one command that stops
	// it — an operator reading "what is running" must get the bare `gopi worker`
	// they most likely started.
	for _, pid := range scan.Foreground {
		fmt.Println(workerForegroundStatusLine(pid))
	}
	// The FOREIGN block sits with the workers (it is about live processes, not
	// files) and BEFORE the residue: it is the answer to "why is my queue not
	// draining while workers are running?".
	for _, line := range workerStatusForeignLines(scan.Foreign) {
		fmt.Println(line)
	}
	// The residue: a count, what the files are, a bounded sample, and the one
	// command that clears them.
	for _, line := range workerStatusResidueLines(dir, scan.Residue, verbose) {
		fmt.Println(line)
	}
	if scanErr != nil {
		// The state DIRECTORY itself could not be read (reported, exit 1): the
		// fallback half of the enumeration still ran, so the operator gets the
		// process-table answer AND the reason the record half is missing.
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, scanErr)
		return workerControlExitFailed
	}
	return workerControlExitOK
}

// workerForegroundStatusLine renders ONE foreground worker for `--status`: the
// pid, the fact that it holds no daemon lock, its own command line, and the ONE
// command that stops it — the actionable half an operator needs (bug
// 01a131c8-0500: the report used to omit these pids entirely, and the stop face
// called them unaffected).
func workerForegroundStatusLine(pid int) string {
	cmd := workerCommandLineOrUnknown(pid)
	return fmt.Sprintf(workerForegroundStatusFmt, workerProgTopLevel, pid,
		workerTaskOfPID(pid).Field(), cmd, workerProgTopLevel, workerControlStop) +
		" — " + workerImageRowNote(pid)
}

// workerImageRowNote renders WHICH IMAGE a pid actually runs, in the terms a
// dispatcher needs at dispatch time: the revision it carries, and whether the
// file it was started from has been replaced under it since (the kernel's own
// " (deleted)" marker). "" is never returned — an unmeasurable probe says so,
// because a blank clause would read as "nothing to report" (Rule 15).
//
// IT IS A PEER READ, not a self read, and that is the point: this process is the
// one ASKING (`gopi worker --status`), while the answer is about the pid that
// will execute the next row. The reader lives in internal/tools
// (image_peer.go) so the sentence, the arm names and the deleted-marker
// decoding have ONE definition that the worker loop also uses.
//
// The procfs root is procnameRoot() — the launcher's own seam
// (procname.EnvProcRoot) — so a test drives the exact production render over a
// fabricated tree instead of reading a live unrelated process.
func workerImageRowNote(pid int) string {
	return tools.PeerImageProbeAt(procnameRoot(), pid).RowNote()
}

// workerCommandLineOrUnknown renders a pid's own command line, or the shared
// unknown marker when /proc will not answer.
func workerCommandLineOrUnknown(pid int) string {
	if argv, ok := procname.Cmdline(procnameRoot(), strconv.Itoa(pid)); ok && len(argv) > 0 {
		return strings.Join(argv, " ")
	}
	return workerForegroundNoCmd
}

// workerStatusNoWorkersLines renders the WHY behind an EMPTY enumeration, as
// lines so the shape can be asserted (and capped) by a test rather than by
// taste: why there is no worker, the lock that was measured, what an empty lock
// does and does not prove, and the two ways to start one.
//
// foreignCount is the number of worker-shaped processes that belong to another
// profile. When it is non-zero the empty enumeration has a SECOND cause beside
// "nothing was started", and the note names it — otherwise "no workers running"
// beside a non-empty pending queue reads as a broken scan rather than as the
// exact situation (their workers, not mine).
func workerStatusNoWorkersLines(lockPath string, foreignCount int) []string {
	lines := []string{
		fmt.Sprintf(workerStatusNoWorkersWhy, workerProgTopLevel, workerProgTopLevel),
		fmt.Sprintf(workerStatusNoWorkersLock, lockPath),
		workerStatusNoWorkersLockNote,
		fmt.Sprintf(workerStatusNoWorkersGo, workerProgTopLevel, workerProgTopLevel),
	}
	if foreignCount > 0 {
		lines = append(lines, fmt.Sprintf(workerStatusNoWorkersForeignFmt, foreignCount))
	}
	return lines
}

// workerLockPathFor is the read-only lock path of THIS profile, or "" when it
// cannot be resolved (the note then simply omits the path rather than printing
// a guess).
func workerLockPathFor() string {
	path, err := workerLockPathReadOnly()
	if err != nil {
		return ""
	}
	return path
}
