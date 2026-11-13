package tools

// rebuild_vet_gate.go — the POST-MERGE GATE RE-RUN: the close loop's second
// measurement.
//
// WHY IT EXISTS. The close loop's first measurement is the Rule-12 declared
// build (RebuildAfterMerge's own subject): "did master advance without the
// deployed binary following it". That build is `go build` — and `go build`
// NEVER COMPILES `_test.go` FILES, so a merge that lands a tree whose TEST
// trees do not compile is reported as a SUCCESSFUL round: the binary was
// rebuilt, the version follows the tip, the round looks green, and the very
// next `go test` in that repo fails on a file the merge introduced. The gate
// that reds on exactly that class is `go vet ./...` — the one command already
// in the repo's vocabulary that type-checks test trees while `go build` stays
// green (measured for this shape, 2026-09-16: `go build ./...` exits 0 while
// `go vet ./...` and `go test -run '^$' -count=1 ./...` both report the
// collision). So the close loop re-runs it and REPORTS the verdict.
//
// REPORT-ONLY, and deliberately so (Rule 12: a build failure is never a
// rollback — and neither is this). A red gate is a NOTIFY EVENT through the
// close loop's EXISTING channel (`BuildFailedEvent`, the same "build-failed"
// family the rebuild failure uses, so no live operator subscription has to
// change) plus a log line. It is never a merge refusal and never an unwind:
// the merge is done and good, git history is fine, and the round's own build
// succeeded — what is reported is that the LANDED TREE does not vet.
//
// SCOPE — the tree the round closes on, never a prepared merge result. This
// gate runs IN the repo with master checked out (no worktree, no alternate
// index, no archive extraction): a full `go vet ./...` over a checkout is
// minutes, and preparing a second tree for it would double that for nothing.
// The comparative question ("does the MERGE RESULT compile, unlike its
// side?") is already owned by the merge-TIME gates (merge_build_gate.go,
// merge_union_gate.go) and their answer is a refusal, which is a different
// decision from this one. This gate is the post-hoc measurement whose answer
// is "what did we just land, and does it still hold together".
//
// ABSTAIN IS THE DEFAULT (the merger's DETECT-never-block contract, learned
// the hard way by its sibling gates): a repo with no go.mod, a host with no
// Go toolchain, no usable build/module cache, a timeout, or an output that
// could not be resolved are all ABSTENTIONS with a stated reason — a branch
// is never reported red because the merger's host could not measure it.
//
// BOUNDED, because this runs on the merge/verify smoke path: ONE named
// timeout constant (vetGateDefaultTimeoutSec) with exactly ONE env reader
// (vetGateTimeout — Rule 2), the gate's own process group killed at the
// deadline, and the MERGER's own shared build cache reused rather than a new
// one (Rules 1a/2). No load is generated to make a number (Rule 27): the
// command run is the project's own gate, over the repo's own sources, with
// the budget of the build gate's order of work.
//
// ZERO new dependency, and it MUST stay that way: the finding is parsed with
// stdlib strings/substr machinery, never with a YAML/JSON vet reporter (that
// would be a new module in go.mod for one regex-shaped line — Rule 10.7).
//
// WHAT IS DELIBERATELY NOT HERE (honest scope, Rule 26 corollary):
//   - migrations are NOT run. "run the vet gate and any repo-declared
//     migrations" asked for a migration hook (a `migrations` entry in the
//     settings repos table, a bounded runner for it, and a story for
//     reporting one migration's failure). Nothing in the repo consumes or
//     declares such a thing today, so inventing the contract here would be a
//     guess dressed as a feature; the vet half is the half that is testable
//     against a planted red gate, and it is the half that landed.
//   - the ROLLBACK half of the close loop is owned by
//     serveRebuildAfterMerge/RestoreMaster (mechanism 2, a LANDING build
//     failure). This gate deliberately holds no revert handle: a red vet is
//     not an unbuildable master.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// VetFailedEvent is the notify event a red post-merge gate is reported through.
//
// It is DELIBERATELY the build-failed family's own name and not a new event:
// the close loop already publishes every "the binary/artifact did not follow
// master" alarm as BuildFailedEvent, operators subscribe to it by name in
// GOPI_TASK_NOTIFY_EVENTS, and a live merger must not have to be
// reconfigured for a new measurement to be heard. The payload's summary says
// WHICH measurement failed (gate=vet) so the reader still knows what happened.
const VetFailedEvent = BuildFailedEvent

const (
	// vetGateName is how the gate identifies itself in every message it emits
	// (the reported failure must be readable as a GATE, not as an anonymous
	// error): the notify payload and the log line both carry it.
	vetGateName = "vet"

	// vetGateToolchainBinary / vetGateToolchainVerb are the toolchain call
	// (Rule 1b: the binary name and the subcommand are spelled once).
	vetGateToolchainBinary = "go"
	vetGateToolchainVerb   = "vet"

	// vetGateLoopArgument is the package pattern: the whole module, from the
	// repo root. `./...` is what makes the gate a MODULE-wide claim rather
	// than a claim about one package.
	vetGateLoopArgument = "./..."

	// vetGateModFile is the marker that makes this measurement applicable at
	// all: a Go module root. A repo that is not one has no known way to be
	// vetted (the declared build is all the toolchain knowledge we have —
	// Rule 12's declared-not-discovered rule), so it ABSTAINS rather than
	// guessing a command.
	vetGateModFile = "go.mod"

	// vetGateDisabledEnvName names the operator escape hatch (Rule 2: the
	// constant is declared here, the ONE reader is vetGateDisabled). An
	// unforeseen false report must always be one env var away from gone.
	vetGateDisabledEnvName = "GOPI_POSTMERGE_VET_GATE"

	// vetGateDisabledValue turns the gate off.
	vetGateDisabledValue = "0"

	// vetGateTimeoutEnvName overrides the gate's budget in seconds (Rule 2:
	// ONE reader, vetGateTimeout). An unparsable value is an ABSTENTION with
	// the reason, never a silent fallback (Rule 15b).
	vetGateTimeoutEnvName = "GOPI_POSTMERGE_VET_TIMEOUT_SEC"

	// vetGateDefaultTimeoutSec bounds the vet subprocess when the operator
	// declares no budget. TWICE the build gate's own default
	// (mergeBuildGateDefaultTimeoutSec = 300s) on purpose: vet type-checks
	// every package's TEST tree too — compile input `go build` does not even
	// read — and the first pass after a cache trim compiles that wider set
	// from cold. Measured on this host (2026-09-16, warm shared cache):
	// `go vet ./...` over this repo = 21.3s; a cold one-package fixture = 18.4s,
	// warm = 0.6s.
	vetGateDefaultTimeoutSec = 600

	// vetGatePackageFailPrefix is how the toolchain marks a package whose vet
	// failed, at the head of a line: "FAIL\t<import path> [build failed]".
	// Counting these lines is what distinguishes a real red gate from an
	// abstention ("nothing was measured") — the two are reported apart, never
	// merged into one "not green" bucket.
	vetGatePackageFailPrefix = "FAIL\t"

	// vetGatePackageHeaderPrefix is the toolchain's own package banner
	// ("# <import path>"), which is never a diagnostic — the parser must not
	// read a package path as a file path.
	vetGatePackageHeaderPrefix = "# "

	// vetGatePackageOKPrefix is the toolchain's passing half of a package
	// verdict ("ok  \t<import path>"), which is what a measured PASS looks
	// like when the toolchain decides to print it.
	vetGatePackageOKPrefix = "ok  \t"

	// vetGateNoPackagesMarker is the toolchain's own announcement that the
	// package pattern matched nothing ("go: warning: \"./...\" matched no
	// packages" / "no packages to vet", measured with exit 1). An empty
	// module is NOT a red tree — it is a module with nothing to measure — so
	// it ABSTAINS rather than alarming every round in an empty repo.
	vetGateNoPackagesMarker = "matched no packages"

	// vetGateDiagnosticPrefix is the prefix `go vet` puts on its OWN
	// type-check diagnostics: "vet: <file>:<line>:<col>: <message>"
	// (measured for a test-tree type error). The prefix has to be dropped
	// before the file:line can be read, or the reference would name a file
	// called "vet:".
	vetGateDiagnosticPrefix = "vet: "

	// vetGateGoSourceSuffix is the file shape a diagnostic may name. A vet
	// finding always points at a Go source file; requiring the extension is
	// what keeps prose (and a message that merely contains a colon) out of
	// the findings.
	vetGateGoSourceSuffix = ".go"

	// vetGateFindingsMax caps the findings kept on the result (a repo-wide
	// red gate can produce thousands of lines; the reported reference is the
	// first one and the COUNT is what tells the operator the size). The cap is
	// on what is RETAINED — the count itself is measured over every line.
	vetGateFindingsMax = 8

	// vetGateOutputTailMax bounds the output tail kept when the gate failed
	// without a parseable diagnostic (a failure shape the parser does not
	// know). The failure is reported by name with this tail rather than
	// disappearing (Rule 15b).
	vetGateOutputTailMax = 600

	// The abstention reasons. Each is a statement about the MEASUREMENT, never
	// about the tree: a repo is not red because the merger's host could not
	// run the gate.
	vetGateNoModuleReason     = "the repo is not a Go module root (no go.mod) — this gate has no declared way to vet it"
	vetGateNoPackagesReason   = "the toolchain vetted no package (empty module) — nothing was measured"
	vetGateUnresolvedReason   = "the toolchain could not resolve the module set (no reachable module proxy) — the tree was never vetted"
	vetGateNoDiagnosticReason = "the gate failed without printing a file:line diagnostic"
)

// vetGateSpec is the resolved measurement contract: the toolchain call, the
// caches it reads, and the budget it is allowed. It exists so the resolution
// (which ABSTAINS with a reason) is separable from the run (which MEASURES).
type vetGateSpec struct {
	// Argv is the command the gate runs, as an argument vector (no shell):
	// argv[0] is a bare binary name resolved through PATH, never a literal
	// shell line — there is nothing to quote and nothing an operator's data
	// can inject into. It is a spec field rather than a constant at the call
	// site so the BOUND (the timeout) can be exercised against a command the
	// test controls without a seam in production: what runs is always this
	// vector.
	Argv []string
	// GoCacheDir is the SHARED, disk-backed, bounded build cache the gate's
	// own runs read and fill (the build gate's cache — one cache per repo, not
	// a second one for a second command: the same compiled packages answer
	// both).
	GoCacheDir string
	// ModCacheDir is the module cache to resolve from.
	ModCacheDir string
	// TimeoutSec bounds the subprocess (<=0 = no timeout).
	TimeoutSec int
}

// vetGateFinding is ONE diagnostic, in the parts the report needs: WHERE it is
// (file + line + column — the reference an operator reads) and WHAT the
// toolchain said.
type vetGateFinding struct {
	// File is the source file exactly as the toolchain named it (repo-relative
	// for a module-root run).
	File string
	// Line / Col are the position inside File ("" when the toolchain printed
	// no column).
	Line string
	Col  string
	// Message is the toolchain's own text for this finding.
	Message string
}

// Ref renders the finding's position as "file:line" (the acceptance's
// reference: a failure must name WHERE, not merely that something is wrong).
func (f vetGateFinding) Ref() string {
	return f.File + ":" + f.Line
}

// RefFull renders the position with its column when the toolchain printed one.
func (f vetGateFinding) RefFull() string {
	if f.Col == "" {
		return f.Ref()
	}
	return f.Ref() + ":" + f.Col
}

// String renders the finding as one line, position first.
func (f vetGateFinding) String() string {
	return f.RefFull() + ": " + f.Message
}

// VetGateResult is the post-merge gate re-run's verdict. It is carried on the
// rebuild result so a caller sees both halves of the close loop from one
// place, and its Summary is the ONE rendering of the verdict (the notify
// payload and the log line are the same sentence).
type VetGateResult struct {
	// Status is PASS | FAIL | ABSTAIN (the build gate's vocabulary — one
	// spelling of the three outcomes per package, never a second one).
	Status string
	// Classification is the COMPARATIVE verdict of a red gate: whether the
	// failure is introduced by the merge, was already there, or could not be
	// compared. It is set only for a FAIL (see rebuild_vet_revert.go, the
	// owner of the judgement), it is empty for a PASS/ABSTAIN, and it is the
	// ONE field the revert decision reads.
	Classification vetClassificationReason
	// ClassificationDetail is the sentence behind the Classification — the
	// baseline's own diagnostic for a pre-existing red, the blocker for an
	// uncomparable one. Reported verbatim so an operator never has to re-run
	// the comparison to learn the reason.
	ClassificationDetail string
	// ClassificationRef is the position the failure was first seen at, for a
	// classification that has one (an introduced red). "" otherwise.
	ClassificationRef string
	// Gate is the gate's own name (vetGateName) — the reported failure says
	// WHICH gate red, so a reader never has to infer it from the wording.
	Gate string
	// Repo is the checkout the gate ran in.
	Repo string
	// Reason is the abstention reason ("" unless Status is ABSTAIN).
	Reason string
	// PackageCount is how many package verdict lines the toolchain printed
	// (the FAIL verdicts of a red run, the ok verdicts of a measured pass); 0
	// when nothing was measured.
	PackageCount int
	// ErrorCount is how many file:line diagnostics the output carried — the
	// size of the red, measured over every line even when only
	// vetGateFindingsMax of them are retained. 0 IS the pass: `go vet` prints
	// nothing on a clean tree (measured), so silence is what success looks
	// like here.
	ErrorCount int
	// Findings are the retained diagnostics (capped, first ones first).
	Findings []vetGateFinding
	// OutputTail is the gate's own output when it failed without a parseable
	// diagnostic ("" otherwise) — the evidence a reader needs for a shape the
	// parser does not know.
	OutputTail string
	// Elapsed is the gate's wall time (0 for an abstention that never ran the
	// command).
	Elapsed time.Duration
	// TimeoutSec is the budget the run was given (0 = none declared).
	TimeoutSec int
}

// Failed reports whether the gate measured a RED tree. This is the only
// condition under which anything is reported: an abstention is not a failure
// and never reds a round.
func (v *VetGateResult) Failed() bool {
	return v != nil && v.Status == mergeBuildGateStatusFail
}

// Summary renders the verdict as ONE line — the sentence the notify payload
// and the log line both carry. A failure always names the GATE and the
// offending FILE:LINE first; the counts follow, so a reader can tell a
// one-line break from a repo-wide one.
func (v *VetGateResult) Summary() string {
	if v == nil {
		return ""
	}
	switch v.Status {
	case mergeBuildGateStatusFail:
		return fmt.Sprintf(vetGateFailSummaryFmt, v.Gate, v.Repo, v.failureRef(), v.ErrorCount, v.PackageCount)
	case mergeBuildGateStatusAbstain:
		return fmt.Sprintf(vetGateAbstainSummaryFmt, v.Gate, v.Repo, v.Reason)
	default:
		return fmt.Sprintf(vetGatePassSummaryFmt, v.Gate, v.Repo)
	}
}

// failureRef is the failure's position: the first finding when the output
// carried one, else the toolchain's own text (bounded) so a failure shape the
// parser does not know is still reported with evidence, never as a bare
// "something failed".
func (v *VetGateResult) failureRef() string {
	if len(v.Findings) > 0 {
		return v.Findings[0].String()
	}
	if v.OutputTail != "" {
		return v.OutputTail + " " + vetGateNoDiagnosticReason
	}
	return vetGateNoDiagnosticReason
}

// The three summary renderings (Rule 1b: the report's own spellings). The gate
// announces itself by NAME in every one of them, so a reader of the notify
// stream knows WHICH measurement spoke without inferring it from the wording.
const (
	// vetGateFailSummaryFmt is the MEASUREMENT, and deliberately NOT a
	// disposition: this sentence is delivered the moment the gate runs, which is
	// BEFORE any classification can exist (attributing the red needs a second
	// measurement of the pre-merge tip). It therefore states only what the gate
	// knows — gate, repo, position, counts. The DISPOSITION is its own delivered
	// sentence, emitted by the classifier once it knows
	// (ClassifyVetGateRevert: ReportOnlyBanner for a kept red,
	// RebuildVetRevertedWord for an introduced one).
	//
	// Measured 2026-09-17, the reason for the split: this format used to end with
	// the "merge kept, this is a REPORT" banner, so a round that was in fact
	// REVERTED still announced itself to a subscribed operator as kept —
	// two contradictory sentences about one round, the second arriving later.
	vetGateFailSummaryFmt    = "post-merge gate FAILED: gate=%s repo=%s %s (%d finding(s), %d package(s))"
	vetGateAbstainSummaryFmt = "post-merge gate ABSTAINED: gate=%s repo=%s (%s) — the round stands"
	vetGatePassSummaryFmt    = "post-merge gate passed: gate=%s repo=%s"
)

// runPostMergeVetGate resolves, runs and REPORTS the post-merge gate re-run for
// repo. It is the entry point the close loop calls, and it never returns an
// error: every outcome is carried on the result (the merger's DETECT-never-block
// contract) and the reporting side effect — notify on a red gate, a log line
// otherwise — happens here, so no caller can forget it.
func runPostMergeVetGate(repo string) *VetGateResult {
	res := &VetGateResult{Status: mergeBuildGateStatusAbstain, Gate: vetGateName, Repo: repo}
	if strings.TrimSpace(repo) == "" {
		res.Reason = "no repo to vet"
		return res
	}
	if vetGateDisabled() {
		res.Reason = vetGateDisabledEnvName + "=" + vetGateDisabledValue + " (the operator turned the gate off)"
		return res
	}
	spec, reason := vetGateResolveSpec(repo)
	if reason != "" {
		res.Reason = reason
		vetGateLog("ABSTAINS: %s", res.Summary())
		return res
	}
	res.TimeoutSec = spec.TimeoutSec

	// The measurement itself lives with the classifier's baseline path
	// (rebuild_vet_revert.go, vetGateRunIn): the tree the round closed on and
	// the tree it started from must be judged by ONE reading of the same
	// command, or the comparison between them would be a comparison of two
	// different measurements.
	resolved := vetGateRunIn(repo, spec, repo)
	res.Status, res.Reason, res.Findings, res.ErrorCount = resolved.Status, resolved.Reason, resolved.Findings, resolved.ErrorCount
	res.PackageCount, res.OutputTail, res.Elapsed = resolved.PackageCount, resolved.OutputTail, resolved.Elapsed
	if res.Status == mergeBuildGateStatusAbstain {
		vetGateLog("ABSTAINS: %s", res.Summary())
		return res
	}
	vetGateReport(res)
	return res
}

// vetGateReport is the ONE place a verdict becomes visible: a red gate is
// notified AND logged, everything else is logged. Nothing is swallowed — an
// abstention says why, a pass says it passed, a failure says which gate and
// where (Rule 15b).
func vetGateReport(res *VetGateResult) {
	if res.Failed() {
		vetGateLog("FAILED: %s", res.Summary())
		// The close loop's OWN channel: the same synthetic event the rebuild
		// failure uses, so a subscribed operator hears this without any
		// reconfiguration (see VetFailedEvent).
		NotifySyntheticEvent(VetFailedEvent, res.Repo, "", res.Summary())
		return
	}
	vetGateLog("PASS: %s", res.Summary())
}

// vetGateResolveSpec resolves the measurement contract for repo, or returns the
// abstention reason when the measurement cannot be made at all.
func vetGateResolveSpec(repo string) (vetGateSpec, string) {
	if _, err := os.Stat(filepath.Join(repo, vetGateModFile)); err != nil {
		return vetGateSpec{}, vetGateNoModuleReason
	}
	if _, err := exec.LookPath(vetGateToolchainBinary); err != nil {
		return vetGateSpec{}, fmt.Sprintf("no %s toolchain on this host: %v", vetGateToolchainBinary, err)
	}
	// The caches come from the build gate's resolvers — one cache per repo, one
	// module cache per host — rather than a second pair of readers: the vet
	// compile shares the compiled packages the build already produced, and a
	// second cache would be the drift (and the disk) Rule 14 forbids. Both
	// resolvers report their own abstention reason, so this gate abstains with
	// the same word the build gate would.
	cacheDir, ok := mergeBuildGateGoCache(repo)
	if !ok {
		return vetGateSpec{}, "no usable shared Go build cache — the reason is logged above"
	}
	modCacheDir, ok := mergeBuildGateSharedModCache()
	if !ok {
		return vetGateSpec{}, "no populated shared module cache — the reason is logged above"
	}
	timeout, err := vetGateTimeout()
	if err != nil {
		return vetGateSpec{}, err.Error()
	}
	return vetGateSpec{
		Argv:        []string{vetGateToolchainBinary, vetGateToolchainVerb, vetGateLoopArgument},
		GoCacheDir:  cacheDir,
		ModCacheDir: modCacheDir,
		TimeoutSec:  timeout,
	}, ""
}

// vetGateTimeout resolves the budget: the operator's override when it is a
// positive integer, else the default. Rule 2 — this is the ONE reader of
// vetGateTimeoutEnvName, and an unparsable value is an ERROR (the gate
// abstains with the reason), never a silent fallback to the default.
func vetGateTimeout() (int, error) {
	raw := strings.TrimSpace(os.Getenv(vetGateTimeoutEnvName))
	if raw == "" {
		return vetGateDefaultTimeoutSec, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive number of seconds", vetGateTimeoutEnvName, raw)
	}
	return n, nil
}

// vetGateDisabled reports whether the operator escape hatch is set. Rule 2:
// this is the ONE reader of vetGateDisabledEnvName; unset (or any other value)
// means ENABLED.
func vetGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(vetGateDisabledEnvName)) == vetGateDisabledValue
}

// vetGateOutcome is one execution of the gate command: the two output streams
// (kept apart — the diagnostics are on stderr and the package verdicts on
// stdout), the exit error, the wall time, and the abstention reason when the
// run itself could not be measured.
type vetGateOutcome struct {
	Stdout   string
	Stderr   string
	Err      error
	Elapsed  time.Duration
	Abstain  string
	TimedOut bool
}

// vetGateExecute runs the gate command under the spec's bounds. A non-zero exit
// is NOT an abstention (it is the measurement: a red tree); only a failure to
// MEASURE — a timeout, a toolchain that cannot resolve its modules, a process
// that could not be started — abstains. That is why there is no error return:
// EVERY outcome of a call is on the outcome itself, and a caller cannot
// accidentally treat "the toolchain answered red" as a harness failure.
func vetGateExecute(repo string, spec vetGateSpec) vetGateOutcome {
	var ctx context.Context
	var cancel context.CancelFunc
	if spec.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(spec.TimeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = repo
	// The environment comes from the build gate's ONE builder: GOCACHE = the
	// shared, disk-backed, bounded cache; GOMODCACHE = the shared read-only one;
	// the caller's GIT_CONFIG_* normalised. A close-loop probe must not fill the
	// operator's ~/.cache/go-build, and the two gates must never disagree about
	// where a compile reads from (Rule 14).
	cmd.Env = mergeBuildGateEnv(spec.GoCacheDir, spec.ModCacheDir)
	// Process-group isolation + WaitDelay: the same two guards the sibling
	// gates measured — a hung compile is killed with its children at the
	// deadline, and a GRANDCHILD holding the pipes cannot make Wait outlive the
	// budget (mergeBuildGateWaitDelay's root cause).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = mergeBuildGateWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := vetGateOutcome{Stdout: stdout.String(), Stderr: stderr.String(), Err: err, Elapsed: time.Since(start)}

	if ctx.Err() != nil && cmd.Process != nil {
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH (the group already gone) is the expected error
	}
	if ctx.Err() == context.DeadlineExceeded {
		out.TimedOut = true
		out.Abstain = fmt.Sprintf("the gate exceeded its %ds budget (a timeout is not a red tree)", spec.TimeoutSec)
		return out
	}
	if err != nil {
		if !isExitError(err) {
			// The command could not be started at all: a measurement failure,
			// never a red tree.
			out.Abstain = fmt.Sprintf("the gate could not be started: %v", err)
			return out
		}
		// A module set the toolchain could not RESOLVE was never vetted: the
		// same predicate and the same reason as the build gate's (one
		// predicate, never a second spelling — Rule 14).
		if mergeBuildGateUnresolvedModules(vetGateOutput(out.Stdout, out.Stderr)) {
			out.Abstain = vetGateUnresolvedReason
			return out
		}
		// A vet leg the FILESYSTEM killed was never vetted either: the same
		// class, the same predicate and the same dual-partition reason as the
		// build gate's (Rule 14). Read on THIS leg's OWN output value — the two
		// streams this leg captured — because an abstention decided on another
		// leg's text would be a measurement of something that did not run.
		if marker, yes := mergeBuildGateDiskExhausted(vetGateOutput(out.Stdout, out.Stderr)); yes {
			out.Abstain = mergeBuildGateDiskExhaustedErr(marker, repo)
			return out
		}
	}
	return out
}

// isExitError reports whether err is a subprocess EXIT (the command ran and
// answered) rather than a start/harness failure.
func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

// vetGateOutput joins the two streams for the predicates that read the whole
// output (the unresolved-modules test, the non-blocking fallback tail). A
// newline separates them so a line can never be glued across streams.
func vetGateOutput(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

// vetGateCountPackages counts the toolchain's own package verdict lines
// ("FAIL\t<import path>", and "ok \t<import path>" for the passing half) — the
// SIZE of the red, which is what tells a one-package break from a module-wide
// one. It is measured over both streams' joined text because WHICH stream the
// toolchain writes its verdicts to is not a contract we own.
func vetGateCountPackages(output string) int {
	n := 0
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, vetGatePackageFailPrefix) ||
			strings.HasPrefix(line, vetGatePackageOKPrefix) {
			n++
		}
	}
	return n
}

// vetGateParseFindings extracts the file:line diagnostics from the gate's
// output and returns them (capped at vetGateFindingsMax) with their TRUE count.
//
// Both streams are read: `go vet` puts the diagnostics on stderr (measured),
// and a shape that printed them on stdout must not vanish from the report. The
// parse is stdlib substring work on purpose — a vet reporter that emits JSON
// would be a new dependency for one line shape (Rule 10.7), and this gate may
// not add one.
func vetGateParseFindings(stdout, stderr string) ([]vetGateFinding, int) {
	findings := make([]vetGateFinding, 0, vetGateFindingsMax)
	seen := make(map[string]bool)
	total := 0
	for _, stream := range []string{stderr, stdout} {
		for _, raw := range strings.Split(stream, "\n") {
			finding, ok := vetGateDiagnostic(strings.TrimSpace(strings.TrimRight(raw, "\r")))
			if !ok {
				continue
			}
			key := finding.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			total++
			if len(findings) < vetGateFindingsMax {
				findings = append(findings, finding)
			}
		}
	}
	return findings, total
}

// vetGateDiagnostic parses ONE output line as "<file>:<line>[:<col>]: <message>"
// — the only line shape this gate reports a position from — and reports false
// for everything else (a "# package" banner, a FAIL verdict, prose, an empty
// line). The file must carry the Go source extension: that is what keeps a
// message containing colons from being read as a position.
func vetGateDiagnostic(line string) (vetGateFinding, bool) {
	if line == "" {
		return vetGateFinding{}, false
	}
	// `go vet`'s own type-check diagnostics carry a "vet: " prefix; a compiler
	// diagnostic does not. Dropping it when present is what makes the FIRST
	// token the file rather than the word "vet:".
	//
	// A line that does NOT carry the prefix is still accepted: the toolchain's
	// diagnostic shapes are not one shape (a type error, a build failure under
	// a package banner, a direct compile diagnostic), and refusing every
	// unprefixed line would make the parser depend on a spelling the toolchain
	// owns. What keeps prose out is the SHAPE test below (a Go source file
	// followed by a decimal line number), not the prefix.
	rest := strings.TrimPrefix(line, vetGateDiagnosticPrefix)
	file, rest, ok := strings.Cut(rest, ":")
	if !ok || !strings.HasSuffix(file, vetGateGoSourceSuffix) || strings.ContainsAny(file, " \t") {
		return vetGateFinding{}, false
	}
	lineNo, rest, ok := strings.Cut(rest, ":")
	if !ok || !vetGateIsDigits(lineNo) {
		return vetGateFinding{}, false
	}
	finding := vetGateFinding{File: file, Line: lineNo}
	// The column is optional: `<file>:<line>: <message>` is a valid diagnostic.
	if col, tail, ok := strings.Cut(rest, ":"); ok && vetGateIsDigits(col) {
		finding.Col = col
		rest = tail
	}
	finding.Message = strings.TrimSpace(rest)
	if finding.Message == "" {
		return vetGateFinding{}, false
	}
	return finding, true
}

// vetGateIsDigits reports whether s is a non-empty run of ASCII digits (the
// decimal-integer half of a source position).
func vetGateIsDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// vetGateLog renders one verdict line with the gate's own greppable prefix, so
// an operator reading the merger log sees WHICH measurement produced a line.
func vetGateLog(format string, args ...any) {
	taskNotifyLog("merger: VET gate "+format, args...)
}
