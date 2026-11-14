<!-- doc-class: normative -->
# Chat Cleanup Authorization — reading is not writing, and cleanup needs the operator's own words

**Task**: `01a115484b107acb9769d02f9d52940c`
**Date**: 2026-10-05
**Files**: `internal/tools/chat_write_guard_readonly.go`,
`internal/tools/chat_cleanup_authorization.go`,
`internal/tools/chat_write_guard.go`, `internal/agent/chat_write_boundary.go`,
`internal/agent/chat_write_guard.go`, `internal/agent/loop.go`,
`internal/agent/chat_ops.go`

## The defect, as measured

The chat write boundary refused `git worktree list` — twice, in PA field tests —
while the sentence the console is handed said reads are always fine:

> `(1) run NO git mutation … (reads — git status, log, show, diff — are always fine)`

The guard (`internal/tools/chat_write_guard.go`) matched the git **verb name**
against `ChatBannedGitSubcommands`, and `worktree` is on that list because
`worktree remove` reclaims a directory. So the *name* decided, and every
invocation of the name inherited the refusal: the console could not inspect the
worktree registry it is supposed to help clean up. A boundary that states a
predicate and enforces a name-table is not merely annoying — a model that reads
"reads are fine", is refused, and re-reads the refusal learns to distrust the
prompt, which is the only mechanism that keeps it inside the boundary.

## The fix: judge the INVOCATION, not the verb

`chatGitReadOnlyVerdict` (`chat_write_guard_readonly.go`) answers the question the
prompt already answers — *does this invocation only read?* — from ONE table keyed
by verb, with:

- the second **words** that name a read-only operation (`worktree list`,
  `stash list`, `stash show`);
- the **flags** admitted exactly, plus prefixes for options whose value rides the
  word (`-n5`, `--format=…`, `--sort=…`);
- `bareAllowed`, for a verb whose own default action is a query.

A word that is not admitted loses (so `worktree remove` loses on `remove`,
`branch -D` on `-D`), a flag must be admitted exactly (an option whose effect the
table does not know refuses the invocation rather than being waved through), the
`--` terminator refuses the whole invocation (past it a word is an OPERAND, and no
read-only word is an operand), and an unclassifiable shape is a refusal
(Rule 15 — never a silent pass).

### The measurement that shaped the table

`git branch list` does not list anything: **it creates a branch named `list`.**
Measured against real git — after `git branch list`, `git for-each-ref` showed
`heads/list`; `git tag list` created tag `list`. So the `branch` and `tag` rows
have an **empty** words map and `wordNamesARef` set: their non-flag word is a ref
NAME, their read-only forms are their listing flags alone, and the refusal says so
in git's own grammar rather than as "not a read-only operation" — otherwise a
reader who meant "list the branches" reads the refusal as a bug in the guard and
writes the same invocation again.

Two spellings were measured as **invalid git** and are therefore refused rather
than admitted (an unclassifiable shape is never a passive pass):

| spelling | measured |
|---|---|
| `git worktree -C <dir> list` | exit 129 (`-C` is a *git*-level option, not a worktree one) |
| `git worktree --subcommand=list` | exit 129 (the form needs `=`; the bare form needs a word) |
| `git stash --list` | exit 129 (`unknown option 'list'`; the spelling is `stash list`) |
| `git --no-pager worktree list` | exit 0 — allowed, because the table judges the WORDS OF THE VERB, not the leading git-level options the reader already strips |

The renderer (`ChatReadOnlyGitInvocations`) renders the spellings from the table's
own `renderWords`, so the prompt can only teach a spelling the guard admits — and
a test asserts exactly that (the `git branch list` trap is what it catches).

## The authorization channel

The boundary's rule (Rule 37) is that a chat owns no branch and no worktree, so it
commits not — but the console IS where an operator notices that a finished row's
ground is still on disk. The repository owns that capability, and it is **plan by
default** (measured in its own parser: `dryRunGate()` is `!apply || dryRun`):

- `gopi task sweep-worktrees` — reclaim stale worktree registrations.

HISTORICAL (worktree-retirement phase 2): this list once had a second entry,
`gopi task finish-cleanup` (the close-out chain for a terminal row), gated by its
own `finishCleanupPlanGate()`. That verb, its constant and its plan gate were
deleted with the worktree model; the ONE face list that feeds both the prompt and
the refusals (`internal/tools/chat_write_guard_readonly.go:chatCleanupFaces`) now
carries `sweep-worktrees` alone. Nothing below about the *authorization
mechanism* changes — only the face it enumerates.

The chat may run either, and `--apply` — the destructive half — is gated on **the
operator's own turn message** (`ChatTurnAuthorizesCleanup`). No new cleaner, no
new script, no new face (Rule 3/35).

### Why the gate is a turn's text and not a flag

The requirement was that it must not become an always-open switch, and the
mechanism for that is *where the authorization lives*:

| design | why it was rejected |
|---|---|
| a settings key / env var | a persistent artifact (Rule 35) created so the operator need not write one sentence; and it stays on |
| a `--yes`-style flag on the guard | the model would learn to add it; the operator's intent never enters |
| a stored permission on the agent | state that a later turn inherits — the leak the test suite pins |
| **this turn's message** | not stored anywhere: `chatTurnInput` is published at the start of a turn and restored on the way out, so a turn that asks nothing authorizes nothing, a prior turn's authorization cannot leak, and there is nothing left enabled |

The known limits are stated rather than hidden (Rule 15): a negation ("do not
clean up") is handled by requiring the message to name the FACE ITSELF before its
cleanup phrases count — a crude lexical rule, not a semantic parse — and a
paraphrase outside the vocabulary is a false negative whose remedy is printed in
the refusal (name the verb, or run the plan and ask).

### Where the wiring lives, and the property it must hold

`Agent.Chat` and `Agent.ChatOps` both publish the turn (`a.publishChatTurn(msg)()`,
a deliberate pair so the restore cannot be forgotten by one path), and the
dispatch's ONE entry point (`chatToolWriteGuard`) receives it as a parameter. The
spellings that carry **no** turn — `ChatBashWriteRefusal`,
`ChatBashWriteRefusalIn` — hold the gate **closed**, so a caller cannot open it by
omitting an argument.

## Rule 37 coverage: does cleanup contradict "a chat does not commit"?

No, and the argument is worth stating because the two look adjacent:

1. **What Rule 37 forbids is a chat producing an unowned ARTIFACT** — a commit
   with no branch, no ledger row and no rollback point, in a tree shared with the
   operator.
2. **What cleanup does is COLLECT a run that already has all of those.** The
   worktree and the branch it removes belong to a *terminal* row whose
   deliverable was decided and recorded elsewhere; the ground is the disposable
   half (Rule 30's own distinction: the branch carries the work, the directory is
   the environment).
3. **So the two rules agree on the invariant** — nothing a chat does may leave
   work unattributed. Refusing the destructive half unasked protects the opposite
   error: a console deleting a LIVE run's ground. That is why the faces are gated
   rather than banned, and why they are the *repository's* faces rather than a new
   `rm -rf` the console invents: the faces already carry the guards this argument
   depends on — a live holder process in the directory, a non-terminal owner row,
   a dirty worktree, the declared root, and the main checkout.
4. **What a chat must never do is therefore unchanged**: it does not commit, it
   does not delete a directory by hand, and it does not touch another run's live
   ground. It may run a PLAN in any turn; it may run the apply when the operator
   asked for cleanup in that turn.

## Guardrails (planned vs. observed)

The four safety rails the epic asked to be pinned. The last three are **the
repository's own**, reached by running its face — not re-implemented here:

| rail | where it lives | pinned by |
|---|---|---|
| live process in the directory | `sweepWorktreeHolderAlive` (the holder scan; Rule 24: liveness from `/proc/<pid>/cwd`, never `pgrep -f`) | `internal/tools` sweep suite |
| uncommitted tracked changes | the sweep's CLEAN precondition (Rule 30: a dirty worktree is REPORTED, never removed) | same |
| the main checkout / declared root | the sweep's root refusal | same |
| **no operator authorization** | `chatCleanupApplyRefusalFor` (this change) | `internal/tools/chat_cleanup_authorization_test.go`, `internal/agent/chat_cleanup_authorization_test.go` |

## Verification

See `tasks/01a115484b107acb9769d02f9d52940c-report.md` for the Rule 5 block
(build, branch-scoped tests, the exact invocations and what they printed).

## Not done (honest)

- `worktreeroots`-based resolution of the cleanup faces' own `--apply` reach is
  the faces' business and is not re-derived here.
- The negation handling is lexical (see above): "do not clean up" is caught, but a
  same-sentence negation of a long paraphrase is not parsed.
