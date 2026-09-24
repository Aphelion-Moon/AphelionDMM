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
	requests chan Request
	Results  chan Result
}

func NewWorker() *Worker { return newWorker(Decode) }
func newWorker(decode func(context.Context, string) (*Decoded, error)) *Worker {
	w := &Worker{requests: make(chan Request, 64), Results: make(chan Result)}
	go func() {
		for request := range w.requests {
			if request.Context.Err() != nil {
				continue
			}
			decoded, err := decode(request.Context, request.Path)
			result := Result{Context: request.Context, Key: request.Key, Decoded: decoded, Err: err}
			select {
			case w.Results <- result:
			case <-request.Context.Done():
				decoded.Release()
			}
		}
	}()
	return w
}
func (w *Worker) Submit(request Request) bool {
	select {
	case w.requests <- request:
		return true
	default:
		return false
	}
}

// Close is called only after submissions stop. Per-environment cancellation is
// separate so project switches keep using the same bounded worker.
func (w *Worker) Close() { close(w.requests) }
