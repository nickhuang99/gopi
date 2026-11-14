<!-- doc-class: historical -->
# Rework: `task list` display honesty — the two verify defects closed

Branch: **`fix/task-list-state-honesty`**
Tip: `718bcbe2` (+ this report's commit) on the current master line (`8380ce9c`).
Recovery: `git show fix/task-list-state-honesty:devdoc/task-list-state-honesty-rework-20260913.md`
Original fix: `70eece5a` · merge `aad25579` · revert `d4512e18` · verification `01de5d70`
(independent verification doc: `devdoc/task-list-state-honesty-verification-20260912.md`)

## 1. Why this is a rework, not a re-merge

The fix was merged (`aad25579`, parents `01c8b838` + `70eece5a`) and reverted
(`d4512e18`, `-m 1`) after the post-merge verify child judged the MERGED state
FAILED. The merger log records the verdict verbatim — it is a **substantive**
failure (two reproduced defects on this machine's own ledger rows), not a wait-cap
kill: the verify child was a full rework of the deliverable's own test suite, and
the revert message names both defects and the exact root causes.

Two consequences shaped the recovery:

1. **`git merge master` at `70eece5a` is a FAST-FORWARD to master.** Master
   descends from `70eece5a` through the merge *and* the revert, so merging master
   into the recreated branch moved HEAD to `8380ce9c` and silently DELETED the fix
   content (`internal/tools/taskstate.go` became ABSENT while `70eece5a` stayed an
   ancestor). The branch reflog makes it explicit:
   `8380ce9c fix/task-list-state-honesty@{0}: merge master: Fast-forward`.
2. **The content had to be re-applied, not merged.** Recovery =
   `git reset --hard 8380ce9c` then `git revert -n d4512e18` (revert of the
   revert) → commit `49db2fc7`. No force-push, no history rewrite; `70eece5a`
   remains an ancestor of the branch.

## 2. The two defects and how they are closed

### DEFECT 1 — own-host dead pid reported as "another host" (false reason)

Observed on the live ledger (row `01a09573`, `claimed_by=peerbox:2881717`, hostname
`peerbox`, `/proc/2881717` absent): `unknown: claim stamp names another host`.

The old fallback in `taskClaimUnverifiableReason` conflated two *different*
situations that both reach it:

| Situation | Old render | Correct render |
|---|---|---|
| stamp minted on another machine (pid unknowable here) | unknown (foreign host) | unknown (foreign host) |
| stamp names THIS host, pid probed, probe voted DEAD | unknown (foreign host) ✘ | died ✔ |

The second case is a decidable kill/crash, and it is the realistic shape of a
worker row that dies. Knock-on: because `claimStampFreshness` returns `stale` only
for a *previous* claimant, a killed current claimant keeps a FRESH stamp — so the
old ladder's escape hatches never fired and **"died" was unreachable for any
claimed row**, while the fix's own died fixture used a *claimless* row.

**Fix.** New helper, referenced from the ladder and from the reason function:

```go
func taskClaimAttributable(claimedBy string, aliveChecked bool) bool {
	return aliveChecked && strings.TrimSpace(claimedBy) != ""
}
```

`aliveChecked` is the claim layer's own report that it accepted the stamp and
probed the pid on this host. The ladder now reads (first match wins):

```go
case aliveChecked && alive:                                    → running
case st.SessionEnd && st.WorktreeGone:                         → finished-but-unrecorded (+ worktree gone)
case !taskClaimAttributable(t.ClaimedBy, aliveChecked):        → unknown (<claim reason>)   // NEW position
case aliveChecked && !alive:                                   → died   (worktree still present ⇒ unknown)
case st.SessionTailKnown && !st.SessionEnd:                    → died   (worktree still present ⇒ unknown)
case st.SessionEnd && thresholdKnown && !heartbeatStale:       → unknown (heartbeat inside the stall grace)
case !thresholdKnown:                                          → unknown (stall grace unavailable)
default:                                                       → unknown (<claim|session reason>)
```

`taskClaimUnverifiableReason` is now documented as reachable only for stamps this
host never probed, so its three reasons can no longer be recycled for a decidable row.

### DEFECT 2 — a foreign-host row rendered "died"

With a READABLE, silent session the old ladder's session-only branch fired
(`!aliveChecked && SessionTailKnown && !SessionEnd → died`) **above** any claim
inspection — asserting death for a process this host cannot see, contradicting the
fix's own three-times-stated promise. The new `!taskClaimAttributable` branch sits
above it, so an unattributable claim is settled first and the label names the
missing signal instead.

### Third defect found while writing the tests: the worktree facts were session-gated

`taskProbeRowState` returned early on an empty `SessionID` **before** probing the
row's worktree, so every sessionless row silently lost the "recoverable work on
disk" fact — exactly the shape that now reaches the death verdict on claim
evidence alone. The worktree probe now runs first; the empty-session-id
short-circuit only skips the session layers (its documented purpose).

## 3. Regression fixtures (proved through the production entry point)

`internal/tools/taskstate_rework_test.go` — every case calls
`taskInProgressStateNow` / `taskStateSuffix` (the real renderer), **no stub on the
pid layer** wherever the case is the pid layer's own vote, and every case asserts
its FIXTURE is in the intended shape (dead pid really dead; session really
resolves and really carries no `session_end`; stamp really fresh; no worktree
recorded) before asserting the label — the verification report's own trap (a probe
that does not assert its fixture is as untrustworthy as a test that does not assert
the state).

| Test | Pins |
|---|---|
| `TestReworkDefect1OwnHostDeadPidIsNotForeign` | own-host + dead pid + FRESH stamp → `died`, never the foreign-host reason (the live row `01a09573` shape) |
| `TestReworkDefect1OwnHostDeadPidWithReadableSession` | same, session readable and silent → `died` (and the rendered suffix carries it) |
| `TestReworkDefect1ForeignHostIsUnknown` | foreign stamp + readable silent session → `unknown (claim stamp names another host)` |
| `TestReworkDefect2ForeignHostReadableSessionIsNeverDied` | the defect-2 repro: never `died`, never a false claim about the session — unknown + foreign-host reason |
| `TestReworkClaimedDiedDistinguishableFromFinishedAndRunning` | the epic's core table on CLAIMED rows: running / finished-but-unrecorded / died are three distinct labels, none of them `unknown` |
| `TestReworkReasonVocabularyNamesTheMissingSignal` | no claim / previous claimant / foreign host each name their own missing signal; malformed stamps are never `died` |
| `TestReworkOwnHostLivePidButForeignCommIsUnknown` | the pid-reuse shape the original suite dodged: the probe voted ⇒ never the foreign-host reason, never `running` (see §5 for the honest residency) |
| `TestReworkWorktreeHeldWithDeadHolderIsUnknown` | dead holder + INTACT worktree ⇒ death withheld, reason names the worktree |

The original suite was updated to the new authority rather than weakened:
`TestTaskInProgressStateDistinctLabels`'s died fixture is now a CLAIMED row with an
own-host dead pid (the shape "died" exists for); `TestTaskInProgressStateHonestyBoundaries`
pins the claim layer's precedence (stale stamp, foreign stamp) and keeps the
heartbeat and worktree hatches pinned on rows the claim layer cannot attribute.

## 4. RED → GREEN evidence (production path, stub-free)

Before the fix (`/tmp/listhonest-red-proof.txt`):

```
--- FAIL: TestReworkDefect1OwnHostDeadPidIsNotForeign
    DEFECT 1: own-host dead pid rendered "unknown: claim stamp names another host" — the stamp names THIS host (peerbox:1073741824), the pid-probe voted DEAD
--- FAIL: TestReworkDefect1ForeignHostIsUnknown
    foreign-host row with a readable silent session = "died", want "unknown: claim stamp names another host"
--- FAIL: TestReworkDefect2ForeignHostReadableSessionIsNeverDied
    DEFECT 2: foreign-host row rendered "died" from session evidence gathered on this host
--- FAIL: TestReworkWorktreeHeldWithDeadHolderIsUnknown
    dead holder + intact worktree = "unknown: claim stamp names another host", want "unknown: worktree still present, session did not end"
```

After the fix: `go test -count=1 -run 'TestRework' ./internal/tools/` → **ok**.

## 5. Honest boundaries and gaps

- **The unattributable-pid residency.** For a LIVE pid whose `/proc/<pid>/comm` is
  unreadable, `pidAlive` resolves EPERM to *alive*, the comm guard then refuses the
  name, and the ladder's dead vote fires ⇒ `died`. A live-but-unattributable pid is
  therefore indistinguishable from a recycled one **by the evidence the probe
  gathers**. The rework does not invent a new state for it (no new status
  vocabulary is allowed); the test pins the invariant that matters (never a false
  "another host", never `running` for a pid we cannot name) and documents the
  residency.
- **`aliveChecked` is the ladder's only authority for "is this stamp ours".** It
  deliberately does not re-parse the stamp, so the ladder and the pid layer can
  never disagree about which shapes are local.
- **No live-ledger end-to-end run** in this session; the defects are proved through
  the production code path in tests, encoding the verify report's live rows.
- **Pre-existing reds** are classified by the master differential (§6), never
  claimed green.

## 6. Verification

| Gate | Command | Result |
|---|---|---|
| Build | `CGO_ENABLED=0 go build ./...` | exit 0 |
| Build (binary) | `CGO_ENABLED=0 go build -o /tmp/listhonest-gopi ./cmd/agent/` | exit 0 |
| gofmt | `gofmt -l internal/tools/` | empty |
| vet | `go vet ./internal/tools/` | clean |
| Targeted | `go test -count=1 -run 'TaskState\|TaskListRunning\|Running\|TaskInProgress\|TestRework' ./internal/tools/` | ok |
| Display-only | `git grep -nE 'db\.\|Exec(\|INSERT\|UPDATE\|WriteFile\|os\.Remove' -- internal/tools/taskstate.go` | empty |
| No new status enum | no `taskStatusX = …` added | confirmed |
| ff-mergeable | `git merge-base --is-ancestor master fix/task-list-state-honesty` | exit 0 |
| No rewrite | `git merge-base --is-ancestor 70eece5a HEAD` | true |
| Full-tree differential | branch tree vs control worktree `/dev/shm/listhonest-control` (un-merged master `8380ce9c`), failing names compared with `comm` | see below |

Differential result (failing test names, branch minus control) — the acceptance is
"no NEW failing names", not "all green":

<!-- DIFFERENTIAL -->
