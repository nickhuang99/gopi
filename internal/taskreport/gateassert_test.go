package taskreport

// gateassert_test.go — the LEAF half of the gate-assertion seam: which sentences
// count as "SURFACE X is RED on <integration branch>", measured against the REAL
// line that motivated the epic.
//
// THE MEASURED CASE this file's positive leg is built from (row
// 01a136be-e3b2-71b9-9219-0b4a2258feff, authored 2026-10-14 03:30:42, dispatched
// 2026-10-16 08:52:47): the row's briefing opened with the sentence below and was
// handed to a run two days after the defect it asserts had been repaired.
// gateAssertBriefingRealText is a VERBATIM copy of that row's briefing text —
// measured blindness on it (before this scanner existed): the base half returned
// claims=0 and the sibling unlanded half fired on nothing, because the sentence
// names no revision and asserts the branch HAS something rather than lacks it.
//
// FOUR ARMS, and the controls matter more than the positive:
//
//	① the motivating sentence — and the whole real briefing — is read, and the
//	   surface it names is the FENCED COMMAND (the thing a child can re-run).
//	② NEGATIVE CONTROL, the ordinary briefing — a body that talks about red ink,
//	   a failing build elsewhere, or the word "masterpiece" yields NOTHING: the
//	   seam must not fire on prose, or it becomes noise the reader learns to skip.
//	③ POSITIVE CONTROL of the negation arm — a sentence reporting that a surface
//	   is NOT red on the branch any more is the REPAIR, not the assertion, and
//	   must stay silent.
//	④ THE UNREADABLE ARM (Rule 15) — a verdict and the branch with NO surface
//	   token is a Miss, not a silent nothing: the caller must be able to tell
//	   "this briefing asserts no such thing" from "it asserts one I could not
//	   read", and the fixture line is REAL repo prose.

import (
	"strings"
	"testing"
)

// gateAssertTestBranch is the integration branch name this file's fixtures spell.
// It is the package's OWN literal (unlandedclaim_test.go's unlandedTestBranch),
// because internal/taskreport is a LEAF and does not import internal/tools.
const gateAssertTestBranch = "master"

// gateAssertBriefingRealText is the VERBATIM first section of row
// 01a136be-e3b2-71b9-9219-0b4a2258feff's briefing (the row that motivated this
// epic), unmodified — including its own "## Symptom" heading, its indented test
// transcript and its file/command spellings.
const gateAssertBriefingRealText = "## Symptom (measured 2026-10-14, worktree of task 01a135da-debd-786d-b384-c0a0751d5923, branch task/01a135dadebd786db384c0a0751d5923)\n" +
	"\n" +
	"`go test -run TestDocClassBannerMatchesManifest ./internal/standards/` is RED on master, in a file\n" +
	"this branch does not touch:\n" +
	"\n" +
	"```\n" +
	"--- FAIL: TestDocClassBannerMatchesManifest (0.01s)\n" +
	"    docclass_gate_test.go:205: devdoc/ledger-readonly-faces.md: class \"historical\" but the file does not open with its banner\n" +
	"```\n" +
	"\n" +
	"## Evidence (commands + raw output, Rule 41 §1)\n" +
	"\n" +
	"Pre-existing, NOT introduced by the branch that measured it:\n" +
	"\n" +
	"```\n" +
	"$ git status --porcelain devdoc/ledger-readonly-faces.md      # (empty — untouched)\n" +
	"```\n" +
	"\n" +
	"## Why it matters\n" +
	"\n" +
	"The row and the banner are written by two different doors (Rule 36): the row went in, the banner did\n" +
	"not. Every `go test ./internal/standards/` on master reports this red beside whatever the branch\n" +
	"under test actually changed.\n"

// gateAssertBriefingMissText is real repo prose for the unreadable arm: the
// epic's OWN briefing describes the shape in the abstract — verdict word, branch
// name, no surface to send a child back to. It is a MEASURED fixture, copied from
// a briefing of this epic, not an invented one.
const gateAssertBriefingMissText = "The third stamp: a briefing of the form \"names its measurement point as a **branch/worktree** and asserts **\\\"<surface> is RED on master\\\"**.\"\n"

// TestGateAssertions_ReadsTheMotivatingBriefingsOwnSentence is arm ① — the
// scanner's reason to exist: the sentence above must yield the command it names,
// so the seam can hand the child back the surface to re-measure.
func TestGateAssertions_ReadsTheMotivatingBriefingsOwnSentence(t *testing.T) {
	claims := GateAssertionsIn(gateAssertBriefingRealText, gateAssertTestBranch)
	if len(claims) != 1 {
		t.Fatalf("the motivating sentence asserts exactly one surface is RED on %s; got %d claim(s): %+v",
			gateAssertTestBranch, len(claims), claims)
	}
	c := claims[0]
	if !strings.Contains(c.Surface, "TestDocClassBannerMatchesManifest") {
		t.Errorf("the surface read must be the command the sentence names (the thing a child can re-run); got %q", c.Surface)
	}
	if c.Branch != gateAssertTestBranch {
		t.Errorf("the claim must carry the branch word the author used (%q); got %q", gateAssertTestBranch, c.Branch)
	}
	if !strings.Contains(c.Line, "is RED on master") {
		t.Errorf("the claim must quote the author's line verbatim; got %q", c.Line)
	}
	if misses := GateAssertionMissesIn(gateAssertBriefingRealText, gateAssertTestBranch); len(misses) != 0 {
		t.Errorf("the real briefing names a surface, so nothing may be reported unreadable; got %+v", misses)
	}
}

// TestGateAssertions_TheRealBriefingMissProseIsANamedRefusal is arm ④, and it
// pins the Rule 15 split: a line that states a RED verdict on the branch without
// naming any surface is a MISS the caller can print, never the same nothing a
// briefing with no assertion produces.
func TestGateAssertions_TheRealBriefingMissProseIsANamedRefusal(t *testing.T) {
	if claims := GateAssertionsIn(gateAssertBriefingMissText, gateAssertTestBranch); len(claims) != 0 {
		t.Fatalf("a line naming no surface-shaped token yields no claim to measure; got %+v", claims)
	}
	misses := GateAssertionMissesIn(gateAssertBriefingMissText, gateAssertTestBranch)
	if len(misses) != 1 {
		t.Fatalf("the unreadable assertion must be STATED (one miss), or a refusal is indistinguishable "+
			"from a briefing that asserts no such thing; got %d: %+v", len(misses), misses)
	}
	if misses[0].Branch != gateAssertTestBranch {
		t.Errorf("the miss must carry the branch word the line used; got %q", misses[0].Branch)
	}
}

// TestGateAssertions_AnOrdinaryBriefingIsUntouched is arm ② in both directions:
// prose about red ink, a build failing elsewhere, and a branch word inside a
// longer word must all yield NOTHING — no claim and no miss. A scanner that
// fires here would stamp ordinary briefings, which is worse than not firing.
func TestGateAssertions_AnOrdinaryBriefingIsUntouched(t *testing.T) {
	for name, body := range map[string]string{
		"red ink, no branch": "# Fix the parser\n\nThe build is red in a hundred places and the test fails on\n" +
			"an unrelated host. Re-measure before repairing.\n",
		"branch word only": "# Fix the parser\n\nThe masterpiece of this refactor is the seam on master; read it first.\n",
		// THE MEASURED FALSE POSITIVE, pinned as prose: the branch and the word
		// "red" are co-resident but the verdict REFERS to an assertion made
		// elsewhere ("reports this red"). This sentence is copied from the
		// motivating briefing's own text, and the binding rule exists because the
		// co-residence rule stamped it.
		"branch before a referred verdict": "# Why it matters\n\n" +
			"Every `go test ./internal/standards/` on master reports this red beside whatever the branch\n" +
			"under test actually changed, so a real regression is masked.\n",
		"glued verdict word": "# Fix the parser\n\nThe failsafe path is enabled on master, and the test suite passes there.\n",
		"no briefing at all": "",
	} {
		claims := GateAssertionsIn(body, gateAssertTestBranch)
		misses := GateAssertionMissesIn(body, gateAssertTestBranch)
		if len(claims) != 0 || len(misses) != 0 {
			t.Errorf("%s: an ordinary briefing asserts no red surface, so the scanner must be silent "+
				"(claims=%+v misses=%+v)", name, claims, misses)
		}
	}
}

// TestGateAssertions_ARepairReportIsNotAnAssertion is arm ③: the sentence that
// reports the defect is GONE states the negation of the very verdict this
// scanner reads, and firing on it would stamp the briefing that already carries
// the answer.
func TestGateAssertions_ARepairReportIsNotAnAssertion(t *testing.T) {
	for name, body := range map[string]string{
		"not red":            "# Already repaired\n\nThe gate is not red on master any more — measured after the fix.\n",
		"no longer red":      "# Already repaired\n\n`go test ./internal/standards/` is no longer red on master.\n",
		"never failed there": "# Already repaired\n\nThe suite never fails on master; the red seen earlier was local.\n",
	} {
		claims := GateAssertionsIn(body, gateAssertTestBranch)
		misses := GateAssertionMissesIn(body, gateAssertTestBranch)
		if len(claims) != 0 || len(misses) != 0 {
			t.Errorf("%s: a negated verdict is the repair, not the assertion (claims=%+v misses=%+v)",
				name, claims, misses)
		}
	}
}

// TestGateAssertions_ReadsTheFourSurfaceShapes pins the surface vocabulary the
// seam's clause quotes, so a later narrowing of the predicate cannot silently
// drop a spelling this repo's briefings actually use.
func TestGateAssertions_ReadsTheFourSurfaceShapes(t *testing.T) {
	for name, tc := range map[string]struct{ line, want string }{
		"fenced command":  {"`go test ./internal/standards/` is RED on master.", "go test ./internal/standards/"},
		"revision anchor": {"The gate is RED on master at 2f697e5 (the revision the report names).", "2f697e5"},
		"test name":       {"TestDocClassBannerMatchesManifest is RED on master, in a file we do not touch.", "TestDocClassBannerMatchesManifest"},
		"repo path":       {"devdoc/ledger-readonly-faces.md is broken on master — the banner never landed.", "devdoc/ledger-readonly-faces.md"},
	} {
		claims := GateAssertionsIn(tc.line+"\n", gateAssertTestBranch)
		if len(claims) != 1 {
			t.Errorf("%s: got %d claim(s), want 1: %+v", name, len(claims), claims)
			continue
		}
		if claims[0].Surface != tc.want {
			t.Errorf("%s: surface = %q, want %q", name, claims[0].Surface, tc.want)
		}
	}
}

// TestGateAssertions_ABriefingInNoOpOut is the seam's own precondition, asserted
// where the predicate lives: a briefing with no such assertion yields no claim
// AND no miss, which is what lets the dispatch seam leave the ordinary briefing
// byte-identical.
func TestGateAssertions_ABriefingInNoOpOut(t *testing.T) {
	const body = "# Fix the read path\n\nRe-implement the parser and add a test for the boundary.\n"
	if claims := GateAssertionsIn(body, gateAssertTestBranch); len(claims) != 0 {
		t.Fatalf("claims=%+v, want none", claims)
	}
	if misses := GateAssertionMissesIn(body, gateAssertTestBranch); len(misses) != 0 {
		t.Fatalf("misses=%+v, want none", misses)
	}
	// An EMPTY branch name is an unanswerable question, not a licence to match
	// any word: the scanners must abstain rather than fire on every line.
	if claims := GateAssertionsIn("`x` is RED on master.\n", ""); len(claims) != 0 {
		t.Fatalf("an empty integration branch names no subject, so nothing may be claimed; got %+v", claims)
	}
}

// TestGateAssertionLineExcerpt_BoundsTheQuotedLine pins the record's own budget:
// a briefing line can be a paragraph, and a record has to stay a record.
func TestGateAssertionLineExcerpt_BoundsTheQuotedLine(t *testing.T) {
	long := strings.Repeat("x", gateAssertionLineMaxRunes*2)
	got := GateAssertionLineExcerpt(long)
	if n := len([]rune(got)); n != gateAssertionLineMaxRunes+len([]rune(gateAssertionEllipsis)) {
		t.Fatalf("a long line must be quoted bounded (%d runes + the ellipsis); got %d", gateAssertionLineMaxRunes, n)
	}
	short := "  `go test ./x/` is RED on master  "
	if got := GateAssertionLineExcerpt(short); got != strings.TrimSpace(short) {
		t.Fatalf("a short line is quoted trimmed; got %q", got)
	}
}

// gateAssertBriefingBranchlessRealText is the VERBATIM first two lines of row
// 01a1432e-4441-71a6-bd4c-f94fce6cbe11's briefing as the dispatch seam COMPOSES
// it (tools.taskRunBriefing: the summary under its header, then the payload) —
// the row this file's branch-less arm was built for. Measured on this tree
// before the arm existed: claims=0 / misses=0, i.e. the sentence the epic exists
// for was invisible and the row went out against a premise its own base tip had
// already fixed.
const gateAssertBriefingBranchlessRealText = "[Task summary]\n" +
	"devdoc/removal-audit-shell-census.md 自相矛盾：文件顶 historical banner，DOC-INDEX.tsv 却归 normative，" +
	"TestDocClassBannerMatchesManifest 在 pristine 树上即红\n" +
	"[Briefing payload]\n" +
	"发现者：task `01a14197-c3ba-7e38-8fdb-ac5e2e092d37`，在跑本 branch 的门禁时撞上，**与本次改动无关**。\n"

// TestGateAssertions_ReadsTheBranchlessPristineAssertion is the arm row
// 01a1432e measured: a verdict bound to a TREE STATE instead of to the
// integration branch's name, in this repo's CJK word order (anchor first,
// verdict last). It is the positive leg of the spelling-gap fix; the negative
// legs below keep the false-positive rule the file already removed intact.
func TestGateAssertions_ReadsTheBranchlessPristineAssertion(t *testing.T) {
	claims := GateAssertionsIn(gateAssertBriefingBranchlessRealText, gateAssertTestBranch)
	if len(claims) != 1 {
		t.Fatalf("the branch-less assertion must be read; got %d claim(s): %+v", len(claims), claims)
	}
	if claims[0].Surface != "TestDocClassBannerMatchesManifest" {
		t.Errorf("surface = %q, want the test name the line names", claims[0].Surface)
	}
	if claims[0].Branch != gateAssertionBranchlessCJKMarker {
		t.Errorf("the claim must carry the ANCHOR the author used (%q, a tree state, not a branch); got %q",
			gateAssertionBranchlessCJKMarker, claims[0].Branch)
	}
	if misses := GateAssertionMissesIn(gateAssertBriefingBranchlessRealText, gateAssertTestBranch); len(misses) != 0 {
		t.Errorf("a read assertion is not a miss; got %+v", misses)
	}
}

// TestGateAssertions_TheBranchlessArmIsBoundNotCoResident pins the arm's SECOND
// half: the same anchors must be silent when no verdict is bound to them, when
// the verdict is negated, and when the verdict is the Latin "red" — the last one
// because a Chinese character is gluing-free, so a co-residence rule would make
// every "红" anywhere near "master" a stamp.
func TestGateAssertions_TheBranchlessArmIsBoundNotCoResident(t *testing.T) {
	for name, body := range map[string]string{
		"anchor with no verdict": "# Report\n\n在 pristine 树上跑完整包，输出见附件。\n",
		"pristine but green":     "# Report\n\n在 pristine 树上即绿，无需修复。\n",
		"negated cjk verdict":    "# Already repaired\n\n这个测试在 pristine 树上已经不红了。\n",
		"latin co-residence": "# Why it matters\n\n" +
			"Every `go test ./internal/standards/` on a pristine tree reports this red beside whatever the branch\n",
		"bare red ink": "# Fix the parser\n\nThe build is red in a hundred places; a pristine checkout is not needed.\n",
	} {
		claims := GateAssertionsIn(body, gateAssertTestBranch)
		misses := GateAssertionMissesIn(body, gateAssertTestBranch)
		if len(claims) != 0 || len(misses) != 0 {
			t.Errorf("%s: no verdict is BOUND to an anchor here, so nothing may be claimed "+
				"(claims=%+v misses=%+v)", name, claims, misses)
		}
	}
}

// TestGateAssertions_TheLatinArmStillNeedsItsBranch is the regression guard for
// the rule the file's own measured false positive installed: widening WHICH
// anchor counts must not widen the DIRECTION. A branch standing after a verdict
// still binds; a branch standing before one still does not.
func TestGateAssertions_TheLatinArmStillNeedsItsBranch(t *testing.T) {
	yes := "`go test ./internal/standards/` is RED on master, in a file this branch does not touch.\n"
	if claims := GateAssertionsIn(yes, gateAssertTestBranch); len(claims) != 1 {
		t.Fatalf("the motivating spelling must still be read; got %+v", claims)
	}
	no := "Every `go test ./internal/standards/` on master reports this red beside whatever the branch changed.\n"
	if claims := GateAssertionsIn(no, gateAssertTestBranch); len(claims) != 0 {
		t.Fatalf("a verdict that REFERS to an assertion made elsewhere must stay silent; got %+v", claims)
	}
}

// TestGateAssertions_TheCJKAnchorMustPrecedeTheVerdict pins the reversed-window
// rule against the corpus-measured shape it excludes: "「恒红」不是 master 的
// 缺陷" ("always-red is NOT a defect of master") asserts the OPPOSITE of a red
// surface, and an anchor-after reading of a CJK verdict fires on it.
func TestGateAssertions_TheCJKAnchorMustPrecedeTheVerdict(t *testing.T) {
	const body = "即「恒红」不是 master 的缺陷，是本机 ref 陈旧造成的假阴性。\n"
	if claims := GateAssertionsIn(body, gateAssertTestBranch); len(claims) != 0 {
		t.Fatalf("a sentence DENYING that master has the defect asserts no red surface; got %+v", claims)
	}
	// The same sentence in the author's asserting order IS read, with the branch
	// as its anchor — the arm is not CJK-only.
	const asserting = "这个门禁在 master 上红：`go test ./internal/standards/` 复现。\n"
	claims := GateAssertionsIn(asserting, gateAssertTestBranch)
	if len(claims) != 1 || claims[0].Branch != gateAssertTestBranch {
		t.Fatalf("a branch-anchored CJK assertion must be read with the branch word; got %+v", claims)
	}
}
