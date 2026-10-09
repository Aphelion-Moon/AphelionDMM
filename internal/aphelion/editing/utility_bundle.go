package editing

// UtilityLine is one atom the Brush tool lays on every tile it crosses.
type UtilityLine struct {
	Path    string
	Enabled bool
}

// DefaultUtilityBundle is the station standard Meridian-Rift maps use: air
// supply on piping layer 4, scrubbers on layer 2 and a power cable. Smart
// pipes and cables connect themselves in game, so a straight list suffices.
func DefaultUtilityBundle() []UtilityLine {
	return []UtilityLine{
		{Path: "/obj/machinery/atmospherics/pipe/smart/manifold4w/supply/hidden/layer4", Enabled: true},
		{Path: "/obj/machinery/atmospherics/pipe/smart/manifold4w/scrubbers/hidden/layer2", Enabled: true},
		{Path: "/obj/structure/cable", Enabled: true},
	}
}

// Bundle returns the configured utility lines, installing the defaults the
// first time.
func (s *MapperSettings) Bundle() []UtilityLine {
	if s.UtilityBundle == nil {
		s.UtilityBundle = DefaultUtilityBundle()
	}
	return s.UtilityBundle
}
