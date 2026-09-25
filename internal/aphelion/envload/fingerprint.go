// Package envload contains preparation owned by one parsed environment generation.
package envload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"sdmm/internal/dmapi/dmvars"
)

// Fingerprint streams the established environment.v1 framing. It deliberately
// includes the same explicit variable names and effective values as that format.
func Fingerprint(objects map[string]*dmvars.Variables) (string, error) {
	paths := make([]string, 0, len(objects))
	for path := range objects {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	encoded := sha256.New()
	writeNumber := func(value uint64) { var b [8]byte; binary.BigEndian.PutUint64(b[:], value); _, _ = encoded.Write(b[:]) }
	writeString := func(value string) { writeNumber(uint64(len(value))); _, _ = io.WriteString(encoded, value) }
	writeString("apheliondmm.environment.v1")
	writeNumber(uint64(len(paths)))
	for _, path := range paths {
		variables := objects[path]
		if variables == nil {
			return "", fmt.Errorf("hash environment: object %q has nil variables", path)
		}
		writeString(path)
		names := append([]string(nil), variables.Iterate()...)
		sort.Strings(names)
		writeNumber(uint64(len(names)))
		for _, name := range names {
			value, exists := variables.Value(name)
			if !exists {
				return "", fmt.Errorf("hash environment: variable %q on %q has no value", name, path)
			}
			writeString(name)
			writeString(value)
		}
	}
	return hex.EncodeToString(encoded.Sum(nil)), nil
}
