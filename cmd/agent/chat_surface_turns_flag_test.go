package main

// chat_surface_turns_flag_test.go — the `gopi chat` spelling of the self-driven
// turn budget knob, driven through the REAL parser and the REAL publisher.
//
// What this covers that the config-package tests cannot: the flag has to survive
// the chat verb's OWN argv walk and reach the surface through the ONE env
// representation. A knob whose flag parses into a local field the resolver never
// reads is the defect this file exists to catch — the run would look configured
// and enforce the default.
//
// The three shapes are the ones the geometry flag established: bare+spaced
// (`--chat-surface-turns 30`), inline (`--chat-surface-turns=30`), and the
// usage errors (a bare flag at the end of argv, an empty inline value) which are
// REFUSED rather than silently leaving the run at the default (Rule 15).

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// pinTurnsSurfaceEnv clears every channel of the knob and points the settings
// channel at a scratch profile, so a case observes exactly one declaration.
func pinTurnsSurfaceEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.ChatSurfaceTurnsEnv, "")
	t.Setenv(config.SettingsPathEnv, filepath.Join(t.TempDir(), "settings.json"))
}

// TestChatParseSurfaceTurnsArgPinsAllThreeShapes drives the parser directly: the
// spaced form consumes the next slot, the inline form does not, and anything that
// is not this flag is left alone (so the walk can keep going).
func TestChatParseSurfaceTurnsArgPinsAllThreeShapes(t *testing.T) {
	cases := []struct {
		name            string
		argv0, argv1    string
		hasNext         bool
		wantValue       string
		wantConsumed    bool
		wantErrSubstr   string
		wantValueAbsent bool
	}{
		{name: "spaced", argv0: chatSurfaceTurnsFlag, argv1: "30", hasNext: true, wantValue: "30", wantConsumed: true},
		{name: "inline", argv0: chatSurfaceTurnsFlag + chatFlagValueSep + "30", hasNext: false, wantValue: "30", wantConsumed: true},
		{name: "not-this-flag", argv0: chatGeometryFlag, argv1: "117x26", hasNext: true, wantConsumed: false},
		{name: "bare at end of argv", argv0: chatSurfaceTurnsFlag, hasNext: false, wantConsumed: true, wantErrSubstr: "needs a value"},
		{name: "bare followed by another flag", argv0: chatSurfaceTurnsFlag, argv1: chatGeometryFlag, hasNext: true, wantConsumed: true, wantErrSubstr: "needs a value"},
		{name: "empty inline", argv0: chatSurfaceTurnsFlag + chatFlagValueSep, hasNext: false, wantConsumed: true, wantErrSubstr: "empty value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, consumed, err := chatParseSurfaceTurnsArg(c.argv0, c.argv1, c.hasNext)
			if consumed != c.wantConsumed {
				t.Fatalf("consumed = %v, want %v (%q %q)", consumed, c.wantConsumed, c.argv0, c.argv1)
			}
			if c.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrSubstr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.wantValue {
				t.Fatalf("value = %q, want %q", got, c.wantValue)
			}
		})
	}
}

// TestPublishChatSurfaceTurnsFlagReachesTheResolver is the WHOLE point of the
// flag: what the parser produced must be what the resolution reads. The value is
// published through the real writer and then observed through the real resolver
// the agent package calls.
func TestPublishChatSurfaceTurnsFlagReachesTheResolver(t *testing.T) {
	pinTurnsSurfaceEnv(t)
	config.PublishChatSurfaceTurnsFlag("37")

	state := config.ChatSurfaceTurns()
	if state.Turns != 37 {
		t.Fatalf("the resolver sees %d after the flag published 37 — the flag reaches no channel", state.Turns)
	}
	if state.Source != config.ChatSurfaceTurnsEnv {
		t.Fatalf("source = %q, want %q (the flag's ONE representation)", state.Source, config.ChatSurfaceTurnsEnv)
	}
}

// TestChatSurfaceTurnsFlagBeatsAnExportedVariable pins the precedence the usage
// page promises: the flag outranks an operator's export because the publisher
// writes the variable unconditionally, and the hoist runs after the environment
// was settled.
func TestChatSurfaceTurnsFlagBeatsAnExportedVariable(t *testing.T) {
	pinTurnsSurfaceEnv(t)
	t.Setenv(config.ChatSurfaceTurnsEnv, "9")
	config.PublishChatSurfaceTurnsFlag("44")

	if got := config.ChatSurfaceTurns(); got.Turns != 44 {
		t.Fatalf("resolved %d with an export of 9 and a flag of 44 — the flag must win", got.Turns)
	}
}

// TestChatSurfaceTurnsUsageNamesTheKnob pins the help section: the page an
// operator reads must carry the flag, the env name, the settings key and the live
// default, all rendered from the constants (so raising the default moves the page
// with it).
func TestChatSurfaceTurnsUsageNamesTheKnob(t *testing.T) {
	for _, want := range []string{
		"--" + config.ChatSurfaceTurnsFlagName,
		config.ChatSurfaceTurnsEnv,
		config.ChatSurfaceTurnsSettingsKey,
		"default 20",
	} {
		if !strings.Contains(chatSurfaceTurnsUsage, want) {
			t.Fatalf("the usage section does not name %q:\n%s", want, chatSurfaceTurnsUsage)
		}
	}
	if !strings.Contains(chatUsage, config.ChatSurfaceTurnsFlagName) {
		t.Fatalf("the `gopi chat` usage line does not advertise %q", config.ChatSurfaceTurnsFlagName)
	}
}

// TestChatSurfaceTurnsEnvIsReadByRealCode is Rule 21's arm for this knob: the env
// name must have a REAL reader, so an operator who exports it is not describing a
// hook nothing consumes. It asserts the reader by driving it, not by grepping.
func TestChatSurfaceTurnsEnvIsReadByRealCode(t *testing.T) {
	pinTurnsSurfaceEnv(t)
	// t.Setenv, not os.Setenv: the flip is for THIS test only, and a leak into a
	// sibling case would make the default-bound assertions pass or fail by order
	// (Rule 17).
	t.Setenv(config.ChatSurfaceTurnsEnv, "23")
	if got := config.ChatSurfaceTurnCount(); got != 23 {
		t.Fatalf("exporting %s=23 resolved %d — the name has no reader", config.ChatSurfaceTurnsEnv, got)
	}
}

// TestChatSurfaceBoundNoticeCarriesTheConfiguredNumber is the OPERATOR-VISIBLE
// half of the acceptance, driven through the knobs a run actually uses.
//
// It builds its claim from the SAME path the REPL uses — the configured bound the
// agent reports, formatted through the SAME notice constant the console prints —
// so the line quoted in a report is the line the console draws, not a paraphrase
// of it. A raised default whose notices still read "of 8" would be a knob that
// moved nothing an operator can see.
func TestChatSurfaceBoundNoticeCarriesTheConfiguredNumber(t *testing.T) {
	pinTurnsSurfaceEnv(t)
	t.Setenv(config.ChatSurfaceTurnsEnv, "5")

	bound := config.ChatSurfaceTurnCount()
	if bound != 5 {
		t.Fatalf("the configured bound resolved to %d, want 5", bound)
	}
	turn := fmt.Sprintf(chatSelfDrivenTurnNotice, 1, bound)
	t.Logf("console line at the configured bound: %s", turn)
	if want := "self-driven turn 1/5 of this period"; !strings.Contains(turn, want) {
		t.Fatalf("the console line does not name the configured bound: %q (want %q)", turn, want)
	}
	closing := fmt.Sprintf(chatSelfDrivenClosingNotice, bound)
	t.Logf("closing line at the configured bound: %s", closing)
	if want := "(bound 5)"; !strings.Contains(closing, want) {
		t.Fatalf("the closing notice does not name the configured bound: %q (want %q)", closing, want)
	}
	renewal := fmt.Sprintf(chatSelfDrivenRenewalNotice, bound)
	t.Logf("renewal line at the configured bound: %s", renewal)
	if want := "budget (5 turns)"; !strings.Contains(renewal, want) {
		t.Fatalf("the renewal notice does not name the configured bound: %q (want %q)", renewal, want)
	}
}
