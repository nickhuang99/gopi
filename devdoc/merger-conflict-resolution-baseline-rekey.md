<!-- doc-class: normative -->
# Merger Conflict Resolution: fix/merger-status → master

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Record, not current
> truth: the merger conflict resolver this document describes ran inside the
> resident merger's tick, which was deleted (`f9ff1a89`, `55c4d6f3`); every
> spelling of `gopi merger` now refuses and the one-tick merge is the bare
> `gopi merge` *(re-dated 2026-11-01: that bare verb was removed too by the
> merge-removal epic and `./bin/gopi merge` exits 1; the one-tick merge is
> `gopi task merge` today — `man/gopi-merge.md` is its contract page)*. See
> `devdoc/merger-retired.md` and the dated correction in
> `devdoc/merger-self-driving.md`.


Task: merger's conflict resolver for branch `fix/merger-status` merging into `master`
in `/home/user/work/GoPi`. Executed in the linked worktree
`/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-resolve-1284455/gopi-merge-resolve-2585762633/wt`.

## Completed

- **Reproduced the merge**: `git merge master --no-commit --no-ff` → exactly ONE
  content conflict, `internal/standards/baseline.go` (two hunks). `cmd/agent/main.go`,
  `cmd/agent/cli_tasks.go` and the rest auto-merged.
- **Resolved both hunks by re-keying to the MERGED tree, never by dropping a side.**

### Hunk 1 — `errorWrapBaseline`: the `cmd/agent/main.go` %v site

Neither side's key survived the merge, and *neither side's number was the merged number*:
HEAD keyed the site `685`, master `681` — each had counted only its own insertions above
the site. The merged tree carries **both** sides' code (master's `gopi merger` const/help
block **and** this branch's `gopi merger --status` argv guard) over master's agent-spawn
guard, so the live line is **691**:

```
grep -n 'escape a literal prompt' cmd/agent/main.go   →  691
```

Resolution: ONE re-keyed entry `"cmd/agent/main.go:691"` carrying both sides' re-key
rationale. Both stale keys were dropped — a permit that matches nothing is a *silent
permission*, and the gate fails on a stale entry exactly as it fails on a new one.

### Hunk 2 — `elseAfterReturnBaseline`: `cmd/agent/cli_tasks.go:1171`

**Both sides agreed** on this one and disagreed only in wording. Master rewrote the site
(`... , err := ...; err != nil { return ... } else if len(replicaPaths) > 0 {`) into a
guard-clause pair, and HEAD kept the guard-clause form — so no `} else if` exists at that
site in the merged tree, and an entry keyed to it would be a STALE permit. Resolution:
**neither side's `cli_tasks.go:1171` entry is carried**, with both sides' merge-note
rationale preserved in one comment. HEAD's pre-guard `cli_tasks_rebuild.go:66` was likewise
not carried (master's agent-spawn guard moved that site); the live keys are the re-keyed
`:77` / `:87` pair the baseline already carried for the same site.

## Not Done

- No full-suite `go test ./...` run — by the task's rules (the repo has a known
  pre-existing full-suite hang, and a hanging check must not delay the commit). Verification
  was deliberately bounded to the build plus the touched package and the `cmd/agent` merger path.
- No push, no merge, no branch deletion: out of scope for the resolver, per the task
  constraints (the merger re-merges and cleans up).

## Key lesson (generalizes to every baseline-table conflict)

A `baseline.go` conflict is a **line-key conflict**: `file:line` permits are keyed to a
position, and both sides moved the same site. During a resolution the merged line number can
be **neither side's** number (here 685 and 681 → **691**), because the merge unions both
sides' insertions above the site. So never pick a side's number by inspection — **re-derive
it from the merged tree** (`grep -n` the offending construct) and re-key, then let the gate's
data report prove it: `NEW=0 AND stale=0` is the only passing signature.

## Verification

- Build: `go build ./...` → **OK** (exit 0)
- Touched package: `go test ./internal/standards/` → **ok 2.636s**
- Gate data report: `go test -run TestStandardGateDataReport -v ./internal/standards/` →
  - Rule 16 (%w wrap): `hits=8 baselined=8 NEW=0` ← the re-keyed `cmd/agent/main.go:691` is live
  - Rule 18 (else-after-return): `hits=5 baselined=5 NEW=0` ← no stale permit, no new violation
- Consumer package: `go test ./cmd/agent/ -run 'TestMerge|TestMerger'` → **ok 19.883s**
- gofmt: `gofmt -l internal/standards/baseline.go` → clean
- Commit: **e617183d** `resolve merge fix/merger-status into master`
  (parents `518d3bdb` HEAD + `d259403e` master = a true merge commit on `fix/merger-status`)
