<!-- doc-class: historical -->
# The build-metadata keys: an independent re-verification on master, and the SHELL half of the same defect class

**Reviewed at:** `refs/heads/master` = `refs/remotes/gitPushcheck/master` = `01c1674121dc089ada8e1d0292a5f2a1cb90a3e5`
**Ground:** `task/01a1419754e27acdb2f479051fb0afd8` (cut from `01c1674`), task `01a14197-54e2-7acd-b2f4-79051fb0afd8`
**Predecessor:** task `01a1362e-290b-78e0-a707-a9c0aa477647`, commits `729601f90` + `4188501ef`
**Evidence:** `~/.gopi/sessions/01a14197-54e2-7acd-b2f4-79051fb0afd8/evidence/E1-*.txt`, `E2-*.txt`, `E3-*.txt`

## 1. What was re-verified, and how

The claim under review was "the build-metadata keys (`vcs.revision` / `vcs.modified` /
`vcs.time`) have exactly ONE definition site in production Go code, and that site is a gate".

Measured **on the cut point, not on the predecessor's branch** (the predecessor's own report is
not evidence that the commit reached master):

| check | command | result |
|---|---|---|
| the fix commit is an ancestor of master | `git merge-base --is-ancestor 4188501ef refs/heads/master` | exit 0 → `4188501ef IS on master` |
| the two refs agree | `git rev-parse refs/heads/master refs/remotes/gitPushcheck/master` | both `01c1674121dc…` |
| production Go carries the keys **once** | `git grep -n '"vcs\.\(revision\|modified\|time\)"' HEAD -- '*.go' \| grep -v _test.go` | three hits, **all three** in `internal/buildidentity/buildidentity.go:63,69,74` |
| every production reader references the constant | `git grep -n 'BuildInfoKey\(Revision\|Modified\|Time\)' HEAD -- '*.go' \| grep -v _test.go` | `cmd/agent/cli_version.go`, `internal/sessionevent/artifact.go`, `internal/tools/image_selfcheck.go`, `internal/tools/memory_sync.go`, `internal/version/self.go` (re-export) — **no second spelling** |
| the gate exists and is non-vacuous | `go test ./internal/buildidentity/ -v` | `TestBuildInfoKeysHaveOneDefinitionSite` PASS, `TestBuildInfoWireValues` PASS |

The gate's own non-vacuity is a `minScanned = 100` floor against a walk that
actually reaches the tree; the independent count of production `.go` files it
walks is **876** (measured: `find . -name '*.go' -not -name '*_test.go' -not -path './.git/*'
-not -path '*/testdata/*' -not -path '*/vendor/*' -not -path '*/node_modules/*' | wc -l`), so the
floor is not the thing that makes it pass — the scan did reach the tree.

**Red proof A** (planting probe → FAIL → remove → PASS), verbatim in
`evidence/E2-redproof-buildidentity-gate.txt`: a new production file
`internal/buildidentityprobe/redprobe.go` carrying `const redProbe = "vcs.revision"` makes the
gate FAIL **and name that file**; removing it turns the package green and
`git status --porcelain` clean.

### Residual of the same class, in production Go, checked and found EMPTY

Every other thing a `debug.BuildInfo` consumer could re-type was searched for and none is
read in production code: `git grep -n '"-buildvcs"\|"CGO_ENABLED"\|"GOARCH"\|"GOOS"\|"-compiler"' HEAD -- '*.go' | grep -v _test.go`
returns only `buildidentity.LdflagsFlag` (the flag, already a constant). The one remaining
bare-`vcs.*` pair on master lives in `internal/tools/rebuild_tracked_guard_test.go:136` — a
**test** fixture that writes a throwaway probe module and asserts against the real toolchain's
output, which is exactly what the Go gate deliberately excludes (`_test.go` is not scanned).
That exclusion is not a gap: the value it asserts is the toolchain's, not gopi's.

## 2. The residual that WAS open: the shell recipe's `-X` operand

Rule 13/14 asks for one definition of a shared value. On `01c1674` that holds for every **Go**
consumer and does **not** hold for the **shell** consumer. `scripts/test-budget.sh` stamps the
tested binary with its own spelling:

```sh
tested_bin_identity_var="Revision"                 # typed literal
tested_bin_identity_pkg="./internal/version"       # typed literal
tested_bin_pkg="$(cd "$root" && go list -f '{{.ImportPath}}' "$tested_bin_identity_pkg" …)"
tested_ldflag_args=(-ldflags "-X $tested_bin_pkg.$tested_bin_identity_var=$tested_bin_rev")
```

The script's own comment calls `go list` "the module's own source of truth", and for the **module**
half that is true. The **package** half and the **variable** half are literals that no machine
holds against `buildidentity.RevisionLdflagPath` — measured: `git grep -n
'tested_bin_identity_var\|tested_bin_identity_pkg' HEAD -- '*.go'` returns **nothing**. The
neighbouring gates in `internal/testutil` read the script for `budget_subdir`,
`tested_bin_dir_suffix` and `start_record_suffix`, but none of them reads the operand.

**Why that is the same defect and not a cosmetic one.** The Go linker checks no operand. A
renamed variable or a moved package degrades this recipe to stamping **nothing**, at exit 0 with
empty stderr — the silent-drop path `internal/version/module_path_agreement_test.go` documents
for the Go constant. Every freshness verdict that rests on the stamp
(`cmd/agent/gopibin_identity_guard_test.go`) then falls back to comparing mtimes, which cannot
tell a byte-identical rewrite from a real edit: the failure the stamp exists to prevent.

### The fix

`internal/testutil/testbudget_identity_operand_gate_test.go` (317 lines, commit `cc9cbfc3d`).
It resolves the script's package half **through the toolchain** (`go list -f {{.ImportPath}}`)
rather than against a re-typed constant, composes `<resolved>.<var>` and requires it to equal
`buildidentity.RevisionLdflagPath`; it also pins the split halves against
`buildidentity.RevisionVarName`.

Deliberate properties, each one Rule-15/Rule-13 material:

- **not-found is a finding.** A missing `tested_bin_identity_var=` / `…_pkg=` assignment, a
  missing composition site (`"-X $tested_bin_pkg.$tested_bin_identity_var=`), and an **empty**
  import path are each reported — the gate cannot pass by scanning nothing.
- **the fact comes from the toolchain**, not from a copied constant: `go list` is asked what the
  script's spec resolves to, so a package move is caught by the resolution rather than by a
  string compare of the spec.
- **it lives where the script's other gates live** (`internal/testutil`, which already owns this
  script's start-record, run-condition, no-events and concurrent-stream gates). The script path
  comes from the existing `budgetScriptPath` constant (Rule 1/13). `internal/buildidentity` stays
  a **leaf**: this package's non-test code already imports it (`ldflags.go`) and it imports
  nothing — no new package, no new dependency (Rule 35).
- **no new script, no new resident, no load** (Rule 27/35). The gate is a `go test` arm; the
  fixture builds are `go list` on a package spec.

**Red proof B** (organic mutation of the real recipe — the shape the gate exists to catch),
verbatim in `evidence/E3-redproof-testbudget-operand-gate.txt`:

```
$ sed -i 's|^tested_bin_identity_var="Revision"$|tested_bin_identity_var="Revisions"|' scripts/test-budget.sh
$ go test ./internal/testutil/ -run TestBudgetRecipeOperandAgreesWithBuildIdentity
    testbudget_identity_operand_gate_test.go:197: scripts/test-budget.sh: the composed operand
      github.com/nick/gopi/internal/version.Revisions is not buildidentity.RevisionLdflagPath
      (github.com/nick/gopi/internal/version.Revision; …) — the shell recipe and the Go readers
      would disagree about which variable carries the revision … a stale spelling stamps NOTHING
      at exit 0 with empty stderr
--- FAIL: TestBudgetRecipeOperandAgreesWithBuildIdentity (0.01s)

$ git checkout HEAD -- scripts/test-budget.sh
$ test "$(git hash-object scripts/test-budget.sh)" = "$(git rev-parse HEAD:scripts/test-budget.sh)" && echo RESTORED-IDENTICAL
RESTORED-IDENTICAL            # content identity against a REVISION, not against the index
$ go test ./internal/testutil/ -run TestBudgetRecipeOperandAgreesWithBuildIdentity
--- PASS
```

The second test (`TestBudgetIdentityOperandGateIsFalsifiable`) drives the **same** predicate with
four mutations — variable rename, package move, composition line removed, empty resolution — and
refuses to run vacuously: it first requires the REAL script against the REAL resolution to be
clean, and it fails if a mutation anchor rots.

## 3. What is NOT closed (honest)

- **`devdoc/requirements/*.md`, `devdoc/*.md`, `briefings/*.md` and `man/gopi-version.md`** spell
  the operand and the keys in prose recipes (e.g. `man/gopi-version.md:29`,
  `devdoc/binary-identity-on-a-worktree-ground.md:125`). Those are documents, not producers: they
  are not executed by any machinery, and a gate over prose was judged worse than the small gap
  (documented recipes drift at human pace; the executable recipe is the one that silently stamps
  nothing). Not fixed here on purpose.
- **`internal/version/module_path_agreement_test.go`'s `revisionVarName = "Revision"`** is a
  second spelling of the variable half — in a **test**, deliberately: it is what holds the
  constant against the declaration. It is not a producer of the value.
- **The source-scan `sed`-based check performs no such comparison**; nothing else on this host
  produces a stamped artifact from a checked-in recipe besides `scripts/test-budget.sh` and the
  Go helpers (`internal/testutil.LdflagsFlag`), both of which now route through
  `buildidentity.RevisionLdflagPath`.
- **A `-X` operand whose variable half is CORRECT but whose package path resolves through a
  vendor/`replace` mapping** is not exercised by this gate: the resolution is asked of `go list`
  in this module, and a second module path is out of scope for a single-repo recipe.

## 4. Verification

```
$ go build ./...                                        # BUILD OK
$ go test ./internal/buildidentity/ -v -count=1         # PASS (both tests)
$ go test ./internal/testutil/ -run 'TestBudgetRecipeOperandAgreesWithBuildIdentity|TestBudgetIdentityOperandGateIsFalsifiable' -v -count=1
                                                        # PASS (both)
```

**End-to-end — the recipe's OWN operand, executed** (`evidence/E4-recipe-operand-end-to-end.txt`).
The three steps the script composes were run by hand against this tree, and the artifact was then
queried for what it carries:

```
$ go list -f '{{.ImportPath}}' ./internal/version
github.com/nick/gopi/internal/version
$ CGO_ENABLED=0 go build -ldflags "-X github.com/nick/gopi/internal/version.Revision=$(git rev-parse HEAD)" \
      -o ~/.gopi/sessions/01a14197-…/evidence/artifact-from-the-recipe-operand ./cmd/agent/      # exit=0
$ go version -m <artifact> | grep -E 'ldflags|vcs'
	build	-ldflags="-X github.com/nick/gopi/internal/version.Revision=dbb2d1eff70d1af3fd74e57180db0ec2e1c601cf"
$ <artifact> version | grep -E '^revision|^revision-source'
revision: dbb2d1eff70d1af3fd74e57180db0ec2e1c601cf
revision-source: self-declared by the build recipe (-ldflags github.com/nick/gopi/internal/version.Revision) — NOT attested by the toolchain, which writes vcs.revision only in a checkout it recognizes (a linked worktree gets none)
```

So the operand this gate now holds is the operand that **actually stamps** the tested binary, and
the artifact reads its revision back through the same constant the Go consumers use. The artifact
was written outside the worktree (`git status --porcelain` stayed empty through the build).

`internal/stylegate` is RED on this tree and **pre-existing**: every flagged site is a file this
change does not touch (the new file appears zero times in its report: `go test ./internal/stylegate/
-count=1 | grep -c testbudget_identity_operand_gate_test.go` → `0`). It was not "fixed" here — a red
in someone else's package is not this run's to repaint.

**The branch-scoped suite** (`make test-budget PKGS='./internal/testutil ./internal/buildidentity ./internal/version'`,
174s, 378 tests, stream `run-1792126866-4051036.json`): `internal/buildidentity` and
`internal/version` PASS. `internal/testutil` reports **3 failures, none of them this change's** —
the worker-ownership trio (`TestWorkerOwnershipPointerPresent`,
`TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds`,
`TestWorkerOwnershipSlashPairDocIsNotAnOffender`), all on the single file
`devdoc/resume-faces-cross-guidance.md`, which is tracked, unmodified, byte-identical to `HEAD`
(`git hash-object` = `git rev-parse HEAD:…`) and landed by `e63f3730d` — a pre-existing red on
master. Per Rule 41 it was **filed, not repaired**: task `01a14318-d0c0-7996-96d1-da8828d26fb7`
(`--tags bug`), evidence in `evidence/E5-pre-existing-failures.txt`. The two tests added here pass
in the same run.
