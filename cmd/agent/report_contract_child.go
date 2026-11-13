package main

// report_contract_child.go — the shared strip that keeps a gopi SPAWN from
// inheriting somebody else's task-report destination (epic
// nested-report-path-clobber, 2026-09-15).
//
// The defect: taskreport.PathEnv is the report path of ONE session — the
// wrapper exports it before spawning the child that run owns — but the
// environment travels DOWN the whole tree. A gopi process launched from an
// agent session's shell (a self-probe, a nested `go test`, a `merger verify`
// child, a daemon spawn) inherited the PARENT's path, treated it as its own
// destination, and wrote its own report over the parent's task-report.md
// (measured: session 01a0a4ee's deliverable replaced by a child probe's "ls"
// report). The child was not misbehaving — the variable carried no owner.
//
// The fix has three cooperating faces, each necessary:
//
//	① the OWNER marker       cmd/agent's `task run` seam exports
//	                         taskreport.OwnEnv=<task id> beside the path, so a
//	                         report contract identifies itself;
//	② this strip             every OTHER gopi-spawning site REMOVES the pair
//	                         from the child's environment (a child gopi must
//	                         mint its own / write nowhere, never inherit);
//	③ the read guard         internal/agent refuses an inherited contract at
//	                         session start and says so (Rule 15: a refused
//	                         export is reported, never silently obeyed), which
//	                         covers the third-party launcher this tree does
//	                         not own.
//
// Face ② is the one seam: `task run` itself subtracts the pair in
// taskChildExecEnv (it is about to export its own). Every spawn that must NOT
// pass a report contract calls stripSpawnedReportContract.
//
// The env NAMES come from the packages that own them (taskreport.PathEnv /
// taskreport.OwnEnv — Rule 1b/13/14); this file carries no literal.

import (
	"fmt"
	"os"

	"github.com/nick/gopi/internal/tools"
)

// stripSpawnedReportContract returns env without the task-report contract pair
// and the names actually removed. The removed set is RETURNED so the caller
// records it (Rule 15) — an inherited report destination that vanishes
// silently is indistinguishable from one that was never there, and the whole
// point of this epic is that the operator can tell which process tried to
// carry somebody else's report path.
func stripSpawnedReportContract(env []string) (stripped []string, removed []string) {
	return tools.StripInheritedReportContract(env)
}

// reportContractStripLine renders the strip record for a spawn whose child
// must not inherit a report contract (the same shape as taskEnvStripLine, so
// one grep finds both sides of the seam).
func reportContractStripLine(removed []string, child string) string {
	if len(removed) == 0 {
		return ""
	}
	return fmt.Sprintf("gopi %s: stripped inherited task-report contract %v from the child env (a spawned gopi must not write another session's report)\n", child, removed)
}

// reportContractStripAndLog applies the strip and prints the record on stderr
// — the call shape every spawn site uses, so the report of the strip can never
// be forgotten at a new site (Rule 15).
func reportContractStripAndLog(env []string, child string) []string {
	stripped, removed := stripSpawnedReportContract(env)
	if line := reportContractStripLine(removed, child); line != "" {
		fmt.Fprint(os.Stderr, line)
	}
	return stripped
}
