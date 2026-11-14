<!-- doc-class: historical -->
# The removal-audit census read Go files only — a tracked shell script deleted refs silently

**Task**: 01a13b156dc57f14abdba1fab21f5315 (2026-10-15, peerbox)
**Measured at**: 3ca27b4 (this run's worktree base)

## The question that opened this

Three `task/…` branches (`01a132ed…`, `01a1344031…`, `01a13449f5…`) that a merge
run measured on 2026-10-13 and explicitly **refused** to delete were absent when
the run measured them again — and `~/.gopi/logs/removal-audit.log` (399096 lines,
1529 `op=branch:delete` records across 5 arms) named **none** of them. So either a
removal path bypasses the audited arms, or the log lost them.

## The answer: a removal path bypassed the census — because the census cannot read shell

**The census walks Go files only.** `removalAuditWalk` (`internal/standards/removal_audit_gate_test.go`)
calls `goFilesUnder(t, root, prodRoots, false)`. A ref deletion written in a
tracked **shell script** was therefore not "unrecorded" — it was *nonexistent* to
the predicate. This is the same shape the 2026-10-12 sequel fixed *inside* Go (a
deletion written as `update-ref -d` that the census could not SEE), one language
over.

**The site.** `scripts/post-merge-cleanup.sh` ran

```bash
git branch $DEL_FLAG "$BRANCH"        # DEL_FLAG = -d | -D
```

and wrote nothing to the audit sink.

### A/B proof (real verb, private sink)

Two fixture repos, three refs each, the **same real verb** (`gopi task archive
--reclaim`), two binaries built from this host's own object store:

| binary | source | refs/heads after | audit records |
|---|---|---|---|
| `gopi-old` | `e4ec081d1^` (pre-2026-10-12-fix) | `feat/abprobe` **gone** | **sink not created — 0 records** |
| `gopi-new` | this tree | `feat/abprobe` **gone** | **1 record**, `arm=archive-reclaim-branch`, with the deleted sha |

* `e4ec081d1` = *"fix(removal-audit): the archive-reclaim arm records the branch ref it deletes"* (2026-10-12 10:42), and `git merge-base --is-ancestor e4ec081d1 master` → **YES**. The HP fix **is** on peerbox's master; the hypothesis that it was absent is **refuted** by measurement.

### The shell path, measured

`bash scripts/post-merge-cleanup.sh feat/scriptprobe` in a fixture:

- **before the fix**: `refs/heads/feat/scriptprobe` deleted, `GOPI_REMOVAL_AUDIT_LOG` sink **never created**.
- **after the fix**: the deletion still happens, and the sink holds one `branch:delete`
  record (`arm=archive-reclaim-branch`, `target=feat/scriptprobe`, with `sha`).

## The fix — two halves, each with its own driver

1. **`scripts/post-merge-cleanup.sh` no longer deletes raw.** It routes the deletion
   through `gopi task archive <branch> --reason … --override-completed` — the ONE
   writer that removes the head *and* writes one `branch:delete` record. A missing
   or failing `gopi` **skips and reports** rather than degrading to a silent raw
   delete: the branch stays for a human, because a deletion is a DECISION.
   `--repo`/`--reason` were probed first (an earlier probe with `--reason post-merge`
   was refused by the verb's own vocabulary — a real refusal, not a silent one).

2. **The census gained a shell half.** Every tracked shell file (`git ls-files -z`,
   the same index reader `scratch_gate.go` / `repo_hygiene_gate.go` use) is scanned for

   ```
   git [ -C <path> ] branch -d|-D|--delete | update-ref -d|--delete | tag -d
   ```

   with two details that were forced by measurement, not by taste:
   - **flag variables are resolved per file** — the real line passed its flag through
     `$DEL_FLAG`, so a scanner matching a literal `-d` would have read the very line
     this half exists to catch as clean (the first RED proof run failed exactly there);
   - **quoted spans are stripped** before tokenising, so a documented spelling
     (`echo "run git branch -d …"`) is prose and the real line is a site.

   Non-vacuity floor: 200 tracked shell files (measured 243); a scan below it fails
   loud rather than passing blind (Rule 15).

## Red proof, on the real tree, with a content-anchored restore

`scripts/post-merge-cleanup.sh` was mutated **back** to its pre-fix raw delete:

```
--- FAIL: TestRemovalAuditGateShellRemovalsAreRouted
    scripts/post-merge-cleanup.sh:176 runs a git ref deletion in a TRACKED SHELL SCRIPT, outside every emitting lane
```

Restored, and the restore measured **against the revision**, not the index:

```
hash-object  = 96732c1d1ae0d8eb544a9488cfd51b7167591513
HEAD:script  = 96732c1d1ae0d8eb544a9488cfd51b7167591513   → RESTORED=YES
git diff HEAD -- scripts/post-merge-cleanup.sh  → empty
git status --porcelain → 0 lines
```

Green after restore: `TestRemovalAuditGateShellRemovalsAreRouted` PASS
(`tracked shell files scanned=243 raw ref deletions=0`).

## What is NOT determined (honest)

**Which path removed the three specific refs is not determinable from the evidence
in hand, and this is stated as undetermined rather than guessed.** The searched space
and what it returned:

| probe | result |
|---|---|
| `grep` the three ids in `~/.gopi/logs/removal-audit.log` (399096 lines, 2026-09-19 → 2026-10-14 16:07) | **0 hits, in any field** |
| any other `removal-audit.log` on this host (`find` under the profile, workspace, `/dev/shm`, `/var/tmp`) | only the one file |
| archive tags / `refs/gopi/archive-reason` for the three | **0** |
| the three refs today; their recorded tips | refs ABSENT; tips `593995b7a` / `3cca1c4b8` / `3beb56ba1` still resolve as commits |
| 187 session dirs in the 2026-10-13 18:40 → 10-14 23:30 window, for any of the three ids | **0 hits** |
| `~/.bash_history` (1403 lines, mtime 10-14 05:25) for these ids / `branch -D` / `update-ref -d` | **0 hits** |

There is **no journal of ref deletions in the repo** — only the audit sink, which the
silent path never wrote to. So the log does not "lose" records; a path that never
wrote to it is the explanation the evidence supports. Whether those three went out
through the script in question (which is in use — its own header records it, and the
2026-10-12 tag batch it documents) or through a raw `git branch -D` by an operator or
an agent following GOPI.md Rule 9's literal `git tag … + git branch -D …` text
**cannot be distinguished**, and neither can a `--force`-driven `-D` from the script.

**What is fixed is the only silent path found in the tracked tree.** The Rule 9 prose
itself still teaches `git branch -D` to agents — that is a *documentation* surface, not
a script, and it is outside this branch's scope; it is named here as the remaining
known gap (see the report's `## Not Done`).

## Files

- `internal/standards/removal_audit_gate_test.go` — the shell half: scanner,
  invocation-time gate, and its RED/GREEN driver.
- `scripts/post-merge-cleanup.sh` — the deletion routed through the recorded lane.
