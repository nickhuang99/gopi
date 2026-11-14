<!-- doc-class: normative -->
# Retired verb spellings in `scripts/` — the sweep, and the instrument that found them

## What was wrong

The verb-retirement epic (2026-10-05, `cmd/agent/cli_verb_aliases.go`) removed the
bare top-level verbs from dispatch. A removed word now prints a RETIREMENT NOTICE
naming its family door and exits non-zero — **it does not silently fall through to
the agent** (that trap is why `scratch-gc-watchdog.sh` probes with `-h` at all: an
unknown word with `-h` starts an agent run, and the watchdog refuses to invoke a
binary that does not answer a usage head).

`scripts/` was never swept. Three instruments that the demo/evidence pipeline
depends on were therefore measuring nothing:

| file | the defect | what it reported |
|---|---|---|
| `scratch-gc-watchdog.sh` | invoked `${BIN} scratch-gc` | `FAIL ... does not carry the scratch-gc verb`, rc=1 — the scratch tree was **not swept**, every tick, forever |
| `demo-coverage.sh` | ~20 probe rows spelled the retired words | rows recorded the **refusal** (`rc=1`) and counted it as coverage |
| `demo-materials-coverage.sh` | `"$BIN" sandbox -h` / `checkpoint -h` for the denominator | **0** subcommands for both doors, carried silently into `total` |

## The three shapes, and why one fix is not the other

1. **A call site** (`"$BIN" sandbox create`) — a real invocation of a word the
   binary refuses. The remedy is the door: `"$BIN" tool sandbox create`.
2. **A help probe** (`"$BIN" sandbox -h`) — the same, but the failure is a
   SILENT ZERO rather than an error, because the probe's output is *counted*.
   The remedy is the door **plus** a refusal to count a zero (Rule 15): a count
   that silently becomes 0 is the defect, not the low number.
3. **A prose citation** (`` `gopi annotate` 把标签画在框上方 ``) — a MENTION, not
   an invocation. It is history and remains legitimate; the file's own new
   header comment cites the retired spellings for that reason.

## The instrument, and the false green it produced first

`artifacts/scan-old-spellings.sh` takes its vocabulary from **the binary's own
verdicts** (Rule 31): every word that appears in command position is run as
`gopi <word> -h` inside a private profile, and the ones whose output carries the
retirement notice are the retired set. The scan then reports command-position
hits, with shell comments stripped.

**It reported "0 sites" against the master baseline — which was wrong**, and the
baseline is exactly the control that caught it (Rule 39's "a scan that found
nothing must be distinguishable from a scan that never ran"). The cause: the
pattern was `(…|\$BIN|…)[[:space:]]+word`, which does not match **`"$BIN" word`** —
the closing quote sits between the variable and the word. That is the commonest
call shape in these scripts, so the scan was blind to most of the tree while
reporting a clean result.

With `"?` added to both patterns the instrument went red on master (13 sites)
and green on this worktree (0), which is the only shape in which its verdict
means anything. Points worth keeping:

- **A scan needs a control tree.** `git worktree add --detach <path> master` is
  the cheap one — it does not read the working copy, and it is the only way to
  say "the instrument can still see the defect" after the fix.
- **Its own vocabulary must not come from a source that can silently shrink.**
  An earlier version derived the word list from `cli_verb_aliases.go` and parsed
  the file badly, producing an empty vocabulary and an "OK". Deriving it from
  the binary's verdicts has the same failure mode in principle, which is why the
  script refuses to report clean when it classifies zero words (exit 2).

## Files

`scripts/{scratch-gc-watchdog,demo-coverage,demo-materials-coverage,demo-memory-loop,
voice-capture-failmatrix,voice-e2e-hp,voice-out-e2e-stub,demo-memory-loop-figs}.sh`

## Not done

- `scripts/materials/env-catalog-audit.sh:78` still calls the flat
  `gopi sandbox run` with the silent `2>/dev/null` that hides the failure. The
  scan reports it; it is outside this task's scope and is dispatched separately.
- The four red `internal/standards` tests
  (`TestCapdemoExternalBundleAnchorsResolve`, `TestCommandsDocGeneratedBlockMatchesSource`,
  `TestCommandsDocGateRedProofStaleGeneratedBlock`, `TestDocClassBannerMatchesManifest`)
  are red on **master** too — measured in a detached worktree of master. They
  belong to other rows; nothing here touches them.
