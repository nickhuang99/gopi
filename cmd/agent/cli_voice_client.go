package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/tools"
)

// voice_input --client — Phase 3 of the voice-input feature (capture side).
//
// The server half (cli_voice_server.go) accepts audio and returns text. This
// half produces the audio: it records from a real capture device and forwards
// the result to one of the two server shapes that already exist.
//
//	client --server URL   record N seconds → POST multipart to /transcribe → text
//	client --stdio        record N seconds → raw PCM on stdout (pipes into
//	                      `ssh host gopi tool voice_input --server --stdio`)
//
// ROUTE: zero runtime dependency. arecord (alsa-utils) and parecord
// (pulseaudio-utils) are already installed on the capture-capable host, cost no
// new dependency, and expose device selection directly — the pinned capture
// contract (16 kHz, mono, signed 16-bit little-endian) is expressed as their
// argv, so nothing between the mic and the wire can silently resample. The
// miniaudio.h (cgo) route from devdoc/SPEC-voice-input-phase23.md §4 would put
// the same contract behind a 4.1 MB header and a CGO_ENABLED=1 binary; it is
// documented in devdoc/voice-input-client.md as the fallback for a host where
// neither recorder exists, and is deliberately NOT built here.

const (
	// Recorders. Named because they are spawned by path lookup and a typo is a
	// silent "not found" (Rule 1b).
	voiceArecordBinary  = "arecord"
	voiceParecordBinary = "parecord"
	voicePactlBinary    = "pactl"

	// The pinned capture contract: 16 kHz mono PCM16. These four values are
	// what makes a stream decodable on the far side; a 44.1 kHz stereo stream
	// does not error, it transcribes garbage — which is why they are constants
	// referenced at every use rather than literals at one (Rule 13).
	voiceCaptureSampleRate     = 16000
	voiceCaptureChannels       = 1
	voiceCaptureBitsPerSample  = 16
	voiceCaptureBytesPerSample = voiceCaptureBitsPerSample / 8

	// The same contract spelled in each recorder's own vocabulary.
	voiceArecordFormatS16LE  = "S16_LE"
	voiceParecordFormatS16LE = "s16le"

	// arecord argv (alsa-utils).
	voiceArecordDeviceFlag   = "-D"
	voiceArecordFormatFlag   = "-f"
	voiceArecordRateFlag     = "-r"
	voiceArecordChannelsFlag = "-c"
	voiceArecordTypeFlag     = "-t"
	voiceArecordQuietFlag    = "-q"
	voiceArecordDurationFlag = "-d"
	voiceArecordListFlag     = "-l"
	voiceArecordRawType      = "raw"

	// parecord argv (pulseaudio-utils).
	voiceParecordDeviceFormat   = "--device=%s"
	voiceParecordRawFlag        = "--raw"
	voiceParecordRateFormat     = "--rate=%d"
	voiceParecordChannelsFormat = "--channels=%d"
	voiceParecordFormatFormat   = "--format=%s"
	voiceParecordLatencyFormat  = "--latency-msec=%d"

	// voiceParecordLatencyMs asks the Pulse server for a SMALL transfer
	// fragment, and it is the second half of task 01a11853's latency fix.
	//
	// parecord's default fragment is large (2 s on this host) and two costs
	// follow from it, both measured on zbook 2026-10-08 (strace on the recorder's
	// stdout, real card and rig):
	//
	//   - the first byte of audio is not written until 0.08-2.32 s after the
	//     process starts, so a window's first samples are late by that much; and
	//   - the delivery arrives in 2 s BURSTS (11876 B clusters every ~2 s), so a
	//     window that ends mid-burst is closed before the burst carrying the tail
	//     of the utterance has been handed over.
	//
	// At 50 ms the same measurement reads 0.049-0.121 s to the first sample, and
	// the chunks arrive as the audio is captured. A rare underrun is the trade,
	// and the Pulse server owns that trade (it will pad rather than fail); the
	// value is therefore conservative and named, not tuned per run.
	voiceParecordLatencyMs = 50

	// pactl argv (device enumeration through the Pulse server).
	voicePactlListFlag         = "list"
	voicePactlShortFlag        = "short"
	voicePactlSourcesArg       = "sources"
	voicePactlDefaultSourceArg = "get-default-source"

	// Device-ID namespaces. An ID is "<backend>:<recorder-argument>", so the
	// ID a user passes to --device is exactly what gets spawned (Rule 1b).
	voiceDeviceBackendPulse = "pulse"
	voiceDeviceBackendALSA  = "alsa"

	// voicePulseDefaultSource is Pulse's own wildcard for "whatever the server
	// has selected" — a STABLE id across replugs, unlike a concrete source
	// name, so it is offered first.
	voicePulseDefaultSource = "@DEFAULT_SOURCE@"

	// voicePulseMonitorSuffix marks a source that is the loopback of an output
	// sink (it captures what is PLAYING, not the microphone) — excluded from
	// the microphone list, and kept only as a test rig (see the client devdoc).
	voicePulseMonitorSuffix = ".monitor"

	// voiceMonitorLabelSuffix is appended to a monitor's --list-devices-style
	// label when it is reached by NAME (it is never listed): the label has to
	// say why this device is not a microphone (Rule 1b — the text a user reads
	// comes from one place).
	voiceMonitorLabelSuffix = " (monitor — captures what is PLAYING, not a microphone)"

	// Default recording length. Long enough to hold a sentence, short enough
	// that a forgotten --seconds does not look like a hang.
	voiceClientDefaultSeconds = 5

	// voiceCaptureGraceSec is the budget for the recorder to reach its FIRST
	// sample — NOT a flat addition to --seconds (task 01a11853).
	//
	// WHAT IT WAS, AND WHAT IT COST. The whole call used to run under
	// context.WithTimeout(seconds+voiceCaptureGraceSec) and the recorder was
	// let run to that deadline, so the operator paid 10 s of DEAD AIR on every
	// round before --seconds of audio even began: a `--seconds 6` round
	// measured 16.0 s of recorder wall time, 10.0 s of it recording nothing
	// (measured on zbook, 2026-10-08, task 01a11829; the same flat cost at
	// --seconds 3 → 13.03 s and --seconds 10 → 20.03 s).
	//
	// WHAT IT IS NOW. The grace is spent ONLY when the device is genuinely slow
	// to produce its first sample (a suspended card, a Bluetooth headset still
	// negotiating — the case the constant exists for), and the window clock
	// starts at that first sample: the caller receives --seconds of REAL audio
	// as soon as it exists, and an already-warm device pays nothing. The value
	// stays 10 because the protection it buys is unchanged; what changed is
	// WHEN it is paid.
	voiceCaptureGraceSec = 10

	// voiceCapturePollInterval is the polling period of the capture read: the
	// recorder's stdout and its exit are both observed at this granularity, so
	// the window ends within one interval of the bytes being complete.
	voiceCapturePollInterval = 10 * time.Millisecond

	// voiceCaptureSignalGraceSec bounds the wait for the recorder to die after
	// the stop signal. It is not part of a recording window: it is the
	// difference between "the process is winding down" and "the process is
	// stuck", and past it the kill is a refusal rather than a hang (Rule 15).
	//
	// THERE IS NO SEPARATE "CLOSE GRACE" BEFORE THE SIGNAL, and that is the fix
	// of task 01a11853, not an omission. The stop is sent the moment the window
	// is full (see stopVoiceRecorder): a recorder that carries its own duration
	// (arecord's -d) has already exited by then, and one that does not (parecord
	// has no duration flag) is waiting for exactly this signal — its window IS
	// closed by being stopped. A grace period spent waiting for such a process
	// to exit on its own is pure dead air, and a double unit conversion on the
	// constant that expressed it (`time.Duration(500 * time.Millisecond) *
	// time.Second`, 5e8 s) is what made every round run to the caller's context
	// deadline instead: `--seconds 6` took seconds+voiceCaptureGraceSec = 16.03 s
	// (measured on zbook, 2026-10-08).
	voiceCaptureSignalGraceSec = 2 * time.Second

	// voiceCaptureForceKillGraceSec is the SECOND grace: the extra wait for a
	// recorder after the SIGKILL of the forced stop in stopVoiceRecorder.
	//
	// IT IS NOT A BUDGET, AND ITS EXPIRY IS NOT A SUCCESS. SIGKILL cannot be
	// caught or ignored, so a process still reachable after it is one the
	// KERNEL cannot retire (an uninterruptible device read on a wedged sound
	// driver). There is nothing further a caller can do about that process —
	// what the wait buys is the confirmation that the KILL landed, so the
	// ordinary case leaves no zombie behind. Short on purpose: it is reaping,
	// not waiting for a process to wind down.
	//
	// WHY THE FORCED PATH EXISTS AT ALL (measured 2026-10-22): the grace
	// expiry used to only RETURN an error. `parecord` ignores SIGINT, so three
	// recorders survived in `gopi chat`'s own process group — `<defunct>` in
	// the table, unreaped, because the `exited` channel is read by nobody once
	// the grace select has fired. A recorder nobody owns is a microphone
	// nobody owns (Rule 15), on top of the group hazard killChildTree now
	// refuses by its own guard.
	voiceCaptureForceKillGraceSec = time.Second

	// voiceCaptureStopKilledNote is what the operator reads when the grace
	// expired and the recorder was FORCE-KILLED, i.e. the ordinary ending for a
	// recorder that ignores the stop signal (parecord does). It keeps the
	// reason and the recovered fact in one sentence, and carries the same "the
	// window closed" cause the grace note does, because the recorded audio is
	// unaffected either way — the caller classifies it by
	// voiceCaptureContextSensitiveError.
	voiceCaptureStopKilledNote = "the recorder ignored the stop signal for %s and was killed; its window's audio was kept"

	// voiceCaptureStillAliveNote is the one case a recorder cannot be recovered
	// from: SIGKILL was sent and even that could not retire the process within
	// voiceCaptureForceKillGraceSec (an interruptible-sleep block in a device
	// read). Stated as an UNCOLLECTED process, by pid, so a stuck sound driver
	// is visible instead of pretending the reap worked.
	voiceCaptureStillAliveNote = "the recorder pid %d survived SIGKILL after %s — the capture device is wedged in the kernel: the process could not be reaped"

	// voiceCaptureReadChunk is one read from the recorder's stdout. 4096 B is
	// 128 ms of 16 kHz mono PCM16 — small enough that the window's end is
	// detected within a poll interval, large enough not to spin the loop.
	voiceCaptureReadChunk = 4096

	// voiceStagedRecorderNoteTail is how many lines of a staged recorder's stderr
	// a refusal quotes (voiceStagedRecorderNote). Three is the order the one-shot
	// path prints (voiceRecorderFailure's lastLines(msg, 3)), so a cause found by
	// either shape reads the same length of evidence.
	voiceStagedRecorderNoteTail = 3
	// voiceCapturePeakWindowMS is the peak-tracking window the trailing-silence
	// rule is measured over. 128 ms is the same width as one read chunk, so the
	// peak and the silence run are read off the same samples and neither can
	// drift from the other (task 01a1188c).
	voiceCapturePeakWindowMS = 128

	// voiceCaptureTrailingSilenceMS is the SHORTEST run of near-silence that may
	// be taken as a round boundary.
	//
	// IT IS MEASURED AGAINST THE OPERATOR'S OWN SPEECH, not chosen for comfort.
	// ffmpeg's silencedetect on the fixture this closes on (task 01a11829's
	// 12.455 s recording, -40 dB, d=0.10) finds the pauses its speaker actually
	// made: 0.24 s between two phrases, and 0.38 s of tail. A SEVEN-HUNDRED-
	// MILLISECOND run looked safer and was simply wrong — the first rig run with
	// it found no boundary anywhere in that recording, so every window went to
	// its bound and was cut inside a word, which is the defect itself. The
	// constant has to be able to fire on the audio people actually record.
	//
	// 200 ms is therefore a FLOOR on what may be called a pause — below it a gap
	// is closure of a consonant, not a boundary — and the boundary is chosen as
	// the LONGEST qualifying run the window holds (voiceWindowPlan.cut), so a
	// longer pause always wins and a shorter one is never mistaken for the end.
	voiceCaptureTrailingSilenceMS = 200

	// voiceCaptureSilenceFloorDBFS is how quiet "silence" is, in dBFS relative to
	// PCM16 full scale.
	//
	// IT IS MEASURED, NOT GUESSED. The fixture's lead-in and its interior pause
	// are DIGITAL silence (-90.3 dBFS, i.e. all-zero samples) while its quietest
	// 128 ms block of speech is -36 dBFS, so this floor has 54 dB of margin below
	// the recorded speech and needs none above it; a person speaking even far
	// more quietly than the fixture is still tens of dB above it. A recorder whose
	// device noise sits above this floor simply never triggers the early close,
	// i.e. keeps the pre-existing behaviour — that is the safe direction.
	voiceCaptureSilenceFloorDBFS = -40.0

	// voiceCaptureBurstArrivalFactor is the received-audio length, as a multiple
	// of the window bound, beyond which a round's samples did NOT arrive as the
	// audio was spoken and the energy rule is therefore not offered to it.
	//
	// WHY THIS EXISTS (it is a statement about the TRANSPORT, not about speech).
	// PulseAudio's transfer fragmentation decides both how late the first sample
	// is and whether the audio arrives live: a large fragment delivers whole
	// seconds in one write (measured 2 s clusters, task 01a11853), so a reader
	// that stopped at the first quiet run would save nothing and could cut a real
	// utterance short. A window holding more than 1.5x its own bound cannot have
	// arrived live — the bound is 6 s of audio and only 6 s of wall clock passed
	// — so that recording keeps the fixed bound exactly as before.
	voiceCaptureBurstArrivalFactor = 1.5

	// The two stderr signatures above, named (Rule 1b).
	voiceCaptureBrokenPipeMarker        = "Broken pipe"
	voiceCaptureConnectionFailureMarker = "Connection failure"

	// voiceCaptureKilledMarker is how os/exec renders a signalled process.
	// Named (Rule 1b) because the classification in
	// voiceCaptureContextSensitiveError compares it and nothing else may.
	voiceCaptureKilledMarker = "signal:"

	// voiceMinCaptureBytes: below this the recorder produced no audio at all
	// (a wrong device, a muted source) — reported as a device error rather
	// than forwarded as an empty utterance.
	voiceMinCaptureBytes = voiceCaptureBytesPerSample

	// voiceClientHTTPTimeoutSec bounds the round trip to the transcriber.
	// Generous: this covers a 910 MB model's first (cold) load, not a network
	// stall — the audio itself is sent in one small request.
	voiceClientHTTPTimeoutSec = 300

	// voiceTranscribePath is the server's contract (cli_voice_server.go).
	voiceTranscribePath = "/transcribe"
	// voiceMultipartField is the multipart field name the server reads.
	voiceMultipartField = "audio"
	// voiceUploadFilename carries a .wav extension because the server keeps
	// the client's extension when it stages the upload for the engine.
	voiceUploadFilename = "capture.wav"

	// voiceErrBusyMarker is the ALSA/Pulse refusal that means the device is
	// held by another process — on a desktop that is almost always the sound
	// server, which is why it earns its own hint.
	voiceErrBusyMarker = "Device or resource busy"

	// PCM16 full scale, for the int16 → float32 conversion the stdio route
	// needs (the server's stdio shape reads raw f32 — see voiceServeStdio).
	voicePCM16FullScale = 32768.0
	// voicePCM16FullScaleDecibels and voicePCM16FullScaleDecades are the SAME
	// full scale expressed for a decibel conversion: dBFS is a ratio against
	// full scale, so the amplitude a level names is
	// 10 ** (dbfs / voicePCM16FullScaleDecibels) * voicePCM16FullScale. Named
	// (Rule 1) because the silence floor and this conversion must state the
	// same reference — a bare 20 here would be a second, silent definition of
	// "full scale" (Rule 14).
	voicePCM16FullScaleDecibels = 20.0
	voicePCM16FullScaleDecades  = 10.0
)

// voiceDevice is one selectable capture endpoint.
type voiceDevice struct {
	// ID is the stable handle for --device: "<backend>:<name>", where name is
	// the exact argument handed to the recorder. ALSA names use CARD=<card
	// NAME> and not the card index, so an ID survives a device being
	// renumbered (a USB headset plugged in after boot).
	ID string
	// Backend is voiceDeviceBackendPulse or voiceDeviceBackendALSA.
	Backend string
	// Name is what the recorder is told to open.
	Name string
	// Label is the human description shown by --list-devices.
	Label string
}

// voiceCommandRunner runs a command and returns its combined stdout. Injected
// so device enumeration is unit-testable against captured real output.
type voiceCommandRunner func(name string, args ...string) (string, error)

// voiceRunCommand is the production runner: stdout only, because every
// enumerator parses a listing and stderr would corrupt it.
func voiceRunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg != "" {
			return out.String(), fmt.Errorf("%s: %w — %s", name, err, lastLines(msg, 2))
		}
		return out.String(), fmt.Errorf("%s: %w", name, err)
	}
	return out.String(), nil
}

// voiceArecordListRe matches one line of `arecord -l`:
//
//	card 1: Generic_1 [HD-Audio Generic], device 0: ALC236 Analog [ALC236 Analog]
//
// Captured from the only capture-capable host in this fleet (HP laptop).
var voiceArecordListRe = regexp.MustCompile(
	`^card (\d+): (\S+) \[([^\]]*)\], device (\d+): ([^\[]+)\[([^\]]*)\]`)

// voiceParseArecordList turns `arecord -l` output into ALSA devices. `arecord
// -l` lists capture-capable hardware only, so every line it prints is a
// microphone candidate.
//
// Two IDs per card/device are offered, and the difference matters: plughw
// accepts the 16 kHz mono PCM16 contract and lets ALSA convert, while hw needs
// the hardware to support it natively (on a typical HDA codec it does not, and
// hw: fails with "Channels count non available"). plughw comes first.
func voiceParseArecordList(out string) []voiceDevice {
	var devices []voiceDevice
	for _, line := range strings.Split(out, "\n") {
		m := voiceArecordListRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		cardName, cardLabel := m[2], m[3]
		devIdx, devLabel := m[4], strings.TrimSpace(m[5])
		label := strings.TrimSpace(strings.Join([]string{cardLabel, devLabel}, " — "))
		for _, plugin := range []string{voiceALSAPluginPlug, voiceALSAPluginRaw} {
			name := fmt.Sprintf(voiceALSADeviceFormat, plugin, cardName, devIdx)
			devices = append(devices, voiceDevice{
				ID:      voiceDeviceBackendALSA + voiceDeviceIDSeparator + name,
				Backend: voiceDeviceBackendALSA,
				Name:    name,
				Label:   fmt.Sprintf("%s [%s]", label, plugin),
			})
		}
	}
	return devices
}

const (
	// ALSA plugin prefix and the CARD=/DEV= addressing that replaces a card
	// INDEX — the whole point of a stable id.
	voiceALSAPluginPlug    = "plughw"
	voiceALSAPluginRaw     = "hw"
	voiceALSADeviceFormat  = "%s:CARD=%s,DEV=%s"
	voiceDeviceIDSeparator = ":"
)

// voiceParsePactlSources turns `pactl list short sources` output into Pulse
// devices. The listing is tab-separated (index, name, driver, spec, state);
// names come from the second column.
//
// Monitors are dropped: a monitor source carries whatever is being PLAYED on
// its sink, so listing it as a microphone would record silence and read as a
// broken mic. It stays reachable by naming it explicitly.
func voiceParsePactlSources(out string) []voiceDevice {
	var devices []voiceDevice
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[1])
		if name == "" || strings.HasSuffix(name, voicePulseMonitorSuffix) {
			continue
		}
		devices = append(devices, voiceDevice{
			ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + name,
			Backend: voiceDeviceBackendPulse,
			Name:    name,
			Label:   name,
		})
	}
	return devices
}

// voiceListDevices enumerates capture endpoints, Pulse first: the Pulse server
// is what actually owns the card on a desktop, so its devices are the ones
// that work while the desktop is running (a bare ALSA hw: device is refused
// with "Device or resource busy" in exactly that situation).
//
// A missing enumerator is not an error — a host can have arecord and no Pulse,
// or the reverse — but an enumerator that RUNS and reports nothing is
// definitive: the device list is then genuinely empty, and the whole set is
// discarded so "no capture devices" is what the caller reports. Keeping a
// zero-device backend's empty contribution would look like a success.
func voiceListDevices(run voiceCommandRunner) ([]voiceDevice, []string, error) {
	var devices []voiceDevice
	var notes []string

	if out, err := run(voicePactlBinary, voicePactlListFlag, voicePactlShortFlag, voicePactlSourcesArg); err != nil {
		notes = append(notes, fmt.Sprintf("pulse unavailable (%v)", err))
	} else {
		pulse := voiceParsePactlSources(out)
		if len(pulse) > 0 {
			// The default-source wildcard is offered first and separately: it
			// is the one id that keeps working when the user's default source
			// changes.
			def := voicePulseDefaultSource
			if d, derr := run(voicePactlBinary, voicePactlDefaultSourceArg); derr == nil {
				if s := strings.TrimSpace(d); s != "" {
					def = s
				}
			}
			devices = append(devices, voiceDevice{
				ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + voicePulseDefaultSource,
				Backend: voiceDeviceBackendPulse,
				Name:    voicePulseDefaultSource,
				Label:   fmt.Sprintf("PulseAudio default source (%s)", def),
			})
			devices = append(devices, pulse...)
		}
	}

	if out, err := run(voiceArecordBinary, voiceArecordListFlag); err != nil {
		notes = append(notes, fmt.Sprintf("alsa unavailable (%v)", err))
	} else {
		devices = append(devices, voiceParseArecordList(out)...)
	}

	if len(devices) == 0 {
		detail := ""
		if len(notes) > 0 {
			detail = " (" + strings.Join(notes, "; ") + ")"
		}
		// The literal message is built from the constants so the hint cannot
		// name a different command than the one that was actually run.
		return nil, notes, fmt.Errorf(
			"no capture devices found: `%s %s` lists nothing and no PulseAudio sources are visible%s — "+
				"on a headless or VM host there is usually no sound card at all, so record on the machine with the microphone",
			voiceArecordBinary, voiceArecordListFlag, detail)
	}
	return devices, notes, nil
}

// voiceResolveDevice picks the device --device names, or a sensible default.
//
// Resolution accepts the full ID, and also the bare recorder argument (a user
// copying "plughw:CARD=Generic_1,DEV=0" out of `arecord -L` should not have to
// learn the prefix) — but never a bare card INDEX, which is the one form that
// silently selects a different device after a renumber.
func voiceResolveDevice(id string, devices []voiceDevice, run voiceCommandRunner) (voiceDevice, error) {
	want := strings.TrimSpace(id)
	if want == "" {
		if len(devices) == 0 {
			return voiceDevice{}, fmt.Errorf("no capture devices available")
		}
		return devices[0], nil
	}
	for _, d := range devices {
		if d.ID == want {
			return d, nil
		}
	}
	for _, d := range devices {
		if d.Name == want {
			return d, nil
		}
	}
	// A monitor is deliberately absent from the enumeration, so a name miss is
	// not yet a "no such device" — ask the server once more before refusing.
	if mon, ok := voiceResolveMonitorSource(want, run); ok {
		return mon, nil
	}
	// An ALSA PLUGIN spelling is a legitimate --device argument that no
	// enumerator can list: `arecord -l` enumerates CARDS, while `default` is a
	// card-less name the ALSA library synthesises (tier 3 uses it for exactly
	// that reason). Refusing it here would contradict the help text, which
	// documents `default` as the plug chain every host has — and the two
	// spellings are the same device, so they resolve to the same entry. The
	// caller still proves it opens (voiceVerifyDevice), so an absent or busy
	// `default` fails loudly by name instead of being reported as unknown.
	if want == voiceALSADefaultName || want == voiceDeviceBackendALSA+voiceDeviceIDSeparator+voiceALSADefaultName {
		return voiceSystemDefaultDevice(), nil
	}
	// A backend-qualified miss gets the list of that backend's ids, so the
	// error is actionable rather than a wall of every device.
	var valid []string
	for _, d := range devices {
		valid = append(valid, d.ID)
	}
	sort.Strings(valid)
	return voiceDevice{}, fmt.Errorf("unknown device %q — available: %s", want, strings.Join(valid, ", "))
}

// voiceIsMonitorSource reports whether a Pulse source name is a sink's monitor.
// A monitor carries whatever is PLAYING on its sink, so it is never OFFERED as
// a microphone — but it is a recordable source, and it is the deterministic
// input a capture rig feeds a fixture into.
func voiceIsMonitorSource(name string) bool {
	return strings.HasSuffix(strings.TrimSpace(name), voicePulseMonitorSuffix)
}

// voiceResolveMonitorSource resolves an explicitly named Pulse monitor — the
// one device enumeration withholds on purpose. --list-devices excludes monitors
// because offering one as a microphone records silence and reads as a broken
// mic, but refusing to RESOLVE one makes a documented route impossible: the
// E2E capture rig's source is a monitor, and a user who names it has been
// unambiguous. The name is accepted only if the server still lists it (the same
// liveness question every other tier answers) and the name really is a monitor
// — a non-monitor miss keeps the plain "unknown device" error.
func voiceResolveMonitorSource(id string, run voiceCommandRunner) (voiceDevice, bool) {
	prefix := voiceDeviceBackendPulse + voiceDeviceIDSeparator
	name := strings.TrimSpace(id)
	if strings.HasPrefix(name, prefix) {
		name = strings.TrimSpace(strings.TrimPrefix(name, prefix))
	}
	if !voiceIsMonitorSource(name) {
		return voiceDevice{}, false
	}
	out, err := run(voicePactlBinary, voicePactlListFlag, voicePactlShortFlag, voicePactlSourcesArg)
	if err != nil {
		return voiceDevice{}, false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 {
			continue
		}
		if strings.TrimSpace(fields[1]) != name {
			continue
		}
		return voiceDevice{
			ID:      prefix + name,
			Backend: voiceDeviceBackendPulse,
			Name:    name,
			Label:   name + voiceMonitorLabelSuffix,
		}, true
	}
	return voiceDevice{}, false
}

// voiceRecorderCommand renders the exact argv for a device. Every field comes
// from the constants above, so the pinned contract (rate/channels/format) is
// identical across backends by construction, not by review.
func voiceRecorderCommand(dev voiceDevice, seconds int) (string, []string) {
	switch dev.Backend {
	case voiceDeviceBackendPulse:
		return voiceParecordBinary, []string{
			fmt.Sprintf(voiceParecordDeviceFormat, dev.Name),
			voiceParecordRawFlag,
			fmt.Sprintf(voiceParecordRateFormat, voiceCaptureSampleRate),
			fmt.Sprintf(voiceParecordChannelsFormat, voiceCaptureChannels),
			fmt.Sprintf(voiceParecordFormatFormat, voiceParecordFormatS16LE),
			fmt.Sprintf(voiceParecordLatencyFormat, voiceParecordLatencyMs),
		}
	default:
		return voiceArecordBinary, []string{
			voiceArecordDeviceFlag, dev.Name,
			voiceArecordFormatFlag, voiceArecordFormatS16LE,
			voiceArecordRateFlag, strconv.Itoa(voiceCaptureSampleRate),
			voiceArecordChannelsFlag, strconv.Itoa(voiceCaptureChannels),
			voiceArecordTypeFlag, voiceArecordRawType,
			voiceArecordQuietFlag,
			voiceArecordDurationFlag, strconv.Itoa(seconds),
		}
	}
}

// voiceRecord captures `seconds` of audio and returns raw PCM16 — the real
// recording, where "no audio at all" is a device failure.
func voiceRecord(dev voiceDevice, seconds int) ([]byte, error) {
	data, err := voiceCaptureAttempt(dev, seconds)
	if err != nil {
		return nil, err
	}
	if len(data) < voiceMinCaptureBytes {
		return nil, fmt.Errorf("%s produced no audio from %q — the device is reachable but silent: "+
			"check the capture level is not muted", voiceRecorderNameFor(dev), dev.ID)
	}
	return data, nil
}

// voiceRecorderNameFor is the recorder binary a device is captured with, named
// once so a message and the spawn cannot disagree.
func voiceRecorderNameFor(dev voiceDevice) string {
	binary, _ := voiceRecorderCommand(dev, voiceClientDefaultSeconds)
	return binary
}

// voiceCaptureAttempt runs the recorder ONCE and returns whatever audio it
// produced, WITHOUT deciding whether that is enough. The two callers disagree on
// that on purpose: the real recording treats "no audio" as a device failure,
// while the policy's liveness probe treats it as a note (a muted input is a
// real device — the failure-mode matrix owns that case).
//
// The stop condition differs per recorder: arecord is told the duration (-d),
// while parecord has no equivalent and is killed at the deadline — so for
// parecord a signal at the deadline is the normal exit, not an error, as long
// as audio arrived.
//
// This is also where a device that DISAPPEARED between enumeration and capture
// is caught: the recorder's own open() fails, and the refusal is turned into the
// re-list advice by voiceMissingDeviceHint.
func voiceCaptureAttempt(dev voiceDevice, seconds int) ([]byte, error) {
	return voiceCaptureStreaming(dev, seconds)
}

// voiceCaptureSilenceFloor is the ONE statement of "this is silence" that both
// readers share (task 01a1188c): the live staged reader, which buffers a window
// and cuts it, and the report's ledger, which describes what it did. It is a
// TYPE rather than a loose float so the derivation (dBFS → PCM16 amplitude) cannot
// be re-typed at a call site with a different reference (Rule 1/14).
type voiceCaptureSilenceFloor struct {
	// amplitude is the largest absolute sample value still called silence.
	amplitude float64
}

// newVoiceCaptureSilenceFloor derives the floor from the named dBFS constant.
func newVoiceCaptureSilenceFloor() voiceCaptureSilenceFloor {
	return voiceCaptureSilenceFloor{amplitude: voicePCM16AmplitudeAtDBFS(voiceCaptureSilenceFloorDBFS)}
}

// isSilent reports whether a whole chunk is quiet enough to be called silence.
//
// AN UNCERTAIN CHUNK IS NOT SILENCE: the test is the chunk's PEAK amplitude
// against the floor, so one sample loud enough to be speech rescues the whole
// chunk — the direction that can only fail to find a boundary, never invent one.
func (w voiceCaptureSilenceFloor) isSilent(chunk []byte) bool {
	samples := len(chunk) / voiceCaptureBytesPerSample
	if samples == 0 {
		return false
	}
	peak := 0.0
	for i := 0; i < samples; i++ {
		sample := float64(pcm16LEAt(chunk, i))
		if sample < 0 {
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	return peak <= w.amplitude
}

// pcm16LEAt reads sample i of a raw PCM16 little-endian chunk as int16. Callers
// derive i from len(chunk), so the slice is always in range.
func pcm16LEAt(chunk []byte, i int) int16 {
	return int16(binary.LittleEndian.Uint16(chunk[i*voiceCaptureBytesPerSample:]))
}

// voiceCaptureStreaming captures ONE window: the recorder runs for the window's
// bound, or closes early when the window is judged finished (whichever comes
// first, below).
//
// THE WINDOW'S CLOCK STARTS AT THE DEVICE'S FIRST SAMPLE, not at the spawn (task
// 01a11853). The defect that closed: the window used to be counted from spawn, so
// the operator paid the device's own startup BEFORE the seconds they asked for —
// and, in the flat-grace shape that replaced, voiceCaptureGraceSec of dead air on
// top of it. Here the bytes returned are the FIRST `seconds` of audio the device
// produced, so the startup cost is paid once, at the device's real cost, and is
// never subtracted from the window.
//
// THE SLOW-DEVICE PROTECTION IS UNCHANGED: voiceCaptureGraceSec remains the
// budget for reaching that first sample (the deadline still spans
// seconds+voiceCaptureGraceSec), so a Bluetooth headset that takes 8 s to open
// keeps working — it simply stops making every other round wait for it.
//
// AND THE WINDOW CAN NOW END EARLY, on the recording itself (task 01a1188c):
// voicePCM16AmplitudeAtDBFS converts a level in dBFS to the PCM16 amplitude it
// names, so the silence rule is STATED in the unit the measurement is made in
// (dBFS) and COMPARED in the unit the samples carry.
func voicePCM16AmplitudeAtDBFS(dbfs float64) float64 {
	return math.Pow(voicePCM16FullScaleDecades, dbfs/voicePCM16FullScaleDecibels) * voicePCM16FullScale
}

// voiceCaptureStreaming captures ONE window: the recorder runs for the window's
// bound, or closes early when the window is judged finished (whichever comes
// first, below).
//
// THE WINDOW'S CLOCK STARTS AT THE DEVICE'S FIRST SAMPLE, not at the spawn (task
// 01a11853). The defect that closed: the window used to be counted from spawn, so
// the operator paid the device's own startup BEFORE the seconds they asked for —
// and, in the flat-grace shape that replaced, voiceCaptureGraceSec of dead air on
// top of it. Here the bytes returned are the FIRST `seconds` of audio the device
// produced, so the startup cost is paid once, at the device's real cost, and is
// never subtracted from the window.
//
// THE SLOW-DEVICE PROTECTION IS UNCHANGED: voiceCaptureGraceSec remains the
// budget for reaching that first sample (the deadline still spans
// seconds+voiceCaptureGraceSec), so a Bluetooth headset that takes 8 s to open
// keeps working — it simply stops making every other round wait for it.
//
// AND THE WINDOW CAN NOW END EARLY, on the recording itself (task 01a1188c):
// voiceCaptureSampleWindow tracks the window's own peak and its trailing run of
// near-silence, and a run of voiceCaptureTrailingSilenceMS CLOSES the window once
// the window is known to hold live audio. That is what takes the round's cut off
// the clock and puts it where the operator stopped speaking — the difference
// between a continuous utterance being carried across rounds and a slice of it
// falling into the gap where no recorder is open. A recorder whose samples do not
// arrive live (see voiceCaptureSampleWindow.bursty) never enters the rule and
// keeps the fixed bound exactly as before.
func voiceCaptureStreaming(dev voiceDevice, seconds int) ([]byte, error) {
	binary, args := voiceRecorderCommand(dev, seconds)
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(seconds+voiceCaptureGraceSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	// THE RECORDER LEADS ITS OWN GROUP — see voiceRecorderSysProcAttr: never the
	// caller's, or a group signal aimed at us takes the operator's terminal.
	cmd.SysProcAttr = voiceRecorderSysProcAttr()
	cmd.Env = voiceRecorderEnv()
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot read %s's audio from %q: %w", binary, dev.ID, err)
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s failed on %q: %w", binary, dev.ID, err)
	}

	want := voiceBytesPerSeconds(seconds)
	pcm := make([]byte, 0, want)
	buf := make([]byte, voiceCaptureReadChunk)
	firstSample := time.Time{}
	for len(pcm) < want {
		if ctx.Err() != nil {
			break
		}
		read, rerr := stdout.Read(buf)
		if read > 0 {
			if firstSample.IsZero() {
				// THE WINDOW CLOCK STARTS HERE — at the first sample, never at
				// the spawn. Everything before it was the device opening.
				firstSample = time.Now()
			}
			// EVERY SAMPLE IS KEPT, in the order the recorder produced it.
			pcm = append(pcm, buf[:read]...)
			continue
		}
		if rerr != nil {
			break
		}
	}
	if len(pcm) > want {
		pcm = pcm[:want]
	}

	// The recorder is ALWAYS reaped, on every path out: a failure that left it
	// running would leave the microphone held by a process nobody owns (Rule 15).
	runErr := stopVoiceRecorder(cmd, stdout)
	if runErr != nil && !voiceCaptureContextSensitiveError(runErr, errBuf.String()) &&
		len(pcm) < voiceMinCaptureBytes {
		return nil, voiceRecorderFailure(binary, dev, runErr, errBuf.String(), time.Since(start))
	}

	if len(pcm) < voiceMinCaptureBytes {
		// Nothing arrived at all: the device opened but never produced a
		// sample. Stated as a DEVICE condition (a muted or dead input), never
		// as a failed transcription.
		return nil, fmt.Errorf("%s produced no audio from %q (%s) — the device is reachable but silent: "+
			"check the capture level is not muted",
			binary, dev.ID, time.Since(start).Round(time.Millisecond))
	}
	return pcm, nil
}

// voiceRecorderSysProcAttr is the ONE process-group policy for a spawned
// recorder: the child leads a group of its OWN (Setpgid), never the caller's.
//
// WHY THIS IS NOT COSMETIC (measured 2026-10-22, task 01a163df). A recorder
// spawned without Setpgid inherits the CALLER's process group. Under `gopi
// chat` the caller is the chat process, which is its own group leader — so
// every `parecord` became a member of the group the chat itself lives in.
// Anything that later addresses that group addresses the operator's terminal
// too: `internal/tools`' killChildTree sends SIGKILL to `-pgid`, and its
// `pgid == pid` guard says only "the CHILD is a leader" — it cannot tell that
// the group it is about to signal is the CALLER's. Three such recorders, one
// 10m42s old, were measured in a live chat's group.
//
// THE FIX IS STRUCTURAL, NOT SIGNAL-HANDLING: SIGKILL cannot be caught, so the
// only durable answer is that the hazard never becomes addressable — the
// recorder sits in a group nobody else is in, and both parties (the chat that
// spawned it and any tool that signals groups) are outside it. See also
// killChildTree's os.Getpgrp() guard, the second half of this defence.
func voiceRecorderSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// stopVoiceRecorder ends a recorder whose window is complete: the stop signal
// immediately (a recorder with its own duration flag has already exited), then
// voiceCaptureSignalGraceSec for it to die before the caller's context kills it.
//
// THE SIGNAL IS THE NORMAL END for parecord, whose argv has no duration flag:
// closing the window is done BY stopping the process. The exit status it
// reports (SIGINT ⇒ "signal: interrupt") is the caller's to classify — see
// voiceCaptureContextSensitiveError.
func stopVoiceRecorder(cmd *exec.Cmd, stdout io.ReadCloser) error {
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// THE STOP IS SENT AT THE MOMENT THE WINDOW IS FULL — no wait first.
	// A recorder that carries its own duration (arecord's -d) has already
	// exited and the first select sees it; one that does not (parecord) is
	// waiting for exactly this signal, because its window is closed BY being
	// stopped. Waiting a grace period BEFORE signalling would therefore charge
	// every parecord round for a process that is doing nothing wrong, and
	// (measured 2026-10-08, task 01a11853) the earlier `time.Duration(
	// voiceCaptureCloseGraceSec) * time.Second` double conversion turned a
	// 500 ms intent into 5e8 s, so the wait ran to the caller's context
	// deadline instead — that is where the observed seconds+voiceCaptureGraceSec
	// round length came from.
	select {
	case err := <-exited:
		return err
	default:
	}
	if cmd.Process != nil {
		// SIGINT, not SIGTERM: the recorded signal name is what the failure
		// text prints, and this is a STOP (ask it to finish its buffer), not a
		// failure. Closing the read side keeps a full pipe from wedging it.
		_ = cmd.Process.Signal(os.Interrupt) //stylegate:ignore SIGINT is a STOP request, not a failure: the caller learns the outcome from the exited channel and the printed signal name
	}
	_ = stdout.Close() //stylegate:ignore closing the read side is what unwedges a full pipe; the record's own error arrives on the exited channel
	select {
	case err := <-exited:
		return err
	case <-time.After(voiceCaptureSignalGraceSec):
	}

	// THE GRACE EXPIRED: THE RECORDER IS OURS TO TAKE DOWN, so take it down.
	//
	// This branch used to only RETURN an error, on the assumption that "the
	// caller's context kills it". Two things are wrong with that, both measured
	// (2026-10-22, task 01a163df): the context kill is `cmd.Process.Kill()` on
	// THIS pid only (exec.CommandContext's own goroutine), which fires at the
	// CALLER's deadline — and the caller may have none; and `exited` is read by
	// THIS function alone, so returning without consuming it means the child is
	// never `wait()`ed. The result was visible in the process table: three
	// `parecord` in `gopi chat`'s own group, one of them 10m42s old, all
	// `<defunct>` — unreaped, still holding the microphone.
	//
	// SIGKILL, not SIGINT/SIGTERM: SIGINT was already sent above and this branch
	// exists BECAUSE it was ignored. SIGKILL cannot be caught, so this is the
	// last signal available; carrying an audio buffer is no longer a
	// consideration for a process that declined to finish it in
	// voiceCaptureSignalGraceSec.
	var killErr error
	if cmd.Process != nil {
		killErr = cmd.Process.Kill()
	}
	select {
	case waitErr := <-exited:
		// REAPED: the channel read above IS the wait, and it is why no zombie is
		// left behind on this path (Rule 15 — a resource we opened, we close).
		if waitErr == nil {
			// It exited on its own at the boundary; the grace error would be a
			// false claim (nothing was killed), so report the truth.
			return nil
		}
		return fmt.Errorf(voiceCaptureStopKilledNote+": %w", voiceCaptureSignalGraceSec, waitErr)
	case <-time.After(voiceCaptureForceKillGraceSec):
		// Even SIGKILL could not retire it: a process blocked in an
		// uninterruptible device read. Named by pid rather than reported as a
		// successful stop — the process is still there, and the operator is the
		// only one who can act on a wedged capture device.
		stuck := fmt.Errorf(voiceCaptureStillAliveNote, cmd.Process.Pid,
			voiceCaptureForceKillGraceSec)
		if killErr != nil {
			// The Kill call's own reason is not swallowed (Rule 15): an EPERM or
			// an already-reaped pid reads very differently from a wedged device.
			return fmt.Errorf("%w: %w", stuck, killErr)
		}
		return stuck
	}
}

// voiceCaptureContextSensitiveError reports whether a recorder's exit was
// CAUSED by us closing its window, rather than the recorder refusing to work.
//
// The distinction cannot be made on the exit CODE (measured 2026-10-08).
// arecord and ffmpeg both exit 1 when their stdout goes away, and parecord
// exits on the signal it was sent to end the window — so the classification
// reads the error SHAPE (a killed process, a signal) and the recorder's own
// stderr signature, and is deliberately narrow: everything it does not
// recognise stays a real failure.
func voiceCaptureContextSensitiveError(err error, stderr string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if strings.Contains(err.Error(), voiceCaptureKilledMarker) {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return true
		}
	}
	for _, marker := range voiceCapturePipeMarkers {
		if strings.Contains(stderr, marker) {
			return true
		}
	}
	return false
}

// voiceRecorderFailure renders a recorder refusal with the two hints a measured
// failure mode is worth: a device held by another process, and a device that
// went away. It is the ONE place those hints are built, so both capture shapes
// report the same advice for the same cause.
func voiceRecorderFailure(binary string, dev voiceDevice, err error, stderr string, elapsed time.Duration) error {
	msg := strings.TrimSpace(stderr)
	hint := voiceRecorderHint(msg)
	if msg == "" {
		return fmt.Errorf("%s failed on %q (%s): %w%s", binary, dev.ID,
			elapsed.Round(time.Millisecond), err, hint)
	}
	return fmt.Errorf("%s failed on %q (%s): %w — %s%s", binary, dev.ID,
		elapsed.Round(time.Millisecond), err, lastLines(msg, 3), hint)
}

// voiceRecorderEnv is the environment a recorder child is spawned with: this
// process's, with the MESSAGE LOCALE pinned (Rule 2: the policy is read from
// its one owner, internal/tools.ChildLocaleEnv).
//
// WHY A RECORDER CHILD NEEDS A LOCALE AT ALL. This process READS its stderr,
// and the signatures it reads are English literals — two of them are glibc
// strerror messages:
//
//	voiceCaptureBrokenPipeMarker        = "Broken pipe"     (EPIPE)
//	voiceErrBusyMarker                  = "Device or resource busy" (EBUSY)
//
// gettext translates both. An UNPINNED child (cmd.Env == nil) inherits this
// process's message directories, so `stdout: Broken pipe` becomes
// `stdout: 断开的管道` and the marker matches NOTHING:
//
//   - voiceCaptureContextSensitiveError stops recognising the read side we
//     closed OURSELVES, so a normal window close is reclassified as a recorder
//     refusing to work — a Rule 15 silent failure, where the wrong CAUSE is
//     reported for the right observation;
//   - voiceRecorderFailure's busy hint (and voiceMissingDeviceHint's) is lost
//     on the refusal path, so the operator gets no advice for the one cause
//     that has an action.
//
// MEASURED against the REAL recorders on this host, 2026-10-12 (not inferred):
//
//	$ arecord -D pulse … | head -c 3200 >/dev/null            # LANG=C
//	stdout: Broken pipe
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D pulse … | head -c 3200 >/dev/null
//	stdout: 断开的管道
//
//	$ parecord --raw | head -c 3200 >/dev/null                # LANG=C
//	write() failed: Broken pipe
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 parecord --raw | head -c 3200 >/dev/null
//	write() 失败：断开的管道
//
// The parecord pair is the one that decides this: arecord dies of SIGPIPE on a
// closed read side and is caught by the signal branch BEFORE any marker is
// consulted, while parecord reports `write() failed` and EXITS — so for gopi's
// Pulse backend the marker loop is the only branch that can recognise the read
// side, and under an unpinned locale it matches nothing.
//
//	$ arecord -D plughw:1,0 … /dev/null                       # LANG=C
//	arecord: main:831: audio open error: Device or resource busy
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D plughw:1,0 … /dev/null
//	arecord: main:831: 音乐打开错误： 设备或资源忙
//
// The pin is STRIP-THEN-APPEND, and the strip is what is load-bearing: a
// duplicated entry in a child's environment is decided by the READER, not by
// one rule — glibc's gettext catalogue path and Go's os.Getenv take the FIRST
// copy while dash and bash take the LAST (ChildLocaleDuplicateKeyRule,
// internal/tools/git_child_locale.go; measured 2026-10-16, and `env K=v1 K=v2`
// measures coreutils `env`, which collapses the pair before the reader sees
// it). `append(os.Environ(), "LC_ALL=C")` therefore leaves the inherited
// translation winning for whatever reader happens to look first, and is not a
// pin at all. The child is given exactly ONE entry instead — the property that
// holds for every reader, in every order. Both halves — the strip list and the
// pinned value — come from internal/tools, never re-typed here (Rule 13/14).
func voiceRecorderEnv() []string {
	return tools.ChildLocaleEnv()
}

// voiceRecorderHint is the advice a measured recorder refusal is worth: a device
// held by another process, or a device that went away. It is ONE function because
// two shapes now print the same child's stderr — the one-shot capture's refusal
// (voiceRecorderFailure) and the staged capture's diagnostic
// (voiceStagedRecorderNote) — and the same cause must not acquire two different
// instructions depending on which shape happened to meet it (Rule 14).
func voiceRecorderHint(msg string) string {
	if strings.Contains(msg, voiceErrBusyMarker) {
		return fmt.Sprintf(" (the device is held by another process — on a desktop that is the sound "+
			"server, so pick a %s%s device from --list-devices)",
			voiceDeviceBackendPulse, voiceDeviceIDSeparator)
	}
	return voiceMissingDeviceHint(msg)
}

// voiceStagedRecorderNote renders what the STAGED capture's recorder said, for a
// refusal that would otherwise report only "no audio arrived". It reuses the
// one-shot path's hint builder (voiceRecorderHint above), so the operator reads
// the same advice for the same cause whichever capture shape found it.
//
// IT QUOTES THE TAIL, NOT THE STREAM: stderr accumulates for the whole life of
// the mode, and a refusal wants the lines that explain the end. voiceStagedRecorderNoteTail
// is small on purpose — three lines, the same order voiceRecorderFailure prints.
func voiceStagedRecorderNote(stderr string) string {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		// NOTHING TO ADD — and that is a RESULT, not a gap: a recorder that exited
		// silently has told us nothing, and a "the recorder said: " with nothing
		// after it would be a statement that a diagnostic exists.
		return ""
	}
	return fmt.Sprintf("the recorder said: %s%s", lastLines(msg, voiceStagedRecorderNoteTail), voiceRecorderHint(msg))
}

// voiceCapturePipeMarkers are the recorder stderr signatures that mean "the
// read side went away", not "the device refused". Both were measured on this
// host (2026-10-08) against a window closed under a running recorder; a
// signature that is not here stays a real failure, which is the point.
var voiceCapturePipeMarkers = []string{voiceCaptureBrokenPipeMarker, voiceCaptureConnectionFailureMarker}

// voiceMissingDeviceHint turns the recorder refusals that mean "that device is
// not there" into the one action that fixes it. The signatures are the three
// measured on HP against a correctly-spelled but absent device, plus the plain
// "No such device" ALSA prints for a missing card name.
func voiceMissingDeviceHint(msg string) string {
	for _, marker := range voiceErrMissingMarkers {
		if strings.Contains(msg, marker) {
			return fmt.Sprintf(" (no capture device by that name — it may have been unplugged or taken: "+
				"run `%s %s` to see what is there now)", voiceCLIVerb, voiceListDevicesFlag())
		}
	}
	return ""
}

// voiceBytesPerSeconds is the byte count of `seconds` of the capture contract
// (16 kHz mono PCM16) — the window's length expressed in the unit the streaming
// read loop counts in, so "the window is full" and "trim to the window" cannot
// disagree (Rule 13: one definition of the contract's arithmetic).
func voiceBytesPerSeconds(seconds int) int {
	return seconds * voiceCaptureSampleRate * voiceCaptureBytesPerSample
}

// voiceTrimToSeconds drops the tail beyond `seconds`. A recorder can overrun
// (parecord before its kill, arecord rounding up) and the far side transcribes
// whatever it is given — extra silence costs latency for no text.
func voiceTrimToSeconds(pcm []byte, seconds int) []byte {
	want := voiceBytesPerSeconds(seconds)
	if seconds <= 0 || len(pcm) <= want {
		return pcm
	}
	// `want` is a whole number of samples by construction, so the cut is on a
	// sample boundary.
	return pcm[:want]
}

// voicePCM16ToWAV wraps raw PCM16 in a canonical WAV container so the HTTP
// route can hand the server an ordinary file (the server preserves the
// extension and the engine's decoder sniffs the format).
func voicePCM16ToWAV(pcm []byte) []byte {
	return voiceBuildWAV(pcm, voiceWAVFormatPCM, voiceCaptureBitsPerSample)
}

// voicePCM16ToFloat32 converts PCM16 to the raw f32 the server's stdio shape
// reads (voiceServeStdio). Dividing by full scale maps int16 onto [-1,1);
// anything else is a gain error that survives into the transcript.
func voicePCM16ToFloat32(pcm []byte) []byte {
	out := make([]byte, 0, len(pcm)/voiceCaptureBytesPerSample*voiceFloat32Bytes)
	var buf [voiceFloat32Bytes]byte
	for i := 0; i+voiceCaptureBytesPerSample <= len(pcm); i += voiceCaptureBytesPerSample {
		s := int16(binary.LittleEndian.Uint16(pcm[i:]))
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(float32(s)/voicePCM16FullScale))
		out = append(out, buf[:]...)
	}
	return out
}

// voiceForwardHTTP posts the recording to an already-resolved transcribe URL
// (see voiceTranscribeURL) and returns the transcript.
func voiceForwardHTTP(url string, wav []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile(voiceMultipartField, voiceUploadFilename)
	if err != nil {
		return "", fmt.Errorf("build upload: %w", err)
	}
	if _, err := part.Write(wav); err != nil {
		return "", fmt.Errorf("build upload: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("build upload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return "", fmt.Errorf("bad server URL %q: %w", url, err)
	}
	req.Header.Set(voiceHeaderContentType, mw.FormDataContentType())

	client := &http.Client{Timeout: voiceClientHTTPTimeoutSec * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach transcriber at %s: %w %s", url, err, voiceStartServerHint(url))
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, voiceMaxAudioBytes))
	if err != nil {
		return "", fmt.Errorf("read response from %s: %w", url, err)
	}

	var parsed struct {
		Text      string `json:"text"`
		Error     string `json:"error"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", fmt.Errorf("transcriber at %s returned no JSON (HTTP %d): %s",
			url, resp.StatusCode, truncateForError(payload))
	}
	if resp.StatusCode != http.StatusOK || parsed.Error != "" {
		if parsed.Error == "" {
			parsed.Error = strings.TrimSpace(string(payload))
		}
		return "", fmt.Errorf("transcriber at %s failed (HTTP %d): %s", url, resp.StatusCode, parsed.Error)
	}
	fmt.Fprintf(os.Stderr, "[voice] transcribed in %.1fs\n", float64(parsed.ElapsedMS)/voiceMSPerSecond)
	return parsed.Text, nil
}

// voiceStartServerHint names the server to start for the URL this client just
// failed to reach — the address comes from the URL THAT FAILED, never from the
// default.
//
// Before this the hint was the literal "(start it with `gopi tool voice_input
// --server --listen :7788`)", printed for EVERY target: a client that asked
// for another port was told to start a server somewhere else, so the advice
// named an address this run never used — exactly the shape that reads as "it
// is still using the previous run's server" (measured 2026-10-05: `--client
// --out FILE --server-url http://127.0.0.1:7999` against a dead 7998 printed
// the :7788 default one line under "cannot reach transcriber at
// http://127.0.0.1:7998/transcribe"). The hint is advice about THIS run's
// target (Rule 15: the message must not contradict the state it reports), so it
// quotes that target. Only a target carrying no port falls back to the port the
// server's own default listen address uses (voiceDefaultListenPort), and a
// non-local host is told to run the server THERE instead of being pointed at
// this machine's loopback.
func voiceStartServerHint(url string) string {
	u, err := neturl.Parse(url)
	if err != nil || u.Hostname() == "" {
		return fmt.Sprintf("(start a transcriber for this target: `gopi tool voice_input --server --listen %s`)",
			voiceDefaultListen)
	}
	port := u.Port()
	if port == "" {
		port = voiceDefaultListenPort
	}
	host := u.Hostname()
	if host != voiceLoopbackHost && host != voiceLocalhostName {
		return fmt.Sprintf("(no transcriber is listening at %s — run `gopi tool voice_input --server --listen :%s` ON that host, then retry)",
			host, port)
	}
	return fmt.Sprintf("(start it with `gopi tool voice_input --server --listen %s%s%s`)",
		voiceLoopbackHost, voiceHostPortSeparator, port)
}

// voiceTranscribeURL resolves and validates the transcriber endpoint. It is
// called BEFORE the microphone is opened (a typo discovered after the user has
// spoken wastes the sentence), so it parses the whole URL — a bad port must be
// caught here, not by http.NewRequest later.
//
// The result is the final endpoint; callers must not join the path again.
func voiceTranscribeURL(target string) (string, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return "", fmt.Errorf("empty server URL")
	}
	if !strings.HasPrefix(t, voiceSchemeHTTP) && !strings.HasPrefix(t, voiceSchemeHTTPS) {
		return "", fmt.Errorf("server URL must start with %s or %s, got %q",
			voiceSchemeHTTP, voiceSchemeHTTPS, t)
	}
	u, err := neturl.Parse(t)
	if err != nil {
		return "", fmt.Errorf("bad server URL %q: %w", target, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("server URL %q has no host", target)
	}
	if port := u.Port(); port != "" {
		if _, err := strconv.Atoi(port); err != nil {
			return "", fmt.Errorf("bad port in server URL %q", target)
		}
	}
	// Assigning Path (not concatenating strings) is what keeps a target that
	// already ends in "/" from producing "//transcribe".
	u.Path = strings.TrimRight(u.Path, "/") + voiceTranscribePath
	return u.String(), nil
}

const (
	voiceSchemeHTTP  = "http://"
	voiceSchemeHTTPS = "https://"
	// voiceLocalhostName is the other spelling of the loopback host the client
	// treats as "this machine" when it advises where to start a server
	// (voiceStartServerHint).
	voiceLocalhostName     = "localhost"
	voiceHeaderContentType = "Content-Type"
	voiceFloat32Bytes      = 4
	voiceMSPerSecond       = 1000.0
)

// truncateForError keeps an error message readable when a non-JSON body comes
// back (an HTML error page from a proxy, say).
func truncateForError(b []byte) string {
	const maxChars = voiceErrorBodyChars
	s := strings.TrimSpace(string(b))
	if len(s) > maxChars {
		return s[:maxChars] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

const voiceErrorBodyChars = 200

// The debug file's create semantics. O_EXCL is what makes "already exists" a
// refusal (voiceValidateOutPath checks it too, but the flag is the atomic form:
// a file created between the parse and the write must not be truncated either).
const (
	voiceOutCreateFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	voiceOutFileMode    = 0o644
)

// runVoiceClient is the --client entry point. Device choice is delegated to
// voiceResolveSelection (see cli_voice_device_policy.go) and the chosen device
// is reported on every run.
func runVoiceClient(opts *voiceOptions) {
	// Validate the target BEFORE touching the microphone: discovering a typo
	// in --server-url after the user has already spoken wastes the recording
	// (and the sentence) for a mistake that was knowable up front.
	transcribeURL := ""
	if !opts.listDevices && !opts.stdio {
		url, err := voiceTranscribeURL(opts.target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		transcribeURL = url
	}

	// --audio-source: the capture reads a FILE instead of a device (task
	// 01a11f13). IT IS HANDLED BEFORE THE DEVICE IS EVEN ENUMERATED, and the
	// ORDER IS THE FEATURE: a file run must enumerate nothing, probe nothing and
	// spawn no recorder, so that it runs on a host with no sound card at all —
	// which is the whole point of the seam. Placed after `voiceListDevices`, the
	// flag works but the run still pays a pulse socket probe, an ALSA open
	// attempt and their warnings on the way (measured on this host:
	// `pulse unavailable …`, `device alsa:default is not reachable …` printed
	// ahead of the file's own line); on a host with no device at all it is worse
	// than noise — an enumerator that reports nothing is an ERROR, so the run
	// would refuse before ever reaching the file. Everything after the audio is
	// in hand (keep it, forward it, type it) is the SAME code as the device path
	// below, so the two cannot drift.
	if opts.audioSource != "" {
		src := voiceFileSourceFor(opts.audioSource, voiceSourceImmediate)
		pcm, err := voiceCapturePCM(src, opts.seconds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "[voice] --%s %s (no capture device is opened)\n",
			voiceFlagAudioSource, opts.audioSource)
		voiceDeliverCapture(opts, pcm, transcribeURL)
		return
	}

	devices, notes, err := voiceListDevices(voiceRunCommand)
	if err != nil {
		for _, n := range notes {
			fmt.Fprintf(os.Stderr, "[voice] %s\n", n)
		}
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
		os.Exit(1)
	}
	for _, n := range notes {
		fmt.Fprintf(os.Stderr, "[voice] %s\n", n)
	}

	if opts.listDevices {
		voicePrintDevices(devices)
		return
	}

	sel, selNotes, err := voiceResolveSelection(opts.device, devices, voiceRunCommand, voiceCaptureAttempt)
	for _, n := range selNotes {
		fmt.Fprintf(os.Stderr, "[voice] %s\n", n)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[voice] %s\n", voiceDescribeCapture(sel, opts.seconds))
	pcm, err := voiceRecord(sel.Device, opts.seconds)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
		os.Exit(1)
	}
	voiceDeliverCapture(opts, pcm, transcribeURL)
}

// voiceDeliverCapture does everything a capture's audio is for, ONCE: report how
// much arrived, keep the recording if a flag asked for one, hand the audio to the
// chosen destination (stdout as f32 for --stdio, or a POST to the transcriber),
// type it if --self asked, and print the transcript.
//
// IT IS ONE FUNCTION BECAUSE IT IS ONE CONTRACT. The device path and the
// --audio-source path capture differently and do everything else identically;
// duplicating these forty lines is how "the file run forgot to write --out"
// becomes a bug nobody looks for (Rule 14: one implementation of one behaviour).
func voiceDeliverCapture(opts *voiceOptions, pcm []byte, transcribeURL string) {
	fmt.Fprintf(os.Stderr, "[voice] captured %d bytes (%.1fs of %dHz mono PCM16)\n",
		len(pcm), voicePCMSeconds(pcm), voiceCaptureSampleRate)

	// Keep the recording BEFORE forwarding anything. The whole point of the
	// debug file is the failing run: a transcriber that is down, a --server-url
	// typo, or a forward that returns an error used to consume the audio with
	// nothing to inspect. Writing first makes the WAV survive every one of
	// those (and it is what makes "capture on host A, debug on host B" a
	// one-liner). A write failure is fatal and NOT ignored (Rule 15): the
	// operator asked for evidence, and continuing without it would report a
	// success they cannot check.
	if opts.out != "" {
		pcmFile, err := voiceWriteCaptureFile(opts.out, pcm)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "[voice] wrote %d bytes of WAV (%.1fs) to %s\n",
			len(pcmFile), voicePCMSeconds(pcm), opts.out)
	}

	if opts.stdio {
		// The server's stdio shape reads raw f32 (voiceServeStdio), so the
		// conversion belongs here, on the capture side.
		if _, err := os.Stdout.Write(voicePCM16ToFloat32(pcm)); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: write stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}

	text, err := voiceForwardHTTP(transcribeURL, voicePCM16ToWAV(pcm))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
		os.Exit(1)
	}
	// --self: TYPE the transcript into this process's window instead of only
	// printing it. stdout keeps the text too, so an operator running from a
	// terminal still sees exactly what was typed (and a caller capturing the
	// command's output still gets it) — the injection is an ADDITION, never a
	// replacement that would make the transcript unreadable to a script.
	if opts.self {
		if err := voiceTypeIntoOwnWindow(opts, text, transcribeURL); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println(text)
}

// voiceTypeIntoOwnWindow types a finished transcript into the window that hosts
// this process, through the SAME addressing + injection path `gopi tool act type
// --self` uses (shot.XActuator.TypeIntoSelfWindow) — one implementation, so the
// two verbs cannot drift.
//
// The transcript is typed VERBATIM: escape decoding belongs to `act type`'s flag
// grammar (where the shell hands over backslashes it did not interpret), not to
// text a speech model produced. The text is NOT submitted — no Return is
// appended — because the review-before-send property is the whole reason voice
// input is a CLI verb rather than a tool (the usage text says so at length): one
// character in sixty is wrong, and a wrong technical term costs more than a
// wrong character.
func voiceTypeIntoOwnWindow(opts *voiceOptions, text, transcribeURL string) error {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return fmt.Errorf("--%s: the transcriber at %s returned no text, so there is nothing to type (stdout is unaffected)",
			voiceFlagSelf, transcribeURL)
	}
	// Refuse BEFORE opening the display when the transcript cannot be pressed:
	// the X11 layer would refuse mid-way, after some characters were already on
	// screen. Checking first makes the refusal atomic and says which rune did it.
	if err := shot.TypableText(text); err != nil {
		return fmt.Errorf("--%s: %w (the transcript contains a character this client cannot press — the text is still on stdout, so it can be copied by hand)",
			voiceFlagSelf, err)
	}
	a, err := shot.OpenXActuator(opts.display)
	if err != nil {
		return fmt.Errorf("--%s: %w", voiceFlagSelf, err)
	}
	defer a.Close()
	sf, err := a.TypeIntoSelfWindow(text)
	if err != nil {
		return fmt.Errorf("--%s: %w", voiceFlagSelf, err)
	}
	fmt.Fprintf(os.Stderr,
		"[voice] typed %d character(s) into window 0x%x via %s (%s → %s, mode %s)%s\n",
		len([]rune(text)), sf.Window, sf.Label, fmtFocused(sf.FocusBefore), fmtFocused(sf.FocusAfter), sf.Mode, voiceSubmitHint)
	return nil
}

// voiceSubmitHint closes the loop for the operator: the transcript is in the
// window, unreviewed and unsent, and ONE more command sends it.
const voiceSubmitHint = " — not submitted; review it, then press Return with `" + CliVerbAct + " key --" + actFlagKey + " return --" + actFlagSelf + "`"

// fmtFocused renders a focus readback as a window id.
func fmtFocused(w uint32) string { return fmt.Sprintf("0x%x", w) }

// voiceWriteCaptureFile writes the captured PCM16 to path as a canonical WAV
// and returns the bytes written. The container is synthesized by the same
// voicePCM16ToWAV the HTTP route posts, so the file on disk is byte-identical
// to what the transcriber would have received — a debug copy that differed
// from the forwarded audio would be worthless as evidence.
//
// Three refusal shapes, each reported with the ACTION rather than the
// errno (Rule 15: a swallowed write error is a silent data loss):
//   - a missing parent directory  → name the directory to create
//   - a permission denial         → the file cannot be created here
//   - a short write               → the file is INCOMPLETE; say so, because a
//     truncated WAV looks playable and would be debugged as if it were real
//     audio. The partial file is removed so the next run cannot mistake it for
//     a complete recording.
func voiceWriteCaptureFile(path string, pcm []byte) ([]byte, error) {
	wav := voicePCM16ToWAV(pcm)
	f, err := os.OpenFile(path, voiceOutCreateFlags, voiceOutFileMode)
	if err != nil {
		return nil, fmt.Errorf("--%s %s: %s", voiceFlagOut, path, voiceOutWriteHint(err))
	}
	n, err := f.Write(wav)
	if err == nil && n != len(wav) {
		err = fmt.Errorf("short write: %d of %d bytes", n, len(wav))
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		// Best effort: the point of removing the stub is that a later run (or a
		// player) cannot mistake a partial capture for a complete one. A
		// failure to remove it is reported ALONGSIDE the original error, never
		// in place of it: see voiceCaptureFailure for why BOTH stay reachable
		// through errors.Is/As.
		//
		// merge-note (fix/rule16-voice-out-join × master): both sides fixed the
		// SAME stylegate errorf-verb site with the same intent — keep the removal
		// failure in the chain instead of rendering it with %v. master inlined
		// the two-%w shapes here (50413d89); this branch factored them into the
		// voiceCaptureFailure helper (d5e54fd1), which cli_voice_out_test.go
		// calls directly. Resolution: keep the helper (its callers and its
		// documented reasoning — two %w, Go ≥ 1.20, errors.Join rejected — live
		// just below) and keep master's comment wording. Behaviour is identical
		// on both paths; neither side's intent is dropped.
		return nil, voiceCaptureFailure(path, err, os.Remove(path))
	}
	return wav, nil
}

// voiceCaptureFailure reports a capture that failed to be written, folding in
// the cleanup failure when the incomplete file could not be removed.
//
// Chosen option: both errors stay IN THE CHAIN. The write error is what the
// caller acts on and the cleanup error is the evidence that a partial capture
// is still on disk, so neither may shadow the other — errors.Is/As must reach
// EITHER one. Two %w verbs in one fmt.Errorf do exactly that (Go ≥ 1.20; the
// repo's own precedent is internal/scratch/gc.go), and it keeps the single
// operator-facing sentence. errors.Join(err, rmErr) was the alternative —
// the same chain, but it drops the sentence to the joined form, and the
// sibling voiceDiscardSpool already covers the case where no message is
// wanted. A %v here would make rmErr unreachable (Rule 16 / checklist 2.2).
func voiceCaptureFailure(path string, writeErr, rmErr error) error {
	if rmErr == nil {
		return fmt.Errorf("--%s %s: %w (incomplete file removed)", voiceFlagOut, path, writeErr)
	}
	return fmt.Errorf("--%s %s: %w (and the incomplete file could not be removed: %w)",
		voiceFlagOut, path, writeErr, rmErr)
}

// voiceOutWriteHint turns the two write failures an operator actually hits into
// the action that fixes them.
func voiceOutWriteHint(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("%v — the directory does not exist (create it, or name a path inside one that does)", err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("%v — no write permission there (pick another path, or fix the directory)", err)
	default:
		return err.Error()
	}
}

// voicePCMSeconds reports the duration of a PCM16 buffer.
func voicePCMSeconds(pcm []byte) float64 {
	return float64(len(pcm)) / float64(voiceCaptureSampleRate*voiceCaptureBytesPerSample)
}

// voicePrintDevices renders the table --list-devices prints. The id column is
// what --device takes verbatim, so it is printed in full rather than
// abbreviated.
func voicePrintDevices(devices []voiceDevice) {
	width := 0
	for _, d := range devices {
		if len(d.ID) > width {
			width = len(d.ID)
		}
	}
	fmt.Printf("%-*s  %-*s  %s\n", width, voiceDeviceHeaderID,
		len(voiceDeviceBackendALSA), voiceDeviceHeaderBackend, voiceDeviceHeaderLabel)
	for _, d := range devices {
		fmt.Printf("%-*s  %-*s  %s\n", width, d.ID,
			len(voiceDeviceBackendALSA), d.Backend, d.Label)
	}
	fmt.Fprintf(os.Stderr, "[voice] %d capture device(s); pass one with --device <id>\n", len(devices))
}

const (
	voiceDeviceHeaderID      = "ID"
	voiceDeviceHeaderBackend = "BACKEND"
	voiceDeviceHeaderLabel   = "DESCRIPTION"
)
