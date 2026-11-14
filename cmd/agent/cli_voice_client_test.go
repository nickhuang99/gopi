package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The capture client's logic is the part that can be tested without a
// microphone: argument parsing, device enumeration (parsed from real recorder
// output captured on the fleet's only capture-capable host), device
// resolution, the recorder argv that pins the format contract, and the two
// conversions. The recorder spawn itself is exercised against real hardware
// separately (see devdoc/voice-input-client.md).

// voiceRealArecordList is the verbatim `arecord -l` output of the HP laptop
// (the only capture-capable machine here: the peerbox reports "no soundcards
// found"). Kept as a fixture because the parser exists to handle exactly this
// real formatting, not an idealized one.
const voiceRealArecordList = `**** List of CAPTURE Hardware Devices ****
card 1: Generic_1 [HD-Audio Generic], device 0: ALC236 Analog [ALC236 Analog]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
`

// voiceRealPactlSources is the verbatim `pactl list short sources` output of
// the same host, including the monitor source that must be excluded.
const voiceRealPactlSources = `1	alsa_output.pci-0000_05_00.6.analog-stereo.monitor	module-alsa-card.c	s16le 2ch 44100Hz	IDLE
2	alsa_input.pci-0000_05_00.6.analog-stereo	module-alsa-card.c	s16le 2ch 48000Hz	SUSPENDED
`

func TestVoiceParseValuelessFlagDoesNotEatNextArg(t *testing.T) {
	// Regression: the hand-rolled parser's "a bare --flag takes the next token
	// as its value" rule silently swallowed the token after a valueless flag.
	// `--client clip.wav` recorded from the device and never saw the path, and
	// a trailing `--server` left server=false. Both shapes are pinned here.
	opts, err := parseVoiceArgs([]string{"--server", "--stdio"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.server || !opts.stdio {
		t.Errorf("server=%v stdio=%v, want both true", opts.server, opts.stdio)
	}

	// A valueless flag followed by a positional must leave the positional
	// visible to the mode checks (here: rejected, because --client takes no
	// file). Before the fix the path vanished into the flag's value and the
	// invocation was accepted.
	if _, err := parseVoiceArgs([]string{"--client", "clip.wav"}); err == nil {
		t.Error("--client must not swallow the following positional argument")
	}
}

func TestMemoryFlagSetBooleanFlagKeepsItsValuelessShape(t *testing.T) {
	// The mechanism itself, at the shared-parser level: a declared boolean flag
	// leaves the next token as a positional, and an undeclared one does not.
	fs := newMemoryFlagSet()
	fs.boolean("flag-it")
	positionals, err := fs.parse([]string{"--flag-it", "trailing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(positionals) != 1 || positionals[0] != "trailing" {
		t.Errorf("positionals = %v, want [trailing]", positionals)
	}
	if !fs.has("flag-it") {
		t.Error("the boolean flag should still be recorded as seen")
	}

	// Undeclared: unchanged last-wins behavior (this is what every other
	// caller of the shared parser relies on).
	fs2 := newMemoryFlagSet()
	if _, err := fs2.parse([]string{"--takes-value", "value"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fs2.get("takes-value") != "value" {
		t.Errorf("undeclared flag value = %q, want value", fs2.get("takes-value"))
	}
}

func TestVoiceParseClientDefaults(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--client"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.client {
		t.Error("client should be set")
	}
	if opts.listDevices {
		t.Error("listDevices should default to false")
	}
	if opts.seconds != voiceClientDefaultSeconds {
		t.Errorf("seconds = %d, want default %d", opts.seconds, voiceClientDefaultSeconds)
	}
	if opts.target != voiceDefaultServerURL {
		t.Errorf("target = %q, want default %q", opts.target, voiceDefaultServerURL)
	}
	if opts.server {
		t.Error("client mode must not also be server mode")
	}
}

func TestVoiceParseClientListDevices(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--client", "--list-devices"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.listDevices {
		t.Error("listDevices should be set")
	}
}

func TestVoiceParseClientSecondsAndDevice(t *testing.T) {
	opts, err := parseVoiceArgs([]string{
		"--client", "--seconds", "8", "--device", "pulse:@DEFAULT_SOURCE@",
		"--server-url", "http://192.168.7.6:7788",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.seconds != 8 {
		t.Errorf("seconds = %d, want 8", opts.seconds)
	}
	if opts.device != "pulse:@DEFAULT_SOURCE@" {
		t.Errorf("device = %q", opts.device)
	}
	if opts.target != "http://192.168.7.6:7788" {
		t.Errorf("target = %q", opts.target)
	}
}

func TestVoiceParseClientRejectsBadSeconds(t *testing.T) {
	// A recording of 0 or -1 seconds is a mistake, not a zero-length file.
	for _, bad := range []string{"0", "-3", "abc", ""} {
		if _, err := parseVoiceArgs([]string{"--client", "--seconds", bad}); err == nil {
			t.Errorf("--seconds %q should be rejected", bad)
		}
	}
}

func TestVoiceParseClientRejectsServer(t *testing.T) {
	// The two halves of the hop are different roles; one process cannot be both.
	if _, err := parseVoiceArgs([]string{"--client", "--server"}); err == nil {
		t.Error("--client with --server should be rejected")
	}
}

func TestVoiceParseClientRejectsAudioFile(t *testing.T) {
	// The client's input is a device. Accepting a path here would silently
	// record instead of transcribing the file the user named.
	if _, err := parseVoiceArgs([]string{"--client", "clip.wav"}); err == nil {
		t.Error("--client with a positional audio file should be rejected")
	}
}

func TestVoiceClientDefaultTargetPointsAtServerDefault(t *testing.T) {
	// The client's default URL and the server's default listen address are one
	// contract: if they drift, the out-of-the-box flow is a connection refused
	// with nothing pointing at the cause.
	if !strings.Contains(voiceDefaultServerURL, voiceDefaultListenPort) {
		t.Errorf("default server URL %q does not carry the server's default port %q",
			voiceDefaultServerURL, voiceDefaultListenPort)
	}
	if !strings.Contains(voiceDefaultListen, voiceDefaultListenPort) {
		t.Errorf("default listen %q does not carry %q", voiceDefaultListen, voiceDefaultListenPort)
	}
}

func TestVoiceParseArecordListRealOutput(t *testing.T) {
	devices := voiceParseArecordList(voiceRealArecordList)
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2 (plughw + hw for the one card)", len(devices))
	}
	for _, want := range []string{"CARD=Generic_1", "DEV=0"} {
		if !strings.Contains(devices[0].Name, want) {
			t.Errorf("device name %q missing %q", devices[0].Name, want)
		}
	}
	if !strings.Contains(devices[0].Name, voiceALSAPluginPlug) {
		t.Errorf("first device %q should be the converting plugin (%s): the raw hw: device "+
			"refuses 16kHz mono on this codec", devices[0].Name, voiceALSAPluginPlug)
	}
	if !strings.Contains(devices[1].Name, voiceALSAPluginRaw) {
		t.Errorf("second device %q should be the raw plugin", devices[1].Name)
	}
	if devices[0].Backend != voiceDeviceBackendALSA {
		t.Errorf("backend = %q, want %q", devices[0].Backend, voiceDeviceBackendALSA)
	}
	if !strings.Contains(devices[0].ID, voiceDeviceBackendALSA+voiceDeviceIDSeparator) {
		t.Errorf("id %q should be prefixed with its backend", devices[0].ID)
	}
	if !strings.Contains(devices[0].Label, "ALC236") {
		t.Errorf("label %q should carry the codec name", devices[0].Label)
	}
}

func TestVoiceParseArecordListAddressesByCardNameNotIndex(t *testing.T) {
	// The stability requirement: an ID must survive a card being renumbered
	// (a USB headset plugged in after boot). CARD=<name> does; CARD=<index>
	// would silently select a different device.
	devices := voiceParseArecordList(voiceRealArecordList)
	if strings.Contains(devices[0].Name, "CARD=1") {
		t.Errorf("device %q addresses the card by index — that is not a stable id", devices[0].Name)
	}
}

func TestVoiceParseArecordListIgnoresNonDeviceLines(t *testing.T) {
	for _, line := range []string{
		"**** List of CAPTURE Hardware Devices ****",
		"  Subdevices: 1/1",
		"  Subdevice #0: subdevice #0",
		"",
	} {
		if got := voiceParseArecordList(line); len(got) != 0 {
			t.Errorf("line %q parsed as %d device(s), want 0", line, len(got))
		}
	}
}

func TestVoiceParseArecordListEmptyMeansNoCaptureHardware(t *testing.T) {
	// The peerbox's real answer: arecord runs fine and lists nothing.
	if got := voiceParseArecordList("**** List of CAPTURE Hardware Devices ****\n"); len(got) != 0 {
		t.Errorf("got %d devices, want 0", len(got))
	}
}

func TestVoiceParsePactlSourcesExcludesMonitors(t *testing.T) {
	devices := voiceParsePactlSources(voiceRealPactlSources)
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1 (the monitor source must be excluded)", len(devices))
	}
	if strings.HasSuffix(devices[0].Name, voicePulseMonitorSuffix) {
		t.Errorf("device %q is a monitor — it carries playback, not the microphone", devices[0].Name)
	}
	if devices[0].Name != "alsa_input.pci-0000_05_00.6.analog-stereo" {
		t.Errorf("name = %q", devices[0].Name)
	}
	if devices[0].Backend != voiceDeviceBackendPulse {
		t.Errorf("backend = %q", devices[0].Backend)
	}
}

func TestVoiceParsePactlSourcesTolerantOfShortLines(t *testing.T) {
	if got := voiceParsePactlSources("\n\nbroken\n"); len(got) != 0 {
		t.Errorf("got %d devices, want 0", len(got))
	}
}

// voiceFakePulseRunner simulates a host with PulseAudio up plus the two
// recorder enumerators, so voiceListDevices can be tested without a device.
func voiceFakePulseRunner(sources string) voiceCommandRunner {
	return func(name string, args ...string) (string, error) {
		switch name {
		case voicePactlBinary:
			for _, a := range args {
				if a == voicePactlSourcesArg {
					return sources, nil
				}
			}
			return "alsa_input.pci-0000_05_00.6.analog-stereo\n", nil
		case voiceArecordBinary:
			return voiceRealArecordList, nil
		}
		return "", fmt.Errorf("unexpected command %s", name)
	}
}

func TestVoiceListDevicesPulseFirst(t *testing.T) {
	devices, notes, err := voiceListDevices(voiceFakePulseRunner(voiceRealPactlSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("unexpected notes: %v", notes)
	}
	if len(devices) != 4 {
		t.Fatalf("got %d devices, want 4 (pulse default + 1 pulse source + 2 alsa)", len(devices))
	}
	// Pulse first, because a bare ALSA hw: device is refused while the desktop
	// owns the card ("Device or resource busy" — measured on this host).
	if devices[0].Backend != voiceDeviceBackendPulse {
		t.Errorf("first device backend = %q, want %q", devices[0].Backend, voiceDeviceBackendPulse)
	}
	if devices[0].Name != voicePulseDefaultSource {
		t.Errorf("first device = %q, want the default-source wildcard %q (the id that survives "+
			"the user changing their default)", devices[0].Name, voicePulseDefaultSource)
	}
	if devices[len(devices)-1].Backend != voiceDeviceBackendALSA {
		t.Errorf("last device backend = %q, want %q", devices[len(devices)-1].Backend, voiceDeviceBackendALSA)
	}
}

func TestVoiceListDevicesSurvivesMissingPulse(t *testing.T) {
	// arecord alone is enough to record: a host with ALSA and no sound server
	// must still produce a device list, with a note rather than a failure.
	run := func(name string, args ...string) (string, error) {
		if name == voicePactlBinary {
			return "", fmt.Errorf("connection refused")
		}
		return voiceRealArecordList, nil
	}
	devices, notes, err := voiceListDevices(run)
	if err != nil {
		t.Fatalf("a missing pactl must not be fatal: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want the 2 ALSA ones", len(devices))
	}
	if len(notes) == 0 {
		t.Error("the missing enumerator should be reported as a note")
	}
}

func TestVoiceListDevicesReportsNothingToRecordFrom(t *testing.T) {
	// The peerbox's situation: both enumerators run, neither sees a capture
	// device. The message must say what was tried and where to go instead.
	run := func(name string, args ...string) (string, error) { return "", nil }
	devices, _, err := voiceListDevices(run)
	if err == nil {
		t.Fatalf("want an error when no capture device exists, got %d devices", len(devices))
	}
	msg := err.Error()
	for _, want := range []string{"no capture devices", voiceArecordListFlag, "microphone"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %q", msg, want)
		}
	}
}

func TestVoiceResolveDeviceByIDAndByBareName(t *testing.T) {
	devices := []voiceDevice{
		{ID: "pulse:@DEFAULT_SOURCE@", Backend: voiceDeviceBackendPulse, Name: "@DEFAULT_SOURCE@"},
		{ID: "alsa:plughw:CARD=Generic_1,DEV=0", Backend: voiceDeviceBackendALSA, Name: "plughw:CARD=Generic_1,DEV=0"},
	}
	// Full id.
	got, err := voiceResolveDevice("alsa:plughw:CARD=Generic_1,DEV=0", devices, voiceFakeRunner(t, map[string]string{}))
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got.Name != "plughw:CARD=Generic_1,DEV=0" {
		t.Errorf("by id got %q", got.Name)
	}
	// The bare recorder argument, for a user copying a line out of `arecord -L`.
	got, err = voiceResolveDevice("plughw:CARD=Generic_1,DEV=0", devices, voiceFakeRunner(t, map[string]string{}))
	if err != nil {
		t.Fatalf("by bare name: %v", err)
	}
	if got.ID != "alsa:plughw:CARD=Generic_1,DEV=0" {
		t.Errorf("by bare name got id %q", got.ID)
	}
}

func TestVoiceResolveDeviceDefaultIsFirst(t *testing.T) {
	devices := []voiceDevice{
		{ID: "pulse:@DEFAULT_SOURCE@", Name: "@DEFAULT_SOURCE@"},
		{ID: "alsa:plughw:CARD=Generic_1,DEV=0", Name: "plughw:CARD=Generic_1,DEV=0"},
	}
	got, err := voiceResolveDevice("", devices, voiceFakeRunner(t, map[string]string{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != devices[0].ID {
		t.Errorf("default device = %q, want the first listed %q", got.ID, devices[0].ID)
	}
}

func TestVoiceResolveDeviceUnknownListsAlternatives(t *testing.T) {
	devices := []voiceDevice{
		{ID: "pulse:@DEFAULT_SOURCE@"},
		{ID: "alsa:plughw:CARD=Generic_1,DEV=0"},
	}
	_, err := voiceResolveDevice("pulse:nope", devices, voiceFakeRunner(t, map[string]string{}))
	if err == nil {
		t.Fatal("an unknown device must be an error, not a silent default")
	}
	for _, want := range []string{"pulse:nope", "pulse:@DEFAULT_SOURCE@", "alsa:plughw:CARD=Generic_1,DEV=0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

func TestVoiceResolveDeviceEmptyList(t *testing.T) {
	if _, err := voiceResolveDevice("", nil, voiceFakeRunner(t, map[string]string{})); err == nil {
		t.Error("resolving from an empty list must fail")
	}
}

func TestVoiceRecorderCommandPinsTheContract(t *testing.T) {
	// The pinned contract (16kHz / mono / PCM16) as recorder argv, for both
	// backends. This is where a wrong rate would silently produce garbage
	// rather than an error, so both shapes are asserted field by field.
	alsa := voiceDevice{ID: "alsa:x", Backend: voiceDeviceBackendALSA, Name: "plughw:CARD=Generic_1,DEV=0"}
	bin, args := voiceRecorderCommand(alsa, 5)
	if bin != voiceArecordBinary {
		t.Errorf("alsa binary = %q, want %q", bin, voiceArecordBinary)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		voiceArecordFormatS16LE,
		voiceArecordRawType,
		fmt.Sprintf("%d", voiceCaptureSampleRate),
		fmt.Sprintf("%d", voiceCaptureChannels),
		"plughw:CARD=Generic_1,DEV=0",
		"-d 5",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("arecord argv %q missing %q", joined, want)
		}
	}

	pulse := voiceDevice{ID: "pulse:@DEFAULT_SOURCE@", Backend: voiceDeviceBackendPulse, Name: voicePulseDefaultSource}
	bin, args = voiceRecorderCommand(pulse, 5)
	if bin != voiceParecordBinary {
		t.Errorf("pulse binary = %q, want %q", bin, voiceParecordBinary)
	}
	joined = strings.Join(args, " ")
	for _, want := range []string{
		voiceParecordFormatS16LE,
		voiceParecordRawFlag,
		fmt.Sprintf(voiceParecordRateFormat, voiceCaptureSampleRate),
		fmt.Sprintf(voiceParecordChannelsFormat, voiceCaptureChannels),
		voicePulseDefaultSource,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("parecord argv %q missing %q", joined, want)
		}
	}
	// parecord has no duration flag — the deadline kill is the stop condition,
	// so a stray -d would be an unknown-option failure at record time.
	if strings.Contains(joined, voiceArecordDurationFlag+" ") {
		t.Errorf("parecord argv %q must not carry arecord's duration flag", joined)
	}
}

func TestVoiceTrimToSeconds(t *testing.T) {
	full := voiceCaptureSampleRate * voiceCaptureBytesPerSample * 5 // 5s
	pcm := make([]byte, full+1000)
	if got := len(voiceTrimToSeconds(pcm, 5)); got != full {
		t.Errorf("overrun trimmed to %d bytes, want %d", got, full)
	}
	if got := len(voiceTrimToSeconds(pcm[:1000], 5)); got != 1000 {
		t.Errorf("short buffer changed to %d bytes, want 1000", got)
	}
	if got := len(voiceTrimToSeconds(pcm, 0)); got != len(pcm) {
		t.Errorf("seconds=0 means unused: got %d, want %d", got, len(pcm))
	}
}

func TestVoicePCM16ToWAVHeader(t *testing.T) {
	pcm := make([]byte, voiceCaptureBytesPerSample*10)
	wav := voicePCM16ToWAV(pcm)
	if len(wav) != voiceWAVHeaderBytes+len(pcm) {
		t.Fatalf("len = %d, want %d", len(wav), voiceWAVHeaderBytes+len(pcm))
	}
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(wav[off:]) }
	u16 := func(off int) uint16 { return binary.LittleEndian.Uint16(wav[off:]) }

	if string(wav[0:4]) != voiceMagicWAV || string(wav[8:12]) != voiceWAVFmtChunk[:4] {
		t.Errorf("RIFF/fmt chunks wrong: %q %q", wav[0:4], wav[8:12])
	}
	if got := u16(20); got != voiceWAVFormatPCM {
		t.Errorf("format tag = %d, want %d (integer PCM — the client captures PCM16, not f32)",
			got, voiceWAVFormatPCM)
	}
	if got := u16(22); got != voiceCaptureChannels {
		t.Errorf("channels = %d, want %d", got, voiceCaptureChannels)
	}
	if got := u32(24); got != voiceCaptureSampleRate {
		t.Errorf("sample rate = %d, want %d", got, voiceCaptureSampleRate)
	}
	if got := u16(34); got != voiceCaptureBitsPerSample {
		t.Errorf("bits = %d, want %d", got, voiceCaptureBitsPerSample)
	}
	if got := u16(32); got != voiceCaptureChannels*voiceCaptureBitsPerSample/8 {
		t.Errorf("block align = %d", got)
	}
	if got := u32(28); got != voiceCaptureSampleRate*voiceCaptureChannels*voiceCaptureBitsPerSample/8 {
		t.Errorf("byte rate = %d", got)
	}
	if got := u32(40); got != uint32(len(pcm)) {
		t.Errorf("data size = %d, want %d", got, len(pcm))
	}
	if got := u32(4); got != uint32(voiceWAVHeaderBytes-8+len(pcm)) {
		t.Errorf("RIFF size = %d", got)
	}
}

func TestVoicePCM16ToFloat32(t *testing.T) {
	// A full-scale negative, silence, and a third-of-scale sample: the
	// int16 → [-1,1) mapping is a gain contract, and a wrong divisor is
	// audible-but-transcribable rather than an error.
	samples := []int16{-32768, 0, 10922}
	pcm := make([]byte, 0, len(samples)*2)
	for _, s := range samples {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(s))
	}
	out := voicePCM16ToFloat32(pcm)
	if len(out) != len(samples)*voiceFloat32Bytes {
		t.Fatalf("len = %d, want %d", len(out), len(samples)*voiceFloat32Bytes)
	}
	want := []float32{-1, 0, float32(10922) / voicePCM16FullScale}
	for i, w := range want {
		got := math.Float32frombits(binary.LittleEndian.Uint32(out[i*voiceFloat32Bytes:]))
		if math.Abs(float64(got-w)) > 1e-6 {
			t.Errorf("sample %d = %v, want %v", i, got, w)
		}
	}
}

func TestVoicePCM16ToFloat32DropsPartialSample(t *testing.T) {
	// A truncated stream must not shift every later sample.
	out := voicePCM16ToFloat32([]byte{0x00, 0x01, 0x02})
	if len(out) != voiceFloat32Bytes {
		t.Errorf("len = %d, want %d (one whole sample)", len(out), voiceFloat32Bytes)
	}
}

func TestVoiceTranscribeURL(t *testing.T) {
	got, err := voiceTranscribeURL("http://192.168.7.6:7788")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://192.168.7.6:7788"+voiceTranscribePath {
		t.Errorf("url = %q", got)
	}
	// A trailing slash must not produce a double slash — the server's mux
	// would 301 it and the POST body would be dropped.
	got, err = voiceTranscribeURL("http://127.0.0.1:7788/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://127.0.0.1:7788"+voiceTranscribePath {
		t.Errorf("url = %q", got)
	}
	// Regression: the resolved endpoint must be FINAL. It used to be joined
	// once by the caller and once by the forwarder, producing
	// /transcribe/transcribe — a 404 after a full recording was already spent.
	if strings.Count(got, voiceTranscribePath) != 1 {
		t.Errorf("url %q carries the path %d times, want 1", got, strings.Count(got, voiceTranscribePath))
	}
	// A schemeless target must be named as such, not fail later as
	// "unsupported protocol scheme".
	for _, bad := range []string{"", "192.168.7.6:7788", "127.0.0.1"} {
		if _, err := voiceTranscribeURL(bad); err == nil {
			t.Errorf("target %q should be rejected", bad)
		}
	}
	// A bad port must be caught BEFORE the microphone opens (the whole point
	// of resolving the URL up front), not by http.NewRequest during the POST.
	for _, bad := range []string{"http://127.0.0.1:9nowhere", "http://host:notaport"} {
		if _, err := voiceTranscribeURL(bad); err == nil {
			t.Errorf("target %q has an invalid port and should be rejected up front", bad)
		}
	}
	if _, err := voiceTranscribeURL("http:///nohost"); err == nil {
		t.Error("a URL with no host should be rejected")
	}
}

func TestVoiceTruncateForError(t *testing.T) {
	if got := truncateForError(nil); got != "(empty body)" {
		t.Errorf("empty = %q", got)
	}
	long := strings.Repeat("x", voiceErrorBodyChars+50)
	got := truncateForError([]byte(long))
	if len(got) > voiceErrorBodyChars+4 {
		t.Errorf("len = %d, want it truncated near %d", len(got), voiceErrorBodyChars)
	}
}

// TestVoiceClientNoBareDeviceLiterals is the Rule 13 gate for this feature:
// the device strings and the capture contract exist as constants, and the
// production voice files must REFERENCE them. A re-inlined "16000" or
// "@DEFAULT_SOURCE@" is exactly the drift that makes a rate change land in one
// place only.
func TestVoiceClientNoBareDeviceLiterals(t *testing.T) {
	// Each literal is allowed only where its constant is defined; the scan
	// below recognises that one site.
	literals := map[string]string{
		`"@DEFAULT_SOURCE@"`:   "voicePulseDefaultSource",
		`"S16_LE"`:             "voiceArecordFormatS16LE",
		`"s16le"`:              "voiceParecordFormatS16LE",
		`"16000"`:              "voiceCaptureSampleRate",
		`"plughw"`:             "voiceALSAPluginPlug",
		`"get-default-source"`: "voicePactlDefaultSourceArg",
	}

	files, err := filepath.Glob("cli_voice*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no voice files found — the glob is wrong, not the code")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for literal, constName := range literals {
			// The constant's own definition site is the one legitimate
			// appearance: `const x = "s16le"` / `const x = 16000`. The match is
			// whitespace-tolerant because gofmt aligns the `=` of a const
			// block, so a literal string compare would miss the very site it
			// exists to permit (and this test was written after that mistake).
			defRe := regexp.MustCompile(regexp.QuoteMeta(constName) + `\s*=\s*` + regexp.QuoteMeta(literal))
			if defRe.Match(data) {
				continue
			}
			if bytes.Contains(data, []byte(literal)) {
				t.Errorf("%s carries the bare literal %s — reference %s instead "+
					"(Rule 1b/13: one definition, or a rename lands in one site only)",
					f, literal, constName)
			}
		}
	}
}

// TestVoiceResolveMonitorSourceByName pins the one device enumeration WITHHOLDS
// on purpose. --list-devices excludes monitors (offering one as a microphone
// records silence and reads as a broken mic), but a PCM capture for a fixture
// cannot be built without one: the deterministic input of the E2E rig IS a
// monitor source, and the client devdoc tells a reader to name it. Refusing the
// name made that documented route impossible (measured on HP 2026-09-14:
// `--device pulse:gopi_src.monitor` → "unknown device", while the rig's audio
// demonstrably carried the fixture). The name is honoured only when the server
// still lists it — the same liveness question every other tier answers.
func TestVoiceResolveMonitorSourceByName(t *testing.T) {
	devices := []voiceDevice{
		{ID: "pulse:@DEFAULT_SOURCE@", Backend: voiceDeviceBackendPulse, Name: "@DEFAULT_SOURCE@"},
	}
	listed := "42\tgopi_src.monitor\tmodule-null-sink.c\ts16le 2ch 44100Hz\tRUNNING\n" +
		"43\tgopi_mic\tmodule-remap-source.c\ts16le 2ch 48000Hz\tIDLE\n"
	server := map[string]string{
		voicePactlBinary + " " + voicePactlListFlag + " " + voicePactlShortFlag + " " + voicePactlSourcesArg: listed,
	}

	t.Run("a listed monitor resolves by name", func(t *testing.T) {
		got, err := voiceResolveDevice("pulse:gopi_src.monitor", devices, voiceFakeRunner(t, server))
		if err != nil {
			t.Fatalf("a monitor named explicitly must resolve: %v", err)
		}
		if got.ID != "pulse:gopi_src.monitor" {
			t.Errorf("id = %q, want pulse:gopi_src.monitor", got.ID)
		}
		if !strings.Contains(got.Label, voiceMonitorLabelSuffix) {
			t.Errorf("label %q must say the device is a monitor, not a microphone", got.Label)
		}
	})

	t.Run("a monitor the server no longer lists stays unknown", func(t *testing.T) {
		// The hotplug answer for a monitor: the name is well-formed, so the
		// question is only whether the server still has it.
		gone := map[string]string{
			voicePactlBinary + " " + voicePactlListFlag + " " + voicePactlShortFlag + " " + voicePactlSourcesArg: "1\talsa_input.usb\tmodule-alsa-card.c\ts16le 2ch 44100Hz\tIDLE\n",
		}
		if _, err := voiceResolveDevice("pulse:gopi_src.monitor", devices, voiceFakeRunner(t, gone)); err == nil {
			t.Fatal("a monitor that no longer exists must not resolve silently")
		}
	})

	t.Run("a non-monitor miss does not consult the server", func(t *testing.T) {
		// voiceFakeRunner FAILS the test on any undeclared command, so this
		// subtest also pins that the extra lookup is monitor-specific: a plain
		// typo must not turn into a second enumerator call.
		if _, err := voiceResolveDevice("pulse:nope", devices, voiceFakeRunner(t, map[string]string{})); err == nil {
			t.Fatal("a typo must still be an unknown-device error")
		}
	})
}

// TestVoiceResolveALSADefaultSpelling pins the two spellings the help text
// documents for the system default capture and the enumeration cannot produce:
// ALSA's `default` is a CARD-LESS plugin the library synthesises (`arecord -l`
// lists cards, so it is never an enumerated ID), and tier 3 hands out the
// `alsa:default` form while `-h` and every ALSA-on-the-web example use the bare
// `default`. Measured on HP 2026-09-14: `--device default` and
// `--device alsa:default` both answered "unknown device" although the same
// device was tried (and reported busy) by tier 3 in the same session — a
// help/behaviour contradiction, not a missing device.
func TestVoiceResolveALSADefaultSpelling(t *testing.T) {
	devices := []voiceDevice{
		{ID: "pulse:@DEFAULT_SOURCE@", Backend: voiceDeviceBackendPulse, Name: voicePulseDefaultSource},
	}
	// Neither form is enumerated, and no server call may be needed for either
	// (voiceFakeRunner fails the test on any undeclared command).
	for _, want := range []string{voiceALSADefaultName, "alsa:" + voiceALSADefaultName} {
		got, err := voiceResolveDevice(want, devices, voiceFakeRunner(t, map[string]string{}))
		if err != nil {
			t.Fatalf("--device %q must resolve to the system default capture, got %v", want, err)
		}
		if got.ID != "alsa:"+voiceALSADefaultName || got.Backend != voiceDeviceBackendALSA || got.Name != voiceALSADefaultName {
			t.Errorf("--device %q resolved to %+v, want the alsa:%s entry", want, got, voiceALSADefaultName)
		}
	}
	// The spelling is exactly the documented name — nothing broader is accepted
	// by accident (a near-miss must still be an unknown device).
	if _, err := voiceResolveDevice("alsa:default2", devices, voiceFakeRunner(t, map[string]string{})); err == nil {
		t.Fatal("a near-miss plug spelling must not resolve")
	}
}

// TestVoiceSelectionSystemDefaultNamedExplicitlyPlaysOut walks the user-facing
// consequence of the resolution above on the HP shape (PulseAudio up, so tier 3
// is skipped): naming `default` explicitly must reach the ALSA plug device and
// then report what happened to THAT device (here the real HP answer, "Device or
// resource busy") — never "unknown device", and never a silent fall-through.
func TestVoiceSelectionSystemDefaultNamedExplicitlyPlaysOut(t *testing.T) {
	devices := voiceTestDevices()
	busy := fmt.Errorf("arecord failed on %q: exit status 1 — arecord: main:831: audio open error: Device or resource busy", "alsa:"+voiceALSADefaultName)
	capture := func(dev voiceDevice, seconds int) ([]byte, error) {
		if dev.ID == "alsa:"+voiceALSADefaultName {
			return nil, busy
		}
		return make([]byte, voiceMinCaptureBytes*seconds), nil
	}
	_, _, err := voiceResolveSelection(voiceALSADefaultName, devices, voiceFakeRunner(t, voiceServerUp(t)), capture)
	if err == nil {
		t.Fatal("the busy system default must fail loudly, not record")
	}
	for _, want := range []string{"alsa:default", "Device or resource busy", voiceFlagListDevices} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "unknown device") {
		t.Errorf("the system default is a real device — the error must not call it unknown: %v", err)
	}
}
