package main

// cli_voice_refusal_test.go — the CONTRADICTORY-COMBINATION table.
//
// Every row is a pair of things the operator asked for that cannot both be
// true: two roles in one process (--client/--server), a shape that records
// nothing given a flag that only means something while recording, or a flag
// that modifies a shape which is not the one being run. The contract these
// rows pin is the one the whole refusal layer exists for:
//
//	the refusal is NAMED (the flag, the shape it was given on, and what to do
//	instead) and it happens at PARSE time — before a recorder is spawned, so
//	a mistake that is knowable up front never costs a recording.
//
// It is asserted as TEXT, not as `err != nil`: the value of a refusal is that
// the operator can act on it, and an unnamed error ("invalid arguments") would
// pass a bare error check while leaving them exactly as stuck as before.
//
// The sibling files own their own faces and stay where they are: the
// author's cli_voice_test.go and cli_voice_local_test.go keep the capture
// defaults and the first refusals, cli_self_addressing_test.go the --self
// window family, cli_voice_out_test.go the client's debug file. This file is
// the MATRIX: one row per genuinely contradictory pair, so a new flag cannot
// be added to a shape where it means nothing without reddening here.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// voiceRefusalCase is one contradictory pair. wantFlag and wantPhrase are
// SUBSTRING assertions on the message, and at least one of them must exist on
// every row: a refusal that names neither the flag nor the reason is not
// actionable.
type voiceRefusalCase struct {
	name     string
	args     []string
	wantFlag string // "--chunk" — the flag the message must name
	wantText string // a fragment of the reason, quoted from the message
}

// voiceContradictoryCombinations is the matrix. Rows are grouped by the shape
// the extra flag was given on, because that is the axis the messages name.
func voiceContradictoryCombinations() []voiceRefusalCase {
	f := func(name string) string { return "--" + name }
	return []voiceRefusalCase{
		// ── two roles in one process ──────────────────────────────────────
		{"client and server are the two ends of one hop", []string{f(voiceFlagClient), f(voiceFlagServer)}, f(voiceFlagClient), "pick one side"},
		// NOTE on `--client --list-devices`: it is NOT in this table, and that
		// is a finding, not an omission. The pair is legal — --list-devices
		// prints the device table and exits before any capture, and the help
		// page advertises exactly that spelling — so it is asserted ACCEPTED in
		// TestVoiceParseDefaultsAreTheCaptureShapeAndItsFlags below. A refusal
		// here would have broken a documented shape.

		// ── two transports of one server ──────────────────────────────────
		{"stdio and listen are mutually exclusive", []string{f(voiceFlagStdio), f(voiceFlagListen), ":7788"}, f(voiceFlagStdio), "mutually exclusive"},

		// ── a server shape never reads a named file ───────────────────────
		{"an audio file on the server shape", []string{f(voiceFlagServer), "clip.wav"}, f(voiceFlagServer), "no audio-file argument"},
		{"an audio file while listing devices", []string{f(voiceFlagListDevices), "clip.wav"}, f(voiceFlagServer), "no audio-file argument"},
		{"an audio file for the client (its input is a device)", []string{f(voiceFlagClient), "clip.wav"}, f(voiceFlagClient), "takes no audio file"},

		// ── flags that only mean something while RECORDING ────────────────
		{"seconds on a file run", []string{"clip.wav", f(voiceFlagSeconds), "5"}, f(voiceFlagSeconds), "records nothing"},
		{"seconds while listing devices", []string{f(voiceFlagListDevices), f(voiceFlagSeconds), "5"}, f(voiceFlagSeconds), "records nothing"},
		{"device on a file run", []string{"clip.wav", f(voiceFlagDevice), "default"}, f(voiceFlagDevice), "records nothing"},
		{"record on a file run", []string{"clip.wav", f(voiceFlagRecord), "keep.wav"}, f(voiceFlagRecord), "records nothing"},
		{"record on the server shape", []string{f(voiceFlagServer), f(voiceFlagRecord), "keep.wav"}, f(voiceFlagRecord), "records nothing"},
		{"record while listing devices", []string{f(voiceFlagListDevices), f(voiceFlagRecord), "keep.wav"}, f(voiceFlagRecord), voiceListDevicesShapeName},
		{"the client's --out on the bare capture", []string{f(voiceFlagOut), "kept.wav"}, f(voiceFlagOut), voiceCaptureShapeName},
		{"--out while listing devices", []string{f(voiceFlagListDevices), f(voiceFlagOut), "kept.wav"}, f(voiceFlagOut), voiceListDevicesShapeName},
		{"the server-url a client posts to, on the server itself", []string{f(voiceFlagServer), f(voiceFlagServerURL), "http://x:1"}, f(voiceFlagServerURL), voiceServerShapeName},
		{"the server-url on a device listing", []string{f(voiceFlagListDevices), f(voiceFlagServerURL), "http://x:1"}, f(voiceFlagServerURL), voiceListDevicesShapeName},

		// ── --chunk windows a FILE on disk: nothing else has one ──────────
		{"chunk on the bare capture (there is no file to window)", []string{f(voiceFlagChunk), "15"}, f(voiceFlagChunk), "reads the microphone"},
		{"chunk on the client (it records and posts, it does not window)", []string{f(voiceFlagClient), f(voiceFlagChunk), "15"}, f(voiceFlagChunk), voiceClientShapeName},
		{"chunk while listing devices", []string{f(voiceFlagListDevices), f(voiceFlagChunk), "15"}, f(voiceFlagChunk), voiceListDevicesShapeName},

		// ── weights are read by the LOCAL transcribing shapes only ────────
		{"models on the client (the far side owns its weights)", []string{f(voiceFlagClient), f(voiceFlagModels), "/models"}, f(voiceFlagModels), voiceClientShapeName},
		{"runtime on the client", []string{f(voiceFlagClient), f(voiceFlagRuntime), "/bin/engine"}, f(voiceFlagRuntime), voiceClientShapeName},
		{"models while listing devices", []string{f(voiceFlagListDevices), f(voiceFlagModels), "/models"}, f(voiceFlagModels), voiceListDevicesShapeName},
		{"runtime while listing devices", []string{f(voiceFlagListDevices), f(voiceFlagRuntime), "/bin/engine"}, f(voiceFlagRuntime), voiceListDevicesShapeName},

		// ── --display only reaches a window through --self ────────────────
		{"display without self, on the capture shape", []string{f(voiceFlagDisplay), ":57"}, f(voiceFlagDisplay), f(voiceFlagSelf)},
		{"display on a file run", []string{"clip.wav", f(voiceFlagDisplay), ":57"}, f(voiceFlagDisplay), f(voiceFlagSelf)},
		{"display on the client without self", []string{f(voiceFlagClient), f(voiceFlagDisplay), ":57"}, f(voiceFlagDisplay), f(voiceFlagSelf)},

		// ── --self has no window on these shapes ─────────────────────────
		{"self on the server (an ssh hop has no shared window)", []string{f(voiceFlagServer), f(voiceFlagSelf)}, f(voiceFlagSelf), voiceServerShapeName},
		{"self on a pipe", []string{f(voiceFlagStdio), f(voiceFlagSelf)}, f(voiceFlagSelf), f(voiceFlagStdio)},
		{"self while listing devices (there is no transcript)", []string{f(voiceFlagListDevices), f(voiceFlagSelf)}, f(voiceFlagSelf), voiceListDevicesShapeName},
	}
}

// TestVoiceParseRefusesContradictoryCombinations drives the matrix through the
// real parser and reports every row that was ACCEPTED — an accepted
// contradiction is the defect this file exists to prevent, because the run
// would then proceed to spend a recording on a shape that cannot honour what
// was asked for.
func TestVoiceParseRefusesContradictoryCombinations(t *testing.T) {
	for _, tc := range voiceContradictoryCombinations() {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseVoiceArgs(tc.args)
			if err == nil {
				t.Fatalf("parseVoiceArgs(%q) was ACCEPTED — a contradictory pair must be refused at parse time, before any recording is spent", tc.args)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.wantFlag) {
				t.Errorf("refusal does not name the flag %q: %s", tc.wantFlag, msg)
			}
			if !strings.Contains(msg, tc.wantText) {
				t.Errorf("refusal does not carry the reason %q: %s", tc.wantText, msg)
			}
		})
	}
}

// TestVoiceParseRefusalDoesNotCreateTheNamedFile pins the "no recording is
// spent" half of the contract at the layer the parser can honour: --record and
// --out NAME a file, and a refused invocation must leave that path exactly as
// it found it. A parser that opened or truncated the file before deciding the
// combination was impossible would destroy a kept recording while reporting
// that nothing had happened.
func TestVoiceParseRefusalDoesNotCreateTheNamedFile(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "keep.wav")
	out := filepath.Join(dir, "kept.wav")
	fileRun := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(fileRun, []byte("RIFF-old"), 0o644); err != nil {
		t.Fatal(err)
	}

	refused := [][]string{
		{"clip.wav", "--" + voiceFlagRecord, record},
		{"--" + voiceFlagOut, out},
		{"--" + voiceFlagListDevices, "--" + voiceFlagRecord, record},
	}
	for _, args := range refused {
		args := append([]string(nil), args...)
		for i, a := range args {
			switch a {
			case "clip.wav":
				args[i] = fileRun
			}
		}
		if _, err := parseVoiceArgs(args); err == nil {
			t.Fatalf("parseVoiceArgs(%q) was accepted; the row above exists to be refused", args)
		}
	}
	for _, p := range []string{record, out} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("a refused parse left %s on disk (stat err = %v) — the file is only written by a run that got past the parser", p, err)
		}
	}
	// The file the run itself named must be untouched as well: a refusal is not
	// allowed to truncate an input.
	if b, err := os.ReadFile(fileRun); err != nil || string(b) != "RIFF-old" {
		t.Errorf("the audio file named by a refused invocation was modified: %q, %v", b, err)
	}
}

// TestVoiceParseAdvertisedShapesStillParse pins the OTHER side of the refusal
// layer against the surface it could most easily break: every shape the verb's
// own usage text advertises must still parse. This is the check that would have
// caught my first draft, which listed `--client --list-devices` as a
// contradiction — the usage text advertises it ("which microphones exist"), so
// the parser was right and the draft was wrong.
func TestVoiceParseAdvertisedShapesStillParse(t *testing.T) {
	advertised := map[string][]string{
		"<audio-file> [--chunk N]":                        {"talk.wav", "--" + voiceFlagChunk, "15"},
		"--server":                                        {"--" + voiceFlagServer},
		"--server --listen :7788":                         {"--" + voiceFlagServer, "--" + voiceFlagListen, ":7788"},
		"--server --stdio":                                {"--" + voiceFlagServer, "--" + voiceFlagStdio},
		"--client [--device ID] [--seconds N]":            {"--" + voiceFlagClient, "--" + voiceFlagDevice, "default", "--" + voiceFlagSeconds, "8"},
		"--list-devices":                                  {"--" + voiceFlagListDevices},
		"--client --list-devices":                         {"--" + voiceFlagClient, "--" + voiceFlagListDevices},
		"--client --seconds N --stdio":                    {"--" + voiceFlagClient, "--" + voiceFlagSeconds, "8", "--" + voiceFlagStdio},
		"--client --seconds 8 --out nick.wav":             {"--" + voiceFlagClient, "--" + voiceFlagSeconds, "8"},
		"--client --self --seconds 8":                     {"--" + voiceFlagClient, "--" + voiceFlagSelf, "--" + voiceFlagSeconds, "8"},
		"--client --self --seconds 8 --display :57":       {"--" + voiceFlagClient, "--" + voiceFlagSelf, "--" + voiceFlagSeconds, "8", "--" + voiceFlagDisplay, ":57"},
		"--client --server-url … --seconds 8":             {"--" + voiceFlagClient, "--" + voiceFlagServerURL, "http://192.168.1.6:7788", "--" + voiceFlagSeconds, "8"},
		"--seconds 5 (the bare capture)":                  {"--" + voiceFlagSeconds, "5"},
		"--record (the bare capture keeps its utterance)": {"--" + voiceFlagRecord, "utterance.wav"},
	}
	for shape, args := range advertised {
		t.Run(shape, func(t *testing.T) {
			if _, err := parseVoiceArgs(args); err != nil {
				t.Fatalf("the usage text advertises %q, but parseVoiceArgs(%q) refused it: %v — a refusal layer that rejects an advertised shape is the defect, not the shape", shape, args, err)
			}
		})
	}
}

// TestVoiceParseDefaultsAreTheCaptureShapeAndItsFlags is the positive half of
// the matrix: the combinations that must stay ACCEPTED. A refusal layer that
// over-triggers is the same defect in the opposite coat, and the default shape
// is the one it would break first — the whole verb is "no argument records".
func TestVoiceParseDefaultsAreTheCaptureShapeAndItsFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want func(*voiceOptions) bool
	}{
		{"bare invocation is the capture default",
			nil,
			func(o *voiceOptions) bool {
				return o.capture && !o.server && !o.client && !o.listDevices && o.audio == "" &&
					o.seconds == voiceLocalDefaultSeconds
			}},
		{"the capture keeps its own flags",
			[]string{"--" + voiceFlagSeconds, "3", "--" + voiceFlagDevice, "default", "--" + voiceFlagRecord, "keep.wav"},
			func(o *voiceOptions) bool {
				return o.capture && o.seconds == 3 && o.device == "default" && o.record == "keep.wav"
			}},
		{"listening implies the server",
			[]string{"--" + voiceFlagListen, ":7788"},
			func(o *voiceOptions) bool { return o.server && !o.capture && o.listen == ":7788" }},
		{"the client keeps seconds, device and its target",
			[]string{"--" + voiceFlagClient, "--" + voiceFlagSeconds, "8", "--" + voiceFlagDevice, "default", "--" + voiceFlagServerURL, "http://x:1"},
			func(o *voiceOptions) bool {
				return o.client && o.seconds == 8 && o.device == "default" && o.target == "http://x:1"
			}},
		{"a file run keeps its window size",
			[]string{"clip.wav", "--" + voiceFlagChunk, "15"},
			func(o *voiceOptions) bool { return o.audio == "clip.wav" && o.chunk == 15 && !o.capture }},
		{"the client keeps --out and --record (it captures)",
			[]string{"--" + voiceFlagClient, "--" + voiceFlagOut, "kept.wav", "--" + voiceFlagRecord, "keep.wav"},
			func(o *voiceOptions) bool { return o.client && o.out == "kept.wav" && o.record == "keep.wav" }},
		// The listing spelling is ADVERTISED by the help page ("which
		// microphones exist"), so it is a legal combination and must stay one:
		// --list-devices prints the table and exits before any capture.
		{"the client's own listing shape stays legal",
			[]string{"--" + voiceFlagClient, "--" + voiceFlagListDevices},
			func(o *voiceOptions) bool { return o.client && o.listDevices }},

		// The two shapes that transcribe a file HERE keep the chunk window.
		{"a server keeps its window size (it windows every upload)",
			[]string{"--" + voiceFlagServer, "--" + voiceFlagChunk, "15"},
			func(o *voiceOptions) bool { return o.server && o.chunk == 15 }},
		{"--display rides along with --self",
			[]string{"--" + voiceFlagClient, "--" + voiceFlagSelf, "--" + voiceFlagDisplay, ":57"},
			func(o *voiceOptions) bool { return o.client && o.self && o.display == ":57" }},
		{"the local shapes keep reading their weights",
			[]string{"clip.wav", "--" + voiceFlagModels, "/m", "--" + voiceFlagRuntime, "/r"},
			func(o *voiceOptions) bool { return o.models == "/m" && o.runtime == "/r" }},
		{"listing devices keeps the device filter",
			[]string{"--" + voiceFlagListDevices, "--" + voiceFlagDevice, "default"},
			func(o *voiceOptions) bool { return o.listDevices && o.device == "default" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseVoiceArgs(tc.args)
			if err != nil {
				t.Fatalf("parseVoiceArgs(%q) was refused, but it is a legal combination: %v", tc.args, err)
			}
			if !tc.want(opts) {
				t.Fatalf("parseVoiceArgs(%q) produced the wrong options: %+v", tc.args, opts)
			}
		})
	}
}
