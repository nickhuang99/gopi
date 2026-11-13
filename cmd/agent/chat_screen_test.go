package main

// chat_screen_test.go — the `/screen` command's own gates, driven through the
// REAL surfaces: the argument parser, the capture (against an honest recording
// written by uiclient's own writer AND painted with the renderer's own turn
// header), and the message boundary's next-turn injection.
//
// WHAT IS PINNED HERE, and why each one is the failure that matters:
//
//  1. THE TURN, NOT A BUDGET. `/screen 58`'s artifact contains turn 58's screen
//     and GREP-ABLE `turn 58`, and does not contain the next turn's body. This is
//     the epic's acceptance, and it is asserted on the file's bytes, not on a
//     note's wording — a name is a claim about the bytes beside it.
//  2. THE NAME STATES WHAT THE FILE HOLDS. The default name is composed from the
//     LOCATED span, so a capture cannot be filed as turn 58 while holding turn
//     59 (the measured defect).
//  3. THE INJECTION CARRIES NO BYTES. The next turn's message carries the path,
//     the byte count and the page advice, and contains none of the SGR payload —
//     this is the epic's hard ban, asserted as an ABSENCE of the painted bytes.
//  4. EVERY REFUSAL IS BY NAME. `--full` (removed), `58x`, `-1`, `0`, two turn
//     numbers, and a turn the recording does not carry each get their own
//     sentence; and a recording with no turn header is refused, never guessed.
//  5. ONCE. The note is consumed by the first turn that reads it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// chatScreenTestFrame builds one painted frame. The payload is the shape the
// renderer actually writes — an SGR run plus text — so an absence assertion over
// its bytes is an assertion about real painted content.
func chatScreenTestFrame(tag string) []byte {
	return []byte("\x1b[1m" + tag + "\x1b[0m\r\n")
}

// chatScreenTestHeader is the renderer's OWN turn header, composed by calling it,
// so a change to the glyph, the spacing or the word reaches this fixture without
// an edit here.
func chatScreenTestHeader(turn int, prompt string) []byte {
	return []byte("\x1b[22;1H" + lineedit.TurnHeader(lineedit.TurnInfo{Index: turn, Prompt: prompt}, 120) + "\x1b[0m")
}

// itoa spells a decimal, so the fixtures need no import beyond the assertions'.
func chatScreenItoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

// chatScreenTestSession builds a chatSession whose live recording holds the given
// frames, and returns it with its session directory and the recording's payload
// total.
//
// THE FIXTURE IS PRODUCED THROUGH THE REAL WRITER AND THEN CLOSED. That is the
// honest shape: `/screen` reads `sgr.Path()` and nothing else, so a session whose
// stream was written and closed is exactly the state a real run is in at any
// instant after a repaint (a live stream is the same file, one frame longer), and
// the trailer makes the byte count a stable number the assertions can compare
// against. Building the file some other way would test a format nothing writes.
func chatScreenTestSession(t *testing.T, frames ...[]byte) (*chatSession, string, int64) {
	t.Helper()
	dir := t.TempDir()
	s, err := uiclient.OpenSgrStream(uiclient.SgrStreamConfig{
		Path:     filepath.Join(dir, "live.screen.stream"),
		Cols:     120,
		Rows:     24,
		Revision: "test",
	})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	var total int64
	for _, p := range frames {
		if werr := s.Write(p); werr != nil {
			t.Fatalf("SgrStream.Write: %v", werr)
		}
		total += int64(len(p))
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatalf("SgrStream.Close: %v", cerr)
	}
	return &chatSession{sessionDir: dir, sgr: s}, dir, total
}

// chatScreenTwoTurnFrames is the canonical fixture: two numbered turns, each with
// its header and its own greppable body, plus a prompt line before the first.
func chatScreenTwoTurnFrames() [][]byte {
	return [][]byte{
		[]byte("\x1b[23;1Hyou> what is on my screen\r\n"),
		chatScreenTestHeader(58, "what is on my screen"),
		[]byte("\x1b[22;1HBODY-OF-TURN-58\r\n"),
		[]byte("\x1b[23;1Hyou> and now\r\n"),
		chatScreenTestHeader(59, "and now"),
		[]byte("\x1b[22;1HBODY-OF-TURN-59\r\n"),
	}
}

// TestScreenSlashProducesAReadableArtifact is the epic's FIRST acceptance: a real
// capture file exists, is a valid stream, and its stated byte count matches what
// the file holds.
func TestScreenSlashProducesAReadableArtifact(t *testing.T) {
	sess, dir, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	in := &chatInput{}
	chatScreenHandleLine(in, nil, sess, "/screen")

	if sess.screenPending == "" {
		t.Fatalf("/screen parked no reference — the next turn would carry nothing")
	}
	// The derived name, read off the block itself (the block is what the model is
	// told, so the path it names is the path that must exist).
	path := chatScreenPathFromNote(t, sess.screenPending)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the captured path %q does not exist: %v", path, err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("the capture landed in %q, want the session directory %q", filepath.Dir(path), dir)
	}
	meta, frames, err := uiclient.ReadSgrFrames(mustOpen(t, path))
	if err != nil {
		t.Fatalf("the capture is not a readable SGR stream: %v", err)
	}
	if !meta.Trailer {
		t.Errorf("the capture has no trailer — it is not a valid stream")
	}
	if meta.Frames != len(frames) {
		t.Errorf("trailer says %d frames, the reader recovered %d", meta.Frames, len(frames))
	}
	if got := chatScreenStatedBytes(t, sess.screenPending); got != meta.Bytes {
		t.Errorf("the injected block states %d bytes, the file's own trailer says %d", got, meta.Bytes)
	}
}

// TestScreenSlashExplicitTurnIsExact is the epic's HEADLINE acceptance: the file
// named for turn 58 contains turn 58's screen, by content.
func TestScreenSlashExplicitTurnIsExact(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	chatScreenHandleLine(&chatInput{}, nil, sess, "/screen 58")
	note := sess.screenPending
	if note == "" {
		t.Fatalf("`/screen 58` parked no reference")
	}
	path := chatScreenPathFromNote(t, note)
	body := chatScreenReadPayload(t, path)
	if !strings.Contains(body, "turn 58") {
		t.Fatalf("the artifact of `/screen 58` does not contain \"turn 58\":\n%s", chatScreenExcerpt(body))
	}
	if !strings.Contains(body, "BODY-OF-TURN-58") {
		t.Errorf("the artifact does not hold turn 58's own painting:\n%s", chatScreenExcerpt(body))
	}
	if strings.Contains(body, "BODY-OF-TURN-59") {
		t.Errorf("the artifact holds the NEXT turn's body — the segment is one turn too long:\n%s", chatScreenExcerpt(body))
	}
	if strings.Contains(body, "turn 59") {
		t.Errorf("the artifact of `/screen 58` names turn 59 as well:\n%s", chatScreenExcerpt(body))
	}
	if filepath.Base(path) != "screen-turn58"+uiclient.ScreenStreamSuffix {
		t.Errorf("the artifact is named %q, want %q — the name states the turn the file HOLDS", filepath.Base(path), "screen-turn58"+uiclient.ScreenStreamSuffix)
	}
}

// TestScreenSlashNameStatesWhatTheFileHolds pins the defect this task measured:
// the file used to be named after the session's turn BASELINE while holding
// whatever turn the run had reached.
func TestScreenSlashNameStatesWhatTheFileHolds(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	// A RESUMED session: the baseline says 57 turns already existed before this run.
	sess.turnBaseline = 57
	chatScreenHandleLine(&chatInput{}, nil, sess, "/screen")
	note := sess.screenPending
	if note == "" {
		t.Fatalf("/screen parked no reference")
	}
	path := chatScreenPathFromNote(t, note)
	want := "screen-turn59" + uiclient.ScreenStreamSuffix
	if filepath.Base(path) != want {
		t.Fatalf("the artifact is named %q, want %q — the name comes from the located span, never from the session's turn baseline", filepath.Base(path), want)
	}
	body := chatScreenReadPayload(t, path)
	if !strings.Contains(body, "turn 59") {
		t.Errorf("the file is named for turn 59 but does not hold it:\n%s", chatScreenExcerpt(body))
	}
	if strings.Contains(body, "turn 58") {
		t.Errorf("the file is named for turn 59 but holds turn 58 as well:\n%s", chatScreenExcerpt(body))
	}
}

// TestScreenSlashBareFormIsTheCurrentTurn pins the no-argument semantics: the
// newest located turn, which is the screen as it stands.
func TestScreenSlashBareFormIsTheCurrentTurn(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	chatScreenHandleLine(&chatInput{}, nil, sess, "/screen")
	note := sess.screenPending
	if note == "" {
		t.Fatalf("/screen parked no reference")
	}
	path := chatScreenPathFromNote(t, note)
	if filepath.Base(path) != "screen-turn59"+uiclient.ScreenStreamSuffix {
		t.Errorf("the bare form captured %q, want the newest turn (59)", filepath.Base(path))
	}
	body := chatScreenReadPayload(t, path)
	if !strings.Contains(body, "turn 59") || !strings.Contains(body, "BODY-OF-TURN-59") {
		t.Errorf("the bare form did not capture the newest turn's painting:\n%s", chatScreenExcerpt(body))
	}
	if strings.Contains(body, "BODY-OF-TURN-58") {
		t.Errorf("the bare form captured an earlier turn's body — it must be ONE turn:\n%s", chatScreenExcerpt(body))
	}
}

// TestScreenSlashInjectionCarriesNoSgrBytes is the epic's hard ban: the next
// turn's message carries the path, the byte count and the page advice, and none
// of the SGR payload.
func TestScreenSlashInjectionCarriesNoSgrBytes(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	chatScreenHandleLine(&chatInput{}, nil, sess, "/screen 58")
	note := sess.screenPending
	if note == "" {
		t.Fatalf("/screen parked no reference")
	}
	path := chatScreenPathFromNote(t, note)
	if !filepath.IsAbs(path) {
		t.Errorf("the injected reference names %q, which is not an absolute path", path)
	}
	if !strings.Contains(note, "screen capture") {
		t.Errorf("the block does not say it is a screen capture:\n%s", note)
	}
	if strings.Contains(note, "\x1b") {
		t.Errorf("the block carries an ESC byte — SGR content leaked into the request:\n%q", note)
	}
	for _, mark := range []string{"BODY-OF-TURN-58", "BODY-OF-TURN-59"} {
		if strings.Contains(note, mark) {
			t.Errorf("the block carries recorded frame content %q:\n%s", mark, note)
		}
	}
	if !strings.Contains(note, "read") {
		t.Errorf("the block does not tell the model how to read the file:\n%s", note)
	}
	if !strings.Contains(note, "turn 58") {
		t.Errorf("the block does not state WHICH turn the file holds:\n%s", note)
	}
	// The composed message is bounded: framing, not payload.
	if len(note) > chatScreenInjectionMaxBytes {
		t.Errorf("the injected block is %d bytes, past the %d-byte framing bound", len(note), chatScreenInjectionMaxBytes)
	}
}

// TestScreenSlashOutOverridesAndOnceConsumes pins the two argument-shaped
// promises: `--out PATH` writes where the operator said, and the parked reference
// describes EXACTLY ONE turn (the second take is empty).
func TestScreenSlashOutOverridesAndOnceConsumes(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	dest := filepath.Join(t.TempDir(), "mine.screen.stream")
	chatScreenHandleLine(&chatInput{}, nil, sess, "/screen --out "+dest)
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("--out %q produced no file: %v", dest, err)
	}
	if note := sess.screenPending; !strings.Contains(note, dest) {
		t.Errorf("the injected block names %q, not the override %q", note, dest)
	}
	// ONCE: the boundary's take clears the slot.
	if got := sess.chatScreenTakePending(); got == "" {
		t.Fatalf("the parked reference was not delivered")
	}
	if got := sess.chatScreenTakePending(); got != "" {
		t.Errorf("the reference was delivered TWICE (second take = %q) — it must be consumed once", got)
	}
}

// TestScreenSlashRefusesUnknownArgumentsAndHelp pins the vocabulary: every word
// this command does not accept is refused BY NAME, `--full` is refused with the
// reason it is gone, and `/screen help` answers from the ONE usage constant
// `/help screen` returns.
func TestScreenSlashRefusesUnknownArgumentsAndHelp(t *testing.T) {
	cases := []struct {
		label string
		args  []string
		want  string
	}{
		{"the removed bound flag", []string{chatScreenRemovedFullFlag}, chatScreenRemovedFullFlag},
		{"a non-numeric turn", []string{"58x"}, "58x"},
		{"a negative turn", []string{"-1"}, "-1"},
		{"turn zero", []string{"0"}, "0"},
		{"an unknown flag", []string{"--outt=x"}, "--outt=x"},
		{"--out with no value", []string{"--out"}, chatScreenOutFlag},
	}
	for _, c := range cases {
		_, err := parseChatScreenArgs(c.args)
		if err == nil {
			t.Errorf("%s (%v) was ACCEPTED — a caller who typed it believes it was understood", c.label, c.args)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the refusal %q does not name %q — the caller cannot see what to fix", c.label, err, c.want)
		}
	}
	// TWO turn numbers is a contradiction, not a "take the first".
	if _, err := parseChatScreenArgs([]string{"58", "59"}); err == nil {
		t.Errorf("two turn numbers were accepted — /screen takes ONE turn")
	} else if !strings.Contains(err.Error(), "58") || !strings.Contains(err.Error(), "59") {
		t.Errorf("the two-turn refusal %q does not name both numbers", err)
	}
	// The accepted shapes.
	req, err := parseChatScreenArgs([]string{"58", "--out=" + filepath.Join(chatScreenFixtureOutDir, "x")})
	if err != nil {
		t.Fatalf("parseChatScreenArgs: %v", err)
	}
	if req.Turn != 58 || req.Path == "" {
		t.Errorf("the joined --out form with a turn did not parse: %+v", req)
	}
	if bare, berr := parseChatScreenArgs(nil); berr != nil || bare.Turn != 0 {
		t.Errorf("the bare form did not parse to the current turn: %+v (%v)", bare, berr)
	}
	// ONE usage definition, reached from three directions.
	if usage, ok := chatNativeUsage(chatScreenCommandName); !ok || usage != chatScreenUsage {
		t.Errorf("/help screen does not answer from chatScreenUsage (ok=%v)", ok)
	}
	if usage, ok := chatReplSlashVocabulary(chatScreenCommandName); !ok || usage != chatScreenUsage {
		t.Errorf("the refusal vocabulary does not answer /screen from chatScreenUsage (ok=%v)", ok)
	}
	if !chatSessionWordKnown(chatScreenCommandName) {
		t.Errorf("chatSessionWords does not carry %q — the menu and the refusal would omit a command the switch answers", chatScreenCommandName)
	}
	// `/screen help` and `/help screen` are the same text, by construction.
	if helt, herr := parseChatScreenArgs([]string{chatScreenHelpWord}); herr != nil || !helt.Help {
		t.Errorf("`/screen %s` did not ask for the usage block: %+v (%v)", chatScreenHelpWord, helt, herr)
	}
}

// TestScreenSlashRefusesATurnTheRecordingLacks pins the "取错段比取不到更糟" rule on
// the selectable path: a turn the recording does not carry is refused BY NAME
// with the range that would work, not satisfied with a neighbour.
func TestScreenSlashRefusesATurnTheRecordingLacks(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t, chatScreenTwoTurnFrames()...)
	note, console, err := chatScreenCapture(nil, sess, chatScreenRequest{Turn: 58})
	if err == nil && note == "" && console != "" {
		// The captured path is fine; this branch is the "absent turn" case below.
		_ = console
	}
	if err != nil {
		t.Fatalf("capturing turn 58 is not an error: %v", err)
	}
	// Now a turn that is not there.
	note, console, err = chatScreenCapture(nil, sess, chatScreenRequest{Turn: 57})
	if err != nil {
		t.Fatalf("an absent turn is a refusal, not an error: %v", err)
	}
	if note != "" {
		t.Errorf("an absent turn parked a reference: %q — a neighbour's screen would be the wrong segment", note)
	}
	for _, want := range []string{"57", "58..59"} {
		if !strings.Contains(console, want) {
			t.Errorf("the absent-turn refusal %q does not state %q — a refusal without the alternatives leaves the caller guessing", console, want)
		}
	}
}

// TestScreenSlashRefusesARecordingWithNoTurnHeader is Rule 15 on the degraded
// path: a recording that paints frames but never a turn header cannot be
// selected from, and the command says so instead of guessing at a segment.
func TestScreenSlashRefusesARecordingWithNoTurnHeader(t *testing.T) {
	sess, _, _ := chatScreenTestSession(t,
		[]byte("\x1b[1;1Hyou> hello\r\n"),
		chatScreenTestFrame("AN-UNNUMBERED-ANSWER"),
	)
	note, console, err := chatScreenCapture(nil, sess, chatScreenRequest{})
	if err != nil {
		t.Fatalf("a recording with no turn header is a refusal, not an error: %v", err)
	}
	if note != "" {
		t.Fatalf("a recording with no turn header parked a reference: %q — a guessed segment is the defect this command exists to end", note)
	}
	if !strings.Contains(console, "no rendered turn header") {
		t.Errorf("the refusal does not state WHY: %q", console)
	}
}

// TestScreenSlashNoStreamIsStatedNotSilent is Rule 15 on the other degraded path:
// a run with no recording says so and parks nothing.
func TestScreenSlashNoStreamIsStatedNotSilent(t *testing.T) {
	note, console, err := chatScreenCapture(nil, &chatSession{sessionDir: t.TempDir()}, chatScreenRequest{})
	if err != nil {
		t.Fatalf("a run with no recording is not an error: %v", err)
	}
	if note != "" {
		t.Errorf("a run with no recording parked a reference: %q", note)
	}
	if !strings.Contains(console, "not recording") {
		t.Errorf("the no-recording case was not stated on the console: %q", console)
	}
}

// TestScreenSlashTruncatesNothing is the epic's "no truncation" acceptance, at the
// size where a byte budget would have cut: a turn far larger than the old 64 KiB
// budget must come back WHOLE.
//
// The frame here is intentionally bigger than the old bound, so a re-introduced
// budget would show up as a smaller file rather than as a slow test.
func TestScreenSlashTruncatesNothing(t *testing.T) {
	big := "\x1b[22;1H" + strings.Repeat("x", chatScreenOldBudgetBytes+4096)
	frames := [][]byte{
		chatScreenTestHeader(70, "a very wide answer"),
		[]byte(big),
		chatScreenTestHeader(71, "second"),
		[]byte("\x1b[22;1HBODY-OF-TURN-71\r\n"),
	}
	sess, _, _ := chatScreenTestSession(t, frames...)
	chatScreenHandleLine(&chatInput{}, nil, sess, "/screen 70")
	note := sess.screenPending
	if note == "" {
		t.Fatalf("`/screen 70` parked no reference")
	}
	path := chatScreenPathFromNote(t, note)
	meta, _, err := uiclient.ReadSgrFrames(mustOpen(t, path))
	if err != nil {
		t.Fatalf("the capture is unreadable: %v", err)
	}
	if meta.Bytes < int64(len(big)) {
		t.Errorf("the capture holds %d bytes, turn 70's frame alone is %d — a bound is back", meta.Bytes, len(big))
	}
	body := chatScreenReadPayload(t, path)
	if !strings.Contains(body, "turn 70") {
		t.Errorf("the big capture does not hold turn 70")
	}
	if strings.Contains(body, "TRUNCATED") || strings.Contains(body, "NOT in it") {
		t.Errorf("the capture's block still speaks of truncation")
	}
}

// chatScreenFixtureOutDir is the fixture's OUT-OF-ROOT directory: the accepted
// `--out=` shape below needs an ABSOLUTE path that is NOT the session directory,
// and the test's point is only that the joined form parses — nothing here
// asserts a refusal of the host temp root. NAMED rather than inlined because a
// bare host root written into a fixture is Rule 13's defect (the gate's
// named-fixture rule: the spelling gets ONE declared home and a name).
const chatScreenFixtureOutDir = "/tmp"

// chatScreenOldBudgetBytes is the byte budget this command used to apply. It is
// named here as a NUMBER THE TEST COMPARES AGAINST (the capture must exceed it),
// not as a policy: nothing in the command reads it any more.
const chatScreenOldBudgetBytes = 64 << 10

// chatScreenInjectionMaxBytes bounds the injected FRAMING. It is derived from the
// shape of the block, not from a wish: one paragraph around four numbers is a few
// hundred bytes, so this is already an order of magnitude of slack, and a payload
// that rode along could not hide under it.
const chatScreenInjectionMaxBytes = 2048

// chatScreenReadPayload reads a capture back and returns its payload with the
// record framing removed — the bytes the model would see, which is what the
// content assertions are about.
func chatScreenReadPayload(t *testing.T, path string) string {
	t.Helper()
	_, frames, err := uiclient.ReadSgrFrames(mustOpen(t, path))
	if err != nil {
		t.Fatalf("read the capture %q: %v", path, err)
	}
	var b strings.Builder
	for _, f := range frames {
		b.Write(f)
	}
	return b.String()
}

// chatScreenExcerpt renders a payload for a failure message: the SGR bytes are
// stripped so the assertion's own words are legible, and the length is stated.
func chatScreenExcerpt(body string) string {
	clean := body
	for _, seq := range []string{"\x1b", "\r"} {
		clean = strings.ReplaceAll(clean, seq, "")
	}
	if len(clean) > 600 {
		return chatScreenItoa(len(clean)) + " bytes: " + clean[:600]
	}
	return clean
}

// chatScreenPathFromNote recovers the path the injected block names. It asserts
// the shape rather than guessing: the block opens with the marker and the path
// follows it, so a note whose first field is not a path fails HERE rather than
// silently making the other assertions vacuous.
func chatScreenPathFromNote(t *testing.T, note string) string {
	t.Helper()
	prefix := chatScreenInjectionHeader + " "
	if !strings.HasPrefix(note, prefix) {
		t.Fatalf("the injected block does not open with %q:\n%s", prefix, note)
	}
	rest := strings.TrimPrefix(note, prefix)
	path := rest
	if i := strings.IndexAny(rest, " \n"); i > 0 {
		path = rest[:i]
	}
	if path == "" {
		t.Fatalf("the injected block names no path:\n%s", note)
	}
	return path
}

// chatScreenStatedBytes recovers the byte count the block states, so the console
// number and the file's own trailer can be compared instead of assumed equal.
func chatScreenStatedBytes(t *testing.T, note string) int64 {
	t.Helper()
	const marker = "holding "
	i := strings.Index(note, marker)
	if i < 0 {
		t.Fatalf("the injected block states no byte count:\n%s", note)
	}
	rest := note[i+len(marker):]
	j := strings.Index(rest, " ")
	if j < 0 {
		t.Fatalf("the byte count is not followed by a unit word:\n%s", note)
	}
	digits := rest[:j]
	var n int64
	for _, c := range digits {
		if c < '0' || c > '9' {
			t.Fatalf("the stated byte count %q is not a number", digits)
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// mustOpen opens a path for the stream reader, failing the test loudly.
func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() }) //stylegate:ignore test teardown of a file this helper opened
	return f
}
