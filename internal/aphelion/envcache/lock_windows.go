package envcache

import "golang.org/x/sys/windows"

// A no-sharing handle provides process-wide exclusion and is released by the
// kernel after interruption. Cache contention skips persistence, never loading.
func storageLock(path string) (func(), error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
