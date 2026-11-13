package main

// daemon_scope_no_privilege_test.go — the exhaustive gate for the epic
// "merger cannot escape cgroup with sudo" (2026-09-14).
//
// THE DEFECT THIS GATE FREEZES: told that a detached daemon could not leave its
// login session scope, the project asked for a privilege — the daemon seam
// grew an operator hint prescribing `systemd-run --scope --unit=… <gopi argv>`
// as root, both faces' usage grew a "Privileges (which mode needs them)"
// section, and the work was dispatched as "escape cgroup requires super user".
// The privilege is NEITHER necessary NOR effective (measured 2026-09-14,
// devdoc/no-privilege-daemon.md): a daemon started under root was measured in
// the cgroup of its UNPRIVILEGED launcher's login session
// (/user.slice/user-1000.slice/session-18855.scope — a process inherits its
// cgroup from its launcher), root has no user manager of its own to migrate
// into, and the unprivileged migration is blocked by the per-uid slice's
// ownership. Privilege is a dead end, and an operator-facing prompt that
// prescribes it is worse than no prompt: it costs the whole process tree to
// root and buys nothing.
//
// WHY A SOURCE GATE AND NOT A WORDING CASE (Rule 13/14 discipline, the
// TestUnknownNode_MirrorLiteralGate precedent): the offending text is an
// operator-facing STRING, and a string that is deleted today is re-added by the
// next agent who reads the same log line and reaches the same wrong conclusion
// — unless the tree refuses to carry it. So the gate scans every production
// .go file of this package for the PRESCRIPTION SHAPE, not a single constant:
// a bare `sudo` (gopi must never tell a user to run itself privileged) and the
// phrases that actually prescribe the root route.
//
// MENTION IS NOT PRESCRIPTION, which is why the phrase list is explicit rather
// than a regex over "root": the corrected text DOES speak about privilege ("no
// privilege is needed", "Running it as root does not obtain the escape either")
// — that is the correction doing its job. What may not come back is a sentence
// that tells the operator to go get one.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// dsNoPrivSudoWord is the bare word the whole project must never spell at a
	// user: gopi runs daemons unprivileged, and a suggestion to the contrary is
	// the defect this epic exists to remove. It is deliberately a plain
	// substring scan — a prompt may not be softened into `sudo`-by-another-name
	// either.
	dsNoPrivSudoWord = "sudo"
	// dsNoPrivCorrectionFragment is the sentence the correction MUST carry: the
	// gate asserts its presence (a rule that only forbids can be satisfied by
	// deleting the whole explanation, leaving the operator with the misleading
	// line again).
	dsNoPrivCorrectionFragment = "not a privilege problem"
	// dsNoPrivHelpFragment is the same answer in the usage heads: no privilege
	// to start a daemon, which is the question the operator asked when they
	// reached for sudo.
	dsNoPrivHelpFragment = "needs NO privilege"
)

// dsNoPrivPrescriptions are the PRESCRIPTION shapes — each one is a sentence
// that sends the reader to a privilege (or to the root system scope) as the
// remedy. They are literal fragments on purpose: they are the exact text that
// shipped on fix/daemon-scope-privilege-hint, so re-adding any of them fails
// with the branch it came from.
var dsNoPrivPrescriptions = []struct {
	fragment string
	why      string
}{
	{"re-run it as root", "prescribes re-running the daemon as root (measured: inherits the launcher's cgroup — no effect)"},
	{"run it as root", "prescribes running the daemon as root (measured: no effect on the cgroup)"},
	{"as root inside a SYSTEM scope", "prescribes a root SYSTEM scope (the dead end this epic closes)"},
	{"systemd-run --scope --unit=", "prescribes the root system-scope wrapper as the remedy"},
	{"Privileges (which mode needs them)", "the usage section that taught the privilege requirement that does not exist"},
	{"Root membership alone is not enough", "the hint's premise sentence — true about cgroups, wrong as a remedy (it led straight to the system scope)"},
	{"requires super user", "states a privilege requirement for a daemon, which is false"},
	{"needs superuser", "states a privilege requirement for a daemon, which is false"},
}

// TestDaemonScopeNoPrivilege_PrescriptionsAreFlagged is the RED PROOF: the
// predicate must flag the text that actually shipped, or a green run of the
// sweep below proves nothing (the blind-gate trap internal/standards records).
func TestDaemonScopeNoPrivilege_PrescriptionsAreFlagged(t *testing.T) {
	shipped := []string{
		"gopi merger: to make the next start independent of this login session, re-run it as root inside a SYSTEM scope:\n" +
			"  /usr/bin/systemd-run --scope --unit=gopi-daemon-merger <the gopi argv>\n",
		"Privileges (which mode needs them): --daemon can need one.",
		"Root membership alone is not enough — a process inherits its cgroup from its launcher.",
		"escape cgroup requires super user",
		"this needs superuser access",
		"# ask the operator to sudo the daemon",
	}
	for _, text := range shipped {
		if hit, _ := dsNoPrivilegeOffense(text); !hit {
			t.Errorf("the gate did not flag shipped prescription text %q — a green sweep would be blind", text)
		}
	}
	// The other direction, on the text this tree now carries: the correction
	// mentions root and privilege and must NOT be flagged, or the gate forbids
	// the very explanation it exists to protect.
	for _, clean := range []string{
		"gopi worker: that is not a privilege problem: no privilege is needed to run this daemon, and none obtains the escape (measured) — the start stays unprivileged.",
		"Running it as root does not obtain the escape either (measured).",
		"a plain mkdir + a write into cgroup.procs is enough; no dbus is required",
	} {
		if hit, offense := dsNoPrivilegeOffense(clean); hit {
			t.Errorf("the gate flagged legitimate corrective text %q (offense %q)", clean, offense)
		}
	}
}

// dsNoPrivilegeOffense reports whether text carries a privilege PRESCRIPTION.
// It returns the offending fragment so a failure names the sentence, not just
// the file: an operator-facing defect is fixed by reading the sentence.
func dsNoPrivilegeOffense(text string) (bool, string) {
	if strings.Contains(text, dsNoPrivSudoWord) {
		return true, dsNoPrivSudoWord
	}
	for _, p := range dsNoPrivPrescriptions {
		if strings.Contains(text, p.fragment) {
			return true, p.fragment
		}
	}
	return false, ""
}

// TestDaemonScopeNoPrivilege_NoProductionSourcePrescribesOne is the exhaustive
// sweep: EVERY production .go file of this package is parsed and its STRING
// LITERALS searched, so the prescription cannot come back in a file this test
// does not know about (a new face, a new helper, a usage constant added
// elsewhere).
//
// WHY LITERALS AND NOT THE RAW BYTES: the criterion is what gopi PRINTS at an
// operator. A comment recording the history ("the operator asked for
// passwordless sudo, which does not work") is documentation, not a prompt, and
// a byte scan would force the explanation out of the code that needs it. Test
// files are out of scope for the same reason this gate is named after the
// package's production sources: this file's own fixtures must spell the defect
// to prove the predicate is not blind.
func TestDaemonScopeNoPrivilege_NoProductionSourcePrescribesOne(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		for _, text := range productionStringLiterals(t, f) {
			if hit, offense := dsNoPrivilegeOffense(text); hit {
				t.Errorf("%s: a string gopi can print prescribes a privilege (%q) — this project runs its daemons "+
					"unprivileged: no privilege is needed to start one, and none obtains the cgroup escape "+
					"(measured; devdoc/no-privilege-daemon.md)\n  literal: %q", f, offense, text)
			}
		}
	}
	// A sweep that read nothing passes vacuously.
	if scanned < dsNoPrivMinSources {
		t.Fatalf("the sweep scanned only %d production sources (want >= %d) — it went blind, fix it before trusting a green run",
			scanned, dsNoPrivMinSources)
	}
}

// productionStringLiterals returns every string literal in one production
// source file (parsed, not grepped: a partially-quoted line is not text gopi
// prints). A file that cannot be parsed fails the gate rather than being
// skipped silently.
func productionStringLiterals(t *testing.T, file string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("%s: unquote %s: %v", file, lit.Value, err)
		}
		out = append(out, text)
		return true
	})
	return out
}

// dsNoPrivMinSources is the floor on how many production sources the sweep must
// read (this package has dozens; the number only has to be high enough that a
// broken glob cannot pass).
const dsNoPrivMinSources = 40

// TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted pins the PRESENCE half at
// the seam's own output: a degraded start must carry the correction beside the
// measured note. Deleting the correction satisfies every "must not" above and
// would leave the operator with the misleading line alone — so the gate asserts
// the answer is there.
func TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted(t *testing.T) {
	if !strings.Contains(daemonScopeNoPrivilegeNote, dsNoPrivCorrectionFragment) {
		t.Fatalf("the correction must answer the privilege question plainly, got %q", daemonScopeNoPrivilegeNote)
	}
	if hit, offense := dsNoPrivilegeOffense(daemonScopeNoPrivilegeNote); hit {
		t.Fatalf("the correction itself prescribes a privilege (%q): %s", offense, daemonScopeNoPrivilegeNote)
	}
	// The pid is dsNoPrivUnreadablePID: this arm is the cgroup-UNREADABLE
	// degradation, and the verdict must therefore come from the DECISION
	// (dsNoteUnavailDecision is the not-escaped one), never from the cgroup of
	// whatever process happens to be running this test.
	//
	// MEASURED 2026-10-12: this call used to pass os.Getpid(), whose cgroup on
	// this host is readable — and not a login session scope — so
	// daemonScopeNoteFor took its MIDDLE branch and returned "", the fixture read
	// 0 lines and failed while the seam was doing exactly what it promises. The
	// unreadable input is what the arm is about, and it is the same const the
	// "escaped daemon is silent" case below already uses.
	lines := daemonScopeMeasureLines(daemonSpec{prog: dsNoteProg}, dsNoPrivUnreadablePID, dsNoteUnavailDecision())
	if len(lines) != 2 {
		t.Fatalf("a degraded start reads the measured note AND the correction, got %d line(s): %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "could not leave the login session scope") {
		t.Errorf("line 1 must stay the measured note, got %q", lines[0])
	}
	if !strings.Contains(lines[1], dsNoPrivCorrectionFragment) {
		t.Errorf("line 2 must be the correction, got %q", lines[1])
	}
	// The other direction: an ESCAPED daemon gets no correction. There is no
	// missing ingredient to explain away, and a "no privilege needed" line next
	// to a successful escape would invent a problem the measurement disproved.
	// The pid is deliberately one whose /proc entry cannot be read, so the case
	// is decided by the DECISION (escaped) rather than by the cgroup of the
	// process running this test.
	escaped := daemonScopeDecision{mode: daemonScopeModeCgfs, detail: "/sys/fs/cgroup/x"}
	if got := daemonScopeMeasureLines(daemonSpec{prog: dsNoteProg}, dsNoPrivUnreadablePID, escaped); len(got) != 0 {
		t.Errorf("an escaped daemon must produce no lines, got %q", got)
	}
}

// dsNoPrivUnreadablePID is a pid whose /proc/<pid>/cgroup cannot exist, so a
// measurement of it always fails. It keeps the "escaped daemon is silent" case
// independent of where this test process happens to live.
const dsNoPrivUnreadablePID = -1

// TestDaemonScopeNoPrivilege_BothFacesTellTheOperatorTheSameAnswer is the

// "both faces" half of the epic: worker and merger are ONE mechanism, so the
// privilege answer is emitted by the shared seam (nothing per-face to drift),
// and the two usage heads that teach the daemon contract carry the same
// sentence. A worker-only or merger-only correction is exactly the asymmetry
// that let the wrong answer spread in the first place.
func TestDaemonScopeNoPrivilege_BothFacesTellTheOperatorTheSameAnswer(t *testing.T) {
	for _, head := range []struct {
		name string
		text string
	}{
		{"workerUsageHead", workerUsageHead},
		// THE MERGER PAGE IS GONE (2026-09-19, devdoc/merger-retired.md): the
		// resident merger daemon was retired and its `-h` page with it, so the
		// asymmetry this gate guards can no longer arise on that page — there is
		// no merger daemon face to teach a privilege to. The remaining daemon
		// face is the worker's, and it is measured here.
	} {
		if !strings.Contains(head.text, dsNoPrivHelpFragment) {
			t.Errorf("%s must answer whether a daemon needs a privilege (missing %q)", head.name, dsNoPrivHelpFragment)
		}
		if hit, offense := dsNoPrivilegeOffense(head.text); hit {
			t.Errorf("%s prescribes a privilege (%q)", head.name, offense)
		}
		// Both faces' --daemon contract must name --require-scope as the lever
		// for an operator who needs the guarantee: the zero-privilege answer is
		// only honest if it also says what IS available.
		if !strings.Contains(head.text, daemonScopeRequireFlag) {
			t.Errorf("%s must name %s as the guarantee lever", head.name, daemonScopeRequireFlag)
		}
	}
	// The refusal is the OTHER exit of the same seam: --require-scope turns an
	// unavailable escape into a hard failure, and that is where a reader is most
	// desperate for a remedy — so it must print the same measured answer, not a
	// bare reason the reader fills in with a password. Asserted on the call site
	// (the print lives in the spawn seam, which cannot be exercised without a
	// host whose escape is unavailable, and this host's state must not decide
	// whether the operator-facing text is tested).
	spawnSrc, err := os.ReadFile("cli_daemon_spawn.go")
	if err != nil {
		t.Fatalf("read the spawn seam: %v", err)
	}
	if !strings.Contains(string(spawnSrc), "daemonScopeNoPrivilegeNote") {
		t.Error("the --require-scope refusal must carry the same no-privilege correction as a degraded start")
	}
	// The shared seam is the ONE place the correction is rendered from, so both
	// faces reach it through the same code path (assert the call, not a copy).
	src, err := os.ReadFile("cli_daemon_scope.go")
	if err != nil {
		t.Fatalf("read the seam: %v", err)
	}
	if !strings.Contains(string(src), "daemonScopeMeasureLines") {
		t.Error("the seam must render the operator's lines in one place (daemonScopeMeasureLines)")
	}
	if !regexp.MustCompile(`func daemonScopeMeasureLines\(`).Match(src) {
		t.Error("daemonScopeMeasureLines must be defined in the seam")
	}
}
