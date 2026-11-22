package envctx

import (
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// Write creates the profile directory and writes the description into it.
//
// THE CREATION GOES THROUGH THE ONE SUITE-RUN REGISTRAR (this run's fix). The
// profile can live inside a PER-RUN SUITE ROOT — a child process whose HOME still
// points at a run root its suite's pin has already removed recreates that root
// here — and a plain os.MkdirAll brings it back with NO `.holder` registration, so
// the collector cannot judge its owner and only the 6-hour age window bounds it.
// scratch.MkdirAllWithSuiteRunOwnerPerm is that writer: it claims exactly the
// suite-run-named ancestors this call CREATES (never an existing root, never a
// directory outside the namespace), records this process as their owner, and
// reports a failed registration on stderr rather than swallowing it (Rule 15b).
func Write(path string, f Facts) error {
	return WriteDocument(path, []byte(Render(f)))
}

// WriteDocument writes ALREADY-RENDERED bytes to the description path. It is the
// ONE creation seam for this package: `gopi config env --write` renders the text
// itself (it prints the conflict lines it measured) and then lands it here, so
// the profile directory is created by ONE call rather than by a second copy of
// the mode bits beside it (Rule 14 — two spellings of "create the profile dir"
// drift, and the drift is exactly the path that loses the owner registration).
func WriteDocument(path string, body []byte) error {
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	return os.WriteFile(path, body, fileMode)
}
