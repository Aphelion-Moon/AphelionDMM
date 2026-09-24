package protocol

import (
	"encoding/json"
	"fmt"

	"sdmm/internal/aphelion/collab/model"
)

const BulkSubprotocol = "apheliondmm.collaboration.bulk-edit-v2"

// DecodeClientEnvelope also accepts the typed result of the verified bulk
// decoder. The typed field is local-only and cannot be injected through JSON.
func DecodeClientEnvelope(e ClientEnvelope) (DecodedClient, error) {
	if e.BulkOperation == nil {
		data, err := json.Marshal(e)
		if err != nil {
			return DecodedClient{}, err
		}
		return DecodeClientForSession(data, e.BulkSession)
	}
	if len(e.Payload) != 0 || e.Type != ClientOperationSubmit {
		return DecodedClient{}, fmt.Errorf("invalid bulk submission envelope")
	}
	if err := validateEnvelope(e.ProtocolVersion, e.MessageID, e.SessionID); err != nil {
		return DecodedClient{}, err
	}
	if err := validateOperationLimit(*e.BulkOperation, model.MaxMapCells); err != nil {
		return DecodedClient{}, err
	}
	return DecodedClient{Envelope: e, Payload: &OperationSubmitPayload{Operation: *e.BulkOperation}}, nil
}

func DecodeServerEnvelope(e ServerEnvelope) (DecodedServer, error) {
	if e.BulkAccepted == nil {
		data, err := json.Marshal(e)
		if err != nil {
			return DecodedServer{}, err
		}
		return DecodeServerForSession(data, e.BulkSession)
	}
	if len(e.Payload) != 0 || e.Type != ServerOperationAccepted {
		return DecodedServer{}, fmt.Errorf("invalid bulk acceptance envelope")
	}
	if err := validateEnvelope(e.ProtocolVersion, e.MessageID, e.SessionID); err != nil {
		return DecodedServer{}, err
	}
	p := e.BulkAccepted
	if err := validateOperationLimit(p.Operation.Operation, model.MaxMapCells); err != nil {
		return DecodedServer{}, err
	}
	if p.Operation.Revision == 0 || p.Operation.AcceptedAt.IsZero() {
		return DecodedServer{}, fmt.Errorf("accepted operation requires revision and timestamp")
	}
	if err := validateHash("map hash", p.MapHash); err != nil {
		return DecodedServer{}, err
	}
	return DecodedServer{Envelope: e, Payload: p}, nil
}
