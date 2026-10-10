package crashlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const childEnv = "APHELION_CRASHLOG_CHILD"

// TestCaptureRecordsGoroutinePanic crashes a child process from a background
// goroutine, the case that left no trace in the desktop session log.
func TestCaptureRecordsGoroutinePanic(t *testing.T) {
	if path := os.Getenv(childEnv); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(3)
		}
		if err := Capture(file); err != nil {
			os.Exit(4)
		}
		go func() {
			var tiles map[string]int
			tiles["light"]++ // nil map write: an unrecovered runtime panic
		}()
		// No deferred signal: deferred calls run while a panic unwinds, which
		// let the main goroutine exit cleanly before the runtime crashed.
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "session.log")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCaptureRecordsGoroutinePanic$")
	cmd.Env = append(os.Environ(), childEnv+"="+path)
	if err := cmd.Run(); err == nil {
		t.Fatal("child did not crash")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, want := range []string{"crash reports for this session", "panic: assignment to entry in nil map", "goroutine", "crashlog_test.go"} {
		if !strings.Contains(log, want) {
			t.Fatalf("session log lacks %q:\n%s", want, log)
		}
	}
}
