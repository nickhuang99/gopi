package tools

// Process IDENTITY for an in_progress task row (epic task-process-identity,
// 2026-09-13): the "big three" facts about the process that is executing a
// row — pid, process START TIME, and command line — recorded in the tasks
// table when the row enters in_progress, and verified against the live OS
// process later.
//
// WHY THREE FACTORS. A pid alone is not an identity: the OS recycles
// numbers, so a dead runner's pid can be owned by an unrelated process a
// minute later, and every liveness consumer that trusts the pid reads that
// stranger as "my task is still running" (the false-live that keeps a zombie
// row in_progress forever). /proc/<pid>/stat's starttime is unique per
// process INSTANCE (it counts clock ticks since boot at process creation), so
// pid+starttime pins one incarnation; the command line is the third factor,
// which catches the remaining degenerate shapes (a same-pid same-tick
// mismatch is not physically possible for one process, but a row stamped by a
// build that could not read the cmdline must degrade to a weaker verdict
// rather than a fabricated one).
//
// WHERE IT IS WRITTEN: taskUpdateStamped (claimed→in_progress) and
// TaskStampSelfClaim (auto-registered rows) capture the identity of the pid
// they stamp as the holder, in the SAME UPDATE as the transition — a row can
// never carry a holder without the identity that makes the holder checkable.
// Every other transition CLEARS it (the stretch is over).
//
// WHERE IT IS READ: TaskProcessStatus (the `gopi task status` verb and the
// `gopi session resume` gate) and the `task list` display ladder. On a host without
// procfs (non-Linux) every reader degrades to "unknown" — never a fabricated
// "dead" (Rule 15: evidence may only weaken a verdict, never manufacture one).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/runlock"
)

const (
	// procCmdlineName is the per-process command-line file under
	// procRootName: NUL-separated argv, empty for a kernel thread. It is an
	// ALIAS of internal/procname's value (Rules 13/14: the name has ONE
	// definition, in the leaf package every process-fact consumer can import).
	procCmdlineName = procname.CmdlineName

	// procStatFieldOffset is the number of leading fields in procStat that
	// are NOT addressable by a simple index after the command name is
	// stripped: pid (1), the parenthesised comm (2) and state (3). A field's
	// index in the post-comm field list is therefore field-3 —
	// finishCleanupProcParent reads the parent (field 4) at index 1, which is
	// the same offset (procStatParentField). Alias of procname's state field:
	// the state letter IS the last non-addressable field, so the offset and the
	// field number are the same value (Rule 13 — referenced, never re-typed).
	procStatFieldOffset = procname.StatFieldState

	// procStatStartTimeField is procStat's starttime column (field 22 of
	// <procRootName>/<pid>/<stat>): clock ticks between system boot and the
	// process's creation. Together with the pid it identifies ONE process
	// incarnation — the property pid reuse destroys. Alias of
	// procname.StatFieldStartTime (Rules 13/14), which DERIVES the number from
	// the state field and the offset, so it has one home.
	procStatStartTimeField = procname.StatFieldStartTime

	// procClockTicksPerSec is the userspace clock-tick rate (sysconf
	// _SC_CLK_TCK). The CGO-free build cannot call sysconf, so the value is
	// named and documented instead: Linux's tick rate is 100 Hz on the
	// architectures gopi ships on. It is used ONLY to render a human-readable
	// start time — identity comparison is done on the raw tick counts, so a
	// different HZ can never produce a wrong verdict, only an approximate
	// display.
	procClockTicksPerSec = 100

	// procIdentityCmdlineMax bounds the recorded command line (Rule 1): a
	// stranger's argv can be megabytes, and the row's job is to identify the
	// process, not to archive it. Both the capture and the comparison
	// truncate to this length, so equality is stable across the bound. (It is
	// this package's own presentation bound, not a process FACT shared with
	// another side, so it stays here: the task-row schema is the only reader.)
	procIdentityCmdlineMax = 512

	// procUptimeName is the procfs uptime file, read to convert a start tick
	// count into a boot-relative wall clock for display.
	procUptimeName = "uptime"

	// procIdentityNone is the rendered "no identity recorded" value.
	procIdentityNone = "(none)"
)

// TaskProcessState* are the tri-factor verdicts for a row's holder process.
// They are the DEFINITE answers `gopi task status` prints and `gopi session resume`
// gates on (taskValidateResumePermission), so each name states who the
// holder is, not how it was computed.
const (
	// TaskProcessStateRunning: the recorded identity (or, for a row written
	// before the identity columns existed, the claim stamp's pid) names a
	// process alive RIGHT NOW on this host.
	TaskProcessStateRunning = "running"

	// TaskProcessStateDead: the recorded pid is gone — the run is over,
	// however it ended, and the row is a zombie in_progress that a resume
	// may reclaim.
	TaskProcessStateDead = "dead"

	// TaskProcessStateRecycled: the pid is alive but is NOT the recorded
	// process — a different start time (or command line). The runner is
	// dead and its pid number was reused: the row is reclaimable AND the
	// live stranger must never be reported as the holder.
	TaskProcessStateRecycled = "recycled"

	// TaskProcessStateForeign: the claim stamp names ANOTHER HOST. It is a
	// CLASSIFICATION of the stamp, not a verdict this build prints: since the
	// host dimension was added (2026-10-29) the verdict for such a row is
	// TaskProcessStateUnknown — "this host measured nothing about that pid" —
	// and the classification rides TaskProcessStatus.Foreign, so a consumer
	// that needs the reason reads the flag instead of parsing a verdict.
	//
	// The word is kept (rather than deleted) because it is in the ledger's
	// OWN OUTPUT: an older transcript, a peer's JSON, a stored report still
	// says "foreign" about a row, and a reader — human or a test — must be
	// able to meet that word and know what it meant. See
	// taskVerdictResumable's arm for the same reason.
	TaskProcessStateForeign = "foreign"

	// TaskProcessStateNoHolder: the row carries no claim stamp at all (an
	// auto-registered row from a build without self-claim, or a row whose
	// stamp was cleared). Nothing is running BY THE LEDGER'S EVIDENCE: the
	// shape the epic calls a fake in_progress, reclaimable by assigning a
	// worker.
	TaskProcessStateNoHolder = "no-holder"

	// TaskProcessStateUnknown: the evidence does not settle it (procfs
	// absent / unreadable, a stale claim stamp that cannot name this row's
	// executor). Never a fabricated death.
	TaskProcessStateUnknown = "unknown"

	// TaskProcessStateNotMeasured: NOTHING was measured — the row records no
	// process identity for this predicate to judge (a legacy row, a row
	// stamped by a build that could not read /proc), or the identity could
	// not be read back at all. It is the ledger answering "I have no
	// evidence", which must stay distinguishable from the MEASUREMENT
	// TaskProcessStateDead ("I looked, and the recorded process is gone") —
	// a caller that cannot measure reports THIS, never a silent false that
	// downstream code reads as "dead" (Rule 15b).
	//
	// It is deliberately NOT part of taskProcessStateSet: that set is the
	// vocabulary `gopi task status` prints for a row whose status names a
	// holder, and a row there always HAS a claim stamp to judge. This value
	// belongs to the identity predicate (TaskProcessLiveness), where the
	// absence of a record is the whole question.
	TaskProcessStateNotMeasured = "not-measured"
)

// taskProcessStateSet is the verdict vocabulary in report order — the
// `task status` usage text and the tests derive from it (Rule 14: one
// definition of the value set).
var taskProcessStateSet = []string{
	TaskProcessStateRunning, TaskProcessStateDead, TaskProcessStateRecycled,
	TaskProcessStateForeign, TaskProcessStateNoHolder, TaskProcessStateUnknown,
}

// taskProcIdentity is the captured "big three" of one process. HasStart is
// false when the stat file could not be read (the identity is then the pid +
// cmdline pair alone — a weaker but still useful record).
type taskProcIdentity struct {
	Pid    int
	Start  int64
	HasStr bool
	Cmd    string

	// CwdRow is the ROW this record belongs to, so the ladder can ask the
	// question the tri-factor is blind to: is the live pid still serving THIS
	// row, or did it hand the number on to another one? It is not a factor of
	// the incarnation — it is the row-scoped evidence applied AFTER the
	// pid-level verdict, by the one rung in taskproc_cwd_row.go (Rule 14).
	// "" = the caller holds no row identity (a legacy record, or a direct
	// caller with no ledger row), and the rung then only REPORTS the cwd.
	CwdRow string
}

// identityProcRoot is the procfs root every identity comparison in this package
// reads, so a test can drive the REAL callers (the resume gate, the reclaim
// disposition, the takeover guard) over a synthetic tree instead of asserting
// against whatever this host happens to be running. Production reads
// procname.DefaultProcRoot — the value is REFERENCED, never mirrored (Rule 13),
// and it is a package variable rather than an environment variable on purpose:
// an env-steered root would let a caller export a path that makes every live
// runner read "gone", which is a fail-OPEN hazard on the destructive reclaim
// path (memory #2632). Nothing outside a test may replace it.
var identityProcRoot = procname.DefaultProcRoot

// identityLiveness is the ONE way this package turns a RECORDED row identity
// into the predicate's input. It exists because the identity callers do not
// share a data shape — the ledger names a holder PID (the execution pid, and the
// run may have re-exec'd or be a wrapper), while a worktree's recorder
// identifies a RUNNER — yet they must all ask about the SAME recorded facts. A
// caller that passed only the pid where another passed a recorded command line
// was how two of them came to answer differently about one row.
//
//	an incarnation recorded (hasStart)  → the tick is the factor; the recorded
//	    command line is a HINT by construction (a runner may replace its image in
//	    place), which procname.Match's precedence enforces inside the predicate —
//	    passing it would be harmless, and NOT passing it keeps the hint out of a
//	    comparison that does not use it.
//	NO incarnation recorded             → the command line is the ONLY factor, so
//	    it is passed as an axis; with neither factor the predicate answers
//	    UNMEASURABLE rather than inventing a death (Rule 15a).
func identityLiveness(pid int, startTicks int64, hasStart bool, cmd string) procname.Liveness {
	l := procname.Liveness{Pid: strconv.Itoa(pid)}
	if hasStart {
		l.StartTicks = startTicks
	} else if cmd != "" {
		l.CmdlineText = cmd
	}
	return l
}

// taskCaptureProcIdentity reads the identity of one LOCAL process. ok=false
// means nothing could be read (the pid is gone, or procfs is unavailable) —
// the caller then records no identity rather than a zero-valued one, which
// would read back as "pid 0 / start 0" and give every probe a false answer.
func taskCaptureProcIdentity(pid int) (taskProcIdentity, bool) {
	if pid <= 0 {
		return taskProcIdentity{}, false
	}
	start, hasStart := procStartTicks(pid)
	cmd := procCmdline(pid)
	if !hasStart && cmd == "" {
		return taskProcIdentity{}, false
	}
	return taskProcIdentity{Pid: pid, Start: start, HasStr: hasStart, Cmd: cmd}, true
}

// procStartTicks reads /proc/<pid>/stat's starttime field. (0,false) =
// unreadable (process gone, procfs absent, unparseable). The command name in
// that file is parenthesised and may itself contain spaces and parentheses,
// so fields are counted after the LAST ')' — the same rule
// finishCleanupProcParent uses, because both DELEGATE to the ONE parse in
// internal/procname (Rules 13/14). The n<=0 guard is kept on this side: the
// parser answers field 22 as the kernel wrote it, and a stored-integer caller
// must still reject a non-positive count rather than record it as an identity.
func procStartTicks(pid int) (int64, bool) {
	raw, ok := procname.StatFieldString(filepath.Join(procRootName, strconv.Itoa(pid), procStatName), procStatStartTimeField)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// procCmdline reads and normalises /proc/<pid>/cmdline: NUL-separated argv
// joined with spaces and truncated to procIdentityCmdlineMax, so the record
// and the comparison see the same string. "" = unreadable or empty (a kernel
// thread) — the caller treats it as "no cmdline evidence", never as a
// mismatch.
func procCmdline(pid int) string {
	return procCmdlineAt(procRootName, pid)
}

// procCmdlineAt is the ONE command-line reader, parameterised by procfs root:
// the CAPTURE path reads the real /proc, and the COMPARISON path reads
// identityProcRoot so a test can drive the real callers over a fixture. One
// implementation means the recorded spelling and the compared spelling cannot
// diverge (a raw capture against a canonical form was this host's measured false
// death proof on 2026-09-17).
func procCmdlineAt(root string, pid int) string {
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), procCmdlineName))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(strings.Join(strings.Split(strings.TrimRight(string(b), "\x00"), "\x00"), " "))
	if len(s) > procIdentityCmdlineMax {
		s = s[:procIdentityCmdlineMax]
	}
	return s
}

// taskIdentityVerdict is the tri-factor judgement of ONE recorded identity
// against the live process table. It is the single implementation of
// "is this pid MY process?" — every verdict consumer (task status, resume,
// the list ladder, the run door, `task show`) calls it through ONE of the two
// faces below, so none of them can disagree about a row:
//
//	taskIdentityVerdict(pid, recorded)  the LADDER itself — takes an
//	                                    identity, returns a taskProcVerdict
//	                                    (State + the evidence Reason)
//	taskRowProcessLiveness(row)         a LEDGER ROW's recorded identity
//	                                    through the ladder — the one place a
//	                                    row without an identity becomes
//	                                    TaskProcessStateNotMeasured instead of
//	                                    a verdict about a pid
//
// TaskProcessLiveness is the exported, documented face over a row ID
// (loads the row, then taskRowProcessLiveness); the row-aware legs inside this
// package (taskHolderVerdictFor, taskRunHolderVerdict, taskInProgressStateNow,
// taskShowIdentityLine) call taskRowProcessLiveness and never re-derive the
// comparison. NOTHING here reads /proc except the three shared readers
// (pidAlive / procStartTicks / procCmdline) — a caller with its own /proc
// probe is the parallel implementation Rule 14 forbids.
//
// Degradation ladder (strongest evidence first):
//
//	pid gone                    → dead      (definite)
//	start ticks differ          → recycled  (definite: the number was reused)
//	start ticks agree           → running   (definite: pid+starttime is unique
//	                                         per process INCARNATION, so the
//	                                         live process IS the recorded one;
//	                                         a changed command line here is
//	                                         the in-place exec case — see the
//	                                         cmdline note below)
//	no start recorded, cmdline
//	  differs                   → recycled  (the weaker evidence is all there is)
//	start+cmdline agree         → running   (definite — the identity IS the
//	                                         evidence, so the name-prefix
//	                                         heuristic is NOT consulted: it
//	                                         exists only as a substitute
//	                                         when no identity was recorded)
//	no identity recorded at all → the name-prefix guard's vote (legacy rows:
//	                              exactly what the ledger said before this
//	                              epic, no stronger and no weaker)
//	unreadable procfs / race    → unknown   (never a fabricated death)
//
// THE CMDLINE IS CORROBORATION, NOT A SEPARATE IDENTITY AXIS (epic
// task-run-singleton-guard, 2026-09-15). The starttime tick IS the incarnation
// — two processes cannot share (pid, starttime) — so when the ticks match, a
// differing command line cannot mean "another process owns the number". It
// means the SAME process replaced its image in place: the classic exec
// optimisation (`/bin/bash -c "sleep 30"` becomes `/bin/sleep 30` under the
// same pid — measured while building the singleton guard's E2E fixture). The
// pre-fix ladder reported that shape as `recycled`, i.e. "the recorded runner
// is dead", for a runner sitting right there — a false "reclaimable" verdict,
// the exact hazard this epic is about. The cmdline keeps its diagnostic value
// only where it is the ONLY factor (a row stamped without a readable stat
// line), which is the case it was designed for.
// taskProcVerdict is one ladder ruling: the verdict plus the evidence line it
// rests on. It is a struct rather than a bare string because every caller
// needs the reason too (the operator-facing note in TaskProcessStatus and the
// two-line render of `task show`), and deriving it a second time at each call
// site is exactly the parallel-implementation drift Rule 14 forbids.
type taskProcVerdict struct {
	// State is one of the TaskProcessState* values (including
	// TaskProcessStateNotMeasured, which only taskRowProcessLiveness
	// produces — the ladder itself always measures something about a pid).
	State string
	// Reason is the one-line evidence chain behind State.
	Reason string
}

// measured reports whether this verdict was OBSERVED. The one value that is
// not is TaskProcessStateNotMeasured — the caller must render that as "no
// evidence", never collapse it into a false (Rule 15b).
func (v taskProcVerdict) measured() bool { return v.State != TaskProcessStateNotMeasured }

func taskIdentityVerdict(pid int, recorded taskProcIdentity) taskProcVerdict {
	if pid <= 0 {
		return taskProcVerdict{State: TaskProcessStateNoHolder, Reason: "the row names no pid — nothing to probe"}
	}
	// WHICH question is asked comes from what the row RECORDED, and every case
	// is answered by procname.RunnerIdentity — the ONE identity comparison in
	// this repo (Rule 14). This function TRANSLATES its outcome into the ledger's
	// vocabulary; it does not compare factors itself.
	if recorded.HasStr {
		// The row recorded the incarnation. The command line is deliberately NOT
		// a factor here: matching starttime ticks ARE the identity (two processes
		// cannot share a (pid, starttime) pair), so a runner that re-exec'd in
		// place — a wrapper becoming its target under the same pid — must still
		// read "running". Reading the cmdline as an axis was this host's measured
		// false "reclaimable" verdict (2026-09-17), and it made a LIVE pty-holding
		// runner look dead.
		outcome, why := procname.RunnerIdentity(identityProcRoot,
			identityLiveness(pid, recorded.Start, recorded.HasStr, recorded.Cmd))
		return taskHolderCwdRung(pid, recorded, taskProcessStateFromOutcome(pid, recorded, outcome, why))
	}
	if recorded.Cmd != "" {
		// No incarnation recorded: the command line is the ONLY factor the row
		// states, and the predicate compares it (through the same ONE
		// comparison) against the live side this package reads from
		// identityProcRoot.
		outcome, why := procname.RunnerIdentity(identityProcRoot,
			identityLiveness(pid, 0, false, recorded.Cmd))
		return taskHolderCwdRung(pid, recorded, taskProcessStateFromOutcome(pid, recorded, outcome, why))
	}
	// No identity on the row (a legacy record): the only remaining evidence is
	// the pre-existing ladder, kept VERBATIM so no row's verdict gets WEAKER
	// than what the ledger already reported. A pid this host can see is GONE is
	// DEAD — the zombie-recovery path this epic's resume gate exists for — and
	// only a LIVE pid that is not a gopi runner is a reused number. (Dropping the
	// pidAlive rung here reported a reaped holder as "recycled", which the
	// epic's own TestTaskStatusDeadForReapedChild caught: it changes what
	// `gopi session resume` is offered.)
	if !pidAlive(pid) {
		return taskProcVerdict{State: TaskProcessStateDead, Reason: fmt.Sprintf("pid %d is gone", pid)}
	}
	if !taskPidIsRunner(pid) {
		return taskProcVerdict{State: TaskProcessStateRecycled,
			Reason: fmt.Sprintf("pid %d is alive but is not a %s* process (no identity on the row)", pid, taskAliveProcessNamePrefix)}
	}
	return taskProcVerdict{State: TaskProcessStateRunning,
		Reason: fmt.Sprintf("pid %d is alive and named %s* (no identity on the row)", pid, taskAliveProcessNamePrefix)}
}

// TaskProcessLiveness is THE exported predicate answering "is this task row's
// recorded process live?" for one row of the tasks ledger.
//
// It exists so that every liveness CALLER asks the same three questions of
// the same three factors and cannot disagree about a row: (a) what did the
// row record (pid + starttime + command line, taskproc's identity columns),
// (b) what does the live process table say, judged by the SINGLE ladder
// taskIdentityVerdict (this function adds NO /proc parsing of its own — it
// reads /proc exclusively through procStartTicks / procCmdline / pidAlive),
// (c) was anything measured at all.
//
// FIVE ANSWERS, AND THE LAST ONE IS NOT "DEAD":
//
//	live = true,  TaskProcessStateRunning    the recorded process is running
//	live = false, TaskProcessStateDead       measured: the recorded pid is gone
//	live = false, TaskProcessStateRecycled   measured: the pid is alive but is
//	                                         NOT the recorded process — the
//	                                         runner is dead, its pid number was
//	                                         reused
//	live = false, TaskProcessStateUnknown    the evidence did not settle it
//	live = false, TaskProcessStateNotMeasured  NOTHING was measured (Rule 15b):
//	                                         the row records no identity, so the
//	                                         ledger has no evidence to compare.
//	                                         This is the answer that must never
//	                                         be rendered as "dead".
//
// The measured flag is the point of the return shape: `measured=false` means
// "no evidence exists", `measured=true && live=false` means "I looked and it
// is not there". A caller that only wants a boolean must still branch on
// measured first — collapsing the two is the silent-false-as-dead failure the
// epic forbids.
//
// reason names the evidence chain (or the missing evidence) in one line, so
// the caller can report WHY without re-deriving it — the same discipline
// TaskProcessStatus's Note follows.
//
// The row is loaded once (taskByID) and only its RECORDED identity is judged;
// the claim stamp / freshness / foreign-host layers stay in
// taskProcessStatus, which is a different question ("who claims this row?")
// and is deliberately NOT duplicated here (Rule 14: one implementation per
// question).
func TaskProcessLiveness(taskID string) (live bool, verdict string, measured bool, reason string, err error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return false, TaskProcessStateUnknown, false, "", fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false, TaskProcessStateUnknown, false, "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return false, TaskProcessStateUnknown, false, "", fmt.Errorf("task: liveness: %w", err)
	}
	if t == nil {
		return false, TaskProcessStateUnknown, false, "", fmt.Errorf("task: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	v := taskRowProcessLiveness(t)
	return v.State == TaskProcessStateRunning, v.State, v.measured(), v.Reason, nil
}

// taskRowProcessLiveness judges an already-loaded row through the single
// ladder. It is the row-image half of TaskProcessLiveness (the list renderer
// and the tests hold a row and must not re-read the DB).
func taskRowProcessLiveness(t *Task) taskProcVerdict {
	if t == nil {
		return taskProcVerdict{State: TaskProcessStateNotMeasured, Reason: "no row"}
	}
	// THE HOST COMES FIRST, because it decides whether ANY /proc reading below
	// is a reading of the right machine at all. A stamp minted on another host
	// names a pid in that kernel's namespace — this host's pid N is an unrelated
	// process, so the ladder's "running" would be a stranger and its "dead"
	// would be a verdict about a run that may be alive over there. The honest
	// answer is UNKNOWN, and it is the same answer the status view gives
	// (taskHolderVerdictFor), so the two faces can never disagree about a row
	// stamped elsewhere.
	if !claimHostIsLocalOrUnstamped(t.ClaimedBy) {
		return taskProcVerdict{State: TaskProcessStateUnknown, Reason: taskForeignHolderNote(t.ClaimedBy)}
	}
	if !t.HasProcIdentity || t.ProcPid <= 0 {
		return taskProcVerdict{State: TaskProcessStateNotMeasured,
			Reason: "the row records no process identity (pid/starttime/cmdline) — nothing to measure"}
	}
	return taskIdentityVerdict(t.ProcPid, taskRecordedProcIdentity(t))
}

// taskRecordedProcIdentity is the ONE extraction of a row's recorded identity
// triple into the ladder's input shape (Rule 14). HasStr is derived from the
// recorded start tick, never from the row's HasProcIdentity flag: a row may
// carry a pid + cmdline with no readable start line, and that weaker record
// must reach the ladder as exactly that (the ladder has a documented rung for
// it), not be silently promoted to a full triple.
func taskRecordedProcIdentity(t *Task) taskProcIdentity {
	return taskProcIdentity{
		Pid:    t.ProcPid,
		Start:  t.ProcStartTicks,
		HasStr: t.ProcStartTicks > 0,
		Cmd:    t.ProcCmd,
		CwdRow: sessionContainerRowOfTask(t),
	}
}

// sessionContainerRowOfTask is the ROW half of the recorded identity: the id
// whose session container (and whose ground directory) this run's process stands
// in. Both layouts name the directory after the row id, and the ledger's own
// id column is the authority for WHICH id that is — never a path.
//
// The lookup has one fallback and it is keyed on a measured shape rather than
// on a guess: a row whose directory carries a minted ordinal suffix
// (`<id>-c1`, `<id>-r1`) is a CLONE/REISSUE whose id column names the PARENT
// (internal/tools' taskCloneRecord/clone path records the parent id while the
// child container is named after the suffixed id). When the bare id yields no
// existing session container but `<id>-c1` does, the suffixed id IS this run's
// row and is what the cwd must name. A row with neither has no row identity to
// compare and reads as "" — the rung then reports the cwd instead of judging
// with it (Rule 15: an unmeasured comparison is stated, never assumed).
func sessionContainerRowOfTask(t *Task) string {
	if t == nil {
		return ""
	}
	id := taskIDNormalize(t.TaskID)
	if id == "" {
		return ""
	}
	if taskSessionDirAddress(id) != "" {
		return id
	}
	cloneID := fmt.Sprintf(TaskCloneSuccessorIDFmt, id, taskCloneFirstOrdinal)
	if taskSessionDirAddress(cloneID) != "" {
		return cloneID
	}
	return ""
}

// taskProcessStateFromOutcome translates procname's identity outcome into the
// ledger verdict vocabulary AND pairs it with the evidence line the operator
// reads — one function, so the machine verdict and the sentence cannot drift.
// "Dead" and "unknown" are DISTINCT values on purpose: an unmeasurable factor
// must never be reported as a death (Rule 15a), because "dead" is exactly the
// verdict that authorises a reclaim; the unmeasurable reason is NAMED in the
// sentence rather than flattened into a bare "could not be read".
func taskProcessStateFromOutcome(pid int, recorded taskProcIdentity, outcome procname.Outcome, why procname.UnmeasurableReason) taskProcVerdict {
	switch outcome {
	case procname.OutcomeLive:
		if recorded.HasStr {
			return taskProcVerdict{State: TaskProcessStateRunning,
				Reason: fmt.Sprintf("pid %d is alive and matches the recorded start ticks (%d)", pid, recorded.Start)}
		}
		return taskProcVerdict{State: TaskProcessStateRunning,
			Reason: fmt.Sprintf("pid %d is alive and matches the recorded command line (no start tick on the row)", pid)}
	case procname.OutcomeGone:
		return taskProcVerdict{State: TaskProcessStateDead,
			Reason: fmt.Sprintf("pid %d is gone — no process directory at that pid, or a terminated-but-unreaped zombie", pid)}
	case procname.OutcomeRecycled:
		if recorded.HasStr {
			return taskProcVerdict{State: TaskProcessStateRecycled,
				Reason: fmt.Sprintf("pid %d was REUSED: recorded start ticks %d are not the live ones — the runner is dead, the number now belongs to another process", pid, recorded.Start)}
		}
		return taskProcVerdict{State: TaskProcessStateRecycled,
			Reason: fmt.Sprintf("pid %d is alive but its command line is not the recorded one (no start tick on the row)", pid)}
	default:
		return taskProcVerdict{State: TaskProcessStateUnknown,
			Reason: fmt.Sprintf("pid %d is alive but its identity could not be measured (%s) — an unmeasured factor is never a death", pid, why)}
	}
}

// TaskProcessStatus is the machine-readable answer `gopi task status` prints
// (and the struct `gopi session resume` gates on): the row's ledger status, the
// tri-factor verdict for its holder process, and every raw fact the verdict
// rests on — so an operator can audit the decision instead of trusting a
// label (Rule 15: no silent verdict).
type TaskProcessStatus struct {
	TaskID    string `json:"task_id"`
	Summary   string `json:"summary,omitempty"`
	Status    string `json:"status"`
	Verdict   string `json:"verdict"`
	Note      string `json:"note,omitempty"`
	Resumable bool   `json:"resumable"`

	// RunEndExitCode / RunEndReason are the RUN'S OWN account of how it ended,
	// read from its session log — the corroboration the `dead` verdict needs
	// (see taskproc_runend.go). They are pointers so "the log does not record
	// it" (nil) is distinguishable from "it recorded 0" — the same Rule 15a
	// argument the ground facts above make about false.
	RunEndExitCode *int   `json:"run_end_exit_code,omitempty"`
	RunEndReason   string `json:"run_end_reason,omitempty"`

	// Holder facts: the claim stamp (host:pid) and its freshness against the
	// row's in_progress_at referent.
	Holder       string  `json:"holder,omitempty"`
	HolderPid    int     `json:"holder_pid,omitempty"`
	Freshness    string  `json:"holder_freshness,omitempty"`
	Foreign      bool    `json:"holder_foreign,omitempty"`
	InProgressAt float64 `json:"in_progress_at,omitempty"`

	// Identity facts: what the row RECORDED vs what the OS reports now.
	IdentityRecorded bool   `json:"identity_recorded"`
	RecordedPid      int    `json:"recorded_pid,omitempty"`
	RecordedStart    int64  `json:"recorded_start_ticks,omitempty"`
	RecordedStartAt  string `json:"recorded_start_at,omitempty"`
	RecordedCmd      string `json:"recorded_cmd,omitempty"`
	LiveAlive        bool   `json:"live_alive"`
	LiveStartTicks   int64  `json:"live_start_ticks,omitempty"`
	LiveStartAt      string `json:"live_start_at,omitempty"`
	LiveCmd          string `json:"live_cmd,omitempty"`

	// JudgedPid / JudgedPidRole / JudgedPidWhy name WHICH pid the verdict was
	// measured against, and why that one (bug epic 01a1b5bb). They exist
	// because a row can carry TWO pids — the claim stamp's and the recorded
	// executor's — and this host's worker rows normally do: the stamp names the
	// `gopi worker` PARENT, the identity triple names the CHILD it spawned. A
	// verdict that named neither was read as "the evidence does not settle it"
	// about a run that was measurably working (4/4 live rows, 2026-11-07).
	//
	// JudgedPid == 0 means the verdict rests on nothing measurable (a foreign /
	// stale / malformed stamp), and the renderer then prints NO judged line
	// rather than inventing a subject. JudgedPidRole says which of the two pids
	// it was; JudgedPidWhy is the sentence naming BOTH numbers, filled only when
	// they differ.
	JudgedPid     int    `json:"judged_pid,omitempty"`
	JudgedPidRole string `json:"judged_pid_role,omitempty"`
	JudgedPidWhy  string `json:"judged_pid_why,omitempty"`

	// Executor* is the RECORDED EXECUTOR's own /proc reading, filled only when
	// the verdict above was measured against it (i.e. the two pids differ —
	// see JudgedPidRole). They are separate fields from the Live* block because
	// that block is about the CLAIM STAMP's pid, and a view that mixed the two
	// would print one process's numbers under the other's name.
	ExecutorAlive     bool   `json:"executor_alive,omitempty"`
	ExecutorStartAt   string `json:"executor_start_at,omitempty"`
	ExecutorStartTick int64  `json:"executor_start_ticks,omitempty"`
	// ExecutorCwdNote is the cwd-row evidence for that same pid, worded by the
	// ONE reading (taskHolderCwdReading) — "the process stands in THIS row's
	// directory" is what makes the identity match a statement about the WORK
	// rather than about a number.
	ExecutorCwdNote string `json:"executor_cwd_note,omitempty"`

	// Cwd facts: WHICH ROW the live process is standing in, measured from
	// /proc/<pid>/cwd (taskproc_cwd_row.go). They are printed beside `live:`
	// because they answer the question the identity triple cannot: two runs of
	// one pid are told apart by the row they serve, not by the incarnation.
	// CwdRowCompared is false when the row records no row identity OR the cwd
	// could not be read — the distinction between "checked and consistent" and
	// "nothing to check" must survive to the reader (Rule 15b), which is why the
	// note carries the reading either way.
	CwdSeen     string `json:"cwd,omitempty"`
	CwdRow      string `json:"cwd_row,omitempty"`
	CwdRecorded string `json:"cwd_recorded_row,omitempty"`
	// CwdNote is the ONE evidence sentence for this reading (agrees /
	// names another row / names no row / could not be read / nothing to compare
	// against), built by taskHolderCwdReading so the view and the ladder cannot
	// word the same fact differently.
	CwdNote string `json:"cwd_note,omitempty"`

	// SessionID is the row's session (the resume input when the resume is
	// needed) — resolved by the caller, not here.
	SessionID string `json:"session_id,omitempty"`

	// SessionIsFiler says that SessionID names the session that FILED this row
	// and therefore is NOT a run of it. The two facts share one field because
	// the ledger has only one column to answer "which session belongs to this
	// row" (taskProcessSessionOf: the row's own run session first, the filer as
	// the fallback), and a reader that treats the fallback as a RUN address
	// continues somebody else's conversation.
	//
	// IT EXISTS BECAUSE THAT READER WAS REAL (measured 2026-11-01 on this host,
	// row 01a19075-80b5-7a85-b0df-02a55f1c8b16). The row never ran: its
	// directory held metadata.json + output.log and NO run log, and its
	// session_id column was EMPTY with submitter_session naming the chat that
	// filed it. TaskResumeTarget's legacy fallback took SessionID (the filer),
	// resolved the filer's checkpoint, and launched `gopi session resume` onto
	// ANOTHER CONVERSATION — which then wrote this row's terminal status from
	// its own run's statistics (the two `completed` events at 12:51:49/50
	// carry the FILER session's session_end digest, elapsed=28.5s iter=9
	// tokens=541766, and the row's own directory never gained a run log). A
	// recovery must land on the row's own work or not run at all, so the
	// fallback is now refused by name rather than silently substituted.
	SessionIsFiler bool `json:"session_is_filer,omitempty"`

	// Ground facts: the row's RECORDED ground (deliverable.worktree) and
	// whether a live process stands in it RIGHT NOW. They are here because the
	// LEDGER is not the only thing that can hold a row: the recorded directory
	// is written by a different party than the holder columns, so a row can
	// read holderless and still be occupied (measured 2026-10-07, row
	// 01a11561: a resume on a `failed` row left `holder (none)` while its run
	// worked on in that directory). GroundInUse is the fact that makes
	// `resumable` honest; GroundHolder/GroundReason carry the same evidence a
	// refused sweep prints, so one reading explains both surfaces (Rule 14).
	//
	// GroundInUse carries NO omitempty, like its sibling required facts
	// (IdentityRecorded, LiveAlive): false IS the semantic value ("the ground is
	// free, a resume may proceed"), so omitting it at the zero value would erase
	// the answer a machine consumer came for — measured on the wire
	// 2026-10-07, where the free case simply had no `ground_in_use` key. A row
	// that records NO ground reads as this pair: ground_worktree absent +
	// ground_in_use false (nothing recorded, therefore nothing occupied).
	// GroundWorktree stays omitempty: an absent ground is genuinely optional.
	Ground      string `json:"ground_worktree,omitempty"`
	GroundInUse bool   `json:"ground_in_use"`

	GroundHolder string `json:"ground_holder,omitempty"`
	GroundReason string `json:"ground_reason,omitempty"`

	// GroundFacts is the ground's MEASURED summary — its SHAPE (clone vs linked
	// worktree, from devworkspace.GroundModelOf, the one discriminator) and its
	// rebase state (from `git rev-parse --git-path`, so the question is asked
	// the same way on both shapes). It is rendered by the ONE renderer,
	// devworkspace.GroundFactsLine, so this view can never disagree with the
	// discriminator (Rules 13/14).
	//
	// WHY A FIELD AND NOT PROSE IN THE REPORT (measured 2026-10-27, row
	// 01a17a48): a summary of a ground once said, in ONE sentence, that it was
	// "a LINKED WORKTREE (.git is a FILE)" AND that there was "No rebase in
	// progress (no .git/rebase-merge|apply)" — the first clause read off a
	// FAILED probe, the second unsound on the shape the first claimed. Rule 6c
	// gives those two shapes OPPOSITE delivery instructions, so that sentence
	// decided whether a run published its commit. A claim about the shape must
	// be RENDERED from the measured shape, never re-derived by a second reader.
	//
	// IT IS FILLED ONLY BY THE ROW-SCOPED VERB (TaskProcessStatusFor), not by
	// the list renderer: measuring it costs two git subprocesses, and `task
	// list` renders every row of the ledger. A list row therefore reports the
	// fact as absent, which is honest — see taskProcessStatusGroundFacts.
	//
	// omitempty is correct (Rule 15a): a row that records NO ground has no
	// facts to state, and no consumer validates its presence.
	GroundFacts string `json:"ground_facts,omitempty"`

	// Progress is THE PROGRESS AXIS (taskprogress.go, bug epic 01a1ac0f):
	// whether the process holding this row is DOING anything, as distinct from
	// whether it exists. Measured ONLY on the row-scoped verb (the ground-facts
	// form of this struct), never by the list renderer, because it reads the
	// run's own transcript and the list is O(rows) with no session I/O.
	//
	// WHY IT IS NOT FOLDED INTO Verdict: "the pid is alive" and "the run is
	// working" are two questions, and the whole epic exists because one was
	// answered as the other. Measured on this host 2026-11-05, hours apart, with
	// the SAME refusal sentence: holder 518745 (up 5h18m, 24s of CPU, all 36
	// threads in futex, transcript silent since 16:57:18) was NOT working, while
	// holder 2680836 (up 2m25s) was. A struct carrying one word for both could
	// not have said that.
	//
	// It is a VALUE, not a pointer: the zero TaskProgress has an empty State
	// that no renderer prints, so a consumer that never asked sees nothing
	// rather than a fabricated "running".
	Progress TaskProgress `json:"progress,omitempty"`

	// RunLock* are the run directory's KERNEL FLOCK - the only verdict about
	// whether a process is executing that directory (internal/runlock rule 1).
	// RunLockState is one of runlock.LockStateFree / Held / Unmeasured, and
	// RunLockHolderPid names the holder when held.
	//
	// It is separate from LockStamp below ON PURPOSE, and keeping them apart is
	// this epic's point: the flock cannot outlive its holder, while the STAMP
	// outlives its writer by construction (the file is never deleted). A reader
	// who sees a pid in the stamp must be able to see, on the next line, that
	// nobody holds the lock.
	RunLockState string `json:"run_lock_state,omitempty"`
	RunLockPid   int    `json:"run_lock_pid,omitempty"`
	RunLockWhy   string `json:"run_lock_why,omitempty"`
	RunLockPath  string `json:"run_lock_path,omitempty"`

	// LockStamp is the lock file's CONTENT as the last holder wrote it, rendered
	// as DIAGNOSIS and labelled as such at the render site. It is evidence about
	// who held the directory ONCE, never evidence that anything holds it now.
	LockStamp string `json:"lock_stamp,omitempty"`

	// StatusRefusal carries the STATUS half of the resume gate: when the row's
	// ledger status says the WORK IS FINISHED (a success or retired terminal),
	// this names the status, says why a resume is wrong for it, and prints the
	// documented way forward. Resumable is false in exactly that case.
	// `failed` is NOT one of them: a failure judges one attempt, and the fix is
	// to resume the same row (level 1) before copying it (level 2).
	//
	// WHY IT IS A FIELD AND NOT PROSE IN Note: the holder VERDICT and the
	// STATUS answer two different questions, and both must stay readable at
	// once — the same reason the ground axis got its own fields (measured
	// 2026-10-07). A `completed` row with a provably dead holder is the shape
	// that made the two questions look like one: verdict dead, status refused.
	//
	// omitempty is correct here (Rule 15a): the value is genuinely optional —
	// an admitted row has nothing to refuse — and no consumer validates its
	// presence.
	StatusRefusal string `json:"status_refusal,omitempty"`

	// StatusCaveat carries the OTHER half of the status axis: a status the gate
	// ADMITS whose admission has a condition the operator must check first.
	// Today exactly one status has one — `cancelled`, which is also how a row is
	// closed when its work was handed to another node (resuming that one here is
	// a cross-host double run) — and the sentence names the command that answers
	// it. It is a separate field from StatusRefusal for the obvious reason: a
	// caveat rides an ADMITTED row (resumable stays true), a refusal replaces one.
	//
	// omitempty is correct (Rule 15a): most admitted rows carry no condition.
	StatusCaveat string `json:"status_caveat,omitempty"`

	// DeliveryVerdict / DeliveryWhy carry the SECOND axis of the resume gate —
	// whether the work this row produced ever reached the integration branch
	// (task_resume_delivery_axis.go). They are filled only for a row whose
	// status CLAIMS finished work (completed/verified/superseded), because no
	// other row's resume decision depends on the answer.
	//
	// WHY THEY EXIST AS FIELDS: Resumable and StatusCaveat already carry the
	// decision, and a decision is not its own evidence. A `completed` row that
	// reads `resumable: yes` is a SURPRISING answer — the epic that added this
	// axis exists because that answer was wrong for the whole population — so
	// the row has to state which way the axis went and what was read to get
	// there, including the UNKNOWN case, whose refusal would otherwise be
	// indistinguishable from a measurement.
	//
	// omitempty is correct (Rule 15a): a row the axis has no say about carries
	// neither field, and no consumer validates their presence.
	DeliveryVerdict string `json:"delivery_verdict,omitempty"`
	DeliveryWhy     string `json:"delivery_why,omitempty"`
}

// TaskProcessStatusFor resolves the tri-factor verdict for one task row —
// the engine half of `gopi task status` (the CLI only renders it) and the
// gate `gopi session resume` asks before it runs. An absent row is an error, never an
// empty verdict (Rule 15).
func TaskProcessStatusFor(taskID string) (*TaskProcessStatus, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return nil, fmt.Errorf("task: status: %w", err)
	}
	if t == nil {
		return nil, fmt.Errorf("task: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	return taskProcessStatusWithGroundFacts(t), nil
}

// taskProcessStatusWithGroundFacts is taskProcessStatus PLUS the ground's
// measured summary — the row-scoped verb's form. It lives here rather than
// inside taskProcessStatus because MEASURING costs two git subprocesses and the
// list renderer builds one of these per ledger row; the list keeps the cheap
// form and its ground facts are simply absent (omitempty), which is an honest
// "not measured" rather than a guessed one.
//
// It is ALSO where the two run-directory axes are measured (bug epic
// 01a1ac0f): the PROGRESS axis (taskprogress.go) and the run directory's
// KERNEL FLOCK plus the lock file's stamp (runlock.LiveHolderOf). Both are
// row-scoped only, for the same cost argument — the progress axis stats the
// run's transcript and the lock axis reads /proc/locks, so neither belongs on a
// renderer that builds one struct per ledger row.
func taskProcessStatusWithGroundFacts(t *Task) *TaskProcessStatus {
	st := taskProcessStatus(t)
	st.GroundFacts = taskGroundFactsForStatus(st.Ground)
	taskProcessStatusRunDirAxes(t, st)
	return st
}

// taskProcessStatusRunDirAxes fills the two facts about the row's RUN
// DIRECTORY that the holder ladder cannot see, each stated as its own line:
//
//  1. PROGRESS — is the process holding this row DOING anything? Read from the
//     run's own transcript, through the ONE function (taskProgressAxisFor).
//     Measured for any row with a holder pid, whatever the sub-verdict: a `dead`
//     holder has no progress to measure, but a `running` one may be wedged, and
//     `running` is exactly the verdict this axis exists to split.
//  2. THE LOCK — the kernel's flock (the VERDICT) and the stamp inside the file
//     (the DIAGNOSIS), read as two facts and stored as two fields, so no
//     renderer can print them as one.
//
// A run directory that cannot be resolved fills NEITHER field and says nothing:
// a row with no run directory has no lock and no transcript, and inventing a
// reading for it would be the silent degradation Rule 15 forbids. The `run_dir:`
// line already states that absence.
func taskProcessStatusRunDirAxes(t *Task, st *TaskProcessStatus) {
	if t == nil || st == nil {
		return
	}
	if st.HolderPid > 0 {
		st.Progress = taskProgressAxisFor(t, st.HolderPid, time.Now())
	}
	dir := taskRunDir(t.TaskID)
	if dir == "" {
		return
	}
	path := runlock.PathIn(dir)
	live := runlock.LiveHolderOf(path)
	st.RunLockPath = live.Path
	st.RunLockState = live.State
	st.RunLockPid = live.Pid
	st.RunLockWhy = live.Why
	st.LockStamp = runlock.StampOf(path)
}

// taskGroundFactsForStatus renders the ground's summary through the ONE
// renderer (devworkspace.GroundFactsLine → GroundModelOf for the shape,
// `git rev-parse --git-path` for the rebase state). A row that records no ground
// has nothing to summarise and gets "" — never a sentence about the current
// working directory, which is not the row's ground.
func taskGroundFactsForStatus(ground string) string {
	if strings.TrimSpace(ground) == "" {
		return ""
	}
	return devworkspace.GroundFactsLine(ground)
}

// taskProcessSessionOf answers the session this row's recovery should address:
// its RUN session when it has one, and the session that FILED it otherwise. The
// two are different columns since 2026-10-28; this accessor is the one place the
// resume path's "which session belongs to this row" question is answered, so the
// fallback cannot drift between callers.
//
// THE FALLBACK IS NOT A RUN, and taskProcessSessionIsFiler is how a caller tells
// the two apart: the value is the one that made a resume continue a stranger's
// conversation (see SessionIsFiler on TaskProcessStatus).
func taskProcessSessionOf(t *Task) string {
	if t.SessionID != "" {
		return t.SessionID
	}
	return taskSubmitterSessionOf(t)
}

// taskProcessSessionIsFiler reports whether taskProcessSessionOf answered with
// the FILER's session rather than one of this row's own runs. It is a separate
// accessor so the two callers cannot disagree about which case they are in: the
// status view records the flag, and TaskResumeTarget refuses the fallback.
func taskProcessSessionIsFiler(t *Task) bool {
	if t == nil {
		return false
	}
	if strings.TrimSpace(t.SessionID) != "" {
		return false
	}
	return taskSubmitterSessionOf(t) != ""
}

// taskProcessStatus builds the verdict for an already-loaded row (the list
// renderer and the sync path hold a row image and must not re-read the DB).
func taskProcessStatus(t *Task) *TaskProcessStatus {
	now := time.Now()
	st := &TaskProcessStatus{
		TaskID:  t.TaskID,
		Summary: t.Summary,
		Status:  t.Status,
		// THE ROW'S RUN SESSION, and the FILER as the fallback (2026-10-28).
		// This field is the resume input ("what session belongs to this row"),
		// and the two facts it may answer with are now separate columns. A row
		// that ran answers with its run session; a row that never started has
		// none (the cell is empty by construction now), and the only transcript
		// that exists for it is the filer's — which is what this field held for
		// such a row before the split, when the filer sat in session_id. Reading
		// it through taskSubmitterSessionOf keeps that behaviour instead of
		// turning every unstarted row's resume target into "".
		SessionID:      taskProcessSessionOf(t),
		SessionIsFiler: taskProcessSessionIsFiler(t),

		Freshness:    claimStampVerdict(t),
		InProgressAt: t.InProgressAt,
	}
	st.IdentityRecorded = t.HasProcIdentity
	st.RecordedPid = t.ProcPid
	st.RecordedStart = t.ProcStartTicks
	st.RecordedCmd = t.ProcCmd
	if t.ProcStartTicks > 0 {
		st.RecordedStartAt = procStartTimeDisplay(t.ProcStartTicks)
	}
	// A row with no holder process to judge is n/a and resumable (nothing can
	// double-run). "Has a holder" is CLAIMED or in_progress, not in_progress
	// alone (epic task-run-singleton-guard, 2026-09-15): the claimed window is
	// precisely where a second runner's claim CAS can be won twice, so the
	// operator's inspection verb must judge a live claim-holder there instead
	// of answering n/a (which read as "nothing to worry about" for the one
	// window that needed the warning). A row on neither status has no holder.
	if !taskRunRowIsLive(t.Status) {
		st.Verdict = taskProcessStateNotApplicable
		st.Note = "row is " + t.Status + " — no holder to verify"
		// The holder axis has nothing to refuse here, so the row starts from
		// "no process can double-run me" and each LATER axis may only make the
		// answer stricter: the status axis (judged work) below, then the ground
		// axis (an occupied directory).
		st.Resumable = true
		taskProcessStatusStatusAxis(t, st)
		taskProcessStatusGroundAxis(t, st)
		return st
	}
	host, pid, local := claimHolder(t.ClaimedBy)
	_ = host
	st.Holder = t.ClaimedBy
	st.HolderPid = pid
	if !local {
		if strings.TrimSpace(t.ClaimedBy) == "" {
			st.Verdict = TaskProcessStateNoHolder
			st.Note = "no claim stamp on the row — nothing names an executor"
		} else if claimHostIsLocal(t.ClaimedBy) {
			// A local host with a malformed pid part: unjudgeable.
			st.Verdict = TaskProcessStateUnknown
			st.Note = "claim stamp is malformed (" + t.ClaimedBy + ")"
		} else {
			// THE HOST DIMENSION. The verdict is UNKNOWN, not `foreign`, and
			// not `running`/`dead`: this host measured nothing about that pid,
			// and a verdict must name what was MEASURED (the vocabulary's own
			// contract). `foreign` is retained as the stamp's CLASSIFICATION —
			// it rides the JSON flag below, which is what a consumer branches
			// on — while the VERDICT stays the one honest word for "not
			// measurable here", the same word a stale stamp gets.
			st.Verdict = TaskProcessStateUnknown
			st.Foreign = true
			st.Note = taskForeignHolderNote(t.ClaimedBy)
		}
	} else if t.FreshnessStale() {
		// The stamp predates this row's in_progress stretch: it names a
		// PREVIOUS claimant, so the pid says nothing about the current run.
		st.Verdict = TaskProcessStateUnknown
		st.Note = taskStateUnknownStaleClaim + " — the pid is not this run's executor"
	} else {
		verdict, judged := taskHolderVerdictFor(t, pid)
		st.Verdict = verdict
		st.JudgedPid = judged.Pid
		st.JudgedPidRole = judged.Role
		st.JudgedPidWhy = judged.Why
		st.LiveAlive = pidAlive(pid)
		st.LiveStartTicks, _ = procStartTicks(pid)
		if st.LiveStartTicks > 0 {
			st.LiveStartAt = procStartTimeDisplay(st.LiveStartTicks)
		}
		st.LiveCmd = procCmdline(pid)
		// THE ROW-SCOPED EVIDENCE, as its own MEASURED fields (not prose): the
		// status view must be able to say which row the pid serves without
		// re-reading /proc with a second reader (Rule 14). The ladder already
		// consulted exactly this reading through taskIdentityVerdict, so the
		// fields and the verdict can never disagree.
		//
		// IT IS RESOLVED BEFORE THE EXECUTOR BLOCK BELOW, because that block
		// reads the SAME row identity to compare the executor's cwd against it
		// (a reading taken before this line would say "the row records no row
		// identity" about a row that does — measured on this fix's own first
		// run, 2026-11-07).
		st.CwdRecorded = sessionContainerRowOfTask(t)
		// THE RECORDED EXECUTOR'S OWN READING, when the verdict was measured
		// against it rather than against the stamp (bug epic 01a1b5bb). The
		// `live:` line above stays about the STAMP's pid — that is what the
		// holder column names — and these fields carry the measurement the
		// verdict actually rests on, so a reader of `verdict: running` can see
		// the process it is talking about. Filled only when the two pids
		// DIFFER: on a same-pid row the two readings would be the same one
		// printed twice, which reads as a second, independent confirmation.
		if st.JudgedPidRole == taskJudgedPidRoleRecordedExecutor {
			st.ExecutorAlive = pidAlive(st.JudgedPid)
			st.ExecutorStartTick, _ = procStartTicks(st.JudgedPid)
			if st.ExecutorStartTick > 0 {
				st.ExecutorStartAt = procStartTimeDisplay(st.ExecutorStartTick)
			}
			st.ExecutorCwdNote = taskHolderCwdReading(st.JudgedPid, st.CwdRecorded).Note
		}
		if st.LiveAlive && st.IdentityRecorded && st.CwdRecorded != "" {
			cwdReading := taskHolderCwdReading(pid, st.CwdRecorded)
			st.CwdSeen = cwdReading.Dir
			st.CwdRow = cwdReading.Row
			st.CwdNote = cwdReading.Note
		}
		st.Note = taskProcessVerdictNote(st)
		// THE RUN'S OWN ACCOUNT, read only where it can change the answer: a
		// `dead` holder is the one verdict whose CAUSE the ladder never
		// measured (taskproc_runend.go). Reading it here — not for every row —
		// keeps `task list` O(rows) with no session I/O on the healthy path.
		if st.Verdict == TaskProcessStateDead {
			end := taskRunEndOf(t.TaskID, t.SessionID)
			if end.Known {
				code := end.ExitCode
				st.RunEndExitCode = &code
				st.RunEndReason = end.Reason
			}
			st.Note = taskProcessDeadNote(pid, end)
		}
	}
	st.Resumable = taskVerdictResumable(st.Verdict)
	// THE HOST ARM, and it is the reason the verdict above could stay honest:
	// a stamp minted on ANOTHER host yields `unknown` (this host measured
	// nothing about that pid), and `unknown` alone does NOT refuse a resume —
	// it must not, because a STALE local stamp yields the same word and that is
	// the crash-recovery path. Here the reason is knowable (the flag), so the
	// refusal is stated exactly where the fact is: the run may be alive over
	// there, and nothing on this machine can say it is not.
	if st.Foreign {
		st.Resumable = false
	}
	taskProcessStatusStatusAxis(t, st)
	taskProcessStatusGroundAxis(t, st)
	_ = now
	return st
}

// taskProcessStatusGroundAxis fills the GROUND half of the row's answer: does
// the ledger row's RECORDED ground (deliverable.worktree) hold a live process
// right now? It is asked on EVERY row, whatever the status, because the fact it
// measures is not the status's to know — a terminal row whose run is still
// standing in its ground is exactly the shape the ledger cannot see (see
// task_ground_in_use.go for the measured incident).
//
// ONE EFFECT ON THE VERDICT, and it is deliberately narrow: it may only ever
// make `resumable` MORE conservative. A row the status ladder already calls
// running or foreign stays un-resumable; a row the ladder called FREE (n/a on a
// terminal status, dead, recycled, no-holder) becomes un-resumable when the
// ground it records is occupied — because resuming it would put a SECOND run
// into a directory a first one is working in, which is the double-run the whole
// gate exists to prevent, arriving through the door the ledger does not watch.
//
// The LABELS are untouched: Verdict still reports the ledger's own answer about
// the row's holder (the enum is read by the retry ladder, the resume gate and
// the CLI exit code, and those questions are genuinely different from "is
// somebody in the directory"). The ground axis is stated in its own fields and
// its own report line, so a reader can never mistake one for the other.
// taskProcessStatusStatusAxis fills the STATUS half of the resume answer: does
// this row's ledger status admit a resume AT ALL? It is the same decision
// TaskRunChildResumable takes (via the same function, TaskRunResumeStatusGate
// — Rule 14), surfaced so `gopi task status` and `gopi worker --resume` cannot
// disagree about one row.
//
// ONE EFFECT, and it is to make the answer STRICTER: it can only turn
// `resumable` to false and fill StatusRefusal. It never overrides a verdict the
// holder ladder already refused, and it never turns a refusal into an
// admission. A row whose status is refused reads `resumable: no` WITH the
// status and the way forward printed, while the holder fields still report
// what the ledger measured (verdict dead, holder none) — both facts, distinct.
func taskProcessStatusStatusAxis(t *Task, st *TaskProcessStatus) {
	if t == nil || st == nil {
		return
	}
	if admitted, refusal := TaskRunResumeStatusGate(t.TaskID, t.Status); !admitted {
		// THE STATUS SAYS "FINISHED" — and that is a claim about the ROW, not
		// about the WORK. The second axis measures the work's delivery, and it
		// is consulted ONLY here: a status the gate already admits has no
		// finished-work claim to override, and paying two git subprocesses per
		// admitted row to confirm what nobody disputed would be cost with no
		// decision hanging on it.
		//
		// AND ONLY FOR A STATUS THAT CLAIMS THIS ROW'S OWN DELIVERY
		// (taskResumeDeliveryStatusMayBeOverridden): `superseded` means the row
		// was retired IN FAVOUR OF A SUCCESSOR, so its work lives in another
		// row's delivery and overriding it here would run the same work twice
		// under two ids. An unlanded tip on a superseded row is the NORMAL shape
		// of a reissue, not evidence of owed work.
		if d := taskProcessStatusDeliveryAxis(t, st); taskResumeDeliveryAdmits(t.Status, d) {
			// EVIDENCE of owed work: the row's own tip is contained in no
			// integration ref the recorded repo holds. Admit, quoting the
			// status that was overridden and the measurement that overrode it.
			st.Resumable = true
			st.StatusCaveat = taskResumeDeliveryOverrideCaveat(t.Status, d)
			return
		}
		st.Resumable = false
		st.StatusRefusal = refusal
		return
	}
	// ADMITTED — and an admission may still carry a condition the operator has
	// to check before acting on it (Rule 15: an admitted answer that hides its
	// own precondition is a silent failure waiting to happen).
	st.StatusCaveat = taskRunResumeAdmissionCaveat(t.Status)
}

// taskProcessStatusDeliveryAxis measures the DELIVERY axis and records it on the
// status, for the one status it may have a say about (a finished-work status),
// and returns it so the caller branches on the value it just recorded rather
// than on a second reading.
//
// The fields are set BEFORE the branch, on every finished-work row, because the
// UNKNOWN case must be visible: a refusal that rests on an axis the verb could
// not read is a different answer from one that rests on a measurement, and the
// operator cannot tell them apart unless the unreadable axis is printed
// (Rule 15 — the UNKNOWN is loud without being persisted; see this file's
// header for why the ledger is not written from an inspection verb).
func taskProcessStatusDeliveryAxis(t *Task, st *TaskProcessStatus) taskResumeDeliveryVerdict {
	d := taskResumeDeliveryAxis(t)
	st.DeliveryVerdict = d.State
	st.DeliveryWhy = d.Why
	return d
}

func taskProcessStatusGroundAxis(t *Task, st *TaskProcessStatus) {
	if t == nil || st == nil {
		return
	}
	st.Ground = taskDeliverableWorktree(t.Deliverable)
	if st.Ground == "" {
		return // no recorded ground: nothing to occupy, nothing to report
	}
	holder, reason := taskGroundInUseForRow(t.Deliverable)
	if reason == "" {
		return
	}
	st.GroundInUse = true
	st.GroundHolder = holder
	st.GroundReason = reason
	st.Resumable = false
}

// taskProcessStateNotApplicable is the verdict for a row whose status is not
// in_progress: there is no holder to verify (the epic's "definite answer" is
// about in_progress rows; a completed row answering "running" would be
// nonsense).
const taskProcessStateNotApplicable = "n/a"

// taskHolderVerdictFor is the row-aware tri-factor vote: with a recorded
// identity it judges pid+start+cmdline; without one (a row stamped by an
// older build) it falls back to the pre-existing pid-liveness+name guard, so
// no verdict this build reports is weaker than what the ledger said before.
//
// THE HOST IS PART OF THE IDENTITY (and this is the arm that was missing). A
// pid is a number in ONE kernel's namespace: `nick-HP:4242` and this host's
// 4242 are unrelated processes, so every /proc reading of a foreign stamp is a
// reading of a STRANGER — it can say "running" about a pid that happens to be
// alive here (a false double-run refusal) or "dead" about a run that is very
// much alive over there (a wrecked resume: the operator resumes a row another
// machine is executing, and two agents work the same task in two workspaces).
// Neither answer is recoverable after the fact, so the host arm answers the ONLY
// honest verdict for a stamp minted elsewhere: UNKNOWN — "this host cannot
// judge it" — which taskVerdictResumable already treats as NOT resumable. A
// host is named only by the claim stamp's own "host:pid" prefix (the same
// parsing claimHostIsLocal uses), never inferred from a pid.
//
// An EMPTY host prefix is a LOCAL stamp: `taskDefaultClaimHolder` has always
// written "<this host>:<pid>", and the pre-host builds wrote no stamp at all
// (the pid-only fallback below), so a colon-less value is not evidence of a
// foreign machine.
//
// It reads the row's identity through the SAME extraction the exported
// predicate uses (taskRecordedProcIdentity), so this row-aware vote and
// TaskProcessLiveness can never disagree about what the row recorded.
func taskHolderVerdictFor(t *Task, pid int) (string, taskJudgedPidReading) {
	if t != nil {
		if host := claimHostOf(t.ClaimedBy); host != "" && !claimHostIsLocal(t.ClaimedBy) {
			return TaskProcessStateUnknown, taskJudgedPidReading{}
		}
	}
	if t.HasProcIdentity && t.ProcPid == pid {
		return taskIdentityVerdict(pid, taskRecordedProcIdentity(t)).State,
			taskJudgedPidReading{Pid: pid, Role: taskJudgedPidRoleStamp}
	}
	if t.HasProcIdentity && t.ProcPid > 0 && t.ProcPid != pid {
		// THE TWO PIDS DIFFER — AND THAT IS NOT BY ITSELF A REASON TO ABSTAIN
		// (bug epic 01a1b5bb, measured on this host 2026-11-07 on 4/4 live
		// worker rows). The measurement it replaced: every row executed by a
		// `gopi worker` read `verdict: unknown`, `resumable: yes`, exit 3 while
		// its run was measurably working — the stamp's pid was the worker
		// PARENT (cmdline `gopi worker`) and the row's recorded executor was
		// the CHILD it spawned (cmdline `/home/user/work/gopi/bin/gopi`,
		// ppid == the stamp's pid), so `ProcPid != claimStampPid` fired on a
		// perfectly healthy run and the `unknown` fell through
		// taskVerdictResumable's default to `yes`. The operator acting on that
		// yes starts a SECOND executor on the same files.
		//
		// The audit below is the same ladder every other face uses, run against
		// the pid the row RECORDED — its own pid + start ticks (+ the cwd-row
		// rung), never the stamp's — so the answer is a MEASUREMENT of that
		// process and not an opinion about the pair of numbers.
		//
		// IT ONLY EVER MOVES TOWARD SAFETY, and the whitelist stays narrow:
		// exactly ONE state is promoted, `running`, which is the state that
		// REFUSES a resume. Every other outcome of that audit (gone, recycled,
		// unmeasurable) and every case where the record cannot be read at all
		// keeps the pre-existing `unknown` — because "the recorded executor is
		// gone" does NOT prove nobody is executing this row: the stamp names a
		// live process this host cannot tie to the work either.
		v := taskIdentityVerdict(t.ProcPid, taskRecordedProcIdentity(t))
		if v.State != TaskProcessStateRunning {
			return TaskProcessStateUnknown, taskJudgedPidReading{}
		}
		return TaskProcessStateRunning, taskJudgedPidReading{
			Pid:  t.ProcPid,
			Role: taskJudgedPidRoleRecordedExecutor,
			Why:  fmt.Sprintf(taskJudgedPidDifferingWhyFmt, pid, v.Reason),
		}
	}
	return taskIdentityVerdict(pid, taskProcIdentity{Pid: pid}).State,
		taskJudgedPidReading{Pid: pid, Role: taskJudgedPidRoleStamp}
}

// taskJudgedPidRole* name WHICH pid a holder verdict was measured against. The
// pair is what the status view prints and the JSON exposes, because "verdict:
// running" with no subject is the ambiguity this epic is made of: a reader who
// sees two pids on the row must be told which one the answer came from.
const (
	taskJudgedPidRoleStamp            = "claim-stamp"
	taskJudgedPidRoleRecordedExecutor = "recorded-executor"
)

// taskJudgedPidDifferingWhyFmt is the sentence printed when the judged pid is
// NOT the claim stamp's pid. It names both numbers and the measurement that
// settled it, so a reader never has to guess which pid the verdict is about.
// The first verb is the stamp's pid; %s is the ladder's own evidence line for
// the recorded executor.
const taskJudgedPidDifferingWhyFmt = "the claim stamp names pid %d — a differing pid is not by itself " +
	"unjudgeable, so the row's RECORDED executor was measured on its own identity: %s"

// taskJudgedPidReading is the answer to "which pid did this verdict measure, and
// why that one": Pid == 0 means the verdict rests on nothing measurable (the
// foreign / stale / malformed-stamp cases), where the status view prints no
// judged line at all rather than inventing a subject.
type taskJudgedPidReading struct {
	Pid  int
	Role string
	Why  string
}

// claimHostIsLocalOrUnstamped reports whether a claim stamp may be judged ON
// THIS HOST: either it was minted here, or it carries no host prefix at all (a
// colon-less legacy stamp — "no evidence of another machine" is not "another
// machine"). It is the ONE reading both holder faces use, so a foreign stamp is
// never judged by one and skipped by the other.
func claimHostIsLocalOrUnstamped(claimedBy string) bool {
	if claimHostOf(claimedBy) == "" {
		return true
	}
	return claimHostIsLocal(claimedBy)
}

// taskForeignHolderLiveLine is what the `live:` row says when the holder's pid
// belongs to another kernel: NOT MEASURED, and why (Rule 15 — a null reading is
// never rendered as a measurement).
const taskForeignHolderLiveLine = "not measured — the holder pid belongs to another host's kernel"

// taskForeignHolderNote renders the evidence line for a holder whose claim
// stamp was minted on ANOTHER host — the verdict is unknown, and the sentence
// must say WHY the verdict is not "dead" (Rule 15: an unmeasurable factor is
// never reported as a death, because "dead" is the verdict that authorises a
// resume here while the run may be alive over there).
func taskForeignHolderNote(claimedBy string) string {
	return "claim stamp names host " + claimHostOf(claimedBy) +
		" — this host cannot judge that pid; resume it THERE (gopi task diff --peer <alias> shows whether that node's ledger moved it on)"
}

// taskProcessVerdictNote renders the one-line evidence chain behind a
// verdict — what the operator (and the resume refusal message) reads.
func taskProcessVerdictNote(st *TaskProcessStatus) string {
	switch st.Verdict {
	case TaskProcessStateRunning:
		if st.JudgedPidRole == taskJudgedPidRoleRecordedExecutor {
			// THE JUDGED PID IS NOT THE STAMP'S PID, so this line must not name
			// the stamp's (bug epic 01a1b5bb). It names the pid the verdict was
			// measured against, the two readings that matched, and — through the
			// ONE cwd reading — which row that process stands in. Without the
			// subject a reader holding two pids on the same report cannot tell
			// whether `running` is about the worker parent or the executor.
			line := fmt.Sprintf("pid %d (the row's RECORDED executor, not the claim stamp's pid) is alive and matches the recorded identity start=%s, cmdline %q",
				st.JudgedPid, st.RecordedStartAt, st.RecordedCmd)
			if st.ExecutorCwdNote != "" {
				line += "; " + st.ExecutorCwdNote
			}
			// The two-pid explanation is NOT repeated here: it is printed on its
			// own `judged:` line, and a note that restated it made one report
			// say the same fact three times (measured on this fix's first run).
			return line
		}
		if st.IdentityRecorded {
			line := fmt.Sprintf("pid %d is alive and matches the recorded identity (start=%s, cmdline identical)",
				st.HolderPid, st.RecordedStartAt)
			// The row-scoped half of the same answer, WORDED BY THE ONE READING
			// (taskHolderCwdReading's Note), never by a second spelling here: a
			// running verdict that could not check WHICH ROW the pid serves must
			// say so, and one that checked and agreed must say that. Silence
			// would make "the cwd named this row" indistinguishable from "the
			// cwd was never read" — the exact collapse Rule 15b forbids
			// (measured 2026-11-01: the pre-fix line claimed the same identity
			// for a pid that had long moved on to another row).
			if st.CwdNote != "" {
				line += "; " + st.CwdNote
			}
			return line
		}
		return fmt.Sprintf("pid %d is alive and named %s* (no recorded identity on this row — legacy record)", st.HolderPid, taskAliveProcessNamePrefix)
	case TaskProcessStateDead:
		// The ladder measured "the pid is gone" and NOTHING about why, so this
		// line never names a cause: the run's own ending is what says whether
		// the work finished (taskProcessDeadNote / taskproc_runend.go, which
		// overrides this line when the log can be read).
		return fmt.Sprintf("pid %d is gone — the run ended and the row was never closed", st.HolderPid)
	case TaskProcessStateRecycled:
		if st.IdentityRecorded && st.LiveStartTicks > 0 && st.LiveStartTicks != st.RecordedStart {
			return fmt.Sprintf("pid %d was REUSED: recorded start %s ≠ live start %s — the runner is dead, the number now belongs to another process",
				st.HolderPid, st.RecordedStartAt, st.LiveStartAt)
		}
		// THE CWD ARM, and it must be named BEFORE the command-line sentence:
		// this is the verdict the pid-level factors all AGREE about, so a reader
		// that only saw "command line does not match" would be told the wrong
		// reason (the command line matches perfectly — the pid simply serves a
		// different ROW now). taskproc_cwd_row.go is the one place that decides
		// it; this only renders it.
		if st.IdentityRecorded && st.CwdRecorded != "" && st.CwdRow != "" && st.CwdRow != st.CwdRecorded {
			return fmt.Sprintf("pid %d IS the recorded process (pid+starttime+cmdline match) but it is serving ANOTHER row: this row is %s and the process's cwd is %s — the recorded runner handed this row's pid on, so the row is reclaimable",
				st.HolderPid, st.CwdRecorded, st.CwdSeen)
		}
		return fmt.Sprintf("pid %d is alive but is not the recorded runner (command line does not match) — the runner is dead", st.HolderPid)
	case TaskProcessStateUnknown:
		return "the evidence does not settle whether the recorded process is alive"
	default:
		return ""
	}
}

// taskVerdictResumable reports whether a resume may proceed on this verdict:
// everything except a genuinely live holder (double-run) and a foreign host
// (the run may be live over there — this host cannot tell).
//
// UNKNOWN IS DELIBERATELY **NOT** ON THIS LIST, and the distinction is the
// difference between two refusals that look alike. `unknown` in this vocabulary
// is what a STALE claim stamp yields (the pid is not this run's executor) — the
// crash-recovery shape, where the resume must be allowed or nothing recovers a
// dead run. A FOREIGN stamp is a different fact and it is refused by its own arm
// in taskProcessStatus (the `Foreign` flag), not by a blanket "unknown is
// dangerous" rule: widening this switch would strand every crashed row.
//
// `foreign` stays in the switch as the legacy spelling — older builds and peers
// wrote it as a VERDICT, and a reader that meets that word must reach the same
// refusal.
func taskVerdictResumable(verdict string) bool {
	switch verdict {
	case TaskProcessStateRunning, TaskProcessStateForeign:
		return false
	}
	return true
}

// ClaimPidOf is the exported form of claimPidOf: the pid a "hostname:pid"
// claim stamp names (0 when the stamp is empty/malformed), regardless of
// whether the host is this one. Callers that need the LOCAL-only judgement
// plus the freshness axis use TaskProcessStatusFor instead.
func ClaimPidOf(claimedBy string) int { return claimPidOf(claimedBy) }

// claimPidOf extracts the pid from a "hostname:pid" claim stamp; 0 when the
// stamp carries none. Unlike claimHolder it does not filter by host — the
// identity capture path needs the raw number to decide whether it can read
// that process at all.
func claimPidOf(claimedBy string) int {
	s := strings.TrimSpace(claimedBy)
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return 0
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// taskShowLiveWord* are the `task show` render vocabulary for the
// `process:` line's live check — one word per ladder verdict, so the display
// cannot invent a fifth reading and "not-measured" can never be spelled as
// the death word (Rule 15b).
const (
	taskShowLiveAlive       = "alive"
	taskShowLiveGone        = "gone"
	taskShowLiveRecycled    = "alive-but-recycled"
	taskShowLiveUnsettled   = "unsettled"
	taskShowLiveNotMeasured = TaskProcessStateNotMeasured
)

// taskShowIdentityLine renders one `task show` line from the row's recorded
// identity plus the live-side check. An absent record is NAMED (a legacy row
// must not read as "verified fine"), and the live check is included so the
// line answers the epic's question — "is this in_progress row really running?"
// — on its own.
//
// The live word comes from the ONE predicate (taskRowProcessLiveness): this
// line used to re-derive the ladder inline (pidAlive + name guard +
// start-tick comparison), which is how a display and the ledger can disagree
// about a row — the duplication Rule 14 forbids and this epic removes. The
// not-measured reading is rendered as itself, never as "gone".
func taskShowIdentityLine(t *Task) string {
	if !t.HasProcIdentity {
		return procIdentityNone + "  (row predates the process-identity columns; `gopi task status` falls back to the claim stamp)"
	}
	// The live word comes from the raw identity functions (retired in the merge)
	// or from the ONE verdict — never from a second ladder here (Rule 14).
	live := taskShowLiveWord(taskRowProcessLiveness(t).State)
	return fmt.Sprintf("pid=%d start=%s cmd=%q  (live: %s)",
		t.ProcPid, procStartTimeDisplay(t.ProcStartTicks), t.ProcCmd, live)
}

// taskShowLiveWord maps the ladder's verdict onto the display word.
func taskShowLiveWord(state string) string {
	switch state {
	case TaskProcessStateRunning:
		return taskShowLiveAlive
	case TaskProcessStateDead:
		return taskShowLiveGone
	case TaskProcessStateRecycled:
		return taskShowLiveRecycled
	case TaskProcessStateUnknown:
		return taskShowLiveUnsettled
	default:
		return taskShowLiveNotMeasured
	}
}

// The `task show` spelling of one identity verdict lives ONCE, in
// taskShowLiveWord above (Rule 14): the pre-merge branch carried a second
// function of that name, which the merge onto master's vocabulary collapsed
// into this one.

// taskSelfIdentityValues captures THIS process's identity triple as the three
// SQL argument values for the identity columns (NULL/NULL/” when nothing
// could be read — see the Rule-15 note in taskUpdateStamped). Shared by the
// two writers so a row stamped by either path carries the same shape.
func taskSelfIdentityValues() (pid any, start any, cmd any) {
	ident, ok := taskCaptureProcIdentity(os.Getpid())
	if !ok {
		return nil, nil, ""
	}
	return ident.Pid, ident.Start, ident.Cmd
}

// FreshnessStale reports whether the row's claim stamp PREDATES its current
// in_progress stretch — i.e. it names a previous claimant, so its pid says
// nothing about the process executing the row now (claimStampFreshness is the
// single definition; this is the boolean view the verdict builders read).
func (t *Task) FreshnessStale() bool {
	return claimStampFreshness(t) == claimStampStale
}

// TaskProcessStateNotApplicable is the exported verdict for a row whose
// status is not in_progress (no holder process to verify).
const TaskProcessStateNotApplicable = taskProcessStateNotApplicable

// claimHostOf returns the host part of a "hostname:pid" claim stamp ("" when
// the stamp carries no colon).
func claimHostOf(claimedBy string) string {
	s := strings.TrimSpace(claimedBy)
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return ""
	}
	return s[:i]
}

// claimHostIsLocal reports whether a claim stamp was minted on THIS host —
// the predicate that separates "foreign pid, unjudgeable" from "malformed
// stamp on our own host" (both are unjudgeable, but they are different
// operator stories).
func claimHostIsLocal(claimedBy string) bool {
	h := claimHostOf(claimedBy)
	return h != "" && h == hostnameOrEmpty()
}

// procStartTimeDisplay converts a starttime tick count into an absolute
// wall-clock stamp: boot = now - uptime, then start = boot +
// ticks/procClockTicksPerSec. Best-effort by contract — an unreadable uptime
// renders the raw tick count instead of a wrong clock (Rule 15: a display
// never asserts a fact it could not read).
func procStartTimeDisplay(ticks int64) string {
	b, err := os.ReadFile(filepath.Join(procRootName, procUptimeName))
	if err != nil {
		return fmt.Sprintf("ticks=%d", ticks)
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return fmt.Sprintf("ticks=%d", ticks)
	}
	up, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return fmt.Sprintf("ticks=%d", ticks)
	}
	boot := time.Now().Add(-time.Duration(up * float64(time.Second)))
	start := boot.Add(time.Duration(float64(ticks) / procClockTicksPerSec * float64(time.Second)))
	return start.Format(time.RFC3339)
}

// TaskResumeTarget resolves a `gopi session resume <task-id>` invocation (task-mode
// resume, epic task-process-identity): given a task id it returns the row's
// tri-factor holder verdict AND the raw-log path to resume from ("" when the
// row cannot name one — the caller then reports the recovery recipe instead
// of guessing).
//
// It exists so an operator no longer has to hunt a session id by hand before
// resuming a killed run: the ledger already knows which session executed the
// row (tasks.session_id, captured at run start), and this resolves that to
// the session's checkpoint. The verdict is returned in the same call so the
// caller can gate on the PROCESS, never on the status string (the epic's
// third requirement).
//
// ROW-KEYED SINCE run-identity-unify (2026-09-20): the checkpoint is resolved
// from the row's RUN DIRECTORY (sessions/<task_id>/, whose newest run with a
// raw log is the run a resume continues), NOT from the row's session_id
// column.
//
// WHY THE COLUMN HAD TO GO — three measured reasons, in the order they bite:
//  1. the directory is 1:N and the column is 1:1, so it names only the FIRST
//     run of a serial chain (a killed run plus its resume/retry), which is
//     exactly the run a recovery does not want;
//  2. it is frequently the WRONG run: the column's documented meaning is "the
//     session that SUBMITTED or executed" (one worker session executes MANY
//     rows), so it can name a submitter session whose log lives in another
//     task's directory entirely;
//  3. reading it cost a whole-tree search (task_id → session_id →
//     taskFindSessionJSONL over ~70k session dirs here) for a path the row's
//     own task id already names.
//
// The session-keyed lookup is NOT deleted: it stays the fallback for a row
// whose run directory holds no run log at all (a re-homed or pre-layout run),
// so no row that could resolve before resolves to nothing now.
func TaskResumeTarget(taskID string) (*TaskProcessStatus, string, error) {
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		return nil, "", err
	}
	// The row's run DIRECTORY is the address; the newest run with a checkpoint
	// in it is the target. A row that never ran (no directory), or whose runs
	// kept no raw log, yields no path — a definite absence, reported as such
	// rather than papered over (Rule 15).
	if raw := taskRunResumeRawLog(st.TaskID); raw != "" {
		return st, raw, nil
	}
	// The row's OWN directory holds run logs, none of which still owns a
	// checkpoint. Before falling back to the session-keyed chain (which
	// resolves the same file by a whole-tree search — measured 1m30s on this
	// host), answer with the newest RECORDED raw-log path when there is one: it
	// names the checkpoint that is GONE, so the caller fails fast and precisely
	// instead of re-searching the tree to reach the same conclusion.
	if taskRunDirHoldsRuns(st.TaskID) {
		if recorded := taskRunRecordedRawLog(st.TaskID); recorded != "" {
			return st, recorded, nil
		}
		// The row ran, its runs kept no checkpoint and none of them recorded
		// one. This is a DEFINITE absence for THIS row, and the answer is ""
		// rather than a substitution.
		//
		// THE SUBSTITUTION WAS THE DEFECT (measured 2026-10-28 on this host).
		// The session-keyed chain below resolves whatever session the row's
		// `session_id` COLUMN names. That column is documented as "the session
		// that SUBMITTED or executed the row" (one worker session executes many
		// rows), so for a row filed by a chat it names the CHAT session, whose
		// checkpoint is a different conversation entirely. Measured with a
		// fixture profile: row ffffffff-… held its own run log
		// (sessions/<row>/12121212-….jsonl, no raw log) and a session_id naming
		// the submitter chat (abcdefab-…, raw log present) — `gopi session
		// resume <row>` continued the CHAT and reported
		//
		//	[gopi] resume: continuing session abcdefab-… in sessions/abcdefab-…
		//
		// i.e. it resumed work that had nothing to do with the row. A recovery
		// must land on the row's own work or not run at all.
		return st, "", nil
	}
	// Legacy fallback (see the doc comment): rows whose run directory holds NO
	// run log at all — a re-homed or pre-layout run, where the column is the
	// only pointer that exists. An empty session id yields no path — still a
	// definite absence.
	//
	// A FILER'S SESSION IS NOT THAT POINTER, and refusing it is the repair of a
	// measured defect (2026-11-01, row 01a19075-80b5-7a85-b0df-02a55f1c8b16).
	// That row never ran; its session_id column was empty and submitter_session
	// named the chat that filed it. This fallback took the filer, resolved the
	// filer's checkpoint, and `gopi session resume` continued ANOTHER
	// conversation, which then wrote the row's terminal status from its own
	// run's statistics — the row's directory never gained a run log, and its
	// two `completed` events carry the FILER's session_end digest
	// (elapsed=28.5s iter=9 tokens=541766). The whole ladder above exists to
	// stop exactly this substitution; the last rung was the hole.
	//
	// So the answer here is "no address for THIS row", never "here is
	// somebody else's": a recovery lands on the row's own work or does not run.
	if st.SessionIsFiler {
		return st, "", nil
	}
	if st.SessionID == "" {
		return st, "", nil
	}
	return st, taskRawLogPathFromSession(st.SessionID), nil
}

// machine-readable face of `gopi task status --json`). It lives beside the
// struct so the wire shape and the type cannot drift, and the CLI stays a
// renderer (Rule 14: the value's owner serialises it).
func TaskProcessStatusJSON(st *TaskProcessStatus) (string, error) {
	blob, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	return string(blob) + "\n", nil
}

// TaskProcessStatusReport renders the human answer for `gopi task status`:
// the ledger status, the DEFINITE verdict, and every raw fact behind it. It
// is deliberately verbose — the whole point of the verb is that an operator
// (or a script) can audit "is this row really running?" without reading
// /proc by hand, which is what the epic's zombie rows forced.
func TaskProcessStatusReport(st *TaskProcessStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task:        %s\n", st.TaskID)
	if st.Summary != "" {
		// Same render-time text boundary as TaskList/TaskShow — this is an
		// operator-facing view of the same column (tasksummarytext.go).
		fmt.Fprintf(&b, "summary:     %s\n", sanitizeTaskSummary(st.Summary))
	}
	fmt.Fprintf(&b, "status:      %s\n", st.Status)
	fmt.Fprintf(&b, "verdict:     %s\n", st.Verdict)
	if st.Note != "" {
		fmt.Fprintf(&b, "evidence:    %s\n", st.Note)
	}
	fmt.Fprintf(&b, "resumable:   %s\n", taskYesNo(st.Resumable))
	// The STATUS half of the answer, when it refused. It is printed as its own
	// indented block rather than folded into `evidence:` because a reader must
	// be able to tell "the holder looked free but the status forbids this" from
	// "a process is holding the row" — the two refusals have different remedies.
	if st.StatusRefusal != "" {
		for _, line := range strings.Split(st.StatusRefusal, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	// The ADMITTED-CONDITION half (see StatusCaveat): printed after `resumable:
	// yes`, indented like the refusal, because a reader who acts on the yes
	// without reading this is exactly the double-run the caveat exists for.
	if st.StatusCaveat != "" {
		fmt.Fprintf(&b, "  caveat: %s\n", st.StatusCaveat)
	}
	// THE SECOND AXIS' OWN READING, printed for the same reason the status
	// refusal is: a `resumable:`/refusal line that rests on a measurement must
	// let a reader see the measurement, and a refusal that rests on an
	// UNREADABLE axis must say which one (Rule 15).
	if st.DeliveryVerdict != "" {
		fmt.Fprintf(&b, "delivery:    %s — %s\n", st.DeliveryVerdict, st.DeliveryWhy)
	}
	if st.Holder != "" {
		fmt.Fprintf(&b, "holder:      %s%s\n", st.Holder, taskFreshnessSuffix(st.Freshness))
	} else {
		fmt.Fprintf(&b, "holder:      %s\n", procIdentityNone)
	}
	if st.InProgressAt > 0 {
		fmt.Fprintf(&b, "started_at:  %s\n", formatTaskTime(st.InProgressAt))
	}
	if st.IdentityRecorded {
		fmt.Fprintf(&b, "recorded:    pid=%d start=%s cmd=%q\n", st.RecordedPid, taskStartDisplay(st), st.RecordedCmd)
	} else {
		fmt.Fprintf(&b, "recorded:    %s (row predates the identity columns)\n", procIdentityNone)
	}
	// WHICH PID THE VERDICT WAS MEASURED AGAINST (bug epic 01a1b5bb). A worker
	// row normally carries TWO pids — the stamp's (`gopi worker`, the parent)
	// and the recorded executor's (the child it spawned) — and the verdict above
	// is about exactly one of them. Printing the subject is what turns
	// "verdict: running" from a claim into a measurement a reader can check
	// against the two `pid=` numbers on this same report.
	//
	// An absent subject (JudgedPid == 0: a foreign / stale / malformed stamp)
	// prints NOTHING here rather than a line naming no pid (Rule 15b).
	if st.JudgedPid > 0 {
		fmt.Fprintf(&b, "judged:      pid %d (%s)\n", st.JudgedPid, st.JudgedPidRole)
		if st.JudgedPidWhy != "" {
			fmt.Fprintf(&b, "             %s\n", st.JudgedPidWhy)
		}
	}
	// THE RECORDED EXECUTOR'S OWN READING — printed only when it is a DIFFERENT
	// process from the stamp's pid (see JudgedPidRole). On a same-pid row these
	// numbers would repeat the `live:` line above, which reads as a second,
	// independent confirmation of the same single measurement.
	if st.JudgedPidRole == taskJudgedPidRoleRecordedExecutor {
		alive := "no"
		if st.ExecutorAlive {
			alive = "yes"
		}
		fmt.Fprintf(&b, "executor:    pid %d alive=%s", st.JudgedPid, alive)
		if st.ExecutorStartAt != "" {
			fmt.Fprintf(&b, " start=%s", st.ExecutorStartAt)
		}
		b.WriteString("\n")
		if st.ExecutorCwdNote != "" {
			fmt.Fprintf(&b, "             %s\n", st.ExecutorCwdNote)
		}
	}
	// THE LIVE LINE IS A MEASUREMENT, SO IT IS PRINTED ONLY WHERE ONE WAS TAKEN.
	// For a stamp minted on ANOTHER host this process never probed /proc (it
	// would be probing a STRANGER that happens to hold the same number), so
	// `alive=no` would be a fabricated measurement reading exactly like the
	// verdict that authorises a resume — the very confusion the host dimension
	// exists to prevent. A foreign holder gets the sentence instead.
	if st.Foreign {
		fmt.Fprintf(&b, "live:        %s\n", taskForeignHolderLiveLine)
	} else if st.HolderPid > 0 {
		live := "no"
		if st.LiveAlive {
			live = "yes"
		}
		fmt.Fprintf(&b, "live:        pid %d alive=%s", st.HolderPid, live)
		if st.LiveStartAt != "" {
			fmt.Fprintf(&b, " start=%s", st.LiveStartAt)
		}
		if st.LiveCmd != "" {
			fmt.Fprintf(&b, " cmd=%q", st.LiveCmd)
		}
		b.WriteString("\n")
		// THE ROW-SCOPED HALF of the same measurement: an identity triple that
		// agrees says the pid is the recorded INCARNATION — it says nothing about
		// which row that process is serving, and that gap is exactly the bug this
		// line closes (measured 2026-11-01: pid 1604177 matched row 01a19075's
		// triple perfectly while standing in 01a1954e's session directory). The
		// three shapes are printed DISTINCTLY — names this row / names another
		// row / could not be read — because collapsing "not checked" into
		// "checked and fine" is the silent degradation Rule 15 forbids.
		// NOTHING TO REPORT unless a reading was ATTEMPTED: CwdRecorded == ""
		// means this row records no row identity, and a line that said "NOT
		// READ" for it would claim a measurement that was never asked for.
		switch {
		case st.CwdRecorded == "":
			// no row identity on this record — the line is omitted entirely
		case st.CwdSeen == "":
			// ATTEMPTED AND NOT READ. The word is "NOT READ", never a blank and
			// never a measurement — the note says which failure it was (Rule 15).
			fmt.Fprintf(&b, "cwd_row:     NOT READ — %s\n", st.CwdNote)
		case st.CwdRow == "":
			fmt.Fprintf(&b, "cwd_row:     %s — the directory name is not a minted row name, so it neither corroborates nor contradicts\n", st.CwdSeen)
		case st.CwdRow == st.CwdRecorded:
			fmt.Fprintf(&b, "cwd_row:     %s = row %s — the live process stands in THIS row's directory\n", st.CwdSeen, st.CwdRow)
		default:
			fmt.Fprintf(&b, "cwd_row:     %s = row %s ≠ THIS row (%s) — the pid has been handed on\n", st.CwdSeen, st.CwdRow, st.CwdRecorded)
		}
	}
	// The row's RUN DIRECTORY (epic run-identity-unify) — the address a reader
	// of this view actually needs ("where does this row's run live"). It is
	// rendered from the row's own id, and it is MEASURED before it is printed:
	// the ONE renderer (taskRunDirValue) labels an absent directory MISSING and
	// names the session directory the run really occupies, so this face can
	// never disagree with `task show`'s run block about the same row
	// (Rule 14 — two faces, one reading).
	if v := taskRunDirValue(st.TaskID, st.SessionID); v != "" {
		fmt.Fprintf(&b, "%s\n", taskStatusRunDirField(v))
	}
	// THE PROGRESS AXIS (taskprogress.go) — printed immediately after the run
	// directory, because it is a fact ABOUT that run: is whatever is holding
	// this row DOING anything? It is a separate line from `verdict:` on purpose;
	// the whole epic exists because "the pid is alive" was read as "the run is
	// working" (measured 2026-11-05: holder 518745 alive 5h18m with 24s of CPU
	// and a transcript silent since 16:57, refused a resume it should have got).
	// Only rendered when a reading was taken (State != ""), never as a
	// fabricated default.
	if st.Progress.State != "" {
		fmt.Fprintf(&b, "%s%s — %s\n", taskStatusProgressLabel, st.Progress.State, st.Progress.Why)
	}
	// THE LOCK AXIS (internal/runlock): the kernel flock is the VERDICT about
	// whether a process is executing this directory, and the stamp inside the
	// file is DIAGNOSIS that outlives its writer. Two lines, two facts, because
	// printing one pid without saying which fact it came from is the exact
	// misreading this epic was filed for.
	if st.RunLockState != "" {
		switch st.RunLockState {
		case runlock.LockStateHeld:
			fmt.Fprintf(&b, "%s%s — held by pid %d (%s)\n", taskStatusRunLockLabel, st.RunLockState, st.RunLockPid, st.RunLockPath)
		default:
			fmt.Fprintf(&b, "%s%s — %s (%s)\n", taskStatusRunLockLabel, st.RunLockState, st.RunLockWhy, st.RunLockPath)
		}
		if stamp := st.LockStamp; stamp != "" {
			fmt.Fprintf(&b, "%s%s\n", taskStatusLockStampLabel, stamp)
		} else {
			fmt.Fprintf(&b, "%s%s\n", taskStatusLockStampLabel, taskLockStampNone)
		}
		fmt.Fprintf(&b, "             %s\n", taskLockStampDiagnosis)
		// THE RECONCILIATION, printed directly under the two axes it arbitrates
		// (bug epic 01a1b5f2): `verdict:` and `run_lock:` answer different
		// questions about different subjects, and where a reader could take one
		// for a denial of the other, this line says which one decides. It is
		// rendered only when such a pair was actually measured — see
		// taskLockScopeNote (Rule 15b: no explanation for a shape not observed).
		if note := taskLockScopeNote(st); note != "" {
			fmt.Fprintf(&b, "%s%s\n", taskStatusLockNoteLabel, note)
		}
	}
	// THE GROUND AXIS (task_ground_in_use.go): the row's RECORDED ground and
	// whether somebody is standing in it. Rendered only when the row records a
	// ground, so a row that never created one does not grow a line of noise —
	// and when it IS occupied the sentence is the SAME evidence a refused
	// removal arm prints, which is what makes the two surfaces one reading
	// (Rule 14) instead of two.
	if st.Ground != "" {
		fmt.Fprintf(&b, "ground:      %s\n", st.Ground)
		if st.GroundInUse {
			fmt.Fprintf(&b, "ground_now:  IN USE by live process %s — cwd %s\n", st.GroundHolder, st.Ground)
			fmt.Fprintf(&b, "ground_why:  %s\n", st.GroundReason)
		} else {
			fmt.Fprintf(&b, "ground_now:  no live process stands in it\n")
		}
		// THE MEASURED SHAPE AND REBASE STATE, on one line, from the ONE
		// renderer (devworkspace.GroundFactsLine). A reader of this view is
		// deciding whether a row's commit still owes a delivery — Rule 6c draws
		// that answer from the ground's SHAPE, so the shape must be MEASURED
		// here rather than inferred by each reader.
		if st.GroundFacts != "" {
			fmt.Fprintf(&b, "%s\n", st.GroundFacts)
		}
	}
	// The ledger COLUMN is labelled as a column (epic run-identity-unify): it
	// records the session that submitted/executed the row, and it is NOT the
	// run's address (the directory is named by the row's id). The old bare
	// "session_id:" label promised a run name this value cannot serve — the
	// source of the operator's "completely meaningless" reading.
	if st.SessionID != "" {
		fmt.Fprintf(&b, "session_id_column:  %s\n", st.SessionID)
	}
	return b.String()
}

// taskStartDisplay is the recorded start in its most useful form: the wall
// clock when it could be computed, else the raw ticks.
func taskStartDisplay(st *TaskProcessStatus) string {
	if st.RecordedStartAt != "" {
		return st.RecordedStartAt
	}
	return fmt.Sprintf("ticks=%d", st.RecordedStart)
}

// taskFreshnessSuffix renders the claim-stamp freshness as a short suffix
// ("" for the unknown case — absence is not a claim of staleness).
func taskFreshnessSuffix(freshness string) string {
	switch freshness {
	case claimStampFresh:
		return " (claim stamp is fresh for this run)"
	case claimStampStale:
		return " (claim stamp PREDATES this run — a previous claimant)"
	case claimStampHolderGone:
		// The stamp IS this row's own, but the process it names is measurably
		// gone — "fresh" would deny the death the verdict line reports.
		return " (claim stamp belongs to this run, but its holder is GONE)"
	default:
		return ""
	}
}

// taskYesNo renders a boolean for operator-facing reports.
func taskYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
