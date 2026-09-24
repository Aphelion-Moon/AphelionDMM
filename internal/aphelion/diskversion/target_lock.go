package diskversion

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type targetLock struct {
	mu   sync.Mutex
	refs int
}

var saveTargets = struct {
	sync.Mutex
	locks map[string]*targetLock
}{locks: make(map[string]*targetLock)}

// LockTarget serializes cooperating saves to one filesystem path for the full
// stage, validate, expected-version check, and atomic replacement sequence.
func LockTarget(path string) func() {
	key, err := filepath.Abs(path)
	if err != nil {
		key = filepath.Clean(path)
	}
	key = filepath.Clean(key)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}

	saveTargets.Lock()
	lock := saveTargets.locks[key]
	if lock == nil {
		lock = &targetLock{}
		saveTargets.locks[key] = lock
	}
	lock.refs++
	saveTargets.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		saveTargets.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(saveTargets.locks, key)
		}
		saveTargets.Unlock()
	}
}
