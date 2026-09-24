//go:build !windows && !linux && !darwin

package resources

import "fmt"

func hostAvailableMemory() (uint64, error) {
	return 0, fmt.Errorf("host memory accounting is unavailable on this platform")
}
