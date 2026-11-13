package tools

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// The two spellings of Go's fmt diagnostics, as they land in RENDERED text.
// Named here (Rule 1) because they are compared against, not typed inline.
const (
	fmtMarkerExtraPrefix = "%!(EXTRA "
	fmtMarkerMissingTail = "(MISSING)"
)

// TestTaskRendersCarryNoFmtMarkers is the arity guard for the ledger's two row
// renderers (`taskListRowFormat`, `taskSearchRowFormat`) — the same guard the
// shot usage template has carried since cli_shot_surface_test.go.
//
// Why it exists (bug row 01a12e53-a1c9-7836-87af-2b36b0dbaac5, 2026-10-12): the
// row reported that a `gopi task …` line rendered
//
//	%!(EXTRA &errors.errorString=&{XML syntax error on line 1: unexpected EOF})
//
// inside the `(model …, source …, created …)` column. The marker itself is
// genuine Go fmt output, but no renderer on master produces it — the text lives
// in a ROW'S OWN SUMMARY (see devdoc/task-render-pct-extra-determination.md for
// the measured trail), i.e. it is ledger DATA echoed verbatim, and the
// renderer is correct because the summary arrives as an ARGUMENT (a '%' inside
// it is a byte, never a verb).
//
// This test holds BOTH halves of that statement, on the same fixture, because
// the two are one letter apart and only the pair distinguishes them:
//
//	① a CLEAN row must render through list / show / search with no fmt
//	   diagnostic anywhere — that is the template/argument arity contract, and
//	   it is what a template edit breaks (measured precedent, 2026-08-28:
//	   `merge --status` printed `%!s(MISSING)` into the operator's view);
//	② a row whose summary is ITSELF full of fmt markers and live verbs must
//	   echo that text VERBATIM — data is never a format string, so '%' in a
//	   summary must neither be interpreted nor shredded.
func TestTaskRendersCarryNoFmtMarkers(t *testing.T) {
	testutil.NewTestDBPath(t) // the ledger IS the fixture: a fresh per-test DB

	// ① the clean row: no '%' at all, so any marker in its render was minted
	// by the template.
	const cleanID = "t-clean-row"
	if _, err := TaskNew("an ordinary summary with no percent signs", cleanID, "", "glm-clean"); err != nil {
		t.Fatalf("TaskNew(clean): %v", err)
	}
	// ② the marker row: fmt markers AND live verbs, all of them DATA.
	// A renderer that treated this summary as a format string would emit
	// "%!s(MISSING)"/"%!d(MISSING)" for the verbs (and would reinterpret the
	// marker itself).
	const markerID = "t-marker-row"
	markerText := fmtMarkerExtraPrefix + "&errors.errorString=&{XML syntax error on line 1: unexpected EOF})"
	summary := "payload " + markerText + " verbs %s %d %q %% tail"
	if _, err := TaskNew(summary, markerID, "", "glm-marker"); err != nil {
		t.Fatalf("TaskNew(marker): %v", err)
	}

	renders := []struct {
		name      string
		got       func() (string, error)
		cleanRow  string
		markerRow string
	}{
		{"task list", func() (string, error) { return TaskList(taskStatusInProgress) }, cleanID, markerID},
		{"task show", func() (string, error) { return TaskShow(markerID) }, "", markerID},
		{"task search", func() (string, error) { return TaskSearch("payload") }, "", markerID},
	}
	for _, r := range renders {
		out, err := r.got()
		if err != nil {
			t.Fatalf("%s render: %v", r.name, err)
		}
		// The fixture row must be IN this render, or the assertions below pass
		// vacuously.
		if !strings.Contains(out, r.markerRow) {
			t.Fatalf("%s: the fixture row %s is missing from this render:\n%s", r.name, r.markerRow, out)
		}
		// ② the data survives verbatim — the marker is CONTENT here.
		if !strings.Contains(out, markerText) {
			t.Errorf("%s did not echo the summary verbatim — the marker in the "+
				"DATA must survive as data:\n%s", r.name, out)
		}
		// ②b and it is neither interpreted nor duplicated: exactly the one the
		// summary carries, no second one minted by the renderer, and no
		// missing-verb diagnostic for the '%s %d %q %%' tail.
		if got := strings.Count(out, fmtMarkerExtraPrefix); got != 1 {
			t.Errorf("%s renders %d marker(s), want exactly the 1 carried by the "+
				"summary:\n%s", r.name, got, out)
		}
		if strings.Contains(out, fmtMarkerMissingTail) {
			t.Errorf("%s emitted a (MISSING) diagnostic — the summary was used as "+
				"a format string, or the template has more verbs than arguments:\n%s", r.name, out)
		}
	}

	// ① the arity contract on a CLEAN row: with no '%' anywhere in the data,
	// any fmt diagnostic in the render belongs to the template.
	cleanList, err := TaskList(taskStatusInProgress)
	if err != nil {
		t.Fatalf("clean list render: %v", err)
	}
	if !strings.Contains(cleanList, cleanID) || !strings.Contains(cleanList, markerID) {
		t.Fatalf("both fixture rows must be in the in_progress view:\n%s", cleanList)
	}
	// The list line for the CLEAN row is the arity evidence: one line, no marker.
	for _, line := range strings.Split(cleanList, "\n") {
		if !strings.Contains(line, cleanID) {
			continue
		}
		if strings.Contains(line, "%!") {
			t.Errorf("the clean row's line carries a fmt diagnostic — the list "+
				"template and its argument list disagree:\n%s", line)
		}
	}
}
