package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// Capture-device SELECTION POLICY and hotplug robustness.
//
// THE POLICY — tier order, first match wins:
//
//	tier 1  --device ID          explicit; a typo fails loudly, nothing else
//	                             is tried (the user named a device on purpose)
//	tier 2  configured default   the default_device knob (env GOPI_VOICE_DEVICE,
//	                             or settings.json "default_device")
//	tier 3  system default       ALSA's own `default` — considered only when NO
//	                             sound server is reachable, because while a
//	                             server owns the card ALSA routes `default` to
//	                             that same server, making the tier redundant
//	tier 4  first enumerated     --list-devices order: PulseAudio first (the
//	                             @DEFAULT_SOURCE@ wildcard, then concrete
//	                             sources), then ALSA plughw before hw
//
// IDENTITY (stable across reboots): a device id is "<backend>:<recorder
// argument>", and the argument addresses the device BY NAME — a Pulse source
// name, or ALSA CARD=<card name>,DEV=<n>. A CARD INDEX is never accepted: it is
// a kernel probe order that changes when hardware is added or removed, so an id
// built from one silently records from a different device after a reboot. The
// same reasoning is why `arecord -l` is parsed for the card NAME and not the
// index column.
//
// HOTPLUG: enumeration and capture are separate moments, so the selected device
// can disappear in between (USB mic unplugged, sound server stopped, another
// process taking the card). The recorder's own open() is the liveness check —
// neither arecord nor parecord offers a probe that does not capture (measured on
// HP 2026-09-13: `arecord -d 0` is not "capture nothing", it captures with no
// limit and hangs). So the policy VERIFIES the selected device with a short
// capture before spending the user's speech on it, and an open failure is
// reported as unreachable-by-name with the re-list advice — never a hang, and
// never a silent fallback to a different microphone.
//
// The measured failure signatures behind that are recorded on HP and named
// below (voiceErrMissingMarkers); the tier table and the evidence behind it
// live in devdoc/voice-input-client.md (§"tier" — tier 1 is hard, tier 2 soft).

const (
	// The tier names, as printed next to the chosen device. They are constants
	// because the usage text and the devdoc quote them verbatim (Rule 1b).
	voiceTierFlag   = "explicit --device"
	voiceTierConfig = "configured default"
	voiceTierSystem = "system default capture"
	voiceTierFirst  = "first enumerated"

	// voiceALSADefaultName is ALSA's own system-default capture device. It is
	// synthesised rather than enumerated because `arecord -L` does not list
	// `default` at all — while `arecord -D default` is the one device string
	// measured working on the capture-capable host under both 16 kHz mono and
	// 48 kHz stereo (devdoc/voice-hw-matrix.md §2).
	voiceALSADefaultName = "default"

	// voicePulseInfoArg answers "is a sound server reachable at all?" — the
	// question tier 3 turns on.
	voicePulseInfoArg = "info"
	// voicePulseServerMarker appears in `pactl info` output exactly when a
	// server answered (PulseAudio or PipeWire's pulse shim).
	voicePulseServerMarker = "Server Name"

	// voiceVerifySeconds is the length of the pre-capture liveness check. One
	// second is the smallest sample that still proves an open()+read() pair
	// succeeded: a device that cannot be opened fails immediately, and one that
	// opens returns 32000 bytes (16 kHz mono PCM16) rather than nothing.
	voiceVerifySeconds = 1
)

// voiceErrMissingMarkers are the measured recorder refusals that mean "the
// device named is not there any more" rather than "the device is busy". All
// three were reproduced on HP (2026-09-13) against a device spelled correctly
// but no longer present:
//
//	arecord -D plughw:CARD=Generic_1,DEV=5 → "audio open error: No such file or directory"
//	parecord --device=no_such_source_xyz  → "Stream error: No such entity"
//	arecord -D plughw:9,0                 → "Cannot get card index for 9"
//
// They are matched case-sensitively as the tools print them, and only to ADD a
// hint — a message that does not match one is still reported verbatim.
var voiceErrMissingMarkers = []string{
	"No such file or directory",
	"No such entity",
	"Cannot get card index",
	"No such device",
}

// voiceCardIndexRe matches the forms a card index comes in: "1" or "1,0" (a
// user copying the index column of `arecord -l`), and "hw:1,0" /
// "plughw:1,0" (the index spelling of a device string). The name form that
// should have been used instead is produced in the error.
var voiceCardIndexRe = regexp.MustCompile(`^(?:[a-z]*:)?\d+(?:,\d+)?$`)

// voiceCapturer performs the short capture that proves a device is reachable.
// It is injected (like voiceCommandRunner) so the policy's ordering and
// hotplug answers are decided without a sound card: production passes
// voiceCaptureAttempt, tests pass a scripted answer.
type voiceCapturer func(dev voiceDevice, seconds int) ([]byte, error)

// voiceSelection is the policy's answer: which device, and which tier chose it.
type voiceSelection struct {
	Device voiceDevice
	Tier   string
}

// voiceSystemDefaultDevice is ALSA's system-default capture endpoint.
func voiceSystemDefaultDevice() voiceDevice {
	return voiceDevice{
		ID:      voiceDeviceBackendALSA + voiceDeviceIDSeparator + voiceALSADefaultName,
		Backend: voiceDeviceBackendALSA,
		Name:    voiceALSADefaultName,
		Label:   voiceALSADefaultName + " (ALSA's configured capture device)",
	}
}

// voiceConfigDefaultDevice returns the tier-2 configured default and the name
// of the knob that supplied it ("" when unconfigured). Reading the env var and
// the settings file happens HERE and nowhere else (Rule 2).
//
// A settings file that cannot be parsed is not fatal: the knob is a preference,
// and refusing to record because a preference is unreadable would be a worse
// failure than falling through to tiers 3/4.
func voiceConfigDefaultDevice() (value, source string) {
	if v := strings.TrimSpace(os.Getenv(config.VoiceDeviceEnv)); v != "" {
		return v, config.VoiceDeviceEnv
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil || cfg == nil {
		return "", ""
	}
	if v := strings.TrimSpace(cfg.DefaultDevice); v != "" {
		return v, config.VoiceDeviceSettingsKey
	}
	return "", ""
}

// voiceRejectCardIndex refuses a card-index device spelling with the by-name
// form that replaces it. A card index is not merely deprecated here: it is the
// one spelling that appears to work and then picks a DIFFERENT device after a
// reboot or a replug, so accepting it would make the policy's identity rule a
// lie.
func voiceRejectCardIndex(want string) error {
	if !voiceCardIndexRe.MatchString(want) {
		return nil
	}
	return fmt.Errorf("device %q is a card INDEX — that is the kernel's probe order and it changes "+
		"when hardware is added or removed, so it can silently record another device after a reboot; "+
		"use the by-name form from --list-devices (e.g. %s%splughw:CARD=<card name>,DEV=0)",
		want, voiceDeviceBackendALSA, voiceDeviceIDSeparator)
}

// voicePulseServerUp reports whether a sound server answers. It decides tier 3:
// with a server up, ALSA's `default` resolves to that same server and the tier
// would add nothing; with no server, `default` is the only capture route that
// still works (the raw hw: spelling has no plug layer and cannot accept the
// 16 kHz mono contract — see voiceRecorderCommand).
func voicePulseServerUp(run voiceCommandRunner) bool {
	out, err := run(voicePactlBinary, voicePulseInfoArg)
	if err != nil {
		return false
	}
	return strings.Contains(out, voicePulseServerMarker)
}

// voiceResolveSelection applies the policy to an enumerated device list and
// reports which tier answered. `run` is injected so the ordering is testable
// without a sound card.
//
// `notes` carries the non-fatal findings a caller should print: an enumerator
// that was unavailable, or a configured default that no longer exists. A stale
// CONFIGURED DEFAULT falls through to the next tier rather than aborting — it is
// a stored preference, and "your saved mic is gone, using the host default"
// beats refusing to record. An explicit --device never falls through: the user
// named a device, so that device or a named error.
func voiceResolveSelection(explicit string, devices []voiceDevice, run voiceCommandRunner, capture voiceCapturer) (voiceSelection, []string, error) {
	var notes []string

	// Tier 1 — explicit.
	if want := strings.TrimSpace(explicit); want != "" {
		if err := voiceRejectCardIndex(want); err != nil {
			return voiceSelection{}, notes, err
		}
		dev, err := voiceResolveDevice(want, devices, run)
		if err != nil {
			return voiceSelection{}, notes, err
		}
		if err := voiceVerifyDevice(dev, capture, &notes); err != nil {
			return voiceSelection{}, notes, err
		}
		return voiceSelection{Device: dev, Tier: voiceTierFlag}, notes, nil
	}

	// Tier 2 — configured default.
	if want, source := voiceConfigDefaultDevice(); want != "" {
		switch err := voiceRejectCardIndex(want); {
		case err != nil:
			notes = append(notes, fmt.Sprintf("%s is a card index and was ignored: %v", source, err))
		default:
			dev, rerr := voiceResolveDevice(want, devices, run)
			if rerr != nil {
				notes = append(notes, fmt.Sprintf("configured default device %q (%s) is not present — "+
					"falling through to the next rule", want, source))
			} else if verr := voiceVerifyDevice(dev, capture, &notes); verr != nil {
				notes = append(notes, fmt.Sprintf("configured default device %q (%s) is unreachable — "+
					"falling through to the next rule: %v", want, source, verr))
			} else {
				return voiceSelection{Device: dev, Tier: voiceTierConfig}, notes, nil
			}
		}
	}

	// Tier 3 — the system default capture, only when nothing else owns a server.
	if !voicePulseServerUp(run) {
		dev := voiceSystemDefaultDevice()
		verr := voiceVerifyDevice(dev, capture, &notes)
		if verr == nil {
			return voiceSelection{Device: dev, Tier: voiceTierSystem}, notes, nil
		}
		// A tier that was CONSIDERED and failed must say so: falling
		// through silently would hide the reason (a busy or absent
		// `default`) behind whatever tier 4 reports afterwards, and the
		// reader would blame the wrong device (Rule 15b).
		notes = append(notes, fmt.Sprintf("system default capture %q is not reachable — "+
			"falling through to the next rule: %v", dev.ID, verr))
	}

	// Tier 4 — the first enumerated device.
	dev, err := voiceResolveDevice("", devices, run)
	if err != nil {
		return voiceSelection{}, notes, err
	}
	if err := voiceVerifyDevice(dev, capture, &notes); err != nil {
		return voiceSelection{}, notes, err
	}
	return voiceSelection{Device: dev, Tier: voiceTierFirst}, notes, nil
}

// voiceVerifyDevice proves the selected device can actually be opened, by
// capturing voiceVerifySeconds of audio before the real recording starts. This
// is the hotplug answer: a device that disappeared between enumeration and
// capture fails here, by name, with the advice to re-list — instead of
// surfacing as a mysterious failure after the user has spoken.
//
// The Pulse default-source wildcard is exempt: it is a server-side alias with
// no concrete device to open (the server itself resolves it), so probing it
// would test a different thing than the one being selected.
//
// A device that opens but delivers NO samples is a note, not an error: a muted
// or zero-gain input is a real device (the failure-mode matrix owns that case),
// and refusing to record would hide the level problem behind a device error.
func voiceVerifyDevice(dev voiceDevice, capture voiceCapturer, notes *[]string) error {
	if dev.Name == voicePulseDefaultSource {
		return nil
	}
	pcm, err := capture(dev, voiceVerifySeconds)
	if err != nil {
		return fmt.Errorf("device %s is not reachable: %w — it may have been removed or taken since "+
			"it was listed; run `%s %s` again to see what is there now",
			dev.ID, err, voiceCLIVerb, voiceListDevicesFlag())
	}
	if len(pcm) < voiceMinCaptureBytes {
		*notes = append(*notes, fmt.Sprintf("device %s opened but produced no audio in %ds — "+
			"the input may be muted or at zero gain", dev.ID, voiceVerifySeconds))
	}
	return nil
}

// voiceCLIVerb and voiceListDevicesFlag exist so the advice printed inside an
// error names the same verb and flag the CLI parses (Rule 1b/13: the message
// cannot drift from the surface).
const voiceCLIVerb = "gopi tool voice_input"

func voiceListDevicesFlag() string { return "--" + voiceFlagListDevices }

// voiceDescribeCapture is the pre-record line: device, label, tier and length.
func voiceDescribeCapture(sel voiceSelection, seconds int) string {
	return fmt.Sprintf("recording %ds from %s (%s) [%s]",
		seconds, sel.Device.ID, sel.Device.Label, sel.Tier)
}
