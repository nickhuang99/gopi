<!-- doc-class: historical -->
# Task Report: the task dispatcher's ssh-passthrough arm no longer hijacks a gopi command line

Task `01a0ece6-09dc-7219-afd3-22b8183586a1` · branch `task/01a0ece6-09dc-7219-afd3-22b8183586a1`

## The defect, measured

`gopi web search "task list"` printed the LEDGER. Not an error, not a usage
page: a successful ledger listing, exit 0, for a command the operator never
typed. Same for any argv whose first token is a gopi verb and whose remaining
text contains the word `task`:

```
$ ./bin/gopi web search "task list"      # PRE-FIX build (git archive HEAD → /…/gopi-prefix)
2916 task(s):
#01a0ecf5-…  [pending]  素材轮1/6: gopi task 17 子命令 …
```

**5-why.** The arm in `isTasksSubcommand` (`cmd/agent/cli_tasks.go`) is entered
whenever `args[0] != "task"`; it then (a) walked past flags, (b) ASSUMED the
first non-flag token was an ssh HOST, (c) word-split everything after it and
(d) looked for the literal `task` **anywhere** in the result. So
`[web, search, "task list"]` made `web` a host, and `task` — a word inside the
QUERY TEXT — a verb, dispatching `runTasksSubcommand(["list"])`. The arm was
written for one real shape (the option-carrying ssh argv `ssh -o BatchMode=yes
bnode "gopi task sync --serve-remote"`, M4.13/strace-proven: without it the argv
fell into the agent loop and burned an LLM session). Its precondition — "this
argv is a raw ssh argv" — was never checked, so *every* argv that was not
`task …` was candidate material for a host and a verb.

The hijacked run is the worst shape a routing bug takes: exit 0, no error, a
plausible-looking output. Only the CONTENT reveals that a different, and
ledger-touching, action ran.

## Completed

- **`cmd/agent/cli_tasks.go` — the gate.** The passthrough arm now begins with
  two refusals that make the word-scan unreachable for a gopi command line:
  - `argvAfterSSHPrefix(args)` — **one** walk (the router and the parser share
    it) that returns the remote command only when argv really carries an ssh
    TRANSPORT prefix (`-o KEY=VALUE` pairs, lone transport flags, then the host
    token) and returns `nil` for anything else, **including any argv whose
    first token is a gopi verb**. A verb word is never a host name, so the
    ambiguous case is refused instead of guessed.
  - the peer-side PROLOGUE (`argv[0] == "gopi"`, i.e. the peer receiving the
    remote command line) is recognized **by position**, and dispatches only
    when the next token really is the verb `task` — no search for the word.
- The word-scan itself is now quote-aware (`splitQuotedArgs`) so a remote
  command the shim left quoted in pieces (`"gopi task sync" --serve-remote`)
  still reads as one command line. Rule 1/13: the words it compares come from
  the `cliVerbs` constants + `taskverbs`; the ssh option flag is the named
  constant `sshOptionPairFlag`, the program word `gopiProgramWord`.
- **Regression tests — `cmd/agent/cli_tasks_ssh_passthrough_test.go`** (7 tests,
  30 subtests):
  - in-process, on the router: 10 argv shapes that must be **false**
    (`web search "task list"`, the same with flags, `web fetch <url with task/>`,
    `web_search "task list"`, `memory search "task list"`, `code-search`,
    `keyword_search`, a bare prompt, …);
  - in-process, on the shared walk: it consumes a transport prefix, and answers
    `nil` for a command line;
  - built-binary contract probes: the hijack repro reaches the web tool's JSON
    payload (and not the ledger header), the `web fetch` URL shape likewise, an
    unrelated query is unchanged, and six task-family shapes **still dispatch** —
    plain `task list`, the `gopi`-prologue, and three option-carrying ssh argvs.

## Not Done

- **Deliberate non-scope, recorded so nobody reads it as coverage.** Two other
  argv shapes containing the word `task` still reach the task family by an
  older lexical scan that this task did not touch: `gopi --timeout 3 task list`
  and the single login-shell-quoted element `gopi "gopi task sync"`. Both were
  ALREADY true before this fix (pre-fix measurement, below) and neither is a
  hijack of a gopi command line — the first IS a task invocation with the verb
  misplaced, the second is the shape the passthrough exists for. The briefing's
  own memory note (`6012`) records the same exemption for the first. Gating them
  needs a change to `main.go`'s flag-prefix handling / the one-positional shape,
  which is a different surface from the reported defect.
- **Inherited RED in the same package, NOT caused by this change**:
  `TestCompletionCandidateSetsCoverTopLevel` fails with
  `topLevelCommandNames contains "annotate" but no dispatcher handles it`.
  A/B measured: `git archive HEAD` → same failure (see ## Evidence). The test's
  `want` list is hand-maintained and lacks `annotate`; the fix belongs to
  whoever added `CliVerbAnnotate` to the completion set.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./cmd/agent/` → clean.
- Tests:
  - `go test ./cmd/agent/ -run 'TestIsTasksSubcommand|TestSSHPrefixWalk|TestWebSearchWithTaskQuery|TestWebFetchOfATaskURL|TestTaskFamilyStillDispatchesThroughTheSSHShapes|TestWebSearchOfAnUnrelatedQuery|TestWebSearchProbeHome' -count=1` → **ok, 7/7 PASS** (new tests).
  - `go test ./cmd/agent/ -run 'CLIContract|VerbGuard|Completion|Tasks|Undispatched|SSHPrefix|IsTasksSubcommand|WebSearch|WebFetch|PositionalVerb' -count=1` → 1 FAIL, and it is the inherited `TestCompletionCandidateSetsCoverTopLevel` above; every routing/contract test in the set passes.
  - `GOPI_TEST_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run TestVerbCensusMatchesTheDispatch -count=1` → ok (the verb census's runner-line projection is unaffected by the added walk).
- Ran (real built binary in this worktree, Rule 31):
  - `GOPI_HOME=<sandbox> ./bin/gopi web search "task list"` →
    `{"status": "ok", "method": "merged", "query": "task list", "elapsed_ms": 40, "count": 5, "inputs": [{"method": "bing", …}, {"method": "google", …}, {"method": "arxiv", …}], "results": [{"title": "Windows命令之tasklist命令-CSDN博客", …}]}` — **exit 0, five real search results**, no ledger text.
  - `GOPI_HOME=<sandbox> ./bin/gopi web fetch "http://127.0.0.1:1/task/12"` → the web_fetch JSON envelope (`{"status":"unavailable", …}`), i.e. the tool path, not the ledger.
  - `GOPI_HOME=<sandbox> ./bin/gopi -o BatchMode=yes bnode gopi task list` → the ledger render (not `unknown subcommand "BatchMode=yes"`).
  - `GOPI_HOME=<sandbox> ./bin/gopi task list` → the ledger render; `gopi gopi task list` → the same.
  - control: `GOPI_HOME=<sandbox> ./bin/gopi web search "postgres append-only ledger"` → `status ok, method merged, count 5`
    ("PostgreSQL: The world's most advanced open source database", …).
- Result: on the pre-fix binary (`git archive HEAD` → built `/…/gopi-prefix`) the
  same three probes give `No tasks found.` for the first two shapes and
  `gopi task list: unknown flag --method` for the flag-carrying variant — the
  ledger answering a web query, and the abandoned web search's own flag being
  parsed as a task flag. Post-fix: web JSON for the web shapes, ledger for the
  ledger shapes.

## Evidence (self-filed)

- Hijack, pre-fix binary: `GOPI_HOME=/…/h2 /…/gopi-prefix web search "task list" | head -3` → `No tasks found.`
  (the probe home is empty; the operator's own ledger made it `2916 task(s):`).
- Hijack, fixed binary: `GOPI_HOME=/…/h2 ./bin/gopi web search "task list"` →
  `{"status":"unavailable", …, "message":"websearch scripts dir not found: /…/h2/.gopi/plugins/websearch/scripts …"}`
  (isolated profile → no plugin tree ⇒ the tool's own payload, exit 3).
- RED→GREEN of the new suite, same source, only the binary differs:
  `GOPI_TEST_BIN=<pre-fix binary> go test ./cmd/agent/ -run 'TestWebSearchWithTaskQueryReachesTheWebTool|TestWebFetchOfATaskURLReachesTheWebTool|TestTaskFamilyStillDispatchesThroughTheSSHShapes'`
  → `--- FAIL: TestWebSearchWithTaskQueryReachesTheWebTool … the LEDGER answered instead of the web tool (the hijack) / --- output --- / No tasks found.`
  With `GOPI_TEST_BIN=$PWD/bin/gopi` (this worktree) the same three tests →
  `--- PASS` ×3.
- The in-process test's RED half is evidenced by the binary probe above rather
  than by running it against the old source: the test imports
  `argvAfterSSHPrefix`, which does not exist pre-fix, so the old tree cannot
  compile it. The binary probe is the same assertion on the same argv.
- The wiki control's emptiness is ENVIRONMENTAL, not a regression, A/B measured:
  `web search "postgres append-only ledger" --method wiki` → `{"status":"empty",
  "method":"wiki","count":0,"source":"remotebox"}` on the PRE-FIX binary and on
  this worktree's binary alike (the merged multi-method control returns 5
  results on both). The test's control probe therefore asserts the payload's
  SHAPE, never a result count.
- Inherited RED, A/B: `TestCompletionCandidateSetsCoverTopLevel` fails
  identically when run against `git archive HEAD` extracted to a scratch
  directory (`--- FAIL: cli_completion_test.go:47: topLevelCommandNames contains
  "annotate" but no dispatcher handles it`) and in this worktree.
- `git status --porcelain` at close: empty.

## Files

- `cmd/agent/cli_tasks.go` — the gate (`argvAfterSSHPrefix`, `isArgvVerb`,
  `gopiProgramWord`, `sshOptionPairFlag`) + the rewritten passthrough arm.
- `cmd/agent/cli_tasks_ssh_passthrough_test.go` — the 7 regression tests.
- `devdoc/task-ssh-passthrough-verb-gate.md` — this report.
- `devdoc/DOC-INDEX.tsv` — the classification row for the report (Rule 36).

## Commit

`973cf5bc` — `fix(cli): the ssh-passthrough arm may not read a gopi command line as an ssh argv`
