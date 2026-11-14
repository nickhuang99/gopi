<!-- doc-class: historical -->
# The chat slot registry: who owns it, who may take a slot, and what happens when nobody received the event

Task `01a12d05-230d-7a94-aad5-ab49288f6697`. **Class:** A (diagnosis + fix).
**Scope:** three defects on ONE channel — the slot registry that `gopi chat` writes
and the sample sink `scripts/notify/notify.sh` scans. No new notification
mechanism, no new resident process, no new configuration key.

## The channel, in one paragraph

`gopi chat` registers itself at its ARM POINT (`chat_surface_surface.go`,
`chatSessionSlotRegister`) by writing `<dir>/chat_session_id_<0..9>.txt` holding
`<pid>:<session-directory>` (`config.FormatChatSurfaceIdentity` — the same value
the `GOPI_CHAT_SURFACE_ID` environment channel publishes). The registry travels
SIDEWAYS: `notify.cmd` (settings.json → `bash ~/.gopi/notify/notify.sh`) is exec'd
by the ledger's own machinery — the worker tick, the merge loop, a `gopi task
update` from a third shell — none of which is a descendant of the chat run it must
reach, so the environment channel cannot deliver the news. The sink loops the ten
slots and runs `gopi notify --session <dir> --message <summary>` for each.

## The three defects, and the one measurement that exposed them

Measured on the operator's own profile 2026-10-12 (`artifacts/slots-content-before.txt`):
**nine of the ten slots** named sessions under
`<scratch>/queue/queue-runtime-ui-session-*/chatreplay-sessions-*/sessions/<uuid>`
— i.e. **replay/film runs** — each with a dead pid and a session directory the
scratch sweep had already collected. The machine's one real chat sat in slot 1,
squeezed beside them. The operator's symptom: eight hours of task events delivered
to nobody.

### A — the registry resolved through the profile, while sessions resolved through a tree

`chatSessionSlotDir()` called `config.GopiDir()`. (The `GOPI_SESSIONS_ROOT`
relocation those runs published is GONE as of task `01a15981` — a replay mints no
session, see `devdoc/chat-replay-mints-no-session.md` — but this fix stands on its
own: `ChatSlotsDir()` is still the one derivation of "which tree owns this run's
registry", and a test or sandbox that relocates its sessions is still answered
correctly.) A **relocated** run — `gopi chat
--replay`/`--film`, which publishes `GOPI_SESSIONS_ROOT` before minting its session
(`cmd/agent/chat_sessions_root.go`, `applyChatReplaySessionsRoot`) — therefore had
its session in its own scratch tree and its slot in the **operator's** registry:
two paths, and the relocation moved only one of them. That is why a scratch run
occupied the operator's registry at all.

**Fix — one derivation, not a second channel.** `internal/config/sessions_root.go`
gains `ChatSlotsDir()`: the registry is *the directory the session tree lives in*,
i.e. `filepath.Dir(SessionsRoot())`. The default branch of `SessionsRoot` is
`filepath.Join(GopiDir(), SessionsDirName)`, so an ordinary run's answer is that
GopiDir **character for character**, while a relocated run gets the tree the
caller named. There is deliberately **no** `GOPI_CHAT_SLOTS` variable and no flag:
a second knob would be a second spelling of "which tree is mine", and the two could
be set to disagree. `ChatSlotsDirIn(gopiDir)` is the same derivation for a caller
that already holds the data root (Rules 13/14).

`scripts/notify/notify.sh` reads the SAME derivation on its side: `SLOT_DIR` is
`dirname "$GOPI_SESSIONS_ROOT"` when that variable is set, else the profile root —
so a sink exec'd from a relocated process tree scans the relocated run's registry,
and a production sink (the variable unset, which is every worker and merger) scans
the profile exactly as before.

### B — "the oldest by mtime" is not a lifetime

The old rule for a full registry was *replace the oldest mtime*. mtime records when
a run **started**, so a long-lived console — the one an operator actually watches —
could be evicted in favour of nothing, and the only trace was a line saying a
replacement happened.

**Fix — an order by EVIDENCE that the entry's subject still exists**
(`chat_session_slots.go`, `chatSessionSlotIndex`):

1. this run's own slot (idempotence: a re-registration must not consume a second);
2. a **free** slot;
3. a **reclaimable** slot — its session directory no longer exists, **or** the pid
   half names a process that is not running (`tools.PidAlive`, which counts a
   zombie as alive). Reclaiming one of these is not a choice between two sessions
   at all: it is what a dead run left behind;
4. **nothing**, when every entry still looks alive: the registration is
   **REFUSED** with an error naming the occupants.

Refusal rather than eviction is the deliberate choice: evicting a live chat costs
that console every later notification **and tells nobody**; refusing costs *this*
run the same silence **and says so on stderr, naming the slots**. Both outcomes
deliver nothing to one chat; only one of them can be seen.

An **unknown is a KEEP**: a `self` marker or an unparsable pid half is never
reclaimed on the pid ground — only on the directory ground — and an entry with a
directory that still exists is kept, because the sink's refusal is the honest answer
and this function must not be the second opinion that silently disagrees.

The registration also **prints where it registered** on success
(`chatSessionSlotSelfCheckLine`): the registry has two ends (this run writes a file,
the sink scans a directory derived from ITS OWN environment) and until now nothing
in the writing run said which directory that was — so the defect this document
fixes looked exactly like a correct run. One line naming the tree, the path and the
slot makes the channel checkable by the person watching the console.

### C — silence when nothing arrived

The loop reported per-slot refusals and stopped there. A machine where every
registered chat refused — or where no chat was registered at all — printed the same
SHAPE as a machine where nine of ten were served, and the operator learned their
notifications had been lost eight hours later from a ledger that moved without them
(Rule 15: 静默降级必须出声).

**Fix.** The summary line now carries the aggregate (`dead=`/`registered=`/
`slots_dir=`), and step **3b** adds the line the per-slot output cannot state:

```
notify.sh: ⚠ NO CHAT RECEIVED THIS EVENT — no chat is registered in <dir> (slots scanned=0). …
notify.sh: ⚠ NO CHAT RECEIVED THIS EVENT — 3 slot(s) registered in <dir>, 3 of them dead (their session directory no longer exists), 3 refused. …
```

The condition is about **delivery**, not refusal counts: what was lost is the
message, and it was lost whenever `delivered` is zero — whether because no slot was
registered, because every registered one was dead, or because every live one
refused. The two failing aggregates get different wording because they call for
different actions. The anchor (`<profile>/notify/task-events.jsonl`) is untouched
and still always written.

## Rule 31 — what was actually run

Binary: rebuilt from this worktree, `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`.
Raw evidence in `/home/user/.gopi/sessions/01a12d05-230d-7a94-aad5-ab49288f6697/artifacts/`.

### (a) A real event, through the real production path, into a real inbox

```
$ printf '%s' '{"event":"completed","task_id":"01a12d05-…","status":"completed","summary":"slot registry isolation (Rule 31 acceptance delivery 1/1)"}' \
    | bash /home/user/.gopi/notify/notify.sh
notify.sh: slot …/chat_session_id_0.txt (…/chatreplay-sessions-2839262006/sessions/262fd6c2-…): delivery refused: gopi notify: no session directory at …
… (slots 2..9, same shape) …
notify.sh: slot …/chat_session_id_1.txt (…/5af45c3e-24b6-4578-9f32-f4da2c40cde7): delivered
notify.sh: delivered=1 refused=9
exit=0
```

`notify.sh` in `~/.gopi/notify/` is the **installed copy** of this branch's
pre-change script (sha256 `8fcbdfa7…`, identical to `git show HEAD:scripts/notify/notify.sh`)
— deliberately: the acceptance run exercised the production path as it stands, and
the new lines are covered by the contract test and the (c) scenarios below.

The receiving session's inbox gained exactly one line (`artifacts/`; `inbox-before`
86 lines → 87):

```json
{"kind":"message","id":"3c1f9ef60369694b","session_id":"5af45c3e-24b6-4578-9f32-f4da2c40cde7","message":"task event: event=completed task=01a12d05-230d-7a94-aad5-ab49288f6697 status=completed summary=slot registry isolation (Rule 31 acceptance delivery 1/1)","source":"notify","created_at":"2026-10-11T23:25:02Z"}
```

### (b) A scratch run no longer touches the operator's registry

```
$ ls -l --time-style=full-iso ~/.gopi/chat_session_id_*.txt > slots-before.txt   # 10 files
$ sha256sum ~/.gopi/chat_session_id_*.txt            > slots-before.sha256
$ timeout 60 env GOPI_HOME=$SB/probe-home HOME=$SB/probe-home \
      ./bin/gopi chat --replay $SB/rec.jsonl --no-editor </dev/null
… [gopi] chat: slot 0 registered in $SB/probe-home/workspace/gopi-test-scratch/queue/queue-runtime-ui-session-1677919/chatreplay-sessions-1590426876 (this run's own tree) — a chat sink reading that directory will find …/sessions/5875f2b5-96a6-4c80-a2b2-b20777be43a3
exit=0
$ ls -l --time-style=full-iso ~/.gopi/chat_session_id_*.txt > slots-after.txt
$ diff slots-before.txt slots-after.txt   → no output: ls -l IDENTICAL (name, size, mtime)
$ sha256sum -c slots-before.sha256        → all 10 OK
```

The run's own registry is the relocated tree:
`…/chatreplay-sessions-1590426876/chat_session_id_0.txt` →
`1677919:…/chatreplay-sessions-1590426876/sessions/5875f2b5-…`. The local run's
profile never saw a slot file (`ls` of it: *No such file or directory*).

### (c) The two scenarios that used to be silent

**c1 — no delivery target at all** (`artifacts/c1-no-target.out`):

```
notify.sh: delivered=0 refused=0 dead=0 registered=0 slots_dir=$SB/c1/empty-profile/.gopi
notify.sh: ⚠ NO CHAT RECEIVED THIS EVENT — no chat is registered in $SB/c1/empty-profile/.gopi (slots scanned=0). The raw event is still anchored in …/task-events.jsonl; every chat console is blind to it until a `gopi chat` writes a slot there.
```

Anchor written (`artifacts/c1-anchor.jsonl`, 95 bytes).

**c2 — three registered slots, all dead** (`artifacts/c2-all-dead.out`):

```
notify.sh: delivered=0 refused=3 dead=3 registered=3 slots_dir=$SB/c2/profile/.gopi
notify.sh: ⚠ NO CHAT RECEIVED THIS EVENT — 3 slot(s) registered in $SB/c2/profile/.gopi, 3 of them dead (their session directory no longer exists), 3 refused. …
```

**c3 — the registry is full of chats that still look alive**
(`artifacts/c3-slot-lines.out`): a real `sleep` pid held all ten slots with session
directories that exist; a local `gopi chat` run:

```
chat session slots: all 10 slots are held by chats that still look alike… and none could be shown to be gone; refusing to evict one — slot 0 (<path>) holds "1683941:<path>/sessions/s-0"; … slot 9 …  — this run will not receive `notify.cmd` deliveries that address chats
```

Nothing was evicted (the registry still holds ten files). Before this change the
same run would have overwritten slot 0 — the oldest mtime — and the chat that owned
it would have gone silent with no line anywhere.

**c4 / c5 — the order is measured, not asserted** (`artifacts/c4-*.err`,
`artifacts/c5-*.err`), same scratch profile shape, differing only in whether a free
slot exists:

| scenario | slots 0..2 | slots 3..8 | slot 9 | taken |
|---|---|---|---|---|
| c4 | dead | free | free | **slot 3** (a free one) |
| c5 | live holder pid, dir exists | live holder pid, dir exists | dead | **slot 9** (the reclaimable one) |

In c5 all nine live holders still name the holder pid afterwards (`grep -c` per
file: 1,1,1,1,1,1,1,1,1,0).

## Regression

`go build ./...` — OK (from this worktree).
`make test-budget PKGS='./cmd/agent ./internal/config'` — see the run's
`## Verification` block in the task report for the observed result.
`bash scripts/notify/notify_slot_contract_test.sh` — 5 cases / 17 assertions, all
green, including the **control arm** (case 5: nothing relocated ⇒ the sink scans the
profile root, byte for byte the pre-change behaviour) and the **relocated arm**
(case 4: the sink scans the relocated tree and does *not* touch the profile's
registry).

Tests changed: `cmd/agent/chat_session_slots_test.go` — the mtime pin
(`TestChatSessionSlotReplacesTheOldestEntry…`) is **deleted on purpose**: it pinned
the rule this change removes. Two arms replace it
(`TestChatSessionSlotReclaimsOnlyAnEntryWhoseSubjectIsGone`,
`TestChatSessionSlotRefusesToEvictALiveChat`) plus
`TestChatSessionSlotRegistryDirFollowsThisRunsOwnTree` (the isolation pair, both
arms). `cmd/agent/chat_session_slots_script_test.go` (the cross-language pin on
`SLOT_LIMIT`/`SLOT_PREFIX`/`SLOT_SUFFIX`) is untouched and still green.

## Not Done (honest)

- The **installed** `~/.gopi/notify/notify.sh` was not replaced: it is outside this
  run's tree, and writing an operator's profile is out of scope. The new aggregate
  lines therefore ride the next branch that installs them; the (a) run deliberately
  used the installed copy so the acceptance observation is of the *production* path.
- The `dead` counter counts a slot whose directory is gone **and** an unparsable
  identity together; the `registered` count is the denominator for both reports.
- Nothing reclaims a dead slot from the sink side, by design (Rule 35): a dead slot
  costs one refused delivery and is reclaimed by the next chat that needs a slot.
- `parent`/`chat` sessions on a peer host, and a sink exec'd from a *different*
  process tree than the relocated run, are out of scope — the two ends agree only
  when the relocation is inherited (which is how `bash`-tool children get it).
