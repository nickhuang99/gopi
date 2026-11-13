// The v2 binary protocol: the four-byte header, the three message shapes the
// client sends, the three the server answers with, and the JSON both sides
// carry. Everything here is ONE spelling of a wire fact (Rule 1/13) — the
// header templates in particular are byte constants, because a header that
// disagrees with the service is not a compile error, it is a refused
// handshake several layers away from the typo.
package doubaostream

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ── header templates (the official Go demo's byte values, measured) ─────
//
// byte 0: protocol version (0x1) | header size 4 (0x1)
// byte 1: message type           | message-type-specific flags
// byte 2: serialization (JSON)   | compression (gzip)
// byte 3: reserved
var (
	// headerFullClientRequest is the first message: 0b0001 full client
	// request, flags 0b0000 (none), JSON + gzip.
	headerFullClientRequest = [headerBytes]byte{0x11, 0x10, 0x11, 0x00}
	// headerAudioOnly is a non-final audio packet: 0b0010 audio only, flags
	// 0b0000.
	headerAudioOnly = [headerBytes]byte{0x11, 0x20, 0x11, 0x00}
	// headerLastAudio is the FINAL audio packet: 0b0010 audio only, flags
	// 0b0010 (NEG_SEQUENCE — "this is the last packet of the request", the
	// signal the docs describe as "the last packet's sequence is negative").
	// It is spelled as a flag rather than a negative number in the frame
	// because THIS is the form the official demo sends and the form the
	// probe measured as accepted.
	headerLastAudio = [headerBytes]byte{0x11, 0x22, 0x11, 0x00}
)

// ── server message types (byte 1 >> 4) ─────────────────────────────────
const (
	// msgTypeServerFullResponse carries the recognition result:
	// [4B payload size][payload].
	msgTypeServerFullResponse = 0x09
	// msgTypeServerAck acknowledges a packet for which there is no new
	// result: [4B sequence][4B payload size][payload].
	msgTypeServerAck = 0x0b
	// msgTypeServerError carries the FAILURE the client must surface:
	// [4B code][4B payload size][payload] (gzipped JSON, same as a response —
	// the demo prints the raw bytes, but the payload is a response document
	// and is parsed as one here, Rule 15: the real reason is in it).
	msgTypeServerError = 0x0f
)

// ── message sizes ──────────────────────────────────────────────────────
const (
	// headerBytes is the fixed header length: header size nibble 1 × 4.
	headerBytes = 4
	// sizeFieldBytes is the big-endian payload-size field.
	sizeFieldBytes = 4
	// seqFieldBytes is the big-endian sequence field of an ACK / error.
	seqFieldBytes = 4
	// headerSizeNibbleMask extracts byte 0's low nibble (header size / 4).
	headerSizeNibbleMask = 0x0f
	// msgTypeShift moves byte 1's high nibble down to the message type.
	msgTypeShift = 4
)

// ── application codes ──────────────────────────────────────────────────
const (
	// codeSuccess is the only code that means "this packet was understood and
	// answered"; every other code is a verdict the caller must see.
	codeSuccess = 1000
	// CodeNoSpeech is the service's CORRECT answer for audio that holds no
	// speech. It is not a transport failure and it is not a bug — see
	// NoSpeechError for why the distinction is load-bearing.
	CodeNoSpeech = 1013
)

// ── the request document ───────────────────────────────────────────────

// appMeta / userMeta / audioMeta / requestMeta are the four top-level objects
// of the full client request (the docs' field table, typed).
type appMeta struct {
	AppID   string `json:"appid"`
	Token   string `json:"token"`
	Cluster string `json:"cluster"`
}

type userMeta struct {
	UID string `json:"uid"`
}

type audioMeta struct {
	Format   string `json:"format"`
	Codec    string `json:"codec,omitempty"`
	Rate     int    `json:"rate"`
	Bits     int    `json:"bits"`
	Channel  int    `json:"channel"`
	Language string `json:"language,omitempty"`
}

type requestMeta struct {
	ReqID          string `json:"reqid"`
	Sequence       int    `json:"sequence"`
	NBest          int    `json:"nbest"`
	Workflow       string `json:"workflow"`
	ResultType     string `json:"result_type"`
	ShowUtterances bool   `json:"show_utterances"`
}

type requestDoc struct {
	App     appMeta     `json:"app"`
	User    userMeta    `json:"user"`
	Audio   audioMeta   `json:"audio"`
	Request requestMeta `json:"request"`
}

// ── the response document ──────────────────────────────────────────────

// serverResponse is one parsed full server response. Duration is a
// json.Number because the service has been seen to send it both ways (a bare
// number and a quoted one); a typed int would turn a quoted duration into a
// parse failure of the WHOLE document, which would lose the transcript
// alongside it.
type serverResponse struct {
	ReqID    string       `json:"reqid"`
	Code     int          `json:"code"`
	Message  string       `json:"message"`
	Sequence int          `json:"sequence"`
	Duration json.Number  `json:"duration"`
	LogID    string       `json:"log_id"`
	Result   []resultNode `json:"result"`
}

type resultNode struct {
	Text       string       `json:"text"`
	Confidence int          `json:"confidence"`
	Utterances []utteranceN `json:"utterances"`
}

type utteranceN struct {
	Text      string `json:"text"`
	StartTime int    `json:"start_time"`
	EndTime   int    `json:"end_time"`
	Definite  bool   `json:"definite"`
}

// text is the transcript this response carries: the result's own text when
// present, otherwise the utterance texts joined. The service sets both; taking
// the first non-empty one keeps a response that carries only utterances from
// reading as "no speech" (which would be the exact false negative Rule 15
// forbids).
func (r serverResponse) text() string {
	for _, res := range r.Result {
		if t := strings.TrimSpace(res.Text); t != "" {
			return t
		}
	}
	var parts []string
	for _, res := range r.Result {
		for _, u := range res.Utterances {
			if t := strings.TrimSpace(u.Text); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "")
}

// ── frame construction ─────────────────────────────────────────────────

// buildFullClientRequest renders message 1: header + size + gzip(JSON).
func buildFullClientRequest(doc requestDoc) ([]byte, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode the request document: %w", err)
	}
	body, err := gzipCompress(raw)
	if err != nil {
		return nil, err
	}
	return assembleHeader(headerFullClientRequest, uint32(len(body)), body), nil
}

// buildAudioPacket renders one audio-only message. last selects the final
// header template, whose flag is the end-of-stream signal.
func buildAudioPacket(pcm []byte, last bool) ([]byte, error) {
	body, err := gzipCompress(pcm)
	if err != nil {
		return nil, err
	}
	header := headerAudioOnly
	if last {
		header = headerLastAudio
	}
	return assembleHeader(header, uint32(len(body)), body), nil
}

// assembleHeader joins the fixed header, the big-endian size and the payload.
func assembleHeader(header [headerBytes]byte, size uint32, body []byte) []byte {
	out := make([]byte, 0, headerBytes+sizeFieldBytes+len(body))
	out = append(out, header[:]...)
	var sizeField [sizeFieldBytes]byte
	binary.BigEndian.PutUint32(sizeField[:], size)
	out = append(out, sizeField[:]...)
	return append(out, body...)
}

// ── frame parsing ──────────────────────────────────────────────────────

// serverMessage is one decoded message from the service.
type serverMessage struct {
	// Kind is the message type nibble (msgTypeServerFullResponse and friends).
	Kind byte
	// Sequence is the ACK's sequence field, when the message carries one.
	Sequence int32
	// Code is the SERVER_ERROR message's own code field.
	Code int32
	// JSON is the (decompressed) payload of a response/error.
	JSON []byte
	// Response is the parsed document; zero for an ACK with no payload.
	Response serverResponse
	// HasResponse reports whether JSON held a response document (so an empty
	// one is distinguishable from an unparsed one — Rule 15).
	HasResponse bool
}

// parseServerMessage decodes one server frame. Payloads are gzipped when the
// compression nibble says so; a frame that says gzip but does not decompress
// is an ERROR carrying the raw bytes rather than a silently empty document
// (Rule 15 — "the service sent something we could not read" must not look
// like "the service said nothing").
func parseServerMessage(msg []byte) (serverMessage, error) {
	if len(msg) < headerBytes {
		return serverMessage{}, fmt.Errorf("server frame of %d bytes is shorter than the %d byte header", len(msg), headerBytes)
	}
	headerSize := int(msg[0]&headerSizeNibbleMask) * headerBytes
	if headerSize < headerBytes || len(msg) < headerSize {
		return serverMessage{}, fmt.Errorf("server frame declares a %d byte header but carries %d bytes", headerSize, len(msg))
	}
	out := serverMessage{Kind: msg[1] >> msgTypeShift}
	body := msg[headerSize:]
	compression := msg[2] & headerSizeNibbleMask

	switch out.Kind {
	case msgTypeServerFullResponse:
		payload, err := splitSizeAndPayload(body, out.Kind)
		if err != nil {
			return serverMessage{}, err
		}
		out.JSON = payload
	case msgTypeServerAck:
		if len(body) >= seqFieldBytes {
			out.Sequence = int32(binary.BigEndian.Uint32(body[:seqFieldBytes]))
			body = body[seqFieldBytes:]
		}
		if len(body) >= sizeFieldBytes {
			payload, err := splitSizeAndPayload(body, out.Kind)
			if err != nil {
				return serverMessage{}, err
			}
			out.JSON = payload
		}
	case msgTypeServerError:
		if len(body) < seqFieldBytes+sizeFieldBytes {
			return serverMessage{}, fmt.Errorf("error frame carries %d payload bytes, need at least %d",
				len(body), seqFieldBytes+sizeFieldBytes)
		}
		out.Code = int32(binary.BigEndian.Uint32(body[:seqFieldBytes]))
		payload, err := splitSizeAndPayload(body[seqFieldBytes:], out.Kind)
		if err != nil {
			return serverMessage{}, err
		}
		out.JSON = payload
		if len(payload) == 0 {
			return out, nil
		}
	default:
		return serverMessage{}, fmt.Errorf("unknown server message type 0x%x", out.Kind)
	}

	if len(out.JSON) == 0 {
		return out, nil
	}
	if compression == compressionGzip {
		plain, err := gzipDecompress(out.JSON)
		if err != nil {
			return out, fmt.Errorf("gunzip a %.0f byte %s payload: %w", float64(len(out.JSON)), kindName(out.Kind), err)
		}
		out.JSON = plain
	}
	if !json.Valid(out.JSON) {
		// An error frame is allowed to carry a non-JSON body (the demo prints
		// it verbatim), but a RESPONSE that is not JSON is a protocol break we
		// must not paper over.
		if out.Kind == msgTypeServerError {
			return out, nil
		}
		return out, fmt.Errorf("%s payload is not JSON: %q", kindName(out.Kind), truncateForMessage(out.JSON))
	}
	if err := json.Unmarshal(out.JSON, &out.Response); err != nil {
		return out, fmt.Errorf("decode the %s payload: %w", kindName(out.Kind), err)
	}
	out.HasResponse = true
	return out, nil
}

// splitSizeAndPayload reads the big-endian size field and returns the payload
// it declares.
func splitSizeAndPayload(body []byte, kind byte) ([]byte, error) {
	if len(body) < sizeFieldBytes {
		return nil, fmt.Errorf("%s frame carries %d payload bytes, need at least %d",
			kindName(kind), len(body), sizeFieldBytes)
	}
	size := int(binary.BigEndian.Uint32(body[:sizeFieldBytes]))
	payload := body[sizeFieldBytes:]
	// A size field larger than what arrived is a TRUNCATED frame; a smaller one
	// is a frame with trailing bytes. Neither is silently accepted: the first
	// would decode a partial document, the second would hide a protocol change.
	if size != len(payload) {
		return nil, fmt.Errorf("%s frame declares a %d byte payload but carries %d bytes",
			kindName(kind), size, len(payload))
	}
	return payload, nil
}

// kindName names a message type for a human reading an error.
func kindName(kind byte) string {
	switch kind {
	case msgTypeServerFullResponse:
		return "full-server-response"
	case msgTypeServerAck:
		return "ack"
	case msgTypeServerError:
		return "server-error"
	}
	return fmt.Sprintf("msg-type-0x%x", kind)
}

// ── gzip (application-level compression, per the protocol's byte 2) ────

func gzipCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("gzip the payload: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip the payload: %w", err)
	}
	return buf.Bytes(), nil
}

func gzipDecompress(raw []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	plain, err := io.ReadAll(io.LimitReader(zr, maxPayloadBytes))
	if err != nil {
		return nil, err
	}
	if err := zr.Close(); err != nil {
		return nil, err
	}
	return plain, nil
}

// truncateForMessage keeps an unparseable payload short enough to be an error
// message (a vendor frame should never inject a screenful of bytes into one).
func truncateForMessage(b []byte) string {
	const limit = 200
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "…"
}
