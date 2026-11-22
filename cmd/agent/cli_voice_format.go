package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

// THE voice_input FORMAT CONTRACT — one source of truth (Rule 14).
//
// The transcribing engine is the vendored FunASR llama.cpp runtime
// (work/runtime/llama.cpp). Its decoder is miniaudio with WAV, MP3 and FLAC
// compiled in and Vorbis/Ogg NOT compiled in, so its own refusal line is the
// authority on what it can read:
//
//	audio: failed to open/decode <path> (supported: wav/mp3/flac)
//
// MEASURED (2026-09-20, task feat/voice-format-contract) against the real
// runtime — /home/user/work/runtime/llama.cpp/build/bin/llama-funasr-cli:
//
//	t.wav  → transcript,  exit 0
//	t.mp3  → transcript,  exit 0
//	t.flac → transcript,  exit 0
//	t.ogg  → "audio: failed to open/decode t.ogg (supported: wav/mp3/flac)"
//
// Ogg also fails through gopi on EVERY channel (file: exit 1; HTTP: 500 with
// the same engine line; --stdio: exit 1), so the old help text — which
// advertised "(wav/mp3/flac/ogg)" — was advertising a format no channel of
// this build can transcribe. The accepted set below is therefore the ENGINE's
// set, spelled once: the help renders it, the refusals quote it, and the
// literal gate in cli_voice_format_test.go fails if any second spelling of a
// format list appears in production code (Rule 13).
const (
	voiceAcceptWAV  = "wav"
	voiceAcceptMP3  = "mp3"
	voiceAcceptFLAC = "flac"

	// voiceFormatSeparator is the joiner the help and the engine's own
	// refusal line both render the set with.
	voiceFormatSeparator = "/"

	// voiceAcceptedFormats is THE accepted format set: the three containers
	// the engine's decoder can open, in the engine's own print order. Every
	// consumer — help text, refusal hint, tests — reads this one constant.
	voiceAcceptedFormats = voiceAcceptWAV + voiceFormatSeparator +
		voiceAcceptMP3 + voiceFormatSeparator + voiceAcceptFLAC
)

// The two channels this verb offers, named once because the container-less
// sentence has to say WHICH channel accepts what (Rule 1b: a channel name is a
// meaning-bearing string, not prose).
const (
	// voiceStdioShape is the flag shape that reads bare PCM on stdin and
	// WAV-wraps it (voiceServeStdio → voiceMaterializeAudio).
	voiceStdioShape = "--server --" + voiceFlagStdio
	// voiceFileShape is the positional-file shape, which hands the path
	// straight to the engine — container-less input is refused there.
	voiceFileShape = "a file argument"
)

// voiceFormatPlaceholder is replaced by voiceAcceptedFormats when the help
// template is rendered (cli_voice.go). It is a placeholder rather than a
// Sprintf verb because the usage text carries other percent signs (the CER
// figures), and a literal "%" in a format string is a trap that only shows up
// at run time.
const voiceFormatPlaceholder = "{formats}"

// voiceEngineDoubaoPlaceholder is the same mechanism for the ONE other value
// the help must not re-spell: the cloud engine's name. It is the flag value
// (config.AudioEngineRecording), so the sentence that says "the local set does
// not bound the cloud engine" cannot drift from the value the parser accepts.
const voiceEngineDoubaoPlaceholder = "{engineDoubao}"

// voiceEngineLocalPlaceholder / voiceEngineDefaultPlaceholder are the same
// mechanism for the other engine name and for the name of the engine a bare
// run resolves to. Both are rendered from their constants
// (config.AudioEngineLocal, config.AudioEngineDefault), so the help page cannot
// go on describing a default that was flipped — which is exactly the staleness
// this change would otherwise leave behind (the default moved and a sentence
// typed by hand did not).
const (
	voiceEngineLocalPlaceholder   = "{engineLocal}"
	voiceEngineDefaultPlaceholder = "{engineDefault}"
	// voiceEngineStreamPlaceholder is the same mechanism for the streaming
	// engine's name, so the help page cannot advertise a value the parser
	// would refuse (Rule 13 — one spelling of each value).
	voiceEngineStreamPlaceholder = "{engineStream}"
	// voiceEngineSaucPlaceholder is the same mechanism for the v3 SAUC engine's
	// name — a THIRD cloud arm whose name must not be typed into the help text
	// (a help page that advertises a value the parser refuses is the staleness
	// this whole family of placeholders exists to prevent).
	voiceEngineSaucPlaceholder = "{engineSauc}"
)

// voiceSecondsPlaceholder is the capture length the usage text quotes. It is a
// placeholder for the same reason the format one is: the number lives in ONE
// named constant (voiceLocalDefaultSeconds) and the -h page renders it through
// the ONE function below, so a changed bound cannot leave the help text lying.
const voiceSecondsPlaceholder = "{seconds}"

// voiceRealtimePlaceholder is the `--realtime` row's flag spelling. It is a
// placeholder for the same reason voiceSecondsPlaceholder is one: the value a
// reader sees comes from the constant the parser matches on.
const voiceRealtimePlaceholder = "{realtime}"

// voiceAcceptedFormatSuffixes is the accepted set as file suffixes — the
// spelling an operator sees on disk. Derived from the constants above, never
// re-typed.
func voiceAcceptedFormatSuffixes() []string {
	return []string{
		"." + voiceAcceptWAV,
		"." + voiceAcceptMP3,
		"." + voiceAcceptFLAC,
	}
}

// voiceFormatAccepted reports whether a path suffix is inside the engine's
// set. Case-insensitive: a decoder that sniffs containers does not care about
// the spelling of the name; neither does this contract.
func voiceFormatAccepted(suffix string) bool {
	s := strings.ToLower(suffix)
	for _, ok := range voiceAcceptedFormatSuffixes() {
		if s == ok {
			return true
		}
	}
	return false
}

// voiceFormatNameOGG is the Vorbis/Ogg container's name. It is NOT in the
// accepted set (the engine's decoder has no Vorbis — measured), but the
// classification needs it: the OggS signature is a container the pass-through
// path recognises, so "carries a container" and "carries a container we can
// decode" are two different questions.
const voiceFormatNameOGG = "ogg"

// voiceContainerFormat maps each container signature to the format it
// identifies, derived from the SAME constants the help renders (Rule 14).
// voiceAcceptedFormats has no entry for ogg here, so an Ogg stream is
// classified as a container OUTSIDE the set rather than as container-less
// bytes — the distinction the refusal sentence turns on.
var voiceContainerFormat = map[string]string{
	voiceMagicWAV:  voiceAcceptWAV,
	voiceMagicMP3:  voiceAcceptMP3,
	voiceMagicFLAC: voiceAcceptFLAC,
	voiceMagicOGG:  voiceFormatNameOGG,
}

// voiceContainerNameOf returns the container format a byte prefix identifies
// and whether it identified one at all (an mp3 frame sync counts as mp3).
func voiceContainerNameOf(b []byte) (string, bool) {
	for magic, name := range voiceContainerFormat {
		if len(b) >= len(magic) && string(b[:len(magic)]) == string(magic) {
			return name, true
		}
	}
	// mp3 without an ID3 tag starts with a frame sync: 0xFF 0xEx.
	if len(b) >= 2 && b[0] == voiceMP3FrameSync && b[1]&voiceMP3FrameMask == voiceMP3FrameMask {
		return voiceAcceptMP3, true
	}
	return "", false
}

// voiceHasContainerMagic reports whether b starts with a container signature
// we know — the same sniff voiceMaterializeAudio uses to decide whether bytes
// need a WAV header. A container-less stream (raw f32 PCM from the capture
// client) matches nothing here.
func voiceHasContainerMagic(b []byte) bool {
	_, ok := voiceContainerNameOf(b)
	return ok
}

// voiceFormatRefusalHint returns the sentence that explains a refusal in terms
// of the CHANNEL, or "" when the failure is not a format question.
//
// The rule is deliberately narrow, so the hint never talks over an unrelated
// failure (a missing file, a path that is a directory): it fires only when the
// engine was handed an existing regular file that this contract can classify
// as outside its set — either by a suffix outside voiceAcceptedFormats, or by
// content with no container signature at all.
func voiceFormatRefusalHint(audioPath string) string {
	info, err := os.Stat(audioPath)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	suffix := strings.ToLower(filepath.Ext(audioPath))
	containerName, hasContainer, sniffKnown := "", false, true
	if f, err := os.Open(audioPath); err == nil {
		head := make([]byte, len(voiceContainerWAV))
		n, _ := f.Read(head) //stylegate:ignore short read is the expected shape for a tiny file; the zero-filled prefix cannot match a container magic
		containerName, hasContainer = voiceContainerNameOf(head[:n])
		if cerr := f.Close(); cerr != nil { //stylegate:ignore read-only probe; the classification is best-effort and the engine's own message still follows
			sniffKnown = false // unknown: do not claim the bytes carry no container
		}
	}
	suffixInSet := voiceFormatAccepted(suffix)
	containerInSet := hasContainer && voiceFormatAccepted("."+containerName)
	switch {
	case !suffixInSet && !hasContainer && sniffKnown:
		// The documented case: a bare PCM file. The set is quoted from the
		// constant, and the channel that DOES accept the same bytes is named —
		// this is the whole point of the sentence.
		return fmt.Sprintf("%s is not in the engine's decode set (%s) and its bytes carry no container; %s hands the path straight to the decoder, which sniffs bytes and cannot read raw PCM — the SAME bytes work on %s, which wraps a bare 16 kHz mono f32 PCM stream in a WAV header",
			suffixOrBare(suffix), voiceAcceptedFormats, voiceFileShape, voiceStdioShape)
	case suffixInSet && !hasContainer && sniffKnown:
		// An accepted NAME over container-less bytes. The engine sniffs bytes,
		// so the name cannot rescue it — but piping the same bytes through
		// --stdio does, because THAT channel adds the missing header.
		return fmt.Sprintf("%s is named inside the decode set (%s), but its bytes carry no container and the engine sniffs the bytes, not the name; a container-less 16 kHz mono f32 PCM stream is the shape %s takes, and it wraps it in a WAV header",
			filepath.Base(audioPath), voiceAcceptedFormats, voiceStdioShape)
	case hasContainer && !containerInSet:
		// The bytes carry a container the decoder does not have compiled in
		// (Vorbis/Ogg is the measured case). No channel of this build can read
		// it and piping it does not help — passing it through --stdio only
		// renames the temp file (measured: exit 1) — so this branch names NO
		// channel: pointing at one would be a promise that shape cannot keep.
		// The only route is a re-encode into the set.
		return fmt.Sprintf("the bytes carry the %s container, which the engine's decoder does not have compiled in (%s); no channel of this build can decode it, and piping the same bytes does not help — the container itself has to go: re-encode into one of %s first",
			containerName, voiceAcceptedFormats, voiceAcceptedFormats)
	}
	// Either the bytes carry a container the decoder HAS, or the sniff could
	// not be trusted: the engine refused for a reason that is not this
	// contract's business, and no hint may talk over it.
	return ""
}

// suffixOrBare spells a suffix for the message; an extension-less path would
// otherwise render as an empty pair of quotes.
func suffixOrBare(suffix string) string {
	if suffix == "" {
		return "this input (no file suffix)"
	}
	return suffix
}

// voiceFormatRefusal folds the channel hint into an engine failure. %w keeps
// the engine error in the chain (Rule 16), so errors.Is/As on the underlying
// failure still work with the hint appended.
func voiceFormatRefusal(audioPath string, err error) error {
	hint := voiceFormatRefusalHint(audioPath)
	if hint == "" {
		return err
	}
	return fmt.Errorf("%w; hint: %s", err, hint)
}

// voiceUsageText is the rendered help: the ONE place the accepted-format
// constant enters the user-visible text (Rule 14). Callers print this, never
// voiceInputUsage directly, so the placeholder cannot survive into the output
// and the two can never disagree.
func voiceUsageText() string {
	return strings.NewReplacer(
		voiceFormatPlaceholder, voiceAcceptedFormats,
		voiceEngineDoubaoPlaceholder, config.AudioEngineRecording,
		voiceEngineStreamPlaceholder, config.AudioEngineStreaming,
		voiceEngineSaucPlaceholder, config.AudioEngineSauc,
		voiceEngineLocalPlaceholder, config.AudioEngineLocal,
		voiceEngineDefaultPlaceholder, config.AudioEngineDefault,
		// The capture length is a PLACEHOLDER, not a number typed into the text:
		// the usage text says the same bound the parser applies (Rule 1's named
		// constant, rendered by the ONE function every caller prints through), so
		// changing voiceLocalDefaultSeconds cannot leave the -h page lying.
		voiceSecondsPlaceholder, strconv.Itoa(voiceLocalDefaultSeconds),
		// The realtime flag's spelling is rendered from its constant, like every
		// other flag on the page: the help and the parser cannot drift about what
		// the flag is called (Rule 1b/13).
		voiceRealtimePlaceholder, voiceFlagRealtime,
	).Replace(voiceInputUsage)
}
