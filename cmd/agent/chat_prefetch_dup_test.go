package main

// chat_prefetch_dup_test.go — ONE typed line is retrieved ONCE (task 01a11601).
//
// THE DEFECT THIS PINS, measured before it was fixed. The session hands the
// operator's line to TWO machines: the typing path (chat_prefetch.go's loop +
// memoryprefetch.Session, which fires on a pause) and the turn boundary
// (chat_prefetch_turn.go, which fires once per sealed turn). For an ordinary
// TYPED line both machines are handed the SAME text, so both built the SAME key
// and both spent a retrieval on it. Measured on a real `bin/gopi chat` over a
// pty, one line typed in full and one Enter (evidence: the run's own record):
//
//	{"query":"worktree base tip master fetch","hits":1,"outcome":"hits","adopted":"yes"}
//	{"query":"worktree base tip master fetch","hits":1,"outcome":"hits","adopted":"no","source":"human"}
//
// Two lookups, one question, two prompts' worth of ledger work for one turn —
// the shape the field audit counted 15 times in a live session.
//
// WHAT IS NOT A DEFECT, and this case says so by construction: the turn boundary
// EXISTS for the four faces the typing sampler cannot see (a notify-delivered
// turn, a whole-block paste whose words are in a spilled file, a /queue'd
// prompt, a model-typed line). Those turns carry a key the typing machine never
// built, so they must still retrieve — the sharing below is keyed on the QUERY,
// never on "the turn boundary should be quieter".
//
// The fixture is the smallest one that can show the duplication: one line, one
// Enter, the real functions the REPL calls (chatPrefetchAdoptTurn and the
// executor's own fetch), a real read-only ledger.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/inputsource"
)

// dupLine is the line the case types. Named (Rule 1) because both the typing
// path's gate and the turn boundary's gate must accept it, and its words must
// HIT the fixture ledger — a line with no hits would leave the turn boundary
// nothing to share and the case would pass vacuously.
const dupLine = "notify turn boundary payload retrieval"

// dupRecordLine is the record's wire shape as far as THIS case reads it — a
// second decoding of the same file the sibling cases decode, deliberately (a
// struct shared with production would not notice a renamed field).
type dupRecordLine struct {
	Query   string `json:"query"`
	Hits    int    `json:"hits"`
	Outcome string `json:"outcome"`
	Adopted string `json:"adopted"`
	Source  string `json:"source"`
}

// dupRecordLines decodes the run's record.
func dupRecordLines(t *testing.T, dir string) []dupRecordLine {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, config.MemoryPrefetchRecordName))
	if err != nil {
		t.Fatalf("read the prefetch record: %v", err)
	}
	var out []dupRecordLine
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var r dupRecordLine
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			t.Fatalf("the record line %q is not decodable JSON: %v", ln, err)
		}
		out = append(out, r)
	}
	return out
}

// dupLookupCount counts the record's RETRIEVAL lines: the ones that actually ran
// a query (outcome=hits is what a query that came back with rows records). The
// adoption and supersession lines are events ABOUT a lookup, not lookups, which
// is why the acceptance counts this one outcome.
func dupLookupCount(lines []dupRecordLine) int {
	n := 0
	for _, l := range lines {
		if l.Outcome == prefetchOutcomeHits {
			n++
		}
	}
	return n
}

// TestOneTypedLineIsRetrievedOnceAtTheTurnBoundary is the acceptance: one line
// typed and one Enter produce ONE retrieval, the section still reaches the
// prompt, and the adoption is still recorded with the face it came from.
func TestOneTypedLineIsRetrievedOnceAtTheTurnBoundary(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	t.Setenv("GOPI_MEMORY_DB", seedTurnLedger(t))
	dir := t.TempDir()
	line := dupLine
	p := chatPrefetchFor(dir, func() string { return line }, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	// THE TYPING PATH, driven the way its own loop drives it: the differential
	// claims the line, the lookup runs, and the result is stored for the turn
	// that is about to be sealed.
	query, ok := p.session.Claim(line, time.Now(), p.cfg)
	if !ok {
		t.Fatalf("the typing gate refused the line %q — this case needs the line to be retrievable on BOTH paths", line)
	}
	p.fetch(query)

	typed := dupRecordLines(t, dir)
	if n := dupLookupCount(typed); n != 1 {
		t.Fatalf("the typing path alone recorded %d lookup line(s), want 1:\n%s", n, dupRecordRaw(typed))
	}
	if typed[0].Hits == 0 {
		t.Fatalf("the fixture ledger answered the lookup with 0 hits, so there is no section for the turn boundary to share — the case would prove nothing:\n%s",
			dupRecordRaw(typed))
	}

	// THE TURN BOUNDARY for the SAME line, the way the REPL calls it at the seal
	// (cli_chat.go): the same terms, the same message.
	out := chatOutbound{Message: line, Display: line}
	got, adopted := chatPrefetchAdoptTurn(out, p, inputsource.Human, []string{line})
	if !adopted {
		t.Fatalf("the turn boundary adopted no section although the typing path held one for this exact key:\n%s",
			dupRecordRaw(dupRecordLines(t, dir)))
	}
	if got.Display != out.Display {
		t.Errorf("Display was modified: %q", got.Display)
	}
	if !strings.Contains(got.Prefetch.Section, "memory-prefetch") {
		t.Errorf("the shared section did not ride the message:\n%s", got.describe())
	}

	all := dupRecordLines(t, dir)
	if n := dupLookupCount(all); n != 1 {
		t.Fatalf("one line typed + one Enter recorded %d outcome=%s lookup line(s), want 1 — the same question was paid for twice:\n%s",
			n, prefetchOutcomeHits, dupRecordRaw(all))
	}
	foundSource := false
	for _, l := range all {
		// The adoption of a SHARED section is recorded as `shared` (no query ran),
		// the adoption of the turn's own lookup as `adopted`. Both are adoptions,
		// and the acceptance is about the FACE being named, which is the field
		// this loop reads.
		if (l.Outcome == prefetchOutcomeAdopted || l.Outcome == prefetchOutcomeShared) &&
			l.Adopted == prefetchRecordAdoptedYes && l.Source == inputsource.Human {
			foundSource = true
		}
	}
	if !foundSource {
		t.Errorf("the adoption is not recorded with the face it came from (%q):\n%s",
			inputsource.Human, dupRecordRaw(all))
	}
}

// TestTheTurnBoundaryStillRetrievesAKeyTheTypingPathNeverBuilt is the negative
// control, and without it the fix above could be "make the turn boundary
// quieter" — which would silently delete the four faces it exists for. A
// whole-block PASTE is the sharpest of them: the line the editor holds is a
// REFERENCE (a path and a size) and the words that matter are in the spilled
// file, so the key the turn boundary builds is NOT the key the typing path built
// and the retrieval must still happen.
func TestTheTurnBoundaryStillRetrievesAKeyTheTypingPathNeverBuilt(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	t.Setenv("GOPI_MEMORY_DB", seedTurnLedger(t))
	dir := t.TempDir()
	p := chatPrefetchFor(dir, nil, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	// The typing path saw ONLY the reference line (it is what the editor
	// published), and the reference's own "words" are a path and a size — not the
	// turn's question.
	refLine := "[paste 1: /tmp/dup-probe-payload.txt 5411 bytes]"
	if q, ok := p.session.Claim(refLine, time.Now(), p.cfg); ok {
		p.fetch(q)
	}
	before := dupLookupCount(dupRecordLines(t, dir))

	const payload = "notify turn boundary payload retrieval behind a spilled reference"
	out, adopted := chatPrefetchAdoptTurn(chatOutbound{Message: refLine}, p, "paste", []string{payload})
	if !adopted {
		t.Fatalf("the turn boundary skipped a payload-derived key the typing path never built — the face it exists for is gone:\n%s",
			dupRecordRaw(dupRecordLines(t, dir)))
	}
	if !strings.Contains(out.Prefetch.Section, "memory-prefetch") {
		t.Errorf("the payload-derived section did not ride the message:\n%s", out.describe())
	}
	if after := dupLookupCount(dupRecordLines(t, dir)); after != before+1 {
		t.Errorf("the payload-derived lookup was not recorded exactly once: before=%d after=%d", before, after)
	}
}

// dupRecordRaw renders the record for a failure message: the whole file, in
// order, so a reviewer reads what was written rather than a summary of it.
func dupRecordRaw(lines []dupRecordLine) string {
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(strings.Join([]string{
			"[" + string(rune('0'+i%10)) + "] ",
			"query=" + l.Query, " hits=" + string(rune('0'+l.Hits%10)),
			" outcome=" + l.Outcome, " adopted=" + l.Adopted, " source=" + l.Source, "\n",
		}, ""))
	}
	return b.String()
}
