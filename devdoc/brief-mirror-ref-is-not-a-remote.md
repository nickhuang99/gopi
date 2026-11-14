<!-- doc-class: normative -->
# The mirror name is a REF, not a remote — and the briefing now says so

**Measured 2026-10-17**, row `01a14877-322b-7d56-9702-bf1528d4be69`, host
host-b, ground cut at `7ecfa12`.

## What was wrong

`refs/remotes/gitPushcheck/master` is the push-back lane's scratch MIRROR: it is
written by a fetch refspec (`gitPeerMirrorRefspec()` =
`+refs/heads/master:refs/remotes/gitPushcheck/master`,
`internal/tools/git_peer_fetch.go:145`), and the namespace's name is
`gitPushbackCheckNamespace` (`internal/tools/task_sync_git_pushback.go:57`). No
remote of that name is ever created — deliberately, because a real remote would
collide with the lane's own refspec target.

The briefing seam's "which `master`" clause
(`internal/tools/task_briefing_master_reading.go`, appended by
`cmd/agent/cli_tasks.go:3013`) rendered that ref's FULL NAME in a table and said
what the ground was cut from, but nothing about the name being a namespace. A
reader one sentence away from `git fetch gitPushcheck` runs it and gets:

```
$ git fetch --quiet gitPushcheck
fatal: 'gitPushcheck' does not appear to be a git repository
exit=128
```

while the ref itself answers every ancestry probe (measured:
`git merge-base --is-ancestor 7ecfa12da refs/remotes/gitPushcheck/master` ⇒ 0).
Several briefings carried that spelling in their own acceptance prose, which made
it look like the probe was broken rather than that the spelling was wrong.

## What is delivered

The clause now MEASURES the tree's remote layout (`git remote`) and renders one
of three sentences — it never assumes:

| layout | rendered |
|---|---|
| a remote named `gitPushcheck` exists | nothing (the name is fetchable; no warning is noise) |
| the name is NOT a remote, remotes exist | the ref is a REF; `git remote` lists them; `git fetch gitPushcheck` fails with git's own sentence + exit 128; the runnable refresh is `git fetch <remote> <lane refspec>` |
| no remote at all | the ref is READ, never fetched — no refresh command is printed |
| the listing failed | the layout is UNMEASURED (Rule 15: never "no remotes") |

The refresh target is chosen from the measurement: the lane's peer when this
tree has it as a configured remote, otherwise the first remote the tree has.

## Why the generator side and not the brief's prose

The prose is the submitter's (Rule 37's boundary) and has no generator to mend.
What gopi OWNS is the clause it appends, and that clause is where the name was
first handed to a reader. Rule 31 caught two rendering defects during execution
that no amount of reading would have: `git fetch fetch peer1:/home/user/work/GoPi …`
(the verb was in both the verb constant and the target helper) and doubled quotes
around the name. Both were fixed and the fix was re-run on the real tree; the
rendered command was then executed (`git fetch peer1
+refs/heads/master:refs/remotes/gitPushcheck/master` ⇒ exit 0).

## Check

`go test ./internal/tools/ -run TestMasterReadingClauseStatesTheMirrorIsARefNotARemote`
— three arms, with the positive control (the name IS a remote ⇒ no warning) so
the red arm cannot be satisfied by a clause that warns on every tree.
