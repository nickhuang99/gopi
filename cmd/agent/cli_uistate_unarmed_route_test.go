package main

// cli_uistate_unarmed_route_test.go — THE ROUTE OUT of an unarmed compare.
//
// WHY THIS FILE EXISTS. `gopi ui_state --compare` is the check an agent runs
// when it suspects its own surface painted something other than what it said.
// The check's most common outcome on a machine that has never armed a dump is
// exit 1 — and until this file's subject existed, the whole of that outcome was
//
//	gopi ui_state: no ui-screen.dump dump found in any scanned location
//
// MEASURED (2026-09-27, task 01a0e070-d783-72b0-adc7-c5d1cda6bf53): that line,
// exit 1, and nothing else. It names what is missing and says nothing about how
// to have one — which is exactly how the loop went unrun: an agent that reads
// only that sentence learns there is no screen, not that arming is a flag away.
//
// The three assertions below are, in order: the route IS named when the absence
// is the reason; it is NOT named when the refusal has a different remedy (a
// notice that fires on everything carries no information, the same argument
// TestUIStatePaintedSurfaceCarriesNoUnpaintedNotice makes for its notice); and
// the machine rendering carries it as a FIELD, so a JSON caller acts on the
// absence instead of scraping an English sentence.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/uiclient"
)

// unarmedRouteLine is the compare line these legs ask about. Ordinary ASCII, so
// a failure here is about the ROUTE and never about row fitting.
const unarmedRouteLine = "the unarmed refusal names the arming path"

// TestUIStateUnarmedCompareNamesTheRouteOut is the GREEN leg on a profile with
// no dump anywhere: the absence is still exit 1, and the output now names the
// flag and both verbs that write a dump.
func TestUIStateUnarmedCompareNamesTheRouteOut(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	// The profile has scanned clean: the sessions tree is empty and holds no
	// dump — the shape every machine has before its first armed run.

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, unarmedRouteLine,
		uiStateFlagSession, filepath.Join(gopiDir, "sessions", compareSessionID, compareSessionFileName),
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != 1 {
		t.Fatalf("an unarmed compare exited %d, want 1 (the check could not run)\n%s", code, out)
	}
	// The measured pre-fix line, still present: the absence is a fact of its own
	// and the route is added BESIDE it, never instead of it.
	if !strings.Contains(out, uiclient.DumpFileName) {
		t.Errorf("the refusal no longer names the artifact it looked for (%s):\n%s", uiclient.DumpFileName, out)
	}
	if !strings.Contains(out, uiclient.DumpNotArmedNotice) {
		t.Fatalf("an unarmed compare does not name the route out:\n%s", out)
	}
	// The route must be ACTIONABLE: a flag and the verb that carries it. Assert
	// the pieces rather than only the whole sentence, so a notice that keeps the
	// shape while dropping the flag fails here.
	for _, want := range []string{"--dump-screen", CliVerbChat, CliVerbUI} {
		if !strings.Contains(uiclient.DumpNotArmedNotice, want) {
			t.Errorf("the route out does not name %q: %s", want, uiclient.DumpNotArmedNotice)
		}
	}
}

// TestUIStateUnarmedRouteIsAbsentWhenTheRemedyIsAnother is the NEGATIVE control
// — the red leg's substance. With a dump on hand the refusal, when it happens at
// all, is about something else, and the arming instruction must NOT appear:
// otherwise "does the output carry the route?" would be true of every run and
// would measure nothing.
func TestUIStateUnarmedRouteIsAbsentWhenTheRemedyIsAnother(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	// An ARMED surface: the product's own sink, painted, live pid.
	writeUIStateDump(t, filepath.Join(gopiDir, "sessions", "armed"), os.Getpid(), liveStartStamp(t),
		"http://127.0.0.1:8421", "gopi> an armed surface paints this row")

	list, code := uiStateRun(t, []string{uiStateFlagList})
	if code != 0 {
		t.Fatalf("--list with a dump on hand exited %d\n%s", code, list)
	}
	if strings.Contains(list, uiclient.DumpNotArmedNotice) {
		t.Errorf("a listing that found a dump carries the arming instruction — a notice that fires on every run measures nothing:\n%s", list)
	}
}

// TestUIStateNoDumpForTheNamedSessionNamesTheRouteOut covers the SECOND shape of
// absence, and it is a different fact from "no dump anywhere": dumps exist, none
// belongs to the session the caller names. The remedy is still to arm the run
// whose surface is in question, so the same notice must print — and it must
// print it through the ONE predicate (uiclient.DumpAbsent), not by matching the
// refusal's prose (which master reworded once already).
func TestUIStateNoDumpForTheNamedSessionNamesTheRouteOut(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	// TWO armed surfaces on hand, so the single-candidate override cannot answer
	// for the session that was asked about (that override is a different,
	// deliberate behaviour and is not what this leg measures).
	for _, id := range []string{"aaa", "bbb"} {
		writeUIStateDump(t, filepath.Join(gopiDir, "sessions", id, compareDumpDirName), os.Getpid(),
			liveStartStamp(t), "http://127.0.0.1:8500", "gopi> surface "+id)
	}
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, unarmedRouteLine,
		uiStateFlagSession, filepath.Join(gopiDir, "sessions", "ccc", compareSessionFileName),
	})
	if code != 1 {
		t.Fatalf("a compare naming a session with no dump exited %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "belongs to session") {
		t.Errorf("the refusal does not state which fact it is (no dump belongs to the named session):\n%s", out)
	}
	if !strings.Contains(out, uiclient.DumpNotArmedNotice) {
		t.Errorf("the second absence shape does not carry the route out:\n%s", out)
	}
}

// TestUIStateUnarmedCompareCarriesTheRouteAsAField pins the machine rendering: a
// caller that never reads English must still be able to act on the absence.
func TestUIStateUnarmedCompareCarriesTheRouteAsAField(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)

	out, code := uiStateRun(t, []string{
		uiStateFlagJSON,
		uiStateFlagCompare, unarmedRouteLine,
		uiStateFlagSession, filepath.Join(gopiDir, "sessions", compareSessionID, compareSessionFileName),
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != 1 {
		t.Fatalf("--json on an unarmed compare exited %d, want 1\n%s", code, out)
	}
	var doc struct {
		Refusal  string `json:"refusal"`
		RouteOut string `json:"route_out"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--json is not JSON: %v\n%s", err, out)
	}
	if doc.RouteOut != uiclient.DumpNotArmedNotice {
		t.Errorf("JSON route_out = %q, want the arming route %q", doc.RouteOut, uiclient.DumpNotArmedNotice)
	}
	if !strings.Contains(doc.Refusal, uiclient.DumpFileName) {
		t.Errorf("JSON refusal = %q, want it to name the missing artifact", doc.Refusal)
	}
}
