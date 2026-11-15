<!-- doc-class: roadmap -->
# Task Report: Go → Rust port — reframe + ordered roadmap

## Verdict
`task(action=reframe)` returned **TOO_BIG**. This epic is a greenfield rewrite
(133,216 LOC Go, 15 packages, 1,913 test functions) plus a 30G live-data
migration surface — it cannot be one unit of work. The harness enqueued one
child task per roadmap item (13 children, ids `01a088f9-f6b2…` … `01a088fa-0154…`).

## Verified inventory (this session, HEAD 91201ba)
- 394 `.go` files (248 of them `*_test.go`), 1,913 `func Test…` functions, 15 packages.
- LOC: internal/tools 54,011 · cmd/agent 26,675 · internal/agent 25,316 ·
  internal/gopilog 9,057 · internal/deepseek 4,850 · internal/config 4,545 ·
  internal/proxy 2,155 · internal/text2img 1,291 · internal/testutil 1,083 ·
  internal/shot 1,014 · internal/pricing 688 · internal/checkpoint 668 ·
  internal/toolnames 88.
- Zero Rust: `find -name '*.rs' -o -name Cargo.toml` → 0 hits. Pure greenfield.
- Toolchain present: cargo 1.98.0. CLI: 95 `case "…"` verbs in `cmd/agent`.
- 26 tool-name constants in `internal/toolnames/names.go` (wire-name source of truth).
- Live state: `~/.gopi` = 30G, 3,926 session dirs, `memory.db` 48,488,448 bytes
  (plus audit copy), `distilled.db` / `tasks.db` / `gopi.db` present (0-byte in this checkout).
- Golden fixtures: only 1 `testdata/` dir, 0 `*.golden` files → the differential
  corpus (child 1) largely does not exist yet and must be BUILT before any porting.

## Why TOO_BIG (not IMPOSSIBLE)
Nothing here is unattainable — it is 133k LOC of mechanical-but-large porting with a
hard oracle (the Go binary itself). The blocker is *scale + independence*: 5 subsystems
(memory engine, task ledger, agent loop, CLI, servers) each large enough to be its own
task, plus a 30G read-back audit and a cutover plan. Roadmap, not verdict.

## Ordered roadmap (children, dependency order)
1. Differential golden-corpus harness (Go oracle → frozen fixtures). **Blocks 5/6/10.**
2. Cargo workspace + musl static binary proof + Rule 12 `repos.<path>.build` declaration.
3. Constants + config + logging crates (bottom of the dep graph).
4. deepseek wire protocol crate (Rule 15a omitempty/required-field semantics).
5. SQLite memory engine (FTS5 + trigram, identical result set AND rank order).
6. text2img crate (SFNT/TrueType, IoU threshold vs Go renderer).
7. Tool layer part 1 — task ledger + verify/merge + Rule 9 closeout.
8. Tool layer part 2 — remaining tools, 16/17 identical wire names.
9. Agent loop crate.
10. CLI surface crate (golden matrix, byte-identical stdout/stderr/exit).
11. Chat + web servers + proxy/shot/pricing/checkpoint.
12. 30G persisted-state migration + read-back audit.
13. Cutover / rollback / retirement plan.

## Not Done (honest)
- No Rust code written in this session — that is the children's work, not the reframe's.
- Children 1–13 not started; they are queued `pending` in the ledger.

## Verification
- Build: n/a (no code change; documentation-only commit).
- Inventory commands: `find -name '*.go' | wc -l` → 394; `grep -rh '^func Test' | wc -l` → 1913;
  `find -name '*.rs' | wc -l` → 0; `cargo --version` → 1.98.0; `du -sh ~/.gopi` → 30G.
- Ledger: `gopi task list` → 13 children `pending` + 1 epic `in_progress`.
