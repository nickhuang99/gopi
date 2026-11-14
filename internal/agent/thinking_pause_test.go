package agent

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/thinkmode"
)

// ---------------------------------------------------------------------------
// Thinking-budget pause + recovery probe (2026-09-15).
//
// Defect pinned here: the maxThinkingAbortStreak downgrade used to be a ONE-WAY
// latch. thinkingForceDisabled was set on the second consecutive over-budget
// round and never cleared, so a run that hit two runaway rounds early lost
// reasoning for EVERY later round — while the banner told the operator
// "disabling thinking for the rest of the session". One early runaway therefore
// cost hours of reasoning on a long task.
//
// Contract now: the pause is a CYCLE. Every thinkingProbeSpacing-th round is
// sent thinking-ENABLED as a recovery probe. A probe that answers within budget
// RESTORES thinking for the rest of the run; a probe that blows the budget
// re-engages the pause immediately (and the round is re-issued disabled as
// usual — no second paid double-prefill inside the same round).
//
// The scripted server below classifies every request into a round index (a
// thinking-enabled request starts a round; the disabled request that
// immediately follows an enabled one is its re-issue, not a new round) so the
// assertions read the pause/probe cadence directly off the wire.
// ---------------------------------------------------------------------------

const (
	testThinkingBudgetTokens = 16 // 16 tokens x 4 chars/token = 64 chars abort cut
	// testFinalRound is the round index whose answer ends the scripted run, and
	// testMaxThinkingIterations the loop cap — comfortably above it.
	testFinalRound            = 18
	testMaxThinkingIterations = 40
)

// thinkingBudgetTestServer scripts a thinking-budget scenario: probeOverBudget
// says whether the recovery probe (probeRound) blows the budget, and every
// other thinking-enabled round is an over-budget abort round. Paused rounds
// answer with a tool call (distinct args per round, so the loop's repeated-call
// guards stay quiet) keeping the ReAct loop alive through the pause.
func thinkingBudgetTestServer(t *testing.T, probeRound int, probeOverBudget bool) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	round := 0
	lastEnabled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := claudeRequestFrom(t, r)
		enabled := req.Thinking != nil && req.Thinking.Type == thinkmode.Enabled

		mu.Lock()
		if enabled {
			round++
		} else if !lastEnabled {
			// A disabled request that does NOT follow an enabled one is a fresh
			// paused round; one that does is that round's re-issue.
			round++
		}
		lastEnabled = enabled
		n := round
		seen = append(seen, fmt.Sprintf("%d:%s", n, map[bool]string{true: "enabled", false: "disabled"}[enabled]))
		mu.Unlock()

		if enabled {
			if n == probeRound && !probeOverBudget {
				// The probe answers within budget: no abort, thinking restored.
				claudeTextStream(w, "Probe answered within budget.", "end_turn")
				return
			}
			if n > probeRound {
				// After a SUCCESSFUL probe, thinking stays on: these rounds think
				// lightly (no thinking blocks at all) so the run is not re-paused,
				// which is exactly what the restored state means.
				if n >= testFinalRound {
					claudeTextStream(w, "All tasks completed.", "end_turn")
					return
				}
				claudeToolUseStream(w, "read", map[string]interface{}{"path": fmt.Sprintf("/tmp/thinking-resumed-%d", n)})
				return
			}
			// Over-budget thinking: stream past the abort cut, then a late text
			// block (the round is aborted anyway).
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
			for i := 0; i < 3; i++ {
				writeClaudeSSE(w, "content_block_delta",
					fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%q}}",
						strings.Repeat("x", 50)))
			}
			claudeTextStream(w, "Thinking too much.", "end_turn")
			return
		}

		if n >= testFinalRound {
			claudeTextStream(w, "All tasks completed.", "end_turn")
			return
		}
		// A tool round so the loop continues instead of treating this as the
		// final answer.
		claudeToolUseStream(w, "read", map[string]interface{}{"path": fmt.Sprintf("/tmp/thinking-pause-%d", n)})
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(seen))
		copy(out, seen)
		return out
	}
}

func newThinkingBudgetAgent(server *httptest.Server, logs *strings.Builder) *Agent {
	return &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		Logger:        log.New(logs, "", 0),
		MaxIterations: testMaxThinkingIterations,
		Thinking:      &deepseek.Thinking{Type: thinkmode.Enabled, BudgetTokens: testThinkingBudgetTokens},
	}
}

// seenRoundEnabled reports whether the request with the given round index went
// out with thinking enabled.
func seenRoundEnabled(seen []string, round int) bool {
	want := fmt.Sprintf("%d:enabled", round)
	for _, s := range seen {
		if s == want {
			return true
		}
	}
	return false
}

// TestAgent_Run_ThinkingRecoveryProbeRestoresThinking pins the fix: after two
// over-budget rounds pause thinking, the pause lasts thinkingProbeSpacing
// rounds and then a single thinking-enabled PROBE goes out; when that probe
// answers within budget the pause ends and thinking stays on for the rest of
// the run (pre-fix: it stayed off forever).
func TestAgent_Run_ThinkingRecoveryProbeRestoresThinking(t *testing.T) {
	probeRound := maxThinkingAbortStreak + thinkingProbeSpacing + 1 // rounds 1,2 abort; 3..10 paused; 11 = probe
	server, seen := thinkingBudgetTestServer(t, probeRound, false)

	var logs strings.Builder
	ag := newThinkingBudgetAgent(server, &logs)
	if _, err := ag.Run("Complete the task"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	got := seen()
	// Rounds 1 and 2 are the abort rounds (thinking enabled).
	if !seenRoundEnabled(got, 1) || !seenRoundEnabled(got, 2) {
		t.Fatalf("expected thinking enabled on rounds 1-2, saw %v", got)
	}
	// Rounds 3..probeRound-1 are the pause: thinking must be OFF.
	for r := 3; r < probeRound; r++ {
		if seenRoundEnabled(got, r) {
			t.Errorf("round %d was sent thinking-enabled during the pause (want disabled); sequence %v", r, got)
		}
	}
	// The probe round must be thinking-enabled again.
	if !seenRoundEnabled(got, probeRound) {
		t.Fatalf("expected a thinking-enabled recovery probe at round %d, saw %v", probeRound, got)
	}
	// The probe answered within budget: the pause must be over.
	if ag.thinkingForceDisabled {
		t.Errorf("thinking must be restored after a within-budget probe, but the run is still paused")
	}
	if !strings.Contains(logs.String(), "thinking recovery probe finished within budget") {
		t.Errorf("expected the recovery log line; logs:\n%s", logs.String())
	}
}

// TestAgent_Run_ThinkingProbeReLatch pins the other half: a probe that blows
// the budget re-engages the pause IMMEDIATELY (the run already proved twice
// that its thinking runs away) and the probe's own round is still re-issued
// thinking-disabled, so no second double-prefill is paid.
func TestAgent_Run_ThinkingProbeReLatch(t *testing.T) {
	probeRound := maxThinkingAbortStreak + thinkingProbeSpacing + 1
	server, seen := thinkingBudgetTestServer(t, probeRound, true)

	var logs strings.Builder
	ag := newThinkingBudgetAgent(server, &logs)
	if _, err := ag.Run("Complete the task"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	got := seen()
	if !seenRoundEnabled(got, probeRound) {
		t.Fatalf("expected a thinking-enabled recovery probe at round %d, saw %v", probeRound, got)
	}
	// The paused rounds AFTER the failed probe must be thinking-disabled: the
	// pause re-engaged instead of leaving thinking on.
	for r := probeRound + 1; r < testFinalRound; r++ {
		if seenRoundEnabled(got, r) {
			t.Errorf("round %d was sent thinking-enabled after a failed probe (want disabled); sequence %v", r, got)
		}
	}
	if !strings.Contains(logs.String(), "keeping thinking paused for another") {
		t.Errorf("expected the re-latch line; logs:\n%s", logs.String())
	}
	// The probe round itself must appear TWICE (probe + disabled re-issue).
	probeReqs := 0
	for _, s := range got {
		if strings.HasPrefix(s, fmt.Sprintf("%d:", probeRound)) {
			probeReqs++
		}
	}
	if probeReqs != 2 {
		t.Errorf("expected probe round %d to be re-issued once (2 requests), saw %d in %v", probeRound, probeReqs, got)
	}
}

// TestAgent_ReportEffectiveThinkingBudget pins the Rule 15 report: a configured
// budget above max_tokens/2 is silently clamped by the request builder (16384
// configured vs the default max_tokens 16384 → effective 8192), which is also
// the harness's abort point. The clamp must be named once per run, with both
// numbers; an unclamped budget must log nothing.
func TestAgent_ReportEffectiveThinkingBudget(t *testing.T) {
	var logs strings.Builder
	ag := &Agent{Logger: log.New(&logs, "", 0)}

	ag.reportEffectiveThinkingBudget(16384, 16384, 8192)
	if !strings.Contains(logs.String(), "clamped to 8192") {
		t.Fatalf("expected the clamp report naming the effective budget; logs:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "16384") || !strings.Contains(logs.String(), "GOPI_MAX_TOKENS") {
		t.Errorf("the clamp report must name the configured value and the knob to move; logs:\n%s", logs.String())
	}
	// Once per run: a second report must not repeat the line.
	first := logs.String()
	ag.reportEffectiveThinkingBudget(16384, 16384, 8192)
	if logs.String() != first {
		t.Errorf("the budget report must fire once per run, got a second line:\n%s", logs.String()[len(first):])
	}

	// No clamp → no line (the common case must stay quiet).
	var quiet strings.Builder
	(&Agent{Logger: log.New(&quiet, "", 0)}).reportEffectiveThinkingBudget(1024, 16384, 1024)
	if quiet.Len() != 0 {
		t.Errorf("an unclamped budget must log nothing, got:\n%s", quiet.String())
	}
}

// TestAgent_ThinkingPausedForRound pins the pause/probe schedule itself: the
// first thinkingProbeSpacing rounds after the pause are paused, the next one is
// the probe, and a cleared pause reports no pause at all.
func TestAgent_ThinkingPausedForRound(t *testing.T) {
	var ag Agent
	if paused, probe := ag.thinkingPausedForRound(); paused || probe {
		t.Fatalf("an un-paused run must report neither pause nor probe, got paused=%v probe=%v", paused, probe)
	}
	ag.thinkingForceDisabled = true
	for i := 1; i <= thinkingProbeSpacing; i++ {
		paused, probe := ag.thinkingPausedForRound()
		if !paused || probe {
			t.Fatalf("paused round %d: got paused=%v probe=%v, want paused=true probe=false", i, paused, probe)
		}
	}
	if paused, probe := ag.thinkingPausedForRound(); paused || !probe {
		t.Fatalf("round %d must be the recovery probe, got paused=%v probe=%v", thinkingProbeSpacing+1, paused, probe)
	}
}

// TestThinkingTypeLiteralGate is Rule 13's ratchet for the thinking wire values:
// thinkmode.Enabled / thinkmode.Disabled are the single source of truth, and no
// production file outside internal/thinkmode may re-inline "enabled"/"disabled"
// at a thinking-type site. A re-inlined literal is one rename away from a
// silent mismatch — the client would put one spelling on the wire and compare
// another.
func TestThinkingTypeLiteralGate(t *testing.T) {
	root, err := gitRootForTest()
	if err != nil {
		t.Skipf("no repo root (%v) — literal gate skipped", err)
	}
	thinkingLiteral := regexp.MustCompile(`"(enabled|disabled)"`)
	var scanned, offenders int
	for _, tree := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, tree), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err //nolint: nil error propagates a walk failure — reported below
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel := filepath.ToSlash(path)
			if strings.Contains(rel, "/internal/thinkmode/") {
				return nil // the ONE allowed holder
			}
			if strings.Contains(rel, "/internal/standards/") {
				return nil // the standards gate tree carries synthetic sources
			}
			scanned++
			b, readErr := os.ReadFile(path) //stylegate:ignore scanning the tree for a gate is the point
			if readErr != nil {
				return readErr
			}
			for i, line := range strings.Split(string(b), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") || strings.Contains(trimmed, "//thinkmode-literal-ok:") {
					continue // prose/examples in comments are documentation, not a wire value
				}
				// Only thinking-type sites: the words "enable/disable" appear
				// legitimately for many other knobs (task register, env bools).
				if !strings.Contains(line, "hinking") {
					continue
				}
				if thinkingLiteral.MatchString(line) {
					offenders++
					t.Errorf("%s:%d re-inlines a thinking wire value — use thinkmode.Enabled/Disabled (or mark it //thinkmode-literal-ok: <reason>):\n\t%s", rel, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	if scanned == 0 {
		t.Fatal("literal gate scanned 0 files — the walk is broken, not the tree")
	}
	if offenders == 0 {
		t.Logf("thinking-type literal gate: %d production files scanned, no re-inlined wire value", scanned)
	}
}

// gitRootForTest walks up from the test's working directory to the repo root.
func gitRootForTest() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
