<!-- doc-class: historical -->
# code_search / keyword_search on a host with no search backend — the in-process scanner leg

**Task**: `01a12d8e-feda-7748-9298-d03013f753b1` (branch `task/01a12d8e-feda-7748-9298-d03013f753b1`)
**Filed from**: the peerbox-side bug row that captured three defects measured on
`hosta` (the host had **neither** `ripgrep` nor `universal-ctags` installed;
it has both since 2026-10-12 08:51, which is why every test below controls PATH).

## The three defects, verbatim from the filed evidence

| # | symptom |
|---|---|
| ① | neither tool had a fallback that is not itself an external binary, so a host with no package had **no** search at all |
| ② | `codeSearchImpl`'s clean-miss hop called `keywordSearchImpl` **unconditionally**, i.e. it required `rg` — installing only `ctags` could not repair `code_search` |
| ③ | because of ②, the error prefix flipped surfaces: `code_search:` reported `keyword_search: rg: …` |

Red proof against the **pre-fix binary** (`artifacts/fallback-probe/red-proof-master.txt`):

```
### A: PATH=<empty dir> — code_search (symbol exists in internal/tools)
gopi tool code-search: code_search: ctags: exec: "ctags": executable file not found in $PATH:
exit=1
### B: PATH=<empty dir> — keyword_search
gopi keyword_search: keyword_search: rg: exec: "rg": executable file not found in $PATH:
exit=1
### C: PATH=<ctags shim only> — code_search (defect ②: the prefix flips to keyword_search)
gopi tool code-search: keyword_search: rg: exec: "rg": executable file not found in $PATH:
exit=1
```

## The fix

Two files under `internal/tools/` (Rule 28: this is the system layer, peerbox's half).

### `search_fallback.go` (new) — the zero-binary scanner

One in-process walk of `scope.RootPath`, **standard library only** (Rule 35: no
`doublestar`, no new dependency). Two entry points:

- `scanTextHits(root, scope, query)` — keyword_search's leg: one `TextHit` per
  matching line, shaped by the **same** `buildSnippet` the ripgrep leg uses, so the
  two render byte-identically. An invalid regex is searched **literally** instead of
  failing the tool.
- `scanSymbolHits(root, scope, symbol)` — code_search's last-resort leg:
  definition-shaped lines first (kind `function`/`type`/`variable`), occurrences
  second (kind `reference`) — the two-answer contract the ctags-miss hop always had.

**Glob translation** (`globToRegexp`): a scope glob becomes an anchored regexp where
`**/` is "zero or more directories" (so `**/*.go` matches a **root-level** `.go`
file — the property `path.Match` cannot express), `*`/`?` never cross a separator, and
everything else is quoted. A file is tested against its full relative path **and** its
base name (ripgrep's own glob semantics); a directory matches only a `…/**` glob's
prune form, which is how `vendor/**` skips the whole subtree without descending it.
A glob that will not compile is a named error, never a silently dropped filter.

**Parity with ripgrep's defaults, and the two deliberate divergences.** Hidden
entries skipped, `.git` never descended, symlinks never followed, NUL marks binary and
stops that file, the scope's cap bounds memory (while still counting the total, so
"N more matches" stays truthful). Divergences, stated: **ignore files are not
honored** (`.gitignore` semantics are a dependency's worth of work and this is the
last resort), and an **unreadable file is a loud error naming the path** (rg exits 2
on the same input — Rule 15, never a silent gap in coverage).

**Definition vs. occurrence** is a *declared-name* test, not a keyword test:
`definitionSignature` finds the definition keyword and returns the name it declares,
and the hit only counts as a definition when that name **is** the symbol
(identifier-bounded). Without it `func caller() { _ = GadgetTarget }` would be handed
back as a *definition* of `GadgetTarget` — a false positive the caller cannot see.

### `search.go` (modified) — the leg chooser

- `searchBackendAvailable(name)` = `exec.LookPath`, so a host without the package
  never spawns a doomed child.
- `codeSearchImpl`: a ctags error that is `exec.ErrNotFound` runs `scanSymbolHits`
  instead of propagating (defect ②); on a clean ctags **miss** the hop is
  `referenceHitsViaRipgrep` when rg is startable and the scanner when it is not.
- `keywordSearchImpl` is now the **argument-checking entry point and the chooser**;
  the ripgrep body moved to `keywordSearchViaRipgrep`, which returns a typed
  `backendMissingError` when the binary cannot start — that is the one condition the
  scanner repairs. Every other rg verdict (a bad regex, an unreadable tree) is
  delivered as-is: the scanner is not a retry for the backend's own answer.
- Prefixes (defect ③): the scanner's failures are re-wrapped by the caller, so a
  code_search run reports `code_search: …` and a keyword_search run reports
  `keyword_search: …`. The other tool's name can no longer appear on either surface —
  pinned by `TestSearchFallbackErrorsCarryTheCallingSurface`.

## Why the red proof is a two-shape argument

The in-process test file calls `keywordSearchViaRipgrep`, a symbol that does not
exist on the pre-fix tree, so *reverting `search.go` alone* makes the test package
**fail to build** rather than fail an assertion — not a useful red proof. The
observable pre-fix state is therefore the **binary**: `bin-master` (built from the
unmodified tree) vs `bin-fixed`, both driven with a controlled PATH. That is the
epic's own acceptance shape (`PATH=<empty dir> gopi tool code-search …`), and the red
proof above is that run on the pre-fix binary.

## Verification summary

Full report: the task's own report file (`task-report.md`, same session directory).

- `go build ./...` OK; `go vet ./internal/tools/` OK.
- `make test-budget PKGS='./internal/tools'`: read per test out of the run JSON — the
  eight new tests pass; the pre-existing host-dependent reds are unchanged from the
  base control (they are the same host-package/build-cache reds recorded in
  `devdoc/` before this task: no `sqlite3`-backed build-cache sandbox on this host).
- Real CLI, controlled PATH (`artifacts/fallback-probe/green-proof-fixed.txt`):
  `code-search` and `keyword_search` both exit **0** with hits at `PATH=<empty dir>`,
  the ctags-shim-only `code-search` returns the fixture's **reference** hits instead
  of the `keyword_search: rg:` error, and a genuine miss still prints
  `no symbol found: …` with **exit 1**.

## Not done / left deliberately

- **Ignore-file support** (`.gitignore`, `.ignore`) is not implemented in the scanner
  — see the divergence note above. On a host with a backend installed the scanner is
  never reached, so this only affects the no-backend case.
- The scanner is **not** wired into `ensureTags`/`queryDefinition`: `ctags` remains
  the only producer of the symbol index, and `code_search` still calls it first. On a
  host with ctags that keeps the definition-quality results; on a host without ctags
  the scanner answers directly, so no index is built at all.
- `internal/agent/` and `cmd/agent/` were **not touched**: the tool descriptions
  already promise "auto-falling back to ripgrep", which the fix makes true on every
  host rather than only those with the package installed.
