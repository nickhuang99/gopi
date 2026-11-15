<!-- doc-class: normative -->
# The removal-audit census reads Go — the shell half and the two carriers it found

**Measured: 2026-10-15, task 01a13b36, base 0bbb41c6f.**

`internal/standards/removal_audit_gate_test.go`'s walk (`removalAuditWalk` →
`goFilesUnder(t, root, prodRoots, false)`) reads **Go files only**. So a destructive
ref removal written in a tracked shell script — or TAUGHT in the instruction file —
was invisible to the census that exists to make every removal attributable, and the
census stayed green while the removal wrote nothing.

## The measurement, before the fix

`scripts/post-merge-cleanup.sh` ran a raw branch delete (`git branch $DEL_FLAG
"$BRANCH"`, the flag held in a variable and upgraded to `-D` under `--force`).
Driven against a private fixture repo with `GOPI_REMOVAL_AUDIT_LOG` pointing at an
empty directory:

```
SB=<session>/refaudit; FX=$SB/fx; SINK=$SB/sink
git -C $FX init -q -b master
git -C $FX -c user.email=a@b -c user.name=t commit -q --allow-empty -m base
git -C $FX branch feat/scriptprobe
cd $FX && GOPI_REMOVAL_AUDIT_LOG=$SINK/removal-audit.log bash <tree>/scripts/post-merge-cleanup.sh feat/scriptprobe
```

Observed BEFORE: `refs/heads/feat/scriptprobe` deleted, and **the sink directory
still empty** — the removal was silent, which is the 2026-09-17/18 incident shape
one language over.

## What the fix is (and what it deliberately is not)

The census gained a TEXT half (`removal_audit_shell_gate.go`, registered in
`gate_test.go`) over the carriers the archive-mint gate already uses: `scripts/`,
`GOPI.md` and the PRODUCTION Go trees (`internal/`, `cmd/`). devdoc/, tasks/ and
briefings/ stay OUT because they are HISTORY and must be able to quote the pre-fix
command; a `_test.go` fixture is out because it exists to CONSTRUCT the shape.

The predicate reports three spellings of a ref removal — `git branch -d|-D|-r|
--delete`, `git update-ref -d <ref>`, `git push --delete` / `:<refs/heads/…>` —
with the script's own measured shape covered: a flag carried into the delete through
a VARIABLE is resolved (`DEL_FLAG="-d"` → `git branch $DEL_FLAG "$BRANCH"`), the
same way the Go half resolves the tree's constants. A `#`-prefixed line and a Go
`//` comment are prose, never a site.

The remedy is not a new writer: `gopi task archive <branch> --reason <code>` already
does the whole job. MEASURED on this host, against a fixture with an unowned merged
branch: it wrote `archive/feat/scriptprobe` + its reason ref, reclaimed the head ref
through `archive-reclaim-branch`, and appended a `kind=branch-delete` record to the
sink. That is why the census's green arm is "route through the writer", and why the
script now has ONE path to a removed ref instead of two.

## The two primary carriers, repaired

1. `scripts/post-merge-cleanup.sh`: the raw delete is gone. The archive leg — which
   used to run only when `AHEAD > 0` (provably never, for a script that only cleans
   merged branches) — now runs for every merged branch, because the reclaim leg IS
   the ref removal. If the ref survives, the script REPORTS it and refuses to delete
   it by hand; `POST_MERGE_TAG=0` therefore no longer means "delete without a
   record", it means "report only". `--force` was retired with its only reason
   (`-D`) — a flag that silently selects an unaudited deletion is not a knob to keep.
2. `GOPI.md` Rule 9 step 2 taught `git branch -D <name>` beside the tag mint; the
   Commit-Path step 6 taught `git branch -d`. Both now name the one writer, and the
   prose states the measurement (the sink stayed empty) instead of the command.

## The other seven carriers the census reported

Fixing only the two the epic named would have left the census RED, and the seven
others were REAL: each rendered or taught a spelled raw deletion.

| Site | What it was | Repair |
| --- | --- | --- |
| `internal/tools/task_adoption.go:260` | `adoptionPruneCommand` RETURNED `git branch -D <b>` as the operator's next command | returns `gopi task archive <b> --reason superseded` (local); a remote-tracking ref — which no archive writer owns — is cleared by the fetch that prunes it |
| `internal/tools/task_sync_git_pushback.go:265` | the unrelated-history refusal told the operator to `git branch -D` the ref | names `gopi task archive <b> --reason unrelated-history` (the vocabulary's own code for this case) |
| `internal/tools/task_staleness_guard.go:1025` | a verdict sentence explained the ref's absence as "`git branch -d`" | names the close-out chain's branch delete — the observation is unchanged, the taught spelling is gone |
| `internal/tools/verify_gate.go:800` | the verifier briefing explained the consumed ref as "`git branch -d`" | same (names the close-out chain's delete) |
| `internal/tools/branch_delete_guard.go:522` | a refusal sentence quoted "`git branch -d`" to say it is not a lock | states the merged-only lock by name |
| `scripts/gopi-stats.sh:795` | `git branch -r --format=…│count` — a READER | predicate narrowed: `-r`/`-a`/`-f` are list/move flags, not deletions (a fused `-rd` still matches) |
| `GOPI.md:319`, `:355-359` | my own new prose spelled the removed commands | reworded to name the acts without spelling them |

## Cross-file anchors this change touches (checked, not assumed)

`cmd/agent/merge_prompt_gate_test.go` and `internal/tools/task_sync_git_pushback_ancestry_test.go`
pin the pushback refusal's text (`git branch -D feat/orphan`); both were re-run green
after the reword, and the DOC-INDEX rows whose evidence anchors land in the touched
files were re-verified.

## Not done (deliberately, and filed)

`cmd/agent/merge_prompt.md` teaches the same raw deletion in its Merger disposition
table (~10 rows). It is NOT a carrier of this half — it is a closed, separately gated
surface of the Merger's decision path — and rewriting those rows changes the merge
lane's own instructions rather than this census's reach. It is filed as its own work
item; the exclusion is stated in the gate's own Rule-15 paragraph so the hole is
visible rather than assumed away.
