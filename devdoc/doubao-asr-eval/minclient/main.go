// Command minclient is a ZERO-DEPENDENCY client for the Volcengine (ByteDance)
// Doubao streaming ASR "SAUC" v3 WebSocket API.
//
// Why hand-rolled instead of the official Go SDK (sauc_go.zip): the official
// demo drags in gorilla/websocket + bytedance/sonic + BurntSushi/toml +
// google/uuid (+6 indirect CGO-free-but-heavy modules) merely to speak a
// 4-byte-header binary protocol over a WebSocket. gopi ships as a single
// static binary with one dependency (modernc.org/sqlite); this file proves the
// same API is reachable with the Go standard library alone: the WebSocket
// handshake and framing (RFC 6455) are ~150 lines, the rest is gzip +
// encoding/json, both stdlib.
//
// It is also the evaluation harness: it timestamps connection setup, first
// server text (首字返回), per-packet sends and the final response, so the
// realtime (seg_duration=200ms, paced) and file (unpaced / large segment)
// semantics can be reported separately.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ── Doubao SAUC endpoint + models (all overridable, never implicit) ──────
const (
	// EndpointBidirectional is the bidirectional streaming endpoint; the
	// client feeds audio continuously and the server returns partial text.
	EndpointBidirectional = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"
	// EndpointNostream is the one-shot file endpoint (send everything, get
	// one result) — the "file semantics" comparator.
	EndpointNostream = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream"

	// ResourceSeedASR20 is 豆包流式语音识别模型 2.0, hourly billing.
	ResourceSeedASR20 = "volc.seedasr.sauc.duration"
	// ResourceBigASR10 is 豆包流式语音识别模型 1.0, hourly billing.
	ResourceBigASR10 = "volc.bigasr.sauc.duration"

	// ModelName is the only value the docs allow for model_name.
	ModelName = "bigmodel"
)

// ── Authentication (two schemes, both explicit — never a default) ────────
//
// The SAUC v3 endpoint accepts two credential shapes and they are NOT
// interchangeable:
//
//   - AuthLegacy  — the old-console *application* credentials: the App ID
//     (app key) plus its Access Token, sent as two headers. This is the
//     account-scoped grant, and it is the one that answers 101 for
//     resource_id volc.seedasr.sauc.duration on this account.
//   - AuthAPIKey  — a single UUID API key sent as one header. That key
//     belongs to whatever application it was minted for; against this
//     resource it is answered with 403 "requested resource not granted",
//     which reads like a missing entitlement but is an auth-shape mismatch.
const (
	// AuthLegacy selects X-Api-App-Key + X-Api-Access-Key.
	AuthLegacy = "legacy"
	// AuthAPIKey selects the single X-Api-Key header.
	AuthAPIKey = "apikey"

	// HeaderAPIKey names the single-key header.
	HeaderAPIKey = "X-Api-Key"
	// HeaderAppKey names the application-id header of the legacy pair.
	HeaderAppKey = "X-Api-App-Key"
	// HeaderAccessKey names the access-token header of the legacy pair.
	HeaderAccessKey = "X-Api-Access-Key"
	// HeaderResourceID selects the product line (billing entitlement).
	HeaderResourceID = "X-Api-Resource-Id"
	// HeaderRequestID carries the caller's correlation id.
	HeaderRequestID = "X-Api-Request-Id"
)

// redact renders a credential as a length + leading 2 characters, never whole.
func redact(s string) string {
	if s == "" {
		return "(unset)"
	}
	n := 2
	if len(s) < n {
		n = len(s)
	}
	return fmt.Sprintf("%s…(%d chars)", s[:n], len(s))
}

// ── Credential file keys (the ~/.doubao/v2_voice.env shape) ─────────────
const (
	envKeyAppID     = "APPID"
	envKeyToken     = "TOKEN"
	envKeyAccessTok = "ACCESS_TOKEN"
	credFileSep     = "="
	credComment     = "#"
)

// credentials is the legacy-console pair: the App ID and its Access Token.
// Both are held only in process memory; neither is ever written to a file,
// a log line or the evidence output (Rule 35 / the epic's no-secret clause).
type credentials struct {
	appID       string
	accessToken string
}

// Complete reports whether the legacy pair is usable.
func (c credentials) Complete() bool {
	return c.appID != "" && c.accessToken != ""
}

// authHeaders returns the ordered HTTP headers this credential shape emits.
// It is the single construction site the regression test drives, so the
// "legacy sends two headers, apikey sends one" contract has exactly one
// implementation.
func authHeaders(mode string, key string, creds credentials) [][2]string {
	switch mode {
	case AuthLegacy:
		return [][2]string{
			{HeaderAppKey, creds.appID},
			{HeaderAccessKey, creds.accessToken},
		}
	default: // AuthAPIKey
		return [][2]string{{HeaderAPIKey, key}}
	}
}

// parseCredFile reads KEY=VALUE lines (the shape of ~/.doubao/v2_voice.env):
// blank lines and # comments are skipped, whitespace around key/value is
// trimmed. TOKEN is the Access Token; ACCESS_TOKEN is accepted as an alias
// because the header it feeds is literally named Access-Key.
func parseCredFile(b []byte) (credentials, error) {
	var c credentials
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, credComment) {
			continue
		}
		i := strings.Index(line, credFileSep)
		if i < 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		switch k {
		case envKeyAppID:
			c.appID = v
		case envKeyToken, envKeyAccessTok:
			c.accessToken = v
		}
	}
	if !c.Complete() {
		return c, fmt.Errorf("credential file did not yield both %s and %s", envKeyAppID, envKeyToken)
	}
	return c, nil
}

func readCredFile(path string) (credentials, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return credentials{}, err
	}
	return parseCredFile(b)
}

// ── Protocol constants (SAUC v3 binary framing) ─────────────────────────
const (
	protocolVersion   = 0b0001
	headerSize4Bytes  = 4
	headerSizeByte0   = 0x11 // version<<4 | header size
	msgTypeFullReq    = 0b0001
	msgTypeAudioOnly  = 0b0010
	msgTypeServerFull = 0b1001
	msgTypeServerErr  = 0b1111
	flagNoSeq         = 0b0000
	flagPosSeq        = 0b0001
	flagNegWithSeq    = 0b0011
	serializationJSON = 0b0001
	compressionGzip   = 0b0001
)

// ── WebSocket (RFC 6455) constants ──────────────────────────────────────
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
	finBit         = 0x80
	maskBit        = 0x80
	lenExtended16  = 126
	lenExtended64  = 127
	maskKeyLen     = 4
	wsGUID         = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsAcceptMaxLen = 4096
	// maxFramePayload bounds an inbound frame so a malformed server cannot
	// make us allocate unbounded memory.
	maxFramePayload = 64 << 20
)

// ── Timing / I/O defaults ───────────────────────────────────────────────
const (
	defaultSegDurationMS = 200
	defaultDialTimeoutS  = 20
	defaultIdleTimeoutS  = 180
	maxLogBytes          = 8 << 20
	// incomingQueueLen bounds buffered server messages.
	incomingQueueLen = 4096
)

// ── SAUC request JSON ───────────────────────────────────────────────────
type audioMeta struct {
	Format  string `json:"format"`
	Codec   string `json:"codec"`
	Rate    int    `json:"rate"`
	Bits    int    `json:"bits"`
	Channel int    `json:"channel"`
}

type userMeta struct {
	UID string `json:"uid"`
}

type corpusMeta struct {
	BoostingTableName string   `json:"boosting_table_name,omitempty"`
	Context           string   `json:"context,omitempty"`
	Hotwords          []string `json:"-"`
}

type requestMeta struct {
	ModelName         string     `json:"model_name"`
	EnableITN         bool       `json:"enable_itn"`
	EnablePUNC        bool       `json:"enable_punc"`
	EnableDDC         bool       `json:"enable_ddc"`
	EnableSpeakerInfo bool       `json:"enable_speaker_info"`
	EnableLID         bool       `json:"enable_lid"`
	ShowUtterances    bool       `json:"show_utterances"`
	EnableNonstream   bool       `json:"enable_nonstream"`
	ResultType        string     `json:"result_type,omitempty"`
	Corpus            corpusMeta `json:"corpus,omitempty"`
}

type asrRequest struct {
	User    userMeta    `json:"user"`
	Audio   audioMeta   `json:"audio"`
	Request requestMeta `json:"request"`
}

// ── Server response ─────────────────────────────────────────────────────
type asrResponse struct {
	Code            int             `json:"code"`
	Event           int             `json:"event"`
	IsLastPackage   bool            `json:"is_last_package"`
	PayloadSequence int32           `json:"payload_sequence"`
	PayloadSize     int             `json:"payload_size"`
	PayloadMsg      json.RawMessage `json:"payload_msg,omitempty"`
}

type resultPayload struct {
	AudioInfo struct {
		Duration int `json:"duration"`
	} `json:"audio_info"`
	Result struct {
		Text       string `json:"text"`
		Utterances []struct {
			Definite  bool   `json:"definite"`
			StartTime int    `json:"start_time"`
			EndTime   int    `json:"end_time"`
			Text      string `json:"text"`
			Speaker   string `json:"speaker_id,omitempty"`
			Words     []struct {
				StartTime int    `json:"start_time"`
				EndTime   int    `json:"end_time"`
				Text      string `json:"text"`
			} `json:"words"`
		} `json:"utterances"`
	} `json:"result"`
	Error string `json:"error,omitempty"`
}

// ── Metrics / evidence ──────────────────────────────────────────────────
type metrics struct {
	ResourceID       string   `json:"resource_id"`
	Endpoint         string   `json:"endpoint"`
	SegDurationMS    int      `json:"seg_duration_ms"`
	Paced            bool     `json:"paced"`
	AudioSeconds     float64  `json:"audio_seconds"`
	WavBytes         int      `json:"wav_bytes"`
	PacketsSent      int      `json:"packets_sent"`
	DialMS           int64    `json:"dial_ms"`
	TLSMS            int64    `json:"tls_ms"`
	UpgradeMS        int64    `json:"upgrade_ms"`
	FirstCharMS      int64    `json:"first_char_ms"`
	LastSendMS       int64    `json:"last_send_ms"`
	FinalRespMS      int64    `json:"final_resp_ms"`
	RTF              float64  `json:"rtf"`
	SendSpeedXReel   float64  `json:"send_speed_x_realtime"`
	LogID            string   `json:"log_id"`
	Codes            []int    `json:"codes"`
	Errors           []string `json:"errors"`
	FinalTextLen     int      `json:"final_text_len"`
	UtteranceCount   int      `json:"utterance_count"`
	ServerDurationMS int      `json:"server_audio_duration_ms"`
}

func main() {
	var (
		audioPath  = flag.String("file", "", "audio file (wav 16k mono; other formats need ffmpeg pre-conversion)")
		endpoint   = flag.String("url", EndpointBidirectional, "websocket endpoint")
		resourceID = flag.String("resource_id", ResourceSeedASR20, HeaderResourceID+": "+ResourceSeedASR20+" (2.0) or "+ResourceBigASR10+" (1.0)")
		apiKeyPath = flag.String("api_key_file", os.Getenv("HOME")+"/.doubao/apikey.txt", "file holding the single API key, used by -auth apikey (never logged)")
		authMode   = flag.String("auth", AuthLegacy, "auth scheme: "+AuthLegacy+" (X-Api-App-Key + X-Api-Access-Key) or "+AuthAPIKey+" (single "+HeaderAPIKey+")")
		appKey     = flag.String("app_key", "", "legacy auth: App ID for "+HeaderAppKey+" (falls back to the credential file)")
		accessKey  = flag.String("access_key", "", "legacy auth: Access Token for "+HeaderAccessKey+" (falls back to the credential file)")
		credPath   = flag.String("cred_file", os.Getenv("HOME")+"/.doubao/v2_voice.env", "legacy auth: KEY=VALUE file supplying "+envKeyAppID+"/"+envKeyToken+" (never logged)")
		segMS      = flag.Int("seg_duration", defaultSegDurationMS, "audio milliseconds per packet")
		paced      = flag.Bool("paced", true, "pace packets in real time (true = realtime semantics, false = file/turbo semantics)")
		outDir     = flag.String("outdir", ".", "directory for transcript / metrics / raw evidence")
		tag        = flag.String("tag", "run", "label used in output file names")
		itn        = flag.Bool("itn", true, "enable_itn")
		punc       = flag.Bool("punc", true, "enable_punc")
		ddc        = flag.Bool("ddc", false, "enable_ddc (semantic smoothing)")
		speaker    = flag.Bool("speaker", false, "enable_speaker_info (needs show_utterances)")
		lid        = flag.Bool("lid", false, "enable_lid")
		nonstream  = flag.Bool("enable_nonstream", false, "enable_nonstream second pass")
		hotwords   = flag.String("hotwords", "", "comma-separated hotwords -> corpus.context JSON")
		boostTable = flag.String("boosting_table", "", "boosting_table_name")
		verbose    = flag.Bool("v", false, "log every server message to stderr")
	)
	flag.Parse()

	if *audioPath == "" {
		fatalf("-file is required")
	}
	if *authMode != AuthLegacy && *authMode != AuthAPIKey {
		fatalf("-auth %q is not one of {%s, %s}", *authMode, AuthLegacy, AuthAPIKey)
	}
	creds, key, err := resolveCredentials(*authMode, *appKey, *accessKey, *credPath, *apiKeyPath)
	if err != nil {
		fatalf("credentials: %v", err)
	}
	fmt.Fprint(os.Stderr, authAnnounce(*authMode, key, creds, *apiKeyPath, *credPath))

	wav, err := os.ReadFile(*audioPath)
	if err != nil {
		fatalf("read audio: %v", err)
	}
	pcm, sampleRate, channels, bits, err := splitWAV(wav)
	if err != nil {
		fatalf("parse wav: %v", err)
	}
	bytesPerSample := bits / 8
	bytesPerSecond := sampleRate * channels * bytesPerSample
	if bytesPerSecond <= 0 {
		fatalf("bad wav geometry: rate=%d ch=%d bits=%d", sampleRate, channels, bits)
	}
	audioSeconds := float64(len(pcm)) / float64(bytesPerSecond)

	req := asrRequest{
		User:  userMeta{UID: "gopi-doubao-eval"},
		Audio: audioMeta{Format: "pcm", Codec: "raw", Rate: sampleRate, Bits: bits, Channel: channels},
		Request: requestMeta{
			ModelName:         ModelName,
			EnableITN:         *itn,
			EnablePUNC:        *punc,
			EnableDDC:         *ddc,
			EnableSpeakerInfo: *speaker,
			EnableLID:         *lid,
			ShowUtterances:    true,
			EnableNonstream:   *nonstream,
			Corpus: corpusMeta{
				BoostingTableName: *boostTable,
				Context:           buildContext(*hotwords),
			},
		},
	}

	m := &metrics{
		ResourceID:    *resourceID,
		Endpoint:      *endpoint,
		SegDurationMS: *segMS,
		Paced:         *paced,
		AudioSeconds:  audioSeconds,
		WavBytes:      len(wav),
	}

	conn, err := dialSAUC(*endpoint, *authMode, key, creds, *resourceID, m)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer conn.Close()

	segBytes := bytesPerSecond * *segMS / 1000
	if segBytes <= 0 {
		fatalf("seg_duration %dms is below one sample frame", *segMS)
	}

	var rawAll bytes.Buffer
	start := time.Now()

	if err := conn.sendFullRequest(req); err != nil {
		fatalf("send full request: %v", err)
	}
	if err := conn.readOne(m, &rawAll, start, *verbose); err != nil {
		fatalf("read handshake ack: %v", err)
	}

	sendErr := sendAudio(conn, pcm, segBytes, *segMS, *paced, m, start, &rawAll, *verbose)
	if sendErr != nil {
		m.Errors = append(m.Errors, "send: "+sendErr.Error())
	}
	m.LastSendMS = time.Since(start).Milliseconds()

	full := finalize(conn, m, &rawAll, start, *verbose)
	m.FinalRespMS = time.Since(start).Milliseconds()
	m.RTF = float64(m.FinalRespMS) / 1000.0 / audioSeconds
	m.SendSpeedXReel = audioSeconds / (float64(m.LastSendMS) / 1000.0)

	writeEvidence(*outDir, *tag, full, m, rawAll.Bytes())
	if full == "" {
		fmt.Fprintln(os.Stderr, "WARNING: empty transcript")
		os.Exit(2)
	}
}

// ── audio helpers ───────────────────────────────────────────────────────

// splitWAV returns the PCM payload and geometry of a canonical RIFF/WAVE file.
func splitWAV(b []byte) (pcm []byte, rate, channels, bits int, err error) {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, 0, 0, fmt.Errorf("not a RIFF/WAVE file")
	}
	off := 12
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := b[off+8:]
		if size > len(body) {
			size = len(body)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, 0, 0, fmt.Errorf("short fmt chunk")
			}
			channels = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits = int(binary.LittleEndian.Uint16(body[14:16]))
		case "data":
			return body[:size], rate, channels, bits, nil
		}
		off += 8 + size + (size & 1)
	}
	return nil, 0, 0, 0, fmt.Errorf("no data chunk")
}

func buildContext(hotwords string) string {
	if hotwords == "" {
		return ""
	}
	words := []map[string]string{}
	for _, w := range bytes.Split([]byte(hotwords), []byte(",")) {
		if len(w) == 0 {
			continue
		}
		words = append(words, map[string]string{"word": string(w)})
	}
	ctx, err := json.Marshal(map[string]any{"hotwords": words})
	if err != nil {
		return ""
	}
	return string(ctx)
}

func readAPIKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(bytes.TrimSpace(b))
	if s == "" {
		return "", fmt.Errorf("empty key file")
	}
	return s, nil
}

// resolveCredentials materialises the credential shape the selected mode
// needs: legacy returns the App ID + Access Token pair (flags win over the
// credential file) and leaves the single key empty; apikey returns the UUID
// key and leaves the pair empty. Reading only the source the mode needs keeps
// a legacy run from failing on an unrelated missing key file (and vice versa).
func resolveCredentials(mode, appKey, accessKey, credPath, keyPath string) (credentials, string, error) {
	if mode == AuthAPIKey {
		k, err := readAPIKey(keyPath)
		if err != nil {
			return credentials{}, "", fmt.Errorf("api key: %w", err)
		}
		return credentials{}, k, nil
	}
	creds := credentials{appID: appKey, accessToken: accessKey}
	if !creds.Complete() {
		fromFile, err := readCredFile(credPath)
		if err != nil {
			return credentials{}, "", fmt.Errorf("legacy auth needs -app_key/-access_key or a readable credential file: %w", err)
		}
		if creds.appID == "" {
			creds.appID = fromFile.appID
		}
		if creds.accessToken == "" {
			creds.accessToken = fromFile.accessToken
		}
	}
	if !creds.Complete() {
		return credentials{}, "", fmt.Errorf("legacy auth requires both %s and %s", HeaderAppKey, HeaderAccessKey)
	}
	return creds, "", nil
}

// ── send loop ───────────────────────────────────────────────────────────

func sendAudio(c *saucConn, pcm []byte, segBytes, segMS int, paced bool, m *metrics, start time.Time, rawOut *bytes.Buffer, verbose bool) error {
	var ticker *time.Ticker
	if paced {
		ticker = time.NewTicker(time.Duration(segMS) * time.Millisecond)
		defer ticker.Stop()
	}
	// Sequence numbering starts at 2: the full client request already consumed
	// sequence 1 (the server auto-assigns it), and a packet that repeats a used
	// sequence is rejected with "autoAssignedSequence (N) mismatch sequence in
	// request (M)". The official demo gets this right only implicitly, by
	// pre-incrementing its counter before the audio loop.
	seq := 2
	for off := 0; off < len(pcm); off += segBytes {
		end := off + segBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		if paced {
			<-ticker.C
		}
		last := end >= len(pcm)
		s := seq
		if last {
			// Negative sequence on the final audio packet marks end-of-stream
			// (mirrors the official demo's seq negation).
			s = -seq
		}
		if err := c.sendAudioPacket(s, pcm[off:end]); err != nil {
			return err
		}
		m.PacketsSent++
		seq++
		// Drain whatever the server has produced so far (non-blocking-ish:
		// only reads what is already buffered) so partial text is captured.
		// The read side runs in a goroutine, so this only collects results.
		drain(c, m, start, rawOut, verbose)
	}
	return nil
}

// drain collects server messages the reader goroutine has already pushed.
func drain(c *saucConn, m *metrics, start time.Time, rawOut *bytes.Buffer, verbose bool) {
	for {
		select {
		case msg, ok := <-c.incoming:
			if !ok {
				return
			}
			c.apply(msg, m, rawOut, start, verbose)
		default:
			return
		}
	}
}

// finalize reads until the server's last package (or the socket closes) and
// returns the concatenated final transcript.
func finalize(c *saucConn, m *metrics, rawAll *bytes.Buffer, start time.Time, verbose bool) string {
	deadline := time.Now().Add(defaultIdleTimeoutS * time.Second)
	for !c.lastSeen() && time.Now().Before(deadline) {
		msg, ok := c.next()
		if !ok {
			break
		}
		c.apply(msg, m, rawAll, start, verbose)
	}
	return c.transcript()
}

// ── WebSocket connection ────────────────────────────────────────────────

type saucConn struct {
	conn  net.Conn
	r     *bufio.Reader
	logID string
	text  bytes.Buffer
	last  bool
	// incoming carries raw server messages from the reader goroutine, which
	// drains the socket continuously so the send loop never blocks on I/O.
	incoming chan []byte
	readErr  error
}

func (c *saucConn) Close() error       { return c.conn.Close() }
func (c *saucConn) transcript() string { return c.text.String() }
func (c *saucConn) lastSeen() bool     { return c.last }
func (c *saucConn) buffered() bool     { return c.r.Buffered() > 0 }

// authAnnounce renders the one-line stderr banner naming the scheme, the
// header names it uses and a REDACTED view of the credential — enough to see
// which shape went out, never enough to reconstruct a secret.
func authAnnounce(mode, key string, creds credentials, keyPath, credPath string) string {
	if mode == AuthAPIKey {
		return fmt.Sprintf("[auth] %s: %s=%s (from %s)\n", AuthAPIKey, HeaderAPIKey, redact(key), keyPath)
	}
	return fmt.Sprintf("[auth] %s: %s=%s %s=%s (from %s)\n",
		AuthLegacy, HeaderAppKey, redact(creds.appID), HeaderAccessKey, redact(creds.accessToken), credPath)
}

// buildHandshake renders the RFC 6455 upgrade request. Kept separate from the
// dialer so the credential headers are inspectable in a test without a socket:
// which headers a scheme emits is the contract the legacy-auth change is about.
func buildHandshake(path, host, wsKey, reqID, resourceID, authMode, key string, creds credentials) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&b, "Host: %s\r\n", host)
	b.WriteString("Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(&b, "Sec-WebSocket-Key: %s\r\n", wsKey)
	b.WriteString("Sec-WebSocket-Version: 13\r\n")
	// Auth + resource selection, both explicit — never left to a default.
	// The header SET comes from authHeaders so the two schemes stay in one
	// place; key material is never logged.
	for _, h := range authHeaders(authMode, key, creds) {
		fmt.Fprintf(&b, "%s: %s\r\n", h[0], h[1])
	}
	fmt.Fprintf(&b, "%s: %s\r\n", HeaderResourceID, resourceID)
	fmt.Fprintf(&b, "%s: %s\r\n", HeaderRequestID, reqID)
	b.WriteString("\r\n")
	return b.Bytes()
}

func dialSAUC(rawURL, authMode, key string, creds credentials, resourceID string, m *metrics) (*saucConn, error) {
	host, path, useTLS, err := parseWSURL(rawURL)
	if err != nil {
		return nil, err
	}
	dialStart := time.Now()
	d := net.Dialer{Timeout: defaultDialTimeoutS * time.Second}
	tcp, err := d.Dial("tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return nil, err
	}
	m.DialMS = time.Since(dialStart).Milliseconds()

	if useTLS {
		tlsStart := time.Now()
		tc := tls.Client(tcp, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tc.Handshake(); err != nil {
			tcp.Close()
			return nil, fmt.Errorf("tls: %w", err)
		}
		m.TLSMS = time.Since(tlsStart).Milliseconds()
		tcp = tc
	}

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		tcp.Close()
		return nil, err
	}
	wsKey := base64.StdEncoding.EncodeToString(keyRaw)
	reqID := newRequestID()

	req := buildHandshake(path, host, wsKey, reqID, resourceID, authMode, key, creds)

	upStart := time.Now()
	if _, err := tcp.Write(req); err != nil {
		tcp.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(tcp, 1<<16)
	status, headers, err := readHTTPResponse(r)
	if err != nil {
		tcp.Close()
		return nil, err
	}
	m.UpgradeMS = time.Since(upStart).Milliseconds()
	// Record the handshake result explicitly: a 101 is the positive evidence
	// that the credential SHAPE was accepted (a rejected one is answered 401
	// or 403 before any frame is exchanged), so it belongs in the record
	// rather than being inferred from the absence of an error.
	fmt.Fprintf(os.Stderr, "[ws] upgrade HTTP %d logid=%s in %dms\n", status, headers["x-tt-logid"], m.UpgradeMS)
	if status != 101 {
		body := readErrorBody(r, headers)
		tcp.Close()
		if logid := headers["x-tt-logid"]; logid != "" {
			m.LogID = logid
		}
		return nil, fmt.Errorf("upgrade refused: HTTP %d logid=%s body=%s", status, headers["x-tt-logid"], body)
	}
	if got := headers["sec-websocket-accept"]; got != "" {
		want := acceptFor(wsKey)
		if got != want {
			tcp.Close()
			return nil, fmt.Errorf("bad Sec-WebSocket-Accept: got %q want %q", got, want)
		}
	}
	c := &saucConn{
		conn:     tcp,
		r:        r,
		logID:    headers["x-tt-logid"],
		incoming: make(chan []byte, incomingQueueLen),
	}
	m.LogID = c.logID
	go c.readLoop()
	return c, nil
}

// readLoop pumps every inbound WebSocket message into the incoming channel and
// closes it when the connection ends, so no reader ever blocks the sender.
func (c *saucConn) readLoop() {
	defer close(c.incoming)
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(defaultIdleTimeoutS * time.Second))
		msg, ended, err := c.readMessage()
		if len(msg) > 0 {
			c.incoming <- msg
		}
		if err != nil || ended {
			c.readErr = err
			return
		}
	}
}

// next blocks for the next server message (used for the handshake ack and the
// final drain).
func (c *saucConn) next() ([]byte, bool) {
	msg, ok := <-c.incoming
	return msg, ok
}

func parseWSURL(u string) (host, path string, useTLS bool, err error) {
	switch {
	case len(u) > 6 && u[:6] == "wss://":
		useTLS = true
		u = u[6:]
	case len(u) > 5 && u[:5] == "ws://":
		u = u[5:]
	default:
		return "", "", false, fmt.Errorf("unsupported url scheme: %q", u)
	}
	slash := bytes.IndexByte([]byte(u), '/')
	if slash < 0 {
		return u, "/", useTLS, nil
	}
	return u[:slash], u[slash:], useTLS, nil
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func acceptFor(key string) string {
	h := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h[:])
}

// readHTTPResponse reads an HTTP/1.1 response head from a bufio.Reader,
// leaving any trailing bytes (WebSocket frames) in the reader.
func readHTTPResponse(r *bufio.Reader) (int, map[string]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, nil, err
	}
	var proto string
	var status int
	if _, err := fmt.Sscanf(line, "%s %d", &proto, &status); err != nil {
		return 0, nil, fmt.Errorf("bad status line %q: %w", line, err)
	}
	headers := map[string]string{}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return 0, nil, err
		}
		l = trimCRLF(l)
		if l == "" {
			return status, headers, nil
		}
		i := indexByte(l, ':')
		if i <= 0 {
			continue
		}
		headers[lowerASCII(l[:i])] = trimSpaceASCII(l[i+1:])
	}
}

// readErrorBody drains a refused-handshake response body so the server's
// reason (e.g. an auth/permission message) is visible instead of a bare 403.
func readErrorBody(r *bufio.Reader, headers map[string]string) string {
	n := 0
	if v := headers["content-length"]; v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n == 0 || n > wsAcceptMaxLen {
		n = wsAcceptMaxLen
	}
	buf := make([]byte, n)
	got, _ := io.ReadFull(r, buf)
	return trimSpaceASCII(string(buf[:got]))
}

// ── frames ──────────────────────────────────────────────────────────────

func (c *saucConn) writeFrame(opcode byte, payload []byte) error {
	var hdr []byte
	hdr = append(hdr, finBit|opcode)
	n := len(payload)
	switch {
	case n < lenExtended16:
		hdr = append(hdr, maskBit|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, maskBit|lenExtended16, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, maskBit|lenExtended64)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(hdr, ext[:]...)
	}
	mask := make([]byte, maskKeyLen)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	hdr = append(hdr, mask...)
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%maskKeyLen]
	}
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

// readMessage reassembles one WebSocket message across continuation frames,
// answering pings. It returns the payload and whether the stream ended.
func (c *saucConn) readMessage() ([]byte, bool, error) {
	var out []byte
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, true, err
		}
		switch opcode {
		case opPing:
			_ = c.writeFrame(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			return out, true, nil
		case opContinuation, opText, opBinary:
			out = append(out, payload...)
			if fin {
				return out, false, nil
			}
		default:
			continue
		}
	}
}

func (c *saucConn) readFrame() (bool, byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.r, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin := h[0]&finBit != 0
	opcode := h[0] & 0x0F
	masked := h[1]&maskBit != 0
	n := int(h[1] & 0x7F)
	switch n {
	case lenExtended16:
		var ext [2]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(ext[:]))
	case lenExtended64:
		var ext [8]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = int(binary.BigEndian.Uint64(ext[:]))
	}
	if n < 0 || n > maxFramePayload {
		return false, 0, nil, fmt.Errorf("frame payload %d out of range", n)
	}
	var mask []byte
	if masked {
		mask = make([]byte, maskKeyLen)
		if _, err := io.ReadFull(c.r, mask); err != nil {
			return false, 0, nil, err
		}
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return false, 0, nil, err
	}
	for i := 0; i < n && masked; i++ {
		payload[i] ^= mask[i%maskKeyLen]
	}
	return fin, opcode, payload, nil
}

// ── SAUC messages ───────────────────────────────────────────────────────

func saucHeader(msgType, flags byte) []byte {
	return []byte{headerSizeByte0, msgType<<4 | flags, serializationJSON<<4 | compressionGzip, 0x00}
}

func (c *saucConn) sendFullRequest(req asrRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	gz, err := gzipBytes(body)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.Write(saucHeader(msgTypeFullReq, flagPosSeq))
	var seq [4]byte
	binary.BigEndian.PutUint32(seq[:], 1)
	buf.Write(seq[:])
	var sz [4]byte
	binary.BigEndian.PutUint32(sz[:], uint32(len(gz)))
	buf.Write(sz[:])
	buf.Write(gz)
	return c.writeFrame(opBinary, buf.Bytes())
}

func (c *saucConn) sendAudioPacket(seq int, pcm []byte) error {
	gz, err := gzipBytes(pcm)
	if err != nil {
		return err
	}
	flags := byte(flagPosSeq)
	if seq < 0 {
		flags = flagNegWithSeq
	}
	var buf bytes.Buffer
	buf.Write(saucHeader(msgTypeAudioOnly, flags))
	var s [4]byte
	binary.BigEndian.PutUint32(s[:], uint32(int32(seq)))
	buf.Write(s[:])
	var sz [4]byte
	binary.BigEndian.PutUint32(sz[:], uint32(len(gz)))
	buf.Write(sz[:])
	buf.Write(gz)
	return c.writeFrame(opBinary, buf.Bytes())
}

func (c *saucConn) readOne(m *metrics, rawOut *bytes.Buffer, start time.Time, verbose bool) error {
	msg, ok := c.next()
	if !ok {
		return io.EOF
	}
	c.apply(msg, m, rawOut, start, verbose)
	return nil
}

// apply decodes one server message and folds it into the accumulated state.
func (c *saucConn) apply(msg []byte, m *metrics, rawOut *bytes.Buffer, start time.Time, verbose bool) {
	if rawOut != nil {
		fmt.Fprintf(rawOut, "# t=+%dms len=%d\n%s\n", time.Since(start).Milliseconds(), len(msg), hexPreview(msg))
	}
	code, last, msgText := parseSaucResponse(msg, verbose)
	if code != 0 {
		m.Codes = append(m.Codes, code)
	}
	if len(msgText) > 0 {
		if m.FirstCharMS == 0 {
			m.FirstCharMS = time.Since(start).Milliseconds()
		}
		c.text.Reset()
		c.text.WriteString(msgText)
	}
	if last {
		c.last = true
	}
}

// parseSaucResponse decodes the SAUC binary envelope and returns the response
// code, whether it is the last package, and the accumulated text.
func parseSaucResponse(msg []byte, verbose bool) (code int, last bool, text string) {
	if len(msg) < headerSize4Bytes {
		return 0, false, ""
	}
	hSize := int(msg[0]&0x0F) * headerSize4Bytes
	if hSize > len(msg) {
		return 0, false, ""
	}
	msgType := msg[1] >> 4
	flags := msg[1] & 0x0F
	serialization := msg[2] >> 4
	compression := msg[2] & 0x0F
	p := msg[hSize:]

	if flags&0x01 != 0 && len(p) >= 4 {
		p = p[4:]
	}
	if flags&0x02 != 0 {
		last = true
	}
	if flags&0x04 != 0 && len(p) >= 4 {
		event := int(binary.BigEndian.Uint32(p[:4]))
		p = p[4:]
		_ = event
	}
	switch msgType {
	case msgTypeServerFull:
		if len(p) >= 4 {
			p = p[4:]
		}
	case msgTypeServerErr:
		if len(p) >= 8 {
			code = int(binary.BigEndian.Uint32(p[:4]))
			p = p[8:]
		}
	}
	if len(p) == 0 {
		return code, last, ""
	}
	if compression == compressionGzip {
		// Padding bytes are tolerated: gzip readers stop at the trailer.
		if out, err := gunzipBytes(p); err == nil {
			p = out
		}
	}
	if serialization != serializationJSON {
		return code, last, ""
	}
	var rp resultPayload
	if err := json.Unmarshal(p, &rp); err != nil {
		return code, last, ""
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[resp] code=%d last=%v text=%q err=%q\n", code, last, rp.Result.Text, rp.Error)
	}
	if rp.Error != "" {
		fmt.Fprintf(os.Stderr, "[server error] %s\n", rp.Error)
	}
	return code, last, rp.Result.Text
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// hexPreview renders a response envelope's header bytes plus the leading
// printable payload so the raw evidence file is human-auditable.
func hexPreview(b []byte) string {
	n := 32
	if len(b) < n {
		n = len(b)
	}
	var sb bytes.Buffer
	fmt.Fprintf(&sb, "hdr=% x", b[:n])
	if len(b) > n {
		body := b[n:]
		if out, err := gunzipBytes(body); err == nil {
			body = out
		}
		if len(body) > 400 {
			body = body[:400]
		}
		fmt.Fprintf(&sb, " body=%s", string(body))
	}
	return sb.String()
}

// ── evidence output ─────────────────────────────────────────────────────

func writeEvidence(dir, tag, transcript string, m *metrics, raw []byte) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatalf("mkdir %s: %v", dir, err)
	}
	m.FinalTextLen = len([]rune(transcript))
	write(filepath.Join(dir, tag+".txt"), []byte(transcript))
	mj, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatalf("marshal metrics: %v", err)
	}
	write(filepath.Join(dir, tag+".metrics.json"), mj)
	if len(raw) > maxLogBytes {
		raw = raw[:maxLogBytes]
	}
	if len(raw) > 0 {
		write(filepath.Join(dir, tag+".raw.txt"), raw)
	}
	fmt.Printf("[%s] log_id=%s dial=%dms tls=%dms upgrade=%dms first_char=%dms last_send=%dms final=%dms rtf=%.3f packets=%d chars=%d\n",
		tag, m.LogID, m.DialMS, m.TLSMS, m.UpgradeMS, m.FirstCharMS, m.LastSendMS, m.FinalRespMS, m.RTF, m.PacketsSent, m.FinalTextLen)
}

func write(path string, b []byte) {
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(1)
}

// ── tiny string helpers (keep the file stdlib-only and allocation-light) ─

func trimCRLF(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func trimSpaceASCII(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func lowerASCII(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
