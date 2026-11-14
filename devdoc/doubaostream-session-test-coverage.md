<!-- doc-class: normative -->
# `internal/doubaostream` — the live Session's tests (a PORT from a superseded branch)

Task: `01a11dc9-befc-7c56-9bc8-669cdb853780` · Branch `task/01a11dc9befc7c569bc8669cdb853780`
· Master at port time: `823305a17` (this worktree's base)

> **THIS IS A PORT, NOT A NEW TEST.** Source: branch
> `task/01a11b5d48887a4a9b13ff18a2c83673`, file `internal/doubaostream/session_test.go`
> at commit `96a2f812f` — eight real-loopback assertions that branch measured green.
> That branch's `Session` (same name, different type: `Push`/`End`/`Wait` +
> `SessionOptions`) was declared REDUNDANT against master's landed implementation
> and is kept as a ref, not merged (task `01a11b5d`, memory record #8838). What
> survived of its work is the gap it found: master's `session.go` is 477 lines with
> **no test inside the package** — `cmd/agent`'s coverage drives a scripted stand-in,
> so the schedule properties of the live arm had nobody verifying them.

## 1. What landed

`internal/doubaostream/session_test.go` (new, ~940 lines), containing:

- **The fake server, kept whole** (as the brief requires): `asrFakeServer`, a real
  `net/http` handler that hijacks the connection and speaks the vendor's frame
  shapes; `readClientFrame` (masked client framing); `writeServerFrame` (unmasked
  server framing); and the header constants **derived** from production
  (`headerFullClientRequest[1] >> msgTypeShift`, `headerLastAudio[1] & 0x0f`,
  `fakeResponseHeader` built from `headerFullClientRequest`), so a change to the
  protocol's own bytes cannot leave the server speaking a vocabulary nothing sends.
- **Nine test functions** covering eight ported properties (the credential case is a
  3-row table), each rewritten against the API master actually has.
- **The `awaitServerClose` bounded-grace fix**, ported with its measured reason.

## 2. Why the assertions could not simply be copied

Master's live `Session` is a **different design** from the source branch's, and
`session.go`'s own header states the difference — ported verbatim:

> a FILE is sent against a CLOCK the client owns … a LIVE microphone IS the clock.
> Samples arrive in real time by construction, so there is nothing to pace and no
> way to run ahead — each packet is written the moment its audio was captured.

So the source branch's headline assertion — *"packets go out on their own clock
while the caller pushes nothing"* — is **false by design here**: master has no
sender goroutine, because `Say` carries the audio and there is nothing to pace.
Copying it would have produced a test of the branch's architecture, run against a
different one. Every assertion was therefore re-derived; the table records what
each one became and why.

| # | source assertion (branch `01a11b5d`) | ported assertion (master) | what changed, and why |
|---|---|---|---|
| 1 | "packets go out with an EMPTY caller" — the self-pacing sender | `TestSessionSendsExactlyWhatTheCallerSaid` | **Inverted on purpose.** The property now asserted is `packets == Says + the end-of-stream packet`: a session that sat on the caller's audio waiting for its own tick, or one that manufactured a packet the caller never gave it, both fail. `AudioMS` is checked against the same reading. |
| 2 | OnPartial fires while audio is still going | `TestSessionPushesPartialWhileAudioIsStillGoing` | Kept; re-timed. The source sat on a `40ms` sleep per push; this one waits on the **server's receipt** of the i-th packet (`awaitAudioPackets`), because `Say` is synchronous on the wire here — so the observation is deterministic, not a race against a clock. Also asserts `PartialCount()` agrees with the callback, and the `Index` sequence. |
| 3 | End flushes the pending tail, last packet flagged | `TestFinishFlushesTheTailAndFlagsIt` | Kept. The "tail is not padded" reading is now taken from **gunzipped PCM**, not the frame length (see §4 — a frame's length is a *compressed* length, so the source form of this check is not a comparison of audio at all). |
| 4 | empty-transcript verdict | `TestFinishReportsTheServicesOwnVerdictOnSpeechlessAudio` | **Reachability changed.** The source called a free function (`emptyTranscriptVerdict`); master renders the verdict from *inside* `finishResult`, so the port drives a whole session (Open+Say+Finish) against a server answering `CodeNoSpeech`, and requires the `*NoSpeechError` out of `Finish`. |
| 4b | (same source case, other half) | `TestFinishRefusesAnEmptyTranscriptWithNoCode` | The half a "did it error?" test cannot see: empty text + `codeSuccess` must be an `*APIError` naming the reason, **not** a no-speech verdict and **not** an empty string handed back as success. |
| 5 | Push refuses a full backlog | `TestSessionRefusesAudioAfterFinishAndAfterClose` | **Property replaced.** There is no client-side backlog bound in this design (`sessionPendingBound` does not exist here; `Say` writes immediately and reports a transport failure). What this design *does* guarantee — with two exported sentinels, `ErrSessionClosed` / `ErrSessionFinished` — is that a session refuses audio by NAME in each of its two terminal states, through one predicate (`stateErrLocked`) so `Say` and `Finish` cannot disagree. That is what the port asserts. |
| 6 | Close is idempotent; Push after it is refused | `TestSessionRefusesAudioAfterFinishAndAfterClose` + `TestSessionCloseIsIdempotent` + **`awaitServerClose`** | Kept, and this is the case that carried the source branch's **measured flake fix**: the assertion on the server's view of the close is a bounded grace poll, not the next line. See §3. |
| 7 | Open refuses missing credentials before dialing | `TestOpenRefusesMissingCredentialsBeforeDialing` | Kept, strengthened: a 3-row table (none / token-only / app-id-only) and an added assertion that the refusal is **not** an `*APIError` — a missing credential is a caller mistake, and it must not be shaped like a wire failure. |
| 8 | refused handshake carries the log id | `TestOpenReportsARefusedHandshakeWithTheLogID` | Kept; the port also pins `Op == "connect"` (which step failed) and that the caller's `ReqID` survives a handshake that never opened. This is the one case that keeps its own raw listener: it needs a 403, which the fake server cannot produce. |

## 3. The `awaitServerClose` fix, ported with its measurement

The source branch measured this case **red 3 of 5 runs under load (loadavg 7.06, 4
cores), 0 of 5 idle**, each red printing *"the server never saw the connection
close"*. It was not a product bug: the handler runs on its own goroutine and writes
the flag when **its own read fails**, so a snapshot taken on the line after
`Close()` returns is a statement about the scheduler.

The port keeps the remedy — `awaitServerClose` polls under a 5 s bounded grace and
reports what it saw when the grace runs out — and **re-derives the flag's meaning**,
which the source did not: the fake server here also terminates its dialog on the
client's own goodbye frame (the vendor's real terminator timing), so a session that
finishes *normally* would leave `closed` false forever if the flag were written only
inside a failed read. The handler therefore records its end from a `defer` — the
moment it stopped owning the connection — and the assertion is about **the server's
view of the connection**, on either exit path, still polled rather than read one line
later. Two distinct things were conflated in one boolean; the port separates them by
making the flag honest.

## 4. The defect this port found (reported, not fixed)

**`Session.Finish` closes the socket without setting `closed`, so a `Close()` after
`Finish` really re-closes the socket and reports the error it caused.**

```go
// session.go — Finish
_ = s.conn.Close()
s.awaitReader()
return s.finishResult()
// session.go — Close
s.mu.Lock(); if s.closed { ...return nil }; s.closed = true; s.mu.Unlock()
err := s.conn.Close()   // second close on the same fd → "use of closed network connection"
```

- **Observed**: `session.go:802: Close after Finish: close tcp 127.0.0.1:37620->127.0.0.1:37915: use of closed network connection`.
- **Reached by the real caller**: `cmd/agent/chat_voice_live.go:249` calls
  `runVoiceLiveSession`, whose shared body (`cmd/agent/voice_stream_runtime.go`) defers
  `_ = sess.Close()` at line 191 and calls `sess.Finish(tail)` at line 237 — the ordinary
  path is `Finish` then the deferred `Close`, which is exactly the sequence above. The error
  is discarded there, so the user-visible effect today is **nil**; the contract is
  still wrong, and `Close`'s refusal to be idempotent after `Finish` is what makes it
  so.
- **Not fixed here, and not asserted**: the brief for this task is a test port, and
  no product code may be changed to make a test convenient. The test therefore does
  **not** assert this error as expected behaviour (a test that did would have to go
  red again the day it is fixed); the line carries a comment naming the defect and
  pointing here. Filed separately as bug task `01a11dfe-615f-7491-863a-2333f6c45325`.

**Re-confirmed on the MERGED tree, after master's `e0220ca9d`.** When this branch's
delivery-point sync landed, master had moved two commits ahead and both touch this
package (the mid-frame desync fix: `client.go`, `session.go`, `ws.go` +
`session_finish_drain_test.go`, `ws_read_deadline_test.go`, `live_voice_drill_test.go`).
After merging, the defect was re-measured rather than assumed: `Finish` in the NEW
`session.go` still closes the socket (`_ = s.conn.Close()`) without setting `closed`,
and `Close`'s guard is still `if s.closed`. A temporary repro on the merged tree
(deleted, not committed) prints the same failure:

```
zz_repro_close_test.go:20: REPRO: Close after Finish: close tcp 127.0.0.1:58328->127.0.0.1:43551: use of closed network connection
```

So the filed bug is against the current master tip of this file, not a stale copy.
The ported suite itself stays green on the merged tree (`ok … 28.762s`).

## 4b. What the merge changed in this port

Master's newer commits are the **same subject** this port's fake server exercises —
the mid-frame read deadline — so the merge made the boundary explicit rather than
overlapping:

- Master's new tests use a `socketPair` of REAL sockets and drive the session
  **without the handshake** (`liveTestSession`), i.e. at the frame level: a deadline
  landing mid-frame. Their subject is the *desync* mechanism.
- This suite uses the **loopback WebSocket server** and drives the session through
  its **public API** (`Open`/`Say`/`Finish`/`Close`): its subject is the *schedule*
  (when a partial reaches the caller, what teardown reports, which frames went out).
  The two meet at exactly one place, and both are needed to say the thing either
  alone cannot: a desync is *reported*, **and** the frame stream that produced it was
  the one the caller actually sent.

`devdoc/doubaostream-read-deadline-frame-desync.md` (master's) covers the mechanism;
this document covers the API-level schedule and the port. No content is duplicated.

**A second, smaller finding, in the port itself** (not a product defect, recorded so
nobody repeats it): a packet's frame length is a **compressed** length. Two packets
of visibly different audio can be the same few bytes on the wire, so the source
branch's *"the tail was padded"* check — comparing the last packet's frame length to
the first's — passes for the wrong reason. The port gunzips the body and compares
PCM. Note this is a **false-negative** shape, not a false positive: it would silently
stop testing anything the day a change made both packets compress alike.

## 5. Files

- `internal/doubaostream/session_test.go` — new (the port).
- `devdoc/doubaostream-session-test-coverage.md` — this document, plus its
  `devdoc/DOC-INDEX.tsv` row (in the same commit, Rule 36).

No product file was modified. `git status --porcelain` in the worktree prints only
the two new files before the commit.

## 6. Not Done (honest)

- **The source branch's self-pacing sender has no ported test, because it has no
  counterpart here.** That is §2 row 1, stated rather than hidden: if master ever
  gains a sender goroutine, the assertion to port is the source's, and it is still
  on disk at `task/01a11b5d48887a4a9b13ff18a2c83673:internal/doubaostream/session_test.go`.
- **No live-vendor run.** Every assertion here drives the loopback fake; the real
  endpoint was not exercised by this task (it needs credentials, and the properties
  under test are schedule properties that the loopback reproduces faithfully). The
  live path's evidence belongs to the tasks that own it.
- **`Options.ChunkMillis` is passed but unread by `Open`.** `sessionOptions` sets it
  so a session driven by these tests carries the same shape a caller would, but the
  live arm does not use it (`Say` carries the audio). Not a defect claim — noted so
  the next reader does not read the field as load-bearing here.
- **The defect in §4 is NOT fixed** by this task, by design. It is filed.

## 7. Verification

- **Build**: `go build ./internal/doubaostream/` → OK; `go vet ./internal/doubaostream/` → clean; `gofmt -l internal/doubaostream/session_test.go` → prints nothing.
- **Tests**: `make test-budget PKGS='./internal/doubaostream'` → `exit=0 outcome=pass`,
  26s wall, at load 5.21 6.63 6.07 (4 cores), **before** the delivery-point sync:

  ```
  stamp: 2026-10-09T08:10:22+08:00 host=peerbox pkg=./internal/doubaostream budget=25m elapsed=26s \
         load_start=5.21 6.63 6.07 load_end=5.18 6.55 6.05 cores=4 exit=0 outcome=pass \
         stream=run-1791504622-3698192.json
  ```

  And again on the **merged** tree (master's mid-frame desync work included), which is
  the reading that counts:

  ```
  stamp: 2026-10-09T08:14:43+08:00 host=peerbox pkg=./internal/doubaostream budget=25m elapsed=43s \
         load_start=4.65 4.97 5.48 load_end=4.36 4.86 5.43 cores=4 exit=0 outcome=pass \
         stream=run-1791504883-3705588.json
  ```
- **Ran** (Rule 31) — the nine cases, verbatim:
  `go test ./internal/doubaostream/ -run 'TestSession|TestFinish|TestOpen' -count=1 -v` → **9/9 PASS** (`ok github.com/nick/gopi/internal/doubaostream 11.601s`).
- **Result** — red→green, two assertions, each a temporary mutation of `session.go`
  in this worktree (restored byte-identically afterwards; sha256 of the restored
  file equals the pre-mutation hash):
  - **A** — `onPartial` stops calling the caller's callback (the mutation is in the
    production path, so nothing is being simulated): `TestSessionPushesPartialWhileAudioIsStillGoing`
    → `FAIL … OnPartial fired 0 times while the caller was still sending audio, want 6 (5 packets + the handshake's own response)`.
  - **B** — `stateErrLocked` drops both named refusals: `TestSessionRefusesAudioAfterFinishAndAfterClose`
    → `FAIL … Say after Finish gave streaming asr: send-audio: … use of closed network connection …, want ErrSessionFinished` (three failures).
  - Restored: `sha256sum internal/doubaostream/session.go` → `67501281ae04…` = the pre-mutation value; `git status --porcelain` → only the new untracked test file.
- **Result — repeat**: `-count=5` on the same scope → `ok`, no flaky case. Load at
  the time: see the evidence file (Rule 27: the load figure is **observed**, never
  manufactured — nothing in this task generates load).
- **Commit**: see the report's `Commit:` line.

## 8. Evidence (self-filed)

Directory: `/home/user/.gopi/sessions/01a11dc9-befc-7c56-9bc8-669cdb853780/evidence/`

- `session.go.orig`, `session_test.go.orig` — the pre-mutation copies, with their
  sha256 in the sha lines of `red-A.txt`… (see the report).
- `red-A.txt`, `red-B.txt` — the red outputs quoted above.
- `count5.txt` — the repeat run with the load figure at its start and end.
- `stamp.txt` — the `make test-budget` stamp.

## 9. Follow-up: the reported defect is FIXED (task 01a11dfe, 2026-10-09)

§ "The defect this port found" above is the RECORD of what this port observed and
deliberately did NOT fix: `Finish` closed the socket without setting the flag
`Close` guarded on, so the live arm's own teardown sequence (`Finish`, then the
deferred `sess.Close()`) re-closed the same descriptor and reported
`use of closed network connection`. That section is left as written — it is what
was measured then, and rewriting it would destroy the record.

It is now fixed on `task/01a11dfe615f7491863a2333f6c45325`: the socket's release
is claimed exactly once (`Session.releaseSocket`) and shared by both paths, so
`Close` after `Finish` returns nil while `closed` keeps its original meaning.
`internal/doubaostream/session_test.go`'s parked line — the one that said the
return value was "NOT ASSERTED HERE" because a test would have to go red the day
the defect was fixed — now asserts the contract, i.e. the predicted red is
exactly what happened. The fix, its red→green measurement and the regression
tests are in `devdoc/doubaostream-session-teardown.md`.
