package testutil

// The SCRIPTS half of the /tmp ban (the Go suites' half lives in
// scratchroot.go). scripts/ is the other entry point named by the epic: the
// 2026-09-11 exhaustion came from an UNBOUNDED shell loop (`while :; do dd ...
// conv=fsync; done`) writing into /tmp, while suites abandoned 13,044 scratch
// directories there. A convention that is only documented regresses; this file
// makes the convention a build failure.
//
// Four mechanical gates, all zero-tolerance (no allowlist beyond the ban's own
// text files, because there is no legitimate reason for a dev/test script to
// write into /tmp or to spin unboundedly):
//
//  1. `bash -n` parses every scripts/*.sh (a syntactically broken helper is a
//     broken entry point).
//  2. no unbounded loop header (`while :` / `while true` / `[[ 1 ]]` /
//     `until false` / Python `while True:`) — an iteration cap or `timeout`
//     wrapper is required instead.
//  3. no /tmp literal outside the ban's own documentation/helper files.
//  4. every script that builds scratch data of its own ROUTES to
//     scripts/lib/tmp-ban.sh / scripts/lib/tmp_ban.py with a real source/import
//     STATEMENT, and every script that routes builds nothing of its own — a
//     MENTION of the helper (a comment, an `echo`, even a `[ -r file ]` probe)
//     is not routing, and a routed script that still calls `mktemp` hands its
//     path to $TMPDIR, i.e. to /tmp. (Tightened 2026-10-07, task
//     01a1137f90d07f10a4d41f9227190ceb: the previous predicate was
//     `strings.Contains(body, "tmp-ban.sh")` over ALL lines, so an offender that
//     named the helper only in prose — or that added a `source` line while
//     KEEPING its own `mktemp -d` — passed. Both were measured, see
//     devdoc/scratch-gate-shell-routing.md.)
//
// The scan asserts it actually SAW script files, so a moved directory can
// never turn this gate into a vacuous pass.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// scriptsDirName is the directory under the repo root this gate audits.
	scriptsDirName = "scripts"
	// devdocDirName / testdataDirName hold the OTHER dev/test script entry
	// points: one-off harnesses committed next to their findings (devdoc) and
	// the smoke driver (testdata). They are audited by the same three rules —
	// a harness that writes into /tmp or spins forever is exactly the
	// 2026-09-11 failure mode, whatever directory it lives in.
	devdocDirName   = "devdoc"
	testdataDirName = "testdata"
	// auditedScriptSuffix is the extension both the parse check and the text
	// rules apply to.
	auditedScriptSuffix = ".sh"
	// scratchHelperShell / scratchHelperPython are the shared helper files
	// every scratch-creating script must source/import: they carry the
	// off-`/` assertion and the trap-time cleanup.
	scratchHelperShell  = "lib/tmp-ban.sh"
	scratchHelperPython = "lib/tmp_ban.py"
	// scratchTempCommandShell is the ONE shell command that builds a scratch
	// path the CALLER chooses: `mktemp` honours $TMPDIR, and $TMPDIR is /tmp on
	// this host — i.e. the boot device the 2026-09-11 exhaustion filled. The
	// helper is the only sanctioned construction (Rule 13: the name the ban
	// hinges on is spelled once).
	scratchTempCommandShell = "mktemp"
	// scratchTempCommandPythonDir / scratchTempCommandPythonFile are the
	// equivalent constructions on the Python side.
	scratchTempCommandPythonDir  = "mkdtemp"
	scratchTempCommandPythonFile = "mkstemp"
	// scratchTempRootVar is the environment variable that decides where an
	// unrouted temp path lands. A `mkdir -p "$TMPDIR"` is the same defect one
	// step removed: the path is the ambient temp root's, whoever made the
	// directory.
	scratchTempRootVar = "TMPDIR"
	// shellSourceVerbs are the shell verbs that ROUTE to a file: POSIX `.` and
	// bash's `source`. A line naming the helper without one of them (an `echo`,
	// a `[ -r … ]` probe, a variable assignment alone) documents or probes the
	// contract — it does not enter it.
	shellSourceVerbDot   = "."
	shellSourceVerbPosix = "source"
	// gateMinTrackedScripts is the floor for the COVERAGE proof below: the walk
	// must account for at least this share of the .sh/.py files the repository
	// TRACKS. It is not a file count — it is the statement that the gate's scope
	// is the tree and not a corner of it, which is the defect the 2026-10-07
	// widening repaired (eleven tracked scripts were building /tmp scratch in
	// directories no root covered). Measured at the widening commit: 333 of 363
	// tracked files were collected, and the 30 the walk skips by design are the
	// 3 gate FIXTURES under internal/testdata (input data, deliberately unfixed),
	// 2 internal/tools test data probes, the 1 top-level blast-radius.sh (it
	// ROUTES the helper — it is excluded by location, not by defect) and the 16
	// plugins/ files of a deployed remote service.
	gateMinTrackedCoveragePermille = 850
	// gateMinScriptFiles guards against a vacuous pass when the scan finds
	// (almost) nothing. It was 12 while the walk covered three roots; since the
	// 2026-10-07 widening (task 01a11397-ef8e) the same walk covers the whole
	// tree, and it COLLECTED 333 files out of 363 tracked .sh/.py at the commit
	// that widened it (the count and its generator are in
	// devdoc/scratch-gate-root-widening.md). The floor is set below that
	// measurement with room for ordinary churn, because its job is to catch a
	// walk that stopped looking, not to freeze the file count.
	gateMinScriptFiles = 300
	// gateMinTempPackages guards the same way for the Go-side pin gate: the
	// repo has many temp-using test packages, so a scan that finds almost none
	// is looking in the wrong place.
	gateMinTempPackages = 5
	// gateMinTestGoFiles guards the Go-test half of the /tmp gate the same way:
	// the repo has hundreds of test files, so a scan that finds a handful is
	// looking in the wrong place.
	gateMinTestGoFiles = 100
	// archiveDirName holds historical material (a former implementation, kept
	// for reference) — it is not built, not run, and not audited.
	archiveDirName = "archive"
	// gateMaxReportedHits caps how many offending sites one gate LISTS. It is a
	// readability cap, never a disclosure cap: every capped loop calls
	// gateReportWithheldHits, which states the total and how many lines were
	// withheld, so a red report can never under-state its own blast radius.
	// (2026-09-16: the worker-ownership gate printed "… (truncated)" after 20
	// hits with no total, and a reader treated a partial list as the whole
	// defect — devdoc/worker-ownership-gate-full-report-20260916.md.)
	gateMaxReportedHits = 20
	// tmpPathPrefix is the banned path PREFIX. It is spelled once here so the
	// gate never matches a longer word that merely contains the three
	// letters (the shared helper's own name does: "tmp-ban.sh").
	tmpPathPrefix = "/tmp" + "/"
)

const (
	// scratchHomeSetupScript is RETIRED (operator ruling, 2026-09-18): the scratch
	// home stays on the block device, so the script no longer mounts a tmpfs or
	// links to one. It is kept as the record of the fsync measurement that
	// motivated the memory backing, and the test below pins its two live
	// contracts: it changes NOTHING, and it never creates the self-referential
	// link that would point a directory at its own ancestor (the observed
	// 2026-09-11 stray on this host).
	//
	// The retired routes are exercised by
	// TestSetupScratchHomeIsRetiredAndChangesNothing below.
	scratchHomeSetupScript = "setup-scratch-home.sh"
	// scratchMemoryBaseDefault is the memory-backed base the setup script
	// defaults to. A test that runs the script MUST point
	// GOPI_SCRATCH_MEMORY_BASE at a private temp base instead (never touch the
	// host's real scratch tree).
	//
	// It is an ALIAS of the one definition (Rule 14, worktreeroots.ShmDir):
	// the value used to be a second literal here, which is the mirror that
	// drifts. The host's real scratch home is no longer memory-backed (the
	// 2026-09-18 ruling retired that route) and the script is inert, so on
	// this host the fixture path is simply unusable and the test SKIPS — which
	// is the honest reading, not a reason to keep a copy of the value.
	scratchMemoryBaseDefault = worktreeroots.ShmDir
	// scratchSetupTestBasePrefix names the private base a route test creates
	// under the memory base.
	scratchSetupTestBasePrefix = "gopi-scratch-home-test-"
	// scratchSetupTreeName is the scratch tree the setup script creates inside
	// the memory base (<base>/gopi-test-scratch).
	scratchSetupTreeName = "gopi-test-scratch"
	// scratchSetupHomeName is the scratch home a route test aims the script at
	// (a path OUTSIDE the memory tree — the healthy route).
	scratchSetupHomeName = "setup-home"
	// scratchMemoryBaseEnvName lets a test point the setup script's memory base
	// at a private directory instead of the host's real /dev/shm. It is this
	// package's own ALIAS of the ONE definition (Rule 14) —
	// scratch.MemoryBaseEnvName, surfaced here through this package's own
	// alias (tmproot.go), not a third spelling — so the name a test writes and
	// the name the setup script reads cannot drift; this test drives the script
	// the variable names.
	scratchMemoryBaseEnvName = MemoryBaseEnvName
)

// tmpLiteralFiles are the files allowed to carry the /tmp literal: the ban's
// own helpers (their messages quote /tmp to explain WHY it is banned). Every
// other script must reach the scratch tree through those helpers.
var tmpLiteralFiles = map[string]bool{
	"scripts/" + scratchHelperShell:  true,
	"scripts/" + scratchHelperPython: true,
}

// unboundedLoopRes match a loop header that can spin forever: bash `while :`,
// `while true`, `while [ 1 ]`, `while [[ 1 ]]`, `until false`, and the Python
// `while True:` form.
var unboundedLoopRes = []*regexp.Regexp{
	regexp.MustCompile(`^\s*while\s+(:\s*;|:$|true\s*;|true$|\[\s*1\s*\]|\[\[\s*1\s*\]\])`),
	regexp.MustCompile(`^\s*until\s+(false\s*;|false$|:\s*;|:$)`),
	regexp.MustCompile(`^\s*while\s+True\s*:`),
}

// timeoutWrapperRes recognise a wall-clock bound: a `timeout` command or a
// deadline/iteration guard the loop body compares against.
var timeoutWrapperRes = []*regexp.Regexp{
	regexp.MustCompile(`\btimeout\b`),
	regexp.MustCompile(`\bdeadline\b|\bSECONDS\b|\$\{?SECONDS\b`),
	regexp.MustCompile(`\b-?(ge|gt|lt|le)\b`),
	regexp.MustCompile(`\+\+|\+= *1|i\+\+|max_?(iter|attempts|rounds)`),
}

// deviceLoadRes match a command that saturates the SHARED DEVICE, which no
// bound makes acceptable. This is the 2026-09-16 incident's shape and the reason
// the rule cannot be about loop bounds: `iopress.sh` used a PERFECTLY BOUNDED
// loop (`for ((i=0; i<ITERS; i++))` with ITERS defaulting to 4000) and therefore
// satisfied "an iteration cap is required" while opening 48 concurrent
// `dd ... oflag=direct conv=fsync` writers against the one block device. A bound
// on the number of rounds says nothing about how much damage each round does.
//
// The ban (PROJECT.md Rule 27) is absolute: our system has no requirement that
// needs device saturation, and the cheapest way to satisfy a pressure KPI is to
// flood the device rather than to reproduce the behaviour under test. So these
// are matched ANYWHERE in an audited script, inside or outside a loop.
var deviceLoadRes = []*regexp.Regexp{
	// O_DIRECT / fsync'd writers: the exact shape of the 8/27, 9/11 and 9/16
	// incidents. `oflag=direct` bypasses page cache; `conv=fsync` forces the
	// device; both together make each round a real device write.
	regexp.MustCompile(`\boflag=direct\b`),
	regexp.MustCompile(`\bconv=fsync\b`),
	regexp.MustCompile(`\bdirect=1\b|\\bdsync\b`),
	// A dd writing zeroes into a file: the generator's inner command.
	regexp.MustCompile(`\bdd\b[^|;]*\bif=/dev/(zero|urandom)\b`),
	// fio is a load generator by definition.
	regexp.MustCompile(`\bfio\b\s`),
	// Explicit pressure / stress helpers, the thing Rule 27 names.
	regexp.MustCompile(`\b(stress-ng|stress|iopress|ioblitz|loadgen)\b`),
}

// deviceLoadExemptRels are files allowed to NAME the banned shapes: the rule's
// own documentation and the gate itself (its messages quote the pattern). Every
// other audited file must not carry them.
var deviceLoadExemptRels = map[string]bool{
	"internal/testutil/scripts_gate_test.go": true,
	// The /tmp ban's own helpers quote the 2026-09-11 generator in their
	// comments to explain WHY the ban exists. Naming a past incident in prose is
	// documentation, not a generator — the detector strips comments (see
	// isCommentLine) before matching, so these are exempt only for their prose;
	// a real `dd` in either file would still fail.
	"scripts/lib/tmp-ban.sh": true,
	"scripts/lib/tmp_ban.py": true,
}

// isCommentLine reports whether a source line is a comment whose body must not
// be scanned for banned commands. Both incidents we know of were NAMED in
// comments by the very documents banning them, and a detector that cannot tell
// a quotation from a command is a detector that gets relaxed to a no-op the
// first time it fires on prose (the argv-check precedent). Shell and Python
// share the '#' marker; a shebang is a comment too.
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// scriptFile mirrors one audited file.
type scriptFile struct {
	rel   string   // repo-root-relative, slash-separated
	lines []string // file content split into lines
	shell bool     // true for *.sh, false for *.py
}

// TestScriptsAreBoundedAndOffTmp is the whole gate: parse check + the three
// text rules. It is one test (not four) so the failure output reads as a single
// audit of scripts/, and so a future contributor fixes scripts/ once.
func TestScriptsAreBoundedAndOffTmp(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	files := collectScriptFiles(t, root)
	if len(files) < gateMinScriptFiles {
		t.Fatalf("the scripts audit found %d files (want at least %d) — the scan is not looking where the scripts live (vacuous pass guard)",
			len(files), gateMinScriptFiles)
	}
	assertScopeCoversTheTrackedTree(t, root, len(files))

	for _, f := range files {
		if f.shell {
			assertShellParses(t, root, f)
		}
		assertNoUnboundedLoop(t, f)
		assertNoDeviceLoad(t, f)
		assertNoTmpLiteral(t, f)
	}
	assertScratchHelpersAreUsed(t, files)
	// The Go half of the same ban: the audit found the /tmp writes had survived
	// in the TEST sources (the scripts were gated, the tests were not).
	assertTestSourcesCreateNoTmpPath(t, root)
}

// TestEveryTempUsingSuiteIsPinned is the BOUNDED-BLAST-RADIUS guard for the
// Go half of the ban: any package whose tests create scratch directories must
// have a TestMain that pins the scratch root through internal/testutil.
//
// Without it the pin is opt-in, and the failure mode is silent: a new package
// (or a newly added t.TempDir() in an existing one) puts its scratch on the
// ambient temp root — i.e. /tmp, the 87%-full partition the 2026-09-11
// exhaustion filled — while `go test ./...` stays green. The gate is static:
// it reads the test sources, not the runtime.
//
// The evidence it accepts is a TestMain that pins the scratch root, in EITHER
// recognised shape, and the two are read differently on purpose: the in-tree
// marker strings (pinEntryPointMarkers) for a package of the gopi module, and
// the TestMain BODY (pin_redirect_scan_test.go) for the SPECIFIC remedy the
// gate's FAIL text hands a package in a nested standalone module — the in-tree
// import is refused by Go there, so such a module redirects the pin env name in
// its own TestMain instead. A comment naming that variable is not a pin, and
// pointing it at the ban's own root, at os.TempDir(), or at what the variable
// already holds is refused as the ambient root by definition.
func TestEveryTempUsingSuiteIsPinned(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	scanned := 0
	for _, rel := range testPackageDirs(t, root) {
		src := readTestSources(t, filepath.Join(root, rel))
		if !usesScratchTemp(src) {
			continue
		}
		scanned++
		if packagePinned(t, root, rel) {
			continue
		}
		t.Errorf("%s", scratchPinFailure(root, rel))
	}
	if scanned < gateMinTempPackages {
		t.Fatalf("the pin gate audited only %d temp-using test packages (want at least %d) — "+
			"the scan is not looking where the tests live (vacuous pass guard)", scanned, gateMinTempPackages)
	}
}

// The remedy text the pin gate emits, spelled as named constants (Rules 1/13):
// the same sentences are asserted by the message regression test, so a bare
// literal in either place would be a second source of truth for what the gate
// TELLS a reader to do.
const (
	// scratchPinVerdict is the diagnosis half — true for BOTH shapes, because
	// the predicate is one predicate: a package whose tests create scratch and
	// whose TestMain is absent (or does not route the pin through
	// internal/testutil) puts that scratch on the ambient temp root.
	scratchPinVerdict = "creates scratch directories (t.TempDir()/os.MkdirTemp) but no TestMain " +
		"pins the scratch root — its scratch data lands on the ambient temp root, i.e. " +
		"/tmp on this host (the 87%-full / partition behind the 2026-09-11 exhaustion)"

	// scratchPinFixInTree is the remedy for an IN-TREE package (one that is part
	// of the gopi module): the pin goes through internal/testutil, which is the
	// one place the scratch root, the notify sandbox and the ledger sandbox are
	// installed.
	scratchPinFixInTree = "FIX (in-tree package): add a TestMain calling testutil.RunSuiteMain(m, %q) — " +
		"it pins the temp root that `go test` hands to THIS package into a directory off / (the repo's " +
		"scratch home) AND installs the notify/ledger sandboxes — see devdoc/tmp-ban-scratch-root.md"

	// scratchPinFixInTreeOwnRoot is the in-tree alternative for the case the
	// plain pin cannot cover: the scratch is created outside `go test`'s own
	// temp root (a child process, a fixture, a second module driven by the
	// test), so redirecting TMPDIR decides nothing. Both named levers are used
	// in this repo already.
	scratchPinFixInTreeOwnRoot = "FIX (in-tree, only if that pin is unavailable because the scratch is " +
		"created OUTSIDE `go test`'s temp root): create it under the repo's scratch home instead — " +
		"internal/scratch.MkdirTemp(class, pattern) or testutil.ScratchRootQueue(), or the home " +
		"testutil.ScratchHomeDir() reports (materialise it in TestMain and pass it to the child) — " +
		"which REFUSES a fallback to /. Judge that by DEVICE ID (the directory's device number must " +
		"differ from the device number of /), never by the path's spelling"

	// scratchPinFixNestedModule is the remedy for a package in a NESTED STANDALONE
	// MODULE — a module that is not the repo's, so the in-tree machinery above is
	// not merely inconvenient but absent: it runs on the repo's tempDir channel
	// and re-execs `go test` on the repo's own module.
	scratchPinFixNestedModule = "FIX (nested standalone module): this package is NOT in the gopi " +
		"module, so the in-tree remedy above does not exist here. The pin this gate asks for is the " +
		"same criterion Rule 22 states — this module's own scratch must not land on the / partition: " +
		"(1) REMOVE the scratch creation (keep the bytes in a read-only testdata/ fixture when the " +
		"temp file was only ever READ, or in a table when `go test` does not expose the root); " +
		"(2) if the scratch is genuinely produced at run time, redirect it in this module's OWN " +
		"TestMain (os.Setenv(\"TMPDIR\", dir) to a directory whose DEVICE ID differs from the id of " +
		"/), or any helper you write that refuses a fallback to the ambient root; (3) if the module " +
		"can do neither, report it — the criterion is off /, and a pin is only the repo's instrument " +
		"for it"

	// scratchPinWhyNestedNotImportable is the DISCLOSURE half: it states the
	// mechanism of the unavailability and names the unavailable species, because
	// that is the only way the eventual relaxation of this gate can be justified
	// on the record instead of by omission.
	scratchPinWhyNestedNotImportable = "WHY NOT the in-tree remedy here: it runs on the REPO's own " +
		"tempDir channel and re-execs `go test` on the REPO's module — neither exists for a module that " +
		"is not the repo's — and the import it needs is one Go refuses (no such module in this module's " +
		"graph; internal/ is decided by IMPORT PATH, so an import from another module is refused as " +
		"'use of internal package ... not allowed' even with a require+replace edge, which would also " +
		"pull the whole gopi dependency tree into a module declared zero-dependency)"

	// nestedModuleMarker is the file whose PRESENCE in the package's ancestors,
	// at or below the repo root, means the package belongs to a nested module
	// rather than to the gopi module. It is the same value the module-root
	// walkers in this repo use; spelled once, here (Rules 1/13).
	nestedModuleMarker = "go.mod"
)

// scratchPinPackageKind classifies a test package by the Go module it belongs
// to: the gopi module itself ("in-tree") or a standalone nested module ("nested").
func scratchPinPackageKind(root, rel string) string {
	if nestedModuleRoot(root, rel) != "" {
		return "nested"
	}
	return "in-tree"
}

// nestedModuleRoot returns the REL of the nearest ancestor directory of rel
// (including rel itself, never the repo root) that holds its own go.mod — the
// declaration of a nested standalone module. An empty result means the package
// belongs to the repo's own module.
func nestedModuleRoot(root, rel string) string {
	dir := strings.TrimSuffix(rel, "/")
	for dir != "" && dir != "." {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir), nestedModuleMarker)); err == nil {
			return dir
		}
		if i := strings.LastIndex(dir, "/"); i >= 0 {
			dir = dir[:i]
			continue
		}
		break
	}
	// The repo root's own go.mod is the gopi module, not a nested one.
	return ""
}

// scratchPinShapeTag names the module a package belongs to, so a reader knows
// WHICH remedy applies without having to look the path up.
func scratchPinShapeTag(root, rel string) string {
	if kind := scratchPinPackageKind(root, rel); kind == "nested" {
		moduleRoot := nestedModuleRoot(root, rel)
		if moduleRoot == strings.TrimSuffix(rel, "/") {
			return kind + ", standalone nested module rooted here"
		}
		return fmt.Sprintf("%s, standalone nested module under %s/", kind, moduleRoot)
	}
	return scratchPinPackageKind(root, rel)
}

// scratchPinFailure renders the pin gate's FAIL text for one offending package.
// It exists as ONE function (rather than an inline t.Errorf) because the two
// remedies are shape-dependent and the shape comes from the filesystem, not
// from the message: a nested standalone module cannot import
// internal/testutil, so telling it to is an instruction Go refuses.
func scratchPinFailure(root, rel string) string {
	if scratchPinPackageKind(root, rel) == "nested" {
		return fmt.Sprintf("%s (%s): %s. %s %s",
			rel, scratchPinShapeTag(root, rel), scratchPinVerdict,
			scratchPinFixNestedModule, scratchPinWhyNestedNotImportable)
	}
	return fmt.Sprintf("%s (%s): %s. %s. %s",
		rel, scratchPinShapeTag(root, rel), scratchPinVerdict,
		fmt.Sprintf(scratchPinFixInTree, filepath.Base(rel)), scratchPinFixInTreeOwnRoot)
}

// TestEveryTestMainSuiteIsNotifyHermetic is the STATIC half of the notify-env
// guard (Rule 21 — an invariant with a real reader, not a documented intent):
// every package whose tests carry a TestMain must route it through
// internal/testutil, because that is the only place the notify sandbox is
// installed (suiteenv.go). A hand-rolled TestMain that drops
// GOPI_TASK_NOTIFY_* and stops there is NOT hermetic — config.ResolveNotify
// falls through to the ambient GOPI_SETTINGS, the operator's real
// settings.json, whose notify.cmd appends to the LIVE sink. That is exactly
// what the 2026-09-12 incident measured (~200 test task-ids delivered into the
// live sink by one suite run). The runtime half of the guard is
// AssertSuiteNotifyEnvHermetic, called by the suites that own the risk.
func TestEveryTestMainSuiteIsNotifyHermetic(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	scanned := 0
	for _, rel := range testPackageDirs(t, root) {
		src := readTestSources(t, filepath.Join(root, rel))
		if !strings.Contains(src, "func TestMain(") {
			continue
		}
		// This package defines the entry points, so its own TestMain spells the
		// pin helpers' names to exercise them — it is the definition site, not a
		// consumer (the one allowed holder).
		if rel == testutilPackageDir {
			continue
		}
		scanned++
		if containsAny(src, pinEntryPointMarkers) {
			continue
		}
		t.Errorf("%s carries a TestMain that does not route through internal/testutil — the notify env "+
			"sandbox is installed ONLY there, so this suite reads the ambient GOPI_SETTINGS (the operator's "+
			"real settings.json, whose notify.cmd writes the live sink). FIX: call "+
			"testutil.PinSuiteForTestMain(%q) (or RunSuiteMain) in TestMain — see "+
			"internal/testutil/suiteenv.go and devdoc/sweep-zombies-dryrun-notify.md",
			rel, filepath.Base(rel))
	}
	if scanned < gateMinTestMainSuites {
		t.Fatalf("the notify-hermeticity gate audited only %d TestMain suites (want at least %d) — "+
			"the scan is not looking where the suites live (vacuous pass guard)", scanned, gateMinTestMainSuites)
	}
}

// testutilPackageDir holds the entry points the gate above insists on; its own
// TestMain is the definition site of the pin, so it is exempt.
const testutilPackageDir = "internal/testutil"

// TestAtMostOneTestMainPerSuite is the build-break guard: a Go test binary links
// EVERY *_test.go of a directory, whether it declares `package foo` or
// `package foo_test`, so a directory carrying two TestMain functions does not
// compile at all —
//
//	# github.com/nick/gopi/internal/daemoncwd
//	multiple definitions of TestMain
//	FAIL	github.com/nick/gopi/internal/daemoncwd [setup failed]
//
// and `go build ./...` stays GREEN while it does, because test files are not
// part of it. That is what made the real defect (2026-09-15) survive a merge
// wave: a stale branch's pin (740f28a6, under the conventional
// testmain_scratchroot_test.go name) and a RELAND of an earlier reverted merge
// (2bc93022, restoring testmain_test.go) each looked correct alone, and
// internal/daemoncwd was the only package in the repo that held both.
//
// The check is per DIRECTORY and counts every TestMain regardless of package
// clause — the internal/external split is exactly what makes the collision
// invisible by eye.
func TestAtMostOneTestMainPerSuite(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	suites := 0
	for _, rel := range testPackageDirs(t, root) {
		files := testSourceFileNames(t, filepath.Join(root, rel))
		var holders []string
		for _, name := range files {
			raw, rerr := os.ReadFile(filepath.Join(root, rel, name))
			if rerr != nil {
				t.Fatalf("read %s: %v", filepath.Join(rel, name), rerr)
			}
			if declaresTestMain(string(raw)) {
				holders = append(holders, name)
			}
		}
		if len(holders) == 0 {
			continue
		}
		suites++
		if len(holders) > 1 {
			t.Errorf("%s declares %d TestMain functions (%s) — a test binary links every *_test.go of the "+
				"directory, including the `_test` package, so this package does NOT COMPILE: "+
				"\"multiple definitions of TestMain\" — and `go build ./...` cannot see it, because test files "+
				"are not part of it. FIX: keep ONE entry point (the conventional name is "+
				"testmain_scratchroot_test.go) and delete or fold in the other — a RELAND of an earlier "+
				"reverted merge is the known way this returns",
				rel, len(holders), strings.Join(holders, ", "))
		}
	}
	t.Logf("one-TestMain gate: audited %d suites", suites)
	if suites < gateMinTestMainSuites {
		t.Fatalf("the one-TestMain gate audited only %d suites (want at least %d) — the scan is not looking "+
			"where the suites live (vacuous pass guard)", suites, gateMinTestMainSuites)
	}
}

// declaresTestMain reports whether a single test source declares the suite entry
// point. It is deliberately a line-prefix test rather than a substring one: the
// pin gates above spell "func TestMain(" in their own prose and doc comments, so
// a substring match would count a COMMENT as a definition and make this gate
// fire on its own package.
//
// The prefix test runs on scanViewCodeOnly(src), not on the raw source: a
// DECLARATION is never inside a comment or a literal (the compiler proves it),
// while a raw-string FIXTURE spelling the declaration is — and that was a
// measured false positive on this gate (internal/tools, 2026-10-31: the fixture
// body's `func TestMain` was counted as a second entry point for a package that
// compiles fine). See srcview_test.go for the boundary and why it is the
// opposite of the "never blank literals" rule that a defect-spelling gate needs.
func declaresTestMain(src string) bool {
	for _, line := range strings.Split(scanViewCodeOnly(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), testMainDeclPrefix) {
			return true
		}
	}
	return false
}

// testMainDeclPrefix is the definition shape, at a line start (after indent).
const testMainDeclPrefix = "func TestMain("

// testSourceFileNames lists the *_test.go files of one package directory.
func testSourceFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
			out = append(out, e.Name())
		}
	}
	return out
}

// gateMinTestMainSuites is the floor for the audit above (17 suites carry a
// TestMain on 2026-09-12). A scan that suddenly sees far fewer is scanning the
// wrong tree, which must fail loudly rather than pass vacuously.
const gateMinTestMainSuites = 12

// TestScratchHelperCleanupSurvivesSubshell is the minimal repro for a real
// leak found by the helper's own self-check: the natural caller form is
//
//	WORK="$(gopi_scratch_mkdir sub)"
//
// and a command substitution runs in a SUBSHELL — so a registry kept only in a
// shell VARIABLE was never visible to the parent, `gopi_scratch_cleanup` removed
// nothing, and every caller leaked its scratch directory until the age GC got
// to it. The registry is a FILE for exactly this reason; this test fails if it
// ever regresses to a variable.
func TestScratchHelperCleanupSurvivesSubshell(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	helper := filepath.Join(root, scriptsDirName, scratchHelperShell)
	if _, err := os.Stat(helper); err != nil {
		t.Skipf("%s is absent on this checkout", helper)
	}
	home := t.TempDir()
	script := `. "$1"
d="$(gopi_scratch_mkdir subshell-leak-probe)" || exit 3
[ -d "$d" ] || { echo "scratch dir was never created"; exit 4; }
gopi_scratch_cleanup
[ -d "$d" ] && { echo "LEAKED $d"; exit 5; }
exit 0
`
	cmd := exec.Command("bash", "-c", script, "bash", helper)
	cmd.Env = append(os.Environ(), "GOPI_TEST_HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the shared shell helper leaked its scratch directory:\n%s", out)
	}
}

// testPackageDirs lists the repo-root-relative directories that hold Go test
// files, walking out from the module root.
func testPackageDirs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := d.Name()
		if path != root {
			switch base {
			case ".git", "bin", "node_modules", "vendor", archiveDirName:
				return filepath.SkipDir
			}
			if strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
		}
		// A directory holding at least one *_test.go is a test package.
		entries, rerr := os.ReadDir(path)
		if rerr != nil {
			return nil
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
				rel, rerr := filepath.Rel(root, path)
				if rerr != nil {
					return rerr
				}
				out = append(out, filepath.ToSlash(rel))
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// pinEntryPointMarkers are the strings a pinned suite's TestMain carries —
// ANY one of them means the suite's TestMain routed the pin through
// internal/testutil (the one-line RunSuiteMain form, or the two-call form a
// suite with extra hermeticity setup uses).
//
// This is the IN-TREE half of the pin evidence. For a package in a nested
// standalone module every one of these imports is refused by Go, so the gate's
// FAIL text names a different remedy there — redirecting the pin env name in the
// module's own TestMain — and this marker set cannot see it. The second half
// (the shape scan in pin_redirect_scan_test.go, reached through packagePinned)
// reads that assignment from the syntax tree, so the text a nested module is
// handed and the predicate that judges it finally agree. The marker list itself
// is unchanged: the completion only ADDS a recognised shape.
var pinEntryPointMarkers = []string{
	"testutil.RunSuiteMain",
	"testutil.PinSuiteForTestMain",
	"testutil.PinSuiteTempRoot",
}

// usesScratchTemp reports whether test sources create scratch directories.
//
// It is deliberately broader than t.TempDir(): the audit of this gate
// (devdoc/scratch-residue-audit.md) found the /tmp incident's real signature —
// scratch reached through os.TempDir() (the AMBIENT root, i.e. /tmp unless a
// pin moved it) or through an explicit "/tmp" literal handed to MkdirTemp /
// CreateTemp. Those are the channels a package pin redirects and an unpinned
// package pays for, so a package using either one must carry the pin exactly
// like a t.TempDir() caller. A "/tmp" path that is only PARSED (inert string
// data: `--db /tmp/x.db`) is not a creation channel and is not matched.
func usesScratchTemp(src string) bool {
	for _, marker := range scratchCreationMarkers {
		if strings.Contains(src, marker) {
			return true
		}
	}
	return false
}

// temptRootDirName is the banned temp-root directory name, spelled once so the
// marker list below never carries a bare literal (Rule 1b). It is the same
// VALUE banProbeDir holds for the runtime probes, restated as its own constant
// because a static scan must not assume which file defines that one.
const temptRootDirName = "tmp"

// scratchCreationMarkers are the strings that mean "this package creates
// scratch data whose location the ambient temp root decides".
var scratchCreationMarkers = []string{
	"t.TempDir()",
	`os.MkdirTemp("`,
	"os.TempDir()",
	`os.MkdirTemp("/` + temptRootDirName,
	`os.CreateTemp("/` + temptRootDirName,
}

// containsAny reports whether any marker appears in src.
func containsAny(src string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(src, m) {
			return true
		}
	}
	return false
}

// readTestSources concatenates every *_test.go in dir (the pin may live in a
// different file from the t.TempDir() call, which is the common layout).
func readTestSources(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(dir, e.Name()), err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}

// The audited roots, one named constant per directory (Rule 1b/13): the value
// decides WHERE an audit looks, so a bare literal would be a second source of
// truth for the gate's own scope.
const (
	evidDirName         = ".evid"
	evid2DirName        = ".evid2"
	evid4DirName        = ".evid4"
	docsDirName         = "docs"
	tasksDirName        = "tasks"
	toolsDirName        = "tools"
	testsuiteDirName    = "testsuite"
	text2imgEvalDirName = "text2img-eval"
)

// auditedScratchRoots are the roots that exist FOR their scratch behaviour:
// scripts/ is the repo's dev/test entry point, devdoc/ keeps one-off harnesses
// next to their finding, testdata/ holds the smoke driver, and the .evid*/
// directories are the evidence drivers of finished tasks — each of them a
// harness whose whole job is to build a working directory and run something.
var auditedScratchRoots = []string{scriptsDirName, devdocDirName, testdataDirName,
	evidDirName, evid2DirName, evid4DirName}

// auditedDriverRoots are the remaining directories that ship a committed .sh/.py
// driver of their own: docs/archive/demo holds a demo's regenerator, tasks/ a
// task's evidence driver, tools/ the verify utilities, testsuite/ the acceptance
// rigs, text2img-eval/ the A/B runner. They were NOT scanned until 2026-10-07
// (task 01a11397-ef8e): the tree-wide census in
// devdoc/scratch-gate-root-widening.md — the generator is named and pinned to a
// commit there (Rule 38) — found ELEVEN tracked scripts outside the original
// three roots that build their own scratch and never route to
// scripts/lib/tmp-ban.sh, and every one of them lives in one of these
// directories. A gate that "audits scripts/" while eleven unscanned scripts
// build /tmp scratch reports a clean tree it never measured.
var auditedDriverRoots = []string{docsDirName, tasksDirName, toolsDirName,
	testsuiteDirName, text2imgEvalDirName}

// auditedScriptRoots is the walk list: both groups, so a root cannot be added to
// one of them and silently forgotten by the walk.
//
// Deliberately NOT here:
//   - archive/ (archiveDirName): a former implementation that is not built and
//     not run — auditing dead code would freeze it under rules it never agreed
//     to;
//   - plugins/: those scripts are a deployed remote SERVICE (a systemd watchdog
//     whose `while true` is its job, and a search plugin whose embedded remote
//     shell runs on ANOTHER host against that host's temp root), not a dev/test
//     scratch workspace — see devdoc/tmp-ban-scratch-root.md. Measured
//     2026-10-07: widening to it would redden SIX files (19 sites), every one of
//     them the searchbox's remote-side restart/dashboard scripts and its CDP
//     server, i.e. reds about a service we do not run here (the measured list is
//     in the devdoc).
var auditedScriptRoots = append(append([]string{}, auditedScratchRoots...),
	auditedDriverRoots...)

// auditedRootAbsences is the TOLERANCE TABLE for the per-root non-vacuity guard
// in assertScopeCoversTheTrackedTree: one entry per audited root this tree does
// not ship a .sh/.py driver under, each carrying the MEASURED reason.
//
// Why a table at all. The guard's job is to catch a root that scans nothing — a
// misspelled directory, or a scope that quietly stopped covering the files the
// 2026-10-07 widening extended it to. But three roots can never satisfy it on
// THIS tree, and no walk can fix that: the repository's first commit is the
// source snapshot 964b240 ("initial: source snapshot at 0592c9ae4, no history
// (docs/archive, text2img-eval, tasks excluded)"), so docs/ and text2img-eval/
// are not in the tree at all and tasks/ carries three .md reports and no
// driver. A guard that is red on every clone is a green nobody can reach, and
// the tempting repair — skip a root that contributed nothing — is the Rule 15
// defect this table exists to avoid: a silent tolerance is indistinguishable
// from a scope that stopped looking.
//
// The tolerated branch is therefore NOT a silent pass. It demands, in order:
//
//  1. the INDEX tracks no .sh/.py under the root. A root whose scripts ARE in
//     the index but not in the walk is a BROKEN WALK, not a missing directory,
//     and it fails with its own message — the arm the tolerance must never
//     swallow;
//  2. the root has an entry in this table. A zero-contributing root with
//     neither indexed scripts nor an entry gets the ORIGINAL failure, verbatim:
//     that is the misspelled-directory case, unchanged;
//  3. every entry is READ OUT LOUD: assertScopeCoversTheTrackedTree writes the
//     tolerated roots and their reasons to stderr whether the gate passes or
//     fails, so a reader of a green run knows exactly what it did not cover;
//  4. the table and the walk list are checked against EACH OTHER both ways — an
//     entry naming a root nobody walks is a permit checked by nobody, and a root
//     that starts contributing while its entry stands is a permit that outlived
//     its reason. Both are failures.
//
// The keys are LITERALS rather than the ...DirName constants, and that is
// deliberate: this is the ONE place the gate carries a second spelling of a
// directory name, because it is the independent anchor that makes a MISSPELLED
// root detectable. The constants (auditedDriverRoots) and this table are in one
// file, so a key derived from the same constant would follow the typo and bless
// it (a root the tree does not ship and a root that does not exist are the same
// observation); the bidirectional check in (4) is what turns the two spellings
// disagreeing into a failure, in both directions.
//
// This table is DATA, like budgetExemptScripts: each row names its evidence.
var auditedRootAbsences = map[string]string{
	"docs": "the tree carries no docs/ entry at all — the source snapshot 964b240 excluded docs/archive; " +
		"measured at 76e6faf: `git ls-tree --name-only HEAD | grep -cx docs` = 0, `git ls-files docs | wc -l` = 0",
	"tasks": "tasks/ is in the tree but ships prose only — 3 tracked .md reports, 0 .sh/.py; " +
		"measured at 76e6faf: `git ls-files tasks | grep -cE '\\.(sh|py)$'` = 0, `git ls-files tasks | wc -l` = 3",
	"text2img-eval": "the tree carries no text2img-eval/ entry — the source snapshot 964b240 excluded it; " +
		"measured at 76e6faf: `git ls-tree --name-only HEAD | grep -cx text2img-eval` = 0, `git ls-files text2img-eval | wc -l` = 0",
}

// auditedScriptRootsText renders the walk list for a failure message, so a
// reader sees the scope the guard actually used rather than the one it expected.
func auditedScriptRootsText() string { return strings.Join(auditedScriptRoots, " ") }

// isAuditedScriptRoot reports whether dir is one of the walked roots (an exact
// name match — a prefix is a different question, asked by underAnyAuditedRoot).
func isAuditedScriptRoot(dir string) bool {
	for _, r := range auditedScriptRoots {
		if r == dir {
			return true
		}
	}
	return false
}

// auditAbsentRoots enforces the tolerance table's two honesty rules: every entry
// must name a root the walk covers, and every entry must still be NEEDED (a root
// contributing files with an entry standing is a permit that outlived its
// reason). It returns nothing; a violation is a failure with the row named.
func auditAbsentRoots(t *testing.T, contributes func(dir string) int) {
	t.Helper()
	dirs := make([]string, 0, len(auditedRootAbsences))
	for dir := range auditedRootAbsences {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if !isAuditedScriptRoot(dir) {
			t.Fatalf("the tolerance table names the root %q, which is not one of the walked roots (%s) — "+
				"a permit for a root nobody walks is a permit checked by nobody, and a misspelled root "+
				"hides here (Rule 15)", dir, auditedScriptRootsText())
		}
		if n := contributes(dir); n > 0 {
			t.Fatalf("the root %q now contributes %d .sh/.py to the walk, so its tolerance entry outlived "+
				"its reason — delete the entry so this run's green is measured against a tree that ships "+
				"the drivers again", dir, n)
		}
	}
}

// collectScriptFiles walks the audited roots (recursively, so a future
// scripts/lib/ or scripts/ci/ is covered) and returns every .sh/.py file.
func collectScriptFiles(t *testing.T, root string) []scriptFile {
	t.Helper()
	var out []scriptFile
	for _, dirName := range auditedScriptRoots {
		base := filepath.Join(root, dirName)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			continue // an absent root is not a failure (a docs-only checkout)
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			shell := strings.HasSuffix(name, ".sh")
			if !shell && !strings.HasSuffix(name, ".py") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, scriptFile{
				rel:   filepath.ToSlash(rel),
				lines: strings.Split(string(raw), "\n"),
				shell: shell,
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return out
}

// assertScopeCoversTheTrackedTree is the NON-VACUOUS-GREEN proof for the walk's
// scope. A gate that stops looking reports a clean tree, and the numbers alone
// cannot tell the two apart: "0 offenders" and "0 files scanned" print the same
// way. So the count is checked against an INDEPENDENT enumeration — `git
// ls-files`, which is the same INDEX a census uses (Rule 38) rather than a second
// walk of the same directories — and the walk must account for a stated share of
// it.
//
// The residue is REPORTED, not silently allowed: a caller fixing a script in one
// of the excluded trees has to know whether it is counted. An empty enumeration
// (no git, no index) fails loudly rather than passing on a zero denominator
// (Rule 15: a scan that found nothing must be distinguishable from one that
// never ran).
func assertScopeCoversTheTrackedTree(t *testing.T, root string, collected int) {
	t.Helper()
	tracked, err := gitLsFiles(root)
	if err != nil {
		t.Fatalf("cannot enumerate the tracked tree to prove the audit's scope: %v", err)
	}
	total := 0
	var skipped []string
	for _, rel := range strings.Split(strings.TrimSpace(tracked), "\n") {
		if rel == "" || !(strings.HasSuffix(rel, ".sh") || strings.HasSuffix(rel, ".py")) {
			continue
		}
		total++
		if !underAnyAuditedRoot(rel) {
			skipped = append(skipped, rel)
		}
	}
	if total == 0 {
		t.Fatalf("the tracked tree carries no .sh/.py at all — the INDEX is empty or unreadable, so the " +
			"coverage below would be a statement about nothing")
	}
	// The per-root accounting needs the SAME file set the walk used; it is
	// recovered from the walk itself, not from a second enumeration, so the two
	// numbers cannot describe different trees.
	collectedRels := auditedScriptRelsUnder(t, root)
	// indexedUnder is the OTHER side of the same comparison: how many tracked
	// .sh/.py the INDEX carries under each walked root. It comes from the
	// enumeration above (the same `git ls-files` snapshot that produced total),
	// so "absent from the tree" and "the walk failed to collect what the index
	// has" are two DIFFERENT, separately measured facts — which is the whole
	// distinction the tolerance table rests on.
	var indexed []string
	for _, rel := range strings.Split(strings.TrimSpace(tracked), "\n") {
		if rel == "" || !(strings.HasSuffix(rel, ".sh") || strings.HasSuffix(rel, ".py")) {
			continue
		}
		indexed = append(indexed, rel)
	}
	indexedUnder := make(map[string]int, len(auditedScriptRoots))
	for _, dir := range auditedScriptRoots {
		indexedUnder[dir] = countUnder(indexed, dir)
	}
	var perRoot []string
	permille := collected * 1000 / total
	t.Logf("scope: %d of %d tracked .sh/.py collected (%d.%d%%), %d outside the audited roots",
		collected, total, permille/10, permille%10, len(skipped))
	if len(skipped) > 0 {
		sort.Strings(skipped)
		t.Logf("outside the audited roots (not scanned, by design): %s", strings.Join(skipped, " "))
	}
	var tolerated []string
	for _, dir := range auditedScriptRoots {
		n := countUnder(collectedRels, dir)
		if n == 0 {
			// The stronger arm first: scripts the index HAS but the walk did not
			// reach. That is never a missing directory — it is the walk broken
			// (a root spelled right but skipped, an unreadable subtree), and the
			// tolerance table must not be able to swallow it.
			if idx := indexedUnder[dir]; idx > 0 {
				t.Fatalf("the index tracks %d .sh/.py under the root %q but the walk collected NONE of "+
					"them — the root is present, so this is the WALK that stopped looking, not a "+
					"directory this tree does not ship (the tolerance table cannot excuse it)", idx, dir)
			}
			reason, ok := auditedRootAbsences[dir]
			if !ok {
				t.Fatalf("the root %q contributed no .sh/.py to the walk — a root that scans nothing is a "+
					"misspelled directory or a scope that no longer exists, and either way this run's green "+
					"does not cover it", dir)
			}
			tolerated = append(tolerated, fmt.Sprintf("%s (%s)", dir, reason))
			perRoot = append(perRoot, fmt.Sprintf("%s=0~", dir))
			continue
		}
		perRoot = append(perRoot, fmt.Sprintf("%s=%d", dir, n))
	}
	// The tolerance goes to STDERR (not t.Logf) so it is visible in BOTH of the
	// shapes a reader meets: with `-v`, and on any failure. It is NOT visible in a
	// plain `go test` PASS — measured 2026-10-24 with a 20-line probe package: Go
	// buffers a passing package's output and prints only `ok …` regardless of
	// which stream wrote it (the same reason this package's own
	// "testutil: suite …" verdicts appear only with -v). Rule 15's requirement is
	// that a tolerated root is NAMED rather than silently skipped — the naming is
	// the table in the source plus this line, and `-v` is how a reader of a green
	// run sees it.
	if len(tolerated) > 0 {
		fmt.Fprintf(os.Stderr, "scripts gate: %d audited root(s) TOLERATED as absent from this tree — "+
			"each one is listed by name with its measured reason, and each still fails if the index "+
			"starts tracking a .sh/.py under it:\n", len(tolerated))
		for _, line := range tolerated {
			fmt.Fprintf(os.Stderr, "scripts gate:   tolerated root: %s\n", line)
		}
	}
	t.Logf("per-root files collected: %s (`=0~` = tolerated absence, see stderr)", strings.Join(perRoot, " "))
	// The table's own two honesty rules (a permit for a root nobody walks; a
	// permit that outlived its reason).
	auditAbsentRoots(t, func(dir string) int { return countUnder(collectedRels, dir) })
	if permille < gateMinTrackedCoveragePermille {
		t.Fatalf("the audit collected %d of %d tracked .sh/.py (%d.%d%%, floor %d.%d%%) — the walk's scope "+
			"shrank, so this run's green says nothing about the files it no longer reads",
			collected, total, permille/10, permille%10,
			gateMinTrackedCoveragePermille/10, gateMinTrackedCoveragePermille%10)
	}
}

// countUnder returns how many repo-root-relative script paths live under dir.
// It is the ONE spelling of "under this root" for the per-root accounting — the
// same predicate underAnyAuditedRoot applies to the walk list, so a root is
// never counted by one rule and coverage-tested by another.
func countUnder(rels []string, dir string) int {
	n := 0
	for _, rel := range rels {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			n++
		}
	}
	return n
}

// auditedScriptRelsUnder returns the repo-root-relative paths the walk covers,
// for the per-root accounting above.
func auditedScriptRelsUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, f := range collectScriptFiles(t, root) {
		out = append(out, f.rel)
	}
	return out
}

// underAnyAuditedRoot reports whether a repo-root-relative path lives under one
// of the roots the walk covers.
func underAnyAuditedRoot(rel string) bool {
	for _, dir := range auditedScriptRoots {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// assertShellParses runs `bash -n` on a shell script: the entry point must at
// least be syntactically valid (the epic's "scripts must pass bash -n").
func assertShellParses(t *testing.T, root string, f scriptFile) {
	t.Helper()
	cmd := exec.Command("bash", "-n", filepath.Join(root, f.rel))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("%s does not parse: %v\n%s", f.rel, err, out)
	}
}

// assertNoDeviceLoad fails any audited script that carries a command which
// saturates the shared device. This is deliberately NOT a loop-bound rule (see
// deviceLoadRes): the 2026-09-16 generator used a bounded loop and passed the
// bound rule, then opened 48 concurrent O_DIRECT+fsync writers against the one
// device. The ban is on the ACTION, wherever it appears.
func assertNoDeviceLoad(t *testing.T, f scriptFile) {
	t.Helper()
	if deviceLoadExemptRels[f.rel] {
		return
	}
	reported, withheld := 0, 0
	for i, line := range f.lines {
		// A quoted incident in a comment is documentation, not a generator.
		// Both known incidents are named in the prose of the documents that ban
		// them, so prose must be stripped or the gate fires on its own history.
		if isCommentLine(line) {
			continue
		}
		if !matchesAny(deviceLoadRes, line) {
			continue
		}
		reported++
		if reported > gateMaxReportedHits {
			withheld++
			continue
		}
		t.Errorf("%s:%d device-load generator %q — FORBIDDEN by GOPI.md Rule 27. "+
			"No test may saturate the shared device to produce a number: a bound on "+
			"the number of rounds (the 2026-09-16 generator had one) says nothing "+
			"about the damage each round does. Measure the operation under test "+
			"instead of flooding the device (the 2026-08-27, 2026-09-11 and "+
			"2026-09-16 incidents were all this shape)",
			f.rel, i+1, strings.TrimSpace(line))
	}
	assertReportedAllHits(t, f.rel, reported, withheld)
}

// assertNoUnboundedLoop fails a loop header that can spin forever unless the
// loop body carries a wall-clock or iteration bound.
func assertNoUnboundedLoop(t *testing.T, f scriptFile) {
	t.Helper()
	reported, withheld := 0, 0
	for i, line := range f.lines {
		if !matchesAny(unboundedLoopRes, line) {
			continue
		}
		if loopBodyIsBounded(f.lines, i) {
			continue
		}
		reported++
		if reported > gateMaxReportedHits {
			withheld++
			continue
		}
		t.Errorf("%s:%d unbounded loop %q — bound it with an iteration cap "+
			"(for ((i=0;i<CAP;i++))) or wrap the body in `timeout`; the 2026-09-11 "+
			"exhaustion was one such loop (devdoc/tmp-ban-scratch-root.md)",
			f.rel, i+1, strings.TrimSpace(line))
	}
	assertReportedAllHits(t, f.rel, reported, withheld)
}

// loopBodyIsBounded reports whether the loop starting at headerIdx carries a
// bound anywhere in the follower block a static scan can see: the header line
// itself, or the next loopBodyLookahead lines (the body up to the nesting
// depth the scan tracks). A `timeout` on the header counts; so does a counter
// or deadline guard inside the body.
func loopBodyIsBounded(lines []string, headerIdx int) bool {
	if matchesAny(timeoutWrapperRes, lines[headerIdx]) {
		return true
	}
	depth := 0
	for i := headerIdx; i < len(lines) && i < headerIdx+loopBodyLookahead; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(stripComment(line))
		if strings.HasPrefix(trimmed, "while ") || strings.HasPrefix(trimmed, "until ") ||
			strings.HasPrefix(trimmed, "for ") {
			depth++
		}
		if trimmed == "done" || strings.HasPrefix(trimmed, "done ") {
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				return false // the loop ended: its body held no bound
			}
		}
		if matchesAny(timeoutWrapperRes, line) {
			return true
		}
	}
	// The body is longer than the lookahead window: the scan cannot prove the
	// bound, but it also cannot prove its absence — a long body is reported as
	// unbounded only when NO bound appears in the window, which is the
	// conservative direction for a gate whose remedy is cheap.
	return false
}

// loopBodyLookahead is how far into a loop body the bound search looks: a loop
// whose bound is further away than this is reported (the fix is one line).
const loopBodyLookahead = 40

// assertNoTmpLiteral fails a /tmp mention outside the ban's own helper files —
// in shell, any line; in Python, a quoted literal (a comment is not a write).
// assertNoTmpLiteral flags a /tmp path PREFIX in an audited file. It is the
// ban's static half for scripts; assertTestSourcesCreateNoTmpPath is the
// equivalent for Go test sources.
func assertNoTmpLiteral(t *testing.T, f scriptFile) {
	t.Helper()
	if tmpLiteralFiles[f.rel] {
		return
	}
	reported, withheld := 0, 0
	for i, line := range f.lines {
		if !carriesTmpLiteral(line, f.shell) {
			continue
		}
		reported++
		if reported > gateMaxReportedHits {
			withheld++
			continue
		}
		t.Errorf("%s:%d carries a /tmp literal %q — /tmp is on the 87%%-full / "+
			"partition (the 2026-09-11 exhaustion): source %s or import %s and "+
			"use its scratch helpers",
			f.rel, i+1, strings.TrimSpace(line), scratchHelperShell, scratchHelperPython)
	}
	assertReportedAllHits(t, f.rel, reported, withheld)
}

// TestGateWithheldHitsMessageDisclosesTheTotal pins the sibling cap's
// disclosure contract (gateMaxReportedHits in this file): a capped listing must
// state the total, and a complete one must claim nothing.
func TestGateWithheldHitsMessageDisclosesTheTotal(t *testing.T) {
	const reported = gateMaxReportedHits + 3
	msg := gateWithheldHitsMessage("scripts/example.sh", reported, 3)
	for _, want := range []string{fmt.Sprintf("%d offending site(s)", reported), "3 withheld"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the withheld-hits message does not state %q: %q", want, msg)
		}
	}
	if got := gateWithheldHitsMessage("scripts/example.sh", gateMaxReportedHits, 0); got != "" {
		t.Errorf("a complete listing must claim no truncation, got %q", got)
	}
}

// assertReportedAllHits is the anti-under-report half of gateMaxReportedHits:
// when a gate withheld lines from its listing, the TOTAL must still be stated,
// or a reader (and whoever writes the next briefing) sees a partial list and
// fixes part of the defect. One t.Errorf per file names the file, the total
// and the withheld count — the count is the truth, the listing is a courtesy.
func assertReportedAllHits(t *testing.T, rel string, reported, withheld int) {
	t.Helper()
	if msg := gateWithheldHitsMessage(rel, reported, withheld); msg != "" {
		t.Error(msg)
	}
}

// gateWithheldHitsMessage is the disclosure text for a capped listing, or ""
// when nothing was withheld (a complete listing must not claim a truncation it
// did not perform). Split from assertReportedAllHits so the contract is
// testable without a *testing.T.
func gateWithheldHitsMessage(rel string, reported, withheld int) string {
	if withheld == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %d offending site(s), %d withheld from the listing above (cap %d) — the count above is "+
		"the full extent; fix every one, not only the listed ones",
		rel, reported, withheld, gateMaxReportedHits)
}

// carriesTmpLiteral reports whether a line writes/points at /tmp: for shell any
// non-comment occurrence, for Python a quoted string literal (a bare /tmp in a
// comment is not an action).
func carriesTmpLiteral(line string, shell bool) bool {
	if shell {
		return strings.Contains(stripComment(line), tmpPathPrefix)
	}
	code := stripPythonComment(line)
	return strings.Contains(code, `"`+tmpPathPrefix) || strings.Contains(code, `'`+tmpPathPrefix) ||
		strings.Contains(code, `os.sep + "tmp"`)
}

// ---------------------------------------------------------------------------
// The Go half of the /tmp gate: TEST SOURCES.
// ---------------------------------------------------------------------------

// assertTestSourcesCreateNoTmpPath is the /tmp gate extended to Go test
// sources. The audit that produced devdoc/test-code-discipline-audit.md found
// the scripts were gated while the Go tests were not — and the tests were
// exactly where /tmp writes had survived: cmd/agent built its probe binaries to
// fixed "/tmp/gopi-*-test" paths (20MB of executable left on the boot device,
// nothing age-GCs it, and the predictable name is a plant-a-symlink hazard).
//
// A test that merely PARSES a /tmp path (inert string data: `--db /tmp/x.db`,
// which is a /tmp-shaped INPUT the production code is being asked about) is not
// a write and is exempted by name below. An exemption is a reviewed decision
// with a reason, never a wildcard — and the exempt list is asserted to still
// exist, so a deletion cannot silently turn an exemption into a hole.
func assertTestSourcesCreateNoTmpPath(t *testing.T, root string) {
	t.Helper()
	scanned, exempted := 0, 0
	for _, rel := range testPackageDirs(t, root) {
		entries, err := os.ReadDir(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			fileRel := filepath.ToSlash(filepath.Join(rel, e.Name()))
			raw, err := os.ReadFile(filepath.Join(root, fileRel))
			if err != nil {
				t.Fatalf("read %s: %v", fileRel, err)
			}
			scanned++
			if name, reason, ok := tmpPathTestExemption(fileRel); ok {
				exempted++
				if _, statErr := os.Stat(filepath.Join(root, fileRel)); statErr != nil {
					t.Errorf("%s is exempted (%q) against %s but does not exist — a stale exemption hides the rule",
						fileRel, reason, name)
				}
				continue
			}
			reported, withheld := 0, 0
			for i, line := range strings.Split(string(raw), "\n") {
				if !tmpPathIsCreatedBy(line) {
					continue
				}
				reported++
				if reported > gateMaxReportedHits {
					withheld++
					continue
				}
				t.Errorf("%s:%d carries a /tmp literal %q — /tmp is on the 87%%-full / partition (the "+
					"2026-09-11 exhaustion, and a boot-device fsync hangs the suite on a scratch DB there): "+
					"use t.TempDir() or testutil.ScratchRootQueue() (GC'd, off `/`). If the path is inert "+
					"string DATA the code under test merely parses, add it to tmpPathTestExemptions with a reason",
					fileRel, i+1, strings.TrimSpace(line))
			}
			assertReportedAllHits(t, fileRel, reported, withheld)
			// The AMBIENT half of the same ban: the literal scan above cannot
			// see a test that reaches the temp root without naming it
			// (os.MkdirTemp("", …), os.TempDir()). That silent form is what
			// survived the first pass of this gate.
			reported, withheld = 0, 0
			for i, line := range strings.Split(string(raw), "\n") {
				if !ambientTmpRootIsCreatedBy(line) {
					continue
				}
				reported++
				if reported > gateMaxReportedHits {
					withheld++
					continue
				}
				t.Errorf("%s:%d creates scratch through the AMBIENT temp root %q — an empty dir argument "+
					"means os.TempDir(), i.e. /tmp on a default host: the fixed-path form of the very leak "+
					"the /tmp ban closed (20MB of scratch nothing GCs, on the boot device the 2026-09-11 "+
					"exhaustion filled). Use t.TempDir(), testutil.SuiteTempRoot() (the per-run pinned "+
					"root) or testutil.ScratchRootQueue() — every one of them is off `/` and collected",
					fileRel, i+1, strings.TrimSpace(line))
			}
			assertReportedAllHits(t, fileRel, reported, withheld)
		}
	}
	if scanned < gateMinTestGoFiles {
		t.Fatalf("the Go /tmp gate audited only %d test files (want at least %d) — the scan is not looking "+
			"where the tests live (vacuous pass guard)", scanned, gateMinTestGoFiles)
	}
	t.Logf("Go /tmp gate: %d test files scanned, %d exempted by name (%d/%d)",
		scanned, exempted, len(tmpPathTestExemptions), len(tmpPathTestExemptions))
}

// tmpPathTestExemptions names the test files (and the checked-in corpora) where
// a /tmp string is inert INPUT rather than a write. Each entry carries its
// reason; the gate asserts every one still exists.
var tmpPathTestExemptions = map[string]string{
	"internal/testutil/scripts_gate_test.go": "the gate's own literal rules and exemption table are spelled here — for BOTH halves of the ban (the /tmp literal rule and the ambient-root rule)",
	"internal/testutil/pin_coverage_test.go": `the widened scratch-channel detector is exercised on synthetic sources containing a /tmp path`,
	// The X11 protocol's own lock file lives in /tmp by definition
	// (/tmp/.X<n>-lock, next to the /tmp/.X11-unix socket directory that
	// internal/shot/x11.go already documents as protocol-mandated). The test
	// only REMOVES it in the cleanup of a private Xvfb it started — it creates
	// nothing there, and no repo-level choice can move a path the X server
	// decides.
	"internal/shot/cov_shot_x11_test.go": `removes the X server's own protocol-mandated lock file in cleanup — the path is the X11 protocol's, not scratch`,
}

// tmpPathSpellings are the quoted spellings of the banned prefix a Go test
// line may carry: the directory itself ("/tmp") and any path under it
// ("/tmp/…"), in double quotes or a raw string. tmpPathPrefix alone ("/tmp/")
// would MISS os.MkdirTemp("/tmp", …) — the exact form this rule was written for.
var tmpPathSpellings = []string{
	`"` + tmpPathPrefix, "`" + tmpPathPrefix,
	`"/` + temptRootDirName + `"`, "`/" + temptRootDirName + "`",
}

// lineNamesTmpPath reports whether a code line carries a /tmp path literal in
// one of its spellings.
func lineNamesTmpPath(code string) bool {
	for _, spelling := range tmpPathSpellings {
		if strings.Contains(code, spelling) {
			return true
		}
	}
	return false
}

// tmpPathCreationCalls are the calls that CREATE or MOVE a filesystem object.
// A /tmp string literal is only a defect when one of them consumes it: the same
// line must carry both. That is the predicate the audit settled on — a test
// legitimately quotes /tmp as inert INPUT (`"/tmp/x.png"` handed to an argument
// parser, `{"path": "/tmp/x"}` inside a mocked tool call), and a gate that
// flagged those 125 lines would be red on the day it landed and disabled by
// the next contributor (the exact failure mode the standards gates call out).
var tmpPathCreationCalls = []string{
	"os.MkdirTemp(", "os.CreateTemp(", "os.Mkdir(", "os.MkdirAll(", "os.WriteFile(",
	"os.Create(", "os.OpenFile(", "os.RemoveAll(", "os.Remove(", "os.Chdir(", "os.Symlink(",
	`"-o", "/` + temptRootDirName, `"-o /` + temptRootDirName,
}

// tmpPathIsCreatedBy reports whether a Go test line both names a /tmp path and
// hands it to a filesystem-mutating call (or to `go build -o`). A comment line
// is never an action (stripComment), so documenting the ban is not a violation.
func tmpPathIsCreatedBy(line string) bool {
	code := stripComment(line)
	if !lineNamesTmpPath(code) {
		return false
	}
	for _, call := range tmpPathCreationCalls {
		if strings.Contains(code, call) {
			return true
		}
	}
	return false
}

// tmpPathTestExemption reports whether a test file is exempt, with its reason.
func tmpPathTestExemption(rel string) (name, reason string, ok bool) {
	reason, ok = tmpPathTestExemptions[rel]
	return rel, reason, ok
}

// ambientTmpPathSpellings are the quoted spellings that mean "the AMBIENT temp
// root": the empty directory argument (os.MkdirTemp("", …) / os.CreateTemp("",
// …)) and os.TempDir() itself. Both reach /tmp on a default host WITHOUT ever
// naming it — which is why the literal scan above cannot see them.
var ambientTmpPathSpellings = []string{
	`os.MkdirTemp(""`,
	`os.CreateTemp(""`,
	`os.TempDir()`,
}

// ambientCreationCalls are the calls that create or MOVE a filesystem object.
// ambientTmpRootIsCreatedBy requires BOTH a spelling and one of these on the
// SAME line, so a mere mention ("why not os.TempDir()?" in prose, an inert
// comparison) is not a violation — the same narrow predicate the /tmp half
// uses, and for the same reason (a red-on-day-one gate gets disabled).
var ambientCreationCalls = []string{
	"os.MkdirTemp(", "os.CreateTemp(", "os.Mkdir(", "os.MkdirAll(", "os.WriteFile(",
	"os.Create(", "os.OpenFile(", "os.RemoveAll(", "os.Remove(", "os.Chdir(", "os.Symlink(",
}

// ambientTmpRootIsCreatedBy reports whether a Go test line creates a filesystem
// object at the AMBIENT temp root (os.MkdirTemp("", …), os.TempDir()). It is
// the second half of the /tmp ban the audit of devdoc/test-code-discipline-audit.md
// found missing: the literal gate cannot flag a path the code never spells, and
// exactly that form had survived (internal/agent's ledger pin built its
// per-run root with os.MkdirTemp("", …) — i.e. /tmp — in a suite whose TestMain
// pins the scratch root, so the pin was silently bypassed).
//
// A line that points the ambient root INTO the pinned tree is not a violation:
// it names a scratch location the suite owns and cleans.
func ambientTmpRootIsCreatedBy(line string) bool {
	code := stripComment(line)
	if !containsAny(code, ambientTmpPathSpellings) {
		return false
	}
	if strings.Contains(code, pinnedScratchRootMarker) {
		return false
	}
	return containsAny(code, ambientCreationCalls)
}

// pinnedScratchRootMarker is the call that redirects scratch to the suite's
// pinned per-run root — the fix this gate's message recommends.
const pinnedScratchRootMarker = "testutil.SuiteTempRoot()"

// stripComment removes a comment from a line: a shell '#' comment (leading, or
// preceded by whitespace), a whole-line Go '//' comment, and a trailing Go
// ' //' comment. A comment naming the banned path is documentation, not an
// action — and the whole-line form matters because documenting the ban (as the
// test sources do) must not redden the gate that enforces it.
func stripComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return ""
	}
	if idx := strings.Index(line, " #"); idx >= 0 {
		line = line[:idx]
	}
	if idx := strings.Index(line, " //"); idx >= 0 {
		line = line[:idx]
	}
	return line
}

// stripPythonComment removes a Python comment (the part after a '#'). A '#' in
// a string literal is not handled — and does not need to be: a false NEGATIVE
// here (a literal hidden behind a '#') is caught by the shell-side rule for
// any script that also shells out, and the quoted-literal rule below is what
// the gate actually enforces.
func stripPythonComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

// assertScratchHelpersAreUsed is the ROUTING gate: every script that builds
// scratch data of its own must ROUTE to the shared helper (a real source/import
// STATEMENT), and every script that routes must keep its own constructions out
// of the scratch path.
//
// Both halves are behaviour, not vocabulary, and both were probed on 2026-10-07:
//
//   - a script carrying the helper's NAME only in prose (a comment, an `echo`,
//     a `[ -r … ]` probe) passed the old `strings.Contains(body, …)` predicate
//     without ever entering the contract;
//   - a script that ADDED `. lib/tmp-ban.sh` and KEPT its own
//     `mktemp -d -p "$SCRATCH_PARENT"` also passed, while its scratch kept
//     landing wherever $TMPDIR pointed — the `source` line satisfied the gate and
//     decided nothing. That is the marginal round-3 probe in
//     devdoc/replayplay-scratch-pin-20261007.md, now a red.
func assertScratchHelpersAreUsed(t gateFailer, files []scriptFile) {
	t.Helper()
	for _, f := range files {
		if tmpLiteralFiles[f.rel] {
			continue
		}
		helper := scratchHelperShell
		if !f.shell {
			helper = scratchHelperPython
		}
		routed, routeLine := routesToScratchHelper(f, helper)
		constructions := scratchConstructionsOf(f)
		if len(constructions) == 0 {
			// No construction of its own: the script never invents a scratch
			// path, so it owes the helper nothing (a script may legitimately
			// reach the scratch tree only through the `gopi` CLI).
			continue
		}
		if !routed {
			t.Errorf("%s:%d builds scratch data of its own (%s) but does not ROUTE to scripts/%s — "+
				"naming the helper is not using it: a `. \"/path/to/%s\"` (or `source`) statement is "+
				"what enters the contract (the helper obtains the path from the scratch root, asserts "+
				"it is off `/` BY DEVICE ID and registers it for trap-time cleanup). Putting the "+
				"helper's name in a comment, an echo or a `[ -r … ]` probe leaves the path to $%s, "+
				"i.e. /tmp on this host — the 2026-09-11 exhaustion's filesystem",
				f.rel, constructions[0].line, constructions[0].command, helper,
				filepath.Base(helper), scratchTempRootVar)
			continue
		}
		for _, c := range constructions {
			t.Errorf("%s:%d routes to scripts/%s (line %d) and STILL runs `%s` — the helper is the "+
				"ONE construction: a routed script takes its directory from `%s` (the helper's own "+
				"generator) instead of making one. `%s` honours $%s, so this line decides the landing "+
				"spot however the `%s` line above reads",
				f.rel, c.line, helper, routeLine, c.command, scratchScratchMkdirWord(),
				c.command, scratchTempRootVar, filepath.Base(helper))
		}
	}
}

// scratchScratchMkdirWord is the shell helper's directory constructor, named
// once: the gate's messages quote it, and a message that spells it inline would
// be a second source of truth for the very word the ban is about (Rule 13).
func scratchScratchMkdirWord() string {
	return "gopi_scratch_mkdir"
}

// scratchConstruction is one line that builds a scratch path of the script's
// own making, with the command it uses and its 1-based line number.
type scratchConstruction struct {
	line    int
	command string
	text    string
}

// scratchConstructionsOf lists the lines that make a THROWAWAY path without
// asking the helper for it. It is the union of shapes:
//
//	(a) a temp-path constructor — shell `mktemp`, Python `mkdtemp`/`mkstemp`,
//	    Python `os.tempdir()`;
//	(b) a directory made under the AMBIENT temp root — `mkdir … $TMPDIR` where
//	    $TMPDIR is the standard variable, or one this script set to a value the
//	    helper did not hand it. A path the script resolved through the helper
//	    (gopi_scratch…) is never flagged, and neither is a $TMPDIR the script
//	    pointed INTO the helper's own tree (helperDerivedVars); a hard-coded
//	    path like `/tmp/x` is already the sibling literal rule's red.
//
// Both halves are per-LANGUAGE, so a Go/Python accessor name inside a shell
// script (`os.tempdir()` in a grep pattern, a heredoc) is not a construction —
// measured on devdoc/daemon-survives-logout-probes/p8-gc-candidate.sh, a
// read-only probe the first cut of this rule flagged.
// The `mkdir` participle is deliberately narrow: an ordinary artifact/build
// directory (`mkdir -p "$OUT"`, `mkdir -p bin`) is not a scratch construction,
// while `mkdir -p "$TMPDIR"` is the same defect one step removed from `mktemp`
// — whoever makes the directory, the PATH is the ambient temp root's.
//
// Comments are stripped first, so a script that DOCUMENTS "no mktemp, no /tmp"
// is not read as one that calls it (the discipline createsScratch learned).
func scratchConstructionsOf(f scriptFile) []scratchConstruction {
	var out []scratchConstruction
	helperVars := helperDerivedVars(f)
	for i, raw := range f.lines {
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		command := scratchConstructionCommand(line, f.shell, helperVars)
		if command == "" {
			continue
		}
		out = append(out, scratchConstruction{line: i + 1, command: command, text: strings.TrimSpace(line)})
	}
	return out
}

// scratchHelperShellWords / scratchHelperPythonWords are the helper's own
// path constructors: a line carrying one of them resolved its scratch path
// THROUGH the contract, so it is not a construction of the script's own.
var (
	scratchHelperShellWords  = []string{"scratch_mkdir"}
	scratchHelperPythonWords = []string{"make_scratch_dir", "scratch_dir"}
)

// scratchConstructionCommand names the offending command on a line, or "".
// helperVars are the names this script derived from the shared helper
// (helperDerivedVars): a directory made under one of them lives in the helper's
// own scratch tree, not at the ambient root.
//
// The command vocabulary is per-LANGUAGE, and that is load-bearing: a Go/Python
// accessor name is not a shell construction. `os.tempdir()` inside a shell
// script is a character sequence in a grep pattern or a heredoc (the probe
// scripts in devdoc/ do exactly that), never a directory the shell makes — so
// matching it there produced a false red on a read-only probe.
func scratchConstructionCommand(line string, shell bool, helperVars map[string]bool) string {
	lower := strings.ToLower(line)
	if shell {
		if hasWord(line, scratchTempCommandShell) {
			return scratchTempCommandShell
		}
		if hasWord(lower, mkdirVerb) &&
			mentionsAnyVar(line, ambientTempRootVars(helperVars)) &&
			!containsAny(line, scratchHelperShellWords) {
			return mkdirVerb + " $" + scratchTempRootVar
		}
		return ""
	}
	// Python: the ambient temp root's own accessor, and the two temp-path
	// constructors. A line naming the helper resolved its path THROUGH the
	// contract, not around it.
	if strings.Contains(lower, scratchTempRootVarCall) && !containsAny(line, scratchHelperPythonWords) {
		return scratchTempRootVar
	}
	for _, cmd := range []string{scratchTempCommandPythonDir, scratchTempCommandPythonFile} {
		if hasWord(line, cmd) && !containsAny(line, scratchHelperPythonWords) {
			return cmd
		}
	}
	return ""
}

// mkdirVerb is the directory-creating shell verb the ambient-root rule keys on.
const mkdirVerb = "mkdir"

// ambientTempRootVars is the set of variable names whose value is the AMBIENT
// temp root: the standard $TMPDIR — minus every name THIS script derived from
// the shared helper. A script that does
//
//	SCRATCH="$(gopi_scratch_mkdir smoke)"; export TMPDIR="$SCRATCH/tmp"; mkdir -p "$TMPDIR"
//
// has pointed $TMPDIR INTO the helper's own scratch tree, so its `mkdir` makes a
// directory off `/` — which is exactly what the helper asks its callers to do
// (scripts/e2e_smoke.sh). The exemption cannot be earned without routing: a
// script that never names the helper has no derived variable, and
// `TMPDIR="$(mktemp -d)"` is red on its own assignment line.
func ambientTempRootVars(helperVars map[string]bool) map[string]bool {
	if helperVars[scratchTempRootVar] {
		return nil
	}
	return map[string]bool{scratchTempRootVar: true}
}

// helperDerivedVars are the names this script assigned a value that came out of
// the shared helper — directly (`SCRATCH="$(gopi_scratch_mkdir …)"`) or through
// another helper-derived variable (`export TMPDIR="$SCRATCH/tmp"`). The closure
// is computed to a fixed point, so the assignments may appear in any order.
func helperDerivedVars(f scriptFile) map[string]bool {
	vars := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, cl := range codeLines(f) {
			for _, st := range shellStatements(cl.text) {
				name, value, ok := shellAssignment(st)
				if !ok || vars[name] {
					continue
				}
				if containsAny(value, scratchHelperShellWords) || mentionsAnyVar(value, vars) {
					vars[name] = true
					changed = true
				}
			}
		}
	}
	return vars
}

// scratchTempRootCall is the Go spelling of the AMBIENT temp root's accessor;
// scratchTempRootVarCall is the Python one. A line that CALLS it and creates
// something is the defect the /tmp literal rule cannot see (the Go half is
// ambientTmpRootIsCreatedBy).
const (
	scratchTempRootCall    = "TempDir()"
	scratchTempRootVarCall = "os.tempdir("
)

// hasWord reports whether text carries word as a whole identifier — `mktemp`
// matches, `mktempfile` does not.
func hasWord(text, word string) bool {
	idx := 0
	for {
		found := strings.Index(text[idx:], word)
		if found < 0 {
			return false
		}
		start := idx + found
		end := start + len(word)
		if !isIdentByte(text, start-1) && !isIdentByte(text, end) {
			return true
		}
		idx = start + 1
	}
}

// isIdentByte reports whether the byte at i is part of an identifier; an
// out-of-range index is not.
func isIdentByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// routesToScratchHelper reports whether the script ENTERS the shared contract
// with a real statement, and the 1-based line that does it.
//
// Shell: a `. ` / `source ` statement whose path names the helper — directly
// (`. "$HERE/lib/tmp-ban.sh"`), or through a variable the script assigned the
// helper's path to first (`TMPBAN="…/lib/tmp-ban.sh"; . "$TMPBAN"`). Using a
// variable is not a bypass: the GATE reads the assignment without resolving it,
// and the shell resolves the same string at run time.
//
// Python: `from …tmp_ban import …` / `import tmp_ban`. The scripts put
// scripts/lib on sys.path first, so the bare module name is the routing.
func routesToScratchHelper(f scriptFile, helper string) (bool, int) {
	code := codeLines(f)
	if !f.shell {
		for _, cl := range code {
			if pythonImportsHelper(cl.text) {
				return true, cl.line
			}
		}
		return false, 0
	}
	base := filepath.Base(helper)
	// Pass 1: the variables the script assigned the helper's PATH (or its
	// basename) to. Pass 2 exists because the assignment usually precedes the
	// source line, but reading a variable before it is set is a plain mistake
	// in shell, not a form worth honouring.
	helperVars := map[string]bool{}
	for _, cl := range code {
		for _, st := range shellStatements(cl.text) {
			name, value, ok := shellAssignment(st)
			if !ok {
				continue
			}
			if strings.Contains(value, base) {
				helperVars[name] = true
			}
		}
	}
	for _, cl := range code {
		for _, st := range shellStatements(cl.text) {
			arg, ok := shellSourceArg(st)
			if !ok {
				continue
			}
			if strings.Contains(arg, base) || mentionsAnyVar(arg, helperVars) {
				return true, cl.line
			}
		}
	}
	return false, 0
}

// shellStatements splits one shell line into its `;`-separated statements.
//
// A `. ` / `source` statement inside a COMPOUND line routes exactly like one on
// a line of its own, and the brief's own repro writes it that way:
//
//	HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"; . "$HERE/lib/tmp-ban.sh"
//
// Reading only the first verb of such a line (an assignment) reported the very
// shape this change exists to catch as "does not route" — found by running the
// repro, not by reading the code.
func shellStatements(text string) []string {
	parts := strings.Split(text, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// codeLine is one non-comment source line with its 1-based number.
type codeLine struct {
	line int
	text string
}

// codeLines is the script's non-comment lines (a shebang is a comment too).
func codeLines(f scriptFile) []codeLine {
	out := make([]codeLine, 0, len(f.lines))
	for i, raw := range f.lines {
		if isCommentLine(raw) || strings.TrimSpace(stripComment(raw)) == "" {
			continue
		}
		out = append(out, codeLine{line: i + 1, text: stripComment(raw)})
	}
	return out
}

// pythonImportsHelper reports whether a line is a real import of the Python
// helper — the statement form, never a mention in a string or a docstring.
func pythonImportsHelper(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "from ") {
		fields := strings.Fields(strings.TrimSuffix(trimmed, "\r"))
		// `from <module> import <names>` — field 1 is the module path.
		return len(fields) >= 3 && fields[len(fields)-1] != "import" &&
			containsAny(fields[1], []string{"tmp_ban", scratchHelperPythonModule})
	}
	if strings.HasPrefix(trimmed, "import ") {
		fields := strings.Fields(trimmed)
		return len(fields) >= 2 && fields[1] == scratchHelperPythonModule
	}
	return false
}

// scratchHelperPythonModule is the helper's importable name, derived from the
// ONE constant that spells its file name (Rule 14): the walker and the
// predicate cannot drift.
var scratchHelperPythonModule = strings.TrimSuffix(filepath.Base(scratchHelperPython), ".py")

// shellAssignment reads a plain `NAME=value` assignment (no command
// substitution needed — only the literal value matters here).
func shellAssignment(line string) (name, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	eq := strings.Index(trimmed, "=")
	if eq <= 0 {
		return "", "", false
	}
	name = strings.TrimSpace(trimmed[:eq])
	if !isShellName(name) {
		return "", "", false
	}
	return name, strings.TrimSpace(trimmed[eq+1:]), true
}

// isShellName reports whether s is a plain shell variable name.
func isShellName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// shellSourceArg extracts the path argument of a `. ` / `source ` statement.
// The argument is the rest of the line: callers compose it out of a variable, a
// command substitution and a basename, and none of them may be dropped.
func shellSourceArg(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, verb := range []string{shellSourceVerbDot, shellSourceVerbPosix} {
		rest, ok := strings.CutPrefix(trimmed, verb)
		if !ok {
			continue
		}
		if verb == shellSourceVerbDot {
			// `.` must be followed by whitespace: `.foo` is a path, not a dot
			// command.
			if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
				continue
			}
		} else if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		arg := strings.TrimSpace(rest)
		if arg == "" {
			continue
		}
		return arg, true
	}
	return "", false
}

// mentionsAnyVar reports whether arg references one of the given shell
// variables ($NAME or ${NAME}).
func mentionsAnyVar(arg string, vars map[string]bool) bool {
	for name := range vars {
		if strings.Contains(arg, "$"+name) || strings.Contains(arg, "${"+name+"}") {
			return true
		}
	}
	return false
}

// matchesAny reports whether any regexp in res matches line.
func matchesAny(res []*regexp.Regexp, line string) bool {
	for _, re := range res {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// repoRootFromCwd walks up from the test binary's working directory to the
// module root (the directory holding go.mod).
func repoRootFromCwd() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
