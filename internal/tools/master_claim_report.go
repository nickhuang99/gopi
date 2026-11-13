package tools

// master_claim_report.go — the SEAL's half of the implementation-claim check:
// when a report says the integration branch ALREADY has something, the commit
// it names is MEASURED (master_claim.go) and a negative verdict is RECORDED.
//
// THE MEASURED CASE (session 01a0f3de): the run concluded "master already has
// it, no gap" by reading the tip of an ARCHIVED branch (feat/capture-self-window,
// e1701ac8) that master does not contain — and finished exit=0. Its own
// observations, in the same run, said the branch ref was gone; the tag kept the
// commit alive, so reading it worked, and a working command reads like evidence.
//
// THE ACTION IS (A) RECORD — never a rewrite, never a refusal — the discipline
// gateclaim.go and runcheck.go established and the reason is also theirs: this
// project's reports are full of honestly-measured sentences whose subject the
// seal cannot see (work measured against a SIBLING worktree, a PEER repository,
// a report quoting an upstream project's master). A refusal keyed on the text
// alone would refuse those, and a gate that fires on honest work gets switched
// off within a day. So: the report keeps its sentences byte for byte, the
// finding rides the row's append-only digest + stderr, and an operator reads it
// where the artifact of record is closed out (Rule 15 — no silent failure, no
// silent rewrite).
//
// WHAT IS MEASURED, and why the measurement is trustworthy where the text is
// not: the ancestry probe runs against THE REPO THE RUN ITSELF WAS DRIVEN IN
// (resolved from the run's own session record — the authority the report's
// harness facts read FIRST for the commit line, before the row's recorded
// grounds, task_report.go:taskReportCommitDirs). When that repo
// cannot be resolved the check no-ops and says so in the record: an unresolvable
// repo is never read as a pass, and never as an accusation against the report.
//
// MERGE NOTE (rework run 01a12e51, 2026-10-13): the sentence above is the
// archived tip 352ba4726's re-expression of THIS comment and it is kept
// VERBATIM, because the thing it describes survived the merge in full — the
// sibling file it names (task_report.go's taskReportCommitDirs) is present
// below and is the rung order the run's own facts are read through. The
// rework carries no change of its own to this file.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// taskReportMasterClaimRecord adjudicates the sealed body's implementation
// claims and returns the digest fragment naming the unsupported ones ("" when
// none is claimed, or when the repo to measure in cannot be resolved). The
// stderr warning is emitted HERE, so the operator reads the finding at the
// moment the artifact of record is closed out.
func taskReportMasterClaimRecord(taskID, body string) string {
	claims := taskreport.ImplClaimsIn(body, IntegrationBranchName)
	if len(claims.Claims) == 0 {
		return ""
	}
	repo := masterClaimRepoForRun()
	if repo == "" {
		// The run's repo could not be resolved: nothing is measured, and the
		// absence is STATED on the digest rather than passed over (a silent
		// hole is the defect this whole family exists to close).
		record := MasterClaimDigestPrefix + "unmeasured — the run's repo could not be resolved"
		fmt.Fprintf(os.Stderr, "%s%s%s %s: %s\n", taskReportClaimWarnPrefix,
			MasterClaimWarningLabel, " "+taskID, "(unmeasured)", record)
		return record
	}
	var unsupported []string
	measured := map[string]bool{}
	for _, c := range claims.Claims {
		if c.Commit == "" {
			// A claim that names no commit cannot be re-measured by anyone: it
			// is recorded as such rather than skipped (Rule 15).
			unsupported = append(unsupported, fmt.Sprintf("%s [no commit named — nothing to re-measure]", c.Sentence))
			continue
		}
		if measured[c.Commit] {
			continue
		}
		measured[c.Commit] = true
		ev := MasterClaimEvidenceAt(repo, c.Commit)
		if ev.OK() {
			continue
		}
		unsupported = append(unsupported, fmt.Sprintf("%s [%s]", c.Sentence, masterClaimFinding(ev)))
		if len(unsupported) >= MaxRecordedMasterClaims {
			break
		}
	}
	if len(unsupported) == 0 {
		return ""
	}
	record := MasterClaimDigestPrefix + strings.Join(unsupported, claimRecordSeparator)
	for _, line := range strings.Split(strings.TrimRight(
		masterClaimWarning(taskID, repo, unsupported), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", taskReportClaimWarnPrefix, line)
	}
	return record
}

// claimRecordSeparator is the same separator the gate-claim record uses: the
// sentences quoted in a digest fragment can never contain it, so a reader can
// split one fragment back into its claims.
const claimRecordSeparator = " | "

// masterClaimFinding renders one failed measurement compactly: the command whose
// EXIT STATUS decided it, that status, and what is holding the commit alive.
func masterClaimFinding(ev MasterClaimEvidence) string {
	if ev.Err != "" {
		return fmt.Sprintf("%s → %s — NOT re-measurable", ev.PeelCmd(), ev.Err)
	}
	held := "no ref names it"
	if len(ev.ArchiveRefs) > 0 {
		held = "held alive by " + strings.Join(ev.ArchiveRefs, ", ")
	}
	return fmt.Sprintf("%s → exit=%d — not evidence that %s implements it; %s",
		ev.AncestorCmd(), ev.AncestorExit, IntegrationBranchName, held)
}

// masterClaimWarning renders the operator lines for a seal-time finding.
func masterClaimWarning(taskID, repo string, findings []string) string {
	var b strings.Builder
	b.WriteString(MasterClaimWarningLabel)
	if taskID != "" {
		b.WriteString(" ")
		b.WriteString(taskID)
	}
	fmt.Fprintf(&b, " — in %s: the report says %s already has something, and the commit it names is not an ancestor of the ref named for it\n",
		repo, IntegrationBranchName)
	for _, f := range findings {
		b.WriteString("  ")
		b.WriteString(f)
		b.WriteString("\n")
	}
	return b.String()
}

// masterClaimRepoForRun resolves the repo the run was driven in: the working
// directory recorded on the run's OWN session (the authority the report's
// harness facts already use), else this process's working directory. "" when
// neither resolves — the caller records the absence.
func masterClaimRepoForRun() string {
	sid := strings.TrimSpace(os.Getenv(TaskChildSessionEnv))
	if sid == "" {
		sid = strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv))
	}
	if sid != "" {
		if wd := taskWorkdirFromSession(sid); wd != "" {
			if repo, err := RepoRootAt(wd); err == nil {
				return repo
			}
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	repo, err := RepoRootAt(wd)
	if err != nil {
		return ""
	}
	return repo
}
