package main

// cli_screen_replay_verb_test.go — the TOP-LEVEL verb's own proof.
//
// The arms are chosen so the two things this file ADDS over the sub-verb are
// the two things measured: (1) the operand shape (`--verify A B`, two named
// recordings, a non-zero status on divergence) and (2) that the WRITE arm is
// the sub-verb's own writer rather than a second one — asserted by comparing the
// two doors' stdout, which is the only way "one implementation" can be checked
// instead of promised.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScreenReplayVerbWritesTheSameBytesAsTheSubVerb is the Rule 14 arm: the
// two doors must produce the SAME bytes for the same recording. It runs both and
// compares, so a future edit that re-implements the frame walk here fails here.
func TestScreenReplayVerbWritesTheSameBytesAsTheSubVerb(t *testing.T) {
	dir := t.TempDir()
	id := "01a1b90e-sr-verb"
	path := writeTestScreenStream(t, dir, id, testScreenStream{frames: testScreenFrames(), trailer: true})

	var topOut, topErr bytes.Buffer
	if code := runScreenReplayVerb([]string{path}, &topOut, &topErr); code != chatExitOK {
		t.Fatalf("verb exit = %d, want %d (stderr: %s)", code, chatExitOK, topErr.String())
	}
	var subOut, subErr bytes.Buffer
	if code := screenReplayRun([]string{path}, &subOut, &subErr); code != chatExitOK {
		t.Fatalf("sub-verb exit = %d, want %d (stderr: %s)", code, chatExitOK, subErr.String())
	}
	if !bytes.Equal(topOut.Bytes(), subOut.Bytes()) {
		t.Fatalf("the two doors disagree about the bytes:\n top %q\n sub %q", topOut.Bytes(), subOut.Bytes())
	}
	// AND both are the recording itself, not merely equal to each other — two
	// equally-wrong writers would pass the comparison above.
	if want := bytes.Join(testScreenFrames(), nil); !bytes.Equal(topOut.Bytes(), want) {
		t.Fatalf("stdout is not the recording:\n got %q\nwant %q", topOut.Bytes(), want)
	}
	// stdout carries the recording and NOTHING else: the counts and the ending
	// sentence are stderr's, so a redirected stdout never gains a word.
	if bytes.Contains(topOut.Bytes(), []byte("frames=")) {
		t.Fatalf("the counts line must not reach stdout, got %q", topOut.String())
	}
	if !strings.Contains(topErr.String(), "frames=") {
		t.Fatalf("stderr must state the counts it wrote, got %q", topErr.String())
	}
}

// TestScreenReplayVerbResolvesASessionID drives the SESSION-ID route, which is
// the operand an operator actually types, through the profile tree the resolver
// walks (the same place `gopi session log` looks).
func TestScreenReplayVerbResolvesASessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	id := "01a1b90e-sr-verb-sid"
	dir := filepath.Join(home, ".gopi", "sessions", id)
	writeTestScreenStream(t, dir, id, testScreenStream{frames: testScreenFrames(), trailer: true})

	var out, errw bytes.Buffer
	if code := runScreenReplayVerb([]string{id}, &out, &errw); code != chatExitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, chatExitOK, errw.String())
	}
	if want := bytes.Join(testScreenFrames(), nil); !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("stdout is not the recording:\n got %q\nwant %q", out.Bytes(), want)
	}
}

// TestScreenReplayVerbVerifyIdentical: the identical answer is ONE counts line,
// exit 0. The counts must be the MEASURED ones, so the assertion recomputes them
// from the fixture rather than reading the trailer.
func TestScreenReplayVerbVerifyIdentical(t *testing.T) {
	dir := t.TempDir()
	a := writeTestScreenStream(t, dir, "a", testScreenStream{frames: testScreenFrames(), trailer: true})
	// B is a SEPARATE FILE with the same frames: the comparison is over frames,
	// not over file identity, which is the whole point of `--verify`.
	b := writeTestScreenStream(t, dir, "b", testScreenStream{frames: testScreenFrames(), trailer: true})
	if a == b {
		t.Fatalf("fixture error: the two sides are the same path (%s)", a)
	}

	var out, errw bytes.Buffer
	if code := runScreenReplayVerb([]string{screenReplayVerbVerifyFlag, a, b}, &out, &errw); code != chatExitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, chatExitOK, errw.String())
	}
	want := fmt.Sprintf(screenReplayVerbIdenticalLine, len(testScreenFrames()), payloadBytesOf(testScreenFrames()))
	if !strings.Contains(out.String(), want) {
		t.Fatalf("an identical comparison must print %q, got %q", want, out.String())
	}
	if strings.Contains(out.String(), "differ:") {
		t.Fatalf("an identical comparison must not report a divergence, got %q", out.String())
	}
}

// TestScreenReplayVerbVerifyOneByteChange: a single changed byte is a divergence,
// and the answer names the FRAME, the OFFSET of that frame in each file, and the
// payloads as context. Exit status is non-zero — the reason this door exists
// (a script pipes it into a branch).
func TestScreenReplayVerbVerifyOneByteChange(t *testing.T) {
	dir := t.TempDir()
	frames := testScreenFrames()
	a := writeTestScreenStream(t, dir, "a", testScreenStream{frames: frames, trailer: true})
	mutated := make([][]byte, len(frames))
	for i, f := range frames {
		mutated[i] = append([]byte(nil), f...)
	}
	// ONE byte, in the SECOND frame: the answer must name frame 2, and the
	// offsets it prints must be the offsets of frame 2 in each file.
	mutated[1][0] = 'R'
	b := writeTestScreenStream(t, dir, "b", testScreenStream{frames: mutated, trailer: true})

	var out, errw bytes.Buffer
	code := runScreenReplayVerb([]string{screenReplayVerbVerifyFlag, a, b}, &out, &errw)
	if code == chatExitOK {
		t.Fatalf("a measured divergence must exit non-zero, got %d (stdout: %q)", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "differ: frame 2") {
		t.Fatalf("the divergence must name frame 2, got %q", text)
	}
	// BOTH sides' offsets are printed, and they are DIFFERENT byte positions in
	// the file (frame 1 differs in length from frame 0, so a single offset pair
	// copied from one side would be wrong for the other).
	for _, label := range []string{"  A: frame 2 at byte offset ", "  B: frame 2 at byte offset "} {
		if !strings.Contains(text, label) {
			t.Fatalf("the divergence must print %q, got %q", strings.TrimSpace(label), text)
		}
	}
	// The context is the payload, escaped the same way the sub-verb escapes it:
	// the differing byte must be visible as the payload it is.
	if !strings.Contains(text, "R") {
		t.Fatalf("the divergence must show the payload that differs, got %q", text)
	}
	if strings.Contains(text, "identical:") {
		t.Fatalf("a divergence must not also claim identity, got %q", text)
	}
}

// TestScreenReplayVerbVerifyFrameCountMismatch: a file that ENDS before the
// other is not an empty frame — the missing side is named as missing, which is
// the distinction uiclient.SgrFrameDivergence documents with its -1 offset.
func TestScreenReplayVerbVerifyFrameCountMismatch(t *testing.T) {
	dir := t.TempDir()
	full := testScreenFrames()
	a := writeTestScreenStream(t, dir, "a", testScreenStream{frames: full, trailer: true})
	b := writeTestScreenStream(t, dir, "b", testScreenStream{frames: full[:1], trailer: true})

	var out, errw bytes.Buffer
	code := runScreenReplayVerb([]string{screenReplayVerbVerifyFlag, a, b}, &out, &errw)
	if code == chatExitOK {
		t.Fatalf("a frame-count mismatch must exit non-zero, got %d (stdout: %q)", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "no frame at position") {
		t.Fatalf("the shorter side must be named as MISSING, got %q", text)
	}
	if strings.Contains(text, "at byte offset -1") {
		t.Fatalf("a missing frame must not be reported as an offset, got %q", text)
	}
}

// TestScreenReplayVerbRefusesBadOperandCounts: the flag is a MODE over two
// operands, so 0, 1 and 3 recordings are each refused BY NAME with the shape the
// caller should have typed — a silent acceptance would make `--verify A` mean
// "compare A with nothing" and print a divergence nobody measured.
func TestScreenReplayVerbRefusesBadOperandCounts(t *testing.T) {
	dir := t.TempDir()
	a := writeTestScreenStream(t, dir, "a", testScreenStream{frames: testScreenFrames(), trailer: true})
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no operands", nil, "no recording was given"},
		{"verify with one", []string{screenReplayVerbVerifyFlag, a}, "were given, want 2"},
		{"verify with three", []string{screenReplayVerbVerifyFlag, a, a, a}, "were given, want 2"},
		{"write with two", []string{a, a}, "were given, want 1"},
		{"unknown flag", []string{"--nope", a}, "unknown flag"},
		{"verify given a value", []string{screenReplayVerbVerifyFlag + "=" + a, a}, "takes no value"},
	} {
		var out, errw bytes.Buffer
		code := runScreenReplayVerb(tc.args, &out, &errw)
		if code == chatExitOK {
			t.Fatalf("%s: exit = 0, want non-zero", tc.name)
		}
		if out.Len() != 0 {
			t.Fatalf("%s: a refusal must write NOTHING to stdout, got %q", tc.name, out.String())
		}
		if !strings.Contains(errw.String(), tc.want) {
			t.Fatalf("%s: refusal must say %q, got %q", tc.name, tc.want, errw.String())
		}
		if !strings.Contains(errw.String(), screenReplayVerbSynopsis) {
			t.Fatalf("%s: refusal must carry the usage line, got %q", tc.name, errw.String())
		}
	}
}

// TestScreenReplayVerbRefusesAnUnreadableOperand: the "could not compare" state
// keeps a non-zero status but NEVER reports a divergence it did not measure —
// the distinction between a negative answer and no answer.
func TestScreenReplayVerbRefusesAnUnreadableOperand(t *testing.T) {
	dir := t.TempDir()
	a := writeTestScreenStream(t, dir, "a", testScreenStream{frames: testScreenFrames(), trailer: true})
	var out, errw bytes.Buffer
	code := runScreenReplayVerb([]string{screenReplayVerbVerifyFlag, a, filepath.Join(dir, "absent.screen.stream")}, &out, &errw)
	if code == chatExitOK {
		t.Fatalf("exit = 0, want non-zero")
	}
	if strings.Contains(out.String(), "differ:") {
		t.Fatalf("a comparison that could not run must not report a divergence, got %q", out.String())
	}
	if !strings.Contains(errw.String(), "B:") {
		t.Fatalf("the refusal must name WHICH side failed, got %q", errw.String())
	}
}

// TestScreenReplayVerbRefusesASessionWithNoStream: the refusal reaches the
// operator on stderr with nothing on stdout, through the SAME resolver — a
// fallback to the renderer's dump would answer a different question in the same
// shape.
func TestScreenReplayVerbRefusesASessionWithNoStream(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	id := "01a1b90e-sr-verb-nostream"
	if err := os.MkdirAll(filepath.Join(home, ".gopi", "sessions", id), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var out, errw bytes.Buffer
	if code := runScreenReplayVerb([]string{id}, &out, &errw); code != chatExitFailed {
		t.Fatalf("exit = %d, want %d", code, chatExitFailed)
	}
	if out.Len() != 0 {
		t.Fatalf("a refusal must write NOTHING to stdout, got %q", out.String())
	}
	if !strings.Contains(errw.String(), screenReplayVerbErrorPrefix) {
		t.Fatalf("the refusal must carry the verb's prefix, got %q", errw.String())
	}
}

// TestScreenReplayVerbHelpIsItsOwnPage pins the page's head and its two modes.
// The contract suite fingerprints the same head against the BUILT binary; this
// arm keeps the page's CONTENT honest, which a fingerprint cannot do.
func TestScreenReplayVerbHelpIsItsOwnPage(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runScreenReplayVerb([]string{HelpFlag}, &out, &errw); code != chatExitOK {
		t.Fatalf("exit = %d, want %d", code, chatExitOK)
	}
	text := out.String()
	if !strings.HasPrefix(text, screenReplayVerbUsageHead) {
		t.Fatalf("page must open with its head %q, got %q", screenReplayVerbUsageHead, firstLine(text))
	}
	for _, want := range []string{screenReplayVerbSynopsis, screenReplayVerbVerifyFlag + " A B", "STDERR", "EXIT STATUS"} {
		if !strings.Contains(text, want) {
			t.Fatalf("page must carry %q, got:\n%s", want, text)
		}
	}
	// A page that taught the group hop would send the reader back to the address
	// this verb exists to replace.
	if !strings.Contains(text, "gopi "+CliVerbScreen+" "+screenSubcmdReplay) {
		t.Fatalf("page must name the sub-verb it shares its reader with, got:\n%s", text)
	}
}

// TestScreenReplayVerbWordIsNotTheAbolishedOne is the guard for the one mistake
// this verb's NAME invites: the top-level word `replay` was abolished, and
// session_replay_abolition_test.go pins that on four surfaces (vocabulary, census
// row, built binary, source identifier). The assertions here are local and blunt
// — the constant must be the agreed spelling, must not BE the abolished word,
// and the dispatch comparison must be on the WHOLE word, so the family verb
// `screen` and the abolished leaf `replay` can never reach this reader.
//
// The dispatched predicate itself is not called here: it exits the process on a
// match (every cli predicate does). The live-binary arm that proves the app
// actually dispatches it is the contract suite's
// `{argv: CliVerbScreenReplay, args: {CliVerbScreenReplay, -h}}` row, which runs
// the BUILT binary — Rule 31's check for a new surface.
func TestScreenReplayVerbWordIsNotTheAbolishedOne(t *testing.T) {
	if CliVerbScreenReplay != "screen-replay" {
		t.Fatalf("the verb word is %q — the agreed spelling is %q", CliVerbScreenReplay, "screen-replay")
	}
	if screenSubcmdReplay == CliVerbScreenReplay {
		t.Fatalf("the abolished top-level word %q must not be this verb's word", screenSubcmdReplay)
	}
	// The membership test is the comparison the predicate performs. A prefix
	// test would make `screen` match `screen-replay` and vice versa; both
	// directions are asserted.
	matches := func(word string) bool { return word == CliVerbScreenReplay }
	if !matches(CliVerbScreenReplay) {
		t.Fatalf("the whole-word comparison must accept %q", CliVerbScreenReplay)
	}
	// The neighbours are checked ONE BY ONE rather than through a slice: the
	// census generator counts a `[]string{<verb>, …}` literal as a real drive of
	// that verb, and this arm drives nothing — it compares words. Written as a
	// slice it would have inflated `screen`'s test_drive cell by one, which is a
	// corpus lie of exactly the kind that table is re-derived to prevent.
	family := CliVerbScreen
	if matches(family) {
		t.Fatalf("the whole-word comparison must NOT accept %q — that is the family verb, dispatched elsewhere", family)
	}
	abolished := screenSubcmdReplay
	if matches(abolished) {
		t.Fatalf("the whole-word comparison must NOT accept %q — the top-level replay verb is abolished", abolished)
	}
	longer := CliVerbScreenReplay + "s"
	if matches(longer) {
		t.Fatalf("the whole-word comparison must NOT accept %q — a longer word is not this verb", longer)
	}
	if matches("") {
		t.Fatalf("the whole-word comparison must NOT accept an empty word")
	}
}

// payloadBytesOf is the counted total the comparison reports, computed from the
// fixture rather than read from a trailer (a trailer is compared, not believed).
func payloadBytesOf(frames [][]byte) int {
	n := 0
	for _, f := range frames {
		n += len(f)
	}
	return n
}
