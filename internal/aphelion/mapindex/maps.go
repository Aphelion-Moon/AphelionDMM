// Package mapindex supplies project-map discovery for the workspace browser.
package mapindex

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"sdmm/internal/aphelion/diagnostics/uistage"
)

// Contains preserves lexical project membership of an already opened map. It
// does no filesystem I/O: neither a sibling path prefix nor a TGM is a DMM entry.
func Contains(root, path string) bool {
	if root == "" || filepath.Ext(path) != ".dmm" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Scan belongs on a worker. As with the inherited Walk, directory symlinks are
// not followed and all project directories participate, including hidden ones.
func Scan(ctx context.Context, root string) ([]string, error) {
	region := uistage.Begin(uistage.Stage("aphelion.environment.map_discovery"))
	defer region.End()
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".dmm" {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}
