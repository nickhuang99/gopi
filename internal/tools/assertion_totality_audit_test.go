package tools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── the assertion-totality audit gate ───────────────────────────────────────
//
// WHY THIS FILE EXISTS (devdoc/assert-the-bug-audit-20260917.md §5): the
// 2026-09-16 push/dirty incident was not a missing guard, it was a TEST that
// pinned the WRONG SPECIFICATION. task_finish_cleanup_test.go asserted "no
// steps may run on a dirty worktree" — a claim TRUE of the destructive steps
// (③ worktree remove --force, ④ branch -d) and FALSE of step ② (an
// idempotent push that only moves a PEER ref). The bundle let a single
// untracked scratch file skip the whole close-out chain, stranding finished
// branches on a client host while the ledger said `completed`.
//
// The defect shape is therefore: a NEGATIVE ASSERTION OVER A SET OF STEPS
// ("no step ran") whose MESSAGE names a behaviour with an action verb. The
// action verb is the tell: it claims *nothing happened*, when the correct
// claim is *nothing destructive happened*.
//
// This gate turns "is this assertion right?" into a repeatable check: every
// such assertion must be CLASSIFIED with a reason (auditTotalityAllow). An
// unclassified one fails the build, so a future agent writing the same
// over-broad shorthand meets the question at the moment of writing it. It is
// the Rule 16 idiom (deterministic predicate, cheap stdlib scan, green on the
// day it lands) applied to the audit family the epic is about.
//
// Limitation, stated up front (devdoc §6): the predicate finds NEGATIVE
// TOTALITY assertions. The incident's root shape — a bundle that should be a
// split — is broader: a positive assertion can bundle two claims just as
// easily, and no AST predicate sees "this expected value is wrong". The gate
// mechanises the shape that is mechanically visible and hands the rest to a
// review checklist; it never claims to certify correctness.
//
// House rule this file follows: it lives in internal/tools (test-only, no
// production code) rather than internal/standards, because this epic runs on a
// FEATURE host (Rule 28 — internal/standards gates are the system layer).
// Promotion to internal/standards is a follow-up, deliberately not done here.
const (
	auditScanRootInternal = "internal"
	auditScanRootCmd      = "cmd"
	auditTestSuffix       = "_test.go"
	auditGateModName      = "go.mod"

	// auditAllowMinReasonLen is the read-floor for a classification: a reason
	// shorter than this is a rubber stamp, not a decision (the Rule 16
	// "explicitly reasoned" convention, made measurable).
	auditAllowMinReasonLen = 25

	// auditInlineAllowToken is the inline escape for a site whose totality is
	// correct and obvious at the line (same spirit as //stylegate:ignore).
	auditInlineAllowToken = "audit-allow:"

	// auditWhyToken is the OTHER inline escape — the site-level carrier of the
	// classification: a `why:` comment beside the assertion, which
	// auditSiteWhy reads and auditClassified accepts in place of an
	// auditTotalityAllow entry.
	//
	// WHY IT EXISTS (root-caused 2026-09-28, on master's own run of this gate).
	// The gate is an ASSERTION-TOTALITY audit, but when it landed (0635d320,
	// 2026-09-17) only ONE carrier was mechanised: a classification had to be a
	// map entry keyed "path:TestFunc". A reason written at the SITE — where the
	// person editing that assertion reads it — was therefore invisible to the
	// scan, and the gate reported such a site UNCLASSIFIED while printing "add:
	// auditTotalityAllow[...]" as its remedy: it asked for a SECOND copy of a
	// decision already written one line above the assertion. That is a false
	// red, and the repo's own `why:` convention across the test trees is
	// exactly the shape that trips it.
	//
	// It WIDENS NOTHING the gate refuses (measured both ways by
	// TestAssertionTotalityAuditGateSiteWhyRedProof): a site with neither
	// carrier still fails, a reason below auditAllowMinReasonLen still fails at
	// either carrier, a bare `why:` with nothing behind it fails, and a
	// non-comment `why:` (a case table's struct field) classifies nothing.
	auditWhyToken = "why:"

	// ── the scan's SOURCE ANCHOR (root-caused 2026-10-14) ───────────────────
	//
	// auditRepoRoot used to walk up from the test's WORKING DIRECTORY to the
	// module root and walk internal/ + cmd/ from there. That made the gate's
	// verdict a function of WHERE THE PROCESS HAPPENED TO STAND, and under the
	// whole-package run (`go test ./internal/tools/`) it is not the source tree
	// at all: cwd is internal/tools, whose parent — internal/ — carries NO
	// go.mod, so the walk climbed to the REPOSITORY ROOT, which IS a module
	// root (`go.mod` there). From a task worktree that is a different checkout
	// than the one this binary was built from, so the gate scanned a SIBLING
	// RUN's tree — and once a concurrent run's own scratch probe (a zz_*_test.go
	// it was writing or deleting at that instant) vanished between the directory
	// read and the open, os.ReadFile returned ENOENT, the WalkDir callback
	// returned it, and the whole gate went red ("scan …: … no such file or
	// directory") with nothing wrong in the tree it was supposed to be reading.
	//
	// The anchor is now the file's OWN location: runtime.Caller on this very
	// function. The path is compiled in, so it is true from any cwd and names
	// the tree this binary was BUILT from — never a sibling worktree.
	//
	// trimpath caveat, stated rather than assumed: `-trimpath`, which the
	// release build (inside container/deploy.sh's `podman build`) passes,
	// rewrites the recorded path to a module-relative one
	// ("github.com/nickhuang99/gopi/internal/tools/…"), so it no longer resolves on
	// disk. A path that does not name a real directory is REFUSED as an anchor
	// and the search FALLS BACK to the walking contract below — announced, so
	// the degradation is visible (Rule 15) instead of silently selecting
	// another tree. The fallback is reachable (pinned by
	// TestAssertionTotalityAuditScanRootFallsBackToTheWalkingContract) and is
	// what keeps the test suite honest under a trimmed build.
	auditAnchorFile         = "assertion_totality_audit_test.go"
	auditSourceAnchorFrame  = 0
	auditAnchorFallbackNote = "scan root: the source anchor (runtime.Caller on %s) did not name a directory (%s) — falling back to the walking contract from the working directory (the shape a -trimpath release build produces)"

	// auditVanishedNote is the report line for a file that disappeared between
	// the directory read and the open — the concurrent-scratch-probe shape this
	// scan met in the field. It is LOGGED (path + error) and counted, never
	// silent: a skip nobody can see is a defect of its own (Rule 15), and the
	// summary count below is what makes "the scan read a second file" and "the
	// scan quietly read nothing" different observations.
	auditVanishedNote = "scan skip: %s vanished between the directory read and the open (%v) — counted as unreadable, not a red"

	// auditScanRootNote names the tree a verdict BELONGS to. The gate's scan
	// root comes from this file's compile-time location (auditRepoRoot), so a
	// verdict is a statement about the tree the BINARY WAS BUILT FROM — and
	// nothing in the pre-fix message said so (root-caused 2026-10-31, task
	// 01a19081: a run standing in a DIFFERENT checkout read a red that named a
	// file its own tree does not contain, and had no way to tell whose tree the
	// claim was about). Printing it is the cheap half of the fix: the reader
	// learns which checkout the scan read, in the same line as the verdict.
	auditScanRootNote = "scan root: %s (the tree this test binary was BUILT from — never the checkout the process happens to stand in)"

	// auditKeyFuncSep separates a classification key's two halves
	// ("path:TestFunc" — auditHit.Key). Named because the separator is
	// behaviour, not decoration: auditKeySourcePath splits on it, and a key
	// that lost it must be refused rather than silently truncated.
	auditKeyFuncSep = ":"

	// auditAlienCitationNote is the VACUUM REFUSAL for a citation whose source
	// file is not in the scanned tree (root-caused 2026-10-31, the report this
	// epic answers). The pre-fix gate had exactly one vocabulary for "this key
	// matches no hit" — STALE PERMIT — and it therefore reported a key naming a
	// file that does not exist AT ALL in the scanned tree as if it were a
	// permit for a real, once-existing assertion. That is a false claim of the
	// most expensive kind: it tells the reader to go delete an entry for a file
	// they cannot open. The two facts are different and get different
	// sentences — an assertion FIXED OR RENAMED (file present, key gone) stays
	// the stale-permit arm; a citation whose FILE is absent is refused as a
	// vacuum, because nothing in this tree can be classified or deleted for it.
	auditAlienCitationNote = "REFUSED (vacuum): the citation %q names NO source file in the scanned tree %s.\n" +
		"  This is not an UNCLASSIFIED assertion and not a stale permit: this tree does not contain the file at all, so there is nothing here to classify and nothing here to delete.\n" +
		"  Two causes, told apart by the scan root above: the verdict was produced against ANOTHER checkout (a test binary built in a sibling tree — re-run the gate there), or the file was removed and the citation is orphaned."

	// ── the anchor + vanish regression tests' own fixture vocabulary ────────
	// (Rule 1: every literal these tests compare on lives here, so a test and
	// the contract it pins cannot drift apart.)

	// auditFixtureModLine is the fixture module's one directive; the temp module
	// needs no real Go source to be a MODULE root — `go.mod` is what
	// auditModuleRootAbove looks for.
	auditFixtureModLine = "module assertiontotalityfixture\n"
	// auditFixtureLiveBody holds the incident's shape, so a scan over a fixture
	// yields a KNOWN hit — the proof that the walk went on after a vanished file
	// (a skip with no hit after it would be indistinguishable from a scan that
	// stopped early and reported quibbles instead).
	auditFixtureLiveBody = auditSyntheticBundleSource
	// auditFixtureDanglingName is a dangling SYMLINK, not a missing path: the
	// directory listing names it (so the walk visits it) and the open cannot
	// produce it — the vanish shape at the syscall level, deterministic, with no
	// sleep and no concurrent writer. auditFixtureDanglingTarget is the name it
	// points at, deliberately never created.
	auditFixtureDanglingName   = "zz_dangling_test.go"
	auditFixtureDanglingTarget = "zz_never_created_test.go"
	// auditFixtureDeniedDir is chmod 0o000, so listing it fails EACCES — a
	// NON-NotExist read failure, which must stay loud.
	auditFixtureDeniedDir = "zz_denied"
	// auditFixtureMissingRoot is passed as a scan root, so the "broken anchor
	// stays loud" arm is driven: a walk that swallowed this would report an
	// empty tree as a clean one.
	auditFixtureMissingRoot = "zz_no_such_root"
	// auditFixtureProbeDir is the subtree the fixtures put their files in.
	auditFixtureProbeDir = "probe"

	auditDirPermDefault = 0o755
	auditDirPermDenied  = 0o000
	auditFilePerm       = 0o644

	// auditFakeSiblingName is the trap directory for the cwd test: a directory
	// that IS a module root and holds a decoy *_test.go, i.e. exactly what the
	// pre-fix walk would have selected when the process stood in a sibling
	// worktree.
	auditFakeSiblingName = "gopi-0000000000000000000000000000fake"
	auditFakeSiblingFile = "zz_decoy_test.go"

	// auditTrimpathAnchor is a `-trimpath` build's recorded source path: shaped
	// like this file's, module-relative, resolving to nothing on disk. It must
	// be REFUSED as an anchor (the fallback is what keeps a release build's
	// suite honest) — an anchor that accepted it would scan a tree named by a
	// path nobody can stat.
	auditTrimpathAnchor = "github.com/nickhuang99/gopi/internal/tools/" + "assertion_totality_audit_test.go"
)

// auditTotalityActionRe matches the message vocabulary of an action claim:
// the words that turn "count == 0" into "nothing HAPPENED". A negation of a
// step count whose message avoids every one of these words is not making the
// bundled claim this gate is about (e.g. "want exactly one push step").
var auditTotalityActionRe = regexp.MustCompile(
	`(?i)\b(run|runs|ran|delete|deletes|remove|removes|push|pushes|pushes|fetch|merge|merges|write|writes|mutate|mutates|touch|touches|kill|kills|prune|prunes|commit|commits|apply|applies|act|acts|execute|executes|executed|fire|fires|touch)\b`)

// auditTotalityNegationRe matches the NEGATION half of the claim: the words
// that make an empty set mean "nothing happened at all" rather than "this
// expected thing is present". Without a negation, `len(x) != 0` is a POSITIVE
// assertion ("we did see the row") and is out of scope — that polarity
// distinction is what keeps this predicate's false-positive count low
// (measured: it takes the 46 raw `len(...) != 0` sites in the test trees down
// to the handful that actually assert a totality).
var auditTotalityNegationRe = regexp.MustCompile(
	`(?i)\b(no|not|never|nothing|none|without|cannot|mustn't|doesn't)\b`)

// auditHit is one negative-totality assertion found in the tree.
type auditHit struct {
	// Key is "path:funcName" — the classification key. Deliberately NOT the
	// line number: a site keeps its identity when lines shift around it, so a
	// sibling branch editing the same test does not invalidate the audit.
	Key string
	// Pos is "path:line" as measured now — report-only (the data half).
	Pos string
	// Message is the assertion's own words (the claim being made).
	Message string
	// Why is the reason carried BESIDE the assertion (a `why:` comment in the
	// auditWhyLookback window), or "" when the site carries none. It is the
	// site-level carrier of the same decision the map holds — see
	// auditSiteWhy for why both exist and auditClassified for how they combine.
	Why string
}

// TestAssertionTotalityAuditGate is the gate: every negative-totality
// assertion in the test trees must carry a classification. On failure it
// prints the exact key to add to auditTotalityAllow, so the fix is to make a
// decision — never to widen the predicate.
func TestAssertionTotalityAuditGate(t *testing.T) {
	root := auditRepoRoot(t)
	t.Logf(auditScanRootNote, root)
	hits := auditScanTree(t, root).hits
	if len(hits) == 0 {
		t.Fatalf("the scan found ZERO negative-totality assertions — the gate cannot read green-blind; " +
			"check the scan roots and the fixture's own assertions against auditSyntheticBundleSource")
	}
	// A hit may be classified at EITHER carrier — the allow map or the site's
	// own `why:` comment (auditClassified decides, in one place). A hit with
	// neither is UNCLASSIFIED, and that is what the remedy line below asks for:
	// a decision, written down where the next reader of that assertion will
	// meet it.
	//
	// The inverse half is a classification that no longer matches a site: the
	// STALE PERMIT, the failure mode a map-of-keys audit drifts into (the
	// assertion is fixed or renamed, the entry survives, and the next reader
	// believes a claim is still being made that no longer exists). Same ratchet
	// the standards gates use — a stale baseline entry is an error.
	//
	// Those two facts are TOLD APART by auditGateVerdict, and that separation is
	// this gate's VACUUM CRITERION (root-caused 2026-10-31): a key whose source
	// FILE is not in the scanned tree is neither an UNCLASSIFIED assertion nor a
	// stale permit — it is a citation of something this tree does not contain,
	// so it is REFUSED by name and never sent to the reader as "delete this
	// entry" for a file they cannot open. See auditAlienCitationNote.
	v := auditGateVerdict(root, hits, auditTotalityAllow)
	for _, h := range v.shortReason {
		t.Errorf("%s: classification is too short to be a reason: %q", h.Key, h.why)
	}
	for _, h := range v.unclassified {
		t.Errorf("%s (%s): negative-totality assertion %q is UNCLASSIFIED.\n"+
			"  - is the bundled claim right, or does the set contain an idempotent step that must still run (the 2026-09-16 push/dirty defect)?\n"+
			"  - then write the decision down, at EITHER carrier (this key is UNCLASSIFIED in this run, so it appears in NO `classified …` line below — those name OTHER snippets, and one file may hold same-named Test* functions; a `why:` that only quotes this message back is not a reason):\n"+
			"      auditTotalityAllow[%q] = \"<correct | over-broad: which half> — <evidence>\"\n"+
			"    or a `// why: <the same> — <evidence>` comment on the assertion or the %d line(s) above it, at least %d chars",
			h.Key, h.Pos, h.Message, h.Key, auditWhyLookback, auditAllowMinReasonLen)
	}
	for _, key := range v.alien {
		t.Errorf(auditAlienCitationNote, key, root)
	}
	if len(v.stale) > 0 {
		t.Errorf("auditTotalityAllow carries %d STALE key(s) — no such assertion is in the tree any more.\n"+
			"  The claim was fixed, renamed or removed: re-read the reason and DELETE the entry (a stale permit silently re-arms the next agent).\n  %s",
			len(v.stale), strings.Join(v.stale, "\n  "))
	}
	// Data report (visible, never a failure): every classified site and why.
	for _, h := range hits {
		if reason, carrier, ok := auditClassified(h, auditTotalityAllow); ok {
			t.Logf("classified %-72s %s via %s\n    claim: %s\n    why:   %s", h.Key, h.Pos, carrier, h.Message, reason)
		} else if strings.Contains(h.Message, auditInlineAllowToken) {
			t.Logf("inline-allowed %-68s %s", h.Key, h.Pos)
		}
	}
}

// TestAssertionTotalityAuditGateRedProof is the predicate's own red proof (the
// Rule 16 convention): the SAME body that is silent when it asserts a
// non-zero count, or when its message makes no action claim, is reported when
// it bundles "nothing ran". Without this the gate could be green-blind — a
// predicate that never fires looks exactly like a clean tree.
func TestAssertionTotalityAuditGateRedProof(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"the incident's shape: a negated step count + an action-verb claim", auditSyntheticBundleSource, 1},
		{"a count of one (positive polarity — out of scope)", `package p
import "testing"
func TestX(t *testing.T) {
	if len(rep.Steps) != 1 {
		t.Fatalf("exactly one push step must run: %+v", rep.Steps)
	}
}`, 0},
		{"a negated count whose message claims nothing", `package p
import "testing"
func TestX(t *testing.T) {
	if len(rep.Steps) != 0 {
		t.Fatalf("want no extra entries: %+v", rep.Steps)
	}
}`, 0},
		{"a positive expectation (== 0 is the hazard, not >= 1)", `package p
import "testing"
func TestX(t *testing.T) {
	if len(rep.Steps) == 0 {
		t.Fatalf("the push step must be reported: %+v", rep.Steps)
	}
}`, 0},
		{"ast.Expr count outside an if statement", `package p
import "testing"
func TestX(t *testing.T) {
	n := len(rep.Steps)
	if n != 0 {
		t.Fatalf("nothing may run: %d", n)
	}
}`, 0},
		{"the inline escape exempts the site", `package p
import "testing"
func TestX(t *testing.T) {
	if len(rep.Steps) != 0 { // audit-allow: the claim is scoped to the worktree step
		t.Fatalf("no step may run: %+v", rep.Steps)
	}
}`, 0},
		{"production files are not scanned", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := auditHitsInSource("synthetic_test.go", tc.src)
			if len(got) != tc.want {
				t.Fatalf("hits = %d want %d (%+v)", len(got), tc.want, got)
			}
		})
	}
}

// TestAssertionTotalityAuditGateClassifiesTheLiveTree asserts the gate reads
// the REAL tree (not an empty glob): at least the three known sites of the
// incident family are present, and each is classified.
func TestAssertionTotalityAuditGateClassifiesTheLiveTree(t *testing.T) {
	root := auditRepoRoot(t)
	hits := auditScanTree(t, root).hits
	globs, err := filepath.Glob(filepath.Join(root, "internal", "tools", "*"+auditTestSuffix))
	if err != nil || len(globs) == 0 {
		t.Fatalf("the scan roots resolve to nothing (globs=%d err=%v) — a broken glob must fail, not pass vacuously", len(globs), err)
	}
	// The two keys are the incident's OWN two arms, and re-pointing is this
	// check's established repair (the merger-tick precedent in the allow file's
	// comment): a witness that no longer fires must be REPLACED by a live one,
	// never dropped, or the check degenerates into a tautology.
	//
	//   • TestServerRecycleDirtyWorktreeSkipped (:958) — THE KEY WAS REWRITTEN
	//     OUT OF THE PREDICATE, so it can no longer be a witness. 6b1d753c
	//     ("feat(align): make integration-branch alignment a precondition of
	//     delivery") hoisted the ALIGNMENT step ahead of the dirty gate
	//     (task_finish_cleanup.go:1343), so a dirty worktree legitimately
	//     records an align step; the old condition then fired on a CORRECT tree
	//     and the assertion became `len(rep.Steps) == 0 || !strings.Contains(…,
	//     finishCleanupUntrackedResidueNote)` — a POSITIVE claim about the last
	//     step, with no negated count to see. Nothing was deleted or weakened:
	//     the prediction changed shape, and the predicate deliberately does not
	//     reach that shape (widening it to `len(x) == 0 || …` would re-hunt
	//     every positive-emptiness assertion in the tree — see
	//     auditCondIsNegatedCount's "wrong-polarity" note). Its history is kept
	//     in assertion_totality_allow_test.go's "rows NOT in the gate's hit
	//     list" section, so a reader meets it rather than a silent gap.
	//
	//   • TestRunGroundCollectRefusesACloneGroundCarryingUndeliveredWork — THE
	//     SECOND KEY WAS RETIRED, AND THIS IS ITS LIVE REPLACEMENT (2026-10-30,
	//     task 01a18ea5). It used to be
	//     TestFinishCleanupServerRecycleSkipsWorktreeInUse (:1307), the surviving
	//     carrier of the incident's shape at that removal site: with a live CWD
	//     holder the arm returned at its liveness gate BEFORE the alignment step,
	//     so genuinely no step ran. 58b5618 retired the finish-cleanup mechanism
	//     and deleted that test FILE, so the witness is gone and the scan cannot
	//     see it — a check that kept naming it would be asserting about a
	//     deleted file (and did fail, correctly, before this rewrite).
	//
	//     The replacement drives the LIVE removal site: RunGroundCollect over a
	//     clone ground that carries undelivered work must keep the ground and
	//     write no removal record. Same shape, same subject — "a refusal means
	//     the removal step did not run" — asked at the site that still exists,
	//     and it is classified by name in assertion_totality_allow_test.go with
	//     its own reason (both halves worked through there). Re-pointing rather
	//     than dropping is this check's established repair: a witness that no
	//     longer fires must be REPLACED by a live one, never dropped, or the
	//     check degenerates into a tautology.
	//
	//   • TestMergeCloseOutNamesNothingWhenThePositionDidNotMove — was a third
	//     key here, a witness from the merge close-out MINT family. The epic
	//     version-identity (2026-09-24) deleted that mint (and the per-landing
	//     mint with it), so the assertion — and its key — are gone.
	wantKeys := []string{
		"internal/tools/run_ground_collect_clone_test.go:TestRunGroundCollectRefusesACloneGroundCarryingUndeliveredWork",
	}
	keys := make([]string, 0, len(hits))
	for _, h := range hits {
		keys = append(keys, h.Key)
	}
	var missing []string
	for _, want := range wantKeys {
		found := false
		for _, k := range keys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		sort.Strings(keys)
		t.Fatalf("the incident family's own assertions are not in the scan result: %s — the predicate no longer sees the shape it was written for.\nseen:\n%s",
			strings.Join(missing, ", "), strings.Join(keys, "\n"))
	}
}

// TestAssertionTotalityAuditGateStaleKeyRedProof pins the inverse failure and
// its precision: a key that matches a live site is fine, a key with no site is
// a stale permit, and one live + one stale key fails on exactly the stale one
// (so the check cannot be satisfied by an empty allow-map — an empty map has no
// stale keys AND fails the unclassified half for every hit, which is the other
// direction of the same ratchet).
func TestAssertionTotalityAuditGateStaleKeyRedProof(t *testing.T) {
	live := auditHitsInSource("synthetic_test.go", auditSyntheticBundleSource)
	if len(live) != 1 {
		t.Fatalf("fixture: want exactly one hit, got %+v", live)
	}
	reasons := func(keys ...string) map[string]string {
		m := map[string]string{}
		for _, k := range keys {
			m[k] = "a reason long enough to pass the read floor"
		}
		return m
	}
	// The fixture root carries the file the live hit was parsed from, so the
	// STALE arm has a resolvable citation to work with. Without it every key
	// below would take the vacuum arm and this proof would measure the wrong
	// predicate (which is exactly the confusion the split exists to end).
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "synthetic_test.go"), []byte(auditSyntheticBundleSource), auditFilePerm); err != nil {
		t.Fatalf("fixture: write synthetic_test.go: %v", err)
	}
	if got := auditGateVerdict(root, live, reasons(live[0].Key)); len(got.stale)+len(got.alien) != 0 {
		t.Fatalf("a key with a live site must be neither stale nor alien, got stale=%v alien=%v", got.stale, got.alien)
	}
	if got := auditGateVerdict(root, live, reasons("synthetic_test.go:TestGone")); len(got.stale) != 1 || len(got.alien) != 0 {
		t.Fatalf("a key whose FILE exists but whose site is gone must be STALE (not alien), got stale=%v alien=%v", got.stale, got.alien)
	}
	got := auditGateVerdict(root, live, reasons(live[0].Key, "synthetic_test.go:TestGone"))
	if len(got.stale) != 1 || got.stale[0] != "synthetic_test.go:TestGone" || len(got.alien) != 0 {
		t.Fatalf("one live + one stale key must report exactly the stale one, got stale=%v alien=%v", got.stale, got.alien)
	}
	// A short reason is a rubber stamp, checked by the gate's own floor.
	if len(strings.TrimSpace("too short")) >= auditAllowMinReasonLen {
		t.Fatal("fixture: the short-reason sample must be below auditAllowMinReasonLen")
	}
}

// TestAssertionTotalityAuditGateRefusesACitationWhoseFileIsAbsent is the RED
// PROOF for the vacuum criterion (root-caused 2026-10-31, task 01a19081).
//
// THE FIELD FAILURE IT PINS: a run whose tree did not contain the cited file at
// ALL read a line telling it the key was UNCLASSIFIED in its tree and to delete
// the corresponding entry — advice about a file it could not open, produced by a
// scan that had read a DIFFERENT checkout. The gate could not tell "this
// assertion was fixed or renamed" (the file is here, the site is gone) from "the
// file is not in the tree I read" (a vacuum), because both arrived as the same
// single sentence.
//
// The three arms below are the whole distinction, driven through the production
// predicate (auditGateVerdict), so a later edit that collapses them back into
// one sentence turns exactly one of these red:
//
//	file absent            → alien  (REFUSED as a vacuum; the tree cannot be asked to classify it)
//	file present, site gone → stale  (a real permit drift; delete the entry)
//	file present, site live → neither
//
// And the refusal must be MACHINE-CHECKABLE, not prose: auditCitationResolvesIn
// is measured against the filesystem, so a key naming a directory, a
// separator-less key and an empty-path key are all non-resolving — three shapes
// a prefix test would have credited.
func TestAssertionTotalityAuditGateRefusesACitationWhoseFileIsAbsent(t *testing.T) {
	live := auditHitsInSource("synthetic_test.go", auditSyntheticBundleSource)
	if len(live) != 1 {
		t.Fatalf("fixture: want exactly one hit, got %+v", live)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "synthetic_test.go"), []byte(auditSyntheticBundleSource), auditFilePerm); err != nil {
		t.Fatalf("fixture: write synthetic_test.go: %v", err)
	}

	// ARM 1 — the reported shape, verbatim: a citation into a source directory
	// whose file this tree does not carry. It must be REFUSED as a vacuum and
	// must NOT be reported as a stale permit, because no entry can be deleted
	// for a file that is not here.
	alienKey := "internal/standards/config_anchor_census_publication_leg_test.go:TestConfigAnchorCensusRefusesAnUnknownFlagInsteadOfWritingIt"
	allow := map[string]string{alienKey: "a reason long enough to pass the read floor"}
	got := auditGateVerdict(root, live, allow)
	if len(got.alien) != 1 || got.alien[0] != alienKey {
		t.Fatalf("a citation whose file is ABSENT must be refused as a vacuum, got alien=%v stale=%v", got.alien, got.stale)
	}
	// why: correct — the claim is "the STALE-PERMIT bucket stayed EMPTY for a citation whose file is absent", and the negated set is auditGateVerdict's classified output. Half (a) the claim is right: auditGateVerdict routes every non-live key through auditCitationResolvesIn FIRST (the `for key := range allow` loop of this file) and appends to `alien`, never to `stale`, when the stat fails — so an entry in stale here would mean the vacuum split had been collapsed back into the single sentence this epic removed. Half (b) no idempotent member is excluded: the key set is a MAP keyed by citation, so each key is visited exactly once and "not stale" cannot suppress a bucket some later stage still owed. The exclusion is asserted POSITIVELY in the same test: ARM 1 requires len(alien)==1 naming the key, the refusal text must carry both the citation and the scanned root, and ARM 2 re-points the SAME shape at a file that IS present and requires it to land in stale — so the emptiness is the absence of the file, not a stale bucket that cannot be reached.
	if len(got.stale) != 0 {
		t.Fatalf("a citation whose file is ABSENT must not be reported as a STALE PERMIT (that sentence tells the reader to delete an entry for a file they cannot open), got stale=%v", got.stale)
	}
	// The refusal names the key AND the scanned root: a reader must be able to
	// tell which checkout the verdict belongs to without guessing.
	msg := fmt.Sprintf(auditAlienCitationNote, got.alien[0], root)
	if !strings.Contains(msg, alienKey) || !strings.Contains(msg, root) {
		t.Fatalf("the vacuum refusal must name BOTH the citation and the scanned tree, got:\n%s", msg)
	}
	if !strings.Contains(msg, "REFUSED (vacuum)") {
		t.Fatalf("the vacuum refusal must say what it is doing, got:\n%s", msg)
	}

	// ARM 2 — the SAME key shape, but the file IS in the tree: now the absence
	// is about the ASSERTION, so the stale-permit arm is the right one. This is
	// what proves arm 1 was measuring the file's presence and not the key's
	// spelling.
	presentKey := "synthetic_test.go:TestGone"
	got = auditGateVerdict(root, live, map[string]string{presentKey: "a reason long enough to pass the read floor"})
	if len(got.stale) != 1 || got.stale[0] != presentKey || len(got.alien) != 0 {
		t.Fatalf("a key whose file IS in the tree and whose site is gone must be STALE, got stale=%v alien=%v", got.stale, got.alien)
	}

	// ARM 3 — the citation-shape half, measured: only a real FILE resolves.
	// A directory, a key with no separator and a key with an empty path half
	// are all vacuums, and each is a shape a prefix test would have accepted.
	if err := os.MkdirAll(filepath.Join(root, "zz_dir"), auditDirPermDefault); err != nil {
		t.Fatalf("fixture: mkdir zz_dir: %v", err)
	}
	for _, tc := range []struct{ name, key string }{
		{"a directory is not a source file", "zz_dir:TestX"},
		{"no separator at all", "synthetic_test.go"},
		{"empty path half", ":TestX"},
		{"empty function half", "synthetic_test.go:"},
		{"a path outside the root", "../outside_test.go:TestX"},
	} {
		if auditCitationResolvesIn(root, tc.key) {
			t.Errorf("%s (%q) must NOT resolve in the scanned root — a citation that resolves to nothing is a vacuum, not a stale permit", tc.name, tc.key)
		}
	}
	// And the positive control, so the loop above is not passing because
	// auditCitationResolvesIn answers false universally.
	if !auditCitationResolvesIn(root, "synthetic_test.go:TestX") {
		t.Fatal("positive control: a real file in the scanned root must resolve — otherwise the refusals above measure nothing")
	}
}

// TestAssertionTotalityAuditGateSiteWhyRedProof pins the SECOND carrier of the
// classification, in both directions, because a gate that accepts a reason it
// cannot read is a gate that stopped asking:
//
//	① a site whose assertion carries a `why:` comment IS classified — and the
//	   carrier is REPORTED as the site's, so a reader knows where the decision
//	   lives (the map wins when both exist: it is the longer, evidenced form);
//	② the SAME source with the comment stripped is UNCLASSIFIED — the escape
//	   must not be reachable by anything but an actual written reason;
//	③ a `// why:` with nothing behind it, and one shorter than the read floor,
//	   are both REFUSED: the token cannot be the reason (Rule 15 — a stub is
//	   not a decision);
//	④ a bare `why:` that is NOT in a comment is refused too, so the fixture
//	   convention (`why: "…"` struct fields in a case table) is not mistaken
//	   for an author's classification of a nearby assertion.
func TestAssertionTotalityAuditGateSiteWhyRedProof(t *testing.T) {
	body := func(comment string) string {
		// Built by SUBSTITUTION, never by a fmt verb: the assertion's own
		// message carries a `%+v`, which a Sprintf would read as the caller's
		// argument (measured — go vet refuses the build).
		return `	rep := run()
	` + comment + `
	if len(rep.Steps) != 0 && !onlyUntrackedResidueNotes(rep.Steps) {
		t.Errorf("no steps may run on a dirty worktree: %+v", rep.Steps)
	}
`
	}
	long := "correct — the claim is scoped to the worktree step, and the guard is the alignment gate's own"
	for _, tc := range []struct {
		name       string
		comment    string
		classified bool
		// siteReason is whether the site's comment carried a why: clause at
		// all — the distinction the floor must not blur: "no decision written"
		// and "a decision too thin to read" are both REFUSED, and the proof
		// pins that they are refused through different observations.
		siteReason bool
	}{
		{"a why: on the line above the assertion", "// why: " + long, true, true},
		{"a why: two lines above (the same window as the inline escape)", "// why: " + long + "\n\t// (continued)", true, true},
		{"no comment at all", "// a note that classifies nothing", false, false},
		{"a non-comment why: (a case table's struct field)", `why: "` + long + `"`, false, false},
		// ③ the bare `// why:` carries the TOKEN and no reason, so the site is
		// UNCLASSIFIED (the gate's unclassified half fires) — the token can
		// never stand in for a decision.
		{"a why: with nothing behind it", "// why:", false, false},
		{"a why: shorter than the read floor", "// why: correct", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `package p

import "testing"

func TestSyntheticDirtyBundle(t *testing.T) {
` + body(tc.comment) + `}
`
			hits := auditHitsInSource("synthetic_test.go", src)
			if len(hits) != 1 {
				t.Fatalf("fixture: want exactly one hit, got %+v", hits)
			}
			// The GATE's own decision is read through one helper, so what this
			// proof asserts and what the gate enforces cannot drift: a site is
			// classified exactly when a reason of at least
			// auditAllowMinReasonLen stands behind one of the two carriers.
			why := auditSiteWhy(strings.Split(src, "\n"), auditHitLine(t, src))
			if carried := strings.TrimSpace(why) != ""; carried != tc.siteReason {
				t.Fatalf("the site's own why: clause was read as %q, want carried=%v", why, tc.siteReason)
			}
			// The gate's decision, from the ONE classifier the gate itself
			// calls — a site is accepted exactly when a reason of at least
			// auditAllowMinReasonLen stands behind a carrier.
			_, carrier, ok := auditClassified(hits[0], map[string]string{})
			accepted := ok && len(strings.TrimSpace(why)) >= auditAllowMinReasonLen
			if accepted != tc.classified {
				t.Fatalf("site reason %q gives accepted=%v, want %v — a site carrier is read exactly when a decision is written behind it",
					why, accepted, tc.classified)
			}
			if accepted && carrier != auditCarrierSiteWhy {
				t.Fatalf("carrier=%q, want the site carrier %q (the map was empty)", carrier, auditCarrierSiteWhy)
			}
		})
	}
	// ④ The map wins when both carriers exist, and is reported as the map's:
	// the entry is the longer, evidenced form, so a reader is sent to it.
	src := `package p

import "testing"

func TestSyntheticDirtyBundle(t *testing.T) {
` + body("// why: "+long) + `}
`
	hits := auditHitsInSource("synthetic_test.go", src)
	if len(hits) != 1 {
		t.Fatalf("fixture: want exactly one hit, got %+v", hits)
	}
	reason, carrier, ok := auditClassified(hits[0], map[string]string{hits[0].Key: "over-broad — the map's own entry, longer than the site note"})
	if !ok || carrier != auditCarrierAllow {
		t.Fatalf("an allow-map entry must be reported as the map's carrier, got %q ok=%v", carrier, ok)
	}
	if !strings.Contains(reason, "the map's own entry") {
		t.Fatalf("the map's reason must be the one reported, got %q", reason)
	}
}

// auditSyntheticBundleSource is the incident's shape in miniature, used by the
// red proof AND as the read-floor witness (if the predicate stops firing on
// it, the gate fails loudly instead of passing on an empty hit set).
const auditSyntheticBundleSource = `package p

import "testing"

func TestSyntheticDirtyBundle(t *testing.T) {
	rep := run()
	if len(rep.Steps) != 0 && !onlyUntrackedResidueNotes(rep.Steps) {
		t.Errorf("no steps may run on a dirty worktree: %+v", rep.Steps)
	}
}
`

// ── the predicate ───────────────────────────────────────────────────────────

// auditScanTree walks the two test roots and returns every hit, sorted by key
// for a stable report.
//
// A file that cannot be READ because it is not there any more is COUNTED and
// reported, never returned as the walk's error: the first observed defect of
// this gate was a concurrent run's scratch probe (a zz_*_test.go) vanishing
// between WalkDir's directory read and the open, which made a tree with nothing
// wrong in it go red (see the source-anchor/`vanished` comment block above).
// Every OTHER read failure — a permission error, an I/O error, a broken symlink
// target — stays a failure: the skip is scoped to os.IsNotExist, exactly the
// shape the walk cannot prevent, and it is never silent (Rule 15).
func auditScanTree(t *testing.T, root string) auditScan {
	t.Helper()
	var out auditScan
	for _, sub := range []string{auditScanRootInternal, auditScanRootCmd} {
		if err := auditWalkRoot(t, root, sub, &out); err != nil {
			t.Fatalf("scan %s: %v", filepath.Join(root, sub), err)
		}
	}
	out.sortHits()
	out.logSummary(t, root)
	return out
}

// auditWalkRoot walks ONE test root (root/sub) into out, applying the
// vanish-tolerance contract and returning every OTHER error to its caller. It
// is factored out — rather than inlined in the loop — so the contract's two
// halves can each be driven on a fixture of their own: a vanishing member
// (returns nil, counts a skip) and a MISSING ROOT (returns the error, so a
// broken anchor can never read as a clean tree). Pinned by
// TestAssertionTotalityAuditScanToleratesAVanishingFile.
func auditWalkRoot(t *testing.T, root, sub string, out *auditScan) error {
	t.Helper()
	base := filepath.Join(root, sub)
	return filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// An entry removed while the walk is in flight must not redden
			// the gate — at the DIRECTORY level either, which is the same
			// race one rung up. The walk's own root is excluded: "the scan
			// root does not exist" is a broken anchor, and a walk that
			// tolerated it would report an empty tree as a clean one.
			if os.IsNotExist(err) && path != base {
				out.skip(t, path, err)
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, auditTestSuffix) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			if os.IsNotExist(rerr) {
				out.skip(t, path, rerr)
				return nil
			}
			return rerr
		}
		out.read++
		out.hits = append(out.hits, auditHitsInSource(filepath.ToSlash(rel), string(src))...)
		return nil
	})
}

// sortHits gives the report a stable order (by classification key), so two runs
// over the same tree print the same sequence.
func (s *auditScan) sortHits() {
	sort.Slice(s.hits, func(i, j int) bool { return s.hits[i].Key < s.hits[j].Key })
}

// auditScan is the evidence of ONE scan: the hits it read, the files that
// vanished under it, and how many files it actually read. The last two are not
// decoration — they are what makes "the scan tolerated a vanishing file" and
// "the scan read a whole tree" observable claims rather than assumptions
// (Rule 15: a skip that leaves no trace is the defect, not the fix).
type auditScan struct {
	hits     []auditHit
	vanished []string
	read     int
}

// skip records one unreadable-because-absent path: counted, logged with its
// path and error, and kept for the caller's own assertions.
func (s *auditScan) skip(t *testing.T, path string, err error) {
	t.Helper()
	s.vanished = append(s.vanished, path)
	t.Logf(auditVanishedNote, path, err)
}

func (s *auditScan) logSummary(t *testing.T, root string) {
	t.Helper()
	if len(s.vanished) == 0 {
		return
	}
	t.Logf("scan %s: %d file(s) vanished mid-walk and were counted as unreadable", root, len(s.vanished))
}

// auditHitsInSource is the predicate over one file's text. A file that does
// not parse yields no hits: the gate is about assertions, and a build error is
// already someone else's red test.
func auditHitsInSource(rel, src string) []auditHit {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil
	}
	lines := strings.Split(src, "\n")
	var hits []auditHit
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ifStmt, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			if !auditCondIsNegatedCount(ifStmt.Cond, src, fset) {
				return true
			}
			msg := auditMessageIn(ifStmt.Body, src, fset)
			if msg == "" || !auditTotalityActionRe.MatchString(msg) || !auditTotalityNegationRe.MatchString(msg) {
				return true
			}
			line := fset.Position(ifStmt.Pos()).Line
			if auditInlineAllowed(lines, line) {
				return true
			}
			hits = append(hits, auditHit{
				Key:     rel + ":" + fn.Name.Name,
				Pos:     rel + ":" + strconv.Itoa(line),
				Message: msg,
				Why:     auditSiteWhy(lines, line),
			})
			return true
		})
	}
	return hits
}

// auditCondIsNegatedCount reports whether the condition asserts that a COUNT
// is zero — i.e. "none of these things happened". Three spellings:
//
//	len(x) != 0     (the totality spelling)
//	!(len(x) == 0)
//	<any of the above> && <further clause>   (the incident's own shape:
//	                                           "count != 0 && !onlyNotes(...)")
//
// `len(x) != 1` / `!= n` for n > 0 is deliberately OUT of scope: "we did see
// exactly one" is a positive claim about the set's contents, and it is the
// wrong-polarity site the first draft of this gate drowned in.
func auditCondIsNegatedCount(cond ast.Expr, src string, fset *token.FileSet) bool {
	switch e := cond.(type) {
	case *ast.ParenExpr:
		return auditCondIsNegatedCount(e.X, src, fset)
	case *ast.UnaryExpr:
		if e.Op != token.NOT {
			return false
		}
		return auditCondIsZeroCount(e.X, src, fset)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND, token.LOR:
			return auditCondIsNegatedCount(e.X, src, fset) || auditCondIsNegatedCount(e.Y, src, fset)
		case token.NEQ:
			if _, ok := auditLenArg(e.X); ok {
				return auditZeroLiteral(e.Y, src, fset)
			}
			if _, ok := auditLenArg(e.Y); ok {
				return auditZeroLiteral(e.X, src, fset)
			}
		}
	}
	return false
}

// auditCondIsZeroCount matches the `len(x) == 0` half (used under a NOT).
func auditCondIsZeroCount(cond ast.Expr, src string, fset *token.FileSet) bool {
	if p, ok := cond.(*ast.ParenExpr); ok {
		return auditCondIsZeroCount(p.X, src, fset)
	}
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.EQL {
		return false
	}
	if _, ok := auditLenArg(be.X); ok {
		return auditZeroLiteral(be.Y, src, fset)
	}
	if _, ok := auditLenArg(be.Y); ok {
		return auditZeroLiteral(be.X, src, fset)
	}
	return false
}

// auditLenArg returns the name of a len(...) call's argument.
func auditLenArg(e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "len" || len(call.Args) != 1 {
		return "", false
	}
	return auditExprText(call.Args[0]), true
}

func auditNonZeroLiteral(e ast.Expr, src string, fset *token.FileSet) bool {
	txt := auditExprTextAt(e, src, fset)
	n, err := strconv.Atoi(strings.TrimSpace(txt))
	return err == nil && n != 0
}

func auditZeroLiteral(e ast.Expr, src string, fset *token.FileSet) bool {
	n, err := strconv.Atoi(strings.TrimSpace(auditExprTextAt(e, src, fset)))
	return err == nil && n == 0
}

// auditMessageIn returns the first string literal passed to a test-failure
// call inside the statement (t.Errorf / t.Fatalf / t.Error / t.Fatal).
func auditMessageIn(body *ast.BlockStmt, src string, fset *token.FileSet) string {
	var msg string
	ast.Inspect(body, func(n ast.Node) bool {
		if msg != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Errorf", "Fatalf", "Error", "Fatal":
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					msg = s
				}
			}
		}
		return true
	})
	return msg
}

// auditInlineAllowed reports whether the statement's line — or either of the
// two lines above it — carries the inline escape.
func auditInlineAllowed(lines []string, line int) bool {
	for i := line - 1; i >= 0 && i >= line-2-1; i-- {
		if i < len(lines) && strings.Contains(lines[i], auditInlineAllowToken) {
			return true
		}
	}
	return false
}

// auditWhyLookback is how many lines above an assertion the site's own `why:`
// reason may sit. It is the same window the inline escape uses (three lines),
// so the two site-level carriers of one decision are read by the same rule —
// which is the point: a reason belongs on the sentence it justifies.
const auditWhyLookback = 3

// auditSiteWhy returns the reason carried BESIDE the assertion, or "".
//
// The site is the assertion's own line and the auditWhyLookback-1 lines above
// it (auditInlineAllowed's window). A reason is recognised by its carrier
// token (auditWhyToken) INSIDE A COMMENT — the line, trimmed, must open with
// `//`, so a table row's `why:` struct field (the fixture convention this
// predicate scans right past) can never be mistaken for a classification the
// author wrote about this assertion. Only the TEXT AFTER the token is the
// reason: the token itself must not count toward the read floor, or a stub
// `// why:` with nothing behind it would satisfy a check whose whole purpose is
// to force a decision to be written down (Rule 15).
func auditSiteWhy(lines []string, line int) string {
	for i := line - 1; i >= 0 && i >= line-auditWhyLookback; i-- {
		if i >= len(lines) {
			continue
		}
		text := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(text, auditCommentPrefix) {
			continue
		}
		at := strings.Index(text, auditWhyToken)
		if at < 0 {
			continue
		}
		return strings.TrimSpace(text[at+len(auditWhyToken):])
	}
	return ""
}

// auditCommentPrefix is what a Go line comment opens with — the marker that
// makes a `why:` in a scanned line a reader's decision rather than a struct
// field's name (Rule 1b: the spelling lives here, not at the comparison).
const auditCommentPrefix = "//"

// auditClassified returns the reason classifying this hit, and where it came
// from — the map entry, the site's own `why:`, or neither. ONE function, so
// the gate body, the stale-key half and the live-tree check cannot disagree
// about what counts as classified (Rules 13/14).
//
// The MAP WINS when both carriers exist: an entry names the claim, its set and
// its evidence at length, while the site's comment is the working note beside
// the code. Either carrier satisfies the gate; the return says which, so the
// report can tell a reader where to look.
func auditClassified(h auditHit, allow map[string]string) (reason, carrier string, ok bool) {
	if r, found := allow[h.Key]; found {
		return r, auditCarrierAllow, true
	}
	if h.Why != "" {
		return h.Why, auditCarrierSiteWhy, true
	}
	return "", "", false
}

// The two carriers auditClassified reports. They are values with meaning
// (printed, and asserted by the red proofs), so each has one definition
// (Rule 1b/13).
const (
	auditCarrierAllow   = "auditTotalityAllow"
	auditCarrierSiteWhy = "site why:"
)

// auditShortReason is a hit whose classification exists but is a rubber stamp.
type auditShortReason struct {
	auditHit
	why string
}

// auditVerdict is the gate's whole reading of ONE tree, split into the four
// facts it must never confuse (root-caused 2026-10-31 — the report this epic
// answers read a "STALE PERMIT — delete the entry" line as an instruction about
// a file its own checkout does not contain):
//
//	unclassified  a hit with NO carrier        → a decision is owed HERE
//	shortReason   a carrier that is a stamp    → a real reason is owed HERE
//	alien         a citation naming a file NOT IN THE SCANNED TREE
//	              → a VACUUM: nothing in this tree can be classified or deleted
//	stale         a citation whose FILE IS THERE and whose key matched no hit
//	              → the assertion was fixed/renamed: delete the entry
//
// The split between the last two is the vacuum criterion. Without it the gate
// had ONE sentence for both, and the sentence it chose ("delete the entry")
// was the wrong advice for a file that is not here.
type auditVerdict struct {
	unclassified []auditHit
	shortReason  []auditShortReason
	alien        []string
	stale        []string
}

// auditGateVerdict reads one tree's hits against one allow map. It is factored
// out of the gate body so the red proofs drive the SAME predicate the gate
// runs — never a copy of its logic (the file's established pattern: a proof
// that reimplements the rule proves nothing).
func auditGateVerdict(root string, hits []auditHit, allow map[string]string) auditVerdict {
	var v auditVerdict
	seen := make(map[string]bool, len(hits))
	for _, h := range hits {
		seen[h.Key] = true
		reason, _, ok := auditClassified(h, allow)
		if !ok {
			v.unclassified = append(v.unclassified, h)
			continue
		}
		if len(strings.TrimSpace(reason)) < auditAllowMinReasonLen {
			v.shortReason = append(v.shortReason, auditShortReason{auditHit: h, why: reason})
		}
	}
	for key := range allow {
		if seen[key] {
			continue
		}
		if auditCitationResolvesIn(root, key) {
			v.stale = append(v.stale, key)
			continue
		}
		v.alien = append(v.alien, key)
	}
	sort.Strings(v.stale)
	sort.Strings(v.alien)
	return v
}

// auditCitationResolvesIn reports whether a classification key's path half
// names a SOURCE FILE inside the scanned root. The criterion is a measured
// stat of the path under root — never a prefix test and never a guess from the
// key's spelling — because the question the gate must answer before it accuses
// a file is "is that file in the tree I read", and only the filesystem answers
// it.
func auditCitationResolvesIn(root, key string) bool {
	rel, ok := auditKeySourcePath(key)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && !info.IsDir()
}

// auditKeySourcePath splits a classification key ("path:TestFunc") into its
// path half. ok=false means the key carries no separator or an empty half — a
// malformed citation, which cannot resolve and is therefore refused as a
// vacuum rather than credited to some file.
func auditKeySourcePath(key string) (string, bool) {
	path, fn, found := strings.Cut(key, auditKeyFuncSep)
	if !found || path == "" || fn == "" {
		return "", false
	}
	return path, true
}

func auditExprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return auditExprText(v.X) + "." + v.Sel.Name
	case *ast.IndexExpr:
		return auditExprText(v.X) + "[" + auditExprText(v.Index) + "]"
	}
	return "?"
}

// auditExprTextAt renders a literal-ish expression from the source text (used
// for the compared integer).
func auditExprTextAt(e ast.Expr, src string, fset *token.FileSet) string {
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Value
	}
	start := fset.Position(e.Pos()).Offset
	end := fset.Position(e.End()).Offset
	if start >= 0 && end <= len(src) && start < end {
		return src[start:end]
	}
	return auditExprText(e)
}

// auditRepoRoot returns the module root the gate scans — the tree THIS test
// binary was BUILT from, never the tree the process happens to stand in.
//
// THE DEFECT THIS REPLACES (root-caused 2026-10-14, filed as the epic's own
// symptom): the old form walked up from os.Getwd() looking for go.mod. Under
// `go test ./internal/tools/` cwd IS …/internal/tools, and internal/ carries no
// go.mod, so the walk ran past it to the REPOSITORY root — which is a module
// root wherever the test was invoked from, INCLUDING a task worktree. From
// there the scan walked internal/ + cmd/ of a SIBLING run's checkout while that
// run was writing/deleting its own probes; one ENOENT through the WalkDir error
// return turned the whole gate red with nothing wrong in the tree under test
// (measured: the gate alone is `ok 13.481s`, the failure appears only when a
// concurrent run exists).
//
// The anchor is this FILE's compile-time location: runtime.Caller(0) at the top
// of this function resolves …/internal/tools/assertion_totality_audit_test.go,
// and the module root is the directory whose go.mod is found walking up from
// there (auditGateModName). A cwd cannot influence it, so the gate can never
// scan another run's tree.
//
// The cwd walk survives as the announced FALLBACK for a build whose recorded
// path does not resolve on disk (`-trimpath`, which the release build passes —
// the path becomes module-relative and no longer names a directory). The
// fallback is announced through t.Logf, so a degraded anchor is visible; the
// refusal is deliberate and pinned by
// TestAssertionTotalityAuditScanRootFallsBackToTheWalkingContract.
func auditRepoRoot(t *testing.T) string {
	t.Helper()
	if root, ok := auditRootFromSourceAnchor(); ok {
		return root
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, ok := auditModuleRootAbove(dir)
	if !ok {
		t.Fatalf("no %s above %q, and the source anchor did not name a directory either", auditGateModName, dir)
	}
	// Rule 15: the degradation is REPORTED. A reader of a fallback run must be
	// able to tell that the scan root came from the cwd — that is the reading
	// under which a concurrent run's tree can be scanned at all.
	t.Logf(auditAnchorFallbackNote, auditAnchorFile, root)
	return root
}

// auditRootFromSourceAnchor resolves the module root from this file's own
// compile-time path. ok=false means the anchor is unusable — NOT that the tree
// lacks a go.mod — so the caller falls back to the walking contract.
func auditRootFromSourceAnchor() (string, bool) {
	_, file, _, ok := runtime.Caller(auditSourceAnchorFrame)
	if !ok || file == "" {
		return "", false
	}
	return auditAnchorRootFrom(file)
}

// auditAnchorRootFrom turns a RECORDED source path into a module root. The
// path is refused as an anchor unless it names a real file: a -trimpath build
// records a module-relative path ("github.com/nickhuang99/gopi/internal/tools/…")
// which resolves to nothing on disk, and a directory is not this file either.
// Both refusals are pinned by the anchor regression test, so the fallback
// contract cannot be silently narrowed to "always trust the recorded path".
func auditAnchorRootFrom(file string) (string, bool) {
	if file == "" {
		return "", false
	}
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		return "", false
	}
	return auditModuleRootAbove(filepath.Dir(file))
}

// auditModuleRootAbove walks up from dir and returns the first directory
// carrying go.mod — the walking contract, factored out so the anchor and the
// fallback cannot drift into two different definitions of "the module root".
func auditModuleRootAbove(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, auditGateModName)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// ── the two behaviours this epic fixed, each pinned ────────────────────────
//
// The gate went red in the field for two INDEPENDENT reasons, and the fix has
// two halves, so there are two regression tests below. Both drive the
// PRODUCTION helpers (auditWalkRoot, auditModuleRootAbove, auditRepoRoot) —
// never a copy of their logic — so a later edit that re-breaks either half
// turns exactly one of them red.

// auditFixtureTree builds a throwaway MODULE root with a probe subtree and
// returns (root, probe): root carries go.mod (so it IS a module root for
// auditModuleRootAbove), and probe is the directory the vanish/deny fixtures
// live in.
func auditFixtureTree(t *testing.T) (root, probe string) {
	t.Helper()
	root = t.TempDir()
	probe = filepath.Join(root, auditFixtureProbeDir)
	if err := os.MkdirAll(probe, auditDirPermDefault); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", probe, err)
	}
	if err := os.WriteFile(filepath.Join(root, auditGateModName), []byte(auditFixtureModLine), auditFilePerm); err != nil {
		t.Fatalf("fixture: write %s: %v", auditGateModName, err)
	}
	return root, probe
}

// TestAssertionTotalityAuditScanToleratesAVanishingFile is the RED PROOF for the
// first half of the fix: a file that cannot be read because it is NOT THERE is
// counted and reported, never returned as the walk's error. Before the fix the
// callback was `if rerr != nil { return rerr }` and this fixture turned the
// gate red with "scan …: no such file or directory" — the exact field failure
// (a concurrent run's scratch probe vanishing between the directory read and
// the open).
//
// The fixture is DETERMINISTIC — no sleep, no second process, no load (Rule
// 27): a DANGLING SYMLINK is listed by the directory read and cannot be opened,
// which is the race's observation with the race removed. The live file's name
// sorts FIRST, so the walk meets it before the skip — and that is deliberate:
// the assertion is then "no error AND read==1 AND the skip was recorded", i.e.
// the walk finished the work it could do and reported what it could not, rather
// than stopping at the first thing it could not open.
func TestAssertionTotalityAuditScanToleratesAVanishingFile(t *testing.T) {
	root, probe := auditFixtureTree(t)

	// The live file's name sorts BEFORE the dangling one, so the walk meets it
	// first and the skip is exercised last: that ordering makes "hits>0" true
	// for a walk that aborted at the skip as well — which is why the assertion
	// below is written over BOTH the hit and the recorded skip.
	live := filepath.Join(probe, "aa_live"+auditTestSuffix)
	if err := os.WriteFile(live, []byte(auditFixtureLiveBody), auditFilePerm); err != nil {
		t.Fatalf("fixture: write %s: %v", live, err)
	}
	dangling := filepath.Join(probe, auditFixtureDanglingName)
	if err := os.Symlink(auditFixtureDanglingTarget, dangling); err != nil {
		t.Fatalf("fixture: symlink %s: %v", dangling, err)
	}
	// The fixture must reproduce the failure mode it claims to — a dangling
	// link that somehow reads is not the vanish shape, and the proof would pass
	// without measuring anything.
	if _, err := os.ReadFile(dangling); !os.IsNotExist(err) {
		t.Fatalf("fixture: reading the dangling link %s gave err=%v; want a NotExist error (the vanish shape)", dangling, err)
	}

	out := auditScan{}
	if err := auditWalkRoot(t, root, auditFixtureProbeDir, &out); err != nil {
		t.Fatalf("a file that VANISHED mid-walk must not fail the scan, got %v — this is the field failure: a concurrent run's scratch probe disappearing between the directory read and the open", err)
	}
	if out.read != 1 {
		t.Fatalf("the walk read %d file(s), want exactly 1 — the skip must not take the rest of the tree with it", out.read)
	}
	if len(out.hits) == 0 {
		t.Fatalf("the walk read the live fixture and found no hits: the skip turned into an early exit, not a skip")
	}
	if len(out.vanished) != 1 || out.vanished[0] != dangling {
		t.Fatalf("vanished=%v, want exactly [%s] — a skip nobody can see is the defect the fix must not introduce (Rule 15)", out.vanished, dangling)
	}

	// THE OTHER DIRECTION, in the same fixture: a read failure that is NOT a
	// disappearance stays loud. The denied directory can be listed (as a child
	// of probe) but not opened, so the walk returns EACCES — a gate that
	// tolerated this would be inert rather than robust.
	denied := filepath.Join(probe, auditFixtureDeniedDir)
	if err := os.MkdirAll(denied, auditDirPermDenied); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", denied, err)
	}
	t.Cleanup(func() { _ = os.Chmod(denied, auditDirPermDefault) }) //stylegate:ignore restoring the mode so t.TempDir can remove the tree; the error is the cleanup's, not the assertion's
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode 0000 does not deny access, so the non-NotExist arm cannot fire here")
	}
	deniedOut := auditScan{}
	err := auditWalkRoot(t, root, auditFixtureProbeDir, &deniedOut)
	if err == nil {
		t.Fatalf("an UNREADABLE (not missing) directory must stay a failure, got nil — a tolerated EACCES makes the scan inert, not robust")
	}
	if os.IsNotExist(err) {
		t.Fatalf("the fixture produced a NotExist error (%v) where a permission error was intended — the arm measured nothing", err)
	}
}

// TestAssertionTotalityAuditScanRootComesFromTheSourceTreeNotTheCwd is the RED
// PROOF for the second half: the scan root is derived from THIS FILE's
// compile-time location, never from the working directory.
//
// It reproduces the field condition exactly — a process standing in a SIBLING
// module root that holds a decoy tree — and asserts the anchor does not move:
// auditRepoRoot still returns the module root of the tree it was built from,
// while the cwd-based contract (auditModuleRootAbove) is shown, in the SAME
// run, to select the decoy. Without that second assertion the test could pass
// because the trap failed to materialise rather than because the anchor held.
func TestAssertionTotalityAuditScanRootComesFromTheSourceTreeNotTheCwd(t *testing.T) {
	anchorRoot, ok := auditRootFromSourceAnchor()
	if !ok {
		t.Skipf("this build's recorded source path does not resolve (a -trimpath release build: %q) — the anchor is refused and the walking fallback applies, pinned by the case below", auditTrimpathAnchor)
	}
	if anchorRoot == "" {
		t.Fatalf("the source anchor resolved to an EMPTY root")
	}
	if filepath.Base(anchorRoot) == filepath.Base(t.TempDir()) {
		t.Fatalf("the anchor must not be a temp path")
	}
	// The anchor names the tree that holds THIS file: assert by identity, not
	// by shape — the tree it returns must be the one whose copy of this file is
	// the compiled one.
	if _, err := os.Stat(filepath.Join(anchorRoot, "internal", "tools", auditAnchorFile)); err != nil {
		t.Fatalf("the anchor root %q does not hold internal/tools/%s: %v", anchorRoot, auditAnchorFile, err)
	}

	// THE TRAP: a module root named like a sibling task worktree, holding a
	// decoy test tree. `go test` runs each package with cwd = that package's
	// source directory (internal/tools), so the pre-fix walk climbed past the
	// package to a REPOSITORY root — from a task worktree, a DECOY tree exactly
	// like this one. Standing here is the field condition.
	trap := filepath.Join(t.TempDir(), auditFakeSiblingName)
	for _, sub := range []string{auditScanRootInternal, auditScanRootCmd} {
		if err := os.MkdirAll(filepath.Join(trap, sub), auditDirPermDefault); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", filepath.Join(trap, sub), err)
		}
	}
	decoyDir := filepath.Join(trap, auditScanRootInternal, "tools")
	if err := os.MkdirAll(decoyDir, auditDirPermDefault); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", decoyDir, err)
	}
	if err := os.WriteFile(filepath.Join(trap, auditGateModName), []byte(auditFixtureModLine), auditFilePerm); err != nil {
		t.Fatalf("fixture: write trap %s: %v", auditGateModName, err)
	}
	decoy := filepath.Join(decoyDir, auditFakeSiblingFile)
	if err := os.WriteFile(decoy, []byte(auditFixtureLiveBody), auditFilePerm); err != nil {
		t.Fatalf("fixture: write %s: %v", decoy, err)
	}
	t.Chdir(trap)

	// (a) the cwd contract DOES select the decoy — so the trap is real and the
	// test is not passing on a fixture that never materialised.
	cwdRoot, ok := auditModuleRootAbove(trap)
	if !ok {
		t.Fatalf("the trap %s carries %s and must be a module root for the cwd contract", trap, auditGateModName)
	}
	decoyOut := auditScanTree(t, cwdRoot)
	decoyHit := false
	for _, h := range decoyOut.hits {
		if strings.Contains(h.Key, auditFakeSiblingFile) {
			decoyHit = true
		}
	}
	if !decoyHit {
		t.Fatalf("the decoy tree %s yielded no hit — the trap measured nothing, so the assertion below would be vacuous", cwdRoot)
	}

	// (b) THE ASSERTION: the production root resolver ignores the cwd entirely.
	got := auditRepoRoot(t)
	want, err := filepath.EvalSymlinks(anchorRoot)
	if err != nil {
		t.Fatalf("eval symlinks %s: %v", anchorRoot, err)
	}
	gotReal, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("eval symlinks %s: %v", got, err)
	}
	if gotReal != want {
		t.Fatalf("standing in the decoy %s, the scan root came out as %q — want the source tree %q. A root chosen from the cwd scans a SIBLING run's tree: that is the defect this anchors away (and the reason a concurrent run's scratch probe could redden this gate).",
			trap, gotReal, want)
	}
	if strings.HasPrefix(gotReal, trap) {
		t.Fatalf("the scan root %q lies inside the trap %q — the resolver followed the cwd", gotReal, trap)
	}

	// (c) THE FALLBACK CONTRACT, measured rather than asserted in prose: a
	// -trimpath build records a module-relative path, which is REFUSED as an
	// anchor (it names no file). If that refusal ever became "accept it", the
	// release build's scan would read a tree that does not exist — and the
	// walking contract is what keeps it honest instead.
	if _, ok := auditAnchorRootFrom(auditTrimpathAnchor); ok {
		t.Fatalf("a -trimpath-recorded path (%q) must be REFUSED as an anchor: it resolves to nothing on disk, and an accepted anchor scans a tree nobody can stat", auditTrimpathAnchor)
	}
	// A directory is not this file either — the same refusal, on the other
	// shape a recorded path can take.
	if _, ok := auditAnchorRootFrom(filepath.Dir(gotReal)); ok {
		t.Fatalf("a DIRECTORY (%q) must not be accepted as the recorded source file", filepath.Dir(gotReal))
	}
	// And the walking contract itself still finds the same tree when it is
	// handed the anchor's own directory — the two arms of the resolver are one
	// definition of "the module root", not two.
	walked, ok := auditModuleRootAbove(filepath.Join(gotReal, auditScanRootInternal, "tools"))
	if !ok || walked != gotReal {
		t.Fatalf("walking up from the anchor's own directory gave %q (ok=%v), want %q — the anchor and the fallback must agree on what the module root is", walked, ok, gotReal)
	}
}

// TestAssertionTotalityAuditScanFailsOnAMissingRoot pins the OTHER half of the
// vanish contract, in the place it could silently regress: the walk's own ROOT.
// "The scan root does not exist" must stay an error. If the vanish tolerance
// were written without the `path != base` exclusion, a broken anchor (a root
// naming the wrong tree, a removed internal/ directory) would walk nothing,
// report no vanished file, and the gate would read an EMPTY hit set — which is
// green-blind, not green.
func TestAssertionTotalityAuditScanFailsOnAMissingRoot(t *testing.T) {
	root, _ := auditFixtureTree(t)
	out := auditScan{}
	err := auditWalkRoot(t, root, auditFixtureMissingRoot, &out)
	if err == nil {
		t.Fatalf("a scan root that does not exist must FAIL the walk, got nil (read=%d vanished=%d) — a tolerated missing root reports an unread tree as a clean one", out.read, len(out.vanished))
	}
	if len(out.vanished) != 0 {
		t.Fatalf("a missing ROOT is not a vanished file: it must not be counted as one (vanished=%v)", out.vanished)
	}
	if out.read != 0 || len(out.hits) != 0 {
		t.Fatalf("the failed walk must contribute nothing (read=%d hits=%d)", out.read, len(out.hits))
	}
}

// auditHitLine returns the line the fixture's one synthetic hit sits on — the
// assertion the gate found, resolved through the same scan the gate runs.
func auditHitLine(t *testing.T, src string) int {
	t.Helper()
	hits := auditHitsInSource("synthetic_test.go", src)
	if len(hits) != 1 {
		t.Fatalf("fixture: want exactly one hit, got %+v", hits)
	}
	at := strings.LastIndex(hits[0].Pos, ":")
	n, err := strconv.Atoi(hits[0].Pos[at+1:])
	if err != nil {
		t.Fatalf("gate reported an unparseable position %q: %v", hits[0].Pos, err)
	}
	return n
}
