package testutil

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// Session is a parsed session JSONL file from a sandboxed GOPI_HOME — the
// structure-assertion library every feature's E2E acceptance builds on (the
// P0-P3 acceptance commands in the design doc all reference session JSONL
// event sequences).
//
// Wire format (written by internal/agent/session.go SessionLogger):
//
//	session_start {type, sessionId, timestamp, model, workdir}
//	user          {type, message:{role,content}, promptId}
//	assistant     {type, message:{role,content:[{type:text|tool_use|thinking,...}]}}
//	tool_result   {type, message:{role,content:[{tool_use_id,type,content}]}}
//	request       {type, iteration, prompt_tokens, cache_hit, cache_miss, completion_tokens}
//	session_end   {type, exit_code, total_iterations, total_tokens, elapsed_sec}
type Session struct {
	Path   string
	ID     string
	Events []Event
}

// Event is one parsed JSONL line.
type Event struct {
	Type string
	Raw  map[string]interface{}
}

// Content returns the event's message content as a string ("" when the event
// carries none — a tool_result block list, or any event without a string
// message.content).
func (e Event) Content() string {
	m, ok := e.Raw["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	c, _ := m["content"].(string)
	return c
}

// IsHarnessUser reports whether this event is a user row the HARNESS injected.
//
// The predicate is not re-implemented here: the marker belongs to the harness
// and a READER of a session log needs it — that is the whole reason
// sessionevent.IsHarnessInjected exists (see that file's comment, and
// gopilog's round view). A second spelling of "[system] " here would be the
// mirror Rules 1b/13/14 forbid, and it would drift the moment the prefix does.
func (e Event) IsHarnessUser() bool {
	return e.Type == eventTypeUser && sessionevent.IsHarnessInjected(e.Content())
}

// ToolUse is one tool_use block found in an assistant event.
type ToolUse struct {
	ID    string
	Name  string
	Input map[string]interface{}
}

// jsonlSuffix is the extension of every JSONL artifact a session tree holds —
// the value of internal/sessionlayout.JSONLSuffix, the shared layout-keyed
// classification production readers use (Rule 14: one value, one home; this
// alias keeps the package's call sites reading naturally).
const jsonlSuffix = sessionlayout.JSONLSuffix

// eventTypeUser is the session record's USER row type ("user") — the row a
// human turn and a harness injection share. Named (Rule 1) because three
// places in this fixture compare against it and a fourth would be the fourth
// place to drift. The value is NOT taken from internal/sessionevent: that
// package names the events the LOGGER writes (session_start, session_end,
// task_row_closed, …) and deliberately does not name the transcript row types
// the reader classifies — internal/gopilog spells its own, for the same
// reader-side reason (reader.go's eventTypeUser).
const eventTypeUser = "user"

// sessionRecordName is the session RECORD's file name in the bare-session
// layout (session-dir-unify S1): a session dir holds exactly one, and the
// bare session UUID lives inside the file (see AssertSingleSession), not in
// its name. The value comes from internal/sessionlayout.RecordName, so the
// test-side accounting and the production readers can never disagree about
// which file is the record.
const sessionRecordName = sessionlayout.RecordName

// sessionSideFileNames are the SIDE FILES a session DIRECTORY legitimately
// holds beside its session record — artifacts of the SAME session, never a
// second session:
//
//	signal-provenance.jsonl  signalrec.FileName — one JSON record per caught
//	                         termination signal for THIS session's process
//	                         (written by signalrec.Install, panic-safe).
//
// The list (and the "unknown .jsonl is reported, never silently ignored"
// rule) lives in internal/sessionlayout — the SHARED classification the
// production readers use, so a test and the reader it tests cannot drift
// apart. This package only logs the side files a session carries.
func loadSessionSideFiles(dir string) []string { return sessionlayout.SideFilesIn(dir) }

// sessionRecordRef is one candidate session RECORD on disk: the file plus the
// session directory that contains it (the flat legacy layout is its own dir).
type sessionRecordRef struct {
	Path    string
	Dir     string
	SideCar []string // side files found in the SAME dir, for failure messages
}

// LoadSession finds the session RECORD under gopiHome/.gopi/sessions and
// parses it. Exactly one SESSION is expected (a single-session run writes
// exactly one session record; two records mean the run leaked a session —
// itself an assertion failure). Session-dir-unify S1: the record may live in
// any of the three layouts, so the scan descends one level:
//
//	legacy flat       <sessions>/<uuid>.jsonl
//	task run child    <sessions>/<task_id>/<run-uuid>.jsonl
//	bare prompt/web   <sessions>/<uuid>/session.jsonl
//
// The unit counted is the SESSION, not the *.jsonl file (regression:
// TestWebToolsSignalShutdownWritesSessionEnd on master was red because the
// count said "2 session files" for ONE session — <uuid>/session.jsonl beside
// <uuid>/signal-provenance.jsonl, the latter a side file of the same session
// written by the signal recorder; the fix is this accounting, NOT deleting or
// renaming the provenance file, which the SIGTERM path legitimately writes).
//
// Layout keying decides the grouping, so the count cannot drift with the
// names the run happens to use:
//   - one level down = the session DIR form: at most ONE record (bare
//     session.jsonl or a single task run child — several would be a leak;
//     more than one because a task dir can legitimately hold several RUN
//     files), and everything else *.jsonl there is a side file (known →
//     ignored, unknown → reported).
//   - directly under the sessions root = the legacy flat form: every
//     <uuid>.jsonl IS a record, because the flat layout has no session dir
//     for a side file to live in — so a bare *.jsonl there (a provenance
//     file with no record) is reported as a stray, never silently ignored.
func LoadSession(t testing.TB, gopiHome string) *Session {
	t.Helper()
	dir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName)
	refs, err := findSessionRecords(dir)
	if err != nil {
		t.Fatalf("session: cannot list %s: %v (is GOPI_HOME sandboxed?)", dir, err)
	}
	if len(refs) == 0 {
		t.Fatalf("session: no session record under %s", dir)
	}
	if len(refs) > 1 {
		t.Fatalf("session: %d sessions under %s — a single-session run must create exactly one (a session dir's signal-provenance.jsonl and other side files are NOT sessions): %v",
			len(refs), dir, sessionRecordList(refs))
	}
	if len(refs[0].SideCar) > 0 {
		t.Logf("session: %s carries side file(s) %v — same session, not a second one",
			filepath.Base(filepath.Dir(refs[0].Path)), refs[0].SideCar)
	}
	return parseSession(t, refs[0].Path)
}

// findSessionRecords delegates to the SHARED layout-keyed classification
// (internal/sessionlayout.Records — the same call the production readers
// internal/gopilog and internal/agent make), so a test's idea of "one
// session" and the reader's can never drift apart. See LoadSession for the
// layout keying; the legacy flat/stray-file reporting detail is that package's
// documented rule.
func findSessionRecords(root string) ([]sessionRecordRef, error) {
	records, err := sessionlayout.Records(root)
	if err != nil {
		return nil, err
	}
	refs := make([]sessionRecordRef, 0, len(records))
	for _, r := range records {
		refs = append(refs, sessionRecordRef{Path: r.Path, Dir: r.Dir, SideCar: r.SideFiles})
	}
	return refs, nil
}

// sessionRecordList renders each found session for a failure message, naming
// the side files per session so a leak is readable at a glance.
func sessionRecordList(refs []sessionRecordRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Path
		if len(r.SideCar) > 0 {
			s += " (+ side files " + strings.Join(r.SideCar, ", ") + ")"
		}
		out = append(out, s)
	}
	return out
}

// SessionRecordPath returns the session record file under sessionsRoot, or ""
// when the tree holds none yet. It is the ARTIFACT-based readiness probe for
// tests that must wait for a spawned run's session to exist before asserting
// on it — deliberately the same accounting LoadSession uses (a session dir's
// side files are never records), so a test's readiness probe and its final
// assertion cannot disagree about what a session is. Unlike LoadSession it
// does not fail the test: "no record yet" is the normal state while the child
// is still starting.
func SessionRecordPath(sessionsRoot string) string {
	return sessionlayout.RecordPath(sessionsRoot)
}

// parseSession reads one session JSONL and returns its parsed events.
func parseSession(t testing.TB, path string) *Session {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("session: open %s: %v", path, err)
	}
	defer f.Close()

	s := &Session{Path: path}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("session: invalid JSON line in %s: %v — %q", path, err, line)
		}
		typ, _ := ev["type"].(string)
		s.Events = append(s.Events, Event{Type: typ, Raw: ev})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("session: scan %s: %v", path, err)
	}
	if len(s.Events) == 0 {
		t.Fatalf("session: %s is empty", path)
	}
	return s
}

// Types returns the event types in file order.
func (s *Session) Types() []string {
	out := make([]string, len(s.Events))
	for i, ev := range s.Events {
		out[i] = ev.Type
	}
	return out
}

// Counts returns the number of events of each type.
func (s *Session) Counts() map[string]int {
	out := map[string]int{}
	for _, ev := range s.Events {
		out[ev.Type]++
	}
	return out
}

// First returns the first event of the given type, or nil.
func (s *Session) First(typ string) *Event {
	for i := range s.Events {
		if s.Events[i].Type == typ {
			return &s.Events[i]
		}
	}
	return nil
}

// AssertSingleSession asserts the file carries exactly one session_start with
// a non-empty sessionId. The file-name ↔ session-id match is layout-aware
// (session-dir-unify S1): a flat or task-run file is named <session-id>.jsonl
// while a bare session file is literally session.jsonl — only the flat/task
// forms are checked against the id.
func (s *Session) AssertSingleSession(t *testing.T) {
	t.Helper()
	start := s.First(sessionevent.EventTypeSessionStart)
	if start == nil {
		t.Fatalf("session: no session_start event in %s", s.Path)
	}
	id, _ := start.Raw["sessionId"].(string)
	if id == "" {
		t.Fatalf("session: session_start carries no sessionId: %v", start.Raw)
	}
	s.ID = id
	switch base := filepath.Base(s.Path); base {
	case sessionRecordName:
		// bare layout — file name is fixed, id lives in the event only.
	default:
		if got := base; got != id+jsonlSuffix {
			t.Errorf("session: file %q does not match sessionId %q (want %s.jsonl)", got, id, id)
		}
	}
	if n := s.Counts()[sessionevent.EventTypeSessionStart]; n != 1 {
		t.Errorf("session: %d session_start events, want exactly 1 (one session per file)", n)
	}
}

// AssertEventOrder asserts the given event types appear in the file IN THIS
// RELATIVE ORDER (subsequence match): other event types (e.g. request rows,
// extra confirmation rounds) may appear anywhere between them. This is the
// canonical task-run sequence check:
//
//	AssertEventOrder(t, sessionevent.EventTypeSessionStart, "user", "assistant",
//		"tool_result", "assistant", sessionevent.EventTypeSessionEnd)
func (s *Session) AssertEventOrder(t *testing.T, want ...string) {
	t.Helper()
	types := s.Types()
	i := 0
	for _, typ := range types {
		if i < len(want) && typ == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Errorf("session: event order %v does not contain %v as a subsequence", types, want)
	}
}

// AssertCounts asserts exact per-type event counts. Types not mentioned in
// want are unconstrained (they may legitimately vary with loop rounds).
//
// THE "user" KEY COUNTS THE OPERATOR'S TURNS, not raw user rows — the same
// meaning Session.UserContents gives them, and for the same measured reason
// (see that method: the harness stamp is written on every request, so a raw
// census reports two rows per turn). Keeping the two definitions apart would
// make a test read "2 turns" from one call and "4" from the other over the
// same record. A test that needs the RAW user-row census asserts on
// AllUserContents (or the harness half via HarnessUserContents) instead of
// reaching for a count this method deliberately redefines.
func (s *Session) AssertCounts(t *testing.T, want map[string]int) {
	t.Helper()
	got := s.Counts()
	for typ, n := range want {
		if typ == eventTypeUser {
			if op := len(s.UserContents()); op != n {
				t.Errorf("session: operator user turns = %d, want %d (harness rows = %d, all user rows = %d; types: %v)",
					op, n, len(s.HarnessUserContents()), got[typ], s.Types())
			}
			continue
		}
		if got[typ] != n {
			t.Errorf("session: %s events = %d, want %d (types: %v)", typ, got[typ], n, s.Types())
		}
	}
}

// ToolUseResultPairs asserts the tool_use ↔ tool_result pairing contract:
// every tool_use block id has exactly one matching tool_result with the same
// tool_use_id appearing AFTER it, and no tool_result references an unknown id.
func (s *Session) ToolUseResultPairs(t *testing.T) {
	t.Helper()
	uses := map[string]bool{}
	seen := map[string]bool{}
	for _, ev := range s.Events {
		switch ev.Type {
		case "assistant":
			for _, b := range contentBlocks(ev) {
				if b["type"] == "tool_use" {
					id, _ := b["id"].(string)
					if id == "" {
						t.Errorf("session: tool_use block without id: %v", b)
						continue
					}
					uses[id] = true
				}
			}
		case "tool_result":
			for _, b := range contentBlocks(ev) {
				id, _ := b["tool_use_id"].(string)
				if id == "" {
					t.Errorf("session: tool_result block without tool_use_id: %v", b)
					continue
				}
				if !uses[id] {
					t.Errorf("session: tool_result references unknown tool_use id %q (no matching tool_use block)", id)
					continue
				}
				if seen[id] {
					t.Errorf("session: duplicate tool_result for tool_use id %q", id)
				}
				seen[id] = true
			}
		}
	}
	for id := range uses {
		if !seen[id] {
			t.Errorf("session: tool_use id %q has no matching tool_result", id)
		}
	}
}

// ToolUses returns every tool_use block found in assistant events, in file
// order.
func (s *Session) ToolUses() []ToolUse {
	var out []ToolUse
	for _, ev := range s.Events {
		if ev.Type != "assistant" {
			continue
		}
		for _, b := range contentBlocks(ev) {
			if b["type"] != "tool_use" {
				continue
			}
			tu := ToolUse{}
			tu.ID, _ = b["id"].(string)
			tu.Name, _ = b["name"].(string)
			if in, ok := b["input"].(map[string]interface{}); ok {
				tu.Input = in
			}
			out = append(out, tu)
		}
	}
	return out
}

// UserContents returns the text of the OPERATOR's user turns, in order — every
// user row that is NOT a harness injection.
//
// WHY THE HARNESS ROWS ARE EXCLUDED, and why the filter lives here rather than
// at each call site. A session record's "user" rows carry TWO different things:
// what the person typed, and what the framework appended for the model (the
// turn-stamp clock line, the task-start and commit checkpoints, job and
// truncation notices — 18 append sites in internal/agent, every one of them
// opening with the harness prefix). A test asking "what turns did the operator
// take?" means the first kind, and every call site of this method is asking
// exactly that.
//
// It used to return both, which was survivable only while the two never met.
// The clock stamp is written on EVERY request (internal/agent/clock.go's
// turnStampLine), so ANY chat test that asserts user turns sees it. Measured
// 2026-10-12 on a piped two-turn `gopi chat`: the record held four user rows
// for two turns — "hello there", the stamp, "what is two plus two", the stamp
// — and both TestChatCookedFallback_NonTTY (cmd/agent) and
// TestChat_HappyPath_TwoTurnsSameSession (testsuite) were red for it.
//
// Excluding them is NOT a licence to stop looking at them: HarnessUserContents
// returns only the injections (so a test can assert one really was recorded),
// and AllUserContents returns every user row, which is what a test must use
// when it is auditing the record rather than counting turns.
//
// A CALLER THAT WANTS BOTH HALVES MUST NOT FILTER THIS METHOD'S RESULT AGAIN.
// The method has already applied sessionevent.IsHarnessInjected, so a loop that
// re-tests each returned element with the SAME predicate partitions nothing:
// the injected half comes back permanently empty while the operator half reads
// correctly, and a "one stamp per request, zero is a defect" assertion built on
// it then fails on a record that is exactly right. That shape shipped in
// cmd/agent/cli_lineedit_test.go and was measured 2026-10-12: the record held
// the two expected stamps while the fixture reported `harness turn stamps = 0`.
// The two-halves view is HarnessUserContents / AllUserContents (or
// IsHarnessUser per EVENT, which AllUserContents exists to allow) — never this
// method filtered a second time.
func (s *Session) UserContents() []string {
	var out []string
	for _, ev := range s.Events {
		if ev.Type != eventTypeUser || ev.IsHarnessUser() {
			continue
		}
		if c := ev.Content(); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// HarnessUserContents returns the text of the user rows the harness injected,
// in order — the exact complement of UserContents (sessionevent.IsHarnessInjected
// decides, so the two can never drift into overlapping or double-counted sets).
//
// It exists so that filtering the injections out of UserContents does not put
// them beyond a test's reach: a test that asserts the framework really told the
// model something (the task-start checkpoint, a job notice, the budget tag on a
// request) asserts it HERE, which is both narrower and stronger than asserting
// it against the union of every user row.
func (s *Session) HarnessUserContents() []string {
	var out []string
	for _, ev := range s.Events {
		if !ev.IsHarnessUser() {
			continue
		}
		if c := ev.Content(); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// AllUserContents returns the text of EVERY user row, harness injections
// included — the unfiltered view, for a test auditing the record as a whole
// ("the record shows every request the model got") rather than counting the
// operator's turns.
func (s *Session) AllUserContents() []string {
	var out []string
	for _, ev := range s.Events {
		if ev.Type != eventTypeUser {
			continue
		}
		if c := ev.Content(); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// SessionEndExitCode returns the session_end exit_code, or -1 when no
// session_end exists.
func (s *Session) SessionEndExitCode() int {
	end := s.First(sessionevent.EventTypeSessionEnd)
	if end == nil {
		return -1
	}
	code, _ := end.Raw["exit_code"].(float64)
	return int(code)
}

// contentBlocks extracts message.content[].* blocks from a session event
// (assistant text/tool_use/thinking and user tool_result blocks share the
// same message.content array shape).
func contentBlocks(ev Event) []map[string]interface{} {
	m, ok := ev.Raw["message"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := m["content"].([]interface{})
	if !ok {
		return nil
	}
	var out []map[string]interface{}
	for _, item := range raw {
		if b, ok := item.(map[string]interface{}); ok {
			out = append(out, b)
		}
	}
	return out
}

// Summary renders a compact one-line picture of the session for failure
// messages and the smoke report.
func (s *Session) Summary() string {
	return fmt.Sprintf("%s (%d events: %v)", filepath.Base(s.Path), len(s.Events), s.Types())
}
