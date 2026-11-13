package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chat_surface_turns_test.go — the ONE resolution behind the chat_surface tool's
// self-driven turn budget, and the three channels that feed it.
//
// The point of these cases is the ORDER, not the parsing: a knob whose channels
// can be reordered silently is a knob whose refusal names the wrong remedy. Every
// case drives the REAL resolver (config.ChatSurfaceTurns) — never a copy of its
// precedence — so a change to the order fails here (Rule 31: the surface is
// executed, not described).

// writeTurnsSettings points the resolver's settings channel at a scratch profile
// carrying this knob's key, and returns nothing: the pinning is the whole job
// (Rule 32: a test pins the environment, it never writes the operator's own
// settings file).
func writeTurnsSettings(t *testing.T, value string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := "{\"" + ChatSurfaceTurnsSettingsKey + "\": \"" + value + "\"}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(SettingsPathEnv, path)
}

// clearTurnsEnv removes every channel below the settings key, so a case can be
// sure which one decided.
func clearTurnsEnv(t *testing.T) {
	t.Helper()
	t.Setenv(ChatSurfaceTurnsEnv, "")
}

// TestChatSurfaceTurnsDefaultIsTwenty pins the DEFAULT the epic raised: the bound
// an operator who declares nothing gets. It is asserted through the resolver (not
// just the constant) because the number a run OBSERVES is what the tool
// description, the console notice and the refusal all print.
func TestChatSurfaceTurnsDefaultIsTwenty(t *testing.T) {
	clearTurnsEnv(t)
	t.Setenv(SettingsPathEnv, filepath.Join(t.TempDir(), "absent.json"))

	if ChatSurfaceTurnsDefault != 20 {
		t.Fatalf("ChatSurfaceTurnsDefault = %d, want 20", ChatSurfaceTurnsDefault)
	}
	got := ChatSurfaceTurns()
	if got.Turns != 20 {
		t.Fatalf("ChatSurfaceTurns() = %d, want 20 (the built-in default)", got.Turns)
	}
	if got.Source != ChatSurfaceTurnsSourceDefault {
		t.Fatalf("source = %q, want %q", got.Source, ChatSurfaceTurnsSourceDefault)
	}
}

// TestChatSurfaceTurnsSettingsChannel pins the lowest declared channel: the
// settings key decides when nobody exported anything.
func TestChatSurfaceTurnsSettingsChannel(t *testing.T) {
	clearTurnsEnv(t)
	writeTurnsSettings(t, "35")

	got := ChatSurfaceTurns()
	if got.Turns != 35 || got.Source != ChatSurfaceTurnsSettingsKey {
		t.Fatalf("ChatSurfaceTurns() = %d from %q, want 35 from %q", got.Turns, got.Source, ChatSurfaceTurnsSettingsKey)
	}
}

// TestChatSurfaceTurnsEnvBeatsSettings pins the env channel above the settings
// key — an operator's export outranks the file.
func TestChatSurfaceTurnsEnvBeatsSettings(t *testing.T) {
	writeTurnsSettings(t, "35")
	t.Setenv(ChatSurfaceTurnsEnv, "50")

	got := ChatSurfaceTurns()
	// The resolved state is LOGGED on the passing path as well: it is the one line a
	// report can quote as "the resolution ran, here is what it decided" (Rule 31 —
	// evidence, not a claim).
	t.Logf("resolved turns=%d source=%q (settings said 35, the env said 50)", got.Turns, got.Source)
	if got.Turns != 50 || got.Source != ChatSurfaceTurnsEnv {
		t.Fatalf("ChatSurfaceTurns() = %d from %q, want 50 from %q", got.Turns, got.Source, ChatSurfaceTurnsEnv)
	}
}

// TestChatSurfaceTurnsFlagBeatsEnv: the flag channel outranks an operator's
// exported variable, because the publisher writes the variable UNCONDITIONALLY —
// which is exactly how a flag wins. The publisher is the real one, so this covers
// the pre-verb hoist and the chat verb's own walk.
func TestChatSurfaceTurnsFlagBeatsEnv(t *testing.T) {
	writeTurnsSettings(t, "35")
	t.Setenv(ChatSurfaceTurnsEnv, "50")
	PublishChatSurfaceTurnsFlag("40")

	got := ChatSurfaceTurns()
	if got.Turns != 40 || got.Source != ChatSurfaceTurnsEnv {
		t.Fatalf("ChatSurfaceTurns() = %d from %q, want 40 (the flag's value, published through %s)", got.Turns, got.Source, ChatSurfaceTurnsEnv)
	}
}

// TestChatSurfaceTurnsUnusableValueFallsThroughAndIsReported is the Rule 15 half:
// a declaration nobody can parse is REPORTED (never silent) and the NEXT channel
// decides (never fatal, never "nobody asked").
func TestChatSurfaceTurnsUnusableValueFallsThroughAndIsReported(t *testing.T) {
	// An EMPTY value is not in this list on purpose: "unset" and "set to the
	// empty string" are the same declaration, and the env channel treats an empty
	// export as nobody having asked (the same shape setStr applies on the fill
	// side) — so there is nothing to refuse and nothing to report.
	for _, bad := range []string{"many", "0", "-3", "1", "two", "3.5", "1001"} {
		t.Run(bad, func(t *testing.T) {
			writeTurnsSettings(t, "35")
			t.Setenv(ChatSurfaceTurnsEnv, bad)

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			prev := os.Stderr
			os.Stderr = w
			got := ChatSurfaceTurns()
			os.Stderr = prev
			w.Close() //stylegate:ignore closing this pipe's OWN write end is what ends the ReadFrom below; the report read back is the assertion
			var buf bytes.Buffer
			if _, rerr := buf.ReadFrom(r); rerr != nil {
				t.Fatalf("read stderr: %v", rerr)
			}
			reported := buf

			if got.Turns != 35 || got.Source != ChatSurfaceTurnsSettingsKey {
				t.Fatalf("turns = %d from %q, want the settings channel's 35 after %q was refused",
					got.Turns, got.Source, bad)
			}
			if !strings.Contains(reported.String(), ChatSurfaceTurnsEnv) {
				t.Fatalf("the refusal did not name the channel that failed: stderr = %q", reported.String())
			}
		})
	}
}

// TestParseChatSurfaceTurnsRange pins the ONE validator's bounds: below the floor
// and above the ceiling are refused, and both are NAMED (a refusal that does not
// say the range sends the operator guessing).
func TestParseChatSurfaceTurnsRange(t *testing.T) {
	ok := []struct {
		raw  string
		want int
	}{
		{"2", 2}, {"20", 20}, {"1000", 1000}, {" 30 ", 30},
	}
	for _, c := range ok {
		if n, good := ParseChatSurfaceTurns(c.raw); !good || n != c.want {
			t.Fatalf("ParseChatSurfaceTurns(%q) = %d,%v want %d,true", c.raw, n, good, c.want)
		}
	}
	bad := []string{"1", "0", "-5", "1001", "", "x", "20t"}
	for _, raw := range bad {
		if n, good := ParseChatSurfaceTurns(raw); good {
			t.Fatalf("ParseChatSurfaceTurns(%q) accepted %d — the range is %d..%d",
				raw, n, ChatSurfaceTurnsFloor, ChatSurfaceTurnsCeiling)
		}
	}
	// The ceiling must be a bound an operator can actually USE, and the floor must
	// admit at least the first self-driven turn of a period.
	if ChatSurfaceTurnsFloor > ChatSurfaceTurnsDefault || ChatSurfaceTurnsDefault > ChatSurfaceTurnsCeiling {
		t.Fatalf("default %d is not inside the accepted range %d..%d",
			ChatSurfaceTurnsDefault, ChatSurfaceTurnsFloor, ChatSurfaceTurnsCeiling)
	}
}

// TestChatSurfaceTurnsRowSummaryNamesTheLiveNumbers pins the help row against the
// constants: the sentence an operator reads must carry the number the code
// enforces, and it is RENDERED from them so raising the default moves the text.
func TestChatSurfaceTurnsRowSummaryNamesTheLiveNumbers(t *testing.T) {
	for _, want := range []string{"20", ChatSurfaceTurnsEnv, ChatSurfaceTurnsSettingsKey} {
		if !strings.Contains(ChatSurfaceTurnsRowSummary, want) {
			t.Fatalf("the help row does not name %q: %q", want, ChatSurfaceTurnsRowSummary)
		}
	}
}

// TestBootstrapEnvFromArgs_HoistsTheTurnsFlag is the PRE-VERB spelling
// (`gopi --chat-surface-turns 40 chat`), and it is the one that actually has to
// work: `gopi chat` returns from main() at the subcommand chain and never reaches
// the agent-path config block, so a flag the hoist does not publish is a flag the
// verb it exists for silently ignores.
func TestBootstrapEnvFromArgs_HoistsTheTurnsFlag(t *testing.T) {
	clearTurnsEnv(t)
	t.Setenv(SettingsPathEnv, filepath.Join(t.TempDir(), "absent.json"))

	BootstrapEnvFromArgs([]string{"--" + ChatSurfaceTurnsFlagName, "40", "chat"})
	t.Logf("after the hoist %s=%q", ChatSurfaceTurnsEnv, os.Getenv(ChatSurfaceTurnsEnv))
	if got := os.Getenv(ChatSurfaceTurnsEnv); got != "40" {
		t.Fatalf("%s = %q after the pre-verb flag, want 40", ChatSurfaceTurnsEnv, got)
	}
	if state := ChatSurfaceTurns(); state.Turns != 40 || state.Source != ChatSurfaceTurnsEnv {
		t.Fatalf("the hoisted flag did not reach the resolution: %+v", state)
	}

	// The inline form must hoist too — the two spellings are one flag, and a
	// closed vocabulary that accepts only one of them is a help page that lies.
	t.Setenv(ChatSurfaceTurnsEnv, "")
	BootstrapEnvFromArgs([]string{"--" + ChatSurfaceTurnsFlagName + "=41", "chat"})
	if got := os.Getenv(ChatSurfaceTurnsEnv); got != "41" {
		t.Fatalf("%s = %q after the inline pre-verb flag, want 41", ChatSurfaceTurnsEnv, got)
	}
}
