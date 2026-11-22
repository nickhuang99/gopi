package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaosauc"
	"github.com/nickhuang99/gopi/internal/doubaostream"
)

// THE THIRD ARM of the engine dispatch: the v2 real-time STREAMING channel
// (`api/v2/asr`, cluster volcengine_streaming_common), reached by explicitly
// naming the engine.
//
// WHY IT IS A SEPARATE ENGINE VALUE rather than a flag on `doubao`: the two
// cloud engines answer different questions and are billed differently. `doubao`
// is the batch channel — the whole file is uploaded, then polled, then the
// transcript arrives (measured 2.5 s for 8 s of audio). `doubao-stream` is one
// WebSocket with a 200 ms packet cadence, so a PARTIAL transcript exists while
// the audio is still being sent — the property the chat `/voice` window
// machinery needs. Keeping them apart also keeps the guarantee the batch engine
// was chosen for: a machine with nothing deployed still transcribes, because
// streaming is never reachable by DEFAULT.
//
// SIGNATURE, UNSHARED AND UNCHANGED. transcribeFile's contract is "one audio
// file in, one transcript out", and the three shapes (--server, --server
// --stdio, the file run) plus the capture path all share it. Streaming is
// "text while you speak", so the two halves are split rather than the signature
// being widened:
//
//   - the RETURN VALUE is the final transcript, which is what every caller
//     already consumes, so nothing outside this arm changes;
//   - the PARTIALS travel on Options.OnPartial, an in-arm progress channel that
//     the display layer decides what to do with (voiceReportStreamPartials
//     below prints them; a caller that passes nothing gets no printing).
//
// Widening the signature would have been the cheap move and the wrong one: the
// local arm is the regression baseline for this whole feature, and a signature
// change reaches it through all three call sites whether or not the streaming
// arm needs it. This way `transcribeFileLocal` is not touched at all.
//
// THE RESULT STREAM IS PRINTED ON STDERR, never stdout: stdout carries the
// transcript and nothing else, the contract every other shape keeps.

// Default credential location for this channel — the path the operator set up
// for it, and the ONLY thing any message may quote about the credential.
const (
	voiceStreamKeyFileEnv = config.AudioKeyFileEnv
	// voiceStreamCredentialNote names where the credential comes from when no
	// channel named a file. The VALUE never appears anywhere. Since 2026-10-09
	// there are TWO ways to satisfy it — an inline APPID/TOKEN pair in the
	// settings document, or a file — so the sentence names both, in the shape
	// of the resolution voiceStreamCredentialSource applies.
	voiceStreamCredentialNote = "the v2 streaming credential is either an inline APPID/TOKEN pair " +
		"(settings %q / %q, or $%s / $%s) or a KEY=VALUE file (APPID, TOKEN) named with --%s / $%s / settings %q, " +
		"defaulting to %s"
	// voiceStreamFileFellThroughNote is printed when a named credential FILE
	// could not be read and the inline pair answered instead. It is NOT silent
	// by design (Rule 15): an operator who named a file and got a working run
	// from a different credential must be told which one was used, or the next
	// debugging session starts from a false premise.
	voiceStreamFileFellThroughNote = "[voice] the streaming credential file %s could not be used (%v) — " +
		"falling back to the inline APPID/TOKEN (%s)\n"
	// voiceStreamURLSchemePrefix is the scheme a streaming endpoint must carry.
	// The v2 channel is a WebSocket, so a bare host or an https:// URL is
	// refused by NAME before the microphone opens rather than failing inside
	// the dialer — a pre-flight check exists to catch exactly this.
	voiceStreamURLSchemePrefix = "wss://"
)

// ── THE TWO LINES OF voiceReportStreamStart, and the counter block ──────────
//
// The spellings are constants (Rules 1b/13/14) because more than one shape
// prints them and a test reads them back: the file-fed arm
// (cli_voice_first_text.go), the live microphone arm (cli_voice_stream_mic.go)
// and transcribeFileStream all go through the two printers below, so a message
// typed at one call site would be a second, silently divergent spelling of the
// same row.
const (
	// voiceStreamPrivacyNote is the ALWAYS-PRINTED half: the destination, and
	// the fact that the recording leaves this machine as it is spoken. It stays
	// on the default path because it is a DISCLOSURE, not telemetry — the
	// process-level announce in runVoiceSubcommand covers the `recording` and
	// `sauc` engines and cannot cover this one (the streaming arm is reached
	// through the file path and the capture path, not through that switch), so
	// dropping it would make this engine the one cloud channel that uploads
	// without saying so.
	voiceStreamPrivacyNote = "[voice] PRIVACY — engine %s streams the audio to %s; the recording leaves this machine in real time\n"
	// voiceStreamPlumbingNote is the OPT-IN half: the packet cadence, the
	// cluster the request is billed against, and where the credential was read
	// from (a path or a channel name — never a value).
	voiceStreamPlumbingNote = "[voice] stream: %dms packets, cluster %s, credential: %s\n"
	// voiceStreamCredentialInlineLabel names the inline route when no file
	// answered, in the ONE wording the plumbing note uses.
	voiceStreamCredentialInlineLabel = "the settings document / its env channel"

	// voiceStreamPartialKindUngated / voiceStreamPartialKindDefinite are the
	// two words a per-partial line may carry — the vendor's own distinction:
	// a partial that may still be rewritten, and the `definite` one that
	// closes the sentence.
	voiceStreamPartialKindUngated  = "partial"
	voiceStreamPartialKindDefinite = "sentence"

	// The counter block's four sentences. Each is one line's whole template so
	// a reader (and a test) can name the row rather than a fragment of it.
	voiceStreamNoPartialNote    = "[voice] stream: no partial arrived before the final transcript\n"
	voiceStreamPartialCountNote = "[voice] stream: %d partial(s), first at %.2fs, final at %.2fs\n"
	voiceStreamSentCountNote    = "[voice] stream: %dms of audio sent, service reported %dms, logid %s, reqid %s\n"
	voiceStreamSentNote         = "[voice] stream: %dms of audio sent, logid %s, reqid %s\n"
)

// voiceStreamCredentialHome answers the home directory the STREAMING
// credential's default path is assembled from.
//
// It exists instead of calling the shared userHomeDirOrDefault() directly, and
// the reason is a scoping one: that helper also builds the LOCAL model/runtime
// legacy paths, and giving it a $HOME preference would change those paths'
// meaning for every caller — collateral scope on a change about a credential.
// Here the $HOME preference buys something specific: the default rung of this
// resolution is testable by pointing HOME at a temporary directory, so the
// suite can assert "~/.doubao/v2_voice.env" without reading — or risking a
// write to — the operator's own ~. With HOME unset the platform answer stands.
func voiceStreamCredentialHome() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home
	}
	return userHomeDirOrDefault()
}

// streamCredentialSource is WHERE the v2 credential was found, and what it
// resolves to. It is the ONE resolution point for this channel's credential,
// so the pre-flight check, the request assembly and every message agree about
// which channel won (Rule 2).
//
// THE PRECEDENCE, spelled once — the file rungs are the SAME ones the batch
// engine uses (voiceDoubaoKeySource, so the two channels cannot disagree about
// what `--doubao-key-file` names), and the inline pair below them fills the gap
// when a file was named but is not there:
//
//	--doubao-key-file  >  $GOPI_DOUBAO_KEY_FILE  >  settings "doubao_key_file"
//	                   >  (that file exists and parses → its APPID/TOKEN)
//	                   >  $GOPI_DOUBAO_APP_ID + $GOPI_DOUBAO_TOKEN
//	                   >  settings "doubao_app_id" + "doubao_token"
//	                   >  ~/.doubao/v2_voice.env (the legacy location)
//
// Unlike the batch engine's asymmetry (an explicitly named FILE outranks the
// inline value outright), a file that CANNOT be read does not refuse the run
// while an inline pair is configured: the operator configured both, and the
// one that works is the one they want used. The substitution is announced, never
// silent.
type streamCredentialSource struct {
	// inline is the credential read out of the settings document or its env
	// channel ("" when neither carries a pair). NEVER printed.
	inlineAppID string
	inlineToken string
	// inlineFrom names the channel the inline pair came from, for the
	// announcement and the refusal.
	inlineFrom string
	// fileCandidates is the ordered list of paths a credential could be read
	// from — the explicitly named file (when a channel named one, in which case
	// the legacy default is NOT tried: an operator who pointed the knob at a
	// path means that path) or the legacy default alone.
	fileCandidates []string
}

// voiceStreamCredentialSource resolves the streaming credential. It is a
// resolver, not a reader: it decides which channel won and hands the caller the
// candidates to load, so the pre-flight check can answer "can this run proceed?"
// through the same code path the request uses.
func voiceStreamCredentialSource(opts *voiceOptions) streamCredentialSource {
	src := streamCredentialSource{}
	key := voiceDoubaoKeySource(opts)
	voiceReportKeyMode(key)
	// The inline pair, from the ONE reader for this block (Rule 2):
	// config.ResolveAudioStreaming applies the environment over the nested
	// block, with the pre-rename top-level keys already folded in by the load
	// path. Nothing here re-implements the order.
	inlineFrom := fmt.Sprintf("settings %q / %q",
		config.AudioParamSettingsPath(config.AudioEngineStreaming, config.AudioParamKeyAppID),
		config.AudioParamSettingsPath(config.AudioEngineStreaming, config.AudioParamKeyToken))
	if cfg, err := config.FromSettingsFile(config.ResolveSettingsPath()); err == nil && cfg != nil {
		stream := config.ResolveAudioStreaming(cfg)
		appID := strings.TrimSpace(stream.AppID)
		token := strings.TrimSpace(stream.Token)
		if appID != "" || token != "" {
			// The VALUES already carry the environment (ResolveAudioStreaming
			// applies it above the block), so nothing is re-read here — that
			// would be a second reader of the same variable (Rule 2). What the
			// environment changes is only what the message may CLAIM about
			// where the value came from: a note that pointed at the settings
			// block while the value came from the shell would send the operator
			// to the wrong file.
			src.inlineAppID, src.inlineToken = appID, token
			src.inlineFrom = inlineFrom
			for _, name := range []string{config.AudioAppIDEnv, config.DoubaoAppIDEnv} {
				if strings.TrimSpace(os.Getenv(name)) != "" {
					src.inlineFrom = name
					break
				}
			}
			for _, name := range []string{config.AudioTokenEnv, config.DoubaoTokenEnv} {
				if strings.TrimSpace(os.Getenv(name)) != "" {
					src.inlineFrom = name
					break
				}
			}
		}
	}
	if key.file != "" {
		// A named file is the operator's own statement about where the
		// credential lives: try it, and only it.
		src.fileCandidates = []string{key.file}
		return src
	}
	src.fileCandidates = []string{doubaostream.DefaultCredentialPath(voiceStreamCredentialHome())}
	return src
}

// load returns the credential this source resolves to, the path it came from
// ("" for an inline pair) and an error naming BOTH ways to fix a missing one.
//
// IT WRITES NOTHING TO STDERR, and that is a contract rather than an accident:
// this function is called TWICE on a real run — once by the pre-flight check
// (voiceDepsReady) and once by the arm that transcribes — so an announcement
// printed here would print twice, which is the same defect class as the help's
// duplicated line (measured 2026-10-09: the file-fell-through note appeared on
// two consecutive lines of one run). Everything an operator must be TOLD is
// therefore carried out as a value (path / fellThroughNote) and printed by the
// caller that is actually opening the session, where it is printed once.
//
// A file that exists but cannot be parsed falls back to a complete inline pair
// (Rule 15: never silent — see fellThroughNote); a file that cannot be read at
// all is the same case, because the operator may have named the knob BEFORE
// writing the inline pair into settings.json: the two are independent channels.
func (s streamCredentialSource) load() (doubaostream.Credential, string, error) {
	inlineComplete := s.inlineAppID != "" && s.inlineToken != ""
	var firstErr error
	for _, path := range s.fileCandidates {
		if _, err := os.Stat(path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		cred, err := doubaostream.LoadCredentials(path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return cred, path, nil
	}
	if inlineComplete {
		return doubaostream.Credential{AppID: s.inlineAppID, Token: s.inlineToken}, "", nil
	}
	// NOTHING resolved. The refusal names the knob that would fix it and never a
	// value (Rule 15): which channel was tried, why it failed, and both routes.
	path := s.fileCandidates[0]
	why := "no such file"
	if firstErr != nil {
		why = firstErr.Error()
	}
	return doubaostream.Credential{}, "", fmt.Errorf(
		"the %s engine needs a credential (%s): %s. %s", config.AudioEngineStreaming, why, path,
		fmt.Sprintf(voiceStreamCredentialNote,
			config.DoubaoAppIDSettingsKey, config.DoubaoTokenSettingsKey,
			config.DoubaoAppIDEnv, config.DoubaoTokenEnv,
			voiceFlagDoubaoKeyFile, voiceStreamKeyFileEnv, config.DoubaoKeyFileSettingsKey, path))
}

// fellThroughNote reports the sentence a caller must print when a credential
// FILE was tried and could not be used while an inline pair answered instead,
// and "" when nothing was substituted. It RE-RUNS the same resolution rather
// than caching state, so the pre-flight check and the run cannot disagree about
// whether a substitution happened (Rule 2).
func (s streamCredentialSource) fellThroughNote() string {
	if len(s.fileCandidates) != 1 {
		return ""
	}
	if s.inlineAppID == "" || s.inlineToken == "" {
		return ""
	}
	path := s.fileCandidates[0]
	_, err := os.Stat(path)
	if err == nil {
		_, err = doubaostream.LoadCredentials(path)
	}
	if err == nil {
		return ""
	}
	return fmt.Sprintf(voiceStreamFileFellThroughNote, path, err, s.inlineFrom)
}

// voiceStreamEffectiveEndpoint is the URL the streaming run will actually open,
// from the ONE resolution the settings source owns (Rule 2): the flag, then the
// env, then the settings key, then the package's published default.
func voiceStreamEffectiveEndpoint(opts *voiceOptions) string {
	if v := strings.TrimSpace(opts.doubaoStreamEndpoint); v != "" {
		return v
	}
	if v := strings.TrimSpace(voiceDoubaoSettingSource(opts).streamEndpoint); v != "" {
		return v
	}
	return doubaostream.DefaultEndpoint
}

// transcribeFileStream is the streaming arm of transcribeFile: it resolves the
// credential from the SAME key-FILE channel the batch engine uses, streams the
// file, prints each partial as it arrives (stderr), and returns the final
// transcript so the caller's contract holds.
func transcribeFileStream(opts *voiceOptions, audioPath string) (string, time.Duration, error) {
	cred, credPath, err := voiceStreamCredentials(opts)
	if err != nil {
		return "", 0, err
	}
	o := *opts
	o.audio = audioPath // the caller's path is the authority (a spooled upload, an ssh hop)

	endpoint := voiceStreamEffectiveEndpoint(opts)
	streamOpts := doubaostream.Options{
		AppID:     cred.AppID,
		Token:     cred.Token,
		AudioFile: audioPath,
		// The endpoint is resolved on the same three channels as everything
		// else (flag > env > settings > published default), so a regional or
		// private deployment is nameable without a rebuild.
		Endpoint: endpoint,
		// The audio always arrives as a PATH that a shape already materialised:
		// the --stdio arm wraps a bare PCM stdin in a WAV header before the
		// engine sees it (voiceMaterializeAudio), the file arm hands over the
		// operator's own container, and the HTTP arm spools an upload. So the
		// streaming arm decodes a container and never has to guess a geometry
		// — which is why there is no raw-PCM flag here to get wrong.
		// THE ONE GATE of the per-partial line (Rule 2/14): --verbose (the debug
		// sink) OR --realtime (the operator's own display). Asking the fold here
		// rather than writing the disjunction at each call site is what stops the
		// three printers this file and its siblings own from drifting apart.
		OnPartial: func(p doubaostream.Partial) { voiceReportStreamPartial(p, opts.voicePrintPartials()) },
	}
	voiceReportStreamStart(cred, credPath, doubaostream.DefaultChunkMillis, endpoint, opts.voiceVerbose())

	start := time.Now()
	res, err := doubaostream.Transcribe(context.Background(), streamOpts)
	elapsed := time.Since(start)
	if err != nil {
		return "", elapsed, voiceStreamError(err, voiceLiveResultFromV2(res))
	}
	voiceReportStreamSummary(voiceLiveResultFromV2(res), opts.voiceVerbose())
	return res.Text, elapsed, nil
}

// voiceStreamCredentials resolves the credential for the streaming channel
// through the ONE resolution point (voiceStreamCredentialSource), and returns
// it with the PATH it came from ("" for an inline pair) for the messages.
//
// IT IS THE RUN SURFACE, and therefore the ONLY place the substitution and the
// file-mode announcements are printed: the pre-flight check calls the same
// resolution but prints nothing (a note emitted inside the resolver would come
// out twice on every real run — measured 2026-10-09).
//
// `--doubao-key-file` and its siblings were written for a single-line key;
// that is fine, because the rung they name is "where the credential file is",
// and the FORMAT is this channel's business (doubaostream.LoadCredentials).
// One knob, two formats, no new settings key — and now one MORE route for the
// value itself (the inline pair), because a channel whose credential could only
// live in a vendor-owned directory was the asymmetry this knob set closes.
func voiceStreamCredentials(opts *voiceOptions) (doubaostream.Credential, string, error) {
	src := voiceStreamCredentialSource(opts)
	cred, path, err := src.load()
	if err != nil {
		return cred, path, err
	}
	if note := src.fellThroughNote(); note != "" {
		fmt.Fprint(os.Stderr, note)
	}
	if path != "" {
		if note := doubaostream.CheckCredentialFileMode(path); note != "" {
			fmt.Fprintf(os.Stderr, "[voice] %s\n", note)
		}
	}
	return cred, path, nil
}

// voiceStreamCredentialLabel names WHERE the credential came from, for a
// message: a path, or the channel that carried the inline pair. It never
// carries a value.
func voiceStreamCredentialLabel(path, inlineFrom string) string {
	if path != "" {
		return path
	}
	return "inline " + inlineFrom
}

// voiceReportStreamStart says what is about to happen, on stderr: which engine,
// which channel, and where the audio goes.
//
// IT IS TWO LINES, AND THE SPLIT IS THE POINT (task 01a13500):
//
//	the PRIVACY disclosure  printed on EVERY run — the streaming arm is the only
//	                        channel whose upload the process-level announce does
//	                        not cover (runVoiceSubcommand announces `recording`
//	                        and `sauc`; for this engine the disclosure lives
//	                        here), and a default run that said nothing about the
//	                        audio leaving the machine would be LESS honest than
//	                        its two siblings, not quieter.
//	the PLUMBING note       the packet cadence, the cluster the run is billed
//	                        against and WHERE the credential was read from —
//	                        printed only under the debug switch, because that is
//	                        the half an operator reads while debugging and nobody
//	                        reads otherwise. The credential is always a PATH or a
//	                        channel name, never a value.
func voiceReportStreamStart(cred doubaostream.Credential, path string, chunkMillis int, endpoint string, verbose bool) {
	fmt.Fprintf(os.Stderr, voiceStreamPrivacyNote, config.AudioEngineStreaming, endpoint)
	if !verbose {
		return
	}
	fmt.Fprintf(os.Stderr, voiceStreamPlumbingNote, chunkMillis,
		doubaostream.DefaultCluster, voiceStreamCredentialLabel(path, voiceStreamCredentialInlineLabel))
}

// voiceReportStreamPartial prints one partial transcript as it arrives — the
// DEBUG SINK of this channel, and therefore silent unless the switch is on.
//
// WHY IT IS THE ONE THAT HAD TO BECOME OPT-IN (measured, task 01a13500): a
// 19-partial utterance printed 19 lines of `[voice] partial N at T: …` on stderr
// before the transcript, so the answer to "what was said" arrived under a wall
// of progress telemetry. The stream is not lost by this — `--verbose` gives it
// back line for line, and the FINAL transcript was never one of these lines (it
// goes to stdout).
func voiceReportStreamPartial(p doubaostream.Partial, verbose bool) {
	if !verbose {
		return
	}
	kind := voiceStreamPartialKindUngated
	if p.Definite {
		kind = voiceStreamPartialKindDefinite
	}
	fmt.Fprintf(os.Stderr, "[voice] %s %d at %.2fs: %s\n", kind, p.Index, p.At.Seconds(), p.Text)
}

// voiceReportStreamSummary reports what the run actually observed — how many
// partials arrived, when the first one did, and what the service said it
// processed. It is the channel's COUNTER block, and like the per-partial stream
// it belongs to the debug sink: the transcript and the run's own timing line are
// what the default path reports, and the figures below exist to make the
// streaming claim auditable rather than to answer "what was said".
func voiceReportStreamSummary(res *voiceLiveResult, verbose bool) {
	if !verbose || res == nil {
		return
	}
	if res.PartialCount == 0 {
		fmt.Fprint(os.Stderr, voiceStreamNoPartialNote)
	} else {
		fmt.Fprintf(os.Stderr, voiceStreamPartialCountNote,
			res.PartialCount, res.FirstPartialAt.Seconds(), res.Elapsed.Seconds())
	}
	// The service's own duration field is printed only when it carried one:
	// MEASURED, the v2 channel answers every packet with duration 0, and a
	// "service duration 0ms" line next to "10347ms of audio sent" reads as a
	// contradiction rather than as the field being unused.
	if res.ServiceDurationMS > 0 {
		fmt.Fprintf(os.Stderr, voiceStreamSentCountNote,
			res.AudioMS, res.ServiceDurationMS, res.LogID, res.ReqID)
		return
	}
	fmt.Fprintf(os.Stderr, voiceStreamSentNote, res.AudioMS, res.LogID, res.ReqID)
}

// voiceStreamError turns the streaming arm's failure into the operator-facing
// line.
//
// THE NO-SPEECH SPLIT IS THE POINT (Rule 15): code 1013 means the service
// looked at the audio and found no speech, which is a CORRECT verdict, not a
// failure of the run — so it is reported as "no speech in this audio" and never
// as an empty transcript passed off as a recognition result. Everything else
// (a SERVER_ERROR frame, a refused handshake, a peer close, a timeout) is a
// real failure and carries the vendor's code, message and log id.
func voiceStreamError(err error, res *voiceLiveResult) error {
	return voiceLiveErrorFor(config.AudioEngineStreaming, err, res)
}

// voiceLiveErrorFor turns a live run's failure into the CLI's sentence for the
// engine that produced it. The engine is a PARAMETER because the same failure has
// to name the channel it happened on: "doubao-stream engine: …" over a v3 run
// would send the operator to the wrong endpoint, which is the misdirection
// cli_voice_sauc.go's header is written against.
//
// WHAT IT KEEPS VERBATIM, and why: the vendor's own code, wording and log id are
// the three things its support asks for, so a failure that loses them starts the
// incident from zero (Rule 15). The one sentence this function adds is the CLI's
// own knowledge — which channel — plus, for the no-speech verdict, the fact that
// the verdict is about the AUDIO and not about the run.
func voiceLiveErrorFor(engine string, err error, res *voiceLiveResult) error {
	if err == nil {
		return nil
	}
	var noSpeech *doubaostream.NoSpeechError
	if errors.As(err, &noSpeech) {
		// %w, not %v: the verdict stays DETECTABLE (errors.As) so a caller can
		// branch on it — the sentence below is for the operator, the type is for
		// the code.
		return fmt.Errorf("%s engine (code %d): %w — this is the service's verdict on the audio, "+
			"not a failure of the run",
			engine, doubaostream.CodeNoSpeech, err)
	}
	var saucNoSpeech *doubaosauc.NoSpeechError
	if errors.As(err, &saucNoSpeech) {
		// THE v3 CHANNEL'S OWN NO-SPEECH VERDICT, on the same terms and through
		// the same sentence shape: the two channels report this verdict with their
		// own types (they share no application protocol), and an operator learns
		// the same thing from either.
		return fmt.Errorf("%s engine: %w", engine, err)
	}
	if res != nil {
		// The observable half is reported even when the request failed: a run that
		// streamed 4 s and lost the connection should say so — through the SAME
		// printer the v2 arms use, so the two channels' failures are comparable.
		voiceReportLiveSummary(engine, res)
	}
	return fmt.Errorf("%s engine: %w", engine, err)
}

// voiceLiveResultV2 hands the engine-neutral result to the v2 summary printer,
// which is the one a v2 run's report has always gone through. It exists so the
// v2 arms' output is byte-identical to what it was before the seam (task
// 01a13303's acceptance: the streaming path's behaviour is unchanged).
func voiceLiveResultV2(res *voiceLiveResult) *voiceLiveResult { return res }

// voiceReportLiveSummary prints the channel's own figures for a run that failed,
// on BOTH channels: how much audio went out and the vendor's log id, which is
// what the report is compared by.
func voiceReportLiveSummary(engine string, res *voiceLiveResult) {
	if res == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "[voice] %s: %dms of audio sent, logid %s\n", engine, res.AudioMS, res.LogID)
}

// voiceStreamEngineLabel is the engine name the HTTP contract reports for a
// streaming run — the vendor's product name for the channel, so a client that
// parses /health can tell the three apart.
const voiceStreamEngineLabel = "doubao-stream"

// voiceStreamDepsReady reports the first missing dependency of the streaming
// arm, or "" when the run can proceed. Like the batch arm's check it runs
// BEFORE anything is sent, so a missing credential is a one-second fix instead
// of a failure that looks like a broken endpoint. It reads the credential's
// LOCATION and format only — it never prints, logs or returns a value.
func voiceStreamDepsReady(opts *voiceOptions) string {
	if _, _, err := voiceStreamCredentialSource(opts).load(); err != nil {
		return err.Error()
	}
	// The endpoint is checked for SHAPE only: a run cannot proceed against a
	// URL that is not one, and finding that out before the microphone opens is
	// the whole point of a pre-flight check. A well-formed but unreachable host
	// is the network's report, not this one's.
	if ep := voiceStreamEffectiveEndpoint(opts); !strings.HasPrefix(ep, voiceStreamURLSchemePrefix) {
		return fmt.Sprintf("the %s engine needs a %s endpoint, and %q is not one (%s / $%s / settings %q)",
			config.AudioEngineStreaming, voiceStreamURLSchemePrefix, ep,
			voiceFlagDoubaoStreamEndpoint, config.DoubaoStreamEndpointEnv,
			config.DoubaoStreamEndpointSettingsKey)
	}
	return ""
}

// chatVoiceStreamEngineLabel is the chat surface's row for the streaming
// engine: which engine, which cluster it is billed against, and WHERE the
// credential comes from (a path, or the channel that carries an inline pair —
// never a value). It exists as one constant because the chat REPL is not a
// subcommand — it never passes through runVoiceSubcommand — and a second
// spelling at that call site is how two surfaces come to describe different
// routes to the same engine. The ENDPOINT is in the row for the same reason the
// cluster is: both are answers to "where did my audio go".
const chatVoiceStreamEngineLabel = "[voice] streaming engine %s → %s: cluster %s, credential %s\n"

// chatVoiceSaucEngineLabel is the chat surface's row for the v3 SAUC engine:
// which engine, where the AUDIO GOES, the product line it is billed against,
// and WHERE the credential came from — the file path the shape was read from,
// or the settings/env channel that carries the pair (never a value) — and the
// ORIGIN of the endpoint (%s), so a value written for one arm can be seen
// overriding the other. It exists for the same reason as the row above — the
// chat REPL is not a subcommand and never passes through runVoiceSubcommand, so
// a second spelling at that call site is how two surfaces come to describe
// different routes to the same engine.
//
// MERGE NOTE (union of 79190ad05 and 34883c049): 79190ad05 renamed the last
// field to the channel-agnostic `credential %s` (a path OR the settings/env
// channel); 34883c049 added the (%s endpoint) field. Orthogonal, both kept.
const chatVoiceSaucEngineLabel = "[voice] sauc engine %s → %s (%s endpoint): resource %s, credential %s\n"

// voiceStreamCredentialLabelForOpts is the chat surface's credential label: the
// path when a FILE will be used, else the channel carrying the inline pair —
// resolved through the same ONE source the run itself uses (Rule 2), so the
// notice cannot describe a route the request would not take.
func voiceStreamCredentialLabelForOpts(opts *voiceOptions) string {
	src := voiceStreamCredentialSource(opts)
	for _, path := range src.fileCandidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if src.inlineAppID != "" && src.inlineToken != "" {
		return "inline " + src.inlineFrom
	}
	return src.fileCandidates[0]
}
