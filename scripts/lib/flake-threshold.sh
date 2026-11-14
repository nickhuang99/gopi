# shellcheck shell=bash
# flake-threshold.sh — the ONE definition of the order/determinism flake class's
# shared values (GOPI.md Rule 2/14: a value used by several scripts lives in one
# small leaf file every consumer sources; a second copy is drift, a bare literal
# is a defect). Sourced by scripts/flake-catch.sh and asserted against the Go
# side (internal/flaketest) by internal/standards.
#
# WHY THIS FILE EXISTS
# --------------------
# A flake cannot be found by reading durations (that is the heavy class,
# scripts/lib/heavy-threshold.sh). Its signature is a verdict that MOVES across
# repetitions of the SAME code on the SAME host, and the instrument is therefore
# `go test -count=N` on the test itself — never device load, never a pressure
# generator (GOPI.md Rule 27 is absolute: measure the test, not the host).
#
# The registry (testdata/flakes.tsv) records what has already been root-caused;
# this file's values are what the CATCH mechanism (scripts/flake-catch.sh) and
# the standards gate must agree on, so the registry and the runner cannot drift.

# flake_registry_rel — the checked-in registry, repo-relative. Its Go-side single
# source is flaketest.RegistryRel; the gate asserts the two spell the same path,
# because a renamed registry would leave the gate reading an empty file and
# reporting a clean green.
flake_registry_rel="testdata/flakes.tsv"

# flake_catch_script — the ONE catch mechanism, repo-relative.
flake_catch_script="scripts/flake-catch.sh"

# flake_reps_env — the ONE name of the repetition opt-in; its Go-side single
# source is flaketest.RepsEnv. It says HOW MANY times a scope is re-run, because
# repetition IS the instrument: one run measures nothing.
flake_reps_env="GOPI_FLAKE_REPS"

# flake_default_reps — repetitions when the operator does not say. Wide enough to
# catch a frequently-moving verdict, clearly too narrow for a 1-in-100 one — the
# script PRINTS the rate it observed and tells the reader to raise -reps, so the
# default never reads as a proof of stability.
flake_default_reps=10

# flake_min_reps / flake_max_reps — the band. Below the floor a run cannot tell a
# flake from a pass (a single run proves nothing: that is this whole class's
# signature); above the ceiling the mechanism stops being a mechanism and becomes
# a load generator, which Rule 27 forbids absolutely.
flake_min_reps=2
flake_max_reps=100

# flake_scope_default — the widest scope the catch mechanism will run without
# being told otherwise: ONE package, never the whole tree. A whole-tree
# repetition run is a merge-path-sized cost; the point of this mechanism is to
# aim it at the suspect scope named by a registry row.
flake_scope_default="./internal/tools/"

# flake_verdict_marker — prefixes each NAMED unstable test in the report. The
# standards gate and a human reader match this same text (one definition).
flake_verdict_marker="FLAKY"

# flake_timeout_default — the `go test -timeout` each repetition run gets. Only a
# safety net against a hung test (a machine wedged mid-run must not hold the
# opt-in forever); it is NOT a performance threshold and nothing is classified by
# it — a duration threshold on a load-varying host is a lead, not a verdict.
flake_timeout_default="300s"

# flake_stream_sub — the scratch-queue sub-directory each catch run writes its raw
# `go test -json` stream into (GOPI.md Rule 22: a large throwaway artifact lives in
# the scratch tree, off the `/` partition and age-GC'd — never an ambient temp path).
flake_stream_sub="flake-catch"

# flake_gate_packages — package path fragments the mechanism may never be pointed
# at AS A CLASSIFICATION (/never heavy/), kept here so the runner's help and the
# Go gate name the same set. NOTE the difference from the heavy class: a flake is
# not a heaviness classification, so a registry row for a test in a gate package
# would be a DEFECT TO FIX (the gates must run on the smoke path), never a reason
# to skip it. The standards gate rejects registered rows in these packages.
flake_gate_packages="internal/standards internal/stylegate"

# flake_scope_budget_pkgs — the ONE Makefile variable a caller uses to aim the
# mechanism (`make flake-catch PKGS='-reps 20 ./cmd/agent/'`). Named here so the
# Makefile target and the standards gate agree on the seam instead of typing it.
flake_scope_var="PKGS"

# ---- the red-proof probe (scripts/flake-gate-probe.sh + internal/standards) ----
#
# The gate's claim is BEHAVIOURAL ("the mechanism NAMES a new order-dependent
# flake"), so it cannot be grepped: the gate builds a throwaway module with a
# PLANTED map-iteration-order test, runs the real mechanism over it, and asserts
# the plant is named. These are the shared names of that probe — one definition
# here, read by the driver AND asserted by the gate (Rule 14: the fixture's
# identity lives in one place, so the gate can never assert a name the fixture
# does not carry).

# flake_probe_dir — the throwaway module/package the probe builds.
flake_probe_dir="orderfixture"

# flake_probe_plant_test — the PLANT: it takes the "first" key of a Go map and
# asserts it is the one key the contract names. Go randomizes map iteration
# order, so the same code on the same host passes and fails across repetitions.
flake_probe_plant_test="TestOrderFixtureMapIterationDependent"

# flake_probe_clean_test — the same fixture with the order dependence removed
# (the contract asks the map for the key by name). The negative control: the
# mechanism must name NOBODY here, or its naming means nothing.
flake_probe_clean_test="TestOrderFixtureOrderIsPinned"

# flake_probe_reps — repetitions the probe uses. The plant makes three independent
# map-order assertions whose per-repetition pass probability is ~0.47 (measured:
# 0.775 per 3-key map, cubed), i.e. the fixture is a coin flip, and a repetition
# count is what turns a coin flip into evidence. At 20 repetitions BOTH ways the
# probe can lie are ~1e-6 or smaller: a false "no flake" is 0.47^20 and a false
# "plain bug" (every repetition failing) is 0.53^20. Those are the two reading
# errors that matter here — the mechanism's exit 1 means "the verdict MOVED" and
# its exit 3 means "it fails every time, a plain defect with a different remedy".
#
# MEASURED HISTORY, so this number is not re-tuned blindly: the first probe had ONE
# assertion at 6 repetitions (p_pass = 0.875 per repetition on a 2-key map), which
# made the gate report a false green in 2 of 8 runs; the second had TEN assertions
# at 10 repetitions, which made it report the WRONG verdict (exit 3, "fails every
# repetition") in 10 of 12 runs. Both were fixed at the design level — p ~= 1/2 and
# enough repetitions — not by loosening the assertion. The verdict LOGIC is checked
# separately and deterministically by the -replay cases, which involve no map order.
flake_probe_reps=20

# flake_gate_probe_script — the driver itself, repo-relative (single definition;
# its Go-side constant is flaketest.GateProbeScriptRel).
flake_gate_probe_script="scripts/flake-gate-probe.sh"

# flake_passthrough_value_flags — the SELECT flags the runner forwards to
# `go test` that TAKE A VALUE, so the runner can keep the flag and its value
# together instead of mistaking the value for the scope (measured: `-reps 3
# -run TestX ./internal/tools/` once ran `go test … -run` with no test name and
# reported a setup failure as "nothing was measured"). A single-token flag such
# as -short needs no entry; an unlisted one is forwarded alone, which is the
# honest default (the runner never guesses what a flag's value looks like).
flake_passthrough_value_flags="-run -skip -bench -cpu -shuffle"

# flake_replay_cases — the two synthetic-verdict scenarios the runner can replay
# WITHOUT running go test (`-replay <case>`), so a gate can check the VERDICT
# LOGIC deterministically. The plant proves the mechanism sees real map-order
# flakiness; it cannot prove what the report does with an always-failing test, a
# verdict that moves, a moving SUBTEST (which must not be named — its re-run
# scope is its parent, and a registry row names a top-level test) or a scope that
# measured nothing (which must never read as a clean green). A probabilistic
# probe cannot check those, and a gate that only checks the probabilistic half is
# a gate that fires on its own coin flip.
flake_replay_cases="all nothing"

# flake_replay_pkg — the synthetic package the replay events claim, so a replayed
# stream is recognisable in a report and can never be mistaken for a real run.
flake_replay_pkg="github.com/nickhuang99/gopi/internal/orderfixture"

# flake_replay_verdicts — test:verdict pairs for the `all` case. `pass` verifies
# quiet, `fail` verifies the plain-defect reading (exit 3), `moved` verifies the
# flake reading (exit 1), `sub` verifies that a moving SUBTEST is not named.
flake_replay_verdicts="TestReplayAlwaysPasses:pass TestReplayAlwaysFails:fail TestReplayVerdictMoves:moved TestReplaySubtestOnlyMoves:sub"

# flake_replay_expect — case:exit-code pairs. The EXPECTATION belongs next to the
# fixture that produces it (one definition), so a gate asserts behaviour rather
# than re-spelling what it believes the answer is: `all` moves a verdict (1) and
# `nothing` measured nothing (4 — never a clean green).
flake_replay_expect="all:1 nothing:4"

# flake_replay_reps — repetitions for a replayed stream. Small and fixed: a replay
# emits the verdicts itself, so the count only shapes the synthetic stream.
flake_replay_reps=4
