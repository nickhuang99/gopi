<!-- doc-class: historical -->
# The retired-verb gate asks WHICH PROGRAM before it reads argv[0]

Task `01a16db7-49a4-764b-9c2f-2a9936b12d92`. Direct arm of the gate in
`cmd/agent/retired_verb_test_driver_gate_test.go`; the widened (helper) arm's
own program resolution is `devdoc/retired-verb-gate-helper-reach.md`.

## The defect, stated as a fact about the predicate

`retiredVerbDriveGateScanFile` read a program start's FIRST ARGUMENT and
compared it to the retired vocabulary. It never asked **which program** was
being started, so a call whose program was `git` or `go` was reported as

    the real binary is driven with the RETIRED word "log" as argv[0]

— git's own subcommand read as a gopi verb. The helper arm had already been
fixed this way (`retiredVerbGateProgramIsSpelled`, memory #13035); the direct
arm had not, so the same class of false positive survived on the other side of
the gate.

## The fix, as a principle rather than a spelling

Resolve the PROGRAM expression statically, then decide:

* **known, and not a gopi spelling** → the call is not a drive of this binary:
  skip it entirely (no argv[0] finding AND no family-shape finding). The
  measured population is large — 532 program starts over 30 distinct program
  words on this tree.
* **known, and a gopi spelling** (`gopi`, `gopiProgramWord`, `./bin/gopi`) →
  gate it exactly as before.
* **unknown** (`bin`, `s.bin`, `testutil.GopiBin(t)`, `filepath.Join(…)`) →
  gate it exactly as before. **This default direction is the whole safety of the
  fix**: every driver of the real binary in this repo reaches it through a
  variable, so a predicate that read "unknown" as "not gopi" would exempt
  precisely the calls the gate exists to catch.

Resolution reuses what the package already owns (Rule 13) — no second resolver:
`retiredVerbDriveGateLiteralWord` for a literal or a this-package verb constant,
and `retiredVerbGateStringLiterals` for the package-level `NAME = "literal"`
rows (the shape the repo actually uses for a tool's program name:
`const cwdProbeGitBinary = "git"`, `const systemctlPath = "/usr/bin/systemctl"`).
One addition is a FILE-LOCAL binding map, because a local `bin := "go"`
shadows an identically-named package row and the local value is the one the
call actually uses.

## Why the allow-list, not a deny-list

`retiredVerbDriveGateSelfProgramNames` names the spellings that mean THIS
binary, projected from their one definition each (`gopiProgramWord` →
`cliverbs.Program`; `testutil.BinFileName`; `"bin" + "/" + "gopi"`). A new
third-party program must never need a row: a deny-list of tools would rot the
day somebody execs something new, and its rot direction is a false positive
coming back.

## How the fix is proved two-sided

`retiredVerbDriveGateScanCensus(…, readsProgram bool)` — the flag is a
PARAMETER, not a mutated source line, so the pre-fix and shipped predicates run
over the SAME corpus, in the SAME process, at the SAME revision. That is what
makes the two finding counts comparable, and it is why the red proof carries no
snapshot/restore step at all: there is nothing to restore.

* fixture proof (8 shapes): `git log` / `git show`, the same program through a
  package constant and through a file-local binding, all quiet after the fix
  and red before it — beside three POSITIVE CONTROLS (`exec.Command("gopi","log")`,
  the same through `gopiProgramWord`, and the unresolvable `bin`) that stay red
  in BOTH settings. Without those the fix would be a false-negative machine.
* corpus proof: retiring `version` (git's and go's own subcommand) is reported
  by the pre-fix predicate at 2 real sites and by the shipped one at 0; the
  census prints the 30 exempted program names so an exemption that matched
  NOTHING fails instead of reading green.

Measured when reverting the shipped setting to `false`
(`const retiredVerbDriveGateReadsTheProgram = false`): the corpus proof goes
RED with `0 distinct program(s) (floor 10)` — the fix is load-bearing, and the
flip costs one token.

## The honest limit

The direct arm reports 0 findings on the real tree BOTH before and after this
change, so the false positive this fix removes is **latent** on this tree as it
stands (no currently-retired word is handed to a non-gopi program anywhere in
it) — the fixture column and the `version` probe are what measure it. Becoming
live costs nothing but a retirement: the memory-recorded shape (#13070: a git
helper read as a gopi driver, 17 findings) is what the helper arm hit, and the
direct arm was one retired word away from the same. A gate that only breaks the
day a word is retired is exactly the gate you cannot afford to distrust then.
