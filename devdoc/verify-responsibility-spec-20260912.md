<!-- doc-class: normative -->
# Spec: the verifier's responsibility boundary (post-merge verify child)

Task: `01a09242-c9cf-7841-b9f2-b9eef0043928` — Deliverable B. Date 2026-09-12.
Base: master `8aa1f9b5`. Docs only — no product code (Rule 3: no new tool).

Companion: `devdoc/verify-boundary-audit-20260912.md` (Deliverable A — the
file:line audit of today's briefings). Every claim about existing code below is
carried over from that audit with its `file:line`.

**What this is:** the checkable do/don't boundary of the instance that judges a
BUGFIXING merge on master — the `gopi task verify` child that the merger gate
spawns (`cmd/agent/merger_verify_child.go:370`, `runMergeVerifyChild`).

**What this is not:** it does not design or implement the gate (that is
`01a09237-f56a-7af6-8709-9a4825f1a2f4`, landed as `internal/tools/verify_gate.go`
+ `cmd/agent/merger_verify_child.go`), it does not wire verification into the bug
flow (`01a09216-6561`, landed as `internal/tools/task_verify_closure.go`), and it
does not touch the terminal-notify defect (`01a09225-a216`). See §7.

---

## 0. Why a boundary is needed at all

The operator's framing (recorded in the gate's own briefing): the verifier is a
**tester + code reviewer**, not a second developer; its value is **a different
context** — same model, different angle. If it re-derives the fix, it is
"同一个模型在同样的问题空间犯同样的错" and we paid twice for one opinion.
The audit (Deliverable A §2–§4) shows that today this boundary rests on ONE
instruction sentence, with no tool-level and no structural enforcement.

---

## 1. Scope of application (when this spec binds)

Binds the verification instance whenever:

- **S1** the merger gate spawns it for a bugfixing branch — the delivering
  ledger row carries the `bug` tag (`hasBugTag`; predicate
  `MergeVerifyRecord.BugfixDetected`, `internal/tools/verify_gate.go:96`,
  selection `cmd/agent/merger_verify_child.go:130`), i.e. a merge of a bugfixing
  branch; or
- **S2** the same gate path resolved a conflict first and then judged the
  resolved state (`cmd/agent/merger_verify_child.go:213-236`).

Does **not** apply to an ordinary clean merge of a non-bug branch: the operator
has settled that the author's own tests are the baseline competence of a
developer and refuses "每个开发跑两遍". The briefings for those flows are out of
scope here.

---

## 2. DO — the four checks, each as a concrete procedure

Each check ends with ONE command whose output is quoted into the verdict digest
(one line per check, §6). All commands run in the repo named by the judged row's
`deliverable.repo` (the gate sets the child's cwd to it,
`cmd/agent/merger_verify_child.go:379`).

### DO-1 — reproduce the original symptom on the MERGED MASTER (the strongest check)

Why it is the strongest: it is the only check that tests the claim that is
actually at stake. A green test suite shows the merge didn't break what was
green; only the symptom's own repro shows the bug is *fixed on the post-merge
state* — and two branches each green alone can break each other.

Procedure:
1. Read the symptom from the judged row: `gopi task show <task_id>` → the payload
   carries the bug report (and, when it exists, the acceptance criteria; the
   closure briefing names this as the acceptance source,
   `internal/tools/task_verify_closure.go:77-79`).
2. Confirm what master is: `git -C <repo> log --oneline -3 master` and check the
   merge is there (`git merge-base --is-ancestor <delivering-branch-tip> master`;
   the gate's event line records `master=<beforeTip>..<afterTip>`).
3. Build the smallest repro — the named test, never the suite:
   `CGO_ENABLED=0 go test -count=1 -run '<Symptom|Identifier1|Identifier2>' ./<pkg>/`.
   Derive the selector from the bug's OWN vocabulary (the observed repo practice:
   a red-proof recipe names the incident's strings, e.g.
   `devdoc/worktree-recycle-verification-20260912.md`).
4. If the row carries no runnable check, WRITE ONE — a throwaway probe, and
   A/B it (this is the verifier's real move; the probe lives in a DETACHED
   scratch worktree, never in the judged checkout — §4(c)1 is what makes this
   write safe, and it is why the write is allowed here while DON'T-1 forbids
   writing in `<repo>`):
   - `git -C <repo> worktree add --detach <scratch>/verify-fixed master`
     where `<scratch>` is a directory the run creates for itself (`mktemp -d`;
     the gate layer's own scratch helper is `scratch.MkdirTemp`,
     `internal/scratch/scratch.go:151`) — never a path inside `<repo>`.
   - write the probe test there (untracked, inside the throwaway worktree);
   - `git -C <repo> worktree add --detach <scratch>/verify-prefix master^1`
     — the commit the fix's own merge introduced first, i.e. master's parent as
     it stands AFTER the merge under judgement (the pre-merge master tip the
     gate recorded; the gate's event line carries it as `master=<beforeTip>`).
     Falls back to the merge commit's first parent when the tip is unknown. A
     probe that needs no A/B (the fix's symptom is trivially reproducible on a
     tree master no longer is) skips this step and says so in the evidence.
   - the probe must **FAIL** in the prefix worktree with the incident's own
     error string and **PASS** on master. PASS in both trees = the probe does
     not test the fix — report that as a gap, never as a pass.
   - remove both worktrees when done (`git worktree remove --force`), and
     confirm `git worktree list` is back to its original set.
5. Evidence: the command + its output tail.

### DO-2 — match the task's OWN acceptance criteria

Procedure:
1. `gopi task show <task_id>` → list the payload's acceptance/definition-of-done
   items verbatim.
2. For EACH item, name the command that exercised it and the observed result
   (a mapping table in the verdict evidence, one item per line; a long list is
   compressed, never dropped).
3. An item you did NOT execute is written into the verdict as
   `not exercised: <item>` — honesty is part of the artifact, not an
   embarrassment (the observed verifier practice on ledger row
   `01a09250-07bd-71fe-a241-580e5477a469`: "honest gap … established by
   inspection instead of faking a red").
4. Evidence: `gopi task show <task_id>` + the mapping lines.

### DO-3 — code-review the fix itself (read-only)

Procedure:
1. Get the diff the fix actually consists of:
   `git -C <repo> log --oneline --merges -3` → the merge commit; then
   `git -C <repo> show <merge>^1..<merge>^2 --stat` and read the hunks.
2. Ask the four review questions, in this order:
   - does the diff address the symptom's **mechanism**, or only its **string**
     (a guard that matches the exact error text, a special case planted for the
     test)?
   - is the fix **test-only** (no production change) for a bug that manifested
     in production?
   - does the fix **narrow or delete an existing guard**? If yes, is there a
     test asserting the guard still protects its original (live) case — the
     "safety twin" rule (PASS in BOTH trees)?
   - do the new lines re-inline a literal that already has a named constant
     (Rule 1b/13 drift), or swallow an error (`_ =`, `//best-effort` without a
     reason — Rule 15)?
3. For a **conflict-resolved** merge (S2) the same questions apply to the
   RESOLUTION: both sides' intent present in the merged hunks, neither silently
   dropped (the resolve briefing's step 2 states that as the resolver's own duty
   — `internal/tools/repo_resolve_row.go:133-134`, *"combine both sides where
   both are meaningful, do not silently drop either side's intent"* — the
   verifier checks whether it was met on the merged result).
4. A suspicion is **recorded, not repaired** (DON'T-7).
5. Evidence: `git -C <repo> show <merge>^1..<merge>^2 --stat` + the review lines.

### DO-4 — regression

Procedure:
1. Build: `CGO_ENABLED=0 go build ./...`.
2. Format on the fix's files: `gofmt -l <the fix's touched .go files>` → must be
   empty.
3. Tests of the packages the fix touched (never the whole tree —
   `mergeVerifyChildWaitTimeout`, §5):
   `go test -count=1 ./<touched-pkg>/ …`; add `./internal/standards/` when the
   fix touches that tree (the repo's own six machine-checkable gates).
4. Safety twin for a loosening fix: run the assertion that the OLD protection
   still holds, in the pre-fix tree AND on master — PASS/PASS proves the fix
   narrowed a gate instead of removing it (a safety case that passes only
   post-fix proves nothing; the observed verification recipe,
   `devdoc/worktree-recycle-verification-20260912.md`).
5. Evidence: the build/gofmt/test commands + output tails.

---

## 3. DON'T — individually checkable prohibitions

Each item names its check, so a reader can judge an actual run.

- **DON'T-1 — no writes to the judged repo.** Do not create, edit, delete or
  `git add` any tracked file in `<repo>`, and do not leave untracked files
  behind there. The ONLY place this run may write is a scratch directory
  (DO-1.4's detached probe worktree, §4(c)1).
  *Check:* `git -C <repo> status --porcelain` at the end of the run equals its
  value at the start (captured before the first command).
- **DON'T-2 — no refs may move.** No `commit`, `merge`, `rebase`, `reset`,
  `checkout`, `tag`, `branch -d`, `push`, `fetch`, `pull`, `stash`.
  *Check:* `git -C <repo> rev-parse master` and `git -C <repo> reflog -5` are
  unchanged by the run.
- **DON'T-3 — no worktree destruction.** Creating a DETACHED scratch worktree
  for a probe is allowed (DO-1.4); touching, removing or reconfiguring any OTHER
  worktree or branch is not. *Check:* `git -C <repo> worktree list` before and
  after are identical except for the verifier's own detached probe entries.
- **DON'T-4 — no fixing.** Do not repair the bug, the fix, the tests or the
  merge. *Check:* the diff of the run (DON'T-1) is empty.
- **DON'T-5 — no refactoring, no opportunistic improvement, no scope growth.**
  Do not "improve" anything outside the fix's remit, including style and
  comments. *Check:* there is nothing to check — the run left no diff at all
  (DON'T-1 is the mechanism).
- **DON'T-6 — no devdoc/test-file green-washing.** The fix's own devdoc, its own
  test names, or its own claims are not evidence; evidence is a command YOU ran
  and its output tail. *Check:* every digest entry names a command and quotes
  output.
- **DON'T-7 — a separate problem is FILED, not fixed.** If verification surfaces
  a different bug, the verdict names it and stops there; the forward path is a
  new task row (`gopi task submit`), not an edit in this session.
  *Check:* D1/D4 empty + the verdict text naming the separate problem.
- **DON'T-8 — the judged row's history is not rewritten.** Only
  `completed|failed → verified` or `completed → failed` (the local transition
  table, `internal/tools/tasks.go:3406`, `taskAllowedTransitions`: `verified` has
  no outgoing edge). No other status, no second verdict write.
  *Check:* exactly ONE new terminal write attributable to this session (one
  `task_events` row for `verified`/`failed`), and the row is not written again.
- **DON'T-9 — one verdict, at the end.** Do not write a provisional verdict and
  then another. The write is the last action.
  *Check:* as DON'T-8.
- **DON'T-10 — no process/host side effects.** No `pkill`, no killing sockets or
  other gopi sessions, no daemon restart, no `crontab`, no service files.
  *Check:* no process of the run remains (`ps` shows nothing spawned by it).
- **DON'T-11 — no other repo, no network.** Judge the one repo the row names.
  *Check:* the run's commands name only that repo (plus `/tmp` or a scratch dir
  it created).
- **DON'T-12 — never the whole test suite as the verification.** `go test ./...`
  is banned inside the bound (§5.4); it is a known full-suite-hang risk and it
  consumes the entire budget without adding a check the four above miss.
  *Check:* no `./...` argument in the run's test invocations.

---

## 4. Anti-drift mechanism — what exists TODAY, and what does not

Three candidate levels; only two are available.

**(a) Instruction-level (in the briefing) — available today, insufficient
alone.** That is the current state: one sentence, `internal/tools/tasks.go:4244`
("do NOT fix the bug yourself; a verifier that fixes is a developer, not a
verifier"), repeated in the closure payload
(`internal/tools/task_verify_closure.go:89`). Deliverable A §3: this is
persuasion, and nothing machine-checkable backs it.

**(b) Tool-level read-only surface — NOT AVAILABLE today; do not claim it.**
Verified by the audit: the tool list is one global slice
(`internal/agent/loop.go:946` `var claudeToolDefs = …`, fed verbatim at `:2925`);
there is no per-session allow/deny list, no read-only mode, no role-scoped tool
filter in `internal/agent/` or `internal/config/`. The two near-misses are
different things: `gopilog.FormatToolFilter` (`internal/gopilog/formatter.go:380`)
renders a session log, and `gopi chat`'s non-tool path
(`cmd/agent/cli_chat.go:315-338`) is a no-tool Q&A mode. Building (b) is a
feature with its own task; this spec states plainly that it does not exist and
supplies the substitute below.

**(c) Structural side-effect guard — RECOMMENDED, and available today.** Two
halves, no new tool, no new store:

1. **Scoped working copy (prevent).** The child judges a DETACHED WORKTREE at
   master, never the shared checkout — `git worktree add --detach <tmp>/verify
   master`, where `<tmp>` comes from the existing scratch helper
   (`scratch.MkdirTemp`, `internal/scratch/scratch.go:151`; the resolve child
   already uses exactly this pattern for its own isolated scene,
   `cmd/agent/merge_resolve_child.go:111` + `internal/scratch/scratch.go:112`
   `RuntimeClassMergeResolve`). Effect: a drifting verifier's writes land in a
   directory that is discarded at the end of the run — the shared tree the
   merger owns can no longer be dirtied by the act of verifying. The one-line
   briefing instruction is the delivery mechanism; the git command is the
   mechanism.
2. **Machine-decidable post-check (detect).** The parent that spawned the child
   records the judged repo's dirty state BEFORE the child runs and AFTER the
   verdict, using the EXISTING untracked-inclusive probe
   `tools.AdoptionGitStatus(repo)` (`internal/tools/task_adoption.go:137-138`,
   `git status --porcelain`, deliberately untracked-inclusive per its own doc) —
   the same quantity the merge path already refuses to merge onto
   (`internal/tools/task_merge_executor.go:200-215`). A non-empty after-state,
   or an after≠before, is recorded on the verdict event as
   `VERIFIER-LEFT-DIRT: <porcelain>` so it reaches the operator via the existing
   notify lane. Today the gate does NOT do this: `runMergeVerifyChild`
   (`cmd/agent/merger_verify_child.go:370-424`) reads only the ledger verdict.
   **This is a requirement on the gate owner (`01a09237-f56a`), not an
   implementation here** — the spec defines the check; the gate's code is that
   task's.
3. **Human audit path (existing).** The child's session JSONL already exists and
   is already addressable: the ledger's `verify_evidence` names the child
   session (`internal/tools/tasks.go:4285-4286`,
   `verified-by gopi task verify (child session <id>): …`) and
   `gopi log <session-id> --filter TOOL` (`cmd/agent/cli_log.go:57`) renders
   exactly which tools that session called — the only reliable way today to
   answer "did the verifier write anything" after the fact.

**Ruling:** adopt (c). It is instruction + a two-value comparison over an
existing probe — zero new dependencies, and it makes drift *detected by
mechanism* instead of *punished by convention*. (b) remains the long-term answer
and is explicitly out of scope here.

**Does (c) retire (a)?** No — and the distinction is the point of this section.
(a) is the **delivery mechanism**: §2/§3 are prose, and the only thing that
carries them into a spawned child is the briefing line
(`internal/tools/tasks.go:4244`). (c) is the **enforcement substrate**: it makes
the drift *ineffective* (the write lands in a discarded worktree) and *visible*
(the `VERIFIER-LEFT-DIRT` event) instead of merely *forbidden*. So the honest
statement of what today's mechanism covers is:

| drift mode | (a) instruction | (c) substrate |
|---|---|---|
| edits the checkout it was told to edit | forbidden | prevented — writes land in `<scratch>`, `<repo>` stays clean |
| edits `<repo>` directly (ignores the scratch instruction) | forbidden | not prevented, but DETECTED (`git status --porcelain` ≠ before) and recorded |
| decides the fix is wrong and rewrites it in `<scratch>` | forbidden | not detected — the scratch tree is discarded at run end, exactly like the resolve child's (`cmd/agent/merge_resolve_child.go:111`) |

The third row is the residual risk: (c) bounds the blast radius of a drifting
verifier (nothing it writes can reach master, because the gate judges the LEDGER
verdict and reverts by the recorded pre-merge tip — it never reads the working
tree, `cmd/agent/merger_verify_child.go:415-422`), but it does not force the
verifier to have *judged* rather than *repaired*. Addressing that needs (b).

---

## 5. Cost bound — numbered, with the on-timeout default

The bound is written as numbers because the operator's constraint is a number:
*"不能为了 verify 把整个 merge 搞得很慢，那是得不偿失"*.

1. **Wall clock: 15 minutes, whole run.** This is not a new number — it is
   `mergeVerifyChildWaitTimeout = 15 * time.Minute`
   (`cmd/agent/merger_verify_child.go:64`), the cap the merger already blocks on;
   the verifier's own work must fit INSIDE it. Precedent for the shape: the
   resolve child is capped at 20m (`cmd/agent/merge_resolve_child.go:58`) — a
   verifier does strictly less work than an editor, so a tighter cap is the
   honest ordering.
2. **Internal allocation of the 15 minutes** (a plan the run's transcript can be
   judged against): ≤1 min locate (DO-1.1–1.2); ≤6 min the smallest symptom repro
   incl. the A/B probe (DO-1.3–1.4); ≤3 min reading — acceptance mapping and the
   diff review (DO-2/DO-3); ≤5 min the build + targeted tests + safety twin
   (DO-4); the remainder is the verdict write. Any check that cannot fit is
   recorded as `not exercised` (DO-2.3) rather than silently dropped.
3. **Command budget: ≤ 20 bash commands, ≤ 4 test invocations** (`go test`), and
   EXACTLY ONE of them is the evidence-carrying command per check. A verifier
   that has spent 20 commands without a verdict has drifted (most plausibly into
   DO-4 → DON'T-4) and must write FAIL/`reason:` with what it has.
4. **Test scope: the packages the fix touched, never `go test ./...`** (DON'T-12).
   The full suite is a multi-minute run on this box and is deliberately left to
   the merge machinery's own gates (`devdoc/bug-verify-closure-20260912.md`
   records the same split).
5. **On timeout — default = the merge is NOT kept.** At the cap the merger kills
   the child AND its process group
   (`cmd/agent/merger_verify_child.go:405-410`, `Setpgid` at `:390`) and returns
   `mergeVerifyVerdictChildTimeout`, which takes the unwind path with
   `MergeVerifyReasonChildTimeout = "verify child exceeded the merger's wait cap"`
   (`internal/tools/verify_gate.go:62`), reverting master to the recorded
   pre-merge tip and filing the rework row. **Justification:** this is the
   gate's own stated rule — "no-verdict → revert, never an unverified keep"
   (`devdoc/merger-verify-gate-20260912.md`, "Verdict = the ledger, not the exit
   status") — and the failure costs are asymmetric in the operator's own
   ranking: a false revert costs one rework round, a false keep ships a
   possibly-regressed bugfix onto master. A gate that cannot finish judging must
   not bless.
6. **Cannot-execute default — fail closed, same reasoning.** If NO check can be
   executed (no runnable symptom check exists and no probe can be written), the
   verifier writes `--status failed --result "reason: no runnable check for
   <task_id> (checked: <what was tried>)"`. It must NOT write a PASS on
   inspection alone: an unverifiable bugfix is precisely the case this gate
   exists for, and the FAIL lands as a rework row a human can read.
7. **Loop bound (not part of the 15-minute budget — listed here so the two are
   not confused): unchanged, owned by the gate.** At most ONE gated branch per
   merger pass (`cmd/agent/merger_verify_child.go:121-137`) and
   `MergeVerifyFailCap = 3` terminal-FAILED verdicts per (repo, branch) before
   the gate stops filing and records a human-decision row
   (`internal/tools/verify_gate.go:55`). This spec adds no counter and no retry.

---

## 6. Verdict shape and record location (existing machinery only)

**The verifier writes exactly one of two commands — one line, at the end:**

```
PASS: gopi task update <task_id> --status verified --result "evidence: <cmd1> -> <tail1>; <cmd2> -> <tail2>; not exercised: <item>"
FAIL: gopi task update <task_id> --status failed   --result "reason: <what failed>; tried: <cmd> -> <tail>"
```

Machine-usable properties (all enforced by code that exists today):

| property | enforcement |
|---|---|
| `evidence:` prefix REQUIRED for a `verified` write | `TaskVerifyEvidenceArgs` (`internal/tools/task_verify_closure.go:170`), prefixes `:195`/`:199`, CLI refusal `cmd/agent/cli_tasks.go:3419-3420` |
| exactly two legal verdict values (`verified` / `failed`) | `taskAllowedTransitions` (`internal/tools/tasks.go:3406`); the gate reads `TaskStatusVerified`/`TaskStatusFailed` and nothing else (`cmd/agent/merger_verify_child.go:415-422`) |
| one write, no history rewrite | `verified` has no outgoing transition (same table); append-only rework row (`TaskVerifyToReopen`, `internal/tools/tasks.go:9609`) |

**Where the verdict is recorded** (four existing places, no new store — Rule 14):

1. **The judged row's status** — `verified`/`failed`, written by
   `TaskUpdateReason` (`internal/tools/tasks.go:4419`, invoked by the CLI at
   `cmd/agent/cli_tasks.go:3425`).
2. **`task_events`** — the digest lands there (bug056: the summary is
   immutable; the digest is the event detail).
3. **`verify_evidence`** — the PARENT stamps
   `verified-by gopi task verify (child session <id>): <event tail>`
   (`internal/tools/tasks.go:4274-4291`).
4. **The gate's event + notify** — `MergeVerifyRecordEvent` /
   `MergeVerifyNotify` on event name `merge-verify`
   (`internal/tools/verify_gate.go:48,398,410`), delivered through
   `NotifySyntheticEvent` (`internal/tools/taskheartbeat.go:40`) to the operator's
   `GOPI_TASK_NOTIFY_EVENTS` subscription (`internal/tools/tasknotify.go:23-25,96`).
5. **The child's session JSONL** — the raw audit trail
   (`gopi log <session-id> --filter TOOL`, `cmd/agent/cli_log.go:57`).

**Ruling:** the verifier's contract is the two commands above and nothing else.
Any richer verdict (extra fields, new statuses, a new event name) is out of
scope — the gate's verdict channel is the row status, and widening it would
break the gate that already reads it.

---

## 7. What this spec does NOT cover (explicit)

1. **The gate itself** — revert logic, the pre-merge tip record, the rework
   filing, the caps, the notify wiring. Owner: `01a09237-f56a`.
2. **Wiring verification into the bug flow** — the closure gate, the enqueue,
   the trigger site. Owner: `01a09216-6561`.
3. **The terminal-notify gap** — `01a09225-a216`. This spec only names which
   existing notify call carries a verdict.
4. **Ordinary clean merges of non-bug branches** — the operator settled that the
   author's own tests are the baseline; no second-run gate is proposed.
5. **A tool-level read-only surface for the verifier** — does not exist today
   (§4b); this spec supplies the substitute and says so instead of pretending.
6. **General testing methodology** — no test-design survey, no coverage targets,
   no fuzzing/performance/security review beyond the fix's own diff. Every check
   above maps to a command a spawned child can literally be told to run.
7. **Verifying the verifier** — a fabricated evidence digest remains possible;
   the closure epic states this as an honest limit
   (`devdoc/bug-verify-closure-20260912.md:366-372`). This spec makes the lie
   explicit and auditable (session JSONL + `verify_evidence`), not impossible.
8. **Cost tuning of the gate's own numbers** — the 15m cap and the cap of 3 are
   cited as-is from `cmd/agent/merger_verify_child.go:64` and
   `internal/tools/verify_gate.go:55`; changing them belongs to the gate owner.
9. **Any product code or new tool** (Rule 3) — this is a specification of a
   boundary, and it is delivered as documents only.
