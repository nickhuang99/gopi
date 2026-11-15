// voice-zerodep PoC: prove gopi voice input can run with ZERO external files.
//
// Three build shapes (tags), all linked against a STATIC whisper.cpp:
//
//	(none)     — control: model read from a file path (today's shape)
//	embed      — model.bin embedded via //go:embed, handed to whisper directly
//	             (VOICE_POC_COPY=1 copies it to C memory first — see cgo note)
//	embedgz    — model.bin.gz embedded, gunzipped into C memory at init
package main

import (
	"fmt"
	"os"
	"time"
)

// whisperCtx is the opaque engine handle; the C preamble lives with the
// per-shape model source so each shape owns its own init path.

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: poc <16k-mono-s16le.wav>")
		os.Exit(2)
	}
	pcm, err := readWAV16kMonoS16(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "wav: %v\n", err)
		os.Exit(1)
	}
	lang := os.Getenv("VOICE_POC_LANG")
	if lang == "" {
		lang = "zh"
	}

	t0 := time.Now()
	ctx, shape, err := initEngine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}
	initMS := time.Since(t0).Milliseconds()

	t1 := time.Now()
	text, err := transcribe(ctx, pcm, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transcribe: %v\n", err)
		os.Exit(1)
	}
	asrMS := time.Since(t1).Milliseconds()

	fmt.Printf("shape=%s init_ms=%d asr_ms=%d audio_s=%.2f\n", shape, initMS, asrMS, float64(len(pcm))/16000.0)
	fmt.Printf("TEXT: %s\n", text)
	reportRSS()
}

// reportRSS prints peak RSS from /proc/self/status (VmHWM) — the honest
// number when comparing direct-rodata against copy-to-C.
func reportRSS() {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return
	}
	for _, line := range splitLines(string(b)) {
		if len(line) > 6 && (line[:6] == "VmHWM:" || line[:6] == "VmRSS:") {
			fmt.Printf("%s\n", line)
		}
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// readWAV16kMonoS16 decodes the exact contract the voice pipeline pins:
// 16000 Hz, mono, signed 16-bit little-endian, 44-byte canonical header.
func readWAV16kMonoS16(path string) ([]float32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < wavHeaderBytes {
		return nil, fmt.Errorf("too short (%d bytes)", len(b))
	}
	sampleRate := int(b[24]) | int(b[25])<<8 | int(b[26])<<16 | int(b[27])<<24
	channels := int(b[22]) | int(b[23])<<8
	bits := int(b[34]) | int(b[35])<<8
	if sampleRate != wantSampleRate || channels != 1 || bits != 16 {
		return nil, fmt.Errorf("need 16k mono s16le, got %d Hz %d ch %d bit", sampleRate, channels, bits)
	}
	raw := b[wavHeaderBytes:]
	n := len(raw) / 2
	f := make([]float32, n)
	for i := 0; i < n; i++ {
		v := int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8)
		f[i] = float32(v) / 32768.0
	}
	return f, nil
}

const (
	wavHeaderBytes = 44
	wantSampleRate = 16000
)

// textBufBytes bounds a transcript buffer; a 220 s zh clip produced ~2 KB of
// text, so 64 KiB is a wide margin that cannot truncate real speech (Rule 1:
// named, not inlined).
const textBufBytes = 64 << 10

// bytesUntilNUL returns the length of the C string stored in b.
func bytesUntilNUL(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return len(b)
}
