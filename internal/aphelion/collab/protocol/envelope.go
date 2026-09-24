package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

const (
	MaxIdentifierBytes        = 128
	MaxDisplayNameBytes       = 128
	MaxMessageBytes           = 1 << 20
	MaxOperationChanges       = 4096
	MaxPresenceParticipants   = 256
	MaxPresenceSelectionTiles = 4096
	MinPresenceIntervalMS     = 16
	MaxPresenceIntervalMS     = 5000
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type MessageType string

type ClientType = MessageType
type ServerType = MessageType

const (
	ClientJoin                 ClientType = "join"
	ClientOperationSubmit      ClientType = "operation_submit"
	ClientInverseRequest       ClientType = "inverse_request"
	ClientPresenceUpdate       ClientType = "presence_update"
	ClientProfileUpdate        ClientType = "profile_update"
	ClientAcknowledgedRevision ClientType = "acknowledged_revision"
	ClientPing                 ClientType = "ping"

	ServerJoined            ServerType = "joined"
	ServerOperationAccepted ServerType = "operation_accepted"
	ServerOperationRejected ServerType = "operation_rejected"
	ServerReplayComplete    ServerType = "replay_complete"
	ServerPresenceSnapshot  ServerType = "presence_snapshot"
	ServerPresenceUpdate    ServerType = "presence_update"
	ServerSessionNotice     ServerType = "session_notice"
	ServerPong              ServerType = "pong"

	NoticeSnapshotRequired = "snapshot_required"
)

type ClientEnvelope struct {
	BulkSession     bool             `json:"-"`
	BulkOperation   *model.Operation `json:"-"`
	ProtocolVersion uint16           `json:"protocol_version"`
	MessageID       string           `json:"message_id"`
	SessionID       string           `json:"session_id"`
	Type            ClientType       `json:"type"`
	Payload         json.RawMessage  `json:"payload"`
}

type ServerEnvelope struct {
	BulkSession     bool                      `json:"-"`
	BulkAccepted    *OperationAcceptedPayload `json:"-"`
	ProtocolVersion uint16                    `json:"protocol_version"`
	MessageID       string                    `json:"message_id"`
	SessionID       string                    `json:"session_id"`
	Type            ServerType                `json:"type"`
	Payload         json.RawMessage           `json:"payload"`
}

type DecodedClient struct {
	Envelope ClientEnvelope
	Payload  any
}

type DecodedServer struct {
	Envelope ServerEnvelope
	Payload  any
}

type JoinPayload struct {
	JoinToken            string         `json:"join_token"`
	AcknowledgedRevision model.Revision `json:"acknowledged_revision"`
}

type OperationSubmitPayload struct {
	Operation model.Operation `json:"operation"`
}

type InverseRequestPayload struct {
	OperationID model.OperationID `json:"operation_id"`
}

type PresenceUpdatePayload struct {
	Sequence  uint64             `json:"sequence"`
	Cursor    *model.Coord       `json:"cursor,omitempty"`
	Selection *PresenceSelection `json:"selection,omitempty"`
	Status    string             `json:"status"`
}

type ProfileUpdatePayload struct {
	DisplayName string `json:"display_name"`
}

type PresenceSelection struct {
	Min model.Coord `json:"min"`
	Max model.Coord `json:"max"`
}

type AcknowledgedRevisionPayload struct {
	Revision model.Revision `json:"revision"`
}

type PingPayload struct {
	Nonce string `json:"nonce"`
}

type JoinedPayload struct {
	BulkEdits                bool             `json:"bulk_edits,omitempty"`
	DocumentID               model.DocumentID `json:"document_id"`
	ActorID                  model.ActorID    `json:"actor_id"`
	Role                     string           `json:"role"`
	Revision                 model.Revision   `json:"revision"`
	MapHash                  string           `json:"map_hash"`
	PresenceIntervalMS       uint32           `json:"presence_interval_ms"`
	ResumptionToken          string           `json:"resumption_token"`
	ResumptionTokenExpiresAt time.Time        `json:"resumption_token_expires_at"`
}

type OperationAcceptedPayload struct {
	Operation model.AcceptedOperation `json:"operation"`
	MapHash   string                  `json:"map_hash"`
}

type OperationRejectedPayload struct {
	OperationID         model.OperationID `json:"operation_id"`
	Code                string            `json:"code"`
	Message             string            `json:"message"`
	Revision            model.Revision    `json:"revision"`
	MapHash             string            `json:"map_hash"`
	AuthoritativeValues []model.Tile      `json:"authoritative_values,omitempty"`
}

type ReplayCompletePayload struct {
	Revision model.Revision `json:"revision"`
	MapHash  string         `json:"map_hash"`
}

type ParticipantPresence struct {
	ActorID     model.ActorID      `json:"actor_id"`
	DisplayName string             `json:"display_name"`
	Sequence    uint64             `json:"sequence"`
	Cursor      *model.Coord       `json:"cursor,omitempty"`
	Selection   *PresenceSelection `json:"selection,omitempty"`
	Status      string             `json:"status"`
}

type PresenceSnapshotPayload struct {
	Participants []ParticipantPresence `json:"participants"`
}

type ServerPresenceUpdatePayload = ParticipantPresence

type SessionNoticePayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PongPayload = PingPayload

func AllClientTypes() []MessageType {
	return []MessageType{ClientJoin, ClientOperationSubmit, ClientInverseRequest, ClientPresenceUpdate, ClientProfileUpdate, ClientAcknowledgedRevision, ClientPing}
}

func AllServerTypes() []MessageType {
	return []MessageType{ServerJoined, ServerOperationAccepted, ServerOperationRejected, ServerReplayComplete, ServerPresenceSnapshot, ServerPresenceUpdate, ServerSessionNotice, ServerPong}
}

func DecodeClient(data []byte) (DecodedClient, error) {
	return DecodeClientForSession(data, false)
}

func DecodeClientForSession(data []byte, bulk bool) (DecodedClient, error) {
	if len(data) > MaxMessageBytes {
		return DecodedClient{}, fmt.Errorf("client message is %d bytes, maximum is %d", len(data), MaxMessageBytes)
	}
	var envelope ClientEnvelope
	if err := decodeStrict(data, &envelope); err != nil {
		return DecodedClient{}, fmt.Errorf("decode client envelope: %w", err)
	}
	if err := validateEnvelope(envelope.ProtocolVersion, envelope.MessageID, envelope.SessionID); err != nil {
		return DecodedClient{}, err
	}

	var payload any
	switch envelope.Type {
	case ClientJoin:
		payload = &JoinPayload{}
	case ClientOperationSubmit:
		payload = &OperationSubmitPayload{}
	case ClientInverseRequest:
		payload = &InverseRequestPayload{}
	case ClientPresenceUpdate:
		payload = &PresenceUpdatePayload{}
	case ClientProfileUpdate:
		payload = &ProfileUpdatePayload{}
	case ClientAcknowledgedRevision:
		payload = &AcknowledgedRevisionPayload{}
	case ClientPing:
		payload = &PingPayload{}
	default:
		return DecodedClient{}, fmt.Errorf("unsupported client message type %q", envelope.Type)
	}
	if err := decodeStrict(envelope.Payload, payload); err != nil {
		return DecodedClient{}, fmt.Errorf("decode %s payload: %w", envelope.Type, err)
	}
	if err := validateClientPayload(payload, bulk); err != nil {
		return DecodedClient{}, fmt.Errorf("validate %s payload: %w", envelope.Type, err)
	}
	envelope.BulkSession = bulk
	return DecodedClient{Envelope: envelope, Payload: payload}, nil
}

func DecodeServer(data []byte) (DecodedServer, error) {
	return DecodeServerForSession(data, false)
}

func DecodeServerForSession(data []byte, bulk bool) (DecodedServer, error) {
	if len(data) > MaxMessageBytes {
		return DecodedServer{}, fmt.Errorf("server message is %d bytes, maximum is %d", len(data), MaxMessageBytes)
	}
	var envelope ServerEnvelope
	if err := decodeStrict(data, &envelope); err != nil {
		return DecodedServer{}, fmt.Errorf("decode server envelope: %w", err)
	}
	if err := validateEnvelope(envelope.ProtocolVersion, envelope.MessageID, envelope.SessionID); err != nil {
		return DecodedServer{}, err
	}

	var payload any
	switch envelope.Type {
	case ServerJoined:
		payload = &JoinedPayload{}
	case ServerOperationAccepted:
		payload = &OperationAcceptedPayload{}
	case ServerOperationRejected:
		payload = &OperationRejectedPayload{}
	case ServerReplayComplete:
		payload = &ReplayCompletePayload{}
	case ServerPresenceSnapshot:
		payload = &PresenceSnapshotPayload{}
	case ServerPresenceUpdate:
		payload = &ServerPresenceUpdatePayload{}
	case ServerSessionNotice:
		payload = &SessionNoticePayload{}
	case ServerPong:
		payload = &PongPayload{}
	default:
		return DecodedServer{}, fmt.Errorf("unsupported server message type %q", envelope.Type)
	}
	if err := decodeStrict(envelope.Payload, payload); err != nil {
		return DecodedServer{}, fmt.Errorf("decode %s payload: %w", envelope.Type, err)
	}
	if err := validateServerPayload(payload, bulk); err != nil {
		return DecodedServer{}, fmt.Errorf("validate %s payload: %w", envelope.Type, err)
	}
	envelope.BulkSession = bulk
	return DecodedServer{Envelope: envelope, Payload: payload}, nil
}

func validateEnvelope(version uint16, messageID, sessionID string) error {
	if version != model.ProtocolVersion {
		return fmt.Errorf("protocol version is %d, want %d", version, model.ProtocolVersion)
	}
	if err := validateIdentifier("message id", messageID); err != nil {
		return err
	}
	return validateIdentifier("session id", sessionID)
}

func validateClientPayload(payload any, bulk bool) error {
	switch value := payload.(type) {
	case *JoinPayload:
		return validateIdentifier("join token", value.JoinToken)
	case *OperationSubmitPayload:
		return validateOperation(value.Operation)
	case *InverseRequestPayload:
		return value.OperationID.Validate()
	case *PresenceUpdatePayload:
		if value.Sequence == 0 {
			return fmt.Errorf("sequence must be positive")
		}
		if value.Cursor != nil && (value.Cursor.X < 1 || value.Cursor.Y < 1 || value.Cursor.Z < 1) {
			return fmt.Errorf("cursor coordinates must be positive")
		}
		if err := validatePresenceSelection(value.Selection, bulk); err != nil {
			return err
		}
		return validateIdentifier("status", value.Status)
	case *ProfileUpdatePayload:
		if strings.TrimSpace(value.DisplayName) == "" || len(value.DisplayName) > MaxDisplayNameBytes {
			return fmt.Errorf("display name length is %d, want 1..%d non-whitespace bytes", len(value.DisplayName), MaxDisplayNameBytes)
		}
		return nil
	case *AcknowledgedRevisionPayload:
		return nil
	case *PingPayload:
		return validateIdentifier("nonce", value.Nonce)
	default:
		return fmt.Errorf("unsupported client payload %T", payload)
	}
}

func validateServerPayload(payload any, bulk bool) error {
	switch value := payload.(type) {
	case *JoinedPayload:
		if err := value.DocumentID.Validate(); err != nil {
			return err
		}
		if err := value.ActorID.Validate(); err != nil {
			return err
		}
		if value.Role != "viewer" && value.Role != "editor" && value.Role != "owner" {
			return fmt.Errorf("unsupported role %q", value.Role)
		}
		if value.PresenceIntervalMS < MinPresenceIntervalMS || value.PresenceIntervalMS > MaxPresenceIntervalMS {
			return fmt.Errorf("presence interval is %dms, want %d..%dms", value.PresenceIntervalMS, MinPresenceIntervalMS, MaxPresenceIntervalMS)
		}
		if err := validateIdentifier("resumption token", value.ResumptionToken); err != nil {
			return err
		}
		if value.ResumptionTokenExpiresAt.IsZero() {
			return fmt.Errorf("resumption token expiry is required")
		}
		return validateHash("map hash", value.MapHash)
	case *OperationAcceptedPayload:
		if err := validateOperation(value.Operation.Operation); err != nil {
			return err
		}
		if value.Operation.Revision == 0 || value.Operation.AcceptedAt.IsZero() {
			return fmt.Errorf("accepted operation requires revision and timestamp")
		}
		return validateHash("map hash", value.MapHash)
	case *OperationRejectedPayload:
		if err := value.OperationID.Validate(); err != nil {
			return err
		}
		if err := validateIdentifier("error code", value.Code); err != nil {
			return err
		}
		if err := validateIdentifier("error message", value.Message); err != nil {
			return err
		}
		if len(value.AuthoritativeValues) > MaxOperationChanges {
			return fmt.Errorf("rejection has %d authoritative values, maximum is %d", len(value.AuthoritativeValues), MaxOperationChanges)
		}
		coordinates := make(map[model.Coord]struct{}, len(value.AuthoritativeValues))
		stableIDs := make(map[model.StableID]struct{})
		for index, tile := range value.AuthoritativeValues {
			if tile.Coord.X < 1 || tile.Coord.Y < 1 || tile.Coord.Z < 1 {
				return fmt.Errorf("authoritative value %d coordinates must be positive", index)
			}
			if _, exists := coordinates[tile.Coord]; exists {
				return fmt.Errorf("duplicate authoritative coordinate (%d,%d,%d)", tile.Coord.X, tile.Coord.Y, tile.Coord.Z)
			}
			coordinates[tile.Coord] = struct{}{}
			for _, prefab := range tile.State.Prefabs {
				if err := prefab.StableID.Validate(); err != nil {
					return err
				}
				if _, exists := stableIDs[prefab.StableID]; exists {
					return fmt.Errorf("duplicate authoritative stable id %q", prefab.StableID)
				}
				stableIDs[prefab.StableID] = struct{}{}
			}
		}
		return validateHash("map hash", value.MapHash)
	case *ReplayCompletePayload:
		return validateHash("map hash", value.MapHash)
	case *PresenceSnapshotPayload:
		if len(value.Participants) > MaxPresenceParticipants {
			return fmt.Errorf("presence snapshot has %d participants, maximum is %d", len(value.Participants), MaxPresenceParticipants)
		}
		for index := range value.Participants {
			if err := validatePresence(&value.Participants[index], bulk); err != nil {
				return fmt.Errorf("participant %d: %w", index, err)
			}
		}
		return nil
	case *ServerPresenceUpdatePayload:
		return validatePresence(value, bulk)
	case *SessionNoticePayload:
		if err := validateIdentifier("notice code", value.Code); err != nil {
			return err
		}
		return validateIdentifier("notice message", value.Message)
	case *PongPayload:
		return validateIdentifier("nonce", value.Nonce)
	default:
		return fmt.Errorf("unsupported server payload %T", payload)
	}
}

func validateOperation(operation model.Operation) error {
	return validateOperationLimit(operation, MaxOperationChanges)
}

func validateOperationLimit(operation model.Operation, maximum int) error {
	if operation.ProtocolVersion != model.ProtocolVersion {
		return fmt.Errorf("operation protocol version is %d, want %d", operation.ProtocolVersion, model.ProtocolVersion)
	}
	if err := operation.DocumentID.Validate(); err != nil {
		return err
	}
	if err := operation.ActorID.Validate(); err != nil {
		return err
	}
	if err := operation.OperationID.Validate(); err != nil {
		return err
	}
	if err := validateHash("environment hash", operation.EnvironmentHash); err != nil {
		return err
	}
	if err := validateHash("base map hash", operation.BaseMapHash); err != nil {
		return err
	}
	if operation.Kind != model.OperationKindTileChange && operation.Kind != model.OperationKindInverse {
		return fmt.Errorf("unsupported operation kind %q", operation.Kind)
	}
	if len(operation.Changes) == 0 || len(operation.Changes) > maximum {
		return fmt.Errorf("operation change count is %d, want 1..%d", len(operation.Changes), maximum)
	}
	for index, change := range operation.Changes {
		if change.Coord.X < 1 || change.Coord.Y < 1 || change.Coord.Z < 1 {
			return fmt.Errorf("change %d coordinates must be positive", index)
		}
	}
	if operation.Kind == model.OperationKindInverse && operation.InverseOf == nil {
		return fmt.Errorf("inverse operation must name its target")
	}
	if operation.InverseOf != nil {
		if err := operation.InverseOf.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validatePresence(value *ParticipantPresence, bulk bool) error {
	if err := value.ActorID.Validate(); err != nil {
		return err
	}
	if value.Sequence == 0 {
		return fmt.Errorf("sequence must be positive")
	}
	if len(value.DisplayName) == 0 || len(value.DisplayName) > MaxDisplayNameBytes {
		return fmt.Errorf("display name length is %d, want 1..%d", len(value.DisplayName), MaxDisplayNameBytes)
	}
	if value.Cursor != nil && (value.Cursor.X < 1 || value.Cursor.Y < 1 || value.Cursor.Z < 1) {
		return fmt.Errorf("cursor coordinates must be positive")
	}
	if err := validatePresenceSelection(value.Selection, bulk); err != nil {
		return err
	}
	return validateIdentifier("status", value.Status)
}

func validatePresenceSelection(selection *PresenceSelection, bulk bool) error {
	if selection == nil {
		return nil
	}
	if selection.Min.X < 1 || selection.Min.Y < 1 || selection.Min.Z < 1 || selection.Max.X < 1 || selection.Max.Y < 1 || selection.Max.Z < 1 {
		return fmt.Errorf("selection coordinates must be positive")
	}
	if selection.Min.Z != selection.Max.Z || selection.Min.X > selection.Max.X || selection.Min.Y > selection.Max.Y {
		return fmt.Errorf("selection bounds must be normalized on one level")
	}
	if selection.Max.X > model.MaxMapDimension || selection.Max.Y > model.MaxMapDimension || selection.Max.Z > model.MaxMapDimension {
		return fmt.Errorf("selection coordinates exceed supported map dimensions")
	}
	tileCount := int64(selection.Max.X-selection.Min.X+1) * int64(selection.Max.Y-selection.Min.Y+1)
	if !bulk && tileCount > MaxPresenceSelectionTiles {
		return fmt.Errorf("selection has %d tiles, maximum is %d", tileCount, MaxPresenceSelectionTiles)
	}
	return nil
}

func validateIdentifier(name, value string) error {
	if value == "" || len(value) > MaxIdentifierBytes {
		return fmt.Errorf("%s length is %d, want 1..%d", name, len(value), MaxIdentifierBytes)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s has surrounding whitespace", name)
	}
	return nil
}

func validateHash(name, value string) error {
	if !sha256Pattern.MatchString(value) {
		return fmt.Errorf("%s must be lowercase SHA-256 hex", name)
	}
	return nil
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
