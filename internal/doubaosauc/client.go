package doubaosauc

// client.go — the request: one audio file in, one transcript out.
//
// THE SHAPE IS THE FILE-SEMANTICS ONE (DefaultEndpoint is
// `sauc/bigmodel_nostream`), which is what transcribeFile's contract demands:
// every shape that produces text in voice_input hands over ONE audio path and
// consumes ONE string. The bidirectional endpoint is reachable by naming it,
// and Options.Paced controls whether the audio is pushed against its own
// timeline; those are the two knobs a caller genuinely owns, and nothing about
// the protocol is re-derived per caller.

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/doubaostream"
)

// ── sequence numbering of one conversation ─────────────────────────────
const (
	// fullRequestSequence is the sequence the session's parameter message
	// consumes; the server auto-assigns it, and an audio packet that repeats it
	// is refused (see fullClientRequestFrame).
	fullRequestSequence int32 = 1
	// firstAudioSequence is therefore where the audio starts.
	firstAudioSequence int32 = 2
)

// ── read budgets ───────────────────────────────────────────────────────
const (
	// finalBudget bounds the wait for the last package after the audio has all
	// been sent. MEASURED: the endpoint answers a 4.83 s clip's final packet
	// within about half a second of receiving the audio, so a generous budget
	// costs nothing on a healthy run and turns an unreachable service into a
	// REPORT instead of a hang.
	finalBudget = 30 * time.Second
	// drainQuietReads is how many consecutive reads may time out, with the
	// audio fully sent, before the run gives up on the final package. The
	// service answers asynchronously, so "quiet" is a budget rather than a
	// terminator.
	drainQuietReads = 3
	// drainReadBudget bounds ONE read inside the final drain.
	drainReadBudget = 3 * time.Second
	// incomingQueueLen bounds the messages the reader goroutine may hold ahead
	// of the writer. The writer drains between packets, so this is a safety
	// margin rather than a working depth.
	incomingQueueLen = 4096
)

// headerFieldBytes is the width of the two optional envelope fields.
const headerFieldBytes = 4

// ── the request geometry the service is sent ───────────────────────────
//
// The values come from the transport package's own published constants rather
// than from a second copy: the decoder there produces exactly this geometry
// (16 kHz, 1 channel, 16-bit samples), and a client that described its own
// audio with different numbers would ask the service to interpret the bytes
// wrongly — a mistake that produces a transcript of nonsense rather than an
// error.
const (
	audioSampleRate = doubaostream.DefaultSampleRate
	audioBits       = doubaostream.DefaultBits
	audioChannels   = doubaostream.DefaultChannel
)

// Options is one SAUC v3 transcription request.
//
// THE CREDENTIAL IS EXACTLY ONE OF TWO SHAPES, and the field names say which is
// which rather than leaving it to a caller to guess: AppKey+AccessKey is the
// legacy console pair (the measured WORKING shape), APIKey is the single-key
// shape. A caller that fills neither is refused BEFORE a socket is opened, by
// name (see ResolveCredentials).
type Options struct {
	// AppKey / AccessKey are the legacy console credential pair, sent as
	// HeaderAppKey / HeaderAccessKey. Never printed.
	AppKey    string
	AccessKey string
	// APIKey is the single-key shape, sent as HeaderAPIKey. Never printed.
	APIKey string
	// CredentialFile is the KEY=VALUE document the credential is read from
	// (APPID → AppKey, TOKEN → AccessKey). Empty means the transport package's
	// published default — the caller resolves the knob, this package resolves
	// the FILE.
	CredentialFile string

	// ResourceID is the X-Api-Resource-Id product line; empty means
	// DefaultResourceID. It is never left to a service-side default: the header
	// is always written.
	ResourceID string
	// Endpoint is the wss:// URL; empty means DefaultEndpoint.
	Endpoint string
	// UID identifies the caller to the service; empty means defaultUID.
	UID string

	// AudioFile is a canonical WAV (PCM16/PCM32/float32/float64, mono or
	// stereo) — the container is decoded to the 16 kHz mono PCM16 the service
	// takes, by the SAME decoder the v2 arm uses (doubaostream.DecodeAudio).
	AudioFile string

	// SegmentMillis is the audio one packet carries; 0 means
	// defaultSegmentMillis.
	SegmentMillis int
	// Paced pushes each packet at its own slot on the audio timeline instead of
	// as fast as the socket accepts them. It is FALSE by default because the
	// file-semantics endpoint takes a whole file and bills by its duration, so
	// waiting out the clip's own length buys nothing; a caller talking to the
	// bidirectional endpoint (where the service expects real-time arrival) sets
	// it — and the measurement behind that split is in DefaultEndpoint's
	// comment.
	Paced bool

	// OnPartial, when set, is called for every partial transcript the service
	// produces: the text known SO FAR, which each response REWRITES rather than
	// extends (measured 2026-10-13 — see session.go's file header). It is what
	// makes this channel usable interactively, and it is deliberately unused by
	// Transcribe, whose contract is one string out. A nil callback loses nothing
	// but the progress.
	OnPartial func(Partial)
}

// Result is one completed v3 transcription.
type Result struct {
	// Text is the transcript (the service's LATEST full text).
	Text string
	// LogID is the vendor's log id, from the upgrade response and from every
	// response frame — the ONE identifier the vendor's support asks for.
	LogID string
	// ReqID is the caller's own request id.
	ReqID string
	// AudioMS is how much audio was sent, in milliseconds.
	AudioMS int
	// Packets is how many audio packets were written.
	Packets int
	// Elapsed is the wall time of the whole exchange.
	Elapsed time.Duration
	// FirstTextAt is the time from the start to the first response carrying
	// non-empty text; 0 when no response carried any. On the file-semantics arm
	// the first text IS the answer, so this is the arm's latency figure.
	FirstTextAt time.Duration
	// Codes are the non-zero application codes the service returned, in arrival
	// order. An empty list on a completed run is the normal case.
	Codes []int
	// NoSpeech reports the service's own "no valid speech" verdict: the
	// transcript is empty and that is CORRECT for the audio.
	NoSpeech bool

	// last records that the service marked a response as the final package.
	last bool
	// errMsg is the service's own error text, when a response carried one.
	errMsg string
	// frames is how many server messages were actually decoded — the number
	// that separates "the socket said nothing" from "the socket said something
	// I could not use", which the pre-fix error could not tell apart.
	frames int
	// lastMsgType is the message type of the most recent frame (the envelope's
	// own nibble: 0x9 is a full response, 0xf an error frame). It is quoted in
	// the failure message so a reader knows WHAT arrived.
	lastMsgType byte
	// lastDecoded reports whether the MOST RECENT frame's payload survived JSON
	// decoding. A frame that arrived but did not decode is a third thing again:
	// the service answered, and this client could not read the answer.
	lastDecoded bool
	// rawPayload is that last frame's payload, quoted (see
	// responseObservation.rawPayload).
	rawPayload string
	// closeReason is the peer's own goodbye — its close code and reason text —
	// recorded when the socket ends, so a normal close is not reported as
	// silence with no provenance.
	closeReason string
	// level is THIS CLIENT's own measurement of the audio it sent (audio_level.go),
	// folded by whichever arm produced the result: Transcribe measures the decoded
	// file once, Session folds each captured packet. It answers a question the four
	// states above cannot — the service says "the final package carried an empty
	// transcript", and this says whether there was anything to hear in the first
	// place, WITH the figures.
	level audioLevel
}

// APIError is a failure at a named step, carrying everything the vendor's
// support asks for (Rule 15: an incident must not start from zero).
type APIError struct {
	// Op is the step: one of the op* constants.
	Op string
	// Code is the application code the service returned, 0 when none.
	Code int
	// Message is the service's own wording, or this client's.
	Message string
	// LogID / ReqID identify the request.
	LogID string
	ReqID string
	// Err is the underlying failure, when there was one.
	Err error
}

func (e *APIError) Error() string {
	parts := "sauc v3 asr: " + e.Op
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
	// THE UNDERLYING FAILURE IS PROJECTED ONCE. MEASURED (2026-11-15, this host) on
	// a real MP3: the command printed
	//   request: the streaming channel takes a WAV or bare PCM: … (reqid …): the
	//   streaming channel takes a WAV or bare PCM: …
	// because the constructor fills Message with err.Error() AND carries err in Err,
	// and this renderer appended both. The sentence was therefore twice in one
	// line, which reads as two independent failures and buries the request id
	// between them. `Err` still rides the struct — errors.Is/As and Unwrap are
	// untouched — it is only the STRING projection that is not repeated.
	if e.Err != nil && !strings.Contains(e.Message, e.Err.Error()) {
		parts += ": " + e.Err.Error()
	}
	return parts
}

// Unwrap exposes the underlying failure so a caller can test for a timeout or a
// closed connection without parsing text.
func (e *APIError) Unwrap() error { return e.Err }

// NoSpeechError is the service's verdict that the audio carried no speech. It
// is an ERROR rather than an empty transcript so that no caller can print ""
// as though it were a recognition result, and it is its own TYPE so a caller
// can say the true sentence ("no speech in this audio") instead of the false
// one ("the engine failed").
type NoSpeechError struct {
	// Message is the service's own wording.
	Message string
	// LogID / ReqID identify the request.
	LogID string
	ReqID string
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

// NoTranscriptError is the verdict that the run produced NO TRANSCRIPT and the
// service did not explain it with a code — the state the four sentences above
// describe. It is a TYPE of its own, rather than a plain *APIError, because the
// CLI's file shapes must answer it differently from a CHANNEL failure: an empty
// transcript is "this audio produced nothing" (the batch engine's own shaped
// verdict travels the same way, voiceNoTranscriptError in cmd/agent), while a
// refused handshake or a protocol breakage is "the run could not happen". One
// type for both is what made the two indistinguishable before.
//
// It carries the finding SENTENCE (the four states, the frame counts, the peer's
// goodbye) and this client's own measurement of the audio, so a caller can print
// the first and, when it wants to, quote the second.
type NoTranscriptError struct {
	// Finding is emptyTranscriptFinding's sentence.
	Finding string
	// Level is this client's measurement of the audio it sent. Measured is false
	// when nothing was measured — never "silent" (Rule 15).
	Level audioLevel
	// LogID / ReqID identify the request.
	LogID string
	ReqID string
}

func (e *NoTranscriptError) Error() string { return e.Finding }

// ── the FOUR empty-transcript verdicts ─────────────────────────────────
//
// finish() has to answer four different questions with four different
// sentences, because the four facts have four different owners: a silent
// socket is an account/network question, a FINAL package carrying an empty
// transcript is a question about the AUDIO, a code is the service's own
// verdict, and the vendor's error text is the service's own wording. One
// sentence for all four is what the pre-fix code did, and it made every one of
// them unactionable (Rule 15: a failure says what it saw).
//
// The sentences are constants rather than literals so the tests assert the SAME
// text a run prints (Rule 1/13), and so a caller grepping a log and a test
// reading an error agree on one vocabulary.
const (
	// noResponseSentence is the verdict when the socket produced NO response
	// frame at all: nothing was decoded, so there is nothing about the audio to
	// report — the finding is about the channel.
	noResponseSentence = "the stream produced no response frame at all"
	// emptyFinalSentence is the verdict when the service's FINAL package
	// arrived and carried an empty transcript with no code: the service
	// finished the utterance and recognised nothing in it (measured on real
	// traffic — see finish_states_test.go's header for the payload).
	emptyFinalSentence = "the service's final package carried an empty transcript and no code"
	// noConclusionSentence is the verdict when responses arrived but the stream
	// never reached a final package: the exchange has no conclusion yet, which
	// is a different finding from "the service finished and said nothing".
	noConclusionSentence = "the stream ended before the service's final package"
)

// serviceNoSpeechMarker is the phrase the vendor uses for "the audio carried no
// speech". It is the ONE heuristic in this package, and it is LABELLED as one:
// the wording was measured on the v2 streaming channel (code 1013, "No valid
// speeches found in input audio"), and here it only decides whether an empty
// transcript with a message is reported as NoSpeech instead of as an
// unexplained emptiness. A message that does not carry it falls through to the
// honest error below — never to a fabricated verdict.
const serviceNoSpeechMarker = "no valid speech"

// ── the request and response documents ─────────────────────────────────

// requestDoc is the JSON the session's first frame carries. The field set is
// the protocol's; the ones gopi sets are the ones the probe's successful runs
// set — ITN and punctuation on (a human reads the transcript) and utterances
// shown (the timings are evidence) — and everything the vendor enables by
// default is left at its zero value rather than guessed at.
type requestDoc struct {
	User    userMeta    `json:"user"`
	Audio   audioMeta   `json:"audio"`
	Request requestMeta `json:"request"`
}

type userMeta struct {
	UID string `json:"uid"`
}

type audioMeta struct {
	Format  string `json:"format"`
	Codec   string `json:"codec"`
	Rate    int    `json:"rate"`
	Bits    int    `json:"bits"`
	Channel int    `json:"channel"`
}

type requestMeta struct {
	ModelName      string `json:"model_name"`
	EnableITN      bool   `json:"enable_itn"`
	EnablePUNC     bool   `json:"enable_punc"`
	ShowUtterances bool   `json:"show_utterances"`
}

// responseDoc is one server response's JSON payload. `result.text` is the
// service's LATEST FULL TEXT (not a delta), which is why Result.Text is
// REPLACED rather than appended — appending would repeat the transcript once
// per packet.
//
// THE SHAPE IS THE MEASUREMENT, and it is worth stating because the vendor's
// own SDK snapshot suggests another one: the endpoint measured on 2026-10-13
// (`bigmodel_async`, and `bigmodel_nostream` before it) answers with
// `{"audio_info":…,"result":{"text":…}}` DIRECTLY — there is no `payload_msg`
// object wrapping it (devdoc/doubao-asr-eval/minclient's raw transcript is the
// evidence). A `payload_msg` field would therefore be a field nothing sends.
type responseDoc struct {
	Code   int            `json:"code"`
	Error  string         `json:"error,omitempty"`
	Result responseResult `json:"result"`
}

// responseResult is the `result` object of one response. It is a NAMED type
// rather than an anonymous struct because two readers build one (this package's
// tests, and any caller that wants to state a response's shape) and an anonymous
// struct cannot be named at a call site without repeating its every field.
type responseResult struct {
	// Text is the LATEST FULL TEXT (see responseDoc).
	Text string `json:"text"`
	// Utterances is the service's own segmentation. `definite` is its verdict
	// that an utterance is closed; the list is absent (`omitempty` on the far
	// side) on a response that carries no segmentation, which is the honest zero.
	Utterances []responseUtterance `json:"utterances,omitempty"`
}

// responseUtterance is one segment of the service's segmentation.
type responseUtterance struct {
	// Definite is the service's verdict that this utterance is closed.
	Definite bool `json:"definite"`
}

// endpointOrDefault is THE ONE place an empty endpoint becomes a URL, so the
// file arm and the live arm cannot open two different destinations for one
// configuration (Rule 2).
func endpointOrDefault(opts Options) string {
	if ep := strings.TrimSpace(opts.Endpoint); ep != "" {
		return ep
	}
	return DefaultEndpoint
}

// resourceIDOrDefault is the same rule for the product line.
func resourceIDOrDefault(opts Options) string {
	if rid := strings.TrimSpace(opts.ResourceID); rid != "" {
		return rid
	}
	return DefaultResourceID
}

// Transcribe runs one file through the v3 SAUC channel.
//
// IT REFUSES BEFORE IT CONNECTS when no credential of either shape is present:
// the refusal names both shapes, so a missing credential is a one-line fix
// rather than a 403 that reads like a missing entitlement.
func Transcribe(ctx context.Context, opts Options) (*Result, error) {
	cred, err := ResolveCredentials(opts)
	if err != nil {
		return nil, err
	}
	reqID, err := newRequestID()
	if err != nil {
		return nil, &APIError{Op: opCredential, Message: "generate the request id", Err: err}
	}
	endpoint := endpointOrDefault(opts)
	resourceID := resourceIDOrDefault(opts)

	// The audio is loaded BEFORE the socket is opened: a file this arm cannot
	// read is a local report, never a stream the service has to time out.
	samples, audioMS, err := doubaostream.DecodeAudio(opts.AudioFile)
	if err != nil {
		return nil, &APIError{Op: opRequest, Message: err.Error(), ReqID: reqID, Err: err}
	}
	segMillis := opts.SegmentMillis
	if segMillis <= 0 {
		segMillis = defaultSegmentMillis
	}
	segBytes := audioSampleRate * segMillis / milliPerSecond * bytesPerSample16
	if segBytes <= 0 {
		return nil, &APIError{Op: opRequest, ReqID: reqID,
			Message: fmt.Sprintf("a %d ms segment is below one sample frame", segMillis)}
	}

	conn, err := doubaostream.WsDial(ctx, endpoint, doubaostream.WsDialOptions{
		Header: cred.UpgradeHeaders(resourceID, reqID),
	})
	if err != nil {
		// The transport's message already names the status and the vendor's
		// reason body; the log id rides along from the handshake response.
		return nil, &APIError{Op: opConnect, Message: err.Error(), ReqID: reqID, Err: err}
	}
	defer func() { _ = conn.Close() }() //stylegate:ignore one-shot session: the close releases the socket after the transcript was read, and its error has no reader

	res := &Result{ReqID: reqID, LogID: conn.LogID(), AudioMS: audioMS, level: measureAudioLevel(samples)}
	start := time.Now()
	defer func() { res.Elapsed = time.Since(start) }()

	// THE READER RUNS AHEAD OF THE WRITER, in its own goroutine, pumping every
	// message into a channel. This is not an optimisation: the socket must be
	// drained while the audio is being written, or the kernel's receive buffer
	// fills and the WRITE blocks — the run then stalls against its own unread
	// responses. It is also what makes patience possible, since the sender
	// never waits on the service and the final wait happens once, at the end.
	incoming := make(chan []byte, incomingQueueLen)
	readErr := make(chan error, 1)
	// peerGone carries the peer's own goodbye sentence, when it sent one. It is
	// a CHANNEL rather than a field on Result because the reader is a separate
	// goroutine and Result is folded by this one: a shared field would be a data
	// race for a string that only has to survive until the report is built.
	peerGone := make(chan string, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(incoming)
		for {
			// No deadline on the flow phase: the bound is the sender's own, and
			// a deadline landing mid-message would desynchronise the frame
			// state (see the transport's SetReadDeadline contract).
			conn.SetReadDeadline(0)
			msg, err := conn.ReadMessage()
			if err != nil {
				var closeErr *doubaostream.WsCloseError
				if !errors.As(err, &closeErr) || closeErr.Code != doubaostream.WsCloseCodeNormal {
					readErr <- err
				}
				// The peer's goodbye is RECORDED whether or not it is a
				// failure: a normal close is the provenance of "nothing more is
				// coming", and an end-of-run report that cannot say why the
				// stream stopped makes every incident start from zero (Rule 15).
				select {
				case peerGone <- err.Error():
				default:
				}
				return
			}
			select {
			case incoming <- msg:
			case <-done:
				return
			}
		}
	}()

	doc := requestDoc{
		User:  userMeta{UID: uidOrDefault(opts.UID)},
		Audio: audioMeta{Format: audioFormatRaw, Codec: audioCodecRaw, Rate: audioSampleRate, Bits: audioBits, Channel: audioChannels},
		Request: requestMeta{
			ModelName:      modelName,
			EnableITN:      true,
			EnablePUNC:     true,
			ShowUtterances: true,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, &APIError{Op: opRequest, Message: "marshal the request document", ReqID: reqID, LogID: res.LogID, Err: err}
	}
	frame, err := fullClientRequestFrame(body)
	if err != nil {
		return nil, &APIError{Op: opRequest, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
	}
	if err := conn.WriteBinary(frame); err != nil {
		return nil, &APIError{Op: opRequest, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
	}
	res.absorbReady(incoming, start)

	// The audio: one packet per segment, sequence 2 rising, the LAST one NEGATED.
	var audioStart time.Time
	if opts.Paced {
		audioStart = time.Now()
	}
	seq := firstAudioSequence
	for sent, index := 0, 0; sent < len(samples); sent, index = sent+segBytes, index+1 {
		end := sent + segBytes
		last := end >= len(samples)
		if end > len(samples) {
			end = len(samples)
		}
		if last {
			seq = -seq
		}
		if opts.Paced && !last {
			slot := audioStart.Add(time.Duration(index) * time.Duration(segMillis) * time.Millisecond)
			if wait := time.Until(slot); wait > 0 {
				if err := sleepCtx(ctx, wait); err != nil {
					return nil, &APIError{Op: opAudio, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
				}
			}
		}
		pkt, err := audioFrame(seq, samples[sent:end])
		if err != nil {
			return nil, &APIError{Op: opAudio, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
		}
		if err := conn.WriteBinary(pkt); err != nil {
			return nil, &APIError{Op: opAudio, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
		}
		res.Packets++
		res.absorbReady(incoming, start)
		if last {
			break
		}
		seq++
	}

	// The final wait: read until the last package, the peer closes, or the
	// budget runs out.
	deadline := time.Now().Add(finalBudget)
	quiet := 0
	for !res.last && quiet < drainQuietReads && time.Now().Before(deadline) {
		select {
		case msg, ok := <-incoming:
			if !ok {
				// The peer is gone; nothing more will arrive. Only a read
				// ERROR is a failure here — a normal close after the audio is
				// a peer that has said everything it means to.
				select {
				case err := <-readErr:
					return res, &APIError{Op: opRead, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
				default:
				}
				return res, res.finishWithPeer(reqID, peerGone)
			}
			res.apply(msg, start)
			quiet = 0
		case err := <-readErr:
			return res, &APIError{Op: opRead, Message: err.Error(), ReqID: reqID, LogID: res.LogID, Err: err}
		case <-time.After(drainReadBudget):
			quiet++
		case <-ctx.Done():
			return res, &APIError{Op: opRead, Message: ctx.Err().Error(), ReqID: reqID, LogID: res.LogID, Err: ctx.Err()}
		}
	}
	return res, res.finishWithPeer(reqID, peerGone)
}

// finishWithPeer folds whatever goodbye the reader recorded into the result
// before the verdict is built, so the report can say HOW the stream ended
// (a normal close, code 1000, a broken pipe) instead of only that it did.
func (r *Result) finishWithPeer(reqID string, peerGone <-chan string) error {
	select {
	case reason := <-peerGone:
		r.closeReason = reason
	default:
	}
	return r.finish(reqID)
}

// sleepCtx waits for d, or returns early when the context is done — the ONE
// place the two clocks (the audio's timeline and the caller's) meet.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// uidOrDefault returns the caller's uid, or the package default.
func uidOrDefault(uid string) string {
	if v := strings.TrimSpace(uid); v != "" {
		return v
	}
	return defaultUID
}

// newRequestID builds the caller's request id. It is a random 16-byte hex
// string in UUID shape, generated from crypto/rand: the vendor's support asks
// for it beside the log id, so it must be unique per request and must not
// identify the machine.
func newRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// absorbReady folds every message the reader has ALREADY delivered, without
// waiting for more. It is what keeps the response channel from filling while
// the audio is written: the writer's job is to move audio, and a message that
// has already arrived costs nothing to read.
func (r *Result) absorbReady(incoming <-chan []byte, start time.Time) {
	for {
		select {
		case msg, ok := <-incoming:
			if !ok {
				return
			}
			r.apply(msg, start)
		default:
			return
		}
	}
}

// apply decodes one server message and folds it in.
func (r *Result) apply(msg []byte, start time.Time) {
	r.fold(observeResponse(msg), start)
}

// fold records ONE decoded server message. It is separate from apply so the
// LIVE arm (session.go) folds through the SAME code and cannot come to a
// different account of what arrived (Rule 13/14).
func (r *Result) fold(o responseObservation, start time.Time) {
	r.frames++
	r.lastMsgType = o.msgType
	r.lastDecoded = o.decoded
	if o.rawPayload != "" {
		r.rawPayload = o.rawPayload
	}
	if o.code != 0 {
		r.Codes = append(r.Codes, o.code)
	}
	if o.last {
		r.last = true
	}
	if o.errMsg != "" {
		r.errMsg = o.errMsg
	}
	if o.text != "" {
		if r.FirstTextAt == 0 {
			r.FirstTextAt = time.Since(start)
		}
		r.Text = o.text
	}
}

// responseObservation is ONE server message decoded to the depth the failure
// path needs: WHICH envelope arrived (msgType), whether its payload was readable
// JSON at all (decoded), and the four payload facts. The distinction matters
// because an empty transcript has several causes and the message a run prints
// must name the one that happened (measured 2026-11-15 on the real service: a
// final package carrying `"text":""` and no code, versus a socket that produced
// no frame at all, reported the SAME sentence before this type existed).
type responseObservation struct {
	// msgType is byte 1's high nibble: 0x9 full response, 0xf server error.
	msgType byte
	// last is the envelope's last-packet flag.
	last bool
	// decoded reports that the payload reached and survived JSON decoding.
	decoded bool
	// code is the application code, from the error frame's first field or the
	// payload's own `code`; 0 when neither carried one.
	code int
	// text is the service's LATEST FULL TEXT.
	text string
	// errMsg is the service's own wording: its `error` field, verbatim.
	errMsg string
	// rawPayload is the payload of a frame that did NOT decode as JSON, quoted
	// and truncated. It is kept OUT of errMsg on purpose: errMsg is the
	// service's own sentence, and a run that confuses "the vendor said this"
	// with "we could not read what the vendor sent" misdirects the incident.
	rawPayload string
	// definite is the service's "this utterance is closed" verdict, false
	// whenever the response carried no segmentation.
	definite bool
}

// observeResponse decodes the v3 envelope. It never fails loudly: an
// unparseable frame yields a zero-valued observation carrying whatever envelope
// facts were legible, and the caller's own end-of-run report says what it saw.
func observeResponse(msg []byte) responseObservation {
	var o responseObservation
	if len(msg) < headerSize4Bytes {
		return o
	}
	headerBytes := int(msg[0]&0x0F) * headerSize4Bytes
	if headerBytes > len(msg) {
		return o
	}
	o.msgType = msg[1] >> 4
	flags := msg[1] & 0x0F
	serialization := msg[2] >> 4
	compression := msg[2] & 0x0F
	p := msg[headerBytes:]

	if flags&flagSequencePos != 0 && len(p) >= headerFieldBytes {
		p = p[headerFieldBytes:]
	}
	if flags&flagLastPacket != 0 {
		o.last = true
	}
	switch o.msgType {
	case msgTypeServerFullRespond:
		if len(p) >= headerFieldBytes {
			p = p[headerFieldBytes:]
		}
	case msgTypeServerError:
		if len(p) >= 2*headerFieldBytes {
			o.code = int(binary.BigEndian.Uint32(p[:headerFieldBytes]))
			p = p[2*headerFieldBytes:]
		}
	}
	if len(p) == 0 {
		return o
	}
	if compression == compressionGzip {
		// Trailing padding is tolerated: the gzip reader stops at its own
		// trailer, and the service pads its frames.
		if out, err := gunzipBytes(p); err == nil {
			p = out
		}
	}
	if serialization != serializationJSON {
		return o
	}
	var doc responseDoc
	if err := json.Unmarshal(p, &doc); err != nil {
		o.rawPayload = truncateForMessage(p)
		return o
	}
	o.decoded = true
	if o.code == 0 {
		o.code = doc.Code
	}
	// THE LAST UTTERANCE'S VERDICT DECIDES `definite`: a response that carries
	// several utterances is describing a growing text, and the flag the caller
	// reads is whether the text ENDED — which is the last one's.
	if n := len(doc.Result.Utterances); n > 0 {
		o.definite = doc.Result.Utterances[n-1].Definite
	}
	o.text = doc.Result.Text
	o.errMsg = doc.Error
	return o
}

// finish turns the accumulated state into the caller's answer, or into the
// error that explains why there is none (Rule 15: an empty transcript with no
// explanation is a defect, not a result).
func (r *Result) finish(reqID string) error {
	if strings.TrimSpace(r.Text) != "" {
		return nil
	}
	if r.errMsg != "" {
		if strings.Contains(strings.ToLower(r.errMsg), serviceNoSpeechMarker) {
			r.NoSpeech = true
			return &NoSpeechError{Message: r.errMsg, ReqID: reqID, LogID: r.LogID}
		}
		return &APIError{Op: opServer, Message: r.errMsg, ReqID: reqID, LogID: r.LogID}
	}
	// A FRAME THAT ARRIVED AND DID NOT DECODE is reported BEFORE the vendor's
	// wording branch: it is this client's own reading failure, not something
	// the service said, and the quoted payload is the only evidence of it.
	if r.frames > 0 && !r.lastDecoded && r.rawPayload != "" {
		return &APIError{Op: opServer, ReqID: reqID, LogID: r.LogID, Message: r.emptyTranscriptFinding()}
	}
	if len(r.Codes) > 0 {
		return &APIError{Op: opServer, Code: r.Codes[len(r.Codes)-1], ReqID: reqID, LogID: r.LogID,
			Message: "the service returned no transcript"}
	}
	// NO code and no wording. WHICH sentence is true depends on what actually
	// arrived, and that is the whole point of the four cases: an empty socket,
	// a final package with an empty transcript, and a stream that ended before
	// any conclusion are three different findings with three different owners
	// (account/network, the AUDIO, and the exchange). The pre-fix code reported
	// all of them with one sentence that stated none of it.
	//
	// THE TYPE, not only the sentence: a caller must be able to tell "this audio
	// produced nothing" from "the channel failed" without reading English, so the
	// finding travels as NoTranscriptError (see its comment).
	return &NoTranscriptError{Finding: r.emptyTranscriptFinding(), Level: r.level, ReqID: reqID, LogID: r.LogID}
}

// emptyTranscriptFinding is the ONE sentence an empty transcript without a code
// earns, chosen from what this run actually observed. Each branch names the fact
// that produced it plus the diagnostics that make it checkable — how many frames
// arrived, what the last one's message type was, how the peer left — so a reader
// no longer has to guess which of the four situations happened (Rule 15).
func (r *Result) emptyTranscriptFinding() string {
	sentence := r.emptyTranscriptSentence()
	parts := []string{sentence}
	if r.frames == 0 {
		parts = append(parts, "no server frame arrived before the stream ended")
	} else {
		parts = append(parts,
			fmt.Sprintf("%d server frame(s) arrived, the last of msgType 0x%x%s",
				r.frames, r.lastMsgType, finalPackageClause(r.last)))
	}
	if r.frames > 0 && !r.lastDecoded {
		parts = append(parts, "the last frame's payload did not decode as JSON: "+r.rawPayload)
	}
	if r.closeReason != "" {
		parts = append(parts, "the peer said: "+r.closeReason)
	}
	if r.Packets > 0 {
		parts = append(parts, fmt.Sprintf("%d audio packet(s) were sent over %d ms of audio", r.Packets, r.AudioMS))
	}
	// THE AUDIO ITSELF, MEASURED HERE (audio_level.go). The four states above say
	// WHAT the service did; this says whether there was anything in the audio to
	// recognise, which is the question an operator actually has. It is added, never
	// substituted: the frame count, the msgType and the peer's goodbye stay in the
	// sentence, so a channel problem is still readable as one.
	if r.level.Measured {
		parts = append(parts, r.level.Evidence())
	}
	return strings.Join(parts, "; ")
}

// emptyTranscriptSentence picks WHICH of the three no-code situations happened.
// It is a separate function so the choice is one expression a test can name,
// rather than a chain of conditions welded into a message builder.
func (r *Result) emptyTranscriptSentence() string {
	switch {
	case r.frames == 0:
		return noResponseSentence
	case r.last:
		return emptyFinalSentence
	default:
		return noConclusionSentence
	}
}

// finalPackageClause says, in the message, whether the last frame was the
// service's own end of the utterance.
func finalPackageClause(last bool) string {
	if last {
		return " (the service's final package)"
	}
	return " (no final package)"
}

// truncateForMessage keeps an unparseable payload to a length an error message
// can carry.
func truncateForMessage(b []byte) string {
	if len(b) <= errorBodyBytes {
		return string(b)
	}
	return string(b[:errorBodyBytes]) + "..."
}
