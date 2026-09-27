//go:build darwin && cgo

package resources

/*
#include <mach/mach.h>
#include <stdint.h>

static kern_return_t aphelion_available_pages(uint64_t *pages) {
	mach_port_t host = mach_host_self();
	vm_statistics64_data_t stats;
	mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
	kern_return_t result = host_statistics64(host, HOST_VM_INFO64,
		(host_info64_t)&stats, &count);
	mach_port_deallocate(mach_task_self(), host);
	if (result == KERN_SUCCESS) {
		// free_count includes speculative pages. Purgeable pages can overlap
		// inactive pages, so neither is added separately.
		*pages = (uint64_t)stats.free_count + (uint64_t)stats.inactive_count;
	}
	return result;
}
*/
import "C"

import (
	"fmt"
	"os"
)

func hostAvailableMemory() (uint64, error) {
	var pages C.uint64_t
	if status := C.aphelion_available_pages(&pages); status != C.KERN_SUCCESS {
		return 0, fmt.Errorf("read Mach host memory statistics: status %d", status)
	}
	return uint64(pages) * uint64(os.Getpagesize()), nil
}
