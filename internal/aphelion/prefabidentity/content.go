// Package prefabidentity defines structural identity for local editor prefabs.
// It is separate from collaboration instance IDs and canonical protocol hashes.
package prefabidentity

import (
	"encoding/binary"
	"sort"
	"strings"

	"sdmm/internal/dmapi/dmvars"
)

// Key frames every field and sorts explicit overrides, preserving raw values.
// Inherited defaults are environment state and are not saved overrides.
func Key(path string, vars *dmvars.Variables) string {
	var b strings.Builder
	field := func(value string) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		b.Write(size[:])
		b.WriteString(value)
	}
	field(path)
	if vars != nil {
		names := append([]string(nil), vars.Iterate()...)
		sort.Strings(names)
		for _, name := range names {
			field(name)
			value, ok := vars.Value(name)
			if ok {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
			field(value)
		}
	}
	return b.String()
}

func Equal(pathA string, a *dmvars.Variables, pathB string, b *dmvars.Variables) bool {
	if pathA != pathB {
		return false
	}
	length := func(v *dmvars.Variables) int {
		if v == nil {
			return 0
		}
		return v.Len()
	}
	if length(a) != length(b) {
		return false
	}
	if length(a) == 0 {
		return true
	}
	// Compare only explicit names: inherited values cannot stand in for overrides.
	for _, name := range a.Iterate() {
		found := false
		for _, other := range b.Iterate() {
			if name == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		av, aok := a.Value(name)
		bv, bok := b.Value(name)
		if aok != bok || av != bv {
			return false
		}
	}
	return true
}
