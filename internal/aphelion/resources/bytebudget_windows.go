//go:build windows

package resources

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func hostAvailableMemory() (uint64, error) {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	if err := proc.Find(); err != nil {
		return 0, err
	}
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, callErr := proc.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		if callErr != windows.ERROR_SUCCESS {
			return 0, callErr
		}
		return 0, fmt.Errorf("GlobalMemoryStatusEx failed")
	}
	return status.AvailPhys, nil
}
