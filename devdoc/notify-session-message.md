<!-- doc-class: normative -->
# `gopi notify --session <id> --message <text>` — the DELIVER face

**What this document is.** The contract of a NEW direction on an existing verb:
`gopi notify` used to read (the task-event stream: `events` / `failed` / `sink`);
it now also WRITES one message into a session. Everything below is standing
truth about master.

**Why the verb keeps its name.** The direction does not change — gopi is the
subject and the other party is the object ("gopi tells SOMEBODY that something
happened"). Only the object moved: an external sink → a session's own inbox.
A new verb would have been a second spelling of one semantic.

---

## Completed

- **`cmd/agent/cli_notify.go`** — the dispatch and the delivery body.
  - `parseNotifyArgs` tells the two hands apart by the SHAPE of the first
    token: a bare word is a sub-verb, a leading `-` is the deliver face
    (`parseNotifyDeliverArgs`). No sub-verb can begin with `-`, so
    `gopi notify --session X --message Y` never falls through to
    "unknown sub-verb".
  - BOTH flags are required and each ABSENCE is its own message: `--message`
    alone is missing a recipient, `--session` alone says nothing to send.
  - `resolveNotifySessionDir` accepts a session DIRECTORY (absolute path), the
    session's own directory NAME, or a session ID resolved by `gopilog.FindSession`
    (the reader every other session surface uses — exact id, never a prefix).
    An unresolvable session is an ERROR that names the search root.
  - Every refusal exits 1 with the flag named; the read sub-verbs
    (`events`/`failed`/`sink`) are dispatched and behave exactly as before.

- **`internal/tools/session_message.go`** — the durable half and the merge.
  - `SessionInboxDeliver(dir, sessionID, message, source)` appends ONE record to
    `<session-dir>/messages/inbox.jsonl` under an exclusive `flock` on the file
    itself.
  - `SessionInboxLoad(dir)` merges every not-yet-delivered record into
    `theJobs.notices` — the ONE queue — and appends a `delivered` marker in the
    same locked critical section.
  - Read with a JSON STREAM decoder (`json.Decoder` over a reader), never a line
    loop: a message carrying an escaped newline is ONE record.
  - Two named bounds: `sessionInboxTakeBudget` (how many messages ONE round
    boundary merges) and `sessionInboxRefuseBytes` (where the writer refuses,
    loudly, naming the file — nothing prunes the inbox automatically).

- **`internal/agent/session_inbox.go`** — `loadSessionInbox()`, which resolves
  the inbox from the path of the log the run is actually WRITING
  (`a.SessionLog.Path()`'s directory, the one fact about the session's location
  that cannot be stale) and merges it into the queue.

- **The three round boundaries** — `chat_ops.go` (tool-enabled chat turn),
  `internal/agent/loop.go`'s `runEngine` (task loop) and `internal/agent/loop.go`'s `Chat` (the NO-TOOLS chat
  turn, the default `gopi chat`). All three call `loadSessionInbox()`
  immediately BEFORE their `tools.TakeJobNotices()` drain and before the request
  is built.

- **`internal/agent/chat_job_notice_turn.go`** — one injection point,
  `injectJobNotice`: the notice goes into the conversation AND is mirrored into
  the session JSONL as a `user` event. The fact half is shared with the turn
  opener (`jobNoticeFact`), so the message the model reads and the row the record
  carries cannot drift.

- **`internal/tools/job.go`** — `JobNotice.External *ExternalMessage`: the
  external message rides the SAME notice shape, exactly one of the two halves
  populated.

- **`internal/sessionlayout/sessionlayout.go`** — `SessionMessagesDirName`
  ("messages"), the layout's own name for the inbox directory (Rule 14).

## Not Done (honest)

- **No wake-up of an IDLE prompt for an external message.** CLOSED by the
  follow-up, `devdoc/notify-session-message-mechanism.md`: the idle console now
  merges its own inbox while it waits and opens the next turn by itself, and the
  usage page says that instead. What follows is the state THIS document shipped
  with, kept as the record of why the gap was open rather than rewritten to
  match the newer behaviour.

  The epic's scope at the time was
  "the message enters the queue and is seen at the next round boundary". The
  console did NOT mint a turn for it, and the usage page said so. The wake
  primitive (`tools.NotifyJobNoticeWaiters`) IS called by the load, so a reader
  parked at an idle prompt is woken and re-checks its level — but the REPL's
  notice-turn arm (`chatNoticeTurnOwed`) was unchanged at that point, and whether an external
  message should mint a turn is a separate decision with its own trade-offs
  (it would answer unprompted, which is exactly what a job notice is allowed to
  do and an arbitrary message is not). **The follow-up took that trade-off**, and
  resolved it in the direction the operator asked for: a delivered message DOES
  answer unprompted, because that is what "become the next turn's request" means.
  The busy half is what survives of the caution — a turn in flight is never
  interrupted.
- **No automatic pruning of the inbox.** Nothing sweeps `inbox.jsonl`; the
  writer refuses at its ceiling and names the file. Building a sweeper would be
  a new persistent artifact with its own lifecycle (Rule 35), and there is no
  measured need yet.
- **No delivery acknowledgement or idempotency key from the caller.** Delivery
  is at-least-once at the CLI seam: a retry after an ambiguous failure appends a
  SECOND record (a new id). The read side makes the model see each message once,
  which is the property that matters; a caller-visible dedup key was not built.
- **The `--session` operand accepts a path or an id, not a title/summary.** An
  operator who knows their session by its task summary must look the id up
  (`gopi log --last`, `gopi task list`).

## Design decisions, with their reasons

### ① The take point stays UNIQUE

An external message is never a second queue. `SessionInboxLoad` appends into
`theJobs.notices` and returns; the take is the SAME `tools.TakeJobNotices`
(job.go) the background-job notices leave through, called two lines later at the
same boundary. Both boundaries therefore cannot disagree about what is pending.

**Evidence (self-filed) — the single take point, re-derived from the tree:**

```
$ grep -rn "TakeJobNotices" --include=*.go . | grep -v _test.go
internal/tools/job.go:312:func TakeJobNotices() []JobNotice {          ← the ONE definition
internal/agent/chat_ops.go:564:  for _, n := range tools.TakeJobNotices() {
internal/agent/loop.go:4253:     for _, n := range tools.TakeJobNotices() {
```

The same claim is a TEST, so it cannot rot:
`internal/agent/session_inbox_test.go` → `TestSessionInboxLoadHasOneTakePoint`
walks every production `.go` file under `internal/` and `cmd/`, asserts the
definition occurs exactly once (in `internal/tools/job.go`), that the call sites
are exactly those two files, and that EVERY file calling the take ALSO calls
`loadSessionInbox()` — a boundary that drained without loading would deliver the
message a round late at best, never at worst.

### ② The injection point, in code

`internal/agent/chat_ops.go` (tool-enabled chat turn), inside the round loop,
before the request is built:

```go
a.loadSessionInbox()
for _, n := range tools.TakeJobNotices() {
        tools.TaskDeliverableWorktreeJobForRow(a.autoTaskRowID, n.Command)
        a.injectJobNotice(n)
}
```

`internal/agent/loop.go` — `runEngine` (task loop) and `Chat` (no-tools chat
turn), the same pair at the same position. `injectJobNotice`
(`internal/agent/chat_job_notice_turn.go`) is the ONE place a notice reaches
both the conversation and the session record:

```go
func (a *Agent) injectJobNotice(n tools.JobNotice) {
        msg := jobNoticeInjectionMessage(n)
        a.Ctx.AddUserMessage(msg)
        if a.SessionLog != nil {
                a.SessionLog.LogUser(newUUIDv4(), msg)
        }
}
```

**Why the logging half is part of the injection**: an injected wire message that
is not in the session record is indistinguishable from a missing one — and for
an EXTERNAL message it is worse than untidy, because the record is the only
durable evidence that `gopi notify --session … --message …` reached the model at
all. The sender's own copy of the text is not evidence that anything was read.

**Why `internal/agent/loop.go`'s `Chat` was included**: a default `gopi chat` (no `--tools`)
runs `Agent.Chat`, not `ChatOps`. Wiring only `ChatOps` would have made the
feature exist and be unreachable on the default path — the failure Rule 31 is
about.

### ③ The read semantics: at-most-once injection, at-least-once delivery

- **The marker lives inside the same file**, as a SECOND record kind
  (`{"kind":"delivered","id":…}`) appended after the message — never an edit of
  the message's own line. A rewrite would need either truncate-in-place (a crash
  mid-rewrite corrupts everything already in the inbox) or write-then-rename
  (which invalidates every concurrent appender's descriptor, so two processes
  could hold the lock on two different inodes and lose an update). Appending has
  neither failure mode, and a torn last record is NAMED by the reader rather than
  silently counted as "no messages".
- **The state is on disk because it must outlive the reader**: the message has
  already been given to the model, and the session's history carries it, so a
  resumed session replays that history. A re-injection would be a duplicate
  instruction — worse than noise, because "run the tests" would run twice.
- **The residual window degrades in the safe direction.** If the process dies
  between the in-process merge and the marker's flush, that ONE message is given
  again on the next start (at-least-once). A repeated instruction is visible and
  recoverable; a silently dropped one is neither.
- **Why not a cursor / a DB / a state file**: a cursor in the process dies with
  the process; a database is a new store (Rule 35); a lock FILE is one more
  artifact. The inbox file is the record, the marker is a record in it, and the
  lock is `flock` on that same file — which has no stale-lock failure mode (the
  kernel releases it when the descriptor closes).

### ④ The refusals, and why each is its own message

`--session` without `--message` and `--message` without `--session` report
DIFFERENT sentences naming the flag that is missing: a single "both are
required" would leave the operator guessing which half the parser wanted.
An unknown session is refused by name with the root that was searched — a
message written into a directory that merely LOOKS like a session would be worse
than a refusal, because the sender would believe it was received. A profile with
no sessions root at all is the same refusal said with the path, never an opaque
`open(2)` failure.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0).
  `gofmt -l` clean on every touched file; `go vet` clean on the touched packages.
- **Tests**: `scripts/test-budget.sh -run 'TestNotify|TestSessionInbox|TestJobNotice|TestTakeJobNotices'
  ./internal/tools ./internal/agent ./cmd/agent ./internal/sessionlayout` →
  **4 packages, 0 failures, exit=0**, 82s wall (`stamp: 2026-10-06T06:49:05+08:00
  host=peerbox … exit=0 outcome=pass`); slowest case
  `TestNotifyDeliverReachesARunningChatSession` 30.12s. Re-run at close-out
  (`go test -run 'TestNotifyDeliverReachesARunningChatSession|TestNotifyDeliverRefuses'
  ./cmd/agent/`) → `ok github.com/nick/gopi/cmd/agent 39.773s`; the single-take-point
  source gate alone → `ok internal/agent 0.364s`.
- **Ran** (the surface itself, verbatim, against real state):
  - `gopi notify --session 11111111-2222-3333-4444-555555555555 --message
    "端到端验收: 沙箱内投递"` inside a `gopi sandbox` profile → exit 0,
    `queued: message b947d79c6d78f37f for session 11111111-…`, and the record on
    disk (`## Evidence` below quotes it). The read direction was regressed in the
    same session: `gopi notify sink` and `gopi notify events --limit 1` both
    exit 0.
  - a live `gopi chat --tools` under a mock model, addressed from a SEPARATE
    process (`TestNotifyDeliverReachesARunningChatSession`), and the same verb's
    three refusals as separate invocations.
- **Result**: the delivered text is on the model's wire AND in the console's own
  session JSONL; each refusal exits 1 naming its flag and writes nothing.

## Evidence (self-filed)

- `grep -rn "TakeJobNotices" --include=*.go . | grep -v _test.go` → one
  definition (`internal/tools/job.go:312`), two call sites
  (`internal/agent/chat_ops.go:564`, `internal/agent/loop.go:4253`) — the
  single-take-point claim, re-derived from the tree.
- **The real invocation and its on-disk result** (sandbox profile, verbatim).
  The trailing clause the command printed at the time read *"a running console
  does not wake for it; an idle prompt is never interrupted"* — that sentence
  is no longer what the verb prints (the follow-up,
  `devdoc/notify-session-message-mechanism.md`, changed it to the idle-wake
  claim), and it is quoted here as the state this document shipped with:

  ```
  $ SB=$(GOPI_HOME=/home/user/.gopi ./bin/gopi sandbox create --label notify-verify | tail -1)
  SB=/home/user/workspace/gopi-sandbox/sandbox-notify-verify-c600ddde
  $ GOPI_HOME="$SB" ./bin/gopi notify --session "$SB/.gopi/sessions/11111111-…-555555555555" \
        --message "端到端验收: 沙箱内投递"
  queued: message 40131371f91ff449 for session 11111111-2222-3333-4444-555555555555
  inbox:  …/sessions/11111111-2222-3333-4444-555555555555/messages/inbox.jsonl
  session dir: …/sessions/11111111-2222-3333-4444-555555555555 (resolved as its directory path)
  the session takes it at its next round boundary (a running console does not wake for it;
  an idle prompt is never interrupted)
  exit=0

  $ cat …/sessions/11111111-…-555555555555/messages/inbox.jsonl
  {"kind":"message","id":"40131371f91ff449","session_id":"11111111-…-555555555555",
   "message":"端到端验收: 沙箱内投递","source":"gopi notify --session",
   "created_at":"2026-10-05T23:04:34Z"}
  ```

  The record is the durable half, observed on disk in a sandbox profile that no
  console was running in — exactly the "survives with no running process"
  property, shown rather than argued. Both refusals (`--session` without
  `--message`, `--message` without `--session`) exit **1** in the same profile,
  and the three read sub-verbs (`sink`, `events --limit 1`, `failed --limit 1`)
  exit **0** — measured in one batch, after the deliver face landed.

- **The refusals** (each its own subprocess, exit 1, stdout empty):

  ```
  $ ./bin/gopi notify --session 11111111-…-555555555555        # no --message
  gopi notify: --message is required: --session names a session but says nothing to send it
               (the deliver form is `gopi notify --session <SESSION-ID> --message <TEXT>`)
  $ ./bin/gopi notify --session nope-none --message hi         # unknown session
  gopi notify: unknown session "nope-none" — no session record with that id under
               <root>/sessions (`gopi log --last` lists recent ones)
  ```

- `go test -run 'TestSessionInbox' ./internal/tools/` → `ok` (7 cases: durable
  record, merge into the one take point, delivered once, a newline inside a
  message is one record, a torn tail is named while the good messages still
  deliver, empty message refused, a burst bounded by `sessionInboxTakeBudget`).
- `go test -run 'TestSessionInbox' ./internal/agent/` → `ok` (the round-boundary
  case: on the wire AND in the session record; the NO-TOOLS `Agent.Chat` turn's
  case; the dedup case: the injected notice occurs exactly once in the second
  turn's request; the single-take-point source gate).
- `go test -run 'TestNotifyDeliverRefuses' ./cmd/agent/` → `ok` (three refusals,
  each naming its flag, no inbox created anywhere in the profile).
- `go test -run 'TestNotifyDeliverReachesARunningChatSession' ./cmd/agent/` →
  `ok` (37.0s) — the end-to-end closure.
- `go test -run 'TestNotify' ./cmd/agent/` → the read sub-verbs' own suite is
  green, unchanged.
- `./bin/gopi doccheck` → `the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row` (exit 0), after this document was classified in
  the same commit that lands it (Rule 36).
- **Two PRE-EXISTING gate reds, measured rather than assumed** (they are on
  master too, and this branch adds nothing to them; the fix is a different
  task's):

  ```
  $ go test -run 'TestStyleGate$' ./internal/stylegate/       # same hits on master
    internal/tools/tasks.go:154, internal/tools/tasks.go:4339 (ignored-error)
    internal/lineedit/probe_pty_test.go:213 (test-sleep)
  $ sha256sum vs `git show 2f4e5cc3:<file>` vs `git show master:<file>`
    internal/tools/tasks.go           base=f1d041a15b4d master=f1d041a15b4d worktree=f1d041a15b4d
    internal/lineedit/probe_pty_test.go base=2440c0b67b3f master=2440c0b67b3f worktree=2440c0b67b3f
    internal/stylegate/gate_test.go   base=2fc6b32db6f5 master=2fc6b32db6f5 worktree=2fc6b32db6f5
  ```

  Both my test files' own sleeps carry `//stylegate:allow-sleep <reason>` and
  the gate reports them no more (the run before that change named exactly the
  three sites above and this branch's two are absent from them).

- `COMMANDS.md`'s generated verb index was regenerated in this commit
  (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`) — the
  line number the index cites for `cmd/agent/cli_notify.go` moved when the
  deliver face landed, and `TestCommandsDocGeneratedBlockMatchesSource` is the
  check that requires the regeneration to be deliberate.


