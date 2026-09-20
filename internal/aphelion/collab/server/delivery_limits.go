package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

var errDeliveryLimit = errors.New("operation exceeds collaboration delivery limit")

// Validate before durable append: fitting the submission envelope does not imply
// its acceptance fits. Reserve room for replay/duplicate headers, future revision
// digits and the actor-scoped inverse, whose changes have the same encoded size.
// Document owners can be shared by sessions, so reserve maximum identifier sizes
// including JSON escaping rather than tying durable history to today's session.
func validateOperationDelivery(accepted model.AcceptedOperation) error {
	probe := accepted
	probe.Revision = model.Revision(^uint64(0))
	probe.BaseRevision = model.Revision(^uint64(0))
	probe.AcceptedAt = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	probe.Kind = model.OperationKindInverse
	probe.InverseOf = &accepted.OperationID
	identifier := strings.Repeat("<", protocol.MaxIdentifierBytes)
	encoded, err := marshalServerEnvelope(protocol.ServerEnvelope{
		ProtocolVersion: model.ProtocolVersion,
		MessageID:       identifier,
		SessionID:       identifier,
		Type:            protocol.ServerOperationAccepted,
	}, protocol.OperationAcceptedPayload{Operation: probe, MapHash: strings.Repeat("0", 64)})
	if err != nil {
		return fmt.Errorf("encode acceptance before append: %w", err)
	}
	if len(encoded) > protocol.MaxMessageBytes {
		return fmt.Errorf("%w: acceptance and inverse need %d bytes including envelope headroom; maximum is %d", errDeliveryLimit, len(encoded), protocol.MaxMessageBytes)
	}
	return nil
}
