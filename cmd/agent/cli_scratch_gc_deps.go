// cli_scratch_gc_deps.go — the `gopi tool scratch-gc deps` arm: the DISCOVERY
// surface for the half of PROJECT.md Rule 35 no test can see.
//
// Rule 35 ("Zero Dependency Is a Discipline") forbids creating a persistent
// dependency — a cron/at line, a systemd unit or timer, a daemon, a state file,
// a new config key — without stating the four things (what breaks without it /
// why it cannot be fixed in-process / who owns its lifecycle and how it is
// reclaimed / what the next reader sees). Two halves cover that rule, and
// NEITHER substitutes for the other:
//
//	REPO-SIDE (a test can decide it)
//	  a self-scheduling script, a scheduler-install recipe, a tracked
//	  state/lock artifact — `internal/testutil/rule35_dependency_gate_test.go`,
//	  run with `go test ./internal/testutil/ -run Rule35`.
//
//	HOST-SIDE (no test in this repo can decide it)
//	  what is INSTALLED on this machine right now: crontab lines, systemd
//	  user units, and resident gopi-shaped processes. This file.
//
// The host half is a REPORT, never a remediation: it writes nothing, edits no
// crontab, stops no unit and kills no process. The operator reads it and makes
// the call (Rule 35's four statements are a human judgement; Rule 25: an
// unwanted resident is not fixed by asking for a privilege, and Rule 27: it is
// certainly not fixed by generating load).
//
// WHY IT RIDES THIS VERB AND ADDS NO TIMER OR DAEMON: `scratch-gc` is already
// the host's ONE one-shot upkeep face (invoked by the existing systemd --user
// timer or a cron line, and by scratchGCTick on a resident host). A new
// surveillance process would itself be the defect Rule 35 describes — a
// dependency created to watch dependencies. So the arm is a pure addition to an
// existing verb, with no new scheduled or resident thing anywhere (Rule 3).

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/cgroupscope"
)

const (
	// scratchGCDepsFlag is the arm selector: `scratch-gc deps` reports what is
	// scheduled and resident on this host.
	scratchGCDepsFlag = "deps"
	// depsLogFmt is the ONE finding line shape this arm prints: machine-parsable
	// logfmt, one finding per line, no blank decoration — the same discipline as
	// every other scratch-gc arm.
	depsLogFmt = "deps kind=%s name=%s where=%s origin=%s"
	// depsSummaryFmt is the arm's TALLY line, kept as its own shape rather than
	// a depsLogFmt row: a summary whose values carry spaces inside a logfmt
	// `key=value` field is a line a parser reads wrong (measured — the first
	// version of this arm printed `origin=gopi=24 foreign=33 total=57`).
	depsSummaryFmt = "deps summary scheduled=%d units_gopi=%d units_foreign=%d resident=%d gopi=%d foreign=%d total=%d"
	// depsNoteLogFmt carries the arm's CONCLUSIONS: the measurement sources it
	// could not read (Rule 15: a probe that cannot measure says so) and the
	// command that covers the other half of the rule.
	depsNoteLogFmt = "deps note=%q"

	// depsCrontabBin is the scheduler CLI whose `-l` output IS the installed
	// cron state. Named (Rule 1b) and never spelled inline.
	depsCrontabBin = "crontab"
	// depsCrontabListFlag reads the crontab. The arm only ever READS it.
	depsCrontabListFlag = "-l"

	// depsSystemdCtlBin is the unit query; depsUserUnitDirRel is the per-user
	// unit directory, relative to the user's home (the gopi watchdog installs
	// there, so it is the directory where a Rule-35 artifact would appear).
	depsSystemdCtlBin     = "systemctl"
	depsUserUnitDirRel    = ".config/systemd/user"
	depsSystemUnitDirPath = "/etc/systemd/system"

	// depsUnitSuffixes are the unit kinds that can SCHEDULE or RESIDE: a
	// .service can be resident, a .timer/.path/.socket can start one. Any of
	// them in the unit directory is a candidate the operator must judge.
	depsUnitSuffixes = ".service,.timer,.socket,.path"

	// depsProcRoot is the process table to enumerate (Rule 24's criterion is
	// read from /proc/<pid>/cgroup, and there is no other source).
	depsProcRoot    = "/proc"
	depsProcCmdline = "cmdline"
	// depsProcExe is the symlink to the process's REAL executable. It is
	// consulted BEFORE argv[0] because argv[0] is not the program (see
	// depsResidentOrigin).
	depsProcExe = "exe"

	// depsInterpreterNames are the shells/interpreters whose exec'd file is a
	// SCRIPT: for these the program name is an argument, so the script path is
	// the thing that carries the name to judge.
	depsInterpreterNames = "sh|bash|dash|python|python3|perl|ruby"

	// depsInterpreterCmdFlag is the interpreter flag whose OPERAND is a command
	// STRING rather than a program (`sh -c "<line>"`). Such an operand is not a
	// name, so it is never classified — see depsOriginFromProgram.
	depsInterpreterCmdFlag = "-c"

	// depsGapMarker is the phrase a measurement GAP carries in its note. The
	// arm's exit status keys off it (Rule 15: a probe that could not read a
	// source must not report the same thing as a probe that read it and found
	// nothing).
	depsGapMarker = "NOT measured"

	// depsOriginGopi / depsOriginForeign classify a finding by the WORDS on
	// it. `foreign` is not a verdict — Rule 35 is about gopi's own
	// dependencies, and showing the neighbours' entries is how the operator
	// knows the scan actually looked at the whole host instead of filtering
	// first.
	depsOriginGopi    = "gopi"
	depsOriginForeign = "foreign"
	depsOriginUnknown = "unknown"

	// depsDetachedYes/No/Unknown are the three cgroup verdicts (Rule 24).
	depsDetachedYes     = "yes"
	depsDetachedNo      = "no"
	depsDetachedUnknown = "unknown"

	// depsExitError is the arm's own non-zero status (the verb's shared
	// status, kept as its own name so the arm reads on its own).
	depsExitError = 1
)

// depsGopiNameRes recognises a gopi-shaped command: the token `gopi` (also
// `gopi-*`), plus the HP-side worker names this host's gopi family uses
// (peer-launcher-autostart / gopi-worker-up). It is deliberately a NAME test, not a
// process-ownership test: the question the operator asks is "is anything of
// OURS scheduled here", and an entry pointing at the retired autostart is
// exactly the case that must be visible.
var depsGopiNameRes = regexp.MustCompile(`(^|[^A-Za-z0-9_-])(gopi|hp-worker)([^A-Za-z0-9_]|$)`)

// depsInterpreterRes matches an interpreter basename (see
// depsInterpreterNames), so the arm knows to look one argument deeper.
var depsInterpreterRes = regexp.MustCompile(`^(` + depsInterpreterNames + `)([0-9.]*)$`)

// depsResidentOrigin answers the origin question for ONE pid, and it does NOT
// read argv[0]: argv[0] is what the process was ASKED to be, not what it is.
//
// Measured 2026-09-26 on this host: Chromium REWRITES its own argv, so the
// first NUL-separated field of /proc/<pid>/cmdline is the ENTIRE command line
// ("chrome --type=gpu-process … --user-data-dir=…/queue-runtime-shot-…/
// gopi-shot-profile-…"). The old argv[0]-based test therefore read the whole
// line and classified 22 chromium processes as `origin=gopi` — the word came
// from a scratch DIRECTORY NAME, so the same browser launched with
// `--user-data-dir=/tmp/prof` would have been `foreign`. A verdict that flips
// on a directory name is not a measurement.
//
// The program is /proc/<pid>/exe (the real exec'd file): `chrome` there, for
// both spellings. An interpreter is the one case where the exe is NOT the
// program — `sh -c /home/user/bin/gopi-worker-up` is really the script — so for
// a shell the argument is judged instead — but only when that argument IS a
// name (see depsOriginFromProgram). An unreadable exe is `unknown`, never a
// silent `foreign` (Rule 15).
func depsResidentOrigin(pid int, parts []string) string {
	exe, err := os.Readlink(filepath.Join(depsProcRoot, strconv.Itoa(pid), depsProcExe))
	if err != nil || exe == "" {
		return depsOriginUnknown
	}
	return depsOriginFromProgram(filepath.Base(exe), parts)
}

// depsOriginFromProgram decides one process's origin from the basename of its
// REAL executable plus its argv. It is the pure half of depsResidentOrigin,
// split out so both argv traps can be tested without planting a process for
// each.
//
// THE ONE DEFECT, TWO SIGHTINGS — both are "a DIRECTORY name decided the
// verdict", and this function exists to make that unrepresentable:
//
//  1. argv[0] (fixed 2026-09-26, commit before this one): Chromium rewrites
//     its own argv, so cmdline field 0 was the WHOLE command line and
//     filepath.Base of it ended in a scratch directory name — 22 browsers
//     reported as gopi's.
//  2. the INTERPRETER branch (measured again 2026-09-26, this commit):
//     `filepath.Base(<first non-flag argument>)` is only a program name when
//     the argument is a PATH. For `bash -c "<line>"` it is a command STRING,
//     so `bash -c "cd /home/user/workspace/gopi-01a0… && sleep 60"` — a
//     generic shell, two of them measured live — read as `origin=gopi`
//     because the line mentions a WORKTREE DIRECTORY. Worse, the line was the
//     arm's own invocation, so the count depended on how the report was run
//     (measured: 7 residents plainly, 8 wrapped, same host, same moment).
//
// A command string is therefore NOT judged (Rule 15: an answer the arm cannot
// justify is `unknown`, not a guess). Nothing real is lost by that: the
// program a command string starts is its own process with its own exe, so
// `bash -c "./bin/gopi web"` still surfaces as the `gopi web` resident.
func depsOriginFromProgram(prog string, parts []string) string {
	if !depsInterpreterRes.MatchString(prog) {
		return depsClassify(prog)
	}
	args := parts[1:]
	for i, arg := range args {
		// `sh -c "<line>"`: the operand is a command string, never a program.
		if arg == depsInterpreterCmdFlag && i+1 < len(args) {
			return depsOriginUnknown
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue // a flag, not the script
		}
		if strings.ContainsAny(arg, " \t") {
			return depsOriginUnknown // a quoted name with spaces is not a name to judge
		}
		return depsClassify(filepath.Base(arg))
	}
	// An interpreter with no script operand at all (an interactive shell): there
	// is no name here, and inventing one from the interpreter's own basename
	// would call every open terminal a dependency.
	return depsOriginUnknown
}

// depsFinding is one scheduled or resident thing on this host.
type depsFinding struct {
	kind   string // crontab | unit | unit-active | resident
	name   string // the entry text, the unit name, or the process argv
	where  string // where it lives: crontab, the unit path, or pid=<n>
	origin string // gopi | foreign | unknown
	extra  string // kind-specific field (enabled=…, cgroup=…), "" when absent
}

// depsClassify returns the origin of a piece of text.
func depsClassify(text string) string {
	if strings.TrimSpace(text) == "" {
		return depsOriginUnknown
	}
	if depsGopiNameRes.MatchString(text) {
		return depsOriginGopi
	}
	return depsOriginForeign
}

// depsCronCommand extracts the COMMAND from one crontab line, and reports
// whether the line is a schedule at all. A crontab also carries environment
// assignments and comments; neither schedules anything, and reporting them as
// findings would be noise a reader learns to skip.
func depsCronCommand(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if strings.HasPrefix(fields[0], "@") {
		if len(fields) < 2 {
			return "", false
		}
		return strings.Join(fields[1:], " "), true
	}
	// Five time fields, then the command (the sixth field onward).
	if len(fields) < 6 {
		return "", false
	}
	for _, f := range fields[:5] {
		if !depsCronFieldRes.MatchString(f) {
			return "", false
		}
	}
	return strings.Join(fields[5:], " "), true
}

// depsCronFieldRes is the cron time-field alphabet: digits, `*`, ranges,
// steps and lists. A field outside it means the line is not a schedule.
var depsCronFieldRes = regexp.MustCompile(`^[0-9*/,\-]+$`)

// depsCrontabFindings reads the installed crontab. A missing `crontab` binary
// or an empty crontab is a MEASURED answer ("nothing is scheduled for this
// user") and is reported as such; only a read that cannot be interpreted is an
// unknown (Rule 15: never a silent empty).
func depsCrontabFindings() (findings []depsFinding, note string) {
	out, err := exec.Command(depsCrontabBin, depsCrontabListFlag).Output()
	if err != nil {
		// `crontab -l` exits non-zero when the user has NO crontab — that is a
		// measurement, not a failure, and the two must not be confused.
		if ee, ok := err.(*exec.ExitError); ok && len(strings.TrimSpace(string(ee.Stderr))) == 0 {
			return nil, "crontab: no crontab for this user (nothing scheduled)"
		}
		if _, lookErr := exec.LookPath(depsCrontabBin); lookErr != nil {
			return nil, "crontab: binary absent on this host — cron state NOT measured"
		}
		return nil, fmt.Sprintf("crontab: unreadable (%v) — cron state NOT measured", err)
	}
	seen, inactive := 0, 0
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		cmd, ok := depsCronCommand(line)
		if !ok {
			inactive++ // a comment or an environment assignment: not a schedule
			continue
		}
		seen++
		findings = append(findings, depsFinding{
			kind:   "crontab",
			name:   cmd,
			where:  depsCrontabBin,
			origin: depsClassify(cmd),
		})
	}
	if seen == 0 {
		// The measured distinction that matters: a crontab holding only
		// commented-out history (this host's real state) is NOT the same as a
		// crontab that holds an active schedule, and neither is the same as no
		// crontab at all. Saying "empty" for a file with 9 lines would be the
		// silent imprecision Rule 15 forbids.
		return nil, fmt.Sprintf("crontab: %d entry(ies), 0 active schedule(s) — every line is commented or an assignment", inactive)
	}
	return findings, ""
}

// depsUnitDirFindings lists the unit FILES installed for this user and for the
// system, filtered to the unit kinds that can schedule or reside. It uses ONE
// `systemctl --user is-enabled` per candidate (bounded) so "installed" and
// "actually enabled" are distinguishable — an installed-but-disabled unit is
// inert, and saying so is the difference between a report and an alarm.
func depsUnitDirFindings() (findings []depsFinding, notes []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, []string{fmt.Sprintf("units: cannot resolve the home directory (%v) — user units NOT measured", err)}
	}
	userDir := filepath.Join(home, depsUserUnitDirRel)
	for _, dir := range []string{userDir, depsSystemUnitDirPath} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// A missing directory is an answer; anything else is a gap, named.
			if os.IsNotExist(err) {
				notes = append(notes, fmt.Sprintf("units: %s absent (nothing installed there)", dir))
				continue
			}
			notes = append(notes, fmt.Sprintf("units: %s unreadable (%v) — NOT measured", dir, err))
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.Contains(depsUnitSuffixes, filepath.Ext(e.Name())) {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		if len(names) == 0 {
			notes = append(notes, fmt.Sprintf("units: %s holds no service/timer/socket/path unit", dir))
		}
		for _, name := range names {
			f := depsFinding{
				kind:   "unit",
				name:   name,
				where:  filepath.Join(dir, name),
				origin: depsClassify(name),
			}
			if state, ok := depsUnitEnabledState(name); ok {
				f.extra = "enabled=" + state
			}
			findings = append(findings, f)
		}
	}
	return findings, notes
}

// depsUnitEnabledState asks systemd whether a unit is enabled. The answer is
// advisory: a host with no user bus cannot answer, and the caller then prints
// no enabled field rather than inventing one.
func depsUnitEnabledState(unit string) (string, bool) {
	out, err := exec.Command(depsSystemdCtlBin, scratchGCDepsSystemdUserFlag, "is-enabled", unit).Output()
	if err != nil {
		return "", false
	}
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "", false
	}
	return state, true
}

// scratchGCDepsSystemdUserFlag selects the user manager (never the system one:
// gopi's own upkeep is a user-scope unit by Rule 25 — a privilege is not a
// remedy here, and this arm never asks for one).
const scratchGCDepsSystemdUserFlag = "--user"

// depsResidentFindings enumerates the process table and returns the
// gopi-shaped residents, each with its detachment verdict measured from
// /proc/<pid>/cgroup (Rule 24 — and ONLY from there: PPID=1, PGID==SID and a
// successful setsid() are not evidence, so this arm reads the cgroup).
func depsResidentFindings() (findings []depsFinding, note string) {
	pids, err := depsProcPIDs()
	if err != nil {
		return nil, fmt.Sprintf("resident: cannot enumerate %s (%v) — residents NOT measured", depsProcRoot, err)
	}
	self := os.Getpid() // the report itself is not a dependency it should report
	for _, pid := range pids {
		if pid == self {
			continue
		}
		argv, err := os.ReadFile(filepath.Join(depsProcRoot, strconv.Itoa(pid), depsProcCmdline))
		if err != nil {
			continue // the process exited mid-scan, or is not ours to read
		}
		parts := strings.Split(strings.TrimRight(string(argv), "\x00"), "\x00")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		// A resident is gopi-SHAPED when the PROGRAM is (Rule 15's "no silent
		// wrong answer" applies to a verdict that comes from the wrong field —
		// see depsResidentOrigin for the measured argv[0] defect this avoids).
		origin := depsResidentOrigin(pid, parts)
		if origin != depsOriginGopi {
			continue
		}
		findings = append(findings, depsFinding{
			kind:   "resident",
			name:   strings.Join(parts, " "),
			where:  "pid=" + strconv.Itoa(pid),
			origin: origin,
			extra:  "detached=" + depsDetachedVerdict(pid),
		})
	}
	return findings, ""
}

// depsProcPIDs lists the numeric entries of /proc (sorted, so two runs of this
// arm on a quiet host print the same order).
func depsProcPIDs() ([]int, error) {
	entries, err := os.ReadDir(depsProcRoot)
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids, nil
}

// depsDetachedVerdict answers Rule 24's question about one pid: has it left the
// login session scope? It reads the cgroup RELATIVE path through the same
// helper the daemon path uses, so this report and the daemon's own start-time
// announcement can never disagree about what "detached" means. An unreadable
// cgroup is `unknown`, never `yes` (Rule 15).
func depsDetachedVerdict(pid int) string {
	rel, err := cgroupscope.RelOfPID(pid)
	if err != nil {
		return depsDetachedUnknown
	}
	if cgroupscope.InSessionScope(rel) {
		return depsDetachedNo
	}
	return depsDetachedYes
}

// depsCgroupOf returns the pid's cgroup relative path and whether it was
// readable — used only for the report line's cgroup field.
func depsCgroupOf(pid int) (string, bool) {
	rel, err := cgroupscope.RelOfPID(pid)
	if err != nil {
		return "", false
	}
	return rel, true
}

// runScratchGCDeps is the arm's entry point: gather, print, exit. It is
// READ-ONLY by construction — every call below reads a file or lists a
// directory; no crontab is written, no unit is touched, no process is signalled.
func runScratchGCDeps() {
	notes := []string{}
	findings := []depsFinding{}

	cron, cronNote := depsCrontabFindings()
	findings = append(findings, cron...)
	if cronNote != "" {
		notes = append(notes, cronNote)
	}

	units, unitNotes := depsUnitDirFindings()
	findings = append(findings, units...)
	notes = append(notes, unitNotes...)
	if foreignUnits := depsCountForeign(units); foreignUnits > 0 {
		notes = append(notes, fmt.Sprintf(
			"units: %d unit file(s) installed here are not gopi-shaped — listed on stderr, so this report keeps only gopi's own", foreignUnits))
	}

	resident, residentNote := depsResidentFindings()
	findings = append(findings, resident...)
	if residentNote != "" {
		notes = append(notes, residentNote)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].kind != findings[j].kind {
			return findings[i].kind < findings[j].kind
		}
		return findings[i].name < findings[j].name
	})
	gopiCount, foreignCount, residentCount, unitForeign, unitGopi := 0, 0, 0, 0, 0
	for _, f := range findings {
		// A FOREIGN entry is one this project does not own (a desktop service,
		// a snap socket). Rule 35 is about gopi's own dependencies, so the
		// listing is the gopi-shaped rows plus a COUNT of the neighbours: a
		// list of 33 unrelated OS units is noise a reader learns to skip, and a
		// bare count would hide that the scan looked at the whole host. Both
		// facts are printed, in that order — the neighbours are named on
		// stderr, so a machine parsing stdout is not asked to filter them.
		line := fmt.Sprintf(depsLogFmt, f.kind, depsQuote(f.name), depsQuote(f.where), f.origin)
		if f.extra != "" {
			line += " " + f.extra
		}
		if f.origin == depsOriginGopi {
			if f.kind == "resident" && strings.HasPrefix(f.where, "pid=") {
				if pid, err := strconv.Atoi(strings.TrimPrefix(f.where, "pid=")); err == nil {
					if rel, ok := depsCgroupOf(pid); ok {
						line += " cgroup=" + depsQuote(rel)
					}
				}
			}
			fmt.Println(line)
		} else {
			// Foreign rows go to STDERR, one line each, so the operator can see
			// the whole host without the stdout report losing its signal.
			fmt.Fprintln(os.Stderr, line)
		}
		switch {
		case f.origin == depsOriginGopi:
			gopiCount++
			if f.kind == "unit" {
				unitGopi++
			}
		default:
			foreignCount++
			if f.kind == "unit" {
				unitForeign++
			}
		}
		if f.kind == "resident" {
			residentCount++
		}
	}
	// units_gopi is what a human scans for; units_foreign is the reason they can
	// trust that the scan looked past the report's own filter.
	fmt.Printf(depsSummaryFmt+"\n", len(cron), unitGopi, unitForeign, residentCount, gopiCount, foreignCount, len(findings))
	for _, n := range notes {
		fmt.Printf(depsNoteLogFmt+"\n", n)
	}
	fmt.Printf(depsNoteLogFmt+"\n", depsRepoHalfNote)
	if gaps := depsMeasurementGaps(notes); gaps > 0 {
		fmt.Printf(depsNoteLogFmt+"\n", fmt.Sprintf(
			"%d measurement source(s) could not be read — the report above is INCOMPLETE, not empty (Rule 15)", gaps))
		os.Exit(depsExitError)
	}
}

// depsCountForeign counts the findings whose origin is not gopi.
func depsCountForeign(findings []depsFinding) int {
	n := 0
	for _, f := range findings {
		if f.origin != depsOriginGopi {
			n++
		}
	}
	return n
}

// depsRepoHalfNote names the other half of Rule 35, on the report itself: a
// reader who runs this arm must not conclude that nothing else checks the rule.
const depsRepoHalfNote = "host half only — the repo half (self-scheduling scripts, scheduler-install " +
	"recipes, tracked state/lock artifacts) is checked by: go test ./internal/testutil/ -run Rule35"

// depsMeasurementGaps counts the notes that report a source this arm could not
// read at all. A gopi-shaped finding is NOT a gap (the operator's own watchdog
// is one, and this arm is read-only — it is the operator's call), so the exit
// status keys off gaps, never off findings.
func depsMeasurementGaps(notes []string) int {
	gaps := 0
	for _, n := range notes {
		if strings.Contains(n, depsGapMarker) {
			gaps++
		}
	}
	return gaps
}

// depsQuote makes a value safe to print in a logfmt field: quoted, with the
// quote and backslash escaped, so a crontab line containing spaces or a `=`
// cannot be mis-parsed as extra fields.
func depsQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\t", " ")
	return `"` + r.Replace(v) + `"`
}
