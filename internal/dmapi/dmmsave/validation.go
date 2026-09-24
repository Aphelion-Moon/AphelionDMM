// APHELION EDIT ADDITION START - EXPECTED INPUT VALIDATION
package dmmsave

import (
	"fmt"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

func (sp *saveProcess) save() error {
	_, err := sp.writeAndValidate(nil)
	return err
}

func (sp *saveProcess) saveWithDiskState(expected diskversion.State) (diskversion.State, error) {
	return sp.writeAndValidate(&expected)
}

func (sp *saveProcess) writeAndValidate(expected *diskversion.State) (diskversion.State, error) {
	write := sp.output.WriteDM
	if sp.output.IsTgm {
		write = sp.output.WriteTGM
	}
	validate := func(path string) error {
		if err := sp.output.ValidateSaved(path); err != nil {
			return err
		}
		if err := sp.expected.ValidateSaved(path); err != nil {
			return fmt.Errorf("saved map differs from intended input: %w", err)
		}
		return nil
	}
	if expected == nil {
		if err := dmmdata.SaveAtomic(sp.output.Filepath, write, validate); err != nil {
			return diskversion.State{}, err
		}
		return diskversion.State{}, nil
	}
	return dmmdata.SaveAtomicWithState(sp.output.Filepath, write, validate, *expected)
}

// APHELION EDIT ADDITION END
