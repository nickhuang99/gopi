<!-- doc-class: historical -->
# devdoc: the counter's THIRD STATE — an unmeasurable entry is reported, never dropped

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator
> owns launching workers; an agent must never run `scripts/gopi-worker-up.sh` to
> start one (submit the work instead). Read-only use — `--count` — is what this
> document's evidence used.

## The defect (bug 01a132a7a4ef743b, measured 2026-10-13)

The image-prefix judge (`devdoc/worker-image-identity.md`, commit `a2b62605e`)
identifies a gopi process by `/proc/<pid>/exe` with `argv[0]` as a fallback, and
then **filters** on it:

```sh
[[ "$image" == "$IMAGE_PREFIX"* ]] || continue
```

That line answered for two different situations with one word. A process whose
image is readable and is a *foreign* program is a **verdict** — it is not this
program. A process whose image cannot be read *at all* (no `exe` link, and an
empty `cmdline`, so `argv[0]` is empty too) is a **missing measurement**. The
filter treated the second exactly like the first, and the entry left the scan:
not counted as a worker, not counted as other, not printed anywhere.

The function's own header, 40 lines above that line, already stated the rule it
was breaking:

```
# Rule 15: an unreadable factor degrades to "unknown", it never silently removes
# a live entry
```

**The shape is real, not hypothetical.** A Z-state process is exactly it: the
kernel reports an empty `cmdline` and `readlink /proc/<pid>/exe` fails, while
`comm` — the name it will not let go of — still says `gopi`. Measured on this
host with the parent task's holder probe (a child started and never `Wait()`ed):

```
comm                = [gopi]
state / ppid        = [Z 502376]
cmdline bytes       = 0
exe readlink        = []
```

(The zombie's pid differs on every run — it is a fresh child each time. What is
stable is the SHAPE: `comm=gopi`, state `Z`, `cmdline` 0 bytes, no readable `exe`.
`artifacts/zombie-third-state-full.log` is one archived run of the script whose
numbers are quoted below; re-running it produces the same verdict with new pids.)

Three judges on **one frozen snapshot** of that moment, per the parent task's
twin-judge method:

| judge | workers | others |
|---|---|---|
| OLD (peer master: `comm == gopi`) | 2 | **5** |
| PRE (parent `a2b62605e`: image prefix) | 2 | **4** |
| NEW (this branch: image prefix + third state) | 2 | **4**, plus `unconfirmed gopi processes: 1 (pids: 502383:4768725)` |

The operator reading PRE's line saw `other gopi processes: 4` and had no way to
know that a live entry — the one the comm judge had counted a moment earlier —
was missing from it. The parent task measured the same difference on a frozen
host snapshot: old `others=6`, new `others=5`, the missing pid `3092881`.

## The fix

An entry is **unconfirmed** — its own third state — exactly when BOTH image
channels failed (no readable `exe`, empty `argv[0]`) while `comm` still names the
process `$PROC_NAME`. It is:

- **not a worker** — the argv shape cannot be read either, so the verb test has
  nothing to compare; counting it would be a fabricated measurement;
- **not other** — the channel that *defines* "other" (the image) never answered;
- **reported**: its own count and its own `pid:starttime` list, on its own line,
  **unconditionally** — including at zero, because a line printed only when it is
  non-zero cannot be told from a line nothing measured (Rule 15a).

```sh
if ((imeasured == 0)); then
  if [[ -n "$name" && "$name" == "$PROC_NAME" ]]; then
    unconfirmed=$((unconfirmed + 1))
    unpids+=" $pid:$(read_incarnation "$d")"
  fi
  continue
fi
```

`comm` is the right evidence to require here, and it is deliberately the
**weakest** channel: unconfirmed claims nothing about *which program* runs, only
that the process is named `$PROC_NAME` and could not be measured further. Reading
`comm` follows the process's own name, so no entry the comm judge would have
missed enters the count — the third state can only explain a difference, never
invent one.

### The output contract (five lines, order is the interface)

A shell function cannot return an array, so the line ORDER is the contract:

| line | value |
|---|---|
| 1 | worker count |
| 2 | other count |
| 3 | **unconfirmed count** |
| 4 | worker `pid:starttime` identities |
| 5 | **unconfirmed `pid:starttime` identities** |

Any caller that slices only the first lines re-creates the silent drop one level
up — the state is measured and then discarded before an operator can read it.
`TestWorkerUpCountOutputIsFullyRead` fails such a caller by name.

The operator-facing line names the channel that failed, so the measurement is
reproducible by hand:

```
unconfirmed gopi processes: 1 (pids: 445450:4739680) ((exe and argv[0] both unreadable — 镜像通道未确认，既非 worker 也非 other，不静默丢弃))
```

And the arithmetic that was impossible before now closes on screen:

```
others_old = others_new + unconfirmed        (measured: 5 = 4 + 1)
```

### The refactor this forced

The incarnation reader (starttime, field `$STAT_FIELD` of `stat`, reached by
parsing past the LAST `)` because `comm` may contain spaces) was inlined in the
worker branch. Both pid lists are stamped with it, so it became ONE function,
`read_incarnation` — the alternative was a second copy of a field-index mapping
that `internal/procname` documents as a "two readers must agree" fact.

`exe` and `stat` also became named shell constants (`EXE_NAME`, `STAT_NAME`),
asserted against `procname.ExeName` / `procname.StatName` by the gate: a bare
`"exe"` in the `readlink` call is exactly the literal two sides can drift on.

## Evidence

- `twin-judge-frozen.log` — three judges over one synthetic root holding the four
  shapes A/B/C/D (the epic's fixture). Confirms the four buckets and that shape D
  lands in the third state with its pid.
- `zombie-third-state-full.log` — a REAL Z-state process held by a
  never-`Wait()`ing holder, frozen into one snapshot, judged three ways. The
  identity `others_old = others_new + unconfirmed` HOLDS (5 = 4 + 1) and the
  zombie pid appears in the third-state list (502383 in that run).
- `internal/testutil/procname_gate_test.go` — `TestWorkerUpReportsTheThirdState`
  asserts the four shapes BY BUCKET against the fixture, and closes the three-bucket
  identity; the fifth entry of the fixture (1016) is the measured defect itself.

## What this does NOT change

The launch path. `--count` / `--check` / `--install` / `--dry-run` semantics,
the setsid+nohup launch, the log contract and `verify_launched_workers` are
untouched; the third state only adds lines to a read-only report. A caller that
counted workers by reading line 1 still reads line 1.
