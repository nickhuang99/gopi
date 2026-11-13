package config

// chat_geometry_test.go — the ideal-geometry knob's PRIORITY and its
// degradation contract.
//
// What is under test:
//
//	1. the three channels + the built-in default resolve in exactly one order
//	   (flag > env > settings > default) and the SOURCE LABEL names the winner;
//	2. a channel that carries an unusable value is REPORTED and the next channel
//	   decides (Rule 15) — it is never read as "unset" and never fatal;
//	3. the ONE validator accepts/rejects the same shapes on every channel.
//
// The flag channel is exercised through its real publisher
// (PublishChatIdealGeometryFlag), which is the same function
// BootstrapEnvFromArgs and the chat verb's argv walk both call — so the test
// covers the mechanism, not a stand-in for it.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeGeometrySettings writes a settings.json carrying the ideal-geometry value
// and pins GOPI_SETTINGS at it, so no test reads the operator's own profile.
func writeGeometrySettings(t *testing.T, value string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body, err := json.Marshal(map[string]any{ChatIdealGeometrySettingsKey: value})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsEnv, path)
}

// clearGeometryEnv makes the env channel absent for a test that is asserting a
// LOWER channel. t.Setenv seeds it empty; the resolver treats empty as absent
// (the same contract an unset variable has).
func clearGeometryEnv(t *testing.T) {
	t.Helper()
	t.Setenv(ChatIdealGeometryEnv, "")
}

// TestChatIdealGeometryDefaultIsTheBuiltInPair: nobody declares anything → the
// built-in default decides, and it says so.
func TestChatIdealGeometryDefaultIsTheBuiltInPair(t *testing.T) {
	clearGeometryEnv(t)
	t.Setenv(SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	got := ChatIdealGeometry()
	if got.Cols != 117 || got.Rows != 26 {
		t.Fatalf("default geometry = %dx%d, want 117x26", got.Cols, got.Rows)
	}
	if got.Source != ChatIdealGeometrySourceDefault {
		t.Fatalf("source = %q, want %q", got.Source, ChatIdealGeometrySourceDefault)
	}
}

// TestChatIdealGeometrySettingsChannel: the settings key decides when the higher
// channels are silent, and the source names the KEY.
func TestChatIdealGeometrySettingsChannel(t *testing.T) {
	clearGeometryEnv(t)
	writeGeometrySettings(t, "100x30")
	got := ChatIdealGeometry()
	if got.Cols != 100 || got.Rows != 30 {
		t.Fatalf("geometry = %dx%d, want 100x30", got.Cols, got.Rows)
	}
	if got.Source != ChatIdealGeometrySettingsKey {
		t.Fatalf("source = %q, want %q", got.Source, ChatIdealGeometrySettingsKey)
	}
}

// TestChatIdealGeometryEnvBeatsSettings: the env channel outranks the settings
// key — the ordinary precedence, asserted rather than assumed.
func TestChatIdealGeometryEnvBeatsSettings(t *testing.T) {
	writeGeometrySettings(t, "100x30")
	t.Setenv(ChatIdealGeometryEnv, "133x40")
	got := ChatIdealGeometry()
	if got.Cols != 133 || got.Rows != 40 {
		t.Fatalf("geometry = %dx%d, want 133x40 (env must beat settings)", got.Cols, got.Rows)
	}
	if got.Source != ChatIdealGeometryEnv {
		t.Fatalf("source = %q, want %q", got.Source, ChatIdealGeometryEnv)
	}
}

// TestChatIdealGeometryFlagBeatsEnv: the flag channel outranks an operator's
// exported variable, because the publisher writes the variable UNCONDITIONALLY
// — which is exactly how the flag wins. The publisher is the real one, so this
// covers the hoist and the chat verb's walk.
func TestChatIdealGeometryFlagBeatsEnv(t *testing.T) {
	writeGeometrySettings(t, "100x30")
	t.Setenv(ChatIdealGeometryEnv, "133x40")
	PublishChatIdealGeometryFlag("90x24")
	got := ChatIdealGeometry()
	if got.Cols != 90 || got.Rows != 24 {
		t.Fatalf("geometry = %dx%d, want 90x24 (flag must beat env)", got.Cols, got.Rows)
	}
}

// TestChatIdealGeometryUnusableValueFallsThroughAndIsReported is the Rule 15
// half: a declaration nobody can parse is REPORTED (never silent) and the NEXT
// channel decides (never fatal, never "nobody asked").
func TestChatIdealGeometryUnusableValueFallsThroughAndIsReported(t *testing.T) {
	for _, bad := range []string{"wide", "117", "117x", "x26", "0x26", "117x0", "-1x26", "117x-1", "117x26x1", "a x b"} {
		t.Run(bad, func(t *testing.T) {
			clearGeometryEnv(t)
			writeGeometrySettings(t, "100x30")
			t.Setenv(ChatIdealGeometryEnv, bad)

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			prev := os.Stderr
			os.Stderr = w
			got := ChatIdealGeometry()
			os.Stderr = prev
			w.Close() //stylegate:ignore closing this pipe's OWN write end is what ends the ReadFrom below; the report read back is the assertion
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(r); err != nil {
				t.Fatal(err)
			}
			report := buf.String()

			if report == "" {
				t.Fatalf("an unusable %s=%q produced NO report — the operator is never told (Rule 15)", ChatIdealGeometryEnv, bad)
			}
			if got.Cols != 100 || got.Rows != 30 {
				t.Fatalf("after an unusable env value the geometry = %dx%d, want the SETTINGS value 100x30",
					got.Cols, got.Rows)
			}
		})
	}
}

// TestChatIdealGeometryUnusableSettingsFallsToDefault: the settings channel is
// the last declaration. An unusable value there is reported and the built-in
// default decides — and the source says so, rather than claiming the settings
// key decided.
func TestChatIdealGeometryUnusableSettingsFallsToDefault(t *testing.T) {
	clearGeometryEnv(t)
	writeGeometrySettings(t, "huge")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	got := ChatIdealGeometry()
	os.Stderr = prev
	w.Close() //stylegate:ignore closing this pipe's OWN write end is what ends the ReadFrom below; the report read back is the assertion
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatalf("an unusable settings value produced NO report")
	}
	if got.Cols != 117 || got.Rows != 26 || got.Source != ChatIdealGeometrySourceDefault {
		t.Fatalf("geometry = %dx%d from %q, want 117x26 from %q",
			got.Cols, got.Rows, got.Source, ChatIdealGeometrySourceDefault)
	}
}

// TestParseChatIdealGeometryIsTheOneValidator pins the accepted and refused
// shapes directly, so a second spelling of "what a geometry is" cannot appear
// elsewhere without this failing.
func TestParseChatIdealGeometryIsTheOneValidator(t *testing.T) {
	ok := map[string][2]int{
		"117x26":   {117, 26},
		" 80x24":   {80, 24},
		"80 x24 ":  {80, 24},
		"1x1":      {1, 1},
		"1000x500": {1000, 500},
	}
	for raw, want := range ok {
		cols, rows, valid := parseChatIdealGeometry(raw)
		if !valid || cols != want[0] || rows != want[1] {
			t.Errorf("parseChatIdealGeometry(%q) = (%d,%d,%v), want (%d,%d,true)",
				raw, cols, rows, valid, want[0], want[1])
		}
	}
	for _, raw := range []string{"", "117", "117x", "x26", "0x26", "26x0", "1001x26", "117x501", "117x26x1", "wide x tall", "117X26"} {
		if _, _, valid := parseChatIdealGeometry(raw); valid {
			t.Errorf("parseChatIdealGeometry(%q) accepted a value no channel can express", raw)
		}
	}
}

// TestChatIdealGeometryDimensionsComeFromOneConstant: the built-in default, the
// row summary and the effect claim are built from ONE pair, so a doc that says
// 117x26 and a code path that draws at something else cannot both be true. The
// assertion is deliberately about the DERIVATION (the parsed default equals the
// constants), not about the literal text.
func TestChatIdealGeometryDimensionsComeFromOneConstant(t *testing.T) {
	clearGeometryEnv(t)
	t.Setenv(SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	def := ChatIdealGeometry()
	cols, rows, ok := parseChatIdealGeometry(chatIdealGeometryDefault)
	if !ok {
		t.Fatalf("the built-in default %q does not parse — the constant is the ONE place the size is written down", chatIdealGeometryDefault)
	}
	if def.Cols != cols || def.Rows != rows {
		t.Fatalf("resolved default = %dx%d, parsed constant = %dx%d — the two spellings have drifted",
			def.Cols, def.Rows, cols, rows)
	}
	if ChatIdealCols() != def.Cols || ChatIdealRows() != def.Rows {
		t.Fatalf("ChatIdealCols/Rows = %d/%d, want %d/%d (the accessors must be the same resolution)",
			ChatIdealCols(), ChatIdealRows(), def.Cols, def.Rows)
	}
}

// TestChatIdealGeometryEnvNameIsPublishedOnce guards the ONE-representation
// rule: the flag publisher writes ChatIdealGeometryEnv, so there is no second
// variable a caller could set and the resolver would not read.
func TestChatIdealGeometryEnvNameIsPublishedOnce(t *testing.T) {
	t.Setenv(ChatIdealGeometryEnv, "")
	PublishChatIdealGeometryFlag("101x27")
	if got := os.Getenv(ChatIdealGeometryEnv); got != "101x27" {
		t.Fatalf("the publisher wrote %q, want the knob's ONE env representation %s=101x27",
			got, ChatIdealGeometryEnv)
	}
	// The publisher must OVERWRITE, not defer: a flag outranks an export.
	t.Setenv(ChatIdealGeometryEnv, "133x40")
	PublishChatIdealGeometryFlag("88x22")
	if got := os.Getenv(ChatIdealGeometryEnv); got != "88x22" {
		t.Fatalf("a published flag left %q in place — a flag must outrank an export", got)
	}
}
