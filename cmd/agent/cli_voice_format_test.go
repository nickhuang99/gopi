package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// THE FORMAT CONTRACT TEST — the guard that keeps the help text and the code's
// accept/refuse behaviour from drifting apart (both read voiceAcceptedFormats,
// and every advertised list must render EXACTLY that string).
//
// RED PROOF (observed, pasted in devdoc/voice-input-format-contract.md): the
// pre-fix help line said "Transcribe one audio file (wav/mp3/flac/ogg)". With
// that line restored and everything else untouched,
// TestVoiceFormatContractAdvertisedListsMatchTheConstant fails on the
// advertised-list comparison — a list is compared as a WHOLE, so a suffix
// appended to the constant's value is caught even though it CONTAINS it.

// voiceAdvertisedListRe matches a slash-joined list of audio-format words —
// the shape the help advertises a format set in ("wav/mp3/flac"). Single
// words are deliberately NOT matched: the help legitimately names one format
// in a negative example ("clip.ogg is refused"), and that is not an
// advertisement of a set.
var voiceAdvertisedListRe = regexp.MustCompile(`\b(?:wav|mp3|flac|ogg)(?:/(?:wav|mp3|flac|ogg))+\b`)

// TestVoiceFormatContractRendersTheConstant pins the ONE-WAY dependency: the
// rendered help carries the constant's value, and no placeholder survives into
// what an operator reads.
func TestVoiceFormatContractRendersTheConstant(t *testing.T) {
	rendered := voiceUsageText()
	if strings.Contains(rendered, voiceFormatPlaceholder) {
		t.Fatalf("a %q placeholder survived into the rendered help — an operator would read a template",
			voiceFormatPlaceholder)
	}
	if !strings.Contains(rendered, voiceAcceptedFormats) {
		t.Fatalf("the rendered help never states the engine's accepted set %q — the advertised list and the code's list are not the same constant",
			voiceAcceptedFormats)
	}
	if n := strings.Count(voiceInputUsage, voiceFormatPlaceholder); n < 3 {
		t.Errorf("the help template carries the placeholder %d time(s), want it at every channel that states the set (file, --server, --server --stdio)", n)
	}
	// The other one-source-of-truth value: the cloud engine's name is the
	// FLAG VALUE (config.AudioEngineRecording), rendered through its own
	// placeholder — never typed into the help.
	if strings.Contains(rendered, voiceEngineDoubaoPlaceholder) {
		t.Fatalf("a %q placeholder survived into the rendered help", voiceEngineDoubaoPlaceholder)
	}
	// Two sites name the cloud engine outside the flag row — the header note and
	// the bare-shape note — and every one of them renders from the constant,
	// which is the property under test; the count is asserted per value so a NEW
	// hand-typed spelling shows up as a changed count rather than passing
	// unnoticed.
	for _, tc := range []struct {
		placeholder string
		value       string
		want        int
	}{
		// Counts as of the 2026-10-08 resolution (the default is doubao; the
		// bullet-shape note lists the TWO deployment rungs in one paragraph, so
		// unlike the pre-merge template there is no separate "{engineLocal} on
		// request" sentence). An exact count is the point: a new hand-typed
		// spelling reddens this test and forces the author to say which
		// placeholder they meant — re-count with
		// `grep -o '{engine[A-Za-z]*}' cmd/agent/cli_voice.go`.
		// Re-counted 2026-10-08 for the streaming engine's own row: the FLAGS
		// row now names all THREE engines (it spells each one through its
		// placeholder), which is what moved doubao 3 → 4 and local 5 → 6. The
		// two streaming placeholders are asserted here so a hand-typed value
		// at a new site cannot slip in as a changed count nobody looks at.
		//
		// Re-counted 2026-10-13 (task 01a13500) for the `-v, --verbose` row:
		// the row names the two streaming arms by placeholder to say WHICH
		// engines the debug sink covers — stream 4 → 5 and sauc 3 → 4 — while
		// doubao, local and the default are untouched by it.
		{voiceEngineDoubaoPlaceholder, config.AudioEngineRecording, 5},
		{voiceEngineLocalPlaceholder, config.AudioEngineLocal, 6},
		{voiceEngineDefaultPlaceholder, config.AudioEngineDefault, 5},
		// The streaming value's sites: its own engine-note row, the sauc row's
		// "same job as" sentence, the sauc KNOBS paragraph (2026-10-14: the v3
		// credential is the same vendor pair the v2 block stores, so the note
		// that says so names the block by placeholder rather than describing it),
		// the FLAGS row, and the verbose row.
		{voiceEngineStreamPlaceholder, config.AudioEngineStreaming, 6},
		// The v3 value's sites: the sauc row's own heading, the `recording` note
		// that warns the two product lines differ, the FLAGS row, and the
		// verbose row.
		{voiceEngineSaucPlaceholder, config.AudioEngineSauc, 4},
	} {
		if n := strings.Count(voiceInputUsage, tc.placeholder); n != tc.want {
			t.Errorf("the help template carries the %q placeholder %d time(s), want exactly %d", tc.placeholder, n, tc.want)
		}
		if strings.Contains(rendered, tc.placeholder) {
			t.Errorf("a %q placeholder survived into the rendered help", tc.placeholder)
		}
		if !strings.Contains(rendered, tc.value) {
			t.Errorf("the rendered help never names %q", tc.value)
		}
	}
	if !strings.Contains(rendered, "by DEFAULT in the cloud") {
		t.Error("the rendered help does not state which engine is the default")
	}
}

// TestVoiceFormatContractAdvertisedListsMatchTheConstant is the contract
// proper: EVERY slash-joined format list the help prints must be exactly the
// constant. This is the assertion that fails on the pre-fix help — the old
// text advertised "wav/mp3/flac/ogg", a set the engine refuses on every
// channel (measured), while the constant says "wav/mp3/flac".
func TestVoiceFormatContractAdvertisedListsMatchTheConstant(t *testing.T) {
	rendered := voiceUsageText()
	lists := voiceAdvertisedListRe.FindAllString(rendered, -1)
	if len(lists) == 0 {
		t.Fatalf("the rendered help advertises no format list at all — the contract has nothing to compare (a silent help rewrite would ship unguarded)")
	}
	for _, got := range lists {
		if got != voiceAcceptedFormats {
			t.Errorf("the help advertises the format list %q, but the code's accepted set is %q — help and behaviour must come from the same constant",
				got, voiceAcceptedFormats)
		}
	}
	// The retired spelling must not come back through a second constant or a
	// literal: no advertised list may carry a format outside the set.
	for _, f := range []string{"ogg"} {
		if strings.Contains(voiceAcceptedFormats, f) {
			continue // legitimately accepted: nothing to assert
		}
		for _, got := range lists {
			if strings.Contains(got, f) {
				t.Errorf("the help advertises %q which contains %q — the engine's decoder does not have it (its own line: \"supported: %s\")",
					got, f, voiceAcceptedFormats)
			}
		}
	}
}

// TestVoiceFormatContractHelpStatesTheBytesNotNameRule pins the one claim the
// help makes about WHY a refusal happens: the decoder sniffs the bytes, not
// the extension. Measured on the real engine (2026-09-20): a WAV stream named
// .ogg DECODES (exit 0) while a real Vorbis stream named .wav is refused — so
// a help text that blamed the suffix would be wrong in both directions.
func TestVoiceFormatContractHelpStatesTheBytesNotNameRule(t *testing.T) {
	rendered := voiceUsageText()
	// The claim itself: the file-channel stanza must say the decoder sniffs
	// bytes rather than trusting the name.
	for _, want := range []string{"sniffs the", "not the file name"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the help no longer carries %q — the measured bytes-not-name behaviour must stay stated (it is why a .ogg-named wav decodes and a .wav-named Vorbis does not)", want)
		}
	}
	// And no stanza may revive the retired mis-statement that the engine
	// "cannot sniff" — it does sniff; what it cannot read is raw PCM.
	if strings.Contains(rendered, "cannot sniff it") {
		t.Errorf("the help carries the retired claim \"cannot sniff it\" — the engine DOES sniff bytes; it cannot read container-less PCM")
	}
}

// voiceEngineMeasuredSet is the engine's OWN statement of what it can decode,
// copied verbatim from the vendored runtime's refusal line. It is test data —
// the second anchor of the contract, and the only place a literal spelling of
// the set is allowed (the literal gate skips _test.go on purpose, so a
// contract may state what it is pinning without becoming a second source of
// truth in production code).
//
// Measured 2026-09-20 (see devdoc/voice-input-format-contract.md):
//
//	t.wav  → transcript   exit 0
//	t.mp3  → transcript   exit 0
//	t.flac → transcript   exit 0
//	t.ogg  → "audio: failed to open/decode t.ogg (supported: wav/mp3/flac)"
const voiceEngineMeasuredSet = "wav/mp3/flac"

// TestVoiceFormatContractConstantIsTheEngineMeasuredSet pins the constant
// itself: the ONE source of truth may not drift from what the engine's decoder
// actually has compiled in. Without this half the contract would happily
// advertise whatever the constant says (help == constant) while the engine
// disagreed with both.
func TestVoiceFormatContractConstantIsTheEngineMeasuredSet(t *testing.T) {
	if voiceAcceptedFormats != voiceEngineMeasuredSet {
		t.Fatalf("voiceAcceptedFormats = %q, but the engine's own line says %q — the advertised set must be the decoder's real set",
			voiceAcceptedFormats, voiceEngineMeasuredSet)
	}
}

// TestVoiceFormatContractAcceptedSuffixesAreTheAdvertisedList pins the OTHER
// half of the contract: the list the code's classifier ACCEPTS must be the
// list the help ADVERTISES — derived from the same constant, not a parallel
// table that could drift. The suffixes are split out of the advertised string
// (never re-typed), so a format added to the constant is accepted the moment
// it is advertised, and a format removed from the constant is refused.
func TestVoiceFormatContractAcceptedSuffixesAreTheAdvertisedList(t *testing.T) {
	want := strings.Split(voiceAcceptedFormats, voiceFormatSeparator)
	got := voiceAcceptedFormatSuffixes()
	if len(got) != len(want) {
		t.Fatalf("the code accepts %v, but the advertised list %q has %d entries — the two lists must be the same set",
			got, voiceAcceptedFormats, len(want))
	}
	for i, w := range want {
		if got[i] != "."+w {
			t.Errorf("accepted suffix %d = %q, want %q (derived from %q)", i, got[i], "."+w, voiceAcceptedFormats)
		}
		if !voiceFormatAccepted(got[i]) {
			t.Errorf("the code's own accepted suffix %q is refused by the classifier", got[i])
		}
		// An accepted format is not a format question: no refusal hint.
		dir := t.TempDir()
		p := filepath.Join(dir, "clip"+got[i])
		if err := os.WriteFile(p, append([]byte(voiceMagicWAV+"????WAVE"), make([]byte, 64)...), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		if hint := voiceFormatRefusalHint(p); hint != "" {
			t.Errorf("a well-formed %s file got a refusal hint: %s", got[i], hint)
		}
	}
	// The measured refusals: the pre-fix help advertised ogg, the engine's
	// decoder does not have it. Refused by name, on every input shape.
	for _, bad := range []string{".ogg", ".ogg.bak", ".vorbis", ""} {
		if voiceFormatAccepted(bad) {
			t.Errorf("the classifier accepts %q, which is outside the advertised list %q", bad, voiceAcceptedFormats)
		}
	}
}

// TestVoiceFormatContractLiteralGate closes the second-spelling hole (Rule 13):
// the format list may be spelled in exactly one declaration — the constants in
// cli_voice_format.go. Any OTHER production file carrying a slash-joined
// format list is a bypass, and so is the retired literal string.
//
// The scan is over production .go files of this package only (the list is a
// CLI text contract local to cmd/agent), and it skips _test.go files, where a
// format spelling is test data rather than a contract.
func TestVoiceFormatContractLiteralGate(t *testing.T) {
	const definitionFile = "cli_voice_format.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		if name == definitionFile {
			continue // the ONE allowed holder of the format spellings
		}
		raw, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(raw)
		for _, hit := range voiceAdvertisedListRe.FindAllString(src, -1) {
			t.Errorf("%s carries the format list %q — reference voiceAcceptedFormats (declared in %s) instead of re-spelling it",
				name, hit, definitionFile)
		}
		if strings.Contains(src, "wav/mp3/flac/ogg") {
			t.Errorf("%s carries the retired literal \"wav/mp3/flac/ogg\" — the engine refuses ogg (measured); the accepted set comes from %s",
				name, definitionFile)
		}
	}
	if scanned < 10 {
		t.Fatalf("the literal gate scanned only %d production files — a broken directory read must not pass vacuously", scanned)
	}
}

// TestVoiceFormatContractClassifiesInputs pins the classifier both the
// refusal hint and voiceMaterializeAudio depend on, on REAL files: a container
// is recognised, container-less bytes are not, and the accepted set is read
// from the constant rather than re-spelled.
func TestVoiceFormatContractClassifiesInputs(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	wav := write("clip.wav", append([]byte([]byte(voiceMagicWAV+"????WAVE")), make([]byte, 64)...))
	ogg := write("clip.ogg", append([]byte(voiceMagicOGG+"\x00\x02"), make([]byte, 64)...))
	pcm := write(".pcm", make([]byte, 640))
	bare := write("clip.pcm", make([]byte, 640))
	pcmWavName := write("hdrless.wav", make([]byte, 640))
	// The bytes-not-name case, measured on the real engine: a WAV stream named
	// .ogg DECODES, so the hint must classify the BYTES (a decodable container)
	// rather than the name.
	wavNamedOgg := write("wavbytes.ogg", append([]byte([]byte(voiceMagicWAV+"????WAVE")), make([]byte, 64)...))

	// wantStdioEscape is the invariant that matters: a hint may only point at
	// the --stdio shape when piping the same bytes actually rescues them —
	// measured true for container-less PCM, measured FALSE for a Vorbis stream.
	for _, tc := range []struct {
		path            string
		wantAccepted    bool
		wantContainer   bool
		wantRefusalHint bool
		wantStdioEscape bool
	}{
		{wav, true, true, false, false},
		{ogg, false, true, true, false}, // Vorbis: piping does NOT help (measured exit 1), so no channel is named
		// WAV bytes named .ogg: the decoder sniffs bytes and reads it, so a
		// hint would be a lie about a file the engine can decode.
		{wavNamedOgg, false, true, false, false},
		{pcm, false, false, true, true},       // the bare ".pcm" — no suffix at all
		{bare, false, false, true, true},      // a .pcm suffix outside the set
		{pcmWavName, true, false, true, true}, // an accepted NAME over container-less bytes
	} {
		suffix := strings.ToLower(filepath.Ext(tc.path))
		if got := voiceFormatAccepted(suffix); got != tc.wantAccepted {
			t.Errorf("voiceFormatAccepted(%q) = %v, want %v", suffix, got, tc.wantAccepted)
		}
		raw, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		if got := voiceHasContainerMagic(raw[0:4]); got != tc.wantContainer {
			t.Errorf("container magic sniff of %s = %v, want %v", filepath.Base(tc.path), got, tc.wantContainer)
		}
		hint := voiceFormatRefusalHint(tc.path)
		if tc.wantRefusalHint && hint == "" {
			t.Errorf("%s: no refusal hint, want one", filepath.Base(tc.path))
		}
		if !tc.wantRefusalHint && hint != "" {
			t.Errorf("%s: unexpected refusal hint %q", filepath.Base(tc.path), hint)
		}
		if hint != "" && !strings.Contains(hint, voiceAcceptedFormats) {
			t.Errorf("%s: hint does not quote the accepted set %q: %s", filepath.Base(tc.path), voiceAcceptedFormats, hint)
		}
		// The load-bearing invariant: naming the --stdio shape is a PROMISE
		// that piping the same bytes rescues them. Only the container-less
		// cases may make it — the engine measured exit 1 for a Vorbis stream
		// piped through exactly that shape.
		if got := strings.Contains(hint, voiceStdioShape); got != tc.wantStdioEscape {
			t.Errorf("%s: hint points at %s = %v, want %v; hint: %s",
				filepath.Base(tc.path), voiceStdioShape, got, tc.wantStdioEscape, hint)
		}
		if hint == "" && tc.wantStdioEscape {
			t.Errorf("%s: want a hint naming %s, got none", filepath.Base(tc.path), voiceStdioShape)
		}
	}

	// A missing path is not a format question: no hint may talk over the
	// engine's own "file not found" verdict.
	if hint := voiceFormatRefusalHint(filepath.Join(dir, "absent.pcm")); hint != "" {
		t.Errorf("hint for a missing file = %q, want none", hint)
	}
}

// TestVoiceFormatContractEngineFailureCarriesTheHint drives the REAL local
// engine call with the stub runtime failing, on a bare-PCM file: the error the
// operator sees must carry the engine's own line AND the channel sentence.
func TestVoiceFormatContractEngineFailureCarriesTheHint(t *testing.T) {
	models := voiceWriteModels(t)
	t.Setenv(voiceStubModeEnv, "1")
	t.Setenv(voiceStubFailEnv, "1")
	t.Setenv(voiceStubTextEnv, "")

	pcm := filepath.Join(t.TempDir(), "clip.pcm")
	if err := os.WriteFile(pcm, make([]byte, 640), 0o600); err != nil {
		t.Fatalf("write pcm: %v", err)
	}
	opts := &voiceOptions{models: models, runtime: os.Args[0], chunk: voiceDefaultChunkSec}
	_, _, err := transcribeFileLocal(opts, pcm)
	if err == nil {
		t.Fatal("a failing engine must return an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "engine failed") || !strings.Contains(msg, "model map failed") {
		t.Errorf("the engine's own verdict is missing from %q", msg)
	}
	if !strings.Contains(msg, voiceStdioShape) {
		t.Errorf("the refusal does not name the channel that accepts bare PCM (%s): %q", voiceStdioShape, msg)
	}
	if !strings.Contains(msg, voiceAcceptedFormats) {
		t.Errorf("the refusal does not quote the accepted set %q: %q", voiceAcceptedFormats, msg)
	}

	// A wav-named file over container-less bytes gets the same channel
	// sentence; a plain missing file never gets one (Rule 15 — the hint must
	// not mask the real failure).
	if _, _, err := transcribeFileLocal(opts, filepath.Join(t.TempDir(), "absent.pcm")); err == nil || strings.Contains(err.Error(), "hint:") {
		t.Errorf("missing file error = %v, want the engine verdict with no format hint", err)
	}
}
