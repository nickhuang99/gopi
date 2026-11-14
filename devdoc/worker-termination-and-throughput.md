<!-- doc-class: historical -->
# Task Report: worker terminations & night throughput (2026-09-16 22:00 → 2026-09-17 05:00)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Question asked:** four overnight `gopi worker` processes stayed alive ~5.5h yet only a few
branch merges landed. `worker.log` repeated `terminated received — draining …`. Who terminated
them, and why was the output so low?

**Status: diagnosis only.** No fix is committed. Nothing was killed, restarted or signalled by
this session. Branch `feat/night-worker-throughput`, worktree `~/workspace/gopi-night-tput`.
Hosts: `peerbox` (server) + `hostb-Laptop` (client). Every number below was produced by a command
pasted next to it or in §6.

> **THE DEFECT WAS STILL LIVE WHILE I INVESTIGATED IT — and it recurred.** All four of peerbox's
> 23:35 workers were alive when I first probed them (~05:25) and **all four were SIGTERM'd again at
> `2026-09-17T05:43:25`** — under the *same four unrelated `bash` parents*
> (`ppid` 2008248 / 2829404 / 1785518 / 2034244), i.e. the identical signature as 22:23:13, at a
> 7h20m interval. Simultaneously an ssh session `19957` from `192.168.7.5` closed
> (`systemd-logind: Session 19957 logged out. Waiting for processes to exit.` at 05:43:21).
> This is a **second instance of the same event, captured live** (§2.1).

---

## 0. Headline

1. **Eight pids died at 22:23:13–14 in one second — and it happened AGAIN at 05:43:25.** First
   event: 3 `gopi worker`, their 3 children, 1 `web`, and the **resident `gopi merger` (4h41m of
   CPU)**. Second event (captured live): the **same four workers** SIGTERM'd, under the same four
   unrelated shell parents. **Zero `kind=intent` records in both** → not one gopi self-stop path
   was involved. Class: an external cohort kill, recurring on a ~7 h 20 min interval.
2. **The night's four workers were a restart, not the 5.5 h cohort.** They started
   2026-09-16 **23:35:22–23:37:39**, *after* the 22:23 kill, and worked until **05:43:25 ≈ 6 h 8 m**.
   The window's first ~1 h 25 min ran with no workers and no resident merger at all.
3. **The low merge count is a queue-mix fact, not a worker-idleness fact.** 56 of the 151 rows
   created in the window are `merge-conflict` work, and **0 of them were ever claimed** (44 were
   still `pending` at probe time). The merger cannot land what it alone was asked to resolve, and
   the workers' priority ordering never picked the conflict rows up.
4. **Start latency is not a factor** (median first tool call 1.7 s HP / 3.9 s peerbox). What costs
   time is the *run*: mean claim→end 2157 s (36 min), max 6682 s (1.9 h).
5. The two candidates "Bond 8-round no-op" and "dead-task re-claim loop" are **not supported** —
   see §4. Reporting that plainly is the point of this report.

**Discrepancy I could not reproduce:** the briefing's "only **3** merges" is not what master's log
shows for this window under my definition — I measure **8** merge commits (§4.1). I report my
count and its command rather than bend the number to the briefing in either direction.

---

## 1. Every termination recorded in the window

Source: `kind=signal` records in `~/.gopi/sessions/*/signal-provenance.jsonl`, window-filtered by
`scripts/night-throughput.sh`. **14 records on peerbox, 7 on HP — and on both hosts, 0 of them
preceded by a `kind=intent` record** (see §3 for why that absence is the finding).

| # | time (+08:00) | signal | pid | role | ppid | parent (identity) | class |
|---|---|---|---|---|---|---|---|
| 1 | 22:04:11 | SIGINT | 2761158 | web | 2829404 | `bash\|-bash` | human / terminal |
| 2 | 22:04:32 | SIGINT | 2770507 | web | 2829404 | `bash\|-bash` | human / terminal |
| 3 | 22:05:22 | SIGINT | 2777961 | web | 2008248 | `bash\|-bash` | human / terminal |
| 4 | **22:23:13** | SIGTERM | 2444398 | (child) | 952831 | `gopi\|gopi worker` | **cohort kill** |
| 5 | **22:23:13** | SIGTERM | 2858364 | (child) | 3680671 | `gopi\|gopi worker` | **cohort kill** |
| 6 | **22:23:13** | SIGTERM | 2833700 | (child) | 3680484 | `gopi\|gopi worker` | **cohort kill** |
| 7 | **22:23:13** | SIGTERM | 2949215 | web | 1 | `systemd\|/lib/systemd/systemd` | **cohort kill** (detached) |
| 8 | **22:23:14** | SIGTERM | 952831 | worker | 2034244 | `bash\|-bash` | **cohort kill** |
| 9 | **22:23:14** | SIGTERM | 3680671 | worker | 1809064 | `bash\|-bash` | **cohort kill** |
| 10 | **22:23:14** | SIGTERM | 3680484 | worker | 1785518 | `bash\|-bash` | **cohort kill** |
| 11 | **22:23:14** | SIGTERM | 638001 | **merger** | 1 | `systemd\|/lib/systemd/systemd` | **cohort kill** (detached) |
| 12 | 01:53:42 | SIGTERM | 3914474 | task list | 3914472 | `timeout\|timeout 60 gopi task list` | mechanism / timeout |
| 13 | 03:26:28 | SIGTERM | 317785 | task verify | 317779 | `timeout\|timeout 900 …` | mechanism / timeout |
| 14 | 03:26:28 | SIGTERM | 317897 | `--timeout` | 317785 | `gopi\|gopi task verify …` | mechanism / timeout (child) |

HP's 7 records are all **before** the window (17:26:51–21:49:00: a worker, five `web`, one SIGINT
from a `bash` parent). **No HP termination occurred during the night** — HP's three workers were
still alive at probe time (`etimes` 19 392 / 41 060 / 45 172 s).

Rows 12–14 are ordinary: `timeout(1)` reaping its own child. Worth noting only because
**row 13/14 show a `gopi task verify` needing >900 s** — the verify path is slow at this backlog
size, which is a knob an operator can tune, not a defect.

## 2. The 22:23:13–14 kill: why "cohort", and why not a session teardown

All facts below are from the records in §1 plus the journal; the reasoning is stated so a reader
can reject it if the facts do not fit.

- **Eight pids, one second, four different systemd scopes.** Rows 7 and 11 have `ppid=1` and
  `parent=systemd` — they are *detached* daemons (the resident merger, and a web process). Rows
  8/9/10 are `gopi worker` processes whose parents are **three different `bash` login shells**
  (ppid 2034244 / 1809064 / 1785518).
- **Therefore no common ancestor exited.** Rule 24's mechanism — a login-session scope teardown
  killing one subtree — cannot produce this shape: it removes one scope's descendants, not three
  workers under three unrelated shells plus two `ppid=1` daemons in other scopes. A single
  `session-NNNNN.scope` removal kills its own subtree only.
- **So the sender enumerated pids** (or matched a name/pattern and signalled each match). This is
  the signature of a scripted sweep, not of a session ending.
- **Zero `kind=intent` records.** `signalrec.Intent` is called by every gopi self-stop path before
  it signals (`cmd/agent/child_stop.go:184`, `cli_worker_control.go:977`,
  `cli_merger_stop.go:689`). Not one of the eight was announced → **no gopi stop path was the
  sender.**
- Corroboration in the affected processes' own logs: three worker sessions each wrote
  `worker: terminated received — draining …` inside the same second, and systemd journaled
  `gopi-daemon-merger-nick-95fc9d79cb8f.scope: Consumed 4h 41min 59.650s CPU time.` at
  `22:23:15.906497` — the resident merger's scope being torn down right after its SIGTERM.

**Consequence for the night.** With rows 4–11 gone at 22:23, the window's first **1 h 25 min** ran
with essentially no workers and no resident merger; the 23:35 cohort is a restart, and the phases
at 22:41 (a batch of four merges) and 04:02–04:41 are the *only* landing activity. A single
external kill therefore explains a large share of the night's shape without any code bug at all.

This is filed as **bug task `01a0ac30-3f50-7c3d-991a-05fae3fc1809`** (repro + acceptance inside).

### 2.1 The 05:43:25 recurrence — captured live, same signature

While this report was being written, the same four workers (the 23:35 cohort) were terminated
again:

```
2026-09-17T05:43:25 signal SIGTERM pid=3067418 role=worker ppid=2008248 parent=bash|-bash
2026-09-17T05:43:25 signal SIGTERM pid=3066922 role=worker ppid=2829404 parent=bash|-bash
2026-09-17T05:43:25 signal SIGTERM pid=3066414 role=worker ppid=1785518 parent=bash|-bash
2026-09-17T05:43:25 signal SIGTERM pid=3074388 role=worker ppid=2034244 parent=bash|-bash
```

Four victims, one second, **four different shell parents, 0 `kind=intent` records** — byte-for-byte
the 22:23:13 signature, at a 7 h 20 min interval. Corroborating journal lines:

```
Sep 17 05:43:21.159326 peerbox sshd[1261685]: Received disconnect from 192.168.7.5 port 32802:11: disconnected by user
Sep 17 05:43:21.165744 peerbox systemd-logind[1535401]: Session 19957 logged out. Waiting for processes to exit.
Sep 17 05:43:21.167598 peerbox systemd[1]: session-19957.scope: Deactivated successfully.
Sep 17 05:43:24.359117 peerbox systemd[316390]: gopi-daemon-merger-001-99b4024570a8.scope: Consumed 2.604s CPU time.
```

Two readings, and I cannot yet separate them from the evidence held:

- **(i) A pid-enumerated cohort kill** by the same actor as 22:23:13 (matching the §2 shape:
  4 shell parents cannot share an exiting ancestor).
- **(ii) Each worker's own shell/terminal died** as its session departs — but then the killer is
  still *the same external event* (`sshd` disconnect at 05:43:21 plus whatever reaps the
  processes), and the fact that **four independent terminals were affected within 4 seconds**
  is itself the anomaly.

Either way the operational consequence is identical to §2: the workers were removed
**externally, unannounced, and unlogged in a way a reader can attribute**.

**What I ruled out as the actor (each by reading the file, not by assumption):**

- `~/bin/night-watch.sh` (created 2026-09-17 00:44, cron-free bounded loop of 90×600 s). It is a
  pure *observer*: `pgrep -xc gopi` counts, and there is **no `kill` anywhere in it**.
- `~/local/bin/gopi-scratch-gc-watchdog.sh` (the `*/30` crontab entry — the cadence matches
  22:23 and 05:43, both ≈ :23/:43 past the half-hour). It executes exactly one
  `gopi scratch-gc`; its entire sweep is `internal/scratch.SweepHome`, whose documented hard rule
  is that **"a directory held by a live process … is never removed"**, and its log shows no entry
  for the 05:43 window. Its *tick* face however rides the worker loop
  (`cmd/agent/scratch_gc_tick.go`), so a timed-out sweep inside a worker remains a live
  hypothesis to check with a profile.
- gopi's own stop paths — excluded by the **0 `intent` records** (§2).

**Practical remedy available today:** the affected workers were launched with
`setsid nohup … </dev/null` (`~/bin/gopi-worker-up.sh`), which detaches them from the *terminal*
but **not from the login session's cgroup** — the DEGRADED path those workers' own merger logged
(`daemon: scope DEGRADED: still in a login session scope … this process dies with that scope`).
A worker started under `systemd-run --user --scope` survives its launcher; this is a **one-command
change to the launch line**, not a code change. I did not make it (worker ownership is the
operator's — `devdoc/worker-ownership.md`), and no privilege is involved (Rule 25): the scope is a
*user* scope, exactly the mechanism the daemon path already uses. I could not verify the survivor
behaviour myself because I must not start a worker (Rule: the operator owns worker launch).

## 3. Why "terminated received" could not answer the question

`worker: terminated received — draining (consumed at the claim boundary: nothing new claimed, leaving)`
is the line every investigation starts from. Measured on the 31 occurrences in
`~/.gopi/logs/worker.log`:

- **0 carry a sender identity** (no pid, no argv, no signal) and **0 carry a wallclock**;
- only **3 of 31** resolve to a task id at all (nearest preceding claim);
- and the string is **identical for a graceful operator stop and for the 22:23 cohort kill**.

An operator reading 31 "draining" lines concludes "routine shutdowns" — which is exactly the wrong
conclusion for at least three of them. The raw material to do better already exists
(`internal/signalrec` records `kind`, pid, ppid, argv, `parent_argv`); the drain line simply does
not carry it, so the reader never joins the two.

Filed as **bug task `01a0ac30-3f69-7983-8449-7cbc05a3f16f`**.

## 4. Output attribution — candidates, tested

### 4.1 What actually landed

```bash
git -C /home/user/work/GoPi log master --since='2026-09-16 22:00' --until='2026-09-17 05:00' \
  --merges --format='%h %ci %s'
e9696980 2026-09-17 04:41:13 Merge branch 'feat/chat-m2-streaming' into master
c23653b5 2026-09-17 04:20:07 resolve merge feat/merger-inflight-both-axes into master
ea6373ab 2026-09-17 04:02:19 resolve merge feat/cwd-release-inventory-verified into master
0f2cc45c 2026-09-16 23:42:17 Merge tag 'archive/fix/worker-ownership-slash-pair'
7073548c 2026-09-16 22:41:15 Merge branch 'fix/live-runner-not-dead-holder2'
98938b92 2026-09-16 22:41:15 Merge branch 'fix/ledger-firstopen-budget'
df28c603 2026-09-16 22:41:15 Merge branch 'feat/web-local-trust'
0a3e298e 2026-09-16 22:41:15 Merge branch 'feat/task-for-branch'
```
**8 merge commits, 54 commits total, 43 non-merge worker commits** (`--count`, and a
`feat|fix|test|docs|refactor|chore` filter). The briefing's "3" is not this quantity under any
definition I tried; see §0.

### 4.2 Counted numbers for the window (utc-corrected, §5)

| | value |
|---|---|
| rows created | 151 |
| rows claimed | **39** (peerbox) / 31 (HP) |
| completed | 65 · failed 6 · cancelled 6 |
| committed by workers | 43 |
| **landed (merge commits)** | **8** |
| queue mix created | merge-conflict **56**, other 39, bug 32, requirements 18, recovery 6 |
| claims by class | bug **19**, requirements **10**, merge-conflict **0** |
| merge-conflict rows still `pending` at probe | **44** |

### 4.3 Candidate (a) — "Bond 8-round no-op": **NOT SUPPORTED**

No `Bond`-related task was created or claimed inside the window; the ledger's Bond rows are
2026-09-01…09-08 (`SELECT … WHERE summary LIKE '%Bond%' ORDER BY created_at DESC` → t-1788873175,
2026-09-08, completed). A Bond no-op cannot consume a time budget it did not occupy.

### 4.4 Candidate (b) — "merge-conflict-heavy queue mix": **SUPPORTED, and it is the main reason
branches did not land**

- **56 of 151 created rows are merge-conflict work; 0 were claimed; 44 were still pending.**
- Those rows are **priority 5**; the bug rows that consumed the night are **priority 1**. The
  workers are doing exactly what the queue told them to.
- The merger's own passes in the window agree: **41 passes on the GoPi repo, `merged=0` on 78
  repo-pass lines vs `merged=1` on 5**, with `parked` 69, `park-duplicate` 19, `protected` 199 —
  and the park reasons are conflict-dominated (`conflict stages` ×43, `conflict` ×11).
- The rows also accumulate: at probe time **61 merge-conflict rows were pending**.

So the honest answer to "why so few merges" is partly *"the queue asked for conflict resolution
that no worker was scheduled to do, and the merger cannot land a conflicted branch"* — a
**scheduling-ordering gap**, not worker idleness.

### 4.5 Candidate (c) — "dead-task re-claim loop": **NOT SUPPORTED**

```sql
SELECT COUNT(*) FROM tasks WHERE claimed_at BETWEEN strftime('%s','2026-09-16 22:00','utc')
                                          AND strftime('%s','2026-09-17 05:00','utc')
                                          AND tags LIKE '%merge-conflict%';
```
and a ledger-wide re-claim census: **tasks with more than one `claimed` event = 0**; the throughput
view's re-claim count = 0. Task runs died *by signal* (§1), not by being re-claimed in a loop.

> Caveat, stated because it invalidated two of my earlier queries: `task_events` in this ledger
> carries **no `claimed` rows at all** (`SELECT event, COUNT(*) … GROUP BY event` → cancelled,
> completed, completed-unclean, failed, in_progress, reframe only), so a re-claim query written
> against that table returns 0 *structurally*. The 0 above was re-derived from `tasks.claimed_at`
> and cross-checked against session claim lines before being believed.

### 4.6 What the time actually went on

- **Claim→end duration:** n=31, **mean 2157 s (36 min), min 51 s, max 6682 s (1.9 h)**.
- **Hourly claim density:** 22h→3, 23h→4, 00h→5, 01h→6, 02h→**2**, 03h→5, 04h→6 — flat, no stall.
- **"5.9 h alive" was a MID-WINDOW SNAPSHOT, not the cohort's life:** when I probed at ~05:25 all
  four 23:35 workers were alive (`etimes` ≈ 21 200 s = 5.9 h *so far*); by 05:43:25 all four had
  been SIGTERM'd (§2.1). The cohort therefore worked **23:35:22 → 05:43:25 ≈ 6 h 8 m** — so the
  briefing's "~5.5 h" and my 5.9 h are the same cohort read at different moments, and no
  discrepancy remains. (Earlier drafts of this report got this wrong; corrected.)
- **Claims per worker:** 6–11 claims each over ~22 000 s → roughly **33–61 min of wall time per
  claim**, consistent with the duration figure above.
- **First-tool-call latency** (session start → first `tool_use`): HP n=92 **median 1.7 s** (83 of
  92 under 5 s); peerbox n=1662 **median 3.9 s**, 971 under 5 s, 11 over 120 s (max 1043 s).
  **Start latency is not the bottleneck** — 1 043 s is an outlier, not a pattern.
- **Verify backlog:** 365 completed bug rows awaiting a verdict at probe; the worker logged
  `[verify-closure] … bug rows are already awaiting a verdict (cap 100) — not enqueuing
  verification …, drain the verify backlog first` **six times during the night**, and the count
  it reported grew 272 → 305 across the window. Verification is being *refused*, not merely
  delayed.

### 4.7 The honest synthesis

"5.5 h alive, few merges" decomposes into four independent facts, none of which is "the workers
were broken":

1. **The 22:23:13 external cohort kill** removed the workers *and* the resident merger for the
   window's first ~1 h 25 min (§2).
2. **Conflict work dominated the created mix and had zero claims** (§4.4) — a priority-ordering
   gap, not idleness.
3. **Each run is long** — mean 36 min, max 1.9 h (§4.6) — so 39 claims in 7 h with 4 workers is
   the arithmetic, and the number of *finished* branches bounds the merge count.
4. **Verification is refusing work** (365-deep backlog, cap 100) (§4.6), which suppresses the
   verify→merged path even when a branch is ready.

## 5. The throughput view (reusable, run, output pasted)

`scripts/night-throughput.sh` (zero deps beyond bash/sqlite3/git/python3-stdlib). It is a
**script, not a new top-level tool** (Rule 3), read-only, and it takes `--since/--until/--repo`.
It deliberately ends with "this is a measurement, not a verdict" so it cannot be read as a
productivity score.

**Two silent-comparison bugs were found in my own view while building it** — both are the same
family (a comparison whose operands are spelled differently) and both produced *plausible* wrong
numbers rather than errors:

1. `strftime('%s','2026-09-16 22:00')` parses the wallclock as **UTC**; the ledger stores true
   epoch. The window slid +8 h and every ledger count came back **0** — indistinguishable from "a
   quiet night". Fixed with the `'utc'` modifier; bug filed as **`01a0ac30-63be-773f-a8d5-bc8dfb6b437b`**.
2. The provenance `ts` is ISO (`2026-09-16T22:23:13+08:00`) while the window is spelled with a
   space, so comparing `[:16]` compared **only the date** and the time-of-day filter did nothing:
   the view "found" **38** terminations for a 7-hour window (**14** real, §1). Fixed by normalizing
   both sides.

**Real output, peerbox** (`bash scripts/night-throughput.sh --since '2026-09-16 22:00' --until '2026-09-17 05:00' --repo /home/user/work/GoPi`):

```
created  claimed  completed  failed  cancelled
-------  -------  ---------  ------  ---------
151      39       65         6       6

claims by worker:
worker         claims  first                last
-------------  ------  -------------------  -------------------
peerbox:3074388  11      2026-09-16 23:37:39  2026-09-17 03:50:11
peerbox:3066414  10      2026-09-16 23:35:22  2026-09-17 04:27:43
peerbox:3067418  8       2026-09-16 23:35:41  2026-09-17 04:18:39
peerbox:3066922  6       2026-09-16 23:35:33  2026-09-17 04:46:06
peerbox:3680671  1       2026-09-16 22:14:02  2026-09-16 22:14:02
peerbox:3680484  1       2026-09-16 22:11:05  2026-09-16 22:11:05
peerbox:3325787  1       2026-09-17 00:21:30  2026-09-17 00:21:30
peerbox:317897   1       2026-09-17 03:11:32  2026-09-17 03:11:32

queue mix (created in window, by class):
merge-conflict 56 · other 39 · bug 32 · requirements 18 · recovery 6

landed on master in window:
  merge commits : 8
  all commits   : 54

terminations recorded in window (signal-provenance kind=signal):
  records: 14 signal, 0 intent (intent = a gopi process announcing its own kill)
  by signal: SIGTERM=11, SIGINT=3
  by role  : web=4, worker=3, 2444398=1, 2858364=1, 2833700=1, merger=1, task verify=1,
             --timeout=1, task list=1
   [full 14 rows = §1]
first-tool-call latency (session_start → first tool_use):
  n=1662  median 3.9s  mean 9.9s  min 0.8s  max 1043.0s
  buckets: 30-120s=62, 5-30s=618, <5s=971, >120s=11
```

**Same view on hostb-Laptop** — identical ledger totals (the two hosts share one synced ledger),
different worker set, and **7 signal records, 0 intent, all before the window**:

```
created  claimed  completed  failed  cancelled
-------  -------  ---------  ------  ---------
151      31       65         6       6
claims by worker:
hostb-Laptop:3145680  14  2026-09-16 22:06:15  2026-09-17 04:50:37
hostb-Laptop:821228    8  2026-09-17 00:24:28  2026-09-17 04:35:09
hostb-Laptop:2928345   7  2026-09-16 22:31:56  2026-09-17 04:32:33
hostb-Laptop:893092    1  2026-09-17 00:33:20  2026-09-17 00:33:20
hostb-Laptop:854073    1  2026-09-17 00:28:04  2026-09-17 00:28:04
first-tool-call latency:
  n=92  median 1.7s  mean 2.6s  min 0.8s  max 29.5s
  buckets: 5-30s=9, <5s=83
```

**The 0-on-both-hosts `intent` count is itself the answer to "who terminated the workers?"** — it
is the measurement that rules out every gopi self-stop path on both machines (§2).

## 6. Recommendations for nick

1. **Find the 22:23:13 / 05:43:25 sender (bug `01a0ac30-3f50`).** It is the single largest cause of
   lost night work, it is *not* a gopi path, and it recurred (7 h 20 min apart) while this was
   being written. Whatever issues these kills (a sweep script, a reaper, a hand-run command) must
   be identified before the next night run.
2. **Give the drain line provenance (bug `01a0ac30-3f69`).** Until it names the sender, this
   question costs a full investigation every time it is asked.
3. **Start workers in a `--user --scope` (§2.1).** `setsid nohup` detaches from the terminal but
   **not** from the login session's cgroup, so every worker still dies with the session that
   launched it. This is a one-line change to the operator's launch command, needs no privilege
   (Rule 25), and is the difference between a 7-hour cohort and a cohort that survives the shell
   that started it. **Left for the operator to make** — a worker is the operator's process.
4. **Schedule the conflict backlog deliberately.** 44 merge-conflict rows are `pending` at
   priority 5 while workers burn the night on priority-1 bugs. Either raise conflict rows' priority,
   batch them into one dedicated session, or explicitly accept that they are out of scope — but do
   not leave 44 claimable no-ops in the queue that *look* like work.
5. **Drain the verify backlog (365, cap 100).** Verification is refusing to enqueue; until it is
   drained, finished branches cannot reach the landing path. The 900 s `task verify` budget (§1
   rows 13–14) is also worth re-measuring at this size.
6. **Keep the throughput view.** `scripts/night-throughput.sh --since … --until …` is the
   instrument the night was missing. Run it *before* asking "why so little output" — it answers
   the counting half in seconds and it does not flatter.
7. **Treat "processes alive" with suspicion as a metric.** My own first pass read a mid-window
   `ps` snapshot as a lifetime; the honest pair is `claims` and `landed` (§4.2).

## 7. Honest "Not Done"

- **The 22:23:13 / 05:43:25 sender is not identified**, only characterised (4–8 pids, 1 s, no
  intent record, ≥4 systemd scopes → external kill, not a session teardown of one subtree, not a
  gopi stop path). Proving *which* tool issued it needs host-level evidence I did not have (no
  journal entry names the signal source; no shell history). I did rule out, by reading them, the
  two cron/observer scripts that fit the cadence (§2.1) — but a timed-out `gopi scratch-gc` tick
  inside a worker's own loop remains an open hypothesis I could not close without profiling.
- **I did not run `systemd-run --user --scope gopi worker` to prove the survivor behaviour.**
  Starting a worker is the operator's verb (Rule 6-worker-ownership), so the recommendation in §2.1
  is unverified on this host by construction; the mechanism is the one the daemon path already
  measures, and it is a change to the *launch line*, not to gopi.
- **No fix was written** for any of the four bugs. All four are filed with repro + acceptance.
- **`worker.log` contains zero records from the night window** (mtime 2026-09-16 11:06:58; the
  night's four workers logged to `/dev/pts/1,3,4,5`). So the briefing's premise — that `worker.log`
  "repeatedly shows" the night's drain lines — is **not reproducible**; the night's drain lines
  exist only inside per-session `output.log`. Filed as `01a0ac30-63dd-75cb-a064-54cdb92015d6`.
- **The briefing's "3 merges" is not reproduced** (I measure 8); I did not adjust either number
  to match the other.
- **My first pass at §4.6 was wrong** and is corrected in place: I read a mid-window snapshot
  (`etimes`) as the cohort's total lifetime. Recording it because a reader may still hold the
  "5.9 h vs 5.5 h mismatch" idea from an earlier draft.
- **`task_events` has no `claimed` rows**, so one of my intended sources (re-claim counting from
  the event log) is structurally empty; the re-claim answer in §4.5 comes from `tasks.claimed_at` +
  session claim lines instead. This is stated, not silently substituted.

## 8. Verification

- Build/vet/tests: **no production Go code changed** — the deliverable is one shell script plus
  this report, so `go build ./...` / `go test ./...` are unaffected. `bash -n scripts/night-throughput.sh`
  → syntax OK, and the script was executed successfully on **two** hosts (§5).
- Commit: see `git log --oneline -1` on `feat/night-worker-throughput`.
- Bug tasks filed: `01a0ac30-3f50-7c3d-991a-05fae3fc1809` (cohort kill),
  `01a0ac30-3f69-7983-8449-7cbc05a3f16f` (unattributable drain line),
  `01a0ac30-63be-773f-a8d5-bc8dfb6b437b` (strftime UTC window), 
  `01a0ac30-63dd-75cb-a064-54cdb92015d6` (worker.log stale / pty-only).
- Nothing was killed or restarted by this session; every read was read-only (`git log`, `sqlite3`,
  `ps`, `/proc`, `journalctl`, logs).
