// APHELION EDIT ADDITION START - COLLABORATION
package dmmdata

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"sdmm/internal/util"
)

func SaveAtomic(path string, write func(io.Writer) error, validate func(string) error) (resultErr error) {
	if write == nil {
		return fmt.Errorf("atomic save: writer is nil")
	}
	if validate == nil {
		return fmt.Errorf("atomic save: validator is nil")
	}

	directory := filepath.Dir(path)
	stage, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create staging file: %w", err)
	}
	stagePath := stage.Name()
	stageOpen := true
	defer func() {
		if stageOpen {
			resultErr = errors.Join(resultErr, stage.Close())
		}
		if removeErr := os.Remove(stagePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove staging file: %w", removeErr))
		}
	}()

	if err := write(stage); err != nil {
		return fmt.Errorf("write staging file: %w", err)
	}
	mode := os.FileMode(0o666)
	if targetInfo, statErr := os.Stat(path); statErr == nil {
		mode = targetInfo.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect target permissions: %w", statErr)
	}
	if err := stage.Chmod(mode); err != nil {
		return fmt.Errorf("set staging permissions: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync staging file: %w", err)
	}
	if err := stage.Close(); err != nil {
		stageOpen = false
		return fmt.Errorf("close staging file: %w", err)
	}
	stageOpen = false
	if err := validate(stagePath); err != nil {
		return fmt.Errorf("validate staging file: %w", err)
	}
	if err := replaceFile(stagePath, path); err != nil {
		return fmt.Errorf("replace target: %w", err)
	}
	return nil
}

// ValidateSaved compares a staged file against this independent semantic view.
func (d DmmData) ValidateSaved(path string) error {
	return d.validateSaved(path, d.IsTgm)
}

func (d DmmData) validateSaved(path string, isTGM bool) error {
	reparsed, err := New(path)
	if err != nil {
		return fmt.Errorf("reparse staged map: %w", err)
	}
	if reparsed.IsTgm != isTGM {
		return fmt.Errorf("staged map format mismatch: got TGM=%t, want %t", reparsed.IsTgm, isTGM)
	}
	if reparsed.MaxX != d.MaxX || reparsed.MaxY != d.MaxY || reparsed.MaxZ != d.MaxZ {
		return fmt.Errorf("staged map dimensions are (%d,%d,%d), want (%d,%d,%d)", reparsed.MaxX, reparsed.MaxY, reparsed.MaxZ, d.MaxX, d.MaxY, d.MaxZ)
	}
	want, err := d.semanticDigest()
	if err != nil {
		return fmt.Errorf("hash source map: %w", err)
	}
	got, err := reparsed.semanticDigest()
	if err != nil {
		return fmt.Errorf("hash staged map: %w", err)
	}
	if got != want {
		return fmt.Errorf("staged map semantic hash mismatch: got %x, want %x", got, want)
	}
	return nil
}

func (d DmmData) semanticDigest() ([sha256.Size]byte, error) {
	if d.MaxX <= 0 || d.MaxY <= 0 || d.MaxZ <= 0 {
		return [sha256.Size]byte{}, fmt.Errorf("dimensions must be positive")
	}
	var encoded bytes.Buffer
	writeSaveUint64(&encoded, uint64(d.MaxX))
	writeSaveUint64(&encoded, uint64(d.MaxY))
	writeSaveUint64(&encoded, uint64(d.MaxZ))
	for z := 1; z <= d.MaxZ; z++ {
		for y := 1; y <= d.MaxY; y++ {
			for x := 1; x <= d.MaxX; x++ {
				point := util.Point{X: x, Y: y, Z: z}
				key, exists := d.Grid[point]
				if !exists {
					return [sha256.Size]byte{}, fmt.Errorf("grid has no key at (%d,%d,%d)", x, y, z)
				}
				prefabs, exists := d.Dictionary[key]
				if !exists {
					return [sha256.Size]byte{}, fmt.Errorf("dictionary has no content for key %q", key)
				}
				writeSaveUint64(&encoded, uint64(len(prefabs)))
				for _, prefab := range prefabs {
					if prefab == nil || prefab.Vars() == nil {
						return [sha256.Size]byte{}, fmt.Errorf("key %q contains nil prefab data", key)
					}
					writeSaveString(&encoded, prefab.Path())
					names := append([]string(nil), prefab.Vars().Iterate()...)
					sort.Strings(names)
					writeSaveUint64(&encoded, uint64(len(names)))
					for _, name := range names {
						value, exists := prefab.Vars().Value(name)
						if !exists {
							return [sha256.Size]byte{}, fmt.Errorf("variable %q on %q has no value", name, prefab.Path())
						}
						writeSaveString(&encoded, name)
						writeSaveString(&encoded, value)
					}
				}
			}
		}
	}
	return sha256.Sum256(encoded.Bytes()), nil
}

func writeSaveString(buffer *bytes.Buffer, value string) {
	writeSaveUint64(buffer, uint64(len(value)))
	_, _ = buffer.WriteString(value)
}

func writeSaveUint64(buffer *bytes.Buffer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = buffer.Write(encoded[:])
}

// APHELION EDIT ADDITION END
