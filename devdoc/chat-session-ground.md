<!-- doc-class: historical -->
# `gopi chat` does NOT run in a ground of its own — it reports where it writes

Epic: **chat-session-ground-retired-when-not-a-row** (2026-09-29). It SUPERSEDES
epic `chat-session-ground` (2026-09-27), whose delivery — every chat session got
its own linked worktree on branch `chat/<session-id>` — the operator retired.

## Why the creation path was retired

A chat is an interactive foreground session, and — in the superseded file's own
words from its first line — **"a chat is not a row"**. Nothing about a chat is
stamped in the ledger, and every reclaim arm in this tree is keyed on something
the ledger holds:

| arm | keying | why it cannot see a chat ground |
|---|---|---|
| finish-cleanup | `deliverable.branch` / `deliverable.worktree` on a ROW | a chat has no row |
| `scratch-gc` / sweep tick | a dead-holder ROW, or a scratch-home path | a chat ground is neither |
| worktree sweep | a registration's owning row, behind process-global all-or-nothing gates | reachable, but keeps everything while any unrelated run is live |

Measured consequence: the only non-`master` branches a chat left on this host
were exactly its `chat/*` refs — 0 commits ahead, no holder, and no arm that
would ever offer to remove them. The residue had a PRODUCER and no COLLECTOR.
`devdoc/chat-session-ground-lifecycle-diagnosis.md` is the diagnosis; this doc
records the remedy the operator chose: **delete the producer.**

That is the stronger guarantee, and it is deliberate: the residue cannot recur
because nothing mints it. Rule 35 is the same instinct — fix the thing, do not
wrap it in something that collects after it.

## What the code does now

| file | before | now |
|---|---|---|
| `internal/tools/chat_ground.go` | `ChatGroundFor` resolved a worktree root, derived a stem from the session id, created `<root>/gopi-<stem>` on `chat/<stem>`, reused an existing ground | a pure READ: it reports the directory the session stands in, and whether that directory is a repository's main checkout. No git verb, no root resolution, no stem, no creation |
| `cmd/agent/chat_ground.go` | `chatEnterGround` (resolve → announce → **`os.Chdir`** into the ground) + `chatLeaveGroundNote` | `chatGroundReport` (resolve → announce). **No chdir**, no exit note — there is nothing to enter and nothing left behind |
| `cmd/agent/cli_chat.go` | `chatEnterGround` before the first turn; `chatLeaveGroundNote` in `finishChat` | `chatGroundReport` before the first turn; the close-out says nothing about a ground |

### The one surviving contract: the report

Silence is the defect (Rule 15). A session whose writes land in a repository's
main checkout says so, in one line, on stderr, before the first turn:

```
[gopi] chat: this session writes in /path/to/checkout — the MAIN CHECKOUT of
/path/to/checkout. No worktree and no branch of its own: every read/write/edit/bash
round lands uncommitted in that checkout's working area.
```

Three shapes, all of them ANSWERS (never refusals):

- **in a main checkout** (or a subdirectory of it) — the line above;
- **in a directory that is not the main checkout** (a linked worktree the
  operator took deliberately, a nested repo) — the directory is named and said
  not to be the main checkout;
- **not inside a git work tree at all** — "no git checkout here, so there is no
  branch to attribute the work to".

### The FOURTH shape (2026-11-04): a REPLAY writes nothing, so the line says so

Every one of the three sentences above states a WRITE. Under `gopi chat --replay
<id>` there is none: internal/replaymode refuses the session mint, the slot
registration, the editor history and every tool round's effect, and the refusal
catalogue is printed beside this line. The line was written against a criterion
that does not cover that run — "is this directory a git checkout?" — so a run that
writes NOTHING was told where its writes land, two lines above a list saying every
write was short-circuited (measured: `gopi chat --replay` printed "this session
writes in /home/user/work/gopi — … every read/write/edit/bash round lands
uncommitted in that checkout's working area").

The fix is a DIFFERENT SENTENCE, not softer wording on the same one: the write
does happen in an ordinary chat and does not happen in a replay, so there is
nothing to hedge. `tools.ChatGroundReadOnlyAnnouncement` renders it for the three
same directory shapes, keeping every fact that stays true (the directory, the
checkout, the absence of a worktree and a branch) and dropping only the write
claim:

```
[gopi] chat: this session writes NOTHING in /path/to/checkout — a REPLAY re-runs a
recording read-only. No worktree and no branch of its own: it stands exactly where
it was started, in the MAIN CHECKOUT of /path/to/checkout, and leaves that checkout
untouched.
```

The criterion is `ChatGroundResolution.ReadOnly`, and it is set **inside the
producer**, by `tools.ChatGroundFor` (which wraps the filesystem-only
`chatGroundResolve`), from `replaymode.Active()` — the ONE reader of the flag
(Rule 13), so no renderer re-derives it and **no caller can forget it**. The
caller-side spelling (`cmd/agent/chat_ground.go` calling `replaymode.Active()` and
setting the field) was implemented first and rejected: the producer is the one
place every caller goes through, so a fact expressed at the call site leaves the
same false sentence live for every other caller. Measured, and it is why the
correction exists: `internal/tools/chat_ground.go` is the only producer of this
line, `cmd/agent/chat_ground.go` only prints what it returns. The degraded shape (the starting directory cannot be resolved) is
asked FIRST and is unchanged: a replay that cannot say where it stands still says
that. Both non-replay families are byte-identical to what they were; the red proof
is `go test ./cmd/agent/ -run TestChatGroundReplayNotice` against the pre-fix
producers (both cases FAIL, printing the old sentence).

Only ONE shape degrades (i.e. is reported as a failure to determine anything):
the starting directory cannot be resolved at all (`os.Getwd` fails) — gopi
cannot say where the writes will land, so it says that. A chat is a human's
session; there is no shape in which it is refused (the superseded epic's rule,
kept).

### Why the session does not move, and what that means for its writes

The file tools resolve a relative path against the PROCESS CWD
(`internal/tools/read.go`'s `filepath.Abs`; `write.go`'s `atomicWriteFile`), and
`bash.go` runs with the process cwd when no `workdir` argument is given. With the
chdir gone, those all resolve in the directory the operator started the chat in —
which is what "no ground of its own" means, stated in the notice. The write is
therefore UNTRACKED work in that checkout; if the operator wants a branch to
attribute it to, taking a worktree is now their deliberate act, not this
session's.

## What was NOT changed (deliberate)

- **No new collector here.** Old `chat/*` branches and their worktrees on this
  (or any) machine are a SIBLING task's business — the one that reclaims existing
  grounds. Nothing here sweeps them, and no cleanup code was added by this
  branch. **That sibling has since landed on `master`**
  (`internal/tools/worktree_sweep_chat.go`, the sweep's ④C rung, epic
  `chat-ground-reclaim`): it collects an already-existing INERT ground left by an
  OLDER gopi. The two halves compose — the collector serves the residue, and the
  producer that used to create it is what this change deletes.
- **No new dependency** (Rule 35): no daemon, no unit, no cron line, no state
  file, no config key. This change REMOVES a producer; it adds nothing to the
  machine.
- **`ChatGroundBranchPrefix` is kept, and it is not a live namespace.** Its
  declaration carries the reason, and the reason is now concrete: the collector
  on `master` reads it as its ONLY classifier (`worktreeSweepChatBranchPrefix`),
  because a chat ground's DIRECTORY is indistinguishable from a task ground's.
  Nothing in this tree mints it any more — the comment says so, and a test
  asserts a chat leaves no `chat/*` ref.
- **The ledger is untouched**: no row, no stamp, no chat-table write beyond what
  already existed.

## Verification

- `go test ./internal/tools/ -run TestChatGround` — nothing is minted (branch
  list, worktree list and the worktree root are byte-identical before/after); the
  caller's own directory is reported, with `InMainCheckout` telling a checkout
  from a worktree; a non-repo directory is an answer, not a degradation; only an
  unresolvable directory degrades; the notices name the directory.
- `go test ./testsuite/ -run TestChatGround_` — the REAL binary, a REAL fixture
  repo, one tool round: git gained no branch (and no `chat/*`), no worktree was
  registered, the pinned worktree root is empty, the turn's relative-path write
  landed in the caller's directory, and stderr said so.
- **Red-proof for the assertion that is not a tautology** (the "no branch
  gained" half is vacuously satisfied by code that never had a producer):
  re-adding a `git worktree add -b chat/<stem>` call to `ChatGroundFor` makes
  `TestChatGroundCreatesNothingAndReportsTheMainCheckout` fail by name on the
  branch-list and worktree-root comparisons — that is the check firing, not a
  test that would pass on any tree.
- Live evidence (Rule 31), outside the test harness, in the session directory:
  `~/.gopi/sessions/01a0eaca-c2b3-7d67-836b-bf671b6fd326/evidence/run_live_chat.sh`.

## Not done / honestly out of scope

- **The existing `chat/*` residue on this machine is NOT reclaimed here.**
  `git -C /home/user/work/GoPi branch --list 'chat/*'` reads empty at the time of
  writing, but the diagnosis doc records six elsewhere; the sibling task owns
  them.
- **Rule 37's "why" is not written here either.** It belongs to the sibling task
  (a chat does not commit; it dispatches), and this doc deliberately does not
  pre-empt it.
- **The `chat/*` prefix constant is now write-less.** It survives only for the
  sibling's collector; when no host carries such a ref, it should go too.
