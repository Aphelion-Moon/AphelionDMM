package playtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestHelperProcess stands in for dm, dreamdaemon and dreamseeker.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("PLAYTEST_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "compile-ok":
		fmt.Println("loading tgstation.dme")
		fmt.Println("tgstation.dmb - 0 errors, 0 warnings")
		os.Exit(0)
	case "compile-fail":
		fmt.Println("code/x.dm:1:error: bad")
		os.Exit(1)
	case "server":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func fakeRunner(t *testing.T, listening *atomic.Bool) (*Runner, *[]string) {
	r := NewRunner()
	var started []string
	r.start = func(ctx context.Context, argv []string, dir string) *exec.Cmd {
		started = append(started, argv[0])
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", argv[len(argv)-1])
		cmd.Env = append(os.Environ(), "PLAYTEST_HELPER=1")
		if argv[0] == "server" {
			listening.Store(true)
		}
		return cmd
	}
	r.dial = func(int) bool { return listening.Load() }
	return r, &started
}

func waitFor(t *testing.T, r *Runner, want Phase) []string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if phase, lines := r.Snapshot(); phase == want {
			return lines
		}
		time.Sleep(20 * time.Millisecond)
	}
	phase, lines := r.Snapshot()
	t.Fatalf("phase %v, want %v; log %v", phase, want, lines)
	return nil
}

func TestRunnerCompilesStartsServerThenClient(t *testing.T) {
	var listening atomic.Bool
	r, started := fakeRunner(t, &listening)
	c := Commands{Compile: []string{"dm", "compile-ok"}, Server: []string{"server", "server"}, Client: []string{"client", "client"}}
	if err := r.Start(c, t.TempDir(), 1337, nil); err != nil {
		t.Fatal(err)
	}
	lines := waitFor(t, r, Running)
	if !strings.Contains(strings.Join(lines, "\n"), "0 errors") {
		t.Fatalf("compile output not logged: %v", lines)
	}
	if err := r.Start(c, t.TempDir(), 1337, nil); err == nil {
		t.Fatal("a second launch started while running")
	}
	r.Stop()
	waitFor(t, r, Stopped)
	if strings.Join(*started, ",") != "dm,server,client" {
		t.Fatalf("started %v", *started)
	}
}

func TestRunnerStopsOnCompileFailure(t *testing.T) {
	var listening atomic.Bool
	r, started := fakeRunner(t, &listening)
	c := Commands{Compile: []string{"dm", "compile-fail"}, Server: []string{"server", "server"}, Client: []string{"client", "client"}}
	if err := r.Start(c, t.TempDir(), 1337, nil); err != nil {
		t.Fatal(err)
	}
	lines := waitFor(t, r, Failed)
	if !strings.Contains(strings.Join(lines, "\n"), "error: bad") || len(*started) != 1 {
		t.Fatalf("log %v started %v", lines, *started)
	}
}
