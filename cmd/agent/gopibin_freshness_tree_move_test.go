package main

// gopibin_freshness_tree_move_test.go — WHICH of the two causes a content
// refusal is: "the binary predates an edit" (the guard's reason for existing) or
// "the checkout MOVED under the run" (an environment fact that is not a test
// defect).
//
// THE DEFECT THIS PINS (measured 2026-10-31, task 01a18daa): the suite builds
// its tested binary once at the start of a run and then runs for 11–13 minutes.
// The freshness guard compares the artifact's build revision against the tree
// the TEST finds by walking up to `go.mod` — so when that checkout is moved
// under the run (another worker's `pull --rebase`, a delivery push, a rebase),
// every e2e test that has not run yet refuses in 0.01–0.04s:
//
//	<bin> does NOT contain the tree's production sources: 3 file(s) differ from
//	the revision it was built from (347579b2) — this test drives the BUILT
//	binary, so it would verify code that is not in that binary.
//	changed: cmd/agent/merge_run.go, …
//
// The refusal is CORRECT about the artifact and WRONG about the run: nothing in
// the package is broken, and the reader's remedy ("rebuild it and re-run") is
// exactly what the next run does before the same move happens again. Measured
// census over 7304 run streams: 520 runs (7%) carry this refusal and the victim
// SET is fixed — 13 top-level e2e tests, peaking at 187 runs with all 13.
//
// SO THE FIX IS AT THE GUARD, and it is an ISOLATION, not a relaxation. The two
// causes are told apart by MEASUREMENT, and the abstention is granted only when
// every one of these holds:
//
//	1. the checkout's HEAD is not the artifact's build revision;
//	2. the checkout's REFLOG records the move that produced the current HEAD,
//	   and that move happened AFTER the artifact was built — a time fact, not an
//	   inference from content;
//	3. every production file the guard would name is accounted for by commits
//	   between the build revision and HEAD (`git diff --name-only R HEAD`), so
//	   nothing in the difference is a hand edit;
//	4. there is NO uncommitted production change (`git status --porcelain`) —
//	   a dirty production file is the ORDINARY case the guard exists for, and it
//	   still refuses, whatever the reflog says.
//
// Condition 4 is what keeps the minimal reproduction alive: append one line to a
// production source without committing it and HEAD has not moved, so nothing
// abstains and the guard refuses exactly as it always did (pinned by
// TestFreshnessStillRefusesAnUncommittedProductionEdit). An abstention is
// withheld whenever ANY of the four questions cannot be answered (no reflog, an
// unreadable revision, a git failure) — an absent answer is reported and the
// refusal stands (Rule 15).
//
// WHAT THE ABSTENTION IS NOT: it is not a pass. The test does not run and does
// not claim to have verified anything; every abstention prints the time facts
// (build revision, build moment, when the checkout moved and from which commit,
// the commits in between) and the suite's close-out counts them, so a run whose
// e2e guards abstained cannot be read as "these e2e paths were verified".

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// treeMoveAbstainPrefix opens EVERY abstention. It is the vocabulary the
	// runner's summary and a reader key on, so it is one value with one spelling
	// (Rule 1b/14) rather than a sentence retyped per arm.
	treeMoveAbstainPrefix = "ABSTAIN (the tree moved under this run)"
	// treeMoveAbstainHint is the remedy an abstention carries. It is deliberately
	// NOT "rebuild and re-run" — that is the refusal's remedy and it is wrong
	// here: the next run rebuilds before the same move happens again.
	treeMoveAbstainHint = "This is not a test defect and not a stale artifact: the checkout moved after the binary was built. Nothing in this package is broken, and this test did NOT run — its e2e path is UNVERIFIED in this run (a green here would be a claim it cannot make). Re-run in a checkout that is not being moved, or accept this run as abstaining on the e2e paths below."
	// treeMoveAbstainSkipReason is the LITERAL reason a guard's skip carries. The
	// measured facts are logged separately (assertGopiBinIsFresh's t.Logf), because
	// a skip whose reason is a format string reads as "%s" in every summary that
	// does not also fetch the log — and the style gate refuses one outright.
	treeMoveAbstainSkipReason = "the checkout MOVED under this run after the tested binary was built, so this e2e test cannot speak for the tree: the difference is commits that landed while the suite ran, not an edit to the code under test (the measured build revision, build moment, the move and its commits are logged above). This path is UNVERIFIED in this run — a green here would be a claim it cannot make"
	// treeMoveCountedLine is the suite close-out's tally line, printed once, so a
	// package that read green while its e2e guards abstained says so out loud.
	treeMoveCountedLine = "SUITE FRESHNESS ABSTENTIONS: %d e2e guard(s) abstained (the checkout moved under this run) — those e2e paths are UNVERIFIED in this run, not passing"
	// treeMoveCountedNone is the same line when nothing abstained: a suite that
	// says "0" is telling the reader the measurement was TAKEN (Rule 15).
	treeMoveCountedNone = "SUITE FRESHNESS ABSTENTIONS: none (the checkout held still for every freshness guard this run reached)"

	// The reflog reading. `%gd` with `--date=unix` renders `HEAD@{<epoch>}` — the
	// moment the ref MOVED, which is the fact that decides "moved under this run";
	// a commit's own timestamp would answer a different question (when the work
	// was authored), and after a `pull --rebase` the commit time is older than
	// the move by hours.
	treeMoveReflogVerb          = "reflog"
	treeMoveReflogShowArg       = "show"
	treeMoveReflogDateFormatArg = "--date=unix"
	treeMoveReflogFormatArg     = "--format=%gd|%H|%gs"
	treeMoveReflogFieldSep      = "|"
	treeMoveReflogFields        = 3
	treeMoveReflogStampOpen     = "{"
	treeMoveReflogStampClose    = "}"

	// The commit range an abstention reports.
	treeMoveLogVerb        = "log"
	treeMoveLogOnelineArg  = "--oneline"
	treeMoveLogNoDecorArg  = "--no-decorate"
	treeMoveRangeSep       = ".."
	treeMoveCommitsShown   = 5
	treeMoveRevListVerb    = "rev-list"
	treeMoveRevListCountFl = "--count"

	// The uncommitted-change reading: the SAME two trees and the SAME filters the
	// guard's content comparison uses (Rule 14 — one reader for the scope).
	treeMoveStatusArg          = "status"
	treeMoveStatusPorcelainArg = "--porcelain"
	treeMoveStatusPathFields   = 2
	treeMoveStatusFieldSep     = " "
)

// treeMoveEvidence is the measured answer "this checkout moved after the
// artifact was built, and the move accounts for every file the guard would name".
// It carries only values that were READ: an empty field means a question was not
// answered, never a guess (Rule 15).
type treeMoveEvidence struct {
	// movedAt is when the reflog says the checkout reached its current HEAD.
	movedAt time.Time
	// from / to are the revisions the move went from and to (the `to` is HEAD).
	from string
	to   string
	// commitCount is how many commits the move carried (0 when git could not
	// count them — reported as "unknown", never as "none").
	commitCount int
	// commits are the move's own one-line subjects, newest first, capped for a
	// readable refusal.
	commits []string
	// explained are the moved files, all of which appear in
	// `git diff --name-only from..to -- cmd internal`.
	explained []string
}

// treeMoveAbstentions counts the guards that abstained in this process. It is a
// COUNTER and not a decision: TestMain reads it at close-out and prints it, so a
// green package cannot hide that its e2e paths did not run.
//
//nolint:gochecknoglobals // process-level tally, read by TestMain's close-out
var treeMoveAbstentions int64

// treeMoveAbstentionCount returns how many guards abstained so far — read by the
// close-out line and by the gate that asserts the tally is wired.
func treeMoveAbstentionCount() int64 { return atomic.LoadInt64(&treeMoveAbstentions) }

// restoreTreeMoveAbstentionTally puts the process tally back to what it was
// before a test deliberately exercised the abstention.
//
// WHY: the tally is what TestMain's close-out line reports, so a test that calls
// the guard with a designed-to-abstain fixture would otherwise make EVERY full
// run print "N e2e guards abstained" — a line that reads as "this suite's e2e
// paths went unverified" when in fact the suite moved nothing and the abstention
// was the test's own. Measured 2026-10-31: the first full run printed
// `SUITE FRESHNESS ABSTENTIONS: 2`, both of them produced by the two arms below,
// and the honest number for that run was ZERO. A tally that counts its own
// witnesses is exactly the kind of instrument that reads green for the wrong
// reason, so each arm restores it and the close-out keeps meaning what it says.
func restoreTreeMoveAbstentionTally(t *testing.T, before int64) {
	t.Helper()
	atomic.StoreInt64(&treeMoveAbstentions, before)
}

// noteTreeMoveAbstention records one abstention. It is called by the guard itself
// (assertGopiBinIsFresh), so the tally cannot drift from the skips a reader sees.
func noteTreeMoveAbstention() { atomic.AddInt64(&treeMoveAbstentions, 1) }

// treeMoveReflogEntry is one reflog row: when the ref was written and the value
// it was written with.
type treeMoveReflogEntry struct {
	at  time.Time
	rev string
	why string
}

// treeMoveReflog reads the checkout's own record of its ref movements, newest
// first. An error means the record could not be read at all — the caller must
// then WITHHOLD the abstention rather than assume the tree held still.
func treeMoveReflog(root string) ([]treeMoveReflogEntry, error) {
	out, err := gitOutput(root, treeMoveReflogVerb, treeMoveReflogShowArg, gitHeadRevision,
		treeMoveReflogDateFormatArg, treeMoveReflogFormatArg)
	if err != nil {
		return nil, err
	}
	var entries []treeMoveReflogEntry
	for _, line := range strings.Split(out, gitLineSeparator) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, treeMoveReflogFieldSep, treeMoveReflogFields)
		if len(fields) != treeMoveReflogFields {
			continue
		}
		epoch, err := treeMoveReflogStamp(fields[0])
		if err != nil {
			continue
		}
		entries = append(entries, treeMoveReflogEntry{
			at:  time.Unix(epoch, 0),
			rev: strings.TrimSpace(fields[1]),
			why: strings.TrimSpace(fields[2]),
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s %s %s printed no reflog rows in %s — the checkout's own record of its movements is absent, so 'the tree moved' cannot be measured",
			gitExecutable, treeMoveReflogVerb, gitHeadRevision, root)
	}
	return entries, nil
}

// treeMoveReflogStamp extracts the epoch from a `HEAD@{<epoch>}` selector.
func treeMoveReflogStamp(selector string) (int64, error) {
	open := strings.LastIndex(selector, treeMoveReflogStampOpen)
	closeAt := strings.LastIndex(selector, treeMoveReflogStampClose)
	if open < 0 || closeAt < open {
		return 0, fmt.Errorf("reflog selector %q carries no {<epoch>} stamp", selector)
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(selector[open+1:closeAt]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reflog selector %q: %w", selector, err)
	}
	return epoch, nil
}

// treeMoveNewestMovement returns the newest entry whose revision DIFFERS from the
// one written just before it — i.e. the movement that produced the revision HEAD
// names now — together with the revision it moved FROM.
//
// A reflog full of identical readings is a checkout that has not moved (a fresh
// clone writes `clone:` and `checkout:` rows that carry the same revision), and
// that answer is a MEASUREMENT, not an absence.
func treeMoveNewestMovement(entries []treeMoveReflogEntry) (to, from treeMoveReflogEntry, ok bool) {
	for i := 0; i+1 < len(entries); i++ {
		if entries[i].rev != entries[i+1].rev {
			return entries[i], entries[i+1], true
		}
	}
	return treeMoveReflogEntry{}, treeMoveReflogEntry{}, false
}

// productionSourcesBetween names the production .go files that differ between two
// revisions of the same repository — the "which commits carried this" half of an
// abstention. An error means the question could not be asked (a revision this
// repository does not hold), and the caller then WITHHOLDS the abstention.
func productionSourcesBetween(root, from, to string) ([]string, error) {
	out, err := gitOutput(root, gitDiffVerb, gitNameOnlyFlag, from, to, gitPathSeparatorFlag, sourceTreeCmdDir, sourceTreeInternalDir)
	if err != nil {
		return nil, err
	}
	return filterProductionSources(root, out), nil
}

// uncommittedProductionSources names the production .go files whose bytes are not
// committed — the ORDINARY shape the freshness guard exists for. A non-empty
// answer VETOES any abstention: a hand edit is a code verdict, whatever the
// checkout's refs did.
//
// The scope is the guard's own (sourceTreeCmdDir + sourceTreeInternalDir, .go
// files that are not tests), read through the same filter, so the two questions
// cannot disagree about which files are "production".
func uncommittedProductionSources(root string) ([]string, error) {
	out, err := gitOutput(root, treeMoveStatusArg, treeMoveStatusPorcelainArg, gitPathSeparatorFlag, sourceTreeCmdDir, sourceTreeInternalDir)
	if err != nil {
		return nil, err
	}
	var dirty []string
	for _, line := range strings.Split(out, gitLineSeparator) {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(strings.TrimSpace(line), treeMoveStatusFieldSep, treeMoveStatusPathFields)
		if len(fields) != treeMoveStatusPathFields {
			continue
		}
		path := strings.TrimSpace(fields[1])
		if !strings.HasSuffix(path, productionSourceSuffix) || strings.HasSuffix(path, testSourceSuffix) {
			continue
		}
		dirty = append(dirty, path)
	}
	return dirty, nil
}

// treeMoveExplainsRefusal decides whether the checkout's OWN record explains a
// content refusal as "the tree moved under this run", and returns the evidence
// when it does.
//
// Every question below is asked of git; none is inferred from the file list. The
// four conditions are stated in the file header, and the LAST of them (no
// uncommitted production change) is what keeps a hand edit a REFUSAL.
func treeMoveExplainsRefusal(root, buildRev string, moved []string, builtAt time.Time) (treeMoveEvidence, bool, error) {
	head, err := gitRevision(root, gitHeadRevision)
	if err != nil {
		return treeMoveEvidence{}, false, err
	}
	if head == buildRev {
		// The checkout's HEAD is still the revision the artifact was built from, so
		// no ref movement can explain the difference — whatever the reflog says, the
		// difference is uncommitted work. This is the minimal reproduction's shape.
		return treeMoveEvidence{}, false, nil
	}
	entries, err := treeMoveReflog(root)
	if err != nil {
		return treeMoveEvidence{}, false, err
	}
	to, from, ok := treeMoveNewestMovement(entries)
	if !ok {
		return treeMoveEvidence{}, false, nil
	}
	// Compared at SECOND granularity, and STRICTLY: git records a reflog entry's
	// time in whole seconds, so a move and a build in the same second are
	// indistinguishable — and the ambiguous case must decide toward the STRONGER
	// conclusion. Treating "same second" as "after the build" would let a move
	// that happened first launder a genuinely stale artifact into an abstention;
	// treating it as "not after" only costs the abstention, leaving a refusal that
	// is by construction about content the binary does not hold.
	if !to.at.After(builtAt.Truncate(time.Second)) {
		// The move predates the build: the artifact was built from a tree that had
		// ALREADY moved, so the difference is a genuinely stale artifact — the
		// guard's own case, and it still refuses.
		return treeMoveEvidence{}, false, nil
	}
	if to.rev != head {
		// The ref moved again after the reading the guard is about to print; the
		// evidence would describe a state this refusal is not about.
		return treeMoveEvidence{}, false, nil
	}
	dirty, err := uncommittedProductionSources(root)
	if err != nil {
		return treeMoveEvidence{}, false, err
	}
	if len(dirty) > 0 {
		return treeMoveEvidence{}, false, nil
	}
	explainedBy, err := productionSourcesBetween(root, buildRev, head)
	if err != nil {
		return treeMoveEvidence{}, false, nil
	}
	explained := map[string]bool{}
	for _, path := range explainedBy {
		explained[path] = true
	}
	for _, path := range moved {
		if !explained[path] {
			// A file differs that no commit between the two revisions touched: the
			// working tree holds something the moves do not account for.
			return treeMoveEvidence{}, false, nil
		}
	}
	return treeMoveEvidence{
		movedAt:     to.at,
		from:        from.rev,
		to:          to.rev,
		commitCount: treeMoveCommitCount(root, from.rev, to.rev),
		commits:     treeMoveCommitSubjects(root, from.rev, to.rev),
		explained:   append([]string{}, moved...),
	}, true, nil
}

// treeMoveFixtureBuildAge is how far BACK the fixture sets the artifact's mtime,
// so its build moment sits in an earlier second than any reflog entry the fixture
// then writes.
//
// WHY THE FIXTURE MOVES THE CLOCK INSTEAD OF WAITING FOR IT: a git reflog records
// whole seconds, so a build and a commit inside the same second are
// indistinguishable, and the guard resolves that ambiguity toward REFUSAL (the
// stronger conclusion). A fixture that slept to cross the boundary would be
// spending wall-clock time to make a measurand cross a threshold — the pattern the
// style gate refuses and Rule 27 forbids in its device form — and it would make
// the arm flaky on a fast machine. Backdating the artifact by a fixed amount
// produces the same measured relation (build SECOND < move SECOND) with no waiting
// at all, and the guard is told nothing: it reads the file's own mtime.
const treeMoveFixtureBuildAge = 2 * time.Minute

// fixtureLdflagRecipeFields is the shape internal/testutil.LdflagsFlag returns: the
// flag and its one operand.
const fixtureLdflagRecipeFields = 2

// reportTreeMoveAbstentionsAtCloseOut prints the suite's tally of guards that
// abstained. It is called from TestMain AFTER m.Run, so it covers every case the
// run reached — including a `-run` group and a whole-package run — and it prints
// the ZERO case too: a run that says "none" is telling the reader the
// measurement was taken, while a run that says nothing leaves "was it taken?"
// unanswerable (Rule 15).
func reportTreeMoveAbstentionsAtCloseOut() {
	if n := treeMoveAbstentionCount(); n > 0 {
		fmt.Fprintf(os.Stderr, treeMoveCountedLine+"\n", n)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", treeMoveCountedNone)
}

// treeMoveAbstention is the guard's ONE call site for the isolation: it returns
// the abstention text when the checkout's own record explains the whole
// difference, and ("", false) otherwise so the caller composes its refusal
// exactly as before.
//
// A question that CANNOT be asked is REPORTED and then behaves like "not
// explained" — the refusal stands (Rule 15: an absent answer is named, never
// dressed up as an answer). That direction is deliberate: the abstention is the
// new, weaker conclusion, so every unreadable record must fall back to the old,
// stronger one.
func treeMoveAbstention(root, buildRev string, moved []string, builtAt time.Time) (string, bool) {
	evidence, ok, err := treeMoveExplainsRefusal(root, buildRev, moved, builtAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "freshness guard: the checkout's movement records could not be read (%v) — the refusal stands and no abstention is granted (Rule 15).\n", err)
		return "", false
	}
	if !ok {
		return "", false
	}
	return evidence.report(root, buildRev, builtAt), true
}

// treeMoveCommitCount counts the commits a move carried, or -1 when git could not
// count them (reported as "unknown", never as zero).
func treeMoveCommitCount(root, from, to string) int {
	out, err := gitOutput(root, treeMoveRevListVerb, treeMoveRevListCountFl, from+treeMoveRangeSep+to)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}
	return n
}

// treeMoveCommitSubjects lists the move's own commit subjects, newest first,
// capped so a refusal stays readable.
func treeMoveCommitSubjects(root, from, to string) []string {
	out, err := gitOutput(root, treeMoveLogVerb, treeMoveLogOnelineArg, treeMoveLogNoDecorArg,
		"-n", strconv.Itoa(treeMoveCommitsShown), from+treeMoveRangeSep+to)
	if err != nil {
		return nil
	}
	var subjects []string
	for _, line := range strings.Split(out, gitLineSeparator) {
		if line = strings.TrimSpace(line); line != "" {
			subjects = append(subjects, line)
		}
	}
	return subjects
}

// report renders the abstention's time facts — what the guard measured, in the
// order a reader needs it: what the artifact is, when it was built, when the
// checkout moved and from where, what the move carried, and which files that
// explains.
func (e treeMoveEvidence) report(bin, buildRev string, builtAt time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", treeMoveAbstainPrefix, bin)
	fmt.Fprintf(&b, "  built from:  %s\n", shortRev(buildRev))
	fmt.Fprintf(&b, "  built at:    %s (the artifact's own file mtime — the moment this run's binary appeared)\n", builtAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "  tree moved:  %s → %s at %s — AFTER the binary was built, so the difference is commits that landed under this run, not an edit to its code\n",
		shortRev(e.from), shortRev(e.to), e.movedAt.Format(time.RFC3339))
	if e.commitCount >= 0 {
		fmt.Fprintf(&b, "  commits:     %s%s%s (%d commit(s))\n", shortRev(e.from), treeMoveRangeSep, shortRev(e.to), e.commitCount)
	} else {
		fmt.Fprintf(&b, "  commits:     %s%s%s (count unknown — git could not count them)\n", shortRev(e.from), treeMoveRangeSep, shortRev(e.to))
	}
	for _, subject := range e.commits {
		fmt.Fprintf(&b, "    %s\n", subject)
	}
	fmt.Fprintf(&b, "  explained:   %d production file(s) differ, ALL of them carried by the commits above: %s\n",
		len(e.explained), strings.Join(e.explained, ", "))
	fmt.Fprintf(&b, "  not the cause: the working tree holds NO uncommitted production change (git status --porcelain -- cmd internal is empty of production .go), so this is not a hand edit to the code under test.\n")
	fmt.Fprintf(&b, "%s\n", treeMoveAbstainHint)
	return b.String()
}

// sortedProductionPaths is the deterministic order every message that lists
// production files uses, so the same tree renders the same sentence.
func sortedProductionPaths(paths []string) []string {
	out := append([]string{}, paths...)
	sort.Strings(out)
	return out
}

// treeMoveFixtureRepo is a git repository holding one committed production source,
// so the reader below can be exercised against a REAL reflog rather than a table.
func treeMoveFixtureRepo(t *testing.T) string {
	t.Helper()
	root := freshnessFixtureRepo(t)
	return root
}

// TestTreeMoveReflogReadsTheMovementsNotTheCommits is the reader's core arm: the
// reflog must yield rows carrying a TIME, and a ref that has never moved must be
// reported as unmoved rather than as unreadable (the two answers differ: one is
// a measurement, the other withholds the abstention).
func TestTreeMoveReflogReadsTheMovementsNotTheCommits(t *testing.T) {
	root := treeMoveFixtureRepo(t)
	entries, err := treeMoveReflog(root)
	if err != nil {
		t.Fatalf("treeMoveReflog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("the fixture's reflog read as empty — a fresh repository has rows")
	}
	for _, entry := range entries {
		if entry.at.IsZero() {
			t.Fatalf("a reflog row carried no timestamp — the move time is the fact the abstention rests on")
		}
		if len(entry.rev) != fullRevisionLen {
			t.Fatalf("a reflog row carried %q; want a %d-hex object name", entry.rev, fullRevisionLen)
		}
	}
	if _, _, moved := treeMoveNewestMovement(entries); moved {
		t.Fatalf("a fixture whose ref never changed was reported as moved — an abstention would be granted for a tree that held still")
	}
}

// TestTreeMoveIsMeasuredAfterTheBuildAndOnlyAfterIt pins the time half: a move
// that happened BEFORE the artifact was built is a stale artifact (the guard's
// own case), and it must NOT explain a refusal.
func TestTreeMoveIsMeasuredAfterTheBuildAndOnlyAfterIt(t *testing.T) {
	root := treeMoveFixtureRepo(t)
	rev := fixtureRevision(t, root)
	bin := fixtureBinary(t, rev)
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat the fixture artifact: %v", err)
	}
	// The fixture's ref moved before the binary existed, so nothing may abstain.
	if _, ok, err := treeMoveExplainsRefusal(root, rev, []string{"internal/agent/second_source.go"}, info.ModTime()); err != nil {
		t.Fatalf("treeMoveExplainsRefusal: %v", err)
	} else if ok {
		t.Fatalf("a move that predates the build was accepted as 'the tree moved under this run' — that is the STALE-ARTIFACT case, which must keep refusing")
	}
}

// TestFreshnessAbstentionResolvesToSkipNeverToPassOrFail is the anti-masquerade
// gate: it pins the mapping the whole epic turns on. An abstention must SKIP — a
// PASS would be a green for a path that never ran, and a FAIL would put the false
// red back; a refusal must still FAIL (an abstention mechanism that quietly
// converted refusals would hide every genuine stale-binary defect), and a fresh
// verdict must still PASS.
func TestFreshnessAbstentionResolvesToSkipNeverToPassOrFail(t *testing.T) {
	cases := []struct {
		name   string
		got    freshnessResolution
		want   freshnessResolution
		reason string
	}{
		{"abstain", freshnessResolutionOf(binFreshness{Abstain: true, Criterion: criterionContent, Refusal: "abstain"}), freshnessSkip,
			"an abstention that resolves to anything else is either a silent green (pass) or the false red this epic removes (fail)"},
		{"refuse", freshnessResolutionOf(binFreshness{Criterion: criterionContent, Refusal: "refuse"}), freshnessFail,
			"a REFUSAL must keep failing — the abstention mechanism may never absorb a stale-binary verdict"},
		{"fresh", freshnessResolutionOf(binFreshness{Fresh: true, Criterion: criterionContent}), freshnessPass,
			"a fresh binary must pass, or every e2e case in this package would skip"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s resolved to %v; want %v — %s", c.name, c.got, c.want, c.reason)
		}
	}
}

// TestFixtureLdflagSpellingMatchesTheBuildRecipe is the anti-regression arm for a
// defect this file found while being written, and it is a GATE, not a comment:
// the freshness fixture built its artifact with the JOINED `-X<path>=<rev>`
// spelling, which the toolchain refuses outright —
//
//	flag provided but not defined: -Xgithub.com/nick/gopi/internal/version.Revision
//
// — so `fixtureBinary` SKIPPED (its own "needs a working Go toolchain" skip) and
// every arm built on it had been proving nothing: a skip reads exactly like a
// host without a toolchain, and no assertion was left to see it. Measured
// 2026-10-31 on this tree: six arms of gopibin_identity_test.go were skipping.
//
// The check compares the fixture's operand with the one the REAL build recipe
// produces (internal/testutil.LdflagsFlag), so the two cannot drift again, and it
// asserts the fixture's binary CARRIES the revision — the fact the joined spelling
// destroyed.
func TestFixtureLdflagSpellingMatchesTheBuildRecipe(t *testing.T) {
	rev := strings.Repeat("a", fullRevisionLen)
	bin := fixtureBinary(t, rev)
	got, ok := binaryBuildRevision(bin)
	if !ok {
		t.Fatalf("the fixture's artifact carries no revision in its %s setting — the build spelling is wrong again, and "+
			"every arm built on this fixture is silently skipping", ldflagsBuildSetting)
	}
	if got != rev {
		t.Fatalf("the fixture's artifact carries revision %q; want the one it was built with (%s)", got, rev)
	}
	recipe := testutil.LdflagsFlag(".")
	if len(recipe) != fixtureLdflagRecipeFields {
		t.Fatalf("internal/testutil.LdflagsFlag no longer returns %d fields (%v) — this gate cannot compare the two spellings",
			fixtureLdflagRecipeFields, recipe)
	}
	if !strings.Contains(recipe[1], " ") {
		t.Fatalf("the build recipe's operand %q no longer separates %q from its path — the fixture's spaced spelling and the "+
			"recipe's have drifted", recipe[1], revisionLdflagKey)
	}
}

// ageTreeMoveFixtureBinary moves the artifact's mtime BACK so its build moment is
// unambiguously earlier than any reflog entry written afterwards. It mutates only
// the fixture's own file, and it is the ONE place the arms below establish that
// relation — a per-arm chtimes call would be three chances to get it wrong.
func ageTreeMoveFixtureBinary(t *testing.T, bin string) time.Time {
	t.Helper()
	builtAt := time.Now().Add(-treeMoveFixtureBuildAge)
	if err := os.Chtimes(bin, builtAt, builtAt); err != nil {
		t.Fatalf("backdate the fixture artifact %s: %v", bin, err)
	}
	return builtAt
}

// treeMoveE2EFixture stands up the shape the whole epic is about, in the smallest
// size that keeps it real: a git repository with a committed production source,
// and a binary built from it BEFORE the tree moves. It returns the repository and
// the artifact's path.
//
// The artifact is a real `go build` because the guard reads its embedded ldflags
// setting; the source it compares is a plain .go file, because the guard compares
// CONTENT. Nothing else about this package is involved, which is the point: the
// decision must hold without a single cmd/agent case running.
func treeMoveE2EFixture(t *testing.T) (root, bin string) {
	t.Helper()
	root = freshnessFixtureRepo(t)
	rev := fixtureRevision(t, root)
	bin = fixtureBinary(t, rev)
	ageTreeMoveFixtureBinary(t, bin)
	// The guard finds the tree by walking UP from the test's cwd to `go.mod`
	// (cwdProbeModuleRoot), so the fixture must be a module root or the guard
	// cannot be asked at all — it would refuse for a reason this file is not
	// measuring. Untracked on purpose: nothing in cmd/ or internal/ is affected by
	// it, so the content comparison the arms rest on is unchanged.
	if err := os.WriteFile(filepath.Join(root, fixtureGoModName), []byte(fixtureGoModBody), 0o644); err != nil {
		t.Fatalf("write %s in the fixture: %v", fixtureGoModName, err)
	}
	return root, bin
}

// TestE2EGuardAbstainsWhenTheCheckoutMovesUnderIt drives `assertGopiBinIsFresh`
// ITSELF — the 21 call sites' only entry point — and asserts the OUTCOME a suite
// reaches: a SKIP carrying the abstention, never a t.Fatalf refusal.
//
// A SKIP is not a pass: `go test -json` records `Action:"skip"` and the case
// never runs, which is exactly the distinction this epic asked for ("make the
// tree-move red an explicit ABSTAIN/SKIP"). Asserting it at the guard (rather
// than at a fixture that calls the decision function) is what proves the 13
// victim tests change behaviour: every one of them reaches this function.
func TestE2EGuardAbstainsWhenTheCheckoutMovesUnderIt(t *testing.T) {
	root, bin := treeMoveE2EFixture(t)
	// The move, exactly as another run's delivery appears: a commit that lands
	// after the artifact was built. treeMoveE2EFixture already backdated the
	// artifact, so the reflog entry this commit writes is in a LATER second by
	// construction — no waiting, nothing to be flaky about.
	if err := os.WriteFile(filepath.Join(root, sourceTreeInternalDir, "agent", fixtureInternalFile),
		[]byte("package probe\n\nvar probeSource = 11\n"), 0o644); err != nil {
		t.Fatalf("write the moving source: %v", err)
	}
	fixtureGit(t, root, "add", filepath.Join(sourceTreeInternalDir, "agent", fixtureInternalFile))
	fixtureGit(t, root, "commit", "-m", "fixture: the move under the run")

	before := treeMoveAbstentionCount()
	t.Cleanup(func() { restoreTreeMoveAbstentionTally(t, before) })
	t.Chdir(root)
	if !guardSkipped(t, bin) {
		t.Fatalf("assertGopiBinIsFresh neither skipped nor refused for a checkout that moved after the build — an " +
			"environment fact must become an explicit ABSTAIN, and the 13 victim tests reach this exact call")
	}
	if got := treeMoveAbstentionCount(); got != before+1 {
		t.Fatalf("the abstention tally read %d → %d; want exactly one increment, so TestMain's close-out can report it", before, got)
	}
}

// TestE2EGuardStillFailsAnUncommittedProductionEdit is the minimal reproduction at
// the SAME entry point: one uncommitted production line, the checkout's HEAD
// untouched, must still produce a REFUSAL — an abstention must never be granted to
// a hand edit, or the guard would let a run verify code that is not in its binary.
func TestE2EGuardStillFailsAnUncommittedProductionEdit(t *testing.T) {
	root, bin := treeMoveE2EFixture(t)
	// The artifact was backdated by the fixture, so the build second is already
	// strictly earlier than anything this arm does: this measures the UNCOMMITTED
	// veto and not a clock relation.
	edited := filepath.Join(root, sourceTreeInternalDir, "agent", fixtureInternalFile)
	if err := os.WriteFile(edited, []byte("package probe\n\nvar probeSource = 12\n"), 0o644); err != nil {
		t.Fatalf("edit %s: %v", edited, err)
	}
	t.Chdir(root)
	before := treeMoveAbstentionCount()
	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if verdict.Fresh || verdict.Abstain {
		t.Fatalf("an UNCOMMITTED production edit produced fresh=%v abstain=%v; want a REFUSAL — the guard would verify "+
			"code that is not in the binary:\n%s", verdict.Fresh, verdict.Abstain, verdict.Refusal)
	}
	if treeMoveAbstentionCount() != before {
		t.Fatalf("the abstention tally moved for a refusal — the two outcomes are not distinct")
	}
}

// TestE2EGuardPassesAnUnmovedCheckoutWithAFreshBinary is the control: the same
// entry point, the same fixture, NO move — the guard must pass, so neither arm
// above can be satisfied by a guard that never passes anything.
func TestE2EGuardPassesAnUnmovedCheckoutWithAFreshBinary(t *testing.T) {
	root, bin := treeMoveE2EFixture(t)
	t.Chdir(root)
	if guardSkipped(t, bin) {
		t.Fatalf("an unmoved checkout with a FRESH artifact abstained — an abstention that fires on a clean tree hides " +
			"every green it touches")
	}
}

// guardSkipped runs assertGopiBinIsFresh — the ONE entry point all 21 call sites
// use — inside a real subtest and reports whether it ABSTAINED (skipped).
//
// This direction is the observable one: a SKIP does not fail its parent, so the
// abstention can be asserted end to end. The other two outcomes cannot be
// observed this way through t.Run (a t.Fatalf marks the PARENT failed too, so a
// passing assertion would be indistinguishable from the failure it is asserting
// about), and they are therefore asserted at decideBinFreshness — with the
// abstention TALLY as the cross-check that the skip path did not fire, which is
// what makes a refusal distinguishable from a silent abstention.
func guardSkipped(t *testing.T, bin string) bool {
	t.Helper()
	reached := false
	t.Run("guard", func(t *testing.T) {
		assertGopiBinIsFresh(t, bin)
		reached = true
	})
	return !reached
}

// TestFreshnessAbstainsWhenCommitsLandedAfterTheBuild is the arm the whole file
// exists for: the artifact is built, the checkout then MOVES (a real commit
// touching a production source, which is what another worker's push looks like),
// and the guard ABSTAINS with the time facts instead of reporting a code verdict.
//
// The fixture is the real machinery — a real repository, a real commit, a real
// reflog, a real binary — because the decision rests on a TIMESTAMP the kernel
// writes, and a table-driven stub would prove nothing about it.
func TestFreshnessAbstainsWhenCommitsLandedAfterTheBuild(t *testing.T) {
	root := freshnessFixtureRepo(t)
	rev := fixtureRevision(t, root)
	bin := fixtureBinary(t, rev)
	// Backdate the artifact so its build second is strictly earlier than the
	// reflog entry the move below writes — the relation this arm measures.
	ageTreeMoveFixtureBinary(t, bin)

	// The move: ONE commit touching a production source, the way another run's
	// delivery lands under an in-flight suite. The committed content differs from
	// what the binary holds, which is exactly the state the guard refuses on.
	moved := filepath.Join(root, sourceTreeInternalDir, "agent", fixtureInternalFile)
	if err := os.WriteFile(moved, []byte("package probe\n\n// the move this arm is about.\nvar probeSource = 3\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", moved, err)
	}
	fixtureGit(t, root, "add", filepath.Join(sourceTreeInternalDir, "agent", fixtureInternalFile))
	fixtureGit(t, root, "commit", "-m", "fixture: the commit that lands under the run")

	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if verdict.Fresh {
		t.Fatalf("a production source the binary does not hold was called fresh")
	}
	if !verdict.Abstain {
		t.Fatalf("commits landed AFTER the artifact was built and the guard still refused — the tree-move isolation is not wired:\n%s", verdict.Refusal)
	}
	for _, want := range []string{
		treeMoveAbstainPrefix,
		"built from:  " + shortRev(rev),
		"tree moved:",
		"explained:",
		"fixture: the commit that lands under the run",
	} {
		if !strings.Contains(verdict.Refusal, want) {
			t.Errorf("the abstention does not carry %q:\n%s", want, verdict.Refusal)
		}
	}
	if verdict.Criterion != criterionContent {
		t.Errorf("deciding criterion = %q; want %q (an abstention is granted on the content arm, not on mtime or build identity)",
			verdict.Criterion, criterionContent)
	}
}

// TestFreshnessStillRefusesAnUncommittedProductionEdit is the MINIMAL
// REPRODUCTION, pinned: one uncommitted line in a production source, with the
// checkout's HEAD untouched, must still refuse — an abstention must never be
// granted to a hand edit.
func TestFreshnessStillRefusesAnUncommittedProductionEdit(t *testing.T) {
	root := freshnessFixtureRepo(t)
	rev := fixtureRevision(t, root)
	bin := fixtureBinary(t, rev)

	edited := filepath.Join(root, sourceTreeInternalDir, "agent", fixtureInternalFile)
	if err := os.WriteFile(edited, []byte("package probe\n\nvar probeSource = 7\n"), 0o644); err != nil {
		t.Fatalf("edit %s: %v", edited, err)
	}
	dirty, err := uncommittedProductionSources(root)
	if err != nil {
		t.Fatalf("uncommittedProductionSources: %v", err)
	}
	if len(dirty) == 0 {
		t.Fatalf("the uncommitted edit was not seen by the status reading — the veto would never fire")
	}
	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("decideBinFreshness: %v", err)
	}
	if verdict.Abstain {
		t.Fatalf("an UNCOMMITTED production edit abstained instead of refusing — the guard would verify code that is not in the binary:\n%s", verdict.Refusal)
	}
	if verdict.Fresh {
		t.Fatalf("an uncommitted production edit was treated as present in the binary")
	}
}

// TestTreeMoveAbstentionTalliesWhatItGranted pins the close-out wiring: an
// abstention increments a counter TestMain reads, so a package that read green
// while its e2e guards abstained says so out loud instead of passing in silence.
func TestTreeMoveAbstentionTalliesWhatItGranted(t *testing.T) {
	before := treeMoveAbstentionCount()
	t.Cleanup(func() { restoreTreeMoveAbstentionTally(t, before) })
	noteTreeMoveAbstention()
	if after := treeMoveAbstentionCount(); after != before+1 {
		t.Fatalf("the abstention tally read %d → %d; want a single increment", before, after)
	}
	if !strings.Contains(treeMoveCountedLine, "%d") {
		t.Fatalf("the close-out line carries no count — a reader could not tell 1 abstention from 13")
	}
	if strings.Contains(treeMoveCountedLine, "passing") && !strings.Contains(treeMoveCountedLine, "NOT passing") && !strings.Contains(treeMoveCountedLine, "not passing") {
		t.Fatalf("the close-out line does not deny that an abstention is a pass")
	}
}

// TestTreeMoveRefusalMessageCarriesTheTimeFacts pins the evidence the abstention
// must print: the build revision, the build moment, the move, and the files it
// explains. A message missing any of them is the refusal this file exists to
// replace.
func TestTreeMoveRefusalMessageCarriesTheTimeFacts(t *testing.T) {
	evidence := treeMoveEvidence{
		movedAt:     time.Date(2026, 10, 31, 7, 30, 0, 0, time.UTC),
		from:        "347579b26f9c0000000000000000000000000000",
		to:          "28e6a2b031cd0000000000000000000000000000",
		commitCount: 3,
		commits:     []string{"abc1234 fix(x): one", "def5678 chore(y): two"},
		explained:   sortedProductionPaths([]string{"cmd/agent/merge_run.go", "internal/config/agentguard.go"}),
	}
	builtAt := time.Date(2026, 10, 31, 7, 20, 0, 0, time.UTC)
	msg := evidence.report("/scratch/gopi", evidence.from, builtAt)
	for _, want := range []string{
		treeMoveAbstainPrefix,
		"built from:  " + shortRev(evidence.from),
		builtAt.Format(time.RFC3339),
		evidence.movedAt.Format(time.RFC3339),
		"3 commit(s)",
		"cmd/agent/merge_run.go",
		"NO uncommitted production change",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the abstention does not carry %q:\n%s", want, msg)
		}
	}
}
