package tools

// mergeset_cmdagent.go — THE CMD/AGENT RATCHET HALF of the merged-result
// measurement (rebuild_vet_mergeset.go).
//
// THE DEFECT this file closes, in the words of whoever next reads the branch.
// cmd/agent holds a population of tests whose subject is NOT cmd/agent: they
// read the repository's own file index (`git ls-files` over devdoc/), the man
// corpus (man/*.md against the CLI's -h pages), the retired-verb reach across
// every Go file, the peak-guard wire tokens, the chat's placeholder spelling.
// Whoever moves the file those tests read turns them red — the landing that
// moves it is usually a DIFFERENT task, working in a DIFFERENT directory.
//
// THIS STEP NEVER RAN THEM. mergesetGatePackageDirs is a FIXED list of three
// gate directories (internal/standards, internal/stylegate, internal/testutil);
// a landing that changes man/gopi-session.md or adds a devdoc file touches NONE
// of them, so the scoped leg never asked, and the whole-repo style leg measures
// internal/stylegate — a different invariant. So the red arrived on master with
// every measurement the close loop owned reading green, and the next task's
// rebase found it.
//
// MEASURED, on this repo at the published tip 2a67e18 (Rule 38: the generator is
// the command quoted, and every number below was produced by running it there):
//
//	cd <a checkout of 2a67e18> && go test -count=1 \
//	    -run '^(TestManSourcesMatchTheirHelpPage|TestManUnmeasuredCensusIsFullyClassified)$' ./cmd/agent
//	--- FAIL: TestManSourcesMatchTheirHelpPage (6.09s)
//	--- FAIL: TestManUnmeasuredCensusIsFullyClassified (0.00s)
//	FAIL	github.com/nickhuang99/gopi/cmd/agent	6.958s
//
// — a red that stood on the integration tip and that no post-merge gate asked
// for. AND IT WAS INTRODUCED BY A DOCUMENT-ONLY LANDING, which is this leg's whole
// justification: `0a2791e "docs(man): regenerate man/gopi-session.md for the new
// 'gopi session gc' verb"` changes ONE file (`man/gopi-session.md`, 7 lines
// added), its diff carries NO Go source at all, and its first parent `54ac8a9` is
// GREEN on both tests. So neither of this step's existing sources could have
// spoken about it: the gate-package leg needs a touched gate DIRECTORY, and the
// whole-repo leg needs touched Go source. Measured with the same two-test command:
//
//	git checkout 54ac8a9  → ok   github.com/nickhuang99/gopi/cmd/agent	8.503s   (base, GREEN)
//	git checkout 0a2791e  → FAIL github.com/nickhuang99/gopi/cmd/agent	6.958s   (landed, RED)
//
// HOW LONG IT STOOD is the other half of the measurement, and it is why this is a
// leg and not a one-off: the red was repaired by hand four commits later, by
// `85f68e0 "fix(mansource): the flag vocabulary reads one page further down"`
// (it carries `cmd/agent/man_source_gate_test.go`, `internal/mansource/…` and
// `man/gopi-session.md`) — i.e. SOMEBODY EVENTUALLY MEASURED IT BY HAND. Asked
// automatically, the same red would have been named at the landing that caused it.
//
// The cost of asking is the OTHER measured number: the ELEVEN-member roster on
// this ground answers WALL 27.55 s with a cold package cache and a warm build
// cache (`/usr/bin/time` around the exact argv this leg builds), against minutes
// for the package's whole 658-file suite — the leg buys the landing seconds.
//
// WHY A NAMED ROSTER AND NOT THE PACKAGE. `go test ./cmd/agent` with no -run
// filter is the whole-tree suite Rule 27 retired from this path: 658 test files,
// minutes of wall time, and e2e walks that spawn real binaries. What this leg
// needs is the narrow half — the tests whose SUBJECT is a file another landing
// owns — so the roster names them one by one (Rule 1/13/14: the names are
// constants in ONE place, and the -run pattern is DERIVED from them so the list
// and the argv cannot drift apart).
//
// WHY HERE AND NOT A NEW HOOK (Rule 3/35 — no new tool, no new script, no new
// resident dependency). RebuildAfterMergeMergeset already runs on every landing,
// already materialises the merged revision in a detached worktree, already
// measures the FIRST PARENT for attribution, and already owns the reporting
// channels (notify + deduped bug row). This is ONE MORE MEMBER in a run set the
// step already builds.
//
// THE ATTRIBUTION ARM IS THE EXISTING ONE, and it is what makes a pre-existing
// red safe: the base revision is already measured for every run in the set, so a
// red the base ALSO had is downgraded to an abstention by mergesetGateAttribute —
// no second policy, no per-leg exemption. That is the difference between "this
// leg reports the landing's regressions" and "this leg fires forever".
//
// NEVER A REPAIR: the leg measures and reports. It edits no roster, no baseline,
// no exclusion list on the tree it measures — that would let a landing excuse
// its own hit.

import (
	"fmt"
	"strings"
)

// mergesetCmdAgentGateName is how this leg identifies itself in every message.
// It is deliberately NOT mergesetGateTestGateLabel: the two are different
// measurements (a directory-scoped run set vs a named roster inside a package
// this step's directory list does not contain), and a reader of the merger log
// must be able to tell which one spoke (Rule 1b).
const (
	mergesetCmdAgentGateName = "cmd-agent-ratchet"

	// mergesetCmdAgentRunFlag is the toolchain flag that narrows the package's
	// suite to the roster — the whole reason this leg is cheap (Rule 27).
	mergesetCmdAgentRunFlag = "-run"

	// mergesetCmdAgentNoSourceNote is the abstention reported when the landing's
	// diff brought no file from the trees the roster's tests read: nothing they
	// measure can have moved, so the leg stands down and SAYS SO (Rule 15 — a
	// skip that cannot be told from a broken leg is a hole, not a saving).
	mergesetCmdAgentNoSourceNote = "the landing touched no file the cmd/agent ratchets read (%s), so none of them can have moved — not run"

	// mergesetCmdAgentFireLogFmt is the line a FIRING leg logs: the scope in
	// numbers, the first few paths that obliged it, and the roster size — so a
	// reader of the close-out can see what obliged the run without re-reading
	// the diff.
	mergesetCmdAgentFireLogFmt = "the landing touched %d file(s) the cmd/agent ratchets read (%s), so %d named ratchet test(s) run over the merged revision"
)

// mergesetCmdAgentDir is the package the roster lives in — a directory name, and
// the toolchain ref is derived from it exactly as the gate directories' are.
const mergesetCmdAgentDir = "cmd/agent"

// mergesetCmdAgentRatchetTests is THE ROSTER: the cmd/agent tests whose subject
// is a file some OTHER landing owns, so their verdict is about the repository and
// not about this package.
//
// MEMBERSHIP CRITERION (one sentence, applied to one test at a time): the test
// reads a repository-wide artifact — the git INDEX or a tree WALK from the module
// root, the man corpus against the binary's own help pages, or the whole-module
// source scan — so a change in a directory it does not own can turn it red.
//
// WHAT IS DELIBERATELY ABSENT:
//   - every test whose fixture builds its own scratch repository (the great
//     majority): its tree is the fixture's, so no landing can move it, and
//     adding it would buy wall time and no signal;
//   - TestVerbCensusMatchesTheDispatch: it meets the criterion (it walks the
//     module) and it is classified HEAVY (device-exec), so on this path it would
//     SKIP — a member that cannot run is a measurement that reads green for the
//     wrong reason (Rule 15). `checks_test.go`'s heavy inventory is where it
//     belongs; the merge path must not pretend to measure it;
//   - the e2e walks (they spawn the real binary and take minutes), and every
//     Test*RunsAsAdvertised surface test whose subject is the help page it is
//     listed beside — those are the directory-scoped leg's business when they
//     live in a gate directory, and they are cheap local assertions otherwise.
//
// ORDER IS THE READING ORDER of the mechanism: the corpus ratchets first (an
// index walk), then the whole-module scans, then the projections that compare a
// published document against the binary's own output.
var mergesetCmdAgentRatchetTests = [...]string{
	// THE DEVdoc CORPUS (PROJECT.md Rule 36): `git ls-files -z -- devdoc/` and
	// line 1 of every tracked document. A landing that adds a document without a
	// class banner reds it, and that landing is usually not this package's.
	"TestEveryTrackedDevdocDeclaresItsClassInItsHeader",
	// THE PEAK-GUARD REMOVAL (whole tree): two scans asserting the retired
	// symbols and wire tokens are gone from every Go file and every non-history
	// document.
	"TestPeakGuardSymbolsAreAbsentFromTheTree",
	"TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance",
	// THE RETIRED-VERB REACH: the helper table plus the drive gate that reads
	// every test file in the module looking for a real-binary invocation of a
	// retired word.
	"TestRetiredVerbGateHelperTableIsNotEmpty",
	"TestRetiredVerbGateNamesTheKnownHelpers",
	"TestNoTestDrivesTheRealBinaryWithARetiredWord",
	// THE SESSION-REPLAY ABOLITION: the door the retired value came back
	// through, asserted over the tree rather than over a call.
	"TestSessionReplayIsTheDoorTheValueCameBackThrough",
	// THE CHAT PLACEHOLDER: one spelling shared by the chat surface and the
	// session view, asserted across both.
	"TestChatNoTextWordIsSingleSourced",
	// THE MAN CORPUS: man/*.md against the help pages the binary prints. These
	// two are the pair that MOTIVATED this leg: they were RED on the integration
	// tip for ~40 minutes (the header carries the command, the two revisions and
	// the landing that introduced it) before a hand-measured fix landed them —
	// which is the whole argument for asking them automatically.
	"TestManSourcesMatchTheirHelpPage",
	"TestManUnmeasuredCensusIsFullyClassified",
	// THE BARE-`--peers` COVERAGE RATCHET: it diffs the RUN's branch against its
	// own cut point and refuses a changed cmd/agent file no coverage list names.
	// Outside an armed run it SKIPS with its reason stated (its own arm refuses
	// to substitute origin/master), so it is a member that measures when there is
	// something to measure and says why when there is not.
	"TestSyncBarePeersTestCoversEveryChangedFile",
}

// mergesetCmdAgentRatchetPattern derives the toolchain's -run expression from the
// roster: anchored, alternation-joined. ONE source (Rule 2/14) — a name added to
// the roster is measured, and a name that is not in the roster cannot be measured
// by this leg even by accident.
func mergesetCmdAgentRatchetPattern() string {
	return "^(" + strings.Join(mergesetCmdAgentRatchetTests[:], "|") + ")$"
}

// mergesetCmdAgentScopeDirs are the trees the roster's tests read. A landing that
// touches none of them cannot have moved any member's answer, and the leg then
// abstains by NAME rather than running a suite to learn that.
//
// Each entry is a directory prefix with its trailing separator, so a membership
// test is a prefix comparison with no second spelling of "is this file under it".
var mergesetCmdAgentScopeDirs = [...]string{
	mergesetCmdAgentDir + mergesetGatePackageSep,       // the roster itself (a renamed test would red its own package)
	mergesetCmdAgentManDir + mergesetGatePackageSep,    // the man corpus the two man gates compare
	mergesetCmdAgentDevdocDir + mergesetGatePackageSep, // the document corpus the class header gate walks
}

// The two non-Go trees in the scope above (Rule 1b: the names are compared, so
// they are spelled once).
const (
	mergesetCmdAgentManDir    = "man"
	mergesetCmdAgentDevdocDir = "devdoc"
)

// mergesetCmdAgentRuns answers this leg's question for one landing and returns
// the run to append to the caller's run set, plus ONE log-ready note. The
// contract is the sibling legs' (mergesetStylegateRuns, mergesetGendocRuns): a
// non-empty note means this leg added NOTHING, and the note distinguishes
// "nothing was obliged" from "the scope could not be read" in its own words.
//
// repo is the repository the landing happened in; mergeRev is the merged
// revision; dir is the landing's own detached checkout, which only has to be
// non-empty (the leg runs a named subset of a package, it reads no file out of
// it — the toolchain does).
func mergesetCmdAgentRuns(repo, mergeRev, dir string) ([]mergesetGateRun, string) {
	if dir == "" {
		return nil, mergesetGateReasonNoScratch
	}
	touched, reason := mergesetCmdAgentScopeFiles(repo, mergeRev)
	if reason != "" {
		return nil, reason
	}
	if len(touched) == 0 {
		return nil, fmt.Sprintf(mergesetCmdAgentNoSourceNote, mergesetCmdAgentScopeLabel(repo, mergeRev))
	}
	ref := "." + mergesetGatePackageSep + mergesetCmdAgentDir
	mergesetGateLog(mergesetCmdAgentFireLogFmt, len(touched),
		strings.Join(mergesetCmdAgentSample(touched), ", "), len(mergesetCmdAgentRatchetTests))
	argv := append(mergesetGateTestArgv(), mergesetCmdAgentRunFlag, mergesetCmdAgentRatchetPattern(), ref)
	return []mergesetGateRun{{
		Gate:     mergesetCmdAgentGateName,
		Argv:     argv,
		Packages: []string{ref},
	}}, ""
}

// mergesetCmdAgentScopeFiles returns the files the landing brought from the trees
// the roster reads — the change set that can move a member's answer.
//
// It reads the same diff as every other leg of this step (`<mergeRev>^ <mergeRev>`),
// so all of this step's scope decisions are answers to ONE question (Rule 2/14).
// A diff that cannot be read is an ABSTENTION, never a silent widening: a leg that
// cannot see what the landing changed must say so rather than run (or not run) on
// a guess.
func mergesetCmdAgentScopeFiles(repo, mergeRev string) (files []string, reason string) {
	out, err := repoSweepGit(repo, mergesetGateDiffVerb, mergesetGateNameOnlyFlag,
		mergeRev+mergesetGateFirstParent, mergeRev)
	if err != nil {
		return nil, fmt.Sprintf("%s (%s)", mergesetGateReasonNoDiffBase, mergeBuildGateTail(err.Error()))
	}
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		if mergesetCmdAgentScopeObliges(p) {
			files = append(files, p)
		}
	}
	return files, ""
}

// mergesetCmdAgentScopeObliges is the membership test, in ONE place so the
// predicate and the reason text cannot disagree: Go source anywhere (the
// whole-module scans read every package), or a path under one of the trees the
// published-document projections read.
func mergesetCmdAgentScopeObliges(path string) bool {
	if strings.HasSuffix(path, mergesetGateGoSourceSuffix) {
		return true
	}
	for _, dir := range mergesetCmdAgentScopeDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}

// mergesetCmdAgentScopeLabel names the diff the scope was read from, so the
// abstention above says WHICH revision pair it looked at (Rule 15b: a report
// without its evidence is not a report).
func mergesetCmdAgentScopeLabel(repo, mergeRev string) string {
	return repo + " " + mergeRev + mergesetGateFirstParent + ".." + mergeRev
}

// mergesetCmdAgentSample caps the path list carried into the log line: a landing
// can bring hundreds of files, and the line exists to say the leg FIRED, not to
// enumerate.
func mergesetCmdAgentSample(files []string) []string {
	if len(files) <= mergesetCmdAgentSampleMax {
		return files
	}
	return files[:mergesetCmdAgentSampleMax]
}

// mergesetCmdAgentSampleMax bounds the sample above (Rule 1b).
const mergesetCmdAgentSampleMax = 3
