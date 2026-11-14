package main

// chat_ground_replay_notice_test.go — A REPLAY DOES NOT "WRITE IN" ANY DIRECTORY,
// SO IT MUST NOT SAY THAT IT DOES (row 01a1a629-62f4, the measured defect in
// /home/user/.gopi/sessions/01a1a629-9dfb-7a57-baa6-cc824db453a6's attachments).
//
// THE DEFECT, AS MEASURED. `gopi chat --replay <id>` is a read-only re-run: under
// internal/replaymode.Active() the run mints no session (internal/agent/
// session.go), opens no raw log (internal/agent/rawlog.go), registers no chat
// slot and writes no `chats` row. And yet its stderr carried
//
//	[gopi] chat: this session writes in /home/user/work/gopi — the MAIN CHECKOUT
//	of /home/user/work/gopi. No worktree and no branch of its own: every
//	read/write/edit/bash round lands uncommitted in that checkout's working area.
//
// — a sentence about an event the run cannot produce. The producer's criterion was
// "is the standing directory a repository's main checkout", which is the RIGHT
// question for a live session and the WRONG one for a replay: a replay's tool
// round is answered from the recording (installReplayToolExecutor), so no round
// of it lands anywhere.
//
// WHAT THIS FILE ASSERTS, and what it deliberately does NOT. The notice function
// decides from ONE fact — tools.ChatGroundResolution.ReadOnly, the field the
// producer sets — and the write-scope sentences are untouched for a live run: the
// non-replay arm below pins them WORD FOR WORD against the producer's constants,
// so a future edit that reworded the live line would have to come here and say so.
// Nothing here asserts anything about the live line's CONTENT beyond that
// identity, because that content is not this row's subject.
//
// THE FIXTURE CARRIES THE VERDICT, AND IT DID NOT USE TO (2026-11-04 correction).
// These arms used to arm the process-wide flag and then hand the notice a
// hand-built resolution that never read it — which encoded the DEFECT as the
// expectation: a caller that asks replaymode.Active() itself, behind the
// producer's back. The report is the input, so the replay arms below build a
// report that SAYS it is read-only (chatGroundReplayReadOnly(report)), and the
// producer's own field is exercised where it belongs
// (TestChatGroundForCarriesTheReadOnlyFactItself).

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/tools"
)

// chatGroundReplayNoticeRecording is the recording this case arms the process
// with. It is never opened: the notice names it, it does not read it.
const chatGroundReplayNoticeRecording = "/a/recording/that/does/not/exist.jsonl"

// chatGroundReplayNoticeDir is the directory the notice would have called the
// session's write target.
const chatGroundReplayNoticeDir = "/home/user/work/gopi"

// chatGroundReplayTestResolved is the resolved report a live chat in a repo's
// main checkout produces — the exact shape whose announcement was the defect.
func chatGroundReplayTestResolved() tools.ChatGroundResolution {
	return tools.ChatGroundResolution{
		Dir:            chatGroundReplayNoticeDir,
		Repo:           chatGroundReplayNoticeDir,
		InMainCheckout: true,
	}
}

// chatGroundReplayReadOnly returns the SAME resolution the producer answers for a
// replay: every filesystem fact unchanged, and ReadOnly set. It is what
// tools.ChatGroundFor returns under `--replay`, built by flipping the one field
// so a change to the other facts cannot hide inside this helper.
func chatGroundReplayReadOnly(g tools.ChatGroundResolution) tools.ChatGroundResolution {
	g.ReadOnly = true
	return g
}

// TestChatGroundNoticeSuppressesTheWriteScopeLineUnderReplay is the RED→GREEN
// assertion of the row: with the process armed as a replay, the notice carries no
// write-scope claim, and it carries the TRUE statement instead (the one the
// operator can act on: this run re-runs a recording and touches no checkout).
func TestChatGroundNoticeSuppressesTheWriteScopeLineUnderReplay(t *testing.T) {
	// THE FLAG IS PROCESS-WIDE, so the disarm is not optional: a leak here would
	// silently rewrite every later case in this package (Rule 15 — a state left
	// behind is a failure the next test reports as its own).
	replaymode.Disarm()
	t.Cleanup(replaymode.Disarm)

	live := chatGroundNotice(chatGroundReplayTestResolved())
	if !strings.Contains(live, chatGroundWriteScopeFragment) {
		t.Fatalf("the LIVE notice no longer carries %q — the write-scope line is this row's untouched baseline:\n%s",
			chatGroundWriteScopeFragment, live)
	}
	if live != tools.ChatGroundAnnouncement(chatGroundReplayTestResolved()) {
		t.Fatalf("the live notice is no longer the producer's own announcement, so a reworded live line would pass this file unnoticed:\n got: %s\nwant: %s",
			live, tools.ChatGroundAnnouncement(chatGroundReplayTestResolved()))
	}

	got := chatGroundNotice(chatGroundReplayReadOnly(chatGroundReplayTestResolved()))
	if strings.Contains(got, chatGroundWriteScopeFragment) {
		t.Fatalf("a REPLAY still claims a write scope (%q):\n%s", chatGroundWriteScopeFragment, got)
	}
	// The replacement must be INFORMATION, not silence: the standing directory
	// and the statement that this run writes nothing are the two facts a reader
	// of that line needs.
	for _, want := range []string{chatGroundReplayNoticeDir, "writes NOTHING", "REPLAY"} {
		if !strings.Contains(got, want) {
			t.Errorf("the replay notice must carry %q (Rule 15: a suppressed line is replaced by a true one, never by nothing):\n%s", want, got)
		}
	}
}

// TestChatGroundNoticeKeepsTheDegradedReasonUnderReplay is the other half: a
// replay whose standing directory cannot be resolved still says so. Suppressing
// the write-scope line must not also suppress the REASON a resolution failed —
// that would trade one silent claim for one silent gap.
func TestChatGroundNoticeKeepsTheDegradedReasonUnderReplay(t *testing.T) {
	replaymode.Disarm()
	t.Cleanup(replaymode.Disarm)

	degraded := tools.ChatGroundResolution{Degraded: true, Detail: tools.ChatGroundDetailDegradedLabel + "the fixture could not resolve it"}
	if live := chatGroundNotice(degraded); live != tools.ChatGroundDegradedNotice(degraded) {
		t.Fatalf("the live degraded notice changed:\n got: %s\nwant: %s", live, tools.ChatGroundDegradedNotice(degraded))
	}

	got := chatGroundNotice(chatGroundReplayReadOnly(degraded))
	if strings.Contains(got, chatGroundWriteScopeFragment) {
		t.Fatalf("a degraded REPLAY still claims a write scope:\n%s", got)
	}
	if !strings.Contains(got, degraded.Detail) {
		t.Errorf("the replay notice dropped the degradation reason %q:\n%s", degraded.Detail, got)
	}
}
