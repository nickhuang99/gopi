package main

// worker_start_notices.go — the startup notices that belong to the `gopi worker`
// face, and the ONE predicate that decides which argv that face is.
//
// ─────────────────────────────────────────────────────────────────────────────
// 2026-10-22 (task 01a160a2): THE COMPUTE-NODE REPORT IS GONE, WITH THE KNOB.
//
// This file used to be compute_nodes_unwired.go, whose header explained at
// length that the `compute_nodes` whitelist was parsed, validated, and read by
// nobody: its only consumer, dispatch.go's runDispatchOnce, had zero call
// sites, its single historical entry point (`gopi task serve`) was a
// retired-daemon refusal, and `gopi task run --nodes` went with that verb. The
// file's answer was to REPORT that state in a ~400-char sentence on stderr at
// every worker start — on the argument that an unwired knob must not be silent
// (Rule 15).
//
// The report was honest and it was still wrong: it made every worker start
// carry an essay about a knob no code reads, on the ONE face operators start
// dozens of times a day. A notice nobody can act on — it told the operator to
// re-wire a dispatcher, which is a resident face and an operator decision
// (Rule 35) — is noise, not reporting. So the knob was DELETED instead:
// ComputeNode, ResolveComputeNodes, its env/settings/flag spellings, the
// `--nodes` refusal arm and dispatch.go's dangling dispatcher all went in the
// same change, and the sentence went with the thing it was about. A stale
// `compute_nodes` key in settings.json is now simply an unknown key: it starts
// nothing, refuses nothing, and prints nothing (the settings reader ignores
// keys the typed Config does not model — the same posture every removed knob
// gets). The decision and its evidence are in the task report.
//
// WHAT SURVIVES HERE is the TERMINAL-SCOPE HINT (below) and isWorkerFaceArgv,
// the predicate that scopes it to the worker face — with ONE deletion since:
//
// THE EMPTY-dev_repo DEPRECATION IS GONE TOO (operator ruling 2026-10-23, task
// 01a16541-57c0). It was the last startup notice in this file, and it was
// withdrawn for the reason the compute_nodes essay was: it announced a state
// that is not a fault. `dev_repo` is the OPTIONAL third source of the run
// repository, not a required switch — a host that does not declare it and
// starts its runs inside any git repository is on the NORMAL path, and telling
// it "DEPRECATED" once per worker start was a false alarm with a mute key.
// What replaces it is an ANSWER, not an announcement: internal/reposource
// resolves the run's repository (row deliverable.repo → cwd's repo →
// settings dev_repo) and speaks ONLY when all three decline, naming each
// source's own verdict; `gopi config show` reports the same answer read-only.
// The mute key (GOPI_NO_DEV_REPO_NOTICE / no_dev_repo_notice) went with the
// line, so no host has to keep a switch for a notice that no longer exists.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/cgroupscope"
)

// isWorkerFaceArgv is the ONE predicate for which argv the worker-face notices
// belong to. argv[0] must already be the worker verb (the caller is
// isWorkerSubcommand, which checked it) — the function does not re-check it as a
// bare literal (Rule 1).
//
// THE `-h` EXCLUSION IS THE WHOLE REASON THIS PREDICATE EXISTS. A usage query is
// answered BEFORE any config resolution — the P0-1 contract the neighbouring
// faces state in their own words (`gopi worker -h` must stay a zero-side-effect
// path, whatever else argv carries) — so no notice may fire for it. Every OTHER
// argv inside this verb is worker-shaped by definition: the loop, --once, a named
// row, --resume, the detached --daemon parent, and the read-only control
// spellings (--status) are all a worker starting, stopping, or being asked what
// is running.
//
// IT IS DELIBERATELY OVER-INCLUSIVE WITHIN THE VERB (Rule 1's spirit: no
// enumerated flag list to re-read every time the worker grows one): the shape is
// decided by WHICH VERB was typed, so a new worker flag cannot silently lose a
// notice, and an unknown-flag argv (refused further down) cannot gain a false one.
func isWorkerFaceArgv(args []string) bool {
	for _, a := range args[1:] {
		if a == workerFlagHelp || a == workerFlagHelpLong {
			return false
		}
	}
	return true
}

// --- the TERMINAL-SCOPE HINT (task 01a163f3) -------------------------------
//
// THE MEASURED DEFECT IT ANSWERS (2026-10-22 22:04:55, read out of
// ~/.gopi/sessions/01a163df-…/signal-provenance.jsonl):
//
//	{"signal":"SIGHUP","pid":1190977,
//	 "cgroup":"…/app-org.gnome.Terminal.slice/vte-spawn-c6f69923-….scope",
//	 "role":"worker","argv":"gopi worker 01a163df-…","sourced":false}
//
// A foreground `gopi worker <task-id>` runs inside the TERMINAL TAB's systemd
// scope, so closing that tab — or losing the ssh session it came from — destroys
// the scope and HANGS UP every member.
//
// WHAT CHANGED SINCE THAT MEASUREMENT (2026-10-27). The hangup used to be a STOP,
// and the forwarder reached into the in-flight run child with SIGTERM: a 38-minute
// round closed out `failed: run interrupted by signal`. It is now a DRAIN — the
// round runs to its own close-out and nothing is forwarded — so the banner must
// state the NEW contract rather than a consequence that no longer holds. The old
// sentence ("takes it down mid-round") would send an operator to `--daemon` for a
// reason that has expired, and would leave them not knowing that a foreground
// worker now survives a teardown with its work intact. `--daemon` stays the
// remedy for the reason that still holds: outliving the terminal for MORE than
// the current round.
//
// WHAT IS DELIBERATELY NOT DONE: the run is not refused, and no process model
// changes. Foreground is the documented way to watch a worker work, so the
// answer is ONE hint line naming the contract and the remedy.

const (
	// workerScopeHintContractFmt is the ONE CONTRACT line: what the terminal's
	// teardown now MEANS. The sentence it replaced said a hangup "takes it down
	// mid-round", which was true when a hangup was a stop. Since the drain change
	// it finishes the round, so an operator reading the old line would detach a
	// worker for a reason that has expired — and would not know that a foreground
	// worker now survives a teardown with its work intact.
	workerScopeHintContractFmt = "%s: this worker runs inside the TERMINAL's systemd scope (%s). " +
		"A hangup (SIGHUP) from its teardown is a DRAIN, not a stop: closing the terminal after a round starts " +
		"does NOT kill that round — it runs to its own close-out, then the worker claims no new row and leaves. " +
		"Start it detached with `%s` when it must outlive the terminal for more than the current round.\n"
	// workerScopeHintRemedy is the exact invocation the contract line recommends, spelled
	// from the daemon flag constant so the advice cannot drift from the flag
	// (Rule 1: the word exists once — daemonFlagLong).
	workerScopeHintRemedy = workerProgTopLevel + " " + daemonFlagLong
	// workerScopeHintProgram labels the line, matching the other worker notices
	// ("gopi worker: …") rather than inventing a second voice.
	workerScopeHintProgram = workerProgTopLevel
)

// workerSelfCgroup is the ONE measurement the hint is built on: this process's
// unified-hierarchy cgroup path, read FRESH from /proc/self/cgroup (Rule 24 —
// nothing else is a verdict about where a process sits). It is a variable only
// so a test can present the three measured/refused shapes without moving the
// test binary into a terminal scope; production never reassigns it.
var workerSelfCgroup = cgroupscope.SelfRel

// workerTerminalScopeHintLine renders the hint for a measured cgroup path, or
// "" when the hint does not apply. PURE — the whole decision is here, so the
// printer below cannot disagree with the tests.
//
// "" IS THE DEFAULT FOR THREE DIFFERENT REASONS, and they are one answer on
// purpose: a scope that is not terminal-backed (a `gopi-daemon-worker-*` scope
// acquired by `--daemon`, a system service, a container payload), an EMPTY path
// (no measurement), and a path the predicate cannot read. A hint that fired
// without a measurement would be a guess, and the one thing worse than a silent
// hint is one an operator learns to skip.
func workerTerminalScopeHintLine(rel string) string {
	if !cgroupscope.TerminalScope(rel) {
		return ""
	}
	return fmt.Sprintf(workerScopeHintContractFmt, workerScopeHintProgram, rel, workerScopeHintRemedy)
}

// reportWorkerTerminalScopeHint measures THIS process and prints the hint when
// the measurement says a terminal owns its lifetime. A failed read prints
// nothing: cgroupscope.SelfRel's error means the unified hierarchy was not
// readable (cgroup v1, a hardened /proc), which is not evidence of a terminal —
// and the hint must never be the thing that fails a worker start.
func reportWorkerTerminalScopeHint() {
	rel, err := workerSelfCgroup()
	if err != nil {
		return
	}
	if line := workerTerminalScopeHintLine(rel); line != "" {
		fmt.Fprint(os.Stderr, line)
	}
}
