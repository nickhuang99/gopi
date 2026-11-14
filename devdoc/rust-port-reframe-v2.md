<!-- doc-class: historical -->
# Task Report: Go → Rust port — epic reframe v2 (TOO_BIG + 13-child roadmap)

## Summary
The Rust-rewrite epic was reframed against a **re-verified** inventory at HEAD
`b684656`/worktree base `3ce88c2`. `task(action=reframe)` returned **TOO_BIG** and the
harness enqueued **13 child tasks** (`01a08913-c576…` … `01a08913-daed…`, tags
`reframe-roadmap`) — one per roadmap item, in dependency order. This run's deliverable is
the diligence: a corrected inventory, the TOO_BIG rationale, and the ordered
decomposition. **No Rust code was written here** — that is the children's work.

## Verified inventory (this session, worktree base 3ce88c2)

| Metric | Value | Command |
|---|---|---|
| Go files (all) | **398** | `find . -name '*.go' -not -path './.git/*' \| wc -l` |
| Test files | 248 | `find . -name '*_test.go' \| wc -l` |
| `func Test…` | **1923** | `grep -rh '^func Test' --include='*_test.go' . \| wc -l` |
| Non-test LOC | **65,716** | `find . -name '*.go' -not -name '*_test.go' -exec cat {} + \| wc -l` |
| Test LOC | **68,652** | `find . -name '*_test.go' -exec cat {} + \| wc -l` |
| Packages | **16** | `go list ./...` (15 internal/cmd + `testsuite`) |
| Rust source | **0** | `find . \( -name '*.rs' -o -name 'Cargo.toml' \) \| wc -l` → 0 |
| Cargo | 1.98.0 (2026-08-05) | `cargo --version` |
| Tool wire names | **26** constants in `internal/toolnames/names.go` | `grep -cE '^\s*Tool[A-Za-z]+\s+='` |
| CLI verbs | **76** unique `case "…"` in `cmd/agent/*.go`; 21 `isXxxSubcommand` predicates | `grep -rho 'case "[a-z0-9-]*"' … \| sort -u \| wc -l` |
| Live state | `~/.gopi` = **30G**, **4130** session dirs, `memory.db` **48,488,448 B** (+ an identical `memory-glm-audit-20260909-184030.db`), `distilled.db`/`tasks.db`/`gopi.db` 0 B | `du -sh ~/.gopi`, `ls -la ~/.gopi/*.db` |
| Frozen golden corpus | **testdata/ only, 0 `*.golden`** | `find . -name '*.golden'` → 0 |
| Devdocs / briefings | 455 / 42 | `ls devdoc \| wc -l` |
| Rule 12 build decl | `~/.gopi/settings.json` → `repos."/home/user/work/GoPi".build = CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, `build_timeout_sec = 600` | python3 json read |

Non-test LOC per package (top): `internal/tools` 27,079 · `cmd/agent` 12,873 ·
`internal/agent` 10,306 · `internal/gopilog` 5,069 · `internal/deepseek` 2,603 ·
`internal/config` 2,457 · `internal/proxy` 1,206 · `internal/testutil` 941 ·
`internal/text2img` 928 · `internal/shot` 692 · `internal/pricing` 432 ·
`internal/checkpoint` 397 · `internal/toolnames` 99 · `internal/taskverdict` 63 ·
`cmd/text2img-verify` 25.

### Corrections vs the v1 reframe (earlier same-day roadmap)
The v1 note said "394 files / 1913 tests / 133k LOC / 15 packages / 95 CLI verbs /
26 toolnames / 3926 sessions". Re-measured at the merged HEAD:
- files **398** (394 → 398), tests **1923** (not 1913);
- non-test LOC **65,716** and test LOC **68,652** (the "133k" figure was total lines
  including assertions/tables; both numbers are now stated separately);
- packages **16** not 15 (the extra one is `testsuite`);
- CLI verbs **76** unique `case` labels + 21 subcommand predicates (the "95" counted a
  broader pattern), so "~40 verbs" in the epic text is an undercount — the golden CLI
  matrix child must enumerate all 76;
- `internal/taskverdict` is a 15th internal package not listed in v1 — it must be in the
  crate graph.
- `task_evaluate` (`toolnames.ToolTaskEvaluate`) is **retired in code** (documented at
  `names.go:64`), so parity is over the 17 live tools, not 16/17.

## Why TOO_BIG (not IMPOSSIBLE)
Nothing is unattainable: it is ~134k LOC of mechanical-but-large porting with a **hard
oracle** (the Go binary). The blocker is scale × independence: five subsystems (memory
engine, task ledger, agent loop, CLI, servers) each big enough to be its own task, plus a
30G read-back audit and a cutover plan. Roadmap, not verdict. The prerequisite ordering is
strict: **no subsystem can be "ported" until observable behavior is frozen**, because
there is no golden corpus today (0 `*.golden`, 1 bare `testdata/`).

## Delegation — 13 children enqueued (dependency order)
| # | Child task id | Scope | Blocks |
|---|---|---|---|
| 1 | `01a08913-c576-7b28-928b-b9c2a0aa04ff` | Differential golden corpus + Go-oracle harness | 5, 6, 10 (all) |
| 2 | `01a08913-c700-7b6d-b0eb-c5a4a14d6054` | Cargo workspace + musl static binary + Rule 12 decl | 3–13 |
| 3 | `01a08913-c900-766c-a8a0-2f55e2838505` | constants/toolnames + config + gopilog crates | 4–11 |
| 4 | `01a08913-cb83-72b9-9690-97b6724883dd` | deepseek wire crate (Rule 15a) | 9 |
| 5 | `01a08913-cd17-7ea7-9856-0492b3957539` | SQLite memory engine (FTS5+trigram, rank order) | 7, 8 |
| 6 | `01a08913-ce4a-7816-8ab0-3edf48e141e0` | text2img crate (SFNT/TrueType, IoU parity) | 8, 11 |
| 7 | `01a08913-cfc3-7ee7-9d0b-4819c2909885` | Tool layer 1 — task ledger + verify/merge + Rule 9 | 9 |
| 8 | `01a08913-d0d7-7f16-acbd-b10b0dc7e8f2` | Tool layer 2 — remaining tools, 26 wire names | 9 |
| 9 | `01a08913-d317-7949-b785-da0ac65fd76a` | Agent loop / sessions / reframe / telemetry / prompts | 10, 11 |
| 10 | `01a08913-d56b-7c9d-8da0-89881b9eacee` | CLI surface (76 verbs, byte-identical matrix) | 13 |
| 11 | `01a08913-d82c-7a6e-b920-57c9e48e8216` | chat + web servers + proxy/shot/pricing/checkpoint | 13 |
| 12 | `01a08913-d9b3-7334-912c-551eea1b5a00` | 30G persisted-state migration + read-back audit | 13 |
| 13 | `01a08913-daed-7f13-9a59-21332f4bd9ae` | Cutover / rollback / retirement plan | — |

Each child carries a goal + a standalone acceptance criterion in its own briefing
(self-contained, per the reframe payload), so a fresh session can execute it without this
report.

## Not Done (honest)
- **No Rust code, no Cargo.toml, no golden corpus written in this session** — deliberately;
  the reframe's deliverable is the decomposition, and the harness forbids the reframing
  session from starting its own children.
- Children 1–13 are `pending` in the ledger; none claimed or executed here.
- The 30G audit (child 12) has **not** been sampled — its acceptance assumes a copy-based
  migration with count/hash read-back; the strategy is stated, not yet proven.
- `devdoc/rust-port-roadmap.md` (v1) is left in place for history; this file supersedes it
  where the two disagree on counts.

## Verification
- Build: n/a — documentation-only change, no Go/Rust source touched.
  (`go build` unaffected; worktree has no source edits.)
- Inventory: all commands in the table above were run in this session and their raw output
  is quoted; every count is reproducible with the listed command.
- Ledger: `gopi task list` before reframe → 1 task (this epic, in_progress). Reframe result
  → 13 children queued `pending`, source `llm`, tags `reframe-roadmap`.
- Worktree: `/home/user/workspace/gopi-rust-reframe-v2` on branch `docs/rust-reframe-v2`
  (created from `3ce88c2`).
- Commit: see the branch's tip (report + this file only).

## Deliverable declaration (Rule 6d)
1. **Branch**: `docs/rust-reframe-v2` (worktree `/home/user/workspace/gopi-rust-reframe-v2`).
2. **Recovery**: `git show docs/rust-reframe-v2:devdoc/rust-port-reframe-v2.md`
   (or `git log --oneline docs/rust-reframe-v2`).
3. **Location**: the commit lives in the shared main repo object store — the worktree
   shares refs, no push needed. The authoritative epic statement + 13 child briefings are
   in the **task ledger** (`gopi task list` / `gopi task show 01a08913-c576-7b28-928b-b9c2a0aa04ff`),
   not only in this file.
