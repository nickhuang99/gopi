// Package doubaosauc is the THIRD cloud arm of gopi's speech-to-text surface:
// the Volcengine (ByteDance) Doubao **SAUC v3** binary protocol over one
// WebSocket — 豆包流式语音识别模型 2.0 (`volc.seedasr.sauc.duration`).
//
// WHERE IT SITS, AND WHY IT IS A THIRD PACKAGE. The two arms beside it answer
// two other shapes (see internal/config/settings.go's engine values):
//
//	internal/doubaoasr   batch     录音文件识别 (AUC v3): POST the audio, poll
//	                                for the transcript — one HTTP JSON protocol
//	internal/doubaostream real-time v2 streaming ASR: a JSON envelope inside
//	                                binary frames, partials while you speak
//	internal/doubaosauc  SAUC v3   the v3 streaming protocol: a JSON request
//	                                frame + gzipped audio frames, same shape as
//	                                v2 in WHAT it does, different in EVERY byte
//	                                of HOW — 4-byte header nibbles, big-endian
//	                                length and sequence fields, a negative
//	                                sequence marking end-of-stream, and a
//	                                DIFFERENT CREDENTIAL SHAPE (below)
//
// The v3 protocol is not a variant of v2 with a flag: v2 wraps each audio
// packet in a 4-byte header too, but its sequence/flags/serialisation fields
// and its response envelope differ enough that a "shared" implementation would
// be two implementations behind one name. So the TRANSPORT is shared (RFC 6455
// framing is protocol-independent and lives in internal/doubaostream, exported
// through WsDial/WriteBinary/ReadMessage) and the APPLICATION PROTOCOL is not.
//
// THE CREDENTIAL SHAPE IS THE POINT OF THE WHOLE CHANNEL, and it was measured
// rather than read: on this account the v3 SAUC endpoint answers
//
//	X-Api-Key: <UUID>                                    → 403 requested resource not granted
//	X-Api-App-Key: <AppID> + X-Api-Access-Key: <Token>   → 101 Switching Protocols
//
// The 403 READ like "the service is not enabled", and the real cause was the
// SHAPE of the credential: the UUID key belongs to a different application, so
// it can never be granted this product line. Both shapes are implemented
// (Options.APIKey for the single-key shape, Options.AppKey/AccessKey for the
// legacy console pair), because a probe that can only speak one of them cannot
// produce that comparison again — the control is what makes the measurement.
//
// WHAT THIS PACKAGE REUSES RATHER THAN RE-DERIVES (Rule 13/14):
//   - the WebSocket transport: internal/doubaostream's WsDial/WriteBinary/
//     ReadMessage (one handshake, one masked-frame writer, one reassembler)
//   - the credential file format and its location:
//     doubaostream.LoadCredentials / DefaultCredentialPath — the operator's
//     existing `~/.doubao/v2_voice.env` KEY=VALUE document ALREADY carries the
//     APPID and TOKEN this channel's legacy shape needs, so a second credential
//     mechanism would be a second file to keep in sync for no reason
//   - the WAV decoding: doubaostream.DecodeAudio (16 kHz mono PCM16)
package doubaosauc

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

// ── the SAUC v3 binary envelope ────────────────────────────────────────
//
// Every message, in both directions, is a 4-byte header followed by optional
// 4-byte fields and then the payload:
//
//	byte 0  | protocol version (high nibble) | header size in 4-byte units (low nibble)
//	byte 1  | message type  (high nibble)    | message-specific flags (low nibble)
//	byte 2  | serialisation (high nibble)    | compression (low nibble)
//	byte 3  | reserved, always 0x00
//	[4..8)  | sequence        (present when the flags say the message carries one)
//	[8..12) | payload length  (big endian; present on client requests and on the
//	                          server's full responses, absent on bare acks)
//	[...)   | payload (gzipped JSON on a request, gzipped JSON on a response)
//
// The nibbles are the protocol's own vocabulary rather than tunables, which is
// why they are named constants and not literal 0x11s scattered through the
// encoder (Rule 1/13).
const (
	// protocolVersion is the only version the v3 endpoints accept.
	protocolVersion = 0b0001
	// headerSize4Bytes is a header of 4 bytes, the only size either end sends.
	headerSize4Bytes = 4
	// headerSizeUnits is that same size expressed in the units byte 0's low
	// nibble counts in, so the byte can be assembled from its two named halves
	// instead of typed as 0x11.
	headerSizeUnits = 4 / headerSize4Bytes

	// Message types (byte 1's high nibble).
	msgTypeFullClientRequest = 0b0001
	msgTypeAudioOnlyRequest  = 0b0010
	msgTypeServerFullRespond = 0b1001
	msgTypeServerError       = 0b1111

	// Flags (byte 1's low nibble). A message may carry a sequence number
	// (bit 0), and the FINAL audio packet does not increment it but NEGATES it
	// (bit 1) — which is how the service is told the stream ended without a
	// second message type for it.
	flagSequencePos = 0b0001
	flagLastPacket  = 0b0010
	flagSequenceNeg = flagSequencePos | flagLastPacket
	flagNoSequence  = 0b0000

	// Serialisation (byte 2's high nibble) and compression (its low nibble).
	serializationJSON = 0b0001
	compressionGzip   = 0b0001

	// reservedByte is byte 3, which every message carries and no message uses.
	reservedByte = 0x00

	// sequenceFieldBytes / lengthFieldBytes are the two optional 4-byte fields
	// between the header and the payload.
	sequenceFieldBytes = 4
	lengthFieldBytes   = 4
)

// ── the endpoint, the resources and the request's fixed vocabulary ─────
const (
	// DefaultEndpoint is the v3 SAUC one-shot ("file semantics") endpoint: send
	// the whole file, receive one final transcript. It is the DEFAULT because it
	// is the shape voice_input's contract asks for — one audio file in, one
	// transcript out — and because it is the one the epic's acceptance run
	// exercises; the bidirectional endpoint is reachable by naming it.
	//
	// MEASURED (2026-10-13, 4.83 s of Mandarin): `bigmodel_nostream` returned
	// the full transcript in 5.52 s with the packets paced in real time, while
	// `bigmodel_async` returned its first character at 2.86 s and its final
	// transcript at 5.22 s. Both are correct; they are different shapes, and the
	// choice is the caller's.
	DefaultEndpoint = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream"
	// EndpointBidirectional is the streaming counterpart, named so a caller can
	// ask for partials without typing a URL.
	EndpointBidirectional = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"

	// DefaultResourceID is 豆包流式语音识别模型 2.0 (hourly billing). The 1.0
	// resource (volc.bigasr.sauc.duration) is reachable by naming it; a resource
	// the account is not granted is refused by the SERVICE (403), which is a
	// report about the account rather than about this client.
	DefaultResourceID = "volc.seedasr.sauc.duration"

	// modelName is the only value the v3 request accepts for its model field.
	modelName = "bigmodel"
	// defaultUID identifies gopi's requests to the service.
	defaultUID = "gopi-doubao-sauc"

	// audioFormatRaw / audioCodecRaw name the payload the service is sent: bare
	// little-endian PCM16. The container is decoded HERE, so the service never
	// sees a RIFF header.
	audioFormatRaw = "pcm"
	audioCodecRaw  = "raw"
)

// ── the legacy console credential ──────────────────────────────────────
//
// THE HEADER NAMES ARE THE MEASUREMENT. They are constants because the two
// spellings are quoted by the refusal message, by the tests and by CONFIG.md,
// and a typo in one of them is a 403 that reads like a missing entitlement.
const (
	// HeaderAppKey / HeaderAccessKey are the legacy console pair: the
	// application id and its access token. This is the shape that was measured
	// to upgrade successfully.
	HeaderAppKey    = "X-Api-App-Key"
	HeaderAccessKey = "X-Api-Access-Key"
	// HeaderAPIKey is the single-key shape (an API key UUID). Implemented
	// because a probe must be able to reproduce the CONTROL case (403) as well
	// as the working one — see the package doc.
	HeaderAPIKey = "X-Api-Key"
	// HeaderResourceID selects the product line the request is billed against.
	// A resource the account is not granted is a 403, never a fallback.
	HeaderResourceID = "X-Api-Resource-Id"
	// HeaderRequestID is the caller's own id for the request; the vendor's
	// support asks for it beside the log id.
	HeaderRequestID = "X-Api-Request-Id"
)

// ── bounds and defaults ────────────────────────────────────────────────
const (
	// defaultSegmentMillis is how much audio one packet carries. It is the
	// geometry the v2 arm measured as the service's steady state (200 ms), and
	// it is a property of the protocol's recommended cadence rather than of a
	// caller.
	defaultSegmentMillis = 200
	// bytesPerSample16 is the ONE place a sample count becomes a byte count for
	// 16-bit audio (reused arithmetic from the v2 arm, named here rather than
	// typed as a 2).
	bytesPerSample16 = 2
	// milliPerSecond converts a duration in samples to a duration in the unit
	// every report on this surface uses.
	milliPerSecond = 1000
	// maxResponseBytes bounds one decompressed response payload, so a broken or
	// hostile peer cannot make this client allocate without limit.
	maxResponseBytes = 32 << 20
	// errorBodyBytes bounds how much of an unparseable payload an error quotes.
	errorBodyBytes = 200
)

// apierrorOp names the step a failure happened at. One vocabulary (Rule 13) so
// a caller grepping a log and a test asserting a message read the same words.
const (
	opConnect    = "connect"
	opHandshake  = "handshake"
	opRequest    = "request"
	opAudio      = "audio"
	opRead       = "read"
	opServer     = "server"
	opCredential = "credential"
)

// headerByte0 is byte 0 of every message: version in the high nibble, header
// size (in 4-byte units) in the low nibble.
func headerByte0() byte { return protocolVersion<<4 | headerSizeUnits }

// saucHeader builds the 4-byte header for an outgoing message.
func saucHeader(msgType, flags byte) []byte {
	return []byte{
		headerByte0(),
		msgType<<4 | flags,
		serializationJSON<<4 | compressionGzip,
		reservedByte,
	}
}

// gzipBytes compresses a payload. The v3 request frames are gzipped JSON and
// the audio frames gzipped PCM — the compression nibble in the header says so,
// and a frame whose nibble disagrees with its bytes is refused by the service
// with an unhelpful message.
func gzipBytes(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("gzip the SAUC payload: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip the SAUC payload: %w", err)
	}
	return buf.Bytes(), nil
}

// gunzipBytes decompresses a response payload, bounded by maxResponseBytes.
func gunzipBytes(raw []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	plain, err := io.ReadAll(io.LimitReader(zr, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if err := zr.Close(); err != nil {
		return nil, err
	}
	return plain, nil
}

// putUint32 appends a big-endian 32-bit field. Both optional fields of the
// envelope are 4 bytes and network byte order, and spelling that once keeps the
// sequence field and the length field from being written two different ways.
func putUint32(dst []byte, v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return append(dst, b[:]...)
}

// fullClientRequestFrame builds the FIRST message of a conversation: the
// session's parameters (JSON), sequence 1, with an explicit length field.
//
// THE SEQUENCE STARTS AT 1 AND THE AUDIO CONTINUES AT 2, which is a fact about
// the protocol that is easy to get wrong in a way the service reports
// confusingly: the full request consumes sequence 1 (the server auto-assigns
// it), and an audio packet that repeats a sequence already used is refused with
// "autoAssignedSequence (N) mismatch sequence in request (M)". The official
// demo expresses the same rule implicitly, by pre-incrementing its counter.
func fullClientRequestFrame(request []byte) ([]byte, error) {
	gz, err := gzipBytes(request)
	if err != nil {
		return nil, err
	}
	frame := saucHeader(msgTypeFullClientRequest, flagSequencePos)
	frame = putUint32(frame, fullRequestSequence)
	frame = putUint32(frame, int32(len(gz)))
	return append(frame, gz...), nil
}

// audioFrame builds one audio message carrying `seq`. A NEGATIVE sequence marks
// the last packet and negates the number rather than incrementing it; that
// sign, plus the flag nibble, is how the service learns the stream has ended.
func audioFrame(seq int32, pcm []byte) ([]byte, error) {
	gz, err := gzipBytes(pcm)
	if err != nil {
		return nil, err
	}
	flags := byte(flagSequencePos)
	if seq < 0 {
		flags = flagSequenceNeg
	}
	frame := saucHeader(msgTypeAudioOnlyRequest, flags)
	frame = putUint32(frame, seq)
	frame = putUint32(frame, int32(len(gz)))
	return append(frame, gz...), nil
}
