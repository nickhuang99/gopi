package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
)

// The RUN'S OWN ACCOUNT OF HOW IT ENDED, read on the `dead` verdict.
//
// WHY THIS EXISTS (row 01a16f9d-88f3-7b53-99a3-130dbb12aa9a, measured
// 2026-10-25): the holder ladder answers "is the recorded pid alive?", and a
// process that exits answers NO — which is TRUE and also useless. The note then
// said "the run ended (killed/crashed)", a claim about WHY that the ladder never
// measured. A worker run that finished its work, wrote its report and exited 0
// was rendered as a casualty, and the operator reading `task status` had no way
// to tell the two apart. The run itself had already recorded the answer —
// session_end carries exit_code and (since task 01a163f8) a machine-readable
// reason — so the corroboration is a READ, not a guess.
//
// The rule this restores is Rule 15 in its negative form: absence of a live pid
// is not evidence of a crash. When the log records a clean end the note says so;
// when it records a failed end it says THAT, with the reason; and when nothing
// can be read the note keeps the old wording but stops asserting a cause it
// never observed.
const (
	// taskRunEndExitCodeKey is the wire key of session_end's exit status. It
	// has no sessionevent constant of its own (the writer in internal/agent
	// spells it inline and internal/rundir carries it as a struct tag), so the
	// single reader that needs it by name declares it here rather than typing
	// the literal at the format call (Rule 1).
	taskRunEndExitCodeKey = "exit_code"
	// taskRunEndTailBytes bounds the read: a 50MB session must not be slurped
	// to answer one line, and session_end is the LAST event by construction.
	taskRunEndTailBytes = 64 << 10
	// taskRunEndCleanExit is the exit status a run that did its work leaves.
	taskRunEndCleanExit = 0
)

// taskRunEnd is what the run's own session log says about its ending. Known is
// false when nothing could be read — the caller must then avoid asserting a
// cause (Rule 15: a reader that could not look says so, it does not guess).
type taskRunEnd struct {
	Known    bool
	ExitCode int
	Reason   string
}

// taskRunEndClean reports whether the recorded ending is a clean one: exit 0
// AND a reason that says the work finished. A run that exited 0 because the
// operator typed /quit, or because its stdin closed under it, is NOT a clean
// end of the WORK — the reason axis is what separates them, which is the whole
// point of task 01a163f8's field.
func (e taskRunEnd) Clean() bool {
	if !e.Known || e.ExitCode != taskRunEndCleanExit {
		return false
	}
	switch e.Reason {
	case sessionevent.SessionEndReasonComplete, sessionevent.SessionEndReasonQuit,
		sessionevent.SessionEndReasonEof, sessionevent.SessionEndReasonInterrupt:
		return true
	}
	return false
}

// taskRunEndOf reads the row's OWN run's ending. The session file is resolved
// through the same helper the ladder uses (taskFindSessionJSONLForRow), so the
// path a `task status` note reads and the path the in_progress ladder reads
// cannot diverge (Rule 14).
func taskRunEndOf(taskID, sessionID string) taskRunEnd {
	if strings.TrimSpace(sessionID) == "" {
		return taskRunEnd{}
	}
	path := taskFindSessionJSONLForRow(
		filepath.Join(gopiDataRoot(), config.SessionsDirName), taskID, sessionID)
	if path == "" {
		return taskRunEnd{}
	}
	tail, err := readFileTail(path, taskRunEndTailBytes)
	if err != nil {
		return taskRunEnd{}
	}
	return taskRunEndFromTail(tail)
}

// taskRunEndFromTail finds the LAST session_end line in the window and returns
// its exit code and reason. Line-by-line JSON parsing, never a substring match:
// prompt and console text is echoed into the log and would match a bare
// contains() (the same false positive sessionTailHasEnd documents).
func taskRunEndFromTail(tail []byte) taskRunEnd {
	start := 0
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		start = i + 1
	} else if !startsWithLineBoundary(tail) {
		return taskRunEnd{}
	}
	end := taskRunEnd{}
	sc := bufio.NewScanner(strings.NewReader(string(tail[start:])))
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var ev struct {
			Type     string  `json:"type"`
			ExitCode *int    `json:"exit_code"`
			Reason   *string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != sessionevent.EventTypeSessionEnd {
			continue
		}
		// A newer session_end REPLACES an older one: a resumed run appends, and
		// the last line is the ending that matters.
		end = taskRunEnd{Known: true}
		if ev.ExitCode != nil {
			end.ExitCode = *ev.ExitCode
		}
		if ev.Reason != nil {
			end.Reason = strings.TrimSpace(*ev.Reason)
		}
	}
	return end
}

// taskProcessDeadNote renders the `dead` verdict's line from the run's own
// ending. Three shapes, and each one states ONLY what was measured:
//
//	clean    → "exited 0 <reason> — the WORK may have finished; the row was never closed"
//	failed   → "exited N <reason> — the run FAILED"
//	unknown  → "the run left no readable ending — a crash is possible, not proven"
//
// The old wording asserted "killed/crashed" on a bare absent pid; that is the
// defect this replaces, so no branch here names a cause the reader could not
// observe.
func taskProcessDeadNote(pid int, end taskRunEnd) string {
	if !end.Known {
		return fmt.Sprintf("pid %d is gone — the run left no readable ending in its session log (a crash is possible, not proven); the row was never closed", pid)
	}
	if end.Clean() {
		return fmt.Sprintf("pid %d is gone — the run recorded a clean end (exit %d, reason %s); the WORK may have finished and the row was never closed",
			pid, end.ExitCode, taskRunEndReasonWord(end.Reason))
	}
	return fmt.Sprintf("pid %d is gone — the run recorded a FAILED end (exit %d, reason %s); this is the casualty shape",
		pid, end.ExitCode, taskRunEndReasonWord(end.Reason))
}

// taskRunEndReasonWord keeps the note readable when an older log wrote no
// reason key: "not recorded" is a fact, never a default invented here.
func taskRunEndReasonWord(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "not recorded"
	}
	return reason
}
