package agent

// cov_agent_cr_test.go — function-level coverage closure for internal/agent
// (coverage slice "cov-agent-cr"; tests only, no product-code change).
//
// The 14 exported functions this file closes, per
// `python3 scripts/func_coverage.py --uncovered --pkg internal/agent`:
//
//	AutoTaskRegisterOffValue   taskregister.go:124
//	ChatRegisterLatch          chatregister.go:117
//	CloseChatRow               chatregister.go:98
//	CountStatus                tasklist.go:315
//	(DetectStaleTasks — DELETED 2026-11-05 with the stale sweep; its two arms
//	 were removed and the property they guarded lives in
//	 internal/tools/tasknotify_test.go)
//	HasSessionEnd              session.go:595
//	Load                       crashdump.go:156/166 (atomicValueFloat/Int)
//	LogHippocampusBrief        session.go:742
//	ParseTaskListBalancedPrefix tasklist.go:186
//	RecordChatTurn             chatregister.go:82
//	RegisterChatSession        chatregister.go:54
//	SessionEndBlock            session.go:497
//	SessionLogFileName         session.go:76
//	UnmarshalJSON              tasklist.go:51 (FlexibleString)
//
// Every function above is CALLED here with real assertions (call-site counts
// are recorded in devdoc/cov-agent-cr.md) — the metric is name-appearance, so
// a mention-only entry would be dishonest coverage. All helpers carry the
// covCr prefix: sibling coverage slices touch this same package and an
// unprefixed helper name is a duplicate-symbol merge conflict.
//
// Isolation: every filesystem effect lands under t.TempDir() (via
// config.GopiDirEnv), every env var is set with t.Setenv (auto-restored), the
// ledger path is pinned per test, and nothing here needs the network or a real
// sleep.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// covCr helpers (task-unique prefix).
// ---------------------------------------------------------------------------

// covCrEnvHeader is the "type" key every session-JSONL event carries (the
// writers all use the literal key; only the VALUES have a constant).
const covCrEnvHeader = "type"

// covCrUnknownTaskID is a ledger id that exists in no test database — the
// "row absent" boundary for the chat hooks.
const covCrUnknownTaskID = "cov-cr-nonexistent-session"

// covCrChatTurnsRecorded is how many times
// TestCovCrChatHooks_RegisterRecordCloseCycle calls RecordChatTurn, i.e. the
// number `chats.turn_count` must hold afterwards. ALL THREE advanced it: the
// third call's tokens are folded (not applied) but its turn is still a turn —
// that is what the hook counts, and the fixture is deliberately out of order to
// pin it. Named here (Rule 1) because it is the expected value of a column.
const covCrChatTurnsRecorded = 3

// covCrChatHealedTurns is the number `chats.turn_count` holds after the ONE
// RecordChatTurn the heal path drives (TestCovCrChatHooks_TurnWithoutRowReEnsures).
const covCrChatHealedTurns = 1

// covCrLedgerEnv pins the run's data root and ledger into throwaway dirs.
// Returns the sessions root and the ledger path.
func covCrLedgerEnv(t *testing.T) (sessionsRoot, ledgerPath string) {
	t.Helper()
	root := t.TempDir()
	// config.GopiDirEnv is the gopi-data root override (Rule 1b: the constant,
	// never the literal). GopiDir() = <GOPI_HOME>/.gopi, so the sessions tree is
	// <GOPI_HOME>/.gopi/<config.SessionsDirName>, spelled through
	// config.ProfileDirIn — the ONE join (Rule 2/13/14). This file used to carry
	// its own `covCrGopiDirComponent = ".gopi"` mirror for that component; the
	// constant existed only because the join was hand-rolled here.
	t.Setenv(config.GopiDirEnv, root)
	// The test process may itself be a task-run child; every agent here must
	// be treated as a PLAIN session unless a test says otherwise.
	t.Setenv(tools.TaskWrapperEnv, "")
	sessionsRoot = filepath.Join(config.ProfileDirIn(root), config.SessionsDirName)
	// The ledger is joined THROUGH the profile, never beside it: `root` is
	// $GOPI_HOME and ProfileDirIn(root) is its `.gopi`, so `<root>/memory.db`
	// was a SIBLING of the profile and therefore a FOREIGN ledger for
	// taskNotifyDelivers (the gate compares that path with
	// <GOPI_HOME>/.gopi/memory.db). An event fired from a foreign ledger is
	// recorded and reported but NEVER broadcast — a silent dead end, not a
	// failure — which is the same defect class the suite pin carried until
	// 974bc5c1. config.ProfilePathIn is the ONE join the production resolver
	// uses (Rule 2/13/14), so this fixture and the gate cannot drift apart.
	ledgerPath = config.ProfilePathIn(root, config.MemoryDBFileName)
	t.Setenv(memoryDBEnv, ledgerPath)
	// The stale sweep resolves its threshold through config (CLI > env >
	// settings.json > default) — pin the env so no ambient settings.json
	// (e.g. a developer's task_stall_sec) can influence the result.
	t.Setenv(config.TaskStallSecEnv, covCrStallSec)
	return sessionsRoot, ledgerPath
}

// covCrStallSec is the pinned stale threshold (seconds) for the sweep tests:
// comfortably above the 60s "live" fixture and below the 2h "stale" one.
const covCrStallSec = "1800"

// covCrSessionLogger opens a real SessionLogger under the isolated data root
// (bare layout: sessions/<uuid>/session.jsonl).
func covCrSessionLogger(t *testing.T) *SessionLogger {
	t.Helper()
	sl, err := NewSessionLogger("cov-cr-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { _ = sl.Close() }) //stylegate:ignore test-owned logger closed at cleanup after the test read its file; the descriptor is what matters, not the close error
	return sl
}

// covCrSeedTaskID is the throwaway row that forces the ledger schema to exist;
// it is deleted before any assertion so the tests count only their own rows.
const covCrSeedTaskID = "cov-cr-schema-seed"

// covCrOpenLedger creates the tasks-ledger schema through the production
// writer (openTasksDB refuses a file that is not a gopi-owned distilled
// knowledge base, so the shell must be minted by tools itself, not hand-rolled)
// and hands back a direct connection for fixture inserts.
func covCrOpenLedger(t *testing.T, path string) *sql.DB {
	t.Helper()
	if _, err := tools.TaskNew("cov-cr ledger schema seed", covCrSeedTaskID, "", ""); err != nil {
		t.Fatalf("seed ledger %s: %v", path, err)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore throwaway ledger handle pinned to the scratch root; closed at cleanup, the OS reclaims the descriptor
	if _, err := db.Exec(`DELETE FROM tasks WHERE task_id = ?`, covCrSeedTaskID); err != nil {
		t.Fatalf("drop seed row: %v", err)
	}
	return db
}

// covCrInsertRow inserts one ledger row with explicit liveness stamps, so a
// test can pin the exact arithmetic the deleted DetectStaleTasks used to do.
// status is the
// ledger status value (taskStatus* consts), stamps in unix seconds;
// updated_at tracks created_at, which keeps a row inside the staleness window
// (the sweep's `updated_at < staleSince` re-arm guard) unless a test says
// otherwise. lastToolcall == 0 leaves the column NULL (the created_at
// fallback the sweep COALESCEs onto).
func covCrInsertRow(t *testing.T, db *sql.DB, taskID, status string, lastToolcall, createdAt float64) {
	t.Helper()
	var lt sql.NullFloat64
	if lastToolcall != 0 {
		lt = sql.NullFloat64{Float64: lastToolcall, Valid: true}
	}
	_, err := db.Exec(
		`INSERT INTO tasks (task_id, summary, status, session_id, created_at, updated_at, last_toolcall_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, "cov-cr row "+taskID, status, taskID, createdAt, createdAt, lt)
	if err != nil {
		t.Fatalf("insert ledger row %s: %v", taskID, err)
	}
}

// covCrNow is the unix-seconds clock the stale fixtures are built around.
func covCrNow(t *testing.T) int64 {
	t.Helper()
	return time.Now().Unix()
}

// covCrReadRow reads back the touched columns of a ledger row inside a
// staleness window (last_toolcall_at is non-NULL by fixture design).
func covCrReadRow(t *testing.T, db *sql.DB, taskID string) (updatedAt float64, status string) {
	t.Helper()
	err := db.QueryRow(
		`SELECT updated_at, status FROM tasks WHERE task_id = ?`, taskID).Scan(&updatedAt, &status)
	if err != nil {
		t.Fatalf("read ledger row %s: %v", taskID, err)
	}
	return updatedAt, status
}

// covCrNotifyCmdFile is the delivery command the stale sweep's notify pipeline
// runs: it appends each payload to a file (no network, no window).
func covCrNotifyCmdFile(t *testing.T, out string) string {
	t.Helper()
	return "cat >> " + out
}

// covCrSessionEndLine renders one session-JSONL line of the given event type
// (a minimal writer for the HasSessionEnd fixtures).
func covCrSessionEndLine(typ string) string {
	return `{"` + covCrEnvHeader + `":"` + typ + `","timestamp":"2026-01-01T00:00:00Z"}`
}

// ---------------------------------------------------------------------------
// 1. ParseTaskListBalancedPrefix (tasklist.go:186) — BURNED-BY contract:
// truncated-list recovery (BEGIN present, END forgotten).
// ---------------------------------------------------------------------------

func TestCovCrParseTaskListBalancedPrefix_RecoversTruncatedList(t *testing.T) {
	// Real failure shape: the LLM writes the list and forgets [TASK_LIST_END],
	// with a pasted tool block AFTER the JSON. Balanced-prefix must recover the
	// JSON array and stop exactly at its closing bracket.
	tail := `
[
  {"task_id": "1", "content": "read the source", "status": "completed"},
  {"id": 2, "summary": "write the tests", "status": "progressing"}
]
</parameter></invoke>`

	tasks, err := ParseTaskListBalancedPrefix(tail)
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("err = %v, want ErrTaskListRecovered (recovery is not a failure)", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("recovered %d tasks, want 2 (trailing prose must be stripped)", len(tasks))
	}
	// Alternate field names + numeric id must be resolved, exactly as the
	// closed-block path does (not second-class recovery).
	if tasks[1].TaskID != "2" {
		t.Errorf("numeric id not folded into TaskID: %q", tasks[1].TaskID)
	}
	if tasks[0].Summary != "read the source" || tasks[1].Summary != "write the tests" {
		t.Errorf("alt summary field not resolved: %q / %q", tasks[0].Summary, tasks[1].Summary)
	}
	if !tasks[0].IsCompleted() || !tasks[1].IsProgressing() {
		t.Errorf("statuses lost: %q / %q", tasks[0].Status, tasks[1].Status)
	}
}

func TestCovCrParseTaskListBalancedPrefix_BracketInsideStringDoesNotUnbalance(t *testing.T) {
	// The scan is string-aware: a ']' inside a quoted summary must NOT close
	// the array early (early close = a truncated list silently accepted).
	tail := `[{"task_id":"1","summary":"fix a] and [b","status":"pending"},{"task_id":"2","summary":"second","status":"completed"}]`
	tasks, err := ParseTaskListBalancedPrefix(tail)
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("err = %v, want ErrTaskListRecovered", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("string-aware scan failed: %d tasks, want 2", len(tasks))
	}
	if tasks[0].Summary != "fix a] and [b" {
		t.Errorf("summary truncated at the quoted bracket: %q", tasks[0].Summary)
	}

	// Same contract with an ESCAPED quote before the bracket.
	esc := `[{"task_id":"1","summary":"say \"hi\"] then","status":"pending"}]`
	tasks, err = ParseTaskListBalancedPrefix(esc)
	if !errors.Is(err, ErrTaskListRecovered) || len(tasks) != 1 {
		t.Fatalf("escaped-quote scan: err=%v tasks=%d, want recovered + 1", err, len(tasks))
	}
	if !strings.Contains(tasks[0].Summary, `"]`) {
		t.Errorf("escaped-quote summary mangled: %q", tasks[0].Summary)
	}
}

func TestCovCrParseTaskListBalancedPrefix_UnparseableNeverFabricates(t *testing.T) {
	cases := []struct {
		name string
		tail string
	}{
		{"empty tail", ""},
		{"whitespace only", " \n\t\r "},
		{"prose reference", "see the [TASK_LIST_BEGIN] block above for details"},
		{"bare XML", "<parameter>nothing to see</parameter>"},
		{"mid-JSON truncation", `[{"task_id":"1","summary":"cut off here"`},
		{"closed but not a list", `{"task_id":"1"}`},
		{"closed JSON string", `"just a string"`},
		{"closed array of non-items", `[1,2,3]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := ParseTaskListBalancedPrefix(tc.tail)
			if !errors.Is(err, ErrTaskListUnparseable) {
				t.Fatalf("err = %v, want ErrTaskListUnparseable", err)
			}
			if tasks != nil {
				t.Fatalf("no garbage list may ever be produced, got %+v", tasks)
			}
		})
	}
}

func TestCovCrParseTaskListBalancedPrefix_EmptyArrayIsAnEmptyRecoveredList(t *testing.T) {
	// Boundary: a legitimately EMPTY array is a valid list (non-nil, zero
	// items) — distinct from "unparseable", or the caller could not tell an
	// empty plan from a dropped one.
	tasks, err := ParseTaskListBalancedPrefix("[]")
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("err = %v, want ErrTaskListRecovered", err)
	}
	if tasks == nil || len(tasks) != 0 {
		t.Fatalf("empty array = %+v (len %d), want non-nil empty list", tasks, len(tasks))
	}
}

// ---------------------------------------------------------------------------
// 2. SessionLogFileName (session.go:76) — BURNED-BY contract: same-second
// collision (two runs of one task must not share a log file).
// ---------------------------------------------------------------------------

func TestCovCrSessionLogFileName_SameSecondRunsNeverCollide(t *testing.T) {
	// The pre-S1 defect: the name was derived from the clock
	// (session-<unix>.jsonl), so two runs of the same task inside one second
	// resolved to the SAME file — the second run appended onto the first's
	// JSONL and both read as one session. The contract now: a task run's file
	// name is the RUN UUID (the caller mints it, one per run), and a bare
	// session uses the fixed name inside its own per-session directory.
	const taskID = "t-cov-cr-same-second"
	runA := "11111111-2222-4333-8444-555555555555"
	runB := "99999999-8888-4777-8666-555555555555"

	nameA := SessionLogFileName(runA, taskID)
	nameB := SessionLogFileName(runB, taskID)

	if nameA == nameB {
		t.Fatalf("same-second runs collide: both resolve to %q", nameA)
	}
	if nameA != runA+".jsonl" || nameB != runB+".jsonl" {
		t.Errorf("task-run names must be the run uuid: %q / %q", nameA, nameB)
	}
	if strings.Contains(nameA, "session.jsonl") {
		t.Errorf("task run must not take the bare fixed name: %q", nameA)
	}
}

func TestCovCrSessionLogFileName_BareSessionUsesFixedName(t *testing.T) {
	// Bare (non-task) session: the session id names the DIRECTORY, so the file
	// itself is the fixed session.jsonl — one run per session dir.
	const sessionID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	if got := SessionLogFileName(sessionID, ""); got != "session.jsonl" {
		t.Fatalf("bare session file name = %q, want the fixed session.jsonl", got)
	}
	// Boundary: an empty task id is the ONLY no-task signal (whitespace is a
	// task id, and takes the uuid form).
	if got := SessionLogFileName(sessionID, "   "); got != sessionID+".jsonl" {
		t.Errorf("whitespace task id treated as bare: %q", got)
	}
	// Boundary: both empty still yields the bare name (no panic, no ".jsonl").
	if got := SessionLogFileName("", ""); got != "session.jsonl" {
		t.Errorf("empty session id: %q, want session.jsonl", got)
	}
}

// ---------------------------------------------------------------------------
// 3. AutoTaskRegisterOffValue (taskregister.go:124) — BURNED-BY contract: the
// "off" spelling semantics (the "Off" name + "1" value incident).
// ---------------------------------------------------------------------------

func TestCovCrAutoTaskRegisterOffValue_OffVocabulary(t *testing.T) {
	// The defect this pins: a spawner paired the OFF NAME with the ON VALUE
	// and minted a duplicate ledger row per child. The value predicate must
	// judge the VALUE alone, with the same vocabulary the enabled-check
	// honors — and must be case/whitespace tolerant (an env value is text).
	off := []string{"0", "off", "OFF", "no", "false", "FALSE", "disabled", "  off  ", "\t0\n"}
	for _, v := range off {
		if !AutoTaskRegisterOffValue(v) {
			t.Errorf("AutoTaskRegisterOffValue(%q) = false, want true (OFF spelling)", v)
		}
	}
	on := []string{"1", "true", "yes", "on", "enabled", "", "  ", "00", "0.0", "False!"}
	for _, v := range on {
		if AutoTaskRegisterOffValue(v) {
			t.Errorf("AutoTaskRegisterOffValue(%q) = true, want false (must stay ON)", v)
		}
	}
}

func TestCovCrAutoTaskRegisterOffValue_AgreesWithEnabledAndExportedPair(t *testing.T) {
	// The two faces must never drift: the value-level predicate (spawner-side,
	// value alone) and the env-level enable check (agent-side).
	t.Setenv(autoTaskRegisterEnv, "off")
	if autoTaskRegisterEnabled() {
		t.Fatal("env-level check ignored the OFF spelling")
	}
	if !AutoTaskRegisterOffValue("off") {
		t.Fatal("value-level predicate disagrees with the env-level check on \"off\"")
	}
	t.Setenv(autoTaskRegisterEnv, "1")
	if !autoTaskRegisterEnabled() {
		t.Fatal("default-ON flipped by the ON spelling")
	}
	// The exported NAME=VALUE pair is unsplittable: exactly one '=' is added
	// on top of the pair, whose value is an OFF spelling for this same
	// predicate (the 2026-09-10 defect was this pair being taken apart).
	name, value, found := strings.Cut(AutoTaskRegisterOff, "=")
	if !found || strings.Contains(value, "=") {
		t.Fatalf("AutoTaskRegisterOff = %q, want exactly one NAME=VALUE pair", AutoTaskRegisterOff)
	}
	if name != autoTaskRegisterEnv {
		t.Errorf("pair name = %q, want %q", name, autoTaskRegisterEnv)
	}
	if !AutoTaskRegisterOffValue(value) {
		t.Errorf("pair VALUE %q is not an OFF spelling — a spawner using it would leave registration ON", value)
	}
	if value == "1" {
		t.Fatal("the 2026-09-10 incident verbatim: OFF name paired with the ON value")
	}
}

// ---------------------------------------------------------------------------
// 4. FlexibleString.UnmarshalJSON (tasklist.go:51) — malformed / partial /
// unknown-field handling.
// ---------------------------------------------------------------------------

func TestCovCrFlexibleStringUnmarshalJSON_AcceptsStringAndNumber(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`"abc"`, "abc"},
		{`""`, ""},
		{`1`, "1"},
		{`0`, "0"},
		{`-7`, "-7"},
		{`1.5`, "1.5"},
		{`1e3`, "1e3"}, // json.Number keeps the literal text, never a float render
		{`"42"`, "42"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var f FlexibleString
			if err := f.UnmarshalJSON([]byte(tc.raw)); err != nil {
				t.Fatalf("UnmarshalJSON(%s): %v", tc.raw, err)
			}
			if string(f) != tc.want {
				t.Fatalf("UnmarshalJSON(%s) = %q, want %q", tc.raw, string(f), tc.want)
			}
			if f.String() != tc.want {
				t.Errorf("String() = %q, want %q", f.String(), tc.want)
			}
		})
	}
}

func TestCovCrFlexibleStringUnmarshalJSON_RejectsMalformed(t *testing.T) {
	// Malformed input must ERROR (never silently leave a stale/zero value):
	// an object, an array, bare unquoted text, a truncated string, a bare word.
	// (null is deliberately NOT in this list: encoding/json gives an
	// Unmarshaler null and the standard convention is to accept it as a no-op,
	// so both branches fail cleanly and the receiver is left untouched — which
	// the assertion below pins for every row.)
	for _, raw := range []string{`{}`, `[]`, `abc`, `{"a":1}`, `"unterminated`, ``, `tru`, `NaN`} {
		t.Run(raw, func(t *testing.T) {
			f := FlexibleString("unchanged")
			if err := f.UnmarshalJSON([]byte(raw)); err == nil {
				t.Fatalf("UnmarshalJSON(%q) accepted malformed input as %q", raw, string(f))
			}
			if f != "unchanged" {
				t.Errorf("failed unmarshal mutated the receiver to %q — partial state leaked", string(f))
			}
		})
	}
	// A bool is a valid JSON scalar but not an id — must error, not become "true".
	var f FlexibleString
	if err := f.UnmarshalJSON([]byte(`true`)); err == nil {
		t.Fatalf("bool accepted as an id: %q", string(f))
	}
}

func TestCovCrFlexibleStringUnmarshalJSON_PartialAndUnknownFields(t *testing.T) {
	// Partial document (a missing id) + an UNKNOWN field must not break the
	// decode, and the alternate-name fallback must still fill the canonical
	// field after Resolve().
	var item TaskItem
	body := `{"content":"only a summary","status":"pending","future_field":{"nested":[1,2]}}`
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		t.Fatalf("partial/unknown-field decode failed: %v", err)
	}
	if item.TaskID != "" {
		t.Errorf("missing canonical id should stay empty, got %q", item.TaskID)
	}
	item.Resolve()
	if item.Summary != "only a summary" || !item.IsPending() {
		t.Fatalf("alt-name fallback broken: %+v", item)
	}

	// A MALFORMED id inside a full item document must surface an error rather
	// than silently dropping the field (Rule 15: no silent loss).
	if err := json.Unmarshal([]byte(`{"task_id":{"nested":true}}`), &item); err == nil {
		t.Fatal("malformed task_id accepted silently")
	}

	// Numeric ids through the whole TaskItem decode, then round-trip clean.
	var list []TaskItem
	if err := json.Unmarshal([]byte(`[{"id":7,"content":"seven","status":"completed"}]`), &list); err != nil {
		t.Fatalf("numeric-id list decode: %v", err)
	}
	list[0].Resolve()
	if list[0].TaskID != "7" || !list[0].IsCompleted() {
		t.Fatalf("numeric id/alt-name resolution failed: %+v", list[0])
	}
	out, err := json.Marshal(list[0])
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !strings.Contains(string(out), `"task_id":"7"`) {
		t.Errorf("round-trip lost the resolved id: %s", out)
	}
}

// ---------------------------------------------------------------------------
// 5. CountStatus (tasklist.go:315).
// ---------------------------------------------------------------------------

func TestCovCrCountStatus_CountsPerVocabulary(t *testing.T) {
	// A PLAN fixture: three states, one of each, plus a decoy status that is
	// not in the vocabulary (never counted, for any query).
	tasks := []TaskItem{
		{TaskID: "1", Status: TaskStatusPending},
		{TaskID: "2", Status: TaskStatusProgressing},
		{TaskID: "3", Status: TaskStatusCompleted},
		{TaskID: "4", Status: TaskStatusCompleted},
		{TaskID: "5", Status: "blocked"},
	}
	if got := CountStatus(tasks, TaskStatusCompleted); got != 2 {
		t.Errorf("CountStatus(completed) = %d, want 2", got)
	}
	if got := CountStatus(tasks, TaskStatusPending); got != 1 {
		t.Errorf("CountStatus(pending) = %d, want 1", got)
	}
	if got := CountStatus(tasks, TaskStatusProgressing); got != 1 {
		t.Errorf("CountStatus(progressing) = %d, want 1", got)
	}
	if got := CountStatus(tasks, "blocked"); got != 1 {
		t.Errorf("exact-match query over an out-of-vocabulary status = %d, want 1", got)
	}
	// The wrappers are this function's contract holders — the count must agree
	// with them (they are what the heartbeat/exit gates actually call).
	if CountPending(tasks) != CountStatus(tasks, TaskStatusPending) ||
		CountProgressing(tasks) != CountStatus(tasks, TaskStatusProgressing) {
		t.Error("CountPending/CountProgressing disagree with CountStatus")
	}
}

func TestCovCrCountStatus_EmptyZeroAndCaseSensitive(t *testing.T) {
	if got := CountStatus(nil, TaskStatusCompleted); got != 0 {
		t.Errorf("CountStatus(nil) = %d, want 0", got)
	}
	if got := CountStatus([]TaskItem{}, TaskStatusCompleted); got != 0 {
		t.Errorf("CountStatus(empty) = %d, want 0", got)
	}
	if got := CountStatus([]TaskItem{{Status: ""}}, ""); got != 1 {
		t.Errorf("empty-status rows are matched by the empty query: got %d", got)
	}
	// Status values are a closed vocabulary written by the writers above —
	// the match is exact, so a differently-cased duplicate is a DIFFERENT
	// status and must not be counted as completed.
	if got := CountStatus([]TaskItem{{Status: "Completed"}}, TaskStatusCompleted); got != 0 {
		t.Errorf("case-insensitive match leaked: got %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 7. DetectStaleTasks (DELETED 2026-11-05) — was a BURNED-BY contract: the
// false-positive guard (a live/fresh row must never be reported stale).
// ---------------------------------------------------------------------------
// The two DetectStaleTasks coverage arms are GONE with the detector itself
// (2026-11-05): the stale sweep — its candidate query, its `task list --sweep`
// trigger, its persisted throttle stamp and its re-arm UPDATE — was deleted, so
// there is no report to count and no dedup window to pin. What the arms really
// covered (the notify pipeline delivering an alarm through settings/env, and a
// best-effort read of an unopenable ledger) is pinned where it still lives:
// internal/tools/tasknotify_test.go (TestAlarmEventLandsWithSettingsOnlyConfig).

// ---------------------------------------------------------------------------
// 8. HasSessionEnd (session.go:595).
// ---------------------------------------------------------------------------

func TestCovCrHasSessionEnd_TrueOnlyForEndTail(t *testing.T) {
	dir := t.TempDir()
	endLine := covCrSessionEndLine("session_end")
	startLine := covCrSessionEndLine("session_start")
	userLine := covCrSessionEndLine("user")

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"ends with session_end", startLine + "\n" + userLine + "\n" + endLine + "\n", true},
		{"session_end as only line, no trailing newline", endLine, true},
		{"session_end as only line, trailing newline", endLine + "\n", true},
		// Established by b33dda6e (fix(crashdump): classify orphan sessions by
		// liveness, not by a missing tail) + the newline-bounded tail walk: an
		// event AFTER session_end does not un-end the session — the file still
		// carries session_end within the scan-back window, and that is the
		// predicate's contract ("does the JSONL contain session_end?"), not
		// "is session_end the last line?". Pinned by
		// internal/agent/sessionend_tail_test.go.
		{"session_end then a later event", startLine + "\n" + endLine + "\n" + userLine + "\n", true},
		{"started but never ended (kill -9)", startLine + "\n" + userLine + "\n", false},
		{"empty file", "", false},
		// A clean trailing line of a DIFFERENT event type is not an end.
		{"other event type as the tail", startLine + "\n" + covCrSessionEndLine("tool_result") + "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".jsonl")
			if err := os.WriteFile(p, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := HasSessionEnd(p); got != tc.want {
				t.Fatalf("HasSessionEnd = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8b. HasSessionEnd path boundaries + the KNOWN GAP found while writing this
// slice.
// ---------------------------------------------------------------------------

// TestCovCrHasSessionEnd_GarbageTailAfterEnd pins the behaviour the tail walk
// settled on: a session_end followed by a HALF-WRITTEN line (SIGKILL mid-write,
// no trailing newline) still reports as ENDED.
//
// HISTORY (why this test changed meaning): it was written when the walk pinned
// every candidate's end to the file end, so a torn tail was spliced onto the
// session_end candidate and this shape misreported as an orphan. That defect was
// found and fixed — the candidate slice is newline-bounded now (a MOVING end
// pointer), pinned by internal/agent/sessionend_tail_test.go
// (TestHasSessionEndGarbageTailAfterSessionEnd) and by the classification change
// b33dda6e (fix(crashdump): classify orphan sessions by liveness, not by a
// missing tail). The old assertion outlived the fix and turned master red; it is
// flipped here, as its own failure message instructed ("flip this assertion and
// close the filed bug").
//
// The clean control below still runs: the same content WITHOUT the partial line
// is detected, so a true result here cannot come from a broken fixture.
func TestCovCrHasSessionEnd_GarbageTailAfterEnd(t *testing.T) {
	p := filepath.Join(t.TempDir(), "garbage-tail.jsonl")
	content := covCrSessionEndLine("session_start") + "\n" +
		covCrSessionEndLine("session_end") + "\n" +
		`{"type":"user","content":"half-writ`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !HasSessionEnd(p) {
		t.Fatal("a cleanly-ended session with a torn trailing line must report ENDED " +
			"(the tail walk is newline-bounded — see sessionend_tail_test.go); a false here " +
			"means the walk regressed to pinning candidates to the file end")
	}
	// The clean control: the SAME content without the partial trailing line is
	// detected, so a true result above cannot be an artifact of the fixture.
	clean := filepath.Join(t.TempDir(), "clean.jsonl")
	if err := os.WriteFile(clean, []byte(covCrSessionEndLine("session_start")+"\n"+covCrSessionEndLine("session_end")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !HasSessionEnd(clean) {
		t.Fatal("clean control failed — the fixture itself is wrong")
	}
}

func TestCovCrHasSessionEnd_MissingAndDirectoryPaths(t *testing.T) {
	dir := t.TempDir()
	if HasSessionEnd(filepath.Join(dir, "does-not-exist.jsonl")) {
		t.Error("a missing file must report false, not panic")
	}
	// A DIRECTORY is the free "opens but cannot be read as a file" case.
	if HasSessionEnd(dir) {
		t.Error("a directory path must report false (best-effort)")
	}
}

// ---------------------------------------------------------------------------
// 9. ErrorRecord.SessionEndBlock (session.go:497).
// ---------------------------------------------------------------------------

func TestCovCrSessionEndBlock_BoundedKeysAndOmission(t *testing.T) {
	full := &ErrorRecord{
		Category:          "5xx",
		HTTPStatus:        502,
		ErrorBody:         strings.Repeat("e", sessionEndErrorBodyMax+128),
		TransportErr:      strings.Repeat("t", sessionEndErrorBodyMax+128),
		LastRequestDigest: "model=x msgs=9 last=user bytes=12345",
		FailedIteration:   3,
		RecoverableHint:   true,
		Remedy:            strings.Repeat("r", remedyTextMax+64),
		DumpPath:          "/tmp/session.dump.json",
	}
	block := full.SessionEndBlock()

	// Always-present keys (the four the reader indexes unconditionally).
	for _, k := range []string{"category", "failed_iteration", "recoverable_hint", "last_request_digest"} {
		if _, ok := block[k]; !ok {
			t.Errorf("unconditional key %q missing from the error block", k)
		}
	}
	if block["category"] != "5xx" || block["failed_iteration"] != 3 || block["recoverable_hint"] != true {
		t.Errorf("scalar fields wrong: %+v", block)
	}
	if block["http_status"] != 502 || block["dump_path"] != "/tmp/session.dump.json" {
		t.Errorf("optional scalars missing: %+v", block)
	}
	if block["remedy"] != tailRunes(full.Remedy, remedyTextMax) {
		t.Errorf("remedy not bounded by remedyTextMax: %d runes", len([]rune(block["remedy"].(string))))
	}

	// Bounded bodies: the session_end line carries a bounded body (the dump
	// file keeps the full one) — a runaway error body must not bloat the JSONL.
	for _, k := range []string{"error_body", "transport_error"} {
		s, ok := block[k].(string)
		if !ok {
			t.Fatalf("%s not rendered as a string: %T", k, block[k])
		}
		if len([]rune(s)) > sessionEndErrorBodyMax+len("…(head trimmed)…") {
			t.Errorf("%s not bounded: %d runes", k, len([]rune(s)))
		}
		if !strings.Contains(s, "head trimmed") {
			t.Errorf("%s lost the head-trim marker: %q", k, s[:min(len(s), 40)])
		}
	}
}

func TestCovCrSessionEndBlock_MinimalRecordOmitsEverythingOptional(t *testing.T) {
	// A clean-ish failure (no status, no body, no remedy, no dump): only the
	// four unconditional keys may appear — presence is the failure marker for
	// the optional ones, so an always-present empty key would be a lie.
	block := (&ErrorRecord{Category: "network"}).SessionEndBlock()
	want := map[string]bool{
		"category": true, "failed_iteration": true, "recoverable_hint": true, "last_request_digest": true,
	}
	if len(block) != len(want) {
		t.Fatalf("minimal record rendered %d keys (%+v), want only the 4 unconditional ones", len(block), block)
	}
	for k := range block {
		if !want[k] {
			t.Errorf("optional key %q present on an empty record", k)
		}
	}
	// HTTPStatus 0 = "no HTTP status" (omitted), NOT http_status: 0.
	if _, ok := block["http_status"]; ok {
		t.Error("http_status=0 must be omitted (0 means transport/vendor/panic)")
	}
	// Boundary: a 4xx status IS carried.
	if b := (&ErrorRecord{Category: "4xx", HTTPStatus: 400}).SessionEndBlock(); b["http_status"] != 400 {
		t.Errorf("4xx status dropped: %+v", b)
	}
	// Boundary: an empty remedy string must not be rendered (presence would be
	// a lie — no remedy was recorded).
	if b := (&ErrorRecord{Remedy: ""}).SessionEndBlock(); b["remedy"] != nil {
		t.Errorf("empty remedy string was rendered: %+v", b)
	}
}

// ---------------------------------------------------------------------------
// 10. (*SessionLogger).LogHippocampusBrief (session.go:742).
// ---------------------------------------------------------------------------

func TestCovCrLogHippocampusBrief_ArchivesRawSignals(t *testing.T) {
	sessionsRoot, _ := covCrLedgerEnv(t)
	sl := covCrSessionLogger(t)

	rec := hippocampusBriefRecord{
		Level:        hippocampusLvTextArchive,
		Summary:      "found a matching recipe (memory id 1727)",
		Confidence:   0.85,
		MemoryWeight: 0.42,
		Memories:     3,
		DurationMS:   17,
		Round:        4,
	}
	if err := sl.LogHippocampusBrief(rec); err != nil {
		t.Fatalf("LogHippocampusBrief: %v", err)
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// The archive event lands in the session's own JSONL, carrying the organ's
	// raw signals plus the scheduler's decision.
	path := filepath.Join(sessionsRoot, sl.SessionID(), SessionLogFileName(sl.SessionID(), ""))
	ev := covCrFindEvent(t, path, hippocampusBriefEvent)
	if ev == nil {
		t.Fatalf("no %s event in %s", hippocampusBriefEvent, path)
	}
	if ev[hippocampusLogKeyLevel] != hippocampusLvTextArchive ||
		ev[hippocampusLogKeyConfidence] != 0.85 ||
		ev[hippocampusLogKeyMemoryWeight] != 0.42 {
		t.Errorf("organ signals not archived verbatim: %+v", ev)
	}
	if ev[hippocampusLogKeyMemories] != float64(3) || ev[hippocampusLogKeyRound] != float64(4) {
		t.Errorf("audit counters wrong: %+v", ev)
	}
	if _, ok := ev["message"]; ok {
		t.Error("the archive event must NOT carry a message key (the two JSONL consumers switch on it)")
	}
	if ev[covCrEnvHeader] != hippocampusBriefEvent {
		t.Errorf("event type key missing: %+v", ev)
	}
}

func TestCovCrLogHippocampusBrief_ZeroRecordAndClosedLogger(t *testing.T) {
	sessionsRoot, _ := covCrLedgerEnv(t)
	sl := covCrSessionLogger(t)

	// Boundary: the zero record (a failed recall with no signals) is still a
	// legitimate audit row — it must be written, not silently skipped.
	if err := sl.LogHippocampusBrief(hippocampusBriefRecord{}); err != nil {
		t.Fatalf("zero record: %v", err)
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	path := filepath.Join(sessionsRoot, sl.SessionID(), SessionLogFileName(sl.SessionID(), ""))
	ev := covCrFindEvent(t, path, hippocampusBriefEvent)
	if ev == nil {
		t.Fatal("zero record was not archived")
	}
	if ev[hippocampusLogKeyLevel] != "" || ev[hippocampusLogKeyDurationMS] != float64(0) {
		t.Errorf("zero record not archived verbatim: %+v", ev)
	}

	// Error path: the logger buffers (sessionLogSyncThreshold), so the failure
	// surfaces at the FLUSH — which is exactly why every round boundary and the
	// task end Sync. After the fd is closed, a Sync must report the write
	// failure rather than silently dropping the buffered archive event (the
	// recording channel the caller logs).
	sl.file.Close() //stylegate:ignore the close IS the fixture: the assertion below proves a write after close surfaces at the flush rather than landing silently
	if err := sl.LogHippocampusBrief(hippocampusBriefRecord{Level: hippocampusLvTextArchive}); err != nil {
		t.Fatalf("buffered write should succeed; the flush is where it fails: %v", err)
	}
	if err := sl.Sync(); err == nil {
		t.Error("flush to a closed session log reported success — a dropped archive event would be silent")
	}
}

// ---------------------------------------------------------------------------
// 11. (*atomicValueFloat/Int).Load (crashdump.go:156/166) — the panic-recover
// stat reader.
// ---------------------------------------------------------------------------

func TestCovCrAtomicValueLoad_ZeroThenStoredValue(t *testing.T) {
	// The panic path reads these atomics WITHOUT a prior Store when the panic
	// fires before the stats were recorded — a zero value must come back as 0,
	// never a panic or a type assertion on nil.
	var f atomicValueFloat
	if got := f.Load(); got != 0 {
		t.Fatalf("zero atomicValueFloat.Load() = %v, want 0", got)
	}
	f.Store(12.5)
	if got := f.Load(); got != 12.5 {
		t.Fatalf("atomicValueFloat.Load() = %v, want 12.5", got)
	}
	f.Store(0)
	if got := f.Load(); got != 0 {
		t.Fatalf("re-stored zero = %v, want 0", got)
	}

	var i atomicValueInt
	if got := i.Load(); got != 0 {
		t.Fatalf("zero atomicValueInt.Load() = %d, want 0", got)
	}
	i.Store(7)
	if got := i.Load(); got != 7 {
		t.Fatalf("atomicValueInt.Load() = %d, want 7", got)
	}
	i.Store(-7)
	if got := i.Load(); got != -7 {
		t.Fatalf("negative value = %d, want -7", got)
	}

	// The three package-level stat slots reset per run and are read together on
	// the recover path: a half-written set must read as independent values.
	panicElapsed.Store(3.25)
	panicIterations.Store(9)
	panicTokens.Store(1234)
	if panicElapsed.Load() != 3.25 || panicIterations.Load() != 9 || panicTokens.Load() != 1234 {
		t.Fatalf("pooled stats = (%v, %d, %d), want (3.25, 9, 1234)",
			panicElapsed.Load(), panicIterations.Load(), panicTokens.Load())
	}
	panicElapsed.Store(0)
	panicIterations.Store(0)
	panicTokens.Store(0)
}

// ---------------------------------------------------------------------------
// 12-15. Chat hooks: RegisterChatSession / RecordChatTurn / CloseChatRow /
// ChatRegisterLatch (chatregister.go:54/82/98/117).
// ---------------------------------------------------------------------------

func TestCovCrChatHooks_RegisterRecordCloseCycle(t *testing.T) {
	covCrLedgerEnv(t)
	t.Setenv(chatRegisterEnv, "1") // pin the kill switch ON (default-ON contract)

	// The ENV-LEVEL off switch is part of the same contract.
	t.Setenv(chatRegisterEnv, "off")
	off := &Agent{SessionLog: covCrSessionLogger(t)}
	off.RegisterChatSession("chat", "must not register", inputsource.Human)
	if off.ChatRegisterLatch() {
		t.Fatal("ChatRegisterLatch latched true while the registration env was OFF")
	}
	off.RecordChatTurn(10, 1, 0)
	off.CloseChatRow(0)
	if off.ChatRegisterLatch() {
		t.Fatal("a switch-off agent ended up latched")
	}

	t.Setenv(chatRegisterEnv, "1")
	a := &Agent{SessionLog: covCrSessionLogger(t)}
	if a.ChatRegisterLatch() {
		t.Fatal("a fresh agent must not report a latched chat row")
	}

	// START hook: row created, latched. The first message is folded to a
	// single bounded line.
	a.RegisterChatSession(covCrChatModeCLI, "  first   message \n with   newlines ", inputsource.Human)
	if !a.ChatRegisterLatch() {
		t.Fatal("RegisterChatSession did not latch")
	}
	list, err := tools.ChatList(tools.ChatListFilter{})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	if !strings.Contains(list, a.SessionLog.SessionID()) {
		t.Fatalf("chat row missing from the index:\n%s", list)
	}
	if !strings.Contains(list, "first message with newlines") {
		t.Errorf("first message not normalized to one line:\n%s", list)
	}

	// Idempotence: a second START (the web/cli re-entry path) must not create a
	// second row or reset the latch.
	a.RegisterChatSession(covCrChatModeCLI, "second call", inputsource.Human)
	if !a.ChatRegisterLatch() {
		t.Fatal("re-registration cleared the latch")
	}

	// PER-TURN hook: turns bump, tokens fold MONOTONICALLY (cumulative totals
	// are passed in, so a smaller later value must never shrink the row).
	a.RecordChatTurn(100, 20, 90)
	a.RecordChatTurn(300, 50, 280)
	a.RecordChatTurn(50, 5, 5) // out-of-order / duplicate: folded, not applied
	list, err = tools.ChatList(tools.ChatListFilter{Query: a.SessionLog.SessionID()})
	if err != nil {
		t.Fatalf("ChatList after turns: %v", err)
	}
	// THE LEDGER COLUMN, READ AS THE LEDGER COLUMN. `chats.turn_count` is what
	// this hook writes, and since epic 2026-11-04 the bare word `turns` on the
	// index row does NOT carry it: that word carries the SESSION LOG's opened
	// count (tools/chat_turns.go: chatTurnClause renders TurnsOpened), while the
	// ledger's number rides its own word on the per-session faces. Asserting the
	// bare word here therefore measured a DIFFERENT quantity, and it only ever
	// passed when no turn counter was installed — the row then rendered
	// "turns not measured — …" and the substring matched by accident.
	//
	// MEASURED on this ground (evidence/02-probe.txt): with the REAL counter
	// installed (cmd/agent's sessionTurnsFromLog, verbatim) this fixture's own
	// session log opens NO turn, so the row reads `turns 0` while TurnCount is 3.
	// The ledger column is the fact under test and it is read through its field.
	detail, err := tools.ChatShow(a.SessionLog.SessionID())
	if err != nil {
		t.Fatalf("ChatShow after turns: %v", err)
	}
	if detail.TurnCount != covCrChatTurnsRecorded {
		t.Errorf("chats.turn_count = %d after three RecordChatTurn calls, want %d",
			detail.TurnCount, covCrChatTurnsRecorded)
	}
	// AND THE BARE WORD ON THAT ROW DOES NOT CARRY IT YET (Rule 15, the other
	// direction). `turns` renders the session log's OPENED count; this fixture's
	// log is seen by no counter in this process, so the row states that the
	// count is unmeasured rather than printing the ledger's 3 under a word that
	// no longer means it. The sentence is asserted POSITIVELY — an absence-only
	// check would pass on a row that rendered no turn fact at all, which is the
	// vacuous shape this whole repair exists to remove.
	if !strings.Contains(list, tools.ChatTurnsNoCounterNote) {
		t.Errorf("the index row lost the state of its turn count — with no counter installed the bare word %q "+
			"must say the count was not measured. Note (tools.ChatTurnsNoCounterNote): %q\n%s",
			sessionevent.CounterUnitTurn, tools.ChatTurnsNoCounterNote, list)
	}
	if !strings.Contains(list, "total tok in=300 out=50 cache=280") {
		t.Errorf("tokens not folded monotonically (want the cumulative totals 300/50/280, labelled as totals):\n%s", list)
	}
	if !strings.Contains(list, "running") {
		t.Errorf("a chat mid-conversation must still read running:\n%s", list)
	}

	// END hook: exit 0 → normal; latched off; idempotent second close.
	a.CloseChatRow(0)
	if a.ChatRegisterLatch() {
		t.Fatal("CloseChatRow did not clear the latch")
	}
	list, err = tools.ChatList(tools.ChatListFilter{Query: a.SessionLog.SessionID()})
	if err != nil {
		t.Fatalf("ChatList after close: %v", err)
	}
	if !strings.Contains(list, "normal") || strings.Contains(list, "running") {
		t.Errorf("exit 0 must stamp end_reason=normal:\n%s", list)
	}
	a.CloseChatRow(0) // no latch → no-op, and the first ending stands
}

func TestCovCrChatHooks_ExitCodeReasonAndGuards(t *testing.T) {
	covCrLedgerEnv(t)
	t.Setenv(chatRegisterEnv, "1")

	// A non-zero chat exit is an API/turn failure → interrupt.
	interrupted := &Agent{SessionLog: covCrSessionLogger(t)}
	interrupted.RegisterChatSession(covCrChatModeCLI, "fails mid-turn", inputsource.Human)
	interrupted.CloseChatRow(1)
	list, err := tools.ChatList(tools.ChatListFilter{Query: interrupted.SessionLog.SessionID()})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	if !strings.Contains(list, "interrupt") {
		t.Errorf("non-zero exit must stamp end_reason=interrupt:\n%s", list)
	}

	// Guard: no session logger → every hook is a no-op (never a nil panic).
	noLog := &Agent{}
	noLog.RegisterChatSession(covCrChatModeCLI, "no session", inputsource.Human)
	noLog.RecordChatTurn(1, 1, 1)
	noLog.CloseChatRow(0)
	if noLog.ChatRegisterLatch() {
		t.Fatal("a SessionLog-less agent latched")
	}

	// Guard: a session that was never registered cannot be closed silently —
	// CloseChatRow without the latch must not create or end anything.
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "unrelated.db"))
	unregistered := &Agent{SessionLog: covCrSessionLogger(t)}
	unregistered.CloseChatRow(9)
	if unregistered.ChatRegisterLatch() {
		t.Fatal("closing an unregistered chat latched")
	}

	// Guard: the ensure path REQUIRES a session id — an empty one fails the
	// registration (reported through the log, latch stays false) rather than
	// writing a junk row.
	empty := &Agent{SessionLog: &SessionLogger{}}
	empty.RegisterChatSession(covCrChatModeCLI, "no id", inputsource.Human)
	if empty.ChatRegisterLatch() {
		t.Fatal("registration without a session id latched")
	}
}

func TestCovCrChatHooks_TurnWithoutRowReEnsures(t *testing.T) {
	_, ledger := covCrLedgerEnv(t)
	t.Setenv(chatRegisterEnv, "1")
	dbPath := ledger

	// The heal path: RecordChatTurn on a row that does not exist re-ensures the
	// row (once) instead of losing the chat from the index.
	a := &Agent{SessionLog: covCrSessionLogger(t)}
	a.RecordChatTurn(11, 2, 1)
	if !a.ChatRegisterLatch() {
		t.Fatal("RecordChatTurn did not re-ensure the missing row")
	}
	list, err := tools.ChatList(tools.ChatListFilter{Query: a.SessionLog.SessionID()})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	if !strings.Contains(list, a.SessionLog.SessionID()) {
		t.Fatalf("the healed row is missing from the index:\n%s", list)
	}
	// THE HEAL PATH's SUBJECT IS THE LEDGER COLUMN, so it is read as one. The
	// bare word `turns` on this row carries the session LOG's opened count since
	// epic 2026-11-04 (tools/chat_turns.go: chatTurnClause renders TurnsOpened),
	// NOT `chats.turn_count` — the quantity RecordChatTurnOutcome advances. The
	// old substring assertion therefore measured the other number, and it only
	// passed while no turn counter was installed in this process (the row then
	// rendered "turns not measured — …"). MEASURED on this ground with the REAL
	// counter installed: this fixture's log opens no turn, so the row reads
	// `turns 0` while the healed ledger row holds 1 — evidence/02-probe.txt.
	healed, err := tools.ChatShow(a.SessionLog.SessionID())
	if err != nil {
		t.Fatalf("ChatShow: %v", err)
	}
	if healed.TurnCount != covCrChatHealedTurns {
		t.Errorf("the re-ensured row's chats.turn_count = %d, want %d (the turn it healed for)",
			healed.TurnCount, covCrChatHealedTurns)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("ledger not created at the pinned path: %v", err)
	}

	// Error path: an UNOPENABLE chat db (a directory where the file belongs)
	// must not latch and must not panic — best-effort, chat continues.
	t.Setenv(memoryDBEnv, t.TempDir())
	failing := &Agent{SessionLog: covCrSessionLogger(t)}
	failing.RegisterChatSession(covCrChatModeWeb, "db is a directory", inputsource.Human)
	if failing.ChatRegisterLatch() {
		t.Fatal("registration latched despite an unopenable chat db")
	}
	failing.RecordChatTurn(1, 1, 1)
	failing.CloseChatRow(0)
	if failing.ChatRegisterLatch() {
		t.Fatal("a failed chat db ended up latched")
	}
	// The unknown-id boundary at the tools layer that this heal path exists for.
	if err := tools.ChatRecordTurn(covCrUnknownTaskID, 1, 1, 1); err == nil {
		t.Error("ChatRecordTurn on a nonexistent row must report an error (the heal trigger)")
	}
}

// TestCovCrChatHooks_OfferFirstMsgYieldsToTheOperator closes the hook that makes
// the whole first_msg design worth having: a session born on a NOTICE turn
// carries the placeholder, and the operator's first real line must be able to
// take that column afterwards — once, and never be displaced again.
//
// It is the agent-layer half of internal/tools' TestChatFirstMsgPlaceholderYieldsToTheOperator:
// that one drives tools.ChatOfferFirstMsg directly, this one proves the Agent
// wrapper reaches it through the SAME kill switch and nil-log guard its siblings
// respect.
func TestCovCrChatHooks_OfferFirstMsgYieldsToTheOperator(t *testing.T) {
	_, ledger := covCrLedgerEnv(t)
	if _, err := os.Stat(ledger); err == nil {
		t.Fatal("the ledger already exists; this case measures the write it causes")
	}
	t.Setenv(chatRegisterEnv, "1")

	// A notice-opened session: the text is framework chatter under ANY face, and
	// the row it creates must carry the placeholder rather than the notice.
	const noticeText = "[system] 2 notices are pending: external message aa, external message bb"
	a := &Agent{SessionLog: covCrSessionLogger(t)}
	a.RegisterChatSession(covCrChatModeCLI, noticeText, inputsource.Notify)
	id := a.SessionLog.SessionID()

	d, err := tools.ChatShow(id)
	if err != nil {
		t.Fatalf("ChatShow after registration: %v", err)
	}
	if d.FirstMsg != tools.ChatFirstMsgPlaceholder {
		t.Fatalf("notice-opened row first_msg = %q; want the placeholder", d.FirstMsg)
	}

	// The kill switch is the same ONE gate the siblings ask: off means no offer.
	t.Setenv(chatRegisterEnv, "off")
	a.OfferChatFirstMsg("这段不该写进去", inputsource.Human)
	if d, err = tools.ChatShow(id); err != nil {
		t.Fatalf("ChatShow after a switch-off offer: %v", err)
	} else if d.FirstMsg != tools.ChatFirstMsgPlaceholder {
		t.Errorf("offer wrote through a switched-off gate: first_msg = %q", d.FirstMsg)
	}
	t.Setenv(chatRegisterEnv, "1")

	// A NOTICE restated under the human face must not win either — the face
	// alone is not evidence, which is why the predicate also opens the text.
	a.OfferChatFirstMsg(noticeText, inputsource.Human)
	if d, err = tools.ChatShow(id); err != nil {
		t.Fatalf("ChatShow after a restated notice: %v", err)
	} else if d.FirstMsg != tools.ChatFirstMsgPlaceholder {
		t.Errorf("a notice restated under the human face took the column: %q", d.FirstMsg)
	}

	// The operator's real line wins, and only the FIRST one.
	a.OfferChatFirstMsg("为什么 worker 一直死？", inputsource.Human)
	if d, err = tools.ChatShow(id); err != nil {
		t.Fatalf("ChatShow after the operator's line: %v", err)
	} else if d.FirstMsg != "为什么 worker 一直死？" {
		t.Fatalf("operator line did not take the column: %q", d.FirstMsg)
	}
	a.OfferChatFirstMsg("第二句话不该覆盖标题", inputsource.Human)
	if d, err = tools.ChatShow(id); err != nil {
		t.Fatalf("ChatShow after a second offer: %v", err)
	} else if d.FirstMsg != "为什么 worker 一直死？" {
		t.Errorf("a later line displaced the first: %q", d.FirstMsg)
	}

	// A nil log is not a panic and not a write — the guard its siblings have.
	(&Agent{}).OfferChatFirstMsg("no session log", inputsource.Human)
}

// ---------------------------------------------------------------------------
// covCr test helpers used above.
// ---------------------------------------------------------------------------

// covCrChatModeCLI / covCrChatModeWeb are the two valid chat modes. The
// values are the tools package's own mode consts (chatRegister.go passes them
// straight through to ChatEnsureRow, whose validChatMode gate rejects anything
// else) — referenced here so this file never re-types the wire values.
const (
	covCrChatModeCLI = tools.ChatModeCLI
	covCrChatModeWeb = tools.ChatModeWeb
)

// covCrFindEvent returns the last session-JSONL event of the given type.
func covCrFindEvent(t *testing.T, path, typ string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session log %s: %v", path, err)
	}
	var found map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("session log line is not JSON: %q", line)
		}
		if ev[covCrEnvHeader] == typ {
			found = ev
		}
	}
	return found
}

// TestCovCrLedgerEnvPinsTheProfilesOwnLedger is the regression guard for the
// fixture that spelled its ledger as a SIBLING of the profile it pinned
// (measured 2026-11-05, task 01a1aab9).
//
// covCrLedgerEnv pins GOPI_HOME=root and used to pin the ledger at
// <root>/memory.db — i.e. BESIDE <root>/.gopi, which is the profile the
// production resolver reads. tools.taskNotifyDelivers compares
// memoryDBPath() with defaultMemoryDBPath() (= config.ProfilePathIn(
// $GOPI_HOME, memory.db)) and treats a mismatch as a FOREIGN ledger: its
// events are recorded and reported on stderr but NEVER broadcast. So any
// coverage arm here that fired an alarm was a silent dead end, and the same
// sibling shape made the whole suite a foreign-ledger run until 974bc5c1 fixed
// the SUITE pin. The per-test fixture was the remainder.
//
// The assertion is the RELATION (this ledger vs this profile's own ledger), and
// it is deliberately checked through config.ProfilePathIn rather than by
// comparing against a literal — a literal here would be a second definition of
// the join the gate uses (Rule 14) and could agree with the fixture while
// disagreeing with the gate.
func TestCovCrLedgerEnvPinsTheProfilesOwnLedger(t *testing.T) {
	_, ledger := covCrLedgerEnv(t)
	want := config.ProfilePathIn(os.Getenv(config.GopiDirEnv), config.MemoryDBFileName)
	if filepath.Clean(ledger) != filepath.Clean(want) {
		t.Fatalf("covCrLedgerEnv pinned the ledger at %q but the profile it pinned (%s=%q) owns %q — a ledger "+
			"beside the profile is FOREIGN to taskNotifyDelivers, so every event fired under this fixture is "+
			"recorded and REPORTED but never broadcast (the notify assertion then fails on the absence of the "+
			"file its own code was told to write). Join the ledger through config.ProfilePathIn, never beside it",
			ledger, config.GopiDirEnv, os.Getenv(config.GopiDirEnv), want)
	}
	if ledger == filepath.Join(os.Getenv(config.GopiDirEnv), config.MemoryDBFileName) {
		t.Fatalf("covCrLedgerEnv pinned the ledger at %q — that is <GOPI_HOME>/memory.db, the SIBLING shape "+
			"this guard exists to refuse", ledger)
	}
}
