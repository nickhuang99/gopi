package main

// cli_voice_sauc_endpoint_origin_test.go — the regression case for the
// UNOBSERVABLE OVERRIDE that this change exists for.
//
// THE DEFECT, as a property rather than an anecdote. The v3 endpoint has two
// producers — the settings document, and the package's published default — and
// `voiceSaucLiveEndpoint`'s first branch is "a configured value wins VERBATIM".
// The two arms then want DIFFERENT defaults on purpose (the file arm wants
// `bigmodel_nostream`, the live arm wants `bigmodel_async`), so ONE line written
// for one arm overrides the other arm's default silently. Measured on this host
// 2026-10-13: `audio_engine_param.sauc.endpoint` said `bigmodel_nostream`, the
// live dictation fix (task 01a13303) was correct and delivered, and it had never
// run here — the run named the URL it was about to open and never said who chose
// it or that it contradicted the arm.
//
// SO THE PROPERTY IS: on a resolved sauc endpoint, the run's OWN output states
// the ORIGIN of the value, and it states the semantic MISMATCH when the value
// contradicts the arm it is being used on. Both halves are asserted on what the
// run PRINTS (the operator's only evidence), through the ONE function that
// resolves the decision (voiceSaucEndpointDecisionNote).
//
// RED WITHOUT THE FIX: `voiceReportSaucPrivacy` printed the privacy row alone and
// the chat engine row named the destination without its author — and these
// assertions fail on the missing sentence while the endpoint row itself still
// prints (so a case cannot pass by the line being absent altogether). Both red
// halves were taken and are quoted in the task report.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaosauc"
)

// saucSettingsDoc is the settings document of ONE host, spelled the way the
// operator writes it: a sauc block whose endpoint is the ~/only~ knob this file
// is about. An empty endpoint means "the block states none".
func saucSettingsDoc(endpoint string) string {
	if endpoint == "" {
		return "{}\n"
	}
	return `{"` + config.AudioEngineParamSettingsKey + `":{"` + config.AudioEngineSauc + `":{` +
		`"` + config.AudioParamKeyEndpoint + `":"` + endpoint + `"}}}` + "\n"
}

// TestVoiceSaucEndpointOriginIsStated is HALF ONE: a configured endpoint is
// reported AS configured, and the file-semantics value is reported as a
// contradiction of the live arm — in the operator's own run.
func TestVoiceSaucEndpointOriginIsStated(t *testing.T) {
	isolateSaucSettings(t, saucSettingsDoc(doubaosauc.DefaultEndpoint))
	if got := voiceSaucEffectiveEndpoint(); got != doubaosauc.DefaultEndpoint {
		t.Fatalf("the configured endpoint did not win: %q", got)
	}

	out := voiceCaptureStderr(t, func() { voiceReportSaucPrivacy(&voiceOptions{engine: config.AudioEngineSauc}) })

	// The origin is named, in the vocabulary the constants fix.
	if !strings.Contains(out, "from "+voiceSaucEndpointSourceSettings) {
		t.Errorf("the run did not say the endpoint came from settings; it printed:\n%s", out)
	}
	// The semantic mismatch is named, and it names the endpoint the arm needed.
	// Both the URL and the consequence are asserted: a row that said "mismatch"
	// without saying WHAT it opens, or what the arm wanted, leaves the operator
	// with the same question they started with.
	for _, want := range []string{doubaosauc.EndpointBidirectional, "answers ONCE"} {
		if !strings.Contains(out, want) {
			t.Errorf("the run did not state %q about a live-arm endpoint; it printed:\n%s", want, out)
		}
	}
}

// chatGridForSaucEndpoint is the ONE rig the two chat cases share: the production
// carrier (a real pty, the real input layer, the real read loop) with the v3
// engine resolved and the two pre-microphone seams answered, so the mode's OWN
// entry row is what the case reads.
//
// THE RECORDER IS THE LIVE SHAPE (liveScriptedRecorder), because the disclosure
// this change adds is about the LIVE arm: a recorder with no live half would
// fall through to the window loop, and the row would then be describing an arm
// the run does not have.
func chatGridForSaucEndpoint(t *testing.T) *voicePtyGrid {
	t.Helper()
	rec := &liveScriptedRecorder{
		desc:        "test mic",
		liveEntered: make(chan struct{}),
		finished:    make(chan struct{}),
	}
	h := newVoicePtyGridTall(t, nil)
	h.mode.recorder = rec
	h.mode.resolveOpts = func() (*voiceOptions, error) {
		return &voiceOptions{engine: config.AudioEngineSauc, device: "test-device"}, nil
	}
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	return h
}

// TestVoiceSaucEndpointOriginOnTheChatLiveArm is the half that the CLI's file
// shape cannot reach: the LIVE arm's OWN row, on the PRODUCTION chat carrier (a
// real pty, the real input layer, the real read loop), entered through the
// operator's own command line.
//
// IT IS THE Rule 31 EVIDENCE FOR THE SURFACE THIS BUG IS ABOUT. The disclosure
// belongs to `/voice` — that is where a live session on a file-semantics endpoint
// costs the operator something (the text arrives after they stop speaking) — so
// asserting it from the CLI's file path alone would be asserting a different
// surface than the one the defect was reported on.
func TestVoiceSaucEndpointOriginOnTheChatLiveArm(t *testing.T) {
	// A host whose settings carry the endpoint written for the FILE arm — the
	// measured configuration that silently overrode the live arm's default.
	isolateSaucSettings(t, saucSettingsDoc(doubaosauc.DefaultEndpoint))

	h := chatGridForSaucEndpoint(t)
	h.enter(t)

	h.waitFor(t, "the sauc engine row on the grid", func(rows string) bool {
		return strings.Contains(rows, config.AudioEngineSauc) && strings.Contains(rows, voiceSaucEndpointSourceSettings)
	})
	announced := h.log.String()
	if !strings.Contains(announced, voiceSaucEndpointSourceSettings) {
		t.Errorf("the chat row did not name the endpoint's origin; announced:\n%s", announced)
	}
	if !strings.Contains(announced, "answers ONCE") {
		t.Errorf("the live arm did not state that a file-semantics endpoint contradicts it; announced:\n%s", announced)
	}
	if !strings.Contains(announced, doubaosauc.EndpointBidirectional) {
		t.Errorf("the row does not name the endpoint the live arm needs; announced:\n%s", announced)
	}
}

// TestVoiceSaucChatRowSaysEngineDefaultWhenNothingIsConfigured is the negative
// half on the same carrier: the row still names an origin (the operator always
// gets an answer to "who chose this URL?"), and it stays silent about a mismatch
// that does not exist.
func TestVoiceSaucChatRowSaysEngineDefaultWhenNothingIsConfigured(t *testing.T) {
	isolateSaucSettings(t, saucSettingsDoc(""))

	h := chatGridForSaucEndpoint(t)
	h.enter(t)

	h.waitFor(t, "the sauc engine row on the grid", func(rows string) bool {
		return strings.Contains(rows, voiceSaucEndpointSourceEngine)
	})
	if announced := h.log.String(); strings.Contains(announced, "answers ONCE") {
		t.Errorf("an unconfigured host was told its endpoint contradicts the live arm; announced:\n%s", announced)
	}
}

// TestVoiceSaucEngineDefaultIsNotAWarning is HALF TWO, and it is the half that
// keeps the row worth reading: with nothing configured, the run says the value is
// the engine's own default and says NOTHING about a mismatch — because on that
// host every arm is doing exactly what its contract asks for (the file arm opens
// the file-semantics endpoint deliberately).
//
// A warning printed here would be a false statement about a working
// configuration, and a row that cries wolf is a row an operator learns to skip —
// which is the state this whole change is trying to leave behind.
func TestVoiceSaucEngineDefaultIsNotAWarning(t *testing.T) {
	isolateSaucSettings(t, saucSettingsDoc(""))
	if got := voiceSaucEffectiveEndpoint(); got != doubaosauc.DefaultEndpoint {
		t.Fatalf("the unconfigured endpoint is %q, want the package's default", got)
	}

	out := voiceCaptureStderr(t, func() { voiceReportSaucPrivacy(&voiceOptions{engine: config.AudioEngineSauc}) })

	if !strings.Contains(out, voiceSaucEndpointSourceEngine) {
		t.Errorf("the run did not say the endpoint came from the engine default; it printed:\n%s", out)
	}
	if strings.Contains(out, "answers ONCE") {
		t.Errorf("an unconfigured (engine-default) endpoint was reported as a mismatch — every arm is "+
			"behaving as designed here, so this row would be noise an operator learns to skip; it printed:\n%s", out)
	}
	// The mismatch sentence is about the OPERATOR's line, and there is none in
	// this configuration — it is keyed on the origin, not on the URL.
	if strings.Contains(out, "LIVE arm") {
		t.Errorf("the live-arm mismatch was reported with nothing configured; it printed:\n%s", out)
	}
}

// TestVoiceSaucEndpointMismatchNamesTheLiveArmDefault pins the sentence's
// ACTIONABLE half: the operator must be able to see which value would restore the
// arm, because "it is not working today" and "the arm's own default never ran"
// look identical from the outside (that is the measured incident).
func TestVoiceSaucEndpointMismatchNamesTheLiveArmDefault(t *testing.T) {
	isolateSaucSettings(t, saucSettingsDoc("wss://example.invalid/api/v3/sauc/bigmodel_nostream"))
	note := voiceSaucLiveEndpointNote(voiceSaucLiveEndpoint())
	if note == "" {
		t.Fatal("a configured file-semantics endpoint produced no mismatch sentence on the live arm")
	}
	if !strings.Contains(note, config.AudioParamSettingsPath(config.AudioEngineSauc, config.AudioParamKeyEndpoint)) {
		t.Errorf("the sentence does not name the setting it is about: %s", note)
	}
	if !strings.Contains(note, doubaosauc.EndpointBidirectional) {
		t.Errorf("the sentence does not name the endpoint the live arm needs: %s", note)
	}
}
