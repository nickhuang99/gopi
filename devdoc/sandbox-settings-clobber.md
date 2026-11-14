<!-- doc-class: historical -->
# Task Report: the settings.json clobber — a profile that was a LINK to the operator's live document

Task `01a0dfdb-d9f2-7472-af20-523178d0e770`. Branch
`task/01a0dfdbd9f27472af20523178d0e770`.

## Completed

### 0. An absent settings document is NAMED, never swallowed

The copy READS its source, where the old link only recorded a name — so a host
whose settings document does not exist (a test profile, `GOPI_SETTINGS` pointed
at a deliberately absent path, an operator who never wrote one) would have failed
at creation. It does not: absence returns the source it looked for with an empty
path, and the creation line renders it exactly as the plugin tree's own case:

```
sandbox <path> created=true … owner=… settings=<source>(absent-not-linked) plugins=<tree>(absent-not-linked)
```

A materialised copy prints nothing (the ordinary line is unchanged). Any OTHER
failure — an unreadable document, a self-referential source — stays a loud error.

### 1. The writer, with file:line and the command that did it

Two writers, one mechanism, and the mechanism is the defect:

- **`internal/sandbox/sandbox.go:273` (pre-change)** —
  `ensureSymlink(filepath.Join(profileRoot, config.SandboxSettingsFileName), target, "the real settings file")`.
  `gopi sandbox create` therefore made `<sandbox>/.gopi/settings.json` a
  **SYMLINK to the operator's live `~/.gopi/settings.json`**, documented
  "READ-ONLY by contract" in the package doc, the `-h` text and the flag help.
  A contract is not an enforcement.
- **The writers were two session fixtures**, run through the bash tool inside
  sandboxes the initializer had just created (session `01a0df2d…`, evidence in
  `~/.gopi/sessions/`):
  - `03:48–03:52` — `printf '{"tmpdir":"%s/tmp"}' "$SB" > "$SB/.gopi/settings.json"`
  - `03:54:52` — `printf '{"tmpdir":"%s/tmp","repos":{"%s":{"build":""}}}' "$SB" "$FX" > "$SB/.gopi/settings.json"`
    with `FX=/home/user/workspace/gopi-test-scratch/e2merge-01a0df2d` — this is
    the 155-byte document the operator found, whose mtime is 03:54, and whose
    single `repos` entry is the scratch repo `gopi merge` then reported on.
- **The reach**: `>` is `O_TRUNC|O_CREAT`, and POSIX **follows symlinks** on
  open. Nothing in the command touches `~/.gopi/`; the path is inside the
  sandbox. The link is what carried the write out.

Living evidence of the shape, on this host, today:
`~/workspace/gopi-sandbox/sandbox-retire-9afae9fb/.gopi/settings.json -> /home/user/.gopi/settings.json`.

### 2. Why `RefuseOperatorProfileWrite` did not fire

Three independent reasons, each sufficient:

1. **Wrong process.** It is a TEST-process guard (`internal/testutil`, it calls
   `t.Fatalf`), so it exists in `.test` binaries. The writer was the **bash
   tool's shell inside the agent's own process** — no test binary was involved.
   A guard inside a test binary cannot see a fixture run by an agent.
2. **Wrong path.** It compares the path it is *given* against the operator's
   profile directory. The path handed to the shell was
   `<sandbox>/.gopi/settings.json`, which is not in the profile — the
   **resolution** of that path (through the symlink) is what landed in the
   profile, and no argument-level check can see a resolution.
3. **Wrong layer.** A contract that depends on nothing write-opening a path
   cannot be enforced by any check at the caller: writers are shell redirects,
   editors, `os.WriteFile`, sqlite. The only layer that can refuse is the one
   that owns the *file* — i.e. inode identity/permission. That is where the fix
   went.

### 3. Structural protection (not detection)

`internal/sandbox/settings_doc.go` — the profile's `settings.json` is **no
longer a link**; it is a **read-only COPY (0444) of the operator's document**,
built in a sibling temp file and renamed into place:

- the redirect now truncates **the copy** — a different inode — and is refused
  outright by the kernel at 0444, so the fixture fails loudly where the mistake
  was made instead of destroying a file outside the sandbox;
- the reader-side resolution (`<GOPI_HOME>/.gopi/settings.json`) is unchanged, so
  a sandbox still runs the operator's real configuration;
- the plugin **tree** keeps its link: a tree is never write-opened and a copy
  would drift the moment a script is deployed. Document is copied, tree is
  linked — opposite shapes for opposite reasons.
- a pre-fix **symlink** at the profile path is now **REFUSED by name** (path +
  target + repair) rather than silently unlinked: unlinking is itself a repair
  window. The in-place migration is explicit (`Options.ReplaceLegacySettingsLink`).

### 4. Loud, early failure when the merge table lacks the cwd repo

`cmd/agent/merge_cwd_repo.go` + one call at `merge_run.go:1358`, **before the
lock** and before anything is walked: if the directory the run was started in
resolves (via `git rev-parse --show-toplevel`) to a repository the `repos` table
does not hold, the run says so on stderr and prints the table:

```
gopi merge: WARNING: the repo this run was started in (/home/user/work/GoPi) is NOT in the settings.json "repos" table — this run will NOT act on it
gopi merge: the "repos" table holds 1 repo(s): /…/e2merge-01a0df2d
```

A **warning, not a refusal**: a one-repo table is legitimate and a merge run may
lawfully be started from a directory that is not a repo (silence is then the
correct answer — three tests pin that silence).

## Not Done

- Not done: no new config key, script, daemon or tool (Rule 35); the fix is in
  the code path that owns the file.
- Not done: the copy can go stale if the operator edits `settings.json` while a
  sandbox lives. Deliberate trade — `Create`/`Ensure` are the documented repair
  path, and irreversibly destroying the live document is the failure being
  bought out.
- Not done: `internal/tools/task_resume_ground.go` + `devdoc/worker-resume-ground.md`
  show in `git diff master` on this branch **before** this task's changes — they
  arrived on the branch tip (`d03becaa`, a merge commit) and are not this task's
  work.
- Known, pre-existing, NOT caused by this change: the operator's live
  `~/.gopi/settings.json` has **no `tmpdir` key**, which fails
  `TestTmpDirSettingsValueReachesTheSpawnedChild` in `cmd/agent`. Proven
  pre-existing: the same test, run in a detached worktree at the **untouched
  `master` tree** (`bbbff3b5`), fails with the identical message. That missing
  key is a live-config symptom of the same clobber (the fixture that landed at
  03:54 carried `tmpdir`, the restored document does not). It is a host-config
  repair, not a code change, and this run does not touch operator config
  (constraint).
- Two tests that the new shape invalidated were updated rather than deleted, and
  both now pin the NEW contract harder than before:
  `TestPluginTreeIsALinkReadOnlyAndIdempotent` (an `Ensure` in that fixture now
  names its settings source explicitly — the copy READS the source, where a link
  only recorded a name) and
  `TestSandboxPlainshellShapeIsExactlyOneRealLedger` (the walk now asserts the
  settings entry is a **regular 0444 file**, and that the plugin tree is the
  only symlink left).
- Delivery-point sync: **done**. An earlier revision of this line said the branch
  was not synced, which was true when it was written. It is not true now:
  `git merge-base --is-ancestor master HEAD` → exit 0, `master` = `de63c0f3`,
  merge commit `1bb3751c`.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `go test ./internal/sandbox/ -count=1` → `ok` (full package, includes
  the new `settings_doc_test.go`); `go test ./cmd/agent/ -count=1 -run
  'Sandbox|Merge|Settings|Profile|CwdRepo'` → OK; `go test ./internal/standards/
  -count=1` → `ok`.
- Ran (the real surface, verbatim): see `## Evidence` below — the sandbox
  create + the exact 2026-09-27 redirect + `gopi merge --fast`.
- Result: redirect REFUSED (`Permission denied`, rc=2 — the 2026-09-27 command was
  unquoted and `sh` prints the permission error on stderr while the redirect's
  own diagnostic gives the shell's conventional rc; the earlier `rc=1` in this
  report's first revision was typed from expectation, re-measured as `rc=2` in
  the paste-ready run below), operator document unchanged; `gopi merge --fast` from a repo absent from the table printed the
  WARNING and the table.

## Evidence (self-filed)

- RED (pre-change tree, `git worktree add --detach <dir> master`, `master` =
  `bbbff3b5`, red fixture `red_fixture_test.go` planted):
  `go test ./internal/sandbox/ -run TestRedProofProfileSettingsWriteReachesTheRealDocument -count=1 -v`
  →
  `RED CONFIRMED: writing <sandbox>/.gopi/settings.json rewrote the REAL document
  <root>/real-settings.json / before: {"model":"demo"} / after:
  {"tmpdir":"/tmp/whatever","repos":{"/elsewhere":{}}}` — `ok`, and the
  pre-change shape is named by grep: `sandbox.go:273 ensureSymlink(…,
  config.SandboxSettingsFileName, "the real settings file")`.
- GREEN (this branch, same fixture re-expressed as
  `internal/sandbox/settings_doc_test.go`):
  `go test ./internal/sandbox/ -count=1` → `ok`, with
  `TestProfileSettingsWriteDoesNotReachTheRealDocument` (operator document
  bit-for-bit unchanged; profile path is not a symlink),
  `TestProfileSettingsCopyIsMode0444` (truncating open REFUSED),
  `TestPreFixSymlinkAtTheProfilePathIsReplacedNotFollowed`,
  `TestPreFixSymlinkIsRefusedWithoutTheMigrationFlag`, and the positive control
  `TestOperatorDocumentIsNotAWriteTarget` (a direct write to a settings document
  still works — the guard did not become "nothing can write settings").
- RED/GREEN pair re-measured at close-out, on ONE settings file and ONE cwd, the
  two sides differing only by binary (pre-change built from `595bd8bd`):
  `printf '{"tmpdir":"/tmp/x","repos":{"/elsewhere":{}}}' > "$SB/.gopi/settings.json"`
  → pre-change: `<SB>/.gopi/settings.json -> …/real.json` (symlink), redirect
  `rc=0`, operator doc became `{"tmpdir":"/tmp/x","repos":{"/elsewhere":{}}}`;
  this branch: `-r--r--r--` regular file, redirect `rc=2`
  (`…/.gopi/settings.json: Permission denied`), operator doc unchanged
  (`{"api_key":"KEEP","model":"m","repos":{"/nowhere/else":{}}}`).
- Merge warning RED/GREEN re-measured at close-out, same cwd and same one-repo
  settings file: pre-change binary → **0** `WARNING` lines, the run reporting
  `no branch in …/scratchrepo needs a merge decision`; this branch → **1**
  `WARNING` line naming the cwd repo, then `the "repos" table holds 1 repo(s): …`.
- Merge warning RED, live (the pre-change binary, built from the `master` tree at
  `bbbff3b5`, same cwd/settings/args as the GREEN run above):
  `./bin/gopi-pre merge --fast` →
  ```
  gopi merge: holding the single-instance lock; 1 repo(s), ceiling 120000 estimated tokens / 25 conflicted files per branch
  gopi merge: --fast — deterministic-only run: …
  gopi merge: exported GOPI_MERGER_LOCK_OWNER=…:merge …
  gopi merge: hand-over budget 45 minute(s) published to the child's GOPI_TIMEOUT_SEC
  no branch in …/evidence/live3/scratchrepo2 needs a merge decision
  ```
  — the operator's exact 2026-09-27 output, five lines, **not one of them naming
  the cwd repo**, while reporting on a repo the operator is not standing in.
- Merge warning GREEN, live: same command with this branch's binary, same cwd,
  same settings file →
  ```
  gopi merge: WARNING: the repo this run was started in (/home/user/workspace/gopi-01a0dfdbd9f27472af20523178d0e770) is NOT in the settings.json "repos" table — this run will NOT act on it
  gopi merge: the "repos" table holds 1 repo(s): /…/evidence/live3/scratchrepo2
  ```
  followed by the run; rc=0.
- Merge warning, SILENCE control (must not cry wolf): same binary, table holding
  this checkout's own path → first line is
  `archived: /home/user/workspace/gopi-01a0dfdbd9f27472af20523178d0e770`, i.e.
  **no WARNING line**; and three unit tests cover the other lawful-silence cases
  (cwd IS the table entry, trailing-slash spelling, cwd not a git repo at all).
- Live structural proof: `gopi sandbox create` under a scratch root →
  `<sandbox>/.gopi/settings.json` is `-r--r--r-- 1 nick nick 105` (a regular
  0444 file, not `lrwxrwxrwx`); `printf '{"tmpdir":"/tmp/x","repos":{"/elsewhere":{}}}'
  > "$SB/.gopi/settings.json"` (the exact 2026-09-27 command) →
  `bash: …: Permission denied`, rc=2, and the source document still reads
  `{"api_key":"sk-REAL-KEY-MUST-SURVIVE","repos":{"/home/user/GoPi":{"build":"make"}},"notify":{"cmd":"x"}}`.
- gofmt: `gofmt -l <each file this branch touched>` → empty. go vet:
  `go vet ./internal/sandbox/ ./cmd/agent/` → clean.

## Paste-ready verify

```bash
cd ~/workspace/gopi-01a0dfdbd9f27472af20523178d0e770 && \
  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && \
  go test ./internal/sandbox/ -count=1 -run 'SettingsDoc|ProfileSettings|Prefix|PreFix|OperatorDocument|PluginTree' -v && \
  go test ./cmd/agent/ -count=1 -run 'CwdRepo' -v && \
  L=$(mktemp -d) && printf '{"api_key":"K","repos":{"/nowhere":{}}}\n' > "$L/real.json" && \
  GOPI_SANDBOX_DIR="$L/sbx" GOPI_HOME="$L/home" GOPI_SETTINGS="$L/real.json" ./bin/gopi sandbox create --label proof | tail -1 | \
  xargs -I{} sh -c 'ls -l {}/.gopi/settings.json; printf "{\"tmpdir\":\"/tmp/x\"}" > {}/.gopi/settings.json; echo "redirect rc=$?"'; \
  echo "operator doc still:"; cat "$L/real.json"
```
