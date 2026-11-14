<!-- doc-class: historical -->
# Root-level build outputs from an explicit `-o` are ignored — and the gate now says so

**Class:** historical — this is the record of ONE run's landing (the red-proof
transcript and the measurements that justified the change). The standing rule
itself does not live in this file: it lives in `.gitignore` (the `/gopi-*` line
and its comment) and in `internal/standards` (`gopiRootArtifactPrefix` + the
root-artifact class), which is where a later reader should look for authority.
It extends `devdoc/repo-hygiene-tracked-scratch.md` (the index half) with the
ignore half for one class.

## The gap this closes

`tasks/01a0cea0-rule31-two-states.sh:35` builds at the CHECKOUT ROOT:

```
CGO_ENABLED=0 go build -o gopi-behind ./cmd/agent/
```

That is a tracked recipe (it is how the two-state A/B evidence for
`internal/version` is produced), and the same shape appears in the devdoc
narratives that compare builds (`go build -o gopi-before ./cmd/agent/`,
`-o gopi-fixed`, `-o gopi-cs-bin`, `-o gopi-peak-control-bin`). Every one of
them drops a ~28 MB ELF at the checkout root, and none of them was covered by
`.gitignore`:

```
$ git check-ignore -v gopi-behind
rc=1          # nothing matched — the artifact was one `git add` from being committed
```

`.gitignore` covered `/agent` and `/gopi` (the no-`-o` names) but not the
`-o`-with-a-variant class. The measured artifact reproduces exactly:
`CGO_ENABLED=0 go build -o gopi-behind ./cmd/agent/` → 28,528,194 bytes, ELF,
exit 0.

**Why this is a gap and not pedantry:** the same door was already walked
through once. A 26284210-byte `gopi-behind` was COMMITTED on an earlier branch
(found while measuring a stale salvage branch) — so the class has a real
precedent, not a hypothetical one.

## The chosen shape, and why it cannot recreate the 2026-09 accident

```gitignore
/gopi-*
```

Both halves of that line are load-bearing:

| decision | why |
|---|---|
| leading `/` (root-anchored) | The accident this rule must not repeat: an UNANCHORED `agent`/`gopi` pattern matched at every depth, so it swallowed `cmd/agent/*` and `internal/agent/*`. `.gitignore` records it in-place: *"unanchored \"agent\"/\"gopi\" accidentally ignored cmd/agent/* and internal/agent/*"*. A `/` match is rooted at the checkout, so the rule cannot reach any directory. |
| the `gopi-` PREFIX | A name list (`/gopi-behind`) is only ever as long as the variants someone remembered; the class is "whatever the last build named". The prefix is what makes it future-proof. |
| the hyphen is not optional | `/gopi` stays on its own line: `*` requires at least the hyphen, so the no-`-o` producer keeps its own pattern and its own reason. A bare `/gopi*` would also swallow a `gopifoo` typo-squat. |

The proof that the anchoring is real, on the live tree:

```
$ git check-ignore --no-index -v cmd/agent/main.go internal/agent/loop.go
rc=1                                     # matched by NOTHING — the class cannot reach the source tree
$ git check-ignore -v gopi-behind gopi-before gopi-fixed
.gitignore:27:/gopi-*	gopi-behind
.gitignore:27:/gopi-*	gopi-before
.gitignore:27:/gopi-*	gopi-fixed
rc=0
```

`/gopi-*` deliberately does NOT cover `scratch/`, whose class has no ignore line
by ruling — an ignore rule would hide the writer, and the `??` entry is the
diagnosing signal Rule 30 reads (`devdoc/scratch-repo-residue-01a0db54.md`).
That asymmetry is intentional and is stated in `.gitignore` next to both rules.

## The gate: the ignore half of `TestRepoRootHasNoTrackedJunk`

The existing gate had two halves already: the INDEX half (`git ls-files -z`,
"is this tracked") and the IGNORE half (`git check-ignore --no-index -v`,
"would a re-appearance show as `??`"). The new class was extended into both, so
a root artifact is now asserted BOTH untracked and ignored:

- `internal/standards/scratch_gate.go`
  - `junkPathChecks` gains a seventh class: root path (`IndexByte(p,'/') < 0`)
    AND the `gopi-` prefix, with the reason and the accident quoted in its `why`.
  - `gopiRootArtifactPrefix = "gopi-"` is the ONE spelling of the prefix
    (Rule 1/13/14) — the gate predicate and the doc's reasoning both reference it.
  - `junkIgnoreSamples` gains `gopi-behind` / `gopi-before` / `gopi-fixed`
    (three spellings, ONE rule — the samples walk the prefix, they do not define it).
  - `junkIgnoreAllowed` gains `cmd/agent/main.go` and `internal/agent/loop.go` —
    the two exact paths the 2026-09 unanchored patterns swallowed. If the class
    is ever loosened to a bare name, THIS list fails the build.
- `internal/standards/scratch_gate_test.go`
  - the red-proof's poisoned list gains `gopi-behind` / `gopi-before`
    (the count assertion means a silently non-matching predicate fails);
  - the clean list gains the same two source paths, so the predicate half is
    pinned the same way as the real-git half.

Nothing else in the gate was touched, and no new dependency was created
(Rule 35): the deliverable is one ignore line, one constant, one class, and the
fixtures that pin them. `internal/standards/scratch_gate.go:98` — the line the
DOC-INDEX anchors for `devdoc/scratch-repo-residue-01a0db54.md` — is unchanged
(the new class was appended AFTER the last existing class; verified by
`sed -n '98p'`).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → **OK** (no output, exit 0)
- Vet: `CGO_ENABLED=0 go vet ./...` → **OK**
- Tests: `go test ./internal/standards/` → **ok github.com/nick/gopi/internal/standards 28.577s**
  (whole package, includes the four other gates; the five `TestRepoRoot*` tests
  individually: all PASS)
- Ran: `git check-ignore -v gopi-behind`
- Result: rc=0, `.gitignore:27:/gopi-*	gopi-behind`

### Red-proof (transcript)

The gate is not vacuous: the mutation makes it fail, and it fails NAMING the
path. Both mutations were run against the live tree with `.gitignore` restored
between them (restored file sha256 == the pre-mutation sha256:
`b5217747350d7784946cf868ea16c895f20fa40bbd13bfc6f56feb1202540c50`).

**RED A — the ignore line removed** (the required mutation):

```
$ sed -i 's|^/gopi-\*$|# RED-A: /gopi-* removed|' .gitignore
$ git check-ignore -v gopi-behind
rc=1
$ go test ./internal/standards/ -run TestRepoRootJunkPathsAreGitignored
--- FAIL: TestRepoRootJunkPathsAreGitignored (0.01s)
    scratch_gate_test.go:158: 3 junk path(s) are NOT covered by .gitignore — an untracked one shows up in `git status` and is one `git add` from being committed:
          gopi-behind (a root build output from an explicit -o)
          gopi-before (a root build output from an explicit -o)
          gopi-fixed (a root build output from an explicit -o)
FAIL
```

**RED B — the rule loosened to the accident's spelling** (bare, unanchored
`agent` — proving the gate also catches the over-broad direction):

```
$ sed -i 's|^/gopi-\*$|agent|' .gitignore
$ git check-ignore --no-index -v cmd/agent/main.go internal/agent/loop.go
.gitignore:27:agent	cmd/agent/main.go
.gitignore:27:agent	internal/agent/loop.go
rc=0
$ go test ./internal/standards/ -run TestRepoRootJunkPathsAreGitignored
--- FAIL: TestRepoRootJunkPathsAreGitignored (0.00s)
    scratch_gate_test.go:158: 3 junk path(s) are not covered …
          gopi-behind (a root build output from an explicit -o) …
    scratch_gate_test.go:173: legitimate path "internal/agent/loop.go" is ignored — the pattern is too broad
    scratch_gate_test.go:173: legitimate path "cmd/agent/main.go" is ignored — the pattern is too broad
FAIL
```

**Restored:**

```
$ git diff --stat .gitignore
 .gitignore | 19 +++++++++++++++++++
$ go test ./internal/standards/ -run TestRepoRootJunkPathsAreGitignored
ok  	github.com/nick/gopi/internal/standards	0.964s
```

Full transcript: `~/.gopi/sessions/01a0e97a-e3e2-7017-977a-d2984b6221cb/redproof.log`.

## Not Done (honest)

- **The no-`-o` class was left as it is.** `rootBuildOutputs` still lists
  `agent` / `gopi` exactly; `/gopi-*` rejects the variant suffix, so the two
  classes stay disjoint and each keeps its own reason. No merge was attempted.
- **The recipe still builds to the root.** `tasks/01a0cea0-rule31-two-states.sh`
  was NOT changed to pass `-o` under the scratch root: it is a tracked evidence
  script whose whole point is a checkout-local A/B run, changing it is a
  different task with its own verification, and this task's remit was the ignore
  rule + the gate. The rule now makes that stray artifact invisible to
  `git add`, which is the safety property intended — but the PREFERRED fix
  (build into `bin/` or the pinned scratch root) is not yet applied to the
  script. Filed as a follow-up rather than done silently here.
- **`.verify-full.log` sits TRACKED at the repo root** (from `git ls-files`), is
  not in any junk class, and is not in this task's scope. A tracked 100 KB+ log
  at the root is the same *shape* as the parking-lot class, but removing it is a
  different decision (something may read it) — reported, not touched.
- **No binary was committed.** The 28 MB `gopi-behind` this run produced stays
  untracked and ignored; it is deleted before close (Rule 30).
