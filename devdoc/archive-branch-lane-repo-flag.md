<!-- doc-class: normative -->
# `gopi task archive <branch> --repo PATH` — the branch lane resolved the repo from the process cwd

**Task**: 01a1301b-f69c-7369-b408-b275a525d8ed (branch `task/01a1301bf69c7369b408b275a525d8ed`)
**Measured**: 2026-10-12, host-b (client host)

## The defect

`--repo` was parsed (`parseArchiveArgs`), advertised in the usage page
(`--repo PATH   Repo to inspect/act on. Default: the repo of the cwd.`), and
honoured by every **census** mode — `--check`, `--reclaim`, `--unreachable`,
`--verify-commit` all go through `archiveTargetRepo(opts.Repo)`. The **branch**
lane did not: `runArchiveSubcommand` called

```go
res, err := tools.ArchiveBranchCwd(opts.Branch, opts.Reason, opts.Detail, opts.Override)
```

and `ArchiveBranchCwd` resolves the repo from the process cwd
(`repoRootOf(".")`), never touching `opts.Repo`.

### Measured before the fix (built `bin/gopi`, one fixture repo + one cwd repo)

```
=== A: --repo fixture (cwd = worktree, a real repo) ===   A_EXIT=1
=== B: no --repo ===                                      B_EXIT=1
=== D: --repo /nonexistent-xyz ===                        D_EXIT=1
=== fixture tags ===                                      (empty)

stderr (all three, byte-identical):
gopi task archive: archive archive/fix/some-failed-attempt: git tag archive/fix/some-failed-attempt fix/some-failed-attempt:
  exit status 128: fatal: Failed to resolve 'fix/some-failed-attempt' as a valid ref.
```

Three different `--repo` values — including a path that does not exist — produced
one byte-identical output and one exit status. The failure message is about a
**missing branch**, which is what the operator is told when the real problem is
that the verb looked in the **wrong repository**.

Why it matters more than a cosmetic flag bug: `refs/tags/archive/<branch>` is the
project's decided-exclusion namespace and every merge path reads it as "a loser
was decided here" (PROJECT.md Rule 9). An archive tag written into the wrong repo is
the worst outcome of a verb whose whole purpose is to make a decision legible —
and the mirror failure (a `--repo` that names a non-repo silently falling back to
the cwd) writes that decision into a repo nobody asked about.

## The fix

`cmd/agent/cli_archive.go`, branch lane: resolve the repo **once**, through the
SAME resolver the other four modes use, before anything is read or written.

```go
repo, err := archiveTargetRepo(opts.Repo)
if err != nil {
	fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
	os.Exit(1)
}
res, err := tools.ArchiveBranch(repo, opts.Branch, opts.Reason, opts.Detail, opts.Override)
```

- `archiveTargetRepo(explicit)` (`cmd/agent/cli_archive.go`) already existed: it is
  `tools.RepoRootAt(explicit)` when `--repo` is given, else `tools.RepoRootAt(".")`.
  Nothing new was introduced — the branch lane now *uses* the resolver that was
  already the single answer to "which repo does this verb act on?".
- A `--repo` that cannot be resolved is an **error** (exit 1), never a silent
  fallback to the cwd (Rule 15). The message carries git's own failure for the
  path it was given.
- `tools.ArchiveBranchCwd` is **not deleted** (it is an exported API with other
  potential callers), but its doc comment now states what it is: a
  process-cwd-relative door that the `gopi task archive` verb is NO LONGER a
  caller of, with the reason (one entry point that resolves the cwd is exactly
  how `--repo` became inert).
- No new literal: the lane reuses the existing `archiveTargetRepo` and the
  existing `progName`/stderr shape (Rule 1/13/14).

## Regression test

`cmd/agent/cli_archive_test.go` →
`TestArchiveCLIBranchLaneHonoursRepoFlag`. Driven through the **real binary**
(`gopiTestBinPath`), because the defect was an exit status and a ref — a unit
test of the parser would have passed while the operator's command resolved the
wrong repo. Two repos: `target` (what `--repo` names) and `cwd` (where the
process stands).

- **(a) the fix** — `--repo <target>` with the cwd in a different repo: exit 0,
  `refs/tags/archive/<branch>` exists IN THE TARGET at its branch tip, the CWD
  repo has no archive tag, and the CWD repo's own branch ref is untouched (a
  cwd-resolved archive would have carried the decision through and deleted it).
- **(b) the refusal** — `--repo` naming a path that is not a repo: non-zero exit,
  the output carries git's own `--show-toplevel` failure, and **nothing changed**
  in either repo (tag sets and the target's branch ref are compared before/after)
  — no silent cwd fallback.
- **(c) the control** — no `--repo`: the cwd's own repo is still the target, so
  the fix cannot be "use whatever path was named last".

Red-then-green: with `cmd/agent/cli_archive.go` reverted to master's version and
the new test in place, the test fails at reading (a)
(`--repo pointing at the target repo failed: exit status 1`); with the fix it
passes.

## Evidence (self-filed)

- Before: the three-invocation block quoted above — one byte-identical stderr, three
  `exit=1`, fixture tag namespace empty.
- After (built `bin/gopi`, `GOPI_HOME` = a private sandbox profile, cwd = a
  different repo than `--repo`):
  `git -C <target> for-each-ref refs/tags/archive/` prints
  `b05d44624f37d79033cf94d06ee9ed1fe9461f1a commit refs/tags/archive/fix/some-failed-attempt`;
  `git -C <cwd> tag -l 'archive/*'` is empty and `refs/heads/feat/elsewhere` still
  resolves. The non-repo `--repo` run: `exit=1` with
  `git rev-parse --show-toplevel in <path>: chdir <path>: no such file or directory`
  and still no tag in the cwd repo.
- Red proof: `go test ./cmd/agent/ -run TestArchiveCLIBranchLaneHonoursRepoFlag`
  → FAIL on the unfixed file, `ok` on the fixed one.

## Not done

- `tools.ArchiveBranchCwd` is kept (only its doc comment changed). Deleting an
  exported function is a separate, wider change; the fix removes the verb's
  dependence on it, which is what the defect was.
- The same audit was NOT performed for other verbs' repos: this change covers
  `gopi task archive`'s branch lane only.
