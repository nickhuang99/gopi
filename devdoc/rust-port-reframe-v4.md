<!-- doc-class: historical -->
# Task Report: Go → Rust port — reframe v4 (independent re-verification + feasibility probes)

## Deliverable (one line)
The 13-child roadmap is confirmed enqueued and self-contained; this pass adds the
**feasibility preflight** the roadmap depended on (static binary, SQLite parity,
crates.io reachability) and three **corrected findings** that change what children 2/5/8
must do.

## What this session verified (beyond v1/v2)

| Claim | Status | Evidence (run in this session) |
|---|---|---|
| 13-child roadmap exists in ledger | **confirmed** | `gopi task list` → 13 rows `pending`, `parent_task_id 01a08911-…`, tags `reframe-roadmap`, created 2026-09-10 10:09 |
| Children self-contained (goal + acceptance) | **confirmed** | `gopi task show <id>` prints goal + standalone acceptance for child 1 and child 8 |
| Static single binary is achievable | **proven by probe** | `RUSTFLAGS="-C target-feature=+crt-static" cargo build` → `static-pie linked`, `ldd` → *statically linked* |
| Go oracle is a static binary | **confirmed** | `file bin/gopi` → `ELF 64-bit LSB executable … statically linked` |
| FTS5 + trigram available to the port | **confirmed in Rust** (bundled SQLite 3.53.2, static, 60s build) | system `sqlite3 3.37.2` runs `fts5(x)` and `tokenize='trigram'`; crates.io index reachable (`cargo add rusqlite --features bundled` resolved 0.40.2) |
| memory.db schema | **confirmed** | `distilled_qa_fts` = fts5 over 13 cols w/ `content='distilled_qa'`; `distilled_qa_tri` = `tokenize='trigram'`; 1532 rows in both. **Ranking is Go-side**: SQL is `ORDER BY fts.rank` then Go **re-sorts** by penalty/confidence with `rank` as tiebreak (`memory.go:1172-1211`) |

### Findings that CHANGE the children (corrections to the roadmap)

1. **The live tool count is 16 — the epic's "16/17" hedge resolves to 16, and child 8's
   briefing is the one stale artifact.**
   `claudeToolDefs` (`internal/agent/loop.go:802`) contains exactly **16** `Name:` entries:
   read, git_blame, write, edit, bash, code_search, keyword_search, web_search, web_fetch,
   memory, context_trim, task_submit, job, task, vision_analyze, text2img.
   `toolnames.ToolTaskEvaluate` is **retired** (`names.go:64`); `GOPI.md:87` already states
   "GoPi has 16 tools" (a repo-wide grep for "17 tools"/"16/17" in `*.go`/`*.md` returns
   **nothing** — the 17 figure appears only in this epic's own text), and
   `TestGOPIRule3_MatchesClaudeToolDefs` (`internal/agent/gopidoc_sync_test.go:54`) binds
   GOPI.md's list to `claudeToolDefs`, so 16 is the enforced contract.
   → **Child 8's briefing still names `task_evaluate` among the tools to port** (its
   summary says 16/17). It must port **16**; implementing a retired tool is a parity
   *failure*, not a feature. No doc fix is needed (GOPI.md is already correct).

2. **`jieba_token` is not jieba — it is Go's own CJK 2-gram segmentation.**
   `cjkSegmentation` (`internal/tools/memory.go:1434-1460`) emits comma-space-joined
   **2-char CJK grams**, written into a live FTS5 index column. 1339 of 1532 rows carry a
   non-empty value (e.g. row 9: `取不, 不到, 酿造, 代码, …`).
   → **Child 5 (memory) must replicate the gram generator byte-for-byte**, not "use a
   jieba crate" — the grams are *persisted* and are revisited by every later search. This
   is also the top risk for child 12's read-back audit.

3. **The static-binary gate must not require the musl target.**
   `rustup target add x86_64-unknown-linux-musl` **fails on this box** (downloads
   `rust-std`, then the installed-target list still shows only `-gnu`; tried twice).
   The workable, already-proven route is `-C target-feature=+crt-static` **on the gnu
   target** (glibc, statically linked, ldd-clean — matches the Go oracle's own linkage).
   → Child 2's acceptance should be **"`ldd` says statically linked"**, not
   "musl target" — otherwise it chases a toolchain the machine cannot install.

## Not Done (honest)
- **No Rust code, no golden corpus** — that is children 1–13's work; a reframing session
  must not start its own children.
- The `jieba_token` gram-for-gram comparison against a Rust reimplementation is
  **not** performed here (no Rust exists); it is named as a child-5 acceptance risk.
- The `task_evaluate` correction could **not** be written into child 8's ledger briefing:
  `gopi task update` accepts only `--status/--reason/--result` (no briefing edit). It is
  recorded here and must be read by child 8's session.
- 30G migration still unproven (unchanged from v2).
- **trigram** was exercised only via the system `sqlite3` CLI, not yet through the Rust
  bundled engine (FTS5 was); child 5 must re-test `tokenize='trigram'` in Rust on day one.
- Every child still has to re-measure its own subsystem; these probes are a preflight, not
  a substitute for child-level acceptance.

## Operational hazard hit during this session (worth knowing)
The v3 worktree was **deleted while in use**: a concurrent task on branch
`fix/worktree-live-cwd-guard` (worktree `gopi-wtcleanup`, held `locked`) removed
`/home/user/workspace/gopi-rust-reframe-v3` mid-session — after `git worktree add` succeeded, the
directory was left holding only the untracked `probe/` + `devdoc/` (no `.git`), so
`git status` failed with `fatal: not a git repository`. The report was recovered by
creating a fresh worktree (`docs/rust-reframe-v4`) on a new branch — note the v3 **branch
name `docs/rust-reframe-v3` still exists with zero commits**, since the wipe destroyed the
worktree before anything was committed. Lesson: commit early in a worktree; a worktree is
not durable against a concurrent cleanup task.

## Verification
- Build: n/a (documentation-only; no Go/Rust sources touched in this worktree).
- Probes: plain static probe `cargo build` exit 0 → `static-pie linked`, ldd *statically linked*;
  **rusqlite-bundled static probe: exit 0 in 60.5s**, binary prints `fts5_rows=1 sqlite=3.53.2`
  (bundled SQLite 3.53.2 with working FTS5) and `ldd` → *statically linked* — the two hardest
  child-2 gate clauses are proven together in one binary;
  `cargo add rusqlite --features bundled` exit 0; `rustup target add …musl` → target NOT
  installed; `sqlite3` FTS5 + trigram queries returned rows; `file bin/gopi` → statically linked.
- Ledger: `gopi task list` → 14 rows (1 epic `in_progress` + 13 children `pending`).
- Worktree: `/home/user/workspace/gopi-rust-rf-v4`, branch `docs/rust-reframe-v4`,
  base `3ce88c2` (= HEAD; same base as the v2 reframe).

## Deliverable declaration (Rule 6d)
1. **Branch**: `docs/rust-reframe-v3` (worktree `/home/user/workspace/gopi-rust-reframe-v3`).
2. **Recovery**: `git show docs/rust-reframe-v4:devdoc/rust-port-reframe-v4.md`
3. **Location**: commit lives in the shared main-repo object store (worktree shares refs —
   no push needed). The authoritative roadmap + child briefings live in the **task ledger**:
   `gopi task list`, `gopi task show 01a08913-c576-7b28-928b-b9c2a0aa04ff`.
