package server

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Snapshot bodies can contain a whole station. Keep ordinary control requests
// on their shorter deadlines while allowing a bounded map transfer.
const snapshotTransferTimeout = 3 * time.Minute

func snapshotTransferDeadlines(w http.ResponseWriter) {
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(snapshotTransferTimeout)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
}

func acceptsGzip(value string) bool {
	for _, coding := range strings.Split(value, ",") {
		parts := strings.Split(coding, ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		for _, parameter := range parts[1:] {
			name, value, _ := strings.Cut(parameter, "=")
			if strings.EqualFold(strings.TrimSpace(name), "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || !(quality > 0 && quality <= 1) {
					return false
				}
			}
		}
		return true
	}
	return false
}

func writeSnapshotJSON(w http.ResponseWriter, r *http.Request, value any) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("Cache-Control", "no-store")
	if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		writeJSON(w, http.StatusOK, value)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	compressed, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	defer func() { _ = compressed.Close() }()
	_ = json.NewEncoder(compressed).Encode(value)
}
