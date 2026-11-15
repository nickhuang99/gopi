<!-- doc-class: normative -->
# The stop receipt outlives the reader (epic 01a17df9-524a)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The defect

`gopi worker --stop` has two files: `TERM_<pid>` (the request, operator→worker)
and `ACK_<pid>` (the receipt, worker→operator). The receipt is the ONLY evidence
that a stop was ever **acknowledged** — the request file says someone asked, and
nothing else on the disk says anyone answered.

The sender deleted it the instant it printed it:

```go
// cmd/agent/cli_worker_ack.go, before this change
if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) { … }
```

So the printed `req_id` / `acked_at` / `acked_by` lived only in the operator's
scrollback. Measured 2026-10-27 during an investigation into a worker that would
not stop: the control directory held no `ACK_*` at all, and the question "did
that worker confirm the request, and at what time?" could not be answered from
the machine — only from a terminal that had scrolled away. The channel was
destroying its own audit trail as its last act.

(It also sat beside the opposite failure in the same family: `TERM_<pid>` files
are never swept — the oldest was 13 days old — tracked separately as
`01a17df9-1fcd`.)

## The shape

**Reading archives; it never deletes.**

```go
func (p *workerAckPoll) consume(a workerAck) {
    if _, err := archiveWorkerAck(a); err != nil { …report… }
}
```

`archiveWorkerAck` is one `rename(2)` inside the control directory:

```
ACK_542818                        the live name — what the poll reads
ACK_542818.1234.consumed          the archive — <ACK_ name>.<req_id><suffix>
acks-consumed.log                 one appended index line per archive
```

Three properties, each doing work:

1. **The live name is free at once.** The poll reads only the live namespace
   (`workerAckIsLiveName`), so a receipt already read can never answer a LATER
   request from the same pid — the failure that would let a second `--stop`
   report "ACKNOWLEDGED" without anyone having read the new request.
2. **`req_id` is in the archived name.** A pid is recycled, so two stops of two
   different workers can share one pid number; a name keyed only on the pid would
   overwrite one handshake's evidence with another's.
3. **The index line is the durable fact.** The file is collected after
   `workerAckArchiveRetention`; the line is not swept at all. A reader months
   later gets `unix_ts  req_id  acked_by  acked_at  mode  exit  file`.

## Reclaim: one rule, two ages, on a tick that already exists

`workerAckOrphanStaleAge` (7d) and `workerAckArchiveRetention` (30d) are both
ages of ONE predicate, `ackRetentionElapsed` — because there is exactly ONE
thing the sweep ever does to a receipt, and it is *archive it*, never *delete it
unread*:

| the sweep finds | age | what happens |
|---|---|---|
| a LIVE receipt nobody read (the sender died: Ctrl-C, a killed unit, OOM) | > 7d | **archived** — the same act the reader performs, so the evidence survives |
| an ARCHIVED receipt | > 30d | collected |

That is the reason the crash path needs no second rule: the normal path archives
on read, the crash path archives by age, and the age rule collects whatever has
been archived. A receipt the sweep finds is never destroyed without first having
been read by somebody.

**Both ages are measured from the receipt's own `acked_at`, never from mtime** —
`rename(2)` preserves mtime, so an mtime-based retention would collect a
just-archived receipt at the instant it was archived. An unparsable stamp falls
back to the mtime (an older or newer writer still gets bounded storage), and that
fallback is stated rather than silent.

**It rides `scratchGCTick`**, which the worker/merger tick already drives — same
cadence, same `scratch_gc` off switch, same fail-open discipline, one `ReadDir`
of a directory holding a handful of ~200-byte files. **No new scheduler, no new
resident process, no new configuration key** (Rule 35: a dependency must justify
itself; a hygiene pass that already ticks has no justification left to make).

The call site is the LAST arm of that tick on purpose: every arm above it can
return early on its own refusal, and a bounded control directory must not depend
on an unrelated sandbox root being configured.

## Compatibility

- A receipt written by an OLDER worker is archived exactly like a new one — the
  archiver moves the FILE, and never requires the parse to succeed (an
  unparsable body is still evidence).
- An OLDER `--stop` deletes as before; the archive arm is harmless to it.
- Nothing new is written where a reader looks for "the answer to my request":
  the live name is the only name the poll reads, and it is the only name
  `workerWriteStopAck` writes.

## The check

```bash
go test ./cmd/agent/ -run 'TestWorkerAck|TestWorkerStop' -count=1
```

- `TestWorkerAckConsumeArchivesTheEvidence` — every field (req_id, req_ts,
  acked_by, acked_at, mode, round, exit) survives the consume, the live name is
  gone, and one index line records the fact.
- `TestWorkerAckArchivedReceiptDoesNotAnswerALaterRequest` — after a consume, a
  poll finds nothing at the live name; the worker's writer still produces a LIVE
  receipt for the next request, and the archive still holds the first unchanged.
- `TestWorkerAckSweepRetiresByAgeOnOneRule` — the four cells of the table above,
  in one pass: aged-unread → archived, fresh-unread → untouched, aged-archived →
  collected, fresh-archived → kept; the index is never swept.
- `TestWorkerStopTwiceArchivesBothReceiptsE2E` — the acceptance: the real CLI,
  twice, two daemons, two archives whose `req_id`s differ.
