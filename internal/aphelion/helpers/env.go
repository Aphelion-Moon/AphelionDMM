package helpers

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"sdmm/internal/dmapi/dmenv"
)

// FromEnvironment indexes every mapping helper the environment defines,
// reading only the DM files those types are declared in, below the
// environment's root.
func FromEnvironment(env *dmenv.Dme) *Index {
	if env == nil {
		return nil
	}
	root, err := filepath.Abs(env.RootDir)
	if err != nil {
		return nil
	}
	var types []Type
	var sources []string
	for path, object := range env.Objects {
		if !isType(path, Root) {
			continue
		}
		vars := object.Vars
		abstract := false
		if raw, ok := vars.Value("abstract_type"); ok && strings.TrimSpace(raw) == path {
			abstract = true
		}
		types = append(types, Type{Path: path, Name: vars.TextV("name", ""), Desc: vars.TextV("desc", ""), Abstract: abstract})
		if file := object.Location.File; file != "" {
			sources = append(sources, file)
		}
	}
	return Build(types, sources, func(file string) ([]byte, error) {
		resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(file)))
		if rel, err := filepath.Rel(root, resolved); err != nil || strings.HasPrefix(rel, "..") {
			return nil, os.ErrPermission // never read outside the environment
		}
		return os.ReadFile(resolved)
	})
}

var (
	activeMu  sync.Mutex
	activeEnv *dmenv.Dme
	active    *Index
)

// ForEnvironment returns the index for env, building it on first use.
func ForEnvironment(env *dmenv.Dme) *Index {
	activeMu.Lock()
	defer activeMu.Unlock()
	if env != activeEnv {
		activeEnv, active = env, FromEnvironment(env)
	}
	return active
}
