package agent

// The auto-memory WRITE gate for a replay (epic `gopi chat --replay` must be
// really read-only; the operator's defect A, measured 2026-10-12).
//
// THE DEFECT THIS FILE CLOSES. internal/replaymode already stopped four writes
// (the slot registry, the editor history, the chats table, the tool dispatch),
// and each of those was chosen because it is a write the SURFACE performs simply
// by being alive. The auto-memory capture paths are the same kind of writer and
// were missed: a replay's rounds run the real extractor
// (captureSelfNotes/captureResponseText) and the model may still call the memory
// tool, so a run whose entire purpose is to draw a picture of a session that
// already happened inserted rows into `distilled_qa` in the OPERATOR's own
// shared ledger (`~/.gopi/memory.db`) — the store the operator reads to remember
// what THEY learned. Nothing in that chain asked whether the process was
// replaying.
//
// WHY THE GATE IS HERE AND NOT INSIDE internal/tools. The store layer is a
// library a CLI verb (`gopi memory store`) also reaches, and its answer to "am I
// replaying?" would be a second decision point that the announcement could not
// reach: Rule 15 wants each refused write RECORDED, and the record has to name
// the write in this run's own voice. So the ONE question (replaymode.Active —
// never a mode string, never a second flag; Rule 13/14) is asked here, at the
// write sites the agent owns, and the sentence is replaymode.Refuse's spelling so
// the vocabulary cannot drift from the sites outside this package.
//
// THE RECORD IS A FILE, NOT THE CHAT (2026-10-25). The first version announced
// through a.Logger, and a chat run points that logger at the LAYOUT — so every
// refusal was DRAWN as a chat row, tangled with the recording's own replies. See
// recordReplayMemoryRefusal: the sentence is appended to the profile's
// replay-memory-refusals.log and the surface is told nothing.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// replayMemoryRefusalLogName is the file, inside the gopi data root's logs
// directory, that carries a replay's memory-write refusals.
//
// WHY A FILE AND NOT THE RUN'S OWN SESSION DIRECTORY. A replay MINTS NO SESSION
// (internal/agent's session logger refuses, cmd/agent keeps no run directory),
// so there is no per-run directory to hang evidence off — and inventing one for
// a run whose whole contract is "writes nothing outside the recording" would be
// the write this epic exists to stop. The profile's logs directory is the ONE
// durable place this process already owns, it outlives the run, and a reader can
// grep it after the fact (`grep 'auto-memory' ~/.gopi/logs/replay-memory-refusals.log`).
const replayMemoryRefusalLogName = "replay-memory-refusals.log"

// replayMemoryRefusalLogDirName is the profile child the evidence file lives
// under (the same "logs" directory the resident daemons write to).
const replayMemoryRefusalLogDirName = "logs"

// The evidence file's permissions (Rule 1): a directory a reader may traverse, a
// file its owner appends to. The sentence carries the head of a captured note, so
// it is owner-only, like this profile's other records.
const (
	replayMemoryRefusalLogDirMode = 0o700
	replayMemoryRefusalLogMode    = 0o600
)

// replayCaptureSkipped reports whether this round's capture pipeline must be
// skipped because the process is re-running a recording, and records the fact
// at most ONCE PER RUN.
//
// WHY ONCE PER RUN. The capture pipeline runs on EVERY round, in two places
// (captureSelfNotes and captureResponseText), so a per-call record would write
// two lines per turn and turn a long recording into a wall of identical
// refusals — evidence that buries its own signal. The refusal is a fact about
// the RUN ("this process captures nothing"), so it is recorded when the run
// first reaches the pipeline and never again.
//
// The flag lives on selfRecord — the struct that is already one-per-run and
// already owns the capture path's state (the dedup map, the record directory) —
// so no new Agent field moves a line of loop.go. That is a diff-cost reason and
// it still holds (a new field above existing code shifts what follows); what no
// longer holds is the enforcement it used to cite: the devdoc line-anchor gate
// (which failed when an anchor drifted) was deleted with internal/docclass in
// 5a35f61, and nothing reads a `loop.go:NNN` anchor now. The census that replaced
// it over CONFIG.md's rows (`config-anchor-census.go --check`, its
// TestMergeConfigAnchorGate* arms in internal/tools) anchored no line of loop.go
// either, and it was deleted with the documentation-citation family in 78bdf05a
// (2026-11-05): nothing executable checks an anchor on this tree.
func (a *Agent) replayCaptureSkipped() bool {
	if !replaymode.Active() {
		return false
	}
	if a.selfRecord.replayCaptureRecorded {
		return true
	}
	a.selfRecord.replayCaptureRecorded = true
	a.recordReplayMemoryRefusal("capture this round's notes and responses into the operator's ledger")
	return true
}

// memoryWriteReplayRefused reports whether a memory store about to be performed
// by this agent must be short-circuited because the process is re-running a
// recording. `what` names the write for the announcement ("store an auto-memory
// note", "store a captured response").
//
// A true return means NOTHING was written: the caller must return without
// touching the store (Rule 15 — the announcement is the record, and a caller
// that announced and stored anyway would be worse than silent).
func (a *Agent) memoryWriteReplayRefused(what string) bool {
	return a.memoryWriteReplayRefusal(what) != ""
}

// memoryWriteReplayRefusal is the SAME decision, spelled for a caller that has
// to hand a sentence back to somebody (the memory TOOL's store returns its
// text to the model as the tool result). Empty means "not refused, go ahead".
//
// WHY A SECOND SPELLING EXISTS AT ALL. The model-initiated site lives in
// loop.go, a file ~7000 lines long that hundreds of devdoc lines cite, many of
// them by LINE NUMBER (`loop.go:NNN`, 870 of them across 166 files): adding an
// import there shifts every line below it and re-drifts documents that have
// nothing to do with this epic. Keeping the replaymode dependency in THIS file —
// the one the epic added — and letting loop.go call a method keeps the edit
// surgical (Rule 4) and the citations stable.
//
// HISTORY: that citation drift used to be ENFORCED — the deleted doc-anchor gate
// failed when a `loop.go:NNN` anchor stopped resolving, and it was deleted whole
// with internal/docclass in 5a35f61. Today the 870 citations are unchecked prose,
// so the argument above is a diff-and-churn cost, not a gate. The census that
// replaced it over CONFIG.md's rows only (`config-anchor-census.go --check`, its
// TestMergeConfigAnchorGate* arms in internal/tools) went with the
// documentation-citation family in 78bdf05a (2026-11-05): nothing executable
// checks an anchor on this tree.
func (a *Agent) memoryWriteReplayRefusal(what string) string {
	if !replaymode.Active() {
		return ""
	}
	a.recordReplayMemoryRefusal(what)
	return replaymode.Refuse(what)
}

// recordReplayMemoryRefusal RECORDS the ONE refusal sentence — and, on a replay, it
// records it in the run's own EVIDENCE rather than on the chat surface.
//
// THE DEFECT THIS CLOSES (the operator's, measured with a real pty run
// 2026-10-25). Every refusal below is correct in OUTCOME and wrong in PLACE: the
// sentence went through a.Logger — which a chat run points at the layout's flow
// seam — so `[gopi] 🧠 [gopi replay] refused to store an auto-memory note in the
// operator's ledger (…)` was drawn INSIDE the answer band, interleaved with the
// recording's own replies, once per captured note and once per captured response.
// A display whose whole job is to draw a session that already happened was
// reading as though this run had tried to write and been turned away. The same
// mistake was already corrected one layer down for the memory PREFETCH
// (cmd/agent/chat_prefetch.go builds nothing at all and says so ONE time); the
// capture paths were left announcing per event.
//
// WHY SILENCE ON SCREEN IS NOT SILENCE (Rule 15, and the constraint this epic
// carries). A refused write must stay READABLE, so the sentence is APPENDED to a
// file in the profile's own logs directory (replayMemoryRefusalLogName) with the
// instant it was refused. That is durable, greppable after the run, and invisible
// to a surface that is drawing someone else's conversation. The chat screen is
// then told nothing at all.
//
// WHY THE LOGGER IS NOT USED FOR THE RECORD. a.Logger's destination is decided by
// its OWNER, and for a chat run that owner routes it to the screen (cmd/agent's
// flow seam and the diagnostics fan-out) — the very channel this gate must not
// reach. So the record is written by this function through a handle it opens
// itself, and the log line that carries the failure of THAT write goes to
// stderr: a replay's stderr is not the band, and losing the evidence silently
// would be the failure.
func (a *Agent) recordReplayMemoryRefusal(what string) {
	sentence := replaymode.Refuse(what)
	if err := RecordReplayMemoryRefusal(sentence); err != nil {
		replaymode.Noticef("%s (the refusal record could not be written: %v)\n", sentence, err)
	}
}

// RecordReplayMemoryRefusal is the SAME recording, exported for the verb-level
// sites in cmd/agent (a memory prefetch that was not built, a slash `/memory
// store` typed into a replay). Those refusals are memory refusals too, so they
// belong in the same evidence file and off the same screen — and one writer for
// both keeps the format from drifting into two shapes (Rule 14).
//
// A caller passes the sentence it will not print: build it with
// replaymode.Refuse(what) so the vocabulary stays single-sourced.
func RecordReplayMemoryRefusal(sentence string) error {
	return appendReplayMemoryRefusal(sentence)
}

// appendReplayMemoryRefusal appends one refusal sentence, stamped with the
// instant it was refused, to the profile's replay-evidence log.
//
// The directory is resolved through config.GopiDir — the ONE resolution every
// consumer goes through (GOPI_HOME honoured, the near-miss refused there) — so
// the file lands in the same profile the refused write would have touched. A
// profile that cannot be resolved or written is an ERROR the caller announces;
// it is never a silent no-op (Rule 15).
func appendReplayMemoryRefusal(sentence string) error {
	gd, err := config.GopiDir()
	if err != nil {
		return fmt.Errorf("resolve the gopi data root: %w", err)
	}
	dir := filepath.Join(gd, replayMemoryRefusalLogDirName)
	if merr := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, replayMemoryRefusalLogDirMode); merr != nil {
		return fmt.Errorf("create %s: %w", dir, merr)
	}
	path := filepath.Join(dir, replayMemoryRefusalLogName)
	f, oerr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, replayMemoryRefusalLogMode)
	if oerr != nil {
		return fmt.Errorf("open %s: %w", path, oerr)
	}
	line := time.Now().UTC().Format(time.RFC3339) + " " + sentence + "\n"
	if _, werr := f.WriteString(line); werr != nil {
		// Recorded even though the run is degraded: a half-written refusal is
		// still evidence that a write was refused.
		_ = f.Close() //stylegate:ignore the close error cannot outrank the write error being reported
		return fmt.Errorf("append to %s: %w", path, werr)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("close %s: %w", path, cerr)
	}
	return nil
}
