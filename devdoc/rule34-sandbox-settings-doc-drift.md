<!-- doc-class: historical -->
# Rule 34 doc drift: the sandbox settings entry is a 0444 COPY, not a symlink

**Date:** 2026-10-15 · **Task:** `01a14a53-d954-7117-889a-2be9bec0b845` · **Baseline:** `2c56271`

## What was reported, and what was actually true

A bug row was filed on a measured fact: `gopi tool sandbox create` leaves
`<sandbox>/.gopi/settings.json` as a **regular `0444` file**, while GOPI.md Rule 34
described it as *"a **symlink** to the REAL settings file, read-only by contract"*.
The row's second half claimed the documented `rename(2)` trap "cannot be reproduced".

**The machinery is correct.** `internal/sandbox/settings_doc.go` replaced the link
with a read-only COPY deliberately, after the 2026-09-27 incident: a sandbox
fixture command of the form

```bash
printf '{"tmpdir":"%s/tmp"}' "$SB" > "$SB/.gopi/settings.json"
```

**followed** the link and truncated the operator's live `~/.gopi/settings.json`
(12 keys lost — the `api_key`, the `repos` table `gopi merge` keys on, the notify
sink). A "read-only" symlink is unenforceable: the permission bits of a symlink
belong to the **target**, which is the file the shape exists to protect.

So the defect was **doc drift**, and the fix is the doc — never a restoration of the
link.

## The second half of the row, resolved

The `rename(2)` sentence was TRUE but attached to the wrong entry. `rename(2)`
replaces the *symlink node*; a copy has no node to replace. The trap still applies to
the entries that remain links or write targets:

- `plugins` — still a symlink to the operator's real plugin tree;
- `memory.db` — a real, private, writable file, where a writer's
  `rename` into place once replaced a link to the shared sample.

The corrected Rule 34 states the trap **on `plugins` and the ledger**, where it is
still true, and states the settings document as the copy it is.

## What changed

| file | change |
|---|---|
| `GOPI.md` (Rule 34 shape paragraph) | `settings.json` = "a read-only COPY (mode `0444`) … NEVER a link", with the 2026-09-27 reason and the repair path (`Create`/`Ensure`); the `rename(2)` trap moved onto `plugins`/ledger |
| `internal/config/sandbox.go` | `SandboxSettingsFileName` doc comment: COPY, not symlink; the incident replaces the stale `.flash_settings.json` note |
| `internal/sandbox/run.go` | `SettingsFileNameIn` / `ProfileEnv` comments: the child reads the copy |
| `internal/cliverbs/cliverbs.go`, `internal/sandbox/plugins.go`, `internal/sandbox/ledger.go` | the same phrase drift in prose |
| `CONFIG.md` | the two `sandbox_dir` / `GOPI_SANDBOX_LEDGER_SEED` rows: one symlink left (`plugins`), the settings entry is a `0444` copy |
| `internal/standards/sandbox_settings_doc_gate.go` | **new gate** (registration + predicate) |
| `internal/standards/sandbox_settings_doc_gate_test.go` | red proof + real-tree half |
| `internal/standards/gate_test.go` | the gate joins `gates()` |
| `CONFIG.md` (`sandbox_dir` read-at) | re-pointed `internal/config/sandbox.go:212` → `:207`: the declaration moved when the `SandboxSettingsFileName` comment above it was rewritten, and the name-carrying reference gate caught it |

## The gate: what it pins, and what it deliberately does not

`TestSandboxSettingsDocGateRedProof` feeds the predicate the sentence that actually
shipped wrong (verbatim from baseline `2c56271`, wrapped as the file wrapped it) and
requires the report. Its arms:

- **RED** — "`` `settings.json` `` — a **symlink**" (the shipped sentence), the same
  claim unemphasised, a copy assertion **without** the `0444` mode, and the same pair
  inside a production Go comment.
- **GREEN** — the shipped shape; the REFUSAL form ("never a link") and the
  HISTORICAL form ("used to be a symlink") — a record must be able to print its own
  subject; a prose line that merely *mentions* the entry; and a `plugins` symlink
  sentence.

Scope: the corpus is `GOPI.md`, `CONFIG.md`, the production `.go` comments and the
non-history markdown; `devdoc/`, `briefings/`, `tasks/`, `reports/`, `docs/` and
`_test.go` are excluded so history can quote the pre-fix shape. The file set has a
read-floor (100 files) and the rule file's own sandbox paragraph must carry the mode
token **and** the refusal — a rule file that stopped stating the shape at all would
otherwise pass by saying nothing.

The inline escape `settings-doc-ok: <reason>` exists for a line whose SUBJECT is the
refused shape; it stays visible in the gate's data report.

## Red proof (observed, not asserted)

1. **Pre-fix text restored** into `GOPI.md` (the shipped sentence put back verbatim),
   `go test ./internal/standards/ -run TestStandardGates`:

```
Rule 34 (a sandbox's settings entry is documented as the shipped 0444 copy, never a link): 1 total hits, 0 baselined, 0 escaped, 1 new (unannotated), 0 baseline entries
Rule 34 …: NEW violation (checklist 34/sandbox-shape) at GOPI.md:921  [the sandbox settings entry is documented as a LINK — …]
--- FAIL
```

2. **Restored** from the frozen snapshot; `git hash-object GOPI.md` =
   `0f1588accec34cb4b17a48f945060e6eff81bf6f` (the HEAD blob, which is still the
   pre-fix text `b49e441d…`, differs by construction — the working tree holds the
   fix). Content identity checked against the snapshot (`sha256 391def47…`), and the
   gate run went green.

## The run (real surface, Rule 31)

```
$ export GOPI_SETTINGS=$S/fixture/settings.json GOPI_SANDBOX_DIR=$S/sbxroot
$ gopi tool sandbox create --label ruledoc
sandbox …/sbxroot/sandbox-ruledoc-28b3a00e created=true reused=false root=… source=from GOPI_SANDBOX_DIR ledger=…/.gopi/memory.db owner=pid=2529449 plugins=…/.gopi/plugins(absent-not-linked)

$ stat -c '%n type=%F mode=%a' $SB/.gopi/settings.json
…/.gopi/settings.json type=regular file mode=444

$ printf '{"tmpdir":"/evil"}\n' > $SB/.gopi/settings.json
bash: …/settings.json: Permission denied      (exit 1 — the COPY refuses, the source is intact)
$ cmp $S/fixture/settings.json $SB/.gopi/settings.json && echo SOURCE_INTACT
SOURCE_INTACT
```

That is the whole point of the shape, observed on the shipped binary: the redirect
that destroyed the operator's document on 2026-09-27 now lands on a read-only copy
and is refused by the kernel.
