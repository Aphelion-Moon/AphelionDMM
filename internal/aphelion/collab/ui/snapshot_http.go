package ui

import (
	"bytes"
	"compress/gzip"
	"net/http"
)

func compressSnapshotRequest(body []byte) ([]byte, error) {
	var encoded bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&encoded, gzip.BestSpeed)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func (client *SessionClient) doSnapshotRequest(request *http.Request) (*http.Response, error) {
	// Copy the client, preserving its transport and redirect policy. Never mutate
	// the timeout shared by concurrent sign-in and session browser requests.
	transfer := *client.http
	transfer.Timeout = client.config.SnapshotTimeout
	return transfer.Do(request)
}
