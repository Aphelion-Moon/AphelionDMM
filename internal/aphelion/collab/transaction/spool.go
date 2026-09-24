package transaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sync"
)

// Budget accounts aggregate admitted bytes across connections. Configure this
// from the host's resource policy; it is not a maximum number of edited tiles.
type Budget struct {
	mutex       sync.Mutex
	limit, used int64
}

type AdmissionError struct{ Required, Available int64 }

func (e *AdmissionError) Error() string {
	return fmt.Sprintf("edit needs %d budget bytes; %d available", e.Required, e.Available)
}

func NewBudget(bytes int64) *Budget { return &Budget{limit: bytes} }
func (b *Budget) Used() int64       { b.mutex.Lock(); defer b.mutex.Unlock(); return b.used }

// Reserve keeps a resource charge until its owner has finished all work. The
// returned release is idempotent, including cancellation and error paths.
func (b *Budget) Reserve(n int64) (func(), error) {
	if b == nil {
		return nil, fmt.Errorf("transaction admission budget is required")
	}
	if err := b.acquire(n); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { b.release(n) }) }, nil
}
func (b *Budget) acquire(n int64) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if n < 1 || b.limit < 1 || n > b.limit-b.used {
		return &AdmissionError{Required: n, Available: b.limit - b.used}
	}
	b.used += n
	return nil
}
func (b *Budget) release(n int64) { b.mutex.Lock(); defer b.mutex.Unlock(); b.used -= n }

type Spool struct {
	file          *os.File
	path          string
	size, written int64
	next          uint64
	want          string
	hash          hash.Hash
	budget        *Budget
	closed        bool
}

// NewSpool takes only a trusted local directory. Peer-provided paths are never
// accepted. A failed upload owns no document mutation and Close releases it.
func NewSpool(directory string, size int64, digest string, budget *Budget) (*Spool, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest {
		return nil, fmt.Errorf("invalid transaction digest")
	}
	if budget == nil {
		return nil, fmt.Errorf("transaction admission budget is required")
	}
	if err := budget.acquire(size); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(directory, "aphelion-transaction-*")
	if err != nil {
		budget.release(size)
		return nil, err
	}
	return &Spool{file: f, path: f.Name(), size: size, want: digest, hash: sha256.New(), budget: budget}, nil
}

func (s *Spool) Append(ctx context.Context, index uint64, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || index != s.next || len(data) == 0 || int64(len(data)) > s.size-s.written {
		return fmt.Errorf("invalid transaction chunk sequence or size")
	}
	n, err := s.file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	_, _ = s.hash.Write(data)
	s.written += int64(n)
	s.next++
	return nil
}

type Body struct {
	path     string
	Size     int64
	Digest   string
	budget   *Budget
	once     sync.Once
	closeErr error
}

func (b *Body) Open() (io.ReadCloser, error) { return os.Open(b.path) }
func (b *Body) Close() error {
	b.once.Do(func() { b.closeErr = os.Remove(b.path); b.budget.release(b.Size) })
	return b.closeErr
}

func (s *Spool) Finish() (*Body, error) {
	if s.closed || s.written != s.size || hex.EncodeToString(s.hash.Sum(nil)) != s.want {
		return nil, fmt.Errorf("transaction body is incomplete or has a digest mismatch")
	}
	if err := s.file.Close(); err != nil {
		return nil, err
	}
	s.closed = true
	return &Body{path: s.path, Size: s.size, Digest: s.want, budget: s.budget}, nil
}
func (s *Spool) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := errors.Join(s.file.Close(), os.Remove(s.path))
	s.budget.release(s.size)
	return err
}
