package uistage

import (
	"errors"
	"fmt"
	"os"
	"runtime/trace"
	"sync"
	"time"
)

// Recording owns one bounded trace session and its exclusive output file.
type Recording struct {
	file     *os.File
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	writeErr error
	err      error
}

// RecordingDuration keeps ordinary captures short while allowing a whole slow
// environment import and subsequent map open in one bounded recording.
func RecordingDuration(value string) (time.Duration, error) {
	if value == "" {
		return 30 * time.Second, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration > 5*time.Minute {
		return 0, fmt.Errorf("trace duration must be positive and at most five minutes")
	}
	return duration, nil
}

func StartFile(path string, duration time.Duration) (*Recording, error) {
	if duration <= 0 || duration > 5*time.Minute {
		return nil, fmt.Errorf("trace duration must be positive and at most five minutes")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	r := &Recording{file: file, done: make(chan struct{})}
	if err := trace.Start(r); err != nil {
		_ = file.Close()
		return nil, err
	}
	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-timer.C:
			_ = r.Close()
		case <-r.done:
		}
	}()
	return r, nil
}

func (r *Recording) Write(data []byte) (int, error) {
	n, err := r.file.Write(data)
	if err != nil {
		r.mu.Lock()
		if r.writeErr == nil {
			r.writeErr = err
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *Recording) Done() <-chan struct{} { return r.done }

// Close waits for trace buffers to flush, including when the deadline wins.
func (r *Recording) Close() error {
	r.once.Do(func() {
		trace.Stop()
		r.mu.Lock()
		r.err = errors.Join(r.writeErr, r.file.Close())
		r.mu.Unlock()
		close(r.done)
	})
	return r.err
}
