package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDiscordFlowChecksConfiguredGuildAndBuildsDiscordIdentity(t *testing.T) {
	now := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	var requests []string
	transport := discordRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.Host+request.URL.Path)
		switch request.URL.Path {
		case "/api/oauth2/token":
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("code") != "authorization-code" || request.Form.Get("redirect_uri") != "https://maps.example.test/v1/auth/complete" {
				t.Fatalf("token form = %#v", request.Form)
			}
			if request.Form.Get("client_id") != "123456" || request.Form.Get("client_secret") != "client-secret" {
				t.Fatalf("token client credentials = %#v", request.Form)
			}
			if request.Form.Has("code_verifier") {
				t.Fatal("Discord token request unexpectedly claimed PKCE support")
			}
			return discordHTTPResponse(request, http.StatusOK, `{"access_token":"short-lived-access-token","refresh_token":"discard-me","token_type":"Bearer","expires_in":900,"scope":"identify guilds.members.read"}`), nil
		case "/api/v10/users/@me":
			if request.Header.Get("Authorization") != "Bearer short-lived-access-token" {
				t.Fatalf("user authorization = %q", request.Header.Get("Authorization"))
			}
			return discordHTTPResponse(request, http.StatusOK, `{"id":"998877665544332211","global_name":"Discord Mapper","username":"fallback","email":"not-requested","roles":["ignored"]}`), nil
		case "/api/v10/users/@me/guilds/112233445566778899/member":
			if request.Header.Get("Authorization") != "Bearer short-lived-access-token" {
				t.Fatalf("membership authorization = %q", request.Header.Get("Authorization"))
			}
			return discordHTTPResponse(request, http.StatusOK, `{"user":{"id":"998877665544332211"},"roles":["ignored"],"pending":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected provider request %s", request.URL)
		}
	})
	flow, err := NewDiscordFlow(DiscordConfig{
		ClientID: "123456", ClientSecret: "client-secret", GuildID: "112233445566778899",
		RedirectURL: "https://maps.example.test/v1/auth/complete", SessionTTL: 6 * time.Hour,
		Now: func() time.Time { return now }, HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}

	authorizationURL := flow.AuthorizationURL("one-time-state", "ignored-nonce", "ignored-verifier")
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "discord.com" || parsed.Path != "/oauth2/authorize" || query.Get("response_type") != "code" || query.Get("client_id") != "123456" || query.Get("redirect_uri") != "https://maps.example.test/v1/auth/complete" || query.Get("state") != "one-time-state" {
		t.Fatalf("authorization URL = %s", authorizationURL)
	}
	if query.Get("scope") != "identify guilds.members.read" || query.Has("code_challenge") || query.Has("code_challenge_method") || query.Has("nonce") {
		t.Fatalf("authorization parameters = %v", query)
	}

	identity, err := flow.Exchange(context.Background(), "authorization-code", "ignored-verifier", "ignored-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Issuer != "https://discord.com" || identity.Subject != "998877665544332211" || identity.DisplayName != "Discord Mapper" || !identity.ExpiresAt.Equal(now.Add(6*time.Hour)) {
		t.Fatalf("identity = %#v", identity)
	}
	if len(requests) != 3 || requests[2] != "GET discord.com/api/v10/users/@me/guilds/112233445566778899/member" {
		t.Fatalf("provider requests = %#v", requests)
	}
}

func TestDiscordFlowClassifiesProviderFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       error
	}{
		{name: "unknown member", statusCode: http.StatusNotFound, body: `{"message":"private-detail","code":10007}`, want: ErrDiscordNotMember},
		{name: "unknown guild is not nonmember proof", statusCode: http.StatusNotFound, body: `{"message":"private-detail","code":10004}`, want: ErrDiscordAuthenticationFailed},
		{name: "forbidden is not nonmember proof", statusCode: http.StatusForbidden, body: `{"message":"private-detail","code":50013}`, want: ErrDiscordAuthenticationFailed},
		{name: "rate limit is temporary", statusCode: http.StatusTooManyRequests, body: `{"message":"private-detail","code":0}`, want: ErrDiscordTemporaryFailure},
		{name: "provider outage is temporary", statusCode: http.StatusServiceUnavailable, body: `{"message":"private-detail","code":0}`, want: ErrDiscordTemporaryFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := discordRoundTripper(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/api/oauth2/token":
					return discordHTTPResponse(request, http.StatusOK, `{"access_token":"access","token_type":"Bearer","expires_in":900,"scope":"identify guilds.members.read"}`), nil
				case "/api/v10/users/@me":
					return discordHTTPResponse(request, http.StatusOK, `{"id":"998877665544332211","username":"Mapper","global_name":"  "}`), nil
				case "/api/v10/users/@me/guilds/112233445566778899/member":
					return discordHTTPResponse(request, test.statusCode, test.body), nil
				default:
					return nil, fmt.Errorf("unexpected provider request %s", request.URL)
				}
			})
			flow, err := NewDiscordFlow(DiscordConfig{
				ClientID: "123456", ClientSecret: "client-secret", GuildID: "112233445566778899",
				RedirectURL: "https://maps.example.test/v1/auth/complete", HTTPClient: &http.Client{Transport: transport},
			})
			if err != nil {
				t.Fatal(err)
			}
			identity, err := flow.Exchange(context.Background(), "authorization-code", "", "")
			if !errors.Is(err, test.want) {
				t.Fatalf("Exchange() error = %v, want %v", err, test.want)
			}
			if strings.Contains(fmt.Sprint(err), "private-detail") || strings.Contains(fmt.Sprint(err), "access") || identity.Subject != "" {
				t.Fatalf("Exchange() exposed provider data or identity: identity=%#v error=%v", identity, err)
			}
		})
	}
}

func TestDiscordFlowUsesUsernameWhenGlobalNameIsBlank(t *testing.T) {
	flow := newDiscordTestFlow(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/oauth2/token":
			return discordHTTPResponse(request, http.StatusOK, `{"access_token":"access","token_type":"Bearer","scope":"identify guilds.members.read"}`), nil
		case "/api/v10/users/@me":
			return discordHTTPResponse(request, http.StatusOK, `{"id":"998877665544332211","username":"Fallback Name","global_name":"  "}`), nil
		case "/api/v10/users/@me/guilds/112233445566778899/member":
			return discordHTTPResponse(request, http.StatusOK, `{"roles":["ignored"],"pending":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected provider request %s", request.URL)
		}
	})
	identity, err := flow.Exchange(context.Background(), "authorization-code", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if identity.DisplayName != "Fallback Name" {
		t.Fatalf("display name = %q", identity.DisplayName)
	}
}

func TestDiscordFlowRejectsMissingScopesAndBoundsProviderResponses(t *testing.T) {
	t.Run("missing scope", func(t *testing.T) {
		calls := 0
		flow := newDiscordTestFlow(t, func(request *http.Request) (*http.Response, error) {
			calls++
			return discordHTTPResponse(request, http.StatusOK, `{"access_token":"access","token_type":"Bearer","scope":"identify"}`), nil
		})
		if _, err := flow.Exchange(context.Background(), "authorization-code", "", ""); !errors.Is(err, ErrDiscordMissingScopes) {
			t.Fatalf("Exchange() error = %v", err)
		}
		if calls != 1 {
			t.Fatalf("provider calls = %d, want token exchange only", calls)
		}
	})

	t.Run("oversized profile", func(t *testing.T) {
		calls := 0
		flow := newDiscordTestFlow(t, func(request *http.Request) (*http.Response, error) {
			calls++
			if request.URL.Path == "/api/oauth2/token" {
				return discordHTTPResponse(request, http.StatusOK, `{"access_token":"access","token_type":"Bearer","scope":"identify guilds.members.read"}`), nil
			}
			return discordHTTPResponse(request, http.StatusOK, `{"id":"998877665544332211","username":"`+strings.Repeat("x", 70<<10)+`"}`), nil
		})
		if _, err := flow.Exchange(context.Background(), "authorization-code", "", ""); !errors.Is(err, ErrDiscordAuthenticationFailed) {
			t.Fatalf("Exchange() error = %v", err)
		}
		if calls != 2 {
			t.Fatalf("provider calls = %d, want token exchange and profile only", calls)
		}
	})
}

func TestDiscordFlowDoesNotForwardCredentialsAcrossRedirects(t *testing.T) {
	var hosts []string
	flow := newDiscordTestFlow(t, func(request *http.Request) (*http.Response, error) {
		hosts = append(hosts, request.URL.Host)
		if request.URL.Path == "/api/oauth2/token" {
			return discordHTTPResponse(request, http.StatusOK, `{"access_token":"access","token_type":"Bearer","scope":"identify guilds.members.read"}`), nil
		}
		response := discordHTTPResponse(request, http.StatusFound, "")
		response.Header.Set("Location", "https://attacker.example/collect")
		return response, nil
	})
	if _, err := flow.Exchange(context.Background(), "authorization-code", "", ""); !errors.Is(err, ErrDiscordAuthenticationFailed) {
		t.Fatalf("Exchange() error = %v", err)
	}
	if len(hosts) != 2 || hosts[0] != "discord.com" || hosts[1] != "discord.com" {
		t.Fatalf("request hosts = %#v", hosts)
	}
}

func newDiscordTestFlow(t *testing.T, roundTrip func(*http.Request) (*http.Response, error)) *DiscordFlow {
	t.Helper()
	flow, err := NewDiscordFlow(DiscordConfig{
		ClientID: "123456", ClientSecret: "client-secret", GuildID: "112233445566778899",
		RedirectURL: "https://maps.example.test/v1/auth/complete", HTTPClient: &http.Client{Transport: discordRoundTripper(roundTrip)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

type discordRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper discordRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func discordHTTPResponse(request *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
