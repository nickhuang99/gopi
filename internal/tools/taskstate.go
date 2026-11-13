package tools

// Honest per-row execution state for `task list` (task-list-state-honesty
// epic, 2026-09-12).
//
// PROBLEM: every in_progress row rendered the SAME "[running 4h13m since
// 08:02]" suffix, so three genuinely different situations were
// indistinguishable to the operator: (a) a live run, (b) work that FINISHED
// and was delivered but whose ledger row was never closed (the worker
// writeback gap), and (c) a run that was killed/crashed. The operator's only
// move was to ask "are these dead?" and go read process tables by hand.
//
// FIX (display layer ONLY): reinterpret the signals the ledger ALREADY
// carries — claimed_by host:pid liveness, claim-stamp freshness, heartbeat
// last_toolcall_at, session-JSONL mtime and its terminal session_end event —
// into one honest label per in_progress row:
//
//	running                 — the run is demonstrably alive
//	finished-but-unrecorded — the session ended AND its worktree is gone:
//	                          the work is done, only the LEDGER was never closed
//	died                    — no live process controls the row and the session
//	                          did not end cleanly (crash/kill/edit)
//	unknown (<reason>)      — the evidence does not settle it; the reason says why
//
// NOTHING HERE MUTATES THE LEDGER. No new status value is invented: the
// rendered label is display-only, the row stays in_progress in the DB (the
// close-out is 01a09431/01a09435's job). Cost is O(rows): the probe runs
// ONLY for in_progress rows, and the session-file layer only for rows the
// cheap layers left undecided.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
)

// taskState* are the rendered-in_progress state labels. "running" is the
// pre-existing display label (tasklist_running_test pins it), so a healthy
// row's output is byte-identical to before this change.
const (
	taskStateRunning   = "running"
	taskStateFinished  = "finished-but-unrecorded"
	taskStateDied      = "died"
	taskStateUnknown   = "unknown"
	taskStateSeparator = ": "
)

// taskStateUnknown* are the closed vocabulary of "why" suffixes that ride
// the unknown label — each names the MISSING signal, never a guess.
const (
	taskStateUnknownNoClaim       = "no claim stamp"
	taskStateUnknownStaleClaim    = "claim stamp predates the row's start"
	taskStateUnknownForeignHost   = "claim stamp names another host"
	taskStateUnknownNoSession     = "no session file"
	taskStateUnknownHeartbeatOK   = "heartbeat inside the stall grace"
	taskStateUnknownThreshold     = "stall grace unavailable"
	taskStateUnknownWorktreeHeld  = "worktree still present, session did not end"
	taskStateUnknownSessionSilent = "session has no end recorded"
)

// taskStateWorktreeSuffix rides the finished-but-unrecorded label: it names
// the second, INDEPENDENT piece of evidence behind that verdict (the
// per-task worktree is gone — the (since-retired, 58b5618b) close-out chain
// recycled it), so
// the operator can see the verdict rests on two facts, not one.
const taskStateWorktreeSuffix = " + worktree gone"

// taskStateSessionTailBytes bounds the session-JSONL tail read: a
// console-text session writes multi-megabyte "assistant" lines, so the tail
// window is deliberately generous. A window too small to hold the final
// event degrades to "not seen" (unknown), never to a wrong verdict.
const taskStateSessionTailBytes = 1 << 20

// taskAliveProcessNamePrefix is the comm-name prefix an alive executor must
// carry: /proc/<pid>/comm for a gopi run is the launched binary's name
// (cmd/agent's binary is `gopi`, plus the historical gopi-* variants).
// Requiring it is what makes the pid layer honest about PID REUSE: an
// unrelated process that happens to own a recycled pid is NOT the runner,
// and reporting it as "running" would be exactly the false-live reading
// this epic exists to remove. A pid whose name does not match gets NO
// verdict from the pid layer at all (the remaining layers decide, possibly
// unknown) — never a fabricated "died" either.
const taskAliveProcessNamePrefix = "gopi"

// taskRowState is one in_progress row's probed signals. Everything else
// (pid liveness, stamp freshness, heartbeat age) is passed in by the caller,
// so this type is the SINGLE place the session-file layer runs per row — the
// layer whose absence degrades the verdict to unknown.
type taskRowState struct {
	// Worktree recorded on the row's deliverable JSON ("" when the row
	// carries none — the verdict then rests on session evidence alone).
	Worktree string
	// WorktreeGone: a worktree IS recorded AND the path no longer exists
	// (the close-out chain recycled it after a delivered run; that chain is
	// RETIRED — 58b5618b — so this is a historical reading, not a live producer).
	WorktreeGone bool
	// SessionFile / SessionTailKnown: locate + bounded tail read of the
	// session JSONL. A missing file is itself evidence (deleted/foreign
	// node); an unreadable tail keeps SessionTailKnown false and the
	// sessionEnded layer reports "not seen", never a verdict.
	SessionFile      string
	SessionTailKnown bool
	// SessionEnd: the session's TERMINAL event is present in the tail.
	SessionEnd bool
	// RunDir / RunDirExists: the run's OWN address — the directory named by
	// the row's task id (sessions/<task_id>, run-identity-unify) and whether
	// it exists. It is the fact that separates "a run happened, its close-out
	// was skipped" from "no run ever started", which no heartbeat can supply:
	// a run killed milliseconds after registration never ticks. The extinct
	// pass (taskzombie_extinct.go) is its second reader, so the probe is the
	// ONE place it is measured.
	RunDir       string
	RunDirExists bool
}

// taskProbeRowState gathers the per-row signals for ONE in_progress row.
//
// It runs ONLY for in_progress rows (the caller gates it): pending/terminal
// rows have no running state to judge, so `task list` stays O(rows) with the
// probe cost paid per in_progress row — a handful in a healthy ledger.
func taskProbeRowState(t *Task, now time.Time) taskRowState {
	st := taskRowState{}
	if t == nil {
		return st
	}
	// Layer (d): the row's own worktree, when the deliverable recorded one.
	// This layer depends on the ROW only — never on the session id — so a
	// claimless/sessionless row still reports whether recoverable working
	// state is on disk. Gating it behind the session-id short-circuit (the
	// pre-rework order) silently dropped the fact for every row whose
	// session id is empty, which is exactly the shape that reaches the
	// ladder's death verdict on claim evidence alone.
	if wt := taskRowDeliverableWorktreeFromBlob(t.Deliverable); wt != "" {
		st.Worktree = wt
		if _, err := os.Stat(wt); err != nil {
			st.WorktreeGone = true
		}
	}
	// Layer (e): the run's own DIRECTORY, named by the row's task id
	// (sessions/<task_id> — the address run-identity-unify gave a run). It is
	// measured for EVERY in_progress row, before the session-id short-circuit,
	// because it answers a question the session layers cannot: whether a RUN
	// ever started. A row killed between auto-registration and the session
	// path's resolution has neither a directory nor a clean session end, and
	// that pair is the litter signature (taskzombie_extinct.go).
	if root := gopiDataRoot(); root != "" {
		st.RunDir = filepath.Join(root, config.SessionsDirName, taskIDNormalize(t.TaskID))
		st.RunDirExists = fileExists(st.RunDir)
	}
	// An EMPTY session id has no file to read — resolving it would let
	// taskFindSessionJSONL's prefix/subdir scan return an UNRELATED session
	// (whose verdict would then be attributed to this row). The same
	// short-circuit guards sessionIdleSeconds in taskzombie.go.
	if strings.TrimSpace(t.SessionID) == "" {
		return st
	}
	// Layer (c): the session audit log. The path is resolved from the row's
	// session_id (session-dir-unify handles all three layouts) and its tail
	// is read BOUNDED — a live 50MB session must not be slurped by a list.
	st.SessionFile = taskFindSessionJSONLForRow(
		filepath.Join(gopiDataRoot(), config.SessionsDirName), t.TaskID, t.SessionID)
	if st.SessionFile == "" {
		return st
	}
	if tail, err := readFileTail(st.SessionFile, taskStateSessionTailBytes); err == nil {
		st.SessionTailKnown = true
		st.SessionEnd = sessionTailHasEnd(tail)
	}
	return st
}

// taskInProgressState maps one in_progress row + its probed signals onto the
// honest display state. Decision ladder (first match wins), ordered from the
// STRONGEST evidence down; when every layer comes back empty the verdict is
// unknown with the reason naming the missing signal:
//
//  1. live holder pid (reuse-guarded)      → running
//  2. session_end + worktree gone          → finished-but-unrecorded
//     session_end alone                     → unknown (heartbeat-gated)
//  3. no live holder + session not seen     → died
//  4. session_end + heartbeat inside grace → unknown ("heartbeat inside the
//     stall grace")
//  5. otherwise                            → unknown (<missing signal>)
//
// Layers 2/4's gate on the heartbeat is the honesty core: session_end is a
// SESSION-lifetime fact, not a run's, and one session can EXECUTE SEVERAL
// rows (a worker dispatching), so a session that ended while the run that
// produced the row is still going must never render as finished. The session
// file's mtime is deliberately NOT used as a liveness signal: `gopi task
// list` itself appends tool RESULTS to the session log, so its own read bumps
// that mtime — a self-inflicted false-live. The ledger's last_toolcall_at is
// the run's own heartbeat and is used instead.
func taskInProgressState(t *Task, alive bool, aliveChecked bool, freshness string,
	heartbeatStale bool, thresholdKnown bool, st taskRowState, now time.Time) string {
	switch {
	case aliveChecked && alive:
		return taskStateRunning
	case st.SessionEnd && st.WorktreeGone:
		// Two independent facts: the session wrote its terminal event AND
		// the run's worktree was already recycled by finish-cleanup. The
		// work landed; only the ledger row is stale.
		return taskStateFinished + taskStateWorktreeSuffix
	case !taskClaimAttributable(t, aliveChecked):
		// The row's claim stamp cannot name an executor ON THIS HOST: absent,
		// malformed, older than the row's own start (a previous claimant), or
		// minted on another machine. NONE of those is a statement about the
		// holder's liveness — the pid is simply not ours to probe — so the
		// session layer must NOT be allowed to assert "died" underneath it.
		// A foreign claim whose session JSONL happens to be readable here
		// (the verification's defect 2: the session-only layer sat ABOVE the
		// claim layer) would otherwise be called dead from evidence gathered
		// on the wrong machine.
		return taskStateUnknown + taskStateSeparator + taskClaimUnverifiableReason(t.ClaimedBy, freshness)
	case aliveChecked && !alive:
		// The claim layer DID vote, and its vote was DEAD: the stamp names a
		// pid on THIS host, on a stamp fresh for this row's execution, and
		// the pid is not a live `gopi` runner (its number was reused, or it
		// is gone). That is a decidable row — it must never be reported as an
		// unknowable foreign host (the verification's defect 1).
		if st.Worktree != "" && !st.WorktreeGone {
			// …unless the run left recoverable working state behind: a
			// worktree nobody is holding is in-progress work, not settled.
			return taskStateUnknown + taskStateSeparator + taskUnknownNoLiveHolderReason(st)
		}
		return taskStateDied
	case st.SessionTailKnown && !st.SessionEnd:
		// No claim stamp named a holder on this host (handled above), and the
		// session never wrote its terminal event: the runner was killed
		// (SIGKILL), crashed, or the session log was edited away. If a
		// worktree IS recorded and still exists the run left recoverable
		// working state behind → not settled → unknown.
		if st.Worktree != "" && !st.WorktreeGone {
			return taskStateUnknown + taskStateSeparator + taskUnknownNoLiveHolderReason(st)
		}
		return taskStateDied
	case st.SessionEnd && thresholdKnown && !heartbeatStale:
		// The session ended, but the LEDGER heartbeat is inside the stall
		// grace: a worker dispatching again would keep the heartbeat live
		// while its session-end is on disk. Cannot tell "finished" from
		// "still working" → honest unknown.
		return taskStateUnknown + taskStateSeparator + taskStateUnknownHeartbeatOK
	case !thresholdKnown:
		return taskStateUnknown + taskStateSeparator + taskStateUnknownThreshold
	default:
		// The remaining unknowns are all "this host cannot name the
		// executing process" shapes. The CLAIM-derived reason wins when the
		// claim stamp itself is the reason (absent / predating the row /
		// minted elsewhere): that names the missing signal precisely, while
		// the session-derived reason would only report a symptom.
		if r := taskClaimUnverifiableReason(t.ClaimedBy, freshness); r != "" {
			return taskStateUnknown + taskStateSeparator + r
		}
		return taskStateUnknown + taskStateSeparator + st.noLiveHolderReason()
	}
}

// taskUnknownNoLiveHolderReason is the worktree-blocks-death variant: the
// row's session did not end cleanly (would otherwise be "died") but the run
// left a live-looking worktree, so the death verdict is withheld. Used by the
// died-branch and by noLiveHolderReason, so the two paths can never drift.
func taskUnknownNoLiveHolderReason(st taskRowState) string {
	if st.Worktree != "" {
		return taskStateUnknownWorktreeHeld
	}
	return taskStateUnknownNoSession
}

// noLiveHolderReason names the signal that kept the row out of "died": a row
// whose session was READ and carried no terminal event is unknown because the
// run's end is unrecorded, not because its session file is missing.
func (st taskRowState) noLiveHolderReason() string {
	if st.SessionTailKnown {
		return taskStateUnknownSessionSilent
	}
	return taskUnknownNoLiveHolderReason(st)
}

// taskRowDeliverableWorktreeFromBlob extracts deliverable.worktree from a
// task row's raw deliverable JSON ("" when absent / not an object). Blob-
// local by design: the list renderer already HAS the scanned row, so the
// worktree layer costs a JSON unmarshal, never a second ledger read.
func taskRowDeliverableWorktreeFromBlob(blob string) string {
	if strings.TrimSpace(blob) == "" {
		return ""
	}
	var d struct {
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		return ""
	}
	return strings.TrimSpace(d.Worktree)
}

// sessionFreshWithin reports whether the session log was written within
// withinSec — the liveness floor for a run that has not finished booting. A
// stat error is reported FRESH (fail-closed: an unreadable file keeps the row).
func sessionFreshWithin(path string, withinSec float64) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()).Seconds() < withinSec
}

// taskClaimAttributable reports whether the row's claim stamp is a statement
// about a process THIS host may judge: a well-formed "hostname:pid" whose host
// is this machine AND whose freshness keeps it attached to this row's
// execution. aliveChecked is the claim layer's own report that it accepted the
// stamp and probed the pid — it is the single authority here, so the ladder
// and the pid layer can never disagree about which shapes are local:
//
//	false → no stamp / malformed stamp / previous claimant / foreign host:
//	        the pid is not ours to probe, so NO layer may assert death
//	true  → this host probed the pid and its vote is authoritative
//
// taskClaimAttributable reports whether something on THIS row named a process
// this host could probe — the precondition for the ladder's dead/live rungs.
//
// TWO stamps can do it, and they are written at the same moment: the `claimed_by`
// string (host:pid) and the recorded process identity (proc_pid +
// proc_start_ticks + proc_cmd). Requiring the STRING alone is what made
// `task list` report "no claim stamp" for a row whose identity columns named
// the very pid `task show` was already calling "gone" (2026-11-05) — the
// ledger held the answer and the display refused to read it. Either stamp
// being present and MEASURED (aliveChecked, i.e. the probe actually reached a
// verdict) is enough; a foreign host or an unreadable /proc still arrives here
// with aliveChecked=false and keeps the honest unknown label.
func taskClaimAttributable(t *Task, aliveChecked bool) bool {
	if !aliveChecked {
		return false
	}
	if strings.TrimSpace(t.ClaimedBy) != "" {
		return true
	}
	return t.HasProcIdentity && t.ProcPid > 0
}

// taskClaimUnverifiableReason is the reason for a row whose claim stamp
// cannot name the executing process: no stamp at all, a stamp older than the
// row's own in_progress transition (a PREVIOUS claimant — the
// claimed-holder-freshness epic's shape), or a stamp minted on another host
// (a foreign pid is unknowable here). All three mean "this host cannot
// decide", which is exactly what the rendered label must say.
//
// A claim the ladder reached with aliveChecked=true is deliberately NOT
// routed here: that pid WAS probed on this host and its (dead) vote is a
// verdict, not an unverifiable stamp — see taskInProgressState's
// aliveChecked-&&-!alive branch.
func taskClaimUnverifiableReason(claimedBy, freshness string) string {
	switch {
	case freshness == claimStampStale:
		return taskStateUnknownStaleClaim
	case strings.TrimSpace(claimedBy) == "":
		return taskStateUnknownNoClaim
	default:
		return taskStateUnknownForeignHost
	}
}

// readFileTail returns at most maxBytes from the END of path. A file shorter
// than maxBytes is returned whole. Reading the tail (not the whole file) is
// what keeps the per-row session check cheap on multi-megabyte sessions; the
// seek lands mid-line, so the first line of the returned window is a partial
// fragment the caller must tolerate (sessionTailHasEnd parses whole lines
// only, from the newline that follows the window's first byte onward).
func readFileTail(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := info.Size() - maxBytes
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, 0); err != nil {
		return nil, err
	}
	buf := make([]byte, info.Size()-off)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// sessionTailHasEnd reports whether the tail window carries the session's
// terminal event. It scans from the window's FIRST NEWLINE onward (the bytes
// before it are a partial line a mid-file seek may have cut), parsing each
// complete line's "type" field — never a substring match on the raw bytes,
// because prompt/console text is echoed into the log and would match a bare
// contains() (the false-positive that would render a dead run as finished).
func sessionTailHasEnd(tail []byte) bool {
	start := 0
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		start = i + 1
	} else if !startsWithLineBoundary(tail) {
		// No newline at all and the window starts mid-line: nothing
		// trustworthy to parse.
		return false
	}
	sc := bufio.NewScanner(strings.NewReader(string(tail[start:])))
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue // prompt echo / console text — not a JSONL event line
		}
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // a truncated or non-event line never decides anything
		}
		if ev.Type == sessionevent.EventTypeSessionEnd {
			return true
		}
	}
	return false
}

// startsWithLineBoundary reports whether b begins at a line boundary — true
// only when the tail read is the WHOLE file (offset 0), which readFileTail
// signals by returning fewer bytes than the requested window. A full-file
// window needs no newline to be trustworthy.
func startsWithLineBoundary(b []byte) bool {
	return int64(len(b)) < taskStateSessionTailBytes
}

// gopiDataRoot resolves the gopi data root (config.GopiDir — $GOPI_HOME/.gopi
// or ~/.gopi). "" when the home cannot be resolved: the session layers then
// report "no session file" instead of guessing a path.
func gopiDataRoot() string {
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	return gd
}
