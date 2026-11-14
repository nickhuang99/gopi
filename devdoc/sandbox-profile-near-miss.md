<!-- doc-class: historical -->
# The near-miss `GOPI_HOME=<sandbox>/.gopi` — a silent second profile, refused

Task `01a0dd8f-c014-7989-9d81-53620de4697f`. Measured 2026-09-26.

> **Follow-up, measured 2026-10-13 (task `01a135da-debd-786d-b384-c0a0751d5923`).** The
> predicate below was keyed on the value's **PARENT** — a sibling sandbox marker, or a
> second profile leaf — and the arithmetic it refuses sits one level ABOVE that window.
> `GOPI_HOME=/home/user/.gopi` (parent `/home/user`, no marker anywhere) therefore walked
> straight through the guard and reproduced the same defect on the operator's OWN profile
> root: `/home/user/.gopi/.gopi`, 2 ledger rows against the real 1376, and
> `gopi task submit` printing a normal `Task queued: <id>` with exit 0. The predicate is now
> the **LEAF alone** (`filepath.Base(value) == config.ProfileDirName`), under any parent, and
> the correction is derived unconditionally as the parent. Everything below still holds —
> the two spellings are one predicate, and `§ The predicate` is superseded accordingly. See
> `§ Second measured family` at the end for the change, its red proof and its controls.

## The defect

`GOPI_HOME` is a **HOME**, not a data root: the documented arithmetic is

```
<gopi data root> = <GOPI_HOME>/<profile leaf>      (config.ProfileDirIn, leaf = ".gopi")
```

`gopi sandbox -h` prints `<sandbox>/.gopi` as *the sandbox's profile*. An operator
(or an agent) who reads that and exports

```sh
GOPI_HOME="$SB/.gopi"      # the spelling the help itself names
```

therefore gets `$SB/.gopi/.gopi` — a **second, nested profile**:

- it grows its own `memory.db` and `sessions/`;
- it has **no `.sandbox-owner.json`**, so `scratch.SweepSandboxes` — which sweeps a
  sandbox ROOT's *children* — never sees it, at any age;
- the sandbox's own ledger and `sessions/` stay **empty**;
- **nothing is printed**. For every reader (`gopi log --last`, `gopi checkpoint
  list`, `gopi memory store`) a nested profile looks exactly like an empty one.

The 2026-09-26 audit found the residue by hand: **4 stray `[auto]` ledger rows and
4 session directories** inside `sandbox-*/.gopi/.gopi/`.

Red proof on the pre-fix binary (this branch's `HEAD` built in a detached
worktree), real CLI:

```
$ GOPI_HOME=$SB/.gopi gopi memory store --topic t --question q --answer a
Memory stored (id=1).                                  # exit 0, no warning
$ find $SB -maxdepth 3 -name memory.db
$SB/.gopi/memory.db          # the sandbox's own ledger — untouched by that write
$SB/.gopi/.gopi/memory.db    # ← the nested profile the run actually wrote
```

A read-only verb does not show it (`gopi log --last` on the pre-fix binary merely
reports `open …/.gopi/.gopi/sessions: no such file or directory`); the tree appears
where a run first **writes** — the ledger's first open. That asymmetry is why the
regression probe is a writer verb.

## The decision: refuse, with a named opt-out

Two options were on the table (the epic left the choice open): warn once and use the
sandbox's own profile, or refuse.

**Chosen: refuse**, exit status 2, naming the correction — with
`GOPI_ALLOW_NESTED_PROFILE=1` as the operator's deliberate opt-out, which turns the
refusal into a printed warning.

Why refuse rather than re-point:

- Re-pointing is **guessing the operator's intent** at a path-arithmetic layer that
  cannot tell `$SB/.gopi` (the near miss) from `~/.gopi` (a legitimate own profile)
  without a marker to ask about. Silently rewriting the value that a run's ledger,
  session dir and settings path all resolve against is the same class of surprise
  the guard exists to remove. **The 2026-10-13 follow-up made this argument expire in
  one respect, and the resolution is worth recording**: the guard now refuses
  `~/.gopi` too, so the two values ARE distinguished — by the leaf, which is the only
  distinction that survives measurement — and neither is guessed at. What changed is
  that `~/.gopi` is no longer treated as “a legitimate own profile”: it is the same
  one-level-deep arithmetic, and it is refused with its parent as the correction.
- A refusal is what makes the near miss **loud at the moment it is typed**, which is
  the only moment the correction is free. Nothing has been materialised at the check
  point, so the refusal is provably free of the directory it forbids (the acceptance
  measure is `test -d <sandbox>/.gopi/.gopi` before/after).
- The opt-out keeps the policy from being a straitjacket: an operator who genuinely
  wants `<sandbox>/.gopi/.gopi` says so once, and the degradation is **announced**,
  never silent (Rule 15/25 — the honest lever, not a hidden one).

### The predicate, and what it deliberately does NOT touch

> **Superseded 2026-10-13** — see `§ Second measured family` below. The table as
> written recorded the PARENT-keyed predicate; the row reading “`~/.gopi` (no sibling
> marker) | no” is the defect that was then measured on a real host.

`config.LookupSandboxProfileMisuse` refuses exactly two path shapes:

| value | refused? | why |
|---|---|---|
| `<sandbox>/.gopi` (sibling carries `.sandbox-owner.json`) | **yes** | names a sandbox's profile → resolves one level deeper |
| `<x>/.gopi/.gopi` (any `x`) | **yes** | a profile inside a profile; no home is spelled that way |
| `<sandbox>` (the canonical spelling) | no | it **is** the home; `gopi sandbox run` exports it |
| `~/.gopi` (no sibling marker) | no | an ordinary own profile nested under another — a different situation |
| any other home | no | — |

The sandbox is identified by **asking for the ownership marker** the ONE
initializer wrote (`config.SandboxOwnerFileName`), never by a path shape — Rule 34's
"never assert a path is a sandbox". A directory the operator made by hand is never
refused for being called something.

The first draft of this check used *existence* as the marker test (a directory named
`…/.gopi` with anything in it, plus a directory named `.gopi` at all). That version
**refused the canonical `GOPI_HOME=$SB`** — measured, exit 2 on the correct value —
which is why both the narrowing and its pinning test exist:
`TestSandboxCanonicalGopiHomeIsNotRefused` and
`TestGopiDirLeavesLegitimateValuesAlone`.

## Enforcement points (one predicate, two doors)

1. `cmd/agent/main.go` — `refuseNestedSandboxProfile()`, called right after
   `BootstrapEnvFromArgs` and **before the subcommand chain**, so the group verbs
   (`worker`, `merger`, `task run`) are covered too, and nothing is materialised.
2. `internal/config.GopiDir()` — returns the sentinel
   (`config.GopiHomeMisuseErrorPrefix`) for any caller that reaches the library
   directly. Consumers generally turn an error into "cannot resolve the profile",
   which is exactly right: the alternative was a *silent* second profile.

`config.GopiHomeMisuseMessage` is the one wording both doors use, so a refusal and a
sentinel cannot describe the same problem differently.

## Files

- `internal/config/gopihome.go` — the predicate, the message, the opt-out, the
  `GopiDir` sentinel.
- `internal/config/sandbox.go` — `SandboxOwnerFileName` documented as the identity
  test.
- `internal/config/settings.go`, `CONFIG.md` — the catalog row + the `gopi_home`
  row's new sentence (and the opt-out's own row).
- `cmd/agent/cli_sandbox_profile_guard.go` — the CLI refusal (new file).
- `cmd/agent/main.go` — the call site.
- `cmd/agent/cli_sandbox.go` — the `NEAR MISS` paragraph in `gopi sandbox -h`.
- `cmd/agent/cli_sandbox_ledger_test.go` — `cliSandboxRunEnv`, the binary/env seam.
- `cmd/agent/cli_sandbox_profile_nearmiss_test.go`,
  `internal/config/gopihome_nearmiss_test.go` — the pins.

## Verification

- Red proof: `GOPI_TEST_BIN=<pre-fix build> go test ./cmd/agent -run
  TestSandboxProfileNearMissIsRefusedAndCreatesNothing` → **FAIL** ("…`.gopi/.gopi`
  was CREATED by a run launched with `GOPI_HOME=…/.gopi`", output `Memory stored
  (id=1).`). Fixed binary → PASS.
- `go test ./internal/config ./internal/sandbox` → ok.
- `go test -run TestConfigDoc ./internal/standards/` → ok (`GOPI_ALLOW_NESTED_PROFILE`
  resolves through its const block, and the `gopi_home` row's line reference was
  re-pointed in this commit).
- Real CLI, canonical spelling unaffected: `GOPI_HOME=$SB gopi shoot -h` → exit 0.

## Second measured family — the leaf, not the parent (2026-10-13, task `01a135da-debd-786d-b384-c0a0751d5923`)

### What was measured

The guard above asked its questions about the value's **PARENT**. The arithmetic it exists to
stop sits one level ABOVE that window, so the shape with no sandbox in it — an ORDINARY home's
own profile root — was exempted by design and reproduced the identical defect on a real host:

```
GOPI_HOME=/home/user/.gopi        # parent /home/user: clean, no .sandbox-owner.json
  → resolves to /home/user/.gopi/.gopi
  → its memory.db held 2 rows while the real ledger held 1376
  → `gopi task submit` printed a normal `Task queued: <id>`, exit 0
```

Silent failure is the defect (Rule 15): a row went into a ledger that nothing on the host
sweeps, dispatches from, or reads back, and the operator's own `Task queued:` line said it had
succeeded. The sandbox arm was never the load-bearing one on this host — the ordinary-home arm
was.

### The change

`LookupSandboxProfileMisuse` now returns on the **LEAF alone**:

```go
clean := filepath.Clean(home)
if filepath.Base(clean) != ProfileDirName {
    return "", "", false
}
return ProfileDirIn(clean), filepath.Dir(clean), true
```

— under ANY parent, and the canonical value is derived unconditionally as `filepath.Dir(value)`.
`sandboxOwnershipMarkerPresent` moved OUT of the predicate and into `GopiHomeMisuseMessage`,
where it selects only which measured CONSEQUENCE clause the sentence prints (a profile nested
inside a sandbox vs. the `$HOME/.gopi/.gopi` shape a plain typo produces). Asserting the sandbox
clause for a value with no sandbox in it would be an invented symptom — Rule 41: inference never
stands in as a criterion. `GopiHomeMisuseErrorPrefix` widened from "it names a sandbox's profile,
not a home" to "it names a profile, not a home", because the sentence must describe both families.

The three legitimate spellings are unchanged and now pinned by name (Rule 31: the test drives the
real binary): the canonical `GOPI_HOME=<sandbox>` that `gopi tool sandbox run` exports, an
ordinary home, and a home whose leaf merely CONTAINS the profile word (`<home>/.gopi-backup` — the
test is the whole leaf, never a prefix).

### Red proof (pre-fix binary, real verb)

The pre-fix binary is this branch's `HEAD` (`git archive HEAD` into a scratch tree, built there —
never a borrow of another run's worktree):

```
$ GOPI_TEST_BIN=/home/user/workspace/_tmp/nm-probe/gopi-prefix \
    go test -count=1 -run TestSandboxNearMissOrdinaryHomeRootIsRefusedAndCreatesNothing ./cmd/agent/
--- FAIL: TestSandboxNearMissOrdinaryHomeRootIsRefusedAndCreatesNothing (0.66s)
    cli_sandbox_profile_nearmiss_test.go:218: /dev/shm/…/001/.gopi/.gopi was CREATED by a run
      launched with GOPI_HOME=/dev/shm/…/001/.gopi — an ordinary home's profile root is the same
      near miss
        --- output ---
        Memory stored (id=1).
```

Same probe, fixed binary → PASS.

### The two stale scripts this found

Two tracked scripts exported the refused spelling and were relying on it:

- `scripts/replay/chat_replay_xtest.sh` — `export GOPI_HOME="$SB/home/.gopi"`, i.e. the replay's
  private profile was one level too deep: the run read an EMPTY profile while the profile a reader
  resolves stayed untouched. Now `$SB/home`.
- `scripts/ui/chat-live-proof.sh` and `scripts/ui/chat-ui-observe.sh` — the same spelling, with
  `GOPI_SETTINGS` pointing at `<run>/home/.gopi/settings.json`, so the settings file and the
  profile the run resolved disagreed by one level. Now `$run/home` / `$home`.

They are not merely updated: they are EVIDENCE that the near miss was reachable in this
repository's own tooling, not only by hand. (Found by grepping the tree for the spelling the
guard refuses — Rule 41's shape, applied to the guard's own blast radius.)

