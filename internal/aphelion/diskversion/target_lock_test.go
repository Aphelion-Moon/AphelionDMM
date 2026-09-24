package diskversion

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLockTargetSerializesEquivalentPaths(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "map.dmm")
	secondPath := filepath.Join(directory, ".", "map.dmm")
	if runtime.GOOS == "windows" {
		secondPath = strings.ToUpper(path)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unexpected test target exists")
		}
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	go func() {
		unlock := LockTarget(path)
		close(firstEntered)
		<-releaseFirst
		unlock()
	}()
	<-firstEntered

	secondAttempting := make(chan struct{})
	secondEntered := make(chan struct{})
	go func() {
		close(secondAttempting)
		unlock := LockTarget(secondPath)
		close(secondEntered)
		unlock()
	}()
	<-secondAttempting
	select {
	case <-secondEntered:
		t.Fatal("equivalent target paths entered concurrently")
	case <-time.After(40 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second target writer did not proceed after release")
	}
}
