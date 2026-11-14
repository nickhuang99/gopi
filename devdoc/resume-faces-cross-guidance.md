<!-- doc-class: historical -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# The Two Resume Faces: Cross-Guidance

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
<!-- disposition: this is a guidance RECORD, not a launch recipe. The worker face is named only as the peer of the session face, and the only commands this file tells a reader to copy are the session-id forms (`gopi session resume <id> --task <id>`), which are agent-side. Starting a worker stays operator-only — the canonical block named in the pointer above, `devdoc/worker-ownership.md`, owns that move; the operator's own launcher is documented there, not here. -->

## The defect

Two live surfaces resume work, and they differ in the one way that costs an
operator something concrete:

| face | layer | what it does to the ground |
|---|---|---|
| `gopi session resume <task-id\|raw-log-path\|session-id>` | session | starts a run and closes **nothing** out; a successful resume leaves the row's worktree **registered for good** |
| `gopi worker --resume <task-id>` (+ `--branch B` / `--worktree P`) | worker | takes the row's existing ground over and runs the close-out chain that **RELEASES** the worktree |

Neither help page named the other, and neither said what the choice costs. The
mechanism behind the session face's leak is not a bug in it — it is structural,
and `devdoc/worker-resume-ground.md` (the measured 2026-09-21 breach on row
`01a0c574`) explains why: `gopi session resume` **chdir's into the row's
worktree**, so the close-out arm that must remove the directory is standing in
it, and `guestRowOwnCheckoutRefusal` correctly refuses. Silent and unreported,
that is a Rule 15 failure: the operator picks a face, one of the two outcomes
leaks a worktree, and nothing says so.

## The fix: guidance, not removal

The session verb cannot be removed: it is the **only** resume route for what has
no row — `gopi chat`, a bare `gopi "<prompt>"`, a killed run with no ledger row
of its own — and those have no ground for the worker face to take over. (A
sibling row, `01a1367a-30fa`, covers the root-cure direction; this one is
guidance only and touches no verb.)

Three surfaces now teach the one choice:

1. **`gopi session resume -h`** names `gopi worker --resume <task-id>` and states
   the consequence of staying (nothing is released), then keeps teaching the
   no-row routes it exists for, and states that a **session-id resume links no
   ledger row**.
2. **`gopi worker -h`**, in the `--resume` paragraph itself (not at the end of
   the page), names `gopi session resume <id>` and says which half of the choice
   it is — the ground-**HOLDING** one.
3. **`gopi task show <id>`** prints the applicable face **beside the row's
   ground**, because the row — not a help page — is where an operator starts.

## Design decisions worth keeping

- **The row's advice follows the LIVE reading, not the record.** A row can
  *record* a worktree whose directory is gone; the worker face resolves an
  *existing* ground and would refuse that row, so the reading decides: live
  ground → `gopi worker --resume`; no live ground → `gopi session resume <id>
  --task <id>`. Exactly one line is printed (a row offering both commands would
  leave the reader where they started — with a choice and no advice).
- **The commands are single-sourced** (Rules 13/14): `internal/cliverbs` owns
  `ResumeTaskRowAddress` / `ResumeSessionAddress` and the program word `Program`,
  and `cmd/agent`'s `gopiProgramWord`, `CliVerbWorker`, `CliVerbSession`,
  `CliVerbResume` and `workerFlagResume` now **alias** them instead of
  re-typing. A page cannot teach a command the CLI does not dispatch.
- **The sentences are NOT single-sourced.** Only the *commands* are shared. Each
  page carries its own wording and its own indentation — a shared blob would
  drag one page's gutter onto another's, and `cmd/agent/usage_test.go` measures
  every rendered line in **display columns** against the page width.
- **A raw string literal cannot contain a backtick**, which is why both pages
  compose their guidance from concatenated constants (`resumeAdviceQuote` in
  `internal/tools` is the same fact, named).

## Verification

- `go test ./cmd/agent/ -run 'ResumeGuidance'` — two real-binary `-h` probes
  (Rule 31) plus the anti-drift case: the rendered pages must carry the shared
  addresses verbatim.
- `go test ./internal/tools/ -run 'ResumeAdvice'` — the classifier (live ground
  → worker face; dead ground → session face), and the read-only invariant
  carried onto the new line.
- Red proof: each guidance paragraph was deleted from its production file in
  turn and the matching case was observed **FAIL**; both files were then
  restored and re-measured with `git diff HEAD -- <path>` (empty) plus a
  `git hash-object` ↔ `git rev-parse HEAD:<path>` comparison. Raw runs:
  `<session>/artifacts/`.
