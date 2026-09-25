package mapping

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverMapConfigurations inspects the existing station configuration folder,
// not every source in the project. Ambiguous matches remain a user choice.
func DiscoverMapConfigurations(ctx context.Context, root, source string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "_maps"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("map configuration directory exceeds discovery limit")
	}
	var matches []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		relative := filepath.ToSlash(filepath.Join("_maps", entry.Name()))
		path, err := BoundPath(root, relative)
		if err != nil {
			continue
		}
		data, err := readSmallConfig(path)
		if err != nil {
			continue
		}
		var config MapConfiguration
		if json.Unmarshal(data, &config) != nil {
			continue
		}
		var file string
		if json.Unmarshal(config.MapFile, &file) != nil {
			continue
		}
		path, err = BoundPath(root, filepath.ToSlash(filepath.Join("_maps", config.MapPath, file)))
		if err == nil && samePath(path, source) {
			matches = append(matches, relative)
		}
	}
	return matches, nil
}
