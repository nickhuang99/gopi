<!-- doc-class: historical -->
# The manual push's receipt: "accepted" was about the LOCK, not the row

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status**: fix landed, red→green measured, real CLI run recorded.
**Touches**: `internal/tools/memory_sync_claim.go`, `cmd/agent/cli_tasks_sync.go`,
`internal/cliverbs/cliverbs.go`, `cmd/agent/cli_tasks.go`, `GOPI.md` (local-only
policy section).

## The defect, restated in one line

`gopi task sync --push-only --task-id T` rides the **CLAIM BROADCAST**: the peer
handler (`SyncClaimRemote`, `--claim-remote`) mirrors the row's **LOCK** to
`in_progress` and answers an `accepted/refused` verdict. It never writes the row
into its own ledger.

The success line said:

```
push peerbox: accepted (1 row(s))
```

exit 0. Every word of that is true **about the lock**, and the sentence reads as
"the row arrived". Measured 2026-10-12 from the client host: exit 0, `accepted`,
while peerbox's `tasks` table held **zero** matching rows and its
`max(updated_at)` predated the row's creation — the row had never been there.

The defect is not the transport. The transport is correct and deliberate: the
claim broadcast exists so a peer cannot run a row another node just claimed, and
the whole-window push and the serve tick share this one primitive. The defect is
that **a receipt about the LOCK was presented as a criterion for PERSISTENCE**,
and a host policy paragraph pointed at it as "the way to get a row into peerbox's
queue" (Rule 15b: prove the EFFECT, never the receipt).

## The fix — wording, never semantics

Constraint 3 said `--push-only` keeps its existing meaning. It does. Nothing
about what crosses the wire changed; one line of output changed.

Before:

```
push peerbox: accepted (1 row(s))
```

After:

```
push peerbox: LOCK mirrored (1 row(s); the peer answered "accepted" about the LOCK) — the row is NOT in the peer's ledger (this leg is the CLAIM BROADCAST) | write the row: `gopi task send --task-id <id>` | check the peer: `ssh peerbox 'gopi task show <id>'`
```

Four things, in the order an operator needs them:

1. **what actually crossed** — `LOCK mirrored`, in the family's own vocabulary;
2. **the explicit absence** — `the row is NOT in the peer's ledger`. This is the
   half the old sentence omitted, and the whole reason the epic exists;
3. **the verb that DOES deposit it** — `gopi task send --task-id <id>`, the row
   leg's push half (`SyncRowSend`), whose receipt is about the ROW (stored /
   already current / migrated / REFUSED with the reason), not about a lock;
4. **a peer-side check** — `ssh <peer> 'gopi task show <id>'`, so the operator
   settles the question from the other end instead of trusting either line.

The peer's own verdict word (`accepted`) is **kept** — the exchange did happen,
and hiding it would be its own small lie — but it is now quoted *inside* a clause
that binds it to the lock, never standing alone as the conclusion.

## Design notes worth keeping

**A shared `--task-id`.** The receipt prints a command for the operator to type,
so the selector had to stop being a literal on the tools side. It now lives in
`internal/cliverbs` (`TaskIDWord` / `TaskIDFlag`, the same word/dashed split
`RawFileWord`/`FileFlag` and `TaskTagsWord`/`TaskTagsFlag` already use), and
cmd/agent's `taskIDFlagName` became an **alias** of it. That is Rule 13/14 applied
to the one string whose drift would print a command the parser refuses.

**The peer-side check is the forwarder's own constant.** The recheck command is
`taskSubmitConfirmVerb` — the verb `task_submit_forward.go` already uses to prove
a forwarded row landed. One definition, referenced twice: the check the receipt
recommends is byte-for-byte the check the submit forwarder proves a row with.

**The remote command is shell-quoted through the existing escaper.**
`harvestShellQuote` (Rule 14: one escaper for the whole family) wraps the remote
command, because it travels through the peer's login shell as one argument.

**The window case has no row to name.** Without `--task-id` the push carries the
whole delta window, so the receipt prints a `<row-id>` placeholder rather than a
command addressing a row it never named.

## The regression guard, and why it is peer-free

`internal/tools/claim_push_receipt_honesty_test.go` drives:

- the **real renderer** (`syncClaimPushReceipt`) — wording, both directions
  (names the lock AND denies the write), the deposit command's derivation, the
  peer-side check, the placeholder case, and that the pre-fix headline
  `push <peer>: accepted` is unprintable;
- the **real push function** (`SyncTaskPushOnlyPeers`, the one
  `gopi task sync --push-only` reaches) — the renderer being right is not enough
  if the push path bypasses it;
- the **CLI seam**, read the way `cliverbs`'s drift guard reads cmd/agent (it is
  `package main`, unimportable) — so the honest line cannot become dead code
  behind a second path.

No peer and no network: the ssh transport is the family's own seam
(`SetClaimSSHExchangeHookForTest`), and the row is seeded through the real write
path so the push's own row selection resolves it.

**Red → green measured.** With the renderer temporarily reverted to the legacy
`fmt.Sprintf("push %s: accepted (%d row(s))", peer, rows)`, all four tests fail
with the legacy line quoted in the failure output (`the REAL push surface still
prints the pre-fix headline — the LOCK receipt is presented as the row's
arrival`). Restored, they pass.

## Existing tests updated, not loosened

Per constraint 4, three assertions that pinned the old wording were tightened:

- `memory_sync_claim_revert_noop_test.go` — now asserts the lock marker AND the
  denial, not a bare `accepted`;
- `taskid_whitespace_normalize_residual_test.go` — same, for the padded-id path;
- the comments in `memory_sync_row.go` / `taskverbs.Send` / the `send` help that
  quote `accepted (1 row(s))` are the **historical record** of the defect and were
  left standing as such.

## Not Done (honest)

- `gopi worker`'s claim path and the serve tick consume the same primitive but
  print their own lines; their wording was not audited — the epic scoped the
  manual face. A tick-side line that reads as persistence would be the same
  defect one surface over, and is dispatchable as its own task.
- The remote `--claim-remote` handler's own JSON (`ClaimPushResponse.Reason`)
  still carries no "this is a lock" marker. The sender's line now says so, which
  is where the operator reads it; the wire field is machine-facing.
