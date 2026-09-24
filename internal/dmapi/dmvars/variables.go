package dmvars

import (
	"strconv"

	"sdmm/internal/util/slice"

	"github.com/rs/zerolog/log"
)

const NullValue = "null"

// Variables is a structure to store a prefab (in the environment or on the map) variables.
// Those variables are stored as a "string2string" map and immutable by concept.
// It means that if you need to modify variables of a prefab,
// then you need to create a new prefab with modified variables.
// That restriction goes for the idea that prefabs by their nature are immutable as well.
type Variables struct {
	names  []string
	vars   map[string]string
	parent *Variables
	// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
	frozen bool
	// APHELION EDIT ADDITION END
}

func (v *Variables) Copy() Variables {
	names := make([]string, len(v.names))
	copy(names, v.names)

	vars := make(map[string]string)
	for n, v := range v.vars {
		vars[n] = v
	}

	return Variables{
		names:  names,
		vars:   vars,
		parent: v.parent,
	}
}

func (v *Variables) put(name string, value string) {
	// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
	if v.frozen {
		*v = v.Copy()
	}
	// APHELION EDIT ADDITION END
	if v.vars == nil {
		v.vars = make(map[string]string)
	}
	if !slice.StrContains(v.names, name) {
		v.names = append(v.names, name)
	}
	v.vars[name] = value
}

func (v *Variables) delete(name string) {
	// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
	if v.frozen {
		*v = v.Copy()
	}
	// APHELION EDIT ADDITION END
	v.names = slice.StrRemove(v.names, name)
	delete(v.vars, name)
}

func FromParent(parent *Variables) *Variables {
	return &Variables{parent: parent}
}

func Set(vars *Variables, name, value string) *Variables {
	cpy := vars.Copy()
	cpy.put(name, value)
	return &cpy
}

func Delete(vars *Variables, name string) *Variables {
	cpy := vars.Copy()
	cpy.delete(name)
	return &cpy
}

// MutableVariables are used to provide a basic modification interface,
// without breaking of an immutability of Variables struct.
type MutableVariables struct {
	Variables
}

func (v *MutableVariables) Put(name string, value string) {
	v.put(name, value)
}

func (v *MutableVariables) ToImmutable() *Variables {
	vars := v.Copy()
	return &vars
}

func (v *Variables) HasParent() bool {
	return v.parent != nil
}

func (v *Variables) Parent() *Variables {
	return v.parent
}

func (v *Variables) LinkParent(parent *Variables) {
	if v.parent != nil {
		log.Panic().Msg("linking of a parent to an occupied variables is prohibited!") // Just to ensure
	}
	v.parent = parent
}

func (v *Variables) Iterate() []string {
	// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
	if v.frozen {
		return append([]string(nil), v.names...)
	}
	// APHELION EDIT ADDITION END
	return v.names
}

// APHELION EDIT ADDITION START - SEALED PREFAB CONTENT
// FrozenCopy owns explicit contents at the interning boundary. Parent linkage
// remains appearance/environment state and does not enter structural identity.
func (v *Variables) FrozenCopy() *Variables {
	if v == nil {
		return nil
	}
	if v.frozen {
		return v
	}
	copy := v.Copy()
	copy.frozen = true
	return &copy
}

func (v *Variables) ExplicitValue(name string) (string, bool) {
	if v == nil {
		return "", false
	}
	value, ok := v.vars[name]
	return value, ok
}

// APHELION EDIT ADDITION END

func (v *Variables) Len() int {
	return len(v.names)
}

func (v *Variables) Value(name string) (string, bool) {
	if v.vars != nil {
		if value, ok := v.vars[name]; ok {
			return value, true
		}
	}
	if v.parent != nil {
		return v.parent.Value(name)
	}
	return "", false
}

func (v *Variables) ValueV(name string, defaultValue string) string {
	if value, ok := v.Value(name); ok {
		return value
	}
	return defaultValue
}

func (v *Variables) Text(name string) (string, bool) {
	if value, ok := v.Value(name); ok && value != NullValue {
		if len(value) > 1 {
			return value[1 : len(value)-1], true
		} else {
			return value, true
		}
	}
	return "", false
}

func (v *Variables) TextV(name string, defaultValue string) string {
	if value, ok := v.Text(name); ok {
		return value
	}
	return defaultValue
}

func (v *Variables) Float(name string) (float32, bool) {
	if value, ok := v.Value(name); ok && value != NullValue {
		if n, err := strconv.ParseFloat(value, 32); err == nil {
			return float32(n), true
		}
	}
	return 0, false
}

func (v *Variables) FloatV(name string, defaultValue float32) float32 {
	if value, ok := v.Float(name); ok {
		return value
	}
	return defaultValue
}

func (v *Variables) Int(name string) (int, bool) {
	if value, ok := v.Value(name); ok && value != NullValue {
		if n, err := strconv.ParseInt(value, 10, 32); err == nil {
			return int(n), true
		}
	}
	return 0, false
}

func (v *Variables) IntV(name string, defaultValue int) int {
	if value, ok := v.Int(name); ok {
		return value
	}
	return defaultValue
}
