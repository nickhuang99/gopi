<!-- doc-class: normative -->
# The Runaway Loop — Measuring "Out of Control" on 2026-09-23

**Status:** CURRENT as of 2026-09-24
**Author:** PA (autonomous, under the user's delegated decision authority)
**Trigger:** the user said, before sleeping: *"you should make decision yourself and
maintain gopi project because I feel it is going to run out of control"*

This document answers that with measurements rather than an opinion, and names the
four tickets the measurements produced.

---

## 1. What "out of control" actually measures to

Four independent series, all read today:

| measurement | value | where |
|---|---|---|
| sessions created today | **79** | `find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' -newermt '2026-09-23 00:00'` |
| of those, raw log > 10 MB | **63 (80%)** | same, `-size +10M` |
| largest single session log | **253 MB** | `01a0ce03` ("gopi log regression") |
| GOPI.md size | **89,842 B** (44,824 on 09-15 — doubled in 8 days) | `wc -c < GOPI.md` |
| GOPI.md share of one request | **96.8%** (89,864 of 94,871 B) | `/tmp/request.txt` |
| prompt tokens, worst session | **25.1 M over 280 rounds** (avg 90,370/round, max 149,845) | `gopi stats ffe7d596-…` |
| today's commits | 181 total — **85 docs** (70 authored by `gopi`) vs ~20 real fix/feat/test | `git log --since` |
| worktrees held with no live worker | **3 of 6** at the time of measurement | `git worktree list` + `/proc/*/cwd` |

**The picture.** It is not a budget problem: the worst session cost ¥1.9954, of
which 53.6% was output. It is a **throughput and attention** problem. 80% of
sessions run long; the constant they carry (GOPI.md) has doubled in eight days;
long sessions produce documents instead of code; and the artefacts of long
sessions (worktrees, branches) are not collected.

## 2. The four mechanisms, each measured

### 2.1 The run ground is built and never announced

`cmd/agent/cli_tasks.go:2062` calls `tools.TaskRunGroundFor(target)` before
spawning a child: it creates a linked worktree `gopi-<task_id>` on branch
`task/<task_id>` and points `cmd.Dir` at it. Correct — and **never communicated**.

Measured on master:

```
$ grep -c -i ground GOPI.md
2                       # neither refers to the mechanism
$ grep -rn "GOPI_RUN_GROUND\|GROUND_DIR\|ground_dir" --include=*.go cmd/agent/ internal/ | grep -v _test
(no output)
```

Meanwhile `GOPI.md:189` (Rule 6a) tells the child to create a worktree for
**its** branch, and the ground's branch name (`task/<uuid>`) matches nothing in
Rule 6a's vocabulary — so the child creates a spare.

**Live proof** (one child, two refs with the *same* tip `d5495181`):

```
task/01a0cea033347737936b7167ad83aa04      d5495181    ← the ground, where the child works
fix/version-borrowed-name-reachability      d5495181    ← the spare, no holder
```

Correction recorded in the brief: the child **does** work in the ground; it also
builds an unused spare. That narrows the fix to "announce the ground and tell
Rule 6a not to build a second one".

→ **`01a0ceac-433e-7180-b3ae-2ba48fc15826`** (brief
`briefings/brief-ground-is-made-and-never-announced.md`).

### 2.2 Nothing drives the merge pass

> **Retired mechanism (merge verb removal, 2026-11-01).** the `gopi merge -h`
> invocation below is the measurement as it was taken on 2026-09-23; the
> top-level verb was removed afterwards (epic merge-verb removal), and the
> surviving trigger is `gopi task merge`. The finding this section documents —
> that nothing resident drove the pass — is unaffected by the spelling change.

Branches whose rows are `completed` sit in `refs/heads` forever. Measured:

```
$ crontab -l | grep -v '^#'     → (empty)
$ systemctl --user list-timers --all → 0 timers listed
$ ./bin/gopi merge -h | grep -i every → (no match)
```

`cmd/agent/merge_run.go:37` names the intended triggers: a human typing
`gopi merge`, `gopi merger --every D`, or the post-receive hook. The daemon is
**retired by name**; the hook **enqueues rather than merges**; so a human is the
only remaining trigger. Three completed rows were unmerged at discovery; running
`gopi merge` once, by hand, landed all three
(`3fc9b703`, `c0c973cf`, `4e414f40`).

This is the upstream cause of the 188 archive tags: branches pile up until a
human sweeps them by hand, and a hand sweep buries live work (33 branches, see
`01a0cea7`).

→ **`01a0ceb2-1e4e-76ef-9093-948521e162ff`** (brief
`briefings/brief-nothing-drives-the-merge-pass.md`). Recommended: a systemd user
timer running `gopi merge --fast` (git-only, zero LLM, stops at anything it
cannot decide), **not** a resurrection of the daemon.

### 2.3 The 60-minute budget lives only in the prompt

The rule exists ("任何子任务都不能超过一个小时") but nothing enforces it:
`TOO_BIG`'s criterion (`loop.go:584`) has **no time dimension**, so judging a
three-hour task as DOABLE is compliant. `commitTaskItem` (`loop.go:5682`) is the
one feedback point that already runs at every subtask boundary and already
returns text (`sessionBudgetLine()` precedent) — that is where an enforcement
belongs.

→ **`01a0cec2-629c-7b69-9972-23176ce53130`** (brief
`briefings/brief-sessions-run-to-250mb.md`).

### 2.4 GOPI.md doubles every eight days and is paid every round

```
2026-09-15   44,824 B
2026-09-18   64,875 B
2026-09-21   70,679 B
2026-09-23   89,842 B
```

Top six sections are 45% of the file, and most of their bytes are **incident
narrative, not rule** (Rule 27 alone is 8,426 B to say one sentence). Gate
coverage measured (`grep -rl "Rule N" --include=*_test.go`): Rule 13 → 102 test
files, Rule 1 → 48, Rule 17 → 32, Rule 27 → 29, Rule 31 → 26 — those are
gate-protected and their *text* may move to Tier B. Rule 6's family, Rule 5/7/8,
Rule 23/26/28 have **no gate** and must stay in the prompt verbatim.

Precedent already in the tree: `CONFIG.md` is 36 KB, **not injected**, referenced
by one line, and kept fresh by `internal/standards/config_doc_gate_test.go`.

→ **`01a0cec2-66a1-7820-83a2-50c95213faf3`** (brief
`briefings/brief-gopi-md-has-doubled.md`).

## 3. The loop, stated once

```
long session  (63 of 79 today > 10 MB; head 253 MB)
   → incident
   → a rule and its narrative are appended to GOPI.md     (44,824 → 89,842 B in 8 days)
   → every round of every session pays for it             (avg 90,370 prompt tokens/round)
   → rounds are slower, attention thinner
   → the session gets longer
   → the artefacts of a long session are not collected    (worktrees, branches)
   → and the diagnosis is written up instead of coded     (85 docs vs ~20 code commits)
   → more incident surface
   → (back to the top, faster)
```

**Nothing in this loop is a bug in the ordinary sense.** Each step is a
reasonable local response. The loop is the system.

## 4. What is healthy (measured, not assumed)

- `go build ./...` RC=0 on master; `git status --porcelain` empty.
- The worker loop is sound and self-limiting: `workerDefaultMaxRounds = 0`
  (unlimited rounds) but `workerDefaultIdleExits = 3` × `workerIdleIntervalDefault
  = 30s` → **a drained worker exits itself after ~90 s** ("a task-DRAIN tool, not
  a daemon").
- `ff657d596`-class sessions end correctly: `01a0ce80` finished with
  `exit_code=0, elapsed_sec=3605.5, total_iterations=314` and its own report.
- Cache hit rates are 97.8–98.3%; the money is not the constraint.
- `gopi stats`, `gopi raw`, `removalaudit`, `signalrec` all work and were used
  as the evidence sources for every claim above.
- Running `gopi merge` once by hand landed everything it was supposed to.

## 5. What a reader should take away

1. **The failure mode is not a crash.** It is a slow tax: every incident makes
   the next session more expensive, and the expense is paid in attention, which
   is what produces the next incident.
2. **Measure before fixing.** Every claim in this document came from a command;
   two of them (`01a0ceac`'s mechanism, and the "child works in the spare"
   claim inside it) were **corrected by measurement within the hour**. The
   correction is left in the brief on purpose.
3. **The four tickets are one tree.** Fixing the merge driver alone leaves
   worktrees doubling; fixing the ground alone leaves branches piling up.
4. **Do not add a rule to GOPI.md for any of this.** Two of the four mechanisms
   are caused by GOPI.md's size. The cure is elsewhere.

## Evidence index

- `briefings/brief-ground-is-made-and-never-announced.md` (with ADDENDUM + CORRECTION)
- `briefings/brief-nothing-drives-the-merge-pass.md`
- `briefings/brief-sessions-run-to-250mb.md`
- `briefings/brief-gopi-md-has-doubled.md`
- `/tmp/request.txt` — 94,871 B captured request: 496 header + 4,511 kernel + 89,864 GOPI.md
- `gopi stats --last 3` and `gopi stats ffe7d596-… --cost` outputs (in the briefs)
- `~/.gopi/sessions/01a0ce80-…/ffe7d596-…jsonl` — `session_end` record
- Commits this session: `9d1af128`, `c32a82d8`, `866bfcb6`, `5a6f5569`, `0ac3db64`
- Merge landed by hand: `3fc9b703`, `c0c973cf`, `4e414f40`
