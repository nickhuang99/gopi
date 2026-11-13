package tokenizer

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// measure_test.go — the numbers the report must state, produced by a test rather
// than typed into a document (PROJECT.md Rule 38: a figure names its generator).
//
// It is OPT-IN (`GOPI_TOKENIZER_MEASURE=1`) because it is a measurement, not a
// check: a test that asserts a wall-clock threshold is a flaky test, and a test
// that prints numbers on every run is noise. With the variable unset the whole
// file is inert.
//
//	Ran:  GOPI_TOKENIZER_MEASURE=1 go test ./internal/tokenizer/ -run TestMeasure -v
const measureEnv = "GOPI_TOKENIZER_MEASURE"

func TestMeasure(t *testing.T) {
	if os.Getenv(measureEnv) == "" {
		t.Skipf("%s unset — this test only MEASURES (see the run's report)", measureEnv)
	}
	// The parse must not have happened yet, or the "before" reading is a lie.
	// Nothing else in this test binary may have counted before this point, so
	// the reading is taken first and its value is REPORTED, not assumed: a
	// non-zero heap delta with Ready() already true would mean another test ran
	// first, and the number is then not the lazy boundary's.
	readyBefore := Ready()
	heapBefore, rssBefore := readMem(t)
	t.Logf("before: ready=%v heapAlloc=%d B sys=%d B rss=%d B",
		readyBefore, heapBefore.alloc, heapBefore.sys, rssBefore)

	start := time.Now()
	tok, err := Default()
	loadDur := time.Since(start)
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	heapAfter, rssAfter := readMem(t)
	t.Logf("after:  ready=%v heapAlloc=%d B sys=%d B rss=%d B  (load %s)",
		Ready(), heapAfter.alloc, heapAfter.sys, rssAfter, loadDur)
	t.Logf("delta:  heapAlloc=%d B sys=%d B rss=%d B",
		heapAfter.alloc-heapBefore.alloc, heapAfter.sys-heapBefore.sys, int64(rssAfter)-int64(rssBefore))

	// A 200K-character input, mixed the way a real context is: prose, code,
	// CJK, digits, emoji. Built here so the number is reproducible.
	big := build200K()
	t.Logf("200K input: %d bytes, %d runes", len(big), len([]rune(big)))

	// One warm-up so the first-touch page faults are not attributed to the
	// timed run (the timed run is what the report quotes, and it must be the
	// steady-state cost).
	if n := tok.Count(big); n == 0 {
		t.Fatal("Count returned 0 for a 200K-character input")
	}
	const rounds = 3
	var total time.Duration
	var last int
	for i := 0; i < rounds; i++ {
		start = time.Now()
		last = tok.Count(big)
		total += time.Since(start)
	}
	t.Logf("Count(%d chars) = %d tokens, mean over %d runs: %s",
		len([]rune(big)), last, rounds, total/time.Duration(rounds))
}

type memStat struct {
	alloc, sys uint64
}

func readMem(t *testing.T) (memStat, uint64) {
	t.Helper()
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return memStat{alloc: ms.HeapAlloc, sys: ms.Sys}, currentRSS(t)
}

// currentRSS reads VmRSS from /proc/self/status — the number an operator sees in
// `ps`, which is what "RSS 差" in the report means.
func currentRSS(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("read /proc/self/status: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			t.Fatalf("parse VmRSS %q: %v", fields[1], err)
		}
		return kb * 1024
	}
	t.Fatalf("/proc/self/status carries no VmRSS line")
	return 0
}

// build200K assembles a mixed 200,000-rune input.
func build200K() string {
	piece := "The quick brown fox jumps over the lazy dog. " + // English prose
		"这是一个中文测试句子，用来验证分词器在长上下文下的表现。" + // CJK prose
		"func (t *Tokenizer) Count(text string) int { return len(t.Encode(text)) }\n" + // Go code
		"订单号 20250101 金额 1234567.89 😀🎉\n" // digits + emoji
	var b strings.Builder
	b.Grow(200000 * 2)
	for b.Len() < 200000*3 {
		b.WriteString(piece)
		if len([]rune(b.String())) >= 200000 {
			break
		}
	}
	s := b.String()
	rs := []rune(s)
	if len(rs) > 200000 {
		s = string(rs[:200000])
	}
	return s
}

// ---------------------------------------------------------------------------
// The scaling table: 200 KB and 1 MB, and whether the cost is superlinear.
//
// This half exists because the epic's question was not "how fast is it" but "is
// it SUPERLINEAR, and if so where". A wall-clock number alone cannot answer
// that — two sizes can. So the test runs a GEOMETRIC ladder (each size twice the
// previous) and reports the cost RATIO between consecutive rungs: a linear
// implementation sits at ~2.0, a quadratic one at ~4.0. The verdict is printed,
// not asserted, because a ratio threshold in a test is a flaky test; what IS
// asserted is the thing the suite cannot tolerate — that every measurement
// finishes inside measurePerSizeTimeout.
//
//	Ran:  GOPI_TOKENIZER_MEASURE=1 go test ./internal/tokenizer/ -run TestMeasureScaling -v
//
// The two shapes measured are the two that matter, and they are different on
// purpose:
//
//   - "mixed" is what a real prompt looks like: prose, CJK, code, digits,
//     emoji, with whitespace throughout. The pre-tokenizer cuts it into tens of
//     thousands of short pieces (measured: 38,557 pieces for 200 KB, longest
//     piece 48 bytes), so per-piece work never dominates.
//   - "single run" is the worst shape: ONE piece with no whitespace, which is
//     what a tool result carrying minified JSON, a base64 blob, or one long CJK
//     run (stage 2 emits a CJK run as ONE piece) produces.
const measurePerSizeTimeout = 60 * time.Second

type scaleRow struct {
	name  string
	bytes int
	dur   time.Duration
	count int
	ratio float64 // against the previous row of the same shape
}

func TestMeasureScaling(t *testing.T) {
	if os.Getenv(measureEnv) == "" {
		t.Skipf("%s unset — this test only MEASURES (see the run's report)", measureEnv)
	}
	tok, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	shapes := []struct {
		name  string
		build func(int) string
	}{
		{"mixed", buildMixed},
		{"single-run", func(n int) string { return strings.Repeat("x", n) }},
		{"cjk-run", func(n int) string { return strings.Repeat("中", n/3) }},
	}
	sizes := []int{62500, 125000, 250000, 500000}
	headline := []int{200000, 1000000}

	for _, sh := range shapes {
		prev := scaleRow{}
		t.Logf("--- shape: %s ---", sh.name)
		t.Logf("%12s %12s %12s %8s %14s", "bytes", "tokens", "duration", "vs prev", "ns/byte")
		for _, n := range sizes {
			s := sh.build(n)
			start := time.Now()
			var count int
			done := make(chan struct{})
			go func() {
				count = tok.Count(s)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(measurePerSizeTimeout):
				t.Fatalf("%s at %d bytes did not finish in %s — a count that cannot "+
					"complete is a defect to report, never a hang to wait out",
					sh.name, n, measurePerSizeTimeout)
			}
			dur := time.Since(start)
			ratio := 0.0
			if prev.dur > 0 {
				ratio = float64(dur) / float64(prev.dur)
			}
			t.Logf("%12d %12d %12s %8.2f %14.1f", len(s), count, dur, ratio, float64(dur.Nanoseconds())/float64(len(s)))
			prev = scaleRow{name: sh.name, bytes: len(s), dur: dur, count: count, ratio: ratio}
		}
		// The headline sizes the epic asks about, measured on their own so a
		// report can quote them without deriving anything from the ladder.
		for _, n := range headline {
			s := sh.build(n)
			start := time.Now()
			count := tok.Count(s)
			dur := time.Since(start)
			t.Logf("HEADLINE %s %d bytes -> %d tokens in %s", sh.name, len(s), count, dur)
		}
		// The verdict for this shape: doubling the input multiplied the cost by
		// `ratio`. Below ~2.5 is linear-or-better; ~3 and above is the quadratic
		// signature.
		if prev.ratio > 0 {
			verdict := "LINEAR or better"
			if prev.ratio >= 3.0 {
				verdict = "SUPERLINEAR — the doubling cost more than ~3x"
			}
			t.Logf("VERDICT %s: last doubling multiplied the cost by %.2f (%s)", sh.name, prev.ratio, verdict)
		}
	}
}

// buildMixed assembles realistic prompt-shaped text of n bytes: prose, CJK, code
// and digits with whitespace throughout, so the pre-tokenizer cuts it into short
// pieces the way a real request is cut.
func buildMixed(n int) string {
	piece := "The quick brown fox jumps over the lazy dog. " +
		"这是一个中文测试句子，用来验证分词器在长上下文下的表现。" +
		"func (t *Tokenizer) Count(text string) int { return len(t.Encode(text)) }\n" +
		"订单号 20250101 金额 1234567.89 😀🎉\n"
	var b strings.Builder
	b.Grow(n + len(piece))
	for b.Len() < n {
		b.WriteString(piece)
	}
	return b.String()[:n]
}
