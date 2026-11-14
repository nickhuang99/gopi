<!-- doc-class: historical -->
# The eight parked `rework:` rows — preservation order

Measured 2026-09-21 against master tip `fb9097bb`. Eight rows are `pending` with
a `rework:` summary. This document is the **order in which their work must be
preserved**, not a resolution: it says where each one's work lives RIGHT NOW and
whether that substrate is still reachable.

## Why this document exists

The mechanical union lane (`cmd/agent/merge_union.go`) removes the class of row
these eight belong to — a conflict whose every path is a declared append-only
generated artefact. But the lane does not resurrect them: by the time it landed,
the producing merge run had already **deleted their branch refs**. So the
question "what is about to be lost" has to be answered from what still exists.

## The measurement

| # | row (task id) | branch | ref exists? | work in master? | substrate that still holds the work |
|---|---|---|---|---|---|
| 1 | `01a0c34c-15bb-…` | `feat/rule31-ran-check` | tag only | no (`451a0117`) | `rework-backup/feat/rule31-ran-check` **+** attachment |
| 2 | `01a0c34c-02da-…` | `fix/unknown-dash-flag-refusal` | tag only | no (`80e33cd2`) | `rework-backup/fix/unknown-dash-flag-refusal` **+** attachment |
| 3 | `01a0c2a7-6b92-…` | `feat/gopi-cli-bubbletea-landing` | tag only | no (`e32f4e93`) | `rework-backup/…` **+** attachment (232 KB) |
| 4 | `01a0c2a7-627f-…` | `feat/chat-colour-palette` | tag only | no (`4f92dc52`) | `rework-backup/…` **+** attachment (204 KB) |
| 5 | `01a0c2a7-7472-…` | `fix/serve-lock-refusal-diagnosis` | tag only | no (`dfc9d688`) | `rework-backup/…` **+** attachment (78 KB) |
| 6 | `01a0c386-d0ce-…` | `feat/g2-sync-help-surface` | **NONE** | no | **attachment only** (three-dot diff, 9 files +1464/-12) |
| 7 | `01a0c308-cfc8-…` | `feat/cli-settled-state` | **NONE** | no | **attachment only** (tip `14cde0d5` unreachable) |
| 8 | `01a0c307-b400-…` | `rework/chat-three-branches` | **NONE** | no | **attachment only** (tip `a1229f31` unreachable) |

Every tip above was checked with `git merge-base --is-ancestor <ref> master` —
all negative, so **no row can be closed as "already landed"**.

## The order, and why it is this order

**Tier 1 — attachments only, nothing else left (rows 6, 7, 8).**
For these three the ORIGINAL commit is gone: no local ref, no remote ref, no
`rework-backup/` tag, and `git log --all` finds nothing. A `git gc` pruned the
objects after the merge run deleted the ref. The archived three-dot diff in the
task's own session directory
(`~/.gopi/sessions/<task-id>/attachments/…`) is therefore the **single copy** of
the work. **Preserve before anything else**: their diffs name whole packages
(`internal/clistate/`, chat one-writer-per-frame work, the g2 sync help surface).
Losing those attachments loses the work outright.

**Tier 2 — tag-pinned, nothing in master, conflict is mechanical (rows 1, 2).**
Both conflicted on exactly ONE file, `devdoc/DOC-INDEX.tsv`, and the row's own
briefing says so ("a TEXTUAL conflict, not a semantic one"). These are the rows
the new union lane was built for: re-deliver the branch on current master and the
lane resolves the manifest mechanically, no context spent.

**Tier 3 — tag-pinned, larger surface (rows 3, 4, 5).** Same protection, but the
conflict set is real code (`cmd/agent/main.go` dispatch, `cli_chat.go` +
`internal/agent/loop.go`, `cmd/agent/cli_tasks.go` modify/delete). Sheet order
matters here: row 3 is 27 files / +5211 and row 4 is 204 KB of chat work — land
the small mechanical ones first so the branch-diff surfaces stop moving under
these.

## What each row needs (all eight share the same first step)

1. **Do not let the attachment be cleaned.** It is the deliverable for rows
   6-8 and the backstop for rows 1-5.
2. **Align the branch with master BEFORE re-delivering** — the delivered branch
   sitting far behind master is the other half of why they parked
   (cf. `01a0c37d-db98-7812-b113-8bcbc00e42ed`).
3. **Re-probe the conflict against the master it will actually merge into**, not
   the master at the time it parked.
4. **Re-run the branch's own tests** on the merged tree
   (`CGO_ENABLED=0 go build ./...`, `go vet ./...`, the branch's own package
   tests), then re-deliver so a later run lands it by fast-forward or by the
   union lane.

## Not Done (honest)

- This is the ORDER, not the execution. Nothing here has been merged, re-based or
  re-delivered.
- `internal/standards/baseline.go` (row 6's conflicting file) is deliberately
  outside the union set: a union of two baseline files can turn a standards gate
  green by admitting violations. It needs its own argument or a
  regenerate-from-clean-tree pass — not a blind union.
