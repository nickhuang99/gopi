package standardgate

// screen_assertion_gate_test.go — a test that asserts a RENDERED screen must
// synchronize on the screen it is about, never on a producer's counter.
//
// WHY THIS GATE EXISTS (measured, not theorized). cmd/agent's
// TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights used to wait like
// this:
//
//	h.waitDraws(t, h.in.Answer().StatusPublishes()+1)   // "the draw happened"
//	if got := h.screen.Line(l.EditorTop); !strings.Contains(got, typed) { …FAIL… }
//
// The wait returns when the editor's read loop has serviced the status
// publish's WAKE. The assertion needs the loop to have READ the scripted
// keystrokes the harness pushed onto the same channel. Those are independent
// events reported as ONE EventWake, so the wait can trip while four of the five
// keystrokes are still unread — and the assertion then reads a screen that is
// still being produced:
//
//   - measured on the pre-fix tip 2fdc65e4 with the loop parked at that wait
//     (instrumented t.Logf): the TRIP point read `read=2/5` with the editing row
//     "you> he", `read=4/5` with "you> hell", and `read=5/5` with the status row
//     still "iter 0" — three shapes, each a different amount of unfinished work;
//   - 2/20 one-shot runs FAIL (`for i in $(seq 1 20); do go test -count=1 …`),
//     and `-count=100` in ONE process FAILs twice — the shapes above.
//
// The fix (commit 65d7e278, merged c74ca08f) waits on the ASSERTION'S OWN
// SUBJECT instead (the editing row carries the typed text AND the status row
// carries the published counters) and moved the violated invariant into a
// DETERMINISTIC guard, TestChatPublishDoesNotOvertakeQueuedInput. That case is
// registered in testdata/flakes.tsv.
//
// WHAT IS REPORTED — the CALL SHAPE, nothing else. A test call site whose
// SELECTOR is one of the producer counters below, i.e. `… .StatusPublishes()`,
// `… .Repaints()` or `… .Writes()`. Only a SelectorExpr in CALL position is
// reported:
//
//   - the editor package's OWN tests call these counters as a DIRECT
//     MEASUREMENT — `band.Writes()` against a computed bound, `h.ed.Repaints()`
//     for the repaint budget — and those are properties of the code under test,
//     not a wait for a screen, so their call shape is identical to the one this
//     gate reports. They are permitted line-by-line in the baseline below, each
//     with the measured value it asserts, so the ratchet direction is visible:
//     a NEW producer-counter assertion fails here.
//   - the editor package's repaint/write BUDGET tests read the same counters
//     after a settled window; they are baselined too.
//   - a DOC comment that names a retired helper is not a call: the gate reads
//     the AST, so prose like "waitDraws was retired, see …" is untouched.
//
// The gate is a `_test.go` scan of prodRoots (internal/ + cmd/) — the empty
// permit list would make it silent, so it has a read floor: a scan that finds
// no test call sites at all fails rather than reading green.

import (
	"go/ast"
	"strings"
	"testing"
)

const (
	// screenAssertionCounters are the producer-side counters a screen assertion
	// must not synchronize on: they report what the PRODUCER did, never what the
	// assertion's subject shows. Each is spelled once, here.
	screenAssertionCounters = "StatusPublishes|Repaints|Writes"

	// screenAssertionMinTestCallSites is the read floor. MEASURED at the commit
	// this gate landed: five call sites in the test trees, all of them direct
	// measurements (internal/lineedit's repaint/write budgets — the baseline
	// below names each one). The floor sits one below that, so a legitimate
	// refactor that folds two budgets into one does not fail the gate, while a
	// traversal that stops seeing the tree does.
	screenAssertionMinTestCallSites = 4

	// screenAssertionEscape documents a producer-counter call site that is a real
	// measurement in a test which is NOT synchronizing on a screen (an editor
	// unit test asserting its own counter). It carries the reason on the same
	// line, like every other inline escape in this package.
	screenAssertionEscape = "//screen-ok:"
)

// screenAssertionBaseline are the producer-counter call sites that are direct
// MEASUREMENTS, not synchronization. Every entry is reviewed and reasoned; each
// names the assertion it serves, so a future reader can tell a measurement from
// a wait without re-deriving it.
var screenAssertionBaseline = []string{
	// internal/lineedit's own budget assertions. Each of these is a MEASUREMENT
	// of the package under test — a count compared against a computed bound —
	// never a wait for a screen another producer is drawing. The counter is
	// read AFTER the window it measures has closed (menu_test.go settles input
	// and quiets the write stream first), which is the property the retired
	// wait lacked. All five were read off the tree at the commit this gate
	// landed; `grep -rn --include=*_test.go -E '\.(StatusPublishes|Repaints|Writes)\(\)' internal cmd`
	// reproduces the list.
	//
	// A line-keyed permit is RE-KEYED only when the SITE is proven identical
	// (`git show <landing-commit>:<file> | grep -n`), never when a new call
	// shape appears above it; screenAssertionBaselineProvenance below carries
	// the re-keys this list has taken, so a moved number never reads as a
	// silent permission.
	"internal/lineedit/answer_test.go\tband.Writes(), len(lines)*3, len(lines), bound)\t#1", // the same reading, quoted in the failure message
	"internal/lineedit/answer_test.go\tif band.Writes() > bound {\t#1",                      // band.Writes() > bound — the answer-band write bound for one streamed block
	"internal/lineedit/answer_test.go\tif got := h.answer.Writes(); got > budget {\t#1",     // h.answer.Writes() > budget — the band-write bound grows with DELTAS, not with the answer
	"internal/lineedit/menu_test.go\tbeforeRepaints := h.ed.Repaints() //screen-ok: measures the repaint budget the test's own keys earned — a count read after the settled window, never a wait on a screen\t#1",                                          // beforeRepaints := h.ed.Repaints() — the budget window OPENS here (after the input has settled), and the site's own escape states what it measures
	"internal/lineedit/menu_test.go\tif got := h.ed.Repaints() - beforeRepaints; got > a7RepaintsPerKey*keys+a7RepaintsOverhead { //screen-ok: measures that redraw DECISIONS stay under the per-key budget (a storm bound), not that a draw finished\t#1", // …Repaints() - beforeRepaints > budget — the repaint storm bound, on DECISIONS not writes, and the site's own escape states what it measures
}

// screenAssertionBaselineProvenance records the HISTORY of the two
// internal/lineedit/menu_test.go permits: their SITE has been unchanged since the
// gate landed, and the numbers have moved twice. It exists so a permit that a
// refactor moved does not read as a silent permission — the SITES were verified
// identical, not merely similar, before the permits were re-pointed.
//
// The map's key is the SITE KEY (internal/sitekey — path, normalized line text,
// ordinal), the same key the list above carries, so this record survives a
// line-number shift for exactly the reason the permits do. The LINE NUMBERS below
// are kept as prose: they are the narrative of one measurement, dated and
// reproducible, not a key anything resolves (PROJECT.md Rule 38).
//
// Measured history — at the gate's landing commit 6620b59c (Sep 21) and at the
// base of the first re-key: `git show 6620b59c:internal/lineedit/menu_test.go |
// grep -n "Repaints()"` prints 607/625; the SAME two lines were 624/642 at that
// base, and the shift is exactly ONE commit's, a02e53d8 ("a one-candidate menu
// states why Up/Down cannot move"), which inserted 17 lines above the window;
// ff7d6e68 and every commit before it left both numbers untouched (measured the
// same way at ff7d6e68^, ff7d6e68, a02e53d8^). So 607→624 and 625→642 are the
// same two call sites, not new ones: the window opener
// (`beforeRepaints := h.ed.Repaints()`, a count read after waitForSettledInput has
// consumed every keystroke) and the repaint-storm bound
// (`h.ed.Repaints() - beforeRepaints`, on redraw DECISIONS not write chunks).
var screenAssertionBaselineProvenance = map[string]string{
	"internal/lineedit/menu_test.go\tbeforeRepaints := h.ed.Repaints() //screen-ok: measures the repaint budget the test's own keys earned — a count read after the settled window, never a wait on a screen\t#1":                                          "re-keyed from :607 by a02e53d8, which inserted 17 lines above the window — same `beforeRepaints := h.ed.Repaints()` window opener",
	"internal/lineedit/menu_test.go\tif got := h.ed.Repaints() - beforeRepaints; got > a7RepaintsPerKey*keys+a7RepaintsOverhead { //screen-ok: measures that redraw DECISIONS stay under the per-key budget (a storm bound), not that a draw finished\t#1": "re-keyed from :625 by the same a02e53d8 — same `h.ed.Repaints() - beforeRepaints > budget` repaint-storm bound, on redraw decisions never a wait for a draw",
}

// screenAssertionFind reports producer-counter CALLS in one parsed file. The
// scope is the call shape, because that is the mechanical half of "the
// synchronization was on the wrong subject": what it cannot see is which
// counter a WAIT polls and which it MEASURES — that distinction is the
// baseline's job, line by line.
func screenAssertionFind(g gate, p *parsed) []hit {
	var hits []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !screenAssertionCounterName(sel.Sel.Name) {
			return true
		}
		hits = append(hits, hit{rel: p.rel, line: p.fset.Position(call.Pos()).Line,
			desc: sel.Sel.Name + "() is a PRODUCER counter — a test that waits on it asserts a screen no " +
				"producer has promised to finish; wait on the assertion's own subject (the rendered row), " +
				"or (for a direct measurement) mark the line " + screenAssertionEscape + " <what it measures>"})
		return true
	})
	return hits
}

// screenAssertionCounterName reports whether a selector name is one of the
// producer counters (matched exactly: `Writes` must not catch `WriteString`).
func screenAssertionCounterName(name string) bool {
	switch name {
	case "StatusPublishes", "Repaints", "Writes":
		return true
	}
	return false
}

// TestScreenAssertionDoesNotSyncOnProducerCounters is the exhaustive half: a
// producer-counter call in ANY test file under internal/ + cmd/ is reported
// unless it is baselined or carries the inline escape.
func TestScreenAssertionDoesNotSyncOnProducerCounters(t *testing.T) {
	root := repoRoot(t)
	files := goFilesUnder(t, root, prodRoots, true)
	total := 0
	for _, rel := range files {
		p := parseRepoFile(t, root, rel)
		ast.Inspect(p.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && screenAssertionCounterName(sel.Sel.Name) {
				total++
			}
			return true
		})
	}
	if total < screenAssertionMinTestCallSites {
		t.Fatalf("the scan found %d producer-counter call site(s) under %v (want at least %d) — a "+
			"count this low is a broken traversal reading green, not a tree without assertions",
			total, prodRoots, screenAssertionMinTestCallSites)
	}

	g := gate{
		id:           "plugin/screen-assertion-sync",
		checklistRow: "4.3 (assert the subject, not a proxy)",
		probe:        "TestScreenAssertionCountersRedProof",
		wantTest:     true,
		scanRoots:    prodRoots,
		escape:       screenAssertionEscape,
		baseline:     screenAssertionBaseline,
		find:         screenAssertionFind,
	}
	hits, parsedFiles := scan(t, g)
	enforce(t, g, hits, parsedFiles)
}

// TestScreenAssertionCountersRedProof proves the predicate reports, on a
// synthetic source: the retired wait's exact call shape must be a hit, the
// bare-identifier and prose forms must not be, and a selector that merely
// CONTAINS a counter name must not match.
func TestScreenAssertionCountersRedProof(t *testing.T) {
	src := `package p

type band struct{}

func (b *band) StatusPublishes() int { return 0 }
func (b *band) Repaints() int        { return 0 }
func (b *band) Writes() int          { return 0 }

// waitDraws was retired: see the comment that names StatusPublishes() in prose.
func TestX(t *testing.T) {
	var h struct{ band band }
	h.band.StatusPublishes()
	h.band.Repaints()
	h.band.Writes()
	waitDraws(t, h.band.StatusPublishes()+1)
}
`
	fset, file := parseSource(t, "probe_test.go", src)
	g := gate{id: "plugin/screen-assertion-sync", find: screenAssertionFind}
	hits := screenAssertionFind(g, &parsed{rel: "probe_test.go", fset: fset, file: file})
	if len(hits) != 4 {
		t.Fatalf("the predicate reported %d hit(s), want 4 (three bare calls + the retired "+
			"wait's argument form): %+v", len(hits), hits)
	}
	if hits[3].line != 15 {
		t.Errorf("the retired wait's call shape was reported at line %d, want 15 — that is the "+
			"shape this gate exists for", hits[3].line)
	}
	for _, h := range hits {
		if !strings.Contains(h.desc, "PRODUCER counter") {
			t.Errorf("hit %q does not explain WHY the shape is a defect — the message is the "+
				"only thing a future reader gets", h.desc)
		}
	}
}

// TestScreenAssertionCounterNameIsExact pins the name matching against the two
// near-misses a substring match would swallow: a longer selector name that
// CONTAINS a counter, and an unrelated identifier.
func TestScreenAssertionCounterNameIsExact(t *testing.T) {
	for _, name := range []string{"StatusPublishes", "Repaints", "Writes"} {
		if !screenAssertionCounterName(name) {
			t.Errorf("%s is a producer counter and must be reported", name)
		}
	}
	for _, name := range []string{"WritesSoFar", "writeWrites", "StatusWrite", "Repaint"} {
		if screenAssertionCounterName(name) {
			t.Errorf("%s is not one of the producer counters — a substring match here would "+
				"report measurements that have nothing to do with screen synchronization", name)
		}
	}
}
