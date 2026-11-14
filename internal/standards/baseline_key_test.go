package standardgate

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sitekey"
)

// ---------------------------------------------------------------------------
// The baseline key's RED PROOF.
//
// It pins the ONE property this keying exists for, and it is written against the
// GATE's own key function (hit.key, stamped by stamp) rather than against
// internal/sitekey directly — so reverting the gate to a `file:line` key turns
// this test RED while every other test in the package stays green. That is the
// point: without it, a revert would be invisible (the suite would pass, and the
// rotted-entry class would come back exactly as measured on 2026-10-16).
// ---------------------------------------------------------------------------

// probeFile parses src and returns the gate's hits for it, STAMPED the way a
// scan stamps them. It exists so the tests exercise the same key a real scan
// mints: an unstamped probe hit carries no digest, and a test that keyed it by
// hand would prove a property of the test, not of the gate.
func probeFile(t *testing.T, g gate, rel, src string) ([]hit, *parsed) {
	t.Helper()
	fset, f := parseSource(t, rel, src)
	p := &parsed{rel: rel, fset: fset, file: f, src: []byte(src)}
	hits := g.find(g, p)
	stamp(hits, map[string]*parsed{rel: p})
	return hits, p
}

// errorWrapProbe is the gate-1 site used by the key tests: a `%v` wrap, whose
// predicate is line-local (so the only thing an insertion above it can move is
// the LINE, which is exactly the variable under test).
const errorWrapProbe = `package p

import "fmt"

func f(err error) error {
	return fmt.Errorf("load config: %v", err)
}

func g() {}
`

// errorWrapProbeSiteLine is the line the wrap sits on in errorWrapProbe.
const errorWrapProbeSiteLine = 6

// probeLineShift is the line inserted above the site by the displacement tests —
// a comment, the most common edit that rots a line-keyed baseline.
const probeLineShift = "// a line inserted above the site\n"

// TestBaselineKeySurvivesAnEditAboveTheSite is the RED PROOF of the content key.
//
// A permit is minted for a site, a line is inserted ABOVE that site, and the
// permit must still name the site:
//
//   - the gate's own key function must return the SAME key after the shift;
//   - classify() must therefore still bucket the shifted hit as baselined;
//   - and the legacy `file:line` spelling the permit used to carry must NOT
//     match, which is asserted so this test cannot pass on a key that never
//     changed in the first place (a vacuous red proof is worse than none).
func TestBaselineKeySurvivesAnEditAboveTheSite(t *testing.T) {
	g := gates()[0] // Rule 16 (%w error wrapping) — a line-local predicate
	hits, _ := probeFile(t, g, "probe_test.go", errorWrapProbe)
	if len(hits) != 1 {
		t.Fatalf("the probe must report exactly one site, got %d: %+v", len(hits), hits)
	}
	if hits[0].line != errorWrapProbeSiteLine {
		t.Fatalf("the probe site moved: got line %d, want %d", hits[0].line, errorWrapProbeSiteLine)
	}
	permit := hits[0].key()
	if permit == "" {
		t.Fatal("the gate minted an EMPTY key for a site that has text — a permit that can never " +
			"match is a silent exemption (Rule 15)")
	}

	// Insert one line ABOVE the site. The site is now one line lower, and it is
	// the same statement.
	shifted := strings.Replace(errorWrapProbe, "func f(err error) error {\n",
		"func f(err error) error {\n\t"+probeLineShift, 1)
	if shifted == errorWrapProbe {
		t.Fatal("the probe fixture did not change — the insertion above the site failed")
	}
	shiftedHits, shiftedP := probeFile(t, g, "probe_test.go", shifted)
	if len(shiftedHits) != 1 {
		t.Fatalf("the shifted probe must still report exactly one site, got %d", len(shiftedHits))
	}
	if shiftedHits[0].line != errorWrapProbeSiteLine+1 {
		t.Fatalf("the probe site did not move down one line: %d", shiftedHits[0].line)
	}

	if got := shiftedHits[0].key(); got != permit {
		t.Fatalf("THE KEY ROTTED — an insert ABOVE the site changed it:\n  before: %q\n  after:  %q\n"+
			"a baseline keyed this way reports one defect twice (NEW at the site, STALE at the "+
			"permit) on every edit above it", permit, got)
	}

	// The permit the gate would have carried before this change: the legacy
	// `file:line` spelling. It MUST NOT name the shifted site — this is the
	// assertion that makes the test above non-vacuous.
	legacy := "probe_test.go:" + strconv.Itoa(errorWrapProbeSiteLine)
	if legacy == "probe_test.go:"+strconv.Itoa(shiftedHits[0].line) {
		t.Fatal("the legacy file:line key would also have survived — this test proves nothing")
	}
	if legacy == permit {
		t.Fatal("the gate's key IS the legacy file:line key — the keying was reverted, and every " +
			"edit above a baselined site will rot its entry again")
	}

	// End to end: the permit still buckets the SHIFTED hit as baselined.
	g.baseline = []string{permit}
	if cls := classify(g, shiftedHits[0], shiftedP); cls != classBaselined {
		t.Fatalf("a permit minted before an edit above its site no longer buckets the site: got %v, "+
			"want classBaselined — this is the STALE-plus-NEW pair the content key removes", cls)
	}
}

// TestBaselineKeyDistinguishesIdenticalLines pins the ordinal half of the key:
// two lines with the SAME normalized text in one file must get two DIFFERENT
// keys. Without it, one permit would silently cover both sites, which is a
// blanket exemption reached by accident.
func TestBaselineKeyDistinguishesIdenticalLines(t *testing.T) {
	src := `package p

func f() {
	os.Setenv("A", "1")
	os.Setenv("B", "2")
	os.Setenv("A", "1")
}
`
	// The 4th and 6th lines are byte-identical once whitespace is collapsed.
	first, err := sitekey.Key("probe_test.go", 4, []byte(src))
	if err != nil {
		t.Fatalf("keying the first site failed: %v", err)
	}
	second, err := sitekey.Key("probe_test.go", 6, []byte(src))
	if err != nil {
		t.Fatalf("keying the second site failed: %v", err)
	}
	if first == second {
		t.Fatalf("two identical lines share ONE key (%q) — a single permit would silently cover "+
			"both sites", first)
	}
	if sitekey.Ordinal(first) != sitekey.FirstOrdinal || sitekey.Ordinal(second) != 2 {
		t.Fatalf("ordinals must count occurrences of the text in file order: got %d and %d, want "+
			"%d and 2", sitekey.Ordinal(first), sitekey.Ordinal(second), sitekey.FirstOrdinal)
	}
	if sitekey.File(first) != "probe_test.go" {
		t.Fatalf("the path must stay in the key (it is what keeps one site's permit out of another "+
			"file): got %q", sitekey.File(first))
	}
}

// TestBaselineKeyStillRotsOnAContentEdit states the LIMIT of the key, so a
// future reader does not read the change as "baseline entries can no longer go
// stale". Editing the site's own line moves the key, and the permit then reports
// STALE — loudly, never silently.
func TestBaselineKeyStillRotsOnAContentEdit(t *testing.T) {
	g := gates()[0]
	hits, _ := probeFile(t, g, "probe_test.go", errorWrapProbe)
	permit := hits[0].key()
	edited := strings.Replace(errorWrapProbe, `"load config: %v"`, `"load config %s: %v"`, 1)
	if edited == errorWrapProbe {
		t.Fatal("the probe fixture did not change — the site edit failed")
	}
	editedHits, editedP := probeFile(t, g, "probe_test.go", edited)
	if len(editedHits) != 1 {
		t.Fatalf("the edited probe must still report exactly one site, got %d", len(editedHits))
	}
	if got := editedHits[0].key(); got == permit {
		t.Fatalf("editing the SITE's own line left the key unchanged (%q) — then a permit would "+
			"outlive the violation it names", got)
	}
	g.baseline = []string{permit}
	if cls := classify(g, editedHits[0], editedP); cls != classUnannotated {
		t.Fatalf("an edited site must be reported as a NEW violation, got %v — a silent pass here "+
			"would mean a permit outliving its site", cls)
	}
	// …and the permit itself must read as STALE (no hit carries it any more).
	seen := map[string]bool{editedHits[0].key(): true}
	if seen[permit] {
		t.Fatal("the OLD permit matched a live hit after the site was edited — the two halves of " +
			"the ratchet no longer agree")
	}
}

// TestBaselineKeyIsWellFormedAndLocatable pins the key's wire shape, because the
// generator, the gates and a human's reading all depend on it: three TAB-separated
// fields, a non-empty text, a positive ordinal.
func TestBaselineKeyIsWellFormedAndLocatable(t *testing.T) {
	src := []byte(errorWrapProbe)
	key, err := sitekey.Key("probe_test.go", errorWrapProbeSiteLine, src)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if !sitekey.WellFormed(key) {
		t.Fatalf("the key the gate mints is not well-formed: %q", key)
	}
	if got := strings.Count(key, sitekey.FieldSep); got != 2 {
		t.Fatalf("the key must carry exactly three fields, got %d separator(s): %q", got, key)
	}
	if line := sitekey.Locate(src, key); line != errorWrapProbeSiteLine {
		t.Fatalf("Locate must find the key's own text at line %d, got %d", errorWrapProbeSiteLine, line)
	}
	// Healthy refusals, both of which a gate must report rather than excuse.
	if _, err := sitekey.Key("probe_test.go", 999, src); err == nil {
		t.Fatal("a line outside the file must be refused")
	}
	if _, err := sitekey.Key("probe_test.go", 2, []byte("package p\n\nfunc f() {}\n")); err == nil {
		t.Fatal("a BLANK line must be refused — every blank line would otherwise share one key")
	}
}
