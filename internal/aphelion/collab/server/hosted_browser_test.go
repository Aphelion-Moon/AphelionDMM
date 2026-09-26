package server

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestHostedBrowserRoutesRequireAuthentication(t *testing.T) {
	service := NewService(ServiceConfig{HostedAuth: newFakeHostedBackend(nil)})
	defer service.Shutdown(context.Background())
	for _, path := range []string{"/v1/hosted/sessions?scope=community", "/v1/hosted/sessions?scope=mine"} {
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 401 {
			t.Fatalf("%s status %d", path, response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("list may be cached")
		}
	}
}
