// Package definition resolves a DM type's source location for "Go to
// Definition" without trusting the parsed file name to stay inside the
// environment root.
package definition

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot reports a source file that does not resolve to a path inside
// the loaded environment's root directory.
var ErrOutsideRoot = errors.New("definition source is outside the environment root")

// SourcePath joins a parsed, root-relative DM source file onto the environment
// root and refuses absolute names and any name that escapes the root.
func SourcePath(root, file string) (string, error) {
	if root == "" || file == "" {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, file)
	}
	slashed := filepath.ToSlash(file)
	if filepath.IsAbs(file) || filepath.VolumeName(file) != "" || strings.HasPrefix(slashed, "/") {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, file)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(base, filepath.FromSlash(slashed))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, file)
	}
	return target, nil
}
