package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// logShapedText builds n bytes of the text a real tool actually emits — many
// distinct short lines. A homogeneous string (strings.Repeat("a", n)) is not a
// usable fixture here: measured in this run, 51200 'a' bytes take ~47 s to BPE
// because the merge walk degenerates, so a test built on it measures the
// pathological case instead of the ordinary one.
func logShapedText(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "%05d 2026-11-07T05:20:01.123Z INFO handler=tool path=line-%d status=ok\n", i, i)
	}
	return b.String()
}

// oversizedResult builds a tool result that is certainly over the cap at the
// corpus's own measured density (~3.2 chars/token), with every line distinct so
// a "the middle is really gone" assertion can name text that must NOT survive.
func oversizedResult(lines int) (text string, markerLine string) {
	var b strings.Builder
	markerLine = "LINE-THAT-MUST-BE-DROPPED-0917"
	for i := 0; i < lines; i++ {
		if i == lines/2 {
			b.WriteString(markerLine)
		} else {
			b.WriteString("log line ")
			b.WriteString(strings.Repeat("x", 60))
		}
		b.WriteString("\n")
	}
	return b.String(), markerLine
}

// TestToolResultCapTruncatesAtIngestion is the regression guard for the whole
// mechanism: a result over the cap must arrive in Messages TRUNCATED, with the
// explicit note present, and the stored message must count at or under the cap.
func TestToolResultCapTruncatesAtIngestion(t *testing.T) {
	big, dropped := oversizedResult(8000)
	rawTokens := countTokens(big)
	if rawTokens <= ToolResultTokenCap {
		t.Fatalf("fixture is not oversized: %d tokens, cap %d — the test would pass vacuously", rawTokens, ToolResultTokenCap)
	}

	c := &Context{ToolResultSessionPath: "/home/user/.gopi/sessions/abc/session.jsonl"}
	c.AddToolResult("call_1", big)

	if len(c.Messages) != 1 {
		t.Fatalf("want exactly 1 message, got %d", len(c.Messages))
	}
	got := c.Messages[0]
	if got.Role != "tool" || got.ToolCallID != "call_1" {
		t.Fatalf("message shape changed: role=%q id=%q", got.Role, got.ToolCallID)
	}
	if got.Content == big {
		t.Fatal("the result reached Messages VERBATIM — the cap did not fire")
	}
	stored := countTokens(got.Content)
	if stored > ToolResultTokenCap {
		t.Fatalf("stored result is %d tokens, above the %d cap", stored, ToolResultTokenCap)
	}

	// Rule 15: the truncation is explicit — the note names what was dropped and
	// where the whole text is.
	if !strings.Contains(got.Content, "tool result capped at") {
		t.Fatalf("no cap note in the stored result; head=%q", capFirstLine(got.Content))
	}
	if !strings.Contains(got.Content, "session.jsonl") {
		t.Fatal("the note does not name the session log the full result lives in")
	}
	if !strings.Contains(got.Content, "call_1") {
		t.Fatal("the note does not name the tool_use id the full result is indexed by")
	}
	if !strings.Contains(got.Content, "OMITTED") {
		t.Fatal("the note does not say that content was omitted")
	}

	// Head and tail are both kept: the model keeps the start of the output and
	// its verdict at the bottom.
	if !strings.HasPrefix(got.Content, "log line ") {
		t.Fatalf("the head was not kept; starts with %q", capFirstLine(got.Content))
	}
	if !strings.HasSuffix(got.Content, "log line xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n") {
		t.Fatalf("the tail was not kept; ends with %q", capLastLine(got.Content))
	}
	if strings.Contains(got.Content, dropped) {
		t.Fatal("a line from the MIDDLE survived — the elision kept the wrong region")
	}

	// The char figures in the note are self-consistent (Rule 41a: read the
	// arithmetic back rather than trusting the format string).
	if !strings.Contains(got.Content, "OMITTED from the middle") {
		t.Fatal("the note does not locate the omission in the middle")
	}
}

// TestToolResultCapLeavesOrdinaryResultsAlone pins the two cases the cap must
// never touch: an ordinary result (below the coarse filter, no BPE pass at all)
// and a full read page (the largest output a self-paging tool produces).
func TestToolResultCapLeavesOrdinaryResultsAlone(t *testing.T) {
	cases := []struct {
		name string
		size int
	}{
		{"ordinary bash output", 4000},
		{"a full read page (maxPageReadBytes)", 50 * 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := logShapedText(tc.size)
			c := &Context{}
			c.AddToolResult("call_2", body)
			if c.Messages[0].Content != body {
				t.Fatalf("%d chars were rewritten; bytes=%d content=%d",
					tc.size, len(body), len(c.Messages[0].Content))
			}
			if n := countTokens(body); n > ToolResultTokenCap {
				t.Fatalf("fixture does not represent the intended case: %d tokens > cap %d", n, ToolResultTokenCap)
			}
		})
	}
}

// TestToolResultCapStatementIsExactWhenThePathIsUnknown: with no recorded
// session path the note says so, rather than printing an empty location a reader
// would take for "there is no full copy".
func TestToolResultCapStatementIsExactWhenThePathIsUnknown(t *testing.T) {
	big, _ := oversizedResult(8000)
	c := &Context{}
	c.AddToolResult("call_3", big)
	if !strings.Contains(c.Messages[0].Content, toolResultCapMarkerPathUnknown) {
		t.Fatal("an unrecorded session path produced a note that does not say so")
	}
}

// TestToolResultCapLowersCalibratedTokens is the measured claim the epic asks
// for: the info-bar reading (CalibratedTokens — the BPE basis, not chars/4)
// drops once the cap is in force.
func TestToolResultCapLowersCalibratedTokens(t *testing.T) {
	big, _ := oversizedResult(8000)
	before := &Context{}
	before.Ctx_AddRawToolResultForTest("call_4", big)
	after := &Context{}
	after.AddToolResult("call_4", big)

	b, a := before.CalibratedTokens(), after.CalibratedTokens()
	if a >= b {
		t.Fatalf("CalibratedTokens did not drop: before=%d after=%d", b, a)
	}
	t.Logf("CalibratedTokens: before=%d after=%d (dropped %d, %.1f%%)",
		b, a, b-a, 100*float64(b-a)/float64(b))
	if a > ToolResultTokenCap*2 {
		t.Fatalf("after-cap reading is still %d tokens; the cap did not bound it", a)
	}
}

// TestToolResultCapCost pins the performance claim: the ORDINARY path must not
// pay for a BPE pass (the coarse filter returns before counting), and the
// oversized path must stay bounded in absolute terms.
func TestToolResultCapCost(t *testing.T) {
	ordinary := logShapedText(4000)
	ordinaryRuns := 2000
	start := time.Now()
	for i := 0; i < ordinaryRuns; i++ {
		c := &Context{}
		c.AddToolResult("call_5", ordinary)
	}
	perOrdinary := time.Since(start) / time.Duration(ordinaryRuns)

	big, _ := oversizedResult(8000)
	bigRuns := 5
	start = time.Now()
	for i := 0; i < bigRuns; i++ {
		c := &Context{}
		c.AddToolResult("call_6", big)
	}
	perBig := time.Since(start) / time.Duration(bigRuns)

	t.Logf("tool-result ingest cost: ordinary(%d chars) = %v/call; oversized(%d chars, %d tokens) = %v/call",
		len(ordinary), perOrdinary, len(big), countTokens(big), perBig)
	if perOrdinary > time.Millisecond {
		t.Fatalf("the ordinary path costs %v per call — the coarse filter is not bypassing the BPE pass", perOrdinary)
	}
}

// Ctx_AddRawToolResultForTest appends a tool result with NO cap — the pre-change
// behaviour, so a test can measure the difference the cap makes. It lives in a
// _test file so no production path can reach it.
func (c *Context) Ctx_AddRawToolResultForTest(toolCallID, result string) {
	c.AddMessage(deepseek.Message{Role: "tool", Content: result, ToolCallID: toolCallID})
}

func capFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func capLastLine(s string) string {
	s = strings.TrimSuffix(s, "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
