package main

// session_list_turns_provenance_test.go — ONE SESSION ID, TWO ROWS, AND THE TURN
// SLOT MUST SAY WHICH HALF EACH NUMBER CAME FROM.
//
// THE DEFECT (epic, PA ruling 2026-11-04). `gopi session list` answers ONE
// session id with TWO rows: kind=chat (a `chats` row, whose turn count exists)
// and kind=run (the run log the sessions tree yields, which has NO turn count).
// Both rows published the value under the SAME bare key — the chat row its
// measured count, the run row a 0 that meant "no chats column stands behind
// this" — so a reader could not tell which row the number belonged to, and a
// `--json` consumer summing the column counted a not-measured 0 as a turn count.
//
// THE RULING KEEPS BOTH ROWS. They are different facts (a conversation, and the
// run that produced it), and collapsing them into one row would delete one of
// them. So this file asserts the OPPOSITE of a merge, on both faces:
//
//	(i)   `--json`: the chat row's `turns` is still the count, and it says so in
//	      `turns_provenance`; the RUN row carries NO `turns` key at all (not a
//	      0) and its provenance word names the absence. Summing `turns` across
//	      the listing therefore stops counting a row with no number.
//	(ii)  the human table: the run row prints `turns none (no chats row)` in the
//	      slot where the chat row prints `turns N` — the distinction is visible
//	      to a reader, in the same place, without a legend.
//
// THE RED PROOF is the pre-fix binary: at f47acf2 the run row shipped
// `"turns": 0` and the table printed no turn slot at all for it, so (i) and (ii)
// both fail while the chat assertions stay green — exactly the shape of the
// defect ("the number works; the provenance is missing"). Reverting
// sessionJSONStripTurns' call in sessionJSONChatRow, or SessionSourceRunTurnsClause
// from the row format, turns each arm red again.
//
// THE FIXTURE IS DUAL ON PURPOSE: one id, one chats row, one run log written in
// that id's own directory — the shape the operator's live profile actually
// holds (measured 2026-11-04: `session list --all --json --limit 200` returned
// 200 rows over 22 session ids that carry BOTH kinds). A fixture with only one
// species cannot tell the fixed render from the broken one.

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/tools"
)

// sessionTurnsProvenanceID is the DUAL session's id: the chats row's session_id
// and the run log's sessionId, so the merged listing must answer it twice.
const sessionTurnsProvenanceID = "eeeeeeee-1111-4222-8333-444444444444"

// sessionTurnsProvenanceTurns is the ledger's count for that chats row — a
// positive number, so "the chat row still carries a real count" cannot pass on a
// zero that the run row would also have carried before the fix.
const sessionTurnsProvenanceTurns = 3

// seedDualSessionFixture writes ONE session id as BOTH species: a chats row with
// a recorded turn count, and a run log (a run directory named by the same id,
// whose session_start names it). It returns nothing — the assertions reach the
// rows through the real listing.
//
// THE LOG IS BUILT BY writeTurnsFixtureSession, NOT by writeCLISessionJSONL: the
// bare word `turns` is the LOG's opened count (epic 2026-11-04, measured on
// session 3a93ac1c: `session list` said 29 while the replay's last band said
// `turn 30`), so a fixture with a `session_start` line and nothing else would
// make the chat row's own count 0 and the test could not tell "the chat row kept
// its number" from "both rows are zero".
func seedDualSessionFixture(t *testing.T) {
	t.Helper()
	sessionChatEnv(t)
	path := writeTurnsFixtureSession(t, sessionTurnsProvenanceID, sessionTurnsProvenanceTurns)
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: sessionTurnsProvenanceID, Mode: tools.ChatModeCLI, Model: "deepseek-flash",
		FirstMsg: "the person's first ask", Workdir: "/tmp/proj", LogPath: path,
	}); err != nil {
		t.Fatalf("seed the chats row: %v", err)
	}
	// The RUN half: the writer is this process, so the liveness verdict is a
	// measurement rather than a coin flip. Same id, its own run directory — the
	// dual shape the operator's live profile holds (22 of 200 rows, 2026-11-04).
	writeRunFixture(t, sessionTurnsProvenanceID, os.Getpid(), true)
}

// dualListingRows decodes the JSON face into RAW maps, not into the struct: the
// defect is a KEY'S PRESENCE, and a struct with a pointer field can only tell
// the test what the test already told it. Raw maps ask the artefact itself.
func dualListingRows(t *testing.T) []map[string]json.RawMessage {
	t.Helper()
	out := captureStdoutAndStderr(t, func() int {
		return runSessionList([]string{sessionJSONFlag, sessionAllFlag})
	})
	start := strings.Index(out, "[")
	if start < 0 {
		t.Fatalf("the JSON face printed no array:\n%s", out)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out[start:]), &rows); err != nil {
		t.Fatalf("the JSON face is not one parseable document (%v):\n%s", err, out)
	}
	return rows
}

// rowOfKind picks the one row for the fixture id carrying the given kind — the
// selection the whole epic is about (by KIND, never by id alone: one id is two
// rows and taking "the row with this id" is what made the numbers ambiguous).
func rowOfKind(t *testing.T, rows []map[string]json.RawMessage, kind string) map[string]json.RawMessage {
	t.Helper()
	var found []map[string]json.RawMessage
	for _, r := range rows {
		var id, k string
		if err := json.Unmarshal(r["session_id"], &id); err != nil {
			t.Fatalf("a row carries no decodable session_id: %v", err)
		}
		if err := json.Unmarshal(r["kind"], &k); err != nil {
			t.Fatalf("a row carries no decodable kind: %v", err)
		}
		if id == sessionTurnsProvenanceID && k == kind {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the listing answered %d row(s) for %s/%s, want exactly 1 — the dual session "+
			"(a chats row AND a run log under one id) is the subject of this test",
			len(found), sessionTurnsProvenanceID, kind)
	}
	return found[0]
}

// TestSessionListTurnsAreLabeledPerHalf is the epic's repro, both faces.
func TestSessionListTurnsAreLabeledPerHalf(t *testing.T) {
	seedDualSessionFixture(t)
	rows := dualListingRows(t)

	chatRow := rowOfKind(t, rows, tools.SessionKindChat)
	runRow := rowOfKind(t, rows, tools.SessionKindRun)

	// (i-a) THE CHAT ROW KEEPS THE COUNT, under the same bare key it always had —
	// the ruling forbids collapsing the two rows, and it equally forbids taking
	// the number away from the half that has one.
	raw, ok := chatRow[sessionevent.JSONKeyTurns]
	if !ok {
		t.Fatalf("the chat row carries no %q key — the ledger's number must not be taken away from "+
			"the half that has one: %v", sessionevent.JSONKeyTurns, chatRow)
	}
	var chatTurns int
	if err := json.Unmarshal(raw, &chatTurns); err != nil {
		t.Fatalf("%q is not a number on the chat row (%s): %v", sessionevent.JSONKeyTurns, raw, err)
	}
	if chatTurns != sessionTurnsProvenanceTurns {
		t.Fatalf("the chat row's %q = %d, want the %d turn(s) the ledger recorded",
			sessionevent.JSONKeyTurns, chatTurns, sessionTurnsProvenanceTurns)
	}
	var chatProv string
	if err := json.Unmarshal(chatRow[sessionevent.JSONKeyTurnsProvenance], &chatProv); err != nil {
		t.Fatalf("the chat row carries no decodable %q (%v): %v",
			sessionevent.JSONKeyTurnsProvenance, chatRow[sessionevent.JSONKeyTurnsProvenance], err)
	}
	if chatProv != sessionevent.TurnsProvenanceChat {
		t.Fatalf("the chat row's provenance = %q, want %q", chatProv, sessionevent.TurnsProvenanceChat)
	}

	// (i-b) THE DEFECT, ASSERTED DIRECTLY: the run row must NOT publish its
	// absence under the chat row's bare key. At f47acf2 this key was present with
	// the value 0 — the ambiguity the PA ruling names.
	if raw, ok := runRow[sessionevent.JSONKeyTurns]; ok {
		t.Fatalf("THE DEFECT: the run row still publishes %q (%s) — one session id's two rows then carry "+
			"ONE bare key with two meanings, and a consumer summing it counts a row that has no number. "+
			"The run row states its provenance in %q instead: %v",
			sessionevent.JSONKeyTurns, raw, sessionevent.JSONKeyTurnsProvenance, runRow)
	}
	// The same for the ledger's key and the deprecated alias: all three are the
	// chat accounting, and all three were shipped as 0 on a run log.
	for _, key := range []string{sessionevent.JSONKeyTurnsCompleted, sessionevent.JSONKeyDeprecatedTurns} {
		if _, ok := runRow[key]; ok {
			t.Fatalf("the run row still publishes %q — it is the same chat accounting under another name, "+
				"and it has the same defect on this row: %v", key, runRow)
		}
	}
	var runProv string
	if err := json.Unmarshal(runRow[sessionevent.JSONKeyTurnsProvenance], &runProv); err != nil {
		t.Fatalf("the run row carries no decodable %q, so its silence about turns is a gap a consumer "+
			"cannot see (%v): %v", sessionevent.JSONKeyTurnsProvenance,
			runRow[sessionevent.JSONKeyTurnsProvenance], err)
	}
	if runProv != sessionevent.TurnsProvenanceRun {
		t.Fatalf("the run row's provenance = %q, want %q", runProv, sessionevent.TurnsProvenanceRun)
	}
	// The two halves must be DISTINGUISHABLE BY THE PROVENANCE WORD alone — if
	// one id's two rows carried one provenance value there would be nothing for a
	// consumer to branch on, which is the state the fix exists to leave.
	if chatProv == runProv {
		t.Fatalf("both rows of ONE session carry provenance %q — the label does not name a half", chatProv)
	}

	// (ii) THE HUMAN TABLE, same distinction in the same place.
	table := captureStdoutAndStderr(t, func() int { return runSessionList(nil) })
	var chatLine, runLine string
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, sessionTurnsProvenanceID) {
			continue
		}
		if strings.Contains(line, "["+tools.SessionKindChat+"]") {
			chatLine = line
		}
		if strings.Contains(line, "["+tools.SessionKindRun+"]") {
			runLine = line
		}
	}
	if chatLine == "" || runLine == "" {
		t.Fatalf("the table does not show the dual session as BOTH rows (chat=%q run=%q):\n%s",
			chatLine, runLine, table)
	}
	if !strings.Contains(chatLine, fmt.Sprintf("%s %d", sessionevent.CounterUnitTurn, chatTurns)) {
		t.Errorf("the chat row does not print its count under %q:\n%s", sessionevent.CounterUnitTurn, chatLine)
	}
	if !strings.Contains(runLine, tools.SessionSourceRunTurnsClause) {
		t.Errorf("THE DEFECT: the run row's line does not state that it has no turn count — a reader "+
			"seeing two rows for one id is left to guess which one the number belongs to:\n  %s\n"+
			"want the clause %q", runLine, tools.SessionSourceRunTurnsClause)
	}
	// AND THE RUN ROW MUST NOT CLAIM A NUMBER: "turns 0" reads as a measurement
	// ("this run opened no turns") when the truth is that no chats column stands
	// behind it.
	if strings.Contains(runLine, strings.Join([]string{sessionevent.CounterUnitTurn, "0"}, " ")) {
		t.Errorf("the run row prints a zero turn count, which reads as a measurement:\n%s", runLine)
	}
}

// TestSessionShowJSONAgreesWithTheRunRow pins the OTHER face onto the same
// decision: `session show --json <run id>` resolves the same run log, and a
// script must not get `turns: 0` from it after learning from the listing that the
// run half publishes no such key (Rule 13: one absence, one spelling).
func TestSessionShowJSONAgreesWithTheRunRow(t *testing.T) {
	seedDualSessionFixture(t)

	out := captureStdoutAndStderr(t, func() int {
		return runSessionShow([]string{sessionTurnsProvenanceID, sessionJSONFlag})
	})
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("the show face printed no object:\n%s", out)
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out[start:]), &row); err != nil {
		t.Fatalf("the show face is not one parseable document (%v):\n%s", err, out)
	}
	var kind string
	if err := json.Unmarshal(row["kind"], &kind); err != nil {
		t.Fatalf("the show face carries no decodable kind: %v", err)
	}
	// The resolver answers the CHAT row when one exists (that is the session the
	// id names); the run half of the same id is reached from the listing. So this
	// arm asserts the CHAT page still publishes its count WITH the provenance
	// word — the page must not go silent about turns just because its sibling
	// half did.
	if kind != tools.SessionKindChat {
		t.Skipf("this id resolved to kind=%q, so the chat-page arm does not apply here", kind)
	}
	if _, ok := row[sessionevent.JSONKeyTurns]; !ok {
		t.Fatalf("the chat show page dropped %q: %v", sessionevent.JSONKeyTurns, row)
	}
	var prov string
	if err := json.Unmarshal(row[sessionevent.JSONKeyTurnsProvenance], &prov); err != nil {
		t.Fatalf("the chat show page carries no decodable %q: %v",
			sessionevent.JSONKeyTurnsProvenance, row[sessionevent.JSONKeyTurnsProvenance])
	}
	if prov != sessionevent.TurnsProvenanceChat {
		t.Fatalf("the chat show page's provenance = %q, want %q", prov, sessionevent.TurnsProvenanceChat)
	}
}

// TestSessionJSONTurnsProvenanceKeyIsTheConstant pins the new key's tag to its
// constant, for the reason session_json_wire_test.go pins `turns`: a struct tag
// cannot hold a constant, so the literal is the one place the two could drift —
// and this key is what a consumer BRANCHES on, so a drift here silently changes
// which rows a script believes carry a turn count (Rule 14).
func TestSessionJSONTurnsProvenanceKeyIsTheConstant(t *testing.T) {
	live := jsonFieldByTag(t, reflect.TypeOf(sessionJSONRow{}), sessionevent.JSONKeyTurnsProvenance)
	if got, want := jsonTagName(live), sessionevent.JSONKeyTurnsProvenance; got != want {
		t.Fatalf("sessionJSONRow.TurnsProvenance wire key is %q, want the constant %q — the tag literal and "+
			"sessionevent.JSONKeyTurnsProvenance have drifted (Rule 14)", got, want)
	}
	// Its two VALUES must differ, or the key carries nothing a consumer can act
	// on: a reader comparing `turns_provenance` to one value has to be able to
	// tell the two halves apart.
	if sessionevent.TurnsProvenanceChat == sessionevent.TurnsProvenanceRun {
		t.Fatalf("both provenance values are %q — the key does not name a half",
			sessionevent.TurnsProvenanceChat)
	}
	// And neither value may repeat the word it qualifies: a marker that says
	// "turns" again is the ambiguity it exists to remove.
	for _, v := range []string{sessionevent.TurnsProvenanceChat, sessionevent.TurnsProvenanceRun} {
		if strings.Contains(v, sessionevent.CounterUnitTurn) {
			t.Errorf("provenance value %q contains the unit word %q — it must name the PRODUCER, not the quantity",
				v, sessionevent.CounterUnitTurn)
		}
	}
}
