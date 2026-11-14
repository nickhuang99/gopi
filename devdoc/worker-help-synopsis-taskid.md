<!-- doc-class: historical -->
# Task Report: unify the `gopi worker` task-id spelling across the two help faces

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## Completed

- **Verdict with citations**: the task-id operand of `gopi worker` is
  **OPTIONAL** — bare `gopi worker` is the queue-draining LOOP, a named operand
  runs exactly that one row. Proof chain (read, not guessed):
  - `cmd/agent/cli_worker_daemon.go:265` — the ONE argv scanner's positional arm
    `case !strings.HasPrefix(a, "-"):`; `:275` `wa.explicit = a`. A bare
    `gopi worker` never enters the arm, so `wa.explicit == ""`.
  - `cmd/agent/cli_tasks_worker.go:254` `runTaskWorker(once bool, explicit string, opts *tasksOptions)`;
    `:264` `if explicit != "" { runTaskWorkerExplicit(explicit, opts); return }`;
    `:268` `runTaskWorkerLoop(once)` — the empty case IS the drain loop.
  - `cmd/agent/cli_tasks_worker.go:287` `runTaskWorkerExplicit(taskID string, …)`
    — ONE round on a NAMED row.
  - Corroboration in the corpus: `internal/taskverbs/verbs.go:187` and
    `internal/taskverbs/verbs_test.go:52,232` already write the OPTIONAL form
    `` `gopi worker [<task-id>]` `` — so the chosen spelling is not invented; it
    is the form the family package already uses for exactly this semantics.
- **Unified spelling applied to both sites**: `[<task-id>]` (optional brackets,
  lowercase go-style placeholder, matching every other `<placeholder>` in
  `internal/config/help.go` — `<symbol>`, `<session-id>`, `<url>`, `<image>`).
  Diff is TWO lines, text only (see `## Diff`).
- **Sweep** of `internal/config/help.go`'s 35 `{"gopi …"}` synopsis entries
  against each command's own `-h` block: **9 findings reported** (report only,
  per the epic), in `## Other top-level vs subcommand mismatches` below.
- **No behavior change**: the diff touches only two string literals (a help
  index row and a usage constant). No parse path, no flag, no constant value.
  The unchanged parse path is cited above; the binary's own refusals for the
  error shapes still fire identically (evidence below).
- Doc: this report (`devdoc/worker-help-synopsis-taskid.md` is the devdoc
  companion; this file is the Rule 5 report).

## Not Done (honest)

- **Not fixed**: the 9 other mismatches. The epic scopes this task to REPORT
  them; a fix needs a per-command semantics read (is that operand actually
  optional?) and would be a code change beyond "text only".
- **Not fixed**: `cmd/agent/cli_tasks.go:431` — the family page's pointer still
  writes `"gopi worker TASK_ID"` (no brackets at all, third spelling of the same
  word inside one command's help). It is a prose mention inside `tasksUsage`, not
  a synopsis line, and changing it was outside the epic's two named sites; it is
  reported here rather than silently edited.
- `internal/config/help.go:469` (`gopi resume <session>`, prose `--task TASK_ID`)
  keeps its `TASK_ID` prose spelling. It names the same operand as the
  `resumeUsage` flag spelling and `resumeUsage` itself uses `<task-id>`; the
  index's prose is not a synopsis line. Reported, not changed.
- No new gate test was added: the two sites are text in two packages, and the
  existing help gates (`TestTopLevelUsageIsAlignedAndTerse`,
  `TestEveryHelpConstantFitsTheWrapBudget`,
  `TestNoHelpTextIsDuplicatedAcrossPages`) already fail on the shapes that
  matter (width, duplication). A cross-package "same spelling" gate would have
  to hardcode the placeholder — Rule 1b's bare-string problem in test form.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0); `go vet ./...` → OK (exit 0).
- Tests: `go test ./internal/config/` → `ok github.com/nick/gopi/internal/config 1.134s`.
  `go test ./cmd/agent/ -run 'Worker|Usage|Help|CapabilityFirst|NoHelpTextIsDuplicated' -count=1`
  → see `## Evidence`.
- Render truth (real binary, not tests): `gopi -h` and `gopi worker -h` rendered
  before/after and pasted below; the worker synopsis spelling is now
  **byte-identical** at both sites (`[<task-id>]`).
- Commit: see `## Commit`.

## Diff

```diff
diff --git a/cmd/agent/main.go b/cmd/agent/main.go
@@ -259,7 +259,7 @@ const workerUsageHead = `GoPi worker — run the queued tasks, then exit. No dae
   gopi worker                 run tasks until the queue drains, exit 0 by itself
   gopi worker --once          drain the queue once, then exit 0
-  gopi worker <task-id>       run THAT row and nothing else, then exit
+  gopi worker [<task-id>]     run THAT row and nothing else, then exit
                               (exit 1 if it failed)

diff --git a/internal/config/help.go b/internal/config/help.go
@@ -457,7 +457,7 @@ var commandIndex = []commandSection{
-		{"gopi worker [TASK_ID] [--once] [--daemon]", "The only command that runs a task row. Plain `gopi worker` drains the queue, then self-exits; --once drains once; a bare TASK_ID runs exactly that row in the foreground (exit 0 completed / 1 failed). …"},
+		{"gopi worker [<task-id>] [--once] [--daemon]", "The only command that runs a task row. Plain `gopi worker` drains the queue, then self-exits; --once drains once; a bare <task-id> runs exactly that row in the foreground (exit 0 completed / 1 failed). …"},
```

(The second hunk's prose tail is unchanged; only `TASK_ID` → `<task-id>` in the
synopsis and in the sentence that renames the operand.)

## Before / after, from the real binary

BEFORE — `gopi -h` (worker row) and `gopi worker -h` (synopsis rows):

```
  gopi worker [TASK_ID] [--once] [--daemon]  The only command that runs a task row.
                                             Plain `gopi worker` drains the queue, then
                                             self-exits; --once drains once; a bare
                                             TASK_ID runs exactly that row in the
                                             foreground (exit 0 completed / 1 failed).
```
```
  gopi worker --once          drain the queue once, then exit 0
  gopi worker <task-id>       run THAT row and nothing else, then exit
                              (exit 1 if it failed)
```
→ **two different spellings of one operand, and both wrong**: `[TASK_ID]` reads
as required-with-brackets (a contradiction), `<task-id>` reads as required.

AFTER — `gopi -h`:

```
  gopi worker [<task-id>] [--once] [--daemon]  The only command that runs a task row.
                                               Plain `gopi worker` drains the queue,
                                               then self-exits; --once drains once; a
                                               bare <task-id> runs exactly that row in
                                               the foreground (exit 0 completed / 1
                                               failed). --daemon detaches the loop (the
                                               command returns immediately with the pid;
                                               the worker keeps the drain-and-self-die
                                               contract) — see `gopi worker -h`.
```

AFTER — `gopi worker -h`:

```
GoPi worker — run the queued tasks, then exit. No daemon to babysit.

  gopi worker                 run tasks until the queue drains, exit 0 by itself
  gopi worker --once          drain the queue once, then exit 0
  gopi worker [<task-id>]     run THAT row and nothing else, then exit
                              (exit 1 if it failed)
  gopi worker --daemon        same loop, detached (returns at once; logs to
                              <gopi-dir>/logs/worker.log)
  ...
```

Both faces now render `[<task-id>]`; `gopi worker -h` changes in exactly ONE
line (verified by diffing the two rendered pages: `5c5`, the synopsis line —
everything else on that page is byte-identical), and the top-level change is the
one row plus the column-width reflow of the rows sharing that section (the
renderer aligns the section on its widest row, so `task` and `merger` re-wrap by
two columns without any text change).

## Choice justification (against the argv parsing read in code)

- Optional brackets are the ONLY honest spelling: `wa.explicit` is `""` for a
  bare `gopi worker` and the code takes the loop branch
  (`cli_tasks_worker.go:264-268`). Printing it without brackets (`<task-id>`)
  asserts a required operand the parser does not require; the top-level
  `[TASK_ID]` was the opposite error — brackets around a token that is *never*
  required *and* is not the word the page names in prose.
- Lowercase `task-id` (not `TASK_ID`): every other placeholder in the same file
  is lowercase-angle (`<symbol>`, `<session-id>`, `<url>`, `<query>`, `<image>`,
  `<input.txt>`); `TASK_ID` was the lone all-caps outlier, and `TASK_ID` is also
  a *flag-value* spelling elsewhere (`--task TASK_ID`), so reusing it for a
  POSITIONAL invites the reader to look for a flag.
- Single source: no derivable source existed for this operand. `workerUsageHead`
  is a hand-written raw-string constant (`cmd/agent/main.go:246-258`) and
  `internal/taskverbs` enumerates VERBS, not operand placeholders (memory #2412's
  derivation applies to verb enumerations — `taskverbs.FamilyVerbs`, used one row
  above at `help.go:459` — not to this placeholder). Where a source DOES exist the
  row already derives from it, so the fix is confined to the two literals.

## Other top-level vs subcommand mismatches (REPORT ONLY — 9 findings + 1 checked-clean)

Each row: the top-level index synopsis (`internal/config/help.go`) vs the
command's own `-h` block (`cmd/agent/*.go`). Findings: **1, 2, 3, 5, 6** are
spelling/arity contradictions (the same class as the worker one); **7, 8, 9, 10**
are index-terseness / pipe-enum style. Row 4 is logged to show the sweep actually
compared it and found agreement.

| # | index synopsis | the command's own `-h` | what differs |
|---|---|---|---|
| 1 | `gopi vision_analyze <image>` (help.go:486) | `gopi vision_analyze <image_path>` (`cli_vision.go:22`) | placeholder NAME: `<image>` vs `<image_path>`. The subcommand's own text uses `<image_path>` in two places (`cli_vision.go:15,22`). |
| 2 | `gopi text2img <input.txt>` (help.go:487) | `gopi text2img <input.txt> [output.png] [flags]` (`cli_text2img.go:26`) | the index omits the OPTIONAL second positional `[output.png]` and `[flags]`. |
| 3 | `gopi voice_input <audio>` (help.go:490) | `gopi voice_input <audio-file> [--chunk N]` (`cli_voice.go:108`) | placeholder NAME `<audio>` vs `<audio-file>`. |
| 4 | `gopi log <session-id>` (help.go:465) | `gopi log <session-id> …` (`cli_log.go:39`) | consistent — listed here only to show the sweep checked it (no finding). |
| 5 | `gopi resume <session>` (help.go:469) | `Usage: gopi resume <task-id\|raw-log-path\|session-id> [continuation-file]` (`cli_resume.go:17`) | the index's `<session>` names only the THIRD of three accepted targets, and omits `[continuation-file]`. The page's own spelling is a three-way union. |
| 6 | `gopi act <action>` (help.go:489) | no synopsis line with `<action>`; the page lists concrete sub-actions `gopi act move X Y` / `gopi act click [--button N]` / `gopi act type [--text STR]` / `gopi act key [--key NAME]` / `gopi act info` (`cli_act.go:65-69`) | the index invents a generic `<action>` operand that appears NOWHERE on the page (the real words are the fixed sub-action verbs). |
| 7 | `gopi completion bash\|zsh\|fish` (help.go:499) | `gopi completion bash` / `gopi completion zsh` / `gopi completion fish` as separate synopsis rows (`cli_completion.go:52,61,66`) | index uses a pipe-enum; the page enumerates one row each. Cosmetic, but the same command has two shapes. (Note `help_width_test.go` already forbids pipe-enums *without spaces*; this one is spaced.) |
| 8 | `gopi checkpoint list\|export` (help.go:468) | `gopi checkpoint list [--dir DIR]` and `gopi checkpoint export <log> [--dir DIR]` as separate rows, plus a third form `gopi resume <log> [continuation…]` (`cli_checkpoint.go:20,27,31`) | same pipe-enum style difference, and the index omits the `resume` form the page teaches. |
| 9 | `gopi web search "<query>"` / `gopi web fetch <url>` (help.go:474-475) | `gopi web search "<query>" [--method M] [--count N]` / `gopi web fetch <url> [--max-chars N]` (`cli_websearch.go:40`, `cli_webfetch.go:32`; also the group page `cli_web.go:139-140`) | the index drops the flag surfaces; the group page and each tool page agree with each other, so this is index-terseness, not a contradiction. |
| 10 | `gopi shot --url URL \| --self` (help.go:491) | `gopi shot --url URL [--out PATH] [flags]` / `gopi shot --self [--addr HOST:PORT] [--out PATH] [flags]` (`cli_shot.go:34,36`) | the index compresses two mutually exclusive forms into one pipe-enum and drops `[flags]`. |

Findings **1, 2, 3, 5, 6** are spelling/arity contradictions of the same class as
the worker one (the index names an operand the command's own page names
differently, or omits an accepted one). Findings **4, 7, 8, 9, 10** are
index-terseness or pipe-enum style (the epic's own "index, not the manual"
design), listed so a reader can see the sweep was exhaustive rather than
selective.

Not a mismatch but worth flagging: `gopi task` is the only family whose index row
is DERIVED (`taskverbs.FamilyVerbs`, `help.go:459`) — that derivation is exactly
the "single source" pattern the other rows lack.

### Remaining `<task-id>` occurrences in RUNTIME text (reported, not changed)

The unified spelling was applied to the two HELP sites. Three more sites carry
the required-reading spelling in text the binary PRINTS, all outside the epic's
scope and all verified unchanged by the diff:

- `cmd/agent/cli_worker_daemon.go:363-364,368-369,375` — the refusal sentences
  ("pick one: `gopi worker --once` (drain) or `gopi worker <task-id>` (that
  row)") and the accepted-argv hint `accepted: -h/--help | --once | <task-id>
  …`. These are parser ERROR text; changing them is a code-text change beyond the
  epic's two named help sites, so they are reported here.
- `cmd/agent/cli_tasks.go:431` — the family page's prose pointer.
- `cmd/agent/cli_tasks.go:4483` — the retired-runner advice sentence.

They are quoted live in `## Evidence` (the probe run) so a follow-up can pick
them up without re-deriving the wording.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0, no output. `go vet ./...` → exit 0, no output. (Run in the worktree, t+384s.)
- `CGO_ENABLED=0 go build -o /tmp/gopi-wh-before ./cmd/agent/` (pre-edit, at branch base `45c6ecc1`) and `-o /tmp/gopi-after` (post-edit) → both exit 0.
- `/tmp/gopi-wh-before -h | grep worker` → `gopi worker [TASK_ID] [--once] [--daemon]`; `/tmp/gopi-wh-before worker -h | grep worker` → `gopi worker <task-id>       run THAT row and nothing else`.
- `/tmp/gopi-after -h | sed -n '73p'` → `  gopi worker [<task-id>] [--once] [--daemon]  The only command that runs a task row.`; `/tmp/gopi-after worker -h | sed -n '5p'` → `  gopi worker [<task-id>]     run THAT row and nothing else, then exit`.
- `diff /tmp/wh-worker-before.txt /tmp/wh-worker-after.txt` → `5c5` only: `-  gopi worker <task-id>       run THAT row and nothing else, then exit` / `+  gopi worker [<task-id>]     run THAT row and nothing else, then exit`. Exit 1 (one line differs) — the subcommand page is otherwise byte-identical, so the NOTES prose (`Naming a row as the argument …`) and the Options line (already written `<task-id>` without a required-claim) needed no change.
- `diff /tmp/wh-usage-before.txt /tmp/wh-usage-after.txt` → the `Task ledger & execution` section (lines 66-96 → 66-97): the worker row synopsis and prose, plus the reflow of the `task` and `merger` rows that share the section's widest-column alignment (no text change in those two rows).
- Width budget (render truth, `config.HelpWrapCol = 88`): `awk` over the rendered `/tmp/wh-usage-after.txt` → max line length **88**; over `/tmp/wh-worker-after.txt` → **81**. Nothing over budget.
- `go test ./internal/config/` → `ok  github.com/nick/gopi/internal/config  1.134s`.
- `go test ./cmd/agent/ -run 'Worker|Usage|Help|CapabilityFirst|NoHelpTextIsDuplicated' -count=1` → see the close-out run's output (this is the branch-scoped smoke for the touched surface).
- Backtick pitfall (memory #1353) preflight: the new text contains no backtick — `[<task-id>]` inside the raw-string `workerUsageHead` and inside a plain quoted Go string in `help.go`. No raw string is terminated by this edit.
- Behavior probe on the POST-change binary (the parse path is untouched, so the error shapes must be identical): `gopi worker --id t-x` → exit refusal `gopi worker: unexpected argument "--id" — refusing to start the worker loop`, hint line `accepted: -h/--help | --once | <task-id> [--takeover] [--as <holder>] [--db <path>] [--report-to <alias>] [--repo <target>] | --daemon`; `gopi worker t-a t-b` → `unexpected argument "t-b"`; `gopi worker --once t-a` → `--once and the row t-a ask for different work … pick one: `gopi worker --once` (drain) or `gopi worker <task-id>` (that row)`. All three still refuse/report exactly as documented — no help-only edit reached a decision site.
- `git status --porcelain` in the worktree → empty (deliverable committed; no untracked residue).

## Commit

`b1c0b7df fix(help): unify the gopi worker task-id synopsis on the optional form`
— branch `fix/worker-help-synopsis` in the shared repo (worktree
`/home/user/workspace/gopi-worker-help`). A follow-up commit carries this report's
`## Evidence` additions.

## Not Done (deferred, dispatchable)

- The 9 reported mismatches: each needs the same argv read this task did for
  `worker` (is the operand optional? does the index's word exist in the parser?).
  Findings 1/2/3/5/6 are the ones worth a follow-up task.

## Close-out re-verification (run after the report was written)

- `CGO_ENABLED=0 go build ./...` → exit 0, no output; `go vet ./...` → exit 0, no
  output; `git status --porcelain` → empty (re-run in the worktree at t+1201s).
- Build+test re-confirmed: `go test ./internal/config/` → `ok 1.134s` (again `ok
  0.784s` at close-out), `go test ./internal/taskverbs/` → `ok 0.002s`.
- The branch-scoped smoke, re-run with `-v`: `go test ./cmd/agent/ -run
  'Worker|Usage|Help|CapabilityFirst|NoHelpTextIsDuplicated' -count=1 -v` →
  **60/60 PASS, `ok github.com/nick/gopi/cmd/agent 249.5s`** — including
  `TestWorkerUsageHeadCoversLoopContract`, `TestWorkerHelpDoesNotRun`,
  `TestWorkerUnknownArgumentIsRefused` (5 subtests), `TestWorkerAcceptedArgumentsParse`
  (7 subtests), `TestNoHelpTextIsDuplicatedAcrossPages` + `RedProof`,
  `TestCLINamingExceptionsDocumentedInTopLevelHelp`.
- **A first run of that filter FAILED, and the failure was NOT this diff.** It
  ended on `TestWorkerReleaseOwnWorktree_Acceptance` (same package, no file this
  diff touches) with output about the HOST's worktrees:
  `own-worktree-release: … kept /home/user/workspace/gopi-heavy-boundary —
  worktree is DIRTY (uncommitted work) — left for a human`, likewise
  `gopi-shm-role` (dirty) and `gopi-shm-gate` / `gopi-shm-gate-control` /
  `gopi-worker-help` (held by a live process). That test scans the machine's
  registered worktrees, and five sibling worktrees were busy at that moment.
  Isolation ruled it out as a regression: the same test alone passes in this
  worktree (`ok 8.123s`) **and** at the pre-edit base `45c6ecc1` (exported tree,
  `ok 15.027s`) — the same result on both sides of the diff.
- Renders re-captured live at close-out from a binary built at each side
  (`/tmp/nick/gopi-wh-before` = base export, `/tmp/nick/gopi-new` = this branch):
  `gopi -h` worker row `gopi worker [<task-id>] [--once] [--daemon]` (line 73) and
  `gopi worker -h` line 5 `gopi worker [<task-id>]     run THAT row and nothing
  else, then exit` — the SAME spelling on both faces.
- Behavior probe on the post-change binary, re-run at close-out: `gopi worker
  --bogus` → `gopi worker: unexpected argument "--bogus" — refusing to start the
  worker loop` with the unchanged `accepted: …` hint, byte-identical before/after;
  `gopi worker --status` → `gopi worker: no workers running` + the same `why:` /
  `lock:` lines before and after. The parse path and every decision site are
  untouched — the diff is two string literals.
- Note on the sibling-worktree scan: this worktree is one of the five the scan
  reported, so while ANY `cmd/agent` test is running here, the machine's other
  worktrees can make `TestWorkerReleaseOwnWorktree_Acceptance` red for an
  unrelated reason. That is a property of the host's concurrent work, not of this
  branch — recorded here so a later reader does not chase it as a regression.
