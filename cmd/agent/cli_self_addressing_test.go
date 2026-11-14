package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/shot"
)

// typableForTest is the typability pre-flight the voice path runs, reached
// through the product's own exported check rather than reimplemented here.
func typableForTest(text string) error { return shot.TypableText(text) }

// cli_self_addressing_test.go — the CLI half of `--self`: which verbs accept it,
// which refuse it, and that the two paths that DO accept it (act, voice_input)
// never route to the display's active window.

// TestParseActArgsSelfAddressing pins the flag's shape on `gopi tool act`: accepted
// on the two verbs that inject keys, refused — LOUDLY, naming the verbs that do
// take it — everywhere else, because a flag accepted where it means nothing is
// the silent no-op Rule 15 refuses.
func TestParseActArgsSelfAddressing(t *testing.T) {
	ok := [][]string{
		{"type", "--text", "hi", "--" + actFlagSelf},
		{"key", "--key", "return", "--" + actFlagSelf},
		{"type", "--text", "hi", "--" + actFlagSelf, "--" + actFlagDryRun},
	}
	for _, argv := range ok {
		req, err := parseActArgs(argv)
		if err != nil {
			t.Errorf("parseActArgs(%v): %v", argv, err)
			continue
		}
		if !req.self {
			t.Errorf("parseActArgs(%v) did not record --%s", argv, actFlagSelf)
		}
	}

	refused := [][]string{
		{"info", "--" + actFlagSelf},
		{"move", "10", "10", "--" + actFlagSelf},
		{"click", "1", "--" + actFlagSelf},
	}
	for _, argv := range refused {
		_, err := parseActArgs(argv)
		if err == nil {
			t.Errorf("parseActArgs(%v) accepted --%s on a verb that has no key target", argv, actFlagSelf)
			continue
		}
		if !strings.Contains(err.Error(), actActionType) || !strings.Contains(err.Error(), actActionKey) {
			t.Errorf("the refusal for %v does not name the verbs that DO take --%s: %v", argv, actFlagSelf, err)
		}
	}
}

// TestActUsageCarriesTheSelfSpellingAndARunnableExample: help is part of the
// surface (an operator learns the addressing rule from it), and it must carry a
// command line that actually runs — the escaping rule for `act type --text` is
// not discoverable otherwise.
func TestActUsageCarriesTheSelfSpellingAndARunnableExample(t *testing.T) {
	for _, want := range []string{
		"--" + actFlagSelf,
		"gopi tool act type --text 'ls -l\\n' --" + actFlagSelf,
		"gopi tool act key --key return --" + actFlagSelf,
		"_NET_WM_PID",
		"PointerRoot",
	} {
		if !strings.Contains(actUsage, want) {
			t.Errorf("actUsage does not carry %q — the addressing rule must be discoverable from -h", want)
		}
	}
}

// TestParseVoiceArgsSelfAddressing pins the voice flag's shape: accepted where a
// transcript exists and a window can receive it, refused — by name, with what to
// do instead — on the shapes that cannot mean it (a pipe has no window; an ssh
// hop's two ends do not share one; listing devices produces no transcript).
func TestParseVoiceArgsSelfAddressing(t *testing.T) {
	ok := [][]string{
		{"--" + voiceFlagSelf, "clip.wav"},
		{"--" + voiceFlagClient, "--" + voiceFlagSelf},
		{"--" + voiceFlagSelf, "--" + voiceFlagDisplay, ":57", "clip.wav"},
	}
	for _, argv := range ok {
		opts, err := parseVoiceArgs(argv)
		if err != nil {
			t.Errorf("parseVoiceArgs(%v): %v", argv, err)
			continue
		}
		if !opts.self {
			t.Errorf("parseVoiceArgs(%v) did not record --%s", argv, voiceFlagSelf)
		}
	}
	opts, err := parseVoiceArgs([]string{"--" + voiceFlagSelf, "--" + voiceFlagDisplay, ":57", "clip.wav"})
	if err != nil {
		t.Fatalf("parseVoiceArgs: %v", err)
	}
	if opts.display != ":57" {
		t.Errorf("--%s parsed to %q, want \":57\"", voiceFlagDisplay, opts.display)
	}

	refused := [][]string{
		{"--" + voiceFlagServer, "--" + voiceFlagSelf},
		{"--" + voiceFlagStdio, "--" + voiceFlagSelf, "clip.wav"},
		{"--" + voiceFlagListDevices, "--" + voiceFlagSelf},
	}
	for _, argv := range refused {
		if _, err := parseVoiceArgs(argv); err == nil {
			t.Errorf("parseVoiceArgs(%v) accepted --%s on a shape with no window to type into", argv, voiceFlagSelf)
		}
	}
}

// TestVoiceUsageCarriesTheSelfRecipe: the voice help must carry the flag, a
// runnable example AND the send-it-later step, because the review-before-send
// property is the reason the transcript is not submitted automatically.
func TestVoiceUsageCarriesTheSelfRecipe(t *testing.T) {
	usage := voiceUsageText()
	for _, want := range []string{
		"--" + voiceFlagSelf,
		"gopi tool voice_input --client --" + voiceFlagSelf,
		CliVerbAct + " key --" + actFlagKey + " return --" + actFlagSelf,
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("the voice_input help does not carry %q", want)
		}
	}
}

// TestSelfPathsNeverSelectTheActiveWindow is the CLI-side half of the
// no-fallback gate (the wire-level half is internal/shot's
// TestSelfAddressingNeverAsksForTheActiveWindow). The two files that implement
// `--self` must not name the active-window question at all: reading the source
// catches a fallback that a test's particular fixture never reached, which is
// exactly how a "just in case" retry gets added.
func TestSelfPathsNeverSelectTheActiveWindow(t *testing.T) {
	files := []string{
		filepath.Join(".", "cli_act.go"),
		filepath.Join(".", "cli_voice_client.go"),
	}
	// The forbidden spellings: the EWMH property, this package's name for it, and
	// the internal/shot symbol that asks it. Quoted from their definitions rather
	// than typed anew (Rule 1b) where they are reachable from here; the property
	// string is written once here because it is the wire name a fallback would
	// have to spell.
	forbidden := []string{"_NET_ACTIVE_WINDOW", "netActiveWindowName", "activeWindow("}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(raw)
		for _, bad := range forbidden {
			if strings.Contains(src, bad) {
				t.Errorf("%s mentions %q — the --self addressing path must never fall back to the ACTIVE window (the defect it exists to remove)", f, bad)
			}
		}
	}
}

// TestSelfAddressSymbolIsTheOneSpelling: the addressing flag is one value across
// three verbs (capture, act, voice_input). A second spelling would be a second
// knob for one thing (Rule 14).
func TestSelfAddressSymbolIsTheOneSpelling(t *testing.T) {
	if actFlagSelf != shot.SelfAddressSymbol || voiceFlagSelf != shot.SelfAddressSymbol {
		t.Errorf("the --self spelling diverged: shot=%q act=%q voice=%q", shot.SelfAddressSymbol, actFlagSelf, voiceFlagSelf)
	}
	if voiceFlagDisplay != actFlagDisplay {
		t.Errorf("--display diverged between act (%q) and voice_input (%q) for the same X11 concept", actFlagDisplay, voiceFlagDisplay)
	}
}
