package iconassets

import "context"

type Request struct {
	Context   context.Context
	Key, Path string
}
type Result struct {
	Context context.Context
	Key     string
	Decoded *Decoded
	Err     error
}

// One worker retains at most one decoded result. The unbuffered handoff means a
// cancelled environment cannot strand decoded memory in an abandoned channel.
type Worker struct {
	requests  chan Request
	Results   chan Result
	available chan struct{}
}

func NewWorker() *Worker { return newWorker(Decode) }
func newWorker(decode func(context.Context, string) (*Decoded, error)) *Worker {
	// Keep at most one request in or at the worker. The cache owns the larger
	// priority queue so visible demand can claim the next slot.
	w := &Worker{requests: make(chan Request, 1), Results: make(chan Result), available: make(chan struct{}, 1)}
	w.available <- struct{}{}
	go func() {
		for request := range w.requests {
			func() {
				defer func() { w.available <- struct{}{} }()
				if request.Context.Err() != nil {
					return
				}
				decoded, err := decode(request.Context, request.Path)
				result := Result{Context: request.Context, Key: request.Key, Decoded: decoded, Err: err}
				select {
				case w.Results <- result:
				case <-request.Context.Done():
					decoded.Release()
				}
			}()
		}
	}()
	return w
}
func (w *Worker) Submit(request Request) bool {
	select {
	case <-w.available:
	default:
		return false
	}
	select {
	case w.requests <- request:
		return true
	default:
		w.available <- struct{}{}
		return false
	}
}

// Close is called only after submissions stop. Per-environment cancellation is
// separate so project switches keep using the same bounded worker.
func (w *Worker) Close() { close(w.requests) }
