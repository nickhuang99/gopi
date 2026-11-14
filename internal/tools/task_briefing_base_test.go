package tools

// task_briefing_base_test.go — the BASE-REVISION half of the dispatch-time
// premise seam, driven through REAL git in a REAL repository (the shape
// task_briefing_premise_test.go uses, because the verdict is an exit status and
// a text match would not be one).
//
// THREE LEGS, and the middle one is the reason this file exists:
//
//	① RED — a briefing declaring a base revision that is BEHIND the integration
//	   tip gets the stamp, naming the base, its distance behind the tip, the
//	   measurement behind that reading, and the instruction RE-MEASURE BEFORE
//	   REPAIRING. The motivating row (01a136a7-dccd) declared base 2f697e5
//	   while the integration tip was already that base's direct child.
//	② POSITIVE CONTROL — the SAME detector, a base that IS the integration tip,
//	   gets NO stamp. A guard that stamps everything is as useless as one that
//	   stamps nothing — and here the failure mode is worse than noise: a stamp
//	   telling a child to re-measure work that is genuinely due would train the
//	   reader to ignore the clause. The seam's sibling control exists for the
//	   same reason.
//	③ THE UNMEASURABLE ARM — no repo to measure against must be STATED (Rule
//	   15), never read as a pass, never as a refusal of the run.
//
// THE CLAUSE'S OWN BOUNDARY IS A FOURTH ASSERTION, not a leg of its own: the
// stamp must say it is NOT evidence the work is done. A moved base is a fact
// about the BRIEFING'S AGE, and the epic's guard doctrine forbids reading it as
// completion — so the negative is asserted on the text the child receives.

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const (
	baseFixtureBaseMsg  = "base"
	baseFixtureTipMsg   = "tip"
	baseFixtureExtraMsg = "extra"

	// baseFixtureShortShaLen is the abbreviation every declaration in this file
	// spells ("base 2f697e5"). The fixture's own invariant and the three legs
	// that render a declaration read ONE length, so a change here cannot leave
	// the fixture proving a shape nobody drives (Rules 1/13/14: one literal, one
	// spelling).
	baseFixtureShortShaLen = 7

	// baseFixtureShaShapeMaxTries bounds the fixture's search for a base commit
	// whose abbreviation is commit-SHAPED. Each attempt is one empty commit, and
	// a random hex run of 7 characters lacks a letter — or lacks a digit — about
	// 3.7% of the time each, so the per-attempt failure rate is ≈7.4%; 64
	// attempts put the residual below 1e-70. The bound exists so a fixture that
	// somehow never finds one fails LOUDLY instead of looping (Rule 15).
	baseFixtureShaShapeMaxTries = 64
)

// baseFixture is a repo whose integration branch tip is AHEAD of a known base
// revision: the two inputs the two arms need, built once.
type baseFixture struct {
	Repo string
	// Base is an ancestor of the integration tip with commits after it — a
	// briefing declaring it is describing a superseded revision.
	Base string
	// Tip is the integration tip itself — a briefing declaring it is current.
	Tip string
	// Ahead is how many commits the integration branch has that Base does not.
	Ahead int
}

// baseBuildRepo builds the fixture with REAL git commands: a base commit, then
// tipAhead further commits on the integration branch.
func baseBuildRepo(t *testing.T, tipAhead int) baseFixture {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), premiseGitTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", IntegrationBranchName)
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	// THE BASE COMMIT IS CHOSEN FOR ITS ABBREVIATION, not merely created.
	//
	// WHY (the flake this fixture used to carry, row 01a1439f): the legs below
	// declare their base by ABBREVIATING it ("base " + f.Base[:7]), and the
	// production leaf predicate that reads such a declaration
	// (taskreport's implClaimShaShape) requires a commit-shaped token to carry
	// a hex LETTER and a hex DIGIT — the property that separates a revision
	// from an English word made of [a-f] and from a bare count. A sha1 prefix
	// is hex-random, so it lacks a letter roughly (10/16)^7 ≈ 3.7% of the time
	// (and lacks a digit just as often): on those runs the leaf refuses the
	// token, the seam honestly reports an UNREADABLE declaration instead of
	// stamping, and the three legs of
	// TestBriefingBase_TheTwoSilentShapesAreStampedAndStated go red together.
	//
	// That is a FIXTURE defect, never a seam one: the predicate is right to
	// refuse a letter-free token, and the fixture must hand it a token of the
	// shape a real briefing carries. So the fixture SEARCHES for a base whose
	// abbreviation is commit-shaped — an empty commit costs nothing and the
	// bound keeps a pathological repository from looping.
	base := ""
	for i := 0; i < baseFixtureShaShapeMaxTries; i++ {
		git("commit", "--allow-empty", "-q", "-m", baseFixtureBaseMsg)
		cand := git("rev-parse", "HEAD")
		if baseFixtureAbbrevIsCommitShaped(cand) {
			base = cand
			break
		}
	}
	if base == "" {
		t.Fatalf("the fixture could not build a base whose %d-character abbreviation carries both a hex letter "+
			"and a digit in %d attempts — the declarations this file renders would be REFUSED by the leaf "+
			"predicate (taskreport's implClaimShaShape) and every leg below would fail for a reason that is "+
			"about the fixture, not the seam",
			baseFixtureShortShaLen, baseFixtureShaShapeMaxTries)
	}
	for i := 0; i < tipAhead; i++ {
		git("commit", "--allow-empty", "-q", "-m", baseFixtureExtraMsg)
	}
	git("commit", "--allow-empty", "-q", "-m", baseFixtureTipMsg)
	tip := git("rev-parse", "HEAD")
	return baseFixture{Repo: repo, Base: base, Tip: tip, Ahead: tipAhead + 1}
}

// baseFixtureAbbrevIsCommitShaped reports whether the fixture's own
// abbreviation of a revision is the shape the production leaf predicate accepts:
// BOTH a hex letter and a hex digit inside the first baseFixtureShortShaLen
// characters (taskreport's implClaimShaShape — the ONE definition of the shape,
// restated here only because this package cannot import an unexported predicate;
// the fixture self-checks the property rather than assuming it).
//
// It is deliberately the fixture's OWN check, not a call into production: what
// the fixture has to guarantee is that the strings it hands the seam are shaped
// like the declarations real briefings carry — a seam that agreed with the
// fixture by construction would prove nothing.
func baseFixtureAbbrevIsCommitShaped(sha string) bool {
	if len(sha) < baseFixtureShortShaLen {
		return false
	}
	short := sha[:baseFixtureShortShaLen]
	return strings.ContainsAny(short, "0123456789") && strings.ContainsAny(short, "abcdefABCDEF")
}

// baseBriefing renders the briefing shape the motivating row carried: a
// reproduction block that declares the revision its measurement was taken at,
// and a demand to repair the surface. It declares NOTHING about the integration
// branch lacking anything — that is the whole reason the sibling scanner is
// blind to it, and the reason this half exists.
//
// The line is quoted from row 01a136a7-dccd's own payload (its Reproduction
// heading, which reads "... (measured 2026-10-14, base 2f697e5)"), with the
// revision parameterised so the fixture can drive both arms.
func baseBriefing(base string) string {
	return "# BUG: `gopi tool doccheck` exits 1 on the clean tree\n\n" +
		"## Reproduction (measured 2026-10-14, base " + base + ")\n\n" +
		"    cd <repo> && gopi tool doccheck\n\n" +
		"## What to do\n\n" +
		"Add the missing permit row to the frozen table and re-run the census.\n"
}

// TestBriefingBase_ABaseBehindTheTipIsStamped is leg ① — the RED PROOF. The
// briefing's own declared base is behind the integration tip, so the child must
// be told, with the measurement behind it, that the surface it calls red was
// measured at the older revision.
func TestBriefingBase_ABaseBehindTheTipIsStamped(t *testing.T) {
	f := baseBuildRepo(t, 2)
	clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, baseBriefing(f.Base))
	if clause == "" {
		t.Fatalf("a briefing declaring base %s (%d commit(s) behind %s) got no stamp — this is the "+
			"defect: the child repairs a surface that may already be green; record: %v",
			f.Base, f.Ahead, IntegrationBranchName, record)
	}
	ev := MasterClaimEvidenceAt(f.Repo, f.Base)
	for _, want := range []string{
		TaskBriefingPremiseBaseLabel,         // the clause's own marker
		ev.ShortCommit,                       // the base, abbreviated
		IntegrationBranchName,                // the branch it is behind
		TaskBriefingPremiseBaseVerifyOnly,    // what to do
		"run THAT surface again HERE",        // the remedy, spelled
		"NOT evidence that the work is done", // the forbidden reading, denied
		premiseLandedExitPhrase,              // the measurement, not an opinion
		strconv.Itoa(f.Ahead),                // the distance, not a guess
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the stamped clause must carry %q; clause:\n%s", want, clause)
		}
	}
	// THE RECORD IS THE STAMP'S EVIDENCE (Rule 15): one line per base examined,
	// and the stale one must say so — with its distance — where the run's facts
	// are read.
	if len(record) != 1 || !strings.Contains(record[0], "STAMPED") {
		t.Fatalf("the seam's record must carry the stale-base verdict; got %v", record)
	}
	if !strings.Contains(record[0], ev.ShortCommit) || !strings.Contains(record[0], "exit=0") {
		t.Errorf("the record line must name the base and its exit status; got %q", record[0])
	}
	if !strings.Contains(record[0], strconv.Itoa(f.Ahead)) {
		t.Errorf("the record line must carry the distance behind the tip (%d); got %q", f.Ahead, record[0])
	}
}

// TestBriefingBase_ABaseThatIsTheTipIsNotStamped is leg ② — the POSITIVE
// CONTROL. The same detector, the same briefing shape, a base that IS the
// integration tip: nothing is stamped, and the control itself still reaches the
// record (a silent control is indistinguishable from a detector that never ran).
func TestBriefingBase_ABaseThatIsTheTipIsNotStamped(t *testing.T) {
	f := baseBuildRepo(t, 2)
	clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, baseBriefing(f.Tip))
	if clause != "" {
		t.Fatalf("a briefing declaring the CURRENT tip %s as its base was stamped — a clause telling "+
			"the child to re-measure a genuinely due surface is noise the reader learns to "+
			"ignore; clause:\n%s", f.Tip, clause)
	}
	if len(record) != 1 {
		t.Fatalf("the control measurement must still be recorded; got %v", record)
	}
	if !strings.Contains(record[0], "0 commit(s) behind") {
		t.Errorf("the control record must state the measured zero distance; got %q", record[0])
	}
}

// TestBriefingBase_AnUnmeasurableRepoIsStatedNotPassed is leg ③. With no repo to
// measure against, every base is UNMEASURED — the one outcome that must never be
// rendered as a pass (which would silently silence the clause), and never as a
// refusal either (the run proceeds).
func TestBriefingBase_AnUnmeasurableRepoIsStatedNotPassed(t *testing.T) {
	f := baseBuildRepo(t, 1)
	clause, record := TaskBriefingPremiseBaseRefresh("", baseBriefing(f.Base))
	if clause != "" {
		t.Fatalf("an unmeasurable repo produced a stamp — nothing was measured, so nothing may be "+
			"asserted; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("an unmeasurable repo must be STATED per base (never a silent pass); got %v", record)
	}
}

// TestBriefingBase_ABareFullLengthShaIsMeasuredWhole is the seam-side half of row
// 01a1373b: the leaf scanner used to apply its look-ahead window to the TEXT, so
// a FULL-LENGTH revision written bare after the base word was handed to this seam
// as a 31-character clip — a revision the author never wrote. The seam then asked
// git about THAT string, and in a repository where the clip happens to be unique
// git answered about the same object, which is why this arm is a LOCK rather than
// a red proof (the red proof lives one level down, in the leaf's own
// TestBaseClaimsReturnsABareFullLengthShaWhole; a red proof HERE would need two
// objects sharing a 124-bit prefix, which no fixture can build).
//
// What this arm pins is the property the epic names: the revision this seam
// measures with IS the author's own object — the resolution of the full-length
// token, not of a shortened one.
func TestBriefingBase_ABareFullLengthShaIsMeasuredWhole(t *testing.T) {
	f := baseBuildRepo(t, 2)
	// The declaration carries NO abbreviation: only a token of full length can
	// tell "measured the author's revision" from "measured a clip of it".
	body := baseBriefingDeclaration("base " + f.Base)
	clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
	if clause == "" {
		t.Fatalf("a briefing declaring base %s (%d commit(s) behind %s) got no stamp; record: %v",
			f.Base, f.Ahead, IntegrationBranchName, record)
	}
	if want := masterClaimShortSha(f.Base); !strings.Contains(clause, want) {
		t.Fatalf("the clause must name the commit the author's full-length revision RESOLVES to (%s); got:\n%s",
			want, clause)
	}
	// The distance the clause states is measured against the ref the seam itself
	// resolved (IntegrationClaimTargetRef — the ONE resolution), so the assertion
	// reads that ref rather than the branch name.
	ref := IntegrationClaimTargetRef(f.Repo)
	if !strings.Contains(clause, masterClaimShortSha(f.Base)+baseClaimRangeSep+ref) {
		t.Errorf("the clause must carry the distance command measured against %s; got:\n%s", ref, clause)
	}
}

// TestBriefingBase_ABriefingNamingNoBaseIsUntouched is the arm that keeps this
// clause from degrading every briefing: the ordinary dispatch declares no base
// revision, and a briefing-in / no-op-out must be indistinguishable from the
// seam never having run.
func TestBriefingBase_ABriefingNamingNoBaseIsUntouched(t *testing.T) {
	f := baseBuildRepo(t, 1)
	for name, body := range map[string]string{
		"no base word":     "# Fix the read path\n\nRe-implement the parser and add a test.\n",
		"base word only":   "# Fix the read path\n\nThe base case of the recursion is wrong.\n",
		"glued substring":  "# Fix the read path\n\nThis codebase 2f697e5 grew a database 34693afb3.\n",
		"rev without base": "# Fix the read path\n\nRe-measure 2f697e5 before repairing.\n",
	} {
		clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
		if clause != "" || len(record) != 0 {
			t.Errorf("%s: the briefing declares no base revision, so the seam must be silent "+
				"(clause=%q record=%v)", name, clause, record)
		}
	}
}

// baseBriefingDeclaration renders the SAME briefing, but takes the parenthetical
// VERBATIM — no literal "base " is prepended. It exists because the fixture above
// hard-codes the word, which means a test that passes "基线2f697e5" through it is
// actually exercising "base 基线2f697e5": the literal word does the work and the
// glued spelling is never read. That false arm was measured while writing this
// (the CJK segment passed pre-fix through the literal, not through the glue).
func baseBriefingDeclaration(decl string) string {
	return "# BUG: `gopi tool doccheck` exits 1 on the clean tree\n\n" +
		"## Reproduction (measured 2026-10-14, " + decl + ")\n\n" +
		"    cd <repo> && gopi tool doccheck\n\n" +
		"## What to do\n\n" +
		"Add the missing permit row to the frozen table and re-run the census.\n"
}

// TestBriefingBase_TheTwoSilentShapesAreStampedAndStated is the arm the epic
// (row 01a13715, 2026-10-15) was filed for: two briefing spellings a REAL
// briefing here uses were SILENTLY silent at the seam — a CJK base word glued
// to its revision ("基线2f697e5") and an upper-case revision ("2F697E5"). Both
// were unreadable to the leaf predicate, so the seam reported nothing, which is
// indistinguishable from "this briefing declares no base revision".
//
// Both shapes now go through the SAME real-git measurement as the lower-case,
// space-separated control (which this arm also drives, so the three are compared
// against one fixture and the difference cannot be an artifact of the fixture):
// each must be STAMPED and its record must name the base.
func TestBriefingBase_TheTwoSilentShapesAreStampedAndStated(t *testing.T) {
	f := baseBuildRepo(t, 2)
	// THE FIXTURE'S INVARIANT, asserted where it is CONSUMED: every declaration
	// below abbreviates the base, and the production leaf only reads an
	// abbreviation that carries both a hex letter and a digit. If the fixture
	// ever stops guaranteeing that, this arm must say so HERE — otherwise the
	// three legs below would fail for a fixture reason dressed as a seam defect
	// (the flake row 01a1439f was filed for).
	if short := f.Base[:baseFixtureShortShaLen]; !baseFixtureAbbrevIsCommitShaped(f.Base) {
		t.Fatalf("fixture invariant broken: base %s abbreviates to %q, which lacks a hex letter or a hex digit — "+
			"the leaf predicate that reads such a declaration would refuse it and every leg below would "+
			"red for a FIXTURE reason, never a seam one", f.Base, short)
	}
	for name, decl := range map[string]string{
		"lower case, spaced (the shipped control)": "base " + f.Base[:baseFixtureShortShaLen],
		"upper case hex":                 "base " + strings.ToUpper(f.Base[:baseFixtureShortShaLen]),
		"cjk word glued to the revision": "基线" + f.Base[:baseFixtureShortShaLen],
	} {
		body := baseBriefingDeclaration(decl)
		clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
		if clause == "" {
			t.Errorf("%s (%q): no stamp — the briefing declares a base %d commit(s) behind %s and "+
				"the seam is silently silent about it; record: %v",
				name, decl, f.Ahead, IntegrationBranchName, record)
			continue
		}
		if !strings.Contains(clause, TaskBriefingPremiseBaseVerifyOnly) {
			t.Errorf("%s: the stamp must carry %q; clause:\n%s", name, TaskBriefingPremiseBaseVerifyOnly, clause)
		}
		if len(record) != 1 || !strings.Contains(record[0], "STAMPED") {
			t.Errorf("%s: the record must carry the stale-base verdict; got %v", name, record)
		}
	}
}

// TestBriefingBase_ARefusedRevisionBesideABaseWordIsStatedNotSilent is the
// HONEST-SILENCE half (Rule 15). A token that is revision-length but not
// revision-SHAPED ("12345678" — no letter) leaves the scanner with no claim, and
// the seam must say so rather than report the same nothing it reports for a
// briefing that declares no base at all. It must NOT stamp: nothing was measured.
func TestBriefingBase_ARefusedRevisionBesideABaseWordIsStatedNotSilent(t *testing.T) {
	f := baseBuildRepo(t, 2)
	const refused = "12345678"
	body := baseBriefing("base " + refused)
	clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
	if clause != "" {
		t.Fatalf("nothing was measured (the token is not revision-shaped), so nothing may be stamped; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], refused) {
		t.Fatalf("the seam must STATE the refused declaration (the token %s and the reason), never "+
			"report the same nothing as a briefing with no base at all; got %v", refused, record)
	}
	if !strings.Contains(record[0], taskBriefingPremiseBaseUnreadableReason) {
		t.Errorf("the record must carry the stated reason %q; got %q",
			taskBriefingPremiseBaseUnreadableReason, record[0])
	}
}

// The stated-refusal arm must stay SILENT on the shapes the leaf is silent about
// by design — the shipped no-base arm above drives those through the same seam,
// and this one pins the boundary the epic's constraint names: an ordinary
// briefing (and an ordinary English word) must stay a briefing-in/no-op-out.
func TestBriefingBase_AnOrdinaryBriefingGainsNoRecordLine(t *testing.T) {
	f := baseBuildRepo(t, 1)
	for name, body := range map[string]string{
		"prose only":     "# Fix the read path\n\nRe-implement the parser and add a test.\n",
		"word as prose":  "# Fix the read path\n\nThe base case of the recursion is wrong.\n",
		"glued word":     "# Fix the read path\n\nThis codebase 12345678 grew a database 34693afb3.\n",
		"revision only":  "# Fix the read path\n\nRe-measure 2f697e5 before repairing.\n",
		"short hex word": "# Fix the read path\n\nThe base is 0xdead.\n",
	} {
		clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
		if clause != "" || len(record) != 0 {
			t.Errorf("%s: the briefing declares no readable base revision, so the seam must be "+
				"silent (clause=%q record=%v)", name, clause, record)
		}
	}
}

// The refusal is stated on SHAPE alone, and this arm pins that deliberately: a
// revision-length digit run standing beside the word "base" is STATED even when
// a human would read the sentence as prose. Judging "did the author mean a
// revision" is a guess, and the cost of this arm being over-broad is one record
// line — never a stamp, never an instruction to the child (Rule 15: a refusal
// the seam cannot see is the defect; a refusal the seam states is a record).
func TestBriefingBase_ARefusalIsStatedOnShapeEvenForProseShapedLines(t *testing.T) {
	f := baseBuildRepo(t, 1)
	const tok = "12345678"
	body := "# Fix the read path\n\nThe base of the tree is " + tok + " nodes deep.\n"
	clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
	if clause != "" {
		t.Fatalf("a refused token measured nothing, so nothing may be stamped; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], tok) {
		t.Fatalf("the refused declaration must be STATED (it is a hole in the instrument, not a "+
			"fact about the briefing); got %v", record)
	}
}

// incidentPopulationBriefing is the shape of the briefing that motivated this
// leg, quoted from the REAL prompt row 01a136ef-87b2-79ab-97d1-bd90bfb8c455
// received (the text was extracted from that run's own session log; the
// revision is parameterised so the fixture can drive it against its own repo).
//
// WHAT THE SHAPE IS: a briefing that reports a MEASUREMENT — a freeze-table
// POPULATION ("carries 168 rows while the tree produces 167") that its own named
// base no longer had — and asserts NONE of the vocabulary the sibling's
// unlanded-claim scanner keys on. The revision the author actually anchored to
// lives in the line above it, as a base DECLARATION naming the revision the
// measurement was taken at.
func incidentPopulationBriefing(base string) string {
	return "# bug: a STALE PERMIT in `internal/docclass/testdata/anchor-number-baseline.tsv` " +
		"reddens three `internal/docclass` tests on a CLEAN tree at HEAD\n\n" +
		"## What is measured (command + raw output, not inference)\n\n" +
		"On THIS run's worktree (base commit `" + base + "`, plus the banner change):\n\n" +
		"```\n" +
		"$ make test-budget PKGS='./internal/docclass ./internal/standards'\n" +
		"  package verdict: FAIL: github.com/nickhuang99/gopi/internal/docclass\n" +
		"    failing tests: 3\n" +
		"```\n\n" +
		"So: the table carries **168 rows** and the tree now produces **167** incoherent\n" +
		"anchor rows — one permit's `(doc, path, anchor)` no longer drifts.\n"
}

// TestBriefingBase_AMeasuredPopulationPremiseIsCaughtThroughItsBase is leg ⑤ —
// THE INCIDENT SHAPE, and it is a COVERAGE assertion rather than a signal for a
// new guard: the row this repo spent a full run on was a briefing quoting a
// freeze-table POPULATION the base no longer had, and the question the run had
// to answer by measurement was whether any dispatch-time machinery covers it.
//
// The answer is measured here on the two halves, and both halves matter:
//
//   - the SIBLING is structurally silent ON THIS SHAPE — a population sentence
//     asserts "not an ancestor" / "never landed" and so on for nothing, so the
//     landed/unlanded scanner returns no clause and no record (the same
//     structural silence measured for the "gate G is RED at rev R" family);
//   - the BASE clause DOES cover it — but only through the DECLARATION the
//     author anchored the measurement to, never through the count itself. The
//     count is a re-derivable figure with no shape predicate behind it; the
//     revision beside "base commit" is the part of such a briefing that is a
//     claim about the object graph.
//
// So a regression here is not "bad wording", it is the incident's cost coming
// back: a briefing whose population sentence is stale reaches a child that was
// told nothing, and the child spends its budget measuring instruments that were
// already green. If a future edit narrows the base predicate past this shape,
// the measured coverage below must fail loudly rather than quietly degrade.
func TestBriefingBase_AMeasuredPopulationPremiseIsCaughtThroughItsBase(t *testing.T) {
	f := baseBuildRepo(t, 2)
	body := incidentPopulationBriefing(f.Base)

	// THE SIBLING'S SILENCE, asserted rather than assumed: it is the reason the
	// base half has to carry this shape at all.
	if clause, _ := TaskBriefingPremiseRefresh(f.Repo, body); clause != "" {
		t.Errorf("the unlanded-claim scanner stamped a briefing that asserts no unlanded claim — "+
			"a premise scanner that fires on a measurement is noise; clause:\n%s", clause)
	}

	// THE BASE HALF'S COVERAGE: the declaration naming the revision the
	// measurement was taken at is read, measured against the integration tip,
	// and stamped with the narrow instruction.
	clause, record := TaskBriefingPremiseBaseRefresh(f.Repo, body)
	if clause == "" {
		t.Fatalf("a briefing whose measurement was taken at base %s (%d commit(s) behind %s) got no "+
			"stamp — this is the incident shape: the child is told nothing and re-measures a surface "+
			"nobody asked it to re-measure; record: %v", f.Base, f.Ahead, IntegrationBranchName, record)
	}
	for _, want := range []string{
		TaskBriefingPremiseBaseLabel,         // the clause's own marker
		TaskBriefingPremiseBaseVerifyOnly,    // RE-MEASURE BEFORE REPAIRING — not "the work is done"
		"NOT evidence that the work is done", // the forbidden reading, denied
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the stamp for the incident shape must carry %q; clause:\n%s", want, clause)
		}
	}
	// The stamp must NOT claim completion: the population the briefing reports
	// may well still be wrong for a different reason, and a moved base is a fact
	// about the briefing's AGE.
	for _, forbidden := range []string{TaskBriefingPremiseVerifyOnly} {
		if strings.Contains(clause, forbidden) {
			t.Errorf("the base clause must never carry the landed-claim instruction %q — a base behind "+
				"the tip is not evidence the work is done; clause:\n%s", forbidden, clause)
		}
	}
	if len(record) != 1 || !strings.Contains(record[0], "STAMPED") {
		t.Fatalf("the incident shape's measurement must be recorded where the run's facts are read; "+
			"got %v", record)
	}
}

// TestBriefingBase_ASecondPassAppendsNothing is the idempotence contract: the
// clause QUOTES the briefing line it contradicts, so a re-read of a stamped
// briefing would re-fire on its own quote. It must append nothing and SAY so —
// a resume, a re-dispatch and an operator paste all hand a briefing over twice.
func TestBriefingBase_ASecondPassAppendsNothing(t *testing.T) {
	f := baseBuildRepo(t, 1)
	body := baseBriefing(f.Base)
	clause, _ := TaskBriefingPremiseBaseRefresh(f.Repo, body)
	if clause == "" {
		t.Fatalf("leg setup failed: the first pass did not stamp")
	}
	again, record := TaskBriefingPremiseBaseRefresh(f.Repo, body+"\n\n"+clause)
	if again != "" {
		t.Fatalf("a second pass appended a clause — a stamped briefing must be idempotent; got:\n%s", again)
	}
	if len(record) != 1 || !strings.Contains(record[0], "already carries a freshness stamp") {
		t.Fatalf("a second pass must STATE that nothing was appended; got %v", record)
	}
}
