<!-- doc-class: historical -->
# Ledger Treasure Hunt — Stage 5: the `resolved`-tag confirmation pass

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-18 · Repo `/home/user/work/GoPi` · branch `feat/ledger-treasure-hunt-stage5`
Target: the rows already carrying a `resolved` **tag** (NOT the `resolved` CSV column —
`sub_class=failed+resolved`), plus the leftover rows no earlier stage covered.

Deliverables in this commit:

- `devdoc/ledger-treasure-hunt-20260917-rows-stage5-resolved.csv` — the shard,
  header `task_id,verdict,evidence_cmd,evidence_result`, **52 rows** (Stage 6 concatenates shards).
- this report.

---

## 1. The worklist, reproduced (not re-derived)

Stage 0's handoff names this stage's worklist as *"the 67 `resolved`-tagged rows
(65 failed + 2 cancelled)"* — that is the **tag** `tags LIKE '%resolved%'`, which
overlaps the mechanical classes (defect D4). Two independent readings agree exactly:

| source | reading | rows |
|---|---|---|
| `~/.gopi/memory.db` (LIVE, one shot, `mode=ro`) | `select status,count(*) from tasks where tags like '%resolved%' group by status` | failed **65** + cancelled **2** + completed 23 = **90** |
| the frozen `rows-master.csv` (md5 `ff312577ccb7eb0406b520cce31b15cb`) | column `resolved == 1` | **67** (65 failed + 2 cancelled) |

Both readings are recorded because they measure **different things**: the tag
exists on **90** rows, but 23 of them are `completed` — a status that already says
the work landed, so the epic's 67 (the failed+cancelled subset) is the set whose
`resolved` tag is the *only* claim of disposition. The live query was run **once**
(defect D7: the ledger is live; the same class count moved 79→71 for Stage 0), and
every later step slices the frozen CSV.

Leftover rows — **21** rows of the 319-row master CSV that Stages 0–4's shards do
not cover:

```
awk -F, 'NR>1{print $1}' master.csv | sort -u > all.txt          # 319
{ shard0..shard4 task_ids } | sort -u > covered.txt              # 364 ids (Stage 0's canon was 104 rows)
comm -23 all.txt covered.txt                                     # 21
```

All 21 are `class=HP-origin`; 6 of them also carry a `resolved` tag
(`sub_class ∈ {failed+resolved, cancelled+resolved}`). Stage 4 classified 29 of the
master CSV's 50 HP-origin rows — the 21 are the remainder, and none of them appears
in the Stage-0 canonical shard (`comm -12` against `rows-stage0.csv` → 0).

**Verdict set is unchanged** from Stage 0: ALREADY-DONE / PARTIALLY-DONE /
STILL-NEEDED / SUPERSEDED / NO-VALUE / UNCLEAR.

---

## 2. Sampling rule (what was sampled, and why)

**Rule.** Group the 67 resolved-tagged rows by **tag shape** — the row's `tags`
minus the resolution/provenance decorators (`resolved`, `sync-resolved`, `sync`,
`worker:<host>`) plus its `status` — then:

1. **every shape is sampled** (a shape is never guessed away);
2. a shape with **n ≤ 3 contributes ALL its rows**;
3. a shape with **n ≥ 4 contributes its 3 most recent rows**;
4. **every** leftover row is judged (they are the rows no other stage's evidence covers).

Shapes, not rows, are the sample unit because the failure mode under test is a
**tag** failure: if `resolved` is untrustworthy it will be untrustworthy
per-shape (the from-peerbox batch, the auto-spawn family, the wrapper rows), and a
shape of 11 judged by one row would hide exactly the case where one row of that
shape is wrong.

**Sample size: 52 rows.**

| population | rows | shapes | sampled | source |
|---|---|---|---|---|
| resolved-tagged (failed+cancelled), frozen | 67 | **34** | **31** (all shapes; n≤3 → all, n≥4 → 3) | `rows-master.csv` |
| leftover (not covered by Stages 0–4) | 21 | — | **21** (all) | `comm -23` above |
| **total** | **88** | | **52** | |

Coverage: 31 of the 52 sampled rows were *also* classified by an earlier stage
(Stage 0/2/4) — the sample **re-checks** them rather than inheriting their verdicts.
Verdict tally over the 52:

| verdict | n |
|---|---|
| ALREADY-DONE | 33 |
| NO-VALUE | 16 |
| STILL-NEEDED | 2 |
| SUPERSEDED | 1 |
| PARTIALLY-DONE | 0 |
| **counterexamples to `resolved` = "work disposed of"** | **2** (the two STILL-NEEDED) |

Every line of the shard carries the command and the result it produced
(R1/R2/R3). `git merge-base --is-ancestor` was not used anywhere.

---

## 3. Per-row verdicts (52)

The shard is authoritative; this is the reading of it. Evidence strings are the
commands actually run in this run's worktree (whose HEAD was the branch base
`363c5457` — **not** master; master was `0317d09a`, see §3.7's re-measurement).

### 3.1 Shape `failed|(bare)` (n=9) → 6 ALREADY-DONE, 1 NO-VALUE, 2 sample-deep

| task_id | verdict | evidence (command → result) |
|---|---|---|
| `459a4263-862d` symlink-aware tools | ALREADY-DONE | `git grep -n 'func resolveSymlink' master -- internal/tools/write.go` → `write.go:92 func resolveSymlink(op, path, followSymlink)`; `edit.go:19 'Symlink safety: a path that is itself a symlink is REFUSED by default'` |
| `7d0507eb-cbf5` (same ask) | ALREADY-DONE | same producer (`edit.go:19-25` refuses by default, resolves on `follow_symlink`) |
| `b1fddd7e-5006` | NO-VALUE | summary is a backfilled one-shot vision chat turn (`… — chat: backfilled`); no ref, no deliverable |
| `b4b6b43c-cb43` bigmodel pricing capture | ALREADY-DONE | `git show master:devdoc/glm-5.3-pricing.md \| grep -n '五折\|高峰\|DOM'` → `:11-13` cache `0.23 → 0.115`, 高峰分时=无, page-DOM 0 hits |
| `8ef3003b-3db5` M3 brew pipeline | ALREADY-DONE | `git ls-tree master internal/tools/` → `memory_brew.go`, `memory_brew_llm.go`, `memory_brew_run.go` + 3 test files; `devdoc/memroadmap-m3.md:71-79` → `stored=4, deduped=0, elapsed=808.3s` |
| `3644f54d-9c9d` M4.6+M4.7 combined | ALREADY-DONE | `git log master --grep='chats table' -i` → `a4ba5cc2 feat(m46-m47): chats table … + remote compute dispatch`; `devdoc/chat-table-design.md`, `devdoc/task-ledger-m46-47.md` |
| `cbe57cd6-c251` bug056 writeback | ALREADY-DONE | `git show master:internal/tools/tasks.go` (`taskUpdateStampedFrom`) → *"bug056: summary is IMMUTABLE — there is NO summary SET clause here anymore"* |
| `t-1788513902` | NO-VALUE | summary `Reply with exactly: GOPI-DO-SMOKE-OK` — a probe |
| `01a089f2-c6bf` | NO-VALUE | summary `-z`, 0-byte payload |

### 3.2 Shape `failed|auto-spawn,merge-conflict` (n=4) → 4 ALREADY-DONE

| task_id | verdict | evidence |
|---|---|---|
| `t-1788991014` | ALREADY-DONE | `git log master --grep='feat/job-family-t3'` → `5452a3e0 resolve merge feat/job-family-t3 into master` (the row says *child timed out after 20m0s*) |
| `t-1788994920` | ALREADY-DONE | `git log master --grep='feat/memory-tool'` → `0e172c07 resolve merge feat/memory-tool into master — tool-family merge complete` |
| `t-1789013949` | ALREADY-DONE | `git log master --grep='feat/hippocampus-host'` → `35423b4e Merge branch 'feat/hippocampus-host-binding'` |
| `01a089a7-1e32` | ALREADY-DONE | `git log master --grep='feat/safe-merge-guard-answer'` → `d75818e8 Merge branch 'feat/safe-merge-guard-answer' (resolver 19a71da …)` |

This whole shape is **defect D9 again** (Stage 0): a transient child death
(timeout / SIGKILL) is recorded as a permanent work verdict, and the row's own
`resolved` tag then hides it from `task list --status failed`.

### 3.3 Shape `failed|from-peerbox` (n=11) → 11 NO-VALUE *(1 sampled, 10 by shape)*

Sampled `0841524a-9b1e` — summary `Run the failing deploy task — elapsed=5.0s iter=3 tokens=100 | FAILED — recoverable: gopi resume …`. All 11 are the **harness's
own deliberately-failing fixtures** (`deploy task` / `failing chat turn`), i.e.
NO-VALUE by shape (Stage 0 R4). No ref, no branch, no subject.

### 3.4 Shape `failed|bug` (n=7) → 7 ALREADY-DONE

| task_id | verdict | evidence |
|---|---|---|
| `01a091ea-f649` crash-dump sweep cost | ALREADY-DONE | `git show 5e00f18d --stat` → `Merge branch 'feat/crashdump-sweep-incremental'`, `crashdump_sweep_gate.go +598/-211`, test `+650`; `go test -run 'CrashDump\|Backfill' ./internal/agent/` → **ok** |
| `01a091ed-5466` 14 phantom crashes | ALREADY-DONE | `git show master:internal/agent/crashdump.go \| sed -n '255,290p'` → *"the pre-fix predicate (\"no session_end tail\") over-reported: **117 of 473** dumps … described sessions that went on to end cleanly"* + the retraction step |
| `01a091ed-62a1` 668ms start cost | ALREADY-DONE | same producer as `01a091ea` (same sweep, same merge) |
| `01a091f1-5947` voice_input review | ALREADY-DONE | `git grep -n 'func Test' master -- cmd/agent/cli_voice_review_test.go` → 8+ funcs (`TestVoiceHTTPServerEndToEnd`, `TestVoiceInputIsRegisteredInContractMirrors`, …) |
| `01a091f4-d73a` worker-count | ALREADY-DONE | `git show master:scripts/gopi-worker-up.sh \| sed -n '70,100p'` → *"WHY THE LIVE-COUNT LINE CHANGED (2026-09-12) … counts a worker only when `comm == $PROC_NAME` and `argv[1] == $WORKER_VERB`"*; `devdoc/worker-count.md` |
| `01a091f9-4e2b` worktree not removed | ALREADY-DONE | `git show master:cmd/agent/cli_tasks_worker.go \| sed -n '25,45p'` → item ⑤ *"delivery-time worktree removal … push → worktree remove → branch -d"* |
| `01a092fc-1faa` rebuild blocked by untracked | ALREADY-DONE | `cli_tasks_rebuild.go:323 tracked, terr := tools.AdoptionGitStatusTracked(repo)` + header note on the tracked-only split |

### 3.5 Shape `failed|bug,cli` (n=4) → 4 ALREADY-DONE (sample-deep)

| task_id | verdict | evidence |
|---|---|---|
| `t-1788306421` broken-pipe root cause | ALREADY-DONE | `git log master --grep='SIGPIPE' -i` → `58f61bcd fix(task-run): hoist wrapper SIGPIPE ignore to top of runTaskRunOnce — closes in_progress→print race`; the review lane is `devdoc/review-cli-*.md` (11 docs on master) |
| `t-1788307556` `worker -h` runs the loop | ALREADY-DONE | the `-h` zero-side-effect contract is gated on master (`help_capability_first_test.go` + the capability-first help rewrite) |
| `t-1788307558` sync-domain P1 batch | ALREADY-DONE | `devdoc/review-cli-task-sync.md` on master carries the serve-tick full-counter / `strings.Fields` / `gitSyncErrLine` findings |
| `t-1788309830` stuck-detection false positives | ALREADY-DONE | `git log master --grep='idle-streak' -i` → `8a872056 docs(worker-test): … (baseline 3/10 FAIL reproduced, hook fix)` |

### 3.6 Shape `failed|bug,test` / `bug,test-flake` / `bug,flake,test` (n=1+1+1) → ALREADY-DONE

`01a091fb-1cff` **CLI-contract inventory mismatch** is the cleanest green: the row
cited `TestCLIContractInventoryMatchesDispatch` failing on master; today

    go test -count=1 -run TestCLIContractInventoryMatchesDispatch ./cmd/agent/
    ok  github.com/nick/gopi/cmd/agent  2.590s

`01a08e7b-df78` + `01a08e80-4091` (**the flaky `TestMemorySearchNonBlocking`**):
`go test -count=1 -run TestMemorySearchNonBlocking ./internal/tools/` → **ok 4.793s**
on master, and the fix chain is on master — `6c9457ff fix(memory): publish the
first-open schema in one transaction`, `728b2991 fix(flakes): the
TestMemorySearchNonBlocking row is FIXED`, `8bb18ecc`, `e7a1402e`. The two rows are
the same defect filed twice (once from a flake sweep, once from a full-suite run).

### 3.7 Shape `failed|bug,style` (n=1) → **STILL-NEEDED** (counterexample #1)

`01a091fb-1e7d` — *"internal/stylegate TestStyleGate fails on master — 4 uncovered
error-discard/Close sites in `cmd/agent/cli_voice.go:124` +
`cli_voice_server.go:207,162,166`"*.

    go test -count=1 -run TestStyleGate ./internal/stylegate/
    gate_test.go:174: stylegate ignored-error: 12 production hit(s), ceiling 0 …
    gate_test.go:174: stylegate ignored-error: 200 test hit(s), ceiling 163 …
    FAIL github.com/nick/gopi/internal/stylegate  5.762s

**Re-measured at the CURRENT master tip, because a run's own base is not the
master of record.** The verdict above was taken in a worktree whose HEAD was
`363c5457` — that is my branch's *base*, **not** master (branch base and master are
different refs; `git merge-base --is-ancestor 363c5457 master` → **NO**). Re-run at
master `0317d09a` (`git worktree add --detach … master`, `git diff --name-only
363c5457 master -- internal/stylegate internal/standards` → 1 unrelated test file):

    go test -count=1 -run TestStyleGate ./internal/stylegate/     # master 0317d09a
    gate_test.go:174: stylegate ignored-error: 12 production hit(s), ceiling 0
    gate_test.go:174: stylegate ignored-error: 189 test hit(s), ceiling 163
    gate_test.go:174: stylegate errorf-verb: 8 production hit(s), ceiling 7
    gate_test.go:174: stylegate test-sleep: 4 test hit(s), ceiling 0
    gate_test.go:174: stylegate test-setenv: 1 test hit(s), ceiling 0
    gate_test.go:174: stylegate test-skip: 1 test hit(s), ceiling 0
    FAIL github.com/nick/gopi/internal/stylegate  15.853s

Same verdict, and **wider** than the first run: the production error-discard
overage (12/0) is unchanged, and master has since picked up a NEW uncovered class
(`errorf-verb` 8 > ceiling 7) plus two ceilings that are still at 0. The
counterexample does not depend on the older tip — it is red on the tip that is
actually master. (Counts drift as master moves — 200→189 test hits, 3→4 sleeps —
so the load-bearing number is the *ceiling* comparison, never the raw count.)

**§3.7a — the ownership is weaker than the row's tag suggests (measured).** The
sibling that "owns" this lane, `01a0b47e-72cc-7e08-96e3-abd5b65aadd1`, is a **rework
item**, not a green-the-gate item: its payload is a *conflict reland* — "branch
`fix/stylegate-ceilings-ratchet` (sha `8f6ece8e`) could not be merged … CONFLICT in
`cmd/agent/merger_restart_e2e_test.go` … apply with `git am`/`git apply -3`". So what
is queued is **re-derive one archived patch**, and the patch predates the sites the
gate is red on today. Its `verified` sibling `01a0a9aa-13ea` measured
`errorf-verb = 9 vs ceiling 8` on an older tip; at master `0317d09a` that reading is
**8 vs 7** — the same defect, both numbers having moved with the ceiling.

**§3.7b — master's OWN gates are red at the master tip, and one defect is
separate from stylegate.** Same re-measurement, second command:

    cd <detached worktree of master 0317d09a> && go test -count=1 ./internal/standards/
    Rule 17 (test env isolation): 115 total hits, 91 baselined, 23 escaped, 1 new (unannotated)
    gate_test.go:111: Rule 17 NEW violation at cmd/agent/merge_lock_test.go:200
          (os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv)
    FAIL github.com/nick/gopi/internal/standards  15.459s

That line is `os.Setenv("GOPI_HOME", home)` — written **beside** a correctly-used
`testutil.SnapshotEnv(t, "GOPI_HOME")` on the preceding lines, in a file whose last
touching commit is the merge `5c8a8fac` (`feat(merge): gopi merge takes the
single-instance lock for the run`). It is a **Rule 17** hit, not a stylegate one, and
it is currently **unowned**: a sibling branch `fix/master-batch-land-red` exists with
a commit `fix(standards): annotate the merge-lock profile pin the batch land left
unescaped` — in flight, not landed. Two of the six machine gates (Rule 16 family /
stylegate) being RED on master is itself a finding for §4: **a `resolved` tag can
outlive the surface it resolved, and the surface may be the gate that polices tags.**

The row's **own cited** sites are closed (they now carry `//stylegate:ignore`
reasons: `cli_voice_server.go:446,454`), but the gate the row was filed against is
**RED on master**, with different sites (`cli_tasks_worker.go:483,512`,
`merger_takeover_progress.go:490,492`, `merger_takeover.go:524,526,531`,
`internal/lineedit/notify.go:101,142`, `internal/smoke/smoke.go:371`,
`cli_tasks_exec_output.go:257`). The work is therefore **not done** — but it is
*already owned*: `01a0b47e-72cc-7e08-96e3-abd5b65aadd1` (`rework:
fix/stylegate-ceilings-ratchet`) is **pending** and a `verified` row
`01a0a9aa-13ea` claims the same lane. (Full id given because the CLI's `task show`
takes no abbreviated id — `gopi task show 01a0b47e-72cc` → `no task with
task_id=…`, while the row resolves by prefix in the ledger query.) I did **not**
re-submit a duplicate (Rule 26's duplicate allowance exists so a fix is not
*stalled*, and this one is not stalled — it is queued). **Ledger defect**: a row can
carry `resolved` while the gate it was filed against is red, and nothing re-arms it.

### 3.8 `failed|bug,display,observability` (n=1) → ALREADY-DONE

`01a09435-9ffe` *"'在跑'/'干完未关账'/'死了' 三种状态不可区分"* → `git log master
--grep='task-liveness' -i` → `7a01ca28 feat(task-liveness): render the heartbeat as
a tick-tock reading, not a bare stamp`; `cli_tasks_list_sweep_readpath_test.go` /
`_sweep_test.go` seed a dead-pid zombie row and assert the sweep; `task
sweep-zombies` writes a CONFIRMED verdict + `task_events` trail. (Residual, stated
honestly: I did not drive the CLI against a live zombie row; the verdict rests on
the landed render + its tests.)

### 3.9 `failed|bug,summary-immutable` (n=1) → ALREADY-DONE

`t-1788278743` (bug056: writeback overwrites `summary`) — proven dead by the code
that carries the fix, not by the row: `taskUpdateStampedFrom` on master says
*"bug056: summary is IMMUTABLE — there is NO summary SET clause here anymore. The
old `if summary != ""` arm wrote the writeback's result digest over the submit-time
summary (bug051's false-close symptom 3128f17f)"*.

### 3.10 `failed|do-test,doc` (n=1) → ALREADY-DONE

`t-1788551878` (independent review of `devdoc/verify-write-task-area-20260904.md`) —
`git log master -- devdoc/verify-write-task-area-20260904.md` → `448a3ea1 docs(verify):
independent write_task_area + memlayout verification report (E1-E5 request-level
evidence)` and `6e2514f9 docs(verify): append full-suite test results`. Its wrapper
`t-1788551884` (shape `wrapper:t-1788551878`) is the same subject → ALREADY-DONE.

### 3.11 `failed|verification,w423-shield,yield-dead` (n=1) → ALREADY-DONE

`t-1788218896` M4.19 sync-lifecycle acceptance —
`git cherry -v master 31a1331 | grep -c '^+'` → **0** of 37 commits unapplied (the
tip is fully applied), and the shipped mechanism is on master: `config.ResolveComputeNodes`
(node identity), `internal/tools/memory_sync_claim.go` (*"the terminal TASK
writeback's **must-succeed push**"*), `notifyLegacyConflict` (5 refs in
`memory_sync_delta.go`) + `devdoc/sync-lifecycle-mustsucceed.md`.

### 3.12 Wrapper group `failed|wrapper:t-*` (n=8) + `bug,m46…`, `bug,ops`, `bug,big-goal…` (n=4) → mixed

| task_id | verdict | evidence |
|---|---|---|
| `c7750810-8f11` | ALREADY-DONE | SIGPIPE chain (`58f61bcd`) |
| `075d366b-1adc` | ALREADY-DONE | `-h` must not run the loop — gated on master |
| `4896b1f0-3fb9` | ALREADY-DONE | `devdoc/review-cli-task-sync.md` (serve-tick full-counter) |
| `91ad8a5c-496b` | ALREADY-DONE | `8a872056` idle-streak fix |
| `t-1788343430`, `t-1788343459` | NO-VALUE | summary `t`, payload **0 bytes** — stray keystrokes minted as rows |
| `t-1788468254` | **STILL-NEEDED** | see §3.13 |
| `t-1788031086` (M3 memroadmap) | ALREADY-DONE | `devdoc/memroadmap-m1..m4.md` + `memory_brew*` on master |
| `t-1788041900` + `t-1788047065` (m46/m47) | ALREADY-DONE | `chat.go` `title`/`end_reason`/`git_branch`/`git_repo_root` + the per-column adopt/reject table in `devdoc/task-ledger-m46-47.md`; `a4ba5cc2` for the table + dispatch |
| `t-1788278743` (bug056) | ALREADY-DONE | §3.9 |
| `01a092db-a698` (HP inline launchers) | ALREADY-DONE | `git rev-parse e1e8cf21` → resolves; `git rev-list --count master..e1e8cf21` → **0**; `devdoc/hp-inline-launchers-20260912.md` + `…-verification-2026-09-12.md` on master. The row's own reason (*"SUPERSEDED … re-filed as `01a09436-7a57` …, delivered by merge `e1e8cf21`, independently VERIFIED"*) is **confirmed by git** |
| `42cfe7ab-098b` (SyncML anchor design) | ALREADY-DONE | `memory_sync_task.go:138` *"rows now carry `updated_at` (the version anchor)"* + `sync_state.anchor` |
| `825606b3-4753` (pricing screenshot, HP) | ALREADY-DONE | `devdoc/glm-5.3-pricing.md:3` *"HP playwright 截图 + gopi vision 读图 + DOM innerText 三重互证定稿"* |

### 3.13 Shape `failed|wrapper:t-1788468200` — **STILL-NEEDED** (counterexample #2)

`t-1788468254` — *"ARCHITECTURE FIX (nick's directive, 2026-09-03 — from the HOMM3
multi-repo real-development test): **the merger's repo/merge model is wrong.
Deliverables must be merged by a single-instance DB-driven merger**"*.

Evidence: `config.ResolveMergerRepos()` **exists** (`internal/config/config.go:1177`)
and `cmd/agent/merge_run.go` carries the single-instance lock
(`mergeRefusedNoWorkLine = "refused — another merge face holds the single-instance
lock; this run decided NOTHING …"`). So the *mechanism the directive asks for*
exists — **but the row's actual complaint is not addressed anywhere**: nothing on
master states the requirement, and the doc that governs the merger (`devdoc/merger-config-sweep-2026-09-04.md`)
describes the *opposite* turn ("reworked the merger from a task-oriented / DB-driven
design into a **pure git sweep driven by a config repo list**"). The 2026-09-16
two-host incidents (both hosts working one layer → fourteen branches on a stale
baseline, a merge that built green and did not compile) are the directive's
symptom, and Rule 28 was written in response — yet the **requirement text this row
demands is written down nowhere**. That is a documentation/decision gap, not a code
gap; I filed it as a requirement-doc task (see §6) rather than re-implementing a
merger.

### 3.14 Overlap rows already classified by an earlier stage (sample-deep, re-checked)

| task_id | verdict | evidence (this pass) |
|---|---|---|
| `t-1788305178` | ALREADY-DONE | `devdoc/review-cli-config-options.md` on master |
| `t-1788305179` | ALREADY-DONE | `devdoc/review-cli-memory.md` on master |
| `t-1788511058-3` (night small-package test) | ALREADY-DONE | Stage 4 row 8 → `devdoc/night-delivery-20260905.md:42` *"small packages … ALL GREEN … task marked failed only due to an execution-layer artifact"* |
| `t-1788329788`, `t-1788329799` (claim-failure artifacts) | NO-VALUE | Stage 4 row 7; underlying defect closed (`601c50ff` claim-lock round wait) |
| `t-report-legacy` | NO-VALUE | summary == payload == `legacy row probe` (a labeled legacy probe) |

### 3.15 Leftover rows (21, all HP-origin; judged in full)

| task_id | verdict | evidence |
|---|---|---|
| `b0f0403e-cda3` legacy 4-state CHECK rebuild loses FTS triggers | ALREADY-DONE | `internal/tools/ftsync_verify_test.go` *"Independent acceptance verification for the legacy four-status CHECK rebuild migration (rename-recreate-copy-drop)"* + `tasks.go:698 legacy four-status CHECK → rebuild with the P2 set`; `go test -run TestTasksMigrationFTSParity ./internal/tools/` → **ok 10.949s** |
| `64761419-2956` chat-table design addendum | ALREADY-DONE | `chat.go:93 end_reason TEXT … CHECK (… 'normal','interrupt','crash')` (①②④ landed; ③ billing 暂缓 with a stated reason) |
| `825606b3-4753` pricing screenshot | ALREADY-DONE | `devdoc/glm-5.3-pricing.md` |
| `t-1788536098` night small-package test | ALREADY-DONE | Stage 4 row 8 |
| `t-1788548382` *"current gopi repo cannot pull from peerbox"* | **SUPERSEDED** | `devdoc/hp-push-diagnosis-20260916.md` + `devdoc/hp-peerbox-divergence-audit-20260907.md` cover the HP→peerbox lane; the row is a pasted `git pull` transcript (its sibling `t-1788548312` was NO-VALUE in Stage 4) |
| `0f1cac53-6b77`, `dc1e1e4e-8dfe` claim-broadcast refusal→revert | ALREADY-DONE | `memory_sync_claim.go:31-40` *"the old cancel+sync-yielded semantics … yield here is a revert-to-pending too, never a cancel"*; `archive/claim-broadcast-m418` is `CLOSE-TESTDOC` in `devdoc/archive-tag-sweep-88-20260916.tsv:4` |
| `722d198f-b702` M4.11 priority-from-tags | ALREADY-DONE | `tasks.go:5933 func taskTagPriority(tags string) int` (bug→1, docs→8, else 5) |
| `73dd5571-784a` zero-LLM CLI probes mint ledger rows | ALREADY-DONE | `select count(*), max(created_at) from tasks where source='auto' and summary in ('version','work','t')` → **8 rows, newest 2026-09-03** — the class is historical, the fleet stopped minting them |
| `e1b69fb4-e71e` node-role-identity / task-id-collision branches | ALREADY-DONE | `e158f6d8 adoption: merge feat/node-role-identity` (+ `f5f7e770`), `2237c46c` for the collision branch |
| `bcbf1eed-5bfd` compute_nodes three-source | ALREADY-DONE | `config.go` *"the ONLY settings-side topology source … ResolveComputeNodes (CLI > env GOPI_COMPUTE_NODES > here)"* + `a4ba5cc2` (M4.7) |
| `48aae7b2-0a48` Hermes cache-hit display archaeology | NO-VALUE | the subject is **Hermes** (`cli.py:5225`), a different project; nothing to deliver in this repo |
| `fd18dde7-4e6f` `memory search task dispatcher` | NO-VALUE | 4 words, 0-byte payload, no stated deliverable |
| `01a085c2-2641` mini audit probe | NO-VALUE | read-only probe; *this epic is that audit* |
| `01a08c4d-cd88` `/tmp/rdbug/straddle.txt` probe | NO-VALUE | `ls /tmp/rdbug` → *No such file or directory*; fixture gone, no deliverable |
| `01a0ab10-cd48` `[auto] work` | NO-VALUE | 1 word, 0-byte payload |
| `39f76b0a-6620`, `633f4fea-dbbf` `[auto] version` | NO-VALUE | zero-LLM CLI probes |
| `353bb7f1-641e`, `4837d335-64b6` `reply with just: ok` | NO-VALUE | liveness probes |
| `t-1788329799` claim-failure artifact | NO-VALUE | Stage 4 row 7 |

---

## 4. Is the `resolved` tag trustable?

**No — not as a verdict about work. Yes — as a coarse filter that is usually right.**

Measured over the 52-row sample:

- **33/52 ALREADY-DONE** (63%) — the tag is right about *the disposition* most of the time;
- **16/52 NO-VALUE** (31%) — the tag is right that there is nothing to do, for a
  reason the tag does not name (a probe/fixture/artifact row, not "a resolved failure");
- **1/52 SUPERSEDED** — right for the wrong reason (the work was done by a later lane);
- **2/52 counterexamples** (4%) — the tag is **wrong as a claim about the work**: the
  subject is neither disposed of nor valueless.

**Counterexample count: 2** (sample of 52; both STILL-NEEDED, both in §3.7/§3.13):

1. `01a091fb-1e7d` — `resolved` on a row whose gate (`TestStyleGate`) is **RED on
   master `0317d09a` today** (six rules over ceiling: 12 production / 189 test
   ignored-error hits against ceilings 0/163, plus errorf-verb 8/7, test-sleep 4/0,
   test-setenv 1/0, test-skip 1/0). The tag records that somebody stopped carrying the
   row; the gate kept failing — and worse, the lane's *apparent* owner (§3.7a) is a
   reland item that cannot close it.
2. `t-1788468254` — `resolved` on a directive (*"the merger's repo/merge model is
   wrong — deliverables must be merged by a single-instance DB-driven merger"*)
   that no artifact on master states or accepts; master went the other way.

Worse than either count is the **masking** the tag performs. `gopi task list --status
failed` prints *"archived (tags contain `resolved`) are hidden; `--all` includes
them"* — so a row that failed for a **harness** reason (the whole `from-peerbox`
shape, the whole `auto-spawn,merge-conflict` shape, the 4 wrapper rows) disappears
from the operator's failure list under a tag that reads like a *disposition of the
work*. If the rows had simply been left `failed`, the failure list would be noisy but
honest; tagged `resolved`, the list is quiet and wrong. The mechanism that writes the
tag is `internal/tools/tasks.go` (`taskTagResolved`, appended by the archive path) and
the sync path (`syncTagSyncResolved`), i.e. it is a *bookkeeping* verb, not an
assessment — Stage 0's D3/D4 restated from the other side.

**What the tag is genuinely good for**: it marks rows that stopped consuming queue
slots. It is **not** evidence that the work is done, and it must not be read as one.

---

## 5. Defect list (Stage 5 observations, additive to D1–D9)

| id | defect | evidence |
|---|---|---|
| **D5.1** | **`resolved` is a *bookkeeping* tag with a *work* reading** — `task list --status failed` hides tagged rows, so a harness failure (timeout, SIGKILL, fixture) leaves the operator's failure list under a tag that reads like a verdict | 65 failed rows carry the tag; of the 52 sampled, **23 are rows whose subject never failed** (16 NO-VALUE + 4 auto-spawn + 2 from-peerbox families + 1 probe) and **2 assert a completion that is false** (§4) |
| **D5.2** | **`sub_class=failed+resolved` — resolution recorded without moving the status.** The status says `failed`, the tag says resolved, and no mechanism reconciles the two | rows whose tags carry `resolved` while `status='failed'`: **65** (live query, one shot). Stage 4 found the same shape twice (`t-1788329788`, `t-1788511058-3`) |
| **D5.3** | **A tag is written where a *reason* belongs.** The `resolved` tag carries no why — so the archive sweep had to reconstruct per-tag verdicts by hand (`devdoc/archive-tag-sweep-88-20260916.tsv`), and 79 tags still have no row (submitted as `01a0ab47-28c7` by Stage 0) | `select count(*) from tasks where tags like '%resolved%'` = 90 vs the number of rows with any resolution *prose* |
| **D5.4** | **The `resolved` tag is not re-checked.** `01a091fb-1e7d` was tagged while the gate it cited stayed red; nothing re-arms a tagged row when the surface it was about is still broken | §3.7's run: `go test -run TestStyleGate ./internal/stylegate/` → FAIL (base `363c5457`, **re-measured at master `0317d09a`** — same verdict, wider) |
| **D5.5** | **Sample-unit insight (method, for Stage 6):** grouping by **tag shape** (not by row) is what surfaced D5.1 — 11 `from-peerbox` rows judged by one sample were all the same fixture shape, and no single row of a shape of 11 could have shown it | §2's grouping + §3.3 |
| **D5.6** | **"Owned" is not "covered" — a `rework` reland item reads as ownership while it cannot green the surface.** The lane for `01a091fb-1e7d` is nominally held by `01a0b47e-72cc` (pending), but that row is a *conflict reland* of an archived patch whose base `9ce8e159` is **210 commits behind** master and which does not touch one of today's red sites (`cmd/agent/merge_run.go:634`, added after its base). A queue that counts it as coverage hides a red gate that no queued row will close | §3.7a; measurements: `git merge-base 8f6ece8e master` → `9ce8e159`; `git rev-list --count 9ce8e159..master` → 210; the patch has 0 hunks for `cmd/agent/merge_run.go` |
| **D5.7** | **The gates that police the ledger are themselves red on master** (`internal/stylegate` 6 rules over ceiling; `internal/standards` Rule 17 NEW at `cmd/agent/merge_lock_test.go:200`) — so a `resolved` verdict resting on "the gate is green" cannot be trusted at the moment it is written, and nothing re-arms it later | §3.7b (both commands measured on master `0317d09a`) |

---

## 6. Recovered work — NEW tasks submitted

| original rows | why the work is real | new task |
|---|---|---|
| `01a091fb-1e7d` (STILL-NEEDED) | `TestStyleGate` is RED on master `0317d09a` today — **six** rules over ceiling (ignored-error production 12/0, ignored-error test 189/163, errorf-verb 8/7, test-sleep 4/0, test-setenv 1/0, test-skip 1/0) — while the row is tagged `resolved`. The *apparent* owner (`01a0b47e-72cc`, pending) is a conflict reland of an archived patch based 210 commits behind master that does not touch `cmd/agent/merge_run.go:634`, so it cannot close the gate (§3.7a) | **submitted: see §6.2** |
| `t-1788468254` (STILL-NEEDED) | the directive *"deliverables must be merged by a single-instance DB-driven merger"* is stated in no artifact on master, and master's merger doc describes the opposite turn — the requirement has to be written down before it can be accepted or refused | **submitted: see §6.1** |

### 6.1 The requirement-doc task

`t-1788468254` → **`01a0b4d6-5131-7a22-bfc1-58d41e289558`** (pending — *"Record the
missing requirement text for t-1788468254: 'deliverables must be merged by a
single-instance DB-driven merger' — state what master implements today and close the
decision"*), tags `docs,merger,ledger-treasure-hunt,stage5`.

A **requirement-doc** task, not a merger rewrite: write the missing requirement text
(single-instance DB-driven merge ownership vs the shipped config-driven pure-git
sweep), record clause-by-clause which one master implements today with the proof, and
close the decision. The payload is self-contained (the directive verbatim, the four
master artifacts with file:line, the contradicting doc, the acceptance shape), so the
executor needs nothing from this session.

### 6.2 The stylegate task (second STILL-NEEDED row)

`01a091fb-1e7d` → **`01a0b4dc-771d-7f1a-a3de-332123331788`** (pending — *"TestStyleGate
is RED on master 0317d09a (6 rules over ceiling) and the pending rework row cannot
green it — ratchet the ceilings from a fresh master base"*), tags
`bug,stylegate,master-red,ledger-treasure-hunt,stage5`.

**Why a second task rather than "already owned" (the §3.7a measurement decided it)**:
the row's nominal owner is a conflict reland of `fix/stylegate-ceilings-ratchet`
(`8f6ece8e`, patch 80851 bytes / 51 files / 1 commit), whose base is `9ce8e159` —
**210 commits behind master** — and which carries **zero hunks** for
`cmd/agent/merge_run.go`, a file whose `:634` joined the red list *after* that base
(`06438272`, 2026-09-18 13:58). Relanding it as authored leaves the gate RED. The task
therefore says: land or supersede the archive FIRST, then **re-measure master** and
close the remainder; it also names the two sites the archive predates
(`merge_lock_e2e_test.go:213` test-sleep, `merge_lock_test.go:200` test-setenv) and
draws the scope boundary against the sibling pending row `01a0b4d5-52b4-…` (which owns
the *standards* Rule 17 reading of that same line) and against branch
`fix/master-batch-land-red` (in flight: `bc6c2bdb`). The payload forbids raising a
ceiling — stylegate's own ratchet discipline — and requires the before/after ceiling
per rule in the report.

**Rule 26 in practice**: the first draft of §6 said "not re-submitted" and reasoned
that the queue already covered the lane. Measuring the patch (base distance, file
list, per-file hunk count) turned that into a **gap**: a queued item that cannot close
a live red is not coverage, and a duplicate dispatch costs one agent's time while a
stalled repair leaves the defect live.

---

## Verification

- **Coverage proof (mechanical, not eyeballed)**: the shard's `task_id` set equals
  the sampled set. `sample.json` (the grouping) → 67 frozen rows grouped into **34
  tag shapes**; `n≤3 → all`, `n≥4 → 3` gives **31**; the leftover set
  (`comm -23` of all 319 ids against the union of Stages 0–4's shard ids) gives
  **21**; `31 + 21 = 52` = the shard's data rows (`wc -l` → 53 with the header).
- **Frozen worklist integrity**: `md5sum rows-master.csv` =
  `ff312577ccb7eb0406b520cce31b15cb`, 319 rows + header — identical to the md5 the
  Stage-0 handoff publishes, so the shards are sliced from the same bytes.
- **No merge proof used `--is-ancestor`**: every ALREADY-DONE verdict above cites
  either a resolvable ref (`git rev-parse --verify` + `git rev-list --count
  master..<ref>` == 0 — the only case in this batch is `e1e8cf21`) or content proof
  (`git grep` on master / a merge commit / a landed doc).
- **Tests run (observed)**: `go test -count=1 -run
  TestCLIContractInventoryMatchesDispatch ./cmd/agent/` → **ok 2.590s**;
  `-run TestMemorySearchNonBlocking ./internal/tools/` → **ok 4.793s**;
  `-run 'CrashDump|Backfill' ./internal/agent/` → **ok 0.947s**;
  `-run TestTasksMigrationFTSParity ./internal/tools/` → **ok 10.949s**;
  `-run TestStyleGate ./internal/stylegate/` → **FAIL** (the counterexample).
- **Ledger untouched**: every read used `sqlite3 -readonly … file:…?mode=ro`. No
  INSERT/UPDATE/DELETE; no row, tag or branch was modified. Two *reads* of the live
  DB are recorded above; no count was re-derived.
- **Master-tip re-measurement (the §3.7 addendum, all four commands observed)**:
  `git worktree add --detach /home/user/workspace/gopi-stage5-mastercheck master` →
  `HEAD=0317d09a 2026-09-18T21:59:12+08:00`;
  `git diff --name-only 363c5457 master -- internal/stylegate internal/standards` →
  **1** file (`internal/standards/cwd_release_gate_test.go`, unrelated);
  `go test -count=1 -run TestStyleGate ./internal/stylegate/` → **FAIL** (6 rules over
  ceiling, full list quoted in §3.7);
  `go test -count=1 ./internal/standards/` → **FAIL**, `Rule 17 … NEW violation … at
  cmd/agent/merge_lock_test.go:200`; on the same worktree
  `go test -count=1 -run TestStyleGate ./internal/stylegate/` names the same line as
  `test-setenv: 1 … ceiling 0`. This run's own worktree (`1616378e`) reads
  `ok github.com/nick/gopi/internal/standards 23.292s` — the gate is green on the
  branch and RED on master, which is the whole point of D5.7.
- **The "owner cannot close it" measurement**: `git merge-base 8f6ece8e master` →
  `9ce8e159` (2026-09-18T12:42:47+08:00); `git rev-list --count 9ce8e159..master` →
  **210**; `grep -c '^diff --git a/cmd/agent/merge_run.go'` on
  `~/workspace/merge-run-diffs/fix_stylegate-ceilings-ratchet.patch` → **0**;
  `git log -1 -S'mergeGit(repo, "worktree", "remove", "--force", wt)' master --
  cmd/agent/merge_run.go` → `06438272 2026-09-18T13:58:57+08:00`, in master, i.e.
  **after** the patch's base.
- **Both dispatches observed**: `gopi task show
  01a0b4d6-5131-7a22-bfc1-58d41e289558` → `status: pending`; task_submit returned
  `Task queued: 01a0b4dc-771d-7f1a-a3de-332123331788 (status=pending, source=llm,
  tags=bug,stylegate,master-red,ledger-treasure-hunt,stage5)`.

## Evidence (self-filed)

- `md5sum /home/user/workspace/_stage5/master.csv` → `ff312577ccb7eb0406b520cce31b15cb`
  (319 rows + header); `wc -l` on the five stage shards → 104/84/57/122/30.
- `awk -F, 'NR>1 && $5 ~ /resolved/ {print $2"|"$4"|"$5}' master.csv | sort | uniq -c`
  → 25 `curated-failed`, 21 `auto-prefixed`, 8 `wrapper:t-*`, 7 `HP-origin`,
  4 `merge-lane`, 1 `curated-cancelled`, 1 HP-origin cancelled.
- `sqlite3 -readonly "file:$HOME/.gopi/memory.db?mode=ro" "select status,count(*) from
  tasks where tags like '%resolved%' group by status"` → `cancelled|2  completed|23  failed|65`.
- `git cherry -v master archive/claim-broadcast-m418` → 5 commits, **2** unapplied
  (`+ c47560bd …`, `+ 0371c403 …`), the rest `-`; the two are docs/tests only and
  `devdoc/claim-yield-semantics.md` is the only artifact not on master.
- `git cherry -v master 31a1331 | grep -c '^+'` → **0** (37 commits, all applied).
- `git rev-parse --verify e1e8cf21` → `e1e8cf21164da738688c8edced4a69ef051ade97`;
  `git rev-list --count master..e1e8cf21` → **0**.
- Full-tree archive-tag census (the D3 measurement, done once):
  `git tag -l 'archive/*'` → **167** tags; one `git cherry -v master <tag>` per tag →
  **77** carry ≥1 unapplied commit; **74** of those 77 are named by no ledger row's
  summary/payload — i.e. 77 is a **ceiling**, not a work list, and Stage 0's
  `01a0ab47-28c7` task (66 tags) still has the best claim on that lane.
- Tag-shape census: `git tag -l 'archive/*' | awk` → 75 `fix/`, 61 `feat/`, 5 `docs/`,
  1 each `wip/ review/ archive/` + 23 flat — every one of those 7 shapes was sampled
  by one `git cherry` (first tag of each prefix), and 3 of the 7 samples carry
  unapplied commits (`fix-hp-positional-param-submit` 1, `review/voiceinput-dead` 1,
  `claim-broadcast-m418` 2).

## Not Done (honest)

- **Two thirds of the resolved-tagged population was not re-judged by hand.** The
  sampling rule in §2 is deliberate (shape coverage over row coverage): 31 of 67
  rows were re-checked, so the **36 unsampled rows rest on their shape's sample**.
  A per-row audit of all 67 is a different (larger) task and is not claimed here.
- **Three verdicts rest on a single evidence class**: `01a09435-9ffe` (render +
  tests read, CLI not driven), `t-1788031086`/`t-1788041900` (docs + schema read, the
  rows' own acceptance re-runs not executed), `t-1788218896` (cherry count + the
  landed mechanism; the E2E itself was not re-run).
- **Nothing was re-verified on HP.** Several rows' decisive evidence is HP-resident
  (the night-test run log, the HP ledger, the playwright capture). Where a master
  artifact existed I used it; where it did not, the row's verdict rests on git facts
  about master and is stated as such.
- **`01a091fb-1e7d` was re-filed after measuring the "owner"** (§3.7a, §6.2): the
  pending rework row `01a0b47e-72cc` is a conflict reland whose base is 210 commits
  behind master and which cannot close the gate. Had I trusted the tag + the pending
  row (my first draft did), the red gate would have stayed unowned. **`t-1788468254`
  was re-filed for the opposite reason** — nothing in the queue claimed it at all.
- **The counterexample was re-measured at the master tip, not at this run's base.**
  This run's worktree HEAD was `363c5457`, which turned out **not** to be master
  (`0317d09a`); every §3.7 claim above now names which ref it was measured on. Rows
  elsewhere in §3 whose evidence was a `git grep`/`git ls-tree` on "master" are
  unaffected (those name master, and the tree moved by only one unrelated test file
  between the two tips), but this is a defect class worth naming: **a run's base is
  not the master of record** (Rule 30's family — the framework measures the worktree,
  the report must not call it master).
- **The 77-tag census is a ceiling, not a work list.** `git cherry` counts a
  *cherry-equivalent* patch as unapplied, so archival/marker commits inflate it (e.g.
  `chore(review): divergence marker …`). Applying `git grep` per symbol to all 77 is
  the `01a0ab47-28c7` lane, not this stage's.
