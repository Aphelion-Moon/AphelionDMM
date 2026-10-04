//go:build darwin && !cgo

package resources

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func hostAvailableMemory() (uint64, error) {
	// Without Mach access, admit conservatively from free pages only.
	// macOS does not expose inactive or purgeable counts through sysctl.
	count, err := unix.SysctlUint32("vm.page_free_count")
	if err != nil {
		return 0, fmt.Errorf("read vm.page_free_count: %w", err)
	}
	return uint64(count) * uint64(os.Getpagesize()), nil
}
