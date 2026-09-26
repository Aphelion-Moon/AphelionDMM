package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/auth"
)

// Exercise the callback with the browser-owned secret, separately from the desktop verifier.
func completeBoundHostedLogin(t *testing.T, service *Service, baseURL, query string) (*http.Response, error) {
	t.Helper()
	stateHash := sha256.Sum256([]byte("state-value"))
	service.mutex.Lock()
	id := service.desktopAuthStates[stateHash]
	h := service.desktopHandoffs[id]
	h.browserStarted = true
	h.browserHash = sha256.Sum256([]byte("browser-secret"))
	service.desktopHandoffs[id] = h
	service.mutex.Unlock()
	request, err := http.NewRequest("GET", baseURL+"/v1/auth/complete?state=state-value&"+query, nil)
	if err != nil {
		return nil, err
	}
	request.AddCookie(&http.Cookie{Name: browserCookieName(stateHash), Value: "browser-secret"})
	return http.DefaultClient.Do(request)
}

func TestHostedCallbackRequiresBrowserBinding(t *testing.T) {
	login := &stubHostedLogin{session: auth.Session{Token: "secret", DisplayName: "Mapper", ExpiresAt: time.Now().Add(time.Hour)}}
	service := NewService(ServiceConfig{HostedLogin: login})
	defer service.Shutdown(context.Background())
	challenge := sha256.Sum256([]byte("verifier"))
	begin := httptest.NewRecorder()
	service.Handler().ServeHTTP(begin, httptest.NewRequest("POST", "/v1/auth/desktop/begin", strings.NewReader(`{"verifier_challenge":"`+base64.RawURLEncoding.EncodeToString(challenge[:])+`"}`)))
	var started struct {
		AuthorizationURL string `json:"authorization_url"`
		HandoffID        string `json:"handoff_id"`
	}
	if err := json.Unmarshal(begin.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	callback := httptest.NewRecorder()
	service.Handler().ServeHTTP(callback, httptest.NewRequest("GET", "/v1/auth/complete?state=state-value&code=code-value", nil))
	if callback.Code != http.StatusUnauthorized {
		t.Fatalf("unbound callback status = %d", callback.Code)
	}
	browser := httptest.NewRecorder()
	service.Handler().ServeHTTP(browser, httptest.NewRequest("GET", started.AuthorizationURL, nil))
	if browser.Code != http.StatusFound || len(browser.Result().Cookies()) != 1 {
		t.Fatalf("browser start = %d %s", browser.Code, browser.Body.String())
	}
	cookie := browser.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe browser cookie")
	}
	request := httptest.NewRequest("GET", "/v1/auth/complete?state=state-value&code=code-value", nil)
	request.AddCookie(cookie)
	callback = httptest.NewRecorder()
	service.Handler().ServeHTTP(callback, request)
	if callback.Code != http.StatusOK || strings.Contains(callback.Body.String(), "secret") {
		t.Fatalf("bound callback = %d %s", callback.Code, callback.Body.String())
	}
	replay := httptest.NewRecorder()
	service.Handler().ServeHTTP(replay, request)
	if replay.Code != http.StatusUnauthorized {
		t.Fatal("callback replay accepted")
	}
}
