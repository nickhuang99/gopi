<!-- doc-class: normative -->
# The G6 help-surface gate, the forwarded DISPLAY, and the 11 unaccounted forms

Class: report. Scope: `cmd/agent/cli_tools_maintenance_help_surface_*_test.go`,
`cmd/agent/cli_contracts_test.go`, `cmd/agent/cli_scratch_gc_stubs.go`, and
`devdoc/gopi-tools-maintenance-help-surface-inventory.md`.

Measured 2026-10-01 on `peer1`, branch `task/01a0f4b1daaa7acab5a88b0edb1685bf`,
worktree `/home/user/workspace/gopi-01a0f4b1daaa7acab5a88b0edb1685bf`.

## What was red, and what the red actually was

`TestToolsMaintenanceHelpSurface*` failed in four places, and it is worth being
precise, because the four have three different causes:

| assertion | count | cause |
|---|---|---|
| `InventoryMatchesDerivation` | 11 forms | the help advertises them; neither map knew them |
| `EveryFormIsTestedOrBlockedWithAReason` | same 11 | the same defect, seen from the completeness side |
| `FormsAreTyped` | 3 subcases | **not** a table defect — an inherited `DISPLAY` |
| `FormsAreTyped` | 0 after the table fix | — |

The first two are ONE defect (a form in neither map fails both), and the recipe
for it is the one this suite's own memory entry #5260 records: **type the form at
its first local refusal, then regenerate the table**. The third is the finding
worth keeping.

## The finding: an inherited DISPLAY made a probe hang to its bound

This host's `DISPLAY` is `localhost:15.0` — an ssh `-X` forward. Measured:

```
DISPLAY=localhost:15.0  gopi screen_capture --out X → 28.3s / 49.4s (two runs)
DISPLAY=:1              gopi screen_capture --out X → 0.08s / 0.18s
DISPLAY unset / empty   gopi screen_capture --out X → 0.01s, `no X11 display`
```

TCP `localhost:6015` connects in 0.05s and then answers nothing (`recv` → `b''`),
so `x11` blocks on its own io bound (20s) while the probe's contract budget is 15s
(`cliContractTimeout`). The suite was killed with **no output at all** — i.e. with
exactly the signature `g6TimeoutDiagnosis` reserves for "the argv fell through to
the agent loop", which is a false positive of the kind that message exists to
prevent.

Root cause, in one sentence: **a probe that does not need a display still
inherited one, so its outcome was a property of the operator's session instead of
the case's fixture.** The remedy is not a longer bound (that would mask the same
mistake for the next host): the suite's base environment now MASKS `DISPLAY`
(`cliContractMasksAmbientDisplay` in `cli_contracts_test.go`), and the cases that
exercise an X11 guard pin the display themselves (`g6NoX11Env`) — after the case's
own env, so the case owns the decision and the default is only what an
unopinionated case gets.

The same measurement produced the marker constant `g6NoX11Marker` ("no X11
display"), shared by the `act`, `voice_input --self` and `screen_capture` case
families so a reworded guard reddens all of them at once.

## The 11 forms: what each one is asserted at, and why

Every one of them was added as a TYPED case at the form's **first local
refusal** — no `g6BlockedForms` entry was added (that would have bought green
without a claim), and no fixture pretends to be a device or a window it cannot be.

| form(s) | first local refusal | how it is made the CASE's state |
|---|---|---|
| `act type --text 'ls -l\n' --self`, `act type --text 'x' --self --dry-run`, `act key --key return --self` | `no X11 display` | display pinned empty by the case (`g6NoX11Env`) |
| `voice_input --client --self` (+ `--seconds 8`, `+ --display :57`) | `device alsa:plughw:CARD=…,DEV=0 is not reachable … run … --list-devices` | `PATH` prefixed with a stub `arecord`/`pactl` set: the enumerator lists ONE card, the recorder then fails to open it (the measured hotplug shape) |
| `archive --reclaim [--repo PATH]` | (no refusal — it succeeds) | a fixture repo carrying a branch AND its `archive/<branch>` tag: the tag is reclaimed and the branch is gone from git afterwards |
| `archive --verify-commit <rev> [--repo PATH]`, `archive --verify-commit e1701ac8` | `UNRESOLVED — git rev-parse could not resolve` | the falsifier is asked about a revision the fixture never created |
| `scratch-gc stubs`, `… --dry-run`, `… --yes` | (no refusal — the arm succeeds) | a leaked ground in scratch's own shape: a `gopi-<id>` dir whose `.git` FILE points at a gitdir under a `suite-…-run-…` directory that no longer exists, so the verdict is the sweep's real `collectable` |

Two details that cost time and are worth not re-deriving:

1. **`--self`, `--seconds`, `--display` and `--client` are FLAG SPELLINGS**, and
   the cases take them from the production constants (`voiceFlagSelf`,
   `voiceFlagSeconds`, `voiceFlagDisplay`, `voiceFlagClient`) rather than typing
   `"--self"` (Rule 14). The first run of the new `voice_input` cases passed the
   values WITHOUT the leading `--`, so the argv read them as positional audio
   files and the engine answered `failed to open/decode client` — a plausible
   failure that says nothing about the form.
2. **The `scratch-gc stubs` log line's `dry-run=` field is a contract three
   readers depend on** (the operator, whatever greps the shared format, and this
   gate's case), so it is now `scratchGCStubsDryRunField` in
   `cli_scratch_gc_stubs.go`, beside `scratchGCStubsVerdictWouldRemove`, which the
   per-candidate verdict line (`keep` / `would-remove`) had been spelling inline.

## Not done (honest)

- **The happy path of the three `--self` forms and of the three `voice_input
  --self` forms is not driven.** Both need something the suite cannot manufacture:
  a window that hosts the probe process (resolved by pid against
  `_NET_WM_PID`), and a microphone whose transcript is what gets typed. What is
  asserted is each form's first refusal, which is a real answer of the resolver or
  the device policy. What was MEASURED as the next refusal, for whenever someone
  runs this on a host with a live display: `act … --self` then answers "no window
  on display … carries the pid of this process or of its ancestry" and prints the
  ancestry it walked plus the enumerated `_NET_WM_PID` set — i.e. the addressing
  resolver refuses by name rather than guessing a window.
- The inventory's **Totals** line moved (123 → 147 rows, 100 → 109 typed) with the
  surface; the coverage FLOORS in the gate were deliberately left at 85/28/115.
  They are a floor, not a target, and raising them here would only add a second
  number to keep in step with a surface that grows on its own.

## A second defect the scoped run found: a host-dependent tick assertion

`TestScratchGCTick_ThrottleAndFailOpen` asserts the FIRST tick's return value is
exactly 1, and `scratchGCTick` returns the SUM of its arms (home + base + capture
units + sandbox). Its fixtures pin the home and base arms; the SANDBOX and
CAPTURE-UNIT arms read host configuration, so an abandoned `sandbox-*` the
operator's own root happens to be holding past its 72h age is counted into the
assertion. Measured 2026-10-01 with the real root in force:

```
--- FAIL: TestScratchGCTick_ThrottleAndFailOpen (1.61s)
    scratch_gc_tick_test.go:104: first tick removed 2, want 1
    [worker] [scratch-gc] removed 1 abandoned sandbox(es) older than 72h0m0s from /home/user/workspace/gopi-sandbox
```

The tick behaved exactly as specified — the assertion was reading the host. It is
the same shape as the DISPLAY finding above (a case whose state is the
operator's session rather than the case's fixture), so it gets the same remedy:
the test pins BOTH sandbox-shaped arms at empty temp roots
(`GOPI_SANDBOX_DIR` and `GOPI_SCRATCH_BASE`), which is the "this arm finds
nothing" state its other fixtures already rely on. Red-then-green, same command,
same fixture: `/home/user/.gopi/sessions/01a0f4b1-…/sandbox-arm-regression.txt`.

## Verification

`## A second defect the scoped run found: a host-dependent tick assertion

`TestScratchGCTick_ThrottleAndFailOpen` asserts the FIRST tick's return value is
exactly 1, and `scratchGCTick` returns the SUM of its arms (home + base + capture
units + sandbox). Its fixtures pin the home and base arms; the SANDBOX and
CAPTURE-UNIT arms read host configuration, so an abandoned `sandbox-*` the
operator's own root happens to be holding past its 72h age is counted into the
assertion. Measured 2026-10-01 with the real root in force:

```
--- FAIL: TestScratchGCTick_ThrottleAndFailOpen (1.61s)
    scratch_gc_tick_test.go:104: first tick removed 2, want 1
    [worker] [scratch-gc] removed 1 abandoned sandbox(es) older than 72h0m0s from /home/user/workspace/gopi-sandbox
```

The tick behaved exactly as specified — the assertion was reading the host. It is
the same shape as the DISPLAY finding above (a case whose state is the
operator's session rather than the case's fixture), so it gets the same remedy:
the test pins BOTH sandbox-shaped arms at empty temp roots
(`GOPI_SANDBOX_DIR` and `GOPI_SCRATCH_BASE`), which is the "this arm finds
nothing" state its other fixtures already rely on. Red-then-green, same command,
same fixture: `/home/user/.gopi/sessions/01a0f4b1-…/sandbox-arm-regression.txt`.

## Verification` of the task report carries the commands and their output. The
short form: `TestToolsMaintenanceHelpSurface*` all PASS, `go build ./...` OK,
`gopi doccheck` consistent.
