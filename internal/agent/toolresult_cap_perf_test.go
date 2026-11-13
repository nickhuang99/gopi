package agent

import (
	"strings"
	"testing"
	"time"
)

// TestToolResultCapCostMeasure measures the cap's extra cost in the two cases the
// epic asks about, and PRINTS them (the figures go into the task report).
//
// It is a test rather than a benchmark because the claim is absolute — "the
// ordinary path costs no BPE pass at all" — and an absolute assertion is the one
// a regression breaks.
func TestToolResultCapCostMeasure(t *testing.T) {
	_ = countTokens("warm the tokenizer parse") // one-time parse, outside every measurement

	ordinary := logShapedText(400)   // 4xx chars: a bash result, a small read
	page := logShapedText(50 * 1024) // maxPageReadBytes: the largest self-paged output
	huge := logShapedText(1000000)   // 1 MB: the pathological tail
	if n := countTokens(huge); n <= ToolResultTokenCap {
		t.Fatalf("the %d-char fixture is only %d tokens — not the case under test", len(huge), n)
	}

	measure := func(name, body string, runs int) time.Duration {
		_ = (&Context{}).capToolResult("warm", body)
		start := time.Now()
		for i := 0; i < runs; i++ {
			c := &Context{}
			c.AddToolResult("call_perf", body)
		}
		per := time.Since(start) / time.Duration(runs)
		t.Logf("%-22s chars=%7d tokens=%7d  AddToolResult=%9v/call",
			name, len(body), countTokens(body), per)
		return per
	}

	ordinaryCost := measure("ordinary", ordinary, 2000)
	pageCost := measure("read page (50KiB)", page, 200)
	hugeCost := measure("oversized (1MB)", huge, 3)

	t.Logf("cap = %d tokens", ToolResultTokenCap)
	if ordinaryCost > 10*time.Microsecond {
		t.Fatalf("ordinary path costs %v — it must be a memcmp-class byte-length test with no BPE call", ordinaryCost)
	}
	// A full read page is ABOVE the free tier's byte bound (50 KiB > 32 KiB), so
	// it pays ONE sample count — measured 14 ms here — and must then be returned
	// whole. That is the tier's design: the free tier exists so ORDINARY results
	// cost nothing, and the sample tier bounds the price of the large ones.
	if pageCost > 40*time.Millisecond {
		t.Fatalf("a full read page costs %v — one bounded sample count should be ~15 ms", pageCost)
	}
	if hugeCost > 5*time.Second {
		t.Fatalf("a 1MB result costs %v — the cap is supposed to bound this", hugeCost)
	}
}

// TestToolResultCapKeepsAReadPageWhole is the constraint the epic states: the
// cap must not become a second pagination for `read`. It is asserted on the
// WORST case — the densest text a read page could carry — not on prose.
func TestToolResultCapKeepsAReadPageWhole(t *testing.T) {
	dense := strings.Repeat("0", 50*1024) // ~19.7k tokens at this host's measured density
	c := &Context{}
	c.AddToolResult("call_page", dense)
	if c.Messages[0].Content != dense {
		t.Fatalf("a %d-byte read page was capped (%d tokens) — read's paging is being overridden",
			len(dense), countTokens(dense))
	}
}
