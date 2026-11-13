package main

// cli_uistate_compare_exit_contract_test.go — the compare's EXIT CONTRACT and the
// SESSION-AWARE selection, both asserted through the verb's own entry point (the
// caller's view: stdout, stderr, exit code), never through a helper the code and
// the test would agree on by construction.
//
// WHAT THIS FILE PINS, and why each case exists as a test rather than as a
// comment:
//
//	① a check that COULD NOT RUN exits 1 — not 0, and NOT the disagreement code 3.
//	   Measured defect (2026-09-27, real binary): the "no transcript lines were
//	   available … no comparison can be claimed" compare exited 3, the code that
//	   means "the two sides disagree". A script branching on 3 concluded the paint
//	   had lost the line, from a check that never read the authority.
//	② the printed outcome WORD is the contract's own, on the same line as the exit
//	   code's cause, so a reader (an operator, or an agent reading its own output)
//	   never has to infer could-not-run from `match=false`.
//	③ a SINGLE candidate is selected with no --pid: the old verb refused it.
//	④ a NAMED SESSION selects the dump that belongs to it, among many: the
//	   deterministic disambiguator twelve exited chat dumps were missing.
//	⑤ the run-uuid LAYOUT resolves: a dump written under the session dir the TASK
//	   MACHINERY uses derives its transcript, where the bare-layout-only rule
//	   derived nothing.
//	⑥ an explicit --session that cannot be read is reported as the caller's OWN
//	   operand — not as "derived from the dump's own session directory", with no
//	   swallowed `<nil>`.
//
// The vocabulary strings are spelled LITERALLY here, not read from the
// implementation: they are the operator-facing contract, and a test that took
// them from the code could not notice the contract moving.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/uiclient"
)

// The contract's words, spelled as a caller reads them.
const (
	compareWordCouldNotRun = "could-not-run"
	compareWordAgreed      = "agreed"
	compareWordDisagreed   = "disagreed"
)

// runUUID is a session id in the task-run layout's shape: the file is named for
// the RUN uuid, not for the session directory.
const runUUID = "82e9613e-c26b-4382-a7b6-ebbed154735a"

// TestUIStateCompare_CouldNotRunExitsOneNotTheDisagreementCode is ① — the exit
// contract's core case, reached through the verb.
//
// The fixture is the measured one: a dump that IS readable and painted (so the
// check gets past every grid refusal and actually reads an authority), whose
// session log holds a session_start and a user turn but NO assistant text. The
// authoritative side exists and is EMPTY, so no comparison can be claimed.
func TestUIStateCompare_CouldNotRunExitsOneNotTheDisagreementCode(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	sessionDir := filepath.Join(gopiDir, "sessions", compareSessionID)
	path := compareDumpIn(t, sessionDir, uiclient.GeometrySourceLayout, compareLine)
	// A session log with no assistant text block: readable, and empty of the
	// authority (`gopi session log <id> --response --text` would print nothing).
	writeCompareSessionLog(t, filepath.Join(sessionDir, compareSessionFileName), nil)

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, filepath.Join(sessionDir, compareSessionFileName),
	})
	if code == uiStateExitCompareMismatch {
		t.Fatalf("exit = %d (the DISAGREEMENT code) for a check that could not run: a script branching on it concludes the paint lost the line\noutput:\n%s", code, out)
	}
	if code != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d, want %d (could not run)\noutput:\n%s", code, uiStateExitCouldNotRun, out)
	}
	if code == 0 {
		t.Fatalf("exit = 0 for an unchecked screen — an unchecked screen is not a verified one:\n%s", out)
	}
	// ② the outcome WORD is printed, not merely implied by the code.
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "outcome"); got != compareWordCouldNotRun {
		t.Fatalf("outcome=%q, want %q — the word is the contract, and `match=false` alone reads as a disagreement\nheader:\n%s",
			got, compareWordCouldNotRun, header)
	}
	if !strings.Contains(out, string(uiclient.CompareNoTranscript)) {
		t.Fatalf("the verdict does not say the transcript side was unusable:\n%s", out)
	}
	_ = path
}

// TestUIStateCompare_SingleCandidateNeedsNoPID is ③: one dump on hand is one
// screen, so the compare runs — the old verb refused an ambiguity over a single
// candidate.
func TestUIStateCompare_SingleCandidateNeedsNoPID(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	out, code := uiStateRun(t, []string{uiStateFlagCompare, compareLine})
	if code == uiStateExitAmbiguous {
		t.Fatalf("exit = %d (ambiguity) with a SINGLE dump on hand\noutput:\n%s", code, out)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for a single candidate whose line is on both sides\noutput:\n%s", code, out)
	}
	if got := compareHeaderField(t, compareHeaderLine(t, out, "#gopi-ui-state-compare"), "outcome"); got != compareWordAgreed {
		t.Fatalf("outcome=%q, want %q\noutput:\n%s", got, compareWordAgreed, out)
	}
	_ = path
}

// TestUIStateCompare_NamesTheSessionToDisambiguate is ④ — the missing
// disambiguator, measured on this host with twelve exited chat dumps: the caller
// names the session it means, and the dump belonging to THAT session is the
// target. No --pid, no ambiguity refusal, and the picked artifact is reported so
// the reader can see which one answered.
func TestUIStateCompare_NamesTheSessionToDisambiguate(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	sessions := filepath.Join(gopiDir, "sessions")

	// THREE candidate dumps, each in its own session directory, each with its own
	// transcript. The middle one carries the line under test; the others do not —
	// so a verb that picked the wrong one could not answer "agreed".
	wantDir := filepath.Join(sessions, "bbbbbbbb-1111-4222-8333-444444444444")
	for _, d := range []string{
		filepath.Join(sessions, "aaaaaaaa-1111-4222-8333-444444444444"),
		wantDir,
		filepath.Join(sessions, "cccccccc-1111-4222-8333-444444444444"),
	} {
		compareDumpIn(t, d, uiclient.GeometrySourceLayout, compareLine)
		writeCompareSessionLog(t, filepath.Join(d, compareSessionFileName), []string{compareOtherLine})
	}
	// The wanted session's own transcript DOES carry the line, and its dump
	// belongs to it.
	writeCompareSessionLog(t, filepath.Join(wantDir, compareSessionFileName), []string{compareLine})

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, filepath.Join(wantDir, compareSessionFileName),
	})
	if code == uiStateExitAmbiguous {
		t.Fatalf("exit = %d (ambiguity) although the caller named the session it means\noutput:\n%s", code, out)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0: the named session's own dump + transcript hold the line\noutput:\n%s", code, out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "dump"); !strings.Contains(got, wantDir) {
		t.Fatalf("the check ran against %q, want the dump inside %q\nheader:\n%s", got, wantDir, header)
	}
	if got := compareHeaderField(t, header, "outcome"); got != compareWordAgreed {
		t.Fatalf("outcome=%q, want %q\nheader:\n%s", got, compareWordAgreed, header)
	}

	// …and a session that owns NO dump is reported as the session fact — naming
	// the sessions the dumps on hand DO belong to — never as a bare surface
	// ambiguity the caller did not ask about.
	stranger := filepath.Join(sessions, "dddddddd-1111-4222-8333-444444444444")
	writeCompareSessionLogAt(t, stranger, []string{compareLine})
	out2, code2 := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, filepath.Join(stranger, compareSessionFileName),
	})
	if code2 != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d for a session owning no dump, want %d\noutput:\n%s", code2, uiStateExitCouldNotRun, out2)
	}
	for _, want := range []string{wantDir, "belongs to session"} {
		if !strings.Contains(out2, want) {
			t.Fatalf("the refusal does not carry %q (it must name the sessions the dumps belong to):\n%s", want, out2)
		}
	}
}

// TestUIStateCompare_ResolvesTheRunUUIDLayout is ⑤ — the layout the task
// machinery itself uses: <session dir>/<run uuid>.jsonl with the dump one level
// down (<session dir>/<artifact dir>/chat-screen.dump). The old derivation tried
// exactly two names (<dir>/session.jsonl, <dir>/../session.jsonl), both of which
// are ABSENT here, so the one-call check refused the surface the caller was
// looking at.
func TestUIStateCompare_ResolvesTheRunUUIDLayout(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	sessionDir := filepath.Join(gopiDir, "sessions", compareSessionID)
	compareDumpIn(t, filepath.Join(sessionDir, "run1"), uiclient.GeometrySourceLayout, compareLine)
	writeCompareSessionLog(t, filepath.Join(sessionDir, runUUID+".jsonl"), []string{compareLine})

	// The two layouts the OLD rule knew are provably absent: without them the old
	// derivation found nothing, which is the defect this case exists to keep out.
	for _, legacy := range []string{
		filepath.Join(sessionDir, "run1", compareSessionFileName),
		filepath.Join(sessionDir, compareSessionFileName),
	} {
		if _, err := os.Stat(legacy); err == nil {
			t.Fatalf("the fixture still has %s: it no longer exercises the task-run layout", legacy)
		}
	}

	out, code := uiStateRun(t, []string{uiStateFlagCompare, compareLine})
	if code != 0 {
		t.Fatalf("exit = %d, want 0: the run-uuid layout must resolve without --session\noutput:\n%s", code, out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "session_file"); !strings.HasSuffix(got, runUUID+".jsonl") {
		t.Fatalf("session_file=%q, want the run-uuid record beside the dump\nheader:\n%s", got, header)
	}
	if got := compareHeaderField(t, header, "transcript_lines"); got == "0" {
		t.Fatalf("transcript_lines=0: the derived authority was not read\nheader:\n%s", header)
	}
}

// TestUIStateCompare_ExplicitSessionFailureNamesTheCallersOwnOperand is ⑥: an
// unreadable authority the CALLER named is reported as their argument — with the
// operand quoted and a reason, and with the word could-not-run — never with the
// derived-path sentence (a false statement about their own flag) and never with
// a swallowed `<nil>` (Rule 15).
func TestUIStateCompare_ExplicitSessionFailureNamesTheCallersOwnOperand(t *testing.T) {
	compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	const bogus = "no-such-session-id-here"
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, bogus,
	})
	if code != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d, want %d for an unreadable authority\noutput:\n%s", code, uiStateExitCouldNotRun, out)
	}
	if !strings.Contains(out, bogus) {
		t.Fatalf("the failure does not name the operand the caller typed (%q):\n%s", bogus, out)
	}
	if strings.Contains(out, "derived from the dump") {
		t.Fatalf("an operand the CALLER typed is reported as DERIVED — a false statement about their own flag:\n%s", out)
	}
	if strings.Contains(out, "<nil>") {
		t.Fatalf("the failure carries a swallowed `<nil>` (Rule 15: a failure must say why):\n%s", out)
	}
	// The reason is present, and it is the lookup's own sentence — not an empty
	// path and not a bare errno.
	for _, want := range []string{"no session matches that id", "cannot read the transcript"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the failure does not carry %q:\n%s", want, out)
		}
	}
	if got := compareHeaderField(t, compareHeaderLine(t, out, "#gopi-ui-state-compare"), "outcome"); got != compareWordCouldNotRun {
		t.Fatalf("outcome=%q, want %q\noutput:\n%s", got, compareWordCouldNotRun, out)
	}

	// The same contract for a PATH operand: the caller's own spelling, with the
	// open failure as the reason. The fixture dump is the single candidate on
	// hand, so the name identifies no dump — the documented OVERRIDE case — and
	// the check still runs and reports the unreadable authority the caller named.
	missing := filepath.Join(filepath.Dir(compareTranscriptPath(compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine))), "not-there.jsonl")
	out2, code2 := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, missing,
	})
	if code2 != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d, want %d for a missing transcript path\noutput:\n%s", code2, uiStateExitCouldNotRun, out2)
	}
	for _, want := range []string{missing, "no such file or directory"} {
		if !strings.Contains(out2, want) {
			t.Fatalf("the failure does not carry %q:\n%s", want, out2)
		}
	}
	if strings.Contains(out2, "<nil>") || strings.Contains(out2, "derived from the dump") {
		t.Fatalf("the path operand's failure misreports its own argument:\n%s", out2)
	}
}

// writeCompareSessionLogAt writes a session record into a session directory,
// creating the directory first (a fixture session whose only artifact is the log
// itself — the shape the --session-name cases need).
func writeCompareSessionLogAt(t *testing.T, dir string, lines []string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir the session dir %s: %v", dir, err)
	}
	path := filepath.Join(dir, compareSessionFileName)
	writeCompareSessionLog(t, path, lines)
	return path
}

// compareDumpIn arms the product's sink at the given directory and paints the
// line, returning the dump path. It is the fixture used by the cases above that
// need control over WHERE the dump sits (the layouts are the point of several).
func compareDumpIn(t *testing.T, dir string, geometry uiclient.GeometrySource, painted string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path:     path,
		Cols:     dumpGeomPtyCols,
		Rows:     dumpGeomPtyRows,
		Geometry: geometry,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			ColsRows:         [2]int{dumpGeomPtyCols, dumpGeomPtyRows},
			TerminalColsRows: [2]int{dumpGeomPtyCols, dumpGeomPtyRows},
			Started:          liveStartStamp(t),
		},
	})
	if err != nil {
		t.Fatalf("arm the fixture sink at %s: %v", path, err)
	}
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + painted + "\r\n")); err != nil {
		t.Fatalf("paint the fixture dump: %v", err)
	}
	time.Sleep(uiStateFixtureRefreshWait) //stylegate:allow-sleep waits out the fixture's repaint before the reader re-observes the same grid
	if _, err := d.Write([]byte("\x1b[H" + painted + "\x1b[3;1Hstatus: idle\r\n")); err != nil {
		t.Fatalf("repaint the fixture dump: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("finalize the fixture dump: %v", err)
	}
	return path
}
