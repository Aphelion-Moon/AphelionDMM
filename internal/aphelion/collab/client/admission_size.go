package client

import (
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/model"
)

// operationAdmissionBytes counts the existing Operation JSON representation
// without building/sorting it on the UI thread. Actual transport encoding and
// validation remain unchanged. Keep this in sync with model.Operation,
// TileChange, Coord, TileState and PrefabState; differential tests compare it
// against encoding/json, including nil collections and escaped/invalid UTF-8.
func operationAdmissionBytes(operation model.Operation) uint64 {
	size := uint64(len(`{"protocol_version":,"document_id":,"actor_id":,"operation_id":,"base_revision":,"environment_hash":,"base_map_hash":,"kind":,"changes":}`))
	size = estimateCaptureAdd(size, jsonUnsignedBytes(uint64(operation.ProtocolVersion)))
	size = estimateCaptureAdd(size, jsonUnsignedBytes(uint64(operation.BaseRevision)))
	for _, value := range [...]string{string(operation.DocumentID), string(operation.ActorID), string(operation.OperationID), operation.EnvironmentHash, operation.BaseMapHash, string(operation.Kind)} {
		size = estimateCaptureAdd(size, jsonStringBytes(value))
	}
	if operation.InverseOf != nil {
		size = estimateCaptureAdd(size, uint64(len(`,"inverse_of":`)))
		size = estimateCaptureAdd(size, jsonStringBytes(string(*operation.InverseOf)))
	}
	if operation.Changes == nil {
		return estimateCaptureAdd(size, 4)
	}
	size = estimateCaptureAdd(size, 2)
	for i, change := range operation.Changes {
		if i != 0 {
			size = estimateCaptureAdd(size, 1)
		}
		size = estimateCaptureAdd(size, uint64(len(`{"coord":,"before":,"after":}`)+len(`{"x":,"y":,"z":}`)))
		for _, coordinate := range [...]int{change.Coord.X, change.Coord.Y, change.Coord.Z} {
			size = estimateCaptureAdd(size, jsonSignedBytes(coordinate))
		}
		size = estimateCaptureAdd(size, tileStateJSONBytes(change.Before))
		size = estimateCaptureAdd(size, tileStateJSONBytes(change.After))
	}
	return size
}

func tileStateJSONBytes(state model.TileState) uint64 {
	size := uint64(len(`{"prefabs":}`))
	if state.Prefabs == nil {
		return size + 4
	}
	size += 2
	for i, prefab := range state.Prefabs {
		if i != 0 {
			size = estimateCaptureAdd(size, 1)
		}
		size = estimateCaptureAdd(size, uint64(len(`{"stable_id":,"path":,"vars":}`)))
		size = estimateCaptureAdd(size, jsonStringBytes(string(prefab.StableID)))
		size = estimateCaptureAdd(size, jsonStringBytes(prefab.Path))
		if prefab.Vars == nil {
			size = estimateCaptureAdd(size, 4)
			continue
		}
		size = estimateCaptureAdd(size, 2)
		if len(prefab.Vars) > 0 {
			size = estimateCaptureAdd(size, uint64(len(prefab.Vars)-1))
		}
		for name, value := range prefab.Vars {
			size = estimateCaptureAdd(size, 1)
			size = estimateCaptureAdd(size, jsonStringBytes(name))
			size = estimateCaptureAdd(size, jsonStringBytes(value))
		}
	}
	return size
}

func jsonUnsignedBytes(value uint64) uint64 {
	size := uint64(1)
	for value >= 10 {
		size++
		value /= 10
	}
	return size
}

func jsonSignedBytes(value int) uint64 {
	if value < 0 {
		return 1 + jsonUnsignedBytes(uint64(-(value+1))+1)
	}
	return jsonUnsignedBytes(uint64(value))
}

func jsonStringBytes(value string) uint64 {
	size := uint64(len(value)) + 2
	for i := 0; i < len(value); {
		b := value[i]
		if b < utf8.RuneSelf {
			switch b {
			case '"', '\\', '\b', '\f', '\n', '\r', '\t':
				size = estimateCaptureAdd(size, 1)
			case '<', '>', '&':
				size = estimateCaptureAdd(size, 5)
			default:
				if b < 0x20 {
					size = estimateCaptureAdd(size, 5)
				}
			}
			i++
			continue
		}
		r, width := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && width == 1 {
			size = estimateCaptureAdd(size, 5)
		} else if r == '\u2028' || r == '\u2029' {
			size = estimateCaptureAdd(size, 3)
		}
		i += width
	}
	return size
}
