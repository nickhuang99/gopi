// worker_control_selector.go — the control face's TARGET SELECTORS
// (`--pid <n>`, `--branch <b>`).
//
// THE DEFECT THIS FILE CLOSES (epic goal, measured 2026-10-13): `gopi worker
// --stop --branch <B>` was accepted and IGNORED. `--branch` is the RESUME
// mode's ground selector (cli_worker_resume.go), and on the control face the
// word fell through to the daemon-shaped probe, which found no live daemon
// state file, printed the honest FOREGROUND note and then — having no selector
// to honour — stopped EVERY foreground worker of the profile, or none. The
// operator believed they were stopping the worker that holds ONE branch; what
// ran was the enumeration fallback. Two facts were wrong at once: the selector
// did nothing, and the command acted on targets nobody named.
//
// WHAT A SELECTOR IS HERE. `--stop`/`--restart`/`--status` act on a WORKER
// process; a selector is the operator naming WHICH one, in the vocabulary the
// rest of this verb already uses:
//
//	--pid <n>        the process, as an operator reading `--status` has it.
//	--branch <b>     the BRANCH, resolved through the ledger: the row that owns
//	                 the branch (tools.TaskForBranch — the ONE branch→row
//	                 loader, Rule 14), its claim stamp (claimed_by, the value
//	                 EVERY live-holder judgement in the tree reads), and from
//	                 that stamp the holder PID ON THIS HOST.
//
// A SELECTOR WHOSE TARGET DOES NOT EXIST IS REFUSED, NEVER DROPPED (Rule 15).
// That is the whole point: the defect was a selector that changed nothing and
// said nothing. So every arm below ends in one of two states — a pid this
// command will act on, or a named refusal:
//
//	the branch has no owner row            → named, with the ledger that answered
//	the owner row's holder is FOREIGN      → named: a peer's pid means nothing here
//	the owner row records no holder        → named (the row was never claimed)
//	the holder pid is GONE                 → named, with the row's status
//	the pid is alive but is NOT a worker   → named, with its command line
//	a selector with no action              → the argv scanner's refusal
//
// WHY --branch IS RESOLVED THROUGH THE LEDGER AND NOT BY SCANNING /proc. A
// worker's argv does not carry the branch it is working on; the ledger does
// (deliverable.branch is stamped at claim time, and the row's claim stamp names
// the executing process). Reconstructing "which branch is this worker holding"
// from /proc would be a second, weaker definition of the same fact — the
// duplicate-source defect Rules 13/14 forbid.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO: it adds no stop semantics of its
// own. A selected target is stopped by the SAME arms the unselected paths use
// (workerControlStopOne for a daemon record, workerControlStopForeground for a
// process with no record), so "stop means finish the in-flight round, then
// exit" cannot come to mean two different things depending on how the operator
// addressed the worker.

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// workerControlPIDFlag is the process selector. It is spelled here rather
	// than inline because the usage head, the refusals and the parser must name
	// ONE word (Rule 1b); the worker face's only other `--pid` is the chat/uistate
	// selector, which is a different verb's vocabulary and deliberately not shared.
	workerControlPIDFlag = "--pid"
	// workerControlPIDName is the SAME word without its operand prefix — the form
	// the argv scanner matches against, derived from the flag so the two cannot
	// drift (Rule 13: a second literal is a defect waiting to disagree).
	workerControlPIDName = "pid"

	// workerControlBranchFlag is the BRANCH selector. Its VALUE (`--branch`) comes
	// from the resume family's own constant — the two faces address a branch with
	// the same word, and re-typing it here would be the mirror Rule 14 forbids.
	workerControlBranchFlag = tools.TaskResumeGroundBranchFlag
	// workerControlBranchName is the scanned form of the same word, derived for
	// the reason above.
	workerControlBranchName = "branch"

	// workerControlPIDSyntax / workerControlBranchSyntax are the operand
	// placeholders the operator-facing strings use, so the grammar synopsis and
	// the refusals cannot drift apart.
	workerControlPIDSyntax    = "<pid>"
	workerControlBranchSyntax = workerResumeValueSyntax

	// workerControlSelectorsRefusalFmt is the TWO-ANSWERS-ONE-QUESTION refusal. A
	// pid and a branch can name different workers, and silently preferring one is
	// the degradation every other contradiction of this verb is refused for.
	// %s = the two flags that were typed.
	workerControlSelectorsRefusalFmt = "%s: %s and %s name ONE target two ways, not a filter pair — pass whichever you hold\n  `%s %s %s` or `%s %s %s`\n"

	// workerControlSelectorValueRefusalFmt is the operand refusal: a selector with
	// no value, a repeated selector, or a pid that is not a number. Each of those
	// messages OPENS with the flag it is about (that is what
	// workerControlSelectorWord reads), so this renderer adds only the program
	// prefix — one shape for every selector error, and no word twice.
	workerControlSelectorValueRefusalFmt = "%s: %v\n"

	// workerControlSelectorNoActionFragment is the refusal's wording WITHOUT its
	// operands and remedies — the words that say WHICH question was answered.
	// The renderer below is built from it (Rule 13: a second literal is a
	// defect waiting to disagree), and it is what a reader asserting "neither
	// face's answer is this one" quotes, so the route a refusal teaches can be
	// checked without re-typing the sentence.
	workerControlSelectorNoActionFragment = "names a TARGET but no ACTION"

	// workerControlSelectorNoActionFmt is the refusal for a selector that arrived
	// without an action (a bare `gopi worker --pid 12`): a target with nothing to
	// do to it is a malformed command line, not a status query by default.
	workerControlSelectorNoActionFmt = "%s: %s " + workerControlSelectorNoActionFragment + " — say what to do with it (`%s %s`, `%s %s`, `%s %s`)\n"

	// workerControlSelectorResolvedFmt reports HOW a selected pid was reached, so
	// the operator reads the chain rather than trusting the command: the branch,
	// the row that owns it, that row's status, and the holder pid. %s = program
	// name, %s = the selector as typed, %s = the row id, %s = its status, %d = pid.
	workerControlSelectorResolvedFmt = "%s: %s → row %s (%s) → holder pid %d\n"

	// workerControlSelectorPIDFmt is the same line for the `--pid` spelling, where
	// there is no row to name. %s = program name, %s = the selector as typed,
	// %s = the shape measured for that pid (foreground / daemon).
	workerControlSelectorPIDFmt = "%s: %s → %s\n"

	// workerControlSelectorNoOwnerFmt is the named "this branch has no owner row"
	// answer. It carries the ledger that answered (an unqualified "no row names
	// branch X" reads as a claim about the MACHINE, and a host holds several
	// ledgers — the same scoping TaskForBranch's own renderer applies).
	// %s = program name, %s = branch, %s = ledger path, %d = rows in that ledger.
	workerControlSelectorNoOwnerFmt = "%s: no ledger row names branch %s (ledger %s holds %d row(s)) — there is no worker for this branch HERE\n"

	// workerControlSelectorUnreadableFmt is the "I could not look" answer, kept
	// DISTINCT from "no row names it": a ledger that cannot be read is not
	// evidence of absence (Rule 15). %s = program name, %s = branch, %w = why
	// (Rule 16: both call sites are under `if err != nil`, or pass a
	// fmt.Errorf that is an error by construction — never nil, so %w cannot
	// render `%!w(<nil>)`, and errors.Is/As keep the chain).
	workerControlSelectorUnreadableFmt = "%s: the ledger could not be read for branch %s: %w\n"

	// workerControlSelectorForeignFmt is the peer-host refusal: the owner row's
	// claim stamp names ANOTHER host, where the pid names a different process
	// (or none). Acting on it here would signal a stranger. %s = program name,
	// %s = the row, %s = the foreign host, %d = its pid.
	workerControlSelectorForeignFmt = "%s: row %s is held on host %s (pid %d there) — that pid means nothing on this host; run the stop there\n"

	// workerControlSelectorUnheldFmt is the row that records no holder at all:
	// nobody has claimed it, so there is no process to address. %s = program
	// name, %s = row, %s = its status.
	workerControlSelectorUnheldFmt = "%s: row %s (%s) records NO claim stamp — no process holds this branch, so there is nothing to address\n"

	// workerControlSelectorDeadFmt is the gone-holder answer: the row still names
	// a local pid and that process is not running. %s = program name, %s = row,
	// %s = status, %d = the pid.
	workerControlSelectorDeadFmt = "%s: row %s (%s) names holder pid %d, which is GONE — no worker is running this branch\n"

	// workerControlSelectorNotWorkerFmt is the alive-but-not-a-worker refusal. A
	// recycled pid, a shell, another program: the ledger says who, /proc says
	// what, and they disagree — which is a fact the operator must see before a
	// signal is aimed at it. %s = program name, %d = pid, %s = its command line.
	workerControlSelectorNotWorkerFmt = "%s: pid %d is alive but is NOT a %s worker process (%s) — the claim stamp is stale; nothing was addressed\n"

	// workerControlSelectorPIDDeadFmt is the `--pid` spelling of the same answer.
	// %s = program name, %d = pid.
	workerControlSelectorPIDDeadFmt = "%s: pid %d is not running — nothing to address\n"

	// workerControlSelectorUnknownWordFmt names a word that LOOKS like a selector
	// but is not this face's (`--worktree` is the resume family's). Refusing it by
	// name keeps the vocabulary closed instead of letting a near-miss fall into
	// the fallback that produced the defect. %s = program name, %s = the word.
	workerControlSelectorUnknownWordFmt = "%s: %s is not a target selector of this face — use %s %s or %s %s\n"

	// workerControlShapeDaemon / workerControlShapeForeground / workerControlShapeDetached
	// are the shapes a selected pid can have, as they are rendered.
	workerControlShapeDaemon     = "a DETACHED daemon (it publishes a state record)"
	workerControlShapeForeground = "a FOREGROUND worker (no daemon record)"
	// workerControlShapeDetached names the THIRD shape: this program, running the
	// worker verb, leading its own session, with NO live state record on disk
	// (its record was cleared, or the worker runs an older binary that writes
	// none). It is a shape worth NAMING rather than a line worth leaving blank —
	// measured 2026-10-16 by the pid-consistency case in
	// worker_list_alias_test.go: this arm returned a target with an EMPTY report,
	// so `gopi worker --status --pid N` printed a blank line where the resolved
	// pid belonged — a resolved target rendered as "nothing was reached", which
	// is the one answer this face must never give silently (Rule 15).
	workerControlShapeDetached = "a DETACHED worker with no live state record"
)

// workerControlSelection is the operator's TARGET SELECTION parsed off a control
// argv — one of the two spellings, or none (the ordinary unselected face).
type workerControlSelection struct {
	// pid is `--pid <n>` (0 when unset).
	pid int
	// branch is `--branch <b>` ("" when unset).
	branch string
	// applied reports whether the operator named a target at all. It is a
	// separate field rather than `pid != 0 || branch != ""` so a future selector
	// cannot be added without answering "does this count as set?" in one place.
	applied bool
	// raw is the selector as the operator typed it, for the report line.
	raw string
}

// workerControlSelectionFrom reads the target selectors out of a control argv.
//
// It is called ONLY once the argv is known to carry a control action (the
// routing half, workerControlRequested), so a selector without an action is
// refused by the caller rather than silently dropped here — see
// workerControlSelectorWithoutAction.
func workerControlSelectionFrom(args []string) (workerControlSelection, error) {
	var sel workerControlSelection
	seenPID, seenBranch := "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value := workerControlSelectorNameValue(a)
		switch name {
		case workerControlPIDName:
			if seenPID != "" {
				return workerControlSelection{}, fmt.Errorf("%s repeated (given twice)", workerControlPIDFlag)
			}
			if value == "" {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
					return workerControlSelection{}, fmt.Errorf("%s needs a value (`%s %s`)", workerControlPIDFlag, workerControlPIDFlag, workerControlPIDSyntax)
				}
				value = args[i+1]
				i++
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 {
				return workerControlSelection{}, fmt.Errorf("%s %q is not a positive process id", workerControlPIDFlag, value)
			}
			sel.pid, sel.raw, seenPID = n, workerControlPIDFlag+" "+value, value
		case workerControlBranchName:
			if seenBranch != "" {
				return workerControlSelection{}, fmt.Errorf("%s repeated (given twice)", workerControlBranchFlag)
			}
			if value == "" {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
					return workerControlSelection{}, fmt.Errorf("%s needs a value (`%s %s`)", workerControlBranchFlag, workerControlBranchFlag, workerControlBranchSyntax)
				}
				value = args[i+1]
				i++
			}
			if strings.TrimSpace(value) == "" {
				return workerControlSelection{}, fmt.Errorf("%s %q is not a branch name", workerControlBranchFlag, value)
			}
			sel.branch, sel.raw, seenBranch = value, workerControlBranchFlag+" "+value, value
		}
	}
	sel.applied = sel.pid != 0 || sel.branch != ""
	if sel.pid != 0 && sel.branch != "" {
		return workerControlSelection{}, errTwoSelectors
	}
	return sel, nil
}

// errTwoSelectors is the two-answers-one-question error; the caller renders it
// with the refusal sentence (the message carries no prog prefix of its own,
// Rule 2: ONE renderer per refusal class).
var errTwoSelectors = fmt.Errorf("both selectors")

// workerControlSelectedFlagWord reports whether an argv word OPENS a selector
// (either spelling, bare or `=`). It is the routing test: it is what keeps a
// near-miss (`--worktree`) from being silently ignored.
func workerControlSelectedFlagWord(a string) bool {
	name, _ := workerControlSelectorNameValue(a)
	return name == workerControlPIDName || name == workerControlBranchName
}

// workerControlSelectorLikeWord reports whether a word is spelled like a
// selector of this family but is not one of its members — `--worktree`, the
// sibling ground selector. It exists so the face can name the near-miss instead
// of dropping it (Rule 15).
func workerControlSelectorLikeWord(a string) bool {
	name, _ := workerControlSelectorNameValue(a)
	switch name {
	case "worktree", "task-id", "id":
		return true
	}
	return false
}

// workerControlSelectorNameValue splits a word into a flag NAME and an
// `--flag=VALUE` operand. A word that is not `--`-prefixed has no name.
func workerControlSelectorNameValue(a string) (name, value string) {
	if !strings.HasPrefix(a, "--") {
		return "", ""
	}
	name = strings.TrimPrefix(a, "--")
	if eq := strings.Index(name, workerFlagValueSep); eq >= 0 {
		value = name[eq+1:]
		name = name[:eq]
	}
	return name, value
}

// workerControlTarget is a resolved target: the pid this command will address,
// plus the chain that produced it (rendered before anything acts, so the
// operator can see WHICH worker the selector reached).
type workerControlTarget struct {
	pid int
	// daemon is true when a live daemon state record names this pid: it chooses
	// the stop ARM (workerControlStopOne) and the shape line.
	daemon bool
	// report is the chain line for the operator (already formatted).
	report string
}

// workerControlResolveSelection turns a parsed selection into the pid(s) it
// addresses. It NEVER answers "no target" by falling back to the enumeration:
// an unresolvable selector is an error, and the caller exits non-zero with it
// (Rule 15 — the defect was a selector that quietly changed nothing).
func workerControlResolveSelection(sel workerControlSelection, dir string) ([]workerControlTarget, error) {
	if !sel.applied {
		return nil, nil // unselected: the callers keep their own routes
	}
	if sel.pid != 0 {
		return workerControlResolvePID(sel, dir)
	}
	return workerControlResolveBranch(sel, dir)
}

// workerControlResolvePID validates the operator's own pid: it must be the
// worker shape this verb addresses, or the refusal says what it actually is.
func workerControlResolvePID(sel workerControlSelection, dir string) ([]workerControlTarget, error) {
	t, ok := workerControlWorkerShape(sel.pid, dir)
	if ok {
		return []workerControlTarget{t}, nil
	}
	if !tools.PidAlive(sel.pid) {
		return nil, fmt.Errorf(workerControlSelectorPIDDeadFmt, workerProgTopLevel, sel.pid)
	}
	return nil, fmt.Errorf(workerControlSelectorNotWorkerFmt, workerProgTopLevel, sel.pid,
		binaryNameGopi, workerCommandLineOrUnknown(sel.pid))
}

// workerControlResolveBranch resolves `--branch B` through the ledger.
func workerControlResolveBranch(sel workerControlSelection, dir string) ([]workerControlTarget, error) {
	lookup, err := tools.TaskForBranch(tools.TaskForBranchQuery{Branch: sel.branch}, "")
	if err != nil {
		return nil, fmt.Errorf(workerControlSelectorUnreadableFmt, workerProgTopLevel, sel.branch, err)
	}
	if !lookup.Readable {
		// "I could not look" must never render as "there is nothing there"
		// (Rule 15b): the ledger was not read, so this command has NO evidence
		// about the branch and says exactly that instead of refusing the
		// operator's target on the strength of a failed read.
		return nil, fmt.Errorf(workerControlSelectorUnreadableFmt, workerProgTopLevel, sel.branch,
			fmt.Errorf("ledger %s was not readable", lookup.LedgerPath))
	}
	if lookup.LedgerRowsTotal == 0 {
		return nil, fmt.Errorf(workerControlSelectorNoOwnerFmt, workerProgTopLevel, sel.branch, lookup.LedgerPath, lookup.LedgerRowsTotal)
	}
	owner, ok := tools.TaskForBranchResolvedRow(lookup)
	if !ok {
		return nil, fmt.Errorf(workerControlSelectorNoOwnerFmt, workerProgTopLevel, sel.branch, lookup.LedgerPath, lookup.LedgerRowsTotal)
	}
	host, pid, local := tools.ClaimHolderPID(owner.ClaimedBy)
	if strings.TrimSpace(owner.ClaimedBy) == "" {
		return nil, fmt.Errorf(workerControlSelectorUnheldFmt, workerProgTopLevel, owner.TaskID, owner.Status)
	}
	if !local {
		return nil, fmt.Errorf(workerControlSelectorForeignFmt, workerProgTopLevel, owner.TaskID, host, pid)
	}
	if !tools.PidAlive(pid) {
		return nil, fmt.Errorf(workerControlSelectorDeadFmt, workerProgTopLevel, owner.TaskID, owner.Status, pid)
	}
	t, ok := workerControlWorkerShape(pid, dir)
	if !ok {
		return nil, fmt.Errorf(workerControlSelectorNotWorkerFmt, workerProgTopLevel, pid,
			binaryNameGopi, workerCommandLineOrUnknown(pid))
	}
	t.report = fmt.Sprintf(workerControlSelectorResolvedFmt, workerProgTopLevel, sel.raw,
		owner.TaskID, owner.Status, pid)
	return []workerControlTarget{t}, nil
}

// workerControlWorkerShape classifies one pid as the worker this face addresses,
// or refuses (ok=false) when it is not one. The daemon record is checked FIRST:
// a daemon pid is also `gopi worker`-shaped, and the caller needs to know which
// arm to use.
func workerControlWorkerShape(pid int, dir string) (workerControlTarget, bool) {
	for _, f := range workerLiveStateFiles(dir) {
		if f.PID == pid {
			return workerControlTarget{pid: pid, daemon: true,
				report: fmt.Sprintf(workerControlSelectorPIDFmt, workerProgTopLevel,
					workerControlPIDFlag+" "+strconv.Itoa(pid), workerControlShapeDaemon)}, true
		}
	}
	if workerForegroundShape(pid) {
		return workerControlTarget{pid: pid,
			report: fmt.Sprintf(workerControlSelectorPIDFmt, workerProgTopLevel,
				workerControlPIDFlag+" "+strconv.Itoa(pid), workerControlShapeForeground)}, true
	}
	if tools.PidAlive(pid) && workerGopiImageWorker(pid) {
		// Alive, our program, `worker` in argv[1], but it leads its own session
		// with no state record: a detached process whose record was cleared. It
		// is still a worker, and the FILE channel addresses it by pid — the
		// conservative direction (the request is read at its claim boundary).
		// The SHAPE is named in the report: a resolved target that renders as an
		// empty line would read as "the selector reached nothing", which is the
		// one answer this face must never give silently (Rule 15).
		return workerControlTarget{pid: pid,
			report: fmt.Sprintf(workerControlSelectorPIDFmt, workerProgTopLevel,
				workerControlPIDFlag+" "+strconv.Itoa(pid), workerControlShapeDetached)}, true
	}
	return workerControlTarget{}, false
}

// workerGopiImageWorker is the lenient half of the shape test: the process IS
// this program and IS running the worker verb, whatever its session shape. It
// is deliberately weaker than workerForegroundShape (which also requires the
// ABSENCE of detachment) because `--pid`/`--branch` name a target the operator
// has already identified — the question here is only "is it our worker?".
func workerGopiImageWorker(pid int) bool {
	root := procnameRoot()
	argv, ok := procname.Cmdline(root, strconv.Itoa(pid))
	if !ok || len(argv) < 2 || argv[1] != procname.VerbWorker {
		return false
	}
	return workerGopiImage(root, pid, argv[0])
}

// workerControlStopSelected stops the targets a selector resolved, using the
// SAME arms the unselected paths use: a daemon record goes through
// workerControlStopOne (file + signal, version-checked), a process without one
// through workerControlStopForeground. Returns true when the requested end
// state holds for every target.
func workerControlStopSelected(targets []workerControlTarget, dir string, bound time.Duration, force bool) bool {
	report := len(targets) > 1 || targets[0].report != ""
	if report {
		for _, t := range targets {
			if t.report != "" {
				fmt.Println(t.report)
			}
		}
	}
	ok := true
	var foreground []int
	for _, t := range targets {
		if !t.daemon {
			foreground = append(foreground, t.pid)
			continue
		}
		st, found := workerStateForPID(dir, t.pid)
		if !found {
			foreground = append(foreground, t.pid) // the record went away: the process arm still applies
			continue
		}
		if !workerControlStopOne(st, bound, force) {
			ok = false
		}
	}
	if len(foreground) > 0 && !workerControlStopForeground(foreground, bound, force) {
		ok = false
	}
	return ok
}

// workerStateForPID finds the live state record naming pid, if there is one.
func workerStateForPID(dir string, pid int) (workerStateFile, bool) {
	for _, f := range workerLiveStateFiles(dir) {
		if f.PID == pid {
			return f, true
		}
	}
	return workerStateFile{}, false
}

// workerControlSelectionRefusal renders the selector-class refusals that are
// decided on the ARGV (before anything is resolved), so the caller has ONE
// place that turns a selector error into the operator's sentence + exit code.
func workerControlSelectionRefusal(err error) string {
	if err == errTwoSelectors {
		return fmt.Sprintf(workerControlSelectorsRefusalFmt, workerProgTopLevel,
			workerControlPIDFlag, workerControlBranchFlag,
			workerProgTopLevel, workerControlPIDFlag, workerControlPIDSyntax,
			workerProgTopLevel, workerControlBranchFlag, workerControlBranchSyntax)
	}
	return fmt.Sprintf(workerControlSelectorValueRefusalFmt, workerProgTopLevel, err)
}

// workerControlSelectorWord names the selector an error is about. Every
// selector error message OPENS with the flag it is about (one shape, so this
// reader is a prefix test rather than a search): the sentence the operator sees
// therefore starts with the word they typed, which is what makes a refusal
// actionable. An error that does not open with a flag is a programming
// oversight, and the fallback names BOTH so it cannot pass for a specific one.
func workerControlSelectorWord(err error) string {
	msg := err.Error()
	for _, f := range []string{workerControlPIDFlag, workerControlBranchFlag} {
		if strings.HasPrefix(msg, f+" ") {
			return f
		}
	}
	return workerControlPIDFlag + "/" + workerControlBranchFlag
}

// workerControlSelectionGate is the ROUTER's one call: it decides, from the
// argv and the action the control parser found, whether this invocation carries
// a target selection, whether that selection is malformed, and whether the word
// it was built from belongs to THIS face at all.
//
// Four outcomes, and each is a different sentence to the operator:
//
//	refuse        a selector that cannot be read (no value, a repeated flag, a
//	              non-numeric pid) or that arrived with NO action — both are
//	              malformed command lines, refused in the usage class;
//	applied       a well-formed selector beside an action: the caller routes to
//	              the selected arm, which resolves the target and REFUSES a
//	              selector that names nothing (never the enumeration fallback);
//	deferred      a selector that is ANOTHER face's vocabulary and arrived
//	              without the action that would have made it this face's
//	              (workerControlDefersToTheResumeFace): the caller must let the
//	              owning face answer, NOT this one — see that predicate for why
//	              the deferral is a conjunction rather than "a branch is set";
//	none          no selector: the ordinary unselected routes.
func workerControlSelectionGate(args []string, action workerControlAction) (sentence string, refuse, applied, deferred bool) {
	sel, err := workerControlSelectionFrom(args)
	if err != nil {
		return workerControlSelectionRefusal(err), true, false, false
	}
	if !sel.applied {
		return "", false, false, false
	}
	if action == "" {
		if workerControlDefersToTheResumeFace(action, sel) {
			return "", false, false, true
		}
		return fmt.Sprintf(workerControlSelectorNoActionFmt, workerProgTopLevel, sel.raw,
			workerProgTopLevel, workerControlStatus,
			workerProgTopLevel, workerControlStop,
			workerProgTopLevel, workerControlRestart), true, false, false
	}
	return "", false, true, false
}

// workerControlDefersToTheResumeFace reports whether a parsed selection with no
// action is the RESUME face's command line rather than this one's — the ONE
// argv shape where a selector word arrives at this gate and this gate must NOT
// answer it.
//
// WHY IT MUST DEFER (measured 2026-10-13, `gopi worker --branch feat/x`). The
// branch word is the resume family's GROUND selector — this file reads it under
// that family's own constant (workerControlBranchFlag =
// tools.TaskResumeGroundBranchFlag), and the resume face refuses it on its own
// when the mode switch is missing (cli_worker_resume.go, workerResumeOnlyFlag:
// "`--branch` is the GROUND of a resume — it means nothing without `--resume`").
// That sentence TEACHES the repair. Answering first with this face's
// aimless-target refusal sent the operator to `--status`/`--stop`/`--restart` —
// three actions that cannot start a resume — so the route the gate recommended
// was the route the resume face then refused again. Two refusals for one typo,
// and the wrong one first.
//
// THE DISCRIMINATOR IS A CONJUNCTION, and every half is load-bearing:
//
//	action == ""       WITH an action, `--branch B` is this face's SELECTION
//	                   (`--stop --branch B`): the caller's applied arm, which
//	                   resolves the branch through the ledger. A deferral there
//	                   would hand the word back to the face that has no stop.
//	sel.pid == 0       `--pid` is this face's OWN selector — no other face reads
//	                   it — so a bare `--pid 12` keeps this face's refusal
//	                   ("names a TARGET but no ACTION"), which is the only
//	                   correct sentence for it.
//	sel.branch != ""   the word that is left is the resume family's ground
//	                   selector, i.e. the one word the two faces SHARE and whose
//	                   bare spelling the resume family owns.
//
// IT IS DELIBERATELY NOT "a branch is set": that single test also swallows the
// `--pid` case above (it is blind to WHICH word the operator typed and would
// defer every bare selector to a face that cannot refuse a pid), which is the
// shape that reddens the union case of
// TestWorkerControlSelectorDeferralDiscriminator.
//
// `--resume` ITSELF CANNOT REACH HERE: workerControlRequested yields the branch
// word when the mode switch rides along, so `gopi worker --resume --branch B`
// was routed to the resume face before this gate was called at all — this
// predicate would be answering about an argv it never sees.
func workerControlDefersToTheResumeFace(action workerControlAction, sel workerControlSelection) bool {
	return action == "" && sel.pid == 0 && sel.branch != ""
}

// workerControlEmitSelectionRefusal prints a selector refusal and exits with the
// usage class: every word was known, its VALUE (or the action beside it) was not
// — the same class workerConflictExitCode covers for a contradictory argv.
func workerControlEmitSelectionRefusal(err error) {
	fmt.Fprint(os.Stderr, workerControlSelectionRefusal(err))
	os.Exit(workerConflictExitCode)
}

// workerControlSelected is the SELECTED face: the operator named a target, so
// this invocation touches that target and nothing else.
//
// IT IS THE WHOLE POINT OF THE EPIC that a selector cannot silently decay into
// the enumeration fallback: every arm here either acts on the resolved pid or
// exits NON-ZERO with a sentence naming why the selector reached nothing (a
// branch with no owner row, a foreign holder, a dead holder, a live pid that is
// not a worker, a ledger that could not be read). There is deliberately no
// branch of this function that says "I could not resolve it, so I did the old
// thing".
//
// --status is the read-only action: it reports the resolved pid and returns 0
// without stopping anything (the same lock-free, side-effect-free contract the
// unselected status face has).
func workerControlSelected(sel workerControlSelection, dir string, bound time.Duration, o workerControlOptions) {
	targets, err := workerControlResolveSelection(sel, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(workerControlExitFailed)
	}
	switch o.action {
	case workerActionStatus:
		for _, t := range targets {
			fmt.Println(t.report)
		}
		os.Exit(workerControlExitOK)
	case workerActionStart:
		// `--start --branch B` is a contradiction, not a start: starting is
		// addressed by --daemon and the singleton lock, and a selector here
		// would be an option the command does not read. Refused by name (a
		// SELECTED start is not a supported shape) rather than ignored.
		fmt.Fprintf(os.Stderr, "%s: %s names a target, but %s starts a worker and is addressed by no target — drop the selector\n",
			workerProgTopLevel, sel.raw, workerControlStart)
		os.Exit(workerConflictExitCode)
	case workerActionRestart:
		if !workerControlStopSelected(targets, dir, bound, o.force) {
			os.Exit(workerControlExitFailed)
		}
		// The replacement daemon is the ordinary start path, and it is spawned
		// with the SAME argv the operator typed: workerControlStartDaemon's own
		// filter (workerDaemonLoopArgs) keeps only loop flags, so the selector
		// words and the restart action are dropped there by the ONE existing
		// rule rather than by a second pruning list here (Rule 13/14).
		workerControlEnsureDaemonOrExit(dir, os.Args[1:], 0)
		workerReportSpawnedDaemon(dir, 0, "")
	default: // workerActionStop / workerActionForce
		if !workerControlStopSelected(targets, dir, bound, o.force) {
			os.Exit(workerControlExitFailed)
		}
	}
	os.Exit(workerControlExitOK)
}
