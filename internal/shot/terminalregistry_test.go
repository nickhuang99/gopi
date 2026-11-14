package shot

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestFilmTerminalEveryUsableRowCarriesTheGround is the GROUND regression, and
// the defect it pins was measured rather than imagined.
//
// WHAT WENT WRONG (2026-10-04). `gopi chat --replay <id> --film DIR --out f.gif`
// produced 150 frames that were 96 % PURE WHITE with gopi's pale-gold chrome on
// them: the filmed xterm had been started with no ground, so it used its own
// DEFAULT — white paper — while every palette the chat surface draws with
// (internal/lineedit: Hermes' #FFF8DC body, #FFD700 primary) was composed against
// a dark one. The film was a picture of a surface at roughly 1.2:1 contrast, and
// the operator's verdict ("不像 gopi 聊天界面") was correct.
//
// HOW IT IS CAUGHT HERE, at the level the starter can be held to with no display:
// EVERY usable row must declare BOTH ground switches, and TerminalArgs must emit
// the values internal/lineedit declares. A row that forgets one is REFUSED rather
// than filmed on a default nobody chose, which is what makes this a gate and not
// a preference.
func TestFilmTerminalEveryUsableRowCarriesTheGround(t *testing.T) {
	usable := 0
	for _, spec := range filmTerminalRegistry() {
		if !spec.usable() {
			// A refused row is never started, so it needs no ground — but saying
			// so explicitly is what stops the next reader from reading its empty
			// fields as an oversight.
			continue
		}
		usable++
		if spec.GroundBackgroundFlag == "" || spec.GroundForegroundFlag == "" {
			t.Errorf("%s declares no ground (%q/%q): a film in it photographs the terminal's OWN default instead of the dark ground the chat palettes were composed against",
				spec.Name, spec.GroundBackgroundFlag, spec.GroundForegroundFlag)
			continue
		}
		got, err := spec.TerminalArgs("117x26", []string{"gopi", "chat"}, "t", "")
		if err != nil {
			t.Fatalf("%s.TerminalArgs: %v", spec.Name, err)
		}
		joined := strings.Join(got, " ")
		for _, want := range []string{lineedit.GroundBackgroundHex, lineedit.GroundForegroundHex} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s's argv does not carry the declared ground %s: %v", spec.Name, want, got)
			}
		}
	}
	if usable < 2 {
		t.Fatalf("only %d usable terminal(s) in the registry — this test would pass vacuously", usable)
	}
}

// TestFilmTerminalGroundRefusedWhenTheRowHasNoSwitch is the Rule-15 half: a row
// this driver has NO ground switch for must be REFUSED by name, never filmed on
// its own default. The refusal names the terminal and the two knobs, so a caller
// can act on it without reading this package.
func TestFilmTerminalGroundRefusedWhenTheRowHasNoSwitch(t *testing.T) {
	spec := FilmTerminalSpec{
		Name:             "future-terminal",
		GeometryFlag:     filmTerminalGeometryFlag,
		GeometryAccepted: true,
		ExecFlag:         filmTerminalExecFlag,
		WindowRule:       FilmWindowRulePIDTitleRect,
	}
	_, err := spec.TerminalArgs("117x26", []string{"x"}, "", "")
	if err == nil {
		t.Fatal("a terminal with no ground switch was started anyway — the film would be a picture of a ground nobody chose")
	}
	for _, want := range []string{spec.Name, lineedit.GroundBackgroundName, lineedit.GroundForegroundName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// The registry is DATA, and these tests drive the DATA through the real
// consumers (TerminalArgs, FilmTerminalSpecByName, ResolveFilmTerminal) rather
// than restating the table — a test that copied the table would agree with it
// while the starter disagreed with both.

// TestFilmTerminalRegistryShape pins the three rows' launch shape at the level a
// reader checks: the exact argv each terminal is started with. It is the
// anti-regression guard for the one thing this file exists to fix — xterm's flag
// protocol must not leak into another terminal (measured 2026-10-03: xfce4
// rejects `-geometry 117x26` in two argv slots and needs `--geometry=117x26`, and
// it needs `-x` where xterm needs `-e`).
func TestFilmTerminalRegistryShape(t *testing.T) {
	cases := []struct {
		name     string
		geometry string
		title    string
		session  string
		argv     []string
		want     []string
	}{
		{
			name: FilmTerminalNameXterm, geometry: "117x26", title: "t", session: "/tmp/s.log",
			argv: []string{"gopi", "chat", "--once", "hi"},
			// The GROUND pair sits between the title and the session log, in this
			// terminal's own spelling (two options, the colour in the next slot).
			// It is part of the argv for a measured reason, not for symmetry —
			// see TestFilmTerminalEveryUsableRowCarriesTheGround.
			want: []string{"-geometry", "117x26", "-T", "t", "-bg", lineedit.GroundBackgroundHex, "-fg", lineedit.GroundForegroundHex, "-l", "-lf", "/tmp/s.log", "-e", "gopi", "chat", "--once", "hi"},
		},
		{
			name: FilmTerminalNameXfce4, geometry: "117x26", title: "t", session: "",
			argv: []string{"gopi", "chat", "--once", "hi"},
			want: []string{"--geometry=117x26", "-T", "t", "--color-bg=" + lineedit.GroundBackgroundHex, "--color-text=" + lineedit.GroundForegroundHex, "-x", "gopi", "chat", "--once", "hi"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := FilmTerminalSpecByName(c.name)
			if err != nil {
				t.Fatalf("FilmTerminalSpecByName(%q) refused: %v", c.name, err)
			}
			got, err := spec.TerminalArgs(c.geometry, c.argv, c.title, c.session)
			if err != nil {
				t.Fatalf("TerminalArgs: %v", err)
			}
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("argv for %s:\n got %v\nwant %v", c.name, got, c.want)
			}
		})
	}
}

// TestFilmTerminalNoSessionLogIsOmittedAndAnnounced is the Rule-15 half of the
// table: a terminal with NO session-log switch must never be handed xterm's -l/-lf
// (a foreign flag would start it with an argument it does not know, or start a
// different command than the one asked for) — and the omission must be
// ANNOUNCEABLE, because a film without its producer log still films, while an
// empty producer.log that nobody explained reads as "the producer printed
// nothing" (the wrong conclusion to leave behind).
func TestFilmTerminalNoSessionLogIsOmittedAndAnnounced(t *testing.T) {
	spec, err := FilmTerminalSpecByName(FilmTerminalNameXfce4)
	if err != nil {
		t.Fatalf("xfce4-terminal must be a usable row: %v", err)
	}
	if spec.SupportsSessionLog() {
		t.Fatal("xfce4-terminal reports a session-log switch it does not have")
	}
	got, err := spec.TerminalArgs("117x26", []string{"x"}, "", "/tmp/session.log")
	if err != nil {
		t.Fatalf("a session log must not fail the start — the log is evidence, not a precondition: %v", err)
	}
	joined := strings.Join(got, " ")
	for _, banned := range []string{filmTerminalLogFlag, filmTerminalLogFileFlag, "/tmp/session.log"} {
		if strings.Contains(joined, banned) {
			t.Errorf("xterm's session-log protocol (%q) leaked into xfce4-terminal's argv: %v", banned, got)
		}
	}
	notice := SessionLogAbsentNotice(spec, "/tmp/session.log")
	for _, want := range []string{FilmTerminalNameXfce4, "/tmp/session.log", "pty"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice must name %q, got: %s", want, notice)
		}
	}
	// xterm keeps its own channel, and it is still emitted for it.
	x, err := FilmTerminalSpecByName(FilmTerminalNameXterm)
	if err != nil {
		t.Fatalf("xterm: %v", err)
	}
	if !x.SupportsSessionLog() {
		t.Error("xterm reports no session-log switch — its -l/-lf channel is the one this pipeline measured with")
	}
}

// TestFilmTerminalUnknownNameRefusesWithTheList pins the unknown-name contract:
// the refusal names the name, the reason, and the USABLE list — and no usable
// list entry is a name the driver would refuse (the list is derived from the
// registry, so this also catches a list that grew a terminal nobody can start).
func TestFilmTerminalUnknownNameRefusesWithTheList(t *testing.T) {
	_, err := FilmTerminalSpecByName("konsole")
	if err == nil {
		t.Fatal("an unknown terminal name was accepted")
	}
	msg := err.Error()
	for _, want := range []string{"konsole", filmTerminalExactNameReason, FilmTerminalUsableList()} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	for _, name := range strings.Split(FilmTerminalUsableList(), FilmTerminalNameListSep) {
		if _, err := FilmTerminalSpecByName(name); err != nil {
			t.Errorf("the usable list advertises %q, which the resolver refuses: %v", name, err)
		}
	}
}

// TestFilmTerminalKnownButUnusableNamesItsOwnReason is the distinction that makes
// the refusal usable: a terminal the driver KNOWS and cannot use must not read
// like a typo. gnome-terminal's row carries that reason, and the empty value is
// its own refusal too.
func TestFilmTerminalKnownButUnusableNamesItsOwnReason(t *testing.T) {
	if _, err := FilmTerminalSpecByName(FilmTerminalNameGnome); err == nil {
		t.Fatal("gnome-terminal resolved as usable — its row's measurement says otherwise")
	} else if !strings.Contains(err.Error(), "known but NOT usable") || !strings.Contains(err.Error(), FilmTerminalNameGnome) {
		t.Errorf("a known-but-unusable terminal must be refused as such: %v", err)
	}
	if _, err := FilmTerminalSpecByName("  "); err == nil {
		t.Error("an empty name was accepted")
	}
	// A KNOWN name must never be reported as unknown (the two are different facts).
	if _, err := FilmTerminalSpecByName(FilmTerminalNameGnome); err != nil && strings.Contains(err.Error(), "not a terminal this film driver knows") {
		t.Errorf("a known terminal was refused as unknown: %v", err)
	}
}

// TestFilmTerminalUsableListExcludesRefusals states the list's own invariant.
func TestFilmTerminalUsableListExcludesRefusals(t *testing.T) {
	list := FilmTerminalUsableList()
	if list == "" {
		t.Fatal("no terminal is usable — a film could never be made")
	}
	for _, s := range filmTerminalRegistry() {
		inList := strings.Contains(list, s.Name)
		if s.usable() && !inList {
			t.Errorf("usable terminal %s is missing from the list %q", s.Name, list)
		}
		if !s.usable() && inList {
			t.Errorf("unusable terminal %s is advertised as usable in %q", s.Name, list)
		}
	}
}

// TestWindowForSpecPrefersTheRealWindowOverThePlaceholder is the window-selection
// regression: xfce4-terminal maps THREE top-levels under one pid (measured
// 2026-10-03), so "the first window of this pid" photographs a 10x10 placeholder.
// The rule is pid AND title AND a real rectangle.
func TestWindowForSpecPrefersTheRealWindowOverThePlaceholder(t *testing.T) {
	spec, err := FilmTerminalSpecByName(FilmTerminalNameXfce4)
	if err != nil {
		t.Fatalf("xfce4-terminal: %v", err)
	}
	list := XWindowList{Windows: []XWindowInfo{
		{ID: "0x400001", Name: "Xfce Terminal", PID: 42, W: 10, H: 10, Measured: true},
		{ID: "0x400009", Name: "", PID: 42, W: 1, H: 1, Measured: true},
		{ID: "0x400003", Name: "gopi-film", PID: 42, W: 1186, H: 521, Measured: true},
	}}
	got, ok := windowForSpec(list, spec, 42, "gopi-film")
	if !ok {
		t.Fatal("the real window was not selected")
	}
	if got.ID != "0x400003" {
		t.Errorf("selected %s (%s %dx%d) — the placeholder won over the real window", got.ID, got.Name, got.W, got.H)
	}
	// A pid that owns only unusable windows selects nothing (never a 10x10 frame).
	onlyPlaceholder := XWindowList{Windows: []XWindowInfo{{ID: "0x1", Name: "Xfce Terminal", PID: 7, W: 10, H: 10, Measured: true}}}
	if _, ok := windowForSpec(onlyPlaceholder, spec, 7, ""); ok {
		t.Error("a 10x10 placeholder was selected as the filmed window")
	}
	// An unmeasured window has no area to photograph.
	unmeasured := XWindowList{Windows: []XWindowInfo{{ID: "0x2", Name: "gopi-film", PID: 7, W: 1186, H: 521, Measured: false}}}
	if _, ok := windowForSpec(unmeasured, spec, 7, "gopi-film"); ok {
		t.Error("an unmeasured rectangle was selected as the filmed window")
	}
	// Another process's window is never selected, whatever its size.
	otherPID := XWindowList{Windows: []XWindowInfo{{ID: "0x3", Name: "gopi-film", PID: 99, W: 1186, H: 521, Measured: true}}}
	if _, ok := windowForSpec(otherPID, spec, 7, "gopi-film"); ok {
		t.Error("a window of another pid was selected")
	}
	// The title disambiguates when one pid owns two real windows.
	twoReal := XWindowList{Windows: []XWindowInfo{
		{ID: "0xa", Name: "Xfce Terminal", PID: 7, W: 1186, H: 521, Measured: true},
		{ID: "0xb", Name: "gopi-film", PID: 7, W: 816, H: 483, Measured: true},
	}}
	if got, ok := windowForSpec(twoReal, spec, 7, "gopi-film"); !ok || got.ID != "0xb" {
		t.Errorf("the title did not disambiguate: got %+v ok=%v", got, ok)
	}
}

// TestFilmTerminalDefaultRendersCJK pins the built-in default at the level the
// change was made for. It asserts three things a reader can check without a
// display (the pixel-level half lives in devdoc/film-cjk-default-terminal.md):
//
//  1. FilmTerminalDefault IS xfce4-terminal — the operator's 2026-10-04 ruling
//     that a CJK surface is never filmed in xterm's ISO8859-1 font, pinned so a
//     later edit cannot quietly walk it back;
//  2. the default RESOLVES — a default that named a refused row would make
//     `gopi chat --film` fail for every caller who said nothing, so the default
//     is held to the same usability every other row is (Rule 15: the refusal is
//     an outcome for a NAME, never for the value nobody had to ask for);
//  3. xterm is still SELECTABLE and its launch shape is untouched — the ruling
//     demoted it from the default, it did not remove it, and every historical
//     measurement of this pipeline (706x342 = 117x26 x 6.034 px/cell, 106/106
//     settled frames) was taken with it.
func TestFilmTerminalDefaultRendersCJK(t *testing.T) {
	if FilmTerminalDefault != FilmTerminalNameXfce4 {
		t.Fatalf("the built-in default is %q, want %q: xterm's default font fixed(6x13) is ISO8859-1 and carries no CJK glyph, so a Chinese chat surface films as boxes",
			FilmTerminalDefault, FilmTerminalNameXfce4)
	}
	// The default must be USABLE — a refusal here would strand every caller who
	// named no terminal.
	spec, err := FilmTerminalSpecByName(FilmTerminalDefault)
	if err != nil {
		t.Fatalf("the built-in default %q does not resolve: %v", FilmTerminalDefault, err)
	}
	if !spec.usable() {
		t.Fatalf("the built-in default %q is a refused row — a caller who said nothing would get no film at all", FilmTerminalDefault)
	}
	// The refusal machinery must never be what the default hits: check the two
	// expressions a starter uses, so "usable" is measured rather than declared.
	if spec.refusalText() != "" {
		t.Errorf("the default %q carries a refusal text: %s", FilmTerminalDefault, spec.refusalText())
	}
	args, aerr := spec.TerminalArgs("117x26", []string{"gopi", "chat"}, "t", "")
	if aerr != nil {
		t.Fatalf("the default %q cannot compose an argv: %v", FilmTerminalDefault, aerr)
	}
	if len(args) == 0 {
		t.Fatalf("the default %q composed an empty argv", FilmTerminalDefault)
	}
	// And the resolved default is the one the RESOLVER hands a caller who said
	// nothing on any channel — the whole point of the change.
	t.Setenv(FilmTerminalEnv, "")
	t.Setenv("GOPI_SETTINGS", t.TempDir()+"/absent-settings.json")
	got, source, rerr := ResolveFilmTerminal("")
	if rerr != nil {
		t.Fatalf("resolve with no channel set: %v", rerr)
	}
	if got.Name != FilmTerminalDefault || source != FilmTerminalSourceDefault {
		t.Errorf("a caller who named nothing got %q via %q, want %q via %s",
			got.Name, source, FilmTerminalDefault, FilmTerminalSourceDefault)
	}
	// demotion, not removal: xterm stays a name this driver knows and can start.
	xterm, xerr := FilmTerminalSpecByName(FilmTerminalNameXterm)
	if xerr != nil {
		t.Fatalf("%s is no longer selectable: %v", FilmTerminalNameXterm, xerr)
	}
	if xterm.Name != FilmTerminalNameXterm {
		t.Errorf("xterm resolved to %q", xterm.Name)
	}
	if !strings.Contains(FilmTerminalUsableList(), FilmTerminalNameXterm) {
		t.Errorf("the usable list dropped %s: %s", FilmTerminalNameXterm, FilmTerminalUsableList())
	}
}

// TestFilmTerminalDefaultIsAUsableRow is the table-wide form of the rule above:
// WHATEVER the default names, that row must be startable. It is written against
// the registry rather than against the current value, so it keeps protecting the
// next change of default as well — the failure it prevents is the expensive one,
// where the value nobody had to ask for is the one value the driver refuses.
func TestFilmTerminalDefaultIsAUsableRow(t *testing.T) {
	for _, s := range filmTerminalRegistry() {
		if s.Name != FilmTerminalDefault {
			continue
		}
		if !s.usable() {
			t.Fatalf("the registry row for the default %q is a REFUSAL: %s", FilmTerminalDefault, s.refusalText())
		}
		if strings.TrimSpace(s.Binary) == "" {
			t.Fatalf("the default %q names no binary", FilmTerminalDefault)
		}
		if s.WindowRule != FilmWindowRulePIDTitleRect {
			t.Errorf("the default %q uses window rule %q; the film's window selection is %q",
				FilmTerminalDefault, s.WindowRule, FilmWindowRulePIDTitleRect)
		}
		if !s.GeometryAccepted {
			t.Errorf("the default %q does not accept the layout's own geometry — the film would be drawn at a size its layout did not use", FilmTerminalDefault)
		}
		return
	}
	t.Fatalf("the built-in default %q is not a row of the registry at all", FilmTerminalDefault)
}

// TestWindowForSpecRefusesAnUnknownRule keeps the selection rule a NAME rather
// than a default: a spec whose rule the driver does not implement selects
// NOTHING (Rule 15 — falling back to a weaker rule is how a film silently
// photographs the wrong window).
func TestWindowForSpecRefusesAnUnknownRule(t *testing.T) {
	unknown := FilmTerminalSpec{Name: "future-terminal", WindowRule: "whatever-is-first"}
	list := XWindowList{Windows: []XWindowInfo{{ID: "0x1", Name: "x", PID: 7, W: 800, H: 600, Measured: true}}}
	if _, ok := windowForSpec(list, unknown, 7, ""); ok {
		t.Error("a spec with an unknown window rule still selected a window")
	}
}
