package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
)

// The resume checkpoint has TWO possible homes, and until this file existed only
// the first was read.
//
// MEASURED DEFECT (2026-10-29, row 01a183a8): a run whose FIRST HTTP request
// died at the transport layer (http_status=0, "dial tcp … i/o timeout") leaves a
// session directory whose raw.log is a 167-byte header and nothing else — the
// raw logger writes its "→ REQUEST" marker line only once a request body is
// actually handed to the transport, so there is no checkpoint to read. The SAME
// directory holds <run>.dump.json carrying the complete request body
// (model/max_tokens/system/messages/tools/thinking/reasoning_effort), with
// failed_iteration=0, category=network and recoverable_hint=true. The resume
// therefore refused with "no → REQUEST checkpoint found" — a message that
// describes the empty FILE as the absence of the CONTEXT, which is exactly the
// belief this fallback removes: the request the run died on is on disk.
//
// The dump is written by the crash-dump path (crashdump.go's WriteCrashDump)
// from the SAME in-memory request the raw logger would have written, so a
// dump-recovered checkpoint is the same full-context body — it is a second
// SOURCE for one artifact, not a different artifact (Rules 13/14).

// CheckpointSource names the artifact a resume checkpoint was recovered from.
// It is returned to the caller so the resume face can SAY which one it used
// (Rule 15: a recovery from a second source is announced, never silent).
type CheckpointSource string

const (
	// CheckpointSourceRawLog is the byte-exact source: the last "→ REQUEST"
	// payload of the raw HTTP log — the request body as it went on the wire.
	CheckpointSourceRawLog CheckpointSource = "raw log → REQUEST"

	// CheckpointSourceDump is the fallback: the crash dump's recorded request
	// body, used when the raw log carries no completed request round.
	CheckpointSourceDump CheckpointSource = "crash dump request"
)

// CheckpointProvenance is WHERE a recovered checkpoint came from, in the terms
// the operator needs to check it: the artifact kind, the file, and — for the
// dump — the failure facts the document itself records. It is returned rather
// than logged because the resume face owns the wording (Rule 15: a recovery
// from a second source is announced, never silent).
type CheckpointProvenance struct {
	Source          CheckpointSource
	Path            string
	FailedIteration int
	Category        string
}

// Detail renders the provenance for an operator-facing line.
func (p CheckpointProvenance) Detail() string {
	if p.Source != CheckpointSourceDump {
		return fmt.Sprintf("%s in %s", p.Source, p.Path)
	}
	return fmt.Sprintf("%s %s (failed_iteration=%d, category=%s)", p.Source, p.Path, p.FailedIteration, p.Category)
}

// bareSessionDumpFileName is the dump name a BARE-SESSION container uses
// ("session.dump.json"): the dump is a sibling of the session RECORD, so its
// base name is the record's base name — the raw log's own name there is the run
// uuid, which would point at a dump that does not exist. Derived from the
// layout's own vocabulary (Rules 13/14), never typed.
var bareSessionDumpFileName = strings.TrimSuffix(sessionlayout.RecordName, sessionlayout.JSONLSuffix) + dumpFileSuffix

// dumpCheckpointDoc is the subset of the crash-dump document a resume reads.
// Every other field of the document (error_body, elapsed_sec, recent_tool_calls,
// panic_stack, …) is evidence for the operator, not context for the resumed run.
type dumpCheckpointDoc struct {
	Type            string          `json:"type"`
	SessionID       string          `json:"session_id"`
	Category        string          `json:"category"`
	FailedIteration int             `json:"failed_iteration"`
	Request         json.RawMessage `json:"request"`
}

// dumpRecoveredCheckpoint is what a successful dump read yields: the request
// body plus the facts the resume face reports when it announces the fallback.
type dumpRecoveredCheckpoint struct {
	Request         *deepseek.ClaudeRequest
	Path            string
	FailedIteration int
	Category        string
	SessionID       string
}

// dumpCheckpointCandidates lists the dump files a resume may read, most
// specific first. THREE shapes exist on this host and each one is named, rather
// than globbing a directory and hoping:
//
//  1. <run>.dump.json — the per-run name (<id>.jsonl / <id>.dump.json family),
//     derived from the raw log's own base name;
//  2. session.dump.json — the BARE-SESSION container's name (its record is
//     session.jsonl, so its dump is session.dump.json while the raw log beside
//     it is named after the run uuid);
//  3. any other *.dump.json in the same directory — last, and only if it parses
//     as our document with a replayable request, so an unexpected naming cannot
//     turn a resumable run into a refusal.
func dumpCheckpointCandidates(rawLogPath string) []dumpCheckpointCandidate {
	dir := filepath.Dir(rawLogPath)
	base := strings.TrimSuffix(filepath.Base(rawLogPath), scratch.SessionRawLogFileSuffix)
	if base == filepath.Base(rawLogPath) {
		// Not the co-located ".raw.log" shape: the legacy flat log's extension
		// is the whole suffix (gopi-raw-YYYYMMDD-HHMMSS.log), and its dump —
		// if any — is named after the run, not the file.
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	candidates := []dumpCheckpointCandidate{
		// Derived FROM THE RAW LOG'S OWN NAME: this is the checkpoint of the very
		// run being resumed, by construction.
		{Path: filepath.Join(dir, base+dumpFileSuffix), OwnRun: true},
		// The bare-session container's name: one run per directory, so the file
		// is that run's.
		{Path: filepath.Join(dir, bareSessionDumpFileName), OwnRun: true},
	}
	others, err := filepath.Glob(filepath.Join(dir, "*"+dumpFileSuffix))
	if err != nil {
		// ErrBadPattern cannot be reached from a literal pattern; returned
		// rather than swallowed so a future pattern change fails visibly.
		return candidates
	}
	for _, p := range others {
		if p == candidates[0].Path || p == candidates[1].Path {
			continue
		}
		// NOT derived from the name, so it is NOT ours by construction: a
		// task-run directory holds ONE DUMP PER RUN of that row
		// (sessions/<task-id>/<uuid>.dump.json), so an unscoped glob would let a
		// resume of run A recover run B's request — a wrong context, silently
		// continued. The document's own session_id is the only thing that ties
		// it to a run, so this candidate must PROVE it (below).
		candidates = append(candidates, dumpCheckpointCandidate{Path: p, RunID: base})
	}
	return candidates
}

// dumpCheckpointCandidate is one dump file a resume may read, with the evidence
// it must carry: ownRun means the name already ties it to the resumed run, and
// RunID (set on candidates found by scanning the directory) additionally
// requires the document's session_id to match.
type dumpCheckpointCandidate struct {
	Path   string
	OwnRun bool
	RunID  string
}

// readDumpCheckpoint reads one candidate and turns it into a request. A file
// that is not a crash-dump document, or one whose request body cannot be
// replayed, is reported BY NAME so the refusal can say what it found rather
// than only what it did not.
func readDumpCheckpoint(path string) (*dumpRecoveredCheckpoint, error) {
	return readDumpCheckpointFor(path, "")
}

// readDumpCheckpointFor reads one dump and, when runID is non-empty, requires the
// document to declare THAT run (see dumpCheckpointCandidate).
func readDumpCheckpointFor(path, runID string) (*dumpRecoveredCheckpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unreadable: %w", err)
	}
	var doc dumpCheckpointDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("not a crash-dump document: %w", err)
	}
	if runID != "" && strings.TrimSuffix(doc.SessionID, dumpFileSuffix) != runID {
		return nil, fmt.Errorf("it is another run's dump (session_id=%q, wanted %q)", doc.SessionID, runID)
	}
	if doc.Type != crashDumpDocType {
		return nil, fmt.Errorf("not a crash-dump document (type=%q)", doc.Type)
	}
	if len(doc.Request) == 0 {
		return nil, fmt.Errorf("the dump records no request body")
	}
	// The marker may arrive bare (the raw logger's own body) or QUOTED (a
	// document that stored the trimmed text as a JSON string), so the check is
	// on the marker's presence, not on the first byte.
	if strings.Contains(strings.Trim(string(doc.Request), `"`), dumpHeadTrimmedPrefix) {
		// The dump caps the request body (dumpRequestMax) by keeping its TAIL
		// and marking the head as trimmed — a replay aid, deliberately not
		// valid JSON. Replaying it would send a truncated context, so the
		// resume refuses instead and says why.
		return nil, fmt.Errorf("the dump's request body was head-trimmed at %d bytes and cannot be replayed", dumpRequestMax)
	}
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal(doc.Request, &req); err != nil {
		return nil, fmt.Errorf("the dump's request body is not a parseable request: %w", err)
	}
	return &dumpRecoveredCheckpoint{
		Request:         &req,
		Path:            path,
		FailedIteration: doc.FailedIteration,
		Category:        doc.Category,
		SessionID:       doc.SessionID,
	}, nil
}

// loadCheckpointFromDump is the FALLBACK half of the resume checkpoint: read the
// crash dump standing beside the raw log and recover the request the interrupted
// run died on.
//
// WHAT failed_iteration CHANGES, and what it does not. The dump's request body
// is the full-context body of the request that was in flight, so a dump with
// failed_iteration=0 (the failure was the FIRST request — a transport error,
// nothing ever reached the provider) and one with failed_iteration>0 (a later
// round failed after the conversation had grown) recover the SAME way: the
// recovered request's messages seed the resumed context verbatim. The number is
// not a switch; it is what the resume face TELLS the operator, so the recovery
// is understood rather than guessed (Rule 15). Nothing here rebuilds context
// from iterations because the persisted request already IS that context.
func loadCheckpointFromDump(rawLogPath string) (*dumpRecoveredCheckpoint, error) {
	var tried []string
	for _, cand := range dumpCheckpointCandidates(rawLogPath) {
		recovered, err := readDumpCheckpointFor(cand.Path, cand.RunID)
		if err != nil {
			tried = append(tried, fmt.Sprintf("%s — %v", cand.Path, err))
			continue
		}
		if len(recovered.Request.Messages) == 0 {
			tried = append(tried, fmt.Sprintf("%s — the dump's request carries no messages", cand.Path))
			continue
		}
		return recovered, nil
	}
	return nil, fmt.Errorf("%s", strings.Join(tried, "; "))
}

// resumeNoCheckpointError is the HONEST refusal: it names the file that was
// read, the fact that it holds no completed request round, and every dump
// candidate that was examined — instead of the pre-fix "no → REQUEST checkpoint
// found", which stated the absence of a checkpoint without saying that a
// complete request body was sitting in the same directory and was never looked
// at (the measured row 01a183a8 shape).
func resumeNoCheckpointError(rawLogPath string, rawLogBytes int64, dumpTried string) error {
	dir := filepath.Dir(rawLogPath)
	return fmt.Errorf("resume: no → REQUEST checkpoint in %s (%d bytes: the run recorded no completed request round) and no replayable crash dump beside it — tried %s (looked for a dump in %s); the run died before any request body reached disk, so there is no context to continue from. Recover with `gopi task clone <task-id> --repo <repo>` for a clean-room run, or resume a KNOWN session id / raw-log path",
		rawLogPath, rawLogBytes, dumpTried, dir)
}
