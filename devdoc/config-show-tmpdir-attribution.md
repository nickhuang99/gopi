<!-- doc-class: historical -->
# `gopi config show` named the wrong channel for the temp workspace

**Status:** fixed (branch `task/01a0de69ee2f75ffb1b9070bc7387d08`)
**Defect class:** caller-side provenance — a value that moved correctly reported a
channel that never decided it.

## The symptom

Every invocation of `gopi config show` printed the temp workspace with the SAME
parenthetical, whatever channel had actually decided it:

```
--tmpdir X   →  temp workspace  X                       (from the caller's TMPDIR)
GOPI_TMPDIR=X→  temp workspace  X                       (from the caller's TMPDIR)
settings key →  temp workspace  X                       (from the caller's TMPDIR)
--tmpdir X   →  temp workspace  X                       (from the caller's TMPDIR)   [all four channels at once]
```

The PATH half was always right. Only the attribution lied — which is exactly the
half the verb exists for ("what IS in force, and where did each value come
from").

## Root cause (two mechanisms, one report)

1. `cmd/agent/cli_config.go:configTmpDirReport()` called
   `config.ResolveTmpDirConfigured(settingsCfg, nil)` — a NIL **flag** Config, so
   the resolver's highest-priority arm (`flagCfg.TmpDir`) could never fire.
2. `cmd/agent/main.go` runs `tmpDirBootstrapFromArgs(args)` BEFORE the
   subcommand chain (`config show` returns from `main()` at that chain), and
   `publishTmpDir` writes the RESOLVED directory into gopi's OWN `TMPDIR` so
   every child inherits one temp workspace. The resolver tests its ambient arm
   (`TmpDirFromCallerEnv` → `TMPDIR`) BEFORE its `GOPI_TMPDIR` arm — so the
   report's second read of the channels matched **gopi's own publication** and
   answered `TmpDirSourceAmbient`.

A third fact that shaped the fix: `config.BootstrapEnvFromArgs` (which runs
before both) HOISTS `--tmpdir` into `GOPI_TMPDIR`, so by report time the flag
channel also lives in the environment — there is no CLI-only reading left to
make at that point.

So the report was re-deriving an answer the process had already produced, using
the very variable the process had overwritten in the meantime. No resolver unit
test can see this: the resolver is correct; the *caller* asked the wrong
question.

## The fix

The report is now a READER of the publisher's outcome, not a second opinion
(Rule 2: `ResolveTmpDirConfigured` has exactly ONE caller — the publisher).

* `internal/scratch`: new `TmpDirSourceEnvName = "GOPI_TMPDIR_SOURCE"` — the
  LABEL travels in the environment beside the VALUE, through the same `os.Setenv`
  the children inherit.
* `cmd/agent/tmpdir_inject.go`: `publishTmpDir` publishes the label whenever it
  differs from what is in place — including the pass whose value was already
  published (which is what covers the ambient and settings channels, where
  `TMPDIR` already holds the caller's own value and no value write happens). A
  failed `os.Setenv` is the same loud `failTmpDir` refusal as the value's own
  (Rule 15b) — a value with no label would report a provenance nobody measured.
  The `true`/`false` return still means exactly "this call installed the VALUE".
* `cmd/agent/cli_config.go`: `configTmpDirReport()` reads back
  `(scratch.TmpDirEnvName, scratch.TmpDirSourceEnvName)` and hands the pair to
  `scratch.ResolvePublishedTmpDir` — the SAME function that produced the
  published value, so the reader cannot disagree with the publisher. With
  nothing published it falls back to the built-in default
  (`TmpDirSourceBuiltInDefault`) behind the same usability probe as before.
  The now-unused `configSettingsForReport` helper was deleted with it.
* Labels stay single-sourced: `config show` prints what
  `internal/config.TmpDirSource*` / `internal/scratch.TmpDirSourceBuiltInDefault`
  define, never a literal of its own (Rules 1b/13/14).

## Verification (red → green)

Five real invocations, captured verbatim into
`~/.gopi/sessions/01a0de69-ee2f-75ff-b1b9-070bc7387d08/evidence/`:
`01-RED-prefix-binary.txt`, `02-GREEN-fixed-binary.txt` (plus `-lines-only`
extracts and `00-binaries.txt` with both binaries' sha256).

Records of the same five invocations replayed against a pre-fix binary
(`gopi-prefix`, built from the pre-change tree) and the fixed one produce
identical PATHs and four distinct correct labels.

The regression test is `cmd/agent/cli_config_show_tmpdir_test.go`:
`TestConfigShowTmpDirAttribution` executes the built binary as a subprocess
across the four single-channel cases, the all-at-once case and the default,
asserting the printed PATH **and** parenthetical, plus that the single-channel
labels are DISTINCT (the defect's signature is four cases collapsing onto one
label). It runs the suite's binary (`gopibin_test.go`'s cache, overridable with
`GOPI_TEST_BIN`), which is what makes the red half reproducible:

```
GOPI_TEST_BIN=<pre-fix binary> CGO_ENABLED=0 go test ./cmd/agent/ -run TestConfigShowTmpDirAttribution -count=1   # FAILS (4 cases)
GOPI_TEST_BIN=<fixed binary>  CGO_ENABLED=0 go test ./cmd/agent/ -run TestConfigShowTmpDirAttribution -count=1   # PASSES
```

The first draft called `testutil.RunGopi`, which resolves `$GOPI_BIN` /
`<repo>/bin/gopi` and **skips** when that is absent — so the "RED proof" silently
skipped all six subtests. A red proof that skips is not a red proof: the helper
was replaced with `runGopiIn` (the suite's own subprocess runner) and the RED run
above is the one that reports four failures.

## What was NOT changed, and why

* **`CONFIG.md`** — the knob's precedence, default, channels and the
  `internal/config/tmpdir.go` source line are all unchanged, and
  `GOPI_TMPDIR_SOURCE` is deliberately NOT a knobs-table row: it is an internal
  value→label handoff between the publisher and the report, not something an
  operator sets (documenting it as a knob is what
  `internal/standards/config_doc_gate_test.go` calls inventing one).
* **The resolver's precedence** — `--tmpdir` > a caller-exported `TMPDIR` >
  `GOPI_TMPDIR` > settings `tmpdir` > default is untouched, and the operator
  decision behind it (the ambient variable a child actually reads outranks the
  declarative file) still holds.
