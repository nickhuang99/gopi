<!-- doc-class: historical -->
# devdoc: session-dir-merge (flat → 3-layout migration)

## What / why
`~/.gopi/sessions/` had 1230 legacy flat `<uuid>.jsonl` files alongside the
new per-session dirs (uuid skeleton dirs, t-* task-run dirs). All readers
(collectSessionFiles, FindSession, taskFindSessionJSONL, orphan detection)
already scan all three layouts and resolve sessions by the **session_start
sessionId**, so moving flat files into dirs never breaks tracking — the
tasks DB (`memory.db`) needs zero changes.

## Classification algorithm (the migration's core)
Anchor = basename for uuid-shaped flat files, else session_start sessionId
(covers the one oddball `zz-cjk-test.jsonl`).
- TRUE: some tasks row `session_id==uuid` has `task_id!=uuid`. Placement task
  dir = the EARLIEST such row (`created_at ASC, task_id ASC`).
- AUTO: rows exist but every `task_id==uuid` (old auto-mirror skeleton rows).
- ORPHAN: no tasks row `session_id==uuid`.

35 uuids have BOTH auto rows and true rows (a uuid reused later as a real
task's run): auto row keeps linking by uuid scan — no special handling needed.

## Pitfalls found (all fixed)
1. **Live-DB volatility**: the tasks table is written by external gopi
   processes — classification must read ONE consistent snapshot. Use
   `sqlite3 DB ".backup <tmp>"` (WAL-safe) and classify against the copy.
2. **Bash `set -u` + empty array**: `declare -a flat` then `${#flat[@]}`
   errors "unbound" when no files match — initialize `flat=()` explicitly.
3. **jq object-building with empties**: `.model = (if … then … else empty
   end)` on the WHOLE object deletes the object when the value is empty;
   build the object by conditional ADDITION instead
   (`{session_id: X} + (if $model != "" then {model:$model} else {} end)`).
4. **Separator hygiene**: sqlite3 `-separator` must match the read delimiter
   (`IFS='|'` ↔ `-separator '|'`); a `\x1f`/`|` mismatch silently yields one
   field (model + created concatenated).

## Raw logs: why NOT moved (divergence from epic premise)
Epic said move `<uuid>.raw.log` with the jsonl. Reality: raw logs are
`~/.gopi/raw/gopi-raw-<timestamp>.log` (timestamp-named, 1432 files), and
sessions reference them by an ABSOLUTE path recorded in session_start
(`rawLogPath`). ResolveRawLogForSession + SessionsRecordingRawLog resolve by
that recorded path regardless of where the jsonl lives. Moving the raw log
would orphan the recorded absolute path (breaking `gopi raw`) with no DB
rewrite allowed → raw/ stays put. New task-run sessions already store raw
logs next to the jsonl only when GOPI_RAW_LOG_DIR is redirected into the
session dir; the default raw/ location is untouched by this migration.

## Verification recipe (proven)
Sandbox first: `--home <tmp>/.gopi` with a fixture memory.db (3 task rows:
t-…/auto/true) + 4 flat files (1 true, 1 auto, 2 orphan incl. non-uuid);
assert plan counts, moved counts, metadata.json contents, and that a second
`--execute` prints "no flat session files at root — nothing to do". Then the
real tree with `gopi stats|log|raw <uuid>` per class (numeric views are
context-safe; `gopi log` on a session with MB-scale tool-result lines dumps
huge output — use `gopi stats` first and pick small sessions for `log`).
