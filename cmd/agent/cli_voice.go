package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/toolnames"
)

// isVoiceSubcommand reports whether argv begins with the voice_input verb.
// When it does it runs the subcommand and returns true (the caller must not
// treat the remaining args as an input file).
func isVoiceSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == toolnames.ToolVoiceInput {
		runVoiceSubcommand(args[1:])
		return true
	}
	return false
}

// voice_input — Phase 2 of the voice-input feature (see
// devdoc/SPEC-voice-input-phase23.md).
//
// Engine: Fun-ASR-Nano via the official FunASR llama.cpp runtime. Chosen on
// measurement: same 220s Chinese clip, same scorer — CER 1.58% / 71s against
// whisper-small's 5.14% / 209s and whisper-medium's 2.77% / 738s, at 910MB of
// weights instead of 487MB/1463MB.
//
// Two server shapes, both implemented so they can be compared:
//
//	file   — one-shot: gopi tool voice_input <audio>            (simplest, always works)
//	A      — HTTP:    gopi tool voice_input --server                 (loopback by default)
//	                  gopi tool voice_input --server --listen :7788    (explicit LAN opt-in)
//	B      — pipe:    gopi tool voice_input --server --stdio      (reads PCM on stdin)
//
// A and B share transcribeFile(); only the transport differs.

const (
	voiceDefaultChunkSec = 15
	// voiceDefaultListen is the HTTP server's DEFAULT bind address, and it is
	// loopback ON PURPOSE: POST /transcribe accepts unauthenticated multipart
	// audio uploads, so a wildcard default would turn "run it with no flags"
	// into a world-reachable transcription service on any shared or untrusted
	// network. The sibling listeners in this package make the same choice
	// (cli_web.go webDefaultAddrValue, cli_proxy.go parseProxyArgs — both
	// 127.0.0.1:8090). Serving the LAN stays possible, but only as an explicit
	// opt-in: `--listen :7788` / `--listen <this-machine>:7788`, documented in
	// voiceInputUsage.
	voiceDefaultListen = "127.0.0.1:7788"
	// voiceDefaultListenPort is the port half of voiceDefaultListen, named so
	// a test can pin the port without duplicating the literal (Rule 1).
	voiceDefaultListenPort = "7788"
	// voiceLoopbackHost is the host the capture client assumes when no
	// --server-url is given: the same machine.
	voiceLoopbackHost = "127.0.0.1"
	// voiceDefaultServerURL is the --server-url default, assembled from the
	// host/port the server actually binds — a client default that drifts from
	// the server's address is a connection refused with no clue why (Rule 2).
	voiceDefaultServerURL = voiceSchemeHTTP + voiceLoopbackHost + voiceHostPortSeparator + voiceDefaultListenPort
	// voiceHostPortSeparator splits host from port in a URL authority.
	voiceHostPortSeparator = ":"
	voiceMaxAudioBytes     = 512 << 20 // 512 MiB — generous, but bounded
	// voiceMultipartFramingSlack is the allowance added on top of the audio
	// budget for the multipart envelope (boundary delimiters, part headers and
	// filename) a client wraps around the file part.
	voiceMultipartFramingSlack = 8 << 20 // 8 MiB
	// voiceMaxUploadBytes caps the REQUEST BODY of POST /transcribe before the
	// multipart parser spools anything (http.MaxBytesReader) — the audio
	// budget plus the framing the client adds around it.
	voiceMaxUploadBytes = voiceMaxAudioBytes + voiceMultipartFramingSlack

	// voiceLegacyModelRoot / voiceLegacyRuntime spell RUNG 2 of the local
	// engine's deployment search: the $HOME/work directory pair that was the only
	// answer until this change. gopi never creates it and does not maintain it —
	// it is a deployment that happens to exist on the host where the weights were
	// first installed (measured: absent entirely on a client host) — so it is now
	// the COMPATIBILITY rung, read only when rung 1 (voiceProfileVoiceDir) does
	// not hold the deployment, and announced with a migration line when it
	// answers (voiceLegacyDeploymentNote). The names say "legacy" on purpose: a
	// reader who sees `voiceLegacyModelRoot` presented as the default would be
	// reading a lie about which rung decides.
	voiceLegacyModelRoot = "work/funasr-models"
	voiceLegacyRuntime   = "work/runtime/llama.cpp/build/bin/llama-funasr-cli"

	// voiceProfileDirName is the subdirectory of the operator's gopi PROFILE
	// (config.GopiDir — $GOPI_HOME/.gopi when GOPI_HOME is set, else ~/.gopi)
	// that the LOCAL weights are looked for in. It is RUNG 1 of the deployment
	// search and therefore the DEFAULT location: the profile is the one place in
	// this story gopi owns and maintains, so a host that puts the deployment
	// there needs no flag and no directory outside its own profile.
	voiceProfileDirName = "voice"
	// voiceProfileRuntimeName is the runtime's file name inside
	// <profile>/<voiceProfileDirName>/ — the same binary the legacy rung spells
	// as voiceLegacyRuntime, resolved from the profile instead of $HOME/work.
	voiceProfileRuntimeName = "llama-funasr-cli"

	// voiceLocalDefaultSeconds is the DEFAULT capture length of the bare
	// `gopi tool voice_input` (microphone → local engine, one command). It is a
	// bound, not a preference: the recorder is stopped by the clock, so a
	// forgotten flag cannot leave the microphone open. Named (Rule 1) because
	// the usage text, the parser and the recording all state the same number.
	voiceLocalDefaultSeconds = 10

	// voiceLocalDeploymentFileCount is how many files the local engine's
	// deployment is: the two GGUFs. Named (Rule 1) because the install guidance
	// states the number the check walks.
	voiceLocalDeploymentFileCount = 2

	// The two weight files inside the models directory. Named (Rule 1) because
	// they are a cross-file contract: the engine is handed these paths, and a
	// test that builds a satisfying models dir must not duplicate the literal.
	voiceEncoderName = "funasr-encoder-f16.gguf"
	voiceLLMName     = "qwen3-0.6b-q4km.gguf"
)

// The shape names voiceRunShape renders and the refusal messages quote. Named
// (Rule 1b) because the shape switch, the error text and the usage text must
// name the same shape — three spellings of "--server" is the drift Rule 13
// closes.
const (
	voiceServerShapeName      = "--server"
	voiceListDevicesShapeName = "--list-devices"
	// voiceClientShapeName names the shape that CAPTURES and POSTS its audio.
	// It has no transcript of its own (the far side produces it), which is why
	// the flags that only the local transcribing shapes can honour are refused
	// by this name (see the capture-only refusal arms in parseVoiceArgs).
	voiceClientShapeName  = "--client"
	voiceCaptureShapeName = "capture"
	voiceFileShapeName    = "file-transcription"
)

const voiceInputUsage = `GoPi voice input — speech-to-text, by DEFAULT in the cloud.

Engine: the DEFAULT is {engineDefault} — 豆包/Volcengine 录音文件识别,
volc.seedasr.auc: no model, no runtime, the audio is UPLOADED and the transcript
comes back. It is the default because the burden is not symmetric — the local
engine needs 1.7GB of weights on disk (910MB loaded per run) plus a runtime
binary, and a default is what a machine with nothing deployed gets. Every run
that resolves to it says so on stderr, naming the endpoint and how to switch
back. The local arm is the other half of the same decision: it is not a
dependency gopi can fetch (the weights and the runtime are ~914MB and a foreign
binary), so a machine without them is told what to install, or to name the
engine that needs nothing.

  --engine {engineDoubao}    the DEFAULT; 0B of weights, 0B of runtime, audio uploaded
                     Needs an API key: settings.json
                     "audio_engine_param.recording.api_key" (inline, alongside
                     the vision credential), or a key FILE named by
                     --doubao-key-file / $GOPI_AUDIO_KEY_FILE / settings.json
                     "audio_engine_param.recording.key_file", or the legacy
                     ~/.doubao/apikey.txt. A missing key is refused BEFORE any
                     audio is sent, and the
                     refusal names the way back to --engine {engineLocal}. This
                     engine is the BATCH shape (录音文件识别, volc.seedasr.auc):
                     it waits for the whole file and returns one transcript.
                     The v3 shape below (sauc) is a SEPARATE product line from
                     the v2 realtime channel, and — measured 2026-10-13 — it is
                     granted on this account once its credential is the legacy
                     APPID/TOKEN pair: the single API key this engine takes is
                     refused there with HTTP 403 "requested resource not
                     granted", which reads like a missing entitlement and is
                     not one. See --engine {engineSauc}.
  --engine {engineStream}  the STREAMING shape: the same vendor, the OTHER
                     protocol. One WebSocket to api/v2/asr (cluster
                     volcengine_streaming_common), a 200ms audio packet paced
                     against the audio's own timeline, and a PARTIAL transcript
                     arriving WHILE you are still speaking — the channel to use
                     when waiting for the end of a recording is the problem.
                     Needs the same vendor account but a KEY=VALUE credential
                     file (an APPID line and a TOKEN line) named by
                     --doubao-key-file / $GOPI_AUDIO_KEY_FILE / settings.json
                     "audio_engine_param.streaming.key_file" — or the same pair
                     INLINE, in settings.json
                     "audio_engine_param.streaming.app_id" / ".token" (also
                     $GOPI_AUDIO_APP_ID / $GOPI_AUDIO_TOKEN), which is what
                     answers when no such file is there; the file's default is
                     ~/.doubao/v2_voice.env. A missing credential is refused
                     before anything is sent, and the credential's VALUE is
                     never printed. It is NEVER
                     the default: streaming is reached by naming it, so a
                     machine that deployed nothing still transcribes.
  --engine {engineSauc}      the v3 STREAMING shape: the same vendor and the same
                     JOB as {engineStream} above — one WebSocket, the transcript
                     built from audio that is still being sent — on the NEWER
                     protocol (api/v3/sauc, resource volc.seedasr.sauc.duration,
                     豆包流式语音识别模型 2.0) and with a DIFFERENT credential.
                     THE CREDENTIAL SHAPE IS THE WHOLE DIFFERENCE, and it is
                     measured rather than documented: this endpoint answers the
                     single API key ({engineDoubao}'s shape) with HTTP 403
                     "requested resource not granted", and the LEGACY CONSOLE
                     PAIR (X-Api-App-Key + X-Api-Access-Key: the APPID and TOKEN
                     lines of the same KEY=VALUE file this feature already uses)
                     with 101 Switching Protocols. A 403 there is a SHAPE
                     mismatch, not a missing entitlement — which is why the
                     refusal names the shape instead of echoing the status.
                     TWO ENDPOINTS, ONE PER ARM, and the difference is about the
                     WORK: a FILE (this tool's shape) opens bigmodel_nostream —
                     the batch-semantics one, which answers once at the end — and
                     the /voice LIVE arm in the chat verb opens
                     bigmodel_async (the BIDIRECTIONAL one), so the text
                     appears WHILE you speak
                     (measured 2026-10-13: first text ~2.5 s into a 4.8 s clip,
                     then rewritten every ~300 ms until the sentence closed).
                     The setting audio_engine_param.sauc.endpoint overrides
                     BOTH arms, and every run SAYS WHICH of the two produced
                     the URL it is about to open ("from settings …" or "from
                     the engine default") — so a value written for one arm can
                     be seen overriding the other. When the value is not the
                     bidirectional endpoint, the run also states that a LIVE
                     session on it will answer only once, at the end.
                     Knobs: settings.json "audio_engine_param.sauc.endpoint" /
                     ".resource_id"; the credential PAIR itself goes in the same
                     block's ".app_id" / ".token" (or $GOPI_AUDIO_APP_ID /
                     $GOPI_AUDIO_TOKEN over it) — the SAME two settings the
                     {engineStream} block uses for the same vendor pair, and the
                     reason a gopi deployment no longer depends on the vendor's
                     own directory. A key FILE named on an explicit channel
                     (--doubao-key-file / $GOPI_AUDIO_KEY_FILE / settings
                     "audio_engine_param.recording.key_file") still OUTRANKS the
                     inline pair, and the legacy file ~/.doubao/v2_voice.env is
                     the fallback — a run that ends on it says so, naming the path
                     and the settings that supersede it. Half a pair is REFUSED by
                     name before any audio moves. No credential VALUE is ever
                     printed: messages carry the origin (a path, a settings key or
                     an env name), and 'gopi config show' renders the pair as a
                     '<set: N chars>' shape.
                     Like {engineStream} it is NEVER the default.
  --engine {engineLocal}     Fun-ASR-Nano (SAN-M encoder + Qwen3-0.6B) on this machine
                     via the official FunASR llama.cpp runtime; the 910MB of
                     weights never leave the disk and no audio leaves the
                     machine. Same clip, same scorer: CER 1.58% at 0.32x
                     realtime, against whisper-small's 5.14%/0.95x and
                     whisper-medium's 2.77%/3.4x.

Usage:
  gopi tool voice_input
      THE DEFAULT SHAPE: speak, and read the transcript. Records
      {seconds}s from this machine's microphone — the device and the tier
      that chose it are printed — and transcribes it with the selected
      engine — {engineDefault} by default, {engineLocal} on request. One
      command, no hop for the local engine: the same process is capture client
      and transcriber, which is what the machine holding both the microphone
      and the weights wants. Its recording is BOUNDED by that length ({seconds}s
      by default; --seconds N changes it), so a forgotten flag cannot leave
      the microphone open. With --engine {engineLocal} the weights need no flag
      either when they are deployed in the place gopi looks first — its OWN
      profile:
        <gopi profile>/voice                RUNG 1: config.GopiDir
                                           + "voice", i.e. $GOPI_HOME/.gopi/voice
                                           with GOPI_HOME set, ~/.gopi/voice
                                           otherwise. The runtime
                                           (llama-funasr-cli) is looked for in
                                           the same directory.
        $HOME/work/funasr-models            RUNG 2, the LEGACY deployment: read
                                           only when rung 1 holds no complete
                                           deployment, and the run then prints a
                                           migration note saying so — gopi does
                                           not build, ship or maintain anything
                                           in $HOME/work. The runtime there is
                                           the same llama-funasr-cli, under
                                           $HOME/work/runtime/llama.cpp/build/
                                           bin/ — the two move together.
      When neither rung holds the deployment, the run does NOT load anything:
      it names the missing file and prints the two files, the binary and the
      directory to put them in — plus the way to need none of them
      (--engine {engineDoubao}). The local engine is 914MB of weights and a
      ~6MB foreign runtime, so "not installed here" is a normal answer, not a
      broken one.
      Its own recorder, device policy and dead-device reporting are the
      --client ones, so a device that cannot be opened is named here exactly
      as it is there. For the client/server split (capture on one host,
      transcribe on another) use --client and --server below; this shape
      never opens a port.

  gopi tool voice_input <audio-file> [--chunk N]
      Transcribe one audio file and print the text. The file must be one of
      the containers the engine's decoder has compiled in: {formats}. Any
      other container (ogg/vorbis among them) is refused BEFORE/while it is
      decoded — measured, not inferred: the runtime answers "audio: failed
      to open/decode <path> (supported: {formats})". The decoder sniffs the
      BYTES, not the file name: a real Vorbis stream is refused however it is
      named, and bytes already in one of the {formats} containers decode
      however they are named. A container-less input is refused on THIS
      channel (the path goes straight to the engine, which cannot read raw
      PCM) — the bytes still work piped through the --stdio shape below,
      which wraps them in a WAV header first.
      NOTE ({engineDoubao}): that cloud engine takes whatever the service
      accepts — the local runtime's set above does NOT bound it.

  gopi tool voice_input --server
      HTTP server on 127.0.0.1:7788 (loopback only). POST /transcribe with
      multipart field "audio" → JSON {"text":...,"elapsed_ms":...}. Any
      client on THIS machine can talk to it. The upload is spooled with the
      client's own filename suffix and handed to the engine by PATH — so
      this channel accepts the SAME set as the file channel ({formats}):
      a container-less upload is refused too, with the same engine line.
      (Over ssh without a port, use --server --stdio below — that is the
      shape that takes bare PCM.)

  gopi tool voice_input --server --listen :7788
      Same server, bound to every interface — the explicit LAN opt-in, for
      the "server on the big machine, client on the laptop" case. The
      endpoint has NO authentication and accepts audio uploads of up to
      512 MiB per request, so only do this on a network you trust (or put
      it behind ssh — see --server --stdio below, which needs no port).
      Unreachable from other hosts unless this is passed.

  gopi tool voice_input --server --stdio
      Pipe server. Reads raw 16kHz mono f32 PCM on stdin until EOF, writes
      the transcript to stdout. Intended for:  ssh host gopi tool voice_input \
      --server --stdio < audio.pcm — zero new ports, reuses existing ssh.
      CONTAINER-LESS BY DESIGN: this channel wraps a bare PCM stream in a
      WAV header (voiceMaterializeAudio) and then decodes it, so the same
      bytes that a .pcm FILE argument is refused for are ACCEPTED here.
      A stream that already carries one of these containers ({formats}) is
      passed through unchanged and decoded the same way — i.e. this channel
      takes both shapes, while the file channel takes containers only. A
      stream in some OTHER container (Ogg/Vorbis is the measured case) is
      also passed through unchanged, and the engine refuses it: this channel
      rescues container-LESS bytes, it does not add decoder support.

  gopi tool voice_input --client [--device ID] [--seconds N]
      Capture client. Records from a microphone and posts it to a running
      transcriber (see --server-url), printing the transcript. Needs no
      models locally, so it runs on the machine that has the microphone.

  gopi tool voice_input --list-devices
      Print the capture devices this host can offer.

  gopi tool voice_input --client --list-devices
      List capture devices with stable IDs (pulse:/alsa: prefixed) to pass
      to --device. PulseAudio defaults come first, then ALSA plughw/hw
      pairs. A bare Pulse id is stable; an ALSA hw:/plughw: id is addressed
      by CARD NAME, not card number, so it survives renumbering.

  gopi tool voice_input --client --seconds N --stdio
      Same capture, but raw 16kHz mono f32 PCM on stdout instead of a POST
      (f32 is what the server's --stdio shape reads), for a transcriber on
      another machine:
        gopi tool voice_input --client --seconds 5 --stdio \
          | ssh host gopi tool voice_input --server --stdio

  gopi tool voice_input --client --audio-source FILE [--seconds N]
      The same capture client, fed from a RECORDING instead of the
      microphone: FILE is read at the pace it was spoken and driven through
      exactly the same capture path — the window boundary, the WAV the engine
      is handed, the forward to a server — so a recording can reproduce a
      session that a device produced. FILE is bare PCM or a WAV of the capture
      contract (16kHz mono s16le); a container of any other geometry is
      REFUSED BY NAME (a 44.1kHz stereo file is not a near miss, it is a run
      whose every window is the wrong length). The flag works on the bare
      invocation too, and both shapes then open NO capture device at all, so it
      is also how this verb runs on a host with no sound card.

  Which device the client records from is decided by ONE fixed order, first
  match wins:
    1. --device ID            explicit; a bad id is a hard error — nothing
                              else is tried, because you named a device
    2. configured default     $GOPI_VOICE_DEVICE, or settings.json
                              "default_device". A device that is no longer
                              present is reported, then the search continues
    3. system default capture ALSA's own "default" device, considered only
                              when no sound server answers — with a server
                              running, ALSA's default routes back to that same
                              server, so the tier would add nothing
    4. first enumerated       the first entry of --list-devices (the Pulse
                              default source, else the first ALSA pair)
  Every run prints the device it chose and the tier that chose it, e.g.
  [explicit --device] / [configured default] / [system default capture] /
  [first enumerated]. The chosen device is opened for 1s before the real
  recording: a device that disappeared between listing and capture (unplugged,
  or taken by another process) is reported BY NAME with a --list-devices hint
  instead of failing after you have already spoken. An input that opens but
  produces no audio is a warning, not an error (a muted mic is a real device).

Flags:
  --engine NAME    transcription engine: "{engineDoubao}" (Volcengine cloud
                   batch, DEFAULT — the audio is uploaded, 0B of models),
                   "{engineStream}" (Volcengine v2 real-time streaming —
                   partials while you speak, never the default, see the engine
                   note above), "{engineSauc}" (Volcengine v3 SAUC — the same
                   streaming job on the newer protocol, and the one whose
                   credential must be the legacy APPID/TOKEN pair; never the
                   default) or "{engineLocal}" (Fun-ASR-Nano, the 910MB of
                   weights, nothing leaves the machine; see the engine note
                   above).
                   Flag > $GOPI_AUDIO_ENGINE > settings.json "audio_engine" >
                   {engineDefault}. An unknown name is refused, never silently
                   ignored. The PRE-RENAME spelling — "voice_engine" /
                   $GOPI_VOICE_ENGINE on the selector, "doubao" and
                   "doubao-stream" as engine NAMES — is still read and still
                   selects the same engine; a run that finds one says so on
                   stderr and names the replacement.
  --doubao-key-file PATH  file holding the doubao API key (default
                   ~/.doubao/apikey.txt; also $GOPI_DOUBAO_KEY_FILE or
                   settings.json "doubao_key_file"). A FILE named here still
                   outranks the inline key below. Neither path nor value is
                   ever printed or logged.
  --doubao-resource ID    doubao X-Api-Resource-Id (default
                   volc.seedasr.auc = 录音文件识别 2.0). Always sent
                   explicitly — a client-side default is how the official SDK
                   turns a working call into a 403 (measured).
  --doubao-hotwords LIST  comma-separated hotwords (request.corpus.context)
  --doubao-base-url URL    the doubao BATCH endpoint ROOT, from which both AUC
                   URLs derive (also $GOPI_AUDIO_API_BASE_URL or settings.json
                   "audio_engine_param.recording.api_base_url"; default the public
                   openspeech.bytedance.com/api/v3/auc/bigmodel). One root, so
                   the submit and the query can never be pointed at two hosts.
  --doubao-submit-url URL  --doubao-query-url URL
                   name ONE of the two AUC URLs outright, overriding the root.
                   Named, never inferred: a private endpoint is an explicit
                   choice.
  --doubao-stream-endpoint WSS_URL  the v2 realtime channel's endpoint
                   ($GOPI_AUDIO_STREAM_ENDPOINT / settings.json
                   "audio_engine_param.streaming.endpoint"; default the public
                   wss://openspeech.bytedance.com/api/v2/asr).
  --client         capture audio on THIS machine and print the transcript from
                   a transcriber elsewhere (--server-url). The bare invocation
                   above is the same capture + a LOCAL transcription, so
                   --client is the REMOTE shape: use it when the engine lives
                   on another host, or with --stdio to pipe the PCM to one
  --device D       capture device string (default "default" — the plug chain;
                   raw hw:/plughw: spellings are refused while a sound server
                   holds the PCM, so prefer default/pulse/plughw:CARD=…).
                   This applies to the bare invocation too
  --audio-source FILE  capture from a FILE instead of a microphone. FILE is
                   the audio of a recording in the capture contract — bare PCM,
                   or a WAV declaring 16kHz/mono/16-bit; anything else is
                   refused with the geometry it declares (resample with
                   ffmpeg -i IN -ar 16000 -ac 1 -f s16le OUT.pcm). It is read
                   at its own real-time rate, so the run has the same shape as
                   a live one. Read by the shapes that RECORD — --client (and
                   its --stdio form) and the bare invocation — because it
                   replaces the microphone those shapes open; the file shape is
                   given a file as its argument instead, and a --server or
                   --list-devices run opens no capture at all. A path that is
                   missing, empty or a directory is refused at parse time,
                   before anything is opened.
  --seconds N      capture length (default {seconds}, max 600). It bounds every
                   capturing shape — --client, and the bare invocation, whose
                   default it IS — so the recorder is always stopped by a
                   clock and never by a person remembering to press Ctrl-C
  --out FILE       with --client: write the captured audio to FILE as a WAV
                   (16kHz mono PCM16) before it is forwarded, and print the
                   path. The recording is then evidence you can play, inspect
                   or re-transcribe — a failed forward (server down, a
                   mistyped server URL, a refused device) no longer costs the
                   audio. REFUSED: an empty path, a directory, and an existing
                   file (a capture silently overwriting yesterday's recording
                   is a data-loss bug, not a convenience). A run that wants to
                   overwrite deliberately uses --record instead, which works on
                   the bare capture shape too.
  --record FILE    keep this capture's audio in FILE as a WAV (16kHz mono
                   PCM16) and print the path. Same bytes the engine was handed,
                   so the transcript and the recording can be compared. Unlike
                   --out it is NOT restricted to --client (the bare invocation
                   records too) and an EXISTING file is overwritten ON PURPOSE
                   — the flag was given to store this run's audio under that
                   name, and re-recording one fixed utterance name is what it
                   is for; the write is announced with its byte count. Refused:
                   an empty path and a directory.
  --chunk N        seconds per transcription window (default 15). NOT
                   optional for long audio: without chunking a 220s clip is
                   fed as one block and the model emits degenerate output
                   while taking 8x longer (measured: 567s and a page of
                   repeated characters, vs 71s and correct text). It windows a
                   file transcribed HERE, so it is refused on the bare capture
                   (there is no file) and with --client / --list-devices
                   (neither transcribes a file in this process).
  --server         run as the long-running HTTP transcriber instead of the
                   one-shot shapes. It is the EXPLICIT opt-in: the bare
                   invocation transcribes in-process, so a server is only ever
                   started because it was asked for by name. It resolves the
                   engine on its OWN three channels and, when that is {engineDefault},
                   prints the same privacy disclosure before it arms — this is
                   the process that performs the upload on behalf of its
                   clients, so it is the one that announces it
  --listen ADDR    HTTP listen address for --server. Default 127.0.0.1:7788
                   (loopback). Pass "--listen :7788" to serve the LAN, or an
                   explicit address to pick one interface. Passing it IMPLIES
                   --server: an address to listen on has no other meaning, and
                   a --listen that was silently ignored was the one way to be
                   surprised by this verb
  --stdio          pipe mode (--server: read f32 PCM on stdin, mutually
                   exclusive with --listen; --client: write f32 PCM on
                   stdout — the two ends of one ssh hop). The --server end is
                   the channel that takes container-less PCM: it WAV-wraps
                   the stream (see --server --stdio above), so a .pcm file
                   refused as a file argument is accepted here
  --models DIR     model directory. Unset, the deployment is searched in two
                   rungs — the gopi profile's voice/ directory first (the
                   default; see the bare-invocation note above), then the legacy
                   $HOME/work/funasr-models. Naming it SKIPS the search and uses
                   exactly this directory. Read by the shapes that transcribe
                   HERE (a file run, the bare capture, a server); refused with
                   --client and --list-devices, which open no weights here
  --runtime PATH   llama-funasr-cli path (same rungs, same override, same reach
                   as --models)
  --client         capture from a microphone and forward to a transcriber
  --list-devices   with --client: list capture devices and exit. Ids are
                   stable by NAME (pulse:<source name> / alsa:plughw:CARD=
                   <card name>,DEV=<n>), so they survive renumbering
  --device ID      with --client: record from this device, bypassing the
                   selection order above. Use an id exactly as --list-devices
                   prints it; a CARD INDEX ("1", "hw:1,0") is refused, because
                   it is kernel probe order and picks a different device after
                   a reboot
  --seconds N      with --client: recording length in seconds (default 5)
  --server-url URL with --client: base URL of the transcriber, without the
                   /transcribe path (default http://127.0.0.1:7788). Ignored
                   with --stdio.
  --self           TALK INTO YOUR OWN WINDOW. Type the transcript into the
                   window that HOSTS this gopi process (resolved by tty → pty
                   master → _NET_WM_PID, the same chain "gopi tool capture --self"
                   uses), instead of only printing it. The transcript is typed
                   verbatim and is NOT submitted: no Return is pressed, so the
                   text sits in the input box for review — at CER 1.58% roughly
                   one character in sixty is wrong, and a wrong technical term
                   costs more than a wrong character. Send it with
                     gopi tool act key --key return --self
                   The text still goes to stdout as well, so a caller reading
                   the command's output is not deprived of it.
                   Refused on --server and --stdio (a pipe has no window, and
                   an ssh hop's two ends do not share one), and on
                   --list-devices (there is no transcript). A transcript
                   carrying a character this client cannot press is refused
                   BEFORE anything is typed — do not lose the text on stdout.
  --display DISPLAY  X11 display for --self (default: $DISPLAY). Same spelling
                   as "gopi tool act --display" — and, like it, refused on a
                   run that never types (there is no --self to address)
  -v, --verbose    print the streaming DEBUG SINK instead of only the answer.
                   On the {engineStream} and {engineSauc} arms this adds one line
                   per partial ("[voice] partial N at T: …"), the channel's
                   counters (how many partials, when the first and last arrived,
                   how much audio went out, the vendor's logid/reqid), and — on
                   the microphone arm — the note that --seconds bounds the
                   UTTERANCE there rather than being a recording window.
                   Default OFF: the transcript on stdout and the ordinary
                   per-run line ("… in 12.4s") are what a run prints either way,
                   so this flag ADDS a stream and never changes the answer. Its
                   own channels, in precedence order: this flag > the env name
                   GOPI_AUDIO_VERBOSE > the settings key "audio_verbose" (the
                   channel for a headless run that cannot pass a flag).

Examples:
  gopi tool voice_input                                  speak, read the transcript
                                                    ({engineDefault} engine by default,
                                                    {engineLocal} keeps it local)
  gopi tool voice_input --seconds 5                       a shorter window
  gopi tool voice_input talk.wav                         transcribe an audio file
  gopi tool voice_input long-interview.wav --chunk 15    long audio, chunked
  gopi tool voice_input --server --listen :7788          start the HTTP server
  gopi tool voice_input --client --list-devices          which microphones exist
  gopi tool voice_input --client --seconds 8             record; transcribe on
                                                    the host named by --server-url
  gopi tool voice_input --client --seconds 8 --out nick.wav  …and keep the WAV
  gopi tool voice_input --client --self --seconds 8      speak, and it lands in the
                                                    window you ran this in
  gopi tool voice_input --client --self --seconds 8 --display :57   …on another X11
                                                    display (same as act)
  gopi tool act key --key return --self                  send the reviewed text
  gopi tool voice_input --client --server-url http://192.168.1.6:7788 --seconds 8
  ssh host gopi tool voice_input --server --stdio < a.pcm   pipe over an ssh hop
  gopi tool voice_input clip.ogg                 refused: ogg is not in {formats}
  gopi tool voice_input clip.pcm                         refused; pipe it instead:
  gopi tool voice_input --server --stdio < clip.pcm      the container-less shape

Capture route: the recorder is spawned (arecord for ALSA devices, parecord
for Pulse), not linked in — both are stock alsa-utils/pulseaudio-utils, so
the capture side adds no dependency to this binary. Audio is captured as
16kHz mono signed-16-bit LE, the contract the transcriber expects; the
recorded container is built to match rather than resampled afterwards.

Voice input is a CLI verb only — no agent tool: transcription happens BEFORE
the model sees anything, and the user reviews the text first.

The transcript is meant to land in the user's input box for review before
sending, not to be submitted automatically — at CER 1.58% roughly one
character in sixty is wrong, and a wrong technical term (RAG→Rug) costs more
than a wrong character.
`

// voiceOptions is the parsed invocation.
type voiceOptions struct {
	audio   string
	chunk   int
	server  bool
	listen  string
	stdio   bool
	models  string
	runtime string
	// modelsExplicit / runtimeExplicit record that a path was NAMED — by
	// --models/--runtime, or by the configured local block
	// (audio_engine_param.local.models_dir / .runtime_bin, or their env
	// channels). A named path is outside the deployment search
	// (voiceResolveDeployment): the two rungs only fill in what nobody answered,
	// because re-pointing a path the operator stated is the run reading the
	// wrong weights while reporting the intended ones.
	modelsExplicit  bool
	runtimeExplicit bool
	// modelsFromConfig / runtimeFromConfig say the path came from configuration
	// rather than from the flag. The search treats the two identically (both are
	// explicit); what differs is only WHICH channel a message names when it
	// explains where the deployment came from.
	modelsFromConfig  bool
	runtimeFromConfig bool

	// audioSource names a FILE of already-recorded audio to capture from
	// instead of the microphone (--audio-source). Empty means "a device", which
	// is every run that does not name one. See cli_voice_audio_source.go.
	audioSource string

	// Capture client (--client). See cli_voice_client.go.
	//
	// merge-note: master shipped a parallel capture client in
	// cli_voice_capture.go (failure-mode matrix + --out/--recorder flags).
	// Both cannot coexist (duplicate runVoiceClient / voiceCaptureChannels),
	// so this branch's verified client is the one wired in; master's unique
	// value — the named failure modes — is being carried over into it (merge
	// follow-up task). Master's `--list-devices` spelling IS kept: it is a
	// top-level verb, not only a --client sub-flag.
	client      bool
	listDevices bool
	device      string
	seconds     int
	// capture records from the microphone and transcribes the result ON THIS
	// MACHINE — the bare `gopi tool voice_input` shape, and the ONE place the
	// local engine is reached through the capture path. It is set by the
	// absence of both a positional file and --server (the parser's own
	// default), so a run that names nothing captures; nothing about it is
	// remotely configured, which is exactly why it needs no flag.
	capture bool
	// target is the base URL of a running transcriber for --client without
	// --stdio; the /transcribe path is appended by voiceTranscribeURL.
	target string
	// out is the client's debug file: the captured PCM16 is written to it as a
	// playable WAV before anything is forwarded, so a client-side failure
	// (unreachable transcriber, refused device) still leaves the recording on
	// disk to inspect. See voiceWriteCaptureFile.
	//
	// recorder belongs to master's capture client (--recorder,
	// cli_voice_capture.go), which is NOT part of this resolution — the
	// duplicate cannot link against this branch's client — so nothing assigns
	// it and the flag stays unadvertised (an unknown flag is a loud error,
	// better than a silent no-op). Kept so the failure-mode module ports over
	// without editing this struct. merge-note: restore the --recorder wiring
	// together with the failure-mode matrix.
	out      string
	recorder string
	// record is the file a capture's audio is kept in (--record): a playable WAV
	// of the PCM the engine was handed, so the run that produced a transcript can
	// be listened to again — by the operator, and by whatever scores it. It
	// applies to the bare capture shape and to --client (see voiceFlagRecord for
	// why it is not --out).
	record string

	// self types the transcript into the window that HOSTS this process (see
	// voiceTypeIntoOwnWindow) instead of only printing it. It is the same
	// addressing statement `gopi tool act type --self` makes, and it reuses that
	// verb's path rather than opening a second injection route.
	self bool
	// display is the X11 display addressed when self is set ("" → $DISPLAY).
	display string

	// verbose is the DEBUG SINK of the streaming arms: the per-partial
	// transcript stream and the channel's counters (partial count, the
	// first/final partial instants, the vendor's logid/reqid, how much audio
	// went out) plus the live-shape note. It is set by --verbose/-v, and the
	// value written HERE is the flag rung of a three-channel resolution
	// (config.ResolveAudioVerbose, the ONE reader): the parser records "the
	// flag was passed" and runVoiceSubcommand resolves the rest once, so no
	// printer re-reads the environment.
	//
	// THE DEFAULT PATH IS UNCHANGED: the transcript and the ordinary per-run
	// lines (the engine/timing line, the first-text report) print exactly as
	// they did before this knob existed — the switch can only ADD a stream.
	verbose bool

	// Engine selection (--engine local|recording|streaming, plus the
	// pre-rename names doubao/doubao-stream) and the cloud engines' knobs.
	// engine is "" until resolved (voiceResolveEngine): an empty value means
	// "nobody chose" and resolves to the terminal default (AudioEngineDefault —
	// the cloud BATCH engine, which needs no deployment), while the other two
	// are reached because they were asked for by name or configured.
	//
	// The cloud knobs follow the same three rungs as every other voice knob
	// (flag > env > settings > named default); the resolver fills them once,
	// at run start, so transcribeFile needs only this struct. See
	// devdoc/doubao-asr-integration.md §引擎分派.
	engine string
	// engineRaw is the FLAG rung's value exactly as typed, before the
	// pre-rename translation. It exists for one reader (the migration notice,
	// cmd/agent/cli_voice_deprecation.go): a translated value cannot say which
	// spelling the operator used, and reporting the translation back to them
	// would be a notice about nothing.
	engineRaw      string
	doubaoKeyFile  string
	doubaoResource string
	doubaoHotwords string
	// doubaoSubmit / doubaoQuery override the two endpoint URLs. They exist
	// for the same reason the resource_id does: a private/regional endpoint or
	// a local stand-in must be nameable rather than implicit (the package's
	// named defaults are the production values, and the CLI's test seam uses
	// these two).
	doubaoSubmit string
	doubaoQuery  string
	// doubaoBaseURL is the BATCH channel's endpoint ROOT, from which both URLs
	// derive when neither was named individually — the settings/env face of the
	// same decision the two flags above make one URL at a time (the voice-side
	// counterpart of --vision-base-url beside --vision-model). Empty means the
	// published root; the URLs above, when set, win for the URL they name.
	doubaoBaseURL string
	// doubaoStreamEndpoint is the STREAMING channel's wss:// URL. Empty means
	// doubaostream.DefaultEndpoint.
	doubaoStreamEndpoint string
}

// voiceVerbose answers whether this run's DEBUG SINK is on.
//
// IT IS A METHOD RATHER THAN A FIELD READ at each printer call site, and the
// reason is the resolution's own shape: opts.verbose carries the FLAG rung only
// as far as runVoiceSubcommand, which folds the env and the settings in
// (config.ResolveAudioVerbose, the ONE reader) and writes the answer BACK into
// this same field. So a reader here sees the resolved answer — never the raw
// flag — and the printers can ask the question without knowing how many channels
// decided it.
//
// A nil opts is "no switch", not a crash: the transcription arms are called by
// paths that build their own options (the chat live arm's seam tests, the helper
// entry points), and a nil there must mean the DEFAULT, which is OFF.
func (o *voiceOptions) voiceVerbose() bool { return o != nil && o.verbose }

// Voice flag names. Each is used by the parser's membership check, by the
// valueless-flag declaration, and by the lookup that reads the value — three
// sites that must agree, which is exactly why they are constants and not
// three spellings of the same string (Rule 1b/13).
const (
	voiceFlagChunk       = "chunk"
	voiceFlagServer      = "server"
	voiceFlagListen      = "listen"
	voiceFlagStdio       = "stdio"
	voiceFlagModels      = "models"
	voiceFlagRuntime     = "runtime"
	voiceFlagClient      = "client"
	voiceFlagListDevices = "list-devices"
	voiceFlagDevice      = "device"
	voiceFlagSeconds     = "seconds"
	voiceFlagServerURL   = "server-url"
	// voiceFlagAudioSource replaces the microphone with a FILE of recorded audio
	// (task 01a11f13): the capture path reads the recording at its own real-time
	// rate instead of opening a device, so the whole chain — window boundary,
	// WAV, engine — runs with no sound card on the host. Named here with the
	// other flags because it is a flag of this verb, not a second tool (Rule 3).
	voiceFlagAudioSource = "audio-source"
	// Second-engine selection and its knobs (--engine doubao …). Named here so
	// the parser, the help text and the settings reader share one spelling
	// (Rule 1b).
	voiceFlagEngine         = "engine"
	voiceFlagDoubaoKeyFile  = "doubao-key-file"
	voiceFlagDoubaoResource = "doubao-resource"
	voiceFlagDoubaoHotwords = "doubao-hotwords"
	voiceFlagDoubaoSubmit   = "doubao-submit-url"
	voiceFlagDoubaoQuery    = "doubao-query-url"
	// The two CHANNEL-level endpoint knobs (task 01a11f90): the batch root and
	// the streaming URL. Named here with the per-URL flags above because the
	// parser, the settings-source resolver and the help share one spelling
	// (Rule 1b).
	voiceFlagDoubaoBaseURL        = "doubao-base-url"
	voiceFlagDoubaoStreamEndpoint = "doubao-stream-endpoint"
	// Master's capture flags (--out/--recorder, see voiceOptions.out). Named
	// here so the values never appear as bare literals. --out IS accepted by
	// this branch's client (the debug file); --recorder is not yet.
	voiceFlagOut      = "out"
	voiceFlagRecorder = "recorder"
	// voiceFlagRecord keeps the audio of a CAPTURE (the bare invocation or
	// --client) as a playable WAV. It is the sibling of --out with the two
	// differences that made a second spelling necessary: it applies to the bare
	// capture too (--out is a client flag, see voiceRunShape), and it is
	// permitted to point at an EXISTING path, because a recording kept
	// deliberately (a corpus of utterances, the same name every run) is the
	// point of it — the collision rule `--out` exists for would make it
	// unusable. Overwriting is never implicit: an existing file is replaced only
	// for this flag, and the write is announced with the byte count.
	voiceFlagRecord = "record"
	// voiceFlagSelf types the transcript into the window that HOSTS this
	// process instead of only printing it (see voiceTypeIntoOwnWindow). The
	// value is the SHARED symbol shot.SelfAddressSymbol, so `--self` names one
	// behaviour across `gopi tool capture`, `gopi tool act` and `gopi tool voice_input`
	// (Rule 14: one definition, every consumer imports it).
	voiceFlagSelf = shot.SelfAddressSymbol
	// voiceFlagVerbose (and its short spelling voiceFlagVerboseShort) is the
	// DEBUG SINK of the streaming arms: with it, the per-partial transcript
	// stream and the channel's counters are printed; without it (the default)
	// only the transcript and the ordinary per-run lines appear. The LONG name
	// comes from config.AudioVerboseFlagName — the constant the catalog row and
	// the settings/env channels are grouped under (Rule 14) — so the flag the
	// help advertises and the knob CONFIG.md documents cannot drift.
	voiceFlagVerbose = config.AudioVerboseFlagName
	// voiceFlagVerboseShort is the single-dash spelling. It is NOT a
	// voiceValuelessFlags member like the long one: the shared parser only
	// recognises the double form (argvFlagPrefix), so `-v` reaches the parser as
	// a POSITIONAL and is claimed by the one prefix loop in parseVoiceArgs —
	// exactly like `-h/--help` is claimed in runVoiceSubcommand. Keeping it out
	// of the strict parser's declared set is also what stops the bare-capture
	// shape from ever seeing it as a file name.
	voiceFlagVerboseShort = "-v"
	// voiceFlagDisplay selects the X11 display --self types into. It is the SAME
	// spelling `gopi tool act --display` uses (actFlagDisplay), because it names the
	// same X11 concept and two spellings of it would be two knobs for one thing.
	voiceFlagDisplay = actFlagDisplay
)

// voiceValuelessFlags take no argument. Declared so the shared parser never
// consumes the token after one (see memoryFlagSet.boolean).
var voiceValuelessFlags = []string{
	voiceFlagServer, voiceFlagStdio, voiceFlagClient, voiceFlagListDevices,
	voiceFlagSelf, voiceFlagVerbose,
}

// voiceFlagConstNames maps each of master's still-bare capture flag literals to
// its constant, so the membership check below compares constants (Rule 1b/13)
// instead of hexing the strings inline. merge-note: master wrote these flags as
// literals in the switch; the branch's constant discipline is kept, and the two
// master-only flags (out/recorder, see voiceOptions.out) are named here too so a
// future re-inlining fails review rather than passing silently.
var voiceFlagConstNames = map[string]string{
	voiceFlagChunk:                voiceFlagChunk,
	voiceFlagServer:               voiceFlagServer,
	voiceFlagListen:               voiceFlagListen,
	voiceFlagStdio:                voiceFlagStdio,
	voiceFlagModels:               voiceFlagModels,
	voiceFlagRuntime:              voiceFlagRuntime,
	voiceFlagClient:               voiceFlagClient,
	voiceFlagListDevices:          voiceFlagListDevices,
	voiceFlagDevice:               voiceFlagDevice,
	voiceFlagSeconds:              voiceFlagSeconds,
	voiceFlagServerURL:            voiceFlagServerURL,
	voiceFlagAudioSource:          voiceFlagAudioSource,
	voiceFlagEngine:               voiceFlagEngine,
	voiceFlagDoubaoKeyFile:        voiceFlagDoubaoKeyFile,
	voiceFlagDoubaoResource:       voiceFlagDoubaoResource,
	voiceFlagDoubaoHotwords:       voiceFlagDoubaoHotwords,
	voiceFlagDoubaoSubmit:         voiceFlagDoubaoSubmit,
	voiceFlagDoubaoQuery:          voiceFlagDoubaoQuery,
	voiceFlagDoubaoBaseURL:        voiceFlagDoubaoBaseURL,
	voiceFlagDoubaoStreamEndpoint: voiceFlagDoubaoStreamEndpoint,
	voiceFlagOut:                  voiceFlagOut,
	voiceFlagRecorder:             voiceFlagRecorder,
	voiceFlagRecord:               voiceFlagRecord,
	voiceFlagSelf:                 voiceFlagSelf,
	voiceFlagVerbose:              voiceFlagVerbose,
	voiceFlagDisplay:              voiceFlagDisplay,
}

func voiceFlagMember(f string) bool {
	switch f {
	case voiceFlagChunk, voiceFlagServer, voiceFlagListen, voiceFlagStdio,
		voiceFlagModels, voiceFlagRuntime, voiceFlagClient, voiceFlagListDevices,
		voiceFlagDevice, voiceFlagSeconds, voiceFlagServerURL, voiceFlagAudioSource,
		voiceFlagEngine, voiceFlagDoubaoKeyFile, voiceFlagDoubaoResource, voiceFlagDoubaoHotwords,
		voiceFlagDoubaoSubmit, voiceFlagDoubaoQuery,
		voiceFlagDoubaoBaseURL, voiceFlagDoubaoStreamEndpoint,
		voiceFlagOut, voiceFlagSelf, voiceFlagDisplay, voiceFlagRecord, voiceFlagVerbose:
		return true
	}
	// Master accepted --recorder (cli_voice_capture.go) and this client does
	// not wire it: the recorder argv is derived from the chosen device
	// (voiceRecorderCommand), so an override would have to re-decide the
	// backend. It stays a NON-member: "unknown flag" is louder and more honest
	// than accepting a flag that would be silently ignored (Rule 15).
	// See voiceOptions.recorder.
	return false
}

// voiceShortFlagPrefix is the single-dash spelling. The shared parser only
// recognises the DOUBLE form (argvFlagPrefix), so a single-dash token reaches it
// as a positional — which is why this verb has to check for it itself.
const voiceShortFlagPrefix = "-"

// voiceShortFlagHints maps a short spelling an operator plausibly types onto the
// long flag this verb actually accepts. It exists so the refusal can answer with
// the flag they MEANT rather than only the one they did not write; the values are
// constants the parser and the help already read (Rule 1b/13), never new
// spellings.
var voiceShortFlagHints = map[string]string{
	voiceShortFlagPrefix + "s": voiceFlagSeconds,
	voiceShortFlagPrefix + "d": voiceFlagDevice,
	voiceShortFlagPrefix + "c": voiceFlagChunk,
	voiceShortFlagPrefix + "o": voiceFlagOut,
}

// voiceTakeVerboseShort claims the `-v` spelling out of the positionals before
// the parser ever sees them.
//
// WHY IT IS NEEDED, and why it is not laziness about the long form: the shared
// parser recognises only the DOUBLE-dash form, so `-v` arrives as a POSITIONAL
// word — and this verb reads a lone positional as THE AUDIO FILE. Left alone,
// `gopi tool voice_input -v clip.wav` would be refused twice over ("takes long
// flags, not short ones") and `gopi tool voice_input -v` would be read as the
// file shape for a file named "-v": the same misreading the -s/-d/-c/-o hints
// exist to answer, except that here the flag IS accepted, so the answer is to
// claim it rather than to refuse it.
//
// It is deliberately narrow: exactly the one token voiceFlagVerboseShort, no
// prefix matching, no clustering (`-vv`, `-vx` stay refused by the loop that
// follows — a flag nobody typed must not be invented).
func voiceTakeVerboseShort(positionals []string) (rest []string, found bool) {
	rest = make([]string, 0, len(positionals))
	for _, arg := range positionals {
		if arg == voiceFlagVerboseShort {
			found = true
			continue
		}
		rest = append(rest, arg)
	}
	return rest, found
}

// voiceRefuseUndeclaredFlags refuses every flag spelling this verb does not
// accept, BEFORE the remaining words are interpreted as positionals.
//
// It has TWO halves because the parser has two behaviours: an undeclared
// `--flag` is already refused inside parse() (the set is strict for this verb),
// so the membership loop here is belt-and-braces for the double form; the SHORT
// form is the one the parser cannot see at all (to the parser it is not a flag).
// Answering both in one place is what makes the refusal independent of the road
// the word took.
func voiceRefuseUndeclaredFlags(fs *memoryFlagSet, positionals []string) error {
	for _, fname := range fs.order {
		if !voiceFlagMember(fname) {
			return fmt.Errorf("unknown flag %s%s", argvFlagPrefix, fname)
		}
	}
	for _, arg := range positionals {
		if !voiceShortFlagWord(arg) {
			continue
		}
		if long, ok := voiceShortFlagHints[arg]; ok {
			return fmt.Errorf("unknown flag %s (%s takes long flags, not short ones — did you mean %s%s ?)",
				arg, voiceCLIVerb, argvFlagPrefix, long)
		}
		return fmt.Errorf("unknown flag %s (%s takes long flags, not short ones)", arg, voiceCLIVerb)
	}
	return nil
}

// voiceShortFlagWord reports whether a positional WORD is spelled like a flag
// rather than like a path. A lone `-` is the stdin convention and stays a
// positional; a token whose second character is a DIGIT is not a flag either (a
// value such as `-3` a caller meant as a positional). Everything else that starts
// with a single dash is a flag spelling, and this verb accepts none of them.
func voiceShortFlagWord(arg string) bool {
	if !strings.HasPrefix(arg, voiceShortFlagPrefix) {
		return false
	}
	if strings.HasPrefix(arg, argvFlagPrefix) {
		return false
	}
	if len(arg) <= len(voiceShortFlagPrefix) {
		return false
	}
	second := arg[len(voiceShortFlagPrefix)]
	return second < '0' || second > '9'
}

// parseVoiceArgs parses without touching the process (no exit, no env
// mutation) so it is unit-testable.
func parseVoiceArgs(args []string) (*voiceOptions, error) {
	fs := newMemoryFlagSet()
	for _, bf := range voiceValuelessFlags {
		fs.boolean(bf)
	}
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	// `-v` FIRST, before anything reads a positional as a file name: the short
	// spelling is invisible to the parser (see voiceTakeVerboseShort), so this
	// is the one place it can be claimed.
	positionals, verboseShort := voiceTakeVerboseShort(positionals)
	// UNKNOWN FLAGS ARE REFUSED BY THE PARSER ITSELF, not after the fact.
	//
	// The parser is already in STRICT mode for this verb (boolean()/repeat()
	// close it), so an undeclared `--flag` is refused inside parse() and never
	// reaches the whitelist loop below. The loop's own membership check therefore
	// only ever saw a flag the parser had DECLARED — and the one shape it could
	// not protect was a SHORT spelling: `-s 5` does not start with `--`, so the
	// parser filed it as a POSITIONAL, and the loop looked at `fs.order`, which
	// never contained it. The invocation was then read as the FILE shape for a
	// file named "-s", and the run failed with the cloud engine's
	// `read audio "-s": open -s: no such file or directory` — a refusal that
	// names a file the operator never wrote and never mentions the flag they
	// typed (Rule 15: a mistake must be answered with the mistake, not with the
	// consequence of misreading it).
	//
	// So: an undeclared flag is refused BEFORE the positionals are interpreted,
	// in BOTH spellings, and the sentence names the long spelling this verb does
	// accept when the short one has an obvious long equivalent.
	if err := voiceRefuseUndeclaredFlags(fs, positionals); err != nil {
		return nil, err
	}

	home := userHomeDirOrDefault()
	opts := &voiceOptions{
		chunk:   voiceDefaultChunkSec,
		listen:  voiceDefaultListen,
		seconds: voiceClientDefaultSeconds,
		target:  voiceDefaultServerURL,
		// THE DEFAULT DEPLOYMENT IS THE GOPI PROFILE (rung 1): <gopi dir>/voice
		// and the runtime inside it. The $HOME/work pair is rung 2 — the
		// compatibility answer, used only when rung 1 does not hold a complete
		// deployment and announced with a migration line when it does
		// (voiceResolveDeployment is the ONE walk; this only fills in the
		// starting point, so a caller that never walks still points where a real
		// run would).
		//
		// capture is decided at the END of this function, from what the
		// invocation NAMES (a file? a server?) rather than from a flag: the
		// bare command's meaning is "nothing was named, so record" (see the
		// switch after the positionals).
		// engine is deliberately NOT defaulted here: an empty value means
		// "nobody chose", and voiceResolveEngine resolves that to the terminal
		// default (AudioEngineDefault — the cloud batch engine) after
		// consulting the env and the settings file. Pre-filling it here would
		// make a settings.json choice unreachable (the flag rung would always
		// look answered).
	}
	voiceDefaultDeploymentPaths(home, opts)
	// THE CONFIGURED DEPLOYMENT, for the local engine (Rule 2: one reader —
	// config.ResolveAudioLocal applies the environment over the nested
	// audio_engine_param.local block). It arrives at the SAME rung an explicit
	// --models/--runtime occupies, and that is deliberate: a path an operator
	// wrote into settings.json is their declaration of where the deployment
	// lives, exactly like the flag, so the two-stage search may not
	// second-guess it (the modelsExplicit/runtimeExplicit contract below). The
	// FLAG rung is applied where the flags are parsed, so it overrides this.
	//
	// Resolved through the SAME settings-path resolution the rest of the voice
	// surface uses; an unreadable document is not fatal here (the run still
	// has the search), and the agent path reports its own parse failure.
	if cfg, err := config.FromSettingsFile(config.ResolveSettingsPath()); err == nil && cfg != nil {
		configured := config.ResolveAudioLocal(cfg)
		if v := strings.TrimSpace(configured.ModelsDir); v != "" {
			opts.models = v
			opts.modelsExplicit = true
			opts.modelsFromConfig = true
		}
		if v := strings.TrimSpace(configured.RuntimeBin); v != "" {
			opts.runtime = v
			opts.runtimeExplicit = true
			opts.runtimeFromConfig = true
		}
	}

	if fs.has(voiceFlagClient) {
		opts.client = true
	}
	if fs.has(voiceFlagListDevices) {
		opts.listDevices = true
	}
	if fs.has(voiceFlagDevice) {
		opts.device = strings.TrimSpace(fs.get(voiceFlagDevice))
	}
	if fs.has(voiceFlagSeconds) {
		v, err := strconv.Atoi(strings.TrimSpace(fs.get(voiceFlagSeconds)))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("--seconds must be a positive integer, got %q", fs.get(voiceFlagSeconds))
		}
		opts.seconds = v
	}
	if fs.has(voiceFlagServerURL) {
		opts.target = strings.TrimSpace(fs.get(voiceFlagServerURL))
	}
	// --audio-source: the file the capture reads instead of a device. Checked at
	// PARSE time, like --out and for the same reason — a path that cannot be read
	// is knowable before anything is opened, so it must not be discovered after a
	// run shell has been spent waiting for audio that is never coming.
	if fs.has(voiceFlagAudioSource) {
		opts.audioSource = strings.TrimSpace(fs.get(voiceFlagAudioSource))
		if err := voiceValidateAudioSource(opts.audioSource); err != nil {
			return nil, err
		}
	}
	if fs.has(voiceFlagChunk) {
		v, err := strconv.Atoi(strings.TrimSpace(fs.get(voiceFlagChunk)))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("--chunk must be a positive integer, got %q", fs.get(voiceFlagChunk))
		}
		opts.chunk = v
	}
	if fs.has(voiceFlagServer) {
		opts.server = true
	}
	if fs.has(voiceFlagStdio) {
		opts.stdio = true
	}
	if fs.has(voiceFlagListen) {
		opts.listen = strings.TrimSpace(fs.get(voiceFlagListen))
	}
	if fs.has(voiceFlagModels) {
		opts.models = strings.TrimSpace(fs.get(voiceFlagModels))
		opts.modelsExplicit = true
	}
	if fs.has(voiceFlagRuntime) {
		opts.runtime = strings.TrimSpace(fs.get(voiceFlagRuntime))
		opts.runtimeExplicit = true
	}
	if fs.has(voiceFlagEngine) {
		opts.engine = strings.TrimSpace(fs.get(voiceFlagEngine))
		// An explicitly passed empty value is a MISTAKE, not "nobody chose":
		// the empty string is how the resolver spells "no flag rung", so
		// accepting `--engine ""` would silently answer with the env or the
		// settings file while the operator believed they had overridden it.
		if opts.engine == "" {
			return nil, fmt.Errorf("--%s requires a value (want one of %s)", voiceFlagEngine, strings.Join(voiceEngineValues, ", "))
		}
		if err := voiceValidateEngine(opts.engine); err != nil {
			return nil, err
		}
		// The flag rung stores the CURRENT name, so every downstream switch
		// compares one vocabulary: a pre-rename name on the flag is translated
		// here, at the one place the flag is read. What the OPERATOR typed is
		// kept beside it, because the migration notice must report their
		// spelling and a translated value can no longer say it.
		opts.engineRaw = opts.engine
		opts.engine, _ = config.AudioEngineLegacyVendorName(opts.engine)
	}
	if fs.has(voiceFlagDoubaoKeyFile) {
		opts.doubaoKeyFile = strings.TrimSpace(fs.get(voiceFlagDoubaoKeyFile))
	}
	if fs.has(voiceFlagDoubaoResource) {
		opts.doubaoResource = strings.TrimSpace(fs.get(voiceFlagDoubaoResource))
	}
	if fs.has(voiceFlagDoubaoHotwords) {
		opts.doubaoHotwords = strings.TrimSpace(fs.get(voiceFlagDoubaoHotwords))
	}
	if fs.has(voiceFlagDoubaoSubmit) {
		opts.doubaoSubmit = strings.TrimSpace(fs.get(voiceFlagDoubaoSubmit))
	}
	if fs.has(voiceFlagDoubaoQuery) {
		opts.doubaoQuery = strings.TrimSpace(fs.get(voiceFlagDoubaoQuery))
	}
	if fs.has(voiceFlagDoubaoBaseURL) {
		opts.doubaoBaseURL = strings.TrimSpace(fs.get(voiceFlagDoubaoBaseURL))
	}
	if fs.has(voiceFlagDoubaoStreamEndpoint) {
		opts.doubaoStreamEndpoint = strings.TrimSpace(fs.get(voiceFlagDoubaoStreamEndpoint))
	}
	if fs.has(voiceFlagOut) {
		opts.out = strings.TrimSpace(fs.get(voiceFlagOut))
		if err := voiceValidateOutPath(opts.out); err != nil {
			return nil, err
		}
	}
	// --record: the capture's own keep-flag. Validated for shape (never empty,
	// never a directory) but NOT for existence — overwriting is what it is for
	// (see voiceFlagRecord). Both flags may be given; they are two files.
	if fs.has(voiceFlagRecord) {
		opts.record = strings.TrimSpace(fs.get(voiceFlagRecord))
		if opts.record == "" {
			return nil, fmt.Errorf("--%s requires a file path (e.g. --%s utterance.wav)", voiceFlagRecord, voiceFlagRecord)
		}
		if fi, err := os.Stat(opts.record); err == nil && fi.IsDir() {
			return nil, fmt.Errorf("--%s %s is a directory — name the file to write", voiceFlagRecord, opts.record)
		}
	}
	if fs.has(voiceFlagSelf) {
		opts.self = true
	}
	if fs.has(voiceFlagVerbose) || verboseShort {
		opts.verbose = true
	}
	if fs.has(voiceFlagDisplay) {
		opts.display = strings.TrimSpace(fs.get(voiceFlagDisplay))
	}

	// ── --listen implies --server, and the bare invocation is CAPTURE ──────
	//
	// --listen names an address to LISTEN on. The only shape that listens is
	// the HTTP server, so the flag now implies the mode it needs: a --listen
	// that was silently ignored (the file shape took it and did nothing with
	// it) is the one surprise this verb used to be able to produce. The default
	// address is applied whenever --server is on, so the implication does not
	// invent a second default (Rule 2).
	if fs.has(voiceFlagListen) {
		opts.server = true
	}

	// THE DEFAULT SHAPE, and it is the ABSENCE of a choice: no audio file, no
	// --server, no --client, no --list-devices. The recorder captures locally,
	// the local engine transcribes here, and the microphone is bounded by
	// voiceLocalDefaultSeconds so a forgotten flag cannot leave it open. Set
	// BEFORE the refusals below, because every one of them asks "is this a
	// capture shape?" — the answer is already fixed by what the invocation
	// NAMES, and resolving it later is how the first draft refused the bare
	// `--record utterance.wav` it was supposed to accept.
	opts.capture = len(positionals) == 0 && !opts.server && !opts.client && !opts.listDevices
	if opts.capture && !fs.has(voiceFlagSeconds) {
		// A capture is a capture at the CLI's own default length unless the
		// operator said otherwise — the remote client's shorter default (5s) is
		// its own, and inheriting it here would make the two documented lengths
		// disagree about one and the same recording.
		opts.seconds = voiceLocalDefaultSeconds
	}

	// --stdio and --listen are two shapes of --server; asking for both is a
	// mistake worth naming rather than silently preferring one.
	if opts.stdio && fs.has(voiceFlagListen) {
		return nil, fmt.Errorf("--stdio and --listen are mutually exclusive")
	}
	if opts.client && opts.server {
		return nil, fmt.Errorf("--client captures audio and --server transcribes it — pick one side of the hop")
	}
	// --out names the CLIENT's debug file and --record the capture's keep-file:
	// two files written by the two shapes that RECORD (--client, and the bare
	// capture). On every other shape the write would not happen while the
	// operator believed a recording was being kept — a flag that changes
	// nothing is the defect this layer exists to refuse (Rule 15). The two are
	// refused BY NAME (their own message, naming the shape), and --list-devices
	// gets its own sentence because "a recording you are keeping" and "a
	// listing that exits" are not the same mistake.
	//
	// The ARM ORDER below is the point: the list-devices case is tested BEFORE
	// the general non-recording one. Reversed, the general arm swallows
	// --list-devices — the `--out`/`--record` + `--list-devices` refusals were
	// dead code until the matrix test drove them (the general clause matched
	// first and said only "it records nothing").
	keepFlags := []struct {
		flag  string
		value string
		what  string // what the flag keeps, quoted into the sentence
	}{
		{voiceFlagOut, opts.out, "keeps the captured audio"},
		{voiceFlagRecord, opts.record, "keeps a recording"},
	}
	for _, kf := range keepFlags {
		if kf.value == "" {
			continue
		}
		switch {
		case opts.listDevices:
			return nil, fmt.Errorf("--%s %s, but --%s only lists devices and exits — drop one",
				kf.flag, kf.what, voiceFlagListDevices)
		case kf.flag == voiceFlagOut && !opts.client:
			return nil, fmt.Errorf("--%s is a --%s flag (it %s); it does nothing on a %s run",
				kf.flag, voiceFlagClient, kf.what, voiceRunShape(opts))
		case !opts.capture && !opts.client:
			return nil, fmt.Errorf("--%s %s, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally",
				kf.flag, kf.what, voiceRunShape(opts), voiceFlagClient)
		}
	}
	// --audio-source replaces the MICROPHONE. On a shape that opens none, the
	// flag would be read by nothing — and the operator would believe the audio
	// they named was the audio transcribed, which is worse than a refusal
	// (Rule 15). Refused BY NAME, with the shape named, so the correction is
	// obvious: the file shape already takes a file, and it takes it as the
	// positional argument (which this flag deliberately is not, because a file
	// run hands the FILE to the engine whole, while this one drives the capture
	// window by window).
	if fs.has(voiceFlagAudioSource) && !voiceAudioSourceShapes(opts) {
		if opts.listDevices {
			return nil, fmt.Errorf("--%s names the audio the CAPTURE reads, but --%s only lists devices and exits — drop one",
				voiceFlagAudioSource, voiceFlagListDevices)
		}
		return nil, fmt.Errorf("--%s replaces the microphone, but this is a %s run — it opens no capture: use it with --%s (or with no argument at all, which captures locally), or name the file directly if you meant one engine pass over a whole recording",
			voiceFlagAudioSource, voiceRunShape(opts), voiceFlagClient)
	}
	// --chunk windows a FILE that is already on disk. With no file there is
	// nothing to window, so accepting it would leave the operator's stated
	// intent (a chunked pass) silently unreported — and `--chunk N` with a bare
	// invocation reads as "chunk the microphone", which this verb does not do.
	if fs.has(voiceFlagChunk) && opts.capture {
		return nil, fmt.Errorf("--%s windows an audio FILE, but the bare invocation reads the microphone: name the file (`%s <audio-file> --%s %d`), or drop --%s",
			voiceFlagChunk, voiceCLIVerb, voiceFlagChunk, opts.chunk, voiceFlagChunk)
	}
	// …and on the two shapes that have no file of their own: --list-devices
	// opens none at all, and --client hands its audio to a server that windows
	// it on the far side. Accepting the flag there would report a chunked pass
	// that never happened.
	if fs.has(voiceFlagChunk) && (opts.client || opts.listDevices) {
		return nil, fmt.Errorf("--%s windows a file that is transcribed HERE, but this is a %s run — it transcribes no file: the window belongs to `%s <audio-file> --%s %d`, or to the server the client posts to",
			voiceFlagChunk, voiceRunShape(opts), voiceCLIVerb, voiceFlagChunk, opts.chunk)
	}
	// --models/--runtime point at the weights and the runtime THIS process
	// reads. The two shapes that never transcribe here would accept them and
	// open nothing — an operator who redirected the weights and got the default
	// ones has been told something false (Rule 15): --client's audio is
	// transcribed on the host it posts to, which owns its own weights, and
	// --list-devices opens no model at all.
	if (fs.has(voiceFlagModels) || fs.has(voiceFlagRuntime)) && (opts.client || opts.listDevices) {
		flag := voiceFlagModels
		if fs.has(voiceFlagRuntime) {
			flag = voiceFlagRuntime
		}
		return nil, fmt.Errorf("--%s is read by the shapes that transcribe HERE (a file run, the bare capture, a server), but this is a %s run — it opens no weights: a %s is transcribed on the host it posts to, and --list-devices transcribes nothing",
			flag, voiceRunShape(opts), voiceFlagClient)
	}
	// The remaining capture-only flags, refused on the shapes that cannot mean
	// them. --server-url is the sharpest of the three: left alone it would let a
	// LOCAL engine produce the transcript while the operator believed the host
	// they named had done it — a wrong answer delivered confidently, which is
	// worse than a refusal (Rule 15).
	if fs.has(voiceFlagServerURL) && (opts.server || opts.listDevices) {
		return nil, fmt.Errorf("--%s names the transcriber a --client posts to, but this is a %s run — it captures nothing, so drop --%s (the local shapes transcribe HERE, they never post anywhere)",
			voiceFlagServerURL, voiceRunShape(opts), voiceFlagServerURL)
	}
	if fs.has(voiceFlagDevice) && !opts.capture && !opts.client && !opts.listDevices {
		return nil, fmt.Errorf("--%s names a capture device, but this is a %s run — it records nothing: use it with --%s, or with no argument at all",
			voiceFlagDevice, voiceRunShape(opts), voiceFlagClient)
	}
	if fs.has(voiceFlagSeconds) && !opts.capture && !opts.client {
		return nil, fmt.Errorf("--%s bounds a recording, but this is a %s run — it records nothing: use it with --%s, or with no argument at all to record locally",
			voiceFlagSeconds, voiceRunShape(opts), voiceFlagClient)
	}
	// --display picks the screen --self types into. On a shape where the window
	// is not addressed at all (no --self) it is read by nothing, and the
	// operator's chosen display would be silently abandoned — the transcript
	// still prints, but on the display they thought they had moved past
	// (Rule 15: a flag that changes nothing is a no-op wearing an instruction).
	if fs.has(voiceFlagDisplay) && !opts.self {
		return nil, fmt.Errorf("--%s picks the display --%s types into, but this run never types anywhere — add --%s, or drop --%s (the transcript still goes to stdout)",
			voiceFlagDisplay, voiceFlagSelf, voiceFlagSelf, voiceFlagDisplay)
	}

	// --self types the transcript into this process's own window. The shapes
	// where it cannot mean that are refused BY NAME, because each of them
	// either prints to stdout for a pipe (where an injected keystroke would be
	// invisible AND useless) or does not produce text at all:
	if opts.self {
		switch {
		case opts.server:
			return nil, fmt.Errorf("--%s types into a WINDOW, but --%s prints the transcript to stdout for a client: the two ends of an ssh hop have no shared window — run --%s on the machine holding the window (the client side)",
				voiceFlagSelf, voiceFlagServer, voiceFlagSelf)
		case opts.stdio:
			return nil, fmt.Errorf("--%s is a pipe shape: its stdout IS the audio, and a transcript typed into a window would corrupt the very stream the caller is reading — drop --%s",
				voiceFlagStdio, voiceFlagSelf)
		case opts.listDevices:
			return nil, fmt.Errorf("--%s types a transcript into this process's window, but --%s only lists devices and exits — drop one",
				voiceFlagSelf, voiceFlagListDevices)
		}
	}

	// These two are boolean verbs. The flag parser would otherwise swallow a
	// following word as their value (`--client clip.wav`), silently turning a
	// stray filename into a no-op — name it instead (master's guard, kept).
	for _, boolFlag := range []string{voiceFlagClient, voiceFlagListDevices} {
		if fs.has(boolFlag) && strings.TrimSpace(fs.get(boolFlag)) != "" {
			return nil, fmt.Errorf("--%s takes no value (got %q)", boolFlag, fs.get(boolFlag))
		}
	}

	// The two boolean capture flags must not have swallowed a value (the shared
	// parser would otherwise eat a following word as their argument).
	for _, boolFlag := range []string{voiceFlagClient, voiceFlagListDevices} {
		if fs.has(boolFlag) && strings.TrimSpace(fs.get(boolFlag)) != "" {
			return nil, fmt.Errorf("--%s takes no value (got %q)", boolFlag, fs.get(boolFlag))
		}
	}

	switch {
	case opts.client:
		// The client's input is a device, not a file.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("--client records from a device and takes no audio file (got %q)", positionals[0])
		}
	case opts.capture:
		// Nothing to assign: the capture shape's input is the microphone.
	case opts.server || opts.listDevices:
		// A server shape takes no positional: `--server clip.wav` would start a
		// transcriber that never looks at the file, and a mistyped flag would be
		// found only when the process that should have exited stayed up.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("--%s takes no audio-file argument (got %q): a file is transcribed by `%s <audio-file>`, and the microphone by a bare `%s`",
				voiceFlagServer, positionals[0], voiceCLIVerb, voiceCLIVerb)
		}
	default:
		opts.audio = strings.TrimSpace(positionals[0])
	}
	return opts, nil
}

// voiceValidateOutPath refuses the three ways the client's debug file would
// lie about what it kept: an empty path (the flag was given with no value), a
// directory (the write cannot succeed), and an existing file (a capture
// silently overwriting a recording the operator kept is data loss, while a
// refusal costs one flag value). Checked at PARSE time, before the microphone
// is opened: a mistake that is knowable up front must not cost a recording.
func voiceValidateOutPath(path string) error {
	if path == "" {
		return fmt.Errorf("--%s requires a file path (e.g. --%s nick.wav)", voiceFlagOut, voiceFlagOut)
	}
	fi, err := os.Stat(path)
	if err != nil {
		// Any other stat failure (a missing parent directory, a permission
		// problem) is left to the write, which reports it with the path.
		return nil
	}
	if fi.IsDir() {
		return fmt.Errorf("--%s %s is a directory — name the file to write", voiceFlagOut, path)
	}
	return fmt.Errorf("--%s %s already exists — remove it or name a new file "+
		"(a capture must not overwrite a recording you kept)", voiceFlagOut, path)
}

// voiceRunShape names what the operator DID ask for, so a --client-only flag on
// another shape reads as a correction rather than a mystery. The file shape is
// only reached with a positional (the "missing <audio-file> argument" refusal is
// what an empty invocation used to get); the bare shape captures instead.
func voiceRunShape(opts *voiceOptions) string {
	switch {
	case opts.server:
		return voiceServerShapeName
	case opts.listDevices:
		return voiceListDevicesShapeName
	// The client is its own shape and must be named as such: it captures here
	// and transcribes on the host it posts to, so a refusal that told it
	// "file-transcription" would send the operator to the wrong correction.
	// (It went unnoticed while every caller happened to exclude the client;
	// the capture-only arms below are the callers that do not.)
	case opts.client:
		return voiceClientShapeName
	case opts.capture:
		return voiceCaptureShapeName
	default:
		return voiceFileShapeName
	}
}

// voiceModelPaths returns the two GGUF files the runtime needs.
func voiceModelPaths(modelsDir string) (enc, llm string) {
	return filepath.Join(modelsDir, voiceEncoderName),
		filepath.Join(modelsDir, voiceLLMName)
}

// voiceDepsReady reports the first missing dependency, or "" when everything
// the engine needs is present. Checked up front so a missing model produces a
// clear message instead of a runtime error after a 910MB load attempt.
//
// The check is PER ENGINE, because the cloud engine's whole point is the
// missing dependency it removes: 910MB of weights plus a 20MB runtime that the
// doubao path never opens. Demanding them on the cloud path would refuse a
// transcription that would have worked (and hide the zero-model property).
func voiceDepsReady(opts *voiceOptions) string {
	switch voiceResolveEngine(opts) {
	case config.AudioEngineRecording:
		return voiceDoubaoDepsReady(opts)
	case config.AudioEngineStreaming:
		return voiceStreamDepsReady(opts)
	case config.AudioEngineSauc:
		return voiceSaucDepsReady(opts)
	}
	if !voiceFilePresent(opts.runtime) {
		return voiceLocalDeploymentMissing(fmt.Sprintf("runtime not found: %s", opts.runtime), opts)
	}
	enc, llm := voiceModelPaths(opts.models)
	if !voiceFilePresent(enc) {
		return voiceLocalDeploymentMissing(fmt.Sprintf("encoder model not found: %s", enc), opts)
	}
	if !voiceFilePresent(llm) {
		return voiceLocalDeploymentMissing(fmt.Sprintf("llm model not found: %s", llm), opts)
	}
	return ""
}

// voiceLocalDeploymentMissing turns one missing dependency into an INSTALL
// INSTRUCTION rather than a dead end.
//
// WHY THE MESSAGE IS LONG. The local engine is 914 MB of weights plus a foreign
// C++ runtime (Rule 35) — nothing gopi can fetch, and nothing it should fetch on
// its own. "model not found: /home/x/.gopi/voice/funasr-encoder-f16.gguf" states
// a fact and leaves the operator with a path and no next action; the two
// decisions that actually unblock the run are (a) WHICH two files at WHICH path,
// (b) that the cloud engine is a legitimate way to not need them at all. So the
// message names both, and names the compatibility rung too — a host that already
// has the $HOME/work deployment should not read a fresh-install recipe and
// conclude it must move 914 MB.
//
// The `missing` line is kept VERBATIM as the first line: it is the precise
// file that failed the check, in the vocabulary this check has always used
// (runtime → encoder → llm, first missing wins), so the actionable half is an
// addition to the report rather than a replacement for it.
func voiceLocalDeploymentMissing(missing string, opts *voiceOptions) string {
	profileModels, profileRuntime, profileErr := voiceProfileVoicePaths()
	if profileErr != nil {
		// A profile that cannot be resolved is the one case where the recipe
		// cannot point at rung 1: name the failure, never a path that does not
		// exist (Rule 15).
		profileModels, profileRuntime = opts.models, opts.runtime
	}
	legacyModels, legacyRuntime := voiceLegacyModelPaths(userHomeDirOrDefault())
	enc, llm := voiceModelPaths(profileModels)
	legacyEnc, legacyLLM := voiceModelPaths(legacyModels)
	return fmt.Sprintf(`%s
  the local engine (%s) reads %d weight files and a runtime binary that gopi neither
  builds nor downloads (Rule 35 — it is a foreign deployment, so this is an install
  step, and a missing one is expected on a machine that never installed it).
  1. weights → both files in the model directory:
       %s   %s
       %s   %s
     %s
  2. runtime → the %s binary at:
       %s
     %s
  3. no local deployment at all? the cloud engine needs neither, and is used by
     naming it:
       %s --engine %s
  %s --models DIR / --runtime PATH override BOTH rungs and are used exactly as
  given (no fallback search), so a wrong path is reported rather than silently
  replaced.`,
		missing,
		config.AudioEngineLocal,
		voiceLocalDeploymentFileCount,
		enc, voiceEncoderName,
		llm, voiceLLMName,
		voiceProfileResolutionNote(profileModels, profileErr),
		voiceProfileRuntimeName,
		profileRuntime,
		voiceLegacyRungNote(legacyModels, legacyRuntime, legacyEnc, legacyLLM),
		voiceCLIVerb, config.AudioEngineRecording,
		voiceCLIVerb)
}

// voiceProfileResolutionNote names rung 1 the way the run actually resolved it —
// as a concrete path, or as the failure that stopped it from being one.
func voiceProfileResolutionNote(profileModels string, err error) string {
	if err != nil {
		return fmt.Sprintf("(%s — the profile could not be resolved, so rung 1 is unavailable on this host)", voiceProfileUnresolvedNote(err))
	}
	return fmt.Sprintf("(this is rung 1 — the gopi profile: config.GopiDir + %q)", voiceProfileDirName)
}

// voiceLegacyRungNote states rung 2 — accepted, and NOT the place to install into.
func voiceLegacyRungNote(legacyModels, legacyRuntime, legacyEnc, legacyLLM string) string {
	return fmt.Sprintf("(the legacy rung is still read when rung 1 is empty: %s / %s — i.e. %s, %s and %s — but a run that uses it prints a migration note)",
		legacyModels, legacyRuntime, legacyEnc, legacyLLM, voiceProfileRuntimeName)
}

// transcribeFile runs the engine over one audio file and returns the text.
// This is the single core both server shapes call — and, since the cloud
// engine landed, the single point where the engine is CHOSEN (see
// cli_voice_engine.go for the seam's rationale and the default-engine
// argument). Its two arms have identical signatures and identical error
// discipline, so every caller stays engine-blind; the local arm's behaviour
// (and its argv contract) is byte-for-byte what it was before the dispatch
// existed.
func transcribeFile(opts *voiceOptions, audioPath string) (string, time.Duration, error) {
	text, elapsed, _, err := transcribeFileReport(opts, audioPath, newVoiceRunClock(voiceRunOrigin()))
	return text, elapsed, err
}

// transcribeFileReport is transcribeFile PLUS what the run observed about its own
// FIRST text (cli_voice_first_text.go). The signature above is kept and delegates
// here, because the two server shapes and the capture path call it and none of
// them needs the report — widening the signature would reach all three call sites
// for a figure only one shape prints.
//
// THE REPORT IS BUILT WITH THE SAME TWO NUMBERS THE CALLER ALREADY GETS, from the
// same clock: EngineMS is the engine's own duration (the figure [voice]
// transcribed by … already printed), and for the streaming arm it is the CHANNEL's
// first-partial latency instead, because that is when the first text existed
// there. Nothing here re-measures anything the engine already reported.
func transcribeFileReport(opts *voiceOptions, audioPath string, runClock *voiceRunClock) (string, time.Duration, voiceFirstTextReport, error) {
	var report voiceFirstTextReport
	switch voiceResolveEngine(opts) {
	case config.AudioEngineRecording:
		text, elapsed, err := transcribeFileDoubao(opts, audioPath)
		report = voiceFirstTextFromEngine(elapsed, runClock)
		return text, elapsed, report, err
	case config.AudioEngineStreaming:
		text, report, err := voiceStreamFirstText(opts, audioPath, runClock)
		// THE FULL WALL, not the first-text figure: the streaming arm reports TWO
		// instants and the line that prints this duration is about the whole
		// exchange. The first text is the report's FirstTextMS.
		elapsed := time.Duration(report.FinalMS) * time.Millisecond
		return text, elapsed, report, err
	case config.AudioEngineSauc:
		// THE v3 ARM'S FIRST TEXT IS ALSO ITS ANSWER (the default endpoint is
		// the file-semantics one), so it returns through the same helper the
		// batch and local arms use and lands in the same report shape.
		return transcribeFileSauc(opts, audioPath, runClock)
	}
	text, elapsed, err := transcribeFileLocal(opts, audioPath)
	report = voiceFirstTextFromEngine(elapsed, runClock)
	return text, elapsed, report, err
}

// voiceFirstTextFromEngine is the report for the two engines whose first text IS
// their answer: the batch channel returns one transcript for the whole recording
// and the local runtime returns one for the whole file, so there is no earlier
// instant to report. Its INSTANT is taken from the run's own clock at the moment
// the engine returned — which is what the operator waited, startup and staging
// included — while EngineMS carries the engine's own duration, i.e. the part of
// that wait the engine accounts for. WindowMS is filled in by the capture path,
// the only shape with a recording in front of the engine.
func voiceFirstTextFromEngine(engine time.Duration, runClock *voiceRunClock) voiceFirstTextReport {
	ms := int(engine.Milliseconds())
	return voiceFirstTextReport{
		FirstTextMS: int(runClock.elapsed().Milliseconds()),
		EngineMS:    ms,
		FinalMS:     int(runClock.elapsed().Milliseconds()),
	}
}

// transcribeFileLocal is the Fun-ASR-Nano arm: the local llama.cpp runtime
// over the two GGUF weights.
func transcribeFileLocal(opts *voiceOptions, audioPath string) (string, time.Duration, error) {
	enc, llm := voiceModelPaths(opts.models)
	cmd := exec.Command(opts.runtime,
		"--enc", enc,
		"-m", llm,
		"-a", audioPath,
		"--chunk", strconv.Itoa(opts.chunk),
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			// voiceFormatRefusal adds the channel sentence when the input is
			// classifiable as container-less / outside the engine's set — the
			// bare-PCM-file case, where the same bytes DO work on --stdio.
			return "", 0, voiceFormatRefusal(audioPath, fmt.Errorf("engine failed: %w — %s", err, lastLines(msg, 4)))
		}
		return "", 0, voiceFormatRefusal(audioPath, fmt.Errorf("engine failed: %w", err))
	}
	return strings.TrimSpace(stdout.String()), time.Since(start), nil
}

// lastLines keeps the tail of engine stderr — the useful part when the
// failure is "model map failed" rather than anything at the head.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// runVoiceSubcommand is the CLI entry point.
func runVoiceSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(voiceUsageText())
			os.Exit(0)
		}
	}

	opts, err := parseVoiceArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n%s", err, voiceUsageText())
		os.Exit(1)
	}
	// THE DEBUG SINK IS RESOLVED ONCE, HERE (Rule 2): flag > env > settings,
	// through the ONE reader. Every printer below is handed the resulting bool,
	// so no call site re-reads the environment and the default path cannot
	// depend on where in the run it happens to ask.
	//
	// THE FLAG IS CONSUMED BEFORE THE RESOLUTION, because it is the rung that
	// OUTRANKS the others and the reader must not be able to re-decide it: with
	// `-v` on the command line the answer is "on" whatever the env and the
	// settings say, and ResolveAudioVerbose encodes exactly that.
	opts.verbose, _ = config.ResolveAudioVerbose(opts.verbose)
	// The model/runtime roots are resolved ONCE, here, through the deployment
	// search (voiceResolveDeployment): rung 1 is the operator's gopi profile,
	// rung 2 is the legacy $HOME/work deployment, and an explicitly named
	// --models/--runtime is outside the order and used as given. Done here so
	// EVERY shape reads the same paths — server, client-less file run, bare
	// capture and the deps check below.
	voiceResolveDeployment(opts)

	// The engine is only needed on the transcribing side. --list-devices
	// captures nothing at all and --client sends its audio to a server that
	// already has the weights; the bare capture shape is the one capture shape
	// that transcribes HERE, so it is covered by this guard along with the file
	// and server shapes (that is the whole point of it: one command, no hop).
	// The requirement is on the LOCAL engine only — `--engine doubao` has no
	// weights to find, and voiceDepsReady owns that distinction.
	needsEngine := !opts.listDevices && !opts.client
	if needsEngine {
		if missing := voiceDepsReady(opts); missing != "" {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %s\n", missing)
			os.Exit(1)
		}
	}
	// Say which engine runs — and, for the cloud engine, that the audio LEAVES
	// this machine — BEFORE any work happens, and before a server arms.
	//
	// The guard is needsEngine, i.e. THE PROCESS THAT UPLOADS IS THE PROCESS
	// THAT ANNOUNCES. `--server` is inside it (it transcribes on behalf of its
	// clients: under the cloud engine it is the process that uploads their
	// audio, once, at startup — not a line per request), and both `--client` and
	// `--list-devices` are outside it. The client is deliberately excluded and
	// not merely skipped: it transcribes NOTHING, so the engine the upload
	// actually uses is its SERVER's, resolved on the server's own three channels
	// — a client-side sentence naming a cloud endpoint would describe an upload
	// the server may never make, and a false disclosure is worse than the
	// silence. The server announces; the client carries audio to whoever it
	// was pointed at.
	if needsEngine && voiceResolveEngine(opts) == config.AudioEngineRecording {
		voiceReportDoubaoPrivacy(opts)
	}
	// The v3 SAUC engine uploads too, and its disclosure is its own sentence:
	// it names the CREDENTIAL SHAPE as well as the destination, because on this
	// channel the shape is the difference between a working run and a 403 that
	// reads like a missing entitlement. The same guard (needsEngine) applies for
	// the same reason — the process that uploads is the process that announces.
	if needsEngine && voiceResolveEngine(opts) == config.AudioEngineSauc {
		voiceReportSaucPrivacy(opts)
	}
	// The MIGRATION NOTICE, same guard and same reason as the disclosure above:
	// a run that will read a pre-rename settings key or a pre-rename env var is
	// told which nested path replaces it, once, before it does the work. It is
	// silent on a migrated document, so it is noise only to an operator who has
	// not migrated yet — the one reader it is for.
	if needsEngine {
		voiceReportLegacyAudioConfig(opts)
	}

	switch {
	// The bare invocation: capture on this machine, transcribe on this machine.
	case opts.capture:
		runVoiceCaptureLocal(opts)

	// Master's top-level --list-devices verb, kept as a CLI surface: it is
	// usable without --client (the headless/scripting path). The table itself
	// comes from this branch's client (--client --list-devices prints the same
	// one) — master's cli_voice_capture.go owned a second, duplicate
	// implementation and could not link alongside this branch's client.
	case opts.listDevices && !opts.client:
		runVoiceClient(opts)

	case opts.client:
		runVoiceClient(opts)

	case !opts.server:
		// Which engine, reading which deployment — the same line the bare
		// capture prints (voiceReportEngineProvenance is silent for the cloud
		// engine). Print it BEFORE the run so it is on stderr even if the
		// engine then fails: "it could not find the weights" is only
		// diagnosable next to "here is where it looked".
		voiceReportEngineProvenance(opts)
		runClock := newVoiceRunClock(voiceRunOrigin())
		text, elapsed, report, err := transcribeFileReport(opts, opts.audio, runClock)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(text)
		fmt.Fprintf(os.Stderr, "[voice] %s in %.1fs (chunk=%ds)\n",
			filepath.Base(opts.audio), elapsed.Seconds(), opts.chunk)
		// THE REPORT IS THE RUN'S OWN MEASUREMENT, not the engine's: the instant it
		// names was taken from the clock this shape started, so a file run says how
		// long the operator waited rather than only how long the engine said it
		// worked.
		voiceReportFirstText(opts, report)

	case opts.stdio:
		if err := voiceServeStdio(opts); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}

	default:
		if err := voiceServeHTTP(opts); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
	}
}

// voiceReadAll reads stdin with a hard cap so a runaway writer cannot exhaust
// memory.
func voiceReadAll(r io.Reader, limit int64) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return buf, nil
}
