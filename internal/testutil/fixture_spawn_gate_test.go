package testutil

// The SOURCE gate for the 2026-10-27 leak (task 01a179d7): every long-lived
// `sleep` fixture in this tree must be spawned in a way that survives its test
// binary being KILLED, not merely one that survives a normal test end.
//
// WHY A SOURCE GATE AND NOT ONLY THE RUNTIME ARM. The runtime arm
// (mockproc_childdeath_test.go) proves the SANCTIONED path is safe — but a
// fixture that hand-rolls `exec.Command("sleep", …)` never goes through it, and
// dozens of such spawns exist. Measured in the same session:
// `TestProcsCmdlineMatch_SubtractsTheBaseline` (bash_daemon_test.go) leaked a
// `sleep 300` standing in the checkout when its test binary was SIGKILLed, and
// the two specimens the briefing started from came from `startLiveRunner` the
// same way. Per-site conversion fixes TODAY's sites; only a gate keeps the next
// one from reopening it, because the failure is invisible — the child dies
// normally, and only a killed binary exposes it.
//
// THE CRITERION is the kernel property, never a spelling:
//
//   - the spawn goes through the reaping helper (StartMockProcess /
//     StartMockProcessCmd / StartMockProcessDir), which arms Pdeathsig and a
//     neutral cwd for the caller; or
//   - the fixture builds its own *exec.Cmd and arms Pdeathsig on it (the helper
//     is unusable when the SUBJECT of the test is the group/session shape the
//     helper would change).
//
// A permission is stated in the file — `//fixture-spawn-ok: <reason>` on the
// spawn or a few lines above it — so a reader meets the exception and its reason
// together, and a permit cannot outlive its justification unnoticed.
//
// SCOPE: `sleep`-shaped fixtures only, i.e. a child that HOLDS (a daemon
// stand-in). A bounded poll interval (`sleep 0.02`) cannot outlive anything and
// is out of scope — the duration decides, read from the call, never guessed.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// sleepFixtureMinHoldSeconds is the floor that makes a `sleep` a FIXTURE
	// (something meant to be alive while the test runs) rather than a poll
	// interval, which ends by itself and cannot outlive the test.
	sleepFixtureMinHoldSeconds = 5
	// sleepFixtureEscape is the stated-permission marker.
	sleepFixtureEscape = "fixture-spawn-ok:"
	// sleepFixtureWindowLines is how far above a spawn a permit may sit.
	sleepFixtureWindowLines = 6
	// sleepFixturePdeathsigReach is how far below the spawn inside its own
	// function a Pdeathsig assignment may sit.
	sleepFixturePdeathsigReach = 40
	// sleepFixtureFloor is the number of spawn sites this gate must JUDGE for
	// its green to mean anything: a walk that judged nothing is a vacuum, not
	// a clean tree (Rule 38).
	sleepFixtureFloor = 15
	// sleepFixtureBaselineRel is the frozen census of sites that predate this
	// gate. The freeze has ONE direction — DOWN: a site may be removed from
	// this file, never added, and a row whose site is now covered is an ERROR
	// (a permit that outlives its justification is a silent skip wearing a
	// passing green). Rule 39's ratchet, applied to this gate.
	sleepFixtureBaselineRel = "internal/testutil/testdata/sleep-fixture-spawn-baseline.tsv"
	// sleepFixtureBaselineDumpEnv regenerates that table FROM the scan below —
	// the generator is this gate, never a hand edit (Rule 38).
	sleepFixtureBaselineDumpEnv = "GOPI_SLEEP_FIXTURE_BASELINE_DUMP"
	// sleepFixtureFrozenReason is the one reason a frozen row carries: it
	// predates the gate. A row with a DIFFERENT reason is a claim its author
	// made and must be reviewed, not inherited.
	sleepFixtureFrozenReason = "pre-gate site: hand-rolled spawn, unfixed (predates the gate; convert to StartMockProcessCmd or arm Pdeathsig)"
	// sleepFixtureShellNames are the shells a sleep fixture wraps its hold in.
	// ANY of them is the shell arm's shape — the recognizer must not depend on
	// which one the author reached for.
	sleepFixtureShellNames = `sh|bash|dash|zsh|ksh`
	// sleepFixtureShellPaths is the OPTIONAL path prefix in front of a shell
	// name: none at all (PATH lookup), a bare directory run, or an absolute
	// one — `"bash"`, `"bin/bash"`, `"/bin/bash"`, `"/usr/local/bin/bash"`.
	// It is deliberately broader than the two absolute spellings this gate used
	// to accept: the measured false negative (task 01a1888d) was
	// `exec.Command("bash", "-c", "sleep 60")`, which the old regex could not
	// see while its `sleep 60` stood in the checkout.
	sleepFixtureShellPaths = `(?:\/?(?:[A-Za-z0-9_.-]+\/)*)?`
	// sleepFixtureDirectPrograms are the non-shell programs that spell a
	// sleep-fixture hold: the kernel binary itself, this package's constants,
	// and a PATH-resolved `sleep` (`exec.Command(exec.LookPath("sleep"), …)`).
	sleepFixtureDirectPrograms = `sleep|MockDaemonBinary|testDaemonSleepArg|sleepFixtureName`
	// sleepFixtureLookPathPrefix is the `exec.Command(exec.LookPath("sleep")`
	// spelling: the same fixture, resolved from PATH — so the recognizer reads
	// through the wrapper rather than past it.
	sleepFixtureLookPathPrefix = `(?:exec\.LookPath\(\s*)?`
	// sleepFixtureArgReach is how far BELOW a spawn's `exec.Command(` line the
	// recognizer reads for the call's first argument — the multi-line spelling
	// (`exec.Command(` then `"/bin/sh",` on the next line), which splits the
	// program across lines and would otherwise be the same blind spot as an
	// unlisted name.
	sleepFixtureArgReach = 2
	// sleepFixtureHoldWord recognizes the shell arm's command string carrying a
	// hold: `"sleep 60"`, `"exec sleep 300"`, `"sleep "+n+" & wait"`. The word is
	// the shape; the NUMBER decides whether it is a fixture (see
	// sleepFixtureShellHoldDuration).
	sleepFixtureHoldWord = `\bsleep\b`
	// sleepFixtureHoldSecondsRe reads the hold's SECONDS out of a shell command
	// string. No match means the duration is unreadable at the source (a
	// variable, a concatenation) — and an unreadable hold is judged a fixture,
	// exactly as the direct arm does.
	sleepFixtureHoldSecondsRe = `\bsleep\s+"?\s*([0-9]+)`
	// sleepFixtureExecHoldRe matches the ONE shell shape whose grandchild CANNOT
	// outlive the test: `-c "exec sleep 300"`. `exec` REPLACES the shell with the
	// hold, so there is no grandchild at all — the process the test holds the
	// handle to IS the sleep, and a Pdeathsig armed on it reaches it. Measured on
	// this host: `bash -c 'sleep 60'` forks (comm stays `bash`, a child exists)
	// while `bash -c 'exec sleep 60'` execs (comm becomes `sleep`) — so this is a
	// real distinction, not a stylistic one.
	sleepFixtureExecHoldRe = `\bexec\s+sleep\b`
	// sleepFixturePermittedCover is the `covered` value a stated permission
	// produces, so the census can count permits without re-reading the source.
	sleepFixturePermittedCover = "permitted by "
	// sleepFixtureHelperCover is the `covered` value the reaping helper produces.
	sleepFixtureHelperCover = "reaping helper"
	// sleepFixtureHelperWindowLines is how far ABOVE a spawn the recognizer looks
	// for the helper wrapping it, as in
	// `StartMockProcessCmd(t, func() *exec.Cmd { … })`: the call that owns the
	// spawn is the statement the spawn lives in, not an arbitrary distance away.
	sleepFixtureHelperWindowLines = 8
)

// sleepFixtureCallStart matches the FIRST line of a spawn expression: the
// `exec.Command(`/`exec.CommandContext(` call itself, whatever follows it on
// that line (Rule 1a — the spelling lives here, once).
var sleepFixtureCallStart = regexp.MustCompile(`exec\.(Command|CommandContext)\(`)

// sleepFixtureSpawnAssign names the variable a spawn is assigned to
// (`cmd := exec.Command(…`), so the helper cover below can require the helper to
// be applied to THAT command rather than to any command in the neighbourhood.
var sleepFixtureSpawnAssign = regexp.MustCompile(`^\s*(\w+)\s*:?=\s*exec\.(Command|CommandContext)\(`)

// sleepFixtureHelperCall matches the sanctioned reaping helper — the spawn
// criterion's FIRST branch ("the spawn goes through the reaping helper, which
// arms Pdeathsig and a neutral cwd for the caller"). A cover that accepted only
// a literal `Pdeathsig` arm would call every helper caller a violation, which is
// the same spelling-keyed mistake one level over: the cover is the PROPERTY
// (a reaped group), not the identifier it happens to be written with.
var sleepFixtureHelperCall = regexp.MustCompile(`StartMockProcess(?:Cmd|Dir)?\(`)

// sleepFixtureSpawnLine matches the program-and-hold shape of a sleep fixture spawn,// whatever spelling the hold takes (literal, constant, strconv.Itoa, …) AND whatever
// path prefix the program carries: the binary is `sleep` on Linux, the helper's own
// constants are the other spellings, and `/bin/sleep` / `/usr/bin/sleep` /
// `exec.LookPath("sleep")` are the SAME fixture as a bare `"sleep"`.
var sleepFixtureSpawnLine = regexp.MustCompile(
	`exec\.(Command|CommandContext)\(\s*"` + sleepFixtureShellPaths + `(?:` + sleepFixtureDirectPrograms + `)`)

// sleepFixturePollLiteral matches a literal hold below the fixture floor.
var sleepFixturePollLiteral = regexp.MustCompile(`"([0-9]*\.?[0-9]+)"`)

// sleepFixtureShellHoldConst matches a package constant that IS a long-lived
// sleep command line (`holderHoldSeconds`, `"sleep "+x`), the shape a fixture
// uses when it needs a SHELL around the hold.
var sleepFixtureShellHoldConst = regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"sleep\s+`)

// sleepFixtureShellSpawn matches a spawn of a shell, the second producer of
// leaked sleeps: `/bin/sh -c "sleep 300"` holds a GRANDCHILD, and a Pdeathsig
// armed on the shell is NOT propagated to it — so the hold outlives the test
// binary even though the arm looks right (measured, task 01a179d7: two per PASS
// in internal/tools/task_finish_cleanup_holder_naming_test.go).
//
// THE PATH PREFIX IS OPTIONAL. This arm used to accept only `"/bin/sh"` and
// `"/bin/bash"`, i.e. it keyed on the first argument's SPELLING rather than on
// the shape, and that is a measured false negative (task 01a1888d): a bare
// `exec.Command("bash", "-c", "sleep 60")` spawned a `sleep` that was found
// standing inside the checkout with PPid=systemd (its test binary gone), while
// this gate read GREEN on the very same tree — `offending 0`. A spawn hides
// from a spelling; it cannot hide from the program's NAME.
var sleepFixtureShellSpawn = regexp.MustCompile(
	`exec\.(Command|CommandContext)\(\s*"` + sleepFixtureShellPaths + `(?:` + sleepFixtureShellNames + `)"`)

// sleepFixtureShellHoldLine matches a shell command string that carries a hold,
// the second half of the shell arm: the shell name says a shell was spawned, the
// command string says it is holding a `sleep` (which is what makes it a FIXTURE,
// not a `sh -c "exit 0"`). Kept separate from the program name because the two
// can live on different lines and in different shapes
// (`"sleep "+strconv.Itoa(n)+" & wait"`).
var sleepFixtureShellHoldLine = regexp.MustCompile(sleepFixtureHoldWord)

// sleepFixtureHoldSeconds matches the seconds a shell command string holds for;
// no match means the duration is unreadable at the source.
var sleepFixtureHoldSeconds = regexp.MustCompile(sleepFixtureHoldSecondsRe)

// sleepFixtureExecHold matches the shell shape that REPLACES itself with the hold.
var sleepFixtureExecHold = regexp.MustCompile(sleepFixtureExecHoldRe)

// sleepFixtureSite is one long-lived sleep spawn the gate found.
type sleepFixtureSite struct {
	file    string
	line    int
	covered string // the covering mechanism, "" when nothing covers it
}

// TestLongLivedFixtureSpawnsCannotOutliveTheTestBinary is the gate.
func TestLongLivedFixtureSpawnsCannotOutliveTheTestBinary(t *testing.T) {
	root := sleepFixtureRepoRoot(t)
	sites, err := sleepFixtureSites(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if len(sites) < sleepFixtureFloor {
		t.Fatalf("the scan judged only %d sleep-fixture spawn site(s) (floor %d) — a walk that sees almost "+
			"nothing cannot pass, or its green would be indistinguishable from a broken scanner",
			len(sites), sleepFixtureFloor)
	}
	// The generator is THIS test, so the table can never describe a scanner the
	// gate does not run (Rule 38): with the dump variable set it rewrites the
	// frozen census from the sites it just judged, and passes only when it
	// wrote something.
	if os.Getenv(sleepFixtureBaselineDumpEnv) == "1" {
		if err := sleepFixtureWriteBaseline(root, sites); err != nil {
			t.Fatalf("write the baseline: %v", err)
		}
		t.Logf("baseline regenerated from %d judged site(s)", len(sites))
		return
	}
	baseline, err := sleepFixtureBaseline(root)
	if err != nil {
		t.Fatalf("read the baseline %s: %v", sleepFixtureBaselineRel, err)
	}
	verdict := sleepFixtureJudge(root, sites, baseline)
	for _, line := range verdict.frozenSites {
		t.Log(line)
	}
	for _, msg := range verdict.red {
		t.Error(msg)
	}
	t.Logf("sleep-fixture spawn census: walked %d site(s); helper-or-Pdeathsig %d, permitted %d, frozen %d, offending %d",
		verdict.walked, verdict.armed, verdict.permitted, verdict.frozen, verdict.offenders)
}

// sleepFixtureVerdict is ONE judging pass over a set of sites: the census counts a
// reader compares between runs, the frozen lines it recognised, and the RED
// messages — one per site that is neither covered nor frozen, plus one per frozen
// row that no longer matches a site.
//
// It is a separate function from the gate for ONE reason, and it is the reason the
// planted proof exists: the red proof must run the GATE'S OWN judging over a
// planted tree, not a copy of it. A proof that re-implemented the walk would stay
// green while the real judge broke.
type sleepFixtureVerdict struct {
	walked, armed, permitted, frozen, offenders int
	frozenSites                                 []string
	red                                         []string
}

// sleepFixtureJudge is the gate's judgement, over any (root, sites, baseline)
// triple — the real tree's, or a planted one's.
func sleepFixtureJudge(root string, sites []sleepFixtureSite, baseline map[string]string) sleepFixtureVerdict {
	var v sleepFixtureVerdict
	v.walked = len(sites)
	seen := map[string]bool{}
	for _, s := range sites {
		key := sleepFixtureBaselineKey(root, s)
		seen[key] = true
		switch {
		case s.covered == "":
			if reason, ok := baseline[key]; ok {
				v.frozen++
				v.frozenSites = append(v.frozenSites, fmt.Sprintf("frozen (pre-existing): %s:%d — %s", s.file, s.line, reason))
				continue
			}
			v.offenders++
			v.red = append(v.red, fmt.Sprintf("%s:%d spawns a long-lived `sleep` fixture with neither the reaping "+
				"helper nor Pdeathsig armed — when `go test` hits its -timeout (an os.Exit) or the binary is "+
				"SIGKILLed, t.Cleanup never runs and the child survives, cwd and all; a survivor standing in a "+
				"checkout makes that ground read IN USE forever. Spawn it through testutil.StartMockProcessCmd, "+
				"arm syscall.SIGKILL as Pdeathsig on it, or state why not with `//%s <reason>`",
				s.file, s.line, sleepFixtureEscape))
		case strings.HasPrefix(s.covered, sleepFixturePermittedCover):
			v.permitted++
		default:
			v.armed++
		}
	}
	for key, reason := range baseline {
		if !seen[key] {
			v.red = append(v.red, fmt.Sprintf("baseline row %q (%s) no longer matches any offending site — the site "+
				"was fixed or moved; DELETE the row in the same change (the freeze only ratchets down, Rule 39)", key, reason))
		}
	}
	return v
}

// sleepFixtureBaselineKey identifies a site independently of its line number,
// so an unrelated edit above it does not invalidate the freeze.
func sleepFixtureBaselineKey(root string, s sleepFixtureSite) string {
	rel, err := filepath.Rel(root, s.file)
	if err != nil {
		rel = s.file
	}
	return rel + "|" + strconv.Itoa(s.line)
}

// sleepFixtureWriteBaseline rewrites the frozen census from a judged pass.
func sleepFixtureWriteBaseline(root string, sites []sleepFixtureSite) error {
	var rows []string
	rows = append(rows, "# sleep-fixture spawn census — sites that predate this gate.",
		"# Generated by TestLongLivedFixtureSpawnsCannotOutliveTheTestBinary with",
		"# "+sleepFixtureBaselineDumpEnv+"=1. NEVER hand-edit: the freeze ratchets DOWN only —",
		"# fix a site and DELETE its row in the same change (a row whose site is now",
		"# covered is an error).")
	for _, s := range sites {
		if s.covered != "" {
			continue
		}
		rows = append(rows, sleepFixtureBaselineKey(root, s)+"\t"+sleepFixtureFrozenReason)
	}
	path := filepath.Join(root, sleepFixtureBaselineRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644)
}

// sleepFixtureBaseline reads the frozen census: one `path|line<TAB>reason` row
// per pre-existing site.
func sleepFixtureBaseline(root string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, sleepFixtureBaselineRel))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, reason, ok := strings.Cut(line, "\t")
		if !ok || reason == "" {
			return nil, fmt.Errorf("malformed baseline row %q (want `path|line<TAB>reason`)", line)
		}
		out[key] = reason
	}
	return out, nil
}

// sleepFixtureRepoRoot resolves the repository root from this test's directory.
func sleepFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatalf("resolve the repo root from %s: %v", wd, err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s does not look like the repo root (no go.mod): %v", root, err)
	}
	return root
}

// sleepFixtureSites walks internal/ and cmd/ and judges every sleep-fixture
// spawn in a _test.go file.
func sleepFixtureSites(root string) ([]sleepFixtureSite, error) {
	var sites []sleepFixtureSite
	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")} {
		walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.Contains(path, filepath.Join("internal", "testutil")) {
				return nil // the helper and its own tests define the behaviour under test
			}
			if strings.Contains(path, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
				return nil // fixtures, not source
			}
			found, ferr := sleepFixtureSitesInFile(path)
			if ferr != nil {
				return ferr
			}
			sites = append(sites, found...)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return sites, nil
}

// sleepFixtureSitesInFile judges the sleep spawns of one test file, reading its
// text: the criterion (a Pdeathsig arm inside the same function) is a property
// of the surrounding code, which needs the lines, not just the call.
func sleepFixtureSitesInFile(path string) ([]sleepFixtureSite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	var sites []sleepFixtureSite
	shellHolds := sleepFixtureShellHoldConsts(lines)
	for i := range lines {
		code, _ := splitGoLine(lines[i])
		// A site is anchored where the CALL STARTS — never on a line that merely
		// falls inside the look-ahead window of a spawn written above it, which
		// would report one spawn once per line it occupies.
		if !sleepFixtureCallStart.MatchString(code) {
			continue
		}
		// The spawn's FIRST ARGUMENT may sit on this line or the next ones
		// (a multi-line `exec.Command(`); reading the call as a unit is what
		// keeps the recognizer independent of the author's line breaks.
		call := sleepFixtureCallText(lines, i)
		if sleepFixtureIsShellHeldSleep(call, shellHolds) {
			cover := sleepFixtureShellCoverage(lines, i)
			if cover == "" && sleepFixtureShellHoldIsExec(call) {
				// `exec sleep …` replaces the shell, so there is NO grandchild:
				// the criterion is the direct arm's (Pdeathsig on the command the
				// test holds the handle to), not the shell arm's.
				cover = sleepFixtureCoverage(lines, i)
			}
			sites = append(sites, sleepFixtureSite{
				file:    path,
				line:    i + 1,
				covered: cover,
			})
			continue
		}
		if !sleepFixtureSpawnLine.MatchString(call) {
			continue
		}
		if !sleepFixtureIsLongLived(call) {
			continue // a bounded poll interval: out of scope by the hold it names
		}
		sites = append(sites, sleepFixtureSite{
			file:    path,
			line:    i + 1,
			covered: sleepFixtureCoverage(lines, i),
		})
	}
	return sites, nil
}

// sleepFixtureCallText returns the spawn expression starting at lines[idx] as
// CODE: this line plus the next sleepFixtureArgReach ones (comments stripped),
// so a first argument written on the following line is still read.
func sleepFixtureCallText(lines []string, idx int) string {
	var b strings.Builder
	for i := idx; i < len(lines) && i <= idx+sleepFixtureArgReach; i++ {
		code, _ := splitGoLine(lines[i])
		b.WriteString(code)
		b.WriteByte('\n')
	}
	return b.String()
}

// sleepFixtureShellHoldConsts names every constant in this file that IS a
// long-lived sleep command line.
func sleepFixtureShellHoldConsts(lines []string) map[string]bool {
	out := map[string]bool{}
	for _, line := range lines {
		code, _ := splitGoLine(line)
		if m := sleepFixtureShellHoldConst.FindStringSubmatch(code); m != nil {
			out[m[1]] = true
		}
	}
	return out
}

// sleepFixtureIsShellHeldSleep reports whether call — one spawn expression, with
// its arguments and a couple of following lines — spawns a shell carrying a
// long-lived hold. TWO facts must hold, and neither is a spelling of the path:
//
//   - the program is a shell (any prefix, any of sleepFixtureShellNames);
//   - the command string holds a `sleep` whose duration is unknown at the source
//     or at least sleepFixtureMinHoldSeconds — a bounded `sleep 0.02` inside a
//     shell is a poll interval and is out of scope, exactly as the direct arm
//     decides it.
//
// A hold named by one of this file's shell-hold CONSTANTS counts as a hold whose
// duration cannot be read from the call site, so it is judged a fixture.
func sleepFixtureIsShellHeldSleep(call string, holds map[string]bool) bool {
	if !sleepFixtureShellSpawn.MatchString(call) {
		return false
	}
	if !sleepFixtureShellHoldLine.MatchString(call) {
		for name := range holds {
			if strings.Contains(call, name) {
				return true
			}
		}
		return false
	}
	m := sleepFixtureHoldSeconds.FindStringSubmatch(call)
	if m == nil {
		return true // unreadable duration: the gate must not pass what it cannot measure
	}
	secs, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return true
	}
	return secs >= sleepFixtureMinHoldSeconds
}

// sleepFixtureShellCoverage judges a shell-held sleep. A Pdeathsig arm is NOT
// sufficient here (it stops at the shell: the grandchild ignores it), so the
// covers are the reaping HELPER (it arms Pdeathsig *and* Setpgid, and its cleanup
// signals the whole group, which is what reaches the grandchild) and a stated
// permission — a bare Pdeathsig assignment on the shell is neither.
func sleepFixtureShellCoverage(lines []string, idx int) string {
	for i := idx; i >= 0 && i >= idx-sleepFixtureWindowLines; i-- {
		if strings.Contains(lines[i], sleepFixtureEscape) {
			return sleepFixturePermittedCover + sleepFixtureEscape
		}
	}
	if sleepFixtureHelperCoverage(lines, idx) {
		return sleepFixtureHelperCover
	}
	return ""
}

// sleepFixtureShellHoldIsExec reports whether the shell command string carries the
// hold via `exec`, i.e. the shell REPLACES itself with the sleep and leaves no
// grandchild behind. The recognizer needs this because the shell arm's whole
// premise — "a Pdeathsig stops at the shell, the forked grandchild ignores it" —
// is FALSE for this shape: there is nothing to fork.
func sleepFixtureShellHoldIsExec(call string) bool {
	return sleepFixtureExecHold.MatchString(call)
}

// sleepFixtureIsLongLived reports whether the spawn's hold is long enough to be a
// fixture. An UNREADABLE hold is judged a fixture: the gate must not pass a site
// whose duration it could not measure.
func sleepFixtureIsLongLived(code string) bool {
	m := sleepFixturePollLiteral.FindStringSubmatch(code)
	if m == nil {
		return true
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return true
	}
	return f >= sleepFixtureMinHoldSeconds
}

// sleepFixtureCoverage names the mechanism covering the spawn at lines[idx], or
// "" when nothing does.
func sleepFixtureCoverage(lines []string, idx int) string {
	// A stated permission within the window above the spawn (or on it).
	for i := idx; i >= 0 && i >= idx-sleepFixtureWindowLines; i-- {
		if strings.Contains(lines[i], sleepFixtureEscape) {
			return sleepFixturePermittedCover + sleepFixtureEscape
		}
	}
	// The reaping helper — either applied to this spawn's command below it, or
	// wrapping it (the `StartMockProcessCmd(t, func() *exec.Cmd { … }())` shape,
	// where the helper call is the statement the spawn lives in).
	if sleepFixtureHelperCoverage(lines, idx) {
		return sleepFixtureHelperCover
	}
	// A Pdeathsig armed inside the SAME function: walk down to the end of the
	// enclosing block, bounded.
	depth := 0
	for i := idx; i < len(lines) && i < idx+sleepFixturePdeathsigReach; i++ {
		code, trimmed := splitGoLine(lines[i])
		if i > idx {
			depth += strings.Count(code, "{") - strings.Count(code, "}")
			if depth < 0 || (trimmed == "}" && depth == 0) {
				break // the enclosing function ended: a later fixture's arm is not this one's
			}
		} else {
			depth += strings.Count(code, "{") - strings.Count(code, "}")
		}
		if strings.Contains(code, "Pdeathsig") {
			return "Pdeathsig armed"
		}
	}
	return ""
}

// sleepFixtureHelperCoverage reports whether the spawn at lines[idx] is handed to
// the reaping helper. TWO shapes count, and both are the same property — the
// helper owns the process group and arms Pdeathsig:
//
//   - the spawn is assigned to a variable the helper is then given below it
//     (`cmd := exec.Command(…); testutil.StartMockProcessCmd(t, cmd)`);
//   - the spawn sits INSIDE the helper's own argument list
//     (`testutil.StartMockProcessCmd(t, func() *exec.Cmd { return exec.Command(…) }())`),
//     so the helper call is the enclosing statement, a few lines ABOVE.
func sleepFixtureHelperCoverage(lines []string, idx int) bool {
	code, _ := splitGoLine(lines[idx])
	name := ""
	if m := sleepFixtureSpawnAssign.FindStringSubmatch(code); m != nil {
		name = m[1]
	}
	for i := idx; i < len(lines) && i < idx+sleepFixturePdeathsigReach; i++ {
		if i > idx && sleepFixtureCallStart.MatchString(mustCode(lines[i])) {
			break // a LATER spawn: the helper below it is not this one's cover
		}
		if !sleepFixtureHelperCall.MatchString(mustCode(lines[i])) {
			continue
		}
		if name == "" || i == idx || strings.Contains(mustCode(lines[i]), name+" ") ||
			strings.Contains(mustCode(lines[i]), name+",") || strings.Contains(mustCode(lines[i]), name+")") {
			return true
		}
	}
	// The wrapping shape: walk UP to the statement that opened the call the spawn
	// lives in, stopping at the previous spawn so a sibling's helper cannot cover.
	for i := idx - 1; i >= 0 && i >= idx-sleepFixtureHelperWindowLines; i-- {
		c := mustCode(lines[i])
		if sleepFixtureCallStart.MatchString(c) || sleepFixtureSpawnLine.MatchString(c) {
			return false
		}
		if sleepFixtureHelperCall.MatchString(c) {
			return true
		}
	}
	return false
}

// mustCode is splitGoLine's code half for callers that do not need the trimmed
// form.
func mustCode(line string) string {
	code, _ := splitGoLine(line)
	return code
}

// splitGoLine returns a line with its trailing comment removed, plus the
// trimmed code — comments must not make a site LOOK covered (`// Pdeathsig` in
// prose is not an arm) nor hidden.
func splitGoLine(line string) (code, trimmed string) {
	if i := strings.Index(line, "//"); i >= 0 {
		line = line[:i]
	}
	return line, strings.TrimSpace(line)
}
