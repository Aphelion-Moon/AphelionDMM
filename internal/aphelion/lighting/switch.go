package lighting

import (
	"strconv"
	"strings"
)

// VarChange is one instance variable edit. Remove drops the instance's edit so
// the type default applies; otherwise Value is the DM source text to set.
type VarChange struct {
	Name   string
	Value  string
	Remove bool
}

// Switch is how a placed light turns on or off with a map edit the game
// honours.
type Switch struct {
	On bool
	// Toggle flips the light: the edits that turn it off when On, or on.
	Toggle []VarChange
}

// LightSwitch reports whether the atom at path is a light and how to switch
// it. get reads the instance (edits over the type), typeVars the type alone.
//
// Ordinary lights switch with light_on (lighting_atom.dm:50). Wall fixtures
// ignore light_on and are lit while powered with a working bulb, so they
// switch with the bulb status: LIGHT_EMPTY and the "-empty" icon state, as
// the /empty mapping subtypes do (light_mapping_helpers.dm). A change back to
// the type default removes the edit instead of restating it.
func LightSwitch(path string, get, typeVars VarLookup, profiles []Profile) (Switch, bool) {
	if get == nil {
		get = func(string) (string, bool) { return "", false }
	}
	if typeVars == nil {
		typeVars = func(string) (string, bool) { return "", false }
	}
	change := func(name, value string) VarChange {
		if def, ok := typeVars(name); ok && sameValue(def, value) {
			return VarChange{Name: name, Remove: true}
		}
		return VarChange{Name: name, Value: value}
	}

	if prof := profileFor(path, profiles); prof != nil && prof.StatusVar != "" {
		status, e := numVar(get, prof.StatusVar, float64(prof.StatusOK))
		if e != "" {
			return Switch{}, false
		}
		on := int(status) == prof.StatusOK
		target := prof.StatusOK
		if on {
			target = prof.StatusOff
		}
		sw := Switch{On: on, Toggle: []VarChange{change(prof.StatusVar, strconv.Itoa(target))}}
		if base, ok := get(prof.BaseStateVar); ok && prof.BaseStateVar != "" && !isNull(base) {
			state := strings.Trim(strings.TrimSpace(base), `"`)
			if on {
				state += "-empty"
			}
			sw.Toggle = append(sw.Toggle, change("icon_state", strconv.Quote(state)))
		}
		return sw, true
	}

	if raw, ok := get("light_system"); ok && !isNull(raw) {
		if system, ok := parseSystem(raw); !ok || system == SystemNone {
			return Switch{}, false
		}
	}
	lightRange, e := numVar(get, "light_range", 0)
	if e != "" || lightRange <= 0 {
		return Switch{}, false
	}
	on, e := boolVar(get, "light_on", true)
	if e != "" {
		return Switch{}, false
	}
	// Only lights the type has on. Space derives starlight and items such as
	// guns and welders are lit by the game at runtime; light_on = TRUE on the
	// map would not switch them on in a meaningful way.
	if typeOn, e := boolVar(typeVars, "light_on", true); e != "" || !typeOn {
		return Switch{}, false
	}
	value := "TRUE"
	if on {
		value = "FALSE"
	}
	toggle := change("light_on", value)
	if !toggle.Remove && value == "TRUE" {
		// An undeclared light_on defaults to TRUE (_atom.dm), so "on" can
		// always be expressed by removing the edit.
		if _, declared := typeVars("light_on"); !declared {
			toggle = VarChange{Name: "light_on", Remove: true}
		}
	}
	return Switch{On: on, Toggle: []VarChange{toggle}}, true
}

// sameValue compares DM source values; the parser hands type defaults over
// evaluated, so TRUE arrives as "1".
func sameValue(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == b {
		return true
	}
	number := func(s string) (float64, bool) {
		switch strings.ToUpper(s) {
		case "TRUE":
			return 1, true
		case "FALSE":
			return 0, true
		}
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil
	}
	x, okA := number(a)
	y, okB := number(b)
	return okA && okB && x == y
}
