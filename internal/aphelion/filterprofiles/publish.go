package filterprofiles

import (
	"errors"

	"sdmm/internal/dmapi/dm"
)

// RenameActive changes only display metadata; temporary visibility remains intact.
func (s *Session) RenameActive(id, name string) error {
	if s.active == nil || s.active.ID != id {
		return nil
	}
	profile := Clone(*s.active)
	profile.Name = name
	if err := Validate(profile); err != nil {
		return err
	}
	s.active = &profile
	return nil
}

// ApplyCompiled publishes a prepared profile in one filter-policy update.
// Async callers must verify their environment and request generation first.
func (s *Session) ApplyCompiled(profile Profile, compiled Compiled, filter *dm.PathsFilter) error {
	if filter == nil {
		return errors.New("no visibility filter is available")
	}
	if err := Validate(profile); err != nil {
		return err
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	copy := Clone(profile)
	s.active = &copy
	s.overrides = Overrides{}
	s.lastHidden = nil
	s.warnings = append([]Warning(nil), compiled.Warnings...)
	s.effective = compiled
	s.recordVisibility("Apply " + profile.Name)
	return nil
}
