package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/protocol"
)

// The snapshot endpoint serves the owner's cached hash as its ETag and a body
// that hashes to it, without rehashing or copying the map on the owner loop.
func TestSnapshotEndpointServesAuthoritativeHashWithoutRehash(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(t, 3)
	service := NewService(ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	launchToken, err := service.NewLaunchToken()
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(service.Handler())
	defer testServer.Close()
	created := createTestSession(t, testServer.URL, launchToken, snapshot)
	connection := connectTestClient(t, testServer.URL, created.SessionID, created.OwnerToken, 0)
	defer func() { _ = connection.CloseNow() }()

	submitTestOperation(t, connection, created.SessionID, "operation-1", testOperation(t, snapshot, 1))
	accepted := readServerEnvelopeType(t, connection, protocol.ServerOperationAccepted).Payload.(*protocol.OperationAcceptedPayload)

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/sessions/"+created.SessionID+"/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+created.OwnerToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200", response.StatusCode)
	}
	etag := strings.Trim(response.Header.Get("ETag"), `"`)
	if etag != accepted.MapHash {
		t.Fatalf("snapshot ETag = %q, want accepted hash %q", etag, accepted.MapHash)
	}
	var fetched struct {
		Revision uint64 `json:"revision"`
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.Revision != uint64(accepted.Operation.Revision) {
		t.Fatalf("snapshot revision = %d, want %d", fetched.Revision, accepted.Operation.Revision)
	}
	materialized, err := service.sessions[created.SessionID].owner.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := materialized.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash != etag {
		t.Fatalf("materialized snapshot hash = %q, want ETag %q", hash, etag)
	}
}
