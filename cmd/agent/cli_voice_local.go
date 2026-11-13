package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// cli_voice_local.go — the BARE `gopi tool voice_input`: capture on this
// machine, transcribe on this machine, print the text.
//
// WHY IT EXISTS. The verb used to have exactly two doors: name an audio file,
// or run half a client/server pair (--client here, --server there). The
// intuitive shape — speak, and read the transcript — was reachable only by
// starting a server, pointing a client at it, and knowing both halves of a
// topology that has no reason to exist when the microphone and the 910MB of
// weights live on the same machine. The bare invocation IS that shape: the
// same process is the capture client and the transcriber.
//
// WHAT IT REUSES, DELIBERATELY. Nothing about capture is re-implemented: the
// device policy (four tiers, every one printed), the recorder argv, the
// dead-device reporting, the liveness probe and the "device is reachable but
// silent" verdict are the --client ones (cli_voice_client.go /
// cli_voice_device_policy.go). One capture implementation means a device that
// cannot be opened is reported identically whichever door it was reached
// through, and a fix to the policy cannot land on one door only.
//
// WHAT IT DOES NOT DO. It never opens a port, never posts anything anywhere and
// never asks for a --server-url: the transcription is transcribeFile() — the
// single engine seam every other shape already funnels through (see
// cli_voice_engine.go) — called on the recording staged below.

// voiceCaptureStagePrefix names the staged WAV of a bare capture. It lives in
// the runtime scratch class (internal/scratch), so the file is off `/` by
// construction and is collected by the sweep that already handles this class;
// the ".wav" suffix is part of the pattern because the engine stages/decodes by
// path and refuses an extension whose container it cannot read.
const voiceCaptureStagePrefix = "gopi-voice-capture-*.wav"

// runVoiceCaptureLocal is the bare invocation's whole run: resolve a device,
// record for a BOUNDED length, transcribe the recording with the local engine,
// print the transcript.
func runVoiceCaptureLocal(opts *voiceOptions) {
	// THE RUN'S CLOCK IS STARTED FIRST, before a device is looked for and before
	// a byte is captured: every figure the first-text report prints is measured
	// from here, so "the operator waited 18.5s for the first text" counts the
	// enumeration and the probing too — which is what waiting means.
	runClock := newVoiceRunClock(voiceRunOrigin())
	// The engine was already checked before this point (runVoiceSubcommand's
	// voiceDepsReady), so a missing model or runtime is refused BEFORE the
	// microphone is opened and cannot cost a recording. Say which engine runs,
	// where its weights came from, and WHICH RUNG supplied them: the local arm
	// is the default without being the only answer, and a deployment that came
	// from the legacy rung is a fact about the run (it printed the migration
	// note once, at resolution time — voiceResolveDeployment — so this line
	// stays a statement of what was read rather than a second warning).
	// The line itself lives in voiceReportEngineProvenance: the file shape
	// prints the same sentence, so it is written once (Rule 1b/14).
	voiceReportEngineProvenance(opts)

	// THE SOURCE IS RESOLVED FIRST, and it is either named or selected — never
	// both (voiceCapturePCM). A run that named --audio-source opens NO device: it
	// enumerates nothing, probes nothing, and works on a host with no sound card
	// at all, which is exactly what the seam is for. The device path below is
	// byte-for-byte what it was before that flag existed.
	pcm := []byte(nil)
	captureStart := time.Now()
	switch {
	case opts.audioSource != "":
		src := voiceFileSourceFor(opts.audioSource, voiceSourceImmediate)
		var err error
		pcm, err = voiceCapturePCM(src, opts.seconds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "[voice] --%s %s (no capture device is opened)\n",
			voiceFlagAudioSource, opts.audioSource)
	default:
		devices, notes, err := voiceListDevices(voiceRunCommand)
		for _, n := range notes {
			fmt.Fprintf(os.Stderr, "[voice] %s\n", n)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		sel, selNotes, err := voiceResolveSelection(opts.device, devices, voiceRunCommand, voiceCaptureAttempt)
		for _, n := range selNotes {
			fmt.Fprintf(os.Stderr, "[voice] %s\n", n)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		// THE ONE ROUTING DECISION OF THE STREAMING ENGINE, taken BEFORE a
		// recording is made: on the microphone shape the streaming channel is fed
		// AS THE AUDIO IS CAPTURED (cli_voice_stream_mic.go), so nothing is
		// recorded in front of it and the run's own report carries no capture
		// window. Every other engine — and every shape that named a file — falls
		// through to the recording path below, byte for byte what it was.
		//
		// IT IS TAKEN HERE rather than after the capture because the choice IS
		// "capture a block, then transcribe it" versus "feed the live capture":
		// by the time voiceRecord has returned, the window has already been paid.
		if voiceCaptureTakesLiveStream(opts) {
			// THE SAME ANNOUNCING LINE the recording path prints, so a reader can
			// tell WHICH DEVICE was selected and by WHICH tier whichever shape ran
			// (Rule 2/14: one sentence, two callers).
			fmt.Fprintf(os.Stderr, "[voice] %s\n", voiceDescribeCapture(sel, opts.seconds))
			voiceRunMicStream(opts, sel.Device, runClock)
			return
		}
		fmt.Fprintf(os.Stderr, "[voice] %s\n", voiceDescribeCapture(sel, opts.seconds))
		pcm, err = voiceRecord(sel.Device, opts.seconds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Fprintf(os.Stderr, "[voice] captured %d bytes (%.1fs of %dHz mono PCM16)\n",
		len(pcm), voicePCMSeconds(pcm), voiceCaptureSampleRate)
	// THE CAPTURE WINDOW, measured — not the number that was asked for. `--seconds
	// 30` is a REQUEST: the device may produce the audio faster or slower than its
	// own clock, and the file source produces it as fast as the disk allows. What
	// the operator waited through is this, so this is what the split reports.
	captureWindow := time.Since(captureStart)

	wav := voicePCM16ToWAV(pcm)
	if opts.record != "" {
		if err := voiceStoreCapture(opts.record, wav); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "[voice] kept %d bytes of WAV (%.1fs) in %s\n",
			len(wav), voicePCMSeconds(pcm), opts.record)
	}

	path, cleanup, err := voiceStageCaptureAudio(wav)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
		os.Exit(1)
	}
	text, elapsed, report, terr := transcribeFileReport(opts, path, runClock)
	// The staged copy goes as soon as the engine is done with it: it is a
	// transport detail, and leaving it behind would put a second copy of every
	// utterance in the scratch tree. The removal reports its own failure rather
	// than swallowing it (Rule 15b).
	cleanup()

	if terr != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", terr)
		os.Exit(1)
	}
	engineDone := time.Now()
	voiceReportTranscribedBy(opts, text, elapsed)
	// THE FIRST-TEXT REPORT: when the text existed, measured from the TOOL's
	// start (not from the engine's), with the split that explains it — the measured
	// capture window, the engine's own duration, and whatever the two do not
	// account for. See cli_voice_first_text.go.
	//
	// A RECORDING THAT HELD NO SPEECH HAS NO FIRST TEXT, and it is reported as
	// absent rather than as a very fast answer: the engine returning empty on a
	// silent window is a correct verdict about the audio, and a duration printed
	// beside it would read as "the text came quickly" (Rule 15).
	firstTextAt := runClock.since(engineDone)
	report.WindowMS = int(captureWindow.Milliseconds())
	report.FirstTextMS = int(firstTextAt.Milliseconds())
	report.FinalMS = report.FirstTextMS
	if strings.TrimSpace(text) == "" {
		report.Missing = fmt.Sprintf("the %s engine returned no text for the %.1fs window (silence, or the speech fell outside it)",
			voiceEngineLabel(opts), float64(opts.seconds))
	}
	voiceReportFirstText(opts, report)

	// --self types the transcript into this process's own window instead of only
	// printing it. The third argument names WHERE the text came from, for the
	// message that reports "no text to type": here the transcriber is this
	// process's own engine, not a URL.
	if opts.self {
		if err := voiceTypeIntoOwnWindow(opts, text, voiceLocalEngineLabel(opts)); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println(text)
}

// voiceDeploymentSource names the RUNG that supplied the paths this run is
// about to read, for the lines that report a local run's provenance.
//
// It exists because "models /home/nick/.gopi/voice" and "models
// /home/nick/work/funasr-models" are two different statements about the
// installation — rung 1 is the deployment gopi owns the location of, rung 2 is
// the legacy one it merely tolerates — and a reader of one [voice] line should
// not have to compare paths against a doc to tell which they got. The
// comparison is on the paths THEMSELVES (never on a flag), so a run that named
// --models is reported as explicit whatever the search would have said.
func voiceDeploymentSource(opts *voiceOptions) string {
	legacyModels, legacyRuntime := voiceLegacyModelPaths(userHomeDirOrDefault())
	profileModels, profileRuntime, err := voiceProfileVoicePaths()
	if opts.modelsFromConfig || opts.runtimeFromConfig {
		return voiceSourceConfig
	}
	if opts.modelsExplicit || opts.runtimeExplicit {
		return voiceSourceExplicit
	}
	if err == nil && opts.models == profileModels && opts.runtime == profileRuntime {
		return voiceSourceProfile
	}
	if opts.models == legacyModels || opts.runtime == legacyRuntime {
		return voiceSourceLegacy
	}
	// Neither rung (a caller-supplied root): the honest answer is that this run
	// is reading paths the search did not produce.
	return voiceSourceOther
}

// The three provenance labels voiceDeploymentSource returns. Named (Rule 1b):
// the same words appear in the run's stderr line and in the tests that pin it.
const (
	voiceSourceProfile  = "rung 1: the gopi profile"
	voiceSourceLegacy   = "rung 2: the legacy $HOME/work deployment"
	voiceSourceExplicit = "explicit --models/--runtime"
	// voiceSourceConfig is the same fact for a path that came from
	// configuration instead of the flag: it is JUST as explicit (the search may
	// not override it — modelsExplicit/runtimeExplicit), and the distinction
	// exists only so the provenance line names the channel that actually chose.
	voiceSourceConfig = "configured audio_engine_param.local"
	voiceSourceOther  = "caller-supplied paths"
)

// voiceEngineProvenanceLabel is the ONE spelling of the line that says which
// engine will transcribe and WHERE its deployment was found. Two shapes print
// it (the bare capture and the file run) and they print the same sentence, so
// the format lives here rather than being typed twice (Rule 1b/14): a reader
// who has seen one run recognises the next, and a change to the wording cannot
// reach one shape without the other.
const voiceEngineProvenanceLabel = "[voice] engine %s: models %s, runtime %s (%s)\n"

// voiceTranscribedByNote is the closing line of a run that produced text: WHICH
// engine transcribed, how long it took, which window size. The engine is named
// through voiceEngineLabel — the same spelling the announcing rows print — so the
// closing line and the opening line cannot name different engines, and the cloud
// arm does not get described as an on-machine run (Rule 13/14).
const voiceTranscribedByNote = "[voice] transcribed by %s in %.1fs (chunk=%ds)\n"

// voiceEngineNoTextNote is the same line's empty-transcript half. It names the
// engine for the same reason (an answer that reads "the local engine" on a run
// that uploaded is a false statement about where the audio went).
const voiceEngineNoTextNote = "[voice] the %s engine returned no text for a %.1fs window (chunk=%ds) — the recording may be silence, or the speech may have fallen outside it\n"

// voiceReportEngineProvenance prints that line. Called at the point a local
// transcription is about to happen — the moment "which engine, reading what?"
// stops being a question the operator can answer from the command line alone.
//
// It stays SILENT on the cloud engine: `--engine doubao` neither reads the GGUF
// weights nor spawns the runtime, so naming them would report a deployment this
// run is not touching — the line is a statement about what will be read, and on
// that engine nothing is.
func voiceReportEngineProvenance(opts *voiceOptions) {
	if voiceResolveEngine(opts) != config.AudioEngineLocal {
		return
	}
	fmt.Fprintf(os.Stderr, voiceEngineProvenanceLabel,
		voiceResolveEngine(opts), opts.models, opts.runtime, voiceDeploymentSource(opts))
}

// voiceLocalEngineLabel names the local transcriber for operator-facing text:
// the engine and the weights directory it read, because "which models did that
// come from?" is the first question a wrong transcript raises.
func voiceLocalEngineLabel(opts *voiceOptions) string {
	return fmt.Sprintf("the local %s engine (%s)", voiceResolveEngine(opts), opts.models)
}

// voiceReportTranscribedBy reports what ONE bare-capture / file run did, on
// STDERR, so stdout stays exactly the transcript — a caller that captures stdout
// gets the text and nothing else, the contract every other shape keeps.
//
// An empty transcript gets its own line rather than being passed off as a
// success: the engine exiting 0 with no text is a real outcome (silence, or a
// --seconds window that missed the speech), and an empty stdout with no
// explanation reads as a broken pipe.
func voiceReportTranscribedBy(opts *voiceOptions, text string, elapsed time.Duration) {
	// THE ENGINE IS NAMED, and the word "locally" is GONE — "locally" carried two
	// meanings at once (this machine vs the far end of a --client hop, and the
	// funasr engine vs the cloud one), so the run that had just announced an
	// UPLOAD closed with "transcribed locally in 2.5s" and read as its own
	// contradiction (two independent misreadings — task 01a11da05). The label is
	// voiceEngineLabel's, the SAME one the announcing rows print (Rule 13/14), so
	// the closing line and the opening line cannot name different engines — and
	// the sentence stays TRUE on the cloud arm of the bare capture, which
	// transcribes with doubao, not on this machine.
	if strings.TrimSpace(text) == "" {
		fmt.Fprintf(os.Stderr, voiceEngineNoTextNote,
			voiceEngineLabel(opts), float64(opts.seconds), opts.chunk)
		return
	}
	fmt.Fprintf(os.Stderr, voiceTranscribedByNote,
		voiceEngineLabel(opts), elapsed.Seconds(), opts.chunk)
}

// voiceStageCaptureAudio writes the capture's WAV into the runtime scratch
// class and returns its path plus the removal to run once the engine is done.
//
// The engine takes a PATH (the file channel and the HTTP server both hand it
// one — see voiceSpoolAudioPart), so this shape keeps that contract rather than
// teaching the engine a second way in. The scratch class is the same one the
// job/verification runs use: off `/` by construction and swept by the existing
// GC, never a directory invented here (Rule 22).
func voiceStageCaptureAudio(wav []byte) (string, func(), error) {
	f, err := scratch.CreateTemp("", voiceCaptureStagePrefix)
	if err != nil {
		return "", nil, fmt.Errorf("stage the recording: %w", err)
	}
	path := f.Name()
	cleanup := func() {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "[voice] could not remove the staged recording %s: %v\n", path, rmErr)
		}
	}
	n, err := f.Write(wav)
	if err == nil && n != len(wav) {
		err = fmt.Errorf("short write: %d of %d bytes", n, len(wav))
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("stage the recording: %w", err)
	}
	return path, cleanup, nil
}

// voiceStoreCapture writes a capture's WAV to path, REPLACING an existing file.
//
// This is the one deliberate difference from --out (voiceValidateOutPath): the
// flag exists to store this run's audio under a name the operator chose, and
// re-recording one fixed utterance name is what it is for, so an existing path
// is the normal case rather than a collision. The truncation is explicit
// (O_TRUNC) and the caller announces the byte count, so a replaced file is
// visible in the run's own output — never a silent overwrite.
func voiceStoreCapture(path string, wav []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, voiceOutFileMode)
	if err != nil {
		return fmt.Errorf("--%s %s: %w", voiceFlagRecord, path, err)
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
		return fmt.Errorf("--%s %s: %w", voiceFlagRecord, path, err)
	}
	return nil
}

// ── where the local engine's deployment is looked for: rung 1, then rung 2 ──
//
// THE ORDER IS A POLICY, not an accident of history. The local engine needs
// ~914 MB of weights plus a ~6 MB C++ runtime that gopi neither builds nor
// ships (Rule 35: it is a foreign dependency), so its deployment is something
// the OPERATOR puts somewhere — and the only directory in that story gopi owns
// is its own profile (config.GopiDir). That is RUNG 1: <profile>/voice, which
// resolves to $GOPI_HOME/.gopi/voice when GOPI_HOME is set and ~/.gopi/voice
// otherwise, so a sandboxed or peer run reads ITS OWN profile instead of the OS
// user's home. A deployment put there needs no flag and no directory gopi does
// not own.
//
// RUNG 2 is $HOME/work (funasr-models +
// runtime/llama.cpp/build/bin/llama-funasr-cli). It was the ONLY rung until this
// change, so a host where it holds the weights keeps working — but it is a
// COMPATIBILITY rung now, and the run SAYS so: a path supplied by it prints the
// migration line (voiceLegacyDeploymentNote), so the operator learns where gopi
// expects the deployment next time instead of depending on a path nothing
// maintains. Rung 2 is never preferred over rung 1, even when both are complete.
//
// A path the operator NAMED (--models / --runtime) is outside the order
// entirely (opts.modelsExplicit / opts.runtimeExplicit): an explicit answer is
// never second-guessed by a search, because "I said where the weights are and it
// read others" is the failure this function exists to prevent.
//
// THE TWO FIELDS RESOLVE INDEPENDENTLY on purpose: a host may keep the weights in
// the profile and the binary in $HOME/work (or the other way round), and forcing
// one root on both would refuse a working installation.
func voiceResolveDeployment(opts *voiceOptions) {
	legacyModels, legacyRuntime := voiceLegacyModelPaths(userHomeDirOrDefault())
	profileModels, profileRuntime, profileErr := voiceProfileVoicePaths()

	if !opts.modelsExplicit {
		switch {
		case profileErr == nil && voiceModelsComplete(profileModels):
			opts.models = profileModels
		case voiceModelsComplete(legacyModels):
			opts.models = legacyModels
		case voiceModelsComplete(opts.models):
			// A root the caller handed in (a test rig, a peer's own answer)
			// already holds the two weight files: nothing to resolve.
		case profileErr == nil:
			// Neither rung is complete. The answer is left on rung 1 so every
			// message that follows names the DEFAULT location first.
			opts.models = profileModels
		}
	}
	if !opts.runtimeExplicit {
		switch {
		// profileErr == nil BEFORE any path test, deliberately. Rung 1 names a
		// directory (the profile's voice/), so its runtime path exists only as a
		// SPELLING unless a binary is actually there: testing presence first
		// would let a complete legacy deployment keep answering the runtime
		// question while the weights came from the profile, and the migration
		// note would then never fire — the exact silent compatibility this
		// change exists to make visible.
		case profileErr == nil && !voiceFilePresent(profileRuntime):
			// Rung 1 has no binary: fall THROUGH the rungs, and keep rung 1's
			// spelling when none of them answers (it is the default location, so
			// the install guidance must name it).
			if voiceFilePresent(legacyRuntime) {
				opts.runtime = legacyRuntime
			} else if voiceFilePresent(opts.runtime) {
				// The caller's own root (a test rig, a peer's answer).
			} else {
				opts.runtime = profileRuntime
			}
		case profileErr == nil:
			opts.runtime = profileRuntime
		default:
			// Rung 1 cannot be SPELLED on this host (an unresolvable profile):
			// rung 2 is the only root there is, and the path the caller already
			// holds is kept unless it does not exist at all. Overwriting it with
			// rung 1's empty string would report "runtime not found: " — a
			// missing path with no path in it.
			if !voiceFilePresent(opts.runtime) && voiceFilePresent(legacyRuntime) {
				opts.runtime = legacyRuntime
			}
		}
	}

	// THE MIGRATION LINE — a compatibility rung still in use must say so, or the
	// operator keeps depending on a deployment gopi does not own and never learns
	// there is a place gopi does. Printed only when rung 2 actually SUPPLIED a
	// path (not merely because it exists), on stderr, once per run.
	//
	// AND ONLY WHEN A LOCAL TRANSCRIPTION WILL READ IT. Since the terminal default
	// flipped to the cloud engine (2026-10-07) a bare `gopi tool voice_input`
	// resolves to `doubao`, which opens neither the weights nor the runtime: a
	// note telling that run to move a deployment it never touches is a false
	// statement about the run, exactly the class of defect the note itself exists
	// to prevent. The resolution below is still done for every engine (one walk,
	// every shape), but the SENTENCE is the local arm's.
	localRun := voiceResolveEngine(opts) == config.AudioEngineLocal
	if localRun && ((!opts.modelsExplicit && opts.models == legacyModels) ||
		(!opts.runtimeExplicit && opts.runtime == legacyRuntime)) {
		note := voiceLegacyDeploymentNote(legacyModels, legacyRuntime, profileModels, profileRuntime)
		if profileErr != nil {
			// The destination cannot be named (an unresolvable profile), so the
			// note must not print empty paths as if they were an instruction.
			note = voiceLegacyDeploymentUnresolvedNote(legacyModels, legacyRuntime, profileErr)
		}
		fmt.Fprintf(os.Stderr, "[voice] %s\n", note)
	}
	if localRun && profileErr != nil {
		// Rung 1 does not exist on this host (an unresolvable profile). Said out
		// loud rather than swallowed (Rule 15b): the search then had exactly one
		// rung, which is the fact that explains the message following it. Scoped
		// to the local arm for the same reason as the migration note above — it
		// explains a search for local weights, which a cloud run never performs.
		fmt.Fprintf(os.Stderr, "[voice] %s\n", voiceProfileUnresolvedNote(profileErr))
	}
}

// voiceDefaultDeploymentPaths fills in the PARSER's default answer: rung 1, the
// operator's gopi profile. The walk itself is voiceResolveDeployment's job; this
// exists so that even a caller which never resolves (a unit test, a hand-built
// voiceOptions) points at the same place a real run does — one spelling of the
// default, not two.
//
// A profile that cannot be resolved falls back to rung 2's SPELLING, quietly:
// the parser must not print (it is used by tests and by the chat reply path), and
// both the resolver and voiceDepsReady report the failure properly right after.
func voiceDefaultDeploymentPaths(home string, opts *voiceOptions) {
	profileModels, profileRuntime, err := voiceProfileVoicePaths()
	if err != nil {
		opts.models, opts.runtime = voiceLegacyModelPaths(home)
		return
	}
	opts.models, opts.runtime = profileModels, profileRuntime
}

// voiceProfileVoicePaths is rung 1 in one place: the weights directory inside the
// operator's gopi profile, and the runtime binary inside that same directory.
// config.GopiDir is the ONE resolution of the profile ($GOPI_HOME/.gopi, else
// ~/.gopi) — Rule 2/14, one spelling, never a hand-joined ~/.gopi.
func voiceProfileVoicePaths() (models, runtime string, err error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(gd, voiceProfileDirName)
	return dir, filepath.Join(dir, voiceProfileRuntimeName), nil
}

// voiceLegacyModelPaths is rung 2 in one place: the $HOME/work deployment that
// gopi never created and does not maintain.
func voiceLegacyModelPaths(home string) (models, runtime string) {
	return filepath.Join(home, voiceLegacyModelRoot),
		filepath.Join(home, voiceLegacyRuntime)
}

// voiceLegacyDeploymentNote is the MIGRATION LINE: rung 2 answered, and here is
// where the answer is expected to live instead. It names both spellings so the
// move is a copy, not a search — and it names the runtime too, because moving
// the weights alone leaves the binary behind and the next run prints this again.
func voiceLegacyDeploymentNote(legacyModels, legacyRuntime, profileModels, profileRuntime string) string {
	return fmt.Sprintf("the local engine came from the legacy deployment %s (runtime %s) — "+
		"gopi's own location for it is the gopi profile: put the weights and %s in %s (runtime %s) and this note goes away",
		legacyModels, legacyRuntime, voiceProfileRuntimeName, profileModels, profileRuntime)
}

// voiceLegacyDeploymentUnresolvedNote is the same statement for the one host
// where the destination cannot be named: the profile itself did not resolve, so
// the note says that instead of pointing at an empty path.
func voiceLegacyDeploymentUnresolvedNote(legacyModels, legacyRuntime string, err error) string {
	return fmt.Sprintf("the local engine came from the legacy deployment %s (runtime %s) — "+
		"and the gopi profile, where gopi expects it, could not be resolved on this host: %v",
		legacyModels, legacyRuntime, err)
}

// voiceProfileUnresolvedNote names the one rung that could not be resolved, so a
// single-rung search is visible rather than inferred from a missing file.
func voiceProfileUnresolvedNote(err error) string {
	return fmt.Sprintf("the gopi profile (rung 1 for the local engine) could not be resolved: %v", err)
}

// voiceModelsComplete reports whether a model directory holds BOTH GGUF files —
// the encoder AND the llm. ONE of them is not a deployment: the engine is handed
// the pair together, so a root holding half of it would shadow the other rung and
// then fail at load time.
func voiceModelsComplete(modelsDir string) bool {
	enc, llm := voiceModelPaths(modelsDir)
	return voiceFilePresent(enc) && voiceFilePresent(llm)
}

// voiceFilePresent reports whether path is an existing NON-directory — both the
// runtime and the weights are files that get read, so a directory wearing the
// name is not one of them.
func voiceFilePresent(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
