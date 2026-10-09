package playtest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Phase is where a launch is.
type Phase int

const (
	Idle Phase = iota
	Compiling
	Starting
	Running
	Failed
	Stopped
)

func (p Phase) String() string {
	return [...]string{"Idle", "Compiling", "Starting server", "Running", "Failed", "Stopped"}[p]
}

const logLimit = 400

// Runner owns one launch at a time. Methods are safe from any goroutine.
type Runner struct {
	mu     sync.Mutex
	phase  Phase
	lines  []string
	cancel context.CancelFunc
	server *exec.Cmd

	// start and dial are replaced in tests.
	start func(ctx context.Context, argv []string, dir string) *exec.Cmd
	dial  func(port int) bool
}

func NewRunner() *Runner {
	return &Runner{
		start: func(ctx context.Context, argv []string, dir string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // no shell
			cmd.Dir = dir
			return cmd
		},
		dial: func(port int) bool {
			conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
			}
			return err == nil
		},
	}
}

// Snapshot returns the phase and a copy of the log.
func (r *Runner) Snapshot() (Phase, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.phase, append([]string(nil), r.lines...)
}

// Busy reports a launch in progress or a server still running.
func (r *Runner) Busy() bool {
	phase, _ := r.Snapshot()
	return phase == Compiling || phase == Starting || phase == Running
}

func (r *Runner) logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
	if len(r.lines) > logLimit {
		r.lines = r.lines[len(r.lines)-logLimit:]
	}
}

func (r *Runner) setPhase(p Phase) {
	r.mu.Lock()
	r.phase = p
	r.mu.Unlock()
}

// Start runs the launch on its own goroutine; changed is called after every
// phase change (callers hop to their UI thread).
func (r *Runner) Start(c Commands, dir string, port int, changed func()) error {
	if r.Busy() {
		return fmt.Errorf("a playtest is already running; stop it first")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.lines, r.cancel, r.server = nil, cancel, nil
	r.mu.Unlock()
	notify := func(p Phase) {
		r.setPhase(p)
		if changed != nil {
			changed()
		}
	}
	go func() {
		if len(c.Compile) != 0 {
			notify(Compiling)
			r.logf("Compiling: %s", c.Compile[1])
			if err := r.runLogged(ctx, c.Compile, dir); err != nil {
				r.logf("Compile failed: %v", err)
				notify(Failed)
				return
			}
		} else {
			r.logf("Build is current; skipping compile.")
		}
		notify(Starting)
		server := r.start(ctx, c.Server, dir)
		if err := server.Start(); err != nil {
			r.logf("Could not start DreamDaemon: %v", err)
			notify(Failed)
			return
		}
		r.mu.Lock()
		r.server = server
		r.mu.Unlock()
		exited := make(chan error, 1)
		go func() { exited <- server.Wait() }()
		r.logf("DreamDaemon started; waiting for port %d.", port)
		deadline := time.Now().Add(ServerWait)
		for !r.dial(port) {
			select {
			case err := <-exited:
				r.logf("DreamDaemon exited before accepting connections: %v", err)
				notify(Failed)
				return
			case <-ctx.Done():
				notify(Stopped)
				return
			case <-time.After(time.Second):
			}
			if time.Now().After(deadline) {
				r.logf("DreamDaemon did not open port %d in %v.", port, ServerWait)
				_ = server.Process.Kill()
				notify(Failed)
				return
			}
		}
		if err := r.start(context.Background(), c.Client, dir).Start(); err != nil {
			r.logf("Server is up, but DreamSeeker could not start: %v. Connect to byond://127.0.0.1:%d.", err, port)
		} else {
			r.logf("Connected DreamSeeker to byond://127.0.0.1:%d.", port)
		}
		notify(Running)
		err := <-exited
		if ctx.Err() != nil {
			r.logf("Server stopped.")
			notify(Stopped)
		} else {
			r.logf("Server exited: %v", err)
			notify(Stopped)
		}
	}()
	return nil
}

func (r *Runner) runLogged(ctx context.Context, argv []string, dir string) error {
	cmd := r.start(ctx, argv, dir)
	hideConsole(cmd) // dm.exe is a console program; its output goes to the log
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	r.copyLines(out)
	return cmd.Wait()
}

func (r *Runner) copyLines(out io.Reader) {
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		r.logf("%s", scanner.Text())
	}
}

// Stop ends a compile or the server.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel, server := r.cancel, r.server
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server != nil && server.Process != nil {
		_ = server.Process.Kill()
	}
}
