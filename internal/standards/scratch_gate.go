package standardgate

import (
	"os/exec"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// tracked-scratch hygiene gate
//
// Rule 5's sibling: a repository is not clean just because the WORKTREE is
// clean. The 2026-09 repo-hygiene sweep found 457 junk paths committed to the
// repo root — a 19 MB `go test -c` binary, a fake-$HOME tree holding a copy of
// a real ~/.gopi (memory.db + session jsonl), a whole go-build cache and
// hand-written e2e logs — none of which any production file, test, script or
// doc reads. `git rm --cached` + .gitignore fixed the instance; this gate is
// the ratchet that stops the next one, because .gitignore only helps files that
// are ALREADY untracked: a tracked path ignores every ignore rule, so the exact
// same commit can happen again.
//
// The check is deliberately `git ls-files` (the index) rather than a filesystem
// walk: the defect is "this is TRACKED", not "this exists", and the index is the
// only thing that answers it.
// ---------------------------------------------------------------------------

// gitBinary runs the index query. Named so the gate has one obvious seam and
// `git` is not a bare literal at the call site (Rule 1b).
const gitBinary = "git"

// gitLsFilesArgv is the index-listing invocation: every tracked path,
// NUL-separated so a path containing a space or a newline cannot be split into
// two phantom entries.
var gitLsFilesArgv = []string{"ls-files", "-z"}

// junkPathChecks are the tracked-path classes the gate rejects. Each class was
// observed in the sweep; new classes are added here WITH their reason, so the
// gate's refusal list stays a reviewed list and not a growing regex.
var junkPathChecks = []struct {
	// why is the doc comment the gate prints on a hit — the rule, not the fact.
	why string
	// matches reports whether a repo-relative slash path is junk.
	matches func(path string) bool
}{
	{
		why: "a compiled test/probe binary (`go test -c .` wrote a 19 MB .leakprobe-agent.test into the repo root). Build test binaries into the pinned scratch root, never into the tracked tree.",
		matches: func(p string) bool {
			return strings.HasSuffix(p, testBinarySuffix)
		},
	},
	{
		why: "a leak-probe sandbox tree (a fake $HOME holding a copied ~/.gopi: memory.db, sessions jsonl, raw logs). Never commit agent home state.",
		matches: func(p string) bool {
			// Root-anchored prefix, so the sibling trees .leakprobe/,
			// .leakprobe2/ AND the root files .leakprobe-agent.test /
			// .leakprobe-full.out all match (the red-proof found this: an
			// exact-dir check silently missed .leakprobe2/, the 405-path
			// class) while a nested lookalike does not.
			return strings.HasPrefix(p, leakProbeDirPrefix)
		},
	},
	{
		why: "a hand-written e2e evidence tree (logs + session dumps). Scripted suites write to the pinned scratch root (scripts/lib/tmp-ban.sh), never into the repo.",
		matches: func(p string) bool {
			return hasPathPrefix(p, tmpE2EDirName) || hasPathPrefix(p, e2eRankDirName)
		},
	},
	{
		why: "a root-level merge/leak-probe leftover. These names are legitimate INSIDE a test temp dir; only the repo-root copy is the defect.",
		matches: func(p string) bool {
			// Repo root only: no directory component.
			return strings.IndexByte(p, '/') < 0 && rootScratchFiles[p]
		},
	},
	{
		why: "a point-in-time report doc parked in the repo root — 37 of them accumulated there in 2026-08..09 (see devdoc/repo-workspace-hygiene-2026-09-12.md). A report belongs under reports/archive/<YYYY-MM>/, a design narrative under devdoc/; the root's markdown surface is exactly rootDocAllowlist, and it grows only by a reviewed entry here.",
		matches: func(p string) bool {
			// Repo root only: no directory component. Tracked docs inside
			// reports/, devdoc/, tasks/ … are deliberately out of scope —
			// only the ROOT parking lot is the defect.
			return strings.IndexByte(p, '/') < 0 && strings.HasSuffix(p, rootMarkdownSuffix) && !rootDocAllowlist[p]
		},
	},
	{
		why: "a compiled binary tracked at the repo root. `CGO_ENABLED=0 go build ./cmd/agent/` from the root writes ./agent (one main package, no -o) — the 19 MB debris the sweep found. Build to bin/gopi (the declared build) or pass -o.",
		matches: func(p string) bool {
			return strings.IndexByte(p, '/') < 0 && rootBuildOutputs[p]
		},
	},
	{
		// INDEX-ONLY class, deliberately: it has no .gitignore sample below, so
		// an untracked `scratch/` still shows up as `??` in `git status` instead
		// of being hidden by an ignore rule. That is the operator's standing
		// ruling — "ignore rules hide writers" — and it is the diagnosing half
		// Rule 30 relies on (untracked litter is RECORDED, not silenced). What
		// this class refuses is the state that made the invention look licensed:
		// a COMMITTED `scratch/` inside a checkout.
		why: "a repo-relative `scratch/` tree. gopi's scratch root is `tmpdir` — default `<home>/workspace/_tmp`, resolved by config.ResolveTmpDirConfigured and published as TMPDIR for gopi and every child (off `/`, Rule 22) — never a directory inside a checkout. A committed `scratch/` is what makes the invented root look like an existing convention: three such paths (introduced 2026-09-01 by 6cf7d1ef) were the causal precondition of the 135 MB untracked residue of 2026-09-26 (devdoc/scratch-repo-residue-01a0db54.md). Evidence belongs in the run's OWN session dir (~/.gopi/sessions/<task-id>/, outside every checkout) or — for a program that must execute — a sandbox (Rule 34).",
		matches: func(p string) bool {
			return hasPathPrefix(p, repoScratchDirName)
		},
	},
	{
		// ROOT-ANCHORED PREFIX class, and the anchoring is the whole point of
		// it: see the `why` below and the .gitignore comment that pairs with
		// this rule. The ignore half of the ratchet lives in junkIgnoreSamples.
		why: "a root-level build output produced by an explicit `-o` (`go build -o gopi-behind ./cmd/agent/` — the tracked A/B recipe tasks/01a0cea0-rule31-two-states.sh line 35, and the compare runs in devdoc/ that build ./gopi-before / ./gopi-fixed). Compiled output is not an artifact of the repo: build to bin/gopi (the declared build) or pass an -o under the pinned scratch root, never into the checkout root. Matched as a ROOT path carrying the `gopi-` prefix, and that restriction is exactly what keeps it off cmd/agent/* and internal/agent/* — the 2026-09 accident was an UNANCHORED `agent`/`gopi` pattern, which matched those two DIRECTORY names at every depth.",
		matches: func(p string) bool {
			return strings.IndexByte(p, '/') < 0 && strings.HasPrefix(p, gopiRootArtifactPrefix)
		},
	},
}

const (
	// rootMarkdownSuffix is the extension the root-report-doc class keys on.
	rootMarkdownSuffix = ".md"
)

// rootDocAllowlist is the complete set of markdown docs that may be TRACKED at
// the repo root: the release surface and the documents of record. Every other
// root doc is a parked report and belongs under reports/ or devdoc/.
//
// PROJECT.md IS here (2026-10-19) because the root rule file was SPLIT BY
// CONTENT KIND: Rules 1-41 are the project's engineering contract — they hold in
// a bare clone on any machine (internal Go standards, internal/standards gates,
// devdoc references, `make test-budget`) — so they must travel with the code,
// and the file that carries them is tracked beside CONFIG.md and COMMANDS.md. It
// is the rule file a reader is sent to, so it belongs where the reader looks;
// its own gate is TestRepoRootHasNoTrackedJunk's clean half plus the docclass
// manifest, and it is read by the pack's doc gates in place of the ignored file
// (see each gate's instruction-file constant).
//
// GOPI.md is deliberately NOT here (2026-10-04, unchanged by the split): it is
// the CLIENT's own rule file — this machine's operations policy. gopi still
// loads it from the run's cwd (internal/agent's project-context walk accepts it
// as the SECOND candidate name), but git must not carry one machine's local
// policy into every other machine's checkout — so the file is ignored at the
// root like any other root doc and the copy each host runs is its operator's.
// Adding it back here would re-track the file the moment a root-doc path were
// committed, which is exactly the propagation this gate now refuses. The read
// half is pinned by TestRepoRootGopiDocIsUntrackedButReadable in this package,
// and the tracked half by TestRepoRootProjectDocIsTrackedAndReadable.
//
// CONFIG.md is a DOCUMENT OF RECORD, not a report: it is the single reference
// for every knob (env / settings key / flag / precedence), so its path is part
// of the product the way GOPI.md's is — an operator looks for it at the root.
// It is kept honest by the machines that still read it — the §2c flag-column
// gate, the sandbox-shape walk, the root-doc rule. The ANCHOR CENSUS an author
// used to run (`internal/standards/testdata/config-anchor-census.go`) went with
// the documentation-citation family in 78bdf05a (2026-11-05) — as did the
// document gate that once asserted its anchors, which left this package whole in
// the 2026-10-22 doc-pipeline removal. CONFIG.md says so in its own prose: no
// machine judges whether an anchor names the line that DECIDES the row.
//
// COMMANDS.md is the second document of record: the manual for every top-level
// verb the binary dispatches, with the same anchor discipline (each claim names
// the file:line that decides it). Its verb table is generated
// from cmd/agent/cli_verbs.go + the contract suite's token map, so the root is
// the right home for a page a user is sent to.
// ProjectDocName is the TRACKED root rule file's name — the project's
// engineering contract (Rules 1-41). It is spelled once here and imported by the
// doc gates that scan instruction prose, so the 2026-10-19 split moved ONE
// constant instead of five literals (Rule 1b/14).
const ProjectDocName = "PROJECT.md"

var rootDocAllowlist = map[string]bool{
	"CHANGELOG.md": true,
	"CONFIG.md":    true,
	"COMMANDS.md":  true,
	// README.md is the FACE of the project — the one root document written for
	// someone who has not installed anything yet (what it is, what it can do,
	// how to get a `gopi` on their PATH). Its siblings above are all written for
	// a reader who ALREADY runs gopi and is looking something up: COMMANDS.md is
	// derived from the source, CONFIG.md is the per-knob reference. Without this
	// entry a checkout explains its verbs and its env vars but never says what
	// the program IS — which a 17-dimension comparison against 18 peers ranked
	// as the single real gap (devdoc/competitor/summary.md §5). It is a
	// document of record, not a parked report: it is tracked, reviewed in place,
	// and the `make install` target it documents lives in the Makefile beside it.
	"README.md": true,
	// The project's engineering contract (Rules 1-41), tracked so every clone
	// and worktree carries it — the half of the 2026-10-19 root-file split that
	// is machine-independent. GOPI.md stays absent: that half is the client's.
	ProjectDocName: true,
}

// rootBuildOutputs are the binary names a no-`-o` build drops at the repo root.
// Compiled output is never a tracked artifact: bin/ is the build home and the
// declared build writes bin/gopi.
var rootBuildOutputs = map[string]bool{
	"agent": true, // `go build ./cmd/agent/` from the root
	"gopi":  true, // `go build .` from a root main package
}

const (
	// testBinarySuffix is what `go test -c <pkg>` names its output by default.
	testBinarySuffix = ".test"
	// leakProbeDirPrefix covers .leakprobe/, .leakprobe2/ and the root files
	// .leakprobe-agent.test / .leakprobe-full.out — every artifact that probe
	// left behind. Matched as a root-anchored PREFIX, not a directory name:
	// the red-proof showed an exact-dir check misses .leakprobe2/.
	leakProbeDirPrefix = ".leakprobe"
	// tmpE2EDirName / e2eRankDirName are the two hand-rolled e2e output trees
	// that were committed.
	tmpE2EDirName  = "tmp-e2e"
	e2eRankDirName = "e2e-m420-rank"
	// repoScratchDirName is the repo-RELATIVE `scratch/` tree — the directory a
	// run invents at a checkout root when nobody handed it a destination. It is
	// NOT gopi's scratch root: that is `tmpdir` (default `<home>/workspace/_tmp`,
	// `config.ResolveTmpDirConfigured` + `internal/config/tmpdir.go`) and the
	// suite scratch home, both OUTSIDE every checkout (Rule 22's criterion).
	// Naming it once here keeps the class out of a bare literal at the call site
	// (Rule 13/14).
	repoScratchDirName = "scratch"
)

// rootScratchFiles is the allow-refusal list of repo-root leftovers from the
// merge/leak probes: `shared.txt` and friends were seed files a probe typed into
// the repo root (the same names also appear in `_test.go` temp dirs, which is
// why the check is anchored to the root).
var rootScratchFiles = map[string]bool{
	"w.txt":               true, // probe: one-line scratch
	"y.txt":               true, // probe: one-line scratch
	"base.txt":            true,
	"div.txt":             true,
	"ff.txt":              true, // merge-probe branch seed files
	"ff3.txt":             true,
	"shared.txt":          true,
	"master-side.txt":     true,
	"ort-client.txt":      true,
	"ort-master.txt":      true,
	"probe-ff-note.txt":   true,
	"result.json":         true,
	"s6.png":              true,
	"text2img":            true, // one-off render script, superseded by internal/text2img
	"memory.db":           true, // zero-byte sqlite a probe left with a relative --db
	"gopi.db":             true,
	"recycle-epic-marker": true,
	"worktree-owner":      true,
}

// hasPathPrefix reports whether p is root itself or lives under it.
func hasPathPrefix(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

// trackedPaths returns every tracked repo-relative path. It FAILS rather than
// returning an empty list on error: a gate that reads green-blind (git missing,
// wrong cwd) is worse than no gate, so "no paths" is an error, not a pass.
func trackedPaths(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command(gitBinary, gitLsFilesArgv...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %s in %s: %v", gitBinary, strings.Join(gitLsFilesArgv, " "), root, err)
	}
	paths := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	nonEmpty := paths[:0]
	for _, p := range paths {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) == 0 {
		t.Fatalf("%s %s returned no paths in %s — the gate cannot read the index (refusing to pass blind)",
			gitBinary, strings.Join(gitLsFilesArgv, " "), root)
	}
	return nonEmpty
}

// junkTrackedPaths is the pure predicate: the tracked paths matching any junk
// class, rendered as "<path>: <why>" lines. Split from the test so the red-proof
// can feed it a synthetic index instead of poisoning the real one.
func junkTrackedPaths(paths []string) []string {
	var hits []string
	for _, p := range paths {
		for _, check := range junkPathChecks {
			if check.matches(p) {
				hits = append(hits, p+": "+check.why)
				break
			}
		}
	}
	return hits
}

// ---------------------------------------------------------------------------
// .gitignore coverage — the other half of the ratchet
//
// The index gate above stops a junk path from being COMMITTED. This one stops it
// from being committed BY ACCIDENT: an unignored junk path shows up in
// `git status` as `??`, one `git add` away from the same defect. The two rules are
// complementary and neither subsumes the other (.gitignore does not apply to an
// already-tracked path; the index gate cannot see an untracked one).
//
// It is a name-level check (the paths need not exist), so it also pins the
// patterns for classes whose debris was deleted from disk.
// ---------------------------------------------------------------------------

// gitCheckIgnoreArgv asks git which of the given paths its ignore rules match.
// --no-index keeps an already-tracked path visible to the rule (so the answer
// describes the pattern, not the index state), -v makes the output carry the
// matching pattern for a readable failure.
var gitCheckIgnoreArgv = []string{"check-ignore", "--no-index", "-v", "--"}

// gitignoreSample is one representative path per junk class plus the pattern
// every instance of that class must be kept out by.
type gitignoreSample struct {
	// path is a repo-relative path of the class (need not exist on disk).
	path string
	// why is the reason printed when the path is NOT ignored.
	why string
}

// junkIgnoreSamples walk every junk class so a typo in one .gitignore line
// (a missing dot, a missing entry) fails the gate instead of lying dormant.
var junkIgnoreSamples = []gitignoreSample{
	{".leakprobe-agent.test", "a compiled test binary"},
	{".leakprobe-full.out", "a probe stdout dump"},
	{".leakprobe/home/.gopi/memory.db", "a copied real knowledge base"},
	{".leakprobe2/home/.cache/go-build/00/x-d", "a copied go-build cache entry"},
	{"tmp-e2e/session-logs/run1.err", "a hand-written e2e log"},
	{"e2e-m420-rank/a/.gopi/memory.db", "an ad-hoc e2e sandbox db"},
	{"w.txt", "a root scratch file"},
	{"y.txt", "a root scratch file"},
	{"base.txt", "a root merge-probe seed"},
	{"shared.txt", "a root merge-probe seed"},
	{"result.json", "a root probe artifact"},
	{"s6.png", "a root probe image"},
	{"memory.db", "a root probe sqlite file"},
	{"gopi.db", "a root probe sqlite file"},
	{"text2img", "the superseded root render script"},
	{".recycle-epic-marker", "a sweeper marker"},
	{".worktree-owner", "a worktree marker left at the root"},
	// The parking-lot class (2026-09): report docs parked at the repo root and
	// root build outputs. The `/*.md` rule must not be deleted to "make a report
	// visible" — a reviewed root doc goes into rootDocAllowlist, which holds the
	// negations asserted trackable in junkIgnoreAllowed below (CHANGELOG.md,
	// CONFIG.md, COMMANDS.md and, since the 2026-10-19 split, PROJECT.md).
	{"report-r3.md", "a parked root report doc"},
	{"FINAL-VERIFICATION-REPORT.md", "a parked root report doc"},
	{"agent", "the root build output (`go build ./cmd/agent/` with no -o)"},
	{"gopi", "the root build output"},
	// The root-artifact PREFIX class (an explicit `-o`). The tracked A/B recipe
	// builds ./gopi-behind at the checkout root, and the compare runs the
	// devdoc/ narratives describe build ./gopi-before / ./gopi-fixed the same
	// way. Three spellings, one rule — the samples walk the prefix, they do not
	// define it.
	{"gopi-behind", "a root build output from an explicit -o"},
	{"gopi-before", "a root build output from an explicit -o"},
	{"gopi-fixed", "a root build output from an explicit -o"},
	// The ctags index's IN-FLIGHT temp (internal/tools/symbol.go tagsIndexTempPrefix
	// = tagsIndexFileName + "-tmp-"). It lands in the INDEX's own directory and is
	// renamed into place, so a code_search rebuild from a package subdirectory
	// materialises `<that dir>/.gopi-tags-tmp-<random>` for its lifetime. The path
	// below is the one measured on this host on 2026-10-31: it appeared under
	// cmd/agent while the suite ran, `.gitignore` ignored only `.gopi-tags` (an
	// EXACT name), `git check-ignore -v <that path>` exited 1, and
	// `git ls-files --others --exclude-standard` reported it — which
	// TestSyncBarePeersTestCoversEveryChangedFile reads as "this run created it"
	// and reds on. Its name carries a random component, so the pattern must cover
	// the PREFIX, not this sample.
	{"cmd/agent/.gopi-tags-tmp-2642549001", "an in-flight ctags index temp (a publish artifact of code_search)"},
}

// junkIgnoreAllowed are paths that MUST stay trackable: the anchored patterns
// exist precisely so these same basenames remain valid where they belong.
var junkIgnoreAllowed = []gitignoreSample{
	{"cmd/agent/main.go", "the real source tree"},
	{"internal/standards/scratch_gate.go", "this gate"},
	{"devdoc/base.txt", "a file that merely shares a basename with a root-anchored pattern"},
	// The negations in the /*.md rule. CHANGELOG.md is the release surface;
	// CONFIG.md and COMMANDS.md are documents of record; PROJECT.md is the
	// project's engineering contract (the tracked half of the 2026-10-19 split).
	// If a negation is dropped, this gate fails.
	// (PROJECT.md IS listed above: the 2026-10-19 split moved the project's own
	// rules into it, and a tracked root doc must stay trackable through the
	// `/*.md` rule. GOPI.md is deliberately ABSENT: it is not whitelisted at the root —
	// it is the client's own rule file, ignored like any other root doc and
	// carried by each host's operator rather than by git. The read half lives in
	// TestRepoRootGopiDocIsUntrackedButReadable; bin/agent is likewise absent
	// because bin/ is the ignored build dir, so "ignored" is the correct answer
	// there. The rule this fixture protects is "a pattern must not swallow a
	// subdirectory path that is not the class".)
	{"CHANGELOG.md", "the release surface"},
	{"CONFIG.md", "the configuration reference of record"},
	{"PROJECT.md", "the project's engineering contract (Rules 1-41), tracked so every clone carries it"},
	{"reports/archive/2026-08/report-r3.md", "a report in its proper home"},
	{"devdoc/workspace-hygiene-report.md", "a narrative in its proper home"},
	// The root-artifact prefix must not reach into the source tree. These two
	// are the exact paths the 2026-09 unanchored `agent`/`gopi` patterns
	// swallowed (the accident .gitignore records): neither directory carries a
	// component with the `gopi-` prefix, so the anchored class leaves them
	// trackable — and if the class is ever loosened to a bare name, this list
	// fails the build instead of quietly ignoring the source tree.
	{"cmd/agent/main.go", "the agent's own main package"},
	{"internal/agent/loop.go", "the agent loop's package"},
}

// ignoredPaths returns the subset of paths git's ignore rules match. Exit status
// 1 means "none matched" (git's own contract, not an error); anything else is a
// failure — a check-ignore that could not run must never read as "all clear".
func ignoredPaths(t *testing.T, paths []string) map[string]bool {
	t.Helper()
	root := repoRoot(t)
	argv := append(append([]string{}, gitCheckIgnoreArgv...), paths...)
	cmd := exec.Command(gitBinary, argv...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("%s %s: %v\n%s", gitBinary, strings.Join(gitCheckIgnoreArgv, " "), err, cmd.Stderr)
		}
	}
	ignored := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		// "<pattern>:<line>:<source>\t<path>"
		tab := strings.LastIndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		// `check-ignore -v` prints the LAST pattern that matched, whether that
		// pattern ignores the path or un-ignores it. A negation therefore shows
		// up in the output while the path is NOT ignored — counting it as
		// ignored would invert the rule it documents. The head is
		// "<source>:<line>:<pattern>"; a pattern may itself contain ':' (our
		// "a regex: with a colon" shapes do), so split only the first two.
		head := line[:tab]
		parts := strings.SplitN(head, ":", 3)
		if len(parts) != 3 {
			continue
		}
		ignored[strings.TrimPrefix(line[tab+1:], "./")] = !strings.HasPrefix(parts[2], "!")
	}
	return ignored
}

// gopiRootArtifactPrefix is the one spelling of the root-artifact PREFIX class:
// `<repo>/gopi-<variant>` — the A/B builds an explicit `-o` drops at the
// checkout root (`gopi-behind`, `gopi-before`, `gopi-fixed`, `gopi-cs-bin`, …).
//
// It is a PREFIX and not a name list on purpose: a list is only ever as long as
// the variants someone remembered, while the class is "whatever the last build
// named". It carries the hyphen because it must NOT cover the no-`-o` names —
// `gopi` (rootBuildOutputs) is a different producer with its own reason, and a
// bare `gopi` prefix would also swallow any future `gopifoo` typo-squat.
//
// The `gopi-` prefix is what makes this class safe where the 2026-09
// unanchored `agent`/`gopi` patterns were not: neither cmd/agent/ nor
// internal/agent/ has a path component starting with `gopi-`, and the gate
// anchors it to the ROOT as well (strings.IndexByte(p, '/') < 0). Both
// restrictions are asserted in TestRepoRootJunkGateRedProof's clean half, so a
// later "simplification" to a bare `gopi` fails the build instead of silently
// re-ignoring the source tree.
const gopiRootArtifactPrefix = "gopi-"
