<!-- doc-class: historical -->
# Task Report: code_search self-heals a corrupt ctags index

## Bug (2026-09-10)

`code_search` died **permanently** on a corrupt index. `.gopi-tags` was
2,584,800 bytes of pure NUL (`0x00`) — a crash/disk-full during a ctags write —
and every later call failed until a human deleted the artifact by hand.

## Root cause — TWO independent failure paths (both fixed)

The epic named one symptom. Probing the real binary/toolchain in this session
showed the corrupt index can kill `code_search` **two** different ways, and a
fix for only one leaves the tool dead:

1. **`ensureTags` (generation path).** An index that is not a tag file makes
   ctags refuse: `"<path>" doesn't look like a tag file; I refuse to overwrite
   it.` Depending on the index mtime vs. the tree's newest source, the stale
   path either never runs ctags (the poison file is then *searched*, path 2) or
   runs it and is refused. Reproduced A/B: with a NUL index present and the fix
   stashed, `cmd/agent TestCodeSearchBackendNoSymbol` fails; with the file
   deleted (or the index mtime backdated) the very same test passes.
   *Aside:* the refusal at the *exact* byte count is version/state dependent —
   on Universal Ctags 5.9.0 an explicit `ctags ... -f <nul-file> .` sometimes
   overwrote it and exit 0'd. That is precisely why the fix does **not** depend
   on ctags cooperating: it never hands ctags an unusable target.
2. **`queryDefinition` (query path).** An index that *keeps a valid header* but
   has a torn body (oversized line / NUL run past the header / I/O failure) is
   not caught by a header check at all: the line scanner fails with
   `code_search: tags line exceeds 1048576 bytes` on **every** call. The
   pre-existing staleness logic only regenerated when the index was *older*
   than the sources, so a corrupt-but-new index was re-read forever.

## Completed

`internal/tools/symbol.go`:

- **Consts (Rule 1a/1b/13):** `tagsIndexFileName` (`.gopi-tags` — the one
  definition, now referenced instead of the bare literal at its old site),
  `tagFileHeaderPrefix`, `tagHeaderProbeBytes` (256), `tagsSelfHealMaxRetries`
  (1), plus a `tagsSelfHealCount` atomic for observability and assertions.
- **`tagsIndexValid`** — cheap structural probe: the file is non-empty and
  opens with the ctags header; rejects NUL fill, foreign files, truncation.
  Reads only `tagHeaderProbeBytes`: a NUL-filled 2.5 MB artifact fails on byte 0
  in one read.
- **`ensureTags` treats an invalid index as a CACHE MISS** — the probe runs
  *before* any staleness work (skipping a whole tree walk for a file about to
  be replaced), then `removeInvalidTagsIndex` + regenerate.
- **`queryDefinition` self-heals** a header-valid but unsearchable index: it
  drops the artifact, rebuilds once, and retries the lookup — bounded to one
  rebuild, never a loop.
- **`runCtags`** extracted, plus a last-resort retry when ctags itself refuses
  (`tagsIndexInvalidErr`, matched on the refusal text): a damaged artifact costs
  one retry. Bounded — the second failure is surfaced untouched (Rule 15).

## Not Done

- **Path 2's trigger is synthetic.** No real-world artifact in this session
  reproduced "valid header + oversized body"; that fixture is constructed to
  pin the code path (`tagMaxLineBytes` = 1 MiB = the scanner limit). The code
  path is genuinely reachable (disk-full mid-write after the header lands), but
  I did not observe it in the wild. Stated plainly rather than claimed.

- No `.gitignore` change: `.gopi-tags` was already gitignored.

## Verification

Every claim below is an executed command.

| Check | Command | Result |
|---|---|---|
| Build | `go build ./...` | OK |
| Epic repro, fix present | `go test ./cmd/agent/ -run TestCodeSearchBackend` | PASS (0.02s) |
| Epic repro, fix **stashed** (A/B control) | same, `go clean -testcache` | **FAIL** `code_search: tags line exceeds 1048576 bytes` |
| New regression tests | `go test ./internal/tools/ -run 'TestCodeSearchSelfHeals\|TestTagsIndexValid\|TestEnsureTagsSelfHeals\|TestEnsureTagsHealsCtagsRefusal\|TestEnsureTagsCtagsRefusalRetryIsBounded\|TestQueryDefinitionSelfHealsUnsearchableIndex'` | 6/6 PASS |
| RED proof — path 1 | retry branch neutralized → refusal tests | **FAIL** (as designed) |
| RED proof — path 2 | query-path heal neutralized → oversized-line test | **FAIL** `tags line exceeds 1048576 bytes` |
| Full suites, **with 2.58 MB NUL index planted in the repo root** | `go test ./internal/tools/ ./cmd/agent/ -count=1` | ok 236.4s / ok 186.6s |
| E2E, real binary | `./bin/gopi code-search tagsIndexValid --root .e2e-probe` over a NUL index | hit returned; index repaired 2,584,800 → 19,134 B, header `!_TAG_FILE_FORM` |

Two traps found while verifying (both would have produced a false "pass"):

- A stale `go test` **cache entry** made the A/B control pass once; `go clean
  -testcache` exposed the real failure. Never trust a bare `ok` from a
  cache-warm package when A/B-testing a fix.
- The first version of the path-2 fixture put NULs **inside** the 256-byte
  probe window, so path 1 caught it and the test passed for the wrong reason.
  The fixture now places corruption beyond the probe window.

## Regression tests (new)

- `internal/tools/corrupt_index_test.go` — NUL-filled 2 MiB / truncated header
  / foreign text / empty index all yield definition hits, exactly one self-heal,
  a repaired file on disk, and reuse (not re-heal) on the second call.
- `internal/tools/ctags_refusal_test.go` — a fake `ctags` shim reproduces the
  epic's literal refusal, asserting **2** invocations (refusal + one retry) and
  a bounded stop when refusing forever; plus the header-valid-but-unsearchable
  index. Zero-dependency: the shim is a POSIX shell script, no new imports.

## Deliverable location

- **Branch:** `fix/code-search-corrupt-index` (worktree
  `~/workspace/gopi-tags-selfheal`, base `2207a4d`)
- **Recover:** `git show fix/code-search-corrupt-index:devdoc/code-search-corrupt-index-selfheal.md`
- Commit sits in the shared object store — a worktree needs no push.
