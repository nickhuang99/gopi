<!-- doc-class: normative -->
# devdoc: the `gopi shot` index row keeps its reachable surface

## Symptom

`go test -count=1 -run TestUsageAdvertisesShot ./cmd/agent/` was RED on master:

```
cli_shot_test.go:21: top-level usage is missing "--wait-selector"
cli_shot_test.go:21: top-level usage is missing "gopi vision_analyze <out.png>"
```

## Which side was wrong: THE USAGE TEXT (one line)

The **usage text** regressed and the **test was correct** — the asserted features
still exist, and commit `bd0a0a06` ("fix(cmd/agent): register memory backup once,
and keep gopi -h inside its budget", 2026-09-19) deleted both tokens while
compressing the top-level help.

Attribution — measured, not assumed:

```
git show bd0a0a06 -- internal/config/help.go
- {"gopi shot --url URL | --self", "…(headless Chromium); feeds `gopi vision_analyze <out.png>`.
                                      Flags: --out, --viewport, --wait, --wait-selector."},
+ {"gopi shot --url URL | --self", "…(headless Chromium); flags in `gopi shot -h`."},
```

Both halves are still real:

- `cmd/agent/cli_shot.go:50` — `--wait-selector CSS` is a live flag (parsed at
  `cli_shot.go:77/110`, example at `:70`).
- `cmd/agent/cli_shot.go:31` — the verb's own `-h` still prints the
  `gopi shot --self && gopi vision_analyze <out.png>` loop example.

So the *features* never went away; only their discoverability from the front
door did. The test is the anti-regression guard for exactly that property, and
it fired — the correct response was to restore the row, not to weaken the test.

## Fix (within the budgets, nothing else touched)

One row of `internal/config/help.go`'s `commandIndex`, restored in compressed
form:

```go
{"gopi shot --url URL | --self", "Screenshot a URL or the local chat UI (headless Chromium); --wait-selector waits for an SPA element, then feeds `gopi vision_analyze <out.png>`; flags in `gopi shot -h`."},
```

Both asserted tokens are carried; the long flag list still lives in the verb's
own `-h`, which is the row's stated policy. Budgets hold:

- wrap: every rendered line ≤ `internal/config.HelpWrapCol` = 88 display cols
  (`TestTopLevelUsageIsAlignedAndTerse` PASS — the row wraps to two lines).
- length: `./bin/gopi -h | wc -l` = **148** lines, cap
  `topLevelUsageMaxLines` = 160 (12 lines of headroom).

## Not Done (honest)

- No new test was added: `TestUsageAdvertisesShot` **is** the regression guard,
  and it was green against the restored row. Adding a second assertion over the
  same two tokens would be a duplicate, not a guard.
- No audit of other index rows for the same "trimmed one phrase too far"
  pattern. `bd0a0a06`'s sibling `06e4aeed` already repaired the `voice_input`
  row that same day; whether a third row was trimmed is not established here.

## Verification

```
$ go test -count=1 -run 'TestUsageAdvertisesShot|TestShotUsageShowsDefaults|TestTopLevelUsage|TestFormatUsage' ./cmd/agent/ ./internal/config/
ok  github.com/the operator/gopi/cmd/agent     0.522s
ok  github.com/the operator/gopi/internal/config 0.720s

$ go build -o bin/gopi ./cmd/agent/ && ./bin/gopi -h | wc -l
148
$ ./bin/gopi -h | grep -c -- "--wait-selector"
1
$ ./bin/gopi -h | grep -c "gopi vision_analyze <out.png>"
1
$ ./bin/gopi shot -h | grep -c -- "--wait-selector"
2

$ go test -count=1 -run TestTopLevelUsageIsAlignedAndTerse -v ./cmd/agent/
--- PASS: TestTopLevelUsageIsAlignedAndTerse (0.00s)
PASS
```
