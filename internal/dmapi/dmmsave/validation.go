// APHELION EDIT ADDITION START - EXPECTED INPUT VALIDATION
package dmmsave

import (
	"fmt"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

func (sp *saveProcess) save() error {
	write := sp.output.WriteDM
	if sp.output.IsTgm {
		write = sp.output.WriteTGM
	}
	return dmmdata.SaveAtomic(sp.output.Filepath, write, func(path string) error {
		if err := sp.output.ValidateSaved(path); err != nil {
			return err
		}
		if err := sp.expected.ValidateSaved(path); err != nil {
			return fmt.Errorf("saved map differs from intended input: %w", err)
		}
		return nil
	})
}

// APHELION EDIT ADDITION END
