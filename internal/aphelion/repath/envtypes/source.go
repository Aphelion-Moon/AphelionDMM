// Package envtypes adapts a parsed DreamMaker environment to repath.
package envtypes

import (
	"sdmm/internal/aphelion/repath"
	"sdmm/internal/dmapi/dmenv"
)

// Source reads a published environment, which is immutable and safe for
// concurrent readers.
type Source struct {
	env *dmenv.Dme
}

var _ repath.TypeSource = Source{}

func New(env *dmenv.Dme) Source { return Source{env: env} }

func (s Source) Paths() []string {
	paths := make([]string, 0, len(s.env.Objects))
	for path, object := range s.env.Objects {
		if object != nil {
			paths = append(paths, path)
		}
	}
	return paths
}

func (s Source) Value(path, name string) (string, bool) {
	object := s.env.Objects[path]
	if object == nil || object.Vars == nil {
		return "", false
	}
	return object.Vars.Value(name)
}
