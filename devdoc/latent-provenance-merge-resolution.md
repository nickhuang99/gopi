<!-- doc-class: historical -->
# Task Report: merge resolution — feat/latent-provenance ← master

Worktree `/tmp/gopi-merge-resolve-1796869929/wt` (branch `feat/latent-provenance`,
pre-merge tip `1c204537`), master `dd1c267a` ("Merge branch 'fix/stylegate-ceiling-audit'").

## Completed

- Reproduced the merge: `git merge master --no-commit --no-ff` → **exactly one
  conflict** (`UU internal/tools/memory_provenance.go`); 21 other paths
  auto-merged cleanly (mostly `_test.go` per-site `//stylegate:ignore` reasons
  from master's unchecked-close audit, plus `devdoc/stylegate-ceiling-audit.md`).
- Resolved the single hunk (`memory_provenance.go:475`, the deferred
  `tx.Rollback()` in `provenanceBackfill`) and removed every
  `<<<<<<< / ======= / >>>>>>>` marker (`grep -c '^<<<<<<<'` → 0).
- Committed the resolution as `a43138c7`, message
  "resolve merge feat/latent-provenance into master"; `MERGE_HEAD` is gone and
  `git diff --check` is clean.

### The one conflict, and why this is the right resolution

Both sides fixed the **same** stylegate finding — the ignored-error hit on
`defer func() { _ = tx.Rollback() }()` after a successful `Commit` — and both
refused to restructure control flow for a gate:

| side | shape |
|---|---|
| HEAD (branch) | the marker alone, on a comment line **above** the defer: `//stylegate:ignore after a successful Commit the rollback is a no-op; …` (commit `8e6c72ad`, "make the latent harvest add zero new gate findings") |
| master | the audited superset: prose rationale **plus** the marker on the defer line itself (commit `c7ae8569` of `fix/stylegate-ceiling-audit`), matching `devdoc/stylegate-ceiling-audit.md` §1 |

**Resolution: kept master's block** (the superset — rationale *and* the
gate-preferred marker placement, which is the form that branch's own audit
committed to) and **recorded the branch's intent in a `merge-note:` comment**
above it. Neither side is silently dropped: the branch's intent (documented
inline escape, control flow untouched) is the same intent master's text states,
and the note says so at the site so the next reader does not "simplify" the
block back into a conflict. No behaviour change on either side — the code line
is character-identical in both sides' versions.

## Not Done

- No code semantics were changed: this is a documentation/comment-block merge.
- The pre-existing `TestStyleGate` failure below was **not** fixed — it is
  outside this task (a merge resolution must not widen a style sweep), and it is
  reported instead.

## Verification

- Build: `go build ./...` → exit 0 (OK).
- Touched package (the one internal/tools file I resolved):
  `go test ./internal/tools/ -run 'Provenance|Latent' -count=1` → `ok
  github.com/nick/gopi/internal/tools 6.818s` (PASS).
- Markers / merge state: `grep -c '^<<<<<<<' internal/tools/memory_provenance.go`
  → 0; `git rev-parse -q --verify MERGE_HEAD` → none; `git diff --check` → clean.
- Stylegate probe (bounded): `go test ./internal/stylegate/ -run TestStyleGate`
  → FAIL, and it is **pre-existing on the branch tip, not introduced by this
  resolution**:

  ```
  stylegate errorf-verb: 10 production hit(s), ceiling 9
  ```

  9 of the 10 sites are master-side files untouched by this merge
  (`internal/tools/memory.go` ×2, `memory_procedural.go` ×4,
  `memory_sync_claim.go`, `memory_sync_procedural.go`, `task_merge.go` —
  exactly master's ceiling of 9); the 10th is
  `internal/tools/memory_provenance_latent.go:747`
  (`fmt.Errorf("calibrate latent: scan: %w (close: %v)", err, cerr)`), a file
  that exists **only on this branch** and is byte-identical before and after the
  merge. So the count is 10 with or without my resolution, and the hit is not in
  any file this merge touched. (The site's `// no-wrap:` comment is Rule 16's
  escape in `internal/standards`, which `internal/stylegate`'s errorf-verb rule
  does not consult — the two gates have separate escape vocabularies. Fixing the
  count means either allowing `%w (close: %v)` in stylegate's errorf-verb rule or
  splitting the wrap, which is a stylegate/standards decision, not a merge one.)
- Full `go test ./...` deliberately not run (repo's known pre-existing suite hang;
  the epic waives it).

## Commit

`a43138c7` — resolve merge feat/latent-provenance into master (branch
`feat/latent-provenance`, in the shared repo; no push — a worktree shares refs).

## Round 3 (worktree /tmp/gopi-merge-resolve-1521622821/wt, pre-merge tip 23152227, master c9e37b53)

master's tip this round is itself a previous merge resolution (`c9e37b53 resolve
merge feat/latent-provenance into master`), so the round is small.

### Completed

- Reproduced: `git merge master --no-commit --no-ff` → **exactly one** conflicted
  path, `UU internal/tools/memory_provenance_test.go` (2 hunks); the other 22
  paths (master's per-site `//stylegate:ignore` reasons on `_test.go` files,
  `devdoc/stylegate-ceiling-audit.md`, the two files this branch added) applied
  cleanly.
- Resolved both hunks, removed every marker (`grep -n '^<<<<<<<' …` → no match),
  `git diff --check` clean, `gofmt -l` empty, staged and **committed first**:
  `c7657661 resolve merge feat/latent-provenance into master` (merge commit with
  `--no-ff`; `MERGE_HEAD` gone, working tree clean).

### The two hunks, and why this is the right resolution

Both sides fixed the **same** stylegate finding (`rows.Close()` / `rows2.Close()`
discarded after a complete iteration in the additive/idempotency proof) and both
pointed at the same code lines:

| side | shape |
|---|---|
| HEAD (branch) | `if err := rows.Close(); err != nil { t.Fatalf(…) }` — check the close error, with a comment saying the gate counts a *bare* `Close()` as silent |
| master | `rows.Close() //stylegate:ignore <rationale>` — the audit's marker-with-reason form |

**Resolution: kept HEAD's checked-close form** (behaviorally stronger — it acts
on the error instead of documenting its discard) **and recorded master's
rationale in the comment block** ("the assertions below compare the before/after
maps captured from it…"). Neither intent is lost: master's intent is "this close
is a deliberate, reasoned non-check", which HEAD's side satisfies by
construction — a *checked* close is never a bare close, so no `//stylegate:ignore`
escape is needed and master's unchecked-close gate stays green (verified below).

### Not Done

- The pre-existing `TestStyleGate` `errorf-verb` failure (ceiling 9, actual 10;
  the 10th site is branch-only `memory_provenance_latent.go`) — outside a merge
  resolution's scope and not touched here. Note for the next reader: the fix
  described in the round-2 section above is stale — no `"calibrate latent: scan:
  %w (close: %v)"` site exists in this tree any more, so the ceiling gap came from
  master's own ceiling table, not from that line.
- Full `go test ./...` deliberately not run (repo's known pre-existing suite hang;
  the epic waives it).

### Verification (bounded, this round)

- Build: `go build ./...` → exit 0 (OK).
- `go test ./internal/stylegate/ -count=1` → `ok …internal/stylegate 2.189s`
  (the gate set master's side hardened is green with the checked-close form).
- Touched package: `go test ./internal/tools/ -run 'TestProvenance|TestMemoryProvenance' -count=1`
  → `ok github.com/nick/gopi/internal/tools 0.298s` (PASS).
- Markers / merge state: no conflict markers in the resolved file, `MERGE_HEAD`
  absent, `git status --porcelain` empty.

### Commit (round 3)

`c7657661` — resolve merge feat/latent-provenance into master (branch
`feat/latent-provenance`, in the shared repo; no push — a worktree shares refs;
recovery: `git show c7657661:internal/tools/memory_provenance_test.go`).
