package tools

// The PUBLISHED-ANCHOR gate's REFUSAL path, tested where it decides.
//
// WHY THIS FILE EXISTS. `merge_anchor_gate_test.go` was retired with the merge
// route (`5a9af79b`, 95 test files) and took the gate's whole refusal path with
// it: `mergeAnchorGateBlamesBranch`, `mergeAnchorGateAnchorRef`,
// `mergeAnchorGateDetail` and the entire `task_merge_refuse_anchor.go` file sat
// at 0.0% — a P0 merge gate whose REFUSAL could stop firing and the suite would
// stay green (the shape a rule forbids: a named mechanism nothing measures). The
// union-fixture arms (`TestMergeAnchorGate_RefusesAStaleDocumentInTheUnion` and
// its two siblings) could not simply be restored either: they declare
// `heavytest.CriterionDeviceExec`, and a heavy run is REFUSED inside a work
// context (`internal/heavytest/boundary.go`), so an arm that needs them is an
// arm that does not run here. What CAN be measured here is the decision layer
// itself, and it is the layer that turns a red document into a refusal: the
// extraction that reads an anchor out of a finding, the attribution that decides
// whether the branch is to blame, the sentence the operator reads, and the
// archive + rework hand-off that closes a refusal.
//
// WHAT IS NOT COVERED HERE, stated rather than implied (Rule 15): the union
// measurement itself — `mergeAnchorGate`'s REFUSE arm on a real union fixture,
// its control arm, and its red proof. Those three need a real toolchain run over
// a materialised union tree and are heavy by construction; they are exercised
// only under `make test-heavy` from an OPERATOR terminal, and no arm below
// claims otherwise.
//
// THE FIXTURE IS THE INCIDENT'S EVIDENCE, not a synthetic string: a document
// test reports a stale anchor by quoting the `path.go:<identity>` it disagrees
// about, in BOTH identity shapes the published block has carried (a symbol name
// since 2026-10-12, a line number before it), and the attribution reads exactly
// that. Both shapes are asserted, because a reader narrowed back to `\d+`
// extracts NOTHING from a symbol finding — and an empty extraction blames
// nobody, which turns the refusal into an abstention (the defect `6507aec`
// repaired one layer down).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliindex"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// magrBranch is the candidate the refusal lanes refuse.
	magrBranch = "feat/stale-published-anchor"
	// magrAnchorSrc is the anchor SOURCE the branch changed: the file the
	// document's reference points into.
	magrAnchorSrc = "internal/maganchor/thing.go"
	// magrOtherSrc is a file the branch never touched — the negative arm's own
	// path, so "blames nobody" is measured against a real-looking anchor rather
	// than against prose.
	magrOtherSrc = "internal/other/thing.go"
	// magrIdentity is the SYMBOL identity the published block carries today.
	magrIdentity = "isThingSubcommand"
	// magrLegacyLine is the LEGACY identity shape an unregenerated span still
	// carries; the extraction must read both.
	magrLegacyLine = "18"
	// magrProse is a finding with no reference at all — a runner that failed
	// without naming a file (the caller must blame nobody).
	magrProse = "the document tests failed without naming a failing test"
	// magrDetail is the sentence a refusal carries into the rework brief.
	magrDetail = "the MERGE RESULT fails the published-anchor document tests"
	// magrMandate is the edge's own sentence about the document.
	magrMandate = "COMMANDS.md is the tree's generated document of record"
)

// magrSymbolRef / magrLegacyRef / magrForeignRef are the three anchor shapes the
// arms below feed the extraction — spelled once so an arm cannot drift from the
// shape it claims to read.
var (
	magrSymbolRef  = magrAnchorSrc + ":" + magrIdentity
	magrLegacyRef  = magrAnchorSrc + ":" + magrLegacyLine
	magrForeignRef = magrOtherSrc + ":" + magrIdentity
)

// TestMergeAnchorGateRefusal_AnchorRefExtraction pins the EXTRACTION contract,
// and it is the arm that turns red the moment the identity alternation narrows
// back to `\d+`: every symbol-addressed finding would extract nothing, and no
// downstream arm can tell "the branch is innocent" from "the reader saw
// nothing".
func TestMergeAnchorGateRefusal_AnchorRefExtraction(t *testing.T) {
	cases := []struct {
		name    string
		finding string
		want    []string
	}{
		{
			name:    "a symbol identity is extracted, in repo-relative form",
			finding: "the document says " + magrSymbolRef + " is the declaration",
			want:    []string{magrAnchorSrc},
		},
		{
			name:    "a legacy line identity is still extracted",
			finding: "the document says " + magrLegacyRef + " is the declaration",
			want:    []string{magrAnchorSrc},
		},
		{
			name:    "both identity shapes in ONE finding yield both references",
			finding: "see " + magrLegacyRef + " and " + magrSymbolRef,
			want:    []string{magrAnchorSrc, magrAnchorSrc},
		},
		{
			name:    "a leading ./ is trimmed (the changed-path set is repo-relative)",
			finding: "the document says ./" + magrSymbolRef + " is the declaration",
			want:    []string{magrAnchorSrc},
		},
		{
			name:    "a finding that quotes no reference extracts nothing",
			finding: magrProse,
			want:    nil,
		},
		{
			name:    "a non-Go path is not an anchor",
			finding: "the document says internal/maganchor/thing.md:" + magrIdentity + " is stale",
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeAnchorGateAnchorRef(tc.finding)
			if len(got) != len(tc.want) {
				t.Fatalf("extracted %q from %q, want %q — an empty extraction blames nobody, so the lane "+
					"abstains instead of refusing", got, tc.finding, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("ref[%d] = %q, want %q (got %q)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestMergeAnchorGateRefusal_PatternUsesTheOneExportedSpelling is the ANTI-DRIFT
// arm, and it is the half of the retired `TestMergeAnchorGateReadsSymbolAnchors`
// that can come back as written: the compiled pattern must carry
// `cliindex`'s exported separator AND identity alternation verbatim. A second,
// hand-typed alternation beside the renderer's is the defect both `6507aec` and
// this gate's own history are made of — two spellings that are green apart and
// disagree in production (Rule 14: one definition per shared value).
func TestMergeAnchorGateRefusal_PatternUsesTheOneExportedSpelling(t *testing.T) {
	src := mergeAnchorGateAnchorPattern.String()
	if !strings.Contains(src, cliindex.AnchorIdentityPatternSrc) {
		t.Fatalf("the anchor pattern does not carry cliindex.AnchorIdentityPatternSrc (%q): %q",
			cliindex.AnchorIdentityPatternSrc, src)
	}
	if !strings.Contains(src, cliindex.AnchorSep) {
		t.Fatalf("the anchor pattern does not carry cliindex.AnchorSep (%q): %q", cliindex.AnchorSep, src)
	}
	// The executable form of the same claim: the shape the document publishes
	// must MATCH — a pattern built from the right pieces but anchored wrongly is
	// still an extraction that returns nothing.
	if !mergeAnchorGateAnchorPattern.MatchString(magrSymbolRef) {
		t.Fatalf("the compiled pattern does not match the shape the document publishes (%q): %q", magrSymbolRef, src)
	}
}

// TestMergeAnchorGateRefusal_BlamesBranch pins the ATTRIBUTION contract in both
// directions. It is what makes a refusal a statement about the branch it refuses
// rather than about whatever else the target left red — measured 2026-10-13: the
// integration ref the lane was written against was itself red, so an
// unattributed refusal lands on the first branch offered after such a state.
func TestMergeAnchorGateRefusal_BlamesBranch(t *testing.T) {
	touched := map[string]bool{magrAnchorSrc: true}
	cases := []struct {
		name     string
		findings []string
		touched  map[string]bool
		want     bool
	}{
		{
			name:     "a symbol anchor in a file the branch changed blames it",
			findings: []string{"the document says " + magrSymbolRef + " is the declaration"},
			touched:  touched,
			want:     true,
		},
		{
			name:     "a legacy line anchor in a file the branch changed blames it",
			findings: []string{"the document says " + magrLegacyRef + " is the declaration"},
			touched:  touched,
			want:     true,
		},
		{
			name:     "an anchor in a file the branch never touched blames nobody",
			findings: []string{"the document says " + magrForeignRef + " is the declaration"},
			touched:  touched,
			want:     false,
		},
		{
			name:     "a finding quoting no reference blames nobody",
			findings: []string{magrProse},
			touched:  touched,
			want:     false,
		},
		{
			name:     "a changed path that is not an anchor does not blame",
			findings: []string{"the document says " + magrSymbolRef + " is the declaration"},
			touched:  map[string]bool{"internal/unrelated/other.go": true},
			want:     false,
		},
		{
			name:     "one blaming finding among several is enough",
			findings: []string{magrProse, "the document says " + magrForeignRef + " is stale", "and " + magrSymbolRef},
			touched:  touched,
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeAnchorGateBlamesBranch(tc.findings, tc.touched); got != tc.want {
				t.Fatalf("blamesBranch = %v, want %v (findings=%q touched=%v)", got, tc.want, tc.findings, tc.touched)
			}
		})
	}
}

// TestMergeAnchorGateRefusal_DetailContract pins the refusal's SENTENCE: it names
// the branch and the target, the packages that were measured, and the document's
// own findings — capped, with the tally echoed, because a rework brief must stay
// a brief and a silent cap would misreport how wide the failure is (Rule 15).
func TestMergeAnchorGateRefusal_DetailContract(t *testing.T) {
	packages := []string{"internal/magpkg", "internal/magpkg/sub"}
	t.Run("the refusal names the branch, the target and the measured packages", func(t *testing.T) {
		detail := mergeAnchorGateDetail(magrBranch, masterBranchName, packages, []string{magrProse})
		for _, want := range []string{magrBranch, masterBranchName, strings.Join(packages, mergeAnchorGateFindingSep), magrProse} {
			if !strings.Contains(detail, want) {
				t.Fatalf("the refusal must carry %q:\n%s", want, detail)
			}
		}
	})
	t.Run("findings under the cap are quoted whole and carry no tally", func(t *testing.T) {
		findings := []string{"first", "second"}
		detail := mergeAnchorGateDetail(magrBranch, masterBranchName, packages, findings)
		for _, want := range findings {
			if !strings.Contains(detail, want) {
				t.Fatalf("finding %q was dropped under the cap:\n%s", want, detail)
			}
		}
		if strings.Contains(detail, "more)") {
			t.Fatalf("a refusal that quoted every finding must not report a tally:\n%s", detail)
		}
	})
	t.Run("the cap bites with the exact count and never the whole list", func(t *testing.T) {
		overflow := 3
		findings := make([]string, 0, mergeAnchorGateFindingLimit+overflow)
		for i := 0; i < mergeAnchorGateFindingLimit+overflow; i++ {
			findings = append(findings, "finding-"+string(rune('a'+i)))
		}
		detail := mergeAnchorGateDetail(magrBranch, masterBranchName, packages, findings)
		tally := fmt.Sprintf(mergeAnchorGateFindingMoreFmt, overflow)
		if !strings.Contains(detail, tally) {
			t.Fatalf("the cap must report how many findings it withheld (%q):\n%s", tally, detail)
		}
		if strings.Contains(detail, findings[len(findings)-1]) {
			t.Fatalf("a finding past the cap was quoted anyway (%q):\n%s", findings[len(findings)-1], detail)
		}
		for _, kept := range findings[:mergeAnchorGateFindingLimit] {
			if !strings.Contains(detail, kept) {
				t.Fatalf("a finding WITHIN the cap was dropped (%q):\n%s", kept, detail)
			}
		}
	})
	t.Run("findings and packages are joined with the single-line separator", func(t *testing.T) {
		// THE JOINER IS THE CONTRACT (not the findings' own text: a finding is a
		// test's output and may itself span lines). What must hold is that the
		// refusal never SEPARATES two items with a comma or a newline — the
		// merger's log line and the row's detail field are single-line consumers,
		// and `mergeAnchorGateFindingSep` is the one spelling of that join.
		detail := mergeAnchorGateDetail(magrBranch, masterBranchName, packages, []string{"first", "second"})
		if want := "first" + mergeAnchorGateFindingSep + "second"; !strings.Contains(detail, want) {
			t.Fatalf("findings must be joined by %q:\n%s", mergeAnchorGateFindingSep, detail)
		}
		if !strings.Contains(detail, strings.Join(packages, mergeAnchorGateFindingSep)) {
			t.Fatalf("packages must be joined by %q:\n%s", mergeAnchorGateFindingSep, detail)
		}
	})
}

// TestMergeAnchorGateRefusal_DisabledAbstains pins the operator escape hatch: the
// gate reads ONE env value and `0` alone turns it into an abstention. It is the
// RED PROOF's switch — the union arms that depend on it are heavy, but the switch
// itself is a two-line contract and an unmeasured switch is a switch nobody can
// trust.
func TestMergeAnchorGateRefusal_DisabledAbstains(t *testing.T) {
	t.Setenv(mergeAnchorGateEnvName, "")
	if mergeAnchorGateDisabled() {
		t.Fatal("an UNSET switch must leave the gate enabled")
	}
	t.Setenv(mergeAnchorGateEnvName, mergeAnchorGateDisabledValue)
	if !mergeAnchorGateDisabled() {
		t.Fatalf("the documented off value %q must disable the gate", mergeAnchorGateDisabledValue)
	}
	t.Setenv(mergeAnchorGateEnvName, "1")
	if mergeAnchorGateDisabled() {
		t.Fatal("a value that is not the documented off value must leave the gate enabled")
	}
}

// TestTaskMergeRefuseAnchor_ArchivesAndNotifies is the REFUSAL HAND-OFF, driven
// for real on a real repository and a real ledger: the branch leaves the queue
// as a reasoned archive tag, master is not touched, and exactly ONE branch-rework
// row carries what a reworker cannot reconstruct (the findings and the edge's
// mandate). This is `task_merge_refuse_anchor.go`'s whole job, and until now its
// three functions were executed by nothing.
func TestTaskMergeRefuseAnchor_ArchivesAndNotifies(t *testing.T) {
	repo := magrTestRepo(t)
	testutil.NewTestDBPath(t) // the refusal writes a ledger row: it needs a ledger

	findings := []string{"the document says " + magrSymbolRef + " is the declaration, but that line reads a comment"}
	outcome := taskMergeRefuseAnchor(repo, masterBranchName, magrBranch, magrDetail, findings, magrMandate)

	if outcome.Action != mergeBuildGateActionRefused {
		t.Fatalf("Action = %q, want %q (outcome=%+v)", outcome.Action, mergeBuildGateActionRefused, outcome)
	}
	if !outcome.BuildNotified {
		t.Fatalf("a submitted rework row must be reported as notified: %+v", outcome)
	}
	tag := archiveBranchPrefix + magrBranch
	if !strings.Contains(outcome.Detail, tag) {
		t.Fatalf("the outcome must name the archive tag %q: %s", tag, outcome.Detail)
	}
	// THE ARCHIVE: a tag AND its WHY record — never a bare `branch -d`, so a
	// later reader can answer "who decided this, and why".
	if got := strings.TrimSpace(magrGit(t, repo, "tag", "-l", tag)); got != tag {
		t.Fatalf("archive tag %q missing — a refused branch must leave the queue", tag)
	}
	reason := strings.TrimSpace(magrGit(t, repo, "cat-file", "blob", archiveReasonRefPrefix+magrBranch))
	if !strings.Contains(reason, ArchiveReasonPublishedAnchor) {
		t.Fatalf("the recorded WHY must name this lane's reason %q: %s", ArchiveReasonPublishedAnchor, reason)
	}
	if !repoSweepArchiveExempt(repo, magrBranch) {
		t.Fatalf("the archived branch must be exempt from every merge path")
	}
	// THE ROW: exactly one, claimable, and carrying the brief's two
	// unreconstructable elements.
	summary := resolveRowReworkSummaryPrefix + magrBranch
	if n := magrCountRows(t, `SELECT COUNT(*) FROM tasks WHERE summary = ?`, summary); n != 1 {
		t.Fatalf("branch-rework rows = %d, want exactly 1", n)
	}
	payload, status := magrReworkRow(t, magrBranch)
	if status != taskStatusPending {
		t.Fatalf("row status = %q, want %q (claimable)", status, taskStatusPending)
	}
	for _, want := range []string{magrBranch, masterBranchName, "refs/tags/" + tag, magrMandate, findings[0]} {
		if !strings.Contains(payload, want) {
			t.Fatalf("the rework payload is missing %q:\n%s", want, payload)
		}
	}

	// THE DEDUP ARM: one branch, ONE rework row, however often it is refused —
	// the second call must report the duplicate instead of minting a second row.
	again := taskMergeRefuseAnchor(repo, masterBranchName, magrBranch, magrDetail, findings, magrMandate)
	if again.BuildNotified {
		t.Fatalf("a duplicate refusal must not report a fresh notification: %+v", again)
	}
	if n := magrCountRows(t, `SELECT COUNT(*) FROM tasks WHERE summary = ?`, summary); n != 1 {
		t.Fatalf("a repeated refusal minted %d rows, want 1", n)
	}
}

// TestTaskMergeRefuseAnchor_FinishedOwnerIsNotBuried is the refusal's OWNER arm,
// and it is the 21:04 incident's lesson applied to this lane: the archive step is
// the SHARED writer (`repoSweepArchiveBranch`), so when the ledger says a row has
// FINISHED over this branch the archive is refused, the rework row is NOT minted,
// and the outcome says so — a refusal must never bury a finished deliverable
// under an exclusion tag nothing mentions again.
func TestTaskMergeRefuseAnchor_FinishedOwnerIsNotBuried(t *testing.T) {
	repo := magrTestRepo(t)
	testutil.NewTestDBPath(t)
	archSeedRow(t, "magr-finished-owner", TaskStatusCompleted, magrBranch)

	outcome := taskMergeRefuseAnchor(repo, masterBranchName, magrBranch, magrDetail,
		[]string{"the document says " + magrSymbolRef + " is stale"}, magrMandate)

	if outcome.BuildNotified {
		t.Fatalf("an archive the owner gate refused must not report a notification: %+v", outcome)
	}
	if !strings.Contains(outcome.Detail, "archive failed") {
		t.Fatalf("the outcome must state the refused archive (Rule 15): %s", outcome.Detail)
	}
	if got := strings.TrimSpace(magrGit(t, repo, "tag", "-l", archiveBranchPrefix+magrBranch)); got != "" {
		t.Fatalf("a refused archive wrote tag %q anyway", got)
	}
	if n := magrCountRows(t, `SELECT COUNT(*) FROM tasks WHERE summary = ?`, resolveRowReworkSummaryPrefix+magrBranch); n != 0 {
		t.Fatalf("a refused archive minted %d rework rows, want 0", n)
	}
}

// TestTaskMergeRefuseAnchor_NoMandateIsStatedNotSilent pins the brief's element 3
// when the edge rendered no mandate: the reworker must read a STATED absence, not
// an empty element it would interpret as "no document is involved" (Rule 15).
func TestTaskMergeRefuseAnchor_NoMandateIsStatedNotSilent(t *testing.T) {
	repo := magrTestRepo(t)
	testutil.NewTestDBPath(t)

	outcome := taskMergeRefuseAnchor(repo, masterBranchName, magrBranch, magrDetail,
		[]string{"the document says " + magrSymbolRef + " is stale"}, "")
	if outcome.Action != mergeBuildGateActionRefused {
		t.Fatalf("Action = %q, want %q", outcome.Action, mergeBuildGateActionRefused)
	}
	payload, _ := magrReworkRow(t, magrBranch)
	if !strings.Contains(payload, anchorGateMandateMissingNote) {
		t.Fatalf("the brief must state the missing mandate, not leave the element empty:\n%s", payload)
	}
}

// TestTaskMergeRefuseAnchor_ArchiveFailureKeepsTheBranch is the refusal's FAILURE
// arm, and it is the one that keeps a broken archive from silently consuming a
// branch: when the archive cannot be written, no rework row is minted and the
// outcome says so (the branch stays a candidate for a later tick).
func TestTaskMergeRefuseAnchor_ArchiveFailureKeepsTheBranch(t *testing.T) {
	repo := magrTestRepo(t)
	testutil.NewTestDBPath(t)

	missing := "feat/no-such-branch"
	outcome := taskMergeRefuseAnchor(repo, masterBranchName, missing, magrDetail,
		[]string{"the document says " + magrSymbolRef + " is stale"}, magrMandate)

	if outcome.Action != mergeBuildGateActionRefused {
		t.Fatalf("Action = %q, want %q", outcome.Action, mergeBuildGateActionRefused)
	}
	if outcome.BuildNotified {
		t.Fatalf("an archive failure must not report a notification: %+v", outcome)
	}
	if !strings.Contains(outcome.Detail, "archive failed") {
		t.Fatalf("the outcome must state the archive failure (Rule 15): %s", outcome.Detail)
	}
	if n := magrCountRows(t, `SELECT COUNT(*) FROM tasks WHERE summary = ?`, resolveRowReworkSummaryPrefix+missing); n != 0 {
		t.Fatalf("an archive failure minted %d rework rows, want 0", n)
	}
}

// --- fixture -----------------------------------------------------------------

// magrTestRepo builds a throwaway repository with master and one candidate
// branch that carries a real commit (the archive lane tags a REF, so the branch
// must exist in a real repo — mocking git here would test nothing).
func magrTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	magrGit(t, repo, "init", "-q", "-b", masterBranchName)
	magrGit(t, repo, "config", "user.email", "t@t")
	magrGit(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	magrGit(t, repo, "add", "README.md")
	magrGit(t, repo, "commit", "-q", "-m", "seed")
	magrGit(t, repo, "checkout", "-q", "-b", magrBranch)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\ncandidate\n"), 0o644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	magrGit(t, repo, "commit", "-q", "-am", "candidate work")
	magrGit(t, repo, "checkout", "-q", masterBranchName)
	return repo
}

// magrGit runs git -C repo and fails the test on error.
//
// THE ENVIRONMENT IS THE FIXTURE'S, NEVER THE CALLER'S (testutil.GitEnv strips
// every repository-routing variable): a child that inherits GIT_DIR / GIT_WORK_TREE
// from whatever shell or clone spawned the test measures THAT repository instead of
// the fixture — green where the author ran it, red on every clone branch.
func magrGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = testutil.GitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// magrCountRows reads one scalar from the run's ledger (the dedup assertion).
func magrCountRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("close ledger: %v", cerr)
		}
	}()
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}

// magrReworkRow reads the branch-rework row's payload and status for branch.
func magrReworkRow(t *testing.T, branch string) (payload, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("close ledger: %v", cerr)
		}
	}()
	row := db.QueryRowContext(ctx,
		`SELECT payload, status FROM tasks WHERE summary = ?`, resolveRowReworkSummaryPrefix+branch)
	if err := row.Scan(&payload, &status); err != nil {
		t.Fatalf("no branch-rework row for %s: %v", branch, err)
	}
	return payload, status
}
