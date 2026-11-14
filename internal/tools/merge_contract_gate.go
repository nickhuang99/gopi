package tools

// merge_contract_gate.go — the MERGE-TIME CLI contract gate. The 2026-09-12
// voice_input incident (bd3d34a5) landed `|| isVoiceSubcommand(dispatchArgs)`
// on master without the matching cliContractDispatchPredicates entry, so
// TestCLIContractInventoryMatchesDispatch went red ON MASTER. The drift guard
// exists but fires only in `go test`, and at the time nothing ran it on the
// merge decision path (the chain the incident briefings named then was
// MergerRepoSweep → TaskMerge → taskMergeOne). This file closes that hole ON the
// merge decision path, token-free and deterministic — mergeContractGate is now
// asked by BOTH landing arms — the `mergeContractGate(repo, master, branch)`
// call in internal/tools/task_merge.go (the reachable `gopi task merge` face),
// and the call in repoSweepDecideBranch here (whose resident arm has had no
// production caller since 2026-11-01, task 01a19466).
//
// MECHANISM (merger-side narrow check, chosen over "run the real inventory
// test at merge time" — see devdoc/merge-time-contract-gate-20260912.md for
// the measured cost argument): read the POST-MERGE content of the branch's
// main.go dispatch chain and its cli_contracts_test.go predicate mirror (git
// show, no checkout, no worktree mutation, no build, no test process, no LLM),
// compare them against master's own pair through internal/clicontract, and
// REFUSE the merge when the branch newly wires a predicate its contract suite
// does not track. The refusal happens BEFORE `git merge` ever runs, so master
// is byte-identical by construction — there is no merge state to abort.
//
// POST-MERGE CONTENT, NOT THE TIP (2026-09-13 fix): the gate judges the blob a
// three-way merge would KEEP per file (mergeContractGateFileWinner) — a branch
// that never touched cmd/agent merges MASTER's contract files, and comparing
// its stale fork copy instead refused a docs-only branch whose merged tree was
// perfectly consistent (and told the reworker to delete valid master entries).
// When both sides rewrote a file the gate ABSTAINS: that file is a merge
// conflict, and resolving it is the conflict lane's job, not the gate's.
//
// DELIBERATE LIMITS (honest scope, never overclaimed): the gate catches ONE
// failure mode — main.go's dispatch chain and the contract-suite mirror
// drifting apart IN THE TREE THE MERGE WOULD PRODUCE. It does NOT compile, does
// NOT run any test, and does NOT verify inventory fingerprints or cliVerbs
// coverage. Everything else needs the full build/test gate that the merger
// deliberately does not run. A missing/moved file or a refactored chain
// ABSTAINS (merge proceeds, the abstention is logged) — the gate must never be
// able to block every merge forever on a shape change.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/clicontract"
)

const (
	// mergeContractGateActionRefused is the per-branch outcome when the gate
	// refuses a branch (master untouched, branch intact). It is a distinct
	// action so the sweep report distinguishes "refused for contract drift"
	// from "conflict" — the two need different fixes (a code edit vs a merge
	// resolution).
	mergeContractGateActionRefused = "contract-refused"

	// mergeContractGateRefuseDetailFmt renders the per-branch refusal line.
	// It names the branch AND the target so the report is actionable without
	// re-deriving git state, and it says what the executor must do.
	mergeContractGateRefuseDetailFmt = "CLI contract drift — %s (branch %s, target %s, master unchanged)"

	// mergeContractGateRevisionSep separates rev and path in the
	// `<rev>:<path>` object spec (git's own object-spec syntax).
	mergeContractGateRevisionSep = ":"

	// mergeContractGateMergeBaseVerb is the git subcommand that resolves the
	// third revision the winner rule needs (the branch's fork point).
	mergeContractGateMergeBaseVerb = "merge-base"

	// mergeContractGateCatFileVerb / mergeContractGateBatchFlag assemble the
	// batch object reader: `git cat-file --batch` takes `<rev>:<path>` specs
	// on stdin and streams every object back in ONE subprocess.
	mergeContractGateCatFileVerb = "cat-file"
	mergeContractGateBatchFlag   = "--batch"

	// mergeContractGateLineSep is the batch spec separator (and the newline
	// stripped after each object's payload).
	mergeContractGateLineSep = "\n"

	// mergeContractGateMissingSuffix is the token `cat-file --batch` prints
	// for a spec it cannot resolve ("<spec> missing").
	mergeContractGateMissingSuffix = "missing"

	// mergeContractGateDisabledValue is the GOPI_MERGE_CONTRACT_GATE value
	// that turns the gate off (operator escape hatch: an unforeseen
	// false-positive must never be able to block every merge with no exit).
	mergeContractGateDisabledValue = "0"

	// mergeContractGateEnvName names the operator escape hatch. Rule 2: the
	// default is the constant above, and this is the ONE reader.
	mergeContractGateEnvName = "GOPI_MERGE_CONTRACT_GATE"

	// mergeContractGateFilesPerRevision is how many FILES the winner rule
	// consults: main.go (the wired predicates) and cli_contracts_test.go (the
	// mirror AND the token map — one file, two anchors, so still ONE read).
	// Kept named because the cost model — and the test that pins it — is
	// expressed in terms of it.
	mergeContractGateFilesPerRevision = 2

	// mergeContractGateRevisions is how many revisions the winner rule
	// consults per file: the merge-base, the merge target, and the branch tip.
	// A 3-way merge keeps the branch's blob only when the branch actually
	// CHANGED it since the base; otherwise the target's blob survives. Reading
	// all three is what lets the gate judge the POST-MERGE content instead of
	// the tip (see mergeContractGateFileWinner). Files × revisions is the blob
	// count the single batch process must carry — pinned by the cost test.
	mergeContractGateRevisions = 3

	// mergeContractGateBatchCalls is how many `git cat-file --batch` processes
	// carry EVERY blob the winner rule needs. ONE: the winner rule reads six
	// blobs (2 files × 3 revisions), and MEASURED on this machine six separate
	// `git show` processes cost ~34 ms against ~4 ms for one batch carrying the
	// same six objects (a single `git show` of a small blob is ~5.6 ms). The
	// batch is therefore what keeps the correctness fix from roughly tripling
	// the gate's wall cost.
	mergeContractGateBatchCalls = 1

	// mergeContractGateBaseRefCalls is the ONE other subprocess the gate
	// spawns: `git merge-base`, which names the third revision the winner rule
	// needs. It cannot ride the batch pipe (a different subcommand) and it is
	// the gate's single most expensive call — measured 21.5 ms on a pair 831
	// commits divergent — i.e. that call IS the price of judging the post-merge
	// tree instead of the tip.
	mergeContractGateBaseRefCalls = 1

	// mergeContractGateSubprocessBudget is the gate's TOTAL subprocess budget
	// per branch, derived from the two constants above so the number is stated
	// ONCE. TestMergeContractGate_CostIsBounded fails if the gate ever exceeds
	// it — no checkout, no build, no test process, no LLM.
	mergeContractGateSubprocessBudget = mergeContractGateBatchCalls + mergeContractGateBaseRefCalls

	// mergeContractGatePresenceProbeBytes bounds the head probe that decides
	// whether the contract suite still declares the dispatcher token map. A
	// declaration way below the file's parse anchors would be a moved file,
	// not a documented one, so the probe only has to look far enough.
	mergeContractGatePresenceProbeBytes = 65536
)

// headOf truncates a source to the presence-probe window (the anchor is
// declared in the file's first few kilobytes in every real revision).
func headOf(src string) string {
	if len(src) <= mergeContractGatePresenceProbeBytes {
		return src
	}
	return src[:mergeContractGatePresenceProbeBytes]
}

// mergeContractGateVerdict is the gate's decision for one branch.
type mergeContractGateVerdict struct {
	// Refused is true when the branch must not be merged (contract drift).
	Refused bool
	// Detail is the refusal reason ("" unless Refused).
	Detail string
}

// mergeContractGateBlobReader is the batch object reader the winner rule and
// the target pair share: one `git cat-file --batch` process answers every
// `<rev>:<path>` spec on stdin. It exists because the winner rule needs six
// blobs (2 contract files × base/tip/target) and subprocesses are the cost:
// measured here, six separate `git show` processes take ~34 ms while one batch
// carrying the same six objects takes ~4 ms (a single `git show` of a small
// blob is ~5.6 ms).
//
// A reader is per-call (not shared/swappable) so a nested read can never read
// from a stale cursor — the batch protocol is strictly sequential: send spec,
// read one "<spec> <type> <size>" header, read size bytes, read one newline.
type mergeContractGateBlobReader struct {
	// ask delivers one spec and returns that object's bytes.
	ask func(spec string) (string, error)
	// stop ends the batch process. Safe to call more than once.
	stop func()
}

// mergeContractGateReadBlobs starts one batch reader for repo. The caller MUST
// defer stop (the process would otherwise leak for the whole merger tick).
func mergeContractGateReadBlobs(repo string) (*mergeContractGateBlobReader, error) {
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	cmd := exec.CommandContext(ctx, "git", "-C", repo, mergeContractGateCatFileVerb, mergeContractGateBatchFlag)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cat-file stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cat-file stdout: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("cat-file start: %w", err)
	}
	buf := bufio.NewReader(stdout)
	r := &mergeContractGateBlobReader{
		ask: func(spec string) (string, error) {
			if _, err := fmt.Fprintf(stdin, "%s%s", spec, mergeContractGateLineSep); err != nil {
				return "", fmt.Errorf("write spec %s: %w", spec, err)
			}
			header, err := buf.ReadString(mergeContractGateLineSep[0])
			if err != nil {
				return "", fmt.Errorf("read header for %s: %w", spec, err)
			}
			header = strings.TrimSpace(header)
			// "<spec> missing" for an unresolvable spec; never a payload.
			if strings.HasSuffix(header, " "+mergeContractGateMissingSuffix) {
				return "", fmt.Errorf("object %s does not exist", spec)
			}
			fields := strings.Fields(header)
			if len(fields) != 3 {
				return "", fmt.Errorf("unexpected cat-file header %q for %s", header, spec)
			}
			size, err := strconv.Atoi(fields[2])
			if err != nil {
				return "", fmt.Errorf("cat-file size %q for %s: %w", fields[2], spec, err)
			}
			payload := make([]byte, size)
			if _, err := io.ReadFull(buf, payload); err != nil {
				return "", fmt.Errorf("read payload for %s: %w", spec, err)
			}
			if _, err := buf.ReadByte(); err != nil { // the newline closing the object
				return "", fmt.Errorf("read terminator for %s: %w", spec, err)
			}
			return string(payload), nil
		},
		stop: func() {
			stdin.Close() //stylegate:ignore batch stdin closed after the last request; the exit status that matters is cmd.Wait()'s on the next line, and a Close error here cannot fail the read
			waitErr := cmd.Wait()
			cancel()
			// A non-zero wait is only surprising when the batch produced no
			// payload at all; a killed context after the last read is routine.
			if waitErr != nil && stderr.Len() > 0 {
				taskNotifyLog("merger: CLI contract gate batch reader exited: %v (%s)", waitErr, strings.TrimSpace(stderr.String()))
			}
		},
	}
	return r, nil
}

// mergeContractGate decides whether branch may land on master by comparing the
// CLI contract pair the MERGE WOULD PRODUCE against master's pair. It NEVER
// writes: the comparison is object text only, so a refusal leaves master and
// the worktree byte-identical.
//
// Fail-open by design (the merger's design contract: DETECT, never block on
// an unreadable input): an unreadable file, a refactored chain, a both-sides
// rewrite (a real conflict), or a disabled gate returns the zero verdict — the
// merge proceeds exactly as before this gate existed. Only a POSITIVE drift
// finding (the branch newly wires a predicate its contract suite does not
// track) refuses.
func mergeContractGate(repo, master, branch string) mergeContractGateVerdict {
	if mergeContractGateDisabled() {
		return mergeContractGateVerdict{}
	}
	reader, err := mergeContractGateReadBlobs(repo)
	if err != nil {
		taskNotifyLog("merger: CLI contract gate ABSTAINS for %s (object reader unavailable: %v) — merging unchanged", branch, err)
		return mergeContractGateVerdict{}
	}
	defer reader.stop()
	base, err := repoSweepGit(repo, mergeContractGateMergeBaseVerb, master, branch)
	if err != nil {
		taskNotifyLog("merger: CLI contract gate ABSTAINS for %s (merge-base with %s unresolved: %v) — merging unchanged", branch, master, err)
		return mergeContractGateVerdict{}
	}
	spec := func(rev, path string) string { return rev + mergeContractGateRevisionSep + path }
	// The tip is judged on what the MERGE would keep, not on the branch's own
	// bytes: a branch that never touched cmd/agent merges MASTER's contract
	// files, so comparing the branch's stale fork copy against master is a
	// comparison of two blobs that will never coexist in one tree.
	tip, err := mergeContractGatePair(func(path string) (string, error) {
		return mergeContractGateFileWinner(path, spec(base, path), spec(master, path), spec(branch, path), reader.ask)
	})
	if err != nil {
		taskNotifyLog("merger: CLI contract gate ABSTAINS for %s (post-merge contract unreadable: %v) — merging unchanged", branch, err)
		return mergeContractGateVerdict{}
	}
	target, err := mergeContractGatePair(func(path string) (string, error) {
		return reader.ask(spec(master, path))
	})
	if err != nil {
		taskNotifyLog("merger: CLI contract gate ABSTAINS for %s (target %s unreadable: %v) — merging unchanged", branch, master, err)
		return mergeContractGateVerdict{}
	}
	drift := clicontract.Compare(tip.chain, tip.mirror, target.chain, target.mirror, tip.tokens)
	if drift.Empty() {
		return mergeContractGateVerdict{}
	}
	return mergeContractGateVerdict{
		Refused: true,
		Detail:  fmt.Sprintf(mergeContractGateRefuseDetailFmt, drift.Detail(), branch, master),
	}
}

// mergeContractGateFileWinner returns the blob a three-way merge of master and
// branch would KEEP for a contract file — the content the gate must judge. It
// takes the three object SPECS (not revisions) because the caller's reader is
// a sequential batch pipe: resolving a spec is that reader's job.
//
// WHY: the gate's first version compared the branch TIP's blobs against
// master's. On a branch that never touched cmd/agent (the 2026-09-13
// tab-order-POC refusal: forked 831 commits ago, docs-only since) that asks
// the wrong question twice over — the tip's chain is the FORK's chain and its
// mirror the fork's mirror, so master's own newer predicates surface as
// `TrackedUnwired` drift even though the merge takes MASTER's files and lands
// a perfectly consistent pair. The refusal then instructed the reworker to
// delete five valid master mirror entries, which is both a no-op on that drift
// arm and destructive on the merged tree.
//
// The rule is git's own 3-way rule, applied per file: an unchanged branch side
// yields the target's blob; a one-sided change yields the changed blob; a
// both-sides change is a REAL conflict, whose resolution belongs to the
// merger's conflict lane — the gate ABSTAINS (its design contract: DETECT,
// never block on ambiguity).
func mergeContractGateFileWinner(path, baseSpec, masterSpec, branchSpec string, read func(spec string) (string, error)) (string, error) {
	baseSrc, err := read(baseSpec)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", baseSpec, err)
	}
	branchSrc, err := read(branchSpec)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", branchSpec, err)
	}
	if branchSrc == baseSrc {
		return read(masterSpec)
	}
	masterSrc, err := read(masterSpec)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", masterSpec, err)
	}
	if masterSrc == baseSrc || masterSrc == branchSrc {
		return branchSrc, nil
	}
	// Both sides rewrote the file since the base: the two blobs cannot both
	// survive, so no contract pair derived from them is a prediction of the
	// merged tree.
	return "", fmt.Errorf("%s changed on BOTH sides of the merge-base — a merge conflict decides this file, not the gate", path)
}

// mergeContractPair is one post-merge revision's parsed contract surface.
type mergeContractPair struct {
	// chain is the predicate set that revision's main.go wires.
	chain []string
	// mirror is the predicate set its cliContractDispatchPredicates tracks.
	mirror []string
	// tokens is the predicate set its dispatcherOfToken VALUES route a
	// contract case to (empty when the map is absent/unparsable — see below).
	tokens []string
}

// mergeContractGatePair reads and parses one post-merge contract pair through
// the supplied reader (the winner tree, or the merge target).
func mergeContractGatePair(read func(path string) (string, error)) (mergeContractPair, error) {
	// Read the CONTRACT SUITE first: it carries both the mirror and the token
	// map, and an absent anchor must ABSTAIN (never refuse) — so it is the
	// first question, not the last. main.go's chain comes last because a
	// missing chain is itself an abstention.
	testSrc, err := read(clicontract.ContractSuiteFile)
	if err != nil {
		return mergeContractPair{}, fmt.Errorf("read %s: %w", clicontract.ContractSuiteFile, err)
	}
	// A revision whose suite no longer declares the dispatcher token map is a
	// project this gate does not understand — ABSTAIN (merge proceeds,
	// logged), exactly as for a moved file. The 2026-09-12 live demonstration
	// is why this is an abstention and not a refusal: the first version
	// silently merged a half-fixed branch, and the fix must not swing the gate
	// into refusing every branch of a repo that dropped the map.
	if !strings.Contains(headOf(testSrc), clicontract.DispatcherTokenMapAnchor) {
		return mergeContractPair{}, fmt.Errorf(
			"%s carries no %s declaration — contract-case coverage is unverifiable (gate shape changed?)",
			clicontract.ContractSuiteFile, clicontract.DispatcherTokenMapAnchor)
	}
	mirror, err := clicontract.ParseContractMirror(testSrc)
	if err != nil {
		return mergeContractPair{}, err
	}
	// A present-but-unparsable token map yields NO token coverage (empty, no
	// error): the gate then judges the branch on chain+mirror alone, which can
	// only make it refuse MORE — never silently bless a predicate it cannot see
	// a contract case for.
	tokens, tokErr := clicontract.ParseDispatcherTokenMap(testSrc)
	if tokErr != nil {
		tokens = nil
	}
	mainSrc, err := read(clicontract.DispatchChainFile)
	if err != nil {
		return mergeContractPair{}, fmt.Errorf("read %s: %w", clicontract.DispatchChainFile, err)
	}
	chain, err := clicontract.ParseDispatchChain(mainSrc)
	if err != nil {
		return mergeContractPair{}, err
	}
	return mergeContractPair{chain: chain, mirror: mirror, tokens: tokens}, nil
}

// mergeContractGateDisabled reports whether the operator escape hatch is set.
// Rule 2: this is the ONE reader of mergeContractGateEnvName; the default
// (unset / any other value) is ENABLED.
func mergeContractGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(mergeContractGateEnvName)) == mergeContractGateDisabledValue
}
