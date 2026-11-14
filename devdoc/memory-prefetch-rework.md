<!-- doc-class: historical -->
# Rework: typing-time memory prefetch onto the split `internal/memoryprefetch`

**Class**: historical (the record of one rework run, 2026-09-28).
**Task**: `01a0e713-e7c9-72a1-a489-594b28aad148`. **Branch**:
`task/01a0e713e7c972a1a489594b28aad148`.

## What was handed over, and why it could not land

`feat/memory-prefetch-executor` (tip `b13429ac`, ref deleted; the only surviving
copy is a 2509-line diff archived under the session directory) shipped the
feature as a **monolithic** `internal/memoryprefetch/prefetch.go`. Master had
meanwhile split that package into `budget.go / config.go / extract.go / gate.go /
render.go / state.go`. A probe merge of the branch into master was **textually
clean (`rc=0`) and did not compile**: the monolith re-declared `Hit`, `State`,
`State.Reset`, `DefaultMaxKeywords` and `stopwords`, and referenced fields master
had removed. Git cannot see this class of conflict; only a compiler can
(`go build` on the MERGED tree, not the merge's exit status).

## The re-expression (every behaviour kept, on the file that owns it)

The monolith was **not** resurrected. Its contents were re-expressed:

| branch (monolith) | re-expressed as | why there |
|---|---|---|
| `Params` / `DefaultParams` | master's existing `Config` / `DefaultConfig` + new `DefaultMinKeywordRunes` (referencing `extract.go`'s own `minCandidateRunes`) | one tuning surface, one floor — Rules 1/13/14 |
| `Keywords`, `isCJK`, `stopwords` | master's existing `ExtractCandidates` | already there, richer (CJK bigram degradation path) |
| `Query` | `session.go` | the typing path's gate — delegates the decision to `EvaluateGate` |
| `State`, `Observe`, `Claim`, `Store`, `Take`, `LastSectionQuery`, `Reset` | new `session.go`: `Session`, `Observe`, `Claim`, `Use`, `Take`, `PendingQuery`, `Reset` | a **different** state machine from `state.go`'s `State` (turn boundaries vs pause/differential) — so a second name, not a second meaning |
| `Fold` / `hitBody` / `trimRunes` / `collapseSpace` / `itoa` / `conf1` / `tierUnknown` | master's `RenderSegment` / `TrimHits` / `clipBody`, plus new `InjectSection` | the renderer master already owns; `InjectSection` is the one piece it lacked (see below) |

### Why `InjectSection` is new, and lives in the pure package

The executor must append a block to the model's INPUT and never to `Display`.
Master's `RenderSegment` returns a `Segment`; nothing joined it to a message.
`InjectSection(out, hits, cfg)` does that join **in the pure package** for Rule 31
reasons: the claims about the injected form — "an empty result appends nothing,
so the message is byte-identical" and "the section is a separate block" — are
then testable by driving the real function with no ledger, no goroutine and no
terminal (`session_test.go:TestInjectSectionJoinsOnlyARealSection`). The executor
calls it and adds the outbound-boundary concern (which field the section may ride)
on top.

## The `standalone_test.go` arm that had to change

Master's `TestNoAgentOrCLIImport` asserted "nothing under `internal/agent` or
`cmd/` imports this package" — true while wiring was a later child, and false the
moment the executor exists. It was **replaced, not deleted**:

- `TestAgentPathStaysUnwired` — `internal/agent` still imports nothing (the agent
  path is not the typing path).
- `TestOnlyTheExecutorImportsThePackage` — under `cmd/`, the import appears in
  exactly the executor and its tests. A second file reaching for the package is
  how a second goroutine, a second record and a second adoption point appear.

## Unrelated drift in the handed-over branch: judged, measured

`internal/uiclient/dumpread.go`'s one-line edit rewording a comment
(`re-typed "sessions")` → `re-typed spelling of "sessions")`) is **not** part of
the feature: it repairs master's own red gate
(`internal/config.TestSessionsDirNameNoHardcodedLiterals`, measured red on a
pristine master worktree) whose scan covers `internal/`. Because the acceptance
requires `./internal/config/...` green, the repair is kept and committed **as its
own commit, with its reason stated**, rather than smuggled in as feature code.

## Where the feature does not reach (stated, not claimed)

`gopi chat --tools` seals its prompt in `internal/agent`'s loop, not in
`cmd/agent/cli_chat.go`'s turn loop — a different seam with its own tests, and
wiring it here would mean one prefetch feeding two boundaries. The plain chat
face, which is the surface the knob documents, is wired.

## Action loop (what changed after the evaluation)

**Deliverable**: branch `task/01a0e713e7c972a1a489594b28aad148`, three commits on
top of master `21b426d0` — `24fb9bac` (the re-expression), `09861dfc` (the
pre-existing master gate red, repaired with its measurement), `78914eaf` (this
doc + its index row). Worktree
`/home/user/workspace/gopi-01a0e713e7c972a1a489594b28aad148`, clean.

**Owner decision (the eval's menu #1)** — the handed-over branch's monolithic
`prefetch.go` is **superseded**, not carried: re-expressed on master's split
files. The menu's option (a) ("land the monolith and reconcile at merge") was
rejected because it would recreate master's already-split package and lose the
12-file layout that the split child owns.

**Root cause of the non-compiling clean merge (menu #4)** — not a merge problem:
the branch was cut BEFORE master's split, so both trees declare the same symbols
in the same package in different files. Two independent Go definitions merge
textually; only a compiler sees the collision. The durable guard is the one the
acceptance names: **always build the MERGED tree**, never trust `rc=0`.

**Live values (menu #7/#8)**: measured in this run, on the real binary —
`GOPI_MEMORY_PREFETCH=1`, `--memory-prefetch chat` (bare, before the verb), and
`{"memory_prefetch": true}` each reached the executor
(`[gopi] memory prefetch: on (source: <channel>)`), and the silent tree (nothing
declared) printed no prefetch line at all.

**Queued rather than done (menu #13/#14/#15)** — dispatchable, none blocking:

1. `gopi chat --tools` adoption: the section would have to be adopted at
   `internal/agent`'s outbound boundary, not `cmd/agent/cli_chat.go`'s.
2. `--memory-prefetch` as a bare top-level flag on the cooked path: the hoist is
   in place (`prefetch_bound_env.go`) and the executor is armed, but a cooked
   reader has no published snapshot, so the run correctly records nothing.
3. Operator setting knob for the prefetch's own numbers (timeout / busy timeout /
   poll interval / hit limit) — today they are executor constants (Rule 1), not
   settings keys.
