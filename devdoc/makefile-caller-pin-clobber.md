<!-- doc-class: normative -->
# The Makefile's build-artifact path and the caller's binary pin must not share a name

**Standing rule about master.** The Makefile MUST bind its build artifact to a
repo-PRIVATE variable (`BIN_PATH`), and the two names internal/testutil
publishes as CALLER PINS — `BinaryPinEnv` (`GOPI_BIN`) and `SuiteBinEnv`
(`GOPI_TEST_BIN`) — MUST NOT be unconditionally assigned there; the Makefile may
adopt such a pin only with a DEFERRING assignment (`?=`). This is the single
source of truth for the boundary between the two meanings, and a Makefile that
crosses it fails the build: `TestMakefileNeverClobbersTheCallerBinaryPin` in
`internal/standards/makefile_caller_env_gate_test.go`, whose read-floors refuse
a scan that read nothing. The priority arm it protects is `resolveGopiBin` at
`internal/testutil/run.go:126`.

## Why the two meanings must not be one name

`make` takes the value from its OWN assignment over the one it imported from the
environment, and the imported variable keeps its **export bit**, so every child
of every recipe sees the Makefile's value instead of the caller's. Until
2026-10-11 the Makefile read `GOPI_BIN := $(BIN_DIR)/gopi` — the artifact's path
bound to the name whose entire contract was "the caller names the tested
binary". The consequences chain, and each link is measured:

1. `GOPI_BIN=<absolute> make test-budget PKGS=…` ran every package against the
   **relative** `bin/gopi`;
2. `go test` gives each package its own directory as the working directory, so a
   relative path does not resolve;
3. `resolveGopiBin`'s first arm refuses a pin that does not stat —
   deliberately, with no silent substitution — so the caller's pin became a red
   suite under `make` and a green suite when the same command was run directly.

The fix is a rename, not a behaviour change: the artifact path is now `BIN_PATH`,
and the caller's `GOPI_BIN` passes through untouched. Which of the two a change
may do to the pin — adopt it with a DEFERRING assignment (`GOPI_BIN ?= …`) or
leave it alone — is the gate's message, not a matter of taste.

## Measured evidence (2026-10-11, worktree task/01a12d14513a7b21baeed08e75acd312)

The mechanism, from a recipe's own child rather than from `make -p`'s inference.
The probe include-s the shipped Makefile, so the value under measurement is the
one the repository defines; the pre-fix shape is reproduced by a minimal
makefile carrying exactly the old line (session-scoped tooling, never a repo
artifact — Rule 35/37):

```console
$ GOPI_BIN=$PWD/bin/gopi make -f <session>/probe.mk envprobe     # SHIPPED (fixed)
child-env GOPI_BIN=[/home/user/workspace/gopi-01a12d14513a7b21baeed08e75acd312/bin/gopi]   ← the caller's value, verbatim

$ GOPI_BIN=$PWD/bin/gopi make -f <session>/legacy.mk envprobe    # PRE-FIX SHAPE
child-env GOPI_BIN=[bin/gopi]                                     ← the caller's absolute pin is GONE
```

The two `make -p` shapes show the same thing from the variable table, and are the
quickest reproduction of the defect and of its absence:

```console
$ GOPI_BIN=$PWD/bin/gopi make -p | grep '^GOPI_BIN'   # before the fix
GOPI_BIN := bin/gopi                                  # the Makefile wins, export bit kept
$ GOPI_BIN=$PWD/bin/gopi make -p -f /dev/null | grep '^GOPI_BIN'
GOPI_BIN = /home/…/bin/gopi                           # what the caller actually exported

$ env -u GOPI_BIN make -p | grep '^GOPI_BIN'          # after the fix: no such line (grep exit 1)
$ env -u GOPI_BIN make -p | grep '^BIN_PATH'
BIN_PATH := bin/gopi                                  # the artifact keeps its relative path
```

The reverse arm — that `make` behaves exactly as before for a caller that sets
nothing — is the last line above plus `make build` still writing `bin/gopi`; the
artifact's path and its relative spelling are unchanged, only the variable's name
is.

## Red proof for the gate

The gate was driven red twice, in the two shapes the defect can return in, and
returned green when the Makefile was restored byte-for-byte (sha256
`6fcda3eb…`):

| planted shape | verdict |
|---|---|
| `BIN_PATH` renamed back onto the pin (`GOPI_BIN := $(BIN_DIR)/gopi`) | FAIL — "no assignment to BIN_PATH … a rename back onto the caller pin is exactly what this gate exists to catch" |
| shipped Makefile plus a stray `GOPI_BIN := $(BIN_DIR)/gopi` at EOF | FAIL — names `Makefile:147  GOPI_BIN := $(BIN_DIR)/gopi  (:=)` |

## A pre-existing defect this fix EXPOSED (follow-up row `01a12d2c-a3a6-7c5d-b2a1-fa03081027d5`)

`TestGopiBinPublishesTheTreeBuildToChildren` (`internal/testutil/gopibin_pin_test.go:60`)
FAILS whenever `GOPI_BIN` names a usable binary — it clears only `GOPI_TEST_BIN` and
then asserts the arm that PUBLISHES `GOPI_TEST_BIN`, an arm `GOPI_BIN` short-circuits
(`resolveGopiBin`, arm 1). The file is byte-identical to the base commit in this
worktree, so the rename did not cause it: the clobber used to keep `GOPI_BIN` empty,
and the fix removed that accident. Measured, both ways, plus the fact that the same
short-circuit leaves the package's other e2e cases silently `t.Skip`ped:
`artifacts/evidence/08-preexisting-test-defect.txt` in the finding task's session
directory. Filed as its own row rather than fixed here (Rule 41: evidence and a row,
a separate change with its own review).

## What the gate does NOT do

It is not a lint over every variable a Makefile may own: only the caller pins
whose definition lives in `internal/testutil` (referenced, never re-spelled —
Rule 14) are protected, and `= ` on any other name stays the Makefile's business.
It also does not replace Rule 31's execution requirement: a green gate says the
name is free, and only a real `make` run says the surface works.
