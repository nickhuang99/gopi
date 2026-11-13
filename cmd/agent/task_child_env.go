package main

// task_child_env.go — the EXECUTION-CONTEXT HYGIENE of the executor child
// spawn seam (daemon-context-isolation epic, 2026-09-13).
//
// WHY THIS EXISTS: the child every task runs as is spawned from a process
// that may be a RESIDENT DAEMON (the worker loop, the merger's resolve/verify
// path). That daemon was itself launched from SOMEWHERE — commonly an agent
// shell, which exports gopi's agent-session handle (config.AgentSessionEnv)
// and the agent-chain anchor (config.AgentSessionAncestorEnv) beside it. A
// child that inherits that pair starts its life inside a foreign execution
// context: the handle names a session that is not its own, and the anchor
// makes every process below it read as a DESCENDANT of that agent session
// rather than as the fresh session it is. The environment is how an execution
// context travels, so the isolation belongs at the ONE place the child's
// environment is assembled — here.
//
// The strip is a SUBTRACTION from the launch environment, never a rebuilt
// whitelist: a task child legitimately inherits GOPI_HOME / GOPI_MEMORY_DB /
// the model credentials / PATH, and a whitelist would silently drop the next
// variable someone adds. Only the execution-context names go
// (tools.DaemonContextEnvVars — one list, Rule 14).
//
// WHAT IS DELIBERATELY NOT STRIPPED (each for its own reason, so a future
// reader does not "complete" the list):
//
//   - GOPI_TASK_WRAPPER_ID / GOPI_TASK_RUN_ID / GOPI_TASK_RUN_OUTCOME: the
//     wrapper-child identity — the child's auto-register gate (it must not
//     mint a mirror row for a run that already has one) and its SIGPIPE
//     latch. They are set FOR the child by this very seam.
//   - GOPI_TASK_WORKDIR: the workdir handoff, re-set by the seam.
//   - GOPI_TASK_CHILD_SESSION: a PARENT-side read. The child MINTS this value
//     (previously `task run` read the child's session id and exported it into
//     its own environment for the writeback); stripping it from the child's
//     environment changes nothing for the child and protects nothing — the
//     leak that matters travels DOWN, not up.
//   - GOPI_WORKER_* / GOPI_MERGE_RESOLVE_*: loop configuration. A child that
//     is itself going to start a worker (a task whose deliverable IS a
//     runner) legitimately reads it; it is configuration, not identity.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// Rule 1 constants for the seam's two decisions (see cli_tasks.go): the
// message shapes live here, never as inline literals at the call site, so a
// log reader and the tests that assert the record agree on one spelling.
const (
	// taskCwdSourceRepo is the resolution outcome "the task's own deliverable
	// repo" — the project whose GOPI.md/guardrails/build contract the executor
	// lives under.
	taskCwdSourceRepo = "deliverable repo"
	// taskCwdSourceLaunch is the resolution outcome "the workdir this process
	// was launched in" — the PRE-EPIC behaviour, kept as the documented
	// fallback for a row that carries no deliverable.repo.
	taskCwdSourceLaunch = "launch workdir (no deliverable repo ground)"
	// taskCwdSourceTaskGround is the resolution outcome "the spawner already
	// stands in the task's own ground" — the launch workdir IS the row's
	// repo/worktree, so it is kept as it is (both the project contract and
	// the branch authority the deliverable backfill reads).
	taskCwdSourceTaskGround = "launch workdir (already the task's own ground)"
	// taskCwdResolvedFmt renders the one-line record of which directory the
	// executor stands in and WHY. The fallback is not optional decoration: a
	// task silently running under the daemon's project instead of its own is
	// the defect this epic removes, so the fallback is as loud as the
	// resolution (Rule 15).
	taskCwdResolvedFmt = taskProgName + ": executor cwd → %s (task %s: %s)\n"
	// taskGroundLineFmt is the ONE spelling of the CHILD'S GROUND record (epic
	// worker-creates-child-ground, 2026-09-22): the directory the row's ground
	// resolved to, and whether this run CREATED it or REUSED it (the detail
	// also names the branch). It is the second of the two cwd decisions, and
	// the record that follows it (taskCwdResolvedFmt, written after the ground
	// resolution) names the directory the child actually starts in — so an
	// operator reads the ground first and then the placement (Rule 15).
	taskGroundLineFmt = taskProgName + ": child ground → %s (task %s: %s)\n"
	// taskGroundBranchUnpublishedFmt is the record of an announcement that was
	// DELIBERATELY NOT MADE: the run's ground resolved, but neither the row's
	// derivation nor the recorded branch produced a value, so
	// GOPI_RUN_GROUND_BRANCH is left unset rather than exported empty (an
	// exported "" claims the ground has no branch; no measurement established
	// that). Rule 15 — the absence is stated, never silent.
	taskGroundBranchUnpublishedFmt = taskProgName + ": child ground %s on branch: NOT ANNOUNCED (task %s has no derivable ledger address and the ground records no branch) — %s stays unset"
	// taskGroundAnnounceLineFmt is the ONE spelling of the record that the seam
	// ANNOUNCED the ground to the child (epic ground-announcement, 2026-09-25).
	// It is the operator-facing half of the announcement: the clause itself goes
	// into the child's briefing, and this line is what lets a later reader of
	// the round log tell "the child was told" from "the child was not" —
	// without it the fix would be invisible in exactly the artifact an evidence
	// reader has (the round log), which is the shape of the original defect.
	taskGroundAnnounceLineFmt = taskProgName + ": child ground ANNOUNCED to the run (task %s: dir %s branch %s)\n"
	// taskBriefingPremiseLineFmt is the ONE spelling of the record that the
	// briefing's stale premises were re-measured and a clause was APPENDED (epic
	// briefing-premise-freshness, 2026-10-12). The per-claim lines (rendered by
	// tools.TaskBriefingPremiseRefresh) carry the measurements; this line is the
	// seam's own note that the child's briefiing was changed, and it names the
	// ground the measurement was taken against — a premise measured against the
	// wrong tree would be a different verdict, so the tree is part of the record.
	taskBriefingPremiseLineFmt = taskProgName + ": briefing premise RE-MEASURED at dispatch (task %s: ground %s, integration branch %s — a stale premise was stamped into the child's briefing)\n"
	// taskBriefingBaseLineFmt is the SAME record for the base-revision half of
	// the seam (tools.TaskBriefingPremiseBaseRefresh), spelled separately
	// because it reports the other measurement: that half never claims the work
	// is done, only that the briefing's own base is behind the branch — so a
	// reader grepping the run's facts for "premise RE-MEASURED" and for "base
	// RE-MEASURED" learns WHICH of the two fired, and a run stamped by both says
	// so twice rather than once.
	taskBriefingBaseLineFmt = taskProgName + ": briefing BASE RE-MEASURED at dispatch (task %s: ground %s, integration branch %s — a base behind the tip was stamped into the child's briefing; re-measure the surface before repairing it)\n"
	// taskBriefingCountLineFmt is the SAME record for the THIRD half of the seam
	// (tools.TaskBriefingCountRefresh, epic 01a137cb, 2026-10-14), spelled
	// separately because it reports a third measurement: a population FIGURE the
	// briefing quotes and this ground reads differently. It names no integration
	// branch — the comparison is against THIS GROUND's own files, not against a
	// ref — so a reader learns from the line itself what was compared with what.
	taskBriefingCountLineFmt = taskProgName + ": briefing FIGURE RE-MEASURED at dispatch (task %s: ground %s — a quoted population figure this ground reads differently was stamped into the child's briefing; re-measure the figure before repairing it)\n"
	// taskBriefingPathSetLineFmt is the SAME record for the FOURTH half of the
	// seam (tools.TaskBriefingPathSetRefresh, epic 01a1447c's incident,
	// 2026-10-16). It is spelled separately because it reports a different
	// reading from its neighbours: the briefing pins a POPULATION OF PATHS and
	// this seam did NOT re-derive it (that would cost a classifier pass on every
	// dispatch), so the line says the set was NOT re-measured rather than claiming
	// a measurement a reader could take for evidence.
	taskBriefingPathSetLineFmt = taskProgName + ": briefing PINNED PATH SET examined at dispatch (task %s: ground %s — a pinned population of paths was NOT re-measured and the child's briefing now carries the instruction to re-measure the SET before working off it)\n"
	// taskBriefingGateAssertLineFmt is the SAME record for the FIFTH half of the
	// seam (tools.TaskBriefingGateAssertRefresh, epic 01a14242, 2026-10-16),
	// spelled separately because it reports a fourth measurement: an assertion of
	// the shape "<surface> is RED on <integration branch>", whose measurement
	// moment is not in its text at all and was recovered from the ROW's own
	// authoring time. It names the row as well as the ground, because the anchor
	// that produced the reading is a property of the ROW — a reader must be able
	// to see which row's age was measured, not only which tree it was measured
	// against.
	taskBriefingGateAssertLineFmt = taskProgName + ": briefing GATE ASSERTION anchored at dispatch (task %s: ground %s, integration branch %s — an assertion written at the row's own authoring time was stamped into the child's briefing; re-measure the surface before repairing it)\n"
	// taskBriefingRunAltLineFmt is the SAME record for the SIXTH half of the
	// seam (tools.TaskBriefingRunAlternativesRefresh, bug row 01a18f76,
	// 2026-10-31), spelled separately because it reports a different reading
	// from every neighbour: those re-measure what the briefing ASSERTS about the
	// tree, while this one re-measures what the briefing SELECTS in it — a
	// `go test -run` alternative that reaches no test function and would make
	// `go test` print `no tests to run` and exit 0. A reader of the round log
	// can therefore tell "the child was told its acceptance selector is dead"
	// from "no selector was judged" without reading the child's prompt.
	taskBriefingRunAltLineFmt = taskProgName + ": briefing ACCEPTANCE SELECTOR judged at dispatch (task %s: ground %s — a `go test -run` alternative reaching NO test function in its package was stamped into the child's briefing; replace the dead name or drop the alternative)\n"
	// taskBriefingProseTestLineFmt is the SAME record for the SEVENTH half of the
	// seam (tools.TaskBriefingProseTestNamesRefresh, bug row 01a18f86,
	// 2026-10-31). It is spelled separately because it reports the OTHER shape of
	// the same defect: the sixth clause reads a `go test -run` SELECTION, while
	// this one reads a name written in a SENTENCE. The two records must be
	// distinguishable, because a reader of the round log has to know whether the
	// child was told its command selects nothing or its prose cites a name that
	// exists nowhere — the repairs are different (re-write the command vs find or
	// write the test).
	taskBriefingProseTestLineFmt = taskProgName + ": briefing PROSE-NAMED TEST judged at dispatch (task %s: ground %s — a `Test…` identifier the briefing writes as acceptance evidence reaches NO test function in this ground, and was stamped into the child's briefing; name the test that exists or write the one you mean)\n"
	// taskBriefingRunAltPartialLineFmt is the SAME record for the EIGHTH half of
	// the seam (tools.TaskBriefingRunAltPartialRefresh, this epic's own case).
	// It is spelled separately from taskBriefingRunAltLineFmt above because it
	// reports the OTHER rung of the same defect and the two repairs differ: the
	// sixth clause reports a selector that reaches NOTHING (`go test` prints
	// `no tests to run`), while this one reports a selector that reaches
	// SOMETHING — the command really runs, really passes, really exits 0, and the
	// green it prints covers only the live half. A reader of the round log has to
	// be able to tell those apart, because only the second shape is invisible to
	// every other instrument (no marker, and `scripts/test-budget.sh`'s exit 5
	// fires only for a run that executed ZERO tests).
	taskBriefingRunAltPartialLineFmt = taskProgName + ": briefing ACCEPTANCE SELECTOR partially live at dispatch (task %s: ground %s — a `go test -run` alternative reaching NO test function was stamped into the child's briefing while its siblings run, so the command's `ok`/PASS covers only those; replace the dead alternatives or drop them)\n"
	// taskBriefingAuthoredUnreadLineFmt is the STATED arm for the anchor itself
	// (Rule 15): a row whose created_at could not be read leaves every gate
	// assertion UNMEASURED rather than measured against an invented moment, and
	// this line is what makes that silence audible at the seam.
	taskBriefingAuthoredUnreadLineFmt = taskProgName + ": briefing GATE ASSERTION anchor UNAVAILABLE (task %s: the row's authoring time could not be read — %v; nothing was anchored and nothing was stamped)\n"
	// taskGroundCutPointLineFmt is the ONE spelling of the record that the
	// briefing now NAMES THE BASELINE it must be measured against (epic
	// 01a12d66, 2026-10-12). Like the premises line above it, it is the seam's
	// own note that the child's briefing was changed, and it names the ground
	// the cut point was read from — a baseline read off another tree would be a
	// different number, so the tree is part of the record. The measurement
	// itself rides the tools-side record line (tools.TaskGroundCutPointClause's
	// second return value): one sentence for the decision, one line per
	// measurement, neither re-typed here.
	taskGroundCutPointLineFmt = taskProgName + ": briefing baseline NAMED at dispatch (task %s: ground %s, branch %s — the cut point was appended to the child's briefing so a criterion phrased against the integration branch is re-based)\n"
	// taskBriefingMasterReadingLineFmt is the SAME record for the FOURTH half of
	// the seam (tools.TaskBriefingMasterReadingClause, epic 01a13af7,
	// 2026-10-14), spelled separately because it reports a fourth measurement:
	// the two refs the word "master" can denote in this tree, and the distance
	// between them. The cut-point line above names the branch a run was CUT at;
	// this one names the refs a criterion phrased against "master" must choose
	// between, so a reader grepping the run's facts learns WHICH ambiguity was
	// stamped into the child's briefing rather than only that one was.
	taskBriefingMasterReadingLineFmt = taskProgName + ": briefing MASTER READING NAMED at dispatch (task %s: ground %s, branch %s vs %s — the two refs a \"%s version\" criterion can mean were appended to the child's briefing so the criterion names one)\n"
	// taskGroundLineLabel is the leading text of the child's-ground record
	// (taskGroundLineFmt below) — spelled here so a test can assert the seam
	// RECORDED the ground it resolved without re-typing the whole format.
	taskGroundLineLabel = "child ground →"
	// taskCwdSourceGround is the resolution outcome "the row's GROUND, resolved
	// or created by this spawn" — the source label of the ordinary repo-aware
	// run once the worker owns the ground.
	taskCwdSourceGround = "row's own linked worktree (the worker created or reused it)"
	// taskEnvStripFmt renders the strip record: WHICH execution-context names
	// the spawning process carried, so an operator can tell "nothing leaked"
	// from "nothing was checked".
	taskEnvStripFmt = taskProgName + ": child env stripped %v (the spawning process's own execution context must not reach task %s)\n"
	// taskEnvStripNoneLine is printed when the spawning process carried NONE
	// of the names — the positive record of an already-clean environment.
	taskEnvStripNoneLine = taskProgName + ": child env is already free of the daemon execution context\n"
	// taskProgName prefixes the cwd-escape / fallback lines this seam prints
	// (daemoncwd.ReportFmt / FallbackFmt), so an operator greps one spelling
	// for everything the spawn seam decided.
)

// taskChildExecEnv assembles the executor child's environment: the launch
// environment minus the execution-context names, minus the task-REPORT
// contract pair, plus the record of what was removed (the caller reports it —
// Rule 15: a silent isolation strip hides which identity was inherited).
//
// The report pair is subtracted UNCONDITIONALLY here, unlike the conditional
// strip the bash/job shells use (internal/tools/childenv.go): this seam is
// about to append the CHILD's own pair just below, and the child is a
// DIFFERENT run — an inherited path must never survive as the destination a
// nested process could still read before the child's own export lands. See
// stripSpawnedReportContract for the same decision at the other gopi-spawning
// sites.
func taskChildExecEnv() (env []string, removed []string) {
	env, removed = stripDaemonContextEnv(os.Environ())
	env, spendRemoved := stripSpendBoundaryEnv(env)
	env, reportRemoved := tools.StripInheritedReportContract(env)
	env, checkoutRemoved := stripCheckoutHomeEnv(env)
	return env, append(append(append(removed, spendRemoved...), reportRemoved...), checkoutRemoved...)
}

// stripCheckoutHomeEnv drops a GOPI_HOME whose VALUE names a git work tree — the
// one inheritance this seam must NOT pass on, and the reason the list above can
// stay a subtraction instead of becoming a whitelist.
//
// WHY THE EXECUTOR CHILD IS THE RIGHT PLACE (epic: no child resolves a throwaway
// profile). GOPI_HOME is documented as a HOME and the profile root is
// `<GOPI_HOME>/.gopi`. A wrapper standing in the task's own worktree can carry
// that worktree as GOPI_HOME (a launcher's export, a script that sets it to
// `$PWD`, a checkout that IS the sandbox), and then the executor — and every
// shell below it — resolves `<worktree>/.gopi` as its profile: a throwaway
// profile nothing sweeps, nothing dispatches from and nothing reads back, whose
// files DIRTY the checkout the deliverable is being built in. Measured on this
// run's own bash-tool child (2026-10-15).
//
// THE KEPT ARM IS THE LOAD-BEARING ONE: `GOPI_HOME=<sandbox>` (a real directory
// holding `.gopi` — what `gopi tool sandbox` materialises and what the sandbox
// docs instruct) carries NO checkout marker, so it still inherits. The predicate
// is asked, never a path shape guessed (tools.IsCheckoutHomeEnv →
// config.GopiHomeNamesCodeCheckout, Rules 13/14: one definition).
func stripCheckoutHomeEnv(env []string) (stripped []string, removed []string) {
	stripped = make([]string, 0, len(env))
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if tools.IsCheckoutHomeEnv(name, value) {
			removed = append(removed, name)
			continue
		}
		stripped = append(stripped, kv)
	}
	return stripped, removed
}

// taskCheckoutHomeStripFmt is the ONE spelling of the record that announces a
// checkout-shaped GOPI_HOME was dropped from the executor child's environment.
// It carries the VALUE and the reason (Rule 15: a strip that cannot be read back
// is indistinguishable from a guard that never ran).
const taskCheckoutHomeStripFmt = taskProgName +
	": child env: dropped %s=%s — that value names a code checkout (it carries %s), so the child would resolve a " +
	"throwaway profile inside the checkout; the child resolves the operator's own profile instead. " +
	"Point %s at a real home to give the task a private one.\n"

// taskCheckoutHomeStripLine renders taskCheckoutHomeStripFmt from the values the
// strip removed, or "" when GOPI_HOME was kept (an ordinary run prints nothing —
// the kept arm is silent BY DESIGN, because inheritance is the normal case).
func taskCheckoutHomeStripLine(removed []string, env []string) string {
	kept := true
	for _, name := range removed {
		if name == tools.GopiHomeEnvName {
			kept = false
			break
		}
	}
	if kept {
		return ""
	}
	value := ""
	for _, kv := range env {
		if name, v, ok := strings.Cut(kv, "="); ok && name == tools.GopiHomeEnvName {
			value = v
			break
		}
	}
	return fmt.Sprintf(taskCheckoutHomeStripFmt, tools.GopiHomeEnvName, value, config.CheckoutMarkerName, tools.GopiHomeEnvName)
}

// stripSpendBoundaryEnv drops the heavy-test spend boundary from a child's
// environment (tools.SpendBoundaryEnvVars): the heavy-set opt-in and the
// operator's terminal declaration. The declaration is the load-bearing one —
// agentguard documents GOPI_HUMAN_TERMINAL as per-invocation precisely so no
// descendant can inherit it, and a task child that inherited it would be able
// to claim the operator's authority for the most expensive thing this repo can
// be asked to do. The opt-in alone is already refused at the test itself
// (internal/heavytest's loud REFUSAL); dropping it here means a task child
// cannot even look like an operator invocation.
func stripSpendBoundaryEnv(env []string) (stripped []string, removed []string) {
	stripped = make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if tools.IsSpendBoundaryEnv(name) {
			removed = append(removed, name)
			continue
		}
		stripped = append(stripped, kv)
	}
	return stripped, removed
}

// stripDaemonContextEnv returns env WITHOUT the execution-context variables a
// spawned executor child must not inherit (tools.DaemonContextEnvVars) and the
// names actually removed. The removed set is RETURNED so the spawn seam can
// report it: an isolation strip that happens silently leaves an operator
// unable to tell "nothing leaked" from "nothing was checked" (Rule 15).
//
// A name matches when the "NAME=value" entry's NAME half is the constant
// exactly. Values are never inspected — the question is whether the daemon's
// handle is PRESENT in the child's environment, and an empty value is still a
// presence (the guard reads presence, not truthiness).
func stripDaemonContextEnv(env []string) (stripped []string, removed []string) {
	stripped = make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if isDaemonContextEnv(name) {
			removed = append(removed, name)
			continue
		}
		stripped = append(stripped, kv)
	}
	return stripped, removed
}

// isDaemonContextEnv reports whether name is one of the daemon-context names
// (tools.DaemonContextEnvVars — the single source of truth; this is the
// predicate, not a second list).
func isDaemonContextEnv(name string) bool {
	for _, v := range tools.DaemonContextEnvVars {
		if name == v {
			return true
		}
	}
	return false
}

// taskEnvStripLine renders the strip's record (the reported half of the
// seam's env hygiene): the removed names when there were any, the
// already-clean line otherwise. Both outcomes are printed — the operator
// reading a worker log must be able to see WHICH one happened.
func taskEnvStripLine(removed []string, taskID string) string {
	if len(removed) == 0 {
		return taskEnvStripNoneLine
	}
	return fmt.Sprintf(taskEnvStripFmt, removed, taskID)
}
