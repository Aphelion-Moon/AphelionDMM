package uistage

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/trace"
	"testing"
	"time"
)

func TestRecordingFlushesAtDeadlineAndDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui.trace")
	recording, err := StartFile(path, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = recording.Close() }()
	Begin(Workload).End()
	select {
	case <-recording.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("recording missed deadline")
	}
	if err := recording.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte("go 1.")) || !bytes.Contains(data, []byte(Workload)) {
		t.Fatalf("trace was not flushed: %v", err)
	}
	if _, err := StartFile(path, time.Second); err == nil {
		t.Fatal("existing trace was overwritten")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("failed recording changed existing trace")
	}
}

func TestRecordingCannotStopAnotherTrace(t *testing.T) {
	var data bytes.Buffer
	if err := trace.Start(&data); err != nil {
		t.Fatal(err)
	}
	defer trace.Stop()
	if _, err := StartFile(filepath.Join(t.TempDir(), "busy.trace"), time.Second); err == nil {
		t.Fatal("second recorder took ownership")
	}
	if !trace.IsEnabled() {
		t.Fatal("failed start stopped the existing trace")
	}
}
