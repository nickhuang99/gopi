package main

// chat_ground.go — the chat ENTRYPOINT's half of the session ground report
// (epic chat-session-ground-retired-when-not-a-row, 2026-09-29).
//
// THE CREATION PATH IS GONE. Until 2026-09-27 this file did two things only an
// entrypoint can do: ANNOUNCE the session's own worktree on stderr and ENTER it
// (chdir) — the chdir being the wiring that made every later tool round land in
// that worktree. The operator retired that decision: a chat is not a row, so a
// ground it mints can be collected by no arm, and the `chat/*` refs this host
// still carries are exactly that residue (devdoc/
// chat-session-ground-lifecycle-diagnosis.md). The producer is deleted
// (internal/tools/chat_ground.go) and with it the chdir: the session stands
// where the caller stood, so there is nothing to enter and nothing to leave.
//
// WHAT SURVIVES IS THE REPORT, and it is not decoration: an interactive session
// whose writes land in a repository's MAIN CHECKOUT must say so, in one line,
// before the first turn (Rule 15 — silence is the defect). That line now comes
// from chatGroundReport below.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatGroundReport states, once at session start, where this session's writes
// will land — the surviving contract of the retired chat-ground epic.
//
// It MOVES NOTHING: no worktree is created, no branch is named, no directory is
// entered, no `git` write verb is run. The resolution is a pure read (the
// directory, and whether it belongs to a repository's main checkout), so this
// function cannot leave anything behind. Its ONLY effect is the stderr line.
//
// A DEGRADED resolution is not an error: the session continues where it was
// started and the reason is printed (tools.ChatGroundDegradedNotice). The session
// must start — a chat is a human's session, and refusing it for a directory that
// could not be resolved would take away the thing that was asked for (the
// superseded epic's rule, kept). The error return is kept for the same contract
// it always had: the callers act on it, and a future change that needs to refuse
// the session has one place to say so.
func chatGroundReport(ag *agent.Agent) (tools.ChatGroundResolution, error) {
	sessionID := ""
	if ag != nil && ag.SessionLog != nil {
		sessionID = ag.SessionLog.SessionID()
	}
	startDir, err := os.Getwd()
	if err != nil {
		return tools.ChatGroundResolution{}, fmt.Errorf("cannot resolve the directory this session was started in, so gopi cannot say where its writes will land: %w", err)
	}
	// NO CHDIR, AND THAT IS THE CHANGE: the session runs in startDir, and a
	// relative path in a later read/write/edit/bash round resolves against it
	// (internal/tools/read.go's filepath.Abs, bash.go's no-workdir default) —
	// which is what "no ground of its own" means, stated in the notice below.
	report := tools.ChatGroundFor(sessionID, startDir)
	if notice := chatGroundNotice(report); notice != "" {
		fmt.Fprint(os.Stderr, notice)
	}
	return report, nil
}

// chatGroundNotice picks the ONE line this run prints about its session ground,
// and BOTH halves of the choice are read from the report.
//
// THE DEFECT THIS ANSWERS (row 01a1a629-62f4, evidence in the sibling row
// 01a1a629-9dfb's attachments). Under `gopi chat --replay <id>` the run printed
// the MAIN CHECKOUT sentence — "this session writes in <dir> … every
// read/write/edit/bash round lands uncommitted in that checkout's working
// area" — while a replay performs no write at all: internal/replaymode is armed
// for exactly that run, so it mints no session, opens no raw log, registers no
// chat slot and answers every tool call from the recording. So the sentence
// described an event that cannot happen, and the criterion that picked it ("is
// the standing directory a repository's main checkout") was the right question
// about the wrong thing: what decides whether a session WRITES is not where it
// stands but whether it is a replay.
//
// THE VERDICT IS READ FROM THE REPORT, NOT ASKED AGAIN HERE (2026-11-04
// correction). This function used to call replaymode.Active() a second time,
// which made tools.ChatGroundResolution.ReadOnly — the field its producer
// ChatGroundFor sets — DEAD: a field nothing reads cannot be wrong, so a caller
// holding a resolution had no way to ask "is this run a replay", and the whole
// resolution could disagree with the line printed about it without any test
// noticing. There is ONE question ("is this run a replay") and ONE place that
// asks it — the producer (internal/tools/chat_ground.go), whose own comment
// states that the flag is asked there rather than at the caller precisely so a
// caller-side re-ask cannot be the bypass. So the input is report.ReadOnly.
//
// AND THE SENTENCE IS THE PRODUCER'S, FOR THE SAME REASON (the second half of
// the same defect). This file used to carry its OWN replay sentence
// (chatGroundReplayNoticeFmt, "this session re-runs a recording, so it writes
// nothing: …"). That sentence was true and it still stated a write family the
// producer had ALSO shipped — tools.ChatGroundReadOnlyAnnouncement, rendered by
// tools.ChatGroundAnnouncement whenever the resolution is read-only. Two
// renderers for one fact is two texts that can drift, and they did: the arms in
// chat_ground_replay_readonly_notice_test.go quote the PRODUCER's three phrases
// ("writes NOTHING", "REPLAY", "read-only") and the caller's private sentence
// contained none of them, so the line under a replay was reported as not saying
// what this run is. The fix is not to reword either text — the producer's is
// the shipped one and the caller's claim was correct — it is to print ONE
// renderer: ChatGroundAnnouncement already delegates to the read-only family on
// ReadOnly, so the caller no longer chooses a sentence at all.
//
// replaymode.Source() STAYS and is NOT the same question: it returns the
// RECORDING PATH, not whether this run is a replay. It is read here only to name
// which recording is being re-run — a fact the resolution does not carry and the
// operator cannot get from any other line.
func chatGroundNotice(report tools.ChatGroundResolution) string {
	degraded := tools.ChatGroundDegradedNotice(report)
	notice := tools.ChatGroundAnnouncement(report)
	// A DEGRADED resolution renders no announcement (its reason is the line), and
	// the replay's own fact is still worth stating: the run answered from a
	// recording even where its directory could not be resolved.
	if report.ReadOnly {
		notice += fmt.Sprintf(chatGroundReplayRecordingFmt, replayRecordingSource())
	}
	return degraded + notice
}

// chatGroundReplayRecordingFmt names the recording a replay answers from. It is
// the ONE thing this file adds to the producer's sentence, and it is not a claim
// about writing: the producer already stated that (writes NOTHING).
const chatGroundReplayRecordingFmt = "[gopi] chat: every round of this re-run is answered from the recording %s, never from a live model or a tool dispatch.\n"

// replayRecordingSource answers the recording's path, stating an unknown one
// rather than printing an empty slot (Rule 15: a stated gap, never a silent one).
func replayRecordingSource() string {
	if src := strings.TrimSpace(replaymode.Source()); src != "" {
		return src
	}
	return chatGroundUnknownRecording
}

// chatGroundUnknownRecording is what the line says when the flag is armed with no
// recording named — a diagnostic state, said out loud instead of rendered as "".
const chatGroundUnknownRecording = "an unnamed recording"

// chatGroundWriteScopeFragment is the phrase that makes the live line a WRITE
// claim. It lives beside the producer so the package's own arms measure the
// phrase the producer actually emits — a reworded live line then breaks the
// assertion loudly instead of silently passing (the arms compare whole strings
// against tools.ChatGroundAnnouncement as well).
const chatGroundWriteScopeFragment = "this session writes in"
