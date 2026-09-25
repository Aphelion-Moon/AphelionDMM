package mapping

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sdmm/internal/aphelion/resources"
)

// ScanProject is explicit background discovery. It reads directory entries only,
// skips symlinks/build metadata, and leaves source parsing to a selected preview.
func ScanProject(ctx context.Context, root string) ([]string, error) {
	lease, err := resources.DefaultBudget().Reserve(8 << 20)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	var paths []string
	defer func() { sort.Strings(paths) }()
	visited := 0
	stack := []string{root}
	for len(stack) > 0 {
		if err = ctx.Err(); err != nil {
			return paths, err
		}
		directory := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		f, err := os.Open(directory)
		if err != nil {
			return paths, err
		}
		for {
			entries, readErr := f.ReadDir(64)
			for _, entry := range entries {
				if err = ctx.Err(); err != nil {
					_ = f.Close()
					return paths, err
				}
				visited++
				if visited > 50000 {
					_ = f.Close()
					return paths, fmt.Errorf("project scan stopped at 50000 entries")
				}
				if entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				path := filepath.Join(directory, entry.Name())
				if len(path) > 1024 {
					_ = f.Close()
					return paths, fmt.Errorf("project scan encountered a path longer than 1024 bytes")
				}
				if entry.IsDir() {
					if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "target" {
						continue
					}
					if len(stack) >= 1024 {
						_ = f.Close()
						return paths, fmt.Errorf("project directory queue limit reached")
					}
					stack = append(stack, path)
					continue
				}
				ext := strings.ToLower(filepath.Ext(path))
				if ext == ".dmm" || ext == ".tgm" {
					paths = append(paths, path)
					if len(paths) >= 2000 {
						_ = f.Close()
						return paths, fmt.Errorf("project scan stopped at 2000 map sources")
					}
				}
			}
			if readErr != nil {
				_ = f.Close()
				if readErr != io.EOF {
					return paths, readErr
				}
				break
			}
		}
	}
	return paths, nil
}
