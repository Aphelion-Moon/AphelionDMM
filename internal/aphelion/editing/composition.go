package editing

import (
	"fmt"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dm"
)

type PasteMode uint8

const (
	OnlyOverwriteWithData PasteMode = iota
	ApplyOver
	ReplaceIncludingBlanks
)

type Channel uint8

const (
	Areas Channel = iota
	Turfs
	Objects
	Mobs
	channelCount
	AllChannels uint8 = (1 << channelCount) - 1
)

type IntentAction uint8

const (
	Keep IntentAction = iota
	Set
	Clear
)

// Keep is the zero value. Only explicitly included coordinates have intents;
// missing coordinates never acquire writes from a bounding rectangle.
type ChannelIntent struct {
	Action IntentAction
	Data   []model.PrefabState
}

type TileIntent [channelCount]ChannelIntent

type PastePolicy struct {
	Mode     PasteMode
	Channels uint8
}

func ChannelForPath(path string) Channel {
	switch {
	case dm.IsPath(path, "/area"):
		return Areas
	case dm.IsPath(path, "/turf"):
		return Turfs
	case dm.IsPath(path, "/mob"):
		return Mobs
	default:
		return Objects // Unknown types are retained as collection data.
	}
}

func (policy PastePolicy) Writes(channel Channel, intent ChannelIntent) bool {
	return policy.Channels&(1<<channel) != 0 &&
		(intent.Action == Set && len(intent.Data) != 0 || intent.Action == Clear && policy.Mode == ReplaceIncludingBlanks)
}

// Suppresses is shared by the composer and renderer. The caller supplies only
// source intents whose singleton conflicts have been checked for this target.
func (policy PastePolicy) Suppresses(path string, source TileIntent, visible func(string) bool) bool {
	channel := ChannelForPath(path)
	return visible(path) && policy.Writes(channel, source[channel]) &&
		!(policy.Mode == ApplyOver && channel >= Objects && source[channel].Action == Set)
}

func CheckComposition(before model.TileState, source TileIntent, policy PastePolicy, visible func(string) bool) error {
	if policy.Mode > ReplaceIncludingBlanks || visible == nil {
		return fmt.Errorf("invalid paste policy")
	}
	for channel, intent := range source {
		if !policy.Writes(Channel(channel), intent) {
			continue
		}
		if channel < int(Objects) && len(intent.Data) > 1 {
			return fmt.Errorf("paste has multiple area or turf values")
		}
		if channel < int(Objects) && intent.Action == Set {
			for _, retained := range before.Prefabs {
				if ChannelForPath(retained.Path) == Channel(channel) && !visible(retained.Path) {
					return fmt.Errorf("paste would replace a hidden area or turf at the destination")
				}
			}
		}
	}
	return nil
}

// ComposeTile owns its result and preserves order, identities, duplicate
// objects and unknown variables. Source data already owns copied identities.
func ComposeTile(before model.TileState, source TileIntent, policy PastePolicy, visible func(string) bool) (model.TileState, error) {
	if err := CheckComposition(before, source, policy, visible); err != nil {
		return model.TileState{}, err
	}
	after := model.TileState{}
	for _, prefab := range before.Prefabs {
		if !policy.Suppresses(prefab.Path, source, visible) {
			after.Prefabs = append(after.Prefabs, clonePrefabState(prefab))
		}
	}
	for channel, intent := range source {
		if !policy.Writes(Channel(channel), intent) || intent.Action != Set {
			continue
		}
		for _, prefab := range intent.Data {
			after.Prefabs = append(after.Prefabs, clonePrefabState(prefab))
		}
	}
	return after, nil
}
