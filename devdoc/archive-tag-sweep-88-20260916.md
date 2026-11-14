<!-- doc-class: normative -->
# Archive Tag Sweep — 88 tags, a verdict for every one

Date: 2026-09-16 (re-measured 2026-09-17)
Repo: `/home/user/work/GoPi`
Branch: `feat/archive-why-gate` (worktree `/home/user/workspace/gopi-arch-tag-sweep`)
Supersedes the coverage of: `devdoc/archive-tag-audit-20260916.md` (54 tags — that
audit stopped at "how many have commits master lacks"; this one carries a VERDICT
per tag, a RECOVER list, and the mechanism fix that stops the class recurring).

Regenerate the table with one command (it measures every row against the master tip
you hand it, because both the tag set and master move while a sweep is running):

```bash
python3 devdoc/archive-tag-sweep-build-verdicts.py > devdoc/archive-tag-sweep-88-20260916.tsv
```

---

## 0. Why this exists

An archive tag is not a tombstone, it is a **decided exclusion**:
`repoSweepArchiveExempt` makes every merge path skip the branch forever. On
2026-09-16 a completed task's deliverable (`feat/worker-run-entry`) was found
unreachable from master on exactly that account, and the first audit found more.
This sweep answers, for **every** archive tag in the repo: is there work here that
master does not have?

**51 of the 88 tags are owned by a ledger row that says the work FINISHED**
(`completed` or `verified`) while its branch is excluded from every merge path —
the "successful task, buried deliverable" population. That number, not any single
tag, is the finding of this sweep.

## 1. Result

| verdict | tags | meaning |
|---|---|---|
| CLOSE | 31 | tip is an ancestor of master — the branch landed; the tag is a spent ref |
| CLOSE-TESTDOC | 43 | not an ancestor, but only tests/docs are absent; every production path exists on master |
| REVIEW | 7 | not an ancestor, 0 files missing — a **path** match that is not a **symbol** match |
| RECOVER | 6 | an absent production file whose symbols are defined nowhere on master |
| SUPERSEDED | 1 | the capability IS on master, under a renamed surface (hand-adjudicated) |
| **total** | **88** | |

The four single-file rows this table scores have been **decided** (2026-09-18, task
`01a0ab11-7414`; §3.1): all five of their tags CLOSE, and the RECOVER count for
that group is **0 by re-measurement** — the table below is kept as measured on
2026-09-16, not rewritten. The two remaining RECOVER rows (claim-time stamp,
cwd-and-lifetime) are untouched; see §3.

## 2. Method — and the three errors the re-measurement exposed

Per tag: `git merge-base --is-ancestor <tip> master`, `git rev-list --count
master..<tip>`, `git diff --name-only <mergebase> <tip>` with `git cat-file -e
master:<path>` per path, and — for every absent production file — the symbols it
defines, greped by name across master.

**The obvious predicates do not discriminate.** "Commits ahead of master" mislabels
almost every tag: an archive tag is usually ahead simply because it branched off an
older master. "Files absent" mislabels them the same way: the tag's own base files
read as absent. The predicate that survives both is:

> an absent **production** `.go` file (not `_test.go`, not docs) **whose own
> defined symbols appear nowhere on master**.

Re-deriving every row live (rather than trusting the first pass's census) exposed
three defects in my own first pass. They are recorded because each one, left
standing, produces a confidently wrong verdict:

1. **The symbol half is "ANY symbol missing", never "all".** A file's methods are
   drawn from a small shared vocabulary (`close`, `signal`, `String`), so
   "every symbol absent" is a false-negative machine: it called
   `internal/lineedit/event.go` (a whole wake-pipe implementation) *present on
   master* on the strength of the word `close`. The rule is ≥1 missing symbol;
   the builder's docstring names the file that exposed it.
2. **The predicate is a NAME test, so it cannot see a capability master
   re-implemented under a different name.** `archive/hippocampus-host-superseded-20260910`
   scored RECOVER (7 absent symbols) and is in fact SUPERSEDED: master's
   `internal/agent/hippocampus_agent.go:174` calls `hippocampus.NewHostEngine(...)`
   (`internal/hippocampus/host.go:109`) — the same live-organ binding the tag
   introduced as `bindHippocampusOrgan`/`hippoTransport`. It is an explicit
   `ADJUDICATED` entry in the builder carrying that file:line, i.e. visible in the
   data, because an absent symbol is a question, not a verdict.
3. **The owning ledger row is the one that owns the BRANCH (`deliverable.branch`),
   chosen earliest-first.** Matching `summary LIKE %branch%` newest-first picks up
   the *recovery* task filed against that same branch name and inverts the finding:
   it reported `archive/docs/peer-push-diagnosis` as owned by `01a0aaf2` (pending)
   when its owner is `01a0aab1` (`completed`). The summary match remains only as a
   fallback for rows that predate the deliverable stamp, and it is ordered the same
   way.

A census copied from an earlier run is stale by construction — during this task
`internal/heavytest/heavy.go` landed on master and turned a RECOVER row into
CLOSE-TESTDOC, and the tempdir family landed while the sweep ran. That is why the
builder measures everything itself.

## 3. RECOVER — 6 tags with production work master lacks

| tag | tip | delta | syms absent | owning row | status | absent production file(s) |
|---|---|---|---|---|---|---|
| archive/feat/abnormal-child-retry | ca8eecea | 3 | 7 | 01a0a9b1-72f6 | completed | internal/tools/task_run_retry.go |
| archive/feat/chat-m2-status-wake | d8596127 | 2 | 14 | 01a0aa9a-84de | completed | internal/lineedit/notify.go |
| archive/feat/chat-m2-streaming | 900c0a05 | 20 | 3 | 01a0a97e-e402 | completed | internal/lineedit/event.go |
| archive/feat/claim-time-deliverable-stamp | a80866e9 | 1 | 1 | 01a0a9b1-6dc6 | failed | cmd/agent/task_claim_ground.go |
| archive/fix/cwd-and-lifetime | b37b776f | 7 | 37 | 01a0a211-f9f5 | failed | internal/procname/identity.go, internal/procname/liveness.go, internal/tools/daemon_lifetime.go, internal/tools/process_cwd_release.go |
| archive/fix/task-child-fail-escape-sandbox | b77d53dc | 12 | 9 | 01a0a940-bff2 | completed | cmd/agent/task_child_fail_guard.go |

Filed as three follow-up tasks (a recovery is a FRESH branch off master — Rule 23,
never work on the archived branch): `01a0ab00-1950` (claim-time ground stamp),
`01a0ab00-28b2` (procname liveness / daemon-orphan sweep / cwd-release, 37 symbols),
`01a0ab00-4479` (the remaining single-file items — `task_run_retry`,
`task_child_fail_guard`, and the two `lineedit` files).

**The `01a0ab00-4479` group is no longer a follow-up — it is DECIDED (2026-09-18,
task `01a0ab11-7414`, branch `feat/archive-recover-4items`): all four of its tags
CLOSE, no re-land.** The other two follow-ups (`01a0ab00-1950`, `01a0ab00-28b2`)
and their READY rows remain UNTOUCHED — the five rows this paragraph named are not
the whole RECOVER list, and only the 4479 group carries a decision here. See §3.1
for the per-tag verdicts and their master `file:line` proofs.

### 3.1 The four single-file items — DECIDED (2026-09-18, task `01a0ab11-7414`)

The `01a0ab00-4479` group (plus the `heavy-tests-classification` row from §4) was
re-measured against master `9ce8e159`. **Every one of the five tags closed: master
already carries the work.** Four of the five named files are not merely present on
master, they are byte-identical to the tag's copy, and the fifth is a **rename**.
Three of the rows were a RACE — the work landed on master after this sweep measured
the tag (the mechanism §2 already documents for `heavy.go`); two are decided because
master is the SUPERSET of the archived tip. **No item needed a fresh re-land**; the
proofs below are master `file:line`.

| tag | tip | decision | master proof |
|---|---|---|---|
| `archive/feat/abnormal-child-retry` | `ca8eecea` | **CLOSE** — raced | `internal/tools/task_run_retry.go` byte-identical to the tag's; defined by master's own `32af5af3` (ancestor of master). All 6 symbols there: `TaskRunRetryCapReason:69`, `TaskRowStatusAt:77`, `TaskRowTerminalAt:110`, `TaskRunRetryCounts:122`, `taskRunRetryCountsFromTags:132`, `TaskRunRetryNote:159`; caller `cmd/agent/cli_tasks.go:1968` → `:2088` |
| `archive/fix/task-child-fail-escape-sandbox` | `b77d53dc` | **CLOSE** — raced, master is later | `cmd/agent/task_child_fail_guard.go` byte-identical to the tag's (landed as master's `6242c514`); master's *tests* are newer — `task_child_fail_sandbox_guard_e2e_test.go` uses `worktreeroots.FixtureBannedRoot` and `tools.LedgerReadOnlyDSN(dbPath)` where the tag seeded a `/tmp` literal and `dbPath+"?mode=ro"`, and `task_child_fail_guard_test.go:115` dropped `mergerGracefulExitCode` (retired with the resident merger — `cmd/agent/cli_tasks_worker_stop.go:44`) |
| `archive/fix/heavy-tests-classification` | `5a159274` | **CLOSE** — master is the superset (this also resolves its §4 REVIEW row) | `internal/heavytest/heavy.go` present on master; `func` set identical to the tag's, exported-const set differs only by master's ADDED `SkipMarkerPrefix` (`internal/heavytest/heavy.go:59`) and master's body carries the added `WorkRefusal()` boundary refusal (`:123-136`). The one unmerged patch-id (`git cherry master 5a159274` → `+ 243b06e0`) is an EARLIER version of the same mechanism (`heavy.go` lacks both hunks above) |
| `archive/feat/chat-m2-status-wake` | `d8596127` | **CLOSE** — landed, master is the superset | `internal/lineedit/notify.go` present (landed via master's `ff9927a0`); the tag's file lacks master's `funcNotifier` (`:70-78`). The tag is an ancestor of master |
| `archive/feat/chat-m2-streaming` | `900c0a05` | **CLOSE** — **rename**, not a loss | the "absent" `internal/lineedit/event.go` is master's `internal/lineedit/notify.go` (`wakePipe:39`, `newWakePipe:91`, `signal:109`, `drain:121`, `close:135`, `setNotificationPipe:147`). Code-only diff of `git show 900c0a05:internal/lineedit/event.go` vs master's file: master adds `wakePipeBytes`, `drain`, the three notifier sinks and `%w` error wrapping; the archived file has no code master lacks. The tag itself is an ancestor of master |

**Two of the five tag NAMES no longer exist** (measured 2026-09-18, master
`9ce8e159`): `git rev-parse --verify refs/tags/archive/feat/abnormal-child-retry`
and `…/archive/feat/chat-m2-streaming` both fail, while the other three resolve.
The archived *set* itself moved after this sweep — 161 `archive/*` tags exist now
against the 88 measured here, and every tag this doc names has a `-20260917`
sibling. Their **tips** are still reachable and were measured directly:
`ca8eecea` via `backup/feat/abnormal-child-retry-mergetip` (which contains it), and
`900c0a05` — an ANCESTOR of master, which is itself the proof that item D2 landed.
No tag was created, moved or deleted by the deciding task (Rule 23 / Rule 9: an
archive tag is never deleted).

The remaining two RECOVER rows (claim-time ground stamp, cwd-and-lifetime) are the
subject of the two other follow-ups named above and are untouched by this task.

The smallest sample was validated by hand end to end: 1 commit, 1 symbol —
`cmd/agent/task_claim_ground.go` defines `taskClaimGroundStamp`, that symbol is
defined nowhere on master, master's `cmd/agent/cli_tasks.go` has no claim-time
caller, and master's `internal/tools/task_deliverable_worktree.go` still offers only
the creation-time/completion-time writers. The gap the archived item closes is open.

## 4. REVIEW — 7 tags where a path match is not a symbol match

| tag | tip | delta | owning row | status | why it is not resolved |
|---|---|---|---|---|---|
| archive/fix-hp-positional-param-submit-20260907 | 54c3ba25 | 1 | - | - | 0 files missing, but `git cherry` marks its patch + (master lacks it) — the two axes disagree |
| archive/fix/hp-positional-param-submit | 54c3ba25 | 1 | t-1788790964 | completed | same commit as the row above |
| archive/fix/heavy-tests-classification | 5a159274 | 4 | 01a0a88e-4a59 | completed | 0 files missing; `internal/heavytest/heavy.go` landed on master during this task — 1 of its 4 patch-ids is still unmerged. **DECIDED 2026-09-18 (§3.1): CLOSE — master is the superset of the archived tip; the unmerged patch-id is an earlier version of the same mechanism** |
| archive/fix/task-list-address-no-scan-rework-20260915 | a0dc51a2 | 1 | - | - | 0 files missing |
| archive/hp-zombie-closeout-20260902 | 74113d41 | 2 | - | - | 0 files missing |
| archive/single-row-verify-20260905 | 80c08198 | 1 | - | - | **its 3-dot diff against master is EMPTY** — it differs by nothing; CLOSE-class in substance, kept named rather than silently folded in |
| archive/testsuite-codex-20260827 | 164ac04f | 1 | - | - | 3 absent production files whose symbols are ALL present on master (`expandHome` is defined at master:internal/tools/read.go) — the file-path predicate's weakest case |

`archive/testsuite-codex-20260827` is the one the first pass called RECOVER. It is
not: master carries the same content under the same paths. Nothing was re-landed
from any REVIEW row.

The disagreement matters because it is systematic: a path-level census will call a
tag "already on master" whenever master has *a file at that path*, and the patch-id
axis will call the same tag "not merged" whenever master's copy arrives by a
different commit. Neither alone is the answer; where they disagree the tag is named
REVIEW instead of rounded.

## 5. The former UNKNOWN row — decided, and its deliverable RECOVERED

`archive/docs/peer-push-diagnosis` (tip `dcb35012`) was reported UNKNOWN by the first
pass because the tag arrived after `audit.tsv` was measured. It is now decided and
its content restored:

- Census: 3 commits beyond master, master lacks exactly one path —
  `devdoc/peer-push-diagnosis-20260916.md` (466 lines) → **CLOSE-TESTDOC**, owner row
  `01a0aab1-587a-7ca3-ba98-4b66b11b8b90` = **completed** → buried.
- Master has a *sibling* diagnosis, not this one: `devdoc/hp-push-topology-audit.md`
  (the `sync_peers` finding) is present, while `git grep 'chain skip' master` is
  EMPTY and the absent doc's §4 fix prescription is verbatim the payload of the
  still-**pending** task `01a0aaef` ("client close-out: deliver the branch
  independently of worktree cleanup …"). Master thus lacks the document the open
  repair task's own evidence chain points at.
- **Recovered**: `devdoc/peer-push-diagnosis-20260916.md` is now on this branch,
  copied from the archived tag (`git show archive/docs/peer-push-diagnosis:devdoc/peer-push-diagnosis-20260916.md`).
  Rule 23: a fresh branch, the archived branch is never worked on.
- The archive record `01a0aaf2` ("resolve merge conflict for docs/peer-push-diagnosis …
  parked at archive/docs/peer-push-diagnosis") is independent evidence for §6: the
  conflict-resolution path is a SECOND writer that parks a branch under `archive/`
  with no reason recorded.

## 6. The mechanism that produced this class — and its fix

51 tags carry a ledger row that says the work FINISHED, and 49 of them render as
BURIED. The write side had no bookkeeping at all: the tag recorded no reason,
consulted no ledger, filed no remedy and appeared in no report. See
`devdoc/archive-why-gate.md` for the fix landed on this branch (annotated tag
carrying the reason + census; refusal when the owner row says finished; the archived
set rendered by `merger --status`, buried rows shouted).
