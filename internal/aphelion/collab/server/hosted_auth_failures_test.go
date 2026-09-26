package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/auth"
)

func TestHostedAuthFailuresReturnSanitizedDesktopCategories(t *testing.T) {
	tests := []struct {
		name  string
		cause error
		want  string
	}{
		{name: "not a guild member", cause: auth.ErrDiscordNotMember, want: "not_a_member"},
		{name: "missing Discord scopes", cause: auth.ErrDiscordMissingScopes, want: "missing_permissions"},
		{name: "provider temporary failure", cause: auth.ErrDiscordTemporaryFailure, want: "provider_unavailable"},
		{name: "invalid provider state", cause: auth.ErrInvalidState, want: "expired_sign_in"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			login := &failingHostedLogin{cause: test.cause}
			service := NewService(ServiceConfig{
				HostedLogin:        login,
				HostedPublicOrigin: "https://maps.example.test",
			})
			t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

			const verifier = "desktop-verifier"
			challenge := sha256.Sum256([]byte(verifier))
			beginBody := `{"verifier_challenge":"` + base64.RawURLEncoding.EncodeToString(challenge[:]) + `"}`
			begin := httptest.NewRecorder()
			service.Handler().ServeHTTP(begin, httptest.NewRequest(http.MethodPost, "/v1/auth/desktop/begin", strings.NewReader(beginBody)))
			var started struct {
				AuthorizationURL string `json:"authorization_url"`
				HandoffID        string `json:"handoff_id"`
			}
			if begin.Code != http.StatusOK || json.Unmarshal(begin.Body.Bytes(), &started) != nil || started.HandoffID == "" || started.AuthorizationURL == "" {
				t.Fatalf("desktop begin = %d %s", begin.Code, begin.Body.String())
			}

			stateHash := sha256.Sum256([]byte("state-value"))
			browserStart := httptest.NewRecorder()
			service.Handler().ServeHTTP(browserStart, httptest.NewRequest(http.MethodGet, started.AuthorizationURL, nil))
			if browserStart.Code != http.StatusFound || len(browserStart.Result().Cookies()) != 1 {
				t.Fatalf("browser start = %d %s", browserStart.Code, browserStart.Body.String())
			}
			cookie := browserStart.Result().Cookies()[0]
			if cookie.Name != browserCookieName(stateHash) {
				t.Fatalf("browser cookie name = %q", cookie.Name)
			}

			callback := httptest.NewRequest(http.MethodGet, "/v1/auth/complete?state=state-value&code=code-value", nil)
			callback.AddCookie(cookie)
			callbackResult := httptest.NewRecorder()
			service.Handler().ServeHTTP(callbackResult, callback)
			if callbackResult.Code != http.StatusUnauthorized || strings.Contains(callbackResult.Body.String(), "provider-private-detail") {
				t.Fatalf("browser callback = %d %s", callbackResult.Code, callbackResult.Body.String())
			}

			exchangeBody := `{"handoff_id":"` + started.HandoffID + `","verifier":"` + verifier + `"}`
			exchange := httptest.NewRecorder()
			service.Handler().ServeHTTP(exchange, httptest.NewRequest(http.MethodPost, "/v1/auth/desktop/exchange", strings.NewReader(exchangeBody)))
			var failure struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(exchange.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if exchange.Code != http.StatusUnauthorized || failure.Code != test.want || strings.Contains(failure.Message, "provider-private-detail") {
				t.Fatalf("desktop exchange = %d %#v; want category %q", exchange.Code, failure, test.want)
			}
		})
	}
}

type failingHostedLogin struct {
	cause error
}

func (*failingHostedLogin) Begin(context.Context) (auth.BeginResult, error) {
	return auth.BeginResult{AuthorizationURL: "https://issuer.example/authorize?state=state-value", State: "state-value"}, nil
}

func (login *failingHostedLogin) Complete(context.Context, string, string) (auth.Session, error) {
	return auth.Session{}, errors.Join(login.cause, errors.New("provider-private-detail"))
}

func (*failingHostedLogin) Logout(string) {}
