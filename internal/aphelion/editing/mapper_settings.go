package editing

// MapperSettings is serialized by the existing user preferences store.
// Selection membership and the restriction toggle remain document-local.
type MapperSettings struct {
	AreaMode         bool
	AllMatchingAreas bool
	Shape            ShapeDescriptor
	RandomFill       bool
	Palette          RandomPalette
	SavedPalettes    []RandomPalette
	Density          float64
	Seed             string
	SeedLock         bool
	// UtilityBundle is what the Brush tool lays; nil means the defaults.
	UtilityBundle []UtilityLine
	// BrushDisposals also lays a disposal pipe along the Brush route.
	BrushDisposals bool
}
