<!-- doc-class: historical -->
# `gopi notify --session <id> --message <text>` — the IDLE WAKE mechanism

**What this document is.** The second half of a feature whose first half shipped
on master (`devdoc/notify-session-message.md`): the delivery path. That document's
`## Not Done (honest)` named the gap this one closes, in its own words — *"No
wake-up of an IDLE prompt for an external message … the REPL's notice-turn arm
(`chatNoticeTurnOwed`) is unchanged"* — and the verb's help said the same thing to
the operator: *"the console does NOT wake for it"*.

**The goal, restated as the operator stated it**: a message delivered by
`gopi notify --session <ID> --message "text"` must become the **next turn's
request** — the session opens the turn BY ITSELF, as if a person had typed it,
rather than sitting on the message until somebody happens to type something.

---

## Why the delivered half alone could not do it (the root cause)

The mechanism that carries a notice into a conversation already existed and was
correct: a LEVEL (`tools.JobNoticePending`, over the one notice slice) plus an
EDGE (`tools.JobNoticeWake`, a process-local channel). The REPL's idle `select`
already had the EDGE as an arm, so a job completing in THIS process wakes a parked
console.

What it could not see is a delivery made by **another process**. Two facts, and
both are needed to explain the observed behaviour:

1. **The level is only true once somebody LOADS the file.** `tools.TakeJobNotices`
   drains an in-process slice; the durable inbox
   (`<session-dir>/messages/inbox.jsonl`) becomes a notice ONLY when
   `agent.loadSessionInbox()` merges it in — and that call sat at the **round
   boundary**, a point reachable from *inside* a turn.
2. **A turn only starts when a line arrives.** So on an idle console nobody was
   at a round boundary, nobody merged the file, the level stayed false, and no arm
   of the parked `select` could ever fire: the EDGE had no producer for this case
   at all (the sender is a different process, which cannot signal this one's
   channels), and the LEVEL was false because nothing had looked.

The two failures compound into exactly the reported symptom: the message IS on
disk, the session IS running, and nothing happens.

## The fix, and why it is the smallest one available

Two additions, both reusing the existing level/edge pair rather than growing a
second one:

- **`Agent.SessionInboxIdleLoad`** (`internal/agent/session_inbox.go`) runs the
  SAME merge the round boundary runs (`loadSessionInbox`) while the console is
  idle, and signals a new **idle edge**
  (`agent.SessionInboxIdleWake`) when that made the level true.
- **`Agent.SessionInboxIdlePoll`** is the loop that calls it once per
  `sessionInboxIdleLoadInterval`; `cmd/agent/cli_chat.go` starts it beside the
  REPL and joins it on the way out, and adds ONE arm
  (`case <-agent.SessionInboxIdleWake():`) to each of the two parked `select`s.

Four properties were load-bearing, and each is a decision rather than an
implementation detail:

**① The take point does not move.** `SessionInboxIdleLoad` merges and returns; it
never drains. The notice still leaves through the single
`tools.TakeJobNotices` at the turn's own round boundary, which injects it "BEFORE
the request is built". `TestSessionInboxLoadHasOneTakePoint` (the source gate the
deliver face shipped) re-runs unchanged and still passes.

**② The wake is a NUDGE, never the decision.** Every receiving arm re-checks the
LEVEL through `chatNoticeTurnOwed()` and `continue`s when it is false — the
bounded-staleness contract `internal/tools/job_notice_wake.go` states. This is
also the FIRST thing the loop does on the way in (`if chatNoticeTurnOwed()`
before parking), so a delivery merged *before* the console parked still mints a
turn at the first prompt rather than waiting for an edge that already fired.

**③ The idle edge is its own channel, and that is not a second queue.** A channel
that carries no payload and is never a take point is not a message path; it is a
park-breaker. It cannot be `tools.JobNoticeWake` because the producer is a
different package AND a different concern (a cross-process delivery vs. the job
reaper). It is process-local, created by its own package's initialisation, never
written to disk, and needs no cleanup — the Rule 35 argument the tools-side
channel already makes, applied to the new producer.

**④ The busy case is untouched BY CONSTRUCTION.** The arms live only inside the
two `select`s that are reached with the loop at its prompt. A delivery arriving
mid-turn merges into the queue (the round boundary after the turn would have
taken it anyway) and the running turn is neither interrupted nor rewritten. This
is what makes the operator-facing sentence honest in both halves: an IDLE console
wakes; a turn in flight is never interrupted.

### The honest cost of the poll

`sessionInboxIdleLoadInterval = 2s`, one goroutine, and one `os.Stat` + tail read
per tick. On a session with no inbox the file does not exist, so
`SessionInboxLoad` returns after the stat alone. Nothing is written, no file
outside the session directory is touched, and no process is started: this is
**in-process**, which is why it is inside the Rule 35 boundary rather than outside
it — a cross-process signal (a socket, a watcher file, a daemon) would have been a
new persistent dependency with a lifecycle nobody owns.

The alternative that was considered and rejected: making the DELIVER verb signal
a running console. It cannot — the console has no address, and giving it one is
the dependency above. Polling the file the feature already owns is the whole of
the answer.

## What was deliberately NOT done

- **No wake for a session that is not running.** Nothing is running to wake; the
  message waits in the file and mints the FIRST turn of that session's next
  console (the console's poller does one load before its first tick, so this is
  not "wait up to 2s" — it is at the first prompt). The help page says exactly
  this rather than implying a console can be started.
- **No auto-pruning, no delivery acknowledgement, no second store.** Unchanged
  from the deliver face's own `## Not Done`, and none of them is needed by this
  half.
- **No new flag, no new verb, no new config key.** The wake is not optional: it is
  what the feature means. A knob here would be a way to have the feature and not
  have it.

## Verification

- **Build**: `go build ./...` → clean; `go vet ./internal/agent/ ./cmd/agent/` →
  clean.
- **Tests**: `make test-budget PKGS='./internal/agent ./cmd/agent ./internal/tools'`
  — results in the task report beside this document.
- **Two PRE-EXISTING reds, measured at a MASTER tree rather than assumed** (they
  are not this branch's, and neither names a file this branch touched):

  ```
  $ B=/dev/shm/gopi-idle-base2; mkdir -p $B
  $ git archive master | tar -x -C $B      # a master tree, no worktree (Rule 29)
  $ cd $B && ls cmd/agent/cli_notify_deliver_idle_test.go
  ls: cannot access '…cli_notify_deliver_idle_test.go': No such file or directory  # this branch is absent
  $ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
      docanchor_test.go:227: doc anchors verified: 2952 across 644 normative docs
        (240 pre-existing drift entries permitted, 16 new, 10 stale permits)
  FAIL
  $ go test ./cmd/agent/ -run TestChatStatus_LiveBlockKeepsTheStateWord -count=1
  FAIL
  ```

  The anchor gate's numbers are IDENTICAL to the in-worktree run that named it
  (2952/644/240/16/10), and none of its 16 "new drift" entries names
  `session_inbox.go`, `notify-session-message*`, `cli_notify.go`, `cli_chat.go`
  or `chat_notice_turn.go` — so the branch adds nothing to it and its fix is a
  different task's. The status-row red is likewise on master and is unrelated to
  the notice path (it is the live-block render of a long-running turn).
- **Ran** (Rule 31 — the surface itself, two real processes, verbatim):

  ```
  $ # terminal 1: a real console under a mock model, idle, stdin never typed into
  $ GOPI_HOME=$H bin/gopi chat --tools --no-editor
  $ # terminal 2: a SEPARATE process delivers; no line is written on terminal 1
  $ GOPI_HOME=$H bin/gopi notify --session <session-id> --message "NOTIFY-IDLE-…"
  queued: message 4f0c… for session <session-id>
  inbox:  $H/.gopi/sessions/<session-id>/messages/inbox.jsonl
  the session takes it at its next round boundary (an IDLE console wakes for it and opens the next turn by itself; a turn already in flight is never interrupted)
  exit=0
  ```

  The console then opened a turn with NOTHING typed, and the session record
  carries it — `session.jsonl:3` in the end-to-end case
  (`TestNotifyDeliverWakesAnIdleChatSessionWithNoInput`, 52.98s, PASS).
