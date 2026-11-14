<!-- doc-class: historical -->
# `task sync --task-id` on the session faces — RE-LAND AUDIT: the fix was already on master

**Run**: re-land attempt (worktree `~/workspace/gopi-sync-taskid-refuse`, branch
`fix/sync-taskid-session-refuse`, off master `8c316cf6`), 2026-09-23.
**Verdict of the audit: no re-land was needed — the original fix is an ancestor of master
and is intact. This run added no code; it RE-VERIFIED the fix on the real binary and
records the result here so a third dispatch does not repeat the cycle.**

## What the re-land premise claimed, and what the repo said

The briefing said a worktree reset had destroyed the original branch, and asked for the
fix to be re-derived from a task-report attachment + memory. Both sources are real, but
the premise was false:

```
$ git merge-base --is-ancestor 1097152a HEAD   # 1097152a = the original fix commit
→ exit 0: 1097152a IS an ancestor of master (8c316cf6)
```

That single command is the whole check, and it should have run **before** a worktree was
created — `git log --oneline -3 -- <the file>` answers it too. The commit landed on master
via the merge union lane; the *branch* is gone, the *fix* is not. (Rule 15's quieter
sibling, one level up: a destroyed branch is not a destroyed commit, and "the branch is
missing" was read as "the work is missing".)

## The re-land is byte-identical to the landed fix

Every deliverable named by the epic already exists on master, unchanged since `1097152a`:

| deliverable | state on master |
|---|---|
| `cmd/agent/cli_tasks_sync.go` — `syncSessionFaceSpelling`, `syncTaskIDSessionRefusal`, 9 flag constants, `syncUsageHead` sentence | present; **zero diff** `1097152a..HEAD` in the guard region |
| `cmd/agent/cli_tasks.go` — `taskIDFlagName` + refusal call before the composition checks; admission row + parse assignments on the constants | present; call at `cli_tasks.go:1077` |
| `cmd/agent/cli_tasks_sync_taskid_surface_test.go` — 9-face matrix, accept half, resolver completeness, real-binary leg | present; **zero diff** `1097152a..HEAD` |
| `devdoc/sync-taskid-session-refuse.md` + `devdoc/DOC-INDEX.tsv` row | present; **zero diff** `1097152a..HEAD` |

The epic's own deliverable list and the landed code agree item for item, including the
`syncUsageHead` sentence (`--task-id belongs to THIS face alone: a session (--peer/--peers/--full/…)`).

## Re-verification on the real binary (this run's measurement)

Built `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` from master `8c316cf6`.

Refusal matrix — exit 1, first stderr line names the flag, the row, the face AS TYPED, and
both remedies; **no** `sync <peer>:` session line is printed (the session is never reached):

| invocation | face as typed in the refusal |
|---|---|
| `--peer p1 --task-id t-seed1` | `--peer p1` |
| `--peers p1,p2 --task-id t-seed1` | `--peers p1,p2` |
| `--full --peer p1 --task-id t-seed1` | `--peer p1` |
| `--full --task-id t-seed1` (no peer — targets come from the declared whitelist) | `--full` |
| `--status --task-id t-seed1` | `--status` |
| `--serve-remote --task-id t-seed1` | `--serve-remote` |
| `--claim-remote --task-id t-seed1` | `--claim-remote` |
| inline `--task-id=t-inline` | `--peer p1` |
| selector BEFORE the face (`--task-id t-seed1 --peer p1`) | `--peer p1` |

Refusal text, verbatim (the double `task sync: task sync:` prefix is the known pre-existing
parser shape, out of scope — identical for every sibling refusal on this parse case):

```
gopi task sync: task sync: --task-id applies to --push-only only — --peer surface-peer is a SESSION (its export window is the delta window, so the row would be silently ignored); to push ONE row use `gopi task sync --push-only --task-id t-seed1`, to sync the whole window drop the --task-id
```

Counterweight (the guard did not overshoot): `--push-only --task-id t-seed1` without
`GOPI_SYNC_PEERS` → exit 1 `memsync push: no sync_peers declared`, i.e. the selector PARSED
and reached the push path. Sibling refusals unchanged: `--push-only --tags x` →
`--push-only takes no --tags`; `--serve-remote --peer p` still refuses.

**`--full` alone is a live session face**, not a degenerate one: `task sync --full` with a
declared whitelist opens a real session (`peer p1: memsync full: ssh p1: exit status 255`).
Its refusal row is therefore measured, not hypothetical.

## Gates observed

- `go test ./cmd/agent/ -run Sync -count=1` → `ok github.com/nick/gopi/cmd/agent 49.563s`
- `gofmt -l` on the three touched files → empty output
- `git status --porcelain` in the worktree → empty (before this record was committed,
  and it is the only file this run adds)

**Pre-existing, measured on master, NOT caused by and NOT fixed by this run** (against the
briefing's list, which is confirmed and sharpened): `TestDocManifestCoversEveryDevdocFile`
fails on `devdoc/assertion-totality-gate-master-red.md` (no DOC-INDEX row), and
`TestDocClassBannerMatchesManifest` fails on four historical docs missing their banner
(`bill-analysis-deepseek-20260922.md`, `handoff-20260922-bill-analysis.md`,
`handoff-20260922-0503.md`, `session-budget-prompt.md`) — both counting gates, both red on
master `8c316cf6` with no relation to `task sync`.

## Lesson for the next dispatch in this family

1. **Before creating a worktree for a "re-land", run
   `git merge-base --is-ancestor <original-commit> master`.** A branch that a worktree reset
   destroyed is not a commit that did not land — the merger's union lane lands commits whose
   branch is long gone, so "the branch is missing" is a false signal for "the work is missing".
2. When the answer is "already landed", the deliverable is a **verification record** (this
   file), not a re-implementation: re-deriving identical code would prove nothing the
   `1097152a..HEAD` zero-diff does not prove better, and would burn a worktree to produce a
   no-op commit.
