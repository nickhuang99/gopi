<!-- doc-class: historical -->
# Sandbox plugin tree + `web search` exit status — the two halves of "dead inside a sandbox"

Task: `01a0dde884ad772aa90f138b7db68981` · date 2026-09-27 · branch
`task/01a0dde884ad772aa90f138b7db68981`

## The defect (measured, not inferred)

A Rule-34 sandbox is a `GOPI_HOME`. The web-search tool resolves its python3
plugin scripts through `config.GopiDir()`:

```
$ SB=$(gopi sandbox create --label websearch-repro | tail -1)
$ GOPI_HOME=$SB gopi web search x; echo $?
{"status":"unavailable","method":"merged",…,
 "message":"websearch scripts dir not found: …/.gopi/plugins/websearch/scripts"}
0
```

Two independent failures in one line:

1. the sandbox initializer never materialised `<profile>/plugins`, so the
   profile was one the tool could not run in at all (the operator's profile has
   the tree: `~/.gopi/plugins/websearch/scripts/{bing,google,arxiv,wiki,github}_search.py`,
   `merge_results.py`, `web_fetch.py`);
2. the tool's "I could not run" answer came back as **exit 0**, so a scripted
   caller had nothing to branch on — the only signal was prose inside a JSON
   line.

## Root cause (5-why)

- *symptom*: a search inside a sandbox returns `unavailable`.
- *mechanism*: the scripts directory is `<GOPI_HOME>/.gopi/plugins/websearch/scripts`,
  and nothing created it inside the sandbox.
- *norm*: `GOPI_HOME` relocates the **whole** profile root, so every profile-
  scoped resource must be materialised by the initializer or linked into it —
  the sandbox's own doc says so for `sessions/raw/logs/settings.json/memory.db`.
  The plugin tree was simply not on that list.
- *why it stayed invisible*: the path was spelled **twice** (once in
  `internal/tools`, once nowhere) as a chain of bare literals, so no consumer
  could compare "what the tool resolves" against "what the sandbox made". Nothing
  owned the path.
- *why nobody noticed the exit status*: the tool's contract is "never fail — the
  LLM reads a payload", which is right for a *tool* and wrong for a *CLI*: a
  process exit status is the one signal a shell script can read without parsing.

## The fix

### 1. ONE resolver (`internal/config/plugins.go`)

`config.WebSearchScriptsDir()` is now the single reader of
`GOPI_WEBSEARCH_SCRIPTS_DIR` (the env name moved here, Rule 1b/2), and the joins
are named functions over named leaves:
`PluginsRootIn(gopiDir)` = `<gopiDir>/plugins`,
`WebSearchScriptsDirIn(gopiDir)` = `<gopiDir>/plugins/websearch/scripts`.
`internal/tools/websearch.go`'s resolver is now a one-line delegation, and it no
longer spells `"plugins"`, `"websearch"`, `"scripts"` or the env name as
literals.

### 2. The initializer links the tree (`internal/sandbox/plugins.go`)

```
<profile>/plugins -> <the real plugin tree>     (symlink, READ-ONLY by contract)
```

- **Why the TREE and not `<scripts>`**: the tree is the level at which a link
  serves every consumer — the web-search scripts, any other plugin under it, and
  the whole resolution chain. A link straight at `<scripts>` would break the
  moment a second plugin appears.
- **Why a LINK and not a copy**: a copy drifts; the operator updates the deployed
  scripts in place. Same argument, and the same read-only contract, as the
  existing `settings.json` symlink (`ensureSymlink` is reused verbatim).
- **Where the source comes from**: `config.WebSearchScriptsDir()` is *asked*, then
  climbed two leaves (`WebSearchPluginDirName`, `WebSearchScriptsDirName`) to the
  tree. So when `GOPI_WEBSEARCH_SCRIPTS_DIR` points at a deployment mirror, the
  sandbox links **that** tree — the sandbox links what the process actually reads,
  never a hard-coded profile default.
- **Absence is NAMED (Rule 15)**: a host with no plugin tree still gets a valid
  sandbox, but `Sandbox.PluginsSource` carries the source it looked for and
  `PluginsLink` stays empty; `gopi sandbox create` prints
  `plugins=<source>(absent-not-linked)` on its create line. The gap is visible at
  creation time instead of surfacing later as a failing search.
- **Refusals are loud**: a source INSIDE the sandbox (the self-referential shape
  the settings link already refuses) and a source that is not a directory are
  both errors, named.

### 3. `status=unavailable` exits non-zero (`cmd/agent/cli_websearch.go`, `cli_webfetch.go`)

`tools.UnavailableStatus(payload)` (exported, next to the exported
`tools.WebSearchStatusUnavailable`) lets the CLI branch on the same spelling the
payload carries. The JSON is still printed — it holds the reason — and the
process exits `webPluginExitUnavailable = 3`, one constant for both faces of the
plugin (`webFetchExitUnavailable` is an alias, Rule 14). Usage errors stay `1`;
`status=ok` and the natural `status=empty` stay `0`; `status=error` (a URL the
tool itself refuses) stays `0`, because there the tool RAN and answered.

Both pages document the exit status in an `Exit status:` block, and the
unavailable message now names BOTH halves of the fix: the directory it looked in
and the knob that points elsewhere
(`… install the plugin scripts there, or point GOPI_WEBSEARCH_SCRIPTS_DIR at them`),
including the `merge_results.py not found` path (the shape a *partially* populated
scripts directory produces).

## The deliberate unit of the link (why the tree is not materialised per-script)

A tempting alternative — link each `*_search.py` — was rejected: the scripts
directory is the tool's resolution UNIT, the plugin owns the set, and a
per-file link would have to be updated whenever the plugin ships a new method.
The tree is the smallest unit that stays correct as the plugin grows.

## Measured evidence

See the task report (`## Verification`) for the verbatim invocations. Summary:

| case | pre-fix | post-fix |
|---|---|---|
| `gopi sandbox create` profile | `settings.json` only | `settings.json` + `plugins -> /home/user/.gopi/plugins` |
| `GOPI_HOME=$SB gopi web search … --method wiki` | `unavailable` exit **0** | `status ok`, 3 live Wikipedia hits, exit **0** |
| old sandbox (no link) read by the new binary | n/a | `unavailable` exit **3**, message names the path + the knob |
| `gopi web fetch https://example.com` in the sandbox | — | `status ok`, 142 chars |
| `gopi web fetch` with no scripts | `unavailable` exit 0 | `unavailable` exit **3** |

## Files

- `internal/config/plugins.go` (new) — the ONE path/env resolver.
- `internal/sandbox/plugins.go` (new) — the link, its derivation and its refusals.
- `internal/sandbox/sandbox.go` — `Options.PluginsPath`, `Sandbox.Plugins{Source,Link}`,
  the `Ensure` step; package doc updated.
- `internal/tools/websearch.go` — delegate to the ONE resolver, actionable
  messages, exported status helpers.
- `cmd/agent/cli_websearch.go`, `cli_webfetch.go`, `cli_sandbox.go` — exit status,
  help text, create-line reporting.
- `CONFIG.md`, `GOPI.md` — the knob's row (with its reader) and the sandbox shape.

## Not Done (honest)

- `gopi web fetch` is included (same plugin, same failure class) — the briefing
  scoped it as a sibling; nothing in the web surface is knowingly left behind.
- No test drives a REAL search engine from the sandbox suite: the sandbox tests
  use a fixture tree, and the live proof (a real Wikipedia search) was taken by
  hand. A network-dependent test would be flaky and Rule 27's spirit argues
  against manufacturing conditions for it; the resolution property (the directory
  the tool resolves is materialised by the initializer) is asserted instead.
