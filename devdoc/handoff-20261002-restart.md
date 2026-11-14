<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Handoff — 2026-10-02 restart

**Written for:** the next PA instance after nick restarts Hermes.
**Why restart:** PA was consuming context on a long-running four-node session; nick is restarting to get a clean window.

---

## 1. What is running RIGHT NOW (restart will kill these)

### peer1 — 4 workers, 4 tasks in_progress

| task | worker pid | worktree | ahead | summary |
|---|---|---|---|---|
| `01a0f88a-a830` | 131027 | `~/workspace/gopi-01a0f88aa830731f97a46772a7805130` | 5 | 修两条管线都退回 xterm：探测把超时误判成"未安装" |
| `01a0f89d-bec3` | 2835652 | `~/workspace/gopi-01a0f89dbec37a81b1c73302301ceb7c` | 4 | 修上下文用量的两处显示口径（loop.go:5499） |
| `01a0f8aa-631d` | 130462 | `~/workspace/gopi-01a0f8aa631d78e9b7438dd8e7e42e92` | 1 | Finish attributing the residual red in TestChatInject_Escape |
| `01a0f8ae-3d17` | 1601417 | `~/workspace/gopi-01a0f8ae3d1770048de7dfef72f02a65` | 2 | bug(cmd/agent): chat-surface bound case fails in whole-package |

**All four worktrees are dirty-free and committed** (verified: `git status --porcelain` empty, ahead 5/4/1/2). Nothing already-written is at risk.

**What IS at risk:** the in-flight `make test-budget` / `go build` runs die mid-flight, and each run's LLM conversation loses its live context. Their ledger rows will be left `in_progress` with a dead `claimed_by` pid → **they become zombies**. See §4 for the recovery command.

### peer1 — one long-lived merger loop

`3119319  129660  gopi merge --every 5m` (started ~10:44 ago, PPID 129660 = a shell).

> *(`ps` output quoted verbatim from the 2026-10-02 restart. That loop — and the
> top-level `gopi merge` it ran — was removed on 2026-11-01, so no process on this
> host can print this row today. Kept as the record of what was running then.)*
This is a foreground loop. **If its parent shell dies with the restart, it dies too** — that is fine and expected (nick: "launcher 死了就死了"). No state to lose.

### peer1 — one live chat surface (nick's own)

`133940  131479  gopi chat --tools --dump-screen` (13h42m). This is **nick's own interactive session** — do not kill it. It has `--dump-screen` armed, so its dump is being written and `gopi ui_state --target chat` can read it.

### peer1 — the round-1/2/3 four-vendor probe leftovers

Many `/usr/bin/dbus-daemon ... at-spi/bus` + `gvfsd-fuse` children under
`~/.gopi/sessions/01a0f88a-a830-.../probe/{rt,rt3,runtime,armE/scratch}/`.
These are all children of the `01a0f88a` task above (the X/xterm probe task). They die with it.

### HP — 1 worker + 1 merger loop

- `2185839  1270688  gopi worker` (1m06s, PPID = nick's interactive shell `-bash`) — freshly started by nick, currently idle-cycling.
- `2171337  1634225  gopi merge --every 5m` (39m53s).
- HP ledger: **pending 0, claimed 0, in_progress 0** — fully clean. Nothing on HP needs rescue.

---

## 2. State of the fleet (correct as of this handoff)

| | peer1 | HP |
|---|---|---|
| master | `682e417d` | `682e417d` (aligned, divergence 0 0) |
| ledger | see below | completed 1885 / cancelled 1065 / failed 204 / verified 68 |
| workers | 4 | 1 |
| merger loop | yes (pid 3119319) | yes (pid 2171337) |

peer1 ledger (from the same query as the running tasks above): 4 in_progress, 17 pending.

**peer1 pending queue (17 rows, priority-first)** — these are the "next work" if a worker frees up:

```
01a0f8c2-148b  answer band 渲染 CJK 行时行尾多一个重复字符（"边界。界。"）
01a0f8c2-5154  gopi ui_state --target chat 挂死零输出（两次实测）
01a0f8c9-2022  chat status row: ctx occupancy read-out evicted
01a0f7b7-7449  HP 私有 Xvfb 演示录制：xterm 产出 7 帧 + verify-frames.py 7/7
01a0f7fa-0b85  arm3: HP 真跑一次拍摄取 7/7 正文整屏
01a0f871-1dd8  rework: cmd/agent TestChatSurfaceBound strict-count assert
01a0f88f-87b4  demo 主页接线：净化图 + 动画接进 final/index.html
01a0f891-9f7b  demo 主页改进依据（19 张图里 17 张是 materials 素材图）
01a0f897-0184  task update 放开内容修改（现 summary/payload/parent immutable）
01a0f898-a4e3  Rework: task/01a0f84aae... (chat quiet-*)
01a0f8b3-54a8  AST 标注辅助读代码的 A/B 测量（go_parser.txt）
01a0f8b9-41ba  泛化 job-notice 通道为通用"外部事件→chat 输入"
01a0f8c6-2f57  rework: land task/01a0f86ccb... (bound-*)
01a0f765-323d  docs/archive/demo/final: 用 01a0f677 的新展示图重跑 build.sh
```

---

## 3. What was accomplished this session (do not redo)

### 3a. Four-vendor competition — round 3 DONE and backflowed

peer1 `682e417d` — `docs(report): round-3 four-vendor competition`. Brought in **file-level from HP (hp/master `afe3fb5a`)**, never by branch merge.

Results (all measured, not judged):

| grid | before | after |
|---|---|---|
| **T3** breakpoint-resume | 未判 (all four finished too fast to SIGKILL mid-run) | **实测 4/4** — each single-turn run bound to real tool round-trips >60s (84.80 / 82.42 / 60.70 / 138.90 s); SIGKILL landed mid-run at 28/60, 31/60, 6/60, 53/60; **all four resumed from the breakpoint** with no replay of 1..N |
| **T5** dependency gate | 已判 but the recipe was WRONG | **逐条钉死** — `pending→completed` is ILLEGAL; legal is `claimed→in_progress→completed`; 6 parent states × verbatim suffix; dangling parent id refused at submit time (exit 1) |
| **T6** execution isolation | 旁证 only | **实测** — `claude --worktree` writes only inside its own worktree (parent checkout status unchanged); gopi/codex/dsh write into the checkout they were pointed at; main checkout `git status` diff empty at every leg |

**The trap that was correctly avoided (same family as round 2's `heartbeat.go`):** HP's base is ~482 commits behind. `git diff master hp/master` showed **1446 lines of deletions** — but those were *master's own newer files* (`internal/lineedit/parkgate.go`, `tools/replay/verify.py`, `editor.go`, `DOC-INDEX.tsv`, …) that HP's old base simply predates. **An old base makes master's additions look like the branch's deletions.** Always verify each file with `git cat-file -e master:<path>` / `git cat-file -e <hp-tip>:<path>` before backflowing. Only 1 true addition + 1 pure-addition edit were taken.

### 3b. HP ledger fully cleaned

- Cancelled ghost row `01a0ece0-ede2` (source=server, from-peer1, zero claim stamp, no session dir ⇒ "orphaned by construction"; its subject was already cancelled on peer1).
- Cancelled zombie row `01a0f8a8` — **this was the round-3 T3 experiment's own gopi leg**, deliberately SIGKILLed at 60s to test resume (kill landed at 53/60, resumed session `675c49eb`).
- Cancelled 3 × round-3 T5 experiment residue rows (`01a0f8a5-2956/-2d40/-2d87`).
- Killed zombie worker `635823` (4d23h, cwd=/, PPID 1) + 2 orphan `tee` processes — this was the actual cause of round-2 T6's "submitted then waited 220s, nobody claimed".

HP now: **pending 0 / claimed 0 / in_progress 0.**

### 3c. Answered: where is `system.md`

**`/home/user/.hermes/system.md`** (47,469 B, mtime 2026-09-23 17:14; **line 2 declares it was generated from `system.db` at 2026-07-17** — so its content is ~2 months staler than its own mtime).

It is the source of the `Infrastructure Overview` block injected into **my** system prompt. It contains a lot of stale facts: RPi5 IP written as .14 (actual .15), Ollama/host-b written as .13 (actual .12), NAS and HP **both .5**, WiFi AP .12 colliding with host-b, DeepSeek written as `deepseek-v4-pro ¥1/M` ( **v4-pro retired 2026-09-14**, now routed to flash), glow-moon written as "2254 tasks, 7 cronjobs" (cronjobs now 0), and the project table is missing gopi/GoPi/Maps entirely.

**BUT — and this is the key correction:** `gopi chat` **cannot read this file.** gopi is an independent binary that does not recognise Hermes profile directories; it reads **`~/work/GoPi/GOPI.md`**. So if gopi chat is reporting stale environment info, **the source to fix is `GOPI.md`, not `system.md`** — these are two different injection channels hit by the same disease (hand-maintained environment facts rot).

**Not yet done:** auditing `~/work/GoPi/GOPI.md` for stale environment facts, and deciding whether to fix `~/.hermes/system.md` (would need confirming whether the generator `system.db` still exists, or whether the file is now hand-maintained).

---

## 4. What to do immediately after restart

### Step 1 — find the zombies

The 4 in_progress rows will have dead `claimed_by` pids. Check:

```bash
cd ~/work/GoPi
sqlite3 ~/.gopi/memory.db "SELECT substr(task_id,1,13), status, claimed_by FROM tasks WHERE status='in_progress';"
```

For each, `ps -p <pid>` — empty means the process died. Then decide: **resume** (if the raw log checkpoint exists and the work was incomplete) or **cancel** (if it was an experiment/probe row).

**Do NOT merge a killed run's branch** — a killed run is "中断待 resume", not "完成待 merge". The worktrees are already committed, so nothing is lost, but the run's own report is missing.

### Step 2 — restart the merger loop (it died with its parent shell)

```bash
cd ~/work/GoPi && systemd-run --user --scope --unit=gopi-merger-loop-pa --quiet -- ./bin/gopi merger --every 5m
```

Verify cgroup is NOT `session-*.scope`:
```bash
cat /proc/<pid>/cgroup
```
Should be under `user@1000.service/app.slice/gopi-merger-loop-pa.scope`.

### Step 3 — HP worker (operator-only, nick must run it)

```bash
ssh hp '~/bin/gopi-worker-up.sh 1'
```

HP currently has 1 worker that nick started; if it died with the restart, restart it.

---

## 5. Standing constraints (do not violate)

- **Worker is operator-only.** PA must never run `gopi worker` / `~/bin/gopi-worker-up.sh`. The script's own header says OPERATOR-ONLY.
- **gopi is a single program.** Never add a shell script/wrapper/timer/unit file for something gopi should do itself. (Rule 35; nick has explicitly banned unit files — "注定会被人遗忘，然后莫名其妙的自己就开炮了".)
- **Backflow is file-level, never branch merge** when the peer's base is far behind. Verify each file with `git cat-file -e`.
- **Killed runs are resume candidates, not merge candidates.**
- **`gopi task update` can only change status** — no `--priority`, no content edit. Already-submitted rows cannot be reordered; to raise priority you must `gopi task run <full-id>`.
- **Always give the full task id** — prefixes are not accepted by the CLI, and prefixes collide in the ledger.
- **Never start VPN on peer1** (hijacks routes → DeepSeek API breaks).
- **Test DB / scratch work goes in `~/workspace/`, never `/tmp`** (`/tmp` is on the cramped `/` partition).
- **Every shell command must have a `timeout`.** Exit 124 = that path is blocked right now; switch to a lighter method, don't raise the timeout.

---

## 6. Housekeeping debt (known, not blocking)

- peer1 `.git/gc.log` exists → automatic git gc is disabled, so unreachable loose objects accumulate. `git prune` is destructive (unreachable objects may be another run's only surviving work) — **not run; needs a decision**.
- HP banner reports `failed-needs-reissue: 138 failed row(s), no successor row and no "resolved" tag`. Task `01a0f3cf` (the predicate that surfaces this) is already live. Reissue-or-tag is outstanding.
- `~/.hermes/system.md` staleness (see §3c) — source identified, fix not dispatched.
- `~/work/GoPi/GOPI.md` possibly stale (what gopi chat actually reads) — not yet audited.

---

## 7. Deliverables from this session

- peer1 `682e417d` — round-3 competition report + T5 recipe fix (backflowed from HP).
- `tasks/01a0f8960d5c789fa36dd946cecf4062-report.md` — the round-3 report (T3/T5/T6, full evidence index, honest Not Done).
- `briefings/brief-four-vendor-comparison-task-set.md` — T5 recipe corrected: `pending→completed` marked illegal, the legal three-step written in, plus the measured gate matrix.
- HP evidence root: `~/.gopi/sessions/01a0f896-0d5c-789f-a36d-d946cecf4062/{evidence,env,probes,lab}/`.
- HP safety tag: `keep/hp-round3-afe3fb5a` (HP's round-3 tip before the reset).
