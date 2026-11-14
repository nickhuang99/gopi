package main

// chat_sgr_stream_test.go — the switch's own vocabulary, pinned without starting a
// chat: the env channel, the joined-form flag, the refused separated form, and the
// film refusal.
//
// These are unit cases for the PARSER, not for the recording. The recording has a
// real-pty acceptance case (testsuite/chat_sgr_stream_pty_test.go) whose judge is
// the kernel's pty; what is pinned here is the part that fails SILENTLY when it is
// wrong — a switch nobody armed, or a path nobody wrote down.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// TestChatSgrEnvChannel pins the env channel's FOUR states, which are the whole
// of Rule 15's requirement here: unset is the DEFAULT (recorded), a falsey value
// is OFF, a truthy value is ON at the resolved path, and any other value IS the
// path. Collapsing any two of them is the defect this case exists to prevent —
// "armed by default" must not make `GOPI_SCREEN_STREAM=0` a path named `0`.
func TestChatSgrEnvChannel(t *testing.T) {
	// Unset: the DEFAULT — the stream IS recorded, and nothing was stated.
	t.Setenv(chatSgrStreamEnv, "")
	got := chatSgrFromEnv()
	if !got.recorded() || got.disabled || got.path != "" || got.stated {
		t.Fatalf("unset %s produced %+v, want the armed-by-default state with nothing stated", chatSgrStreamEnv, got)
	}

	// A falsey value: OFF, and STATED — a decline is a decision, not a silence.
	for _, falsey := range []string{"0", "false", "off", "no", "disabled"} {
		t.Setenv(chatSgrStreamEnv, falsey)
		got := chatSgrFromEnv()
		if got.recorded() || !got.disabled || !got.stated || got.path != "" {
			t.Errorf("%s=%q produced %+v, want the declined state (disabled+stated, no path)", chatSgrStreamEnv, falsey, got)
		}
	}

	// Truthy: ON, with the path left to the resolver, and stated.
	for _, truthy := range []string{"1", "true", "yes", "on"} {
		t.Setenv(chatSgrStreamEnv, truthy)
		got := chatSgrFromEnv()
		if !got.recorded() || got.disabled || got.path != "" || !got.stated {
			t.Errorf("%s=%q produced %+v, want recorded with no explicit path", chatSgrStreamEnv, truthy, got)
		}
	}

	// Anything else IS the path.
	t.Setenv(chatSgrStreamEnv, "/var/tmp/run.screen.stream")
	if got := chatSgrFromEnv(); !got.recorded() || got.path != "/var/tmp/run.screen.stream" {
		t.Errorf("%s as a path produced %+v, want that path", chatSgrStreamEnv, got)
	}
}

// TestChatSgrDefaultIsArmed pins the DEFAULT on its own, apart from any env: this
// is the epic's whole subject, and a case that only observed it THROUGH the env
// reader would pass if the reader returned the default by accident.
func TestChatSgrDefaultIsArmed(t *testing.T) {
	got := chatSgrDefault()
	if !got.recorded() {
		t.Fatalf("the raw stream's default is NOT armed: %+v — the epic requires it ON", got)
	}
	if got.stated {
		t.Errorf("the DEFAULT counts as a statement (%+v) — it would supersede a resumed session's recorded destination", got)
	}
	if got.path != "" {
		t.Errorf("the DEFAULT names a destination (%q) — the resolver chooses it", got.path)
	}
}

// TestChatSgrFlagForms pins both accepted spellings and both refusals BY NAME.
func TestChatSgrFlagForms(t *testing.T) {
	// The bare flag: enabled, resolver's path.
	got, matched, err := parseChatSgrArg(chatSgrStreamFlag, "", false)
	if !matched || err != nil || !got.enabled || got.path != "" {
		t.Fatalf("bare %s → %+v matched=%v err=%v", chatSgrStreamFlag, got, matched, err)
	}

	// The joined value: that exact path.
	got, matched, err = parseChatSgrArg(chatSgrStreamFlag+"=/x/y.stream", "", false)
	if !matched || err != nil || got.path != "/x/y.stream" || !got.enabled {
		t.Fatalf("joined form → %+v matched=%v err=%v", got, matched, err)
	}

	// The OFF switch: no path, disabled, and STATED.
	got, matched, err = parseChatSgrArg(chatNoSgrStreamFlag, "", false)
	if !matched || err != nil || !got.disabled || got.recorded() || got.path != "" || !got.stated {
		t.Fatalf("%s → %+v matched=%v err=%v, want the declined state", chatNoSgrStreamFlag, got, matched, err)
	}

	// The OFF switch takes NO value, in EITHER spelling — refused BY NAME, and the
	// refusal names the flag that DOES take a destination (Rule 15).
	if _, matched, err = parseChatSgrArg(chatNoSgrStreamFlag, "x", true); !matched || err == nil {
		t.Fatalf("a separated value after %s matched=%v err=%v, want a refusal", chatNoSgrStreamFlag, matched, err)
	} else if !strings.Contains(err.Error(), chatSgrStreamFlag+"=x") {
		t.Errorf("the off switch's refusal does not name the flag that takes a destination: %v", err)
	}
	if _, matched, err = parseChatSgrArg(chatNoSgrStreamFlag+"=x", "", false); !matched || err == nil {
		t.Fatalf("%s=x matched=%v err=%v, want a refusal by name", chatNoSgrStreamFlag, matched, err)
	} else if !strings.Contains(err.Error(), chatNoSgrStreamFlag) {
		t.Errorf("the joined-form refusal does not name the off switch: %v", err)
	}

	// A word outside the vocabulary is NOT this parser's business.
	if _, matched, _ := parseChatSgrArg("--dump-screen", "", false); matched { //stylegate:ignore parseChatSgrArg returns an error ONLY on the separated-path refusal, and that arm also sets matched — so matched==false here implies err==nil and the discard cannot hide a failure
		t.Fatalf("the parser claimed %s, which belongs to the dump", chatDumpFlag)
	}
	if _, matched, _ := parseChatSgrArg(chatFlagTools, "", false); matched { //stylegate:ignore the same two-arm contract: an error is only ever returned together with matched==true, so this negative assertion on matched cannot be made to pass by swallowing one
		t.Fatalf("the parser claimed %s", chatFlagTools)
	}

	// The SEPARATED form is refused, and the refusal names the joined form that
	// works: an accepted separated path would be read as a session seed, which is
	// a flag quietly doing something else.
	_, matched, err = parseChatSgrArg(chatSgrStreamFlag, "/x/y.stream", true)
	if !matched || err == nil {
		t.Fatalf("separated form matched=%v err=%v, want a refusal", matched, err)
	}
	if !strings.Contains(err.Error(), chatSgrStreamFlag+"=/x/y.stream") {
		t.Errorf("the refusal does not spell the joined form that works: %v", err)
	}
}

// TestChatSgrFilmRefusal pins that the FILM refusal is about the flag only: the
// environment is the channel that works in a film run, so refusing it too would
// remove the only way to record a film.
func TestChatSgrFilmRefusal(t *testing.T) {
	if err := chatFilmSgrRefusal(false, chatSgrDefault()); err != nil {
		t.Fatalf("a film line with no raw-stream word was refused: %v", err)
	}
	err := chatFilmSgrRefusal(true, chatSgrOptions{enabled: true, stated: true})
	if err == nil {
		t.Fatal("a film line carrying the raw-stream flag was accepted — it would write nothing and say nothing")
	}
	if !strings.Contains(err.Error(), chatSgrStreamEnv) {
		t.Errorf("the refusal does not name the channel that works (%s): %v", chatSgrStreamEnv, err)
	}
	// The OFF switch has its OWN sentence: the film's producer would keep
	// RECORDING, which is the opposite failure and must not be described as
	// "would write nothing".
	offErr := chatFilmSgrRefusal(true, chatSgrOptions{disabled: true, stated: true})
	if offErr == nil {
		t.Fatal("a film line carrying the OFF switch was accepted — the filmed run would keep recording")
	}
	if offErr.Error() == err.Error() {
		t.Error("the arm refusal and the decline refusal share one sentence — they describe opposite failures")
	}
	if !strings.Contains(offErr.Error(), chatNoSgrStreamFlag) || !strings.Contains(offErr.Error(), chatSgrStreamEnv) {
		t.Errorf("the decline refusal names neither the switch nor the channel that works: %v", offErr)
	}
}

// TestChatResolveSgrPathExplicitWins pins the resolver's first rule and that it
// CREATES the directory it was given (a path whose parent does not exist is the
// failure mode that reads as "the switch is broken").
func TestChatResolveSgrPathExplicitWins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deeper")
	want := filepath.Join(dir, "explicit"+".screen.stream")
	got, err := chatResolveSgrPath(chatSgrOptions{enabled: true, path: want}, nil)
	if err != nil {
		t.Fatalf("explicit path: %v", err)
	}
	if got != want {
		t.Fatalf("resolved %q, want the explicit %q", got, want)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the resolver did not create the directory it was handed: %v", err)
	}
	// OFF resolves to NO path at all — not to a scratch directory somebody would
	// then have to explain.
	if got, err := chatResolveSgrPath(chatSgrOptions{}, nil); got != "" || err != nil {
		t.Fatalf("the off state resolved to %q (err=%v), want no path", got, err)
	}
}

// TestChatSgrResumeRecordRoundTrip pins that a recorded word is restored and that
// an explicit one supersedes it — the dump flag's own two-value precedence, applied
// to the leg that also names a destination.
func TestChatSgrResumeRecordRoundTrip(t *testing.T) {
	recorded := chatSgrStreamFlag + chatFlagValueSep + "recorded.screen.stream"
	noEditor, noColor, verbose := false, false, false
	sess := &chatSession{}

	// Nothing stated here: the record fills the silence.
	opts := chatSgrOptions{}
	applied, superseded := chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose,
		&chatDumpOptions{}, &opts, []string{recorded}, false, false)
	if opts.path != "recorded.screen.stream" || !opts.enabled || len(applied) != 1 || len(superseded) != 0 {
		t.Fatalf("recorded word → %+v applied=%v superseded=%v, want it adopted", opts, applied, superseded)
	}

	// This command line stated its own: it wins and the record is NAMED.
	opts = chatSgrOptions{enabled: true, path: "explicit.screen.stream"}
	applied, superseded = chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose,
		&chatDumpOptions{}, &opts, []string{recorded}, false, true)
	if opts.path != "explicit.screen.stream" {
		t.Errorf("the recorded destination OVERRODE the command line's own: %+v", opts)
	}
	if len(applied) != 0 || len(superseded) != 1 {
		t.Errorf("applied=%v superseded=%v, want the recorded word named as superseded", applied, superseded)
	}

	// The OFF word is a recorded choice too, and it must be restored as one: a
	// resumed run that ignored the decline would re-arm an artifact the operator
	// turned off (the dump's own contract, applied to this leg).
	opts = chatSgrOptions{}
	applied, superseded = chatApplyResumeFlags(sess, &noEditor, &noColor, &verbose,
		&chatDumpOptions{}, &opts, []string{chatNoSgrStreamFlag}, false, false)
	if !opts.disabled || opts.recorded() || len(applied) != 1 || len(superseded) != 0 {
		t.Fatalf("recorded %s → %+v applied=%v superseded=%v, want the decline adopted", chatNoSgrStreamFlag, opts, applied, superseded)
	}

	// A recorded word must not be reported as a VERSION MISMATCH: that derivation
	// is chatResumeKnownWord's (the word this binary can place), which is a wider
	// table than chatResumeOptionWord (the options it RECORDS) — and a flag carrying
	// a value is a word with a value, not an unknown flag.
	if !chatResumeKnownWord(chatSgrStreamFlag) || !chatResumeKnownWord(chatSgrStreamFlag+"=/x") ||
		!chatResumeKnownWord(chatNoSgrStreamFlag) || !chatResumeKnownWord(chatNoSgrStreamFlag+"=x") {
		t.Errorf("the resume vocabulary does not recognize the raw-stream family in every spelling")
	}
}

// TestChatSgrStatedSuppressesTheAutodetect pins the ARGV reading: a raw-stream flag
// on this command line is a STATEMENT about this run's options, so it must stop the
// silent restore of a crashed run's recorded destination — while a value that only
// came from the environment is NOT a statement and must not.
func TestChatSgrStatedSuppressesTheAutodetect(t *testing.T) {
	t.Setenv(chatSgrStreamEnv, "/from/the/env.screen.stream")
	if got := chatSgrFromEnv(); !got.enabled || got.path != "/from/the/env.screen.stream" {
		t.Fatalf("fixture: env channel produced %+v", got)
	}
	// The environment alone is not a statement.
	if chatStatedOptions([]string{"chat"}) {
		t.Errorf("an env-only raw stream counted as a command-line statement")
	}
	// Both flag spellings are — and so is the OFF switch: it says which state this
	// run is in, so a recording must not overrule it.
	for _, word := range []string{chatSgrStreamFlag, chatSgrStreamFlag + "=/typed.screen.stream", chatNoSgrStreamFlag} {
		if !chatStatedOptions([]string{"chat", word}) {
			t.Errorf("%q did not count as a command-line statement about the raw stream", word)
		}
	}
}

// TestChatSgrEnvLegacyAliasIsRead pins the ALIAS rule from both sides: the
// pre-rename spelling still drives the decision, AND the current one wins when
// both are set. A rename that dropped the old name would silently un-arm every
// shell profile written before it — the operator would see no artifact and no
// reason (Rule 15), which is why the alias is read rather than retired.
func TestChatSgrEnvLegacyAliasIsRead(t *testing.T) {
	t.Run("legacy alone still switches the stream", func(t *testing.T) {
		t.Setenv(cliverbs.ScreenStreamEnv, "")
		t.Setenv(cliverbs.LegacyScreenStreamEnv, "/legacy/run.screen.stream")
		got := chatSgrFromEnv()
		if !got.recorded() || got.path != "/legacy/run.screen.stream" {
			t.Fatalf("legacy alias ignored: enabled=%v path=%q", got.enabled, got.path)
		}
		if !got.stated {
			t.Fatal("a legacy value must count as STATED: it is a choice, not the default")
		}
		name, v := chatSgrEnvSource()
		if name != cliverbs.LegacyScreenStreamEnv || v != "/legacy/run.screen.stream" {
			t.Fatalf("chatSgrEnvSource = %q,%q; want the legacy name and its value", name, v)
		}
	})
	t.Run("a legacy OFF is honoured", func(t *testing.T) {
		t.Setenv(cliverbs.ScreenStreamEnv, "")
		t.Setenv(cliverbs.LegacyScreenStreamEnv, "0")
		if got := chatSgrFromEnv(); got.recorded() || !got.disabled {
			t.Fatalf("legacy `0` did not decline the stream: enabled=%v disabled=%v", got.enabled, got.disabled)
		}
	})
	t.Run("the current name WINS over the legacy one", func(t *testing.T) {
		t.Setenv(cliverbs.LegacyScreenStreamEnv, "/legacy/old.screen.stream")
		t.Setenv(cliverbs.ScreenStreamEnv, "0")
		got := chatSgrFromEnv()
		if got.recorded() {
			t.Fatalf("the current name did not override the legacy one: path=%q", got.path)
		}
		name, _ := chatSgrEnvSource()
		if name != cliverbs.ScreenStreamEnv {
			t.Fatalf("chatSgrEnvSource = %q; want the CURRENT name to be the deciding one", name)
		}
	})
	t.Run("the alias notice names both spellings, and only for the legacy one", func(t *testing.T) {
		var buf bytes.Buffer
		t.Setenv(cliverbs.ScreenStreamEnv, "1")
		t.Setenv(cliverbs.LegacyScreenStreamEnv, "")
		chatSgrEnvAliasWarn(&buf)
		if buf.Len() != 0 {
			t.Fatalf("the current spelling produced a notice: %q", buf.String())
		}
		t.Setenv(cliverbs.ScreenStreamEnv, "")
		t.Setenv(cliverbs.LegacyScreenStreamEnv, "1")
		chatSgrEnvAliasWarn(&buf)
		out := buf.String()
		if !strings.Contains(out, cliverbs.LegacyScreenStreamEnv) || !strings.Contains(out, cliverbs.ScreenStreamEnv) {
			t.Fatalf("the alias notice %q does not name both spellings", out)
		}
	})
}

// TestChatSgrFlagDeprecatedSpellingsStillWork is the ALIAS half of the 2026-11-08
// rename, and it asserts BOTH things at once because either alone is a way to get
// the deprecation wrong:
//
//   - the pre-rename word PARSES to exactly the same request as the current one
//     (a rename that quietly stopped honouring shell history is a regression, not
//     a rename), and
//   - it PRINTS the notice, naming ITSELF and the word that replaced it
//     (a `const SgrStream = ScreenStream` alias would make the line read
//     "--screen-stream is deprecated: use --screen-stream", which is the one thing
//     an alias must never say — measured on the first build of this change).
//
// The nil-warn arm is the third assertion and it is the one that keeps the parser
// usable by a SILENT reader (a recorded resume option, chatResumeKnownWord): the
// same word parses identically and prints nothing, because the notice is about
// what the OPERATOR typed.
func TestChatSgrFlagDeprecatedSpellingsStillWork(t *testing.T) {
	for _, c := range []struct{ old, cur string }{
		{chatSgrStreamDeprecatedFlag, chatSgrStreamFlag},
		{chatNoSgrStreamDeprecatedFlag, chatNoSgrStreamFlag},
	} {
		t.Run(c.old, func(t *testing.T) {
			var buf strings.Builder
			gotAlias, matched, err := parseChatSgrArgWithNotice(c.old, "", false, &buf)
			if !matched || err != nil {
				t.Fatalf("%s matched=%v err=%v — the deprecated spelling stopped parsing", c.old, matched, err)
			}
			gotCur, matchedCur, errCur := parseChatSgrArg(c.cur, "", false)
			if !matchedCur || errCur != nil {
				t.Fatalf("%s matched=%v err=%v", c.cur, matchedCur, errCur)
			}
			if gotAlias != gotCur {
				t.Fatalf("%s resolved to %+v but %s resolves to %+v — the alias is a DIFFERENT behaviour",
					c.old, gotAlias, c.cur, gotCur)
			}
			notice := buf.String()
			if !strings.Contains(notice, "deprecated: use "+c.cur) {
				t.Fatalf("%s printed %q, want a line containing %q", c.old, notice, "deprecated: use "+c.cur)
			}
			if !strings.Contains(notice, c.old) {
				t.Fatalf("%s printed %q — the notice does not name the word the caller actually typed", c.old, notice)
			}
			// SILENT READER (a recorded resume option, chatResumeKnownWord):
			// identical request, and nothing written anywhere — a nil writer is the
			// contract, so a notice that fired unconditionally would panic there
			// instead of being quiet.
			gotSilent, _, errSilent := parseChatSgrArgWithNotice(c.old, "", false, nil)
			if errSilent != nil || gotSilent != gotAlias {
				t.Fatalf("the nil-warn path changed the parse: %+v vs %+v", gotSilent, gotAlias)
			}
		})
	}

	// The ARM flag's JOINED value travels through the alias untouched: the
	// translation must rewrite the WORD, never the path beside it.
	var buf strings.Builder
	got, matched, err := parseChatSgrArgWithNotice(chatSgrStreamDeprecatedFlag+"=/x/y.screen.stream", "", false, &buf)
	if !matched || err != nil || got.path != "/x/y.screen.stream" || !got.enabled {
		t.Fatalf("joined alias → %+v matched=%v err=%v", got, matched, err)
	}
	if !strings.Contains(buf.String(), "deprecated: use "+chatSgrStreamFlag) {
		t.Fatalf("the joined alias printed %q", buf.String())
	}

	// The OFF switch still REFUSES a value in its deprecated spelling, by name —
	// an alias that swallowed the value would teach the caller something false.
	if _, matched, err := parseChatSgrArgWithNotice(chatNoSgrStreamDeprecatedFlag+"=x", "", false, nil); !matched || err == nil {
		t.Fatalf("%s=x matched=%v err=%v, want a refusal by name", chatNoSgrStreamDeprecatedFlag, matched, err)
	}
}

// TestChatSgrReplayDeprecatedSpellingStillWorks is the replay flag's arm of the
// same contract: same target, and a notice naming both words.
func TestChatSgrReplayDeprecatedSpellingStillWorks(t *testing.T) {
	var buf strings.Builder
	got, consumed, err := parseChatSgrReplayArgWithNotice(chatSgrReplayDeprecatedFlag, "sid-1", true, &buf)
	if !consumed || err != nil || got.target != "sid-1" || !got.set {
		t.Fatalf("spaced alias → %+v consumed=%v err=%v", got, consumed, err)
	}
	if !strings.Contains(buf.String(), "deprecated: use "+chatSgrReplayFlag) {
		t.Fatalf("the spaced alias printed %q", buf.String())
	}

	var joined strings.Builder
	gotJ, consumedJ, errJ := parseChatSgrReplayArgWithNotice(chatSgrReplayDeprecatedFlag+"=abc", "", false, &joined)
	if !consumedJ || errJ != nil || gotJ.target != "abc" {
		t.Fatalf("joined alias → %+v consumed=%v err=%v", gotJ, consumedJ, errJ)
	}
	if !strings.Contains(joined.String(), "deprecated: use "+chatSgrReplayFlag) {
		t.Fatalf("the joined alias printed %q", joined.String())
	}

	// A missing operand is still refused, in the deprecated spelling, and the
	// refusal is the CURRENT flag's sentence (one definition, Rule 13).
	if _, consumed, err := parseChatSgrReplayArgWithNotice(chatSgrReplayDeprecatedFlag, "", false, nil); !consumed || err == nil {
		t.Fatalf("a bare alias consumed=%v err=%v, want the missing-target refusal", consumed, err)
	}
}

// TestChatDeprecatedFlagsAreListedAndDocumented: the three deprecated words are in
// this verb's closed vocabulary (so `gopi chat --sgr-stream=x -h` answers the page
// instead of "unknown flag") and on the page — a flag the parser accepts and the
// help never mentions is a flag nobody can find, and the deprecation is exactly
// the case where a caller needs to be told which word to type next.
func TestChatDeprecatedFlagsAreListedAndDocumented(t *testing.T) {
	for _, word := range []string{
		chatSgrStreamDeprecatedFlag,
		chatNoSgrStreamDeprecatedFlag,
		chatSgrReplayDeprecatedFlag,
	} {
		listed := false
		for _, f := range chatREPLFlags() {
			if f == word {
				listed = true
				break
			}
		}
		if !listed {
			t.Errorf("%s parses but is absent from chatREPLFlags — the closed vocabulary would refuse a command line this binary obeys", word)
		}
		if !strings.Contains(chatUsage, word) {
			t.Errorf("%s is not documented in chatUsage — the help and the vocabulary disagree", word)
		}
	}
}
