<!-- doc-class: historical -->
# The plain prompt's first request carries no turn stamp — as delivered, and how the A4 fixtures converged

Task `01a12c5f-1eb6-7cbe-b67d-2691d89509dc` · branch
`task/01a12c5f1eb67cbeb67d2691d89509dc` (cut point `30a3a6870`). This file is
the write-up the delivery originally lacked: the exemption in
`internal/agent/clock.go` (`turnStampForRequest`) landed on master in `cafe04e48`
**without a dedicated devdoc** (its only doc-side trace was one line of
`devdoc/chat-turn-stamp-census-double-filter.md`), and it records the verdict of
the task that was dispatched to re-apply it.

## The contract

A plain prompt (`gopi [options] "<prompt>"`, `Agent.plainPrompt`) reaches the
model on its FIRST request as the operator's text ALONE — no turn stamp, no
budget clause. The reason is a wire property, not a preference: the stamp is
appended as a user-role message (`requestMessages`) and
`deepseek/claude_client.go` `BuildClaudeRequest` MERGES consecutive user text
(the Messages API requires alternating roles), so on this surface "appended"
means "glued INSIDE the operator's own text" — which `gopi -h`'s
`"<prompt>"  The task text` forbids. From the second request on there is an
assistant turn in between, so the stamp gets its own user slot and the run keeps
its time base; the exemption costs the run one tick, never its clock.

## The verdict this task was dispatched to reach: nothing to re-apply

The epic's premise was that the change had to be **re-applied** onto master,
because master already carried "landed A4 fixture restatements (6ab91facd,
7ae446e47) of the SAME relation in DIFFERENT words" for
`cmd/agent/chat_voice_failure_run_test.go` and `cmd/agent/cli_lineedit_test.go`.
**Measured, that premise does not hold on this ground, and the deliverables are
already here.** The generator for every turnover figure below is plain `git` at
the named revision (`git show <rev>:<path> | grep -c <phrase>`; the raw output is
`artifacts/03-convergence-census.out` in this run's session directory), and the
figures are as measured at ground `30a3a6870` / master `0ab59bf72`:

| fact | measurement |
|---|---|
| the exemption | `cafe04e48` (2026-10-12 05:18:18) is an **ancestor of this ground** — `git merge-base --is-ancestor` |
| its in-process test | `internal/agent/plainprompt_turnstamp_test.go` carries `TestPlainPrompt_FirstRequestCarriesNoTurnStamp`; the ground has NO `plainprompt_firstmessage_test.go` |
| the RP-1 comment | `cmd/agent/cli_runpath_boundary_rp1_test.go` mentions `turnStampForRequest` twice (lines 257, 285) |
| the two A4 fixtures | ground `30a3a6870` and master `0ab59bf72` are **byte-identical** for both files (`git diff --stat` prints nothing) |
| the ordering the epic assumed | `abf7f4c` (02:39:07) does **not** contain `6ab91facd` (01:39:04) or `7ae446e47` (02:39:02) — `git merge-base --is-ancestor` says NOT for both, so its `+` hunks for those two files are a restatement **against a base that predated the landed one**, not a second wording landing beside it |
| the convergence | the branch wording appears **0** times in the ground (1 time in `abf7f4c`); the master wording appears 2 / 1 times in the ground |

**And convergence is not a wording preference here — it is forced by the
residue.** `abf7f4c`'s `cli_lineedit_test.go` hunk is *not* a comment rival: it
edits the pre-`6ab91facd` literal census (`sess.UserContents()`, asserting
`len(injected) == reqs`), which on the ground cannot pass for a measured reason —
`testutil.Session.UserContents()` is ALREADY the operator-only view
(`internal/testutil/session.go`, filtering through the production predicate
`sessionevent.IsHarnessInjected`), so the branch's own `injected` slice is
always empty and the assertion compares 0 stamps against 2 requests. The landed
form reads `AllUserContents()` for exactly that reason
(`devdoc/chat-turn-stamp-census-double-filter.md`, the landed diagnosis). Taking
master's form is therefore not merely the epic's tie-breaker — the alternative is
a red test.

**What is genuinely the branch's, and is already on the ground by supersession:**
the exemption itself (`turnStampForRequest`'s body is byte-identical between
`abf7f4c:internal/agent/clock.go` and the ground — measured) and the RP-1 arm's
comment; the voice fixture's own assertion (`waitForBound` on the conjunction
`FAILED row && !mode.active()`) and the lineedit fixture's split-and-relation
assertion, both delivered by later tasks (`01a12c12`, `01a12c70`, `6ab91facd`,
`7ae446e47`) in master's canonical A4 phrasing.

**The original branch ref is gone, and that is measured, not assumed.** The epic
asked for `task/01a12be312bc78c78e0f808ea3200dc1` to be deleted once the work
landed; it exists on no ref of this host (`git for-each-ref | grep 01a12be3` →
empty; absent in the main repo, this worktree and `merge-run-01a12c5d`). No
deletion was performed because there is nothing to delete. Its tip commit
`abf7f4c` survives as an unreferenced object — `git show abf7f4c` still reads,
which is how every figure above was taken.

## What this run measured, rather than inferred

- **Red proof (Rule 6, Rule 31).** `requestMessages` was mutated to call
  `turnStampLine` — the pre-fix shape, i.e. every request stamped — and BOTH arms
  went red on the reverted code, in-process and at the real request boundary:
  `artifacts/04-redproof.out` carries both verbatim, the boundary arm printing the
  same 128-char first user message the RP-1 defect always printed
  (`"RP1-BOUNDARY-PLAIN-PROMPT-4f2a[system] [t+0s] — first request of this
  session. session budget: 0s elapsed of 1h0m0s (0% used)."`, want the 30-char
  token). Restore was verified by BLOB IDENTITY, not by an index-relative diff
  (`git hash-object internal/agent/clock.go` == `git rev-parse
  HEAD:internal/agent/clock.go` == `1ce9362f517a7e7b7a2c80f7117e9d3cd0f7f2c2`);
  `artifacts/05-restore-verified.out`.
- **Green, on the unmodified ground:** `artifacts/00-inprocess-arm.out`
  (`TestPlainPrompt_FirstRequestCarriesNoTurnStamp` → `ok internal/agent 4.054s`)
  and `artifacts/01-rp1-arm.out` (`TestRunPathPlainPromptIsVerbatim` →
  `ok cmd/agent 8.908s`).
- **The gap this file closes:** none of the exemption's reasoning was in
  `devdoc/` before it — only in `clock.go`'s own comments. The reasoning is left
  where the code is (it is read by whoever edits the function), and this file is
  the corpus-side record of WHY the branch that carried it did not land.

## Not Done

- **No code, test or fixture was re-applied, because this ground already carries
  all of it.** Reporting "re-applied" would be false; the honest result is
  "already present, in master's canonical form", which is what the table above
  measures. The three deliverables the epic enumerated as needing writing — the
  three-arm test, the RP-1 comment, the two converged fixtures — exist under
  other file names, delivered with the same assertions.
- **`abf7f4c`'s own devdoc text was not transplanted.** Its prose is superseded
  in places (it names fixtures this ground has since restructured) and its
  central claim is already in `clock.go`; transplanting paraphrased prose about
  code would create drift, not a record.
