package tools

// session_search_test.go — the session scope's own proof.
//
// Two of these are REGRESSION GUARDS for defects this work found by running the
// real thing rather than trusting the reasoning:
//
//   - TestSessionSearchSurvivesALongRow — a >1 MiB session row made the reused
//     search wrapper fail the whole call (`bufio.Scanner: token too long`).
//   - TestSessionRowAttributesResolvesAgainstTheReader — the round was derived
//     from `iteration` (which a RESUMED session restarts from 0) and then from
//     the PREVIOUS request row (off by one); `gopi session log <id> <round>` was
//     used as the oracle both times.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// writeSessionFixture builds a session JSONL with the REAL row cadence, taken
// from a measured session: content rows first, then the `request` row that
// flushes them into a round. The needle sits in the round's USER row, so a hit's
// round is exactly the ordinal of the request row that follows it.
//
// bigRowBytes > 0 appends a tool_result row of that size before a final request
// row, so the big row is attributable instead of falling outside every round.
func writeSessionFixture(t *testing.T, dir, sessionID string, rounds int, needle string, bigRowBytes int) string {
	t.Helper()
	path := filepath.Join(dir, sessionID, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var b strings.Builder
	emit := func(v interface{}) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}

	emit(map[string]interface{}{"type": sessionevent.EventTypeSessionStart, "timestamp": "2026-11-07T03:22:00.000000001+08:00"})
	for i := 0; i < rounds; i++ {
		round := sessionevent.RoundNumberOf(i)
		// The content rows of the round...
		emit(map[string]interface{}{
			"type": "user", "timestamp": fmt.Sprintf("2026-11-07T03:22:%02d.000000001+08:00", i%60),
			"message": map[string]interface{}{"content": fmt.Sprintf("%s-r%d", needle, round)},
		})
		emit(map[string]interface{}{
			"type": "assistant", "timestamp": "2026-11-07T03:22:05.000000001+08:00",
			"message": map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "text", "text": fmt.Sprintf("answer %d", round)},
			}},
		})
		// ...and the request row that flushes them into THIS round.
		emit(map[string]interface{}{
			"type": "request", "iteration": i, "timestamp": "2026-11-07T03:22:06.000000001+08:00",
		})
	}
	if bigRowBytes > 0 {
		emit(map[string]interface{}{
			"type": "tool_result", "timestamp": "2026-11-07T03:23:00.000000001+08:00",
			"message": map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call_big",
					"content": fmt.Sprintf("%s-big %s", needle, strings.Repeat("x", bigRowBytes))},
			}},
		})
		// A final request flushes the big row into its own round.
		emit(map[string]interface{}{
			"type": "request", "iteration": rounds, "timestamp": "2026-11-07T03:23:01.000000001+08:00",
		})
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestSessionSearchRendersStructuredHitsNotRawJSON is the core contract: a hit is
// round + row type + timestamp + human-readable snippet + locator, and NOT the
// JSON line the match lives on.
func TestSessionSearchRendersStructuredHitsNotRawJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeSessionFixture(t, dir, "sess-structured", 3, "needle", 0)

	res, err := SessionSearch(SessionSearchOptions{Query: "needle", SessionID: path, Limit: 10})
	if err != nil {
		t.Fatalf("SessionSearch: %v", err)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("expected 3 hits (one per round), got %d", len(res.Hits))
	}

	for i, h := range res.Hits {
		wantRound := sessionevent.RoundNumberOf(i)
		if h.Round != wantRound {
			t.Errorf("hit %d: round=%d want %d", i, h.Round, wantRound)
		}
		if h.RowType != sessionRowTypeUser {
			t.Errorf("hit %d: rowType=%q want %q", i, h.RowType, sessionRowTypeUser)
		}
		if h.Timestamp == "—" || h.Timestamp == "" {
			t.Errorf("hit %d: timestamp not rendered: %q", i, h.Timestamp)
		}
		if !strings.Contains(h.Locate, fmt.Sprintf("gopi session log sess-structured %d", wantRound)) {
			t.Errorf("hit %d: locator does not name the round: %q", i, h.Locate)
		}
		// The snippet is the row's WORDS, not its JSON envelope.
		if !strings.Contains(h.Snippet, fmt.Sprintf("needle-r%d", wantRound)) {
			t.Errorf("hit %d: snippet does not carry the row's text: %q", i, h.Snippet)
		}
		for _, junk := range []string{`"content"`, `"type"`, `"timestamp"`, `{"`} {
			if strings.Contains(h.Snippet, junk) {
				t.Errorf("hit %d: snippet leaks the JSON envelope (%s): %q", i, junk, h.Snippet)
			}
		}
	}

	page := FormatSessionSearchHits(res)
	if strings.Contains(page, `"type":`) || strings.Contains(page, `"tool_use_id":`) {
		t.Errorf("rendered page leaks raw JSON fields:\n%s", page)
	}
	for _, want := range []string{"round 1", "round 2", "round 3", sessionRowTypeUser, "→ gopi session log"} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered page missing %q:\n%s", want, page)
		}
	}
}

// TestSessionSearchSurvivesALongRow is the REGRESSION GUARD for the wrapper
// defect: a session row of 3.9 MB (measured on this host) made KeywordSearch fail
// wholesale with `bufio.Scanner: token too long`, because the wrapper's output
// parser capped ONE output line at 1 MiB. The fixture row here is 2 MiB — above
// that old cap, below the session reader's own ceiling.
func TestSessionSearchSurvivesALongRow(t *testing.T) {
	dir := t.TempDir()
	path := writeSessionFixture(t, dir, "sess-bigrow", 1, "needle", 2<<20)

	res, err := SessionSearch(SessionSearchOptions{Query: "needle", SessionID: path, Limit: 10})
	if err != nil {
		t.Fatalf("SessionSearch over a long row failed: %v", err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("expected 2 needle rows (round 1 + the big row), got %d", len(res.Hits))
	}

	var big *SessionSearchHit
	for i := range res.Hits {
		if res.Hits[i].ToolUseID == "call_big" {
			big = &res.Hits[i]
		}
	}
	if big == nil {
		t.Fatalf("the 2 MiB row was not returned as a hit: %+v", res.Hits)
	}
	if len([]rune(big.Snippet)) > sessionSearchSnippetTotal+2 {
		t.Errorf("snippet is %d runes, want <= %d", len([]rune(big.Snippet)), sessionSearchSnippetTotal+2)
	}
	// The big row is flushed by the request AFTER it, so it belongs to round 2.
	if want := sessionevent.RoundNumberOf(1); big.Round != want {
		t.Errorf("big row round=%d want %d", big.Round, want)
	}
}

// TestSessionSearchLimitIsReportedNotSilent pins the page cap: limit lowers the
// page AND the drop is counted, so a reader can tell "two matches" from "two
// shown of six".
func TestSessionSearchLimitIsReportedNotSilent(t *testing.T) {
	dir := t.TempDir()
	path := writeSessionFixture(t, dir, "sess-limit", 6, "needle", 0)

	res, err := SessionSearch(SessionSearchOptions{Query: "needle", SessionID: path, Limit: 2})
	if err != nil {
		t.Fatalf("SessionSearch: %v", err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("limit=2 returned %d hits", len(res.Hits))
	}
	if res.Dropped < 1 {
		t.Errorf("limit=2 over 6 matches reported dropped=%d, want >= 1", res.Dropped)
	}
	page := FormatSessionSearchHits(res)
	if !strings.Contains(page, "more match(es) not shown") {
		t.Errorf("render does not state the dropped matches:\n%s", page)
	}
}

// TestSessionSearchRawLogIsOffByDefault pins the decision the header argues for:
// one file is searched unless the caller explicitly asks for the second, and an
// explicit ask on a session that recorded no raw log is REFUSED, not silently
// answered with one file.
func TestSessionSearchRawLogIsOffByDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeSessionFixture(t, dir, "sess-raw", 2, "needle", 0)

	res, err := SessionSearch(SessionSearchOptions{Query: "needle", SessionID: path, Limit: 10})
	if err != nil {
		t.Fatalf("SessionSearch: %v", err)
	}
	if res.RawLogSearched {
		t.Fatal("raw.log was searched without being asked for")
	}
	if len(res.Files) != 1 || res.Files[0] != path {
		t.Fatalf("files searched = %v, want just %s", res.Files, path)
	}
	for _, h := range res.Hits {
		if h.Source != sessionSearchSourceJSONL {
			t.Errorf("hit source = %q, want %q", h.Source, sessionSearchSourceJSONL)
		}
	}

	if _, err := SessionSearch(SessionSearchOptions{Query: "needle", SessionID: path, IncludeRawLog: true}); err == nil {
		t.Fatal("include_raw_log over a session with no recorded raw log returned nil error (a silent single-file search)")
	}
}

// TestSessionSearchEmptyTargetIsRefused: scope=session with no session named must
// say so, not search an arbitrary session (Rule 15).
func TestSessionSearchEmptyTargetIsRefused(t *testing.T) {
	_, err := SessionSearch(SessionSearchOptions{Query: "x"})
	if err == nil {
		t.Fatal("empty session target returned nil error")
	}
	if !strings.Contains(err.Error(), SessionSearchScopeSession) {
		t.Errorf("refusal does not name the scope: %v", err)
	}
}

// TestSessionSearchRunRecordLayoutResolvesFromABareID is the minimal repro for
// the layout defect measured on 2026-11-09 (task 01a1bcd6-96d3-7aaf-bb10-a903fb940c4a):
// a TASK RUN's record is `<root>/<id>/<id>.jsonl` — dir named after the id, file
// named after the id too — and a bare id was refused with "no session record"
// while the file sat right there.
//
// The fixture reproduces the SHAPE that made it fail, not just the happy path:
// the directory ALSO holds the side files a real run dir carries (output.log,
// metadata.json). A bare dir holding only a jsonl is a different layout, so a
// fixture without them would pass on the OLD code and prove nothing.
func TestSessionSearchRunRecordLayoutResolvesFromABareID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	const id = "01a1bcd6-96d3-7aaf-bb10-a903fb940c4a"
	const needle = "RUNLAYOUT_NEEDLE_ZZZ"

	dir := filepath.Join(home, ".gopi", "sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	record := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(record, []byte(`{"type":"user","timestamp":"2026-11-09T04:27:46+08:00","message":{"content":"`+needle+`"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	for _, side := range []string{"output.log", "metadata.json", "task-report.md"} {
		if err := os.WriteFile(filepath.Join(dir, side), []byte("x"), 0o644); err != nil {
			t.Fatalf("write side file %s: %v", side, err)
		}
	}

	gotID, gotPath, err := resolveSessionSearchTarget(id)
	if err != nil {
		t.Fatalf("a bare id naming the run-record layout was refused: %v", err)
	}
	if gotID != id || gotPath != record {
		t.Fatalf("resolved (%q, %q), want (%q, %q)", gotID, gotPath, id, record)
	}

	res, err := SessionSearch(SessionSearchOptions{Query: needle, SessionID: id, Limit: 3})
	if err != nil {
		t.Fatalf("SessionSearch through the bare id: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("got %d hit(s) from the run-record layout, want 1 (files searched: %v)", len(res.Hits), res.Files)
	}
	if res.Hits[0].LineNo != 1 || res.Hits[0].Path != record {
		t.Errorf("hit = line %d of %q, want line 1 of %q", res.Hits[0].LineNo, res.Hits[0].Path, record)
	}
}

// TestSessionSearchMissingSessionIsRefusedByName: an id that resolves to nothing
// names what was looked for, so a reader can tell "wrong id" from "no matches".
func TestSessionSearchMissingSessionIsRefusedByName(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	_, err := SessionSearch(SessionSearchOptions{Query: "x", SessionID: "no-such-session-id"})
	if err == nil {
		t.Fatal("a nonexistent session id returned nil error")
	}
	if !strings.Contains(err.Error(), "no session record for") {
		t.Errorf("refusal does not name the lookup: %v", err)
	}
}

// TestSessionRowAttributesResolvesAgainstTheReader pins BOTH directions of the
// round boundary, because getting it wrong is silent and was wrong twice:
//
//	the PREVIOUS-request reading  → every hit opened the round BEFORE its own;
//	the iteration-derived reading  → every hit of a resumed session opened a
//	                                 round from a counter that had restarted.
//
// The rule is the reader's own: a `request` row FLUSHES the content accumulated
// before it, so a content row belongs to the round of the request row that comes
// AFTER it, and the request row itself belongs to that same round. A row the log
// never flushes belongs to none.
func TestSessionRowAttributesResolvesAgainstTheReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	rows := []string{
		`{"type":"session_start","timestamp":"2026-11-07T03:00:00Z"}`,
		`{"type":"user","timestamp":"2026-11-07T03:00:01Z","message":{"content":"early"}}`,                           // line 2
		`{"type":"request","iteration":0,"timestamp":"2026-11-07T03:00:02Z"}`,                                        // line 3 → round 1
		`{"type":"tool_result","timestamp":"2026-11-07T03:00:03Z","message":{"content":[{"tool_use_id":"call_a"}]}}`, // line 4
		`{"type":"request","iteration":9,"timestamp":"2026-11-07T03:00:04Z"}`,                                        // line 5 → round 2
		`{"type":"tool_result","timestamp":"2026-11-07T03:00:05Z","message":{"content":[{"tool_use_id":"call_b"}]}}`, // line 6
	}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	matches := []TextHit{{LineNo: 2}, {LineNo: 3}, {LineNo: 4}, {LineNo: 5}, {LineNo: 6}}
	attr, err := sessionRowAttributes(path, matches)
	if err != nil {
		t.Fatalf("sessionRowAttributes: %v", err)
	}

	want := map[int]int{
		2: sessionevent.RoundNumberOf(0), // flushed by the request on line 3 → round 1
		3: sessionevent.RoundNumberOf(0), // the flushing request is round 1 itself
		4: sessionevent.RoundNumberOf(1), // flushed by line 5 → round 2 (NOT round 1)
		5: sessionevent.RoundNumberOf(1), // its `iteration` is 9 and must NOT be used
	}
	for line, wantRound := range want {
		if got := attr[line].Round; got != wantRound {
			t.Errorf("line %d: round=%d want %d", line, got, wantRound)
		}
	}
	if got := attr[6].Round; sessionevent.RoundIndexRecorded(got) {
		t.Errorf("a row after the LAST request row got round %d, want no round", got)
	}
}

// TestSessionRowAttributesSurvivesAResume is the REGRESSION GUARD for the
// iteration-derived reading, measured on session e68fddf5: that session resumed
// four times, so its `iteration` field never exceeds 35 while the file carries
// 1381 `request` rows, and `gopi session log <id> <round>` numbers by the ORDINAL
// ("ROUND N of 1381"). An iteration-derived locator opened a round from a counter
// that had restarted — plausible and wrong. The fixture is that shape: two runs,
// each numbering its iterations 0..1.
func TestSessionRowAttributesSurvivesAResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	rows := []string{
		`{"type":"session_start","timestamp":"2026-11-07T03:00:00Z"}`,                                                // 1
		`{"type":"request","iteration":0,"timestamp":"2026-11-07T03:00:01Z"}`,                                        // 2 → round 1
		`{"type":"tool_result","timestamp":"2026-11-07T03:00:02Z","message":{"content":[{"tool_use_id":"call_a"}]}}`, // 3
		`{"type":"request","iteration":1,"timestamp":"2026-11-07T03:00:03Z"}`,                                        // 4 → round 2
		`{"type":"request","iteration":2,"timestamp":"2026-11-07T03:00:04Z"}`,                                        // 5 → round 3
		`{"type":"session_start","timestamp":"2026-11-07T04:00:00Z"}`,                                                // 6 — RESUME
		`{"type":"request","iteration":0,"timestamp":"2026-11-07T04:00:01Z"}`,                                        // 7 → round 4
		`{"type":"tool_result","timestamp":"2026-11-07T04:00:02Z","message":{"content":[{"tool_use_id":"call_b"}]}}`, // 8
		`{"type":"request","iteration":1,"timestamp":"2026-11-07T04:00:03Z"}`,                                        // 9 → round 5
	}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines := []int{2, 3, 4, 5, 7, 8, 9}
	matches := make([]TextHit, 0, len(lines))
	for _, l := range lines {
		matches = append(matches, TextHit{LineNo: l})
	}
	attr, err := sessionRowAttributes(path, matches)
	if err != nil {
		t.Fatalf("sessionRowAttributes: %v", err)
	}

	// The count carries ACROSS the resume: the post-resume requests are 4 and 5,
	// never 1 and 2 again.
	want := map[int]int{
		2: sessionevent.RoundNumberOf(0),
		3: sessionevent.RoundNumberOf(1),
		4: sessionevent.RoundNumberOf(1),
		5: sessionevent.RoundNumberOf(2),
		7: sessionevent.RoundNumberOf(3),
		8: sessionevent.RoundNumberOf(4),
		9: sessionevent.RoundNumberOf(4),
	}
	for line, wantRound := range want {
		if got := attr[line].Round; got != wantRound {
			t.Errorf("line %d: round=%d want %d (an iteration-derived round would repeat 1 after the resume)", line, got, wantRound)
		}
	}

	// And the reported rounds never go BACKWARDS down the file — non-decreasing,
	// since several rows share one round. This is the property whose absence
	// exposed the defect on the real corpus.
	prev := 0
	for _, line := range lines {
		if attr[line].Round < prev {
			t.Fatalf("rounds go backwards down the file at line %d: %d after %d", line, attr[line].Round, prev)
		}
		prev = attr[line].Round
	}
}

// TestSessionSearchResolvesEveryLayoutShape is a GUARD, and its first job is to
// record what was MEASURED rather than what was assumed.
//
// THE CLAIM IT WAS WRITTEN TO PUSH BACK ON (task 01a1bd40-8110's briefing): that
// resolveSessionSearchTarget covered only three layouts and therefore refused a
// bare id for a task run, whose record is `<root>/<id>/<id>.jsonl`. Measured on
// the pristine tree at 7f48b1e before any change: a bare id resolved that shape
// correctly, WITH and WITHOUT the side files a real run dir carries, for the real
// directory this very run lives in
// (/home/user/.gopi/sessions/01a1bd40-…/01a1bd40-….jsonl) and for the one the
// briefing cites as its measurement (01a1bcd6-…). The reason is layout 3:
// sessionlayout.Records() classifies a task-run dir as a record container and
// returns each JSONL in it as a record, so the loop finds BOTH `<id>` (the dir's
// basename) and `<id>` (the file's stem).
//
// So no defect is repaired here — and no cheap stat for `<id>/<id>.jsonl` was
// added, because a second resolution arm is a second place the id derivation can
// drift (Rule 14). What this test does is pin the SHAPES, so the next reader can
// see which of them a bare id reaches and cannot "fix" a non-defect by adding an
// arm, nor break one of them without a red.
//
// The last row (H) is the ONE shape a bare id does NOT reach, and it is pinned on
// purpose: a run directory holding only side files holds no record, so the
// refusal is CORRECT — and its text must name the layouts that were actually
// tried (Rule 15), which is the real defect this run fixed: the old text said
// "looked for session.jsonl/", naming a directory that does not exist and
// omitting both the correct layout-1 spelling and the task-run shape.
func TestSessionSearchResolvesEveryLayoutShape(t *testing.T) {
	const id = "01a1bd40-8110-75e6-8858-e64e21db699b"
	writeJSONL := func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeSide := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
				t.Fatalf("write side file %s: %v", n, err)
			}
		}
	}

	for _, tc := range []struct {
		name string
		// build materialises the shape inside the sessions root.
		build func(t *testing.T, root string)
		// want is the record a bare id must resolve to, relative to root; ""
		// means the shape holds no record and the id must be REFUSED.
		want string
	}{
		{
			name: "bare dir holding session.jsonl",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				writeJSONL(t, filepath.Join(d, sessionlayout.RecordName))
			},
			want: filepath.Join(id, sessionlayout.RecordName),
		},
		{
			name: "flat <id>.jsonl at the sessions root",
			build: func(t *testing.T, root string) {
				writeJSONL(t, filepath.Join(root, id+sessionlayout.JSONLSuffix))
			},
			want: id + sessionlayout.JSONLSuffix,
		},
		{
			name: "task-run dir holding <id>/<id>.jsonl, no side files",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				writeJSONL(t, filepath.Join(d, id+sessionlayout.JSONLSuffix))
			},
			want: filepath.Join(id, id+sessionlayout.JSONLSuffix),
		},
		{
			// THE SHAPE THE BRIEFING CALLED UNREACHABLE, with the side files a
			// real run dir carries — without them the dir would be the BARE
			// shape and the case would prove nothing.
			name: "task-run dir holding <id>/<id>.jsonl beside its side files",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				writeJSONL(t, filepath.Join(d, id+sessionlayout.JSONLSuffix))
				writeSide(t, d, "output.log", "metadata.json", "task-report.md", "run.lock")
			},
			want: filepath.Join(id, id+sessionlayout.JSONLSuffix),
		},
		{
			name: "task-run dir holding an older per-run <uuid>.jsonl",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				writeJSONL(t, filepath.Join(d, "6f9a1c22-1111-2222-3333-444455556666"+sessionlayout.JSONLSuffix))
				writeSide(t, d, "output.log", "metadata.json")
			},
			want: filepath.Join(id, "6f9a1c22-1111-2222-3333-444455556666"+sessionlayout.JSONLSuffix),
		},
		{
			// attachments/ is a DIRECTORY inside the run dir; the walk must not
			// descend into it or claim a file there as the record.
			name: "task-run dir holding <id>/<id>.jsonl and an attachments/ subtree",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				att := filepath.Join(d, "attachments")
				if err := os.MkdirAll(att, 0o755); err != nil {
					t.Fatal(err)
				}
				writeJSONL(t, filepath.Join(d, id+sessionlayout.JSONLSuffix))
				writeSide(t, d, "output.log", "metadata.json")
				writeSide(t, att, "log.txt")
			},
			want: filepath.Join(id, id+sessionlayout.JSONLSuffix),
		},
		{
			// Both layouts present: layout 1 is tried first, so the BARE
			// record is the answer. Pinned because an arm added ahead of it
			// would silently change which file a reader searches.
			name: "bare session.jsonl AND a run record in the same dir",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				writeJSONL(t, filepath.Join(d, sessionlayout.RecordName))
				writeJSONL(t, filepath.Join(d, id+sessionlayout.JSONLSuffix))
			},
			want: filepath.Join(id, sessionlayout.RecordName),
		},
		{
			// The only shape a bare id must NOT reach: no record at all. The
			// refusal is correct here, and the case exists so a future "fix"
			// cannot make the resolver invent a path for a dir that holds none.
			name: "task-run dir holding ONLY side files",
			build: func(t *testing.T, root string) {
				d := filepath.Join(root, id)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				writeSide(t, d, "output.log", "metadata.json")
			},
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("GOPI_HOME", home)
			root := filepath.Join(home, ".gopi", "sessions")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			tc.build(t, root)

			gotID, gotPath, err := resolveSessionSearchTarget(id)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("a dir holding no record resolved to %q — the resolver invented a record", gotPath)
				}
				// Rule 15: the refusal must name the layouts it tried, spelled
				// as PATHS. The old text's "%s/" produced "session.jsonl/",
				// a directory that exists nowhere.
				for _, want := range []string{
					filepath.Join(id, sessionlayout.RecordName),
					id + sessionlayout.JSONLSuffix,
					filepath.Join(id, id+sessionlayout.JSONLSuffix),
				} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not name %q — a list that omits the layout the reader is looking at is a lie with a helpful face:\n%v", want, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("a bare id naming this layout was refused: %v", err)
			}
			if gotID != id {
				t.Errorf("id = %q, want %q", gotID, id)
			}
			if want := filepath.Join(root, tc.want); gotPath != want {
				t.Errorf("path = %q, want %q", gotPath, want)
			}
		})
	}
}
