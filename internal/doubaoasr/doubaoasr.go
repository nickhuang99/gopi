// Package doubaoasr is the SECOND speech-recognition engine of `gopi
// voice_input`: 豆包 / Volcengine (ByteDance) cloud ASR, reached over plain
// net/http with the Go standard library alone.
//
// WHY ZERO THIRD-PARTY DEPENDENCIES (the package's central constraint): gopi
// ships as a single static binary with ONE third-party dependency
// (modernc.org/sqlite). The official Volcengine Go SDK would add four direct
// modules plus a transitive tree (~3.65 MB of binary) — and its own
// default is actively harmful: `protocol/config.go ResourceID()` returns
// volc.bigasr.sauc.duration, a resource this account is NOT granted, so a
// caller who lets the SDK pick its default gets an HTTP 403 handshake refusal
// (measured 2026-09-13, devdoc/doubao-asr-eval.md §SDK 默认值有害). This
// package therefore has NO config object with silent defaults: ResourceID is
// an explicit field of Options with a named default constant, and the whole
// client is JSON over https.
//
// THE CHANNEL THAT IS ACTUALLY OPEN. Two Doubao product lines were probed
// against this account (real calls, log ids in devdoc/doubao-asr-integration.md):
//
//   - 流式 SAUC (`volc.seedasr.sauc.duration`, the epic's first choice):
//     HTTP 403 "requested resource not granted" — NOT usable, so no streaming
//     client is shipped here (a client for a route that 403s is dead code).
//   - 录音文件识别 AUC (`volc.seedasr.auc`, the file/batch channel): granted
//     and proven — CER 0.69% on the 220 s evaluation clip, RTF 0.084–0.104.
//     This is the channel this package implements.
//
// The AUC channel is asynchronous by design: POST /submit hands over the
// audio (inline base64 or a URL) and returns a request id; POST /query is
// polled with that id until the status code says the transcript is ready.
// That is the "非实时 / 批量" speech shape — a recording, not a live stream.
package doubaoasr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ── Endpoints and resources (all named; Rule 1) ─────────────────────────
const (
	// DefaultSubmitURL / DefaultQueryURL are the OpenSpeech AUC endpoints.
	// They are now DERIVED from DefaultBaseURL (one root, two URLs) so that a
	// single configurable base can never make the submit and the query
	// disagree — a submit on one host polled on another's names a task id the
	// poller never saw. Options may still override either URL, and never
	// implicitly: a caller that wants a different region names it.
	DefaultSubmitURL = DefaultBaseURL + SubmitURLPath
	DefaultQueryURL  = DefaultBaseURL + QueryURLPath

	// DefaultBaseURL is the endpoint ROOT of the batch (AUC) channel — the
	// value the `doubao_base_url` setting and $GOPI_DOUBAO_BASE_URL carry, and
	// the single source the two URLs above derive from (Rule 13: the root is
	// defined once and referenced, never re-typed three times).
	DefaultBaseURL = "https://openspeech.bytedance.com/api/v3/auc/bigmodel"

	// SubmitURLPath / QueryURLPath are the two paths under the root. Named
	// (Rule 1) because SubmitURLFor / QueryURLFor append them and a test
	// asserts the derivation equals the literal endpoints above.
	SubmitURLPath = "/submit"
	QueryURLPath  = "/query"

	// ResourceSeedASRAUC20 is 豆包录音文件识别模型 2.0 (the DEFAULT here:
	// same measured quality as 1.0 in the evaluation, 0.8 元/小时 against
	// 2.3 元/小时 — see devdoc/doubao-asr-eval.md §定价).
	ResourceSeedASRAUC20 = "volc.seedasr.auc"
	// ResourceBigASRAUC10 is 大模型录音文件识别（标准版）, 1.0.
	ResourceBigASRAUC10 = "volc.bigasr.auc"
	// ResourceBigASRAUCTurbo is 极速版.
	ResourceBigASRAUCTurbo = "volc.bigasr.auc_turbo"
	// ResourceSeedASRStreaming is the 流式小时包 resource the epic asked for
	// first. Kept as a NAMED constant because it is the value this account is
	// refused and the one the devdoc's probe evidence names — a bare literal
	// in a comment would drift from the wire value.
	ResourceSeedASRStreaming = "volc.seedasr.sauc.duration"

	// ModelName is the only value the API documents for model_name.
	ModelName = "bigmodel"
)

// ── Wire header names (Rule 1b: naming a wire contract once) ────────────
const (
	headerAPIKey      = "X-Api-Key"
	headerResourceID  = "X-Api-Resource-Id"
	headerRequestID   = "X-Api-Request-Id"
	headerSequence    = "X-Api-Sequence"
	headerLogID       = "X-Tt-Logid"
	headerStatusCode  = "X-Api-Status-Code"
	headerAPIMessage  = "X-Api-Message"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
)

// ── X-Api-Status-Code values ────────────────────────────────────────────
const (
	// StatusOK is the only success code; every other value is a failure the
	// caller must be able to see (Rule 15 — no silent failure).
	StatusOK = "20000000"
	// StatusProcessing / StatusQueued mean "the job is still running" and are
	// the only two codes the query loop retries.
	StatusProcessing = "20000001"
	StatusQueued     = "20000002"
	// StatusNoSpeech is the service's verdict on audio that arrived intact and
	// carried no speech: it is a COMPLETED job with an empty transcript, not a
	// failed request. Measured on this account (2026-10-08, logid
	// 2026100819203030ED41BEECA127FF8C6E): X-Api-Status-Code 20000003,
	// X-Api-Message "Normal silence audio Handle response: no valid speech in
	// audio", body {"audio_info":{"duration":6000},"result":{"text":""}} — the
	// duration proves the 6 s window was RECEIVED, so the verdict is about the
	// audio, not about the upload.
	//
	// It is named here because a caller that has to tell "the window was quiet"
	// from "the engine is broken" cannot decide that from a message string
	// (Rule 1/13: the value has ONE definition, and this is it).
	StatusNoSpeech = "20000003"
)

// NoSpeech reports whether the service's verdict is the one above: the job
// finished and the audio carried no speech. It is a method on the typed error so
// a caller reaches the verdict THROUGH the wrapped chain (Rule 16) instead of
// matching on text, and it is false for every other code — including the
// transport failures that carry no code at all, which stay failures.
func (e *APIError) NoSpeech() bool {
	return e != nil && e.StatusCode == StatusNoSpeech
}

// ── Audio geometry defaults (the capture contract, not magic) ───────────
const (
	DefaultFormat  = "wav"
	DefaultCodec   = "raw"
	DefaultRate    = 16000
	DefaultChannel = 1
	DefaultBits    = 16

	// sequenceSingle marks the one-shot request sequence the AUC API expects
	// on both submit and query (-1 = "no client-side sequencing").
	sequenceSingle = -1
)

// ── Timeouts and bounds (Rule 1: no inline literals) ────────────────────
const (
	// DefaultHTTPTimeoutSec bounds ONE request (submit or query).
	DefaultHTTPTimeoutSec = 120
	// DefaultPollIntervalSec is the gap between two /query calls.
	DefaultPollIntervalSec = 2
	// DefaultPollTimeoutSec bounds the WHOLE poll loop.
	DefaultPollTimeoutSec = 600
	// maxBodyBytes caps a response body we will read.
	maxBodyBytes = 32 << 20
	// maxErrorBodyChars caps how much of a non-JSON error body reaches an
	// error message.
	maxErrorBodyChars = 400
)

// ── API-key location ────────────────────────────────────────────────────
const (
	// APIKeyDirName / APIKeyFileName locate the key file under $HOME. NEVER
	// logged, never echoed: the loader returns the key and nothing else does.
	APIKeyDirName  = ".doubao"
	APIKeyFileName = "apikey.txt"
)

// Error prefix constants — the message shapes callers and tests match on.
const (
	errPrefix         = "doubaoasr: "
	msgNoAPIKey       = "no API key"
	msgNoAudio        = "no audio: pass a file path or a URL"
	msgEmptyAudioFile = "empty audio file"
	msgStatusRefused  = "request refused"
)

// DefaultAPIKeyPath is $HOME/.doubao/apikey.txt — the path the operator was
// told to use, assembled (not spelled inline) so the file name has ONE
// definition.
func DefaultAPIKeyPath(home string) string {
	return filepath.Join(home, APIKeyDirName, APIKeyFileName)
}

// LoadAPIKey reads the key file and trims surrounding whitespace. An empty
// file is an error, not an empty key: a request with no key would be refused
// by the service with a message that hides the real cause (a missing file the
// operator can fix in one second).
func LoadAPIKey(path string) (string, error) {
	b, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("%s%s %q: %w", errPrefix, msgNoAPIKey, path, err)
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", fmt.Errorf("%s%s: %q is empty", errPrefix, msgNoAPIKey, path)
	}
	return key, nil
}

// APIError is the typed failure of a Doubao call. It carries the RAW service
// verdict — HTTP status, X-Api-Status-Code, X-Api-Message and the request's
// X-Tt-Logid — because those four are what the vendor's own support asks for,
// and a bare "request failed" (Rule 15's silent failure) would make every
// incident start from zero.
type APIError struct {
	// Op names the call ("submit" / "query").
	Op string
	// HTTPStatus is the transport-level status (0 when the request never
	// reached a response).
	HTTPStatus int
	// StatusCode is the X-Api-Status-Code header — the service's own verdict,
	// which can be a failure even on HTTP 200.
	StatusCode string
	// Message is X-Api-Message (or the first bytes of the body when the
	// service sent no header).
	Message string
	// LogID is X-Tt-Logid: the identifier the vendor traces a call by.
	LogID string
	// RequestID is the X-Api-Request-Id this client sent.
	RequestID string
	// Body is a truncated copy of the response body (never contains the API
	// key — the key travels only in a request header).
	Body string
	// Err is the underlying transport error, when there is one.
	Err error
}

// Error renders every field a support ticket needs, in one line.
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString(errPrefix)
	b.WriteString(e.Op)
	b.WriteString(" ")
	b.WriteString(msgStatusRefused)
	fmt.Fprintf(&b, ": HTTP %d status=%q", e.HTTPStatus, e.StatusCode)
	if e.Message != "" {
		fmt.Fprintf(&b, " message=%q", e.Message)
	}
	if e.LogID != "" {
		fmt.Fprintf(&b, " logid=%s", e.LogID)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " reqid=%s", e.RequestID)
	}
	if e.Body != "" {
		fmt.Fprintf(&b, " body=%s", e.Body)
	}
	if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return b.String()
}

// Unwrap exposes the transport error to errors.Is/errors.As (Rule 16: the
// chain is kept, never flattened into a string).
func (e *APIError) Unwrap() error { return e.Err }

// ErrEmptyAudio is reported for a request with no audio at all (an empty file
// or an empty URL) — the service would answer a generic bad-request, which
// hides a local mistake.
var ErrEmptyAudio = errors.New(errPrefix + msgNoAudio)

// Options is one transcription request. Every knob has a named default and
// nothing is inferred from the environment except the API key path, which the
// caller resolves (the CLI knows $HOME; this package does not read env).
type Options struct {
	// ResourceID is MANDATORY in spirit: the zero value is replaced by
	// DefaultResourceID in Normalize (see ResourceIDOrDefault). It is never
	// left to a service-side default — the SDK's wrong default is exactly the
	// defect this package exists to avoid.
	ResourceID string

	// AudioFile is a local file sent inline as base64 audio.data. Mutually
	// exclusive with AudioURL.
	AudioFile string
	// AudioURL is a publicly reachable URL (audio.url).
	AudioURL string

	// AudioGeometry describes the payload when it is not a WAV we can sniff.
	Format   string
	Codec    string
	Rate     int
	Channels int
	Bits     int

	// Feature switches (the AUC channel's own set — it has no seg_duration,
	// paced or enable_nonstream; those belong to 流式 SAUC, which this account
	// is not granted).
	EnableITN         bool
	EnablePUNC        bool
	EnableDDC         bool
	EnableSpeakerInfo bool
	EnableLID         bool
	ShowUtterances    bool

	// Hotwords are pushed through request.corpus.context as the DOUBLE-ENCODED
	// JSON string the API demands: corpus.context is a string whose content is
	// `{"hotwords":[{"word":"…"}]}`. Passing an object instead is refused with
	// "add unmapped key hotwords to params store" (measured, see
	// devdoc/doubao-asr-eval.md §热词).
	Hotwords []string
	// BoostingTableName is 自学习平台热词表 (corpus.boosting_table_name).
	BoostingTableName string

	// APIKey is the credential. Empty is an error — see LoadAPIKey.
	APIKey string

	// Endpoint / timing overrides (zero = the named default above).
	SubmitURL       string
	QueryURL        string
	HTTPTimeoutSec  int
	PollIntervalSec int
	PollTimeoutSec  int
	// UID identifies the caller in the service's own logs.
	UID string
}

// Result is one finished transcription plus the evidence of the call that
// produced it (log ids and the status codes observed while polling).
type Result struct {
	Text string
	// Utterances are the per-sentence slices (with speaker id when
	// EnableSpeakerInfo and ShowUtterances were set).
	Utterances []Utterance
	// DurationMS is the audio duration the SERVICE measured (0 when it
	// reported none).
	DurationMS int

	ResourceID  string
	RequestID   string
	SubmitLogID string
	QueryLogID  string
	// StatusCodes is every X-Api-Status-Code seen, in order (submit first,
	// then each query) — the audit trail of what the service said.
	StatusCodes []string
	// Polls is how many /query calls it took.
	Polls int
	// SubmitMS / TotalMS are wall-clock costs (TotalMS covers submit+polling).
	SubmitMS int64
	TotalMS  int64
	// RTF is TotalMS / service-measured audio seconds (0 when unknown).
	RTF float64
}

// Utterance is one sentence of the transcript.
type Utterance struct {
	Definite  bool
	Text      string
	SpeakerID string
	StartMS   int
	EndMS     int
}

// SplitHotwords splits the comma-separated hotword form used by the CLI, the
// env var and settings.json into the list corpus.context carries. A blank
// entry never reaches the wire (the service would count it as a hotword).
func SplitHotwords(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, hotwordSeparator)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// hotwordSeparator is the one delimiter the operator-facing hotword form uses.
const hotwordSeparator = ","

// ResourceIDOrDefault returns the explicitly requested resource or the named
// default (2.0). Exported so the CLI can print the same value it will send.
func ResourceIDOrDefault(id string) string {
	if strings.TrimSpace(id) == "" {
		return ResourceSeedASRAUC20
	}
	return strings.TrimSpace(id)
}

func (o Options) submitURL() string { return stringOrDefault(o.SubmitURL, DefaultSubmitURL) }
func (o Options) queryURL() string  { return stringOrDefault(o.QueryURL, DefaultQueryURL) }

// SubmitURLFor / QueryURLFor derive ONE URL from a configurable endpoint ROOT —
// the operation behind the `doubao_base_url` setting (and its env channel),
// which is the voice-side counterpart of `vision_base_url`: one root for the
// vendor's host, with the channel's own path appended, so the two URLs a batch
// run needs can never be pointed at two different hosts.
//
// An empty (or blank) base is the published root, so a machine with nothing
// configured derives exactly DefaultSubmitURL / DefaultQueryURL (the test that
// pins this is TestSubmitURLForDerivesThePublishedEndpoints). A base that
// already carries the path is not special-cased: the setting documents a ROOT,
// and guessing which suffix an operator "meant" would be a second, invisible
// rule about their own string.
func SubmitURLFor(base string) string { return baseOrDefault(base) + SubmitURLPath }

// QueryURLFor is QueryURLPath's half of the same derivation.
func QueryURLFor(base string) string { return baseOrDefault(base) + QueryURLPath }

// baseOrDefault returns the endpoint root, or the published default when the
// configured value is blank (Rule 15a: "" means unset, never a URL of "").
func baseOrDefault(base string) string {
	return stringOrDefault(base, DefaultBaseURL)
}
func (o Options) uid() string         { return stringOrDefault(o.UID, defaultUID) }
func (o Options) httpTimeoutSec() int { return intOrDefault(o.HTTPTimeoutSec, DefaultHTTPTimeoutSec) }
func (o Options) pollIntervalSec() int {
	return intOrDefault(o.PollIntervalSec, DefaultPollIntervalSec)
}
func (o Options) pollTimeoutSec() int { return intOrDefault(o.PollTimeoutSec, DefaultPollTimeoutSec) }

// defaultUID tags this client in the service's logs.
const defaultUID = "gopi-voice-doubao"

func stringOrDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func intOrDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// ── Wire types ──────────────────────────────────────────────────────────

type audioMeta struct {
	URL     string `json:"url,omitempty"`
	Data    string `json:"data,omitempty"`
	Format  string `json:"format"`
	Codec   string `json:"codec"`
	Rate    int    `json:"rate"`
	Bits    int    `json:"bits"`
	Channel int    `json:"channel"`
}

type corpusMeta struct {
	BoostingTableName string `json:"boosting_table_name,omitempty"`
	Context           string `json:"context,omitempty"`
}

type requestMeta struct {
	ModelName         string     `json:"model_name"`
	EnableITN         bool       `json:"enable_itn"`
	EnablePUNC        bool       `json:"enable_punc"`
	EnableDDC         bool       `json:"enable_ddc"`
	EnableSpeakerInfo bool       `json:"enable_speaker_info"`
	EnableLID         bool       `json:"enable_lid"`
	ShowUtterances    bool       `json:"show_utterances"`
	Corpus            corpusMeta `json:"corpus,omitempty"`
}

type requestBody struct {
	User    map[string]string `json:"user"`
	Audio   audioMeta         `json:"audio"`
	Request requestMeta       `json:"request"`
}

type responseBody struct {
	Header struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		ReqID   string `json:"reqid"`
	} `json:"header"`
	Result *struct {
		Text       string `json:"text"`
		Utterances []struct {
			Definite bool   `json:"definite"`
			Text     string `json:"text"`
			Speaker  string `json:"speaker_id,omitempty"`
			Start    int    `json:"start_time"`
			End      int    `json:"end_time"`
		} `json:"utterances"`
	} `json:"result"`
	AudioInfo *struct {
		Duration int `json:"duration"`
	} `json:"audio_info"`
}

// buildContext renders corpus.context — a STRING holding a JSON document.
// It returns "" when there is nothing to send, so the field stays omitted.
func buildContext(hotwords []string) (string, error) {
	words := make([]map[string]string, 0, len(hotwords))
	for _, w := range hotwords {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		words = append(words, map[string]string{"word": w})
	}
	if len(words) == 0 {
		return "", nil
	}
	inner, err := json.Marshal(map[string]any{"hotwords": words})
	if err != nil {
		return "", fmt.Errorf("%smarshal hotwords: %w", errPrefix, err)
	}
	return string(inner), nil
}

// Transcribe runs one file/batch transcription end to end: submit, then poll
// until the service reports the result. The context bounds the WHOLE call
// (submit + every poll); cancellation returns promptly with the context's
// error, never a half-written result.
func Transcribe(ctx context.Context, opts Options) (*Result, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, fmt.Errorf("%s%s — set it from %s or pass -api-key-file", errPrefix, msgNoAPIKey, DefaultAPIKeyPath("$HOME"))
	}
	if strings.TrimSpace(opts.AudioFile) == "" && strings.TrimSpace(opts.AudioURL) == "" {
		return nil, ErrEmptyAudio
	}
	if strings.TrimSpace(opts.AudioFile) != "" && strings.TrimSpace(opts.AudioURL) != "" {
		return nil, fmt.Errorf("%s%s: pass either a file or a URL, not both", errPrefix, msgStatusRefused)
	}

	c := &client{opts: opts, resourceID: ResourceIDOrDefault(opts.ResourceID)}
	body, err := c.requestBody()
	if err != nil {
		return nil, err
	}
	return c.run(ctx, body)
}

// timeNow is a seam for tests (the package's only clock read lives here).
var timeNow = time.Now
