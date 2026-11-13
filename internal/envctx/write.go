package envctx

import (
	"os"
	"path/filepath"
)

func Write(path string, f Facts) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Render(f)), fileMode)
}
