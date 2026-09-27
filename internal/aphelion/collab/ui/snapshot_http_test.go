package ui

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestHostedSnapshotTransferUsesNegotiatedCompressionAndSeparateDeadline(t *testing.T) {
	for _, compression := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "gzip"}[compression], func(t *testing.T) {
			snapshot := controllerSnapshot(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/hosted/capabilities":
					if compression {
						w.Header().Set("Accept-Encoding", "gzip")
					}
					_ = json.NewEncoder(w).Encode(protocol.HostedCapabilities{Provider: "discord", SessionBrowser: true})
				case "/v1/hosted/sessions":
					if (r.Header.Get("Content-Encoding") == "gzip") != compression {
						t.Error("upload compression was not negotiated")
					}
					if compression {
						decoded, err := gzip.NewReader(r.Body)
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						defer func() { _ = decoded.Close() }()
						r.Body = decoded
					}
					var body struct{ Snapshot model.Snapshot }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if got, _ := body.Snapshot.Hash(); got != mustSnapshotHash(t, snapshot) {
						t.Error("upload changed map")
					}
					time.Sleep(100 * time.Millisecond)
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "session", "document_id": snapshot.DocumentID})
				default:
					time.Sleep(100 * time.Millisecond)
					_ = json.NewEncoder(w).Encode(snapshot)
				}
			}))
			defer server.Close()
			client := NewSessionClient(SessionClientConfig{HTTPTimeout: 30 * time.Millisecond, SnapshotTimeout: 2 * time.Second})
			client.hostedBaseURL, client.hostedCredential = server.URL, "credential"
			client.hostedCredentialExpires = time.Now().Add(time.Hour)
			if _, err := client.CreateHostedWithMetadata(context.Background(), client.HostedAccount(), snapshot, protocol.HostedSessionMetadata{}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.fetchConnectionSnapshot(context.Background(), connectionParameters{BaseURL: server.URL, SessionID: "session", Token: "credential"}); err != nil {
				t.Fatal(err)
			}
			request, _ := http.NewRequest("GET", server.URL+"/control", nil)
			if response, err := client.http.Do(request); err == nil {
				_ = response.Body.Close()
				t.Fatal("control timeout was extended")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := client.fetchConnectionSnapshot(ctx, connectionParameters{BaseURL: server.URL, SessionID: "session", Token: "credential"}); err == nil {
				t.Fatal("canceled transfer succeeded")
			}
		})
	}
}
