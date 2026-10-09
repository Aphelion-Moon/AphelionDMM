package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

func (client *SessionClient) HasRetainedDrafts() bool {
	network := client.NetworkExecutor()
	return network != nil && network.ConflictCount() != 0
}

// ExportConflict saves a recovery reference, not a wire message or an operation
// ready to resubmit. It deliberately omits connection details and error text.
// Exporting never resolves a conflict or changes acknowledged map state.
func (client *SessionClient) ExportConflict(operationID model.OperationID, path string) error {
	network, _, err := client.conflictExecutor()
	if err != nil {
		return err
	}
	if conflict, exists := network.Conflict(operationID); exists {
		export := struct {
			FormatVersion    int             `json:"format_version"`
			Operation        model.Operation `json:"operation"`
			RecordedRevision model.Revision  `json:"recorded_revision"`
			RecordedMapHash  string          `json:"recorded_map_hash"`
		}{1, conflict.Draft, conflict.Revision, conflict.MapHash}
		encoded, err := json.MarshalIndent(export, "", "  ")
		if err != nil {
			return fmt.Errorf("encode collaboration draft: %w", err)
		}
		encoded = append(encoded, '\n')
		return dmmdata.SaveAtomic(path, func(writer io.Writer) error {
			_, err := writer.Write(encoded)
			return err
		}, func(staged string) error {
			actual, err := os.ReadFile(staged)
			if err != nil {
				return err
			}
			if !bytes.Equal(actual, encoded) {
				return fmt.Errorf("staged collaboration draft differs from retained intent")
			}
			return nil
		})
	}
	return fmt.Errorf("collaboration draft %s is unavailable", operationID)
}

// RetainedDraftIDs lists every retained draft, not just the visible page, in
// the order the executor recorded them. Bulk actions iterate this snapshot.
func (client *SessionClient) RetainedDraftIDs() []model.OperationID {
	network := client.NetworkExecutor()
	if network == nil {
		return nil
	}
	conflicts := network.Conflicts()
	ids := make([]model.OperationID, len(conflicts))
	for index, conflict := range conflicts {
		ids[index] = conflict.OperationID
	}
	return ids
}

// DraftPromptView is a per-frame check for the "Unsent collaboration changes"
// prompt. It builds the view model without conflict previews, so it is cheap.
func (client *SessionClient) DraftPromptView() ViewModel {
	client.mutex.Lock()
	machine := client.machine
	status := SessionStatus{SessionID: client.sessionID, Role: client.role, Err: client.lastErr}
	if client.resumptionToken != "" {
		status.ReconnectReady = !client.reconnecting && client.config.Now().Before(client.resumptionExpiresAt)
	}
	network := client.network
	client.mutex.Unlock()
	status.State = collabclient.StateDisconnected
	if machine != nil {
		status.State = machine.State()
	}
	if network != nil {
		status.ConflictCount = network.ConflictCount()
	}
	return BuildViewModel(status)
}
