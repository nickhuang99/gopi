package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// voiceServeHTTP is server shape A: a standalone HTTP endpoint.
//
//	POST /transcribe   multipart/form-data, field "audio" = the file
//	→ {"text":"...","elapsed_ms":N,"chunk":15}
//	GET  /health       → {"ok":true,"engine":"funasr-nano"}
//
// Chosen over a bespoke protocol because any client can then drive it (curl,
// a browser, another machine) and the failure modes are ones every HTTP tool
// already understands. The tradeoff is a listening port.
func voiceServeHTTP(opts *voiceOptions) error {
	srv := newVoiceHTTPServer(opts)
	fmt.Fprint(os.Stderr, voiceListenBannerVoice(opts))

	// The SHUTDOWN SEAM: when a caller has armed one (voiceServeShutdownCh),
	// this server accepts on the loopback address that channel names and stops
	// the moment the channel is closed. It exists because ListenAndServe is a
	// one-way door — that is the whole point of the shipped shape — so a caller
	// that must END the server (the handler-contract test, which drives the real
	// serving path rather than a mux approximation) has no other handle on it.
	// Production never arms it (cli_voice.go's only call goes through
	// runVoiceSubcommand), so the shipped behaviour is untouched: the option is
	// read here, defaults to absent, and the absent case is the code below.
	if addr, ok := voiceServeShutdownAddr(opts); ok {
		// ONE read of the seam, handed down: the value this goroutine blocks on
		// is the channel the arming caller closes, taken under the lock.
		return voiceServeUntilClosed(srv, addr, voiceServeSeam())
	}
	return srv.ListenAndServe()
}

// voiceServeShutdownCh is armed by a caller that must be able to STOP the
// server voiceServeHTTP starts; its VALUE is the listen address to serve on.
// Nil (the production case, and the default of every voiceOptions literal) means
// the shipped ListenAndServe path.
//
// It is a package-level seam rather than a field of voiceOptions on purpose:
// voiceOptions is the CLI's parsed flag set, shared by every voice surface, and
// a test-only field there would be a knob the operator's parser can never set —
// while this seam is read at exactly one place and can only arrive from the
// process that owns the test.
// THE SEAM IS READ FROM TWO GOROUTINES: the caller ARMS it (writes the var, and
// disarms it to nil in its cleanup) while the server goroutine READS it to find
// out what to serve on and then blocks on that channel. A package-level var
// touched that way has no happens-before edge at all, and -race reported it by
// name (measured 2026-10-27, task 01a1756e, `go test ./cmd/agent -race`:
// Write by TestVoiceServeHTTPHandlerContract.func1 at cli_voice_review_test.go:1772
// vs Previous read by voiceServeUntilClosed at cli_voice_server.go:100).
// voiceServeSeamMu orders both — and voiceServeHTTP takes the value ONCE, so the
// server goroutine blocks on the very channel the caller closed.
var (
	voiceServeSeamMu     sync.Mutex
	voiceServeShutdownCh chan string
)

// armVoiceServeSeam sets (or, with nil, disarms) the shutdown seam. It is the
// ONLY writer: a caller that assigned the variable directly would be writing
// outside the lock the reader takes.
func armVoiceServeSeam(ch chan string) {
	voiceServeSeamMu.Lock()
	defer voiceServeSeamMu.Unlock()
	voiceServeShutdownCh = ch
}

// voiceServeSeam returns the armed seam (nil when no caller armed one).
func voiceServeSeam() chan string {
	voiceServeSeamMu.Lock()
	defer voiceServeSeamMu.Unlock()
	return voiceServeShutdownCh
}

// voiceServeShutdownAddr resolves the seam: the address to serve on when a
// caller has armed one. ok=false is the production path (ListenAndServe on
// opts.listen).
func voiceServeShutdownAddr(opts *voiceOptions) (string, bool) {
	if voiceServeSeam() == nil {
		return "", false
	}
	return opts.listen, true
}

// newVoiceHTTPServer builds the server shape both paths share, so the seam
// cannot drift from what ships (the timeouts below are the shipped ones).
func newVoiceHTTPServer(opts *voiceOptions) *http.Server {
	mux := http.NewServeMux()
	voiceServeTranscribeHTTP(mux, opts)

	return &http.Server{
		Addr:              opts.listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// A response deadline as well as a request one: EVERY response of this
		// server is one small JSON body, so a client that stops reading must not
		// hold a connection open forever. Without it a probe whose connection is
		// accepted but never answered — measured: a client that sends an
		// over-limit body, whose remaining bytes the server abandons and
		// correctly does NOT read — parks the CLIENT indefinitely (a test using
		// http.DefaultClient has no timeout at all) and the suite dies at its
		// own 10m panic instead of failing legibly.
		WriteTimeout: voiceServerWriteTimeout,
	}
}

// voiceServeUntilClosed runs srv until the seam's channel is closed, then shuts
// it down gracefully. It is the ONLY caller that ever calls Shutdown on this
// server, and it returns net/http's own verdict — ErrServerClosed
// ("http: Server closed") is the ordinary END of this path, not a failure, so a
// caller that cares can tell a clean stop from a real refusal (Rule 15: nothing
// is swallowed; the value is handed back and named).
func voiceServeUntilClosed(srv *http.Server, addr string, seam chan string) error {
	err := make(chan error, 1)
	go func() { err <- srv.ListenAndServe() }()

	<-seam
	ctx, cancel := context.WithTimeout(context.Background(), voiceServeShutdownTimeout)
	defer cancel()
	if shutdownErr := srv.Shutdown(ctx); shutdownErr != nil {
		// Close is the blunt half: an in-flight request that ignored the
		// context would otherwise keep the listener's goroutine alive, and this
		// path exists to guarantee the server is GONE.
		_ = srv.Close() //stylegate:ignore the graceful shutdown already failed; Close is the escalation and its error is subsumed by shutdownErr below
		return fmt.Errorf("voice_input server on %s did not shut down: %w", addr, shutdownErr)
	}
	return <-err
}

// voiceServeShutdownTimeout bounds the graceful half of the seam's shutdown; a
// request still in flight when it expires is closed by force (see
// voiceServeUntilClosed).
const voiceServeShutdownTimeout = 5 * time.Second

// voiceServerWriteTimeout bounds a single response write. It is longer than
// VoiceServerReadyTimeout's client-side probe deadline on purpose: the server
// state must not be able to expire a connection underneath a client still
// legitimately waiting on it, so the client always times out FIRST and reports
// the URL it could not reach.
const voiceServerWriteTimeout = 60 * time.Second

// voiceListenBannerVoice renders the startup banner for a run, naming the
// engine it will actually use (a server started with --engine doubao says so
// before it accepts a single request).
func voiceListenBannerVoice(opts *voiceOptions) string {
	return voiceListenBanner(opts.listen, voiceEngineLabel(opts), opts.chunk)
}

// voiceListenBanner is the startup banner: the two routes, plus an explicit
// warning when the listen address binds EVERY interface. It is a pure
// function of the address so the warning can be asserted without binding a
// wildcard in a test.
func voiceListenBanner(listen, engine string, chunk int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gopi tool voice_input: HTTP server on %s (engine=%s chunk=%ds)\n", listen, engine, chunk)
	fmt.Fprintf(&b, "  POST /transcribe  (multipart field \"audio\")\n")
	fmt.Fprintf(&b, "  GET  /health\n")
	if voiceListenIsWildcard(listen) {
		// Binding every interface is allowed (the LAN case), but it is a
		// security decision — say so, where the operator will read it.
		fmt.Fprintf(&b, "  WARNING: %s binds EVERY interface (0.0.0.0 + ::) and this endpoint has\n"+
			"           NO authentication — anyone who can reach this port can post audio\n"+
			"           and read the transcript. Drop --listen (default %s), or pass\n"+
			"           --listen 127.0.0.1:%s, to keep it loopback-only.\n",
			listen, voiceDefaultListen, voiceDefaultListenPort)
	}
	return b.String()
}

// voiceListenIsWildcard reports whether a listen address binds EVERY interface
// rather than one interface: Go's net package reads an EMPTY host as the
// wildcard, and 0.0.0.0 / :: (or their bracketed IPv6 form) spell it out. The
// distinction decides whether the machine running the server is the only
// possible caller, which is the whole point of voiceDefaultListen being
// loopback — so it is asserted rather than left to the operator's eye.
func voiceListenIsWildcard(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Not a host:port address at all. net.Listen still resolves such a
		// string (":7788" splits fine; "7788" is a port-only shorthand), so
		// the safe reading of "I cannot tell what this binds" is the warning.
		return true
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

// voiceServeTranscribeHTTP registers the server's two routes. Split out of
// voiceServeHTTP so the handler contract (the body cap, the status codes) is
// testable in-process, without binding a port.
func voiceServeTranscribeHTTP(mux *http.ServeMux, opts *voiceOptions) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeVoiceJSON(w, http.StatusOK, map[string]any{
			"ok": true, "engine": voiceEngineLabel(opts), "chunk": opts.chunk,
		})
	})
	mux.HandleFunc("/transcribe", voiceTranscribeHandler(opts))
}

// voiceTranscribeHandler is POST /transcribe: multipart field "audio" in, the
// transcript as JSON out.
func voiceTranscribeHandler(opts *voiceOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeVoiceJSON(w, http.StatusMethodNotAllowed,
				map[string]any{"error": "POST required"})
			return
		}

		// Cap the REQUEST BODY before any multipart parsing. The multipart
		// reader spools the body to disk (or memory) while hunting for the
		// part, so a client-controlled body would be written before any audio
		// budget is consulted. Two guards, because neither alone is
		// sufficient: a declared Content-Length is refused outright (cheap,
		// exact, and the common case), and a chunked / undeclared body —
		// ContentLength -1 — is stopped by the capped reader itself.
		if r.ContentLength > voiceMaxUploadBytes {
			writeVoiceJSON(w, http.StatusRequestEntityTooLarge,
				map[string]any{"error": voiceUploadTooLargeMessage()})
			return
		}

		// The engine reads from a path (it decodes via miniaudio), so the
		// upload is spooled to a temp file by the capped reader below — the
		// client's extension rides along because the decoder sniffs the
		// container from the name.
		audioPath, err := voiceSpoolAudioPart(w, r, voiceMaxUploadBytes)
		if err != nil {
			if voiceUploadTooLarge(err) {
				writeVoiceJSON(w, http.StatusRequestEntityTooLarge,
					map[string]any{"error": voiceUploadTooLargeMessage()})
				return
			}
			writeVoiceJSON(w, http.StatusBadRequest,
				map[string]any{"error": "missing multipart field \"audio\""})
			return
		}
		defer os.Remove(audioPath)

		text, elapsed, err := transcribeFile(opts, audioPath)
		if err != nil {
			writeVoiceJSON(w, http.StatusInternalServerError,
				map[string]any{"error": err.Error()})
			return
		}
		writeVoiceJSON(w, http.StatusOK, map[string]any{
			"text":       text,
			"elapsed_ms": elapsed.Milliseconds(),
			"chunk":      opts.chunk,
			"engine":     voiceEngineLabel(opts),
		})
	}
}

// voiceUploadTooLarge reports whether a multipart read failed because the body
// exceeded the MaxBytesReader cap. The *http.MaxBytesError is the precise
// signal; the message check covers a client-side read error that carries the
// same words, so an over-limit upload is never reported as a malformed one.
func voiceUploadTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	// The multipart reader may surface the cap as a wrapped read error rather
	// than the typed one; the standard library's wording is the marker.
	return strings.Contains(err.Error(), "request body too large")
}

// voiceUploadTooLargeMessage is the refusal the operator sees when an upload
// exceeds the /transcribe body cap (named once: the handler and the test both
// quote it).
func voiceUploadTooLargeMessage() string {
	return fmt.Sprintf("upload exceeds the %d byte limit for /transcribe", voiceMaxUploadBytes)
}

// errVoiceNoAudioPart is the sentinel for a multipart body with no "audio"
// part, so the handler can tell it apart from a read failure (a different
// client mistake with a different fix).
var errVoiceNoAudioPart = errors.New("no multipart part named \"audio\"")

// voiceSpoolAudioPart streams the "audio" part of r's multipart body into a
// temp file and returns its path — capped at limit, so a client cannot decide
// how many bytes this process reads or writes. The caller owns the file and
// must remove it.
//
// WHY NOT *http.Request.FormFile: FormFile parses the form with a ZERO
// maxMemory, which routes every part through multipart's own temp-file path —
// a spool that is (a) bounded only by the request body, i.e. by the client,
// unless the body is capped first, and (b) removed as soon as the
// multipart.Reader is done, so a part handed back as an *os.File is already
// unlinked and cannot be passed to the engine by path. Capping the body AND
// owning the spool removes both traps; the multipart envelope (boundary
// delimiters, part headers, filename) is charged to the budget too, which is
// why the cap carries the framing slack.
//
// The client's extension is preserved on the returned path: the engine's
// decoder sniffs the container from the file.
func voiceSpoolAudioPart(w http.ResponseWriter, r *http.Request, limit int64) (string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	mr, err := r.MultipartReader()
	if err != nil {
		return "", err
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return "", errVoiceNoAudioPart
		}
		if err != nil {
			return "", err
		}
		if part.FormName() != voiceAudioFormField {
			if err := part.Close(); err != nil {
				return "", err
			}
			continue
		}

		suffix := filepath.Ext(part.FileName())
		if suffix == "" {
			suffix = voiceDefaultAudioSuffix
		}
		tmp, err := os.CreateTemp("", "gopi-voice-*"+suffix)
		if err != nil {
			return "", closeVoicePart(part, err)
		}
		path := tmp.Name()
		if _, err := io.Copy(tmp, part); err != nil {
			return "", voiceDiscardSpool(path, tmp, part, err)
		}
		if err := tmp.Close(); err != nil {
			return "", voiceDiscardSpool(path, nil, part, err)
		}
		return path, closeVoicePart(part, nil)
	}
}

// voiceAudioFormField is the multipart field name the endpoint documents.
const voiceAudioFormField = "audio"

// voiceDefaultAudioSuffix is used when a client sends no filename extension;
// the engine sniffs the real container anyway, and a nameless suffix leaves the
// decoder without its hint.
const voiceDefaultAudioSuffix = ".wav"

// closeVoicePart closes the current part and folds its error into err — a part
// close failure is worth reporting, but never at the cost of losing the error
// that brought us here.
func closeVoicePart(part *multipart.Part, err error) error {
	if cerr := part.Close(); cerr != nil {
		return errors.Join(err, cerr)
	}
	return err
}

// voiceDiscardSpool abandons a half-written spool file: the temp file is
// closed (when it is still open) and removed, and both cleanup failures are
// joined onto the real error rather than replacing it (Rule 15 — no silent
// failure, no masked failure).
func voiceDiscardSpool(path string, f *os.File, part *multipart.Part, err error) error {
	if f != nil {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}
	if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
		err = errors.Join(err, rerr)
	}
	return closeVoicePart(part, err)
}

// voiceServeStdio is server shape B: read PCM on stdin, write text on stdout.
//
//	ssh host gopi tool voice_input --server --stdio < audio.pcm
//
// Zero new ports, and it reuses the ssh trust already in place between
// nodes. Input is raw 16 kHz mono float32 PCM (what the client's capture
// produces); anything decodable by the engine also works, which is what
// makes it testable without a microphone.
func voiceServeStdio(opts *voiceOptions) error {
	raw, err := voiceReadAll(os.Stdin, voiceMaxAudioBytes)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty stdin")
	}

	// Raw f32 PCM has no container, so the engine cannot sniff it. Wrap it in
	// a WAV header when it is not already a container format.
	audioPath, cleanup, err := voiceMaterializeAudio(raw)
	if err != nil {
		return err
	}
	defer cleanup()

	text, elapsed, err := transcribeFile(opts, audioPath)
	if err != nil {
		return err
	}
	fmt.Println(text)
	fmt.Fprintf(os.Stderr, "[voice] %d bytes in, %.1fs (chunk=%ds)\n",
		len(raw), elapsed.Seconds(), opts.chunk)
	return nil
}

// voiceContainerMagic lists the container signatures we pass through. A raw
// f32 PCM stream matches none of them. It is DERIVED from voiceContainerFormat
// (cli_voice_format.go) so the set of recognised containers has ONE spelling:
// that map answers "which format is this", this one answers "do the bytes need
// a WAV header", and both are keyed by the same voiceMagic* constants.
var voiceContainerMagic = func() map[string][]byte {
	m := make(map[string][]byte, len(voiceContainerFormat))
	for magic, format := range voiceContainerFormat {
		m[format] = []byte(magic)
	}
	return m
}()

const (
	voiceContainerWAV  = voiceMagicWAV
	voiceContainerMP3  = voiceMagicMP3
	voiceContainerOGG  = voiceMagicOGG
	voiceContainerFLAC = voiceMagicFLAC

	voiceMagicWAV  = "RIFF"
	voiceMagicMP3  = "ID3"
	voiceMagicOGG  = "OggS"
	voiceMagicFLAC = "fLaC"

	// mp3 without an ID3 tag starts with a frame sync: 0xFF 0xEx.
	voiceMP3FrameSync = 0xFF
	voiceMP3FrameMask = 0xE0
)

// voiceMaterializeAudio writes the received bytes to a temp file, wrapping
// raw f32 PCM in a WAV header first so the engine's decoder can read it.
func voiceMaterializeAudio(raw []byte) (path string, cleanup func(), err error) {
	for _, magic := range voiceContainerMagic {
		if len(raw) >= len(magic) && string(raw[:len(magic)]) == string(magic) {
			return voiceWriteTemp(raw, voicePassthroughSuffix)
		}
	}
	if len(raw) >= 2 && raw[0] == voiceMP3FrameSync && raw[1]&voiceMP3FrameMask == voiceMP3FrameMask {
		return voiceWriteTemp(raw, voicePassthroughSuffix)
	}
	// Otherwise treat as raw mono PCM at the capture rate and wrap it.
	return voiceWriteTemp(voiceWrapPCMAsWAV(raw), voiceWAWSuffix)
}

// voiceWAWSuffix is the temp-file suffix (voiceWriteTemp) and the uploaded
// filename both use, so the stage the engine reads is recognisably a wav.
const voiceWAWSuffix = ".wav"

// voicePassthroughSuffix is the temp-file suffix for bytes that ALREADY carry
// a container: the stream is not wrapped, so the name says so rather than
// borrowing the wav one. (It is also the suffix a --stdio upload of a
// container type the ENGINE cannot decode shows up as — measured: an OggS
// stream is passed through unchanged and the engine refuses it, exit 1.)
const voicePassthroughSuffix = ".audio"

func voiceWriteTemp(b []byte, suffix string) (string, func(), error) {
	f, err := os.CreateTemp("", "gopi-voice-*"+suffix)
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	if _, err := f.Write(b); err != nil {
		discardFile(f) //stylegate:ignore temp-file cleanup on the error path; the write error is what is returned
		os.Remove(name)
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", func() {}, err
	}
	return name, func() { os.Remove(name) }, nil
}

// voiceWrapPCMAsWAV prepends a canonical WAV header to a raw f32 PCM stream —
// the shape the stdio server reads (voiceServeStdio).
func voiceWrapPCMAsWAV(pcm []byte) []byte {
	return voiceBuildWAV(pcm, voiceWAVFormatFloat, voiceWAVFloat32Bits)
}

// WAV container constants, shared by both halves of the feature: the server's
// f32 stdio path (voiceWrapPCMAsWAV) and the capture client's PCM16 upload
// (voicePCM16ToWAV). One definition, because a header that disagrees with the
// samples is not a crash — it is a transcript of noise.
const (
	voiceWAVFormatPCM   = 1 // wFormatTag: integer PCM
	voiceWAVFormatFloat = 3 // wFormatTag: IEEE float
	voiceWAVFloat32Bits = 32
	voiceWAVHeaderBytes = 44
)

// voiceBuildWAV renders a 44-byte-header canonical WAV around raw PCM. Rate,
// channel count and byte order all come from the shared capture contract
// (voiceCapture*), so a change lands in the recorder argv and the header
// together.
func voiceBuildWAV(pcm []byte, wavFormat uint16, bitsPerSample int) []byte {
	blockAlign := voiceCaptureChannels * bitsPerSample / 8
	byteRate := voiceCaptureSampleRate * blockAlign
	dataLen := len(pcm)

	h := make([]byte, 0, voiceWAVHeaderBytes)
	le32 := func(v uint32) []byte {
		return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
	}
	le16 := func(v uint16) []byte {
		return []byte{byte(v), byte(v >> 8)}
	}
	h = append(h, []byte(voiceMagicWAV)...)
	h = append(h, le32(uint32(voiceWAVHeaderBytes-8+dataLen))...)
	h = append(h, []byte(voiceWAVFmtChunk)...)
	h = append(h, le32(16)...)
	h = append(h, le16(wavFormat)...)
	h = append(h, le16(uint16(voiceCaptureChannels))...)
	h = append(h, le32(uint32(voiceCaptureSampleRate))...)
	h = append(h, le32(uint32(byteRate))...)
	h = append(h, le16(uint16(blockAlign))...)
	h = append(h, le16(uint16(bitsPerSample))...)
	h = append(h, []byte(voiceWAVDataChunk)...)
	h = append(h, le32(uint32(dataLen))...)
	return append(h, pcm...)
}

const (
	voiceWAVFmtChunk  = "WAVEfmt "
	voiceWAVDataChunk = "data"
)

func writeVoiceJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The response is already committed by WriteHeader above and the payload is
	// an in-process map: an encode failure (a broken client socket) can only be
	// dropped, there is no status left to change.
	if err := json.NewEncoder(w).Encode(payload); err != nil { //stylegate:ignore nothing to report: headers sent, payload in-process
		return
	}
}

// discardFile closes a temp file whose content is being abandoned; the close
// error carries no signal on a path that is already failing.
func discardFile(f *os.File) {
	if err := f.Close(); err != nil { //stylegate:ignore best-effort cleanup of an abandoned temp file
		return
	}
}

// userHomeDirOrDefault resolves the operator's home for the voice model/runtime
// roots, falling back to os.TempDir when it cannot be determined — a missing
// HOME must not leave the voice paths as bare relative names.
func userHomeDirOrDefault() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return home
}
