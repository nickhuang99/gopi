package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// cli_voice_local_test.go — the BARE `gopi tool voice_input` contract: what the
// invocation with no argument and no --client MEANS (capture, bounded, local
// transcription), the refusals that keep a flag from being a silent no-op, and
// the two-root model resolution the shape depends on.
//
// These tests are deliberately about the SHAPE, not the audio: the end-to-end
// run against real hardware and real weights is the acceptance run recorded in
// the task's devdoc (Rule 31), and a unit test asserting "it recorded" would be
// the imagined caller the rule warns about.

// voiceTestProfilePointsAt puts GOPI_HOME on a fresh directory so a test that
// touches profile resolution cannot read (or write) the operator's own profile.
func voiceTestProfilePointsAt(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	return home
}

func TestVoiceParseBareInvocationIsTheCaptureDefault(t *testing.T) {
	opts, err := parseVoiceArgs(nil)
	if err != nil {
		t.Fatalf("the bare invocation must parse — it is the microphone default, not an error: %v", err)
	}
	if !opts.capture {
		t.Fatal("bare `gopi tool voice_input` must be the CAPTURE shape (microphone → local engine)")
	}
	if opts.server || opts.client || opts.listDevices {
		t.Fatalf("bare invocation must not become another shape: server=%v client=%v listDevices=%v",
			opts.server, opts.client, opts.listDevices)
	}
	if opts.audio != "" {
		t.Fatalf("bare invocation has no audio file, got %q", opts.audio)
	}
	// The bound: a named default length, so the recorder is stopped by a clock.
	if opts.seconds != voiceLocalDefaultSeconds {
		t.Fatalf("bare capture seconds = %d, want the named default %d", opts.seconds, voiceLocalDefaultSeconds)
	}
	// …and it must NOT be the remote client's shorter one: two documented
	// lengths for the same act is exactly the drift a default prevents.
	if voiceLocalDefaultSeconds == voiceClientDefaultSeconds {
		t.Fatal("the local default and the client default are now the same number; keep them distinguishable or collapse the constants deliberately")
	}
}

func TestVoiceParseBareInvocationHonoursSeconds(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--seconds", "3"})
	if err != nil {
		t.Fatalf("--seconds must apply to the bare capture too: %v", err)
	}
	if !opts.capture || opts.seconds != 3 {
		t.Fatalf("capture=%v seconds=%d, want true/3", opts.capture, opts.seconds)
	}
}

func TestVoiceParseBareInvocationHonoursDevice(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--device", "default"})
	if err != nil {
		t.Fatalf("--device must apply to the bare capture: %v", err)
	}
	if !opts.capture || opts.device != "default" {
		t.Fatalf("capture=%v device=%q, want true/default", opts.capture, opts.device)
	}
}

// The refusals: every one of these was a flag that changed NOTHING before, and
// the shape it was given on is named in the message so the correction is obvious.
func TestVoiceParseRefusesFlagsOnShapesThatCannotMeanThem(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"server-url on a server shape", []string{"--server", "--server-url", "http://x:1"}, "--server-url"},
		{"server-url while listing devices", []string{"--list-devices", "--server-url", "http://x:1"}, "captures nothing"},
		{"device on a file run", []string{"clip.wav", "--device", "default"}, "records nothing"},
		{"seconds on a file run", []string{"clip.wav", "--seconds", "5"}, "records nothing"},
		{"seconds while listing devices", []string{"--list-devices", "--seconds", "5"}, "records nothing"},
		{"record on a file run", []string{"clip.wav", "--record", "keep.wav"}, "records nothing"},
		{"audio file on a server shape", []string{"--server", "clip.wav"}, "no audio-file argument"},
		{"audio file while listing devices", []string{"--list-devices", "clip.wav"}, "no audio-file argument"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseVoiceArgs(tc.args)
			if err == nil {
				t.Fatalf("parseVoiceArgs(%q) must be refused at parse time — no recording may be spent discovering it", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the flag/shape %q", err.Error(), tc.want)
			}
		})
	}
}

// A refusal must not fire on the shapes that DO mean the flag — a guard that
// over-triggers is the same defect wearing the opposite coat.
func TestVoiceParseKeepsTheCaptureFlagsWhereTheyApply(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want func(*voiceOptions) bool
	}{
		{"client keeps seconds", []string{"--client", "--seconds", "8"}, func(o *voiceOptions) bool { return o.client && o.seconds == 8 }},
		{"client keeps device", []string{"--client", "--device", "default"}, func(o *voiceOptions) bool { return o.client && o.device == "default" }},
		{"client keeps the server url", []string{"--client", "--server-url", "http://x:1"}, func(o *voiceOptions) bool { return o.client && o.target == "http://x:1" }},
		{"bare capture keeps --record", []string{"--record", "keep.wav"}, func(o *voiceOptions) bool { return o.capture && o.record == "keep.wav" }},
		{"client keeps --record", []string{"--client", "--record", "keep.wav"}, func(o *voiceOptions) bool { return o.client && o.record == "keep.wav" }},
		{"list-devices keeps device", []string{"--list-devices", "--device", "default"}, func(o *voiceOptions) bool { return o.listDevices && o.device == "default" }},
		{"file run keeps chunk", []string{"clip.wav", "--chunk", "15"}, func(o *voiceOptions) bool { return o.audio == "clip.wav" && o.chunk == 15 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseVoiceArgs(tc.args)
			if err != nil {
				t.Fatalf("parseVoiceArgs(%q): %v", tc.args, err)
			}
			if !tc.want(opts) {
				t.Fatalf("parseVoiceArgs(%q) produced the wrong options: %+v", tc.args, opts)
			}
		})
	}
}

func TestVoiceParseRecordShape(t *testing.T) {
	if _, err := parseVoiceArgs([]string{"--record"}); err == nil {
		t.Fatal("--record with no path must be refused")
	}
	if _, err := parseVoiceArgs([]string{"--record", "  "}); err == nil {
		t.Fatal("--record with a blank path must be refused")
	}
	dir := t.TempDir()
	if _, err := parseVoiceArgs([]string{"--record", dir}); err == nil {
		t.Fatal("--record pointing at a directory must be refused")
	}
	// …but an EXISTING FILE is the point of --record (re-keeping one fixed
	// utterance name), so it must NOT be refused the way --out refuses it.
	existing := filepath.Join(t.TempDir(), "keep.wav")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseVoiceArgs([]string{"--record", existing}); err != nil {
		t.Fatalf("--record over an existing file must be allowed (that is its difference from --out): %v", err)
	}
	if _, err := parseVoiceArgs([]string{"--client", "--out", existing}); err == nil {
		t.Fatal("--out over an existing file must still be refused (the data-loss rule it exists for)")
	}
}

func TestVoiceStoreCaptureReplacesDeliberately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.wav")
	if err := os.WriteFile(path, []byte("a much longer old recording"), 0o644); err != nil {
		t.Fatal(err)
	}
	wav := []byte("RIFF-new-bytes")
	if err := voiceStoreCapture(path, wav); err != nil {
		t.Fatalf("voiceStoreCapture: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// REPLACED, not appended and not left longer: O_TRUNC is the whole point.
	if string(got) != string(wav) {
		t.Fatalf("file = %q, want exactly %q (a partial overwrite would leave tail bytes of the old recording)", got, wav)
	}
}

// ── the deployment search: rung 1 (the gopi profile) beats rung 2 ($HOME/work) ──
//
// These four tests are the POLICY, not an implementation detail: the epic's
// subject is "which of two places decides where the local engine's 914MB of
// weights live", so the order, the migration note, the independence of the two
// paths and the authority of an explicit flag each get their own assertion.

// voiceDeploymentRig materialises a COMPLETE deployment: both GGUFs under
// modelsDir and the runtime binary AT runtimePath. The paths are separate
// parameters because the two rungs spell them differently — rung 1 keeps the
// binary INSIDE its voice/ directory, rung 2 keeps it in work/runtime/…/bin/ —
// and a rig that assumed one layout would silently test the wrong rung.
func voiceDeploymentRig(t *testing.T, modelsDir, runtimePath string) {
	t.Helper()
	voiceRigWeightsOnly(t, modelsDir)
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// voiceRigWeightsOnly materialises just the two GGUF files in modelsDir — the
// half of a deployment a run may legitimately keep in one rung while the binary
// lives in the other.
func voiceRigWeightsOnly(t *testing.T, modelsDir string) {
	t.Helper()
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	enc, llm := voiceModelPaths(modelsDir)
	for _, p := range []string{enc, llm} {
		if err := os.WriteFile(p, []byte("weights"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// voiceRigLegacyDeployment rigs rung 2 for the pinned home and returns its two
// paths — the pair a host that never moved its deployment would have.
func voiceRigLegacyDeployment(t *testing.T, home string) (models, runtime string) {
	t.Helper()
	models, runtime = voiceLegacyModelPaths(home)
	voiceDeploymentRig(t, models, runtime)
	return models, runtime
}

// voiceRigProfileDeployment rigs rung 1 and returns its two paths.
func voiceRigProfileDeployment(t *testing.T) (models, runtime string) {
	t.Helper()
	models, runtime, err := voiceProfileVoicePaths()
	if err != nil {
		t.Fatalf("resolve the profile: %v", err)
	}
	voiceDeploymentRig(t, models, runtime)
	return models, runtime
}

// voiceTestLegacyHome points $HOME at a fresh directory, so the legacy rung
// ($HOME/work/...) is a path a test owns rather than the operator's real
// deployment. os.UserHomeDir reads HOME on unix, which is exactly the input
// voiceLegacyModelPaths consumes.
func voiceTestLegacyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// voiceCaptureStderr runs fn with os.Stderr redirected and returns what it
// printed — the run's own [voice] lines are the deliverable of this change
// (migration note, provenance), so they are asserted on, not eyeballed.
func voiceCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) //stylegate:ignore reads the local voice fixture's captured stdout for the transcript comparison
		done <- string(b)
	}()
	fn()
	os.Stderr = saved
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// TestVoiceDeploymentPrefersTheProfileRung is the ORDER, with both rungs
// complete on disk: the profile wins, and nothing is said about the legacy rung,
// because nothing came from it. Reversing the two cases inside
// voiceResolveDeployment turns this red — that is the red proof this change
// carries (the branch's report records the measurement).
func TestVoiceDeploymentPrefersTheProfileRung(t *testing.T) {
	voiceTestProfilePointsAt(t)
	legacyHome := voiceTestLegacyHome(t)
	wantModels, wantRuntime := voiceRigProfileDeployment(t)
	// Rung 2 is COMPLETE TOO — the point of the test. A host carrying both
	// deployments must read the one gopi owns.
	legacyModels, legacyRuntime := voiceRigLegacyDeployment(t, legacyHome)
	if legacyRuntime == wantRuntime || legacyModels == wantModels {
		t.Fatal("the rig built one deployment twice — the order is not being tested")
	}

	opts := &voiceOptions{engine: config.AudioEngineLocal}
	voiceDefaultDeploymentPaths(legacyHome, opts)
	out := voiceCaptureStderr(t, func() { voiceResolveDeployment(opts) })

	if opts.models != wantModels {
		t.Fatalf("models = %q, want rung 1 (the profile) %q", opts.models, wantModels)
	}
	if opts.runtime != wantRuntime {
		t.Fatalf("runtime = %q, want rung 1 (the profile) %q", opts.runtime, wantRuntime)
	}
	if strings.Contains(out, "legacy") {
		t.Fatalf("the migration note fired although rung 1 answered: %q", out)
	}
}

// TestVoiceDeploymentLegacyRungAnswersWithAMigrationNote: a host where only
// $HOME/work holds the deployment keeps working — and is TOLD. Silence here is
// the defect this test exists for: the legacy rung is a path gopi does not own,
// so depending on it must be visible.
func TestVoiceDeploymentLegacyRungAnswersWithAMigrationNote(t *testing.T) {
	voiceTestProfilePointsAt(t)
	legacyHome := voiceTestLegacyHome(t)
	legacyModels, legacyRuntime := voiceRigLegacyDeployment(t, legacyHome)
	profileModels, profileRuntime, err := voiceProfileVoicePaths()
	if err != nil {
		t.Fatal(err)
	}

	opts := &voiceOptions{engine: config.AudioEngineLocal}
	voiceDefaultDeploymentPaths(legacyHome, opts)
	out := voiceCaptureStderr(t, func() { voiceResolveDeployment(opts) })

	if opts.models != legacyModels || opts.runtime != legacyRuntime {
		t.Fatalf("rung 2 should answer when rung 1 is empty: models=%q runtime=%q", opts.models, opts.runtime)
	}
	// The note must be ACTIONABLE: it names the legacy path that answered and the
	// profile path to move to, including the runtime file's name — moving the
	// weights alone leaves the binary behind and the next run repeats the note.
	for _, want := range []string{"legacy", legacyModels, profileModels, profileRuntime, voiceProfileRuntimeName} {
		if !strings.Contains(out, want) {
			t.Errorf("migration note %q does not mention %q", out, want)
		}
	}
}

// TestVoiceDeploymentMigrationNoteIsSilentOnTheCloudArm is the flip's regression
// guard on the note's SCOPE. The engine default moved to doubao on 2026-10-07,
// and the cloud arm reads neither the weights nor the runtime — so a bare run
// that resolves to doubao must not be told to move a deployment it never opens.
// Before this test the same walk ran for both arms and the note fired on a run
// whose engine touches none of those files: a sentence about a file the run does
// not read, which is the false-provenance class the note itself exists to end.
// Reverting the `localRun` guard in voiceResolveDeployment turns this red.
func TestVoiceDeploymentMigrationNoteIsSilentOnTheCloudArm(t *testing.T) {
	voiceTestProfilePointsAt(t)
	legacyHome := voiceTestLegacyHome(t)
	legacyModels, legacyRuntime := voiceRigLegacyDeployment(t, legacyHome)

	opts := &voiceOptions{engine: config.AudioEngineRecording}
	voiceDefaultDeploymentPaths(legacyHome, opts)
	out := voiceCaptureStderr(t, func() { voiceResolveDeployment(opts) })

	if opts.models != legacyModels || opts.runtime != legacyRuntime {
		t.Fatalf("the walk itself must still run on every engine (one resolution, every shape): models=%q runtime=%q", opts.models, opts.runtime)
	}
	if strings.Contains(out, "legacy") {
		t.Fatalf("a doubao run reads no local deployment, so the migration note must not fire: %q", out)
	}
}

// the binary still in $HOME/work (or the other way round) is a WORKING
// installation, so neither path may be forced onto the other's root.
func TestVoiceDeploymentRungsResolveIndependently(t *testing.T) {
	voiceTestProfilePointsAt(t)
	legacyHome := voiceTestLegacyHome(t)
	// Rung 1 holds the WEIGHTS only; rung 2 holds everything. The run must take
	// each path from the rung that can answer for it.
	profileModels, _, err := voiceProfileVoicePaths()
	if err != nil {
		t.Fatal(err)
	}
	voiceRigWeightsOnly(t, profileModels)
	_, legacyRuntime := voiceRigLegacyDeployment(t, legacyHome)

	opts := &voiceOptions{engine: config.AudioEngineLocal}
	voiceDefaultDeploymentPaths(legacyHome, opts)
	out := voiceCaptureStderr(t, func() { voiceResolveDeployment(opts) })

	if opts.models != profileModels {
		t.Fatalf("models = %q, want the profile's %q", opts.models, profileModels)
	}
	if opts.runtime != legacyRuntime {
		t.Fatalf("runtime = %q, want the legacy binary %q", opts.runtime, legacyRuntime)
	}
	if !strings.Contains(out, "legacy") {
		t.Fatalf("the runtime came from rung 2, so the migration note must fire: %q", out)
	}
}

// TestVoiceDeploymentHonoursExplicitPaths: a path the operator NAMED is outside
// the search. Re-pointing it would make the run read different weights than the
// ones it reports — the failure the search must never be able to cause.
func TestVoiceDeploymentHonoursExplicitPaths(t *testing.T) {
	voiceTestProfilePointsAt(t)
	voiceTestLegacyHome(t)
	voiceRigProfileDeployment(t) // a COMPLETE rung 1, to be ignored

	named := filepath.Join(t.TempDir(), "somewhere-else")
	opts, err := parseVoiceArgs([]string{"clip.wav", "--models", named, "--runtime", filepath.Join(named, "engine")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.modelsExplicit || !opts.runtimeExplicit {
		t.Fatal("--models/--runtime must be recorded as named — the search reads that bit to stay out of the way")
	}
	out := voiceCaptureStderr(t, func() { voiceResolveDeployment(opts) })

	if opts.models != named {
		t.Fatalf("models = %q, want the named %q", opts.models, named)
	}
	if opts.runtime != filepath.Join(named, "engine") {
		t.Fatalf("runtime = %q, want the named path", opts.runtime)
	}
	if strings.Contains(out, "legacy") {
		t.Fatalf("nothing was resolved for this run, so no rung may be announced: %q", out)
	}
	if got := voiceDeploymentSource(opts); got != voiceSourceExplicit {
		t.Fatalf("provenance = %q, want %q", got, voiceSourceExplicit)
	}
}

// TestVoiceEngineProvenanceLineNamesTheRung pins the LINE every local
// transcription prints before it starts — the claim "the engine is no longer
// silent" reduced to something a test can hold: the sentence names the engine,
// both resolved paths AND the rung, and the cloud engine prints nothing at all
// (it reads neither the weights nor the runtime, so naming them would report a
// deployment this run is not touching).
//
// The rung label is asserted via voiceDeploymentSource's own value rather than
// its spelling, because the wording is that function's business; what this test
// owns is that the LINE carries it.
func TestVoiceEngineProvenanceLineNamesTheRung(t *testing.T) {
	voiceTestProfilePointsAt(t)
	voiceTestLegacyHome(t)
	models, runtime := voiceRigProfileDeployment(t)

	// The LOCAL engine is named explicitly: this test owns the on-machine
	// deployment contract (rungs, provenance line, weights), and since
	// 2026-10-07 the terminal default is the CLOUD engine — which reads no
	// deployment at all and announces nothing, so an engine-less literal here
	// would assert the wrong engine entirely (devdoc/doubao-asr-integration.md §3).
	opts := &voiceOptions{engine: config.AudioEngineLocal, chunk: voiceDefaultChunkSec}
	voiceResolveDeployment(opts)
	out := voiceCaptureStderr(t, func() { voiceReportEngineProvenance(opts) })

	for _, want := range []string{
		voiceResolveEngine(opts),
		models,
		runtime,
		voiceDeploymentSource(opts),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the provenance line %q does not name %q", out, want)
		}
	}

	// The cloud engine: no deployment is read, so nothing is announced.
	cloud := &voiceOptions{engine: config.AudioEngineRecording, chunk: voiceDefaultChunkSec}
	cloudOut := voiceCaptureStderr(t, func() { voiceReportEngineProvenance(cloud) })
	if cloudOut != "" {
		t.Fatalf("the cloud engine reads no deployment, yet printed %q", cloudOut)
	}
}

// TestVoiceDepsMissGivesAnInstallInstruction is the "no deployment" half of
// this change: the failure must say WHICH file is missing (the check's own
// vocabulary, unchanged) and then what to do about it — both files and where,
// the binary and where, and the engine that needs neither.
func TestVoiceDepsMissGivesAnInstallInstruction(t *testing.T) {
	voiceTestProfilePointsAt(t)
	voiceTestLegacyHome(t) // rung 2 empty as well: nothing is installed anywhere
	profileModels, profileRuntime, err := voiceProfileVoicePaths()
	if err != nil {
		t.Fatal(err)
	}
	enc, llm := voiceModelPaths(profileModels)
	legacyModels, legacyRuntime := voiceLegacyModelPaths(userHomeDirOrDefault())
	legacyEnc, legacyLLM := voiceModelPaths(legacyModels)

	// The LOCAL engine is named explicitly: the deployment walker's contract is
	// the on-machine engine's (rungs, weights, runtime), and the terminal default
	// has been the CLOUD engine since 2026-10-07 — a default-engine literal here
	// would walk the cloud arm's single credential check instead.
	local := &voiceOptions{engine: config.AudioEngineLocal, models: profileModels, runtime: profileRuntime}
	msg := voiceDepsReady(local)
	for _, want := range []string{
		"runtime not found", // the first missing thing, in the check's own words
		enc, llm,            // both weight files, by name
		profileRuntime,       // the runtime, by path
		"--engine recording", // the way to need none of it (the cloud BATCH engine)
		"--models",           // the two flags that override both rungs
		"--runtime",
		legacyModels, legacyRuntime, legacyEnc, legacyLLM, // the compatibility rung is named, not hidden
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("install guidance does not mention %q:\n%s", want, msg)
		}
	}
	// …and the guidance must not be a silent substitution for the precise
	// report: the file that FAILED is the first line.
	if !strings.HasPrefix(msg, "runtime not found:") {
		t.Errorf("guidance must open with the missing file it detected, got:\n%s", msg)
	}
}

// TestVoiceDepsGuidanceNamesTheProfileAsThePlaceToInstall: with a resolvable
// profile the instruction points at rung 1 and says so — the install step must
// send the operator to a directory gopi owns.
func TestVoiceDepsGuidanceNamesTheProfileAsThePlaceToInstall(t *testing.T) {
	voiceTestProfilePointsAt(t)
	voiceTestLegacyHome(t)
	profileModels, profileRuntime, err := voiceProfileVoicePaths()
	if err != nil {
		t.Fatal(err)
	}
	// Named LOCAL for the same reason as the test above: rung 1 is the on-machine
	// engine's deployment, and the terminal default is the cloud engine.
	msg := voiceDepsReady(&voiceOptions{engine: config.AudioEngineLocal, models: profileModels, runtime: profileRuntime})
	if !strings.Contains(msg, fmt.Sprintf("config.GopiDir + %q", voiceProfileDirName)) {
		t.Errorf("guidance does not name rung 1 as the profile: %s", msg)
	}
}

// TestVoiceUsageRendersTheCaptureBound is the regression guard for the defect
// this file's feature introduced and then had to fix: the usage text quoted the
// capture length as a PLACEHOLDER while the rendered help substituted only two
// placeholders, so `-h` printed the literal "{seconds}". A number the operator
// reads and the parser applies must come from the same named constant, rendered
// by the same function.
func TestVoiceUsageRendersTheCaptureBound(t *testing.T) {
	text := voiceUsageText()
	if strings.Contains(text, voiceSecondsPlaceholder) {
		t.Fatalf("voiceUsageText left %q unrendered — the help page would quote a placeholder instead of the bound the parser applies", voiceSecondsPlaceholder)
	}
	if want := strconv.Itoa(voiceLocalDefaultSeconds); !strings.Contains(text, want) {
		t.Fatalf("the rendered usage does not carry the capture bound %s (voiceLocalDefaultSeconds) — the number the operator reads and the one the parser applies have drifted", want)
	}
	// The other two placeholders must keep rendering too (a replacer that
	// dropped one while gaining another is the same defect).
	for _, ph := range []string{voiceFormatPlaceholder, voiceEngineDoubaoPlaceholder} {
		if strings.Contains(text, ph) {
			t.Fatalf("voiceUsageText left %q unrendered", ph)
		}
	}
}
