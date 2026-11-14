<!-- doc-class: historical -->
# The cmd/agent red set: how a fixture drifts from the code, and the differential that says whose red it is

Task `01a12c5b-df55-703b-9a8d-a942a4891b6c`, 2026-10-12. Three tests in
`cmd/agent` were RED at HEAD `96a4320f2` with every one of the production paths
they cover behaving exactly as promised. This doc is the mechanism, so the next
reader recognises the shape instead of re-deriving it.

## Why it matters

**A red test is a claim, and the claim can be wrong.** "The suite is red" was
read for a while as "the product is broken". Measured, it was three fixtures
that had drifted — each in a different way — plus one host-dependent anchor.
Nothing in the product needed fixing, and the two failure modes look identical
from the outside: a non-zero exit.

The rule this repo already carries covers the response (Rule 41: observe, then
file; the finder does not have to explain it). What this doc adds is the
**classification step**: before you "fix" a red, say which of three things it is
— a product defect, a test anchor that drifted, or a property of the host —
because the three have three different correct outcomes.

## The three drift shapes (each measured here)

### 1. A fixture that re-applies a predicate its reader already applied

`internal/testutil` split the user rows into two views: `UserContents()` = the
OPERATOR's turns (harness rows excluded) and `HarnessUserContents()` = the
injections. The A4 census fixture looped `UserContents()` and then tested each
element with `sessionevent.IsHarnessInjected` — the very predicate its reader had
just used to exclude them. The "injected" half could only ever be empty, so the
arm compared **0 stamps against a record's 2 requests** while the one-stamp-per-
request rule held perfectly.

**The tell**: an assertion whose two sides are always `0` and a constant. If a
count-based relation can be satisfied by `0 == 0`, it is not measuring anything —
which is why this run's version also refuses `requests == 0` outright (Rule 15:
silence is not a result).

### 2. An equality that a later, deliberate feature made unsatisfiable

`TestRunPathPlainPromptIsVerbatim` asserted the first user message **is** the
prompt. Twelve days later `211f7dcec` shipped the session-budget turn stamp,
which `requestMessages` appends **after** the prompt — a documented signal, on
purpose, in a commit that does not touch the fixture. The RP-1 property (nothing
prepended) still held; equality could no longer see it.

**The tell**: an `==`/`!=` on a string that a pipe or a decorator also writes to.
**The repair must strengthen, not weaken**: the anchor is `HasPrefix` (strictly
stronger than the `Contains` a hurried fix reaches for), and the "unframed" scan
reads EVERY message's concatenation rather than the first alone, so reordering
the frame into a later message still fails.

### 3. A fixture passing the runner's own identity where the case needs a known-bad one

The daemon-scope arm handed `os.Getpid()` to `daemonScopeMeasureLines` for a case
whose SUBJECT is the cgroup-**unreadable** degradation. That makes the arm's
verdict a property of whoever runs the suite:

```
$ cat /proc/self/cgroup
0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.gnome.Terminal.slice/vte-spawn-<uuid>.scope
```

`cgroupscope.InSessionScope` requires the path's PARENT to be `user-<uid>.slice`;
here it is `app-org.gnome.Terminal.slice`, so the predicate answers false, the
seam takes its middle branch and returns `""`, and the fixture reads `0 line(s)`
while the code is right. The file ALREADY had the correct input for exactly this
arm — `dsNoPrivUnreadablePID`, used by the sibling "escaped daemon is silent"
case three lines below.

**The tell**: `os.Getpid()` (or `os.Getenv`, the wall clock, the hostname) inside
an assertion. An arm whose outcome depends on the runner cannot be red for a
reason about the code.

### 4. The same shape one level up: an anchor that encodes ONE mechanism

`TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange` demanded `PPID == 1`.
The seam chooses among three rungs and on this host takes rung ①
(`systemd-run --user --scope`), where the transient service manager starts the
daemon: a daemon that has genuinely left the login-session scope has PPID = that
manager (**measured 3100 = `/lib/systemd/systemd --user`**), never 1.

Rule 24 already says it: **process independence is judged by `/proc/<pid>/cgroup`,
never by PPID or SID.** The sibling case had already written the trap down in
full ("PPID and SID are NOT evidence … BOTH fail against a daemon that detached
the STRONGEST way") — this arm had simply not been brought along.

**The repair**: branch on the mechanism the daemon carries in its OWN environment
stamp, and for rung ① assert the pair that rung actually produces — the live
cgroup has left the session scope **and** names the scope unit the daemon was
started under. That is a STRONGER statement than `PPID == 1` could make, which is
why this is a repair and not a relaxation.

## The differential: whose red is it?

Saying "this branch doesn't touch that file" is an argument. A control tree is a
measurement:

```bash
mkdir -p /home/user/workspace/gopi-ctl-<task>
git archive origin/master | tar -x -C /home/user/workspace/gopi-ctl-<task>
cd /home/user/workspace/gopi-ctl-<task>
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u GOPI_WORKER_LOCK_FD \
  GOPI_IGNORE_PEAK_HOURS=1 go test ./cmd/agent/ -count=1 -run '<the failing test>' -v
```

A plain `git archive` directory, **not** a `git worktree add --detach`: a
worktree is registered with the main repo, the sweep can recycle it, and a
detached HEAD in somebody's worktree is the coupling Rule 29 and Rule 6 exist to
avoid.

Measured here: the same message, the same `PPID=3100`, on a tree containing none
of this branch. That is what turns "inherited" from a hypothesis into a fact —
and it is exactly what the epic's briefing got wrong at the other end, claiming
master was 175 commits ahead when `git merge-base HEAD origin/master` WAS master's
tip (`acc3fa3eb`). **A briefing's premise is a claim like any other; measure it.**

## Reusable recipes

- **Classify before you fix**: product defect / drifted anchor / host property.
  Three outcomes, three different repairs.
- **A count relation needs a nonzero guard** (`requests == 0` → `Fatalf`), or
  `0 == 0` passes as a green.
- **Never pass your own pid/env/clock into a case that is about bad input** — a
  fixture that asked the host a question has answered it about the host.
- **A detach/independence verdict reads the cgroup, never PPID or SID** (Rule 24);
  when an anchor must encode a mechanism, read the mechanism the code stamped.
- **Attribute with a control tree** (`git archive <the integration branch>`), never
  with a file list alone: the file list argues, the differential measures.
- **`git rev-parse --verify refs/…` exit 1 says the REF is gone, not the work** —
  check `git log --all --grep=<branch>` / the peer's merge commits before
  concluding a branch never existed.
