package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
)

func TestHostedCompressedSnapshotLimitsAndIntegrity(t *testing.T) {
	for _, mode := range []string{"valid", "expanded-limit", "bad-checksum", "trailing-json"} {
		t.Run(mode, func(t *testing.T) {
			actor, _ := model.NewActorID()
			backend := newFakeHostedBackend(map[string]auth.Session{"owner": {Token: "owner", ActorID: actor, Issuer: "issuer", Subject: "owner", DisplayName: "Owner", ExpiresAt: time.Now().Add(time.Hour)}})
			service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend})
			defer func() { _ = service.Shutdown(context.Background()) }()
			body, _ := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1)})
			if mode == "expanded-limit" {
				service.limits.MaxSnapshotBodyBytes = int64(len(body) - 1)
			}
			if mode == "trailing-json" {
				body = append(body, []byte("{}")...)
			}
			var encoded bytes.Buffer
			compress := gzip.NewWriter(&encoded)
			_, _ = compress.Write(body)
			_ = compress.Close()
			if mode == "bad-checksum" {
				encoded.Bytes()[encoded.Len()-8] ^= 1
			}
			request := httptest.NewRequest("POST", "/v1/hosted/sessions", &encoded)
			request.Header.Set("Authorization", "Bearer owner")
			request.Header.Set("Content-Encoding", "gzip")
			response := httptest.NewRecorder()
			service.Handler().ServeHTTP(response, request)
			want := 201
			if mode == "expanded-limit" {
				want = 413
			} else if mode != "valid" {
				want = 400
			}
			if response.Code != want {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			sessions, _ := backend.ListHostedSessions(context.Background())
			if mode != "valid" && len(sessions) != 0 {
				t.Fatal("invalid upload registered a session")
			}
		})
	}
}

func TestSnapshotResponseNegotiatesCompression(t *testing.T) {
	snapshot := testSnapshot(t, 1)
	want, _ := snapshot.Hash()
	for _, accept := range []string{"", "gzip", "gzip;q=0", "br, gzip;q=0.5"} {
		request := httptest.NewRequest("GET", "/snapshot", nil)
		request.Header.Set("Accept-Encoding", accept)
		response := httptest.NewRecorder()
		writeSnapshotJSON(response, request, snapshot)
		var reader io.Reader = response.Body
		compressed := accept == "gzip" || strings.Contains(accept, "0.5")
		if got := response.Header().Get("Content-Encoding") == "gzip"; got != compressed {
			t.Fatalf("%q compression = %v", accept, got)
		}
		if compressed {
			decoded, err := gzip.NewReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = decoded.Close() }()
			reader = decoded
		}
		var actual model.Snapshot
		if err := json.NewDecoder(reader).Decode(&actual); err != nil {
			t.Fatal(err)
		}
		if got, _ := actual.Hash(); got != want {
			t.Fatal("compression changed snapshot")
		}
	}
}

type slowSnapshotBody struct {
	io.Reader
	started bool
}

func (body *slowSnapshotBody) Read(p []byte) (int, error) {
	if !body.started {
		body.started = true
		time.Sleep(100 * time.Millisecond)
	}
	return body.Reader.Read(p)
}

func TestHostedSnapshotUploadOutlivesControlDeadline(t *testing.T) {
	actor, _ := model.NewActorID()
	backend := newFakeHostedBackend(map[string]auth.Session{"owner": {Token: "owner", ActorID: actor, Issuer: "issuer", Subject: "owner", DisplayName: "Owner", ExpiresAt: time.Now().Add(time.Hour)}})
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend})
	defer func() { _ = service.Shutdown(context.Background()) }()
	server := httptest.NewUnstartedServer(service.Handler())
	server.Config.ReadTimeout = 30 * time.Millisecond
	server.Config.WriteTimeout = 30 * time.Millisecond
	server.Start()
	defer server.Close()
	body, _ := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1)})
	request, _ := http.NewRequest("POST", server.URL+"/v1/hosted/sessions", &slowSnapshotBody{Reader: bytes.NewReader(body)})
	request.Header.Set("Authorization", "Bearer owner")
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 201 {
		t.Fatalf("HTTP %d", response.StatusCode)
	}
}
