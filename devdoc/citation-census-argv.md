<!-- doc-class: historical -->

> **Retired mechanism (documentation-citation teardown, 2026-11-05).** 本页记录的机制在树里
> **已不存在**。`78bdf05a`（operator ruling）与 `0ee5745c` / `1e3b0976` 删掉了整个
> documentation-citation 家族：三个生成器 `internal/standards/testdata/config-anchor-census.go`、
> `citation-census.go`、`citation-selector-census.go`，它们写的三张 `.tsv` 表，驱动它们的
> 两道门 `internal/standards/citation_gate.go` / `citation_selector_gate.go`（其表已不存在，
> 这两道门本体由 `1e3b0976` 删除；`78bdf05a` 删的是它们的测试），
> 以及交付路径上的 census-citation 腿（`internal/devworkspace/census_gate.go`、
> `census_redrive.go`，见 `0ee5745c`）。
>
> **下面没有任何一条命令还能跑**：`go run …` 得到 `stat … no such file or directory`，
> `go test -run TestMergeConfigAnchorGate` 打印 `[no tests to run]` 并 exit 0（**空绿，不是通过**）。
> 本页保留为**历史记录**：它记的是当时那个缺陷与堵住它的推理。测量方法本身已随生成器消失。
>
> 今天仍能跑、也仍该跑的，是 Rule 36 的语料门：
>
> ```bash
> go test ./internal/standards/ -run RetiredPipeline -count=1
> go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader -count=1
> ```

# The citation census generator's argv is a CLOSED vocabulary

**Rule anchor:** PROJECT.md Rule 15 (no silent failure — refuse by name), Rule 30
(a run leaves its worktree clean), Rule 31 (execute the surface you changed).

## The defect this file is the regression test for

`internal/standards/testdata/citation-census.go` used to parse argv as
`[<root> [<table>]]`: the second POSITIONAL was the table path, and nothing
looked at whether a word was spelled like a flag. Measured on a clean ground
(2026-10-28):

```sh
$ git status --porcelain
(empty)
$ go run internal/standards/testdata/citation-census.go . --print --at ee0a1f3
citation-census: wrote 525 rows to --print at d9a0153 (6556 tracked paths, ...)
rc=0
$ ls -la -- ./--print
-rw-r--r-- 1 nick nick 66802 Oct 28 21:50 ./--print
```

Two defects in one argv word:

1. `--print` was not refused — it became the **table path**, so the program
   WROTE a 66 KB file named `--print` into the caller's worktree and reported it
   as success (exit 0).
2. That file is untracked residue (Rule 30), and removing it needs
   `rm -- ./--print`; a plain `rm --print` is a *different* command.

It matters beyond tidiness: the SIBLING generator
(`internal/standards/testdata/config-anchor-census.go`) accepts `--print` and
`--at REV`, and refusal text elsewhere names `--at REV` as the remedy — so a
reader who follows the documented advice produced residue instead of a reading.

## The vocabulary, and why each rule is there

```
citation-census <repo-root> [<table.tsv>] [--check | --print] [--at REV] [-h|--help] [--]
```

| rule | the failure it prevents |
|---|---|
| an unknown `-`-leading word is refused BY NAME, listing the accepted set | `--prnt` becoming a filename |
| `--print` is a REAL flag: it prints the table and writes nothing | the write leg being reached by a flag |
| `--at REV` reads the index AND the bytes from the object store (`git ls-tree`, `git show`), never the checkout | "measure REV" silently re-measuring the worktree |
| a value flag needs a non-empty operand and refuses a flag-shaped one | `--at --print` eating the next flag as a revision |
| `--` terminates flags; after it every word is a path | a path that begins with `-` being unusable |
| a positional path whose BASE NAME begins with `-` is refused even after `--` | a reader who really does type `--print` as a path still leaving residue |
| `-h` / `--help` is decided from argv BEFORE anything runs | a usage query that touches the filesystem |

## How the test proves it

`internal/standards/citation_census_args_test.go` builds the generator ONCE into
the test's temp dir and executes the BINARY, so it reads the generator's real
exit status (`go run` collapses a program's status to 1, and the split between
`exitDrift` and `exitError` is part of the vocabulary). Each arm runs the
generator **inside an empty temp directory** and asserts two things together:

- the exit status and the refusal text, and
- **no file was created** — the directory is empty afterwards.

The last arm is the control: the documented plain forms
(`<root> <table> --check`, and `<root> --print --at REV`) must still work, so a
green suite cannot be one that refuses everything.
