package main

// gopibin_identity_guard_test.go — WHAT the binary under test was built from,
// and whether the tree under test still holds that content.
//
// The e2e guards in this package drive a BUILT artifact, so their green is
// falsifiable only against the tree that artifact came from. The predecessor of
// this decision compared the binary's MTIME against the newest production .go
// source, and an mtime is not an identity: any rewrite that moves time forward
// without touching a byte — a red-proof restore, `cp`, `tar -x`, a `git
// checkout` of the same content — made a byte-identical tree look "newer" than
// the binary it came from, and the guard refused a run whose binary already
// contained the code. Measured 2026-10-13 (run 1791902369-3257849, task
// 01a13508).
//
// So identity here is a CLAIM read out of the artifact, and the claim is CHECKED
// against git — which stores content, the only thing that decides whether the
// binary contains the tree:
//
//	build revision   the `-X github.com/nickhuang99/gopi/internal/version.Revision=<commit>`
//	                 operand the build recipe supplies (the path half is
//	                 buildidentity.RevisionLdflagPath, never re-typed — Rule 13/14).
//	                 It is read back with debug/buildinfo.ReadFile, so it is a
//	                 fact about the FILE being driven, not about this process.
//	source content   `git diff --name-only <that revision> -- cmd internal`:
//	                 every tracked production source whose CURRENT bytes differ
//	                 from the bytes the binary was built from — empty means the
//	                 binary holds the tree's content, whatever the timestamps
//	                 say, and a non-empty answer NAMES the files.
//	mtime            the predecessor's criterion, consulted ONLY when the
//	                 artifact carries no revision at all (a hand-built binary),
//	                 and only downwards. The verdict says it decided by mtime, so
//	                 nobody reads it as a content proof.

import (
	"bytes"
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/buildidentity"
)

const (
	// criterionBuildIdentity / criterionContent / criterionMtime name the three
	// criteria the verdict is allowed to rest on. They are constants (Rule
	// 1b/13/14) because the failure TEXT must carry the one that decided, and a
	// message assembled from inline words would drift away from the vocabulary
	// the tests assert.
	criterionBuildIdentity = "build identity"
	criterionContent       = "content"
	criterionMtime         = "mtime"

	// sourceTreeCmdDir / sourceTreeInternalDir are the two trees the artifact
	// compiles. Named once (Rule 1a/1b): the diff scope, the mtime walk and the
	// failure text all speak about the same two directories.
	sourceTreeCmdDir      = "cmd"
	sourceTreeInternalDir = "internal"

	// productionSourceSuffix selects .go files; testSourceSuffix marks the files
	// no build of the binary compiles.
	productionSourceSuffix = ".go"
	testSourceSuffix       = "_test.go"

	// gitExecutable runs every question asked of the repository. Named (Rule 1a)
	// so a host without git fails by name instead of by a bare exec error.
	gitExecutable = "git"
	// gitDiffVerb/gitNameOnlyFlag ask git for the NAMES of the entries that
	// differ (the names are the point: "something changed" is not a verdict a
	// reader can act on) and gitPathSeparatorFlag ends each option list.
	gitDiffVerb            = "diff"
	gitNameOnlyFlag        = "--name-only"
	gitRevParseVerb        = "rev-parse"
	gitCatFileVerb         = "cat-file"
	gitExistsFlag          = "-e"
	gitMergeBaseVerb       = "merge-base"
	gitIsAncestorFlag      = "--is-ancestor"
	gitHeadRevision        = "HEAD"
	gitPathSeparatorFlag   = "--"
	gitLineSeparator       = "\n"
	gitCommandOutputNotice = "git"

	// ldflagsBuildSetting is the artifact setting carrying the linker flags the
	// build was given: the channel the identity operand arrives through. It is
	// the Go toolchain's own key, named here because this check is its reader.
	ldflagsBuildSetting = "-ldflags"
	// revisionLdflagKey is the operand key the recipe stamps the revision with
	// (`-X <path>=<value>`). Only the command LINE reaches this setting: a
	// GOFLAGS-forwarded -ldflags is dropped from the embedded build settings,
	// which is what keeps the operand a witness supplied by the build recipe
	// rather than a rubber stamp from the caller's environment (measured: a
	// GOFLAGS build carried `-ldflags=…` with an EMPTY revision).
	revisionLdflagKey     = "-X"
	revisionOperandSplit  = "="
	revisionOperandFields = 2
	shortRevisionLen      = 12
	fullRevisionLen       = 40
	// gitObjectHashHexLenFull is the length of a full git object name (the
	// fixture compares two of them, so the number has one spelling).
	gitObjectHashHexLenFull = 40
	// freshnessRefusalSignature is the ONE sentence EVERY refusal arm carries
	// (Rule 1b/14): the mtime arm's fixed text below composes it, and the content
	// and build-identity arms print it as their own line. It is composed from the
	// marker constant (cwdProbeFreshnessMarker, task_session_cwd_freshness_evidence_test.go)
	// rather than retyped, so the phrase the shell classifier keys on cannot be
	// deleted from the guard's messages while the shell side keeps matching it.
	//
	// WHY EVERY ARM AND NOT ONLY THE MTIME ONE (the defect this closes, measured
	// on master cbdaca0): the runner's classifier (scripts/lib/stream-failures.sh,
	// stream_freshness_marker) recognises a refusal BY THIS SENTENCE, and on master
	// the CONTENT arm — the arm a mid-run content change actually fires — did not
	// carry it. So the ordinary shape (a production source edited while the suite
	// ran) produced refusals the classifier could not see, and the report named
	// none of them: the run read as N unrelated test failures.
	freshnessRefusalSignature = "this test " + cwdProbeFreshnessMarker + ", so it would verify code that is not in that binary."
	// freshnessCriterionPrefix opens the line EVERY arm uses to say which
	// criterion decided it. The runner RELAYS that line instead of re-deriving a
	// criterion of its own (scripts/lib/stream-failures.sh's
	// stream_freshness_criterion_prefix, which reads this line by prefix), so the
	// prefix is one value with two readers rather than two spellings — and a gate
	// (TestFreshnessCriterionPrefixSpellingsAgree) compares the two files, because
	// a shell literal beside a Go literal drifts in silence.
	freshnessCriterionPrefix = "deciding criterion: "
	// freshnessRefusalText is the ONE sentence both mtime-arm refusals begin
	// with (Rule 1b/14): the shape assertion and build-failure composer below
	// and the evidence arm's composer
	// (task_session_cwd_freshness_evidence_test.go) print it, so the guard's
	// refusal cannot lose its two mtimes or its marker because one of the three
	// writers was edited alone.
	freshnessRefusalText = "%s (mtime %s) is OLDER than %s (mtime %s) — " + freshnessRefusalSignature
	// freshnessSourceReportPrefix introduces each newer source a refusal names.
	// A prefix rather than a bare path, because a path the reader cannot tell
	// apart from prose is not evidence. It is shared with the evidence arm's
	// composer, which prints the same lines.
	freshnessSourceReportPrefix = "newer source: "
)

// binFreshness is the guard's verdict: whether the driven binary may be treated
// as containing the tree's production sources, WHICH criterion decided, and —
// when it may not — the message a reader gets (already naming the criterion and
// the rebuild command).
type binFreshness struct {
	// Fresh is the verdict: true = the binary holds the tree's content (or, for
	// a hand-built artifact, is not older than its sources).
	Fresh bool
	// Criterion is the criterion that decided — one of criterionBuildIdentity /
	// criterionContent / criterionMtime. It is carried in the verdict, not
	// recomputed in the message, so a message can never claim a criterion the
	// decision did not use.
	Criterion string
	// Refusal is the failure text when Fresh is false.
	Refusal string
	// Abstain is the THIRD answer, beside fresh and refused: the difference the
	// guard found is not a property of this package's code at all — the checkout
	// MOVED after the artifact was built, so every file it would name is carried
	// by commits that landed under the run. Fresh is false in this case (the
	// binary really does not hold the tree's content) and Refusal carries the
	// time facts; the CALLER decides what to do with it, and it skips rather than
	// fails.
	//
	// WHY THIS IS NOT A RELAXATION: the guard's job is to keep a green from
	// meaning "code that is not in the binary". An abstention does not produce a
	// green — the test does not run, its e2e path is unverified, the skip message
	// says so, and TestMain counts every one of them. What it stops is the guard
	// reporting an ENVIRONMENT fact as a code verdict.
	Abstain bool
}

// assertGopiBinIsFresh REFUSES a run against a binary that is not known to
// contain the tree's production sources — a verification whose subject is a
// prebuilt artifact must prove the artifact reflects the tree, or every green is
// unfalsifiable.
//
// The failure mode the guard exists for was measured while writing the
// child-cwd test: the source edit that disables the child's Cmd.Dir was made,
// the test ran green, and only after rebuilding bin/gopi did the same test fail
// as designed.
func assertGopiBinIsFresh(t *testing.T, bin string) {
	t.Helper()
	root := cwdProbeModuleRoot(t)
	verdict, err := decideBinFreshness(root, bin)
	if err != nil {
		t.Fatalf("%s: %v — refusing to treat the run as verification (a binary whose provenance cannot be established is not evidence)", bin, err)
	}
	// The THREE outcomes are resolved by one function, so the rule "an abstention
	// is never a pass and never a failure" lives in a single place a gate can
	// assert against — not in the order two if-statements happen to be written.
	//
	// The abstention is checked BEFORE the refusal: both carry Fresh=false (the
	// binary genuinely does not hold the tree's content), and the difference is
	// what the run may conclude from that — a code verdict, or an environment
	// fact it cannot speak about.
	switch freshnessResolutionOf(verdict) {
	case freshnessSkip:
		noteTreeMoveAbstention()
		// TWO calls on purpose. The measured facts (build revision, build moment,
		// the move, the commits it carried) are LOGGED — they are what a reader
		// acts on — and the skip carries the LITERAL reason, because a skip whose
		// reason is a format string is a skip nobody can read in a summary.
		t.Logf("%s", verdict.Refusal)
		t.Skip(treeMoveAbstainSkipReason)
	case freshnessFail:
		t.Fatalf("%s", verdict.Refusal)
	case freshnessPass:
	}
}

// freshnessResolution is what a test must DO with a verdict. It exists so the
// mapping is one expression (Rule 14): a SKIP that silently became a PASS is the
// defect this epic is about, and an assertion can only pin that if the mapping is
// a function rather than the order of two statements.
type freshnessResolution int

const (
	freshnessPass freshnessResolution = iota
	freshnessFail
	freshnessSkip
)

// freshnessResolutionOf maps a verdict to its outcome. Abstain is tested FIRST:
// an abstaining verdict also carries Fresh=false, and reading it as a failure is
// exactly the 7% false red this fixes.
func freshnessResolutionOf(verdict binFreshness) freshnessResolution {
	switch {
	case verdict.Abstain:
		return freshnessSkip
	case verdict.Fresh:
		return freshnessPass
	default:
		return freshnessFail
	}
}

// freshnessRefusalNamesTheRefusingTest is the phrase the RUNNER's classifier keys
// on when it attributes a refusal to a test (scripts/lib/stream-failures.sh,
// stream_freshness_marker) — exported from here so the gate can assert that EVERY
// refusal arm carries it: the mid-run CONTENT change (decideBinFreshness's content
// arm) is the arm the runner's classifier missed on master, and a refusal that
// does not carry the signature is a condition the report cannot name (Rule 15).
func freshnessRefusalNamesTheRefusingTest(refusal string) bool {
	return strings.Contains(refusal, cwdProbeFreshnessMarker)
}

// decideBinFreshness answers "may this artifact be treated as containing root's
// production sources?" and says which criterion decided. An error return means a
// question could not be ASKED (git unavailable, the artifact unreadable) — the
// caller reports that, never a pass (Rule 15).
//
// The chain, and why an equal revision does NOT short-circuit it: the artifact's
// revision is the ANCHOR the content question is asked against, never the answer
// itself. A tree whose HEAD still equals the anchor can hold an EDITED
// production file (the ordinary "source changed, binary not rebuilt" case), and
// a tree whose timestamps all moved forward can hold byte-identical content. Both
// are decided from the same git call — which is exactly why neither time nor the
// revision alone is the criterion.
func decideBinFreshness(root, bin string) (binFreshness, error) {
	info, err := os.Stat(bin)
	if err != nil {
		return binFreshness{}, fmt.Errorf("stat the driven binary: %w", err)
	}
	rev, ok := binaryBuildRevision(bin)
	if !ok {
		return decideBinFreshnessByMtime(root, bin, info)
	}
	if !revisionExists(root, rev) {
		return binFreshness{Criterion: criterionBuildIdentity, Refusal: fmt.Sprintf(
			"%s was built from revision %s, which this repository does not contain — %s\n"+
				freshnessCriterionPrefix+"%s (the revision the binary was built with, read out of the artifact's own %s setting).\n"+
				"Rebuild it (CGO_ENABLED=0 go build -ldflags \"%s %s=$(%s %s %s)\" -o %s ./cmd/agent/) and re-run.",
			bin, shortRev(rev), freshnessRefusalSignature, criterionBuildIdentity, ldflagsBuildSetting,
			revisionLdflagKey, buildidentity.RevisionLdflagPath, gitExecutable, gitRevParseVerb, gitHeadRevision, bin)}, nil
	}
	moved, err := productionSourcesChangedSince(root, rev)
	if err != nil {
		return binFreshness{}, err
	}
	if len(moved) == 0 {
		return binFreshness{Fresh: true, Criterion: criterionContent}, nil
	}
	if abstention, ok := treeMoveAbstention(root, rev, moved, info.ModTime()); ok {
		return binFreshness{Abstain: true, Criterion: criterionContent, Refusal: abstention}, nil
	}
	return binFreshness{Criterion: criterionContent, Refusal: fmt.Sprintf(
		"%s does NOT contain the tree's production sources: %d file(s) differ from the revision it was built from (%s) — %s\n"+
			freshnessCriterionPrefix+"%s (git compared the tracked production %s content NOW against the content at %s — a timestamp was never consulted).\n"+
			"%s"+
			"changed: %s\n"+
			"Rebuild it (CGO_ENABLED=0 go build -o %s ./cmd/agent/) and re-run.",
		bin, len(moved), shortRev(rev), freshnessRefusalSignature, criterionContent, productionSourceSuffix, shortRev(rev),
		ancestryNote(root, rev), strings.Join(moved, ", "), bin)}, nil
}

// ancestryNote adds the build-identity half to a content refusal: WHEN the
// revision the binary carries is not an ancestor of the tree's HEAD, the two
// facts together say the binary is from a different line of history (not merely
// from before a later edit). It returns "" in the ordinary case — a note that
// always printed would be noise a reader learns to skip.
func ancestryNote(root, rev string) string {
	head, err := gitRevision(root, gitHeadRevision)
	if err != nil {
		return ""
	}
	if isAncestor(root, rev, head) {
		return ""
	}
	return fmt.Sprintf("build identity: the binary's revision %s is NOT an ancestor of the tree's %s %s — the artifact is from a different line of history, not merely from before a later edit.\n",
		shortRev(rev), gitHeadRevision, shortRev(head))
}

// decideBinFreshnessByMtime is the LAST RESORT, for the one shape with no other
// witness: an artifact whose build carried no revision operand (built by hand, or
// by a recipe that does not stamp one). It compares the binary's mtime against the
// newest production source and SAYS it decided by mtime — an mtime comparison
// cannot tell an identical-bytes rewrite from a real edit, and the message must
// not let a reader believe otherwise.
//
// The comparison used to end at that verdict, and that was the defect this arm
// repairs: ONE sentence for two different causes. A reader who followed it while
// the run's own red proof was writing the tree got the same refusal back
// (measured 2026-10-13, run-1791849386-333342: internal/agent/clock.go was
// rewritten in place 1.6s AFTER the tested binary was built and restored 2m16s
// later, so every refusal named the RESTORED file against a binary that already
// contained the code — 12 of that run's 15 failures). The refusal now reports the
// tree's HEAD, the artifact's own build identity, which newer sources the tree's
// records show as differing from HEAD, whether any source was written WHILE the
// run was comparing, and a VERDICT chosen by that measurement.
func decideBinFreshnessByMtime(root, bin string, info os.FileInfo) (binFreshness, error) {
	newest, newestPath, err := cwdProbeNewestProductionSource(root)
	if err != nil {
		return binFreshness{}, fmt.Errorf("scan %s for production sources: %w", root, err)
	}
	absence := fmt.Sprintf("%s carries no revision in its %s setting", bin, ldflagsBuildSetting)
	if newest.IsZero() {
		return binFreshness{Criterion: criterionMtime, Refusal: fmt.Sprintf(
			"no %s source found under %s — cannot prove %s is fresh, and %s; %s refusing to treat the run as verification.\n"+
				freshnessCriterionPrefix+"%s (the last resort, taken because nothing else could be asked).",
			productionSourceSuffix, root, bin, absence, freshnessRefusalSignature, criterionMtime)}, nil
	}
	if !info.ModTime().Before(newest) {
		return binFreshness{Fresh: true, Criterion: criterionMtime}, nil
	}
	evidence, err := freshnessRefusalEvidence(root, bin, info.ModTime())
	if err != nil {
		return binFreshness{}, err
	}
	return binFreshness{Criterion: criterionMtime, Refusal: fmt.Sprintf(
		freshnessRefusalText+"\n"+
			"%s\n"+
			freshnessCriterionPrefix+"%s (the last resort; an mtime comparison cannot tell an identical-bytes rewrite from a real edit, so a build that stamps its revision gets a content proof instead).\n"+
			"%s\n"+
			"Rebuild it (CGO_ENABLED=0 go build -o %s ./cmd/agent/) and re-run.",
		bin, info.ModTime().Format(time.RFC3339), newestPath, newest.Format(time.RFC3339), absence, criterionMtime,
		evidence, bin)}, nil
}

// binaryBuildRevision returns the revision the ARTIFACT itself carries in its
// embedded `-ldflags` setting, and whether the artifact carries one at all.
//
// It reads the FILE (debug/buildinfo.ReadFile), never the running process,
// because the test drives a file that is not this process. ok=false means there
// is no identity operand — a witness that is ABSENT, which every caller must
// report rather than treat as a match.
//
// The recorded setting is the linker flag's VALUE as the build was given it, so
// both spellings a recipe may use are accepted — `-X <path>=<rev>` (one flag and
// its operand, the form internal/testutil.LdflagsFlag and
// buildidentity's constants produce) and the joined `-X<path>=<rev>`: the flag key is stripped,
// then the operand is split once on `=`, and only an operand naming
// buildidentity.RevisionLdflagPath — the ONE spelling of the path — counts.
func binaryBuildRevision(path string) (revision string, ok bool) {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, s := range bi.Settings {
		if s.Key != ldflagsBuildSetting {
			continue
		}
		operand := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s.Value), buildidentity.LdflagXKey))
		fields := strings.SplitN(operand, revisionOperandSplit, revisionOperandFields)
		if len(fields) != revisionOperandFields {
			continue
		}
		if strings.TrimSpace(fields[0]) != buildidentity.RevisionLdflagPath {
			continue
		}
		value := strings.TrimSpace(fields[1])
		return value, value != ""
	}
	return "", false
}

// productionSourcesChangedSince names every TRACKED production .go file under
// root's cmd/ and internal/ trees whose current bytes differ from their bytes at
// rev — the "which file" half of the content criterion, answered by git (which
// compares content, never timestamps).
//
// UNTRACKED production files are deliberately outside this comparison: a
// brand-new file that is in no commit may legitimately be in the binary the run
// just built, so naming it would be a false refusal. The scope is stated here
// once rather than rediscovered per caller.
func productionSourcesChangedSince(root, rev string) ([]string, error) {
	out, err := gitOutput(root, gitDiffVerb, gitNameOnlyFlag, rev, gitPathSeparatorFlag, sourceTreeCmdDir, sourceTreeInternalDir)
	if err != nil {
		return nil, err
	}
	return filterProductionSources(root, out), nil
}

// filterProductionSources narrows a git name list to the PRODUCTION sources the
// guard compares: .go, not a test, inside the two trees the artifact compiles and
// not in a subtree the build does not carry.
//
// It exists as ONE function because two questions are asked of this scope — "which
// files differ from the build revision" (the refusal) and "which files did the
// commits in between carry" (the abstention's evidence) — and a second copy of
// the filter would let the two disagree about what counts as production, which
// would produce an abstention whose evidence and whose complaint name different
// file sets.
func filterProductionSources(root, out string) []string {
	var moved []string
	for _, line := range strings.Split(out, gitLineSeparator) {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if !strings.HasSuffix(path, productionSourceSuffix) || strings.HasSuffix(path, testSourceSuffix) {
			continue
		}
		if cwdProbeBinaryScopeSkipped(root, filepath.Join(root, path)) {
			continue
		}
		moved = append(moved, path)
	}
	return moved
}

// revisionExists reports whether the repository at root contains rev at all —
// the question that separates "the artifact came from this repository" from "the
// artifact came from somewhere else entirely" (a binary copied in from another
// checkout). A missing rev is not a content mismatch, so it is answered before
// any content comparison is attempted.
func revisionExists(root, rev string) bool {
	cmd := exec.Command(gitExecutable, gitCatFileVerb, gitExistsFlag, rev)
	cmd.Dir = root
	return cmd.Run() == nil
}

// isAncestor reports whether rev is an ancestor of (or equal to) head — the
// question "does the revision the binary carries precede the tree", which is
// what makes a several-commit-old artifact still answerable about the lines it
// shares with the tree.
func isAncestor(root, rev, head string) bool {
	cmd := exec.Command(gitExecutable, gitMergeBaseVerb, gitIsAncestorFlag, rev, head)
	cmd.Dir = root
	return cmd.Run() == nil
}

// gitRevision resolves one rev through git and returns its full object name.
func gitRevision(root, rev string) (string, error) {
	out, err := gitOutput(root, gitRevParseVerb, rev)
	if err != nil {
		return "", err
	}
	revision := strings.TrimSpace(out)
	if len(revision) != fullRevisionLen {
		return "", fmt.Errorf("%s %s answered %q; want a %d-hex object name",
			gitExecutable, rev, revision, fullRevisionLen)
	}
	return revision, nil
}

// gitOutput runs one git command in root and returns stdout, turning a
// non-zero exit into an error that carries the command and git's own stderr —
// a bare exit status would read as "no differences" (Rule 15).
func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command(gitExecutable, args...)
	cmd.Dir = root
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s in %s: %w (stderr: %s)",
			gitCommandOutputNotice, strings.Join(args, " "), root, err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// shortRev abbreviates a revision for a message: the full 40 hex are noise in a
// failure a human reads (the value printed is still the one that decided).
func shortRev(rev string) string {
	if len(rev) <= shortRevisionLen {
		return rev
	}
	return rev[:shortRevisionLen]
}
