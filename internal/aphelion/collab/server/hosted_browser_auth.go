package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"

	"sdmm/internal/aphelion/collab/auth"
)

func browserCookieName(state [sha256.Size]byte) string {
	return fmt.Sprintf("__Host-aphelion-login-%x", state[:8])
}

func browserHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (service *Service) handleHostedBrowserStart(w http.ResponseWriter, r *http.Request) {
	browserHeaders(w)
	id, start := r.URL.Query().Get("handoff"), r.URL.Query().Get("start")
	if len(id) > 128 || len(start) > 128 || id == "" || start == "" {
		writeError(w, 400, "invalid_request", "Invalid browser sign-in.")
		return
	}
	hash := sha256.Sum256([]byte(id))
	startHash := sha256.Sum256([]byte(start))
	cookie, err := randomToken()
	if err != nil {
		writeError(w, 503, "unavailable", "Browser sign-in unavailable.")
		return
	}
	service.mutex.Lock()
	h, ok := service.desktopHandoffs[hash]
	if !ok || h.browserStarted || !service.config.Now().Before(h.expiresAt) || subtle.ConstantTimeCompare(startHash[:], h.startHash[:]) != 1 {
		service.mutex.Unlock()
		writeError(w, 401, "unauthorized", "Browser sign-in expired. Start again from the desktop.")
		return
	}
	h.browserStarted = true
	h.browserHash = sha256.Sum256([]byte(cookie))
	service.desktopHandoffs[hash] = h
	service.mutex.Unlock()
	http.SetCookie(w, &http.Cookie{Name: browserCookieName(h.stateHash), Value: cookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, h.authorizationURL, http.StatusFound)
}

func (service *Service) handleHostedAuthComplete(w http.ResponseWriter, r *http.Request) {
	browserHeaders(w)
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	if state == "" || len(state) > 512 || len(code) > 4096 {
		writeError(w, 400, "invalid_request", "Invalid sign-in callback.")
		return
	}
	stateHash := sha256.Sum256([]byte(state))
	cookie, err := r.Cookie(browserCookieName(stateHash))
	if err != nil {
		writeError(w, 401, "unauthorized", "Sign-in browser binding is missing. Start again from the desktop.")
		return
	}
	browserHash := sha256.Sum256([]byte(cookie.Value))
	service.mutex.Lock()
	handoffHash, exists := service.desktopAuthStates[stateHash]
	h, found := service.desktopHandoffs[handoffHash]
	if !exists || !found || !h.browserStarted || !service.config.Now().Before(h.expiresAt) || subtle.ConstantTimeCompare(browserHash[:], h.browserHash[:]) != 1 {
		service.mutex.Unlock()
		writeError(w, 401, "unauthorized", "Sign-in is invalid or expired.")
		return
	}
	delete(service.desktopAuthStates, stateHash)
	service.mutex.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookie.Name, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if r.URL.Query().Get("error") != "" {
		code = ""
	}
	session, exchangeErr := service.config.HostedLogin.Complete(r.Context(), state, code)
	service.mutex.Lock()
	h, found = service.desktopHandoffs[handoffHash]
	if !found || !service.config.Now().Before(h.expiresAt) {
		service.mutex.Unlock()
		if exchangeErr == nil {
			service.config.HostedLogin.Logout(session.Token)
		}
		writeError(w, 401, "unauthorized", "Sign-in expired. Start again from the desktop.")
		return
	}
	if exchangeErr != nil {
		h.terminalError = "sign_in_failed"
		switch {
		case errors.Is(exchangeErr, auth.ErrDiscordNotMember):
			h.terminalError = "not_a_member"
		case errors.Is(exchangeErr, auth.ErrDiscordMissingScopes):
			h.terminalError = "missing_permissions"
		case errors.Is(exchangeErr, auth.ErrDiscordTemporaryFailure):
			h.terminalError = "provider_unavailable"
		case errors.Is(exchangeErr, auth.ErrInvalidState):
			h.terminalError = "expired_sign_in"
		}
		if r.URL.Query().Get("error") == "access_denied" {
			h.terminalError = "consent_denied"
		}
	} else {
		h.session = &session
	}
	service.desktopHandoffs[handoffHash] = h
	service.mutex.Unlock()
	if exchangeErr != nil {
		writeError(w, 401, "unauthorized", "Sign-in was not completed. Return to the desktop.")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "complete"})
}
