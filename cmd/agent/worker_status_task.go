package main

// worker_status_task.go — WHICH TASK is each running worker running, as an
// explicit, machine-queryable FIELD on every worker line (epic
// worker-status-task-field, 2026-10-23).
//
// THE MEASURED DEFECT. `gopi worker --status` renders three worker shapes and
// only one of them named a task:
//
//	① a STATE-RECORD worker (a detached daemon that wrote
//	   worker-state-<pid>) rendered `busy — <task-id> (session …)` — but only
//	   WHILE a round was in flight, because the writer clears the field at
//	   every round end (SetRoundEnd);
//	② an ARGV-FALLBACK worker (detached, no state file) rendered its raw
//	   command line and nothing structured;
//	③ a FOREGROUND worker — the COMMON shape on this host: pids 1697081,
//	   1700250, 1701687 were all bare `gopi worker …` — rendered
//	   `pid N: FOREGROUND (holds no daemon lock) — <raw cmdline>; …`.
//
// The task id existed in all three cases, but in ② and ③ only INCIDENTALLY
// inside a free-form command string, so an operator could not read it without
// re-parsing ps output and could not query it at all.
//
// THE FIX, in one place (Rules 2/13/14): a `workerTask` VALUE (id + the honest
// SOURCE it came from) and ONE derivation order — the state record when it
// carries one, else the worker's OWN argv through the verb's OWN grammar
// scanner (scanWorkerArgs — never a second parser), else the declared
// not-recorded marker (Rule 15: an unknown task says so, it never renders
// blank). Every renderer (the record line, the foreground line, the JSON form)
// asks for that value, so the three shapes cannot drift apart.
//
// THE QUESTIONABLE READING, refused: a raw substring search of the command
// line for something task-id-shaped. argv[0] is the binary, argv[1] is the
// verb, and every following word is a FLAG or an OPERAND of a flag — the
// operand of `--repo` is a repo path, the operand of `--db` is a database. Only
// the grammar scan knows which word is the row, so only it is asked.

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/sessionid"
)

const (
	// workerStatusTaskFieldPrefix is the STABLE token every worker line carries:
	// `task=<id>`. It is the queryable half of this epic — an operator (or a
	// script) greps the one token and never parses the sentence around it.
	// One definition, three renderers (the two status shapes and the JSON form
	// uses the same value in its own key).
	workerStatusTaskFieldPrefix = "task="

	// workerStatusTaskNotRecorded is the HONEST value when nothing names a task
	// — a loop worker between rounds, a worker whose argv carries no row, a
	// /proc entry that cannot be read. It is rendered, never omitted (Rule 15):
	// an empty field is indistinguishable from a renderer that forgot it.
	workerStatusTaskNotRecorded = "(not recorded)"

	// workerTaskSource* name WHERE the value came from. The source travels with
	// the id because the two halves answer different questions: the id is the
	// fact, the source is how much to trust it (a state record is the worker's
	// own statement; argv is what it was STARTED with, which for a named-row
	// worker is authoritative and for a loop worker is absent).
	workerTaskSourceState = "state"
	workerTaskSourceArgv  = "argv"
	// workerTaskSourceSessionDir is the ROUND's own working directory: a worker
	// that has entered a task's session container (taskWorkdirEnter) IS running
	// that row, and the container's name is the row. It is the source the
	// FOREGROUND/loop shape has instead of a state record — see
	// workerTaskFromRoundSessionDir for the exact evidence and its refusals.
	workerTaskSourceSessionDir = "session-dir"
	workerTaskSourceNone       = "none"

	// workerStatusStateUnknown is the state word for a shape that publishes no
	// record at all — a foreground or argv-fallback worker. It exists because
	// the JSON form always carries a `state` key and Rule 15a forbids leaving
	// a required field's semantics to the zero value: "" would read as "idle"
	// to a careless consumer of a worker that is running a round right now.
	workerStatusStateUnknown = "unknown"
)

// workerTask is the derived answer for ONE worker: the row it is (or was
// started for) and the honest source of that answer. The zero value is NOT a
// rendered state — Field() is the only renderer and it says so.
type workerTask struct {
	ID     string
	Source string
}

// Field renders the stable query token: `task=<id>` or `task=(not recorded)`.
func (t workerTask) Field() string {
	if t.ID == "" {
		return workerStatusTaskFieldPrefix + workerStatusTaskNotRecorded
	}
	return workerStatusTaskFieldPrefix + t.ID
}

// Recorded reports whether anything actually named a row. It is what the JSON
// form's `task_recorded` boolean carries, so a consumer never has to compare
// the id against a sentinel string.
func (t workerTask) Recorded() bool { return t.ID != "" }

// sourceWord is the rendered source, defaulting to the honest "none" so a
// zero-value task cannot claim a provenance it never had.
func (t workerTask) sourceWord() string {
	if t.Source == "" {
		return workerTaskSourceNone
	}
	return t.Source
}

// workerTaskFromArgv derives the row from a worker's OWN argv, through the
// verb's OWN grammar (scanWorkerArgs): the positional it accepts IS the row
// (`gopi worker <task-id>`), and every flag operand is consumed by the flag
// that owns it — so `gopi worker --repo <path>` can never report the path as a
// task id.
//
// An argv whose grammar the scanner REFUSES (it returns the offending word) is
// not guessed at: the worker is running, so this is a real process and its
// shape does not match what this binary accepts — the honest answer is "no row
// recorded", not a keyword harvested from a sentence the scanner did not
// understand.
func workerTaskFromArgv(argv []string) workerTask {
	if len(argv) < 2 || argv[1] != procname.VerbWorker {
		return workerTask{Source: workerTaskSourceNone}
	}
	wa, bad := scanWorkerArgs(argv[2:])
	if bad != "" || wa.explicit == "" {
		return workerTask{Source: workerTaskSourceNone}
	}
	return workerTask{ID: wa.explicit, Source: workerTaskSourceArgv}
}

// workerTaskFromRoundSessionDir derives the row from the directory the worker
// is STANDING IN — the third source, and the one the loop face actually
// publishes (bug: `gopi worker --status` regressed to `task=(not recorded)`,
// 2026-10-28).
//
// THE EVIDENCE, and why it is a fact rather than a guess. `taskWorkdirEnter`
// moves a worker into `<sessions root>/<task-id>` at its CLAIM boundary and
// `taskWorkdirRestore` moves it back at the round's end (task_session_cwd.go),
// so a worker whose /proc/<pid>/cwd is a task's session CONTAINER is, at that
// instant, running that row. That is not an incidental string: it is the same
// container whose output.log and report the round writes, and the six workers
// this bug was measured on all stood in their own row's container while their
// lines said "not recorded".
//
// WHY IT IS ASKED LAST. A state record is the worker's own statement while a
// round is in flight, and a NAMED row in argv is what the operator started it
// for (authoritative even before the first claim, when the worker still stands
// where it was launched). The directory is the round's live position, so it
// answers only when neither of the two above can.
//
// THE REFUSALS (Rule 15: an unknown says so, it never guesses). A row is named
// only when ALL of these hold:
//
//	① the cwd link could be read (/proc/<pid>/cwd, never a process listing's
//	   string form — `ps -o cwd` prints "-" under this kernel, measured);
//	② its PARENT directory is the sessions root this profile resolves
//	   (config.SessionsRoot: the same ONE resolver the writer used, so a
//	   relocated root is followed and a foreign path is not);
//	③ the container is a DIRECT child — a task dir is one join, never a nested
//	   path;
//	④ the name passes internal/sessionid's shape gate, the seam every task
//	   container path goes through (a dashless uuid is NOT a row);
//	⑤ the container still EXISTS as a directory (a cwd the kernel keeps alive
//	   after its directory was removed is not a live round).
//
// A row that closed its round has been moved back out (taskWorkdirRestore), so
// the honest answer for an idle loop worker remains "not recorded".
func workerTaskFromRoundSessionDir(pid int) workerTask {
	none := workerTask{Source: workerTaskSourceNone}
	cwd, err := os.Readlink(filepath.Join(procnameRoot(), strconv.Itoa(pid), procname.CwdName))
	if err != nil || cwd == "" {
		return none
	}
	root, err := config.SessionsRoot()
	if err != nil || root == "" {
		return none
	}
	if filepath.Dir(cwd) != filepath.Clean(root) {
		return none
	}
	id := filepath.Base(cwd)
	if !sessionid.IsAdmissible(id) {
		return none
	}
	if info, serr := os.Stat(cwd); serr != nil || !info.IsDir() {
		return none
	}
	return workerTask{ID: id, Source: workerTaskSourceSessionDir}
}

// workerTaskOfPID reads a live process's own argv (/proc/<pid>/cmdline) and
// derives the row from it. An unreadable /proc entry yields the not-recorded
// task with source none — the pid is still listed (Rule 15: the worker is
// real; only the attribution is unknown).
func workerTaskOfPID(pid int) workerTask {
	argv, ok := procname.Cmdline(procnameRoot(), strconv.Itoa(pid))
	if !ok {
		return workerTask{Source: workerTaskSourceNone}
	}
	if t := workerTaskFromArgv(argv); t.Recorded() {
		return t
	}
	return workerTaskFromRoundSessionDir(pid)
}

// Task is THE derivation order for every worker shape (Rules 2/13):
//
//	① the STATE RECORD, but only while a round is in flight — the writer clears
//	   TaskID at every round end, so a daemon between rounds has no in-flight
//	   row and must say "not recorded" rather than repeat the last one;
//	② the worker's OWN argv, through the verb's grammar (a named-row worker —
//	   `gopi worker <task-id>` — carries its row there, and a FOREGROUND worker
//	   has nothing else);
//	③ the not-recorded marker.
//
// Step ② is asked for EVERY shape, not only the argv-fallback one: a daemon
// started as `gopi worker <task-id>`? no — `--daemon` refuses a named row —
// but a state-record worker's argv is the second honest source when its
// record is between rounds and the process was started with a row, and asking
// both costs one /proc read.
func (w workerStatusWorker) Task() workerTask {
	if !w.Fallback && w.Record.Busy() && w.Record.TaskID != "" {
		return workerTask{ID: w.Record.TaskID, Source: workerTaskSourceState}
	}
	if w.PID <= 0 {
		return workerTask{Source: workerTaskSourceNone}
	}
	return workerTaskOfPID(w.PID)
}

// workerStateField is the state word for a worker shape that owns no record:
// the JSON form always carries the key, and "unknown" is the truth a foreground
// worker's status line can defend.
func workerStateField(rec workerStateRecord, fallback bool) string {
	if fallback {
		return workerStatusStateUnknown
	}
	return rec.StateWord()
}
