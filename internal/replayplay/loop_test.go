package replayplay

// loop_test.go — THE TRANSPORT, decided without a terminal.
//
// The key map and the playback state machine are PURE: ParseKey is a function
// over bytes and ApplyKey is a function over state. That split is deliberate —
// it is what lets the whole binding set be checked here, including the edges
// (the first round, the last round, the speed clamps, the in-round scroll)
// while the pty driver in cmd/agent proves the same keys reach this code from a
// real keyboard.

import (
	"strings"
	"testing"
)

// TestParseKey_EveryBindingIsTheByteTheHelpPageNames pins the key map against
// the SAME list the usage page and the `?` overlay print: a binding the help
// advertises but the decoder does not know is the defect this test exists to
// catch, and it is checked by driving the sequences the help's own names imply.
func TestParseKey_EveryBindingIsTheByteTheHelpPageNames(t *testing.T) {
	cases := []struct {
		name string
		seq  []byte
		want Key
	}{
		{"space", []byte{' '}, KeyPause},
		{"n", []byte{'n'}, KeyStepForward},
		{"right arrow (CSI)", []byte("\x1b[C"), KeyStepForward},
		{"right arrow (SS3, application cursor mode)", []byte("\x1bOC"), KeyStepForward},
		{"up arrow (CSI)", []byte("\x1b[A"), KeyScrollUp},
		{"up arrow (SS3, application cursor mode)", []byte("\x1bOA"), KeyScrollUp},
		{"p", []byte{'p'}, KeyStepBack},
		{"left arrow (CSI)", []byte("\x1b[D"), KeyStepBack},
		{"left arrow (SS3, application cursor mode)", []byte("\x1bOD"), KeyStepBack},
		{"down arrow (CSI)", []byte("\x1b[B"), KeyScrollDown},
		{"down arrow (SS3, application cursor mode)", []byte("\x1bOB"), KeyScrollDown},
		{"Home (CSI H)", []byte("\x1b[H"), KeyHome},
		{"Home (tilde)", []byte("\x1b[1~"), KeyHome},
		{"Home (tilde alt)", []byte("\x1b[7~"), KeyHome},
		{"End (CSI F)", []byte("\x1b[F"), KeyEnd},
		{"End (tilde)", []byte("\x1b[4~"), KeyEnd},
		{"End (tilde alt)", []byte("\x1b[8~"), KeyEnd},
		{"PgUp (tilde)", []byte("\x1b[5~"), KeyPageUp},
		{"PgDn (tilde)", []byte("\x1b[6~"), KeyPageDown},
		{"plus", []byte{'+'}, KeyFaster},
		{"minus", []byte{'-'}, KeySlower},
		{"question", []byte{'?'}, KeyHelp},
		{"q", []byte{'q'}, KeyQuit},
		{"bare ESC", []byte{0x1b}, KeyQuit},
		{"C-c", []byte{0x03}, KeyQuit},
		{"C-d", []byte{0x04}, KeyQuit},
	}
	for _, c := range cases {
		if got := ParseKey(c.seq); got != c.want {
			t.Errorf("%s (%q) decoded as %v, want %v", c.name, c.seq, got, c.want)
		}
	}
}

// TestParseKey_UnboundKeysAreKeyNone keeps an unbound key from being an action:
// "nothing happened" and "the wrong thing happened" are different failures, and
// only the first is acceptable for a key nobody bound.
func TestParseKey_UnboundKeysAreKeyNone(t *testing.T) {
	for _, seq := range [][]byte{{'z'}, {'1'}, {'\t'}, []byte("\x1b[Z"), nil, {}} {
		if got := ParseKey(seq); got != KeyNone {
			t.Fatalf("%q decoded as %v, want KeyNone", seq, got)
		}
	}
}

// TestParseKey_ABurstActsOnItsFirstByte pins the burst rule: a paste or a fast
// typist must not be read as one unrecognised key.
func TestParseKey_ABurstActsOnItsFirstByte(t *testing.T) {
	if got := ParseKey([]byte("nq")); got != KeyStepForward {
		t.Fatalf("a burst beginning with n decoded as %v, want %v", got, KeyStepForward)
	}
	if got := ParseKey([]byte("qextra")); got != KeyQuit {
		t.Fatalf("a burst beginning with q decoded as %v, want %v", got, KeyQuit)
	}
}

// playerFor builds a player over n TURNS without a terminal (the state machine
// is what these cases drive; Paint is never called). The TURN is the unit the
// player walks (Material.PlayableUnits reads Material.Turns), so the fixture
// populates THAT sequence — a fixture that only filled Rounds would be a
// material the player has nothing to step through. Each turn is taller than the
// canvas's body, so the scroll keys have something to move.
func playerFor(n int) *Player {
	rounds := make([]Round, 0, n)
	for i := 0; i < n; i++ {
		rounds = append(rounds, Round{
			Index: i + 1, Kind: RoundModel, Label: "r",
			Rows: []FrameRow{{Cells: "one"}, {Cells: "two"}, {Cells: "three"}, {Cells: "four"}},
		})
	}
	return NewPlayer(Material{SessionID: "sess", RoundCount: n, TurnCount: n, Rounds: rounds, Turns: rounds}, nil, 20, 5, true)
}

// TestPlayer_SteppingSaturatesAndSaysSo pins the two ends: the round number
// never leaves [1, n], and a step that could not move SAYS so (a key that
// appears to do nothing is indistinguishable from a broken key).
func TestPlayer_SteppingSaturatesAndSaysSo(t *testing.T) {
	p := playerFor(3)
	p.RoundIndex = 1
	p.ApplyKey(KeyStepBack)
	if p.RoundIndex != 1 {
		t.Fatalf("step back from the first round moved to %d", p.RoundIndex)
	}
	if p.Notice != atStartNotice {
		t.Fatalf("stepping back at the start said %q, want %q", p.Notice, atStartNotice)
	}
	p.ApplyKey(KeyStepForward)
	if p.RoundIndex != 2 || p.Notice != "" {
		t.Fatalf("after a real step: index=%d notice=%q, want 2 and no notice", p.RoundIndex, p.Notice)
	}
	p.ApplyKey(KeyEnd)
	if p.RoundIndex != 3 {
		t.Fatalf("end went to %d, want 3", p.RoundIndex)
	}
	p.ApplyKey(KeyStepForward)
	if p.RoundIndex != 3 {
		t.Fatalf("step forward past the last round moved to %d", p.RoundIndex)
	}
	if p.Notice != atEndNotice {
		t.Fatalf("stepping forward at the end said %q, want %q", p.Notice, atEndNotice)
	}
	p.ApplyKey(KeyHome)
	if p.RoundIndex != 1 || p.Notice != "" {
		t.Fatalf("home left index=%d notice=%q", p.RoundIndex, p.Notice)
	}
}

// TestPlayer_AStepStartsAtTheTopOfTheNewRound: a step resets the in-round
// scroll, so the round a viewer stepped TO is shown from its beginning (a scroll
// offset carried across a step would open the new round halfway down).
func TestPlayer_AStepStartsAtTheTopOfTheNewRound(t *testing.T) {
	p := playerFor(3)
	p.RoundIndex = 2
	p.Scroll = 2
	p.ApplyKey(KeyStepForward)
	if p.RoundIndex != 3 || p.Scroll != 0 {
		t.Fatalf("after a step: round=%d scroll=%d, want 3 and 0", p.RoundIndex, p.Scroll)
	}
	p.Scroll = 1
	p.ApplyKey(KeyHome)
	if p.Scroll != 0 {
		t.Fatalf("home left the view scrolled by %d rows", p.Scroll)
	}
}

// TestPlayer_ScrollingMovesInsideTheRoundAndSaysWhenItCannot is the reason the
// scroll keys exist: a round is ONE step and can be taller than the screen, so
// the content past the last visible row is reachable only by scrolling. Both
// ends report themselves rather than looking like a dead key.
func TestPlayer_ScrollingMovesInsideTheRoundAndSaysWhenItCannot(t *testing.T) {
	p := playerFor(2)
	// col 20 x row 5 → 4 body rows; playerFor's rounds carry 4 rows, so it fits.
	if got := p.renderOptions().MaxScroll(); got != 0 {
		t.Fatalf("a round that fits reports MaxScroll %d, want 0", got)
	}
	p.ApplyKey(KeyPageDown)
	if p.Scroll != 0 || p.Notice != atRoundEndNotice {
		t.Fatalf("scrolling down in a round that fits left scroll=%d notice=%q, want 0 and %q", p.Scroll, p.Notice, atRoundEndNotice)
	}
	// Make the round much taller than the body and watch both ends. It is
	// tallRoundRows rows against a 4-row body, so a page move lands INSIDE the
	// round rather than on its end — the two ends are then tested separately.
	tall := repeatRows(tallRoundRows)
	p.Mat.Rounds[0].Rows = tall
	max := p.renderOptions().MaxScroll()
	if want := len(tall) - bodyRows(p.Rows); max != want {
		t.Fatalf("MaxScroll = %d, want the row overflow %d", max, want)
	}
	p.ApplyKey(KeyPageUp)
	if p.Scroll != 0 || p.Notice != atRoundStartNotice {
		t.Fatalf("scrolling up at the top left scroll=%d notice=%q, want 0 and %q", p.Scroll, p.Notice, atRoundStartNotice)
	}
	p.ApplyKey(KeyPageDown)
	if p.Scroll != p.scrollPage() || p.Notice != "" {
		t.Fatalf("one page down left scroll=%d notice=%q, want %d and no notice", p.Scroll, p.Notice, p.scrollPage())
	}
	for i := 0; i < 20; i++ {
		p.ApplyKey(KeyPageDown)
	}
	if p.Scroll != max {
		t.Fatalf("scrolling past the end stopped at %d, want the maximum %d", p.Scroll, max)
	}
	if p.Notice != atRoundEndNotice {
		t.Fatalf("the end of the round said %q, want %q", p.Notice, atRoundEndNotice)
	}
	if s := p.renderOptions(); s.Scroll != max {
		t.Fatalf("the render options carry scroll %d, want the clamped %d", s.Scroll, max)
	}
}

// TestPlayer_MaxScrollIsZeroForADumpFrame: a dump frame is exactly one screen
// (the dump's own grid), so a material whose steps are dump frames cannot be
// scrolled past its own content — and the answer comes from the same place a
// round's does (the row overflow, which for a one-row frame on a 4-row body is 0).
func TestPlayer_MaxScrollIsZeroForADumpFrame(t *testing.T) {
	p := NewPlayer(Material{
		SessionID: "s",
		DumpFrames: []Frame{{Kind: FrameScreen, Rows: []FrameRow{
			{Cells: "x"}, {Cells: "y"}, {Cells: "z"}, {Cells: "w"},
		}}},
	}, nil, 20, 5, true)
	if got := p.renderOptions().MaxScroll(); got != 0 {
		t.Fatalf("MaxScroll = %d for a dump frame exactly as tall as the body, want 0", got)
	}
	if p.RoundCount() != 1 {
		t.Fatalf("a dump-only material has %d steps, want its 1 frame", p.RoundCount())
	}
}

// TestPlayer_AnyStepPauses pins the transport rule: stepping is a PAUSED
// activity (a step while running would be swallowed by the speed clock).
func TestPlayer_AnyStepPauses(t *testing.T) {
	for _, k := range []Key{KeyStepForward, KeyStepBack, KeyHome, KeyEnd} {
		p := playerFor(3)
		p.ApplyKey(k)
		if !p.Paused {
			t.Fatalf("%v did not pause the playback", k)
		}
	}
}

// TestPlayer_SpeedIsClampedAndReversible pins the rate bounds and the `+`/`-`
// pair: the operator can always get back to where they were.
func TestPlayer_SpeedIsClampedAndReversible(t *testing.T) {
	p := playerFor(2)
	for i := 0; i < 20; i++ {
		p.ApplyKey(KeyFaster)
	}
	if p.Speed != SpeedMax {
		t.Fatalf("faster 20 times reached %v, want the bound %v", p.Speed, SpeedMax)
	}
	// THE REVERSIBILITY THAT MATTERS: from the top of the ladder, repeated
	// `-` lands EXACTLY on the default the playback started at. (The
	// multiplicative step this replaced measured 0.7023319615912208 here.)
	for i := 0; i < len(speedLadder); i++ {
		p.ApplyKey(KeySlower)
		if p.Speed == DefaultSpeed {
			break
		}
		if i == len(speedLadder)-1 {
			t.Fatalf("slower never returned to the default rate; it stopped at %v", p.Speed)
		}
	}
	if p.Speed != DefaultSpeed {
		t.Fatalf("slower returned to %v, want exactly %v", p.Speed, DefaultSpeed)
	}
	for i := 0; i < 20; i++ {
		p.ApplyKey(KeySlower)
	}
	if p.Speed != SpeedMin {
		t.Fatalf("slower 20 times reached %v, want the bound %v", p.Speed, SpeedMin)
	}
}

// TestPlayer_TheArrowsAndThePageKeysMoveTheViewInsideTheRound is the regression
// guard the key map was missing: ↑/↓ used to be STEP aliases, so the second half
// of a round taller than the screen was unreachable — the arrows skipped the
// round instead of reading it. The two vertical arrows must now move the READER,
// never the round, while a step really does change which round is on screen.
func TestPlayer_TheArrowsAndThePageKeysMoveTheViewInsideTheRound(t *testing.T) {
	p := playerFor(3)
	p.ApplyKey(KeyPause) // a viewer reading a long round is paused
	tall := repeatRows(tallRoundRows)
	for i := range p.Mat.Rounds {
		p.Mat.Rounds[i].Rows = tall
	}
	max := p.renderOptions().MaxScroll()
	if max < 2 {
		t.Fatalf("the fixture round admits MaxScroll %d: a one-row move would prove nothing", max)
	}
	round, paused := p.RoundIndex, p.Paused
	p.ApplyKey(KeyScrollDown)
	if p.RoundIndex != round {
		t.Fatalf("↓ changed the round (%d → %d): the vertical arrows scroll, they do not step", round, p.RoundIndex)
	}
	if p.Scroll != 1 {
		t.Fatalf("↓ moved the view to row %d, want ONE row (PgDn is the page key)", p.Scroll)
	}
	if p.Paused != paused {
		t.Fatalf("↓ changed the transport state (paused=%v): reading a round is not playing it", p.Paused)
	}
	p.ApplyKey(KeyScrollUp)
	if p.RoundIndex != round || p.Scroll != 0 {
		t.Fatalf("↑ left round=%d scroll=%d, want %d and 0", p.RoundIndex, p.Scroll, round)
	}
	p.ApplyKey(KeyScrollDown)
	p.ApplyKey(KeyScrollDown)
	if p.Scroll != 2 || p.RoundIndex != round {
		t.Fatalf("two ↓ left round=%d scroll=%d, want %d and 2", p.RoundIndex, p.Scroll, round)
	}
	// A PAGE key is still a page: the two movements have different sizes, which
	// is what makes them two keys rather than two spellings of one.
	if page := p.scrollPage(); page <= 1 {
		t.Fatalf("the page size is %d on this canvas: the page keys and the arrows cannot be told apart", page)
	}
	p.ApplyKey(KeyPageDown)
	if want := 2 + p.scrollPage(); p.Scroll != want {
		t.Fatalf("PgDn left scroll=%d, want the page move %d", p.Scroll, want)
	}
	// A STEP starts the round it lands on at the TOP, so the arrows' job is
	// undone by the round change rather than being read as the same movement.
	p.ApplyKey(KeyStepForward)
	if p.RoundIndex != round+1 || p.Scroll != 0 {
		t.Fatalf("→ left round=%d scroll=%d, want %d and 0", p.RoundIndex, p.Scroll, round+1)
	}
}

// TestPlayer_TheScrollKeysDoNotTouchTheTransportOrTheNotice is the rule that makes
// ↑/↓ safe to hold: reading a round says nothing (no notice) and changes neither
// the transport state nor the rate, because a scroll is not a transport decision.
// (The two ENDS of a round are the exception the other test pins: hitting an edge
// SAYS so — a key that appears to do nothing is indistinguishable from a broken
// one.)
func TestPlayer_TheScrollKeysDoNotTouchTheTransportOrTheNotice(t *testing.T) {
	p := playerFor(2)
	p.Mat.Rounds[0].Rows = repeatRows(tallRoundRows)
	p.Paused = false
	p.Speed = DefaultSpeed
	p.ApplyKey(KeyScrollDown)
	if p.Scroll != 1 {
		t.Fatalf("↓ inside the round moved the view to %d, want 1", p.Scroll)
	}
	if p.Notice != "" {
		t.Fatalf("a scroll inside the round said %q: there is nothing to report", p.Notice)
	}
	if p.Paused || p.Speed != DefaultSpeed {
		t.Fatalf("a scroll changed the transport (paused=%v speed=%v)", p.Paused, p.Speed)
	}
	if p.RoundIndex != 1 {
		t.Fatalf("a scroll moved to round %d", p.RoundIndex)
	}
}

// repeatRows builds n identical body rows.
func repeatRows(n int) []FrameRow {
	rows := make([]FrameRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, FrameRow{Cells: "row"})
	}
	return rows
}

// tallRoundRows is how many rows the long-round fixtures carry: several times the
// 4-row body playerFor builds, so a page move lands INSIDE the round and the two
// ends are reached separately.
const tallRoundRows = 20

// TestPlayer_HelpIsAToggleAndAKeyReturnsFromIt pins the overlay's two-way
// contract: `?` opens it, and ANY other key closes it (the help says so, so it
// has to be true).
func TestPlayer_HelpIsAToggleAndAKeyReturnsFromIt(t *testing.T) {
	p := playerFor(2)
	p.ApplyKey(KeyHelp)
	if !p.Help {
		t.Fatal("? did not open the overlay")
	}
	p.ApplyKey(KeyStepForward)
	if p.Help {
		t.Fatal("stepping did not close the overlay")
	}
	// ANY key returns, bound or not — the overlay's own text claims it, and an
	// unbound key is the one an operator presses when they opened it by mistake.
	p.ApplyKey(KeyHelp)
	p.ApplyKey(KeyNone)
	if p.Help {
		t.Fatal("an unbound key did not close the overlay, which the help text promises it will")
	}
	// A scroll key closes it too (it is bound, and the overlay says any key).
	p.ApplyKey(KeyHelp)
	p.ApplyKey(KeyScrollDown)
	if p.Help {
		t.Fatal("a scroll key left the overlay up")
	}
	p.ApplyKey(KeyHelp)
	p.ApplyKey(KeyPageDown)
	if p.Help {
		t.Fatal("a page key left the overlay up")
	}
}

// TestPlayer_QuitOnlyOnTheQuitKeys pins the exit: nothing else ends a playback.
func TestPlayer_QuitOnlyOnTheQuitKeys(t *testing.T) {
	p := playerFor(2)
	for _, k := range []Key{KeyPause, KeyStepForward, KeyStepBack, KeyHome, KeyEnd, KeyFaster, KeySlower, KeyHelp, KeyNone, KeyScrollUp, KeyScrollDown, KeyPageUp, KeyPageDown} {
		if p.ApplyKey(k) {
			t.Fatalf("%v quit the playback", k)
		}
	}
	if !p.ApplyKey(KeyQuit) {
		t.Fatal("the quit key did not quit")
	}
}

// TestPlayer_AdvanceStopsAtTheEndAndNamesIt pins the end of the material: the
// transport pauses ITSELF and says the recording ended, rather than looping
// silently or stopping with no explanation.
func TestPlayer_AdvanceStopsAtTheEndAndNamesIt(t *testing.T) {
	p := playerFor(2)
	p.advance()
	if p.RoundIndex != 2 || p.Paused {
		t.Fatalf("first advance: round=%d paused=%v, want 2 and running", p.RoundIndex, p.Paused)
	}
	p.advance()
	if p.RoundIndex != 2 {
		t.Fatalf("advancing past the end moved to %d", p.RoundIndex)
	}
	if !p.Paused || p.Notice != endOfMaterialNotice {
		t.Fatalf("the end left paused=%v notice=%q, want paused with %q", p.Paused, p.Notice, endOfMaterialNotice)
	}
	// Resuming from the end clears the notice (the operator asked for it).
	p.ApplyKey(KeyPause)
	if p.Paused || p.Notice != "" {
		t.Fatalf("resuming after the end left paused=%v notice=%q", p.Paused, p.Notice)
	}
}

// TestPlayer_RenderOptionsCarryThePlaybackState is the seam between the state
// machine and the renderer: the value painted and the value a test asserts on
// are the SAME value, produced in one place — including WHICH material (a round,
// or the dump's own screen frame) the step resolved to.
func TestPlayer_RenderOptionsCarryThePlaybackState(t *testing.T) {
	p := playerFor(3)
	p.ApplyKey(KeyStepForward)
	opts := p.renderOptions()
	if opts.RoundIndex != 2 || opts.RoundTotal != 3 || !opts.Paused {
		t.Fatalf("options carry round=%d total=%d paused=%v", opts.RoundIndex, opts.RoundTotal, opts.Paused)
	}
	if opts.Cols != p.Cols || opts.Rows != p.Rows {
		t.Fatalf("options carry canvas %dx%d, player has %dx%d", opts.Cols, opts.Rows, p.Cols, p.Rows)
	}
	if opts.Round.Index != 2 {
		t.Fatalf("step 2 resolved to round %d — the status line and the content would disagree", opts.Round.Index)
	}
	if opts.Help != nil {
		t.Fatal("the help overlay was painted without the help key")
	}
	p.ApplyKey(KeyHelp)
	if len(p.renderOptions().Help) == 0 {
		t.Fatal("the help key did not reach the render options — the overlay can never be drawn")
	}
	// A dump-only player resolves its step to a DUMP FRAME and says which one.
	d := &Player{
		Mat:   Material{SessionID: "s", Cols: 20, Rows: 5, DumpFrames: []Frame{{Kind: FrameScreen, Label: "screen", Rows: []FrameRow{{Cells: "s"}}}}},
		Speed: DefaultSpeed, RoundIndex: 1,
	}
	do := d.renderOptions()
	if len(do.Round.Rows) != 0 {
		t.Fatal("a dump-only material resolved its step to a round")
	}
	if do.DumpFrame.Label != "screen" || do.ScreenFrame != "screen" {
		t.Fatalf("the dump step did not name the frame it is showing: %+v", do)
	}
	if !strings.Contains(do.Notice, "screen") {
		t.Fatalf("the status line does not name the dump frame: %q", do.Notice)
	}
}
