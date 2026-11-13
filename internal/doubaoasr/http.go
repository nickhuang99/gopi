package doubaoasr

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// readFile is the package's single file-read seam (a test can substitute it;
// production reads the real file system).
var readFile = os.ReadFile

// client is one in-flight transcription.
type client struct {
	opts       Options
	resourceID string
	http       *http.Client
}

// httpClient builds the request client from the resolved timeout.
func (c *client) httpClient() *http.Client {
	if c.http == nil {
		c.http = &http.Client{Timeout: time.Duration(c.opts.httpTimeoutSec()) * time.Second}
	}
	return c.http
}

// requestBody assembles the wire body: audio (inline base64 or a URL), the
// optional geometry, and the feature/corpus switches.
func (c *client) requestBody() (requestBody, error) {
	o := c.opts
	body := requestBody{
		User: map[string]string{"uid": o.uid()},
		Audio: audioMeta{
			URL:     strings.TrimSpace(o.AudioURL),
			Format:  stringOrDefault(o.Format, DefaultFormat),
			Codec:   stringOrDefault(o.Codec, DefaultCodec),
			Rate:    intOrDefault(o.Rate, DefaultRate),
			Bits:    intOrDefault(o.Bits, DefaultBits),
			Channel: intOrDefault(o.Channels, DefaultChannel),
		},
		Request: requestMeta{
			ModelName:         ModelName,
			EnableITN:         o.EnableITN,
			EnablePUNC:        o.EnablePUNC,
			EnableDDC:         o.EnableDDC,
			EnableSpeakerInfo: o.EnableSpeakerInfo,
			EnableLID:         o.EnableLID,
			ShowUtterances:    o.ShowUtterances,
		},
	}

	if file := strings.TrimSpace(o.AudioFile); file != "" {
		raw, err := readFile(file)
		if err != nil {
			return requestBody{}, fmt.Errorf("%sread audio %q: %w", errPrefix, file, err)
		}
		if len(raw) == 0 {
			return requestBody{}, fmt.Errorf("%s%q is empty", errPrefix, msgEmptyAudioFile)
		}
		body.Audio.Data = base64.StdEncoding.EncodeToString(raw)
		// The service trusts the declared geometry over the container, so a
		// canonical WAV's own header is the most accurate source when the
		// caller did not pin the format explicitly.
		if g, ok := sniffWAVGeometry(raw); ok {
			if strings.TrimSpace(o.Format) == "" {
				body.Audio.Format = DefaultFormat
			}
			if strings.TrimSpace(o.Codec) == "" {
				body.Audio.Codec = DefaultCodec
			}
			if o.Rate <= 0 {
				body.Audio.Rate = g.rate
			}
			if o.Bits <= 0 {
				body.Audio.Bits = g.bits
			}
			if o.Channels <= 0 {
				body.Audio.Channel = g.channels
			}
		}
	}

	ctx, err := buildContext(o.Hotwords)
	if err != nil {
		return requestBody{}, err
	}
	body.Request.Corpus = corpusMeta{
		BoostingTableName: strings.TrimSpace(o.BoostingTableName),
		Context:           ctx,
	}
	return body, nil
}

// run submits the job and polls it to completion.
func (c *client) run(ctx context.Context, body requestBody) (*Result, error) {
	reqID := newRequestID()
	start := timeNow()

	res := &Result{ResourceID: c.resourceID, RequestID: reqID}
	st, hdr, raw, err := c.post(ctx, c.opts.submitURL(), reqID, body)
	res.SubmitMS = msSince(start)
	res.SubmitLogID = hdr.Get(headerLogID)
	code := hdr.Get(headerStatusCode)
	res.StatusCodes = append(res.StatusCodes, code)
	if err != nil {
		return res, &APIError{Op: opSubmit, HTTPStatus: st, StatusCode: code, Message: hdr.Get(headerAPIMessage),
			LogID: res.SubmitLogID, RequestID: reqID, Body: truncateBody(raw), Err: err}
	}
	if st != http.StatusOK || code != StatusOK {
		return res, &APIError{Op: opSubmit, HTTPStatus: st, StatusCode: code, Message: hdr.Get(headerAPIMessage),
			LogID: res.SubmitLogID, RequestID: reqID, Body: truncateBody(raw)}
	}

	final, err := c.poll(ctx, reqID, res, start)
	res.TotalMS = msSince(start)
	if err != nil {
		return res, err
	}
	if final.AudioInfo != nil {
		res.DurationMS = final.AudioInfo.Duration
	}
	if final.Result != nil {
		res.Text = final.Result.Text
		for _, u := range final.Result.Utterances {
			res.Utterances = append(res.Utterances, Utterance{
				Definite: u.Definite, Text: u.Text, SpeakerID: u.Speaker, StartMS: u.Start, EndMS: u.End,
			})
		}
	}
	if secs := float64(res.DurationMS) / msPerSecond; secs > 0 {
		res.RTF = float64(res.TotalMS) / msPerSecond / secs
	}
	return res, nil
}

// poll queries the job until it is done. Two codes mean "still working" and
// are the only ones retried; anything else ends the loop with the service's
// own verdict attached (Rule 15).
func (c *client) poll(ctx context.Context, reqID string, res *Result, start time.Time) (*responseBody, error) {
	deadline := start.Add(time.Duration(c.opts.pollTimeoutSec()) * time.Second)
	interval := time.Duration(c.opts.pollIntervalSec()) * time.Second

	for {
		if err := sleepCtx(ctx, interval); err != nil {
			return nil, err
		}
		res.Polls++

		st, hdr, raw, err := c.post(ctx, c.opts.queryURL(), reqID, map[string]any{})
		res.QueryLogID = hdr.Get(headerLogID)
		code := hdr.Get(headerStatusCode)
		res.StatusCodes = append(res.StatusCodes, code)
		if err != nil {
			return nil, &APIError{Op: opQuery, HTTPStatus: st, StatusCode: code, Message: hdr.Get(headerAPIMessage),
				LogID: res.QueryLogID, RequestID: reqID, Body: truncateBody(raw), Err: err}
		}
		if st != http.StatusOK {
			return nil, &APIError{Op: opQuery, HTTPStatus: st, StatusCode: code, Message: hdr.Get(headerAPIMessage),
				LogID: res.QueryLogID, RequestID: reqID, Body: truncateBody(raw)}
		}

		var rb responseBody
		if err := json.Unmarshal(raw, &rb); err != nil {
			return nil, fmt.Errorf("%sdecode query body: %w (raw=%s)", errPrefix, err, truncateBody(raw))
		}
		switch code {
		case StatusOK:
			return &rb, nil
		case StatusProcessing, StatusQueued:
			// The budget is checked AFTER a poll, so the timeout message always
			// carries at least one real service verdict (a check before the
			// first query would report "0 polls" — nothing observable).
			if timeNow().After(deadline) {
				return nil, fmt.Errorf("%spoll timed out after %ds (%d polls, last status %q)",
					errPrefix, c.opts.pollTimeoutSec(), res.Polls, code)
			}
		default:
			msg := hdr.Get(headerAPIMessage)
			if msg == "" {
				msg = rb.Header.Message
			}
			return nil, &APIError{Op: opQuery, HTTPStatus: st, StatusCode: code, Message: msg,
				LogID: res.QueryLogID, RequestID: reqID, Body: truncateBody(raw)}
		}
	}
}

// post sends one JSON request and returns the status, headers and body.
//
// The API key travels ONLY in the request header: it is never put into the
// body, never logged, and the error paths below copy headers/body into typed
// errors without it.
func (c *client) post(ctx context.Context, url, reqID string, body any) (int, http.Header, []byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%smarshal request: %w", errPrefix, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%sbuild request: %w", errPrefix, err)
	}
	req.Header.Set(headerAPIKey, c.opts.APIKey)
	req.Header.Set(headerResourceID, c.resourceID)
	req.Header.Set(headerRequestID, reqID)
	req.Header.Set(headerSequence, fmt.Sprint(sequenceSingle))
	req.Header.Set(headerContentType, contentTypeJSON)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, http.Header{}, nil, fmt.Errorf("%shttp: %w", errPrefix, err)
	}
	defer closeBody(resp.Body)

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return resp.StatusCode, resp.Header, raw, readErr
}

// closeBody drains and closes a response body; the close error carries no
// signal on a path that already has the answer (the body was fully read).
func closeBody(b io.ReadCloser) {
	if _, err := io.Copy(io.Discard, b); err != nil { //stylegate:ignore draining a response body we already read; nothing left to report
		_ = b.Close()
		return
	}
	if err := b.Close(); err != nil { //stylegate:ignore best-effort close of a fully-drained body
		return
	}
}

// sleepCtx waits for d, or returns early when the context ends.
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

// truncateBody bounds an error body so a verbose service response cannot
// flood a CLI error line.
func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxErrorBodyChars {
		return s[:maxErrorBodyChars] + "…"
	}
	return s
}

// msSince renders an elapsed duration in whole milliseconds.
func msSince(start time.Time) int64 { return timeNow().Sub(start).Milliseconds() }

// msPerSecond converts milliseconds to seconds in one place.
const msPerSecond = 1000.0

// op names (the APIError's Op field — Rule 1b: the wire operations are named).
const (
	opSubmit = "submit"
	opQuery  = "query"
)

// newRequestID renders the X-Api-Request-Id: a random RFC-4122-shaped string.
// Random (not a counter) so two clients on one host cannot collide, and never
// derived from the audio path — the id goes to the vendor's logs.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// A weak id is better than no request: the call still gets a log id
		// from the service, which is what support traces.
		return fmt.Sprintf("fallback-%d", timeNow().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// wavGeometry is the sample geometry read out of a canonical WAV header.
type wavGeometry struct {
	rate     int
	channels int
	bits     int
}

// sniffWAVGeometry reads the fmt chunk of a RIFF/WAVE file. It mirrors the
// evaluation client's parser (devdoc/doubao-asr-eval/aucfile/main.go) so the
// product path declares the same geometry the measurement did. A non-WAV file
// returns ok=false and the caller keeps its own default.
func sniffWAVGeometry(b []byte) (wavGeometry, bool) {
	if len(b) < wavHeaderScanBytes || string(b[0:4]) != wavRIFFMagic || string(b[8:12]) != wavWAVEMagic {
		return wavGeometry{}, false
	}
	off := wavHeaderScanBytes
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := b[off+8:]
		if size > len(body) {
			size = len(body)
		}
		if id == wavFmtChunkID {
			if size < wavFmtChunkMinBytes {
				return wavGeometry{}, false
			}
			return wavGeometry{
				channels: int(binary.LittleEndian.Uint16(body[2:4])),
				rate:     int(binary.LittleEndian.Uint32(body[4:8])),
				bits:     int(binary.LittleEndian.Uint16(body[14:16])),
			}, true
		}
		off += 8 + size + (size & 1)
	}
	return wavGeometry{}, false
}

// WAV container constants (named: the magic bytes are a contract).
const (
	wavRIFFMagic        = "RIFF"
	wavWAVEMagic        = "WAVE"
	wavFmtChunkID       = "fmt "
	wavHeaderScanBytes  = 12
	wavFmtChunkMinBytes = 16
)
