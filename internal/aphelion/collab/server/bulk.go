package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"sdmm/internal/aphelion/collab/bulktransport"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func writeTransactionUpgradeError(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, collabstore.ErrTransactionUpgradeRequired) {
		return false
	}
	writeError(w, http.StatusConflict, "transaction_upgrade_required", "Server storage needs an upgrade for large edits. Ask the server operator to upgrade transaction storage.")
	return true
}

// Storage capability is authoritative across process and hosted-session
// recovery. A default legacy configuration must never downgrade a V2 document.
func configureDocumentTransactions(ctx context.Context, store SessionStore, id model.DocumentID, config DocumentConfig) (DocumentConfig, error) {
	versions, ok := store.(collabstore.TransactionVersionStore)
	if !ok {
		if config.BulkEdits {
			return config, fmt.Errorf("bulk edits require versioned transaction storage")
		}
		return config, nil
	}
	version, err := versions.TransactionVersion(ctx, id)
	if err != nil {
		return config, err
	}
	if config.BulkEdits && version == collabstore.LegacyTransactionVersion {
		if err := versions.ConfigureTransactions(ctx, id, collabstore.BulkTransactionVersion); err != nil {
			return config, err
		}
		version = collabstore.BulkTransactionVersion
	}
	config.BulkEdits = version == collabstore.BulkTransactionVersion
	return config, nil
}

type bulkContextKey struct{}
type bulkConnection struct {
	codec     *bulktransport.Codec
	canUpload bool
}

func bulkCodec(ctx context.Context) *bulktransport.Codec {
	value, _ := ctx.Value(bulkContextKey{}).(bulkConnection)
	return value.codec
}

func bulkUploadAllowed(ctx context.Context) bool {
	value, _ := ctx.Value(bulkContextKey{}).(bulkConnection)
	return value.canUpload
}

func validateDocumentDelivery(accepted model.AcceptedOperation, bulk bool) error {
	if bulk {
		return nil
	}
	if len(accepted.Changes) > protocol.MaxOperationChanges {
		return &engine.Rejection{Code: engine.CodeInvalidOperation, CurrentRevision: accepted.Revision - 1, Cause: fmt.Errorf("legacy session requires bulk-edit-v2 for this operation")}
	}
	return validateOperationDelivery(accepted)
}
