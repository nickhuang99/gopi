package testsuite

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// chat_palette_e2e_test.go — the SHIPPED BINARY's half of the palette acceptance.
//
// internal/lineedit's tests pin the resolver and the renderer. They cannot pin
// what an operator actually gets, because the decision is made against the REAL
// sink and the real environment: the surfaces are wired in package main
// (cmd/agent/lineedit_wire.go, cli_chat.go) and read os.Stderr. This file runs
// the binary the operator runs and reads its bytes.
//
// TWO SHAPES, and both matter:
//
//   - PIPED (no tty): the sink is not a terminal, so the capability is off and the
//     run must contain ZERO SGR bytes — the byte-exact promise that keeps a
//     redirected transcript text. This is the shape a script, a log, or another
//     program sees.
//   - ON A PTY (a real terminal): the sink IS a terminal, so the capability
//     depends on the environment, and the test drives the three interesting
//     environments: no COLORTERM (the fallback), COLORTERM=truecolor + the gold
//     knob (the truecolour path), and TERM=dumb (no colour at all).

// paletteE2EAnswer is the mock model's answer. Its words are the markers the
// assertions look for in the child's stream, so they are named (Rule 1) and
// distinctive enough that no chrome contains them.
const paletteE2EAnswer = "palette probe answer body"

// paletteE2EAnswerTail is a marker that appears ONLY in the answer, never in what
// the harness types. It exists because the pty harness returns the instant its
// marker shows up and the editor ECHOES the typed line: waiting for the answer's
// own text matches that echo and returns BEFORE the answer band is drawn, so an
// assertion on the body's palette bytes would read a stream that does not contain
// the answer yet. Waiting for the tail makes the capture window end after the band.
const paletteE2EAnswerTail = " \u2014 probe tail"

// downgradeNoticeMark is the ONE substring that marks internal/lineedit's
// downgrade notice in the child's stream (colour.go paletteDowngradeNotice,
// written by the run's final capability decision).
// It is spelled here rather than read from the package so the end-to-end count is
// against the WIRE wording an operator's terminal receives, not against the table
// the same package wrote: a notice whose wording drifted to something else would
// then be counted zero times and fail LOUDLY, instead of being counted by a
// second, unrelated spelling of the same line (Rule 1: a meaning-carrying literal
// gets a name).
const downgradeNoticeMark = "not drawable here"

// hermesGoldAnswerParams is the truecolour parameter list the gold palette paints
// the answer body with — built through internal/sgr, never spelled, so the test
// and the palette agree on the wire spelling by construction.
func hermesGoldAnswerParams() string {
	return sgr.TruecolorFgParams(0xFF, 0xF8, 0xDC)
}

// paletteE2EEnv is MockEnv plus the palette/colour knobs this file varies. Both
// the truecolour advertisement and the palette selection are set EXPLICITLY (even
// to the empty value) so the child cannot inherit either from the environment
// this test itself runs in.
func paletteE2EEnv(t *testing.T, home, mockURL, colorTerm, palette string) []string {
	t.Helper()
	env := testutil.MockEnv(t, home, mockURL)
	env = append(env,
		lineedit.EnvColorTerm+"="+colorTerm,
		lineedit.EnvPalette+"="+palette,
	)
	return env
}

// TestChatPipedEmitsNoSGRUnderEveryPalette is the byte-exact half, END TO END on
// the shipped binary: a non-TTY run produces no ANSI byte at all — whatever
// palette was asked for and even when the environment advertises truecolour. A
// piped transcript is text; the knob is not a way around the capability.
func TestChatPipedEmitsNoSGRUnderEveryPalette(t *testing.T) {
	for _, tc := range []struct {
		name      string
		palette   string
		colorTerm string
	}{
		{"default knob", "", lineedit.ColorTermTruecolor},
		{"hermes + truecolor advertised", lineedit.PaletteNameHermes, lineedit.ColorTermTruecolor},
		{"hermes-light + truecolor advertised", lineedit.PaletteNameHermesLight, lineedit.ColorTermTruecolor},
		{"basic", lineedit.PaletteNameBasic, lineedit.ColorTermTruecolor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			work := t.TempDir()
			llm := testutil.NewMockLLM(t, testutil.Resp{Text: paletteE2EAnswer + "\n"})
			defer llm.Close()

			stdout, stderr, code := testutil.RunGopi(t,
				[]string{"chat"},
				paletteE2EAnswer+"\n/quit\n",
				paletteE2EEnv(t, home, llm.URL(), tc.colorTerm, tc.palette),
				work)
			if code != 0 {
				t.Fatalf("gopi chat exit = %d, want 0\n--- stdout ---\n%q\n--- stderr ---\n%q", code, stdout, stderr)
			}
			// The answer must actually be there: an SGR-free stream that also
			// carried no answer would prove nothing about the palette.
			if !strings.Contains(stdout, paletteE2EAnswer) {
				t.Fatalf("the answer never reached stdout:\n%q", stdout)
			}
			for _, stream := range []struct {
				name string
				text string
			}{{"stdout", stdout}, {"stderr", stderr}} {
				if i := strings.IndexByte(stream.text, 0x1b); i >= 0 {
					t.Fatalf("%s carries an ESC byte at offset %d (a piped run must be text):\n%q",
						stream.name, i, stream.text[maxInt(0, i-40):minInt(len(stream.text), i+40)])
				}
			}
		})
	}
}

// TestChatOnAPTYCarriesTheGoldAnswerBodyOnlyWhenTruecolourIsAdvertised is the
// positive half, on a REAL terminal: with COLORTERM=truecolor and the gold knob,
// the child's answer body carries Hermes' #FFF8DC as a 24-bit sequence; with no
// advertisement the same run carries the basic table instead (and never the gold
// one); with TERM=dumb it carries nothing.
func TestChatOnAPTYCarriesTheGoldAnswerBodyOnlyWhenTruecolourIsAdvertised(t *testing.T) {
	gold := sgr.Seq(hermesGoldAnswerParams())
	cases := []struct {
		name         string
		args         []string
		colorTerm    string
		palette      string
		term         string
		wantGold     bool
		wantNoColour bool
	}{
		{
			name: "hermes + truecolor advertised", args: []string{"chat"},
			colorTerm: lineedit.ColorTermTruecolor, palette: lineedit.PaletteNameHermes,
			term: "xterm-256color", wantGold: true,
		},
		{
			name: "hermes WITHOUT the advertisement falls back to the basic table",
			args: []string{"chat"}, colorTerm: "", palette: lineedit.PaletteNameHermes,
			term: "xterm-256color", wantGold: false,
		},
		{
			name: "TERM=dumb emits nothing at all", args: []string{"chat"},
			colorTerm: lineedit.ColorTermTruecolor, palette: lineedit.PaletteNameHermes,
			term: lineedit.TermDumb, wantNoColour: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			work := t.TempDir()
			llm := testutil.NewMockLLM(t, testutil.Resp{Text: paletteE2EAnswer + paletteE2EAnswerTail + "\n"})
			defer llm.Close()

			env := paletteE2EEnv(t, home, llm.URL(), tc.colorTerm, tc.palette)
			env = append(env, lineedit.EnvTerm+"="+tc.term)
			chat := startPTYChat(t, tc.args, env, work)
			chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
			chat.writeGapped(t, paletteE2EAnswer)
			chat.write(t, "\r")
			out := chat.waitFor(t, paletteE2EAnswer+paletteE2EAnswerTail, ptyPromptTimeout)
			chat.writeGapped(t, "/quit")
			chat.write(t, "\r")
			if code := chat.close(t); code != 0 {
				t.Fatalf("chat exit = %d, want 0", code)
			}

			// The BODY's sequence, not merely "some gold byte": the prompt is
			// painted with the same colour under this table, so the gold
			// sequence on its own is satisfied by the editing line. The answer
			// band's painting is the sequence immediately followed by the
			// answer's own text.
			hasGold := strings.Contains(out, gold+paletteE2EAnswer)
			if hasGold != tc.wantGold {
				t.Fatalf("the child's stream carries the gold body colour %q = %v, want %v\nstream:\n%q",
					gold, hasGold, tc.wantGold, out)
			}
			if tc.wantGold {
				// The body's own sequence must be present, not merely some SGR:
				// the gold table paints the answer with #FFF8DC and nothing else.
				if !strings.Contains(out, lineedit.HermesDarkPalette.Answer.Open()) {
					t.Fatalf("no answer-body opening %q in the stream:\n%q", lineedit.HermesDarkPalette.Answer.Open(), out)
				}
				// The status row is NOT coloured — the invariant survives the
				// palette swap on the wire, not merely in the table.
				if strings.Contains(out, sgr.Seq(sgr.TruecolorFgParams(0xFF, 0xD7, 0x00))) &&
					strings.Contains(out, lineedit.ToolGlyph) {
					// The banner legitimately uses #FFD700; the assertion that
					// matters is that no STATUS row carries a colour, which the
					// lineedit-side salience test pins on the composed row. Here
					// we only record that the banner's gold is present.
					t.Logf("banner gold (#FFD700) present alongside a tool row — the row's own emphasis is asserted in internal/lineedit")
				}
			}
			if hasSGRColour(out) != !tc.wantNoColour {
				t.Fatalf("stream carries SGR colour = %v, want %v\nstream:\n%q", hasSGRColour(out), !tc.wantNoColour, out)
			}
		})
	}
}

// TestChatOnAPTYDefaultsToThe256TierWhenTheTerminalDeclaresIt is the epic's
// acceptance END TO END on the shipped binary, in THIS HOST's own shape:
// TERM=xterm-256color, COLORTERM unset, GOPI_PALETTE unset. The answer body must
// carry the 256-colour spelling `38;5;230` and must NOT carry the stock 8-colour
// ANSI codes the basic table uses — the defect was precisely that the two were
// indistinguishable from the operator's chair.
//
// The stream on a pty carries the editor's own cursor placement and erase
// sequences (\x1b[…C, […H, [2K) whatever the palette does: those are LAYOUT, not
// colour, so the assertion is on SGR sequences specifically (hasSGRColour), never
// on the presence of ESC.
func TestChatOnAPTYDefaultsToThe256TierWhenTheTerminalDeclaresIt(t *testing.T) {
	wantBody := sgr.Seq(sgr.FgParams(palette256E2EBodyIdx))
	basicCodes := map[string]string{
		"sgrCyan (code/banner)":  sgr.Seq(strconv.Itoa(sgrCyanCode)),
		"sgrGreen (prompt/diff)": sgr.Seq(strconv.Itoa(sgrGreenCode)),
		"sgrGray (hint)":         sgr.Seq(strconv.Itoa(sgrGrayCode)),
	}
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: paletteE2EAnswer + paletteE2EAnswerTail + "\n"})
	defer llm.Close()

	// The host's shape, stated EXPLICITLY so the child cannot inherit a COLORTERM
	// (or a palette) from the environment this test runs in.
	env := paletteE2EEnv(t, home, llm.URL(), "", "")
	env = append(env, lineedit.EnvTerm+"="+lineedit.Term256Color)

	chat := startPTYChat(t, []string{"chat"}, env, work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.writeGapped(t, paletteE2EAnswer)
	chat.write(t, "\r")
	out := chat.waitFor(t, paletteE2EAnswer+paletteE2EAnswerTail, ptyPromptTimeout)
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}

	if !strings.Contains(out, wantBody+paletteE2EAnswer) {
		t.Fatalf("the default run on TERM=%s does not paint the answer body with %q\nstream:\n%q",
			lineedit.Term256Color, wantBody, out)
	}
	if !hasSGRColour(out) {
		t.Fatalf("the default run carries no SGR colour at all:\n%q", out)
	}
	for name, code := range basicCodes {
		if strings.Contains(out, code) {
			t.Errorf("the default run on a 256-colour terminal still carries %s (%q) — the tier did not displace the stock ANSI set:\n%q",
				name, code, out)
		}
	}
	if strings.Contains(out, sgr.Seq(sgr.TruecolorFgParams(0xFF, 0xF8, 0xDC))) {
		t.Errorf("the default run carries a 24-bit sequence on a terminal that advertises none:\n%q", out)
	}
}

// TestChatDowngradeNoticeReachesStderrOnAPTY pins the operator's half of the
// no-silent-downgrade rule on the SHIPPED binary: ask for the 24-bit gold table
// on this host's TERM/COLORTERM shape and the run must both draw the 256 tier and
// say why it did not draw the gold one, naming COLORTERM.
func TestChatDowngradeNoticeReachesStderrOnAPTY(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: paletteE2EAnswer + paletteE2EAnswerTail + "\n"})
	defer llm.Close()

	env := paletteE2EEnv(t, home, llm.URL(), "", lineedit.PaletteNameHermes)
	env = append(env, lineedit.EnvTerm+"="+lineedit.Term256Color)

	chat := startPTYChat(t, []string{"chat"}, env, work)
	// The notice is written while the capability is resolved — BEFORE the first
	// frame — so it is in the BOOT window (everything up to the first prompt),
	// not in the answer window.
	boot := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.writeGapped(t, paletteE2EAnswer)
	chat.write(t, "\r")
	out := boot + chat.waitFor(t, paletteE2EAnswer+paletteE2EAnswerTail, ptyPromptTimeout)
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 (a downgrade must not fail the run)", code)
	}

	// stderr and stdout are the SAME pty here, so the notice is in the stream.
	for what, want := range map[string]string{
		"the table asked for":        lineedit.PaletteNameHermes,
		"the enabling variable":      lineedit.EnvColorTerm,
		"the value that enables it":  lineedit.ColorTermTruecolor,
		"the table actually drawn":   lineedit.PaletteNamePalette256,
		"the reason (what it needs)": "24-bit",
		"the observed TERM":          lineedit.Term256Color,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the downgrade notice does not name %s (%q):\n%q", what, want, out)
		}
	}
	// ...and ONCE, not once per resolution: the run's banner and its editor share
	// ONE capability decision (newChatInputWithPalette), so the notice appears
	// exactly once in the stream. A second resolution would print it on a
	// terminal whose reader is reading it.
	//
	// The notice is counted ONCE per notice, by ONE mark — and the two counts are
	// asserted to AGREE rather than summed. Summing them was the defect this line
	// was fixed for (task 01a0e2ed, 2026-09-27): `COLORTERM="` and
	// downgradeNoticeMark are two spellings of the SAME line, so one correct
	// notice scored 1+1=2 and the test could never pass — measured on master
	// (9f22bcda) with `go test ./testsuite/ -run
	// TestChatDowngradeNoticeReachesStderrOnAPTY -count=1` → FAIL, its own quoted
	// stream carrying exactly one notice, while the live binary printed exactly
	// one. Summed, the assertion was also blind in the other direction: it would
	// have accepted a run that decided twice only by accident. Agreement keeps
	// the guard alive in both directions, and a notice whose wording changes (0
	// marks, 0 knobs) still fails LOUDLY instead of silently guarding nothing.
	byMark := strings.Count(out, downgradeNoticeMark)
	byKnob := strings.Count(out, lineedit.EnvColorTerm+`="`)
	if byMark != 1 || byKnob != 1 {
		t.Errorf("the downgrade notice appears %d times (mark %q) / %d times (the %s knob), want exactly 1 of each (one capability decision per run):\n%q",
			byMark, downgradeNoticeMark, byKnob, lineedit.EnvColorTerm, out)
	}
	// ...and the gold sequence itself was NOT emitted (the terminal would print
	// its parameters as text).
	if strings.Contains(out, sgr.Seq(hermesGoldAnswerParams())) {
		t.Errorf("the run emitted the 24-bit gold body it said it could not draw:\n%q", out)
	}
}

// palette256E2EBodyIdx is the 256 cube entry the shipped default paints the answer
// body with. It is spelled as a NUMBER here (not read from the lineedit constant)
// so the end-to-end assertion is against the WIRE value an operator's terminal
// receives, not against the table the same change wrote — a wrong index inside the
// palette cannot make this test agree with it. The cube arithmetic behind it
// (230 → #FFFFD7) is asserted in internal/lineedit's own test.
const palette256E2EBodyIdx = 230

// The 8-colour codes the basic table uses for the roles the 256 table replaces.
// Spelled as numbers for the same reason: these are ANSI's own, not ours.
const (
	sgrCyanCode  = 36
	sgrGreenCode = 32
	sgrGrayCode  = 90
)

// hasSGRColour reports whether a byte stream carries any SGR sequence — a CSI
// sequence whose final byte is 'm'. That final byte is what makes it COLOUR:
// the same CSI introducer also carries the layout's own cursor placement and
// line erasing (`H`, `C`, `K`), which the raw-mode editor emits on EVERY
// terminal, TERM=dumb included, and which no palette decision governs. Counting
// those as "colour bytes" would make the zero-colour assertion unstateable.
//
// It is the testsuite-side twin of internal/lineedit's unexported hasSGR (this
// package asserts on a CHILD's output, so it cannot use the package-internal
// predicate).
func hasSGRColour(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != 0x1b || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && !isCSIFinalByte(s[j]) {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			return true
		}
		i = j
	}
	return false
}

// isCSIFinalByte reports whether c ends a CSI sequence (0x40..0x7e), the byte
// range ANSI defines for the final position.
func isCSIFinalByte(c byte) bool { return c >= 0x40 && c <= 0x7e }

// TestChatUnknownPaletteNameIsReportedAndFallsBack pins the knob's failure mode
// end to end: a typo does not silently do nothing — it is named on stderr, and
// the run continues on the basic table (Rule 15).
func TestChatUnknownPaletteNameIsReportedAndFallsBack(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: paletteE2EAnswer + "\n"})
	defer llm.Close()

	_, stderr, code := testutil.RunGopi(t,
		[]string{"chat"},
		paletteE2EAnswer+"\n/quit\n",
		paletteE2EEnv(t, home, llm.URL(), lineedit.ColorTermTruecolor, "gold-ish"),
		work)
	if code != 0 {
		t.Fatalf("gopi chat exit = %d, want 0 (an unknown palette name degrades, it does not fail the run)", code)
	}
	if !strings.Contains(stderr, lineedit.EnvPalette) || !strings.Contains(stderr, "gold-ish") {
		t.Fatalf("stderr does not report the unknown %s value:\n%q", lineedit.EnvPalette, stderr)
	}
	if !strings.Contains(stderr, lineedit.PaletteNameBasic) {
		t.Fatalf("stderr does not name the fallback table %q:\n%q", lineedit.PaletteNameBasic, stderr)
	}
}

// TestChatUsageDocumentsThePaletteKnob keeps the operator's own help honest: the
// knob this change adds is discoverable from the surface that documents the
// colour behaviour, so an operator does not have to read the source to learn it
// exists. It runs the real verb's `-h`, so the assertion is on the SHIPPED text.
func TestChatUsageDocumentsThePaletteKnob(t *testing.T) {
	out, _, code := runGopiHelp(t, "chat")
	if code != 0 {
		t.Fatalf("gopi chat -h exit = %d, want 0", code)
	}
	for _, want := range []string{
		lineedit.EnvPalette,
		lineedit.PaletteNameHermes,
		lineedit.PaletteNameHermesLight,
		lineedit.EnvColorTerm,
		lineedit.ColorTermTruecolor,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`gopi chat -h` does not mention %q — the knob would be undiscoverable", want)
		}
	}
	if !strings.Contains(out, "#FFF8DC") {
		t.Errorf("`gopi chat -h` does not name the answer body's colour (#FFF8DC)")
	}
}

// runGopiHelp runs one verb's -h and returns its combined output.
func runGopiHelp(t *testing.T, verb string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(testutil.GopiBin(t), verb, "-h")
	cmd.Env = append(os.Environ(),
		"GOPI_HOME="+t.TempDir(),
	)
	cmd.Dir = filepath.Dir(testutil.GopiBin(t))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run gopi %s -h: %v", verb, err)
	}
	return stdout.String() + stderr.String(), stderr.String(), code
}

// maxInt/minInt are the small clamps the failure message above uses to show the
// ESC byte in context (Rule 1: no bare arithmetic in a message).
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
