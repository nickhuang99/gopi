<!-- doc-class: historical -->
# Task Report: CJK candidate extraction now delegates to internal/cjkseg (task 01a115e6)

## Completed

- `internal/memoryprefetch/extract.go`: the CJK pass is now **delegation, not a
  sliding window**. Every maximal CJK run is handed to `cjkseg.Words` (gse over
  its embedded `zh_s` dictionary — the project's ONE segmenter, the same call the
  memory WRITER uses to fill the `jieba_token` column), and each returned word
  becomes a `KindCJKWord` candidate.
- The 2-rune sliding window survives as `fallbackCJKRuns`, reached **only** when
  the segmenter returns an error — and its use is recorded as
  `DegradationCJKTokenizerGap`, whose detail now names the segmenter and quotes
  its refusal. The stale premise ("no CJK segmenter in this project") is gone from
  the code, the doc comments and the constants.
- `DegradationCJKBelowFloor` (`cjk-below-retrieval-floor`) is the second, narrower
  named loss: material dropped for being too short (a run below the window, or a
  segmenter word below `minCJKCandidateRunes = 2`). The two are separate codes
  because "the segmenter could not answer" and "the segmenter answered, and this
  fragment is below the floor" are different facts (Rule 15).
- `standalone_test.go`: the package's independent constraint gate was **revised
  with its reason** rather than deleted. `TestPackageImportsStdlibOnly` still bans
  every third-party module (the dot-in-first-element rule is untouched — a `gse`
  import here is still red); one project package is admitted, and
  `TestProjectImportsAreTheSegmenterOnly` pins that it is exactly
  `internal/cjkseg`, so a second project import cannot ride in behind the
  exemption. `TestNoEnvironmentReadsInThePackage` is untouched (no env was added).
- Regression test `TestExtract_CJKSegmentedNotBigramFragments` pins the measured
  failure by its literals: `目前我们的 memory 预取实现有问题` must no longer yield
  `前我` / `们的` (nor `取实` / `现的`), and no fallback-kind candidate may exist
  while the segmenter answers.
- `CONFIG.md` §2c's "where each half lives" paragraph corrected in the same commit
  (it said *stdlib only*; the package now has one project import, and the paragraph
  states which and why).
- devdoc: this report + its `DOC-INDEX.tsv` row, same commit.

## Not Done

- **The fallback path is covered by a fixture, not by a failing dictionary.** The
  dictionary is embedded in the binary and `cjkseg.Words` can only fail if loading
  it fails, which nothing in-process can provoke; the tests therefore replace the
  package's `segmenterWords` value for the duration of one test. That is stated
  rather than dressed up: the CALLER's degraded behaviour is what is verified, the
  segmenter's own failure mode is not (it is `internal/cjkseg`'s subject).
- **English stemming/porter and the recall-ladder wiring were NOT touched.**
  `devdoc/memory-prefetch-recall-ladder.md` and memory #8349 record that
  `cmd/agent/chat_prefetch.go` still builds a pure-OR query
  (`ftsMatchQuery`) and never runs the Route 1.5 ladder. That is a separate
  defect on the query side; this change is the EXTRACTION side and leaves it
  untouched.
- **The real-chat run returns 0 hits**, before and after: the sandbox profile has
  an empty private ledger (`gopi sandbox` seeds nothing here), so the retrieval
  itself finds nothing. The comparison therefore carries the `query` field — which
  is exactly what this change alters — and not the hit count.
- A hit-rate measurement (does the new query actually retrieve more?) is **not**
  claimed: it would need a seeded, non-empty ledger, which is a different fixture
  from the one Rule 31's real-chat requirement exercises.
- **`./internal/standards` and `./cmd/agent` are RED on this branch's inherited
  base, for reasons this diff does not touch** — disclosed here rather than left
  to be discovered:
  - `TestStandardGates/TestGate2TestEnvRedProof` (Rule 17): two NEW unannotated
    violations at `cmd/agent/chat_session_budget_test.go:58` and `:87`
    (`os.Setenv`/`os.Unsetenv` in a test).
  - `TestCommandsDocGeneratedBlockMatchesSource` + `TestCommandsDocAnchorsResolve`
    (the `gopi scratch-gc` row's generated block has drifted from its generator).
  - `TestCLINamingExceptionsAreStillDispatchable` (`./cmd/agent`).

  `git diff master --stat -- cmd/agent/chat_session_budget_test.go` is **empty**,
  i.e. the red file is byte-identical to master, and this branch's 8 files are
  none of the three gates' inputs. Filed as **task 01a11600-baeb-7121-a470-48fee2236732**
  (`tags=[bug]`, with the three gates, the verbatim failure lines, and the three
  candidate remedies) rather than repaired inside this branch: the remedy is a
  decision about master's own surfaces, and mixing it into a memoryprefetch review
  would hide it.

## Verification

- Build: `go build ./...` → OK (no output).
- Tests: `go test ./internal/memoryprefetch/` → `ok github.com/nick/gopi/internal/memoryprefetch`
- Tests: `make test-budget PKGS='./internal/memoryprefetch ./internal/config'` →
  `packages: 2 finished, 0 with a failure` / `outcome=pass` / `exit=0` (38s wall),
  stream `run-1791369914-3526584.json`.
- Tests (FINAL re-run at close-out, same tree, after `go build ./...` exit 0 and
  `git status --porcelain` empty):
  `make test-budget PKGS='./internal/memoryprefetch ./internal/config'` →
  `packages: 2 finished, 0 with a failure`, `elapsed=9s`, `exit=0 outcome=pass`,
  stream `run-1791370588-3594294.json`; slowest test
  `1.28 memoryprefetch TestExtract_CJKSegmentedNotBigramFragments`.
- `gopi tool doccheck` → `the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row`.
- Ran (real surface, Rule 31 — the binary, on a pty, with the knob armed):

  ```
  # AFTER (this branch's build)
  { sleep 12; printf '目前我们的 memory 预取实现有问题需要仔细排查一下'; sleep 10; printf '\x03'; sleep 1; printf '/quit\r'; sleep 4; } \
    | GOPI_HOME=<sandbox-after> GOPI_MEMORY_PREFETCH=on LC_ALL=C.UTF-8 TERM=xterm-256color \
      timeout 120 script -q -c "<evidence>/gopi-after chat --tools --verbose" /dev/null
  ```

  ```
  # BEFORE (baseline build from the unmodified tree)
  ... same command line, binary = <evidence>/gopi-baseline, GOPI_HOME=<sandbox-before>
  ```
- Result — the prefetch record each run left in its own session directory
  (`memory-prefetch.jsonl`, `query` is the retrieval key this change produces):

  | | query |
  |---|---|
  | BEFORE | `memory 目前我们的 目前 前我 们的 预取 取实 实现` |
  | AFTER  | `memory 目前 预取 实现 问题 需要 仔细 排查` |

  The fragments `前我`, `们的`, `取实` are gone; in their place are the segmenter's
  own words (`需要` is a word the segmenter recovered with the HMM, not a window
  artefact). Both runs: `hits=0, outcome="empty"` (empty sandbox ledger).
  Verbatim record lines are in `## Evidence` below.
- Ran (red → green, the regression test):
  - RED: forcing the pre-change path
    (`dropped, segmentErr := 0, fmt.Errorf("RED-PROOF: …")`) made it fail with
    `term "前我" is a 2-rune sliding-window fragment, not a word … terms=[memory
    目前我们的 目前 前我 们的 预取实现有问题 预取 取实 实现 现有 有问 问题]`.
  - GREEN: with the change in place, `go test ./internal/memoryprefetch/ -run
    TestExtract_CJKSegmentedNotBigramFragments` → `ok`.
  - The repo was restored byte-for-byte from the pre-red copy before committing
    (`cmp` clean; `grep -rn RED-PROOF internal/memoryprefetch/` → no match).
- Commit: see `git log -1` on `task/01a115e6686279fe870326a775ed93ec`.

## Files

- `internal/memoryprefetch/extract.go` — segmenter pass, fallback pass, two named
  degradations, `segmenterWords` seam, `runCensus`.
- `internal/memoryprefetch/extract_test.go` — regression test + fallback-path
  fixtures (updated/rewritten).
- `internal/memoryprefetch/render_test.go` — the degradation the render fixture
  really produces.
- `internal/memoryprefetch/standalone_test.go` — the import gate, revised with its
  reason, plus the second half that pins the admitted project import.
- `internal/memoryprefetch/config.go` — the package header's `extract.go` line.
- `CONFIG.md` §2c — "where each half lives".
- `devdoc/memory-prefetch-cjk-segmenter.md` + its `DOC-INDEX.tsv` row.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `make test-budget PKGS='./internal/memoryprefetch ./internal/config'` →
  `packages: 2 finished, 0 with a failure`, `exit=0 outcome=pass`.
- BEFORE record line (verbatim):
  `{"query":"memory 目前我们的 目前 前我 们的 预取 取实 实现","hits":0,"ms":6,"outcome":"empty","adopted":"no","candidates":"or"}`
- AFTER record line (verbatim):
  `{"query":"memory 目前 预取 实现 问题 需要 仔细 排查","hits":0,"ms":6,"outcome":"empty","adopted":"no","candidates":"or"}`
- Both runs printed the knob's own report on stderr:
  `[gopi] memory prefetch: on (source: GOPI_MEMORY_PREFETCH) — record: <session dir>/memory-prefetch.jsonl`
  — i.e. the executor really was armed, so an empty record would have been a
  finding rather than a pass.
- The two binary builds: `<evidence>/gopi-baseline` (from the unmodified tree,
  built before any edit) and `<evidence>/gopi-after` (this branch).

## Landing record: re-dispatched onto branch `task/01a1160137767029b6685d8574bae072` (Rule 26)

`## Completed` above is the work of a dispatch on the peer host (peerbox), and an
earlier attempt to carry it here asserted that its close-out commit had not
reached master. That premise is **FALSE, and it is WITHDRAWN here**: the peer's
close-out commit is an ancestor of master on the host that matters — measured
below, from this tree's own git objects:

- `cfe4ec161` — that attempt's own close-out commit (`docs(memoryprefetch):
  record the close-out test-budget re-run`);
- `34f3aa315` — `Merge branch 'task/01a115e6686279fe870326a775ed93ec' into
  master`, the row that landed this work; its second parent is `1dbdee630` (the
  delivered branch's own close-out), which descends from `cfe4ec161`. Both
  `cfe4ec161` and `34f3aa315` are `git merge-base --is-ancestor … master` →
  **rc=0**;
- the one project import travelled with it: master's
  `internal/memoryprefetch/extract.go` calls `cjkseg` at 11 sites, and master's
  `CONFIG.md` §2c already carries `Its ONE project import is internal/cjkseg`
  (`grep -c 'stdlib only'` → `0`, the unrevised wording is gone).

What follows is therefore a **Rule 26 re-dispatch record of a DELIVERY, not of a
recovery**, and every item that attempt listed as "still to do on this host" was
already done by the merge — the five code/test files, the `CONFIG.md` §2c
paragraph, the `DOC-INDEX.tsv` row and the Rule 31 re-measurement. Nothing of the
implementation survived on that branch as unique content: measured against
current master, the residual is **zero for `CONFIG.md`, zero for
`internal/memoryprefetch/`** and **this appended record only** for this doc.

One real residue stays true and is kept here, because it is a reading of the code
rather than of a merge: `## Not Done` #1 (the fallback exercised by a fixture
rather than by a failing dictionary) and the recall-ladder note — the query path
(`cmd/agent/chat_prefetch.go`'s pure-OR `ftsMatchQuery`, the Route 1.5 ladder it
never runs) is untouched by the landing above. Historical record, class
`historical`: it states no standing claim about current code.
