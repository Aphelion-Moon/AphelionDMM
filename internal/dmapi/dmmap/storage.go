package dmmap

import (
	// APHELION EDIT ADDITION START - UNKNOWN TYPES
	"sdmm/internal/dmapi/dmenv"
	// APHELION EDIT ADDITION END
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"

	"github.com/rs/zerolog/log"
)

var PrefabStorage = &prefabStorage{prefabs: make(map[uint64]*dmmprefab.Prefab)}

type prefabStorage struct {
	prefabs       map[uint64]*dmmprefab.Prefab
	prefabsByPath map[string][]*dmmprefab.Prefab
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	byContent map[string]*dmmprefab.Prefab
	// APHELION EDIT ADDITION END
}

func (s *prefabStorage) Free() {
	log.Printf("cache free; [%d] prefabs disposed", len(s.prefabs))
	s.prefabs = make(map[uint64]*dmmprefab.Prefab)
	s.prefabsByPath = make(map[string][]*dmmprefab.Prefab)
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	s.byContent = make(map[string]*dmmprefab.Prefab)
	// APHELION EDIT ADDITION END
}

// Put persists the provided prefab in the storage.
func (s *prefabStorage) Put(prefab *dmmprefab.Prefab) *dmmprefab.Prefab {
	/* APHELION EDIT REMOVAL START - CONTENT IDENTITY
	if cachedPrefab, ok := s.GetById(prefab.Id()); ok {
		return cachedPrefab
	}
	if prefab.Id() != dmmprefab.IdStage { // Ignore staged prefabs.
		s.persist(prefab)
	}
	return prefab
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	if prefab.IsStaged() {
		return prefab
	}
	if prefab.IsInterned() && s.prefabs[prefab.Id()] == prefab {
		return prefab
	}
	prefab = prefab.Interned()
	// Resolve by content before assigning a free local UI identifier. Hashes are
	// only hints; probing also preserves existing selections on a collision.
	content := prefab.ContentKey()
	if cached, exists := s.byContent[content]; exists {
		return cached
	}
	id := prefab.Id()
	for {
		if _, occupied := s.prefabs[id]; !occupied && id > dmmprefab.IdStage {
			break
		}
		id++
	}
	if id != prefab.Id() {
		prefab = prefab.WithLocalID(id)
	}
	if s.prefabs == nil {
		s.prefabs = make(map[uint64]*dmmprefab.Prefab)
	}
	if s.prefabsByPath == nil {
		s.prefabsByPath = make(map[string][]*dmmprefab.Prefab)
	}
	if s.byContent == nil {
		s.byContent = make(map[string]*dmmprefab.Prefab)
	}
	s.byContent[content] = prefab
	s.persist(prefab)
	return prefab
	// APHELION EDIT ADDITION END
}

// Initial returns a prefab with an initial state (initial prefabs).
func (s *prefabStorage) Initial(path string) *dmmprefab.Prefab {
	/* APHELION EDIT REMOVAL START - UNKNOWN TYPES
	return s.Get(path, dmvars.FromParent(environment.Objects[path].Vars))
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - UNKNOWN TYPES
	// Returns nil for a type the loaded environment does not define. Callers
	// with map-derived paths use InitialV and refuse instead.
	prefab, _ := s.InitialV(path)
	return prefab
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - UNKNOWN TYPES

// InitialV returns the initial prefab for a type defined by the loaded
// environment. Unknown types are preserved in maps but have no environment
// defaults, so it reports false without creating or persisting a prefab.
func (s *prefabStorage) InitialV(path string) (*dmmprefab.Prefab, bool) {
	object := environmentObject(path)
	if object == nil {
		return nil, false
	}
	return s.Get(path, dmvars.FromParent(object.Vars)), true
}

// IsKnownType reports whether the loaded environment defines the type path.
func IsKnownType(path string) bool {
	return environmentObject(path) != nil
}

func environmentObject(path string) *dmenv.Object {
	if environment == nil {
		return nil
	}
	return environment.Objects[path]
}

// APHELION EDIT ADDITION END

// Get returns a prefab for the provided path and variables.
func (s *prefabStorage) Get(path string, vars *dmvars.Variables) *dmmprefab.Prefab {
	p, _ := s.GetV(path, vars)
	return p
}

// GetV returns a prefab for the provided path and variables.
// Same as Get but has the second argument which shows if the prefab was created.
func (s *prefabStorage) GetV(path string, vars *dmvars.Variables) (*dmmprefab.Prefab, bool) {
	/* APHELION EDIT REMOVAL START - CONTENT IDENTITY
	id := dmmprefab.Id(path, vars)
	if prefab, ok := s.prefabs[id]; ok {
		return prefab, false
	}
	prefab := dmmprefab.New(id, path, vars)
	s.persist(prefab)
	return prefab, true
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	candidate := dmmprefab.New(dmmprefab.IdNone, path, vars)
	count := len(s.prefabs)
	prefab := s.Put(candidate)
	return prefab, len(s.prefabs) != count
	// APHELION EDIT ADDITION END
}

// Delete deletes the provided prefab from the storage.
func (s *prefabStorage) Delete(prefab *dmmprefab.Prefab) {
	// APHELION EDIT ADDITION START - CONTENT IDENTITY
	content := prefab.ContentKey()
	stored, exists := s.byContent[content]
	if !exists {
		return
	}
	prefab = stored
	delete(s.byContent, content)
	// APHELION EDIT ADDITION END
	delete(s.prefabs, prefab.Id())
	for idx, p := range s.prefabsByPath[prefab.Path()] {
		if p.Id() == prefab.Id() {
			s.prefabsByPath[prefab.Path()] = append(s.prefabsByPath[prefab.Path()][:idx], s.prefabsByPath[prefab.Path()][idx+1:]...)
			break
		}
	}
}

// GetById returns a prefab by the provided id. If the prefab is a null, the second return value will be a "false".
func (s *prefabStorage) GetById(id uint64) (*dmmprefab.Prefab, bool) {
	prefab, ok := s.prefabs[id]
	return prefab, ok
}

// GetAllByPath returns a slice of prefabs for the provided path.
func (s *prefabStorage) GetAllByPath(path string) []*dmmprefab.Prefab {
	return s.prefabsByPath[path]
}

func (s *prefabStorage) persist(prefab *dmmprefab.Prefab) {
	s.prefabs[prefab.Id()] = prefab
	s.prefabsByPath[prefab.Path()] = append(s.prefabsByPath[prefab.Path()], prefab)
}
