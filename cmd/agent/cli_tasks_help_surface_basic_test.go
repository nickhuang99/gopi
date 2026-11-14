// cli_tasks_help_surface_basic_test.go — G1 of the help-surface hardening
// briefing (briefings/brief-help-surface-hardening.md): every `gopi task`
// verb the family help ADVERTISES gets a BASIC test that RUNS it the way the
// help says to run it — a happy path plus the verb's first refusal.
//
// WHY THIS FILE IS SHAPED THIS WAY. The defect the briefing records was not a
// missing line of code: `gopi task sync --push-only --task-id T` was
// implemented, compiled, unit-tested and reported complete, and had NEVER been
// typed. A test suite that exercises internals through seams the author chose
// cannot see that class — the surface has to be entered at the entry point the
// operator types (PROJECT.md Rule 31). So every case below drives the REAL BINARY
// over the help's own argv, with a sandboxed GOPI_HOME and a per-test ledger,
// and asserts on the help's CLAIM rather than on incidental output.
//
// THE SURFACE LIST IS DERIVED, NOT AUTHORED: the table in
// devdoc/gopi-task-help-surface-inventory.md is generated from `gopi task -h`
// by scripts/g1-task-help-surface.sh; TestTaskHelpSurfaceMatchesInventory
// re-derives the same list here and fails when the help gains or loses a
// synopsis the inventory has not accounted for. A verb added to the help
// without a row (and without a case) is the defect, so the gate is what keeps
// this file from going stale the moment somebody edits tasksUsage.
//
// TWO FIXTURE HELPERS, both pre-existing and both deliberately reused:
//
//   - the binary: gopiTestBinPath (gopibin_test.go) — ONE build per suite.
//   - the env: cliContractEnv (cli_contracts_test.go) — the scrubbed
//     GOPI_*/ANTHROPIC_*/DEEPSEEK_* environment, which is what keeps a probe
//     from reaching the operator's real profile. Its GOPI_ prefix strip is
//     also what makes the ledger override below survive: GOPI_MEMORY_DB is
//     appended AFTER the strip.
//
// Rule 17: the in-process halves of this file use t.Setenv. Rule 32: the ledger
// path is a directory the harness made under the suite's pinned scratch root,
// never a hand-rolled "memory.db" under a guessed root — and the ROWS are
// always written through the ledger's own callers (the CLI's `task submit`, the
// tools layer's transition path), never by a hand-rolled INSERT. No silent
// skip: a surface this package cannot exercise is listed in the inventory with
// its reason, and the inventory gate fails if a row claims a test that does not
// exist.
package main

import (
	"errors"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/devdoc"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// g1TaskVerbTimeout bounds ONE probe's child. It is a TEST ceiling (Rule 27: a
// probe that can park the whole package's budget must carry its own), derived
// from the contract probe's own budget so a CLI routing regression is reported
// as a routing regression rather than as a hang.
const g1TaskVerbTimeout = 25 // seconds

// g1LedgerEnv is the ledger override the probes pass to their child. It is
// config.MemoryDBEnv's VALUE, pinned here on purpose: this is a subprocess's
// environment vector, i.e. the wire spelling, and a rename of the constant
// that changed the wire would be a separate, visible decision.
const g1LedgerEnv = "GOPI_MEMORY_DB"

// g1FixtureID / g1FixtureSummary identify the row the happy paths act on.
const (
	g1FixtureID      = "488fc5b4-f04a-5681-aa10-1a5526bc4832"
	g1FixtureSummary = "g1 help-surface fixture row"
)

// --- the surface list, re-derived from the help -------------------------------

// g1AdvertisedVerbs returns every verb the family help advertises a synopsis
// for, read from the usage constant the binary prints (tasksUsage) — the SAME
// text `gopi task -h` emits, so no probe is needed to enumerate it. A synopsis
// head is a line starting "  gopi task <verb>".
func g1AdvertisedVerbs() []string {
	re := regexp.MustCompile(`(?m)^  gopi task ([a-z][a-z-]*)\b`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(tasksUsage, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// g1AdvertisedFlags returns the flags of one verb's synopsis, including its
// continuation lines — a long synopsis WRAPS.
//
// THE WRAP IS NOT COSMETIC, and missing it was this gate's own blind spot
// (measured 2026-09-22 while writing the inventory): `submit` spells thirteen
// flags over FOUR synopsis lines, and a scanner that stopped at the head saw
// three of them, so ten advertised spellings were never swept while the test
// still reported a green scan. The continuation shape is therefore pinned by
// its own geometry: the head's indent is 2 spaces, a wrap continues at the
// column the bracketed groups open on (>=18 spaces) and STARTS with a group,
// while the description block that ends the synopsis is indented 6. Prose is
// what a scanner must not read: "--db PATH" appears in "Common flags:" and
// belongs to no verb.
func g1AdvertisedFlags(verb string) []string {
	lines := strings.Split(tasksUsage, "\n")
	var buf []string
	in := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  gopi task "+verb+" ") || ln == "  gopi task "+verb {
			in = true
			buf = append(buf, ln)
			continue
		}
		if !in {
			continue
		}
		// A synopsis continuation line: indented to the bracket column and
		// itself beginning a bracket group. The indent is checked BEFORE the
		// bracket so a deeply indented prose line that happens to quote a
		// flag group cannot enter the surface.
		if strings.HasPrefix(ln, "                  ") && strings.HasPrefix(strings.TrimLeft(ln, " "), "[") {
			buf = append(buf, ln)
			continue
		}
		break
	}
	seen := map[string]bool{}
	var out []string
	for _, ln := range buf {
		for _, m := range regexp.MustCompile(`--[a-z][a-z-]*`).FindAllString(ln, -1) {
			f := strings.TrimPrefix(m, "--")
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}

// g1CoveredVerbs is the inventory's COVERED set: a verb with a basic test in
// this file. A verb absent here must be listed in the inventory with a reason,
// and TestTaskHelpSurfaceMatchesInventory fails when a newly advertised verb
// appears in neither.
//
//nolint:gochecknoglobals // the inventory's declared answer, read by the gate
var g1CoveredVerbs = []string{
	taskverbs.Archive, taskverbs.Clone, taskverbs.DedupScan,
	taskverbs.Improvements, taskverbs.List,
	taskverbs.Search, taskverbs.Show, taskverbs.Status,
	taskverbs.Submit,
	taskverbs.SweepWorktrees,
	taskverbs.Update,
}

// g1ExpectedAdvertisedVerbs is the help surface this test file was written
// against. The gate compares the DERIVED list against it AND against
// g1CoveredVerbs, so neither a help edit nor a test deletion can go unnoticed.
//
// IT WAS STALE ON MASTER (corrected 2026-10-16, task
// 01a136fe53df7a26ae5440668c04150b): `branch-refs` (`ff6f56bdd`) and
// `retry-delivery` (`da2a97063`) were added to `taskverbs.FamilyVerbs` — the
// help advertises both, and both are covered below — while this expectation
// stayed at the 17-verb list the G1 briefing names, so the gate read RED on a
// tree carrying no defect at all. The LIST is the derived advertised surface;
// this expectation is the side that had to move. `orphans` joins it in the
// same change (the verb this task wired).
//
// THREE MORE LEFT IN SLICE 2/4 (2026-10-30, task 01a18799): `verify`,
// `retry-delivery` and `reconcile` — the implementation-shaped verbs — are now
// RETIRED spellings too. Their G1 "runs as advertised" tests went with the
// advertised surface (a verb the help no longer names has nothing to invoke);
// TestRetiredTaskVerbsDispatchARemoval is the one place that still drives their
// dispatch contract, and it is where their replacement paths are asserted.
//
// FOUR VERBS LEFT IN SLICE 1/4 (2026-10-30, task 01a18799): `for-branch`, `branch-refs`,
// `orphans` and `stranded` — the read-only audits of the worktree+branch
// delivery semantics — are RETIRED spellings now (`taskverbs.RemovedVerbs`).
// They are advertised nowhere and appear in neither list above; their dispatch
// cases survive to print one removal sentence each, and
// TestRetiredTaskVerbsDispatchARemoval below is what proves that contract
// instead of leaving it untested (an unadvertised but still-dispatched spelling
// is exactly the state a later reader must be able to see).
//
//nolint:gochecknoglobals // the pinned expectation, read by the gate
var g1ExpectedAdvertisedVerbs = []string{
	taskverbs.Archive, taskverbs.Clone, taskverbs.DedupScan,
	taskverbs.Improvements, taskverbs.List,
	taskverbs.Search, taskverbs.Show, taskverbs.Status,
	taskverbs.Submit,
	taskverbs.SweepWorktrees,
	taskverbs.Update,
}

// TestTaskHelpSurfaceMatchesInventory is the re-derivation gate: the help's
// synopsis set, the pinned expectation and the claimed coverage must agree.
// It is the anti-staleness half of the deliverable — an inventory table nobody
// re-derives is a table that describes last month's help.
func TestTaskHelpSurfaceMatchesInventory(t *testing.T) {
	got := g1AdvertisedVerbs()
	if diff := g1StringDiff(g1ExpectedAdvertisedVerbs, got); diff != "" {
		t.Errorf("the family help's advertised verbs changed:\n%s\n"+
			"Every advertised verb needs a test in this file or a row in "+
			"devdoc/gopi-task-help-surface-inventory.md (with a reason), and this "+
			"expectation updated in the same change.", diff)
	}
	if diff := g1StringDiff(g1CoveredVerbs, got); diff != "" {
		t.Errorf("coverage and the advertised surface disagree:\n%s\n"+
			"An advertised verb with no test here is the defect G1 exists to "+
			"remove; a covered verb the help does not advertise is a claim about "+
			"a surface that no longer exists.", diff)
	}
	if len(got) != len(g1AdvertisedVerbs()) {
		t.Fatalf("derivation is not deterministic: %d vs %d", len(got), len(g1AdvertisedVerbs()))
	}
}

// g1InventoryRowRe reads one row of the checked-in inventory table
// (devdoc/gopi-task-help-surface-inventory.md): `| 3 | \`for-branch\` | …`.
var g1InventoryRowRe = regexp.MustCompile("(?m)^\\| *[0-9]+ *\\| *`([a-z][a-z-]*)` *\\|")

// TestTaskHelpSurfaceInventoryTableMatchesHelp closes the loop the briefing asks
// for by name — "the inventory table is re-derivable from the quoted
// help-enumeration command". The table is the deliverable a reader believes; a
// table that lists a verb the help no longer advertises (or omits one it does)
// is a document describing last month's binary. So the table is READ here and
// compared against the same derivation the other half of this file uses.
//
// It also asserts the table's row count, so a truncated or half-written table
// cannot pass by merely mentioning every verb somewhere in prose.
func TestTaskHelpSurfaceInventoryTableMatchesHelp(t *testing.T) {
	rows := g1InventoryRowRe.FindAllStringSubmatch(g1InventoryDoc(t), -1)
	var verbs []string
	for _, m := range rows {
		verbs = append(verbs, m[1])
	}
	sort.Strings(verbs)
	want := g1AdvertisedVerbs()
	if diff := g1StringDiff(want, verbs); diff != "" {
		t.Errorf("devdoc/gopi-task-help-surface-inventory.md and the help disagree:\n%s\n"+
			"Every advertised verb needs a table ROW (the table is the deliverable); "+
			"a row for a verb the help does not advertise is a stale claim.", diff)
	}
	if len(rows) != len(want) {
		t.Errorf("the inventory table has %d verb rows, the help advertises %d — "+
			"the table must carry exactly one row per advertised verb", len(rows), len(want))
	}
}

// g1InventoryDoc reads the checked-in inventory table from the repo root.
func g1InventoryDoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "devdoc", "gopi-task-help-surface-inventory.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the inventory table (the deliverable this gate protects): %v", err)
	}
	return string(b)
}

// TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest is the small, mechanical
// half of the same anti-staleness concern: a devdoc file the build does not
// classify is invisible to every class-driven reader (the class banner on line
// 1 of the document — PROJECT.md Rule 36, whose whole-corpus gate
// internal/standards' TestEveryCorpusDocDeclaresItsClassInItsHeader owns). The
// G1 inventory makes a standing claim about what the help currently advertises,
// so it declares NORMATIVE on its own first line.
//
// WHAT THIS USED TO READ, AND WHY IT NO LONGER DOES (measured 2026-10-24, this
// task): the witness was a row in `devdoc/DOC-INDEX.tsv` read off disk with
// t.Fatalf on a missing file. Commit 475001d untracked that table on the
// operator's instruction, so the read failed on every ground cut after it and
// ./cmd/agent was permanently red for a reason having nothing to do with this
// inventory. The class moved INTO the document (Rule 36); the property did not
// move an inch — an unclassified devdoc is still caught, now by the package's
// ONE reader (devdocClassOf, red-proved by
// TestDevdocClassOfCatchesAnUnclassifiedDoc).
func TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest(t *testing.T) {
	root := repoRoot(t)
	const want = devdoc.Root + "/gopi-task-help-surface-inventory.md"
	class, why := devdocClassOf(root, want)
	if why != "" {
		t.Fatalf("%s — the inventory makes a standing claim about what the help "+
			"advertises, so it must declare %q on line 1 (Rule 36)", why, devdoc.ClassNormative)
	}
	if class != devdoc.ClassNormative {
		t.Errorf("%s declares class %q, want %q — the role is load-bearing: "+
			"merge_union.go resolves a two-class conflict by reading exactly this banner, "+
			"and the preland gate attributes a conflicted document's class from it",
			want, class, devdoc.ClassNormative)
	}
}

// TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted closes the SECOND half of the
// same class, at the parser boundary: every flag a verb's synopsis advertises
// must be ADMITTED by that verb's flag table. The measured defect (2026-09-22,
// this task): `gopi task sweep-worktrees --json` — the help's own spelling —
// was refused with "unknown flag --json" while runTaskSweepWorktrees read
// flags.jsonOut. audit-merges had the identical shape a week earlier
// (cli_tasks_advert_audit_flags_test.go), which is why this is a SWEEP over the
// whole family rather than a case for one verb.
//
// Two deliberate exclusions, both named rather than silently skipped:
//
//   - --db is the family's common flag; it is admitted everywhere it is
//     advertised, and the parser treats it uniformly (checked below anyway —
//     the table admits it, so no exclusion is needed for it).
//   - a flag spelled in a synopsis but owned by a SIBLING page (sync's
//     --push-only and friends) never reaches this parser: those verbs have
//     their own help and their own table.
func TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted(t *testing.T) {
	// A probe value for the flags that require one. A flag with no value here
	// is passed bare (correct for the switches). The PATH-valued probes come
	// from t.TempDir() — a directory the harness made under the suite's pinned
	// scratch root (Rule 22/32) — never a bare host literal: this file must not
	// name a temp root itself (Rule 13's fixture gate reads it).
	probeDir := t.TempDir()
	val := map[string]string{
		"status": "pending", "db": "probe.db", "repo": probeDir, "min-age-h": "24",
		// --branch / --worktree carry a value; both are admitted by show AND
		// status, and the sweep runs every advertised verb-flag pair, so a
		// missing value here would make the bare spelling look like a refusal
		// of the WRONG kind (the probe only fails on "unknown flag --X").
		"branch": "probe/branch", "worktree": probeDir,
		"max": "5", "task": "t-x", "stall-sec": "10", "max-run": "10",
		"payload": "p", "tags": "bug", "parent-task-id": "t-p", "source": "human",
		"task-id": "t-x", "session-id": "s-x", "model": "m", "priority": "5",
		"attachment": filepath.Join(probeDir, "g1-nonexistent-attachment"), "timeout": "5", "reason": "why",
	}
	scanned := 0
	for _, verb := range g1AdvertisedVerbs() {
		flags := g1AdvertisedFlags(verb)
		if len(flags) == 0 && verb != taskverbs.Improvements {
			t.Errorf("verb %q advertises no flags — the synopsis scanner is reading blind", verb)
		}
		for _, f := range flags {
			scanned++
			argv := []string{"t-1"}
			if v, need := val[f]; need {
				argv = append(argv, "--"+f, v)
			} else {
				argv = append(argv, "--"+f)
			}
			// A refusal for any OTHER reason (a bad combination, a missing
			// companion flag) is fine — this test asks one question: does the
			// FLAG TABLE admit the spelling the help prints?
			if _, err := parseTasksArgs(verb, argv); err != nil &&
				strings.Contains(err.Error(), "unknown flag --"+f) {
				t.Errorf("`gopi task %s --%s` is REFUSED by the flag table while the synopsis advertises it: %v\n"+
					"An advertised flag is either obeyed or refused by NAME (Rule 15) — "+
					"add it to taskFlagMember's case for %q, or take it out of the synopsis.",
					verb, f, err, verb)
			}
		}
	}
	// The floor is the measured surface minus headroom: a scanner that regressed
	// to head-lines-only would report far fewer sites and must fail rather than
	// pass quietly.
	//
	// RE-MEASURED 55 → 48 (2026-10-31, the finish-cleanup retirement completes):
	// its synopsis block left the page (the verb refuses with exit 1 since
	// `58b5618b`), taking its four advertised flags with it — `--task --apply
	// --json --db`. The floor keeps the same 5-site headroom it always had:
	// 48 - 5 = 43.
	if scanned < 43 {
		t.Fatalf("the scan inspected only %d advertised flag sites — the synopsis parser is broken, so a green run proves nothing", scanned)
	}
}

// g1StringDiff renders the two-way difference between want and got.
func g1StringDiff(want, got []string) string {
	inGot := map[string]bool{}
	for _, s := range got {
		inGot[s] = true
	}
	inWant := map[string]bool{}
	for _, s := range want {
		inWant[s] = true
	}
	var b strings.Builder
	for _, s := range want {
		if !inGot[s] {
			fmt.Fprintf(&b, "  want-only (advertised/claimed but not present): %s\n", s)
		}
	}
	for _, s := range got {
		if !inWant[s] {
			fmt.Fprintf(&b, "  got-only (present but unaccounted for): %s\n", s)
		}
	}
	return b.String()
}

// --- fixtures -----------------------------------------------------------------

// g1Sandbox is one probe's world: a GOPI_HOME the child may write anything
// into, and the ledger path inside it. Both live under the suite's pinned
// scratch root (Rule 22), never under the operator's profile.
type g1Sandbox struct {
	bin  string
	home string
	db   string
}

func g1NewSandbox(t *testing.T) g1Sandbox {
	t.Helper()
	sb := g1Sandbox{bin: gopiTestBinPath(t), home: t.TempDir()}
	sb.db = filepath.Join(sb.home, "g1-ledger.db")
	return sb
}

// g1Run drives the real binary with the sandboxed env and the ledger override,
// returning combined output and the exit code. A timeout is reported as its own
// failure: a correctly-routed ledger verb never reaches the agent loop, so a
// probe that hangs is a routing defect, not a slow test.
func (s g1Sandbox) g1Run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(s.bin, familyAddressArgs(args)...)
	cmd.Env = append(cliContractEnv(s.home), g1LedgerEnv+"="+s.db)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	timer := time.After(g1TaskVerbTimeout * time.Second)
	select {
	case <-done:
	case <-timer:
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %ds — argv fell through to the agent loop, or parked on a child",
			strings.Join(args, " "), g1TaskVerbTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// g1RunOK runs and requires exit 0.
func (s g1Sandbox) g1RunOK(t *testing.T, args ...string) string {
	t.Helper()
	out, code := s.g1Run(t, args...)
	if code != 0 {
		t.Fatalf("gopi %s: exit %d (want 0)\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

// g1ClonedID reads the id a `task clone` line names — the row it created, or
// the row it WOULD create under --dry-run. Shared by both because the two lines
// differ only in their marker: a clone mints a fresh uuid, so neither the test
// nor the caller can predict it and it must be read back rather than derived.
func g1ClonedID(t *testing.T, out string) string {
	t.Helper()
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, tools.TaskCloneMarker) {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the clone line is not there, so no id can be read:\n%s", out)
	}
	// "<marker>: <id> (status=..."
	_, after, ok := strings.Cut(line, ":")
	if !ok {
		t.Fatalf("the clone line carries no `: <id>`:\n%s", line)
	}
	fields := strings.Fields(after)
	if len(fields) == 0 {
		t.Fatalf("the clone line carries no id after the marker:\n%s", line)
	}
	return fields[0]
}

// g1Seed writes the fixture row into the sandbox's ledger and leaves it in
// `status`, with `branch` stamped as its deliverable when one is asked for.
//
// The row is created the way the PRODUCT creates rows — through the CLI's own
// `task submit` (so a pending row is a real pending row, not a fixture that
// fabricated one) — and then moved along the ledger's LEGAL transition ladder
// in process. That two-step shape is deliberate: the engine has no path that
// conjures a pending row directly (TaskNew is the in_progress creator), so a
// fixture that needs `pending` must either reimplement the INSERT — the
// hand-rolled-schema anti-pattern Rule 32 exists to stop — or use the door the
// operators use.
func g1Seed(t *testing.T, sb g1Sandbox, status, branch string) {
	t.Helper()
	out := sb.g1RunOK(t, "task", taskverbs.Submit, g1FixtureSummary,
		"--task-id", g1FixtureID, "--model", "test-model", "--db", sb.db)
	if !strings.Contains(out, g1FixtureID) {
		t.Fatalf("seed row: submit did not report %s:\n%s", g1FixtureID, out)
	}
	if status != "" && status != tools.TaskStatusPending {
		t.Setenv(g1LedgerEnv, sb.db)
		var steps []string
		switch status {
		case tools.TaskStatusInProgress:
			steps = []string{tools.TaskStatusInProgress}
		case tools.TaskStatusCancelled:
			steps = []string{tools.TaskStatusCancelled}
		case tools.TaskStatusFailed:
			steps = []string{tools.TaskStatusInProgress, tools.TaskStatusFailed}
		case tools.TaskStatusCompleted:
			steps = []string{tools.TaskStatusInProgress, tools.TaskStatusCompleted}
		case tools.TaskStatusVerified:
			steps = []string{tools.TaskStatusInProgress, tools.TaskStatusCompleted, tools.TaskStatusVerified}
		default:
			t.Fatalf("g1Seed: status %q has no legal ladder from pending", status)
		}
		for _, step := range steps {
			if _, err := tools.TaskUpdate(g1FixtureID, step); err != nil {
				t.Fatalf("seed row %s → %s: %v", g1FixtureID, step, err)
			}
		}
	}
	if branch != "" {
		t.Setenv(g1LedgerEnv, sb.db)
		if err := tools.MergeVerifySetDeliverableBranchForTest(g1FixtureID, filepath.Join(sb.home, "seeded-repo"), branch); err != nil {
			t.Fatalf("seed deliverable branch %s: %v", g1FixtureID, err)
		}
	}
}

// --- one case per advertised verb ---------------------------------------------
//
// Each case is BASIC by design: the help's own invocation on the happy path,
// plus the verb's first refusal (the mis-invocation an operator makes before he
// reads the description). Deeper contracts (the exit-code algebra, the guards'
// ladders, the JSON shapes) already have their own homes — the point here is
// that the verb has been RUN at all.

func TestTaskVerbListRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, "")
	out := sb.g1RunOK(t, "task", taskverbs.List, "--db", sb.db)
	if !strings.Contains(out, g1FixtureID) {
		t.Errorf("`gopi task list` must show the seeded row:\n%s", out)
	}
	// --status is the documented view filter; --all is its complement.
	if o := sb.g1RunOK(t, "task", taskverbs.List, "--status", tools.TaskStatusInProgress, "--db", sb.db); !strings.Contains(o, g1FixtureID) {
		t.Errorf("`gopi task list --status in_progress` must show the running row:\n%s", o)
	}
	if o := sb.g1RunOK(t, "task", taskverbs.List, "--all", "--db", sb.db); !strings.Contains(o, g1FixtureID) {
		t.Errorf("`gopi task list --all` must include the row:\n%s", o)
	}
	// First refusal: a status outside the closed vocabulary.
	out, code := sb.g1Run(t, "task", taskverbs.List, "--status", "bogus", "--db", sb.db)
	if code == 0 {
		t.Fatalf("an invalid --status must be refused:\n%s", out)
	}
	if !strings.Contains(out, "invalid status") {
		t.Errorf("the refusal must name the problem:\n%s", out)
	}
}

func TestTaskVerbShowRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, "")
	out := sb.g1RunOK(t, "task", taskverbs.Show, g1FixtureID, "--db", sb.db)
	for _, want := range []string{g1FixtureID, g1FixtureSummary, "test-model"} {
		if !strings.Contains(out, want) {
			t.Errorf("`gopi task show` output must carry %q:\n%s", want, out)
		}
	}
	// First refusal: the positional the synopsis requires.
	out, code := sb.g1Run(t, "task", taskverbs.Show, "--db", sb.db)
	if code == 0 {
		t.Fatalf("`gopi task show` without a task_id must be refused:\n%s", out)
	}
	if !strings.Contains(out, "missing required parameter") {
		t.Errorf("the refusal must name the missing positional:\n%s", out)
	}
}

// TestRetiredTaskVerbsDispatchARemoval is the contract the four READ-ONLY
// audits retired on 2026-10-30 (task 01a18799) still owe their callers.
//
// They are gone from the help and from taskverbs.FamilyVerbs, but the SPELLING
// still dispatches (taskverbs.RemovedVerbs) — because a command recorded in a
// task row, a briefing, a briefing's payload or a shell history is the one
// input this surface cannot migrate. The shape that would be a defect is the
// generic usage dump: it sends the reader hunting a typo that does not exist,
// and it says nothing about where the function went. So all four are asserted
// here, on the BUILT BINARY through the same sandbox the other verb tests use:
//
//	(1) the exit is NON-ZERO — nothing was measured, and a script that gated on
//	    one of these must hear "this surface is gone", never read exit 0 as a
//	    clean audit (the `verdicts` retirement's rule, applied to a readout);
//	(2) stderr names the verb AND says "removed";
//	(3) it names a REPLACEMENT — a surviving spelling the operator can type;
//	(4) no advertisement survives: the family help does not carry the spelling.
func TestRetiredTaskVerbsDispatchARemoval(t *testing.T) {
	sb := g1NewSandbox(t)
	retired := []struct {
		verb    string
		args    []string
		replace string
	}{
		{
			// The row-level question survives as a FLAG on the read.
			verb:    taskverbs.ForBranch,
			args:    []string{"feat/g1-owned-branch"},
			replace: "gopi task show --branch",
		},
		{
			// The ref facts are read from their owners: git, and the machinery
			// that consumes refs.
			verb:    taskverbs.BranchRefs,
			args:    []string{"--peer", "peer1"},
			replace: "git ls-remote",
		},
		{
			verb:    taskverbs.Orphans,
			args:    []string{"--delivery", "peer1"},
			replace: "gopi task show",
		},
		{
			verb:    taskverbs.Stranded,
			args:    nil,
			replace: "gopi task list --status pending",
		},
		// SLICE 2/4 (2026-10-30, task 01a18799): the three implementation-shaped
		// verbs. Each replacement string is the FIRST thing its sentence names —
		// the command the reader types next, not prose about the removal.
		{
			// The verification pass was a SECOND SPAWN judging a run; the verdict
			// belongs to the run that measured it.
			verb:    taskverbs.Verify,
			args:    []string{g1FixtureID, "--timeout", "1"},
			replace: "gopi task update <id> --status verified",
		},
		{
			// The blocked-delivery repair went with the peer/branch hand-off.
			// It must NOT point at `gopi task finish-cleanup`: that spelling was
			// retired in the SAME epic (`58b5618b`, slice 3/4), and a removal
			// message naming another removed spelling sends the reader in a
			// circle — the exact defect taskRemovedAdvice's own comment warns
			// about for the manual-claim case. It points at where the delivery
			// FACTS are readable instead.
			verb:    taskverbs.RetryDelivery,
			args:    []string{"--task", g1FixtureID},
			replace: "git ls-remote",
		},
		{
			// The terminal-row close-out chain. Its own case exists in
			// taskRemovedAdvice now; this arm is what makes a MISSING case red
			// (before it, the refusal fell through to the manual-CLAIM sentence
			// written for `next`, and told an operator to go claim work).
			verb:    taskverbs.FinishCleanup,
			args:    []string{"--task", g1FixtureID},
			replace: "gopi task sweep-worktrees",
		},
		{
			// A verb whose body was "run these other legs" has no content of its
			// own; each leg has a command that names it.
			verb:    taskverbs.Reconcile,
			args:    []string{"--json"},
			replace: "gopi task list",
		},
	}
	help := tasksUsage
	for _, r := range retired {
		// The removal is a REFUSAL, not a run: nothing is claimed, nothing is
		// written, and the exit code says so.
		withLedger := append(append([]string{"task", r.verb}, r.args...), "--db", sb.db)
		out, code := sb.g1Run(t, withLedger...)
		if code == 0 {
			t.Errorf("`gopi task %s` exited 0 — a retired readout must not read as a clean audit:\n%s", r.verb, out)
		}
		if !strings.Contains(out, r.verb) || !strings.Contains(out, "removed") {
			t.Errorf("`gopi task %s` must name the verb and say \"removed\":\n%s", r.verb, out)
		}
		if !strings.Contains(out, r.replace) {
			t.Errorf("`gopi task %s` must name the replacement %q — a removal that says only \"gone\" leaves the reader nowhere to go:\n%s",
				r.verb, r.replace, out)
		}
		// (3b) A REMOVAL MUST NOT SEND THE READER IN A CIRCLE. Naming a
		// surviving command is necessary and NOT sufficient: the sentence can
		// satisfy (3) with its first command and still name a RETIRED spelling
		// later on, so the reader who types that one arrives at a second removal
		// message — the defect `c5548b88` repaired in `retry-delivery` and
		// `reconcile` and MISSED in `branch-refs`, where the sentence went on to
		// name `gopi task finish-cleanup` (retired by the same epic) as "what a
		// terminal row's delivery should do". Measured on the binary at
		// `c5548b88`: `gopi task branch-refs --peer peer1` printed it and exited
		// 1, and `gopi task finish-cleanup` printed a SECOND removal and exited
		// 1. (3) alone is why that shipped green — it checks the ENTRANCE to the
		// sentence, not the far side of it. This arm checks the whole sentence.
		for _, dead := range taskverbs.RemovedVerbs {
			if dead == r.verb {
				continue // the sentence names itself on purpose: "gopi task X: removed"
			}
			if strings.Contains(out, "gopi task "+dead) {
				t.Errorf("`gopi task %s` names the ALSO-RETIRED spelling `gopi task %s` as a replacement — the reader who types it gets a second removal message instead of the command they asked for:\n%s",
					r.verb, dead, out)
			}
		}
		// ...and the same command WITHOUT the ledger flag reaches the same
		// sentence: the refusal sits before the ledger is opened, so it cannot
		// depend on a readable ledger (a network or disk problem must never be
		// able to turn "retired" into "empty").
		out, code = sb.g1Run(t, append([]string{"task", r.verb}, r.args...)...)
		if code == 0 || !strings.Contains(out, "removed") {
			t.Errorf("`gopi task %s` (no --db) must print the same removal, exit non-zero: exit=%d\n%s", r.verb, code, out)
		}
		// (4) Nothing advertises it: the family help's SYNOPSIS set is
		// g1AdvertisedVerbs' source, and the entry text is the page itself.
		if strings.Contains(help, "gopi task "+r.verb) {
			t.Errorf("`gopi task %s` is still advertised in `gopi task -h` — a retired spelling appears in no advertised group:\n%s",
				r.verb, help)
		}
	}
}

func TestTaskVerbStatusRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusPending, "")
	out := sb.g1RunOK(t, "task", taskverbs.Status, g1FixtureID, "--db", sb.db)
	if !strings.Contains(out, g1FixtureID) || !strings.Contains(out, tools.TaskStatusPending) {
		t.Errorf("`gopi task status` must report the row and its state:\n%s", out)
	}
	if o := sb.g1RunOK(t, "task", taskverbs.Status, g1FixtureID, "--json", "--db", sb.db); !strings.Contains(o, `"task_id"`) {
		t.Errorf("`gopi task status --json` must render JSON:\n%s", o)
	}
	// First refusal: an unknown row is not a silent "not running".
	out, code := sb.g1Run(t, "task", taskverbs.Status, "t-g1-does-not-exist", "--db", sb.db)
	if code == 0 {
		t.Errorf("an unknown row must not exit 0 (a blank success is the defect the verb exists to remove):\n%s", out)
	}

	// THE DELIVERABLE SELECTORS (epic task-status-accepts-branch-or-worktree):
	// the help now advertises --branch/--worktree on THIS verb, so the help's
	// own argv is driven here too — the zombie question asked from the worktree,
	// where the row id is the one fact the operator does not hold.
	g1StatusStampBranch(t, sb, g1StatusSelectorBranch)
	byBranch := sb.g1RunOK(t, "task", taskverbs.Status, "--branch", g1StatusSelectorBranch, "--db", sb.db)
	if !strings.Contains(byBranch, g1FixtureID) {
		t.Errorf("`gopi task status --branch B` must report the row that records B:\n%s", byBranch)
	}
	if byBranch != g1StatusVerdictByID(t, sb) {
		t.Errorf("the verdict reached by --branch must be the one reached by the row id:\n%s", byBranch)
	}
	// A selector NO row records is an ANSWER about the ledger with its own code
	// — never exit 1, which on this verb means "running; do NOT resume".
	noOwner, noOwnerCode := sb.g1Run(t, "task", taskverbs.Status, "--branch", g1StatusSelectorNoOwner, "--db", sb.db)
	if noOwnerCode != tools.TaskForBranchExitNoOwner {
		t.Errorf("a selector no row records: exit = %d, want %d (the for-branch no-owner code)\n%s",
			noOwnerCode, tools.TaskForBranchExitNoOwner, noOwner)
	}
	if noOwnerCode == taskStatusExitRunning {
		t.Errorf("the refusal exited %d — the code that means RUNNING:\n%s", noOwnerCode, noOwner)
	}
}

// g1StatusSelectorBranch / g1StatusSelectorNoOwner are the two branches the
// status cases below name: one a row records, one nothing does.
const (
	g1StatusSelectorBranch  = "feat/g1-status-selector"
	g1StatusSelectorNoOwner = "feat/g1-status-nobody-owns"
)

// g1StatusStampBranch stamps the fixture row's deliverable with a branch, the
// way the dispatch path does — through the ledger writer, never a raw INSERT.
func g1StatusStampBranch(t *testing.T, sb g1Sandbox, branch string) {
	t.Helper()
	t.Setenv(g1LedgerEnv, sb.db)
	if err := tools.MergeVerifySetDeliverableBranchForTest(g1FixtureID, filepath.Join(sb.home, "seeded-repo"), branch); err != nil {
		t.Fatalf("stamp deliverable branch %s: %v", branch, err)
	}
}

// g1StatusVerdictByID is the same verdict reached through the canonical address, taken
// AFTER the branch stamp so the two readings describe the same ledger state.
func g1StatusVerdictByID(t *testing.T, sb g1Sandbox) string {
	t.Helper()
	return sb.g1RunOK(t, "task", taskverbs.Status, g1FixtureID, "--db", sb.db)
}
func TestTaskVerbArchiveRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusVerified, "feat/g1-archived")
	out := sb.g1RunOK(t, "task", taskverbs.Archive, g1FixtureID, "--db", sb.db)
	if !strings.Contains(out, "archived") || !strings.Contains(out, "resolved") {
		t.Errorf("archive must report the `resolved` tag it wrote:\n%s", out)
	}
	// The row keeps its verdict (append-only) with the tag added.
	shown := sb.g1RunOK(t, "task", taskverbs.Show, g1FixtureID, "--db", sb.db)
	if !strings.Contains(shown, tools.TaskStatusVerified) || !strings.Contains(shown, "resolved") {
		t.Errorf("archive must preserve the row and stamp the tag:\n%s", shown)
	}

	// First refusal: a NON-TERMINAL row is not archivable. This is the verb's
	// central guard, and the refusal names the state.
	sb2 := g1NewSandbox(t)
	g1Seed(t, sb2, tools.TaskStatusInProgress, "")
	out, code := sb2.g1Run(t, "task", taskverbs.Archive, g1FixtureID, "--db", sb2.db)
	if code == 0 {
		t.Fatalf("archiving a running row must be refused:\n%s", out)
	}
	if !strings.Contains(out, "not terminal") {
		t.Errorf("the refusal must name the reason:\n%s", out)
	}
}

func TestTaskVerbUpdateRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, "")
	out := sb.g1RunOK(t, "task", taskverbs.Update, g1FixtureID,
		"--status", tools.TaskStatusFailed, "--reason", "g1 probe failure", "--db", sb.db)
	if !strings.Contains(out, "status="+tools.TaskStatusFailed) || !strings.Contains(out, "g1 probe failure") {
		t.Errorf("update must report the state and the reason:\n%s", out)
	}
	shown := sb.g1RunOK(t, "task", taskverbs.Show, g1FixtureID, "--db", sb.db)
	if !strings.Contains(shown, "g1 probe failure") {
		t.Errorf("the reason must be readable on the row afterwards:\n%s", shown)
	}

	// First refusal: the verified-closure gate needs an evidence digest.
	// --result is the second carrier the synopsis documents.
	sb2 := g1NewSandbox(t)
	g1Seed(t, sb2, tools.TaskStatusCompleted, "")
	out, code := sb2.g1Run(t, "task", taskverbs.Update, g1FixtureID, "--status", tools.TaskStatusVerified, "--db", sb2.db)
	if code == 0 {
		t.Fatalf("a self-reported `verified` with no evidence must be refused:\n%s", out)
	}
	if !strings.Contains(out, "requires an evidence digest") {
		t.Errorf("the refusal must name the missing evidence:\n%s", out)
	}
	// And the documented carrier is accepted.
	if o := sb2.g1RunOK(t, "task", taskverbs.Update, g1FixtureID, "--status", tools.TaskStatusVerified,
		"--result", "evidence: go test ./cmd/agent -run TestTaskVerbUpdateRunsAsAdvertised PASS", "--db", sb2.db); !strings.Contains(o, "status="+tools.TaskStatusVerified) {
		t.Errorf("the documented --result carrier must be accepted:\n%s", o)
	}
}
func TestTaskVerbSweepWorktreesRunsAsAdvertised(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, "")

	// Happy path: the bare run is the PLAN (the synopsis says so) and reports
	// itself as a dry run.
	out := sb.g1RunOK(t, "task", taskverbs.SweepWorktrees, "--db", sb.db)
	if !strings.Contains(out, "dry-run") && !strings.Contains(out, "sweep-worktrees") {
		t.Errorf("the bare run must render the plan and say it is one:\n%s", out)
	}
	// --json is the machine-readable plan the synopsis advertises. THIS IS THE
	// REGRESSION CASE: it was refused with "unknown flag --json" while
	// runTaskSweepWorktrees read flags.jsonOut (G1's measured defect).
	js := sb.g1RunOK(t, "task", taskverbs.SweepWorktrees, "--json", "--db", sb.db)
	if !strings.HasPrefix(strings.TrimSpace(js), "{") {
		t.Errorf("`gopi task sweep-worktrees --json` must render a JSON plan, got:\n%s", js)
	}
	// The documented flag pair and their values.
	if o := sb.g1RunOK(t, "task", taskverbs.SweepWorktrees, "--dry-run", "--min-age-h", "1", "--max", "5", "--db", sb.db); !strings.Contains(o, "sweep-worktrees") {
		t.Errorf("the documented flag combination must run:\n%s", o)
	}
	// First refusal: --min-age-h is documented as a POSITIVE integer of hours.
	out, code := sb.g1Run(t, "task", taskverbs.SweepWorktrees, "--min-age-h", "0", "--db", sb.db)
	if code == 0 {
		t.Fatalf("--min-age-h 0 must be refused:\n%s", out)
	}
	if !strings.Contains(out, "invalid --min-age-h") {
		t.Errorf("the refusal must name the flag and the domain:\n%s", out)
	}
}

// TestTaskVerbRetiredFinishCleanupExitsNonZero pins the RETIRED `finish-cleanup`
// on the REAL binary — the same contract TestTaskVerbRetiredSweepZombiesExitsNonZero
// pins for its own verb, and for the same reason.
//
// WHAT REPLACED TestTaskVerbFinishCleanupRunsAsAdvertised, and WHY THAT ONE HAD
// TO GO. It drove the verb end to end (`sb.g1RunOK(…)`, four arms) and asserted
// its render — coverage of a capability that no longer exists. `58b5618b`
// (task 01a18799 slice 3/4) retired the close-out chain with the
// worktree+branch delivery semantics it served: a run releases its own ground
// as it ends, so there is nothing left for an operator to sweep afterwards. The
// verb now REFUSES with exit 1, so a G1 test that demands exit 0 was red on a
// tree carrying no defect of its own — and, worse, a test that keeps exercising
// a retired verb is exactly how the verb comes back.
//
// THE DEFECT THIS ALSO PINS, measured on `905755a9`: the refusal was WRONG.
// `taskRemovedAdvice` had no case for the verb, so the sentence fell through to
// the one written for the retired manual-CLAIM spelling (`next`):
//
//	gopi task finish-cleanup: removed — manual claiming was removed — use "gopi worker" …
//
// An operator asking to close out one row was told to go claim work. That is an
// answer to a question nobody asked, on the one surface whose whole job is to
// name where the capability went (Rule 15). The arms below therefore assert the
// sentence names THIS verb's own retirement AND a replacement an operator can
// actually type.
func TestTaskVerbRetiredFinishCleanupExitsNonZero(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusPending, "")

	out, code := sb.g1Run(t, "task", taskverbs.FinishCleanup, "--db", sb.db)
	if code == 0 {
		t.Fatalf("the retired verb must exit non-zero; a 0 would tell an old script the close-out ran:\n%s", out)
	}
	// It names itself and states the removal.
	if !strings.Contains(out, taskverbs.FinishCleanup) || !strings.Contains(out, "removed") {
		t.Errorf("the refusal must name the verb and say \"removed\":\n%s", out)
	}
	// THE ANSWER MUST BE ABOUT THIS VERB. The manual-claim sentence is the one
	// a missing case produces; it is wrong here, so it is refused by name.
	if strings.Contains(out, "manual claiming was removed") {
		t.Errorf("`gopi task %s` answered with the MANUAL-CLAIM retirement sentence — taskRemovedAdvice has no case for this verb, so an operator who asked to close out a row is told to go claim work:\n%s",
			taskverbs.FinishCleanup, out)
	}
	// ...and it leaves the reader somewhere to go: the legs the pass bundled are
	// each reachable by their own command.
	if !strings.Contains(out, "gopi task sweep-worktrees") {
		t.Errorf("the removal must name the surviving worktree reap (`gopi task sweep-worktrees --apply`) — a removal that says only \"gone\" leaves the reader nowhere to go:\n%s", out)
	}
	// It does NOT dump the family usage page (a reader should not have to diff a
	// screen), and nothing advertises it any more.
	if strings.Contains(out, "GoPi task subcommands") {
		t.Errorf("the retired verb must answer with ONE sentence, not the family usage page:\n%s", out)
	}
	if strings.Contains(tasksUsage, "gopi task "+taskverbs.FinishCleanup) {
		t.Errorf("`gopi task %s` is still advertised in `gopi task -h` — a retired spelling appears in no advertised group", taskverbs.FinishCleanup)
	}
}

// TestTaskVerbRetiredSweepZombiesExitsNonZero pins the RETIRED verb on the REAL
// binary (devdoc/zombie-sweep-removed.md, 2026-10-26).
//
// WHAT REPLACED TestTaskVerbSweepZombiesRunsAsAdvertised, and WHY THAT ONE HAD
// TO GO. It drove the verb end to end and asserted its render, its flags and
// their refusals — coverage of a capability that no longer exists. A test that
// keeps exercising a deleted verb is how the verb comes back: the test is what
// makes removing the code look like a regression. So the verb left the
// advertised surface (g1CoveredVerbs / g1ExpectedAdvertisedVerbs above, whose
// derivation gate would otherwise fail) and the coverage became the assertion
// that a caller still gets a POINTED, non-zero answer rather than silence.
//
// The distinction matters for a deployed script: exiting 0 with no output would
// tell an old automation call "nothing to sweep", which is a lie. Exit 1 plus a
// sentence that names the replacement is the honest contract (Rule 15).
func TestTaskVerbRetiredSweepZombiesExitsNonZero(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, "")

	out, code := sb.g1Run(t, "task", taskverbs.SweepZombies, "--db", sb.db)
	if code == 0 {
		t.Fatalf("the retired verb must exit non-zero; a 0 would tell an old script there was nothing to sweep:\n%s", out)
	}
	// It names its retirement, and it does NOT dump the family usage page (a
	// reader should not have to diff a screen).
	if !strings.Contains(out, "removed") {
		t.Errorf("the refusal must say the verb was removed:\n%s", out)
	}
	if strings.Contains(out, "GoPi task subcommands") {
		t.Errorf("the retired spelling dumped the family usage instead of a pointed message:\n%s", out)
	}
	// The retired flags go with it: an accepted-and-ignored flag would be
	// Rule 15's silent failure wearing a parser arm.
	for _, argv := range [][]string{{"--stall-sec", "10"}, {"--max-run", "60"}, {"--kill"}, {"--dry-run"}} {
		full := append([]string{"task", taskverbs.SweepZombies}, append(argv, "--db", sb.db)...)
		if o, c := sb.g1Run(t, full...); c == 0 {
			t.Errorf("%v must not succeed on the retired verb:\n%s", argv, o)
		}
	}
}

func TestTaskVerbSearchRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, "")

	out := sb.g1RunOK(t, "task", taskverbs.Search, "surface fixture", "--db", sb.db)
	if !strings.Contains(out, g1FixtureID) {
		t.Errorf("search must find the row by summary text:\n%s", out)
	}
	// A miss is an ANSWER, and it says so (never a blank success).
	miss := sb.g1RunOK(t, "task", taskverbs.Search, "zzz-g1-definitely-absent", "--db", sb.db)
	if !strings.Contains(miss, "No tasks matching") {
		t.Errorf("a miss must be reported by name:\n%s", miss)
	}
	// First refusal: the synopsis requires the keyword.
	out, code := sb.g1Run(t, "task", taskverbs.Search, "--db", sb.db)
	if code == 0 {
		t.Fatalf("search without a keyword must be refused:\n%s", out)
	}
	if !strings.Contains(out, "missing required parameter") {
		t.Errorf("the refusal must name the missing positional:\n%s", out)
	}
}

func TestTaskVerbImprovementsRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusCompleted, "")

	// The synopsis advertises this verb with NO flags at all, and the run reads
	// the ambient ledger.
	t.Setenv(g1LedgerEnv, sb.db)
	out := sb.g1RunOK(t, "task", taskverbs.Improvements)
	if !strings.Contains(out, "agency scan") {
		t.Errorf("improvements must render the miner's report:\n%s", out)
	}
	// The synopsis's second promise: run it twice, the output is identical
	// (the miner is a pure function of the ledger).
	again := sb.g1RunOK(t, "task", taskverbs.Improvements)
	if again != out {
		t.Errorf("the miner must be a pure function of the ledger:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
	// The synopsis advertises no flags: --db is NOT part of this verb's
	// surface (the family's common flag reaches this one through the
	// environment, which is how the runner reads it). Refusing it is correct
	// help/code agreement, and the inventory records the asymmetry.
	if o, code := sb.g1Run(t, "task", taskverbs.Improvements, "--db", sb.db); code == 0 {
		t.Errorf("improvements advertises no --db; admitting it without a reader would be the accepted-and-ignored defect:\n%s", o)
	}
	// First refusal: a stray positional.
	out, code := sb.g1Run(t, "task", taskverbs.Improvements, "stray")
	if code == 0 {
		t.Fatalf("a positional must be refused:\n%s", out)
	}
	if !strings.Contains(out, "unexpected positional") {
		t.Errorf("the refusal must name the problem:\n%s", out)
	}
}

func TestTaskVerbDedupScanRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusPending, "")

	// Happy path: a clean ledger reports zero twins and exits 0.
	out := sb.g1RunOK(t, "task", taskverbs.DedupScan, "--db", sb.db)
	if !strings.Contains(out, "twin pair(s)") {
		t.Errorf("the audit must report its scan and its verdict:\n%s", out)
	}
	// --live narrows the scan to claimable rows (documented).
	if o := sb.g1RunOK(t, "task", taskverbs.DedupScan, "--live", "--db", sb.db); !strings.Contains(o, "twin pair(s)") {
		t.Errorf("--live must run the narrowed scan:\n%s", o)
	}
	// The synopsis documents exit 2 when twins are found — the CI-visible half
	// of the contract, and the reason this verb is not merely a report.
	t.Setenv(g1LedgerEnv, sb.db)
	seedG1Twin(t, sb.db)
	out, code := sb.g1Run(t, "task", taskverbs.DedupScan, "--db", sb.db)
	if code != 2 {
		t.Errorf("twins found: exit = %d, want 2 (the synopsis documents it)\n%s", code, out)
	}
	if !strings.Contains(out, "TWIN") {
		t.Errorf("the twin must be named, not merely counted:\n%s", out)
	}
	// First refusal: an unknown flag.
	if o, c := sb.g1Run(t, "task", taskverbs.DedupScan, "--bogus", "--db", sb.db); c == 0 {
		t.Errorf("an unknown flag must be refused:\n%s", o)
	}
}

// TestTaskSubmitRefusesANonUUIDRowIDAtTheCommandLine is the ARM's own proof at
// the surface the arm governs (Rule 31). epic 01a12b86 armed
// `taskSubmitRefusesUngroundableID` on runTaskSubmit; that arm is worth nothing
// unless the REAL binary refuses a slug `--task-id` and writes no row, and
// accepts a full uuid with everything else equal. The pair is the point: a
// test that only asserts the refusal cannot tell "the arm fired" from "submit
// is broken", so the uuid half runs the identical argv shape.
func TestTaskSubmitRefusesANonUUIDRowIDAtTheCommandLine(t *testing.T) {
	sb := g1NewSandbox(t)
	repo := t.TempDir()
	g1GitInit(t, repo)
	// The predicate fires only on a REPO-AWARE row (the dispatch gate's own
	// condition): without a resolvable repo nothing is derived and a slug id
	// stays legal by design.
	const slugID = "t-arm-proof-slug"
	out, code := sb.g1Run(t, "task", taskverbs.Submit, "arm proof row",
		"--payload", "the arm's own probe", "--task-id", slugID, "--repo", repo, "--db", sb.db)
	if code == 0 {
		t.Fatalf("`task submit --task-id %s --repo <work tree>` exited 0 — the arm is OFF:\n%s", slugID, out)
	}
	if !strings.Contains(out, "no ground name can be derived") {
		t.Errorf("the refusal must be the ground-name one (not an unrelated error):\n%s", out)
	}
	if strings.Contains(out, "Task queued") {
		t.Errorf("a refused submit must not report a queued row:\n%s", out)
	}
	// Nothing was written: `task show` on an unknown row is non-zero.
	if shown, showCode := sb.g1Run(t, "task", taskverbs.Show, slugID, "--db", sb.db); showCode == 0 {
		t.Errorf("a REFUSED submit wrote a row anyway:\n%s", shown)
	}

	// The SAME argv with a full uuid is accepted — the refusal is about the id
	// shape, not about the command.
	const uuidID = "1d7e2586-1b68-5f8d-a607-fbb086669628"
	out = sb.g1RunOK(t, "task", taskverbs.Submit, "arm proof row",
		"--payload", "the arm's own probe", "--task-id", uuidID, "--repo", repo, "--db", sb.db)
	if !strings.Contains(out, uuidID) || !strings.Contains(out, "queued") {
		t.Errorf("a full-uuid id must still queue:\n%s", out)
	}
}

func TestTaskVerbSubmitRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	// The synopsis's minimal spelling: a summary positional.
	out := sb.g1RunOK(t, "task", taskverbs.Submit, "g1 submitted row", "--db", sb.db)
	if !strings.Contains(out, "queued") || !strings.Contains(out, tools.TaskStatusPending) {
		t.Errorf("submit must report the row and its state:\n%s", out)
	}
	// A payload-less row is the documented, warned-about shape: the summary is
	// mirrored into the payload column so the row is claimable at all.
	if !strings.Contains(out, "positional summary is mirrored into the payload") {
		t.Errorf("submit must WARN about the mirrored payload (the row would otherwise be un-runnable):\n%s", out)
	}

	// The documented flag sweep on ONE invocation, so every advertised flag is
	// exercised at the surface it is advertised on.
	briefPath := filepath.Join(sb.home, "brief.txt")
	if err := os.WriteFile(briefPath, []byte("g1 attachment payload"), 0o600); err != nil {
		t.Fatalf("write brief: %v", err)
	}
	repo := t.TempDir()
	g1GitInit(t, repo)
	// The parent must be a row that EXISTS (a dangling parent id is rejected,
	// which is itself part of the documented contract) — the fixture row is
	// seeded into this sandbox's ledger for exactly that reason.
	g1Seed(t, sb, tools.TaskStatusPending, "")
	attachPath := filepath.Join(sb.home, "evidence.txt")
	if err := os.WriteFile(attachPath, []byte("g1 attachment body"), 0o600); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	out = sb.g1RunOK(t, "task", taskverbs.Submit, "g1 fully flagged row",
		"--payload", "@"+briefPath, "--tags", "g1,docs", "--parent-task-id", g1FixtureID,
		"--source", "human", "--task-id", "1d7e2586-1b68-5f8d-a607-fbb086669628", "--session-id", "sess-g1-submit",
		"--repo", repo, "--model", "test-model", "--priority", "7",
		"--attachment", attachPath, "--db", sb.db)
	if !strings.Contains(out, "1d7e2586-1b68-5f8d-a607-fbb086669628") || !strings.Contains(out, "queued") {
		t.Errorf("the fully-flagged submit must succeed with the documented flags:\n%s", out)
	}
	// One row per documented flag group is not enough — assert the STAMPED row,
	// so an accepted-and-ignored flag cannot pass.
	shown := sb.g1RunOK(t, "task", taskverbs.Show, "1d7e2586-1b68-5f8d-a607-fbb086669628", "--db", sb.db)
	for _, want := range []string{"test-model", g1FixtureID, "g1 attachment payload"} {
		if !strings.Contains(shown, want) {
			t.Errorf("submit --… must stamp %q on the row:\n%s", want, shown)
		}
	}
	// --session-id: assert the COLUMN the flag is documented to write, through
	// the ledger's own reader, not through a rendered page.
	//
	// WHY NOT `strings.Contains(shown, "sess-g1-submit")`. Until e71b998 the
	// submitter and the run session shared the `session_id` cell, so the
	// rendered `session_id:` line was where a submit's session id showed up.
	// That commit split the two facts (tasks.go: taskSubmitterColumnName /
	// Task's SubmitterSession docs): `--session-id` names WHO FILED the row and
	// fills `submitter_session`; `session_id` is the row's RUN session, which a
	// pending row does not have yet and which the deferred stamp fills only
	// while that cell is EMPTY. A pending row therefore renders an empty
	// `session_id:` BY DESIGN — the page cannot prove or disprove the flag any
	// more, and asserting the old rendering is what made this arm red.
	// Measured at c3b1680 on the real binary: `task submit … --session-id
	// sess-g1-submit` writes submitter_session='sess-g1-submit' and leaves
	// session_id=''.
	//
	// The accepted-and-ignored property is preserved and strengthened: the
	// SUBMITTER column is read back through the one accessor the claim funnel's
	// anti-self-reference guard uses, and the run-session column is asserted
	// EMPTY (so a future regression that stuffs the submitter back into the run
	// cell reddens here too).
	t.Setenv(g1LedgerEnv, sb.db)
	submittedBy, err := tools.TaskSubmitterSession("1d7e2586-1b68-5f8d-a607-fbb086669628")
	if err != nil {
		t.Fatalf("read the submitter column back: %v", err)
	}
	if submittedBy != "sess-g1-submit" {
		t.Errorf("submit --session-id must stamp the SUBMITTER column (want %q, got %q) — the flag was accepted but not recorded:\n%s",
			"sess-g1-submit", submittedBy, shown)
	}
	if runSID, err := tools.TaskSessionID("1d7e2586-1b68-5f8d-a607-fbb086669628"); err != nil {
		t.Fatalf("read the run-session column back: %v", err)
	} else if runSID != "" {
		t.Errorf("a PENDING row must leave the RUN session empty (got %q): --session-id names the filer, and the deferred stamp owns that cell after the split in e71b998:\n%s",
			runSID, shown)
	}

	// First refusal: the synopsis requires the summary.
	out, code := sb.g1Run(t, "task", taskverbs.Submit, "--db", sb.db)
	if code == 0 {
		t.Fatalf("submit without a summary must be refused:\n%s", out)
	}
	if !strings.Contains(out, "missing required parameter") {
		t.Errorf("the refusal must name the missing positional:\n%s", out)
	}
	// Second refusal: --repo is verified as a git repo BEFORE the row is
	// written (the synopsis promises the abort).
	out, code = sb.g1Run(t, "task", taskverbs.Submit, "g1 bad repo", "--repo", t.TempDir(), "--db", sb.db)
	if code == 0 {
		t.Fatalf("a non-git --repo must abort the submit:\n%s", out)
	}
}

// TestTaskVerbCloneRunsAsAdvertised covers `gopi task clone`: the clean-room
// copy of a FINISHED row, its two flags, and the first refusal (a row that may
// still be running).
func TestTaskVerbCloneRunsAsAdvertised(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusFailed, "")

	// A NEW pending row with its OWN uuid, parented to the source, stating the
	// copy. The id is READ OFF THE OUTPUT — a clone mints a fresh uuid v7
	// (operator ruling 2026-11-05: an id is an identity, so `<src>-c<N>` is not
	// minted any more), and a test that hardcoded the old suffix would be
	// asserting the very shape the ruling removed.
	out := sb.g1RunOK(t, "task", taskverbs.Clone, g1FixtureID, "--db", sb.db)
	if !strings.Contains(out, "parent="+g1FixtureID) {
		t.Errorf("clone must create a NEW child row and say so:\n%s", out)
	}
	cloneID := g1ClonedID(t, out)
	if strings.Contains(cloneID, g1FixtureID) {
		t.Errorf("the clone's id %q carries the SOURCE id — a minted uuid must not encode its parent:\n%s", cloneID, out)
	}
	shown := sb.g1RunOK(t, "task", taskverbs.Show, cloneID, "--db", sb.db)
	if !strings.Contains(shown, g1FixtureSummary) {
		t.Errorf("the clone's briefing must carry the source's summary verbatim:\n%s", shown)
	}
	if !strings.Contains(shown, tools.TaskCloneMarker) {
		t.Errorf("the clone row must say what it is (%q):\n%s", tools.TaskCloneMarker, shown)
	}

	// --dry-run plans without writing: the row it names must not appear.
	sb2 := g1NewSandbox(t)
	g1Seed(t, sb2, tools.TaskStatusCompleted, "")
	plan := sb2.g1RunOK(t, "task", taskverbs.Clone, g1FixtureID, "--dry-run", "--db", sb2.db)
	if !strings.Contains(plan, tools.TaskCloneDryRunMarker) {
		t.Errorf("--dry-run must state the plan and that nothing was written:\n%s", plan)
	}
	plannedID := g1ClonedID(t, plan)
	if listed, code := sb2.g1Run(t, "task", taskverbs.Show, plannedID, "--db", sb2.db); code == 0 {
		t.Errorf("--dry-run must not create the row, but %s is there:\n%s", plannedID, listed)
	}

	// First refusal: a row that may be RUNNING is not cloneable (its ground has
	// an owner), and the refusal must say why.
	sb3 := g1NewSandbox(t)
	g1Seed(t, sb3, tools.TaskStatusInProgress, "")
	out, code := sb3.g1Run(t, "task", taskverbs.Clone, g1FixtureID, "--db", sb3.db)
	if code == 0 {
		t.Fatalf("cloning an in_progress row must be refused:\n%s", out)
	}
	if !strings.Contains(out, "may be RUNNING") {
		t.Errorf("the refusal must name the reason:\n%s", out)
	}
}

// --- helpers ------------------------------------------------------------------

// seedG1Twin writes a second row carrying the SAME payload as the fixture row,
// which is the audit's `exact` gate — the one shape that needs no similarity
// heuristic to detect.
func seedG1Twin(t *testing.T, dbPath string) {
	t.Helper()
	t.Setenv(g1LedgerEnv, dbPath)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "g1 twin row",
		Payload: "g1 twin payload body",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed twin a: %v", err)
	}
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "g1 twin row",
		Payload: "g1 twin payload body",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed twin b: %v", err)
	}
}

// g1GitInit makes dir a git repository — submit's --repo verification runs
// `git rev-parse --is-inside-work-tree` before it writes the row.
func g1GitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "g1@example.invalid"},
		{"config", "user.name", "g1 test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
		}
	}
}

// --- branch-refs: the exit algebra, asserted on the REAL binary --------------

// g1Git runs one git command (failing the test on error); an empty dir means
// the command names its own target (git init <path>).
func g1Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
