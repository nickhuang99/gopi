package doubaosauc

// protocol_test.go — the v3 envelope, at the byte level.
//
// WHY THESE ASSERTIONS AND NOT A ROUND TRIP. The vendor's service is the only
// other implementation, and it answers a malformed envelope with a code that
// reads like an account problem (the measured 403 of a wrong credential SHAPE
// is the same class of false report). So the frame is asserted FIELD BY FIELD,
// with the field's meaning quoted in the test: a reader who changes the encoder
// has to say what the new bytes mean to the service.

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestHeaderByte0CarriesVersionAndSize asserts the ONE byte every message
// starts with: the protocol version in the high nibble and the header size (in
// 4-byte units) in the low one. 0x11 is what the v3 spec's own diagrams show,
// and it is assembled from the two named constants rather than typed.
func TestHeaderByte0CarriesVersionAndSize(t *testing.T) {
	if got := headerByte0(); got != 0x11 {
		t.Fatalf("byte 0 = %#02x, want 0x11 (version %#x, header size %d bytes)",
			got, protocolVersion, headerSize4Bytes)
	}
	h := saucHeader(msgTypeAudioOnlyRequest, flagSequencePos)
	if len(h) != headerSize4Bytes {
		t.Fatalf("the header is %d bytes, want %d", len(h), headerSize4Bytes)
	}
	if h[1] != msgTypeAudioOnlyRequest<<4|flagSequencePos {
		t.Fatalf("byte 1 = %#02x, want type %#x in the high nibble and flags %#x in the low one",
			h[1], msgTypeAudioOnlyRequest, flagSequencePos)
	}
	if h[2] != serializationJSON<<4|compressionGzip {
		t.Fatalf("byte 2 = %#02x, want JSON serialisation and gzip compression", h[2])
	}
	if h[3] != reservedByte {
		t.Fatalf("byte 3 = %#02x, want the reserved %#02x", h[3], reservedByte)
	}
}

// TestFullClientRequestFrameCarriesSequenceOneAndLength walks the first frame of
// a conversation: 4-byte header, sequence 1, a big-endian payload length that
// matches the gzipped body, and a body that gunzips back to the JSON.
//
// SEQUENCE 1 IS ASSERTED, not "some sequence": the server auto-assigns 1 to the
// session's parameter message, and an audio packet that repeats it is refused
// with a message about an auto-assigned sequence mismatch.
func TestFullClientRequestFrameCarriesSequenceOneAndLength(t *testing.T) {
	body := []byte(`{"request":{"model_name":"bigmodel"}}`)
	frame, err := fullClientRequestFrame(body)
	if err != nil {
		t.Fatalf("build the frame: %v", err)
	}
	if len(frame) < headerSize4Bytes+2*headerFieldBytes {
		t.Fatalf("the frame is %d bytes, shorter than header + two 4-byte fields", len(frame))
	}
	if got := int32(binary.BigEndian.Uint32(frame[headerSize4Bytes:])); got != fullRequestSequence {
		t.Fatalf("sequence = %d, want %d", got, fullRequestSequence)
	}
	payload := frame[headerSize4Bytes+2*headerFieldBytes:]
	if got, want := int32(binary.BigEndian.Uint32(frame[headerSize4Bytes+headerFieldBytes:])), int32(len(payload)); got != want {
		t.Fatalf("declared payload length %d, actual %d", got, want)
	}
	if got := mustGunzip(t, payload); !bytes.Equal(got, body) {
		t.Fatalf("the payload round-trips to %q, want %q", got, body)
	}
}

// TestAudioFrameSequenceNegationMarksTheEnd asserts the ONE sign convention the
// service uses for end-of-stream: the last packet's sequence is NEGATED (not
// incremented) and its flag nibble carries both the sequence bit and the
// last-packet bit.
func TestAudioFrameSequenceNegationMarksTheEnd(t *testing.T) {
	pcm := bytes.Repeat([]byte{0x01, 0x02}, 100)
	for _, tc := range []struct {
		seq      int32
		wantFlag byte
	}{{firstAudioSequence, flagSequencePos}, {-firstAudioSequence, flagSequenceNeg}} {
		frame, err := audioFrame(tc.seq, pcm)
		if err != nil {
			t.Fatalf("seq %d: build the frame: %v", tc.seq, err)
		}
		if got := frame[1] & 0x0F; got != tc.wantFlag {
			t.Fatalf("seq %d: flag nibble = %#x, want %#x", tc.seq, got, tc.wantFlag)
		}
		if got := int32(binary.BigEndian.Uint32(frame[headerSize4Bytes:])); got != tc.seq {
			t.Fatalf("sequence field = %d, want %d", got, tc.seq)
		}
		if got := mustGunzip(t, frame[headerSize4Bytes+2*headerFieldBytes:]); !bytes.Equal(got, pcm) {
			t.Fatalf("the audio payload round-trips to %d bytes, want %d", len(got), len(pcm))
		}
	}
}

// TestParseResponseReadsTheFullResponseFrame asserts what the reader does with
// the frames the SERVICE sends: a full response carries a length field then
// gzipped JSON, and the transcript is the LATEST FULL TEXT.
func TestParseResponseReadsTheFullResponseFrame(t *testing.T) {
	body, err := json.Marshal(responseDoc{Code: 0, Result: responseResult{Text: "你好，世界"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := saucHeader(msgTypeServerFullRespond, flagSequencePos|flagLastPacket)
	frame = putUint32(frame, 1)
	gz := mustGzip(t, body)
	frame = putUint32(frame, int32(len(gz)))
	frame = append(frame, gz...)

	code, last, text, errMsg := parseResponse(frame)
	if code != 0 || errMsg != "" {
		t.Fatalf("code=%d errMsg=%q, want a clean response", code, errMsg)
	}
	if !last {
		t.Fatalf("the last-packet flag was not read from the flags nibble")
	}
	if text != "你好，世界" {
		t.Fatalf("text = %q, want the response's own text", text)
	}
}

// TestParseResponseReadsTheErrorFrame asserts the error frame's layout: the
// code comes FIRST (a big-endian 32-bit field) and is followed by a second
// 4-byte field before the payload — a reader that skipped one of them would
// decompress the middle of the envelope and report nonsense.
func TestParseResponseReadsTheErrorFrame(t *testing.T) {
	body, err := json.Marshal(responseDoc{Code: 45000030, Error: "no valid speech in the audio"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := saucHeader(msgTypeServerError, flagNoSequence)
	frame = putUint32(frame, 45000030)
	gz := mustGzip(t, body)
	frame = putUint32(frame, int32(len(gz)))
	frame = append(frame, gz...)

	code, _, _, errMsg := parseResponse(frame)
	if code != 45000030 {
		t.Fatalf("code = %d, want the frame's own code", code)
	}
	if !strings.Contains(errMsg, serviceNoSpeechMarker) {
		t.Fatalf("error text = %q, want the service's wording (which carries %q)",
			errMsg, serviceNoSpeechMarker)
	}
}

// TestParseResponseToleratesAShortOrAlienFrame is the anti-panic half: bytes
// from a peer that is not this protocol must produce an ANSWER, never an index
// out of range. The reader runs in a goroutine, so a panic there takes the whole
// process down.
//
// WHAT IT DOES NOT ASSERT: that `last` is false. Bit 1 of byte 1 IS the
// last-packet flag, so a frame of 0xFF bytes legitimately reports last=true —
// asserting zero values there would be asserting that the decoder ignores a flag
// it is supposed to read. The contract these frames share is narrower and true:
// no panic, and no TEXT invented out of bytes that carry no transcript.
func TestParseResponseToleratesAShortOrAlienFrame(t *testing.T) {
	hostile := [][]byte{
		nil,
		{0x11},
		{0xF0, 0x00, 0x00, 0x00},
		{0x11, 0x99, 0x10, 0x00, 0x00, 0x00, 0x00, 0x01},
		bytes.Repeat([]byte{0xFF}, 64),
		bytes.Repeat([]byte{0x00}, 4096),
	}
	for _, msg := range hostile {
		code, _, text, errMsg := parseResponse(msg)
		if text != "" {
			t.Fatalf("frame % x produced the text %q from bytes that carry no transcript",
				msg[:min(len(msg), 16)], text)
		}
		if code != 0 && errMsg == "" {
			t.Fatalf("frame % x produced code %d with no explanation", msg[:min(len(msg), 16)], code)
		}
	}
	// The three frames that carry NO header at all are the empty-value case.
	for _, msg := range [][]byte{nil, {0x11}, {0xF0, 0x00, 0x00, 0x00}} {
		if code, last, text, errMsg := parseResponse(msg); code != 0 || last || text != "" || errMsg != "" {
			t.Fatalf("frame % x produced (%d, %v, %q, %q), want zero values", msg, code, last, text, errMsg)
		}
	}
}

// TestFinishReportsNoSpeechAsItsOwnError asserts the ONE place a verdict is
// inferred: the service's own phrase produces NoSpeechError (a typed answer),
// and a message WITHOUT that phrase is NOT turned into a "no speech" claim — it
// stays the service's own wording.
func TestFinishReportsNoSpeechAsItsOwnError(t *testing.T) {
	withPhrase := &Result{errMsg: "No valid speeches found in input audio"}
	err := withPhrase.finish("req-1")
	if _, ok := err.(*NoSpeechError); !ok {
		t.Fatalf("err = %T (%v), want *NoSpeechError", err, err)
	}
	if !withPhrase.NoSpeech {
		t.Fatalf("the result does not record the no-speech verdict")
	}
	other := &Result{errMsg: "quota exceeded"}
	if err := other.finish("req-1"); err == nil {
		t.Fatalf("an unexplained emptiness was reported as a success")
	} else if _, isNoSpeech := err.(*NoSpeechError); isNoSpeech {
		t.Fatalf("a message without the marker was reported as no-speech: %v", err)
	}
	empty := &Result{}
	if err := empty.finish("req-1"); err == nil {
		t.Fatalf("an empty transcript with no code was reported as a success")
	}
}

func mustGzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	out, err := gzipBytes(raw)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	return out
}

func mustGunzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the payload is not gzip: %v", err)
	}
	out, err := io.ReadAll(io.LimitReader(zr, maxResponseBytes))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return out
}
