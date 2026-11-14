<!-- doc-class: historical -->
# The write-end release repair was ALREADY on master — the landing check, and the check itself

**Task**: `01a0fba4-c517-7f40-95f2-bb83ac837cb5` · **Branch**:
`task/01a0fba4c5177f4095f2bb83ac837cb5`
**Question this file answers**: "master:124-127 still `t.Fatalf`s on ANY errno — re-implement
the write-end verdict repair on a fresh branch." By the time this run started, that
premise had already been repaired on master by a sibling task.

## The finding, and the measurement behind it

This run was filed at **16:04:34** from the merge conflict of
`task/01a0fb7294db7ea8a0956bbf7dffaf92`. The epic's premise was TRUE of this branch's
base — `git show a53b8e60:cmd/agent/chat_argv_test.go | sed -n '122,128p'` prints the
old shape:

```go
if err := outW.Close(); err != nil {
	t.Fatalf("close stdout writer: %v", err)
}
if err := errW.Close(); err != nil {
	t.Fatalf("close stderr writer: %v", err)
}
```

It became FALSE of `master` at **16:29:05**, when the sibling task
`01a0fb95-3dd5-7930-8a18-169e43845b2d` landed as commit `7b1ab284`
("the chat argv fixture's WRITE end is landed on this branch, re-measured"), merged by
`616df259`. The repair that landed is not merely equivalent to this run's — it is
**byte-identical to the archived attempt's**, which is a stronger statement than the
earlier devdoc's ("its sibling `-race` capture is on master already"):

```
$ git rev-parse master:cmd/agent/chat_argv_test.go
13ba724c45fc2bc137a1a7e19b052a68e1cd0525
$ git rev-parse rework-backup/01a0fb7294db7ea8a0956bbf7dffaf92:cmd/agent/chat_argv_test.go
13ba724c45fc2bc137a1a7e19b052a68e1cd0525
$ git rev-parse master:devdoc/chat-argv-write-end-release-attribution.md
bc8890ffc638a27e6176918bb281b7b9f9ca11f3
$ git rev-parse rework-backup/01a0fb7294db7ea8a0956bbf7dffaf92:devdoc/chat-argv-write-end-release-attribution.md
bc8890ffc638a27e6176918bb281b7b9f9ca11f3
```

Both blobs are equal, and `git log --oneline master --not a53b8e60 -- cmd/agent/chat_argv_test.go`
names exactly one commit — `7b1ab284`. Master's `cmd/agent/chat_argv_test.go` carries
the classifier-driven write end (`chatRunArgsWriteEndClosed`, fd captured BEFORE the
release) and the guard `TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal`
at line 492.

## What this run therefore did NOT do, and why that is the correct outcome

It did **not** re-implement the repair a second time on top of a tree that already has
it: that would produce a branch whose code delta against master is comment wording
only, and a duplicate of an existing devdoc. Rule 26's duplicate-dispatch allowance is
for **repair**; here there is nothing left to repair, and the honest move is to say so
with the measurement above rather than to manufacture a second diff.

- **Delete-eligible, now proved by content rather than by inference**: the tag
  `rework-backup/01a0fb7294db7ea8a0956bbf7dffaf92` points at `d6c2eaf4`, whose two
  artifacts are the two blobs master already carries. Nothing in it is now unique.
- **The guard is on master, not only on the archived attempt** — verified by running
  it, below, rather than by reading the file listing.

## Verification

Ran against **master's own** landed file (this worktree's tree after the delivery-point
sync, which took master's version of the conflicted file verbatim):

```
$ go test -run '^TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal$' ./cmd/agent/ -v
--- PASS: TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal (0.00s)
PASS
suite child sweep: live child processes: 0 (settled 1s)
suite child sweep: live child processes: 0 (guard measured live child processes: 0 earlier; live child processes: 0 at close-out)
suite goroutine sweep: parked repository goroutines: 0
ok  	github.com/nick/gopi/cmd/agent	3.601s
MASTER_GUARD_RC=0
```

Equality of the synced tree with master, measured rather than assumed:

```
$ git diff --cached --stat master      # (staged merge tree, conflicts resolved to master's side)
                                       # → prints nothing
```

### Scope checks on the delivered tree (tip `655ad116`)

```
$ go build ./...                      → BUILD_RC=0
$ go vet ./...                        → VET_RC=0
$ go test -run '^TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal$' ./cmd/agent/ -v
                                      → ok github.com/nick/gopi/cmd/agent, guard PASS, parked repository goroutines: 0
$ make test-budget PKGS='./cmd/agent'
                                      → exit=0 outcome=pass, elapsed=500s, 1 finished / 0 with a failure
                                        (stamp 2026-10-02T16:48:49+08:00, stream run-1790930929-1031480.json)
$ git status --porcelain              → prints nothing (Rule 30)
```

Four EARLIER budget runs at the same tip each failed on exactly one test, a different
one each time (`TestPrefetchGoroutineSettleGapDetectsALiveLeak`,
`TestWorkerStartedOnAPtyWritesItsLifecycleToTheDeclaredFile`) — both files
blob-identical to master's, both passing in isolation 5×5 (`ok … 43.861s`, rc=0).
They measure a global process/scheduler observable against suite-load timing:
pre-existing and load-sensitive, not this branch's delta. Left in the evidence
(`evidence/testbudget.log`, `testbudget-rerun.log`, `testbudget-run3.log`,
`isolation.log`) rather than dropped.

## Not Done (honest)

- **This run produced no red arm of its own, and does not claim one.** The guard cannot
  be red on master's current tree (it is the landing commit's own test), and on the
  pre-repair tree the guard does not exist at all — so the only honest red measurement
  is the landing commit's, recorded in
  `devdoc/chat-argv-write-end-release-attribution.md`: the pre-fix call restored inside
  the guard's body, `chat_argv_test.go:518: close stdout writer: close |1: bad file
  descriptor`, rc=1. That arm is **manufactured by construction** in that doc too, and
  it says so: the fixture's own write-end close answers a non-nil errno only when another
  path already freed the descriptor number, which no healthy run produces. This file
  repeats the number rather than upgrading it into an observation.
- **The epic's own acceptance could not be satisfied as written** — "RED on master's
  pre-repair tree" describes a tree that has not existed since 16:29:05. The premise is
  reported as measured, not worked around by restoring the pre-fix line to manufacture a
  red.
- The open half of the family is unchanged and still open: **the suite-wide releaser that
  frees a descriptor number it does not own is not named** anywhere
  (`devdoc/chat-argv-fixture-release.md`, "Not covered").
