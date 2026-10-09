package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

type PresenceUpdate struct {
	Sequence  uint64
	Cursor    *model.Coord
	Selection *protocol.PresenceSelection
	Status    string
}

type Presence struct {
	ActorID     model.ActorID
	DisplayName string
	Sequence    uint64
	Cursor      *model.Coord
	Selection   *protocol.PresenceSelection
	Status      string
	UpdatedAt   time.Time
	// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
	CursorColor *int
	// APHELION EDIT ADDITION END
}

type PresenceManager struct {
	mutex   sync.Mutex
	timeout time.Duration
	current map[model.ActorID]Presence
	// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
	colors map[model.ActorID]int // in-memory only; cleared on Remove
	// APHELION EDIT ADDITION END
	subscribers map[uint64]chan Presence
	nextID      uint64
	telemetry   *collabtelemetry.Telemetry
}

func NewPresenceManager(timeout time.Duration) *PresenceManager {
	return NewPresenceManagerWithTelemetry(timeout, nil)
}

func NewPresenceManagerWithTelemetry(timeout time.Duration, observability *collabtelemetry.Telemetry) *PresenceManager {
	if timeout <= 0 {
		timeout = time.Minute
	}
	return &PresenceManager{
		timeout:     timeout,
		current:     make(map[model.ActorID]Presence),
		colors:      make(map[model.ActorID]int),
		subscribers: make(map[uint64]chan Presence),
		telemetry:   observability,
	}
}

func (manager *PresenceManager) Update(principal Principal, update PresenceUpdate) error {
	return manager.updateAt(principal, update, time.Now().UTC())
}

func (manager *PresenceManager) updateAt(principal Principal, update PresenceUpdate, updatedAt time.Time) error {
	if update.Sequence == 0 {
		return fmt.Errorf("presence sequence must be positive")
	}
	if update.Status == "" || len(update.Status) > 128 {
		return fmt.Errorf("presence status length is %d, want 1..128", len(update.Status))
	}
	if update.Cursor != nil && (update.Cursor.X < 1 || update.Cursor.Y < 1 || update.Cursor.Z < 1) {
		return fmt.Errorf("presence cursor coordinates must be positive")
	}
	if err := validatePresenceSelection(update.Selection); err != nil {
		return err
	}

	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if current, exists := manager.current[principal.ActorID()]; exists && update.Sequence <= current.Sequence {
		return fmt.Errorf("presence sequence is %d, current is %d", update.Sequence, current.Sequence)
	}
	value := Presence{
		ActorID:     principal.ActorID(),
		DisplayName: principal.DisplayName(),
		Sequence:    update.Sequence,
		Cursor:      cloneCoord(update.Cursor),
		Selection:   cloneSelection(update.Selection),
		Status:      update.Status,
		UpdatedAt:   updatedAt,
	}
	// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
	value.CursorColor = manager.colorLocked(value.ActorID)
	// APHELION EDIT ADDITION END
	manager.current[value.ActorID] = value
	manager.publish(value)
	return nil
}

func (manager *PresenceManager) Remove(actorID model.ActorID) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	delete(manager.current, actorID)
	// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
	delete(manager.colors, actorID)
	// APHELION EDIT ADDITION END
}

func (manager *PresenceManager) Rename(principal Principal) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	current, exists := manager.current[principal.ActorID()]
	if !exists {
		return
	}
	current.DisplayName = principal.DisplayName()
	manager.current[principal.ActorID()] = current
	manager.publish(current)
}

func (manager *PresenceManager) Expire(now time.Time) int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	removed := 0
	for actorID, presence := range manager.current {
		if now.Sub(presence.UpdatedAt) >= manager.timeout {
			delete(manager.current, actorID)
			removed++
		}
	}
	return removed
}

func (manager *PresenceManager) Subscribe(buffer int) ([]Presence, <-chan Presence, func()) {
	if buffer < 1 {
		buffer = 1
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	manager.nextID++
	id := manager.nextID
	updates := make(chan Presence, buffer)
	manager.subscribers[id] = updates
	snapshot := make([]Presence, 0, len(manager.current))
	for _, presence := range manager.current {
		presence.Cursor = cloneCoord(presence.Cursor)
		presence.Selection = cloneSelection(presence.Selection)
		presence.CursorColor = cloneColor(presence.CursorColor)
		snapshot = append(snapshot, presence)
	}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			manager.mutex.Lock()
			defer manager.mutex.Unlock()
			if subscriber, exists := manager.subscribers[id]; exists {
				delete(manager.subscribers, id)
				close(subscriber)
			}
		})
	}
	return snapshot, updates, cancel
}

func (manager *PresenceManager) publish(value Presence) {
	for _, subscriber := range manager.subscribers {
		copy := value
		copy.Cursor = cloneCoord(value.Cursor)
		copy.Selection = cloneSelection(value.Selection)
		copy.CursorColor = cloneColor(value.CursorColor)
		select {
		case subscriber <- copy:
		default:
			if manager.telemetry != nil {
				manager.telemetry.PresenceDropped(context.Background())
			}
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- copy:
			default:
			}
		}
	}
}

func cloneCoord(coord *model.Coord) *model.Coord {
	if coord == nil {
		return nil
	}
	copy := *coord
	return &copy
}

func cloneSelection(selection *protocol.PresenceSelection) *protocol.PresenceSelection {
	if selection == nil {
		return nil
	}
	copy := *selection
	return &copy
}

func validatePresenceSelection(selection *protocol.PresenceSelection) error {
	if selection == nil {
		return nil
	}
	if selection.Min.X < 1 || selection.Min.Y < 1 || selection.Min.Z < 1 || selection.Max.X < 1 || selection.Max.Y < 1 || selection.Max.Z < 1 {
		return fmt.Errorf("presence selection coordinates must be positive")
	}
	if selection.Min.Z != selection.Max.Z || selection.Min.X > selection.Max.X || selection.Min.Y > selection.Max.Y {
		return fmt.Errorf("presence selection bounds must be normalized on one level")
	}
	// Wire decoders retain the v1 area cap. The shared manager stores only a
	// bounding box, so V2 selections are bounded by valid map dimensions.
	if selection.Max.X > model.MaxMapDimension || selection.Max.Y > model.MaxMapDimension || selection.Max.Z > model.MaxMapDimension {
		return fmt.Errorf("presence selection exceeds supported map dimensions")
	}
	return nil
}

// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR

// SetCursorColor records the actor's palette index and republishes its current
// presence, if any. The caller validates the index. The colour is ephemeral and
// is never persisted or hashed.
func (manager *PresenceManager) SetCursorColor(principal Principal, index int) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	manager.colors[principal.ActorID()] = index
	current, exists := manager.current[principal.ActorID()]
	if !exists {
		return
	}
	current.CursorColor = manager.colorLocked(principal.ActorID())
	manager.current[principal.ActorID()] = current
	manager.publish(current)
}

func (manager *PresenceManager) colorLocked(actorID model.ActorID) *int {
	index, ok := manager.colors[actorID]
	if !ok {
		return nil
	}
	return &index
}

func cloneColor(index *int) *int {
	if index == nil {
		return nil
	}
	copy := *index
	return &copy
}

// APHELION EDIT ADDITION END
