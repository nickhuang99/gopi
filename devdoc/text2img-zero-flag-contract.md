<!-- doc-class: historical -->
# The text2img numeric-flag contract: "positive integer" was a message, not a predicate

Task `01a0dde8-8511-7a0b-9b71-2508100b5a1b`. Fix + regression guard in
`cmd/agent/cli_text2img.go`; the shared floor/sentinel in
`internal/text2img/render.go`.

## The defect (measured, pre-fix binary)

`gopi text2img in.txt out.png --size 0` exited **0** and rendered at **字号 20px**
— the default the user never asked for — while `--size -5` and `--size abc` were
refused by the same walk with the message `--size requires a positive integer`.
The message stated a rule the predicate did not enforce:

```go
// pre-fix: flagInt returned the parsed 0 and forwarded it as-is
if n, err := strconv.Atoi(v); err == nil { return n }
return -1
...
if f.v < 0 { /* refuse */ }   // 0 passes; internal/text2img norm() then reroutes 0 -> Default*
```

So "the user spelled 0" and "the user spelled nothing" had **the same meaning**
all the way down to the renderer, which is the silent-failure shape Rule 15
names: a request that cannot be honoured handled by substitution rather than by
refusal. Measured for all five flags: `--size/--cols/--lh/--lines/--margin 0`
each exited 0 with an empty stderr and a full render summary.

## The contract now (one rule, two faces)

* **ABSENT** (the `--name` token never appeared) -> the documented default. The
  help page's "Default 20 / 50 / 32 / 90 / 12" is still literally true.
* **PRESENT** (the token appeared, whatever the spelling) -> must be a positive
  integer. `0`, `-5`, `abc`, `--size=` and a trailing `--size` are all refused
  with exit 1, the flag named, and the usage page attached. Nothing is rerouted.

The predicate is `n >= text2img.MinPositiveInt`, and the invalid path returns
`text2img.InvalidFlagInt` — the ONE sentinel the dispatch walk compares against,
so "what the message says" and "what the code does" cannot drift apart again.
Both constants live in **`internal/text2img`**, next to the `Default*` they
interact with: that package's `norm()` is what turns a zero into a default, so it
owns the definition of where "given" stops being legal (Rule 14). The CLI reads
them; it does not restate them.

## Why `fs.has(name)` is load-bearing (a second, quieter bug)

`memoryFlagSet` — the hand-rolled parser every CLI verb shares — reports the
**same empty string** for an omitted operand (`--size`) and for `--size=`. A
parser that reads only `fs.get(name)` therefore cannot tell "absent" from
"present and empty", and the pre-fix code silently resolved the second to the
default: measured on the pre-fix binary, `gopi text2img in.txt out.png --size`
exited 0 and rendered at 字号 20px. `text2imgIntFlag` reads `fs.has(name)` first
and falls back to the flag's own default only when the token never appeared.
This arm is invisible to a unit test that calls `parseText2imgArgs` — it is a
property of the shared parser — which is why the regression guard drives the
real binary (Rule 31).

## The guard

`cmd/agent/cli_text2img_zeroflag_e2e_test.go` (real binary, isolated `GOPI_HOME`,
stdout and stderr captured separately, a timeout is a failure — a probe that
hangs has fallen through to the agent loop):

| test | asserts |
|---|---|
| `TestText2imgZeroFlagIsRefusedE2E` | all five flags x `0` -> rc=1, `--<flag> requires a positive integer` on stderr, usage attached, **no** `✅` on stdout |
| `TestText2imgNonIntegerAndNegativeRefusedE2E` | all five flags x `abc`/`-5` stay refused |
| `TestText2imgHelpContractHoldsForSpelledFormsE2E` | trailing `--size`, `--size=`, `--cols=` -> rc=1 |
| `TestText2imgAbsentFlagStillRendersE2E` | absent -> rc=0 at 字号 20px/折行 50; `--size 28` -> rc=0 at 字号 **28px** (so "refuse everything" cannot pass) |
| `TestText2imgUsageStatesThePositiveContract` | the constant states the rule AND the binary's own `-h` prints it |

`cmd/agent/cli_text2img_test.go` gained the parse-layer half (sentinel for
`abc`/`-5`/`0`/empty spellings, the default for absent).

## Red -> green

* **Red**: the pre-fix binary built from `git show HEAD:cmd/agent/cli_text2img.go`
  as `GOPI_TEST_BIN`, with `go test ./cmd/agent/ -run TestText2img` → 4 of the new
  tests FAIL, 13 occurrences of `exit = 0, want 1`, and the captured stdout of the
  zero runs carries `字号 20px` — the silent default render, in the test's own
  output. `-h` did not print the contract note.
* **Green**: same command with the freshly built binary → `ok
  github.com/nick/gopi/cmd/agent`.

## Scope note

Only the five NUMERIC flags go through `text2imgIntFlag`; `--font` keeps reading
`fs.get` (an empty font path means autodetect, which is its documented
behaviour). A value that is a legal positive integer passes through untouched
(`--size=28` → 字号 28px, measured).
