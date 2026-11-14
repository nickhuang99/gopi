<!-- doc-class: historical -->
# Archive Tag Audit — 54 tags swept, misfiled work identified

Date: 2026-09-16
Repo: `/home/user/work/GoPi`
Sweeper: PA (read-only; no branch touched, no tag deleted)

---

## 0. Why this audit exists

A completed piece of work (`feat/worker-run-entry`, task `01a0a9bc`, **status
completed**) was found to be unreachable from `master`. Its branch carried an
`archive/` tag, and `repoSweepArchiveExempt` treats an archive-tagged branch as
a **DECIDED LOSER** — never merged, never deleted, never reported. It simply
vanishes from the merger's candidate set (`gopi merger --status` → `pending: none`).

Nick asked: sweep the whole archive set and find out how much else was misfiled.

---

## 1. Method (four filters, each narrowing)

| # | Filter | Command | Why |
|---|---|---|---|
| 1 | Has independent commits | `git rev-list --count master..<tag>` | a tag sitting on master's tip carries nothing |
| 2 | **Pure delta** vs merge-base | `git diff <merge-base> <tag>` | `git diff master...<tag>` includes master's forward motion as spurious "deletions" — the merge-base diff is the branch's actual contribution |
| 3 | File absent from master | `git cat-file -e master:<path>` | a file present in master may still be a *different* version; absence is unambiguous |
| 4 | **Not already re-expressed** | function-level / package-level cross-check | the killer filter — see §4 |

Filter 4 is what separates real misfiles from legitimate archives. Two examples
where filter 3 fired but the work *was* superseded:

- `archive/fix/cwd-and-lifetime` contributes `internal/procname/{identity,liveness}.go`
  and `internal/tools/{daemon_lifetime,process_cwd_release}.go`. Master lacks
  those *files* — but master has the *functionality* under
  `internal/worktreerelease/` + `internal/testutil/daemonlifetime*`. The branch
  was an earlier organisation of the same feature. Its task is also **failed**.
- `archive/tool-family-merge-superseded-20260910` — self-declared superseded in
  its own tag name.

---

## 2. Sweep result — 54 tags, 37 with a delta, 34 with files absent from master

Sorted by *substantive* content (source + test files absent from master).

| Tag | delta | absent | src | test | doc | other |
|---|---|---|---|---|---|---|
| `feat/cov-tools-rest` | 23 | 16 | 0 | **9** | 7 | 0 |
| `fix/notify-all-events` | 21 | 10 | 0 | **6** | 4 | 0 |
| `fix/cwd-and-lifetime` | 27 | 10 | **4** | **5** | 1 | 0 |
| `testsuite-codex-20260827` | 25 | 8 | **3** | **5** | 0 | 0 |
| `feat/web-m2-b5b8b9b12` | 14 | 7 | 0 | 2 | 2 | **3** |
| `log-stats-raw-gopi` | 11 | 4 | 0 | 1 | 1 | 2 |
| `fix/worker-ownership-slash-pair` | 5 | 4 | 0 | 3 | 1 | 0 |
| `feat/taskid-whitespace-normalize` | 14 | 4 | 0 | 2 | 2 | 0 |
| `feat/raw-round-grammar` | 11 | 4 | 0 | 3 | 1 | 0 |
| `feat/cov-hippocampus-hp` | 5 | 4 | 0 | 3 | 1 | 0 |
| `fix/hippo-host-resolution` | 8 | 3 | 0 | 2 | 1 | 0 |
| `feat/web-m1-b4b7` | 10 | 3 | 0 | 1 | 1 | 1 |
| `feat/cov-deepseek-hp` | 4 | 3 | 0 | 3 | 0 | 0 |
| *(20 further tags with 1–2 absent files each — mostly a single devdoc)* | | | | | | |

---

## 3. THE DECISIVE SIGNAL: task status contradicts the archive

Every archive-tagged branch should correspond to a task row. Cross-referencing
the ledger against the tag set produced the smoking gun:

| Task status | Count | Meaning |
|---|---|---|
| **completed** | **21** | work finished — yet the deliverable is buried |
| **verified** | **1** | verified pass — yet buried |
| failed | 3 | legitimately archived (a loser *was* decided) |
| completed+cancelled | 1 | mixed |

**22 of 29 tag-associated tasks say the work succeeded, but the branch is
tagged DECIDED LOSER.** Only 3 tasks are `failed` — those are the archives that
match their tag's semantics.

This is not a one-off misfiling. It is the mechanism.

---

## 4. Confirmed misfiles (work done, ledger says done, master does not have it)

### 4.1 `feat/web-m2-b5b8b9b12` — 4570 lines — **worst case** (task: completed)

```
task     : 01a0a97e  status=completed  reason="task finished"
deliverable: {branch: feat/web-m2-b5b8b9b12, repo: /home/user/work/GoPi,
              worktree: /home/user/workspace/gopi-web-m2}
archived : 2026-09-16 19:15
```

| | master | branch |
|---|---|---|
| `cmd/agent/cli_web.go` | 1637 lines | **2533 lines** (+1273) |
| tests | `cli_web_m1_test.go` only | +`cli_web_m2_test.go` (565) +`cli_web_m2b_test.go` (538) |
| browser harness | none | `cli_web_page_harness.js` (312) + `cli_web_page_scenarios.js` (267) |
| devdocs | roadmap only | +`web-m1-b4b7.md` (392) +`m2-web-m2.md` (178) +`web-m1-b1b2b3.md` (207) |

Functions the branch adds that master does **not** contain:
`capMenuList`, `detectFileMenu`, `detectSlashMenu`, `transcriptTools`, `webTurnTimeout`.

**Status: RECOVERY DISPATCHED** — task `01a0aa9d`, brief
`briefings/brief-web-m2-rework.md` (commit `cb760920`). Rebuild off master, not merge.

### 4.2 The HP coverage campaign — 5 tags, all completed

```
feat/cov-tools-rest       9 × internal/tools/covtools_*_test.go   (task: completed)
feat/cov-shot-hp          internal/shot/cov_hp_test.go
feat/cov-deepseek-hp      internal/deepseek/cov_deepseek_guard_test.go
feat/cov-gopilog-hp       internal/gopilog/…                      (task: completed)
feat/cov-hippocampus-hp   internal/hippocampus/…                  (task: completed)
fix/notify-all-events     internal/tools/tasknotify_alarms_test.go + 5 covtools
```

These are test files only — **zero production code**. They add coverage for
functions that master still has uncovered. Lowest risk, clear value.

### 4.3 `testsuite-codex-20260827` — codex's testsuite (oldest, 2026-08-27)

```
absent: internal/taskitem/taskitem.go        ← an ENTIRE package master lacks
        internal/agent/env.go
        internal/tools/path.go
        + 5 test files
```

Oldest archive. `internal/taskitem` does not exist on master at all — needs a
diagnosis of whether that design survived under a different name before any
recovery.

### 4.4 `fix/cwd-and-lifetime` — task **failed** (likely legitimate)

Its functionality is re-expressed in master under `internal/worktreerelease/` +
`internal/testutil/daemonlifetime*`. Include in the audit, expect "superseded".

---

## 5. Root cause — the archive tag is a one-way door with no bookkeeping

Four independent faults, all in the archive path:

1. **No reason recorded.** `git tag archive/<branch>` stores a commit, nothing
   else. A reader cannot tell *why* — was it a conflict loser? a failed verify?
   superseded? a hand mistake? All four look identical.
2. **No task-status check.** The archiver (whoever ran it) never consulted the
   ledger. 22 tags correspond to `completed`/`verified` tasks and were archived
   anyway.
3. **No remedy filed.** Compare with the merger's *conflict* lane, which files a
   `branch-rework` task. The archive lane files **nothing** — so a misfile
   produces no signal anywhere.
4. **Permanent and unreported.** `repoSweepArchiveExempt` hides the branch from
   the merger forever, and no report lists archive-tagged branches. It is
   invisible in `merger --status` (`pending: none`) and invisible in `task list`
   (the task rows say completed).

The combination means: **a completed deliverable can be archived by mistake and
nothing in the system will ever mention it again.**

---

## 6. Recommended dispositions (ranked)

| # | Scope | Action | Risk |
|---|---|---|---|
| P1 | `feat/web-m2-b5b8b9b12` | **rework off master** | dispatched (`01a0aa9d`) |
| P2 | 5 × cov tags + `fix/notify-all-events` | rework off master (tests only, no prod code) | low |
| P3 | `testsuite-codex-20260827` | **diagnose first** (`internal/taskitem` may be dead) | unknown |
| P4 | `fix/cwd-and-lifetime`, 3 × `failed`-task tags | diagnose → likely "superseded", close | low |
| P5 | 20 × single-devdoc tags | batch review: recover the doc or close it | low |
| **M** | **the mechanism** | archive must record a reason + refuse/warn when the task is not failed | — |

---

## 7. The mechanism fix (independent of any recovery)

Minimum viable gating, in the archive path:

- **Record the reason** — an annotated tag (`git tag -a -m "<reason>"`) or a
  ledger row, so a future reader can distinguish the four cases in §5.
- **Refuse to archive when the task is `completed`/`verified`** unless an
  explicit override names why (mirrors the merger's conflict lane, which files a
  rework task instead of silently dropping work).
- **Report archive-tagged branches** in `merger --status` (e.g.
  `archived: N [branch — task — reason]`), so the set is visible rather than
  invisible.

Without this, today's sweep will simply need repeating next week.

---

## 8. Reproduction

```bash
# filter 1+2+3: pure delta, files absent from master
for t in $(git tag -l 'archive/*'); do
  c=$(git rev-parse "$t^{commit}"); mb=$(git merge-base master "$c")
  for f in $(git diff --name-only "$mb" "$c"); do
    git cat-file -e "master:$f" 2>/dev/null || echo "ABSENT ${t#archive/} $f"
  done
done

# the decisive signal: task status per archive tag
for t in $(git tag -l 'archive/*' | sed 's|^archive/||'); do
  sqlite3 ~/.gopi/memory.db \
    "SELECT '$t', status FROM tasks WHERE deliverable LIKE '%$t%' OR summary LIKE '%$t%';"
done
```

Raw scan output: `/tmp/arch-scan.txt` (per-tag delta/absent counts),
`/tmp/arch-full.txt` (with src/test/doc/other breakdown).
