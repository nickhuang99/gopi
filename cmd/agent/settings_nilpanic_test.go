package main

// settings_nilpanic_test.go — regression test for P1-D
// (devdoc/review2-cli-config-options-variant-20260903.md): a malformed
// settings.json must NEVER nil-panic the agent. Before the fix, main.go's
// config-load path did
//
//	base, sErr := config.FromSettingsFile(settingsPath)
//	if sErr != nil {
//		fmt.Fprintf(os.Stderr, "[gopi] config: %v\n", sErr)  // prints, then CONTINUES
//	}
//	config.ApplyEnv(base, false)  // base is NIL on parse error → nil-pointer panic
//
// FromSettingsFile returns (nil, error) on a JSON parse failure, and
// ApplyEnv dereferences cfg fields without a nil check — so a user typo
// (trailing comma, bad JSON syntax) in settings.json crashed with
// `panic: invalid memory address or nil pointer dereference` instead of a
// clean error. The fix: hard-exit with an actionable message naming the
// settings path and the JSON problem (the provider surface — api_key /
// base_url / model — must never silently fall back to empty defaults,
// which would only fail later at the API call). This test drives the REAL
// binary through main's config path and asserts: clean non-zero exit,
// parse error + settings path on stderr, and NO panic trace.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMalformedSettingsNoPanic is the P1-D regression gate: run the built
// gopi binary with --settings pointing at a syntactically broken
// settings.json (trailing comma) and a real prompt, so the agent path's
// config-load block executes. The run must exit non-zero with a clean
// [gopi] config error naming the file — and must NOT die with a Go nil
// pointer panic (exit code 2 + "panic:" on stderr was the pre-fix crash).
func TestMalformedSettingsNoPanic(t *testing.T) {
	bin := gopiTestBinPath(t)

	// Scrubbed env: ambient GOPI_*/ANTHROPIC_*/DEEPSEEK_* vars would
	// redirect settings resolution (GOPI_SETTINGS) or leak real credentials
	// into the parse attempt — drop them all, keep HOME/PATH.
	drop := func(k string) bool {
		return strings.HasPrefix(k, "GOPI_") ||
			strings.HasPrefix(k, "ANTHROPIC_") ||
			strings.HasPrefix(k, "DEEPSEEK_")
	}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop(kv[:i]) {
			continue
		}
		env = append(env, kv)
	}

	// Malformed settings.json: a trailing comma after the last key is the
	// classic user typo — valid JSON keys, invalid JSON syntax.
	badPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(badPath, []byte(`{"model": "x",}`), 0o644); err != nil {
		t.Fatalf("write malformed settings: %v", err)
	}

	cmd := exec.Command(bin, "--settings", badPath, "say hi")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	stderr := string(out)

	// Whatever the outcome, it must never be a nil-pointer panic — the
	// pre-fix signature was a raw Go panic trace + exit code 2.
	if strings.Contains(stderr, "panic:") {
		t.Fatalf("malformed settings.json caused a PANIC (pre-fix bug P1-D):\n%s", stderr)
	}
	// Must exit non-zero (the malformed file is rejected loudly), NOT run
	// on empty defaults and NOT succeed.
	if err == nil {
		t.Fatalf("expected non-zero exit for malformed settings.json, got success;\nstderr:\n%s", stderr)
	}
	// The failure must be a CLEAN, actionable error naming the settings
	// path and the JSON problem — never a silent fallback.
	for _, want := range []string{badPath, "config:", "failed to parse"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q (expected the settings error to name path+problem):\n%s", want, stderr)
		}
	}
}
