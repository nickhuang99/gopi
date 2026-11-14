package main

// repo_context_sandbox_guard_test.go — the UPDATE-CLAUSE guard for the
// repo-context sandbox (Rule 13/14 discipline: the class, not the instance).
//
// WHY IT SCANS THE TREE AND NOT A LIST OF TEST NAMES: the defect this epic
// closes was not "one test forgot to pin" — it was that the CLASS of tests which
// hand a spawned gopi a live-repo context had no gate, so the next spawner added
// to this package silently rejoins it. A name list would go stale on the first
// new file; the verdict below is derived from the code.
//
// THE VERDICT, stated as the UPDATE CLAUSE rather than a bare rule: a function in
// cmd/agent that spawns the REAL gopi binary with a WORKER or MERGER verb (or
// `task run`, whose wrapper runs the same close-out) must EITHER pin the repo
// context (SandboxRepoContext, or the helper it delegates to —
// zwPinSweepToSandbox) OR hand the child an env vector that carries its own
// pinned settings path (config.SettingsEnv) with a working directory that is not
// this process's. Anything else hands the child THIS repository, whose
// own-worktree-release pass then enumerates — and can free — the developer's
// worktrees (fulldry.log:522-524).
//
// The scan is textual/AST-shape on purpose: the gate is about SOURCE SHAPE (a
// spawn site and the pin that must sit in the same function), it must run
// without a git repo or a built binary, and a runtime probe would need the very
// spawn it is trying to forbid.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

const (
	// rcsRootFile is the file that DEFINES the pin: its own declarations must
	// never count as a pinned spawn site.
	rcsRootFile = "repo_context_sandbox_test.go"
	// rcsGuardFile is this file (excluded from its own scan for the same
	// reason: it names the identifiers as data, not as calls).
	rcsGuardFile = "repo_context_sandbox_guard_test.go"
	// rcsPinFuncName / rcsDirPinFuncName are the pin's two entry points: the
	// full pin (settings + this process's cwd) and the per-child one (settings
	// + the directory the caller hands the child).
	rcsPinFuncName    = "SandboxRepoContext"
	rcsDirPinFuncName = "RepoSandboxDir"
	// rcsLegacyPinFuncName is the lifecycle suite's pin, which delegates to the
	// same helper — a function that calls it is pinned just as well.
	rcsLegacyPinFuncName = "zwPinSweepToSandbox"
	// rcsSettingsEnvName is the variable a per-variable pin writes into the
	// child's env vector. It is spelled here as the product constant's VALUE
	// because the gate reads SOURCE TEXT, not the linked value.
	rcsSettingsEnvName = "GOPI_SETTINGS"
	// rcsTestFileSuffix selects the package's test sources, rcsProdFileSuffix
	// the production ones (the daemon arm reads the production source that
	// resolves a detached child's working directory).
	rcsTestFileSuffix = "_test.go"
	rcsProdFileSuffix = ".go"
	// The verb identifiers this package passes to a spawned gopi. They are
	// spelled by their Go NAMES because the gate reads source text.
	rcsVerbIdentWorker  = "CliVerbWorker"
	rcsVerbIdentMerger  = "CliVerbMerger"
	rcsVerbIdentTask    = "CliVerbTask"
	rcsVerbIdentTaskRun = "taskVerbRun"
	// rcsVerbIdentMerge is the `gopi merge` verb IDENTIFIER — the word this gate
	// still recognises in SOURCE TEXT, even though the verb no longer dispatches
	// (epic merge-removal, item 2 deleted CliVerbMerge, the dispatch file and the
	// dispatch clause). The gate reads source as TEXT, so the identifier it looks
	// for is a string here rather than a Go reference to a constant that is gone;
	// keeping the arm means a spawn site written against the old name is still
	// recognised as a lifecycle spawn instead of silently sliding past the guard.
	// (No such site exists in the tree today — the walk reports it found none —
	// and that absence is what the positive half of the guard asserts.)
	rcsVerbIdentMerge = "CliVerbMerge"
	// rcsHelpFlagValues holds the usage-query flags BY VALUE: the product's own
	// constants live in this same package (`workerFlagHelp`/`mergeHelpFlag` in
	// main.go / merge_run.go) and this gate compares source TEXT, so a probe that
	// names the constant rather than typing the flag has to be recognized too —
	// measured: `mtlProbe(t, bin, env, CliVerbMerge, mergeHelpFlag)`.
	//
	// It is derived from those constants instead of respelling "-h" (Rule 1b/13:
	// the value has exactly one definition and this file references it).
	rcsHelpFlagValue     = workerFlagHelp
	rcsHelpFlagLongValue = workerFlagHelpLong

	// rcsDirFieldName is the exec.Cmd field naming a child's working directory.
	rcsDirFieldName = "Dir"
	// rcsChdirMethodName is the method name that moves THIS process's working
	// directory — `testing.T.Chdir` (which restores it after the test) and
	// `os.Chdir` share the spelling. See rcsFunctionMovesOwnWorkingDir.
	rcsChdirMethodName = "Chdir"
	// rcsTaskverbsPrefix qualifies a taskverbs constant as a source TEXT would
	// spell it — the gate reads the AST, not the linked value, so a table that
	// writes `taskverbs.Merge` is matched by this prefix plus the constant name.
	rcsTaskverbsPrefix = "taskverbs."
	// rcsRunnerRunGopi is the shared runner whose last argument is the child's
	// working directory.
	rcsRunnerRunGopi = "RunGopi"
	// rcsScanFloor is the number of spawn sites the scan must find for its
	// verdict to mean anything: a glob that silently matched nothing would
	// otherwise report a clean tree (the read-floor that keeps a source gate
	// from passing vacuously). RAISED to 80 by the wdRun/mdRun/mlopStart round
	// (2026-09-19): the measured tree carries 85 (see the gate's own log line),
	// and a floor that trails the tree lets the next predicate regression — a
	// set that stops matching a runner, a spelling that drifts — shrink the scan
	// toward vacuity without failing.
	rcsScanFloor = 80
	// rcsEscapeMarker is the reviewed inline escape: a spawn site whose repo
	// context is pinned by its CALLER (a helper process pinned with cmd.Dir, a
	// helper handed an env vector) says so, with its reason, and keeps showing
	// up in this gate's log line instead of disappearing into a suppression.
	rcsEscapeMarker = "repo-context-ok:"
	// rcsCommentPrefix is the Go line-comment opener, used to tell a marker
	// DIRECTIVE (a comment that IS the escape) from prose that only mentions the
	// marker's spelling — see rcsMarkerDirectives.
	rcsCommentPrefix = "//"

	// rcsFixturePinFuncName is the pin's FIXTURE-REPO variant — the same three
	// inputs with a throwaway repository this test owns as the cwd. A `submit`
	// spawn needs THIS one: its child resolves its own cwd as a repository.
	rcsFixturePinFuncName = "SandboxRepoContextInFixtureRepo"
	// rcsSubmitScanFloor is the submit-cwd arm's read-floor: the number of
	// submit-spawn sites whose child inherits the cwd the scan must find for its
	// verdict to mean anything (a predicate that silently stopped matching would
	// otherwise report a clean tree). Measured 2026-10-01: 22 sites.
	rcsSubmitScanFloor = 10
	// rcsVerbIdentTaskSubmit / rcsTaskSubmitConstName / rcsTaskSubmitVerbValue
	// are the three spellings a source uses for the `submit` task verb: the
	// package-local identifier, the qualified taskverbs constant, and the raw
	// verb string. The gate reads SOURCE TEXT, so it must recognize all three or
	// it reports a smaller, cleaner number than the tree.
	rcsVerbIdentTaskSubmit = "taskVerbSubmit"
	rcsTaskSubmitConstName = "Submit"
	rcsTaskSubmitVerbValue = "submit"
	// rcsCommandFuncName / rcsCommandContextFuncName are exec's two spawn
	// constructors. A call to one builds the command the site then configures
	// (cmd.Dir, cmd.Env), so the directory it hands the child is read from the
	// command's own field, not from the call's arguments.
	rcsCommandFuncName        = "Command"
	rcsCommandContextFuncName = "CommandContext"
)

// rcsLocalRunnerNames are the PACKAGE-LOCAL spawn entry points: this package's
// own helpers that end in exec.Command on the real gopi binary and take the
// child's argv as a parameter. They sit in the same list as the shared testutil
// runners for the same reason — a call to one IS a spawn, so the pin has to sit
// in the calling function.
//
// WHY THEY WERE MISSING AND WHY THAT MATTERED (measured 2026-09-16, this
// epic's re-run on master): the first version of the gate matched only
// exec.Command / exec.CommandContext / the testutil runners, so master's
// `gopi merger --stop` probe (merger_stop_force_modifier_test.go) and
// `gopi worker --once` probes (cli_tasks_worker_unknown_arg_test.go) — all
// reached through runGopiIn — were scanned as ZERO sites while the child really
// did inherit the live repository as its cwd. A gate with an incomplete entry
// point list reports a smaller, cleaner number, not a safer tree.
var rcsLocalRunnerNames = map[string]bool{
	"runGopiIn":               true,
	"wdRun":                   true,
	"mdRun":                   true,
	"mloopStart":              true,
	"runGopiInDir":            true,
	"runGopiInWithPeakEnv":    true,
	"runCLIContractProbeEnv":  true,
	"runGopiForVersion":       true,
	"runGopiVersionSandboxed": true,
	"runGopiForVersionHome":   true,
	// The remaining real-binary runners this epic's exhaustive audit found —
	// every `exec.Command*`/`exec.CommandContext` in the package whose argv is
	// NOT written at the call site, each read for a lifecycle verb (the sweep
	// that found wdRun/mdRun/mloopStart). A wrapper's argv PARAMETER becomes the
	// child's argv, so a call to one IS a spawn and the pin has to sit in the
	// calling function.
	//
	// They are NAMED here rather than waived: leaving one out is what lets its
	// call sites go unscanned, which is the whole defect this map exists to
	// prevent (Rule 13 — a name list nothing consults is a permission nobody
	// reads).
	"mtlProbe":                true, // `gopi merge` e2e cases (merge_lock_e2e_test.go)
	"consoleE2EWorkerStreams": true, // the foreground `gopi worker --once` console walk
	"daemonCwdLaunchFrom":     true, // the `gopi worker --daemon` cwd measurement
	"dcStartDetached":         true, // the `gopi <verb> --daemon` cgroup cases
	// runGopiMapped is the door-mapper forwarder
	// (cli_family_address_test.go: `testutil.RunGopi(t, familyAddressArgs(args), …)`),
	// and RECOGNIZING IT IS NOT OPTIONAL (measured 2026-10-07). The
	// verb-retirement epic routed this package's probes through it, so every
	// migrated call site — `runGopiMapped(t, []string{"worker", "t-1"}, …)` —
	// stopped being a recognized spawn the moment the call was rewritten, and
	// the scan's population fell with it (b258ec65f: 82 sites; here: 64 before
	// this entry, a floor of 80). The map's own comment above is the rule this
	// entry obeys: a gate with an incomplete entry point list reports a smaller,
	// cleaner number, not a safer tree. It is listed because a call to it IS a
	// spawn whose argv the CALL SITE writes, so the pin has to sit in the
	// calling function.
	"runGopiMapped": true,
}

// rcsArgvSliceNames are the identifier spellings a spawn call passes its argv
// through when the ELEMENTS are not written at the call site (`tc.argv...`,
// `args...`, `tc.argv`). An unresolvable one makes the gate read the ENCLOSING
// function's argv data instead of concluding "no verb" — the shape a table-driven
// probe has, and the shape that hid the two sites above.
var rcsArgvSliceNames = map[string]bool{"argv": true, "args": true}

// rcsWorkerControlsReadingNoRepo are the `gopi worker <control>` spellings whose
// invocation RESOLVES NO REPO SET. They run no round, so the child — whatever
// working directory it inherits — never enumerates a worktree and never frees one
// (the same reasoning as `task list`, and the reason a bare `gopi merger` IS
// counted while its `--stop`/`--status` are not).
//
// THE CRITERION, per member, measured in the product rather than assumed
// (2026-10-16, task 01a143b4684f772da77cf3d3394e4080):
//
//   - `--status` and its verbose residue spelling only read: workerStatusRequested
//     → workerStatusReport walks the state files and /proc and returns
//     (cmd/agent/main.go), and nothing on that path opens settings, the repos
//     table or git.
//   - `--stop` ACTS, and it is still listed — because what this map excludes is
//     not "does nothing", it is "reads no repo". workerControlCommand →
//     tools.ControlDir(workerStateDirName) (the profile's own control directory) →
//     the target's pid is signalled and awaited (workerControlStopOne →
//     workerAwaitGone). cli_worker_control.go references no settings, no repos
//     table, no git: the cwd the child inherits cannot reach a worktree through
//     this face. A CONTROL site is therefore neither pinned nor escaped — and a
//     `repo-context-ok:` marker left on one is an ORPHAN, i.e. an escape that
//     covers nothing.
//
// THAT LAST SENTENCE IS THE MEASURED BUG THIS COMMENT REPLACES. The blanket
// argv-shape waiver below fires BEFORE the pin arm and the escape arm, so two
// sites carrying a marker for exactly this reason (worker_control_sigint_e2e_test.go,
// the `worker --stop` fixture) were never counted as escaped: the tree carried 4
// markers while the scan counted 2 sites. The markers are gone (an escape the
// predicate already covers is a permission nobody reads, Rule 13) and the gate's
// mismatch verdict now NAMES both sides, so the next reader sees which marker is
// the orphan instead of re-deriving the census.
var rcsWorkerControlsReadingNoRepo = map[string]bool{
	"workerControlStatus":            true,
	"workerControlStop":              true,
	"workerStatusResidueVerboseFlag": true,
}

// rcsReadOnlyMergerControls are the merger's CONTROL spellings (`--stop`,
// `--status`, `--restart`) and the RETIRED flags (`mergerRetiredFlags`). Their
// exclusion is PROVEN, not assumed — each of them returns before any tick, so
// none of them ever resolves a repo set:
//
//   - `--status` is dispatched before the tick (cmd/agent/main.go: `if
//     opts.statusOnly { runTaskMergerStatus(); return }`) and only READS the
//     lock and the state files;
//   - `--stop` is a NAME for the loop's own SIGTERM gesture
//     (merger_restart.go's head: "the only thing `--stop` without `--every`
//     means is 'end the loop'"). Its acceptance case proves the face does no
//     work: `merger stop --stop` reached the loop's real pid while the E2E's
//     own loop (which the test runs from a SANDBOXED cwd) was resident — a
//     command that resolved a repo set would have released the test's own
//     fixtures, which is exactly what the case asserts does NOT happen.
//     (Only the LOOP resolves a repo set, and a loop is started by another
//     process — never by the control invocation itself.)
//   - `--restart` WITHOUT `--every` is REFUSED for the missing period, i.e.
//     before anything is signalled (`TestMergerRestartRefusesABadPeriodBeforeStopping`);
//     `--restart` WITH `--every` is the control gesture that stops the old loop
//     and leaves a successor behind (runMergerControl). The successor is the
//     hazard that case's rig covers: it starts their loop in a sandbox
//     (SandboxRepoContext) so the successor's cwd can never be the live repo.
//   - a RETIRED flag (`--daemon`, `--start`, `--wait`, `--force`) is refused by
//     the ordinary parser before the tick, with a non-zero exit naming RETIRED
//     (TestMergerRetiredFlagsAreRefusedNotIgnored).
//
// THE WORKER'S `--stop` IS NOT THE MERGER'S, and the difference is the point:
// the worker's control face ACTS on the sandbox's own container (it must find
// the daemon the test started), so a worker-control site is either pinned or
// carries its reason — the class was settled by this gate's earlier round.
var rcsReadOnlyMergerControls = map[string]bool{
	"mergerStopFlag":      true, // --stop
	"mergerRestartFlag":   true, // --restart
	"mergerControlStatus": true, // the status member word ('status')
	"--stop":              true,
	"--restart":           true,
	"--status":            true,
}

// rcsRetiredMergerFlags are the merger spellings that are REFUSED before a tick
// (mergerRetiredFlags, main.go) — named here by identifier AND by literal
// because this gate reads source TEXT: a table that spells the values is judged
// exactly as one that names the constant.
var rcsRetiredMergerFlags = map[string]bool{
	"mergerRetiredFlags": true,
	"--daemon":           true,
	"--start":            true,
	"--wait":             true,
	"--force":            true,
}

// rcsMergerArgvIsControl reports whether one merger invocation is a control
// gesture or a refusal — i.e. whether it reaches a tick at all. A BARE
// `gopi merger` is deliberately NOT excluded: it runs exactly one tick and
// returns, which resolves the repo set and runs the close-out like any other
// round (that is the cronjob's whole contract, and the shape
// task_lifecycle_tick_test.go drives twice).
func rcsMergerArgvIsControl(argv []string) bool {
	for _, v := range argv {
		if rcsReadOnlyMergerControls[v] || rcsRetiredMergerFlags[v] {
			return true
		}
	}
	return false
}

// rcsDaemonFlagSpellings are the source spellings of the ONE flag that turns a
// gopi face into a DETACHED DAEMON. A daemon invocation is not a repo-context
// hazard, and the reason is structural rather than charitable: the production
// spawner resolves the child's OWN working directory before it detaches
// (`daemoncwd.Resolve`, cmd/agent/cli_daemon_spawn.go — "provably off `/`,
// provably outside the worktree it descends from", with the degradation
// announced), so the daemon child never stands in the directory its launcher
// was standing in. The gate re-reads that fact from the production sources
// (rcsDaemonChildrenResolveTheirOwnDir) instead of trusting this note.
var rcsDaemonFlagSpellings = map[string]bool{
	"daemonFlagLong":       true, // "--daemon"
	"workerDaemonFlagLong": true,
	"daemonFlag":           true,
	"--daemon":             true,
}

// rcsDaemonCwdResolverCall is the production call that moves a daemon child out
// of its launcher's working directory. It is the evidence for
// rcsDaemonFlagSpellings: if the package ever stopped resolving the daemon
// child's cwd, the daemon arm below would silently stop being true, so the
// lookup FAILS LOUDLY instead of assuming it.
const rcsDaemonCwdResolverCall = "daemoncwd.Resolve("

// rcsArgvIsDaemonStart reports whether an invocation detaches a daemon.
func rcsArgvIsDaemonStart(argv []string) bool {
	for _, v := range argv {
		if rcsDaemonFlagSpellings[v] {
			return true
		}
	}
	return false
}

// rcsDaemonChildrenResolveTheirOwnDir reports whether this PACKAGE still hands a
// detached daemon child a working directory of its own. It reads the package's
// production sources (the same directory the test glob reads) — a gate about
// source shape may consult source, and a daemon arm resting on a fact that
// vanished must fail rather than quietly widen the exclusion.
// rcsCallArgsNameDaemonFlag reports whether a spawn call spells the detach flag
// among its arguments or inside an argument's composite literal
// (`wdRun(t, bin, env, CliVerbWorker, daemonFlagLong)` / `{verb, daemonFlagLong}`).
func rcsCallArgsNameDaemonFlag(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if v, ok := rcsExprVerbValue(arg); ok && rcsDaemonFlagSpellings[v] {
			return true
		}
		if cl, ok := arg.(*ast.CompositeLit); ok {
			for _, el := range cl.Elts {
				if v, ok := rcsExprVerbValue(el); ok && rcsDaemonFlagSpellings[v] {
					return true
				}
			}
		}
	}
	return false
}

// rcsDaemonHandingHelpers indexes, PER FUNCTION NAME, whether the function hands
// a daemon flag to a spawn of its own. It answers the daemon question for a call
// whose daemon-ness is decided INSIDE the helper it calls rather than at the
// call site — the shape that hid the cgroup cases: their spawn is
// `dcStartDetached(t, bin, home, settings, CliVerbWorker)` and the `--daemon`
// word lives in the helper's own `wdRun(t, bin, env, verb, daemonFlagLong)`.
//
// DERIVED, NEVER LISTED (Rule 13): the answer is read out of the package's own
// source, so a helper that stopped detaching stops being excluded from the
// daemon arm in the same commit — a name list would keep saying "daemon" after
// the flag was gone.
func rcsDaemonHandingHelpers(files []string, t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			hands := false
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !rcsIsSpawnCall(call) {
					return true
				}
				if rcsCallArgsNameDaemonFlag(call) {
					hands = true
				}
				return true
			})
			if hands {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

func rcsDaemonChildrenResolveTheirOwnDir(t *testing.T) bool {
	t.Helper()
	prod, err := filepath.Glob("*" + rcsProdFileSuffix)
	if err != nil || len(prod) == 0 {
		t.Fatalf("the daemon arm needs the package's production sources (%s): glob err=%v, files=%d",
			rcsProdFileSuffix, err, len(prod))
	}
	for _, file := range prod {
		src, rerr := os.ReadFile(file)
		if rerr != nil {
			t.Fatalf("read %s: %v", file, rerr)
		}
		if bytes.Contains(src, []byte(rcsDaemonCwdResolverCall)) {
			return true
		}
	}
	return false
}

// rcsInProcessLifecycleDrivers are the PACKAGE-LOCAL functions that run a
// lifecycle / close-out pass IN THIS PROCESS. They spawn no child, so no env
// vector can be handed to one — the working directory of the CALLING process is
// the only control, because each resolves its repo set from it
// (tools.GitSyncRepoRoot / the settings repos table read against the cwd).
//
// `serveMergedCleanupSweep` was added by this epic's audit (master a9a96cd7):
// it resolves its repo through tools.GitSyncRepoRoot(), and its callers in
// cli_tasks_merged_cleanup_test.go stood in the package directory — an
// unpinned close-out the scan could not see at all. `serveWorktreeReapSweep` is
// its sibling on the same tick. Both keep the same rule the four runners above
// them already carry.
var rcsInProcessLifecycleDrivers = map[string]bool{
	"runTaskWorker":           true,
	"runTaskMerger":           true,
	"runTaskRunOnce":          true,
	"runServeLoop":            true,
	"serveMergedCleanupSweep": true,
	"serveWorktreeReapSweep":  true,
}

// rcsLifecycleVerbs are the verbs whose process runs a lifecycle / close-out
// pass, i.e. the ones that can free a worktree of the repo they resolve. A
// read-only verb (`task list`, `--status`, `-h`) resolves no repo set and is
// therefore not a hazard — which is why the guard is verb-scoped rather than
// "every exec.Command in the package".
var rcsLifecycleVerbs = map[string]bool{
	"CliVerbWorker": true,
	"CliVerbMerger": true,
	"taskVerbRun":   true,
}

// TestSpawnersPinTheRepoContext is the update clause: a worker/merger spawn site
// without the repo-context pin fails HERE, in the same commit that adds it.
func TestSpawnersPinTheRepoContext(t *testing.T) {
	files, err := filepath.Glob("*" + rcsTestFileSuffix)
	if err != nil {
		t.Fatalf("glob test sources: %v", err)
	}
	// The daemon arm's premise is READ from the production sources, and the gate
	// refuses to judge the class when the premise is gone (see
	// rcsDaemonChildrenResolveTheirOwnDir).
	daemonOwnDir := rcsDaemonChildrenResolveTheirOwnDir(t)
	daemonHelpers := rcsDaemonHandingHelpers(files, t)
	pinnedRunners := rcsPinnedRunnerIndex(files, t)
	var offenders []string
	var escapedSites []string
	var scanned, pinned, escaped, daemonStarted, markers int
	for _, file := range files {
		if file == rcsRootFile || file == rcsGuardFile {
			continue
		}
		src, rerr := os.ReadFile(file)
		if rerr != nil {
			t.Fatalf("read %s: %v", file, rerr)
		}
		// THE MARKER ACCOUNT IS KEPT PER FILE (see the verdict below): a
		// documented escape that stops being COUNTED — because the spawn site it
		// describes is no longer recognized — must be an error, never a silent
		// disappearance. This is exactly how the escape above `wdRun`'s callers
		// would vanish: admitting a runner to the recognition set turns its
		// deliberately-escaped sites into PINNED ones, and the reason they
		// carried would go with them.
		markers += rcsMarkerDirectives(string(src))
		lines := strings.Split(string(src), "\n")
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", file, perr)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			spawns, daemonOnly := rcsFunctionSpawnsLifecycleChild(fset, fn, daemonOwnDir, daemonHelpers)
			if !spawns {
				continue
			}
			scanned++
			spawnLine := rcsFunctionFirstSpawnLine(fset, fn)
			if daemonOnly {
				// Every lifecycle spawn in this function is a DAEMON start: the
				// production spawner resolves the child's own working directory
				// (daemoncwd.Resolve), so the child never stands in this process's
				// cwd and the cwd-resolved repo set cannot include it. Measured
				// class: the `worker --daemon` faces.
				daemonStarted++
				continue
			}
			if rcsFunctionPinsRepoContext(fset, fn, lines, pinnedRunners) {
				pinned++
				continue
			}
			if rcsFunctionIsEscaped(fset, fn, lines) {
				escaped++
				escapedSites = append(escapedSites, fmtSpawnSite(file, spawnLine, fn.Name.Name))
				t.Logf("repo-context gate: escaped spawn site (documented): %s", fmtSpawnSite(file, spawnLine, fn.Name.Name))
				continue
			}
			offenders = append(offenders, fmtSpawnSite(file, spawnLine, fn.Name.Name))
		}
	}
	if scanned < rcsScanFloor {
		t.Fatalf("the scan found only %d lifecycle spawn site(s), expected at least %d — "+
			"the verdict would be vacuous (the scan's shape drifted from the tree)", scanned, rcsScanFloor)
	}
	if len(offenders) != 0 {
		sort.Strings(offenders)
		t.Fatalf("%d function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox:\n  %s\n\n"+
			"The spawned child inherits this process's working directory — the live repository — so its "+
			"own-worktree-release pass enumerates the developer's worktrees and frees every clean, unowned "+
			"one past its age floor (fulldry.log:522-524).\n"+
			"FIX: call %s(t) in the spawning function (see %s), or pin the child's own env vector with "+
			"%s pointing at an empty repos table plus a working directory that is not this process's.",
			len(offenders), strings.Join(offenders, "\n  "),
			rcsPinFuncName, rcsRootFile, rcsSettingsEnvName)
	}
	// THE ESCAPE MARKERS MUST STILL BE COUNTED (Rule 13): a `repo-context-ok:`
	// reason whose site stopped being recognized is a permission nobody reads —
	// the exact drift admitting a new runner produces. The gate refuses the
	// whole tree rather than reporting a number that hides it.
	if markers != escaped {
		t.Fatalf("the tree carries %d %q marker(s) but this scan COUNTED only %d escaped "+
			"spawn site(s): a documented exception stopped being recognized — either the site it "+
			"describes left the entry-point set, or the escape moved off its function. "+
			"An escape that no longer covers anything is a permission nobody reads (Rule 13).\n"+
			"\t%s\n"+
			"\t%s\n"+
			"RECONCILE ONE OF THE TWO — never the number in this message:\n"+
			"\t(a) RECOGNIZE the site again if the escape still describes a real permission (a "+
			"predicate or entry-point arm stopped matching its argv), or\n"+
			"\t(b) DELETE the marker if the site's spawn is no longer a recognized lifecycle spawn, "+
			"the predicate that excludes it is evidenced in the product, OR the site became a PINNED "+
			"one — a pin needs no escape, and a marker left on it comes straight back here.",
			markers, rcsEscapeMarker, escaped, rcsMarkerFileLines(files, rcsEscapeMarker), rcsEscapedSiteLines(escapedSites))
	}
	t.Logf("repo-context gate: %d lifecycle spawn site(s) scanned, %d pinned, %d escaped, %d daemon-start (own-dir) site(s), %d escape marker(s)",
		scanned, pinned, escaped, daemonStarted, markers)
}

// rcsMarkerDirectives counts the escape DIRECTIVES in one source: the lines
// that ARE the marker (a comment opening with it), never the lines that merely
// MENTION it.
//
// WHY THE DISTINCTION IS LOAD-BEARING (measured 2026-10-16, this task): the
// account used to be strings.Count(src, marker), so every refactor note that
// quoted the marker's own spelling — a comment explaining that an escape was
// removed, an idiom spelled out in a doc sentence — counted as a live permit.
// The count then read 4 while three real directives existed, and the verdict
// blamed the scan ("the tree carries 4 … but this scan COUNTED only 2"). A
// census that counts mentions reports drift that is not there and hides drift
// that is: the number must be the number of ESCAPES.
func rcsMarkerDirectives(src string) int {
	return rcsMarkerDirectiveLines(strings.Split(src, "\n"))
}

// rcsMarkerDirectiveLines is the ONE predicate both halves of the account read:
// the text census (rcsMarkerDirectives) and the escape arm
// (rcsFunctionIsEscaped). They are answers to one question — "how many escapes
// does this tree carry, and where" — so a second, looser reader on the AST side
// is what let a prose mention become a permit (see rcsFunctionIsEscaped).
func rcsMarkerDirectiveLines(lines []string) int {
	n := 0
	for _, line := range lines {
		if rcsLineIsEscapeDirective(line) {
			n++
		}
	}
	return n
}

// rcsLineIsEscapeDirective reports whether one source line IS the escape
// directive — a `//` comment whose own text opens with the marker — as opposed
// to a line that merely carries the marker's spelling.
//
// THE VOCABULARY this file uses, because the two spellings differ by one char
// and the difference is the whole rule:
//
//	DIRECTIVE — a `//` comment whose first word is the marker:
//	                // repo-context-ok: <why this site is already pinned>
//	            A directive is a PERMIT: it is what the census counts and the
//	            only thing the escape arm accepts.
//	MENTION   — the marker's spelling anywhere else: inside a sentence
//	            ("a `repo-context-ok:` marker left on one is an ORPHAN"), in
//	            this file's own explanatory comments, in a doc, or in a
//	            refactor note recording that an escape was deleted.
//	            A mention is PROSE: it grants nothing and it counts nothing.
//
// The two readings must agree, or the mismatch verdict stops meaning anything:
// a mention admitted as a permit makes the AST side report an escape the census
// never counted (green over a permission nobody wrote), and a mention counted by
// the census reports an orphan that does not exist.
func rcsLineIsEscapeDirective(line string) bool {
	body := strings.TrimSpace(line)
	if !strings.HasPrefix(body, rcsCommentPrefix) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(body, rcsCommentPrefix)), rcsEscapeMarker)
}

// rcsMarkerFileLines renders the "where" half of the marker account: every file
// that carries the escape marker, with the count. A verdict that says "4 markers
// but 2 sites" without naming the files makes the reader re-derive the whole
// census before they can act, and that is how an orphan survives a merged report.
func rcsMarkerFileLines(files []string, marker string) string {
	var parts []string
	for _, file := range files {
		if file == rcsRootFile || file == rcsGuardFile {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			continue // the gate's own read already fails loudly on this file
		}
		if n := rcsMarkerDirectives(string(src)); n > 0 {
			parts = append(parts, file+" ×"+strconv.Itoa(n))
		}
	}
	sort.Strings(parts)
	return "markers in the tree: " + strings.Join(parts, ", ")
}

// rcsEscapedSiteLines renders the same census from the scan's side: the escaped
// spawn sites this run actually COUNTED, in the gate's own file:line: fn() shape.
func rcsEscapedSiteLines(sites []string) string {
	if len(sites) == 0 {
		return "escaped spawn sites this scan counted: none"
	}
	sorted := append([]string(nil), sites...)
	sort.Strings(sorted)
	return "escaped spawn sites this scan counted: " + strings.Join(sorted, ", ")
}

// rcsFunctionSpawnsLifecycleChild reports whether the function spawns the real
// gopi binary with a worker/merger/`task run` verb, and whether EVERY such spawn
// is a DAEMON start (the one class whose child resolves a working directory of
// its own in production — see rcsDaemonFlagSpellings). The daemon fact is
// returned rather than acted on here, so the caller can refuse it when the
// production resolver it rests on is gone.
func rcsFunctionSpawnsLifecycleChild(fset *token.FileSet, fn *ast.FuncDecl, daemonResolvesOwnDir bool, daemonHelpers map[string]bool) (bool, bool) {
	found, allDaemon := false, true
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !rcsIsSpawnCall(call) || !rcsCallSpawnsLifecycleVerb(call, fn) {
			return true
		}
		found = true
		if !daemonResolvesOwnDir || !rcsCallIsDaemonStart(call, fn, daemonHelpers) {
			allDaemon = false
		}
		return true
	})
	return found, found && allDaemon
}

// rcsFunctionFirstSpawnLine is the line of the function's first lifecycle spawn
// — the line the finding points the reader at.
func rcsFunctionFirstSpawnLine(fset *token.FileSet, fn *ast.FuncDecl) int {
	line := 0
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !rcsIsSpawnCall(call) || !rcsCallSpawnsLifecycleVerb(call, fn) {
			return true
		}
		if line == 0 {
			line = fset.Position(call.Pos()).Line
		}
		return true
	})
	return line
}

// rcsCallIsDaemonStart reports whether one spawn call hands the child a daemon
// flag — at the call site, or (for a spread argv) in the enclosing function's
// own argv data.
func rcsCallIsDaemonStart(call *ast.CallExpr, enclosingFn *ast.FuncDecl, daemonHelpers map[string]bool) bool {
	if rcsCallArgsNameDaemonFlag(call) {
		return true
	}
	// The daemon word may live INSIDE the helper this call reaches, which is
	// where a shared detach rig puts it (see rcsDaemonHandingHelpers). Read it
	// from the helper's own source rather than from a list of blessed helpers.
	if name := rcsCalleeName(call.Fun); name != "" && daemonHelpers[name] {
		return true
	}
	if !rcsCallArgvIsUnresolvable(call) || enclosingFn == nil {
		return false
	}
	return rcsFunctionArgvNamesDaemonFlag(enclosingFn)
}

// rcsCalleeName renders the called function's name, whatever the call shape
// (`wdRun(...)`, `runGopiMapped(...)`), or "" when the callee is not a plain
// function reference.
func rcsCalleeName(fun ast.Expr) string {
	switch v := fun.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// rcsCallArgvIsUnresolvable reports whether the call spreads a named argv slice
// (`argv...`, `tc.argv...`) — the shape whose elements are not written at the
// call site.
func rcsCallArgvIsUnresolvable(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if rcsExprIsSpreadOfNamedSlice(arg) {
			return true
		}
	}
	return false
}

// rcsFunctionArgvNamesDaemonFlag is the daemon counterpart of
// rcsFunctionArgvNamesLifecycleVerb: does the function's own argv data carry the
// daemon word?
func rcsFunctionArgvNamesDaemonFlag(fn *ast.FuncDecl) bool {
	for _, argv := range rcsFunctionArgvSlices(fn) {
		if rcsArgvIsDaemonStart(argv) {
			return true
		}
	}
	return false
}

// rcsIsSpawnCall matches the spawn entry points this package uses: raw
// exec.Command / exec.CommandContext, the shared testutil runners, and the
// streaming starters.
func rcsIsSpawnCall(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		// The shared runners, plus the PRODUCER PASSES themselves: a test that
		// calls one directly is running the same close-out a worker round runs,
		// under the test process's own cwd — the shape of the logged culprit.
		switch fun.Sel.Name {
		case "Command", "CommandContext", "RunGopi", "StartGopiBin", "StartMockProcessCmd",
			"ReclaimOwnFinishedWorktrees", "ReclaimOwnFinishedWorktreesWithin",
			"ReclaimSelfOwnRegisteredWorktrees", "ReclaimSelfOwnRegisteredWorktreesWithin":
			return true
		}
		if rcsInProcessLifecycleDrivers[fun.Sel.Name] {
			return true
		}
		if rcsLocalRunnerNames[fun.Sel.Name] {
			return true
		}
	case *ast.Ident:
		// The package-local runners are called unqualified, and so are the
		// IN-PROCESS lifecycle drivers: no child is spawned by the latter, so no
		// env vector can be handed to one — the cwd pin is the only control, and
		// the logged culprit (serve_worker_split_test.go drivev runTaskWorker) is
		// exactly this shape.
		if rcsLocalRunnerNames[fun.Name] {
			return true
		}
		if rcsInProcessLifecycleDrivers[fun.Name] {
			return true
		}
	}
	return false
}

// rcsCallSpawnsLifecycleVerb reports whether one spawn call's arguments name a
// lifecycle verb. The verb list is adjacency-aware for the generic `task` verb:
// `task run` runs a close-out pass (the wrapper releases its own worktrees),
// while `task list` / `task show` resolve no repo set and are not a hazard —
// flagging them would train the reader to ignore this gate.
//
// ITS ARGV IS READ FROM THE ENCLOSING FUNCTION TOO (enclosingFn): a call whose
// argv is a named slice spread at the call site (`...tc.argv...`) carries no
// verb elements of its own, so the sentences that BUILD that slice are the only
// spelling of the argv in the source. Without this the table-driven probes —
// the exact shape master's worker/merger probes have — read as verbless.
func rcsCallSpawnsLifecycleVerb(call *ast.CallExpr, enclosingFn *ast.FuncDecl) bool {
	// An in-process driver carries the lifecycle in its own name — there is no
	// argv to read.
	if id, ok := call.Fun.(*ast.Ident); ok && rcsInProcessLifecycleDrivers[id.Name] {
		return true
	}
	// The producer passes are the lifecycle's close-out by definition.
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		switch sel.Sel.Name {
		case "ReclaimOwnFinishedWorktrees", "ReclaimOwnFinishedWorktreesWithin",
			"ReclaimSelfOwnRegisteredWorktrees", "ReclaimSelfOwnRegisteredWorktreesWithin":
			return true
		}
		if rcsInProcessLifecycleDrivers[sel.Sel.Name] {
			return true
		}
	}
	return rcsCallArgvNamesLifecycleVerb(call, enclosingFn)
}

// rcsCallArgvNamesLifecycleVerb is the argv half: the call's own verb values,
// plus — only when the call's argv is NOT statically resolvable (a spread of a
// named slice) — the enclosing function's own argv-building expressions.
func rcsCallArgvNamesLifecycleVerb(call *ast.CallExpr, enclosingFn *ast.FuncDecl) bool {
	resolvable := true
	var argv []string
	for _, arg := range call.Args {
		if cl, ok := arg.(*ast.CompositeLit); ok { // RunGopi(t, []string{"task","run",…}, …)
			for _, el := range cl.Elts {
				if v, ok := rcsExprVerbValue(el); ok {
					argv = append(argv, v)
				}
			}
			continue
		}
		if rcsExprIsSpreadOfNamedSlice(arg) {
			resolvable = false
			continue
		}
		if v, ok := rcsExprVerbValue(arg); ok {
			argv = append(argv, v)
		}
	}
	if rcsArgvNamesLifecycleVerb(argv) {
		return true
	}
	if resolvable || enclosingFn == nil {
		return false
	}
	return rcsFunctionArgvNamesLifecycleVerb(enclosingFn)
}

// rcsFunctionArgvNamesLifecycleVerb reads the verbs out of the argv-BUILDING
// expressions of one function: composite literals of strings (`[]string{…}`),
// the argv-assignment statements (`argv = append(argv, …)`, `argv := …`), and
// the argv element arguments of a spread call (`runGopiIn(t, bin, env, "",
// face(args...)…)` — the elems the call SPREAD, in the production spelling).
// It answers a deliberately conservative question — "does this function hand
// some call a lifecycle verb?" — so an unresolved spread cannot silently shrink
// the scan.
func rcsFunctionArgvNamesLifecycleVerb(fn *ast.FuncDecl) bool {
	for _, argv := range rcsFunctionArgvSlices(fn) {
		if rcsArgvNamesLifecycleVerb(argv) {
			return true
		}
	}
	return false
}

// rcsFunctionArgvSlices renders every argv vector the function BUILDS: its
// composite literals of strings (`[]string{…}`), the elements appended to an
// argv-named slice (`argv = append(argv, …)`, `argv := …`), and the elements it
// spreads into a call (`face(args...)`). One reader for both verb questions
// (Rule 14) — the argv data of a function is one fact, not two.
func rcsFunctionArgvSlices(fn *ast.FuncDecl) [][]string {
	var out [][]string
	ast.Inspect(fn, func(n ast.Node) bool {
		var exprs []ast.Expr
		switch v := n.(type) {
		case *ast.CompositeLit:
			exprs = v.Elts
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				if !rcsExprIsArgvSliceIdent(lhs) || i >= len(v.Rhs) {
					continue
				}
				exprs = append(exprs, rcsFlattenAppendArgs(v.Rhs[i])...)
			}
		case *ast.CallExpr:
			exprs = rcsSpreadArgElems(v)
		default:
			return true
		}
		var argv []string
		for _, e := range exprs {
			if v, ok := rcsExprVerbValue(e); ok {
				argv = append(argv, v)
			}
		}
		if len(argv) != 0 {
			out = append(out, argv)
		}
		return true
	})
	return out
}

// rcsSpreadArgElems returns the elements of the SPREAD arguments of one call
// (`face(args...)` → args), excluding the argv identifiers themselves. A call
// whose only argument is the spread is a projector: `face(args...)` hands the
// SAME elements on, so the elements its site spelled are the elements a spawner
// under it receives.
func rcsSpreadArgElems(call *ast.CallExpr) []ast.Expr {
	var out []ast.Expr
	for _, arg := range call.Args {
		ell, ok := arg.(*ast.Ellipsis)
		if !ok {
			continue
		}
		switch v := ell.Elt.(type) {
		case *ast.Ident:
			if rcsArgvSliceNames[v.Name] {
				out = append(out, v)
			}
		case *ast.SelectorExpr:
			if rcsArgvSliceNames[v.Sel.Name] {
				out = append(out, v)
			}
		}
	}
	return out
}

// rcsFlattenAppendArgs renders `append(<slice>, a, b, …)` as its appended
// elements (the slice itself is already read through its own assignment).
func rcsFlattenAppendArgs(expr ast.Expr) []ast.Expr {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) < 1 {
		return nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "append" {
		return nil
	}
	return call.Args[1:]
}

// rcsExprIsArgvSliceIdent reports whether an expression is one of the argv-slice
// spellings a spawn call spreads.
func rcsExprIsArgvSliceIdent(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && rcsArgvSliceNames[id.Name]
}

// rcsExprIsSpreadOfNamedSlice reports whether an argument is the spread of an
// argv slice (`argv...`, `tc.argv...`, `args...`) — the form whose elements are
// not written at the call site, so the CALL cannot answer the verb question and
// the enclosing function must. A qualified name counts: the table-driven probes
// spell it `tc.argv...`.
func rcsExprIsSpreadOfNamedSlice(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.Ident:
		return rcsArgvSliceNames[v.Name]
	case *ast.SelectorExpr:
		return rcsArgvSliceNames[v.Sel.Name]
	case *ast.Ellipsis:
		return rcsExprIsSpreadOfNamedSlice(v.Elt)
	}
	return false
}

// rcsArgvNamesLifecycleVerb is the one verb rule both halves share: do these
// argv elements name a worker/merger/`task run` lifecycle?
//
// A HELP QUERY IS NOT A LIFECYCLE, and it is checked FIRST: `-h`/`--help` is
// the zero-side-effect usage query the product short-circuits (P0-1, asserted
// by TestWorkerAcceptedArgumentsParse and by the spawn guard's own
// "help is never refused" case) — it starts no loop, claims nothing and runs no
// close-out, whatever verb rides beside it (`worker -h`, `task run -h`). Without
// this the guard's help probes would be flagged, which is how a gate teaches its
// reader to ignore it.
func rcsArgvNamesLifecycleVerb(argv []string) bool {
	if rcsArgvIsHelpQuery(argv) {
		return false
	}
	for i, v := range argv {
		switch v {
		case "worker", rcsVerbIdentWorker:
			if i+1 < len(argv) && rcsWorkerControlsReadingNoRepo[argv[i+1]] {
				continue // `worker --status` answers a question; it runs no round
			}
			return true
		case rcsVerbIdentMerge:
			// A spawn of `gopi merge` resolves the repos table and mutates it (the
			// same close-out as a merger tick), so it IS a lifecycle spawn — the
			// classification is about the SPAWN, not about whether the word still
			// dispatches. The word does not dispatch any more (epic merge-removal,
			// item 2), so this arm now answers a historical shape: a call site that
			// still spells it is a lifecycle spawn whether or not argv reaches a
			// run. Its `-h` is excluded by the help arm above, like every verb's.
			return true
		case "merger", "serve", rcsVerbIdentMerger:
			// THE MERGER ARM IS FLAG-BOUND, and this is the epic's measured
			// false-positive fix. `gopi merger` (one tick) and
			// `gopi merger --every D` (the same tick, repeated in-process) are
			// lifecycles; `--stop` / `--restart` / `--status` / a RETIRED flag
			// return before any tick and resolve no repo set — see
			// rcsReadOnlyMergerControls. Without this the guard flagged every
			// control probe in the tree, which is how a gate teaches its reader
			// to ignore it.
			if rcsMergerArgvIsControl(argv) {
				continue
			}
			return true
		case "task", rcsVerbIdentTask:
			// `task run` was the retired runner spelling (epic worker-run-entry,
			// 2026-09-16). It is STILL matched: the arm is the historical one — a
			// recorded argv or a legacy transcript that types `gopi task run`
			// describes a lifecycle process, and a new test that reaches for it
			// instead of `worker <task-id>` should be flagged here rather than pass as
			// verbless. The flag set itself has no such site left in the tree.
			for _, v := range argv[i+1:] {
				if v == "run" || v == rcsVerbIdentTaskRun {
					return true
				}
				if rcsMergerWorkFaces[v] {
					return true
				}
			}
		case rcsVerbIdentTaskRun:
			return true
		}
	}
	return false
}

// rcsMergerWorkFaces are the `gopi task <face>` members that reach the SAME
// repo-mutating merge engine as a merger tick (internal/taskverbs): they land
// refs, delete spent branches and reclaim worktrees, so a spawn of one is a
// lifecycle spawn. They are listed by the constant's QUALIFIED NAME and by its
// VALUE because this gate reads source TEXT and a table may spell either.
// The VALUES come from internal/taskverbs (Rule 1b/13 — never respelled here);
// the qualified NAMES are derived from the same constants, so a rename moves
// both spellings at once and neither can drift from the product.
var rcsMergerWorkFaces = func() map[string]bool {
	out := map[string]bool{}
	// One entry per face, under BOTH spellings a table may use: the qualified
	// constant name (`taskverbs.Merge`) and the value it carries (`merge`).
	for _, face := range []struct{ name, value string }{
		{"Merge", taskverbs.Merge},
		{"MergeExecute", taskverbs.MergeExecute},
		{"MergedCleanup", taskverbs.MergedCleanup},
		{"Adopt", taskverbs.Adopt},
		{"AuditMerges", taskverbs.AuditMerges},
	} {
		out[rcsTaskverbsPrefix+face.name] = true
		out[face.value] = true
	}
	return out
}()

// rcsArgvIsHelpQuery reports whether the argv asks for usage. The two spellings
// are the product's own constants (Rule 1b) — the gate reads source text, so
// their NAMES are matched beside their values.
func rcsArgvIsHelpQuery(argv []string) bool {
	for _, v := range argv {
		switch v {
		case "workerFlagHelp", "workerFlagHelpLong", "mergeHelpFlag", "mergeHelpFlagLong",
			rcsHelpFlagValue, rcsHelpFlagLongValue:
			return true
		}
	}
	return false
}

// rcsExprVerbValue renders the statically-known string value of one argument or
// argv element (an identifier's NAME is used as its own value, so a constant
// spelling and a literal spelling compare alike in the adjacency test above).
func rcsExprVerbValue(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name, true
	case *ast.BasicLit:
		return strings.Trim(v.Value, `"`), true
	case *ast.SelectorExpr:
		// A QUALIFIED constant spelling (`taskverbs.Merge`) names its value
		// without writing it, and this gate cannot link the package. It matches
		// the QUALIFIED NAME instead — the same convention the local verb
		// constants use. Measured hole (master a9a96cd7): the merge-face table in
		// merger_work_lock_e2e_test.go spells its argv `{CliVerbTask,
		// taskverbs.Merge, …}` and was invisible to every verb rule.
		if x, ok := v.X.(*ast.Ident); ok {
			return x.Name + "." + v.Sel.Name, true
		}
	case *ast.Ellipsis:
		return rcsExprVerbValue(v.Elt)
	}
	return "", false
}

// rcsFunctionPinsRepoContext reports whether the function pins the repo context
// for the child it spawns. Three acceptable shapes, and they are the criterion
// (not a list of blessed call sites):
//
//	(a) it calls the shared pin (directly, or through the lifecycle suite's
//	    delegating helper) — the process-wide shape;
//	(b) it hands the child its OWN env vector naming the settings variable — the
//	    per-child shape;
//	(c) it hands the child its OWN working directory (cmd.Dir = …, or the
//	    runner's dir argument) — the child then resolves a repo the fixture
//	    chose instead of the ambient live repository. An EMPTY dir (the
//	    runners' "" = "inherit") is NOT a pin, which is exactly the defect:
//	    `RunGopi(t, argv, "", env, "")` launched from the package directory.
func rcsFunctionPinsRepoContext(fset *token.FileSet, fn *ast.FuncDecl, lines []string, pinnedRunners map[string]bool) bool {
	if rcsFunctionCallsPin(fn) {
		return true
	}
	// A call to a runner that PINS THE CHILD ITSELF is a pinned spawn: the
	// criterion is that the child's repo context is CONTROLLED, not that the
	// pinning words appear in this function. Measured case: the foreground
	// `gopi worker --once` console walk hands its child `cmd.Dir = t.TempDir()`
	// (consoleE2EWorkerStreams), so the test calling it never inherits the live
	// repository — flagging it would be a FALSE POSITIVE, and a gate that cries
	// wolf is one nobody keeps.
	if rcsFunctionCallsPinnedRunner(fn, pinnedRunners) {
		return true
	}
	start := fset.Position(fn.Body.Pos()).Line - 1
	end := fset.Position(fn.Body.End()).Line
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	body := strings.Join(lines[start:end], "\n")
	if strings.Contains(body, rcsSettingsEnvName) {
		return true
	}
	return rcsFunctionSetsOwnWorkingDir(fn) || rcsFunctionMovesOwnWorkingDir(fn)
}

// rcsFunctionMovesOwnWorkingDir reports whether the function MOVES THIS PROCESS
// out of the live repository (`t.Chdir(dir)` / `os.Chdir(dir)` with a non-empty
// argument). It is the pin shape that exists for the IN-PROCESS drivers, which
// spawn no child and therefore have no cmd.Dir to set: the repo a close-out
// resolves is the one this process stands in, so leaving the live checkout IS
// the pin.
//
// It is deliberately NOT a licence for `os.Chdir(liveRepo)`: what the gate
// measures is that the site TAKES RESPONSIBILITY for the working directory the
// pass will resolve, in the same function, exactly as cmd.Dir does for a child.
func rcsFunctionMovesOwnWorkingDir(fn *ast.FuncDecl) bool {
	moved := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != rcsChdirMethodName {
			return true
		}
		if len(call.Args) != 1 || rcsExprIsEmptyStringLit(call.Args[0]) {
			return true
		}
		moved = true
		return true
	})
	return moved
}

// rcsFunctionCallsPinnedRunner reports whether the function calls one of this
// package's spawn runners AND that runner pins the child it starts. The index is
// built by the SAME predicate, so a runner that stops pinning stops conferring
// the pin on its callers in the same commit.
func rcsFunctionCallsPinnedRunner(fn *ast.FuncDecl, pinnedRunners map[string]bool) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if pinnedRunners[rcsCalleeName(call.Fun)] {
			found = true
		}
		return true
	})
	return found
}

// rcsPinnedRunnerIndex answers, per package-local spawn runner, whether the
// runner ITSELF pins the child's repo context — measured from its own body by
// the same predicate the gate applies to a spawning function.
func rcsPinnedRunnerIndex(files []string, t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		lines := strings.Split(string(src), "\n")
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
		if perr != nil {
			continue
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !rcsLocalRunnerNames[fn.Name.Name] {
				continue
			}
			if rcsFunctionPinsRepoContext(fset, fn, lines, nil) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// rcsFunctionCallsPin matches the shared pin by identifier. The pin is a FAMILY
// sharing one prefix — SandboxRepoContext, and its fixture-repo variant
// SandboxRepoContextInFixtureRepo (which pins the same three inputs with a
// resolvable throwaway repository as the cwd) — so the whole family counts.
func rcsFunctionCallsPin(fn *ast.FuncDecl) bool {
	pinned := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			if id.Name == rcsPinFuncName || id.Name == rcsDirPinFuncName || id.Name == rcsLegacyPinFuncName ||
				strings.HasPrefix(id.Name, rcsPinFuncName) {
				pinned = true
			}
		}
		return true
	})
	return pinned
}

// rcsFunctionSetsOwnWorkingDir reports whether the function gives the child a
// working directory of its own: `cmd.Dir = <non-empty>` or a runner whose
// directory argument is not the empty "inherit" literal.
func rcsFunctionSetsOwnWorkingDir(fn *ast.FuncDecl) bool {
	own := false
	ast.Inspect(fn, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != rcsDirFieldName || i >= len(v.Rhs) {
					continue
				}
				if !rcsExprIsEmptyStringLit(v.Rhs[i]) {
					own = true
				}
			}
		case *ast.CallExpr:
			if !rcsIsRunnerWithDirArg(v.Fun) {
				return true
			}
			if len(v.Args) > 0 && !rcsExprIsEmptyStringLit(v.Args[len(v.Args)-1]) {
				own = true
			}
		}
		return true
	})
	return own
}

// rcsRunnersWithDirArg are the runners whose LAST argument is the child's
// working directory: the shared testutil runner plus the package-local
// forwarders that end in it. `runGopiMapped` is here for the SAME reason it is
// in rcsLocalRunnerNames (measured 2026-10-07): the verb-retirement epic routed
// every probe through it, so a `runGopiMapped(…, dir)` call — the argument shape
// that says "this site chose the child's directory" — stopped being readable,
// and a real unpinned submit site went unreported until it was.
var rcsRunnersWithDirArg = map[string]bool{
	rcsRunnerRunGopi: true,
	"runGopiMapped":  true,
}

// rcsIsRunnerWithDirArg names the runners whose LAST argument is the child's
// working directory (an empty literal means "inherit this process's").
//
// IT RESOLVES THE CALLEE BY NAME FOR BOTH CALL SHAPES. A qualified runner
// (`testutil.RunGopi`) is an *ast.SelectorExpr and a package-local one
// (`runGopiMapped`) is a bare *ast.Ident; reading only the selector shape is
// exactly how the forwarder's `dir` argument stayed invisible (measured
// 2026-10-07: cli_tasks_waitpid_test.go:148 passes `childDir` and was reported
// as inheriting). rcsCalleeName already owns this resolution — one spelling of
// it, reused rather than re-derived (Rules 13/14).
func rcsIsRunnerWithDirArg(fun ast.Expr) bool {
	return rcsRunnersWithDirArg[rcsCalleeName(fun)]
}

// rcsExprIsEmptyStringLit reports whether an expression is the empty string
// literal (the runners' "inherit my cwd" sentinel) or the empty identifier.
func rcsExprIsEmptyStringLit(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.BasicLit:
		return strings.Trim(v.Value, `"`) == ""
	case *ast.Ident:
		return v.Name == "nil"
	}
	return false
}

// rcsFunctionIsEscaped reports whether the function carries the reviewed inline
// escape — a DIRECTIVE comment (`// repo-context-ok: <reason>`), counted by the
// same predicate the marker census uses (rcsMarkerDirectiveLines). It is the
// opposite of a baseline: the site must STATE why its repo context is already
// pinned, and it keeps appearing in this gate's log line.
//
// WHY A DIRECTIVE AND NOT ANY OCCURRENCE (measured 2026-10-17, this task): this
// used to be strings.Contains over the function's whole body, so ONE prose
// sentence that quoted the marker's spelling — "the census must not count every
// repo-context-ok: mention", a refactor note, a doc line — granted the function
// an escape no reviewer ever wrote. The census counts directives since
// 4d9e7fd96; a body-only containment check is the same account read with a
// looser predicate, and the two disagreeing is what the verdict cannot survive:
// the AST side reports an escape the tree never counted.
func rcsFunctionIsEscaped(fset *token.FileSet, fn *ast.FuncDecl, lines []string) bool {
	start := fset.Position(fn.Pos()).Line - 1
	end := fset.Position(fn.End()).Line
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return false
	}
	// The directive must sit INSIDE the function's own body: a marker above the
	// func line escapes the NEXT function, not this one, and the census counts
	// it wherever it is — so accepting it here would let one file's mention
	// escape a neighbour.
	return rcsMarkerDirectiveLines(lines[start:end]) > 0
}

// fmtSpawnSite renders one finding the way the other source gates in this tree
// do: file, function, and the line to open.
func fmtSpawnSite(file string, line int, fn string) string {
	return file + ":" + strconv.Itoa(line) + ": " + fn + "()"
}

// ---------------------------------------------------------------------------
// The SUBMIT-CWD arm: a spawn whose child resolves ITS OWN cwd as a repository.
//
// WHY IT EXISTS (measured 2026-10-01, epic 01a0f4b2 master reds). The pin above
// answers ONE question — "does this function keep the LIVE repository out of the
// child's reach?" — and both of its variants answer it. But the two variants are
// not interchangeable, because the child of a `task submit` spawn resolves ITS
// OWN working directory as the repository the row names (cli_tasks.go's
// `--repo default` → tools.DurableRepoForSubmit → taskRepoVerifiable):
//
//	SandboxRepoContext(t)             the cwd is a NON-GIT directory — the pin's
//	                                  whole point when the child runs a
//	                                  worker/merger close-out (it must resolve NO
//	                                  repo at all) — so the submit REFUSES:
//	                                  `--repo default: repo "<scratch>/gopi-repo-ctx-<N>"
//	                                  is not a git work tree (git rev-parse failed:
//	                                  exit status 128)`
//	SandboxRepoContextInFixtureRepo(t) the cwd is a throwaway repo this test owns,
//	                                  whose ONLY registration is the main checkout
//	                                  (never removable), so the submit succeeds
//	                                  while the hazard stays closed
//
// Six tests in this package carried the plain pin while submitting through the
// shipped CLI, and all six were RED on master — the fixture reported an
// "is not a git work tree" failure about a directory it had itself created.
//
// THE PREDICATE, deliberately narrow: a spawn site whose argv names the `submit`
// task verb AND which hands the child NO directory of its own (an unset cmd.Dir
// / an empty directory argument — the child inherits this process's cwd, which
// is exactly what the submit resolves). A site that sets cmd.Dir, or passes a
// non-empty runner directory, spawns from a directory IT chose: whether that
// directory is a repo is the site's own business (the SIGKILL case next door
// passes a fixture repo and is green with either variant, because the pin's cwd
// is not what its child resolves).
func TestSubmitSpawnersGiveTheChildAResolvableRepo(t *testing.T) {
	rcsAssertSubmitSpawnersGiveTheChildAResolvableRepo(t, nil)
}

// TestSubmitSpawnersGiveTheChildAResolvableRepo_ExactMatchRule is the built-in
// RED/GREEN proof that the predicate above is neither vacuous nor blind: it runs
// the same scan over four SYNTHETIC sources and pins every verdict, so a
// predicate regression fails on the case that names it rather than on the
// ladder of real sites.
//
//	① plain pin + submit, child inherits cwd   → OFFENDER  (the measured shape)
//	② fixture pin + submit, child inherits cwd → clean     (the fix)
//	③ plain pin + submit, cmd.Dir set          → clean     (the child resolves
//	                                                         the directory the
//	                                                         site chose)
//	④ plain pin + WORKER verb, child inherits  → clean     (a worker/merger
//	                                                         close-out MUST
//	                                                         resolve no repo —
//	                                                         the pin is right
//	                                                         here, and flagging
//	                                                         it would be the
//	                                                         bug)
func TestSubmitSpawnersGiveTheChildAResolvableRepo_ExactMatchRule(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // "" = no offender
	}{
		{
			name: "plain pin, submit, child inherits the cwd",
			src: `package main

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func TestThing(t *testing.T) {
	SandboxRepoContext(t)
	runGopiMapped(t, []string{"task", "submit", "s"}, "", env, "")
}
`,
			want: "TestThing",
		},
		{
			name: "fixture-repo pin, submit, child inherits the cwd",
			src: `package main

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func TestThing(t *testing.T) {
	SandboxRepoContextInFixtureRepo(t)
	runGopiMapped(t, []string{"task", "submit", "s"}, "", env, "")
}
`,
		},
		{
			name: "plain pin, submit, the site chooses the child's cwd",
			src: `package main

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func TestThing(t *testing.T) {
	SandboxRepoContext(t)
	runGopiMapped(t, []string{"task", "submit", "s"}, "", env, childDir)
}
`,
		},
		{
			name: "no pin at all, submit, child inherits the cwd",
			src: `package main

import "testing"

func TestThing(t *testing.T) {
	runGopiMapped(t, []string{"task", "submit", "s"}, "", env, "")
}
`,
		},
		{
			name: "plain pin, worker verb, child inherits the cwd",
			src: `package main

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func TestThing(t *testing.T) {
	SandboxRepoContext(t)
	runGopiMapped(t, []string{"worker", "t-1"}, "", env, "")
}
`,
		},
		{
			// The forwarder's LAST argument is the child's working directory, so a
			// NON-EMPTY one means the SITE chose it (rcsRunnersWithDirArg) and the
			// submit-inherits-cwd arm must not claim it. Measured 2026-10-07: before
			// runGopiMapped joined that family, this shape read as "inherits", and
			// cli_tasks_waitpid_test.go:148 — which passes `childDir` and is not a
			// hazard — was reported as an offender.
			name: "plain pin, submit, the site passes the child's own dir",
			src: `package main

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func TestThing(t *testing.T) {
	SandboxRepoContext(t)
	runGopiMapped(t, []string{"task", "submit", "s"}, "", env, childDir)
}
`,
		},
	}
	for _, tc := range cases {
		got := rcsSubmitOffendersInSource(t, tc.name, []byte(tc.src))
		switch {
		case tc.want == "" && len(got) != 0:
			t.Errorf("%s: predicate flagged %v, want no offender", tc.name, got)
		case tc.want != "" && len(got) != 1:
			t.Errorf("%s: predicate flagged %v, want exactly [%s]", tc.name, got, tc.want)
		case tc.want != "" && got[0] != tc.want:
			t.Errorf("%s: predicate flagged %q, want %q", tc.name, got[0], tc.want)
		}
	}
}

// rcsAssertSubmitSpawnersGiveTheChildAResolvableRepo is the shared scan: every
// test source of this package, every function that spawns a `submit` whose child
// inherits the cwd, and one verdict line. files == nil scans the package's own
// test sources.
func rcsAssertSubmitSpawnersGiveTheChildAResolvableRepo(t *testing.T, files []string) {
	t.Helper()
	if files == nil {
		globbed, err := filepath.Glob("*" + rcsTestFileSuffix)
		if err != nil {
			t.Fatalf("glob test sources: %v", err)
		}
		files = globbed
	}
	var offenders []string
	var scanned, fixturePinned int
	for _, file := range files {
		if file == rcsRootFile || file == rcsGuardFile {
			continue
		}
		src, rerr := os.ReadFile(file)
		if rerr != nil {
			t.Fatalf("read %s: %v", file, rerr)
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", file, perr)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if !rcsFunctionSpawnsSubmitInheritingCwd(fn) {
				continue
			}
			// scanned counts the POPULATION the floor guards: every submit spawn
			// whose child resolves this process's cwd. The offender set is the
			// CONFLICT inside it — a function that pins that cwd with the NON-GIT
			// variant, i.e. pins the directory its own child is about to have
			// refused. A site that pins NOTHING is out of scope: its cwd is the
			// suite's ambient state (in this package the package directory, a real
			// repository), and pinning it at all is the neighbouring gate's
			// business; a site that pins with the fixture repo is the fix.
			scanned++
			if rcsFunctionCallsFixtureRepoPin(fn) {
				fixturePinned++
				continue
			}
			if !rcsFunctionCallsPlainPin(fn) {
				continue
			}
			offenders = append(offenders, fmtSpawnSite(file, rcsFunctionFirstSpawnLine(fset, fn), fn.Name.Name))
		}
	}
	if scanned < rcsSubmitScanFloor {
		t.Fatalf("the scan found only %d submit spawn site(s) whose child inherits the cwd, expected at least %d — "+
			"the verdict would be vacuous (the predicate drifted off the tree's shape)", scanned, rcsSubmitScanFloor)
	}
	if len(offenders) != 0 {
		sort.Strings(offenders)
		t.Fatalf("%d function(s) spawn `gopi task submit` with the child INHERITING this process's working directory, "+
			"but pin that cwd with the NON-GIT variant:\n  %s\n\n"+
			"A submit resolves ITS OWN cwd as the repository the row names (the CLI's `--repo default` → "+
			"DurableRepoForSubmit → taskRepoVerifiable), so under %s(t) — whose cwd is a non-git directory by "+
			"design — the submit REFUSES:\n"+
			"  task submit: --repo default: repo %q is not a git work tree (git rev-parse failed: exit status 128)\n"+
			"and every assertion after it fails on a leg the test never reached.\n"+
			"FIX: use %s(t) — the same three-input pin with a throwaway repository this test owns, whose only "+
			"registration is the main checkout (a main checkout is never removable, so the hazard the pin closes "+
			"stays closed). See %s.",
			len(offenders), strings.Join(offenders, "\n  "),
			rcsPinFuncName, "<scratch-root>/"+repoCtxSandboxDirPrefix+"<N>", rcsFixturePinFuncName, rcsRootFile)
	}
	t.Logf("repo-context submit-cwd gate: %d submit spawn site(s) whose child inherits the cwd, %d pinned with the fixture repo, %d conflicting",
		scanned, fixturePinned, len(offenders))
}

// rcsSubmitOffendersInSource runs the predicate over ONE synthetic source and
// returns the offending function names — the red/green proof's own reader.
func rcsSubmitOffendersInSource(t *testing.T, name string, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse the synthetic source %s: %v", name, err)
	}
	var out []string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if !rcsFunctionSpawnsSubmitInheritingCwd(fn) {
			continue
		}
		if !rcsFunctionCallsPlainPin(fn) || rcsFunctionCallsFixtureRepoPin(fn) {
			continue
		}
		out = append(out, fn.Name.Name)
	}
	return out
}

// rcsFunctionCallsPlainPin reports whether the function pins its cwd with the
// NON-GIT variant (SandboxRepoContext itself — not a sibling sharing its prefix).
func rcsFunctionCallsPlainPin(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == rcsPinFuncName {
			found = true
		}
		return true
	})
	return found
}

// rcsFunctionCallsFixtureRepoPin reports whether the function pins its cwd with
// the FIXTURE-REPO variant — the variant a submit spawn needs.
func rcsFunctionCallsFixtureRepoPin(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == rcsFixturePinFuncName {
			found = true
		}
		return true
	})
	return found
}

// rcsSpawnCallSubmitState reads ONE spawn call: does its argv name the `submit`
// task verb, and does the call hand the child a working directory of its own?
//
// The argv is read from the CALL first (a composite literal of strings —
// `RunGopi(t, []string{"task", "submit", …}, …)`). A call that SPREADS a named
// slice carries no verb elements of its own, so the enclosing function's
// argv-building expressions are read for it — the same two-step read the
// lifecycle-verb rule uses (a resolvable argv never falls back).
func rcsSpawnCallSubmitState(call *ast.CallExpr, fn *ast.FuncDecl) (isSubmit, ownsDir bool) {
	resolvable := true
	var argv []string
	for _, arg := range call.Args {
		if cl, ok := arg.(*ast.CompositeLit); ok {
			for _, el := range cl.Elts {
				if v, ok := rcsExprVerbValue(el); ok {
					argv = append(argv, v)
				}
			}
			continue
		}
		if rcsExprIsSpreadOfNamedSlice(arg) {
			resolvable = false
			continue
		}
		if v, ok := rcsExprVerbValue(arg); ok {
			argv = append(argv, v)
		}
	}
	isSubmit = rcsArgvNamesSubmitVerb(argv)
	if !isSubmit && !resolvable {
		isSubmit = rcsFunctionArgvNamesSubmitVerb(fn)
	}
	return isSubmit, rcsCallGivesChildItsOwnDir(call)
}

// rcsFunctionSpawnsSubmitInheritingCwd reports whether the function holds a spawn
// of the `submit` task verb whose child INHERITS this process's working directory
// (the directory the submit then resolves as the row's repo). A submit spawn that
// hands the child a directory the site itself chose is NOT this shape: the repo
// the row names is the site's own decision, not the pin's cwd.
func rcsFunctionSpawnsSubmitInheritingCwd(fn *ast.FuncDecl) bool {
	inherits := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !rcsIsSpawnCall(call) {
			return true
		}
		isSubmit, ownsDir := rcsSpawnCallSubmitState(call, fn)
		if isSubmit && !ownsDir {
			inherits = true
		}
		return true
	})
	return inherits
}

// rcsFunctionArgvNamesSubmitVerb is the function-wide half: the argv-building
// expressions of the function (its `[]string{…}` literals and its
// `append`-statements) name the submit verb — the shape a spread call leaves
// unreadable at the call site.
func rcsFunctionArgvNamesSubmitVerb(fn *ast.FuncDecl) bool {
	for _, argv := range rcsFunctionArgvSlices(fn) {
		if rcsArgvNamesSubmitVerb(argv) {
			return true
		}
	}
	return false
}

// rcsArgvNamesSubmitVerb reports whether the argv elements name the `submit` task
// verb (the taskverb constant, the raw string, or the positional spelling the
// product reads).
func rcsArgvNamesSubmitVerb(argv []string) bool {
	for _, v := range argv {
		switch v {
		case rcsVerbIdentTaskSubmit, rcsTaskverbsPrefix + rcsTaskSubmitConstName, rcsTaskSubmitVerbValue:
			return true
		}
	}
	return false
}

// rcsCallGivesChildItsOwnDir reports whether the call hands the child a working
// directory of its own: a non-empty runner directory argument. The shared
// runners take the child's directory LAST, so the last argument is the one to
// read (rcsIsRunnerWithDirArg names the family).
func rcsCallGivesChildItsOwnDir(call *ast.CallExpr) bool {
	if rcsIsRunnerWithDirArg(call.Fun) && len(call.Args) > 0 {
		return !rcsExprIsEmptyStringLit(call.Args[len(call.Args)-1])
	}
	// exec.Command / exec.CommandContext with a populated cmd.Dir: the directory
	// is set on the command, so the site chose it (the pattern of the next-door
	// SIGKILL case).
	fn, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return fn.Sel.Name == rcsCommandFuncName || fn.Sel.Name == rcsCommandContextFuncName
}

// TestRepoContextEscapeIsADirective pins the ONE predicate both halves of the
// escape account read. The census counts DIRECTIVES and the escape arm must
// accept exactly those — a prose MENTION of the marker's spelling is neither.
//
// WHY IT IS A TEST AND NOT ONLY A COMMENT (measured 2026-10-17): the census was
// tightened to directives by 4d9e7fd96 and the escape arm kept strings.Contains
// over the function body, so a sentence quoting the marker bought a function an
// escape nobody wrote — the tree stayed green over a permission that does not
// exist. The three arms below are the three readings of one line, and a
// predicate that drifts toward "contains" fails the second one.
func TestRepoContextEscapeIsADirective(t *testing.T) {
	const fnName = "wdForeground"
	cases := []struct {
		name string
		src  string
		want bool // does the escape arm ACCEPT this function?
		// wantCount is the census figure for the same source: the two readers
		// are the same account, so they are asserted together.
		wantCount int
	}{
		{
			name: "a DIRECTIVE inside the body is the permit",
			src: `package main

func wdForeground() {
	// ` + rcsEscapeMarker + ` the env vector is built by the caller from a sandbox home
	spawn()
}
`,
			want:      true,
			wantCount: 1,
		},
		{
			name: "a prose MENTION inside the body is not",
			src: `package main

func wdForeground() {
	// the census must never count a bare ` + rcsEscapeMarker + ` mention as a permit
	spawn()
}
`,
			want:      false,
			wantCount: 0,
		},
		{
			name: "a directive ABOVE the function escapes nothing",
			src: `package main

// ` + rcsEscapeMarker + ` written for the NEXT function, and one line too high
func wdForeground() {
	spawn()
}
`,
			want:      false,
			wantCount: 1,
		},
	}
	for _, tc := range cases {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, "fixture_test.go", tc.src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: parse fixture: %v", tc.name, err)
		}
		fn := rcsFixtureFuncDecl(parsed, fnName)
		if fn == nil {
			t.Fatalf("%s: fixture carries no %s() — the case would assert nothing", tc.name, fnName)
		}
		lines := strings.Split(tc.src, "\n")
		if got := rcsFunctionIsEscaped(fset, fn, lines); got != tc.want {
			t.Errorf("%s: rcsFunctionIsEscaped = %v, want %v\n%s", tc.name, got, tc.want, tc.src)
		}
		if got := rcsMarkerDirectives(tc.src); got != tc.wantCount {
			t.Errorf("%s: rcsMarkerDirectives = %d, want %d (the census and the escape arm read ONE predicate)\n%s",
				tc.name, got, tc.wantCount, tc.src)
		}
	}
}

// rcsFixtureFuncDecl returns the named top-level function of a parsed fixture,
// or nil when the fixture does not declare it.
func rcsFixtureFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Body != nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}
