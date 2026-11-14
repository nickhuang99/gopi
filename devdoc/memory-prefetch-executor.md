<!-- doc-class: historical -->
# Typing-time memory prefetch — the executor wired into the chat input path (epic child 3/3)

## Completed

- **One three-channel switch, one reader** — `internal/config/memory_prefetch.go`:
  `--memory-prefetch` (hoisted into the env before the subcommand chain, so
  `gopi --memory-prefetch chat` reaches a verb that returns from `main()` before
  the agent-path config block) > `GOPI_MEMORY_PREFETCH` > settings.json
  `memory_prefetch`. **Default OFF**, resolved at ONE site
  (`resolveMemoryPrefetch`), with `MemoryPrefetchSource()` reporting *which*
  channel decided. The env channel accepts the truthy/falsey vocabulary; a value
  that is neither is reported on stderr with the next channel deciding (Rule 15).
- **The pure core** — `internal/memoryprefetch` (stdlib only, no DB, no
  goroutine): keyword extraction for CJK runs **and** ASCII words, the complexity
  gate whose PRIMARY signal is the keyword count and whose secondary is the input
  length, the debounce + **differential** state machine (re-fire only on NEW
  keywords), the token-free budget trim, and the provenance-labelled,
  LLM-judgeable RAG rendering. Stated in-file as a MINIMAL slice: its sibling
  pure-logic package (task `01a0e53e-d4ac`) is not on master.
- **The executor** — `cmd/agent/chat_prefetch.go`: a goroutine started ONLY when
  the switch is ON, sampling the editor's *published* line snapshot
  (`Editor.Value().Text()` — no new lineedit seam), running ONE read-only lookup
  through `tools.LedgerReadOnlyDSNIn` (`mode=ro` + `query_only(true)` + its own
  250 ms busy timeout) under a 1500 ms deadline, and **giving up** (recorded)
  rather than queueing behind a writer.
- **The adoption point** — one function, `chatPrefetchAdopt`, called at the
  outbound-message boundary in `cmd/agent/cli_chat.go`: the section is appended
  to `out.Message` and **never** to `Display` (the operator's own line on
  screen). It never waits for an in-flight lookup.
- **The observable** — one JSON line per event in the run's session directory
  (`memory-prefetch.jsonl`): query / hits / ms / outcome / adopted (+
  `section_bytes` on the adoption event). Rule 35: a file inside the session
  directory that already exists — no daemon, no scheduler, no second database.
- **CONFIG.md** row + `prefetch_bound_env()` (the flag whose value is optional,
  so the hoist's peek is conditional and a bare `--memory-prefetch` before a verb
  cannot eat the verb).

## Not Done (honest)

- **`internal/memoryprefetch` is this run's minimal slice**, not child 2's full
  core (that branch is not on master — checked: `git log --all --diff-filter=A --
  internal/memoryprefetch` returns nothing, and the sibling worktree has no such
  tree). Child 2's package, when it lands, is the merge base for this file.
- **The `gopi chat --tools` path (the chat's own agent loop) is not wired** —
  see the architecture note below. This change covers `gopi chat` (the REPL +
  one-shot path) — the surface the epic names — and not the `--tools` variant.
- **No unit test drives the live loop end to end inside `go test`** (it would need
  a chat session and a provider). The loop IS driven for real (Rule 31: the pty
  run below), and the seams it uses are unit-tested through `chatPrefetchFor` /
  `chatPrefetchAdopt` / `prefetchLookup`.
- **The prefetch never fires for a line typed and submitted inside one debounce
  window**: the poll samples the snapshot on a 100 ms tick, so only a line that
  PAUSES is prefetched. That is the design (it is what keeps the prefetch off the
  keystroke path), not a defect.
- `config_doc_flag_gate_test.go`'s `configDocFlagRowCount` 60 → 61 (the row
  entered the check; the constant is the audit trail, and its comment records it).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK (28 MB binary).
- Tests: `make test-budget PKGS="./internal/memoryprefetch ./internal/config
  ./internal/tools ./cmd/agent ./internal/standards"` — the run log is quoted
  under Evidence. Focused: `go test ./internal/memoryprefetch/` ok;
  `go test ./cmd/agent/ -run Prefetch` ok; `go test ./internal/config/` ok;
  `go test ./internal/standards/` ok.
- Ran (the changed surface, verbatim, pty-driven so the REPL sees a real
  terminal):
  - **ON**: `python3 /dev/shm/pty_drive.py ./bin/gopi --memory-prefetch chat --verbose`
    — typed `the memory prefetch seam ` → `reads a published line snapshot ` →
    `and opens the ledger read-only`, pausing >600 ms between chunks, then Enter.
  - **OFF**: the same driver with `./bin/gopi chat` (no flag, no env, no settings
    key).
- Result:
  - ON printed `[gopi] memory prefetch: on (source: GOPI_MEMORY_PREFETCH) — record:
    /home/user/.gopi/sessions/bb173df0-…/memory-prefetch.jsonl`, then
    `[gopi] memory prefetch: adopted into this turn's prompt (1993 bytes)`, and its
    record holds

    ```
    {"query":"memory prefetch seam","hits":3,"ms":56,"outcome":"hits","adopted":"yes"}
    {"query":"memory prefetch seam reads published line snapshot","hits":3,"ms":113,"outcome":"hits","adopted":"yes"}
    {"query":"memory prefetch seam reads published line snapshot opens","hits":3,"ms":88,"outcome":"hits","adopted":"yes"}
    {"query":"memory prefetch seam reads published line snapshot opens","outcome":"adopted","adopted":"yes","section_bytes":1341}
    ```

    and the ON raw log contains the section header exactly once
    (`grep -c "gopi memory prefetch — background retrieval" <session>/*.raw.log` →
    `1`) — the block rode the model's INPUT, and the reply afterwards references
    the prefetched hits by id (`#6425`, `#6577`).
  - OFF printed no prefetch line at all, wrote **no** `memory-prefetch.jsonl`, and
    its raw log contains the header `0` times.
- Commit: the branch tip of `feat/memory-prefetch-executor`.

## Evidence (self-filed)

- Three channels: `go test ./cmd/agent/ -run TestMemoryPrefetch -count=1` → `ok`
  (nothing declared / settings on / settings off / env only / env off beats
  settings on / flag only / flag off beats env on / flag on beats settings off /
  unusable env falls through to settings). The rejection line was observed in that
  run's own output: `config: GOPI_MEMORY_PREFETCH="chat" is not a recognised
  boolean (1/off) — ignored; memory_prefetch decides`.
- OFF starts nothing:
  `TestPrefetchOffStartsNoGoroutineAndLeavesThePromptByteIdentical` → measured
  `runtime.NumGoroutine()` delta ≤ 0, `chatPrefetchFor` returned nil, and
  `chatPrefetchAdopt` returned the message unchanged.
- Give-up: `TestPrefetchGivesUpOnABlockedLedger` → with a `BEGIN EXCLUSIVE` holder
  the outcome is `gave-up` with a non-nil error, inside the bound.
- Flag hoist: `TestMemoryPrefetchHoistCarriesTheFlagToASubcommand` →
  `BootstrapEnvFromArgs(["--memory-prefetch","chat"])` arms the env and returns
  `[chat]` (the verb survives).
- Real run: the pty invocation above, with the printed lines and the record file
  quoted above; the OFF session directory holds no `memory-prefetch.jsonl`.
- Pre-existing failure repaired in passing: `internal/config`'s
  `TestSessionsDirNameNoHardcodedLiterals` was RED on master — the gate scans
  `internal/`, and master's own `internal/uiclient/dumpread.go:801` comment
  contained a quoted `"sessions"` with no exemption marker. Not caused by this
  branch (`git diff master --stat -- internal/config/sessions_dir_name_test.go`
  empty), fixed by rewording that comment to `re-typed spelling of "sessions")`,
  which is the gate's own documented exemption. `internal/config` is green after it.

## Architecture note (why `chat --tools` is out of scope)

`gopi chat` exposes two faces. The plain face seals its prompt in
`cmd/agent/cli_chat.go` and calls the provider from the same file's turn loop —
the seam this run wired. The `--tools` face does not call the provider there at
all: `chatBuildConfig` sets `a.TaskSystemPrompt` and the *unexported*
`a.chatOverrides`, and the turn is executed by the shared agent loop
(`internal/agent`), whose outbound-message boundary is what would have to adopt
the section. That is a different seam with its own tests, and wiring it here would
also mean one prefetch feeding two boundaries. It is named as remaining work
rather than claimed.
