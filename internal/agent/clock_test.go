package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// tickRe matches a tick label produced by SessionClock.TickLabel in EITHER
// form: the exact sub-minute reading ("[t+47s]") and the folded, approximate
// one ("[t+~31m]", "[t+~2h]", "[t+~3d]"). The unit letter is captured so a
// test can fold it back into seconds and still ask monotonic questions — the
// property the label exists to carry.
var tickRe = regexp.MustCompile(`\[t\+(~)?(\d+)([smhd])\]`)

// ticksInRequest returns every tick label found in the request's tool_result
// content blocks, in conversation order.
func ticksInRequest(req *deepseek.ClaudeRequest) []string {
	var ticks []string
	for _, msg := range req.Messages {
		for _, b := range msg.Content {
			if b.Type != "tool_result" {
				continue
			}
			ticks = append(ticks, tickRe.FindAllString(fmt.Sprintf("%v", b.Content), -1)...)
		}
	}
	return ticks
}

// tickUnitSeconds is what one unit letter is worth, so a folded label is
// comparable with a sub-minute one.
var tickUnitSeconds = map[string]int{"s": 1, "m": 60, "h": 3600, "d": 86400}

// tickOf extracts the integer seconds a tick label stands for, or -1 when the
// label is not a tick. It reads ALL tiers (s/m/h/d), because a clock that has
// been running for an hour paints minutes and a monotonicity check has to
// compare that against an earlier seconds reading.
func tickOf(label string) int {
	m := tickRe.FindStringSubmatch(label)
	if m == nil || m[0] != label {
		return -1
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return -1
	}
	return n * tickUnitSeconds[m[3]]
}

func TestSessionClock_Elapsed(t *testing.T) {
	c := NewSessionClock()
	if d := c.Elapsed(); d < 0 {
		t.Fatalf("Elapsed() = %v, want >= 0", d)
	}
	time.Sleep(5 * time.Millisecond) //stylegate:allow-sleep an elapsed-time assertion needs a non-zero interval to actually elapse
	if d := c.Elapsed(); d <= 0 {
		t.Fatalf("Elapsed() = %v after 5ms sleep, want > 0", d)
	}
	// Nil clock reports zero elapsed (Agent{} literals in tests).
	var nilClock *SessionClock
	if d := nilClock.Elapsed(); d != 0 {
		t.Fatalf("nil clock Elapsed() = %v, want 0", d)
	}
}

func TestSessionClock_TickLabelFormat(t *testing.T) {
	c := NewSessionClock()
	label := c.TickLabel()
	if !tickRe.MatchString(label) {
		t.Fatalf("TickLabel() = %q, want one of the %s forms", label, tickShapeLabel)
	}
	// The label folds back into elapsed time, so a reader can still compare it
	// with any other tick.
	if tickOf(label) < 0 {
		t.Fatalf("TickLabel() = %q: cannot fold into seconds", label)
	}
}

func TestSessionClock_TickIncreases(t *testing.T) {
	c := NewSessionClock()
	first := tickOf(c.TickLabel())
	time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep an elapsed-time assertion needs a non-zero interval to actually elapse
	second := tickOf(c.TickLabel())
	if second < first {
		t.Fatalf("tick went backwards: %d -> %d", first, second)
	}
}

func TestAgent_WithTick(t *testing.T) {
	// Nil clock → passthrough (tests building Agent{} literals).
	a := &Agent{}
	if got := a.withTick(`{"status":"ok"}`); got != `{"status":"ok"}` {
		t.Fatalf("nil-clock withTick = %q, want passthrough", got)
	}
	// Ticked agent → prefix + original content preserved verbatim.
	a.Clock = NewSessionClock()
	got := a.withTick("file content: hello\n")
	if !strings.HasPrefix(got, "[t+") || !strings.HasSuffix(got, "file content: hello\n") {
		t.Fatalf("withTick = %q, want a tick prefix + original content", got)
	}
	// The prefix must not mutate the payload body — anything after the
	// marker is byte-identical to the original result.
	body := strings.TrimSpace(strings.SplitN(got, " ", 2)[1])
	if body != "file content: hello" {
		t.Fatalf("tick prefix corrupted the tool result body: %q", body)
	}
}

// TestAgent_Run_ToolResultCarriesTick is the acceptance test for the tick
// injection: a real Run loop round-trip where the mock LLM calls read, and
// the NEXT request's tool_result must carry the tick prefix alongside
// the original content (wire contract preserved).
func TestAgent_Run_ToolResultCarriesTick(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	testContent := "hello from gopi tick test\n"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		switch callCount {
		case 1:
			claudeToolUseStream(w, "read", map[string]interface{}{"path": testFile})
		case 2:
			// The tool_result sent back to the model must carry the tick
			// prefix AND the untampered original content.
			if !claudeHasToolResult(req, "[t+") {
				t.Errorf("tool_result missing the elapsed tick")
			}
			if !claudeHasToolResult(req, testContent) {
				t.Errorf("tool_result missing original content (contract broken)")
			}
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		default:
			// Task-end memory_store checkpoint round: completed again → exit.
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		Clock:         NewSessionClock(),
	}

	if _, err := ag.Run("read the file"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

// TestAgent_Run_TickIncrementsAcrossRounds verifies the tick is a live
// monotonic counter: two consecutive tool rounds must show non-decreasing
// tick values in the model-facing conversation.
func TestAgent_Run_TickIncrementsAcrossRounds(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("x\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		switch callCount {
		case 1, 2:
			claudeToolUseStream(w, "read", map[string]interface{}{"path": testFile})
		case 3:
			// Two tool rounds have happened; both tool_results in this
			// conversation must carry ticks, in non-decreasing order.
			ticks := ticksInRequest(req)
			if len(ticks) < 2 {
				t.Fatalf("expected >= 2 ticked tool_results, got %d (%v)", len(ticks), ticks)
			}
			prev := -1
			for _, lbl := range ticks {
				n := tickOf(lbl)
				if n < 0 {
					t.Fatalf("unparsable tick %q", lbl)
				}
				if n < prev {
					t.Fatalf("tick not monotonic across rounds: %v", ticks)
				}
				prev = n
			}
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		default:
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		Clock:         NewSessionClock(),
	}

	if _, err := ag.Run("read the file twice"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

// TestDurationTier_Boundaries is the tier table (the epic's acceptance): each
// arm is a duration straddling one boundary of clock.go's ceilings, and each
// asserts BOTH halves of what a reader gets — the rendered value and whether it
// is flagged approximate. The arms are the ones where a sign error or an
// off-by-one unit is visible: exactly at a ceiling (60s, 1h, 24h) the COARSER
// unit must already be in use, because the ceilings are exclusive.
func TestDurationTier_Boundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     time.Duration
		want   string
		approx bool
	}{
		{"zero", 0, "0s", false},
		{"sub-second truncated", 900 * time.Millisecond, "0s", false},
		{"59s stays exact seconds", 59 * time.Second, "59s", false},
		{"60s becomes the first minute", 60 * time.Second, "1m", true},
		{"3599s is still minutes", 3599 * time.Second, "59m", true},
		{"3600s becomes the first hour", 3600 * time.Second, "1h", true},
		{"86399s is still hours", 86399 * time.Second, "23h", true},
		{"86400s becomes the first day", 86400 * time.Second, "1d", true},
		{"5d keeps counting days", 5 * 24 * time.Hour, "5d", true},
		// The gap the epic was filed over: 1840s must read as ~half an hour
		// rather than as a number the reader has to divide.
		{"1840s reads as minutes", 1840 * time.Second, "30m", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, approx := durationTier(tc.in)
			if got != tc.want {
				t.Fatalf("durationTier(%v) = %q, want %q", tc.in, got, tc.want)
			}
			if approx != tc.approx {
				t.Fatalf("durationTier(%v) approx = %v, want %v", tc.in, approx, tc.approx)
			}
			// The label is what TickLabel puts between "[t+" and "]", so a value
			// the tier function can produce must survive the full render too.
			label := tickLabelOf(tc.in)
			wantLabel := "[t+" + got + "]"
			if approx {
				wantLabel = "[t+~" + got + "]"
			}
			if label != wantLabel {
				t.Fatalf("tickLabelOf(%v) = %q, want %q", tc.in, label, wantLabel)
			}
		})
	}
}

// TestTickLabel_MatchesThePromptShape pins the single-spelling contract: what
// the clock paints and what the prompt teaches must be the same vocabulary. The
// prompt names the SHAPE (tickShapeLabel), so the assertion is that every unit
// the clock can emit appears in it — a prompt that forgot a tier would teach a
// label the model then fails to recognise.
func TestTickLabel_MatchesThePromptShape(t *testing.T) {
	for _, d := range []time.Duration{47 * time.Second, 31 * time.Minute, 2 * time.Hour, 3 * 24 * time.Hour} {
		label := tickLabelOf(d)
		if !tickRe.MatchString(label) {
			t.Fatalf("tickLabelOf(%v) = %q does not match the package tick shape", d, label)
		}
		unit := string(label[len(label)-2])
		if !strings.Contains(tickShapeLabel, unit) {
			t.Fatalf("the prompt's %s does not name the %q unit the clock emits in %q", tickShapeLabel, unit, label)
		}
	}
	if !strings.Contains(chatTurnClockRules(), tickShapeLabel) {
		t.Fatalf("the chat clock block does not teach the tick shape %q", tickShapeLabel)
	}
}

// TestTurnStamp_RenderFoldsBothHalves pins the epic's other half: the gap and
// the tick go through ONE tier function, so no request can carry a folded tick
// beside a bare-second interval (the two-vocabulary defect this change removes).
func TestTurnStamp_RenderFoldsBothHalves(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sinceLast time.Duration
		wantGap   string
	}{
		{"7s", 7 * time.Second, "+7s"},
		{"5m", 5 * time.Minute, "+5m"},
		{"2h", 2 * time.Hour, "+2h"},
		{"3d", 72 * time.Hour, "+3d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TurnStamp{Tick: "[t+~31m]", SinceLast: tc.sinceLast}.Render()
			if !strings.Contains(got, tc.wantGap+" "+turnStampGapSuffix) {
				t.Fatalf("Render() = %q, want the gap %q before %q", got, tc.wantGap, turnStampGapSuffix)
			}
			// The gap clause itself carries no "~": only the tick's POSITION is
			// flagged as rounded (clock.go turnStampGapFmt says why), so the
			// marker may appear left of the em dash and never right of it.
			if _, gapClause, ok := strings.Cut(got, " — "); ok && strings.Contains(gapClause, "~") {
				t.Fatalf("Render() = %q puts the tick's approximation marker on the gap", got)
			}
		})
	}
	// Every boundary of the shared function, seen from the gap side: the same
	// durations the tick table covers must fold identically in the interval.
	for _, d := range []time.Duration{59 * time.Second, 60 * time.Second, 3599 * time.Second,
		3600 * time.Second, 86399 * time.Second, 86400 * time.Second} {
		want, _ := durationTier(d)
		if got := (TurnStamp{Tick: "[t+~1h]", SinceLast: d}).Render(); !strings.Contains(got, "+"+want+" ") {
			t.Fatalf("gap for %v = %q, want the tier label %q", d, got, want)
		}
	}
}
