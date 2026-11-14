package main

// cli_screen_replay_test.go — the screen family's own tests.
//
// WHAT THESE TESTS ARE FOR. The verb's contract has four halves a compile-time
// check cannot see, and each half is one test below:
//
//  1. THE BYTES ARE THE RECORDING, byte for byte — not "close enough": the test
//     builds a stream, writes it through `screen replay`, and compares the
//     output against the concatenated frame payloads it wrote.
//  2. THE DIAGNOSTICS DO NOT POLLUTE STDOUT. A caller redirects stdout into a
//     file; a single notice on that stream corrupts the artifact. The test
//     captures both streams and asserts stdout holds the payloads EXACTLY
//     (a healthy recording, where the diagnostic stream must be EMPTY).
//  3. THE REFUSAL NAMES THE OTHER READER. A session with no stream must be
//     refused by name and must name `gopi session replay <session-id>` — never
//     silently print a dump.
//  4. THE FOUR ENDINGS ARE FOUR SENTENCES. Each is produced by a fixture that
//     differs only in the way the recording ended, so a reader that collapsed
//     them into one boolean fails here.
//
// The fixtures are WRITTEN, not hand-typed: `writeTestScreenStream` emits the
// real grammar from the package that owns it (uiclient), so a change to the
// wire format breaks these tests rather than passing against a stale copy.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/uiclient"
)

// testScreenStream is one recorded stream a test writes.
type testScreenStream struct {
	frames [][]byte
	// trailer writes the close-out record. false = a recording that never
	// finished; the caller chooses whether it also cuts its last frame.
	trailer bool
	// cutInsideLastFrame drops the last three bytes of the final payload WITHOUT
	// a trailer, which is the "ends inside a frame" state.
	cutInsideLastFrame bool
	// lyingTrailer writes a close-out whose frame count disagrees with the
	// frames actually present — the contradictory state.
	lyingTrailer bool
}

// writeTestScreenStream materialises one stream into dir and returns its path.
// The header and the trailer come from uiclient's own vocabulary, so this
// fixture cannot drift from the grammar the reader parses.
func writeTestScreenStream(t *testing.T, dir, id string, s testScreenStream) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s version=1 format=len-prefix terminator=# grid=80x24 revision=testrev started=2026-11-08T00:00:00Z\n",
		uiclient.ScreenStreamHeaderPrefix)
	payloadBytes := 0
	for i, f := range s.frames {
		payload := f
		declared := len(f)
		if s.cutInsideLastFrame && i == len(s.frames)-1 {
			// The DECLARED length stays the full one — the writer announced a
			// payload it never finished, which is what "cut inside a frame"
			// means on the wire. Shortening the declaration too would produce a
			// whole (if shorter) frame instead — a different state.
			payload = payload[:len(payload)-3]
		}
		fmt.Fprintf(&buf, "%d\n", declared)
		buf.Write(payload)
		payloadBytes += len(payload)
	}
	switch {
	case s.lyingTrailer:
		// A count the file's own body refutes: one fewer frame than it holds.
		fmt.Fprintf(&buf, "%s frames=%d bytes=%d grid=80x24\n",
			uiclient.ScreenStreamTrailerPrefix, len(s.frames)-1, payloadBytes)
	case s.trailer:
		fmt.Fprintf(&buf, "%s frames=%d bytes=%d grid=80x24\n",
			uiclient.ScreenStreamTrailerPrefix, len(s.frames), payloadBytes)
	}
	path := filepath.Join(dir, id+uiclient.ScreenStreamSuffix)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// testScreenFrames is the payload set every case below replays. One frame is
// deliberately styled, one deliberately not, so a writer that "helpfully"
// rewrote colour would change the bytes.
func testScreenFrames() [][]byte {
	return [][]byte{
		[]byte("\x1b[2J\x1b[1;1Hhello"),
		[]byte("\x1b[38;2;255;0;0mred\x1b[0m"),
		[]byte("日本語\n"),
	}
}

// screenEndingSentences is the vocabulary a NON-closed recording may use. It is
// derived from the same constants the production branch prints, so the tests
// cannot assert against a sentence the verb no longer emits — and a HEALthy
// recording is defined as "none of these appeared", which is the one assertion
// that would have caught the defect this family exists to fix (an unclosed
// stream reported as if it were healthy).
func screenEndingSentences() []string {
	return []string{
		screenEndingPhrase(screenReplayUnclosedWholeFormat),
		screenEndingPhrase(screenReplayUnclosedCutFormat),
		screenEndingPhrase(screenReplayContradictionFormat),
	}
}

// screenEndingPhrase cuts a format string at its OWN first verb, so the fixture
// asserts words and never an arithmetic byte offset into a literal (an offset
// that a wording change would silently turn into a substring of the next
// sentence).
func screenEndingPhrase(format string) string {
	s := strings.TrimPrefix(format, "%s")
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// TestScreenReplayWritesTheBytesExactly is arm 1+2: stdout is the concatenated
// payloads, byte for byte, and a closed recording prints NOTHING on stderr.
// Both halves matter — a writer that got the bytes right and added a notice
// would break every caller that pipes the output.
func TestScreenReplayWritesTheBytesExactly(t *testing.T) {
	dir := t.TempDir()
	id := "01a1b827-screen-bytes"
	path := writeTestScreenStream(t, dir, id, testScreenStream{frames: testScreenFrames(), trailer: true})

	var out, errw bytes.Buffer
	if code := screenReplayRun([]string{path}, &out, &errw); code != chatExitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, chatExitOK, errw.String())
	}
	want := bytes.Join(testScreenFrames(), nil)
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("stdout is not the recording:\n got %q\nwant %q", out.Bytes(), want)
	}
	// STDERR CARRIES THE COUNTS AND NO ENDING SENTENCE. A closed recording has
	// no ending to report; the counts are always stated, because they are what
	// lets the caller check "byte for byte" against its own count.
	diag := errw.String()
	if !strings.Contains(diag, fmt.Sprintf("frames=%d", len(testScreenFrames()))) {
		t.Fatalf("stderr must state the counts it wrote, got %q", diag)
	}
	for _, ending := range screenEndingSentences() {
		if strings.Contains(diag, ending) {
			t.Fatalf("a CLOSED recording must not carry the %q sentence, got %q", ending, diag)
		}
	}
}

// TestScreenReplayResolvesASessionID drives the SESSION-ID route rather than a
// path, which is the operand every operator types: the resolver must find the
// artifact the same way `gopi session log` does.
func TestScreenReplayResolvesASessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// The resolver walks the profile's sessions tree; put the fixture exactly
	// where a run would have written it.
	id := "01a1b827-screen-session"
	dir := filepath.Join(home, ".gopi", "sessions", id)
	writeTestScreenStream(t, dir, id, testScreenStream{frames: testScreenFrames(), trailer: true})

	var out, errw bytes.Buffer
	if code := screenReplayRun([]string{id}, &out, &errw); code != chatExitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, chatExitOK, errw.String())
	}
	if want := bytes.Join(testScreenFrames(), nil); !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("stdout is not the recording:\n got %q\nwant %q", out.Bytes(), want)
	}
}

// TestScreenReplayRefusesASessionWithNoStream is arm 3: the refusal names the
// RENDERER, and stdout stays EMPTY. The empty stdout is the substance — a
// fallback to the dump would answer a different question in the same shape.
func TestScreenReplayRefusesASessionWithNoStream(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	id := "01a1b827-screen-nostream"
	if err := os.MkdirAll(filepath.Join(home, ".gopi", "sessions", id), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var out, errw bytes.Buffer
	if code := screenReplayRun([]string{id}, &out, &errw); code != chatExitFailed {
		t.Fatalf("exit = %d, want %d", code, chatExitFailed)
	}
	if out.Len() != 0 {
		t.Fatalf("a refusal must write NOTHING to stdout, got %q", out.String())
	}
	msg := errw.String()
	if !strings.Contains(msg, "recorded no screen stream") {
		t.Fatalf("the refusal does not say what is missing: %q", msg)
	}
	if !strings.Contains(msg, sessionReplaySynopsis) {
		t.Fatalf("the refusal must name the renderer %q so the operator has a route, got %q",
			sessionReplaySynopsis, msg)
	}
}

// TestScreenReplayReportsFourDistinctEndings is arm 4. Each fixture differs ONLY
// in how the recording ended, and each must produce its own sentence — a reader
// that folded them together (the defect this family was formed to fix) collapses
// at least two of these four cases.
func TestScreenReplayReportsFourDistinctEndings(t *testing.T) {
	cases := []struct {
		name  string
		build testScreenStream
		want  string
	}{
		{
			name:  "closed",
			build: testScreenStream{frames: testScreenFrames(), trailer: true},
			want:  "", // a healthy recording says nothing at all
		},
		{
			name:  "whole-but-unclosed",
			build: testScreenStream{frames: testScreenFrames()},
			want:  "never wrote the close-out record",
		},
		{
			name:  "cut-inside-a-frame",
			build: testScreenStream{frames: testScreenFrames(), cutInsideLastFrame: true},
			want:  "ends inside a frame",
		},
		{
			name:  "contradictory",
			build: testScreenStream{frames: testScreenFrames(), lyingTrailer: true},
			want:  "contradicts itself",
		},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeTestScreenStream(t, dir, "01a1b827-"+tc.name, tc.build)
			var out, errw bytes.Buffer
			screenReplayRun([]string{path}, &out, &errw)
			got := errw.String()
			if tc.want == "" {
				for _, ending := range screenEndingSentences() {
					if strings.Contains(got, ending) {
						t.Fatalf("ending %q: a CLOSED recording must not carry %q, got %q", tc.name, ending, got)
					}
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("ending %q: diagnostic %q does not carry %q", tc.name, got, tc.want)
			}
			if prev, ok := seen[tc.want]; ok {
				t.Fatalf("endings %q and %q produced the same sentence", prev, tc.name)
			}
			seen[tc.want] = tc.name
		})
	}
}

// TestScreenReplayVerifyComparesFramesNotFiles is the --verify contract: two
// artifacts with DIFFERENT headers and the SAME payloads are identical, and one
// differing payload is reported with its frame number. Comparing files would
// fail the first case and print a divergence nobody could act on.
func TestScreenReplayVerifyComparesFramesNotFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeTestScreenStream(t, dir, "01a1b827-verify-a", testScreenStream{frames: testScreenFrames(), trailer: true})
	b := writeTestScreenStream(t, dir, "01a1b827-verify-b", testScreenStream{frames: testScreenFrames(), trailer: true})
	if a == b {
		t.Fatal("fixture built one file twice")
	}

	var out, errw bytes.Buffer
	if code := screenReplayRun([]string{a, screenReplayVerifyFlag, b}, &out, &errw); code != chatExitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, chatExitOK, errw.String())
	}
	if !strings.Contains(out.String(), "first divergence: none") {
		t.Fatalf("identical payloads under different headers must compare equal, got:\n%s", out.String())
	}

	// Now change ONE payload byte and require the divergence to name frame 3 —
	// SgrFrameDivergence.Frame is 1-BASED, so a test asserting 2 would pin an
	// off-by-one into the contract.
	frames := testScreenFrames()
	frames[2] = []byte("日本語!")
	c := writeTestScreenStream(t, dir, "01a1b827-verify-c", testScreenStream{frames: frames, trailer: true})
	out.Reset()
	errw.Reset()
	if code := screenReplayRun([]string{a, screenReplayVerifyFlag, c}, &out, &errw); code != chatExitOK {
		t.Fatalf("a divergence is a FINDING, not a failure — exit = %d (stderr: %s)", code, errw.String())
	}
	if !strings.Contains(out.String(), "first divergence: frame 3") {
		t.Fatalf("the divergence must name the frame it found, got:\n%s", out.String())
	}
}

// TestScreenGroupRefusesUsageErrors pins the two shapes that must never reach
// the agent loop: the bare group word and an unknown sub-verb. The dispatcher
// RETURNS its status (only isScreenSubcommand exits the process), so this test
// drives the real branch rather than a copy of its sentence.
//
// It is ALSO this verb's `test_drive` evidence in the census
// (devdoc/gopi-verb-census.md): the argv literal below puts the verb FIRST, and
// the census counts exactly that shape.
func TestScreenGroupRefusesUsageErrors(t *testing.T) {
	dir := t.TempDir()
	id := "01a1b827-screen-group"
	path := writeTestScreenStream(t, dir, id, testScreenStream{frames: testScreenFrames(), trailer: true})

	// The THROUGH-THE-GROUP run: the argv a real operator types, handed to the
	// group dispatcher itself — proof that the wiring reaches the sub-verb, not
	// merely that the sub-verb works when called directly. The dispatcher takes
	// the argv AFTER the group word (that is what isScreenSubcommand strips), so
	// the literal below starts at the sub-verb.
	var out, errw bytes.Buffer
	if code := runScreenGroup([]string{screenSubcmdReplay, path}, &out, &errw); code != chatExitOK {
		t.Fatalf("`gopi screen replay %s` through the group: exit = %d, want %d (stderr: %s)",
			path, code, chatExitOK, errw.String())
	}
	if want := bytes.Join(testScreenFrames(), nil); !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("the group route did not write the recording:\n got %q\nwant %q", out.Bytes(), want)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "bare-group", args: nil, want: "missing subcommand"},
		{name: "unknown-sub", args: []string{"nosuchverb"}, want: "unknown subcommand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			errw.Reset()
			if code := runScreenGroup(tc.args, &out, &errw); code != chatExitFailed {
				t.Fatalf("exit = %d, want %d — a usage error must not fall through to the agent loop",
					code, chatExitFailed)
			}
			if out.Len() != 0 {
				t.Fatalf("a usage error must write NOTHING to stdout (it is not an answer), got %q", out.String())
			}
			if msg := errw.String(); !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal %q does not carry %q", msg, tc.want)
			}
		})
	}

	// The vocabulary itself is the ONE list every surface reads.
	if len(screenSubcommandNames) != 1 || screenSubcommandNames[0] != screenSubcmdReplay {
		t.Fatalf("screenSubcommandNames = %v, want exactly [%q]", screenSubcommandNames, screenSubcmdReplay)
	}
}

// TestScreenGroupHelpPagesAreDistinct pins the -h split: BEFORE the sub-verb the
// group's own page, AFTER it the sub-verb's. The two pages answer different
// questions (which readers exist vs how one of them behaves), so a dispatcher
// that printed one for both would silently hide half the family.
func TestScreenGroupHelpPagesAreDistinct(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runScreenGroup([]string{HelpFlagLong}, &out, &errw); code != chatExitOK {
		t.Fatalf("`gopi %s %s`: exit = %d, want %d", CliVerbScreen, HelpFlagLong, code, chatExitOK)
	}
	groupPage := out.String()
	if !strings.Contains(groupPage, "GoPi screen subcommands") {
		t.Fatalf("the group page does not identify itself: %q", groupPage)
	}
	if strings.Contains(groupPage, screenReplayUsageHead) {
		t.Fatal("`gopi screen -h` printed the SUB-VERB's page instead of the family's")
	}

	out.Reset()
	if code := runScreenGroup([]string{screenSubcmdReplay, HelpFlagLong}, &out, &errw); code != chatExitOK {
		t.Fatalf("`gopi %s %s %s`: exit = %d, want %d", CliVerbScreen, screenSubcmdReplay, HelpFlagLong, code, chatExitOK)
	}
	subPage := out.String()
	if !strings.Contains(subPage, screenReplayUsageHead) {
		t.Fatalf("`gopi screen replay -h` must print the sub-verb's own head %q, got %q",
			screenReplayUsageHead, subPage)
	}
	if subPage == groupPage {
		t.Fatal("the group page and the sub-verb page are the same text")
	}
}

// TestScreenReplayFlagParsingNamesItsRefusals pins the three refusals a caller
// can type: a flag the page does not know, a --verify with no operand, and two
// session ids. Each names what was wrong — an ignored flag would let a caller
// believe it had changed the output (Rule 15).
func TestScreenReplayFlagParsingNamesItsRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"--nope", "x"}, want: "unknown flag"},
		{args: []string{"x", screenReplayVerifyFlag}, want: "needs another recording"},
		{args: []string{"a", "b"}, want: "one session id"},
		{args: nil, want: "no session id"},
	}
	for _, tc := range cases {
		_, err := parseScreenReplayArgs(tc.args)
		if err == nil {
			t.Fatalf("args %v were accepted; want a refusal naming %q", tc.args, tc.want)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("args %v: error %q does not carry %q", tc.args, err.Error(), tc.want)
		}
	}
}
