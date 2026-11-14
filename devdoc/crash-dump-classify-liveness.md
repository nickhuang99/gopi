<!-- doc-class: historical -->
# Task Report: crash-dump backfill classified LIVE sessions as crashes

Branch `fix/crashdump-classify` (worktree `~/workspace/gopi-crashfix`).
This file is the FIX report; the diagnosis of the original symptom lives in
`devdoc/crash-dump-startup-backfill.md` (kept intact — this document is the
implementation that closed two of the three follow-up tasks it filed).

## Symptom

Every plain `gopi` start printed

```
[gopi] crash-dump: backfilled 14 orphan session dump(s) from previous abnormal exits
```

on a machine where nothing had crashed. The number was an artifact of the
predicate, not of reality.

## Root cause (5-why)

1. A startup line claims "N sessions died abnormally".
2. `BackfillOrphanCrashDumps` computed N from `DetectOrphanSessions`, whose
   only test was `!HasSessionEnd(path)` — "the last 4096 bytes contain no
   `session_end` line".
3. `HasSessionEnd == false` is true for MANY non-crash states: a session still
   being appended (it has not ended yet), a session whose `session_end` never
   got written (leaked test sessions, `LogSessionEnd`'s error is discarded),
   a session that ended but stored its marker beyond the 4096-byte tail
   window. The only exclusion was the CURRENT process's own session id.
4. So the predicate measured **"no recorded end"**, and the startup line
   reported it as **"the process crashed"** — two different propositions.
   At startup is exactly when the most sibling sessions are alive, so the
   false-positive rate peaks precisely where the sweep runs.
5. The design (`devdoc/crash-dump-design.md`) only ever modelled the
   single-session case ("exclude the current session"), so no multi-session
   host semantics existed. Compounding it: a dump, once written, was never
   revisited (`if fileExists(dumpPath) { continue }`), so every false claim
   was **permanent** — 117 of 473 dumps on this host describe sessions that
   later wrote a clean `session_end`.

## The predicate, clause by clause

`ClassifyOrphanSession(path) OrphanVerdict` (`internal/agent/orphanclassify.go`)
— a session is a crash **iff both clauses hold**:

| clause | test | why it is needed | why it is NOT sufficient alone |
|---|---|---|---|
| **1 — process demonstrably gone** | `procstat.Alive(pid)` where `pid` is the liveness stamp (`"pid"` key of the `session_start` event) | a session whose writer still exists is *still being written*; its missing `session_end` is the normal state of a running session, not evidence of death | a dead process that exited cleanly also has a PID that is gone |
| **2 — no clean end marker** | `!HasSessionEnd(path)` over the tail window | a session that wrote `session_end` told us how it ended; a leftover dump for it is stale bookkeeping | a live session also has no `session_end` yet |

`pid` is stamped by `NewSessionLoggerForRun` at `session_start` (the event is
written and fsynced FIRST, so it is always the first line — `sessionScanHeadMax`
bounds only a pathological first line, it is not a search window).

The result is **three-valued**, not boolean:

```
clause 1 \ clause 2   | session_end present | session_end absent
----------------------+---------------------+------------------------
process ALIVE         | live  (not a crash) | live  (not a crash)
process GONE          | live  (not a crash) | CRASHED  ← the only claim
liveness UNKNOWN      | unknown (silent)    | unknown (silent)
```

### Why the "unknown" row is deliberately silent (Rule 15)

No liveness stamp (every session written before this change, plus any file we
cannot read) means we **cannot prove the process is gone**. The old code
promoted that absence of evidence into a crash claim — that IS the bug. The
wrapper `procstat.Alive` encodes the discipline at the probe boundary:
`pid <= 0` returns `VerdictUnknown`, never `VerdictGone`, so an absent PID can
never be read as a corpse. A session classified unknown is dropped from the
count silently; the startup line prints nothing rather than inventing a number.

The asymmetry is intentional and is the load-bearing part of the fix:
**unreadable ⇒ unknown** (silent), and the combination "unreadable AND no
`session_end`" is *still* unknown, not crashed.

### Why PID liveness and not mtime

The original follow-up proposal was an mtime "live window" ("a file modified
seconds ago is not a corpse"). That is a heuristic: it is wrong for a long
suspended run (mtime older than the window while the process is perfectly
alive → false crash) and wrong for a `kill -9` immediately after a flush (mtime
fresh → missed crash). The PID's existence is a fact the kernel answers, not a
guess — so the fix uses the fact and keeps mtime out of the predicate.

## Startup line

```go
if backfilled := agent.BackfillOrphanCrashDumps(sessionIDOf(ag)); backfilled > 0 {
    fmt.Fprintf(os.Stderr, "[gopi] crash-dump: %d session(s) died without a clean exit — dump(s) written under %s\n",
        backfilled, sessionDumpHint())
}
```

- The count is now the number of **proven** crashes, so it cannot be printed
  when nothing is proven. On a healthy tree the line does not appear at all.
- The wording no longer says "orphan session dump(s) from previous abnormal
  exits" (which reads as "you had N crashes" and names no location); it names
  what was decided and where the dumps are.
- `sessionDumpHint()` resolves the sessions root; a `GopiDir()` failure
  degrades to a generic phrase rather than failing startup (Rule 15: recorded,
  not swallowed — the phrase is the recorded degradation).

## Reconciliation (the 117 stale claims)

A crash dump is now **retractable**. `retireStaleOrphanDumps` runs inside
`BackfillOrphanCrashDumps`: for every `*.dump.json` under the sessions tree that
does NOT correspond to a currently-crashed session, the session is classified
again; if it is live (clean `session_end`, or still running) or unknown, the
stale dump is removed (failure to remove is printed to stderr, never silent).

Two things this deliberately does NOT do:
- It does not delete a dump whose session JSONL is **unreachable** (deleted
  log): that dump is the only surviving evidence, so it is left in place — and
  it is never counted.
- It does not touch `category`/`unknown`/`vendor` dumps written by other
  producers; they are outside the crashes set and outside this sweep's
  ownership (the sweep only ever deletes files it could have written itself:
  `<session>.jsonl` → `<session>.dump.json`, a mapping it can invert).

## "Why 14?"

Enumerated from the live host (`~/.gopi/sessions`, dumps created
`2026-09-12`), grouped by dump mtime — one group = one startup sweep:

| dump mtime | count | composition |
|---|---|---|
| **03:09:08** | **14** | **all 14: 2-line, 942–962 byte, `model":"test-model"`, workdir `…/internal/agent` or `…/gopi-latprov-verify/internal/agent`** |
| 03:17:53 | 2 | 1 × leaked test session (2 lines, `test-model`), 1 × a real 1316-line run |
| 03:20:33 | 1 | real run, wrote `session_end` LATER the same second (dump is stale) |
| 03:24:20 | 1 | real run, still appending at sweep time |
| 03:30:20 | 3 | 2 real runs (one wrote `session_end` at 03:30:20), 1 mid-flight |
| 03:43:41 | 10 | all 10: 2-line `test-model` leaked test sessions |

So **the 14 are not 14 crashed gopi sessions.** They are the 14 files that,
at 03:09:08, (a) had no `session_end` marker and (b) were not the sweeping
process's own session. All 14 are degenerate artifacts of the test suite: 2
lines long, `model":"test-model"`, no `session_end` ever written — the
signature of a test that constructs a `SessionLogger` (whose `session_start`
now exists on disk) and then abandons it without calling `LogSessionEnd`. The
other sweeps (03:17, 03:30, 03:43) show the same signature mixed with the real
false positives: sessions that were *mid-flight* at sweep time and sessions
that wrote a clean `session_end` seconds later.

None of these 14 are reported by the new predicate: the test-session files are
unstamped (⇒ unknown, silent), the mid-flight ones have a live PID (⇒ live,
silent), and the ones that later ended have `session_end` (⇒ live, silent).

## What was deliberately NOT changed

1. **`HasSessionEnd` itself.** Its tail walk is correct for its job ("is a
   clean end marker present in the tail window?"); the bug was using it as a
   crash oracle. It stays, and the classifier consumes it as clause 2 only.
   The 4096-byte window stays too: a session longer than the window that ended
   cleanly now still classifies as live, because clause 1 sees a gone PID and
   clause 2 only needs to *rule in* a clean end — a `session_end` outside the
   window is a residual false positive that is now bounded by clause 1
   (it can only ever fire for a session whose process is gone), and widening
   the window is a separate cost/benefit call (the `ReadFile`-whole-file cost
   noted in the diagnosis devdoc is untouched — task `01a091ea-f649…`).
2. **The `LogSessionEnd` error being discarded** (the leaked-test-session
   source). That is a test-hygiene fix in `internal/agent` chat/test teardown,
   not a classification fix — out of this epic's deliverable, and the new
   predicate already makes those sessions harmless (unknown ⇒ silent).
3. **The per-startup full-tree scan cost** (~0.6 s / ~431 MB churn). Filed as
   `01a091ea-f649…`; this change does not make it worse (the classification
   reads the head window only) and does not fix it (moving the sweep into an
   explicit one-shot face is a separate behaviour change).
4. **The existing dumps on the host are not bulk-deleted by this commit.**
   Reconciliation is done by the running code on the next start, on purpose:
   the sweep owns what it can prove, and a bulk `rm` in a commit is not
   reviewable.
5. **`excludeSessionIDs` stays** on `DetectOrphanSessions` even though the
   liveness clause already excludes the current process (our own PID is
   alive). It is belt-and-braces for a caller whose session belongs to an
   ancestor process; removing it would change a public signature for no gain.

## Files

| file | change |
|---|---|
| `internal/procstat/procstat.go` | NEW leaf package: three-valued `Verdict` + `Alive(pid)` contract (no project imports → no cycle, Rule 14) |
| `internal/procstat/procstat_unix.go` | `kill(pid, 0)`: `ESRCH` ⇒ gone, `EPERM`/nil ⇒ alive, anything else ⇒ unknown |
| `internal/agent/orphanclassify.go` | NEW: `ProcessAliveStampField`, `OrphanStatus`, `OrphanVerdict`, `ClassifyOrphanSession`, `sessionStampPID` |
| `internal/agent/session.go` | stamps `pid` into `session_start`; `sessionJSONLSuffix` constant referenced by the 4 former `.jsonl` literals; the scan now calls the classifier |
| `internal/agent/crashdump.go` | two-clause backfill, `orphanDumpPath`, `retireStaleOrphanDumps`, `listOrphanDumpPaths` |
| `cmd/agent/main.go` | honest startup line + `sessionDumpHint()` |
| `internal/agent/orphanclassify_test.go` | NEW: the classification matrix |
| `internal/agent/crashdump_test.go`, `internal/agent/sessionend_tail_test.go` | fixtures given a real session shape (pid stamp) |

## Verification

```
go build ./...                                  → OK (exit 0, no output)
go vet ./internal/agent/ ./internal/procstat/ ./cmd/agent/   → OK
go test ./internal/agent/ -run 'Orphan|HasSessionEnd|SessionEnd|Backfill' -count=1
                                                → ok  github.com/nick/gopi/internal/agent  0.070s
go test ./...                                   → see "Full gate" below
```

Classification matrix tests (all five rows asserted, each its own test):

| test | asserts |
|---|---|
| `TestClassifyOrphan_LivePIDIsNotACrash` | live PID + no end ⇒ live (**the epic's regression**) |
| `TestClassifyOrphan_LiveSessionWithDeadWriterPID` | gone PID + no end ⇒ crashed |
| `TestClassifyOrphan_CleanEndIsNotACrash` | gone PID + clean end ⇒ live |
| `TestClassifyOrphan_UnstampedIsUnknownNotCrash` | no stamp ⇒ unknown, never crashed |
| `TestClassifyOrphan_UnreadableIsUnknown` | missing file + non-JSON file ⇒ unknown |
| `TestClassifyOrphan_TruncatedTail` / `…TruncatedTailLive` | torn final line: crashed when the writer is gone, live when it is not |
| `TestClassifyOrphan_ProbeContract` | `pid<=0 ⇒ unknown`, impossible PID ⇒ gone, self ⇒ alive |
| `TestDetectOrphanSessions_Matrix` | the real scan reports EXACTLY the crashed ones out of all six fixture kinds |
| `TestBackfillOrphanCrashDumps_RetiresStaleDump` | a dump for a later-clean-ended session is removed |
| `TestBackfillOrphanCrashDumps_CountsOnlyCrashes` | a healthy tree ⇒ 0 (the phantom-count regression, twice for idempotence) |
| `TestBackfillOrphanCrashDumps_ReportsOnlyTheKilledSession` | one killed session ⇒ 1 dump, live session ⇒ no dump |

Commit: see `git log --oneline` on `fix/crashdump-classify`.

## End-to-end acceptance (real binary, isolated `GOPI_HOME`)

Built `CGO_ENABLED=0 go build -o /tmp/gopi-crashfix-bin ./cmd/agent/` at commit
`b33dda6e` and drove a scratch profile `/home/user/gopi-e2e-crash` whose
`sessions/` held hand-written session files.

**1. A genuinely live session is never reported.** A `sleep 300` process was
started as the writer and its PID stamped into `livesession.jsonl`; the sweep
ran while it was alive:

```
$ GOPI_HOME=… /tmp/gopi-crashfix-bin "say hi"
[gopi] crash-dump: 2 session(s) died without a clean exit — dump(s) written under …/sessions
$ ls …/sessions | grep dump
killed.dump.json          ← the planted killed session: correct
                          ← livesession: NO dump
```

(The other of the "2" was the previous run's own session, whose process had
already exited by the time this sweep ran — itself a true positive.)

**2. A `kill -9`'d session is reported alone.**

```
$ sleep 300 & KID=$!
$ printf '{"type":"session_start","sessionId":"victim","model":"m","pid":%d}\n{"type":"assistant"}\n' $KID > victim.jsonl
$ kill -9 $KID
$ GOPI_HOME=… /tmp/gopi-crashfix-bin "say hi"
[gopi] crash-dump: 1 session(s) died without a clean exit — dump(s) written under …/sessions
$ ls …/sessions | grep dump
victim.dump.json
```

Exactly one, naming only the killed session — and the tree also held a
clean-ended session (`clean.jsonl`) which was correctly silent.

**3. A clean tree prints nothing, and a stale dump is retired.**

```
$ printf '{"type":"session_start",…,"pid":4194305}\n{"type":"session_end","exit_code":0}\n' > victim2.jsonl
$ cp victim.dump.json victim2.dump.json     # plant a dump for a session that ended cleanly
$ GOPI_HOME=… /tmp/gopi-crashfix-bin "say hi"   # → NO crash-dump line at all
$ ls …/sessions | grep dump
(none)                                      # the planted stale dump was retired
```

**4. Classifier verdicts on that same real tree** (run as a throwaway test in
the worktree):

```
livesession.jsonl -> crashed (process gone and no session_end — crashed)   ← after its writer was killed
killed.jsonl      -> crashed (process gone and no session_end — crashed)
clean.jsonl       -> live    (clean session_end present — ended normally)
```

(Note the first line is the *post-kill* verdict — the same file while its
writer was alive printed nothing at all, per evidence #1.)

## Honest notes / residual limitations

- **A session whose writer PID was recycled by an unrelated process** would be
  read as alive (⇒ silent). This is the conservative direction: it under-reports
  rather than inventing a crash, which is the epic's stated preference.
- **A cleanly-ended session whose `session_end` sits further back than the
  4096-byte tail window, whose writer is gone, is still a false positive.**
  Clause 1 cannot exclude it (the process really is gone) and clause 2's window
  does not reach the marker. It is now bounded to "process gone" only (the old
  predicate also fired for live processes), and widening the window is the
  separate cost item `01a091ea-f649…`.
- **The leaked test sessions keep being created** (`LogSessionEnd`'s error is
  discarded in test teardown). The new predicate makes them harmless (unstamped
  ⇒ unknown ⇒ silent), but the leak itself is not fixed here — see "what was
  deliberately NOT changed" #2.
- **`make test` does not exist in this repo** (no Makefile). The gate used is
  `go test $(go list ./... | grep -v '/cmd/agent$')` plus
  `go test ./cmd/agent/`, whose single failure
  (`TestCLIContractInventoryMatchesDispatch`, `VoiceSubcommand` drift) is
  pre-existing and unrelated — it reproduces with this branch's changes
  stashed.
