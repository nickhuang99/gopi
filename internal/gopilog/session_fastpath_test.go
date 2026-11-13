package gopilog

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sessionlayout"
)

// writeRefRecord writes a session record whose session_start names id — the
// one fact every id-based resolution reads.
func writeRefRecord(t *testing.T, path, id string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := `{"type":"session_start","sessionId":"` + id + `","model":"m","timestamp":"2026-08-06T09:00:00+08:00"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestFindSession_ExactIdResolvesInEveryLayout pins the NAME-based resolution
// (FindSession's fast path) against the layout rules: each of the three
// layouts names the record after the id its session_start reports, and the
// resolution must find that file WITHOUT falling back to the tree walk (which
// is also why a mutation here — a candidate the walk would have found — is
// caught: only the name-derived candidates are consulted).
func TestFindSession_ExactIdResolvesInEveryLayout(t *testing.T) {
	root := t.TempDir()

	const (
		idFlat    = "11111111-1111-4111-8111-111111111111"
		idTaskRun = "22222222-2222-4222-8222-222222222222"
		idBare    = "33333333-3333-4333-8333-333333333333"
		idSide    = "44444444-4444-4444-8444-444444444444"
	)
	wantFlat := writeRefRecord(t, filepath.Join(root, idFlat+sessionlayout.JSONLSuffix), idFlat)
	wantTaskRun := writeRefRecord(t, filepath.Join(root, "t-fastpath", idTaskRun+sessionlayout.JSONLSuffix), idTaskRun)
	wantBare := writeRefRecord(t, filepath.Join(root, idBare, sessionlayout.RecordName), idBare)
	// A registered side file whose OWN head names idSide: it is an artifact of
	// the session in that directory, never a session of its own.
	if err := os.MkdirAll(filepath.Join(root, idSide), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, idSide, "signal-provenance.jsonl"),
		[]byte(`{"type":"session_start","sessionId":"`+idSide+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{"legacy flat", idFlat, wantFlat},
		{"task run dir", idTaskRun, wantTaskRun},
		{"bare session dir", idBare, wantBare},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindSession(root, tc.id)
			if err != nil {
				t.Fatalf("FindSession(%s): %v", tc.id, err)
			}
			if got != tc.want {
				t.Errorf("FindSession(%s) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}

	// A side file is not a session, however it is spelled — and its own head
	// naming the operand must not turn into a resolution.
	if got, err := FindSession(root, idSide); err != nil || got != "" {
		t.Errorf("FindSession(side file id) = (%q, %v), want (\"\", nil)", got, err)
	}
	// An id no record names stays "not found" — never an error.
	if got, err := FindSession(root, "99999999-9999-4999-8999-999999999999"); err != nil || got != "" {
		t.Errorf("FindSession(unknown) = (%q, %v), want (\"\", nil)", got, err)
	}
	// A truncated id is still refused by name, never resolved by prefix.
	if _, err := FindSession(root, idFlat[:8]); err == nil {
		t.Error("FindSession(truncated id) = nil error, want a full-id refusal")
	}
}

// TestFindSession_ExactIdDoesNotReadEveryRecord is the regression guard for
// the walk `gopi session raw <id>` / `gopi session log <id>` used to pay: exact-id resolution
// ran collectSessionFiles, which reads EVERY session record's head. Measured
// on the live profile (2026-09-25): 3,665 records across a 76,207-entry tree
// ⇒ 173,589 openat + 333,583 getdents64, ~3.2s to print a name it already had.
//
// THE ORACLE IS A COUNT, NOT A DURATION. sessionWalkProbe counts the tree walks
// and the record heads the shared enumerator reads, and the guard requires BOTH
// to stay at zero across the resolution. That is a property of the code path,
// so it means the same thing on an idle host and on a host running a parallel
// test-tool suite.
//
// What this replaced was a wall-clock ratio (resolution cost × 10 > full-walk
// cost), and the sentence it was justified with — "an absolute bound would be a
// benchmark of the host; a ratio is a property of the code" — is FALSE, measured
// red on 2026-10-27: on a loaded host the resolution measured 3.99 ms against a
// 30.66 ms walk of 2000 records and the guard failed with the fast path fully
// intact, while the same test passed 9/9 in isolation. Both operands are wall
// clock, so load moves the DENOMINATOR (the walk warms the page cache and drags
// under CPU contention) and the guard reports the host's load, not the code.
// The ratio is kept below as a LOG line — it is genuinely informative when a
// human reads a failure — but nothing asserts on it.
//
// THE RED PROOF IS IN THIS TREE, not in a mutated-and-restored file:
// GOPI_GOPILOG_SESSION_FASTPATH_OFF=1 (sessionFastPathDisabled) puts FindSession
// back on the pre-fix path, and with it this test MUST fail —
//
//	$ GOPI_GOPILOG_SESSION_FASTPATH_OFF=1 go test ./internal/gopilog \
//	      -run TestFindSession_ExactIdDoesNotReadEveryRecord -count=1
func TestFindSession_ExactIdDoesNotReadEveryRecord(t *testing.T) {
	root := t.TempDir()
	const decoys = 2000
	for i := 0; i < decoys; i++ {
		// Distinct ids, so the target cannot be found by accident.
		id := formatDecoyID(i)
		writeRefRecord(t, filepath.Join(root, id+sessionlayout.JSONLSuffix), id)
	}
	const target = "11111111-2222-4333-8444-555555555555"
	want := writeRefRecord(t, filepath.Join(root, target+sessionlayout.JSONLSuffix), target)

	// The full walk is measured ONCE, to prove the fixture really holds
	// decoys+1 records and to have a duration to LOG beside the resolution
	// cost. It runs BEFORE the snapshots below so its own walk is not counted.
	walkStart := time.Now()
	refs, err := collectSessionFiles(root)
	walkCost := time.Since(walkStart)
	if err != nil {
		t.Fatalf("collectSessionFiles: %v", err)
	}
	if len(refs) != decoys+1 {
		t.Fatalf("fixture built %d records, want %d", len(refs), decoys+1)
	}

	// THE ASSERTION. Snapshot the counter, resolve, snapshot again: any tree
	// enumeration the resolution performs lands between them.
	walksBefore, recordsBefore := sessionWalkProbe.walks.Load(), sessionWalkProbe.records.Load()
	findStart := time.Now()
	got, err := FindSession(root, target)
	findCost := time.Since(findStart)
	walksAfter, recordsAfter := sessionWalkProbe.walks.Load(), sessionWalkProbe.records.Load()
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if got != want {
		t.Fatalf("FindSession = %q, want %q", got, want)
	}

	walks := walksAfter - walksBefore
	recs := recordsAfter - recordsBefore
	// Log only: the ratio is a property of the HOST (see the header), so it
	// never decides this test. It is printed because a human reading a red
	// needs the two numbers side by side.
	t.Logf("exact-id resolution: %s; full walk of %d records: %s (ratio %.1fx — LOGGED, NOT ASSERTED); tree walks during the resolution: %d, record heads read: %d",
		findCost, decoys, walkCost, ratioFloat(walkCost, findCost), walks, recs)

	if walks != 0 || recs != 0 {
		t.Errorf("exact-id resolution enumerated the sessions tree (%d full walk(s), %d record head(s) read over a %d-record fixture) — the name-based fast path is gone (or was not taken); the resolution must not read every record to answer a question the operand's own name already answers", walks, recs, decoys)
	}
}

// ratioFloat is the walk:resolve ratio for the log line above, guarded against
// a zero divisor so a sub-nanosecond resolution logs "+Inf" instead of panicking.
func ratioFloat(walkCost, findCost time.Duration) float64 {
	if findCost <= 0 {
		return math.Inf(1)
	}
	return float64(walkCost) / float64(findCost)
}

// TestSessionWalkProbeCountsAWalk keeps the guard above from passing by
// accident: a counter that never moves is a green that measured nothing (Rule
// 38's vacuum, applied to a test oracle rather than to a census). The fixture
// is deliberately tiny — this asserts the INSTRUMENT, not the cost.
func TestSessionWalkProbeCountsAWalk(t *testing.T) {
	root := t.TempDir()
	const records = 3
	for i := 0; i < records; i++ {
		id := formatDecoyID(i)
		writeRefRecord(t, filepath.Join(root, id+sessionlayout.JSONLSuffix), id)
	}
	walksBefore, recsBefore := sessionWalkProbe.walks.Load(), sessionWalkProbe.records.Load()
	if _, err := collectSessionFiles(root); err != nil {
		t.Fatalf("collectSessionFiles: %v", err)
	}
	walks := sessionWalkProbe.walks.Load() - walksBefore
	recs := sessionWalkProbe.records.Load() - recsBefore
	if walks != 1 || recs != records {
		t.Fatalf("probe read %d walk(s) / %d record(s) for a %d-record fixture, want 1 / %d — the counter the fast-path guard reads is not counting", walks, recs, records, records)
	}
}

// TestFindSession_RedProofArmDisablesTheFastPath is the IN-TREE proof that the
// red-proof switch really does put FindSession back on the pre-fix path. It
// asserts the MECHANISM (the arm makes the resolution walk the tree), not the
// guard's verdict: the verdict is exercised by running the guard itself with
// the switch on —
//
//	$ GOPI_GOPILOG_SESSION_FASTPATH_OFF=1 go test ./internal/gopilog \
//	      -run TestFindSession_ExactIdDoesNotReadEveryRecord -count=1
//
// Both halves live in the tree, so a future reader can tell "the arm is wired"
// from "the guard fails when it should" with two commands and no file edits.
func TestFindSession_RedProofArmDisablesTheFastPath(t *testing.T) {
	root := t.TempDir()
	const decoys = 8
	for i := 0; i < decoys; i++ {
		id := formatDecoyID(i)
		writeRefRecord(t, filepath.Join(root, id+sessionlayout.JSONLSuffix), id)
	}
	const target = "11111111-2222-4333-8444-555555555555"
	want := writeRefRecord(t, filepath.Join(root, target+sessionlayout.JSONLSuffix), target)

	t.Setenv(sessionFastPathRedProofEnv, redProofEnabledValue)

	walksBefore := sessionWalkProbe.walks.Load()
	got, err := FindSession(root, target)
	walks := sessionWalkProbe.walks.Load() - walksBefore
	if err != nil {
		t.Fatalf("FindSession with the red-proof arm on: %v", err)
	}
	// The arm changes the COST, never the ANSWER — a switch that also changed
	// the resolution would be a second behaviour to review, not a red proof.
	if got != want {
		t.Errorf("FindSession with the red-proof arm on = %q, want %q", got, want)
	}
	if walks == 0 {
		t.Errorf("the red-proof arm (%s=1) left the exact-id fast path in place (0 tree walks) — the red proof would be green and prove nothing", sessionFastPathRedProofEnv)
	}
}

// redProofEnabledValue is the truthy spelling the red-proof arm is documented
// with (sessionFastPathDisabled accepts it verbatim).
const redProofEnabledValue = "1"

// decoyIDGroups is the 8-4-4-4-12 uuid group layout the decoy ids follow.
var decoyIDGroups = [5]int{8, 4, 4, 4, 12}

// formatDecoyID builds a distinct, valid-looking session id per decoy index.
func formatDecoyID(i int) string {
	const alpha = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for g, width := range decoyIDGroups {
		if g > 0 {
			out = append(out, '-')
		}
		for c := 0; c < width; c++ {
			out = append(out, alpha[(i>>((3*g+c)%8))&0xf])
		}
	}
	// Fold the index in directly as well, so neighbouring ids never collide.
	copy(out[len(out)-4:], []byte{
		alpha[(i>>12)&0xf], alpha[(i>>8)&0xf], alpha[(i>>4)&0xf], alpha[i&0xf],
	})
	return string(out)
}

// TestParseHTTPRounds_IgnoresCreationHeader is the other half of the raw log's
// creation header (internal/agent.writeRawLogHeader): the logger now opens every
// log with a `#` line, and the round parser must not see it. The parser matches
// marker lines by WHOLE-LINE equality, so the header is not a round — this pins
// that, because a header counted as a round would shift every round number the
// user reads (`gopi session raw <id> 1 request` would hand back nothing).
func TestParseHTTPRounds_IgnoresCreationHeader(t *testing.T) {
	log := filepath.Join(t.TempDir(), "h.raw.log")
	body := "# gopi session raw HTTP log — created 2026-09-25T04:14:08Z pid=1 session=abc log=h.raw.log\n" +
		markerRequestPrefix + "\n" +
		`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}` + "\n" +
		"← RESPONSE\n" +
		`{"type":"message"}` + "\n"
	if err := os.WriteFile(log, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rounds, err := ParseHTTPRounds(log)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 — the creation header must not count as a round", len(rounds))
	}
	if rounds[0].Number != 1 {
		t.Errorf("first round number = %d, want 1 (the header must not shift the numbering)", rounds[0].Number)
	}
	// Round 1's request must still be readable through the shared grammar.
	if body, ok := RawRoundRequest(&rounds[0]); !ok || !strings.Contains(body, `"content":"hi"`) {
		t.Errorf("round 1 request = %q (ok=%v), want the logged body", body, ok)
	}
}
