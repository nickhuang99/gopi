package standardgate

// Scratch env-name single-home gate (PROJECT.md Rules 13/14) — the ratchet that
// stops the 2026-09-16 semantic collision from coming back.
//
// WHY THIS GATE EXISTS. Two parallel landings auto-merged two independent
// attempts at the SAME Rule-14 constant homes inside internal/scratch:
// BaseEnvName was declared twice with DIFFERENT values (GOPI_SCRATCH_MEMORY_BASE
// vs GOPI_SCRATCH_BASE — a semantic collision, not a spelling one) and
// tmpfsMagic/ramfsMagic were declared twice, so the package did not compile and
// nothing downstream could build. The redeclaration broke the BUILD, which is
// loud; the residue it left behind is the quiet half — a MIRRORED literal in
// another package whose doc comment claimed to be "the SAME variable" under a
// one-source rule, i.e. a second definition site that only drifts at the moment
// someone renames the first. A mirrored constant plus a comment that misstates
// the relationship is exactly Rule 13's defect class.
//
// WHAT IT CHECKS (deterministic, AST-based, no judgement call):
//
//  1. Each env-name VALUE has EXACTLY ONE production holder. The holder is the
//     declared home: scratchMemoryBaseHolderFile (MemoryBaseEnvName, the host
//     variable scripts/setup-scratch-home.sh mounts) and
//     scratchResolveBaseHolderFile (ResolveBaseEnvName, the fixture-overridable
//     knob ResolveBase reads). Every other production file must REFERENCE the
//     constant (or an alias of it) — never re-inline the string.
//  2. The VALUE only, never the name: a doc comment that MENTIONS the variable
//     to explain the contract is prose, not a definition, and is deliberately
//     not a hit. The walk is over string literals, so the gate cannot punish an
//     explanation — the shape that matters is a program-actionable string.
//
// The policed values are READ FROM the constants themselves
// (scratch.MemoryBaseEnvName / scratch.ResolveBaseEnvName) rather than written
// out again here: a gate that mirrored the literals it polices would be the
// very defect it exists to report, and internal/scratch is a leaf package (it
// imports nothing from internal/standards), so referencing it costs no
// dependency and creates no cycle. A defect INSIDE internal/scratch still
// cannot hide: a duplicate declaration there fails the compiler, and the
// redeclaration case is pinned by this gate's own red proof over synthetic
// source.
//
// WHAT IT DELIBERATELY DOES NOT CHECK (so a future agent does not "close the
// gap" by widening it): _test.go files. A test that runs the setup script must
// be able to WRITE the wire spelling into a child process's environment, and
// pinning it there against the script is the drift gate's job — the rule this
// gate enforces is about PRODUCTION definition sites.

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/scratch"
)

const (
	// scratchEnvLiteralEscape marks a production site that deliberately keeps
	// the wire spelling — e.g. a constant whose value IS the interface itself.
	// The Rules 16–21 inline-escape pattern: a reviewed, visible exception
	// rather than a baseline entry.
	scratchEnvLiteralEscape = "// scratch-env-literal-ok:"

	// scratchMemoryBaseHolderFile / scratchResolveBaseHolderFile are the ONE
	// allowed holder per value — the file declaring the constant (Rule 14's
	// single source). Both live in internal/scratch, the leaf package every
	// consumer imports.
	scratchMemoryBaseHolderFile  = "internal/scratch/tmproot.go"
	scratchResolveBaseHolderFile = "internal/scratch/base_residue.go"

	// scratchEnvDefinitionDir is the directory that owns the values: a SECOND
	// declaration anywhere under it is reported even though the directory is
	// otherwise exempt, which is what makes an in-package redeclaration (the
	// shape that broke the build) visible rather than tolerated.
	scratchEnvDefinitionDir = "internal/scratch/"
)

// scratchEnvValues pairs each policed value with the file that owns it and the
// constant a violating site should have referenced instead. The values come
// from the constants, never from a second literal (Rule 14).
var scratchEnvValues = []struct {
	value   string
	holder  string
	refHint string
}{
	{
		value:   scratch.MemoryBaseEnvName,
		holder:  scratchMemoryBaseHolderFile,
		refHint: "scratch.MemoryBaseEnvName (defined in " + scratchMemoryBaseHolderFile + ")",
	},
	{
		value:   scratch.ResolveBaseEnvName,
		holder:  scratchResolveBaseHolderFile,
		refHint: "scratch.ResolveBaseEnvName (defined in " + scratchResolveBaseHolderFile + ")",
	},
}

// scratchEnvValueForHolder returns the permitted values for one file: a holder
// owns its own value and nothing else, so a holder cannot quietly absorb the
// OTHER env name without being reported.
func scratchEnvValueForHolder(rel string) map[string]bool {
	allowed := map[string]bool{}
	for _, v := range scratchEnvValues {
		if rel == v.holder {
			allowed[v.value] = true
		}
	}
	return allowed
}

// scratchEnvFind reports every re-inlined scratch env-name literal in one
// parsed production file.
//
// The walk is over STRING LITERALS only (never source text), so a doc comment
// that quotes the variable to explain the contract is not a hit. A literal that
// is not one of the policed values is not this gate's business.
func scratchEnvFind(g gate, p *parsed) []hit {
	allowed := scratchEnvValueForHolder(p.rel)
	inOwnerPackage := strings.HasPrefix(p.rel, scratchEnvDefinitionDir)

	var hits []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true // a malformed literal is the compiler's business
		}
		for _, v := range scratchEnvValues {
			if value != v.value {
				continue
			}
			if allowed[value] {
				return true // this file is the declared home of that value
			}
			pos := p.fset.Position(lit.Pos())
			if inOwnerPackage {
				hits = append(hits, hit{rel: p.rel, line: pos.Line, desc: fmt.Sprintf(
					"%q is re-declared inside its own package (%s owns it) — a second declaration in this package is the redeclaration that broke the build",
					value, v.holder)})
				return true
			}
			hits = append(hits, hit{rel: p.rel, line: pos.Line, desc: fmt.Sprintf(
				"%q re-inlines the ONE definition — reference %s instead (Rule 13/14: one home per value, never a mirrored literal)",
				value, v.refHint)})
			return true
		}
		return true
	})
	return hits
}

// scratchEnvHomeGate describes the gate for the registry.
func scratchEnvHomeGate() gate {
	return gate{
		id:             "Rule 13/14 (one production home per scratch env name)",
		checklistRow:   "1b/scratch-env",
		probe:          "TestGate8ScratchEnvHomeRedProof",
		escape:         scratchEnvLiteralEscape,
		baselineReason: "no existing site: the mirrored literal in internal/testutil was converted to a true alias of the ONE definition as part of this gate, so it lands with an EMPTY baseline — a re-inlined copy fails on the commit that introduces it.",
		baseline:       scratchEnvHomeBaseline,
		find:           scratchEnvFind,
	}
}

// scratchEnvHomeBaseline is empty BY CONSTRUCTION: the conversion of
// internal/testutil's mirrored literal into `const MemoryBaseEnvName =
// scratch.MemoryBaseEnvName` is what made the gate green the day it landed, and
// a new violation must be FIXED, not baselined.
var scratchEnvHomeBaseline []string
