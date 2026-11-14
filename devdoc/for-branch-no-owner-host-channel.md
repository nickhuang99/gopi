<!-- doc-class: historical -->
# `gopi task for-branch`: the no-owner answer now names its HOST and its boundary

**Class: historical.** The record of ONE change (epic `01a147e5`, 2026-10-17).
The standing contract lives in the verb's own answers (`TaskForBranchReport`'s
named sentences) and in `cmd/agent/merge_prompt.md` §1/§3.11; this file is the
why, the measured defect and the evidence.

## 1. The defect

The no-owner answer was already scoped to the LEDGER:

```
no owner: no ledger row names branch feat/x
  ledger: /home/user/.gopi/memory.db — this LEDGER only (a host carries one per profile; --db names another)
```

That is only half of the scope, and the missing half is the one that cost a
session. A ledger path names a database; it does not name a MACHINE. On a
two-host install both hosts commonly carry the **same path literal**
(`/home/<user>/.gopi/memory.db`), and with row exchange off — `task_sync`'s
default — every row a client submits stays on that client's ledger. So a branch
produced by a completed, correctly-cleaned run on the other host reads as an
orphan HERE, and the merge run escalated it as ownerless work. The answer was
true about one database and was read as a verdict about the install.

## 2. What changed

Three additions, no new machinery, no propagation change (Rule 35; `task_sync`
remains the operator's switch — the new sentence only REPORTS its value):

1. **`TaskForBranchLookup.Host`** — `os.Hostname()` at read time, through ONE
   reader `taskForBranchHostname()` shared by the branch lookup and its worktree
   sibling (Rule 14). Empty means "could not be read", rendered as UNKNOWN,
   never as a blank (Rule 15).
2. **Two lines under the verdict**, before the size line and before the git
   channel:
   ```
     host:   hostb — the machine that ledger was read on
     other hosts' ledgers were NOT consulted (row exchange is task_sync=off): a branch dispatched
     on another host reads as ownerless HERE — `gopi task send --task-id <id>` lands one row in a
     peer's ledger, and `ssh <peer> 'gopi task show <id>'` is the only proof it is there
   ```
   The switch label comes from `config.TaskSyncSettingsKey` + its ONE reader
   (`config.TaskSyncEnabled`), so the sentence cannot claim a value the switch
   would not report.
3. **The §3.11 merge prompt carries the same channel**: §1 states "no ledger row
   named it" is a claim about ONE ledger on ONE host, the ORPHAN example line
   names `host H, ledger L` and the boundary, and the report template's
   `landed ORPHAN:` row requires them.

## 3. Why a field, and why the sentence is not advice

`Host` is a FIELD (`json:"host"`) so a script gets it without parsing prose —
the same reason `LedgerPath`, `Ground` and `Reconciled` are fields. The
sentence is a report, not a prescription: it names the switch because the
reader's next move depends on it, and it explicitly does NOT tell anyone to
turn propagation on.

## 4. Evidence

Red-then-green: with the three render lines removed, both new arms fail
(`the no-owner answer must carry "  host:   …"`, `order must be verdict → host →
boundary (got 0, -1, -1)`) and the prompt gate fails on
`no longer states "other hosts' ledgers were NOT consulted"`; restoring them
turns all green. Real invocation:

```
$ ./bin/gopi task for-branch feat/definitely-no-row-names-this --repo <worktree>
no owner: no ledger row names branch feat/definitely-no-row-names-this (repo …)
  ledger: /home/user/.gopi/memory.db — this LEDGER only (…)
  host:   hostb — the machine that ledger was read on
  other hosts' ledgers were NOT consulted (row exchange is task_sync=off): …
  (1533 rows carry a branch; none names this branch in repo …)
  no repository read (…) registers the branch … — there is no ground to reconcile against
exit=4
```

`--json` carries `"host": "hostb"` beside `"ledger_path"`.
