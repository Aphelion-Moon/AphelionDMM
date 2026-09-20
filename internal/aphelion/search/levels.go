package search

// LevelRange is an inclusive, explicit Z filter. Its zero value selects all
// levels. Keep a selected range across map resize: clamping it automatically
// could redirect a bulk action to a level the user did not select.
type LevelRange struct {
	First, Last int
}

func (r LevelRange) IsAll() bool { return r == (LevelRange{}) }

func (r LevelRange) Contains(level int) bool {
	return r.IsAll() || (r.First > 0 && r.First <= level && level <= r.Last)
}
