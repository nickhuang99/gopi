<!-- doc-class: normative -->
# The scripts gate's per-root guard: a NAMED tolerance for roots this tree does not ship

`internal/testutil/scripts_gate_test.go` → `assertScopeCoversTheTrackedTree`.

## The failure it repairs

The per-root non-vacuity guard asks every root in `auditedScriptRoots` to
contribute at least one `.sh`/`.py` to the walk, and fails the root that
contributes none. That predicate is right — a root that scans nothing is a
misspelled directory or a scope that quietly stopped covering the files the
2026-10-07 widening extended it to.

But on THIS repository three roots can never satisfy it, and no walk can fix
that. The repository's history starts at the source snapshot `964b240`
("initial: source snapshot at 0592c9ae4, no history (docs/archive,
text2img-eval, tasks excluded)"):

| root | tracked `.sh`/`.py` | tracked files | on disk |
|---|---|---|---|
| `docs` | 0 | 0 | absent |
| `tasks` | 0 | 3 (markdown reports) | present |
| `text2img-eval` | 0 | 0 | absent |

Measured at `76e6faf` with `git ls-files <root> | grep -cE '\.(sh|py)$'` and
`git ls-tree --name-only HEAD | grep -cx <root>`; both spellings are quoted per
row in the tolerance table itself, so each reason is re-derivable rather than
asserted. The guard was red on every clone (clean master, peer, detached
worktree) — a gate nobody can reach green is not a gate.

## The rule this replaces (and why it was not enough)

The earlier repair for the same red (memory `#14516`) DELETED the three roots
from `auditedDriverRoots`. That is correct only while the tree never grows those
directories back: once a checkout does ship `tasks/*.sh` or `docs/**/*.sh`, the
walk no longer covers it and NOTHING says so — the gate reports a clean tree it
never measured, which is the exact defect the 2026-10-07 widening repaired.

The tolerance therefore keeps the roots WALKED and makes the exemption a named,
measured, self-auditing row instead of a deletion.

## The shape now in the source

`auditedRootAbsences` — one entry per tolerated root: the MEASURED reason,
quoting the command and its value at the commit it was taken (Rule 38). The keys
are literals, not the `…DirName` constants, and that is deliberate: the
constants build the walk list, so a key derived from the same constant would
follow a typo and bless it (a root the tree does not ship and a root that does
not exist are the same observation). The two spellings disagreeing is turned
into a failure by the bidirectional check below.

The zero-contributing branch now asks, in this order:

1. **Does the INDEX track a `.sh`/`.py` under this root?** If yes, the root is
   present and the WALK failed to collect it — a different defect with its own
   message. The tolerance can never swallow it.
2. **Is the root in `auditedRootAbsences`?** If yes, the absence is tolerated,
   the root and its reason go to STDERR, and the per-root line prints `dir=0~`.
3. **Neither?** The original failure verbatim — the misspelled-directory case.

Then `auditAbsentRoots` checks the table against the walk list in both
directions: an entry naming a root nobody walks is a permit checked by nobody,
and an entry whose root started contributing is a permit that outlived its
reason. Both fail.

The tolerance is written to **STDERR, not `t.Logf`**, because that is where
`internal/testutil`'s suite already publishes its setup verdicts (the scratch
root, the daemon-lifetime binding) and it survives on BOTH shapes a reader
meets: `-v`, and any failure. It is **not** visible in a plain `go test` PASS —
measured 2026-10-24 with a 20-line probe package carrying a `t.Logf`, a
`fmt.Fprintf(os.Stderr, …)` and a `TestMain` writing to both streams: Go prints
only `ok …` for a passing package in non-verbose mode, whichever stream wrote
it. Rule 15 asks that a tolerated root be NAMED rather than silently skipped —
the naming is the table in the source (which every reader of the gate meets
first) plus this line under `-v`/on failure, and the acceptance run recorded
here is the `-v` one.

What was NOT changed: `gateMinScriptFiles` (300), `gateMinTrackedCoveragePermille`
(850), the walk list, the four text rules. The tolerance only decides what a
ZERO-CONTRIBUTING root means.

## The red that removing the roots exposed (Rule 41)

With `docs` no longer failing first, the gate surfaced a real offender the
tolerance had been standing in front of: `scripts/hardcode-audit.sh` built its
two working directories with bare `mktemp -d` (lines 303 and 369), landing them
on `$TMPDIR` — i.e. the `/` partition the 2026-09-11 exhaustion filled. Repaired
in the same change: source `scripts/lib/tmp-ban.sh`, take the directory from
`gopi_scratch_mkdir <label>` (which asserts the device id, not the spelling),
and register cleanup with `trap gopi_scratch_cleanup EXIT`. Both scratch
directories were already removed by the script's own `rm -rf "$work"`; the
helper adds the off-`/` assertion and the age-GC registration.

## Checks

```bash
CGO_ENABLED=0 go test ./internal/testutil -run '^TestScriptsAreBoundedAndOffTmp$' -count=1
# green, and prints on stderr:
#   scripts gate: 3 audited root(s) TOLERATED as absent from this tree …
#   scripts gate:   tolerated root: docs (…) / tasks (…) / text2img-eval (…)

bash scripts/hardcode-audit.sh --self-check   # still classifies (12 checks)
go test ./internal/testutil/ -run 'Scripts|Scratch|Rule35|Rule27' -count=1
```

Red proofs (each mutation applied to the production file, run, then restored and
verified against `git rev-parse HEAD:<path>`):

1. tolerate a MISSPELLED root (`"docs"` → `"docz"`): the walk's `docs` root has
   no entry → the ORIGINAL failure, verbatim;
2. delete the `docs` entry: same original failure;
3. make the walk skip a root the index HAS (`tools`): the "index tracks N … the
   WALK collected none" failure, i.e. the tolerance cannot excuse a broken walk;
4. add an entry for a root that contributes (`tools`): "its tolerance entry
   outlived its reason";
5. add an entry for a root nobody walks (`plugins`): "a permit for a root nobody
   walks is a permit checked by nobody".
