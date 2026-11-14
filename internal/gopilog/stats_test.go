package gopilog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// ---- raw log fixture used by the stats builders ----

// statsRawLog writes a 3-round raw log whose usage numbers are the known-good
// mapping test data: round 2 is a guard probe (annotated marker), round 3
// carries two tool calls and a cliff-sized prompt drop.
func statsRawLog(t *testing.T) (string, []HTTPRound) {
	t.Helper()
	content := "" +
		"→ REQUEST\n" +
		`{"model":"deepseek-v4-flash","stream":true,"system":[{"type":"text","text":"sys"}],"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← CHUNK\n" +
		"event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":104,"cache_read_input_tokens":9344,"cache_creation_input_tokens":0,"output_tokens":0}}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":97}}` + "\n" +
		"\n" +
		"→ REQUEST (1214-guard, stream)\n" +
		`{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"guard probe"}]}` + "\n" +
		"← CHUNK\n" +
		"event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":50,"cache_read_input_tokens":400,"cache_creation_input_tokens":0,"output_tokens":0}}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":11}}` + "\n" +
		"\n" +
		"→ REQUEST\n" +
		`{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"next"}]}` + "\n" +
		"← CHUNK\n" +
		"event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":356,"cache_read_input_tokens":9216,"cache_creation_input_tokens":0,"output_tokens":0}}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"read"}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"read"}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"bash"}}` + "\n" +
		"\n" +
		"← CHUNK\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":218}}` + "\n" +
		"\n"

	path := filepath.Join(t.TempDir(), "gopi-raw-stats-test.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 3 {
		t.Fatalf("len(rounds) = %d, want 3", len(rounds))
	}
	return path, rounds
}

// TestBuildStatsFromRoundsNumbers pins the raw-usage → stats mapping: the
// prompt total is input + cache_read + cache_creation (the JSONL's
// prompt_tokens), the hit is cache_read, the miss is the uncached input.
func TestBuildStatsFromRoundsNumbers(t *testing.T) {
	_, rounds := statsRawLog(t)
	st := BuildStatsFromRounds(rounds)

	if st.Source != StatsSourceRaw {
		t.Errorf("Source = %q, want %q", st.Source, StatsSourceRaw)
	}
	// 3 request markers; the guard probe keeps its row but stays out of the
	// token aggregates.
	if st.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3 (every marker is a round)", st.Rounds)
	}
	if st.RoundsCounted != 2 || st.Probes != 1 {
		t.Errorf("RoundsCounted/Probes = %d/%d, want 2/1", st.RoundsCounted, st.Probes)
	}
	if st.NoUsage != 0 {
		t.Errorf("NoUsage = %d, want 0", st.NoUsage)
	}
	// round 1: 104 + 9344 + 0 ; round 3: 356 + 9216 + 0 (round 2 is a probe)
	wantPrompt := (104 + 9344) + (356 + 9216)
	if st.PromptTokens != wantPrompt {
		t.Errorf("PromptTokens = %d, want %d", st.PromptTokens, wantPrompt)
	}
	wantHit := 9344 + 9216
	if st.CacheHit != wantHit {
		t.Errorf("CacheHit = %d, want %d", st.CacheHit, wantHit)
	}
	wantMiss := 104 + 356
	if st.CacheMiss != wantMiss {
		t.Errorf("CacheMiss = %d, want %d", st.CacheMiss, wantMiss)
	}
	if st.Completion != 97+218 {
		t.Errorf("Completion = %d, want %d (the probe's output tokens stay out)", st.Completion, 97+218)
	}
	if st.MinPrompt != 9448 || st.MaxPrompt != 9572 {
		t.Errorf("MinPrompt/MaxPrompt = %d/%d, want 9448/9572", st.MinPrompt, st.MaxPrompt)
	}
	if st.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3 (read, read, bash)", st.ToolCalls)
	}
	if len(st.ToolKinds) != 2 {
		t.Fatalf("len(ToolKinds) = %d, want 2", len(st.ToolKinds))
	}
	if st.ToolKinds[0].Name != "read" || st.ToolKinds[0].Calls != 2 {
		t.Errorf("ToolKinds[0] = %+v, want read×2 (two calls of one tool are two calls)",
			st.ToolKinds[0])
	}
	if st.ToolKinds[1].Name != "bash" || st.ToolKinds[1].Calls != 1 {
		t.Errorf("ToolKinds[1] = %+v, want bash×1", st.ToolKinds[1])
	}
}

// TestBuildStatsFromRoundsMatchesJSONL is the cross-source equivalence guard:
// the same rounds expressed as session JSONL request events must produce the
// same headline numbers (the JSONL is the raw log's fallback, not a rival).
// The probe round has no JSONL counterpart (a guard probe is not a logged
// iteration), so the equivalence is asserted over the two counted rounds.
func TestBuildStatsFromRoundsMatchesJSONL(t *testing.T) {
	_, rounds := statsRawLog(t)
	fromRaw := BuildStatsFromRounds(rounds)

	u := &SessionUsage{SessionID: "s", Model: "deepseek-v4-flash", TotalIterations: 2}
	for _, hr := range []struct{ in, read, out int }{
		{104, 9344, 97},
		{356, 9216, 218},
	} {
		u.Rounds = append(u.Rounds, UsageRound{
			PromptTokens:     hr.in + hr.read,
			CacheHit:         hr.read,
			CacheMiss:        hr.in,
			CompletionTokens: hr.out,
		})
	}
	fromJSONL := BuildStatsFromUsage(u)

	for _, c := range []struct {
		name string
		a, b int
	}{
		{"rounds", fromRaw.RoundsCounted, fromJSONL.Rounds},
		{"prompt", fromRaw.PromptTokens, fromJSONL.PromptTokens},
		{"cache hit", fromRaw.CacheHit, fromJSONL.CacheHit},
		{"cache miss", fromRaw.CacheMiss, fromJSONL.CacheMiss},
		{"completion", fromRaw.Completion, fromJSONL.Completion},
		{"min", fromRaw.MinPrompt, fromJSONL.MinPrompt},
		{"max", fromRaw.MaxPrompt, fromJSONL.MaxPrompt},
	} {
		if c.a != c.b {
			t.Errorf("%s: raw=%d jsonl=%d (must agree)", c.name, c.a, c.b)
		}
	}
}

// TestTrimCliffDetection pins the two-floor cliff rule: a drop must clear the
// absolute floor AND collapse to under statsCliffMaxRatioPct of the previous
// round. A large-but-shallow drop (session tail) is NOT a cliff.
func TestTrimCliffDetection(t *testing.T) {
	rounds := make([]HTTPRound, 5)
	prompts := []int{50000, 40000, 14000, 13000, 12000}
	for i := range rounds {
		rounds[i].Number = i + 1
		// input 0 + cache_read P → prompt = P
		rounds[i].Response.CacheRead = prompts[i]
		rounds[i].Response.InputTokens = 1
		rounds[i].Response.OutputTokens = 10
	}
	st := BuildStatsFromRounds(rounds)
	if st.CliffCount != 1 {
		t.Fatalf("CliffCount = %d (%+v), want 1", st.CliffCount, st.Cliffs)
	}
	idx := st.Cliffs[0]
	if st.RoundStats[idx].Number != 3 {
		t.Errorf("cliff round = %d, want 3", st.RoundStats[idx].Number)
	}
	if got, want := st.RoundStats[idx].DeltaReq, 14000-40000; got != want {
		t.Errorf("cliff Δreq = %d, want %d", got, want)
	}
	if st.LargestCliff != 14000-40000 || st.LargestCliffNumber != 3 {
		t.Errorf("LargestCliff/Number = %d/%d, want %d/3", st.LargestCliff, st.LargestCliffNumber, 14000-40000)
	}
	// round 2: -10,000 tokens but ratio 0.8 → shallow, no cliff.
	if st.RoundStats[1].Cliff {
		t.Errorf("round 2 marked as a cliff (%d → %d) — shallow drops must not count",
			prompts[0], prompts[1])
	}
}

// TestProbeRoundExcludedFromDeltaChain: a guard probe's tiny body must not
// read as a trim cliff, and the round after the probe compares against the
// last counted round, not the probe.
func TestProbeRoundExcludedFromDeltaChain(t *testing.T) {
	rounds := []HTTPRound{
		{Number: 1, Response: RoundResponse{CacheRead: 50000, InputTokens: 100}},
		{Number: 2, Annotation: "1214-guard, stream", Response: RoundResponse{CacheRead: 400, InputTokens: 50}},
		{Number: 3, Response: RoundResponse{CacheRead: 50500, InputTokens: 120}},
	}
	st := BuildStatsFromRounds(rounds)
	if st.RoundStats[1].DeltaReq != 0 || st.RoundStats[1].Cliff {
		t.Errorf("probe round got Δreq %d cliff=%v — probes must stay out of the Δ chain",
			st.RoundStats[1].DeltaReq, st.RoundStats[1].Cliff)
	}
	if st.RoundStats[1].PromptTokens != 450 {
		t.Errorf("probe prompt = %d, want 450 (the row itself is kept)", st.RoundStats[1].PromptTokens)
	}
	if got, want := st.RoundStats[2].DeltaReq, (50500+120)-(50000+100); got != want {
		t.Errorf("round 3 Δreq = %d, want %d (vs round 1, not the probe)", got, want)
	}
	if st.CliffCount != 0 {
		t.Errorf("CliffCount = %d, want 0", st.CliffCount)
	}
}

// TestNoUsageRoundKeepsRowButSkipsAggregate: a round that reported no usage
// (HTTP error, non-streamed) keeps its table row but must not drag min/avg.
func TestNoUsageRoundKeepsRowButSkipsAggregate(t *testing.T) {
	rounds := []HTTPRound{
		{Number: 1, Response: RoundResponse{Kind: "http-error", ErrorBody: []byte("503")},
			Marker: "→ REQUEST"},
		{Number: 2, Response: RoundResponse{CacheRead: 2000, InputTokens: 100}},
	}
	st := BuildStatsFromRounds(rounds)
	if st.NoUsage != 1 {
		t.Errorf("NoUsage = %d, want 1", st.NoUsage)
	}
	if st.Rounds != 2 || st.RoundsCounted != 1 {
		t.Errorf("Rounds/RoundsCounted = %d/%d, want 2/1", st.Rounds, st.RoundsCounted)
	}
	if len(st.RoundStats) != 2 {
		t.Fatalf("len(RoundStats) = %d, want 2 (the dead round keeps its row)", len(st.RoundStats))
	}
	if st.MinPrompt != 2100 {
		t.Errorf("MinPrompt = %d, want 2100 (the 0-prompt round is excluded)", st.MinPrompt)
	}
}

// TestScanSessionUsageAttributesTools checks the JSONL scan: a round's
// assistant tool_use events precede its request event, so the tools attach to
// the request that follows them.
func TestScanSessionUsageAttributesTools(t *testing.T) {
	lines := []string{
		`{"type":"session_start","sessionId":"abc","model":"m","rawLogPath":"/gone.log","timestamp":"2026-09-04T20:40:46+08:00"}`,
		`{"type":"user","content":"go"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read","id":"t1"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read","id":"t2"}]}}`,
		`{"type":"request","iteration":0,"prompt_tokens":1000,"cache_hit":900,"cache_miss":100,"completion_tokens":50}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
		`{"type":"request","iteration":1,"prompt_tokens":1200,"cache_hit":1000,"cache_miss":200,"completion_tokens":30}`,
		`{"type":"session_end","elapsed_sec":12.5,"total_iterations":2}`,
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	u, err := ScanSessionUsage(path)
	if err != nil {
		t.Fatalf("ScanSessionUsage: %v", err)
	}
	if u.SessionID != "abc" || u.Model != "m" || u.RawLogPath != "/gone.log" {
		t.Errorf("session_start fields = %q/%q/%q", u.SessionID, u.Model, u.RawLogPath)
	}
	if len(u.Rounds) != 2 {
		t.Fatalf("len(Rounds) = %d, want 2", len(u.Rounds))
	}
	if len(u.Rounds[0].Tools) != 2 || u.Rounds[0].Tools[0] != "read" || u.Rounds[0].Tools[1] != "read" {
		t.Errorf("round 0 tools = %v, want [read read] (every call is kept)", u.Rounds[0].Tools)
	}
	if len(u.Rounds[1].Tools) != 0 {
		t.Errorf("round 1 tools = %v, want none (a text-only round)", u.Rounds[1].Tools)
	}
	if !u.HasElapsed || u.ElapsedSec != 12.5 {
		t.Errorf("elapsed = %v/%v, want 12.5/true", u.ElapsedSec, u.HasElapsed)
	}
	st := BuildStatsFromUsage(u)
	// The row NUMBER is the canonical round (1-based, the `request` row's
	// position) — the same number `gopi session log --rounds` prints. It is
	// deliberately NOT the log's `iteration` field, which restarts at 0 for
	// every prompt inside one session and would give two exchanges one number.
	if st.Source != StatsSourceJSONL || st.RoundStats[0].Number != 1 {
		t.Errorf("jsonl source/round0 number = %q/%d, want jsonl/1", st.Source, st.RoundStats[0].Number)
	}
	if st.RoundStats[0].ToolCalls != 2 {
		t.Errorf("round 0 ToolCalls = %d, want 2 (a duplicate name is still two calls)",
			st.RoundStats[0].ToolCalls)
	}
	if st.ToolCalls != 2 || len(st.ToolKinds) != 1 || st.ToolKinds[0].Calls != 2 {
		t.Errorf("kinds = %d calls / %+v, want 2 calls / [read×2]", st.ToolCalls, st.ToolKinds)
	}
	if !st.HasWall || st.WallSeconds != 12.5 {
		t.Errorf("wall = %v/%v, want 12.5/true", st.WallSeconds, st.HasWall)
	}
}

// TestFormatStatsOverviewBoxShape checks the box's structure: every row is
// framed at the same width and the key column is aligned.
func TestFormatStatsOverviewBoxShape(t *testing.T) {
	_, rounds := statsRawLog(t)
	st := BuildStatsFromRounds(rounds)
	st.SessionID = "abc-123"
	st.Model = "deepseek-v4-flash"
	st.SourceDetail = "gopi-raw-stats-test.log"
	out := FormatStatsOverview(st)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("overview has %d lines, want a framed box:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "┌ ") || !strings.HasSuffix(lines[0], "┐") {
		t.Errorf("top border malformed: %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "└") {
		t.Errorf("bottom border malformed: %q", lines[len(lines)-1])
	}
	width := len([]rune(lines[0]))
	// EVERY line, the bottom rule included: the frame is one width, and the
	// line that used to be one rune wider than the others is exactly the defect
	// task 01a17b5f fixed — leaving it out of the loop is how the drift stayed
	// green here while the snapshots recorded it.
	for i, l := range lines[1:] {
		if len([]rune(l)) != width {
			t.Errorf("line %d width %d ≠ border width %d: %q", i+1, len([]rune(l)), width, l)
		}
	}
	for _, want := range []string{
		"model",
		"raw log gopi-raw-stats-test.log",
		"rounds         3",
		"prompt tokens  19,020",
		"cache",
		"completion     315",
		"tools          3 calls / 2 kinds",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("overview missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "97.58% hit") {
		t.Errorf("cache hit %% missing/miscomputed:\n%s", out)
	}
}

// TestFormatStatsOverviewCliffLine pins the cliff sentence.
func TestFormatStatsOverviewCliffLine(t *testing.T) {
	rounds := make([]HTTPRound, 2)
	rounds[0] = HTTPRound{Number: 1, Response: RoundResponse{CacheRead: 50000, InputTokens: 100}}
	rounds[1] = HTTPRound{Number: 2, Response: RoundResponse{CacheRead: 14000, InputTokens: 100}}
	st := BuildStatsFromRounds(rounds)
	out := FormatStatsOverview(st)
	want := "trim cliffs    1 (largest -36.0K @ " + sessionevent.CounterUnitRawRequest + " 2)"
	if !strings.Contains(out, want) {
		t.Errorf("overview missing %q:\n%s", want, out)
	}
}

// TestFormatStatsRoundsTable pins the table shape: column headers, the cliff
// star and the probe row marker.
func TestFormatStatsRoundsTable(t *testing.T) {
	_, rounds := statsRawLog(t)
	st := BuildStatsFromRounds(rounds)
	st.SessionID = "abc-123"
	out := FormatStatsRounds(st)
	for _, want := range []string{
		sessionRoundColumnWord,
		"PROMPT",
		"Δreq",
		sessionevent.CounterUnitRawRequest + " ordinal, 1-based",
		"2p", // probe round keeps its row, marked
		"9.4K",
		"+124",
		"read,bash",
		"tool_use",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--rounds missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(* = trim cliff") {
		t.Errorf("--rounds printed the cliff footnote with no cliff marked:\n%s", out)
	}

	// A jsonl-sourced block states the CANONICAL basis — the same EXCHANGES
	// every session surface numbers (one `request` row = one HTTP
	// request/response), 1-based — so a row here is the same exchange
	// `gopi session log <id> N` prints. The raw-sourced basis is a different
	// corpus and says so; the two are never cross-indexed (BuildStatsFromJSONL
	// is the one that assigns canonical ordinals).
	js := BuildStatsFromUsage(&SessionUsage{SessionID: "s", Rounds: []UsageRound{{PromptTokens: 10}}})
	if out := FormatStatsRounds(js); !strings.Contains(out, sessionevent.CounterUnitRound+", 1-based") {
		t.Errorf("jsonl exchanges table must state the canonical basis:\n%s", out)
	}
}

// TestFormatStatsToolsRanking pins the ranking order and the row cap.
func TestFormatStatsToolsRanking(t *testing.T) {
	st := &SessionStats{ToolCalls: 5}
	for _, n := range []string{"read", "bash", "bash", "edit", "edit"} {
		st.addTool(n, 1)
	}
	st.ToolCalls = 5
	st.finish()
	out := FormatStatsTools(st)
	if !strings.Contains(out, "5 calls / 3 kinds") {
		t.Errorf("tools header wrong:\n%s", out)
	}
	bashIdx := strings.Index(out, "  bash")
	editIdx := strings.Index(out, "  edit")
	readIdx := strings.Index(out, "  read")
	if bashIdx > editIdx || editIdx > readIdx {
		t.Errorf("ranking not calls-desc-then-name (bash/edit/read):\n%s", out)
	}
}

// TestFormatStatsToolsEmpty keeps the no-calls case honest.
func TestFormatStatsToolsEmpty(t *testing.T) {
	if out := FormatStatsTools(&SessionStats{}); !strings.Contains(out, "(no tool calls)") {
		t.Errorf("empty ranking output = %q", out)
	}
}

// TestFormatStatsJSONRoundTrip checks the machine-readable shape parses and
// carries the derived numbers.
func TestFormatStatsJSONRoundTrip(t *testing.T) {
	_, rounds := statsRawLog(t)
	st := BuildStatsFromRounds(rounds)
	st.SessionID = "abc-123"
	out, err := FormatStatsJSON(st)
	if err != nil {
		t.Fatalf("FormatStatsJSON: %v", err)
	}
	var back struct {
		SessionID   string      `json:"session_id"`
		Source      string      `json:"source"`
		Prompt      int         `json:"prompt_tokens"`
		Avg         int         `json:"prompt_avg"`
		CacheHitPct float64     `json:"cache_hit_pct"`
		Rounds      int         `json:"rounds"`
		RoundStats  []RoundStat `json:"round_stats"`
	}
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("json unmarshal: %v\n%s", err, out)
	}
	if back.SessionID != "abc-123" || back.Source != StatsSourceRaw || back.Rounds != 3 {
		t.Errorf("json headline = %+v", back)
	}
	if back.Prompt != st.PromptTokens || back.Avg != st.PromptTokens/3 {
		t.Errorf("json prompt/avg = %d/%d, want %d/%d", back.Prompt, back.Avg, st.PromptTokens, st.PromptTokens/3)
	}
	if len(back.RoundStats) != 3 {
		t.Errorf("json round rows = %d, want 3", len(back.RoundStats))
	}
}

// TestAggregateStatsSums checks the --all aggregate.
func TestAggregateStatsSums(t *testing.T) {
	a := &SessionStats{Source: StatsSourceRaw, Sessions: 1, Model: "m1", Rounds: 3,
		PromptTokens: 100, CacheHit: 90, CacheMiss: 10, Completion: 20, ToolCalls: 4,
		MinPrompt: 20, MaxPrompt: 50, CliffCount: 1, LargestCliff: -9000,
		ToolKinds: []ToolCount{{"read", 4}}, HasWall: true, WallSeconds: 10}
	b := &SessionStats{Source: StatsSourceJSONL, Sessions: 1, Model: "m2", Rounds: 2,
		PromptTokens: 300, CacheHit: 200, CacheMiss: 100, Completion: 40, ToolCalls: 1,
		MinPrompt: 100, MaxPrompt: 200, CliffCount: 2, LargestCliff: -12000,
		ToolKinds: []ToolCount{{"bash", 1}}, HasWall: true, WallSeconds: 5}
	agg := AggregateStats([]*SessionStats{a, b})
	if agg.Source != StatsSourceAggregate || agg.Sessions != 2 {
		t.Errorf("aggregate source/sessions = %q/%d", agg.Source, agg.Sessions)
	}
	if agg.Rounds != 5 || agg.PromptTokens != 400 || agg.Completion != 60 || agg.ToolCalls != 5 {
		t.Errorf("aggregate sums = %+v", agg)
	}
	if agg.MinPrompt != 20 || agg.MaxPrompt != 200 {
		t.Errorf("aggregate min/max = %d/%d, want 20/200", agg.MinPrompt, agg.MaxPrompt)
	}
	if agg.CliffCount != 3 || agg.LargestCliff != -12000 {
		t.Errorf("aggregate cliffs = %d/%d, want 3/-12000", agg.CliffCount, agg.LargestCliff)
	}
	if agg.Model != "(mixed)" {
		t.Errorf("aggregate model = %q, want (mixed)", agg.Model)
	}
	if !agg.HasWall || agg.WallSeconds != 15 {
		t.Errorf("aggregate wall = %v/%v, want 15/true", agg.WallSeconds, agg.HasWall)
	}
	if len(agg.ToolKinds) != 2 {
		t.Errorf("aggregate tool kinds = %+v", agg.ToolKinds)
	}
}

// TestRawLogLineSpanRuneSafeCap checks the 64MiB-style byte cap never splits a
// rune (the CJK-slicing bug class the review P1 called out).
func TestRawLogLineSpanRuneSafeCap(t *testing.T) {
	s := strings.Repeat("知", 300) // 900 bytes of 3-byte runes
	got, cut := capRuneSafe(s, 100)
	if !cut {
		t.Fatal("capRuneSafe reported no cut")
	}
	if !strings.HasSuffix(got, "知") {
		t.Errorf("cut split a rune: tail %q", got[len(got)-6:])
	}
	if len(got) > 100 {
		t.Errorf("capRuneSafe returned %d bytes, want ≤ 100", len(got))
	}
	if full, cut := capRuneSafe(s, 9000); cut || full != s {
		t.Error("capRuneSafe cut a string that fits")
	}
}

// TestHumanTokensRendering pins the number renderings the output is built on
// (the FINAL-USAGE-PROPOSAL mock values are the reference — decimal K/M).
func TestHumanTokensRendering(t *testing.T) {
	for _, c := range []struct {
		in       int
		table    string
		box      string
		positive string
	}{
		{13170, "13.2K", "13K", "13.2K"},
		{32861953, "32.86M", "32.86M", "32.86M"},
		{337000, "337.0K", "337K", "337.0K"},
		{999, "999", "999", "999"},
		{10000, "10.0K", "10K", "10.0K"},
	} {
		if got := humanTokens(c.in); got != c.table {
			t.Errorf("humanTokens(%d) = %q, want %q", c.in, got, c.table)
		}
		if got := humanTokensAuto(c.in); got != c.box {
			t.Errorf("humanTokensAuto(%d) = %q, want %q", c.in, got, c.box)
		}
		if got, want := signedTokens(c.in), "+"+c.positive; got != want {
			t.Errorf("signedTokens(+%d) = %q, want %q", c.in, got, want)
		}
		if got, want := signedTokens(-c.in), "-"+c.positive; got != want {
			t.Errorf("signedTokens(-%d) = %q, want %q", c.in, got, want)
		}
	}
	if got := signedTokens(0); got != "0" {
		t.Errorf("signedTokens(0) = %q, want 0", got)
	}
	if got := commaInt(32861953); got != "32,861,953" {
		t.Errorf("commaInt = %q", got)
	}
	if got := commaInt(999); got != "999" {
		t.Errorf("commaInt(999) = %q", got)
	}
	if got := commaInt(1000); got != "1,000" {
		t.Errorf("commaInt(1000) = %q", got)
	}
	if got := humanWall(2680.97); got != "44m41s" {
		t.Errorf("humanWall(2680.97) = %q, want 44m41s", got)
	}
	if got := humanWall(45.2); got != "45s" {
		t.Errorf("humanWall(45.2) = %q, want 45s", got)
	}
}
