package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The --out flag is the client's debug evidence: `gopi tool voice_input --client
// --out nick.wav` must keep the captured audio on disk. These tests pin the
// three properties that made the shipped state a defect — the flag was
// ADVERTISED (usage text) while the parser REFUSED it ("unknown flag"), so the
// advertised capability did not exist. The last test in this file is the
// class-level guard: nothing may be advertised that is not accepted.

func TestVoiceParseAcceptsOutFlag(t *testing.T) {
	dir := t.TempDir()
	for _, form := range [][]string{
		{"--client", "--out", filepath.Join(dir, "a.wav")},   // separate value
		{"--client", "--out=" + filepath.Join(dir, "b.wav")}, // joined value
		{"--client", "--seconds", "3", "--out", filepath.Join(dir, "c.wav"), "--stdio"},
	} {
		opts, err := parseVoiceArgs(form)
		if err != nil {
			t.Fatalf("parseVoiceArgs(%q): unexpected error: %v", form, err)
		}
		if opts.out == "" {
			t.Errorf("parseVoiceArgs(%q): out is empty — the flag was dropped", form)
		}
		// The regression being guarded: the pre-fix parser answered
		// `unknown flag --out`, even though the usage text advertised it.
		if strings.Contains(opts.out, voiceFlagOut) {
			t.Errorf("parseVoiceArgs(%q): out = %q looks like the flag name, not its value", form, opts.out)
		}
	}
}

func TestVoiceParseOutDefaultsToEmpty(t *testing.T) {
	// Empty means "not asked for": the client must not invent a debug file.
	opts, err := parseVoiceArgs([]string{"--client"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.out != "" {
		t.Errorf("out = %q, want empty by default", opts.out)
	}
}

func TestVoiceParseOutRejectsMisuse(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "kept.wav")
	if err := os.WriteFile(existing, []byte("yesterday's recording"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no value", []string{"--client", "--out"}, "requires a file path"},
		{"directory", []string{"--client", "--out", dir}, "is a directory"},
		{"existing file", []string{"--client", "--out", existing}, "already exists"},
		{"file mode", []string{"clip.wav", "--out", filepath.Join(dir, "n.wav")}, "is a --client flag"},
		{"server mode", []string{"--server", "--out", filepath.Join(dir, "n.wav")}, "is a --client flag"},
		{"list-devices", []string{"--client", "--list-devices", "--out", filepath.Join(dir, "n.wav")}, "only lists devices"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseVoiceArgs(tc.args)
			if err == nil {
				t.Fatalf("parseVoiceArgs(%q): expected a refusal", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
			// A refusal must not be the generic parser complaint: that is the
			// exact message shape this feature exists to eliminate.
			if strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("error = %q — --out must be a KNOWN flag", err)
			}
		})
	}
	// The refusal must leave the kept recording untouched.
	raw, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "yesterday's recording" {
		t.Errorf("existing file was modified: %q", raw)
	}
}

func TestVoiceWriteCaptureFileWritesPlayableWAV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nick.wav")
	// 0.25s of PCM16 at the capture contract — enough for a real header.
	pcm := make([]byte, voiceCaptureSampleRate*voiceCaptureBytesPerSample/4)

	written, err := voiceWriteCaptureFile(path, pcm)
	if err != nil {
		t.Fatalf("voiceWriteCaptureFile: %v", err)
	}
	if len(written) != voiceWAVHeaderBytes+len(pcm) {
		t.Fatalf("returned %d bytes, want %d", len(written), voiceWAVHeaderBytes+len(pcm))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if !bytes.Equal(raw, written) {
		t.Error("file bytes differ from the returned bytes")
	}
	// Ground truth for "playable": the container must DESCRIBE the samples it
	// carries, so a player reads exactly the captured audio.
	if got := string(raw[0:4]); got != voiceMagicWAV {
		t.Errorf("magic = %q, want %q", got, voiceMagicWAV)
	}
	if got := binary.LittleEndian.Uint32(raw[40:]); got != uint32(len(pcm)) {
		t.Errorf("data chunk size = %d, want %d", got, len(pcm))
	}
	if got := binary.LittleEndian.Uint32(raw[24:]); got != voiceCaptureSampleRate {
		t.Errorf("sample rate = %d, want %d", got, voiceCaptureSampleRate)
	}
	if got := binary.LittleEndian.Uint16(raw[34:]); got != voiceCaptureBitsPerSample {
		t.Errorf("bits per sample = %d, want %d", got, voiceCaptureBitsPerSample)
	}
	// The debug copy must be byte-identical to what the HTTP route forwards:
	// a debug file that differed from the sent audio would be worthless.
	if !bytes.Equal(raw, voicePCM16ToWAV(pcm)) {
		t.Error("file is not the same container the HTTP route posts")
	}
}

func TestVoiceWriteCaptureFileRefusesToClobberAndKeepsTheOldFile(t *testing.T) {
	// The parse-time check covers the common case; O_EXCL covers the race (a
	// file appearing between the parse and the write). Either way the kept
	// recording must survive: overwriting it is data loss, not convenience.
	dir := t.TempDir()
	path := filepath.Join(dir, "kept.wav")
	if err := os.WriteFile(path, []byte("yesterday"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := voiceWriteCaptureFile(path, make([]byte, 100)); err == nil {
		t.Fatal("expected a refusal when the target file exists")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "yesterday" {
		t.Errorf("existing file was clobbered: %q", raw)
	}
}

func TestVoiceWriteCaptureFileMissingDirectoryNamesTheAction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent", "nick.wav")
	_, err := voiceWriteCaptureFile(path, make([]byte, 100))
	if err == nil {
		t.Fatal("expected a refusal for a missing parent directory")
	}
	// Rule 15: the failure is reported with the path AND the fix, never
	// swallowed. An empty file must not be left behind either.
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want it to name the missing directory", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("a file was created despite the failure: stat err = %v", statErr)
	}
}

// TestVoiceUsageAdvertisesOnlyAcceptedFlags is the class-level guard for the
// defect this file was written for: the usage text listed `--out` while the
// parser rejected it, so `gopi tool voice_input --client --out nick.wav` answered
// "unknown flag" for a capability the command's own help promised. Every flag
// the Flags section advertises must be a member the parser accepts.
func TestVoiceUsageAdvertisesOnlyAcceptedFlags(t *testing.T) {
	advertised := voiceUsageFlagNames(t)
	if len(advertised) < 10 {
		t.Fatalf("parsed only %d flags out of the usage text — the scan is broken, not the usage: %v",
			len(advertised), advertised)
	}
	for _, name := range advertised {
		if !voiceFlagMember(name) {
			t.Errorf("usage advertises --%s but voiceFlagMember refuses it — "+
				"an advertised flag the parser rejects is a broken promise (the --out defect)", name)
		}
		// Naming the flag is not enough: it must have a constant, so its
		// spelling lives in one place (Rule 1b/13).
		if _, ok := voiceFlagConstNames[name]; !ok {
			t.Errorf("--%s has no entry in voiceFlagConstNames (bare literal)", name)
		}
	}
	// The specific flag from the bug report must be both advertised and wired.
	wantOut := false
	for _, name := range advertised {
		if name == voiceFlagOut {
			wantOut = true
		}
	}
	if !wantOut {
		t.Errorf("the usage text no longer documents --%s", voiceFlagOut)
	}
}

// voiceUsageFlagNames scans the Flags section and returns every flag it
// advertises: each entry is the FIRST `--name` token of its line. Only an
// exactly flag-shaped token counts — a prose continuation that happens to open
// with `--` (e.g. "--server-url, refused device)") is NOT a flag, and the scan
// stops at it. The Examples section never matches (the scan stops at its
// header).
func voiceUsageFlagNames(t *testing.T) []string {
	t.Helper()
	var names []string
	inFlags := false
	for _, line := range strings.Split(voiceUsageText(), "\n") {
		if strings.HasPrefix(line, voiceFlagsHeader) {
			inFlags = true
			continue
		}
		if !inFlags {
			continue
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			continue
		}
		for _, tok := range strings.Fields(line) {
			if !voiceUsageFlagToken.MatchString(tok) {
				break
			}
			names = append(names, strings.TrimPrefix(tok, "--"))
		}
	}
	return names
}

// voiceFlagsHeader opens the section the guard reads, and voiceUsageFlagToken
// is the shape a real flag token has (`--name`, no trailing punctuation). Both
// are named because the guard and the usage text must agree on them.
const voiceFlagsHeader = "Flags:"

var voiceUsageFlagToken = regexp.MustCompile(`^--[a-z0-9][a-z0-9-]*$`)

// TestVoiceCaptureFailureKeepsBothErrorsReachable is the regression guard for
// Rule 16 (checklist 2.2) at this site: the cleanup error used to be rendered
// with %v, so a caller could never detect "the incomplete file is still on
// disk" — errors.Is/As reached only the write error.
func TestVoiceCaptureFailureKeepsBothErrorsReachable(t *testing.T) {
	writeErr := errors.New("short write: 3 of 44 bytes")
	rmErr := errors.New("remove nick.wav: permission denied")

	joined := voiceCaptureFailure("nick.wav", writeErr, rmErr)
	if !errors.Is(joined, writeErr) {
		t.Error("the write error left the chain — errors.Is cannot reach it")
	}
	if !errors.Is(joined, rmErr) {
		t.Error("the cleanup error left the chain — errors.Is cannot reach it")
	}
	// Both must still be READABLE: the message is what an operator sees, and
	// the incomplete-file note is the evidence that a partial capture survives
	// on disk (Rule 15 — no silent failure).
	for _, want := range []string{"short write", "permission denied", "could not be removed"} {
		if !strings.Contains(joined.Error(), want) {
			t.Errorf("message %q does not mention %q", joined.Error(), want)
		}
	}

	// No cleanup failure ⇒ nothing to join, and the sentence says so.
	clean := voiceCaptureFailure("nick.wav", writeErr, nil)
	if !errors.Is(clean, writeErr) {
		t.Error("the write error left the chain in the clean path")
	}
	if strings.Contains(clean.Error(), "could not be removed") {
		t.Errorf("clean path claims a cleanup failure: %q", clean.Error())
	}
	if !strings.Contains(clean.Error(), "incomplete file removed") {
		t.Errorf("clean path does not report the removal: %q", clean.Error())
	}
}

// The bug report this guards (2026-10-05, host host-a):
//
//	$ gopi tool voice_input --client --out /tmp/8seconds1.wav --seconds 8
//	[voice] wrote 256044 bytes of WAV (8.0s) to /tmp/8seconds1.wav
//	gopi tool voice_input: cannot reach transcriber at http://127.0.0.1:7788/transcribe: … connection refused
//	                   (start it with `gopi tool voice_input --server --listen :7788`)
//
// The operator read the run as "it is still using the previous run's server"
// — and the MESSAGE earned that reading: the hint was a literal naming :7788,
// printed under a `cannot reach` line that names the URL this run actually
// asked for, so a client pointed at another port (or another host) was told to
// start a server somewhere it never asked about.
//
// The fix is that the advice is derived from the FAILED URL. The tests below
// pin the property, not the wording: every hint must name the port/host of the
// target it is advising about, and it may never name the :7788 default for a
// run that did not use it.

func TestVoiceStartServerHintQuotesTheFailedTarget(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		want   []string // must appear
		absent []string // must NOT appear
	}{
		{
			name:   "explicit non-default port",
			url:    "http://127.0.0.1:7999" + voiceTranscribePath,
			want:   []string{"127.0.0.1:7999"},
			absent: []string{voiceHostPortSeparator + voiceDefaultListenPort, voiceDefaultListen},
		},
		{
			name:   "a LAN peer is told to run the server THERE, not here",
			url:    "http://10.0.0.6:7788" + voiceTranscribePath,
			want:   []string{"10.0.0.6", "ON that host"},
			absent: []string{voiceDefaultListen},
		},
		{
			name: "localhost spelling stays loopback",
			url:  "http://localhost:7999" + voiceTranscribePath,
			want: []string{voiceLoopbackHost + voiceHostPortSeparator + "7999"},
		},
		{
			name: "no port falls back to the port the server's own default binds",
			url:  "http://127.0.0.1" + voiceTranscribePath,
			want: []string{voiceDefaultListenPort},
		},
		{
			name: "the shipped default still reads as before",
			url:  voiceDefaultServerURL + voiceTranscribePath,
			want: []string{voiceDefaultListen},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := voiceStartServerHint(tc.url)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("hint for %q = %q, want it to name %q", tc.url, got, w)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("hint for %q = %q must not name %q (that is an address this run never used)",
						tc.url, got, a)
				}
			}
		})
	}
}

// The end-to-end shape of the same property, through the REAL forwarding
// function: a client that cannot reach its target gets an error whose text
// names THAT target's port, both in the "cannot reach" line and in the hint.
// Nothing here is a mock of the message — voiceForwardHTTP is the function the
// client runs.
func TestVoiceForwardRefusalNamesTheFailedPortNotTheDefault(t *testing.T) {
	// A port nothing listens on. Reserved-for-testing range, and the failure
	// is instant (connection refused), never a timeout.
	const deadPort = "7951"
	url := voiceSchemeHTTP + voiceLoopbackHost + voiceHostPortSeparator + deadPort + voiceTranscribePath

	_, err := voiceForwardHTTP(url, []byte("WAVDATA"))
	if err == nil {
		t.Fatalf("posting to %s unexpectedly succeeded", url)
	}
	msg := err.Error()
	if !strings.Contains(msg, deadPort) {
		t.Errorf("error %q does not name the port that failed (%s)", msg, deadPort)
	}
	// The pre-fix message carried the literal "(start it with `gopi tool voice_input
	// --server --listen :7788`)" for EVERY target, so a run pointed elsewhere
	// was advised to start a server it never asked for. ":7788" — the default
	// listen port with its separator — must therefore not appear at all here.
	if strings.Contains(msg, voiceHostPortSeparator+voiceDefaultListenPort) {
		t.Errorf("error %q names %s%s, the address of a DIFFERENT run; the advice must be derived "+
			"from the target that failed", msg, voiceHostPortSeparator, voiceDefaultListenPort)
	}
}
