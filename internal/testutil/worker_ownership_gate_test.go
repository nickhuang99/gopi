package testutil

// The GREP GATE for worker ownership (operator ruling 2026-09-12 —
// devdoc/pa-never-runs-tasks-20260912.md §7.1/§7.2).
//
// THE DEFECT THIS CLOSES: the repo's own devdocs, briefings and scripts were
// still written as AGENT-EXECUTABLE instructions — "start a worker", "nohup
// gopi task run <id>" — which re-derives exactly the move the ruling forbids
// (hermes/PA must never start a worker or run a task; the guard refuses with
// exit 3). The audit listed the locations; nothing stopped the NEXT document
// from teaching the same move, and nothing stopped an edit from dropping the
// annotation that now marks those locations operator-only.
//
// THE RULE: every file under devdoc/, briefings/ and scripts/ that MENTIONS a
// worker-launch / task-run shape must carry ONE canonical pointer line —
// the owner marker (workerOwnershipMarker) plus the canonical file's path —
// or be named in workerOwnershipDismissed below with a stated reason. A
// dismiss entry is a reviewed exception; it is never a way to quietly
// un-gate a file that should be annotated (the gate ALSO fails a file that is
// dismissed while carrying the marker, so the table cannot drift out of sync
// with reality).
//
// WHAT "THE CANONICAL POINTER" MEANS: ONE block owns the policy
// (devdoc/worker-ownership.md), and each audited location carries ONE line
// pointing at it. No 25 hand-copied paragraphs — a copied paragraph drifts;
// a pointer cannot.
//
// WHY IT LIVES IN testutil: this package already owns the repo-level SCRIPT
// and DOC gates (scripts_gate_test.go: the /tmp ban, `bash -n`, the unbounded
// loop ban; procname_gate_test.go: the launcher's counting contract), so the
// doc/script pointer gate is the same kind of check in the same place.
// internal/procname imports nothing, so reading the verbs from it closes no
// cycle (Rule 13/14: the verb values are referenced, never re-typed).
//
// THE THREE TESTS:
//
//  1. TestWorkerOwnershipPointerPresent — the gate itself: no file may
//     mention a launch shape without the pointer (or a dismiss reason).
//  2. TestWorkerOwnershipCanonicalBlockIsReal — non-vacuity on the TARGET
//     side: the canonical file must define the marker, say what to do
//     instead, carry the expected exit code, and every pointer must actually
//     name it. A gate over a missing or empty target is worse than no gate.
//  3. TestWorkerOwnershipGateScansTheTree — non-vacuity on the SCAN side: the
//     walk must have seen the whole tree, every audited anchor from the
//     2026-09-12 audit must be in the scan, and no anchor may be dismissed.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/workerownership"
)

const (
	// workerOwnershipCanonicalRel is the ONE file that owns the policy: who
	// starts a worker, what an agent does instead, and what the guard's
	// refusal means. Every pointer names it. It ALIASES
	// workerownership.CanonicalRel (Rule 14: one definition, in the leaf
	// package both the gate and the WRITE PATH import) — the value is never
	// re-typed here, so the writer can never emit a pointer at a path this
	// gate would not accept.
	workerOwnershipCanonicalRel = workerownership.CanonicalRel
	// workerOwnershipMarker is the machine-detectable token a pointer carries.
	// It is deliberately stricter than "see worker-ownership.md": the token
	// says WHO may run the command (operator-only), which is the ruling's
	// content, so a pointer cannot be mistaken for a cross-reference.
	// Aliased from the leaf package for the same reason as the path above.
	workerOwnershipMarker = workerownership.Marker
	// workerOwnershipDirBriefings is the second doc tree audited. devdoc/ and
	// scripts/ reuse this package's existing constants (devdocDirName,
	// scriptsDirName — scripts_gate_test.go) rather than re-spelling them.
	workerOwnershipDirBriefings = "briefings"
	// workerOwnershipExitCodeText is the refusal the pointer's canonical block
	// must state: the guard refuses a forbidden verb with this exit code, and
	// the whole point of the annotation is that a refusal is the contract
	// working, not a bug to work around.
	workerOwnershipExitCodeText = "exit 3"
	// workerOwnershipTaskRunSubverb is the `task` sub-verb that ran one task
	// directly — a RETIRED spelling since epic worker-run-entry (2026-09-16),
	// kept in the needle set because the audited corpus carries the historical
	// shape and a document that teaches it still needs the pointer. Aliased
	// from the leaf package, which builds the needle the WRITER tests against,
	// so the two needle lists cannot drift.
	workerOwnershipTaskRunSubverb = workerownership.TaskRunSubverb
	// workerOwnershipMinScannedFiles / workerOwnershipMinTriggerFiles guard a
	// vacuous pass: the trees hold hundreds of docs and the historical verb
	// mentions are dozens, so a scan finding almost none is looking in the
	// wrong place (a moved directory must redden the gate, not silence it).
	workerOwnershipMinScannedFiles  = 100
	workerOwnershipMinTriggerFiles  = 5
	workerOwnershipAuditedDocSuffix = workerownership.SuffixDoc
	// workerOwnershipMaxReportedHits caps the per-file LISTING only: the total
	// uncovered count is ALWAYS stated (see workerOwnershipUncoveredReport).
	// The 2026-09-15 incident was truncation WITHOUT disclosure — the cap
	// printed "… (truncated)" after 20 hits and said nothing about the count
	// behind it, so a briefing author read a partial list as the whole defect.
	workerOwnershipMaxReportedHits = 20
	// workerOwnershipFullListEnvName is the escape hatch for a report that
	// needs every file: with the cap-printed list a reader sees the total and
	// the first workerOwnershipMaxReportedHits paths, and sets this knob to
	// print the rest. Read through config.EnvBool (Rule 2: the ONE boolean
	// env funnel, so "off"/"0"/"" are all properly disabled — the value is
	// never compared in this file).
	workerOwnershipFullListEnvName = "GOPI_WORKER_OWNERSHIP_FULL_LIST"
	workerOwnershipAuditedShSuffix = workerownership.SuffixSh
	// workerOwnershipAuditedTxtSuffix covers the pasted run transcripts the
	// audit's own §7.2 locations are made of: a smoke-run log committed under
	// devdoc/ still READS as a copyable launch sequence, so it is in scope.
	workerOwnershipAuditedTxtSuffix = workerownership.SuffixPlain
)

// workerOwnershipVerbNeedles are the command SHAPES that read as an
// instruction to start a worker or run a task. They come from the leaf package
// (internal/workerownership) — the SAME list the WRITE PATH matches to decide
// whether a document needs the pointer — so a needle can never be added on one
// side only: the writer would either annotate a document the gate never
// flagged (noise) or miss a shape the gate flags (the per-round red this task
// exists to end). The list itself is built from internal/procname's constants
// (Rule 13/14 — the same values the launcher script and the CLI read) plus one
// path name, so a rename on either side reddens this gate instead of silently
// un-matching the corpus.
var workerOwnershipVerbNeedles = workerownership.Needles

// workerOwnershipAnchors maps each audited location (audit §7.1 + §7.2, plus
// the two out-of-tree companions that read as instructions) to the EXACT line
// its annotation must precede. The anchor asserts the annotation landed at
// the place the audit named — an annotation appended at the end of a 300-line
// post-mortem is present but useless.
//
// Anchors live here (not in the docs) so a doc edit that deletes the
// annotated section fails the gate instead of silently losing the pointer.
var workerOwnershipAnchors = map[string]string{
	// ── §7.1 executable starts ────────────────────────────────────────────
	"scripts/gopi-worker-up.sh": "#!/usr/bin/env bash",
	// scripts/e2e-m421-claim-lock.sh USED TO BE ANCHORED HERE. It was the
	// claim-lock E2E harness, and it was deleted with the cross-node claim
	// lock (batch 4) — an anchor naming a file that is gone fails the gate,
	// which is the gate working: the audited location no longer exists.
	"devdoc/task-ledger-m46-47.md": "nohup gopi task run",
	"devdoc/m47-e2e-report.md":     "nohup gopi task run",
	// ── §7.2 devdocs whose text teaches a spawn ───────────────────────────
	"devdoc/m420-worker-loop.md":                               "gopi worker",
	"devdoc/bugfix-worker-idle-exit-20260908.md":               "gopi worker",
	"devdoc/bugfix-worker-help-20260902.md":                    "gopi worker",
	"devdoc/review-cli-task-sync.md":                           "gopi worker -h",
	"devdoc/gopi-home-hygiene.md":                              "gopi-worker-up.sh",
	"devdoc/nightshift-20260911.md":                            "gopi-worker-up.sh",
	"devdoc/OVERNIGHT-SUMMARY-20260910.md":                     "gopi worker",
	"devdoc/night-report-20260910.md":                          "gopi worker",
	"devdoc/hook-investigation-report.md":                      "spawning worker to drain queue",
	"devdoc/manual-taskrun-recycle-20260903.md":                "gopi task run",
	"devdoc/server-side-worker-recycle-20260904.md":            "gopi worker",
	"devdoc/worker-worktree-recycle-investigation-20260908.md": "gopi worker",
	"devdoc/run-method-provenance.md":                          "gopi worker",
	"devdoc/cli-command-audit.md":                              "gopi worker",
	"devdoc/salvage-20260912-0412-incident.md":                 "gopi worker",
	"devdoc/worker-cohort-kill-20260912.md":                    "gopi worker",
	"devdoc/worker-count.md":                                   "gopi-worker-up.sh",
	// ── §7.2 briefings ────────────────────────────────────────────────────
	"briefings/brief-worker-idle-exit.md": "gopi worker",
}

// workerOwnershipCodeAnchors is the audit's out-of-tree location set: Go source
// files that build and ssh-dispatch a peer's run command. Each is checked
// exactly like a doc anchor (the line must still exist and the pointer must
// still be there) but is NOT part of the doc/script walk, so it lives in its own
// map rather than weakening the in-scan check with a special case.
//
// IT IS EMPTY, AND THAT IS THE MEASURED STATE (2026-10-22, task 01a160a2). Its
// one entry was `cmd/agent/dispatch.go` — the M4.7 remote-dispatcher, whose
// `dispatchRemoteCommand` built the peer run command. That file was DELETED with
// the `compute_nodes` knob it served: its only entry point, `runDispatchOnce`,
// had zero call sites (its single historical caller, `gopi task serve`, is a
// retired-daemon refusal), so the whole dispatch layer went in the same change.
// The map stays — empty, named, and iterated — because the walk below is the
// mechanism that catches a re-introduction, and an audit that loses its
// out-of-tree half the moment that half happens to be empty is the audit
// silently narrowing.
var workerOwnershipCodeAnchors = map[string]string{}

// workerOwnershipDismissed is the reviewed dismiss table: a file that mentions
// a launch shape but must NOT carry the pointer, with the reason. Every entry
// is a decision, kept next to the check so a reviewer can re-litigate it.
//
// The gate also fails an entry whose file carries the marker anyway (a stale
// permit hides a real annotation from the audit), so this table can only ever
// shrink — an annotated file must leave it.
var workerOwnershipDismissed = map[string]string{
	workerOwnershipCanonicalRel: "the canonical block itself — this file IS the target every pointer names; it defines the marker instead of pointing at itself",

	// The 2026-09-12 ruling + its audit (feat/pa-never-runs-tasks — the guard
	// itself is not on master yet) OWNS the subject: it is where the move is
	// defined and forbidden, not a location that instructs it. This gate is
	// scoped to files that EXIST in the tree, so the entry is added by the
	// task that merges that branch — dismissFor files that do not exist
	// would redden the gate on master today.

	// Post-mortems / audits whose mention is EVIDENCE: the quoted command is
	// what the blamed process did, and the pointer would misread the sentence
	// as an instruction to follow.
	// RETIRED (epic 01a0fd2e scope 3/5): the evidence blob below was untracked
	// by the sandbox/evidence removal, and this table fails a permit whose file is
	// gone, so the entry goes in the same change — its reason, kept for the record:
	//   devdoc/assets/serve-lock-diagnosis-liveness/base-cmd-agent-full.txt —
	//   evidence: a captured 720-line test-run transcript; the quoted worker lines
	//   are what the suite under diagnosis PRINTED (cwd churn, contract stripping,
	//   daemon start), not a launch recipe to follow.
	"devdoc/agent-test-cpu-investigation.md": "evidence: counts the live `gopi worker` instances that caused CPU contention",
	"devdoc/auto-mirror-runcmd.md":           "evidence: run_cmd provenance in the ledger (what a row recorded), no launch recipe",
	"devdoc/ab2-control-runcmd-trace.md":     "evidence: an A/B trace of run_cmd stamps per row, not a launch recipe",
	"devdoc/ab-control-run-cmd-settings.md":  "evidence: the --settings control's run_cmd stamps",
	"devdoc/bug054-session-stamp.md":         "evidence: session-stamp forensics on an already-run task",
	"devdoc/bug056-writeback-summary.md":     "evidence: writeback summary of a completed run",
	"devdoc/bug057-two-row-linkage.md":       "bug report: two rows for one execution — quotes the run to describe the defect",
	"devdoc/bugfix-broken-pipe-20260902.md":  "evidence: the SIGPIPE post-mortem of one run",
	// "devdoc/bugfix-task-p1-20260902.md" REMOVED 2026-10-31: the doc now
	// carries the pointer itself (header line 4, added by df8d3b3), so the
	// dismissal had become a permit hiding a real annotation — the ratchet
	// turns, exactly as this table's own comment above prescribes.
	"devdoc/bugfix-task-run-double-exec-2026-09-02.md":             "bug report: double-execution repro — quotes the two shapes it describes",
	"devdoc/bugfix-taskrun-briefing-summary-payload-2026-09-03.md": "evidence: briefing/payload trace of a run",
	"devdoc/crash-dump-startup-backfill.md":                        "bug report: quotes the verb in its TITLE to name the symptom",
	"devdoc/fix-malformed-settings-nilpanic-20260910.md":           "evidence: nilpanic trace through the subcommand chain",
	"devdoc/fix-task-show-run-cmd.md":                              "evidence: the run_cmd rendering fix",
	"devdoc/handoff-20260910-1420.md":                              "archive: a session handoff's pending-work list — kept as the record of that state, not a recipe",
	"devdoc/merge-report-worker-log-session-attribution.md":        "merge record: file/hunk inventory of the launcher change",
	"devdoc/rootcause-useless-auto-task-worker.md":                 "evidence: run_cmd forensics on auto rows",
	"devdoc/scratch-residue-audit.md":                              "audit: /tmp residue forensics; the mention is a process-count measurement",
	"devdoc/scratch-spawn-audit.md":                                "audit: which tests spawn a process — a table of test files",
	"devdoc/sigpipe-flake-fix.md":                                  "evidence: names the wrapper process in a SIGPIPE trace",
	"devdoc/worktree-live-cwd-guard-20260910.md":                   "evidence: CWD forensics of a manual run",
	"devdoc/worktree-cwd-zombie.md":                                "evidence: the 01a09e44 post-mortem — its ONE match is the noun phrase naming the task-run FACE (`cli_tasks.go:1833`) inside a 5-why mechanism sentence about the cwd release; the file has ZERO fenced code blocks (measured: grep -c '^```' = 0), so there is no launch recipe to warn a reader off of; sibling of worktree-live-cwd-guard-20260910.md above",
	"devdoc/reframe-evaluate-merge.md":                             "evidence: the merge/ledger trace of an epic",
	"devdoc/reframe-telemetry-doublecount.md":                      "evidence: GOPI_TASK_WRAPPER_ID provenance",
	"devdoc/review-memory-aware-machinery.md":                      "design note: names the spawn point to explain the memory path",

	// CLI/usage reference material: it DOCUMENTS the subcommands (what the
	// verb does, how it reports) rather than instructing a reader to run one.
	"devdoc/centralized-architecture-20260830.md": "architecture: describes the ssh execution leg, no recipe",
	// "devdoc/chat-table-design.md" REMOVED 2026-10-31: the doc now carries
	// the pointer itself (header line 4, added by d4deb20), so the dismissal
	// had become a permit hiding a real annotation — the ratchet turns.
	"devdoc/help-task-serve-to-merger-20260903.md":        "usage-help rewrite record: quotes the help text it changed",
	"devdoc/positional-submit-payload.md":                 "CLI-input design note",
	"devdoc/post-receive-hook-v5.md":                      "hook design note: names who drains the queue",
	"devdoc/review-cli-task-core.md":                      "CLI core review: quotes the repro commands OF THE REVIEW",
	// "devdoc/review2-tasksync.md" REMOVED 2026-11-08: the doc carries the
	// pointer itself (header line 4), so the dismissal had become a permit
	// hiding a real annotation from the audit — the ratchet turns down, never up.
	"devdoc/run-cmd-settings.md":                          "evidence: run_cmd stamps of the settings control",
	"devdoc/serve-worker-split-20260903.md":               "design record: the split's rationale (who is client/server)",
	"devdoc/task-dispatcher-p2.md":                        "dispatcher design: quotes the command the dispatcher builds",
	"devdoc/task-show-briefing.md":                        "bug report: a dispatch with no payload",
	"devdoc/m411-priority-verification.md":                "verification: claim-ORDER probe using `task run next` as the driver",
	"devdoc/m420-epic-report.md":                          "epic report: the E2E worker-loop result line",
	"devdoc/bug052-dispatch-dbsync.md":                    "bug report: quotes `dispatchToNode`'s ssh command to describe the db-sync defect",
	"devdoc/agent-full-lifecycle-chain-audit-20260911.md": "audit table: names the existing mechanisms",
	"devdoc/worker-log-session-attribution-20260912.md":   "the launcher's OWN change record — it documents the launcher contract the pointer's canonical block now owns",

	// ── 2026-09-13 backfill: the remaining trigger files, each reviewed
	// per-file. The split is the gate's own question — does the file TEACH
	// the launch move (→ pointer, see the diffs in
	// devdoc/worker-ownership-pointer-backfill-20260913.md) or merely
	// NAME/CITE it (→ here, with why)?
	//
	// Findings and incidents: the mention is a measured count / a timeline
	// entry about workers that already existed, never a recipe.
	"briefings/brief-salvage-0412.md":        "evidence: counts the six `gopi worker` casualties of the 04:12 kill as collateral of `hermes`",
	"briefings/brief-worker-cohort-kill.md":  "evidence: the forensic timeline of the 04:12 cohort kill (which workers died, via what launcher) — the count IS the bug report",
	"briefings/brief-worktree-recycle-2.md":  "evidence: quotes the operator's recollection of a past fix ('I thought ... gopi worker or task ... should remove git [worktree]') while reporting a recycling symptom",
	"briefings/brief-task-ledger-hygiene.md": "evidence: a ledger-hygiene inventory listing `gopi worker -h` usage-fix commits by subject line",


	// Design/architecture records: they state which process owns which path
	// (so they name the verb) and explicitly assign the launch to the
	// operator — they are the reasoning BEHIND the single-launcher contract
	// that the pointer's canonical block now owns, not instructions to run it.
	"briefings/brief-merger-topverb-daemon.md":             "design record: argues merger is an independent top-level service 'like `gopi worker` is a top-level command' — the comparison IS the argument",
	"devdoc/daemon-context-isolation.md":                   "design/evidence record: names which process spawns which per task, and its verdict is that ONLY the operator launches (explicitly not an agent)",
	"devdoc/signalrec-session-attribution.md":              "design record: explains that `gopi task run` MINTS the child's run uuid at claim time, so the ledger key is minted by the launcher it documents — a mechanism note, no recipe",
	"devdoc/merge-resolution-test-budget-single-source.md": "evidence: an aborted-test-run log line (`gopi worker: --daemon: write GOPI_DAEMON_READY_FD: ...`) printed as proof of a known full-suite hang class",

	// Pasted run transcripts (.txt, in the walk now — see the suffix const):
	// annotating a captured log corrupts it for anyone grepping the output, and
	// the mention is inside a QUOTED help text / an observed process line.
	"devdoc/merger-daemon-smoke-output.txt": "pasted smoke-run transcript: the mention is the merger's own `-h` text naming its peer ('the peer of gopi worker'), captured, not instructed",

	// ── 2026-09-13 backfill #2: the ONE file the graceful-drain epic left
	// red that `fix/rawlog-test-home-leak` (6b257abe) does NOT cover. Its
	// own 10 files are ANNOTATED there with the canonical pointer, so this
	// branch annotates the same 10 the same way (byte-identical shape —
	// the two branches then merge as a no-op instead of a contradiction,
	// see devdoc/tmpdir-merge-resolution.md §3) and dismisses only this one.

	// ── 2026-09-13 backfill #3: a file that arrived on master MID-TASK (the
	// epic row was briefed against 399ea474; master advanced to 18f61785
	// while this branch was being verified). Its mention is a FAILURE
	// MECHANISM being explained, not a launch recipe — but the durable fix
	// belongs to whoever owns that doc's epic, so this is the one entry
	// here that is explicitly a HOLD, not a verdict on the file.
	//
	// The doc's §A explains why a cmd/agent test fails when the test process
	// itself runs inside a worker: it quotes the test's own `gopi worker
	// --once` child as the SYMPTOM (the child takes the daemon-child re-exec
	// path and dies in the handshake) — the sentence teaches a reader what
	// went wrong in a captured run. The sibling epic fix/tmpdir-settings/
	// peakhours owns this file (acd96f2e, on master); the pointer question
	// is answered in devdoc/tmpdir-merge-resolution.md §5 and belongs to it.
	"devdoc/peakhours-exact-boundary.md": "HOLD (not a teach): quotes the test's `gopi worker --once` child as the symptom of an inherited-daemon-handle failure — a mechanism explanation, not a launch recipe; the file's own epic (the peakhours restack) still owns it and can convert this to a pointer",

	// The peak-guard epic's report (fix/peak-guard-token-free-verbs): its
	// mentions are the epic's OWN live probe matrix, where `gopi worker`
	// (and `gopi task run`, plus their `-h` spellings) appear as TABLE ROWS
	// whose recorded outcome is the guard's refusal (exit 4) — the cells are
	// observations of a command being DENIED inside an armed peak window,
	// not a recipe. The pointer would be a false instruction here: the one
	// command this file teaches a reader to copy is `gopi version`, the
	// invocation whose exemption is the epic's whole subject. The file is a
	// captured evidence record like the transcripts dismissed above, not a
	// how-to-launch document.
	"devdoc/peak-guard-token-free-verbs.md": "evidence: the peak guard's probe matrix — `gopi worker` / `gopi task run` (and their `-h` forms) are rows recording the guard's exit-4 refusal under an armed peak window, the denial ITSELF being the measurement; no launch recipe",

	// This gate's OWN master-red report (fix/worker-ownership-gate-peak-doc):
	// it quotes the offending doc and the needle shapes it hit while
	// explaining the disposition, so it matches itself. Self-referential
	// dismissal, the same disposition devdoc/worker-ownership-pointer-master-red.md
	// took on fix/worker-ownership-pointer-line.
	"devdoc/worker-ownership-gate-peak-doc.md": "evidence: this gate's OWN master-red analysis — its matches are the needles it quotes back while explaining why the peak-guard report was dismissed, not a launch instruction",
	// The Rule 24 rationale (feat/detach-judgment-rule → 2c01d9ec): the
	// 04:12 cohort as the incident's own evidence, (b) the `pgrep -f "gopi
	// worker"` SELF-MATCH trap — a diagnostic whose lesson is that a
	// cmdline pattern must not be used to detect a worker at all — and
	// (c) §6's inventory of the existing `Setsid` SPAWN SITES as the input
	// to a not-yet-implemented gate. No line instructs a reader to start a
	// worker; the doc's own verdict is that only an operator launches, and
	// the cgroup criterion it defines is a diagnostic.
	"devdoc/detach-judgment-rule.md": "mechanism/evidence record for GOPI.md Rule 24: quotes the 04:12 cohort's `ps` output and the `pgrep -f` self-match trap (a detect-the-worker diagnostic), and §6 inventories the existing spawn sites — no launch recipe, and the rule it documents says only the operator launches",

	// ── 2026-09-16 backfill #4 (this branch): four files that REGREW the
	// launch shape while reporting on machinery AROUND a live worker. Each
	// mention below is a MEASUREMENT of processes that already existed or of
	// a step this session is explicitly forbidden to perform — never a
	// command a reader is told to run. The test is the one the gate's own
	// header states: does the sentence teach the launch move (→ pointer) or
	// merely name/cite it (→ here, with why)?
	//
	// ── 2026-09-16: four entries REMOVED, and why that is progress ────────
	//
	// (a) the three worktree reports and (b) nested-report-path-clobber were
	// dismissed here because their mention was evidence rather than a recipe.
	// The judgement was right and the disposition is now the better one: the
	// docs landed their OWN pointer line (fix/worker-ownership-pointer-backfill-3,
	// merged as 900f648c), because a pointer is visible AT the mention site
	// while a dismissal is invisible to a reader. Leaving the entries would
	// make TestWorkerOwnershipDismissTableIsHonest fail — a dismissed file that
	// carries the pointer is a permit hiding a real annotation — so they are
	// deleted here, not relaxed. The table may only ever shrink (see the
	// function's header); this is that ratchet turning.
}

// workerOwnershipAuditedRoots are the trees the gate walks. They come from the
// leaf package (internal/workerownership.AuditedRoots), which the WRITE PATH
// also asks — so "is this destination in the audited scope?" has ONE answer
// for the walker and for the annotator.
func workerOwnershipAuditedRoots() []string {
	return workerownership.AuditedRoots()
}

// workerOwnershipAudited reports whether a file is in the gate's scope.
func workerOwnershipAudited(path string) bool {
	return workerownership.Audited(strings.ToLower(filepath.Ext(path)))
}

// TestWorkerOwnershipPointerPresent is the gate: a file that mentions a
// worker-launch / task-run shape carries the canonical pointer, or it is in
// the dismiss table with a reason.
func TestWorkerOwnershipPointerPresent(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	missing, err := workerOwnershipUncovered(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) == 0 {
		return
	}
	t.Error(workerOwnershipUncoveredReport(missing))
}

// workerOwnershipUncovered is the gate's DECISION, separated from the tree it
// is normally asked about: it walks root's audited trees, drops the dismissed
// files, and returns the rest that mention a launch shape without the pointer.
//
// It takes a root so the decision can be exercised against a FIXTURE (the
// anti-drift and red-proof tests next door plant an offender in a scratch tree
// and assert it is reported) — a gate whose only callable form reads the real
// repository can only be proven red by damaging the repository.
func workerOwnershipUncovered(root string) ([]string, error) {
	_, triggers, err := workerOwnershipScanRoot(root)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, rel := range triggers {
		if _, ok := workerOwnershipDismissed[rel]; ok {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		if !workerownership.Annotated(string(body)) {
			missing = append(missing, rel)
		}
	}
	return missing, nil
}

// workerOwnershipUncoveredReport renders the gate's failure message. Its
// contract is DISCLOSURE: the total count always appears, so a red report can
// never under-state the blast radius, and every withheld path is accounted for
// as a number a reader can see (and, with workerOwnershipFullListEnvName set,
// read in full). The listing cap is readability only — the count is the truth.
//
// (2026-09-16: the message listed ONE file while the real uncovered set was
// six, because the cap was applied to the printed hits with a bare
// "… (truncated)" and no total. Whoever wrote the briefing downstream fixed
// one sixth of the defect. See
// devdoc/worker-ownership-gate-full-report-20260916.md.)
func workerOwnershipUncoveredReport(missing []string) string {
	sort.Strings(missing)
	shown := missing
	withheld := 0
	if !config.EnvBool(workerOwnershipFullListEnvName) && len(missing) > workerOwnershipMaxReportedHits {
		shown = missing[:workerOwnershipMaxReportedHits]
		withheld = len(missing) - len(shown)
	}
	var b strings.Builder
	// The count first — it is the half that must survive any truncation.
	fmt.Fprintf(&b, "%d uncovered file(s) teach a worker-launch / task-run command without the operator-only pointer", len(missing))
	if withheld > 0 {
		fmt.Fprintf(&b, "; listing first %d, %d withheld (set %s=1 to list every file)",
			len(shown), withheld, workerOwnershipFullListEnvName)
	} else {
		fmt.Fprintf(&b, "; listing all %d", len(shown))
	}
	b.WriteString(":\n")
	for _, rel := range shown {
		b.WriteString("  " + rel + "\n")
	}
	b.WriteString("fix: insert the ONE canonical pointer line —\n\t# " + workerOwnershipMarker + " — " + workerOwnershipCanonicalRel + "\n")
	b.WriteString("(the comment form of the file's language: <!-- … --> in markdown, # in shell, // in Go),")
	b.WriteString(" or add the file to workerOwnershipDismissed with a reason.")
	return b.String()
}

// TestWorkerOwnershipCanonicalBlockIsReal is the non-vacuity check on the
// TARGET: the pointer must point at a block that actually carries the policy.
// A green gate over an empty canonical file would be exactly the silent no-op
// this task exists to prevent.
func TestWorkerOwnershipCanonicalBlockIsReal(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(workerOwnershipCanonicalRel)))
	if err != nil {
		t.Fatalf("the canonical block %s must exist — every pointer names it: %v", workerOwnershipCanonicalRel, err)
	}
	body := string(raw)
	for _, want := range []struct{ what, needle string }{
		{"the owner marker", workerOwnershipMarker},
		{"the agent's verb (what to do instead)", procname.NameGopi + " " + procname.VerbTask + " submit"},
		{"the guard's refusal exit code", workerOwnershipExitCodeText},
		{"the operator-owns-it statement", "operator"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("%s does not carry %s (want %q)", workerOwnershipCanonicalRel, want.what, want.needle)
		}
	}

	// Every anchored location must carry the pointer LINE (both halves) — the
	// anchor-presence half is TestWorkerOwnershipGateScansTheTree's job.
	anchored := make(map[string]bool, len(workerOwnershipAnchors)+len(workerOwnershipCodeAnchors))
	for rel := range workerOwnershipAnchors {
		anchored[rel] = true
	}
	for rel := range workerOwnershipCodeAnchors {
		anchored[rel] = true
	}
	for rel := range anchored {
		doc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if _, ok := workerOwnershipPointerLine(string(doc)); !ok {
			t.Errorf("%s: no pointer line carrying %q AND naming %s", rel, workerOwnershipMarker, workerOwnershipCanonicalRel)
		}
	}
}

// TestWorkerOwnershipGateScansTheTree is the non-vacuity check on the SCAN:
// the walk saw the whole tree, every audited anchor is IN the scan, and no
// anchor is dismissed (an annotation is mandatory for an audited location —
// the dismiss table is for files the audit did not name).
func TestWorkerOwnershipGateScansTheTree(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	scanned, triggers, err := workerOwnershipScan(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scanned) < workerOwnershipMinScannedFiles {
		t.Fatalf("the walk saw only %d auditable files — a moved tree must fail this gate, not silence it", len(scanned))
	}
	if len(triggers) < workerOwnershipMinTriggerFiles {
		t.Fatalf("the walk found only %d files mentioning a launch shape — the needles (%v) or the tree are wrong", len(triggers), workerOwnershipVerbNeedles)
	}
	seen := make(map[string]bool, len(scanned))
	for _, rel := range scanned {
		seen[rel] = true
	}
	for rel, anchor := range workerOwnershipAnchors {
		if !seen[rel] {
			t.Errorf("audited location %s is not in the scan (moved or deleted?) — the audit's §7 location list must stay covered", rel)
			continue
		}
		if err := workerOwnershipCheckAnchor(t, root, rel, anchor); err != "" {
			t.Error(err)
		}
	}
	for rel, anchor := range workerOwnershipCodeAnchors {
		if err := workerOwnershipCheckAnchor(t, root, rel, anchor); err != "" {
			t.Error(err)
		}
	}
}

// workerOwnershipCheckAnchor asserts one audited location still carries the
// line the pointer was attached to (an edited doc must re-anchor, not silently
// lose its pointer) and that it is not also dismissed.
func workerOwnershipCheckAnchor(t *testing.T, root, rel, anchor string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Sprintf("read %s: %v", rel, err)
	}
	if !strings.Contains(string(body), anchor) {
		return fmt.Sprintf("%s: the annotated line %q is gone — re-anchor the pointer", rel, anchor)
	}
	if reason, ok := workerOwnershipDismissed[rel]; ok {
		return fmt.Sprintf("%s is an AUDITED location and must carry the pointer, but it is dismissed (%q) — remove the entry", rel, reason)
	}
	return ""
}

// TestWorkerOwnershipDismissTableIsHonest is the table's stale-permit check:
// a dismissed file that DOES carry the pointer is a permit that hides a real
// annotation from the audit, so the table may only ever shrink.
func TestWorkerOwnershipDismissTableIsHonest(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	for rel, reason := range workerOwnershipDismissed {
		if rel == workerOwnershipCanonicalRel {
			continue // the canonical block IS the target; its entry documents that
		}
		if reason == "" {
			t.Errorf("dismiss entry %s has no reason — an unreasoned exception is a silent un-gate", rel)
			continue
		}
		if _, isAnchor := workerOwnershipAnchors[rel]; isAnchor {
			continue // reported by TestWorkerOwnershipGateScansTheTree
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("dismissed file %s is unreadable (gone? then drop the entry): %v", rel, err)
			continue
		}
		if _, ok := workerOwnershipPointerLine(string(body)); ok {
			t.Errorf("%s is dismissed as out of scope but carries the pointer — drop the dismiss entry so the audit sees it as annotated", rel)
		}
	}
}

// workerOwnershipPointerLine returns the single line carrying the canonical
// pointer: it must carry BOTH the marker and the canonical file's path, so a
// pointer can never degrade into a marker with a dead reference (or a
// reference nobody can act on). One line, both halves — that is the contract.
//
// It is the leaf package's predicate (workerownership.Annotated), not a second
// copy of it: the WRITE PATH uses the same function to decide "this file
// already carries the pointer", so an annotation the writer considers present
// can never be one this gate calls missing.
func workerOwnershipPointerLine(body string) (string, bool) {
	return workerownership.AnnotationLine(body)
}

// workerOwnershipScan walks the audited trees of the repository.
func workerOwnershipScan(t *testing.T, root string) (scanned, triggers []string, err error) {
	t.Helper()
	return workerOwnershipScanRoot(root)
}

// workerOwnershipScanRoot walks the audited trees under root and returns every
// auditable file (repo-root-relative, slash-separated) plus the subset whose
// text mentions one of the launch shapes. Reading the files here (not in the
// callers) keeps ONE walk behind all four tests.
//
// The match itself is the leaf package's predicate (workerownership
// MentionsLaunchShape) — the SAME one the write path uses to decide whether a
// document needs the pointer, so the gate cannot flag a shape the writer does
// not know about (which is exactly how master re-reddened per merge round).
func workerOwnershipScanRoot(root string) (scanned, triggers []string, err error) {
	for _, dir := range workerOwnershipAuditedRoots() {
		base := filepath.Join(root, dir)
		if _, statErr := os.Stat(base); statErr != nil {
			return nil, nil, statErr
		}
		walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !workerOwnershipAudited(path) {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			scanned = append(scanned, rel)
			if workerownership.MentionsLaunchShape(string(raw)) {
				triggers = append(triggers, rel)
			}
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}
	sort.Strings(scanned)
	sort.Strings(triggers)
	return scanned, triggers, nil
}
