package bulktransport

import (
	"fmt"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/transaction"
)

func SubmissionHeader(e protocol.ClientEnvelope) transaction.Header {
	op := *e.BulkOperation
	count := len(op.Changes)
	op.Changes = nil
	return transaction.Header{Version: transaction.Version, Kind: string(e.Type), SessionID: e.SessionID, MessageID: e.MessageID, Count: int64(count), Operation: op}
}
func AcceptanceHeader(e protocol.ServerEnvelope, p protocol.OperationAcceptedPayload) transaction.Header {
	op := p.Operation.Operation
	count := len(op.Changes)
	op.Changes = nil
	return transaction.Header{Version: transaction.Version, Kind: string(e.Type), SessionID: e.SessionID, MessageID: e.MessageID, Count: int64(count), Operation: op, Revision: p.Operation.Revision, AcceptedAt: p.Operation.AcceptedAt, MapHash: p.MapHash}
}
func Submission(h transaction.Header, op model.Operation) (protocol.DecodedClient, error) {
	if h.Revision != 0 || !h.AcceptedAt.IsZero() || h.MapHash != "" {
		return protocol.DecodedClient{}, fmt.Errorf("submission contains acceptance metadata")
	}
	return protocol.DecodeClientEnvelope(protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, Type: protocol.ClientType(h.Kind), SessionID: h.SessionID, MessageID: h.MessageID, BulkOperation: &op})
}
func Acceptance(h transaction.Header, op model.Operation) (protocol.DecodedServer, error) {
	return protocol.DecodeServerEnvelope(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, Type: protocol.ServerType(h.Kind), SessionID: h.SessionID, MessageID: h.MessageID, BulkAccepted: &protocol.OperationAcceptedPayload{Operation: model.AcceptedOperation{Operation: op, Revision: h.Revision, AcceptedAt: h.AcceptedAt}, MapHash: h.MapHash}})
}
