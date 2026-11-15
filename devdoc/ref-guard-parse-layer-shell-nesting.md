<!-- doc-class: normative -->
# The ref guards' parse layer: a clause now also yields the commands its nesting runs

## The defect, measured

Row `01a1964f`. With this run's ground announced and `/home/user/work/gopi`
declared as the host's shared checkout, on `1d16b04`:

```
$ git -C /home/user/work/gopi worktree add -b task/probe-guard-01a1964f <nonexistent-parent>/wt 2>&1
REFUSED by the guard (a `worktree add` in a shared checkout)

$ OUT=$(git -C /home/user/work/gopi checkout -b task/x-legal 2>&1); echo "$OUT"
exit 0 — git EXECUTED
```

The branch name was never the problem: `git check-ref-format --branch task/x-legal`
exits 0.

## Why it happened — a parse fact, not a missing spelling

`refGuardClauses` cut the line on `; & |` and newlines only. So the second command
produced ONE clause, `["OUT=$(git" "-C" "/s" "checkout" "-b" "x" "2>&1)"]`, and
`refGuardGitSubcommand` — which reads the clause's COMMAND HEAD — found no `git`
there and answered "not a git invocation". The clause was dropped. The git
invocation inside the substitution was never a clause at all.

Two further facts made the head unreadable even after the splitter improved:

1. The splitter's environment-prefix rule consumed the head word `OUT=$(git` as a
   `VAR=VAL` prefix, dropping the command with it.
2. `strings.Fields` splits a quoted `-c` body into pieces (`'git`, `-C`, …), so a
   body read from the first piece alone is a fragment the recogniser cannot parse.

## The fix — one second dimension on the existing splitter

`internal/tools/ref_guard_shell_nesting.go` adds `refGuardClauseSegments`: a clause
yields its own simple command **plus the simple commands its nesting constructs
run** — `$( … )`, `` `…` ``, `eval`, `bash|sh|zsh|dash|ksh -c` / `-lc` / `-ec`,
recursively (bounded at `refGuardMaxShellNestingDepth`). `refGuardUnwrapShellHead`
turns a wrapper head into the command it wraps **without changing the word count**
(so every existing verb-index reader stays correct).

Consumers iterate segments, and the recogniser, the fail-closed arm and every
refusal sentence stay exactly where they were:

- `clone_ground_branch_create_guard.go` (the measured guard)
- `git_publish_target_guard.go`
- `shared_integration_ref_guard.go`'s `refGuardRecognisedTargets`

It is **structural, not enumerated**: the property is "a shell word whose value
opens a nesting construct is syntax around a command", so `OUT=`, `${X}=`, `then`,
`!`, `2>&1)` are all handled by the same rule rather than by a prefix list.

## Fail-closed

An **unterminated** substitution (or a nesting deeper than the bound) produces a
marker segment; the guard refuses it BY NAME. The original defect was never the
missing spelling — it was that "I did not understand this command" read as "there
is nothing here to judge" (Rule 15).

## What is deliberately NOT covered

A command a **file** supplies (`bash script.sh`, `source /tmp/x`, an alias, a
function defined elsewhere) is out of reach of any text-level guard. Said out loud
so nobody mistakes it for coverage.

## Verification

- Red proof: mutating `refGuardClauseSegments` back to clause-only makes 21
  subtests fail, including `the_measured_bypass … it ran: exit=0`; restore verified
  by content identity against a recorded pre-mutation sha256.
- Green: `go test ./internal/tools/ -run 'TestCloneGroundBranchCreate_|TestRefGuardShellNesting'`
  → 41 subtests PASS.
