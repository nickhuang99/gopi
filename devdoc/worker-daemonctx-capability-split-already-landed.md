<!-- doc-class: historical -->
# The workerDaemonCtx capability split was already on master — the rework's premise, measured

**Class**: historical (a past run's record)
**Task**: `01a132b2-60ef-702b-9937-f34ec671ad78` — rework of `task/01a132799d6c776c8fc14a479d264871`
**Branch reworked**: tip `29b21efe4` (base `8356a4925`); the branch's ref was already gone
from the main repo — its two commits were read from the **object store**, and neither the
ref nor the objects were touched.

## Why this doc exists

The rework row was filed because the branch's merge into master was refused **as
over-ceiling** — a size verdict about the merge machinery, not a statement about the
branch's content. The briefing for this run already carried a first measurement
suggesting the intent had landed independently. This run's job was to decide between two
very different tasks:

- **re-apply a 169k-token diff** (if master lacks the capability split), or
- **verify an already-landed claim and leave a record** (if master carries it).

The measurement says the second. Recording it matters because the same shape recurs:
a merge refused for size generates a rework row whose executor cannot tell, from the
refusal alone, whether the work is missing or merely duplicated.

## The measurement (object store, master tip `f27f4dd62`)

**1. The branch's blobs are byte-identical to master's.**

```
$ git rev-parse 29b21efe4:cmd/agent/worker_foreground_file_stop_test.go
62108329dc6aeb589e79080257ab99f3568d7b85
$ git rev-parse HEAD:cmd/agent/worker_foreground_file_stop_test.go
62108329dc6aeb589e79080257ab99f3568d7b85      # identical
$ git rev-parse 29b21efe4:devdoc/worker-foreground-file-stop.md
3b5693224db48251ea1cbc2b8d43d746704217e2
$ git rev-parse HEAD:devdoc/worker-foreground-file-stop.md
3b5693224db48251ea1cbc2b8d43d746704217e2      # identical
```

**2. Every line the branch added to the two source files exists verbatim on master** —
compared line by line with `grep -qxF` (exact whole-line match, not substring), over
`git diff 8356a4925 29b21efe4 -- <file>`, **unfiltered** (no minimum-length filter that
could hide a short line):

| file | added lines | missing from master's tip |
|---|---|---|
| `cmd/agent/cli_worker_control.go` | 89 | **0** |
| `cmd/agent/cli_worker_foreground.go` | 123 | **0** |

**3. The capability split itself is on master, with the branch's own shape.** The
predicates are methods **on the value receiver** (`func (c workerDaemonCtx)`), not a
pointer — the same form the branch wrote, i.e. master is not a mere re-implementation
that happens to answer the same question:

```
$ grep -n "publishes\|consumesRequests" cmd/agent/cli_worker_control.go
2111:// publishes reports whether this context writes the DAEMON STATE file. …
2114:func (c workerDaemonCtx) publishes() bool { return c.writer != nil }
2116:// consumesRequests reports whether this context reads the durable stop/force …
2125:func (c workerDaemonCtx) consumesRequests() bool { return c.dir != "" }
```

guarding **8** call sites (`consumeDrainRequests`, `installFileWatcher`,
`cleanupRequests`, `drainRequestPending` / `markDraining`, `setRoundEnd`, `setIdle`,
`markClaiming`), with `workerRequestCtxFor` at `:2390` building the foreground shape.

**4. Master's own commit introduced it — the branch was not merged, it is duplicated.**
`git merge-base --is-ancestor 29b21efe4 HEAD` → **NO**; `HEAD..29b21efe4` holds exactly
the branch's two commits. But `git log -S 'consumesRequests() bool' HEAD` names a single
commit, and it is master's own:

```
c0f29000f 2026-10-13 09:11:53 +0800 fix(worker): --stop/--restart/--status address ONE worker (--pid / --branch)
```

**09:11:53 is LATER than the branch tip's 08:12:36** — master re-derived the same fix
independently, an hour after the branch stopped writing. Nothing was carried over: the
branch's content is redundant, not merged, and the right resolution is to let the branch
go (Rule 9: 归档保留路径) rather than merge it again.

**5. The only genuinely differing path is generated data.** `git diff HEAD 29b21efe4`
reports `devdoc/DOC-INDEX.tsv` among a large drift set — but that is the whole-tree
divergence between an older tip and a newer master, not a branch's contribution:
`DOC-INDEX.tsv` is generated (Rule 36), never hand-edited, and re-rendered by
`gopi tool doccheck --fix`.

## Behaviour, on this worktree (master's code)

The branch's own acceptance tests were run **on master's code** — this is the part the
object store cannot answer, because identical text on two branches is not the same as
that text working in master's surrounding context:

```
$ go test ./cmd/agent/ -run 'TestWorkerStopForegroundDeliversByFileWhenTheBinaryDiffers|\
TestWorkerStopForegroundSignalsAProvenTarget|TestWorkerForegroundStopIsIdempotent' -v -count=1
--- PASS: TestWorkerStopForegroundDeliversByFileWhenTheBinaryDiffers (2.04s)
--- PASS: TestWorkerStopForegroundSignalsAProvenTarget (0.51s)
--- PASS: TestWorkerForegroundStopIsIdempotent (0.01s)
ok  	github.com/nick/gopi/cmd/agent	19.636s
```

and the request-channel consumers (the capability split's own guards):

```
$ go test ./cmd/agent/ -run 'TestFileWatcherIsResidentUntilStopped|\
TestInstallFileWatcherIsUninstallable|TestWorkerLoopLeavesNoWatcherPollBehind' -count=1
ok  	github.com/nick/gopi/cmd/agent	15.356s
```

`make build` (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`) exits **0**.

## What this is NOT

- **Not a re-application.** No source line was written on this branch: the measurement
  found nothing missing, so nothing needed landing. The deliverable is the record, and
  the branch that carries it carries no source change.
- **Not a merge of the reworked branch.** Merging a branch whose every added line already
  exists is how a rework row gets consumed without a content decision; the branch's
  history stays readable in the object store (Rule 23) and its resolution belongs in the
  archive step (Rule 9), with a reason.
- **Not a claim about the merge ceiling.** The refusal that opened this row was a size
  verdict; whether the ceiling itself is right is a separate question and is not answered
  here.

## Evidence

- `~/.gopi/sessions/01a132b2-60ef-702b-9937-f34ec671ad78/artifacts/object-store-measurement.txt`
  — the commands and their raw output (artifact, not committed; Rule 30).
- `cmd/agent/worker_foreground_file_stop_test.go` and `devdoc/worker-foreground-file-stop.md`
  on master — the branch's own test and design record, byte-identical.
