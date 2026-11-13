// Package doubaostream is the THIRD speech-recognition engine of
// `gopi voice_input`: 豆包 / Volcengine (ByteDance) **v2 real-time streaming**
// ASR, reached over a hand-written WebSocket on the Go standard library alone.
//
// WHY A SECOND DOUBAO PACKAGE rather than an arm of internal/doubaoasr: the
// two are not the same protocol at all. doubaoasr speaks the v3 录音文件识别
// (AUC) HTTP shape — submit, poll, one transcript per FILE. This package speaks
// the v2 流式 shape — one WebSocket, a packet every few hundred milliseconds,
// a PARTIAL transcript while the audio is still being spoken. Sharing a package
// would mean sharing a name for two different wire protocols (`ResourceID` here
// is a CLUSTER, not a resource id; "sequence" means opposite things), which is
// precisely the drift Rule 13/14 exist to stop. What they DO share is the
// credential CHANNEL — this package reads the same key-FILE knob
// (config.AudioKeyFileEnv / --doubao-key-file / settings doubao_key_file)
// through the caller, and introduces no second credential mechanism.
//
// WHY ZERO DEPENDENCIES: gopi ships as one static binary with one third-party
// module. The official demo for this channel imports github.com/gorilla/websocket
// and github.com/satori/go.uuid; both are avoidable, and the parts that are
// actually needed (an HTTP upgrade, masked frames, ping/pong, gzip) are a few
// hundred lines of stdlib. The constraint is CHECKED by a test that reads every
// production file of this package and refuses any import whose first path
// segment contains a dot.
//
// WHAT THE SERVICE IS AND IS NOT: `code 1013 No valid speeches found in input
// audio` is a CORRECT verdict for audio with no speech in it, not a failure —
// it is reported as NoSpeechError so a caller can distinguish "nobody spoke"
// from "the run broke" (Rule 15). `SERVER_ERROR` frames and a peer close are
// failures and say so, carrying the vendor's own log id.
package doubaostream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ── the endpoint, the cluster, the workflow (named: Rule 1/13) ──────────
const (
	// DefaultEndpoint is the v2 streaming ASR endpoint. It is NOT the v3
	// `sauc` endpoint: v3 streaming is refused for this account (HTTP 403
	// requested resource not granted, measured), and v2 is a separate quota
	// line that IS granted.
	DefaultEndpoint = "wss://openspeech.bytedance.com/api/v2/asr"
	// DefaultCluster decides the PRODUCT LINE the request is billed against,
	// not merely a routing hint: `volcengine_input_common` answers 45000030
	// requested resource not granted, and only the streaming cluster below is
	// granted to this account.
	DefaultCluster = "volcengine_streaming_common"
	// DefaultWorkflow is the pipeline the service runs, from the docs' field
	// table plus the punctuation/ITN stages the probe measured as accepted.
	DefaultWorkflow = "audio_in,resample,partition,vad,fe,decode,itn,nlu_punctuate"
	// DefaultUID is the user identifier the request carries (the docs suggest
	// IMEI/MAC; a fixed product name is the honest value for a CLI).
	DefaultUID = "gopi"
	// resultTypeFull asks for every sentence so far on each partial, rather
	// than only the current one (see the docs' result_type row).
	resultTypeFull = "full"
	// DefaultSampleRate / DefaultBits / DefaultChannel / DefaultFormat
	// describe the audio this client sends: raw little-endian PCM, 16 kHz,
	// 16 bit, mono — the shape a capture produces and the shape the service
	// expects when format is "raw".
	DefaultSampleRate = 16000
	DefaultBits       = 16
	DefaultChannel    = 1
	DefaultFormat     = "raw"
	// DefaultChunkMillis is the audio carried by one packet. The service
	// answers each packet, so this is both the network cadence and the
	// granularity at which partials arrive; 200 ms is the cadence the probe
	// measured (every packet answered with a code-1000 response).
	DefaultChunkMillis = 200
	// maxAudioBytes bounds the audio one request may carry. It is a guard, not
	// a tunable: a caller handing over a file that is a movie should be told,
	// not billed.
	maxAudioBytes = 256 << 20
)

// ── timeouts (named; Rule 1/2: one place reads a knob) ──────────────────
const (
	// defaultDialTimeout bounds the TCP connect and the TLS handshake.
	defaultDialTimeout = 10 * time.Second
	// packetReadBudget is how long the frames ALREADY SENT are waited for in the
	// one dialog read outside the paced loop (the answer to the full client
	// request). Inside the loop the budget is the slot's own remaining time.
	packetReadBudget = 200 * time.Millisecond
	// minDrainBudget is the shortest drain worth starting: below it the read
	// would almost certainly expire before a frame that had already arrived was
	// consumed, and the loop would drift for nothing.
	minDrainBudget = 20 * time.Millisecond
	// maxDrainBudget caps one slot's drain, so a service that answers in bursts
	// cannot make a single packet hold the send loop.
	maxDrainBudget = 200 * time.Millisecond
	// defaultReadTimeout bounds ONE frame read of the FINAL drain, where the
	// service is producing the last transcript and silence is the terminator.
	defaultReadTimeout = 5 * time.Second
	// defaultFinalReadTimeout bounds ONE read after the last packet, while the
	// final transcript is produced. It is the terminator's own budget: the
	// service answers the last packet promptly, and the wall clock a caller
	// sees includes this drain — measured, a 5 s budget here turned a 13 s
	// stream into 21.9 s.
	defaultFinalReadTimeout = 1500 * time.Millisecond
	// defaultCloseTimeout bounds the goodbye frame.
	defaultCloseTimeout = 2 * time.Second
	// drainQuietFrames is how many consecutive read timeouts after the last
	// packet end the final drain: the service answers asynchronously, so the
	// end of the transcript is silence, not a terminator. Two reads' worth of
	// quiet is the budget, and it is deliberately SMALL — the cost of a longer
	// one is paid by every run (see defaultFinalReadTimeout).
	drainQuietFrames = 2
	// errBodyBytes bounds how much of a refusal body is quoted in an error.
	errBodyBytes = 256
	// maxFrameBytes / maxPayloadBytes bound what a single frame (and a single
	// decompressed payload) may be, so a hostile or broken peer cannot make
	// this client allocate without limit.
	maxFrameBytes   = 8 << 20
	maxPayloadBytes = 32 << 20
)

// ── gzip nibble (byte 2's low nibble) ─────────────────────────────────
const compressionGzip = 0x1

// ── upgrade header names ──────────────────────────────────────────────
const (
	headerUpgradeName         = "Upgrade"
	headerUpgradeValue        = "websocket"
	headerConnectionName      = "Connection"
	headerConnectionValue     = "Upgrade"
	headerSecWebSocketKey     = "Sec-WebSocket-Key"
	headerSecWebSocketVersion = "Sec-WebSocket-Version"
	headerSecWebSocketAccept  = "Sec-WebSocket-Accept"
	headerAuthorization       = "Authorization"
	authorizationBearerSep    = ";"
	authorizationBearerPrefix = "Bearer"
	// logIDHeader is the vendor's request log id, present on the UPGRADE
	// RESPONSE and on every ASR response. It is what the vendor's support
	// asks for, so every error this package reports carries it when the
	// service gave one.
	logIDHeader = "X-Tt-Logid"
)

// Options is one streaming transcription request. Every field has a named
// default applied by Transcribe; credential fields are filled by the caller
// from its own credential channel (this package accepts values, and reads a
// file only through LoadCredentials).
type Options struct {
	// AppID / Token are the credential (see LoadCredentials). They are never
	// logged, never embedded in an error and never returned.
	AppID string
	Token string
	// Cluster selects the product line; empty means DefaultCluster.
	Cluster string
	// Endpoint is the wss:// URL; empty means DefaultEndpoint.
	Endpoint string
	// Workflow is the service pipeline; empty means DefaultWorkflow.
	Workflow string
	// UID is the request's user identifier; empty means DefaultUID.
	UID string

	// AudioFile is the audio to stream: a canonical WAV (any of PCM16 /
	// PCM32 / float32 / float64, mono or stereo) or raw 16 kHz mono PCM16 when
	// AudioRawPCM is true. The container is decoded HERE, because the v2
	// channel takes samples, not files.
	AudioFile string
	// AudioRawPCM says AudioFile holds header-less 16 kHz mono PCM16 bytes.
	AudioRawPCM bool
	// ChunkMillis is the audio per packet; 0 means DefaultChunkMillis.
	ChunkMillis int
	// ReadTimeout bounds one frame read; 0 means defaultReadTimeout.
	ReadTimeout time.Duration

	// OnPartial is called for every PARTIAL transcript as it arrives, in
	// arrival order, BEFORE the final text is known. It is the progress
	// channel the caller needs and the reason this arm can serve a UI while
	// still returning one string: the callback carries the "as it is spoken"
	// half, the return value carries the "one transcript" half that
	// transcribeFile's signature demands. A caller that ignores it loses
	// nothing but the progress (nil is the zero value, and the call is
	// guarded).
	OnPartial func(Partial)
}

// Partial is one partial (interim) transcript: the text known so far, and
// whether the service marked it definite (a closed sentence).
type Partial struct {
	// Index counts from 1 in arrival order.
	Index int
	// Text is the transcript so far.
	Text string
	// Definite is the service's own verdict that this sentence is closed.
	Definite bool
	// At is how long after the first audio packet this partial arrived —
	// the first-token latency when Index is 1.
	At time.Duration
}

// Result is one completed streaming transcription.
type Result struct {
	// Text is the final transcript (the streaming arm's answer to
	// transcribeFile's "one string" contract).
	Text string
	// Partials are every partial received, in arrival order — kept so a caller
	// (a report, a test, an operator asking "did partials arrive at all?") can
	// see the streaming behaviour rather than infer it from one string.
	Partials []Partial
	// ReqID / LogID identify the request: the caller's own id and the
	// vendor's, both of which the vendor's support asks for.
	ReqID string
	LogID string
	// AudioMS is how much audio was sent, in milliseconds.
	AudioMS int
	// DurationMS is the service's own duration field, when a response carried
	// one. MEASURED: the v2 channel answers every packet with duration 0, so a
	// caller must not read 0 here as "no audio was processed" — the audio that
	// was SENT is AudioMS.
	DurationMS int
	// NoSpeech reports that the service answered "no valid speech in this
	// audio" (code 1013). The transcript is empty and that is CORRECT for the
	// audio; a caller must not report it as a failure and must not report an
	// empty string as if speech had been recognised.
	NoSpeech bool
	// Elapsed is the wall time of the whole exchange.
	Elapsed time.Duration
	// FirstPartialAt is the time from the start of the exchange to the first
	// partial; 0 when no partial arrived.
	FirstPartialAt time.Duration

	// lastCode is the application code of the most recent response, kept so a
	// stream that ends with no transcript can say WHICH code left it empty.
	lastCode int
	// noSpeechMessage is the service's own wording when it answered 1013.
	noSpeechMessage string
}

// APIError is a failure the service itself reported, or one this client
// detected on the wire. It always carries the vendor log id when the service
// gave one (Rule 15: an incident must not start from zero).
type APIError struct {
	// Op names the step ("connect", "handshake", "send", "read", "server").
	Op string
	// Kind is the server message type, when the failure was one.
	Kind string
	// Code is the application code (SERVER_ERROR's own field, or the code in
	// the response document).
	Code int
	// Message is the service's message.
	Message string
	// LogID / ReqID identify the request.
	LogID string
	ReqID string
	// Err is the underlying transport error, when there was one.
	Err error
}

func (e *APIError) Error() string {
	parts := "streaming asr: " + e.Op
	if e.Code != 0 {
		parts += fmt.Sprintf(": code %d", e.Code)
	}
	if e.Message != "" {
		parts += ": " + e.Message
	}
	if e.LogID != "" {
		parts += " (logid " + e.LogID + ")"
	}
	if e.ReqID != "" {
		parts += " (reqid " + e.ReqID + ")"
	}
	if e.Err != nil {
		parts += ": " + e.Err.Error()
	}
	return parts
}

// Unwrap exposes the transport error so a caller can test for a timeout or a
// closed connection without parsing text.
func (e *APIError) Unwrap() error { return e.Err }

// Is reports whether this failure IS the given one, by the CAUSE it was built
// from. It is what makes ErrFrameDesync (and any other named cause) reachable
// through errors.Is on the error the caller actually receives: the chain is
// APIError → e.Err, and a caller asking "was this a desync?" must not have to
// know the phrasing of the message to find out.
func (e *APIError) Is(target error) bool {
	return e.Err != nil && errors.Is(e.Err, target)
}

// NoSpeechError is the service's code 1013 answer: the audio carried no
// speech. It is an ERROR so that no caller can print an empty transcript as if
// it were a recognition result, and it is its own TYPE so that a caller can
// say the true thing ("no speech in this audio") instead of the false one
// ("the engine failed") — the distinction the brief calls load-bearing.
type NoSpeechError struct {
	// Message is the service's own wording.
	Message string
	// ReqID / LogID identify the request.
	ReqID string
	LogID string
}

func (e *NoSpeechError) Error() string {
	msg := "the service found no speech in this audio"
	if e.Message != "" {
		msg += " (" + e.Message + ")"
	}
	if e.LogID != "" {
		msg += " (logid " + e.LogID + ")"
	}
	return msg
}

// packetAssembler is THE packet geometry of one streaming conversation, and it
// is derived from the protocol rather than from its caller.
//
// IT EXISTS BECAUSE THERE WERE TWO OF IT. The two arms that talk to the v2
// channel assemble their packets differently — the file arm walks a whole
// buffer with an index it derives from the audio timeline (Transcribe, below),
// the live arm accumulates whatever the capture hands it and carries the
// remainder (Session.Say / Session.Finish) — and both spelled the SAME two lines
// out themselves:
//
//	packetBytes = chunkSampleCount(chunkMillis) * bytesPerSample16
//	dur         = pcmDurationMS(len(pcm))
//
// That is the arithmetic Rule 13/14 exist to stop: `bytesPerSample16` is
// described one file over as "the ONE place a sample count becomes a byte
// count", and a second file computing `samples × 2` and `bytes / 32` by hand is
// a second place where the two can drift — silently, because a wrong packet
// size is not an error the service raises (measured: stepping a BYTE buffer by a
// SAMPLE count sent half the audio per packet, doubling the stream's length).
//
// WHAT IT IS NOT: it holds no clock and no state. "How fast to send" is a
// property of the ARMS and not of this type — a file has an external timeline to
// be paced against and a microphone IS one (see session.go's header) — so what
// is shared here is the GEOMETRY, the one arithmetic both arms must agree on.
type PacketAssembler struct {
	// Bytes is the audio one packet carries: whole samples, from the protocol's
	// own sample count. It is the accumulation step of the live arm and the
	// stride of the file arm's walk.
	Bytes int
	// Millis is the cadence those bytes represent, resolved once so the file
	// arm's timeline arithmetic and the library's published default cannot drift
	// apart.
	Millis int
}

// newPacketAssembler derives the geometry for one chunkMillis knob (0 or a
// negative value resolving to DefaultChunkMillis, as every cadence knob here
// does).
func newPacketAssembler(chunkMillis int) PacketAssembler {
	return PacketAssembler{
		Bytes:  chunkSampleCount(chunkMillis) * bytesPerSample16,
		Millis: chunkMillisOrDefault(chunkMillis),
	}
}

// finalPacket builds the end-of-stream packet: the audio still in hand (which
// may be empty — an operator who pressed the finish key between two packets
// still owes the service the flag) with the header's end-of-stream bit set.
func (a PacketAssembler) FinalPacket(tail []byte) ([]byte, error) {
	return buildAudioPacket(tail, true)
}

// packetAssembler is the ONE place the geometry of this conversation is
// computed, and the file arm reads it off the options it already resolves.
func (o Options) packetAssembler() PacketAssembler {
	return newPacketAssembler(o.ChunkMillis)
}

// NewPacketAssembler is the same derivation for a caller OUTSIDE this package:
// the chat surface feeds its packets from its own goroutine and needs the
// packet size to accumulate to, so it asks for the geometry rather than
// computing samples × bytes itself (Rule 13/14 — that product was spelled out
// in cmd/agent too, and it is the arithmetic this type exists to own).
func NewPacketAssembler(chunkMillis int) PacketAssembler {
	return newPacketAssembler(chunkMillis)
}

// DefaultPacketAssembler is the geometry of a session opened with no cadence
// knob: DefaultChunkMillis, and the packet size that follows from it.
func DefaultPacketAssembler() PacketAssembler {
	return newPacketAssembler(0)
}

// Transcribe streams one audio file through the v2 real-time channel and
// returns the final transcript.
//
// THE SHAPE OF THE EXCHANGE (push-to-talk, matching the operator's decision):
// connect → full client request (JSON: credential, cluster, audio geometry) →
// one audio packet per ChunkMillis → the LAST packet's header carries the
// end-of-stream flag → drain the final responses → close. Nothing stays open
// after the call, and nothing is left listening: this is not an always-on
// microphone, which is also what keeps the billing per audio second rather
// than per connection second.
func Transcribe(ctx context.Context, opts Options) (*Result, error) {
	samples, audioMS, err := loadAudio(opts)
	if err != nil {
		return nil, err
	}
	if opts.AppID == "" || opts.Token == "" {
		return nil, errors.New("streaming asr needs an app id and an access token (see LoadCredentials)")
	}
	// The request id comes FIRST: it is stamped into the upgrade request as
	// X-Api-Request-Id (this vendor's convention on every OpenSpeech endpoint
	// — the v2 JSON schema has no such field), so the vendor's log joins the
	// two channels the same way and every error here can name it.
	reqID := newReqID()

	d := wsDialer{
		timeout:         defaultDialTimeout,
		handshakeHeader: http.Header{},
		header: http.Header{
			// The credential travels as an UPGRADE header, and the separator is
			// a SEMICOLON — `Bearer;<token>` — which is the vendor's own
			// spelling (their Go demo builds the same string). A space instead
			// of the semicolon is refused, and the refusal looks like a
			// routing error rather than an auth one.
			headerAuthorization:      []string{authorizationBearerPrefix + authorizationBearerSep + opts.Token},
			headerAPIRequestIDHeader: []string{reqID},
		},
	}
	conn, resp, err := wsDial(ctx, endpointOrDefault(opts.Endpoint), d)
	if err != nil {
		// The log id is on the UPGRADE RESPONSE even when the upgrade failed,
		// so a refused handshake still gets the identifier support needs.
		logID := ""
		if resp != nil {
			logID = resp.Header.Get(logIDHeader)
		}
		return nil, &APIError{Op: "connect", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
	}
	logID := resp.Header.Get(logIDHeader)
	defer func() { _ = conn.Close() }() //stylegate:ignore one-shot batch session: the deferred close releases the socket after the response was read, and its error has no reader

	conn.writeTimeout = defaultDialTimeout
	conn.SetReadDeadline(opts.readTimeout())

	res := &Result{ReqID: reqID, LogID: logID, AudioMS: audioMS}
	start := time.Now()
	defer func() { res.Elapsed = time.Since(start) }()

	// 1) full client request.
	doc := requestDoc{
		App:     appMeta{AppID: opts.AppID, Token: opts.Token, Cluster: clusterOrDefault(opts.Cluster)},
		User:    userMeta{UID: uidOrDefault(opts.UID)},
		Audio:   audioMeta{Format: DefaultFormat, Rate: DefaultSampleRate, Bits: DefaultBits, Channel: DefaultChannel, Language: defaultLanguage},
		Request: requestMeta{ReqID: reqID, Sequence: 1, NBest: 1, Workflow: workflowOrDefault(opts.Workflow), ResultType: resultTypeFull, ShowUtterances: true},
	}
	frame, err := buildFullClientRequest(doc)
	if err != nil {
		return nil, &APIError{Op: "build-request", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
	}
	if err := conn.writeFrame(opcodeBinary, frame); err != nil {
		return nil, &APIError{Op: "send-request", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
	}
	// The service answers the full client request with a code-1000 response
	// before any audio is sent; reading it here is what proves the credential,
	// the cluster and the geometry were accepted (a bad cluster answers
	// 45000030 at THIS point, before a second of audio is billed). Acks are
	// read past: the first message on a fresh connection may be one.
	for i := 0; i < dialogReadsAfterRequest; i++ {
		if err := res.absorb(conn, start, packetReadBudget, opts.OnPartial, logID); err != nil {
			return nil, err
		}
	}

	// 2) the audio: one packet per chunk, paced in REAL TIME against the audio
	// timeline, the last one flagged as the end.
	//
	// THE PACING IS PART OF THE PROTOCOL, not a nicety. The service is a
	// real-time recogniser: it expects the nth packet at n × chunk after the
	// first, and a sender that stops to wait for each answer drifts behind
	// (measured: a 200 ms read per packet turned 10.3 s of audio into a 29.6 s
	// stream — a run that bills 3× the audio and delivers its first partial at
	// 3.65 s). So each packet is written at its own slot on the timeline, and
	// the frames that arrived are read from whatever slack the slot leaves.
	// chunkBytes, not chunkSamples: loadAudio yields PCM16 BYTES, and stepping
	// a byte buffer by a SAMPLE count sends half the audio per packet — which
	// is exactly what this loop did first, and what made a 10.3 s clip stream
	// for 20.7 s (104 packets × 200 ms) with every partial an audio-second
	// late. bytesPerSample16 is the one place that conversion lives, and the
	// assembler below is the one place it is APPLIED — the live arm reads the
	// same geometry from the same constructor instead of spelling it again.
	asm := opts.packetAssembler()
	chunkBytes := asm.Bytes
	chunkDur := time.Duration(asm.Millis) * time.Millisecond
	audioStart := time.Now()
	for sent, index := 0, 0; sent < len(samples); sent, index = sent+chunkBytes, index+1 {
		end := sent + chunkBytes
		last := end >= len(samples)
		if end > len(samples) {
			end = len(samples)
		}
		packet, err := buildAudioPacket(samples[sent:end], last)
		if err != nil {
			return nil, &APIError{Op: "build-audio", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
		}
		// Wait for this packet's slot (a sender that is AHEAD of the timeline
		// waits; one that is behind sends immediately — the service's own
		// packet timeout is the bound, and losing time here is better than
		// losing audio).
		if wait := time.Until(audioStart.Add(time.Duration(index) * chunkDur)); wait > 0 {
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, &APIError{Op: "send-audio", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
			}
		}
		if err := conn.writeFrame(opcodeBinary, packet); err != nil {
			return nil, &APIError{Op: "send-audio", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
		}
		if last {
			break
		}
		// Drain what the service has said, using the rest of this slot. A
		// slot with no time left is skipped rather than borrowed: borrowing
		// is what drifts.
		budget := time.Until(audioStart.Add(time.Duration(index+1) * chunkDur))
		if budget < minDrainBudget {
			continue
		}
		if budget > maxDrainBudget {
			budget = maxDrainBudget
		}
		if err := res.absorb(conn, start, budget, opts.OnPartial, logID); err != nil {
			return nil, err
		}
	}
	// 3) after the last packet the service may still be producing the final
	// transcript; read until it goes quiet.
	if err := res.drain(conn, start, logID); err != nil {
		return nil, err
	}

	code := res.lastCode
	if res.Text == "" && res.NoSpeech {
		return res, &NoSpeechError{Message: res.noSpeechMessage, ReqID: reqID, LogID: logID}
	}
	if res.Text == "" {
		// Rule 15: an empty transcript with a 1000 code is NOT "you said
		// nothing" — it is a result this client cannot explain, and it says so
		// with everything it knows rather than returning "".
		return res, &APIError{
			Op: "final", Code: code,
			Message: "the stream ended without a transcript and without a code saying why",
			LogID:   logID, ReqID: reqID,
		}
	}
	return res, nil
}

// absorb reads the frames ONE packet produced and folds them into the result,
// returning when the service has nothing more immediately to say.
//
// THIS LOOP IS THE WHOLE PACING OF THE CHANNEL, and both of its first two
// shapes were measured wrong on real traffic (2026-10-08):
//
//  1. "read one frame per packet" — the service sends a FULL response followed
//     by a bare ACK (message type 0x0b, sequence 0, no payload), so the frame
//     read for packet N is the ACK for packet N-1, and the transcript is never
//     seen until the last packet. Symptom: a correct final transcript with NO
//     partials at all.
//  2. "read until a FULL response arrives" — deadlock, and the service says so:
//     `code 1020 Timeout waiting for 2-th packet`. The response for packet N and
//     the ACK for packet N are both already in the socket, so the reader that
//     insists on one more response blocks waiting for a packet that the writer
//     above it will not send until it returns.
//
// What works is draining what has ARRIVED and then returning: frames are read
// while they keep coming, and a read that expires ends the drain, so the next
// packet goes out within packetReadBudget of the previous one. The transcript
// still grows (each response is folded as it is read, usually one packet after
// it was produced), and the send loop never stalls — which is the property the
// service's own packet timeout demands.
func (r *Result) absorb(conn *wsConn, start time.Time, budget time.Duration, onPartial func(Partial), logID string) error {
	for {
		msg, err := r.readOne(conn, start, budget, onPartial, logID)
		if err != nil {
			return err
		}
		if msg == nil {
			return nil
		}
	}
}

// ErrFrameDesync is a read that failed with part of a frame already consumed.
//
// IT IS NOT A TIMEOUT AND NOT SILENCE. A frame is read with io.ReadFull, so an
// interruption at a frame BOUNDARY (nothing consumed) is resumable — the next
// read starts where this one did, which is what a quiet service looks like. An
// interruption INSIDE a frame is not: the remaining bytes of that frame are
// still in the socket, and the next parse reads them as a header (measured live:
// a payload byte 0x03 arrives as "unknown WebSocket opcode 0x3", which killed a
// `/voice` dictation). Keeping those two facts apart is what readOne does with
// this error: the first stays (nil, nil), the second becomes a real read
// failure — the caller is never handed a transcript built on a desynced stream.
//
// It is EXPORTED because the fact is worth stating outside this package: a
// desync means the connection was left mid-frame, so nothing that follows it on
// that stream can be trusted.
var ErrFrameDesync = errors.New("read interrupted inside a WebSocket frame")

// readOne reads at most one message frame, folding it into the result.
//
// THREE OUTCOMES, and the difference between the first two is the whole of the
// framing-state question (Rule 15 — a state that is not resumable must never be
// reported as one that is):
//
//   - a timeout at a frame BOUNDARY returns (nil, nil): silence is a state, not
//     a failure, while the audio is still being sent. This is what the live
//     session counts to notice the end of the transcript, and what Finish's
//     bounded wait is built on.
//   - a timeout (or any failure) INSIDE a frame returns errFrameDesync: the
//     framing state is half-consumed and unrecoverable.
//   - anything else is the real transport failure it is.
//
// A zero timeout arms NO deadline, and that is the audio-flow phase's setting
// (Session.readLoop): with no deadline, no deadline can land inside a frame, so
// the desync above is unreachable while the operator is speaking. The drain
// phase still arms one — which is why the guard stays.
func (r *Result) readOne(conn *wsConn, start time.Time, timeout time.Duration, onPartial func(Partial), logID string) (*serverMessage, error) {
	conn.SetReadDeadline(timeout)
	raw, err := conn.readMessage()
	if err != nil {
		var closeErr *wsCloseError
		if errors.As(err, &closeErr) {
			// The peer ending the conversation is a RESULT to report, never a
			// silent truncation: a mid-stream close is how "the quota ran out"
			// or "the credential expired" arrives.
			return nil, &APIError{Op: "peer-close", Message: closeErr.Error(), LogID: logID, ReqID: r.ReqID}
		}
		if conn.frameBytesRead() > 0 {
			// The framing state cannot be resumed, so this is a failure EVEN
			// WHEN the error is a timeout. Reading on is what parses the rest
			// of the frame as a header. The cause carries the sentinel AND the
			// transport error, so a caller can ask for either with errors.Is.
			return nil, &APIError{
				Op:      "read",
				Message: fmt.Sprintf("the framing state cannot be resumed (%d bytes of the current frame were consumed)", conn.frameBytesRead()),
				LogID:   logID, ReqID: r.ReqID,
				Err: fmt.Errorf("%w: %w", ErrFrameDesync, err),
			}
		}
		if isTimeout(err) {
			return nil, nil
		}
		return nil, &APIError{Op: "read", Message: err.Error(), LogID: logID, ReqID: r.ReqID, Err: err}
	}
	return r.fold(raw, start, onPartial, logID)
}

// fold decodes one server frame and records it.
func (r *Result) fold(raw []byte, start time.Time, onPartial func(Partial), logID string) (*serverMessage, error) {
	msg, err := parseServerMessage(raw)
	if err != nil {
		return nil, &APIError{Op: "parse", Message: err.Error(), LogID: logID, ReqID: r.ReqID, Err: err}
	}
	switch msg.Kind {
	case msgTypeServerAck:
		// An ack carries no new result; it is how the service says "packet
		// received".
		return &msg, nil
	case msgTypeServerError:
		return nil, &APIError{
			Op: "server", Kind: kindName(msg.Kind), Code: int(msg.Code),
			Message: serverErrorMessage(msg), LogID: firstNonEmpty(msg.Response.LogID, logID), ReqID: r.ReqID,
		}
	case msgTypeServerFullResponse:
		return &msg, r.foldResponse(msg, start, onPartial, logID)
	}
	return &msg, nil
}

// foldResponse records a full server response: its code, its duration and its
// text (as a partial, and as the running final text).
func (r *Result) foldResponse(msg serverMessage, start time.Time, onPartial func(Partial), logID string) error {
	resp := msg.Response
	r.lastCode = resp.Code
	if resp.ReqID != "" {
		r.ReqID = resp.ReqID
	}
	if id := firstNonEmpty(resp.LogID, logID); id != "" {
		r.LogID = id
	}
	if d, err := resp.Duration.Int64(); err == nil {
		r.DurationMS = int(d)
	}
	switch resp.Code {
	case codeSuccess:
	case CodeNoSpeech:
		// A CORRECT verdict for audio with no speech: remembered, not raised
		// here — the stream may still produce more (a long silence at the head
		// of a clip answers 1013 for its first packets), and only a stream that
		// ends with nothing to show is reported as NoSpeechError.
		r.NoSpeech = true
		r.noSpeechMessage = resp.Message
		return nil
	default:
		return &APIError{
			Op: "server", Kind: kindName(msg.Kind), Code: resp.Code, Message: resp.Message,
			LogID: r.LogID, ReqID: r.ReqID,
		}
	}

	text := resp.text()
	if text == "" {
		return nil
	}
	if text == r.Text {
		return nil
	}
	r.Text = text
	r.Partials = append(r.Partials, Partial{
		Index:    len(r.Partials) + 1,
		Text:     text,
		Definite: definiteOf(resp),
		At:       time.Since(start),
	})
	if r.FirstPartialAt == 0 {
		r.FirstPartialAt = time.Since(start)
	}
	if onPartial != nil {
		onPartial(r.Partials[len(r.Partials)-1])
	}
	return nil
}

// drain reads after the last packet, while the final transcript is produced.
// Silence ENDS it: consecutive read timeouts mean the service has nothing more
// to say. A read that carries only acks resets nothing — an ack is progress,
// so the quiet counter counts only actual silence.
func (r *Result) drain(conn *wsConn, start time.Time, logID string) error {
	quiet := 0
	for quiet < drainQuietFrames {
		msg, err := r.readOne(conn, start, defaultFinalReadTimeout, nil, logID)
		if err != nil {
			return err
		}
		if msg == nil {
			quiet++
			continue
		}
		quiet = 0
		if msg.Kind == msgTypeServerFullResponse && msg.Response.Sequence < 0 {
			// The service echoes the last packet's negative sequence on the
			// final response; that is an explicit terminator.
			return nil
		}
	}
	return nil
}

// definiteOf reports whether the response closed a sentence (any definite
// utterance, or a negative sequence meaning "this is the final answer").
func definiteOf(resp serverResponse) bool {
	if resp.Sequence < 0 {
		return true
	}
	for _, res := range resp.Result {
		for _, u := range res.Utterances {
			if u.Definite {
				return true
			}
		}
	}
	return false
}

// serverErrorMessage pulls the human-readable reason out of an error frame: the
// payload is a response document when it parses as one, and the raw bytes
// otherwise (the docs' own demo prints them verbatim).
func serverErrorMessage(msg serverMessage) string {
	if msg.HasResponse && msg.Response.Message != "" {
		return msg.Response.Message
	}
	if len(msg.JSON) > 0 {
		return string(msg.JSON)
	}
	return "the service reported an error with no message"
}

// newReqID builds the request identifier: RFC 4122 version 4 in the 36-char
// dashed form. It is random from crypto/rand rather than UUID-shaped from a
// formatting library — the protocol wants an identifier unique PER REQUEST, and
// that is exactly what a random 122-bit value is.
func newReqID() string {
	var b [16]byte
	if _, err := randRead(b[:]); err != nil {
		// A request id that cannot be generated is not a reason to invent one
		// silently; the caller sees a fixed, obviously-synthetic value and the
		// transcript is still attributed to a log id.
		return "reqid-unavailable"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// netError is the shape a deadline check needs (the standard library's
// net.Error without importing net for one assertion).
type netError interface {
	error
	Timeout() bool
}

// isTimeout reports whether an error is a deadline expiring on this
// connection — a state the sender continues past, never a failure.
func isTimeout(err error) bool {
	var ne netError
	return errors.As(err, &ne) && ne.Timeout()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ── the one upgrade header that is not part of the WebSocket handshake ──
//
// headerAPIRequestIDHeader carries the request id on the UPGRADE request. The
// v2 JSON schema has no such field, but the header is this vendor's own
// convention on every OpenSpeech endpoint (the v3 channel sets the same one),
// so sending it makes a streaming request joinable in the account's logs.
const headerAPIRequestIDHeader = "X-Api-Request-Id"

// dialogReadsAfterRequest is how many frames the dialogue reader passes over on
// the FIRST request before it accepts silence as the answer: an ack, then the
// response, with slack for the service to answer in its own order.
const dialogReadsAfterRequest = 4

// sleepCtx waits for d, or returns ctx's error if it ends first — the paced
// send loop must still honour a cancelled context.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
