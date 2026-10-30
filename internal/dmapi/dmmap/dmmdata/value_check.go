// APHELION EDIT ADDITION START - VALUE ROUND TRIP
package dmmdata

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const valueCheckPath = "/obj/aphelion_value_check"

// CheckVariableValue reports whether a prefab variable survives being written
// and reparsed unchanged in both map formats. The map parser is the oracle, so
// anything it would truncate, split or normalise is rejected before it can
// reach a map file and make every later save fail validation.
func CheckVariableValue(name, value string) error {
	for _, tgm := range []bool{true, false} {
		if err := checkVariableValue(name, value, tgm); err != nil {
			return err
		}
	}
	return nil
}

func checkVariableValue(name, value string, tgm bool) error {
	prefab := dmmprefab.New(dmmprefab.IdNone, valueCheckPath, dmvars.Set(&dmvars.Variables{}, name, value))
	data := DmmData{
		IsTgm:      tgm,
		LineBreak:  "\n",
		KeyLength:  1,
		MaxX:       1,
		MaxY:       1,
		MaxZ:       1,
		Dictionary: DataDictionary{"a": Prefabs{prefab}},
		Grid:       DataGrid{util.Point{X: 1, Y: 1, Z: 1}: "a"},
	}
	var output bytes.Buffer
	write := data.WriteDM
	if tgm {
		write = data.WriteTGM
	}
	if err := write(&output); err != nil {
		return err
	}
	reparsed, err := parse(namedVersionReader{Reader: &output, name: "value check"})
	if err != nil {
		if strings.Contains(err.Error(), "not terminated") {
			return fmt.Errorf("%s = %s is not a valid value: unterminated string or list", name, value)
		}
		return fmt.Errorf("%s = %s is not a valid value: %w", name, value, err)
	}
	prefabs := reparsed.Dictionary["a"]
	if len(prefabs) != 1 || prefabs[0].Path() != valueCheckPath {
		return fmt.Errorf("%s = %s is not a valid value: it breaks the object definition", name, value)
	}
	names := prefabs[0].Vars().Iterate()
	got, exists := prefabs[0].Vars().Value(name)
	if len(names) != 1 || !exists {
		return fmt.Errorf("%s = %s is not a valid value: it splits into several variables", name, value)
	}
	if got != value {
		return fmt.Errorf("%s = %s would be saved as %s; enter it in that form", name, value, got)
	}
	return nil
}

// ValueProblem is one explicit variable that cannot be saved unchanged.
type ValueProblem struct {
	Path, Name, Value string
	// Point is one tile using the prefab, for the user to find it.
	Point util.Point
	Err   error
}

func (problem ValueProblem) String() string {
	return fmt.Sprintf("%s at (%d,%d,%d): %v", problem.Path, problem.Point.X, problem.Point.Y, problem.Point.Z, problem.Err)
}

// ValueProblems lists up to limit unsaveable variables, each prefab once. It
// is a diagnostic for failed saves, so it favours clarity over speed.
func (d DmmData) ValueProblems(limit int) []ValueProblem {
	firstPoint := make(map[Key]util.Point, len(d.Dictionary))
	for z := 1; z <= d.MaxZ; z++ {
		for y := 1; y <= d.MaxY; y++ {
			for x := 1; x <= d.MaxX; x++ {
				point := util.Point{X: x, Y: y, Z: z}
				if key, ok := d.Grid[point]; ok {
					if _, seen := firstPoint[key]; !seen {
						firstPoint[key] = point
					}
				}
			}
		}
	}

	var problems []ValueProblem
	checked := make(map[*dmmprefab.Prefab]bool)
	for _, key := range d.Keys() {
		point, used := firstPoint[key]
		if !used {
			continue
		}
		for _, prefab := range d.Dictionary[key] {
			if prefab == nil || prefab.Vars() == nil || checked[prefab] {
				continue
			}
			checked[prefab] = true
			names := append([]string(nil), prefab.Vars().Iterate()...)
			sort.Strings(names)
			for _, name := range names {
				value, _ := prefab.Vars().Value(name)
				if err := CheckVariableValue(name, value); err != nil {
					problems = append(problems, ValueProblem{Path: prefab.Path(), Name: name, Value: value, Point: point, Err: err})
					if len(problems) >= limit {
						return problems
					}
				}
			}
		}
	}
	return problems
}

// explainValidationFailure adds the variables that most likely caused a
// failed staged-save validation, so the user can find and fix them.
func (d DmmData) explainValidationFailure(err error) error {
	problems := d.ValueProblems(5)
	if len(problems) == 0 {
		return err
	}
	lines := make([]string, 0, len(problems))
	for _, problem := range problems {
		lines = append(lines, problem.String())
	}
	return errors.Join(err, fmt.Errorf("the map contains values that cannot be saved; fix or undo them and save again:\n%s", strings.Join(lines, "\n")))
}

// APHELION EDIT ADDITION END
