package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/oauth2"
)

const (
	discordIssuer           = "https://discord.com"
	discordAuthorizationURL = "https://discord.com/oauth2/authorize"
	discordTokenURL         = "https://discord.com/api/oauth2/token"
	discordAPIBaseURL       = "https://discord.com/api/v10"
	discordResponseLimit    = 64 << 10
	discordRequestTimeout   = 10 * time.Second
	discordDefaultSession   = 12 * time.Hour
	discordMaximumSession   = 24 * time.Hour
	discordMaxDisplayName   = 128
)

var (
	ErrDiscordNotMember            = errors.New("account is not a member of the configured Discord server")
	ErrDiscordMissingScopes        = errors.New("authorization did not grant the required Discord permissions")
	ErrDiscordTemporaryFailure     = errors.New("the Discord service is temporarily unavailable")
	ErrDiscordAuthenticationFailed = errors.New("the Discord sign-in could not be completed")
	errDiscordResponseTooLarge     = errors.New("the Discord response exceeds the configured limit")
)

type DiscordConfig struct {
	ClientID     string
	ClientSecret string
	GuildID      string
	RedirectURL  string
	SessionTTL   time.Duration
	HTTPClient   *http.Client
	Now          func() time.Time
}

type DiscordFlow struct {
	oauth      oauth2.Config
	client     *http.Client
	guildID    string
	sessionTTL time.Duration
	now        func() time.Time
}

func NewDiscordFlow(config DiscordConfig) (*DiscordFlow, error) {
	if !validDiscordID(config.ClientID) || !validDiscordID(config.GuildID) {
		return nil, fmt.Errorf("the Discord client and guild IDs must be decimal IDs of at most 20 digits")
	}
	if config.ClientSecret == "" || len(config.ClientSecret) > 64<<10 {
		return nil, fmt.Errorf("the Discord client secret is required and must not exceed 64 KiB")
	}
	if err := validateDiscordRedirect(config.RedirectURL); err != nil {
		return nil, err
	}
	ttl := config.SessionTTL
	if ttl == 0 {
		ttl = discordDefaultSession
	}
	if ttl < 0 || ttl > discordMaximumSession {
		return nil, fmt.Errorf("the Discord session lifetime must be positive and at most 24 hours")
	}

	transport := http.RoundTripper(http.DefaultTransport)
	if config.HTTPClient != nil && config.HTTPClient.Transport != nil {
		transport = config.HTTPClient.Transport
	}
	client := &http.Client{
		Transport: boundedDiscordTransport{next: transport, maxBytes: discordResponseLimit},
		Timeout:   discordRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &DiscordFlow{
		oauth: oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  config.RedirectURL,
			Scopes:       []string{"identify", "guilds.members.read"},
			Endpoint: oauth2.Endpoint{
				AuthURL:   discordAuthorizationURL,
				TokenURL:  discordTokenURL,
				AuthStyle: oauth2.AuthStyleInParams,
			},
		},
		client: client, guildID: config.GuildID, sessionTTL: ttl, now: config.Now,
	}, nil
}

func (flow *DiscordFlow) AuthorizationURL(state, _, _ string) string {
	// Discord documents transaction-bound state but not PKCE or OIDC nonce support.
	// The hosted desktop handoff verifier remains independent of provider PKCE.
	return flow.oauth.AuthCodeURL(state)
}

func (flow *DiscordFlow) Exchange(ctx context.Context, code, _, _ string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, discordRequestTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, flow.client)
	token, err := flow.oauth.Exchange(ctx, code)
	if err != nil {
		return Identity{}, classifyDiscordTokenExchangeError(ctx, err)
	}
	defer func() {
		token.AccessToken = ""
		token.RefreshToken = ""
	}()
	if token.AccessToken == "" || len(token.AccessToken) > 8192 || !strings.EqualFold(token.TokenType, "Bearer") {
		return Identity{}, ErrDiscordAuthenticationFailed
	}
	if !discordScopesGranted(token.Extra("scope")) {
		return Identity{}, ErrDiscordMissingScopes
	}

	userBody, err := flow.get(ctx, discordAPIBaseURL+"/users/@me", token.AccessToken)
	if err != nil {
		return Identity{}, err
	}
	var user struct {
		ID       string `json:"id"`
		Global   string `json:"global_name"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userBody, &user); err != nil || !validDiscordID(user.ID) {
		return Identity{}, ErrDiscordAuthenticationFailed
	}
	memberBody, err := flow.get(ctx, discordAPIBaseURL+"/users/@me/guilds/"+url.PathEscape(flow.guildID)+"/member", token.AccessToken)
	if err != nil {
		return Identity{}, err
	}
	trimmedMember := bytes.TrimSpace(memberBody)
	if len(trimmedMember) == 0 || trimmedMember[0] != '{' {
		return Identity{}, ErrDiscordAuthenticationFailed
	}
	var member struct{}
	if err := json.Unmarshal(memberBody, &member); err != nil {
		return Identity{}, ErrDiscordAuthenticationFailed
	}

	displayName := boundedDiscordName(user.Global)
	if displayName == "" {
		displayName = boundedDiscordName(user.Username)
	}
	if displayName == "" {
		return Identity{}, ErrDiscordAuthenticationFailed
	}
	return Identity{
		Issuer: discordIssuer, Subject: user.ID, DisplayName: displayName,
		ExpiresAt: flow.now().Add(flow.sessionTTL),
	}, nil
}

func (flow *DiscordFlow) get(ctx context.Context, endpoint, accessToken string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrDiscordAuthenticationFailed
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := flow.client.Do(request)
	if err != nil {
		return nil, ErrDiscordTemporaryFailure
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		if errors.Is(err, errDiscordResponseTooLarge) {
			return nil, ErrDiscordAuthenticationFailed
		}
		return nil, ErrDiscordTemporaryFailure
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, classifyDiscordAPIError(response.StatusCode, body)
	}
	if len(body) == 0 {
		return nil, ErrDiscordAuthenticationFailed
	}
	return body, nil
}

func classifyDiscordTokenExchangeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ErrDiscordTemporaryFailure
	}
	var retrieveError *oauth2.RetrieveError
	if errors.As(err, &retrieveError) && retrieveError.Response != nil {
		status := retrieveError.Response.StatusCode
		if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError || status == http.StatusRequestTimeout {
			return ErrDiscordTemporaryFailure
		}
		return ErrDiscordAuthenticationFailed
	}
	return ErrDiscordTemporaryFailure
}

func classifyDiscordAPIError(statusCode int, body []byte) error {
	if statusCode == http.StatusNotFound {
		var apiError struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(body, &apiError) == nil && apiError.Code == 10007 {
			return ErrDiscordNotMember
		}
	}
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout || statusCode >= http.StatusInternalServerError {
		return ErrDiscordTemporaryFailure
	}
	return ErrDiscordAuthenticationFailed
}

func discordScopesGranted(value any) bool {
	scopes, ok := value.(string)
	if !ok {
		return false
	}
	granted := make(map[string]struct{}, 2)
	for _, scope := range strings.Fields(scopes) {
		granted[scope] = struct{}{}
	}
	_, identify := granted["identify"]
	_, member := granted["guilds.members.read"]
	return identify && member
}

func validDiscordID(value string) bool {
	if len(value) == 0 || len(value) > 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validateDiscordRedirect(value string) error {
	redirect, err := url.Parse(value)
	if err != nil || redirect.Scheme != "https" || redirect.Host == "" || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.Path != "/v1/auth/complete" {
		return fmt.Errorf("the Discord redirect URL must be HTTPS /v1/auth/complete without credentials, query, or fragment")
	}
	return nil
}

func boundedDiscordName(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= discordMaxDisplayName {
		return value
	}
	value = value[:discordMaxDisplayName]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value)
}

type boundedDiscordTransport struct {
	next     http.RoundTripper
	maxBytes int64
}

func (transport boundedDiscordTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.next.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = &boundedDiscordBody{body: response.Body, remaining: transport.maxBytes}
	return response, nil
}

type boundedDiscordBody struct {
	body      io.ReadCloser
	remaining int64
}

func (body *boundedDiscordBody) Read(buffer []byte) (int, error) {
	if body.remaining > 0 {
		if int64(len(buffer)) > body.remaining {
			buffer = buffer[:body.remaining]
		}
		read, err := body.body.Read(buffer)
		body.remaining -= int64(read)
		return read, err
	}
	var probe [1]byte
	read, err := body.body.Read(probe[:])
	if read > 0 {
		return 0, errDiscordResponseTooLarge
	}
	if err == nil {
		return 0, errDiscordResponseTooLarge
	}
	return 0, err
}

func (body *boundedDiscordBody) Close() error {
	return body.body.Close()
}
