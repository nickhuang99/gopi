package standardgate

// export_test.go - the COMPATIBILITY BRIDGE for the published-anchor edge.
//
// The edge lived in this package (generated_doc_deps.go). It MOVED to
// internal/gendoc (a leaf importing nothing from the module) because the
// delivery path consumes it and the delivery path is internal/tools:
//
//     internal/standards [test] -> internal/tools -> internal/standards
//
// - the cycle 'go test ./internal/standards/' refuses with 'import cycle not
// allowed in test'. Not theoretical: the gate this epic exists for
// (TestCommandsDocGeneratedBlockMatchesSource) LIVES in this package, so a
// consumer that could not run it would refuse every anchor-touching delivery
// with a compile error (measured 2026-10-12, Rule 31 end-to-end run).
//
// ONE definition (Rule 14 - a leaf package every consumer can import), and this
// file keeps the names this package's own tests already read. A RE-EXPORT, not
// a copy: the value is the same object, so the two cannot disagree. An ALIAS
// (not a defined type) is what lets the tests' []generatedDocDep literals stay
// exactly as written.

import gendocpkg "github.com/nick/gopi/internal/gendoc"

// generatedDocDep is an ALIAS of the leaf package's Dep, not a second type.
type generatedDocDep = gendocpkg.Dep

const (
	generatedDocAbsentBaseNote  = gendocpkg.AbsentBaseNote
	generatedDocGoSuffix        = gendocpkg.GoSuffix
	generatedDocHiddenDirPrefix = gendocpkg.HiddenDirPrefix
	generatedDocMarker          = gendocpkg.Marker
	generatedDocMinDeps         = gendocpkg.MinEdges
	generatedDocMinNodes        = gendocpkg.MinNodes
	generatedDocVerbIndexMarker = gendocpkg.VerbIndexMarker
	// The participation vocabulary (the "does this tree take part at all?"
	// answer), re-exported so this package's own tests read the ONE definition
	// rather than a copy of it.
	generatedDocParticipationAbsent   = gendocpkg.ParticipationAbsent
	generatedDocParticipationPresent  = gendocpkg.ParticipationPresent
	generatedDocParticipationUnknown  = gendocpkg.ParticipationUnknown
	generatedDocNoCandidateCode       = gendocpkg.NoCandidateCode
	generatedDocNoRegionCode          = gendocpkg.NoRegionCode
	generatedDocAbsentNoCandidateText = gendocpkg.AbsentNoCandidateReason
	generatedDocAbsentNoRegionFormat  = gendocpkg.AbsentNoRegionReasonFormat
)

var (
	// The consumer-facing pair, readable under its original name here.
	GeneratedDocGates           = gendocpkg.GeneratedDocGates
	GeneratedDocMandate         = gendocpkg.GeneratedDocMandate
	generatedDocAnchorPattern   = gendocpkg.AnchorPattern
	generatedDocBaseAvailable   = gendocpkg.BaseAvailable
	generatedDocBaseDirs        = gendocpkg.BaseDirs
	generatedDocCandidates      = gendocpkg.Candidates
	generatedDocDeps            = gendocpkg.Deps
	generatedDocGatePackages    = gendocpkg.GatePackages
	generatedDocImpact          = gendocpkg.Impact
	generatedDocPathExists      = gendocpkg.PathExists
	generatedDocQuoted          = gendocpkg.Quoted
	generatedDocRunPattern      = gendocpkg.RunPattern
	generatedDocSkippedBaseNote = gendocpkg.SkippedBaseNote
	generatedDocTouches         = gendocpkg.Touches

	// SeparateErrors is the production split of an Impact answer into its EDGE
	// findings and its ERROR-class findings — re-exported so a test that reads
	// one half does not re-spell the severity marker (Rule 1b/13/14).
	generatedDocSeparateErrors = gendocpkg.SeparateErrors
)
