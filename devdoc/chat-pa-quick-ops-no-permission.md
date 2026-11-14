<!-- doc-class: normative -->
# The chat PA's no-permission band: ≤30 s memory work and light read-only investigation

> **溯源。** `TestDocAnchorsResolveInNormativeDocs` 已于 2026-10-22 随 `internal/docclass` 在 `5a35f61` 中删除：本页里的 `file.go:NNN` 引用是落地时的记录，没有任何在跑的门禁解析它。行号对人依然有用，但它不再被测试守护。下文引用的原句都按当时的写法保留 —— 若某句读起来像现在时（"FAIL"、"resolve"、"门禁"），那是那一次运行的状态，不是本树上还活着的门禁。

> **历史文档说明（2026-10-17，task `01a14731`）**：本文件记录的是 operator **2026-10-09** 的裁定，
> 它当时落在**耗时档位**判据上（`ChatOpQuickBudgetSec = 30` / `ChatOpAskThresholdSec = 60`）。
> 那套档位已在 2026-10-11 的裁定后被**动作类别判据**取代（`tools.ChatOpClassify` /
> `tools.ChatOpAdmit`，三类：OPERATOR WORK 直接跑 / REPOSITORY WRITE 拒并路由 task_submit /
> LONG OPERATION 先问），这两个常量**已不存在**。本文里出现的 `ChatOpQuickBudgetSec` /
> `ChatOpAskThresholdSec` / `timeout_sec` 一律按历史读数理解；判据现状见
> `devdoc/chat-ops-authorization.md` §8。本文的**结论**（"记忆操作与轻量只读调查不必请示"）
> 不但没变，而且是新判据里那句 `ChatOpOperatorWorkSentence()` 最直白的成员。

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Operator ruling, 2026-10-09**: 耗时 ≤30 s 的记忆操作与轻量只读调查，PA 随时可做、不必请示 —
the console (the operator's PA) does a memory operation or a light read-only investigation at any
moment, without asking first.

Task: `01a11d88-a748-7504-b4f5-355bb787b4ce`, branch
`task/01a11d88a7487504b4f5355bb787b4ce`.

---

## 1. What the defect was

`internal/agent/chat_pa_duty.go` (the `[CHAT PA DUTY …]` paragraph) already told the console two
things: which maintenance work it does ITSELF, and that only a REPOSITORY CHANGE is a task. It did
**not** say the cheapest operations of all — a `memory` search/store, a `read`, a
`code_search`/`keyword_search`, a quick read-only shell query — need no permission. The measured
consequence is a PA that stops to ask a human about a three-second lookup.

The criterion those operations fall inside **already existed**:
`internal/tools/chat_ops_authorization.go` decides it (`ChatOpQuickBudgetSec = 30`,
`ChatOpAskThresholdSec = 60`, the `timeout_sec` declaration, the standing-posture channel
`GOPI_CHAT_OP_AUTO` / settings key `chat_op_auto`), and `chatWriteBoundaryRules`'s time rule (6)
already RENDERS the two bands.

> **The criterion itself has since been REPLACED (2026-10-11 ruling, landed 2026-10-17, task
> `01a14731`), and that is the model this document's conclusion lives under.** An operation is judged
> by its CATEGORY (`tools.ChatOpClassify` / `tools.ChatOpAdmit`: OPERATOR WORK runs / REPOSITORY
> WRITE is refused and routed to `task submit` / LONG OPERATION asks first), never by a stopwatch —
> `timeout_sec` is the bash tool's execution cap and is no longer read as a declaration. The
> operations this document is about are OPERATOR WORK, so the 2026-10-09 ruling survives as its own
> class rather than as a band: it needs no permission at every posture. The two band constants no
> longer exist; every `ChatOpQuickBudgetSec` / `ChatOpAskThresholdSec` below is a HISTORICAL reading.
> Current criterion: `devdoc/chat-ops-authorization.md` §8.

## 2. What was changed

**One clause, rendered — no new rule, no new criterion.**

| file | change |
|---|---|
| `internal/agent/chat_pa_duty.go` | new clause **(1b)** inside `chatPADutyRules`, rendered by the new `chatPADutyNoPermissionRule(toolsOn bool)`; marker `chatPADutyNoPermissionMarker`; the two honest limbs `chatPADutyQuickOpsTools` / `chatPADutyQuickOpsNoTools`; header comment extended with the clause's provenance |
| `internal/agent/chat_pa_duty_test.go` | three tests: `TestChatPADutyQuickOpsNeedNoPermission` (the ruling is present, and every number/arg/channel is the GUARD's own constant), `TestChatPADutyQuickOpsKeepsTheOtherTwoClasses` (TASK and OPERATOR-ONLY are unmoved, and **no clause is rendered twice**), plus the count assertion recorded in §4.4 |

### 2.1.1 The defect this run found in its own first commit, and fixed

The first commit (`ab27cd248`) spliced (1b) in by **re-emitting the builder's whole (1) block with a
copy of (2) pasted onto it** — so the rendered paragraph carried `(2) ONLY a change to the
REPOSITORY is a task …` **twice** (measured: `grep -c` over the rendered string → `2`; the pre-fix
prompt is quoted verbatim in §4.4). Nothing in the suite caught it: every assertion was a
`strings.Contains`, which is satisfied by a sentence that appears once OR twice.

A duplicated clause is not cosmetic — the model reads the same rule twice, and the paragraph's own
`(1) (2) (3) (4) (5)` numbering stops meaning "in this order". The fix is one line (drop the pasted
copy: the `chatPADutyNoPermissionRule(toolsOn) + "<(2) …>"` splice becomes
`chatPADutyNoPermissionRule(toolsOn))`), and the **guard is now the count**:
`TestChatPADutyQuickOpsKeepsTheOtherTwoClasses` asserts each clause marker occurs **exactly once**,
which is the assertion shape that distinguishes the two forms (a presence check cannot).

### 2.1 Clause (1b) as composed

Placed after clause (1) and before clause (2) — the order a reader needs: *what is yours* → *what
needs no permission* → *what is a task* → *what only the operator may authorise*. It says:

- **the cheap, harmless work is NOT a permission question**; two families are named because these
  are the operations the console keeps stopping on — (a) a `memory(action=search)` lookup and
  `memory(action=store)` when the operator says 记住 or a lesson lands (WHEN is
  `chat_memory_timing.go`'s rule, cited rather than restated); (b) a light read-only investigation —
  `read`, `code_search`, `keyword_search`, read-only `bash` queries, on the smallest sample;
- **this is the SAME criterion** the write-boundary paragraph's rule states, applied to the operations
  that are always inside it — **and the criterion it names is the CATEGORY's, not a band's**: those
  operations are `ChatOpClassOperatorWork`, which runs with no permission question at every posture
  (the historical reading of this sentence was "≤ `ChatOpQuickBudgetSec`s needs no permission, ≤
  `ChatOpAskThresholdSec`s only when the operator's own turn declared that budget"; both constants
  are gone, and what survives is the class), with the standing posture `GOPI_CHAT_OP_AUTO` /
  settings.json `chat_op_auto` = `auto` | `all-ask` remaining its own channel for the LONG class only;
- **ASKING IS STILL RIGHT** for what the CATEGORY — never a clock — makes a question: an unprovoked
  **LONG** operation (the only class the standing posture decides), and it is **no longer WRONG for a
  REPOSITORY CHANGE**: since the operator's **2026-10-24 ruling** the clause renders
  `tools.ChatOpRepoWriteRefusalSentence()` and states the class as the console's to perform, matching
  `tools.ChatOpAdmit` (which admits a repository write at every posture). What the clause retires is
  the **REF MOVE**, not the commit (see `devdoc/chat-pa-duty.md` §2b).

### 2.2 Nothing is typed (Rules 1/13/14)

Every value in the clause is read from its one definition (the class vocabulary and the sentences are
`internal/tools`' own exported renderings, composed there, never spelled here):

| value in the prompt | its single definition |
|---|---|
| the three classes, and the long vocabulary | `tools.ChatOpOperatorWorkSentence()` / `ChatOpRepoWriteRefusalSentence()` / `ChatOpLongRefusalSentence(channel)` — the SAME strings a refusal prints |
| `GOPI_CHAT_OP_AUTO` / `chat_op_auto` | `tools.ChatOpAutoEnv` / `tools.ChatOpAutoSettingsKey` (aliased from `internal/config`) |
| `auto` / `all-ask` | `tools.ChatOpChannelAuto` / `tools.ChatOpChannelAllAsk` |
| tool names | `toolnames.ToolMemory` / `ToolRead` / `ToolCodeSearch` / `ToolKeywordSearch` |
| `memory(action=store)` | `chatMemoryStoreAction()` — the SAME composer `chat_memory_timing.go` renders |

> **Historical (the retired model's reading of this table):** `30` / `60` came from
> `tools.ChatOpQuickBudgetSec` / `tools.ChatOpAskThresholdSec` and `timeout_sec` from
> `tools.ChatBashTimeoutArg`. Neither band constant exists any more; `timeout_sec` survives only as a
> noun the prompt DENIES ("it is the bash tool's execution cap, not a declaration of class").

`gofmt -l internal/agent/chat_pa_duty.go` prints nothing; the file carries no bare band number.

## 3. The two-criterion failure mode this deliberately avoids

The guard (`internal/tools`) and the prompt (`internal/agent`) are a **pair**, and a pair rots in
one way: one side changes, the other keeps rendering an older truth, and each side's own tests stay
green (the guard's tests call the guard, the prompt's read the prompt). So
`TestChatPADutyQuickOpsNeedNoPermission` reads the clause AND drives the guard's real entry point
(`tools.ChatOpRefusal`, which is `ChatOpAdmit` behind one rendering) on the calls the clause names —
a CLASS change that forgets the prompt fails there instead of teaching the model a class the guard
does not apply, which is exactly what the category rework had to fix after the fact (the clause was
still describing the retired band). That is the same shape `chat_ops_budget_prompt_test.go` applies
to rule (6), one clause over.

## 4. Verification

- **Build**: `go build ./...` → exit 0 (`BUILD_OK`).
- **Tests**: `make test-budget PKGS='./internal/agent ./internal/tools'` — see §4.1.
- **Ran** (Rule 31 — the surface really executed, twice, with the real verb):
  - `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BINARY_OK`, 64,852,575 bytes.
  - `./bin/gopi chat --once --tools -p "先查一下我们记忆库里关于 chat 提示词里常量的既有经验（一条就行），然后读一下 internal/agent/chat_pa_duty.go 里第 1b 条款所用的标记常量名，把名字原样报给我。不要问我任何问题，直接做。"`
  - `./bin/gopi chat --once --tools -p "查一下记忆库里关于 worktree 清理的经验有没有，一条就行；再用 keyword_search 找一下仓库里 chatPADutyNoPermissionMarker 出现在哪些文件。两个结果都原样报给我，中途不要停下来问我。"`
  - `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (again, after the final text edit) →
    `REBUILD_OK`, then
    `./bin/gopi chat --once --tools -p "帮我确认两件事，都原样报我，中途不要停下来问我：(1) 用 memory 搜一下我们库里关于「分支强制覆盖 / git push --force」的教训；(2) 用 code_search 找 ChatOpQuickBudgetSec 的定义在哪。做完直接给结论。"`
  - **The §2.1.1 fix, re-verified end to end**: rebuilt the binary after removing the duplicated
    clause (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `64852543` bytes), then ran
    `./bin/gopi chat --once --tools -p "帮我做两件小事，都原样报我，中途不要停下来问我：(1) 用 memory 搜一下我们库里关于「doc-anchor / TestDocAnchorsResolveInNormativeDocs」的既有经验；(2) 用 code_search 找出 ChatOpQuickBudgetSec 定义在哪个文件。做完直接给结论。"`
    → session `f28b7148-5212-4b34-9de6-67a2f7dc1fcb`.
- **Commit**: `ab27cd248 feat(chat): clause (1b) — the ≤30 s memory work and light read-only investigation need no permission`
  (the code + first-cut doc); this section's test paste landed in the follow-up commit on the same
  branch, and the §2.1.1 duplicate-clause fix with its count assertion landed in the last one
  (`fix(chat): render clause (2) once — the (1b) splice had pasted a copy of it`).
- **Result**: two real chat sessions, each running the permission-free work end to end and neither
  stopping to ask — §4.2.

### 4.1 Tests

```
$ go test ./internal/agent/ -run 'TestChatPADuty' -count=1 -v
--- PASS: TestChatPADutyQuickOpsNeedNoPermission (0.02s)
--- PASS: TestChatPADutyQuickOpsKeepsTheOtherTwoClasses (0.00s)
--- PASS: TestChatPADutyBothChatPathsCarryTheirOwnLimb (0.00s)
--- PASS: TestChatPADutySitsAfterTheWriteBoundary (0.02s)
--- PASS: TestChatPADutyNamesItsVocabularyFromTheLeafConstants (0.00s)
--- PASS: TestChatPADutyRendersNoDestructiveBranchForm (0.02s)
--- PASS: TestChatPADutyAgreesWithTheWriteBoundaryOnWhatIsATask (0.00s)
ok  	github.com/nick/gopi/internal/agent	0.382s
```

Neighbours kept green (density budget, the pre-existing same-origin pair):

```
$ go test ./internal/agent/ -run 'TestSystemPromptDensity|TestPromptSectionsSorted'
--- PASS: TestSystemPromptDensity (0.00s)
--- PASS: TestPromptSectionsSorted (0.00s)

$ go test ./internal/agent/ -run 'TestChatPADuty|TestChatOpsBudgetPrompt|TestChatPromptsCarryTheWriteBoundary'
ok  	github.com/nick/gopi/internal/agent	2.485s

$ go test ./internal/tools/ -count=1
ok  	github.com/nick/gopi/internal/tools	260.935s
```

#### 4.1.1 One package test is red, and it was red on master first (measured, not assumed)

`make test-budget PKGS='./internal/agent'` reports `outcome=fail` on
`TestDocAnchorsResolveInNormativeDocs`. That is **not this change**: the same
invocation was run in the main checkout, which sits on `master` (`823305a17`) and
carries **none** of this branch's commits, and it fails identically.

```
$ cd /home/user/workspace/gopi-01a11d88a7487504b4f5355bb787b4ce   # this branch
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.28s)
FAIL	github.com/nick/gopi/internal/agent	1.711s
$ grep -c 'BASELINE DRIFT' …   → 256

$ cd /home/user/work/GoPi && git log --oneline -1   # master, no branch commits
823305a17 feat(voice): streaming dictation — drafts live in the line, Ctrl+G settles the sentence
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.31s)
FAIL	github.com/nick/gopi/internal/agent	1.718s
$ grep -c 'BASELINE DRIFT' …   → 256
```

**Re-measured at the end of the run** (master's tip had advanced by then, so this is the pair that
exists now rather than the pair quoted above):

```
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1   # branch (tip e727e61a3)
FAIL … doc anchors verified: 3084 across 687 normative docs … 244 pre-existing drift entries
     permitted, 35 new, 12 stale permits
$ cd /home/user/work/GoPi && git log --oneline -1
22f3b74b1 fix(voice): drop redundant single-value append in voice defect test (go vet)
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1   # master, no branch commits
FAIL … doc anchors verified: 3082 across 686 normative docs … 244 pre-existing drift entries
     permitted, 36 new, 12 stale permits
```

Both sides: exit 1, 244 permits, 12 stale, and the drift the gate reports is the same backlog — the
branch carries **one more normative doc** (687 vs 686, the devdoc this task wrote) and one **fewer**
`new` entry than master (35 vs 36, a master-side entry that this branch's doc happens to re-anchor).
`grep -c 'chat-pa-quick-ops'` over the failure output → **0** both times.

Same test, same verdict, same drift count (256) with the branch absent — the red is
master's, and it is the doc-anchor backlog (`devdoc/hippocampus.md`,
`devdoc/agent-five-organs-ontology.md`, … anchors pointing at blank lines after
`loop.go` was refactored), whose fix the gate's own message names as "re-key by
symbol". **This change does not silence it and does not widen it**: the new devdoc
appears **zero** times in the drift list (the anchor gate scans `normative` docs
and this one is registered as such — `grep -c 'chat-pa-quick-ops'` over the failure
output → `0`), so no anchor this run wrote is among the 256. Recording it here
rather than reporting `internal/agent` green is the point: a gate result the run
did not observe is not a reportable result, and a gate that is red on both sides of
a diff is the baseline, not the diff's product.

**One consequence the task asked about and this run measured**: the branch's own new devdoc is
COUNTED by the gate (687 normative docs vs master's 686) and its anchor resolves — the row
`devdoc/DOC-INDEX.tsv` carries for it points at the new marker's own definition site
(`internal/agent/chat_pa_duty.go:177`), and `grep -c 'chat-pa-quick-ops'` over the gate's failure
output is `0` on both sides. So the new document ADDS a normative doc without adding a drift entry.

The scope-limited consequence for this branch: **every test that this diff can
affect is green** (`TestChatPADuty*` 7/7, the density/section neighbours, and the
`TestDocAnchorsResolveInNormativeDocs` run is red exactly as it is on master).

Honest note: a first `go test ./internal/tools/` run **failed** while a concurrent second
`go test` of the same package was running from the same scratch root (`FAIL … 309.184s`, the
failures all inside `suite-tools-run-*` fixtures in `/dev/shm`). The package re-ran clean on its
own (`ok … 260.935s`), and this change touches nothing under `internal/tools/` — the two
`go test` sets overlapped, which is the failure mode the bash-timeout rule names.

### 4.2 Real-session evidence (Rule 11)

Evidence directory: `/home/user/.gopi/sessions/01a11d88-a748-7504-b4f5-355bb787b4ce/artifacts/`

| session id | what the turn asked for | tool_use blocks observed (excerpt) |
|---|---|---|
| `76700fcf-7a0e-4aca-b8ee-dde9aa3bdfbc` | memory lookup, then a file read | `memory action=search`, `bash ls -la …/chat_pa_duty.go`, `read internal/agent/chat_pa_duty.go`, `memory action=feedback` |
| `60c42b85-599d-444c-bc56-36eb137a142f` | memory lookup, then a keyword_search | `memory action=search`, `keyword_search chatPADutyNoPermissionMarker`, `memory action=feedback` ×2 |
| `26ef95ee-17e0-489f-be88-90c4ef8808ef` | memory lookup, then a code_search (run against the REBUILT binary) | `memory action=search`, `code_search ChatOpQuickBudgetSec`, `memory action=feedback`, `read internal/tools/chat_ops_authorization.go` |
| `f28b7148-5212-4b34-9de6-67a2f7dc1fcb` | the **fourth** session — run after the §2.1.1 fix, against the re-rebuilt binary | `memory action=search`, `code_search ChatOpQuickBudgetSec`, `memory action=feedback` ×2, `bash command -v gopi; …` |

Raw excerpt file: `artifacts/chat-tooluse-excerpt.txt`; answers: `chat-run-stdout.txt` /
`chat-run2-stdout.txt` / `chat-run3-stdout.txt` / `chat-run4-stdout.txt`; stderr (which carries the
chat-table session id): the matching `chat-run*-stderr.txt`.

All four turns did the memory and read-only work **in the first tool round, without a permission
ask**: a grep for ask-phrasing (`可以吗`, `需要我…吗`, `permission to`, `shall I proceed`) over all
four answers returns nothing. Two of the answers quote the constant they looked up at its
definition site — the second session the new marker (`internal/agent/chat_pa_duty.go:177`), the
third and fourth `tools.ChatOpQuickBudgetSec` (at `internal/tools/chat_ops_authorization.go:95` when
that constant still existed — it is part of the retired model, so treat the line as a HISTORICAL
reading and the quoted name as the part that mattered) — which is how a reader can tell the prompt
under test was the one that ran.

Honest limit: this is LLM behaviour evidence, not a guarantee — a prompt is followed
probabilistically, which is why the guard (`tools.ChatOpAdmit`) is the enforcement half and the
clause only names the band it applies.

### 4.4 The duplicated-clause red proof (the assertion added with the fix)

The count assertion was first run against the **pre-fix** file (`git checkout ab27cd248 --
internal/agent/chat_pa_duty.go`), which is what makes it a proof rather than a comment:

```
$ cp internal/agent/chat_pa_duty.go <session>/artifacts/chat_pa_duty.go.fixed
$ git checkout ab27cd248 -- internal/agent/chat_pa_duty.go      # the version WITH the duplicate
$ go test ./internal/agent/ -run TestChatPADutyQuickOpsKeepsTheOtherTwoClasses -count=1
--- FAIL: TestChatPADutyQuickOpsKeepsTheOtherTwoClasses (0.00s)
    chat_pa_duty_test.go:145: clause "(2) ONLY a change to the REPOSITORY is a task" is rendered 2
    times in the duty paragraph, want exactly once — a clause repeated is a prompt the model reads
    twice:
        … (2) ONLY a change to the REPOSITORY is a task … commands is YOURS. (2) ONLY a change to the
        REPOSITORY is a task … commands is YOURS. (3) OPERATOR-ONLY — …
FAIL
$ grep -c 'ONLY a change to the REPOSITORY is a task' internal/agent/chat_pa_duty.go   # source, pre-fix
2
$ cp <session>/artifacts/chat_pa_duty.go.fixed internal/agent/chat_pa_duty.go   # the fix restored
$ grep -c 'ONLY a change to the REPOSITORY is a task' internal/agent/chat_pa_duty.go
1
$ go test ./internal/agent/ -run TestChatPADutyQuickOpsKeepsTheOtherTwoClasses -count=1
ok  	github.com/nick/gopi/internal/agent	0.748s
```

`master` (`823305a17`, no branch commit) renders clause (2) **once** — the duplicate was this
branch's own, introduced by `ab27cd248` and removed by the fix commit; it is not inherited.

### 4.3 Raw streams

- `go test ./internal/agent/ …` and `go test ./internal/tools/ -count=1` were run through
  `job(action=start)`, whose stdout/stderr land under
  `/home/user/workspace/gopi-test-scratch/queue/queue-runtime-job-3435624/`; the chat sessions'
  own transcripts are the `artifacts/chat-run*` files named in §4.2.

## 5. Not Done

- The clause is **not** enforced by the guard in a new way; it names the band the existing guard
  applies. No new tool, no new config key, no new script, no resident process (Rules 3/35) — the
  mechanism and both constants already existed, which is the whole point of the task's constraint.
- A Chinese-language chat prompt limb was not added: the surrounding block is English, and the one
  Chinese phrase already in it is the operator's quoted trigger (记住). Mixing languages in the
  middle of a rule sentence would cost tokens without changing the decision.
- **The Rule 11 evidence is behaviour evidence, not an A/B measurement.** Four real chat sessions
  show the model doing the memory and read-only work in its first tool round with no ask, but no
  control session was run against the pre-change prompt, so this run does NOT claim the clause is
  the sole cause of the absence of ask-phrasing — the clause is a prompt, and a prompt is followed
  probabilistically. The enforcement half remains `tools.ChatOpAdmit`.
- **`TestDocAnchorsResolveInNormativeDocs` is still red on this branch** (244 permits, 12 stale,
  drift from master's doc-anchor backlog). §4.1.1 carries both sides' output; the branch does not
  silence it, and this report does not claim `./internal/agent` green.
- Two **pre-existing** `gofmt` offenders in the package (`internal/agent/prompt_test.go`,
  `internal/agent/storetiming_test.go`) were left alone — they are on master and unrelated to this
  diff; the two files this task touched are format-clean.
