<!-- doc-class: normative -->
# Task Report: the output-language knob (reply language, three channels, one resolution)

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Epic**: `01a0dde7-30e8-7650-ba32-b7f773165944` — operator-requested: he types
English and wants gopi to *answer* him in Chinese. Ruled shape: **framework
untouched**, one knob, reply language only, three channels.

## Completed

- **`internal/config/output_language.go`** — the knob's ONE home:
  `OutputLanguageSettingsKey` (`"output_language"`), `OutputLanguageEnv`
  (`GOPI_OUTPUT_LANGUAGE`), `OutputLanguageFlagName` (`--output-language`),
  the tag shape (`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`, a **shape** check and
  deliberately not a language whitelist), one resolution
  (`resolveOutputLanguage`, env > settings), and the directive text
  (`OutputLanguageDirective`, **empty string when unset**).
- **Rule 15 discipline, copied from `task_sync_switch.go`**: an env value that is
  not a language tag is reported on stderr (`config: GOPI_OUTPUT_LANGUAGE="…" is
  not a language tag … — ignored; the next channel decides`) and the **next
  channel decides**; an unusable *settings* value is likewise reported and the
  knob stays unset. Never silently read as "unset".
- **Three channels wired**: settings field `Config.OutputLanguage`
  (`json:"output_language,omitempty"`), `applyFlag` case, `ApplyEnv`
  publication (fill-only, so settings can never outrank an export), and
  `BootstrapEnvFromArgs` hoisting the **flag** into the env name — which is what
  makes the flag reach `gopi chat` and a task run, neither of which passes the
  agent-path config block.
- **Catalog row** in `internal/config/settings.go` `EnvVars` → `gopi --help-env`
  and `gopi -h` render it (observed below).
- **Directive emission at the head of BOTH prompt paths**
  (`internal/agent/loop.go`): one helper
  `systemPromptWithOutputLanguage(prompt)` returns the prompt **unchanged** when
  unset, else `directive + "\n\n" + prompt`. `assembleSystemPrompt()` (task path;
  also `gopi chat --tools`, which sends the same `a.SystemPrompt`) and the new
  `chatSystemPromptServed()` (plain `gopi chat`) both go through it — one
  function precisely so the two paths cannot disagree about the silent case.
- **Tests**: `internal/config/output_language_test.go` (14 cases: unset silence,
  empty-value silence, each channel alone, precedence env>settings and
  flag>env>settings through the real `BootstrapEnvFromArgs`, `--flag=value`,
  `FlagOverrides` binding, `ApplyEnv` publish + never-clobber, unrecognised
  env/settings reporting, the tag shape, the catalog row) and
  `internal/agent/output_language_prompt_test.go` (7 cases: byte-identity of the
  **chat** prompt against a FROZEN pre-knob copy, byte-identity of the task
  prompt against the registry's own join computed without the helper, directive
  at the head of both, compaction carries none, clear-restores-silence).
- **`CONFIG.md`** — new `#### Reply language` section with the channels,
  precedence, the reporting contract and the read-at anchors (the config-doc
  gate stays green).

## Not Done

- **The model's REASONING language is deliberately untouched** — the ruled
  scope. `grep -rn 'reasoning' internal/config/output_language.go` finds only the
  comment saying so.
- **Compaction (site 4) carries no directive** — a scope decision, asserted by
  `TestCompactionPromptCarriesNoDirective` rather than claimed. Its prompt asks
  for a summary written *into the context*, not a reply to the user.
- **No directive reaches the `gopi ui` / `gopi term` TUI's own chrome**: those
  are the same two agent prompt paths underneath, so they are covered, but I did
  not run the interactive TUI (no TTY in this session) — the `--once` face of the
  same verb is what I executed.
- **The reasoning-effort/banner reporting of this knob is not on the startup
  banner** (unlike `task_sync`): a language tag is a value, not a switch with an
  "off" that needs distinguishing from "not declared", and the knob already
  reports itself on stderr when a value is refused. Say so if a banner line is
  wanted — it is a ten-line change, not a gap in the resolution.
- **A pre-existing, unrelated red**: `internal/standards` `TestStandardGates`
  Rule 17 reports ONE new unbaselined `os.Setenv` at
  `cmd/agent/cli_tasks_list_order_test.go:236` — **not this change** (that file is
  untouched here; verified with `git diff --name-only`). Everything this change
  adds is `t.Setenv`-only. Likewise
  `internal/agent TestDocAnchorsResolveInNormativeDocs` fails: I checked it out
  at master in a throwaway worktree and it fails **there too** — the `cov_
  finish_cleanup_test.go` past-EOF drift in `devdoc/assert-the-bug-audit-*.md`
  (0 of its findings name `CONFIG.md`, so my doc anchors resolve).

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → no output (OK);
  `CGO_ENABLED=0 go build -o /tmp/gopi-new ./cmd/agent/` → OK.
- **Tests**:
  `go test ./internal/config/ -run OutputLanguage` → `ok github.com/nick/gopi/internal/config 0.796s` (14/14).
  `go test ./internal/agent/ -run 'OutputLanguage|ChatPromptByteIdentical|TaskPromptByteIdentical|TaskPromptCarries|ChatPromptCarries|CompactionPromptCarries'` → `ok … 1.275s` (7/7).
  `go test ./internal/config/ ./internal/agent/` → config `ok`; agent fails ONLY
  on the pre-existing `TestDocAnchorsResolveInNormativeDocs` (master-red, see
  Not Done).
  `go test ./internal/standards/ -run TestConfigDoc` → `ok` (the catalog/doc gate accepts the new row + section).
  `go test ./cmd/agent/ -run 'Chat|TermOnce'` → `ok … 14.035s` (the chat prompt-prefix assertions are unbroken).
- **Ran**: `bash /home/user/.gopi/sessions/01a0dde7-30e8-7650-ba32-b7f773165944/evidence/run-output-language.sh`
  with `BIN=/tmp/gopi-new` (this branch) and `RAW=/tmp/gopi-master` (master
  `933c9a8c`, built the same way) — 8 real runs, each with its own
  `GOPI_HOME` + `GOPI_SETTINGS` under `/dev/shm` and `env -i`, so the operator's
  live profile was never written. The prompt under test is read back from the
  run's own **raw log `"system"` block** — the request actually sent.
  Also: `gopi --help-env`, `gopi -h`, `gopi chat -h`.
- **Result** (table also in `evidence/evidence-table.txt`):

  | case | channels set | directive at head | sys len | language |
  |---|---|---|---|---|
  | c1 unset | — | **False** | 201 | — |
  | c2 env only | `GOPI_OUTPUT_LANGUAGE=zh-CN` | True | 491 | zh-CN |
  | c3 settings only | `"output_language":"en-GB"` | True | 491 | en-GB |
  | c4 precedence | settings `fr` + env `ja` + `--output-language pt-BR` | True | 491 | **pt-BR** |
  | c5 unrecognised | env `"chinese (simplified)"` + settings `fr` | True | 485 | **fr** (env refused, stderr line emitted) |
  | c6 tools path | env `zh-CN`, `chat --once --tools` | True | 4622 | zh-CN |
  | c7 task path | `--output-language zh-CN`, bare run | True | 4622 | zh-CN |
  | c8 master | no knob, master binary | False | 201 | — |

  **Byte-identity**: c1 (this branch, unset) vs c8 (master, no knob) — identical,
  same length and same sha256 `c582d0432d3a4501e034ba18632d0c033839967a48fe0a1b88590a3157d1deb7`.
  c5's stderr, verbatim:
  `config: GOPI_OUTPUT_LANGUAGE="chinese (simplified)" is not a language tag (want e.g. zh, zh-CN, en-GB) — ignored; the next channel decides`.
  `gopi --help-env` renders `GOPI_OUTPUT_LANGUAGE / output_language / --output-language`.
- **Commit**: `ff6cd4c8` `feat(config): reply-language knob — --output-language / GOPI_OUTPUT_LANGUAGE / settings output_language`
  (a second commit carries this report and the machine-regenerated `devdoc/DOC-INDEX.tsv` row for it).

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0, no diagnostics.
- `go test ./internal/config/ -run OutputLanguage` → `ok github.com/nick/gopi/internal/config 0.796s` (was FAIL 3/14 before the `BootstrapEnvFromArgs` hoist was added — red-then-green).
- `go test ./internal/agent/ -run 'OutputLanguage|ChatPrompt…'` → `ok github.com/nick/gopi/internal/agent 1.275s`.
- `go test ./internal/standards/ -run TestConfigDoc` → `ok github.com/nick/gopi/internal/standards 3.021s`.
- `go test ./cmd/agent/ -run 'Chat|TermOnce'` → `ok github.com/nick/gopi/cmd/agent 14.035s`.
- byte-identity hashes: `sha256(c1 prompt) == sha256(c8 prompt) == c582d0432d3a4501e034ba18632d0c033839967a48fe0a1b88590a3157d1deb7` (201 bytes each).
- master-red of the unrelated agent test: `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs` in a detached worktree at `HEAD` (master `933c9a8c`) → `--- FAIL` — same failure, so it predates this change.
- `gofmt -l` on the three new files and every touched Go file → the only entries are `internal/config/config.go`, whose diff is **pre-existing** formatting drift (the ①② comment bullets, untouched by this change).

## Addendum 2026-09-27 — the PREPEND contract, re-anchored (batch-merge semantic conflict)

**Symptom.** After the 2026-09-26 batch merge, `TestTaskPromptCarriesDirectiveAtHead`
went RED on master while it was green on `ff6cd4c8` (this knob's own branch tip):
`output_language_prompt_test.go:127: the directive must PREPEND the prompt, never
replace it`.

**Root cause — the assertion, not the composition.** The composition itself was
correct on master (`systemPromptWithOutputLanguage` prepends; `assembleSystemPrompt`
joins the registry) and the head assertion passed. The failing assertion pinned the
head by a BODY SENTENCE: `strings.Contains(got, "You are GoPi, in gopi's own source
repo")`. That sentence used to be the registry's `header` entry. The self-knowledge
branch (`a72cfaa7`, the direct parent of `78767c5b`) moved it OUT of the header and
INTO the SELF-KNOWLEDGE block, reworded (`"You ARE gopi, in gopi's own source repo"`),
and the header became `"You are GoPi. Use tool_use blocks; a tool's description says
when to use it."` — which is master's header today. Both branches were green in
isolation: nothing in the language branch's own tree contained the newer header, so
its assertion still found its sentence. Git merged both cleanly (different regions),
and the semantic conflict only surfaced in the merged tree.

This is the shape memory #3072 names: **never gate on a RENDERED sentence** — the
gate goes red (or silently quiet) when a sibling branch rewords the surrounding text,
because the sentence lives outside the contract being protected.

**Fix.** The PREPEND contract is now asserted against the BODY, not against a
sentence inside it:

- `taskPromptBody(t)` drives the REAL composition path (`assembleSystemPrompt()`) with
  the knob cleared and returns what it produces — the body the production code itself
  gives when it has nothing to prepend. Deriving the expectation from the path under
  test is what makes it immune to rewording; a stale copy would fail, and a
  body-replacing path still fails.
- the configured prompt must contain that body, END WITH it, and carry it after
  `promptSectionSeparator` — placement proven by suffix and by the registry's own
  separator constant, not by a re-typed sentence.
- the absence assertions now share `outputLanguageDirectiveMarker` (Rule 1/13), and
  `TestTaskPromptCarriesDirectiveAtHead` asserts the prompt STARTS with that marker —
  so the marker is self-pinning: if the config side's wording changes, the head
  assertion fails instead of every absence assertion silently passing.

**The header keeps its bytes, deliberately.** No reply-language sentence was added to
the header: `TestSystemPromptDensity` (~4.5K target) is a hard budget the assembly sits
within a few characters of, and the directive is at the head by construction, so a
second statement of the rule buys nothing and does not fit. A static header is also
what makes the PREPEND contract assertable byte for byte.

**Placement note (Rule 1 without collateral damage).** `header` and its text were bare
literals inside `promptSections` and are now `headerSectionName` / `headerRules`,
referenced by name. The two declarations sit at the **end of `loop.go`**, not beside
their only reader: several landed devdocs cite mid-file symbols in that file by BARE
line number, so inserting them beside `promptSections` moved six anchors off their
symbols and made the (pre-existing-red) doc-anchor gate six entries redder. The diff
is therefore exactly two hunks — a 1:1 literal-for-constant swap at the registry line,
and a pure append after the file's last anchorable line — and the doc-anchor gate's own
summary is byte-identical to the base tree's (`224 pre-existing drift entries
permitted, 1 new, 0 stale permits`, the one new being `internal/tools/websearch.go:235`,
untouched here and red at base too).

**Evidence (real surface, not just the test).** A real run in a sandbox with
`GOPI_OUTPUT_LANGUAGE=zh-CN`, then `gopi raw <session> --sysprompt` (the system prompt
AS SENT, verbatim) vs the same run with the knob cleared:

```
sent_prompt_head      : [OUTPUT LANGUAGE — set by the operator, overrides everything below] …
starts_with_directive : true
LAYER1 body identical : true
LAYER1 arithmetic     : 292 + 2 + 4485 = 4779  (len(set)=4779)
unset prompt has NO directive: true
body has header / repo identity / [SELF-KNOWLEDGE : true / true / true
```
