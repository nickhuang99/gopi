package main

// session_json_wire_test.go — THE WIRE KEY OF THE TURN COUNT IS THE BARE WORD
// "turns", AND THE PERSON-ONLY SUBSET RIDES ITS OWN KEY.
//
// THE RULING (the operator, 2026-10-27) that this file pins, because it reverses
// the shape this file used to defend: there is ONE turn count. An external
// notify is an input, a model-typed line is an input, a line the person typed is
// an input — all three open a turn, and `chats.turn_count` is how many turns a
// session ran. The person-only subset is a DETAIL of that number, never a rival
// reading of the word.
//
// SO THE WIRE SHAPE IS: live key `turns` (= chats.turn_count); the subset at
// `human_turns`; `repl_turns` reachable for one migration window only as a
// NESTED, ANNOUNCED, DATED alias (`deprecated_keys`), never beside the live key
// at the top level. The previous shape — the live count at `repl_turns`, the
// subset at `operator_turns` — was invented to keep two turn counts apart, and
// the ruling abolished the second count.
//
// WHAT THIS TEST PINS, and why each assertion is shaped the way it is:
//
//  1. The live key name comes from sessionevent.JSONKeyTurns — pinned by
//     DECLARING a probe field with the constant and comparing its tag to the
//     real row's tag through reflection. A struct tag cannot hold a constant,
//     so the tag literal and the constant are two spellings of one name; this
//     is where they are held together (the shape
//     internal/tools/tasknotify.go already uses for taskNotifyLedgerJSONKey).
//     Comparing against a hard-coded "turns" would pass on a tree where both had
//     drifted, which is the drift Rule 14 exists to stop.
//  2. The count rides THAT key — by MARSHALLING the real row and parsing it
//     back, not by reading the struct definition: a tag rename that no test
//     parsed would be invisible, and a hand-built map would not be the shipped
//     encoder's behaviour at all.
//  3. No top-level `repl_turns` carries it. The retired key is still readable
//     for the migration window, but only nested inside the announced
//     `deprecated_keys` block, where nothing can mistake it for the live field.
//  4. The two quantities stay DISTINGUISHABLE on the wire: `turns` and
//     `human_turns` are different numbers on a session where they differ, so a
//     reader that grabs the wrong one is not silently given the right one.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/tools"
)

// wireTurnFixture is the smallest detail that can show the collision: the
// recorded REPL-turn count and the measured operator-row count DIFFER, which is
// the only state in which reading the wrong key produces a wrong answer.
const (
	// wireTurnCount is what the BARE key carries: the log's OPENED count — the
	// axis `turn N` and every replay band number along.
	wireTurnCount = 200
	// wireCompletedTurns is the LEDGER's completed count. It differs on purpose:
	// a fixture where the two coincide cannot show whether a render reads the
	// right one (epic 2026-11-04 — the defect was exactly these two disagreeing).
	wireCompletedTurns = 197
	wireHumanTurns     = 66
)

func wireTurnFixture() tools.ChatSessionDetail {
	return tools.ChatSessionDetail{
		SessionID:      "80c07a7d-bb6b-48b4-be66-ea1867f403f2",
		Mode:           "chat",
		Model:          "deepseek-flash",
		TurnsOpened:    wireTurnCount,
		TurnCount:      wireCompletedTurns,
		Turns:          wireHumanTurns,
		TurnsKnown:     true,
		TokensReported: true,
		InputTokens:    100,
		OutputTokens:   10,
		CacheTokens:    50,
		StartedAt:      1792968720,
		EndedAt:        1792968800,
		FilePresent:    true,
	}
}

// jsonTagName returns a field's wire key: the tag up to its first comma.
func jsonTagName(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("json"), ",")[0]
}

// jsonFieldByTag finds the struct field whose wire key is key.
func jsonFieldByTag(t *testing.T, typ reflect.Type, key string) reflect.StructField {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		if jsonTagName(typ.Field(i)) == key {
			return typ.Field(i)
		}
	}
	t.Fatalf("%s carries no field with wire key %q", typ.Name(), key)
	return reflect.StructField{}
}

// TestSessionJSONTurnKeyIsTheConstant pins assertion 1: the live field's tag
// and sessionevent.JSONKeyTurns are ONE name — and that the name is the bare
// word the human table prints the same quantity under (Rule 14: one meaning per
// word, on both faces).
func TestSessionJSONTurnKeyIsTheConstant(t *testing.T) {
	// The tag is a LITERAL (a struct tag cannot hold a constant), so it is
	// checked against the CONSTANT rather than against a second literal: a test
	// that spelled "turns" itself would pass on a tree where the constant and the
	// tag had both drifted, which is the drift this pins.
	live := jsonFieldByTag(t, reflect.TypeOf(sessionJSONRow{}), sessionevent.JSONKeyTurns)
	if got, want := jsonTagName(live), sessionevent.JSONKeyTurns; got != want {
		t.Fatalf("sessionJSONRow.Turns wire key is %q, want the constant %q — "+
			"the tag literal and sessionevent.JSONKeyTurns have drifted (Rule 14)", got, want)
	}
	// The wire key must be the UNDERSCORE form of the word the human table
	// prints, or the two faces spell one quantity two ways.
	if want := strings.ReplaceAll(sessionevent.CounterUnitTurn, " ", "_"); sessionevent.JSONKeyTurns != want {
		t.Fatalf("wire key %q is not the underscore form of the printed unit %q (it prints %q)",
			sessionevent.JSONKeyTurns, sessionevent.CounterUnitTurn, want)
	}
	if sessionevent.JSONKeyTurns == sessionevent.DeprecatedJSONKeyReplTurns {
		t.Fatalf("the live key equals the deprecated key %q — the alias is not an alias",
			sessionevent.DeprecatedJSONKeyReplTurns)
	}
}

// TestSessionJSONRowCarriesTurnsAndHumanTurns pins assertions 2, 3 and 4 by
// MARSHALLING the shipped encoder's output.
func TestSessionJSONRowCarriesTurnsAndHumanTurns(t *testing.T) {
	encoded, err := jsonSessionRows([]tools.ChatSessionDetail{wireTurnFixture()})
	if err != nil {
		t.Fatalf("jsonSessionRows: %v", err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &rows); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	if len(rows) != 1 {
		t.Fatalf("encoded %d row(s), want 1: %s", len(rows), encoded)
	}
	row := rows[0]

	// 2. The TURN COUNT rides the constant's key — the bare word.
	raw, ok := row[sessionevent.JSONKeyTurns]
	if !ok {
		t.Fatalf("the row carries no %q key: %s", sessionevent.JSONKeyTurns, encoded)
	}
	var repl int
	if err := json.Unmarshal(raw, &repl); err != nil {
		t.Fatalf("%q is not a number (%s): %v", sessionevent.JSONKeyTurns, raw, err)
	}
	if repl != wireTurnCount {
		t.Fatalf("%q = %d, want the log's opened count %d (the axis `turn N` addresses)", sessionevent.JSONKeyTurns, repl, wireTurnCount)
	}
	// 2b. THE LEDGER'S COMPLETED COUNT KEEPS ITS OWN KEY, so a script that needed
	//     the old meaning of `turns` moves one key to the right instead of losing
	//     the quantity (epic 2026-11-04).
	compRaw, ok := row[sessionevent.JSONKeyTurnsCompleted]
	if !ok {
		t.Fatalf("the row carries no %q key, so the ledger's completed count is unreachable from the JSON face: %s",
			sessionevent.JSONKeyTurnsCompleted, encoded)
	}
	var comp int
	if err := json.Unmarshal(compRaw, &comp); err != nil {
		t.Fatalf("%q is not a number (%s): %v", sessionevent.JSONKeyTurnsCompleted, compRaw, err)
	}
	if comp != wireCompletedTurns {
		t.Fatalf("%q = %d, want the ledger's completed count %d",
			sessionevent.JSONKeyTurnsCompleted, comp, wireCompletedTurns)
	}
	if comp == repl {
		t.Fatalf("the fixture cannot show the defect: both turn keys read %d", repl)
	}

	// 3. No TOP-LEVEL repl_turns — the retired key lives only in the announced
	//    block. This is the assertion the epic's acceptance names: an external
	//    script already parsing `repl_turns` learns where to move.
	if bare, ok := row[sessionevent.DeprecatedJSONKeyReplTurns]; ok {
		t.Fatalf("the row still carries a TOP-LEVEL %q key (%s): the retirement is not complete — "+
			"the alias belongs in %q", sessionevent.DeprecatedJSONKeyReplTurns, bare, sessionevent.JSONKeyDeprecatedTurns)
	}

	// 3b. The migration surface exists, holds the SAME value, and states where
	//     to move and when it dies (Rule 15: a deprecation nobody receives is a
	//     silent removal with extra steps).
	depRaw, ok := row[sessionevent.JSONKeyDeprecatedTurns]
	if !ok {
		t.Fatalf("no %q block, so a script on the old key breaks with no notice: %s",
			sessionevent.JSONKeyDeprecatedTurns, encoded)
	}
	// The alias's own key is the DEPRECATED one (`repl_turns`) — a block that
	// nested the LIVE key would rename nothing and announce nothing.
	var dep struct {
		ReplTurns   *int   `json:"repl_turns"`
		ReplacedBy  string `json:"replaced_by"`
		RemoveAfter string `json:"remove_after"`
		Note        string `json:"note"`
	}
	if err := json.Unmarshal(depRaw, &dep); err != nil {
		t.Fatalf("decode %q: %v", sessionevent.JSONKeyDeprecatedTurns, err)
	}
	if dep.ReplTurns == nil || *dep.ReplTurns != wireTurnCount {
		t.Fatalf("%s.%s = %v, want the same count %d the live key carries",
			sessionevent.JSONKeyDeprecatedTurns, sessionevent.DeprecatedJSONKeyReplTurns, dep.ReplTurns, wireTurnCount)
	}
	// The alias and the completed key are DIFFERENT quantities: if they ever held
	// one value, a reader of the alias would silently get the ledger's number.
	if *dep.ReplTurns == wireCompletedTurns {
		t.Fatalf("the deprecated alias holds the LEDGER's completed count (%d) — it is an alias of the bare key, "+
			"not of %q", wireCompletedTurns, sessionevent.JSONKeyTurnsCompleted)
	}
	if dep.ReplacedBy != sessionevent.JSONKeyTurns {
		t.Fatalf("%s.replaced_by = %q, want %q", sessionevent.JSONKeyDeprecatedTurns, dep.ReplacedBy, sessionevent.JSONKeyTurns)
	}
	if dep.RemoveAfter != sessionevent.DeprecatedJSONKeyReplTurnsRemoveAfter {
		t.Fatalf("%s.remove_after = %q, want the stated deadline %q",
			sessionevent.JSONKeyDeprecatedTurns, dep.RemoveAfter, sessionevent.DeprecatedJSONKeyReplTurnsRemoveAfter)
	}
	if !strings.Contains(dep.Note, sessionevent.JSONKeyTurns) {
		t.Fatalf("%s.note does not name the replacement key %q: %q",
			sessionevent.JSONKeyDeprecatedTurns, sessionevent.JSONKeyTurns, dep.Note)
	}

	// 4. The two quantities stay distinct on the wire.
	subRaw, ok := row[sessionevent.JSONKeyHumanTurns]
	if !ok {
		t.Fatalf("no %q key, so the person-only subset is unreachable from the JSON face: %s",
			sessionevent.JSONKeyHumanTurns, encoded)
	}
	var sub int
	if err := json.Unmarshal(subRaw, &sub); err != nil {
		t.Fatalf("%q is not a number (%s): %v", sessionevent.JSONKeyHumanTurns, subRaw, err)
	}
	if sub != wireHumanTurns {
		t.Fatalf("%q = %d, want the measured human-turn count %d", sessionevent.JSONKeyHumanTurns, sub, wireHumanTurns)
	}
	if sub == repl {
		t.Fatalf("the fixture cannot show the defect: both keys read %d, so reading the wrong one is undetectable", repl)
	}
	// The SUBSET's key must not be the bare word either — if it were, a reader
	// would be back to two numbers under one name.
	if sessionevent.JSONKeyHumanTurns == sessionevent.JSONKeyTurns {
		t.Fatalf("the subset key %q equals the turn key — two quantities, one name",
			sessionevent.JSONKeyHumanTurns)
	}
}

// TestSessionJSONRowsCarryOneTopLevelTurnNumber pins the SHAPE choice that makes
// the wire unambiguous: exactly ONE top-level key carries a turn number, and the
// deprecated alias is NESTED, so a script reading top-level numbers can never
// pick the wrong one of two spellings.
func TestSessionJSONRowsCarryOneTopLevelTurnNumber(t *testing.T) {
	encoded, err := jsonSessionRows([]tools.ChatSessionDetail{wireTurnFixture()})
	if err != nil {
		t.Fatalf("jsonSessionRows: %v", err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &rows); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	if len(rows) != 1 {
		t.Fatalf("encoded %d row(s), want 1: %s", len(rows), encoded)
	}
	// The live key decodes as the turn count — the positive half.
	var live int
	if err := json.Unmarshal(rows[0][sessionevent.JSONKeyTurns], &live); err != nil || live != wireTurnCount {
		t.Fatalf("top-level %q = %d (err %v), want %d — the turn count must be readable there",
			sessionevent.JSONKeyTurns, live, err, wireTurnCount)
	}
	// ... and no SECOND top-level spelling of a turn number exists.
	for _, alias := range []string{sessionevent.DeprecatedJSONKeyReplTurns, "operator_turns"} {
		if raw, ok := rows[0][alias]; ok {
			t.Fatalf("a top-level %q still decodes (%s): one turn number, one name", alias, raw)
		}
	}
}
