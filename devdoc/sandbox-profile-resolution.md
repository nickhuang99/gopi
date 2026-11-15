<!-- doc-class: normative -->
# Sandbox / PROFILE-RESOLUTION divergence — one truth: `<sandbox>/.gopi`

Branch: `task/01a0dcfa98187a30b3613e4584f6ee71`
Task: `01a0dcfa-9818-7a30-b361-3e4584f6ee71`

## What was wrong (measured, 2026-09-26)

`gopi sandbox create` materialised the private directories **beside** the sandbox
directory, while every reader resolves them **one level deeper**. Verbatim, with
the binary built from the pre-fix tree (`CGO_ENABLED=0 go build -o /tmp/gopi-repro-bin ./cmd/agent/`):

```
$ export GOPI_SANDBOX_DIR=/home/user/workspace/gopi-probe-sandboxes
$ /tmp/gopi-repro-bin sandbox create --label redprobe
sandbox …/sandbox-redprobe-911cb9f1 created=true reused=false root=… source=from GOPI_SANDBOX_DIR owner=pid=3015558
/home/user/workspace/gopi-probe-sandboxes/sandbox-redprobe-911cb9f1

$ ls -la $SB
drwx------ logs
drwx------ raw
drwx------ sessions
lrwxrwxrwx settings.json -> /home/user/.gopi/settings.json
-rw-r--r--  .sandbox-owner.json

$ GOPI_HOME=$SB /tmp/gopi-repro-bin log --last
Error listing sessions: open /home/user/workspace/gopi-probe-sandboxes/sandbox-redprobe-911cb9f1/.gopi/sessions: no such file or directory
```

`ls` showed a `sessions/` directory; the reader looked in `.gopi/sessions`. Two
answers to "where does a gopi profile live" — the creator's and the readers'.

**Root cause (5-why).** The reader resolves through `config.GopiDir()`, which
knows that **`GOPI_HOME` is a HOME, not a data root**: the profile is
`<GOPI_HOME>/.gopi` (`internal/config/gopihome.go`, `ProfileDirName = ".gopi"`).
The initializer (`internal/sandbox.Ensure`) never asked that function — it
`os.MkdirAll`'d the names it holds next to the directory it was given. Because
the two halves each spelled the join themselves, they could (and did) disagree;
the help text and PROJECT.md Rule 34 documented the creator's shape, so a user
following the docs got a path no reader would ever open. A **second** live
consequence: a real run inside the sandbox created `<SB>/.gopi/sessions` itself
and its sessions landed one directory deeper than documented.

## The reading that won (and why)

**The readers'/run path's reading**: the sandbox's profile root is
`<sandbox>/.gopi`. Reasons, in order:

1. `GOPI_HOME`'s `<root>/.gopi` convention is load-bearing machine-wide (session
   dirs, ledger, `gopi_home` hygiene, `--home`, worker enumeration, the peer
   sync). Moving it would touch far more than the sandbox, and it is the shape
   that is *protected* — the sandbox is the newcomer.
2. Without `.gopi`, the two halves must each spell the join, which is exactly how
   this defect was born. Aligning the creator onto the resolve function gives the
   join **one definition** (Rule 14) that both sides reference.
3. A sandbox is *a private GOPI_HOME*: making the created tree literally the tree
   `GOPI_HOME=<sandbox>` resolves means no reader needs a special case, and the
   documented invocation works with no preparation.

## The change

| file | what changed |
|---|---|
| `internal/config/gopihome.go` | NEW `config.ProfileDirIn(home)` and `config.ProfilePathIn(home, component)` — THE profile join, exported so a creator and a reader reference one definition. `GopiDir()` and `gopiDirFromEnv()` route through it (no behaviour change for them: same value, one spelling). |
| `internal/config/sandbox.go` | the shape constants now say where the entries live: they are the **profile's** entries (`SandboxSessionsDirName`/`Raw`/`Logs`), documented as `<sandbox>/.gopi/…` and joined by the readers' function. The now-redundant `SandboxProfileDirName` alias was dropped (the join IS `config.ProfileDirIn`). |
| `internal/sandbox/sandbox.go` | `Ensure` materialises `config.ProfileDirIn(dir)/{sessions,raw,logs}` and links `settings.json` + `memory.db` **inside that profile root**; the package doc's tree is redrawn with `.gopi/` as the level the readers resolve. |
| `internal/sandbox/run.go` | `SettingsFileNameIn(dir)` now returns `<dir>/.gopi/settings.json` — the path `GOPI_SETTINGS` must name for the child's settings read to land in the sandbox (previously it named `<dir>/settings.json`, i.e. a file the child's own resolution would only find for the legacy spelling). |
| `cmd/agent/cli_sandbox.go` | `gopi sandbox -h` draws the real tree (`.gopi/` level + why it exists) — the text a user follows. |
| `GOPI.md` (Rule 34) | the shape paragraph names the profile root and quotes the failing invocation as the reason; the recipe no longer needs the hand-written `GOPI_SETTINGS` (the profile root decides) and adds `GOPI_HOME="$SB" gopi log --last` as the proof line. |
| `devdoc/rules-reference.md` (Tier B of Rule 34) | the shape table moved under `.gopi/`, with the pre-fix divergence recorded. |
| `CONFIG.md` | the `gopi_home` and `sandbox_dir` rows say a sandbox IS a `GOPI_HOME` whose profile is `<sandbox>/.gopi`, materialised by the initializer; line references re-pointed at the post-change lines (`config_doc_gate_test` green). |

**Not changed on purpose** (and why): `internal/scratch/sandbox_gc.go`'s
predicate (name marker / own mtime / owner pid+start ticks) is shape-agnostic —
it looks at the sandbox directory and its marker, never at the profile inside, so
no reclaimer edit was needed. The ownership marker stays **beside** the profile
root (`<sandbox>/.sandbox-owner.json`), which is where the reclaimer reads it.

## The regression test (red → green, one test)

- `cmd/agent/sandbox_profile_resolution_test.go` —
  `TestSandboxCreateProfileRootIsWhatReadersResolve`: creates a sandbox with the
  **real binary** (`gopi sandbox create --label …`, root via `GOPI_SANDBOX_DIR`
  under the suite scratch tree), then asserts (a) `config.ProfileDirIn(sb)` — the
  reader's own resolution — exists and holds the three profile dirs, (b)
  `GOPI_HOME=$SB gopi log --last` does not print `Error listing sessions` and
  names the writer-side directory. It lives in `cmd/agent` **because the defect
  was in the shipped invocation**, not in a function contract: a unit test of the
  initializer was green on the broken tree.
- `internal/sandbox/sandbox_test.go` —
  `TestSandboxProfileRootIsWhatTheReadersResolve` is the in-package twin: it sets
  `GOPI_HOME` to the created sandbox and asserts `config.GopiDir()`,
  `config.ProfilePathIn(...)` and `SettingsFileNameIn(...)` all land on paths the
  initializer materialised. Existing shape tests were re-pointed at the profile
  level (they asserted the old, wrong shape).

**Both halves of the red proof, same test, two binaries** (Rule 31):

```
$ GOPI_TEST_BIN=/tmp/gopi-repro-bin  go test ./cmd/agent/ -run TestSandboxCreateProfileRootIsWhatReadersResolve -count=1
--- FAIL: TestSandboxCreateProfileRootIsWhatReadersResolve (0.05s)
    sandbox_profile_resolution_test.go:98: the reader-resolved profile root
      …/sandbox-resolvetest-09acadba/.gopi does not exist after `gopi sandbox create`
      (stat …: no such file or directory) — the created shape and the readers disagree
FAIL
$ GOPI_TEST_BIN=/tmp/gopi-fixed-bin  go test ./cmd/agent/ -run TestSandboxCreateProfileRootIsWhatReadersResolve -count=1
ok  github.com/the operator/gopi/cmd/agent  2.529s
```

## Acceptance run (verbatim — the documented invocation, red before / green after)

Fix applied, binary rebuilt (`CGO_ENABLED=0 go build -o /tmp/gopi-fixed-bin ./cmd/agent/`):

```
$ SBROOT=/home/user/workspace/gopi-probe-sandboxes/green-$$
$ export GOPI_SANDBOX_DIR=$SBROOT
$ SB=$(/tmp/gopi-fixed-bin sandbox create --label green | tail -1)
$ echo $SB
/home/user/workspace/gopi-probe-sandboxes/green-3047541/sandbox-green-b45a15b5

$ ls -A $SB                       # top level
.gopi
.sandbox-owner.json
$ ls -A $SB/.gopi                 # the profile root — exactly what the reader resolves
logs
raw
sessions
settings.json

$ GOPI_BASE_URL=http://127.0.0.1:9 timeout 90 \
    /tmp/gopi-fixed-bin sandbox run --label green -- /tmp/gopi-fixed-bin "say hi"
… (a real LLM session ran inside the sandbox; dead endpoint ⇒ no network credit spent)
**Task list:** - `t1` — Emit greeting "Hi!" — **completed** (review verdict A: ON-TRACK)

$ GOPI_HOME=$SB /tmp/gopi-fixed-bin log --last
═══════════════════════════════════════════════════════════════
Session: 656c0fef-0282-4667-ac91-828b8b37468f
Model:   deepseek-flash
Started: 2026-09-26 17:47:37
Workdir: /home/user/workspace/gopi-probe-sandboxes/green-3047541/sandbox-green-b45a15b5
═══════════════════════════════════════════════════════════════
TASK:
$ ls $SB/.gopi/sessions
656c0fef-0282-4667-ac91-828b8b37468f
```

Before the fix the same three commands printed
`Error listing sessions: open <SB>/.gopi/sessions: no such file or directory`
(`## What was wrong` above).

## Files

- `internal/config/gopihome.go`, `internal/config/sandbox.go`
- `internal/sandbox/sandbox.go`, `internal/sandbox/run.go`, `internal/sandbox/sandbox_test.go`
- `cmd/agent/cli_sandbox.go`, `cmd/agent/sandbox_profile_resolution_test.go` (new)
- `GOPI.md`, `devdoc/rules-reference.md`, `CONFIG.md`
- `devdoc/sandbox-profile-resolution.md` (this doc), `tasks/<task-id>-report.md`

## Acceptance re-run at close-out (verbatim)

The epic's acceptance is a REAL run, and at the branch's tip it was re-executed
with a binary built from that tip (`CGO_ENABLED=0 go build -o /tmp/gopi-accept
./cmd/agent/` → `BUILD OK`): fresh sandbox under `$GOPI_SANDBOX_DIR` → live
`gopi` run inside it → `GOPI_HOME=$SB /tmp/gopi-accept log --last` printed
`Session: 1e65f8e7-8345-408a-9a0a-83bcfcffcca4` (exit 0), and that id IS the
directory the run created under `$SB/.gopi/sessions`. The full transcript —
created tree (`find $SB -maxdepth 2`), the run's output, the reader's output and
its exit status, `ls -la $SB/.gopi` — is in the task report,
`tasks/01a0dcfa98187a30b3613e4584f6ee71-report.md`, section *Acceptance re-run at
close-out*. Re-read at that tip against the created tree (not against the
source): `/tmp/gopi-accept sandbox -h` draws `.gopi/` as the PROFILE root with
`sessions/ raw/ logs/` under it and states the failure it prevents.

## Not Done (honest)

- **The read-only-ledger sub-finding is NOT fixed here.** A sandbox's `memory.db`
  is a symlink to the shared read-only *sample* (a Rule 34 decision), while the
  run path needs a **writable** ledger: measured, a real run inside a sandbox
  **replaced the symlink with a fresh empty ledger** and left the sample
  untouched, because the ledger is opened from `GOPI_MEMORY_DB`'s fallback
  (`config.LedgerPath()`) and sqlite re-creates a file it cannot open. A profile
  whose `memory.db` is literally read-only therefore *reads empty* and the run's
  `memory_store` is refused (`attempt to write a readonly database (8)`) — with
  the sample still intact on disk. This is a separate defect (the sandbox's
  ledger is neither the sample nor a writable per-sandbox ledger); it is
  **dispatchable** and needs its own task, not a widened scope here.
- The 27 `scripts/*.sh` call sites that set `GOPI_HOME` by hand (Rule 34's
  documented anti-pattern) remain unmigrated — carried over from the
  `agent-sandbox-unify` report, unchanged by this branch.
- `devdoc/evidence/ui-state/*.sh` read `$SB/.gopi/sessions` and are therefore
  **correct** under the aligned shape (they encoded the reader's truth); no edit
  needed, noted for the reader's sake.
