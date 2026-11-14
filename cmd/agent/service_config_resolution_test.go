package main

// service_config_resolution_test.go — the three LONG-RUNNING SERVICE faces
// resolve the configuration.
//
// THE DEFECT THESE GATES PIN (measured 2026-11-12 on this host; the same cause
// as `gopi chat`'s, fixed in 6fd8b134). `gopi web`, `gopi cli` and `gopi ui`
// all reach an agent WITHOUT ever calling resolveRunConfig, because main()
// resolves the configuration AFTER the subcommand chain and each of these faces
// exits from INSIDE that chain. config.ApplyEnv therefore never ran on any of
// the three paths, so not one settings.json knob reached the process
// environment — and internal/agent (which reads reasoning_effort / thinking
// from the ENVIRONMENT only, defaulting to thinking=disabled) sent every
// request with thinking off.
//
// WHY THE GATES DRIVE THE REAL ENTRY POINTS. The defect was never "the resolver
// misbehaves" — it was "the resolver is not on this face's path". A unit test of
// resolveServiceRunConfig was green the whole time the defect was live, exactly
// as the chat face's resolver was. So each gate calls the face's own run
// function (runWeb / runTermChat / runUIChat) with argv a real operator would
// type, and reads the process environment afterwards.
//
// WHY THE RUN IS STOPPED AT THE LISTENER. These faces SERVE until told to stop,
// so a test cannot simply "let it finish" — but it does not have to: the
// assertion is about the LAUNCH PATH, and the launch path is over the moment
// the configuration has been resolved. Every gate therefore hands the face an
// address that is ALREADY BOUND (the test holds the listener), which makes the
// face fail at `net.Listen` with its ordinary exit status — AFTER the
// resolution, BEFORE any agent exists. That the environment is populated on a
// run that dies at the listener is itself the evidence for the position the
// production comment claims: the resolution is above the listener, not inside
// the agent.
//
// DO NOT re-derive this with /proc/<pid>/environ: it is an execve snapshot and
// cannot show a runtime publication (measured: a Setenv'd variable is absent
// from /proc/self/environ while os.Getenv returns it). os.Getenv is what
// internal/agent itself reads, and it is what these gates assert.

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// serviceConfigTestSettings is the settings document the gates put in a
// THROWAWAY profile. It carries the knobs the defect dropped, the provider
// surface around them, and a doubao credential whose NON-publication is the
// one deliberate exception that must survive the fix.
const serviceConfigTestSettings = `{
  "reasoning_effort": "high",
  "thinking_budget": 16384,
  "model": "service-config-test-model",
  "base_url": "https://service-config-test.example/anthropic",
  "api_key": "` + serviceConfigTestAPIKey + `",
  "doubao_api_key": "not-a-real-key"
}`

// serviceConfigTestAPIKey is the PROVIDER credential the settings document
// carries. It is published on purpose (the assertions below read it back), so
// the value is a fixture, never a real key.
const serviceConfigTestAPIKey = "service-config-test-provider-key"

// serviceConfigTestProfile builds a throwaway gopi profile holding the settings
// document above and pins GOPI_HOME at it, so the run resolves the REAL default
// settings path (<GOPI_HOME>/.gopi/settings.json) — no --settings flag, nothing
// the production resolution would not do by itself. The path comes from the ONE
// resolver (config.DefaultSettingsPath), so this fixture cannot disagree with
// the reader under test about where the file lives.
func serviceConfigTestProfile(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("GOPI_MEMORY_DB", "")

	path := config.DefaultSettingsPath()
	if path == "" {
		t.Fatal("config.DefaultSettingsPath() = \"\" — the profile cannot be materialised without a settings path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(serviceConfigTestSettings), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// clearServiceConfigTestEnv UNSETS every variable a service run may decide, so
// the settings layer is the channel under test rather than an export inherited
// from whatever started the suite. Restored through testutil.SnapshotEnv —
// Rule 17's sanctioned per-call pair, because t.Setenv cannot express "unset".
func clearServiceConfigTestEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GOPI_REASONING_EFFORT",
		"GOPI_THINKING_BUDGET",
		"GOPI_THINKING",
		"GOPI_SETTINGS",
		"ANTHROPIC_AUTH_TOKEN",
		"DEEPSEEK_API_KEY",
		"ANTHROPIC_MODEL",
		"DEEPSEEK_MODEL",
		"ANTHROPIC_BASE_URL",
		"DEEPSEEK_BASE_URL",
		config.DoubaoAPIKeyEnv,
		config.AudioAPIKeyEnv,
	} {
		t.Cleanup(testutil.SnapshotEnv(t, k))
		if err := os.Unsetenv(k); err != nil { // env-leak-ok: testutil.SnapshotEnv above restores this on test return, ABSENCE included — the case's SUBJECT is the variable being absent, which t.Setenv cannot express (it only ever sets a present value)
			t.Fatalf("unset %s: %v", k, err)
		}
	}
}

// serviceConfigBoundAddr returns an address whose port the TEST already holds,
// so the face under test reaches its listener and fails there. The listener is
// closed with the test.
func serviceConfigBoundAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port for the refusal: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //stylegate:ignore closing a listener this test opened; its failure carries no state the assertions read
	return ln.Addr().String()
}

// assertServiceConfigPublished is the shared body of the three gates: the
// resolution must have published the settings' knobs (the two the defect
// dropped, then the provider surface the same broken path also lost), the
// provider credential must be present, and the doubao/audio credentials must
// still be absent.
func assertServiceConfigPublished(t *testing.T, face string) {
	t.Helper()

	// THE EXACT ACCEPTANCE CRITERION, read from this process's own environment
	// (os.Getenv is what internal/agent reads when it assembles a request).
	if got, want := os.Getenv("GOPI_REASONING_EFFORT"), "high"; got != want {
		t.Errorf("%s: GOPI_REASONING_EFFORT = %q, want %q — this face did not publish the settings' reasoning_effort, so internal/agent sends no effort at all and thinking stays at its disabled default", face, got, want)
	}
	if got, want := os.Getenv("GOPI_THINKING_BUDGET"), "16384"; got != want {
		t.Errorf("%s: GOPI_THINKING_BUDGET = %q, want %q — this face did not publish the settings' thinking_budget", face, got, want)
	}

	// THE REST OF THE PUBLICATION CHAIN: the defect was never "two knobs went
	// missing" — ApplyEnv did not run AT ALL here, so every settings key was
	// lost and reasoning_effort was only the first casualty found. A regression
	// that republished just the two above would pass while the operator's model
	// and endpoint stayed unreachable from this surface.
	for _, c := range []struct{ name, want, why string }{
		{"ANTHROPIC_MODEL", "service-config-test-model", "the request would name the code default instead of the configured model"},
		{"DEEPSEEK_MODEL", "service-config-test-model", "the legacy spelling of the same knob must agree with the one above"},
		{"ANTHROPIC_BASE_URL", "https://service-config-test.example/anthropic", "the request would go to the code default endpoint"},
	} {
		if got := os.Getenv(c.name); got != c.want {
			t.Errorf("%s: %s = %q, want %q — %s", face, c.name, got, c.want, c.why)
		}
	}

	// THE TWO HALVES OF THE CREDENTIAL QUESTION, asserted together because they
	// are ONE decision split in two: config.ApplyEnv publishes the PROVIDER
	// credential (the child needs it to answer) and withholds the DOUBAO one
	// (the child never needs it, and the environment is inherited by every
	// process the run spawns). Pinning both directions is what stops a later
	// edit "simplifying" either one.
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "DEEPSEEK_API_KEY"} {
		if got := os.Getenv(name); got != serviceConfigTestAPIKey {
			t.Errorf("%s: %s = %q, want the settings' provider key %q — this face must publish the credential its own requests need", face, name, got, serviceConfigTestAPIKey)
		}
	}
	for _, name := range []string{config.DoubaoAPIKeyEnv, config.AudioAPIKeyEnv} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s: %s = %q, want it UNSET — resolving the settings into this face's environment widened the credential's copy", face, name, v)
		}
	}
}

// TestWebServiceConfigResolutionPublishesTheResolvedConfiguration is `gopi
// web`'s gate: a web run started with a settings file and NO environment of its
// own must come out of runWeb with the settings' knobs published.
func TestWebServiceConfigResolutionPublishesTheResolvedConfiguration(t *testing.T) {
	serviceConfigTestProfile(t)
	clearServiceConfigTestEnv(t)

	// A REAL operator command line: the surface's own switches are in it, so
	// the reduction (catalogConfigArgs) is exercised by the very argv the gate
	// drives — a face whose argv was handed to FlagOverrides unreduced would
	// fail here with "unknown flag --tools", the wrong refusal about the wrong
	// command line.
	if code := runWeb([]string{webToolsFlag, "--addr=" + serviceConfigBoundAddr(t)}); code != 1 {
		t.Fatalf("runWeb on an occupied address = %d, want 1 (its ordinary listen refusal) — the assertions below are about a run that must have reached the listener", code)
	}
	assertServiceConfigPublished(t, "gopi web")
}

// TestTermServiceConfigResolutionPublishesTheResolvedConfiguration is `gopi
// cli`'s gate, driven through runTermChat.
func TestTermServiceConfigResolutionPublishesTheResolvedConfiguration(t *testing.T) {
	serviceConfigTestProfile(t)
	clearServiceConfigTestEnv(t)
	// The text browser is a SURFACE capability, so it must be satisfiable here:
	// without it the face refuses before the listener and the gate would be
	// measuring the browser probe instead of the configuration. Any existing
	// executable will do — nothing is ever exec'd on the refusal path.
	t.Setenv(termLinksEnv, "/bin/true")

	if code := runTermChat([]string{webToolsFlag, "--addr", serviceConfigBoundAddr(t)}); code != 1 {
		t.Fatalf("runTermChat on an occupied address = %d, want 1 (its ordinary listen refusal) — the assertions below are about a run that must have reached the listener", code)
	}
	assertServiceConfigPublished(t, "gopi cli")
}

// TestUIServiceConfigResolutionPublishesTheResolvedConfiguration is `gopi ui`'s
// gate, driven through runUIChat.
func TestUIServiceConfigResolutionPublishesTheResolvedConfiguration(t *testing.T) {
	serviceConfigTestProfile(t)
	clearServiceConfigTestEnv(t)

	if code := runUIChat([]string{uiToolsFlag, "--addr", serviceConfigBoundAddr(t)}); code != 1 {
		t.Fatalf("runUIChat on an occupied address = %d, want 1 (its ordinary listen refusal) — the assertions below are about a run that must have reached the listener", code)
	}
	assertServiceConfigPublished(t, "gopi ui")
}

// TestServiceFacesDoNotResolveOnARefusalPath pins the POSITION, which the three
// gates above cannot: they prove the resolution happens on a real run, not that
// it stays off the paths that must not touch the process environment. A usage
// page is a zero-side-effect path (the repo's own worker `-h` contract), and a
// usage query that republished the operator's whole configuration into the
// environment would make every probe of the CLI mutate the run it probes.
func TestServiceFacesDoNotResolveOnARefusalPath(t *testing.T) {
	serviceConfigTestProfile(t)
	clearServiceConfigTestEnv(t)

	for _, c := range []struct {
		face string
		run  func(args []string) int
	}{
		{"gopi web", runWeb},
		{"gopi cli", runTermChat},
		{"gopi ui", runUIChat},
	} {
		if code := c.run([]string{HelpFlag}); code != 0 {
			t.Fatalf("%s -h = %d, want 0", c.face, code)
		}
		for _, name := range []string{"GOPI_REASONING_EFFORT", "GOPI_THINKING_BUDGET", "ANTHROPIC_MODEL"} {
			if v, ok := os.LookupEnv(name); ok {
				t.Errorf("%s -h published %s=%q — a usage query must not resolve or publish the configuration (it is not a run)", c.face, name, v)
			}
		}
	}
}

// TestServiceConfigResolutionReducesTheFacesArgv pins that the reduction the
// three faces share is the chat face's OWN function (run_config_args.go), not a
// second walk: each face's switches stay out of the resolution's argv, and a
// catalog flag keeps its operand.
func TestServiceConfigResolutionReducesTheFacesArgv(t *testing.T) {
	argv := []string{webToolsFlag, "--addr", "127.0.0.1:0", "--reasoning-effort=high", "--thinking", "enabled"}
	got, _ := catalogConfigArgs(argv)
	want := "--reasoning-effort=high --thinking enabled"
	if strings.Join(got, " ") != want {
		t.Fatalf("catalogConfigArgs(%v) = %v, want %q — a service face's own switches must not reach the run path's flag parser", argv, got, want)
	}
}
