package main

// home_settings_test.go — regression tests for P0-1 (review/cli-config-options):
// the `--home X` flag must redirect the settings.json READ, not just the
// sessions/raw-logs/memory.db writes. Before the fix, main.go resolved the
// settings path BEFORE ApplyEnv injected the flag's GOPI_HOME into the process
// env, so `--home X` silently loaded the REAL ~/.gopi/settings.json (api_key,
// model, compute_nodes) into a sandboxed run — the flagship isolation knob
// leaked production credentials. GOPI_HOME env form always worked; the flag
// form did not.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sandboxMarker is the model value planted in the sandbox settings.json. If
// the settings read is truly redirected, the agent's session_start records
// this model (newAgent → deepseek.ResolveModel → DEEPSEEK_MODEL seeded from
// the sandbox settings) AND the API request carries it on the wire; if the
// read leaks to the real profile, both show the real profile's model. The
// marker never appears in the real ~/.gopi/settings.json.
const sandboxMarker = "SANDBOX-MODEL-MARKER"

// homeSettingsProbeResult carries the two observables of one probe run: the
// model recorded in the session_start event (what the agent resolved) and the
// model the mock server saw on the wire (what actually went out).
type homeSettingsProbeResult struct {
	sessionModel string
	wireModel    string
}

// writeSandboxSettings plants a settings.json with the marker model at
// root/.gopi/settings.json and returns root.
func writeSandboxSettings(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".gopi"), 0o755); err != nil {
		t.Fatalf("mkdir sandbox .gopi: %v", err)
	}
	blob, err := json.Marshal(map[string]string{"model": sandboxMarker})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gopi", "settings.json"), blob, 0o644); err != nil {
		t.Fatalf("write sandbox settings: %v", err)
	}
	return root
}

// probeEnv builds a SCRUBBED child environment for one gopi run: all ambient
// GOPI_*/ANTHROPIC_*/DEEPSEEK_* vars are dropped (a developer shell — or the
// harness running these tests — has GOPI_SETTINGS / ANTHROPIC_MODEL set,
// which would beat the sandbox settings resolution and fake the leak), then
// the test's controlled values are applied. HOME/PATH are kept so the binary
// and the mock client work.
func probeEnv(extra ...string) []string {
	drop := func(k string) bool {
		return strings.HasPrefix(k, "GOPI_") ||
			strings.HasPrefix(k, "ANTHROPIC_") ||
			strings.HasPrefix(k, "DEEPSEEK_")
	}
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop(kv[:i]) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// runHomeSettingsProbe runs the built gopi binary once with a scrubbed env
// and returns where its data landed plus the two model observables. The API
// key/base URL point at a local mock SSE server so the run is network-free
// and deterministic: a real session runs, so session_start (with the resolved
// model) is on disk and the wire request carries the model that actually went
// out. GOPI_HOME env is set to envHome ("" = leave it to --home); brew is
// disabled so the exit sweep never touches the network.
func runHomeSettingsProbe(t *testing.T, envHome string, args ...string) (dataRoot string, res homeSettingsProbeResult) {
	t.Helper()
	bin := gopiTestBinPath(t)

	// Local mock Claude SSE server: real request, no network. Reads the
	// request body's model field — the wire-level truth of which settings
	// file won the resolution.
	var wireModel string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		wireModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "ok")
	}))
	defer mock.Close()

	env := probeEnv(
		"DEEPSEEK_BASE_URL="+mock.URL,
		"DEEPSEEK_API_KEY=test-key",
		"GOPI_BREW_INTERVAL_SEC=0",
	)
	if envHome != "" {
		env = append(env, "GOPI_HOME="+envHome)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gopi %s failed: %v\noutput:\n%s", strings.Join(args, " "), err, out)
	}
	// The data root the run actually used: the --home flag value when
	// present (flag > env), else the GOPI_HOME env (a temp dir) — never the
	// real profile.
	dataRoot = ""
	for i, a := range args {
		switch {
		case a == "--home" && i+1 < len(args):
			dataRoot = args[i+1]
		case strings.HasPrefix(a, "--home="):
			dataRoot = strings.TrimPrefix(a, "--home=")
		}
	}
	if dataRoot == "" {
		dataRoot = envHome
	}
	return dataRoot, homeSettingsProbeResult{
		sessionModel: sandboxModelFromSession(t, dataRoot),
		wireModel:    wireModel,
	}
}

// sandboxModelFromSession reads the newest session JSONL under root/.gopi/
// sessions and returns the model field of its session_start event.
// Session-dir-unify S1: bare sessions write <uuid>/session.jsonl (a
// per-session directory), legacy flat files were <uuid>.jsonl — the scan
// covers both layouts and picks the newest by mod time.
func sandboxModelFromSession(t *testing.T, root string) string {
	t.Helper()
	sessDir := filepath.Join(root, ".gopi", "sessions")
	var newestPath string
	var newestMod time.Time
	check := func(p string) {
		fi, err := os.Stat(p)
		if err != nil {
			return
		}
		if fi.ModTime().After(newestMod) {
			newestMod = fi.ModTime()
			newestPath = p
		}
	}
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		t.Fatalf("read sessions dir under %s: %v", root, err)
	}
	if len(entries) == 0 {
		t.Fatalf("no session under %s — the run never started a session there", sessDir)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			check(filepath.Join(sessDir, e.Name()))
			continue
		}
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(sessDir, e.Name())
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, se := range subs {
			if !se.IsDir() && strings.HasSuffix(se.Name(), ".jsonl") {
				check(filepath.Join(sub, se.Name()))
			}
		}
	}
	if newestPath == "" {
		t.Fatalf("no session JSONL under %s — the run never started a session there", sessDir)
	}
	data, err := os.ReadFile(newestPath)
	if err != nil {
		t.Fatalf("read session %s: %v", newestPath, err)
	}
	var start struct {
		Model string `json:"model"`
	}
	// First line is session_start.
	first := strings.SplitN(string(data), "\n", 2)[0]
	if err := json.Unmarshal([]byte(first), &start); err != nil {
		t.Fatalf("parse session_start %q: %v", first, err)
	}
	return start.Model
}

// TestHomeFlagRedirectsSettingsRead is the P0-1 regression guard: `--home
// <sandbox>` with a marker model in the sandbox settings.json must pick up
// the marker. Pre-fix this read the REAL profile's settings (marker absent),
// silently leaking api_key / model / compute_nodes into the sandbox. The
// session_start model AND the wire model must both carry the marker, and the
// session must live under the sandbox root (not the real profile).
func TestHomeFlagRedirectsSettingsRead(t *testing.T) {
	sandbox := writeSandboxSettings(t, t.TempDir())

	// No GOPI_HOME env — the --home flag is the ONLY redirect.
	_, res := runHomeSettingsProbe(t, "", "--home", sandbox, "hello")

	if res.sessionModel != sandboxMarker {
		t.Fatalf("--home %s: session_start model = %q, want marker %q — settings read was NOT redirected (P0-1 leak)", sandbox, res.sessionModel, sandboxMarker)
	}
	if res.wireModel != sandboxMarker {
		t.Fatalf("--home %s: wire model = %q, want marker %q — model resolution leaked", sandbox, res.wireModel, sandboxMarker)
	}
}

// TestGopiHomeEnvStillWorks guards the OTHER half of the contract: the
// GOPI_HOME env form must keep working (it always did — this test pins it so
// the fix's reordering cannot regress it).
func TestGopiHomeEnvStillWorks(t *testing.T) {
	home := writeSandboxSettings(t, t.TempDir())

	dataRoot, res := runHomeSettingsProbe(t, home, "hello")

	if res.sessionModel != sandboxMarker {
		t.Fatalf("GOPI_HOME env form: session_start model = %q, want marker %q", res.sessionModel, sandboxMarker)
	}
	if res.wireModel != sandboxMarker {
		t.Fatalf("GOPI_HOME env form: wire model = %q, want marker %q", res.wireModel, sandboxMarker)
	}
	if dataRoot != home {
		t.Fatalf("data root = %q, want %q", dataRoot, home)
	}
}

// TestHomeEqualsFormStillRedirects — --home=DIR (the = spelling) must go
// through the same GopiHomePath path and redirect the settings read too.
func TestHomeEqualsFormStillRedirects(t *testing.T) {
	sandbox := writeSandboxSettings(t, t.TempDir())
	_, res := runHomeSettingsProbe(t, "", "--home="+sandbox, "hello")
	if res.sessionModel != sandboxMarker {
		t.Fatalf("--home=DIR: session model = %q, want %q", res.sessionModel, sandboxMarker)
	}
	if res.wireModel != sandboxMarker {
		t.Fatalf("--home=DIR: wire model = %q, want %q", res.wireModel, sandboxMarker)
	}
}

// TestHomeFlagSessionsUnderSandbox — the --home run's session must land under
// the sandbox root, never under the process HOME (write-side isolation is the
// other half of the --home contract; a settings-read fix must not regress it).
func TestHomeFlagSessionsUnderSandbox(t *testing.T) {
	sandbox := writeSandboxSettings(t, t.TempDir())
	dataRoot, _ := runHomeSettingsProbe(t, "", "--home", sandbox, "hello")
	if dataRoot != sandbox {
		t.Fatalf("dataRoot = %q, want sandbox %q", dataRoot, sandbox)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, ".gopi", "sessions")); err != nil {
		t.Fatalf("session missing under sandbox: %v", err)
	}
}

// TestHomeFlagBeatsEnv — when BOTH GOPI_HOME=envHome and --home sandbox are
// present, the FLAG must win (flag > env): the sandbox settings are read and
// the session lands under the sandbox. This pins the precedence the
// unconditional os.Setenv preserves (ApplyEnv(fromFlags=true) force-writes
// GOPI_HOME the same way).
func TestHomeFlagBeatsEnv(t *testing.T) {
	sandbox := writeSandboxSettings(t, t.TempDir())
	envHome := writeSandboxSettings(t, t.TempDir()) // different root, same marker

	dataRoot, res := runHomeSettingsProbe(t, envHome, "--home", sandbox, "hello")
	if dataRoot != sandbox {
		t.Fatalf("dataRoot = %q, want sandbox %q (flag must beat env)", dataRoot, sandbox)
	}
	if res.sessionModel != sandboxMarker {
		t.Fatalf("flag-form won but session model = %q, want %q", res.sessionModel, sandboxMarker)
	}
	// The env fallback root must NOT have gained a session.
	if _, err := os.Stat(filepath.Join(envHome, ".gopi", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("env fallback root gained a session — flag did not override env")
	}
}
