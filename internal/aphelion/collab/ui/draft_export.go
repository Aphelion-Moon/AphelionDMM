package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

func (client *SessionClient) HasRetainedDrafts() bool {
	network := client.NetworkExecutor()
	return network != nil && len(network.Conflicts()) != 0
}

// ExportConflict saves a recovery reference, not a wire message or an operation
// ready to resubmit. It deliberately omits connection details and error text.
// Exporting never resolves a conflict or changes acknowledged map state.
func (client *SessionClient) ExportConflict(operationID model.OperationID, path string) error {
	network, _, err := client.conflictExecutor()
	if err != nil {
		return err
	}
	for _, conflict := range network.Conflicts() {
		if conflict.OperationID != operationID {
			continue
		}
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
