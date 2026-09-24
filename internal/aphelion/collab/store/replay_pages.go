package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/transaction"
)

// MaxReplayPageInlineBytes bounds V1 accepted bodies returned in one page.
// V2 pages contain only metadata; their tile body stays in transaction chunks.
const MaxReplayPageInlineBytes = protocol.MaxMessageBytes

func ValidateReplayPageRequest(after, through model.Revision, limit int) error {
	if after > through {
		return ErrReplayRevisionRange
	}
	if limit < 1 || limit > MaxReplayPageEntries {
		return ErrReplayPageLimit
	}
	return nil
}

func CheckReplaySnapshot(expected *model.Revision, actual model.Revision) error {
	if expected != nil && *expected != actual {
		return ErrReplayCheckpointChanged
	}
	return nil
}

func ValidateVersionedReplayEntry(documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, headerBytes []byte, digest string, bodyBytes, changeCount int64) (ReplayEntry, error) {
	if len(headerBytes)+1 > transaction.MaxHeaderBytes || !validReplayHash(mapHash) || !validReplayHash(digest) || bodyBytes < 1 || changeCount < 1 || changeCount > model.MaxMapCells {
		return ReplayEntry{}, fmt.Errorf("versioned replay index metadata is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(headerBytes))
	decoder.DisallowUnknownFields()
	var header transaction.Header
	if err := decoder.Decode(&header); err != nil {
		return ReplayEntry{}, fmt.Errorf("decode replay transaction header: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ReplayEntry{}, fmt.Errorf("replay transaction header has trailing data")
	}
	operation := header.Operation
	if header.Version != transaction.Version || header.Kind != "accepted" || header.Count != changeCount || header.Revision != revision || header.AcceptedAt.IsZero() || header.MapHash != mapHash || operation.ProtocolVersion != model.ProtocolVersion || operation.DocumentID != documentID || operation.OperationID != operationID || len(operation.Changes) != 0 || header.SessionID != "" || header.MessageID != "" {
		return ReplayEntry{}, fmt.Errorf("replay transaction header differs from indexed operation metadata")
	}
	if err := operation.DocumentID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("replay transaction document identity: %w", err)
	}
	if err := operation.ActorID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("replay transaction actor identity: %w", err)
	}
	if err := operation.OperationID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("replay transaction operation identity: %w", err)
	}
	if !validReplayHash(operation.EnvironmentHash) || !validReplayHash(operation.BaseMapHash) {
		return ReplayEntry{}, fmt.Errorf("replay transaction operation hashes are invalid")
	}
	if operation.Kind != model.OperationKindTileChange && operation.Kind != model.OperationKindInverse {
		return ReplayEntry{}, fmt.Errorf("replay transaction operation kind is invalid")
	}
	if operation.Kind == model.OperationKindInverse && operation.InverseOf == nil {
		return ReplayEntry{}, fmt.Errorf("replay inverse transaction has no target")
	}
	if operation.InverseOf != nil {
		if err := operation.InverseOf.Validate(); err != nil {
			return ReplayEntry{}, fmt.Errorf("replay inverse target: %w", err)
		}
	}
	return ReplayEntry{
		Accepted: model.AcceptedOperation{Operation: operation, Revision: header.Revision, AcceptedAt: header.AcceptedAt},
		MapHash:  mapHash, StorageVersion: BulkTransactionVersion, BodyDigest: digest, BodyBytes: bodyBytes, ChangeCount: changeCount,
	}, nil
}

func ValidateLegacyReplayEntry(documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, encodedBytes int, accepted model.AcceptedOperation) (ReplayEntry, error) {
	if encodedBytes < 1 || encodedBytes > protocol.MaxMessageBytes || !validReplayHash(mapHash) || accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || accepted.AcceptedAt.IsZero() || len(accepted.Changes) < 1 || len(accepted.Changes) > protocol.MaxOperationChanges {
		return ReplayEntry{}, fmt.Errorf("legacy replay operation differs from bounded indexed metadata")
	}
	if err := documentID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("legacy replay document identity: %w", err)
	}
	if err := accepted.ActorID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("legacy replay actor identity: %w", err)
	}
	if err := operationID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("legacy replay operation identity: %w", err)
	}
	return ReplayEntry{Accepted: model.CloneAcceptedOperation(accepted), MapHash: mapHash, StorageVersion: LegacyTransactionVersion, ChangeCount: int64(len(accepted.Changes))}, nil
}

func validateMemoryReplayEntry(documentID model.DocumentID, operationID model.OperationID, revision model.Revision, mapHash string, encodedBytes int64, bulk bool, accepted model.AcceptedOperation) (ReplayEntry, error) {
	if !bulk {
		if encodedBytes > protocol.MaxMessageBytes {
			return ReplayEntry{}, fmt.Errorf("legacy in-memory replay operation exceeds the message limit")
		}
		return ValidateLegacyReplayEntry(documentID, operationID, revision, mapHash, int(encodedBytes), accepted)
	}
	if encodedBytes < 1 || !validReplayHash(mapHash) || accepted.DocumentID != documentID || accepted.OperationID != operationID || accepted.Revision != revision || accepted.AcceptedAt.IsZero() || len(accepted.Changes) < 1 || len(accepted.Changes) > model.MaxMapCells {
		return ReplayEntry{}, fmt.Errorf("bulk in-memory replay operation differs from indexed metadata")
	}
	if err := documentID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("bulk in-memory replay document identity: %w", err)
	}
	if err := accepted.ActorID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("bulk in-memory replay actor identity: %w", err)
	}
	if err := operationID.Validate(); err != nil {
		return ReplayEntry{}, fmt.Errorf("bulk in-memory replay operation identity: %w", err)
	}
	return ReplayEntry{Accepted: model.CloneAcceptedOperation(accepted), MapHash: mapHash, StorageVersion: LegacyTransactionVersion, ChangeCount: int64(len(accepted.Changes))}, nil
}

// replayAcceptedJSONSize counts an accepted operation structurally, without
// materializing its potentially multi-megabyte JSON encoding.
func replayAcceptedJSONSize(accepted model.AcceptedOperation) (int64, error) {
	size, fields := int64(2), 0 // object braces
	add := func(name string, valueBytes int64) {
		if fields != 0 {
			size++
		}
		fields++
		size += replayJSONStringSize(name) + 1 + valueBytes
	}
	add("protocol_version", replayUnsignedSize(uint64(accepted.ProtocolVersion)))
	add("document_id", replayJSONStringSize(string(accepted.DocumentID)))
	add("actor_id", replayJSONStringSize(string(accepted.ActorID)))
	add("operation_id", replayJSONStringSize(string(accepted.OperationID)))
	add("base_revision", replayUnsignedSize(uint64(accepted.BaseRevision)))
	add("environment_hash", replayJSONStringSize(accepted.EnvironmentHash))
	add("base_map_hash", replayJSONStringSize(accepted.BaseMapHash))
	add("kind", replayJSONStringSize(string(accepted.Kind)))
	add("changes", replayChangesSize(accepted.Changes))
	if accepted.InverseOf != nil {
		add("inverse_of", replayJSONStringSize(string(*accepted.InverseOf)))
	}
	add("revision", replayUnsignedSize(uint64(accepted.Revision)))
	timestamp, err := accepted.AcceptedAt.MarshalJSON()
	if err != nil {
		return 0, fmt.Errorf("encode in-memory replay timestamp: %w", err)
	}
	add("accepted_at", int64(len(timestamp)))
	return size, nil
}

func replayChangesSize(changes []model.TileChange) int64 {
	if changes == nil {
		return 4 // null
	}
	size := int64(2) // array brackets
	for index, change := range changes {
		if index != 0 {
			size++
		}
		size += replayTileChangeSize(change)
	}
	return size
}

func replayTileChangeSize(change model.TileChange) int64 {
	size, fields := int64(2), 0
	add := func(name string, valueBytes int64) {
		if fields != 0 {
			size++
		}
		fields++
		size += replayJSONStringSize(name) + 1 + valueBytes
	}
	coord := int64(2) + replayJSONStringSize("x") + 1 + replaySignedSize(change.Coord.X) + 1 + replayJSONStringSize("y") + 1 + replaySignedSize(change.Coord.Y) + 1 + replayJSONStringSize("z") + 1 + replaySignedSize(change.Coord.Z)
	add("coord", coord)
	add("before", replayTileStateSize(change.Before))
	add("after", replayTileStateSize(change.After))
	return size
}

func replayTileStateSize(state model.TileState) int64 {
	prefabs := int64(4) // null
	if state.Prefabs == nil {
		return 2 + replayJSONStringSize("prefabs") + 1 + prefabs
	}
	prefabs = 2 // array brackets
	for index, prefab := range state.Prefabs {
		if index != 0 {
			prefabs++
		}
		prefabs += replayPrefabSize(prefab)
	}
	return 2 + replayJSONStringSize("prefabs") + 1 + prefabs
}

func replayPrefabSize(prefab model.PrefabState) int64 {
	size, fields := int64(2), 0
	add := func(name string, valueBytes int64) {
		if fields != 0 {
			size++
		}
		fields++
		size += replayJSONStringSize(name) + 1 + valueBytes
	}
	add("stable_id", replayJSONStringSize(string(prefab.StableID)))
	add("path", replayJSONStringSize(prefab.Path))
	vars := int64(4) // null map
	if prefab.Vars != nil {
		vars = 2 // map braces
		count := 0
		for key, value := range prefab.Vars {
			if count != 0 {
				vars++
			}
			count++
			vars += replayJSONStringSize(key) + 1 + replayJSONStringSize(value)
		}
	}
	add("vars", vars)
	return size
}

func replayJSONStringSize(value string) int64 {
	size := int64(2) // quotes
	for index := 0; index < len(value); {
		current := value[index]
		switch current {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			size += 2
			index++
		case '<', '>', '&':
			size += 6
			index++
		default:
			if current < utf8.RuneSelf {
				if current < 0x20 {
					size += 6
				} else {
					size++
				}
				index++
				continue
			}
			runeValue, runeBytes := utf8.DecodeRuneInString(value[index:])
			if runeValue == utf8.RuneError && runeBytes == 1 {
				size += 6 // encoding/json replaces an invalid byte with \\ufffd.
				index++
				continue
			}
			if runeValue == '\u2028' || runeValue == '\u2029' {
				size += 6
			} else {
				size += int64(runeBytes)
			}
			index += runeBytes
		}
	}
	return size
}

func replayUnsignedSize(value uint64) int64 {
	var scratch [20]byte
	return int64(len(strconv.AppendUint(scratch[:0], value, 10)))
}

func replaySignedSize(value int) int64 {
	var scratch [20]byte
	return int64(len(strconv.AppendInt(scratch[:0], int64(value), 10)))
}

func ValidateReplayPageSequence(entries []ReplayEntry, after, through model.Revision, hasMore bool) error {
	if after == through {
		if len(entries) != 0 || hasMore {
			return fmt.Errorf("%w: page contains revisions after its boundary", ErrReplayGap)
		}
		return nil
	}
	expected := after + 1
	for _, entry := range entries {
		if entry.Accepted.Revision != expected {
			return fmt.Errorf("%w: got revision %d, expected %d", ErrReplayGap, entry.Accepted.Revision, expected)
		}
		expected++
	}
	if hasMore {
		if len(entries) == 0 {
			return fmt.Errorf("%w: page did not make progress", ErrReplayGap)
		}
		return nil
	}
	if expected-1 != through {
		return fmt.Errorf("%w: replay ended at revision %d before %d", ErrReplayGap, expected-1, through)
	}
	return nil
}

func validReplayHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func ValidReplayHash(value string) bool {
	return validReplayHash(value)
}

func (store *MemoryStore) LoadReplayPage(ctx context.Context, documentID model.DocumentID, after, through model.Revision, expectedSnapshot *model.Revision, limit int) (ReplayPage, error) {
	if err := ctx.Err(); err != nil {
		return ReplayPage{}, err
	}
	if err := ValidateReplayPageRequest(after, through, limit); err != nil {
		return ReplayPage{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return ReplayPage{}, ErrStoreClosed
	}
	snapshot, exists := store.sessions[documentID]
	if !exists {
		return ReplayPage{}, ErrSessionMissing
	}
	if err := CheckReplaySnapshot(expectedSnapshot, snapshot.Revision); err != nil {
		return ReplayPage{}, err
	}
	head, hasHead := store.revisionHeads[documentID]
	if !hasHead {
		return ReplayPage{}, ErrSessionMissing
	}
	if through > head || after > head {
		return ReplayPage{}, ErrReplayRevisionRange
	}
	page := ReplayPage{SnapshotRevision: snapshot.Revision, HeadRevision: head, ThroughRevision: through, Entries: make([]ReplayEntry, 0, min(limit, len(store.operations[documentID])))}
	if after < snapshot.Revision || after == through {
		return page, nil
	}
	bulk := store.transactionVersions[documentID] == BulkTransactionVersion
	var inlineBytes int64
	for _, accepted := range store.operations[documentID] {
		if err := ctx.Err(); err != nil {
			return ReplayPage{}, err
		}
		if accepted.Revision <= after || accepted.Revision > through {
			continue
		}
		if len(page.Entries) >= limit {
			page.HasMore = true
			break
		}
		mapHash, found := store.hashes[documentID][accepted.Revision]
		if !found || !validReplayHash(mapHash) || accepted.DocumentID != documentID || accepted.OperationID == "" {
			return ReplayPage{}, fmt.Errorf("stored replay row at revision %d differs from its revision hash ledger", accepted.Revision)
		}
		encodedBytes, err := replayAcceptedJSONSize(accepted)
		if err != nil {
			return ReplayPage{}, err
		}
		if inlineBytes+encodedBytes > MaxReplayPageInlineBytes {
			if len(page.Entries) == 0 && !bulk {
				return ReplayPage{}, fmt.Errorf("in-memory replay operation exceeds the bounded metadata page")
			}
			if len(page.Entries) != 0 {
				page.HasMore = true
				break
			}
		}
		entry, err := validateMemoryReplayEntry(documentID, accepted.OperationID, accepted.Revision, mapHash, encodedBytes, bulk, accepted)
		if err != nil {
			return ReplayPage{}, err
		}
		page.Entries = append(page.Entries, entry)
		inlineBytes += encodedBytes
	}
	if err := ValidateReplayPageSequence(page.Entries, after, through, page.HasMore); err != nil {
		return ReplayPage{}, err
	}
	return page, nil
}
