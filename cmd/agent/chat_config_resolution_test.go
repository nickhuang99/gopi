package main

// chat_config_resolution_test.go — the chat surface RESOLVES the configuration.
//
// THE DEFECT THESE GATES PIN (measured 2026-11-12 on this host). `gopi chat`
// reached an agent without ever calling resolveRunConfig, because main() resolves
// the configuration AFTER the subcommand chain and `isChatSubcommand` exits from
// INSIDE that chain. config.ApplyEnv therefore never ran on the chat path, so not
// one settings.json knob reached the process environment, and internal/agent
// (which reads reasoning_effort / thinking from the ENVIRONMENT only) sent every
// chat request with thinking=disabled. Measured by running the PRE-FIX binary and
// the POST-FIX binary against the same settings document in the same cleared
// environment and reading the request body out of each run's raw log:
//
//	pre-fix  (session 8bb03de7):  "thinking":{"type":"disabled"}   — and no
//	                              "reasoning_effort" field at all
//	post-fix (session ce9c8b92):  "thinking":{"type":"enabled","budget_tokens":16384}
//	                              "reasoning_effort":"high"
//
// DO NOT re-derive that A/B with /proc/<pid>/environ. It is a snapshot taken at
// execve, so it cannot show a RUNTIME publication: measured on this host, a
// Setenv'd variable is absent from /proc/self/environ while os.Getenv returns it,
// which means the probe prints "not published" about a value that WAS published
// and therefore reads the same on a fixed and an unfixed binary. The evidence
// below is os.Getenv (what internal/agent itself reads) plus the raw-log body.

//
// The gate is driven through the REAL REPL (runChatWithInput) rather than through
// resolveChatRunConfig alone, because the bug was never "the function
// misbehaves" — it was "the function is not on the chat path". A unit test of the
// resolver was green the whole time the defect was live.

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/testutil"
)

// chatConfigTestSettings is the settings document the gates below put in a
// THROWAWAY profile. It carries the two knobs the defect dropped, plus a doubao
// credential whose NON-publication is the exception that must survive.
const chatConfigTestSettings = `{
  "reasoning_effort": "high",
  "thinking_budget": 16384,
  "model": "chat-config-test-model",
  "base_url": "https://chat-config-test.example/anthropic",
  "api_key": "` + chatConfigTestAPIKey + `",
  "doubao_api_key": "not-a-real-key"
}`

// chatConfigTestAPIKey is the PROVIDER credential the settings document carries.
// It is published into the child's environment on purpose (see the assertion that
// reads it back), so the value is a fixture, never a real key.
const chatConfigTestAPIKey = "chat-config-test-provider-key"

// chatConfigTestProfile builds a throwaway gopi profile carrying the settings
// document above and pins GOPI_HOME at it, so the run resolves the REAL default
// settings path (<GOPI_HOME>/.gopi/settings.json) — no --settings flag, nothing
// the production resolution would not do by itself. The path is taken from the
// ONE resolver (config.DefaultSettingsPath) rather than spelled here, so this
// fixture cannot disagree with the reader under test about where the file lives.
func chatConfigTestProfile(t *testing.T) {
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
	if err := os.WriteFile(path, []byte(chatConfigTestSettings), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// chatConfigTestEnvKeys are the variables a chat run may decide. They are UNSET
// before the run (so the settings layer is the channel under test, not an
// inherited export) and restored afterwards through testutil.SnapshotEnv —
// Rule 17's sanctioned per-call pair, because t.Setenv cannot express "unset".
func clearChatConfigTestEnv(t *testing.T) {
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

// chatConfigTestModel is the model name the harness's agent declares; it only
// has to be a value the mock provider accepts.
const chatConfigTestModel = "test-model"

// TestChatConfigResolutionPublishesTheResolvedConfiguration is THE gate: a chat run started
// with a settings file and NO environment of its own must come out of
// runChatWithInput with the settings' knobs published into its environment.
//
// The input is an immediate EOF — the run resolves its configuration, builds its
// agent, registers its session and ends without a turn — so the assertion is
// about the LAUNCH PATH, which is exactly where the defect lived. The agent is a
// stub (chatAgentFactory), the seam the tree's other REPL harnesses use: this
// test judges the configuration, never the provider.
func TestChatConfigResolutionPublishesTheResolvedConfiguration(t *testing.T) {
	chatConfigTestProfile(t)
	clearChatConfigTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) //stylegate:ignore a test provider that asserts nothing about the body
		textSSE(w, "ok")
	}))
	t.Cleanup(srv.Close)

	sl, err := agent.NewSessionLogger(chatConfigTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      chatConfigTestModel,
		Timeout:    0,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	in, closeIn := newChatInputCooked(strings.NewReader(""), io.Discard)
	code := runChatWithInput([]string{"--no-editor"}, in)
	closeIn()

	// A usage refusal would mean the argv never reached the resolution, and every
	// assertion below would then be about a run that did not happen — say so
	// first rather than reporting a missing variable as a product defect.
	if code == chatExitUsage {
		t.Fatalf("the chat face refused its own argv (exit %d) — the assertions below would be vacuous", code)
	}

	// THE EXACT ACCEPTANCE CRITERION OF THE EPIC, read from this process's own
	// environment (os.Getenv is what internal/agent reads). /proc/<pid>/environ is
	// a snapshot taken at execve and cannot show a runtime publication — measured
	// on this host: a Setenv'd variable is ABSENT from /proc/self/environ, so a
	// probe built on it would read "not published" about a published value.
	if got, want := os.Getenv("GOPI_REASONING_EFFORT"), "high"; got != want {
		t.Errorf("GOPI_REASONING_EFFORT = %q, want %q — the chat face did not publish the settings' reasoning_effort, so internal/agent sends no effort at all", got, want)
	}
	if got, want := os.Getenv("GOPI_THINKING_BUDGET"), "16384"; got != want {
		t.Errorf("GOPI_THINKING_BUDGET = %q, want %q — the chat face did not publish the settings' thinking_budget", got, want)
	}

	// THE REST OF THE PUBLICATION CHAIN, because the defect was never "two
	// knobs went missing": ApplyEnv did not run AT ALL on this path, so EVERY
	// settings key went missing and reasoning_effort was only the first casualty
	// found. A regression that republishes just the two assertions above would
	// otherwise pass while the operator's model and endpoint stayed unreachable
	// from the chat surface — measured 2026-11-12 on this host, a chat process
	// started with NO inherited exports carried none of the four below before the
	// fix and all four after it.
	for _, c := range []struct{ name, want, why string }{
		{"ANTHROPIC_MODEL", "chat-config-test-model", "the request would name the code default instead of the configured model"},
		{"DEEPSEEK_MODEL", "chat-config-test-model", "the legacy spelling of the same knob must agree with the one above"},
		{"ANTHROPIC_BASE_URL", "https://chat-config-test.example/anthropic", "the request would go to the code default endpoint"},
	} {
		if got := os.Getenv(c.name); got != c.want {
			t.Errorf("%s = %q, want %q — %s", c.name, got, c.want, c.why)
		}
	}

	// THE TWO HALVES OF THE CREDENTIAL QUESTION, asserted together because they
	// are ONE decision split in two: config.ApplyEnv publishes the PROVIDER
	// credential (ANTHROPIC_AUTH_TOKEN
	// / DEEPSEEK_API_KEY, the child needs it to answer) and withholds the DOUBAO
	// one (the child never needs it, and the environment is inherited by every
	// process the run spawns). Asserting BOTH halves pins the shape, so a later
	// edit cannot "simplify" either direction: the provider key must be there or
	// the run cannot authenticate after the settings file stopped being read, and
	// the doubao key must not be there or the credential's copy widened.
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "DEEPSEEK_API_KEY"} {
		if got := os.Getenv(name); got != chatConfigTestAPIKey {
			t.Errorf("%s = %q, want the settings' provider key %q — the chat face must publish the credential its own requests need", name, got, chatConfigTestAPIKey)
		}
	}

	// THE EXCEPTION THAT MUST SURVIVE: a credential with an env channel that
	// config.ApplyEnv deliberately does NOT republish (the process environment is
	// inherited by every child gopi spawns; the 0600 settings document is not).
	// Making the chat face resolve the configuration must not widen that copy.
	for _, name := range []string{config.DoubaoAPIKeyEnv, config.AudioAPIKeyEnv} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s = %q, want it UNSET — publishing the settings into the chat environment widened the credential's copy", name, v)
		}
	}
}

// TestChatConfigResolutionReducesArgvToTheCatalog pins the argv reduction the
// chat face needs: chat's own switches are not flags of the RUN path, and handing
// them to FlagOverrides would refuse the caller's own command line as "unknown
// flag". A value-taking catalog flag keeps its operand; a chat-only switch is
// left to chat.
func TestChatConfigResolutionReducesArgvToTheCatalog(t *testing.T) {
	argv := []string{"--tools", "--reasoning-effort", "high", "--thinking", "enabled", "--no-editor"}
	got, consumed := catalogConfigArgs(argv)
	want := []string{"--reasoning-effort", "high", "--thinking", "enabled"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("catalogConfigArgs(%v) = %v, want %v", argv, got, want)
	}
	// The reduction must also tell the caller WHICH words it took: a face that
	// builds a prompt out of the remainder needs the same walk, not a second one.
	wantConsumed := []bool{false, true, true, true, true, false}
	for i := range wantConsumed {
		if consumed[i] != wantConsumed[i] {
			t.Errorf("consumed[%d] (%q) = %v, want %v", i, argv[i], consumed[i], wantConsumed[i])
		}
	}
}
