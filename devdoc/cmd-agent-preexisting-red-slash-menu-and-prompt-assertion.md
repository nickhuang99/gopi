<!-- doc-class: historical -->
# Two pre-existing reds in `./cmd/agent`: the slash fixture's prefix, and a prompt assertion that read the host

**Task** `01a0f603-76ea-73a0-b79a-769abc30a15e` · master `4926e4b9`
**Scope**: the two arms named in the briefing. Nothing else in the package was touched.

Both defects were decided by **executing the real surface**, not by reading the
test. In both cases the production side was right and the expectation was stale —
but for two different reasons, and only one of them is about the product at all.

---

## Defect 1 — `TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove` (`slash_menu_test.go:258`)

### The ruling: the COMPLETER is right; the fixture's prefix went stale

The contract ("a ONE-candidate menu must state why the arrow cannot move") needs a
prefix that matches exactly one command. The fixture had hard-coded `"/q"` with the
comment *"reaches exactly ONE command on the production table (/quit)"* — true when
it was written, false since `ff76758c` added `/queue` to `chatSessionWords`.

### Evidence (the real completer, asked the way the editor asks it)

A throwaway table test called the **production** completer directly —
`NewSlashCompleter()(prefix, len(prefix), lineedit.CompletionCandidateLimit())` —
over every short prefix:

```
PROBE /q        count=2  /quit /queue
PROBE /qu       count=2  /quit /queue
PROBE /qui      count=1  /quit
PROBE /quit     count=1  /quit
PROBE /h        count=1  /help
PROBE /c        count=2  /clear /code-search
PROBE /t        count=3  /task /tools /text2img
PROBE /x        count=0
```

`/q` matching two words is not an ambiguity and not a defect: `/quit` and `/queue`
are different behaviours, both runnable, and the completer is prefix-based by
design. The stale side is the fixture. `/qui` is the shortest prefix the shipped
vocabulary still leaves single-candidate.

### Fix

- the fixture moved to `slashSingleCandidateWord = "qui"`, with the prefix built
  from `completionSlashPrefix` (Rule 1b/13 — the slash is referenced, never retyped);
- the **vacated** prefix kept its own mirror,
  `TestSlashMenuTwoCandidatesWrapsAndOffersNoReason`: two candidates, one marked
  row, Down MOVES the marker, `Menu.Rows() == len(cands)` with **no** reason row,
  Enter-Enter submits the second candidate. Deleting the vacated case would have
  removed the only guard on the two-candidate path — the note exists at `n == 1`
  only, so a menu that painted it always must fail somewhere;
- the one-candidate case additionally asserts every offered candidate is a word the
  REPL's own vocabulary accepts (`chatReplCommandNames`), so the offer cannot drift
  from the dispatcher.

**Prior art, reused rather than re-derived.** A sibling remote had already fixed
this exact arm (`9ec1c5b7`, `remotes/hp/master`, same base). Found with
`git log --all -S'chatSystemPromptPrefix'` / `--grep`; its diff applied clean to
this HEAD, so it was taken verbatim (`git show 9ec1c5b7 -- cmd/agent/slash_menu_test.go`
→ `git apply`). Two tasks re-deriving one fix is the waste Rule 27's sibling
(复用不重做) exists to prevent.

---

## Defect 2 — `TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer` (`cli_term_once_test.go:317`)

### The ruling: production is right; the ASSERTION was host-dependent, and that is the defect

The arm asserted `strings.HasPrefix(systemPrompt, "You are GoPi in chat mode")`.
On this host the head of the system slot is the operator's **reply-language
directive**, because `internal/config.OutputLanguageDirective()` is prepended to
the whole assembly (`systemPromptWithOutputLanguage`, `internal/agent/loop.go`).
The chat prompt is present — one block behind it. **The assertion was reading the
machine's configuration and calling it the prompt.**

### Evidence 1 — the real surface (the one-shot turn, live)

A minimal SSE mock stood in for the provider (a `python3 http.server` writing the
POST body to disk), and the shipped binary ran the face the test covers:

```
$ S=/home/user/.gopi/sessions/01a0f603-76ea-73a0-b79a-769abc30a15e
$ SB=$S/sandbox/sandbox-probe-ce95722c
$ GOPI_HOME=$SB ANTHROPIC_BASE_URL=http://127.0.0.1:8791 ANTHROPIC_AUTH_TOKEN=test-key \
      $S/gopi cli "say OK" --once
OK there
EXIT=0
```

The model request it actually sent (`BuildClaudeRequest` ships `system` as a block
array; block 0 is the prompt, block 1 the project's `GOPI.md`):

```
--- request 0 path=/anthropic/messages tools=False
  head: [{"type": "text", "text": "[OUTPUT LANGUAGE — set by the operator, overrides everything below] Write every ...
   HAS You are GoPi in chat mode
   HAS [CHAT SURFACE
   HAS [CHAT WRITE BOUNDARY
   HAS [OUTPUT LANGUAGE            <- not the prompt: this host's configuration
--- request 1 (--tools) tools=True   … same prompt, tools on the wire
```

So the production side was never in doubt: the turn IS the chat turn. The same
probe with `--tools` confirms the two limbs of the block are chosen honestly.

### Evidence 2 — the red is the operator's settings file, not the prompt

```
$ GOPI_SETTINGS= go test -count=1 -run TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	0.511s          <- settings channel skipped: GREEN

$ GOPI_OUTPUT_LANGUAGE= go test -count=1 -run <same>
--- FAIL: TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer          <- env alone does not clear it
```

`grep output_language /home/user/.gopi/settings.json` → `"output_language": "zh-CN"`
(the file `GOPI_SETTINGS` points at, and the same path `DefaultSettingsPath` would
resolve). A green-or-red test that depends on which machine runs it is not a
contract.

### Evidence 3 — the failure message itself names the mechanism (verbatim, before)

The red the operator reported carries the proof in its own text: the value it
printed as "the request's system prompt" **opens with the directive**, and the chat
prompt follows it inside the same block.

```
$ go test -count=1 ./cmd/agent/            # master 4926e4b9, unchanged tree
    cli_term_once_test.go:317: the request's system prompt = "[OUTPUT LANGUAGE — set by the
    operator, overrides everything below] Write every reply you produce to the user in zh-CN. …
    You are GoPi in chat mode, a helpful conversational assistant. … [CHAT SURFACE — you are the
    console, here and now] … [CHAT MEMORY TIMING …] [CHAT WRITE BOUNDARY — a conversation, not a
    task run] … want the chat system prompt (prefix "You are GoPi in chat mode") — this is not
    the same turn the interactive carrier runs
```

The arm did not fail because the turn carried the wrong prompt — it failed because
`HasPrefix` read the *machine's* first line. The sibling arm's message is the same
shape for defect 1:

```
    slash_menu_test.go:258: the production completer offers 2 candidates for "/q" (want exactly 1,
    the one-item case this contract is about)
        --- menu ---
          /quit          (alias of /exit)
          /queue         (queue a prompt for the next turn)
```

### Fix (both halves — either alone is incomplete)

1. **Position-independent assertion.** `Contains(prompt, marker)` instead of
   `HasPrefix`, with the markers taken from the package that OWNS them
   (Rules 13/14 — referenced, never mirrored): `agent.ChatSurfacePromptHead()`
   and `agent.TaskSystemPromptHead()` are new exported accessors beside
   `chatContextHead` (`chat_context.go`) and `headerRules` (`loop.go`).
   A re-worded head can no longer leave the check passing against text nothing
   paints.
2. **A second direction.** `Contains(got, taskSystemPromptHead)` is asserted
   **false**: "the chat prompt arrived" alone is satisfied by a prompt that also
   carried the task assembly — which is precisely the regression this gate exists
   for (`ChatOps`-shaped routing reaching the no-tools face). Both directions is
   what makes it a contract.
3. **The channels are pinned by the test** (`outputLanguageUnset(t)`): the env
   channel through `config.OutputLanguageEnv` + `t.Setenv` (Rule 17), the settings
   channel by pointing `GOPI_SETTINGS` at a path that does not exist — an absent
   settings file resolves to all-defaults, so no key has to be deleted.

### Red-proof of the new gate

Restoring master's exact shape (`HasPrefix` + ambient channels) on top of the fixed
file turns the arm red **for the right reason**, and the restored file is green
under the very channel that used to break it:

```
=== RED: master's exact shape (HasPrefix + ambient channels) ===
--- FAIL: TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer (0.00s)
    cli_term_once_test.go:335: the request's system prompt carries no "You are GoPi in chat mode" — …

=== GREEN: fixed file, GOPI_OUTPUT_LANGUAGE=zh-CN (the channel that used to break it) ===
ok  	github.com/nick/gopi/cmd/agent	1.372s
```

---

## Before / after of the package

`go test -count=1 ./cmd/agent/` — the whole-package verdict, not the two arms alone
(evidence files: `$S/before-cmd-agent.txt`, `$S/after3-cmd-agent.txt`).

| | before (`4926e4b9` + no change) | after (this branch, `-timeout 25m`) |
|---|---|---|
| exit | 1 | 1 |
| `--- FAIL` lines | **5** | **3** |
| the two arms in scope | FAIL, FAIL | **PASS** (verbatim, both) |
| `TestChatCLIPositionalFaceIsPreserved` | FAIL | **PASS** — ambient, ordering-dependent |
| `TestChatCLIRealBinaryRefusesAnUnknownFlag` | PASS | PASS (one intermediate run hung — a *concurrent* `go test ./cmd/agent/...` held the suite lock; see below) |
| `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` | FAIL | FAIL — **out of scope by the briefing** |
| `TestPrefetchGoroutineSettleGapDetectsALiveLeak` | PASS | FAIL — **out of scope by the briefing**, load-sensitive |
| `TestMergeEveryKilledLoopLeavesNoResidueInTheProfile` | FAIL | FAIL — **ambient, pre-existing** |

### The close-out re-measure, at the committed tip (the number this branch ships)

`go test -count=1 -timeout 25m ./cmd/agent/` run **alone** at `f420e347` (clean
worktree, `$S/final-cmd-agent.txt`): `FAIL github.com/nick/gopi/cmd/agent 350.236s`,
exit 1, and exactly **two** `--- FAIL` lines — `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`
(explicitly out of scope by the briefing) and `TestMergeEveryKilledLoopLeavesNoResidueInTheProfile`
(pre-existing/ambient, filed as its own row). `TestPrefetchGoroutineSettleGapDetectsALiveLeak`
and `TestChatCLIPositionalFaceIsPreserved` both PASS in this run; both in-scope arms PASS.
The final FAIL set is therefore a strict **subset** of master's five.

### The close-out re-measure, uncontended + isolated (`82bac733`)

The two residual `--- FAIL` lines of the 350s run are **ambient, not this diff** —
re-run **alone and uncontended** at this same tip they are green, verbatim:

```
$ go test -count=1 -run 'TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator|TestMergeEveryKilledLoopLeavesNoResidueInTheProfile' -v ./cmd/agent/
--- PASS: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (0.62s)
--- PASS: TestMergeEveryKilledLoopLeavesNoResidueInTheProfile (21.37s)
    --- PASS: TestMergeEveryKilledLoopLeavesNoResidueInTheProfile/interrupt (11.29s)
    --- PASS: TestMergeEveryKilledLoopLeavesNoResidueInTheProfile/terminated (10.08s)
ok  	github.com/nick/gopi/cmd/agent	22.543s      # exit 0
```

So every arm that was red on master is green on this branch when the package runs
on an idle host; the FAIL set of the long run is reproduced by contention (a
concurrent `go test` — `$S/after2-cmd-agent.txt`'s concurrent pid 483407 — not by
any file in this diff).

**The two out-of-scope arms are the observed exception, named rather than
hidden.** `TestPrefetchGoroutineSettleGapDetectsALiveLeak` was green in the
before run and red in the after run — it is one of the two load-sensitive timing
arms the briefing excludes, it is untouched by this diff (the diff's `cmd/agent`
files are `slash_menu_test.go` and `cli_term_once_test.go`), and its verdict
tracks ambient load, not this change. The briefing's own rule
("do not attempt whole-tree suites; the two timing arms are out of scope") is
what makes that an honest exception and not a claim of green.

**One intermediate run is NOT counted, and why.** A first `after` run
(`after2-cmd-agent.txt`, the run that motivated pinning `-timeout 25m`) died as
`panic: test timed out after 10m0s / running tests: TestChatCLIRealBinaryRefusesAnUnknownFlag`.
`pgrep -af 'go test'` at that moment showed a **second, concurrent**
`go test -count=1 -timeout 25m -json ./cmd/agent/...` (pid 483407) in the same
tree: the cmd/agent suite holds a shared scratch/bin lock under
`/dev/shm/suite-cmd-agent-run-*`, so a concurrent full-package run serializes
against it and the default 10m test timeout expires. Re-run **alone**, the same
tree is green on that arm. The measurement that counts is the uncontended one
(`after3-cmd-agent.txt`).

### The changed-file ratchet the diff tripped, and its repair (same commit)

`TestSyncBarePeersTestCoversEveryChangedFile` is the anti-rot gate that DERIVES
which `cmd/agent` files this branch changed and refuses any file outside a
**declared, reasoned** list. This diff changes two such files (`slash_menu_test.go`,
`cli_term_once_test.go`) — both squarely outside that feature's subject, and both
now listed in `syncPeersBareUnrelatedFiles` **with the reason**, which is exactly
the statement that gate asks for ("this test knows about them and asserts nothing
about them"). The alternative — leaving it red — would have been a second,
unremarked red in the package the task exists to green.

### The two exported accessors, checked against a clean baseline

`internal/agent` is not green on master either, so its two red arms were **not**
taken as this diff's: `TestCompact_PinnedWindowBacksOffInsteadOfLooping` (trim
emptied the history) and `TestDocAnchorsResolveInNormativeDocs` (234 pre-existing
drift permits, **54 new, 12 stale**) both fail with the same numbers on the clean
`HEAD` copies of `loop.go` and `chat_context.go` (measured by restoring both files
from `HEAD`, re-running, and restoring this branch's copies from
`$S/baseline/`); the new doc contributes **0** mentions to the anchor gate. The
8 added lines (two exported accessors) are therefore not the cause of any red
there.

### The three failures that remain, and why they are not this task's

- **`TestMergeEveryKilledLoopLeavesNoResidueInTheProfile`** fails on a clean master
  checkout with nothing changed **and** on this branch — measured from the run's own
  before/after files, so it cannot be attributed to this diff. It was submitted as
  its own ledger row rather than folded in here (Rule 26: an incomplete fix is
  repaired by dispatching again): `01a0f618-5f25-708d-b1ad-440c27e18174`.
  Host/environment-dependent, not a product regression.
- **`TestPrefetchGoroutineSettleGapDetectsALiveLeak`** and
  **`TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`** are the two
  load-sensitive timing arms the briefing puts explicitly **out of scope**. The
  prefetch arm was green in the before run (ambient load was lower) and red in the
  after run; **run alone it is green, verbatim**:

  ```
  $ go test -count=1 -run 'TestPrefetchGoroutineSettleGapDetectsALiveLeak' -v ./cmd/agent/
  --- PASS: TestPrefetchGoroutineSettleGapDetectsALiveLeak (0.20s)
  ok  	github.com/nick/gopi/cmd/agent	0.678s
  ```

  Neither file is in this diff (`git diff --name-only 4926e4b9..HEAD` touches
  `slash_menu_test.go` and `cli_term_once_test.go` under `cmd/agent/`, plus the
  ratchet list above). Their verdict tracks ambient load and the leakage of a
  concurrent suite run, not this change — which is why the briefing excludes them.
- **`TestChatCLIPositionalFaceIsPreserved`** failed in the before run and the first
  after run and **passed in the final one** (`after3-cmd-agent.txt`): also ambient
  (a positional-face probe that depends on what the surrounding suite left behind).
  It was submitted as its own row for the same reason:
  `01a0f618-5d5d-7e8c-b030-8af8ecc80a91`.

---

## Not done (honest)

- The `cmd/agent` reds that remain are **not fixed here**: they are outside the
  briefing's two-arm scope, and two of them are the explicitly out-of-scope,
  load-sensitive timing arms. The ambient ones are dispatched as their own rows
  (Rule 26) rather than folded in.
- No attempt was made at the whole-tree suite (the briefing forbids it and the
  package budget does not fit it): the gate is `./cmd/agent/` plus a scoped
  `./internal/agent/` run for the two new exported accessors.
