<!-- doc-class: normative -->
# Audit: the responsibility boundary in our EXISTING verify / resolve briefings

Task: `01a09242-c9cf-7841-b9f2-b9eef0043928` (verify 职责边界研究) — Deliverable A.
Date: 2026-09-12. Base: master `8aa1f9b5`. Author: docs-only task, no product code.

Scope: read the **existing** briefings that another gopi instance is fed when it
judges or repairs a delivered artifact, and answer three factual questions:

1. does the briefing define a responsibility boundary?
2. what stops drift (the child doing more than judging)?
3. what catches drift when it happens anyway?

Every claim below was read out of the code at the cited `file:line` on this
branch's base (worktree `~/workspace/gopi-verifyspec`, master `8aa1f9b5`).

---

## 1. The three existing briefings

### 1.1 `TaskVerify` — `gopi task verify <task_id>` (the verifier)

`internal/tools/tasks.go:4346-4353` — the whole briefing, verbatim:

```go
	briefing := fmt.Sprintf("You are an independent verification instance for ledger task %s (%q). "+
		"Locate the delivered fix, build the project, and run its own tests against the acceptance criteria below. "+
		"Then record your verdict with the task CLI (bash): PASS → "+
		"`gopi task update %s --status verified --result \"evidence: <the test command> + its output tail\"`, "+
		"or FAIL → `gopi task update %s --status failed --result \"reason: <what failed>\"` (rework is REISSUED as a new task — the judged row stays terminal). "+
		"Record the verdict EXACTLY ONCE, then stop — do NOT fix the bug yourself; a verifier that fixes is a developer, not a verifier.\n"+
		"Acceptance criteria / payload: %s",
		taskID, target.Summary, taskID, taskID, orDash(target.Payload))
```

Named constants around it (same file): `taskVerifyBinTimeout = 0` (`:6244`,
"0 keeps the child unlimited"), `taskVerifyStderrTail = 400` (`:6254`),
`taskVerifyReopenMaxPending = 3` (`:6250`).

### 1.2 The closure row's payload (the other verifier briefing)

`internal/tools/task_verify_closure.go:72-89` — the payload of the
`verify-closure` row the closure gate enqueues at a bug row's terminal
writeback (`internal/tools/task_verify_closure.go:345` `taskVerifyClosure`,
called from `internal/tools/tasks.go:2544`). Its operative lines, verbatim:

```go
	taskClosurePayloadFormat = "Independent verification of ledger row %s.\n\n" +
		"Who you are: a DIFFERENT process and session from the instance that delivered the fix. " +
		...
		"What you do: locate the fix from the judged row's deliverable (branch/repo/worktree), " +
		"build it and run the row's OWN acceptance criteria (`gopi task show %s` carries the payload " +
		"and the appended run result).\n\n" +
		"Preferred — one command does the whole loop for you (it spawns the verification child and " +
		"reads the verdict back): `gopi task verify %s`. If that is unavailable to you, run the " +
		"locate/build/test pass yourself and write the verdict as below.\n\n" +
		"How you write the verdict — the closure command, exactly once:\n" +
		"  PASS: gopi task update %s --status verified --result \"evidence: <the test command> + its output tail\"\n" +
		"  FAIL: gopi task update %s --status failed --result \"reason: <what failed>\"\n\n" +
		"After a PASS the judged row is terminal (verified) and nothing else happens. After a FAIL you " +
		"reissue the rework as a NEW row — the judged row stays terminal and its history is never rewritten:\n" +
		"  gopi task reopen %s --reason \"<what failed>\"\n\n" +
		"Record the verdict EXACTLY ONCE, then stop — a verifier that fixes the bug is a developer, not a verifier."
```

*(The `gopi task reopen` line inside that quoted 2026-09-12 snapshot is QUOTED HISTORY — that verb was deleted 2026-10-21 for minting children of a failed row, which Rule 33's gate never releases. The live briefing names a fresh `gopi task submit` instead; see `devdoc/task-verify-loop.md`.)*

### 1.3 The merger's resolver child (the repairer — NOT a verifier)

`internal/tools/repo_resolve_row.go:124-158`, `MergeResolveBriefing(repo, master,
branch)` — one text fed BOTH to the spawned resolve child
(`cmd/agent/merge_resolve_child.go:142` `briefing := tools.MergeResolveBriefing(repo, master, branch)`,
piped in at `:145`) and stored as the resolve row's payload. Its operative
lines, verbatim:

```go
	fmt.Fprintf(&b, "4. COMMIT FIRST — your commit IS the deliverable. As soon as every marker is "+
		"gone and the resolved files are staged, `git commit` them … Never "+
		"let a long verification run delay the commit: this session is killed at a fixed cap, and "+
		"an uncommitted resolution is lost work, while a committed one is preserved.\n", …)
	fmt.Fprintf(&b, "5. THEN verify, with a BOUNDED check: the project's build (`go build ./...` for "+
		"a Go repo) plus the tests of the package(s) your resolution touched "+
		"(`go test ./<pkg>/ -run <Test>`). A full-suite run (`go test ./...`) is NOT required here "+
		"and must never block your exit — this repo has a known pre-existing full-suite hang … "+
		"If a check hangs, kill it and finish … Fix only what your resolution actually broke, and amend "+
		"the commit if you fix something.\n")
	fmt.Fprintf(&b, "6. Once the commit is on branch %q, you are DONE — say so and stop; do not keep "+
		"polling tests or git state.\n", branch)
	fmt.Fprintf(&b, "\nCONSTRAINTS:\n- Work ONLY inside this worktree. Never touch the main checkout's git "+
		"references, never push, never `branch -d` anything. When you have committed your resolution, "+
		"you are done — the merger re-merges and cleans up.\n")
	fmt.Fprintf(&b, "\nACCEPTANCE (you pass when): the merge reproduces, you resolved every hunk, and your "+
		"resolution is COMMITTED on branch %q. A bounded build/test check of the touched packages is "+
		"expected but a hanging check never blocks your exit. Then say you are done.\n", branch)
```

---

## 2. Q1 — does the briefing define a boundary?

| briefing | boundary sentence(s) | shape of the boundary |
|---|---|---|
| `TaskVerify` (`tasks.go:4244`) | `"Record the verdict EXACTLY ONCE, then stop — do NOT fix the bug yourself; a verifier that fixes is a developer, not a verifier."` | ONE sentence, negative, **role-identity rationale** ("is a developer"). No scope of files, no "stay on master", no budget, no list of forbidden actions beyond "fix the bug". |
| closure payload (`task_verify_closure.go:89`, `:86-88`) | same one-liner + `"Record the verdict EXACTLY ONCE"` + `"the judged row stays terminal and its history is never rewritten"` + `"After a PASS … nothing else happens"` | one negative sentence + a stop condition + an append-only rule. Still **instruction-level only**: no repository scope, no budget, no read-only claim. |
| resolve briefing (`repo_resolve_row.go:152-154`, `:137-149`) | `"Work ONLY inside this worktree. Never touch the main checkout's git references, never push, never branch -d anything."` / `"Fix only what your resolution actually broke"` | a POSITIVE scope for an EDITOR (its job *is* to write code — it owns the commit). There is **no verifier boundary at all** here; a resolver that rewrote the world inside its own worktree would still satisfy this text. |

**Conclusion for Q1:** the two verifier briefings carry one shared sentence of
role boundary (do not fix), plus stop/append-only rules. The resolver briefing
carries a *worktree* boundary and a *commit-first* rule, and no judging
boundary — correctly so, because the resolver is the author, not the judge.
None of the three defines: scope of files to touch, where it must run, whether
it may leave the tree modified, or how much time it may spend.

---

## 3. Q2 — what stops drift?

**Answer: instruction text only. There is no tool-level and no structural
surface today.** Evidence:

1. **The child is a full, unrestricted gopi agent.** `TaskVerify` spawns
   `exec.CommandContext(ctx, gopiExecutablePath(), "--timeout", …)`
   (`internal/tools/tasks.go:4249`) and feeds the briefing on stdin — nothing
   else is configured. The tool surface is one global slice,
   `var claudeToolDefs = []deepseek.ClaudeToolDef{…}`
   (`internal/agent/loop.go:826`, passed verbatim at `:2925`); there is **no
   per-session tool allow/deny list, no "read-only" mode and no role-scoped
   tool filter** anywhere in `internal/agent/` or `internal/config/` (checked
   by full-text search for tool-gating identifiers; the only hits are the
   log-rendering filter `gopilog.FormatToolFilter`, `internal/gopilog/formatter.go:380`,
   and the `gopi chat` "no tools" Q&A path, `cmd/agent/cli_chat.go:315-338` —
   neither is a per-run tool policy).
2. **The child shares the live ledger and the repo.** `GOPI_MEMORY_DB` is
   inherited (`cmd/agent/cli_tasks.go:3793-3794`: "the child inherits the env, so
   both instances read and write the SAME ledger"); the merger gate even passes
   its own cwd (`cmd/agent/merger_verify_child.go:379` `child.Dir = recRepoDir(rec)`)
   and its own environment (`:380-383`: `AutoRegisterOffEnv`,
   `mergeVerifyRoleEnv`). It has `bash`, `write`, `edit` in its tool list.
3. **The only hard constraints sit on the LEDGER WRITE, not on the work.**
   - `internal/tools/task_verify_closure.go:170` `TaskVerifyEvidenceArgs` and
     `:195/:199` `taskVerifyEvidencePrefix = "evidence:"` /
     `taskVerifyReasonPrefix = "reason:"` — a `--status verified` write is
     REFUSED without a digest that starts with `evidence:` (rendered by
     `TaskVerifyEvidenceError`, `:188`; enforced in the CLI at
     `cmd/agent/cli_tasks.go:3419-3420`).
   - `internal/tools/tasks.go:3406` `taskAllowedTransitions`: `completed →
     verified|failed`, `failed → verified|completed`, `verified → {}` — the
     verdict is *writable*, the history is not rewritable.
   - `internal/tools/tasks.go:4274-4291`: the parent stamps `verify_evidence`
     (`"verified-by gopi task verify (child session %s): %s"`) — the record is
     the parent's, not the child's claim.

   All three constrain **what the verdict write may look like**. None of them
   constrains what the child DOES to the repository before writing.
4. **There is no time bound inside the verifier's own spawn either.**
   `TaskVerify` launches the child with `--timeout taskVerifyBinTimeout` and
   `taskVerifyBinTimeout = 0` (`internal/tools/tasks.go:6244`, "0 keeps the
   child unlimited"); the CLI passes it through at `:4249`. The ONLY wall-clock
   bound the verifier has ever been under is the merger gate's own wait cap
   (`mergeVerifyChildWaitTimeout = 15m`,
   `cmd/agent/merger_verify_child.go:64`) — which exists on the gate path and
   on no other (a closure-row invocation is unbounded).

**So the drift is stopped by persuasion alone** — the brief text — and the
mechanisms that look like guardrails (the evidence digest, the transition
table) guard the *report*, not the *action*.

---

## 4. Q3 — what catches drift when it happens anyway?

1. **Nothing in the gate reads the repo for a change made by the verifier.**
   `cmd/agent/merger_verify_child.go:380-424` (`runMergeVerifyChild`) reads the
   verdict out of the LEDGER — `case tools.TaskStatusVerified: return
   mergeVerifyVerdictPass` (`:415`), `case tools.TaskStatusFailed` (`:417`),
   anything else → `mergeVerifyVerdictChildDead` (`:422`). The merger's own doc
   states the rule out loud: *"Verdict = the ledger, not the exit status"*
   (`devdoc/merger-verify-gate-20260912.md`). The gate never diffs the judged
   row's branch, never checks whether the tree the verifier judged is the tree
   the author delivered, and never inspects the verifier's own file writes.
2. **The evidence digest is required but not verified.** Stated as an honest
   limit by the closure epic itself:
   `devdoc/bug-verify-closure-20260912.md:366-372` — *"**A verifier that
   fabricates its evidence.** The evidence digest is required to be *named* as
   evidence; its content is unverified free text. The gate converts 'silent
   self-report' into 'self-report that must lie explicitly', which is auditable
   but not tamper-proof."*
3. **What actually exists as a trace:** the child's session JSONL. The merger's
   child spawn inherits stdout/stderr to the merger log
   (`cmd/agent/merger_verify_child.go:384-385`), and every gopi session is
   logged; `gopi log <session-id> --filter TOOL` (`cmd/agent/cli_log.go:57`)
   renders exactly the tool calls a session made. The ledger's record of who
   verified points at that session: the `01a09250-07bd-71fe-a241-580e5477a469`
   row's `verify_evidence` reads
   `verified-by gopi task verify (child session ae89a16a-2402-47ba-9466-93c08c03c298): …`.
   **This is post-hoc human inspection, not a gate.** Nothing fires an event on
   "the verifier wrote to the repo".

**Conclusion for Q3:** drift is currently *auditable after the fact* (session
JSONL + the verdict event) and *not caught*. The verdict machinery cannot even
distinguish "the verifier only ran tests" from "the verifier also edited files"
— except by a human reading the session log.

---

## 5. Prior art (what already exists, so this spec does not duplicate it)

Ledger (read with `sqlite3 -readonly ~/.gopi/memory.db`):

| row | status | deliverable / scope |
|---|---|---|
| `01a09216-6561-7213-9df9-c228e6f409c6` | `completed` | "bug fixes never reach verified — 0 of 209 bug-tagged rows are independently verified". Implemented by commit `da3c2960` (`fix(tasks): close the verify loop …`, ancestor of master): the closure gate + payload (`internal/tools/task_verify_closure.go`) + the evidence-digest requirement + the CLI refusal (`cmd/agent/cli_tasks.go:3419-3420`). **Owns: wiring verify into the bug flow.** This spec must NOT re-design that trigger. |
| `01a09225-a216-7453-ba0f-2513cf2f8a60` | `completed` | "terminal transitions never notify: completed/failed never reach the operator". **Owns: the terminal-notify gap.** This spec must NOT propose the notify fix; it may only name which existing event/notify call a verdict already uses. |
| `01a09237-f56a-7af6-8709-9a4825f1a2f4` | `completed` | the merger-side gate itself: implemented as `internal/tools/verify_gate.go` + `cmd/agent/merger_verify_child.go` (commits `97fc1c7c`, `64566a6c`), called from the merger tick at `cmd/agent/cli_tasks.go:1934` (predicate) and `:2023` (gate). **Owns: the gate's flow, revert, rework, caps.** This spec must NOT design or implement it. |

Devdocs already written (do not restate them):`devdoc/task-verify-loop.md` (the state machine + `gopi task verify` CLI),
`devdoc/bug-verify-closure-20260912.md` (why the loop never fired, the closure
gate, and §4 honest limits), `devdoc/merger-verify-gate-20260912.md` (the gate's
flow, verdict-from-ledger ruling, `MergeVerifyFailCap = 3`,
`mergeVerifyChildWaitTimeout = 15m`, honest limits),
`devdoc/merger-self-resolve-20260905.md` and
`devdoc/merger-single-owner-architecture-20260905.md` (the resolver child — the
repairer whose edits produce the highest-risk merge),
`devdoc/merge-resolve-merger-verify-gate.md` (a real resolve report),
`devdoc/worktree-recycle-verification-20260912.md` (a real independent
verification report, row `01a09250` — the one row in the ledger that reached
`verified` through an actual independent run).

**Uncovered by all of the above — the gap Deliverable B closes:** none of them
says where the verifier's *responsibility* ends. `01a09216-6561` defines when a
verifier is *queued*; `01a09237-f56a` defines what the merger does with the
verdict; neither defines what the verifier itself may and may not do, how much
it may spend, or what shape its verdict record takes. That is
`devdoc/verify-responsibility-spec-20260912.md`.
