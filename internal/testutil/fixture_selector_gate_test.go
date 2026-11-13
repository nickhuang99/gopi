package testutil

// fixture_selector_gate_test.go — the regression gate for the shape that makes a
// suite UNFINISHABLE: a fixture that re-executes its own test binary with an
// EMPTY `-test.run` value.
//
// MEASURED (a minimal sample binary built in a scratch tree — the task report
// carries the exact invocations and their output):
//
//	$ ./probe.test -test.run=     -test.v  → --- PASS: TestA, --- PASS: TestB
//	$ ./probe.test -test.run ""   -test.v  → --- PASS: TestA, --- PASS: TestB
//	$ ./probe.test -test.run=^$   -test.v  → PASS (no test ran)
//
// An empty selector does not mean "no test": it means **the whole suite**. A
// fixture whose child re-enters the suite then starts the next child, which
// re-enters again — the autophagy chain that never returns (measured on a task
// branch: 26 nested probe processes, an outer `go test` at etime 14:02 that
// unblocked the instant a `kill -9` landed on the chain).
//
// TWO ARMS, deliberately layered so the hard one never depends on the soft one:
//
//	A. RAW TEXT — the argv of a `-test.run` element is read from the line
//	   itself; an EMPTY value FAILS outright, with no resolution involved.
//	B. RESOLUTION — every other self-spawn of the test binary must have its
//	   selector resolved to a literal pattern: a bare literal, a package
//	   constant, a concatenation of both, a helper function's own argv, or a
//	   running test's own name. A site whose selector cannot be resolved is
//	   reported unless it is recorded in fixtureSelectorUnresolvedExempt with
//	   the mechanism that keeps it non-empty.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// fixtureSelectorRunFlag is the ONE flag whose value decides which tests a
	// re-executed test binary runs (Rule 1/13: named once, referenced below).
	fixtureSelectorRunFlag = "-test.run"
	// fixtureSelectorNoTestPattern is the spelling for "run no test", read from
	// its single definition site rather than mirrored (Rule 13/14).
	fixtureSelectorNoTestPattern = fixtureNoTestPattern
	// fixtureSelectorGateFileName is THIS file, excluded from BOTH arms: the red
	// proofs below necessarily contain the defect's literal spelling as DATA (a
	// string handed to the classifier), and reporting the gate's own test data
	// as a defect would make the gate unfixable. The exclusion is narrow (one
	// file name) and COMPENSATED: the synthetic-tree case below proves on a real
	// directory that the same scanner still finds the empty selector, and the
	// arm-B log lists every spawn site in the tree, so nothing is invisible.
	fixtureSelectorGateFileName = "fixture_selector_gate_test.go"
	// fixtureSelectorMinSites is the floor: a scan that finds fewer test-binary
	// spawn sites than this has stopped reading the repository, and its green
	// verdict would be about an empty set (Rule 15 — a scan that found nothing
	// must be distinguishable from a scan that never ran).
	fixtureSelectorMinSites = 25
	// fixtureSelectorLookaheadLines bounds how far past the spawn line the argv
	// text is collected: an argv element may sit on its own line.
	fixtureSelectorLookaheadLines = 4
	// fixtureSelectorMaxResolveDepth bounds constant/function following, so a
	// cycle (`a = b`, `b = a`) terminates instead of hanging the gate.
	fixtureSelectorMaxResolveDepth = 4
	// fixtureSelectorBlankByte replaces each blanked byte of a comment — ONE
	// blank byte for ONE source byte, never more. The contract it serves is that
	// the comment-stripped view addresses the ORIGINAL file's byte offsets
	// exactly, so a line number counted on one view is the reader's line on the
	// other. It was a TWO-byte string until 2026-10-30, which broke that contract
	// in the growth direction: every blanked comment byte added a byte, the view
	// outgrew the source, and the offset of a defect below a comment-dense header
	// then indexed the ORIGINAL text past its end (`slice bounds out of range
	// [:9988] with length 9857`). The regression arm is
	// TestFixtureSelectorBlankPreservesEveryBytePosition.
	fixtureSelectorBlankByte = ' '
)

// fixtureSelectorSite is one test-binary spawn, with the verdict about the
// selector it hands its child.
type fixtureSelectorSite struct {
	file string // repo-relative
	line int
	fn   string // the enclosing function
	// selector is the resolved pattern. "" while spells is true and unresolved
	// is false is THE defect arm B reports.
	selector string
	// spells is true when the site's argv carries a -test.run element at all.
	spells bool
	// resolved is true when selector came from a literal (or a name the scan
	// could follow).
	resolved bool
	// empty is the autophagy shape: a -test.run element whose value is empty.
	empty bool
	// envDispatched is true when the child this site starts is selected by the
	// child's own TestMain (an env marker), so no selector is needed.
	envDispatched bool
}

var (
	// fixtureSelectorEmptyInlineRe matches the -test.run element whose value is
	// written inline and empty — `"-test.run="`, the spelling that cost a
	// >14-minute hang. It is matched on RAW TEXT and WITHOUT anchoring to the
	// end of a line, because the defect appears as an argv ELEMENT: the original
	// spelling carried the element with a VALUE-ELEMENT after it (a comma-separated
	// no-such-test name), which is why end-of-line anchoring is not enough. The only way a test source contains this two-token sequence is a
	// selector with an empty value (or a string handed to a classifier like this
	// one, which is why this file is excluded from the scan by name).
	fixtureSelectorEmptyInlineRe = regexp.MustCompile(`["` + "`" + `]-test\.run=["` + "`" + `]`)
	// fixtureSelectorBareFlagRe matches the flag as its OWN argv element —
	// `"-test.run", "<value>"` — so the next element can be inspected.
	fixtureSelectorBareFlagRe = regexp.MustCompile(`["` + "`" + `]-test\.run["` + "`" + `]`)
	// fixtureSelectorEmptyElementRe matches an argv element that is exactly the
	// empty string literal.
	fixtureSelectorEmptyElementRe = regexp.MustCompile(`^["` + "`" + `]["` + "`" + `]$`)
	// fixtureSelectorConstDeclRe finds a package-level assignment whose
	// right-hand side the resolver can follow (`name = "…"`, `const name = "…"`).
	fixtureSelectorConstDeclRe = regexp.MustCompile(`(?m)^[ \t]*(?:const[ \t]+)?(\w+)[ \t]*=[ \t]*([^\n]+)$`)
	// fixtureSelectorFuncDeclRe finds a top-level function body.
	fixtureSelectorFuncBodyRe = regexp.MustCompile(`(?ms)^func[ \t]+(\w+)\([^)]*\)[^{]*\{.*?^\}`)
	// fixtureSelectorStringLitRe extracts a Go string literal — interpreted
	// (quoted) or raw (backtick), both of which appear in real fixtures.
	fixtureSelectorStringLitRe = regexp.MustCompile("`([^`]*)`|\"([^\"\\\\]*)\"")
	// fixtureSelectorIdentRe extracts an identifier.
	fixtureSelectorIdentRe = regexp.MustCompile(`\b([A-Za-z_]\w*)\b`)
	// fixtureSelectorTestMainRe is the dispatch seam a selector-less child
	// relies on.
	fixtureSelectorTestMainRe = regexp.MustCompile(`(?m)^func[ \t]+TestMain\(`)
	// fixtureSelectorRuntimeNameRe matches an expression built from a running
	// test's own name — non-empty by construction, so it can never be the empty
	// selector.
	fixtureSelectorRuntimeNameRe = regexp.MustCompile(`\.Name\(\)`)
	// fixtureSelectorGoWords are the identifiers a resolver must NOT follow as
	// package constants (language words and the argv builders themselves).
	fixtureSelectorGoWords = map[string]bool{
		"append": true, "os": true, "exec": true, "testing": true, "t": true,
		"const": true, "func": true, "return": true, "true": true, "false": true,
		"Command": true, "Args": true, "string": true, "nil": true,
	}
)

// fixtureSelectorUnresolvedExempt records the sites arm B cannot resolve, each
// with the mechanism that makes its selector non-empty. This table is the
// RATCHET: it is data, reviewed in the file that reads it, and a new entry is a
// deliberate act (an exemption a reader cannot see is an exemption nobody
// reviews).
var fixtureSelectorUnresolvedExempt = map[string]string{
	"cmd/agent/cli_checkpoint_test.go|testHelperCommand": "the selector is the function PARAMETER `name`: every call site in the file passes a literal, non-empty test name (TestResolveRawLog_MissingExits, …) and no call site passes an empty value — read the four call sites to confirm",
}

// TestFixtureSelectorNeverRunsTheWholeSuite is the gate.
func TestFixtureSelectorNeverRunsTheWholeSuite(t *testing.T) {
	root := fixtureRepoRoot(t)

	// ARM A — raw text, no resolution.
	hits := fixtureEmptySelectorHits(t, root)
	for _, hit := range hits {
		t.Errorf("%s:%d carries an EMPTY %s value — that spelling runs the WHOLE suite in the child, so a fixture that re-enters the suite starts the next child forever (measured: 26 nested probes, an outer go test that only unblocked on kill -9). Spell the no-test selector as %q (its own definition: fixtureSelectorNoTestPattern) or name ONE test",
			hit.file, hit.line, fixtureSelectorRunFlag, fixtureSelectorNoTestPattern)
	}

	// ARM B — resolution.
	sites := fixtureSelectorSites(t, root)
	if len(sites) < fixtureSelectorMinSites {
		t.Fatalf("the scan resolved only %d test-binary spawn sites (floor %d) — the repository was not read, so this gate's verdict would be about an empty set", len(sites), fixtureSelectorMinSites)
	}
	spelling := 0
	var unresolved []fixtureSelectorSite
	for _, s := range sites {
		if s.spells {
			spelling++
		}
		t.Logf("selector site %s:%d %s — selector=%q spells=%v resolved=%v empty=%v envDispatched=%v",
			s.file, s.line, s.fn, s.selector, s.spells, s.resolved, s.empty, s.envDispatched)
		if s.empty {
			continue // arm A already reported it, with the sharper message
		}
		if !s.spells {
			// No selector at all: the child selects its body from the
			// environment in its own TestMain, which is the one alternative to
			// a selector — and the site is logged above so a reader sees it.
			continue
		}
		if s.resolved || s.envDispatched {
			continue
		}
		unresolved = append(unresolved, s)
	}
	if spelling == 0 {
		t.Fatalf("no spawn site in %d carries a %s element — the element scanner is broken, not the repository", len(sites), fixtureSelectorRunFlag)
	}
	sort.Slice(unresolved, func(i, j int) bool {
		if unresolved[i].file == unresolved[j].file {
			return unresolved[i].line < unresolved[j].line
		}
		return unresolved[i].file < unresolved[j].file
	})
	for _, s := range unresolved {
		if reason, ok := fixtureSelectorUnresolvedExempt[s.file+"|"+s.fn]; ok {
			t.Logf("unresolved (recorded): %s:%d %s — %s", s.file, s.line, s.fn, reason)
			continue
		}
		t.Errorf("%s:%d %s starts a child from this test binary with a %s selector the scan cannot resolve to a literal — spell the selector as a literal (or a package constant/helper argv the scan can follow), or record the site in fixtureSelectorUnresolvedExempt with the mechanism that keeps it non-empty",
			s.file, s.line, s.fn, fixtureSelectorRunFlag)
	}
	t.Logf("selector gate: %d spawn sites, %d carrying a %s element, %d unresolved (recorded exemptions: fixtureSelectorUnresolvedExempt)",
		len(sites), spelling, fixtureSelectorRunFlag, len(unresolved))
}

// TestFixtureSelectorGateFindsTheEmptySelector is the RED PROOF for arm A: the
// classifier must call the empty selector a defect, and must NOT call the safe
// spellings defects. Without it, a matcher that matched nothing would pass the
// gate for the wrong reason.
func TestFixtureSelectorGateFindsTheEmptySelector(t *testing.T) {
	// NOTE the data below is deliberately the ARGV ELEMENT alone, without the
	// call that would carry it: the sibling spawn inventory scans this package
	// for the spawn literal, and a defect's spelling sitting in a test's DATA
	// would be counted there as a spawn site of this file. The classifier reads
	// the element, so nothing is lost by leaving the call out.
	for name, text := range map[string]string{
		"inline empty value":     `"-test.run=", "TestThisNameSelectsNoTestInTheProbe"`,
		"bare flag, empty value": `"-test.run", ""`,
		`bare flag, backtick`:    "`-test.run=`,\n\t\"TestNoSuch\",\n",
	} {
		if len(fixtureSelectorEmptyOffsets(text)) == 0 {
			t.Errorf("%s must be reported as the empty selector (it runs the whole suite in the child)", name)
		}
	}
	for name, text := range map[string]string{
		"the no-test pattern":       `"-test.run=^$"`,
		"the no-test bare flag":     `"-test.run", "^$"`,
		"a named test":              `"-test.run=^TestHelperHolder$"`,
		"a named test, bare flag":   `"-test.run", "^TestHelperHolder$"`,
		"another flag":              `"-test.timeout=30s"`,
		"no selector at all":        ``,
		"a verbose named test":      `"-test.run=^"+refusalProbeTest+"$", "-test.v"`,
		"a concatenation continues": `"-test.run="+name`,
	} {
		if hits := fixtureSelectorEmptyOffsets(text); len(hits) != 0 {
			t.Errorf("%s must NOT be reported as the empty selector (reported at offsets %v)", name, hits)
		}
	}
}

// TestFixtureSelectorGateJudgesCodeNotComments is the MINIMAL REPRO for the
// false-positive class measured on master b314ed620, where arm A reported PROSE:
// the comment in cmd/agent/chat_session_slots_test.go that documents the
// autophagy defect (quoting `-test.run=` and a no-such-test name while
// explaining why the fixture no longer spawns a probe) came back as two empty
// argv elements, so `go test -count 1 ./internal/testutil` was RED on a tree
// that contains no such defect at all.
//
// BOTH DIRECTIONS SIT IN ONE TABLE, because either alone passes for the wrong
// reason: a scanner that never fires at all would "prove" comments are ignored,
// and one that fires on every occurrence would "prove" code is judged. So the
// same spelling appears once inside a comment and once as the argv ELEMENTS of a
// spawn, and the verdict must be 0 and 1 respectively. The last row is the
// FALSE-NEGATIVE FLOOR: a literal in CODE is still a defect, which is what keeps
// the comment exclusion from becoming a machine that reports nothing.
func TestFixtureSelectorGateJudgesCodeNotComments(t *testing.T) {
	for name, tc := range map[string]struct {
		text    string
		wantHit int
	}{
		"the spelling in a line comment is not a defect": {
			text: "// This used to re-exec the test binary with `-test.run=`\n// and a name the probe does not have.\n",
		},
		"the same spelling as argv elements is the defect": {
			text:    "\t\"-test.run=\", \"TestThisNameSelectsNoTestInTheProbe\"\n",
			wantHit: 1,
		},
		"the spelling in a block comment is not a defect": {
			text: "/**\n * \"-test.run=\", \"TestNoSuch\"\n */\n",
		},
		"a trailing comment does not hide the code before it": {
			text: "flags := []string{\"-test.run=^$\"} // never \"-test.run=\"\n",
		},
		"a comment marker inside a literal opens no comment": {
			text:    "sep := \"//\"\n\t\"-test.run=\", \"TestNoSuch\"\n",
			wantHit: 1,
		},
		"a literal in CODE is still judged (the false-negative floor)": {
			text:    "probeArg := \"-test.run=\"\n",
			wantHit: 1,
		},
		// The DECLARATION exclusion, and its floor beside it. A name bound to the
		// flag's prefix holds no selector (measured: internal/tools's own
		// selector-resolution guard documents the spelling this way, and the gate
		// reported the guard's constant). The second row is what keeps the
		// exclusion from swallowing the defect: the SAME text one line below, as
		// an argv element, is still reported.
		"a named binding of the flag prefix is not a selector": {
			text: "\tprefix = \"-test.run=\"\n",
		},
		"an argv element below such a binding is still the defect": {
			text:    "\tprefix = \"-test.run=\"\n\treturn []string{\"-test.run=\"}\n",
			wantHit: 1,
		},
	} {
		hits := fixtureEmptySelectorHitsInText("synthetic_test.go", tc.text)
		if len(hits) != tc.wantHit {
			t.Errorf("%s: %d empty-selector hits, want %d (hits: %v)", name, len(hits), tc.wantHit, hits)
		}
	}

	// The same repro ONE LEVEL UP, on a real tree: documenting the defect in a
	// file must not cost a hit, and the hit that is reported must still carry the
	// CODE file's name and its ORIGINAL line number — the blanking is
	// position-preserving, so a comment above the defect cannot shift it.
	dir := TestDeviceScratchDir(t, "selector-comment-gate")
	doc := "package fixture\n\n// The old fixture re-exec'd the binary with `-test.run=`\n// and a name the probe does not have — the autophagy chain.\nfunc documented() {}\n"
	code := "package fixture\n\nfunc childArgv() []string {\n\treturn []string{\"-test.run=\", \"TestThisNameSelectsNoTestInTheProbe\"}\n}\n"
	for file, src := range map[string]string{"doc_test.go": doc, "code_test.go": code} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	found := fixtureEmptySelectorHitsInDir(t, dir)
	if len(found) != 1 {
		t.Fatalf("the tree carries ONE empty selector (code_test.go) and one documented spelling (doc_test.go), got %d hits: %v", len(found), found)
	}
	if filepath.Base(found[0].file) != "code_test.go" || found[0].line != 4 {
		t.Errorf("the hit must name code_test.go:4 (the argv line), got %s:%d", found[0].file, found[0].line)
	}
}

// TestFixtureSelectorBlankPreservesEveryBytePosition is the REGRESSION ARM for a
// property the scanner above ADVERTISES and used to break: "the blanking
// preserves every byte position". The blanked view must be exactly as long as
// its source — ONE blank byte for ONE blanked source byte — because the offsets
// it computes are used to index the ORIGINAL text.
//
// It did not hold. The blank constant was a TWO-byte string, so every blanked
// comment byte ADDED a byte, the view outgrew its source, and on a comment-dense
// file the offset of an occurrence below the comments reached past the end of
// the original text. Measured on this tree before the fix:
//
//	go test ./internal/testutil/ -run TestFixtureSelectorNeverRunsTheWholeSuite
//	panic: runtime error: slice bounds out of range [:9988] with length 9857
//
// The fixture below is comment-dense ON PURPOSE (a long doc header — the shape
// every file in this repository carries) and keeps the only occurrence on the
// LAST line, the worst case for a growing view: there the computed offset is the
// file's own length PLUS every blanked byte.
func TestFixtureSelectorBlankPreservesEveryBytePosition(t *testing.T) {
	// One blanked byte in must be one blanked byte out, for every comment shape
	// the scanner distinguishes. Small shapes first, so a failure names the shape.
	for name, src := range map[string]string{
		"a line comment":                    "x := 1 // note\n",
		"a line comment that is bare":       "//\n",
		"two comment markers on one line":   "// a // b\n",
		"a block comment":                   "/* note */ x := 1\n",
		"a multi-line block comment":        "/**\n * a\n * b\n */\nx := 1\n",
		"a trailing block comment":          "x := 1 /* note */\n",
		"a comment marker inside a literal": "s := \"// not a comment\"\n",
		"an escape inside a literal":        "s := \"a\\\"//b\"\n",
		"a raw string with a marker":        "s := `// not a comment`\n",
	} {
		view := fixtureSelectorWithoutComments(src)
		if len(view) != len(src) {
			t.Errorf("%s: the blanked view is %d bytes, source is %d — blanking must preserve every byte position",
				name, len(view), len(src))
		}
	}

	// Now the measured case: a header long enough that the old constant's growth
	// carries the occurrence's offset past the end of the source.
	const headerLines = 140
	var src strings.Builder
	src.WriteString("package fixture\n\n")
	for i := 0; i < headerLines; i++ {
		src.WriteString("// a documented line of the header, long enough to matter\n")
	}
	src.WriteString("\nfunc childArgv() []string {\n\treturn []string{\"-test.run=\", \"TestThisNameSelectsNoTestInTheProbe\"}\n}\n")
	text := src.String()

	view := fixtureSelectorWithoutComments(text)
	if len(view) != len(text) {
		t.Fatalf("the blanked view is %d bytes, source is %d over a %d-line comment header: blanking must preserve every byte position",
			len(view), len(text), headerLines)
	}

	// The occurrence is the ONLY one, and it must be reported at its ORIGINAL
	// line — the line a reader opens the file at. The expected line is derived by
	// splitting the source (a different route than the scanner's byte counting),
	// so the arm cannot agree with the bug by construction.
	wantLine := 0
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "return []string{") {
			wantLine = i + 1
			break
		}
	}
	if wantLine == 0 {
		t.Fatal("the fixture lost its occurrence line")
	}
	hits := fixtureEmptySelectorHitsInText("dense_test.go", text)
	if len(hits) != 1 {
		t.Fatalf("the fixture carries ONE empty selector, got %d: %v", len(hits), hits)
	}
	if hits[0].line != wantLine {
		t.Errorf("the hit is reported at line %d, the occurrence is at line %d — a comment above it must not shift it", hits[0].line, wantLine)
	}
	if filepath.Base(hits[0].file) != "dense_test.go" {
		t.Errorf("the hit names %q, want dense_test.go", hits[0].file)
	}
}

// TestFixtureSelectorScanFindsTheEmptySelectorInAFixture is the SECOND red
// proof, one level up: a synthetic SOURCE TREE (Rule 22: device-backed scratch,
// off the "/" partition) carrying an empty selector must be reported by the
// same scanner the gate runs, and the same tree with a safe selector must come
// back clean. This makes the gate falsifiable on a real tree, not only on
// in-memory strings.
func TestFixtureSelectorScanFindsTheEmptySelectorInAFixture(t *testing.T) {
	for name, tc := range map[string]struct {
		selector string
		wantHit  bool
	}{
		"the empty selector is found":  {selector: `"-test.run="`, wantHit: true},
		"the no-test pattern is clean": {selector: `"-test.run=^$"`, wantHit: false},
		"a named test is clean":        {selector: `"-test.run=^TestHelperHolder$"`, wantHit: false},
	} {
		dir := TestDeviceScratchDir(t, "selector-gate")
		src := "package fixture\n\nfunc startHolder() []string {\n\treturn []string{" + tc.selector + "}\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(src), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		found := fixtureEmptySelectorHitsInDir(t, dir)
		if tc.wantHit && len(found) == 0 {
			t.Errorf("%s: the scanner found no empty selector in a tree that has one", name)
		}
		if !tc.wantHit && len(found) != 0 {
			t.Errorf("%s: the scanner reported %v, but this spelling is safe", name, found)
		}
	}
}

// TestFixtureSelectorResolutionTellsALiteralFromAnUnknown is the RED PROOF for
// arm B: a selector the resolver can follow must resolve (and be non-empty), and
// an expression it cannot follow must be reported as unresolved rather than
// silently counted as safe.
func TestFixtureSelectorResolutionTellsALiteralFromAnUnknown(t *testing.T) {
	pkg := fixtureSelectorPackage{exprs: map[string]string{
		"holderArg":    `"-test.run=^TestHelperHolder$"`,
		"noTestArg":    `"-test.run=" + ` + "`" + `^$` + "`" + ``,
		"emptyArg":     `"-test.run="`,
		"helperSuffix": `"^TestHelperHolder$"`,
	}, funcs: map[string]string{
		"childFlags": "func childFlags(name string) []string {\n\treturn []string{\"-test.run=^\" + name + \"$\"}\n}",
	}}
	for name, tc := range map[string]struct {
		element  string
		value    string
		resolved bool
	}{
		"a package constant resolves":       {element: "holderArg", value: "-test.run=^TestHelperHolder$", resolved: true},
		"a concatenated constant resolves":  {element: "noTestArg", value: "-test.run=^$", resolved: true},
		"a literal resolves":                {element: `"-test.run=^TestHelperHolder$"`, value: "-test.run=^TestHelperHolder$", resolved: true},
		"a helper function's argv resolves": {element: "childFlags(t.Name())", value: "<runtime test name>", resolved: true},
		"a parameter does not resolve":      {element: `"-test.run=" + name`, resolved: false},
		"an unknown name does not resolve":  {element: "someUnknownBuilder(x)", resolved: false},
	} {
		value, _, resolved := fixtureSelectorResolveValue(tc.element, pkg, 0)
		if resolved != tc.resolved {
			t.Errorf("%s: resolved=%v, want %v (value %q)", name, resolved, tc.resolved, value)
			continue
		}
		if tc.resolved && value != tc.value {
			t.Errorf("%s: value %q, want %q", name, value, tc.value)
		}
	}
}

// --- arm A: the raw scanner ------------------------------------------------

// fixtureSelectorHit is one empty-selector occurrence.
type fixtureSelectorHit struct {
	file string
	line int
}

// fixtureEmptySelectorHits scans the three source roots for ARM A.
func fixtureEmptySelectorHits(t *testing.T, root string) []fixtureSelectorHit {
	t.Helper()
	var out []fixtureSelectorHit
	for _, sub := range []string{fixtureInventorySrcName, fixtureInventorySrcNameCmd, fixtureInventorySiblingRoot} {
		out = append(out, fixtureEmptySelectorHitsInDir(t, filepath.Join(root, sub))...)
	}
	return out
}

// fixtureEmptySelectorHitsInDir collects ARM A's hits under one directory. It
// reads RAW TEXT on purpose: arm A must be independent of every resolution rule
// below it. Hits are located by BYTE OFFSET and deduplicated, so one occurrence
// is reported once — a line-numbered window over a multi-line call would
// otherwise report the same element once per line it spans.
func fixtureEmptySelectorHitsInDir(t *testing.T, dir string) []fixtureSelectorHit {
	t.Helper()
	var out []fixtureSelectorHit
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //stylegate:ignore the min-sites floor in the gate above is the loud path
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == fixtureSelectorGateFileName {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //stylegate:ignore same as above
		}
		out = append(out, fixtureEmptySelectorHitsInText(path, string(raw))...)
		return nil
	})
	return out
}

// fixtureEmptySelectorHitsInText reports the empty-selector occurrences in ONE
// file's text, with the line number of each. It is the SINGLE classifier behind
// both the tree walk above and the acceptance tests below, so the verdict the
// gate ships and the verdict its red proofs assert cannot drift apart.
//
// The offsets are computed on the COMMENT-BLANKED view and the line numbers on
// the ORIGINAL text: the blanking preserves every byte position (see
// fixtureSelectorCodeOnly), so the two views address the same place.
func fixtureEmptySelectorHitsInText(path, text string) []fixtureSelectorHit {
	var out []fixtureSelectorHit
	for _, offset := range fixtureSelectorEmptyOffsets(fixtureSelectorWithoutComments(text)) {
		out = append(out, fixtureSelectorHit{file: path, line: strings.Count(text[:offset], "\n") + 1})
	}
	return out
}

// fixtureSelectorWithoutComments returns the file's text with the text of every
// COMMENT blanked to spaces — and nothing else touched.
//
// WHY THIS EXISTS (measured on master b314ed620): arm A scanned RAW text, so the
// comment in `cmd/agent/chat_session_slots_test.go` that DOCUMENTS the very
// autophagy defect — the prose quoting `-test.run=` next to a no-such-test name
// while explaining why the fixture no longer spawns a probe — was reported as two
// empty-selector argv elements. Prose spawns nothing, so a defect arm A reports
// inside a comment is a false positive that makes the gate unfixable: one can
// either keep the documentation or keep the gate green, never both.
//
// WHY ONLY COMMENTS, AND NEVER STRING LITERALS. An argv element IS a string
// literal — the defect this gate exists for is spelled
// `"-test.run=", "TestThisNameSelectsNoTestInTheProbe"`, where both elements are
// literals. Blanking literals would therefore leave arm A with nothing to see and
// turn the gate into a false-negative machine, so the exclusion stops at comments.
// A literal carrying the spelling is DATA: it is exactly what arm A must judge,
// and a fixture passing it to a classifier rather than to exec is arm B's and the
// sibling spawn inventory's business, not this scan's.
//
// The blanking PRESERVES EVERY BYTE POSITION — a newline stays a newline, every
// other blanked byte becomes one space — so the byte offsets this scan reports
// still address the ORIGINAL file and a reported line number is the reader's line.
//
// Like the scanners it is modelled on (cmd/agent/chat_panel_rows_test.go
// stripGoComments, internal/ledgerwindow blankComments — reused, not re-invented,
// Rule 8) it is a SCANNER, not a parser: quotes, runes and backticks are honoured
// so a comment marker inside a literal is not read as a comment, and an escape
// inside a literal is skipped so a `\"` cannot end it early.
func fixtureSelectorWithoutComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	const (
		plain = iota
		inLineComment
		inBlockComment
		inString
		inRawString
		inRune
	)
	state := plain
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case plain:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = inLineComment
				out.WriteByte(fixtureSelectorBlankByte)
				out.WriteByte(fixtureSelectorBlankByte)
				i++
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = inBlockComment
				out.WriteByte(fixtureSelectorBlankByte)
				out.WriteByte(fixtureSelectorBlankByte)
				i++
			case c == '"':
				state = inString
				out.WriteByte(c)
			case c == '`':
				state = inRawString
				out.WriteByte(c)
			case c == '\'':
				state = inRune
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		case inLineComment:
			if c == '\n' {
				state = plain
				out.WriteByte('\n')
				continue
			}
			out.WriteByte(fixtureSelectorBlankByte)
		case inBlockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				state = plain
				out.WriteByte(fixtureSelectorBlankByte)
				out.WriteByte(fixtureSelectorBlankByte)
				i++
				continue
			}
			if c == '\n' {
				out.WriteByte('\n')
				continue
			}
			out.WriteByte(fixtureSelectorBlankByte)
		case inString, inRune, inRawString:
			out.WriteByte(c)
			if state == inRawString {
				if c == '`' {
					state = plain
				}
				continue
			}
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
				continue
			}
			if (state == inString && c == '"') || (state == inRune && c == '\'') {
				state = plain
			}
		}
	}
	return out.String()
}

// fixtureSelectorEmptyOffsets returns the byte offset of every -test.run element

// whose value is EMPTY, in one file's text.
//
// The inline shape is judged by what FOLLOWS the element's closing quote: a `+`
// means the value is a concatenation that continues (`"-test.run=" + name`),
// which is NOT empty — arm B resolves those; anything else (a comma, a closing
// paren, end of line) ends the element, so the empty value stands.
func fixtureSelectorEmptyOffsets(text string) []int {
	var out []int
	seen := map[int]bool{}
	record := func(offset int) {
		if !seen[offset] {
			seen[offset] = true
			out = append(out, offset)
		}
	}
	for _, loc := range fixtureSelectorEmptyInlineRe.FindAllStringSubmatchIndex(text, -1) {
		// The element's END (not its start): the declaration context of the
		// value is what decides, and for a quoted literal those differ.
		if fixtureSelectorConcatenationContinues(text[loc[1]:]) {
			continue
		}
		if fixtureSelectorDeclaresTheFlagValue(text, loc[0]) {
			// A package-level DECLARATION of the flag's prefix — `prefix =
			// "-test.run="` — names the flag so the code that builds an argv can
			// refer to it. It hands no selector to any child: the value is
			// completed elsewhere. Measured on this tree (2026-10-31,
			// internal/tools/holder_fixture_children_test.go:108): the same text
			// was reported as an empty selector, which made the gate and the
			// per-package guard that DOCUMENTS the spelling mutually exclusive —
			// the guard's own constant was the hit.
			continue
		}
		record(loc[0])
	}
	for _, loc := range fixtureSelectorBareFlagRe.FindAllStringIndex(text, -1) {
		rest := text[loc[1]:]
		if !strings.HasPrefix(strings.TrimLeft(rest, " \t\n"), ",") {
			continue // the flag is not followed by another element
		}
		elements := fixtureSelectorElements(rest[strings.Index(rest, ",")+1:])
		if len(elements) == 0 || fixtureSelectorEmptyElementRe.MatchString(elements[0]) {
			record(loc[0])
		}
	}
	sort.Ints(out)
	return out
}

// fixtureSelectorDeclaresTheFlagValue reports whether the literal beginning at
// offset is the RIGHT-HAND SIDE of a named binding — `prefix = "-test.run="` —
// rather than an argv element handed to a child.
//
// The distinction is the whole difference between documenting the flag and
// passing it: a NAME bound to the prefix holds no selector, it names the flag so
// the code that builds an argv can refer to it (the value is completed
// elsewhere, usually by concatenation). Measured on this tree (2026-10-31,
// internal/tools/holder_fixture_children_test.go:108): a package's own
// `testRunSelectorPrefix = "-test.run="` — the constant its selector-RESOLUTION
// guard documents the spelling with — was reported as an empty selector, so the
// gate and the guard that keeps selectors honest could not both exist. Arm A's
// subject is an EMPTY VALUE handed to a child; a binding is not one.
//
// The judgement is made on the line the literal sits on and NOWHERE else: the
// text before it must be exactly `<name> =` (optionally `const`/`var`) and the
// text after it must be the end of the line (or a `//` comment). That keeps the
// shapes that really do hand a selector over firmly in scope, because they carry
// a comma, a closing brace or a call — never a bare end of line:
//
//	"-test.run=", "TestNoSuch"          → an argv element   (reported)
//	probeArg := "-test.run="            → `:=`, not a plain binding (reported)
//	return []string{"-test.run="}       → a closing brace (reported)
func fixtureSelectorDeclaresTheFlagValue(text string, offset int) bool {
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	head := text[lineStart:offset]
	if !fixtureSelectorBindingPrefixRe.MatchString(head) {
		return false
	}
	lineEnd := strings.IndexByte(text[offset:], '\n')
	tail := text[offset:]
	if lineEnd >= 0 {
		tail = text[offset : offset+lineEnd]
	}
	if lit := fixtureSelectorStringLitRe.FindStringIndex(tail); lit != nil {
		tail = tail[lit[1]:]
	}
	if comment := strings.Index(tail, "//"); comment >= 0 {
		tail = tail[:comment]
	}
	return strings.TrimSpace(tail) == ""
}

// fixtureSelectorBindingPrefixRe matches the left-hand side of a named binding
// up to (and including) its `=` — nothing else may appear before the literal.
var fixtureSelectorBindingPrefixRe = regexp.MustCompile(`^[ \t]*(?:const[ \t]+|var[ \t]+)?[A-Za-z_]\w*[ \t]*=[ \t]*$`)

// fixtureSelectorConcatenationContinues reports whether the text right after an
// element's closing quote is a `+` — i.e. the element is one term of a longer
// (therefore non-empty) value.
func fixtureSelectorConcatenationContinues(rest string) bool {
	return strings.HasPrefix(strings.TrimLeft(rest, " \t\n"), "+")
}

// fixtureSelectorElements splits an argv text at top-level commas and returns
// the trimmed elements, dropping the blanks a leading comma leaves behind.
func fixtureSelectorElements(text string) []string {
	var out []string
	for _, raw := range fixtureSelectorArgElements(text) {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// fixtureSelectorArgsText returns the ARGUMENT text of a call: everything after
// the first "(" — so the callee and the argv element are not one element.
func fixtureSelectorArgsText(text string) string {
	open := strings.Index(text, "(")
	if open < 0 {
		return text
	}
	return text[open+1:]
}

// fixtureSelectorArgElements splits an argv text at top-level commas, keeping
// string literals and nested call parentheses intact.
func fixtureSelectorArgElements(text string) []string {
	var out []string
	depth, start := 0, 0
	inString, inBacktick := false, false
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case inString:
			if c == '\\' {
				i++
			} else if c == '"' {
				inString = false
			}
		case inBacktick:
			if c == '`' {
				inBacktick = false
			}
		case c == '"':
			inString = true
		case c == '`':
			inBacktick = true
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				out = append(out, text[start:i])
				return out
			}
			depth--
		case c == ',' && depth == 0:
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	if s := strings.TrimSpace(text[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

// --- arm B: the resolution ratchet -----------------------------------------

// fixtureSelectorPackage is the per-directory knowledge arm B resolves against.
type fixtureSelectorPackage struct {
	exprs map[string]string // constant name → right-hand side
	funcs map[string]string // function name → body
}

// fixtureSelectorSites walks every test source and reports one entry per
// self-spawn of the test binary.
func fixtureSelectorSites(t *testing.T, root string) []fixtureSelectorSite {
	t.Helper()
	roots := []string{fixtureInventorySrcName, fixtureInventorySrcNameCmd, fixtureInventorySiblingRoot}
	index := map[string]fixtureSelectorPackage{}
	for _, sub := range roots {
		_ = filepath.WalkDir(filepath.Join(root, sub), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil //stylegate:ignore the min-sites floor in the gate above is the loud path
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil //stylegate:ignore same as above
			}
			dir := filepath.Dir(path)
			pkg, ok := index[dir]
			if !ok {
				pkg = fixtureSelectorPackage{exprs: map[string]string{}, funcs: map[string]string{}}
			}
			pkg.absorb(string(raw))
			index[dir] = pkg
			return nil
		})
	}

	var sites []fixtureSelectorSite
	for _, sub := range roots {
		_ = filepath.WalkDir(filepath.Join(root, sub), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, fixtureInventoryTestSuffix) {
				return nil //stylegate:ignore the min-sites floor in the gate above is the loud path
			}
			if filepath.Base(path) == fixtureSelectorGateFileName {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil //stylegate:ignore same as above
			}
			text := string(raw)
			if !fixtureSpawnSelfRe.MatchString(text) {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			pkg := index[filepath.Dir(path)]
			lines := strings.Split(text, "\n")
			dispatches := pkg.dispatchesFromEnv()
			for i, line := range lines {
				if !fixtureSpawnSelfRe.MatchString(line) {
					continue
				}
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					// A COMMENT that mentions the shape (the historical
					// self-spawn line, quoted in prose) is documentation, not a spawn.
					continue
				}
				fn, _ := fixtureEnclosingFunc(lines, i)
				site := fixtureSelectorSite{file: rel, line: i + 1, fn: fn}
				args := fixtureSelectorSpawnArgs(fixtureSelectorSpawnText(lines, i))
				value, spells, resolved := fixtureSelectorResolve(args, pkg, 0)
				site.spells, site.selector, site.resolved = spells, value, resolved
				site.empty = spells && resolved && value == ""
				site.envDispatched = dispatches
				sites = append(sites, site)
			}
			return nil
		})
	}
	return sites
}

// absorb indexes the resolvable names of one source file.
func (p fixtureSelectorPackage) absorb(text string) {
	for _, m := range fixtureSelectorConstDeclRe.FindAllStringSubmatch(text, -1) {
		rhs, name := strings.TrimSpace(m[2]), m[1]
		if rhs == "" || strings.Contains(rhs, ":=") || p.rhsIsFunctionHeader(rhs) {
			continue
		}
		if _, exists := p.exprs[name]; !exists {
			p.exprs[name] = rhs
		}
	}
	for _, m := range fixtureSelectorFuncBodyRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if _, exists := p.funcs[name]; !exists {
			p.funcs[name] = m[0]
		}
	}
}

// rhsIsFunctionHeader reports whether a matched assignment is really a function
// declaration's result list, which the constant regex would otherwise swallow.
func (p fixtureSelectorPackage) rhsIsFunctionHeader(rhs string) bool {
	return strings.Contains(rhs, ") {") || strings.Contains(rhs, "){")
}

// dispatchesFromEnv reports whether the package selects a fixture child's body
// from the environment in its own TestMain.
func (p fixtureSelectorPackage) dispatchesFromEnv() bool {
	body, ok := p.funcs[testMainFuncName]
	if !ok {
		return false
	}
	return fixtureSelectorTestMainRe.MatchString(body) && strings.Contains(body, "os.Getenv(")
}

// testMainFuncName is the dispatch seam's name (Rule 1: named, not inline).
const testMainFuncName = "TestMain"

// fixtureSelectorLitValue returns the value of a matched string-literal pair
// (group 1 is the raw-literal form, group 2 the interpreted one).
func fixtureSelectorLitValue(m []string) string {
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// fixtureSelectorLitAt returns the value and the span of the FIRST string
// literal at or after start.
func fixtureSelectorLitAt(text string, start int) (string, int, int, bool) {
	for _, loc := range fixtureSelectorStringLitRe.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] < start {
			continue
		}
		m := fixtureSelectorStringLitRe.FindStringSubmatch(text[loc[0]:loc[1]])
		return fixtureSelectorLitValue(m), loc[0], loc[1], true
	}
	return "", 0, 0, false
}

// fixtureSelectorFuncCallRe matches a helper CALL that builds the whole argv —
// `voiceCLIChildArgs(sub...)`, `subreaperChildFlags(t.Name())`.
var fixtureSelectorFuncCallRe = regexp.MustCompile(`^(\w+)\(`)

// fixtureSelectorResolve extracts the -test.run element from an argv text and
// resolves its value. It returns (value, spells, resolved).
//
// The passes are ordered so a NON-selector element cannot decide the verdict: a
// spawn's argv is `[<binary>, <selector>, …]`, and the binary element is very
// often an identifier (`exe`, `os.Args[0]`) the scan cannot resolve — reading it
// as "the selector" is what made this gate report resolvable sites as
// unresolved. So a resolvable selector is looked for FIRST, and only when no
// element yields one do the unresolvable candidates decide the verdict.
func fixtureSelectorResolve(args string, pkg fixtureSelectorPackage, depth int) (string, bool, bool) {
	// The argv BUILDER shape: the whole argument list is one helper call whose
	// selector lives in the helper's own body.
	if depth <= fixtureSelectorMaxResolveDepth {
		if m := fixtureSelectorFuncCallRe.FindStringSubmatch(strings.TrimSpace(args)); m != nil {
			if _, ok := pkg.funcs[m[1]]; ok {
				return fixtureSelectorResolveInFunc(m[1], pkg, depth+1)
			}
		}
	}
	elements := fixtureSelectorElements(args)
	candidate := false
	for _, raw := range elements {
		element := fixtureSelectorElementClean(raw)
		if element == "" {
			continue
		}
		if strings.Contains(element, fixtureSelectorRunFlag+"=") {
			// The element carries the flag INLINE — possibly as a concatenation
			// (`"-test.run=" + name`), so the value is the element with the flag
			// text removed and the literal quotes left in place: that yields a
			// Go expression (`"" + name`) the resolver already knows how to read.
			expr := strings.Replace(element, fixtureSelectorRunFlag+"=", "", 1)
			// The element may carry a composite literal's head (`[]string{"^$"`):
			// the value starts inside the braces.
			if open := strings.LastIndex(expr, "{"); open >= 0 {
				expr = expr[open+1:]
			}
			expr = strings.TrimRight(strings.TrimSpace(expr), "}")
			value, _, ok := fixtureSelectorResolveValue(expr, pkg, depth)
			return value, true, ok
		}
		if element == `"`+fixtureSelectorRunFlag+`"` || element == "`"+fixtureSelectorRunFlag+"`" {
			continue // the element AFTER the bare flag decides (pass 2 below)
		}
		if value, ok := fixtureSelectorElementSelector(element, pkg, depth); ok {
			return value, true, true
		}
		if pureUnquoted(element) || fixtureSelectorFuncCallRe.MatchString(element) {
			candidate = true
		}
	}
	// Pass 2: a bare flag whose NEXT element is the selector.
	for i, raw := range elements {
		if fixtureSelectorElementClean(raw) != `"`+fixtureSelectorRunFlag+`"` && fixtureSelectorElementClean(raw) != "`"+fixtureSelectorRunFlag+"`" {
			continue
		}
		if i+1 >= len(elements) {
			return "", true, true // a trailing bare flag takes the empty value
		}
		return fixtureSelectorResolveValue(fixtureSelectorElementClean(elements[i+1]), pkg, depth)
	}
	return "", candidate, false
}

// fixtureSelectorElementSelector resolves an element that NAMES the selector —
// a constant, or a helper call — and reports whether the text it resolves to
// actually spells the flag. An element that resolves to something else (the
// binary `exe`, say) is not a selector.
func fixtureSelectorElementSelector(element string, pkg fixtureSelectorPackage, depth int) (string, bool) {
	if pureUnquoted(element) {
		value, _, ok := fixtureSelectorFollowName(element, pkg, depth)
		if ok && strings.Contains(value, fixtureSelectorRunFlag+"=") {
			return value, true
		}
		return "", false
	}
	if m := fixtureSelectorFuncCallRe.FindStringSubmatch(element); m != nil {
		body, ok := pkg.funcs[m[1]]
		if !ok || !strings.Contains(body, fixtureSelectorRunFlag) {
			return "", false
		}
		if strings.Contains(element, ".Name(") {
			// The helper builds its selector from the CALLER's own test name
			// (`subreaperChildFlags(t.Name())`): non-empty by construction, the
			// same family as a selector written as `t.Name()` inline.
			return "<runtime test name>", true
		}
		value, _, resolved := fixtureSelectorResolveInFunc(m[1], pkg, depth+1)
		return value, resolved
	}
	return "", false
}

// fixtureSelectorElementClean strips the punctuation that belongs to the CALL
// rather than to the element: a trailing `...`, call-closing parens and commas.
func fixtureSelectorElementClean(raw string) string {
	element := strings.TrimSpace(raw)
	for {
		trimmed := strings.TrimRight(element, " \t\n,;)")
		trimmed = strings.TrimSuffix(trimmed, "...")
		if trimmed == element {
			return element
		}
		element = trimmed
	}
}

// fixtureSelectorResolveInFunc resolves a HELPER's own argv (a builder such as
// `voiceCLIChildArgs` or `subreaperChildFlags`). The flag is located in the
// body, and the argument list it sits in is read as the segment after the LAST
// "(" before it — which is the call's own opening parenthesis for
// `-test.run=…` written inline in a `[]string{…}` argv) and the builder's for
// `append([]string{"-test.run=^$", …}, sub...)`. When the body spells no flag at
// all, a second helper call is followed (`childFlags(t.Name())`).
func fixtureSelectorResolveInFunc(name string, pkg fixtureSelectorPackage, depth int) (string, bool, bool) {
	body, ok := pkg.funcs[name]
	if !ok || depth > fixtureSelectorMaxResolveDepth {
		return "", true, false
	}
	if idx := strings.Index(body, fixtureSelectorRunFlag); idx >= 0 {
		if open := strings.LastIndex(body[:idx], "("); open >= 0 {
			return fixtureSelectorResolve(body[open+1:], pkg, depth+1)
		}
	}
	if m := fixtureSelectorFuncCallRe.FindStringSubmatch(strings.TrimSpace(body)); m != nil {
		if _, ok := pkg.funcs[m[1]]; ok && m[1] != name {
			return fixtureSelectorResolveInFunc(m[1], pkg, depth+1)
		}
	}
	return "", true, false
}

// fixtureSelectorResolveValue resolves one argv element to the pattern it
// carries: a string literal, a package constant, a concatenation of literals
// and followable names, a helper function's own argv, or an expression built
// from a running test's own (never empty) name.
func fixtureSelectorResolveValue(element string, pkg fixtureSelectorPackage, depth int) (string, bool, bool) {
	element = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(element), "..."))
	if element == "" {
		return "", true, true
	}
	if fixtureSelectorRuntimeNameRe.MatchString(element) {
		// `t.Name()` is the running test's own name: non-empty by construction.
		// Its exact value does not matter — the question is "can it be empty".
		return "<runtime test name>", true, true
	}
	if pureUnquoted(element) {
		return fixtureSelectorFollowName(element, pkg, depth)
	}
	// A concatenation: literals contribute verbatim, the identifiers between
	// them are followed through the index, and an unfollowable term leaves the
	// element UNRESOLVED rather than silently counted as safe.
	var b strings.Builder
	consumed := 0
	for {
		value, start, end, ok := fixtureSelectorLitAt(element, consumed)
		if !ok {
			break
		}
		between, betweenOK := fixtureSelectorResolveFragment(element[consumed:start], pkg, depth)
		if !betweenOK {
			return "", true, false
		}
		b.WriteString(between)
		b.WriteString(value)
		consumed = end
	}
	tail, tailOK := fixtureSelectorResolveFragment(element[consumed:], pkg, depth)
	if !tailOK {
		return "", true, false
	}
	b.WriteString(tail)
	if b.Len() == 0 {
		return "", true, false
	}
	return b.String(), true, true
}

// fixtureSelectorResolveFragment resolves the identifiers of a fragment that
// carries no literal (the glue between two literals, or a trailing term).
func fixtureSelectorResolveFragment(fragment string, pkg fixtureSelectorPackage, depth int) (string, bool) {
	if onlyPunctuation(fragment) {
		return "", true
	}
	var b strings.Builder
	for _, m := range fixtureSelectorIdentRe.FindAllStringSubmatch(fragment, -1) {
		name := m[1]
		if fixtureSelectorGoWords[name] {
			continue
		}
		value, _, ok := fixtureSelectorFollowName(name, pkg, depth+1)
		if !ok {
			return "", false
		}
		b.WriteString(value)
	}
	if b.Len() == 0 && !onlyPunctuation(fragment) {
		return "", false
	}
	return b.String(), true
}

// fixtureSelectorFollowName resolves one identifier against the package index:
// a constant's right-hand side, or a helper function's own argv.
func fixtureSelectorFollowName(name string, pkg fixtureSelectorPackage, depth int) (string, bool, bool) {
	if depth > fixtureSelectorMaxResolveDepth {
		return "", true, false
	}
	if expr, ok := pkg.exprs[name]; ok {
		return fixtureSelectorResolveValue(expr, pkg, depth+1)
	}
	if body, ok := pkg.funcs[name]; ok {
		if loc := fixtureSelectorBareFlagRe.FindStringIndex(body); loc != nil {
			return fixtureSelectorResolveValue(body[loc[0]:], pkg, depth+1)
		}
		if loc := fixtureSelectorEmptyInlineRe.FindStringIndex(body); loc != nil {
			return fixtureSelectorResolveValue(body[loc[0]:], pkg, depth+1)
		}
	}
	return "", true, false
}

// fixtureSelectorSpawnText returns the text of the call that starts on line i:
// that line plus the lines until its parentheses close, so a multi-line argv is
// read as one call.
func fixtureSelectorSpawnText(lines []string, i int) string {
	var b strings.Builder
	depth := 0
	for j := i; j < len(lines) && j <= i+fixtureSelectorLookaheadLines; j++ {
		b.WriteString(lines[j])
		b.WriteString("\n")
		depth += strings.Count(lines[j], "(") - strings.Count(lines[j], ")")
		if j > i && depth <= 0 {
			break
		}
	}
	return b.String()
}

// fixtureSelectorSpawnArgs returns the ARGUMENT text of the call on one line —
// everything after its first "(" — so the callee and the argv element are never
// one element.
func fixtureSelectorSpawnArgs(line string) string {
	open := strings.Index(line, "(")
	if open < 0 {
		return line
	}
	return line[open+1:]
}

// pureUnquoted reports whether the element is exactly one identifier.
func pureUnquoted(element string) bool {
	m := fixtureSelectorIdentRe.FindAllString(element, -1)
	return len(m) == 1 && m[0] == strings.TrimSpace(element)
}

// onlyPunctuation reports whether a fragment carries nothing but the operators
// a concatenation is allowed to contain. Identifiers are NOT punctuation: a
// fragment carrying one must be followed through the index, or an expression
// like `"-test.run=" + name` would resolve to a bare `-test.run=` and be
// reported as EMPTY — the false positive this predicate exists to avoid.
func onlyPunctuation(fragment string) bool {
	return strings.TrimSpace(strings.NewReplacer(
		"+", " ", ",", " ", ";", " ", "(", " ", ")", " ", "[", " ", "]", " ", ".", " ", "&", " ", ":=", " ",
	).Replace(fragment)) == ""
}
