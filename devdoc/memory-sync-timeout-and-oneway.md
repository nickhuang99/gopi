<!-- doc-class: normative -->
# memory sync: the 15 s leg kill, the one-way modes, and what a bound is for

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **LIVE AGAIN 2026-11-06 — the command this page measured came back, so the page
> is now a description as well as a record.** `gopi memory sync` was deleted by
> `5677cc6e` on 2026-11-05 with the whole cross-node sync CLI family, and it was
> restored the next day as a MANUAL door (one invocation = one session, exit,
> no `--every`); `devdoc/memory-sync-manual-door-restored.md` is the shape now.
> **What is live below:** `--peer`, the three direction shapes (bidirectional by
> default, `--push`, `--pull`), the 900 s default bound, and BOTH faces of that
> bound — `--timeout SECONDS` wins over `GOPI_MEMORY_SYNC_TIMEOUT_SEC`, one
> reader (`assetSyncSSHBound`). **What is NOT live below:** the `--every <dur>`
> cadence (§ of the companion page) and the store-trigger, which stay retired for
> their own reasons. The MEASUREMENT the page exists for — 5209 rows / 15 MB,
> a 47.5 s peer-side apply against a 15 s bound — is why the default is 900 s and
> not 15 s, and it is unchanged. The one-way COPY `gopi memory backup` is the
> other manual door and is bounded by its own knob.

`gopi memory sync --peer hp` failed on the real pair with:

```
gopi memory sync: memsync assets: ssh hp: signal: killed (stderr: )
```

Nothing in that line says what happened: not the direction, not the size, not
the bound, not even whether the peer was at fault. This is the post-mortem of
that line, the fix, and the two one-way modes the operator asked for.

## 1. The measured cause — a stall bound doing a work-size estimate's job

`internal/tools/memory_sync_assets.go` bounded its ssh leg with
`memoryAssetSyncSSHTimeout = 15 * time.Second`, copied from the **task-ledger
delta** channel's bound (`syncSSHTimeout`, same file family). The comment
justified it as "assets are the same class of bounded exchange". They are not.

| | task-ledger delta leg | memory-asset leg |
|---|---|---|
| payload | rows newer than an ANCHOR (a delta window) | the WHOLE local knowledge corpus |
| measured here | a handful of rows | **5209 rows / 15 MB** |
| peer work before it answers | merge a delta | apply EVERY row through MemoryStore: exact-question probe + trigram similarity + FTS triggers |
| measured wall time | sub-second | **47.5 s** (measured: `ssh hp 'gopi memory sync --serve-assets' < 15MB`) |

So the leg was killing a perfectly healthy transfer at 15 s, and
`exec.CommandContext` reports that kill as `signal: killed` with empty stderr —
the opaque line above. Reproduced: `gopi memory sync --peer hp` returns in
exactly **15.157 s** with that message, while `ssh hp echo` answers in 0.28 s
and the peer's own `--serve-assets` starts instantly (so it is neither an
unreachable peer nor the classic stale-peer binary, which the code already
detects and hints about separately).

The same 15 s bound was NOT the task channel's bug: a delta window genuinely
is small. The defect is one constant borrowed across two channels with
different payload classes.

## 2. The fix — a bound that separates "stalled" from "working"

1. **Its own value.** `memoryAssetSyncSSHTimeout = 15 * time.Minute` — sized to
   the work, not to the delta channel's window. Pinned by
   `TestAssetSyncBoundIsItsOwnValueAndOutlivesTheTaskLeg`, which FAILS if the
   value ever drops back to (or below) `syncSSHTimeout`.
2. **A knob**, because the corpus grows: `--timeout SECONDS` on `gopi memory
   sync`, and the same value through `GOPI_MEMORY_SYNC_TIMEOUT_SEC`. The flag
   publishes into the env channel rather than adding a second parameter, so
   **one reader** resolves the bound (`assetSyncSSHBound`, Rule 2; the name
   lives in `internal/config` — Rule 14 — and a malformed value is refused, not
   silently ignored, Rule 15).
3. **Dead ≠ slow.** The absolute bound is generous on purpose, so the leg must
   still notice a connection that has genuinely died (laptop suspended, wifi
   moved, peer rebooted): `ServerAliveInterval=60`/`ServerAliveCountMax=5` make
   ssh itself notice — a keepalive probe is answered by the peer's **sshd**,
   which stays responsive while the peer's gopi is busy applying rows, so a
   half-open connection surfaces as ssh's own "Timeout, server … not
   responding" in ~5 minutes while a legitimately long import is never
   mistaken for a dead one. An application-level idle watchdog was implemented
   first and then **removed**: on a large corpus the leg produces no stdout for
   minutes at a time (that is what applying 5209 rows looks like), so any
   liveness threshold that is short enough to catch a dead connection is also
   short enough to kill a healthy one — a false-positive machine. The absolute
   bound is the backstop; the keepalives are the detector.
4. **The error names the cause.** A context kill is now reported as "the asset
   session did not finish within 15m0s (bound elapsed …) — … raise the bound
   with `--timeout SECONDS`", instead of forwarding ssh's `signal: killed`.
   The stale-peer hint path is unchanged.

## 3. The one-way modes (`--push` / `--pull`)

The briefing's ask: a simpler manual push, symmetric between the hosts.

The assets protocol is ONE ssh leg that carries both directions — the peer
merges our patch and answers with its own export in the same breath. A
"direction" is therefore not a different transport, it is **which side of that
one answer this store acts on**:

* `--push` — copy our unique rows to the peer, and do NOT apply the peer's
  reply here. The report shows `pull{skipped=N}` so the operator can see what
  was deliberately not applied (a zero tally would read as "nothing came
  back").
* `--pull` — apply the peer's rows here, and send the peer **an empty patch**.
  The peer imports whatever stdin carries (it has no "do not import stdin"
  mode), so an empty patch is how "do not push" is expressed on the wire — the
  leg is the same single ssh session, but the outbound side carries nothing.
  Measured on the real pair BEFORE this correction: a nominal `peer1 --pull →
  hp` grew hp's ledger 5734 → 5735 with OUR rows, i.e. the flag wrote to the
  peer — not what it promises. After: hp 5735 → 5735, our freshly planted
  marker present on the peer 0 → 0. A side benefit: an empty patch gives the
  peer nothing to apply, so the peer takes no write lock at all — the same pull
  went from 54.3 s to **7.6 s**.
* neither — the existing bidirectional session, unchanged.
* both — refused; an ambiguous direction is an error, not a preference.

Both modes keep the unique-only contract exactly: identity is the exact
question, an existing row is never touched, nothing is ever overwritten or
duplicated, and a re-run after convergence imports ~0. The flags are admitted
strictly (`memoryFlagSet.expect`), so a typo like `--pus` is refused instead of
silently running a bidirectional session — the accepted-and-unread class this
repo has been bitten by before.

## 4. Verified where it matters

* Unit: bound resolution/override/refusal, the actionable timeout error, a
  hermetic refusal of an elapsed bound (a PATH-shadowed `ssh` that sleeps),
  direction admission (including the typo and the both-flags refusal), and
  flag↔env agreement.
* CLI E2E (real binary, two real DBs, ssh shim): `--push` delivers this node's
  unique row and does NOT pull the peer's back; a repeat does not duplicate;
  `--pull` brings the peer's unique row here.
* Real pair (this task's own run): `./bin/gopi memory sync --peer hp --push`
  from peer1 — see the task report for the measured tallies and the row counts
  on both hosts.

## 5. What the live round added (two mechanisms, one symptom)

After the bound fix, the same verb produced a DIFFERENT failure:

```
gopi memory sync: memsync assets: ssh hp: exit status 1
  (stderr: … import: begin transaction: database is locked (5) (SQLITE_BUSY))
```

**Mechanism 2 is not the bound.** The peer-side import applies the whole patch
in ONE transaction, so the peer's ledger is write-locked for the whole apply
(~40 s measured). Another gopi on the peer — a live `gopi chat`/`gopi worker` —
then waits on `dbBusyTimeoutMSec` (15 s) and fails. It is **contention, not
pathology**, and it is intermittent: the identical command succeeded on the next
attempt (measured: `39.2s` / `43.0s` ok, one run locked). A lock probe run
alongside showed a real writer holding the DB (`wait_ms=12756`, `17060`, and two
probes timing out at `20034`).

Telling the two apart is a one-line instrument: **compare the wall time to the
bound**. ≈15.1 s on a 15 s bound → the bound (mechanism 1); ~15.4 s but with a
REMOTE stderr line → SQLite contention (mechanism 2); the fix for 2 is to retry,
not to raise the bound (both mechanisms produce ~15 s only because the busy
timeout and the old bound happened to share the value — a coincidence worth
knowing, since raising `--timeout` cures neither).

The leg's real fix for 2 is out of scope here and should be its own task: the
asset import could commit in BATCHES (or take a shorter lock per N rows) instead
of holding one transaction for the whole corpus.

**A bound kill does not abort the remote handler.** Measured: HP's own (old)
binary was killed at 15 s, yet its rows still landed on peer1 45 s later — the
remote `--serve-assets` keeps importing after the local ssh dies. So "the
command failed" and "nothing happened" are different claims, and a retry after a
bound kill is safe (unique-only is idempotent) but NOT necessarily a no-op.

## 6. Not done (honest)

* **Periodic automation** (`--every 5m`, a serve-tick leg) is deliberately out
  of scope for this session — the briefing stages it after the manual path
  works, and the standalone-verb phase is what this change delivers.
* The peer's side of a push is the **same** `--serve-assets` handler: the peer
  cannot yet be told "do not keep what I sent you". Forgetting is not a goal
  here (unique-only is additive by design), but a future `--push --forget`-style
  mode would need a protocol field, not a flag on the initiator.
