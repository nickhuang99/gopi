<!-- doc-class: historical -->
# The retired-verb test-driver gate, widened to local-helper call sites

Task `01a10ef3-7fbd-7e70-a12c-006c3c99532c`, on peerbox. Successor to
`01a10e03-9daa-76ea-8c65-f0ea4e9c8f28`, whose gate this widens.

## Why this exists

Task `01a10e03` shipped the anti-regression gate
`cmd/agent/retired_verb_test_driver_gate_test.go` —
`TestNoTestDrivesTheRealBinaryWithARetiredWord` — so that no `*_test.go` could
again drive the REAL binary with a RETIRED word as `argv[0]` (the retirement
had produced 263 failing tests across 5 packages, every one of them a stale
DRIVER rather than a missing feature).

That gate reads the two stdlib program starters — `exec.Command` /
`exec.CommandContext` — **at the call site**. Its own report recorded the limit
honestly rather than hiding it:

> **The gate's coverage is a floor, not a ceiling.** It reads the two stdlib
> program starters. A test reaching the binary through a LOCAL HELPER is
> invisible to it — and that is measured, not hypothetical: two such sites in
> `cli_checkpoint_usage_dispatch_test.go` were found by *running the suite*, not
> by the gate.

This task closes that blind spot for the shape that is syntactically decidable.

## The predicate

A helper call site raises one question: *does the word I hand this helper reach
`argv[0]` of a program start?* Two shapes are recognisable, and the distinction
IS the gate:

| shape | helper body | call site |
|---|---|---|
| **VERBATIM** | `exec.Command(bin, args…)` — the caller's argv is forwarded unchanged | **gated**: the caller's first word IS `argv[0]` |
| **TRANSFORMED** | `exec.Command(bin, familyAddressArgs(args)…)` — handed to another call first | **not gated**: which word reaches `argv[0]` is undecidable from here |

A helper enters the table only when **the program arrives as a value, not as a
string literal**. That is what keeps `git`, `sleep`, `bash`, `ps` and
`systemd-run` helpers out: a call site handing them `log` means `git log`, and
gating it would be a false positive. Every gopi driver in this repo passes the
built binary as a variable (`bin`, `s.bin`, `testutil.GopiBin(t)`).

### The TRANSFORMED exclusion is the retirement's own repair, not a loophole

`familyAddressArgs` (`cli_family_address_test.go`) is the ONE door mapper that
turns a retired flat spelling into the family address that dispatches it. A gate
that flagged its call sites would refuse the very mechanism the retirement ships
to fix a stale driver — a false-positive machine, and a gate whose false
positives are noise gets pinned away rather than obeyed.

It is recognised **by predicate** (the argv is handed to a call, not to exec),
never by naming `familyAddressArgs`. Naming it would be the recognition-set rot
this repo has already paid for once: a hardcoded set of helper names makes every
call site of a newly added helper unscanned with no red test
(`TestSpawnersPinTheRepoContext`, task `fix/repo-context-gate-wdrun`).

## Read-floors (Rule 15)

A helper table that silently came back EMPTY would leave every helper call site
invisible while the gate still printed a clean line — a scan that never ran
wearing a pass. Three floors, each measured rather than fitted:

| floor | value | measured |
|---|---|---|
| Go files walked | 1500 | 2143 |
| verbatim forwarders recognised | 3 | 26 |
| parsed `_test.go` files (pre-existing) | 1200 | 1458 |
| refused vocabulary (pre-existing) | 25 | 36 |

## Red proof — two-sided, and asserted on the WORD

`TestRetiredVerbGateHelperArmIsRedOnTheRealTree` retires one word inside a
sub-run alone and runs BOTH scans over the same real corpus:

```
red proof (widened arm): retiring "chat" alone is INVISIBLE to the direct arm
(0 findings) and turns 67 helper-reached call site(s) red across 26 verbatim
forwarder(s) (e.g. testsuite/chat_banner_band_clean_pty_test.go:82: the real
binary is driven with the RETIRED word "chat" as argv[0], through the local
helper startPTYChat — use `chat [flags]`)
```

The second half is what makes this a measurement of **coverage** rather than of
activity: proving only that the widened arm finds something would leave "did the
widening add anything?" unanswered.

**An earlier draft of this proof was green for the wrong reason** and is
recorded here because the failure mode is the interesting part: it retired
`worker` and asserted only `len(findings) > 0`, and it passed — counting findings
produced by OTHER real violations that happened to be in the tree, while
measuring nothing about `worker` at all. Asserting the word by name is what makes
that class of accidental pass impossible.

Fixture proofs cover the four boundaries (`TestRetiredVerbGateHelperArmFixtureProofs`):
verbatim flagged · transformed not · literal-program helper not · the escape
marker suppresses exactly one line.

## What the widening found: four real violations on master

Running the widened arm over the tree found four stale drivers the narrow gate
could not see — and **two of them made their tests RED**:

| file:line | word | status on master |
|---|---|---|
| `internal/shot/units_test.go:197` | `scratch-gc` | **RED** |
| `internal/shot/units_test.go:226` | `scratch-gc` | **RED** |
| `testsuite/chat_info_pty_test.go:215` | `log` | stale driver |
| `testsuite/signal_log_test.go:96` | `log` | stale driver |

Measured, not inferred — the red pair really did fail:

```
$ go test ./internal/shot/ -run 'TestScratchGCCLIFaceReclaimsCaptureUnits|TestScratchGCDryRunReportsTheCapturePlan'
    units_test.go:228: gopi tool scratch-gc --dry-run exited 1
        gopi: `scratch-gc` is RETIRED — use `gopi tool scratch-gc` …
--- FAIL: TestScratchGCDryRunReportsTheCapturePlan (0.01s)
```

and the same invocation after the repair:

```
$ go test ./internal/shot/ -run 'TestScratchGCCLIFaceReclaimsCaptureUnits|TestScratchGCDryRunReportsTheCapturePlan'
ok  	github.com/nick/gopi/internal/shot	17.540s
```

So the widened gate did not merely add coverage on paper: it **found two failing
tests on master** and named them at the source.

### The same class, found by the NARROW arm

The narrow arm was unrepaired on master too, and caught a fifth site:
`testsuite/shot_surface_chat_test.go:69` drove the refused flat `shot` directly.
Repaired the same way.

### The Rule 13 half: two words had no owner

`shot` and `scratch-gc` were reachable in a test only as bare literals — the
defect class Rule 1/13/14 names (a word with no constant to reference). Both now
live in `internal/cliverbs` (`Shot`, `ScratchGC`) with `cmd/agent`'s
`CliVerbShot` / `CliVerbScratchGC` aliasing them, so a rename moves every
spelling. This is the same repair task `01a10e03` made for `shot` — it did not
reach master, and this branch re-applies it.

**Note on the two `internal/shot` sites and `internal/shot`'s own import
direction**: `internal/shot` is a leaf package and now imports
`internal/cliverbs` in a TEST file. `cliverbs` is a constants-only leaf (Rules
13/14 require exactly that), so no cycle is created; the production direction of
`internal/shot` is untouched.

## How a violation is reported

```
internal/shot/units_test.go:197: the real binary is driven with the RETIRED
word "scratch-gc" as argv[0], through the local helper RunGopi — use
`gopi tool scratch-gc`
```

The message names the **helper** as well as the site, because "which helper" is
what tells the reviewer whether the fix belongs at the call site or in the
helper.

The TRANSFORMED arm is not taken on faith. The claim it makes is that a call site
whose argv goes through `familyAddressArgs` is **legal by decision, not invisible
by oversight** — so the mapper's output was run against the real binary:

```
$ $B proxy -h
gopi: `proxy` is RETIRED — use `gopi tool proxy` (`gopi tool proxy -h` for its help). …

$ $B tool proxy --upstream openai --openai-base-url http://127.0.0.1:1/v1 \
      --openai-model stub --openai-key stub --listen 127.0.0.1:45999
gopi tool proxy: listening on 127.0.0.1:45999 (upstream=openai base=http://127.0.0.1:1/v1 model=stub)
```

The flat word is refused; the address the mapper rewrites it into binds its socket
and prints its banner. That is what makes "argv reached exec UNMODIFIED" the right
discriminator rather than "argv mentions a retired word somewhere".

## Residual limits — stated, not implied (Rule 26's corollary)

The predicate is syntactic and always has a boundary. These stay OUT of scope:

1. a word that reaches `argv[0]` through **two or more** helper hops;
2. an argv **built in a loop** or assembled conditionally;
3. a helper reached through a **closure held in a variable**;
4. a helper whose argv position depends on a **runtime branch**.
5. a same-named method on two different receivers collapses into one table
   entry (the method arm matches on the selector name alone) — in this package
   the safe direction, since a recognised name is still only flagged when its
   parsed body is VERBATIM.

## Reproducing

```bash
go test ./cmd/agent/ -count=1 -run 'TestNoTestDrivesTheRealBinaryWithARetiredWord|TestRetiredVerbGate' -v
```
