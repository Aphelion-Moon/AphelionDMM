package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// A hosted service on another collaboration protocol is named before any
// session is created or joined; a service whose version is unreadable is not
// refused, since joining still reports incompatibility.
func TestHostedCapabilitiesNamesServiceProtocolMismatch(t *testing.T) {
	for name, test := range map[string]struct {
		version string
		status  int
		refused bool
	}{
		"older service":   {version: `{"protocol_versions":[1]}`, status: 200, refused: true},
		"current service": {version: `{"protocol_versions":[1,2]}`, status: 200},
		"no version":      {version: `{}`, status: 200},
		"version failure": {version: `oops`, status: 500},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/hosted/capabilities":
					_ = json.NewEncoder(w).Encode(protocol.HostedCapabilities{Provider: "oidc", SessionBrowser: true})
				case "/v1/version":
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.version))
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			}))
			defer server.Close()
			client := NewSessionClient(SessionClientConfig{})
			client.hostedBaseURL, client.hostedCredential, client.hostedCredentialExpires = server.URL, "secret", time.Now().Add(time.Hour)
			_, err := client.HostedCapabilities(context.Background())
			if test.refused {
				if !errors.Is(err, ErrServiceProtocolMismatch) || !strings.Contains(err.Error(), "protocol v1") || !strings.Contains(err.Error(), "requires v2") {
					t.Fatalf("mismatch error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("compatible service refused: %v", err)
			}
		})
	}
	if model.ProtocolVersion != 2 {
		t.Fatal("test expectations name protocol v2")
	}
}
