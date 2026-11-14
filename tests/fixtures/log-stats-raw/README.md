# Fixture: log-stats-raw (real session + paired raw HTTP log)

Golden-test input for `gopi stats` / `gopi raw` (IMPLEMENT-SPEC §4: the
fixtures must be REAL logs, not constructed mocks).

## Files

| file | source |
|---|---|
| `sessions/fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4.jsonl` | `~/.gopi/sessions/fc426fed-e3df-4e30-b73a-3dfdb2a8ccd4.jsonl` |
| `raw/gopi-raw-20260904-204046.log` | `~/.gopi/raw/gopi-raw-20260904-204046.log` |

Both were written by a real gopi run (`deepseek-v4-flash`, workdir
`/home/user/work/GoPi`, 2026-09-04 20:40 local) and are copied byte-for-byte
except for the two documented edits below.

## Edits vs the originals (both required for hermeticity)

1. **`rawLogPath` removed from the `session_start` event.** The recorded value
   is an absolute path on the machine the session ran on
   (`/home/user/.gopi/raw/...`); a test sandbox must not reach outside
   `tests/fixtures/`. With the field gone, pairing falls back to the
   timestamp rule (`FindRawLogForSession`), which matches this fixture by
   filename + session_start time — that fallback is itself a real code path.
2. **The raw log is truncated to its first 5 rounds** (the full file is 476 KB
   / 9 rounds). The cut lands exactly on the round boundary: lines 1..3138,
   the line before round 6's `→ REQUEST` marker, so round numbering, line
   numbers and the per-round structure are the real ones.

The session JSONL is the FULL session (9 rounds / 43 events), which is
intentional: `gopi stats` on this fixture reads the 5-round raw log and its
`--rounds` table must therefore stop at round 5 while the JSONL fallback view
reports 9 — the two sources are never mixed.

## Content-view coverage (`gopi log` golden tests, 2026-09-06)

The same session JSONL also drives the log content-view goldens
(`internal/gopilog/log_golden_test.go` + `cmd/agent/cli_log_golden_test.go`,
snapshots in `golden/log-*.txt`). What the session actually contains:

| view | coverage |
|---|---|
| `--chat` / default / `--response` / `--thinking` (+ `--text`) | real — 9 iterations with user prompt, thinking, text responses |
| `--todo` | real — a 5-item `[TASK_LIST_BEGIN]…[TASK_LIST_END]` task list |
| `--memory` | real — 3 calls (memory_search ×1, memory_feedback ×2) with full input + result |
| `--filter write_task_area` | real — 5 write_task_area commits |
| `--grep` | real — "seine" matches memory results + final answer |
| `--request` / `--request --json` | real — verbatim first request from the paired raw log |
| `--json` | byte-exact passthrough vs the fixture file itself |

Fixture GAPS (views with no real events in this session — golden-tested only
through their empty state, NEVER fabricated): the tool families
`--search`/`--web`/`--job`/`--vision`/`--text2img`/`--git` (no such tool calls
logged) and `--context-trim` (no context_trim activity). A fixture covering
those views needs a real session that exercised them — out of scope of this
fixture (see devdoc/log-content-golden.md).

## Provenance sha256 (import time, 2026-09-05)

| file | sha256 |
|---|---|
| original session JSONL | `bf08cb7ca89c280b9f2febc5d333693671a32e566bc068a0bb710ea39604ac8e` |
| original raw log (476 KB, 9 rounds) | `40f038aece0834b6d0d1f262f244a878965bfbc68e47d4fb495c35e89a912e8e` |
| fixture session JSONL | `56b537b26678cc90894f1a176e12b779153d957fd87e067c28e2022cac7e80fa` |
| fixture raw log (5 rounds) | `fd6848c701c9cddfb44ef26690d2f890e5c0b634f47cbd5f34e597871be9eb7f` |
