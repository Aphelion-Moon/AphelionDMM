package mapadapter

import (
	"fmt"
	"os"
	"path/filepath"

	"sdmm/internal/aphelion/collab/model"
)

// ExportTGMFile stages the snapshot beside path, validates the staged file by
// reparsing it, and only then atomically replaces the destination. An invalid
// snapshot or failed validation leaves any existing file untouched.
func ExportTGMFile(snapshot model.Snapshot, path string) error {
	data, err := Export(snapshot, path, true, "\n")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	if err := data.SaveTGM(path); err != nil {
		return fmt.Errorf("export map file: %w", err)
	}
	return nil
}
