package testutil

// Rule 42 (Destructive Process Signals Are Banned) as a CHECK.
//
// WHY THIS FILE EXISTS
// --------------------
// PROJECT.md Rule 42 shipped (f739709) with a `**Machine check.**` paragraph that
// named THIS file: `go test ./internal/testutil/ -run Rule42`. No file matched, so
// the command printed `testing: warning: no tests to run` and exited 0 — a NAMED
// CHECK THAT MATCHED NOTHING, which is worse than no check at all, because its
// green is indistinguishable from enforcement (Rule 36 and Rule 38 record the same
// failure class, each after its own month of vacuum). This file is the gate the
// paragraph was already claiming; the paragraph was then corrected to describe
// exactly what this file does and does not cover.
//
// WHAT THE RULE BANS, AND THEREFORE WHAT IS SCANNED
// -------------------------------------------------
//   CAUGHT  a Go source calling `Kill(0, …)` or `Kill(-1, …)` at all — those two
//           addresses reach the caller's own process group and every process the
//           caller may signal. There is no ownership question to answer: the
//           targeting is the defect. Measured zero in this tree at 0336db7.
//   CAUGHT  Go code EXECUTING a name/pattern matcher as a control path
//           (`exec.Command…("pkill"|"killall"|"pgrep")`). Measured zero.
//   CAUGHT  a `signal.Reset(sig)` in a file that also re-raises to itself
//           (`Kill(os.Getpid(), sig)`): Reset is cancel(sig, disableSignal) over
//           EVERY registration, so it dismantles a handler another package
//           installed — the measured 2026-10-23 defect (commit c9c0534).
//   CAUGHT  a tracked script under `scripts/` running `pkill -f`, `pgrep -f`,
//           `killall`, `kill 0`, `kill -1` or the `kill -- -PGID` spelling.
//   REPORTED (never failed) the legitimate group signals: ~40 sites send
//           `syscall.Kill(-pgid, …)` after creating the group themselves
//           (`Setpgid: true` on their own `exec.Cmd`, or a `*FilmTerminal` that
//           keeps its `*exec.Cmd`). Ownership is a fact about a HANDLE, and a
//           text gate cannot read one — so this half is printed on every run
//           rather than enforced. That printed census is the reason the gate can
//           say "not flagged" instead of "clean".
//   NOT CAUGHT  a signal whose target arrives through a variable
//           (`kill(name)` / `syscall.Kill(pgid, sig)` where the group id was read
//           or computed), an over-broad signal inside a non-script file (a
//           `.py`, a Makefile recipe, a devdoc/evidence capture — those trees are
//           deliberately outside the walk), or any intent behind a call whose
//           addressing literals look safe. **This gate is a FLOOR, not the rule.**
//
// THE RED PROOF IS A FIXTURE TREE, NOT A HOPE: TestRule42PlantedViolationsTurnItRed
// writes the banned forms into a scratch directory and drives the SAME predicates
// the real-tree gate uses, together with CLEAN CONTROLS (a legitimate
// `Kill(-pgid, …)`, `kill -0 "$pid"` liveness probes, `kill -TERM "$pid"`) that
// must NOT fire — otherwise "everything matches" would pass as detection.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The kinds a hit is labelled with. Bare literals would be Rule 1 violations, and
// the labels are compared, so they live here.
const (
	// rule42KindKillEverything is `Kill(-1, …)`: every process the caller may
	// signal — the operator's desktop included.
	rule42KindKillEverything = "kill-every-process"
	// rule42KindOwnGroup is `Kill(0, …)`: the caller's own process group, which
	// may be the operator's terminal session.
	rule42KindOwnGroup = "kill-own-process-group"
	// rule42KindNamePattern is a name/pattern matcher used as a control path.
	rule42KindNamePattern = "name-pattern-control-path"
	// rule42KindResetReraise is `signal.Reset` without a `signal.Stop` in front,
	// followed by a self re-raise: Reset is cancel(sig, disableSignal) over every
	// registration, so an arm that never dropped its own dismantles another
	// package's handler — the measured 2026-10-23 defect.
	rule42KindResetReraise = "signal-reset-without-stop-reraise"
	// rule42KindResetAfterStop is the same pair in a file that DOES drop its own
	// subscription first (`signal.Stop`). It is still FLAGGED, not tolerated: Rule
	// 42 lists "`signal.Reset(sig)` used to re-raise" in its banned table, and a
	// reported kind would make the gate blind to a NEW such arm as well. Each real
	// site carries a named per-file permit with its measurement (bug 01a16d37),
	// and a permit that outlives its shape fails the ratchet-down check.
	rule42KindResetAfterStop = "signal-reset-after-stop"
	// rule42KindGroupSignal is the REPORTED arm: a group-addressed signal, which is
	// legitimate exactly when the caller made the group (a handle a text gate
	// cannot read), so it is counted and printed on every run and never failed.
	rule42KindGroupSignal = "group-signal-reported"
	// rule42KindPgrepRead is a `pgrep -f` control path: it reads the process table
	// and signals nothing, so it is printed rather than failed.
	rule42KindPgrepRead = "pattern-read-reported"
)

// rule42ReportedKinds are the kinds the gate PRINTS rather than fails. They are
// shapes that are legitimate in some form a TEXT gate cannot distinguish, and
// failing them would redden ~40 reviewed sites — which is how a gate gets
// switched off. Keeping them apart from the exemptions matters: an exemption says
// "this one is reviewed and allowed", a reported kind says "the check does not
// reach this question at all".
var rule42ReportedKinds = map[string]struct{}{
	rule42KindGroupSignal: {},
	rule42KindPgrepRead:   {},
}

const (
	// rule42ScriptsRel is the one script tree the gate JUDGES. `devdoc/evidence/`
	// and `plugins/` carry recorded captures and plugin-owned launchers: they are
	// history and third-party material, not gopi's control paths.
	rule42ScriptsRel = "scripts"
	// rule42GoFileFloor fails the gate if the index walk reaches fewer Go files
	// than this. A scan that judged (almost) nothing must not read as a clean
	// tree — the vacuum this file exists to end. Measured 2955 at 0336db7.
	rule42GoFileFloor = 2000
	// rule42ScriptFileFloor is the same floor for the script walk. Measured 152
	// tracked `scripts/*.sh` at 0336db7.
	rule42ScriptFileFloor = 100
	// rule42AddressScanFileCap bounds both walks of the ADDRESS half; a cap that
	// is HIT is disclosed, never silently truncated into a pass (Rule 15). It is
	// a SEPARATE constant from the re-raise half's rule42ScanFileCap (6000):
	// this arm walks the whole on-disk tree — Go sources plus scripts/ — while
	// that one reads single Go files, so one shared cap would either truncate
	// this walk or hand the other half a bound it never needs.
	rule42AddressScanFileCap = 20000
	// rule42SelfRel is the detector itself, which must NAME every banned shape in
	// its patterns and its planted fixtures.
	rule42SelfRel = "internal/testutil/rule42_destructive_signal_gate_test.go"
)

// rule42Source is one file's comment-stripped code, kept 1:1 with the file's
// lines so a hit can be reported at its real line number.
type rule42Source struct {
	rel  string
	code []string
}

// rule42Hit is one banned form in one file.
type rule42Hit struct {
	rel  string
	kind string
	line int
	text string
}

func (h rule42Hit) String() string {
	return h.rel + ":" + strconv.Itoa(h.line) + " [" + h.kind + "] " + strings.TrimSpace(h.text)
}

// The Go arms. `\bKill\(` covers `syscall.Kill`, `unix.Kill` and a package-level
// alias; the address is what is banned, not which import spelled it.
var (
	rule42KillEverythingRe = regexp.MustCompile(`\bKill\(\s*-\s*1\s*,`)
	rule42KillOwnGroupRe   = regexp.MustCompile(`\bKill\(\s*0\s*,`)
	rule42NameToolExecRe   = regexp.MustCompile(`\bexec\.Command(Context)?\([^)]*\b(pkill|killall|pgrep)\b`)
	rule42ResetRe          = regexp.MustCompile(`\bsignal\.Reset\(`)
	rule42StopRe           = regexp.MustCompile(`\bsignal\.Stop\(`)
	rule42SelfKillRe       = regexp.MustCompile(`\bKill\(\s*(os|syscall)\.Getpid\(\)`)
	// rule42GroupSignalRe is the REPORTED arm: a group-addressed signal, which is
	// legitimate exactly when the caller made the group. It is never a failure.
	rule42GroupSignalRe = regexp.MustCompile(`\bKill\(\s*-[[:space:]]*[A-Za-z_]`)
)

// The script arms. `kill -0 PID` is a liveness PROBE (signal 0 to one pid), not a
// group signal, so the pattern requires the bare `0`/`-1` form that addresses a
// group — a distinction the control fixture pins. `pgrep -f` is split out from
// `pkill`: pgrep only READS the process table, so it is PRINTED, never failed — a
// read cannot signal anything.
var (
	rule42ShellNamePatternRe  = regexp.MustCompile(`(^|[^A-Za-z_.-])(pkill[[:space:]]+-[A-Za-z0-9]*f|killall)([^A-Za-z_-]|$)`)
	rule42ShellPgrepPatternRe = regexp.MustCompile(`(^|[^A-Za-z_.-])pgrep[[:space:]]+-[A-Za-z0-9]*f([^A-Za-z_-]|$)`)
	rule42ShellKillGroupRe    = regexp.MustCompile(`(^|[^A-Za-z_.-])kill[[:space:]]+(-[A-Za-z0-9]+[[:space:]]+)*(-1|0)([[:space:]]|$|;)`)
	rule42ShellKillDashDashRe = regexp.MustCompile(`(^|[^A-Za-z_.-])kill[[:space:]]+(-[A-Za-z0-9]+[[:space:]]+)*--[[:space:]]+-[0-9]+`)
)

// rule42Exemptions is the reviewed allow-list: file → kind → the reason it is
// allowed to carry that shape. An entry is NARROW (one file, one kind) and it is
// checked in both directions — a row whose shape no longer occurs fails the gate,
// so the list can only ratchet down, and a row whose file is gone fails too.
var rule42Exemptions = map[string]map[string]string{
	rule42SelfRel: {
		rule42KindKillEverything: "the detector itself: it must NAME every banned address in its " +
			"patterns and feed the planted fixtures through its own predicate — the same self-exemption " +
			"the Rule 27 gate carries, and it cannot hide a new generator because the file IS the check",
		rule42KindOwnGroup: "the detector itself (see the entry above)",
		rule42KindResetReraise: "the detector itself: it must name `signal.Reset` and the self re-raise " +
			"that makes the pair a defect, and its fixtures spell both",
		rule42KindNamePattern: "the detector itself: its CLEAN CONTROL for `pkill` is a Go string literal " +
			"handed to exec.CommandContext, which is a fixture, not a call — the control must spell the " +
			"banned shape to prove the arm does not reach a script-read-shaped file",
		rule42KindResetAfterStop: "the detector itself: its CLEAN CONTROL for a Reset with no re-raise is " +
			"the same string-literal shape (the arm reads the file, never the string the test holds)",
	},
	// THE FOUR Stop-then-Reset RE-RAISE SITES ARE GONE FROM THIS TABLE, and the
	// deletion is the ratchet working. Task 01a16d37 fixed every one of them:
	// toolsReraise, signalrec's reRaise, lineedit's fatal path and merge_run's
	// second-Ctrl-C arm now release their own subscription (signalrec.
	// ReleaseSignalHandlers / signal.Stop) and re-raise, so no blanket
	// `signal.Reset` remains at any of the four. A permit whose shape is gone
	// FAILS this gate by design — the list only ratchets down — so keeping the
	// rows would have reddened it for exactly the reason the fix succeeded.
	"scripts/voice-capture-failmatrix.sh": {
		rule42KindNamePattern: "case (c) interrupts the client's OWN recorder, matched on the unique " +
			"per-run output path (`arecord.*${out}`) so nothing else on the host can be reached — the " +
			"narrowest form of the ban, grandfathered rather than fixed here (a rule fix belongs in its " +
			"own task, not in the task that makes the citation true)",
	},
}

// rule42StripGoComments reuses the Rule 27 gate's stripper (Rule 35: do not build
// a second one). A comment NAMING a banned shape is documentation; only code is
// judged — that is what lets this file talk about `Kill(-1, …)` in prose.
func rule42StripGoComments(lines []string) []string { return rule27StripGoComments(lines) }

// rule42GoSource prepares a Go file for the arms.
func rule42GoSource(rel, body string) rule42Source {
	lines := strings.Split(body, "\n")
	return rule42Source{rel: rel, code: rule42StripGoComments(lines)}
}

// rule42ShellSource prepares a shell file: full-line and trailing `#` comments
// are removed (stripComment), because both `scripts/nonff-able-cohort-after.sh`
// and `scripts/gopi-worker-up.sh` document the ban they no longer commit.
func rule42ShellSource(rel, body string) rule42Source {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = stripComment(line)
	}
	return rule42Source{rel: rel, code: out}
}

// rule42GoHits applies the Go arms to one source.
func rule42GoHits(src rule42Source) []rule42Hit {
	var hits []rule42Hit
	selfReRaises, resignsFirst := false, false
	for _, line := range src.code {
		if rule42SelfKillRe.MatchString(line) {
			selfReRaises = true
		}
		if rule42StopRe.MatchString(line) {
			resignsFirst = true
		}
	}
	for i, line := range src.code {
		if line == "" {
			continue
		}
		add := func(kind string) {
			hits = append(hits, rule42Hit{rel: src.rel, kind: kind, line: i + 1, text: line})
		}
		if rule42KillEverythingRe.MatchString(line) {
			add(rule42KindKillEverything)
		}
		if rule42KillOwnGroupRe.MatchString(line) {
			add(rule42KindOwnGroup)
		}
		if rule42NameToolExecRe.MatchString(line) {
			add(rule42KindNamePattern)
		}
		// `signal.Reset` is only the BANNED shape when the file never drops its own
		// subscription first: without a `signal.Stop` the Reset tears down every
		// registration, including a handler another package installed (the measured
		// 2026-10-23 defect, commit c9c0534). WITH a Stop in the same file it is the
		// other thing entirely — handing the default disposition back to this
		// process — which Rule 42's own prose endorses, so that pairing is REPORTED.
		if rule42ResetRe.MatchString(line) && selfReRaises {
			if resignsFirst {
				add(rule42KindResetAfterStop)
			} else {
				add(rule42KindResetReraise)
			}
		}
	}
	return hits
}

// rule42ShellHits applies the script arms to one source.
func rule42ShellHits(src rule42Source) []rule42Hit {
	var hits []rule42Hit
	for i, line := range src.code {
		if line == "" {
			continue
		}
		if rule42ShellNamePatternRe.MatchString(line) {
			hits = append(hits, rule42Hit{rel: src.rel, kind: rule42KindNamePattern, line: i + 1, text: line})
		}
		if rule42ShellPgrepPatternRe.MatchString(line) {
			hits = append(hits, rule42Hit{rel: src.rel, kind: rule42KindPgrepRead, line: i + 1, text: line})
		}
		if rule42ShellKillGroupRe.MatchString(line) || rule42ShellKillDashDashRe.MatchString(line) {
			hits = append(hits, rule42Hit{rel: src.rel, kind: rule42KindOwnGroup, line: i + 1, text: line})
		}
	}
	return hits
}

// rule42Court separates the failures from the reviewed exemptions and from the
// kinds the check does not reach at all, so every run can PRINT what it let
// through and WHY (a silent pass is the vacuum this file replaced).
func rule42Court(hits []rule42Hit) (rejected, excused, reported []rule42Hit) {
	for _, h := range hits {
		if _, ok := rule42ReportedKinds[h.kind]; ok {
			reported = append(reported, h)
			continue
		}
		if _, ok := rule42Exemptions[h.rel][h.kind]; ok {
			excused = append(excused, h)
			continue
		}
		rejected = append(rejected, h)
	}
	return rejected, excused, reported
}

// rule42ReportGroupSignals counts the REPORTED arm — the legitimate
// `Kill(-pgid, …)` sites — and returns them so the run can print the census.
func rule42ReportGroupSignals(src rule42Source) []rule42Hit {
	var out []rule42Hit
	for i, line := range src.code {
		if line == "" || !rule42GroupSignalRe.MatchString(line) {
			continue
		}
		// A banned literal is already a rejection; do not double-report it here.
		if rule42KillEverythingRe.MatchString(line) || rule42KillOwnGroupRe.MatchString(line) {
			continue
		}
		out = append(out, rule42Hit{rel: src.rel, kind: rule42KindGroupSignal, line: i + 1, text: line})
	}
	return out
}

// rule42GoFiles lists the Go files the gate JUDGES.
//
// IT WALKS THE WORKING TREE, NOT THE INDEX — and that is a correction this file
// had to make about itself, measured. The first version read `git ls-files`, so
// while this detector was still an untracked file it never scanned ITSELF, and
// the green it printed covered 2955 of 2956 Go files. The moment the file was
// committed the four banned shapes it carries as PLANTED FIXTURE LITERALS
// appeared — which is the self-exemption doing its job, but it was invisible
// until then. A rule about reaching things you do not hold should not rest on a
// scan that skips exactly the file being written; an uncommitted scratch file is
// litter by Rule 30, and a banned address in it is still a banned address.
func rule42GoFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if len(out) >= rule42AddressScanFileCap {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		t.Fatalf("walk %s for Go sources: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// rule42ScriptFiles walks the one script tree the gate judges, untracked files
// included: a shell helper written into `scripts/` during a session is exactly as
// reachable as a committed one.
func rule42ScriptFiles(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(rule42ScriptsRel))
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".sh") && !strings.HasSuffix(path, ".bash") {
			return nil
		}
		if len(out) >= rule42AddressScanFileCap {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		t.Fatalf("walk %s: %v", rule42ScriptsRel, err)
	}
	sort.Strings(out)
	return out
}

// rule42JudgeTree runs every arm over the working tree and returns the rejected
// hits, the excused hits, the reported group-signal census and the counts that
// make the run non-vacuous. It is the ONE judge — the real-tree test and the red
// proof both drive it, so a predicate change cannot make the proof green while
// the gate is red (or the reverse).
func rule42JudgeTree(t *testing.T, root string) (rejected, excused, reported []rule42Hit, judged int) {
	t.Helper()
	goFiles := rule42GoFiles(t, root)
	scripts := rule42ScriptFiles(t, root)
	if len(goFiles) < rule42GoFileFloor {
		t.Fatalf("the working-tree walk reached %d Go file(s), below the %d floor — a scan that judged "+
			"(almost) nothing must not read as a clean tree (Rule 38: refuse a vacuous green)",
			len(goFiles), rule42GoFileFloor)
	}
	if len(scripts) < rule42ScriptFileFloor {
		t.Fatalf("the script walk reached %d file(s) under %s/, below the %d floor — either the tree "+
			"moved or the walk is broken; both make a clean result meaningless",
			len(scripts), rule42ScriptsRel, rule42ScriptFileFloor)
	}
	judged = len(goFiles) + len(scripts)
	var hits []rule42Hit
	for _, rel := range goFiles {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v — an unreadable Go file is not a clean one (Rule 15)", rel, err)
		}
		src := rule42GoSource(rel, string(body))
		hits = append(hits, rule42GoHits(src)...)
		reported = append(reported, rule42ReportGroupSignals(src)...)
	}
	for _, rel := range scripts {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v — an unreadable script is not a clean one (Rule 15)", rel, err)
		}
		hits = append(hits, rule42ShellHits(rule42ShellSource(rel, string(body)))...)
	}
	rejected, excused, reportedFromArms := rule42Court(hits)
	reported = append(reported, reportedFromArms...)
	sortHitsRule42(rejected)
	sortHitsRule42(excused)
	sortHitsRule42(reported)
	return rejected, excused, reported, judged
}

func sortHitsRule42(hits []rule42Hit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].rel != hits[j].rel {
			return hits[i].rel < hits[j].rel
		}
		if hits[i].line != hits[j].line {
			return hits[i].line < hits[j].line
		}
		return hits[i].kind < hits[j].kind
	})
}

// TestRule42NoBannedDestructiveSignals is the gate PROJECT.md Rule 42 names. It
// prints what it walked, judged, let through and found — so its green is a
// MEASUREMENT rather than an empty match.
func TestRule42NoBannedDestructiveSignals(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	rejected, excused, reported, judged := rule42JudgeTree(t, root)
	t.Logf("Rule 42: judged %d file(s) (%d Go from the working tree, %d under %s/), %d exempted "+
		"hit(s), %d reported group-signal site(s), %d rejected",
		judged, len(rule42GoFiles(t, root)), len(rule42ScriptFiles(t, root)), rule42ScriptsRel,
		len(excused), len(reported), len(rejected))
	for _, h := range reported {
		t.Logf("REPORTED (legitimate only when the caller made the group — not judged): %s", h)
	}
	for _, h := range excused {
		t.Logf("exempted: %s — %s", h, rule42Exemptions[h.rel][h.kind])
	}
	for _, h := range rejected {
		t.Errorf("Rule 42 violation (nothing may signal what it does not own): %s", h)
	}
}

// TestRule42PlantedViolationsTurnItRed is the red proof. It writes the banned
// forms — and, alongside them, the CLEAN controls that a text gate must leave
// alone — into a scratch tree and drives the SAME judge the real tree uses. A
// gate whose red proof is a synthetic snippet proves nothing; this one plants the
// smallest real files.
func TestRule42PlantedViolationsTurnItRed(t *testing.T) {
	dir := TestScratchDir(t, "rule42-redproof")

	planted := map[string]string{
		// one banned address per file, so a kind that stopped firing is visible.
		"planted/kill_everything.go": "package planted\n\nimport \"syscall\"\n\n" +
			"func KillEverything() { _ = syscall.Kill(-1, syscall.SIGKILL) }\n",
		"planted/own_group.go": "package planted\n\nimport \"syscall\"\n\n" +
			"func KillOwnGroup() { _ = syscall.Kill(0, syscall.SIGTERM) }\n",
		"planted/name_tool.go": "package planted\n\nimport (\n\t\"context\"\n\t\"os/exec\"\n)\n\n" +
			"func KillByName(ctx context.Context) { _ = exec.CommandContext(ctx, \"pkill\", \"-f\", \"gopi\") }\n",
		"planted/reset_reraise.go": "package planted\n\nimport (\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n)\n\n" +
			"func ResetAndReraise(sig os.Signal) {\n\tsignal.Reset(sig) // rule42-ok: this is FIXTURE TEXT inside a Go string literal, never compiled and never executed — the arm under test reads it as a planted violation, so annotating it is what keeps the two Rule 42 gates from flagging each other\n\t_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))\n}\n",
		"scripts/planted-name.sh":  "#!/usr/bin/env bash\npkill -f gopi\n",
		"scripts/planted-group.sh": "#!/usr/bin/env bash\nkill -TERM -- -1234\nkill 0\nkill -1\n",
		// Stop-then-Reset-then-self-kill. Rule 42 lists the re-raise form as banned,
		// so the gate FLAGS it; discharging it is a per-file permit with a reason
		// (the four real sites do exactly that), never a silent "reported" class.
		"planted/reset_after_stop.go": "package planted\n\nimport (\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n)\n\n" +
			"func HandBack(stop chan os.Signal, sig syscall.Signal) {\n\tsignal.Stop(stop)\n\tsignal.Reset(sig) // rule42-ok: FIXTURE TEXT inside a Go string literal, never compiled and never executed — the arm under test must SEE this banned form to prove it fires, so the two Rule 42 gates must not flag each other's fixtures\n" +
			"\t_ = syscall.Kill(os.Getpid(), sig)\n}\n",
	}

	// The CLEAN controls. Each is a form a naive pattern would flag, and a gate
	// that reddened on them would be routed around within a week.
	clean := map[string]string{
		// the ~40 legitimate group signals: the caller made the group.
		"clean/group_signal.go": "package clean\n\nimport (\n\t\"os/exec\"\n\t\"syscall\"\n)\n\n" +
			"func Stop(cmd *exec.Cmd) {\n\tcmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}\n" +
			"\t_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)\n}\n",
		// a liveness probe (`kill -0 PID`) and a single-pid signal.
		"scripts/clean.sh": "#!/usr/bin/env bash\nset -u\npid=\"$1\"\n" +
			"kill -0 \"$pid\" 2>/dev/null || exit 0\nkill -TERM \"$pid\"\nkill -KILL \"$pid\"\n",
		// prose about the ban is documentation, not an action.
		"scripts/clean-comment.sh": "#!/usr/bin/env bash\n# never run `pkill -f gopi` here — it matches the pkill's own caller\n" +
			"kill -0 \"$1\"\n",
		// A `signal.Reset` with NO self re-raise (a Cleanup restoring a disposition):
		// nothing is re-delivered, so nothing is dismantled. Must not fire.
		"clean/reset_without_reraise.go": "package clean\n\nimport (\n\t\"os/signal\"\n\t\"syscall\"\n)\n\n" +
			"func Restore() { signal.Reset(syscall.SIGINT) }\n",
		// `pgrep -f` READS the process table and signals nothing: reported, never
		// failed (a read cannot reach anyone).
		"scripts/clean-pgrep.sh": "#!/usr/bin/env bash\npgrep -f \"http.server\" >/dev/null || true\n",
	}

	for rel, body := range planted {
		writeRule42Fixture(t, dir, rel, body)
	}
	for rel, body := range clean {
		writeRule42Fixture(t, dir, rel, body)
	}

	// Judge the planted Go files and the planted scripts through ONE arm each, the
	// same predicates the gate runs.
	var hits []rule42Hit
	for rel := range planted {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		hits = append(hits, rule42GoHits(rule42GoSource(rel, readRule42Fixture(t, dir, rel)))...)
	}
	for rel := range planted {
		if !strings.HasSuffix(rel, ".sh") {
			continue
		}
		hits = append(hits, rule42ShellHits(rule42ShellSource(rel, readRule42Fixture(t, dir, rel)))...)
	}

	got := map[string]int{}
	for _, h := range hits {
		if h.rel == rule42SelfRel {
			continue
		}
		got[h.kind]++
	}
	want := map[string]int{
		rule42KindKillEverything: 1,
		rule42KindOwnGroup:       1, // planted/own_group.go
		rule42KindNamePattern:    2, // planted/name_tool.go + scripts/planted-name.sh
		rule42KindResetReraise:   1, // no Stop in the file — the c9c0534 defect shape
		rule42KindResetAfterStop: 1, // Stop first: flagged, then discharged by a per-file permit
	}
	for kind, n := range want {
		if got[kind] < n {
			t.Errorf("the planted %q form produced %d hit(s), want at least %d — the arm no longer "+
				"catches the shape it exists for, so every green above is empty (hits: %v)",
				kind, got[kind], n, hits)
		}
	}
	// scripts/planted-group.sh carries three group-addressing spellings, all of
	// the own-group kind.
	for _, h := range hits {
		if h.rel == "scripts/planted-group.sh" && h.kind != rule42KindOwnGroup {
			t.Errorf("scripts/planted-group.sh was labelled %q; a shell group address is the own-group "+
				"kind: %s", h.kind, h)
		}
	}
	if n := got[rule42KindOwnGroup]; n < 4 {
		t.Errorf("the own-group arm found %d site(s), want at least 4 (one Go literal + three shell "+
			"spellings) — a shell form went unrecognised: %v", n, hits)
	}
	// The court must reject every planted hit UNLESS an exemption names that exact
	// (file, kind) pair — the self-exemption must not launder a fixture.
	rejected, excused, courtReported := rule42Court(hits)
	if len(rejected)+len(courtReported) != len(hits) {
		t.Errorf("the court excused %d of %d planted hit(s) — an exemption is laundering a planted "+
			"violation: %v", len(excused), len(hits), excused)
	}
	if len(courtReported) != 0 {
		t.Errorf("the court classified %d planted hit(s) as REPORTED (not enforced) — a banned form must "+
			"never be reported-only: %v", len(courtReported), courtReported)
	}

	// And the controls: none may be REJECTED. A control may still be REPORTED (a
	// `pgrep -f` read, a Stop-then-Reset re-raise) — that is the point of the
	// reported class — but a false positive here gets the gate switched off, which
	// is the vacuum this file replaced.
	for rel := range clean {
		var controlHits []rule42Hit
		if strings.HasSuffix(rel, ".go") {
			controlHits = rule42GoHits(rule42GoSource(rel, readRule42Fixture(t, dir, rel)))
		} else {
			controlHits = rule42ShellHits(rule42ShellSource(rel, readRule42Fixture(t, dir, rel)))
		}
		controlRejected, _, controlReported := rule42Court(controlHits)
		if len(controlRejected) != 0 {
			t.Errorf("the gate REJECTS a legitimate form (%s): %v — a false positive here gets the gate "+
				"switched off, which is the vacuum this file replaced", rel, controlRejected)
		}
		for _, h := range controlReported {
			t.Logf("control reported, not rejected (%s): %s", rel, h)
		}
	}

	// The fixture tree carries the planted `.go` files, so the SAME walk floors the
	// real tree enforces would pass here for the wrong reason: a fixture is judged
	// by the ARMS, not by a floor.
	if len(hits) == 0 {
		t.Fatal("the planted tree produced no hit at all — the red proof is vacuous")
	}
}

// TestRule42ExemptionsAreNarrowAndReal keeps the allow-list honest in BOTH
// directions: a row whose file is gone fails, and a row whose shape no longer
// occurs fails (so the list can only ratchet down — the Rule 39 discipline).
func TestRule42ExemptionsAreNarrowAndReal(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	// The detector cannot be read as "does it still carry the shape", because its
	// shapes are its own regex literals: each kind must instead still be NAMED by
	// its canary, which is what proves the arm exists at all.
	selfCanary := map[string]string{
		rule42KindKillEverything: "Kill(-1, …)",
		rule42KindOwnGroup:       "Kill(0, …)",
		rule42KindResetReraise:   "signal.Reset",
		rule42KindResetAfterStop: "signal.Reset",
		rule42KindNamePattern:    "pkill",
	}
	paths := make([]string, 0, len(rule42Exemptions))
	for rel := range rule42Exemptions {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	t.Logf("Rule 42 exemptions: %d file(s), %d row(s)", len(paths), rule42ExemptionRows())
	for _, rel := range paths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		body, rerr := os.ReadFile(full)
		if rerr != nil {
			t.Errorf("exemption names %s, which cannot be read (%v) — a permit for a file that does not "+
				"exist is a permission nobody can review", rel, rerr)
			continue
		}
		for kind, reason := range rule42Exemptions[rel] {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("exemption %s [%s] carries no reason — an unexplained permit is the shape this "+
					"gate exists to refuse", rel, kind)
			}
			if rel == rule42SelfRel {
				canary, ok := selfCanary[kind]
				if !ok {
					t.Errorf("exemption %s [%s]: the detector has no canary for this kind, so the arm's "+
						"existence is unverified", rel, kind)
					continue
				}
				if !strings.Contains(string(body), canary) {
					t.Errorf("the detector no longer names %q for kind %q — the arm this exemption "+
						"covers is gone, so drop the exemption", canary, kind)
				}
				continue
			}
			var hits []rule42Hit
			if strings.HasSuffix(rel, ".go") {
				hits = rule42GoHits(rule42GoSource(rel, string(body)))
			} else {
				hits = rule42ShellHits(rule42ShellSource(rel, string(body)))
			}
			found := false
			for _, h := range hits {
				if h.kind == kind {
					found = true
					t.Logf("%s [%s] still occurs at line %d — %s", rel, kind, h.line, reason)
					break
				}
			}
			if !found {
				t.Errorf("exemption %s [%s] no longer matches anything — the shape it excused is gone, "+
					"so the row must be deleted (the list only ratchets down)", rel, kind)
			}
		}
	}
}

// rule42ExemptionRows counts the permit rows, for the printed census.
func rule42ExemptionRows() int {
	n := 0
	for _, kinds := range rule42Exemptions {
		n += len(kinds)
	}
	return n
}

// TestRule42GateRefusesAVacuousGreen pins the floors directly: a judge pointed at
// a directory that is NOT a checkout must fail LOUDLY rather than judge nothing
// and return clean. This is the "no tests to run" failure mode, one level down.
func TestRule42GateRefusesAVacuousGreen(t *testing.T) {
	empty := TestScratchDir(t, "rule42-vacuum")

	var fatal bool
	// A private *testing.T stand-in is deliberately NOT used: the real failure
	// path is `t.Fatalf`, and reaching into another *testing.T's failure state is
	// not something a test may do. The floors are asserted arithmetically instead.
	goFiles := len(rule42GoFiles(t, empty))
	scripts := rule42ScriptFiles(t, empty)
	if len(scripts) != 0 {
		t.Fatalf("the script walk found %d file(s) in an empty directory — the walk is not rooted where "+
			"it says it is", len(scripts))
	}
	if goFiles < rule42GoFileFloor {
		fatal = true
	}
	if len(scripts) < rule42ScriptFileFloor {
		fatal = true
	}
	if !fatal {
		t.Fatalf("an empty tree cleared both floors (%d Go files ≥ %d, %d scripts ≥ %d) — the floors "+
			"are not doing their job", goFiles, rule42GoFileFloor, len(scripts), rule42ScriptFileFloor)
	}
	t.Logf("vacuum refused: %d Go file(s) < floor %d and %d script(s) < floor %d",
		goFiles, rule42GoFileFloor, len(scripts), rule42ScriptFileFloor)
}

// writeRule42Fixture materialises one fixture file under dir.
func writeRule42Fixture(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("fixture dir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", rel, err)
	}
}

// readRule42Fixture reads one fixture file back (the judge reads FILES, not the
// strings the test happens to hold, so an encoding slip shows up).
func readRule42Fixture(t *testing.T, dir, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return string(body)
}
