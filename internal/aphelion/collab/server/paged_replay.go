package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
)

var errReplayNeedsSnapshot = errors.New("replay requires an authoritative snapshot")

// Each metadata page closes its database rows before this function writes to a
// socket. Each V2 body is verified and reframed into an admitted private spool,
// then its database reader closes before any network backpressure is possible.
func (service *Service) writePagedReplay(ctx context.Context, connection *websocket.Conn, pages collabstore.ReplayPageStore, sessionID string, snapshot model.Snapshot, after model.Revision, mapHash, joinMessageID string) error {
	if after > snapshot.Revision {
		return fmt.Errorf("replay cursor is ahead of authoritative revision")
	}
	var retained *model.Revision
	cursor := after
	for {
		page, err := pages.LoadReplayPage(ctx, snapshot.DocumentID, cursor, snapshot.Revision, retained, 32)
		if errors.Is(err, collabstore.ErrReplayCheckpointChanged) || err == nil && cursor < page.SnapshotRevision {
			if err := writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "snapshot-required-" + joinMessageID, SessionID: sessionID, Type: protocol.ServerSessionNotice}, protocol.SessionNoticePayload{Code: protocol.NoticeSnapshotRequired, Message: "authoritative snapshot is required before replay"}); err != nil {
				return err
			}
			_ = connection.Close(websocket.StatusServiceRestart, "snapshot required")
			return errReplayNeedsSnapshot
		}
		if err != nil {
			return fmt.Errorf("load replay page: %w", err)
		}
		floor := page.SnapshotRevision
		retained = &floor
		if page.ThroughRevision != snapshot.Revision {
			return fmt.Errorf("replay page changed authoritative boundary")
		}
		for _, entry := range page.Entries {
			if entry.Accepted.DocumentID != snapshot.DocumentID || entry.Accepted.Revision != cursor+1 || entry.Accepted.Revision > snapshot.Revision {
				return fmt.Errorf("replay page has an invalid document or revision gap")
			}
			if err := service.writeReplayEntry(ctx, connection, sessionID, entry); err != nil {
				return err
			}
			cursor = entry.Accepted.Revision
		}
		if !page.HasMore {
			if cursor != snapshot.Revision {
				return fmt.Errorf("replay ended before authoritative boundary")
			}
			break
		}
		if len(page.Entries) == 0 {
			return fmt.Errorf("replay page made no progress")
		}
	}
	return writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-complete-" + joinMessageID, SessionID: sessionID, Type: protocol.ServerReplayComplete}, protocol.ReplayCompletePayload{Revision: snapshot.Revision, MapHash: mapHash})
}

func (service *Service) writeReplayEntry(ctx context.Context, connection *websocket.Conn, sessionID string, entry collabstore.ReplayEntry) error {
	envelope := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-" + string(entry.Accepted.OperationID), SessionID: sessionID, Type: protocol.ServerOperationAccepted}
	if entry.StorageVersion == collabstore.LegacyTransactionVersion {
		return writeServerEnvelope(ctx, connection, envelope, protocol.OperationAcceptedPayload{Operation: entry.Accepted, MapHash: entry.MapHash})
	}
	codec := bulkCodec(ctx)
	streamed, ok := service.store.(collabstore.StreamedTransactionStore)
	if !ok || codec == nil || entry.StorageVersion != collabstore.BulkTransactionVersion {
		return fmt.Errorf("stored bulk replay requires negotiated streaming capability")
	}
	source, found, err := streamed.OpenTransaction(ctx, entry.Accepted.DocumentID, entry.Accepted.OperationID)
	if err != nil {
		return fmt.Errorf("open replay body: %w", err)
	}
	if !found {
		return fmt.Errorf("replay body is missing")
	}
	header := collabstore.AcceptedTransactionHeader(entry.Accepted, entry.MapHash)
	header.Count = entry.ChangeCount
	if len(envelope.MessageID) > protocol.MaxIdentifierBytes {
		envelope.MessageID = fmt.Sprintf("response-%x", sha256.Sum256([]byte(envelope.MessageID)))
	}
	body, err := codec.PrepareReplay(ctx, source, header, entry.BodyBytes, sessionID, envelope.MessageID)
	closeErr := source.Close()
	if err != nil {
		return fmt.Errorf("prepare replay body: %w", err)
	}
	defer func() { _ = body.Close() }()
	if closeErr != nil {
		return fmt.Errorf("close replay source: %w", closeErr)
	}
	return codec.WriteBody(ctx, connection, body, webSocketIOTimeout)
}
