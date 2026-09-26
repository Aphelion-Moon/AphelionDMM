// Package resources provides byte-based admission for work that keeps large
// immutable edit proposals and their temporary indexes in memory.
package resources

import (
	"fmt"
	"math"
	"sync"
)

// Budget admits reservations against either a configured cap or a fraction of
// currently available host memory. A single Budget should be shared by the
// owners whose concurrent work competes for the same process memory.
type Budget struct {
	named    map[*Reservation]string
	mu       sync.Mutex
	used     uint64
	limit    uint64
	host     bool
	fraction uint64
}

// NewFixedBudget creates a configurable budget useful for applications with an
// operator-selected limit and for deterministic admission tests.
func NewFixedBudget(limit uint64) *Budget {
	return &Budget{limit: limit}
}

// NewHostBudget admits at most maxAvailableBytes and at most one fraction of
// memory currently available to the host. A zero cap leaves only the fraction.
// A fraction of 3 means the operation may reserve no more than one third of
// current free memory, leaving room for the map editor and operating system.
func NewHostBudget(maxAvailableBytes, fraction uint64) *Budget {
	if fraction == 0 {
		fraction = 3
	}
	return &Budget{limit: maxAvailableBytes, host: true, fraction: fraction}
}

var (
	defaultOnce   sync.Once
	defaultBudget *Budget
)

// DefaultBudget is the process-shared host-backed budget used by editor flows.
func DefaultBudget() *Budget {
	defaultOnce.Do(func() { defaultBudget = NewHostBudget(0, 3) })
	return defaultBudget
}

// Reservation accounts for memory while a candidate or rollback is live.
type Reservation struct {
	budget   *Budget
	bytes    uint64
	once     sync.Once
	released bool
}

// AdmissionError reports the estimated requirement and current admission.
type AdmissionError struct {
	Incremental   bool
	Reservations  map[string]uint64
	Needed        uint64
	Available     uint64
	HostAvailable uint64
}

func (e *AdmissionError) Error() string {
	if e.HostAvailable != 0 {
		return fmt.Sprintf("edit needs about %s of temporary memory; current admission is %s (host reports %s available)", formatBytes(e.Needed), formatBytes(e.Available), formatBytes(e.HostAvailable))
	}
	return fmt.Sprintf("edit needs about %s of temporary memory; currently configured admission is %s", formatBytes(e.Needed), formatBytes(e.Available))
}

// Reserve accounts for needed bytes before the caller allocates proposal data.
func (b *Budget) Reserve(needed uint64) (*Reservation, error) {
	if b == nil {
		b = DefaultBudget()
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	limit, hostAvailable, err := b.limitLocked()
	if err != nil {
		return nil, err
	}
	if b.used >= limit {
		return nil, b.admissionError(needed, 0, hostAvailable, false)
	}
	remaining := limit - b.used
	if needed > remaining || needed > math.MaxUint64-b.used {
		return nil, b.admissionError(needed, remaining, hostAvailable, false)
	}
	b.used += needed
	return &Reservation{budget: b, bytes: needed}, nil
}

// Resize adjusts an existing reservation to a refined estimate. Call it before
// allocating the newly discovered additional data.
func (r *Reservation) Resize(needed uint64) error {
	if r == nil || r.budget == nil {
		return fmt.Errorf("memory reservation is unavailable")
	}
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return fmt.Errorf("memory reservation has already been released")
	}
	if needed == r.bytes {
		return nil
	}
	if needed < r.bytes {
		difference := r.bytes - needed
		if difference <= b.used {
			b.used -= difference
		} else {
			b.used = 0
		}
		r.bytes = needed
		return nil
	}
	limit, hostAvailable, err := b.limitLocked()
	if err != nil {
		return err
	}
	difference := needed - r.bytes
	available := uint64(0)
	if b.used < limit {
		available = limit - b.used
	}
	if difference > available || difference > math.MaxUint64-b.used {
		return b.admissionError(difference, available, hostAvailable, true)
	}
	b.used += difference
	r.bytes = needed
	return nil
}

// Release returns reserved capacity. It is safe to call more than once.
func (r *Reservation) Release() {
	if r == nil || r.budget == nil {
		return
	}
	r.once.Do(func() {
		r.budget.mu.Lock()
		defer r.budget.mu.Unlock()
		r.released = true
		delete(r.budget.named, r)
		if r.bytes <= r.budget.used {
			r.budget.used -= r.bytes
		} else {
			r.budget.used = 0
		}
	})
}

// Label attaches diagnostic ownership without changing admission or lifetime.
func (r *Reservation) Label(owner, purpose string) {
	if r == nil || r.budget == nil {
		return
	}
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return
	}
	if b.named == nil {
		b.named = map[*Reservation]string{}
	}
	b.named[r] = owner + ": " + purpose
}

func (b *Budget) admissionError(needed, available, host uint64, incremental bool) *AdmissionError {
	named := map[string]uint64{}
	var accounted uint64
	for r, name := range b.named {
		named[name] += r.bytes
		accounted += r.bytes
	}
	if b.used > accounted {
		named["other reservations"] = b.used - accounted
	}
	return &AdmissionError{Needed: needed, Available: available, HostAvailable: host, Incremental: incremental, Reservations: named}
}

// Bytes reports the reserved amount.
func (r *Reservation) Bytes() uint64 {
	if r == nil {
		return 0
	}
	if r.budget != nil {
		r.budget.mu.Lock()
		defer r.budget.mu.Unlock()
	}
	return r.bytes
}

// Used reports the amount currently reserved from this budget.
func (b *Budget) Used() uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

func (b *Budget) limitLocked() (uint64, uint64, error) {
	limit := b.limit
	var hostAvailable uint64
	if b.host {
		available, err := hostAvailableMemory()
		if err != nil {
			return 0, 0, fmt.Errorf("determine available memory for large edit: %w", err)
		}
		hostAvailable = available
		admission := available / b.fraction
		if limit == 0 || admission < limit {
			limit = admission
		}
	}
	return limit, hostAvailable, nil
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
