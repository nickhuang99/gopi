package main

// chat_exit_marker.go — the ONE artifact that answers "did this chat run reach
// its closeout?" after a death that wrote nothing else.
//
// WHY THIS EXISTS (the measured case). On 2026-10-20 at 08:04:38 a live
// `gopi chat` session (8616b46c) stopped producing anything. Its session
// directory held four artifacts and NOT ONE of them could say how the process
// ended:
//
//	artifact          reading at the death          what it proves
//	session.jsonl     last row = a `request` row    nothing (it is the last
//	                  at 08:04:38.089               write BEFORE the death)
//	session_end       ABSENT                        unlike every guarded exit
//	signal-provenance ABSENT                        no handled signal
//	crash dump        only the 08:39 BACKFILL,      the sweep's own artifact,
//	                  category=orphan-no-session-end written 35 minutes later
//
// Four exit guards exist (finishChat's closeout, the SIGINT/SIGTERM/SIGHUP
// watcher, recoverRunPanic, signalrec's provenance) and NONE of them left a
// mark — which is not "all four failed", it is "the process stopped where no
// guard was reachable" (SIGKILL, an _exit with no deferred work, a death inside
// a syscall). Distinguishing those from "a closeout path forgot to write
// session_end" had to be done by reasoning about which artifacts were missing,
// and it could not be done at all from the directory itself.
//
// WHAT THIS WRITES, AND WHY IT IS THE SMALLEST THING THAT WORKS. One small JSON
// file in the run's OWN session directory — a directory the run already
// creates, swept with the session it annotates (Rule 35: no new process, no new
// daemon, no new directory, no new config key). It is written TWICE, at the two
// instants that make the difference decidable:
//
//	arm    (before the REPL takes the terminal)  phase="live"
//	close  (the FIRST statement of finishChat)   phase="closing"
//
// and once more ONLY on a path that proves the process survived:
//
//	runChatWithInput returns without finishChat  phase="returned-unmarked"
//
// THE READING RULE — ONE FILE, ONE READ, FIVE ANSWERS:
//
//	marker            session_end   verdict
//	----------------  ------------  -------------------------------------------
//	absent            absent        the process died before the REPL armed it
//	                                (no session record was usable either)
//	phase=live        absent        ★ THE MEASURED CASE: the process vanished
//	                                without reaching ANY closeout — SIGKILL /
//	                                _exit / death inside a syscall. Every
//	                                guard was unreachable, not broken.
//	phase=closing     absent        it died INSIDE the closeout, after the
//	                                closeout began and before session_end
//	                                landed — a different bug, named by the fact
//	                                that the marker moved.
//	phase=closing     present       an ordinary ending; session_end names the
//	                                cause (reason / signal / eof_kind).
//	phase=returned-…  (any)         runChatWithInput RETURNED without finishChat
//	                                (a pre-REPL error path, which also printed
//	                                on stderr). Not a silent death.
//
// The verdict is a fact about the MARKER, so it needs no second file and no
// second process: `cat <session dir>/chat-exit.marker` is the whole instrument.
// The pid/start_ticks pair is evidence for a human (it names WHICH incarnation
// died, per the same identity rule internal/sandbox/owner.go uses), never a
// verdict input — the verdict is decided by the phase, which a dead process
// cannot advance.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// chatExitMarkerName is the marker's file name inside the run's own session
	// directory. It is deliberately NOT a `.jsonl` name: in a session dir a
	// JSONL is either a record or a registered side file
	// (internal/sessionlayout.SideFileNames), and this artifact is neither — it
	// is ONE run's evidence, the family that package calls run-evidence
	// (`chat-screen.dump` is the same family, written by default beside it).
	// That keeps the family registry honest without registering a new side file
	// for a file that annotates the RUN rather than the session record.
	//
	// The VALUE comes from internal/sessionlayout, the package that owns the
	// shape of a sessions tree (Rule 14): this writer and the reader that
	// annotates an orphan dump must not spell the name twice.
	chatExitMarkerName = sessionlayout.ExitMarkerName

	// chatExitMarkerTempPattern names the temp file the atomic install writes
	// through (same directory, so rename(2) is atomic): a reader must never see
	// a half-written marker, which is exactly what a death mid-write would
	// leave behind if the bytes went straight to the destination.
	chatExitMarkerTempPattern = ".chat-exit.marker-*"

	// chatExitMarkerPerm — readable by the operator and by any agent reading the
	// session dir; it carries no secret (pid, ticks, phase, exit code).
	chatExitMarkerPerm = 0o644

	// chatExitMarkerVersion is the schema version, so a future field can be
	// added without a reader having to guess which shape it is holding (the
	// same reason internal/sandbox's owner marker carries one).
	chatExitMarkerVersion = 1

	// The three phases. They are the verdict: a reader decides by comparing the
	// phase against whether session_end exists, and nothing else.
	//
	//	chatExitPhaseLive      armed, no closeout reached — the death case
	//	chatExitPhaseClosing   finishChat began (its FIRST statement writes this)
	//	chatExitPhaseReturned  the run FUNCTION returned without finishChat
	chatExitPhaseLive     = "live"
	chatExitPhaseClosing  = "closing"
	chatExitPhaseReturned = "returned-unmarked"

	// chatExitStampLayout is the timestamp layout: RFC3339 with nanoseconds, so
	// the marker's own instants can be ordered against session.jsonl's rows
	// (which carry nanoseconds too) — a second of resolution cannot separate
	// the arm, the last request and the death.
	chatExitStampLayout = time.RFC3339Nano
)

// chatExitMarkerDoc is the file's wire shape. Every field is spelled once here
// (Rule 13) and the writer is the only producer.
type chatExitMarkerDoc struct {
	Version int `json:"version"`
	// Phase is the verdict — see the five-row table in this file's header.
	Phase string `json:"phase"`
	// SessionID is the record's identity, resolved by the session logger (the
	// ONE definition, internal/sessionlayout.SessionID's caller).
	SessionID string `json:"session_id"`
	// PID and StartTicks name the INCARNATION that armed the marker: a pid alone
	// is a recyclable number, the kernel start ticks make it an identity
	// (internal/scratch.ProcStartTime). Evidence for a reader, never an input to
	// the verdict.
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	// Started and Updated are the two instants this file witnesses.
	Started string `json:"started"`
	Updated string `json:"updated"`
	// ExitCode is read ONLY when Phase is closing or returned-unmarked; in the
	// live phase nothing has chosen an exit code yet and 0 is not a claim.
	ExitCode int `json:"exit_code"`
	// Reason / Signal / EOFKind are the closeout's own words (agent.EndCause),
	// recorded only in the closing phase, and each read only for its reason —
	// the same discipline session_end follows (Rule 15a: a zero value here means
	// "this phase does not carry the field", and the phase says which).
	Reason  string `json:"reason"`
	Signal  string `json:"signal"`
	EOFKind string `json:"eof_kind"`
}

// chatExitMark is one run's marker. It is used from the run's OWN goroutine
// only (arm at REPL setup, closing in finishChat, returned in the deferred
// release that wraps them), which is why it carries no lock: a mutex here would
// claim a concurrency this file does not have.
type chatExitMark struct {
	path string
	doc  chatExitMarkerDoc
	// closed records that the closeout (or the returning path) already wrote its
	// verdict, so the deferred release cannot overwrite a real finding with its
	// weaker one.
	closed bool
}

// armChatExitMarker writes the live marker and returns the handle the closeout
// advances. It returns nil when the run has no session directory (a `--replay`
// run mints no session at all, internal/agent.NewSessionLoggerForRun's guard),
// and every method is nil-safe: a run without a session must not pay for — or
// crash on — an instrument about a session it does not have.
//
// A WRITE FAILURE IS REPORTED, NEVER SWALLOWED (Rule 15b): the caller prints the
// one line this returns, and the run continues. A chat must not fail because its
// death certificate could not be opened — but the operator learns that the next
// death will be undecidable, which is the fact worth knowing at arm time rather
// than at the postmortem.
func armChatExitMarker(sessionDir, sessionID string) (*chatExitMark, error) {
	if sessionDir == "" {
		return nil, nil
	}
	now := chatExitStampNow()
	m := &chatExitMark{
		path: filepath.Join(sessionDir, chatExitMarkerName),
		doc: chatExitMarkerDoc{
			Version:   chatExitMarkerVersion,
			Phase:     chatExitPhaseLive,
			SessionID: sessionID,
			PID:       os.Getpid(),
			Started:   now,
			Updated:   now,
		},
	}
	// The ticks are best-effort evidence: an unreadable /proc must not stop the
	// marker (the verdict does not depend on them), and a fabricated 0 would be
	// read as "no identity", which is exactly what an unreadable entry means.
	if ticks, err := scratch.ProcStartTime(os.Getpid()); err == nil {
		m.doc.StartTicks = ticks
	}
	return m, m.write()
}

// chatExitStampNow is this file's clock, named once so every instant it records
// is spelled the same way.
func chatExitStampNow() string { return time.Now().Format(chatExitStampLayout) }

// write installs the document atomically (temp file in the same directory, then
// rename), so a reader never sees a truncated marker: the whole point of the
// file is to be readable after a process died mid-something.
func (m *chatExitMark) write() error {
	if m == nil {
		return nil
	}
	m.doc.Updated = chatExitStampNow()
	blob, err := json.MarshalIndent(m.doc, "", "  ")
	if err != nil {
		return fmt.Errorf("chat exit marker: encode %s: %w", m.path, err)
	}
	blob = append(blob, '\n')
	f, err := os.CreateTemp(filepath.Dir(m.path), chatExitMarkerTempPattern)
	if err != nil {
		return fmt.Errorf("chat exit marker: create the temp file beside %s: %w", m.path, err)
	}
	tmp := f.Name()
	if _, err := f.Write(blob); err != nil {
		_ = f.Close()      //stylegate:ignore the write already failed; this is the teardown of THIS call's own descriptor
		_ = os.Remove(tmp) //stylegate:ignore best-effort removal of THIS call's own temp file
		return fmt.Errorf("chat exit marker: write %s: %w", tmp, err)
	}
	// The bytes must be ON DISK before the rename: this file is read after the
	// process is gone, possibly after a power loss, so a page-cache-only file
	// would defeat it. One small write, twice per run.
	if err := f.Sync(); err != nil {
		_ = f.Close()      //stylegate:ignore the write already failed; this is the teardown of THIS call's own descriptor
		_ = os.Remove(tmp) //stylegate:ignore best-effort removal of THIS call's own temp file
		return fmt.Errorf("chat exit marker: flush %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp) //stylegate:ignore best-effort removal of THIS call's own temp file
		return fmt.Errorf("chat exit marker: close %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, chatExitMarkerPerm); err != nil {
		_ = os.Remove(tmp) //stylegate:ignore best-effort removal of THIS call's own temp file
		return fmt.Errorf("chat exit marker: mode %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp) //stylegate:ignore best-effort removal of THIS call's own temp file
		return fmt.Errorf("chat exit marker: install %s: %w", m.path, err)
	}
	return nil
}

// closing advances the marker to the closeout's verdict. It is called as the
// FIRST statement of finishChat — before the replay join, before the store-sync
// cancel, before anything that could fail — so that a death INSIDE the closeout
// leaves `phase=closing` and is distinguishable from the measured case (the
// process died with the marker still at `live`).
//
// cause is the closeout's own EndCause — WHAT THE CALLER STATED, verbatim. It is
// deliberately NOT the resolved cause: agent.CloseChatSession applies the one
// precedence that lets a recorded signal beat the caller's word
// (agentEndCause, internal/agent/loop.go), and re-deriving it here would be a
// second implementation of that rule (Rule 2) and a second answer to "how did
// this run end".
//
// The honest consequence, measured on a pty (SIGTERM leg): the signal legs pass
// Reason="signalled" with an EMPTY Signal, because the NAME lives on the agent
// (CloseSignal, set on the goroutine that decoded it), so this file records
// `reason=signalled signal=""` while the session_end written moments later
// carries the name. That is the intended division — the marker's job is the
// PHASE (did the closeout begin?), and the resolved cause is session_end's.
func (m *chatExitMark) closing(exitCode int, cause agent.EndCause) error {
	if m == nil || m.closed {
		return nil
	}
	m.closed = true
	m.doc.Phase = chatExitPhaseClosing
	m.doc.ExitCode = exitCode
	m.doc.Reason = cause.Reason
	m.doc.Signal = cause.Signal
	m.doc.EOFKind = cause.EOFKind
	return m.write()
}

// returnedWithoutCloseout is the deferred release registered the instant the
// marker is armed. It writes ONLY when nothing else did — i.e. when
// runChatWithInput returned by a path that never reached finishChat (a pre-REPL
// refusal after the agent existed). Its existence is what keeps `phase=live`
// HONEST: the live phase is reserved for a process that never returned at all,
// which is the class this instrument was built to make decidable.
func (m *chatExitMark) returnedWithoutCloseout() error {
	if m == nil || m.closed {
		return nil
	}
	m.closed = true
	m.doc.Phase = chatExitPhaseReturned
	return m.write()
}
