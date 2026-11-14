<!-- doc-class: historical -->
# Task Report: cmd/agent CLI → toolnames constants (fix the un-imported mirror)

Epic branch: `fix/cli-toolnames-import` · Bug: [hardcoding] cmd/agent CLI 层未导入
toolnames — 工具名扁平别名/子命令硬编码 (t-1788904350)

## Completed

Converted the cmd/agent CLI **tool-wire mirror** sites to reference the
`internal/toolnames` constants (Rule 1 single source of truth), per the
package-header contract "the CLI mirrors … MUST use these constants":

- **cli_websearch.go** — argv predicate `args[0] == "web_search"` →
  `== toolnames.ToolWebSearch`.
- **cli_webfetch.go** — `"web_fetch"` → `toolnames.ToolWebFetch`.
- **cli_vision.go** — `"vision_analyze"` → `toolnames.ToolVisionAnalyze`.
- **cli_text2img.go** — `"text2img"` → `toolnames.ToolText2img`.
- **cli_memory.go `memoryLegacyAliases`** — the legacy flat-verb KEY set that
  is a tool wire name now references toolnames: `memory_search` →
  `toolnames.ToolMemorySearch`, `memory_store`/`memory_update`/
  `memory_feedback` likewise.
- **cli_log.go `toolFamilies`** — the map VALUES that select agent tool names
  for log filtering now reference toolnames (web/code_search/keyword_search/
  job/context/vision/text2img/git + memory family tool entries).
- **cli_completion.go `topLevelCommandNames`** — the two entries that ARE tool
  wire names (`text2img`, `vision_analyze`) reference toolnames so a rename
  propagates to the completion candidate.
- **Contract test `cli_toolnames_contract_test.go`** (new) — three drift
  guards mirroring the dispatcherOfToken idiom:
  - `TestCLIToolMirrorVerbsMatchConstants`: each CLI surface verb that mirrors
    a tool wire name still equals its toolnames constant.
  - `TestCLIMirrorVerbsCoveredByContractInventory`: each argv tool-mirror
    subcommand has a live-binary contract case (rename/regression coverage).
  - `TestCLINonToolSurfaceTokensDoNotOverlapTools`: the deliberately-non-tool
    CLI verbs (see Not Done) are pinned so a future toolname colliding with one
    forces an explicit coupling decision.
  - These do NOT invoke the argv predicates directly (they os.Exit — the
    established convention routes the positive path via live-binary smoke).

## Design-tension resolution (why this scope)

`internal/toolnames/names.go` (authored in 04d1bce) states the header contract
"the CLI mirrors … MUST use these constants". The SAME commit's devdoc
`tool-name-constants.md` "Not Done" listed cmd/agent as intentionally-literal
"public argv/flag surface". These two statements CONTRADICT each other. The
package header is the governing contract (Rule 1: named constants, single
source of truth) and explicitly names the CLI mirrors — so cmd/agent was a
genuine miss, not a sound boundary. This fix reconciles both: the 1:1
tool-wire MIRROR sites (argv predicates, legacy aliases, log-family VALUES)
now read the constant, while genuinely user-only surfaces stay decoupled words.

## Not Done (deliberate, documented exemptions — each with a guard comment)

Binding EVERY cmd/agent string that looks like a tool name to the constant
would wrongly couple separate user-surface layers to the wire protocol. These
remain literal BY DESIGN (comments reference toolnames where the semantics
overlap but the coupling is intentional):

- **`code-search`** (commands.go / cli_code_search.go / completion): the
  hyphenated CLI NAME of the code_search TOOL. A rename of the tool must not
  rename the CLI command word; separate stable surface with alias spellings.
- **`cli_memory.go` runMemoryGroup/runMemorySubcommand/parseMemoryArgs/memberOf
  discriminator strings** (`memory_search`/`store`/`update`/`feedback` and the
  CLI-only `get`/`list`/`delete`): an internal memory-operation KEY threading
  a Go switch, NOT a CLI wire mirror — always followed by a typed
  tools.MemoryX() call. This namespace is inherently mixed tool+CLI verb
  (get/list/delete have NO toolnames constant), so a partial constantization
  would be confusing. Documented at runMemoryGroup.
- **cli_log.go log-flag shorthand** (`memory`/`search`/`web`/`text2img`/
  `code-search` as the `gopi log --<family>` FILTER WORDS and familyOrder):
  the log command's user-facing filter vocabulary, distinct from tool names.
- **commands.go `slashCommands` + `commandAliases`**: interactive `/`-command
  registry and spellings (/text2img, t2i → text2img). User word tables;
  aliases point at slash COMMAND names, not tool constants.
- **completion shell names** (`bash`/`zsh`/`fish`) and other standalone
  command words (`brew`/`chat`/`task`/…): never tool wire names.

## Verification

- Build: `go build ./cmd/agent/` OK; `go build ./...` OK (full)
- gofmt: all changed files clean (verified)
- go vet ./cmd/agent/: clean
- Contract tests: `go test ./cmd/agent/ -run 'TestCLIToolMirror|TestCLINonTool
  Surface'` PASS (new suite)
- Targeted regression: `go test ./cmd/agent/ -run
  'TestCLIToolMirror|TestCLINonToolSurface|IsWebSearch|IsWebFetch|IsVision|
  IsText2img|MemoryLegacy|CompletionCandidate|LogFamily|LogSubcommand'` PASS;
  earlier full affected group 'Memory|Web|Vision|Text2img|Log|Contract|
  Completion' PASS (~23s incl. live-binary contract suite); full cmd/agent
  package suite confirms overall health
- Deprecation smoke (real `/tmp/gopi-cli-verify` binary): `web_search -h` and
  `memory_search -h` still print the deprecation note then the group usage;
  group forms / tool-verb subcommands route correctly.
- Grep: bare tool wire strings in cmd/agent non-test source reduced to the
  documented-exempt user-surface categories above (each with guard comments).

## Commit

9148a09 fix(cmd/agent): CLI tool-wire mirrors reference internal/toolnames
(package-header contract) — branch fix/cli-toolnames-import
