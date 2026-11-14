package testutil

// The GATE + REGRESSION guard for scripts/gopi-worker-up.sh's live-worker count
// (the 2026-09-12 miscount: "live gopi workers: 13" while `ps` showed 5).
//
// WHY IT LIVES IN testutil (and in this package's own test binary): testutil
// already owns the repo-level SCRIPT gates (scripts_gate_test.go: the /tmp ban,
// `bash -n`, the unbounded-loop ban), so this is the same kind of check in the
// same place. internal/procname imports nothing, so depending on it closes no
// cycle, and this suite's TestMain already pins the scratch root.
//
// THREE TESTS, each closing one hole:
//
//  1. TestWorkerUpCountsArgvShapeNotName — the minimal repro. A synthetic procfs
//     root (the script's procfs-root seam) holds three REAL worker shapes and a
//     set of decoys: a one-shot task child that re-execs the same binary, a
//     serve daemon, argv traps (a verb merely CONTAINED in argv[1], an empty
//     argv[1], an empty cmdline), a differently-named binary, and — the decoy
//     this repo has been bitten by repeatedly — a SHELL whose command line
//     mentions the worker command. The count must be exactly the three workers
//     and must report the rest separately. The same test computes what the OLD
//     predicate (`pgrep -x <name> | wc -l`, a NAME-only count) returns on that
//     fixture and asserts it is STRICTLY LARGER — so the test fails on the old
//     code rather than merely passing on the new one.
//
//  2. TestWorkerUpScriptNamesItsValues — Rule 13/14: the script's env-default
//     lines are the definition site of these names on the SHELL side and must
//     equal internal/procname's constants (the Go side). A rename on either
//     side reddens this test instead of silently drifting apart.
//
//  3. TestWorkerUpScriptHasNoPgrepOrPkill — the counting METHOD itself: no
//     name/pattern matcher at all. `pgrep -x <name>` answers "which processes
//     are NAMED like the binary", and the caller asked "which processes are
//     WORKERS"; `pkill` in this repo's history has killed live work.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// workerUpScriptName is the launcher under test. The script itself derives
	// its own name from $0, so this is the test's spelling of the path, once.
	workerUpScriptName = "gopi-worker-up.sh"
	// workerUpScriptRel is the repo-root-relative path of the launcher.
	workerUpScriptRel = scriptsDirName + "/" + workerUpScriptName
	// workerUpCountFlag is the counting seam: it prints the breakdown and exits
	// without launching anything.
	workerUpCountFlag = "--count"
	// fixtureWorkers is how many worker-shaped processes the fixture holds — the
	// exact number the counter must print.
	fixtureWorkers = 6
	// fixtureOthers is how many NON-worker processes of the gopi PROGRAM the
	// fixture holds (task child, task group, serve daemon, the argv traps, the
	// empty cmdline). They must be REPORTED separately, never folded into the
	// worker count.
	fixtureOthers = 6
	// fixtureUnconfirmed is how many fixture entries have NO readable image at
	// all (exe unreadable AND argv[0] empty) while comm still names the gopi
	// process — the THIRD STATE (bug 01a132a7a4ef743b). They are neither workers
	// nor others, and they must be REPORTED: an entry that matches none of the
	// three buckets is the silent drop this constant exists to pin.
	fixtureUnconfirmed = 1
	// fixtureZombiePid is the fixture entry that reproduces the measured shape:
	// a Z-state process named gopi whose exe link is unreadable and whose
	// cmdline is EMPTY (the kernel reports an empty cmdline for a zombie). On the
	// frozen host snapshot that shape was pid 3092881, and the pre-fix judge
	// removed it from the others count with no line mentioning it.
	fixtureZombiePid = "1016"
	// fixtureForeignImageWorkerPid is the fixture entry whose comm is the gopi
	// NAME and whose argv[1] is the worker verb, but whose exe link is
	// UNREADABLE and whose argv[0] names a FOREIGN program: the image channel
	// DID answer, and its answer is "not this program" — a verdict, which is a
	// different thing from the third state's "nothing answered" (Rule 15a).
	fixtureForeignImageWorkerPid = "1015"
	// fixtureZombieState is the process state letter a zombie reports in
	// /proc/<pid>/stat field 3. It is named because the fixture's whole point is
	// that the STATE is what makes the two image channels empty.
	fixtureZombieState = "Z"
	// procnameFixingImage is a gopi binary built under ANOTHER file name — the
	// measured shape of bug 01a131fcb8337c1b (/tmp/gopi-fix). Its worker is a real
	// worker of this program, and it is the fixture entry that makes a
	// comm-equality count WRONG rather than merely imprecise.
	procnameFixingImage = procname.ImageGopiPrefix + "-fix"
	// fixtureForeignImage is a program that is NOT gopi, used as the image of the
	// decoy whose comm looks like a gopi name (the reverse of the bug).
	fixtureForeignImage = "nginx"
	// procFixtureImageDir is the (synthetic) directory the fixture's exe links
	// point into. The target need not exist: readlink reports the link's target
	// whether or not the file is there, which is exactly the channel the counter
	// reads.
	procFixtureImageDir = "/synthetic/proc-fixture"
	// kernelCommTruncationBytes is how many bytes of the executable's basename the
	// kernel keeps in the comm field. It is the reason a NAME-only count cannot
	// even approximate the program identity: a long image name is truncated, so
	// the count's own criterion is not reproducible from the value it reads.
	kernelCommTruncationBytes = 15
	// fixtureMinNameOnly is the floor the name-only predicate must reach on the
	// fixture: without MORE name-matching processes than workers, the old
	// predicate would pass this test and prove nothing (a vacuous-pass guard).
	fixtureMinNameOnly = 4
	// procRootShellVar / procRootDefault are the procfs-root seam: the shell
	// variable and its default, named here so the gate compares against
	// constants rather than bare literals (Rule 1b).
	procRootShellVar = "PROC_ROOT"
	procRootDefault  = "/proc"
	// procStatFieldShellVar / procStatFieldDefault are the STAT-FIELD seam: the
	// /proc/<pid>/stat column holding starttime (field 22). It has a shell-side
	// default for the same reason the name does — a field NUMBER is a fact two
	// readers must agree on, and the gate asserts the script's default equals
	// procname.StatFieldStartTime (Rule 1a/13/14: never a literal 22 in the
	// scan loop).
	procStatFieldShellVar = "STAT_FIELD"
	procStatFieldDefault  = "22"
	// procStatMinPadding is how many filler fields procStatLine writes between
	// the state letter and starttime, so the rendered line really reaches field
	// 22 (a short line is one no reader may trust).
	procStatMinPadding = 18
)

// fixturePidWorkerIDs are the fixture entries that ARE workers; the remaining
// pids are decoys (each with its reason in writeProcFixture).
var fixtureWorkerPids = map[string]bool{
	"1001": true, "1002": true, "1003": true,
	// A process that RUNS the gopi program whose comm is the parenthesised
	// space-name "gopi worker": by the image criterion it IS a worker, and it is
	// also the fixture's stat-field/space trap (the counter must read its
	// starttime past the LAST ')'), so it is counted and its ticks are checked.
	"1009": true,
	// The gopi binary built under another file name — the bug's own case.
	fixtureFixingImagePid: true,
	// SHAPE C: the image is the gopi program while comm is a foreign name —
	// invisible to a comm comparison, a worker to the image criterion.
	"1017": true,
}

// fixtureFixingImagePid is the fixture entry whose worker runs a gopi binary
// built under ANOTHER file name (procnameFixingImage) — the entry that makes a
// comm-equality count WRONG rather than merely imprecise.
const fixtureFixingImagePid = "1014"

// envDefaultRe extracts a script-side env default:
//
//	PROC_NAME="${GOPI_PROC_NAME:-gopi}"
//
// Group 1 is the shell variable, 2 the env NAME, 3 the default VALUE. Anchored
// on the assignment, so a mention inside a comment or a message cannot match.
var envDefaultRe = regexp.MustCompile(`(?m)^([A-Z_]+)="\$\{(GOPI_[A-Z_]+):-([^}]*)\}"`)

// shellVarRefRe matches a shell default that is a REFERENCE to another shell
// variable (`$PROC_NAME`, `${PROC_NAME}`) rather than a literal — the alias form
// one-definition-per-language takes inside a single script.
var shellVarRefRe = regexp.MustCompile(`^\$\{?([A-Z_]+)\}?$`)

// shellVarRef reports whether an env default is a reference to another shell
// variable, and names it. (false) means the default is a literal, which is
// compared against the Go constant directly.
func shellVarRef(value string) (string, bool) {
	m := shellVarRefRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// procNameKnobs maps a shell variable to the env constant and the VALUE
// constant the script must agree with. A knob NOT in this table is checked by
// the gate's "unknown knob" branch instead of being silently ignored.
var procNameKnobs = map[string]struct {
	envName string
	value   string
	what    string
}{
	"PROC_NAME":           {procname.EnvName, procname.NameGopi, "the process NAME field (comm)"},
	"IMAGE_PREFIX":        {procname.EnvImagePrefix, procname.ImageGopiPrefix, "the IMAGE-name prefix of a gopi process (bin/gopi-fix counts)"},
	"WORKER_VERB":         {procname.EnvVerbWorker, procname.VerbWorker, "the top-level worker verb (argv[1] of a worker)"},
	"TASK_VERB":           {procname.EnvVerbTask, procname.VerbTask, "the verb whose `run` form re-execs the binary as a one-shot child"},
	procStatFieldShellVar: {procname.EnvStatField, strconv.Itoa(procname.StatFieldStartTime), "the /proc/<pid>/stat field holding starttime (the pid's incarnation)"},
}

// procNameIgnoredKnobs are the launcher's OTHER env knobs — real settings that
// are not the process-identity contract this gate governs, listed so the gate's
// "unknown knob" branch stays a real signal (an unlisted process-name knob is a
// missing contract, not an ordinary setting).
var procNameIgnoredKnobs = map[string]string{
	"GOPI_WORKER_N":      "how many workers to launch (a count, not an identity)",
	"GOPI_WORKER_REPO":   "the checkout whose bin/ the launcher starts",
	procname.EnvProcRoot: "the procfs root to scan (asserted separately below)",
}

// TestWorkerUpCountsArgvShapeNotName is the minimal repro for the miscount: the
// count must follow the argv SHAPE (comm == the gopi name AND argv[1] == the
// worker verb), never the process NAME alone.
func TestWorkerUpCountsArgvShapeNotName(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("%s is missing: %v", workerUpScriptRel, err)
	}
	fixture := t.TempDir()
	writeProcFixture(t, fixture)

	workers, others, pids := runWorkerUpCount(t, script, fixture)
	if workers != fixtureWorkers {
		t.Errorf("worker count = %d, want %d — the fixture holds %d worker-shaped processes (%q with argv[1] %q) "+
			"and other processes NAMED like one; a different number means the count is following the NAME, not "+
			"the argv shape (devdoc/worker-count.md)", workers, fixtureWorkers, fixtureWorkers, procname.NameGopi, procname.VerbWorker)
	}
	if others != fixtureOthers {
		t.Errorf("other-process count = %d, want %d — the non-worker processes must be REPORTED as their own "+
			"number, never folded into the worker count (a `%s %s` child re-execs this same binary and is exactly "+
			"the inflation the old name-only line produced)", others, fixtureOthers, procname.NameGopi, procname.VerbTask)
	}
	present := map[string]bool{}
	for _, id := range strings.Fields(pids) {
		pid, ticks, ok := splitPIDIdentity(id)
		if !ok {
			t.Errorf("reported worker identity %q is not a pid:starttime pair — a bare pid cannot be checked "+
				"for recycling, which is the whole reason starttime is in the line", id)
			continue
		}
		present[pid] = true
		if !fixtureWorkerPids[pid] {
			t.Errorf("pid %s is reported as a worker but the fixture does not make it one — the counter "+
				"matched a decoy", pid)
		}
		// THE INCARNATION, checked against the fixture's OWN stat line (read by
		// this test, not by the code under test): the reported ticks must be
		// field 22 of that pid's stat, reached by parsing past the LAST ')'
		// because comm may contain spaces.
		wantTicks := fixtureStartTicks(t, fixture, pid)
		if wantTicks == "" {
			continue // the fixture entry has no readable stat: the counter must say "-"
		}
		if ticks != wantTicks {
			t.Errorf("worker %s is reported with starttime %q, but its stat line's field %d is %q — the "+
				"incarnation is read from the WRONG FIELD (a whitespace split shifts every field after the "+
				"parenthesised comm, and a process name may contain spaces)", id, ticks, procname.StatFieldStartTime, wantTicks)
		}
	}
	for pid := range fixtureWorkerPids {
		if !present[pid] {
			t.Errorf("fixture worker pid %s is missing from the reported pid list %q", pid, pids)
		}
	}

	// THE OLD PREDICATE, evaluated on the same fixture: a name-only count
	// (`pgrep -x <name> | wc -l`) matches the process NAME and nothing else, so
	// every decoy that shares the name counts. Asserting it is STRICTLY larger is
	// what makes this test fail on the old code rather than merely pass on the new.
	nameOnly := nameOnlyCount(t, fixture)
	if nameOnly <= workers {
		t.Fatalf("the fixture is toothless: the name-only count (%d) must EXCEED the shape count (%d), otherwise "+
			"the old predicate would pass this very test and prove nothing", nameOnly, workers)
	}
	if nameOnly < fixtureMinNameOnly {
		t.Fatalf("only %d fixture processes are named like a gopi process (want at least %d) — the decoys that "+
			"give this test its teeth are missing, so a regression to a name-only count would go unnoticed",
			nameOnly, fixtureMinNameOnly)
	}
	t.Logf("fixture: shape count = %d workers / %d others; the OLD name-only predicate = %d "+
		"(the %d difference is exactly the reported inflation)", workers, others, nameOnly, nameOnly-workers)
}

// TestWorkerUpCountsADifferentlyBuiltGopiWorker is the minimal repro of bug
// 01a131fcb8337c1b, run against the launcher: the worker of a gopi binary built
// under ANOTHER file name must be counted. The fixture holds it (entry 1014)
// alongside three ordinary workers, and the count is asserted against the exact
// pid SET — a count that follows the executable FILE's basename (comm) reports
// zero for the differently-built worker, which is the fabricated absence the bug
// reported word for word.
func TestWorkerUpCountsADifferentlyBuiltGopiWorker(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("%s is missing: %v", workerUpScriptRel, err)
	}
	fixture := t.TempDir()
	writeProcFixture(t, fixture)

	workers, _, pids := runWorkerUpCount(t, script, fixture)
	reported := map[string]bool{}
	for _, id := range strings.Fields(pids) {
		pid, _, ok := splitPIDIdentity(id)
		if !ok {
			t.Errorf("reported worker identity %q is not a pid:starttime pair", id)
			continue
		}
		reported[pid] = true
	}
	// The differently-built gopi worker: comm == procnameFixingImage, image ==
	// procnameFixingImage. Nothing about it is "gopi"-named except its IMAGE.
	if !reported[fixtureFixingImagePid] {
		t.Errorf("the worker of the %s build (pid %s) was NOT counted — its comm is %q while its IMAGE is %q, "+
			"and a count that reads comm reports ZERO workers while it runs this very program "+
			"(bug 01a131fcb8337c1b). Reported pids: %q", procnameFixingImage, fixtureFixingImagePid,
			procnameFixingImage, procnameFixingImage, pids)
	}
	if workers != fixtureWorkers {
		t.Errorf("worker count = %d, want %d — the fixture's workers include the %s build, so the count must "+
			"follow the process's IMAGE (procname.IsGopiImage), never the executable FILE's basename",
			workers, fixtureWorkers, procnameFixingImage)
	}
	want := map[string]bool{}
	for pid := range fixtureWorkerPids {
		want[pid] = true
	}
	for pid := range reported {
		if !want[pid] {
			t.Errorf("pid %s was counted as a worker but the fixture does not make it one", pid)
		}
	}
	for pid := range want {
		if !reported[pid] {
			t.Errorf("fixture worker pid %s is missing from the reported pid list %q", pid, pids)
		}
	}
}

// TestWorkerUpReportsTheThirdState is the reproducibility guard for bug
// 01a132a7a4ef743b: an entry whose image channel answers NOTHING must get its
// own, printed state — neither counted as a worker nor folded into "other", and
// never dropped on the floor.
//
// THE MEASURED DEFECT (frozen snapshot, 2026-10-13): a Z-state process named gopi
// whose exe link is unreadable and whose cmdline is empty was removed from the
// others count by the image filter, so the operator read `other gopi processes:
// 5` where the comm judge read 6, with no line accounting for the missing entry
// (pid 3092881). The fixture reproduces that shape as entry 1016.
//
// The four shapes the epic names are each asserted by BUCKET, not by count alone,
// so a counter that moved an entry between buckets fails here with the pid named:
//
//	A  normal worker                  → workers        (1017/1001/1002/1003/1009/1014)
//	B  comm+argv say worker, image is
//	   a foreign program              → no bucket at all (a measurable verdict)
//	C  image is gopi, comm is not    → workers
//	D  Z-state, exe unreadable and
//	   argv empty                     → UNCONFIRMED, with its pid listed
func TestWorkerUpReportsTheThirdState(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("%s is missing: %v", workerUpScriptRel, err)
	}
	fixture := t.TempDir()
	writeProcFixture(t, fixture)

	workers, others, unconfirmed, pids, unconfirmedPids := runWorkerUpBreakdown(t, script, fixture)
	if workers != fixtureWorkers {
		t.Errorf("worker count = %d, want %d", workers, fixtureWorkers)
	}
	if others != fixtureOthers {
		t.Errorf("other count = %d, want %d", others, fixtureOthers)
	}
	if unconfirmed != fixtureUnconfirmed {
		t.Errorf("THIRD-STATE count = %d, want %d — an entry is unconfirmed exactly when NEITHER image "+
			"channel answered (exe unreadable AND argv[0] empty) while comm still names the %s process. A 0 "+
			"here means the entry was silently dropped again, the defect this test exists to catch (Rule 15)",
			unconfirmed, fixtureUnconfirmed, procname.NameGopi)
	}

	// SHAPE D: present in the THIRD STATE, with its identity (pid:starttime), not
	// merely counted — the pid is what makes the operator's arithmetic checkable.
	thirdStatePids := map[string]bool{}
	for _, id := range strings.Fields(unconfirmedPids) {
		pid, ticks, ok := splitPIDIdentity(id)
		if !ok {
			t.Errorf("third-state identity %q is not a pid:starttime pair — an unconfirmed entry must be as "+
				"checkable as a worker is", id)
			continue
		}
		thirdStatePids[pid] = true
		if want := fixtureStartTicks(t, fixture, pid); want != "" && ticks != want {
			t.Errorf("third-state pid %s is stamped %q, but its stat line's field %d is %q", pid, ticks,
				procname.StatFieldStartTime, want)
		}
	}
	if !thirdStatePids[fixtureZombiePid] {
		t.Errorf("SHAPE D (pid %s: %s-state, exe unreadable, cmdline empty) is MISSING from the third state's "+
			"pid list %q — it is then in NO bucket and the others count silently reads one lower, which is "+
			"bug 01a132a7a4ef743b exactly", fixtureZombiePid, fixtureZombieState, unconfirmedPids)
	}
	// SHAPE D must not have leaked into the other two buckets: it is neither a
	// worker (its argv is empty, so the verb test measured nothing) nor an other
	// (the channel that DEFINES "other" never answered).
	for _, id := range strings.Fields(pids) {
		if pid, _, ok := splitPIDIdentity(id); ok && pid == fixtureZombiePid {
			t.Errorf("SHAPE D (pid %s) is reported as a WORKER by %q — its argv is empty, so the worker verb "+
				"was never observed; counting it would be a fabricated measurement", fixtureZombiePid, id)
		}
	}

	// SHAPE B: the image channel ANSWERED with a foreign program, so the entry is
	// a VERDICT, not a missing measurement — it must appear in no bucket, and in
	// particular it must not be excused into the third state.
	for _, id := range strings.Fields(unconfirmedPids) {
		if pid, _, ok := splitPIDIdentity(id); ok && pid == fixtureForeignImageWorkerPid {
			t.Errorf("SHAPE B (pid %s: comm and argv[1] look like a worker, image is %q) is reported as "+
				"UNCONFIRMED — but its image channel DID answer (argv[0] = the foreign program), and a "+
				"measurable verdict is not a missing measurement (Rule 15a)", fixtureForeignImageWorkerPid,
				fixtureForeignImage)
		}
	}

	// SHAPE C: the image says gopi while comm does not — a worker, and one a comm
	// comparison cannot see at all.
	reported := map[string]bool{}
	for _, id := range strings.Fields(pids) {
		if pid, _, ok := splitPIDIdentity(id); ok {
			reported[pid] = true
		}
	}
	const shapeCPid = "1017"
	if !reported[shapeCPid] {
		t.Errorf("SHAPE C (pid %s: comm is %q, the IMAGE is %s) is not in the worker pid list %q — a worker is "+
			"identified by its image, so a foreign comm may not hide one", shapeCPid, fixtureForeignImage,
			procname.NameGopi, pids)
	}

	// THE ARITHMETIC THE OPERATOR COULD NOT DO BEFORE: every fixture entry is in
	// exactly one of the three buckets, EXCEPT the ones that are neither named nor
	// run as gopi at all. This is the identity that makes the two judges
	// reconcilable — others_old = others_new + unconfirmed.
	nameOnly := nameOnlyCount(t, fixture)
	foreignCommPidCount := 1 // SHAPE C (1017): the image is gopi, the comm is not
	want := nameOnly + foreignCommPidCount
	if want != workers+others+unconfirmed {
		t.Errorf("the three buckets do not account for every %s process: %d != %d workers + %d others + "+
			"%d unconfirmed — an entry is in NO bucket, which is the silent drop (Rule 15). The identity the "+
			"operator checks is others_old = others_new + unconfirmed; the comm count (%d) plus the "+
			"image-only worker(s) (%d) must close it", procname.NameGopi, want, workers, others, unconfirmed,
			nameOnly, foreignCommPidCount)
	}
	t.Logf("buckets on the fixture: %d workers + %d others + %d unconfirmed = %d named like %s; "+
		"third state pids:%s", workers, others, unconfirmed, nameOnly, procname.NameGopi, unconfirmedPids)
}

// TestWorkerUpScriptNamesItsValues is the Rule 13/14 gate: the script reaches
// the process name and the two verbs through the environment, its env DEFAULTS
// are the shell-side definition site, and those defaults must equal the Go
// constants in internal/procname — the single source of truth both sides read.
func TestWorkerUpScriptNamesItsValues(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel)))
	if err != nil {
		t.Fatalf("read %s: %v", workerUpScriptRel, err)
	}
	body := string(raw)

	found := map[string]bool{}
	for _, m := range envDefaultRe.FindAllStringSubmatch(body, -1) {
		shellVar, envName, value := m[1], m[2], m[3]
		if envName == procname.EnvProcRoot {
			if shellVar != procRootShellVar || value != procRootDefault {
				t.Errorf("%s default is %s=%q, want %s=%q — the procfs root the counter scans, and the seam a "+
					"test drives it through", procname.EnvProcRoot, shellVar, value, procRootShellVar, procRootDefault)
			}
			found[procRootShellVar] = true
			continue
		}
		exp, known := procNameKnobs[shellVar]
		if !known {
			if reason, ignored := procNameIgnoredKnobs[envName]; ignored {
				t.Logf("%s: %s is an ordinary setting (%s)", workerUpScriptRel, envName, reason)
				found[shellVar] = true
				continue
			}
			t.Errorf("%s assigns %s from an env knob this gate does not know — a process-identity name must be "+
				"declared in internal/procname and asserted here (Rule 14)", workerUpScriptRel, shellVar)
			continue
		}
		if envName != exp.envName {
			t.Errorf("%s reads %s where internal/procname declares %s — the env NAME has ONE definition and a "+
				"literal here is the drift this gate exists to catch (Rule 1b/13/14)", workerUpScriptRel, envName, exp.envName)
		}
		// The default may be an ALIAS of another knob in the SAME file — the
		// shell's way of saying one definition per language (Rule 13/14):
		//   IMAGE_PREFIX="${GOPI_PROC_IMAGE_PREFIX:-$PROC_NAME}"
		// An alias is only acceptable when the knob it points at is ITSELF gated
		// and carries the SAME value constant, so the alias cannot be the door a
		// diverging literal comes in through.
		if ref, isRef := shellVarRef(value); isRef {
			refExp, refKnown := procNameKnobs[ref]
			if !refKnown {
				t.Errorf("%s defaults %s to the shell variable $%s, which is not a gated knob — an alias to an "+
					"ungated value is a literal in disguise (Rule 14)", workerUpScriptRel, shellVar, ref)
			} else if refExp.value != exp.value {
				t.Errorf("%s defaults %s to $%s, whose own default is %q (%s's Go constant) — the alias must "+
					"resolve to the SAME value as %s (%q), otherwise the script has two ways to spell %q",
					workerUpScriptRel, shellVar, ref, refExp.value, refExp.what, exp.envName, exp.value, exp.value)
			} else {
				t.Logf("%s: %s aliases $%s, which is gated to the same value (%q)", workerUpScriptRel, shellVar, ref, exp.value)
			}
			found[shellVar] = true
			continue
		}
		if value != exp.value {
			t.Errorf("%s defaults %s to %q, but internal/procname says %q — %s must have one definition per "+
				"language, and the two must agree", workerUpScriptRel, shellVar, value, exp.value, exp.what)
		}
		found[shellVar] = true
	}
	for shellVar := range procNameKnobs {
		if !found[shellVar] {
			t.Errorf("%s never assigns %s from its env knob — the shell-side definition site is gone, so the "+
				"script is either typing a bare literal or reading nothing (Rule 1b/13)", workerUpScriptRel, shellVar)
		}
	}
	if !found[procRootShellVar] {
		t.Errorf("%s never assigns %s — without the procfs-root seam the counter can only be tested against "+
			"whatever this host happens to be running", workerUpScriptRel, procRootShellVar)
	}

	// The procfs FILE NAMES the scan reads are the same contract, one level down:
	// internal/procname declares ExeName / StatName and the script's own
	// constants must be those exact spellings, so a rename on the Go side cannot
	// leave the shell reading a file that no longer exists (it would read nothing
	// and silently report zero — the fabricated absence again).
	for shellVar, want := range map[string]string{
		"EXE_NAME":  procname.ExeName,
		"STAT_NAME": procname.StatName,
	} {
		re := regexp.MustCompile(`(?m)^readonly ` + shellVar + `=([A-Za-z0-9_.-]+)`)
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Errorf("%s no longer declares %s — the procfs file name the scan reads has no shell-side "+
				"definition, so a bare literal (or a stale spelling) is all that is left (Rule 1b/13/14)",
				workerUpScriptRel, shellVar)
			continue
		}
		if m[1] != want {
			t.Errorf("%s declares %s=%q, but internal/procname says %q — the two sides read the procfs "+
				"record with ONE agreed spelling", workerUpScriptRel, shellVar, m[1], want)
		}
	}
}

// TestWorkerUpCountOutputIsFullyRead pins the counter's output CONTRACT against
// the silent drop happening one level up: count_gopi_processes prints FIVE lines
// (workers, others, unconfirmed, worker pids, unconfirmed pids), and every caller
// inside the script must read all five. A caller that slices only the first lines
// re-creates exactly the defect this branch fixes — the third state would be
// computed and then thrown away before any operator could read it.
//
// The lines are positional (a shell function cannot return an array), so the
// check is on the SLICER CALLS: the script may reach line N only through a
// `sed -n 'Np'` on the counter's own output, and the set of such calls must cover
// 1..5. Anything else would have to hard-code the shape a second time.
func TestWorkerUpCountOutputIsFullyRead(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel)))
	if err != nil {
		t.Fatalf("read %s: %v", workerUpScriptRel, err)
	}
	body := stripShellComment(string(raw))
	re := regexp.MustCompile(`sed -n '([0-9]+)p'`)
	read := map[int]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[1]) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
		if n > 0 && n <= countOutputLines {
			read[n] = true
		}
	}
	for line := 1; line <= countOutputLines; line++ {
		if !read[line] {
			t.Errorf("%s never reads line %d of count_gopi_processes' output — the counter prints "+
				"%d lines and the caller that slices only the first %d drops a state the counter "+
				"MEASURED, which is the silent drop this branch exists to end (Rule 15)", workerUpScriptRel,
				line, countOutputLines, line-1)
		}
	}
}

// countOutputLines is how many lines count_gopi_processes prints, in order:
// 1 workers, 2 others, 3 unconfirmed, 4 worker pids, 5 unconfirmed pids.
const countOutputLines = 5

// TestWorkerUpScriptHasNoPgrepOrPkill pins the counting METHOD: no name/pattern
// matcher, no external process tool (and an executable launcher).
func TestWorkerUpScriptHasNoPgrepOrPkill(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read %s: %v", workerUpScriptRel, err)
	}
	body := string(raw)
	for _, banned := range []string{"pgrep", "pkill", "killall"} {
		if strings.Contains(stripShellComment(body), banned) {
			t.Errorf("%s runs %q — the count must read procfs directly (comm + argv SHAPE): a name/pattern "+
				"matcher IS the 2026-09-12 miscount, and pkill has killed live work on this host (stop by "+
				"explicit PID only)", workerUpScriptRel, banned)
		}
	}
	fi, err := os.Stat(script)
	if err != nil {
		t.Fatalf("stat %s: %v", workerUpScriptRel, err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable (mode %v) — a launcher that cannot be run is not a launcher",
			workerUpScriptRel, fi.Mode().Perm())
	}
}

// runWorkerUpCount drives the script's counting seam over a procfs fixture and
// returns the numbers plus both pid lists. It exports the names FROM
// internal/procname, so the test never types what those constants define.
//
// The THIRD STATE is read here too, and its ABSENCE is a failure: a reader that
// takes only the two counts cannot tell "nothing was unmeasurable" from "the
// script stopped reporting it" (Rule 15a).
func runWorkerUpCount(t *testing.T, script, procRoot string) (workers, others int, pids string) {
	t.Helper()
	workers, others, _, pids, _ = runWorkerUpBreakdown(t, script, procRoot)
	return workers, others, pids
}

// runWorkerUpBreakdown is the full reading of the counting seam: the two counts,
// the third state's count and BOTH pid lists.
func runWorkerUpBreakdown(t *testing.T, script, procRoot string) (workers, others, unconfirmed int, pids, unconfirmedPids string) {
	t.Helper()
	cmd := exec.Command("bash", script, workerUpCountFlag)
	cmd.Env = append(os.Environ(),
		procname.EnvName+"="+procname.NameGopi,
		procname.EnvVerbWorker+"="+procname.VerbWorker,
		procname.EnvVerbTask+"="+procname.VerbTask,
		procname.EnvProcRoot+"="+procRoot,
		procname.EnvStatField+"="+strconv.Itoa(procname.StatFieldStartTime),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed over the fixture %s: %v\n%s", workerUpScriptRel, workerUpCountFlag, procRoot, err, out)
	}
	text := string(out)
	workers = parseCountLine(t, text, "live "+procname.NameGopi+" workers:")
	others = parseCountLine(t, text, "other "+procname.NameGopi+" processes:")
	unconfirmed = parseCountLine(t, text, "unconfirmed "+procname.NameGopi+" processes:")
	pids, unconfirmedPids = parsePids(t, text), parseUnconfirmedPids(t, text)
	return workers, others, unconfirmed, pids, unconfirmedPids
}

// parseCountLine pulls the leading integer out of one operator-facing line.
func parseCountLine(t *testing.T, text, prefix string) int {
	t.Helper()
	idx := strings.Index(text, prefix)
	if idx < 0 {
		t.Fatalf("the count output carries no %q line — the operator reads this number to decide whether to "+
			"launch more workers:\n%s", prefix, text)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text[idx:], prefix))
	rest = strings.TrimSpace(strings.SplitN(rest, " ", 2)[0])
	n := 0
	if _, err := fmt.Sscanf(rest, "%d", &n); err != nil {
		t.Fatalf("cannot read the count from %q (line %q): %v", rest, prefix, err)
	}
	return n
}

// parsePids pulls the pid list out of the "(pids: …)" parenthetical.
func parsePids(t *testing.T, text string) string {
	t.Helper()
	const marker = "(pids:"
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatalf("the worker line carries no pid list — the breakdown is what makes a wrong number "+
			"self-evident:\n%s", text)
	}
	rest := text[start+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("unterminated pid list in:\n%s", text)
	}
	return strings.TrimSpace(rest[:end])
}

// pidTickSep separates a pid from its starttime in the reported identity words.
const pidTickSep = ":"

// parseUnconfirmedPids pulls the pid list out of the THIRD STATE's line. It
// reads the second "(pids:" occurrence — the worker line's list is the first —
// and it fails loudly when there is only one, because a missing third-state line
// is exactly the silent drop this whole file guards against.
func parseUnconfirmedPids(t *testing.T, text string) string {
	t.Helper()
	const marker = "(pids:"
	first := strings.Index(text, marker)
	if first < 0 {
		t.Fatalf("no pid list anywhere in:\n%s", text)
	}
	second := strings.Index(text[first+len(marker):], marker)
	if second < 0 {
		t.Fatalf("the output carries no third-state line (only ONE pid list) — an entry the image channel "+
			"could not measure must be REPORTED, never dropped (Rule 15):\n%s", text)
	}
	rest := text[first+len(marker)+second+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("unterminated third-state pid list in:\n%s", text)
	}
	return strings.TrimSpace(rest[:end])
}

// splitPIDIdentity splits a reported worker identity "pid:starttime" into its two
// halves. ok=false means the word is not an identity at all.
func splitPIDIdentity(word string) (pid, ticks string, ok bool) {
	i := strings.Index(word, pidTickSep)
	if i <= 0 || i == len(word)-1 {
		return "", "", false
	}
	return word[:i], word[i+1:], true
}

// fixtureStartTicks reads the fixture's OWN stat line for pid and returns field
// 22 — the ground truth the counter's reported starttime is compared against.
// It parses from the LAST ')' for the same reason the production readers do, and
// it returns "" when the entry has no stat (a process that died mid-walk).
func fixtureStartTicks(t *testing.T, procRoot, pid string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(procRoot, pid, procname.StatName))
	if err != nil {
		return ""
	}
	ticks, ok := procname.StatField(string(raw), procname.StatFieldStartTime)
	if !ok {
		return ""
	}
	return ticks
}

// nameOnlyCount evaluates the OLD predicate — the one `pgrep -x <name> | wc -l`
// implemented — against the same fixture: every process whose NAME field
// identifies the gopi program, whatever its argv. It reads the fixture directly
// rather than shelling out, so the comparison is deterministic and needs no
// external tool.
//
// THE CRITERION IS DELIBERATELY THE STRONGEST FORM A NAME-ONLY COUNT CAN HAVE,
// derived from what `pgrep -x` really does rather than from a convenient
// approximation: pgrep matches the truncation of the basename the KERNEL holds,
// so a pattern on that field matches when it is a PREFIX of the name we ask about
// (pgrep -x gopi matches a process whose comm is "gop"). Using that criterion
// — rather than an equality — is what keeps this comparison honest about the old
// predicate's power, and it is why the differently-named gopi build (1014) lands
// on BOTH sides: as a worker the counter must count, and as a process the old
// name-only count cannot see at all.
func nameOnlyCount(t *testing.T, procRoot string) int {
	t.Helper()
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		t.Fatalf("read fixture %s: %v", procRoot, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(procRoot, e.Name(), procname.CommName))
		if rerr != nil {
			continue
		}
		name := strings.TrimRight(string(raw), "\n")
		if name == "" || len(name) >= kernelCommTruncationBytes {
			continue // a name the kernel would have truncated is not what the count read
		}
		if strings.HasPrefix(procname.NameGopi, name) {
			n++
		}
	}
	return n
}

// procFixtureEntry is one synthetic /proc/<pid>/ directory: the comm, the argv
// vector, and the stat LINE (the incarnation carrier). Every entry's comment
// states the shape it pins (Rule 5: the decoys ARE the regression guard).
type procFixtureEntry struct {
	pid  string
	comm string
	// image is the base name of the executable the process RUNS: the file
	// /proc/<pid>/exe links to. It is the channel the worker-shape predicate
	// reads the PROGRAM from (comm is only that file's basename, truncated), so
	// the fixture states it per entry — a fixture that omitted it would be
	// exercising the argv[0] fallback for every case at once. An EMPTY image
	// string plants NO exe link: the kernel channel is unreadable for that entry
	// (a restricted entry, a deleted image, or — the measured shape — a Z-state
	// process, whose exe readlink fails; the "" sentinel follows the same
	// convention as an empty stat below).
	image string
	argv  []string
	// stat is the FULL /proc/<pid>/stat line, or "" for a process whose stat is
	// unreadable (a process that died mid-walk — a shape the kernel really
	// produces, and the one that decides whether a missing incarnation is
	// reported as unknown or silently folded into a wrong answer).
	stat    string
	comment string
}

// procStatLine renders one /proc/<pid>/stat line: "<pid> (<comm>) <state> …".
//
// THE COMM IS THE POINT. comm is the process's own name, truncated by the kernel
// to 15 bytes, and it may itself contain SPACES and PARENTHESES — the counter and
// internal/procname both read starttime by counting fields from the LAST ')'
// exactly because of this, and a whitespace split of the line silently shifts
// every later field. Every fixture below therefore goes through this helper, so
// the parenthesised-name shape is exercised by the whole fixture rather than by
// one hand-written case. The padding fields ARE part of the format: starttime is
// field 22, and a line with fewer fields is a line no reader may trust.
func procStatLine(pid, comm, state, startTicks string) string {
	padding := make([]string, 0, procStatMinPadding)
	for i := 0; i < procStatMinPadding; i++ {
		padding = append(padding, strconv.Itoa(i))
	}
	return fmt.Sprintf("%s (%s) %s %s %s", pid, comm, state, strings.Join(padding, " "), startTicks)
}

// writeProcFixture builds the synthetic procfs tree: one directory per fake pid,
// each with a comm, a NUL-separated cmdline and a stat line — the three files the
// identity triple reads. Every entry's comment states the shape it pins
// (Rule 5: the decoys ARE the regression guard).
func writeProcFixture(t *testing.T, root string) {
	t.Helper()
	entries := []procFixtureEntry{
		// --- real workers: the gopi PROGRAM (image) AND argv[1] == the verb ---
		{"1001", procname.NameGopi, procname.NameGopi, []string{"/home/user/work/GoPi/bin/" + procname.NameGopi, procname.VerbWorker},
			procStatLine("1001", procname.NameGopi, "S", "111001"),
			"a plain worker (absolute binary path in argv[0])"},
		{"1002", procname.NameGopi + "\n", procname.NameGopi, []string{procname.NameGopi, procname.VerbWorker},
			procStatLine("1002", procname.NameGopi, "S", "222002"),
			"a worker whose comm carries the kernel's trailing newline"},
		{"1003", procname.NameGopi, procname.NameGopi, []string{procname.NameGopi, procname.VerbWorker, "--target=host"},
			procStatLine("1003", procname.NameGopi, "S", "333003"),
			"a worker with a trailing argument"},
		{"1014", procnameFixingImage, procnameFixingImage, []string{"/tmp/build/bin/" + procnameFixingImage, procname.VerbWorker},
			procStatLine("1014", procnameFixingImage, "S", "140001"),
			"THE BUG (01a131fcb8337c1b): a worker of a gopi binary built under ANOTHER file name — its comm " +
				"is not " + procname.NameGopi + ", so a comm comparison reports zero workers while this one runs"},
		// --- decoys named like a gopi process, NOT workers ---
		{"1004", procname.NameGopi, procname.NameGopi, []string{procname.NameGopi, procname.VerbTask, "run", "01a09254"},
			procStatLine("1004", procname.NameGopi, "S", "444004"),
			"a one-shot task child: it re-execs the same binary"},
		{"1005", procname.NameGopi, procname.NameGopi, []string{procname.NameGopi, procname.VerbTask},
			procStatLine("1005", procname.NameGopi, "S", "555005"),
			"a `" + procname.VerbTask + "` group invocation"},
		{"1006", procname.NameGopi, procname.NameGopi, []string{procname.NameGopi, "serve"},
			procStatLine("1006", procname.NameGopi, "S", "666006"),
			"the task-serve daemon"},
		{"1010", procname.NameGopi, procname.NameGopi, []string{procname.NameGopi, procname.VerbWorker + "x"},
			procStatLine("1010", procname.NameGopi, "S", "100001"),
			"argv[1] merely CONTAINS the verb (an exact compare must reject it)"},
		{"1011", procname.NameGopi, procname.NameGopi, []string{procname.NameGopi},
			procStatLine("1011", procname.NameGopi, "S", "110001"),
			"argv[1] absent entirely"},
		{"1012", procname.NameGopi, procname.NameGopi, []string{},
			procStatLine("1012", procname.NameGopi, "S", "120001"),
			"an empty cmdline (a shape the kernel really produces)"},
		// --- decoys NOT named like a gopi process ---
		{"1007", "bash", "bash", []string{"bash", "-c", "echo " + procname.NameGopi + " " + procname.VerbWorker + " | wc -l"},
			procStatLine("1007", "bash", "S", "777007"),
			"THE SELF-MATCH DECOY: a shell whose COMMAND LINE carries the pattern"},
		{"1008", procname.NameGopi + "-runner", fixtureForeignImage,
			[]string{procname.NameGopi + "-runner", procname.VerbWorker},
			procStatLine("1008", procname.NameGopi+"-runner", "S", "888008"),
			"THE REVERSE OF THE BUG: a comm that is a gopi-prefixed name while the IMAGE is " + fixtureForeignImage +
				" — the old comm-only count accepted it, and the image test must refuse it however convincing the name is"},
		{"1009", procname.NameGopi + " " + procname.VerbWorker, procname.NameGopi, []string{procname.NameGopi, procname.VerbWorker},
			// THE PARENTHESISED-SPACE NAME: comm holds a space, so the stat
			// line's field boundary is exactly the one a whitespace split gets
			// wrong. The counter must still read field 22 from it.
			procStatLine("1009", procname.NameGopi+" "+procname.VerbWorker, "S", "999009"),
			"a worker whose comm IS the whole command (spaces) — the stat-field trap, and a worker by its image"},
		// --- SHAPE B: the image channel ANSWERED, and the answer is "not gopi" ---
		{pid: "1015", comm: procname.NameGopi,
			argv: []string{"/opt/" + fixtureForeignImage + "/sbin/" + fixtureForeignImage, procname.VerbWorker},
			stat: procStatLine("1015", procname.NameGopi, "S", "150001"),
			comment: "SHAPE B: comm AND argv[1] both look like a worker, the exe link is UNREADABLE (empty image), " +
				"and argv[0] names " + fixtureForeignImage + " — one image channel ANSWERED, and its answer is a " +
				"measurable verdict (\"not this program\"). It is therefore in NO bucket and must never be " +
				"reported as unconfirmed (Rule 15a: a verdict is not a missing measurement)"},
		// --- SHAPE C: the image says gopi, the comm does not — still a worker ---
		{pid: "1017", comm: fixtureForeignImage, image: procname.NameGopi,
			argv: []string{"/usr/local/bin/" + procname.NameGopi, procname.VerbWorker},
			stat: procStatLine("1017", fixtureForeignImage, "S", "170001"),
			comment: "SHAPE C: comm is " + fixtureForeignImage + " while the IMAGE is " + procname.NameGopi +
				" — a worker by the image criterion, and one a comm comparison cannot see at all"},
		// --- SHAPE D, the measured defect: a Z-state process named gopi whose exe
		// link is unreadable and whose cmdline is EMPTY. NEITHER image channel
		// answers, so before the fix the entry vanished from BOTH counts and the
		// operator's `other gopi processes: N` simply read one lower.
		{pid: fixtureZombiePid, comm: procname.NameGopi,
			stat: procStatLine(fixtureZombiePid, procname.NameGopi, fixtureZombieState, "160001"),
			comment: "SHAPE D (bug 01a132a7a4ef743b): " + fixtureZombieState + "-state, exe unreadable, cmdline " +
				"EMPTY — the THIRD STATE"},
	}
	for _, e := range entries {
		dir := filepath.Join(root, e.pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create fixture entry %s (%s): %v", e.pid, e.comment, err)
		}
		if err := os.WriteFile(filepath.Join(dir, procname.CommName), []byte(e.comm), 0o644); err != nil {
			t.Fatalf("write comm for %s (%s): %v", e.pid, e.comment, err)
		}
		cmdline := ""
		if len(e.argv) > 0 {
			cmdline = strings.Join(e.argv, "\x00") + "\x00"
		}
		if err := os.WriteFile(filepath.Join(dir, procname.CmdlineName), []byte(cmdline), 0o644); err != nil {
			t.Fatalf("write cmdline for %s (%s): %v", e.pid, e.comment, err)
		}
		// The EXECUTABLE LINK: /proc/<pid>/exe is what the counter reads the
		// PROGRAM from (comm is only the basename of that file, and the kernel
		// truncates it). The fixture writes it per entry so the count is decided
		// by the image the entry declares.
		if e.image != "" {
			link := filepath.Join(dir, procname.ExeName)
			if err := os.Symlink(procFixtureImageDir+"/"+e.image, link); err != nil {
				t.Fatalf("plant the %s link for %s (%s): %v", procname.ExeName, e.pid, e.comment, err)
			}
		}
		if e.stat != "" {
			if err := os.WriteFile(filepath.Join(dir, procname.StatName), []byte(e.stat+"\n"), 0o644); err != nil {
				t.Fatalf("write stat for %s (%s): %v", e.pid, e.comment, err)
			}
		}
	}
	// A non-pid directory must be ignored by the scan (the pid glob), a
	// pid-shaped FILE must not be read as a process, and an entry with no
	// cmdline at all (a process that died mid-walk) must be skipped rather than
	// counted.
	if err := os.MkdirAll(filepath.Join(root, "notapid"), 0o755); err != nil {
		t.Fatalf("create the non-pid directory decoy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "9999"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("create the pid-shaped file decoy: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "1013"), 0o755); err != nil {
		t.Fatalf("create the no-cmdline decoy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "1013", procname.CommName), []byte(procname.NameGopi), 0o644); err != nil {
		t.Fatalf("write the no-cmdline decoy's comm: %v", err)
	}
	if err := os.Symlink(procFixtureImageDir+"/"+procname.NameGopi, filepath.Join(root, "1013", procname.ExeName)); err != nil {
		t.Fatalf("plant the %s link for the no-cmdline decoy: %v", procname.ExeName, err)
	}
	if err := os.WriteFile(filepath.Join(root, "1013", procname.StatName),
		[]byte(procStatLine("1013", procname.NameGopi, "S", "130001")+"\n"), 0o644); err != nil {
		t.Fatalf("write the no-cmdline decoy's stat: %v", err)
	}
}

// repoRootForProcnameGate walks up from the test binary's working directory to
// the module root (the directory holding go.mod).
func repoRootForProcnameGate(t *testing.T) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate the repo root: %v", err)
	}
	return root
}

// stripShellComment drops the parts of a shell script that are COMMENTS — whole
// lines whose first non-blank character is '#' — keeping every other line
// verbatim. It is what lets the bans above be checked against the code without
// reddening on the script's own documentation, which must be free to explain
// the defect it replaced (it names the old command as prose).
func stripShellComment(body string) string {
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			b.WriteString("\n")
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
