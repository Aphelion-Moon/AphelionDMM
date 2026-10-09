// Package envresolve locates the DreamMaker environment (.dme) that owns a map
// and classifies dropped file paths. It is pure filesystem logic with no UI.
package envresolve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ErrNotFound reports that no ancestor directory contains a .dme file.
var ErrNotFound = errors.New("unable to find environment")

// Result is the outcome of resolving a map's environment. Exactly one of Path
// and Candidates is set on success; Candidates holds sorted ambiguous choices.
type Result struct {
	Path       string
	Candidates []string
}

// Resolve walks from the map's directory upward and inspects the nearest
// directory that contains .dme files. Several files prefer the one whose stem
// equals the directory name; otherwise all are returned as candidates.
func Resolve(mapPath string) (Result, error) {
	path := mapPath
	for {
		dir := filepath.Dir(path)
		if dir == path {
			return Result{}, ErrNotFound
		}
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			path = dir
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("unable to read dir %s: %w", dir, err)
		}
		var found []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".dme") {
				found = append(found, filepath.Join(dir, entry.Name()))
			}
		}
		switch len(found) {
		case 0:
			path = dir
			continue
		case 1:
			return Result{Path: found[0]}, nil
		}
		sort.Strings(found)
		dirName := filepath.Base(dir)
		for _, candidate := range found {
			stem := strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))
			if strings.EqualFold(stem, dirName) {
				return Result{Path: candidate}, nil
			}
		}
		return Result{Candidates: found}, nil
	}
}

// SameEnvironment compares two environment paths after cleaning and making
// them absolute; the comparison is case-insensitive on Windows.
func SameEnvironment(a, b string) bool {
	return normalize(a) == normalize(b)
}

func normalize(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// OrderDropped keeps only .dme, .dmm and .tgm paths and orders environments
// before maps, preserving relative order within each group.
func OrderDropped(paths []string) []string {
	var envs, maps []string
	for _, p := range paths {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".dme":
			envs = append(envs, p)
		case ".dmm", ".tgm":
			maps = append(maps, p)
		}
	}
	return append(envs, maps...)
}
