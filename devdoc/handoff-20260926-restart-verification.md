<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
# Handoff — restart verification for the new `gopi` binary

**Read this immediately after starting a session.** Written by the session that
spent 2026-09-26 doing operator work on the **old** binary (HP `41475bf5` → later
`166c2a40`), against **peerbox**. It is a *verification* checklist, not a summary:
every "verified" below was measured, and every command is copy-pasteable.

Two rules carried over from the previous handoff and both paid off again:

1. **A handoff's "not done" is a snapshot of its writing moment, never a fact
   about the tree you are standing in** — check the binary before believing a
   checklist. This doc was *itself* stale twice while being written (see §2).
2. **Verify with the artifact, not the ledger.** "Task completed" means
   delivered-to-a-branch — never "on master".

---

## 0. THE FIRST ACTION: build the new binary and re-run the 60-second smoke test

```bash
cd /home/user/work/GoPi
CGO_ENABLED=0 go build -o /tmp/gopi-new ./cmd/agent/     # expect: silent, exit 0
git log --oneline -1
```

Then the surface smoke test (§5 of the previous handoff, unchanged):

```bash
mkdir -p /tmp/ui-check && cd /tmp/ui-check
cat > drive.py <<'PY'
import os, pty, select, time, struct, fcntl, termios
BIN="/tmp/gopi-new"; DUMP="/tmp/ui-check/live.dump"
pid, fd = pty.fork()
if pid == 0:
    os.execv(BIN, [BIN, "ui", "--addr", "127.0.0.1:0", "--dump-screen="+DUMP])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
buf=b""; t0=time.time()
while time.time()-t0 < 6:
    r,_,_ = select.select([fd],[],[],0.5)
    if r:
        try: buf += os.read(fd, 65536)
        except OSError: break
print("pty bytes:", len(buf)); print("dump:", os.path.getsize(DUMP) if os.path.exists(DUMP) else "MISSING")
os.write(fd, b"q"); time.sleep(1.3)
try: os.kill(pid, 9)
except Exception: pass
PY
timeout 30 python3 drive.py
head -2 live.dump
```

**Measured on the old binary (2026-09-26):** `dump: 5500` bytes, provenance head
`#gopi-ui-dump … frames=4`, row 0 `gopi ui → http://127.0.0.1:<port>  (tools off)`,
`cursor row=29 col=0`, `attrs_begin … 000|0:100=default`. **A different non-zero
size is investigate-worthy, not automatic failure; `MISSING` or a head that is not
the provenance line IS a failure — stop and report.**

---

## 1. What landed during this session (verified, with the artifacts)

The session filed and verified two defects and ran several operator
investigations. All four items below were measured, not assumed.

| Item | Status | Evidence |
|---|---|---|
| `TurnHeader` silently truncated long prompts | **FIXED & ON MASTER** | commit `fdfd2627 fix(lineedit): sign every TurnHeader cut with the documented ellipsis` |
| emphasis reaches per-cell attrs on the real render path | **VERIFIED** (test written, uncommitted) | §3 below |
| chat `--dump-screen` capability | **MERGED into peerbox master** (`3709f646`) — see §6.2 | on peerbox: `grep -c dump-screen cmd/agent/cli_chat.go` = **2**, `cmd/agent/chat_dump.go` exists |
| `task/01a0d8a9…` (the turn-header bug row) | ledger `completed`; fix on master as `fdfd2627` | `gopi task show 01a0d8a9-c9b0-7539-9705-33d43822f9ef` |

### 1.1 The turn-header bug is fixed — re-verify like this

**Bug as filed:** `TurnHeader` (`internal/lineedit/answer.go`) truncated the
prompt label to a budget of `cols-14` cells and the cut was **silent** — the doc
comment claimed the status-row ellipsis was appended, but `truncateCols` returned
early without it. Measured at cols=100: label capacity 92 cells, no `…`.

**Now on master** (`cutField` replaced `truncateCols` in every cut path):

```bash
cat > internal/lineedit/zz_probe_test.go <<'EOF'
package lineedit
import "testing"
func TestProbe(t *testing.T) {
  p := "don't be confused with gopi ui and gopi chat! You are gopi chat which is using GO bubbletea."
  for _, c := range []int{80, 100, 120} {
    h := TurnHeader(TurnInfo{Index: 2, Prompt: p}, c)
    t.Logf("cols=%d width=%d %q", c, displayWidth(h), h)
  }
}
EOF
go test ./internal/lineedit/ -run TestProbe -v -count=1
rm -f internal/lineedit/zz_probe_test.go
```

**Measured on the new binary:**

```
cols=100 width=100 "──── turn 2 · don't be confused with gopi ui and gopi chat! You are gopi chat which is usi… ────────"
cols=80  width=80  "──── turn 2 · don't be confused with gopi ui and gopi chat! You are go… ────────"
cols=120 width=120 "──── turn 2 · don't be confused with gopi ui and gopi chat! You are gopi chat which is using GO bubbletea. ─────────────"
```

The `…` is present at 80 and 100, absent at 120 (where the prompt fits). **That
is the fix.** Live version: run `gopi chat`, submit a prompt > ~92 chars, read
the header.

---

## 2. THE TRAP — this doc went stale TWICE while being written (a third change landed during review)

Three claims were true when written and false by the time you read this. All three
were caught by re-measuring, which is the habit this section exists to pass on:

- **"the bug is filed, not fixed"** — it was fixed (`fdfd2627`) *during* this
  session, after the session had stopped looking.
- **`master` moved twice** on HP (`41475bf5` → `166c2a40`), and **15 commits**
  arrived from peerbox.
- **"chat `--dump-screen` is unmerged, absent from master"** — it was **merged and
  its branch deleted** (`3709f646`) between the verification pass and the final
  read. Only the last of the three would have been caught by *assuming*; it was
  caught by *re-running*. (§6.2.)

**RULE: re-run the probe before believing any row of §1.** A row here is a claim
about a moment. The commands are in the doc so that re-checking costs one round,
not one argument.

---

## 3. §2.3 of the PREVIOUS handoff is now closed: emphasis reaches real attrs

The previous handoff's last open item read: *"nobody has yet confirmed on the real
surface that a `**bold**` span shows as a non-default attr run."* It does.

`internal/uiclient/live_emphasis_attr_test.go` — **written this session, PASSING,
but UNCOMMITTED (untracked in `git status`)**. It renders an emphasis-bearing
answer through the real model with `lineedit.BasicPalette` (the live-terminal
capability), pushes the bytes through the surface's own `ScreenDumper`, and reads
the per-cell attrs back. Measured:

```
answer row 1 runs: [0..35=default  36..44=bold  45..67=default]
answer row 1 text: "              gopi> The answer: the row shear comes from counting a"
```

Cells **36..44** are `bold` — exactly the 9 cells of `row shear` — and the markers
are absent from the drawn text.

**Honest limit:** this is the model+dumper path with bubbletea mocked at the
boundary, **not a live PTY**. It closes the renderer→attrs link (the genuinely
unverified gap); the dump side was already confirmed live. A full live check
(type `**bold**` into a real turn, read the dump) is still worth doing on the new
binary.

**DO THIS FIRST:** decide whether to commit that test or discard it — an
untracked test file is a landmine for the next `git status`.

---

## 4. The `**bold**` grammar — one marker, and it is NOT markdown

Measured via `lineedit.LineSpans`:

| you type | markdown means | this surface does |
|---|---|---|
| `**x**` | bold | **bold** (markers stripped, attr run non-default) |
| `*x*` | italic | **literal `*x*`** — no rule matches |
| `2*3*4` / `a footnote*` | — | literal, untouched |

There is **no italic emphasis in this surface**: the enum holds only
`EmphasisNone` and `EmphasisBold`, `BoldMarker = "**"` (emphasis.go). Single
asterisks are never consumed — the conservative choice, since guessing italic
would have to guess about `2*3*4`. **When writing prose into a `gopi chat`
transcript, do not assume markdown; use `**` deliberately or not at all.**

---

## 5. Cursor / layout facts (asked-and-answered, so nobody re-derives them)

`gopi ui` layout on a 100x30 terminal:

```
row  0     gopi ui → <target>  (tools off)     headerLine()
row  1..27 transcript (padded blank when short)
row 28     you> <input>▏                        inputLine()
row 29     idle · turns N · tool events N       statusLine()
```

- **The `▏` in `you>` is PAINTED TEXT, not a terminal cursor.** `inputLine()`
  splices it into the string at `m.cursor`.
- **The real terminal cursor is never positioned by this code.** There is no
  `tea.Cursor` / `WithCursor` / `SetCursor` anywhere in `internal/uiclient/` or
  `cmd/agent/`; `Run()` passes only `WithInput`/`WithOutput`. The dump's
  `cursor row=29 col=0` is "wherever the last written byte left it", not a design.
- **Why it is this way:** a painted caret stays *inside* the text representation
  the whole self-observation design depends on; a hardware cursor is invisible to
  a screen dump. Cost: IME popups / accessibility tools that track the hardware
  cursor are misled. Changing it means opting into bubbletea's cursor API **and**
  keeping the painted `▏` from double-drawing — and it would trade away dump
  visibility of the caret.

---

## 6. Branch / delivery topology — the operator-side facts that cost the most time

### 6.1 Three independent axes; conflating them is the recurring error

| axis | knob | question it answers |
|---|---|---|
| collaboration | `task_sync` (default **false**) | automatic **task-ROW** exchange |
| code delivery | `sync_peers` | branch **REF** push/fetch (and close-out push-back) |
| identity | `role` + `sync_to` | which node arbitrates / whose push-back runs |

**Source, verbatim** (`cmd/agent/cli_tasks_sync_git.go`): the git pass *"is the
CODE-DELIVERY axis… It is NOT the collaboration axis (`config.TaskSyncSettingsKey`),
and it therefore does not consult that switch."* So `task_sync: false` does **not**
stop branch delivery — `sync_peers` does.

**HP's config was changed this session** (`~/.gopi/settings.json` →
`.ds_hp_standalone_settings.json`): added **`"task_sync": false`** (explicit, was
already the default). Backup: `.ds_hp_standalone_settings.json.bak-20260926-000749`.
Current: `task_sync=false`, `sync_peers=[]`, `role=client`, `sync_to=peerbox`,
`memory_peers=[peerbox]`.

**Cosmetic gap worth knowing:** `TaskSyncLine()` prints `off (default …)` whether
the switch is absent OR explicitly `false` — the banner cannot tell a declaration
from a default, even though `taskSyncSource()` exists and the on-branch uses it.
Behavior is unaffected.

### 6.2 Why a "completed" task's branch is still local — TWO different causes

Measured on `task/01a0d5ca…` (the chat `--dump-screen` work):

```bash
git ls-remote --heads peerbox        # it was NOT there
git log --oneline -1 task/01a0d5ca22507cb9ba577dc6000739cd   # 1cf98e3b
```

- **Cause A — `sync_peers: []`.** `finishCleanupServerTarget()` returns `""` →
  the client delivery arm (`push → worktree remove → branch -d`) **skips
  fail-soft**; `HasSyncPeers()` false also short-circuits the serve-tick git pass
  (*"single-node: no peer to fetch/push — the pass is a pure no-op"*).
- **Cause B — the guard is CORRECT.** `branch -d` is gated on
  `finishCleanupBranchForceDeleteReason(delivered)`: *"the push did not land — no
  force-delete of an undelivered branch."* The branch **survives by design** —
  the local copy IS the only copy. The worktree was released anyway (that step is
  not peer-gated), producing the half-cleaned state that looks broken.

**This was pushed manually this session:**
```bash
git push peerbox task/01a0d5ca22507cb9ba577dc6000739cd:refs/heads/task/01a0d5ca22507cb9ba577dc6000739cd
# verified at push time: local tip == remote tip == 1cf98e3b
```

**THEN IT WAS MERGED — and the merge deleted the branch, exactly as designed.**
Shortly after the push, `gopi merge` landed it on peerbox:

```
3709f646  Merge branch 'task/01a0d5ca22507cb9ba577dc6000739cd' into master
1cf98e3b  feat(chat): --dump-screen — the chat surface observes its own answer band
```

Verified on peerbox: `git merge-base --is-ancestor 1cf98e3b master` → **YES**;
`grep -c dump-screen cmd/agent/cli_chat.go` → **2**; `cmd/agent/chat_dump.go`
(13574 bytes) present. The branch then disappeared from `git ls-remote --heads
peerbox` — **this is the completed delivery, not a loss.** It is the end state the
"pushed → delete local branch" rule was describing all along.

**How to see this correctly:** at push time the branch was genuinely the only
copy; by the time you read this it is in peerbox master and the ref is gone. Both
readings were true in their moment — which is the §2 lesson again, at a longer
timescale. **Check `git merge-base --is-ancestor <tip> master` before concluding a
missing branch was deleted by mistake.**

### 6.3 `gopi merge --fast` does NOT touch worktree-held branches — do not blame it

**A wrong diagnosis was made in this session and corrected; record it so it is not
repeated.** The claim was "`gopi merge --fast` swept the worktree away". The
source says otherwise:

- `merge_run.go`: a branch *"held by a worktree stays UNTOUCHED — it is a name
  already decided or somebody's live checkout"*; the verdict constant is
  `mergeDetailWorktreeHeld = "checked out in a worktree — that is somebody's live
  work, left alone"`.
- `--fast` *"STOPS HERE, and stopping here is the whole point of the flag… The
  branch is therefore reported and left EXACTLY as it is — same ref, same tip, no
  archive tag, no ledger row."* Its documented never-list: *"no throwaway
  conflict-probe worktree, no measurement, no branch-rework filing, no merge gopi."*

### 6.4 The REAL remover, and the tool that names removers

**`~/.gopi/logs/removal-audit.log`** (JSONL) is the authoritative record. The
worktree was removed by:

```
2026-09-25T16:57:28Z  kind=worktree-removal  arm=task-self-own-sweep  pid=2183284
  target=/home/user/workspace/gopi-01a0d68746ed7ea090e56bff794e1b9f
  branch=task/01a0d68746ed7ea090e56bff794e1b9f
  evidence="unowned registration of this run's own repo; live-session-veto and the
            live-holder scan did not refuse; checklist own-ground + integration-lineage"
```

**Not a merge — a task run's own sweep** of its own repo, once the killed fixtures
no longer vetoed it. **RULE: an unexplained worktree/branch removal is answered by
`grep <path-or-branch> ~/.gopi/logs/removal-audit.log` — never by reading shell
history.** The audit exists precisely for this.

Two caveats, and the standing doc that carries them:
**`devdoc/worktree-removal-forensics.md`** is the map — the sink's path spelling
(`logs`, plural, from `LogDirName`), the record's fields, the arm census with a
one-line meaning each, `--fast`'s boundary, and the worked grep. Re-run the grep
against **your** sink before repeating a quoted removal: the audit line above was
written by the peer that ran it, and this host's `removal-audit.log` carries no
line for `01a0d68746ed7ea090e56bff794e1b9f` or pid 2183284.

---

## 7. Known defects / open items (honest, not fixed)

1. **`internal/uiclient/live_emphasis_attr_test.go` is UNTRACKED.** Commit or
   delete it (§3).
2. **HP's local branches are stale vs peerbox.** HP still holds
   `task/01a0d5ca…` (`1cf98e3b`) as a local branch; peerbox has **merged** it
   (`3709f646`) and deleted the ref. HP's `master` was `ahead 4, behind 15` vs
   `peerbox/master`. **Reconcile before branching new work** (§6.2) — the old
   handoff's "merge the unmerged branch first" action is now **done**, but HP
   has not pulled that master.
3. **`task/01a0d68746ed7ea090e56bff794e1b9f` (`b14acfe9`)** — the signalrec
   handler-ownership fix. **Master already carries an equivalent**
   (`2a13830c fix(signal): read handler ownership at SIGNAL time`), and
   `ClaimSignalHandlers` is live at `internal/signalrec/signalrec.go:286` +
   `cmd/agent/cli_term.go:244`. A merge-dry-run conflicts in **5 files**
   (`cli_term.go`, `cmd/agent/main.go`, `DOC-INDEX.tsv`, `signalrec.go` ×4 hunks,
   `signalrec_test.go` ×2). **Its work is likely redundant — verify by running
   `TestTask_BarePrompt_SIGTERM_WritesSessionEnd` on master, then retire it (the
   repo's pattern is tag-never-`-D` for undelivered work).**
4. **`TaskSyncLine()` off-branch cannot say "declared false"** (§6.1) —
   cosmetic.
5. **The run-closeout report is duplicated** in `worker --resume` output — the
   whole completion block printed twice and the `skipped:` line three times
   (`/tmp/strange_python.txt` on peerbox). Cosmetic, real.
6. **Test fixtures leak processes that pin worktrees.** Task `01a0d687`'s own test
   left `python3 …/blackhole.py` (pid 1503983) and `signalrec.test` (pid 1525157)
   orphaned to init for **11h+**, standing in the worktree with their `cwd`, which
   made every close-out refuse to release it. **These are the real cause of the
   "worktree in use by live process" loop** — not gopi. Check for leaked fixtures
   after long test runs; they hold both a worktree **and** `/tmp` space.
7. **Pre-existing master reds** (not caused by this session): `internal/stylegate`,
   `internal/smoke`.

---

## 8. Where to read more (in order)

1. `devdoc/handoff-20260924-ui-perception.md` — the parent handoff; its §2.3 is
   now closed by this doc's §3.
2. `~/.gopi/logs/removal-audit.log` — who removed which worktree/branch, and why.
3. `internal/config/task_sync_switch.go` — the four-axis explanation, verbatim.
4. `cmd/agent/merge_run.go` — `--fast` semantics and the worktree-held exclusion.
5. `internal/tools/task_finish_cleanup.go` — the push/worktree/branch chain and
   every guard on it.
6. `internal/lineedit/emphasis.go` — the one-marker grammar (`BoldMarker = "**"`).
