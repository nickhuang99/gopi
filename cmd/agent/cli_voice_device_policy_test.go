package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// The selection policy is testable without a microphone because both of its
// outside-world dependencies are injected: voiceCommandRunner answers the
// `pactl info` question (is a sound server up?), and voiceCapturer answers the
// liveness question (does this device open?). The device list itself is a
// recorded fixture — the real `arecord -l` + `pactl list short sources` output
// of the HP laptop.
//
// The tests below pin the ORDER (tier 1 > 2 > 3 > 4), the STALE paths (a
// configured default or an explicit device that no longer exists), and the
// IDENTITY rule (a card index is refused, never silently resolved).

// voiceFakeRunner answers recorder/utility invocations from a script, and
// fails the test if the policy runs something the scenario did not declare —
// an unexpected probe would mean the policy grew a side effect.
func voiceFakeRunner(t *testing.T, script map[string]string) voiceCommandRunner {
	t.Helper()
	return func(name string, args ...string) (string, error) {
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if out, ok := script[key]; ok {
			return out, nil
		}
		t.Fatalf("policy ran an undeclared command: %q", key)
		return "", nil
	}
}

// voiceServerUp / voiceServerDown are the only two answers tier 3 depends on.
func voiceServerUp(t *testing.T) map[string]string {
	return map[string]string{
		voicePactlBinary + " " + voicePulseInfoArg: "Server String: /run/user/1000/pulse/native\nServer Name: pulseaudio\n",
	}
}

func voiceServerDown(t *testing.T) map[string]string {
	return map[string]string{
		voicePactlBinary + " " + voicePulseInfoArg: "Connection failure: Connection refused\n",
	}
}

// voiceScriptedCapture answers the liveness probe from a set of reachable
// devices. Anything else fails with the recorder's own refusal text, so the
// message the policy surfaces is the one a real unplug produces.
func voiceScriptedCapture(reachable map[string]bool) voiceCapturer {
	return func(dev voiceDevice, seconds int) ([]byte, error) {
		if reachable[dev.ID] {
			return make([]byte, voiceMinCaptureBytes*seconds), nil
		}
		return nil, fmt.Errorf("arecord failed on %q: exit status 1 — arecord: audio open error: No such file or directory", dev.ID)
	}
}

// voiceAllReachable is the happy path: every device opens.
func voiceAllReachable(devices []voiceDevice) map[string]bool {
	ok := make(map[string]bool, len(devices))
	for _, d := range devices {
		ok[d.ID] = true
	}
	return ok
}

// voiceTestDevices is the recorded HP single-input list, in --list-devices
// order (the Pulse wildcard, then the concrete Pulse source). The parser that
// produces such a list from real recorder output is covered by
// cli_voice_client_test.go; here the list is an INPUT, so the policy's order is
// pinned against a fixed list rather than against whatever this host has.
func voiceTestDevices() []voiceDevice {
	return []voiceDevice{
		{
			ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + voicePulseDefaultSource,
			Backend: voiceDeviceBackendPulse,
			Name:    voicePulseDefaultSource,
			Label:   "PulseAudio default source",
		},
		{
			ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + "alsa_input.pci-0000_05_00.6.analog-stereo",
			Backend: voiceDeviceBackendPulse,
			Name:    "alsa_input.pci-0000_05_00.6.analog-stereo",
			Label:   "alsa_input.pci-0000_05_00.6.analog-stereo",
		},
	}
}

// voiceTestALSAFirst is a two-input list where ALSA is first — the shape that
// makes "first enumerated" distinguishable from "pulse first".
func voiceTestALSAFirst() []voiceDevice {
	return []voiceDevice{
		{
			ID:      voiceDeviceBackendALSA + voiceDeviceIDSeparator + "plughw:CARD=Generic_1,DEV=0",
			Backend: voiceDeviceBackendALSA,
			Name:    "plughw:CARD=Generic_1,DEV=0",
			Label:   "HD-Audio Generic — ALC236 Analog [plughw]",
		},
		{
			ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + "alsa_input.pci-0000_05_00.6.analog-stereo",
			Backend: voiceDeviceBackendPulse,
			Name:    "alsa_input.pci-0000_05_00.6.analog-stereo",
			Label:   "alsa_input.pci-0000_05_00.6.analog-stereo",
		},
	}
}

// TestVoiceSelectionTierOrder pins the whole ladder in one place: with all
// tiers available, the highest one wins, and each tier is named in the result
// so the operator can see WHY that device was chosen.
func TestVoiceSelectionTierOrder(t *testing.T) {
	devices := voiceTestALSAFirst()
	first := devices[0]
	second := devices[1]

	t.Run("tier1 explicit beats everything", func(t *testing.T) {
		t.Setenv(voiceDefaultDeviceEnvForTest(t), second.ID)
		sel, _, err := voiceResolveSelection(second.ID, devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sel.Device.ID != second.ID || sel.Tier != voiceTierFlag {
			t.Fatalf("got %q/%q, want %q/%q", sel.Device.ID, sel.Tier, second.ID, voiceTierFlag)
		}
	})

	t.Run("tier2 configured default beats system and first", func(t *testing.T) {
		t.Setenv(voiceDefaultDeviceEnvForTest(t), second.ID)
		sel, _, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sel.Device.ID != second.ID || sel.Tier != voiceTierConfig {
			t.Fatalf("got %q/%q, want %q/%q", sel.Device.ID, sel.Tier, second.ID, voiceTierConfig)
		}
	})

	t.Run("tier4 first enumerated when nothing else is configured", func(t *testing.T) {
		t.Setenv(voiceDefaultDeviceEnvForTest(t), "")
		sel, _, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sel.Device.ID != first.ID || sel.Tier != voiceTierFirst {
			t.Fatalf("got %q/%q, want %q/%q", sel.Device.ID, sel.Tier, first.ID, voiceTierFirst)
		}
	})
}

// TestVoiceSelectionSystemDefaultIsConditional pins tier 3's condition: ALSA's
// `default` is only offered when no sound server answers, because with a
// server up `default` resolves to that same server and the tier would be a
// second name for tier 4.
func TestVoiceSelectionSystemDefaultIsConditional(t *testing.T) {
	devices := voiceTestALSAFirst()
	t.Setenv(voiceDefaultDeviceEnvForTest(t), "")

	t.Run("no sound server: system default is chosen", func(t *testing.T) {
		sysdev := voiceSystemDefaultDevice()
		reachable := voiceAllReachable(devices)
		reachable[sysdev.ID] = true
		sel, _, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerDown(t)), voiceScriptedCapture(reachable))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sel.Tier != voiceTierSystem || sel.Device.Name != voiceALSADefaultName {
			t.Fatalf("got %q/%q, want tier %q on %q", sel.Device.ID, sel.Tier, voiceTierSystem, voiceALSADefaultName)
		}
	})

	t.Run("sound server up: system default is skipped", func(t *testing.T) {
		sel, _, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sel.Tier != voiceTierFirst {
			t.Fatalf("tier %q: a reachable sound server must not be shadowed by ALSA's default", sel.Tier)
		}
	})
}

// TestVoiceSelectionStaleConfiguredDefault pins the graceful degradation: a
// saved default that no longer exists is a NOTE plus a fall-through, never an
// abort — the knob is a preference, and refusing to record because a
// preference is stale is a worse failure than using the host default.
func TestVoiceSelectionStaleConfiguredDefault(t *testing.T) {
	devices := voiceTestALSAFirst()
	t.Setenv(voiceDefaultDeviceEnvForTest(t), voiceDeviceBackendPulse+voiceDeviceIDSeparator+"unplugged_usb_mic")

	sel, notes, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
	if err != nil {
		t.Fatalf("a stale configured default must not abort: %v", err)
	}
	if sel.Tier != voiceTierFirst || sel.Device.ID != devices[0].ID {
		t.Fatalf("got %q/%q, want fall-through to %q", sel.Device.ID, sel.Tier, devices[0].ID)
	}
	if !voiceNotesMention(notes, "falling through") {
		t.Fatalf("the fall-through must be reported, notes=%v", notes)
	}
}

// TestVoiceSelectionStaleConfiguredDefaultUnreachable is the hotplug case for
// tier 2: the device is still ENUMERATED but its open() now fails (the card was
// taken between list and capture). Same degradation, different evidence.
func TestVoiceSelectionStaleConfiguredDefaultUnreachable(t *testing.T) {
	devices := voiceTestALSAFirst()
	t.Setenv(voiceDefaultDeviceEnvForTest(t), devices[1].ID)

	reachable := voiceAllReachable(devices)
	delete(reachable, devices[1].ID) // enumerated, but no longer openable

	sel, notes, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(reachable))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sel.Device.ID != devices[0].ID || sel.Tier != voiceTierFirst {
		t.Fatalf("got %q/%q, want fall-through to %q", sel.Device.ID, sel.Tier, devices[0].ID)
	}
	if !voiceNotesMention(notes, "unreachable") {
		t.Fatalf("the unreachable configured default must be reported, notes=%v", notes)
	}
}

// TestVoiceSelectionExplicitStaleDeviceFailsLoudly pins the OTHER half of the
// asymmetry: an explicit --device that disappeared between list and capture is
// a hard error naming the device and the re-list advice — never a silent
// substitution of a different microphone, which would record the wrong room and
// look like a success.
func TestVoiceSelectionExplicitStaleDeviceFailsLoudly(t *testing.T) {
	devices := voiceTestALSAFirst()
	// Enumerated, so resolution succeeds; unreachable, so the probe fails.
	reachable := voiceAllReachable(devices)
	delete(reachable, devices[0].ID)

	_, _, err := voiceResolveSelection(devices[0].ID, devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(reachable))
	if err == nil {
		t.Fatal("an unreachable explicit device must fail, not fall through")
	}
	if !strings.Contains(err.Error(), devices[0].ID) {
		t.Fatalf("the error must name the chosen device: %v", err)
	}
	if !strings.Contains(err.Error(), voiceFlagListDevices) {
		t.Fatalf("the error must point at the re-list flag: %v", err)
	}
}

// TestVoiceSelectionExplicitUnknownDeviceFails pins tier 1's typo path.
func TestVoiceSelectionExplicitUnknownDeviceFails(t *testing.T) {
	devices := voiceTestALSAFirst()
	_, _, err := voiceResolveSelection("pulse:typo_source", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
	if err == nil || !strings.Contains(err.Error(), "unknown device") {
		t.Fatalf("want an unknown-device error, got %v", err)
	}
}

// TestVoiceSelectionRejectsCardIndex pins the IDENTITY rule. A card index is
// the one device spelling that appears to work and then addresses a DIFFERENT
// device after a reboot or a replug, so it is refused with the by-name form —
// for the explicit flag AND for the configured knob, and never silently
// resolved even when a device with that name exists in the list.
func TestVoiceSelectionRejectsCardIndex(t *testing.T) {
	indexed := []voiceDevice{
		{
			ID:      voiceDeviceBackendALSA + voiceDeviceIDSeparator + "plughw:1,0",
			Backend: voiceDeviceBackendALSA,
			Name:    "plughw:1,0",
			Label:   "card index spelling",
		},
	}

	t.Run("explicit", func(t *testing.T) {
		for _, want := range []string{"1", "1,0", "hw:1,0", "plughw:1,0", "plughw:1"} {
			_, _, err := voiceResolveSelection(want, indexed, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(map[string]bool{}))
			if err == nil {
				t.Fatalf("card index %q must be refused", want)
			}
			if !strings.Contains(err.Error(), "card INDEX") {
				t.Fatalf("%q: want the index explanation, got %v", want, err)
			}
			if !strings.Contains(err.Error(), "CARD=") {
				t.Fatalf("%q: the error must show the by-name form, got %v", want, err)
			}
		}
	})

	t.Run("by-name forms are not mistaken for an index", func(t *testing.T) {
		byName := []voiceDevice{
			{
				ID:      voiceDeviceBackendALSA + voiceDeviceIDSeparator + "plughw:CARD=Generic_1,DEV=0",
				Backend: voiceDeviceBackendALSA,
				Name:    "plughw:CARD=Generic_1,DEV=0",
				Label:   "HD-Audio Generic",
			},
		}
		sel, _, err := voiceResolveSelection("plughw:CARD=Generic_1,DEV=0", byName, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(byName)))
		if err != nil {
			t.Fatalf("the by-name form must be accepted: %v", err)
		}
		if sel.Device.Name != "plughw:CARD=Generic_1,DEV=0" {
			t.Fatalf("resolved %q", sel.Device.Name)
		}
	})

	t.Run("configured default", func(t *testing.T) {
		// The list holds ONLY by-name devices: tier 4 must therefore land on a
		// by-name device, and an index-spelled knob must not be what is used.
		byNameOnly := voiceTestALSAFirst()
		t.Setenv(voiceDefaultDeviceEnvForTest(t), "plughw:1,0")
		sel, notes, err := voiceResolveSelection("", byNameOnly, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(byNameOnly)))
		if err != nil {
			t.Fatalf("a bad configured default must not abort: %v", err)
		}
		if sel.Device.Name == "plughw:1,0" {
			t.Fatal("the policy accepted a card index from the configured default")
		}
		if !voiceNotesMention(notes, "card index") {
			t.Fatalf("the ignored index must be reported, notes=%v", notes)
		}
	})
}

// TestVoiceSelectionVerifiedBeforeTheRealRecording pins the reason the probe
// exists at all: the device that is finally recorded from has been opened
// successfully once already. A policy that returned a device it never probed
// would spend the user's sentence on a device that fails to open.
func TestVoiceSelectionVerifiedBeforeTheRealRecording(t *testing.T) {
	devices := voiceTestALSAFirst()
	t.Setenv(voiceDefaultDeviceEnvForTest(t), "")

	var probed []string
	capture := func(dev voiceDevice, seconds int) ([]byte, error) {
		probed = append(probed, dev.ID)
		if seconds != voiceVerifySeconds {
			t.Errorf("probe length %ds, want %ds", seconds, voiceVerifySeconds)
		}
		return make([]byte, voiceMinCaptureBytes), nil
	}
	sel, _, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), capture)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(probed) == 0 || probed[len(probed)-1] != sel.Device.ID {
		t.Fatalf("the chosen device %q was not probed; probed=%v", sel.Device.ID, probed)
	}
}

// TestVoiceSelectionSilentDeviceIsANoteNotAnError pins the boundary between the
// policy and the failure-mode matrix: a device that OPENS but delivers no
// samples is reachable (a muted or zero-gain input is a real device), so the
// policy notes it and proceeds; the real recording is where "no audio" is a
// failure.
func TestVoiceSelectionSilentDeviceIsANoteNotAnError(t *testing.T) {
	devices := voiceTestALSAFirst()
	t.Setenv(voiceDefaultDeviceEnvForTest(t), "")

	silent := func(dev voiceDevice, seconds int) ([]byte, error) { return nil, nil }
	sel, notes, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), silent)
	if err != nil {
		t.Fatalf("a silent but openable device must not fail selection: %v", err)
	}
	if sel.Device.ID != devices[0].ID {
		t.Fatalf("chose %q, want the first device", sel.Device.ID)
	}
	if !voiceNotesMention(notes, "muted") {
		t.Fatalf("the silence must be reported, notes=%v", notes)
	}
}

// TestVoiceSelectionDefaultSourceWildcardIsNotProbed pins the one exemption: the
// Pulse default-source wildcard is a server-side alias with no concrete device
// to open. Probing it would test the server's choice, not the selection.
func TestVoiceSelectionDefaultSourceWildcardIsNotProbed(t *testing.T) {
	wildcard := voiceDevice{
		ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + voicePulseDefaultSource,
		Backend: voiceDeviceBackendPulse,
		Name:    voicePulseDefaultSource,
		Label:   "PulseAudio default source",
	}
	probes := 0
	capture := func(dev voiceDevice, seconds int) ([]byte, error) {
		probes++
		return nil, fmt.Errorf("the wildcard must not be probed")
	}
	sel, _, err := voiceResolveSelection(wildcard.ID, []voiceDevice{wildcard}, voiceFakeRunner(t, voiceServerUp(t)), capture)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if probes != 0 {
		t.Fatalf("the wildcard was probed %d time(s)", probes)
	}
	if sel.Device.ID != wildcard.ID {
		t.Fatalf("resolved %q", sel.Device.ID)
	}
}

// TestVoiceSelectionSingleInputHostIsUnchanged is the regression guard for the
// common case: one input, nothing configured — the policy must pick it and say
// which tier did, so the printed line never regresses to a bare device.
func TestVoiceSelectionSingleInputHostIsUnchanged(t *testing.T) {
	devices := voiceTestDevices()[1:] // the concrete source alone: one input, nothing configured
	if len(devices) != 1 {
		t.Fatalf("fixture: %d devices", len(devices))
	}
	t.Setenv(voiceDefaultDeviceEnvForTest(t), "")

	sel, _, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerUp(t)), voiceScriptedCapture(voiceAllReachable(devices)))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	line := voiceDescribeCapture(sel, voiceClientDefaultSeconds)
	for _, want := range []string{sel.Device.ID, voiceTierFirst, fmt.Sprint(voiceClientDefaultSeconds)} {
		if !strings.Contains(line, want) {
			t.Fatalf("the pre-record line %q must carry %q", line, want)
		}
	}
}

// voiceNotesMention reports whether any note carries the fragment — the notes
// are operator-facing prose, so the assertions match their wording.
func voiceNotesMention(notes []string, fragment string) bool {
	for _, n := range notes {
		if strings.Contains(n, fragment) {
			return true
		}
	}
	return false
}

// voiceDefaultDeviceEnvForTest is the env var name the policy reads for tier 2.
// It comes from the config package rather than being typed here, so a rename
// lands in the policy and the tests together (Rule 1b).
func voiceDefaultDeviceEnvForTest(t *testing.T) string {
	t.Helper()
	return config.VoiceDeviceEnv
}

// TestVoiceSelectionSystemDefaultFailureIsReported pins that a CONSIDERED tier
// that fails says so. With no sound server, tier 3 is consulted; when ALSA's
// `default` is busy (the exact live state on HP: a desktop holds the PCM and
// `default` routes back into it), the run falls through to tier 4 — and
// without the note the chosen device is blamed for a failure the reader never
// sees. Measured on HP 2026-09-14: the tier-4 line appeared while
// `alsa:plughw:CARD=Generic_1,DEV=0 is not reachable: … Device or resource
// busy` was printed with no indication that tier 3 had been tried first.
func TestVoiceSelectionSystemDefaultFailureIsReported(t *testing.T) {
	devices := voiceTestALSAFirst()
	t.Setenv(voiceDefaultDeviceEnvForTest(t), "")

	reachable := voiceAllReachable(devices) // tier 4 opens; tier 3 does not
	sel, notes, err := voiceResolveSelection("", devices, voiceFakeRunner(t, voiceServerDown(t)), voiceScriptedCapture(reachable))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sel.Tier != voiceTierFirst {
		t.Fatalf("tier %q, want the fall-through to %q", sel.Tier, voiceTierFirst)
	}
	joined := strings.Join(notes, " | ")
	if !strings.Contains(joined, voiceALSADefaultName) || !strings.Contains(joined, "not reachable") {
		t.Fatalf("notes %q must report that tier 3 (%q) was tried and failed", joined, voiceALSADefaultName)
	}
}
