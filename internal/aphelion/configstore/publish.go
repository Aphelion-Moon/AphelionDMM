package configstore

import (
	"fmt"
	"path/filepath"
)

// PublishStaged shares the reviewed platform replacement used by Write. The
// caller owns a closed, flushed temporary file beside the final destination.
func PublishStaged(source, target string) error {
	from, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	to, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if filepath.Dir(from) != filepath.Dir(to) || from == to {
		return fmt.Errorf("staged replacement must use distinct files in one directory")
	}
	return replaceFile(from, to)
}
