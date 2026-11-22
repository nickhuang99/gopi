package tools

// Signal-record destination for a spawn seam (epic signalrec-verify-attribution,
// 2026-09-17).
//
// internal/signalrec files a killed process's record in the OWNING session dir
// of that process and NEVER in a host-global file (no-public-fallback, pinned
// by TestSignalRecWritesNothingUnderTheLogsDir). When no handle resolves it
// REFUSES — loudly — which is correct for a process whose session cannot be
// named, and WRONG for a process whose session is known to its launcher but
// unnameable from inside it: the merger's verify child.
//
// WHY THE VERIFY CHILD CANNOT DERIVE ITS OWN DIR — three independent reasons,
// which is why this seam exists rather than a fallback inside signalrec:
//
//	① it is handed no GOPI_SIGNALREC_SESSION_DIR (nothing knew to hand one);
//	② it carries NO ledger id — the gate STRIPS the daemon context on purpose
//	   (an independent verification must not inherit the resolving agent's
//	   handle, daemon-context-isolation epic) — and it mints its OWN session
//	   uuid at startup, so the judged row's key is not derivable inside it;
//	③ its argv mode (`task verify`) owns its signal handlers, so the
//	   resident process-keyed layout (sessions/process-<pid>) is not available
//	   to it by construction.
//
// The launcher DOES know the directory: `gopi task verify` runs the verifier in
// the TASK's session container (tools.TaskSessionDir), which is where every
// other artifact of that row lives. So the destination is resolved HERE, at the
// spawn seam, and handed over as an explicit opt-in (signalrec.EnvAgentChild) —
// never as a fallback signalrec would apply to any process carrying a ledger id
// (a shell that inherited a stale id must keep being REFUSED: filing under
// another run's session is over-attribution, the very defect the session-dir
// rule removes).

import (
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/signalrec"
)

// SignalRecordDestination is the resolved provenance handoff of a spawned
// child: the session dir its signal records belong in, the ledger id to record
// beside them, and — when Dir is empty — WHY nothing could be resolved (the
// caller prints the reason; Rule 15: a silent miss is the failure mode this
// whole seam exists to end).
type SignalRecordDestination struct {
	Dir    string
	TaskID string
	Note   string
}

// SignalRecordDest resolves the provenance destination of the child that
// executes taskID (the ledger row the child judges) and guarantees the dir
// EXISTS.
//
// It never returns a public/shared path (there is none to return — signalrec
// owns that rule and refuses rather than falling back), and it never invents a
// directory that is not this row's session container: the container is resolved
// through TaskSessionDir, the same single source every other artifact of the
// row uses. The mkdir is deliberate for the handoff and only for the handoff:
// signalrec's own resolution treats a dir that merely MATCHES an id as
// inadmissible unless it carries the session container's marker (metadata.json
// or a *.jsonl), so a destination that does not exist yet would be validated
// only on the die path — i.e. at the exact moment the answer is needed, the
// handoff would be refused. Creating it up front makes the handoff TRUE before
// the child starts (and the child's own session log lands there anyway when it
// mints its session).
func SignalRecordDest(taskID string) SignalRecordDestination {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return SignalRecordDestination{Note: "no ledger id to attribute the record to"}
	}
	dir, err := TaskSessionDir(taskID)
	if err != nil {
		return SignalRecordDestination{Note: "task session dir unresolved: " + err.Error()}
	}
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, signalRecordDirPerm); err != nil {
		return SignalRecordDestination{Note: "task session dir " + dir + " could not be created: " + err.Error()}
	}
	return SignalRecordDestination{Dir: dir, TaskID: taskID}
}

// SignalRecordEnv returns taskID's provenance handoff as the "NAME=value"
// environment entries a launcher appends before spawning the child: the
// destination pair plus the opt-in marker that makes signalrec HONOR it instead
// of refusing (see SignalRecordDest and signalrec.handleEnv). Empty when no
// destination could be resolved — the caller must then say so on stderr rather
// than spawn a child whose death cannot be attributed (Rule 15).
func SignalRecordEnv(taskID string) []string {
	d := SignalRecordDest(taskID)
	if d.Dir == "" {
		return nil
	}
	return []string{
		signalrec.SessionDirEnv + "=" + d.Dir,
		signalrec.TaskIDEnv + "=" + d.TaskID,
		signalrec.EnvAgentChild + "=" + signalRecordAgentChildOn,
	}
}

// signalRecordAgentChildOn is the opt-in value of the marker (the same "1"
// every boolean knob in the tree arms with — spelled once, Rule 1b).
const signalRecordAgentChildOn = "1"

// signalRecordDirPerm is the mode of the session container created for a
// provenance handoff (the same mode the task-session machinery uses; Rule 1:
// one named value, not an inline octal).
const signalRecordDirPerm = 0o755
