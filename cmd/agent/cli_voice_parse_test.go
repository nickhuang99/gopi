package main

// cli_voice_parse_test.go — boundary/error-path tests for the argument layer
// beyond what cli_voice_test.go already covers.
//
// The author's file covers the happy path plus the "obvious" rejects
// (chunk 0/-1/abc, unknown flag, missing audio, stdio+listen). The measured
// remaining gaps in parseVoiceArgs (90.3% before this file) were:
//
//	cli_voice.go:115-117  the flagset's own parse error is propagated verbatim
//	cli_voice.go:148-150  --models   (never exercised)
//	cli_voice.go:151-153  --runtime  (never exercised)
//	cli_voice.go:183-189  voiceDepsReady's encoder/llm branches (25% before)
//
// Each case below is a boundary the CLI can actually be handed, not a
// synthetic permutation.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestVoiceParseModelsAndRuntimeFlags covers the two flags the author's tests
// never touched (cli_voice.go:148-153) and the home-relative defaults they
// override (cli_voice.go:130-134).
func TestVoiceParseModelsAndRuntimeFlags(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"clip.wav", "--models", "/models", "--runtime", "/bin/engine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.models != "/models" {
		t.Errorf("models = %q, want /models", opts.models)
	}
	if opts.runtime != "/bin/engine" {
		t.Errorf("runtime = %q, want /bin/engine", opts.runtime)
	}

	// Defaults point at RUNG 1 of the deployment search — the gopi profile,
	// never the legacy $HOME/work pair: the parser's answer and the search's
	// first rung must be the same place, or a caller that never walks the search
	// reads a different directory than a real run (Rule 2, one resolution).
	home, herr := os.UserHomeDir()
	def, err := parseVoiceArgs([]string{"clip.wav"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if herr == nil {
		wantModels, wantRuntime, err := voiceProfileVoicePaths()
		if err != nil {
			t.Fatalf("resolve the profile: %v", err)
		}
		if def.models != wantModels {
			t.Errorf("default models = %q, want the profile's %q", def.models, wantModels)
		}
		if def.runtime != wantRuntime {
			t.Errorf("default runtime = %q, want the profile's %q", def.runtime, wantRuntime)
		}
		if legacy, _ := voiceLegacyModelPaths(home); def.models == legacy {
			t.Errorf("default models %q is still the legacy $HOME/work rung", def.models)
		}
		if !filepath.IsAbs(def.runtime) {
			t.Errorf("default runtime %q is not absolute — the engine would be resolved against the CWD", def.runtime)
		}
	}
}

// TestVoiceParseTrimsWhitespaceOnValues pins the deliberate TrimSpace calls
// (cli_voice.go:129, 138, 146, 149, 152). A shell-quoted value is a realistic
// input (`--listen ":7788 "`), and an untrimmed address would make ListenAndServe
// fail with a confusing message.
func TestVoiceParseTrimsWhitespaceOnValues(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--server", "--listen", "  :9999  ", "--models", " /m ", "--runtime", " /r "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.listen != ":9999" {
		t.Errorf("listen = %q, want :9999 (trimmed)", opts.listen)
	}
	if opts.models != "/m" || opts.runtime != "/r" {
		t.Errorf("models/runtime = %q/%q, want /m and /r (trimmed)", opts.models, opts.runtime)
	}

	// The audio positional is trimmed too.
	opts, err = parseVoiceArgs([]string{"  clip.mp3  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.audio != "clip.mp3" {
		t.Errorf("audio = %q, want clip.mp3 (trimmed)", opts.audio)
	}
}

// TestVoiceParseChunkBoundaries pins the ACCEPTED edge (chunk=1 is the
// smallest legal window) against the rejected zero — the boundary the
// author's table stops one short of.
func TestVoiceParseChunkBoundaries(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"clip.wav", "--chunk", "1"})
	if err != nil {
		t.Fatalf("chunk=1 must be accepted (smallest legal window): %v", err)
	}
	if opts.chunk != 1 {
		t.Errorf("chunk = %d, want 1", opts.chunk)
	}

	// Whitespace around the number is tolerated by the same TrimSpace.
	opts, err = parseVoiceArgs([]string{"clip.wav", "--chunk", " 7 "})
	if err != nil {
		t.Fatalf("--chunk \" 7 \" should be accepted: %v", err)
	}
	if opts.chunk != 7 {
		t.Errorf("chunk = %d, want 7", opts.chunk)
	}

	// Overflow is NOT a legal window: the parse must fail rather than wrap.
	if _, err := parseVoiceArgs([]string{"clip.wav", "--chunk", "99999999999999999999"}); err == nil {
		t.Error("an out-of-range chunk should be rejected")
	}
}

// TestVoiceParseUnknownFlagShape covers the two disjoint reject paths: the
// flagset's own error (cli_voice.go:115-117 — an empty flag name) and the
// package's membership check (cli_voice.go:119-121). Both must be errors:
// silently ignoring an unknown flag would mean a typo like `--chunnk 15`
// transcribes the whole file in one window — the measured degenerate mode.
func TestVoiceParseUnknownFlagShape(t *testing.T) {
	for _, args := range [][]string{
		{"clip.wav", "--k=v"},          // unknown flag WITH a value
		{"clip.wav", "--k v"},          // unknown flag in the space form
		{"clip.wav", "--helpers=true"}, // a sibling group's flag, not ours
		{"clip.wav", "--"},             // empty flag name → flagset error
	} {
		if _, err := parseVoiceArgs(args); err == nil {
			t.Errorf("parseVoiceArgs(%q) should fail", args)
		}
	}
}

// TestVoiceParseListenImpliesServer replaces the old "documented inconsistency"
// note: `--listen ADDR` alone now MEANS the server, because an address to listen
// on has no other meaning. The old contract (server stays false, the file branch
// takes the value and does nothing with it) was a no-op that could outlive the
// operator's intent — they asked for a server and got a file transcription.
func TestVoiceParseListenImpliesServer(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--listen", ":9999"})
	if err != nil {
		t.Fatalf("--listen alone must parse (it implies --server): %v", err)
	}
	if !opts.server {
		t.Fatal("--listen must imply --server — an address to listen on has no other meaning")
	}
	if opts.listen != ":9999" {
		t.Fatalf("listen = %q, want the given value", opts.listen)
	}
	if opts.capture {
		t.Fatal("--listen is a server shape; it must not also become the local capture default")
	}
	// A positional on a server shape is refused (it would be ignored while the
	// process stayed up).
	if _, err := parseVoiceArgs([]string{"clip.wav", "--listen", ":9999"}); err == nil {
		t.Fatal("--listen with an audio file should be refused: a server never reads that file")
	}
}

// TestVoiceParseStdioWithoutServer keeps the no-op contract symmetric for the
// other transport flag: --stdio alone parses, but it is NOT a server (it is the
// CLIENT's pipe spelling, `--client --seconds N --stdio`), and with no mode at
// all it is the bare CAPTURE writing f32 to stdout — which is why it parses.
func TestVoiceParseStdioWithoutServer(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"clip.wav", "--stdio"})
	if err != nil {
		t.Fatalf("--stdio alone parses: %v", err)
	}
	if opts.server {
		t.Fatal("--stdio must NOT imply --server")
	}
	if !opts.stdio {
		t.Fatal("stdio flag should be recorded")
	}
	// The bare `--stdio`: nothing was named, so the capture default applies and
	// its PCM goes to stdout (the client's own pipe shape).
	opts, err = parseVoiceArgs([]string{"--stdio"})
	if err != nil {
		t.Fatalf("bare --stdio must parse as the capture shape: %v", err)
	}
	if !opts.capture || !opts.stdio {
		t.Fatalf("bare --stdio: capture=%v stdio=%v, want both true", opts.capture, opts.stdio)
	}
	if opts.seconds != voiceLocalDefaultSeconds {
		t.Fatalf("bare --stdio seconds = %d, want the capture default %d", opts.seconds, voiceLocalDefaultSeconds)
	}
}

// TestVoiceDepsReadyWalksEveryDependency closes voiceDepsReady from 25% to
// full path coverage: it must name the FIRST missing item, in order
// (runtime → encoder → llm), so the operator is told the next thing to fix
// rather than a random one.
func TestVoiceDepsReadyWalksEveryDependency(t *testing.T) {
	dir := t.TempDir()

	// Every option names the LOCAL engine: the walker's order (runtime →
	// encoder → llm) is the on-machine engine's dependency list, and since
	// 2026-10-07 the terminal default is the cloud engine, which needs none of
	// them — an engine-less literal here would assert the wrong engine's
	// pre-flight (devdoc/doubao-asr-integration.md §3).
	local := config.AudioEngineLocal

	// 1. runtime missing → the runtime is named.
	opts := &voiceOptions{engine: local, runtime: filepath.Join(dir, "no-runtime"), models: filepath.Join(dir, "no-models")}
	msg := voiceDepsReady(opts)
	if !strings.Contains(msg, "runtime") {
		t.Fatalf("step 1: %q should name the runtime", msg)
	}

	// 2. runtime present, encoder missing → the ENCODER is named.
	runtime := filepath.Join(dir, "engine")
	if err := os.WriteFile(runtime, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	models := filepath.Join(dir, "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	opts = &voiceOptions{engine: local, runtime: runtime, models: models}
	msg = voiceDepsReady(opts)
	enc, _ := voiceModelPaths(models)
	if !strings.Contains(msg, "encoder") || !strings.Contains(msg, enc) {
		t.Fatalf("step 2: %q should name the encoder model %s", msg, enc)
	}

	// 3. encoder present, llm missing → the LLM is named.
	if err := os.WriteFile(enc, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg = voiceDepsReady(opts)
	_, llm := voiceModelPaths(models)
	if !strings.Contains(msg, "llm") || !strings.Contains(msg, llm) {
		t.Fatalf("step 3: %q should name the llm model %s", msg, llm)
	}

	// 4. everything present → the empty string (the success contract).
	if err := os.WriteFile(llm, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := voiceDepsReady(opts); msg != "" {
		t.Fatalf("all deps present should report %q, got %q", "", msg)
	}
}

// TestVoiceModelPathsJoinWithSlash keeps the engine's argv stable: the
// runtime takes these as literal paths, so a models dir with a trailing slash
// must not produce "//".
func TestVoiceModelPathsJoinWithSlash(t *testing.T) {
	enc, llm := voiceModelPaths("/models/")
	if strings.Contains(enc, "//") || strings.Contains(llm, "//") {
		t.Errorf("path join produced a double slash: %q %q", enc, llm)
	}
	if got := filepath.Base(enc); got != "funasr-encoder-f16.gguf" {
		t.Errorf("encoder basename = %q", got)
	}
	if got := filepath.Base(llm); got != "qwen3-0.6b-q4km.gguf" {
		t.Errorf("llm basename = %q", got)
	}
}

// TestVoiceFlagMemberBoundary pins the closed set of accepted flags
// (cli_voice.go:102-110) — including the empty name, which must NOT be a
// member (it reaches parseVoiceArgs only as the flagset's own error).
func TestVoiceFlagMemberBoundary(t *testing.T) {
	for _, f := range []string{"chunk", "server", "listen", "stdio", "models", "runtime"} {
		if !voiceFlagMember(f) {
			t.Errorf("voiceFlagMember(%q) = false, want true", f)
		}
	}
	for _, f := range []string{"", "help", "workers", "models-dir", "Chunk", "web_search"} {
		if voiceFlagMember(f) {
			t.Errorf("voiceFlagMember(%q) = true, want false", f)
		}
	}
}
