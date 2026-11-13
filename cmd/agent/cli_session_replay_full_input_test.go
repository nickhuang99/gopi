package main

// cli_session_replay_full_input_test.go — `--full`: the DEBUG spelling of a tool
// call's input.
//
// THE OPERATOR'S COMPLAINT (2026-10-29): `gopi session replay` is the human-
// readable debug view of a session's raw data, and its tool rows CUT the
// recorded input at a fixed rune bound, printing `…(+N runes)` where the rest of
// the message should be — so the one thing a debug view exists to show (what the
// model actually sent) was the one thing it abbreviated.
//
// BOTH ARMS ARE PINNED HERE, and both are driven through the REAL binary
// (PROJECT.md Rule 31): the DEFAULT view must keep the bounded spelling and its
// note byte for byte (the compact view is what fits a narrow terminal, and it is
// the contract existing readers rely on), and `--full` must print the recorded
// input's WHOLE text — compared against the log's own tool_use input rather than
// against a transcription of it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/replayplay"
)

// replayFullFlagSpelling is this test's OWN spelling of the flag, and it is a
// LITERAL on purpose.
//
// THE RED PROOF REQUIRES IT: the pre-fix tree must BUILD so that the failure is
// an assertion about the VIEW and not "undefined: sessionReplayFullFlag". A test
// that names a constant the fix introduces proves nothing on the tree it is
// supposed to be red on — the build breaks first, and a build break is not a
// measured defect. The production code takes its spelling from the constant
// (Rule 1); only this fixture types it.
const replayFullFlagSpelling = "--full"

// replayFullTestLongCommand is the recorded tool input every arm below reads.
// It is deliberately LONGER than the bounded spelling's cap, so a view that
// truncates it says so (`…(+N runes)`) and a view that expands it must carry
// these bytes. Its text is unique in the fixture, so finding it proves the full
// spelling and only the full spelling produced it.
const replayFullTestLongCommand = "sed -n '1,200p' internal/replayplay/material.go | grep -n 'roundToolInputMaxRune' | tail -3 && echo the-tail-of-the-recorded-command-that-no-bounded-row-can-carry"

// replayFullTestSession writes a one-turn session whose single tool call carries
// replayFullTestLongCommand as its input.
//
// It is a SEPARATE fixture from replayTestSession (not an edit of it) because
// that one's short input is what the other arms of this package measure their
// own thing on: widening it would turn a dozen unrelated assertions into
// measurements of the cap.
func replayFullTestSession(t *testing.T) (profileRoot, sessionID string) {
	t.Helper()
	home := t.TempDir()
	sessionID = "test-session-full-0001"
	dir := filepath.Join(config.ProfilePathIn(home, config.SessionsDirName), sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the session dir: %v", err)
	}
	input := map[string]any{"command": replayFullTestLongCommand}
	blob, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal the tool input: %v", err)
	}
	// THE WRITER'S OWN ORDER (user, assistant, request, tool_result): the
	// `request` row CLOSES the exchange that ran the tool, so it comes after the
	// assistant row that named the call.
	log := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"read the replay material"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"bash","input":` + string(blob) + `}]}}`,
		`{"type":"request","iteration":0}`,
		`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, replayplay.SessionJSONLName), []byte(log), 0o600); err != nil {
		t.Fatalf("write the timeline: %v", err)
	}
	return home, sessionID
}

// TestSessionReplayFullPrintsTheRecordedInputWhole is the RED/GREEN pair the
// change ships with: the default arm is what the tree did BEFORE (`…(+N runes)`,
// no tail), and the --full arm is the delivered behaviour (the whole recorded
// command, no note).
//
// The tail is asserted as a BYTE SUBSTRING of the view, never as "the row is
// longer": a row can be long for a hundred reasons (the result, the prompt, a
// wrap), and only the recorded text proves the INPUT was expanded. The command
// is whitespace-normalised on both sides before comparing, because the view
// wraps `k=v` at the canvas width (a wrap inserts a line break and the row's
// own `↳ ` continuation mark) while the log holds it on one line.
func TestSessionReplayFullPrintsTheRecordedInputWhole(t *testing.T) {
	home, id := replayFullTestSession(t)

	bounded, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "1")
	if err != nil {
		t.Fatalf("--turn 1 failed: %v\n%s", err, bounded)
	}
	if !strings.Contains(bounded, "…(+") {
		t.Fatalf("the DEFAULT view no longer says it cut the input (`…(+N runes)`) — that note is the contract %s must not change:\n%s",
			replayFullFlagSpelling, bounded)
	}
	if flat := flattenReplayCells(bounded); strings.Contains(flat, replayFullTestCommandTail()) {
		t.Fatalf("the DEFAULT view already carries the recorded input's tail %q — the compact spelling is supposed to cut it",
			replayFullTestCommandTail())
	}

	full, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "1", replayFullFlagSpelling)
	if err != nil {
		t.Fatalf("--turn 1 %s failed: %v\n%s", replayFullFlagSpelling, err, full)
	}
	if flat := flattenReplayCells(full); !strings.Contains(flat, replayFullTestCommandTail()) {
		t.Fatalf("%s does not print the recorded input in full: the row is missing the recording's tail %q\n%s",
			replayFullFlagSpelling, replayFullTestCommandTail(), full)
	}
	if flat := flattenReplayCells(full); !strings.Contains(flat, flattenReplayCells(replayFullTestLongCommand)) {
		t.Fatalf("%s does not print the recorded input VERBATIM (whitespace-normalised):\nwant %q\ngot:\n%s",
			replayFullFlagSpelling, replayFullTestLongCommand, full)
	}
	if strings.Contains(full, "…(+") {
		t.Fatalf("%s still carries a truncation note — the whole input is printed, so there is nothing to count:\n%s",
			replayFullFlagSpelling, full)
	}
}

// replayFullTestCommandTail is the LAST segment of the recorded command: the
// bytes no bounded row can reach. It is derived from the command, never typed a
// second time, so a boundary that moves changes both sides at once.
func replayFullTestCommandTail() string {
	fields := strings.Fields(replayFullTestLongCommand)
	return fields[len(fields)-1]
}

// flattenReplayCells is the view with all whitespace removed AND every row's
// leading continuation mark stripped: the comparison the epic names
// ("whitespace-normalised"), applied to the WHOLE output so a wrap cannot turn a
// correct expansion into a false negative.
//
// THE MARK HAS TO GO, and it is not whitespace. A wrapped row opens with
// internal/lineedit's `↳ ` (the discipline toolRows/roundRows keep so a
// continuation is never mistaken for a new call), so a naive "delete the spaces"
// leaves a `↳` standing in the MIDDLE of the recorded text and a verbatim
// comparison fails on a view that is correct — the false negative this helper
// exists to avoid. Only the LEADING run of marks is dropped: a `↳` the recording
// itself contains stays, because dropping it would make the comparison pass for
// the wrong reason.
func flattenReplayCells(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), strings.TrimSpace(lineedit.NoteContinuation))
		for _, r := range line {
			switch r {
			case ' ', '\t', '\r':
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
