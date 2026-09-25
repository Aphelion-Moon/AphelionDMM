package envcache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// One observed validation snapshot memoizes resolved directory prefixes. It
// never reuses this information across cache requests or trusts stored paths.
type pathValidator struct {
	cwd, root   string
	realRoots   [2]string
	directories map[string]string
	approved    map[string]uint8
}

func newPathValidator(cwd, root string) *pathValidator {
	p := &pathValidator{cwd: cwd, root: root, directories: map[string]string{}, approved: map[string]uint8{}}
	p.realRoots[0], _ = filepath.EvalSymlinks(root)
	p.realRoots[1], _ = filepath.EvalSymlinks(cwd)
	return p
}
func (p *pathValidator) directory(path string) (string, error) {
	if real, ok := p.directories[path]; ok {
		return real, nil
	}
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var real string
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		real, err = filepath.EvalSymlinks(path)
	} else {
		parent := filepath.Dir(path)
		if parent == path {
			real = path
			err = nil
		} else {
			var resolvedParent string
			resolvedParent, err = p.directory(parent)
			if err == nil {
				real = filepath.Join(resolvedParent, filepath.Base(path))
			}
		}
	}
	if err == nil {
		p.directories[path] = real
	}
	return real, err
}
func (p *pathValidator) check(raw, resolved string, allowCWD bool) error {
	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.cwd, path)
	}
	if !sameLexical(path, resolved) || strings.ContainsRune(raw, 0) {
		return fmt.Errorf("invalid cached path")
	}
	bit := uint8(1)
	if allowCWD {
		bit = 2
	}
	if p.approved[resolved]&bit != 0 {
		return nil
	}
	lexicalAllowed := contained(p.root, resolved) || (allowCWD && contained(p.cwd, resolved))
	if !lexicalAllowed {
		return fmt.Errorf("cached dependency leaves approved source roots")
	}
	parent, err := p.directory(filepath.Dir(resolved))
	if err != nil {
		return err
	}
	real := filepath.Join(parent, filepath.Base(resolved))
	info, err := os.Lstat(resolved)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		real, err = filepath.EvalSymlinks(resolved)
		if err != nil {
			return err
		}
	}
	for i, root := range []string{p.root, p.cwd} {
		if i == 1 && !allowCWD {
			continue
		}
		if p.realRoots[i] != "" && contained(root, resolved) && contained(p.realRoots[i], real) {
			p.approved[resolved] |= bit
			return nil
		}
	}
	return fmt.Errorf("cached dependency resolves outside approved source roots")
}
