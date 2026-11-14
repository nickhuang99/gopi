package main

// chat_ground_notice_source_test.go — WHO DECIDES THE START-UP LINE, pinned by a
// MUTATION rather than by a sentence (row 01a1a694, 2026-11-04).
//
// THE DEFECT THIS FILE EXISTS TO KEEP CLOSED. Two producers grew for one fact.
// internal/tools/chat_ground.go's ChatGroundFor sets
// ChatGroundResolution.ReadOnly from replaymode.Active() — the ONE reader of the
// flag, stated as such by its own comment — and cmd/agent/chat_ground.go's
// chatGroundNotice then called replaymode.Active() a SECOND time to choose its
// sentence. The field was therefore DEAD: a field nothing reads cannot be wrong,
// so the resolution and the line printed about it could disagree — and nothing in
// the package could tell.
//
// WHY A MUTATION AND NOT AN ASSERTION ON THE OUTPUT. "The replay line appears
// under a replay" is satisfied by EITHER implementation — the report-reading one
// and the flag-re-asking one — so it cannot distinguish them, and it never could:
// with the flag armed both answers agree. The two shapes are told apart only by
// the case where the flag and the field DISAGREE, which is exactly what these arms
// construct. Swap `report.ReadOnly` back for `replaymode.Active()` and both fail;
// that is the whole claim, and it is measured rather than described:
//
//	go test ./cmd/agent/ -run TestChatGroundNoticeReadsTheReportNotTheFlag
//
// THE FLAG IS STILL REAL, so this file does not pretend the two can diverge in
// production: startReplay arms it and ChatGroundFor copies it into the resolution
// on the way out, so a real run has them in agreement. The divergence is a
// DIAGNOSTIC — it is the only observation that names the layer, which is why the
// fixture is allowed to build it.

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/tools"
)

// chatGroundSourceRepoDir is the directory the resolved fixture stands in. It is
// never opened: the notice names it, it does not read it.
const chatGroundSourceRepoDir = "/home/user/work/gopi"

// chatGroundSourceResolved is the report an ordinary chat in a main checkout
// produces — the shape whose announcement the defect printed under a replay.
func chatGroundSourceResolved() tools.ChatGroundResolution {
	return tools.ChatGroundResolution{
		Dir:            chatGroundSourceRepoDir,
		Repo:           chatGroundSourceRepoDir,
		InMainCheckout: true,
	}
}

// chatGroundSourceMustNotClaimWrite fails the case if the line states a write —
// the same three-phrase criterion the sibling arms use, quoted from the same
// slice so the family cannot drift.
func chatGroundSourceMustNotClaimWrite(t *testing.T, line string) {
	t.Helper()
	for _, claim := range chatGroundReplayOrdinaryWriteClaims {
		if strings.Contains(line, claim) {
			t.Fatalf("the line claims a write (%q) while the REPORT says this run is read-only — the consumer "+
				"decided from something other than report.ReadOnly:\n%s", claim, line)
		}
	}
}

// TestChatGroundNoticeReadsTheReportNotTheFlag is THE mutation guard, in both
// directions. Each direction is one half of the fixed defect and each one is red
// under the old implementation:
func TestChatGroundNoticeReadsTheReportNotTheFlag(t *testing.T) {
	// AN ORDINARY FIXTURE MUST NOT LEAK the process-wide flag into a later case
	// (Rule 15: state left behind is a failure the next test reports as its own).
	replaymode.Disarm()
	t.Cleanup(replaymode.Disarm)

	t.Run("report says read-only while the flag is disarmed", func(t *testing.T) {
		// DIRECTION A — the FIELD decides. A consumer that re-asks the flag gets
		// "not replaying" here and prints the write claim; the fixed consumer
		// reads the report and prints the read-only family.
		replaymode.Disarm()
		report := chatGroundSourceResolved()
		report.ReadOnly = true
		line := chatGroundNotice(report)
		chatGroundSourceMustNotClaimWrite(t, line)
		if !strings.Contains(line, "writes NOTHING") {
			t.Fatalf("a report whose ReadOnly is true printed no read-only statement — the field was not consulted:\n%s", line)
		}
		if !strings.Contains(line, chatGroundSourceRepoDir) {
			t.Fatalf("the read-only line stopped naming the directory (%s): the replay changes the write claim, never whose tree this is:\n%s",
				chatGroundSourceRepoDir, line)
		}
	})

	t.Run("flag armed while the report says it is not a replay", func(t *testing.T) {
		// DIRECTION B — the field decides the OTHER way too, so a consumer that
		// ORs the flag in (rather than replacing it) is caught as well. This is
		// the arm that makes "one place asks the question" measurable: the
		// resolution is the answer, and the ambient flag is not a second opinion.
		replaymode.Arm(chatGroundReplayNoticeRecording)
		t.Cleanup(replaymode.Disarm)
		if !replaymode.Active() {
			t.Fatalf("replaymode.Active() is false after Arm, so this direction measures nothing")
		}
		line := chatGroundNotice(chatGroundSourceResolved())
		if !strings.Contains(line, chatGroundWriteScopeFragment) {
			t.Fatalf("the report said ReadOnly=false and the line did not carry the write claim (%q) — the consumer "+
				"asked replaymode.Active() instead of reading the report:\n%s", chatGroundWriteScopeFragment, line)
		}
		if line != tools.ChatGroundAnnouncement(chatGroundSourceResolved()) {
			t.Fatalf("the live line is no longer the producer's own announcement:\n got: %s\nwant: %s",
				line, tools.ChatGroundAnnouncement(chatGroundSourceResolved()))
		}
	})

	t.Run("the degraded reason survives a read-only report", func(t *testing.T) {
		// The suppression must not suppress the REASON a resolution failed, and
		// the replay's own recording statement must survive too — otherwise the
		// fix trades one silent claim for one silent gap (Rule 15).
		replaymode.Disarm()
		report := tools.ChatGroundResolution{
			Degraded: true,
			Detail:   tools.ChatGroundDetailDegradedLabel + "the fixture could not resolve it",
			ReadOnly: true,
		}
		line := chatGroundNotice(report)
		if !strings.Contains(line, report.Detail) {
			t.Fatalf("the degraded reason %q was dropped from a read-only run's line:\n%s", report.Detail, line)
		}
		// A DEGRADED resolution renders no announcement at all — the producer's
		// ChatGroundAnnouncement returns "" for it, and that is by design: the
		// reason IS its line, and "writes NOTHING in <dir>" cannot be printed
		// when the dir could not be resolved. What must NOT vanish is the
		// replay's own fact, which this file adds independently of the
		// announcement (see chatGroundReplayRecordingFmt).
		if !strings.Contains(line, "answered from the recording") {
			t.Fatalf("a degraded read-only run dropped the replay fact while keeping its reason — the two are separate "+
				"statements and both survive:\n%s", line)
		}
	})
}

// TestChatGroundNoticeNamesTheRecording pins the ONE fact this file still adds to
// the producer's sentence — replaymode.Source(), which is the recording's PATH and
// not the question "is this a replay". It is asserted separately so a future
// change that deleted the recording's name would fail HERE, by its own reason,
// instead of being mistaken for the read-only family going missing.
func TestChatGroundNoticeNamesTheRecording(t *testing.T) {
	replaymode.Disarm()
	t.Cleanup(replaymode.Disarm)

	recording := "/a/recording/that/is/only/named/not/read.jsonl"
	replaymode.Arm(recording)
	report := chatGroundSourceResolved()
	report.ReadOnly = true
	line := chatGroundNotice(report)
	if !strings.Contains(line, recording) {
		t.Fatalf("a replay's line did not name the recording it re-runs (%s): an operator with two recordings cannot tell "+
			"which run this was:\n%s", recording, line)
	}
}

// TestChatGroundNoticeStatesAnUnknownRecording guards the Rule-15 edge: an armed
// flag carrying no source must say so, never render an empty slot in the middle of
// the sentence ("answered from the recording , never from a live model").
func TestChatGroundNoticeStatesAnUnknownRecording(t *testing.T) {
	replaymode.Disarm()
	t.Cleanup(replaymode.Disarm)

	replaymode.Arm("")
	report := chatGroundSourceResolved()
	report.ReadOnly = true
	line := chatGroundNotice(report)
	if !strings.Contains(line, chatGroundUnknownRecording) {
		t.Fatalf("an armed replay with no recording name rendered a gap instead of stating one (%q):\n%s",
			chatGroundUnknownRecording, line)
	}
	if strings.Contains(line, "recording ,") || strings.Contains(line, "recording  ") {
		t.Fatalf("the unknown recording was rendered as an empty slot:\n%s", line)
	}
}

// silence an unused-import failure if the capture helper ever moves: the package
// reads os.Stderr through chatGroundNoticeCapture in the sibling file.
var _ = os.Stderr
