<!-- doc-class: historical -->
# CLI surface audit (whole of `gopi -h`) — same disease as `gopi task merger`?

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Author:** PA, 2026-09-15
**Trigger:** after the `gopi task merger` duplicate-help finding, the operator asked
whether similar cases exist elsewhere in gopi's help.
**Method:** read the whole top-level index (`gopi -h`), then ran `-h` on every
command and group, and cross-checked against the two existing ground-truth
inventories: `cmd/agent/cli_contracts_test.go` (`cliContractInventory`) and
`internal/toolnames` (the Rule-14 constant package).

## Answer: yes — three more cases, and one of them already has the correct fix

The `gopi task merger` disease has three distinct shapes. They are NOT the same
bug and should not be fixed the same way.

### Case 1 — DEPRECATED SPELLING ALREADY HANDLED CORRECTLY (`web_search` / `web_fetch`)

This is the *model* the `merger` case should follow:

```
$ gopi web_search -h
note: gopi web_search is deprecated — use gopi web search

$ gopi web search -h
GoPi web search — expose the web_search tool as a CLI command.
```

- Old spelling still dispatches (script compatibility preserved).
- It prints a **one-line** pointer, not a second full help text.
- The top-level index advertises only the canonical spelling
  (`gopi web search`, `gopi web fetch` — `gopi -h` L97-98) and never the old one.
- Both spellings are tracked in `cliContractInventory`
  (L77-78 `web_search`/`web_fetch`, L94-95 `web search`/`web fetch`) — so the
  compat surface is *pinned by test*, not by memory.

**Verdict:** correct as-is. **Use this as the template** for `gopi task merger`,
`gopi task serve`, and any future rename.

### Case 2 — THE SAME DISEASE AS `merger`, UNFIXED: `gopi memsync` vs `gopi memory sync`

Two top-level commands, both documented as standalone, both syncing memory:

- `gopi memory sync` (sub-command of the `memory` group) — "cross-node
  memory-asset sync (unique-only)… distinguished_qa knowledge rows… push OUR
  unique rows to them, pull THEIRS back". Peers from `memory_peers`.
- `gopi memsync` (its own top-level verb) — "node→node sync (git-PR semantics).
  Two channels: **brew rows (knowledge base) and task rows (task ledger)**".
  Does `--export/--import`, `--export-proc`, `--dedup-scan`, `--export-tasks`,
  `--import-tasks`.

**This is worse than the merger case**: neither help text mentions the other, and
**neither declares itself deprecated**. The operator reading `gopi -h` sees both
on adjacent lines and cannot tell which to use:

```
  gopi memory <subcommand>    … and `gopi memory sync` for cross-node sync.
  gopi memsync                Node-to-node knowledge and ledger sync, as patch files.
```

The overlap is real: both move knowledge rows between nodes. `memsync` is the
older patch-file machinery (M4.8-era `--export`/`--import`), `memory sync` is the
newer unique-only one-shot session verb (M4.13-era, born of the
"export batches are useless, aim at sync" correction). `memsync` additionally
carries `--export-tasks`/`--import-tasks` (the task-ledger patch channel) and
`--dedup-scan` (an audit, not a sync at all — misfiled).

**Verdict:** genuine duplicate surface, unpinned by any deprecation note.
Requires a decision: is `memsync` superseded by `memory sync` (→ deprecate like
`web_search` do, keep dispatch, one-line note), or do they do different jobs
(→ the help of EACH must say what the other is for, and `--dedup-scan` should
move out of both)?

### Case 3 — NAMING CONVENTION IS SPLIT (underscore vs dash)

The top-level index mixes two styles with no rule:

| underscore style | dash style |
|---|---|
| `gopi vision_analyze` | `gopi code-search` |
| `gopi screen_capture` | `gopi scratch-gc` |
| `gopi voice_input` | (also: `web search` / `web fetch` use a space) |

The underscore spellings are exactly the ones derived from **tool names**
(`internal/toolnames`: `ToolVisionAnalyze="vision_analyze"`,
`ToolScreenCapture="screen_capture"`, `ToolVoiceInput="voice_input"`), while
`code-search` / `scratch-gc` are hand-chosen CLI verbs. `text2img` is a tool name
too but is a single word so it dodges the issue.

Consequence: the dash variants do **not** work as aliases
(`gopi voice-input -h` → falls through to the top-level help), which is fine,
but a user has no way to *predict* which style a given command uses. And the
help *text* disagrees with the command spelling — `gopi voice_input -h` opens
with "GoPi **voice input** — speech-to-text via Fun-ASR-Nano", i.e. the prose
uses the human spelling while the command uses the tool spelling.

**Verdict:** consistency problem, not a duplicate. Needs a stated rule (e.g.
"CLI verbs are dash-cased; a tool-name command is a deliberate exception and
must say so in its help") or a normalisation. Note the `web_search`→`web search`
rename already chose the *space* form for a tool-named command — so the
precedent points AWAY from underscore, meaning `vision_analyze` /
`screen_capture` / `voice_input` are the outliers.

### Case 4 (minor) — `gopi task serve` has no help of its own

`gopi task serve -h` prints the **whole `gopi task` family usage** (not a
pointed note), unlike `web_search` which prints a one-line pointer. It is
documented in `taskverbs.Serve`'s comment as a deprecated alias that
deliberately stays out of `FamilyVerbs` — so the *design* is right and only the
*hint* is wrong (it should be a one-liner like `web_search`'s).

## Things checked and found CLEAN (no duplicate help)

- `gopi merger -h` / `gopi worker -h`: each is a single, complete, distinct help
  for a distinct service. The duplicate was only inside `gopi task -h`
  (already briefed as `brief-cli-task-surface-restructure.md`).
- `gopi checkpoint` / `gopi resume`: share one help page by design
  ("GoPi checkpoint & resume subcommands — continue an interrupted session…"),
  and `resume -h` is the pointed usage. Coherent, not duplicated.
- `gopi shot` / `gopi screen_capture` / `gopi act`: three genuinely different
  capabilities (URL/self-page screenshot via CDP vs. live-X11 capture vs. live-X11
  input injection). Overlapping subject area, but no duplicated function — and
  each help says which is which.
- `gopi web -h` vs `gopi web search -h` / `web fetch -h`: the group help describes
  the chat UI and does **not** re-document search/fetch. Clean group structure —
  this is what the `task` group should look like.
- `gopi log` / `gopi stats` / `gopi raw`: three distinct readers of the same
  session data (JSONL content / numbers / byte-exact HTTP). Checked for
  duplicated blocks; each has its own scope. Clean.

## What this means for the in-flight briefing

`brief-cli-task-surface-restructure.md` (task `01a0a1e7-2234-76a4-a596-cab3bb6fd2a4`)
covers the `task` group. Findings Case 1–4 above are **outside** that brief's
scope (different groups / whole-CLI conventions), so they need their own task —
otherwise the fix stops at one group and the same confusion survives two inches
away.

## Recommendation

1. **Adopt Case 1's treatment as the CLI-wide deprecation contract**: a
   renamed/deprecated spelling keeps its dispatch case, prints ONE pointer line,
   and never appears in the top-level index or its own group's usage. Pin it with
   a contract-suite entry (the `web_search` precedent already does exactly this).
2. **Resolve `memsync` vs `memory sync`** — the only true duplicate-untouched
   pair on the whole CLI. Requires an operator decision on whether they are
   superseded/siblings; do not let the implementer guess (see the brief's
   instruction to report back rather than invent).
3. **State the naming rule** and either normalise the three underscore commands
   or document them as deliberate exceptions.
4. **Give `gopi task serve` a one-line pointer** instead of the family dump.
