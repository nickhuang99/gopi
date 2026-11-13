package doubaostream

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── audio: the container is decoded here, so its geometry is pinned ─────

// writeWAV renders a canonical WAV with the given format tag, rate and samples
// (the test's own writer, independent of any production helper).
func writeWAV(t *testing.T, formatTag, channels, rate, bits int, data []byte) string {
	t.Helper()
	byteRate := rate * channels * bits / 8
	blockAlign := channels * bits / 8
	var b strings.Builder
	b.WriteString(wavRIFFMagic)
	writeUint32(t, &b, uint32(36+len(data)))
	b.WriteString(wavWAVEMagic)
	b.WriteString(wavFmtChunkID)
	writeUint32(t, &b, uint32(wavBaseFmtBytes))
	writeUint16(t, &b, uint16(formatTag))
	writeUint16(t, &b, uint16(channels))
	writeUint32(t, &b, uint32(rate))
	writeUint32(t, &b, uint32(byteRate))
	writeUint16(t, &b, uint16(blockAlign))
	writeUint16(t, &b, uint16(bits))
	b.WriteString(wavDataChunkID)
	writeUint32(t, &b, uint32(len(data)))
	path := filepath.Join(t.TempDir(), "fixture.wav")
	if err := os.WriteFile(path, append([]byte(b.String()), data...), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func writeUint32(t *testing.T, b *strings.Builder, v uint32) {
	t.Helper()
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}

func writeUint16(t *testing.T, b *strings.Builder, v uint16) {
	t.Helper()
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	b.Write(buf[:])
}

// sinePCM16 renders `seconds` of a 440 Hz tone at the given rate.
func sinePCM16(rate int, seconds float64) []byte {
	n := int(float64(rate) * seconds)
	out := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/float64(rate)) * 12000)
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

// TestDecodeWAVAtTheTargetGeometryIsAnIdentity: a 16 kHz mono PCM16 WAV is the
// shape the service wants, so decoding it must not alter one byte. This is the
// property the streaming claim rests on — an off-by-one here is a transcript of
// nonsense, not an error.
func TestDecodeWAVAtTheTargetGeometryIsAnIdentity(t *testing.T) {
	data := sinePCM16(targetSampleRate, 0.25)
	path := writeWAV(t, wavFormatPCM, 1, targetSampleRate, 16, data)
	samples, ms, err := decodeWAV(mustRead(t, path))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(samples) != len(data) {
		t.Fatalf("decoded %d bytes, want %d", len(samples), len(data))
	}
	for i := range data {
		if samples[i] != data[i] {
			t.Fatalf("byte %d changed: %02x → %02x", i, data[i], samples[i])
		}
	}
	if ms != 250 {
		t.Errorf("duration = %dms, want 250", ms)
	}
}

// TestDecodeWAVDownmixesAndResamples: a 48 kHz stereo clip (the capture shape)
// must come out 16 kHz mono, and the LENGTH must follow the resample ratio —
// the bug that shipped in the first version of the send loop was exactly this
// conversion missing.
func TestDecodeWAVDownmixesAndResamples(t *testing.T) {
	stereo := sinePCM16(captureSampleRate, 0.5)
	data := make([]byte, len(stereo)*2)
	copy(data, stereo)
	copy(data[len(stereo):], stereo)
	path := writeWAV(t, wavFormatPCM, 2, captureSampleRate, 16, data)
	samples, ms, err := decodeWAV(mustRead(t, path))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := captureSampleRate / 2 * targetSampleRate / captureSampleRate * 2
	if len(samples) != want {
		t.Errorf("decoded %d bytes, want %d (0.5s at %d Hz)", len(samples), want, targetSampleRate)
	}
	if ms != 500 {
		t.Errorf("duration = %dms, want 500", ms)
	}
}

// TestDecodeWAVReadsAnExtensibleFloatChunk: the extensible fmt tag hides the
// real one 16 bytes in; reading 0xFFFE as a codec refuses an ordinary clip.
func TestDecodeWAVReadsAnExtensibleFloatChunk(t *testing.T) {
	const n = 1600
	data := make([]byte, n*4)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(float32(math.Sin(float64(i)/10))*0.5))
	}
	// Build the file by hand: an extensible fmt chunk of 40 bytes whose first
	// two extension bytes carry the real tag (IEEE float).
	var b strings.Builder
	b.WriteString(wavRIFFMagic)
	writeUint32(t, &b, uint32(4+8+40+8+len(data)))
	b.WriteString(wavWAVEMagic)
	b.WriteString(wavFmtChunkID)
	writeUint32(t, &b, 40) // 16 base + 22 extension + 2 pad
	writeUint16(t, &b, wavFormatExtensible)
	writeUint16(t, &b, 1)
	writeUint32(t, &b, targetSampleRate)
	writeUint32(t, &b, targetSampleRate*4)
	writeUint16(t, &b, 4)
	writeUint16(t, &b, 32)
	// The extension: cbSize, valid bits, channel mask, then the 16-byte
	// SubFormat GUID whose first two bytes carry the real format tag.
	writeUint16(t, &b, wavExtensibleTailBytes)
	writeUint16(t, &b, 32)
	writeUint32(t, &b, 3)
	writeUint16(t, &b, wavFormatIEEEFloat)
	b.Write([]byte{0, 0, 0, 0, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}) // GUID tail
	// 16 base + 2 cbSize + 22 extension (the GUID included) = 40; the
	// decoder's own tag offset must land on the GUID's first two bytes.
	if got := wavBaseFmtBytes + 2 + wavExtensibleTailBytes; got != 40 {
		t.Fatalf("the fixture's fmt chunk is %d bytes, want 40", got)
	}
	if wavExtensibleSubFormatOffset != 8 {
		t.Fatalf("the sub-format GUID offset is %d, want 8", wavExtensibleSubFormatOffset)
	}
	b.WriteString(wavDataChunkID)
	writeUint32(t, &b, uint32(len(data)))
	path := filepath.Join(t.TempDir(), "float.wav")
	if err := os.WriteFile(path, append([]byte(b.String()), data...), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	samples, _, err := decodeWAV(mustRead(t, path))
	if err != nil {
		t.Fatalf("decode an extensible float WAV: %v", err)
	}
	if len(samples) != n*2 {
		t.Errorf("decoded %d bytes, want %d", len(samples), n*2)
	}
}

// TestDecodeWAVRefusesWhatItCannotDecode, by NAME: an ADPCM/µ-law clip and a
// non-WAV file are both refused with a message that says what was seen — the
// bytes are never guessed at or transcoded silently.
func TestDecodeWAVRefusesWhatItCannotDecode(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		muster string
	}{
		{"a non-WAV file", writeRawFile(t, "notawav.mp3", []byte("ID3\x04\x00\x00")), "RIFF/WAVE"},
		{"an unsupported codec", writeWAV(t, 0x11 /* IMA ADPCM */, 1, targetSampleRate, 4, []byte{1, 2, 3, 4}), "not one the streaming channel decodes"},
	}
	for _, c := range cases {
		_, _, err := decodeWAV(mustRead(t, c.path))
		if err == nil {
			t.Errorf("%s: accepted silently", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.muster) {
			t.Errorf("%s: message %q does not name the reason (%q)", c.name, err.Error(), c.muster)
		}
	}
}

func writeRawFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestChunkSizingIsBytesNotSamples is the regression guard for the defect that
// made a 10.3 s clip stream for 20.7 s: the send loop steps the BYTE buffer, so
// the packet length must be a byte count — a sample count halved every packet
// and doubled the stream.
func TestChunkSizingIsBytesNotSamples(t *testing.T) {
	if got, want := chunkSampleCount(DefaultChunkMillis)*bytesPerSample16, targetSampleRate*DefaultChunkMillis/1000*2; got != want {
		t.Errorf("one packet carries %d bytes, want %d (200ms of 16kHz PCM16)", got, want)
	}
	if got := bytesPerSample16; got != 2 {
		t.Errorf("bytesPerSample16 = %d, want 2", got)
	}
	// A zero/garbage cadence resolves to the named default rather than to a
	// zero-size packet (which would send nothing and end the stream instantly).
	if got := chunkMillisOrDefault(0); got != DefaultChunkMillis {
		t.Errorf("chunkMillisOrDefault(0) = %d, want %d", got, DefaultChunkMillis)
	}
	if got := chunkMillisOrDefault(-5); got != DefaultChunkMillis {
		t.Errorf("chunkMillisOrDefault(-5) = %d, want %d", got, DefaultChunkMillis)
	}
	if got := chunkMillisOrDefault(100); got != 100 {
		t.Errorf("chunkMillisOrDefault(100) = %d, want 100", got)
	}
}

// TestLoadAudioNamesWhatIsMissing: a path that is not there and a time-out of
// the size guard are both reported with the path, never as a silent empty
// stream.
func TestLoadAudioNamesWhatIsMissing(t *testing.T) {
	if _, _, err := loadAudio(Options{AudioFile: filepath.Join(t.TempDir(), "absent.wav")}); err == nil {
		t.Error("a missing audio file was accepted")
	}
	if _, _, err := loadAudio(Options{}); err == nil {
		t.Error("an empty audio path was accepted")
	}
}

// TestPCM16RenderingClampsInsteadOfWrapping: an over-range sample must clamp.
// A wrap turns a loud passage into a burst of noise in the transcript.
func TestPCM16RenderingClampsInsteadOfWrapping(t *testing.T) {
	out := pcm16Bytes([]float32{2.0, -2.0, 0, 0.5})
	if v := int16(binary.LittleEndian.Uint16(out[0:2])); v != math.MaxInt16 {
		t.Errorf("+2.0 rendered as %d, want %d", v, math.MaxInt16)
	}
	if v := int16(binary.LittleEndian.Uint16(out[2:4])); v != math.MinInt16 {
		t.Errorf("-2.0 rendered as %d, want %d", v, math.MinInt16)
	}
}

// ── the credential file ────────────────────────────────────────────────

// TestLoadCredentialsReadsKeyValueLines pins the operator's own file format.
func TestLoadCredentialsReadsKeyValueLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.env")
	body := "# a comment\n\nAPPID=5210362471\nTOKEN=abc-123\nSECRET=not-read\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cred, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cred.AppID != "5210362471" || cred.Token != "abc-123" {
		t.Errorf("loaded %+v", cred)
	}
	if cred.Path != path {
		t.Errorf("path = %q, want %q", cred.Path, path)
	}
}

// TestLoadCredentialsErrorsNameThePathAndTheKeyNeverAValue is the credential
// discipline as a test: every failure message may quote the path and a key
// NAME, and must never quote a value. The file below carries a recognisable
// token, so a leak is detectable rather than a matter of reading carefully.
func TestLoadCredentialsErrorsNameThePathAndTheKeyNeverAValue(t *testing.T) {
	const secret = "SUPER-SECRET-TOKEN-VALUE"
	cases := []struct {
		name string
		body string
	}{
		{"no APPID", "TOKEN=" + secret + "\n"},
		{"no TOKEN", "APPID=1\n"},
		{"a line without a separator", "APPID=1\nTHIS-LINE-IS-" + secret + "\n"},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "v2.env")
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := LoadCredentials(path)
		if err == nil {
			t.Errorf("%s: accepted silently", c.name)
			continue
		}
		msg := err.Error()
		if strings.Contains(msg, secret) {
			t.Errorf("%s: the error leaks the credential VALUE: %q", c.name, msg)
		}
		if !strings.Contains(msg, "v2.env") {
			t.Errorf("%s: the error does not name the file it read: %q", c.name, msg)
		}
	}
	if _, err := LoadCredentials(filepath.Join(t.TempDir(), "absent.env")); err == nil {
		t.Error("a missing credential file was accepted")
	}
}

// TestCheckCredentialFileModeReportsAWorldReadableFile: the announcement the
// brief asks for — a credential file wider than 0600 is NAMED (path and mode),
// and a correctly restricted one is silent.
func TestCheckCredentialFileModeReportsAWorldReadableFile(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose.env")
	if err := os.WriteFile(loose, []byte("APPID=1\nTOKEN=2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	note := CheckCredentialFileMode(loose)
	if note == "" {
		t.Fatal("a 0644 credential file was not reported")
	}
	if !strings.Contains(note, loose) || !strings.Contains(note, "0644") {
		t.Errorf("the note does not name the path and the mode: %q", note)
	}
	tight := filepath.Join(dir, "tight.env")
	if err := os.WriteFile(tight, []byte("APPID=1\nTOKEN=2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if note := CheckCredentialFileMode(tight); note != "" {
		t.Errorf("a 0600 credential file was reported: %q", note)
	}
	if note := CheckCredentialFileMode(filepath.Join(dir, "absent.env")); note != "" {
		t.Errorf("a missing file produced a mode note: %q", note)
	}
}

// TestDefaultCredentialPathIsTheConventionalLocation pins the default, so the
// help text and the loader cannot drift apart.
func TestDefaultCredentialPathIsTheConventionalLocation(t *testing.T) {
	got := DefaultCredentialPath("/home/someone")
	want := filepath.Join("/home/someone", DefaultCredentialDir, DefaultCredentialFile)
	if got != want {
		t.Errorf("DefaultCredentialPath = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "/.doubao/v2_voice.env") {
		t.Errorf("the default is not the documented path: %q", got)
	}
}

// TestParsePositiveIntFallsBackToTheDefault: a junk cadence knob is the named
// default, never a zero-size chunk.
func TestParsePositiveIntFallsBackToTheDefault(t *testing.T) {
	cases := map[string]int{"": DefaultChunkMillis, "0": DefaultChunkMillis, "-1": DefaultChunkMillis,
		"abc": DefaultChunkMillis, "100": 100}
	for in, want := range cases {
		if got := ParsePositiveInt(in, DefaultChunkMillis); got != want {
			t.Errorf("ParsePositiveInt(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestNamedDefaultsAreTheProtocolsValues: the endpoint, cluster and workflow
// are wire values, so a typo in one is a refused request several layers away.
func TestNamedDefaultsAreTheProtocolsValues(t *testing.T) {
	if DefaultEndpoint != "wss://openspeech.bytedance.com/api/v2/asr" {
		t.Errorf("endpoint = %q", DefaultEndpoint)
	}
	if DefaultCluster != "volcengine_streaming_common" {
		t.Errorf("cluster = %q", DefaultCluster)
	}
	if !strings.Contains(DefaultWorkflow, "decode") || !strings.Contains(DefaultWorkflow, "itn") {
		t.Errorf("workflow = %q", DefaultWorkflow)
	}
	for _, v := range []string{endpointOrDefault(""), clusterOrDefault(""), workflowOrDefault(""), uidOrDefault("")} {
		if v == "" {
			t.Errorf("a resolver returned an empty default")
		}
	}
	if got := endpointOrDefault(" wss://example/x "); got != "wss://example/x" {
		t.Errorf("endpointOrDefault did not trim: %q", got)
	}
	if got := clusterOrDefault("volcengine_streaming_common"); got != DefaultCluster {
		t.Errorf("clusterOrDefault changed a value: %q", got)
	}
}

// TestReqIDIsUUIDShaped: the request identifier is what the vendor's support
// traces a call by, so its shape is part of the contract.
func TestReqIDIsUUIDShaped(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		id := newReqID()
		if len(id) != 36 || strings.Count(id, "-") != 4 {
			t.Fatalf("reqid %q is not a 36-char dashed identifier", id)
		}
		if id[14] != '4' {
			t.Errorf("reqid %q is not version 4", id)
		}
		if seen[id] {
			t.Fatalf("reqid %q repeated — identifiers must be unique per request", id)
		}
		seen[id] = true
	}
}

// TestPacketAssemblerMatchesTheProtocolGeometry pins the ONE derivation of a
// packet's geometry (client.go's PacketAssembler) against the protocol's own
// numbers, and against the expression the chat surface used to spell out for
// itself (cmd/agent: voiceLivePacketMillis × voiceCaptureSampleRate ×
// voiceCaptureBytesPerSample / 1000) — that second copy is what this type
// replaced, so the two must agree on the number they produce.
//
// THE VALUES ARE WRITTEN AS ARITHMETIC, not as 6400: a test that hard-codes the
// answer would pass while the derivation drifted. Rule 1's constants are what
// the assertion reads.
func TestPacketAssemblerMatchesTheProtocolGeometry(t *testing.T) {
	a := DefaultPacketAssembler()
	if want := DefaultSampleRate * DefaultChunkMillis / 1000 * bytesPerSample16; a.Bytes != want {
		t.Errorf("DefaultPacketAssembler().Bytes = %d, want %d (the protocol's own geometry)", a.Bytes, want)
	}
	if a.Millis != DefaultChunkMillis {
		t.Errorf("DefaultPacketAssembler().Millis = %d, want %d", a.Millis, DefaultChunkMillis)
	}
	// The derivation is the cadence knob's, not a constant: another cadence gets
	// another width, and the two fields move together.
	if b := NewPacketAssembler(100); b.Bytes != a.Bytes/2 || b.Millis != 100 {
		t.Errorf("NewPacketAssembler(100) = {%d bytes, %d ms}, want {%d, 100}", b.Bytes, b.Millis, a.Bytes/2)
	}
	// A zero or negative knob is the default, as every cadence knob here is.
	if z := NewPacketAssembler(0); z != a {
		t.Errorf("NewPacketAssembler(0) = %+v, want the default %+v", z, a)
	}
	// The end-of-stream packet is the SAME construction whichever arm sends it:
	// FinalPacket must set the last-packet header and nothing else.
	tail := make([]byte, a.Bytes/2)
	got, err := a.FinalPacket(tail)
	if err != nil {
		t.Fatalf("FinalPacket: %v", err)
	}
	want, err := buildAudioPacket(tail, true)
	if err != nil {
		t.Fatalf("buildAudioPacket: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("FinalPacket is not buildAudioPacket(tail, true):\n got % x\nwant % x", got[:headerBytes+sizeFieldBytes], want[:headerBytes+sizeFieldBytes])
	}
	if !bytes.HasPrefix(got, headerLastAudio[:]) {
		t.Errorf("FinalPacket's header is not the end-of-stream header: % x", got[:headerBytes])
	}
	// An EMPTY tail is still a packet — the flag is what ends the utterance.
	if _, err := a.FinalPacket(nil); err != nil {
		t.Errorf("FinalPacket(nil) must still send the end-of-stream packet: %v", err)
	}
}
