package taskreport

// baseclaim_test.go — the base-revision scanner's TWO BLIND SPOTS, measured at
// the leaf (epic row 01a13715, 2026-10-15).
//
// The scanner shipped in b0890fa420 was SILENTLY silent on two briefing shapes a
// real briefing here uses, and silent is exactly the wrong answer: the seam that
// consumes it (tools.TaskBriefingPremiseBaseRefresh) cannot tell "this briefing
// declares no base" from "this briefing declares a base in a spelling I do not
// recognise" — so the one measurement that exists to catch a stale base simply
// never ran, and reported nothing about not running.
//
//   - SHAPE 1 — a CJK base word GLUED to its revision ("基线2f697e5"): the
//     word-boundary guard rejected "基线" because the rune after it is a Latin
//     hex digit, i.e. the guard read the base word and its own revision as one
//     longer word.
//   - SHAPE 2 — an UPPERCASE-hex revision ("2F697E5"): the shape predicate's
//     letter test only accepted [a-f], so a revision spelled the way `git
//     rev-parse` prints it for some tools, or the way an author types it,
//     carried no letter and was refused.
//
// Both were measured against real git before the fix: an upper-cased revision
// resolves and is an ancestor exactly like its lower-case spelling
// (`git rev-parse --verify --quiet 95F4DE7^{commit}` → 95f4de7123f7…;
// `git merge-base --is-ancestor 95F4DE7 master` → exit=0), so shape 2 was a
// FALSE NEGATIVE of the predicate, not a conservatism.
//
// The arms below are the RED half: they fail on the pre-fix predicate and pass
// after it. The last arm pins the OTHER half of the remedy — the shapes that are
// STILL refused must be STATED, never silent (Rule 15), because a refusal the
// seam cannot see is indistinguishable from a briefing that declares no base.

import (
	"strings"
	"testing"
)

const (
	// baseClaimTestSha is a commit-shaped token with both a letter and a digit,
	// the two properties the shape predicate requires (the motivating revision
	// from the doc this scanner was shipped for).
	baseClaimTestSha = "2f697e5"
	// baseClaimTestUpper is the SAME revision as an author may type it.
	baseClaimTestUpper = "2F697E5"
	// baseClaimTestCJKWord is the Chinese base word this repo's briefings use.
	baseClaimTestCJKWord = "基线"
	// baseClaimTestFullSha is a FULL-LENGTH revision — 40 hexadecimal characters
	// carrying both a letter and a digit, i.e. a shape the predicate ACCEPTS. It
	// is longer than baseClaimLookaheadRunes on purpose: it is the token the
	// scanner used to cut (row 01a1373b).
	baseClaimTestFullSha = "74f5df769ec605149273b0cd6ad9f918452f2588"
)

// The clause's own quoting shape, copied from the measured briefing: the base
// stands inside a parenthetical, which is what makes the revision ENCLOSED.
func baseClaimTestLine(decl string) string {
	return "## Reproduction (measured 2026-10-14, " + decl + ")\n"
}

func TestBaseClaimsAcceptsAnUppercaseHexRevision(t *testing.T) {
	for _, decl := range []string{
		"base " + baseClaimTestUpper,
		"base `" + baseClaimTestUpper + "`",
		baseClaimTestCJKWord + " " + baseClaimTestUpper,
	} {
		claims := BaseClaimsIn(baseClaimTestLine(decl))
		if len(claims) != 1 {
			t.Errorf("declaration %q: claims = %+v, want one — an upper-case revision is the same "+
				"object as its lower-case spelling (git resolves both), so refusing it makes the "+
				"seam silent instead of measuring", decl, claims)
			continue
		}
		if claims[0].Commit != baseClaimTestUpper {
			t.Errorf("declaration %q: commit = %q, want %q (the token verbatim — nothing here "+
				"rewrites the author's text)", decl, claims[0].Commit, baseClaimTestUpper)
		}
	}
}

func TestBaseClaimsAcceptsACJKBaseWordGluedToTheRevision(t *testing.T) {
	// The measured shape: no space, no backticks — the word and its revision
	// are one visual unit, and the pre-fix boundary guard read them as one word.
	for _, decl := range []string{
		baseClaimTestCJKWord + baseClaimTestSha,
		baseClaimTestCJKWord + "`" + baseClaimTestSha + "`",
		baseClaimTestCJKWord + baseClaimTestUpper,
	} {
		claims := BaseClaimsIn(baseClaimTestLine(decl))
		if len(claims) != 1 {
			t.Errorf("declaration %q: claims = %+v, want one — a CJK base word followed DIRECTLY by "+
				"a hexadecimal run is the base word and its revision, never a longer word", decl, claims)
			continue
		}
	}
}

// The glued-CJK exception is the ONE place the boundary guard bends, so the
// glue it must STILL reject is pinned beside it: a Latin-adjacent "base" is a
// substring of an ordinary English word, and the CJK word must not swallow a
// non-hex neighbour either.
func TestBaseClaimsStillRejectsGluedLatinSubstringsAndNonHexCJKGlue(t *testing.T) {
	for name, decl := range map[string]string{
		"codebase":    "this codebase " + baseClaimTestSha,
		"database":    "the database " + baseClaimTestSha,
		"cjk+non-hex": baseClaimTestCJKWord + "zz" + baseClaimTestSha,
	} {
		if claims := BaseClaimsIn(baseClaimTestLine(decl)); len(claims) != 0 {
			t.Errorf("%s (%q): claims = %+v, want none — the boundary guard still rejects a base "+
				"word glued into a longer word", name, decl, claims)
		}
	}
}

// TestBaseClaimsReturnsABareFullLengthShaWhole pins the THIRD blind spot — a
// class of its own, and the opposite of the two above: not a shape the predicate
// REFUSED, but a shape it read and TRUNCATED (row 01a1373b, 2026-10-15).
//
// `baseClaimCommitAfter` applied the look-ahead window to the TEXT before
// tokenising the window (`baseClaimRuneWindow(line[at:], 32)`), so a sha written
// bare after the base word — "Measured at base <40 hex>: the surface is red." —
// was cut at 31 hex characters and the scanner returned a revision the author
// never wrote:
//
//	[{Line:Measured at base 74f5df769ec605149273b0cd6ad9f918452f2588: the surface is red.
//	  Commit:74f5df769ec605149273b0cd6ad9f918452f91}]
//
// Both producer faces QUOTE what they measured into re-runnable git commands —
// the dispatch seam (tools.TaskBriefingPremiseBaseRefresh) renders
// "`git merge-base --is-ancestor <rev> <ref>`" and the submit seam
// (tools.TaskSubmitBaseStalenessWarning) the same pair — so the clip did not stay
// in the leaf: a reviewer re-running the quoted command re-runs a revision the
// author never wrote.
//
// The remedy keeps the window a BOUND and removes the CUT: it is applied to a
// token's START offset, so a token beginning inside the window is returned whole.
// The narrowness the scanner exists for is unchanged, which is why the arms below
// include the shapes that must STILL yield no claim.
func TestBaseClaimsReturnsABareFullLengthShaWhole(t *testing.T) {
	for _, decl := range []string{
		// The measured fixture, verbatim in shape: a bare full-length sha right
		// after the base word with prose following it.
		"Measured at base " + baseClaimTestFullSha + ": the surface is red.",
		"base " + baseClaimTestFullSha,
		"base commit " + baseClaimTestFullSha,
		baseClaimTestCJKWord + baseClaimTestFullSha,
		"| base commit | " + baseClaimTestFullSha + " |",
	} {
		claims := BaseClaimsIn(baseClaimTestLine(decl))
		if len(claims) != 1 {
			t.Fatalf("declaration %q: claims = %+v, want one (a full-length sha is commit-SHAPED, "+
				"so the window must not change whether it is read)", decl, claims)
		}
		if claims[0].Commit != baseClaimTestFullSha {
			t.Fatalf("declaration %q: commit = %q (%d chars), want the author's %d-character token "+
				"%q — the look-ahead window bounds HOW FAR the revision may stand, never how much of "+
				"it is returned", decl, claims[0].Commit, len(claims[0].Commit),
				len(baseClaimTestFullSha), baseClaimTestFullSha)
		}
	}
}

// The other half of that remedy — the window is STILL a bound, so the shapes it
// was written to exclude stay excluded, and a base word standing with no
// commit-shaped token beside it still declares nothing at all ("the base case of
// the recursion", "base commit table"). Without these arms a "return the longest
// hex run on the line" rewrite would pass the test above and quietly widen the
// predicate.
func TestBaseClaimsACutFreeWindowIsStillABound(t *testing.T) {
	for name, line := range map[string]string{
		// A base word and NO revision at all: no claim.
		"base word only":   "the base case of the recursion is wrong",
		"base commit only": "the base commit table is stale",
		// A revision-LENGTH token, but too far after the base word to belong to
		// it: the window's own contract, unchanged by this row.
		"revision outside the window": "base " + strings.Repeat("x", baseClaimLookaheadRunes) + " " + baseClaimTestFullSha,
		// A sub-abbreviation run is not a revision.
		"too short": "base 12345",
	} {
		if claims := BaseClaimsIn(baseClaimTestLine(line)); len(claims) != 0 {
			t.Errorf("%s (%q): claims = %+v, want none — the predicate stays as narrow as it was", name, line, claims)
		}
	}
}

// The honest-silence half (Rule 15): a token that is revision-shaped by LENGTH
// but REFUSED by the predicate (`12345678` — no letter; `deadbeef` — no digit)
// leaves the scanner with no claim, and the seam then cannot tell "declared
// nothing" from "declared something I could not read". So the leaf names the
// refusal, and the seam can state it.
func TestBaseClaimsStatesWhenARevisionLengthTokenIsRefused(t *testing.T) {
	for _, tok := range []string{"12345678", "deadbeef", "deadbeefdeadbeef"} {
		line := baseClaimTestLine("base " + tok)
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Fatalf("token %q: claims = %+v, want none — the predicate refuses it", tok, claims)
		}
		misses := BaseClaimMissesIn(line)
		if len(misses) != 1 {
			t.Fatalf("token %q: misses = %+v, want one — a refused revision-length token beside a "+
				"base word must be STATED, never silent", tok, misses)
		}
		if misses[0].Token != tok {
			t.Errorf("token %q: miss names %q, want the refused token itself", tok, misses[0].Token)
		}
	}
}

// The miss arm must stay silent on the shapes the scanner is silent about BY
// DESIGN: prose that merely contains the word "base" (no token at all) declares
// nothing, and the ordinary briefing must not grow a record line for it.
func TestBaseClaimMissesStaySilentWithoutARefusedToken(t *testing.T) {
	for name, body := range map[string]string{
		"no base word":     "## Verification\n- re-implement the parser and add a test\n",
		"base word only":   "## Verification\n- the base case of the recursion is wrong\n",
		"glued substring":  "## Verification\n- this codebase 2f697e5 grew a database 34693afb3\n",
		"rev without base": "## Verification\n- re-measure 2f697e5 before repairing\n",
	} {
		if misses := BaseClaimMissesIn(body); len(misses) != 0 {
			t.Errorf("%s: misses = %+v, want none — the line names no base revision at all", name, misses)
		}
	}
}

// The miss names the token AS THE AUTHOR WROTE IT (the record quotes the
// spelling) and is case-insensitive about the shape it refuses — an upper-case
// token with no digit is refused for the same reason its lower-case spelling is.
func TestBaseClaimMissesQuoteTheAuthorsSpelling(t *testing.T) {
	line := baseClaimTestLine("base DEADBEEF")
	if claims := BaseClaimsIn(line); len(claims) != 0 {
		t.Fatalf("claims = %+v, want none — letters only, no digit", claims)
	}
	misses := BaseClaimMissesIn(line)
	if len(misses) != 1 || misses[0].Token != "DEADBEEF" {
		t.Fatalf("misses = %+v, want the token verbatim (\u201cDEADBEEF\u201d) — the record quotes the "+
			"author's spelling, never a normalised one", misses)
	}
}

// A briefing that declares a READABLE base and ALSO carries a refused token
// stays silent about the readable one (it is measured) — the miss is about the
// line the scanner could not read.
func TestBaseClaimsAndMissesAreIndependent(t *testing.T) {
	body := baseClaimTestLine("base "+baseClaimTestSha) +
		baseClaimTestLine("base 12345678")
	if claims := BaseClaimsIn(body); len(claims) != 1 {
		t.Fatalf("claims = %+v, want one (the readable declaration)", claims)
	}
	misses := BaseClaimMissesIn(body)
	if len(misses) != 1 || misses[0].Token != "12345678" {
		t.Fatalf("misses = %+v, want exactly the refused token", misses)
	}
}

// baseClaimTestAncestryLine is the MEASURED line this row was filed for, verbatim
// from the submit receipt: an ANCESTRY CHECK, not a declaration of the revision
// the briefing was measured at.
const baseClaimTestAncestryLine = "$ git merge-base --is-ancestor 202399cfe 21a72442d && echo YES"

// TestBaseClaimsIgnoresAGitRevisionVerbNeighbourhood is the RED arm of this row
// (01a14511, 2026-10-16).
//
// THE DEFECT: `git merge-base --is-ancestor <sha> <tip>` carries a standalone
// "base" ("merge-base") followed within baseClaimLookaheadRunes by a
// commit-shaped token, so the scanner read it as a DECLARED BASE REVISION — and
// the submit seam then measured that revision against the integration branch and
// stamped "RE-MEASURE BEFORE REPAIRING" on a line whose whole purpose is to
// state that the fix IS an ancestor of the tip. The warned-about revision is an
// ancestor BY CONSTRUCTION, so the warning could only ever be wrong there: a
// warning that misdirects costs more than silence (Rule 15).
//
// THE CRITERION is the verb NEIGHBOURHOOD, never standalone-ness alone: the text
// between the base word and the candidate revision names a git REVISION VERB, so
// the revision belongs to that command, not to a base declaration. Every verb in
// baseClaimRevisionVerbs is exercised — a list one can read but not honour is
// how this defect would come back.
func TestBaseClaimsIgnoresAGitRevisionVerbNeighbourhood(t *testing.T) {
	lines := []string{baseClaimTestAncestryLine}
	for _, verb := range baseClaimRevisionVerbs {
		// The shortest shape the neighbourhood must cover: the verb sits
		// directly between the base word and the revision — "base" inside
		// "merge-base" is the measured spelling, so it is spelled out here.
		lines = append(lines, "merge-"+verb+" "+baseClaimTestSha+" "+baseClaimTestFullSha)
		lines = append(lines, "git "+verb+" --oneline "+baseClaimTestSha)
	}
	for _, line := range lines {
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Errorf("line %q: claims = %+v, want NONE — the text between the base word and the "+
				"revision names a git revision verb, so the revision belongs to that command and "+
				"declares no base (the measured defect: an ancestry check stamped STALE)", line, claims)
		}
	}
}

// The other half — the scanner must NOT go quiet on the declarations it exists
// for, and the negative control must stay where it is. Without these arms a
// "drop any line containing a git verb" rewrite would pass the arm above and
// silently un-instrument the seam.
func TestBaseClaimsStillReadsDeclarationsBesideAGitVerb(t *testing.T) {
	// POSITIVE CONTROL: the three declaration shapes every measured briefing uses
	// still yield exactly one claim, and the claim names the revision.
	for _, decl := range []string{
		"base " + baseClaimTestSha,
		"| base commit | " + baseClaimTestSha + " |",
		baseClaimTestCJKWord + baseClaimTestSha,
	} {
		claims := BaseClaimsIn(baseClaimTestLine(decl))
		if len(claims) != 1 {
			t.Errorf("declaration %q: claims = %+v, want ONE — the verb neighbourhood must not "+
				"widen into 'any git word on the line'", decl, claims)
			continue
		}
		if claims[0].Commit != baseClaimTestSha {
			t.Errorf("declaration %q: commit = %q, want %q", decl, claims[0].Commit, baseClaimTestSha)
		}
	}
	// The same declaration on a line that ALSO carries a git command: the
	// neighbourhood is the text BETWEEN the base word and the revision, so a verb
	// standing elsewhere on the line does not swallow the declaration.
	line := "base " + baseClaimTestSha + " (re-run with git show)"
	if claims := BaseClaimsIn(line); len(claims) != 1 {
		t.Errorf("line %q: claims = %+v, want ONE — a verb that is not between the base word and "+
			"the revision does not own the revision", line, claims)
	}
	// NEGATIVE CONTROL, unchanged by this row.
	if claims := BaseClaimsIn("codebase " + baseClaimTestSha + " grew a database " + baseClaimTestFullSha); len(claims) != 0 {
		t.Errorf("negative control: claims = %+v, want NONE — a substring 'base' declares nothing", claims)
	}
}

// baseClaimTestRefSha / baseClaimTestRefFullSha are the revisions the REF-PINNED
// arms declare — the ones row 01a17dc1's briefing spelled ("origin/master
// a802b47", "on master a802b47") and the pre-fix predicate never measured.
const (
	baseClaimTestRefSha     = "a802b47"
	baseClaimTestRefFullSha = "c6e93569e"
)

// baseClaimTestRefPinLines are the MEASURED spellings a briefing uses to pin a
// revision to a REF — the shape the epic this file belongs to was filed for
// (row 01a17dc1: "detach 到 origin/master `a802b47` 跑同一个测试").
//
// Each is a MEASUREMENT OBJECT: "the ref WAS at this revision when I looked".
// That is exactly the claim BaseClaimsIn exists to hand the seam for a
// staleness comparison, and the pre-fix predicate returned claims=0 AND
// misses=0 on every one of them — the two silences the seam cannot tell apart
// (Rule 15).
var baseClaimTestRefPinLines = []struct{ line, commit string }{
	{"### 1) 在 origin/master a802b47（detached）上跑那个测试", "a802b47"},
	{"on master a802b47", "a802b47"},
	{"| `refs/remotes/origin/master` | `c6e93569e` | 4 |", "c6e93569e"},
	{"- detached at origin/master " + "`b12e9c743`" + " 上复现", "b12e9c743"},
	{"refs/remotes/origin/master = a0e14e8   <-- 移动了", "a0e14e8"},
}

// TestBaseClaimsReadsARevisionPinnedToARef is the POSITIVE arm of this row
// (01a18231, 2026-10-28).
//
// THE DEFECT: the predicate required a base WORD ("base", "基线"), so a briefing
// that named its measurement object the way this repo's briefings actually do —
// a ref plus the revision at it — produced NOTHING. It was not "declared
// something I could not read"; it was indistinguishable from "declared no base
// at all", which is why row 01a17dc1 shipped a 44-minute-stale reading of master
// and the seam said nothing (Rule 15).
//
// A claim OR a stated miss is acceptable for every shape; 0/0 is not.
func TestBaseClaimsReadsARevisionPinnedToARef(t *testing.T) {
	for _, c := range baseClaimTestRefPinLines {
		claims := BaseClaimsIn(c.line)
		misses := BaseClaimMissesIn(c.line)
		if len(claims) == 0 && len(misses) == 0 {
			t.Errorf("line %q: claims=0 AND misses=0 — a revision pinned to a ref is a MEASUREMENT "+
				"OBJECT this scanner must either read or state (the two silences are "+
				"indistinguishable at the seam)", c.line)
			continue
		}
		if len(claims) == 0 {
			continue
		}
		if claims[0].Commit != c.commit {
			t.Errorf("line %q: commit = %q, want %q", c.line, claims[0].Commit, c.commit)
		}
	}
}

// The ref arm must not swallow the shapes that are COMMANDS, not declarations —
// above all the ancestry check this family of fixes already regressed once on
// (row 01a14511), and the range/id shapes its own tokenisation produces.
//
// The distinction is POSITIONAL, never a substring: in "is-ancestor <sha>
// origin/master" the ref is the comparison OPERAND and stands AFTER the
// revision; in "git log origin/master <sha>" the ref is an ARGUMENT, and the
// text before it names a git revision verb. Neither is a measurement object.
func TestBaseClaimsRefArmStaysSilentOnCommandShapes(t *testing.T) {
	for _, line := range []string{
		baseClaimTestAncestryLine,
		"$ git merge-base --is-ancestor f6031ead9 origin/master",
		"$ git log origin/master a802b47",
		"$ git show origin/master:a802b47",
		"$ git rev-list --count refs/remotes/origin/master..afbf9b5   -> 9",
		"92f5018..a0e14e8  master     -> origin/master",
		"- 引入这两个漂移的 commit 都是 origin/master 的祖先",
	} {
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Errorf("line %q: claims = %+v, want NONE — a command's operand or a bare ref is not a "+
				"declared measurement object", line, claims)
		}
		if misses := BaseClaimMissesIn(line); len(misses) != 0 {
			t.Errorf("line %q: misses = %+v, want NONE — nothing on this line declares a revision", line, misses)
		}
	}
	// NEGATIVE CONTROL, unchanged by this row: the letters of "base" inside
	// longer words declare nothing, and the ref arm must not pick them up.
	line := "codebase " + baseClaimTestSha + " grew a database " + baseClaimTestFullSha
	if claims, misses := BaseClaimsIn(line), BaseClaimMissesIn(line); len(claims) != 0 || len(misses) != 0 {
		t.Errorf("negative control: claims=%+v misses=%+v, want 0/0 — 'codebase'/'database' declare nothing",
			claims, misses)
	}
}

// The ref arm's REFUSAL half: a ref pinned to a revision-LENGTH token the shape
// predicate will not read must be STATED, exactly like the base-word arm's
// ("base 12345678"). Otherwise the epic's own acceptance ("a claim OR a stated
// miss, never 0/0") would be satisfiable only for the spellings that happen to
// be readable.
func TestBaseClaimsStatesARevisionPinnedToARefItCannotRead(t *testing.T) {
	for _, tok := range []string{"12345678", "deadbeef"} {
		line := "origin/master " + tok
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Fatalf("line %q: claims = %+v, want none — the predicate refuses that token", line, claims)
		}
		misses := BaseClaimMissesIn(line)
		if len(misses) != 1 || misses[0].Token != tok {
			t.Fatalf("line %q: misses = %+v, want exactly the refused token %q — a refusal the seam "+
				"cannot see is indistinguishable from a briefing that declares nothing", line, misses, tok)
		}
	}
}

// baseClaimTestProbeSha is the revision the SYMPTOM's probe table quotes — the
// one `gopi task submit` stamped as a declared base (row 01a18232's receipt:
// "base a802b473 is 64 commit(s) behind 84beb309").
const baseClaimTestProbeSha = "a802b47"

// baseClaimTestProbeTally is the tally the probe table prints beside a specimen —
// the predicate's OWN answer, which is why the row it stands on measures the
// scanner instead of declaring a base.
const baseClaimTestProbeTally = "claims=1 misses=0"

// baseClaimTestQuotedShapes are the THREE spellings a briefing uses to QUOTE the
// shape "base <rev>" — a row of the probe table that measures this very
// predicate, an inline citation, and the same specimen in quotes. Each carries
// the revision the submit seam warned about (row 01a18232, 2026-10-28).
var baseClaimTestQuotedShapes = []string{
	"| base " + baseClaimTestProbeSha + " | " + baseClaimTestProbeTally + " |",
	"| `base " + baseClaimTestProbeSha + "` | " + baseClaimTestProbeTally + " |",
	"`base " + baseClaimTestProbeSha + "`",
	`"base ` + baseClaimTestProbeSha + " " + baseClaimTestProbeTally + `"`,
}

// TestBaseClaimsQuotedShapeIsNotADeclaration is the RED arm of this row
// (01a18232, 2026-10-28).
//
// THE DEFECT: `gopi task submit` stamped "[Base revision — a declared base was
// re-measured at submit] … RE-MEASURE BEFORE REPAIRING" on a row of the payload's
// own PROBE TABLE, whose text says what this scanner answers for the specimen
// `base a802b47`. The payload declared no base revision anywhere; it measured the
// predicate. The consequence is the expensive one: the warning sends the
// executor to re-measure a802b47 (64 commits behind) instead of re-reading the
// scanner.
//
// The criterion is the same failure mode row 01a14511 closed for COMMAND shapes
// ("merge-base --is-ancestor"): a shape that is NAMED — quoted, fenced, or
// printed beside the predicate's own tally — is a SPECIMEN, not a declaration.
func TestBaseClaimsQuotedShapeIsNotADeclaration(t *testing.T) {
	for _, line := range baseClaimTestQuotedShapes {
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Errorf("line %q: claims = %+v, want NONE — the line quotes the shape to say what the "+
				"predicate answers; it declares no measurement point, and stamping it sends the "+
				"reader to re-measure the wrong object (row 01a18232)", line, claims)
		}
	}
}

// TestBaseClaimsQuotingFenceDoesNotSwallowARealDeclaration is the other half, and
// it is the arm a "drop any line with a quote on it" rewrite would fail.
//
// Every entry is a shape the scanner MUST keep reading, and each one sits inside
// a markdown table cell or a parenthetical — the same punctuation the quoting
// criterion above uses. The discriminator is never the punctuation alone: a
// declaration names a base and a revision, and nothing on the line reports the
// predicate's own answer, which is what a tally in the same cell says.
func TestBaseClaimsQuotingFenceDoesNotSwallowARealDeclaration(t *testing.T) {
	// POSITIVE CONTROL: measured declarations, including the table-cell shape
	// (whose leading cell rule puts "|" before the base word) and the
	// parenthetical citation the closure's own arm spells.
	for _, decl := range []string{
		"base " + baseClaimTestSha,
		"| base commit | " + baseClaimTestSha + " |",
		"(base " + baseClaimTestSha + ")",
		"Measured at base " + baseClaimTestSha + ", the surface is red.",
	} {
		claims := BaseClaimsIn(baseClaimTestLine(decl))
		if len(claims) != 1 {
			t.Errorf("declaration %q: claims = %+v, want ONE — a declaration is not a specimen, and "+
				"the quoting criterion must not widen into 'any line carrying a fence'", decl, claims)
			continue
		}
		if claims[0].Commit != baseClaimTestSha {
			t.Errorf("declaration %q: commit = %q, want %q", decl, claims[0].Commit, baseClaimTestSha)
		}
	}
	// A genuine table declaration beside a non-tally column: text in ANOTHER
	// cell is not a specimen of this revision, so the claim stands.
	cell := "| measured at base " + baseClaimTestSha + " | 63% of the suite ran |"
	if claims := BaseClaimsIn(cell); len(claims) != 1 {
		t.Errorf("line %q: claims = %+v, want ONE — a column in a different cell says nothing about "+
			"this revision", cell, claims)
	}
	// NEGATIVE CONTROLS, unchanged by this row: row 01a14511's ancestry check is
	// still a COMMAND, and the letters of "base" inside longer words still declare
	// nothing.
	if claims := BaseClaimsIn(baseClaimTestAncestryLine); len(claims) != 0 {
		t.Errorf("ancestry line %q: claims = %+v, want NONE", baseClaimTestAncestryLine, claims)
	}
	neg := "codebase " + baseClaimTestSha + " grew a database " + baseClaimTestFullSha
	if claims := BaseClaimsIn(neg); len(claims) != 0 {
		t.Errorf("negative control %q: claims = %+v, want NONE — a substring 'base' declares nothing", neg, claims)
	}
}

// TestBaseClaimsQuotingArmLeavesTheRefArmAlone pins the SIBLING half this row
// must not aggravate (row 01a18231: 7 real briefings spell their measurement
// object as a ref pinned to a revision and were never measured).
//
// The quoting criterion lives on the base-word arm only. A ref pin is a
// MEASUREMENT OBJECT by construction ("the ref WAS here when I looked"), so no
// amount of surrounding prose may silence it — and every spelling below starts
// with CJK prose, a lead-in word or a ref, never with a base word.
func TestBaseClaimsQuotingArmLeavesTheRefArmAlone(t *testing.T) {
	for _, c := range baseClaimTestRefPinLines {
		if claims := BaseClaimsIn(c.line); len(claims) != 1 {
			t.Errorf("ref-pin line %q: claims = %+v, want ONE — the quoting criterion belongs to the "+
				"base-word arm and must not touch the ref arm (row 01a18231)", c.line, claims)
			continue
		}
		if claims := BaseClaimsIn(c.line); claims[0].Commit != c.commit {
			t.Errorf("ref-pin line %q: commit = %q, want %q", c.line, claims[0].Commit, c.commit)
		}
	}
}

// baseClaimTestBranchPinProbes is the shape census for the THIRD shell of this
// scanner's false-positive family (row 01a1a61f, 2026-11-04).
//
// Every clone-channel briefing states, as a fact about its own ground, "checked
// out on branch task/<32hex>" — and the branch's suffix is this row's OWN task
// id, a commit-SHAPED hex run. The bare-branch arm read it as a declared base and
// the seam stamped "briefing base → UNMEASURED (git rev-parse could not resolve
// 01a1a5df…)" on a briefing that declares no base at all. Measured on this rev
// BEFORE the fix: rows 1, 2 and 4 reported claims=1, and the whole real briefing
// (10699 runes) reported claims=1.
//
// The criterion is the token's own BOUNDARY, deliberately not its shape: a
// revision is a NAME of its own, so it can never be the SUFFIX of a ref path
// ("task/<hex>") nor the TAIL of a longer word ("task<hex>"). Rows 2 and 4 are
// that family; rows 5–7 are the shapes that never fired; rows 8–9 are the
// POSITIVE CONTROLS row 01a18231 shipped, which must survive untouched — the
// epic's constraint 2, "正解不许被误伤".
//
// Row 3 is the honest boundary of the fix: a bare hex name standing directly
// after the lead-in and its name ("on branch <hex>", "on master <hex>") is NOT a
// fragment of anything, so it still declares — exactly as it did before. Only
// the fragment reading changed.
var baseClaimTestBranchPinProbes = []struct {
	name   string
	line   string
	commit string // "" = declares nothing
}{
	{"7hex-branch-suffix", "on branch task/2f697e5", ""},
	{"32hex-branch-suffix", "on branch task/01a1a5dfacbb7462b9f36f412e73d6ba", ""},
	{"32hex-bare-name", "on branch 01a1a5dfacbb7462b9f36f412e73d6ba", "01a1a5dfacbb7462b9f36f412e73d6ba"},
	{"no-slash-glued", "on branch task01a1a5dfacbb7462b9f36f412e73d6ba", ""},
	{"name-without-lead-in", "branch 01a1a5dfacbb7462b9f36f412e73d6ba", ""},
	{"ref-path-alone", "task/01a1a5dfacbb7462b9f36f412e73d6ba", ""},
	{"lead-in-alone", "on 01a1a5dfacbb7462b9f36f412e73d6ba", ""},
	{"positive-control-bare-branch", "on master a802b47", "a802b47"},
	{"positive-control-qualified", "rebase 到 origin/master f2d6d39 之上", "f2d6d39"},
}

// TestBaseClaimsIgnoresARevisionThatIsARefNameFragment is the RED-then-GREEN arm
// for row 01a1a61f: the row's whole shape census in ONE table, so the two
// positive controls can never be "fixed" away by a later hand.
func TestBaseClaimsIgnoresARevisionThatIsARefNameFragment(t *testing.T) {
	for _, p := range baseClaimTestBranchPinProbes {
		claims := BaseClaimsIn(p.line)
		if p.commit == "" {
			if len(claims) != 0 {
				t.Errorf("%s (%q): claims = %+v, want NONE — a hex run that is a FRAGMENT of a "+
					"ref name declares no base (the branch's own task id is not a revision of "+
					"the briefing)", p.name, p.line, claims)
			}
			continue
		}
		if len(claims) != 1 {
			t.Errorf("%s (%q): claims = %+v, want ONE — this is a POSITIVE CONTROL (row "+
				"01a18231's acceptance face); the fragment criterion must not reach it",
				p.name, p.line, claims)
			continue
		}
		if claims[0].Commit != p.commit {
			t.Errorf("%s (%q): commit = %q, want %q", p.name, p.line, claims[0].Commit, p.commit)
		}
	}
}

// TestBaseClaimsOnARealCloneChannelBriefing reports the row's own measurement:
// the full 10699-rune briefing this defect was filed from declares NO base, so
// the scanner must be silent on it — claims=0 AND misses=0, because a "stated
// miss" here would still stamp a warning about a revision nobody declared.
//
// The body is INLINE rather than a fixture file on purpose: it is one line of
// real briefing prose, and a fixture would put a second copy of the defect's
// shape in the tree where this comment could not explain it.
func TestBaseClaimsOnARealCloneChannelBriefing(t *testing.T) {
	body := "It is registered in no worktree list and NOBODY ELSE'S repo holds its commits: " +
		"it is a CLONE — a standalone repository of its own, checked out on branch " +
		"task/01a1a5dfacbb7462b9f36f412e73d6ba. Work here.\n" +
		"gopi task show --branch task/01a1a5dfacbb7462b9f36f412e73d6ba reads this row off it.\n"
	claims := BaseClaimsIn(body)
	misses := BaseClaimMissesIn(body)
	if len(claims) != 0 || len(misses) != 0 {
		t.Errorf("clone-channel briefing body: claims=%+v misses=%+v, want none — every such "+
			"briefing names its own task id inside a branch name, so reading it as a base makes "+
			"the seam's UNMEASURED record constant noise (Rule 15: the real UNMEASURED and the "+
			"fake one become one sentence)", claims, misses)
	}
}

// baseClaimTestRefQuotedShapes are the spellings a briefing uses to QUOTE the
// ref-pinned shape — the ref arm's half of row 01a18232's criterion (row
// 01a1a620, 2026-11-04).
//
// THE MEASURED CASE. `gopi task submit` stamped "[Base revision — a declared base
// was re-measured at submit] … - base f2d6d393 is 821 commit(s) behind 8c967277"
// on a payload that declared no base at all: it quoted two POSITIVE CONTROLS to
// say what this predicate must keep answering. Row 01a18232 closed that hole on
// the base-WORD arm and left the ref arm unguarded, so the same warning fired on
// the ref spelling and sent its reader to re-measure a revision the row never
// used.
//
// Every entry is a SPECIMEN — the pair stands inside one quotation, closed by the
// fence that opened it, by the cell rule that closes it, or by a tally of this
// scanner's own answer.
var baseClaimTestRefQuotedShapes = []string{
	"`on master " + baseClaimTestRefSha + "`",
	`"on master ` + baseClaimTestRefSha + `"`,
	"| on master " + baseClaimTestRefSha + " | ok |",
	"| `on master " + baseClaimTestRefSha + "` | " + baseClaimTestProbeTally + " |",
	"we cite `on master " + baseClaimTestRefSha + "` as the acceptance sample",
	"see \"on master " + baseClaimTestRefSha + "\"",
	"`on master " + baseClaimTestRefSha + " " + baseClaimTestProbeTally + "`",
	`BaseClaimsIn("on master ` + baseClaimTestRefSha + `")`,
	`BaseClaimsIn("rebase 到 origin/master ` + baseClaimTestRefSha + ` 之上")`,
	"| measured on master " + baseClaimTestRefSha + " | 4 |",
}

// baseClaimTestRefDeclaredShapes are the ref-pinned spellings that are
// DECLARATIONS and must keep answering — row 01a18231's acceptance face, which
// this row's fix is not allowed to touch.
//
// Each one is a measurement object this repo's briefings really spell, and each
// sits one punctuation mark away from a specimen above: `- detached at
// origin/master `b12e9c743“ fences its REVISION (the shape a declaration is
// usually written in), a two-cell table row fences the ref and the revision
// SEPARATELY, and "rebase 到 origin/master f2d6d39 之上" carries no quotation at
// all.
var baseClaimTestRefDeclaredShapes = []struct{ line, commit string }{
	{"on master " + baseClaimTestRefSha, baseClaimTestRefSha},
	{"we are on master " + baseClaimTestRefSha, baseClaimTestRefSha},
	{"rebase 到 origin/master " + baseClaimTestRefSha + " 之上", baseClaimTestRefSha},
	{"origin/master " + baseClaimTestRefSha, baseClaimTestRefSha},
	{"- detached at origin/master `b12e9c743` 上复现", "b12e9c743"},
	{"| `refs/remotes/origin/master` | `" + baseClaimTestRefFullSha + "` | 4 |", baseClaimTestRefFullSha},
	{"### 1) 在 origin/master " + baseClaimTestRefSha + "（detached）上跑那个测试", baseClaimTestRefSha},
	{"- 交付提交：`174a137`（rebase 到 `origin/master " + baseClaimTestRefSha + "` 之上）", baseClaimTestRefSha},
}

// TestBaseClaimsRefArmQuotedShapeIsNotADeclaration is the RED arm of row
// 01a1a620: the ref arm's specimens must yield NO claim.
func TestBaseClaimsRefArmQuotedShapeIsNotADeclaration(t *testing.T) {
	for _, line := range baseClaimTestRefQuotedShapes {
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Errorf("line %q: claims = %+v, want NONE — the pair stands inside one quotation "+
				"(a closed fence, a quote, a table cell, or beside the scanner's own tally), so "+
				"the line CITES the shape instead of declaring a measurement point (row "+
				"01a1a620)", line, claims)
		}
	}
}

// TestBaseClaimsRefArmStillReadsADeclaration is the other half of the same arm,
// and it is the part a "drop any quoted line" rewrite would fail: row 01a18231's
// positive controls — and this repo's own way of writing a declaration, a fenced
// REVISION — must survive untouched.
func TestBaseClaimsRefArmStillReadsADeclaration(t *testing.T) {
	for _, c := range baseClaimTestRefDeclaredShapes {
		claims := BaseClaimsIn(c.line)
		if len(claims) != 1 {
			t.Errorf("line %q: claims = %+v, want ONE — this is a DECLARATION (row 01a18231's "+
				"acceptance face); the specimen criterion must not reach it", c.line, claims)
			continue
		}
		if claims[0].Commit != c.commit {
			t.Errorf("line %q: commit = %q, want %q", c.line, claims[0].Commit, c.commit)
		}
	}
}

// TestBaseClaimsRefQuotingArmLeavesTheSiblingArmsAlone pins the two behaviours
// this row's fix is bounded by, in ONE table so neither can be traded for the
// other.
//
//  1. `01a1a61f`'s fix must not regress: a hex run that is a FRAGMENT of a ref
//     name ("on branch task/<32hex>") is not a revision, quoted or not.
//  2. The base WORD stays the core shape: `from base <rev>` / `on base <rev>` are
//     real declarations, and a fix that widened into "any quoted ref pin" must
//     never reach them — exempting those would close the very door this family of
//     fixes exists to open.
func TestBaseClaimsRefQuotingArmLeavesTheSiblingArmsAlone(t *testing.T) {
	fragments := []string{
		"on branch task/01a1a5dfacbb7462b9f36f412e73d6ba",
		"`on branch task/01a1a5dfacbb7462b9f36f412e73d6ba`",
		"| on branch task/01a1a5dfacbb7462b9f36f412e73d6ba | ok |",
		"on branch task/2f697e5",
	}
	for _, line := range fragments {
		if claims := BaseClaimsIn(line); len(claims) != 0 {
			t.Errorf("line %q: claims = %+v, want NONE — a hex run that is a FRAGMENT of a ref "+
				"name declares no base (row 01a1a61f, which this row must not regress)",
				line, claims)
		}
	}
	declarations := []string{
		"from base 8c967277e0e5",
		"on base 8c967277e0e5",
		"baseline: 8c967277e0e5 is what I worked from",
	}
	for _, line := range declarations[:2] {
		if claims := BaseClaimsIn(line); len(claims) != 1 {
			t.Errorf("line %q: claims = %+v, want ONE — the base WORD is the core shape; a fix "+
				"that exempts it closes the door this scanner exists to open", line, claims)
		}
	}
	// "baseline" is NOT the base word (measured on master before this row): the
	// predicate reads a STANDALONE word, so the compound declares nothing. Pinned
	// so a later widening cannot start reporting it.
	if claims := BaseClaimsIn(declarations[2]); len(claims) != 0 {
		t.Errorf("line %q: claims = %+v, want NONE — \"baseline\" is a compound, not the base "+
			"word", declarations[2], claims)
	}
}

// TestBaseClaimsOnThePayloadLineThatWasStamped is the row's own reproduction,
// verbatim: the payload line the submit seam quoted in its warning must yield NO
// claim, because the payload declares no base — it states what this predicate
// must keep answering.
//
// Both spellings of the quotes are covered: the payload as the author typed it
// ("on master …", inside a backticked call) and as the stamp re-rendered it with
// escaped quotes (\"on master …\").
func TestBaseClaimsOnThePayloadLineThatWasStamped(t *testing.T) {
	for _, line := range []string{
		"2. **正解不许被误伤**：`BaseClaimsIn(\"on master a802b47\")`、" +
			"`BaseClaimsIn(\"rebase 到 origin/master f2d6d39 之上\")` 仍返回 1 条（01a18231 的验收面）。",
		"2. **正解不许被误伤**：`BaseClaimsIn(\"on master " + baseClaimTestRefSha + "\")`、" +
			"`BaseClaimsIn(\"rebase 到 origin/master " + baseClaimTestRefSha + " 之上\")` 仍返回 1 条。",
	} {
		claims := BaseClaimsIn(line)
		if len(claims) != 0 {
			t.Errorf("payload line %q: claims = %+v, want NONE — the payload QUOTES two positive "+
				"controls; reading them as a declared base stamped a STALE warning about a "+
				"revision the row never used (row 01a1a620)", line, claims)
		}
	}
}
