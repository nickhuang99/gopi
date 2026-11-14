<!-- doc-class: historical -->
# Task Report: re-key the two standards-gate baseline entries shifted by 1a5e8ea7

**Branch**: `fix/standards-rekey-2026-09-16` (cut fresh from master `0dac2ae7`)
**Worktree**: `/home/user/workspace/gopi-standards-rekey`

## Completed

- Re-keyed `errorWrapBaseline` `internal/tools/task_merge.go:362` → **`:366`** (Rule 16), in place.
- Re-keyed `elseAfterReturnBaseline` `internal/tools/task_merge.go:332` → **`:336`** (Rule 18), in place.
- Each re-key carries a `re-key: <cause>` comment recording the cause (commit `1a5e8ea7`, the ⓪c merge-time UNION COMPILE gate) and the evidence that it is a **pure line shift**, not a new violation.
- Cleared the **third** red class found on master: Rule 17 `NEW=2` at
  `internal/agent/journalreport_test.go:101` / `:104` (`os.Unsetenv` preparing a case whose
  *subject is the ABSENT state*). Per Rule 17's own precedent these got the reviewed inline
  escape `// env-leak-ok: absence is the subject — t.Setenv cannot express an unset variable`,
  **not** a baseline entry (the ratchet may not grow).
- Devdoc: this file.

### Why re-key and not delete

The gate's own STALE message says "Delete the entry so the gate keeps ratcheting". That advice
is correct for a site that was **fixed**, and wrong for a site that **moved**: the permit is a
licence over a `file:line` address, so deleting it leaves a genuinely new `%v`-wrap (or
else-after-return) at that address silently accepted later. A pure shift therefore re-keys in
place. The discriminating test is a text diff at the two revisions, not "an insertion happened
nearby":

```
diff <(git show 1a5e8ea7^:internal/tools/task_merge.go | grep -n 'no conflict state to abort') \
     <(git show 1a5e8ea7:internal/tools/task_merge.go  | grep -n 'no conflict state to abort')
# 1c1
# < 362:		return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %v", branch, mergeErr))
# ---
# > 366:		return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %v", branch, mergeErr))
```

Byte-identical statement, only the line NUMBER differs ⇒ pure shift. The Rule 18 site repeats
the signal on all its occurrences — an identical **+4** on every one (`330→334`, `346→350`,
`362→366`), i.e. the insertion sits above all of them, which is a shift and not an edit that
landed on a single chain.

## Verification

### BEFORE (red) — master's permits, master's tree

Captured by leaving `baseline.go` at master's revision in the same tree
(`git checkout <master-tip> -- internal/standards/baseline.go`), so cause is isolated to the
permits — same tree, same tests, only the baseline differs.

```
$ CGO_ENABLED=0 go test ./internal/standards/ -count=1 -v -run TestStandardGates
    gate_test.go:99: Rule 16 (%w error wrapping): 9 total hits, 7 baselined, 1 escaped, 1 new (unannotated), 8 baseline entries
    gate_test.go:111: Rule 16: NEW violation (checklist 2.2) at internal/tools/task_merge.go:366
    gate_test.go:111: Rule 16: STALE baseline entry internal/tools/task_merge.go:362 matches no violation
    gate_test.go:99: Rule 17 (test env isolation): 115 total hits, 91 baselined, 22 escaped, 2 new (unannotated), 86 baseline entries
    gate_test.go:111: Rule 17: NEW violation (checklist 5.2) at internal/agent/journalreport_test.go:101
    gate_test.go:111: Rule 17: NEW violation (checklist 5.2) at internal/agent/journalreport_test.go:104
    gate_test.go:99: Rule 18 (no else after terminating if): 3 total hits, 2 baselined, 0 escaped, 1 new (unannotated), 3 baseline entries
    gate_test.go:111: Rule 18: NEW violation (checklist 4.3) at internal/tools/task_merge.go:336
    gate_test.go:111: Rule 18: STALE baseline entry internal/tools/task_merge.go:332 matches no violation
BEFORE-EXIT=1
```

### AFTER (green) — this branch

```
$ CGO_ENABLED=0 go test ./internal/standards/ -count=1 -v -run TestStandardGates
    gate_test.go:99: Rule 16 (%w error wrapping): 9 total hits, 8 baselined, 1 escaped, 0 new (unannotated), 8 baseline entries
    gate_test.go:99: Rule 17 (test env isolation): 115 total hits, 91 baselined, 24 escaped, 0 new (unannotated), 86 baseline entries
    gate_test.go:99: Rule 18 (no else after terminating if): 3 total hits, 3 baselined, 0 escaped, 0 new (unannotated), 3 baseline entries
AFTER-EXIT=0
$ grep -cE 'task_merge.go|journalreport' .evid/after.txt
0
$ CGO_ENABLED=0 go test ./internal/standards/ -count=1
ok  	github.com/nick/gopi/internal/standards	8.720s
```

A note on exit 0: the package run exits 0 while the ROW for Rule 16 still says `new=0` only
because every hit is now either baselined or escaped. `hits` totals are unchanged on all three
rules (9 / 115 / 3), which is the invariant a re-key must preserve — the re-key moved a hit
between the `new` and `baselined` columns, it did not make a violation disappear.

### Ratchet — entries unchanged, per list

| List | Entries on master | Entries on this branch | Δ |
|---|---|---|---|
| `errorWrapBaseline` (Rule 16) | 8 | 8 | 0 |
| `elseAfterReturnBaseline` (Rule 18) | 3 | 3 | 0 |
| `testEnvBaseline` (Rule 17) | 86 | 86 | 0 |

Counted with `awk '/^var <LIST> = \[\]string\{/,/^\}/' internal/standards/baseline.go | grep -cE '^	"[^"]+'`.
The second-order figure that confirms the mechanism: a re-key moves a hit NEW → BASELINED, so
Rule 16's `baselined` rises `7→8` while its **`hits` total stays 9** (same for Rule 18, `2→3`,
total 3). The escaped count rises `22→24` because the two Rule 17 sites are escapes, not entries.

### Build / tests

```
$ CGO_ENABLED=0 go build ./...      # BUILD-OK
$ CGO_ENABLED=0 go test ./internal/standards/ -count=1     # ok (exit 0)
```

## Not Done (honest)

- **Full-suite regression was not run** in this session: `internal/tools` exceeds the
  per-call output-silence wall, so it ran as a detached job whose result is reported in the
  hand-off message rather than in this file. Only the two changed packages were exercised
  (`internal/standards`, `internal/agent -run TestSanitizeReportContract`). The tree contains
  no production-code change, so the blast radius is the gate itself plus one test's comments.
- The 352 Rule 16 and 96 Rule 17 legacy sites remain baselined — a codemod pass is a separate
  task and deliberately out of scope (that is the ratchet design, not a gap here).
- The gate's STALE message still advises "Delete the entry"; making it distinguish a *moved*
  site from a *fixed* one is a gate improvement, filed as a separate thought, not done here.

## Files

- `internal/standards/baseline.go` (two permits re-keyed, +`re-key:` rationale each)
- `internal/agent/journalreport_test.go` (two reviewed Rule 17 inline escapes)
- `devdoc/standards-baseline-rekey-2026-09-16.md` (this report)

## Recipe worth keeping

The full method (shift-vs-new diff proof, re-key-never-delete, entries-not-hits ratchet check,
escape-over-new-permit for a genuine exception, and the `git checkout <master> -- baseline.go`
cause-isolation trick for capturing BEFORE evidence in the green tree) is in the knowledge base
as record **#3140** — search "standards gate baseline re-key line shift", do not re-derive it.

## Recovery

```
git show fix/standards-rekey-2026-09-16:devdoc/standards-baseline-rekey-2026-09-16.md
git show fix/standards-rekey-2026-09-16 -- internal/standards/baseline.go
```
