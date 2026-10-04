package ui

import (
	"hash/fnv"
	"sort"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

const presenceStyleSlotCount = 8

// PresenceTimeout is the desktop lifetime of an observed cursor or participant.
// Presence is lossy and is not an authoritative connected-member roster.
const PresenceTimeout = 30 * time.Second

type ObservedPresence struct {
	Presence   protocol.ParticipantPresence
	ObservedAt time.Time
}

type PresenceOverlay struct {
	ActorID   model.ActorID
	Label     string
	PixelX    int
	PixelY    int
	StyleSlot uint32
	Selection *PresenceSelectionOverlay
}

type PresenceSelectionOverlay struct {
	PixelX1 int
	PixelY1 int
	PixelX2 int
	PixelY2 int
}

func (client *SessionClient) expirePresenceLocked(now time.Time) {
	for actorID, observed := range client.participants {
		if now.Sub(observed.ObservedAt) > PresenceTimeout {
			delete(client.participants, actorID)
		}
	}
}

func BuildPresenceOverlays(entries []ObservedPresence, activeLevel, iconSize, participantCap int, timeout time.Duration, now time.Time) []PresenceOverlay {
	if activeLevel < 1 || iconSize < 1 || participantCap < 1 || timeout <= 0 {
		return nil
	}
	visible := make([]ObservedPresence, 0, len(entries))
	for _, entry := range entries {
		cursor := entry.Presence.Cursor
		if cursor == nil || cursor.Z != activeLevel || cursor.X < 1 || cursor.Y < 1 || now.Sub(entry.ObservedAt) > timeout {
			continue
		}
		visible = append(visible, entry)
	}
	sort.Slice(visible, func(left, right int) bool {
		return visible[left].Presence.ActorID < visible[right].Presence.ActorID
	})
	if len(visible) > participantCap {
		visible = visible[:participantCap]
	}
	overlays := make([]PresenceOverlay, len(visible))
	for index, entry := range visible {
		label := entry.Presence.DisplayName
		if label == "" {
			label = string(entry.Presence.ActorID)
		}
		overlay := PresenceOverlay{
			ActorID:   entry.Presence.ActorID,
			Label:     label,
			PixelX:    (entry.Presence.Cursor.X - 1) * iconSize,
			PixelY:    (entry.Presence.Cursor.Y - 1) * iconSize,
			StyleSlot: presenceStyleSlot(entry.Presence.ActorID),
		}
		selection := entry.Presence.Selection
		if selection != nil && selection.Min.Z == activeLevel && selection.Max.Z == activeLevel {
			overlay.Selection = &PresenceSelectionOverlay{
				PixelX1: (selection.Min.X - 1) * iconSize,
				PixelY1: (selection.Min.Y - 1) * iconSize,
				PixelX2: selection.Max.X * iconSize,
				PixelY2: selection.Max.Y * iconSize,
			}
		}
		overlays[index] = overlay
	}
	return overlays
}

func presenceStyleSlot(actorID model.ActorID) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(actorID))
	return hash.Sum32() % presenceStyleSlotCount
}
