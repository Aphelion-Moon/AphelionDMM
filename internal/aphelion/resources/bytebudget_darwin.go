//go:build darwin

package resources

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func hostAvailableMemory() (uint64, error) {
	pageSize := uint64(unix.Getpagesize())
	var available uint64
	for _, key := range []string{"vm.page_free_count", "vm.page_inactive_count", "vm.page_purgeable_count"} {
		count, err := unix.SysctlUint32(key)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", key, err)
		}
		available += uint64(count) * pageSize
	}
	return available, nil
}
