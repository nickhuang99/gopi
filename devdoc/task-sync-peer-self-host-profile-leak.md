<!-- doc-class: historical -->
# A peer leg aimed at THIS host writes the OPERATOR's profile — and the id-resolver family is closed

Task: `01a117c9-1111-7111-8111-111111111111` (branch
`task/01a117c9111171118111111111111111`, 2026-10-08). Measured at master
`087f506df`.

This note records two results from one review run. They are kept together
because the first is *why* the second had to be answered.

## What happened (the concrete failure)

A review task running a probe inside a private sandbox
(`sandbox-rid-review-e3-012959-afc2d91e`) executed:

```bash
export GOPI_SYNC_PEERS=peerbox
GOPI_HOME=$SB gopi task sync --peer peerbox --next-eligible <FULL row id>
```

The probe's own ledger got the row (`5 task(s)`, both fixtures present). **The
operator's ledger got it too** — and the copies there carry the `from-peerbox`
provenance tag, which is written only at *import*, never by a local submit.

```bash
$ gopi task list --status pending            # the OPERATOR ledger, read-only
#01a117d7-2222-…  [pending]  rid-review e4 fixture: sibling row
#01a117d7-1111-…  [pending]  rid-review e4 fixture: one row   [next]
```

That `[next]` is the whole defect: the very next claim the dispatcher made was a
probe fixture, and this run is standing in the worktree it was given for it.

## The mechanism — two halves, both measured

**Half 1: the alias resolves to this host.** `~/.ssh/config` on peerbox carries
`Host peerbox → 192.168.7.6`, which *is* peerbox's own `wlo1` address. So
`--peer peerbox` is not a remote machine: it is an `ssh` to self.

```
$ ssh peerbox 'echo GOPI_HOME=[$GOPI_HOME]; command -v gopi'
GOPI_HOME=[]
/usr/local/bin/gopi
```

**Half 2: nothing binds the profile when `--remote-home` is absent.** The
peer-side argv is built by `syncRowSSHExchange` (`internal/tools/memory_sync_row.go`),
and the `GOPI_HOME=… PATH=…/bin:$PATH` prefix is added **only** when
`--remote-home` is non-empty (`syncRowRemotePrefix`). Absent it, the peer's own
login shell decides — which, for an alias pointing at self, is the operator's
profile. The comment on `syncRemoteHomePrefix` states the intent ("a cross-machine
experiment must not write the peer operator's ledger"); on the self-host case the
mechanism has nothing to bind, because the caller declared no home and ssh
happened to land here.

The environment makes this easy to hit: a probe that exports `GOPI_SYNC_PEERS`
and relies on `--remote-home` being "the peer's business" writes real rows into
real work — silently, exit 0, with the sender's own receipt reading `pushed=N`.

This is **not** the already-known empty-`GOPI_HOME` arm (a `sandbox create` that
failed and left `GOPI_HOME` empty in the *caller*): here the caller's profile was
correct and the leak happened on the *peer's* side of the wire.

## The id-resolver family is closed — do not re-open it

The row this run inherited asked whether a *sibling door* to the transfer leg
still accepted a truncated task id. Re-measured on current master, with a
private sandbox ledger (Rule 34) and the worktree's own binary:

```
--next-eligible <FULL>          exit 0  {"eligible":true,"row":<that row>}
--next-eligible <35-char>       exit 1  "not a full id — it is a prefix of <full>; …
                                         resolved against THIS ledger before any peer is contacted"
--next-eligible <3-way prefix>  exit 1  "not a full id — it is ambiguous: 3 identities share that prefix (…)"

--handover <FULL>    exit 0 {"signed":true}          --handover <prefix>  exit 1 (same refusal)
--handover t-not-a-uuid exit 0 "absent from this ledger"

task send --task-id <FULL>    exit 0            task send --task-id <35-char>  exit 1 (same refusal)
task send --task-id <3-way>   exit 1 "it is ambiguous: 3 identities share that prefix"
```

The **move leg** (`task send --task-id`, the *other* value-taking spelling of the
same family) answers with the identical sentences, because it is the same
resolver: there is no sibling door. A sweep of the third spelling was the last
unchecked corner and it is closed too.

and the plain readers are stricter still — **exact match, no prefix at all**:

```
$ gopi task show  01a117c3-aaaa       → no task with task_id="01a117c3-aaaa"     EXIT=1
$ gopi task status 01a117c3           → no task with task_id="01a117c3"          EXIT=1
$ gopi task for-branch 01a117c3-aaaa  → no owner …                               EXIT=4
```

So every door is one of two shapes: an **exact match** that answers "no task with
task_id=…", or the **one `internal/fullid` face**, which refuses a truncated
operand and *names the candidates it refused among*. `--next-eligible <ID>`
reaching that face at all is the fix in `388a63c31`, which is an ancestor of
`087f506df`, and it works in both the bare (`--peer`) and prefixed spellings.

The one answer that looks like a gap is not one: a **non-UUID** operand
(`t-not-a-uuid`) gets the ordinary "not in this ledger" (exit 0) rather than a
refusal sentence. That is `fullid.Refusal`'s deliberate shape — the message is
chosen by `fullid.CanBePrefix`, so a truncated operand with known candidates is
refused *with* them, while an unrelated operand keeps the caller's ordinary
"not found" answer. It is never a permission to resolve a prefix.

## Disposition

- The two `pending` fixture rows (`01a117d7-…1111`, `01a117d7-…2222`) were
  **cancelled on the operator ledger with their provenance in `--reason`** — not
  deleted: the ledger is append-only, and the row *is* the evidence of the leak.
- The two e3 fixtures had already been claimed as real work before this run; they
  hold live grounds and are the running runs' own to finish. Their rows are probes,
  not operator work, and that is recorded here.
- The leak itself is a **defect of the peer leg, not of the probe that tripped
  it**: it is filed as a bug row (tags `bug`) naming the self-host alias and the
  absent `--remote-home` default, with this document as its evidence.
