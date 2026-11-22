// Audio for the streaming channel, and the credential file format.
//
// THE AUDIO: the v2 endpoint takes SAMPLES, not a container — `format: raw`
// with an explicit rate/bits/channel — so a WAV must be decoded here rather
// than handed over as bytes (which is what the batch channel does). Only the
// codecs a WAV container actually carries are decoded: integer PCM 8/16/24/32
// and IEEE float 32/64. Anything else (ADPCM, µ-law, an mp3 or an ogg) is
// refused BY NAME, because resampling or transcoding it would be a second
// silent guess about what the operator meant.
//
// THE CREDENTIAL: `KEY=VALUE`, one per line — the format of the file the
// operator already keeps at ~/.doubao/v2_voice.env, not a second format
// invented here. This reader returns VALUES and never prints, logs or embeds
// them; the only thing any message may quote is a path or a key NAME.
package doubaostream

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ── credential file (KEY=VALUE, the operator's own format) ──────────────
const (
	// DefaultCredentialDir / DefaultCredentialFile are the conventional
	// location: $HOME/.doubao/v2_voice.env. The path is a DEFAULT, never a
	// requirement — every caller can name another file, and the knob that
	// names one is the key-FILE channel gopi already has
	// (config.AudioKeyFileEnv / --doubao-key-file / settings
	// "doubao_key_file"), not a new one.
	DefaultCredentialDir  = ".doubao"
	DefaultCredentialFile = "v2_voice.env"

	// credentialAppIDKey / credentialTokenKey are the KEY names read from that
	// file. The SECRET line the operator's file also carries is deliberately
	// NOT read: the v2 protocol was measured to need the app id and the access
	// token only, and reading a secret that nothing uses would be a credential
	// in memory for no reason.
	credentialAppIDKey = "APPID"
	credentialTokenKey = "TOKEN"

	// keyValueSeparator is the file's assignment operator.
	keyValueSeparator = "="

	// credentialFileMode is the permission this reader REQUIRES of a
	// credential file. A world-readable credential is the defect the mode
	// check exists to catch — and it is caught by REFUSING, not by silently
	// chmod-ing a file the operator owns (Rule 15: the refusal names the path
	// and the mode it wants).
	credentialFileMode = 0o600
)

// Credential is a loaded v2 credential. Its fields are never printed.
type Credential struct {
	// AppID / Token are the credential values.
	AppID string
	Token string
	// Path is the file they were read from — the ONLY thing a message may
	// quote about them.
	Path string
	// EnvVar names the knob that chose the path, so a refusal points at the
	// setting rather than at the engine.
	EnvVar string
}

// DefaultCredentialPath is $HOME/.doubao/v2_voice.env, assembled from its parts
// so the two name constants have one definition each.
func DefaultCredentialPath(home string) string {
	return filepath.Join(home, DefaultCredentialDir, DefaultCredentialFile)
}

// LoadCredentials reads a KEY=VALUE credential file. An unset path means the
// conventional location under home.
//
// The errors name the PATH and the KEY, never a value.
func LoadCredentials(path string) (Credential, error) {
	c := Credential{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read the streaming credential %q: %w", path, err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, keyValueSeparator)
		if !ok {
			// A malformed line is REPORTED with its line number and key name —
			// never with its content, which would put a credential in a log.
			return c, fmt.Errorf("%q line %d is not KEY%sVALUE (the key name is quoted, its value never is)",
				path, len(values)+1, keyValueSeparator)
		}
		values[strings.TrimSpace(strings.ToUpper(name))] = strings.TrimSpace(value)
	}
	c.AppID = values[credentialAppIDKey]
	c.Token = values[credentialTokenKey]
	if c.AppID == "" {
		return c, fmt.Errorf("%q carries no %s line", path, credentialAppIDKey)
	}
	if c.Token == "" {
		return c, fmt.Errorf("%q carries no %s line", path, credentialTokenKey)
	}
	return c, nil
}

// CheckCredentialFileMode reports the file's permission when it is wider than
// credentialFileMode, and "" when it is not — or when it cannot be stat'd
// (a missing file is LoadCredentials' report, not this one's). It exists so a
// caller can ANNOUNCE a world-readable credential instead of discovering it
// after an incident.
func CheckCredentialFileMode(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if perm := info.Mode().Perm(); perm&^os.FileMode(credentialFileMode) != 0 {
		return fmt.Sprintf("the streaming credential %q is mode %04o — it should be %04o (chmod %04o %s)",
			path, perm, credentialFileMode, credentialFileMode, path)
	}
	return ""
}

// ── WAV decoding ───────────────────────────────────────────────────────

// WAV format tags (the `fmt ` chunk's wFormatTag).
const (
	wavFormatPCM        = 1
	wavFormatIEEEFloat  = 3
	wavFormatExtensible = 0xFFFE
)

// WAV chunk identifiers and sizes.
const (
	wavRIFFMagic   = "RIFF"
	wavWAVEMagic   = "WAVE"
	wavFmtChunkID  = "fmt "
	wavDataChunkID = "data"
	// wavExtensibleTailBytes is the extra 22 bytes an extensible fmt chunk
	// carries after the base 16 (cbSize, wValidBitsPerSample, dwChannelMask,
	// and a 16-byte SubFormat GUID whose first two bytes ARE the real tag).
	wavExtensibleTailBytes = 22
	// wavExtensibleSubFormatOffset is where that GUID starts inside the
	// extension: past cbSize (2), wValidBitsPerSample (2) and dwChannelMask
	// (4). Reading the tag at the START of the extension instead returns
	// cbSize — 22 — which is how a 32-bit float clip came to be refused with
	// "format tag 22" (measured on this test's own fixture).
	wavExtensibleSubFormatOffset = 8
	wavBaseFmtBytes              = 16
	wavChunkHeaderBytes          = 8
	// targetSampleRate is the rate the service expects with format "raw".
	targetSampleRate = DefaultSampleRate
	// targetChannels is the channel count the service expects.
	targetChannels = DefaultChannel
)

// loadAudio turns the request's audio into 16 kHz mono PCM16 samples plus the
// audio's own duration in milliseconds.
func loadAudio(opts Options) ([]byte, int, error) {
	path := strings.TrimSpace(opts.AudioFile)
	if path == "" {
		return nil, 0, fmt.Errorf("no audio file named")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read the audio %q: %w", path, err)
	}
	if info.Size() > maxAudioBytes {
		return nil, 0, fmt.Errorf("audio %q is %d bytes, above the %d byte limit for one streaming request",
			path, info.Size(), maxAudioBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read the audio %q: %w", path, err)
	}
	if opts.AudioRawPCM {
		// Bare PCM: the caller states the geometry by choosing this flag. The
		// bytes are resampled to the service's rate rather than sent at
		// whatever rate the capture produced, because a rate the service does
		// not expect is not an error it can raise — it is a transcript of
		// nonsense, which is worse.
		return pcm16From(raw, captureSampleRate, targetSampleRate, 1),
			len(raw) / 2 * 1000 / captureSampleRate, nil
	}
	return decodeWAV(raw)
}

// captureSampleRate is the rate a bare PCM stream is assumed to carry (the
// capture client's own rate — see voicePCM16ToWAV's siblings).
const captureSampleRate = 48000

// ErrAudioNotWAVPCM is the sentinel the WAV decoder refuses with when the bytes
// are not a RIFF/WAVE container and not bare PCM either (an MP3, a FLAC, an Ogg
// — measured on a real MP3 run).
//
// WHY IT IS ITS OWN TYPE AND NOT JUST A SENTENCE (Rule 15, and Rule 43's
// criterion that a symbol is worth exporting when a caller OUTSIDE needs it): the
// refusal is the one failure whose BEST answer is another ENGINE rather than a
// retry. The batch channel (`--engine recording`) uploads the file as it is and
// decodes it on the vendor's side, so the same MP3 transcribes there and not
// here. This package knows the FORMAT but not the flag, so it reports the
// distinction and the CLI — which knows the engines — turns it into the route.
var ErrAudioNotWAVPCM = errors.New("the streaming channel takes a WAV or bare PCM")

// decodeWAV decodes a canonical WAV into 16 kHz mono PCM16.
func decodeWAV(raw []byte) ([]byte, int, error) {
	if len(raw) < wavChunkHeaderBytes || string(raw[0:4]) != wavRIFFMagic || string(raw[8:12]) != wavWAVEMagic {
		return nil, 0, fmt.Errorf("%w: these bytes are neither (no RIFF/WAVE header)", ErrAudioNotWAVPCM)
	}
	var (
		formatTag, channels, rate, bits int
		dataStart, dataLen              int
		haveFmt                         bool
	)
	for off := 12; off+wavChunkHeaderBytes <= len(raw); {
		id := string(raw[off : off+4])
		size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		body := off + wavChunkHeaderBytes
		switch id {
		case wavFmtChunkID:
			if size < wavBaseFmtBytes || body+wavBaseFmtBytes > len(raw) {
				return nil, 0, fmt.Errorf("the WAV fmt chunk is %d bytes, shorter than the %d byte base", size, wavBaseFmtBytes)
			}
			formatTag = int(binary.LittleEndian.Uint16(raw[body : body+2]))
			channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			rate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
			// An EXTENSIBLE fmt chunk hides the real tag in the first two
			// bytes of its extension — reading the 0xFFFE as a codec would
			// refuse a perfectly ordinary 32-bit float WAV.
			if formatTag == wavFormatExtensible && size >= wavBaseFmtBytes+wavExtensibleTailBytes {
				tagAt := body + wavBaseFmtBytes + wavExtensibleSubFormatOffset
				formatTag = int(binary.LittleEndian.Uint16(raw[tagAt : tagAt+2]))
			}
			haveFmt = true
		case wavDataChunkID:
			dataStart, dataLen = body, size
		}
		off = body + size + size%2 // chunks are word-aligned
	}
	if !haveFmt {
		return nil, 0, fmt.Errorf("the WAV carries no fmt chunk")
	}
	if dataLen == 0 {
		return nil, 0, fmt.Errorf("the WAV carries no audio data")
	}
	if dataStart+dataLen > len(raw) {
		dataLen = len(raw) - dataStart
	}
	if channels <= 0 || rate <= 0 {
		return nil, 0, fmt.Errorf("the WAV declares %d channel(s) at %d Hz", channels, rate)
	}
	samples, err := decodeSamples(raw[dataStart:dataStart+dataLen], formatTag, bits)
	if err != nil {
		return nil, 0, err
	}
	// Downmix and resample. A clip already in the target geometry passes
	// through the same two steps, which are identities there (one channel,
	// equal rates).
	mono := toMono(samples, channels)
	return resamplePCM16(mono, rate, targetSampleRate), len(mono) * 1000 / rate, nil
}

// decodeSamples turns a data chunk's bytes into float32 samples in [-1,1].
func decodeSamples(data []byte, formatTag, bits int) ([]float32, error) {
	switch formatTag {
	case wavFormatPCM:
		switch bits {
		case 8:
			out := make([]float32, len(data))
			for i, b := range data {
				out[i] = float32(int(b)-128) / 128
			}
			return out, nil
		case 16:
			out := make([]float32, len(data)/2)
			for i := range out {
				out[i] = float32(int16(binary.LittleEndian.Uint16(data[i*2:]))) / 32768
			}
			return out, nil
		case 24:
			out := make([]float32, len(data)/3)
			for i := range out {
				v := int32(data[i*3]) | int32(data[i*3+1])<<8 | int32(data[i*3+2])<<16
				if v&0x800000 != 0 {
					v |= ^0xFFFFFF
				}
				out[i] = float32(v) / 8388608
			}
			return out, nil
		case 32:
			out := make([]float32, len(data)/4)
			for i := range out {
				out[i] = float32(int32(binary.LittleEndian.Uint32(data[i*4:]))) / 2147483648
			}
			return out, nil
		}
	case wavFormatIEEEFloat:
		switch bits {
		case 32:
			out := make([]float32, len(data)/4)
			for i := range out {
				out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
			}
			return out, nil
		case 64:
			out := make([]float32, len(data)/8)
			for i := range out {
				out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(data[i*8:])))
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("the WAV's format tag %d with %d bits per sample is not one the streaming channel decodes "+
		"(integer PCM 8/16/24/32 or IEEE float 32/64); the bytes are refused rather than transcoded", formatTag, bits)
}

// toMono averages the channels (an identity at one channel).
func toMono(samples []float32, channels int) []float32 {
	if channels <= 1 {
		return samples
	}
	n := len(samples) / channels
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		var sum float32
		for c := 0; c < channels; c++ {
			sum += samples[i*channels+c]
		}
		out[i] = sum / float32(channels)
	}
	return out
}

// resamplePCM16 linearly resamples mono float samples to the target rate and
// renders 16-bit little-endian PCM. Linear interpolation is deliberate: the
// service resamples and runs VAD/feature extraction itself, and the measured
// quality on real speech is unchanged (the probe streamed at the target rate
// directly; a clip at 48 kHz goes through this and transcribes correctly).
func resamplePCM16(in []float32, from, to int) []byte {
	if from == to || len(in) == 0 {
		return pcm16Bytes(in)
	}
	n := len(in) * to / from
	out := make([]float32, n)
	ratio := float64(from) / float64(to)
	for i := 0; i < n; i++ {
		pos := float64(i) * ratio
		i0 := int(pos)
		frac := float32(pos - float64(i0))
		s0 := in[i0]
		s1 := s0
		if i0+1 < len(in) {
			s1 = in[i0+1]
		}
		out[i] = s0 + (s1-s0)*frac
	}
	return pcm16Bytes(out)
}

// pcm16From decodes bare PCM16 bytes at `from` Hz and renders them at `to` Hz.
func pcm16From(raw []byte, from, to, channels int) []byte {
	samples := make([]float32, len(raw)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
	}
	return resamplePCM16(toMono(samples, channels), from, to)
}

// pcm16Bytes clamps and renders float samples as little-endian PCM16.
func pcm16Bytes(in []float32) []byte {
	out := make([]byte, len(in)*2)
	for i, s := range in {
		v := int32(math.Round(float64(s) * 32767))
		if v > math.MaxInt16 {
			v = math.MaxInt16
		}
		if v < math.MinInt16 {
			v = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(v)))
	}
	return out
}

// bytesPerSample16 is the width of the PCM16 samples this client sends: the
// ONE place a sample count becomes a byte count.
const bytesPerSample16 = 2

// chunkMillisOrDefault resolves the packet cadence.
func chunkMillisOrDefault(chunkMillis int) int {
	if chunkMillis <= 0 {
		return DefaultChunkMillis
	}
	return chunkMillis
}

// chunkSampleCount is how many PCM16 SAMPLES one packet carries (multiply by
// bytesPerSample16 for the slice length — the two are not interchangeable).
func chunkSampleCount(chunkMillis int) int {
	chunkMillis = chunkMillisOrDefault(chunkMillis)
	n := targetSampleRate * chunkMillis / 1000
	if n < 1 {
		n = 1
	}
	return n
}

// randRead is the package's single source of randomness, a seam so a test can
// prove the reqid builder's fallback without depending on the platform.
var randRead = rand.Read

// ── resolution helpers (named defaults, one place each) ────────────────

func endpointOrDefault(v string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return DefaultEndpoint
}

func clusterOrDefault(v string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return DefaultCluster
}

func workflowOrDefault(v string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return DefaultWorkflow
}

func uidOrDefault(v string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return DefaultUID
}

func (o Options) readTimeout() time.Duration {
	if o.ReadTimeout > 0 {
		return o.ReadTimeout
	}
	return defaultReadTimeout
}

// defaultLanguage is the recognition language asked for; the service is the
// Chinese streaming cluster, and the field is optional in the schema.
const defaultLanguage = "zh-CN"

// ParsePositiveInt returns def for anything that is not a positive integer —
// the reading rule every packet-cadence knob shares (a bad value is the named
// default, never a zero-size chunk).
func ParsePositiveInt(v string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}
