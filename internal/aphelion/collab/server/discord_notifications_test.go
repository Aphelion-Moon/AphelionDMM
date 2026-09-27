package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collabstore "sdmm/internal/aphelion/collab/store"
)

func TestDiscordSessionStatusProtectsPrivateMetadataAndBoundsContent(t *testing.T) {
	sessions := []collabstore.HostedSession{
		{SessionID: "private-id", Visibility: collabstore.HostedVisibilityPrivate, Title: "private-title", MapLabel: "private-map"},
		{SessionID: "public-id", Visibility: collabstore.HostedVisibilityCommunity, Title: "Public @everyone <@123>"},
	}
	status := discordSessionStatus(sessions, map[string]int{"private-id": 2, "public-id": 1})
	if strings.Contains(status, "private-title") || strings.Contains(status, "private-id") || strings.Contains(status, "private-map") || !strings.Contains(status, "Private sessions: 1 (1 active)") || !strings.Contains(status, "1 participant") {
		t.Fatalf("unsafe or incomplete status: %s", status)
	}
	for i := 0; i < 100; i++ {
		sessions = append(sessions, collabstore.HostedSession{Visibility: collabstore.HostedVisibilityCommunity, Title: strings.Repeat("界", 128)})
	}
	if len([]rune(discordSessionStatus(sessions, nil))) > 2000 {
		t.Fatal("Discord content limit exceeded")
	}
}

func TestDiscordReporterChecksGuildSuppressesMentionsAndCoalesces(t *testing.T) {
	posts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot test-secret" {
			t.Error("missing bot authentication")
		}
		if r.URL.Path == "/channels/123" {
			_, _ = w.Write([]byte(`{"guild_id":"456","type":0}`))
			return
		}
		if r.Method != "POST" || r.URL.Path != "/channels/123/messages" {
			t.Error("unexpected endpoint")
		}
		var body struct {
			Content string `json:"content"`
			Allowed struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Content != "status @everyone" || body.Allowed.Parse == nil || len(body.Allowed.Parse) != 0 {
			t.Error("mention suppression or content missing")
		}
		posts++
		w.WriteHeader(200)
	}))
	defer api.Close()
	reporter := discordSessionReporter{client: api.Client(), baseURL: api.URL, channelID: "123", guildID: "456", token: "test-secret"}
	for i := 0; i < 2; i++ {
		if _, err := reporter.publish(context.Background(), "status @everyone"); err != nil {
			t.Fatal(err)
		}
	}
	if posts != 1 {
		t.Fatalf("unchanged status posted %d times", posts)
	}
	reporter.guildID = "different"
	reporter.channelVerified = false
	if _, err := reporter.publish(context.Background(), "changed"); err == nil || posts != 1 {
		t.Fatal("wrong guild accepted")
	}
}

func TestDiscordReporterHonorsRateLimitAndDoesNotLeakResponse(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"retry_after":123,"message":"secret-response"}`))
	}))
	defer api.Close()
	reporter := discordSessionReporter{client: api.Client(), baseURL: api.URL, channelID: "123", guildID: "456", token: "test-secret", channelVerified: true}
	delay, err := reporter.publish(context.Background(), "status")
	if delay < 123*time.Second || err == nil || strings.Contains(err.Error(), "secret") || reporter.lastContent != "" {
		t.Fatalf("unsafe rate limit result: %v %v", delay, err)
	}
}

func TestDiscordNotificationsConfigurationIsOptionalAndStrict(t *testing.T) {
	block := "\ndiscord_notifications:\n  guild_id: '456'\n  channel_id: '123'\n  bot_token:\n    environment: APHELIONDMM_DISCORD_BOT_TOKEN\n"
	if _, err := LoadHostedConfig(strings.NewReader(validHostedYAML + block)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{strings.Replace(block, "'123'", "'../channels'", 1), strings.Replace(block, "    environment: APHELIONDMM_DISCORD_BOT_TOKEN", "", 1)} {
		if _, err := LoadHostedConfig(strings.NewReader(validHostedYAML + invalid)); err == nil {
			t.Fatal("invalid notification configuration accepted")
		}
	}
}
