<!-- doc-class: historical -->
# Task Report: CONFIG.md — the centralized, source-verified configuration reference

**Task**: `01a0c048-7a0b-737c-9c38-c005b79a8fb4` — "gopi config settings is not
fully documented; the PA is confused about how the functionality works"
**Branch**: `docs/config-md` (worktree `~/workspace/gopi-config-doc`)
**Base**: `e8ee5156`

## Completed

- **`CONFIG.md` at the repo root** — the single reference for gopi's whole
  configuration surface, written from the source rather than from help text:
  - §1 the four channels (flag → env → settings → default), the `SetEnvVars`
    guard that makes a bare `gopi <prompt>` NOT clobber the env, the truthy-only
    boolean vocabulary, the integer-unit rule for durations, and the three knobs
    that rank their own channels (`tmpdir`, `scratch_gc_stale_min`, `notify.*`).
  - §2 the settings file (which path resolution wins, `GOPI_HOME`, unknown keys
    tolerated) plus **per-key tables** for endpoint, thinking, runtime,
    compaction, logging, tools, voice, temp workspace, scratch sweep, ledger
    topology, grace periods, A/B switches, the `repos` table and the `notify`
    section — each row with semantics, default, and the source line that reads
    it.
  - §3 the CLI flags, including the full-env-name spelling escape hatch
    (`--gopi-timeout-sec` ≡ `--timeout`), the flags whose name differs from
    their settings key, and the two flags hoisted before subcommand dispatch.
  - §4 configuration **by process form** — why one knob can look configured but
    ignored (the group verbs return before the agent config block).
  - §5 ten traps, each one observed in the code: zero-value-not-an-override,
    settings never clobber a shell export, `GOPI_HIDE_THINKING=0` does not hide
    anything, per-knob fail-soft vs hard-exit on a malformed settings file,
    legacy aliases (`GOPI_TASK_STALE_SEC`, `GOPI_HIPPO_DB`).
  - §6 pointers to the devdocs that own the deeper stories.

- **`internal/standards/config_doc_gate_test.go`** — the anti-rot gate for the
  document, because a doc that lags the code is a confident wrong answer (and
  the brief's own premise was that the old doc drifted). Three predicates, each
  a pure function with **three red proofs** plus an end-to-end poison check:
  - `TestConfigDocEnvNamesExist` — no env name in the doc is invented;
  - `TestConfigDocCarriesEveryCatalogEntry` — every `internal/config.EnvVars`
    row appears in the doc;
  - `TestConfigDocLineRefsResolve` — every `file.go:NNN` anchor resolves, with a
    read-floor so stripped anchors fail rather than pass vacuously.
  - `TestConfigDocRootAllowlisted` — the doc's home at the repo root is the
    reviewed allowlist entry (the sibling half of the root parking-lot rule).

- **Root parking-lot integration** — `CONFIG.md` added to
  `internal/standards/scratch_gate.go`'s `rootDocAllowlist`, its positive
  fixture, and `.gitignore`'s `!/CONFIG.md` negation. Without this the doc is
  untrackable: `/*.md` is ignored at the root and the junk gate fails a tracked
  root markdown not on the allowlist.

- **`GOPI.md` gained a pointer** at the top of the rule file naming `CONFIG.md`
  as the configuration reference and the gate that keeps it honest.

- **Two pre-existing master failures identified and reported** (not caused by
  this branch): `TestDocManifestCoversEveryDevdocFile` (3 devdoc files have no
  `DOC-INDEX.tsv` row) and `TestDocAnchorsResolveInNormativeDocs` (243 permitted
  drift entries; "6 new, 10 stale permits" on a clean master checkout too).

## Not Done (honest)

- **The doc narrative IS classified** — `devdoc/config-doc-and-its-anti-rot-gate.md`
  has its `DOC-INDEX.tsv` row (`normative`, spot-check
  `anchor internal/standards/config_doc_gate_test.go:70 at func TestConfigDocEnvNamesExist`),
  verified by the anchor gate's own count moving 441→442 normative docs with the
  drift count **unchanged** ("6 new, 10 stale permits", identical to clean
  master) — so this branch adds no drift.
- **The two pre-existing master failures were NOT fixed here** — they are outside
  this task's scope (the manifest repair is a corpus-wide classification call),
  and `task_submit` refused on this node (`role=client` with no authoritative
  `sync_to`), so the bug could not be filed through the queue. The reproduction,
  output and proposed fix are in `## Evidence` below and are dispatchable
  verbatim. The three unclassified files are confirmed to be
  `cli-as-eyes-and-hands-feasibility.md`, `go-toolchain-env-both-nodes-20260920.md`
  and `handoff-20260920-evening.md` — none of them this task's.
- **Durations in §1b are documented from the settings model, not re-measured** —
  `TimeoutSec int` / `ScratchGCStaleMin int` are read, not executed; the probe
  that confirmed flag parsing (see Evidence) covers the flag path only.

## Verification

- Build: `go build ./...` → **OK** (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` also OK; `bin/gopi --help-env` renders the 54-row catalog).
- Tests (branch-scoped, honest about the one pre-existing failure):
  - `go test ./internal/standards/ -run 'TestConfigDoc|TestRepoRootJunk'` → **ok**
    (the new `TestConfigDoc*` suite + the four existing `TestRepoRootJunk*` gates).
  - `go test ./internal/config/` → **ok**.
  - `go test ./cmd/agent/ -run 'TestTopLevelUsageDelegatesEnvVarsToHelpEnv|TestThinkingBudgetHelpMatchesCode'` → **ok**.
  - `go test ./internal/standards/` (whole package) → **1 FAIL**,
    `TestDocManifestCoversEveryDevdocFile`, **byte-identical on clean master
    `e8ee5156`** — the failure set is unchanged by this branch (see Evidence 5).
  - `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs` →
    **1 FAIL**, also identical on clean master ("6 new, 10 stale permits").
- Commit: `422ec569` + `1c010c5b` on `docs/config-md`.

## Evidence (self-filed)

**1. The gate is green on the delivered doc, and RED on a poisoned one — the
same code path both ways.** Poisoned the real `CONFIG.md` three times and ran
the gate, each time restoring from a backup:

```
--- poison 1 (drop a catalogued env name: GOPI_HEARTBEAT_STALE → GOPI_HEARTBEAT_XSTALE)
--- FAIL: TestConfigDocCarriesEveryCatalogEntry (0.02s)
    CONFIG.md does not carry 1 catalogued env var(s):
          GOPI_HEARTBEAT_STALE

--- poison 2 (invent an env name: GOPI_DEBUG → GOPI_TOTALLY_MADE_UP)
--- FAIL: TestConfigDocEnvNamesExist (0.79s)
    CONFIG.md documents 1 env name(s) that exist nowhere in the tree:
          GOPI_TOTALLY_MADE_UP

--- poison 3 (stale anchor: settings.go:133 → settings.go:99999)
--- FAIL: TestConfigDocLineRefsResolve (0.02s)
    CONFIG.md carries 1 unresolvable line reference(s):
          internal/config/settings.go:99999 (file has 508 lines)

--- restored
ok  	github.com/nick/gopi/internal/standards	3.672s
```

**2. The gate found two real gaps in the first draft of the doc**, which is why
it exists — before the fixes: `CONFIG.md documents GOPI_TASK_NOTIFY_` (a
wildcard I had typed as if it were a name) and `does not carry 18 catalogued env
var(s)` (`GOPI_WORKER_*`, `GOPI_SYNC_*`, `GOPI_WEB_*`, `GOPI_NUDGE_*`,
`GOPI_BASH_LOCALE`, `GOPI_CODE_SEARCH_ROOT`, `GOPI_SHOT_CHROME`,
`GOPI_1214_DUMP_DIR`, `GOPI_ERROR_DUMP_DIR`, `GOPI_ANCHOR_GUARD`,
`GOPI_HIPPO_DB`). Both were fixed by reading each knob's reader and documenting
it, not by weakening the check.

**3. Every line reference was mechanically resolved against the tree** —
a script printed the referenced line's text for all 60+ anchors and I corrected
the ~30 that pointed at a comment, a blank line, or the wrong symbol
(e.g. `loop.go:2583` was the struct-literal site, not the read).

**4. The flag/env/settings mechanics were PROBED, not inferred** — a temporary
in-package test called `config.FlagOverrides` for 11 argv shapes:

```
[--gopi-timeout-sec 5] -> setEnv=map[GOPI_TIMEOUT_SEC:true] timeout=5
[--model m]            -> setEnv=map[ANTHROPIC_MODEL:true DEEPSEEK_MODEL:true]
[--scratch-gc 30]      -> setEnv=map[GOPI_TEST_SCRATCH_GC_MIN:true]
[--scratch-gc=false]   -> setEnv=map[GOPI_TEST_SCRATCH_GC_MIN:true]
[--no-such-flag]       -> error: unknown flag --no-such-flag
[--debug off]          -> setEnv=map[GOPI_DEBUG:true] debug=false
```
This is the direct observation behind §1's `SetEnvVars` claim and the §3 table.
The probe file was deleted before commit (`ls internal/config/ | grep zz` →
"probe removed"; `git status --porcelain` is empty of it).

**5. The two pre-existing master failures reproduce on a CLEAN master checkout**
— run in `/home/user/work/GoPi` (untouched, `git log -1` = `e8ee5156`), not in
this worktree:

```
$ go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile
--- FAIL: TestDocManifestCoversEveryDevdocFile (0.17s)
    docclass_gate_test.go:104: devdoc/cli-as-eyes-and-hands-feasibility.md: no row in devdoc/DOC-INDEX.tsv
    docclass_gate_test.go:104: devdoc/go-toolchain-env-both-nodes-20260920.md: no row in devdoc/DOC-INDEX.tsv
    docclass_gate_test.go:104: devdoc/handoff-20260920-evening.md: no row in devdoc/DOC-INDEX.tsv
    docclass_gate_test.go:130: 2109 rows cover 2112 files under devdoc/

$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs
--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.44s)
    docanchor_test.go:214: doc anchors verified: 2110 across 441 normative docs
        (243 pre-existing drift entries permitted, 6 new, 10 stale permits)
```
`task_submit` to file the first as a `bug` task was **REFUSED** (Rule 26 note:
the duplicate-dispatch allowance applies to repair, so the reproduction above is
the artifact to dispatch):
`role=client (a mirror leg) but no authoritative peer is configured (sync_to /
GOPI_SYNC_TO)` — so it is recorded here with a verbatim reproduction and a
proposed fix (append three rows to `devdoc/DOC-INDEX.tsv` following the
manifest header's class vocabulary and each file's own banner) for dispatch.
